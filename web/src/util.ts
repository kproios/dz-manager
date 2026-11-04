// Pure helpers (unit-tested).

/** Chernarus world size in metres (DZMap tiles cover 0..WORLD at zoom 0 in 256 px). */
export const WORLD = 15360
export const TILE = 256
const K = TILE / WORLD

/** Game (x east, z north) → Leaflet CRS.Simple [lat, lng]. Y (altitude) is never used. */
export function toLatLng(x: number, z: number): [number, number] {
  return [z * K - TILE, x * K]
}

/** Leaflet CRS.Simple [lat, lng] → game {x, z}. */
export function fromLatLng(lat: number, lng: number): { x: number; z: number } {
  return { x: lng / K, z: (lat + TILE) / K }
}

export function fmtDuration(s?: number | null): string {
  if (s == null || !isFinite(s)) return '—'
  s = Math.max(0, Math.round(s))
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  if (d) return `${d}d ${h}h`
  if (h) return `${h}h ${m}m`
  if (m) return `${m}m ${s % 60}s`
  return `${s}s`
}

export function fmtTime(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'medium' })
}

export function ago(iso?: string | null, now = Date.now()): string {
  if (!iso) return 'never'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return 'never'
  return fmtDuration((now - t) / 1000) + ' ago'
}

export function pct(v?: number | null): string {
  return v == null ? 'not reported' : `${Math.round(v * 100)}%`
}

/** Grid reference like iZurvive's 3-digit format (x/z in 100 m squares). */
export function grid(x: number, z: number): string {
  const p = (n: number) => String(Math.max(0, Math.floor(n / 100))).padStart(3, '0')
  return `${p(x)} ${p(WORLD - z)}`
}
