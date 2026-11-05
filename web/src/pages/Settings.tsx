import { useEffect, useMemo, useState } from 'react'
import { api, Profile } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, CopyButton, Empty, ErrorBox, IdValue, PageHead, Pager, Seg, SteamId, Trunc, usePaged } from '../ui'
import { Icon } from '../icons'
import { ago } from '../util'
import { useNotify } from '../App'
import { AutoMessages, BotApi, DiscordChannels, HostControl, PlayerChecks, Retention, VehicleIcons } from './SettingsExtra'
import { usePref } from '../prefs'
import { IconName } from '../icons'

const blank = { name: '', provider_kind: 'selfhosted_architect', game_host: '', game_port: 2302, query_host: '', query_port: 27016, rcon_host: '', rcon_port: 2305, rcon_password: '' }
type Edit = typeof blank & { id?: string }
const PROVIDERS: [string, string, string][] = [
  ['selfhosted_architect', 'Self-hosted (CFTools Architect)', 'Your own machine or VM managed by Architect.'],
  ['vykix', 'VYKIX managed panel', 'No SSH or files: everything goes through RCON and the bridge.'],
  ['generic', 'Other host', 'Any other provider with RCON access.'],
]
const providerName = (k: string) => PROVIDERS.find((p) => p[0] === k)?.[1] || k
const reHost = /^[A-Za-z0-9.-]{1,253}$|^\[?[0-9a-fA-F:]+\]?$/

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
  ['server', 'Server', 'power', 'Game servers, bridge, emergency stop'],
  ['players', 'Players', 'players', 'Player checks and priority queue'],
  ['map', 'Map & tracking', 'map', 'Tracked events, items and vehicle icons'],
  ['messages', 'Messages', 'megaphone', 'Welcome and rotating messages'],
  ['integrations', 'Integrations', 'send', 'Discord channels and bot API'],
  ['data', 'Data', 'audit', 'Retention and archives'],
]

function Tabs({ value, onChange }: { value: SettingsTab; onChange: (t: SettingsTab) => void }) {
  return <div className="tabs full" role="tablist" aria-label="Settings sections">{TABS.map(([k, l, i, d]) => <button key={k} role="tab" aria-selected={value === k} title={d} onClick={() => onChange(k)}><Icon name={i} />{l}</button>)}</div>
}

export default function Settings() {
  const profiles = useApi<Profile[]>('/api/profiles', ['profiles'])
  const bridge = useApi<any>('/api/bridge', ['capabilities'])
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
      const { id, ...body } = edit
      const clean = { ...body, name: body.name.trim(), game_host: body.game_host.trim(), query_host: body.query_host.trim(), rcon_host: body.rcon_host.trim() }
      if (id) await api.put(`/api/profiles/${id}`, clean)
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
      <HostControl />
      </>}
      {tab === 'players' && <><PlayerChecks /><PriorityQueue /></>}
      {tab === 'map' && <><Tracking invalid={b?.info?.tracking_invalid || []} /><VehicleIcons /></>}
      {tab === 'messages' && <AutoMessages />}
      {tab === 'integrations' && <><DiscordChannels /><BotApi /></>}
      {tab === 'data' && <Retention />}
      {edit && (
        <Confirm title={edit.id ? `Edit ${edit.name || 'server'}` : 'Add a server'} tone="primary" confirmLabel={edit.id ? 'Save changes' : 'Add server'} busy={saving} disabled={Object.keys(errs).length > 0} onClose={() => setEdit(null)} onConfirm={save} body={
          <div className="form">
            {field('name', 'Name', { placeholder: 'e.g. EndPoint test' })}
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

const CATEGORIES: [string, string][] = [['heli_crash', 'Helicrash'], ['plane_crash', 'Aircraft crash'], ['airdrop', 'Airdrop'], ['loot_box', 'Loot box'], ['military_convoy', 'Convoy'], ['police', 'Police'], ['train', 'Train'], ['base_flag', 'Base flag'], ['custom', 'Other event']]
const reClass = /^[A-Za-z][A-Za-z0-9_]{1,63}$/

function Tracking({ invalid }: { invalid: string[] }) {
  const t = useApi<{ event_classes: Record<string, string>; rare_items: string[] }>('/api/settings/tracking')
  const [rows, setRows] = useState<[string, string][] | null>(null)
  const [items, setItems] = useState<string[] | null>(null)
  const [filter, setFilter] = useState('')
  const [nc, setNc] = useState({ cls: '', cat: 'heli_crash' })
  const [ni, setNi] = useState('')
  const [saving, setSaving] = useState(false)
  const notify = useNotify()
  useEffect(() => {
    if (t.data && rows == null) { setRows(Object.entries(t.data.event_classes).sort((a, b) => a[0].localeCompare(b[0]))); setItems([...t.data.rare_items]) }
  }, [t.data])
  const bad = useMemo(() => new Set(invalid), [invalid.join()])
  const shown = (rows ?? []).filter(([c, cat]) => !filter || (c + ' ' + cat).toLowerCase().includes(filter.toLowerCase()))
  const pg = usePaged(shown, 15)
  if (!t.data || !rows || !items) return t.error ? <Card title="Event & item tracking"><ErrorBox error={t.error} /></Card> : null
  const origRows = Object.entries(t.data.event_classes).sort((a, b) => a[0].localeCompare(b[0]))
  const dirty = JSON.stringify(origRows) !== JSON.stringify([...rows].sort((a, b) => a[0].localeCompare(b[0]))) || JSON.stringify(t.data.rare_items) !== JSON.stringify(items)
  const clsTaken = rows.some(([c]) => c.toLowerCase() === nc.cls.trim().toLowerCase())
  const itemTaken = items.some((c) => c.toLowerCase() === ni.trim().toLowerCase())
  const save = async () => {
    setSaving(true)
    try {
      await api.put('/api/settings/tracking', { event_classes: Object.fromEntries(rows), rare_items: items })
      notify('Tracking saved. The bridge applies it on its next poll and rescans (at most once a minute).')
      setRows(null); setItems(null); t.reload()
    } catch (e: any) { notify(e.message, 'bad') } finally { setSaving(false) }
  }
  return (
    <Card title="Event & item tracking" actions={<>{dirty && <span className="muted small">Unsaved changes</span>}
      {dirty && <button className="small ghost" onClick={() => { setRows(null); setItems(null); t.reload() }}>Discard</button>}
      <button className="small primary" disabled={!dirty || saving} onClick={save}>{saving ? 'Saving…' : 'Save & rescan'}</button></>}>
      {invalid.length > 0 && <div className="warnings">The game server doesn't know {invalid.length === 1 ? 'this class' : 'these classes'}: <b>{invalid.join(', ')}</b>. Check the spelling or remove {invalid.length === 1 ? 'it' : 'them'}.</div>}
      <div className="grid two" style={{ marginBottom: 0 }}>
        <div>
          <h4>World events ({rows.length})</h4>
          <div className="row" style={{ marginBottom: 8 }}>
            <input type="search" placeholder="Filter classes" value={filter} onChange={(e) => { setFilter(e.target.value); pg.setPage(1) }} />
          </div>
          {!shown.length ? <Empty>{filter ? 'No classes match.' : 'No event classes yet.'}</Empty> : (
            <table>
              <thead><tr><th>Classname</th><th>Shown as</th><th /></tr></thead>
              <tbody>{pg.rows.map(([c, cat]) => (
                <tr key={c}>
                  <td><Trunc text={c} max={220} className={bad.has(c) ? 'warn' : ''} /> {bad.has(c) && <Badge state="warn">unknown</Badge>}</td>
                  <td><select value={cat} onChange={(e) => setRows(rows.map((r) => r[0] === c ? [c, e.target.value] : r))} aria-label={`Category for ${c}`}>{CATEGORIES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></td>
                  <td className="num"><button className="icon ghost" title={`Remove ${c}`} onClick={() => setRows(rows.filter((r) => r[0] !== c))}><Icon name="close" /></button></td>
                </tr>
              ))}</tbody>
            </table>
          )}
          <Pager {...pg} noun="classes" />
          <div className="row" style={{ marginTop: 10 }}>
            <input placeholder="New classname, e.g. Wreck_UH1Y" value={nc.cls} aria-invalid={nc.cls !== '' && (!reClass.test(nc.cls.trim()) || clsTaken)} onChange={(e) => setNc({ ...nc, cls: e.target.value })} style={{ flex: 1, minWidth: 0 }} />
            <select value={nc.cat} onChange={(e) => setNc({ ...nc, cat: e.target.value })} style={{ width: 140 }} aria-label="Category">{CATEGORIES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select>
            <button className="small" disabled={!reClass.test(nc.cls.trim()) || clsTaken || rows.length >= 200} onClick={() => { setRows([...rows, [nc.cls.trim(), nc.cat]]); setNc({ ...nc, cls: '' }) }}><Icon name="plus" />Add</button>
          </div>
          {clsTaken && <span className="field-error">Already in the list.</span>}
          <p className="hint">Parents work too: a base class (e.g. a crash base) covers every class that inherits from it.</p>
        </div>
        <div>
          <h4>Tracked items on the ground ({items.length})</h4>
          {!items.length ? <Empty>No items tracked.</Empty> : <div className="chips">{items.map((c) => <span className="chip" key={c} title={bad.has(c) ? 'Unknown on the game server' : c}><span className={bad.has(c) ? 'warn' : ''}>{c}</span><button title={`Stop tracking ${c}`} aria-label={`Remove ${c}`} onClick={() => setItems(items.filter((x) => x !== c))}><Icon name="close" /></button></span>)}</div>}
          <div className="row" style={{ marginTop: 10 }}>
            <input placeholder="Item classname, e.g. PunchedCard" value={ni} aria-invalid={ni !== '' && (!reClass.test(ni.trim()) || itemTaken)} onChange={(e) => setNi(e.target.value)} style={{ flex: 1, minWidth: 0 }} />
            <button className="small" disabled={!reClass.test(ni.trim()) || itemTaken || items.length >= 200} onClick={() => { setItems([...items, ni.trim()]); setNi('') }}><Icon name="plus" />Add</button>
          </div>
          <p className="hint">Only items lying on the ground are shown (never inside inventories or containers). They move when dropped or picked up. No restart is needed.</p>
        </div>
      </div>
    </Card>
  )
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
