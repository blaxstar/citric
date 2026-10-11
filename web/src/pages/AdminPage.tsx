import { useEffect, useRef, useState } from 'react'
import { PageSpinner, Spinner } from '../components/Spinner'
import { RepoSettingsModal } from '../components/RepoSettingsModal'
import { ServerSettingsModal } from '../components/ServerSettingsModal'
import { navigate } from '../lib/router'
import { api } from '../lib/api'
import type { User, Repo, BuildNode } from '../lib/api'
import { getServer, fetchAuthConfig } from '../lib/auth'
import { isAdmin, hasPerm, PERM_REPO_CREATE, PERM_REPO_MODIFY, PERM_SSO_SYNC, PERM_GALLERY_UPLOAD, PERM_GALLERY_REMOVE } from '../lib/auth'
import { ErrorAlert } from '../components/ErrorAlert'

export function AdminPage() {
  const [serverOpen, setServerOpen] = useState(false)
  const fullAdmin    = isAdmin()
  const showRepos    = fullAdmin || hasPerm(PERM_REPO_CREATE) || hasPerm(PERM_REPO_MODIFY)
  const showUsers    = fullAdmin || hasPerm(PERM_SSO_SYNC)
  const showBgs      = fullAdmin || hasPerm(PERM_GALLERY_UPLOAD) || hasPerm(PERM_GALLERY_REMOVE)
  const partial      = !fullAdmin
  return (
    <div>
      <div className="panel" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
        <h1 className="page-title" style={{ margin: 0 }}>Admin</h1>
        {fullAdmin && <button className="btn btn-ghost" onClick={() => setServerOpen(true)}>⚙ Server settings</button>}
      </div>
      <div style={partial
        ? { display: 'flex', flexWrap: 'wrap', gap: '1.5rem', alignItems: 'flex-start' }
        : { display: 'flex', flexDirection: 'column', gap: '2rem' }
      }>
        {showRepos && <div style={partial ? { flex: '1 1 min(100%, 480px)' } : {}}><ReposSection /></div>}
        {showUsers && <div style={partial ? { flex: '1 1 min(100%, 480px)' } : {}}><UsersSection /></div>}
        {showBgs   && <div style={partial ? { flex: '1 1 100%' } : {}}><BackgroundsSection /></div>}
        {/* Server-wide and unscoped, so full admins only — there is no
            narrower permission for identity mapping yet. */}
        {fullAdmin && <div style={partial ? { flex: '1 1 100%' } : {}}><MailmapSection /></div>}
        {fullAdmin && <NodesSection />}
      </div>
      {serverOpen && <ServerSettingsModal onClose={() => setServerOpen(false)} />}
    </div>
  )
}

// ── build nodes ────────────────────────────────────────────────────────────────

const NODE_ONLINE_MS = 90_000

function nodeOnline(n: BuildNode): boolean {
  return !!n.last_seen && Date.now() - new Date(n.last_seen).getTime() < NODE_ONLINE_MS
}

function NodesSection() {
  const [nodes, setNodes]   = useState<BuildNode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError]   = useState('')
  const [token, setToken]   = useState<{ token: string; expires_at: string } | null>(null)
  const [busy, setBusy]     = useState('')

  async function load() {
    try { setNodes(await api.listNodes()); setError('') }
    catch (e: any) { setError(e?.message ?? 'Failed to load nodes') }
    finally { setLoading(false) }
  }
  useEffect(() => {
    load()
    const t = setInterval(load, 10_000) // status and load change on their own
    return () => clearInterval(t)
  }, [])

  async function act(name: string, fn: (n: string) => Promise<void>) {
    setBusy(name)
    try { await fn(name); await load() }
    catch (e: any) { setError(e?.message ?? 'Action failed') }
    finally { setBusy('') }
  }

  async function newToken() {
    try { setToken(await api.createJoinToken()) }
    catch (e: any) { setError(e?.message ?? 'Could not create a join token') }
  }

  const server = getServer() || window.location.origin
  return (
    <div className="card" style={{ padding: '1rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div className="section-title" style={{ margin: 0 }}>Build nodes</div>
        <button className="btn btn-ghost btn-sm" onClick={newToken}>+ Add node</button>
      </div>
      <p className="form-hint" style={{ marginTop: '0.25rem' }}>
        Extra machines that run builds. Without any, this server runs every build itself.
        Tools choose a platform with <code>runs_on = ["windows"]</code> in <code>.citric.toml</code>.
      </p>
      {error && <ErrorAlert msg={error} style={{ marginTop: '0.5rem' }} onDismiss={() => setError('')} />}
      {token && (
        <div className="card" style={{ background: 'var(--s2)', padding: '0.75rem', marginTop: '0.5rem', fontSize: '0.82rem' }}>
          <div style={{ marginBottom: '0.4rem' }}>
            One-time join token, valid until {new Date(token.expires_at).toLocaleString()}. Run on the new machine:
          </div>
          <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: '0.75rem' }}>
{`citric node join --server ${server} --token ${token.token}
citric node run`}
          </pre>
          <div style={{ margin: '0.5rem 0 0.4rem' }}>or with Docker:</div>
          <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: '0.75rem' }}>
{`docker run -d --name citric-node -e CITRIC_NODE_SERVER=${server} \\
  -e CITRIC_NODE_JOIN_TOKEN=${token.token} \\
  -v citric-node:/var/lib/citric-node ghcr.io/blaxstar/citric:latest-full node run`}
          </pre>
          <button className="btn btn-ghost btn-sm" style={{ marginTop: '0.5rem' }} onClick={() => setToken(null)}>Done</button>
        </div>
      )}
      {loading && <PageSpinner />}
      {!loading && nodes.length === 0 && !token && (
        <p style={{ fontSize: '0.85rem', color: 'var(--t3)', marginTop: '0.5rem' }}>No nodes yet.</p>
      )}
      {nodes.length > 0 && (
        <table style={{ width: '100%', marginTop: '0.75rem', fontSize: '0.84rem', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ textAlign: 'left', color: 'var(--t3)' }}>
              <th style={{ padding: '0.3rem' }}>Node</th>
              <th style={{ padding: '0.3rem' }}>Status</th>
              <th style={{ padding: '0.3rem' }}>Platform</th>
              <th style={{ padding: '0.3rem' }}>Labels</th>
              <th style={{ padding: '0.3rem' }}>Jobs</th>
              <th style={{ padding: '0.3rem' }} />
            </tr>
          </thead>
          <tbody>
            {nodes.map(n => {
              const online = nodeOnline(n)
              const state = !online ? 'offline' : n.status
              const color = !online ? 'var(--t3)' : n.status === 'draining' ? 'var(--yellow)' : 'var(--green, #46c46e)'
              return (
                <tr key={n.name} style={{ borderTop: 'var(--hairline) solid var(--s2)' }}>
                  <td style={{ padding: '0.4rem 0.3rem', fontWeight: 600 }}>{n.name}</td>
                  <td style={{ padding: '0.4rem 0.3rem', color }}>{state}</td>
                  <td style={{ padding: '0.4rem 0.3rem' }}>{n.os ? `${n.os}/${n.arch}` : '—'}</td>
                  <td style={{ padding: '0.4rem 0.3rem', color: 'var(--t2)' }}>{n.labels.join(', ') || '—'}</td>
                  <td style={{ padding: '0.4rem 0.3rem' }}>{n.in_flight}/{n.max_concurrent}</td>
                  <td style={{ padding: '0.4rem 0.3rem', textAlign: 'right', whiteSpace: 'nowrap' }}>
                    {n.status === 'active'
                      ? <button className="btn btn-ghost btn-sm" disabled={busy === n.name} onClick={() => act(n.name, api.drainNode)}>Drain</button>
                      : <button className="btn btn-ghost btn-sm" disabled={busy === n.name} onClick={() => act(n.name, api.resumeNode)}>Resume</button>}
                    <button className="btn btn-ghost btn-sm" disabled={busy === n.name}
                      onClick={() => { if (confirm(`Remove node ${n.name}? Its credential stops working.`)) act(n.name, api.removeNode) }}>
                      Remove
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}

// ── identity mapping (git mailmap) ─────────────────────────────────────────────

const MAILMAP_PLACEHOLDER = `# One person, one identity. Map alternate addresses onto a canonical one:
#
#   Your Name <canonical@example.com> <other@example.com>
#
# Applies to every repository. Contributor lists, and any repo without its own
# .mailmap, pick this up immediately — no re-import or push needed.`

function MailmapSection() {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving,  setSaving]  = useState(false)
  const [error,   setError]   = useState('')
  const [saved,   setSaved]   = useState(false)

  useEffect(() => {
    api.getMailmap()
      .then(r => setContent(r.content))
      .catch(e => setError(e?.message ?? 'Failed to load identity mapping'))
      .finally(() => setLoading(false))
  }, [])

  async function save() {
    setSaving(true)
    setError('')
    try {
      await api.updateMailmap(content)
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
    } catch (e: any) {
      // The server rejects unparseable lines rather than writing them, because
      // git ignores bad lines silently — surface exactly which line was wrong.
      setError(e?.message ?? 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="card" style={{ padding: '1rem' }}>
      <div className="section-title">Identity mapping</div>
      <p style={{ fontSize: '0.8rem', color: 'var(--t3)', margin: '0 0 0.75rem' }}>
        Merges a person's alternate email addresses into one contributor. Uses
        git's <code>mailmap</code> format and applies to every repository.
      </p>

      {loading ? <PageSpinner /> : (
        <>
          <textarea
            value={content}
            onChange={e => { setContent(e.target.value); setSaved(false) }}
            spellCheck={false}
            rows={8}
            placeholder={MAILMAP_PLACEHOLDER}
            style={{
              width: '100%', resize: 'vertical', background: 'var(--s2)',
              border: 'var(--hairline) solid var(--border-input)', borderRadius: 'var(--r)',
              color: 'var(--t1)', padding: '0.6rem 0.8rem', fontSize: '0.8rem',
              fontFamily: 'monospace', lineHeight: 1.6, outline: 'none',
              boxSizing: 'border-box',
            }}
          />
          {error && <ErrorAlert msg={error} style={{ marginTop: '0.5rem' }} />}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginTop: '0.75rem' }}>
            <button className="btn btn-primary btn-sm" onClick={save} disabled={saving}>
              {saving ? <Spinner size="0.85em" /> : 'Save'}
            </button>
            {saved && <span style={{ fontSize: '0.8rem', color: 'var(--green)' }}>Saved</span>}
          </div>
        </>
      )}
    </div>
  )
}

// ── repos ──────────────────────────────────────────────────────────────────────

const ROW_HEIGHT_PX = 57 // approx height of one repo row (two text lines + padding + border)

function ReposSection() {
  const [repos,    setRepos]    = useState<Repo[]>([])
  const [loading,  setLoading]  = useState(true)
  const [error,    setError]    = useState('')
  const [newName,  setNewName]  = useState('')
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState('')
  const [settingsRepo, setSettingsRepo] = useState<string | null>(null)
  const [visibleRows, setVisibleRows] = useState(3)

  async function load() {
    try {
      setRepos(await api.listRepos())
    } catch (e: any) {
      setError(e?.message ?? 'Failed to load repos')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function createRepo(e: React.FormEvent) {
    e.preventDefault()
    if (!newName.trim()) return
    setCreating(true)
    setCreateErr('')
    try {
      await api.createRepo(newName.trim())
      setNewName('')
      await load()
    } catch (err: any) {
      setCreateErr(err?.message ?? 'Failed to create repo')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem', alignItems: 'start' }}>
      <div className="card" style={{ padding: '1rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.25rem' }}>
          <div className="section-title" style={{ margin: 0 }}>Repositories</div>
          <select
            className="btn btn-ghost btn-sm"
            value={visibleRows}
            onChange={e => setVisibleRows(Number(e.target.value))}
            style={{ cursor: 'pointer' }}
          >
            <option value={2}>2 visible</option>
            <option value={3}>3 visible</option>
            <option value={4}>4 visible</option>
          </select>
        </div>
        {loading && <PageSpinner />}
        {error && <ErrorAlert msg={error} style={{ marginTop: '0.5rem' }} />}
        {!loading && repos.length === 0 && (
          <p style={{ fontSize: '0.85rem', color: 'var(--t3)' }}>No repositories yet.</p>
        )}
        <div style={{ maxHeight: `${visibleRows * ROW_HEIGHT_PX}px`, overflowY: 'auto', marginTop: '0.25rem' }}>
          {!loading && repos.map(repo => (
            <div key={repo.name} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 0', borderBottom: 'var(--hairline) solid var(--border)' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600, fontSize: '0.85rem' }}>{repo.name}</div>
                <div style={{ fontSize: '0.72rem', color: 'var(--t3)', fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {repo.clone_url}
                </div>
              </div>
              <button className="btn btn-ghost btn-sm" style={{ flexShrink: 0 }}
                onClick={() => setSettingsRepo(repo.name)} title="Repo settings">
                ⚙ Settings
              </button>
            </div>
          ))}
        </div>
      </div>

      <div className="card" style={{ padding: '1rem' }}>
        <div className="section-title">Create repository</div>
        <form onSubmit={createRepo} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          <div>
            <label className="form-label">Name</label>
            <input
              className="input"
              placeholder="my-project"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              pattern="[a-zA-Z0-9_.\-]+"
              title="Letters, digits, hyphens, underscores, and dots only"
              required
              autoComplete="off"
            />
            <p className="form-hint">
              Push with: <code>git push origin HEAD:refs/for/main</code>
            </p>
          </div>
          {createErr && <ErrorAlert msg={createErr} />}
          <button type="submit" className="btn btn-primary" disabled={creating || !newName.trim()}>
            {creating ? <Spinner size="0.9em" /> : 'Create repo'}
          </button>
        </form>
      </div>

      {settingsRepo && (
        <RepoSettingsModal
          repo={settingsRepo}
          onClose={() => setSettingsRepo(null)}
          onDeleted={() => { setSettingsRepo(null); load() }}
        />
      )}
    </div>
  )
}

// ── users ──────────────────────────────────────────────────────────────────────

function roleColor(role: string): string {
  switch (role) {
    case 'admin':     return 'var(--accent)'
    case 'submitter': return 'var(--green, #46c46e)'
    default:          return 'var(--t3)'
  }
}

function UsersSection() {
  const [users,    setUsers]   = useState<User[]>([])
  const [loading,  setLoading] = useState(true)
  const [error,    setError]   = useState('')
  const [syncing,  setSyncing] = useState(false)
  const [syncMsg,  setSyncMsg] = useState('')
  const [syncErr,  setSyncErr] = useState(false) // true = sync failed, false = sync success
  const [userSync, setUserSync] = useState(false) // Keycloak sync configured

  async function load() {
    try {
      setUsers(await api.listUsers())
    } catch (e: any) {
      setError(e?.message ?? 'Failed to load users')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    fetchAuthConfig().then(c => setUserSync(!!c.user_sync))
  }, [])

  async function sync() {
    setSyncing(true); setSyncMsg(''); setSyncErr(false)
    try {
      await api.syncUsers()
      setSyncMsg('Synced'); setSyncErr(false)
      await load()
      setTimeout(() => setSyncMsg(''), 2500)
    } catch (e: any) {
      setSyncMsg(e?.message ?? 'Sync failed'); setSyncErr(true)
      // leave errors on screen — they need reading, not a 2.5s flash
    } finally {
      setSyncing(false)
    }
  }

  return (
    <div className="card" style={{ padding: '1rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div className="section-title" style={{ margin: 0 }}>Users</div>
        {userSync && (
          <button className="btn btn-ghost btn-sm" style={{ flexShrink: 0 }} onClick={sync} disabled={syncing}>
            {syncing ? <Spinner size="0.9em" /> : '⟳ Sync from Keycloak'}
          </button>
        )}
      </div>
      <p className="form-hint" style={{ marginTop: '0.25rem' }}>
        {userSync
          ? 'Users and roles come from Keycloak — admins are members of the configured admin role. Edit membership in Keycloak, then sync.'
          : 'Users appear when they first sign in. Admins hold the admin role in your identity provider, or were made admin on the server with citric admin create.'}
      </p>
      {loading && <PageSpinner />}
      {error && <ErrorAlert msg={error} style={{ marginTop: '0.5rem' }} />}
      {syncMsg && <ErrorAlert msg={syncMsg} variant={syncErr ? 'error' : 'success'} onDismiss={() => setSyncMsg('')} style={{ marginTop: '0.5rem' }} />}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '0.5rem', marginTop: '0.5rem' }}>
        {!loading && users.map(u => (
          <button key={u.username}
            onClick={() => navigate(`/user/${u.username}`)}
            className="card"
            style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 0.6rem', textAlign: 'left', cursor: 'pointer', background: 'var(--s2)' }}>
            <div className="avatar" style={{ width: '1.9rem', height: '1.9rem', fontSize: '0.65rem' }}>
              {u.username.slice(0, 2).toUpperCase()}
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 600, fontSize: '0.88rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{u.username}</div>
              <div style={{ fontSize: '0.82rem', color: roleColor(u.role), fontWeight: 600 }}>{u.role}</div>
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}

// ── backgrounds ─────────────────────────────────────────────────────────────────

async function resizeIfNeeded(file: File, maxPx = 2560): Promise<{ blob: Blob; ext: string }> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    const url = URL.createObjectURL(file)
    img.onload = () => {
      URL.revokeObjectURL(url)
      const scale = Math.min(1, maxPx / Math.max(img.width, img.height))
      const origExt = (file.name.match(/\.[^.]+$/) ?? ['.jpg'])[0]
      if (scale === 1) { resolve({ blob: file, ext: origExt }); return }
      const canvas = document.createElement('canvas')
      canvas.width = Math.round(img.width * scale)
      canvas.height = Math.round(img.height * scale)
      canvas.getContext('2d')!.drawImage(img, 0, 0, canvas.width, canvas.height)
      canvas.toBlob(b => b ? resolve({ blob: b, ext: '.jpg' }) : reject(new Error('resize failed')), 'image/jpeg', 0.92)
    }
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('could not decode image')) }
    img.src = url
  })
}

function BackgroundsSection() {
  const [urls, setUrls]   = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  async function load() {
    try { setUrls(await api.listBackgrounds()) }
    catch (e: any) { setError(e?.message ?? 'Failed to load') }
    finally { setLoading(false) }
  }
  useEffect(() => { load() }, [])

  async function onUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    if (!f) return
    setUploading(true); setError('')
    try {
      const { blob, ext } = await resizeIfNeeded(f)
      await api.uploadBackground(blob, f.name.replace(/\.[^.]+$/, '') + ext)
      await load()
    } catch (err: any) {
      setError(err?.message ?? 'Upload failed')
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  async function del(url: string) {
    const name = url.split('/').pop() ?? ''
    if (!confirm(`Remove background "${name}" from the gallery?`)) return
    try { await api.deleteBackground(name); setUrls(u => u.filter(x => x !== url)) }
    catch (e: any) { alert(e?.message ?? 'Delete failed') }
  }

  return (
    <div className="card" style={{ padding: '1rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div className="section-title" style={{ margin: 0 }}>Background gallery</div>
        <label className="btn btn-ghost btn-sm" style={{ cursor: uploading ? 'wait' : 'pointer' }}>
          <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp,image/gif,image/avif"
            style={{ display: 'none' }} onChange={onUpload} disabled={uploading} />
          {uploading ? 'Uploading…' : '↑ Upload image'}
        </label>
      </div>
      <p className="form-hint" style={{ marginTop: '0.25rem' }}>
        Curated images everyone can choose from in their own Appearance settings.
        Each person's background choice is theirs alone.
      </p>
      {error && <ErrorAlert msg={error} style={{ marginTop: '0.5rem' }} />}
      {loading ? <PageSpinner /> : urls.length === 0 ? (
        <p style={{ fontSize: '0.85rem', color: 'var(--t3)' }}>No background images yet.</p>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))', gap: '0.6rem', marginTop: '0.5rem' }}>
          {urls.map(url => (
            <div key={url} style={{ position: 'relative', borderRadius: 'var(--r)', overflow: 'hidden', border: 'var(--hairline) solid var(--border)', aspectRatio: '16 / 10', backgroundImage: `url(${JSON.stringify(url)})`, backgroundSize: 'cover', backgroundPosition: 'center' }}>
              <button className="btn btn-sm"
                style={{ position: 'absolute', top: '0.3rem', right: '0.3rem', background: 'rgba(0,0,0,0.6)', color: '#fff', padding: '0.1rem 0.4rem' }}
                onClick={() => del(url)} title="Remove">✕</button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
