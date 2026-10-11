import { setSiteTitle } from './site'
import { setSitePalette } from './theme'

const KEY_TOKEN    = 'citric:token'
const KEY_USERNAME = 'citric:username'
const KEY_SERVER   = 'citric:server'
const KEY_ROLE     = 'citric:role'
const KEY_PERMS    = 'citric:permissions'

export const PERM_GALLERY_REMOVE = 'crit-gallery-remove'
export const PERM_GALLERY_UPLOAD = 'crit-gallery-upload'
export const PERM_REPO_CREATE    = 'crit-repo-create'
export const PERM_REPO_MODIFY    = 'crit-repo-modify'
export const PERM_SSO_SYNC       = 'crit-sso-sync'

export function getToken():    string { return localStorage.getItem(KEY_TOKEN)    ?? '' }
export function getUsername(): string { return localStorage.getItem(KEY_USERNAME) ?? '' }
export function getServer():   string { return localStorage.getItem(KEY_SERVER)   ?? window.location.origin }
export function getRole():     string { return localStorage.getItem(KEY_ROLE)     ?? '' }
export function isAdmin():    boolean { return getRole() === 'admin' }

function getPerms(): string[] {
  try { return JSON.parse(localStorage.getItem(KEY_PERMS) ?? '[]') } catch { return [] }
}

export function hasPerm(perm: string): boolean {
  return isAdmin() || getPerms().includes(perm)
}

export function hasAnyAdminPerm(): boolean {
  return isAdmin() || getPerms().some(p => p.startsWith('crit-'))
}

// setAuth is retained for the CLI-token break-glass path (manual token entry).
export function setAuth(server: string, username: string, token: string) {
  localStorage.setItem(KEY_SERVER,   server)
  localStorage.setItem(KEY_USERNAME, username)
  localStorage.setItem(KEY_TOKEN,    token)
}

export function clearAuth() {
  localStorage.removeItem(KEY_TOKEN)
  localStorage.removeItem(KEY_USERNAME)
  localStorage.removeItem(KEY_SERVER)
  localStorage.removeItem(KEY_ROLE)
  localStorage.removeItem(KEY_PERMS)
}

export interface AuthConfig { sso: boolean; login_url: string; site_title?: string; user_sync?: boolean; default_palette?: string }

// fetchAuthConfig reports whether the server has SSO (and Keycloak sync) enabled and the
// configured site title (public — usable on the login page). Side-effect: caches
// the site title so the logo/header/tab paint correctly everywhere.
export async function fetchAuthConfig(): Promise<AuthConfig> {
  try {
    const res = await fetch(getServer() + '/auth/config', { credentials: 'include' })
    if (res.ok) {
      const cfg: AuthConfig = await res.json()
      if (cfg.site_title) setSiteTitle(cfg.site_title)
      setSitePalette(cfg.default_palette)
      return cfg
    }
  } catch { /* fall through */ }
  return { sso: false, login_url: '/auth/sso/login' }
}

// fetchSession resolves the current user from the session cookie (or a
// break-glass Bearer token). Returns the username, or null if unauthenticated.
export async function fetchSession(): Promise<string | null> {
  try {
    const token = getToken()
    const res = await fetch(getServer() + '/auth/me', {
      credentials: 'include',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (!res.ok) return null
    const { username, role, permissions } = await res.json()
    if (username) {
      localStorage.setItem(KEY_USERNAME, username)
      localStorage.setItem(KEY_ROLE, role ?? '')
      localStorage.setItem(KEY_PERMS, JSON.stringify(permissions ?? []))
      return username
    }
    return null
  } catch {
    return null
  }
}

// ssoLogin redirects the browser into the Keycloak login flow.
// Pass prompt='login' to force Keycloak to show the login screen even with an active session.
export function ssoLogin(returnTo: string, prompt?: 'login' | 'select_account') {
  const to = returnTo && returnTo.startsWith('/') ? returnTo : '/'
  const p = prompt ? `&prompt=${prompt}` : ''
  window.location.href = `${getServer()}/auth/sso/login?return_to=${encodeURIComponent(to)}${p}`
}

// logout clears the server session cookie and the local break-glass token.
export function logout() {
  clearAuth()
  window.location.href = `${getServer()}/auth/logout`
}
