// Keeps Leaflet markers in step with data by key. Markers are updated in place and
// never rebuilt, so with positions arriving every two seconds a hover, a click or an
// open tooltip is not lost to a refresh, and nothing on the map can go stale: every
// marker is compared with the latest data on each update.

/** The part of a Leaflet marker this needs; a small fake stands in for it in tests. */
export interface SyncMarker {
  setLatLng(ll: [number, number]): unknown
  setIcon(icon: any): unknown
  setZIndexOffset(n: number): unknown
  bindTooltip(content: any, options?: any): unknown
  unbindTooltip(): unknown
  on(event: string, fn: () => void): unknown
  addTo(group: any): unknown
  remove(): unknown
}

export interface SyncItem {
  key: string
  latlng: [number, number]
  /** Anything that changes the icon. The icon is rebuilt only when this changes. */
  look: string
  icon: () => any
  title: string
  tooltip: () => any
  /** Anything that changes the tooltip. */
  tip: string
  tipOptions: Record<string, unknown>
  zIndexOffset?: number
  group: any
  onClick: () => void
}

interface Entry { marker: SyncMarker; item: SyncItem }

export function syncMarkers(store: Map<string, Entry>, items: SyncItem[], create: (item: SyncItem) => SyncMarker) {
  const wanted = new Set<string>()
  for (const item of items) {
    wanted.add(item.key)
    const entry = store.get(item.key)
    if (!entry) {
      const marker = create(item)
      const fresh: Entry = { marker, item }
      // The handler reads the entry, so it always acts on the newest data.
      marker.on('click', () => fresh.item.onClick())
      marker.bindTooltip(item.tooltip(), item.tipOptions)
      marker.addTo(item.group)
      store.set(item.key, fresh)
      continue
    }
    const { marker, item: old } = entry
    if (old.latlng[0] !== item.latlng[0] || old.latlng[1] !== item.latlng[1]) marker.setLatLng(item.latlng)
    if (old.look !== item.look) marker.setIcon(item.icon())
    if ((old.zIndexOffset ?? 0) !== (item.zIndexOffset ?? 0)) marker.setZIndexOffset(item.zIndexOffset ?? 0)
    if (old.tip !== item.tip) { marker.unbindTooltip(); marker.bindTooltip(item.tooltip(), item.tipOptions) }
    if (old.group !== item.group) { marker.remove(); marker.addTo(item.group) }
    entry.item = item
  }
  for (const [key, entry] of store) {
    if (!wanted.has(key)) { entry.marker.remove(); store.delete(key) }
  }
}

export type MarkerStore = Map<string, Entry>
