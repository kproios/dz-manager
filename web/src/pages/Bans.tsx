import { useState } from 'react'
import { api, Ban } from '../api'
import { useApi } from '../hooks'
import { Badge, Card, Confirm, Empty, ErrorBox, IdValue, PageHead, Pager, Trunc, usePaged } from '../ui'
import { fmtTime } from '../util'
import { useNotify } from '../App'

export default function Bans() {
  const [status, setStatus] = useState('active')
  const bans = useApi<Ban[]>('/api/bans?status=' + status)
  const lists = useApi<any[]>('/api/banlists')
  const queue = useApi<any[]>('/api/review-queue')
  const [drift, setDrift] = useState<any>(null)
  const [revoke, setRevoke] = useState<Ban | null>(null)
  const [imp, setImp] = useState({ name: '', source: '', format: 'csv', content: '' })
  const notify = useNotify()
  const pg = usePaged(bans.data, 15)

  const reconcile = async () => {
    try { setDrift(await api.get('/api/bans/reconcile')) } catch (e: any) { notify(e.message, 'bad') }
  }
  const doRevoke = async () => {
    try {
      await api.post(`/api/bans/${revoke!.id}/revoke`)
      notify('Ban revoked')
      bans.reload()
    } catch (e: any) { notify(e.message, 'bad') } finally { setRevoke(null) }
  }
  const doImport = async () => {
    try {
      const r = await api.post<{ entries: number }>('/api/banlists/import', imp)
      notify(`Imported ${r.entries} entries`)
      lists.reload(); queue.reload()
    } catch (e: any) {
      notify(e.body?.problems ? e.body.problems.slice(0, 5).join('; ') : e.message, 'bad')
    }
  }
  return (
    <>
      <PageHead title="Bans" sub="Local bans are enforced by BattlEye; external lists are evidence only" />
      <Card title="Local bans (BattlEye-enforced)" actions={<>
        <select value={status} onChange={(e) => { setStatus(e.target.value); pg.setPage(1) }} style={{ width: 120 }} aria-label="Show">
          <option value="active">active</option><option value="all">all</option><option value="revoked">revoked</option><option value="expired">expired</option>
        </select>
        <button className="small" onClick={reconcile}>Reconcile with BattlEye</button>
        <a className="button small" href="/api/bans/export">Export CSV</a>
      </>}>
        <ErrorBox error={bans.error} />
        {!bans.data?.length ? <Empty>No bans. Ban from a player profile.</Empty> : (
          <table>
            <thead><tr><th>#</th><th>Player</th><th>Reason</th><th>Issued</th><th>Expires</th><th>Status</th><th>Applied</th><th /></tr></thead>
            <tbody>
              {pg.rows.map((b) => (
                <tr key={b.id}>
                  <td>{b.id}</td>
                  <td>{b.steam64 ? <a href={'#/players/' + b.steam64} title={b.steam64}>{b.name || b.steam64}</a> : <IdValue value={b.be_guid} label="BE GUID" short />}</td>
                  <td><Trunc text={b.reason} max={240} /></td><td>{fmtTime(b.created_at)} <span className="muted small">by {b.issuer_name}</span></td><td>{b.expires_at ? fmtTime(b.expires_at) : 'permanent'}</td>
                  <td><Badge state={b.status} /></td><td className="small">{b.applied}</td>
                  <td>{b.status === 'active' && <button className="small" onClick={() => setRevoke(b)}>Revoke</button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <Pager {...pg} noun="bans" />
        {drift && (
          <div className="small">
            <b>Reconciliation:</b> {drift.in_sync} in sync · {drift.missing.length} missing in BattlEye · {drift.stale.length} stale (ours, revoked here) · {drift.foreign.length} created by others (never touched)
          </div>
        )}
      </Card>
      <div className="grid">
        <Card title="External lists (review only)">
          <p className="small warn">External listings are evidence, not proof. They never kick or ban automatically.</p>
          {!lists.data?.length ? <Empty>No lists imported.</Empty> : (
            <ul>{lists.data.map((l) => <li key={l.id}><b>{l.name}</b> · {l.entries} entries · imported {fmtTime(l.imported_at)} <Badge state={l.status === 'stale' ? 'warn' : 'info'}>{l.status}</Badge></li>)}</ul>
          )}
          <div className="form">
            <label>List name (re-importing replaces it) <input value={imp.name} onChange={(e) => setImp({ ...imp, name: e.target.value })} /></label>
            <label>Source / provenance <input value={imp.source} onChange={(e) => setImp({ ...imp, source: e.target.value })} /></label>
            <label>Format <select value={imp.format} onChange={(e) => setImp({ ...imp, format: e.target.value })}><option>csv</option><option>json</option></select></label>
            <label>Content <textarea rows={5} placeholder="identifier,identifier_type,reason,listed_at" value={imp.content} onChange={(e) => setImp({ ...imp, content: e.target.value })} /></label>
          </div>
          <div className="row end"><button disabled={!imp.name || !imp.content} onClick={doImport}>Import</button></div>
        </Card>
        <Card title="Review queue">
          {!queue.data?.length ? <Empty>No unreviewed matches among known players.</Empty> : (
            <ul>{queue.data.map((q, i) => <li key={i}><a href={'#/players/' + q.steam64}>{q.name || q.steam64}</a> — {q.list} ({q.identifier_type}) {q.reason}</li>)}</ul>
          )}
        </Card>
      </div>
      {revoke && <Confirm title={`Revoke ban #${revoke.id}?`} body={<p>{revoke.name || revoke.be_guid}: {revoke.reason}</p>} onConfirm={doRevoke} onClose={() => setRevoke(null)} />}
    </>
  )
}
