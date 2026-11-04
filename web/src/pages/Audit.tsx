import { useState } from 'react'
import { AuditEntry } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox } from '../ui'
import { fmtTime } from '../util'
import { LogDetails, LogPager, Page, readable, useLogQuery } from '../logs'

export default function AuditPage() {
 const q=useLogQuery(),[result,setResult]=useState('')
 const log=useApi<Page<AuditEntry>>(`/api/audit/search?limit=25&before=${q.before}&q=${encodeURIComponent(q.search)}&result=${result}`,q.before?[]:['events','profiles','restarts'])
 return <><h1>Audit log</h1><Card title="Administration history">
  <form className="row wrap log-filters" onSubmit={e=>{e.preventDefault();q.submit()}}>
   <label className="grow">Search actions, people or details<input type="search" value={q.input} onChange={e=>q.setInput(e.target.value)} /></label>
   <label>Result<select value={result} onChange={e=>{setResult(e.target.value);q.reset()}}><option value="">All results</option><option value="ok">Completed</option><option value="error">Failed</option><option value="denied">Blocked</option></select></label>
   <button type="submit">Search</button>
  </form>
  {log.error && <ErrorBox error={log.error}/>}
  {!log.data?<p role="status">Loading audit history…</p>:!log.data.items.length?<Empty>No actions match these filters.</Empty>:<div className="table-scroll"><table>
   <thead><tr><th>Time</th><th>Who</th><th>Action</th><th>Target</th><th>Result</th><th>Details</th></tr></thead>
   <tbody>{log.data.items.map(a=><tr key={a.id}>
    <td className="nowrap">{fmtTime(a.ts)}</td><td>{a.actor_name==='system'?'Automatic':a.actor_name}</td>
    <td>{readable(a.action)}</td><td>{a.target||'—'}</td><td><Badge state={a.result}>{a.result==='ok'?'Completed':a.result==='error'?'Failed':a.result==='denied'?'Blocked':readable(a.result)}</Badge></td>
    <td><LogDetails params={a.params} detail={a.detail}/></td>
   </tr>)}</tbody></table></div>}
  <LogPager page={q.cursors.length} next={log.data?.next_before} loading={log.loading} onNewer={q.newer} onOlder={q.older} onLatest={q.reset}/>
 </Card></>
}
