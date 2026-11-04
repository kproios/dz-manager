import { ReactNode, useState } from 'react'
import { Cap } from './api'

export function Card({ title, children, actions }: { title?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="card">
      {(title || actions) && (
        <header>
          <h2>{title}</h2>
          <div className="actions">{actions}</div>
        </header>
      )}
      {children}
    </section>
  )
}

const stateClass: Record<string, string> = {
  ok: 'ok', stale: 'warn', degraded: 'warn', unverified: 'info', unconfigured: 'muted', unsupported: 'muted',
  unreachable: 'bad', auth_failed: 'bad', misconfigured: 'bad', active: 'ok', ended: 'muted', expired: 'muted',
  historical: 'info', revoked: 'muted', error: 'bad', denied: 'bad',
}

export function Badge({ state, children }: { state: string; children?: ReactNode }) {
  return <span className={'badge ' + (stateClass[state] || 'muted')}>{children ?? state.replace('_', ' ')}</span>
}

export function CapList({ caps }: { caps: Cap[] }) {
  return (
    <table className="caps">
      <tbody>
        {caps.map((c) => (
          <tr key={c.name}>
            <td><code>{c.name}</code></td>
            <td><Badge state={c.state} /></td>
            <td className="muted small">{c.reason}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function ErrorBox({ error }: { error?: string | null }) {
  if (!error) return null
  return <div className="error">{error}</div>
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

/** Confirm dialog requiring an optional typed word. Shows target + exact parameters. */
export function Confirm({ title, body, word, onConfirm, onClose, busy }: {
  title: string; body: ReactNode; word?: string; onConfirm: (typed: string) => void; onClose: () => void; busy?: boolean
}) {
  const [typed, setTyped] = useState('')
  return (
    <div className="modal-bg" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <h3>{title}</h3>
        <div className="modal-body">{body}</div>
        {word && (
          <label>
            Type <b>{word}</b> to confirm
            <input autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} />
          </label>
        )}
        <div className="row end">
          <button onClick={onClose}>Cancel</button>
          <button className="danger" disabled={busy || (!!word && typed !== word)} onClick={() => onConfirm(typed)}>
            {busy ? 'Working…' : 'Confirm'}
          </button>
        </div>
      </div>
    </div>
  )
}

export function Toast({ msg, kind, onClose }: { msg: string; kind: 'ok' | 'bad'; onClose: () => void }) {
  return (
    <div className={'toast ' + kind} onClick={onClose}>
      {msg}
    </div>
  )
}
