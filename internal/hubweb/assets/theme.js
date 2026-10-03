(function(){
  'use strict';
  const key='reef-view-mode';
  const valid=new Set(['auto','day','night']);
  function localHour(){return new Date().getHours()}
  function resolved(mode){return mode==='auto'?(localHour()>=7&&localHour()<19?'day':'night'):mode}
  function selected(){const value=localStorage.getItem(key)||'auto';return valid.has(value)?value:'auto'}
  function apply(mode){
    document.documentElement.dataset.view=resolved(mode);
    document.documentElement.dataset.viewMode=mode;
    const meta=document.querySelector('meta[name="theme-color"]');
    if(meta)meta.content=resolved(mode)==='day'?'#edf2f3':'#020304';
    document.querySelectorAll('[data-view-option]').forEach(button=>{
      const active=button.dataset.viewOption===mode;
      button.classList.toggle('active',active);button.setAttribute('aria-pressed',String(active));
    });
  }
  apply(selected());
  document.addEventListener('DOMContentLoaded',()=>{
    const host=document.querySelector('.topbar');if(!host)return;
    const control=document.createElement('div');control.className='view-control';control.setAttribute('aria-label','顯示模式');
    control.innerHTML='<div class="view-options"><button type="button" data-view-option="auto">自動</button><button type="button" data-view-option="day">日覽</button><button type="button" data-view-option="night">夜覽</button></div><small>依裝置所在地時間</small>';
    host.append(control);apply(selected());
    control.addEventListener('click',event=>{const button=event.target.closest('[data-view-option]');if(!button)return;localStorage.setItem(key,button.dataset.viewOption);apply(button.dataset.viewOption)});
    setInterval(()=>{if(selected()==='auto')apply('auto')},60000);
  });
})();
