// Thin API client. All state-changing calls send the session CSRF token.

let csrf = ''

export function setCSRF(t: string) {
  csrf = t
}

export class ApiError extends Error {
  status: number
  body: any
  constructor(status: number, body: any) {
    super(body?.error || `HTTP ${status}`)
    this.status = status
    this.body = body
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET') headers['X-DZM-CSRF'] = csrf
  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin' })
  if (res.status === 401) {
    window.location.href = '/auth/login?return=' + encodeURIComponent(window.location.pathname + window.location.hash)
    throw new ApiError(401, { error: 'session expired' })
  }
  const text = await res.text()
  let data: any = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { error: text }
  }
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
  put: <T>(p: string, b: unknown) => request<T>('PUT', p, b),
}

// ---- types mirrored from the Go API ----
export interface Me { sub: string; username: string; email: string; csrf: string }
export interface Cap { name: string; state: 'unconfigured' | 'unverified' | 'ok' | 'degraded' | 'unsupported'; reason: string }
export interface Profile {
  id: string; name: string; provider_kind: string; active: boolean; archived: boolean
  game_host: string; game_port: number; query_host: string; query_port: number
  rcon_host: string; rcon_port: number; has_rcon_password: boolean; metricz_user: string
}
export interface Link { state: string; error?: string; last_ok?: string }
export interface RconPlayer { number: number; name: string; be_guid: string; verified: boolean; lobby: boolean; ping: number; ip?: string }
export interface ServerStatus {
  server_id: string; query: Link; rcon: Link; players: RconPlayer[] | null; locked?: boolean; updated_at: string
  info?: { name: string; map: string; players: number; max_players: number; version: string; latency_ms: number }
}
export interface MzPlayer {
  steam64: string; be_guid?: string; name: string; x: number; y: number; z: number
  health?: number; blood?: number; shock?: number; water?: number; energy?: number
  unconscious?: boolean; bleeding?: boolean; restrained?: boolean; godmode?: boolean; ping_max?: number
}
export interface MzVehicle { id: string; class: string; type: string; x: number; y: number; z: number; health?: number; fuel?: number; crew?: number; speed?: number; engine_on?: boolean }
export interface MapState {
  server_id: string; available: boolean; fresh: boolean; received_at?: string; reason?: string
  players: MzPlayer[] | null; vehicles: MzVehicle[] | null
  perf?: { fps?: number; fps_avg?: number; fps_min?: number; players?: number; infected?: number; animals?: number; items?: number; vehicles: number; uptime_seconds?: number }
}
export interface Marker { id: number; key: string; category: string; class: string; x: number; y: number; z: number; started_at: string; ended_at?: string; status: string; source: string; observation?: string }
export interface Player {
  steam64: string; be_guid: string; dayz_id: string; name: string; first_seen: string; last_seen: string
  playtime_seconds: number; kills: number; deaths: number; id_mismatch: boolean
}
export interface AuditEntry { id: number; ts: string; actor_name: string; action: string; target: string; params: any; result: string; detail: string }
export interface PlayerProfile {
  player: Player
  names: { name: string; first_seen: string; last_seen: string }[]
  sessions: { server_id: string; started_at: string; ended_at?: string; end_kind: string; source: string; duration_seconds: number }[]
  combat: { ts: string; victim: string; killer?: string; weapon?: string; distance_m?: number; cause?: string; role: string }[]
  audit: AuditEntry[]
}
export interface Ban { id: number; steam64?: string; be_guid: string; name?: string; reason: string; created_at: string; expires_at?: string; issuer_name: string; revoked_at?: string; applied: string; status: string }
export interface Flags {
  steam64: string; local_ban?: Ban; disclaimer: string; enforcement: string
  external: { list: string; list_status: string; imported_at: string; identifier_type: string; identifier: string; reason?: string }[]
  review?: { decision: string; note: string; reviewer: string; at: string }
}
export interface ActionAvail { name: string; label: string; destructive: boolean; confirm?: string; available: boolean; reason?: string }
export interface Schedule {
  server_id: string; enabled: boolean; times: string[]; timezone: string; mode: string; announce: boolean
  announce_minutes: number[]; announce_text: string; window_minutes: number; timeout_minutes: number; dry_run: boolean
}
export interface RestartEvent { id: number; down_at?: string; up_at?: string; planned_for?: string; kind: string; outcome: string; duration_seconds?: number }
