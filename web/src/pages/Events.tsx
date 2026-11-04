import { useState } from 'react'
import { Marker } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, PageHead, Trunc } from '../ui'
import { ago, fmtTime, grid } from '../util'
import { ActionName, LogDetails, LogPager, Page, readable, useLogQuery } from '../logs'
import { iconSvg, markerIcon, markerTitle } from '../mapIcons'

interface Event { id: number; ts: string; type: string; actor: string; actor_name: string; known: boolean; data: Record<string, unknown> }
const TYPES = ['player.connect', 'player.disconnect', 'player.death', 'event.start', 'event.end', 'item.spawn', 'item.observed', 'server.start']

export default function Events() {
  const mq = useLogQuery()
  const markers = useApi<Page<Marker>>(`/api/map/events?paged=true&limit=25&before=${mq.before}&q=${encodeURIComponent(mq.search)}`, mq.before ? [] : ['map_events'], 60000)
  const q = useLogQuery(), [type, setType] = useState(''), [heartbeat, setHeartbeat] = useState(false)
  const raw = useApi<Page<Event>>(`/api/events?limit=25&before=${q.before}&q=${encodeURIComponent(q.search)}&type=${encodeURIComponent(type)}&heartbeat=${heartbeat}`, q.before ? [] : ['events'])
  return <>
    <PageHead title="Events" sub="World events, tracked items and player activity" />
    <Card title="World events and tracked items">
      <form className="row wrap log-filters" onSubmit={(e) => { e.preventDefault(); mq.submit() }}><label className="grow">Search classes, categories or status<input type="search" value={mq.input} onChange={(e) => mq.setInput(e.target.value)} /></label><button type="submit">Search</button></form>
      {markers.error && <ErrorBox error={markers.error} />}
      {!markers.data?.items.length ? <Empty>No world events or item observations match. Tracking settings apply through the connected bridge.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>What</th><th>Class</th><th>Status</th><th>Detected</th><th>Ended</th><th>Grid</th></tr></thead>
        <tbody>{markers.data.items.map((m) => { const k = markerIcon(m); return <tr key={m.id}>
          <td><span className="row"><span className={`entity-pin icon-${k}`} style={{ width: 16, height: 16, display: 'inline-block' }} dangerouslySetInnerHTML={{ __html: iconSvg(k) }} />{markerTitle(m) === m.class ? readable(m.category) : markerTitle(m)}</span></td>
          <td><Trunc text={m.class} max={200} /></td>
          <td><Badge state={m.status}>{m.observation === 'item.observed' ? 'First observed' : readable(m.status)}</Badge></td>
          <td title={fmtTime(m.started_at)}>{ago(m.started_at)}</td><td title={fmtTime(m.ended_at)}>{m.ended_at ? ago(m.ended_at) : '—'}</td>
          <td><a href="#/map" title={`${m.x.toFixed(0)} / ${m.z.toFixed(0)}`}>{grid(m.x, m.z)}</a></td>
        </tr> })}</tbody>
      </table></div>}
      <LogPager page={mq.cursors.length} next={markers.data?.next_before} loading={markers.loading} onNewer={mq.newer} onOlder={mq.older} onLatest={mq.reset} />
    </Card>
    <Card title="Activity history">
      <form className="row wrap log-filters" onSubmit={(e) => { e.preventDefault(); q.submit() }}>
        <label className="grow">Search players, items or details<input type="search" value={q.input} onChange={(e) => q.setInput(e.target.value)} /></label>
        <label>Event type<select value={type} onChange={(e) => { setType(e.target.value); q.reset() }}><option value="">All activity</option>{TYPES.map((t) => <option key={t} value={t}>{readable(t)}</option>)}</select></label>
        <button type="submit">Search</button>
        <label className="check"><input type="checkbox" checked={heartbeat} onChange={(e) => { setHeartbeat(e.target.checked); q.reset() }} />Show connection heartbeats</label>
      </form>
      {raw.error && <ErrorBox error={raw.error} />}
      {!raw.data ? <p role="status" className="muted">Loading activity…</p> : !raw.data.items.length ? <Empty>No activity matches these filters. Connection heartbeats are hidden by default.</Empty> : <div className="table-scroll"><table>
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
