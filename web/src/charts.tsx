import { useEffect, useMemo, useRef, useState } from 'react'

export interface Pt { t: number; v: number | null }

function niceMax(v: number) {
  if (v <= 0) return 1
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p
  return 10 * p
}
const fmtNum = (v: number) => v >= 10000 ? (v / 1000).toFixed(0) + 'K' : v >= 100 ? Math.round(v).toLocaleString() : String(Math.round(v * 10) / 10)

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [w, setW] = useState(600)
  useEffect(() => {
    if (!ref.current || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(([e]) => setW(Math.max(200, Math.round(e.contentRect.width))))
    ro.observe(ref.current)
    return () => ro.disconnect()
  }, [])
  return [ref, w] as const
}

/** Single-series time chart: 2px line, 10% area wash, hairline grid, crosshair tooltip. */
export function LineChart({ data, color, height = 180, unit = '', label, minY = 0, fmt = fmtNum }: {
  data: Pt[]; color: string; height?: number; unit?: string; label: string; minY?: number; fmt?: (v: number) => string
}) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const pad = { l: 40, r: 10, t: 10, b: 22 }
  const pts = data.filter((d) => d.v != null) as { t: number; v: number }[]
  const geo = useMemo(() => {
    if (pts.length < 2) return null
    const t0 = pts[0].t, t1 = pts[pts.length - 1].t
    const max = niceMax(Math.max(...pts.map((p) => p.v), minY + 1))
    const x = (t: number) => pad.l + ((t - t0) / Math.max(1, t1 - t0)) * (width - pad.l - pad.r)
    const y = (v: number) => pad.t + (1 - (v - minY) / (max - minY)) * (height - pad.t - pad.b)
    // Gaps over 5 minutes (server down, no telemetry) break the line instead of bridging it.
    let d = '', area = '', seg: { t: number; v: number }[] = []
    const flush = () => {
      if (!seg.length) return
      d += seg.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join('')
      area += `M${x(seg[0].t).toFixed(1)},${y(minY)}` + seg.map((p) => `L${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join('') + `L${x(seg[seg.length - 1].t).toFixed(1)},${y(minY)}Z`
      seg = []
    }
    const gap = Math.max(5 * 60000, 3 * (t1 - t0) / Math.max(1, pts.length - 1))
    pts.forEach((p, i) => { if (i && p.t - pts[i - 1].t > gap) flush(); seg.push(p) })
    flush()
    const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => minY + f * (max - minY))
    const span = t1 - t0
    const xt: number[] = []
    const step = span > 3 * 86400000 ? 86400000 : span > 12 * 3600000 ? 4 * 3600000 : span > 3 * 3600000 ? 3600000 : 15 * 60000
    for (let t = Math.ceil(t0 / step) * step; t <= t1; t += step) xt.push(t)
    return { x, y, d, area, ticks, xt, t0, t1, span }
  }, [pts, width, height, minY])

  if (!geo) return <div className="chart-empty">No {label.toLowerCase()} data for this period yet.</div>
  const fmtT = (t: number) => new Date(t).toLocaleString(undefined, geo.span > 3 * 86400000 ? { weekday: 'short', day: 'numeric' } : { hour: '2-digit', minute: '2-digit' })
  const onMove = (e: React.MouseEvent<SVGRectElement>) => {
    const r = (e.currentTarget as SVGRectElement).getBoundingClientRect()
    const px = e.clientX - r.left + pad.l
    let best = 0, bd = Infinity
    pts.forEach((p, i) => { const d = Math.abs(geo.x(p.t) - px); if (d < bd) { bd = d; best = i } })
    setHover(best)
  }
  const h = hover != null ? pts[hover] : null
  return (
    <div className="chart" ref={ref}>
      <svg viewBox={`0 0 ${width} ${height}`} height={height} role="img" aria-label={`${label} over time`}>
        {geo.ticks.map((v) => <g key={v}>
          <line x1={pad.l} x2={width - pad.r} y1={geo.y(v)} y2={geo.y(v)} stroke="var(--grid)" strokeWidth={1} />
          <text className="tick" x={pad.l - 6} y={geo.y(v) + 4} textAnchor="end">{fmt(v)}</text>
        </g>)}
        {geo.xt.map((t) => <text key={t} className="tick" x={geo.x(t)} y={height - 5} textAnchor="middle">{fmtT(t)}</text>)}
        <path d={geo.area} fill={color} opacity={0.1} />
        <path d={geo.d} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        {h && <>
          <line x1={geo.x(h.t)} x2={geo.x(h.t)} y1={pad.t} y2={height - pad.b} stroke="var(--axis)" strokeWidth={1} />
          <circle cx={geo.x(h.t)} cy={geo.y(h.v)} r={4.5} fill={color} stroke="var(--panel)" strokeWidth={2} />
        </>}
        <rect x={pad.l} y={0} width={width - pad.l - pad.r} height={height} fill="transparent" onMouseMove={onMove} onMouseLeave={() => setHover(null)} />
      </svg>
      {h && <div className="chart-tip" style={{ left: `${(geo.x(h.t) / width) * 100}%`, top: geo.y(h.v) }}>
        <b>{fmt(h.v)}{unit}</b> <span className="muted">{label}</span><br /><span className="muted">{new Date(h.t).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' })}</span>
      </div>}
    </div>
  )
}

/** Tiny trend line for a stat tile (de-emphasised, last point accented). */
export function Sparkline({ values, color = 'var(--series-1)' }: { values: (number | null)[]; color?: string }) {
  const v = values.filter((x): x is number => x != null)
  if (v.length < 2) return <svg className="spark" aria-hidden="true" />
  const w = 120, h = 30, max = Math.max(...v), min = Math.min(...v), span = Math.max(1e-9, max - min)
  const pts = v.map((x, i) => [(i / (v.length - 1)) * (w - 4) + 2, h - 3 - ((x - min) / span) * (h - 6)])
  const last = pts[pts.length - 1]
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <path d={pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('')} fill="none" stroke="#5f6b78" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
      <circle cx={last[0]} cy={last[1]} r={2.5} fill={color} />
    </svg>
  )
}

/** Vertical bars for a short categorical series (e.g. restart downtimes). */
export function Bars({ items, color, unit = '', height = 140, fmt = fmtNum }: { items: { label: string; v: number; tip?: string }[]; color: string; unit?: string; height?: number; fmt?: (v: number) => string }) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  if (!items.length) return <div className="chart-empty">Nothing recorded yet.</div>
  const pad = { l: 40, r: 6, t: 10, b: 22 }
  const max = niceMax(Math.max(...items.map((i) => i.v), 1))
  const band = (width - pad.l - pad.r) / items.length
  const bw = Math.min(24, band - 6)
  const y = (v: number) => pad.t + (1 - v / max) * (height - pad.t - pad.b)
  const base = y(0)
  return (
    <div className="chart" ref={ref}>
      <svg viewBox={`0 0 ${width} ${height}`} height={height} role="img" aria-label="Bar chart">
        {[0, 0.5, 1].map((f) => <g key={f}>
          <line x1={pad.l} x2={width - pad.r} y1={y(f * max)} y2={y(f * max)} stroke="var(--grid)" />
          <text className="tick" x={pad.l - 6} y={y(f * max) + 4} textAnchor="end">{fmt(f * max)}</text>
        </g>)}
        {items.map((it, i) => {
          const x = pad.l + i * band + (band - bw) / 2, top = y(it.v), r = Math.min(4, (base - top) / 2)
          const d = `M${x},${base}V${top + r}Q${x},${top} ${x + r},${top}H${x + bw - r}Q${x + bw},${top} ${x + bw},${top + r}V${base}Z`
          return <g key={i} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
            <rect x={pad.l + i * band} y={pad.t} width={band} height={height - pad.t - pad.b} fill="transparent" />
            {it.v > 0 && <path d={d} fill={color} opacity={hover == null || hover === i ? 1 : 0.55} />}
            {(items.length <= 12 || i % Math.ceil(items.length / 8) === 0) && <text className="tick" x={x + bw / 2} y={height - 5} textAnchor="middle">{it.label}</text>}
          </g>
        })}
      </svg>
      {hover != null && <div className="chart-tip" style={{ left: `${((pad.l + hover * band + band / 2) / width) * 100}%`, top: y(items[hover].v) }}>
        <b>{fmt(items[hover].v)}{unit}</b><br /><span className="muted">{items[hover].tip || items[hover].label}</span>
      </div>}
    </div>
  )
}
