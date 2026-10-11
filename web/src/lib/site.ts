// Site display title (set by admins via Server settings → site_title).
// Cached in localStorage so it paints instantly, including on the login page;
// the authoritative value comes from /auth/config (public).

const KEY = 'citric:site_title'
const DEFAULT = 'citric'

export function getSiteTitle(): string {
  return localStorage.getItem(KEY) || DEFAULT
}

export function setSiteTitle(title: string) {
  const t = (title || '').trim() || DEFAULT
  localStorage.setItem(KEY, t)
  document.title = t
}

// logoMark is the short badge shown in the nav/login logo — first two letters
// of the title, lowercased (e.g. "citric" → "ci").
export function logoMark(): string {
  return getSiteTitle().replace(/[^a-zA-Z0-9]/g, '').slice(0, 2).toLowerCase() || 'ci'
}
