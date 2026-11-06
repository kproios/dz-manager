import { describe, expect, it } from 'vitest'
import { closestVehicle, groupMarkers, markerIcon, markerTitle, vehicleIcon } from './mapIcons'
import { applyLive, isFresh, LiveDelta } from './liveMap'
import { syncMarkers, MarkerStore, SyncItem, SyncMarker } from './mapSync'
import type { MapState, Marker, MzVehicle } from './api'
const marker=(id:number,category='custom',x=100):Marker=>({id,key:String(id),category,class:'PunchedCard',x,y:0,z:100,status:'active',started_at:'2026-11-04T18:00:00Z',source:'DZMBridge'})
describe('map identity and icons',()=>{
 it('uses item identity instead of its custom category',()=>{
  expect(markerTitle(marker(1))).toBe('PunchedCard')
  expect(markerTitle({...marker(1),name:'Access card'})).toBe('Access card')
  expect(markerIcon({...marker(1),kind:'weapon'})).toBe('weapon')
  expect(vehicleIcon({class:'ExpansionMotorbike',type:''})).toBe('bike')
 })
 it('groups scene parts but keeps separate ground items and bases',()=>{
  expect(groupMarkers([marker(1,'police'),marker(2,'police',120),marker(3,'police',500)]).map(s=>s.members.length)).toEqual([2,1])
  expect(groupMarkers([marker(1,'tracked_item'),marker(2,'tracked_item')])).toHaveLength(2)
  expect(groupMarkers([marker(1,'base_flag'),marker(2,'base_flag')])).toHaveLength(2)
 })
})

describe('vehicle rules',()=>{
 const boat={class:'Boat_01_Black',type:'boat'}
 it('applies a name-part rule to the exact vehicle, ignoring case',()=>{
  expect(vehicleIcon({class:'Sedan_02',type:'car'},[{match:'sedan_02',kind:'police'}])).toBe('police')
  expect(vehicleIcon(boat,[{match:'Boat_01',kind:'truck'}])).toBe('truck')
 })
 it('lets the most specific rule win whatever the order',()=>{
  const a=[{match:'Boat_01',kind:'truck' as const},{match:'Boat_01_Black',kind:'police' as const}]
  expect(vehicleIcon(boat,a)).toBe('police')
  expect(vehicleIcon(boat,[...a].reverse())).toBe('police')
  expect(vehicleIcon({class:'Boat_01_Blue',type:'boat'},a)).toBe('truck')
 })
 it('uses the type the bridge reports for vehicles with unhelpful names',()=>{
  expect(vehicleIcon({class:'M1025',type:'motorbike'})).toBe('bike')
  expect(vehicleIcon({class:'Zz',type:'helicopter'})).toBe('heli')
 })
 it('catches the typo that made a rule silently match nothing',()=>{
  expect(closestVehicle('Motorbikes_01',[])).toBe('Motorbike_01')
  expect(closestVehicle('Boat_02',['Boat_01_Black'])?.startsWith('Boat_01')).toBe(true)
  expect(closestVehicle('Boat_01',['Boat_01_Black'])).toBe(undefined) // it already matches
  expect(closestVehicle('qqqqqqqq',[])).toBe(undefined)
 })
})

describe('live map data',()=>{
 const veh=(id:string,x=1):MzVehicle=>({id,class:'Sedan_02',type:'car',x,y:0,z:1})
 const state=(source:'bridge'|'metricz',vehicles:MzVehicle[]):MapState=>({server_id:'s',available:true,fresh:true,source,players:[],vehicles,perf:{vehicles:vehicles.length}})
 const delta=(o:Partial<LiveDelta>):LiveDelta=>({server_id:'s',received_at:'2026-11-06T00:00:00Z',interval_s:2,players:[],vehicles_ready:true,full:false,vehicles:[],gone:[],...o})
 it('moves, adds and removes vehicles from a partial push',()=>{
  const next=applyLive(state('bridge',[veh('a'),veh('b')]),delta({vehicles:[veh('a',9),veh('c')],gone:['b']}))!
  expect(next.vehicles!.map(v=>[v.id,v.x]).sort()).toEqual([['a',9],['c',1]])
  expect(next.perf!.vehicles).toBe(2)
 })
 it('replaces everything on a full push, so nothing can linger',()=>{
  expect(applyLive(state('bridge',[veh('a'),veh('b')]),delta({full:true,vehicles:[veh('z')]}))!.vehicles!.map(v=>v.id)).toEqual(['z'])
 })
 it('never mixes MetricZ vehicles with bridge vehicles',()=>{
  expect(applyLive(state('metricz',[veh('hash1')]),delta({vehicles:[veh('1:2')]}))!.vehicles!.map(v=>v.id)).toEqual(['hash1'])
  expect(applyLive(state('metricz',[veh('hash1')]),delta({vehicles_ready:false,full:true,vehicles:[veh('1:2')]}))!.vehicles!.map(v=>v.id)).toEqual(['hash1'])
  expect(applyLive(state('metricz',[veh('hash1')]),delta({full:true,vehicles:[veh('1:2')]}))!.vehicles!.map(v=>v.id)).toEqual(['1:2'])
 })
 it('turns stale when pushes stop, sooner for the fast source',()=>{
  const live=state('bridge',[])
  expect(isFresh(live,1000,1000+19000)).toBe(true)
  expect(isFresh(live,1000,1000+21000)).toBe(false)
  expect(isFresh(state('metricz',[]),1000,1000+40000)).toBe(true)
 })
})

describe('marker sync',()=>{
 class Fake implements SyncMarker {
  calls:string[]=[];removed=false;click?:()=>void
  setLatLng(){this.calls.push('move')}
  setIcon(){this.calls.push('icon')}
  setZIndexOffset(){this.calls.push('z')}
  bindTooltip(){this.calls.push('tip')}
  unbindTooltip(){}
  on(_:string,fn:()=>void){this.click=fn}
  addTo(){this.calls.push('add')}
  remove(){this.removed=true}
 }
 const item=(o:Partial<SyncItem>={}):SyncItem=>({key:'k',latlng:[1,2],look:'car',icon:()=>null,title:'t',tooltip:()=>null,tip:'t',tipOptions:{},group:'g',onClick:()=>{},...o})
 const run=()=>{const store:MarkerStore=new Map();const made:Fake[]=[];return {store,made,sync:(items:SyncItem[])=>syncMarkers(store,items,()=>{const f=new Fake();made.push(f);return f})}}
 it('moves a marker in place instead of rebuilding it',()=>{
  const t=run()
  t.sync([item()]);t.made[0].calls=[]
  t.sync([item({latlng:[3,4]})])
  expect(t.made).toHaveLength(1)
  expect(t.made[0].calls).toEqual(['move'])
 })
 it('does nothing at all when nothing changed',()=>{
  const t=run()
  t.sync([item()]);t.made[0].calls=[]
  t.sync([item()])
  expect(t.made[0].calls).toEqual([])
 })
 it('changes the icon as soon as a rule changes what the vehicle is',()=>{
  const t=run()
  t.sync([item()]);t.made[0].calls=[]
  t.sync([item({look:'boat'})])
  expect(t.made[0].calls).toEqual(['icon'])
 })
 it('removes markers that are no longer in the data',()=>{
  const t=run()
  t.sync([item()])
  t.sync([])
  expect(t.made[0].removed).toBe(true)
  expect(t.store.size).toBe(0)
 })
 it('clicks act on the newest data',()=>{
  const t=run();const seen:number[]=[]
  t.sync([item({onClick:()=>seen.push(1)})])
  t.sync([item({onClick:()=>seen.push(2)})])
  t.made[0].click!()
  expect(seen).toEqual([2])
 })
})
