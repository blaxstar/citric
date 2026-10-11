// The one place the publisher is credited: small, muted, at the foot of every
// page. The header stays the instance's own (site title + logo mark).
export function Footer({ floating = false }: { floating?: boolean }) {
  return (
    <footer className={`site-footer${floating ? ' floating' : ''}`}>
      citric · © 2026 BLAXSTAR LLC ·{' '}
      <a href="https://github.com/blaxstar/citric" target="_blank" rel="noreferrer">source</a>
    </footer>
  )
}
