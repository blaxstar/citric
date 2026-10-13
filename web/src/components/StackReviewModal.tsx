import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../lib/api'
import type { CL, Build, Comment, FileDiff, Review } from '../lib/api'
import { getUsername } from '../lib/auth'
import { humanTime, buildDuration } from '../lib/theme'
import { latestBuildPerTool } from '../lib/builds'
import { BuildBadge, VerdictBadge } from './Badge'
import { DiffViewer } from './DiffViewer'
import { Spinner } from './Spinner'
import { UserLink } from './UserLink'

// What happened to each CL during this pass. 'lgtm' also covers CLs that
// already carried your LGTM on the latest patchset when the pass started.
// 'own' is a CL you wrote in a repo that requires another reviewer: your LGTM
// wouldn't count, so the pass leaves it for someone else.
export type StepResult = 'lgtm' | 'needs_work' | 'skipped' | 'own'

type Loaded = {
  diffs: FileDiff[]
  comments: Comment[]
  reviews: Review[]
  builds: Build[]
  commitSha?: string
}

function reviewerOf(r: Review): string {
  return r.author ?? r.reviewer
}

// Typing in a comment box must not cast a vote.
function isTyping(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null
  return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
}

// Batch review: walks the stack bottom-up, one CL at a time — read the diff,
// LGTM, and it moves on to the next. When the last CL is done it hands back to
// the stack view, which offers to merge everything at once.
export function StackReviewModal({ cls, onClose, onFinished }: {
  cls: CL[] // stack order, bottom (root) first
  onClose: () => void
  onFinished: (results: Map<number, StepResult>) => void
}) {
  const me = getUsername()
  const open = cls.filter(cl => cl.status === 'open')

  const [index, setIndex] = useState<number | null>(null) // null until we know where to start
  const [loaded, setLoaded] = useState<Map<number, Loaded>>(new Map())
  const [results, setResults] = useState<Map<number, StepResult>>(new Map())
  const [busy, setBusy] = useState<'lgtm' | 'needs_work' | null>(null)
  const [error, setError] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const inflight = useRef<Set<number>>(new Set())

  function load(id: number) {
    if (inflight.current.has(id)) return
    inflight.current.add(id)
    Promise.all([api.getDiff(id), api.getComments(id), api.getReviews(id), api.getBuilds(id), api.getPatchsets(id)])
      .then(([diffs, comments, reviews, builds, patchsets]) => {
        const latest = patchsets.reduce<typeof patchsets[number] | undefined>((a, p) => (!a || p.number > a.number ? p : a), undefined)
        setLoaded(prev => new Map(prev).set(id, { diffs, comments, reviews, builds: latestBuildPerTool(builds), commitSha: latest?.commit_sha }))
      })
      .catch(e => {
        inflight.current.delete(id)
        setError(e?.message ?? `Could not load CL #${id}`)
      })
  }

  // Start at the first CL you haven't LGTM'd yet; ones you already approved
  // count as done, and so do your own when the repo requires another reviewer.
  useEffect(() => {
    let cancelled = false
    const repo = open[0]?.repo
    const requireOther = repo
      ? api.getRepo(repo).then(d => !!(d.require_other_lgtm || d.require_other_lgtm_global)).catch(() => false)
      : Promise.resolve(false)
    Promise.all([requireOther, Promise.all(open.map(cl => api.getReviews(cl.id).then(rs => [cl.id, rs] as const)))])
      .then(([other, entries]) => {
        if (cancelled) return
        const done = new Map<number, StepResult>()
        for (const [id, rs] of entries) {
          if (other && open.find(c => c.id === id)?.author === me) done.set(id, 'own')
          else if (rs.some(r => reviewerOf(r) === me && r.verdict === 'lgtm')) done.set(id, 'lgtm')
        }
        setResults(done)
        const first = open.findIndex(cl => !done.has(cl.id))
        if (first === -1) onFinished(done)
        else setIndex(first)
      })
      .catch(() => { if (!cancelled) setIndex(0) })
    return () => { cancelled = true }
    // cls is fixed for the dialog's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Load the current CL, and the next one in the background so moving on is instant.
  useEffect(() => {
    if (index === null) return
    if (open[index]) load(open[index].id)
    if (open[index + 1]) load(open[index + 1].id)
    scrollRef.current?.scrollTo({ top: 0 })
    setError('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [index])

  function advance(next: Map<number, StepResult>) {
    if (index === null) return
    // Next CL after this one you haven't voted on or skipped; past the end, the pass is over.
    const after = open.findIndex((cl, i) => i > index && !next.has(cl.id))
    if (after !== -1) { setIndex(after); return }
    onFinished(next)
  }

  async function vote(verdict: 'lgtm' | 'needs_work') {
    if (index === null || busy) return
    const cl = open[index]
    setBusy(verdict)
    setError('')
    try {
      await api.postReview(cl.id, verdict)
      const next = new Map(results).set(cl.id, verdict)
      setResults(next)
      advance(next)
    } catch (e: any) {
      setError(e?.message ?? 'Review failed')
    } finally {
      setBusy(null)
    }
  }

  function skip() {
    if (index === null || busy) return
    const id = open[index].id
    const next = new Map(results)
    if (!next.has(id)) next.set(id, 'skipped')
    setResults(next)
    advance(next)
  }

  // The CL before this one you can review (your own are passed over when they need another reviewer).
  let prevIndex = -1
  for (let i = (index ?? 0) - 1; i >= 0; i--) {
    if (results.get(open[i].id) !== 'own') { prevIndex = i; break }
  }

  function back() {
    if (prevIndex === -1 || busy) return
    setIndex(prevIndex)
  }

  async function comment(file: string, line: number, body: string) {
    if (index === null) return
    const id = open[index].id
    await api.postComment(id, { file, line, body })
    const comments = await api.getComments(id)
    setLoaded(prev => {
      const cur = prev.get(id)
      return cur ? new Map(prev).set(id, { ...cur, comments }) : prev
    })
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { if (!busy) onClose(); return }
      if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key === 'l') { e.preventDefault(); vote('lgtm') }
      else if (e.key === 'w') { e.preventDefault(); vote('needs_work') }
      else if (e.key === 's' || e.key === 'ArrowRight') { e.preventDefault(); skip() }
      else if (e.key === 'ArrowLeft') { e.preventDefault(); back() }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const cl = index === null ? undefined : open[index]
  const data = cl ? loaded.get(cl.id) : undefined
  const othersReviews = data?.reviews ?? []
  const settled = open.filter(c => results.has(c.id) && results.get(c.id) !== 'skipped' && results.get(c.id) !== 'own').length
  const reviewable = open.filter(c => results.get(c.id) !== 'own')
  const position = cl ? reviewable.findIndex(c => c.id === cl.id) + 1 : 0

  const body = (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 55,
        background: 'rgba(0,0,0,0.55)',
        backdropFilter: 'blur(8px)', WebkitBackdropFilter: 'blur(8px)',
        display: 'grid', placeItems: 'center', padding: '2rem',
        // Without a fixed track, one long unbreakable line (a CL title, a diff
        // line) widens the column past the viewport and the card centers off-screen.
        gridTemplateColumns: 'minmax(0, 1fr)',
      }}
      onClick={() => { if (!busy) onClose() }}
    >
      <div
        className="card"
        role="dialog"
        aria-label="Review stack"
        style={{ width: 'min(1100px, 100%)', height: '90vh', display: 'flex', flexDirection: 'column', padding: 0 }}
        onClick={e => e.stopPropagation()}
      >
        {/* header + progress */}
        <div style={{ padding: '0.85rem 1rem 0.7rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
            <span style={{ fontWeight: 700 }}>Review stack</span>
            <span style={{ fontSize: '0.8rem', color: 'var(--t3)' }}>
              {index === null ? 'checking…' : `${position} of ${reviewable.length} · ${settled} reviewed`}
            </span>
            <button className="btn btn-ghost btn-icon btn-sm" style={{ marginLeft: 'auto' }} onClick={onClose} disabled={!!busy} aria-label="Close">✕</button>
          </div>
          <div style={{ display: 'flex', gap: 3, marginTop: '0.6rem' }} aria-hidden>
            {open.map((c, i) => {
              const r = results.get(c.id)
              const bg = r === 'lgtm' ? 'var(--accent)' : r === 'needs_work' ? 'var(--red)' : r === 'own' ? 'var(--t3)' : 'var(--border)'
              return (
                <button
                  key={c.id}
                  title={`#${c.id} ${c.title}${r === 'own' ? ' (yours; needs another reviewer)' : ''}`}
                  disabled={r === 'own'}
                  onClick={() => !busy && setIndex(i)}
                  style={{
                    flex: 1, height: 5, padding: 0, border: 0, cursor: 'pointer', background: bg,
                    outline: i === index ? '1px solid var(--t1)' : 'none', outlineOffset: 1,
                  }}
                />
              )
            })}
          </div>
        </div>

        {/* the CL under review */}
        <div ref={scrollRef} style={{ flex: 1, overflow: 'auto', padding: '0.9rem 1rem' }}>
          {!cl || !data ? (
            <div style={{ display: 'grid', placeItems: 'center', height: '100%' }}><Spinner size="1.4em" /></div>
          ) : (
            <>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.5rem', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '0.8rem', color: 'var(--t3)' }}>#{cl.id}</span>
                <span style={{ fontWeight: 700, fontSize: '1rem' }}>{cl.title}</span>
                <a href={`#/cl/${cl.id}`} target="_blank" rel="noreferrer" style={{ fontSize: '0.78rem', marginLeft: 'auto' }}>Open full CL ↗</a>
              </div>
              <div className="cl-meta" style={{ margin: '0.3rem 0 0.6rem' }}>
                <span style={{ color: 'var(--accent)' }}>{cl.target_branch}</span>
                <span>·</span>
                <UserLink username={cl.author} />
                <span>·</span>
                <span>{humanTime(cl.updated_at)}</span>
              </div>
              {cl.description && (
                <p style={{ fontSize: '0.85rem', color: 'var(--t2)', whiteSpace: 'pre-wrap', margin: '0 0 0.75rem' }}>{cl.description}</p>
              )}

              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem 1rem', alignItems: 'center', marginBottom: '0.9rem', fontSize: '0.8rem' }}>
                {data.builds.length === 0
                  ? <span style={{ color: 'var(--t3)' }}>No builds yet.</span>
                  : data.builds.map(b => (
                      <span key={b.id} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem' }}>
                        <BuildBadge status={b.status} /> {b.tool_name}
                        <span style={{ color: 'var(--t3)' }}>{buildDuration(b)}</span>
                      </span>
                    ))}
                {othersReviews.length > 0 && <span style={{ color: 'var(--t3)' }}>·</span>}
                {othersReviews.map(r => (
                  <span key={r.id} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem' }}>
                    <VerdictBadge verdict={r.verdict} /> {reviewerOf(r) === me ? 'you' : reviewerOf(r)}
                  </span>
                ))}
              </div>

              {data.diffs.length === 0
                ? <p style={{ fontSize: '0.85rem', color: 'var(--t3)' }}>No file changes.</p>
                : <DiffViewer diffs={data.diffs} comments={data.comments} onComment={comment} repoName={cl.repo} commitSha={data.commitSha} />}
            </>
          )}
        </div>

        {/* actions */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap', padding: '0.75rem 1rem', borderTop: 'var(--hairline) solid var(--border)' }}>
          <button className="btn btn-ghost btn-sm" onClick={back} disabled={prevIndex === -1 || !!busy}>← Back</button>
          <span style={{ fontSize: '0.74rem', color: 'var(--t3)' }}>
            <kbd>L</kbd> LGTM · <kbd>W</kbd> needs work · <kbd>S</kbd> skip · <kbd>←</kbd> back
          </span>
          {error && <span style={{ fontSize: '0.8rem', color: 'var(--red)' }}>{error}</span>}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.5rem' }}>
            <button className="btn btn-ghost btn-sm" onClick={skip} disabled={!cl || !!busy}>Skip</button>
            <button className="btn btn-ghost btn-sm" style={{ color: 'var(--red)' }} onClick={() => vote('needs_work')} disabled={!cl || !!busy}>
              {busy === 'needs_work' ? <Spinner size="0.9em" /> : 'Needs work'}
            </button>
            <button className="btn btn-primary btn-sm" onClick={() => vote('lgtm')} disabled={!cl || !!busy}>
              {busy === 'lgtm'
                ? <Spinner size="0.9em" />
                : index !== null && open.every((c, i) => i <= index || results.has(c.id))
                  ? 'LGTM & finish'
                  : 'LGTM & next →'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )

  return createPortal(body, document.body)
}
