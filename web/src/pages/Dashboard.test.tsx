import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import Dashboard from './Dashboard'
import Players from './Players'
import MapPage from './MapPage'

vi.mock('../App', () => ({ useNotify: () => () => {} }))
vi.mock('leaflet', () => ({ default: {} }))
vi.mock('../hooks', () => ({
  useApi: (path: string) => ({
    data: path === '/api/server/status' || path === '/api/server/players' ? {
      server_id: 'TestServer', query: { state: 'ok' }, rcon: { state: 'ok' },
      players: null, updated_at: '2026-11-04T12:00:00Z',
      info: { name: 'TestServer', players: 0, max_players: 60, map: 'chernarusplus' },
    } : path === '/api/capabilities' ? { capabilities: [] } : path === '/api/map/state' ? {
      server_id: 'TestServer', available: false, fresh: false, players: null, vehicles: null,
    } : null,
    error: null,
  }),
}))

describe('Dashboard empty RCON roster', () => {
  it('renders zero players for the nullable empty slice returned by the Go API', () => {
    const html = renderToStaticMarkup(<Dashboard />)
    expect(html).toContain('TestServer')
    expect(html).toContain('RCON roster: 0')
  })

  it('renders the Players empty state when the API returns a null roster', () => {
    expect(renderToStaticMarkup(<Players />)).toContain('Nobody online.')
  })

  it('renders empty map counts before any MetricZ payload has arrived', () => {
    const html = renderToStaticMarkup(<MapPage />)
    expect(html).toContain('0 players')
    expect(html).toContain('0 vehicles')
  })
})
