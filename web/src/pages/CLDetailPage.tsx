import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { CLStatusBadge, BuildBadge } from '../components/Badge'
import { PageSpinner, Spinner } from '../components/Spinner'
import { DiffViewer } from '../components/DiffViewer'
import { PatchsetSummaryModal } from '../components/PatchsetSummaryModal'
import { ReviewPanel } from '../components/ReviewPanel'
import { BuildLog } from '../components/BuildLog'
import { UserLink } from '../components/UserLink'
import { navigate, goBack } from '../lib/router'
import { getUsername, getServer } from '../lib/auth'
import { api } from '../lib/api'
import { humanTime, shortSHA, buildDuration } from '../lib/theme'
import { Icon, Glyphs, type IconComponent } from '../components/icons'
import type { CL, Patchset, Review, Comment, Build, FileDiff, User, Tool } from '../lib/api'

type Tab = 'overview' | 'diff' | 'builds' | 'history'

interface Props {
  id: number
}

// ── activity feed ──────────────────────────────────────────────────────────────

type ActivityItem =
  | { kind: 'patchset'; ps: Patchset; ts: string }
  | { kind: 'review';   r: Review;    ts: string }
  | { kind: 'comment';  c: Comment;   ts: string }

function buildActivity(patchsets: Patchset[], reviews: Review[], comments: Comment[]): ActivityItem[] {
  const items: ActivityItem[] = [
    ...patchsets.map(ps => ({ kind: 'patchset' as const, ps, ts: ps.created_at })),
    ...reviews.filter(r => r.verdict !== 'comment').map(r => ({ kind: 'review' as const, r, ts: r.created_at })),
    ...comments.map(c => ({ kind: 'comment' as const, c, ts: c.created_at })),
  ]
  return items.sort((a, b) => (a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : 0))
}

// ── page ───────────────────────────────────────────────────────────────────────

export function CLDetailPage({ id }: Props) {
  const [cl,        setCl]        = useState<CL | null>(null)
  const [patchsets, setPatchsets] = useState<Patchset[]>([])
  const [reviews,   setReviews]   = useState<Review[]>([])
  const [comments,  setComments]  = useState<Comment[]>([])
  const [builds,    setBuilds]    = useState<Build[]>([])
  const [tools,     setTools]     = useState<Tool[]>([])
  const [diffs,     setDiffs]     = useState<FileDiff[]>([])
  const [users,     setUsers]     = useState<User[]>([])
  const [loading,   setLoading]   = useState(true)
  const [error,     setError]     = useState('')
  const [tab,       setTab]       = useState<Tab>('overview')
  const [activeTool, setActiveTool] = useState<string | null>(null)
  const [toast,     setToast]     = useState('')
  const [summaryPS, setSummaryPS] = useState<number | null>(null)
  const [pendingComment, setPendingComment] = useState<number | null>(null)
  const currentUser = getUsername()

  async function load() {
    try {
      const [clData, psData, revData, cmtData, bldData, usersData, toolsData] = await Promise.all([
        api.getCL(id),
        api.getPatchsets(id),
        api.getReviews(id),
        api.getComments(id),
        api.getBuilds(id),
        api.listUsers(),
        api.listTools(),
      ])
      setCl(clData)
      setPatchsets(psData)
      setReviews(revData)
      setComments(cmtData)
      setBuilds(bldData)
      setUsers(usersData)
      setTools(toolsData)
    } catch (e: any) {
      setError(e?.message ?? 'Failed to load')
    } finally {
      setLoading(false)
    }
  }

  async function loadDiff() {
    if (patchsets.length === 0) return
    const latest = patchsets[patchsets.length - 1]
    try {
      const d = await api.getDiff(id, latest.number)
      setDiffs(d)
    } catch {}
  }

  useEffect(() => { load() }, [id])
  useEffect(() => { if (tab === 'diff') loadDiff() }, [tab, patchsets.length])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(''), 3000)
    return () => clearTimeout(t)
  }, [toast])

  // Live updates over SSE: the server pings whenever this CL changes (build
  // created/started/finished, new patchset). We re-fetch authoritative state on
  // each ping. EventSource auto-reconnects and sends the session cookie.
  useEffect(() => {
    const es = new EventSource(`${getServer()}/cl/${id}/events`, { withCredentials: true })
    const refresh = async () => {
      try {
        const [clData, b, ps] = await Promise.all([
          api.getCL(id),
          api.getBuilds(id),
          api.getPatchsets(id),
        ])
        setCl(clData)
        setBuilds(b)
        setPatchsets(ps)
      } catch {}
    }
    es.addEventListener('update', refresh)
    return () => es.close()
  }, [id])

  async function handleReview(verdict: string) {
    await api.postReview(id, verdict)
    setReviews(await api.getReviews(id))
  }

  async function handleComment(file: string, line: number, body: string) {
    await api.postComment(id, { file, line, body })
    setComments(await api.getComments(id))
  }

  // Jump from an activity comment card to its line in the diff. DiffViewer
  // expands the thread and scrolls to it; we clear the focus shortly after so
  // the same jump can re-trigger and tab switches don't re-scroll.
  function jumpToComment(commentID: number) {
    setTab('diff')
    setPendingComment(commentID)
  }
  useEffect(() => {
    if (pendingComment == null) return
    const t = setTimeout(() => setPendingComment(null), 2000)
    return () => clearTimeout(t)
  }, [pendingComment])

  async function handleAssign(username: string) {
    await api.assignReviewer(id, username)
    setReviews(await api.getReviews(id))
  }

  async function handleSubmit() {
    await api.submitCL(id)
    await load()
  }

  async function handleAbandon() {
    await api.abandonCL(id)
    await load()
  }

  async function handleTriggerBuilds(ts: string[]) {
    await api.triggerBuilds(id, ts)
    setBuilds(await api.getBuilds(id)) // instant feedback; SSE keeps it live after
    setToast('Builds queued')
  }

  if (loading) return <PageSpinner />
  if (error || !cl) return <p className="panel" style={{ color: 'var(--red)' }}>{error || 'CL not found'}</p>

  const latestPS   = patchsets[patchsets.length - 1]
  const lgtmCount  = reviews.filter(r => r.verdict === 'lgtm').length
  const needsWork  = reviews.some(r => r.verdict === 'needs_work')
  const canSubmit  = cl.status === 'open' && lgtmCount > 0 && !needsWork
  const isAuthor   = cl.author === currentUser
  const isOpen     = cl.status === 'open'

  return (
    <div>
      {toast && createPortal(<div className="toast">{toast}</div>, document.body)}

      {/* ── header + tabs: one block, so none of it sits bare on the page bg ── */}
      <div className="panel" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.35rem' }}>
          <button className="btn btn-ghost btn-sm" onClick={() => goBack()}>← Back</button>
          <CLStatusBadge status={cl.status} />
          <span style={{ fontSize: '0.8rem', color: 'var(--t3)' }}>CL #{cl.id}</span>
        </div>
        <h1 className="page-title" style={{ marginBottom: '0.25rem' }}>{cl.title}</h1>
        <div className="cl-meta" style={{ fontSize: '0.85rem' }}>
          <button className="btn btn-ghost btn-sm" style={{ padding: 0, color: 'var(--t2)' }} onClick={() => navigate(`/repo/${cl.repo}`)}>{cl.repo}</button>
          <span>→</span>
          <span style={{ color: 'var(--accent)' }}>{cl.target_branch}</span>
          <span>·</span>
          <span>by <UserLink username={cl.author}><strong>{cl.author}</strong></UserLink></span>
          <span>·</span>
          <span>updated {humanTime(cl.updated_at)}</span>
          {latestPS && (
            <>
              <span>·</span>
              <span style={{ fontFamily: 'monospace', fontSize: '0.78rem' }}>{shortSHA(latestPS.commit_sha)}</span>
            </>
          )}
        </div>

        {/* panel-sep already draws the rule above the strip, so .tabs' own
            bottom border would leave a stray line at the panel's edge. */}
        <div className="tabs panel-sep" style={{ marginBottom: 0, borderBottom: 'none' }}>
          {(['overview', 'diff', 'builds', 'history'] as Tab[]).map(t => (
            <button
              key={t}
              className={`tab${tab === t ? ' active' : ''}`}
              onClick={() => setTab(t)}
            >
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
        </div>
      </div>

      {/* ── content + sidebar ── */}
      <div className="detail-layout">
        <div className="detail-main">
          {tab === 'overview' && (
            <OverviewTab
              cl={cl}
              patchsets={patchsets}
              reviews={reviews}
              comments={comments}
              onViewPatchset={setSummaryPS}
              onJumpToComment={jumpToComment}
            />
          )}
          {tab === 'diff' && (
            <DiffViewer
              diffs={diffs}
              comments={comments}
              onComment={isOpen ? handleComment : undefined}
              focusCommentID={pendingComment}
              repoName={cl.repo}
              commitSha={latestPS?.commit_sha}
            />
          )}
          {tab === 'builds' && (
            <BuildsTab
              clID={id}
              builds={builds}
              tools={tools}
              patchsets={patchsets}
              activeTool={activeTool}
              onSelect={setActiveTool}
              onTrigger={handleTriggerBuilds}
              isOpen={isOpen}
            />
          )}
          {tab === 'history' && <HistoryTab patchsets={patchsets} reviews={reviews} />}
        </div>

        {/* ── sidebar (always visible) ── */}
        <div className="detail-sidebar">
          <SubmitRequirements
            tools={tools}
            builds={builds}
            latestPSID={latestPS?.id ?? 0}
            reviews={reviews}
          />
          <div className="card" style={{ padding: '1rem' }}>
            <ReviewPanel
              clID={id}
              patchsetID={latestPS?.id ?? 0}
              reviews={reviews}
              currentUser={currentUser}
              users={users}
              onReview={isOpen ? handleReview : undefined}
              onAssign={isOpen ? handleAssign : undefined}
              onSubmit={canSubmit ? handleSubmit : undefined}
              onAbandon={isOpen && isAuthor ? handleAbandon : undefined}
              canSubmit={canSubmit}
            />
          </div>
        </div>
      </div>

      {summaryPS != null && (
        <PatchsetSummaryModal clID={id} ps={summaryPS} onClose={() => setSummaryPS(null)} />
      )}
    </div>
  )
}

// ── submit requirements panel ──────────────────────────────────────────────────

function SubmitRequirements({ tools, builds, latestPSID, reviews }: {
  tools: Tool[]
  builds: Build[]
  latestPSID: number
  reviews: Review[]
}) {
  const required = tools.filter(t => t.required)
  const lgtmCount = reviews.filter(r => r.verdict === 'lgtm').length
  const needsWork = reviews.some(r => r.verdict === 'needs_work')

  // No requirements at all → nothing to show
  if (required.length === 0 && lgtmCount > 0 && !needsWork) return null

  // Latest build per tool on the current patchset
  const latestBuild = new Map<string, Build>()
  for (const b of builds) {
    if (b.ps_id !== latestPSID) continue
    const prev = latestBuild.get(b.tool_name)
    if (!prev || b.id > prev.id) latestBuild.set(b.tool_name, b)
  }

  // `running` returns no glyph so the row can render a live Spinner instead.
  function toolIcon(name: string): { glyph: IconComponent | null; color: string } {
    const b = latestBuild.get(name)
    if (b?.status === 'pass')    return { glyph: Glyphs.pass,   color: 'var(--green)' }
    if (b?.status === 'fail')    return { glyph: Glyphs.fail,   color: 'var(--red)' }
    if (b?.status === 'running') return { glyph: null,          color: 'var(--accent)' }
    return { glyph: Glyphs.queued, color: 'var(--t3)' }
  }

  return (
    <div className="card" style={{ padding: '1rem', marginBottom: '1rem' }}>
      <div className="section-title" style={{ marginBottom: '0.5rem' }}>Submit requirements</div>

      {required.map(t => {
        const { glyph, color } = toolIcon(t.name)
        return (
          <div key={t.name} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.82rem', padding: '0.18rem 0' }}>
            <span style={{ color, display: 'flex', flexShrink: 0 }}>
              {glyph ? <Icon glyph={glyph} size={14} /> : <Spinner size="0.9em" />}
            </span>
            <span style={{ color: 'var(--t2)' }}>{t.name} passes</span>
          </div>
        )
      })}

      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.82rem', padding: '0.18rem 0' }}>
        <span style={{ color: lgtmCount > 0 ? 'var(--green)' : 'var(--t3)', display: 'flex', flexShrink: 0 }}>
          <Icon glyph={lgtmCount > 0 ? Glyphs.pass : Glyphs.queued} size={14} />
        </span>
        <span style={{ color: 'var(--t2)' }}>LGTM</span>
        {lgtmCount > 0 && <span style={{ color: 'var(--t3)', fontSize: '0.75rem' }}>({lgtmCount})</span>}
      </div>

      {needsWork && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.82rem', padding: '0.18rem 0' }}>
          <span style={{ color: 'var(--red)', display: 'flex', flexShrink: 0 }}>
            <Icon glyph={Glyphs.fail} size={14} />
          </span>
          <span style={{ color: 'var(--t2)' }}>Needs work outstanding</span>
        </div>
      )}
    </div>
  )
}

// ── overview tab ───────────────────────────────────────────────────────────────

function verdictLabel(v: string) {
  if (v === 'lgtm')       return { label: 'LGTM',       color: 'var(--green)' }
  if (v === 'needs_work') return { label: 'Needs work',  color: 'var(--red)' }
  return { label: v, color: 'var(--t3)' }
}

function OverviewTab({ cl, patchsets, reviews, comments, onViewPatchset, onJumpToComment }: {
  cl: CL
  patchsets: Patchset[]
  reviews: Review[]
  comments: Comment[]
  onViewPatchset: (ps: number) => void
  onJumpToComment: (id: number) => void
}) {
  const activity = buildActivity(patchsets, reviews, comments)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
      {cl.description && (
        <div className="card" style={{ padding: '1rem' }}>
          <div className="section-title">Description</div>
          <p style={{ fontSize: '0.9rem', color: 'var(--t2)', whiteSpace: 'pre-wrap', margin: 0 }}>
            {cl.description}
          </p>
        </div>
      )}

      <div className="card" style={{ padding: '1rem' }}>
        <div className="section-title" style={{ marginBottom: '0.75rem' }}>Activity</div>

        {activity.length === 0 && (
          <p style={{ fontSize: '0.85rem', color: 'var(--t3)', margin: 0 }}>No activity yet.</p>
        )}

        <div className="activity-feed">
          {activity.map((item) => {
            if (item.kind === 'patchset') {
              const { ps } = item
              return (
                <div key={`ps-${ps.id}`} className="activity-item activity-patchset">
                  <div className="activity-dot" />
                  <div className="activity-body">
                    <div className="activity-line">
                      <button
                        className="activity-link"
                        onClick={() => onViewPatchset(ps.number)}
                        title="View files changed in this patchset"
                      >
                        Patchset {ps.number} uploaded
                      </button>
                      <code className="activity-sha">{shortSHA(ps.commit_sha)}</code>
                      <span className="activity-time">{humanTime(ps.created_at)}</span>
                    </div>
                    {ps.message && (
                      <p className="activity-detail">{ps.message.split('\n')[0]}</p>
                    )}
                  </div>
                </div>
              )
            }

            if (item.kind === 'review') {
              const { r } = item
              const { label, color } = verdictLabel(r.verdict)
              return (
                <div key={`rev-${r.id}`} className="activity-item activity-review">
                  <div className="activity-dot" style={{ background: color }} />
                  <div className="activity-body">
                    <div className="activity-line">
                      <UserLink username={r.author} className="activity-author" />
                      <span className="activity-label">voted</span>
                      <span style={{ color, fontWeight: 600, fontSize: '0.82rem' }}>{label}</span>
                      <span className="activity-time">{humanTime(r.created_at)}</span>
                    </div>
                  </div>
                </div>
              )
            }

            // comment
            const { c } = item
            return (
              <div key={`cmt-${c.id}`} className="activity-item activity-comment">
                <div className="activity-dot" />
                <div className="activity-body">
                  <div className="activity-line">
                    <UserLink username={c.author} className="activity-author" />
                    <span className="activity-label">commented</span>
                    <span style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>#{c.id}</span>
                    {c.file_path && c.line != null ? (
                      <button
                        className="activity-link"
                        onClick={() => onJumpToComment(c.id)}
                        title="Jump to this line in the diff"
                      >
                        {c.file_path}:{c.line}
                      </button>
                    ) : c.file_path ? (
                      <code className="activity-sha">{c.file_path}</code>
                    ) : null}
                    <span className="activity-time">{humanTime(c.created_at)}</span>
                  </div>
                  <p className="activity-detail">{c.body}</p>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// ── builds tab ─────────────────────────────────────────────────────────────────

function BuildsTab({ clID, builds, tools, patchsets, activeTool, onSelect, onTrigger, isOpen }: {
  clID: number
  builds: Build[]
  tools: Tool[]
  patchsets: Patchset[]
  activeTool: string | null
  onSelect: (tool: string) => void
  onTrigger: (tools: string[]) => Promise<void>
  isOpen: boolean
}) {
  const [selected, setSelected]     = useState<Set<string>>(() => new Set(tools.map(t => t.name)))
  const [showList, setShowList]     = useState(false)
  const [triggering, setTriggering] = useState(false)
  const [viewPS, setViewPS]         = useState<number | null>(null) // null → latest
  const [psMenuOpen, setPsMenuOpen] = useState(false)
  const [runIndex, setRunIndex]     = useState(0) // 0 = latest run
  const [runMenuOpen, setRunMenuOpen] = useState(false)

  // keep selection in sync if tools arrive after initial render
  useEffect(() => {
    setSelected(new Set(tools.map(t => t.name)))
  }, [tools.length])

  const toolOrder = new Map(tools.map((t, i) => [t.name, i]))

  // Dropdown lists EVERY patchset, newest first.
  const sortedPS = [...patchsets].sort((a, b) => b.number - a.number)
  const latestNumber = sortedPS[0]?.number ?? 0
  const selPS = sortedPS.find(p => p.number === viewPS) ?? sortedPS[0] ?? null

  // For the selected patchset, all runs of each tool, newest first.
  const runsByTool = new Map<string, Build[]>()
  if (selPS) {
    for (const b of builds) {
      if (b.ps_id !== selPS.id) continue
      const arr = runsByTool.get(b.tool_name) ?? []
      arr.push(b)
      runsByTool.set(b.tool_name, arr)
    }
    for (const arr of runsByTool.values()) arr.sort((a, b) => b.id - a.id)
  }
  const maxRuns = Math.max(0, ...[...runsByTool.values()].map(a => a.length))
  const safeRun = Math.min(runIndex, Math.max(0, maxRuns - 1))

  // Show the run slice selected by the dropdown; tools with no run at that depth are omitted.
  const viewBuilds = [...runsByTool.entries()]
    .map(([, arr]) => arr[safeRun])
    .filter((b): b is Build => !!b)
    .sort((a, b) => (toolOrder.get(a.tool_name) ?? 99) - (toolOrder.get(b.tool_name) ?? 99))

  // Representative time for a run index (latest finish among that slice).
  function runTime(k: number): string {
    const ts = [...runsByTool.values()]
      .map(a => a[k]?.ended_at)
      .filter(Boolean)
      .sort()
      .pop()
    return ts ? humanTime(ts) : ''
  }
  // Build N: newest run is the highest number; oldest is 1.
  const buildLabel = (k: number) => `Build ${maxRuns - k}`

  // Selection is by tool, resolved against the current run slice, so a
  // rebuild (from the UI or the CLI) moves the log over to the new run instead
  // of staying pinned to the old build's id.
  const activeBuild = viewBuilds.find(b => b.tool_name === activeTool) ?? viewBuilds[0] ?? null

  function toggleTool(name: string) {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(name) ? next.delete(name) : next.add(name)
      return next
    })
  }

  async function trigger() {
    setTriggering(true)
    setShowList(false)
    try { await onTrigger([...selected]); setRunIndex(0) } finally { setTriggering(false) }
  }


  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
      {/* Label rides the control bar rather than floating above it. */}
      <div className="panel" style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap' }}>
        <span className="section-title" style={{ margin: 0 }}>Build runs</span>

        {/* patchset picker */}
        {sortedPS.length > 0 && (
          <>
            <div style={{ position: 'relative' }}>
              <button className="btn btn-ghost btn-sm" onClick={() => setPsMenuOpen(o => !o)}>
                Patchset {selPS?.number ?? '?'} <Icon glyph={psMenuOpen ? Glyphs.chevronUp : Glyphs.chevronDown} size={12} />
              </button>
              {psMenuOpen && (
                <>
                  <div style={{ position: 'fixed', inset: 0, zIndex: 10 }} onClick={() => setPsMenuOpen(false)} />
                  <div className="ps-menu">
                    {sortedPS.map(p => (
                      <button
                        key={p.id}
                        className={`ps-menu-item${p.number === selPS?.number ? ' active' : ''}`}
                        onClick={() => { setViewPS(p.number); setRunIndex(0); setPsMenuOpen(false) }}
                      >
                        Patchset {p.number}{p.number === latestNumber ? ' — latest' : ''}
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
            {selPS?.number === latestNumber && <span className="pill-latest">LATEST</span>}
          </>
        )}

        {/* build (run) picker + trigger, pushed to the right */}
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
          {maxRuns > 0 && (
            <>
              <div style={{ position: 'relative' }}>
                <button className="btn btn-ghost btn-sm" onClick={() => setRunMenuOpen(o => !o)}>
                  {buildLabel(safeRun)} <Icon glyph={runMenuOpen ? Glyphs.chevronUp : Glyphs.chevronDown} size={12} />
                </button>
                {runMenuOpen && (
                  <>
                    <div style={{ position: 'fixed', inset: 0, zIndex: 10 }} onClick={() => setRunMenuOpen(false)} />
                    <div className="ps-menu">
                      {Array.from({ length: maxRuns }, (_, k) => (
                        <button
                          key={k}
                          className={`ps-menu-item${k === safeRun ? ' active' : ''}`}
                          onClick={() => {
                            setRunIndex(k)
                            setRunMenuOpen(false)
                          }}
                        >
                          {buildLabel(k)}{runTime(k) ? ` · ${runTime(k)}` : ''}{k === 0 ? ' — latest' : ''}
                        </button>
                      ))}
                    </div>
                  </>
                )}
              </div>
              {safeRun === 0 && <span className="pill-latest">LATEST</span>}
            </>
          )}
          {isOpen && (
            <button
              className="btn btn-ghost btn-sm"
              disabled={triggering}
              onClick={() => setShowList(v => !v)}
            >
              {triggering
                ? <Spinner size="0.85em" />
                : <><Icon glyph={Glyphs.rerun} size={13} /> Trigger builds <Icon glyph={showList ? Glyphs.chevronUp : Glyphs.chevronDown} size={12} /></>}
            </button>
          )}
        </div>
      </div>

      {showList && tools.length > 0 && (
        <div className="card" style={{ padding: '0.75rem 1rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {tools.map(t => (
            <label key={t.name} style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', cursor: 'pointer', fontSize: '0.85rem' }}>
              <input
                type="checkbox"
                checked={selected.has(t.name)}
                onChange={() => toggleTool(t.name)}
                style={{ accentColor: 'var(--accent)', width: '0.9rem', height: '0.9rem' }}
              />
              <span style={{ flex: 1 }}>{t.name}</span>
              {t.required && <span style={{ fontSize: '0.7rem', color: 'var(--accent)', opacity: 0.8 }}>required</span>}
              {t.depends_on.length > 0 && <span style={{ fontSize: '0.7rem', color: 'var(--t3)' }}>needs {t.depends_on.join(', ')}</span>}
            </label>
          ))}
          <button
            className="btn btn-primary btn-sm"
            style={{ marginTop: '0.25rem' }}
            disabled={selected.size === 0 || triggering}
            onClick={trigger}
          >
            Run selected
          </button>
        </div>
      )}

      {builds.length === 0 && !showList && (
        <div className="empty panel">
          <Icon glyph={Glyphs.queued} size={38} style={{ opacity: 0.5, marginBottom: '0.75rem' }} />
          <div className="empty-title">No builds yet</div>
        </div>
      )}

      {selPS && viewBuilds.length === 0 && builds.length > 0 && (
        <p className="panel" style={{ fontSize: '0.85rem', color: 'var(--t3)', margin: 0 }}>
          No builds for patchset {selPS.number} yet.
        </p>
      )}

      {viewBuilds.length > 0 && (
        <div className="build-list">
          {viewBuilds.map(b => (
            <div
              key={b.id}
              role="button"
              tabIndex={0}
              className={`build-list-item${b.id === activeBuild?.id ? ' active' : ''}`}
              onClick={() => onSelect(b.tool_name)}
              onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect(b.tool_name) } }}
            >
              <BuildBadge status={b.status} />
              <span style={{ flex: 1, textAlign: 'left', fontSize: '0.85rem' }}>{b.tool_name}</span>
              <span style={{ fontSize: '0.75rem', color: 'var(--t3)' }}>{buildDuration(b)}</span>
            </div>
          ))}
        </div>
      )}

      {activeBuild && (
        <div className="card" style={{ padding: '1rem' }}>
          {/* key remounts BuildLog per build so logs are scoped to the
              selected task — clears stale output and re-fetches the
              selected build's full (persisted) log from the server. */}
          <BuildLog key={activeBuild.id} clID={clID} build={activeBuild} />
        </div>
      )}
    </div>
  )
}

// ── history tab ────────────────────────────────────────────────────────────────

function HistoryTab({ patchsets, reviews }: { patchsets: Patchset[]; reviews: Review[] }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
      <div className="card" style={{ padding: '1rem' }}>
        <div className="section-title">Patchset history</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          {[...patchsets].reverse().map(ps => (
            <div key={ps.id} style={{ display: 'flex', gap: '1rem', alignItems: 'flex-start' }}>
              <div style={{ width: '3rem', textAlign: 'right', flexShrink: 0, color: 'var(--t3)', fontSize: '0.8rem', paddingTop: '0.1rem' }}>
                PS{ps.number}
              </div>
              <div style={{ flex: 1 }}>
                <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.2rem' }}>
                  <code style={{ fontSize: '0.78rem', color: 'var(--accent)' }}>{shortSHA(ps.commit_sha)}</code>
                  <span style={{ fontSize: '0.75rem', color: 'var(--t3)' }}>{humanTime(ps.created_at)}</span>
                </div>
                {ps.message && (
                  <p style={{ fontSize: '0.85rem', color: 'var(--t2)', margin: 0, whiteSpace: 'pre-wrap' }}>
                    {ps.message}
                  </p>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      {reviews.length > 0 && (
        <div className="card" style={{ padding: '1rem' }}>
          <div className="section-title">Review history</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            {reviews.map(r => {
              const { label, color } = r.verdict === 'lgtm'
                ? { label: 'LGTM', color: 'var(--green)' }
                : r.verdict === 'needs_work'
                ? { label: 'Needs work', color: 'var(--red)' }
                : { label: r.verdict, color: 'var(--t3)' }
              return (
                <div key={r.id} style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', fontSize: '0.85rem' }}>
                  <span style={{ color, fontWeight: 600, minWidth: '6rem' }}>{label}</span>
                  <UserLink username={r.author} style={{ color: 'var(--t2)' }} />
                  <span style={{ color: 'var(--t3)', marginLeft: 'auto', fontSize: '0.75rem' }}>{humanTime(r.created_at)}</span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
