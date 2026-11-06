import { useRef } from 'react'
import { MapState, MzPlayer, MzVehicle } from './api'
import { useApi, useStream } from './hooks'

/** One position push from the game server, relayed by the panel over the event stream. */
export interface LiveDelta {
  server_id: string; received_at: string; interval_s: number
  players: MzPlayer[]; vehicles_ready: boolean; full: boolean; vehicles: MzVehicle[]; gone: string[]
}

/** Applies a live push to the map state. Pure, so the merge rules can be tested. */
export function applyLive(state: MapState | null, d: LiveDelta): MapState | null {
  if (!state) return state
  const next: MapState = { ...state, available: true, fresh: true, reason: undefined, received_at: d.received_at, source: 'bridge', interval_s: d.interval_s, players: d.players }
  // Until the first complete list arrives the vehicles on screen are MetricZ's,
  // whose ids differ: mixing the two would draw every vehicle twice.
  if (!d.vehicles_ready) return next
  if (d.full) { next.vehicles = d.vehicles; return withCount(next) }
  if (state.source !== 'bridge') return next
  const byId = new Map((state.vehicles ?? []).map((v) => [v.id, v]))
  for (const v of d.vehicles) byId.set(v.id, v)
  for (const id of d.gone) byId.delete(id)
  next.vehicles = [...byId.values()]
  return withCount(next)
}
function withCount(s: MapState): MapState { return s.perf ? { ...s, perf: { ...s.perf, vehicles: s.vehicles?.length ?? 0 } } : s }

/** How old the data on screen may get before the map calls itself stale. */
export function isFresh(state: MapState | null, lastUpdate: number, now: number): boolean {
  if (!state?.fresh) return false
  return now - lastUpdate < (state.source === 'bridge' ? 20000 : 50000)
}

/** The map's data: full fetch on load and every 15 s, small live pushes in between. */
export function useLiveMap() {
  const state = useApi<MapState>('/api/map/state', ['map'], 15000)
  const lastUpdate = useRef(0)
  const seen = useRef<MapState | null>(null)
  if (state.data !== seen.current) { seen.current = state.data; lastUpdate.current = Date.now() }
  useStream((event, data) => {
    if (event === 'live' && data) state.setData((old) => applyLive(old, data as LiveDelta))
  })
  return { ...state, lastUpdate }
}
