import { describe, expect, it } from 'vitest'
import { groupMarkers, markerIcon, markerTitle, vehicleIcon } from './mapIcons'
import type { Marker } from './api'
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
