import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api'

type Listener = (event: string, data: any) => void
const listeners = new Set<Listener>()
let source: EventSource | null = null

function ensureStream() {
  if (source) return
  source = new EventSource('/api/stream')
  const names = ['server_status', 'capabilities', 'map', 'map_events', 'events', 'restarts', 'profiles']
  for (const n of names) {
    source.addEventListener(n, (e) => {
      let data: any = null
      try { data = JSON.parse((e as MessageEvent).data) } catch { /* ignore */ }
      listeners.forEach((l) => l(n, data))
    })
  }
  source.onerror = () => {
    // Browser reconnects automatically; nothing to do.
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

/** Fetch JSON, refetching when any of the given SSE events fire (debounced). */
export function useApi<T>(path: string | null, events: string[] = [], intervalMs = 0) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  const load = useCallback(async () => {
    if (!path) return
    setLoading(true)
    try {
      setData(await api.get<T>(path))
      setError(null)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [path])
  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!intervalMs) return
    const t = window.setInterval(load, intervalMs)
    return () => window.clearInterval(t)
  }, [load, intervalMs])
  useStream((e) => {
    if (!events.includes(e)) return
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(load, 300)
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
