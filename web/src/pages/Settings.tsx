import { useState } from 'react'
import { api, Profile } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, IdValue, PageHead, Pager, Seg, SteamId, Trunc, usePaged } from '../ui'
import { Icon } from '../icons'
import { ago } from '../util'
import { useNotify } from '../App'
import { ApiKeys, AutoMessages, BotInfo, DiscordChannels, PlayerChecks, Retention } from './SettingsExtra'
import { MapTrackingSettings } from './SettingsMap'
import { usePref } from '../prefs'
import { IconName } from '../icons'

const blank = { name: '', provider_kind: 'selfhosted', game_host: '', game_port: 2302, query_host: '', query_port: 27016, rcon_host: '', rcon_port: 2305, rcon_password: '' }
type Edit = typeof blank & { id?: string }
const PROVIDERS: [string, string, string][] = [
  ['selfhosted', 'Self-hosted', 'Your own machine or VM, started by your own scripts or panel.'],
  ['managed', 'Managed host panel', 'No SSH or files: everything goes through RCON and the bridge.'],
  ['generic', 'Other host', 'Any other provider with RCON access.'],
]
const providerName = (k: string) => PROVIDERS.find((p) => p[0] === k)?.[1] || k
const reHost = /^[A-Za-z0-9.-]{1,253}$|^\[?[0-9a-fA-F:]+\]?$/

// The API refuses unknown fields and the Edit form starts from the full profile
// (active, created_at, ...), so send only what profiles.Input accepts.
export function profileInput(e: Edit) {
  return {
    name: e.name.trim(), provider_kind: e.provider_kind,
    game_host: e.game_host.trim(), game_port: e.game_port,
    query_host: e.query_host.trim(), query_port: e.query_port,
    rcon_host: e.rcon_host.trim(), rcon_port: e.rcon_port,
    rcon_password: e.rcon_password,
  }
}

function problems(e: Edit): Partial<Record<keyof Edit, string>> {
  const out: Partial<Record<keyof Edit, string>> = {}
  if (!e.name.trim()) out.name = 'Give the server a name'
  if (!e.game_host.trim()) out.game_host = 'Required'
  else if (!reHost.test(e.game_host.trim())) out.game_host = 'Use an IP address or hostname'
  for (const k of ['query_host', 'rcon_host'] as const) if (e[k].trim() && !reHost.test(e[k].trim())) out[k] = 'Use an IP address or hostname'
  for (const k of ['game_port', 'query_port', 'rcon_port'] as const) if (!Number.isInteger(e[k]) || e[k] < 1 || e[k] > 65535) out[k] = '1–65535'
  return out
}

type SettingsTab = 'server' | 'players' | 'map' | 'messages' | 'integrations' | 'data'
const TABS: [SettingsTab, string, IconName, string][] = [
  ['server', 'Server', 'power', 'Game servers and bridge'],
  ['players', 'Players', 'players', 'Player checks and priority queue'],
  ['map', 'Map & tracking', 'map', 'Tracked events, items and vehicle icons'],
  ['messages', 'Messages', 'megaphone', 'Welcome and rotating messages'],
  ['integrations', 'Integrations', 'send', 'API keys, MCP clients (MCP), Discord'],
  ['data', 'Data', 'audit', 'Retention and archives'],
]

function Tabs({ value, onChange }: { value: SettingsTab; onChange: (t: SettingsTab) => void }) {
  return <div className="tabs full" role="tablist" aria-label="Settings sections">{TABS.map(([k, l, i, d]) => <button key={k} role="tab" aria-selected={value === k} title={d} onClick={() => onChange(k)}><Icon name={i} />{l}</button>)}</div>
}

export default function Settings() {
  const profiles = useApi<Profile[]>('/api/profiles', ['profiles'])
  const bridge = useApi<any>('/api/bridge', ['capabilities'], 10000)
  const [edit, setEdit] = useState<Edit | null>(null)
  const [wizard, setWizard] = useState<any>(null)
  const [confirm, setConfirm] = useState<{ p: Profile; what: 'rotate-ingest' | 'archive' | 'activate' } | null>(null)
  const [saving, setSaving] = useState(false)
  const notify = useNotify()

  const errs = edit ? problems(edit) : {}
  const save = async () => {
    if (!edit || Object.keys(errs).length) return
    setSaving(true)
    try {
      const clean = profileInput(edit)
      if (edit.id) await api.put(`/api/profiles/${edit.id}`, clean)
      else await api.post('/api/profiles', clean)
      notify('Server saved')
      setEdit(null)
      profiles.reload()
    } catch (e: any) { notify(e.message, 'bad') } finally { setSaving(false) }
  }
  const act = async (p: Profile, what: string) => {
    try { await api.post(`/api/profiles/${p.id}/${what}`); notify(what === 'activate' ? `${p.name} is now the active server` : what === 'archive' ? `${p.name} archived` : 'New ingest credentials created'); profiles.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  const showWizard = async (p: Profile) => {
    try { setWizard(await api.get(`/api/profiles/${p.id}/wizard`)) } catch (e: any) { notify(e.message, 'bad') }
  }
  const set = (k: keyof typeof blank, num = false) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setEdit({ ...edit!, [k]: num ? (e.target.value === '' ? 0 : Math.trunc(+e.target.value)) : e.target.value })
  const field = (k: keyof typeof blank, label: string, opts: { num?: boolean; placeholder?: string; type?: string; hint?: string } = {}) => (
    <label className="field">{label}
      <input type={opts.type || (opts.num ? 'number' : 'text')} min={opts.num ? 1 : undefined} max={opts.num ? 65535 : undefined} value={edit![k] as any} placeholder={opts.placeholder}
        autoComplete={opts.type === 'password' ? 'new-password' : 'off'} aria-invalid={!!errs[k]} onChange={set(k, opts.num)} />
      {errs[k] ? <span className="field-error">{errs[k]}</span> : opts.hint ? <span className="hint" style={{ margin: 0 }}>{opts.hint}</span> : null}
    </label>
  )
  const b = bridge.data
  const [tab, setTab] = usePref<SettingsTab>('settings.tab', 'server')

  return (
    <>
      <PageHead title="Settings" />
      <Tabs value={tab} onChange={setTab} />
      {tab === 'server' && <>
      <Card title="Game servers" actions={<button className="small primary" onClick={() => setEdit({ ...blank })}><Icon name="plus" />Add server</button>}>
        <ErrorBox error={profiles.error} />
        {!profiles.data?.length ? <Empty>No servers yet. Add your server to start.</Empty> : (
          <div className="table-scroll"><table>
            <thead><tr><th>Server</th><th>Provider</th><th>Addresses</th><th>RCON</th><th /></tr></thead>
            <tbody>
              {profiles.data.map((p) => (
                <tr key={p.id} className={p.archived ? 'muted' : ''}>
                  <td><b>{p.name}</b> {p.active && <Badge state="ok">active</Badge>} {p.archived && <Badge state="muted">archived</Badge>}<div className="small"><IdValue value={p.id} label="profile ID" short /></div></td>
                  <td>{providerName(p.provider_kind)}</td>
                  <td className="small">
                    <div>Game <code>{p.game_host || '—'}:{p.game_port}</code></div>
                    <div className="muted">Query <code>{(p.query_host || p.game_host) || '—'}:{p.query_port}</code> · RCON <code>{(p.rcon_host || p.game_host) || '—'}:{p.rcon_port}</code></div>
                  </td>
                  <td>{p.has_rcon_password ? <Badge state="ok">password set</Badge> : <Badge state="warn">no password</Badge>}</td>
                  <td>
                    {!p.archived && <div className="btn-group" style={{ justifyContent: 'flex-end' }}>
                      <button className="small" onClick={() => setEdit({ ...blank, ...p, rcon_password: '' })}>Edit</button>
                      <button className="small" onClick={() => showWizard(p)} title="Show the configs to paste on the game server">Connect…</button>
                      {!p.active && <button className="small" onClick={() => setConfirm({ p, what: 'activate' })}>Make active</button>}
                      <button className="small ghost" onClick={() => setConfirm({ p, what: 'rotate-ingest' })} title="Replace the MetricZ and bridge credentials">Rotate keys</button>
                      {!p.active && <button className="small ghost" onClick={() => setConfirm({ p, what: 'archive' })}>Archive</button>}
                    </div>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table></div>
        )}
      </Card>
      <Card title="@DZMBridge" actions={b && (b.connected ? <Badge state="ok">connected</Badge> : <Badge state="warn">not connected</Badge>)}>
        {!b ? <Empty>No active server.</Empty> : b.connected ? (
          <dl>
            <dt>Installed version</dt><dd>{b.info.mod_version || 'unknown'} <span className="muted small">game {b.info.game_version || '?'}</span></dd>
            <dt>Last heartbeat</dt><dd title={b.info.last_seen}>{ago(b.info.last_seen)}</dd>
            <dt>Commands</dt><dd><div className="chips">{(b.info.commands || []).map((c: string) => <span key={c} className="chip" style={{ paddingRight: 10 }}>{c}</span>)}</div></dd>
            <dt>Queue / dropped</dt><dd>{b.info.queue_depth} / {b.info.dropped} {b.info.dropped > 0 && <Badge state="warn">events dropped</Badge>}</dd>
          </dl>
        ) : <p className="muted">Download DZMBridge.zip from the latest GitHub release, install it as a server mod and paste the config from “Connect…”. The game-side PBO is not auto-updated: compare the installed version after each release.</p>}
      </Card>
      </>}
      {tab === 'players' && <><PlayerChecks /><PriorityQueue /></>}
      {tab === 'map' && <MapTrackingSettings bridge={b} />}
      {tab === 'messages' && <AutoMessages />}
      {tab === 'integrations' && <><ApiKeys /><DiscordChannels /><BotInfo /></>}
      {tab === 'data' && <Retention />}
      {edit && (
        <Confirm title={edit.id ? `Edit ${edit.name || 'server'}` : 'Add a server'} tone="primary" confirmLabel={edit.id ? 'Save changes' : 'Add server'} busy={saving} disabled={Object.keys(errs).length > 0} onClose={() => setEdit(null)} onConfirm={save} body={
          <div className="form">
            {field('name', 'Name', { placeholder: 'e.g. My DayZ server' })}
            <div className="field">Provider
              <div className="option-list">{PROVIDERS.map(([k, l, d]) => <label className="option" key={k}><input type="radio" name="prov" checked={edit.provider_kind === k} onChange={() => setEdit({ ...edit, provider_kind: k })} /><div><b>{l}</b><span>{d}</span></div></label>)}</div>
            </div>
            <div className="field-row">{field('game_host', 'Game host', { placeholder: 'IP or hostname' })}{field('game_port', 'Game port', { num: true })}</div>
            <div className="field-row">{field('query_host', 'Query host', { hint: 'Blank = game host' })}{field('query_port', 'Query port', { num: true })}</div>
            <div className="field-row">{field('rcon_host', 'RCON host', { hint: 'Blank = game host' })}{field('rcon_port', 'RCON port', { num: true })}</div>
            {field('rcon_password', edit.id ? 'RCON password (blank keeps the current one)' : 'RCON password', { type: 'password' })}
            <p className="hint">BattlEye RCON is unencrypted UDP: use a long unique password and the provider's IP allowlist where offered. Passwords are stored encrypted and never shown again.</p>
          </div>
        } />
      )}
      {confirm && <Confirm
        title={confirm.what === 'rotate-ingest' ? `Rotate ingest keys for ${confirm.p.name}?` : confirm.what === 'archive' ? `Archive ${confirm.p.name}?` : `Make ${confirm.p.name} the active server?`}
        word={confirm.what === 'rotate-ingest' ? 'ROTATE' : undefined} tone={confirm.what === 'activate' ? 'primary' : 'danger'}
        confirmLabel={confirm.what === 'rotate-ingest' ? 'Rotate keys' : confirm.what === 'archive' ? 'Archive' : 'Make active'}
        body={<p>{confirm.what === 'rotate-ingest' ? 'The current MetricZ and bridge credentials stop working immediately. You must paste the new configs on the game server.' : confirm.what === 'archive' ? 'The server disappears from the lists. Its history is kept.' : 'Every page, the map and all actions switch to this server.'}</p>}
        onClose={() => setConfirm(null)} onConfirm={() => { act(confirm.p, confirm.what); setConfirm(null) }} />}
      {wizard && (
        <div className="modal-bg" onClick={() => setWizard(null)}>
          <div className="modal wide" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
            <h3>Connect the game server</h3>
            <ol>{wizard.steps.map((s: string) => <li key={s}>{s}</li>)}</ol>
            <Snippet title="Launch parameter" text={wizard.servermod_line} />
            <Snippet title={`${wizard.metricz_config_path} (merge)`} text={JSON.stringify(wizard.metricz_config, null, 2)} />
            <Snippet title={wizard.bridge_config_path} text={JSON.stringify(wizard.bridge_config, null, 2)} />
            <p className="hint warn">These contain secrets. Viewing them was audited.</p>
            <div className="row end"><button onClick={() => setWizard(null)}>Close</button></div>
          </div>
        </div>
      )}
    </>
  )
}

function Snippet({ title, text }: { title: string; text: string }) {
  return <><h4 className="row">{title}<CopyButton value={text} label="Copy" /></h4><pre>{text}</pre></>
}

const PRIO_DAYS: [number, string][] = [[0, 'Never expires'], [1, '1 day'], [7, '7 days'], [30, '30 days'], [90, '90 days'], [365, '1 year']]
const reSteam = /^7656119\d{10}$/

function PriorityQueue() {
  const q = useApi<{ entries: { steam64: string; name: string; reason: string; expires_at?: string; expired: boolean; applied: boolean; added_by: string }[]; note: string }>('/api/priority')
  const [f, setF] = useState({ steam64: '', reason: '', days: 30 })
  const [remove, setRemove] = useState<string | null>(null)
  const notify = useNotify()
  const pg = usePaged(q.data?.entries, 10)
  const call = async (fn: () => Promise<unknown>, ok: string) => {
    try { await fn(); notify(ok); q.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  const sid = f.steam64.trim()
  const dup = !!q.data?.entries.some((e) => e.steam64 === sid && !e.expired)
  return (
    <Card title="Priority queue" actions={<a className="button small" href="/api/priority/export" target="_blank" rel="noreferrer">Export Steam64 list</a>}>
      <p className="hint" style={{ marginTop: 0 }}>{q.data?.note}</p>
      {!q.data?.entries.length ? <Empty>No priority players.</Empty> : (<>
        <div className="table-scroll"><table>
          <thead><tr><th>Player</th><th>Steam64</th><th>Reason</th><th>Expires</th><th title="Tick once you added them in the provider panel">In provider</th><th /></tr></thead>
          <tbody>
            {pg.rows.map((e) => (
              <tr key={e.steam64} className={e.expired ? 'muted' : ''}>
                <td>{e.name ? <a href={'#/players/' + e.steam64}>{e.name}</a> : <span className="muted">unknown</span>}</td>
                <td><SteamId id={e.steam64} short /></td>
                <td><Trunc text={e.reason} max={200} /></td>
                <td>{e.expires_at ? (e.expired ? <Badge state="muted">expired</Badge> : new Date(e.expires_at).toLocaleDateString()) : 'Never'}</td>
                <td><input type="checkbox" checked={e.applied} aria-label="Applied in provider" onChange={(ev) => call(() => api.post(`/api/priority/${e.steam64}/applied`, { applied: ev.target.checked }), 'Updated')} /></td>
                <td className="num"><button className="small ghost" onClick={() => setRemove(e.steam64)}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table></div>
        <Pager {...pg} noun="players" />
      </>)}
      <h4>Add a player</h4>
      <div className="field-row">
        <label className="field">Steam64<input placeholder="7656119…" inputMode="numeric" value={f.steam64} aria-invalid={sid !== '' && (!reSteam.test(sid) || dup)} onChange={(e) => setF({ ...f, steam64: e.target.value.replace(/\D/g, '').slice(0, 17) })} />
          {sid !== '' && !reSteam.test(sid) && <span className="field-error">A Steam64 is 17 digits starting with 7656119</span>}
          {dup && <span className="field-error">Already in the queue</span>}</label>
        <label className="field">Reason<input placeholder="e.g. Supporter, event winner" value={f.reason} maxLength={100} onChange={(e) => setF({ ...f, reason: e.target.value })} /></label>
      </div>
      <div className="row wrap" style={{ marginTop: 10 }}>
        <Seg label="Duration" value={f.days} onChange={(v) => setF({ ...f, days: v })} options={PRIO_DAYS} />
        <button className="primary small" style={{ marginLeft: 'auto' }} disabled={!reSteam.test(sid) || dup || !f.reason.trim()} onClick={() => call(() => api.post('/api/priority', { steam64: sid, reason: f.reason.trim(), days: f.days }), 'Added. Remember to add them in the provider panel too.').then(() => setF({ steam64: '', reason: '', days: 30 }))}><Icon name="plus" />Add</button>
      </div>
      {remove && <Confirm title="Remove from priority queue?" confirmLabel="Remove" body={<p>Remove <code>{remove}</code>. Remove them in the provider panel too.</p>} onClose={() => setRemove(null)} onConfirm={() => { call(() => api.post(`/api/priority/${remove}/remove`), 'Removed (remove it in the provider too)'); setRemove(null) }} />}
    </Card>
  )
}
