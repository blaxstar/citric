// Per-user view preferences (sort order, filters, ...). Cached in localStorage
// so a page reads its last-known value instantly on mount without waiting on
// a network round trip — mirrors site.ts's "cache locally, refresh from
// server" pattern. The authoritative copy lives server-side (GET/PUT
// /me/settings) so preferences follow the user across browsers/devices.

import { api } from './api'
import type { ViewSettings } from './api'

const KEY = 'citric:view_settings'

function loadLocal(): ViewSettings {
  try {
    const raw = localStorage.getItem(KEY)
    return raw ? JSON.parse(raw) : {}
  } catch {
    return {}
  }
}

function saveLocal(v: ViewSettings) {
  try { localStorage.setItem(KEY, JSON.stringify(v)) } catch { /* private mode, quota, etc. — non-fatal */ }
}

let cache: ViewSettings = loadLocal()

// Synchronous read of whatever's cached — used as initial component state so
// there's no flicker waiting on the network.
export function getViewSettings(): ViewSettings {
  return cache
}

// Called once at startup, after login is confirmed, to refresh the cache from
// the server. Best-effort: offline or logged-out just keeps the local cache.
export async function refreshViewSettings(): Promise<void> {
  try {
    cache = await api.getViewSettings()
    saveLocal(cache)
  } catch {
    // keep whatever was cached locally
  }
}

// Merges a partial update into the cached settings, saves locally immediately,
// and fires the write to the server in the background — a failed save just
// means this preference doesn't sync to other devices this time.
export function saveViewSettings(patch: ViewSettings) {
  cache = {
    ...cache,
    ...patch,
    cl_list: { ...cache.cl_list, ...patch.cl_list },
  }
  saveLocal(cache)
  void api.updateViewSettings(cache).catch(() => {})
}
