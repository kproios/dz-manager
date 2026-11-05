import type { Marker, MzVehicle } from './api'

export type MapIcon = 'heli'|'plane'|'convoy'|'police'|'car'|'bike'|'boat'|'item'|'weapon'|'flag'|'airdrop'|'train'
// Original, fixed SVG paths. Server-provided names are never interpolated into HTML.
const paths:Record<MapIcon,string>={
 heli:'<path d="M3 3h16M11 3v4M5 9h10l4 5v3H8l-3-4H2V9h3ZM11 9v5h7M8 17v3m8-3v3M6 20h13M20 6v7m-2-5 4 3"/>',
 plane:'<path d="m12 2 2 8 8 5v2l-8-2v5l3 2v1l-5-1-5 1v-1l3-2v-5l-8 2v-2l8-5 2-8Z"/>',
 convoy:'<path d="M2 6h12v11H2ZM14 10h4l4 4v3h-8M16 10v4h6"/><circle cx="6" cy="18" r="2"/><circle cx="18" cy="18" r="2"/><path d="M4 3h9m3 1h4"/>',
 police:'<path d="m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6l8-4Z"/><path d="m12 7 1.5 3 3.5.5-2.5 2.5.5 3.5-3-1.6L9 16.5l.5-3.5L7 10.5l3.5-.5Z"/>',
 car:'<path d="m4 10 2-6h12l2 6M3 10h18v9H3ZM6 19v2m12-2v2M3 14h4m10 0h4M8 4v6m8-6v6"/>',
 bike:'<circle cx="5" cy="17" r="4"/><circle cx="19" cy="17" r="4"/><path d="m5 17 5-8 5 8H5m5-8h6m-1-4h3l2 12M8 6h4"/>',
 boat:'<path d="m2 15 10 5 10-5-3-5H5l-3 5Zm10 5V3m0 1 6 5h-6M2 22l3-1 4 1 3-1 4 1 3-1 3 1"/>',
 item:'<path d="m3 7 9-5 9 5v11l-9 5-9-5V7Zm0 0 9 5 9-5m-9 5v11M7 4l10 6"/>',
 weapon:'<path d="m2 13 5-4h10V7h5v4h-7v4h-4l-2 6H6l2-6H5l-2 3H1l1-5Zm9 2 1 4h3v-4M7 9V6h5m3 3V6"/>',
 flag:'<path d="M5 23V2m0 1c5-4 9 4 15 0v10c-6 4-10-4-15 0M2 23h7"/>',
 airdrop:'<path d="M2 10a10 10 0 0 1 20 0H2Zm0 0 7 7m13-7-7 7M8 10l2 7m6-7-2 7M8 17h8v6H8Z"/>',
 train:'<path d="M5 3h14v14H5ZM5 10h14M9 3v7m6-7v7M7 17l-3 6m13-6 3 6M5 21h14"/><circle cx="9" cy="14" r="1"/><circle cx="15" cy="14" r="1"/>',
}
export const iconLabels:Record<MapIcon,string>={heli:'Helicrash',plane:'Aircraft crash',convoy:'Convoy',police:'Police',car:'Car',bike:'Bike',boat:'Boat',item:'Item',weapon:'Weapon',flag:'Base flag',airdrop:'Airdrop',train:'Train'}
export function iconSvg(kind:MapIcon):string {return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[kind]}</svg>`}
export interface VehicleRule { match:string; kind:MapIcon }
export const vehicleKinds:MapIcon[]=['car','bike','boat','heli','convoy','plane','police','train']
/** Owner rules (classname contains `match`, case-insensitive) win over the built-in guesses. */
export function vehicleIcon(v:Pick<MzVehicle,'class'|'type'>,rules:VehicleRule[]=[]):MapIcon {
 const cls=(v.class||'').toLowerCase()
 for(const r of rules) if(r.match&&cls.includes(r.match.toLowerCase())&&vehicleKinds.includes(r.kind))return r.kind
 const type=((v.type||'')+' '+v.class).toLowerCase()
 if(/motorbike|motorcycle|bicycle|bike|moped|scooter|quad|atv|dirtbike/.test(type))return 'bike'
 if(/boat|zodiac|jetski|rhib|kayak|raft|ship|watercraft/.test(type))return 'boat'
 if(/helicopter|heli/.test(type))return 'heli'
 if(/truck|v3s|ural/.test(type))return 'convoy'
 return 'car'
}
export function markerIcon(m:Pick<Marker,'category'|'kind'>):MapIcon {
 const category:Record<string,MapIcon>={heli_crash:'heli',plane_crash:'plane',military_convoy:'convoy',police:'police',train:'train',airdrop:'airdrop',loot_box:'item',base_flag:'flag'}
 return category[m.category] || (m.kind==='weapon'?'weapon':'item')
}
export function markerTitle(m:Pick<Marker,'name'|'class'|'category'>):string {return m.name?.trim() || m.class || m.category.replaceAll('_',' ')}

export interface MarkerSite { marker:Marker; members:Marker[] }
// Nearby parts of one scene remain individually inspectable, without stacked hit targets.
export function groupMarkers(markers:Marker[]):MarkerSite[] {
 const sites:MarkerSite[]=[]
 const groupable=new Set(['heli_crash','plane_crash','military_convoy','police','train','loot_box','airdrop'])
 for(const marker of markers) {
  const site=groupable.has(marker.category)?sites.find(s=>s.marker.category===marker.category&&s.marker.status===marker.status&&Math.hypot(s.marker.x-marker.x,s.marker.z-marker.z)<80):undefined
  if(site)site.members.push(marker)
  else sites.push({marker,members:[marker]})
 }
 return sites
}
