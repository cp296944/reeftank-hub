// Run with Playwright on NODE_PATH; no aquarium/HA connections are made.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const assets=path.resolve(__dirname,'../internal/hubweb/assets');
const stamp='2026-10-01T14:00:00+08:00';
const state=n=>({state:String(n),entity_id:'sensor.fixture'});
const devices=Array.from({length:18},(_,i)=>({id:`outlet_${String(i+1).padStart(2,'0')}`,slot:i+1,display_name:i===8?'珊瑚熊捲棉機':'設備 '+(i+1),switch_entity:'switch.tp_link_power_strip_fixture_'+(i+1)+'_long_entity_name',high_frequency:i===8}));
const power_strips=['3a31','3f2d','bfb9'].map((id,i)=>({id,display_name:id+' 排插',slot_start:i*6+1,slot_end:i*6+6,poll_interval_seconds:2}));
const strips=power_strips.map(s=>({name:s.display_name,power:state(123),current:state(1.2),today:state(.123),month:state(45),outlets:devices.filter(d=>d.slot>=s.slot_start&&d.slot<=s.slot_end).map(d=>({...d,name:d.display_name,voltage:state(117),current:state(.02),power:state(3),today:state(.01),month:state(2),switch:state('on')}))}));
const historyPoints=(base,wave=.1)=>Array.from({length:24},(_,i)=>({time:new Date(Date.parse(stamp)-(23-i)*3600000).toISOString(),value:base+Math.sin(i/2.7)*wave+(i%4)*wave*.12}));
const waterValues={no3:5,po4:.01,ph:8.2,ca:420,mg:1290,kh:8,sg:1.025};
const water={latest:Object.fromEntries(Object.entries(waterValues).map(([k,value])=>[k,{value,time:stamp}])),history:Object.fromEntries(Object.entries(waterValues).map(([k,value])=>[k,historyPoints(value,Math.max(Math.abs(value)*.015,.003))])),temperature_history:historyPoints(26.3,.25),records:[{id:1,measured_at:stamp,water_change:true,change_liters:40,note:'換水測試',po4:.01}],count:1};
const data={
 '/api/hub/status':{version:'hub-v1.0.0'},'/api/bootstrap/status':{required:false},
 '/api/update/status':{current:'hub-v1.0.0'},'/api/update/history':{current:'hub-v1.0.0',releases:[{tag:'hub-v1.0.0',date:'2026-10-03',notes:'第一個 1.x 正式版',local:false,url:'https://github.com/cp296944/reeftank-hub/releases/tag/hub-v1.0.0'},{tag:'hub-v0.12.0-jebao.12-release-history',date:'2026-10-03',notes:'本地版本紀錄',local:true,url:''}]},'/api/hub/storage/status':{samples:12345,temperature_samples:456,retention_days:0},
 '/api/hub/temperature':{configured:true,value:26.4,source_time:stamp,source:'direct'},
 '/api/hub/settings/temperature':{source:'direct',interval_seconds:60},
 '/api/hub/water':water,'/api/hub/high-frequency':{devices:{outlet_09:{today_count:3,updated_at:stamp,history:[]},outlet_10:{today_count:12,updated_at:stamp,history:[]}},events:{}},
 '/api/hub/equipment':{devices,power_strips},'/api/hub/ha/power':{strips,connected:true,temperature:state(26)},
 '/api/hub/storage/history':[], '/api/hub/thread/status':{configured:true,rcp_present:true,otbr_connected:true,network_name:'Thread fixture',role:'router'},
 '/api/hub/jebao':{busy:false,poll_seconds:30,devices:Array.from({length:4},(_,i)=>({id:i?'wavemaker-'+i:'return-pump-1',name:i?'造浪機 '+i:'主馬達',model:i?'GMP-30':'MDP-10000',connected:true,stale:false,last_success:stamp,mac:'AA:BB:CC:DD:EE:0'+i,identity:{ip:'192.168.0.'+(50+i),product_key:'fixture',firmware:'1.0'},attributes:{Motor_Speed:60+i,Mode:1},labels:{Mode:'隨機浪'}}))},
 '/api/hub/jebao/history':{hours:24,devices:Object.fromEntries(['return-pump-1','wavemaker-1','wavemaker-2','wavemaker-3'].map((id,i)=>[id,Array.from({length:12},(_,j)=>({time:new Date(Date.parse(stamp)-((11-j)*3600000)).toISOString(),connected:true,stale:false,speed:45+i*6+(j%4)*3,mode:'隨機浪',fault:false}))]))},
 '/api/hub/jebao/settings':{hosts:{}},
 '/api/diag':{model:'Raspberry Pi fixture',uptime_seconds:1085040,mem_total_kb:1000000,mem_avail_kb:500000,disk_total_kb:10000000,disk_free_kb:5000000,cpu_percent:20,soc_temp_c:48},
 '/api/hub/dosing':{state:{mode:'simulation',audit:[],heads:Array.from({length:4},(_,i)=>({id:i+1,name:'滴定泵 '+(i+1),liquid:['ALK','Ca','Mg','NP'][i],enabled:true,calibration_ml_per_min:10,container_ml:500,remaining_ml:[340,260,305,400][i],schedule:{daily_ml:[24.3,18.6,12.8,2][i],doses:4,start:'08:00'}})),calculator:{tank_liters:200,po4_concentration:2.435,no3_concentration:49.06,kh_efficiency:2.6,po4_daily_limit:.02,no3_daily_limit:1,kh_daily_limit:1}}}
};
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://localhost'),p=url.pathname;
 if(p.startsWith('/api/')){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data[p]??{}));return}
 if(p.startsWith('/hub-assets/')){const f=p.slice('/hub-assets/'.length);res.setHeader('Content-Type',f.endsWith('.css')?'text/css':f.endsWith('.png')?'image/png':'text/javascript');res.end(fs.readFileSync(path.join(assets,...f.split('/'))));return}
 const module=p.split('/')[1],titles={system:'系統狀態',water:'水質與換水',power:'電源監控',jebao:'JEBAO 造浪與主馬',thread:'Thread / Matter',calculator:'滴定計算工具',dosing:'魔點四頭滴定'},title=titles[module]||'ReefTank Hub';let html=fs.readFileSync(path.join(assets,module?'module.html':'index.html'),'utf8');html=html.replaceAll('{{MODULE}}',module).replaceAll('{{TITLE}}',title).replaceAll('{{SUBTITLE}}','本機設備與資料管理').replaceAll('{{DETAIL}}','正在載入即時資料');res.setHeader('Content-Type','text/html; charset=utf-8');res.end(html);
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,...(process.env.BROWSER_CHANNEL?{channel:process.env.BROWSER_CHANNEL}:{})});
 const base=`http://127.0.0.1:${server.address().port}`;
 try{
  const page=await browser.newPage();let dialogs=[],errors=[];page.on('dialog',async d=>{dialogs.push(d.message());await d.dismiss()});page.on('pageerror',e=>errors.push(e.message));
  for(const width of [320,390,768,1024,1366,1920]){
   await page.setViewportSize({width,height:900});
   for(const route of ['/','/system/','/water/','/power/','/jebao/','/thread/','/calculator/','/dosing/']){
    await page.goto(base+route);await page.waitForTimeout(120);
    await page.waitForSelector(route==='/'?'#water-dashboard .parameter-card':route==='/system/'?'#retention-save':route==='/water/'?'.water-form':route==='/power/'?'.mapping-device-row':route==='/jebao/'?'#jebao-cards article':route==='/thread/'?'.thread-layout':route==='/calculator/'?'#calc-save':'.dosing-grid');
    const overflow=await page.evaluate(()=>({viewport:innerWidth,scroll:document.documentElement.scrollWidth}));
    assert(overflow.scroll<=width+1,`${route} width ${width}: overflow ${JSON.stringify(overflow)}`);
    if(route==='/'){
     await page.waitForFunction(()=>document.querySelectorAll('#target-dosing-grid .dosing-tank').length===4);
     assert.equal(await page.locator('#target-dosing-grid .dosing-tank').count(),4,'home must show all four dosing heads');
     if(width>1100){
      assert.equal(await page.locator('#update-check').isVisible(),true,`home width ${width}: update check must be visible`);
      assert.equal(await page.locator('#version').isVisible(),true,`home width ${width}: version button must be visible`);
     }
    }
    if(route==='/power/'&&width>1100){
     const tops=await page.locator('.power-detail-grid>.power-card').evaluateAll(nodes=>nodes.map(node=>Math.round(node.getBoundingClientRect().top)));
     assert.equal(new Set(tops).size,1,`power width ${width}: three strips must share one row`);
    }
    if(route==='/jebao/'){
     assert.equal(await page.locator('#jebao-cards .jebao-device-card').count(),4,'Jebao must show four status cards');
     assert.equal(await page.locator('.jebao-settings-grid .jebao-setting-card').count(),4,'Jebao must show four settings cards');
     if(width>1100){
      const tops=await page.locator('.jebao-settings-grid .jebao-setting-card').evaluateAll(nodes=>nodes.map(node=>Math.round(node.getBoundingClientRect().top)));
      assert.equal(new Set(tops).size,1,`Jebao width ${width}: four settings cards must share one row`);
     }
    }
    if(route==='/system/'){
     const alignment=await page.evaluate(()=>{const s=document.querySelector('#retention-days').getBoundingClientRect(),p=document.querySelector('.retention-settings p').getBoundingClientRect(),b=document.querySelector('#retention-save').getBoundingClientRect();return {s:s.x,p:p.x,b:b.x,sy:s.y,by:b.y}});
     assert(Math.abs(alignment.s-alignment.p)<1,'description must share select left edge');
     if(alignment.by>alignment.sy+10)assert(Math.abs(alignment.s-alignment.b)<1,'stacked button must align');
    }
   }
  }
  await page.goto(base+'/');await page.getByRole('button',{name:'日覽'}).click();assert.equal(await page.evaluate(()=>document.documentElement.dataset.view),'day');
  await page.reload();assert.equal(await page.evaluate(()=>document.documentElement.dataset.viewMode),'day');
  await page.getByRole('button',{name:'夜覽'}).click();assert.equal(await page.evaluate(()=>document.documentElement.dataset.view),'night');
  await page.locator('#version').click();await page.waitForSelector('#history-dialog:not(.hidden) .history-entry');assert.equal(await page.locator('#history-dialog .history-entry').count(),2);await page.locator('#history-close').click();
  await page.goto(base+'/power/');await page.waitForSelector('[data-field=display-name]');
  await page.locator('[data-field=display-name]').first().fill('修改後保留');await page.route('**/api/hub/equipment',r=>r.request().method()==='PUT'?r.abort():r.continue());
  await page.getByRole('button',{name:'儲存全部設定'}).click();await page.waitForFunction(()=>!document.querySelector('.mapping-save-actions button').disabled);
  assert.equal(await page.locator('[data-field=display-name]').first().inputValue(),'修改後保留');assert(dialogs.pop().includes('輸入已保留'));
  await page.goto(base+'/water/');await page.waitForSelector('[data-water=po4]');await page.locator('[data-water=po4]').fill('0.01');
  await page.route('**/api/hub/water/records',r=>r.abort());await page.getByRole('button',{name:'儲存到樹莓派'}).click();await page.waitForFunction(()=>!document.querySelector('.water-form-actions button').disabled);
  assert.equal(await page.locator('[data-water=po4]').inputValue(),'0.01');assert(dialogs.pop().includes('輸入已保留'));
  assert.deepEqual(errors,[]);
  if(process.env.UI_SCREENSHOT){const route=process.env.UI_SCREENSHOT_ROUTE||'/system/';await page.setViewportSize({width:Number(process.env.UI_SCREENSHOT_WIDTH||390),height:Number(process.env.UI_SCREENSHOT_HEIGHT||900)});await page.goto(base+route);await page.waitForTimeout(250);await page.screenshot({path:process.env.UI_SCREENSHOT,fullPage:process.env.UI_SCREENSHOT_FULL_PAGE!=='false'})}
  console.log('PASS: 8 pages x 6 widths; theme persistence; retention alignment; equipment/water disconnected saves preserve inputs and unlock; zero page errors.');
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);server.close();process.exitCode=1});
