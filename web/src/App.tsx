import { createContext, useContext, useEffect, useState } from 'react'
import { api, Me, setCSRF } from './api'
import { useHashRoute } from './hooks'
import { Toast } from './ui'
import { Icon, IconName } from './icons'
import Dashboard from './pages/Dashboard'
import Live from './pages/Live'
import Players from './pages/Players'
import PlayerPage from './pages/PlayerPage'
import Events from './pages/Events'
import Restarts from './pages/Restarts'
import Bans from './pages/Bans'
import Settings from './pages/Settings'
import AuditPage from './pages/Audit'

type Notify = (msg: string, kind?: 'ok' | 'bad') => void
const NotifyCtx = createContext<Notify>(() => {})
export const useNotify = () => useContext(NotifyCtx)

const nav: [string, string, IconName][] = [
  ['dashboard', 'Dashboard', 'dashboard'], ['live', 'Live', 'live'], ['players', 'Player database', 'players'], ['events', 'Events', 'events'],
  ['restarts', 'Restarts', 'restarts'], ['bans', 'Bans', 'bans'], ['settings', 'Settings', 'settings'], ['audit', 'Audit log', 'audit'],
]

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [sessionError, setSessionError] = useState(false)
  const [toast, setToast] = useState<{ msg: string; kind: 'ok' | 'bad' } | null>(null)
  const [route, args] = useHashRoute()

  useEffect(() => {
    api.get<Me>('/api/me').then((m) => { setCSRF(m.csrf); setMe(m) }).catch(() => setSessionError(true))
  }, [])

  const notify: Notify = (msg, kind = 'ok') => {
    setToast({ msg, kind })
    window.setTimeout(() => setToast(null), 6000)
  }

  if (!me) return sessionError ? (
    <main role="alert"><h1>Couldn’t load your session</h1><p>Reload the page or sign in again.</p><a href="/auth/login">Sign in</a></main>
  ) : <div className="loading">Loading…</div>

  let page
  switch (route) {
    case 'live': page = <Live tab={args[0]} />; break
    case 'map': page = <Live tab="map" />; break
    case 'players': page = args[0] ? <PlayerPage id={args[0]} /> : <Players />; break
    case 'events': page = <Events />; break
    case 'restarts': page = <Restarts />; break
    case 'bans': page = <Bans />; break
    case 'settings': page = <Settings />; break
    case 'audit': page = <AuditPage />; break
    default: page = <Dashboard />
  }

  return (
    <NotifyCtx.Provider value={notify}>
      <div className={'layout' + (route === 'map' || route === 'live' ? ' wide' : '')}>
        <aside>
          <div className="brand"><Icon name="logo" />DZ Manager</div>
          <nav>
            {nav.map(([k, label, icon]) => (
              <a key={k} href={'#/' + k} className={route === k || (k === 'live' && route === 'map') ? 'active' : ''} aria-current={route === k ? 'page' : undefined}><Icon name={icon} />{label}</a>
            ))}
          </nav>
          <div className="me">
            <div className="small muted">Signed in via Authentik</div>
            <div className="who" title={me.email || me.username}>{me.username}</div>
            <form method="post" action="/auth/logout">
              <button className="link">Sign out</button>
            </form>
          </div>
        </aside>
        <main>{page}</main>
      </div>
      {toast && <Toast msg={toast.msg} kind={toast.kind} onClose={() => setToast(null)} />}
    </NotifyCtx.Provider>
  )
}
