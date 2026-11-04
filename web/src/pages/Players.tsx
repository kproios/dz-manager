import { useState } from 'react'
import { Player, RconPlayer, ServerStatus } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Empty, ErrorBox, IdValue, PageHead, Pager, SteamId, Trunc, usePaged } from '../ui'
import { ago, fmtDuration } from '../util'
import { PlayerActions } from '../playerActions'
import { usePref } from '../prefs'

export default function Players() {
  const roster = useApi<{ server_id: string; rcon: ServerStatus['rcon']; players: RconPlayer[] | null }>('/api/server/players', ['server_status'])
  const [q, setQ] = useState('')
  const [query, setQuery] = useState('')
  const hist = useApi<Player[]>('/api/players' + (query ? '?q=' + encodeURIComponent(query) : ''), ['events'])
  const known = new Map((hist.data || []).map((p) => [p.be_guid, p]))
  const [size, setSize] = usePref('players.pageSize', 25)
  const paged = usePaged(hist.data, size)
  const online = roster.data?.players ?? []

  return (
    <>
      <PageHead title="Players" sub={`${online.length} online · ${hist.data?.length ?? 0} known`} />
      <Card title={<>Online now <span className="muted small">RCON roster</span></>} actions={roster.data && <Badge state={roster.data.rcon.state} />}>
        <ErrorBox error={roster.error} />
        {!online.length ? <Empty>{roster.data?.rcon.state === 'ok' || !roster.data ? 'Nobody online.' : 'RCON not connected: ' + (roster.data?.rcon.error || '')}</Empty> : (
          <div className="table-scroll"><table>
            <thead><tr><th>#</th><th>Name</th><th>Steam64</th><th>BE GUID</th><th className="num">Ping</th><th>Actions</th></tr></thead>
            <tbody>
              {online.map((p) => {
                const k = known.get(p.be_guid)
                return (
                  <tr key={p.be_guid + p.number}>
                    <td className="muted">{p.number}</td>
                    <td>
                      {k ? <a href={'#/players/' + k.steam64}><Trunc text={p.name} max={200} /></a> : <Trunc text={p.name} max={200} />}
                      {' '}{p.lobby && <Badge state="info">lobby</Badge>} {!p.verified && <Badge state="warn" title="BattlEye has not verified this GUID yet">unverified</Badge>}
                    </td>
                    <td>{k ? <SteamId id={k.steam64} short /> : <span className="muted small">not linked yet</span>}</td>
                    <td><IdValue value={p.be_guid} label="BE GUID" short /></td>
                    <td className="num"><PingCell ms={p.ping} /></td>
                    <td><PlayerActions target={{ steam64: k?.steam64 || '', name: p.name, be_guid: p.be_guid }} only={['message', 'heal', 'teleport', 'kick']} players={online.flatMap((o) => { const kk = known.get(o.be_guid); return kk ? [{ steam64: kk.steam64, name: o.name, be_guid: o.be_guid }] : [] })} /></td>
                  </tr>
                )
              })}
            </tbody>
          </table></div>
        )}
      </Card>
      <Card title="All players" actions={
        <form onSubmit={(e) => { e.preventDefault(); setQuery(q.trim()); paged.setPage(1) }} className="row">
          <input type="search" placeholder="Name, Steam64, BE GUID or DayZ ID" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 260 }} />
          <button>Search</button>
          {query && <button type="button" className="ghost" onClick={() => { setQ(''); setQuery('') }}>Clear</button>}
        </form>
      }>
        <ErrorBox error={hist.error} />
        {!hist.data?.length ? <Empty>{query ? 'No players match this search.' : 'No players recorded yet. History comes from @DZMBridge events and MetricZ presence.'}</Empty> : (
          <>
            <div className="table-scroll"><table>
              <thead><tr><th>Name</th><th>Steam64</th><th>Last seen</th><th className="num">Playtime</th><th className="num">Kills</th><th className="num">Deaths</th><th className="num">K/D</th></tr></thead>
              <tbody>
                {paged.rows.map((p) => (
                  <tr key={p.steam64}>
                    <td><a href={'#/players/' + p.steam64}><Trunc text={p.name || '(unknown)'} max={220} /></a> {p.id_mismatch && <Badge state="warn" title="A source reported a BE GUID that does not match this Steam64">id mismatch</Badge>}</td>
                    <td><SteamId id={p.steam64} /></td>
                    <td title={new Date(p.last_seen).toLocaleString()}>{ago(p.last_seen)}</td>
                    <td className="num">{fmtDuration(p.playtime_seconds)}</td>
                    <td className="num">{p.kills}</td>
                    <td className="num">{p.deaths}</td>
                    <td className="num">{kd(p.kills, p.deaths)}</td>
                  </tr>
                ))}
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

export const kd = (k: number, d: number) => (d ? k / d : k).toFixed(2)

function PingCell({ ms }: { ms: number }) {
  const c = ms < 80 ? 'okc' : ms < 160 ? 'warn' : ''
  return <span className={c} title={ms < 80 ? 'Good' : ms < 160 ? 'Fair' : 'High'}>{ms} ms</span>
}
