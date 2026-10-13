import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../lib/api'
import type { CL, Readiness } from '../lib/api'
import { getViewSettings, saveViewSettings } from '../lib/viewSettings'
import { Spinner } from './Spinner'

type Phase = 'checking' | 'plan' | 'merging' | 'done'
type Outcome = { kind: 'merged' } | { kind: 'failed'; message: string }

// While a CL is only waiting on running builds, re-check this often so the
// plan updates by itself when CI finishes.
const POLL_MS = 4000

// The "sits on CL-x" blocker is only real if CL-x isn't merging first.
const BASE_REASON = /^sits on CL-\d+/

// Merge stack: checks every CL in stack order, then merges from the bottom up
// to the first one that isn't ready (a stack can only merge bottom-up). With
// the "without asking" preference on, it skips straight from the check to
// merging; the dialog still shows progress and the result.
export function MergeStackModal({ cls, onClose, onMerged }: {
  cls: CL[] // stack order, bottom (root) first
  onClose: () => void
  onMerged: () => void
}) {
  const [phase, setPhase] = useState<Phase>('checking')
  const [readiness, setReadiness] = useState<Map<number, Readiness>>(new Map())
  const [checkError, setCheckError] = useState('')
  const [outcomes, setOutcomes] = useState<Map<number, Outcome>>(new Map())
  const [skipConfirm, setSkipConfirm] = useState(!!getViewSettings().merge?.skip_confirm)
  const [current, setCurrent] = useState<number | null>(null) // CL being merged right now

  // How many CLs, from the bottom, can merge now.
  function readyPrefix(rd: Map<number, Readiness>): number {
    const merging = new Set<number>()
    for (const cl of cls) {
      const r = rd.get(cl.id)
      if (!r) break
      const blockers = r.reasons.filter(reason => !BASE_REASON.test(reason))
      const baseOk = !r.base_cl || merging.has(r.base_cl)
      if (blockers.length > 0 || !baseOk) break
      merging.add(cl.id)
    }
    return merging.size
  }

  async function merge(count: number) {
    setPhase('merging')
    for (const cl of cls.slice(0, count)) {
      setCurrent(cl.id)
      try {
        await api.submitCL(cl.id)
        setOutcomes(prev => new Map(prev).set(cl.id, { kind: 'merged' }))
        onMerged() // the list behind the dialog updates as each CL lands
      } catch (e: any) {
        setOutcomes(prev => new Map(prev).set(cl.id, { kind: 'failed', message: e?.message ?? 'merge failed' }))
        break
      }
    }
    setCurrent(null)
    setPhase('done')
  }

  useEffect(() => {
    let cancelled = false
    Promise.all(cls.map(cl => api.getReadiness(cl.id).then(r => [cl.id, r] as const)))
      .then(entries => {
        if (cancelled) return
        const rd = new Map(entries)
        setReadiness(rd)
        const n = readyPrefix(rd)
        if (n > 0 && getViewSettings().merge?.skip_confirm) merge(n)
        else setPhase('plan')
      })
      .catch(e => {
        if (cancelled) return
        setCheckError(e?.message ?? 'Could not check the stack')
        setPhase('plan')
      })
    return () => { cancelled = true }
    // cls is fixed for the dialog's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Keep the plan live while something is only waiting on CI.
  const anyWaiting = [...readiness.values()].some(r => r.waiting)
  useEffect(() => {
    if (phase !== 'plan' || !anyWaiting) return
    const t = setInterval(() => {
      Promise.all(cls.map(cl => api.getReadiness(cl.id).then(r => [cl.id, r] as const)))
        .then(entries => setReadiness(new Map(entries)))
        .catch(() => {})
    }, POLL_MS)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, anyWaiting])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape' && phase !== 'merging') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, phase])

  function toggleSkip(on: boolean) {
    setSkipConfirm(on)
    saveViewSettings({ merge: { skip_confirm: on } })
  }

  const n = readyPrefix(readiness)
  const blocked = cls[n]
  const blockedReasons = blocked ? (readiness.get(blocked.id)?.reasons ?? []).filter(r => !BASE_REASON.test(r) || n === 0) : []
  const branch = cls[0]?.target_branch ?? 'the branch'
  const merged = [...outcomes.values()].filter(o => o.kind === 'merged').length
  const failure = [...outcomes.entries()].find(([, o]) => o.kind === 'failed')

  function summary(): string {
    if (checkError) return checkError
    if (phase === 'checking') return 'Checking reviews and builds…'
    if (phase === 'merging') return `Merging into ${branch}, bottom of the stack first…`
    if (phase === 'done') {
      const base = `Merged ${merged} CL${merged === 1 ? '' : 's'} into ${branch}.`
      return failure ? `${base} Stopped at CL #${failure[0]}: ${(failure[1] as { message: string }).message}` : base
    }
    if (n === cls.length) return `All ${cls.length} CLs are ready. They merge into ${branch} from the bottom of the stack up.`
    const why = blockedReasons.length ? ` (${blockedReasons.join('; ')})` : ''
    if (n === 0) return `Nothing can merge yet. CL #${blocked.id} is blocked${why}.`
    const above = cls.length - n - 1
    return `${n} of ${cls.length} can merge now. CL #${blocked.id} is blocked${why}, so it` +
      (above > 0 ? ` and the ${above} CL${above === 1 ? '' : 's'} above it stay open.` : ' stays open.')
  }

  type Row = { mark: string; color: string; note: string; spin?: boolean; pop?: boolean }
  function rowState(cl: CL, i: number): Row {
    const o = outcomes.get(cl.id)
    if (o?.kind === 'merged') return { mark: '✓', color: 'var(--green)', note: 'merged', pop: true }
    if (o?.kind === 'failed') return { mark: '✕', color: 'var(--red)', note: o.message, pop: true }
    if (cl.id === current) return { mark: '', color: 'var(--accent)', note: 'merging…', spin: true }
    const r = readiness.get(cl.id)
    if (!r) return { mark: '', color: 'var(--t3)', note: phase === 'checking' ? 'checking…' : '', spin: phase === 'checking' }
    if (i < n) return { mark: '✓', color: 'var(--accent)', note: phase === 'merging' ? 'queued' : 'ready' }
    if (i === n) {
      const reasons = r.reasons.filter(reason => !BASE_REASON.test(reason) || n === 0)
      return r.waiting
        ? { mark: '', color: 'var(--yellow)', note: reasons.join('; '), spin: true }
        : { mark: '✕', color: 'var(--red)', note: reasons.join('; ') }
    }
    return { mark: '–', color: 'var(--t3)', note: `waits for CL #${blocked?.id}` }
  }

  const body = (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 60,
        background: 'rgba(0,0,0,0.55)',
        backdropFilter: 'blur(8px)', WebkitBackdropFilter: 'blur(8px)',
        display: 'grid', placeItems: 'center', padding: '2rem',
        // Without a fixed track, one long unbreakable line (a CL title, a diff
        // line) widens the column past the viewport and the card centers off-screen.
        gridTemplateColumns: 'minmax(0, 1fr)',
      }}
      onClick={() => { if (phase !== 'merging') onClose() }}
    >
      <div
        className="card"
        role="dialog"
        aria-label="Merge stack"
        style={{ width: 'min(620px, 100%)', maxHeight: '85vh', display: 'flex', flexDirection: 'column', padding: 0 }}
        onClick={e => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', padding: '0.85rem 1rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
          <span style={{ fontWeight: 700 }}>Merge stack</span>
          <span style={{ fontSize: '0.8rem', color: 'var(--t3)' }}>{cls.length} CLs → {branch}</span>
          {phase !== 'merging' && (
            <button className="btn btn-ghost btn-icon btn-sm" style={{ marginLeft: 'auto' }} onClick={onClose} aria-label="Close">✕</button>
          )}
        </div>

        <p style={{ margin: 0, padding: '0.85rem 1rem 0.25rem', fontSize: '0.88rem', color: checkError ? 'var(--red)' : 'var(--t1)' }}>
          {summary()}
        </p>

        {/* Top of the stack first, the way it reads in the list; merging runs from the bottom. */}
        <ol style={{ listStyle: 'none', margin: 0, padding: '0.5rem 1rem', overflow: 'auto', display: 'flex', flexDirection: 'column-reverse' }}>
          {cls.map((cl, i) => {
            const st = rowState(cl, i)
            return (
              <li key={cl.id} style={{ display: 'grid', gridTemplateColumns: '1.2rem 1fr', gap: '0.5rem', padding: '0.45rem 0', borderBottom: 'var(--hairline) solid var(--border)' }}>
                <span style={{ color: st.color, fontWeight: 700, display: 'grid', placeItems: 'center', height: '1.3rem' }}>
                  {st.spin
                    ? <Spinner size="0.85em" />
                    : <span key={st.mark + st.note} className={st.pop ? 'merge-mark-pop' : undefined}>{st.mark}</span>}
                </span>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: '0.86rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    <span style={{ color: 'var(--t3)' }}>#{cl.id}</span> {cl.title}
                  </div>
                  {st.note && <div style={{ fontSize: '0.76rem', color: st.color === 'var(--accent)' ? 'var(--t3)' : st.color }}>{st.note}</div>}
                </div>
              </li>
            )
          })}
        </ol>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap', padding: '0.75rem 1rem', borderTop: 'var(--hairline) solid var(--border)' }}>
          {phase === 'plan' && anyWaiting && (
            <span style={{ fontSize: '0.76rem', color: 'var(--t3)' }}>Updating as builds finish…</span>
          )}
          {phase === 'plan' && n > 0 && (
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.45rem', fontSize: '0.8rem', color: 'var(--t2)', cursor: 'pointer' }}>
              <input id="merge-skip-confirm" type="checkbox" checked={skipConfirm} onChange={e => toggleSkip(e.target.checked)}
                style={{ accentColor: 'var(--accent)' }} />
              Always merge the ready CLs without asking
            </label>
          )}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.5rem' }}>
            {phase === 'checking' || phase === 'merging' ? (
              <Spinner size="1.1em" />
            ) : phase === 'plan' && n > 0 ? (
              <>
                <button className="btn btn-ghost btn-sm" onClick={onClose}>Cancel</button>
                <button className="btn btn-primary btn-sm" onClick={() => merge(n)}>
                  {n === cls.length ? `Merge all ${n}` : `Merge ${n} CL${n === 1 ? '' : 's'}`}
                </button>
              </>
            ) : (
              <button className="btn btn-primary btn-sm" onClick={onClose}>{phase === 'done' ? 'Done' : 'Close'}</button>
            )}
          </div>
        </div>
      </div>
    </div>
  )

  return createPortal(body, document.body)
}
