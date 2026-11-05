import { useMemo, useState } from 'react'
import { api, ChatMessage, MapState, PerfPoint, Player, RconPlayer, ServerStatus } from '../api'
import { useApi } from '../hooks'
import { Badge, Empty, ErrorBox, Meter, Trunc } from '../ui'
import { Icon } from '../icons'
import { fmtDuration } from '../util'
import { useNotify } from '../App'
import { usePref } from '../prefs'
import { Sparkline } from '../charts'
import { Avatar, Flag, RiskIcon, useIntel } from '../intel'
import { eventDests, PlayerActions } from '../playerActions'
import { Marker } from '../api'
import MapPage from './MapPage'
import { ServerControls } from './Restarts'
import { kd } from './Players'

interface Ev { id: number; ts: string; type: string; actor: string; actor_name: string; data: Record<string, unknown> }
type FeedKind = 'chat' | 'join' | 'death'

/** Live server view: players and map side by side with a message box and live feed. */
export default function Live({ tab: initial }: { tab?: string }) {
  const [tabPref, setTab] = usePref<'players' | 'map'>('live.tab', 'players')
  const tab = (initial === 'map' || initial === 'players') ? initial : tabPref
  const [focus, setFocus] = useState<string | undefined>()
  const status = useApi<ServerStatus>('/api/server/status', ['server_status'])
  const map = useApi<MapState>('/api/map/state', ['map'], 15000)
  const perf = useApi<PerfPoint[]>('/api/perf?hours=2', [], 60000)
  const s = status.data
  const p = map.data?.available ? map.data.perf : undefined
  const fps = p?.fps_avg ?? p?.fps
  const go = (t: 'players' | 'map') => { setTab(t); if (initial) window.location.hash = '#/live' }
  return (
    <div className="live">
      <div className="live-main">
        <div className="live-head">
          <div className="live-title"><b>{s?.info?.name || 'Server'}</b><span className="muted small">{s?.info ? `${s.info.map} · v${s.info.version}` : ''}</span></div>
          <div className="kv"><span>Players</span><b>{s?.rcon.state === 'ok' ? s.players?.length ?? 0 : s?.info?.players ?? 0}{s?.info ? ` / ${s.info.max_players}` : ''}</b></div>
          <div className="kv"><span>Uptime</span><b>{fmtDuration(p?.uptime_seconds)}</b></div>
          <div className="kv"><span>Server FPS</span><b>{fps != null ? fps.toFixed(0) : '—'}</b></div>
          <div className="kv spark-kv"><Sparkline values={(perf.data ?? []).map((x) => x.fps)} color="var(--series-2)" /></div>
          <div className="grow" />
          {s?.server_id && <ServerControls />}
        </div>
        <div className="live-tabs" role="tablist">
          <button role="tab" aria-selected={tab === 'players'} onClick={() => go('players')}><Icon name="players" />Players</button>
          <button role="tab" aria-selected={tab === 'map'} onClick={() => go('map')}><Icon name="map" />Map</button>
        </div>
        <div className={'live-body' + (tab === 'map' ? ' is-map' : '')}>
          {tab === 'map' ? <MapPage focusId={focus} /> : <Online status={s} map={map.data} onShow={(id) => { setFocus(id); go('map') }} />}
        </div>
      </div>
      <Side status={s} />
    </div>
  )
}

function Online({ status, map, onShow }: { status?: ServerStatus | null; map?: MapState | null; onShow: (steam64: string) => void }) {
  const roster = useApi<{ rcon: ServerStatus['rcon']; players: RconPlayer[] | null }>('/api/server/players', ['server_status'])
  const hist = useApi<Player[]>('/api/players?limit=500', ['events'])
  const events = useApi<Marker[]>('/api/map/events', ['map_events'], 60000)
  const known = useMemo(() => new Map((hist.data || []).map((p) => [p.be_guid, p])), [hist.data])
  const online = roster.data?.players ?? []
  const ids = online.map((p) => known.get(p.be_guid)?.steam64)
  const intel = useIntel(ids)
  const live = new Map((map?.players ?? []).map((p) => [p.steam64, p]))
  const targets = online.flatMap((o) => { const k = known.get(o.be_guid); return k ? [{ steam64: k.steam64, name: o.name, be_guid: o.be_guid }] : [] })
  const dests = eventDests(events.data)
  return (
    <div className="live-pad">
      <ErrorBox error={roster.error} />
      {!online.length ? <Empty>{roster.data?.rcon.state === 'ok' || !roster.data ? 'Nobody online right now.' : 'RCON not connected: ' + (roster.data?.rcon.error || '')}</Empty> : (
        <div className="table-scroll"><table className="roster">
          <thead><tr><th>Player</th><th>Health</th><th className="num">Ping</th><th className="num">Playtime</th><th className="num">K/D</th><th>Actions</th></tr></thead>
          <tbody>
            {online.map((p) => {
              const k = known.get(p.be_guid), i = k ? intel[k.steam64] : undefined, m = k ? live.get(k.steam64) : undefined
              const h = m?.health
              return (
                <tr key={p.be_guid + p.number}>
                  <td>
                    <div className="who-cell">
                      <Avatar intel={i} name={p.name} />
                      <div className="who-text">
                        <span className="row" style={{ gap: 6 }}>
                          <Flag intel={i} />
                          {k ? <a href={'#/players/' + k.steam64}><Trunc text={p.name} max={200} /></a> : <Trunc text={p.name} max={200} />}
                          <RiskIcon intel={i} />
                          {p.lobby && <Badge state="info" title="Still loading into the world">lobby</Badge>}
                          {!p.verified && <Badge state="warn" title="BattlEye has not confirmed this player's GUID with its master server yet. Normal for a few seconds after joining; if it stays, BattlEye's master server is unreachable. It is not a ban or a cheat flag.">GUID pending</Badge>}
                        </span>
                        <span className="muted small">{i?.country || (k ? 'Steam64 ' + k.steam64 : 'not linked to a Steam64 yet')}</span>
                      </div>
                    </div>
                  </td>
                  <td style={{ minWidth: 90 }}>{h != null ? <><Meter value={h} color={h > 0.6 ? '#3ccf7e' : h > 0.3 ? '#f1c40f' : '#ef5b4c'} /><span className="muted small">{Math.round(h * 100)}%</span></> : <span className="muted small">—</span>}</td>
                  <td className="num"><span className={p.ping < 80 ? 'okc' : p.ping < 160 ? 'warn' : ''}>{p.ping} ms</span></td>
                  <td className="num">{k ? fmtDuration(k.playtime_seconds) : '—'}</td>
                  <td className="num">{k ? kd(k.kills, k.deaths) : '—'}</td>
                  <td>
                    <div className="row" style={{ gap: 6 }}>
                      <PlayerActions target={{ steam64: k?.steam64 || '', name: p.name, be_guid: p.be_guid }} only={['message', 'heal', 'teleport', 'kick']} players={targets} dests={dests} />
                      <button className="small ghost" disabled={!k || !m} title={k && m ? 'Show on the map' : 'Not on the map'} onClick={() => k && onShow(k.steam64)}><Icon name="map" /></button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table></div>
      )}
      {status?.rcon.state === 'ok' && <p className="hint">Roster from BattlEye RCON, health from MetricZ, country and risk flags from Steam and the IP lookup (see Settings → Player checks).</p>}
    </div>
  )
}

const FEED_KINDS: [FeedKind, string][] = [['chat', 'Chat'], ['join', 'Joins & leaves'], ['death', 'Deaths']]

function Side({ status }: { status?: ServerStatus | null }) {
  const notify = useNotify()
  const [msg, setMsg] = useState('')
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState(false)
  const [kinds, setKinds] = usePref<FeedKind[]>('live.feed', ['chat', 'join', 'death'])
  const chat = useApi<ChatMessage[]>('/api/chat?limit=80', ['chat'])
  const evs = useApi<{ items: Ev[] }>('/api/events?limit=80&types=player.connect,player.disconnect,player.death', ['events'])
  const players = status?.players ?? []
  const rconOk = status?.rcon.state === 'ok'
  const send = async () => {
    if (!msg.trim()) return
    setBusy(true)
    try {
      await api.post('/api/rcon/say', to ? { message: msg.trim(), be_guid: to } : { message: msg.trim() })
      notify(to ? 'Private message sent' : 'Message sent to everyone')
      setMsg('')
    } catch (e: any) { notify(e.message, 'bad') } finally { setBusy(false) }
  }
  const feed = useMemo(() => {
    const out: { key: string; ts: string; kind: FeedKind; who: string; steam?: string; text: string; cls: string }[] = []
    for (const c of chat.data ?? []) out.push({ key: 'c' + c.id, ts: c.ts, kind: 'chat', who: c.name, text: c.text, cls: c.channel === 'admin' ? 'admin' : 'chat' })
    for (const e of evs.data?.items ?? []) {
      const name = e.actor_name || e.actor
      if (e.type === 'player.connect') out.push({ key: 'e' + e.id, ts: e.ts, kind: 'join', who: name, steam: e.actor, text: 'joined', cls: 'join' })
      else if (e.type === 'player.disconnect') out.push({ key: 'e' + e.id, ts: e.ts, kind: 'join', who: name, steam: e.actor, text: 'left', cls: 'leave' })
      else if (e.type === 'player.death') {
        const byPlayer = typeof e.data?.killer === 'string' && e.data.killer !== ''
        const weapon = typeof e.data?.weapon === 'string' ? e.data.weapon : ''
        const cause = typeof e.data?.cause === 'string' ? e.data.cause : ''
        out.push({ key: 'e' + e.id, ts: e.ts, kind: 'death', who: name, steam: e.actor, text: byPlayer ? `was killed by a player${weapon ? ' (' + weapon + ')' : ''}` : cause ? `died (${cause})` : 'died', cls: 'death' })
      }
    }
    return out.filter((x) => kinds.includes(x.kind)).sort((a, b) => b.ts.localeCompare(a.ts)).slice(0, 120)
  }, [chat.data, evs.data, kinds])
  return (
    <aside className="live-side" aria-label="Messages and live feed">
      <div className="side-send">
        <div className="row">
          <select value={to} onChange={(e) => setTo(e.target.value)} aria-label="Recipient" disabled={!rconOk}>
            <option value="">Everyone</option>
            {players.map((p) => <option key={p.be_guid} value={p.be_guid}>{p.name}</option>)}
          </select>
        </div>
        <div className="row">
          <input value={msg} maxLength={200} disabled={!rconOk || busy} placeholder={rconOk ? (to ? 'Private message…' : 'Message to all players…') : 'RCON not connected'}
            onChange={(e) => setMsg(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') send() }} />
          <button className="primary icon" style={{ width: 34, height: 30 }} disabled={!rconOk || busy || !msg.trim()} onClick={send} title="Send (Enter)"><Icon name="send" /></button>
        </div>
      </div>
      <div className="feed-head">
        <b>Live feed</b>
        <span className="chips">{FEED_KINDS.map(([k, l]) => <button key={k} type="button" className="chip toggle" aria-pressed={kinds.includes(k)} onClick={() => setKinds(kinds.includes(k) ? kinds.filter((x) => x !== k) : [...kinds, k])}>{l}</button>)}</span>
      </div>
      <div className="feed" aria-live="polite">
        {!feed.length ? <Empty>Nothing yet. In-game chat appears here when BattlEye reports it.</Empty> : feed.map((f) => (
          <div key={f.key} className={'feed-row ' + f.cls} title={new Date(f.ts).toLocaleString()}>
            <span className="t">{new Date(f.ts).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })}</span>
            <span className="m">{f.steam ? <a href={'#/players/' + f.steam}>{f.who}</a> : <b>{f.who}</b>}{f.kind === 'chat' ? ': ' : ' '}<span>{f.text}</span></span>
          </div>
        ))}
      </div>
    </aside>
  )
}
