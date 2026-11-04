import { useState } from 'react'
import { api, Player, RconPlayer, ServerStatus } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox } from '../ui'
import { ago, fmtDuration } from '../util'
import { useNotify } from '../App'

export default function Players() {
  const roster = useApi<{ server_id: string; rcon: ServerStatus['rcon']; players: RconPlayer[] }>('/api/server/players', ['server_status'])
  const [q, setQ] = useState('')
  const [query, setQuery] = useState('')
  const hist = useApi<Player[]>('/api/players' + (query ? '?q=' + encodeURIComponent(query) : ''), ['events'])
  const known = new Map((hist.data || []).map((p) => [p.be_guid, p]))

  return (
    <>
      <h1>Players</h1>
      <Card title={<>Online now <span className="muted small">(RCON roster)</span></>} actions={roster.data && <Badge state={roster.data.rcon.state} />}>
        <ErrorBox error={roster.error} />
        {!roster.data?.players.length ? <Empty>{roster.data?.rcon.state === 'ok' ? 'Nobody online.' : 'RCON not connected: ' + (roster.data?.rcon.error || '')}</Empty> : (
          <table>
            <thead><tr><th>#</th><th>Name</th><th>BE GUID</th><th>Ping</th><th /></tr></thead>
            <tbody>
              {roster.data.players.map((p) => {
                const k = known.get(p.be_guid)
                return (
                  <tr key={p.be_guid + p.number}>
                    <td>{p.number}</td>
                    <td>{k ? <a href={'#/players/' + k.steam64}>{p.name}</a> : p.name} {p.lobby && <Badge state="info">lobby</Badge>} {!p.verified && <Badge state="warn">unverified GUID</Badge>}</td>
                    <td><code className="small">{p.be_guid}</code></td>
                    <td>{p.ping}</td>
                    <td className="right"><RconActions p={p} /></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </Card>
      <Card title="History" actions={
        <form onSubmit={(e) => { e.preventDefault(); setQuery(q.trim()) }} className="row">
          <input placeholder="Name, Steam64, BE GUID or DayZ ID" value={q} onChange={(e) => setQ(e.target.value)} />
          <button>Search</button>
        </form>
      }>
        <ErrorBox error={hist.error} />
        {!hist.data?.length ? <Empty>No players recorded yet. History comes from @DZMBridge events (and MetricZ presence).</Empty> : (
          <table>
            <thead><tr><th>Name</th><th>Steam64</th><th>Last seen</th><th>Playtime</th><th>K / D</th></tr></thead>
            <tbody>
              {hist.data.map((p) => (
                <tr key={p.steam64}>
                  <td><a href={'#/players/' + p.steam64}>{p.name || '(unknown)'}</a> {p.id_mismatch && <Badge state="warn">id mismatch</Badge>}</td>
                  <td><code className="small">{p.steam64}</code></td>
                  <td>{ago(p.last_seen)}</td>
                  <td>{fmtDuration(p.playtime_seconds)}</td>
                  <td>{p.kills} / {p.deaths}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}

export function RconActions({ p }: { p: { be_guid: string; name: string } }) {
  const [mode, setMode] = useState<'pm' | 'kick' | null>(null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const notify = useNotify()
  const run = async () => {
    setBusy(true)
    try {
      if (mode === 'pm') await api.post('/api/rcon/say', { be_guid: p.be_guid, message: text })
      else await api.post('/api/rcon/kick', { be_guid: p.be_guid, reason: text })
      notify(mode === 'pm' ? 'Message sent' : 'Player kicked')
    } catch (e: any) {
      notify(e.message, 'bad')
    } finally {
      setBusy(false)
      setMode(null)
      setText('')
    }
  }
  return (
    <>
      <button className="small" onClick={() => setMode('pm')}>Message</button>{' '}
      <button className="small danger" onClick={() => setMode('kick')}>Kick</button>
      {mode && (
        <Confirm
          title={mode === 'pm' ? `Private message to ${p.name}` : `Kick ${p.name}?`}
          body={<>
            <p className="small muted">Target BE GUID <code>{p.be_guid}</code> (resolved against a fresh roster at execution time).</p>
            <input autoFocus maxLength={mode === 'pm' ? 200 : 120} placeholder={mode === 'pm' ? 'Message' : 'Reason (shown to the player)'} value={text} onChange={(e) => setText(e.target.value)} />
          </>}
          busy={busy || !text.trim()}
          onConfirm={run}
          onClose={() => setMode(null)}
        />
      )}
    </>
  )
}
