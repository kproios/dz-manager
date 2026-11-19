import { describe, expect, it } from 'vitest'
import { ago, fmtDuration, fromLatLng, grid, pct, setWorldSize, toLatLng, WORLD } from './util'

describe('coordinates', () => {
  it('maps the map corners onto the zoom-0 tile', () => {
    expect(toLatLng(0, 0)).toEqual([-256, 0]) // south-west
    expect(toLatLng(WORLD, WORLD)).toEqual([0, 256]) // north-east
  })
  it('round-trips', () => {
    const ll = toLatLng(6560.12, 2480.5)
    const g = fromLatLng(ll[0], ll[1])
    expect(g.x).toBeCloseTo(6560.12, 6)
    expect(g.z).toBeCloseTo(2480.5, 6)
  })
  it('puts north at the top (higher z → higher lat)', () => {
    expect(toLatLng(100, 10000)[0]).toBeGreaterThan(toLatLng(100, 100)[0])
  })
  it('formats grid references', () => {
    expect(grid(6560, 15360 - 12650)).toBe('065 126')
  })
  it('follows the active world size (Livonia is 12800 m)', () => {
    setWorldSize(12800)
    try {
      expect(WORLD).toBe(12800)
      expect(toLatLng(12800, 12800)).toEqual([0, 256])
      expect(toLatLng(6400, 6400)).toEqual([-128, 128])
      expect(grid(100, 12800 - 200)).toBe('001 002')
      setWorldSize(NaN) // ignored
      expect(WORLD).toBe(12800)
    } finally {
      setWorldSize(15360)
    }
  })
})

describe('formatting', () => {
  it('durations', () => {
    expect(fmtDuration(3661)).toBe('1h 1m')
    expect(fmtDuration(90061)).toBe('1d 1h')
    expect(fmtDuration(42)).toBe('42s')
    expect(fmtDuration(undefined)).toBe('—')
  })
  it('relative time and percentages', () => {
    expect(ago('2026-01-01T00:00:00Z', Date.parse('2026-01-01T00:01:30Z'))).toBe('1m 30s ago')
    expect(ago(undefined)).toBe('never')
    expect(pct(0.954)).toBe('95%')
    expect(pct(undefined)).toBe('not reported')
  })
})
