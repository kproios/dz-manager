import { AuditEntry } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty } from '../ui'
import { fmtTime } from '../util'

export default function AuditPage() {
  const log = useApi<AuditEntry[]>('/api/audit?limit=300', ['events', 'profiles', 'restarts'])
  return (
    <>
      <h1>Audit log</h1>
      <Card title="Append-only record of every admin and automatic action">
        {!log.data?.length ? <Empty>Nothing yet.</Empty> : (
          <table>
            <thead><tr><th>Time</th><th>Actor</th><th>Action</th><th>Target</th><th>Result</th><th>Params / detail</th></tr></thead>
            <tbody>
              {log.data.map((a) => (
                <tr key={a.id}>
                  <td className="nowrap">{fmtTime(a.ts)}</td><td>{a.actor_name}</td><td><code>{a.action}</code></td>
                  <td className="small">{a.target}</td><td><Badge state={a.result} /></td>
                  <td className="small"><code>{JSON.stringify(a.params)}</code> {a.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}
