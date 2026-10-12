import { getToken, getServer } from './auth'

export interface CL {
  id: number
  change_id: string
  repo: string
  target_branch: string
  author: string
  title: string
  description?: string
  status: 'open' | 'merged' | 'abandoned'
  reviewers?: string[]
  created_at: string
  updated_at: string
  // Only present on list responses (GET /cl) — see model.CL in the Go backend.
  latest_commit_sha?: string
  latest_base_sha?: string
}

// Opaque, per-user view preferences — the server stores/returns this blob
// without interpreting it, so new keys can be added here without a server
// change. Add new per-page preference groups the same way as cl_list.
export interface ViewSettings {
  cl_list?: {
    sort?: 'updated' | 'id_desc' | 'id_asc'
    status?: string
    repo?: string
  }
  merge?: {
    // Merge stack: merge the ready CLs straight away instead of showing the plan first.
    skip_confirm?: boolean
  }
}

// Whether a CL can be merged now, and if not, why (GET /cl/{id}/readiness).
export interface Readiness {
  ready: boolean
  waiting: boolean   // only blocked by builds still running
  reasons: string[]
  base_cl?: number   // the open CL this one sits on, if that's still unmerged
}

export interface Patchset {
  id: number
  cl_id: number
  number: number
  commit_sha: string
  base_sha: string
  ref: string
  message?: string
  created_at: string
}

export interface Review {
  id: number
  cl_id: number
  ps_id: number
  author: string
  reviewer: string
  verdict: 'lgtm' | 'needs_work' | 'comment'
  created_at: string
}

export interface Comment {
  id: number
  cl_id: number
  ps_id: number
  author: string
  file_path: string
  line?: number
  body: string
  parent_id?: number
  created_at: string
}

export interface Build {
  id: number
  cl_id: number
  ps_id: number
  tool_name: string
  status: 'queued' | 'running' | 'pass' | 'fail'
  log_path?: string
  node?: string
  started_at?: string
  ended_at?: string
}

export interface BuildNode {
  id: number
  name: string
  status: 'active' | 'draining'
  os: string
  arch: string
  labels: string[]
  version: string
  max_concurrent: number
  in_flight: number
  last_seen?: string
  created_at: string
}

export interface FileDiff {
  old_path: string
  new_path: string
  unified: string
}

export interface PatchsetFileSummary {
  path: string
  old_path?: string
  additions: number
  deletions: number
  status: 'added' | 'deleted' | 'renamed' | 'modified'
  changed_since_prev: boolean
}

export interface PatchsetSummary {
  patchset: number
  prev_patchset: number
  commit_sha: string
  files: PatchsetFileSummary[]
  total_additions: number
  total_deletions: number
}

export interface User {
  username: string
  email: string
  ssh_key?: string
  role: 'admin' | 'submitter' | 'reviewer' | 'readonly'
  created_at: string
}

export interface Tool {
  name: string
  required: boolean
  depends_on: string[]
}

export interface Repo {
  name: string
  clone_url: string
  description: string
  default_branch: string
}

export interface RepoDetail {
  name: string
  clone_url: string
  description: string
  default_branch: string
  require_other_lgtm?: boolean        // this repo's own setting
  require_other_lgtm_global?: boolean // on server-wide
}

export interface LangStat {
  language: string
  color: string
  bytes: number
  percentage: number
}

export interface Contributor {
  name: string
  commits: number
}

export interface DayCount {
  date: string
  count: number
}

export interface RepoSize {
  bytes: number
}

export interface ReadmeResult {
  content: string
  path: string
}

export interface OwnersPerFile {
  glob: string
  owners: string[]
}

export interface OwnersFile {
  no_parent: boolean
  owners: string[]
  reviewers: string[]
  per_file: OwnersPerFile[] | null
  required_lgtms: number
}

export interface OwnersData {
  branch: string
  raw: string
  parsed: OwnersFile
}

export interface Settings {
  public_url: string
  site_title: string
  default_palette?: string
  require_other_lgtm?: string // "true" | "false"
}

export interface RepoAccess {
  restricted: boolean
  users: string[]
  roles: string[]
}

export interface TreeEntry {
  name: string
  type: 'blob' | 'tree'
  sha: string
  size?: number
  mode: string
  modified_at?: string
}

export interface TreeResult {
  entries: TreeEntry[]
  ref: string
  path: string
  empty?: boolean
}

export interface FileResult {
  content: string
  binary: boolean
  size: number
  path: string
  ref: string
}

export interface BranchesResult {
  branches: string[]
  default: string
}

export interface CommitSummary {
  sha: string
  subject: string
  author: string
  email: string
  time: string
}

export interface ProfileComment {
  id: number
  cl_id: number
  repo: string
  cl_title: string
  file_path: string
  line?: number
  body: string
  created_at: string
}

export interface ProfileReview {
  cl_id: number
  repo: string
  cl_title: string
  verdict: 'lgtm' | 'needs_work' | 'comment'
  created_at: string
}

export interface ProfileActivity {
  user: User
  cls: CL[]
  comments: ProfileComment[]
  reviews: {
    lgtm: ProfileReview[]
    needs_work: ProfileReview[]
    comment: ProfileReview[]
  }
}

class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function req<T>(method: string, path: string, body?: unknown, headers?: Record<string, string>): Promise<T> {
  const token = getToken()
  const server = getServer()
  const res = await fetch(server + path, {
    method,
    credentials: 'include', // send the session cookie (web SSO)
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}), // CLI break-glass token
      ...headers,
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const text = await res.text()
    throw new APIError(res.status, text.trim() || res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  // cl
  listCLs: (params: { status?: string; author?: string; reviewer?: string; mine?: boolean; assigned?: boolean } = {}) => {
    const q = new URLSearchParams()
    if (params.status)    q.set('status',   params.status)
    if (params.mine)      q.set('mine',     '1')
    if (params.assigned)  q.set('assigned', '1')
    if (params.author)    q.set('author',   params.author)
    if (params.reviewer)  q.set('reviewer', params.reviewer)
    return req<CL[]>('GET', `/cl?${q}`)
  },
  getCL:         (id: number) => req<CL>('GET', `/cl/${id}`),
  getPatchsets:  (id: number) => req<Patchset[]>('GET', `/cl/${id}/patchsets`),
  getReviews:    (id: number) => req<Review[]>('GET', `/cl/${id}/reviews`),
  getComments:   (id: number) => req<Comment[]>('GET', `/cl/${id}/comments`),
  getBuilds:     (id: number) => req<Build[]>('GET', `/cl/${id}/builds`),
  getReadiness:  (id: number) => req<Readiness>('GET', `/cl/${id}/readiness`),
  getDiff:       (id: number, ps?: number) =>
    req<FileDiff[]>('GET', ps ? `/cl/${id}/diff/${ps}` : `/cl/${id}/diff`),
  getPatchsetSummary: (id: number, ps: number) =>
    req<PatchsetSummary>('GET', `/cl/${id}/patchsets/${ps}/summary`),
  getDiffText:   (id: number, ps?: number) => {
    const path = ps ? `/cl/${id}/diff/${ps}` : `/cl/${id}/diff`
    return req<string>('GET', path, undefined, { Accept: 'text/plain' })
  },
  postReview:    (id: number, verdict: string) =>
    req<void>('POST', `/cl/${id}/review`, { verdict }),
  postComment:   (id: number, body: { file?: string; line?: number; body: string; parent_id?: number }) =>
    // server expects `file_path`; map from the caller's `file`
    req<{ id: number }>('POST', `/cl/${id}/comment`, {
      file_path: body.file,
      line: body.line,
      body: body.body,
      parent_id: body.parent_id,
    }),
  submitCL:      (id: number, strategy = 'fast-forward', force = false) =>
    req<{ merged_sha: string }>('POST', `/cl/${id}/submit`, { strategy, force_submit: force }),
  abandonCL:     (id: number) => req<void>('POST', `/cl/${id}/abandon`),
  assignReviewer:(id: number, username: string) =>
    req<void>('POST', `/cl/${id}/assign`, { username }),
  triggerBuilds: (id: number, tools?: string[]) =>
    req<void>('POST', `/cl/${id}/builds/trigger`, tools?.length ? { tools } : {}),
  listTools: () => req<Tool[]>('GET', '/tools'),
  cliToken: () => req<{ token: string; username: string }>('POST', '/auth/cli-token'),
  cancelBuild:   (clID: number, buildID: number) =>
    req<void>('POST', `/cl/${clID}/builds/${buildID}/cancel`),

  // repos
  listRepos:  () => req<Repo[]>('GET', '/repo'),
  createRepo: (name: string) => req<Repo>('POST', '/repo', { name }),
  deleteRepo: (name: string) => req<void>('DELETE', `/repo/${name}`),
  getRepo:    (name: string) => req<RepoDetail>('GET', `/repo/${name}`),
  updateRepo: (name: string, body: { description?: string; default_branch?: string; require_other_lgtm?: boolean }) =>
    req<RepoDetail>('PATCH', `/repo/${name}`, body),
  getOwners:  (name: string) => req<OwnersData>('GET', `/repo/${name}/owners`),
  updateOwners: (name: string, raw: string) =>
    req<void>('PUT', `/repo/${name}/owners`, { raw }),
  getRepoAccess:    (name: string) => req<RepoAccess>('GET', `/repo/${name}/access`),
  updateRepoAccess: (name: string, body: RepoAccess) =>
    req<void>('PUT', `/repo/${name}/access`, body),

  // file browser
  getRepoBranches: (name: string) => req<BranchesResult>('GET', `/repo/${name}/branches`),
  getRepoCommits:  (name: string, ref: string, n = 20) =>
    req<CommitSummary[]>('GET', `/repo/${name}/commits?ref=${encodeURIComponent(ref)}&n=${n}`),
  getRepoTree: (name: string, ref: string, path: string) =>
    req<TreeResult>('GET', `/repo/${name}/tree?ref=${encodeURIComponent(ref)}&path=${encodeURIComponent(path)}`),
  getRepoFile: (name: string, ref: string, path: string) =>
    req<FileResult>('GET', `/repo/${name}/file?ref=${encodeURIComponent(ref)}&path=${encodeURIComponent(path)}`),
  editRepoFile: (name: string, body: { ref: string; path: string; content: string; message: string }) =>
    req<{ cl_id: number }>('POST', `/repo/${name}/edit`, body),
  repoRawUrl: (name: string, ref: string, path: string) => {
    const server = getServer()
    return `${server}/repo/${name}/raw?ref=${encodeURIComponent(ref)}&path=${encodeURIComponent(path)}`
  },
  repoArchiveUrl: (name: string, ref: string) => {
    const server = getServer()
    return `${server}/repo/${name}/archive?ref=${encodeURIComponent(ref)}&format=zip`
  },
  // repo metadata
  getRepoReadme:       (name: string, ref: string) =>
    req<ReadmeResult>('GET', `/repo/${name}/readme?ref=${encodeURIComponent(ref)}`),
  getRepoLanguages:    (name: string) =>
    req<LangStat[]>('GET', `/repo/${name}/languages`),
  getRepoContributors: (name: string) =>
    req<Contributor[]>('GET', `/repo/${name}/contributors`),
  getRepoActivity:     (name: string, days = 90) =>
    req<DayCount[]>('GET', `/repo/${name}/activity?days=${days}`),
  getRepoSize:         (name: string) =>
    req<RepoSize>('GET', `/repo/${name}/size`),
  // activity feed & contributions
  getActivityFeed:     () => req<CL[]>('GET', '/activity'),
  getContributions:    (username: string, days = 365) =>
    req<DayCount[]>('GET', `/user/${username}/contributions?days=${days}`),
  // follow system
  isFollowing:    (username: string) =>
    req<{ following: boolean }>('GET', `/user/${username}/follow`),
  follow:         (username: string) => req<void>('POST', `/user/${username}/follow`),
  unfollow:       (username: string) => req<void>('DELETE', `/user/${username}/follow`),
  getFollowing:   (username: string) => req<string[]>('GET', `/user/${username}/following`),
  getFollowers:   (username: string) => req<string[]>('GET', `/user/${username}/followers`),
  // stars
  isStarred:       (name: string) => req<{ starred: boolean }>('GET', `/repo/${name}/star`),
  starRepo:        (name: string) => req<void>('POST', `/repo/${name}/star`),
  unstarRepo:      (name: string) => req<void>('DELETE', `/repo/${name}/star`),
  getRepoStarCount:(name: string) => req<{ count: number }>('GET', `/repo/${name}/stars`),
  getUserStarred:  (username: string) => req<Repo[]>('GET', `/user/${username}/starred`),
  listRealmRoles: () => req<string[]>('GET', '/admin/roles'),

  // users
  listUsers:  () => req<User[]>('GET', '/user'),
  getUser:    (username: string) => req<User>('GET', `/user/${username}`),
  getUserActivity: (username: string) => req<ProfileActivity>('GET', `/user/${username}/activity`),
  deleteUser: (username: string) => req<void>('DELETE', `/user/${username}`),
  syncUsers:  () => req<void>('POST', '/admin/sync-users'),

  // settings
  getSettings:    () => req<Settings>('GET', '/settings'),
  updateSettings: (body: Partial<Settings>) => req<void>('PUT', '/settings', body),

  // per-user view preferences (sort/filter choices, ...) — opaque to the server
  getViewSettings:    () => req<ViewSettings>('GET', '/me/settings'),
  updateViewSettings: (body: ViewSettings) => req<void>('PUT', '/me/settings', body),

  // identity mapping (git mailmap) — full admins only
  getMailmap:    () => req<{ content: string }>('GET', '/admin/mailmap'),
  listNodes:       () => req<BuildNode[]>('GET', '/admin/nodes'),
  createJoinToken: () => req<{ token: string; expires_at: string }>('POST', '/admin/nodes/join-tokens'),
  drainNode:       (name: string) => req<void>('POST', `/admin/nodes/${encodeURIComponent(name)}/drain`),
  resumeNode:      (name: string) => req<void>('POST', `/admin/nodes/${encodeURIComponent(name)}/resume`),
  removeNode:      (name: string) => req<void>('DELETE', `/admin/nodes/${encodeURIComponent(name)}`),
  updateMailmap: (content: string) => req<void>('PUT', '/admin/mailmap', { content }),

  // background gallery
  listBackgrounds:  () => req<string[]>('GET', '/backgrounds'),
  deleteBackground: (name: string) => req<void>('DELETE', `/admin/backgrounds/${encodeURIComponent(name)}`),
  uploadBackground: async (blob: Blob, filename: string): Promise<{ url: string }> => {
    const form = new FormData()
    form.append('file', blob, filename)
    const token = getToken()
    const res = await fetch(getServer() + '/admin/backgrounds', {
      method: 'POST',
      credentials: 'include',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
    })
    if (!res.ok) throw new APIError(res.status, (await res.text()).trim() || res.statusText)
    return res.json()
  },
}

export { APIError }
