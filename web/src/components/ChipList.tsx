import { useRef, useState } from 'react'
import { moveFocus } from '../lib/a11y'

// ChipList edits a list of strings as removable chips with an add field and a
// keyboard-accessible autocomplete dropdown sourced from `suggestions`.
// Used for OWNERS usernames and repo access principals (users / roles).
export function ChipList({ items, onChange, placeholder, suggestions, prefix = '' }: {
  items: string[]
  onChange: (next: string[]) => void
  placeholder: string
  suggestions: string[]
  prefix?: string // shown before each chip (e.g. "@" for usernames)
}) {
  const list = items ?? []
  const [draft, setDraft] = useState('')
  const [open, setOpen] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)

  function add(value?: string) {
    const v = (value ?? draft).trim().replace(/^@/, '')
    if (v && !list.includes(v)) onChange([...list, v])
    setDraft('')
    setOpen(false)
  }

  const q = draft.trim().toLowerCase()
  const matches = (suggestions ?? [])
    .filter(u => !list.includes(u) && (q === '' || u.toLowerCase().includes(q)))
    .slice(0, 6)

  // Close only when focus leaves the whole input+dropdown group, so Tab can move
  // into the suggestion options without dismissing them.
  function onGroupBlur(e: React.FocusEvent<HTMLDivElement>) {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) setOpen(false)
  }

  return (
    <div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.35rem', marginBottom: '0.4rem' }}>
        {list.map(u => (
          <span key={u} style={{
            display: 'inline-flex', alignItems: 'center', gap: '0.35rem',
            padding: '0.15rem 0.5rem', borderRadius: 'var(--r-sm)',
            background: 'var(--s2)', fontSize: '0.78rem',
          }}>
            {prefix}{u}
            <button className="btn btn-ghost btn-sm" style={{ padding: 0, color: 'var(--red)' }}
              onClick={() => onChange(list.filter(x => x !== u))} aria-label={`Remove ${u}`} title="Remove">✕</button>
          </span>
        ))}
        {list.length === 0 && <span style={{ fontSize: '0.78rem', color: 'var(--t3)' }}>none</span>}
      </div>
      <div style={{ display: 'flex', gap: '0.4rem' }}>
        <div style={{ position: 'relative', flex: 1 }} ref={boxRef} onBlur={onGroupBlur}>
          <input className="input" placeholder={placeholder} value={draft}
            role="combobox" aria-expanded={open && matches.length > 0} aria-autocomplete="list"
            onChange={e => { setDraft(e.target.value); setOpen(true) }}
            onFocus={() => setOpen(true)}
            onKeyDown={e => {
              if (e.key === 'Enter') { e.preventDefault(); add() }
              else if (e.key === 'Escape') { setOpen(false) }
              else if (e.key === 'ArrowDown') { e.preventDefault(); setOpen(true); moveFocus(boxRef.current, 1) }
              else if (e.key === 'ArrowUp')   { e.preventDefault(); moveFocus(boxRef.current, -1) }
            }}
            style={{ width: '100%' }} />
          {open && matches.length > 0 && (
            <div className="user-suggestions" role="listbox">
              {matches.map(u => (
                <button key={u} type="button" role="option" className="user-suggestion-item"
                  onClick={() => add(u)}
                  onKeyDown={e => {
                    if (e.key === 'Escape') { setOpen(false) }
                    else if (e.key === 'ArrowDown') { e.preventDefault(); moveFocus(boxRef.current, 1) }
                    else if (e.key === 'ArrowUp')   { e.preventDefault(); moveFocus(boxRef.current, -1) }
                  }}>
                  <div className="avatar" style={{ width: '1.25rem', height: '1.25rem', fontSize: '0.55rem', flexShrink: 0 }}>
                    {u.slice(0, 2).toUpperCase()}
                  </div>
                  <span style={{ fontSize: '0.82rem' }}>{u}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        <button className="btn btn-ghost btn-sm" onClick={() => add()}>Add</button>
      </div>
    </div>
  )
}
