import { describe,it,expect } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { LogDetails,LogPager,readable } from './logs'

describe('readable administration history',()=>{
 it('renders restart settings as labelled fields instead of JSON',()=>{
  const html=renderToStaticMarkup(<LogDetails params={{server_id:'private-id',enabled:true,times:['00:00','04:20'],timezone:'Europe/Helsinki',mode:'drive',announce:false,dry_run:true}}/>)
  expect(readable('restart.schedule_update')).toBe('Restart schedule updated')
  for(const text of ['Restart times','00:00, 04:20','Europe/Helsinki','Simulation only','Yes','Countdown announcements','No']) expect(html).toContain(text)
  expect(html).not.toContain('private-id');expect(html).not.toContain('&quot;times&quot;')
 })
 it('keeps older-page navigation bounded',()=>{
  const html=renderToStaticMarkup(<LogPager page={1} loading={false} onLatest={()=>{}} onNewer={()=>{}} onOlder={()=>{}}/>)
  expect(html).toContain('Page 1');expect((html.match(/disabled/g)||[]).length).toBe(3)
 })
})
