import { useEffect, useState } from 'react'
import { api, type CL, type DayCount } from '../lib/api'
import { navigate } from '../lib/router'
import { humanTime } from '../lib/theme'
import { CLStatusBadge } from '../components/Badge'
import { PageSpinner } from '../components/Spinner'
import { getUsername } from '../lib/auth'

// ── contribution line graph ────────────────────────────────────────────────────

function ContribGraph({ data, days }: { data: DayCount[]; days: number }) {
  // layout constants
  const W = 700, H = 100, PAD_Y = 10, PAD_R = 8
  const PAD_L = 34 // space for Y-axis labels

  const now = new Date()
  const buckets: number[] = Array(days).fill(0)
  const dayMap = new Map(data.map(d => [d.date, d.count]))
  for (let i = 0; i < days; i++) {
    const d = new Date(now)
    d.setDate(d.getDate() - (days - 1 - i))
    buckets[i] = dayMap.get(d.toISOString().slice(0, 10)) ?? 0
  }

  const max = Math.max(...buckets, 1)
  const plotW = W - PAD_L - PAD_R
  const plotH = H - PAD_Y * 2
  const xStep = plotW / (days - 1)
  const yScale = plotH / max

  const xOf = (i: number) => PAD_L + i * xStep
  const yOf = (v: number) => H - PAD_Y - v * yScale

  const pts = buckets.map((v, i) => [xOf(i), yOf(v)] as [number, number])
  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ')
  const areaPath = `${linePath} L${pts[pts.length - 1]![0].toFixed(1)},${H - PAD_Y} L${pts[0]![0].toFixed(1)},${H - PAD_Y} Z`

  // Y-axis ticks: 0, mid, max (deduplicated)
  const mid = Math.round(max / 2)
  const yTicks = [...new Set([0, mid, max])].filter(v => v >= 0)

  // month labels — only on the 1st of each month to avoid duplicate month names
  // at the start/end of the range, and enforce a minimum pixel gap as a safety net.
  // The two-digit year is carried on every label: a 365-day window spans two
  // years, and "Jan" alone doesn't say which one.
  const MIN_LABEL_GAP = 52
  const monthLabels: { x: number; label: string }[] = []
  let lastLabelX = -MIN_LABEL_GAP
  for (let i = 0; i < days; i++) {
    const d = new Date(now)
    d.setDate(d.getDate() - (days - 1 - i))
    if (d.getDate() !== 1) continue
    const x = xOf(i)
    if (x - lastLabelX >= MIN_LABEL_GAP) {
      const month = d.toLocaleString('default', { month: 'short' })
      const yy = String(d.getFullYear() % 100).padStart(2, '0')
      monthLabels.push({ x, label: `${month} '${yy}` })
      lastLabelX = x
    }
  }

  const total = buckets.reduce((a, b) => a + b, 0)

  return (
    <div style={{ background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)', padding: '1.25rem 1.5rem' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
        <div style={{ fontSize: '0.82rem', fontWeight: 600 }}>Contributions</div>
        <div style={{ fontSize: '0.72rem', color: 'var(--t3)' }}>
          <span style={{ color: 'var(--t1)', fontWeight: 600 }}>{total}</span> CLs opened in the last {days} days
        </div>
      </div>
      <svg width="100%" viewBox={`0 0 ${W} ${H + 16}`} style={{ display: 'block', overflow: 'visible' }}>
        <defs>
          <linearGradient id="cg-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.3" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0.02" />
          </linearGradient>
        </defs>

        {/* Y gridlines + labels */}
        {yTicks.map(v => {
          const y = yOf(v)
          return (
            <g key={v}>
              <line x1={PAD_L} y1={y} x2={W - PAD_R} y2={y}
                stroke="var(--border)" strokeWidth="1" strokeDasharray={v === 0 ? undefined : '3 3'} />
              <text x={PAD_L - 5} y={y + 3.5} fontSize="9" fill="var(--t3)" textAnchor="end">{v}</text>
            </g>
          )
        })}

        <path d={areaPath} fill="url(#cg-fill)" />
        <path d={linePath} fill="none" stroke="var(--accent)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />

        {/* dots for non-zero days */}
        {pts.map(([x, y], i) => buckets[i]! > 0 && (
          <circle key={i} cx={x} cy={y} r="2.5" fill="var(--accent)" opacity="0.7">
            <title>{`${new Date(now.getFullYear(), now.getMonth(), now.getDate() - (days - 1 - i)).toISOString().slice(0, 10)}: ${buckets[i]} CLs`}</title>
          </circle>
        ))}

        {/* month labels */}
        {monthLabels.map(({ x, label }, i) => (
          <text key={i} x={x} y={H + 14} fontSize="10" fill="var(--t3)" textAnchor="middle">{label}</text>
        ))}
      </svg>
    </div>
  )
}

// ── activity feed ──────────────────────────────────────────────────────────────

function ActivityFeed({ cls }: { cls: CL[] }) {
  if (cls.length === 0) {
    return (
      <div style={{ padding: '3rem', textAlign: 'center', color: 'var(--t3)', background: 'var(--s2)', border: 'var(--hairline) solid var(--border)', borderRadius: 'var(--r)' }}>
        <div style={{ fontSize: '0.9rem', marginBottom: '0.35rem' }}>No activity yet</div>
        <div style={{ fontSize: '0.8rem' }}>CLs from you and people you follow will appear here.</div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      {cls.map(cl => (
        <div key={cl.id} className="card cl-card"
          style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', cursor: 'pointer' }}
          onClick={() => navigate(`/cl/${cl.id}`)}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 600, fontSize: '0.88rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cl.title}</div>
            <div className="cl-meta" style={{ marginTop: '2px' }}>
              <button className="btn btn-ghost btn-sm" style={{ padding: 0, fontSize: '0.75rem', fontWeight: 600, color: 'var(--accent)' }}
                onClick={e => { e.stopPropagation(); navigate(`/user/${cl.author}`) }}>
                {cl.author}
              </button>
              <span>·</span>
              <span>{cl.repo}</span>
              <span>·</span>
              <span>{humanTime(cl.updated_at)}</span>
            </div>
          </div>
          <CLStatusBadge status={cl.status} />
        </div>
      ))}
    </div>
  )
}

// ── welcome messages ───────────────────────────────────────────────────────────

// Time-agnostic on purpose: the greeting rotates on a UTC day boundary, so
// anything tied to time of day ("Morning") is wrong for most of the people
// reading it for most of the day.
const GREETINGS = [
  (u: string) => `Good to see you, ${u}.`,
  (u: string) => `Welcome back, ${u}.`,
  (u: string) => `Hey, ${u}. Ready to ship?`,
  (u: string) => `What are we reviewing today, ${u}?`,
  (u: string) => `Hello, ${u}.`,
  (u: string) => `Let's get to work, ${u}.`,
  (u: string) => `Good to have you back, ${u}.`,
]

function greeting(username: string): string {
  // Rotates daily so it changes each day without randomness per reload.
  const dayIndex = Math.floor(Date.now() / 86_400_000) % GREETINGS.length
  return GREETINGS[dayIndex]!(username)
}

// ── page ───────────────────────────────────────────────────────────────────────

export function HomePage() {
  const username = getUsername()
  const [contributions, setContributions] = useState<DayCount[]>([])
  const [feed, setFeed] = useState<CL[]>([])
  const [loading, setLoading] = useState(true)

  const DAYS = 365

  useEffect(() => {
    Promise.all([
      api.getContributions(username, DAYS),
      api.getActivityFeed(),
    ]).then(([contribs, cls]) => {
      setContributions(contribs ?? [])
      setFeed(cls ?? [])
    }).catch(() => {})
      .finally(() => setLoading(false))
  }, [username])

  if (loading) return <PageSpinner />

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      <div className="panel">
        <h1 className="page-title" style={{ marginBottom: '0.75rem' }}>
          {greeting(username)}
        </h1>
        <ContribGraph data={contributions} days={DAYS} />
      </div>

      <div className="panel">
        <div style={{ fontSize: '0.82rem', fontWeight: 600, color: 'var(--t3)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '0.75rem' }}>
          Activity
        </div>
        <ActivityFeed cls={feed} />
      </div>
    </div>
  )
}
