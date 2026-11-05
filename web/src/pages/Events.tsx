import { Marker } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, PageHead, Seg, Trunc } from '../ui'
import { ago, fmtTime, grid } from '../util'
import { ActionName, LogDetails, LogPager, Page, readable, useLogQuery } from '../logs'
import { iconLabels, iconSvg, MapIcon, markerIcon, markerTitle } from '../mapIcons'
import { usePref } from '../prefs'
import { Icon, IconName } from '../icons'

interface Event { id: number; ts: string; type: string; actor: string; actor_name: string; known: boolean; data: Record<string, unknown> }

// World-event categories as the map draws them.
const CATS: [string, MapIcon, string][] = [
  ['heli_crash', 'heli', 'Helicrash'], ['plane_crash', 'plane', 'Aircraft crash'], ['military_convoy', 'convoy', 'Convoy'], ['police', 'police', 'Police'],
  ['train', 'train', 'Train'], ['airdrop', 'airdrop', 'Airdrop'], ['loot_box', 'item', 'Loot box'], ['base_flag', 'flag', 'Base flag'], ['raid', 'raid', 'Raid'],
  ['tracked_item', 'item', 'Tracked item'], ['rare_item', 'item', 'Item spawn'], ['custom', 'item', 'Other'],
]
// Activity types with an icon each.
const TYPES: [string, IconName][] = [['player.connect', 'kick'], ['player.disconnect', 'kick'], ['player.death', 'skull'], ['raid.hit', 'shield'], ['raid.destroyed', 'shield'], ['raid.codelock', 'lock'], ['event.start', 'events'], ['event.end', 'events'], ['item.spawn', 'gift'], ['item.observed', 'gift'], ['server.start', 'power']]
type Order = 'desc' | 'asc'

export default function Events() {
  const mq = useLogQuery()
  const [cats, setCats] = usePref<string[]>('events.categories', [])
  const [mOrder, setMOrder] = usePref<Order>('events.order', 'desc')
  const markers = useApi<Page<Marker>>(`/api/map/events?paged=true&limit=25&before=${mq.before}&q=${encodeURIComponent(mq.search)}&cat=${cats.join(',')}&order=${mOrder}`, mq.before ? [] : ['map_events'], 60000)
  const q = useLogQuery()
  const [types, setTypes] = usePref<string[]>('activity.types', [])
  const [aOrder, setAOrder] = usePref<Order>('activity.order', 'desc')
  const [heartbeat, setHeartbeat] = usePref('activity.heartbeat', false)
  const raw = useApi<Page<Event>>(`/api/events?limit=25&before=${q.before}&q=${encodeURIComponent(q.search)}&types=${encodeURIComponent(types.join(','))}&heartbeat=${heartbeat}&order=${aOrder}`, q.before ? [] : ['events'])
  const toggle = (list: string[], v: string) => list.includes(v) ? list.filter((x) => x !== v) : [...list, v]
  const sortSeg = (v: Order, set: (o: Order) => void, reset: () => void) => <Seg label="Sort" value={v} onChange={(o) => { set(o); reset() }} options={[['desc', 'Newest first'], ['asc', 'Oldest first']]} />
  return <>
    <PageHead title="Events" sub="World events, tracked items and player activity · filters are remembered for your account" />
    <Card title="World events and tracked items" actions={sortSeg(mOrder, setMOrder, mq.reset)}>
      <div className="filter-chips" role="group" aria-label="Categories">
        <button type="button" className="chip toggle" aria-pressed={!cats.length} onClick={() => { setCats([]); mq.reset() }}>All</button>
        {CATS.map(([c, icon, label]) => <button key={c} type="button" className={`chip toggle icon-${icon}`} aria-pressed={cats.includes(c)} onClick={() => { setCats(toggle(cats, c)); mq.reset() }}><span className="chip-ico" dangerouslySetInnerHTML={{ __html: iconSvg(icon) }} /><em>{label}</em></button>)}
      </div>
      <form className="row wrap log-filters" onSubmit={(e) => { e.preventDefault(); mq.submit() }}><label className="grow">Search classes or status<input type="search" value={mq.input} onChange={(e) => mq.setInput(e.target.value)} /></label><button type="submit">Search</button></form>
      {markers.error && <ErrorBox error={markers.error} />}
      {!markers.data?.items.length ? <Empty>No world events or item observations match.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>What</th><th>Class</th><th>Status</th><th>Detected</th><th>Ended</th><th>Grid</th></tr></thead>
        <tbody>{markers.data.items.map((m) => { const k = markerIcon(m); return <tr key={m.id}>
          <td><span className="row"><span className={`entity-pin icon-${k}`} style={{ width: 16, height: 16, display: 'inline-block' }} dangerouslySetInnerHTML={{ __html: iconSvg(k) }} />{markerTitle(m) === m.class ? (CATS.find((c) => c[0] === m.category)?.[2] || iconLabels[k]) : markerTitle(m)}</span></td>
          <td><Trunc text={m.class} max={240} /></td>
          <td><Badge state={m.status}>{m.observation === 'item.observed' ? 'First observed' : readable(m.status)}</Badge></td>
          <td title={fmtTime(m.started_at)}>{ago(m.started_at)}</td><td title={fmtTime(m.ended_at)}>{m.ended_at ? ago(m.ended_at) : '—'}</td>
          <td><a href="#/map" title={`${m.x.toFixed(0)} / ${m.z.toFixed(0)}`}>{grid(m.x, m.z)}</a></td>
        </tr> })}</tbody>
      </table></div>}
      <LogPager page={mq.cursors.length} next={markers.data?.next_before} loading={markers.loading} onNewer={mq.newer} onOlder={mq.older} onLatest={mq.reset} />
    </Card>
    <Card title="Activity history" actions={sortSeg(aOrder, setAOrder, q.reset)}>
      <div className="filter-chips" role="group" aria-label="Activity types">
        <button type="button" className="chip toggle" aria-pressed={!types.length} onClick={() => { setTypes([]); q.reset() }}>All</button>
        {TYPES.map(([t, icon]) => <button key={t} type="button" className="chip toggle" aria-pressed={types.includes(t)} onClick={() => { setTypes(toggle(types, t)); q.reset() }}><Icon name={icon} /><em>{readable(t)}</em></button>)}
        <label className="check small" style={{ marginLeft: 'auto' }}><input type="checkbox" checked={heartbeat} onChange={(e) => { setHeartbeat(e.target.checked); q.reset() }} />Show heartbeats</label>
      </div>
      <form className="row wrap log-filters" onSubmit={(e) => { e.preventDefault(); q.submit() }}>
        <label className="grow">Search players, items or details<input type="search" value={q.input} onChange={(e) => q.setInput(e.target.value)} /></label>
        <button type="submit">Search</button>
      </form>
      {raw.error && <ErrorBox error={raw.error} />}
      {!raw.data ? <p role="status" className="muted">Loading activity…</p> : !raw.data.items.length ? <Empty>No activity matches these filters.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>When</th><th>Event</th><th>Player</th><th>Details</th></tr></thead>
        <tbody>{raw.data.items.map((e) => <tr key={e.id}>
          <td className="nowrap" title={fmtTime(e.ts)}>{ago(e.ts)}</td>
          <td><ActionName action={e.type} /> {!e.known && <Badge state="warn" title="DZ Manager stored this event but has no handler for it">not handled</Badge>}</td>
          <td>{e.actor ? <a href={'#/players/' + e.actor} title={e.actor}>{e.actor_name || e.actor}</a> : <span className="muted">—</span>}</td>
          <td><LogDetails params={e.data} /></td>
        </tr>)}</tbody>
      </table></div>}
      <LogPager page={q.cursors.length} next={raw.data?.next_before} loading={raw.loading} onNewer={q.newer} onOlder={q.older} onLatest={q.reset} />
    </Card>
  </>
}
