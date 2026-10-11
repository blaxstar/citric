import { useEffect, useState } from 'react'
import { Footer } from '../components/Footer'
import { Spinner } from '../components/Spinner'
import { fetchAuthConfig, setAuth, ssoLogin, getServer } from '../lib/auth'
import { currentPath } from '../lib/router'
import { getSiteTitle, logoMark } from '../lib/site'

export function LoginPage() {
  const [sso,   setSso]   = useState<boolean | null>(null) // null = loading
  const [token, setToken] = useState('')
  const [busy,  setBusy]  = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    fetchAuthConfig().then(cfg => setSso(cfg.sso))
  }, [])

  // break-glass: validate a CLI-issued token and store it for Bearer auth
  async function handleTokenSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!token.trim()) return
    setBusy(true)
    setError('')
    try {
      const srv = getServer().replace(/\/$/, '')
      const res = await fetch(`${srv}/auth/me`, {
        headers: { Authorization: `Bearer ${token.trim()}` },
      })
      if (!res.ok) throw new Error((await res.text()).trim() || `Invalid token (${res.status})`)
      const { username } = await res.json()
      setAuth(srv, username, token.trim())
      window.location.reload()
    } catch (err: any) {
      setError(err?.message ?? 'Token validation failed')
      setBusy(false)
    }
  }

  return (
    <div className="login-root">
      <Footer floating />
      <div className="login-card card">
        <div className="login-logo">
          <div className="nav-logo-mark" style={{ width: '2.5rem', height: '2.5rem', fontSize: '1rem' }}>{logoMark()}</div>
          <span style={{ fontSize: '1.75rem', fontWeight: 800, letterSpacing: '-0.04em' }}>{getSiteTitle()}</span>
        </div>

        <p style={{ textAlign: 'center', color: 'var(--t3)', fontSize: '0.85rem', marginBottom: '1.5rem' }}>
          Code review and CI
        </p>

        {sso === null && (
          <div style={{ display: 'flex', justifyContent: 'center', padding: '1rem' }}>
            <Spinner size="1.2em" />
          </div>
        )}

        {sso === true && (
          <button
            className="btn btn-primary"
            style={{ width: '100%', justifyContent: 'center' }}
            onClick={() => ssoLogin(currentPath())}
          >
            Sign in with SSO
          </button>
        )}

        {sso === false && (
          <form onSubmit={handleTokenSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <div style={{ padding: '0.65rem 0.9rem', background: 'var(--s2)', borderRadius: 'var(--r)', fontSize: '0.82rem', color: 'var(--t2)', lineHeight: 1.6 }}>
              SSO is not configured. Sign in with a CLI token (break-glass):
              <pre style={{ margin: '0.4rem 0 0', color: 'var(--accent)', fontSize: '0.8rem', fontFamily: 'monospace' }}>
                {'citric auth token   # prints your stored token'}
              </pre>
            </div>
            <div>
              <label className="form-label">Token</label>
              <textarea
                className="input"
                rows={4}
                placeholder="eyJ..."
                value={token}
                onChange={e => setToken(e.target.value)}
                style={{ fontFamily: 'monospace', fontSize: '0.75rem', resize: 'vertical' }}
                autoFocus
                disabled={busy}
              />
            </div>
            {error && (
              <div style={{ padding: '0.5rem 0.75rem', background: 'color-mix(in srgb, var(--red) 15%, transparent)', borderRadius: 'var(--r)', fontSize: '0.8rem', color: 'var(--red)' }}>
                {error}
              </div>
            )}
            <button type="submit" className="btn btn-primary" disabled={busy || !token.trim()}>
              {busy ? <><Spinner size="0.9em" /> Verifying…</> : 'Sign in'}
            </button>
          </form>
        )}
      </div>
    </div>
  )
}
