import { useEffect, useState } from 'react'
import { Spinner } from './Spinner'
import { api } from '../lib/api'
import { setSiteTitle as applySiteTitle } from '../lib/site'
import { setSitePalette } from '../lib/theme'
import { PALETTES, DEFAULT_PALETTE } from '../lib/palettes'

export function ServerSettingsModal({ onClose }: { onClose: () => void }) {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving]   = useState(false)
  const [error, setError]     = useState('')
  const [saved, setSaved]     = useState(false)
  const [siteTitle, setSiteTitle] = useState('')
  const [palette, setPalette]     = useState(DEFAULT_PALETTE)
  const [requireOther, setRequireOther] = useState(false)

  useEffect(() => {
    api.getSettings().then(s => {
      setSiteTitle(s.site_title ?? '')
      setPalette(s.default_palette || DEFAULT_PALETTE)
      setRequireOther(s.require_other_lgtm === 'true')
    }).catch(e => setError(e?.message ?? 'Failed to load settings')).finally(() => setLoading(false))
  }, [])

  async function save() {
    setSaving(true); setError(''); setSaved(false)
    try {
      await api.updateSettings({ site_title: siteTitle.trim(), default_palette: palette, require_other_lgtm: String(requireOther) })
      applySiteTitle(siteTitle.trim()) // updates the tab title + cached value immediately
      setSitePalette(palette)
      setSaved(true); setTimeout(() => setSaved(false), 1500)
    } catch (e: any) {
      setError(e?.message ?? 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="settings-panel open">
      <div className="settings-backdrop" onClick={onClose} />
      <div className="settings-drawer">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 700, fontSize: '1rem' }}>Server settings</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>

        {loading ? <Spinner /> : (
          <>
            <div>
              <div className="section-title">Site title</div>
              <input className="input" placeholder="CitriC" value={siteTitle}
                onChange={e => setSiteTitle(e.target.value)} />
            </div>

            <div>
              <div className="section-title">Default color palette</div>
              <select className="input" value={palette} onChange={e => setPalette(e.target.value)}>
                {PALETTES.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
              <p className="form-hint" style={{ marginTop: '0.35rem' }}>
                Used on the sign-in page and for everyone who hasn't picked their own in Appearance.
              </p>
            </div>

            <div>
              <div className="section-title">Reviews</div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                <input type="checkbox" checked={requireOther} onChange={e => setRequireOther(e.target.checked)} />
                Require an LGTM from someone other than the author
              </label>
              <p className="form-hint" style={{ marginTop: '0.35rem' }}>
                Applies to every repo. When off, repos can still turn it on in their own settings.
              </p>
            </div>

            {error && <p style={{ color: 'var(--red)', fontSize: '0.8rem' }}>{error}</p>}
            <button className="btn btn-primary" disabled={saving} onClick={save}>
              {saving ? <Spinner size="0.9em" /> : saved ? '✓ Saved' : 'Save settings'}
            </button>

            <div style={{ marginTop: '0.5rem', borderTop: 'var(--hairline) solid var(--border)', paddingTop: '0.75rem' }}>
              <div className="section-title">This session</div>
              <p style={{ fontSize: '0.85rem', color: 'var(--t3)', fontFamily: 'var(--font)' }}>
                {window.location.origin}
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
