import { useEffect, useState, useRef } from 'react'
import { api } from '../lib/api'
import { navigate } from '../lib/router'
import { shortSHA, humanTime } from '../lib/theme'
import { Spinner } from '../components/Spinner'
import { Icon, Glyphs, BranchIcon } from '../components/icons'
import { renderMarkdown } from '../lib/markdown'
import { getUsername, isAdmin } from '../lib/auth'
import type {
  TreeEntry, FileResult, CommitSummary, BranchesResult,
  LangStat, Contributor, DayCount, RepoDetail,
} from '../lib/api'

interface Props {
  name: string
  urlRef?: string
  urlPath?: string
  initialMode?: 'file' | 'tree'
}

// ── icons ──────────────────────────────────────────────────────────────────────

function FolderIcon() {
  return <Icon glyph={Glyphs.folder} size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
}

function FileIcon() {
  return <Icon glyph={Glyphs.file} size={15} style={{ color: 'var(--t3)', flexShrink: 0 }} />
}

function ChevronRight() {
  return <Icon glyph={Glyphs.chevronRight} size={12} />
}

// ── helpers ────────────────────────────────────────────────────────────────────

function humanSize(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + ' MB'
  return (n / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
}

function fileDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function extOf(path: string): string {
  const base = path.split('/').pop() ?? path
  const i = base.lastIndexOf('.')
  return i >= 0 ? base.slice(i + 1).toLowerCase() : ''
}

type PreviewKind = 'text' | 'markdown' | 'image' | 'video' | 'audio' | 'pdf' | 'binary'

function previewKind(path: string, binary: boolean): PreviewKind {
  const ext = extOf(path)
  // Check extension first — some types (svg, pdf) are text but render as non-text
  if (['jpg','jpeg','png','gif','webp','bmp','ico','svg'].includes(ext)) return 'image'
  if (['mp4','webm','ogv','mov','avi'].includes(ext)) return 'video'
  if (['mp3','ogg','wav','flac','m4a','aac'].includes(ext)) return 'audio'
  if (ext === 'pdf') return 'pdf'
  if (binary) return 'binary'
  if (['md','mdx','markdown'].includes(ext)) return 'markdown'
  return 'text'
}

function encPath(parts: string[]) {
  return parts.map(encodeURIComponent).join('/')
}

// ── breadcrumb ─────────────────────────────────────────────────────────────────

function Breadcrumb({ name, path, onNavigate }: {
  name: string; ref: string; path: string; onNavigate: (p: string) => void
}) {
  const parts = path ? path.split('/') : []
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '0.85rem', flexWrap: 'wrap' }}>
      <button className="btn btn-ghost btn-sm" style={{ padding: '2px 6px' }}
        onClick={() => onNavigate('')}>{name}</button>
      {parts.map((p, i) => (
        <span key={i} style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          <ChevronRight />
          {i < parts.length - 1 ? (
            <button className="btn btn-ghost btn-sm" style={{ padding: '2px 6px' }}
              onClick={() => onNavigate(parts.slice(0, i + 1).join('/'))}>
              {p}
            </button>
          ) : (
            <span style={{ color: 'var(--t1)', fontWeight: 600 }}>{p}</span>
          )}
        </span>
      ))}
    </div>
  )
}

// ── file view ──────────────────────────────────────────────────────────────────

function FileView({ name, ref, path, onEdit }: {
  name: string; ref: string; path: string
  onBack: () => void; onEdit: () => void
}) {
  const [file, setFile] = useState<FileResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true); setError('')
    api.getRepoFile(name, ref, path)
      .then(setFile)
      .catch(e => setError(e?.message ?? 'Failed to load file'))
      .finally(() => setLoading(false))
  }, [name, ref, path])

  if (loading) return <div className="panel" style={{ padding: '2rem', textAlign: 'center' }}><Spinner /></div>
  if (error) return <p className="panel" style={{ color: 'var(--red)' }}>{error}</p>
  if (!file) return null

  const rawUrl = api.repoRawUrl(name, ref, path)
  const kind = previewKind(path, file.binary)
  const lines = (kind === 'text' || kind === 'markdown') ? file.content.split('\n') : []

  return (
    <div>
      <div className="panel" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem', flexWrap: 'wrap', gap: '0.5rem', padding: '0.5rem 0.9rem' }}>
        <div style={{ fontSize: '0.75rem', color: 'var(--t3)', fontFamily: 'monospace' }}>
          {file.binary ? `Binary · ${humanSize(file.size)}` : `${lines.length} lines · ${humanSize(file.size)}`}
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <a href={rawUrl} download className="btn btn-ghost btn-sm"><Icon glyph={Glyphs.download} size={13} /> Raw</a>
          {kind === 'text' || kind === 'markdown' ? (
            <button className="btn btn-ghost btn-sm" onClick={onEdit}><Icon glyph={Glyphs.edit} size={13} /> Edit</button>
          ) : null}
        </div>
      </div>

      {kind === 'image' && (
        <div style={{ padding: '2rem', textAlign: 'center', background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)' }}>
          <img src={rawUrl} alt={path} style={{ maxWidth: '100%', maxHeight: '600px', borderRadius: 0 }} />
        </div>
      )}

      {kind === 'video' && (
        <div style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '1rem' }}>
          <video controls style={{ width: '100%', maxHeight: '480px' }}>
            <source src={rawUrl} />
          </video>
        </div>
      )}

      {kind === 'audio' && (
        <div style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '2rem', textAlign: 'center' }}>
          <audio controls style={{ width: '100%', maxWidth: '480px' }}>
            <source src={rawUrl} />
          </audio>
        </div>
      )}

      {kind === 'pdf' && (
        <div style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', overflow: 'hidden' }}>
          <embed src={rawUrl} type="application/pdf" width="100%" height="720px" />
          <div style={{ padding: '0.5rem', textAlign: 'center', fontSize: '0.8rem', color: 'var(--t3)' }}>
            <a href={rawUrl} download style={{ color: 'var(--accent)' }}>Download PDF</a> if preview is not showing.
          </div>
        </div>
      )}

      {kind === 'markdown' && (
        <div className="md-body" style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '1.5rem 2rem' }}
          dangerouslySetInnerHTML={{ __html: renderMarkdown(file.content) }} />
      )}

      {kind === 'binary' && (
        <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--t3)', background: 'var(--s2)', borderRadius: 'var(--r)', border: 'var(--hairline) solid var(--border)' }}>
          <div style={{ fontSize: '1.5rem', marginBottom: '0.5rem' }}>⬡</div>
          Binary file · <a href={rawUrl} download style={{ color: 'var(--accent)' }}>Download</a>
        </div>
      )}

      {kind === 'text' && (
        <div style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', overflow: 'auto' }}>
          <table style={{ borderCollapse: 'collapse', width: '100%', fontSize: '0.78rem', fontFamily: 'monospace' }}>
            <tbody>
              {lines.map((line, i) => (
                <tr key={i} style={{ lineHeight: '1.5' }}>
                  <td style={{ padding: '0 12px 0 8px', color: 'var(--t3)', userSelect: 'none', textAlign: 'right', minWidth: '3rem', borderRight: 'var(--hairline) solid var(--border)', verticalAlign: 'top' }}>
                    {i + 1}
                  </td>
                  <td style={{ padding: '0 8px 0 12px', whiteSpace: 'pre', color: 'var(--t1)', verticalAlign: 'top' }}>
                    {line}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

// ── edit view ──────────────────────────────────────────────────────────────────

type EditViewMode = 'edit' | 'split' | 'preview'

function EditView({ name, ref, path, initialContent, onCancel, onSuccess }: {
  name: string; ref: string; path: string; initialContent: string
  onCancel: () => void; onSuccess: (clID: number) => void
}) {
  const [content, setContent] = useState(initialContent)
  const [message, setMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const isMarkdown = ['md', 'mdx', 'markdown'].includes(extOf(path))
  const [viewMode, setViewMode] = useState<EditViewMode>(isMarkdown ? 'split' : 'edit')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!message.trim()) { setError('Commit message is required'); return }
    setSaving(true); setError('')
    try {
      const { cl_id } = await api.editRepoFile(name, { ref, path, content, message: message.trim() })
      onSuccess(cl_id)
    } catch (err: any) {
      setError(err?.message ?? 'Failed to propose change')
    } finally {
      setSaving(false)
    }
  }

  const editorStyle: React.CSSProperties = {
    flex: 1, minHeight: '460px', background: 'var(--s2)', border: 'var(--hairline) solid var(--border)',
    borderRadius: 'var(--r)', color: 'var(--t1)', fontFamily: 'monospace', fontSize: '0.78rem',
    lineHeight: '1.55', padding: '12px', resize: 'none', outline: 'none', boxSizing: 'border-box',
    width: '100%',
  }

  return (
    <form onSubmit={handleSubmit}>
      {/* toolbar */}
      <div className="panel" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem', flexWrap: 'wrap', padding: '0.5rem 0.9rem' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--t3)', flex: 1 }}>
          Editing on <code style={{ fontFamily: 'monospace', color: 'var(--t2)' }}>{ref}</code>
        </span>
        {isMarkdown && (
          <div style={{ display: 'flex', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', overflow: 'hidden' }}>
            {(['edit', 'split', 'preview'] as const).map(m => (
              <button key={m} type="button"
                onClick={() => setViewMode(m)}
                style={{
                  padding: '3px 10px', fontSize: '0.75rem', border: 'none', cursor: 'pointer',
                  background: viewMode === m ? 'var(--accent)' : 'var(--s2)',
                  color: viewMode === m ? '#fff' : 'var(--t2)',
                  fontWeight: viewMode === m ? 600 : 400,
                  borderRight: m !== 'preview' ? 'var(--hairline) solid var(--border)' : 'none',
                  transition: 'background 120ms',
                }}>
                {m.charAt(0).toUpperCase() + m.slice(1)}
              </button>
            ))}
          </div>
        )}
        <button type="button" className="btn btn-ghost btn-sm" onClick={onCancel}>Cancel</button>
      </div>

      {/* editor / preview area */}
      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'stretch' }}>
        {viewMode !== 'preview' && (
          <textarea value={content} onChange={e => setContent(e.target.value)}
            style={{ ...editorStyle, minHeight: '460px' }}
            spellCheck={false} />
        )}
        {isMarkdown && viewMode !== 'edit' && (
          <div className="md-body"
            style={{
              flex: 1, minHeight: '460px', background: 'var(--s2)', border: 'var(--hairline) solid var(--border)',
              borderRadius: 'var(--r)', padding: '1rem 1.5rem', overflowY: 'auto',
              boxSizing: 'border-box',
            }}
            dangerouslySetInnerHTML={{ __html: renderMarkdown(content) }}
          />
        )}
      </div>

      {/* commit form */}
      <div className="panel" style={{ marginTop: '0.75rem' }}>
        <label style={{ fontSize: '0.8rem', color: 'var(--t2)', display: 'block', marginBottom: '0.35rem' }}>Commit message</label>
        <input className="input" value={message} onChange={e => setMessage(e.target.value)} placeholder="Describe your change…" required />
        <p style={{ fontSize: '0.72rem', color: 'var(--t3)', marginTop: '0.3rem' }}>A new change list (CL) will be opened for review. Merge it from the CL page.</p>
        {error && <p style={{ color: 'var(--red)', fontSize: '0.8rem', marginTop: '0.5rem' }}>{error}</p>}
        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem' }}>
          <button type="submit" className="btn btn-primary" disabled={saving}>{saving ? <Spinner size="0.85em" /> : 'Propose changes'}</button>
          <button type="button" className="btn btn-ghost" onClick={onCancel}>Cancel</button>
        </div>
      </div>
    </form>
  )
}

// ── sidebar ────────────────────────────────────────────────────────────────────

function Sparkline({ data, days }: { data: DayCount[]; days: number }) {
  const W = 220, H = 48, PAD = 2
  if (data.length === 0) {
    return <div style={{ height: H, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--t3)', fontSize: '0.72rem' }}>No commits</div>
  }

  // build a day-indexed map for the last N days
  const now = new Date()
  const buckets: number[] = Array(days).fill(0)
  const dayMap = new Map(data.map(d => [d.date, d.count]))
  for (let i = 0; i < days; i++) {
    const d = new Date(now)
    d.setDate(d.getDate() - (days - 1 - i))
    const key = d.toISOString().slice(0, 10)
    buckets[i] = dayMap.get(key) ?? 0
  }

  const max = Math.max(...buckets, 1)
  const xStep = (W - PAD * 2) / (days - 1)
  const yScale = (H - PAD * 2) / max

  const pts = buckets.map((v, i) => [PAD + i * xStep, H - PAD - v * yScale] as [number, number])
  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ')
  const areaPath = `${linePath} L${pts[pts.length - 1]![0].toFixed(1)},${H - PAD} L${pts[0]![0].toFixed(1)},${H - PAD} Z`

  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: H }}>
      <defs>
        <linearGradient id="spark-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.25" />
          <stop offset="100%" stopColor="var(--accent)" stopOpacity="0.02" />
        </linearGradient>
      </defs>
      <path d={areaPath} fill="url(#spark-fill)" />
      <path d={linePath} fill="none" stroke="var(--accent)" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

function LangBar({ langs }: { langs: LangStat[] }) {
  if (langs.length === 0) return null
  const top = langs.slice(0, 8)
  return (
    <div>
      <div style={{ display: 'flex', height: '6px', borderRadius: 0, overflow: 'hidden', gap: '1px', marginBottom: '0.5rem' }}>
        {top.map(l => (
          <div key={l.language} style={{ flex: l.percentage, background: l.color, minWidth: '1px' }} title={`${l.language} ${l.percentage.toFixed(1)}%`} />
        ))}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem 0.75rem' }}>
        {top.map(l => (
          <div key={l.language} style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '0.72rem', color: 'var(--t2)' }}>
            <span style={{ width: 8, height: 8, borderRadius: 0, background: l.color, flexShrink: 0, display: 'inline-block' }} />
            {l.language} <span style={{ color: 'var(--t3)' }}>{l.percentage.toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function DownloadPanel({ name, ref, cloneUrl }: { name: string; ref: string; cloneUrl: string }) {
  const [open, setOpen] = useState(false)
  const gitUrl = cloneUrl.replace(/^https?:\/\//, 'git://')
  const zipUrl = api.repoArchiveUrl(name, ref)

  return (
    <div style={{ position: 'relative' }}>
      <button
        className="btn btn-ghost btn-sm"
        style={{ width: '100%', justifyContent: 'center', gap: '6px' }}
        onClick={() => setOpen(v => !v)}
      >
        <Icon glyph={Glyphs.download} size={13} />
        Clone / Download
      </button>
      {open && (
        <div style={{ marginTop: '0.5rem', background: 'var(--s1)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          <div>
            <div style={{ fontSize: '0.68rem', color: 'var(--t3)', marginBottom: '2px', fontWeight: 600 }}>HTTPS</div>
            <div style={{ display: 'flex', gap: '4px' }}>
              <input readOnly value={cloneUrl} className="input" style={{ fontSize: '0.72rem', fontFamily: 'monospace', flex: 1, padding: '4px 8px' }}
                onClick={e => (e.target as HTMLInputElement).select()} />
              <button className="btn btn-ghost btn-sm" onClick={() => navigator.clipboard.writeText(cloneUrl)} title="Copy"><Icon glyph={Glyphs.copy} size={13} /></button>
            </div>
          </div>
          <div>
            <div style={{ fontSize: '0.68rem', color: 'var(--t3)', marginBottom: '2px', fontWeight: 600 }}>GIT</div>
            <div style={{ display: 'flex', gap: '4px' }}>
              <input readOnly value={gitUrl} className="input" style={{ fontSize: '0.72rem', fontFamily: 'monospace', flex: 1, padding: '4px 8px' }}
                onClick={e => (e.target as HTMLInputElement).select()} />
              <button className="btn btn-ghost btn-sm" onClick={() => navigator.clipboard.writeText(gitUrl)} title="Copy"><Icon glyph={Glyphs.copy} size={13} /></button>
            </div>
          </div>
          <a href={zipUrl} download className="btn btn-ghost btn-sm" style={{ textAlign: 'center', justifyContent: 'center' }}>
            <Icon glyph={Glyphs.download} size={13} /> Download ZIP
          </a>
        </div>
      )}
    </div>
  )
}

interface SidebarData {
  langs: LangStat[]
  contributors: Contributor[]
  activity: DayCount[]
  sizeBytes: number
}

function Sidebar({ name, ref, detail }: { name: string; ref: string; detail: RepoDetail }) {
  const [data, setData] = useState<SidebarData | null>(null)

  useEffect(() => {
    Promise.all([
      api.getRepoLanguages(name),
      api.getRepoContributors(name),
      api.getRepoActivity(name, 90),
      api.getRepoSize(name),
    ]).then(([langs, contributors, activity, size]) => {
      setData({ langs, contributors, activity, sizeBytes: size.bytes })
    }).catch(() => setData({ langs: [], contributors: [], activity: [], sizeBytes: 0 }))
  }, [name])

  const DAYS = 90

  // One panel for the whole rail — clone links, size, languages, commit graph
  // and contributors are all bare text against the page background otherwise.
  return (
    <aside className="panel" style={{ width: '260px', flexShrink: 0 }}>
      {/* download */}
      <DownloadPanel name={name} ref={ref} cloneUrl={detail.clone_url} />

      {/* size */}
      {data && data.sizeBytes > 0 && (
        <div className="panel-sep" style={{ fontSize: '0.75rem', color: 'var(--t3)', display: 'flex', justifyContent: 'space-between' }}>
          <span>Size on disk</span>
          <span style={{ color: 'var(--t2)' }}>{humanSize(data.sizeBytes)}</span>
        </div>
      )}

      {/* languages */}
      {data && data.langs.length > 0 && (
        <div className="panel-sep">
          <div style={{ fontSize: '0.72rem', fontWeight: 600, color: 'var(--t3)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '0.5rem' }}>Languages</div>
          <LangBar langs={data.langs} />
        </div>
      )}

      {/* activity sparkline */}
      {data && (
        <div className="panel-sep">
          <div style={{ fontSize: '0.72rem', fontWeight: 600, color: 'var(--t3)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '0.4rem' }}>
            Commits · last {DAYS}d
          </div>
          <Sparkline data={data.activity} days={DAYS} />
        </div>
      )}

      {/* contributors */}
      {data && data.contributors.length > 0 && (
        <div className="panel-sep">
          <div style={{ fontSize: '0.72rem', fontWeight: 600, color: 'var(--t3)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '0.5rem' }}>Contributors</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
            {data.contributors.slice(0, 6).map(c => (
              <div key={c.name} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', justifyContent: 'space-between' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', minWidth: 0 }}>
                  <div className="avatar" style={{ width: '20px', height: '20px', fontSize: '0.55rem', borderRadius: 0, flexShrink: 0 }}>
                    {c.name.slice(0, 2).toUpperCase()}
                  </div>
                  <span style={{ fontSize: '0.78rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.name}</span>
                </div>
                <span style={{ fontSize: '0.85rem', color: 'var(--t3)', flexShrink: 0 }}>{c.commits}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </aside>
  )
}

// ── readme ─────────────────────────────────────────────────────────────────────

function ReadmePanel({ name, ref }: { name: string; ref: string }) {
  const [content, setContent] = useState<string | null>(null)

  useEffect(() => {
    api.getRepoReadme(name, ref)
      .then(r => setContent(r.content))
      .catch(() => setContent(null))
  }, [name, ref])

  if (!content) return null

  // Label lives on the box rather than floating above it, so it is never bare
  // text over the page background.
  return (
    <div style={{ marginTop: '1.5rem', background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', overflow: 'hidden' }}>
      <div style={{ fontSize: '0.72rem', fontWeight: 600, color: 'var(--t3)', textTransform: 'uppercase', letterSpacing: '0.05em', padding: '0.6rem 1rem', borderBottom: 'var(--hairline) solid var(--border)', display: 'flex', alignItems: 'center', gap: '6px' }}>
        <Icon glyph={Glyphs.readme} size={13} />
        README
      </div>
      <div className="md-body" style={{ padding: '1.5rem 2rem' }}
        dangerouslySetInnerHTML={{ __html: renderMarkdown(content) }} />
    </div>
  )
}

// ── main page ──────────────────────────────────────────────────────────────────

export function RepoBrowsePage({ name, urlRef, urlPath = '', initialMode = 'tree' }: Props) {
  const [branchInfo, setBranchInfo] = useState<BranchesResult | null>(null)
  const [detail, setDetail] = useState<RepoDetail | null>(null)
  const [isOwner, setIsOwner] = useState(false)
  const [editingDesc, setEditingDesc] = useState(false)
  const [descDraft, setDescDraft] = useState('')
  const [savingDesc, setSavingDesc] = useState(false)
  const [ref, setRef] = useState(urlRef ?? '')
  const [path, setPath] = useState(urlPath)
  const [entries, setEntries] = useState<TreeEntry[] | null>(null)
  const [file, setFile] = useState<FileResult | null>(null)
  const [commits, setCommits] = useState<CommitSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [mode, setMode] = useState<'tree' | 'file' | 'edit'>('tree')
  const [editContent, setEditContent] = useState('')
  const [tab, setTab] = useState<'files' | 'commits'>('files')
  const [showBranchPicker, setShowBranchPicker] = useState(false)
  const branchRef = useRef<HTMLDivElement>(null)
  // tracks whether the one-time initial file-load (for /blob/ URLs) has been done
  const didInitialFileLoad = useRef(false)

  useEffect(() => {
    function handler(e: MouseEvent) {
      if (branchRef.current && !branchRef.current.contains(e.target as Node)) {
        setShowBranchPicker(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  useEffect(() => {
    api.getRepoBranches(name).then(b => {
      setBranchInfo(b)
      if (!ref && b.default) setRef(b.default)
    }).catch(() => setBranchInfo({ branches: [], default: '' }))
    api.getRepo(name).then(setDetail).catch(() => {})
    Promise.all([
      api.getOwners(name),
    ]).then(([owners]) => {
      const me = getUsername()
      setIsOwner(isAdmin() || owners.parsed.owners.includes(me))
    }).catch(() => setIsOwner(isAdmin()))
  }, [name])

  useEffect(() => {
    if (branchInfo === null) return
    if (!ref) { setLoading(false); return }

    // On initial load from a /blob/ URL, load the file directly instead of listing a tree.
    if (!didInitialFileLoad.current && initialMode === 'file' && urlPath) {
      didInitialFileLoad.current = true
      setMode('file')
      setLoading(true); setError('')
      api.getRepoFile(name, ref, urlPath)
        .then(f => setFile(f))
        .catch(e => setError(e?.message ?? 'Failed to load file'))
        .finally(() => setLoading(false))
      api.getRepoCommits(name, ref, 10).then(setCommits).catch(() => {})
      return
    }
    didInitialFileLoad.current = true

    setLoading(true); setError(''); setEntries(null); setFile(null); setMode('tree')
    Promise.all([
      api.getRepoTree(name, ref, path),
      api.getRepoCommits(name, ref, 10),
    ]).then(([tree, log]) => {
      setEntries(tree.entries)
      setCommits(log)
    }).catch(e => setError(e?.message ?? 'Failed to load repository'))
      .finally(() => setLoading(false))
  }, [name, ref, path, branchInfo])

  function openFile(entry: TreeEntry) {
    if (entry.type === 'tree') {
      setPath(path ? path + '/' + entry.name : entry.name)
      return
    }
    const filePath = path ? path + '/' + entry.name : entry.name
    navigate(`/repo/${name}/blob/${encodeURIComponent(ref)}/${encPath(filePath.split('/'))}`)
    setLoading(true); setError('')
    api.getRepoFile(name, ref, filePath)
      .then(f => { setFile(f); setMode('file') })
      .catch(e => setError(e?.message ?? 'Failed to load file'))
      .finally(() => setLoading(false))
  }

  function enterEdit() {
    if (!file) return
    setEditContent(file.content)
    setMode('edit')
  }

  const atRoot = mode === 'tree' && !path
  const currentFilePath = file?.path ?? ''

  async function saveDesc() {
    setSavingDesc(true)
    try {
      const updated = await api.updateRepo(name, { description: descDraft.trim() })
      setDetail(updated)
      setEditingDesc(false)
    } finally {
      setSavingDesc(false)
    }
  }

  return (
    <div>
      {/* Repo identity: name, branch, description and breadcrumb are one block
          rather than three loose rows floating on the page background. */}
      <div className="panel" style={{ marginBottom: '1.25rem' }}>
        {/* header */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <button className="btn btn-ghost btn-sm" onClick={() => navigate('/repo')}
            style={{ display: 'flex', alignItems: 'center', gap: '5px', color: 'var(--t3)' }}>
            <Icon glyph={Glyphs.chevronLeft} size={13} />
            Repositories
          </button>
          <span style={{ color: 'var(--border)', fontSize: '1rem' }}>/</span>
          <h1 className="page-title" style={{ margin: 0 }}>{name}</h1>
          {/* branch picker */}
          <div ref={branchRef} style={{ position: 'relative' }}>
            <button className="btn btn-ghost btn-sm" style={{ fontFamily: 'monospace', gap: '6px' }}
              onClick={() => setShowBranchPicker(v => !v)}>
              <BranchIcon size={13} />
              {ref || '…'}
              <Icon glyph={Glyphs.chevronDown} size={11} />
            </button>
            {showBranchPicker && (
              <div style={{ position: 'absolute', top: '100%', left: 0, zIndex: 50, background: 'var(--s1)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '4px', minWidth: '180px', marginTop: '4px', boxShadow: '0 4px 12px rgba(0,0,0,0.4)' }}>
                {(branchInfo?.branches ?? []).map(b => (
                  <button key={b} className="btn btn-ghost btn-sm"
                    style={{ width: '100%', justifyContent: 'flex-start', fontFamily: 'monospace', fontWeight: b === ref ? 700 : 400, color: b === ref ? 'var(--accent)' : undefined }}
                    onClick={() => { setRef(b); setPath(''); setShowBranchPicker(false) }}>
                    {b === ref && '✓ '}{b}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* description */}
        {detail && (editingDesc || detail.description || isOwner) && (
          <div className="panel-sep" style={{ display: 'flex', alignItems: 'flex-start', gap: '0.5rem' }}>
            {editingDesc ? (
              <>
                <textarea
                  autoFocus
                  value={descDraft}
                  onChange={e => setDescDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); saveDesc() } if (e.key === 'Escape') setEditingDesc(false) }}
                  rows={2}
                  style={{ flex: 1, resize: 'vertical', background: 'var(--s2)', border: 'var(--hairline) solid var(--border-input)', borderRadius: 'var(--r)', color: 'var(--t1)', padding: '0.4rem 0.6rem', fontSize: '0.96rem', fontFamily: 'var(--font)', outline: 'none' }}
                />
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', flexShrink: 0 }}>
                  <button className="btn btn-primary btn-sm" onClick={saveDesc} disabled={savingDesc}>
                    {savingDesc ? <Spinner size="0.8em" /> : 'Save'}
                  </button>
                  <button className="btn btn-ghost btn-sm" onClick={() => setEditingDesc(false)}>Cancel</button>
                </div>
              </>
            ) : (
              <>
                <div style={{ flex: 1 }}>
                  {detail.description
                    ? <h2 style={{ margin: 0, fontSize: '1.1rem', fontWeight: 600, color: 'var(--t1)', lineHeight: 1.4, letterSpacing: '-0.01em' }}>{detail.description}</h2>
                    : <span style={{ fontSize: '0.85rem', color: 'var(--t3)', fontStyle: 'italic' }}>No description</span>
                  }
                </div>
                {isOwner && (
                  <button className="btn btn-ghost btn-icon btn-sm" title="Edit description"
                    style={{ flexShrink: 0, color: 'var(--t3)' }}
                    onClick={() => { setDescDraft(detail.description ?? ''); setEditingDesc(true) }}>
                    <Icon glyph={Glyphs.edit} size={14} />
                  </button>
                )}
              </>
            )}
          </div>
        )}

        {/* breadcrumb */}
        {(mode !== 'tree' || path) && (
          <div className="panel-sep">
            <Breadcrumb name={name} ref={ref}
              path={mode === 'tree' ? path : currentFilePath}
              onNavigate={p => {
                setPath(p); setMode('tree'); setFile(null)
                navigate(`/repo/${name}/tree/${encodeURIComponent(ref)}${p ? '/' + encPath(p.split('/')) : ''}`)
              }} />
          </div>
        )}
      </div>

      {mode === 'edit' && file ? (
        <EditView name={name} ref={ref} path={file.path} initialContent={editContent}
          onCancel={() => setMode('file')} onSuccess={id => navigate(`/cl/${id}`)} />
      ) : mode === 'file' && file ? (
        <FileView name={name} ref={ref} path={file.path}
          onBack={() => { setMode('tree'); setFile(null) }} onEdit={enterEdit} />
      ) : (
        <div style={{ display: 'flex', gap: '2rem', alignItems: 'flex-start' }}>
          {/* main column */}
          <div style={{ flex: 1, minWidth: 0 }}>
            {/* Tabs and the list they switch are one surface: the tab strip is a
                header row on the same card, and rows below carry no background
                of their own so nothing reads as a card inside a card. */}
            <div className="panel" style={{ padding: 0, overflow: 'hidden' }}>
              <div style={{ display: 'flex', gap: '4px', padding: '0.35rem 0.5rem', borderBottom: 'var(--hairline) solid var(--border)' }}>
                {(['files', 'commits'] as const).map(t => (
                  <button key={t} className="btn btn-ghost btn-sm"
                    style={{ borderRadius: 'var(--r-sm)', borderBottom: `2px solid ${tab === t ? 'var(--accent)' : 'transparent'}`, color: tab === t ? 'var(--t1)' : 'var(--t3)', fontWeight: tab === t ? 600 : 400, background: tab === t ? 'var(--s2)' : 'transparent' }}
                    onClick={() => setTab(t)}>
                    {t.charAt(0).toUpperCase() + t.slice(1)}
                  </button>
                ))}
              </div>

              {loading && <div style={{ padding: '2rem', textAlign: 'center' }}><Spinner /></div>}
              {error && <p style={{ color: 'var(--red)', padding: '1rem' }}>{error}</p>}

              {!loading && !error && (branchInfo?.branches ?? []).length === 0 && (
                <div style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)' }}>
                  <div style={{ fontSize: '0.9rem', marginBottom: '0.5rem' }}>Empty repository</div>
                  <div style={{ fontSize: '0.8rem' }}>No commits yet — push to get started.</div>
                </div>
              )}

              {!loading && !error && !!ref && tab === 'files' && (
                entries && entries.length === 0 ? (
                  <div style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)' }}>
                    <div style={{ fontSize: '0.9rem' }}>Empty directory</div>
                  </div>
                ) : (
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <tbody>
                      {path && (
                        <tr style={{ borderBottom: 'var(--hairline) solid var(--border)' }}>
                          <td style={{ padding: '8px 14px' }} colSpan={2}>
                            <button className="btn btn-ghost btn-sm" style={{ fontFamily: 'var(--font)', color: 'var(--t3)' }}
                              onClick={() => setPath(path.split('/').slice(0, -1).join('/'))}>
                              ..
                            </button>
                          </td>
                        </tr>
                      )}
                      {(entries ?? []).map((entry, i, all) => (
                        <tr key={entry.name}
                          style={{ borderBottom: i === all.length - 1 ? undefined : 'var(--hairline) solid var(--border)', cursor: 'pointer' }}
                          onClick={() => openFile(entry)}
                          onMouseEnter={e => (e.currentTarget.style.background = 'var(--s3)')}
                          onMouseLeave={e => (e.currentTarget.style.background = '')}>
                          <td style={{ padding: '8px 14px', width: '100%' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                              {entry.type === 'tree' ? <FolderIcon /> : <FileIcon />}
                              <span style={{ fontSize: '0.96rem', fontFamily: 'var(--font)', color: entry.type === 'tree' ? 'var(--accent)' : 'var(--t1)' }}>
                                {entry.name}
                              </span>
                            </div>
                          </td>
                          <td style={{ padding: '8px 14px', textAlign: 'right', whiteSpace: 'nowrap' }}>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '0.75rem' }}>
                              {entry.modified_at && (() => {
                                const rel = humanTime(entry.modified_at)
                                const isRelative = rel.includes('ago') || rel === 'just now'
                                return (
                                  <span style={{ fontSize: '0.82rem', color: 'var(--t3)', fontFamily: 'var(--font)' }}>
                                    {isRelative ? `${rel} · ${fileDate(entry.modified_at)}` : rel}
                                  </span>
                                )
                              })()}
                              {entry.type === 'blob' && entry.size != null && (
                                <span style={{ fontSize: '0.82rem', color: 'var(--t3)', fontFamily: 'var(--font)' }}>
                                  {humanSize(entry.size)}
                                </span>
                              )}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )
              )}

              {!loading && !error && !!ref && tab === 'commits' && (
                <>
                  {(commits ?? []).length === 0 && (
                    <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--t3)' }}>No commits yet</div>
                  )}
                  {(commits ?? []).map((c, i, all) => (
                    <div key={c.sha} style={{ display: 'flex', alignItems: 'center', gap: '1rem', padding: '10px 14px', flexWrap: 'wrap', borderBottom: i === all.length - 1 ? undefined : 'var(--hairline) solid var(--border)' }}>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ fontSize: '0.85rem', fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.subject}</div>
                        <div style={{ fontSize: '0.72rem', color: 'var(--t3)', marginTop: '2px' }}>
                          {c.author} · {new Date(c.time).toLocaleDateString()}
                        </div>
                      </div>
                      <code style={{ fontSize: '0.72rem', color: 'var(--t3)', fontFamily: 'monospace', flexShrink: 0 }}>{shortSHA(c.sha)}</code>
                    </div>
                  ))}
                </>
              )}
            </div>

            {!loading && !error && !!ref && tab === 'files' && atRoot && (
              <ReadmePanel name={name} ref={ref} />
            )}
          </div>

          {/* sidebar — only at repo root */}
          {atRoot && detail && (
            <Sidebar name={name} ref={ref} detail={detail} />
          )}
        </div>
      )}
    </div>
  )
}
