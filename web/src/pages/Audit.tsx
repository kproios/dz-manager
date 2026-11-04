import { useState } from 'react'
import { AuditEntry } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, PageHead, Trunc } from '../ui'
import { ago, fmtTime } from '../util'
import { ActionName, LogDetails, LogPager, Page, readable, useLogQuery } from '../logs'

const RESULTS: Record<string, [string, string]> = { ok: ['ok', 'Completed'], error: ['bad', 'Failed'], denied: ['bad', 'Blocked'] }

export function Target({ a }: { a: Pick<AuditEntry, 'target' | 'params'> }) {
  const name = (a.params && typeof a.params === 'object' && typeof a.params.name === 'string') ? a.params.name : ''
  if (/^\d{17}$/.test(a.target)) return <a href={'#/players/' + a.target} title={a.target}>{name || a.target}</a>
  if (a.target === 'all') return <span>Everyone</span>
  if (a.target === 'server' || /^[0-9a-f-]{36}$/.test(a.target)) return <span className="muted" title={a.target}>Server</span>
  return name ? <span title={a.target}>{name}</span> : <Trunc text={a.target} max={160} />
}

export default function AuditPage() {
  const q = useLogQuery(), [result, setResult] = useState('')
  const log = useApi<Page<AuditEntry>>(`/api/audit/search?limit=25&before=${q.before}&q=${encodeURIComponent(q.search)}&result=${result}`, q.before ? [] : ['events', 'profiles', 'restarts'])
  return <>
    <PageHead title="Audit log" sub="Every admin and automatic action, newest first" />
    <Card title="Administration history">
      <form className="row wrap log-filters" onSubmit={(e) => { e.preventDefault(); q.submit() }}>
        <label className="grow">Search actions, people or details<input type="search" value={q.input} onChange={(e) => q.setInput(e.target.value)} /></label>
        <label>Result<select value={result} onChange={(e) => { setResult(e.target.value); q.reset() }}><option value="">All results</option><option value="ok">Completed</option><option value="error">Failed</option><option value="denied">Blocked</option></select></label>
        <button type="submit">Search</button>
      </form>
      {log.error && <ErrorBox error={log.error} />}
      {!log.data ? <p role="status" className="muted">Loading audit history…</p> : !log.data.items.length ? <Empty>No actions match these filters.</Empty> : <div className="table-scroll"><table>
        <thead><tr><th>When</th><th>Who</th><th>Action</th><th>Target</th><th>Result</th><th>Details</th></tr></thead>
        <tbody>{log.data.items.map((a) => <tr key={a.id}>
          <td className="nowrap" title={fmtTime(a.ts)}>{ago(a.ts)}</td>
          <td>{a.actor_name === 'system' ? <span className="muted">Automatic</span> : a.actor_name}</td>
          <td><ActionName action={a.action} /></td>
          <td><Target a={a} /></td>
          <td><Badge state={RESULTS[a.result]?.[0] || a.result}>{RESULTS[a.result]?.[1] || readable(a.result)}</Badge></td>
          <td><LogDetails params={a.params} detail={a.detail} /></td>
        </tr>)}</tbody></table></div>}
      <LogPager page={q.cursors.length} next={log.data?.next_before} loading={log.loading} onNewer={q.newer} onOlder={q.older} onLatest={q.reset} />
    </Card>
  </>
}
