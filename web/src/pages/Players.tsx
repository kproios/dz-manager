import { useState } from 'react'
import { Player, RconPlayer, ServerStatus } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, PageHead, Pager, Seg, SteamId, Trunc, usePaged } from '../ui'
import { ago, fmtDuration } from '../util'
import { usePref } from '../prefs'
import { Avatar, Flag, risks, RiskIcon, useIntel } from '../intel'

export const kd = (k: number, d: number) => (d ? k / d : k).toFixed(2)

type Filter = 'all' | 'online' | 'flagged'

/** Everyone who ever joined. Online players and live actions are on the Live page. */
export default function Players() {
  const roster = useApi<{ server_id: string; rcon: ServerStatus['rcon']; players: RconPlayer[] | null }>('/api/server/players', ['server_status'])
  const [q, setQ] = useState('')
  const [query, setQuery] = useState('')
  const hist = useApi<Player[]>('/api/players?limit=500' + (query ? '&q=' + encodeURIComponent(query) : ''), ['events'])
  const [size, setSize] = usePref('players.pageSize', 25)
  const [filter, setFilter] = usePref<Filter>('players.filter', 'all')
  const onlineGuids = new Set((roster.data?.players ?? []).map((p) => p.be_guid))
  const intel = useIntel((hist.data ?? []).map((p) => p.steam64))
  const rows = (hist.data ?? []).filter((p) => filter === 'all' || (filter === 'online' ? onlineGuids.has(p.be_guid) : risks(intel[p.steam64]).some((r) => r.level !== 'info')))
  const paged = usePaged(rows, size)

  return (
    <>
      <PageHead title="Player database" sub={`${hist.data?.length ?? 0} players · ${onlineGuids.size} online now`} actions={<a className="button small" href="#/live">Open Live view</a>} />
      <Card title={<Seg label="Show" value={filter} onChange={(v) => { setFilter(v); paged.setPage(1) }} options={[['all', 'Everyone'], ['online', 'Online now'], ['flagged', 'Flagged']]} />} actions={
        <form onSubmit={(e) => { e.preventDefault(); setQuery(q.trim()); paged.setPage(1) }} className="row">
          <input type="search" placeholder="Name, Steam64, BE GUID or DayZ ID" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 260 }} />
          <button>Search</button>
          {query && <button type="button" className="ghost" onClick={() => { setQ(''); setQuery('') }}>Clear</button>}
        </form>
      }>
        <ErrorBox error={hist.error} />
        {!rows.length ? <Empty>{query ? 'No players match this search.' : filter === 'flagged' ? 'No flagged players (Steam bans or VPN).' : filter === 'online' ? 'Nobody online.' : 'No players recorded yet. History comes from @DZMBridge events and MetricZ presence.'}</Empty> : (
          <>
            <div className="table-scroll"><table>
              <thead><tr><th>Player</th><th>Steam64</th><th>Checks</th><th>Last seen</th><th className="num">Playtime</th><th className="num">Kills</th><th className="num">Deaths</th><th className="num">K/D</th></tr></thead>
              <tbody>
                {paged.rows.map((p) => {
                  const i = intel[p.steam64]
                  return (
                    <tr key={p.steam64}>
                      <td><div className="who-cell"><Avatar intel={i} name={p.name} size={24} /><span className="row" style={{ gap: 6 }}><Flag intel={i} /><a href={'#/players/' + p.steam64}><Trunc text={p.name || '(unknown)'} max={200} /></a><RiskIcon intel={i} />{onlineGuids.has(p.be_guid) && <Badge state="ok">online</Badge>}{p.id_mismatch && <Badge state="warn" title="A source reported a BE GUID that does not match this Steam64">id mismatch</Badge>}</span></div></td>
                      <td><SteamId id={p.steam64} /></td>
                      <td>{risks(i).filter((r) => r.level !== 'info').map((r) => <Badge key={r.label} state={r.level} title={r.tip}>{r.label}</Badge>)}{!risks(i).some((r) => r.level !== 'info') && (i?.steam_checked_at ? <span className="muted small">clean</span> : <span className="faint small">pending</span>)}</td>
                      <td title={new Date(p.last_seen).toLocaleString()}>{ago(p.last_seen)}</td>
                      <td className="num">{fmtDuration(p.playtime_seconds)}</td>
                      <td className="num">{p.kills}</td>
                      <td className="num">{p.deaths}</td>
                      <td className="num">{kd(p.kills, p.deaths)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table></div>
            <div className="row pager">
              <label className="row small muted">Rows
                <select value={size} onChange={(e) => { setSize(+e.target.value); paged.setPage(1) }} style={{ width: 70 }}>{[10, 25, 50, 100].map((n) => <option key={n}>{n}</option>)}</select>
              </label>
              <Pager {...paged} noun="players" />
            </div>
          </>
        )}
      </Card>
    </>
  )
}
