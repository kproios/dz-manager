// Small original line icons (24px grid, currentColor). No third-party icon set.
const paths = {
  dashboard: 'M3 13h8V3H3Zm10 8h8V11h-8ZM3 21h8v-6H3Zm10-18v6h8V3Z',
  map: 'm9 4-6 2v14l6-2 6 2 6-2V4l-6 2-6-2Zm0 0v14m6-12v14',
  players: 'M16 19v-1a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v1M9 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm13 9v-1a4 4 0 0 0-3-3.9M16 4.1a3 3 0 0 1 0 5.8',
  events: 'M22 12h-4l-3 8L9 4l-3 8H2',
  restarts: 'M21 12a9 9 0 1 1-2.6-6.4M21 4v5h-5',
  bans: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20ZM5 5l14 14',
  settings: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm7.4-3a7.4 7.4 0 0 0-.1-1.2l2-1.6-2-3.4-2.4 1a7 7 0 0 0-2-1.2L14.5 3h-5l-.4 2.6a7 7 0 0 0-2 1.2l-2.4-1-2 3.4 2 1.6a7.4 7.4 0 0 0 0 2.4l-2 1.6 2 3.4 2.4-1a7 7 0 0 0 2 1.2l.4 2.6h5l.4-2.6a7 7 0 0 0 2-1.2l2.4 1 2-3.4-2-1.6c.1-.4.1-.8.1-1.2Z',
  audit: 'M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2M9 5a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2M9 5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2m-6 7h6m-6 4h4',
  copy: 'M9 9h11v11H9ZM5 15H4V4h11v1',
  check: 'M4 12l5 5L20 6',
  external: 'M14 4h6v6m0-6-9 9M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5',
  message: 'M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2Z',
  heal: 'M12 21s-8-5-8-11a5 5 0 0 1 8-4 5 5 0 0 1 8 4c0 6-8 11-8 11Zm0-12v6m-3-3h6',
  teleport: 'M12 22s7-6.2 7-12a7 7 0 1 0-14 0c0 5.8 7 12 7 12Zm0-9a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z',
  target: 'M12 2v4m0 12v4M2 12h4m12 0h4M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10Z',
  gift: 'M20 12v9H4v-9M2 7h20v5H2Zm10 14V7m0 0H7.5a2.5 2.5 0 1 1 0-5C11 2 12 7 12 7Zm0 0h4.5a2.5 2.5 0 1 0 0-5C13 2 12 7 12 7Z',
  kick: 'M16 17l5-5-5-5M21 12H9m4 9H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8',
  skull: 'M12 2a8 8 0 0 0-8 8c0 3 1.5 5 3 6v3h10v-3c1.5-1 3-3 3-6a8 8 0 0 0-8-8ZM9 12h.01M15 12h.01M10 19v2m4-2v2',
  strip: 'M3 6h18M8 6V4h8v2m-9 0 1 14h8l1-14M10 10v6m4-6v6',
  profile: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2m8-10a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z',
  lock: 'M5 11h14v10H5Zm3 0V7a4 4 0 0 1 8 0v4',
  unlock: 'M5 11h14v10H5Zm3 0V7a4 4 0 0 1 7.8-1.2',
  power: 'M12 2v10M18.4 6.6a9 9 0 1 1-12.8 0',
  close: 'M6 6l12 12M18 6 6 18',
  clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Zm0-15v5l3 3',
  megaphone: 'M3 11v2a1 1 0 0 0 1 1h3l5 4V6L7 10H4a1 1 0 0 0-1 1Zm13-3a5 5 0 0 1 0 8',
  plus: 'M12 5v14M5 12h14',
  logo: 'M12 2 3 7v10l9 5 9-5V7Zm0 0v20M3 7l9 5 9-5',
  shield: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Zm0-14v4m0 4h.01',
  live: 'M12 13a1 1 0 1 0 0-2 1 1 0 0 0 0 2Zm4.2-5.2a6 6 0 0 1 0 8.4M7.8 16.2a6 6 0 0 1 0-8.4m11.3-2.8a10 10 0 0 1 0 14M4.9 19a10 10 0 0 1 0-14',
  send: 'M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z',
  search: 'M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm10 2-4.3-4.3',
  refresh: 'M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5',
  eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Zm10 3a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z',
  sort: 'M7 4v16m0 0-3-3m3 3 3-3M17 20V4m0 0-3 3m3-3 3 3',
  chat: 'M21 12a8 8 0 0 1-11.8 7L3 21l2-5.5A8 8 0 1 1 21 12Z',
}
export type IconName = keyof typeof paths
export function Icon({ name, title }: { name: IconName; title?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden={title ? undefined : true} role={title ? 'img' : undefined}>
      {title && <title>{title}</title>}
      <path d={paths[name]} />
    </svg>
  )
}
