import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { loadTheme, saveTheme, sitePalette, type Theme } from '../lib/theme'
import { PALETTES } from '../lib/palettes'
import { getViewSettings, saveViewSettings } from '../lib/viewSettings'

export function ThemeSettings({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [settings, setSettings] = useState(loadTheme)
  const [bgInput, setBgInput]   = useState(settings.bgImage)
  const [gallery, setGallery]   = useState<string[]>([])
  const [mergeNoAsk, setMergeNoAsk] = useState(!!getViewSettings().merge?.skip_confirm)

  useEffect(() => {
    if (!open) return
    setMergeNoAsk(!!getViewSettings().merge?.skip_confirm)
    api.listBackgrounds().then(setGallery).catch(() => setGallery([]))
  }, [open])

  function setTheme(theme: Theme) {
    const next = { ...settings, theme }
    setSettings(next)
    saveTheme(next)
  }

  // A diagonal split of the palette's background and accent for this mode.
  function chip(p: (typeof PALETTES)[number]) {
    const [bg, accent] = p[settings.theme]
    return { background: `linear-gradient(135deg, ${bg} 50%, ${accent} 50%)` }
  }

  function setPalette(palette: string) {
    const next = { ...settings, palette }
    setSettings(next)
    saveTheme(next)
  }

  function selectBg(url: string) {
    const next = { ...settings, bgImage: url }
    setBgInput(url)
    setSettings(next)
    saveTheme(next)
  }

  function clearBg() {
    const next = { ...settings, bgImage: '' }
    setBgInput('')
    setSettings(next)
    saveTheme(next)
  }

  return (
    <div className={`settings-panel${open ? ' open' : ''}`}>
      <div className="settings-backdrop" onClick={onClose} />
      <div className="settings-drawer">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 700, fontSize: '1rem' }}>Appearance</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>

        <div>
          <div className="section-title">Mode</div>
          <div className="theme-swatches">
            {(['dark', 'light'] as Theme[]).map(t => {
              const shown = PALETTES.find(p => p.id === (settings.palette || sitePalette())) ?? PALETTES[0]
              const [bg, accent] = shown[t]
              return (
                <button key={t}
                  className={`theme-swatch${settings.theme === t ? ' active' : ''}`}
                  style={{ background: bg, color: accent }}
                  onClick={() => setTheme(t)}
                >
                  {t === 'dark' ? '◐ Dark' : '○ Light'}
                </button>
              )
            })}
          </div>
        </div>

        <div>
          <div className="section-title">Color palette</div>
          <div className="palette-grid" role="radiogroup" aria-label="Color palette">
            <button
              role="radio"
              aria-checked={!settings.palette}
              className={`palette-option${!settings.palette ? ' active' : ''}`}
              onClick={() => setPalette('')}
              title="Follow this server's default"
            >
              <span className="palette-chip" style={chip(PALETTES.find(p => p.id === sitePalette()) ?? PALETTES[0])} />
              <span>Server default</span>
            </button>
            {PALETTES.map(p => {
              const on = settings.palette === p.id
              return (
                <button key={p.id}
                  role="radio"
                  aria-checked={on}
                  className={`palette-option${on ? ' active' : ''}`}
                  onClick={() => setPalette(p.id)}
                >
                  <span className="palette-chip" style={chip(p)} />
                  <span>{p.name}</span>
                </button>
              )
            })}
          </div>
        </div>

        <div>
          <div className="section-title">Behavior</div>
          <label style={{ display: 'flex', alignItems: 'flex-start', gap: '0.5rem', fontSize: '0.85rem', color: 'var(--t1)', cursor: 'pointer' }}>
            <input id="merge-no-ask" type="checkbox" checked={mergeNoAsk}
              onChange={e => { setMergeNoAsk(e.target.checked); saveViewSettings({ merge: { skip_confirm: e.target.checked } }) }}
              style={{ accentColor: 'var(--accent)', marginTop: '0.2rem' }} />
            <span>
              Merge stack merges the ready CLs without asking
              <span className="form-hint" style={{ display: 'block', marginTop: '0.15rem' }}>
                Saved to your account. When off, you see the plan first.
              </span>
            </span>
          </label>
        </div>

        <div>
          <div className="section-title">Background image</div>
          <p className="form-hint" style={{ marginTop: 0, marginBottom: '0.5rem' }}>
            Your choice is saved on this device only — it won't change anyone else's view.
          </p>

          {gallery.length > 0 && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '0.4rem', marginBottom: '0.6rem' }}>
              {gallery.map(url => (
                <button key={url} onClick={() => selectBg(url)}
                  style={{
                    aspectRatio: '16 / 10', borderRadius: 'var(--r)', cursor: 'pointer',
                    border: settings.bgImage === url ? '2px solid var(--accent)' : 'var(--hairline) solid var(--border)',
                    backgroundImage: `url(${JSON.stringify(url)})`, backgroundSize: 'cover', backgroundPosition: 'center',
                    padding: 0,
                  }}
                  title={url.split('/').pop()} />
              ))}
            </div>
          )}

          <div style={{ display: 'flex', gap: '0.4rem', marginBottom: '0.5rem' }}>
            <input
              className="input"
              placeholder="or paste an image URL"
              value={bgInput}
              onChange={e => setBgInput(e.target.value)}
              style={{ flex: 1 }}
            />
            <button className="btn btn-primary btn-sm" onClick={() => selectBg(bgInput)}>
              Apply
            </button>
          </div>

          {settings.bgImage && (
            <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={clearBg}>
              Clear background
            </button>
          )}
        </div>

        {settings.bgImage && (
          <div style={{
            borderRadius: 'var(--r)',
            overflow: 'hidden',
            border: 'var(--hairline) solid var(--border)',
            height: '100px',
            backgroundImage: `url(${JSON.stringify(settings.bgImage)})`,
            backgroundSize: 'cover',
            backgroundPosition: 'center',
          }} />
        )}
      </div>
    </div>
  )
}
