import { useRef, useState } from 'react'
import { VerdictBadge } from './Badge'
import { Spinner } from './Spinner'
import { UserLink } from './UserLink'
import { moveFocus } from '../lib/a11y'
import type { Review, User } from '../lib/api'

interface ReviewPanelProps {
  clID: number
  patchsetID: number
  reviews: Review[]
  currentUser: string
  users?: User[]
  onReview?: (verdict: string) => Promise<void>
  onAssign?: (username: string) => Promise<void>
  onSubmit?: () => Promise<void>
  onAbandon?: () => Promise<void>
  canSubmit?: boolean
}

export function ReviewPanel({
  reviews,
  currentUser,
  users,
  onReview,
  onAssign,
  onSubmit,
  onAbandon,
  canSubmit,
}: ReviewPanelProps) {
  const [assigning, setAssigning] = useState(false)
  const [assignee, setAssignee] = useState('')
  const [showSuggestions, setShowSuggestions] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState('')
  const assignBoxRef = useRef<HTMLDivElement>(null)

  const myReview = reviews.find(r => (r.author ?? r.reviewer) === currentUser)

  async function act(action: string, fn: () => Promise<void>) {
    setBusy(action)
    setError('')
    try {
      await fn()
    } catch (e: any) {
      setError(e?.message ?? 'Request failed')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      {/* reviewers list */}
      <div>
        <div className="section-title">Reviewers</div>
        {reviews.length === 0 && (
          <p style={{ fontSize: '0.85rem', color: 'var(--t3)' }}>No reviews yet.</p>
        )}
        {reviews.map(r => {
          const name = r.author ?? r.reviewer
          return (
            <div key={r.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.35rem 0' }}>
              <UserLink username={name} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flex: 1 }}>
                <div className="avatar" style={{ width: '1.5rem', height: '1.5rem', fontSize: '0.6rem' }}>
                  {name.slice(0, 2).toUpperCase()}
                </div>
                <span style={{ fontSize: '0.85rem', fontWeight: 600 }}>{name}</span>
              </UserLink>
              <VerdictBadge verdict={r.verdict} />
            </div>
          )
        })}
      </div>

      {/* my vote — only when CL is open */}
      {onReview && (
        <div>
          <div className="section-title">Your vote</div>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button
              className={`btn btn-sm${myReview?.verdict === 'lgtm' ? ' btn-primary' : ' btn-ghost'}`}
              style={{ flex: 1 }}
              disabled={!!busy}
              onClick={() => act('lgtm', () => onReview('lgtm'))}
            >
              {busy === 'lgtm' ? <Spinner size="0.9em" /> : '✓ LGTM'}
            </button>
            <button
              className={`btn btn-sm${myReview?.verdict === 'needs_work' ? ' btn-danger' : ' btn-ghost'}`}
              style={{ flex: 1 }}
              disabled={!!busy}
              onClick={() => act('needs_work', () => onReview('needs_work'))}
            >
              {busy === 'needs_work' ? <Spinner size="0.9em" /> : '↩ Needs work'}
            </button>
          </div>
        </div>
      )}

      {/* assign reviewer */}
      {onAssign && (
        <div>
          <div className="section-title">Add reviewer</div>
          {!assigning ? (
            <button className="btn btn-ghost btn-sm" onClick={() => setAssigning(true)}>
              + Assign reviewer
            </button>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <div
                style={{ position: 'relative' }}
                ref={assignBoxRef}
                onBlur={e => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setShowSuggestions(false) }}
              >
                <input
                  className="input"
                  placeholder="Username"
                  value={assignee}
                  role="combobox"
                  aria-autocomplete="list"
                  aria-expanded={showSuggestions}
                  onChange={e => { setAssignee(e.target.value); setShowSuggestions(true) }}
                  onFocus={() => setShowSuggestions(true)}
                  autoFocus
                  onKeyDown={e => {
                    if (e.key === 'Enter' && assignee.trim())
                      act('assign', () => onAssign(assignee).then(() => { setAssigning(false); setAssignee('') }))
                    else if (e.key === 'Escape') { setAssigning(false); setAssignee('') }
                    else if (e.key === 'ArrowDown') { e.preventDefault(); setShowSuggestions(true); moveFocus(assignBoxRef.current, 1) }
                    else if (e.key === 'ArrowUp')   { e.preventDefault(); moveFocus(assignBoxRef.current, -1) }
                  }}
                  style={{ width: '100%' }}
                />
                {showSuggestions && users && (() => {
                  const q = assignee.trim().toLowerCase()
                  const matches = users
                    .filter(u => q === '' || u.username.toLowerCase().includes(q))
                    .slice(0, 6)
                  if (!matches.length) return null
                  return (
                    <div className="user-suggestions" role="listbox">
                      {matches.map(u => (
                        <button
                          key={u.username}
                          type="button"
                          role="option"
                          className="user-suggestion-item"
                          onClick={() => { setAssignee(u.username); setShowSuggestions(false) }}
                          onKeyDown={e => {
                            if (e.key === 'Escape') { setShowSuggestions(false) }
                            else if (e.key === 'ArrowDown') { e.preventDefault(); moveFocus(assignBoxRef.current, 1) }
                            else if (e.key === 'ArrowUp')   { e.preventDefault(); moveFocus(assignBoxRef.current, -1) }
                          }}
                        >
                          <div className="avatar" style={{ width: '1.25rem', height: '1.25rem', fontSize: '0.55rem', flexShrink: 0 }}>
                            {u.username.slice(0, 2).toUpperCase()}
                          </div>
                          <span style={{ fontSize: '0.82rem' }}>{u.username}</span>
                        </button>
                      ))}
                    </div>
                  )
                })()}
              </div>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <button
                  className="btn btn-primary btn-sm"
                  style={{ flex: 1 }}
                  disabled={!assignee.trim() || !!busy}
                  onClick={() => act('assign', () => onAssign(assignee).then(() => { setAssigning(false); setAssignee('') }))}
                >
                  {busy === 'assign' ? <Spinner size="0.9em" /> : 'Add'}
                </button>
                <button className="btn btn-ghost btn-sm" onClick={() => { setAssigning(false); setAssignee('') }}>✕</button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* submit / abandon */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginTop: 'auto' }}>
        {canSubmit && onSubmit && (
          <button
            className="btn btn-primary"
            disabled={!!busy}
            onClick={() => act('submit', onSubmit)}
          >
            {busy === 'submit' ? <Spinner size="0.9em" /> : '⤴ Submit CL'}
          </button>
        )}
        {onAbandon && (
          <button
            className="btn btn-ghost"
            style={{ color: 'var(--red)' }}
            disabled={!!busy}
            onClick={() => {
              if (!confirm('Abandon this CL? This cannot be undone.')) return
              act('abandon', onAbandon)
            }}
          >
            {busy === 'abandon' ? <Spinner size="0.9em" /> : 'Abandon'}
          </button>
        )}
        {error && <p style={{ color: 'var(--red)', fontSize: '0.8rem' }}>{error}</p>}
      </div>
    </div>
  )
}
