import { useMemo, useState } from 'react'
import { api, Flags, MapState, Marker, PlayerProfile } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, IdValue, PageHead, Pager, Seg, StatTile, steamUrl, Trunc, usePaged } from '../ui'
import { ago, fmtDuration, fmtTime } from '../util'
import { useNotify } from '../App'
import { eventDests, PlayerActions } from '../playerActions'
import { Bars } from '../charts'
import { Icon } from '../icons'
import { LogDetails, readable } from '../logs'
import { kd } from './Players'

const DAY = 86400000

export default function PlayerPage({ id }: { id: string }) {
  const prof = useApi<PlayerProfile>('/api/players/' + encodeURIComponent(id), ['events'])
  const flags = useApi<Flags>('/api/players/' + encodeURIComponent(id) + '/flags')
  const live = useApi<MapState>('/api/map/state', ['map'], 30000)
  const events = useApi<Marker[]>('/api/map/events', ['map_events'], 60000)
  const d = prof.data
  const stats = useMemo(() => {
    if (!d) return null
    const sessions = d.sessions
    const total = sessions.reduce((s, x) => s + (x.duration_seconds || 0), 0)
    const kills = d.combat.filter((c) => c.role === 'kill')
    const longest = kills.reduce((m, c) => Math.max(m, c.distance_m ?? 0), 0)
    const weapons = new Map<string, number>()
    for (const k of kills) if (k.weapon) weapons.set(k.weapon, (weapons.get(k.weapon) || 0) + 1)
    const fav = [...weapons.entries()].sort((a, b) => b[1] - a[1])[0]
    // Playtime per day for the last 14 days (local days), split across midnight.
    const today = new Date(); today.setHours(0, 0, 0, 0)
    const days = Array.from({ length: 14 }, (_, i) => ({ start: today.getTime() - (13 - i) * DAY, s: 0 }))
    for (const s of sessions) {
      const a = new Date(s.started_at).getTime(), b = s.ended_at ? new Date(s.ended_at).getTime() : Date.now()
      for (const day of days) {
        const ov = Math.min(b, day.start + DAY) - Math.max(a, day.start)
        if (ov > 0) day.s += ov / 1000
      }
    }
    return { count: sessions.length, avg: sessions.length ? total / sessions.length : 0, longest, fav, days, online: sessions.some((s) => !s.ended_at) }
  }, [d])
  if (prof.error) return <><p><a href="#/players">← Players</a></p><ErrorBox error={prof.error} /></>
  const p = d?.player
  if (!p || !d || !stats) return <div className="loading">Loading…</div>
  const livePlayer = live.data?.players?.find((x) => x.steam64 === p.steam64)
  const others = (live.data?.players ?? []).map((x) => ({ steam64: x.steam64, name: x.name || x.steam64, be_guid: x.be_guid }))

  return (
    <>
      <p className="small"><a href="#/players">← Players</a></p>
      <PageHead
        title={<span className="row wrap">{p.name || '(unknown)'}
          {stats.online ? <Badge state="ok">online</Badge> : <Badge state="muted" title={fmtTime(p.last_seen)}>seen {ago(p.last_seen)}</Badge>}
          {flags.data?.local_ban && <Badge state="bad">banned</Badge>}
          {!!flags.data?.external.length && <Badge state="warn">external listing</Badge>}</span>}
        actions={<a className="button small" href={steamUrl(p.steam64)} target="_blank" rel="noreferrer noopener"><Icon name="external" />Steam profile</a>}
      />
      <div className="tiles">
        <StatTile label="Playtime" value={fmtDuration(p.playtime_seconds)} foot={`${stats.count} sessions · avg ${fmtDuration(stats.avg)}`} />
        <StatTile label="Kills" value={p.kills} foot={stats.fav ? `Favourite: ${stats.fav[0]}` : 'No kills recorded'} />
        <StatTile label="Deaths" value={p.deaths} foot={`K/D ${kd(p.kills, p.deaths)}`} />
        <StatTile label="Longest kill" value={stats.longest ? Math.round(stats.longest) : '—'} unit={stats.longest ? 'm' : undefined} foot="From bridge kill reports" />
        <StatTile label="First seen" value={<span style={{ fontSize: 16 }}>{new Date(p.first_seen).toLocaleDateString()}</span>} foot={ago(p.first_seen)} />
      </div>
      <div className="grid two">
        <Card title="Identity">
          <dl>
            <dt>Steam64</dt><dd className="id-line"><a href={steamUrl(p.steam64)} target="_blank" rel="noreferrer noopener" title="Open Steam profile"><code>{p.steam64}</code></a><CopyButton value={p.steam64} label="Copy Steam64" /></dd>
            <dt>BE GUID</dt><dd><IdValue value={p.be_guid} label="BE GUID" /></dd>
            <dt>DayZ ID</dt><dd><IdValue value={p.dayz_id} label="DayZ ID" /></dd>
            <dt>Last seen</dt><dd title={fmtTime(p.last_seen)}>{ago(p.last_seen)}</dd>
            {d.names.length > 1 && <><dt>Also known as</dt><dd><Trunc text={d.names.map((n) => n.name).join(', ')} max={300} /></dd></>}
          </dl>
          {p.id_mismatch && <p className="hint warn">A source reported a BE GUID that does not match this Steam64. The computed value is shown.</p>}
        </Card>
        <Card title="Quick actions" actions={livePlayer ? <Badge state="ok">on the map</Badge> : <Badge state="muted">not on the map</Badge>}>
          <PlayerActions target={{ steam64: p.steam64, name: p.name, be_guid: p.be_guid }} dests={eventDests(events.data)} players={others} />
          <p className="hint">Mod actions run through @DZMBridge and report the in-game result; unavailable ones are disabled with the reason on hover. <a href="#/map">Open the map</a> to drag-teleport.</p>
          <BanCard steam64={p.steam64} flags={flags.data} reload={flags.reload} />
        </Card>
      </div>
      <Card title="Playtime, last 14 days">
        <Bars color="var(--series-1)" unit=" h" fmt={(v) => (Math.round(v * 10) / 10).toString()}
          items={stats.days.map((x) => ({ label: new Date(x.start).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }), v: x.s / 3600, tip: new Date(x.start).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' }) }))} />
      </Card>
      <Sessions sessions={d.sessions} />
      <Combat combat={d.combat} />
      <History audit={d.audit} />
    </>
  )
}

function Sessions({ sessions }: { sessions: PlayerProfile['sessions'] }) {
  const pg = usePaged(sessions, 10)
  return (
    <Card title="Sessions" actions={<span className="muted small">{sessions.length} total</span>}>
      {!sessions.length ? <Empty>No sessions recorded.</Empty> : <>
        <div className="table-scroll"><table>
          <thead><tr><th>Joined</th><th>Left</th><th className="num">Duration</th><th>How it ended</th></tr></thead>
          <tbody>
            {pg.rows.map((s, i) => (
              <tr key={i}>
                <td title={fmtTime(s.started_at)}>{new Date(s.started_at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}</td>
                <td>{s.ended_at ? new Date(s.ended_at).toLocaleTimeString(undefined, { timeStyle: 'short' }) : <Badge state="ok">online now</Badge>}</td>
                <td className="num">{fmtDuration(s.duration_seconds)}</td>
                <td>{s.end_kind === 'inferred_end' ? <Badge state="warn" title="No disconnect was reported; the session was closed by a crash or restart">server restart / crash</Badge> : s.ended_at ? <span className="muted">{readable(s.end_kind)}</span> : null}</td>
              </tr>
            ))}
          </tbody>
        </table></div>
        <Pager {...pg} noun="sessions" />
      </>}
    </Card>
  )
}

function Combat({ combat }: { combat: PlayerProfile['combat'] }) {
  const [f, setF] = useState<'all' | 'kill' | 'death'>('all')
  const rows = combat.filter((c) => f === 'all' || c.role === f)
  const pg = usePaged(rows, 10)
  return (
    <Card title="Kills & deaths" actions={<Seg label="Filter" value={f} onChange={(v) => { setF(v); pg.setPage(1) }} options={[['all', 'All'], ['kill', 'Kills'], ['death', 'Deaths']]} />}>
      {!rows.length ? <Empty>No {f === 'all' ? 'kills or deaths' : f + 's'} recorded.</Empty> : <>
        <div className="table-scroll"><table>
          <thead><tr><th>When</th><th>What happened</th><th>Weapon</th><th className="num">Distance</th></tr></thead>
          <tbody>
            {pg.rows.map((c, i) => (
              <tr key={i}>
                <td title={fmtTime(c.ts)}>{ago(c.ts)}</td>
                <td>{c.role === 'kill'
                  ? <><Badge state="info">kill</Badge> killed <a href={'#/players/' + c.victim}>{c.victim}</a></>
                  : c.killer ? <><Badge state="bad">death</Badge> killed by <a href={'#/players/' + c.killer}>{c.killer}</a></>
                    : <><Badge state="bad">death</Badge> <span className="muted">{c.cause ? readable(c.cause) : 'environment / unknown'}</span></>}</td>
                <td><Trunc text={c.weapon} max={180} /></td>
                <td className="num">{c.distance_m != null ? c.distance_m.toFixed(0) + ' m' : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table></div>
        <Pager {...pg} noun="entries" />
      </>}
    </Card>
  )
}

function History({ audit }: { audit: PlayerProfile['audit'] }) {
  const pg = usePaged(audit, 10)
  return (
    <Card title="Admin history">
      {!audit.length ? <Empty>No admin actions for this player.</Empty> : <>
        <div className="table-scroll"><table>
          <thead><tr><th>When</th><th>Who</th><th>Action</th><th>Result</th><th>Details</th></tr></thead>
          <tbody>
            {pg.rows.map((a) => (
              <tr key={a.id}>
                <td title={fmtTime(a.ts)} className="nowrap">{ago(a.ts)}</td>
                <td>{a.actor_name === 'system' ? 'Automatic' : a.actor_name}</td>
                <td>{readable(a.action)}</td>
                <td><Badge state={a.result}>{a.result === 'ok' ? 'Completed' : a.result === 'error' ? 'Failed' : readable(a.result)}</Badge></td>
                <td><LogDetails params={a.params} detail={a.detail} /></td>
              </tr>
            ))}
          </tbody>
        </table></div>
        <Pager {...pg} noun="actions" />
      </>}
    </Card>
  )
}

const BAN_DURATIONS: [number, string][] = [[60, '1 hour'], [1440, '1 day'], [4320, '3 days'], [10080, '7 days'], [43200, '30 days'], [0, 'Permanent']]
const BAN_REASONS = ['Cheating / hacking', 'Exploiting / duping', 'Combat logging', 'Toxic behaviour', 'Ban evasion']

function BanCard({ steam64, flags, reload }: { steam64: string; flags: Flags | null; reload: () => void }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState(BAN_REASONS[0])
  const [minutes, setMinutes] = useState(1440)
  const [busy, setBusy] = useState(false)
  const [decision, setDecision] = useState('watch')
  const [note, setNote] = useState('')
  const notify = useNotify()
  const ban = async () => {
    setBusy(true)
    try {
      await api.post('/api/bans', { steam64, reason, minutes })
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
    <div className="qa-section">
      <h4>Bans & listings</h4>
      {flags?.local_ban ? (
        <p className="small"><Badge state="bad">banned</Badge> {flags.local_ban.reason} · by {flags.local_ban.issuer_name} · {flags.local_ban.expires_at ? 'until ' + fmtTime(flags.local_ban.expires_at) : 'permanent'} <a href="#/bans">Manage on the Bans page</a></p>
      ) : <button className="small danger" onClick={() => setOpen(true)}><Icon name="bans" />Ban…</button>}
      {!flags?.external.length ? <p className="hint">No matches in imported external lists.</p> : (
        <>
          <ul className="small">{flags.external.map((m, i) => <li key={i}><b>{m.list}</b> <Badge state={m.list_status === 'stale' ? 'warn' : 'info'}>{m.list_status}</Badge> {m.identifier_type}: {m.reason || 'no reason shared'} <span className="muted">imported {ago(m.imported_at)}</span></li>)}</ul>
          <p className="hint warn">{flags.disclaimer}</p>
          {flags.review && <p className="small">Reviewed: <b>{flags.review.decision}</b> by {flags.review.reviewer} — {flags.review.note}</p>}
          <div className="row wrap">
            <Seg label="Decision" value={decision} onChange={setDecision} options={[['ignored', 'Ignore'], ['watch', 'Watch'], ['banned_manually', 'Banned manually']]} />
            <input placeholder="Rationale" value={note} onChange={(e) => setNote(e.target.value)} style={{ flex: 1, minWidth: 140 }} />
            <button className="small" onClick={review}>Save review</button>
          </div>
        </>
      )}
      {open && (
        <Confirm title="Ban player" word="BAN" busy={busy} disabled={!reason.trim()} confirmLabel="Ban" onClose={() => setOpen(false)} onConfirm={ban} body={
          <div className="form">
            <p className="small muted">Enforced by BattlEye (RCON addBan) on the active server; online players are kicked.</p>
            <label className="field">Reason
              <select value={BAN_REASONS.includes(reason) ? reason : ''} onChange={(e) => setReason(e.target.value)}>
                {BAN_REASONS.map((r) => <option key={r}>{r}</option>)}
                <option value="">Other…</option>
              </select>
            </label>
            {!BAN_REASONS.includes(reason) && <input autoFocus placeholder="Reason shown in the ban list" value={reason} onChange={(e) => setReason(e.target.value)} maxLength={100} />}
            <div className="field">Duration
              <Seg label="Duration" value={minutes} onChange={setMinutes} options={BAN_DURATIONS} />
            </div>
          </div>
        } />
      )}
    </div>
  )
}
