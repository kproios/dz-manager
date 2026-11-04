import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import Dashboard from './Dashboard'
import Players from './Players'
import MapPage from './MapPage'

vi.mock('../App', () => ({ useNotify: () => () => {} }))
vi.mock('leaflet', () => ({ default: {} }))
vi.mock('../hooks', () => ({
  useNow: () => Date.parse('2026-11-04T12:00:00Z'),
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

  it('shows the lock, kick-all and restart controls and empty trend charts on the dashboard', () => {
    const html = renderToStaticMarkup(<Dashboard />)
    for (const text of ['Lock', 'Kick all', 'Restart…', 'Players online', 'Server FPS', 'No players data for this period yet.']) expect(html).toContain(text)
  })
})

describe('restart schedule helpers', () => {
  it('fills announcement placeholders the way the server does', async () => {
    const { render, TEMPLATES } = await import('./Restarts')
    expect(render(TEMPLATES[1], 5, '12:00')).toBe('Server restarts at 12:00 — 5 min left. Find a safe spot and log out.')
    expect(TEMPLATES.every((t) => t.includes('{m}'))).toBe(true)
  })
})
