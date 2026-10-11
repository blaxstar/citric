import { navigate } from '../lib/router'

// UserLink renders a username as a link to that user's profile. Stops click
// propagation so it works inside clickable rows/cards (e.g. CL list items).
export function UserLink({ username, className, style, children }: {
  username: string
  className?: string
  style?: React.CSSProperties
  children?: React.ReactNode
}) {
  if (!username) return null
  return (
    <a
      href={`#/user/${username}`}
      className={`user-link${className ? ' ' + className : ''}`}
      style={style}
      onClick={e => { e.preventDefault(); e.stopPropagation(); navigate(`/user/${username}`) }}
    >
      {children ?? username}
    </a>
  )
}
