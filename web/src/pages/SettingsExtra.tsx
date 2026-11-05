import { useEffect, useState } from 'react'
import { api, AutoMsgConfig, MapState } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, Seg } from '../ui'
import { Icon } from '../icons'
import { useNotify } from '../App'
import { usePref } from '../prefs'
import { iconLabels, iconSvg, MapIcon, vehicleIcon, vehicleKinds, VehicleRule } from '../mapIcons'

/** IP lookup switch and optional Steam Web API key. */
export function PlayerChecks() {
  const s = useApi<{ ip_lookup: boolean; has_steam_key: boolean; ip_provider: string }>('/api/settings/intel')
  const [key, setKey] = useState('')
  const notify = useNotify()
  const save = async (body: Record<string, unknown>, ok: string) => {
    try { await api.put('/api/settings/intel', { ip_lookup: s.data?.ip_lookup ?? true, ...body }); notify(ok); setKey(''); s.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  const keyOk = /^[0-9A-Fa-f]{32}$/.test(key.trim())
  return (
    <Card title={<><Icon name="shield" />Player checks</>}>
      {!s.data ? <ErrorBox error={s.error} /> : <div className="stack">
        <label className="check"><input type="checkbox" checked={s.data.ip_lookup} onChange={(e) => save({ ip_lookup: e.target.checked }, e.target.checked ? 'IP checks on' : 'IP checks off')} />
          Look up country and VPN / data-center IPs <span className="muted small">(online players' IPs are sent to {s.data.ip_provider}, free tier, at most once a week per player)</span></label>
        <div className="field">Steam Web API key <span className="muted small">optional · adds game-ban counts and faster avatar lookups; without it the public Steam profile is used (VAC flag, avatar)</span>
          <div className="row">
            <input type="password" autoComplete="off" placeholder={s.data.has_steam_key ? 'A key is saved (encrypted). Paste a new one to replace it.' : '32 characters from steamcommunity.com/dev/apikey'} value={key} onChange={(e) => setKey(e.target.value)} aria-invalid={key !== '' && !keyOk} />
            <button className="small primary" disabled={!keyOk} onClick={() => save({ steam_key: key.trim() }, 'Steam key saved; players are re-checked in the background')}>Save key</button>
            {s.data.has_steam_key && <button className="small ghost" onClick={() => save({ steam_key: '' }, 'Steam key removed')}>Remove</button>}
          </div>
          {s.data.has_steam_key ? <Badge state="ok">key saved</Badge> : <Badge state="muted">no key</Badge>}
        </div>
        <p className="hint">Results are evidence only: nothing is kicked or banned automatically. Players are checked when they are online (IP) or first appear in a list (Steam), then refreshed daily.</p>
      </div>}
    </Card>
  )
}

const DELAYS: [number, string][] = [[15, '15 s'], [30, '30 s'], [60, '1 min'], [120, '2 min']]
const INTERVALS: [number, string][] = [[5, '5 min'], [10, '10 min'], [15, '15 min'], [30, '30 min'], [60, '1 h'], [120, '2 h']]
const WELCOMES = ['Welcome to the server, %playername%! Have fun and play fair.', 'Hey %playername%, welcome back! Rules and support: join our Discord.', 'Welcome %playername%! %players% survivors are online right now.']

/** Welcome message and rotating tips. */
export function AutoMessages() {
  const r = useApi<{ config: AutoMsgConfig; notify_available: boolean }>('/api/settings/messages')
  const [c, setC] = useState<AutoMsgConfig | null>(null)
  const [dirty, setDirty] = useState(false)
  const [draft, setDraft] = useState('')
  const notify = useNotify()
  useEffect(() => { if (r.data && !dirty) setC(r.data.config) }, [r.data])
  if (!c) return r.error ? <Card title="Automatic messages"><ErrorBox error={r.error} /></Card> : null
  const edit = (patch: Partial<AutoMsgConfig>) => { setC({ ...c, ...patch }); setDirty(true) }
  const w = c.welcome, rot = c.rotation
  const save = async () => {
    try { const out = await api.put<AutoMsgConfig>('/api/settings/messages', c); setC(out); setDirty(false); notify('Automatic messages saved') } catch (e: any) { notify(e.message, 'bad') }
  }
  const sendNow = async (text: string) => {
    try { await api.post('/api/settings/messages/send', { text }); notify('Sent to everyone') } catch (e: any) { notify(e.message, 'bad') }
  }
  const move = (i: number, d: number) => { const m = [...rot.messages]; const [x] = m.splice(i, 1); m.splice(i + d, 0, x); edit({ rotation: { ...rot, messages: m } }) }
  const custom = !WELCOMES.includes(w.text)
  return (
    <Card title={<><Icon name="megaphone" />Automatic messages</>} actions={<>{dirty && <span className="muted small">Unsaved changes</span>}
      {dirty && <button className="small ghost" onClick={() => { setC(r.data!.config); setDirty(false) }}>Discard</button>}
      <button className="small primary" disabled={!dirty} onClick={save}>Save</button></>}>
      <div className="field">Delivered as
        <Seg label="Channel" value={c.channel} onChange={(v) => edit({ channel: v })} options={[['rcon', 'Chat message (RCON)'], ['notify', 'On-screen notification']]} />
        {c.channel === 'notify' && !r.data?.notify_available && <span className="hint warn">The connected @DZMBridge doesn't offer on-screen notifications yet (coming with the next bridge release). Until then nothing is sent; use chat messages.</span>}
      </div>
      <div className="grid two" style={{ marginTop: 12, marginBottom: 0 }}>
        <div className="stack">
          <label className="check"><input type="checkbox" checked={w.enabled} onChange={(e) => edit({ welcome: { ...w, enabled: e.target.checked } })} /> <b>Welcome each player</b></label>
          <div className="option-list">
            {WELCOMES.map((t) => <label className="option" key={t}><input type="radio" name="welcome" disabled={!w.enabled} checked={w.text === t} onChange={() => edit({ welcome: { ...w, text: t } })} /><div><span style={{ color: 'var(--text)' }}>{t.replaceAll('%playername%', 'Alice').replaceAll('%players%', '12')}</span></div></label>)}
            <label className="option"><input type="radio" name="welcome" disabled={!w.enabled} checked={custom} onChange={() => edit({ welcome: { ...w, text: custom ? w.text : 'Welcome %playername%!' } })} /><div style={{ flex: 1 }}><b>Custom</b>
              {custom && <input value={w.text} maxLength={200} onChange={(e) => edit({ welcome: { ...w, text: e.target.value } })} style={{ marginTop: 6 }} />}
              <span>%playername% = the player's name · %players% = players online</span></div></label>
          </div>
          <div className="field">Send after joining<Seg label="Welcome delay" value={w.delay_seconds} onChange={(v) => edit({ welcome: { ...w, delay_seconds: v } })} options={DELAYS} /></div>
          <span className="hint">Players load into the world first, so a short delay makes sure they see it. Each player is welcomed at most once per 10 minutes.</span>
        </div>
        <div className="stack">
          <label className="check"><input type="checkbox" checked={rot.enabled} onChange={(e) => edit({ rotation: { ...rot, enabled: e.target.checked } })} /> <b>Rotating tips & announcements</b></label>
          <div className="field">Every<Seg label="Rotation interval" value={rot.interval_minutes} onChange={(v) => edit({ rotation: { ...rot, interval_minutes: v } })} options={INTERVALS} /></div>
          <label className="check small"><input type="checkbox" checked={rot.only_with_players} onChange={(e) => edit({ rotation: { ...rot, only_with_players: e.target.checked } })} /> Only when players are online</label>
          {!rot.messages.length ? <Empty>No messages yet.</Empty> : <ol className="msg-list">{rot.messages.map((m, i) => (
            <li key={i}><span className="grow">{m}</span>
              <button className="icon ghost" title="Move up" disabled={i === 0} onClick={() => move(i, -1)}>↑</button>
              <button className="icon ghost" title="Move down" disabled={i === rot.messages.length - 1} onClick={() => move(i, 1)}>↓</button>
              <button className="icon ghost" title="Send to everyone now" onClick={() => sendNow(m)}><Icon name="send" /></button>
              <button className="icon ghost" title="Remove" onClick={() => edit({ rotation: { ...rot, messages: rot.messages.filter((_, j) => j !== i) } })}><Icon name="close" /></button>
            </li>))}</ol>}
          <div className="row">
            <input placeholder="New message, e.g. Join our Discord for support" value={draft} maxLength={200} onChange={(e) => setDraft(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && draft.trim()) { edit({ rotation: { ...rot, messages: [...rot.messages, draft.trim()] } }); setDraft('') } }} />
            <button className="small" disabled={!draft.trim() || rot.messages.length >= 20} onClick={() => { edit({ rotation: { ...rot, messages: [...rot.messages, draft.trim()] } }); setDraft('') }}><Icon name="plus" />Add</button>
          </div>
          <span className="hint">Messages go out one at a time, in order. %players% = players online.</span>
        </div>
      </div>
    </Card>
  )
}

/** Which icon each vehicle class gets on the map (per account). */
export function VehicleIcons() {
  const map = useApi<MapState>('/api/map/state', ['map'], 30000)
  const [rules, setRules] = usePref<VehicleRule[]>('map.vehicleRules', [])
  const [match, setMatch] = useState('')
  const [kind, setKind] = useState<MapIcon>('bike')
  const classes = new Map<string, number>()
  for (const v of map.data?.vehicles ?? []) classes.set(v.class, (classes.get(v.class) ?? 0) + 1)
  const set = (m: string, k: MapIcon) => setRules([{ match: m, kind: k }, ...rules.filter((r) => r.match.toLowerCase() !== m.toLowerCase())].slice(0, 100))
  const ico = (k: MapIcon) => <span className={`entity-pin icon-${k}`} style={{ width: 16, height: 16, display: 'inline-block', verticalAlign: 'middle' }} dangerouslySetInnerHTML={{ __html: iconSvg(k) }} />
  return (
    <Card title={<><Icon name="map" />Map vehicle icons</>}>
      <p className="hint" style={{ marginTop: 0 }}>The map shows every vehicle MetricZ reports. If bikes or boats are missing from the list below, the game isn't reporting them (MetricZ only tracks vehicles it recognises as transport).</p>
      {!classes.size ? <Empty>No vehicles reported right now.</Empty> : <table><thead><tr><th>Vehicle class (live)</th><th className="num">On map</th><th>Shown as</th></tr></thead><tbody>
        {[...classes.entries()].sort().map(([cls, n]) => { const k = vehicleIcon({ class: cls, type: '' }, rules); return <tr key={cls}><td><code>{cls}</code></td><td className="num">{n}</td>
          <td><span className="row">{ico(k)}<select value={k} onChange={(e) => set(cls, e.target.value as MapIcon)} style={{ width: 150 }} aria-label={`Icon for ${cls}`}>{vehicleKinds.map((x) => <option key={x} value={x}>{iconLabels[x]}</option>)}</select></span></td></tr> })}
      </tbody></table>}
      <h4>Rules by name part</h4>
      {rules.length > 0 && <div className="chips" style={{ marginBottom: 8 }}>{rules.map((r) => <span className="chip" key={r.match}>{ico(r.kind)} contains “{r.match}”<button title="Remove rule" onClick={() => setRules(rules.filter((x) => x !== r))}><Icon name="close" /></button></span>)}</div>}
      <div className="row">
        <input placeholder="Class name contains, e.g. Bicycle" value={match} onChange={(e) => setMatch(e.target.value)} />
        <select value={kind} onChange={(e) => setKind(e.target.value as MapIcon)} style={{ width: 150 }} aria-label="Icon">{vehicleKinds.map((x) => <option key={x} value={x}>{iconLabels[x]}</option>)}</select>
        <button className="small" disabled={match.trim().length < 2} onClick={() => { set(match.trim(), kind); setMatch('') }}><Icon name="plus" />Add</button>
      </div>
    </Card>
  )
}
