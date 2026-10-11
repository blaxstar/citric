import { useEffect, useState } from 'react'
import { PageSpinner, Spinner } from '../components/Spinner'
import { CLStatusBadge } from '../components/Badge'
import { navigate } from '../lib/router'
import { humanTime } from '../lib/theme'
import { api, type ProfileActivity, type ProfileReview } from '../lib/api'
import { getUsername } from '../lib/auth'

type Tab = 'cls' | 'comments' | 'reviews'
type FollowModal = 'followers' | 'following' | null

function roleColor(role: string): string {
  switch (role) {
    case 'admin':     return 'var(--accent)'
    case 'submitter': return 'var(--green, #46c46e)'
    default:          return 'var(--t3)'
  }
}

function UserListModal({ title, users, onClose }: { title: string; users: string[]; onClose: () => void }) {
  return (
    <div
      style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.55)', zIndex: 100, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
      onClick={onClose}
    >
      <div
        style={{ background: 'var(--s1)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', width: '320px', maxHeight: '480px', display: 'flex', flexDirection: 'column', boxShadow: '0 8px 32px rgba(0,0,0,0.5)' }}
        onClick={e => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.875rem 1rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
          <span style={{ fontWeight: 600, fontSize: '0.9rem' }}>{title}</span>
          <button className="btn btn-ghost btn-sm" style={{ padding: '2px 6px' }} onClick={onClose}>✕</button>
        </div>
        <div style={{ overflowY: 'auto', padding: '0.5rem' }}>
          {users.length === 0 ? (
            <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--t3)', fontSize: '0.85rem' }}>Nobody here yet.</div>
          ) : (
            users.map(u => (
              <button key={u}
                style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', width: '100%', padding: '0.6rem 0.75rem', background: 'none', border: 'none', cursor: 'pointer', borderRadius: 'var(--r)', textAlign: 'left' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--s3)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'none')}
                onClick={() => { onClose(); navigate(`/user/${u}`) }}
              >
                <div className="avatar" style={{ width: '32px', height: '32px', fontSize: '0.75rem', borderRadius: 0, flexShrink: 0 }}>
                  {u.slice(0, 2).toUpperCase()}
                </div>
                <span style={{ fontSize: '0.88rem', fontWeight: 500 }}>{u}</span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

function FollowStats({ username, isSelf }: { username: string; isSelf: boolean }) {
  const [following, setFollowing] = useState<boolean | null>(null)
  const [followers, setFollowers] = useState<string[]>([])
  const [followingList, setFollowingList] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [modal, setModal] = useState<FollowModal>(null)

  useEffect(() => {
    const reqs: Promise<any>[] = [
      api.getFollowers(username),
      api.getFollowing(username),
    ]
    if (!isSelf) reqs.push(api.isFollowing(username))
    Promise.all(reqs).then(([fl, fg, isF]) => {
      setFollowers(fl)
      setFollowingList(fg)
      if (!isSelf) setFollowing(isF.following)
    }).catch(() => { if (!isSelf) setFollowing(false) })
  }, [username, isSelf])

  async function toggle() {
    if (following === null) return
    setBusy(true)
    try {
      if (following) {
        await api.unfollow(username)
        setFollowing(false)
        setFollowers(f => f.filter(u => u !== getUsername()))
      } else {
        await api.follow(username)
        setFollowing(true)
        setFollowers(f => [getUsername(), ...f])
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginTop: '0.5rem', flexWrap: 'wrap' }}>
        <button className="btn btn-ghost btn-sm" style={{ padding: '2px 4px', fontSize: '0.78rem', color: 'var(--t2)' }} onClick={() => setModal('followers')}>
          <span style={{ color: 'var(--t1)', fontWeight: 700 }}>{followers.length}</span>&nbsp;followers
        </button>
        <span style={{ color: 'var(--t3)', fontSize: '0.78rem' }}>·</span>
        <button className="btn btn-ghost btn-sm" style={{ padding: '2px 4px', fontSize: '0.78rem', color: 'var(--t2)' }} onClick={() => setModal('following')}>
          <span style={{ color: 'var(--t1)', fontWeight: 700 }}>{followingList.length}</span>&nbsp;following
        </button>
        {!isSelf && following !== null && (
          <button
            className={`btn btn-sm ${following ? 'btn-ghost' : 'btn-primary'}`}
            onClick={toggle}
            disabled={busy}
          >
            {busy ? <Spinner size="0.75em" /> : following ? 'Unfollow' : 'Follow'}
          </button>
        )}
      </div>
      {modal === 'followers' && (
        <UserListModal title="Followers" users={followers} onClose={() => setModal(null)} />
      )}
      {modal === 'following' && (
        <UserListModal title="Following" users={followingList} onClose={() => setModal(null)} />
      )}
    </>
  )
}

export function ProfilePage({ username }: { username: string }) {
  const [data, setData]     = useState<ProfileActivity | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError]   = useState('')
  const [tab, setTab]       = useState<Tab>('cls')
  const currentUser = getUsername()

  useEffect(() => {
    setLoading(true)
    api.getUserActivity(username)
      .then(setData)
      .catch(e => setError(e?.message ?? 'Failed to load profile'))
      .finally(() => setLoading(false))
  }, [username])

  if (loading) return <PageSpinner />
  if (error || !data) return <p className="panel" style={{ color: 'var(--red)' }}>{error || 'Not found'}</p>

  const { user, cls, comments, reviews } = data
  const reviewCount = reviews.lgtm.length + reviews.needs_work.length + reviews.comment.length

  return (
    <div>
      {/* header + tabs as one block */}
      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: '1.25rem', flexWrap: 'wrap' }}>
          <div className="avatar" style={{ width: '5rem', height: '5rem', fontSize: '1.6rem', borderRadius: 0 }}>
            {user.username.slice(0, 2).toUpperCase()}
          </div>
          <div style={{ flex: 1 }}>
            <h1 className="page-title" style={{ margin: 0 }}>{user.username}</h1>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginTop: '0.3rem' }}>
              <span style={{ fontSize: '0.8rem', color: roleColor(user.role), fontWeight: 600 }}>{user.role}</span>
              {user.email && <span style={{ fontSize: '0.8rem', color: 'var(--t3)' }}>{user.email}</span>}
            </div>
            <FollowStats username={username} isSelf={currentUser === username} />
          </div>
        </div>

        {/* tabs */}
        <div className="panel-sep" style={{ display: 'flex', gap: '0.4rem' }}>
          <TabBtn active={tab === 'cls'}      onClick={() => setTab('cls')}      label={`CLs (${cls.length})`} />
          <TabBtn active={tab === 'comments'} onClick={() => setTab('comments')} label={`Comments (${comments.length})`} />
          <TabBtn active={tab === 'reviews'}  onClick={() => setTab('reviews')}  label={`Reviews (${reviewCount})`} />
        </div>
      </div>

      {tab === 'cls' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {cls.length === 0 && <Empty>No CLs submitted yet.</Empty>}
          {cls.map(cl => (
            <div key={cl.id} className="card cl-card" style={rowStyle} onClick={() => navigate(`/cl/${cl.id}`)}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600, fontSize: '0.88rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cl.title}</div>
                <div style={{ fontSize: '0.74rem', color: 'var(--t3)', marginTop: '2px' }}>{cl.repo} · {humanTime(cl.updated_at)}</div>
              </div>
              <CLStatusBadge status={cl.status} />
            </div>
          ))}
        </div>
      )}

      {tab === 'comments' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {comments.length === 0 && <Empty>No comments yet.</Empty>}
          {comments.map(c => (
            <div key={c.id} className="card cl-card" style={{ alignItems: 'flex-start', flexDirection: 'column', gap: '0.25rem', cursor: 'pointer' }}
              onClick={() => navigate(`/cl/${c.cl_id}`)}>
              <div style={{ fontSize: '0.74rem', color: 'var(--t3)' }}>
                {c.repo} · {c.cl_title}{c.file_path ? ` · ${c.file_path}${c.line ? `:${c.line}` : ''}` : ''} · {humanTime(c.created_at)}
              </div>
              <div style={{ fontSize: '0.85rem', whiteSpace: 'pre-wrap', textAlign: 'left' }}>{c.body}</div>
            </div>
          ))}
        </div>
      )}

      {tab === 'reviews' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
          <ReviewGroup title="LGTM" color="var(--green, #46c46e)" items={reviews.lgtm} />
          <ReviewGroup title="Needs work" color="var(--red)" items={reviews.needs_work} />
          <ReviewGroup title="Comment" color="var(--t2)" items={reviews.comment} />
        </div>
      )}
    </div>
  )
}

const rowStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', gap: '0.75rem',
  textAlign: 'left', cursor: 'pointer', width: '100%',
}

function TabBtn({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }) {
  return (
    <button onClick={onClick} className="btn btn-ghost btn-sm"
      style={{
        borderRadius: 'var(--r-sm)',
        borderBottom: `2px solid ${active ? 'var(--accent)' : 'transparent'}`,
        color: active ? 'var(--t1)' : 'var(--t3)', fontWeight: active ? 700 : 500,
      }}>
      {label}
    </button>
  )
}

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="panel" style={{ fontSize: '0.85rem', color: 'var(--t3)' }}>{children}</p>
}


function ReviewGroup({ title, color, items }: { title: string; color: string; items: ProfileReview[] }) {
  if (items.length === 0) return null
  return (
    <div>
      <div className="section-title panel" style={{ color, marginBottom: '0.4rem' }}>{title} ({items.length})</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
        {items.map((rv, i) => (
          <div key={i} className="card cl-card" style={rowStyle} onClick={() => navigate(`/cl/${rv.cl_id}`)}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 600, fontSize: '0.85rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{rv.cl_title}</div>
              <div style={{ fontSize: '0.74rem', color: 'var(--t3)', marginTop: '2px' }}>{rv.repo} · {humanTime(rv.created_at)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
