import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { navigate } from '../lib/router'
import { PageSpinner, Spinner } from '../components/Spinner'
import type { Repo } from '../lib/api'
import { Icon, Glyphs, BranchIcon } from '../components/icons'

function StarButton({ name }: { name: string }) {
  const [starred, setStarred] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.isStarred(name).then(r => setStarred(r.starred)).catch(() => setStarred(false))
  }, [name])

  async function toggle(e: React.MouseEvent) {
    e.stopPropagation()
    if (starred === null) return
    setBusy(true)
    try {
      if (starred) await api.unstarRepo(name)
      else await api.starRepo(name)
      setStarred(v => !v)
    } finally {
      setBusy(false)
    }
  }

  return (
    <button
      className="btn btn-ghost btn-sm btn-icon"
      onClick={toggle}
      disabled={busy || starred === null}
      title={starred ? 'Unstar' : 'Star'}
      style={{ fontSize: '1rem', color: starred ? 'var(--accent)' : 'var(--t3)', flexShrink: 0 }}
    >
      {busy
        ? <Spinner size="0.85em" />
        : <Icon glyph={starred ? Glyphs.starredOn : Glyphs.starred} size={15} />}
    </button>
  )
}

const PAGE_SIZES = [5, 10, 25, 0] // 0 = All

export function ReposPage() {
  const [repos, setRepos] = useState<Repo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pageSize, setPageSize] = useState(25)
  const [search, setSearch] = useState('')

  useEffect(() => {
    api.listRepos()
      .then(setRepos)
      .catch(e => setError(e?.message ?? 'Failed to load repositories'))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <PageSpinner />
  if (error) return <p style={{ color: 'var(--red)' }}>{error}</p>

  const q = search.trim().toLowerCase()
  const matched = q
    ? repos.filter(r =>
        r.name.toLowerCase().includes(q) ||
        (r.description ?? '').toLowerCase().includes(q))
    : repos
  const visible = pageSize === 0 ? matched : matched.slice(0, pageSize)

  return (
    <div>
      {/* Title and controls read as one header block. */}
      <div className="panel" style={{ marginBottom: '1rem' }}>
        <h1 className="page-title" style={{ margin: 0 }}>Repositories</h1>

        {/* Same row shape as the CL list: controls on their own line, left-packed. */}
        {repos.length > 0 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.6rem', flexWrap: 'wrap' }}>
            <input
              className="input"
              style={{ maxWidth: '14rem' }}
              placeholder="Search repositories…"
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.8rem', color: 'var(--t3)' }}>
              <span style={{ marginRight: '0.25rem' }}>Show</span>
              {PAGE_SIZES.map(n => (
                <button key={n} onClick={() => setPageSize(n)}
                  className="btn btn-ghost btn-sm"
                  style={{ minWidth: '2rem', color: pageSize === n ? 'var(--accent)' : undefined, fontWeight: pageSize === n ? 700 : undefined }}>
                  {n === 0 ? 'All' : n}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {repos.length > 0 && matched.length === 0 ? (
        <div className="panel" style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)' }}>
          No repositories match “{search.trim()}”.
        </div>
      ) : repos.length === 0 ? (
        <div className="panel" style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)' }}>
          No repositories yet. Create one in Admin.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {visible.map(repo => (
            <div key={repo.name} className="card cl-card"
              onClick={() => navigate(`/repo/${repo.name}`)}
              style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
              <BranchIcon size={15} style={{ color: 'var(--t3)', flexShrink: 0 }} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600, fontSize: '0.9rem' }}>{repo.name}</div>
                {repo.description ? (
                  <div style={{ fontSize: '0.78rem', color: 'var(--t2)', marginTop: '2px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {repo.description}
                  </div>
                ) : (
                  <div style={{ fontSize: '0.72rem', color: 'var(--t3)', fontFamily: 'monospace', marginTop: '2px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {repo.clone_url}
                  </div>
                )}
              </div>
              <StarButton name={repo.name} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
