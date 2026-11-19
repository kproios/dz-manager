import { useEffect, useState } from 'react'
import { api, ApiError, AuthState, Enrollment, Methods } from '../api'
import { useNotify } from '../App'
import { CodeField, RecoveryCodes, roleLabels } from '../auth'
import { Icon } from '../icons'
import { ErrorBox } from '../ui'

const errorText: Record<string, string> = {
  no_account: 'No DZ Manager account is linked to that sign-in. Ask an owner for an invite, or sign in another way and link it under Account.',
  disabled: 'This account is disabled. Ask an owner.',
  locked: 'This account is locked for a while after failed sign-ins. Try again later.',
  expired: 'The sign-in took too long or was started in another tab. Try again.',
  cancelled: 'The sign-in was cancelled.',
  failed: 'The sign-in could not be verified. Try again.',
  sso_down: 'The single sign-on provider is not reachable right now.',
  invite: 'This invite link is not valid any more. Ask for a new one.',
  name: 'That name can’t be used (taken, or not 2–32 letters, digits, spaces, dots, dashes or underscores).',
  taken: 'That sign-in is already linked to another DZ Manager account.',
}

function errMsg(e: unknown) { return e instanceof ApiError ? e.message : 'Something went wrong. Try again.' }

/** The whole signed-out experience: sign in, 2FA, 2FA setup, invites. */
export default function Login({ route, args, query, onSignedIn }: { route: string; args: string[]; query: URLSearchParams; onSignedIn: () => void }) {
  const [state, setState] = useState<AuthState | null>(null)
  const [err, setErr] = useState<string | null>(errorText[query.get('e') || ''] ?? null)
  const reload = () => api.get<AuthState>('/auth/state').then(setState).catch(() => setErr('DZ Manager is not reachable.'))
  useEffect(() => { reload() }, [])
  useEffect(() => { if (state?.signed_in) onSignedIn() }, [state])

  let body
  if (!state) body = <div className="muted">Loading…</div>
  else if (state.pending === 'totp') body = <TotpStep onDone={onSignedIn} onCancel={() => api.post('/auth/cancel').then(reload)} />
  else if (state.pending === 'enroll') body = <EnrollStep onDone={onSignedIn} />
  else if (route === 'invite' && args[0]) body = <InviteStep token={args[0]} methods={state.methods} onNext={reload} />
  else body = <SignIn state={state} onNext={reload} />

  return (
    <div className="signin-page">
      <div className="signin">
        <div className="brand"><Icon name="logo" />DZ Manager</div>
        <ErrorBox error={err} />
        {body}
      </div>
    </div>
  )
}

function OtherMethods({ methods, query = '' }: { methods: Methods; query?: string }) {
  if (!methods.steam && !methods.oidc) return null
  return (
    <div className="signin-alt">
      {methods.steam && <a className="button" href={'/auth/steam/login' + query}>Sign in with Steam</a>}
      {methods.oidc && <a className="button" href={'/auth/oidc/login' + query}>{methods.oidc_label || 'Single sign-on'}</a>}
    </div>
  )
}

function SignIn({ state, onNext }: { state: AuthState; onNext: () => void }) {
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr(null)
    try {
      await api.post('/auth/password', { login, password })
      setPassword('')
      onNext()
    } catch (x) { setErr(errMsg(x)) } finally { setBusy(false) }
  }
  if (state.setup_needed) return (
    <div>
      <h1>Welcome</h1>
      <p className="muted">There are no accounts yet. On the server, run this command and open the link it prints to create the owner account:</p>
      <pre>dzm admin invite-owner</pre>
      <p className="muted small">Run it with the same environment as the service (Docker: <code>docker exec dzm dzm admin invite-owner</code>).</p>
    </div>
  )
  return (
    <form className="form" onSubmit={submit}>
      <h1>Sign in</h1>
      <ErrorBox error={err} />
      <label className="field"><span>User name</span><input autoFocus value={login} onChange={(e) => setLogin(e.target.value)} autoComplete="username" required /></label>
      <label className="field"><span>Password</span><input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required /></label>
      <button className="primary" disabled={busy || !login || !password}>{busy ? 'Checking…' : 'Continue'}</button>
      {(state.methods.steam || state.methods.oidc) && <div className="signin-or"><span>or</span></div>}
      <OtherMethods methods={state.methods} />
      <p className="muted small">Every sign-in asks for a code from your authenticator app too.</p>
    </form>
  )
}

function TotpStep({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const notify = useNotify()
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const ready = recovery ? code.trim().length >= 12 : code.length === 6
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (!ready) return
    setBusy(true); setErr(null)
    try {
      const r = await api.post<{ recovery_used: boolean }>('/auth/totp', recovery ? { recovery: code } : { code })
      if (r.recovery_used) notify('You used a recovery code. Make new ones under Account when you can.')
      onDone()
    } catch (x) { setErr(errMsg(x)); setCode(''); setBusy(false) }
  }
  useEffect(() => { if (!recovery && code.length === 6) submit() }, [code])
  return (
    <form className="form" onSubmit={submit}>
      <h1>Two-step check</h1>
      <ErrorBox error={err} />
      <CodeField value={code} onChange={setCode} recovery={recovery} />
      <button className="primary" disabled={busy || !ready}>{busy ? 'Checking…' : 'Sign in'}</button>
      <div className="row">
        <button type="button" className="link small" onClick={() => { setRecovery(!recovery); setCode('') }}>
          {recovery ? 'Use the authenticator app' : 'Lost your phone? Use a recovery code'}
        </button>
        <button type="button" className="link small" style={{ marginLeft: 'auto' }} onClick={onCancel}>Start over</button>
      </div>
    </form>
  )
}

/** Sets up the authenticator app (first sign-in, or after an owner reset 2FA). */
export function EnrollStep({ onDone, start = '/auth/enroll/start', finish = '/auth/enroll/finish' }: { onDone: () => void; start?: string; finish?: string }) {
  const [enr, setEnr] = useState<Enrollment | null>(null)
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [showKey, setShowKey] = useState(false)
  useEffect(() => { api.post<Enrollment>(start).then(setEnr).catch((x) => setErr(errMsg(x))) }, [start])
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr(null)
    try {
      const r = await api.post<{ recovery_codes: string[] }>(finish, { code, ticket: enr?.ticket })
      setCodes(r.recovery_codes)
    } catch (x) { setErr(errMsg(x)); setCode('') } finally { setBusy(false) }
  }
  if (codes) return (
    <div className="form">
      <h1>Two-step check is on</h1>
      <RecoveryCodes codes={codes} />
      <button className="primary" onClick={onDone}>I saved them, continue</button>
    </div>
  )
  return (
    <form className="form" onSubmit={submit}>
      <h1>Set up the two-step check</h1>
      <p className="muted small">Scan the code with an authenticator app (Google Authenticator, Microsoft Authenticator, 2FAS, Aegis, 1Password, Bitwarden…), then enter the 6-digit code it shows.</p>
      <ErrorBox error={err} />
      {enr ? <>
        <img className="qr" src={enr.qr} alt="QR code for your authenticator app" width={200} height={200} />
        <button type="button" className="link small" onClick={() => setShowKey(!showKey)}>{showKey ? 'Hide the key' : 'Can’t scan? Type the key instead'}</button>
        {showKey && <p className="small">Account <b>{enr.account}</b>, key <code className="secret-key">{enr.secret.match(/.{1,4}/g)?.join(' ')}</code> (time-based)</p>}
        <CodeField value={code} onChange={setCode} autoFocus={false} />
        <button className="primary" disabled={busy || code.length !== 6}>{busy ? 'Checking…' : 'Turn it on'}</button>
      </> : !err && <div className="muted">Preparing…</div>}
    </form>
  )
}

function InviteStep({ token, methods, onNext }: { token: string; methods: Methods; onNext: () => void }) {
  const [info, setInfo] = useState<{ role: string; role_label: string; expires_at: string } | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [password2, setPassword2] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { api.get<typeof info>('/auth/invite/' + encodeURIComponent(token)).then(setInfo).catch((x) => setErr(errMsg(x))) }, [token])
  if (!info) return err ? <ErrorBox error={err} /> : <div className="muted">Loading…</div>
  const nameOk = /^[\p{L}\p{N}][\p{L}\p{N} ._-]{1,31}$/u.test(name.trim())
  const mismatch = password2 !== '' && password !== password2
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr(null)
    try {
      await api.post('/auth/invite/' + encodeURIComponent(token) + '/password', { username: name.trim(), password })
      onNext()
    } catch (x) { setErr(errMsg(x)) } finally { setBusy(false) }
  }
  const q = `?invite=${encodeURIComponent(token)}&name=${encodeURIComponent(name.trim())}`
  return (
    <form className="form" onSubmit={submit}>
      <h1>Join DZ Manager</h1>
      <p className="muted">You were invited as <b>{info.role_label || roleLabels[info.role]}</b>. The link works once, until {new Date(info.expires_at).toLocaleString()}.</p>
      <ErrorBox error={err} />
      <label className="field"><span>Your name in DZ Manager</span>
        <input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={32} autoComplete="username" />
        {name && !nameOk && <span className="field-error">2–32 letters, digits, spaces, dots, dashes or underscores</span>}
      </label>
      <h4>Sign in with a password</h4>
      <label className="field"><span>Password (at least 12 characters; a few words are easiest)</span>
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
      </label>
      <label className="field"><span>Password again</span>
        <input type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} autoComplete="new-password" />
        {mismatch && <span className="field-error">The passwords are not the same</span>}
      </label>
      <button className="primary" disabled={busy || !nameOk || password.length < 12 || password !== password2}>{busy ? 'Creating…' : 'Create account'}</button>
      {(methods.steam || methods.oidc) && <>
        <div className="signin-or"><span>or, instead of a password</span></div>
        {nameOk ? <OtherMethods methods={methods} query={q} /> : <p className="muted small">Pick your name first.</p>}
      </>}
      <p className="muted small">Next you set up a code app on your phone: every sign-in asks for it.</p>
    </form>
  )
}
