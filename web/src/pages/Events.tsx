import { useState } from 'react'
import { Marker } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox } from '../ui'
import { fmtTime, grid } from '../util'
import { LogDetails, LogPager, Page, readable, useLogQuery } from '../logs'

interface Event {id:number;ts:string;type:string;actor:string;actor_name:string;known:boolean;data:Record<string,unknown>}
export default function Events() {
 const mq=useLogQuery()
 const markers=useApi<Page<Marker>>(`/api/map/events?paged=true&limit=25&before=${mq.before}&q=${encodeURIComponent(mq.search)}`,mq.before?[]:['map_events'],60000)
 const q=useLogQuery(),[type,setType]=useState(''),[heartbeat,setHeartbeat]=useState(false)
 const raw=useApi<Page<Event>>(`/api/events?limit=25&before=${q.before}&q=${encodeURIComponent(q.search)}&type=${encodeURIComponent(type)}&heartbeat=${heartbeat}`,q.before?[]:['events'])
 return <><h1>Events</h1><Card title="World events and tracked items">
  <form className="row wrap log-filters" onSubmit={e=>{e.preventDefault();mq.submit()}}><label className="grow">Search classes, categories or status<input type="search" value={mq.input} onChange={e=>mq.setInput(e.target.value)}/></label><button type="submit">Search</button></form>
  {markers.error && <ErrorBox error={markers.error}/>}
  {!markers.data?.items.length?<Empty>No world events or item observations match. Tracking settings apply through the connected bridge.</Empty>:<div className="table-scroll"><table>
   <thead><tr><th>Category</th><th>Class</th><th>Status</th><th>Detected</th><th>Ended</th><th>Grid</th></tr></thead>
   <tbody>{markers.data.items.map(m=><tr key={m.id}><td>{readable(m.category)}</td><td>{m.class||'—'}</td><td><Badge state={m.status}>{m.observation==='item.observed'?'First observed':readable(m.status)}</Badge></td><td>{fmtTime(m.started_at)}</td><td>{fmtTime(m.ended_at)}</td><td>{grid(m.x,m.z)}</td></tr>)}</tbody>
  </table></div>}
  <LogPager page={mq.cursors.length} next={markers.data?.next_before} loading={markers.loading} onNewer={mq.newer} onOlder={mq.older} onLatest={mq.reset}/>
 </Card><Card title="Activity history">
  <form className="row wrap log-filters" onSubmit={e=>{e.preventDefault();q.submit()}}>
   <label className="grow">Search players, items or details<input type="search" value={q.input} onChange={e=>q.setInput(e.target.value)}/></label>
   <label>Event type<select value={type} onChange={e=>{setType(e.target.value);q.reset()}}><option value="">All activity</option>{['player.connect','player.disconnect','player.death','event.start','event.end','item.spawn','item.observed','server.start'].map(t=><option key={t} value={t}>{readable(t)}</option>)}</select></label>
   <button type="submit">Search</button>
   <label className="check"><input type="checkbox" checked={heartbeat} onChange={e=>{setHeartbeat(e.target.checked);q.reset()}}/>Show connection heartbeats</label>
  </form>
  {raw.error && <ErrorBox error={raw.error}/>}
  {!raw.data?<p role="status">Loading activity…</p>:!raw.data.items.length?<Empty>No activity matches these filters. Connection heartbeats are hidden by default.</Empty>:<div className="table-scroll"><table>
   <thead><tr><th>Time</th><th>Event</th><th>Player</th><th>Details</th></tr></thead>
   <tbody>{raw.data.items.map(e=><tr key={e.id}><td className="nowrap">{fmtTime(e.ts)}</td><td>{readable(e.type)} {!e.known&&<Badge state="warn">Not handled</Badge>}</td><td>{e.actor?<a href={'#/players/'+e.actor}>{e.actor_name||e.actor}</a>:'—'}</td><td><LogDetails params={e.data}/></td></tr>)}</tbody>
  </table></div>}
  <LogPager page={q.cursors.length} next={raw.data?.next_before} loading={raw.loading} onNewer={q.newer} onOlder={q.older} onLatest={q.reset}/>
 </Card></>
}
