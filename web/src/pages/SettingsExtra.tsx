import { useEffect, useState } from 'react'
import { api, AutoMsgConfig, MapState } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, Pager, Seg, usePaged } from '../ui'
import { Icon } from '../icons'
import { useNotify } from '../App'
import { usePref } from '../prefs'
import { iconLabels, iconSvg, MapIcon, vehicleIcon, vehicleKinds, VehicleRule } from '../mapIcons'

/** Emergency stop: SSH key pinned to a kill-only wrapper on the game host. */
export function HostControl() {
  const h = useApi<{ host: string; port: number; user: string; ready: boolean; authorized_keys_line: string; fingerprint: string; wrapper_path: string; wrapper_script: string }>('/api/settings/host')
  const [f, setF] = useState<{ host: string; port: number; user: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [rotate, setRotate] = useState(false)
  const notify = useNotify()
  useEffect(() => { if (h.data && !f) setF({ host: h.data.host, port: h.data.port, user: h.data.user }) }, [h.data])
  if (!h.data || !f) return h.error ? <Card title="Emergency stop"><ErrorBox error={h.error} /></Card> : null
  const valid = /^[A-Za-z0-9.-]{1,253}$/.test(f.host) && f.port >= 1 && f.port <= 65535 && /^[a-z_][a-z0-9_-]{0,31}$/.test(f.user)
  const run = async (fn: () => Promise<unknown>, ok: string) => { setBusy(true); try { await fn(); notify(ok); h.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setBusy(false) } }
  return (
    <Card title={<><Icon name="skull" />Emergency stop (kill a frozen server)</>} actions={h.data.ready ? <Badge state="ok">ready</Badge> : <Badge state="muted">not set up</Badge>}>
      <p className="hint" style={{ marginTop: 0 }}>For a server that hangs and ignores RCON. DZ Manager connects over SSH with its own key; on the game host that key can only run a two-command wrapper (status, kill). Architect restarts the process afterwards.</p>
      <div className="field-row">
        <label className="field">Game host<input value={f.host} onChange={(e) => setF({ ...f, host: e.target.value.trim() })} placeholder="192.0.2.10" /></label>
        <label className="field">SSH port<input type="number" min={1} max={65535} value={f.port} onChange={(e) => setF({ ...f, port: +e.target.value })} /></label>
        <label className="field">User<input value={f.user} onChange={(e) => setF({ ...f, user: e.target.value.trim() })} /></label>
      </div>
      <div className="row end">
        <button className="small" disabled={!valid || busy} onClick={() => run(() => api.put('/api/settings/host', f), 'Host saved')}>Save host</button>
        <button className="small" disabled={busy} onClick={() => h.data!.authorized_keys_line ? setRotate(true) : run(() => api.post('/api/settings/host/key'), 'Key created')}><Icon name="lock" />{h.data.authorized_keys_line ? 'New key…' : 'Create key'}</button>
        <button className="small" disabled={!h.data.ready || busy} onClick={() => run(async () => { const r = await api.post<{ status: string }>('/api/server/host-status'); notify('Game host answers: server ' + r.status) }, 'Connection works')}>Test connection</button>
      </div>
      {h.data.authorized_keys_line && <>
        <h4>1 · Wrapper on the game host ({h.data.wrapper_path}, chmod 755)</h4>
        <pre>{h.data.wrapper_script}</pre>
        <h4 className="row">2 · Line for {f.user}'s ~/.ssh/authorized_keys<CopyButton value={h.data.authorized_keys_line} label="Copy line" /></h4>
        <pre className="wrap">{h.data.authorized_keys_line}</pre>
        <p className="hint">Key fingerprint {h.data.fingerprint}. The private key is stored encrypted and never shown.</p>
      </>}
      {rotate && <Confirm title="Create a new key?" confirmLabel="Create key" onClose={() => setRotate(false)} onConfirm={() => { setRotate(false); run(() => api.post('/api/settings/host/key'), 'New key created: replace the line on the game host') }} body={<p>The current key stops working until you replace the line in authorized_keys on the game host.</p>} />}
    </Card>
  )
}

/** Discord webhooks per channel. */
export function DiscordChannels() {
  const d = useApi<{ key: string; label: string; help: string; enabled: boolean; has_url: boolean }[]>('/api/settings/discord')
  const [urls, setUrls] = useState<Record<string, string>>({})
  const notify = useNotify()
  const re = /^https:\/\/(discord\.com|discordapp\.com|ptb\.discord\.com|canary\.discord\.com)\/api\/webhooks\/\d{5,25}\/[A-Za-z0-9_-]{20,100}$/
  const put = async (key: string, body: Record<string, unknown>, ok: string) => {
    try { await api.put('/api/settings/discord/' + key, body); notify(ok); setUrls({ ...urls, [key]: '' }); d.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  const test = async (key: string) => { try { await api.post(`/api/settings/discord/${key}/test`); notify('Test message sent to Discord') } catch (e: any) { notify(e.message, 'bad') } }
  return (
    <Card title={<><Icon name="send" />Discord channels</>}>
      <p className="hint" style={{ marginTop: 0 }}>In Discord: channel settings → Integrations → Webhooks → New webhook → Copy URL, then paste it here. Each channel only gets its kind of message; URLs are stored encrypted.</p>
      <div className="table-scroll"><table>
        <thead><tr><th>Channel</th><th style={{ width: '50%' }}>Webhook</th><th>On</th><th /></tr></thead>
        <tbody>{(d.data ?? []).map((c) => {
          const u = urls[c.key] ?? ''
          return <tr key={c.key}>
            <td><b>{c.label}</b><div className="muted small">{c.help}</div></td>
            <td style={{ minWidth: 280 }}>
              <div className="row">
                <input type="password" autoComplete="off" placeholder={c.has_url ? 'Saved · paste a new URL to replace' : 'https://discord.com/api/webhooks/…'} value={u} aria-invalid={u !== '' && !re.test(u.trim())} onChange={(e) => setUrls({ ...urls, [c.key]: e.target.value })} />
                <button className="small" disabled={!re.test(u.trim())} onClick={() => put(c.key, { enabled: true, url: u.trim() }, c.label + ' connected')}>Save</button>
              </div>
            </td>
            <td><label className="switch" title={c.has_url ? 'Send to this channel' : 'Save a webhook first'}><input type="checkbox" checked={c.enabled} disabled={!c.has_url} onChange={(e) => put(c.key, { enabled: e.target.checked }, e.target.checked ? c.label + ' on' : c.label + ' off')} /><span /></label></td>
            <td className="num"><div className="btn-group" style={{ justifyContent: 'flex-end' }}>
              <button className="small ghost" disabled={!c.has_url} onClick={() => test(c.key)}>Test</button>
              {c.has_url && <button className="small ghost" onClick={() => put(c.key, { enabled: false, url: '' }, c.label + ' disconnected')}>Remove</button>}
            </div></td>
          </tr>
        })}</tbody>
      </table></div>
    </Card>
  )
}

const BOT_ENDPOINTS: [string, string][] = [
  ['/public/v1/status', 'Name, players online, uptime, FPS, next restart'],
  ['/public/v1/online', 'Names of players online'],
  ['/public/v1/killfeed?limit=20', 'Latest kills: killer, victim, weapon, distance'],
  ['/public/v1/leaderboard?by=kills|playtime|kd', 'Top players'],
  ['/public/v1/player?name=Alice', 'Stats for one player by name'],
  ['/public/v1/bans', 'Active bans: name, reason, until'],
  ['/public/v1/events', 'How many helicrashes/airdrops/convoys are active (no locations)'],
  ['/public/v1/info', 'Your server info text below (rules, mechanics, FAQ)'],
  ['/public/v1/feed?after=0', 'Every public event in order (kills, joins, raids, bans, restarts); poll with the last id'],
]

/** Token-protected read-only API for a Discord bot. */
export function BotApi() {
  const b = useApi<{ enabled: boolean; created_at: string; info: string; base_path: string }>('/api/settings/bot')
  const [token, setToken] = useState('')
  const [info, setInfo] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<'new' | 'revoke' | null>(null)
  const notify = useNotify()
  if (!b.data) return b.error ? <Card title="Bot API"><ErrorBox error={b.error} /></Card> : null
  const text = info ?? b.data.info
  const newToken = async () => { try { const r = await api.post<{ token: string }>('/api/settings/bot/token'); setToken(r.token); b.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setConfirm(null) } }
  const revoke = async () => { try { await api.post('/api/settings/bot/revoke'); setToken(''); notify('Bot token revoked'); b.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setConfirm(null) } }
  const saveInfo = async () => { try { await api.put('/api/settings/bot/info', { info: text }); setInfo(null); notify('Server info saved'); b.reload() } catch (e: any) { notify(e.message, 'bad') } }
  return (
    <Card title={<><Icon name="chat" />Bot API (for a Discord bot)</>} actions={b.data.enabled ? <Badge state="ok">token active</Badge> : <Badge state="muted">no token</Badge>}>
      <p className="hint" style={{ marginTop: 0 }}>Read-only and public-safe: no Steam IDs, IPs or exact event locations. Send the token as <code>Authorization: Bearer …</code> to <code>{window.location.origin}{b.data.base_path}</code>.</p>
      <div className="row">
        <button className="small primary" onClick={() => b.data!.enabled ? setConfirm('new') : newToken()}>{b.data.enabled ? 'New token…' : 'Create token'}</button>
        {b.data.enabled && <button className="small ghost" onClick={() => setConfirm('revoke')}>Revoke</button>}
        {b.data.created_at && <span className="muted small">created {new Date(b.data.created_at).toLocaleString()}</span>}
      </div>
      {token && <div className="warnings" style={{ marginTop: 10 }}><b>Copy this token now, it is shown only once:</b> <span className="id-line"><code>{token}</code><CopyButton value={token} label="Copy token" /></span></div>}
      <h4>Endpoints</h4>
      <table className="small"><tbody>{BOT_ENDPOINTS.map(([p, d]) => <tr key={p}><td><code>GET {p}</code></td><td className="muted">{d}</td></tr>)}</tbody></table>
      <h4>Server info for the bot</h4>
      <textarea rows={6} value={text} maxLength={20000} placeholder={'Rules, wipe schedule, how raiding works, trader locations, Discord links…\nThe bot can quote this when players ask questions.'} onChange={(e) => setInfo(e.target.value)} />
      <div className="row end"><span className="muted small">{text.length}/20000</span><button className="small" disabled={info == null} onClick={saveInfo}>Save info</button></div>
      {confirm && <Confirm title={confirm === 'new' ? 'Create a new token?' : 'Revoke the bot token?'} confirmLabel={confirm === 'new' ? 'Create token' : 'Revoke'} onClose={() => setConfirm(null)} onConfirm={confirm === 'new' ? newToken : revoke}
        body={<p>The current token stops working immediately{confirm === 'new' ? '; update your bot with the new one' : ''}.</p>} />}
    </Card>
  )
}

/** Data retention: archive old rows into compressed files. */
export function Retention() {
  const r = useApi<{ settings: { archive_after_days: number; keep_archive_days: number }; archive_after: number[]; keep_archives: number[]; db_bytes: number; rows: Record<string, number>; archives: { name: string; table: string; bytes: number; created: string }[] }>('/api/settings/retention')
  const [busy, setBusy] = useState(false)
  const notify = useNotify()
  const pg = usePaged(r.data?.archives, 10)
  if (!r.data) return r.error ? <Card title="Data retention"><ErrorBox error={r.error} /></Card> : null
  const st = r.data.settings
  const save = async (patch: Partial<typeof st>) => { try { await api.put('/api/settings/retention', { ...st, ...patch }); notify('Retention saved'); r.reload() } catch (e: any) { notify(e.message, 'bad') } }
  const runNow = async () => {
    setBusy(true)
    try { const out = await api.post<{ archived: Record<string, number> }>('/api/archives/run'); const n = Object.values(out.archived).reduce((a, b) => a + b, 0); notify(n ? `Archived ${n.toLocaleString()} rows` : 'Nothing old enough to archive'); r.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setBusy(false) }
  }
  const mb = (b: number) => (b / 1048576).toFixed(1) + ' MB'
  const labels: Record<string, string> = { events: 'Activity events', chat_messages: 'Chat', map_events: 'Map markers', metricz_perf: 'Performance samples', bridge_commands: 'Bridge commands' }
  return (
    <Card title={<><Icon name="audit" />Data retention</>} actions={<button className="small" disabled={busy} onClick={runNow}>{busy ? 'Archiving…' : 'Archive now'}</button>}>
      <p className="hint" style={{ marginTop: 0 }}>Every day, finished records older than the first period are moved out of the database into compressed files (gzip, one per table and day); files older than the second period are deleted. The audit log is never archived.</p>
      <div className="field-row">
        <div className="field">Keep in the database<Seg label="Archive after" value={st.archive_after_days} onChange={(v) => save({ archive_after_days: v })} options={r.data.archive_after.map((d) => [d, d + ' days'])} /></div>
        <div className="field">Keep archive files<Seg label="Keep archives" value={st.keep_archive_days} onChange={(v) => save({ keep_archive_days: v })} options={r.data.keep_archives.map((d) => [d, d >= 365 ? d / 365 + (d === 365 ? ' year' : ' years') : d + ' days'])} /></div>
      </div>
      <div className="statline" style={{ margin: '14px 0' }}>
        <div><span className="k">Database</span><span className="v">{mb(r.data.db_bytes)}</span></div>
        {Object.entries(r.data.rows).map(([t, n]) => <div key={t}><span className="k">{labels[t] || t}</span><span className="v">{n.toLocaleString()}</span></div>)}
      </div>
      <h4>Archives</h4>
      {!r.data.archives.length ? <Empty>No archives yet.</Empty> : <>
        <table><tbody>{pg.rows.map((a) => <tr key={a.name}><td><a href={'/api/archives/' + encodeURIComponent(a.name)} download>{a.name}</a></td><td className="muted">{labels[a.table] || a.table}</td><td className="num">{mb(a.bytes)}</td><td className="muted small">{new Date(a.created).toLocaleDateString()}</td></tr>)}</tbody></table>
        <Pager {...pg} noun="files" />
      </>}
    </Card>
  )
}

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
