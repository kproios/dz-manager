import { createContext, useContext, useEffect, useRef, useState } from 'react'
import { api, ApiError, Me, Permission } from './api'
import { ErrorBox } from './ui'

export const MeCtx = createContext<Me | null>(null)
export const useMe = () => useContext(MeCtx)

/** Whether the signed-in user's role allows a permission. The server checks
 * every request; this only hides what would be refused anyway. */
export function useCan() {
  const me = useMe()
  return (p: Permission) => !!me?.permissions?.includes(p)
}

export const roleLabels: Record<string, string> = { owner: 'Owner', admin: 'Admin', moderator: 'Moderator', viewer: 'Viewer' }
export const methodLabels: Record<string, string> = { password: 'Password', steam: 'Steam', oidc: 'Single sign-on' }

/** A 6-digit code field (or a recovery code when the user switches). */
export function CodeField({ value, onChange, recovery, autoFocus = true }: { value: string; onChange: (v: string) => void; recovery?: boolean; autoFocus?: boolean }) {
  return recovery ? (
    <label className="field"><span>Recovery code</span>
      <input autoFocus={autoFocus} value={value} onChange={(e) => onChange(e.target.value)} placeholder="xxxx-xxxx-xxxx" autoComplete="off" spellCheck={false} />
    </label>
  ) : (
    <label className="field"><span>Code from your authenticator app</span>
      <input autoFocus={autoFocus} value={value} onChange={(e) => onChange(e.target.value.replace(/[^0-9]/g, '').slice(0, 6))}
        inputMode="numeric" autoComplete="one-time-code" placeholder="123456" className="code-input" />
    </label>
  )
}

/** Asks for a 2FA code before a sensitive action (the server said step_up). */
export function StepUpDialog({ onDone }: { onDone: (ok: boolean) => void }) {
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const done = useRef(onDone)
  done.current = onDone
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') done.current(false) }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [])
  const ready = recovery ? code.trim().length >= 12 : code.length === 6
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ready) return
    setBusy(true); setErr(null)
    try {
      await api.post('/api/account/confirm', recovery ? { recovery: code } : { code })
      onDone(true)
    } catch (x) {
      setErr(x instanceof ApiError ? x.message : 'Could not confirm')
      setBusy(false)
    }
  }
  return (
    <div className="modal-bg" onClick={() => onDone(false)}>
      <form className="modal" onClick={(e) => e.stopPropagation()} onSubmit={submit} role="dialog" aria-modal="true" aria-label="Confirm with 2FA">
        <h3>Confirm it’s you</h3>
        <p className="muted small">This action needs a fresh 2FA code. It stays unlocked for 10 minutes.</p>
        <ErrorBox error={err} />
        <CodeField value={code} onChange={setCode} recovery={recovery} />
        <button type="button" className="link small" onClick={() => { setRecovery(!recovery); setCode('') }}>
          {recovery ? 'Use the authenticator app instead' : 'Use a recovery code instead'}
        </button>
        <div className="row end">
          <button type="button" onClick={() => onDone(false)}>Cancel</button>
          <button className="primary" disabled={busy || !ready}>{busy ? 'Checking…' : 'Confirm'}</button>
        </div>
      </form>
    </div>
  )
}

/** Shows freshly made recovery codes once, with copy and download. */
export function RecoveryCodes({ codes }: { codes: string[] }) {
  const text = codes.join('\n')
  const download = () => {
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([`DZ Manager recovery codes (each works once)\n\n${text}\n`], { type: 'text/plain' }))
    a.download = 'dz-manager-recovery-codes.txt'
    a.click()
    URL.revokeObjectURL(a.href)
  }
  return (
    <div className="recovery">
      <p className="small">Keep these somewhere safe. Each one works once, instead of a code from your phone. You won’t see them again.</p>
      <ol className="recovery-codes">{codes.map((c) => <li key={c}><code>{c}</code></li>)}</ol>
      <div className="row">
        <button type="button" onClick={() => navigator.clipboard?.writeText(text)}>Copy</button>
        <button type="button" onClick={download}>Download .txt</button>
      </div>
    </div>
  )
}
