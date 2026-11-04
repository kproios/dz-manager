import { ReactNode, useEffect, useState } from 'react'
import { Cap } from './api'
import { Icon } from './icons'

export function Card({ title, children, actions, className }: { title?: ReactNode; children: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <section className={'card' + (className ? ' ' + className : '')}>
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

export function PageHead({ title, sub, actions }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="page-head">
      <h1>{title}</h1>
      {sub && <span className="sub">{sub}</span>}
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

const stateClass: Record<string, string> = {
  ok: 'ok', stale: 'warn', degraded: 'warn', unverified: 'info', unconfigured: 'muted', unsupported: 'muted',
  unreachable: 'bad', auth_failed: 'bad', misconfigured: 'bad', active: 'ok', ended: 'muted', expired: 'muted',
  historical: 'info', revoked: 'muted', error: 'bad', denied: 'bad', info: 'info', warn: 'warn', bad: 'bad', muted: 'muted',
}
const stateLabel: Record<string, string> = { ok: 'OK', auth_failed: 'auth failed', unverified: 'unverified', unconfigured: 'not set up' }

export function Badge({ state, children, title }: { state: string; children?: ReactNode; title?: string }) {
  return <span className={'badge ' + (stateClass[state] || 'muted')} title={title}>{children ?? stateLabel[state] ?? state.replaceAll('_', ' ')}</span>
}

const capLabels: Record<string, string> = {
  rcon: 'RCON', a2s: 'Server query (A2S)', metricz: 'MetricZ telemetry', bridge: '@DZMBridge', 'bridge.commands': 'Bridge commands',
  'provider.schedule': 'Restart schedule', 'restart.drive': 'Restart control',
}
export function CapList({ caps }: { caps: Cap[] }) {
  return (
    <table className="caps">
      <tbody>
        {caps.map((c) => (
          <tr key={c.name}>
            <td title={c.name}>{capLabels[c.name] || c.name}</td>
            <td><Badge state={c.state} /></td>
            <td className="muted small"><span className="truncate cell-max" title={c.reason}>{c.reason}</span></td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function ErrorBox({ error }: { error?: string | null }) {
  if (!error) return null
  return <div className="error" role="alert">{error}</div>
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

/** Truncated text that reveals the full value on hover. */
export function Trunc({ text, max = 260, className = '' }: { text?: string | null; max?: number; className?: string }) {
  if (!text) return <span className="muted">—</span>
  return <span className={'truncate ' + className} style={{ maxWidth: max }} title={text}>{text}</span>
}

/** Subtle copy-to-clipboard button. */
export function CopyButton({ value, label = 'Copy' }: { value: string; label?: string }) {
  const [done, setDone] = useState(false)
  useEffect(() => { if (!done) return; const t = window.setTimeout(() => setDone(false), 1400); return () => window.clearTimeout(t) }, [done])
  const copy = async (e: React.MouseEvent) => {
    e.preventDefault(); e.stopPropagation()
    try { await navigator.clipboard.writeText(value); setDone(true) } catch { /* clipboard blocked: the value stays selectable */ }
  }
  return (
    <button type="button" className={'copy' + (done ? ' done' : '')} onClick={copy} title={done ? 'Copied' : `${label}: ${value}`} aria-label={`${label} ${value}`}>
      <Icon name={done ? 'check' : 'copy'} />
    </button>
  )
}

export function steamUrl(steam64: string) { return 'https://steamcommunity.com/profiles/' + encodeURIComponent(steam64) }

/** A Steam64 shown as a link to the public Steam profile, with a copy button. */
export function SteamId({ id, short = false }: { id: string; short?: boolean }) {
  if (!id) return <span className="muted">—</span>
  return (
    <span className="id-line">
      <a href={steamUrl(id)} target="_blank" rel="noreferrer noopener" title="Open Steam profile in a new tab"><code>{short ? id.slice(0, 7) + '…' + id.slice(-4) : id}</code></a>
      <CopyButton value={id} label="Copy Steam64" />
    </span>
  )
}

/** Any long identifier with a copy button; shortened in the cell, full on hover. */
export function IdValue({ value, label, short = false }: { value?: string; label: string; short?: boolean }) {
  if (!value) return <span className="muted">—</span>
  return (
    <span className="id-line">
      <code className="truncate" style={{ maxWidth: short ? 120 : 280 }} title={value}>{short ? value.slice(0, 8) + '…' : value}</code>
      <CopyButton value={value} label={'Copy ' + label} />
    </span>
  )
}

/** Client-side pagination for in-memory lists. */
export function usePaged<T>(items: T[] | null | undefined, size = 10) {
  const [page, setPage] = useState(1)
  const all = items ?? []
  const pages = Math.max(1, Math.ceil(all.length / size))
  const cur = Math.min(page, pages)
  return { rows: all.slice((cur - 1) * size, cur * size), page: cur, pages, total: all.length, setPage }
}

export function Pager({ page, pages, total, setPage, noun = 'rows' }: { page: number; pages: number; total: number; setPage: (n: number) => void; noun?: string }) {
  if (total === 0 || pages <= 1) return null
  return (
    <nav className="row pager" aria-label="Pages">
      <span>{total} {noun}</span>
      <button className="small" disabled={page <= 1} onClick={() => setPage(1)}>First</button>
      <button className="small" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</button>
      <span aria-live="polite">Page {page} of {pages}</span>
      <button className="small" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
    </nav>
  )
}

/** Segmented single-choice control for small fixed option sets. */
export function Seg<T extends string | number>({ value, options, onChange, label }: { value: T; options: [T, string][]; onChange: (v: T) => void; label: string }) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map(([v, l]) => <button type="button" key={String(v)} aria-pressed={v === value} onClick={() => onChange(v)}>{l}</button>)}
    </div>
  )
}

export function StatTile({ label, value, unit, foot, children, title }: { label: ReactNode; value: ReactNode; unit?: string; foot?: ReactNode; children?: ReactNode; title?: string }) {
  return (
    <div className="tile" title={title}>
      <div className="label">{label}</div>
      <div className="value">{value}{unit && <small>{unit}</small>}</div>
      {children}
      <div className="foot">{foot}</div>
    </div>
  )
}

export function Meter({ value, color }: { value: number | null | undefined; color?: string }) {
  const v = value == null || !isFinite(value) ? 0 : Math.max(0, Math.min(1, value))
  return <div className="meter" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(v * 100)}><i style={{ width: `${v * 100}%`, background: color }} /></div>
}

/** Confirm dialog requiring an optional typed word. Shows target + exact parameters. */
export function Confirm({ title, body, word, onConfirm, onClose, busy, disabled, confirmLabel = 'Confirm', tone = 'danger' }: {
  title: string; body: ReactNode; word?: string; onConfirm: (typed: string) => void; onClose: () => void; busy?: boolean; disabled?: boolean; confirmLabel?: string; tone?: 'danger' | 'primary'
}) {
  const [typed, setTyped] = useState('')
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [onClose])
  return (
    <div className="modal-bg" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={title}>
        <h3>{title}</h3>
        <div className="modal-body">{body}</div>
        {word && (
          <label className="field">
            <span>Type <b>{word}</b> to confirm</span>
            <input autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} />
          </label>
        )}
        <div className="row end">
          <button onClick={onClose}>Cancel</button>
          <button className={tone} disabled={busy || disabled || (!!word && typed !== word)} onClick={() => onConfirm(typed)}>
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

export function Toast({ msg, kind, onClose }: { msg: string; kind: 'ok' | 'bad'; onClose: () => void }) {
  return (
    <div className={'toast ' + kind} onClick={onClose} role="status">
      {msg}
    </div>
  )
}
