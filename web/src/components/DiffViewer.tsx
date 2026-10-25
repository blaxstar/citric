import { Fragment, useEffect, useState } from 'react'
import { UserLink } from './UserLink'
import { humanTime } from '../lib/theme'
import { api } from '../lib/api'
import type { FileDiff, Comment } from '../lib/api'
import { Icon, Glyphs } from './icons'
import { renderMarkdown } from '../lib/markdown'

interface DiffViewerProps {
  diffs: FileDiff[]
  comments: Comment[]
  onComment?: (file: string, line: number, body: string) => Promise<void>
  focusCommentID?: number | null
  /** Repo name + the patchset's commit SHA, so a markdown file's *rendered*
   * preview can fetch its full content (a diff only ever has the changed
   * hunks, not the whole file) via the same repo-file endpoint the repo
   * browser already uses. Preview is unavailable without both. */
  repoName?: string
  commitSha?: string
}

function isMarkdownPath(path: string): boolean {
  const base = path.split('/').pop() ?? path
  const i = base.lastIndexOf('.')
  const ext = i >= 0 ? base.slice(i + 1).toLowerCase() : ''
  return ext === 'md' || ext === 'mdx' || ext === 'markdown'
}

interface ParsedLine {
  type: 'context' | 'add' | 'del' | 'hunk'
  oldLine?: number
  newLine?: number
  content: string
}

// git header prefixes that should not be rendered as diff content
const GIT_HEADERS = [
  'diff --git', 'index ', 'deleted file', 'new file', 'old mode',
  'new mode', 'Binary files', 'similarity ', 'rename ', '--- ', '+++ ',
]

function parseDiff(unified: string): ParsedLine[] {
  const lines: ParsedLine[] = []
  let oldLine = 0
  let newLine = 0

  for (const raw of unified.split('\n')) {
    if (!raw) continue

    if (raw.startsWith('@@')) {
      const m = raw.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/)
      if (m) {
        oldLine = parseInt(m[1], 10)
        newLine = parseInt(m[2], 10)
      }
      lines.push({ type: 'hunk', content: raw })
      continue
    }

    if (GIT_HEADERS.some(h => raw.startsWith(h))) continue

    if (raw.startsWith('+')) {
      lines.push({ type: 'add', newLine: newLine++, content: raw.slice(1) })
    } else if (raw.startsWith('-')) {
      lines.push({ type: 'del', oldLine: oldLine++, content: raw.slice(1) })
    } else {
      lines.push({ type: 'context', oldLine: oldLine++, newLine: newLine++, content: raw.slice(1) })
    }
  }
  return lines
}

function DiffFile({
  diff,
  comments,
  onComment,
  focusCommentID,
  repoName,
  commitSha,
}: {
  diff: FileDiff
  comments: Comment[]
  onComment?: (file: string, line: number, body: string) => Promise<void>
  focusCommentID?: number | null
  repoName?: string
  commitSha?: string
}) {
  const [collapsed, setCollapsed] = useState(false)
  const [commentLine, setCommentLine] = useState<number | null>(null)
  const [commentBody, setCommentBody] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [openThreads, setOpenThreads] = useState<Set<number>>(new Set())
  const [showPreview, setShowPreview] = useState(false)
  const [previewContent, setPreviewContent] = useState<string | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [previewLoading, setPreviewLoading] = useState(false)

  const parsed = parseDiff(diff.unified)
  // go-git uses empty string (not /dev/null) for the missing side of new/deleted files
  const filename = (diff.new_path && diff.new_path !== '/dev/null')
    ? diff.new_path
    : (diff.old_path || '')

  let adds = 0, dels = 0
  for (const l of parsed) {
    if (l.type === 'add') adds++
    if (l.type === 'del') dels++
  }

  const commentsByLine = new Map<number, Comment[]>()
  for (const c of comments) {
    if (c.file_path === filename && c.line != null) {
      const arr = commentsByLine.get(c.line) ?? []
      arr.push(c)
      commentsByLine.set(c.line, arr)
    }
  }

  // Each line number is hosted by exactly one row so a modified line (a del +
  // an add sharing the same number) doesn't render its comments twice. New-side
  // rows (add/context) win; a deleted line only hosts if its number has no
  // new-side counterpart (i.e. the line was truly removed).
  const hostIdx = new Map<number, number>()
  parsed.forEach((l, i) => {
    if ((l.type === 'add' || l.type === 'context') && l.newLine != null && !hostIdx.has(l.newLine)) {
      hostIdx.set(l.newLine, i)
    }
  })
  parsed.forEach((l, i) => {
    if (l.type === 'del' && l.oldLine != null && !hostIdx.has(l.oldLine)) {
      hostIdx.set(l.oldLine, i)
    }
  })

  // When an activity card jumps to a comment in this file, expand its thread
  // and scroll to it.
  useEffect(() => {
    if (focusCommentID == null) return
    const target = comments.find(c => c.id === focusCommentID && c.file_path === filename && c.line != null)
    if (!target) return
    setCollapsed(false)
    setOpenThreads(prev => new Set(prev).add(target.line!))
    const t = setTimeout(() => {
      const el = document.getElementById(`c-${focusCommentID}`)
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        el.classList.add('comment-flash')
        setTimeout(() => el.classList.remove('comment-flash'), 1600)
      }
    }, 60)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusCommentID])

  function toggleThread(line: number) {
    setOpenThreads(prev => {
      const next = new Set(prev)
      next.has(line) ? next.delete(line) : next.add(line)
      return next
    })
  }

  async function submitComment() {
    if (!onComment || commentLine === null || !commentBody.trim()) return
    setSubmitting(true)
    try {
      await onComment(filename, commentLine, commentBody)
      setOpenThreads(prev => new Set(prev).add(commentLine))
      setCommentBody('')
      setCommentLine(null)
    } finally {
      setSubmitting(false)
    }
  }

  const isNew = !diff.old_path || diff.old_path === '/dev/null'
  const isDel = !diff.new_path || diff.new_path === '/dev/null'
  // A deleted file has nothing left in the tree to fetch and render; a
  // preview only makes sense for a path that still exists at this commit.
  const canPreview = isMarkdownPath(filename) && !isDel && !!repoName && !!commitSha

  useEffect(() => {
    if (!showPreview || previewContent !== null || previewLoading || !canPreview) return
    setPreviewLoading(true)
    setPreviewError('')
    api.getRepoFile(repoName!, commitSha!, filename)
      .then(f => setPreviewContent(f.content))
      .catch(e => setPreviewError(e?.message ?? 'Failed to load file'))
      .finally(() => setPreviewLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showPreview])

  return (
    <div className="diff-file">
      <div className="diff-file-header">
        <span className="diff-file-name">
          {isNew && <span style={{ color: 'var(--green)', marginRight: '0.4em' }}>+</span>}
          {isDel && <span style={{ color: 'var(--red)',   marginRight: '0.4em' }}>−</span>}
          {filename}
        </span>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
          <span style={{ color: 'var(--green)', fontSize: '0.75rem' }}>+{adds}</span>
          <span style={{ color: 'var(--red)',   fontSize: '0.75rem' }}>−{dels}</span>
          {canPreview && (
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => setShowPreview(p => !p)}
              title={showPreview ? 'Show raw diff' : 'Show rendered markdown'}
            >
              {showPreview ? 'Diff' : 'Preview'}
            </button>
          )}
          <button
            className="btn btn-ghost btn-sm btn-icon"
            onClick={() => setCollapsed(c => !c)}
            title={collapsed ? 'Expand' : 'Collapse'}
          >
            <Icon glyph={collapsed ? Glyphs.chevronRight : Glyphs.chevronDown} size={14} />
          </button>
        </div>
      </div>

      {!collapsed && showPreview && canPreview && (
        <div style={{ padding: '0.5rem' }}>
          {previewLoading && (
            <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--t3)' }}>Loading…</div>
          )}
          {previewError && (
            <p style={{ color: 'var(--red)', fontSize: '0.85rem', padding: '0.5rem' }}>{previewError}</p>
          )}
          {previewContent !== null && !previewLoading && !previewError && (
            <div className="md-body"
              style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '1.5rem 2rem' }}
              dangerouslySetInnerHTML={{ __html: renderMarkdown(previewContent) }} />
          )}
        </div>
      )}

      {!collapsed && !showPreview && (
        <div className="diff-scroll">
        <table className="diff-table">
          <tbody>
            {parsed.map((line, idx) => {
              if (line.type === 'hunk') {
                return (
                  <tr key={idx} className="diff-hunk">
                    <td colSpan={4} className="diff-hunk">{line.content}</td>
                  </tr>
                )
              }

              // candidate = the line number this row would comment on; isHost =
              // this row is the single owner of that number's comments/form.
              const candidate = (line.type === 'del' ? line.oldLine : line.newLine) ?? null
              const hostLine = candidate != null && hostIdx.get(candidate) === idx ? candidate : null
              const lineComments = hostLine != null ? (commentsByLine.get(hostLine) ?? []) : []
              const threadOpen = hostLine != null && openThreads.has(hostLine)
              const isFormHere = commentLine != null && hostIdx.get(commentLine) === idx

              return (
                <Fragment key={idx}>
                  <tr className={`diff-${line.type}`}>
                    <td
                      className="diff-ln"
                      title="Click to comment"
                      onClick={() => {
                        if (!onComment || candidate == null) return
                        setCommentLine(commentLine === candidate ? null : candidate)
                      }}
                    >
                      {line.type === 'context' ? '' : (line.oldLine ?? '')}
                    </td>
                    <td className="diff-ln">{line.newLine ?? ''}</td>
                    <td className="diff-sign">
                      {line.type === 'add' ? '+' : line.type === 'del' ? '−' : ' '}
                    </td>
                    <td className="diff-code">
                      {lineComments.length > 0 && (
                        <button
                          className={`diff-comment-bubble${threadOpen ? ' active' : ''}`}
                          title={`${lineComments.length} comment${lineComments.length > 1 ? 's' : ''}`}
                          onClick={() => hostLine != null && toggleThread(hostLine)}
                        >
                          💬 {lineComments.length}
                        </button>
                      )}
                      {line.content}
                    </td>
                  </tr>

                  {lineComments.length > 0 && threadOpen && (
                    <tr>
                      <td colSpan={4}>
                        <div className="diff-comment-thread">
                          {lineComments.map(c => (
                            <div key={c.id} id={`c-${c.id}`} className="diff-comment">
                              <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                                <UserLink username={c.author} style={{ fontWeight: 700, fontSize: '0.8rem', color: 'var(--accent)' }} />
                                <span style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>#{c.id}</span>
                                <span style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>
                                  · line {c.line} · {humanTime(c.created_at)}
                                </span>
                              </div>
                              <p style={{ fontSize: '0.85rem', color: 'var(--t1)', margin: '0.2rem 0 0' }}>
                                {c.body}
                              </p>
                            </div>
                          ))}
                        </div>
                      </td>
                    </tr>
                  )}

                  {isFormHere && onComment && (
                    <tr>
                      <td colSpan={4}>
                        <div className="diff-comment-form">
                          <textarea
                            className="input"
                            rows={3}
                            placeholder={`Comment on line ${commentLine}…`}
                            value={commentBody}
                            onChange={e => setCommentBody(e.target.value)}
                            style={{ resize: 'vertical', marginBottom: '0.5rem' }}
                            autoFocus
                          />
                          <div style={{ display: 'flex', gap: '0.5rem' }}>
                            <button
                              className="btn btn-primary btn-sm"
                              onClick={submitComment}
                              disabled={submitting || !commentBody.trim()}
                            >
                              {submitting ? 'Posting…' : 'Post comment'}
                            </button>
                            <button
                              className="btn btn-ghost btn-sm"
                              onClick={() => { setCommentLine(null); setCommentBody('') }}
                            >
                              Cancel
                            </button>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
        </div>
      )}
    </div>
  )
}

export function DiffViewer({ diffs, comments, onComment, focusCommentID, repoName, commitSha }: DiffViewerProps) {
  if (diffs.length === 0) {
    return (
      <div className="empty panel">
        <Icon glyph={Glyphs.changes} size={38} style={{ opacity: 0.5, marginBottom: '0.75rem' }} />
        <div className="empty-title">No changes</div>
      </div>
    )
  }

  return (
    <div>
      {diffs.map((d, i) => (
        <DiffFile
          key={i}
          diff={d}
          comments={comments}
          onComment={onComment}
          focusCommentID={focusCommentID}
          repoName={repoName}
          commitSha={commitSha}
        />
      ))}
    </div>
  )
}
