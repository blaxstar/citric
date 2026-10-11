import { ExclamationTriangleIcon, CheckCircleIcon, InformationCircleIcon } from '@heroicons/react/24/outline'
import { Icon, type IconComponent } from './icons'
type Variant = 'error' | 'success' | 'warning' | 'info'

const VARIANT_CLASS: Record<Variant, string> = {
  error:   'alert-err',
  success: 'alert-ok',
  warning: 'alert-warn',
  info:    'alert-info',
}

const VARIANT_ICON: Record<Variant, IconComponent> = {
  error:   ExclamationTriangleIcon,
  success: CheckCircleIcon,
  warning: ExclamationTriangleIcon,
  info:    InformationCircleIcon,
}

interface Props {
  msg: string
  variant?: Variant
  onDismiss?: () => void
  style?: React.CSSProperties
}

export function ErrorAlert({ msg, variant = 'error', onDismiss, style }: Props) {
  if (!msg) return null
  return (
    <div
      className={`alert ${VARIANT_CLASS[variant]}`}
      style={{ display: 'flex', alignItems: 'flex-start', gap: '0.6rem', marginBottom: 0, ...style }}
    >
      <Icon glyph={VARIANT_ICON[variant]} size={15} style={{ flexShrink: 0, opacity: 0.8 }} />
      <span style={{ flex: 1, lineHeight: 1.5 }}>{msg}</span>
      {onDismiss && (
        <button
          onClick={onDismiss}
          style={{ flexShrink: 0, background: 'none', border: 'none', cursor: 'pointer', color: 'inherit', opacity: 0.6, fontSize: '1em', lineHeight: 1, padding: 0 }}
          aria-label="Dismiss"
        >
          ✕
        </button>
      )}
    </div>
  )
}
