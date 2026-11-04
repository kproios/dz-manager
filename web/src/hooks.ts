import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError } from './api'

type Listener = (event: string, data: any) => void
const listeners = new Set<Listener>()
let source: EventSource | null = null
let connectedOnce = false

/** Pseudo-event fired when live updates may have been missed (stream reconnect, tab shown again). */
export const RESYNC = '*resync'

function emit(name: string, data: any) { listeners.forEach((l) => l(name, data)) }

function ensureStream() {
  if (source || typeof EventSource === 'undefined') return
  source = new EventSource('/api/stream')
  const names = ['server_status', 'capabilities', 'map', 'map_events', 'events', 'restarts', 'profiles']
  for (const n of names) {
    source.addEventListener(n, (e) => {
      let data: any = null
      try { data = JSON.parse((e as MessageEvent).data) } catch { /* ignore */ }
      emit(n, data)
    })
  }
  // The server greets every (re)connection. Anything published while the stream
  // was down was lost, so views refetch instead of trusting stale data.
  source.addEventListener('hello', () => {
    if (connectedOnce) emit(RESYNC, null)
    connectedOnce = true
  })
  source.onerror = () => { /* the browser reconnects automatically */ }
  if (typeof document !== 'undefined') {
    // Background tabs throttle timers to minutes; refresh as soon as the tab is visible again.
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') emit(RESYNC, null) })
    window.addEventListener('online', () => emit(RESYNC, null))
  }
}

/** Subscribe to server-sent events. */
export function useStream(fn: Listener) {
  const ref = useRef(fn)
  ref.current = fn
  useEffect(() => {
    ensureStream()
    const l: Listener = (e, d) => ref.current(e, d)
    listeners.add(l)
    return () => { listeners.delete(l) }
  }, [])
}

/** Fetch JSON, refetching when any of the given SSE events fire (debounced),
 * on an optional interval, after a reconnect/tab focus, and shortly after a failure. */
export function useApi<T>(path: string | null, events: string[] = [], intervalMs = 0) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  const retry = useRef<number | undefined>(undefined)
  const generation = useRef(0)
  const load = useCallback(async () => {
    if (!path) return
    const current = ++generation.current
    window.clearTimeout(retry.current)
    setLoading(true)
    try {
      const next = await api.get<T>(path)
      if (current !== generation.current) return
      setData(next)
      setError(null)
    } catch (e: any) {
      if (current !== generation.current) return
      setError(e.message)
      // Transient failures (tunnel hiccup, busy DB) must not leave a view empty until the next interval.
      if (!(e instanceof ApiError) || e.status >= 500) retry.current = window.setTimeout(() => { if (current === generation.current) load() }, 4000)
    } finally {
      if (current === generation.current) setLoading(false)
    }
  }, [path])
  useEffect(() => {
    setData(null); load()
    return () => { generation.current++; window.clearTimeout(timer.current); window.clearTimeout(retry.current) }
  }, [load])
  useEffect(() => {
    if (!intervalMs) return
    const t = window.setInterval(load, intervalMs)
    return () => window.clearInterval(t)
  }, [load, intervalMs])
  useStream((e) => {
    if (e !== RESYNC && !events.includes(e)) return
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(load, e === RESYNC ? 50 : 300)
  })
  return { data, error, loading, reload: load, setData }
}

export function useHashRoute(): [string, string[]] {
  const get = () => (window.location.hash.replace(/^#\/?/, '') || 'dashboard')
  const [route, setRoute] = useState(get())
  useEffect(() => {
    const on = () => setRoute(get())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const parts = route.split('/').map(decodeURIComponent)
  return [parts[0], parts.slice(1)]
}

/** A ticking clock for countdowns. */
export function useNow(ms = 1000) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => { const t = window.setInterval(() => setNow(Date.now()), ms); return () => window.clearInterval(t) }, [ms])
  return now
}
