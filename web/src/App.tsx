import { createContext, useContext, useEffect, useState } from 'react'
import { api, Me, setCSRF } from './api'
import { useHashRoute } from './hooks'
import { Toast } from './ui'
import Dashboard from './pages/Dashboard'
import MapPage from './pages/MapPage'
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

const nav: [string, string][] = [
  ['dashboard', 'Dashboard'], ['map', 'Live map'], ['players', 'Players'], ['events', 'Events'],
  ['restarts', 'Restarts'], ['bans', 'Bans'], ['settings', 'Settings'], ['audit', 'Audit'],
]

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [toast, setToast] = useState<{ msg: string; kind: 'ok' | 'bad' } | null>(null)
  const [route, args] = useHashRoute()

  useEffect(() => {
    api.get<Me>('/api/me').then((m) => { setCSRF(m.csrf); setMe(m) }).catch(() => {})
  }, [])

  const notify: Notify = (msg, kind = 'ok') => {
    setToast({ msg, kind })
    window.setTimeout(() => setToast(null), 6000)
  }

  if (!me) return <div className="loading">Loading…</div>

  let page
  switch (route) {
    case 'map': page = <MapPage />; break
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
      <div className={'layout' + (route === 'map' ? ' wide' : '')}>
        <aside>
          <div className="brand">DZ Manager</div>
          <nav>
            {nav.map(([k, label]) => (
              <a key={k} href={'#/' + k} className={route === k ? 'active' : ''}>{label}</a>
            ))}
          </nav>
          <div className="me">
            <div className="small muted">Signed in via Authentik</div>
            <div>{me.username}</div>
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
