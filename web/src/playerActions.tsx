import { useState } from 'react'
import { ActionAvail, api, Marker } from './api'
import { useApi } from './hooks'
import { useNotify } from './App'
import { Confirm } from './ui'
import { Icon, IconName } from './icons'
import { grid, WORLD } from './util'
import { iconLabels, markerIcon, markerTitle } from './mapIcons'

export interface Target { steam64: string; name: string; be_guid?: string }
export interface Dest { label: string; x: number; z: number }

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

export function eventDests(markers: Marker[] | null | undefined): Dest[] {
  return (markers ?? []).filter((m) => m.status === 'active')
    .map((m) => ({ label: `${iconLabels[markerIcon(m)]}: ${markerTitle(m)} · ${grid(m.x, m.z)}`, ...besides(m) }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

type Mode = 'message' | 'kick' | 'teleport' | 'give_item' | 'heal' | 'kill' | 'strip' | null

export function PlayerActions({ target, layout = 'row', dests = [], players = [], onPick, only }: {
  target: Target; layout?: 'row' | 'grid'; dests?: Dest[]; players?: Target[]; onPick?: () => void; only?: Exclude<Mode, null>[]
}) {
  const acts = useApi<ActionAvail[]>('/api/actions', ['capabilities'])
  const run = useRunAction()
  const notify = useNotify()
  const [mode, setMode] = useState<Mode>(null)
  const [busy, setBusy] = useState(false)
  const [text, setText] = useState('')
  const [dest, setDest] = useState('')
  const [xz, setXz] = useState({ x: '', z: '' })
  const [item, setItem] = useState({ cls: '', qty: 1 })
  const av = (n: string) => acts.data?.find((a) => a.name === n)
  const close = () => { setMode(null); setText(''); setDest(''); setBusy(false) }

  const btn = (m: Exclude<Mode, null>, icon: IconName, label: string, opts: { danger?: boolean; disabled?: boolean; why?: string } = {}) => {
    if (only && !only.includes(m)) return null
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
                {dests.length > 0 && <optgroup label="Active events (lands a few metres beside it)">{dests.map((d, i) => <option key={i} value={'e:' + i}>{d.label}</option>)}</optgroup>}
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
