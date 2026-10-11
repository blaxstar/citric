import { useEffect, useRef, useState } from 'react'
import { navigate, currentPath, matchRoute } from '../lib/router'
import { getUsername, hasAnyAdminPerm, ssoLogin, logout as doLogout } from '../lib/auth'
import { getSiteTitle } from '../lib/site'
import { ThemeSettings as ThemeSettingsPanel } from './ThemeSettings'
import { CliTokenModal } from './CliTokenModal'
import { Icon, Glyphs, type IconComponent } from './icons'

// Outline in every state — the active row is carried by accent color and
// background alone (see .nav-link.active), so the icon weight stays uniform.
const ICON_SIZE = 17

function NavLink({ href, label, icon, activePrefix }: { href: string; label: string; icon: IconComponent; activePrefix?: string }) {
  const path = currentPath()
  const active = activePrefix ? path.startsWith(activePrefix) : matchRoute(href, path) !== null
  return (
    <a
      href={`#${href}`}
      className={`nav-link${active ? ' active' : ''}`}
      onClick={e => { e.preventDefault(); navigate(href) }}
    >
      <Icon glyph={icon} size={ICON_SIZE} style={{ flexShrink: 0, opacity: 0.8 }} />
      {label}
    </a>
  )
}

function UserMenu({ onCli, onTheme }: { onCli: () => void; onTheme: () => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const username = getUsername()

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  function item(label: string, icon: IconComponent, action: () => void, danger = false) {
    return (
      <button
        className="ps-menu-item"
        style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', ...(danger ? { color: 'var(--red)' } : {}) }}
        onClick={() => { setOpen(false); action() }}
      >
        <Icon glyph={icon} size={ICON_SIZE} style={{ flexShrink: 0, opacity: 0.7 }} />
        {label}
      </button>
    )
  }

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      {open && (
        <div className="ps-menu" style={{ bottom: 'calc(100% + 0.4rem)', top: 'auto', left: 0, right: 0, minWidth: 'unset' }}>
          {item('View profile', Glyphs.profile, () => navigate(`/user/${username}`))}
          <div style={{ height: '1px', background: 'var(--border)', margin: '0.25rem 0' }} />
          {item('CLI token', Glyphs.cli, onCli)}
          {item('Appearance', Glyphs.appearance, onTheme)}
          {item('Switch account', Glyphs.switchAccount, () => ssoLogin(currentPath(), 'login'))}
          <div style={{ height: '1px', background: 'var(--border)', margin: '0.25rem 0' }} />
          {item('Sign out', Glyphs.signOut, doLogout, true)}
        </div>
      )}
      <button
        className="nav-user"
        style={{ width: '100%', background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left' }}
        onClick={() => setOpen(v => !v)}
        aria-expanded={open}
      >
        <div className="avatar">{username.slice(0, 2).toUpperCase()}</div>
        <span style={{ fontWeight: 600, fontSize: '0.85rem', flex: 1 }}>{username}</span>
        <Icon glyph={open ? Glyphs.chevronUp : Glyphs.chevronDown} size={13} style={{ opacity: 0.4, marginLeft: 'auto' }} />
      </button>
    </div>
  )
}

export function Nav() {
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [cliOpen, setCliOpen] = useState(false)

  return (
    <>
      <nav className="sidebar">
        <a
          href="#/"
          className="nav-logo"
          onClick={e => { e.preventDefault(); navigate('/') }}
        >
          {/* The bolt mark, inline so it takes the active palette's accent. */}
          <svg width="28" height="28" viewBox="0 0 32 32" fill="none" aria-hidden="true" style={{ display: 'block', overflow: 'visible' }}>
            <path d="M 24,6 L 16,16 L 22,16 L 14,27 L 8,22" stroke="var(--accent)" strokeWidth="3.5"
              strokeLinecap="round" strokeLinejoin="round" style={{ filter: 'drop-shadow(0 0 3px var(--accent-a20))' }} />
          </svg>
          {getSiteTitle()}
        </a>

        <div style={{ padding: '0.5rem 0.5rem' }}>
          <NavLink href="/" icon={Glyphs.home} label="Home" />

          {/* One meaning each, and each distinguishable at 17px: a stack of
              changes, a change you authored, a change awaiting your verdict. */}
          <div className="nav-section" style={{ marginTop: '0.75rem' }}>Review</div>
          <NavLink href="/changes"  icon={Glyphs.changes} label="Changes" activePrefix="/changes" />
          <NavLink href="/mine"     icon={Glyphs.authored} label="My CLs" />
          <NavLink href="/assigned" icon={Glyphs.assigned} label="Assigned to me" />

          <div className="nav-section" style={{ marginTop: '0.75rem' }}>Code</div>
          <NavLink href="/repo" icon={Glyphs.repos} label="Repositories" activePrefix="/repo" />
          <NavLink href="/starred" icon={Glyphs.starred} label="Starred" activePrefix="/starred" />

          {hasAnyAdminPerm() && (
            <>
              <div className="nav-section" style={{ marginTop: '0.75rem' }}>Tools</div>
              <NavLink href="/admin" icon={Glyphs.admin} label="Admin" />
            </>
          )}
        </div>

        <div className="nav-footer">
          <UserMenu onCli={() => setCliOpen(true)} onTheme={() => setSettingsOpen(true)} />
        </div>
      </nav>

      <ThemeSettingsPanel open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <CliTokenModal open={cliOpen} onClose={() => setCliOpen(false)} />
    </>
  )
}
