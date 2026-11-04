import { useEffect, useRef, useState } from 'react'
import L from 'leaflet'
import { MapState, Marker, MzPlayer, MzVehicle } from '../api'
import { useApi } from '../hooks'
import { Badge } from '../ui'
import { ago, fromLatLng, grid, pct, toLatLng, TILE } from '../util'

const TILE_URL = '/tiles/chernarusplus/topographic/{z}/{x}/{y}.webp'
const catColor: Record<string, string> = { heli_crash: '#e67e22', airdrop: '#3498db', loot_box: '#9b59b6', custom: '#1abc9c', rare_item: '#f1c40f' }

type Sel = { kind: 'player'; p: MzPlayer } | { kind: 'vehicle'; v: MzVehicle } | { kind: 'event'; m: Marker } | null

export default function MapPage() {
  const state = useApi<MapState>('/api/map/state', ['map'], 15000)
  const events = useApi<Marker[]>('/api/map/events', ['map_events'], 60000)
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<L.Map | null>(null)
  const layers = useRef<Record<string, L.LayerGroup>>({})
  const [sel, setSel] = useState<Sel>(null)
  const [labels, setLabels] = useState(true)
  const [show, setShow] = useState({ players: true, vehicles: true, events: true, ended: false, items: true })
  const [tilesOk, setTilesOk] = useState<boolean | null>(null)
  const [cursor, setCursor] = useState<string>('')

  useEffect(() => {
    if (!el.current || map.current) return
    const m = L.map(el.current, { crs: L.CRS.Simple, minZoom: 0, maxZoom: 9, zoomSnap: 0.5, attributionControl: true })
    const bounds = L.latLngBounds([[-TILE, 0], [0, TILE]])
    L.tileLayer(TILE_URL, { maxNativeZoom: 7, maxZoom: 9, bounds, noWrap: true, tileSize: TILE, attribution: 'Tiles: DZMap loader (private use)' }).addTo(m)
    m.fitBounds(bounds)
    m.setMaxBounds(bounds.pad(0.2))
    m.on('mousemove', (e: L.LeafletMouseEvent) => {
      const g = fromLatLng(e.latlng.lat, e.latlng.lng)
      setCursor(`${g.x.toFixed(0)} / ${g.z.toFixed(0)} · ${grid(g.x, g.z)}`)
    })
    for (const k of ['players', 'vehicles', 'events']) layers.current[k] = L.layerGroup().addTo(m)
    map.current = m
    const img = new Image()
    img.onload = () => setTilesOk(true)
    img.onerror = () => setTilesOk(false)
    img.src = TILE_URL.replace('{z}', '0').replace('{x}', '0').replace('{y}', '0')
    return () => { m.remove(); map.current = null }
  }, [])

  const data = state.data
  useEffect(() => {
    const g = layers.current
    if (!g.players) return
    g.players.clearLayers()
    g.vehicles.clearLayers()
    if (!data?.available) return
    const stale = !data.fresh
    if (show.players) for (const p of data.players) {
      const mk = L.circleMarker(toLatLng(p.x, p.z), { radius: 6, color: '#fff', weight: 1.5, fillColor: stale ? '#888' : p.unconscious ? '#e74c3c' : '#2ecc71', fillOpacity: 0.95 })
      if (labels) mk.bindTooltip(p.name || p.steam64, { permanent: true, direction: 'right', className: 'lbl' })
      mk.on('click', () => setSel({ kind: 'player', p }))
      mk.addTo(g.players)
    }
    if (show.vehicles) for (const v of data.vehicles) {
      const mk = L.circleMarker(toLatLng(v.x, v.z), { radius: 5, color: '#000', weight: 1, fillColor: stale ? '#888' : v.engine_on ? '#f39c12' : '#95a5a6', fillOpacity: 0.9 })
      mk.bindTooltip(v.class, { direction: 'top' })
      mk.on('click', () => setSel({ kind: 'vehicle', v }))
      mk.addTo(g.vehicles)
    }
  }, [data, labels, show.players, show.vehicles])

  useEffect(() => {
    const g = layers.current.events
    if (!g) return
    g.clearLayers()
    if (!show.events || !events.data) return
    for (const m of events.data) {
      if (m.category === 'rare_item' && !show.items) continue
      if ((m.status === 'ended' || m.status === 'expired') && !show.ended) continue
      const live = m.status === 'active'
      const mk = L.circleMarker(toLatLng(m.x, m.z), { radius: m.category === 'rare_item' ? 4 : 9, color: catColor[m.category] || '#fff', weight: live ? 3 : 1, dashArray: live ? undefined : '3 3', fillOpacity: live ? 0.4 : 0.1 })
      mk.bindTooltip(`${m.category.replace('_', ' ')} · ${m.status}`, { direction: 'top' })
      mk.on('click', () => setSel({ kind: 'event', m }))
      mk.addTo(g)
    }
  }, [events.data, show.events, show.ended, show.items])

  const toggle = (k: keyof typeof show) => setShow((s) => ({ ...s, [k]: !s[k] }))

  return (
    <div className="mapwrap">
      <div className="mapbar">
        <strong>Chernarus</strong>
        {data && (data.available
          ? <Badge state={data.fresh ? 'ok' : 'stale'}>{data.fresh ? 'live' : 'stale'} · {ago(data.received_at)}</Badge>
          : <Badge state="unconfigured">no MetricZ data</Badge>)}
        <span>{data?.players.length ?? 0} players · {data?.vehicles.length ?? 0} vehicles · {events.data?.filter((m) => m.status === 'active').length ?? 0} active events</span>
        <label><input type="checkbox" checked={show.players} onChange={() => toggle('players')} /> players</label>
        <label><input type="checkbox" checked={labels} onChange={() => setLabels(!labels)} /> names</label>
        <label><input type="checkbox" checked={show.vehicles} onChange={() => toggle('vehicles')} /> vehicles</label>
        <label><input type="checkbox" checked={show.events} onChange={() => toggle('events')} /> events</label>
        <label><input type="checkbox" checked={show.items} onChange={() => toggle('items')} /> rare items</label>
        <label><input type="checkbox" checked={show.ended} onChange={() => toggle('ended')} /> ended</label>
        <span className="muted small grow right">{cursor}</span>
      </div>
      {tilesOk === false && <div className="warnings">Map tiles are not installed in /var/lib/dzm/tiles/chernarusplus (see docs/install-game-side.md). Markers still work.</div>}
      {data && !data.available && <div className="warnings">{data.reason}</div>}
      <div className="mapbody">
        <div ref={el} className="map" />
        {sel && <Inspector sel={sel} onClose={() => setSel(null)} />}
      </div>
      <div className="legend small">
        <span><i style={{ background: '#2ecc71' }} /> player</span><span><i style={{ background: '#e74c3c' }} /> unconscious</span>
        <span><i style={{ background: '#f39c12' }} /> vehicle (engine on)</span><span><i style={{ background: '#95a5a6' }} /> vehicle</span>
        {Object.entries(catColor).map(([k, c]) => <span key={k}><i style={{ background: c }} /> {k.replace('_', ' ')}</span>)}
        <span className="muted">Rare items are spawn records ("spawned at"), never a current location. No AI, infected or position history is tracked.</span>
      </div>
    </div>
  )
}

function Inspector({ sel, onClose }: { sel: NonNullable<Sel>; onClose: () => void }) {
  return (
    <div className="inspector">
      <button className="link right" onClick={onClose}>✕</button>
      {sel.kind === 'player' && (
        <>
          <h3>{sel.p.name || 'Unknown'}</h3>
          <dl>
            <dt>Steam64</dt><dd><code>{sel.p.steam64}</code></dd>
            <dt>Position</dt><dd>{sel.p.x.toFixed(0)} / {sel.p.z.toFixed(0)} ({grid(sel.p.x, sel.p.z)})</dd>
            <dt>Health</dt><dd>{pct(sel.p.health)}</dd>
            <dt>Blood</dt><dd>{sel.p.blood?.toFixed(0) ?? 'not reported'}</dd>
            <dt>Status</dt><dd>{[sel.p.unconscious && 'unconscious', sel.p.bleeding && 'bleeding', sel.p.restrained && 'restrained', sel.p.godmode && 'GODMODE'].filter(Boolean).join(', ') || 'normal'}</dd>
            <dt>Ping</dt><dd>{sel.p.ping_max ?? 'not reported'}</dd>
          </dl>
          <a href={'#/players/' + sel.p.steam64}>Open profile & actions →</a>
        </>
      )}
      {sel.kind === 'vehicle' && (
        <>
          <h3>{sel.v.class}</h3>
          <dl>
            <dt>Type</dt><dd>{sel.v.type || '—'}</dd>
            <dt>Position</dt><dd>{sel.v.x.toFixed(0)} / {sel.v.z.toFixed(0)} ({grid(sel.v.x, sel.v.z)})</dd>
            <dt>Health</dt><dd>{pct(sel.v.health)}</dd>
            <dt>Fuel</dt><dd>{pct(sel.v.fuel)}</dd>
            <dt>Crew</dt><dd>{sel.v.crew ?? 'not reported'}</dd>
            <dt>Engine</dt><dd>{sel.v.engine_on == null ? 'not reported' : sel.v.engine_on ? 'on' : 'off'}</dd>
          </dl>
        </>
      )}
      {sel.kind === 'event' && (
        <>
          <h3>{sel.m.category.replace('_', ' ')}</h3>
          <dl>
            <dt>Status</dt><dd><Badge state={sel.m.status} /></dd>
            <dt>Class</dt><dd>{sel.m.class || '—'}</dd>
            <dt>{sel.m.category === 'rare_item' ? 'Spawned at' : 'Started'}</dt><dd>{new Date(sel.m.started_at).toLocaleString()}</dd>
            {sel.m.ended_at && <><dt>Ended</dt><dd>{new Date(sel.m.ended_at).toLocaleString()}</dd></>}
            <dt>Position</dt><dd>{sel.m.x.toFixed(0)} / {sel.m.z.toFixed(0)} ({grid(sel.m.x, sel.m.z)})</dd>
            <dt>Source</dt><dd>{sel.m.source}</dd>
          </dl>
        </>
      )}
    </div>
  )
}
