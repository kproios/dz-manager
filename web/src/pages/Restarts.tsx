import { useEffect, useMemo, useState } from 'react'
import { api, Cap, ManualRestart, RestartEvent, Schedule, ServerStatus } from '../api'
import { useApi, useNow } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox, PageHead, Pager, Seg, usePaged } from '../ui'
import { Icon } from '../icons'
import { fmtDuration, fmtTime } from '../util'
import { useNotify } from '../App'
import { Bars } from '../charts'

interface Resp { schedule: Schedule; events: RestartEvent[]; next?: string; now: string; online?: boolean; provider_schedule: Cap; restart_drive: Cap; manual?: ManualRestart; manual_delays?: number[]; skipped?: string }

function SkipButton({ skipped, provider, reload }: { skipped: boolean; provider: boolean; reload: () => void }) {
  const notify = useNotify()
  const [open, setOpen] = useState(false)
  const toggle = async () => {
    try { await api.post('/api/restarts/skip', { skip: !skipped }); notify(skipped ? 'Next restart restored' : 'Next restart skipped'); reload() } catch (e: any) { notify(e.message, 'bad') } finally { setOpen(false) }
  }
  return <>
    <button className="small" onClick={() => skipped ? toggle() : setOpen(true)} title={skipped ? 'Run the next restart after all' : 'Skip only the next planned restart'}><Icon name={skipped ? 'restarts' : 'clock'} />{skipped ? 'Undo skip' : 'Skip next restart'}</button>
    {open && <Confirm title="Skip the next restart?" tone="primary" confirmLabel="Skip it" onClose={() => setOpen(false)} onConfirm={toggle} body={<>
      <p>The next planned restart does not happen: no warnings, lock, kick or shutdown. If its countdown already started, the server is unlocked again and players are told it is cancelled. The one after runs normally, and you can undo this until then.</p>
      {provider && <p className="warn small">Your provider's own schedule still restarts the server: in provider mode this only stops DZ Manager's warnings, lock and kick.</p>}
    </>} />}
  </>
}

const INTERVALS: [number, string][] = [[2, 'Every 2 h'], [3, 'Every 3 h'], [4, 'Every 4 h'], [6, 'Every 6 h'], [8, 'Every 8 h'], [12, 'Every 12 h'], [24, 'Once a day'], [0, 'Custom times']]
const ANNOUNCE_CHOICES = [30, 20, 15, 10, 5, 3, 2, 1]
const LOCK_CHOICES: [number, string][] = [[0, 'Off'], [1, '1 min'], [2, '2 min'], [3, '3 min'], [5, '5 min'], [10, '10 min'], [15, '15 min']]
const KICK_CHOICES: [number, string][] = [[0, 'Off'], [1, '1 min'], [2, '2 min'], [3, '3 min'], [5, '5 min']]
const WINDOW_CHOICES: [number, string][] = [[1, '±1 min'], [2, '±2 min'], [3, '±3 min'], [5, '±5 min'], [10, '±10 min']]
const TIMEOUT_CHOICES: [number, string][] = [[5, '5 min'], [10, '10 min'], [15, '15 min'], [20, '20 min'], [30, '30 min']]
export const TEMPLATES = [
  'Server restart in {m} minute(s).',
  'Server restarts at {time} — {m} min left. Find a safe spot and log out.',
  'Restart in {m} min ({time}). Log out now to keep your gear safe.',
]
const KICK_MESSAGES = ['Server restarting now. Back in a few minutes.', 'Scheduled restart — rejoin in about 3 minutes.', 'Server restart. Thanks for playing!']
const pad = (n: number) => String(n).padStart(2, '0')
const toMin = (t: string) => +t.slice(0, 2) * 60 + +t.slice(3, 5)

function timesFor(interval: number, startH: number, startM: number) {
  const out: string[] = []
  for (let h = startH % interval; h < 24; h += interval) out.push(`${pad(h)}:${pad(startM)}`)
  return out
}
function detectPattern(times: string[]): { interval: number; h: number; m: number } {
  const sorted = [...times].sort()
  for (const [iv] of INTERVALS) {
    if (!iv || !sorted.length) continue
    const m = +sorted[0].slice(3, 5), h = +sorted[0].slice(0, 2)
    if (timesFor(iv, h, m).join() === sorted.join()) return { interval: iv, h: h % iv, m }
  }
  return { interval: 0, h: 0, m: 0 }
}
function zones(): string[] {
  try { return (Intl as any).supportedValuesOf('timeZone') as string[] } catch { return ['UTC', 'Europe/Helsinki', 'Europe/London', 'Europe/Berlin', 'America/New_York'] }
}
export function render(tpl: string, m: number, time: string) { return tpl.replaceAll('{m}', String(m)).replaceAll('{time}', time) }

export default function Restarts() {
  const r = useApi<Resp>('/api/restarts', ['restarts', 'capabilities'], 30000)
  const [sc, setSc] = useState<Schedule | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const now = useNow()
  const notify = useNotify()
  useEffect(() => {
    if (r.data && (!sc || !dirty)) setSc({ ...r.data.schedule, lock_minutes: r.data.schedule.lock_minutes ?? 0, kick_minutes: r.data.schedule.kick_minutes ?? 0, kick_message: r.data.schedule.kick_message || KICK_MESSAGES[0] })
  }, [r.data])
  if (r.error && !r.data) return <><PageHead title="Restarts" /><ErrorBox error={r.error} /></>
  if (!r.data || !sc) return <div className="loading">Loading…</div>
  const edit = (patch: Partial<Schedule>) => { setSc({ ...sc, ...patch }); setDirty(true) }
  const problems = validate(sc)
  const save = async () => {
    setSaving(true)
    try {
      const out = await api.put<Schedule>('/api/restarts/schedule', sc)
      setSc(out); setDirty(false)
      notify('Restart schedule saved')
      r.reload()
    } catch (e: any) {
      notify(e.message, 'bad')
    } finally { setSaving(false) }
  }
  const next = r.data.next ? new Date(r.data.next).getTime() : null
  const manual = r.data.manual
  return (
    <>
      <PageHead title="Restarts" sub={r.data.online == null ? 'Server status unknown' : r.data.online ? 'Server online' : 'Server down'} actions={<ServerControls resp={r.data} reload={r.reload} />} />
      {manual && <CountdownBanner m={manual} now={now} reload={r.reload} />}
      <div className="grid two">
        <Card title="Next planned restart" actions={next ? <SkipButton skipped={!!r.data.skipped && new Date(r.data.skipped).getTime() === next} provider={sc.mode !== 'drive'} reload={r.reload} /> : undefined}>
          {next ? <>
            <div className={'big' + (r.data.skipped && new Date(r.data.skipped).getTime() === next ? ' struck' : '')}>{fmtDuration((next - now) / 1000)}</div>
            {r.data.skipped && new Date(r.data.skipped).getTime() === next && <Badge state="warn">skipped: no warnings, lock, kick or restart</Badge>}
            <div className="muted">{new Date(next).toLocaleString(undefined, { weekday: 'long', hour: '2-digit', minute: '2-digit' })} · {sc.timezone}</div>
          </> : <p className="muted">No schedule enabled.</p>}
          <Timeline sc={sc} />
          <p className="hint">{sc.mode === 'drive' ? sc.dry_run ? 'Simulation only — DZ Manager logs what it would do and sends no restart.' : 'DZ Manager sends a graceful shutdown; the provider starts the server again.' : 'Your provider restarts the server; DZ Manager announces, locks, kicks and checks it came back.'}
            {' '}<Badge state={r.data.provider_schedule.state} title={r.data.provider_schedule.reason} /></p>
        </Card>
        <Card title="Restart history" actions={<span className="muted small">last {Math.min(r.data.events.length, 20)}</span>}>
          <Bars color="var(--series-3)" unit=" min" fmt={(v) => (Math.round(v * 10) / 10).toString()}
            items={r.data.events.filter((e) => e.duration_seconds != null).slice(0, 20).reverse().map((e) => ({ label: e.down_at ? new Date(e.down_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : '', v: (e.duration_seconds || 0) / 60, tip: `${kindLabel(e.kind)} · down ${fmtTime(e.down_at)}` }))} />
          <p className="hint">Downtime per restart, in minutes.</p>
        </Card>
      </div>

      <Card title="Schedule" actions={<label className="check"><input type="checkbox" checked={sc.enabled} onChange={(e) => edit({ enabled: e.target.checked })} /> {sc.mode === 'drive' ? 'Scheduled restarts on' : 'Watch the provider schedule'}</label>}>
        <ScheduleTimes sc={sc} edit={edit} />
        <h4>Who restarts the server</h4>
        <div className="option-list">
          <label className="option"><input type="radio" name="mode" checked={sc.mode !== 'drive'} onChange={() => edit({ mode: 'provider' })} /><div><b>My provider (recommended)</b><span>The provider's own schedule restarts the server at these times. DZ Manager mirrors it, warns players, locks and kicks, and alerts if a restart is missed.</span></div></label>
          <label className="option"><input type="radio" name="mode" checked={sc.mode === 'drive'} onChange={() => edit({ mode: 'drive' })} disabled={r.data.restart_drive.state !== 'ok' && sc.mode !== 'drive'} /><div><b>DZ Manager</b><span>DZ Manager sends a graceful shutdown at these times and the provider relaunches it. {r.data.restart_drive.state !== 'ok' && <span className="warn">Needs verification: {r.data.restart_drive.reason}</span>}</span></div></label>
        </div>
        {sc.mode === 'drive' && <label className="check" style={{ marginTop: 10 }}><input type="checkbox" checked={sc.dry_run} onChange={(e) => edit({ dry_run: e.target.checked })} /> Simulation only (log what would happen, send nothing)</label>}
      </Card>

      <Card title="Before each restart">
        <div className="grid two" style={{ marginBottom: 0 }}>
          <div className="stack">
            <label className="check"><input type="checkbox" checked={sc.announce} onChange={(e) => edit({ announce: e.target.checked })} /> Warn players in chat <span className="muted small">(turn off if the provider already announces)</span></label>
            <div className="field">Warn at (minutes before)
              <div className="chips">{ANNOUNCE_CHOICES.map((m) => {
                const on = sc.announce_minutes.includes(m)
                return <button type="button" key={m} className="chip toggle" aria-pressed={on} disabled={!sc.announce} onClick={() => edit({ announce_minutes: on ? sc.announce_minutes.filter((x) => x !== m) : [...sc.announce_minutes, m].sort((a, b) => b - a) })}>{m}</button>
              })}</div>
            </div>
            <MessagePicker sc={sc} edit={edit} />
          </div>
          <div className="stack">
            <div className="field">Lock the server (no new joins)
              <Seg label="Lock before restart" value={sc.lock_minutes} onChange={(v) => edit({ lock_minutes: v, kick_minutes: v && sc.kick_minutes > v ? v : sc.kick_minutes })} options={LOCK_CHOICES.map(([v, l]) => [v, v ? l + ' before' : l])} />
              {sc.mode === 'drive' && !sc.lock_minutes && <span className="hint">DZ Manager mode still locks 1 minute before its shutdown.</span>}
            </div>
            <div className="field">Kick everyone (so they log out cleanly)
              <Seg label="Kick before restart" value={sc.kick_minutes} onChange={(v) => edit({ kick_minutes: v })} options={KICK_CHOICES.filter(([v]) => !sc.lock_minutes || v <= sc.lock_minutes).map(([v, l]) => [v, v ? l + ' before' : l])} />
              {!sc.lock_minutes && sc.kick_minutes > 0 && <span className="hint warn">Without a lock, kicked players can rejoin before the restart.</span>}
            </div>
            <label className="field">Kick message
              <select value={KICK_MESSAGES.includes(sc.kick_message) ? sc.kick_message : ''} disabled={!sc.kick_minutes} onChange={(e) => edit({ kick_message: e.target.value || sc.kick_message })}>
                {KICK_MESSAGES.map((k) => <option key={k}>{k}</option>)}
                {!KICK_MESSAGES.includes(sc.kick_message) && <option value="">{sc.kick_message}</option>}
              </select>
            </label>
            <details>
              <summary className="small muted">Detection & alerts</summary>
              <div className="stack" style={{ marginTop: 10 }}>
                <div className="field">Count a restart as planned within<Seg label="Detection window" value={sc.window_minutes} onChange={(v) => edit({ window_minutes: v })} options={WINDOW_CHOICES} /></div>
                <div className="field">Alert if the server is not back after<Seg label="Recovery timeout" value={sc.timeout_minutes} onChange={(v) => edit({ timeout_minutes: v })} options={TIMEOUT_CHOICES} /></div>
              </div>
            </details>
          </div>
        </div>
        {problems.length > 0 && <div className="warnings" style={{ marginTop: 12 }}>{problems.map((p) => <div key={p}>{p}</div>)}</div>}
        <div className="row end">
          {dirty && <span className="muted small">Unsaved changes</span>}
          {dirty && <button className="ghost" onClick={() => { setSc(r.data!.schedule); setDirty(false) }}>Discard</button>}
          <button className="primary" disabled={!dirty || saving || problems.length > 0} onClick={save}>{saving ? 'Saving…' : 'Save schedule'}</button>
        </div>
      </Card>
      <History events={r.data.events} />
    </>
  )
}

function validate(sc: Schedule): string[] {
  const out: string[] = []
  if (!sc.times.length) out.push('Add at least one restart time.')
  const m = [...sc.times].map(toMin).sort((a, b) => a - b)
  for (let i = 1; i < m.length; i++) if (m[i] - m[i - 1] < 15) { out.push('Restart times must be at least 15 minutes apart.'); break }
  if (sc.announce && !sc.announce_minutes.length) out.push('Pick at least one warning time, or turn warnings off.')
  if (sc.announce && !sc.announce_text.includes('{m}')) out.push('The warning message must contain {m} (minutes left).')
  if (sc.announce_text.length > 150) out.push('The warning message is longer than 150 characters.')
  return out
}

function ScheduleTimes({ sc, edit }: { sc: Schedule; edit: (p: Partial<Schedule>) => void }) {
  const detected = useMemo(() => detectPattern(sc.times), [sc.times.join()])
  const [iv, setIv] = useState(detected.interval)
  const [add, setAdd] = useState({ h: 6, m: 0 })
  const tz = useMemo(zones, [])
  useEffect(() => { setIv(detected.interval) }, [detected.interval])
  const pick = (v: number) => {
    setIv(v)
    if (v) edit({ times: timesFor(v, Math.min(detected.h, v - 1), detected.m) })
  }
  const startHours = iv ? Array.from({ length: Math.min(iv, 24) }, (_, i) => i) : []
  const sorted = [...sc.times].sort()
  const addTime = `${pad(add.h)}:${pad(add.m)}`
  const tooClose = sorted.some((t) => Math.min(Math.abs(toMin(t) - toMin(addTime)), 1440 - Math.abs(toMin(t) - toMin(addTime))) < 15)
  return (
    <div className="stack">
      <div className="field">Restart pattern<Seg label="Restart pattern" value={iv} onChange={pick} options={INTERVALS} /></div>
      <div className="field-row">
        {iv > 0 && <label className="field">First restart of the day
          <div className="row">
            <select value={detected.h} onChange={(e) => edit({ times: timesFor(iv, +e.target.value, detected.m) })}>{startHours.map((h) => <option key={h} value={h}>{pad(h)}</option>)}</select>
            <span>:</span>
            <select value={detected.m} onChange={(e) => edit({ times: timesFor(iv, detected.h, +e.target.value) })}>{[0, 15, 20, 30, 45].map((m) => <option key={m} value={m}>{pad(m)}</option>)}</select>
          </div>
        </label>}
        <label className="field">Timezone
          <select value={sc.timezone} onChange={(e) => edit({ timezone: e.target.value })}>{(tz.includes(sc.timezone) ? tz : [sc.timezone, ...tz]).map((z) => <option key={z}>{z}</option>)}</select>
        </label>
      </div>
      <div className="field">{sorted.length} restart{sorted.length === 1 ? '' : 's'} a day
        <div className="chips">
          {sorted.map((t) => <span className="chip" key={t}>{t}{iv === 0 && <button type="button" title={`Remove ${t}`} aria-label={`Remove ${t}`} onClick={() => edit({ times: sc.times.filter((x) => x !== t) })}><Icon name="close" /></button>}</span>)}
          {iv === 0 && <span className="row">
            <select value={add.h} onChange={(e) => setAdd({ ...add, h: +e.target.value })} style={{ width: 64 }} aria-label="Hour">{Array.from({ length: 24 }, (_, h) => <option key={h} value={h}>{pad(h)}</option>)}</select>
            <select value={add.m} onChange={(e) => setAdd({ ...add, m: +e.target.value })} style={{ width: 64 }} aria-label="Minute">{Array.from({ length: 12 }, (_, i) => i * 5).map((m) => <option key={m} value={m}>{pad(m)}</option>)}</select>
            <button type="button" className="small" disabled={tooClose || sorted.length >= 24} title={tooClose ? 'Must be at least 15 minutes from another restart' : 'Add restart time'} onClick={() => edit({ times: [...sc.times, addTime] })}><Icon name="plus" />Add</button>
          </span>}
        </div>
      </div>
    </div>
  )
}

function MessagePicker({ sc, edit }: { sc: Schedule; edit: (p: Partial<Schedule>) => void }) {
  const custom = !TEMPLATES.includes(sc.announce_text)
  const example = sc.announce_minutes[sc.announce_minutes.length - 1] ?? 5
  const next = sc.times.length ? [...sc.times].sort()[0] : '12:00'
  return (
    <div className="field">Warning message
      <div className="option-list">
        {TEMPLATES.map((t) => <label className="option" key={t}><input type="radio" name="tpl" disabled={!sc.announce} checked={sc.announce_text === t} onChange={() => edit({ announce_text: t })} /><div><span style={{ color: 'var(--text)' }}>{render(t, 5, next)}</span></div></label>)}
        <label className="option"><input type="radio" name="tpl" disabled={!sc.announce} checked={custom} onChange={() => edit({ announce_text: custom ? sc.announce_text : 'Restart in {m} min.' })} /><div style={{ flex: 1 }}><b>Custom</b>
          {custom && <input value={sc.announce_text} maxLength={150} aria-invalid={!sc.announce_text.includes('{m}')} onChange={(e) => edit({ announce_text: e.target.value })} style={{ marginTop: 6 }} />}
          <span>Use {'{m}'} for minutes left and {'{time}'} for the restart time.</span></div></label>
      </div>
      {sc.announce && <div className="preview">{render(sc.announce_text, example, next)}</div>}
    </div>
  )
}

function Timeline({ sc }: { sc: Schedule }) {
  const steps: [number, string][] = []
  if (sc.announce) for (const m of sc.announce_minutes) steps.push([m, 'Warning'])
  const lock = sc.lock_minutes || (sc.mode === 'drive' ? 1 : 0)
  if (lock) steps.push([lock, 'Lock server'])
  if (sc.kick_minutes) steps.push([sc.kick_minutes, 'Kick everyone'])
  steps.sort((a, b) => b[0] - a[0])
  return (
    <div className="chips" style={{ marginTop: 12 }} aria-label="What happens before each restart">
      {steps.map(([m, l], i) => <span key={i} className="chip" style={{ paddingRight: 10 }}><span className="muted">−{m}m</span> {l}</span>)}
      <span className="chip" style={{ paddingRight: 10 }}><Icon name="restarts" /> Restart</span>
    </div>
  )
}

function CountdownBanner({ m, now, reload }: { m: ManualRestart; now: number; reload: () => void }) {
  const notify = useNotify()
  const left = Math.max(0, (new Date(m.at).getTime() - now) / 1000)
  const cancel = async () => {
    try { await api.post('/api/restarts/cancel'); notify('Restart cancelled; players were told'); reload() } catch (e: any) { notify(e.message, 'bad') }
  }
  return (
    <div className="warnings row wrap" role="status">
      <Icon name="clock" /><b>Restart in {fmtDuration(left)}</b>
      <span className="muted">requested by {m.by || 'an admin'} · at {new Date(m.at).toLocaleTimeString(undefined, { timeStyle: 'short' })}</span>
      <button className="small" style={{ marginLeft: 'auto' }} onClick={cancel}>Cancel restart</button>
    </div>
  )
}

/** Restart / lock / unlock / kick-all controls. Also used on the dashboard. */
export function ServerControls({ resp, reload }: { resp?: Resp | null; reload?: () => void }) {
  const status = useApi<ServerStatus>('/api/server/status', ['server_status'])
  const r = useApi<Resp>(resp ? null : '/api/restarts', ['restarts', 'capabilities'])
  const data = resp ?? r.data
  const notify = useNotify()
  const [open, setOpen] = useState<'restart' | 'kick' | 'lock' | 'unlock' | null>(null)
  const [busy, setBusy] = useState(false)
  const [delay, setDelay] = useState(5)
  const [kickMsg, setKickMsg] = useState(KICK_MESSAGES[0])
  const rconOk = status.data?.rcon.state === 'ok'
  const locked = status.data?.locked
  const canRestart = data?.restart_drive.state === 'ok' && !!data?.online && !data?.manual
  const restartWhy = !data ? 'Loading…' : data.restart_drive.state !== 'ok' ? data.restart_drive.reason : !data.online ? 'Server is not online' : data.manual ? 'A countdown restart is already running' : ''
  const sc = data?.schedule
  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    try { await fn(); notify(ok); setOpen(null); status.reload(); (reload ?? r.reload)() } catch (e: any) { notify(e.message, 'bad') } finally { setBusy(false) }
  }
  const delays = data?.manual_delays ?? [0, 1, 2, 5, 10, 15, 30]
  return (
    <>
      <span className="row">
        <Badge state={locked == null ? 'muted' : locked ? 'warn' : 'ok'} title="RCON cannot read the lock state back; this is the last state DZ Manager set">{locked == null ? 'lock unknown' : locked ? 'locked' : 'open for joins'}</Badge>
        {locked
          ? <button className="small" disabled={!rconOk} title={rconOk ? 'Allow players to join again' : 'Requires RCON'} onClick={() => setOpen('unlock')}><Icon name="unlock" />Unlock</button>
          : <button className="small" disabled={!rconOk} title={rconOk ? 'Stop new players from joining' : 'Requires RCON'} onClick={() => setOpen('lock')}><Icon name="lock" />Lock</button>}
        {locked == null && <button className="small ghost" disabled={!rconOk} onClick={() => setOpen('unlock')} title="Allow players to join"><Icon name="unlock" />Unlock</button>}
        <button className="small danger" disabled={!rconOk} title={rconOk ? 'Kick every player' : 'Requires RCON'} onClick={() => setOpen('kick')}><Icon name="kick" />Kick all</button>
        <button className="small danger" disabled={!canRestart} title={restartWhy || 'Restart the game server'} onClick={() => setOpen('restart')}><Icon name="power" />Restart…</button>
      </span>
      {(open === 'lock' || open === 'unlock') && <Confirm title={open === 'lock' ? 'Lock the server?' : 'Unlock the server?'} tone="primary" confirmLabel={open === 'lock' ? 'Lock' : 'Unlock'} busy={busy} onClose={() => setOpen(null)}
        onConfirm={() => act(() => api.post('/api/rcon/' + open), open === 'lock' ? 'Server locked: no new joins' : 'Server unlocked')}
        body={<p>{open === 'lock' ? 'Players already online stay connected; nobody new can join until you unlock or the server restarts.' : 'Players can join again.'}</p>} />}
      {open === 'kick' && <Confirm title="Kick every player?" word="KICK" confirmLabel="Kick all" busy={busy} onClose={() => setOpen(null)}
        onConfirm={() => act(() => api.post('/api/rcon/kick-all', { reason: kickMsg, confirm: 'KICK' }), 'All players kicked')}
        body={<div className="form">
          <p className="small muted">{status.data?.players?.length ?? 0} player(s) online. They can rejoin unless the server is locked.</p>
          <label className="field">Message<select value={kickMsg} onChange={(e) => setKickMsg(e.target.value)}>{KICK_MESSAGES.map((k) => <option key={k}>{k}</option>)}</select></label>
        </div>} />}
      {open === 'restart' && <Confirm title="Restart the game server" word="RESTART" confirmLabel={delay ? `Restart in ${delay} min` : 'Restart now'} busy={busy} onClose={() => setOpen(null)}
        onConfirm={() => act(() => api.post('/api/restarts/now', { confirm: 'RESTART', delay_minutes: delay }), delay ? `Restart in ${delay} minutes — players are being warned` : 'Restart requested. Waiting for the server to return.')}
        body={<div className="form">
          <div className="field">When<Seg label="Restart delay" value={delay} onChange={setDelay} options={delays.map((d) => [d, d ? `${d} min` : 'Now'])} /></div>
          {delay > 0 && sc ? <ul className="small">
            <li>Players get a warning now{sc.announce_minutes.some((m) => m < delay) ? ` and at ${sc.announce_minutes.filter((m) => m < delay).join(', ')} min` : ''}: <i>{render(sc.announce_text.includes('{m}') ? sc.announce_text : TEMPLATES[0], delay, 'the restart time')}</i></li>
            {sc.lock_minutes > 0 && <li>{sc.lock_minutes >= delay ? 'Server locks right away (no new joins)' : `Server locks ${sc.lock_minutes} min before`}</li>}
            {sc.kick_minutes > 0 && <li>{sc.kick_minutes >= delay ? 'Everyone is kicked right away' : `Everyone is kicked ${sc.kick_minutes} min before`}</li>}
            <li>Then a graceful shutdown; the provider starts it again. You can cancel until then.</li>
          </ul> : <p className="small">Players disconnect immediately. DayZ saves and shuts down gracefully; the provider starts it again.</p>}
        </div>} />}
    </>
  )
}

const kindLabel = (k: string) => ({ planned: 'Scheduled', unplanned: 'Unplanned (crash?)', missed: 'Missed', manual: 'Manual' } as Record<string, string>)[k] || k
const outcome: Record<string, [string, string]> = { ok: ['ok', 'Back online'], down: ['warn', 'Down now'], not_back: ['bad', 'Not back in time'], recovered_late: ['warn', 'Back, but late'], missed: ['bad', 'Did not happen'] }

function History({ events }: { events: RestartEvent[] }) {
  const pg = usePaged(events, 10)
  return (
    <Card title="All restarts">
      {!events.length ? <Empty>No restarts observed yet.</Empty> : <>
        <div className="table-scroll"><table>
          <thead><tr><th>Went down</th><th>Back up</th><th>Planned for</th><th>Type</th><th>Result</th><th className="num">Downtime</th></tr></thead>
          <tbody>
            {pg.rows.map((e) => (
              <tr key={e.id}>
                <td>{fmtTime(e.down_at)}</td><td>{fmtTime(e.up_at)}</td><td>{e.planned_for ? new Date(e.planned_for).toLocaleTimeString(undefined, { timeStyle: 'short' }) : '—'}</td>
                <td>{kindLabel(e.kind)}</td><td><Badge state={outcome[e.outcome]?.[0] || 'muted'}>{outcome[e.outcome]?.[1] || e.outcome.replaceAll('_', ' ')}</Badge></td>
                <td className="num">{fmtDuration(e.duration_seconds)}</td>
              </tr>
            ))}
          </tbody>
        </table></div>
        <Pager {...pg} noun="restarts" />
      </>}
    </Card>
  )
}
