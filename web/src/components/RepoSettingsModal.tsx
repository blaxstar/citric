import { useEffect, useState } from 'react'
import { Spinner } from './Spinner'
import { OwnersEditorModal } from './OwnersEditorModal'
import { ChipList } from './ChipList'
import { api, type RepoDetail } from '../lib/api'
import { Icon, Glyphs } from './icons'

export function RepoSettingsModal({ repo, onClose, onDeleted }: {
  repo: string; onClose: () => void; onDeleted: () => void
}) {
  const [detail, setDetail]   = useState<RepoDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving]   = useState(false)
  const [error, setError]     = useState('')
  const [desc, setDesc]       = useState('')
  const [branch, setBranch]   = useState('')
  const [copied, setCopied]   = useState(false)
  const [ownersOpen, setOwnersOpen] = useState(false)

  // visibility / access control
  const [restricted, setRestricted] = useState(false)
  const [aclUsers, setAclUsers]     = useState<string[]>([])
  const [aclRoles, setAclRoles]     = useState<string[]>([])
  const [allUsers, setAllUsers]     = useState<string[]>([])
  const [allRoles, setAllRoles]     = useState<string[]>([])
  const [savingAccess, setSavingAccess] = useState(false)
  const [accessSaved, setAccessSaved]   = useState(false)

  useEffect(() => {
    api.getRepo(repo).then(d => {
      setDetail(d); setDesc(d.description); setBranch(d.default_branch)
    }).catch(e => setError(e?.message ?? 'Failed to load repo')).finally(() => setLoading(false))
    api.getRepoAccess(repo).then(a => {
      setRestricted(a.restricted); setAclUsers(a.users ?? []); setAclRoles(a.roles ?? [])
    }).catch(() => {})
    api.listUsers().then(us => setAllUsers(us.map(u => u.username))).catch(() => {})
    api.listRealmRoles().then(setAllRoles).catch(() => setAllRoles([]))
  }, [repo])

  async function saveAccess() {
    setSavingAccess(true); setError(''); setAccessSaved(false)
    try {
      await api.updateRepoAccess(repo, { restricted, users: aclUsers, roles: aclRoles })
      setAccessSaved(true); setTimeout(() => setAccessSaved(false), 1500)
    } catch (e: any) {
      setError(e?.message ?? 'Save failed')
    } finally {
      setSavingAccess(false)
    }
  }

  async function save() {
    setSaving(true); setError('')
    try {
      const d = await api.updateRepo(repo, { description: desc, default_branch: branch })
      setDetail(d)
    } catch (e: any) {
      setError(e?.message ?? 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  async function copyClone() {
    if (!detail) return
    try {
      await navigator.clipboard.writeText(detail.clone_url)
      setCopied(true); setTimeout(() => setCopied(false), 1500)
    } catch { /* clipboard may be unavailable */ }
  }

  async function del() {
    if (!confirm(`Delete repo "${repo}"? This permanently removes all history and cannot be undone.`)) return
    try {
      await api.deleteRepo(repo)
      onDeleted()
    } catch (e: any) {
      alert(e?.message ?? 'Delete failed')
    }
  }

  return (
    <>
      <div className="settings-panel open">
        <div className="settings-backdrop" onClick={onClose} />
        <div className="settings-drawer">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontWeight: 700, fontSize: '1rem' }}>Repository settings</span>
            <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
          </div>

          {loading ? <Spinner /> : detail && (
            <>
              <div>
                <div className="section-title">Clone URL</div>
                <div style={{ display: 'flex', gap: '0.4rem' }}>
                  <input className="input" readOnly value={detail.clone_url}
                    style={{ flex: 1, fontFamily: 'monospace', fontSize: '0.75rem' }} />
                  <button className="btn btn-ghost btn-sm" onClick={copyClone}>{copied ? <Icon glyph={Glyphs.pass} size={14} /> : 'Copy'}</button>
                </div>
              </div>

              <div>
                <div className="section-title">Description</div>
                <input className="input" placeholder="What is this repo for?" value={desc}
                  onChange={e => setDesc(e.target.value)} />
              </div>

              <div>
                <div className="section-title">Default branch</div>
                <input className="input" value={branch} onChange={e => setBranch(e.target.value)}
                  style={{ width: '12rem' }} />
              </div>

              {error && <p style={{ color: 'var(--red)', fontSize: '0.8rem' }}>{error}</p>}
              <button className="btn btn-primary" disabled={saving} onClick={save}>
                {saving ? <Spinner size="0.9em" /> : 'Save settings'}
              </button>

              <div style={{ borderTop: 'var(--hairline) solid var(--border)', paddingTop: '0.75rem' }}>
                <div className="section-title">Who can see this repo</div>
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
                  <input type="checkbox" checked={restricted} onChange={e => setRestricted(e.target.checked)} />
                  Restrict access (hide from everyone except admins + the lists below)
                </label>
                {!restricted ? (
                  <p className="form-hint">Open — visible to all signed-in users, and clonable by anyone with the URL.</p>
                ) : (
                  <>
                    <div style={{ marginBottom: '0.6rem' }}>
                      <div className="section-title">Teams (roles and groups)</div>
                      <ChipList items={aclRoles} placeholder="role or group, e.g. team-infra"
                        suggestions={allRoles} onChange={setAclRoles} />
                      <p className="form-hint" style={{ marginTop: '0.3rem' }}>
                        Roles and groups from your identity provider. Suggestions list the ones seen at sign-in so far; you can type any name.
                      </p>
                    </div>
                    <div style={{ marginBottom: '0.6rem' }}>
                      <div className="section-title">People</div>
                      <ChipList items={aclUsers} placeholder="username" prefix="@"
                        suggestions={allUsers} onChange={setAclUsers} />
                    </div>
                    <p className="form-hint">Admins always have access. Restricted repos require a signed-in, allowed user to clone.</p>
                  </>
                )}
                <button className="btn btn-primary btn-sm" style={{ marginTop: '0.4rem' }} disabled={savingAccess} onClick={saveAccess}>
                  {savingAccess ? <Spinner size="0.9em" /> : accessSaved ? '✓ Saved' : 'Save visibility'}
                </button>
              </div>

              <div>
                <div className="section-title">Review owners (OWNERS)</div>
                <button className="btn btn-ghost" style={{ width: '100%' }} onClick={() => setOwnersOpen(true)}>
                  <Icon glyph={Glyphs.edit} size={13} /> Edit OWNERS
                </button>
              </div>

              <div style={{ marginTop: '0.5rem', borderTop: 'var(--hairline) solid var(--border)', paddingTop: '0.75rem' }}>
                <div className="section-title" style={{ color: 'var(--red)' }}>Danger zone</div>
                <button className="btn btn-ghost" style={{ width: '100%', color: 'var(--red)' }} onClick={del}>
                  Delete repository
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      {ownersOpen && <OwnersEditorModal repo={repo} onClose={() => setOwnersOpen(false)} />}
    </>
  )
}
