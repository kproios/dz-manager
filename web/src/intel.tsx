import { useMemo } from 'react'
import { Intel } from './api'
import { useApi } from './hooks'
import { Badge } from './ui'
import { Icon } from './icons'

/** Cached intel for a set of players (fetched in one call, refreshed on the 'intel' stream event). */
export function useIntel(ids: (string | undefined)[]) {
  const key = useMemo(() => [...new Set(ids.filter((x): x is string => !!x && /^\d{17}$/.test(x)))].sort().join(','), [ids.join(',')])
  const r = useApi<Record<string, Intel>>(key ? '/api/intel?ids=' + key : null, ['intel'])
  return r.data ?? {}
}

export function Avatar({ intel, name, size = 28 }: { intel?: Intel; name?: string; size?: number }) {
  const initials = (name || intel?.persona || '?').replace(/[^\p{L}\p{N}]/gu, '').slice(0, 2).toUpperCase() || '?'
  if (intel?.avatar) return <img className="avatar" src={intel.avatar} alt="" width={size} height={size} loading="lazy" referrerPolicy="no-referrer" />
  return <span className="avatar placeholder" style={{ width: size, height: size, fontSize: size * 0.38 }} aria-hidden="true">{initials}</span>
}

export function Flag({ intel }: { intel?: Intel }) {
  const cc = intel?.country_code?.toLowerCase()
  if (!cc || !/^[a-z]{2}$/.test(cc)) return null
  return <img className="flag" src={`https://flagcdn.com/20x15/${cc}.png`} width={20} height={15} alt={intel?.country || cc} title={`${intel?.country || cc.toUpperCase()}${intel?.isp ? ' · ' + intel.isp : ''}`} loading="lazy" referrerPolicy="no-referrer" />
}

export interface Risk { level: 'bad' | 'warn' | 'info'; label: string; tip: string }

/** Every signal worth showing, worst first. Wording explains what the badge means. */
export function risks(i?: Intel): Risk[] {
  if (!i) return []
  const out: Risk[] = []
  const ago = i.days_since_ban != null ? ` Last ban ${i.days_since_ban} day(s) ago.` : ''
  if (i.vac_banned || (i.vac_bans ?? 0) > 0) out.push({ level: 'bad', label: i.vac_bans ? `VAC ×${i.vac_bans}` : 'VAC ban', tip: `Valve Anti-Cheat ban on this Steam account (it may be for another game).${ago}` })
  if ((i.game_bans ?? 0) > 0) out.push({ level: 'bad', label: `Game ban ×${i.game_bans}`, tip: `A game developer or its anti-cheat banned this Steam account in some game and reported it to Steam.${ago}` })
  if (i.community_banned) out.push({ level: 'bad', label: 'Community ban', tip: 'Banned from Steam Community features by Valve (often for scams or abuse).' })
  if (i.trade_ban && i.trade_ban !== 'None' && i.trade_ban !== 'none') out.push({ level: 'warn', label: 'Trade ban', tip: `Steam trade/market restriction: ${i.trade_ban}.` })
  if (i.proxy) out.push({ level: 'warn', label: 'VPN / proxy', tip: 'The current IP is a known VPN or proxy exit. Often used to dodge IP bans or hide location.' })
  else if (i.hosting) out.push({ level: 'warn', label: 'Data center IP', tip: 'The current IP belongs to a hosting / data-center network, which usually means a VPN.' })
  if (i.limited) out.push({ level: 'info', label: 'Limited account', tip: 'Limited Steam account (has never spent $5). Common for throwaway alt accounts.' })
  if (i.profile_private) out.push({ level: 'info', label: 'Private profile', tip: 'The Steam profile is private, so fewer details are visible.' })
  return out
}

export function RiskBadges({ intel }: { intel?: Intel }) {
  const r = risks(intel)
  if (!intel?.steam_checked_at && !intel?.ip_checked_at) return <span className="muted small">not checked yet</span>
  if (!r.length) return <Badge state="ok" title="No Steam bans, no VPN detected on the current IP">clean</Badge>
  return <span className="row wrap" style={{ gap: 4 }}>{r.map((x) => <Badge key={x.label} state={x.level} title={x.tip}>{x.label}</Badge>)}</span>
}

/** One compact icon for lists: red shield for bans, amber for VPN, nothing when clean. */
export function RiskIcon({ intel }: { intel?: Intel }) {
  const r = risks(intel).filter((x) => x.level !== 'info')
  if (!r.length) return null
  const bad = r.some((x) => x.level === 'bad')
  return <span className={'risk-icon ' + (bad ? 'bad' : 'warn')} title={r.map((x) => `${x.label}: ${x.tip}`).join('\n')} aria-label={r.map((x) => x.label).join(', ')}><Icon name={bad ? 'bans' : 'shield'} /></span>
}
