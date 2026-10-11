import { useState } from 'react'
import { Spinner } from './Spinner'
import { api } from '../lib/api'
import { getServer } from '../lib/auth'

// CliTokenModal mints a CLI token for the signed-in user and shows the exact
// `citric auth login` command to run. The token is a citric session JWT, so it's
// equivalent to the web session — treat it like a password.
export function CliTokenModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [token,   setToken]   = useState('')
  const [loading, setLoading] = useState(false)
  const [error,   setError]   = useState('')
  const [copied,  setCopied]  = useState(false)
  const [installCopied, setInstallCopied] = useState(false)

  if (!open) return null

  const server = getServer().replace(/\/$/, '')
  const cmd = token ? `citric auth login --server ${server} --token ${token}` : ''
  const installCmd = `curl -fsSL ${server}/install.sh | sh`

  async function generate() {
    setLoading(true)
    setError('')
    try {
      const { token } = await api.cliToken()
      setToken(token)
    } catch (e: any) {
      setError(e?.message ?? 'Could not generate token')
    } finally {
      setLoading(false)
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(cmd)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch { /* clipboard may be unavailable */ }
  }

  async function copyInstall() {
    try {
      await navigator.clipboard.writeText(installCmd)
      setInstallCopied(true)
      setTimeout(() => setInstallCopied(false), 1500)
    } catch { /* clipboard may be unavailable */ }
  }

  return (
    <div className="settings-panel open">
      <div className="settings-backdrop" onClick={onClose} />
      <div className="settings-drawer">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 700, fontSize: '1rem' }}>CLI access</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>

        <p style={{ fontSize: '0.82rem', color: 'var(--t2)', lineHeight: 1.6 }}>
          Install the <code>citric</code> CLI, then generate a token to authenticate it on
          this machine. The token carries your identity — treat it like a password.
        </p>

        <div>
          <div className="section-title">1 · Install the CLI</div>
          <pre style={{
            margin: 0, padding: '0.65rem 0.8rem', background: 'var(--s2)', borderRadius: 'var(--r)',
            fontSize: '0.72rem', fontFamily: 'monospace', color: 'var(--accent)',
            whiteSpace: 'pre-wrap', wordBreak: 'break-all', lineHeight: 1.5,
          }}>
            {installCmd}
          </pre>
          <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.4rem' }}>
            <button className="btn btn-primary btn-sm" style={{ flex: 1 }} onClick={copyInstall}>
              {installCopied ? '✓ Copied' : 'Copy install command'}
            </button>
            <a className="btn btn-ghost btn-sm" href={`${server}/install.sh`} download="install.sh">
              Download script
            </a>
          </div>
          <p className="form-hint">
            The script downloads the <code>citric</code> binary for your platform from this server.
          </p>
        </div>

        <div className="section-title" style={{ marginTop: '0.25rem' }}>2 · Authenticate</div>

        {!token && (
          <button className="btn btn-primary" disabled={loading} onClick={generate}>
            {loading ? <><Spinner size="0.9em" /> Generating…</> : 'Generate CLI token'}
          </button>
        )}

        {error && (
          <div style={{ padding: '0.5rem 0.75rem', background: 'color-mix(in srgb, var(--red) 15%, transparent)', borderRadius: 'var(--r)', fontSize: '0.8rem', color: 'var(--red)' }}>
            {error}
          </div>
        )}

        {token && (
          <>
            <div>
              <div className="section-title">Run this on your machine</div>
              <pre style={{
                margin: 0, padding: '0.65rem 0.8rem', background: 'var(--s2)', borderRadius: 'var(--r)',
                fontSize: '0.72rem', fontFamily: 'monospace', color: 'var(--accent)',
                whiteSpace: 'pre-wrap', wordBreak: 'break-all', lineHeight: 1.5,
              }}>
                {cmd}
              </pre>
            </div>
            <div style={{ display: 'flex', gap: '0.4rem' }}>
              <button className="btn btn-primary btn-sm" style={{ flex: 1 }} onClick={copy}>
                {copied ? '✓ Copied' : 'Copy command'}
              </button>
              <button className="btn btn-ghost btn-sm" onClick={generate} disabled={loading}>
                Regenerate
              </button>
            </div>
            <p className="form-hint">
              Generating again does not revoke previous tokens — they remain valid until they expire.
            </p>
          </>
        )}
      </div>
    </div>
  )
}
