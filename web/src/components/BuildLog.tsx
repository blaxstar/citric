import { useEffect, useRef, useState } from 'react'
import { getToken, getServer } from '../lib/auth'
import { BuildBadge } from './Badge'
import { Spinner } from './Spinner'
import type { Build } from '../lib/api'

interface BuildLogProps {
  clID: number
  build: Build
}

function classifyLine(line: string): string {
  const l = line.toLowerCase()
  if (l.includes('error') || l.includes('fail') || l.startsWith('[error]') || l.startsWith('[cancelled]')) return 'build-log-line-err'
  if (l.includes('warning') || l.includes('warn'))                         return 'build-log-line-warn'
  if (l.startsWith('[ok]') || l.includes('passed') || l.includes('success')) return 'build-log-line-ok'
  if (l.startsWith('[') || l.startsWith('=='))                              return 'build-log-line-dim'
  return ''
}

export function BuildLog({ clID, build }: BuildLogProps) {
  const [lines, setLines] = useState<string[]>([])
  const [finalStatus, setFinalStatus] = useState<string | null>(
    build.status === 'pass' || build.status === 'fail' ? build.status : null,
  )
  const [streaming, setStreaming] = useState(false)
  // The log box scrolls itself; the page never does. It follows new output
  // only while you're at its bottom, so scrolling up to read (or to reach the
  // page's controls) isn't fought by the stream.
  const logRef = useRef<HTMLDivElement>(null)
  const [follow, setFollow] = useState(true)
  const [unseen, setUnseen] = useState(false)
  const abortRef  = useRef<AbortController | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    abortRef.current = ctrl
    setStreaming(true)

    const token  = getToken()
    const server = getServer()
    const url    = `${server}/cl/${clID}/builds/${build.id}/stream`

    fetch(url, {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: 'text/event-stream',
      },
      signal: ctrl.signal,
    }).then(async res => {
      if (!res.ok || !res.body) { setStreaming(false); return }
      const reader  = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      let eventType = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const parts = buf.split('\n')
        buf = parts.pop() ?? ''

        for (const part of parts) {
          if (part.startsWith('event: ')) {
            eventType = part.slice(7).trim()
          } else if (part.startsWith('data: ')) {
            const data = part.slice(6)
            if (eventType === 'done') {
              setFinalStatus(data)
              setStreaming(false)
              reader.cancel()
              return
            }
            setLines(ls => [...ls, data])
            eventType = ''
          }
        }
      }
      setStreaming(false)
    }).catch(() => setStreaming(false))

    return () => ctrl.abort()
  }, [clID, build.id])

  const seenLines = useRef(0)
  useEffect(() => {
    const el = logRef.current
    if (!el) return
    if (follow) el.scrollTop = el.scrollHeight
    else if (lines.length > seenLines.current) setUnseen(true)
    seenLines.current = lines.length
  }, [lines, follow])

  function onLogScroll() {
    const el = logRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24
    setFollow(atBottom)
    if (atBottom) setUnseen(false)
  }

  function jumpToLatest() {
    const el = logRef.current
    if (el) el.scrollTop = el.scrollHeight
    setFollow(true)
    setUnseen(false)
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.75rem' }}>
        <span style={{ fontWeight: 700 }}>{build.tool_name}</span>
        {build.node && <span style={{ fontSize: '0.78rem', color: 'var(--t3)' }}>on {build.node}</span>}
        {streaming
          ? <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.8rem', color: 'var(--yellow)' }}>
              <Spinner size="0.9em" /> streaming
            </span>
          : finalStatus
            ? <BuildBadge status={finalStatus} />
            : <BuildBadge status={build.status} />
        }
        {unseen && (
          <button className="btn btn-ghost btn-sm" style={{ marginLeft: 'auto' }} onClick={jumpToLatest}>
            ↓ Jump to latest
          </button>
        )}
      </div>

      <div className="build-log" ref={logRef} onScroll={onLogScroll}>
        {lines.length === 0 && !finalStatus && (
          <span className="build-log-line-dim">Waiting for output…</span>
        )}
        {lines.map((line, i) => (
          <div key={i} className={classifyLine(line)}>
            {line || '​'}
          </div>
        ))}
        {finalStatus && (
          <div
            className={finalStatus === 'pass' ? 'build-log-line-ok' : 'build-log-line-err'}
            style={{ marginTop: '0.5rem', fontWeight: 700 }}
          >
            ── {finalStatus.toUpperCase()} ──
          </div>
        )}
      </div>
    </div>
  )
}
