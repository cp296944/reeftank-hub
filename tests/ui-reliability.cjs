// Run with Playwright on NODE_PATH; no aquarium/HA connections are made.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const assets=path.resolve(__dirname,'../internal/hubweb/assets');
const stamp='2026-10-01T14:00:00+08:00';
const state=n=>({state:String(n),entity_id:'sensor.fixture'});
const devices=Array.from({length:18},(_,i)=>({id:`outlet_${String(i+1).padStart(2,'0')}`,slot:i+1,display_name:i===8?'珊瑚熊捲棉機':'設備 '+(i+1),switch_entity:'switch.tp_link_power_strip_fixture_'+(i+1)+'_long_entity_name',high_frequency:i===8}));
const power_strips=['3a31','3f2d','bfb9'].map((id,i)=>({id,display_name:id+' 排插',slot_start:i*6+1,slot_end:i*6+6,poll_interval_seconds:2}));
const strips=power_strips.map(s=>({name:s.display_name,power:state(123),current:state(1.2),today:state(.123),month:state(45),outlets:devices.filter(d=>d.slot>=s.slot_start&&d.slot<=s.slot_end).map(d=>({...d,name:d.display_name,voltage:state(117),current:state(.02),power:state(3),today:state(.01),month:state(2),switch:state('on')}))}));
const water={latest:Object.fromEntries(Object.entries({no3:5,po4:.01,ca:420,mg:1290,kh:8,sg:1.025}).map(([k,value])=>[k,{value,time:stamp}])),history:{},temperature_history:[],records:[{id:1,measured_at:stamp,water_change:true,change_liters:40,note:'換水測試',po4:.01}],count:1};
const data={
 '/api/hub/status':{version:'review-build'},'/api/bootstrap/status':{required:false},
 '/api/update/status':{current:'review-build'},'/api/hub/storage/status':{samples:12345,temperature_samples:456,retention_days:0},
 '/api/hub/temperature':{configured:true,value:26.4,source_time:stamp,source:'direct'},
 '/api/hub/settings/temperature':{source:'direct',interval_seconds:60},
 '/api/hub/water':water,'/api/hub/high-frequency':{devices:{},events:{}},
 '/api/hub/equipment':{devices,power_strips},'/api/hub/ha/power':{strips,connected:true,temperature:state(26)},
 '/api/hub/storage/history':[], '/api/hub/thread/status':{configured:true,rcp_present:true,otbr_connected:true,network_name:'Thread fixture',role:'router'},
 '/api/diag':{model:'Raspberry Pi fixture',mem_total_kb:1000000,mem_avail_kb:500000,disk_total_kb:10000000,disk_free_kb:5000000,cpu_percent:20,soc_temp_c:48},
 '/api/hub/dosing':{state:{mode:'simulation',audit:[],heads:Array.from({length:4},(_,i)=>({id:i+1,name:'滴定泵 '+(i+1),calibration_ml_per_min:10,container_ml:500,remaining_ml:400,schedule:{daily_ml:2,doses:4,start:'08:00'}})),calculator:{tank_liters:200,po4_concentration:2.435,no3_concentration:49.06,kh_efficiency:2.6,po4_daily_limit:.02,no3_daily_limit:1,kh_daily_limit:1}}}
};
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://localhost'),p=url.pathname;
 if(p.startsWith('/api/')){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data[p]??{}));return}
 if(p.startsWith('/hub-assets/')){const f=path.basename(p);res.setHeader('Content-Type',f.endsWith('.css')?'text/css':'text/javascript');res.end(fs.readFileSync(path.join(assets,f)));return}
 const module=p.split('/')[1];let html=fs.readFileSync(path.join(assets,module?'module.html':'index.html'),'utf8');html=html.replaceAll('{{MODULE}}',module).replaceAll('{{TITLE}}','系統測試').replaceAll('{{SUBTITLE}}','介面驗證').replaceAll('{{DETAIL}}','載入資料');res.setHeader('Content-Type','text/html; charset=utf-8');res.end(html);
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,...(process.env.BROWSER_CHANNEL?{channel:process.env.BROWSER_CHANNEL}:{})});
 const base=`http://127.0.0.1:${server.address().port}`;
 try{
  const page=await browser.newPage();let dialogs=[],errors=[];page.on('dialog',async d=>{dialogs.push(d.message());await d.dismiss()});page.on('pageerror',e=>errors.push(e.message));
  for(const width of [320,390,768,1024,1366,1920]){
   await page.setViewportSize({width,height:900});
   for(const route of ['/','/system/','/water/','/power/','/thread/','/calculator/','/dosing/']){
    await page.goto(base+route);await page.waitForTimeout(120);
    await page.waitForSelector(route==='/'?'#water-dashboard .parameter-card':route==='/system/'?'#retention-save':route==='/water/'?'.water-form':route==='/power/'?'.mapping-device-row':route==='/thread/'?'.thread-layout':route==='/calculator/'?'#calc-save':'.dosing-grid');
    const overflow=await page.evaluate(()=>({viewport:innerWidth,scroll:document.documentElement.scrollWidth}));
    assert(overflow.scroll<=width+1,`${route} width ${width}: overflow ${JSON.stringify(overflow)}`);
    if(route==='/system/'){
     const alignment=await page.evaluate(()=>{const s=document.querySelector('#retention-days').getBoundingClientRect(),p=document.querySelector('.retention-settings p').getBoundingClientRect(),b=document.querySelector('#retention-save').getBoundingClientRect();return {s:s.x,p:p.x,b:b.x,sy:s.y,by:b.y}});
     assert(Math.abs(alignment.s-alignment.p)<1,'description must share select left edge');
     if(alignment.by>alignment.sy+10)assert(Math.abs(alignment.s-alignment.b)<1,'stacked button must align');
    }
   }
  }
  await page.goto(base+'/power/');await page.waitForSelector('[data-field=display-name]');
  await page.locator('[data-field=display-name]').first().fill('修改後保留');await page.route('**/api/hub/equipment',r=>r.request().method()==='PUT'?r.abort():r.continue());
  await page.getByRole('button',{name:'儲存全部設定'}).click();await page.waitForFunction(()=>!document.querySelector('.mapping-save-actions button').disabled);
  assert.equal(await page.locator('[data-field=display-name]').first().inputValue(),'修改後保留');assert(dialogs.pop().includes('輸入已保留'));
  await page.goto(base+'/water/');await page.waitForSelector('[data-water=po4]');await page.locator('[data-water=po4]').fill('0.01');
  await page.route('**/api/hub/water/records',r=>r.abort());await page.getByRole('button',{name:'儲存到樹莓派'}).click();await page.waitForFunction(()=>!document.querySelector('.water-form-actions button').disabled);
  assert.equal(await page.locator('[data-water=po4]').inputValue(),'0.01');assert(dialogs.pop().includes('輸入已保留'));
  assert.deepEqual(errors,[]);
  if(process.env.UI_SCREENSHOT){await page.setViewportSize({width:390,height:900});await page.goto(base+'/system/');await page.waitForSelector('#retention-save');await page.locator('.power-card').filter({hasText:'本機資料庫'}).screenshot({path:process.env.UI_SCREENSHOT})}
  console.log('PASS: 7 pages x 6 widths; retention alignment; equipment/water disconnected saves preserve inputs and unlock; zero page errors.');
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);server.close();process.exitCode=1});
