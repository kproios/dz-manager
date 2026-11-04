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

describe('teleport to an event', () => {
  it('lands beside the object and stays inside the world', () => {
    expect(besides({ x: 100, z: 200 })).toEqual({ x: 104, z: 204 })
    expect(besides({ x: 15359, z: 15359 })).toEqual({ x: 15359, z: 15359 })
  })
})
