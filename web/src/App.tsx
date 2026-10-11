import { useEffect, useState } from 'react'
import { Nav } from './components/Nav'
import { LoginPage } from './pages/LoginPage'
import { CLListPage } from './pages/CLListPage'
import { CLDetailPage } from './pages/CLDetailPage'
import { AdminPage } from './pages/AdminPage'
import { ProfilePage } from './pages/ProfilePage'
import { RepoBrowsePage } from './pages/RepoBrowsePage'
import { ReposPage } from './pages/ReposPage'
import { HomePage } from './pages/HomePage'
import { StarredPage } from './pages/StarredPage'
import { PageSpinner } from './components/Spinner'
import { Footer } from './components/Footer'
import { fetchSession, fetchAuthConfig, hasAnyAdminPerm } from './lib/auth'
import { currentPath, matchRoute, onRenavigate } from './lib/router'
import { loadTheme, applyTheme } from './lib/theme'
import { getSiteTitle } from './lib/site'
import { refreshViewSettings } from './lib/viewSettings'

type AuthState = 'loading' | 'in' | 'out'

function Router({ path }: { path: string }) {
  let m: Record<string, string> | null

  if ((m = matchRoute('/', path))) {
    return <HomePage />
  }
  if ((m = matchRoute('/changes', path))) {
    return <CLListPage filter="all" />
  }
  if ((m = matchRoute('/mine', path))) {
    return <CLListPage filter="mine" />
  }
  if ((m = matchRoute('/assigned', path))) {
    return <CLListPage filter="assigned" />
  }
  if ((m = matchRoute('/cl/:id', path))) {
    return <CLDetailPage id={Number(m.id)} />
  }
  if ((m = matchRoute('/user/:username', path))) {
    return <ProfilePage username={m.username} />
  }
  if ((m = matchRoute('/repo/:name/blob/:ref/*', path))) {
    return <RepoBrowsePage name={m.name} urlRef={m.ref} urlPath={m['*']} initialMode="file" />
  }
  if ((m = matchRoute('/repo/:name/tree/:ref/*', path))) {
    return <RepoBrowsePage name={m.name} urlRef={m.ref} urlPath={m['*']} />
  }
  if ((m = matchRoute('/repo/:name/tree/:ref', path))) {
    return <RepoBrowsePage name={m.name} urlRef={m.ref} urlPath="" />
  }
  if ((m = matchRoute('/repo/:name', path))) {
    return <RepoBrowsePage name={m.name} />
  }
  if ((m = matchRoute('/starred', path))) {
    return <StarredPage />
  }
  if ((m = matchRoute('/repo', path))) {
    return <ReposPage />
  }
  if ((m = matchRoute('/admin', path))) {
    return hasAnyAdminPerm() ? <AdminPage /> : <p style={{ color: 'var(--t3)' }}>Admins only.</p>
  }

  return <HomePage />
}

export function App() {
  const [path, setPath] = useState(currentPath)
  const [auth, setAuth] = useState<AuthState>('loading')
  // Bumped on every navigation — including re-navigating to the current route.
  // Feeding it into the Router key remounts the page so it refetches, which is
  // what makes clicking the link for the page you're already on reload it.
  const [navSeq, setNavSeq] = useState(0)

  useEffect(() => {
    applyTheme(loadTheme())
    document.title = getSiteTitle()
    fetchAuthConfig() // refresh + cache site title (also sets document.title)
    fetchSession().then(user => {
      setAuth(user ? 'in' : 'out')
      if (user) refreshViewSettings()
    })

    function onHash() { setPath(currentPath()); setNavSeq(n => n + 1) }
    window.addEventListener('hashchange', onHash)
    const offRenav = onRenavigate(() => setNavSeq(n => n + 1))
    return () => { window.removeEventListener('hashchange', onHash); offRenav() }
  }, [])

  if (auth === 'loading') {
    return (
      <div className="app-shell">
        <main className="main-content"><PageSpinner /></main>
      </div>
    )
  }

  if (auth === 'out') return <LoginPage />

  return (
    <div className="app-shell with-nav">
      <Nav />
      <main className="main-content">
        <div className="main-body">
          <Router key={`${path}#${navSeq}`} path={path} />
        </div>
        <Footer />
      </main>
    </div>
  )
}
