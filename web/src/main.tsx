import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './theme.css'
import './palettes.css'
import { App } from './App'

// citric was renamed from crit: carry the old crit:* localStorage entries
// (token, theme, view settings, cached title) over once, so the rename
// doesn't sign anyone out or reset their preferences.
try {
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (!key?.startsWith('crit:')) continue
    const renamed = 'citric:' + key.slice('crit:'.length)
    if (localStorage.getItem(renamed) === null) localStorage.setItem(renamed, localStorage.getItem(key) ?? '')
  }
} catch {
  // storage unavailable (private mode, blocked): nothing to migrate
}

document.addEventListener('click', e => {
  const btn = (e.target as Element).closest('.md-copy-btn') as HTMLButtonElement | null
  if (!btn) return
  const code = btn.closest('pre')?.querySelector('code')
  if (!code) return
  navigator.clipboard.writeText(code.textContent ?? '').then(() => {
    btn.textContent = 'Copied!'
    btn.classList.add('copied')
    setTimeout(() => { btn.textContent = 'Copy'; btn.classList.remove('copied') }, 1500)
  })
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
