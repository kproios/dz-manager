import { Marker } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty } from '../ui'
import { fmtTime, grid } from '../util'

export default function Events() {
  const markers = useApi<Marker[]>('/api/map/events', ['map_events'], 60000)
  const raw = useApi<any[]>('/api/events/raw?limit=100', ['events'])
  return (
    <>
      <h1>Events</h1>
      <Card title="World events & rare-item spawns">
        {!markers.data?.length ? <Empty>No active or recent events. Events come from @DZMBridge hooks; categories without a verified hook are not shown.</Empty> : (
          <table>
            <thead><tr><th>Category</th><th>Class</th><th>Status</th><th>Started / spawned</th><th>Ended</th><th>Grid</th></tr></thead>
            <tbody>
              {markers.data.map((m) => (
                <tr key={m.id}>
                  <td>{m.category.replace('_', ' ')}</td><td>{m.class || '—'}</td><td><Badge state={m.status} /></td>
                  <td>{fmtTime(m.started_at)}</td><td>{fmtTime(m.ended_at)}</td><td>{grid(m.x, m.z)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card title="Raw bridge events (latest 100)">
        {!raw.data?.length ? <Empty>No bridge events received.</Empty> : (
          <table>
            <thead><tr><th>Time</th><th>Type</th><th>Actor</th><th>Data</th></tr></thead>
            <tbody>
              {raw.data.map((e) => (
                <tr key={e.id}>
                  <td>{fmtTime(e.ts)}</td><td><code>{e.type}</code> {!e.known && <Badge state="warn">unhandled</Badge>}</td>
                  <td>{e.actor ? <a href={'#/players/' + e.actor}>{e.actor}</a> : '—'}</td>
                  <td><code className="small">{JSON.stringify(e.data)}</code></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}
