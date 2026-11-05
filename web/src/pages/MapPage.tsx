import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import L from 'leaflet'
import { MapState, Marker, MzPlayer, MzVehicle, PlayerProfile } from '../api'
import { useApi } from '../hooks'
import { Badge, Confirm, CopyButton, Meter, Seg, steamUrl } from '../ui'
import { Icon } from '../icons'
import { ago, fromLatLng, grid, pct, toLatLng, TILE, WORLD } from '../util'
import { groupMarkers, iconLabels, iconSvg, MapIcon, markerIcon, markerTitle, vehicleIcon, vehicleKinds, VehicleRule } from '../mapIcons'
import { usePref } from '../prefs'
import { besides, eventDests, PlayerActions, Target, useRunAction } from '../playerActions'

const TILE_URL = '/tiles/chernarusplus/topographic/{z}/{x}/{y}.webp'
type Sel = { kind: 'player'; id: string } | { kind: 'vehicle'; v: MzVehicle } | { kind: 'event'; m: Marker; members: Marker[] } | null
type Layers = { players: boolean; vehicles: boolean; events: boolean; items: boolean; bases: boolean; deaths: boolean; ended: boolean }
const defaultLayers: Layers = { players: true, vehicles: true, events: true, items: true, bases: true, deaths: true, ended: false }
const layerNames: [keyof Layers, string][] = [['players', 'Players'], ['vehicles', 'Vehicles'], ['events', 'Events'], ['items', 'Items'], ['bases', 'Bases'], ['deaths', 'Deaths & raids'], ['ended', 'History']]
const HISTORY: [number, string][] = [[1, '1 h'], [6, '6 h'], [24, '24 h'], [72, '3 days'], [168, '7 days']]
type PendingTp = { target: Target; x: number; z: number; label: string }

function tooltip(text: string) { const node = document.createElement('span'); node.textContent = text; return node }
function entityIcon(kind: MapIcon, muted = false, count = 1) {
  return L.divIcon({ className: `entity-pin icon-${kind}${muted ? ' is-muted' : ''}`, html: iconSvg(kind) + (count > 1 ? `<span class="map-count">${count}</span>` : ''), iconSize: [20, 20], iconAnchor: [10, 10], tooltipAnchor: [0, -12] })
}
function playerIcon(stale: boolean, selected: boolean) {
  return L.divIcon({ className: '', html: `<div class="player-pin${stale ? ' stale' : ''}${selected ? ' sel' : ''}"></div>`, iconSize: [14, 14], iconAnchor: [7, 7], tooltipAnchor: [8, 0] })
}
const clampWorld = (v: number) => Math.max(1, Math.min(WORLD - 1, v))
/** MetricZ reports some stats as 0..1 fractions and others as raw DayZ values. */
const frac = (v: number | undefined, max: number) => v == null || !isFinite(v) ? null : Math.max(0, Math.min(1, v <= 1.0001 ? v : v / max))

export default function MapPage({ focusId, focusKey }: { focusId?: string; focusKey?: string } = {}) {
  const state = useApi<MapState>('/api/map/state', ['map'], 15000)
  const [storedLayersEarly] = usePref<Layers>('map.layers', defaultLayers)
  const historyOn = { ...defaultLayers, ...storedLayersEarly }.ended
  const [histHours, setHistHours] = usePref('map.historyHours', 6)
  const [histPos, setHistPos] = useState(100) // timeline position, % of the range (100 = now)
  const events = useApi<Marker[]>('/api/map/events?history_hours=' + (historyOn ? histHours : focusKey ? 24 : 2), ['map_events'], 30000)
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<L.Map | null>(null)
  const layers = useRef<Record<string, L.LayerGroup>>({})
  const playerMarkers = useRef(new Map<string, L.Marker>())
  const frozen = useRef<string | null>(null) // player being dragged / awaiting confirmation
  const [sel, setSel] = useState<Sel>(null)
  const [labels, setLabels] = usePref('map.labels', true)
  const [, setLayers] = usePref<Layers>('map.layers', defaultLayers)
  const show = { ...defaultLayers, ...storedLayersEarly }
  const [hiddenList, setHiddenList] = usePref<MapIcon[]>('map.hidden', [])
  const [vehicleRules, setVehicleRules] = usePref<VehicleRule[]>('map.vehicleRules', [])
  const hidden = useMemo(() => new Set(hiddenList), [hiddenList.join(',')])
  const [tilesOk, setTilesOk] = useState<boolean | null>(null)
  const [cursor, setCursor] = useState('')
  const [pick, setPick] = useState<Target | null>(null)
  const [pending, setPending] = useState<PendingTp | null>(null)
  const [tpBusy, setTpBusy] = useState(false)
  const [redraw, setRedraw] = useState(0)
  const runAction = useRunAction()
  const pickRef = useRef<Target | null>(null)
  pickRef.current = pick

  useEffect(() => {
    if (!el.current || map.current) return
    const m = L.map(el.current, { crs: L.CRS.Simple, minZoom: 0, maxZoom: 9, zoomSnap: 0.5, attributionControl: true })
    const bounds = L.latLngBounds([[-TILE, 0], [0, TILE]])
    L.tileLayer(TILE_URL, { maxNativeZoom: 6, maxZoom: 9, bounds, noWrap: true, tileSize: TILE, attribution: 'Tiles © Xam.nu · DZMap loader (private use)' }).addTo(m)
    m.fitBounds(bounds)
    m.setMaxBounds(bounds.pad(0.2))
    // Players always draw above every event, vehicle and item icon.
    m.createPane('dzm-players').style.zIndex = '640'
    m.on('mousemove', (e: L.LeafletMouseEvent) => {
      const g = fromLatLng(e.latlng.lat, e.latlng.lng)
      setCursor(`${g.x.toFixed(0)} / ${g.z.toFixed(0)} · ${grid(g.x, g.z)}`)
    })
    m.on('click', (e: L.LeafletMouseEvent) => {
      const t = pickRef.current
      if (!t) return
      const g = fromLatLng(e.latlng.lat, e.latlng.lng)
      const x = clampWorld(g.x), z = clampWorld(g.z)
      setPick(null)
      setPending({ target: t, x, z, label: `${x.toFixed(0)} / ${z.toFixed(0)} (${grid(x, z)})` })
    })
    for (const k of ['players', 'vehicles', 'events', 'items', 'bases']) layers.current[k] = L.layerGroup().addTo(m)
    map.current = m
    const img = new Image()
    img.onload = () => setTilesOk(true)
    img.onerror = () => setTilesOk(false)
    img.src = TILE_URL.replace('{z}', '0').replace('{x}', '0').replace('{y}', '0')
    return () => { m.remove(); map.current = null; layers.current = {}; playerMarkers.current.clear() }
  }, [])

  const data = state.data
  const players = data?.available ? data.players ?? [] : []
  // Opened from the Live players list: select that player and centre the map once.
  const focused = useRef<string | undefined>(undefined)
  useEffect(() => {
    const p = focusId ? players.find((x) => x.steam64 === focusId) : undefined
    if (!p || !map.current || focused.current === focusId) return
    focused.current = focusId
    setSel({ kind: 'player', id: p.steam64 })
    map.current.setView(toLatLng(p.x, p.z), 5)
  }, [focusId, data])
  const selPlayer = sel?.kind === 'player' ? players.find((p) => p.steam64 === sel.id) : undefined

  // Players: markers are kept per Steam64 and moved, so dragging one is never interrupted by a refresh.
  useEffect(() => {
    const g = layers.current.players
    if (!g) return
    const known = playerMarkers.current
    const stale = !data?.fresh
    const wanted = new Set<string>()
    if (show.players) for (const p of players) {
      wanted.add(p.steam64)
      const icon = playerIcon(stale, sel?.kind === 'player' && sel.id === p.steam64)
      let mk = known.get(p.steam64)
      if (!mk) {
        mk = L.marker(toLatLng(p.x, p.z), { icon, pane: 'dzm-players', draggable: true, autoPan: true, keyboard: true, title: p.name || p.steam64, alt: p.name || p.steam64 })
        const id = p.steam64
        mk.on('click', () => setSel({ kind: 'player', id }))
        // Never re-render this marker while it is dragged: replacing its icon aborts Leaflet's drag.
        mk.on('dragstart', () => { frozen.current = id })
        mk.on('dragend', (e) => {
          const ll = (e.target as L.Marker).getLatLng()
          const gg = fromLatLng(ll.lat, ll.lng)
          const x = clampWorld(gg.x), z = clampWorld(gg.z)
          const cur = playerMarkers.current.get(id) as L.Marker & { dzm?: MzPlayer }
          const pl = cur?.dzm
          setPending({ target: { steam64: id, name: pl?.name || id, be_guid: pl?.be_guid }, x, z, label: `${x.toFixed(0)} / ${z.toFixed(0)} (${grid(x, z)})` })
          setSel({ kind: 'player', id })
        })
        mk.addTo(g)
        ;(mk as L.Marker & { dzmLook?: string }).dzmLook = `${stale}/${sel?.kind === 'player' && sel.id === p.steam64}`
        known.set(p.steam64, mk)
      } else if (frozen.current !== p.steam64) {
        const look = `${stale}/${sel?.kind === 'player' && sel.id === p.steam64}`
        const tagged = mk as L.Marker & { dzmLook?: string }
        if (tagged.dzmLook !== look) mk.setIcon(icon)
        tagged.dzmLook = look
        mk.setLatLng(toLatLng(p.x, p.z))
      }
      ;(mk as L.Marker & { dzm?: MzPlayer }).dzm = p
      if (frozen.current === p.steam64) continue
      mk.unbindTooltip()
      if (labels) mk.bindTooltip(tooltip(p.name || p.steam64), { permanent: true, direction: 'right', className: 'lbl', pane: 'tooltipPane' })
      else mk.bindTooltip(tooltip(p.name || p.steam64), { direction: 'top' })
    }
    for (const [id, mk] of known) if (!wanted.has(id) && frozen.current !== id) { mk.remove(); known.delete(id) }
  }, [data, labels, show.players, sel, redraw])

  useEffect(() => {
    const g = layers.current.vehicles
    if (!g) return
    g.clearLayers()
    if (!data?.available || !show.vehicles) return
    for (const v of data.vehicles ?? []) {
      const kind = vehicleIcon(v, vehicleRules)
      if (hidden.has(kind)) continue
      const mk = L.marker(toLatLng(v.x, v.z), { icon: entityIcon(kind, !data.fresh), title: v.class, alt: v.class, keyboard: true })
      mk.bindTooltip(tooltip(v.class), { direction: 'top' })
      mk.on('click', () => setSel({ kind: 'vehicle', v }))
      mk.addTo(g)
    }
  }, [data, show.vehicles, hidden, vehicleRules])

  const sites = useMemo(() => groupMarkers(events.data ?? []), [events.data])
  // Which layer a marker belongs to, so the draw loop and the "hidden" notice agree.
  const markerLayer = (m: Marker) => {
    const kind = markerIcon(m)
    const item = ['tracked_item', 'rare_item'].includes(m.category) || (m.category === 'custom' && ['item', 'weapon'].includes(m.kind || ''))
    const base = m.category === 'base_flag'
    const transient = m.category === 'death' || m.category === 'raid'
    const hiddenBy = hidden.has(kind) ? 'icon' : (!show.items && item) || (!show.bases && base) || (transient && !show.deaths) || (!show.events && !item && !base && !transient) ? 'layer' : ''
    return { kind, item, base, transient, hiddenBy }
  }
  const hiddenActive = (events.data ?? []).filter((m) => m.status === 'active' && markerLayer(m).hiddenBy).length
  // Timeline: history shows only what started inside the selected slice of time.
  const range = histHours * 3600000
  const sliceLen = Math.max(30 * 60000, range / 6)
  const nowMinute = Math.floor(Date.now() / 60000) * 60000 // stable between renders (cursor moves re-render often)
  const sliceEnd = nowMinute - Math.round((1 - histPos / 100) * range)
  const sliceStart = sliceEnd - sliceLen
  useEffect(() => {
    const groups = layers.current
    if (!groups.events) return
    for (const name of ['events', 'items', 'bases']) groups[name].clearLayers()
    for (const site of sites) {
      const m = site.marker
      const { kind, item, base, hiddenBy } = markerLayer(m)
      const focusedOne = !!focusKey && m.key === focusKey
      if (!focusedOne) {
        if (hiddenBy) continue
        if (m.status !== 'active') {
          if (!show.ended) continue
          const t = new Date(m.started_at).getTime()
          if (t < sliceStart || t > sliceEnd) continue
        }
      }
      const live = m.status === 'active'
      const title = site.members.length > 1 ? `${iconLabels[kind]} · ${site.members.length} objects` : markerTitle(m)
      const g = groups[item ? 'items' : base ? 'bases' : 'events']
      const mk = L.marker(toLatLng(m.x, m.z), { icon: entityIcon(kind, !live, site.members.length), title, alt: title, keyboard: true, zIndexOffset: live ? 100 : 0 })
      mk.bindTooltip(tooltip(title), { direction: 'top', permanent: labels && (item || base), className: 'lbl' })
      mk.on('click', () => setSel({ kind: 'event', m, members: site.members }))
      mk.addTo(g)
    }
  }, [sites, show.events, show.ended, show.items, show.bases, show.deaths, labels, hidden, focusKey, sliceStart, sliceEnd])

  // Opened from a death in the Live feed: select it and centre the map once.
  const focusedKey = useRef<string | undefined>(undefined)
  useEffect(() => {
    const m = focusKey ? (events.data ?? []).find((x) => x.key === focusKey) : undefined
    if (!m || !map.current || focusedKey.current === focusKey) return
    focusedKey.current = focusKey
    setSel({ kind: 'event', m, members: [m] })
    map.current.setView(toLatLng(m.x, m.z), 5)
  }, [focusKey, events.data])

  useEffect(() => {
    const m = map.current
    if (!m || sel?.kind !== 'event' || sel.m.category !== 'base_flag' || !sel.m.radius) return
    const ring = L.circle(toLatLng(sel.m.x, sel.m.z), { radius: sel.m.radius * TILE / WORLD, color: '#c3a6ff', weight: 1, fillOpacity: 0.06, interactive: false }).addTo(m)
    return () => { ring.remove() }
  }, [sel])

  // A selected kill shows where the killer stood, joined to the victim.
  useEffect(() => {
    const m = map.current
    const kp = sel?.kind === 'event' && sel.m.category === 'death' ? sel.m.data?.killer_pos : undefined
    if (!m || !kp || sel?.kind !== 'event') return
    const line = L.polyline([toLatLng(sel.m.x, sel.m.z), toLatLng(kp.x, kp.z)], { color: '#ff5a3c', weight: 2, dashArray: '6 6', interactive: false }).addTo(m)
    const killer = L.marker(toLatLng(kp.x, kp.z), { icon: L.divIcon({ className: 'killer-pin', html: '<span></span>', iconSize: [14, 14], iconAnchor: [7, 7] }), interactive: false }).addTo(m)
    killer.bindTooltip(tooltip('Killer: ' + (sel.m.data?.killer_name || 'unknown')), { permanent: true, direction: 'right', className: 'lbl' })
    return () => { line.remove(); killer.remove() }
  }, [sel])

  useEffect(() => { el.current?.classList.toggle('picking', !!pick) }, [pick])
  useEffect(() => {
    if (!pick) return
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') setPick(null) }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [pick])

  const toggle = (k: keyof Layers) => setLayers({ ...show, [k]: !show[k] })
  const toggleKind = (kind: MapIcon) => setHiddenList((old) => old.includes(kind) ? old.filter((k) => k !== kind) : [...old, kind])
  const targets: Target[] = players.map((p) => ({ steam64: p.steam64, name: p.name || p.steam64, be_guid: p.be_guid }))
  const dests = eventDests(events.data)

  const cancelPending = () => { frozen.current = null; setPending(null); setRedraw((n) => n + 1) }
  const confirmTp = async () => {
    if (!pending) return
    setTpBusy(true)
    const ok = await runAction(pending.target, 'teleport', 'Teleport', { x: pending.x, z: pending.z })
    setTpBusy(false)
    frozen.current = null
    setPending(null)
    if (ok) {
      // Show the new spot right away; the next telemetry push confirms it.
      playerMarkers.current.get(pending.target.steam64)?.setLatLng(toLatLng(pending.x, pending.z))
      state.reload()
    } else setRedraw((n) => n + 1)
  }
  const activeEvents = events.data?.filter((m) => m.status === 'active').length ?? 0

  return (
    <div className="mapwrap">
      <div className="mapbar">
        <strong>Chernarus</strong>
        {data && (data.available
          ? <Badge state={data.fresh ? 'ok' : 'stale'} title={data.received_at ? 'Last telemetry ' + new Date(data.received_at).toLocaleString() : undefined}>{data.fresh ? 'live' : 'stale'} · {ago(data.received_at)}</Badge>
          : <Badge state="unconfigured">no MetricZ data</Badge>)}
        <span className="counts">{players.length} players · {data?.vehicles?.length ?? 0} vehicles · {activeEvents} active events</span>
        <div className="layers" role="group" aria-label="Map layers">
          {layerNames.map(([k, l]) => <button key={k} type="button" className="chip toggle" aria-pressed={show[k]} onClick={() => toggle(k)} title={`Show or hide ${l.toLowerCase()} (remembered for your account)`}>{l}</button>)}
        </div>
        <span className="row small muted" title="Names next to players, items and bases: always visible, or only when you hover an icon">Labels
          <Seg label="Labels" value={labels ? 'always' : 'hover'} onChange={(v) => setLabels(v === 'always')} options={[['always', 'Always'], ['hover', 'On hover']]} />
        </span>
        <span className="muted small grow right nowrap">{cursor}</span>
      </div>
      {tilesOk === false && <div className="warnings">Map tiles are not installed in /var/lib/dzm/tiles/chernarusplus (see docs/install-game-side.md). Markers still work.</div>}
      {data && !data.available && <div className="warnings">{data.reason}</div>}
      {hiddenActive > 0 && <div className="warnings row" role="status">
        <Icon name="events" /><span><b>{hiddenActive} active event{hiddenActive === 1 ? '' : 's'}</b> hidden by your map filters{hidden.size ? ` (hidden icons: ${[...hidden].map((k) => iconLabels[k]).join(', ')})` : ''}.</span>
        <button className="small" style={{ marginLeft: 'auto' }} onClick={() => { setLayers({ ...show, events: true, items: true, bases: true, deaths: true }); setHiddenList([]) }}>Show all</button>
      </div>}
      {(events.error || state.error) && <div className="warnings" role="status">Couldn’t refresh the map ({events.error || state.error}). Retrying automatically…</div>}
      <div className="mapbody">
        <div ref={el} className="map" />
        {pick && <div className="pick-banner"><Icon name="target" />Click the map to teleport {pick.name}<button className="small" onClick={() => setPick(null)}>Cancel</button></div>}
        {sel && <Inspector sel={sel} player={selPlayer} targets={targets} dests={dests} onClose={() => setSel(null)} onPick={(t) => setPick(t)}
          vehicleKind={sel.kind === 'vehicle' ? vehicleIcon(sel.v, vehicleRules) : undefined}
          onVehicleKind={(cls, kind) => setVehicleRules([{ match: cls, kind }, ...vehicleRules.filter((r) => r.match.toLowerCase() !== cls.toLowerCase())].slice(0, 100))}
          onTpHere={(t, m) => { const b = besides(m); setPending({ target: t, ...b, label: `beside ${markerTitle(m)} (${grid(b.x, b.z)})` }) }} />}
      </div>
      {show.ended && (
        <div className="timeline" role="group" aria-label="History timeline">
          <Seg label="History range" value={histHours} onChange={(v) => { setHistHours(v); setHistPos(100) }} options={HISTORY} />
          <input type="range" min={0} max={100} step={1} value={histPos} onChange={(e) => setHistPos(+e.target.value)} aria-label="Point in time" />
          {/* Fixed-width label and an always-present button: the slider must never change size while it is dragged. */}
          <span className="timeline-label">
            <span>{new Date(sliceStart).toLocaleString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })} – {histPos === 100 ? 'now' : new Date(sliceEnd).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })}</span>
            <span className="muted">{(events.data ?? []).filter((m) => m.status !== 'active' && +new Date(m.started_at) >= sliceStart && +new Date(m.started_at) <= sliceEnd).length} past events</span>
          </span>
          <button className="small ghost" disabled={histPos === 100} style={{ visibility: histPos === 100 ? 'hidden' : 'visible' }} onClick={() => setHistPos(100)}>Back to now</button>
        </div>
      )}
      <div className="legend small">
        <span className="map-key" title="Players are always drawn on top"><span className="dot" /><em>Players</em></span>
        {(Object.keys(iconLabels) as MapIcon[]).map((kind) => <button key={kind} className={`map-key icon-${kind}`} aria-pressed={!hidden.has(kind)} onClick={() => toggleKind(kind)} title={`${hidden.has(kind) ? 'Show' : 'Hide'} ${iconLabels[kind]} (remembered for your account)`}><span dangerouslySetInnerHTML={{ __html: iconSvg(kind) }} /><em>{iconLabels[kind]}</em></button>)}
        <span className="hint" title="Items show confirmed ground locations. History adds earlier observations. Numbered icons combine nearby objects of one scene.">Click an icon for details · drag a player to teleport</span>
      </div>
      {pending && (
        <Confirm title={`Teleport ${pending.target.name}?`} tone="primary" confirmLabel="Teleport" busy={tpBusy} onClose={cancelPending} onConfirm={confirmTp} body={
          <p>Move <b>{pending.target.name}</b> to <b>{pending.label}</b>. They are placed on the ground there; players inside a vehicle can't be moved.</p>} />
      )}
    </div>
  )
}

function Inspector({ sel, player, targets, dests, onClose, onPick, onTpHere, vehicleKind, onVehicleKind }: {
  sel: NonNullable<Sel>; player?: MzPlayer; targets: Target[]; dests: ReturnType<typeof eventDests>
  onClose: () => void; onPick: (t: Target) => void; onTpHere: (t: Target, m: Marker) => void
  vehicleKind?: MapIcon; onVehicleKind: (cls: string, kind: MapIcon) => void
}) {
  const id = sel.kind === 'player' ? sel.id : null
  const prof = useApi<PlayerProfile>(id ? '/api/players/' + encodeURIComponent(id) : null)
  const [who, setWho] = useState('')
  return (
    <div className="inspector" role="dialog" aria-label="Details">
      {sel.kind === 'player' && (!player ? (
        <><div className="head"><h3>Player</h3><button className="icon ghost" onClick={onClose} title="Close"><Icon name="close" /></button></div><p className="muted">This player is no longer reported on the map.</p></>
      ) : (() => {
        const t: Target = { steam64: player.steam64, name: player.name || player.steam64, be_guid: player.be_guid || prof.data?.player.be_guid }
        const flags = [player.unconscious && 'Unconscious', player.bleeding && 'Bleeding', player.restrained && 'Restrained', player.godmode && 'God mode'].filter(Boolean) as string[]
        return <>
          <div className="head">
            <h3>{player.name || 'Unknown'}</h3>
            <a className="button small" href={'#/players/' + player.steam64} title="Open full profile"><Icon name="profile" /></a>
            <button className="icon ghost" onClick={onClose} title="Close"><Icon name="close" /></button>
          </div>
          <dl>
            <dt>Steam64</dt><dd className="id-line"><a href={steamUrl(player.steam64)} target="_blank" rel="noreferrer noopener" title="Open Steam profile"><code>{player.steam64}</code></a><CopyButton value={player.steam64} label="Copy Steam64" /></dd>
            <dt>Position</dt><dd>{grid(player.x, player.z)} <span className="muted small">{player.x.toFixed(0)} / {player.z.toFixed(0)}</span><CopyButton value={`${player.x.toFixed(1)} ${player.z.toFixed(1)}`} label="Copy coordinates" /></dd>
            <dt>Ping</dt><dd>{player.ping_max != null ? `${player.ping_max} ms` : '—'}</dd>
            {prof.data && <><dt>Playtime</dt><dd>{Math.round(prof.data.player.playtime_seconds / 3600)} h · K/D {prof.data.player.kills}/{prof.data.player.deaths}</dd></>}
          </dl>
          <div className="bars">
            {([['Health', player.health, 1, '#3ccf7e'], ['Blood', player.blood, 5000, '#ef5b4c'], ['Water', player.water, 5000, '#3987e5'], ['Energy', player.energy, 20000, '#f1c40f']] as const).map(([l, v, max, c]) => {
              const f = frac(v, max)
              return <Fragment key={l}><span className="muted">{l}</span><Meter value={f} color={c} /><span title={v != null ? String(Math.round(v * 100) / 100) : 'Not reported by MetricZ'}>{f == null ? '—' : Math.round(f * 100) + '%'}</span></Fragment>
            })}
          </div>
          {flags.length > 0 && <p className="row wrap">{flags.map((f) => <Badge key={f} state="warn">{f}</Badge>)}</p>}
          <PlayerActions target={t} layout="grid" dests={dests} players={targets} onPick={() => onPick(t)} />
        </>
      })())}
      {sel.kind === 'vehicle' && (
        <>
          <div className="head"><h3>{sel.v.class}</h3><button className="icon ghost" onClick={onClose} title="Close"><Icon name="close" /></button></div>
          <dl>
            <dt>Type</dt><dd>{sel.v.type || '—'}</dd>
            <dt>Position</dt><dd>{grid(sel.v.x, sel.v.z)} <span className="muted small">{sel.v.x.toFixed(0)} / {sel.v.z.toFixed(0)}</span></dd>
            <dt>Health</dt><dd>{pct(sel.v.health)}</dd>
            <dt>Fuel</dt><dd>{pct(sel.v.fuel)}</dd>
            <dt>Crew</dt><dd>{sel.v.crew ?? '—'}</dd>
            <dt>Engine</dt><dd>{sel.v.engine_on == null ? '—' : sel.v.engine_on ? 'Running' : 'Off'}</dd>
            <dt>Show as</dt><dd>
              <select value={vehicleKind} onChange={(e) => onVehicleKind(sel.v.class, e.target.value as MapIcon)} aria-label="Map icon for this vehicle class" title="Remembered for every vehicle of this class">
                {vehicleKinds.map((k) => <option key={k} value={k}>{iconLabels[k]}</option>)}
              </select>
            </dd>
          </dl>
          <TpHere targets={targets} who={who} setWho={setWho} onGo={(t) => onTpHere(t, { ...sel.v, id: 0, key: '', category: 'custom', started_at: '', status: 'active', source: '', name: sel.v.class } as Marker)} />
        </>
      )}
      {sel.kind === 'event' && (
        <>
          <div className="head"><h3>{sel.members.length > 1 ? iconLabels[markerIcon(sel.m)] : markerTitle(sel.m)}</h3><button className="icon ghost" onClick={onClose} title="Close"><Icon name="close" /></button></div>
          <dl>
            <dt>Status</dt><dd><Badge state={sel.m.status}>{sel.m.status === 'active' ? 'on map now' : sel.m.status}</Badge></dd>
            {sel.m.category === 'death' && sel.m.data ? <>
              <dt>Victim</dt><dd>{sel.m.data.victim ? <a href={'#/players/' + sel.m.data.victim}>{sel.m.data.victim_name || sel.m.data.victim}</a> : '—'}</dd>
              {sel.m.data.killer ? <>
                <dt>Killer</dt><dd><a href={'#/players/' + sel.m.data.killer}>{sel.m.data.killer_name || sel.m.data.killer}</a></dd>
                <dt>Weapon</dt><dd>{sel.m.data.weapon || '—'}</dd>
                <dt>Distance</dt><dd>{sel.m.data.distance_m != null ? Math.round(sel.m.data.distance_m) + ' m' : '—'}</dd>
                {sel.m.data.killer_pos && <><dt>Killer stood</dt><dd>{grid(sel.m.data.killer_pos.x, sel.m.data.killer_pos.z)} <span className="muted small">(red dot, dashed line)</span></dd></>}
              </> : <><dt>Cause</dt><dd>{sel.m.data.cause || 'unknown'}</dd></>}
            </> : <><dt>Class</dt><dd>{sel.m.class || '—'}</dd></>}
            {sel.m.category === 'raid' && sel.m.data && <><dt>Part</dt><dd>{sel.m.data.part || '—'}</dd><dt>Tool</dt><dd>{sel.m.data.detail || '—'}</dd></>}
            {sel.m.container && <><dt>Inside</dt><dd>{sel.m.container}</dd></>}
            {sel.m.group && <><dt>Group</dt><dd>{sel.m.group}</dd></>}
            {!!sel.m.radius && <><dt>Territory</dt><dd>{sel.m.radius} m radius</dd></>}
            {sel.members.length > 1 && <><dt>Objects</dt><dd>{sel.members.length}<ul className="small">{sel.members.map((member) => <li key={member.id}>{markerTitle(member)} · {grid(member.x, member.z)}</li>)}</ul></dd></>}
            <dt>{sel.m.category === 'rare_item' ? sel.m.observation === 'item.observed' ? 'First observed' : 'Spawned' : 'Detected'}</dt><dd title={new Date(sel.m.started_at).toLocaleString()}>{ago(sel.m.started_at)}</dd>
            {sel.m.ended_at && <><dt>Ended</dt><dd title={new Date(sel.m.ended_at).toLocaleString()}>{ago(sel.m.ended_at)}</dd></>}
            <dt>Position</dt><dd>{grid(sel.m.x, sel.m.z)} <span className="muted small">{sel.m.x.toFixed(0)} / {sel.m.z.toFixed(0)}</span><CopyButton value={`${sel.m.x.toFixed(1)} ${sel.m.z.toFixed(1)}`} label="Copy coordinates" /></dd>
          </dl>
          {sel.m.status === 'active' && <TpHere targets={targets} who={who} setWho={setWho} onGo={(t) => onTpHere(t, sel.m)} />}
        </>
      )}
    </div>
  )
}

function TpHere({ targets, who, setWho, onGo }: { targets: Target[]; who: string; setWho: (v: string) => void; onGo: (t: Target) => void }) {
  if (!targets.length) return <p className="hint">No players online to teleport here.</p>
  const t = targets.find((x) => x.steam64 === who)
  return (
    <div className="qa-section">
      <h4>Teleport a player here</h4>
      <div className="row">
        <select value={who} onChange={(e) => setWho(e.target.value)} aria-label="Player to teleport" style={{ flex: 1, minWidth: 0 }}>
          <option value="">Choose player…</option>
          {targets.map((p) => <option key={p.steam64} value={p.steam64}>{p.name}</option>)}
        </select>
        <button className="primary small" disabled={!t} onClick={() => t && onGo(t)}><Icon name="teleport" />Go</button>
      </div>
      <p className="hint">Lands a few metres beside it so you can check the loot.</p>
    </div>
  )
}
