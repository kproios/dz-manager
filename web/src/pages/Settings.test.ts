import { describe, expect, it, vi } from 'vitest'
import { profileInput } from './Settings'

vi.mock('../App', () => ({ useNotify: () => () => {} }))
vi.mock('leaflet', () => ({ default: {} }))

describe('profileInput', () => {
  it('drops the read-only profile fields the API would refuse', () => {
    const fromApi = {
      id: 'TestServer', name: ' Test Server ', provider_kind: 'managed', active: true, archived: false,
      game_host: ' 203.0.113.10 ', game_port: 2302, query_host: '', query_port: 27016,
      rcon_host: '', rcon_port: 2305, has_rcon_password: true, metricz_user: 'metricz',
      created_at: '2026-11-04T12:00:00Z', updated_at: '2026-11-04T12:00:00Z', rcon_password: '',
    }
    expect(profileInput(fromApi)).toEqual({
      name: 'Test Server', provider_kind: 'managed',
      game_host: '203.0.113.10', game_port: 2302,
      query_host: '', query_port: 27016,
      rcon_host: '', rcon_port: 2305,
      rcon_password: '',
    })
  })
})
