import { useEffect, useState } from 'react'
import { api, Cap, RestartEvent, Schedule } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox } from '../ui'
import { fmtDuration, fmtTime } from '../util'
import { useNotify } from '../App'

interface Resp { schedule: Schedule; events: RestartEvent[]; next?: string; now: string; online?: boolean; provider_schedule: Cap; restart_drive: Cap }

export default function Restarts() {
  const r = useApi<Resp>('/api/restarts', ['restarts', 'capabilities'], 30000)
  const [sc, setSc] = useState<Schedule | null>(null)
  const [times, setTimes] = useState('')
  const [now, setNow] = useState(Date.now())
  const notify = useNotify()
  const [restartOpen,setRestartOpen]=useState(false)
  const [restarting,setRestarting]=useState(false)
  const restart=async()=>{
    setRestarting(true)
    try { await api.post('/api/restarts/now',{confirm:'RESTART'});notify('Restart requested. Waiting for the server to return.');setRestartOpen(false);r.reload() }
    catch(e:any){notify(e.message,'bad')}finally{setRestarting(false)}
  }
  useEffect(() => {
    if (r.data && !sc) { setSc(r.data.schedule); setTimes(r.data.schedule.times.join(', ')) }
  }, [r.data, sc])
  useEffect(() => { const t = window.setInterval(() => setNow(Date.now()), 1000); return () => window.clearInterval(t) }, [])
  if (r.error) return <><h1>Restarts</h1><ErrorBox error={r.error} /></>
  if (!r.data || !sc) return <div className="loading">Loading…</div>
  const save = async () => {
    try {
      const out = await api.put<Schedule>('/api/restarts/schedule', { ...sc, times: times.split(/[ ,]+/).filter(Boolean) })
      setSc(out); setTimes(out.times.join(', '))
      notify('Schedule saved')
      r.reload()
    } catch (e: any) {
      notify(e.message, 'bad')
    }
  }
  const next = r.data.next ? new Date(r.data.next).getTime() : null
  return (
    <>
      <h1>Restarts</h1>
      <div className="row wrap">
        <button className="danger" disabled={r.data.restart_drive.state!=='ok'||!r.data.online||restarting} onClick={()=>setRestartOpen(true)}>Restart server…</button>
        {r.data.restart_drive.state!=='ok'&&<span className="muted">{r.data.restart_drive.reason}</span>}
      </div>
      {restartOpen&&<Confirm title="Restart the game server" word="RESTART" busy={restarting} onClose={()=>setRestartOpen(false)} onConfirm={restart} body={<p>Players will disconnect. DayZ saves and shuts down gracefully; the verified provider starts it again. Recovery is shown in the history below.</p>}/>}
      <div className="grid">
        <Card title="Next planned restart">
          {next ? <><div className="big">{fmtDuration((next - now) / 1000)}</div><div>{new Date(next).toLocaleString()}</div></> : <p className="muted">No schedule enabled.</p>}
          <p>Server: {r.data.online == null ? 'unknown' : r.data.online ? <Badge state="ok">online</Badge> : <Badge state="bad">down</Badge>}</p>
          <p className="small"><b>Scheduled restarts:</b> {sc.mode === 'drive' ? sc.dry_run ? 'Simulation only — no restart commands are sent' : 'DZ Manager requests shutdown; provider relaunches' : 'Provider schedule; DZ Manager observes'} · <Badge state={r.data.provider_schedule.state} /> <span className="muted">{r.data.provider_schedule.reason}</span></p>
        </Card>
        <Card title="Restart schedule">
          <div className="form">
            <label><input type="checkbox" checked={sc.enabled} onChange={(e) => setSc({ ...sc, enabled: e.target.checked })} /> {sc.mode==='drive'?'Enable scheduled restarts':'Observe the provider schedule'}</label>
            <label>Times (HH:MM, local) <input value={times} onChange={(e) => setTimes(e.target.value)} /></label>
            <label>Timezone <input value={sc.timezone} onChange={(e) => setSc({ ...sc, timezone: e.target.value })} /></label>
            <label>Detection window (± minutes) <input type="number" value={sc.window_minutes} onChange={(e) => setSc({ ...sc, window_minutes: +e.target.value })} /></label>
            <label>Alert if not back after (minutes) <input type="number" value={sc.timeout_minutes} onChange={(e) => setSc({ ...sc, timeout_minutes: +e.target.value })} /></label>
            <label><input type="checkbox" checked={sc.announce} onChange={(e) => setSc({ ...sc, announce: e.target.checked })} /> Extra RCON countdown announcements (off if the provider already announces)</label>
            <label>Announce at (minutes before) <input value={sc.announce_minutes.join(', ')} onChange={(e) => setSc({ ...sc, announce_minutes: e.target.value.split(/[ ,]+/).filter(Boolean).map(Number) })} /></label>
            <label>Announcement text <input value={sc.announce_text} onChange={(e) => setSc({ ...sc, announce_text: e.target.value })} /></label>
            <label>Mode
              <select value={sc.mode} onChange={(e) => setSc({ ...sc, mode: e.target.value })}>
                <option value="provider">provider is authority (recommended)</option>
                <option value="drive">DZ Manager drives (needs verification)</option>
              </select>
            </label>
            {sc.mode === 'drive' && <>
              <label><input type="checkbox" checked={sc.dry_run} onChange={(e) => setSc({ ...sc, dry_run: e.target.checked })} /> Dry-run (log only)</label>
              <p className="small warn">restart.drive: <Badge state={r.data.restart_drive.state} /> {r.data.restart_drive.reason}. Commands are only sent after owner verification and with dry-run off.</p>
            </>}
          </div>
          <div className="row end"><button onClick={save}>Save</button></div>
        </Card>
      </div>
      <Card title="History">
        {!r.data.events.length ? <Empty>No restarts observed yet.</Empty> : (
          <table>
            <thead><tr><th>Down</th><th>Up</th><th>Planned for</th><th>Kind</th><th>Outcome</th><th>Downtime</th></tr></thead>
            <tbody>
              {r.data.events.map((e) => (
                <tr key={e.id}>
                  <td>{fmtTime(e.down_at)}</td><td>{fmtTime(e.up_at)}</td><td>{fmtTime(e.planned_for)}</td>
                  <td>{e.kind}</td><td><Badge state={e.outcome === 'ok' ? 'ok' : e.outcome === 'down' ? 'warn' : 'bad'}>{e.outcome.replace('_', ' ')}</Badge></td>
                  <td>{fmtDuration(e.duration_seconds)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}
