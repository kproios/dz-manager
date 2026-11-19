import { useEffect, useMemo, useRef, useState } from 'react'
import { api, MapJob, MapSources, Marker, WorldInfo, WorldResp } from '../api'
import { useApi, useNow } from '../hooks'
import { Badge, Card, Empty, ErrorBox, Meter, Trunc } from '../ui'
import { ago } from '../util'
import { Icon } from '../icons'
import { useNotify } from '../App'
import { usePref } from '../prefs'
import { closestVehicle, iconLabels, iconSvg, MapIcon, ruleMatches, vehicleIcon, vehicleKinds, VehicleRule } from '../mapIcons'
import { isFresh, useLiveMap } from '../liveMap'

const CATEGORIES: [string, string][] = [['heli_crash', 'Helicrash'], ['plane_crash', 'Aircraft crash'], ['airdrop', 'Airdrop'], ['loot_box', 'Loot box'], ['military_convoy', 'Convoy'], ['police', 'Police'], ['train', 'Train'], ['base_flag', 'Base flag'], ['custom', 'Other event']]
const reClass = /^[A-Za-z][A-Za-z0-9_]{1,63}$/
const MIN_BRIDGE_LIVE = '0.1.11'

const newer = (a: string, b: string) => {
  const x = a.split('.').map(Number), y = b.split('.').map(Number)
  for (let i = 0; i < 3; i++) if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) > (y[i] || 0)
  return true
}
const ico = (k: MapIcon) => <span className={`entity-pin icon-${k} find-ico`} dangerouslySetInnerHTML={{ __html: iconSvg(k) }} />

interface Tracking { revision: number; event_classes: Record<string, string>; rare_items: string[] }

/** Settings → Map & tracking: what the map is doing now, and everything that changes it. */
export function MapTrackingSettings({ bridge }: { bridge: any }) {
  const live = useLiveMap()
  const tracking = useApi<Tracking>('/api/settings/tracking')
  const events = useApi<Marker[]>('/api/map/events', ['map_events'], 30000)
  const active = useMemo(() => (events.data ?? []).filter((m) => m.status === 'active'), [events.data])
  return <>
    <MapImage />
    <MapStatus live={live} bridge={bridge} tracking={tracking.data} active={active} />
    <VehicleIcons vehicles={live.data?.vehicles ?? []} />
    <TrackedThings tracking={tracking} active={active} invalid={bridge?.info?.tracking_invalid || []} />
  </>
}

const BUSY = ['downloading', 'decoding', 'tiling']
const KNOWN_WORLDS: [string, string, number][] = [['chernarusplus', 'Chernarus', 15360], ['enoch', 'Livonia', 12800], ['namalsk', 'Namalsk', 12800]]

/** Which map this server runs, and the terrain image drawn under the markers. */
function MapImage() {
  const w = useApi<WorldResp>('/api/map/world', ['world', 'map_install'])
  const [running, setRunning] = useState(false)
  const job = useApi<MapJob>('/api/map/install', ['map_install'], running ? 2000 : 0)
  const [sources, setSources] = useState<MapSources | null>(null)
  const [srcError, setSrcError] = useState('')
  const [slug, setSlug] = useState('')
  const [url, setUrl] = useState('')
  const [starting, setStarting] = useState(false)
  const notify = useNotify()
  const info = w.data?.world
  const tiles = w.data?.tiles
  const busy = !!job.data && BUSY.includes(job.data.state)
  useEffect(() => { setRunning(busy) }, [busy])
  const prevState = useRef<string | undefined>(undefined)
  useEffect(() => {
    const s = job.data?.state
    if (prevState.current && BUSY.includes(prevState.current) && s === 'done') { notify('Map image installed'); w.reload() }
    if (prevState.current && BUSY.includes(prevState.current) && s === 'error') notify('Map install failed: ' + job.data?.error, 'bad')
    prevState.current = s
  }, [job.data?.state])

  const loadSources = async (refresh = false) => {
    setSrcError('')
    try {
      const s = await api.get<MapSources>('/api/map/sources' + (refresh ? '?refresh=1' : ''))
      setSources(s)
      const pick = s.sources.find((x) => x.slug === (slug || s.suggested))
      if (pick) {
        setSlug(pick.slug)
        const best = [...pick.files].reverse().find((f) => f.fits)
        if (best && !url) setUrl(best.url)
      }
    } catch (e: any) { setSrcError(e.message) }
  }
  useEffect(() => { loadSources() }, [])
  const chosen = sources?.sources.find((x) => x.slug === slug)
  const pickSlug = (s: string) => {
    setSlug(s)
    const src = sources?.sources.find((x) => x.slug === s)
    const best = src ? [...src.files].reverse().find((f) => f.fits) : undefined
    setUrl(best?.url ?? '')
  }
  const install = async () => {
    setStarting(true)
    try { await api.post('/api/map/install', { url }); setRunning(true); job.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setStarting(false) }
  }
  const mismatch = !!sources?.suggested && !!slug && slug !== sources.suggested
  const j = job.data
  return (
    <Card title={<><Icon name="map" />Map image</>}>
      <dl className="status-list">
        <dt>Map</dt>
        <dd>{!info ? '…' : <>
          <b>{info.label}</b> <span className="muted small">{info.world} · {info.size.toLocaleString()} m · {info.places.length} place names</span>{' '}
          {info.source === 'bridge' ? <Badge state="ok">reported by the game server</Badge>
            : info.source === 'manual' ? <Badge state="warn">set by hand</Badge>
              : <Badge state="unconfigured">default</Badge>}
        </>}</dd>
        <dt>Installed image</dt>
        <dd>{!w.data ? '…' : tiles
          ? <span>{tiles.source.replace(/^dzhosts:/, 'dzhosts.com ').replace(/^file:/, 'file ')} · {tiles.source_px.toLocaleString()} px · zoom 0–{tiles.max_zoom} · <span className="muted">{ago(tiles.installed_at)}</span></span>
          : <Badge state="warn">none for this map</Badge>}</dd>
      </dl>
      {info && info.source !== 'bridge' && <WorldByHand info={info} onSaved={w.reload} />}
      <h4>Download a map image</h4>
      <p className="hint">Images of the terrain rendered from DayZ's own textures by <a href="https://github.com/jmhobbs/dayz-terrain-map" target="_blank" rel="noreferrer noopener">dayz-terrain-map</a> and published at <a href={sources?.list_url || 'https://tools.dzhosts.com/maps/'} target="_blank" rel="noreferrer noopener">tools.dzhosts.com/maps</a>. DZ Manager downloads the image, cuts it into map tiles on this server and deletes the download.</p>
      <ErrorBox error={srcError || null} />
      <div className="row wrap">
        <label className="field">Map
          <select value={slug} onChange={(e) => pickSlug(e.target.value)} disabled={!sources || busy}>
            <option value="">{sources ? 'Choose…' : 'Loading the list…'}</option>
            {['Official', 'Workshop'].map((g) => <optgroup key={g} label={g}>{sources?.sources.filter((s) => s.group === g).map((s) => <option key={s.slug} value={s.slug}>{s.label}{s.slug === sources.suggested ? ' (this server)' : ''}</option>)}</optgroup>)}
          </select>
        </label>
        <label className="field">Size
          <select value={url} onChange={(e) => setUrl(e.target.value)} disabled={!chosen || busy}>
            {chosen?.files.map((f) => <option key={f.url} value={f.url} disabled={!f.fits}>{f.px.toLocaleString()} px{f.size ? ` · ${f.size}B` : ''}{f.fits ? '' : ' · too large for this server'}</option>)}
          </select>
        </label>
        <button className="primary" style={{ alignSelf: 'end' }} disabled={!url || busy || starting} onClick={install}><Icon name="refresh" />{tiles ? 'Download and replace' : 'Download and install'}</button>
        <button className="ghost small" style={{ alignSelf: 'end' }} disabled={busy} onClick={() => loadSources(true)} title="Fetch the list again">Refresh list</button>
      </div>
      {mismatch && <p className="warn small">This server runs {info?.label}; the image you picked is for another map. It would be drawn under {info?.label}'s markers.</p>}
      <p className="hint">Bigger images show more detail when zoomed in but take longer. This server tiles images up to {w.data?.max_px.toLocaleString() ?? '…'} px (DZM_MAP_MAX_PX); 7,000–8,000 px is plenty.</p>
      {j && j.state !== 'idle' && <div className="map-job" role="status">
        {j.state === 'downloading' && <><span>Downloading {j.source?.replace(/^dzhosts:/, '')}…</span><Meter value={j.total ? (j.bytes ?? 0) / j.total : null} color="#3987e5" /><span className="muted small">{((j.bytes ?? 0) / 1048576).toFixed(0)}{j.total ? ` / ${(j.total / 1048576).toFixed(0)}` : ''} MB</span></>}
        {j.state === 'decoding' && <span>Reading the image…</span>}
        {j.state === 'tiling' && <><span>Cutting tiles…</span><Meter value={j.tiles_total ? (j.tiles ?? 0) / j.tiles_total : null} color="#3ccf7e" /><span className="muted small">{j.tiles ?? 0} / {j.tiles_total ?? '…'}</span></>}
        {j.state === 'done' && <span><Badge state="ok">installed</Badge> {j.source?.replace(/^dzhosts:/, '')} for {j.world}, {ago(j.finished_at)}</span>}
        {j.state === 'error' && <span><Badge state="error">failed</Badge> {j.error}</span>}
      </div>}
      <p className="hint">Or install from the command line, also with your own PNG made with dayz-terrain-map for a map that is not listed: <code>dzm map install --world namalsk --file namalsk.png</code></p>
    </Card>
  )
}

function WorldByHand({ info, onSaved }: { info: WorldInfo; onSaved: () => void }) {
  const [world, setWorld] = useState(info.source === 'manual' ? info.world : '')
  const [size, setSize] = useState(info.source === 'manual' ? String(info.size) : '')
  const [saving, setSaving] = useState(false)
  const notify = useNotify()
  const known = KNOWN_WORLDS.find((k) => k[0] === world.trim().toLowerCase())
  const n = +size || known?.[2] || 0
  const ok = /^[a-z0-9_]{1,40}$/i.test(world.trim()) && n >= 1000 && n <= 100000
  const save = async (clear = false) => {
    setSaving(true)
    try { await api.put('/api/map/world', clear ? { world: '' } : { world: world.trim(), size: n }); notify(clear ? 'Back to the default map' : 'Map saved'); onSaved() } catch (e: any) { notify(e.message, 'bad') } finally { setSaving(false) }
  }
  return <div className="form">
    <p className="hint">The game server reports its map through @DZMBridge. Until it does, set it here.</p>
    <div className="row wrap">
      <label className="field">World (CfgWorlds name)
        <input list="dzm-worlds" value={world} placeholder="chernarusplus" onChange={(e) => setWorld(e.target.value)} />
        <datalist id="dzm-worlds">{KNOWN_WORLDS.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</datalist>
      </label>
      <label className="field">Size in metres
        <input inputMode="numeric" value={size} placeholder={known ? String(known[2]) : 'e.g. 15360'} onChange={(e) => setSize(e.target.value.replace(/\D/g, ''))} />
      </label>
      <button className="small primary" style={{ alignSelf: 'end' }} disabled={!ok || saving} onClick={() => save()}>Save</button>
      {info.source === 'manual' && <button className="small ghost" style={{ alignSelf: 'end' }} disabled={saving} onClick={() => save(true)}>Clear</button>}
    </div>
  </div>
}

function MapStatus({ live, bridge, tracking, active }: { live: ReturnType<typeof useLiveMap>; bridge: any; tracking: Tracking | null; active: Marker[] }) {
  const now = useNow(1000)
  const info = bridge?.connected ? bridge.info : null
  const data = live.data
  const fresh = isFresh(data, live.lastUpdate.current, now)
  const age = Math.max(0, Math.round((now - live.lastUpdate.current) / 1000))
  const version: string = info?.mod_version || ''
  const canConfirm = info?.tracking_revision != null
  const applied = canConfirm && tracking && info.tracking_revision === tracking.revision
  return (
    <Card title={<><Icon name="map" />Is the map live?</>}>
      <dl className="status-list">
        <dt>Positions</dt>
        <dd>{!data ? '…' : !data.available ? <Badge state="unconfigured">no data</Badge> : !fresh
          ? <><Badge state="stale">stale</Badge> <span className="muted">no update for {age} s. The game server may be restarting, or its connection is down.</span></>
          : data.source === 'bridge'
            ? <><Badge state="ok">live</Badge> <span className="muted">from the game server every {data.interval_s || 2} s, last {age} s ago</span></>
            : <><Badge state="warn">slow</Badge> <span className="muted">from MetricZ every 15 s, without motorbikes. {version && !newer(version, MIN_BRIDGE_LIVE) ? `Update @DZMBridge to ${MIN_BRIDGE_LIVE} or newer for live positions (installed: ${version}).` : 'Waiting for live positions from @DZMBridge.'}</span></>}</dd>
        <dt>On the map now</dt>
        <dd>{data?.players?.length ?? 0} players · {data?.vehicles?.length ?? 0} vehicles · {active.length} events and items</dd>
        <dt>Game server</dt>
        <dd>{!info ? <Badge state="warn">bridge not connected</Badge> : <>
          <Badge state="ok">@DZMBridge {version}</Badge>{' '}
          {info.census_running ? <span className="muted">scanning the world for tracked things…</span>
            : !canConfirm ? <span className="muted">this bridge version can't confirm settings were applied</span>
              : applied ? <span className="muted">has your latest tracking settings</span>
                : <span className="muted">picking up your latest tracking settings…</span>}
        </>}</dd>
        <dt>Remove events</dt>
        <dd><StaleSetting /></dd>
      </dl>
    </Card>
  )
}

function StaleSetting() {
  const s = useApi<{ stale_seconds: number }>('/api/settings/map')
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const notify = useNotify()
  useEffect(() => { if (s.data) setValue(String(s.data.stale_seconds)) }, [s.data?.stale_seconds])
  const n = +value
  const ok = Number.isInteger(n) && n >= 60 && n <= 3600
  const dirty = !!s.data && n !== s.data.stale_seconds
  const save = async () => {
    setSaving(true)
    try { await api.put('/api/settings/map', { stale_seconds: n }); notify('Saved. Applies right away.'); s.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setSaving(false) }
  }
  return <>
    <span className="row">
      after <input className="stale-input" inputMode="numeric" aria-label="Seconds without a ping" aria-invalid={!ok} value={value} onChange={(e) => setValue(e.target.value.replace(/\D/g, ''))} onKeyDown={(e) => { if (e.key === 'Enter' && ok && dirty) save() }} /> seconds without a ping
      {dirty && <button className="small primary" disabled={!ok || saving} onClick={save}>Save</button>}
    </span>
    <span className="hint">{ok ? 'The game server confirms every event and item every 30 s. Anything it stops confirming leaves the map after this long, and comes back if it is confirmed again.' : 'Use 60 to 3600 seconds.'}</span>
  </>
}

function VehicleIcons({ vehicles }: { vehicles: { class: string; type: string }[] }) {
  const [rules, setRules] = usePref<VehicleRule[]>('map.vehicleRules', [])
  const [match, setMatch] = useState('')
  const [picked, setPicked] = useState<MapIcon | null>(null)
  const classes = useMemo(() => {
    const m = new Map<string, { n: number; type: string }>()
    for (const v of vehicles) m.set(v.class, { n: (m.get(v.class)?.n ?? 0) + 1, type: v.type })
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [vehicles])
  const names = classes.map(([c]) => c)
  const countFor = (text: string) => classes.reduce((sum, [c, { n }]) => ruleMatches({ match: text }, c) ? sum + n : sum, 0)
  const typed = match.trim()
  // The icon follows what is typed until one is chosen by hand.
  const kind = picked ?? vehicleIcon({ class: typed, type: '' }, [])
  const set = (m: string, k: MapIcon) => setRules([{ match: m, kind: k }, ...rules.filter((r) => r.match.toLowerCase() !== m.toLowerCase())].slice(0, 100))
  const add = (text = typed) => { set(text, kind); setMatch(''); setPicked(null) }
  const seen = typed.length >= 2 ? classes.filter(([c]) => ruleMatches({ match: typed }, c)) : []
  const suggestion = typed.length >= 3 && !seen.length ? closestVehicle(typed, names) : undefined
  return (
    <Card title={<><Icon name="map" />Vehicle icons</>}>
      <p className="hint" style={{ marginTop: 0 }}>Changes show on the map immediately, on all your devices.</p>
      <h4>On the map now</h4>
      {!classes.length ? <Empty>No vehicles reported right now.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>Vehicle</th><th className="num">Count</th><th>Shown as</th><th /></tr></thead>
        <tbody>{classes.map(([cls, { n, type }]) => {
          const k = vehicleIcon({ class: cls, type }, rules)
          return <tr key={cls}>
            <td><code>{cls}</code></td>
            <td className="num">{n}</td>
            <td><span className="row">{ico(k)}<select value={k} onChange={(e) => set(cls, e.target.value as MapIcon)} style={{ width: 150 }} aria-label={`Icon for ${cls}`}>{vehicleKinds.map((x) => <option key={x} value={x}>{iconLabels[x]}</option>)}</select></span></td>
            <td><a className="button small ghost" href={'#/map/class/' + encodeURIComponent(cls)} title={`Show ${cls} on the map`}><Icon name="target" />Show</a></td>
          </tr>
        })}</tbody>
      </table></div>}
      <h4>Rules for a whole family</h4>
      <p className="hint" style={{ marginTop: 0 }}>A rule covers every vehicle whose name contains the text, including ones that spawn later. The longest match wins.</p>
      {rules.map((r) => {
        const n = countFor(r.match)
        const close = n === 0 ? closestVehicle(r.match, names) : undefined
        return <div className="rule-row" key={r.match}>
          <span><code>{r.match}</code>{close && <button className="small ghost" style={{ marginLeft: 6 }} title="Replace the rule with this spelling" onClick={() => setRules([{ match: close, kind: r.kind }, ...rules.filter((x) => x !== r)])}>Use {close}?</button>}</span>
          {n > 0 ? <span className="muted small nowrap">{n} on the map</span> : <span className="warn small nowrap" title="The rule waits until a vehicle with this name appears">none on the map</span>}
          <span className="row">{ico(r.kind)}<select value={r.kind} onChange={(e) => set(r.match, e.target.value as MapIcon)} aria-label={`Icon for ${r.match}`} style={{ flex: 1, minWidth: 0 }}>{vehicleKinds.map((x) => <option key={x} value={x}>{iconLabels[x]}</option>)}</select></span>
          <button className="icon ghost" title={`Remove the rule for ${r.match}`} aria-label={`Remove the rule for ${r.match}`} onClick={() => setRules(rules.filter((x) => x !== r))}><Icon name="close" /></button>
        </div>
      })}
      <div className="row" style={{ marginTop: 10 }}>
        <input list="vehicle-classes" placeholder="Name contains, e.g. Motorbike_01" value={match} onChange={(e) => setMatch(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && typed.length >= 2) add() }} style={{ flex: 1, minWidth: 0 }} aria-label="Vehicle name contains" />
        <datalist id="vehicle-classes">{names.map((c) => <option key={c} value={c} />)}</datalist>
        <select value={kind} onChange={(e) => setPicked(e.target.value as MapIcon)} style={{ width: 150 }} aria-label="Icon for the new rule">{vehicleKinds.map((x) => <option key={x} value={x}>{iconLabels[x]}</option>)}</select>
        <button className="small" disabled={typed.length < 2} onClick={() => add()}><Icon name="plus" />Add</button>
      </div>
      {typed.length >= 2 && <div className="rule-preview" role="status">
        {seen.length > 0
          ? <span className="okc">Matches {seen.reduce((s, [, v]) => s + v.n, 0)} on the map: {seen.slice(0, 4).map(([c]) => c).join(', ')}{seen.length > 4 ? '…' : ''}</span>
          : <span className="warn">Nothing on the map contains “{typed}” right now.</span>}
        {suggestion && <button className="small" onClick={() => { setMatch(suggestion); setPicked(null) }}>Did you mean {suggestion}?</button>}
      </div>}
    </Card>
  )
}

function TrackedThings({ tracking, active, invalid }: { tracking: ReturnType<typeof useApi<Tracking>>; active: Marker[]; invalid: string[] }) {
  const notify = useNotify()
  const [rows, setRows] = useState<[string, string][] | null>(null)
  const [items, setItems] = useState<string[] | null>(null)
  const [filter, setFilter] = useState('')
  const [nc, setNc] = useState({ cls: '', cat: 'heli_crash' })
  const [ni, setNi] = useState('')
  const [state, setState] = useState<'idle' | 'saving' | 'saved'>('idle')
  const timer = useRef<number | undefined>(undefined)
  const t = tracking.data
  useEffect(() => {
    if (t && rows == null) { setRows(Object.entries(t.event_classes).sort((a, b) => a[0].localeCompare(b[0]))); setItems([...t.rare_items]) }
  }, [t])
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const bad = useMemo(() => new Set(invalid), [invalid.join()])
  if (!t || !rows || !items) return tracking.error ? <Card title="Tracked events and items"><ErrorBox error={tracking.error} /></Card> : null
  // Every change saves itself: there is no separate step to forget.
  const commit = (nextRows: [string, string][], nextItems: string[]) => {
    setRows(nextRows); setItems(nextItems); setState('saving')
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(async () => {
      try {
        await api.put('/api/settings/tracking', { event_classes: Object.fromEntries(nextRows), rare_items: nextItems })
        setState('saved')
        tracking.reload()
      } catch (e: any) { setState('idle'); notify(e.message, 'bad') }
    }, 600)
  }
  // A base class covers its children, so count what the game server reports by name.
  const onMap = (cls: string) => {
    const stem = cls.toLowerCase().replace(/_base$/, '').replace(/_colorbase$/, '')
    return active.filter((m) => m.class.toLowerCase().startsWith(stem)).length
  }
  const q = filter.trim().toLowerCase()
  const shown = rows.filter(([c, cat]) => !q || (c + ' ' + cat).toLowerCase().includes(q))
  const clsTaken = rows.some(([c]) => c.toLowerCase() === nc.cls.trim().toLowerCase())
  const itemTaken = items.some((c) => c.toLowerCase() === ni.trim().toLowerCase())
  const addClass = () => { if (!reClass.test(nc.cls.trim()) || clsTaken || rows.length >= 200) return; commit([[nc.cls.trim(), nc.cat], ...rows], items); setNc({ ...nc, cls: '' }) }
  const addItem = () => { if (!reClass.test(ni.trim()) || itemTaken || items.length >= 200) return; commit(rows, [...items, ni.trim()]); setNi('') }
  return (
    <Card title="Tracked events and items" actions={state === 'saving' ? <span className="muted small">Saving…</span> : state === 'saved' ? <span className="okc small">Saved. The game server rescans within a minute.</span> : undefined}>
      {invalid.length > 0 && <div className="warnings">The game server doesn't know {invalid.length === 1 ? 'this class' : 'these classes'}: <b>{invalid.join(', ')}</b>. Check the spelling or remove {invalid.length === 1 ? 'it' : 'them'}.</div>}
      <p className="hint" style={{ marginTop: 0 }}>These are the things the game server looks for and reports. Vehicles are always on the map and don't belong here. Changes save by themselves; no restart is needed.</p>
      <h4>Items on the ground ({items.length})</h4>
      {!items.length ? <Empty>No items tracked.</Empty> : <div className="chips">{items.map((c) => {
        const n = onMap(c)
        return <span className="chip" key={c} title={bad.has(c) ? 'Unknown on the game server' : `${n} on the map now`}><span className={bad.has(c) ? 'warn' : ''}>{c}</span>{n > 0 && <span className="muted"> · {n}</span>}<button title={`Stop tracking ${c}`} aria-label={`Stop tracking ${c}`} onClick={() => commit(rows, items.filter((x) => x !== c))}><Icon name="close" /></button></span>
      })}</div>}
      <div className="row" style={{ marginTop: 10 }}>
        <input placeholder="Item class, e.g. PunchedCard" value={ni} aria-invalid={ni !== '' && (!reClass.test(ni.trim()) || itemTaken)} onChange={(e) => setNi(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') addItem() }} style={{ flex: 1, minWidth: 0 }} aria-label="Item class to track" />
        <button className="small" disabled={!reClass.test(ni.trim()) || itemTaken || items.length >= 200} onClick={addItem}><Icon name="plus" />Add</button>
      </div>
      {itemTaken && <span className="field-error">Already in the list.</span>}
      <p className="hint">Only items lying on the ground are shown, never inside inventories or containers.</p>
      <h4 style={{ marginTop: 18 }}>World events ({rows.length})</h4>
      <div className="row" style={{ marginBottom: 10 }}>
        <input placeholder="Event class, e.g. Wreck_UH1Y" value={nc.cls} aria-invalid={nc.cls !== '' && (!reClass.test(nc.cls.trim()) || clsTaken)} onChange={(e) => setNc({ ...nc, cls: e.target.value })} onKeyDown={(e) => { if (e.key === 'Enter') addClass() }} style={{ flex: 1, minWidth: 0 }} aria-label="Event class to track" />
        <select value={nc.cat} onChange={(e) => setNc({ ...nc, cat: e.target.value })} style={{ width: 150 }} aria-label="Shown as">{CATEGORIES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select>
        <button className="small" disabled={!reClass.test(nc.cls.trim()) || clsTaken || rows.length >= 200} onClick={addClass}><Icon name="plus" />Add</button>
      </div>
      {clsTaken && <span className="field-error">Already in the list.</span>}
      <p className="hint">A parent class covers everything that inherits from it.</p>
      {rows.length > 8 && <input type="search" placeholder="Filter this list" value={filter} onChange={(e) => setFilter(e.target.value)} style={{ marginBottom: 8, width: '100%' }} aria-label="Filter event classes" />}
      {!shown.length ? <Empty>{q ? 'No classes match.' : 'No event classes yet.'}</Empty> : <div className="table-scroll" style={{ maxHeight: 420, overflowY: 'auto' }}><table>
        <thead><tr><th>Class</th><th>Shown as</th><th className="num">On map</th><th /></tr></thead>
        <tbody>{shown.map(([c, cat]) => (
          <tr key={c}>
            <td><Trunc text={c} max={260} className={bad.has(c) ? 'warn' : ''} /> {bad.has(c) && <Badge state="warn">unknown</Badge>}</td>
            <td><select value={cat} onChange={(e) => commit(rows.map((r) => r[0] === c ? [c, e.target.value] : r), items)} aria-label={`Category for ${c}`}>{CATEGORIES.map(([k, l]) => <option key={k} value={k}>{l}</option>)}</select></td>
            <td className="num">{onMap(c)}</td>
            <td className="num"><button className="icon ghost" title={`Remove ${c}`} aria-label={`Remove ${c}`} onClick={() => commit(rows.filter((r) => r[0] !== c), items)}><Icon name="close" /></button></td>
          </tr>
        ))}</tbody>
      </table></div>}
    </Card>
  )
}
