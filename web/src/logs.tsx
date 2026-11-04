import { useState } from 'react'
import { fmtTime } from './util'

const names: Record<string,string> = {
 'restart.schedule_update':'Restart schedule updated', 'restart.drive_dryrun':'Restart simulated',
 'restart.drive':'Scheduled restart requested', 'restart.now':'Manual restart requested', 'restart.announce':'Restart announcement sent',
 'settings.tracking_update':'Item and event tracking updated', 'player.connect':'Player connected',
 'player.disconnect':'Player disconnected', 'player.death':'Player died', 'player.give_item':'Item created',
 'player.kill':'Kill player', 'player.heal':'Heal player', 'player.strip':'Clear inventory',
 'event.start':'World event detected', 'event.end':'World event ended', 'item.spawn':'Item spawned',
 'item.observed':'Ground item first observed', 'server.start':'Server started', heartbeat:'Connection heartbeat',
}
export const readable = (s: string) => names[s] || s.replace(/[._]/g,' ').replace(/^./,c=>c.toUpperCase())
const labels:Record<string,string> = { times:'Restart times', timezone:'Timezone', mode:'Restart control', enabled:'Schedule enabled',
 announce:'Countdown announcements', announce_minutes:'Announce minutes before', announce_text:'Announcement',
 window_minutes:'Detection window (± minutes)', timeout_minutes:'Recovery timeout (minutes)', dry_run:'Simulation only',
 event_classes:'Event classes', rare_items:'Tracked item classes', in_inventory:'Added to inventory', on_ground:'Placed on ground',
 to_player:'Destination player', item:'Item class', quantity:'Quantity', cmd:'Command', planned_for:'Planned restart', alive:'Player alive' }
export function displayValue(key:string,value:unknown):string {
 if (typeof value==='boolean') return value?'Yes':'No'
 if (value==null || value==='') return '—'
 if (key==='mode') return value==='drive'?'DZ Manager requests; provider restarts':'Provider schedule (observation only)'
 if (key==='planned_for') return fmtTime(String(value))
 if (Array.isArray(value)) return value.length?value.map(v=>displayValue('',v)).join(', '):'None'
 if (typeof value==='object') return Object.entries(value).map(([k,v])=>`${readable(k)}: ${displayValue(k,v)}`).join('; ')
 return String(value)
}
export function LogDetails({ params, detail }: { params?:Record<string,unknown>; detail?:string }) {
 let extra:unknown=detail
 try { extra=detail?JSON.parse(detail):null } catch { /* plain text result */ }
 const entries=Object.entries(params||{}).filter(([k])=>!['server_id','name','key'].includes(k))
 const content=<>
  {entries.length>0 && <dl>{entries.map(([k,v])=><div key={k}><dt>{labels[k]||readable(k)}</dt><dd>{displayValue(k,v)}</dd></div>)}</dl>}
  {extra!=null && extra!=='' && <p>{typeof extra==='object'?displayValue('',extra):readable(String(extra))}</p>}
  {!entries.length && !extra && <span className="muted">—</span>}
 </>
 const summary=Array.isArray(params?.times)
  ? `${params?.enabled?'Daily at '+params.times.join(', '):'Schedule disabled'} · ${params.timezone}${params.mode==='drive'&&params.dry_run?' · Simulation only':''}`
  : 'View details'
 return <div className="log-details">{entries.length>3?<details><summary>{summary}</summary>{content}</details>:content}</div>
}
export interface Page<T> { items:T[]; next_before?:number }
export function useLogQuery() {
 const [input,setInput]=useState(''),[search,setSearch]=useState(''),[cursors,setCursors]=useState<number[]>([0])
 const reset=()=>setCursors([0])
 return {input,setInput,search,cursors,before:cursors[cursors.length-1],reset,
  submit:()=>{setSearch(input.trim());reset()},older:(cursor:number)=>setCursors(c=>[...c,cursor]),newer:()=>setCursors(c=>c.slice(0,-1))}
}
export function LogPager({ page,next,loading,onNewer,onOlder,onLatest }:{page:number;next?:number;loading:boolean;onNewer:()=>void;onOlder:(id:number)=>void;onLatest:()=>void}) {
 return <nav className="row log-pager" aria-label="Log pages">
  <button disabled={page===1||loading} onClick={onLatest}>Latest</button>
  <button disabled={page===1||loading} onClick={onNewer}>Newer</button>
  <span aria-live="polite">Page {page}</span>
  <button disabled={!next||loading} onClick={()=>next&&onOlder(next)}>Older</button>
 </nav>
}
