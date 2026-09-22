async function json(url){const r=await fetch(url,{cache:'no-store'});if(!r.ok)throw new Error(`${r.status}`);return r.json()}
async function start(){
  try{const s=await json('/api/hub/status');const version=document.querySelector('#version');const status=document.querySelector('#hub-status');if(version)version.textContent=s.version||'dev';if(status)status.textContent='Hub 正常運作'}catch(e){const status=document.querySelector('#hub-status');if(status)status.textContent='Hub 狀態異常'}
  try{const b=await json('/api/bootstrap/status');if(b.required){const card=document.querySelector('#bootstrap-card')||document.querySelector('#system-bootstrap');const command=document.querySelector('#bootstrap-command');if(card)card.classList.remove('hidden');if(command)command.textContent=b.install_command||'等待支援的 Raspberry Pi 版本'}}catch(e){}
  if(document.body.dataset.module==='power')loadPowerMapping()
}
async function loadPowerMapping(){
  const content=document.querySelector('#module-content'),placeholder=document.querySelector('#module-placeholder');
  try{
    const data=await json('/api/hub/equipment');
    content.textContent='';
    const note=document.createElement('div');note.className='mapping-note';note.textContent='HA 的 entity_id 不必改名。設備換插座時，只要在這裡選擇新的實體；若該實體已被使用，兩個設備會自動交換。';content.append(note);
    const grid=document.createElement('div');grid.className='strip-grid';
    const strips=data.power_strips||[
      {id:'3a31',display_name:'3A31 主機',slot_start:1,slot_end:6},
      {id:'3f2d',display_name:'3F2D 副機',slot_start:7,slot_end:12},
      {id:'bfb9',display_name:'BFB9 擴充',slot_start:13,slot_end:18}
    ];
    for(const strip of strips){
      const card=document.createElement('section');card.className='strip-card';card.dataset.strip=strip.id;
      const header=document.createElement('header');header.className='strip-header';
      const title=document.createElement('div');const heading=document.createElement('h2');heading.textContent=strip.display_name;const range=document.createElement('small');range.textContent=`插座 ${String(strip.slot_start).padStart(2,'0')}–${String(strip.slot_end).padStart(2,'0')}`;title.append(heading,range);
      const badge=document.createElement('span');badge.textContent=strip.id.toUpperCase();header.append(title,badge);card.append(header);
      const rows=document.createElement('div');rows.className='strip-rows';
      for(const device of data.devices.filter(d=>d.slot>=strip.slot_start&&d.slot<=strip.slot_end)){
      const row=document.createElement('article');row.className='mapping-row';
      const slot=document.createElement('span');slot.className='mapping-slot';slot.textContent=String(device.slot).padStart(2,'0');
      const name=document.createElement('div');name.className='mapping-name';name.textContent=device.display_name;if(device.critical){const tag=document.createElement('small');tag.textContent='重要設備';name.append(tag)}
      const select=document.createElement('select');select.setAttribute('aria-label',`${device.display_name} 的 HA 開關實體`);
      for(const optionDevice of data.devices){const option=document.createElement('option');option.value=optionDevice.switch_entity;option.textContent=`${String(optionDevice.slot).padStart(2,'0')} · ${optionDevice.switch_entity}`;option.selected=optionDevice.switch_entity===device.switch_entity;select.append(option)}
      select.addEventListener('change',async()=>{const chosen=select.value;if(!confirm(`將「${device.display_name}」重新指定到所選 HA 實體？`)){select.value=device.switch_entity;return}grid.classList.add('mapping-saving');try{await fetch(`/api/hub/equipment/${encodeURIComponent(device.id)}`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({switch_entity:chosen})}).then(r=>{if(!r.ok)throw new Error(r.status)});await loadPowerMapping()}catch(e){alert('儲存失敗，原設定未變更。');select.value=device.switch_entity}finally{grid.classList.remove('mapping-saving')}});
      row.append(slot,name,select);rows.append(row);
      }
      card.append(rows);grid.append(card);
    }
    content.append(grid);placeholder.classList.add('hidden');content.classList.remove('hidden');
  }catch(e){placeholder.querySelector('p').textContent='無法讀取設備映射，請查看系統狀態。'}
}
start();
