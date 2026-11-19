import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, Me, Permission, setCSRF, setSignedOutHandler, setStepUpHandler } from './api'
import { MeCtx, methodLabels, roleLabels, StepUpDialog } from './auth'
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
import Login from './pages/Login'
import Account from './pages/Account'
import Users from './pages/Users'

type Notify = (msg: string, kind?: 'ok' | 'bad') => void
const NotifyCtx = createContext<Notify>(() => {})
export const useNotify = () => useContext(NotifyCtx)

// [route, label, icon, permission needed to see it (any of)]
const nav: [string, string, IconName, Permission[]][] = [
  ['dashboard', 'Dashboard', 'dashboard', ['view.live']], ['live', 'Live', 'live', ['view.live']], ['players', 'Player database', 'players', ['view.live']],
  ['events', 'Events', 'events', ['view.live']], ['restarts', 'Restarts', 'restarts', ['view.live']], ['bans', 'Bans', 'bans', ['bans.manage']],
  ['settings', 'Settings', 'settings', ['settings.manage', 'servers.manage', 'bans.manage', 'players.private']],
  ['users', 'Users', 'profile', ['users.manage']], ['audit', 'Audit log', 'audit', []],
]

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [signedOut, setSignedOut] = useState(false)
  const [toast, setToast] = useState<{ msg: string; kind: 'ok' | 'bad' } | null>(null)
  const [route, args, query] = useHashRoute()
  const [stepUp, setStepUp] = useState<((ok: boolean) => void) | null>(null)
  const stepUpWait = useRef<Promise<boolean> | null>(null)

  const loadMe = useCallback(() => {
    api.get<Me>('/api/me').then((m) => { setCSRF(m.csrf); setMe(m); setSignedOut(false) }).catch(() => setSignedOut(true))
  }, [])
  useEffect(() => {
    setSignedOutHandler(() => { setMe(null); setSignedOut(true) })
    // Several requests may need the same confirmation: ask once.
    setStepUpHandler(() => {
      if (!stepUpWait.current) {
        stepUpWait.current = new Promise<boolean>((resolve) => setStepUp(() => (ok: boolean) => {
          setStepUp(null); stepUpWait.current = null
          if (ok) loadMe()
          resolve(ok)
        }))
      }
      return stepUpWait.current
    })
    loadMe()
    return () => { setSignedOutHandler(null); setStepUpHandler(null) }
  }, [loadMe])

  const notify: Notify = (msg, kind = 'ok') => {
    setToast({ msg, kind })
    window.setTimeout(() => setToast(null), 6000)
  }

  const onSignedIn = () => {
    if (route === 'login' || route === 'invite') window.location.hash = '#/'
    loadMe()
  }

  if (signedOut || route === 'invite' || (route === 'login' && !me)) {
    return <NotifyCtx.Provider value={notify}><Login route={route} args={args} query={query} onSignedIn={onSignedIn} /></NotifyCtx.Provider>
  }
  if (!me) return <div className="loading">Loading…</div>

  const can = (ps: Permission[]) => ps.length === 0 || ps.some((p) => me.permissions.includes(p))
  const visible = nav.filter(([, , , ps]) => can(ps))
  const allowed = (r: string) => { const n = nav.find(([k]) => k === r); return !n || can(n[3]) }

  let page
  const r = route === 'map' ? 'live' : route === 'login' ? 'dashboard' : route
  if (!allowed(r)) page = <main role="alert"><h1>Not available</h1><p className="muted">Your role ({roleLabels[me.role]}) doesn’t include this page.</p></main>
  else switch (r) {
    case 'live': page = route === 'map' ? <Live tab="map" focusType={args[0]} focusValue={args[1]} /> : <Live tab={args[0]} />; break
    case 'players': page = args[0] ? <PlayerPage id={args[0]} /> : <Players />; break
    case 'events': page = <Events />; break
    case 'restarts': page = <Restarts />; break
    case 'bans': page = <Bans />; break
    case 'settings': page = <Settings />; break
    case 'audit': page = <AuditPage />; break
    case 'users': page = <Users />; break
    case 'account': page = <Account query={query} />; break
    default: page = can(['view.live']) ? <Dashboard /> : <Account query={query} />
  }

  return (
    <MeCtx.Provider value={me}>
      <NotifyCtx.Provider value={notify}>
        <div className={'layout' + (route === 'map' || route === 'live' ? ' wide' : '')}>
          <aside>
            <div className="brand"><Icon name="logo" />DZ Manager</div>
            <nav>
              {visible.map(([k, label, icon]) => (
                <a key={k} href={'#/' + k} className={route === k || (k === 'live' && route === 'map') ? 'active' : ''} aria-current={route === k ? 'page' : undefined}><Icon name={icon} />{label}</a>
              ))}
            </nav>
            <div className="me">
              <a className={'who' + (route === 'account' ? ' active' : '')} href="#/account" title="Your account, 2FA and sessions">{me.username}</a>
              <div className="small muted">{roleLabels[me.role]} · {methodLabels[me.method] || me.method}</div>
              <form method="post" action="/auth/logout">
                <button className="link">Sign out</button>
              </form>
            </div>
          </aside>
          <main>{page}</main>
        </div>
        {toast && <Toast msg={toast.msg} kind={toast.kind} onClose={() => setToast(null)} />}
        {stepUp && <StepUpDialog onDone={stepUp} />}
      </NotifyCtx.Provider>
    </MeCtx.Provider>
  )
}
