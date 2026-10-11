export type Params = Record<string, string>

export function matchRoute(pattern: string, path: string): Params | null {
  const patParts = pattern.split('/')
  const pathParts = path.split('/')

  // Trailing * captures remaining segments as params['*']
  if (patParts[patParts.length - 1] === '*') {
    const fixed = patParts.slice(0, -1)
    if (pathParts.length < fixed.length) return null
    const params: Params = {}
    for (let i = 0; i < fixed.length; i++) {
      if (fixed[i].startsWith(':')) {
        params[fixed[i].slice(1)] = decodeURIComponent(pathParts[i])
      } else if (fixed[i] !== pathParts[i]) {
        return null
      }
    }
    params['*'] = pathParts.slice(fixed.length).map(decodeURIComponent).join('/')
    return params
  }

  if (patParts.length !== pathParts.length) return null
  const params: Params = {}
  for (let i = 0; i < patParts.length; i++) {
    if (patParts[i].startsWith(':')) {
      params[patParts[i].slice(1)] = decodeURIComponent(pathParts[i])
    } else if (patParts[i] !== pathParts[i]) {
      return null
    }
  }
  return params
}

// Fired when navigate() is called for the route we are already on. Assigning an
// unchanged value to location.hash is a no-op — the browser emits no hashchange —
// so without this, clicking the link for the current page does nothing at all.
const RENAV_EVENT = 'citric:renavigate'

export function navigate(hash: string) {
  if (hash === currentPath()) {
    window.dispatchEvent(new Event(RENAV_EVENT))
    return
  }
  window.location.hash = hash
}

// Subscribe to same-route navigations. Returns an unsubscribe function.
export function onRenavigate(fn: () => void): () => void {
  window.addEventListener(RENAV_EVENT, fn)
  return () => window.removeEventListener(RENAV_EVENT, fn)
}

export function currentPath(): string {
  return window.location.hash.replace(/^#/, '') || '/'
}

// goBack returns to wherever the user came from, using real browser history so
// it stays in sync with the actual back/forward buttons (not just our own
// in-app Back links). Falls back to navigating to fallback when there's
// nothing to go back to in this tab — e.g. the CL detail page was opened
// directly via a pasted link or a page refresh, with no prior in-app hash.
export function goBack(fallback = '/') {
  if (window.history.length > 1) {
    window.history.back()
  } else {
    navigate(fallback)
  }
}
