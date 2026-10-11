import { Icon, Glyphs, type IconComponent } from './icons'
import { Spinner } from './Spinner'

interface BadgeProps {
  variant: string
  children: React.ReactNode
}

export function Badge({ variant, children }: BadgeProps) {
  return <span className={`badge badge-${variant}`}>{children}</span>
}

export function CLStatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = {
    open: 'Open', merged: 'Merged', abandoned: 'Abandoned',
  }
  return <Badge variant={status}>{labels[status] ?? status}</Badge>
}

export function BuildBadge({ status }: { status: string }) {
  const icons: Record<string, IconComponent> = {
    pass: Glyphs.pass, fail: Glyphs.fail, queued: Glyphs.queued,
  }
  const glyph = icons[status]
  return (
    <Badge variant={status}>
      {/* running has no glyph — it gets the spinner, which actually animates */}
      {status === 'running'
        ? <Spinner size="0.9em" />
        : glyph && <Icon glyph={glyph} size={13} style={{ flexShrink: 0 }} />}
      {status}
    </Badge>
  )
}

export function VerdictBadge({ verdict }: { verdict: string }) {
  const labels: Record<string, string> = {
    lgtm: 'LGTM', needs_work: 'Needs Work', comment: 'Comment',
  }
  return <Badge variant={verdict}>{labels[verdict] ?? verdict}</Badge>
}
