// Pure helpers (unit-tested).

/**
 * World size in metres of the active map (map tiles cover 0..WORLD at zoom 0
 * in 256 px). Chernarus until /api/map/world says otherwise; a live binding,
 * so every importer sees the value set by setWorldSize.
 */
export let WORLD = 15360
export const TILE = 256

/** Sets the active world size (called before the map is created). */
export function setWorldSize(metres: number) {
  if (isFinite(metres) && metres >= 1000) WORLD = metres
}

/** Game (x east, z north) → Leaflet CRS.Simple [lat, lng]. Y (altitude) is never used. */
export function toLatLng(x: number, z: number): [number, number] {
  const k = TILE / WORLD
  return [z * k - TILE, x * k]
}

/** Leaflet CRS.Simple [lat, lng] → game {x, z}. */
export function fromLatLng(lat: number, lng: number): { x: number; z: number } {
  const k = TILE / WORLD
  return { x: lng / k, z: (lat + TILE) / k }
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

/** "in 3h 20m" for a future instant, "2h 1m ago" for a past one. */
export function relative(iso?: string | null, now = Date.now()): string {
  if (!iso) return '—'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return '—'
  return t > now ? 'in ' + fmtDuration((t - now) / 1000) : fmtDuration((now - t) / 1000) + ' ago'
}

export function pct(v?: number | null): string {
  return v == null ? 'not reported' : `${Math.round(v * 100)}%`
}

/** The usual 3-digit DayZ grid reference (x/z in 100 m squares, counted from the north-west). */
export function grid(x: number, z: number): string {
  const p = (n: number) => String(Math.max(0, Math.floor(n / 100))).padStart(3, '0')
  return `${p(x)} ${p(WORLD - z)}`
}
