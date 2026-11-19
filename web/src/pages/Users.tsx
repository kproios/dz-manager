import { useState } from 'react'
import { api, ApiError, Role, UserRow, UsersResp } from '../api'
import { useNotify } from '../App'
import { methodLabels, roleLabels } from '../auth'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, PageHead, Seg } from '../ui'
import { ago } from '../util'

type Pending = { user: UserRow; action: 'role' | 'disable' | 'enable' | 'reset-2fa' | 'sign-out' | 'delete'; role?: Role }

const actionText: Record<Pending['action'], [string, string, string]> = {
  // title, body, button
  role: ['Change role', 'Their sessions end and they sign in again with the new role.', 'Change role'],
  disable: ['Disable account', 'They are signed out at once, can’t sign in, and their API keys stop working. You can enable the account again later.', 'Disable'],
  enable: ['Enable account', 'They can sign in again.', 'Enable'],
  'reset-2fa': ['Reset 2FA', 'Their sessions end and their recovery codes stop working. At the next sign-in they set up the authenticator app again. Only do this when you are sure who asked.', 'Reset 2FA'],
  'sign-out': ['Sign out everywhere', 'All their sessions end. They can sign in again.', 'Sign out'],
  delete: ['Remove account', 'The account and its API keys are deleted. The audit log keeps their name. This can’t be undone; disabling is usually better.', 'Remove'],
}

export default function Users() {
  const { data, error, reload } = useApi<UsersResp>('/api/users')
  const notify = useNotify()
  const [pending, setPending] = useState<Pending | null>(null)
  const [busy, setBusy] = useState(false)
  const [inviting, setInviting] = useState(false)
  if (!data) return <><PageHead title="Users" /><ErrorBox error={error} /></>
  const run = async () => {
    if (!pending) return
    const { user, action, role } = pending
    setBusy(true)
    try {
      if (action === 'role') await api.put(`/api/users/${user.id}/role`, { role })
      else if (action === 'delete') await api.del(`/api/users/${user.id}`)
      else await api.post(`/api/users/${user.id}/${action}`)
      notify(`${actionText[action][0]}: ${user.username}`); setPending(null); reload()
    } catch (x) { notify(x instanceof ApiError ? x.message : 'Failed', 'bad') } finally { setBusy(false) }
  }
  const methods = (u: UserRow) => [u.password && 'password', u.steam64 && 'steam', u.oidc && 'oidc'].filter(Boolean) as string[]
  return (
    <>
      <PageHead title="Users" sub="Who can open DZ Manager and what their role allows" actions={<button className="primary" onClick={() => setInviting(true)}>Invite someone</button>} />
      <Card title="Accounts">
        <div className="table-scroll"><table>
          <thead><tr><th>Name</th><th>Role</th><th>Signs in with</th><th>2FA</th><th>Last sign-in</th><th>Sessions</th><th /></tr></thead>
          <tbody>{data.users.map((u) => (
            <tr key={u.id} className={u.disabled ? 'muted' : ''}>
              <td><b>{u.username}</b>{u.self && <> <Badge state="info">you</Badge></>}{u.disabled && <> <Badge state="muted">disabled</Badge></>}
                {u.locked_until && <> <Badge state="warn" title={'until ' + u.locked_until}>locked</Badge></>}</td>
              <td>{u.self ? roleLabels[u.role] : (
                <select value={u.role} disabled={u.disabled} aria-label={'Role of ' + u.username}
                  onChange={(e) => setPending({ user: u, action: 'role', role: e.target.value as Role })}>
                  {data.roles.map((r) => <option key={r.name} value={r.name}>{r.label}</option>)}
                </select>)}</td>
              <td className="small">{methods(u).map((m) => methodLabels[m]).join(', ') || <span className="muted">none yet</span>}</td>
              <td><Badge state={u.totp ? 'ok' : 'warn'}>{u.totp ? 'on' : 'not set up'}</Badge></td>
              <td title={u.last_login_at}>{ago(u.last_login_at)}</td>
              <td>{u.sessions}</td>
              <td>{!u.self && <div className="row wrap">
                {u.disabled
                  ? <button className="small" onClick={() => setPending({ user: u, action: 'enable' })}>Enable</button>
                  : <button className="small" onClick={() => setPending({ user: u, action: 'disable' })}>Disable</button>}
                {u.sessions > 0 && <button className="small" onClick={() => setPending({ user: u, action: 'sign-out' })}>Sign out</button>}
                {u.totp && <button className="small" onClick={() => setPending({ user: u, action: 'reset-2fa' })}>Reset 2FA</button>}
                <button className="small danger" onClick={() => setPending({ user: u, action: 'delete' })}>Remove</button>
              </div>}</td>
            </tr>
          ))}</tbody>
        </table></div>
      </Card>
      <Card title="Open invites">
        {data.invites.length === 0 ? <Empty>No open invites</Empty> : (
          <div className="table-scroll"><table>
            <thead><tr><th>Role</th><th>Note</th><th>Made by</th><th>Expires</th><th /></tr></thead>
            <tbody>{data.invites.map((i) => (
              <tr key={i.id}>
                <td>{roleLabels[i.role]}</td><td>{i.note || <span className="muted">—</span>}</td><td>{i.created_by}</td>
                <td title={i.expires_at}>{new Date(i.expires_at).toLocaleString()}</td>
                <td><button className="small" onClick={async () => { try { await api.del('/api/users/invites/' + i.id); reload() } catch (x) { notify(x instanceof ApiError ? x.message : 'Failed', 'bad') } }}>Cancel</button></td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
        <p className="hint">An invite link works once. The link itself is shown only when you make it.</p>
      </Card>
      <Card title="Roles">
        <div className="table-scroll"><table className="roles-table">
          <thead><tr><th>Permission</th>{data.roles.map((r) => <th key={r.name} title={r.help}>{r.label}</th>)}</tr></thead>
          <tbody>{data.permissions.map((p) => (
            <tr key={p.name}><td>{p.label}</td>{data.roles.map((r) => <td key={r.name}>{r.permissions.includes(p.name) ? '✓' : <span className="muted">—</span>}</td>)}</tr>
          ))}</tbody>
        </table></div>
        <p className="hint">Sensitive actions (users, server profiles, API keys, strong game actions, bans, restarts) also ask for a fresh 2FA code. Every action is in the audit log under the person’s name.</p>
      </Card>
      {inviting && <InviteDialog roles={data.roles.map((r) => [r.name, r.label] as [Role, string])} onClose={() => { setInviting(false); reload() }} />}
      {pending && <Confirm title={`${actionText[pending.action][0]}: ${pending.user.username}`} confirmLabel={actionText[pending.action][2]}
        tone={pending.action === 'enable' ? 'primary' : 'danger'} busy={busy} onClose={() => setPending(null)} onConfirm={run}
        body={<>{pending.action === 'role' && <p>From <b>{roleLabels[pending.user.role]}</b> to <b>{roleLabels[pending.role!]}</b>.</p>}<p>{actionText[pending.action][1]}</p></>} />}
    </>
  )
}

function InviteDialog({ roles, onClose }: { roles: [Role, string][]; onClose: () => void }) {
  const [role, setRole] = useState<Role>('moderator')
  const [note, setNote] = useState('')
  const [days, setDays] = useState(2)
  const [link, setLink] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const make = async () => {
    setBusy(true); setErr(null)
    try { setLink((await api.post<{ link: string }>('/api/users/invites', { role, note, days })).link) }
    catch (x) { setErr(x instanceof ApiError ? x.message : 'Failed') } finally { setBusy(false) }
  }
  return (
    <div className="modal-bg" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Invite someone">
        <h3>Invite someone</h3>
        <ErrorBox error={err} />
        {link ? <>
          <p>Send this link to the person (it works once, for {days} day{days > 1 ? 's' : ''}). They choose their name and how they sign in, then set up 2FA.</p>
          <div className="id-line invite-link"><code>{link}</code><CopyButton value={link} label="Copy invite link" /></div>
          <p className="hint">Anyone with the link can use it, so send it privately. You won’t see it again; cancel the invite if it went to the wrong place.</p>
          <div className="row end"><button className="primary" onClick={onClose}>Done</button></div>
        </> : <div className="form">
          <div className="field"><span>Role</span><Seg label="Role" value={role} options={roles} onChange={setRole} /></div>
          <label className="field"><span>Note (who it is for, shown to owners)</span><input value={note} maxLength={80} onChange={(e) => setNote(e.target.value)} placeholder="e.g. Steve, weekend moderator" /></label>
          <div className="field"><span>Link valid for</span><Seg label="Valid for" value={days} options={[[1, '1 day'], [2, '2 days'], [7, '7 days']]} onChange={setDays} /></div>
          <div className="row end"><button onClick={onClose}>Cancel</button><button className="primary" disabled={busy} onClick={make}>{busy ? 'Making…' : 'Make invite link'}</button></div>
        </div>}
      </div>
    </div>
  )
}
