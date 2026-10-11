import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { navigate } from '../lib/router'
import { PageSpinner } from '../components/Spinner'
import { getUsername } from '../lib/auth'
import type { Repo } from '../lib/api'
import { Icon, Glyphs, BranchIcon } from '../components/icons'

export function StarredPage() {
  const [repos, setRepos] = useState<Repo[]>([])
  const [loading, setLoading] = useState(true)
  const username = getUsername()

  useEffect(() => {
    api.getUserStarred(username)
      .then(setRepos)
      .finally(() => setLoading(false))
  }, [username])

  if (loading) return <PageSpinner />

  return (
    <div>
      <div className="panel" style={{ marginBottom: '1.25rem' }}>
        <h1 className="page-title" style={{ margin: 0 }}>Starred</h1>
      </div>

      {repos.length === 0 ? (
        <div className="panel" style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)' }}>
          <Icon glyph={Glyphs.starred} size={34} style={{ opacity: 0.5, marginBottom: '0.5rem' }} />
          <div style={{ fontSize: '0.9rem', marginBottom: '0.25rem' }}>No starred repositories</div>
          <div style={{ fontSize: '0.8rem' }}>Star repos from the Repositories page to find them here.</div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {repos.map(repo => (
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
              <Icon glyph={Glyphs.chevronRight} size={14} style={{ color: 'var(--t3)', flexShrink: 0 }} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
