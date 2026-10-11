import { useEffect, useState } from 'react'
import { Spinner } from './Spinner'
import { ChipList } from './ChipList'
import { api, type OwnersFile } from '../lib/api'

const EMPTY: OwnersFile = { no_parent: false, owners: [], reviewers: [], per_file: [], required_lgtms: 1 }

// serialize turns the structured OWNERS state back into OWNERS file text in the
// exact subset the server parser understands (see internal/owners/parser.go).
function serialize(o: OwnersFile): string {
  const lines: string[] = []
  if (o.no_parent) lines.push('set noparent')
  if (o.required_lgtms > 1) lines.push(`required_lgtms: ${o.required_lgtms}`)
  for (const owner of o.owners) lines.push(`@${owner}`)
  for (const rule of o.per_file ?? []) {
    if (rule.glob && rule.owners.length) {
      lines.push(`per-file ${rule.glob} = ${rule.owners.map(u => '@' + u).join(', ')}`)
    }
  }
  return lines.join('\n') + (lines.length ? '\n' : '')
}

export function OwnersEditorModal({ repo, onClose }: { repo: string; onClose: () => void }) {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving]   = useState(false)
  const [error, setError]     = useState('')
  const [branch, setBranch]   = useState('')
  const [structured, setStructured] = useState<OwnersFile>(EMPTY)
  const [raw, setRaw]         = useState('')
  const [rawMode, setRawMode] = useState(false)
  const [usernames, setUsernames] = useState<string[]>([])

  useEffect(() => {
    api.listUsers().then(us => setUsernames(us.map(u => u.username))).catch(() => setUsernames([]))
  }, [])

  useEffect(() => {
    api.getOwners(repo).then(d => {
      setBranch(d.branch)
      setRaw(d.raw ?? '')
      const p = d.parsed ?? EMPTY
      // Go marshals nil slices as null — coalesce every array so the UI never
      // maps over null.
      setStructured({
        no_parent: p.no_parent ?? false,
        owners: p.owners ?? [],
        reviewers: p.reviewers ?? [],
        per_file: (p.per_file ?? []).map(r => ({ glob: r.glob ?? '', owners: r.owners ?? [] })),
        required_lgtms: p.required_lgtms || 1,
      })
    }).catch(e => setError(e?.message ?? 'Failed to load OWNERS')).finally(() => setLoading(false))
  }, [repo])

  function patch(p: Partial<OwnersFile>) { setStructured(s => ({ ...s, ...p })) }

  async function save() {
    setSaving(true); setError('')
    try {
      const text = rawMode ? raw : serialize(structured)
      await api.updateOwners(repo, text)
      onClose()
    } catch (e: any) {
      setError(e?.message ?? 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  const perFile = structured.per_file ?? []

  return (
    <div className="settings-panel open">
      <div className="settings-backdrop" onClick={onClose} />
      <div className="settings-drawer">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 700, fontSize: '1rem' }}>OWNERS — {repo}</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>
        <p className="form-hint">
          Access control lives in the repo's <code>OWNERS</code> file on
          <code> {branch || 'the default branch'}</code>. Saving commits it directly.
        </p>

        {loading ? <Spinner /> : (
          <>
            <div style={{ display: 'flex', gap: '0.4rem' }}>
              <button className={`btn btn-sm ${rawMode ? 'btn-ghost' : 'btn-primary'}`}
                onClick={() => setRawMode(false)}>Guided</button>
              <button className={`btn btn-sm ${rawMode ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => { setRaw(serialize(structured)); setRawMode(true) }}>Raw text</button>
            </div>

            {!rawMode ? (
              <>
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                  <input type="checkbox" checked={structured.no_parent}
                    onChange={e => patch({ no_parent: e.target.checked })} />
                  Ignore parent OWNERS (<code>set noparent</code>)
                </label>

                <div>
                  <div className="section-title">Required LGTMs</div>
                  <input className="input" type="number" min={1} value={structured.required_lgtms}
                    onChange={e => patch({ required_lgtms: Math.max(1, Number(e.target.value) || 1) })}
                    style={{ width: '6rem' }} />
                </div>

                <div>
                  <div className="section-title">Owners (required approvers)</div>
                  <ChipList items={structured.owners} placeholder="username" prefix="@"
                    suggestions={usernames} onChange={owners => patch({ owners })} />
                </div>

                <div>
                  <div className="section-title">Per-file rules</div>
                  {perFile.map((rule, i) => (
                    <div key={i} style={{ border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '0.6rem', marginBottom: '0.5rem' }}>
                      <div style={{ display: 'flex', gap: '0.4rem', marginBottom: '0.4rem' }}>
                        <input className="input" placeholder="glob e.g. *.go" value={rule.glob}
                          onChange={e => {
                            const next = [...perFile]; next[i] = { ...rule, glob: e.target.value }; patch({ per_file: next })
                          }} style={{ flex: 1 }} />
                        <button className="btn btn-ghost btn-sm" style={{ color: 'var(--red)' }}
                          onClick={() => patch({ per_file: perFile.filter((_, j) => j !== i) })}>Remove</button>
                      </div>
                      <ChipList items={rule.owners} placeholder="owner for this glob" prefix="@"
                        suggestions={usernames}
                        onChange={owners => { const next = [...perFile]; next[i] = { ...rule, owners }; patch({ per_file: next }) }} />
                    </div>
                  ))}
                  <button className="btn btn-ghost btn-sm"
                    onClick={() => patch({ per_file: [...perFile, { glob: '', owners: [] }] })}>+ Add rule</button>
                </div>
              </>
            ) : (
              <textarea className="input" rows={14} value={raw} onChange={e => setRaw(e.target.value)}
                style={{ fontFamily: 'monospace', fontSize: '0.8rem', resize: 'vertical' }} />
            )}

            {error && <p style={{ color: 'var(--red)', fontSize: '0.8rem' }}>{error}</p>}
            <button className="btn btn-primary" disabled={saving} onClick={save}>
              {saving ? <Spinner size="0.9em" /> : 'Save & commit OWNERS'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
