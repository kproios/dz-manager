import { useState } from 'react'
import { ActionAvail, api, MapState, Marker, MzVehicle, Permission } from './api'
import { useCan } from './auth'
import { useApi } from './hooks'
import { useNotify } from './App'
import { usePref } from './prefs'
import { Confirm } from './ui'
import { Icon, IconName } from './icons'
import { grid, WORLD } from './util'
import { iconLabels, markerIcon, markerTitle } from './mapIcons'

export interface Target { steam64: string; name: string; be_guid?: string }
export interface Dest { label: string; x: number; z: number; group: string }

const reClass = /^[A-Za-z][A-Za-z0-9_]{1,63}$/
const KICK_REASONS = ['Server restart — please rejoin in a few minutes', 'Admin check — you may rejoin', 'Rule violation', 'Unstable connection / desync']
const MESSAGES = ['An admin is checking on you, please stand by.', 'Please read the server rules (Discord).', 'You are about to be teleported, stay still.']
const QTY = [1, 2, 3, 5, 10, 20]

/** Runs a mod-backed action and reports the in-game result. */
export function useRunAction() {
  const notify = useNotify()
  return async (t: Target, action: string, label: string, body: Record<string, unknown>) => {
    try {
      await api.post<{ ok: boolean; result: any }>(`/api/players/${encodeURIComponent(t.steam64)}/actions/${action}`, body)
      notify(`${label}: done for ${t.name || t.steam64}`)
      return true
    } catch (e: any) {
      notify(`${label} failed: ${e.message}`, 'bad')
      return false
    }
  }
}

/** Offset a destination a few metres so the player lands beside, not inside, a wreck or crate. */
export function besides(m: Pick<Marker, 'x' | 'z'>): { x: number; z: number } {
  const clamp = (v: number) => Math.max(1, Math.min(WORLD - 1, v))
  return { x: clamp(m.x + 4), z: clamp(m.z + 4) }
}

const when = (iso?: string) => iso ? new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }) : ''

/** Teleport destinations: what is on the map now, then where people died, then past events. */
export const DEST_GROUPS = ['Active events (lands a few metres beside it)', 'Recent deaths (where the body fell)', 'Past events (where it last was)']
export function eventDests(markers: Marker[] | null | undefined): Dest[] {
  const newestFirst = (a: Marker, b: Marker) => b.started_at.localeCompare(a.started_at)
  const all = (markers ?? []).filter((m) => m.x != null && m.z != null)
  const active = all.filter((m) => m.status === 'active')
    .map((m) => ({ label: `${iconLabels[markerIcon(m)]}: ${markerTitle(m)} · ${grid(m.x, m.z)}`, group: DEST_GROUPS[0], ...besides(m) }))
    .sort((a, b) => a.label.localeCompare(b.label))
  const past = all.filter((m) => m.status !== 'active').sort(newestFirst)
  const deaths = past.filter((m) => m.category === 'death').slice(0, 30)
    .map((m) => ({ label: `${markerTitle(m)} · ${when(m.started_at)} · ${grid(m.x, m.z)}`, group: DEST_GROUPS[1], ...besides(m) }))
  const ended = past.filter((m) => m.category !== 'death').slice(0, 30)
    .map((m) => ({ label: `${iconLabels[markerIcon(m)]}: ${markerTitle(m)} · ${when(m.ended_at || m.started_at)} · ${grid(m.x, m.z)}`, group: DEST_GROUPS[2], ...besides(m) }))
  return [...active, ...deaths, ...ended]
}

/** Who to teleport: remembers the last choice, which is usually the admin themself. */
export function TpHere({ targets, onGo, ended, death }: { targets: Target[]; onGo: (t: Target) => void; ended?: boolean; death?: boolean }) {
  const [who, setWho] = usePref('map.tpPlayer', '')
  if (!targets.length) return <p className="hint">No players online to teleport here.</p>
  const t = targets.find((x) => x.steam64 === who)
  return (
    <div className="qa-section">
      <h4>Teleport a player here</h4>
      <div className="row">
        <select value={t ? who : ''} onChange={(e) => setWho(e.target.value)} aria-label="Player to teleport" style={{ flex: 1, minWidth: 0 }}>
          <option value="">Choose player…</option>
          {targets.map((p) => <option key={p.steam64} value={p.steam64}>{p.name}</option>)}
        </select>
        <button className="primary small" disabled={!t} onClick={() => t && onGo(t)}><Icon name="teleport" />Go</button>
      </div>
      <p className="hint">{death ? 'Lands beside where they died. The body may be gone by now.' : ended ? 'This has ended: lands a few metres from where it last was.' : 'Lands a few metres beside it so you can check the loot.'}</p>
    </div>
  )
}

/** Teleport to a stored position from a page without a map (event log, player history). */
export function TeleportButton({ x, z, label, death }: { x: number; z: number; label: string; death?: boolean }) {
  const [open, setOpen] = useState(false)
  if (!useCan()('actions.safe')) return null
  return <>
    <button className="small ghost" title={`Teleport a player to ${label}`} aria-label={`Teleport a player to ${label}`} onClick={() => setOpen(true)}><Icon name="teleport" /></button>
    {open && <TeleportDialog x={x} z={z} label={label} death={death} onClose={() => setOpen(false)} />}
  </>
}

function TeleportDialog({ x, z, label, death, onClose }: { x: number; z: number; label: string; death?: boolean; onClose: () => void }) {
  const state = useApi<MapState>('/api/map/state', ['map'])
  const run = useRunAction()
  const [who, setWho] = usePref('map.tpPlayer', '')
  const [busy, setBusy] = useState(false)
  const targets: Target[] = (state.data?.players ?? []).map((p) => ({ steam64: p.steam64, name: p.name || p.steam64 }))
  const t = targets.find((p) => p.steam64 === who)
  const go = async () => {
    if (!t) return
    setBusy(true)
    const ok = await run(t, 'teleport', 'Teleport', besides({ x, z }))
    setBusy(false)
    if (ok) onClose()
  }
  return <Confirm title={`Teleport to ${label}`} tone="primary" confirmLabel="Teleport" busy={busy} disabled={!t} onClose={onClose} onConfirm={go} body={
    <div className="form">
      {state.data && !targets.length ? <p className="muted">No players online to teleport.</p> : <label className="field">Player
        <select value={t ? who : ''} autoFocus onChange={(e) => setWho(e.target.value)}>
          <option value="">Choose player…</option>
          {targets.map((p) => <option key={p.steam64} value={p.steam64}>{p.name}</option>)}
        </select>
      </label>}
      <span className="hint">{grid(x, z)} · {x.toFixed(0)} / {z.toFixed(0)}. {death ? 'Lands beside where they died. The body may be gone by now.' : 'Lands a few metres beside this spot, on the ground.'}</span>
    </div>} />
}

const VEHICLE_ICONS: Record<string, IconName> = { vehicle_refuel: 'fuel', vehicle_repair: 'wrench', vehicle_flip: 'flip', vehicle_stop_engine: 'power', vehicle_delete: 'trash' }

/** Refuel, repair, flip, stop the engine of, or delete one vehicle (needs @DZMBridge). */
export function VehicleActions({ vehicle }: { vehicle: MzVehicle }) {
  const acts = useApi<ActionAvail[]>('/api/vehicles/actions', ['capabilities'])
  const notify = useNotify()
  const [mode, setMode] = useState<ActionAvail | null>(null)
  const [busy, setBusy] = useState(false)
  // MetricZ ids are hashes; only the bridge's network ids ("lo:hi") can be targeted.
  const bridgeId = /^-?\d+:-?\d+$/.test(vehicle.id)
  const run = async (a: ActionAvail, typed?: string) => {
    setBusy(true)
    try {
      await api.post(`/api/vehicles/${encodeURIComponent(vehicle.id)}/actions/${a.name}`, { confirm: typed || undefined })
      notify(`${a.label}: done for ${vehicle.class}`)
      setMode(null)
    } catch (e: any) {
      notify(`${a.label} failed: ${e.message}`, 'bad')
    } finally { setBusy(false) }
  }
  const can = useCan()
  const list = (acts.data ?? []).filter((a) => can(a.destructive ? 'actions.strong' : 'actions.safe'))
  if (acts.data && !list.length) return null
  return (
    <div className="qa-section">
      <h4>Vehicle</h4>
      <div className="qa">
        {list.map((a) => {
          const why = !bridgeId ? 'Vehicle actions need live positions from @DZMBridge (this vehicle comes from MetricZ)' : !a.available ? a.reason : a.help
          const quick = !a.destructive && a.name !== 'vehicle_repair'
          return <button key={a.name} className={a.destructive ? 'danger' : ''} disabled={!bridgeId || !a.available || busy} title={why || a.label}
            onClick={() => quick ? run(a) : setMode(a)}><Icon name={VEHICLE_ICONS[a.name] || 'check'} />{a.label}</button>
        })}
      </div>
      {mode && <Confirm title={`${mode.label}: ${vehicle.class}`} word={mode.confirm} tone={mode.destructive ? 'danger' : 'primary'}
        confirmLabel={mode.label} busy={busy} onClose={() => setMode(null)} onConfirm={(typed: string) => run(mode, typed)} body={
          <p className="small muted">{mode.help}{mode.destructive ? ' This cannot be undone.' : ''}</p>} />}
    </div>
  )
}

type Mode = 'message' | 'kick' | 'teleport' | 'give_item' | 'heal' | 'kill' | 'strip' | null

// The role permission each button needs (the server checks it too).
const ACTION_PERM: Record<Exclude<Mode, null>, Permission> = {
  message: 'players.moderate', kick: 'players.moderate', heal: 'actions.safe', teleport: 'actions.safe',
  give_item: 'actions.strong', kill: 'actions.strong', strip: 'actions.strong',
}

export function PlayerActions({ target, layout = 'row', dests = [], players = [], onPick, only }: {
  target: Target; layout?: 'row' | 'grid'; dests?: Dest[]; players?: Target[]; onPick?: () => void; only?: Exclude<Mode, null>[]
}) {
  const acts = useApi<ActionAvail[]>('/api/actions', ['capabilities'])
  const run = useRunAction()
  const notify = useNotify()
  const can = useCan()
  const [mode, setMode] = useState<Mode>(null)
  const [busy, setBusy] = useState(false)
  const [text, setText] = useState('')
  const [dest, setDest] = useState('')
  const [xz, setXz] = useState({ x: '', z: '' })
  const [item, setItem] = useState({ cls: '', qty: 1 })
  const av = (n: string) => acts.data?.find((a) => a.name === n)
  const close = () => { setMode(null); setText(''); setDest(''); setBusy(false) }

  const btn = (m: Exclude<Mode, null>, icon: IconName, label: string, opts: { danger?: boolean; disabled?: boolean; why?: string } = {}) => {
    if ((only && !only.includes(m)) || !can(ACTION_PERM[m])) return null
    const a = av(m)
    const rcon = m === 'message' || m === 'kick'
    const noSteam = !rcon && !target.steam64
    const disabled = opts.disabled || noSteam || (!rcon && !a?.available)
    const why = opts.why || (noSteam ? 'Steam64 not linked to this RCON player yet' : !rcon && !a?.available ? a?.reason || 'Checking bridge…' : undefined)
    return (
      <button key={m} className={(layout === 'row' ? 'small ' : '') + (opts.danger ? 'danger' : '')} disabled={disabled} title={why || label} onClick={() => setMode(m)}>
        <Icon name={icon} />{label}
      </button>
    )
  }
  const noGuid = !target.be_guid
  const rconWhy = noGuid ? 'BattlEye GUID not known yet for this player' : undefined

  const doRcon = async () => {
    setBusy(true)
    try {
      if (mode === 'message') await api.post('/api/rcon/say', { be_guid: target.be_guid, message: text })
      else await api.post('/api/rcon/kick', { be_guid: target.be_guid, reason: text })
      notify(mode === 'message' ? 'Message sent' : `${target.name} kicked`)
      close()
    } catch (e: any) { notify(e.message, 'bad'); setBusy(false) }
  }
  const doMod = async (typed: string) => {
    if (!mode) return
    const a = av(mode)
    const body: Record<string, unknown> = { confirm: typed || undefined }
    if (mode === 'teleport') {
      if (dest.startsWith('p:')) body.to_player = dest.slice(2)
      else if (dest.startsWith('e:')) { const d = dests[+dest.slice(2)]; body.x = d.x; body.z = d.z }
      else { body.x = parseFloat(xz.x); body.z = parseFloat(xz.z) }
    }
    if (mode === 'give_item') { body.class = item.cls.trim(); body.quantity = item.qty }
    setBusy(true)
    const ok = await run(target, mode, a?.label || mode, body)
    if (ok) close(); else setBusy(false)
  }

  const coordOk = (v: string) => v !== '' && isFinite(+v) && +v >= 0 && +v <= WORLD
  const tpReady = dest.startsWith('p:') || dest.startsWith('e:') || (dest === 'xz' && coordOk(xz.x) && coordOk(xz.z))

  return (
    <>
      <div className={layout === 'grid' ? 'qa' : 'btn-group'}>
        {btn('message', 'message', 'Message', { disabled: noGuid, why: rconWhy })}
        {btn('heal', 'heal', 'Heal')}
        {btn('teleport', 'teleport', 'Teleport')}
        {btn('give_item', 'gift', 'Give item')}
        {btn('kick', 'kick', 'Kick', { danger: true, disabled: noGuid, why: rconWhy })}
        {btn('kill', 'skull', 'Kill', { danger: true })}
        {layout === 'row' && btn('strip', 'strip', 'Strip', { danger: true })}
      </div>
      {onPick && <p className="hint">Tip: drag the green player dot on the map to teleport to that exact spot.</p>}

      {(mode === 'message' || mode === 'kick') && (
        <Confirm title={mode === 'message' ? `Message ${target.name}` : `Kick ${target.name}?`} tone={mode === 'message' ? 'primary' : 'danger'}
          confirmLabel={mode === 'message' ? 'Send' : 'Kick'} busy={busy} disabled={!text.trim()} onClose={close} onConfirm={doRcon} body={
            <div className="form">
              <label className="field">{mode === 'message' ? 'Quick messages' : 'Reason (shown to the player)'}
                <select value="" onChange={(e) => e.target.value && setText(e.target.value)}>
                  <option value="">Choose a preset…</option>
                  {(mode === 'message' ? MESSAGES : KICK_REASONS).map((m) => <option key={m}>{m}</option>)}
                </select>
              </label>
              <input autoFocus maxLength={mode === 'message' ? 200 : 120} placeholder={mode === 'message' ? 'Or type a message' : 'Or type a reason'} value={text} onChange={(e) => setText(e.target.value)} />
              <span className="hint">{text.length}/{mode === 'message' ? 200 : 120} · resolved against a fresh RCON roster when sent.</span>
            </div>} />
      )}

      {mode === 'teleport' && (
        <Confirm title={`Teleport ${target.name}`} tone="primary" confirmLabel="Teleport" busy={busy} disabled={!tpReady} onClose={close} onConfirm={doMod} body={
          <div className="form">
            <label className="field">Destination
              <select value={dest} onChange={(e) => setDest(e.target.value)} autoFocus>
                <option value="">Choose…</option>
                {DEST_GROUPS.map((g) => dests.some((d) => d.group === g) && <optgroup key={g} label={g}>{dests.map((d, i) => d.group === g && <option key={i} value={'e:' + i}>{d.label}</option>)}</optgroup>)}
                {players.filter((p) => p.steam64 !== target.steam64).length > 0 && <optgroup label="To another player">
                  {players.filter((p) => p.steam64 !== target.steam64).map((p) => <option key={p.steam64} value={'p:' + p.steam64}>{p.name || p.steam64}</option>)}
                </optgroup>}
                <option value="xz">Exact coordinates…</option>
              </select>
            </label>
            {dest === 'xz' && <div className="field-row">
              <label className="field">X (east, 0–{WORLD})<input inputMode="decimal" value={xz.x} aria-invalid={xz.x !== '' && !coordOk(xz.x)} onChange={(e) => setXz({ ...xz, x: e.target.value })} /></label>
              <label className="field">Z (north, 0–{WORLD})<input inputMode="decimal" value={xz.z} aria-invalid={xz.z !== '' && !coordOk(xz.z)} onChange={(e) => setXz({ ...xz, z: e.target.value })} /></label>
            </div>}
            {onPick && <button type="button" className="small" onClick={() => { close(); onPick() }}><Icon name="target" />Pick a point on the map instead</button>}
            <span className="hint">The player is placed on the ground at the destination. Players in vehicles can't be teleported.</span>
          </div>} />
      )}

      {mode === 'give_item' && (
        <Confirm title={`Give an item to ${target.name}`} tone="primary" confirmLabel="Give" busy={busy} disabled={!reClass.test(item.cls.trim())} onClose={close} onConfirm={doMod} body={
          <div className="form">
            <label className="field">Classname
              <input autoFocus value={item.cls} placeholder="e.g. M4A1, BandageDressing" aria-invalid={item.cls !== '' && !reClass.test(item.cls.trim())} onChange={(e) => setItem({ ...item, cls: e.target.value })} />
            </label>
            <div className="field">Quantity
              <div className="seg" role="group" aria-label="Quantity">{QTY.map((q) => <button type="button" key={q} aria-pressed={item.qty === q} onClick={() => setItem({ ...item, qty: q })}>{q}</button>)}</div>
            </div>
            <span className="hint">The server checks the classname exists in the loaded mods before creating anything.</span>
          </div>} />
      )}

      {(mode === 'heal' || mode === 'kill' || mode === 'strip') && (
        <Confirm title={`${av(mode)?.label || mode}: ${target.name}`} word={av(mode)?.confirm} tone={mode === 'heal' ? 'primary' : 'danger'}
          confirmLabel={av(mode)?.label || 'Confirm'} busy={busy} onClose={close} onConfirm={doMod} body={
            <p className="small muted">{mode === 'heal' ? 'Restores health, blood and stops bleeding.' : mode === 'kill' ? 'The player dies in game. This cannot be undone.' : 'Deletes everything the player carries. This cannot be undone.'}</p>} />
      )}
    </>
  )
}
