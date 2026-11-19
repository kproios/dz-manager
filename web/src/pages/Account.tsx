import { useEffect, useState } from 'react'
import { Account as AccountT, api, ApiError, ensureStepUp } from '../api'
import { useNotify } from '../App'
import { methodLabels, RecoveryCodes, roleLabels } from '../auth'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, ErrorBox, PageHead, SteamId } from '../ui'
import { ago } from '../util'
import { EnrollStep } from './Login'

const linkErrors: Record<string, string> = {
  confirm: 'Confirm with your 2FA code first, then link again.',
  taken: 'That sign-in is already linked to another DZ Manager account.',
}

export default function Account({ query }: { query: URLSearchParams }) {
  const { data: acc, error, reload } = useApi<AccountT>('/api/account')
  const notify = useNotify()
  const [codes, setCodes] = useState<string[] | null>(null)
  const [newPhone, setNewPhone] = useState(false)
  const [disable2FA, setDisable2FA] = useState(false)
  const [disableBusy, setDisableBusy] = useState(false)
  useEffect(() => {
    const l = query.get('linked')
    if (l) { notify(`${methodLabels[l] || l} sign-in linked`); window.location.hash = '#/account' }
  }, [])
  if (!acc) return <><PageHead title="Account" /><ErrorBox error={error} /></>
  const run = async (f: () => Promise<unknown>, ok: string) => {
    try { await f(); notify(ok); reload() } catch (x) { notify(x instanceof ApiError ? x.message : 'Failed', 'bad') }
  }
  const link = async (method: 'steam' | 'oidc') => {
    if (!acc.totp || await ensureStepUp(acc.step_up_until)) window.location.href = `/auth/${method}/login?link=1`
  }
  const turnOff2FA = async () => {
    setDisableBusy(true)
    try {
      await api.del('/api/account/2fa')
      setCodes(null); setDisable2FA(false); reload(); notify('2FA turned off')
    } catch (x) { notify(x instanceof ApiError ? x.message : 'Could not turn off 2FA', 'bad') }
    finally { setDisableBusy(false) }
  }
  const closeSetup = () => { setNewPhone(false); reload() }
  const role = acc.user.role
  return (
    <>
      <PageHead title="Account" sub={<>{acc.user.username} · <Badge state="info">{roleLabels[role]}</Badge></>} />
      <ErrorBox error={linkErrors[query.get('e') || ''] ?? null} />
      <div className="grid two">
        <Card title="Sign-in">
          <PasswordForm has={acc.has_password} login={acc.login || acc.user.username.toLowerCase()} onSaved={reload} />
          {acc.methods.steam && <>
            <h4>Steam</h4>
            {acc.steam64
              ? <div className="row wrap"><SteamId id={acc.steam64} /><button className="small" onClick={() => run(() => api.del('/api/account/steam'), 'Steam sign-in removed')}>Remove</button></div>
              : <button onClick={() => link('steam')}>Link my Steam account</button>}
          </>}
          {(acc.methods.oidc || acc.oidc_linked) && <>
            <h4>{acc.methods.oidc_label || 'Single sign-on'}</h4>
            {acc.oidc_linked
              ? <div className="row wrap"><Badge state="ok">linked</Badge><button className="small" onClick={() => run(() => api.del('/api/account/oidc'), 'Single sign-on removed')}>Remove</button></div>
              : <button onClick={() => link('oidc')}>Link {acc.methods.oidc_label || 'single sign-on'}</button>}
          </>}
          <p className="hint">Keep at least one way to sign in. You can add an authenticator app for extra protection.</p>
        </Card>
        <Card title="Two-step check (2FA)">
          <dl>
            <dt>Authenticator app</dt><dd><Badge state={acc.totp ? 'ok' : 'muted'}>{acc.totp ? 'on' : 'off'}</Badge></dd>
            {acc.totp && <><dt>Recovery codes left</dt><dd><Badge state={acc.recovery_left > 2 ? 'ok' : 'warn'}>{acc.recovery_left}</Badge></dd></>}
          </dl>
          <p className="hint">{acc.totp ? 'A code is required when you sign in and before sensitive actions.' : '2FA is optional. Turn it on to require a code as well as your usual sign-in.'}</p>
          {acc.totp ? <div className="row wrap" style={{ marginTop: 12 }}>
            <button onClick={() => run(async () => setCodes((await api.post<{ recovery_codes: string[] }>('/api/account/recovery-codes')).recovery_codes), 'New recovery codes made; the old ones no longer work')}>New recovery codes</button>
            <button onClick={() => setNewPhone(true)}>Move to a new phone</button>
            <button onClick={() => setDisable2FA(true)}>Turn off 2FA</button>
          </div> : <div className="row wrap" style={{ marginTop: 12 }}><button className="primary" onClick={() => setNewPhone(true)}>Turn on 2FA</button></div>}
          {codes && <><hr /><RecoveryCodes codes={codes} /><button className="small" onClick={() => setCodes(null)}>Hide</button></>}
        </Card>
      </div>
      <Card title="Where you are signed in" actions={acc.sessions.length > 1 &&
        <button className="small" onClick={() => run(() => api.post('/api/account/sessions/sign-out-all'), 'Signed out everywhere else')}>Sign out everywhere else</button>}>
        <div className="table-scroll"><table>
          <thead><tr><th>Signed in with</th><th>Address</th><th>Browser</th><th>Signed in</th><th>Last active</th><th /></tr></thead>
          <tbody>{acc.sessions.map((s) => (
            <tr key={s.id}>
              <td>{methodLabels[s.method] || s.method}{s.current && <> <Badge state="ok">this browser</Badge></>}</td>
              <td><code>{s.ip || '—'}</code></td>
              <td className="small muted"><span className="truncate" style={{ maxWidth: 320 }} title={s.user_agent}>{browserName(s.user_agent)}</span></td>
              <td title={s.created_at}>{ago(s.created_at)}</td>
              <td title={s.last_seen_at}>{ago(s.last_seen_at)}</td>
              <td>{!s.current && <button className="small" onClick={() => run(() => api.del('/api/account/sessions/' + s.id), 'Signed out')}>Sign out</button>}</td>
            </tr>
          ))}</tbody>
        </table></div>
      </Card>
      <Card title="What your role allows">
        <ul className="perm-list">{acc.permissions.map((p) => (
          <li key={p.name} className={acc.user.permissions.includes(p.name) ? '' : 'muted'}>{acc.user.permissions.includes(p.name) ? '✓' : '—'} {p.label}</li>
        ))}</ul>
      </Card>
      {newPhone && (
        <div className="modal-bg" onClick={closeSetup}>
          <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={acc.totp ? 'Move to a new phone' : 'Turn on 2FA'}>
            <EnrollStep start="/api/account/2fa/start" finish="/api/account/2fa/finish" onDone={() => { setNewPhone(false); reload(); notify(acc.totp ? '2FA moved to the new phone' : '2FA turned on') }} />
            <div className="row end"><button onClick={closeSetup}>Cancel</button></div>
          </div>
        </div>
      )}
      {disable2FA && <Confirm title="Turn off 2FA?" confirmLabel="Turn off 2FA" busy={disableBusy} onClose={() => setDisable2FA(false)}
        body="Future sign-ins will use your password, Steam or single sign-on without an authenticator code. Recovery codes will stop working, and your other sessions will be signed out."
        onConfirm={turnOff2FA} />}
    </>
  )
}

function browserName(ua: string) {
  if (!ua) return '—'
  const b = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  const os = /Windows/.test(ua) ? 'Windows' : /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : ''
  return os ? `${b} on ${os}` : b
}

function PasswordForm({ has, login, onSaved }: { has: boolean; login: string; onSaved: () => void }) {
  const [open, setOpen] = useState(false)
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const notify = useNotify()
  const save = async () => {
    setBusy(true); setErr(null)
    try {
      await api.put('/api/account/password', { password: pw })
      notify(has ? 'Password changed' : 'Password set'); setOpen(false); setPw(''); setPw2(''); onSaved()
    } catch (x) { setErr(x instanceof ApiError ? x.message : 'Failed') } finally { setBusy(false) }
  }
  return (
    <>
      <h4>Password</h4>
      <div className="row wrap">
        {has ? <span>Sign in as <code>{login}</code></span> : <span className="muted">No password set</span>}
        <button className="small" onClick={() => setOpen(true)}>{has ? 'Change password' : 'Set a password'}</button>
      </div>
      {open && <Confirm title={has ? 'Change password' : 'Set a password'} tone="primary" confirmLabel="Save" busy={busy}
        disabled={pw.length < 12 || pw !== pw2} onClose={() => setOpen(false)} onConfirm={save}
        body={<div className="form">
          <ErrorBox error={err} />
          <p className="muted small">You sign in as <code>{login}</code>. At least 12 characters; a few words are easiest.</p>
          <label className="field"><span>New password</span><input type="password" autoFocus value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="new-password" /></label>
          <label className="field"><span>New password again</span><input type="password" value={pw2} onChange={(e) => setPw2(e.target.value)} autoComplete="new-password" />
            {pw2 && pw !== pw2 && <span className="field-error">The passwords are not the same</span>}</label>
        </div>} />}
    </>
  )
}
