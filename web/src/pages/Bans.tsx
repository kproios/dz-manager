import { useEffect, useState } from 'react'
import { api, Ban, Player } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox, IdValue, PageHead, Pager, Seg, StatTile, Trunc, usePaged } from '../ui'
import { ago, fmtTime, relative } from '../util'
import { useNotify } from '../App'
import { Icon } from '../icons'
import { Avatar, Flag, RiskBadges, useIntel } from '../intel'
import { usePref } from '../prefs'

const REASONS = ['Cheating / hacking', 'Exploiting / duping', 'Combat logging', 'Toxic behaviour', 'Ban evasion']
const DURATIONS: [number, string][] = [[60, '1 hour'], [1440, '1 day'], [4320, '3 days'], [10080, '7 days'], [43200, '30 days'], [0, 'Permanent']]
type Status = 'active' | 'all' | 'expired' | 'revoked'

export default function Bans() {
  const [status, setStatus] = usePref<Status>('bans.status', 'active')
  const all = useApi<Ban[]>('/api/bans?status=all', ['events'])
  const lists = useApi<any[]>('/api/banlists')
  const queue = useApi<any[]>('/api/review-queue')
  const [drift, setDrift] = useState<any>(null)
  const [revoke, setRevoke] = useState<Ban | null>(null)
  const [search, setSearch] = useState('')
  const notify = useNotify()
  const bans = (all.data ?? []).filter((b) => (status === 'all' || b.status === status) &&
    (!search || `${b.name} ${b.steam64} ${b.be_guid} ${b.reason} ${b.issuer_name}`.toLowerCase().includes(search.toLowerCase())))
  const pg = usePaged(bans, 15)
  const count = (s: string) => (all.data ?? []).filter((b) => b.status === s).length
  const soon = (all.data ?? []).filter((b) => b.status === 'active' && b.expires_at && new Date(b.expires_at).getTime() - Date.now() < 86400000).length
  const failed = (all.data ?? []).filter((b) => b.status === 'active' && b.applied.startsWith('error')).length
  const intel = useIntel(pg.rows.map((b) => b.steam64))

  const reconcile = async () => {
    try { setDrift(await api.get('/api/bans/reconcile')) } catch (e: any) { notify(e.message, 'bad') }
  }
  const doRevoke = async () => {
    try { await api.post(`/api/bans/${revoke!.id}/revoke`); notify('Ban revoked; BattlEye updated'); all.reload() } catch (e: any) { notify(e.message, 'bad') } finally { setRevoke(null) }
  }
  return (
    <>
      <PageHead title="Bans" sub="Local bans are enforced by BattlEye · external lists are evidence only and never ban by themselves"
        actions={<><button className="small" onClick={reconcile} title="Compare our list with BattlEye's ban list"><Icon name="refresh" />Check against BattlEye</button><a className="button small" href="/api/bans/export">Export CSV</a></>} />
      <div className="tiles">
        <StatTile label="Active bans" value={count('active')} foot={`${count('expired')} expired · ${count('revoked')} revoked`} />
        <StatTile label="Expiring within 24 h" value={soon} foot="temporary bans that lift soon" />
        <StatTile label="Not applied" value={<span className={failed ? 'warn' : ''}>{failed}</span>} foot={failed ? 'BattlEye did not confirm these: use Check against BattlEye' : 'every active ban reached BattlEye'} />
        <StatTile label="Players to review" value={queue.data?.length ?? 0} foot="matches in imported external lists" />
      </div>
      {drift && <div className={drift.missing.length ? 'warnings' : 'card'} role="status">
        <b>BattlEye check:</b> {drift.in_sync} in sync · {drift.missing.length} missing in BattlEye · {drift.stale.length} still in BattlEye but revoked here · {drift.foreign.length} added by others (never touched)
      </div>}
      <BanForm onDone={all.reload} />
      <Card title={<Seg label="Status" value={status} onChange={(v) => { setStatus(v); pg.setPage(1) }} options={[['active', `Active (${count('active')})`], ['expired', 'Expired'], ['revoked', 'Revoked'], ['all', 'All']]} />}
        actions={<input type="search" placeholder="Search name, ID, reason or admin" value={search} onChange={(e) => { setSearch(e.target.value); pg.setPage(1) }} style={{ width: 260 }} />}>
        <ErrorBox error={all.error} />
        {!bans.length ? <Empty>{search ? 'No bans match this search.' : status === 'active' ? 'No active bans.' : 'Nothing here.'}</Empty> : <>
          <div className="table-scroll"><table>
            <thead><tr><th>Player</th><th>Reason</th><th>Issued</th><th>Ends</th><th>Status</th><th>BattlEye</th><th /></tr></thead>
            <tbody>
              {pg.rows.map((b) => {
                const i = b.steam64 ? intel[b.steam64] : undefined
                return (
                  <tr key={b.id}>
                    <td><div className="who-cell"><Avatar intel={i} name={b.name} size={24} /><span className="row" style={{ gap: 6 }}><Flag intel={i} />{b.steam64 ? <a href={'#/players/' + b.steam64} title={b.steam64}>{b.name || b.steam64}</a> : <IdValue value={b.be_guid} label="BE GUID" short />}</span></div></td>
                    <td><Trunc text={b.reason} max={240} /></td>
                    <td title={fmtTime(b.created_at)}>{ago(b.created_at)} <span className="muted small">by {b.issuer_name}</span></td>
                    <td>{b.revoked_at ? <span className="muted">revoked {ago(b.revoked_at)}</span> : b.expires_at ? <span title={fmtTime(b.expires_at)}>{relative(b.expires_at)}</span> : <Badge state="bad" title="Never expires">permanent</Badge>}</td>
                    <td><Badge state={b.status} /></td>
                    <td>{b.applied === 'ok' ? <Badge state="ok" title="BattlEye accepted the ban">applied</Badge> : b.applied === 'pending' ? <Badge state="info" title="Waiting for RCON">pending</Badge> : <Badge state="warn" title={b.applied}>not applied</Badge>}</td>
                    <td className="num">{b.status === 'active' && <button className="small" onClick={() => setRevoke(b)}>Revoke</button>}</td>
                  </tr>
                )
              })}
            </tbody>
          </table></div>
          <Pager {...pg} noun="bans" />
        </>}
      </Card>
      <div className="grid two">
        <Card title="Review queue" actions={<span className="muted small">known players found in external lists</span>}>
          {!queue.data?.length ? <Empty>No unreviewed matches among known players.</Empty> : (
            <table><tbody>{queue.data.map((q, i) => <tr key={i}><td><a href={'#/players/' + q.steam64}>{q.name || q.steam64}</a></td><td><Badge state="warn">{q.list}</Badge></td><td className="muted small"><Trunc text={`${q.identifier_type}: ${q.reason || 'no reason shared'}`} max={260} /></td></tr>)}</tbody></table>
          )}
        </Card>
        <ExternalLists lists={lists.data} reload={() => { lists.reload(); queue.reload() }} />
      </div>
      {revoke && <Confirm title={`Revoke ban #${revoke.id}?`} confirmLabel="Revoke" tone="primary" body={<p><b>{revoke.name || revoke.be_guid}</b>: {revoke.reason}. BattlEye removes the ban and the player can join again.</p>} onConfirm={doRevoke} onClose={() => setRevoke(null)} />}
    </>
  )
}

function BanForm({ onDone }: { onDone: () => void }) {
  const notify = useNotify()
  const [q, setQ] = useState('')
  const [debounced, setDebounced] = useState('')
  const [pick, setPick] = useState<Player | null>(null)
  const [reason, setReason] = useState(REASONS[0])
  const [minutes, setMinutes] = useState(1440)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => { const t = window.setTimeout(() => setDebounced(q.trim()), 250); return () => window.clearTimeout(t) }, [q])
  const found = useApi<Player[]>(debounced.length >= 2 && !pick ? '/api/players?limit=8&q=' + encodeURIComponent(debounced) : null)
  const intel = useIntel(pick ? [pick.steam64] : (found.data ?? []).map((p) => p.steam64))
  const ban = async () => {
    if (!pick) return
    setBusy(true)
    try {
      await api.post('/api/bans', { steam64: pick.steam64, reason: reason.trim(), minutes })
      notify(`${pick.name || pick.steam64} banned${minutes ? ' for ' + DURATIONS.find((d) => d[0] === minutes)?.[1] : ' permanently'}`)
      setPick(null); setQ(''); setOpen(false); onDone()
    } catch (e: any) { notify(e.message, 'bad') } finally { setBusy(false) }
  }
  return (
    <Card title={<><Icon name="bans" />Ban a player</>}>
      <div className="ban-form">
        <div className="field">1 · Player
          {pick ? (
            <div className="picked"><Avatar intel={intel[pick.steam64]} name={pick.name} size={32} /><div><b>{pick.name || '(unknown)'}</b><div className="muted small">{pick.steam64} · last seen {ago(pick.last_seen)}</div><RiskBadges intel={intel[pick.steam64]} /></div>
              <button className="small ghost" onClick={() => setPick(null)}>Change</button></div>
          ) : (
            <div className="search-box">
              <input type="search" placeholder="Search the player database: name, Steam64 or BE GUID" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Find player" />
              {debounced.length >= 2 && <div className="results">
                {!found.data ? <div className="muted small">Searching…</div> : !found.data.length ? <div className="muted small">No players found. Only players who joined at least once can be banned here.</div>
                  : found.data.map((p) => <button key={p.steam64} type="button" className="result" onClick={() => setPick(p)}>
                    <Avatar intel={intel[p.steam64]} name={p.name} size={24} /><span className="grow" style={{ textAlign: 'left' }}><b>{p.name || '(unknown)'}</b> <span className="muted small">{p.steam64}</span></span><span className="muted small">seen {ago(p.last_seen)}</span>
                  </button>)}
              </div>}
            </div>
          )}
        </div>
        <div className="field">2 · Reason
          <select value={REASONS.includes(reason) ? reason : ''} onChange={(e) => setReason(e.target.value)}>{REASONS.map((r) => <option key={r}>{r}</option>)}<option value="">Other…</option></select>
          {!REASONS.includes(reason) && <input placeholder="Reason shown in the ban list" value={reason} maxLength={100} onChange={(e) => setReason(e.target.value)} />}
        </div>
        <div className="field">3 · Duration<Seg label="Duration" value={minutes} onChange={setMinutes} options={DURATIONS} /></div>
        <div className="row end"><button className="danger" disabled={!pick || !reason.trim()} onClick={() => setOpen(true)}>Ban {pick ? pick.name || 'player' : '…'}</button></div>
      </div>
      {open && pick && <Confirm title={`Ban ${pick.name || pick.steam64}?`} word="BAN" confirmLabel="Ban" busy={busy} onClose={() => setOpen(false)} onConfirm={ban} body={
        <ul className="small">
          <li>Reason: <b>{reason}</b></li>
          <li>Duration: <b>{DURATIONS.find((d) => d[0] === minutes)?.[1]}</b></li>
          <li>BattlEye enforces it immediately; if the player is online they are kicked.</li>
        </ul>} />}
    </Card>
  )
}

function ExternalLists({ lists, reload }: { lists: any[] | null; reload: () => void }) {
  const notify = useNotify()
  const [imp, setImp] = useState({ name: '', source: '', format: 'csv', content: '' })
  const doImport = async () => {
    try {
      const r = await api.post<{ entries: number }>('/api/banlists/import', imp)
      notify(`Imported ${r.entries} entries`)
      setImp({ ...imp, content: '' }); reload()
    } catch (e: any) { notify(e.body?.problems ? e.body.problems.slice(0, 5).join('; ') : e.message, 'bad') }
  }
  return (
    <Card title="External lists">
      {!lists?.length ? <Empty>No lists imported.</Empty> : (
        <table><tbody>{lists.map((l) => <tr key={l.id}><td><b>{l.name}</b></td><td className="num">{l.entries} entries</td><td className="muted small">imported {ago(l.imported_at)}</td><td><Badge state={l.status === 'stale' ? 'warn' : 'info'}>{l.status}</Badge></td></tr>)}</tbody></table>
      )}
      <details style={{ marginTop: 10 }}>
        <summary className="small">Import a list</summary>
        <div className="form" style={{ marginTop: 10 }}>
          <div className="field-row">
            <label className="field">List name (re-importing replaces it)<input value={imp.name} onChange={(e) => setImp({ ...imp, name: e.target.value })} /></label>
            <label className="field">Format<Seg label="Format" value={imp.format} onChange={(v) => setImp({ ...imp, format: v })} options={[['csv', 'CSV'], ['json', 'JSON']]} /></label>
          </div>
          <label className="field">Source / where it came from<input value={imp.source} onChange={(e) => setImp({ ...imp, source: e.target.value })} /></label>
          <label className="field">Content<textarea rows={4} placeholder="identifier,identifier_type,reason,listed_at" value={imp.content} onChange={(e) => setImp({ ...imp, content: e.target.value })} /></label>
          <div className="row end"><button className="small" disabled={!imp.name || !imp.content} onClick={doImport}>Import</button></div>
        </div>
      </details>
    </Card>
  )
}
