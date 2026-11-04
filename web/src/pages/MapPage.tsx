import { useEffect, useMemo, useRef, useState } from 'react'
import L from 'leaflet'
import { MapState, Marker, MzPlayer, MzVehicle } from '../api'
import { useApi } from '../hooks'
import { Badge } from '../ui'
import { ago, fromLatLng, grid, pct, toLatLng, TILE, WORLD } from '../util'
import { groupMarkers, iconLabels, iconSvg, MapIcon, markerIcon, markerTitle, vehicleIcon } from '../mapIcons'

const TILE_URL = '/tiles/chernarusplus/topographic/{z}/{x}/{y}.webp'
type Sel = { kind: 'player'; p: MzPlayer } | { kind: 'vehicle'; v: MzVehicle } | { kind: 'event'; m: Marker; members:Marker[] } | null
function tooltip(text:string) { const node=document.createElement('span');node.textContent=text;return node }
function entityIcon(kind:MapIcon,muted=false,count=1) {
 return L.divIcon({className:`entity-pin icon-${kind}${muted?' is-muted':''}`,html:iconSvg(kind)+(count>1?`<span class="map-count">${count}</span>`:''),iconSize:[30,30],iconAnchor:[15,15],tooltipAnchor:[0,-18]})
}

export default function MapPage() {
  const state = useApi<MapState>('/api/map/state', ['map'], 15000)
  const events = useApi<Marker[]>('/api/map/events', ['map_events'], 60000)
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<L.Map | null>(null)
  const layers = useRef<Record<string, L.LayerGroup>>({})
  const [sel, setSel] = useState<Sel>(null)
  const [labels, setLabels] = useState(true)
  const [show, setShow] = useState({ players: true, vehicles: true, events: true, ended: false, items: true, bases:true })
  const [hidden,setHidden]=useState<Set<MapIcon>>(()=>new Set())
  const [tilesOk, setTilesOk] = useState<boolean | null>(null)
  const [cursor, setCursor] = useState<string>('')

  useEffect(() => {
    if (!el.current || map.current) return
    const m = L.map(el.current, { crs: L.CRS.Simple, minZoom: 0, maxZoom: 9, zoomSnap: 0.5, attributionControl: true })
    const bounds = L.latLngBounds([[-TILE, 0], [0, TILE]])
    L.tileLayer(TILE_URL, { maxNativeZoom: 6, maxZoom: 9, bounds, noWrap: true, tileSize: TILE, attribution: 'Tiles © Xam.nu · DZMap loader (private use)' }).addTo(m)
    m.fitBounds(bounds)
    m.setMaxBounds(bounds.pad(0.2))
    m.on('mousemove', (e: L.LeafletMouseEvent) => {
      const g = fromLatLng(e.latlng.lat, e.latlng.lng)
      setCursor(`${g.x.toFixed(0)} / ${g.z.toFixed(0)} · ${grid(g.x, g.z)}`)
    })
    for (const k of ['players', 'vehicles', 'events','items','bases']) layers.current[k] = L.layerGroup().addTo(m)
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
    if (show.players) for (const p of data.players ?? []) {
      const mk = L.circleMarker(toLatLng(p.x, p.z), { radius: 6, color: '#fff', weight: 1.5, fillColor: '#2ecc71', fillOpacity: stale?0.45:0.95 })
      if (labels) mk.bindTooltip(tooltip(p.name || p.steam64), { permanent: true, direction: 'right', className: 'lbl' })
      mk.on('click', () => setSel({ kind: 'player', p }))
      mk.addTo(g.players)
    }
    if (show.vehicles) for (const v of data.vehicles ?? []) {
      const kind=vehicleIcon(v)
      if(hidden.has(kind))continue
      const mk = L.marker(toLatLng(v.x, v.z), {icon:entityIcon(kind,stale),title:v.class,alt:v.class,keyboard:true})
      mk.bindTooltip(tooltip(v.class), { direction: 'top' })
      mk.on('click', () => setSel({ kind: 'vehicle', v }))
      mk.addTo(g.vehicles)
    }
  }, [data, labels, show.players, show.vehicles,hidden])

  const sites=useMemo(()=>groupMarkers(events.data??[]),[events.data])
  useEffect(() => {
    const groups=layers.current
    if (!groups.events) return
    for(const name of ['events','items','bases'])groups[name].clearLayers()
    for (const site of sites) {
      const m=site.marker,kind=markerIcon(m)
      const item=['tracked_item','rare_item'].includes(m.category)||(m.category==='custom'&&['item','weapon'].includes(m.kind||''))
      const base=m.category==='base_flag'
      if (hidden.has(kind)||(!show.items&&item)||(!show.bases&&base)||(!show.events&&!item&&!base))continue
      if ((m.status === 'ended' || m.status === 'expired') && !show.ended) continue
      if(m.status==='historical'&&!show.ended)continue
      const live = m.status === 'active'
      const title=site.members.length>1?`${iconLabels[kind]} · ${site.members.length} observed objects`:markerTitle(m)
      const g=groups[item?'items':base?'bases':'events']
      const mk = L.marker(toLatLng(m.x, m.z), {icon:entityIcon(kind,!live,site.members.length),title,alt:title,keyboard:true})
      mk.bindTooltip(tooltip(title), { direction: 'top',permanent:labels&&(item||base),className:'lbl' })
      mk.on('click', () => setSel({ kind: 'event', m,members:site.members }))
      mk.addTo(g)
    }
  }, [sites, show.events, show.ended, show.items,show.bases,labels,hidden])

  useEffect(()=>{
    const m=map.current
    if(!m||sel?.kind!=='event'||sel.m.category!=='base_flag'||!sel.m.radius)return
    const ring=L.circle(toLatLng(sel.m.x,sel.m.z),{radius:sel.m.radius*TILE/WORLD,color:'#bba0ff',weight:1,fillOpacity:0.06,interactive:false}).addTo(m)
    return()=>{ring.remove()}
  },[sel])

  const toggle = (k: keyof typeof show) => setShow((s) => ({ ...s, [k]: !s[k] }))

  return (
    <div className="mapwrap">
      <div className="mapbar">
        <strong>Chernarus</strong>
        {data && (data.available
          ? <Badge state={data.fresh ? 'ok' : 'stale'}>{data.fresh ? 'live' : 'stale'} · {ago(data.received_at)}</Badge>
          : <Badge state="unconfigured">no MetricZ data</Badge>)}
        <span>{data?.players?.length ?? 0} players · {data?.vehicles?.length ?? 0} vehicles · {events.data?.filter((m) => m.status === 'active').length ?? 0} active event markers</span>
        <label><input type="checkbox" checked={show.players} onChange={() => toggle('players')} /> players</label>
        <label><input type="checkbox" checked={labels} onChange={() => setLabels(!labels)} /> names</label>
        <label><input type="checkbox" checked={show.vehicles} onChange={() => toggle('vehicles')} /> vehicles</label>
        <label><input type="checkbox" checked={show.events} onChange={() => toggle('events')} /> events</label>
        <label><input type="checkbox" checked={show.items} onChange={() => toggle('items')} /> items</label>
        <label><input type="checkbox" checked={show.bases} onChange={() => toggle('bases')} /> base flags</label>
        <label><input type="checkbox" checked={show.ended} onChange={() => toggle('ended')} /> history</label>
        <span className="muted small grow right">{cursor}</span>
      </div>
      {tilesOk === false && <div className="warnings">Map tiles are not installed in /var/lib/dzm/tiles/chernarusplus (see docs/install-game-side.md). Markers still work.</div>}
      {data && !data.available && <div className="warnings">{data.reason}</div>}
      <div className="mapbody">
        <div ref={el} className="map" />
        {sel && <Inspector sel={sel} onClose={() => setSel(null)} />}
      </div>
      <div className="legend small">
        <span><i style={{ background: '#2ecc71' }} /> Players</span>
        {(Object.keys(iconLabels) as MapIcon[]).map(kind=><button key={kind} className={`map-key icon-${kind}`} aria-pressed={!hidden.has(kind)} onClick={()=>setHidden(old=>{const next=new Set(old);if(next.has(kind))next.delete(kind);else next.add(kind);return next})} title={`Show or hide ${iconLabels[kind]}`}><span dangerouslySetInnerHTML={{__html:iconSvg(kind)}}/>{iconLabels[kind]}</button>)}
        <span className="muted">Items show confirmed ground locations. History includes earlier observations. Select an icon for details; numbered icons combine nearby objects.</span>
      </div>
    </div>
  )
}

function Inspector({ sel, onClose }: { sel: NonNullable<Sel>; onClose: () => void }) {
  return (
    <div className="inspector">
      <button className="link right" onClick={onClose}>Close</button>
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
          <h3>{sel.members.length>1?iconLabels[markerIcon(sel.m)]:markerTitle(sel.m)}</h3>
          <dl>
            <dt>Status</dt><dd><Badge state={sel.m.status} /></dd>
            <dt>Class</dt><dd>{sel.m.class || '—'}</dd>
            {sel.m.group&&<><dt>Group</dt><dd>{sel.m.group}</dd></>}
            {!!sel.m.radius&&<><dt>Territory radius</dt><dd>{sel.m.radius} m</dd></>}
            {sel.members.length>1&&<><dt>Observed objects</dt><dd>{sel.members.length}<ul>{sel.members.map(member=><li key={member.id}>{markerTitle(member)} · {grid(member.x,member.z)}</li>)}</ul></dd></>}
            <dt>{sel.m.category === 'rare_item' ? sel.m.observation === 'item.observed' ? 'First observed' : 'Spawned at' : 'Started'}</dt><dd>{new Date(sel.m.started_at).toLocaleString()}</dd>
            {sel.m.ended_at && <><dt>Ended</dt><dd>{new Date(sel.m.ended_at).toLocaleString()}</dd></>}
            <dt>Position</dt><dd>{sel.m.x.toFixed(0)} / {sel.m.z.toFixed(0)} ({grid(sel.m.x, sel.m.z)})</dd>
            <dt>Source</dt><dd>{sel.m.source}</dd>
          </dl>
        </>
      )}
    </div>
  )
}
