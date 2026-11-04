import { useState } from 'react'
import { api, Cap, MapState, ServerStatus } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, CapList, Confirm, ErrorBox } from '../ui'
import { ago, fmtDuration, fmtTime } from '../util'
import { useNotify } from '../App'

export default function Dashboard() {
  const status = useApi<any>('/api/status', ['profiles'], 60000)
  const srv = useApi<ServerStatus>('/api/server/status', ['server_status'])
  const caps = useApi<{ server_id: string | null; capabilities: Cap[] }>('/api/capabilities', ['capabilities', 'profiles'])
  const map = useApi<MapState>('/api/map/state', ['map'], 30000)
  const s = srv.data
  const st = status.data

  return (
    <>
      <h1>{s?.info?.name || st?.active_server?.name || 'Dashboard'}</h1>
      <ErrorBox error={status.error || srv.error} />
      {st?.warnings?.length > 0 && (
        <div className="warnings">{st.warnings.map((w: string) => <div key={w}>⚠ {w}</div>)}</div>
      )}
      <div className="grid">
        <Card title="Server">
          {!s || !s.server_id ? <p className="muted">No active server. Add one under Settings → Servers.</p> : (
            <dl>
              <dt>Query (A2S)</dt><dd><Badge state={s.query.state} /> <span className="small muted">{s.query.error}</span></dd>
              <dt>RCON</dt><dd><Badge state={s.rcon.state} /> <span className="small muted">{s.rcon.error}</span></dd>
              <dt>Players</dt><dd>{s.info ? `${s.info.players} / ${s.info.max_players}` : '—'} {s.rcon.state === 'ok' && <span className="muted small">(RCON roster: {s.players.length})</span>}</dd>
              <dt>Map / version</dt><dd>{s.info ? `${s.info.map} · ${s.info.version}` : '—'}</dd>
              <dt>Latency</dt><dd>{s.info ? `${s.info.latency_ms} ms` : '—'}</dd>
              <dt>Locked</dt><dd>{s.locked == null ? 'unknown' : s.locked ? 'yes' : 'no'}</dd>
              <dt>Updated</dt><dd>{ago(s.updated_at)}</dd>
            </dl>
          )}
          <p className="small muted">A2S may be cached by the provider; "query ok" does not prove the server is joinable.</p>
        </Card>
        <Card title="Performance (MetricZ)">
          {!map.data?.available ? <p className="muted">{map.data?.reason || 'No data'}</p> : (
            <dl>
              <dt>FPS</dt><dd>{map.data.perf?.fps_avg?.toFixed(1) ?? map.data.perf?.fps?.toFixed(1) ?? '—'} {map.data.perf?.fps_min != null && <span className="muted small">min {map.data.perf.fps_min.toFixed(0)}</span>}</dd>
              <dt>Players</dt><dd>{map.data.perf?.players ?? '—'}</dd>
              <dt>Vehicles</dt><dd>{map.data.perf?.vehicles ?? '—'}</dd>
              <dt>Infected / animals</dt><dd>{map.data.perf?.infected ?? '—'} / {map.data.perf?.animals ?? '—'}</dd>
              <dt>Uptime</dt><dd>{fmtDuration(map.data.perf?.uptime_seconds)}</dd>
              <dt>Data</dt><dd><Badge state={map.data.fresh ? 'ok' : 'stale'}>{map.data.fresh ? 'live' : 'stale'}</Badge> {ago(map.data.received_at)}</dd>
            </dl>
          )}
        </Card>
        <Card title="Capabilities">{caps.data && <CapList caps={caps.data.capabilities} />}</Card>
        <Broadcast disabled={s?.rcon.state !== 'ok'} />
        <Card title="DZ Manager">
          {st && (
            <dl>
              <dt>Version</dt><dd>{st.version}</dd>
              <dt>Started</dt><dd>{fmtTime(st.started_at)}</dd>
              <dt>Timezone</dt><dd>{st.timezone}</dd>
              <dt>Database</dt><dd>schema v{st.schema_version}, {st.db_bytes ? (st.db_bytes / 1048576).toFixed(1) + ' MB' : '—'}</dd>
              <dt>Updater</dt><dd>{st.updater ? <><Badge state={st.updater.last_result === 'ok' ? 'ok' : 'warn'}>{st.updater.last_result}</Badge> checked {ago(st.updater.checked_at)}</> : 'not installed'}</dd>
            </dl>
          )}
        </Card>
      </div>
    </>
  )
}

function Broadcast({ disabled }: { disabled: boolean }) {
  const [msg, setMsg] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const notify = useNotify()
  const send = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ ok: boolean; reply: string }>('/api/rcon/say', { message: msg })
      notify('Broadcast sent' + (r.reply ? `: ${r.reply}` : ''))
      setMsg('')
    } catch (e: any) {
      notify(e.message, 'bad')
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }
  return (
    <Card title="Broadcast (RCON)">
      <textarea maxLength={200} rows={2} value={msg} placeholder="Message to all players" onChange={(e) => setMsg(e.target.value)} disabled={disabled} />
      <div className="row end">
        <button disabled={disabled || !msg.trim()} onClick={() => setConfirm(true)}>Send to all</button>
      </div>
      {disabled && <p className="small muted">Requires RCON (capability rcon = ok).</p>}
      {confirm && <Confirm title="Broadcast to all players?" body={<blockquote>{msg}</blockquote>} busy={busy} onConfirm={send} onClose={() => setConfirm(false)} />}
    </Card>
  )
}
