import { useState } from 'react'
import { fmtTime } from './util'

const names: Record<string,string> = {
 'restart.schedule_update':'Restart schedule updated', 'restart.drive_dryrun':'Restart step simulated',
 'restart.drive':'Scheduled restart command sent', 'restart.now':'Server restart requested', 'restart.announce':'Restart warning sent',
 'restart.lock':'Server locked before restart', 'restart.unlock':'Server unlocked again', 'restart.kick_all':'Everyone kicked before restart',
 'restart.countdown':'Countdown restart started', 'restart.cancel':'Countdown restart cancelled', 'restart.countdown_expired':'Countdown restart expired',
 'restart.drive_verification':'Restart control verification changed',
 'rcon.broadcast':'Message to everyone', 'rcon.message':'Private message', 'rcon.kick':'Player kicked', 'rcon.kick_all':'All players kicked',
 'rcon.lock':'Server locked', 'rcon.unlock':'Server unlocked', 'players.reveal_ip':'Player IPs revealed',
 'settings.tracking_update':'Item and event tracking updated', 'player.connect':'Player connected',
 'player.disconnect':'Player disconnected', 'player.death':'Player died', 'player.give_item':'Item given',
 'player.kill':'Player killed by admin', 'player.heal':'Player healed', 'player.strip':'Inventory cleared', 'player.teleport':'Player teleported',
 'ban.create':'Player banned', 'ban.revoke':'Ban revoked', 'banlist.import':'External ban list imported', 'banlist.review':'External listing reviewed',
 'profile.create':'Server added', 'profile.update':'Server settings changed', 'profile.reveal_wizard':'Connection configs viewed',
 'profile.activate':'Active server changed', 'profile.archive':'Server archived', 'profile.rotate_ingest':'Ingest keys rotated',
 'priority.add':'Added to priority queue', 'priority.remove':'Removed from priority queue', 'priority.applied':'Priority queue marked applied',
 'raid.hit':'Base under attack', 'raid.destroyed':'Base part destroyed', 'raid.codelock':'Code lock raided',
 'banlist.sync':'External ban list synced from the server', 'settings.intel_update':'Player check settings changed', 'settings.messages_update':'Automatic messages changed',
 'event.start':'World event detected', 'event.end':'World event ended', 'item.spawn':'Item spawned',
 'item.observed':'Ground item first observed', 'server.start':'Server started', heartbeat:'Connection heartbeat',
}
export const readable = (s: string) => names[s] || s.replace(/[._]/g,' ').replace(/^./,c=>c.toUpperCase())
/** Colour group for an action, shown as a small dot beside its name. */
export function actionGroup(a: string): string {
 if (a.startsWith('restart.')) return 'restart'
 if (a.startsWith('player.') || a.startsWith('rcon.message') || a === 'rcon.kick') return 'player'
 if (a.startsWith('ban')) return 'ban'
 if (a.startsWith('rcon.') || a.startsWith('server.')) return 'server'
 if (a.startsWith('settings.') || a.startsWith('profile.') || a.startsWith('priority.')) return 'settings'
 return ''
}
export function ActionName({ action }: { action: string }) {
 return <span className="log-action" title={action}><i className={actionGroup(action)} />{readable(action)}</span>
}
const labels:Record<string,string> = { times:'Restart times', timezone:'Timezone', mode:'Restart control', enabled:'Schedule enabled',
 announce:'Countdown announcements', announce_minutes:'Warn at (minutes before)', announce_text:'Warning message',
 window_minutes:'Detection window (± minutes)', timeout_minutes:'Recovery timeout (minutes)', dry_run:'Simulation only',
 lock_minutes:'Lock before restart (minutes)', kick_minutes:'Kick before restart (minutes)', kick_message:'Kick message', kicked:'Players kicked',
 delay_minutes:'Countdown (minutes)', minutes:'Minutes before', reason:'Reason', message:'Message', x:'X (east)', z:'Z (north)', y:'Altitude',
 event_classes:'Event classes', rare_items:'Tracked item classes', in_inventory:'Added to inventory', on_ground:'Placed on ground',
 to_player:'Destination player', item:'Item class', quantity:'Quantity', cmd:'Command', planned_for:'Planned restart', alive:'Player alive',
 has_y:'Altitude given', revision:'Revision', part:'Part', detail:'Tool / detail', health_pct:'Health left (%)', class:'Class', category:'Category', victim:'Victim', killer:'Killer', weapon:'Weapon', distance_m:'Distance (m)', cause:'Cause' }
export function displayValue(key:string,value:unknown):string {
 if (typeof value==='boolean') return value?'Yes':'No'
 if (value==null || value==='') return '—'
 if (key==='mode') return value==='drive'?'DZ Manager requests; provider restarts':'Provider schedule (observation only)'
 if (key==='planned_for') return fmtTime(String(value))
 if (key==='lock_minutes'||key==='kick_minutes') return value===0?'Off':String(value)
 if (key==='x'||key==='z'||key==='distance_m') return typeof value==='number'?value.toFixed(0):String(value)
 if (key==='event_classes'&&typeof value==='object') return `${Object.keys(value as object).length} classes`
 if (Array.isArray(value)) return value.length?value.map(v=>displayValue('',v)).join(', '):'None'
 if (typeof value==='object') return Object.entries(value).map(([k,v])=>`${labels[k]||readable(k)}: ${displayValue(k,v)}`).join('; ')
 return String(value)
}
const hidden = new Set(['server_id','name','key','has_y','revision','by_sub'])
export function LogDetails({ params, detail }: { params?:Record<string,unknown>; detail?:string }) {
 let extra:unknown=detail
 try { extra=detail?JSON.parse(detail):null } catch { /* plain text result */ }
 const entries=Object.entries(params||{}).filter(([k])=>!hidden.has(k))
 const content=<>
  {entries.length>0 && <dl>{entries.map(([k,v])=><div key={k}><dt>{labels[k]||readable(k)}</dt><dd>{displayValue(k,v)}</dd></div>)}</dl>}
  {extra!=null && extra!=='' && <p className="muted">{typeof extra==='object'?displayValue('',extra):String(extra)}</p>}
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
  <button className="small" disabled={page===1||loading} onClick={onLatest}>Latest</button>
  <button className="small" disabled={page===1||loading} onClick={onNewer}>Newer</button>
  <span className="muted small" aria-live="polite">Page {page}</span>
  <button className="small" disabled={!next||loading} onClick={()=>next&&onOlder(next)}>Older</button>
 </nav>
}
