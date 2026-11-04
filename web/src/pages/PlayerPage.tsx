import { useState } from 'react'
import { ActionAvail, api, Flags, PlayerProfile } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox } from '../ui'
import { fmtDuration, fmtTime } from '../util'
import { useNotify } from '../App'
import { RconActions } from './Players'

export default function PlayerPage({ id }: { id: string }) {
  const prof = useApi<PlayerProfile>('/api/players/' + encodeURIComponent(id), ['events'])
  const flags = useApi<Flags>('/api/players/' + encodeURIComponent(id) + '/flags')
  const acts = useApi<ActionAvail[]>('/api/actions', ['capabilities'])
  if (prof.error) return <ErrorBox error={prof.error} />
  const p = prof.data?.player
  if (!p) return <div className="loading">Loading…</div>
  return (
    <>
      <p><a href="#/players">← Players</a></p>
      <h1>{p.name || '(unknown)'} {flags.data?.local_ban && <Badge state="bad">banned</Badge>} {!!flags.data?.external.length && <Badge state="warn">external listing</Badge>}</h1>
      <div className="grid">
        <Card title="Identity">
          <dl>
            <dt>Steam64</dt><dd><code>{p.steam64}</code></dd>
            <dt>BE GUID</dt><dd><code>{p.be_guid}</code></dd>
            <dt>DayZ ID</dt><dd><code className="small">{p.dayz_id}</code></dd>
            <dt>First / last seen</dt><dd>{fmtTime(p.first_seen)} · {fmtTime(p.last_seen)}</dd>
            <dt>Playtime</dt><dd>{fmtDuration(p.playtime_seconds)}</dd>
            <dt>Kills / deaths</dt><dd>{p.kills} / {p.deaths}</dd>
          </dl>
          {p.id_mismatch && <p className="small warn">A source reported a BE GUID that does not match this Steam64. The computed value is shown.</p>}
          {prof.data!.names.length > 1 && <p className="small">Also known as: {prof.data!.names.map((n) => n.name).join(', ')}</p>}
        </Card>
        <Card title="Actions">
          <div className="row wrap">
            <RconActions p={{ be_guid: p.be_guid, name: p.name }} />
          </div>
          <hr />
          {acts.data?.map((a) => <ModAction key={a.name} a={a} steam64={p.steam64} name={p.name} />)}
          <p className="small muted">Mod actions run through @DZMBridge and report the in-game result. Unavailable actions are disabled, never faked.</p>
        </Card>
        <BanCard steam64={p.steam64} flags={flags.data} reload={flags.reload} />
      </div>
      <Card title="Sessions">
        {!prof.data!.sessions.length ? <Empty>No sessions recorded.</Empty> : (
          <table>
            <thead><tr><th>Start</th><th>End</th><th>Duration</th><th>End type</th><th>Source</th></tr></thead>
            <tbody>
              {prof.data!.sessions.map((s, i) => (
                <tr key={i}>
                  <td>{fmtTime(s.started_at)}</td><td>{s.ended_at ? fmtTime(s.ended_at) : <Badge state="ok">online</Badge>}</td>
                  <td>{fmtDuration(s.duration_seconds)}</td>
                  <td>{s.end_kind === 'inferred_end' ? <Badge state="warn">inferred (crash/restart)</Badge> : s.end_kind.replace('_', ' ')}</td>
                  <td className="small muted">{s.source}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card title="Combat">
        {!prof.data!.combat.length ? <Empty>No kills or deaths recorded.</Empty> : (
          <table>
            <thead><tr><th>Time</th><th>Event</th><th>Other party</th><th>Weapon</th><th>Distance</th></tr></thead>
            <tbody>
              {prof.data!.combat.map((c, i) => (
                <tr key={i}>
                  <td>{fmtTime(c.ts)}</td>
                  <td>{c.role === 'kill' ? <Badge state="info">kill</Badge> : <Badge state="bad">death</Badge>}</td>
                  <td>{c.role === 'kill' ? <a href={'#/players/' + c.victim}>{c.victim}</a> : c.killer ? <a href={'#/players/' + c.killer}>{c.killer}</a> : <span className="muted">{c.cause || 'environment / unknown'}</span>}</td>
                  <td>{c.weapon || '—'}</td>
                  <td>{c.distance_m != null ? c.distance_m.toFixed(0) + ' m' : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card title="Admin history">
        {!prof.data!.audit.length ? <Empty>No admin actions for this player.</Empty> : (
          <table>
            <tbody>
              {prof.data!.audit.map((a) => (
                <tr key={a.id}><td>{fmtTime(a.ts)}</td><td>{a.actor_name}</td><td><code>{a.action}</code></td><td><Badge state={a.result} /></td><td className="small muted">{a.detail}</td></tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}

function ModAction({ a, steam64, name }: { a: ActionAvail; steam64: string; name: string }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [f, setF] = useState({ x: '', z: '', to_player: '', class: '', quantity: '1', reason: '' })
  const notify = useNotify()
  const run = async (typed: string) => {
    setBusy(true)
    const body: any = { reason: f.reason || undefined, confirm: typed || undefined }
    if (a.name === 'teleport') {
      if (f.to_player) body.to_player = f.to_player
      else { body.x = parseFloat(f.x); body.z = parseFloat(f.z) }
    }
    if (a.name === 'give_item') { body.class = f.class; body.quantity = parseInt(f.quantity, 10) }
    try {
      const r = await api.post<{ ok: boolean; result: any }>(`/api/players/${steam64}/actions/${a.name}`, body)
      notify(`${a.label}: done${r.result ? ' · ' + JSON.stringify(r.result) : ''}`)
      setOpen(false)
    } catch (e: any) {
      notify(`${a.label} failed: ${e.message}`, 'bad')
    } finally {
      setBusy(false)
    }
  }
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.value })
  return (
    <>
      <button className={a.destructive ? 'danger' : ''} disabled={!a.available} title={a.reason} onClick={() => setOpen(true)}>{a.label}</button>{' '}
      {open && (
        <Confirm title={`${a.label}: ${name}`} word={a.confirm} busy={busy} onClose={() => setOpen(false)} onConfirm={run} body={
          <div className="form">
            <p className="small muted">Target Steam64 <code>{steam64}</code></p>
            {a.name === 'teleport' && <>
              <label>X (east) <input value={f.x} onChange={set('x')} inputMode="decimal" /></label>
              <label>Z (north) <input value={f.z} onChange={set('z')} inputMode="decimal" /></label>
              <label>…or to player (Steam64) <input value={f.to_player} onChange={set('to_player')} /></label>
            </>}
            {a.name === 'give_item' && <>
              <label>Classname <input value={f.class} onChange={set('class')} placeholder="e.g. M4A1" /></label>
              <label>Quantity <input value={f.quantity} onChange={set('quantity')} type="number" min={1} max={20} /></label>
            </>}
            <label>Reason (audit) <input value={f.reason} onChange={set('reason')} maxLength={120} /></label>
          </div>
        } />
      )}
    </>
  )
}

function BanCard({ steam64, flags, reload }: { steam64: string; flags: Flags | null; reload: () => void }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [minutes, setMinutes] = useState('0')
  const [busy, setBusy] = useState(false)
  const [decision, setDecision] = useState('watch')
  const [note, setNote] = useState('')
  const notify = useNotify()
  const ban = async () => {
    setBusy(true)
    try {
      await api.post('/api/bans', { steam64, reason, minutes: parseInt(minutes, 10) || 0 })
      notify('Ban applied via BattlEye')
      reload()
      setOpen(false)
    } catch (e: any) {
      notify(e.message, 'bad')
    } finally {
      setBusy(false)
    }
  }
  const review = async () => {
    try {
      await api.post(`/api/players/${steam64}/review`, { decision, note })
      notify('Review saved')
      reload()
    } catch (e: any) {
      notify(e.message, 'bad')
    }
  }
  return (
    <Card title="Bans & external listings">
      {flags?.local_ban ? (
        <p><Badge state="bad">banned</Badge> {flags.local_ban.reason} · by {flags.local_ban.issuer_name} · {flags.local_ban.expires_at ? 'until ' + fmtTime(flags.local_ban.expires_at) : 'permanent'} (revoke on the Bans page)</p>
      ) : <button className="danger" onClick={() => setOpen(true)}>Ban…</button>}
      <h4>External listings</h4>
      {!flags?.external.length ? <p className="muted small">No matches in imported lists.</p> : (
        <>
          <ul>{flags.external.map((m, i) => <li key={i}><b>{m.list}</b> <Badge state={m.list_status === 'stale' ? 'warn' : 'info'}>{m.list_status}</Badge> {m.identifier_type}: {m.reason || 'no reason shared'} <span className="small muted">imported {fmtTime(m.imported_at)}</span></li>)}</ul>
          <p className="small warn">{flags.disclaimer}</p>
          {flags.review && <p className="small">Reviewed: <b>{flags.review.decision}</b> by {flags.review.reviewer} — {flags.review.note}</p>}
          <div className="row">
            <select value={decision} onChange={(e) => setDecision(e.target.value)}>
              <option value="ignored">ignore</option><option value="watch">watch</option><option value="banned_manually">banned manually</option>
            </select>
            <input placeholder="Rationale" value={note} onChange={(e) => setNote(e.target.value)} />
            <button onClick={review}>Save review</button>
          </div>
        </>
      )}
      {open && (
        <Confirm title="Ban player" word="BAN" busy={busy || !reason.trim()} onClose={() => setOpen(false)} onConfirm={ban} body={
          <div className="form">
            <p className="small muted">Enforced by BattlEye (RCON addBan) on the active server; online players are kicked.</p>
            <label>Reason <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={100} /></label>
            <label>Duration (minutes, 0 = permanent) <input type="number" min={0} value={minutes} onChange={(e) => setMinutes(e.target.value)} /></label>
          </div>
        } />
      )}
    </Card>
  )
}
