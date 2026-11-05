import { useMemo } from 'react'
import { Cap, MapState, PerfPoint, ServerStatus } from '../api'
import { useApi, useNow } from '../hooks'
import { Badge, Card, CapList, ErrorBox, Meter, PageHead, Seg, StatTile } from '../ui'
import { ago, fmtDuration, fmtTime } from '../util'
import { LineChart, Pt, Sparkline } from '../charts'
import { usePref } from '../prefs'
import { ServerControls } from './Restarts'
import { Icon } from '../icons'

const RANGES: [number, string][] = [[6, '6 h'], [24, '24 h'], [72, '3 days'], [168, '7 days']]

/** Average minute buckets down to at most n points so long ranges stay light. */
function series(points: PerfPoint[] | null, key: 'players' | 'fps' | 'vehicles', n = 360): Pt[] {
  if (!points?.length) return []
  const raw = points.map((p) => ({ t: new Date(p.minute.length === 16 ? p.minute + ':00Z' : p.minute).getTime(), v: p[key] }))
  if (raw.length <= n) return raw
  const size = Math.ceil(raw.length / n), out: Pt[] = []
  for (let i = 0; i < raw.length; i += size) {
    const chunk = raw.slice(i, i + size), vals = chunk.map((c) => c.v).filter((v): v is number => v != null)
    out.push({ t: chunk[Math.floor(chunk.length / 2)].t, v: vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null })
  }
  return out
}

export default function Dashboard() {
  const status = useApi<any>('/api/status', ['profiles'], 60000)
  const srv = useApi<ServerStatus>('/api/server/status', ['server_status'])
  const caps = useApi<{ server_id: string | null; capabilities: Cap[] }>('/api/capabilities', ['capabilities', 'profiles'])
  const map = useApi<MapState>('/api/map/state', ['map'], 30000)
  const [hours, setHours] = usePref('dashboard.hours', 24)
  const perf = useApi<PerfPoint[]>('/api/perf?hours=' + hours, [], 60000)
  const spark = useApi<PerfPoint[]>('/api/perf?hours=2', [], 60000)
  const restarts = useApi<{ next?: string }>('/api/restarts', ['restarts'], 60000)
  const now = useNow(1000)
  const s = srv.data
  const st = status.data
  const perfNow = map.data?.available ? map.data.perf : undefined
  const online = s?.rcon.state === 'ok' ? s.players?.length ?? 0 : s?.info?.players ?? 0
  const max = s?.info?.max_players
  const pl = useMemo(() => series(perf.data, 'players'), [perf.data])
  const fps = useMemo(() => series(perf.data, 'fps'), [perf.data])
  const sp = spark.data ?? []
  const peak = pl.reduce((m, p) => Math.max(m, p.v ?? 0), 0)
  const fpsVals = fps.map((p) => p.v).filter((v): v is number => v != null)
  const fpsAvg = fpsVals.length ? fpsVals.reduce((a, b) => a + b, 0) / fpsVals.length : null
  const next = restarts.data?.next ? new Date(restarts.data.next).getTime() : null
  const curFps = perfNow?.fps_avg ?? perfNow?.fps
  const fpsColor = curFps == null ? undefined : curFps >= 30 ? 'okc' : curFps >= 15 ? 'warn' : ''

  return (
    <>
      <PageHead title={s?.info?.name || st?.active_server?.name || 'Dashboard'}
        sub={s?.server_id ? <span className="row wrap">
          <Badge state={s.query.state} title={s.query.error || 'Server query (A2S)'}>{s.query.state === 'ok' ? 'query ok' : 'query ' + s.query.state.replaceAll('_', ' ')}</Badge>
          <Badge state={s.rcon.state} title={s.rcon.error || 'BattlEye RCON'}>{s.rcon.state === 'ok' ? 'RCON ok' : 'RCON ' + s.rcon.state.replaceAll('_', ' ')}</Badge>
          {s.info && <span>{s.info.map} · v{s.info.version} · {s.info.latency_ms} ms</span>}
        </span> : undefined}
        actions={s?.server_id ? <ServerControls /> : undefined} />
      <ErrorBox error={status.error || srv.error} />
      {st?.warnings?.length > 0 && <div className="warnings">{st.warnings.map((w: string) => <div key={w}>⚠ {w}</div>)}</div>}
      {s && !s.server_id && <div className="warnings">No active server yet. Add one under <a href="#/settings">Settings → Game servers</a>.</div>}

      <div className="tiles">
        <StatTile label={<><Icon name="players" />Players online</>} value={online} unit={max ? `/ ${max}` : undefined}
          foot={s?.rcon.state === 'ok' ? `RCON roster: ${s.players?.length ?? 0}` : 'From server query'}>
          {max ? <Meter value={online / max} /> : null}
        </StatTile>
        <StatTile label="Server FPS" value={<span className={fpsColor}>{curFps != null ? curFps.toFixed(0) : '—'}</span>} foot={perfNow?.fps_min != null ? `min ${perfNow.fps_min.toFixed(0)} this minute` : 'MetricZ'}>
          <Sparkline values={sp.map((p) => p.fps)} color="var(--series-2)" />
        </StatTile>
        <StatTile label="Uptime" value={fmtDuration(perfNow?.uptime_seconds)} foot={perfNow?.uptime_seconds ? 'since ' + new Date(Date.now() - perfNow.uptime_seconds * 1000).toLocaleTimeString(undefined, { timeStyle: 'short' }) : '—'} />
        <StatTile label={<><Icon name="restarts" />Next restart</>} value={next ? fmtDuration((next - now) / 1000) : '—'} foot={next ? new Date(next).toLocaleTimeString(undefined, { timeStyle: 'short' }) : <a href="#/restarts">Set up a schedule</a>} />
        <StatTile label="Vehicles" value={perfNow?.vehicles ?? map.data?.vehicles?.length ?? '—'} foot="in the world">
          <Sparkline values={sp.map((p) => p.vehicles)} color="var(--series-3)" />
        </StatTile>
        <StatTile label="AI" value={perfNow?.infected ?? '—'} unit="infected" foot={perfNow?.animals != null ? `${perfNow.animals} animals` : '—'} />
      </div>

      <div className="row" style={{ marginBottom: 10 }}>
        <h2>Trends</h2>
        <span className="muted small">{map.data?.available ? <>MetricZ <Badge state={map.data.fresh ? 'ok' : 'stale'}>{map.data.fresh ? 'live' : 'stale'}</Badge> {ago(map.data.received_at)}</> : map.data?.reason || ''}</span>
        <span className="grow" />
        <Seg label="Time range" value={hours} onChange={setHours} options={RANGES} />
      </div>
      <div className="grid two">
        <Card title="Players online" actions={<span className="statline"><span className="small muted">peak <b style={{ color: 'var(--text)' }}>{Math.round(peak)}</b></span></span>}>
          <LineChart data={pl} color="var(--series-1)" label="Players" />
        </Card>
        <Card title="Server FPS" actions={<span className="small muted">average <b style={{ color: 'var(--text)' }}>{fpsAvg != null ? fpsAvg.toFixed(0) : '—'}</b></span>}>
          <LineChart data={fps} color="var(--series-2)" label="FPS" />
        </Card>
      </div>

      <div className="grid">
        <Card title="Capabilities">{caps.data && <CapList caps={caps.data.capabilities} />}</Card>
        <Card title="DZ Manager">
          {st && (
            <dl>
              <dt>Version</dt><dd>{st.version}</dd>
              <dt>Running since</dt><dd title={fmtTime(st.started_at)}>{ago(st.started_at).replace(' ago', '')}</dd>
              <dt>Timezone</dt><dd>{st.timezone}</dd>
              <dt>Database</dt><dd>schema v{st.schema_version} · {st.db_bytes ? (st.db_bytes / 1048576).toFixed(1) + ' MB' : '—'}</dd>
              <dt>Updates</dt><dd>{st.updater ? <><Badge state={st.updater.last_result === 'ok' ? 'ok' : 'warn'}>{st.updater.last_result}</Badge> <span className="muted small">checked {ago(st.updater.checked_at)}</span></> : 'not installed'}</dd>
            </dl>
          )}
        </Card>
      </div>
    </>
  )
}
