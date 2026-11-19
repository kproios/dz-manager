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

/** Set by the app: asks for a 2FA code when a sensitive action needs a fresh
 * confirmation. Resolves true when confirmed, false when cancelled. */
let stepUp: (() => Promise<boolean>) | null = null
export function setStepUpHandler(h: (() => Promise<boolean>) | null) { stepUp = h }

/** Makes sure a 2FA confirmation is fresh (asks if needed) before something
 * that leaves the page, such as linking a Steam account. */
export async function ensureStepUp(until: string | undefined): Promise<boolean> {
  if (until && Date.parse(until) - Date.now() > 60_000) return true
  return stepUp ? stepUp() : false
}

/** Called on 401 for API calls (the session ended): the app shows the sign-in page. */
let onSignedOut: (() => void) | null = null
export function setSignedOutHandler(h: (() => void) | null) { onSignedOut = h }

async function request<T>(method: string, path: string, body?: unknown, retried = false): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET') headers['X-DZM-CSRF'] = csrf
  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin' })
  const text = await res.text()
  let data: any = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { error: text }
  }
  if (res.status === 401 && path.startsWith('/api/') && path !== '/api/account/confirm') {
    onSignedOut?.()
    throw new ApiError(401, { error: 'Your session ended. Sign in again.' })
  }
  if (res.status === 403 && data?.step_up && !retried && stepUp) {
    if (await stepUp()) return request<T>(method, path, body, true)
    throw new ApiError(403, { error: 'Not confirmed: nothing was changed.' })
  }
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
  put: <T>(p: string, b: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}

// ---- types mirrored from the Go API ----
export type Role = 'owner' | 'admin' | 'moderator' | 'viewer'
export type Permission = 'view.live' | 'players.private' | 'players.moderate' | 'bans.manage' | 'actions.safe' | 'actions.strong'
  | 'server.control' | 'settings.manage' | 'servers.manage' | 'users.manage'
export interface Me { id: number; sub: string; username: string; email: string; csrf: string; role: Role; permissions: Permission[]; method: string; step_up_until: string }
export interface Methods { password: boolean; steam: boolean; oidc: boolean; oidc_label?: string }
export interface AuthState { signed_in: boolean; username?: string; pending?: 'totp' | 'enroll'; setup_needed: boolean; methods: Methods }
export interface RoleInfo { name: Role; label: string; help: string; permissions: Permission[] }
export interface PermInfo { name: Permission; label: string }
export interface Enrollment { secret: string; uri: string; qr: string; account: string; issuer: string; ticket?: string }
export interface SessionInfo { id: string; method: string; ip: string; user_agent: string; created_at: string; last_seen_at: string; current: boolean }
export interface Account {
  user: Me; role_label: string; login: string; has_password: boolean; steam64: string; oidc_linked: boolean
  totp: boolean; recovery_left: number; sessions: SessionInfo[]; methods: Methods; step_up_until: string; permissions: PermInfo[]
}
export interface UserRow {
  id: number; username: string; role: Role; password: boolean; steam64?: string; oidc: boolean; totp: boolean; disabled: boolean
  created_at: string; last_login_at: string; sessions: number; locked_until?: string; self: boolean
}
export interface InviteRow { id: string; role: Role; note: string; created_by: string; created_at: string; expires_at: string }
export interface UsersResp { users: UserRow[]; invites: InviteRow[]; roles: RoleInfo[]; permissions: PermInfo[]; methods: Methods }
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
  /** Who provides positions now: 'bridge' (live) or 'metricz' (the slower fallback). */
  source?: 'bridge' | 'metricz'; interval_s?: number
  players: MzPlayer[] | null; vehicles: MzVehicle[] | null
  perf?: { fps?: number; fps_avg?: number; fps_min?: number; players?: number; infected?: number; animals?: number; items?: number; vehicles: number; uptime_seconds?: number }
}
export interface DeathData { victim?: string; victim_name?: string; killer?: string; killer_name?: string; weapon?: string; cause?: string; distance_m?: number; killer_pos?: { x: number; z: number }; part?: string; detail?: string }
export interface Marker { id: number; key: string; category: string; class: string; x: number; y: number; z: number; started_at: string; last_seen_at?: string; ended_at?: string; status: string; source: string; observation?: string; kind?:string; name?:string; group?:string; radius?:number; container?: string; data?: DeathData }
export interface Player {
  steam64: string; be_guid: string; dayz_id: string; name: string; first_seen: string; last_seen: string
  playtime_seconds: number; kills: number; deaths: number; id_mismatch: boolean
}
export interface AuditEntry { id: number; ts: string; actor_name: string; action: string; target: string; params: any; result: string; detail: string }
export interface PlayerProfile {
  player: Player
  names: { name: string; first_seen: string; last_seen: string }[]
  sessions: { server_id: string; started_at: string; ended_at?: string; end_kind: string; source: string; duration_seconds: number }[]
  combat: { ts: string; victim: string; killer?: string; weapon?: string; distance_m?: number; cause?: string; role: string; x?: number; z?: number }[]
  audit: AuditEntry[]
}
export interface Ban { id: number; steam64?: string; be_guid: string; name?: string; reason: string; created_at: string; expires_at?: string; issuer_name: string; revoked_at?: string; applied: string; status: string }
export interface Flags {
  steam64: string; local_ban?: Ban; disclaimer: string; enforcement: string
  external: { list: string; list_status: string; imported_at: string; identifier_type: string; identifier: string; reason?: string }[]
  review?: { decision: string; note: string; reviewer: string; at: string }
}
export interface ActionAvail { name: string; label: string; target?: 'player' | 'vehicle'; help?: string; destructive: boolean; confirm?: string; available: boolean; reason?: string }

/** A named place from the game's CfgWorlds config (towns, villages, hills…). */
export interface WorldPlace { name: string; local_name?: string; kind: string; x: number; z: number }
export interface WorldInfo { world: string; label: string; size: number; places: WorldPlace[]; source: 'bridge' | 'manual' | 'default'; reported_at?: string }
export interface TileMeta { world: string; source: string; source_px: number; max_zoom: number; tile_size: number; format: string; installed_at: string }
export interface WorldResp { world: WorldInfo; tiles?: TileMeta; tile_url?: string; max_px: number }
export interface MapSourceFile { px: number; url: string; size?: string; fits: boolean; name: string; built?: string }
export interface MapSource { slug: string; label: string; group: string; files: MapSourceFile[] }
export interface MapSources { list_url: string; sources: MapSource[]; suggested: string; max_px: number }
export interface MapJob { state: 'idle' | 'downloading' | 'decoding' | 'tiling' | 'done' | 'error'; world?: string; source?: string; bytes?: number; total?: number; tiles?: number; tiles_total?: number; error?: string; started_at?: string; finished_at?: string }
export interface Schedule {
  server_id: string; enabled: boolean; times: string[]; timezone: string; mode: string; announce: boolean
  announce_minutes: number[]; announce_text: string; window_minutes: number; timeout_minutes: number; dry_run: boolean
  lock_minutes: number; kick_minutes: number; kick_message: string
}
export interface Intel {
  steam64: string; avatar?: string; persona?: string; profile_private: boolean; limited: boolean
  vac_banned: boolean; vac_bans?: number; game_bans?: number; days_since_ban?: number; community_banned: boolean; trade_ban?: string
  steam_checked_at?: string; steam_error?: string; ip?: string; has_ip: boolean; country?: string; country_code?: string; isp?: string
  proxy: boolean; hosting: boolean; ip_checked_at?: string
}
export interface ChatMessage { id: number; ts: string; channel: string; name: string; text: string }
export interface AutoMsgConfig {
  channel: 'rcon' | 'notify'
  welcome: { enabled: boolean; text: string; delay_seconds: number }
  rotation: { enabled: boolean; interval_minutes: number; messages: string[]; only_with_players: boolean }
}
export interface ManualRestart { at: string; requested: string; delay_minutes: number; by: string }
export interface PerfPoint { minute: string; fps: number | null; fps_min: number | null; players: number | null; infected: number | null; animals: number | null; items: number | null; vehicles: number | null }
export interface RestartEvent { id: number; down_at?: string; up_at?: string; planned_for?: string; kind: string; outcome: string; duration_seconds?: number }
