import { useEffect, useState } from 'react'
import { api, AutoMsgConfig } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, Pager, Seg, usePaged } from '../ui'
import { Icon } from '../icons'
import { useNotify } from '../App'

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

interface ApiScope { name: string; label: string; help: string; sensitive: boolean }
interface ApiKey { id: string; name: string; prefix: string; scopes: string[]; created_at: string; created_by: string; expires_at?: string; last_used_at?: string; revoked_at?: string }
const EXPIRY: [number, string][] = [[0, 'Never'], [30, '30 days'], [90, '90 days'], [365, '1 year']]
const PRESETS: [string, string, string[]][] = [
  ['website', 'Website / Discord bot', ['public']],
  ['sharing', 'Share bans and priority with another server', ['bans:read', 'priority:read']],
  ['mcp-read', 'MCP client, read only', ['public', 'players:read', 'bans:read', 'priority:read', 'map:read']],
  ['mcp-admin', 'MCP client that can act', ['public', 'players:read', 'bans:read', 'priority:read', 'map:read', 'actions:write']],
]

/** API keys for websites, bots, other servers and MCP clients (MCP). */
export function ApiKeys() {
  const k = useApi<{ keys: ApiKey[]; scopes: ApiScope[]; base_path: string; mcp_path: string; docs: string }>('/api/settings/api-keys')
  const [form, setForm] = useState<{ name: string; scopes: string[]; days: number } | null>(null)
  const [created, setCreated] = useState<{ name: string; token: string; mcp: boolean } | null>(null)
  const [revoke, setRevoke] = useState<ApiKey | null>(null)
  const [showOld, setShowOld] = useState(false)
  const notify = useNotify()
  if (!k.data) return k.error ? <Card title="API keys"><ErrorBox error={k.error} /></Card> : null
  const origin = window.location.origin
  const scopes = k.data.scopes
  const label = (s: string) => scopes.find((x) => x.name === s)?.label ?? s
  const active = k.data.keys.filter((x) => !x.revoked_at)
  const shown = showOld ? k.data.keys : active
  const toggle = (s: string) => form && setForm({ ...form, scopes: form.scopes.includes(s) ? form.scopes.filter((x) => x !== s) : [...form.scopes, s] })
  const create = async () => {
    if (!form) return
    try {
      const r = await api.post<{ key: ApiKey; token: string }>('/api/settings/api-keys', { name: form.name, scopes: form.scopes, expires_days: form.days })
      setCreated({ name: r.key.name, token: r.token, mcp: form.scopes.some((s) => s !== 'public') })
      setForm(null)
      k.reload()
    } catch (e: any) { notify(e.message, 'bad') }
  }
  const doRevoke = async () => {
    if (!revoke) return
    try { await api.del('/api/settings/api-keys/' + revoke.id); notify(`“${revoke.name}” revoked`); k.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setRevoke(null) }
  }
  const mcpConfig = (token: string) => JSON.stringify({ mcpServers: { 'dz-manager': { type: 'http', url: origin + k.data!.mcp_path, headers: { Authorization: 'Bearer ' + token } } } }, null, 2)
  const sensitive = form?.scopes.some((s) => scopes.find((x) => x.name === s)?.sensitive)
  return (
    <Card title={<><Icon name="lock" />API keys</>} actions={<button className="small primary" onClick={() => setForm({ name: '', scopes: ['public'], days: 0 })}><Icon name="plus" />New key</button>}>
      <p className="hint" style={{ marginTop: 0 }}>Each key gets only the scopes you tick. Websites and bots call <code>{origin}{k.data.base_path}</code> with <code>Authorization: Bearer dzm_…</code> (<a href={k.data.docs} target="_blank" rel="noreferrer noopener">OpenAPI description</a>). MCP clients connect to the MCP server at <code>{origin}{k.data.mcp_path}</code> and only see the tools their key allows. Everything a key does in game is in the audit log under its name.</p>
      {!shown.length ? <Empty>No API keys yet.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>Name</th><th>Scopes</th><th>Last used</th><th>Expires</th><th /></tr></thead>
        <tbody>{shown.map((x) => <tr key={x.id} className={x.revoked_at ? 'muted' : ''}>
          <td><b>{x.name}</b><div className="muted small"><code>{x.prefix}…</code> · by {x.created_by}, {new Date(x.created_at).toLocaleDateString()}</div></td>
          <td><div className="row wrap">{x.scopes.map((s) => <Badge key={s} state={scopes.find((y) => y.name === s)?.sensitive ? 'warn' : 'muted'}>{label(s)}</Badge>)}</div></td>
          <td className="small">{x.last_used_at ? new Date(x.last_used_at).toLocaleString() : <span className="muted">never</span>}</td>
          <td className="small">{x.revoked_at ? <Badge state="error">revoked</Badge> : x.expires_at ? new Date(x.expires_at).toLocaleDateString() : <span className="muted">never</span>}</td>
          <td className="num">{!x.revoked_at && <button className="small ghost" onClick={() => setRevoke(x)}>Revoke</button>}</td>
        </tr>)}</tbody>
      </table></div>}
      {k.data.keys.length > active.length && <button className="small ghost" onClick={() => setShowOld(!showOld)}>{showOld ? 'Hide revoked keys' : `Show ${k.data.keys.length - active.length} revoked`}</button>}

      {created && <div className="warnings" style={{ marginTop: 10, display: 'block' }}>
        <p style={{ margin: '0 0 6px' }}><b>Copy the key for “{created.name}” now, it is shown only once:</b></p>
        <span className="id-line"><code>{created.token}</code><CopyButton value={created.token} label="Copy key" /></span>
        {created.mcp && <><p className="small" style={{ margin: '10px 0 4px' }}>To connect an MCP client that supports Streamable HTTP:</p>
          <pre className="code-block">{mcpConfig(created.token)}</pre>
          <CopyButton value={mcpConfig(created.token)} label="Copy MCP config" /></>}
        <div className="row end"><button className="small" onClick={() => setCreated(null)}>Done</button></div>
      </div>}

      {form && <Confirm title="New API key" tone="primary" confirmLabel="Create key" disabled={!form.name.trim() || !form.scopes.length} onClose={() => setForm(null)} onConfirm={create} body={
        <div className="form">
          <label className="field">Name<input autoFocus maxLength={60} value={form.name} placeholder="e.g. Website stats, Discord bot, Integration" onChange={(e) => setForm({ ...form, name: e.target.value })} /></label>
          <label className="field">Start from
            <select value="" onChange={(e) => { const p = PRESETS.find((x) => x[0] === e.target.value); if (p) setForm({ ...form, scopes: p[2], name: form.name || p[1] }) }}>
              <option value="">Choose a preset…</option>
              {PRESETS.map(([id, l]) => <option key={id} value={id}>{l}</option>)}
            </select>
          </label>
          <div className="field">Scopes
            {scopes.map((s) => <label key={s.name} className="check" style={{ alignItems: 'flex-start' }}>
              <input type="checkbox" checked={form.scopes.includes(s.name)} onChange={() => toggle(s.name)} />
              <span><b>{s.label}</b> <code className="small">{s.name}</code><br /><span className="muted small">{s.help}</span></span>
            </label>)}
          </div>
          <div className="field">Expires<Seg label="Expires" value={form.days} onChange={(v) => setForm({ ...form, days: v })} options={EXPIRY} /></div>
          {sensitive && <p className="warn small">This key can see Steam IDs or positions{form.scopes.includes('actions:write') ? ' and change the game' : ''}. Keep it on a server or in your AI client, never in a web page.</p>}
        </div>} />}
      {revoke && <Confirm title={`Revoke “${revoke.name}”?`} confirmLabel="Revoke" onClose={() => setRevoke(null)} onConfirm={doRevoke}
        body={<p>The key stops working immediately. Anything using it needs a new key.</p>} />}
    </Card>
  )
}

/** The server info text the public API serves (rules, FAQ) for bots. */
export function BotInfo() {
  const b = useApi<{ info: string; base_path: string }>('/api/settings/bot')
  const [info, setInfo] = useState<string | null>(null)
  const notify = useNotify()
  if (!b.data) return b.error ? <Card title="Server info"><ErrorBox error={b.error} /></Card> : null
  const text = info ?? b.data.info
  const saveInfo = async () => { try { await api.put('/api/settings/bot/info', { info: text }); setInfo(null); notify('Server info saved'); b.reload() } catch (e: any) { notify(e.message, 'bad') } }
  return (
    <Card title={<><Icon name="chat" />Server info for bots</>}>
      <p className="hint" style={{ marginTop: 0 }}>Served at <code>GET {b.data.base_path}/info</code> to keys with the Public stats scope.</p>
      <textarea rows={6} value={text} maxLength={20000} placeholder={'Rules, wipe schedule, how raiding works, trader locations, Discord links…\nThe bot can quote this when players ask questions.'} onChange={(e) => setInfo(e.target.value)} />
      <div className="row end"><span className="muted small">{text.length}/20000</span><button className="small" disabled={info == null} onClick={saveInfo}>Save info</button></div>
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
