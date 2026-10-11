import { PALETTES, DEFAULT_PALETTE } from './palettes'

export type Theme = 'dark' | 'light'

export interface ThemeSettings {
  theme: Theme
  bgImage: string
  // Color palette id; '' follows the instance default set by an admin.
  palette?: string
}

const KEY = 'citric:theme'
const SITE_PALETTE_KEY = 'citric:site_palette'

export function loadTheme(): ThemeSettings {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) return { palette: '', ...(JSON.parse(raw) as ThemeSettings) }
  } catch { /* ignore */ }
  return { theme: 'dark', bgImage: '', palette: '' }
}

export function saveTheme(s: ThemeSettings) {
  try { localStorage.setItem(KEY, JSON.stringify(s)) } catch { /* private mode */ }
  applyTheme(s)
}

// The instance's default palette (admin setting), cached so the first paint,
// including the sign-in page, already has the right colors.
export function sitePalette(): string {
  try {
    const id = localStorage.getItem(SITE_PALETTE_KEY)
    if (id && PALETTES.some(p => p.id === id)) return id
  } catch { /* ignore */ }
  return DEFAULT_PALETTE
}

export function setSitePalette(id: string | undefined) {
  try {
    if (id && PALETTES.some(p => p.id === id)) localStorage.setItem(SITE_PALETTE_KEY, id)
    else localStorage.removeItem(SITE_PALETTE_KEY)
  } catch { /* ignore */ }
  applyTheme(loadTheme())
}

export function applyTheme(s: ThemeSettings) {
  const own = s.palette && PALETTES.some(p => p.id === s.palette) ? s.palette : ''
  const palette = own || sitePalette()
  if (palette === DEFAULT_PALETTE) delete document.documentElement.dataset.palette
  else document.documentElement.dataset.palette = palette
  document.documentElement.dataset.theme = s.theme
  document.documentElement.style.setProperty(
    '--bg-image',
    s.bgImage ? `url(${JSON.stringify(s.bgImage)})` : 'none',
  )
}

export function humanTime(iso: string): string {
  const d = Date.now() - new Date(iso).getTime()
  const min = Math.floor(d / 60000)
  if (min < 1)   return 'just now'
  if (min < 60)  return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr  < 24)  return `${hr}h ago`
  const day = Math.floor(hr / 24)
  if (day < 7)   return `${day}d ago`
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

export function shortSHA(sha: string): string {
  return sha.slice(0, 8)
}

export function buildDuration(b: { started_at?: string; ended_at?: string }): string {
  if (!b.started_at || !b.ended_at) return ''
  const ms = new Date(b.ended_at).getTime() - new Date(b.started_at).getTime()
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m${s % 60 > 0 ? ` ${s % 60}s` : ''}`
}
