import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { CLStatusBadge, BuildBadge, VerdictBadge } from '../components/Badge'
import { PageSpinner, Spinner } from '../components/Spinner'
import { UserLink } from '../components/UserLink'
import { navigate } from '../lib/router'
import { api } from '../lib/api'
import { humanTime, buildDuration } from '../lib/theme'
import type { CL, Build, Review } from '../lib/api'
import { getViewSettings, saveViewSettings } from '../lib/viewSettings'
import { Icon, Glyphs } from '../components/icons'
import { MergeStackModal } from '../components/MergeStackModal'
import { StackReviewModal } from '../components/StackReviewModal'
import type { StepResult } from '../components/StackReviewModal'
import { latestBuildPerTool } from '../lib/builds'
import { getUsername } from '../lib/auth'

type Filter = 'all' | 'open' | 'merged' | 'abandoned' | 'mine' | 'assigned'
type SortBy = 'updated' | 'id_desc' | 'id_asc'

// How often the feed refetches in the background so new CLs, status changes,
// patchsets and reviews show up without a manual reload.
const LIST_POLL_MS = 10000

interface Props {
  filter?: Filter
}

interface ChainInfo {
  rootId: number   // id of the earliest still-open ancestor in this chain
  position: number // 0-based depth within the chain (0 = root/ancestor)
  size: number      // total open CLs in this chain (1 = not actually a chain)
}

// computeChains links CLs that were pushed together in the same stack: a CL's
// base commit matches another still-open CL's tip commit (see ListCLs' JOIN
// in internal/store/cl.go, which is what makes latest_commit_sha/
// latest_base_sha available here). Runs over every loaded CL, not just the
// currently filtered/visible ones, so a repo filter or search term can't make
// a chain look shorter than it really is.
function computeChains(list: CL[]): Map<number, ChainInfo> {
  const open = list.filter(cl => cl.status === 'open' && cl.latest_commit_sha)
  const byTip = new Map<string, CL>()
  for (const cl of open) byTip.set(`${cl.repo}:${cl.latest_commit_sha}`, cl)

  const parentOf = new Map<number, CL>()
  for (const cl of open) {
    if (!cl.latest_base_sha) continue
    const parent = byTip.get(`${cl.repo}:${cl.latest_base_sha}`)
    if (parent && parent.id !== cl.id) parentOf.set(cl.id, parent)
  }

  function rootOf(cl: CL): CL {
    let cur = cl
    const seen = new Set([cl.id])
    for (let p = parentOf.get(cur.id); p && !seen.has(p.id); p = parentOf.get(cur.id)) {
      seen.add(p.id)
      cur = p
    }
    return cur
  }

  function depthOf(cl: CL): number {
    let d = 0
    const seen = new Set([cl.id])
    for (let p = parentOf.get(cl.id); p && !seen.has(p.id); p = parentOf.get(p.id)) {
      seen.add(p.id)
      d++
    }
    return d
  }

  const sizeByRoot = new Map<number, number>()
  for (const cl of open) {
    const rootId = rootOf(cl).id
    sizeByRoot.set(rootId, (sizeByRoot.get(rootId) ?? 0) + 1)
  }

  const chains = new Map<number, ChainInfo>()
  for (const cl of open) {
    const rootId = rootOf(cl).id
    chains.set(cl.id, { rootId, position: depthOf(cl), size: sizeByRoot.get(rootId) ?? 1 })
  }
  return chains
}

// Groups chain members together (root first) and orders the resulting blocks
// (lone CLs and whole chains alike) by sortBy, so a stack never gets split
// across the list no matter which sort the user picked.
function groupAndSort(list: CL[], chains: Map<number, ChainInfo>, sortBy: SortBy): CL[] {
  const byRoot = new Map<number, CL[]>()
  const blocks: CL[][] = []
  for (const cl of list) {
    const info = chains.get(cl.id)
    if (info && info.size > 1) {
      let arr = byRoot.get(info.rootId)
      if (!arr) { arr = []; byRoot.set(info.rootId, arr); blocks.push(arr) }
      arr.push(cl)
    } else {
      blocks.push([cl])
    }
  }
  for (const block of blocks) {
    if (block.length > 1) block.sort((a, b) => chains.get(a.id)!.position - chains.get(b.id)!.position)
  }

  const keyOf = (block: CL[]): number => {
    if (sortBy === 'id_asc')  return Math.min(...block.map(c => c.id))
    if (sortBy === 'id_desc') return Math.max(...block.map(c => c.id))
    return Math.max(...block.map(c => new Date(c.updated_at).getTime()))
  }
  blocks.sort((a, b) => sortBy === 'id_asc' ? keyOf(a) - keyOf(b) : keyOf(b) - keyOf(a))

  return blocks.flat()
}

// Regroups the already-ordered flat list back into renderable blocks: a chain
// (size > 1) becomes one block of its members — already contiguous and
// root-first thanks to groupAndSort — everything else is its own lone block.
function toRenderBlocks(list: CL[], chains: Map<number, ChainInfo>): CL[][] {
  const blocks: CL[][] = []
  let i = 0
  while (i < list.length) {
    const info = chains.get(list[i].id)
    if (info && info.size > 1) {
      const rootId = info.rootId
      const block: CL[] = []
      while (i < list.length && chains.get(list[i].id)?.rootId === rootId) {
        block.push(list[i])
        i++
      }
      blocks.push(block)
    } else {
      blocks.push([list[i]])
      i++
    }
  }
  return blocks
}

export function CLListPage({ filter = 'all' }: Props) {
  // Saved view preferences apply as the initial value only — direct links to
  // a specific status tab (/changes/open etc.) still win over a saved
  // preference, and the existing tab-switch reset below still clears the
  // in-session repo filter as before; only explicit interaction re-saves.
  const savedCL = getViewSettings().cl_list

  const [cls,        setCls]        = useState<CL[]>([])
  const [loading,    setLoading]    = useState(true)
  const [error,      setError]      = useState('')
  const [search,     setSearch]     = useState('')
  const [status,     setStatus]     = useState<string>(
    filter === 'all' || filter === 'mine' || filter === 'assigned'
      ? (savedCL?.status ?? '')
      : filter
  )
  const [repoFilter, setRepoFilter] = useState(savedCL?.repo ?? '')
  const [sortBy,     setSortBy]     = useState<SortBy>(savedCL?.sort ?? 'updated')

  // bulk-select state
  const [selectMode, setSelectMode] = useState(false)
  const [selected,   setSelected]   = useState<Set<number>>(new Set())
  const [working,    setWorking]    = useState(false)

  // Stacks (chains) render collapsed into one compact card by default; a
  // stack's root CL id in this set means it's temporarily expanded into its
  // individual CL cards instead. Not persisted — resets on reload.
  const [expandedChains, setExpandedChains] = useState<Set<number>>(new Set())
  // The stack being merged. Held here, not in StackCard: merging the bottom CL
  // changes the stack's identity, which remounts its card, and the dialog has
  // to survive that to show progress and the result.
  const [mergeStack, setMergeStack] = useState<CL[] | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    api.listCLs({ status: status || undefined, mine: filter === 'mine', assigned: filter === 'assigned' })
      .then(data => { setCls(data); setLoading(false) })
      .catch(e  => { setError(e?.message ?? 'Failed to load'); setLoading(false) })
  }, [filter, status])

  useEffect(() => { load() }, [load])

  // Refresh in place, without the loading spinner: used while a merge is
  // running so the list behind the dialog updates as each CL lands.
  const refreshQuietly = useCallback(() => {
    api.listCLs({ status: status || undefined, mine: filter === 'mine', assigned: filter === 'assigned' })
      .then(setCls)
      .catch(() => {})
  }, [filter, status])

  // Reset filters/selection when the tab/status changes so nothing ghosts
  useEffect(() => {
    setRepoFilter('')
    setSelectMode(false)
    setSelected(new Set())
  }, [filter, status])

  // Keep the feed live: new CLs, status changes, patchsets and reviews land
  // without a manual reload. Polled (crit is served over HTTP/1.1, same
  // reasoning as the stack view) and quiet — no spinner — so the list never
  // flashes. Paused while the tab is hidden, while bulk-selecting (a reorder
  // mid-select is jarring and could move a row out from under the cursor), and
  // while the merge dialog is open (it drives its own refresh).
  useEffect(() => {
    if (selectMode || mergeStack) return
    const t = setInterval(() => {
      if (document.hidden) return
      refreshQuietly()
    }, LIST_POLL_MS)
    return () => clearInterval(t)
  }, [refreshQuietly, selectMode, mergeStack])

  // Any open CL is selectable — build/review/submit are meaningful regardless
  // of authorship (unlike abandon, which the server still restricts to the
  // author; an unauthorized bulk-abandon just surfaces in the failure count).
  const selectable = (cl: CL) => cl.status === 'open'

  function toggleSelect(id: number) {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  function selectChain(rootId: number) {
    const ids = sorted.filter(cl => chains.get(cl.id)?.rootId === rootId).map(cl => cl.id)
    setSelected(prev => new Set([...prev, ...ids]))
  }

  // Toggle-capable version for the collapsed stack card's checkbox — selects
  // the whole chain if any member isn't yet selected, deselects it entirely
  // once every member already is (same on/off pattern as a single checkbox).
  function toggleChainSelected(rootId: number) {
    const ids = sorted.filter(cl => chains.get(cl.id)?.rootId === rootId).map(cl => cl.id)
    const allSelected = ids.every(id => selected.has(id))
    setSelected(prev => {
      const next = new Set(prev)
      for (const id of ids) allSelected ? next.delete(id) : next.add(id)
      return next
    })
  }

  function toggleExpandChain(rootId: number) {
    setExpandedChains(prev => {
      const next = new Set(prev)
      next.has(rootId) ? next.delete(rootId) : next.add(rootId)
      return next
    })
  }

  // Fan out a bulk action across every selected CL independently (order
  // doesn't matter — used for build-trigger and review, unlike submit).
  async function runBulk(label: string, fn: (id: number) => Promise<unknown>) {
    if (selected.size === 0) return
    setWorking(true)
    const ids = [...selected]
    const results = await Promise.allSettled(ids.map(fn))
    setWorking(false)
    const failures = results.filter(r => r.status === 'rejected').length
    setError(failures > 0 ? `${label}: ${failures} of ${ids.length} failed` : '')
    setSelected(new Set())
    setSelectMode(false)
    load()
  }

  async function abandonSelected() {
    if (selected.size === 0) return
    if (!window.confirm(`Abandon ${selected.size} CL${selected.size > 1 ? 's' : ''}? This can be undone by pushing a new patchset.`)) return
    await runBulk('Abandon', id => api.abandonCL(id))
  }

  async function triggerBuildsSelected() {
    if (selected.size === 0) return
    await runBulk('Trigger builds', id => api.triggerBuilds(id))
  }

  async function reviewSelected(verdict: 'lgtm' | 'needs_work') {
    if (selected.size === 0) return
    const label = verdict === 'lgtm' ? 'LGTM' : 'Needs Work'
    if (!window.confirm(`Mark ${selected.size} CL${selected.size > 1 ? 's' : ''} as ${label}?`)) return
    await runBulk(label, id => api.postReview(id, verdict))
  }

  // Submit in chain order (root/ancestor first) and stop at the first
  // failure — submitting a descendant before its still-open ancestor is
  // rejected by the server's chain guard anyway, and racing ahead past a
  // failure just produces confusing extra errors for CLs that never had a
  // chance. Selected CLs from unrelated chains sort by their own root's
  // position, which for standalone CLs (size 1) is just position 0.
  async function submitSelected() {
    if (selected.size === 0) return
    const ids = [...selected].sort((a, b) => {
      const ca = chains.get(a), cb = chains.get(b)
      if (ca && cb && ca.rootId === cb.rootId) return ca.position - cb.position
      return a - b
    })
    if (!window.confirm(`Submit ${ids.length} CL${ids.length > 1 ? 's' : ''} in order (CL #${ids[0]} first)?`)) return
    setWorking(true)
    let submitted = 0
    for (const id of ids) {
      try {
        await api.submitCL(id)
        submitted++
      } catch (e: any) {
        setError(`Submitted ${submitted} of ${ids.length}; stopped at CL #${id}: ${e?.message ?? 'submit failed'}`)
        break
      }
    }
    setWorking(false)
    if (submitted === ids.length) setError('')
    setSelected(new Set())
    setSelectMode(false)
    load()
  }

  const repos = [...new Set(cls.map(cl => cl.repo))].sort()

  const filtered = cls.filter(cl => {
    if (repoFilter && cl.repo !== repoFilter) return false
    if (!search) return true
    const q = search.toLowerCase()
    return (
      cl.title.toLowerCase().includes(q) ||
      cl.repo.toLowerCase().includes(q) ||
      cl.author.toLowerCase().includes(q) ||
      String(cl.id).includes(q)
    )
  })

  // Computed over the full unfiltered list so a repo filter or search term
  // can't make a chain look shorter than it really is (see computeChains).
  const chains = computeChains(cls)
  const sorted = groupAndSort(filtered, chains, sortBy)

  const pageTitle =
    filter === 'mine'     ? 'My CLs' :
    filter === 'assigned' ? 'Assigned to me' :
    'All CLs'

  return (
    <div>
      {/* Title, controls and status filter read as one header block. */}
      <div className="panel" style={{ marginBottom: '0.75rem' }}>
        <h1 className="page-title" style={{ margin: '0 0 0.6rem' }}>{pageTitle}</h1>

        {/* Controls sit on their own row, packed to the left. Previously they shared
            the title's row and the title took the slack (flex: 1 1 auto), which threw
            them against the right edge — a long mouse trip on a wide screen. */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem', flexWrap: 'wrap' }}>
          {repos.length > 1 && (
            <select
              className="input"
              style={{ maxWidth: '14rem' }}
              value={repoFilter}
              onChange={e => {
                setRepoFilter(e.target.value)
                saveViewSettings({ cl_list: { repo: e.target.value } })
              }}
            >
              <option value="">All projects</option>
              {repos.map(r => <option key={r} value={r}>{r}</option>)}
            </select>
          )}
          <input
            className="input"
            style={{ maxWidth: '14rem' }}
            placeholder="Search CLs…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          <select
            className="input"
            style={{ maxWidth: '11rem' }}
            value={sortBy}
            onChange={e => {
              const v = e.target.value as SortBy
              setSortBy(v)
              saveViewSettings({ cl_list: { sort: v } })
            }}
            title="Sort order"
          >
            <option value="updated">Recently updated</option>
            <option value="id_desc">CL # (newest first)</option>
            <option value="id_asc">CL # (oldest first)</option>
          </select>
          <button
            className={`btn btn-sm ${selectMode ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => { setSelectMode(v => !v); setSelected(new Set()) }}
          >
            {selectMode ? 'Done' : 'Select'}
          </button>
        </div>

        <div className="filter-bar" style={{ marginBottom: 0 }}>
          {(['', 'open', 'merged', 'abandoned'] as const).map(s => (
            <button
              key={s || 'all'}
              className={`filter-pill${status === s ? ' active' : ''}`}
              onClick={() => {
                setStatus(s)
                saveViewSettings({ cl_list: { status: s } })
              }}
            >
              {s || 'All'}
            </button>
          ))}
        </div>
      </div>

      {selectMode && (
        <div className="card" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 0.9rem', marginBottom: '0.75rem', flexWrap: 'wrap' }}>
          <span style={{ fontSize: '0.85rem', color: 'var(--t2)', flex: 1 }}>
            {selected.size === 0 ? 'Select open CLs to act on in bulk.' : `${selected.size} selected`}
          </span>
          <button className="btn btn-ghost btn-sm" disabled={selected.size === 0 || working} onClick={triggerBuildsSelected}>
            {working ? 'Working…' : 'Trigger builds'}
          </button>
          <button className="btn btn-ghost btn-sm" disabled={selected.size === 0 || working} onClick={() => reviewSelected('lgtm')}>
            LGTM selected
          </button>
          <button className="btn btn-ghost btn-sm" disabled={selected.size === 0 || working} onClick={() => reviewSelected('needs_work')}>
            Needs Work selected
          </button>
          <button className="btn btn-primary btn-sm" disabled={selected.size === 0 || working} onClick={submitSelected}>
            Submit selected (in order)
          </button>
          <button
            className="btn btn-danger btn-sm"
            disabled={selected.size === 0 || working}
            onClick={abandonSelected}
          >
            Abandon selected
          </button>
        </div>
      )}

      {loading && <PageSpinner />}
      {error && <p className="panel" style={{ color: 'var(--red)' }}>{error}</p>}

      {!loading && !error && sorted.length === 0 && (
        <div className="empty panel">
          <Icon glyph={Glyphs.changes} size={38} style={{ opacity: 0.5, marginBottom: '0.75rem' }} />
          <div className="empty-title">No CLs found</div>
          <div className="empty-sub">Push a branch with <code>git push origin HEAD:refs/for/main</code></div>
        </div>
      )}

      {!loading && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {toRenderBlocks(sorted, chains).map(block => {
            if (block.length === 1) {
              const cl = block[0]
              return (
                <CLCard
                  key={cl.id}
                  cl={cl}
                  chain={chains.get(cl.id)}
                  showRepo={!repoFilter}
                  selectMode={selectMode}
                  selectable={selectable(cl)}
                  selected={selected.has(cl.id)}
                  onToggle={() => toggleSelect(cl.id)}
                  onSelectChain={selectChain}
                />
              )
            }
            const rootId = chains.get(block[0].id)!.rootId
            return (
              <StackCard
                key={`stack-${rootId}`}
                cls={block}
                chains={chains}
                expanded={expandedChains.has(rootId)}
                onToggleExpand={() => toggleExpandChain(rootId)}
                showRepo={!repoFilter}
                selectMode={selectMode}
                selected={selected}
                onToggleOne={toggleSelect}
                onSelectChain={selectChain}
                onToggleChainSelected={toggleChainSelected}
                onMerge={() => setMergeStack(block)}
              />
            )
          })}
        </div>
      )}

      {mergeStack && (
        <MergeStackModal cls={mergeStack} onClose={() => setMergeStack(null)} onMerged={refreshQuietly} />
      )}
    </div>
  )
}

const STACK_PREVIEW_COUNT = 2
// How often the open stack view refetches its CLs, builds and reviews.
const STACK_POLL_MS = 4000

// A stack of CLs pushed together. Collapsed by default into one compact card
// (roughly two list-rows tall) with a short preview + "+N more". The whole card
// is one target: clicking anywhere opens the stack view (every CL with its
// description and builds). Its buttons merge the stack in order, or expand it
// into the individual CLCards in the list.
function StackCard({ cls, chains, expanded, onToggleExpand, showRepo, selectMode, selected, onToggleOne, onSelectChain, onToggleChainSelected, onMerge }: {
  cls: CL[]
  chains: Map<number, ChainInfo>
  expanded: boolean
  onToggleExpand: () => void
  showRepo: boolean
  selectMode: boolean
  selected: Set<number>
  onToggleOne: (id: number) => void
  onSelectChain: (rootId: number) => void
  onToggleChainSelected: (rootId: number) => void
  onMerge: () => void
}) {
  const rootId = chains.get(cls[0].id)!.rootId

  const [showAll, setShowAll] = useState(false)

  if (expanded) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        {/* Up top, not the bottom — with a big stack this button used to end
            up scrolled far below the fold by the time you wanted it again. */}
        <div style={{ display: 'flex', gap: '0.4rem' }}>
          <button className="btn btn-ghost btn-sm" onClick={onToggleExpand}>
            Collapse stack
          </button>
          <button className="btn btn-primary btn-sm" onClick={onMerge}>
            Merge stack
          </button>
        </div>
        {cls.map(cl => (
          <CLCard
            key={cl.id}
            cl={cl}
            chain={chains.get(cl.id)}
            showRepo={showRepo}
            selectMode={selectMode}
            selectable={cl.status === 'open'}
            selected={selected.has(cl.id)}
            onToggle={() => onToggleOne(cl.id)}
            onSelectChain={onSelectChain}
          />
        ))}
      </div>
    )
  }

  const allSelected = selectMode && cls.every(cl => selected.has(cl.id))
  const someSelected = cls.some(cl => selected.has(cl.id))
  const preview = cls.slice(0, STACK_PREVIEW_COUNT)
  const remaining = cls.length - preview.length

  return (
    <>
      <div
        className="card cl-card"
        onClick={() => selectMode ? onToggleChainSelected(rootId) : setShowAll(true)}
        style={{
          cursor: 'pointer',
          borderLeft: '3px solid var(--accent)',
          outline: someSelected ? '2px solid var(--accent)' : 'none',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: '0.75rem' }}>
          {selectMode && (
            <input
              type="checkbox"
              checked={allSelected}
              onChange={() => onToggleChainSelected(rootId)}
              onClick={e => e.stopPropagation()}
              style={{ accentColor: 'var(--accent)', width: '1rem', height: '1rem', marginTop: '0.2rem', flexShrink: 0 }}
            />
          )}
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.4rem' }}>
              <Icon glyph={Glyphs.changes} size={14} style={{ opacity: 0.7 }} />
              <span style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--accent)' }}>Stack of {cls.length} CLs</span>
              <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.4rem' }}>
                <button className="btn btn-ghost btn-sm" onClick={e => { e.stopPropagation(); onToggleExpand() }}>
                  Expand stack
                </button>
                <button className="btn btn-primary btn-sm" onClick={e => { e.stopPropagation(); onMerge() }}>
                  Merge stack
                </button>
              </div>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem' }}>
              {preview.map(cl => (
                <div
                  key={cl.id}
                  style={{
                    fontSize: '0.8rem',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <span style={{ color: 'var(--t1)' }}>CL #{cl.id}: {cl.title}</span>
                  <span style={{ color: 'var(--t3)' }}>
                    {' '}| {cl.target_branch} | {cl.author} | {humanTime(cl.updated_at)}
                  </span>
                </div>
              ))}
              {remaining > 0 && (
                <div style={{ fontSize: '0.78rem', color: 'var(--t3)' }}>+ {remaining} more</div>
              )}
            </div>
          </div>
        </div>
      </div>
      {/* Sibling, not a child of the clickable card above — a React portal's
          content still bubbles synthetic events up the REACT tree (not just
          the DOM tree) to its JSX ancestors regardless of where createPortal
          renders it, so nesting it inside the card would fire the card's own
          onClick (toggling expand/select) on every click inside the modal. */}
      {showAll && <StackModal cls={cls} onClose={() => setShowAll(false)} onMerge={() => { setShowAll(false); onMerge() }} />}
    </>
  )
}

// The stack view: every CL's description, latest build statuses and reviews in
// one scrollable list. "View full CL" opens a CL's detail page; "Review stack"
// walks the stack one CL at a time (StackReviewModal); "Merge stack" hands off
// to MergeStackModal.
function StackModal({ cls: initial, onClose, onMerge }: { cls: CL[]; onClose: () => void; onMerge: () => void }) {
  // The members as last fetched, so a CL that merges, picks up a patchset or
  // goes stale updates here too, not just its builds and reviews.
  const [cls, setCls] = useState<CL[]>(initial)
  const [buildsByCL, setBuildsByCL] = useState<Map<number, Build[]>>(new Map())
  const [reviewsByCL, setReviewsByCL] = useState<Map<number, Review[]>>(new Map())
  const [loadedIds,  setLoadedIds]  = useState<Set<number>>(new Set())
  const [reviewing, setReviewing] = useState(false)
  // Set when a review pass ends; drives the banner and the highlighted Merge button.
  const [reviewed, setReviewed] = useState<Map<number, StepResult> | null>(null)
  const me = getUsername()
  const openCLs = cls.filter(cl => cl.status === 'open')

  function load() {
    return Promise.allSettled(initial.map(cl =>
      Promise.all([api.getCL(cl.id), api.getBuilds(cl.id), api.getReviews(cl.id)])
        .then(([fresh, builds, reviews]) => ({ id: cl.id, fresh, builds, reviews }))))
      .then(results => {
        const map = new Map<number, Build[]>()
        const revs = new Map<number, Review[]>()
        const fresh = new Map<number, CL>()
        const loaded = new Set<number>()
        for (const r of results) {
          if (r.status === 'fulfilled') {
            map.set(r.value.id, latestBuildPerTool(r.value.builds))
            revs.set(r.value.id, r.value.reviews)
            fresh.set(r.value.id, r.value.fresh)
            loaded.add(r.value.id)
          }
        }
        return { map, revs, fresh, loaded }
      })
  }

  function apply({ map, revs, fresh, loaded }: Awaited<ReturnType<typeof load>>) {
    setBuildsByCL(map)
    setReviewsByCL(revs)
    setLoadedIds(loaded)
    // A CL that failed to load this time keeps what it last showed.
    setCls(prev => prev.map(cl => fresh.get(cl.id) ?? cl))
  }

  useEffect(() => {
    let cancelled = false
    load().then(r => { if (!cancelled) apply(r) })
    return () => { cancelled = true }
    // cls is the stack's member list, stable for the modal's lifetime — refetching
    // if the array identity changes on a parent re-render isn't needed here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Keep it live while it's open: builds start and finish, reviews and
  // patchsets land. Polled rather than one /cl/{id}/events stream per CL —
  // crit is served over HTTP/1.1, where a stack's worth of open streams would
  // use up the browser's six connections to the host. Same interval as
  // MergeStackModal; paused while the tab is hidden or a review is open.
  useEffect(() => {
    if (reviewing) return
    let cancelled = false
    const t = setInterval(() => {
      if (document.hidden) return
      load().then(r => { if (!cancelled) apply(r) })
    }, STACK_POLL_MS)
    return () => { cancelled = true; clearInterval(t) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reviewing])

  function finishReview(results: Map<number, StepResult>) {
    setReviewing(false)
    setReviewed(results)
    load().then(apply)
  }

  const allLGTM = !!reviewed && openCLs.length > 0 && openCLs.every(cl => reviewed.get(cl.id) === 'lgtm')
  const needsWork = reviewed ? openCLs.filter(cl => reviewed.get(cl.id) === 'needs_work').length : 0
  const skipped = reviewed ? openCLs.filter(cl => !reviewed.has(cl.id) || reviewed.get(cl.id) === 'skipped').length : 0
  const own = reviewed ? openCLs.filter(cl => reviewed.get(cl.id) === 'own').length : 0
  const ownNote = own ? ` ${own === 1 ? '1 is yours and needs' : `${own} are yours and need`} another reviewer's LGTM.` : ''

  useEffect(() => {
    if (reviewing) return // the review dialog owns Escape while it's open
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, reviewing])

  if (reviewing) {
    return <StackReviewModal cls={cls} onClose={() => { setReviewing(false); load().then(apply) }} onFinished={finishReview} />
  }

  const body = (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 50,
        background: 'rgba(0,0,0,0.55)',
        backdropFilter: 'blur(8px)',
        WebkitBackdropFilter: 'blur(8px)',
        display: 'grid', placeItems: 'center', padding: '2rem',
        // Without a fixed track, one long unbreakable line (a CL title, a diff
        // line) widens the column past the viewport and the card centers off-screen.
        gridTemplateColumns: 'minmax(0, 1fr)',
      }}
      onClick={onClose}
    >
      <div
        className="card"
        style={{ width: 'min(720px, 100%)', maxHeight: '85vh', display: 'flex', flexDirection: 'column', padding: 0 }}
        onClick={e => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', padding: '0.85rem 1rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
          <span style={{ fontWeight: 700 }}>Stack of {cls.length} CLs</span>
          {openCLs.length > 0 && (
            <button className={`btn btn-sm ${allLGTM ? 'btn-ghost' : 'btn-primary'}`} style={{ marginLeft: 'auto' }} onClick={() => setReviewing(true)}>
              Review stack
            </button>
          )}
          <button
            className={`btn btn-sm ${allLGTM || openCLs.length === 0 ? 'btn-primary' : 'btn-ghost'}${allLGTM ? ' btn-attention' : ''}`}
            style={openCLs.length === 0 ? { marginLeft: 'auto' } : undefined}
            onClick={onMerge}
          >
            Merge stack
          </button>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose} aria-label="Close">✕</button>
        </div>

        {reviewed && (
          <div className="stack-review-banner" style={{ padding: '0.6rem 1rem', fontSize: '0.84rem', borderBottom: 'var(--hairline) solid var(--border)', color: allLGTM ? 'var(--accent)' : 'var(--t2)' }}>
            {allLGTM
              ? `All ${openCLs.length} CLs have your LGTM. Merge the stack to land them all at once.`
              : `Review pass done${needsWork ? `: ${needsWork} need${needsWork === 1 ? 's' : ''} work` : ''}${skipped ? `${needsWork ? ',' : ':'} ${skipped} skipped` : ''}.${ownNote} Merge stack lands whatever is ready, bottom-up.`}
          </div>
        )}

        <div style={{ overflow: 'auto', padding: '0.5rem' }}>
          {cls.map(cl => {
            const builds = buildsByCL.get(cl.id) ?? []
            const isLoaded = loadedIds.has(cl.id)
            return (
              <div key={cl.id} style={{ padding: '0.75rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.4rem' }}>
                  <CLStatusBadge status={cl.status} />
                  <span style={{ fontSize: '0.78rem', color: 'var(--t3)' }}>#{cl.id}</span>
                  <span style={{ fontWeight: 600, fontSize: '0.9rem' }}>{cl.title}</span>
                  <button
                    className="btn btn-primary btn-sm"
                    style={{ marginLeft: 'auto', flexShrink: 0 }}
                    onClick={() => navigate(`/cl/${cl.id}`)}
                  >
                    View full CL
                  </button>
                </div>
                <div className="cl-meta" style={{ marginBottom: '0.5rem' }}>
                  <span style={{ color: 'var(--accent)' }}>{cl.target_branch}</span>
                  <span>·</span>
                  <UserLink username={cl.author} />
                  <span>·</span>
                  <span>{humanTime(cl.updated_at)}</span>
                  {(reviewsByCL.get(cl.id) ?? []).map(r => (
                    <span key={r.id} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}>
                      <span>·</span>
                      <VerdictBadge verdict={r.verdict} />
                      {(r.author ?? r.reviewer) === me ? 'you' : (r.author ?? r.reviewer)}
                    </span>
                  ))}
                </div>
                {cl.description && (
                  <p style={{ fontSize: '0.85rem', color: 'var(--t2)', whiteSpace: 'pre-wrap', margin: '0 0 0.5rem' }}>
                    {cl.description}
                  </p>
                )}
                {!isLoaded && (
                  <div style={{ display: 'flex', justifyContent: 'center', padding: '0.5rem' }}><Spinner size="1em" /></div>
                )}
                {isLoaded && builds.length === 0 && (
                  <p style={{ fontSize: '0.8rem', color: 'var(--t3)', margin: 0 }}>No builds yet.</p>
                )}
                {isLoaded && builds.length > 0 && (
                  <div className="build-list">
                    {builds.map(b => (
                      <div key={b.id} className="build-list-item" style={{ cursor: 'default' }}>
                        <BuildBadge status={b.status} />
                        <span style={{ flex: 1, textAlign: 'left', fontSize: '0.82rem' }}>{b.tool_name}</span>
                        <span style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>{buildDuration(b)}</span>
                      </div>
                    ))}
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

function CLCard({ cl, chain, showRepo, selectMode, selectable, selected, onToggle, onSelectChain }: {
  cl: CL
  chain?: ChainInfo
  showRepo: boolean
  selectMode: boolean
  selectable: boolean
  selected: boolean
  onToggle: () => void
  onSelectChain: (rootId: number) => void
}) {
  const inSelect = selectMode && selectable
  const inChain = !!chain && chain.size > 1
  return (
    <div
      className="card cl-card"
      onClick={() => inSelect ? onToggle() : navigate(`/cl/${cl.id}`)}
      style={{
        cursor: selectMode && !selectable ? 'default' : 'pointer',
        opacity: selectMode && !selectable ? 0.5 : 1,
        outline: selected ? '2px solid var(--accent)' : 'none',
        borderLeft: inChain ? '3px solid var(--accent)' : undefined,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: '0.75rem' }}>
        {inSelect && (
          <input
            type="checkbox"
            checked={selected}
            onChange={onToggle}
            onClick={e => e.stopPropagation()}
            style={{ accentColor: 'var(--accent)', width: '1rem', height: '1rem', marginTop: '0.2rem', flexShrink: 0 }}
          />
        )}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem' }}>
            <CLStatusBadge status={cl.status} />
            <span style={{ fontSize: '0.75rem', color: 'var(--t3)' }}>#{cl.id}</span>
            {inChain && (
              <span style={{ fontSize: '0.75rem', color: 'var(--accent)' }}>
                stack {chain!.position + 1}/{chain!.size}
              </span>
            )}
            {inChain && selectMode && chain!.position === 0 && (
              <button
                className="btn btn-ghost btn-sm"
                style={{ padding: '0 0.4rem', fontSize: '0.75rem' }}
                onClick={e => { e.stopPropagation(); onSelectChain(chain!.rootId) }}
              >
                select stack
              </button>
            )}
          </div>
          <div className="cl-title">{cl.title}</div>
          <div className="cl-meta">
            {showRepo && <span style={{ color: 'var(--t2)' }}>{cl.repo}</span>}
            {showRepo && <span>→</span>}
            <span style={{ color: 'var(--accent)' }}>{cl.target_branch}</span>
            <span>·</span>
            <UserLink username={cl.author} />
            <span>·</span>
            <span>{humanTime(cl.updated_at)}</span>
          </div>
        </div>
        {cl.reviewers && cl.reviewers.length > 0 && (
          <div style={{ display: 'flex', gap: '0.25rem', flexShrink: 0, marginTop: '0.2rem' }}>
            {cl.reviewers.slice(0, 3).map(r => (
              <UserLink key={r} username={r}>
                <div className="avatar" style={{ width: '1.5rem', height: '1.5rem', fontSize: '0.55rem' }} title={r}>
                  {r.slice(0, 2).toUpperCase()}
                </div>
              </UserLink>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
