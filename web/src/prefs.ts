import { useEffect, useState } from 'react'
import { api } from './api'
import { onStream, RESYNC } from './hooks'

// Per-user UI preferences, stored server-side (/api/prefs, keyed by the user)
// so they survive refreshes, sign-outs and other PCs. localStorage only
// mirrors the last value for an instant first paint.
type Prefs = Record<string, unknown>
const LS = 'dzm.prefs'
let prefs: Prefs = readLocal()
let loaded = false
let loading: Promise<void> | null = null
let saveTimer: number | undefined
const subs = new Set<() => void>()

function readLocal(): Prefs {
  try { return JSON.parse(localStorage.getItem(LS) || '{}') || {} } catch { return {} }
}
function writeLocal() {
  try { localStorage.setItem(LS, JSON.stringify(prefs)) } catch { /* storage blocked */ }
}

function ensureLoaded() {
  if (loaded || loading) return
  loading = api.get<Prefs>('/api/prefs').then((p) => {
    // Keep anything changed locally while the request was in flight.
    prefs = { ...(p || {}), ...pendingLocal }
    pendingLocal = {}
    loaded = true
    writeLocal()
    subs.forEach((f) => f())
  }).catch(() => { loading = null })
}
let pendingLocal: Prefs = {}

function save() {
  window.clearTimeout(saveTimer)
  saving = true
  const run = () => {
    api.put('/api/prefs', prefs).catch(() => { /* retried on the next change */ }).finally(() => { saving = false })
  }
  // Never overwrite the stored set with a partial copy before it has loaded.
  saveTimer = window.setTimeout(() => { if (loaded) run(); else loading?.then(() => { if (loaded) run() }) }, 500)
}

// Another device (or tab) changed a preference: show it here without a reload.
let saving = false
let listening = false
function listen() {
  if (listening) return
  listening = true
  onStream((event) => {
    if ((event !== 'prefs' && event !== RESYNC) || saving || !loaded) return
    api.get<Prefs>('/api/prefs').then((p) => {
      if (saving || !p || JSON.stringify(p) === JSON.stringify(prefs)) return
      prefs = p
      writeLocal()
      subs.forEach((f) => f())
    }).catch(() => { /* the next change retries */ })
  })
}

export function usePref<T>(key: string, fallback: T): [T, (v: T | ((old: T) => T)) => void] {
  const [, force] = useState(0)
  useEffect(() => {
    const f = () => force((n) => n + 1)
    subs.add(f)
    ensureLoaded()
    listen()
    return () => { subs.delete(f) }
  }, [])
  const value = (key in prefs ? prefs[key] : fallback) as T
  const set = (v: T | ((old: T) => T)) => {
    const cur = (key in prefs ? prefs[key] : fallback) as T
    const next = typeof v === 'function' ? (v as (o: T) => T)(cur) : v
    prefs = { ...prefs, [key]: next }
    if (!loaded) pendingLocal = { ...pendingLocal, [key]: next }
    writeLocal()
    subs.forEach((f) => f())
    save()
  }
  return [value, set]
}
