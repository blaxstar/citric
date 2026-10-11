import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Spinner } from './Spinner'
import { DiffViewer } from './DiffViewer'
import { api } from '../lib/api'
import { shortSHA } from '../lib/theme'
import type { PatchsetSummary, FileDiff } from '../lib/api'
import { Icon, Glyphs } from './icons'

const STATUS_MARK: Record<string, { sym: string; color: string }> = {
  added:    { sym: 'A', color: 'var(--green)' },
  deleted:  { sym: 'D', color: 'var(--red)' },
  renamed:  { sym: 'R', color: 'var(--yellow)' },
  modified: { sym: 'M', color: 'var(--accent)' },
}

export function PatchsetSummaryModal({ clID, ps, onClose }: {
  clID: number
  ps: number
  onClose: () => void
}) {
  const [summary, setSummary] = useState<PatchsetSummary | null>(null)
  const [error,   setError]   = useState('')
  const [diffMap, setDiffMap] = useState<Map<string, FileDiff> | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  useEffect(() => {
    api.getPatchsetSummary(clID, ps)
      .then(setSummary)
      .catch(e => setError(e?.message ?? 'Failed to load summary'))
  }, [clID, ps])

  // close on Escape
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function toggleFile(path: string) {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(path) ? next.delete(path) : next.add(path)
      return next
    })
    // lazily fetch the full diff once, keyed by file path
    if (!diffMap) {
      try {
        const diffs = await api.getDiff(clID, ps)
        const m = new Map<string, FileDiff>()
        for (const d of diffs) m.set(d.new_path || d.old_path, d)
        setDiffMap(m)
      } catch { /* leave diffMap null; expand will show a spinner */ }
    }
  }

  const body = (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 50,
        background: 'rgba(0,0,0,0.55)',
        display: 'grid', placeItems: 'center', padding: '2rem',
      }}
      onClick={onClose}
    >
      <div
        className="card"
        style={{ width: 'min(760px, 100%)', maxHeight: '85vh', display: 'flex', flexDirection: 'column', padding: 0 }}
        onClick={e => e.stopPropagation()}
      >
        {/* header */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', padding: '0.85rem 1rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
          <span style={{ fontWeight: 700 }}>Patchset {ps}</span>
          {summary && <code className="activity-sha">{shortSHA(summary.commit_sha)}</code>}
          {summary && (
            <span style={{ fontSize: '0.78rem' }}>
              <span style={{ color: 'var(--green)' }}>+{summary.total_additions}</span>{' '}
              <span style={{ color: 'var(--red)' }}>−{summary.total_deletions}</span>
            </span>
          )}
          {summary && summary.prev_patchset > 0 && (
            <span style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>
              vs PS{summary.prev_patchset}
            </span>
          )}
          <button className="btn btn-ghost btn-icon btn-sm" style={{ marginLeft: 'auto' }} onClick={onClose}>✕</button>
        </div>

        {/* body */}
        <div style={{ overflow: 'auto', padding: '0.5rem' }}>
          {!summary && !error && (
            <div style={{ display: 'flex', justifyContent: 'center', padding: '2rem' }}><Spinner size="1.2em" /></div>
          )}
          {error && <p style={{ color: 'var(--red)', padding: '1rem' }}>{error}</p>}

          {summary && summary.files.length === 0 && (
            <p style={{ color: 'var(--t3)', padding: '1rem', fontSize: '0.85rem' }}>No file changes.</p>
          )}

          {summary?.files.map(f => {
            const mark = STATUS_MARK[f.status] ?? STATUS_MARK.modified
            const isOpen = expanded.has(f.path)
            const fd = diffMap?.get(f.path)
            return (
              <div key={f.path} style={{ borderBottom: 'var(--hairline) solid var(--border)' }}>
                <div
                  onClick={() => toggleFile(f.path)}
                  style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', padding: '0.5rem 0.6rem', cursor: 'pointer' }}
                >
                  <span style={{ width: '1.1em', textAlign: 'center', fontFamily: 'monospace', fontWeight: 700, color: mark.color }}>
                    {mark.sym}
                  </span>
                  <span style={{ flex: 1, minWidth: 0, fontSize: '0.82rem', fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {f.old_path ? `${f.old_path} → ${f.path}` : f.path}
                  </span>
                  {f.changed_since_prev && (
                    <span
                      title={`changed since PS${summary.prev_patchset}`}
                      style={{ fontSize: '0.65rem', fontWeight: 700, color: 'var(--yellow)', border: 'var(--hairline) solid var(--yellow)', borderRadius: 0, padding: '0.05rem 0.35rem' }}
                    >
                      ● changed
                    </span>
                  )}
                  <span style={{ fontSize: '0.72rem', color: 'var(--green)' }}>+{f.additions}</span>
                  <span style={{ fontSize: '0.72rem', color: 'var(--red)' }}>−{f.deletions}</span>
                  <Icon glyph={isOpen ? Glyphs.chevronDown : Glyphs.chevronRight} size={13} style={{ color: 'var(--t3)', flexShrink: 0 }} />
                </div>
                {isOpen && (
                  <div style={{ padding: '0 0.3rem 0.5rem' }}>
                    {fd
                      ? <DiffViewer diffs={[fd]} comments={[]} />
                      : <div style={{ display: 'flex', justifyContent: 'center', padding: '1rem' }}><Spinner size="1em" /></div>}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )

  return createPortal(body, document.body)
}
