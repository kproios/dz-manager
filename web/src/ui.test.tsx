import { describe, expect, it, vi } from 'vitest'

vi.mock('./App', () => ({ useNotify: () => () => {} }))
import { renderToStaticMarkup } from 'react-dom/server'
import { SteamId, steamUrl } from './ui'
import { actionGroup, readable } from './logs'
import { besides } from './playerActions'

describe('identity helpers', () => {
  it('links a Steam64 to its public Steam profile with a copy button', () => {
    const html = renderToStaticMarkup(<SteamId id="76561198000000001" />)
    expect(html).toContain('https://steamcommunity.com/profiles/76561198000000001')
    expect(html).toContain('target="_blank"')
    expect(html).toContain('Copy Steam64')
    expect(steamUrl('7656/../x')).not.toContain('/../')
  })
})

describe('readable logs', () => {
  it('names the new restart and RCON actions', () => {
    expect(readable('restart.kick_all')).toBe('Everyone kicked before restart')
    expect(readable('rcon.kick_all')).toBe('All players kicked')
    expect(readable('player.teleport')).toBe('Player teleported')
    expect(actionGroup('restart.lock')).toBe('restart')
    expect(actionGroup('ban.create')).toBe('ban')
  })
})

describe('player risk flags', () => {
  it('explains bans and VPNs, worst first, and stays quiet for clean players', async () => {
    const { risks } = await import('./intel')
    const base = { steam64: '76561198000000001', profile_private: false, limited: false, vac_banned: false, community_banned: false, has_ip: true, proxy: false, hosting: false }
    expect(risks(base)).toEqual([])
    const r = risks({ ...base, vac_banned: true, vac_bans: 2, days_since_ban: 40, hosting: true, limited: true })
    expect(r.map((x) => x.level)).toEqual(['bad', 'warn', 'info'])
    expect(r[0].label).toBe('VAC ×2')
    expect(r[0].tip).toContain('40 day')
  })
})

describe('vehicle icon rules', () => {
  it('lets owner rules override the built-in guess and recognises boats and bikes', async () => {
    const { vehicleIcon } = await import('./mapIcons')
    expect(vehicleIcon({ class: 'Boat_01_Orange', type: 'Boat' })).toBe('boat')
    expect(vehicleIcon({ class: 'MountainBike', type: '' })).toBe('bike')
    expect(vehicleIcon({ class: 'CivilianSedan', type: 'Car' }, [{ match: 'sedan', kind: 'police' }])).toBe('police')
    // Trucks get their own icon; the convoy icon is reserved for convoy world events.
    expect(vehicleIcon({ class: 'Truck_01_Covered', type: 'Car' })).toBe('truck')
    const { markerIcon } = await import('./mapIcons')
    expect(markerIcon({ category: 'military_convoy' })).toBe('convoy')
    expect(markerIcon({ category: 'death' })).toBe('death')
  })
})

describe('teleport to an event', () => {
  it('lands beside the object and stays inside the world', () => {
    expect(besides({ x: 100, z: 200 })).toEqual({ x: 104, z: 204 })
    expect(besides({ x: 15359, z: 15359 })).toEqual({ x: 15359, z: 15359 })
  })
})
