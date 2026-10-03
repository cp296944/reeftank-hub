const jebaoLabels={SwitchON:'控制器開關',Switch:'控制器開關',switch:'控制器開關',Motor_Speed:'設定速度',Flow:'設定流速',flow:'設定流速',Frequency:'頻率',frequency:'頻率',Mode:'模式',mode:'模式',Linkage:'聯動',FeedSwitch:'餵食狀態',FeedTime:'餵食時間',TimerON:'排程啟用',PulseTide:'潮汐設定',AutoMode:'排程模式'};
const jebaoFaults={Fault_Overcurrent:'過電流',Fault_Overvoltage:'過電壓',Fault_OverTemp:'過熱',Fault_Undervoltage:'欠壓',Fault_Lockedrotor:'堵轉',Fault_no_liveload:'無負載',Fault_UART:'通訊故障'};

function jebaoNumber(device){
  const attrs=device.attributes||{};
  const value=attrs.Motor_Speed??attrs.Flow??attrs.flow??attrs.Frequency??attrs.frequency;
  const number=Number(value);
  return Number.isFinite(number)?number:null;
}

function jebaoStatus(device){
  if(!device.connected)return {label:'離線',kind:'offline'};
  if(device.stale)return {label:'舊資料',kind:'warning'};
  const attrs=device.attributes||{};
  if(Object.keys(jebaoFaults).some(key=>attrs[key]===true))return {label:'警告',kind:'danger'};
  return {label:'運行中',kind:'online'};
}

function drawJebaoHistory(canvas,points,color='#f8c126'){
  if(!canvas)return;
  const ratio=window.devicePixelRatio||1,w=canvas.clientWidth||360,h=canvas.clientHeight||120;
  canvas.width=w*ratio;canvas.height=h*ratio;
  const ctx=canvas.getContext('2d');ctx.scale(ratio,ratio);ctx.clearRect(0,0,w,h);
  ctx.strokeStyle=getComputedStyle(document.documentElement).getPropertyValue('--v2-line').trim()||'#293844';ctx.lineWidth=1;
  for(let i=1;i<4;i++){ctx.beginPath();ctx.moveTo(0,i*h/4);ctx.lineTo(w,i*h/4);ctx.stroke()}
  const values=(points||[]).map(p=>({t:Date.parse(p.time),v:Number(p.speed??p.frequency)})).filter(p=>Number.isFinite(p.t)&&Number.isFinite(p.v));
  if(!values.length){ctx.fillStyle=getComputedStyle(document.documentElement).getPropertyValue('--v2-muted').trim()||'#8394a2';ctx.font='12px system-ui';ctx.fillText('正在累積本機歷史資料',12,h/2);return}
  const minT=Math.min(...values.map(p=>p.t)),maxT=Math.max(...values.map(p=>p.t)),maxV=Math.max(100,...values.map(p=>p.v));
  const gradient=ctx.createLinearGradient(0,0,0,h);gradient.addColorStop(0,color+'55');gradient.addColorStop(1,color+'05');
  ctx.beginPath();values.forEach((p,i)=>{const x=8+(p.t-minT)/Math.max(1,maxT-minT)*(w-16),y=h-8-p.v/maxV*(h-16);i?ctx.lineTo(x,y):ctx.moveTo(x,y)});ctx.lineTo(w-8,h-8);ctx.lineTo(8,h-8);ctx.closePath();ctx.fillStyle=gradient;ctx.fill();
  ctx.beginPath();values.forEach((p,i)=>{const x=8+(p.t-minT)/Math.max(1,maxT-minT)*(w-16),y=h-8-p.v/maxV*(h-16);i?ctx.lineTo(x,y):ctx.moveTo(x,y)});ctx.strokeStyle=color;ctx.lineWidth=2.5;ctx.stroke();
}

async function loadJebaoDashboard(){
  const content=document.querySelector('#module-content'),placeholder=document.querySelector('#module-placeholder');
  let timer,pending=false,hours=24,settings={hosts:{}};
  const time=value=>value?new Date(value).toLocaleString('zh-TW',{hour12:false}):'尚未讀取';

  function historyFor(history,id){return history?.devices?.[id]||[]}
  function renderCards(data,history){
    const root=content.querySelector('#jebao-cards');if(!root)return;
    root.innerHTML=(data.devices||[]).map((device,index)=>{
      const attrs=device.attributes||{},status=jebaoStatus(device),value=jebaoNumber(device),isPump=device.id==='return-pump-1';
      const rawMode=device.labels?.Mode??device.labels?.mode??attrs.Mode??attrs.mode,mode=typeof rawMode==='boolean'?'—':(rawMode??'—');
      const unit=isPump?'%':Object.hasOwn(attrs,'Flow')||Object.hasOwn(attrs,'flow')?'%':'Hz';
      const fields=Object.entries(jebaoLabels).filter(([key])=>Object.hasOwn(attrs,key)).slice(0,6).map(([key,label])=>'<div><dt>'+label+'</dt><dd>'+escapeHTML(typeof attrs[key]==='boolean'?(attrs[key]?'開啟':'關閉'):(device.labels?.[key]??String(attrs[key])))+'</dd></div>').join('');
      const activeFaults=Object.entries(jebaoFaults).filter(([key])=>attrs[key]===true).map(([,label])=>label);
      const accent=['#43dd8b','#ffd027','#20c6e8','#a78bfa'][index%4];
      return '<article class="jebao-device-card" data-device="'+escapeHTML(device.id)+'" style="--device-accent:'+accent+'">'+
        '<header><div><small>'+(isPump?'RETURN PUMP':'WAVEMAKER '+index)+'</small><h2>'+escapeHTML(device.name)+'</h2><p>'+escapeHTML(device.model)+'</p></div><span class="device-state '+status.kind+'"><i></i>'+status.label+'</span></header>'+
        '<div class="device-reading"><strong>'+(value==null?'—':value)+'</strong><span>'+unit+'</span><em>'+(isPump?'主馬轉速':'造浪設定值')+'</em></div>'+
        '<canvas class="jebao-history" aria-label="'+escapeHTML(device.name)+' 歷史曲線"></canvas>'+
        '<dl class="device-facts"><div><dt>模式</dt><dd>'+escapeHTML(mode)+'</dd></div><div><dt>最後回報</dt><dd>'+time(device.last_success)+'</dd></div><div><dt>IP</dt><dd>'+escapeHTML(device.identity?.ip||'自動探索')+'</dd></div><div><dt>紀錄</dt><dd>'+historyFor(history,device.id).length+' 筆</dd></div>'+fields+'</dl>'+
        (activeFaults.length?'<p class="device-alert">'+escapeHTML(activeFaults.join('、'))+'</p>':'')+
        (device.error?'<p class="device-alert">'+escapeHTML(device.error)+'</p>':'')+
        '<details><summary>設備識別與原始讀值</summary><p>MAC：'+escapeHTML(device.mac)+'</p><p>Product key：'+escapeHTML(device.identity?.product_key||'待探索')+'</p><p>韌體：'+escapeHTML(device.identity?.firmware||'未知')+'</p><pre>'+escapeHTML(JSON.stringify(attrs,null,2))+'</pre></details></article>';
    }).join('');
    (data.devices||[]).forEach((device,index)=>drawJebaoHistory(root.querySelector('[data-device="'+device.id+'"] canvas'),historyFor(history,device.id),['#43dd8b','#ffd027','#20c6e8','#a78bfa'][index%4]));
    const online=(data.devices||[]).filter(d=>d.connected&&!d.stale).length;
    content.querySelector('#jebao-status').textContent=(data.busy?'正在探索／讀取 · ':'')+online+'/'+(data.devices||[]).length+' 台連線 · 每 '+data.poll_seconds+' 秒寫入本機歷史';
    content.querySelector('#jebao-discovery-error').textContent=data.discovery_error||'';
  }

  async function refresh(){
    if(pending)return;pending=true;
    try{const [data,history]=await Promise.all([json('/api/hub/jebao'),json('/api/hub/jebao/history?hours='+hours)]);renderCards(data,history)}
    catch(e){const status=content.querySelector('#jebao-status');if(status)status.textContent='監控 API 讀取失敗；保留上一筆畫面'}finally{pending=false}
  }

  try{
    const [data,initialSettings,history]=await Promise.all([json('/api/hub/jebao'),json('/api/hub/jebao/settings'),json('/api/hub/jebao/history?hours=24')]);settings=initialSettings;
    const settingCards=data.devices.map((device,index)=>{
      const isPump=device.id==='return-pump-1',status=jebaoStatus(device),accent=['#43dd8b','#ffd027','#20c6e8','#a78bfa'][index%4],value=isPump?(device.attributes?.Motor_Speed??''):(device.attributes?.Flow??'');
      const control=isPump?'<form id="jebao-speed" class="jebao-device-control"><label>設定轉速 (%)<input name="speed" type="number" min="1" max="100" step="1" required></label><button class="top-button" type="submit">套用並確認</button><p id="jebao-speed-result" role="status"></p></form><div class="feed-controls"><button id="jebao-feed-start" class="top-button" type="button">開始餵食</button><button id="jebao-feed-stop" class="top-button" type="button">結束餵食</button></div><p id="jebao-feed-result" role="status"></p>':'<form class="jebao-flow-form jebao-device-control" data-id="'+escapeHTML(device.id)+'"><label>造浪強度 (%)<input name="flow" type="number" min="1" max="100" step="1" value="'+escapeHTML(value)+'" required></label><button class="top-button" type="submit">套用強度並確認</button><p role="status"></p></form>';
      return '<section class="power-card jebao-setting-card" style="--device-accent:'+accent+'"><header><div><small>'+(isPump?'RETURN PUMP':'WAVEMAKER '+index)+'</small><h2>'+escapeHTML(device.name)+'</h2><p>'+escapeHTML(device.model)+'</p></div><span class="device-state '+status.kind+'"><i></i>'+status.label+'</span></header><div class="jebao-setting-body"><label class="setting-field">設備 IP<input form="jebao-settings" name="'+escapeHTML(device.id)+'" value="'+escapeHTML(settings.hosts?.[device.id]||'')+'" placeholder="自動探索" inputmode="decimal"></label>'+control+'<button class="top-button jebao-ip-save" type="submit" form="jebao-settings">儲存設備 IP</button></div><p class="jebao-setting-note">'+(isPump?'主馬速度與餵食均由設備讀回確認。':'只調整強度；排程、餵食或聯動啟用時不寫入。')+'</p></section>';
    }).join('');
    content.innerHTML='<section class="jebao-overview"><div><p class="eyebrow">LOCAL LAN MONITOR</p><h2>Jebao 水流控制</h2><p>四台設備每輪狀態都寫入樹莓派資料庫，離線紀錄也會保留。</p></div><div class="jebao-toolbar"><div class="history-range" role="group" aria-label="歷史範圍"><button class="active" data-hours="24">24 小時</button><button data-hours="168">7 天</button><button data-hours="720">30 天</button></div><button id="jebao-refresh" class="top-button" type="button">立即更新</button></div></section><div class="jebao-summary-line"><span id="jebao-status" role="status"></span><span id="jebao-discovery-error"></span></div><div id="jebao-cards" class="jebao-grid"></div><div class="jebao-settings-head"><h2>設備設定</h2><span>四台設備個別設定</span></div><div class="jebao-settings-grid">'+settingCards+'</div><form id="jebao-settings" class="jebao-settings-master"><button class="top-button" type="submit">儲存全部設備 IP</button><p id="jebao-save-result" role="status"></p></form>';
    placeholder.classList.add('hidden');content.classList.remove('hidden');renderCards(data,history);
    content.querySelectorAll('[data-hours]').forEach(button=>button.onclick=async()=>{hours=Number(button.dataset.hours);content.querySelectorAll('[data-hours]').forEach(x=>x.classList.toggle('active',x===button));await refresh()});
    content.querySelector('#jebao-refresh').onclick=async e=>{e.target.disabled=true;try{await saveJSON('/api/hub/jebao/refresh','POST',{});await refresh()}catch(err){content.querySelector('#jebao-status').textContent=err.message}finally{e.target.disabled=false}};
    content.querySelectorAll('.jebao-flow-form').forEach(form=>form.onsubmit=async e=>{e.preventDefault();const button=form.querySelector('button'),result=form.querySelector('[role="status"]'),flow=Number(form.elements.flow.value);if(!Number.isInteger(flow)||flow<1||flow>100)return;button.disabled=true;result.textContent='正在送出並讀回強度…';try{const reply=await saveJSON('/api/hub/jebao/wavemakers/'+encodeURIComponent(form.dataset.id)+'/flow','POST',{flow});result.textContent='設備已確認強度 '+reply.flow+'%；請核對 App 與現場水流';await refresh()}catch(err){result.textContent=err.message}finally{button.disabled=false}});
    const speedForm=content.querySelector('#jebao-speed');speedForm.elements.speed.value=data.devices.find(d=>d.id==='return-pump-1')?.attributes?.Motor_Speed??'';
    speedForm.onsubmit=async e=>{e.preventDefault();const button=speedForm.querySelector('button'),result=content.querySelector('#jebao-speed-result'),speed=Number(speedForm.elements.speed.value);if(!Number.isInteger(speed)||speed<1||speed>100)return;button.disabled=true;result.textContent='正在送出並讀回確認…';try{const reply=await saveJSON('/api/hub/jebao/return-pump/speed','POST',{speed});result.textContent='設備已確認 '+reply.speed+'%';await refresh()}catch(err){result.textContent=err.message}finally{button.disabled=false}};
    async function setFeeding(enabled){const start=content.querySelector('#jebao-feed-start'),stop=content.querySelector('#jebao-feed-stop'),result=content.querySelector('#jebao-feed-result');start.disabled=stop.disabled=true;result.textContent='正在讀回餵食狀態…';try{const reply=await saveJSON('/api/hub/jebao/return-pump/feeding','POST',{enabled});result.textContent='餵食模式已確認：'+(reply.enabled?'開啟':'關閉');await refresh()}catch(err){result.textContent=err.message}finally{start.disabled=stop.disabled=false}}
    content.querySelector('#jebao-feed-start').onclick=()=>setFeeding(true);content.querySelector('#jebao-feed-stop').onclick=()=>setFeeding(false);
    content.querySelector('#jebao-settings').onsubmit=async e=>{e.preventDefault();const button=e.submitter||e.target.querySelector('button'),hosts={};for(const [id,value] of new FormData(e.target)){if(String(value).trim())hosts[id]=String(value).trim()}button.disabled=true;try{await saveJSON('/api/hub/jebao/settings','PUT',{hosts});content.querySelector('#jebao-save-result').textContent='已儲存；下一輪讀取生效'}catch(err){content.querySelector('#jebao-save-result').textContent=err.message}finally{button.disabled=false}};
    timer=setInterval(refresh,10000);window.addEventListener('pagehide',()=>clearInterval(timer),{once:true});
  }catch(e){placeholder.textContent='Jebao 監控尚無法載入：'+e.message}
}
