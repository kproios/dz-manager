import { useState } from 'react'
import { api, Profile } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox } from '../ui'
import { ago } from '../util'
import { useNotify } from '../App'

const blank = { name: '', provider_kind: 'selfhosted_architect', game_host: '', game_port: 2302, query_host: '', query_port: 27016, rcon_host: '', rcon_port: 2305, rcon_password: '' }

export default function Settings() {
  const profiles = useApi<Profile[]>('/api/profiles', ['profiles'])
  const bridge = useApi<any>('/api/bridge', ['capabilities'])
  const [edit, setEdit] = useState<(typeof blank & { id?: string }) | null>(null)
  const [wizard, setWizard] = useState<any>(null)
  const [rotate, setRotate] = useState<Profile | null>(null)
  const notify = useNotify()

  const save = async () => {
    if (!edit) return
    try {
      const { id, ...body } = edit
      if (id) await api.put(`/api/profiles/${id}`, body)
      else await api.post('/api/profiles', body)
      notify('Saved')
      setEdit(null)
      profiles.reload()
    } catch (e: any) { notify(e.message, 'bad') }
  }
  const act = async (p: Profile, what: string) => {
    try { await api.post(`/api/profiles/${p.id}/${what}`); notify('Done'); profiles.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  const showWizard = async (p: Profile) => {
    try { setWizard(await api.get(`/api/profiles/${p.id}/wizard`)) } catch (e: any) { notify(e.message, 'bad') }
  }
  const set = (k: keyof typeof blank, num = false) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setEdit({ ...edit!, [k]: num ? parseInt(e.target.value, 10) || 0 : e.target.value })

  return (
    <>
      <h1>Settings</h1>
      <Card title="Game servers" actions={<button onClick={() => setEdit({ ...blank })}>Add server</button>}>
        <ErrorBox error={profiles.error} />
        {!profiles.data?.length ? <Empty>No servers yet. Add TestServer to start.</Empty> : (
          <table>
            <thead><tr><th>Name</th><th>Provider</th><th>Endpoints</th><th>RCON password</th><th /></tr></thead>
            <tbody>
              {profiles.data.map((p) => (
                <tr key={p.id} className={p.archived ? 'muted' : ''}>
                  <td>{p.name} {p.active && <Badge state="ok">active</Badge>} {p.archived && <Badge state="muted">archived</Badge>}<div className="small muted"><code>{p.id}</code></div></td>
                  <td>{p.provider_kind}</td>
                  <td className="small">game {p.game_host || '—'}:{p.game_port} · query {(p.query_host || p.game_host) || '—'}:{p.query_port} · rcon {(p.rcon_host || p.game_host) || '—'}:{p.rcon_port}</td>
                  <td>{p.has_rcon_password ? 'set' : <Badge state="warn">missing</Badge>}</td>
                  <td className="right">
                    {!p.archived && <>
                      <button className="small" onClick={() => setEdit({ ...blank, ...p, rcon_password: '' })}>Edit</button>{' '}
                      <button className="small" onClick={() => showWizard(p)}>Connect…</button>{' '}
                      {!p.active && <button className="small" onClick={() => act(p, 'activate')}>Activate</button>}{' '}
                      <button className="small" onClick={() => setRotate(p)}>Rotate ingest keys</button>{' '}
                      <button className="small" onClick={() => act(p, 'archive')}>Archive</button>
                    </>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card title="@DZMBridge">
        {!bridge.data ? <Empty>No active server.</Empty> : bridge.data.connected ? (
          <dl>
            <dt>Installed version</dt><dd>{bridge.data.info.mod_version || 'unknown'} (game {bridge.data.info.game_version || '?'})</dd>
            <dt>Last heartbeat</dt><dd>{ago(bridge.data.info.last_seen)}</dd>
            <dt>Handlers</dt><dd>{(bridge.data.info.commands || []).join(', ') || 'none'}</dd>
            <dt>Queue / dropped</dt><dd>{bridge.data.info.queue_depth} / {bridge.data.info.dropped}</dd>
          </dl>
        ) : <p className="muted">Not connected. Download DZMBridge.zip from the latest GitHub release, install it as a server mod, and paste the config from "Connect…". The game-side PBO is not auto-updated: compare the installed version with the latest release after each update.</p>}
      </Card>
      <Tracking />
      <PriorityQueue />
      {edit && (
        <Confirm title={edit.id ? 'Edit server' : 'Add server'} onClose={() => setEdit(null)} onConfirm={save} body={
          <div className="form">
            <label>Name <input value={edit.name} onChange={set('name')} /></label>
            <label>Provider
              <select value={edit.provider_kind} onChange={set('provider_kind')}>
                <option value="selfhosted_architect">Self-hosted (CFTools Architect)</option>
                <option value="vykix">VYKIX managed panel</option>
                <option value="generic">Generic</option>
              </select>
            </label>
            <label>Game host <input value={edit.game_host} onChange={set('game_host')} placeholder="IP or hostname" /></label>
            <label>Game port <input type="number" value={edit.game_port} onChange={set('game_port', true)} /></label>
            <label>Query host (blank = game host) <input value={edit.query_host} onChange={set('query_host')} /></label>
            <label>Query port <input type="number" value={edit.query_port} onChange={set('query_port', true)} /></label>
            <label>RCON host (blank = game host) <input value={edit.rcon_host} onChange={set('rcon_host')} /></label>
            <label>RCON port <input type="number" value={edit.rcon_port} onChange={set('rcon_port', true)} /></label>
            <label>RCON password {edit.id && '(leave blank to keep)'} <input type="password" autoComplete="new-password" value={edit.rcon_password} onChange={set('rcon_password')} /></label>
            <p className="small muted">BattlEye RCON is unencrypted UDP: use a long unique password and a provider IP allowlist where offered.</p>
          </div>
        } />
      )}
      {rotate && <Confirm title={`Rotate ingest credentials for ${rotate.name}?`} body={<p>The current MetricZ and bridge credentials stop working immediately. You must paste the new configs on the game server.</p>} onClose={() => setRotate(null)} onConfirm={() => { act(rotate, 'rotate-ingest'); setRotate(null) }} />}
      {wizard && (
        <div className="modal-bg" onClick={() => setWizard(null)}>
          <div className="modal wide" onClick={(e) => e.stopPropagation()}>
            <h3>Connect the game server</h3>
            <ol>{wizard.steps.map((s: string) => <li key={s}>{s}</li>)}</ol>
            <h4>Launch parameter</h4><pre>{wizard.servermod_line}</pre>
            <h4>{wizard.metricz_config_path} (merge)</h4><pre>{JSON.stringify(wizard.metricz_config, null, 2)}</pre>
            <h4>{wizard.bridge_config_path}</h4><pre>{JSON.stringify(wizard.bridge_config, null, 2)}</pre>
            <p className="small warn">These contain secrets. Viewing them was audited.</p>
            <div className="row end"><button onClick={() => setWizard(null)}>Close</button></div>
          </div>
        </div>
      )}
    </>
  )
}

function Tracking() {
  const t = useApi<{ event_classes: Record<string, string>; rare_items: string[] }>('/api/settings/tracking')
  const [classes, setClasses] = useState<string | null>(null)
  const [items, setItems] = useState<string | null>(null)
  const notify = useNotify()
  if (!t.data) return null
  const cls = classes ?? Object.entries(t.data.event_classes).map(([k, v]) => `${k}=${v}`).join('\n')
  const its = items ?? t.data.rare_items.join('\n')
  const save = async () => {
    const event_classes: Record<string, string> = {}
    for (const line of cls.split('\n').map((l) => l.trim()).filter(Boolean)) {
      const [k, v] = line.split('=').map((s) => s.trim())
      event_classes[k] = v
    }
    try {
      await api.put('/api/settings/tracking', { event_classes, rare_items: its.split(/\s+/).filter(Boolean) })
      notify('Saved. Re-open "Connect…" and update the bridge config on the server.')
      t.reload()
    } catch (e: any) { notify(e.message, 'bad') }
  }
  return (
    <Card title="Event & rare-item tracking">
      <div className="grid">
        <label>Event entity classes (<code>Class=category</code>; categories: heli_crash, airdrop, loot_box, custom)
          <textarea rows={6} value={cls} onChange={(e) => setClasses(e.target.value)} />
        </label>
        <label>Rare item classnames (one per line; only Central Economy spawns of these are recorded)
          <textarea rows={6} value={its} onChange={(e) => setItems(e.target.value)} />
        </label>
      </div>
      <div className="row end"><button onClick={save}>Save</button></div>
    </Card>
  )
}

function PriorityQueue() {
  const q = useApi<{ entries: { steam64: string; name: string; reason: string; expires_at?: string; expired: boolean; applied: boolean; added_by: string }[]; note: string }>('/api/priority')
  const [f, setF] = useState({ steam64: '', reason: '', days: '0' })
  const notify = useNotify()
  const call = async (fn: () => Promise<unknown>, ok: string) => {
    try { await fn(); notify(ok); q.reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  return (
    <Card title="Priority queue (list only)" actions={<a className="button" href="/api/priority/export" target="_blank" rel="noreferrer">Export Steam64 list</a>}>
      <p className="small muted">{q.data?.note}</p>
      {!q.data?.entries.length ? <Empty>No priority players.</Empty> : (
        <table>
          <thead><tr><th>Player</th><th>Reason</th><th>Expires</th><th>Applied in provider?</th><th /></tr></thead>
          <tbody>
            {q.data.entries.map((e) => (
              <tr key={e.steam64} className={e.expired ? 'muted' : ''}>
                <td>{e.name || <code className="small">{e.steam64}</code>}</td>
                <td>{e.reason}</td>
                <td>{e.expires_at ? (e.expired ? 'expired' : new Date(e.expires_at).toLocaleDateString()) : 'never'}</td>
                <td><input type="checkbox" checked={e.applied} onChange={(ev) => call(() => api.post(`/api/priority/${e.steam64}/applied`, { applied: ev.target.checked }), 'Updated')} /></td>
                <td><button className="small" onClick={() => call(() => api.post(`/api/priority/${e.steam64}/remove`), 'Removed (remove it in the provider too)')}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="row wrap">
        <input placeholder="Steam64" value={f.steam64} onChange={(e) => setF({ ...f, steam64: e.target.value })} />
        <input placeholder="Reason" value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} />
        <input type="number" min={0} title="days (0 = never)" value={f.days} onChange={(e) => setF({ ...f, days: e.target.value })} />
        <button onClick={() => call(() => api.post('/api/priority', { steam64: f.steam64.trim(), reason: f.reason, days: parseInt(f.days, 10) || 0 }), 'Added')}>Add</button>
      </div>
    </Card>
  )
}
