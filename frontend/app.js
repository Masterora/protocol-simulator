let nativeContext=null,nativeDesktop=false;
// Snapshots and channel configurations are JSON data from the Go bridge.
const cloneBridgeData=value=>typeof globalThis.structuredClone==='function'?globalThis.structuredClone(value):JSON.parse(JSON.stringify(value));
// Older macOS WebKit supports validated button submission before requestSubmit.
function submitWithButton(form,button){if(typeof form.requestSubmit==='function')form.requestSubmit(button);else button?.click();}
const $ = id => document.getElementById(id);
const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const names={iec104:'IEC104','modbus-tcp':'Modbus TCP','modbus-rtu':'Modbus RTU',raw:'RAW'};
const kinds={mc:'脉冲',position:'步位置',yk:'遥控',yt:'遥调设值',step:'步调节',yx:'遥信',yc:'遥测',coil:'线圈',discrete:'离散量',holding:'保持寄存器',input:'输入寄存器'};
const statuses={stopped:'已停止',connecting:'等待连接',connected:'已连接',listening:'监听中',disconnected:'已断开',error:'连接失败'};
const active=s=>['connected','connecting','listening'].includes(s.status);
const keyOf=p=>`${p.kind}:${p.address}`;
let snapshot={project:{name:'工程1',channels:[]},states:[],messages:[]},selected='',selectedDevice='',busy=null,refreshing=null;
let sidebarKey='',actionKey='',pointKey='',messageKey='',pointPage=0,paused=false,frozen=[],shownMessages=[],toastTimer;
let controlEditor=null,controlSelection=null;
let pointEditor=null,confirmResolver=null,projectIdentity='';
let stationMode='client',deviceKey='initial',pointTabsKey='';
const pausedViews=new Map();
const drafts=new Map();
const channel=()=>snapshot.project.channels.find(c=>c.id===selected);
const device=()=>channel()?.devices?.find(d=>d.id===selectedDevice);
const current=()=>{const c=channel(),d=device();return c?{...c,points:d?.points||[],unit:d?.unit??c.unit,commonAddress:d?.commonAddress??c.commonAddress}:null;};
const draftKey=()=>selected+'/'+(selectedDevice||'raw');
function normalizeSnapshot(){for(const c of snapshot.project.channels){if(!Array.isArray(c.devices))c.devices=c.protocol==='raw'?[]:[{id:'device-1',name:'装置 1',unit:c.unit,commonAddress:c.commonAddress,points:c.points||[]}];}}
const state=()=>snapshot.states.find(s=>s.id===selected)||{status:'stopped',peers:0,tx:0,rx:0};
const field=(form,name)=>form.elements.namedItem(name);
function toast(text,error=false){$('toast').textContent=text;$('toast').className=`visible${error?' error':''}`;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').className='',error?6000:2500);}
function report(error,form){const text=String(error?.message||error);toast(text,true);const box=form?.querySelector('.form-error');if(box){box.textContent=text;box.hidden=false;}}
function api(method,...args){const bridge=window.go?.main?.App;if(nativeContext?.kind)return bridge.CallNative(method,args);if(!bridge?.[method])return Promise.reject(Error('桌面服务未就绪'));return bridge[method](...args);}
async function refresh(force=false){
  let reloadNetwork=false;if(nativeContext?.kind==='channel-dialog'||nativeContext?.kind==='device-dialog'&&!nativeContext.edit){const c=await window.go.main.App.GetNativeContext();reloadNetwork=c.kind==='channel-dialog'&&c.activate!==nativeContext.activate;nativeContext=c;selected=c.channel;}
  if(refreshing){await refreshing;if(!force)return;}
  refreshing=(async()=>{try{snapshot=await api('GetSnapshot');normalizeSnapshot();const identity=snapshot.project.channels.map(c=>c.id).join('|');if(identity!==projectIdentity){projectIdentity=identity;const valid=new Set(snapshot.project.channels.flatMap(c=>[c.id+'/raw',...c.devices.map(d=>c.id+'/'+d.id)]));for(const id of drafts.keys())if(!valid.has(id))drafts.delete(id);}
    if(!snapshot.project.channels.some(c=>c.id===selected&&c.role===stationMode))selectChannel(snapshot.project.channels.find(c=>c.role===stationMode)?.id||'',false);render();
  }catch(error){$('footer-detail').textContent=String(error?.message||error);$('footer-detail').hidden=false;}finally{refreshing=null;}})();
  await refreshing;if(reloadNetwork)channelDialog();
}
async function act(method,...args){const result=await api(method,...args);await refresh(true);return result;}
async function run(action,fn,form=null){if(busy)return;busy={action,channel:selected};if(form){const box=form.querySelector('.form-error');if(box)box.hidden=true;}renderControls();try{return await fn();}catch(error){report(error,form);}finally{busy=null;renderControls();}}
function on(id,event,fn,locked=true){if(!$(id))return;$(id).addEventListener(event,async e=>{if(event==='submit')e.preventDefault();if(locked)await run(id,()=>fn(e),e.target.closest('form'));else{try{await fn(e);}catch(error){report(error);}}});}
function messageScope(){return $('all-messages').checked?'*':selected;}
function loadPauseView(){const saved=pausedViews.get(messageScope());paused=!!saved;frozen=saved||[];$('pause-messages').textContent=paused?'继续':'暂停';$('pause-messages').classList.toggle('is-paused',paused);shownMessages=[];messageKey='';}
function selectChannel(id,draw=true){if(id!==selected){saveDraft();selected=id;selectedDevice=channel()?.devices?.[0]?.id||'';const c=current();if(c)stationMode=c.role;pointPage=0;pointKey='';messageKey='';$('point-search').value='';$('point-kind').value='';loadPauseView();}if(nativeDesktop&&!nativeContext?.kind)api('SetNativeSelection',{role:stationMode,channel:selected,device:selectedDevice}).catch(report);if(draw)render();}
function selectDevice(id,draw=true){if(id!==selectedDevice){saveDraft();selectedDevice=id;pointPage=0;pointKey='';$('point-search').value='';}if(draw)render();}
function setButtonLabel(id,text){const b=$(id);(b.querySelector('span')||b).textContent=text;}
function setHTML(id,html){if($(id).innerHTML!==html)$(id).innerHTML=html;}
function formatBytes(n){if(n<1024)return `${n} <em>B</em>`;if(n<1048576)return `${(n/1024).toFixed(1)} <em>KB</em>`;return `${(n/1048576).toFixed(1)} <em>MB</em>`;}
function render(){
  if(!channel()?.devices?.some(d=>d.id===selectedDevice))selectDevice(channel()?.devices?.[0]?.id||'',false);
  const c=current(),s=state();const unsaved=!snapshot.path||snapshot.dirty;$('project-name').textContent=snapshot.project.name+(unsaved?'（未保存）':'');$('project-name').title=$('project-name').textContent+(snapshot.path?'\n'+snapshot.path:'');
  const channels=snapshot.project.channels.filter(ch=>ch.role===stationMode);$('channel-count').textContent=channels.length;
  for(const button of document.querySelectorAll('[data-mode]')){const chosen=button.dataset.mode===stationMode;button.classList.toggle('selected',chosen);button.setAttribute('aria-selected',String(chosen));}
  const sk=JSON.stringify([selected,stationMode,channels.map(ch=>[ch.id,ch.name,ch.protocol,ch.role,snapshot.states.find(st=>st.id===ch.id)?.status])]);
  if(sk!==sidebarKey){sidebarKey=sk;const focused=document.activeElement?.dataset.select;const scroll=$('channel-list').scrollTop;$('channel-list').innerHTML=channels.map(ch=>{const st=snapshot.states.find(v=>v.id===ch.id)||{status:'stopped'};return `<button class="channel-item ${ch.id===selected?'selected':''}" data-select="${escape(ch.id)}" aria-pressed="${ch.id===selected}" title="${escape(ch.address)}"><span class="channel-symbol">${ch.role==='server'?'▣':'⇄'}</span><span class="text"><b>${escape(ch.name)}</b><small>${escape(names[ch.protocol])} · ${ch.role==='server'?'子站':'主站'}</small></span><i title="${escape(statuses[st.status])}" class="status-dot ${active(st)?'online':st.status==='error'?'error':''}"></i></button>`;}).join('')||'<div class="channel-placeholder">暂无通道</div>';$('channel-list').scrollTop=scroll;if(focused){const button=[...$('channel-list').querySelectorAll('button')].find(b=>b.dataset.select===focused);button?.focus({preventScroll:true});}}
  const dk=JSON.stringify([c?.id,selectedDevice,c?.devices.map(d=>[d.id,d.name,d.unit,d.commonAddress])]);if(dk!==deviceKey){deviceKey=dk;const list=$('device-list'),scroll=list.scrollTop,focused=document.activeElement?.dataset.device;list.innerHTML=c?.devices.length?c.devices.map(d=>`<button class="device-item ${d.id===selectedDevice?'selected':''}" data-device="${escape(d.id)}" aria-pressed="${d.id===selectedDevice}" title="${escape(d.name)} · 双击修改装置参数"><span>地址：${c.protocol==='iec104'?d.commonAddress:d.unit} ${escape(d.name)}</span><small>${c.protocol==='iec104'?'公共地址 '+d.commonAddress:'设备地址 '+d.unit}</small></button>`).join(''):'<div class="channel-placeholder">暂无装置</div>';list.scrollTop=scroll;if(focused)[...list.querySelectorAll('button')].find(b=>b.dataset.device===focused)?.focus({preventScroll:true});}
  $('device-count').textContent=String(c?.devices.length||0);$('selected-device-name').textContent=device()?.name||'';
  $('current-protocol').textContent=c?names[c.protocol]:'';$('channel-name').textContent=c?.name||'未选择通道';
  setHTML('channel-meta',c?`<i class="status-dot ${active(s)?'online':s.status==='error'?'error':''}"></i><span class="connection-status">${escape(statuses[s.status]||s.status)}</span><span>·</span><span class="endpoint" title="${escape(s.address||c.address)}">${escape(s.address||c.address)}</span><span>·</span>${c.role==='server'?'子站':'主站'}`:'');
  setHTML('tx-count',formatBytes(s.tx||0));setHTML('rx-count',formatBytes(s.rx||0));$('peer-count').textContent=s.peers||0;
  $('footer-detail').textContent=s.status==='connecting'?'自动重试中':s.lastError||'';$('footer-detail').title=s.lastError||'';$('footer-detail').hidden=s.status!=='connecting'&&!s.lastError;
  $('information-caption').textContent=(stationMode==='server'?'子站':'主站')+'装置';renderActions(c);renderPointTabs(c);renderPoints(c);renderMessages();renderEvents();renderDebug();renderManager();renderControls();
}
function renderControls(){
  if($('auto-data-dialog').open)updateDataControls();
  const c=current(),s=state(),locked=!!busy,online=!!c&&s.peers>0&&s.status==='connected';
  for(const id of ['new-project','open-project','save-project','save-as','add-channel','stop-all'])$(id).disabled=locked;
  $('stop-all').disabled=locked||!snapshot.states.some(active);
  $('edit-channel').disabled=locked||!c;$('delete-channel').disabled=locked||!c||active(s);if($('channel-dialog').open)updateChannelFields();
  $('device-config').disabled=locked||!device();$('add-device').disabled=locked;
  $('toggle-channel').disabled=locked||!c;for(const id of ['add-point','import-points','export-points'])$(id).disabled=locked||!device();
  setButtonLabel('toggle-channel',busy?.action==='toggle-channel'&&busy.channel===selected?(active(s)?'停止中…':'连接中…'):(active(s)?'停止':s.status==='disconnected'||s.status==='error'?'重连':'连接'));
  for(const id of ['read-clock','counter-interrogate','clock-sync','interrogate','publish','read-modbus','write-modbus','open-yk','open-yt','open-step'])if($(id))$(id).disabled=locked||!online||!device();
  for(const button of document.querySelectorAll('[data-demo],[data-edit-point],[data-delete-point]'))button.disabled=locked;
  for(const button of document.querySelectorAll('#channel-form button[type=submit],#point-form button[type=submit],#device-form button[type=submit]'))button.disabled=locked;
  const raw=c?.protocol==='raw';$('raw-form').hidden=!raw;$('raw-hex').disabled=locked||!raw;$('append-crc').disabled=locked||!raw;$('raw-form').querySelector('button').disabled=locked||!raw||!online;
  $('export-messages').disabled=locked||!snapshot.messages.length;$('clear-messages').disabled=locked||(!snapshot.messages.length&&!frozen.length);
  updateAutoButtons();for(const id of ['open-auto-yk','open-auto-yt'])if($(id))$(id).disabled=locked||!device();updateControlButtons();$('channel-form').setAttribute('aria-busy',String(locked));$('point-form').setAttribute('aria-busy',String(locked));
}
function saveDraft(){if(!selected)return;const data={};for(const el of $('protocol-actions').querySelectorAll('input,select'))data[el.id]=el.value;data.hex=$('raw-hex').value;data.crc=$('append-crc').checked;drafts.set(draftKey(),data);}
function renderActions(c){
  const key=c?`${c.id}:${selectedDevice}:${c.protocol}:${c.role}`:'';if(key===actionKey)return;actionKey=key;const box=$('protocol-actions');box.hidden=!c||!device();
  if(!c){box.innerHTML='';return;}
  if(c.protocol==='iec104')box.innerHTML=c.role==='client'?'<button id="interrogate" class="primary">总召唤</button><button id="counter-interrogate">脉冲召唤</button><button id="clock-sync">校时</button><button id="show-events">SOE / COS</button><button id="open-yk">遥控</button><button id="open-auto-yk">自动遥控</button><button id="open-step">遥调（步调节）</button><button id="open-yt">遥调（设值）</button><div class="operation-group auto-yt-source"><label>点地址<input id="auto-yt-addresses" type="text" placeholder="1,3-5" value="0"></label><select id="auto-yt-type" aria-label="遥调类型"><option value="48">归一化</option><option value="49">标度化</option><option value="50">浮点数</option></select><label>遥调数值<input id="auto-yt-value" type="text" value="0"></label><button id="open-auto-yt">自动遥调</button></div>':'<button id="publish" class="primary">上送</button><button id="open-auto-data">自动数据</button>';
  else if(c.protocol.startsWith('modbus')&&c.role==='client')box.innerHTML='<button id="clock-sync">校时</button><button id="read-clock">读时</button><div class="operation-group"><select id="read-fc" aria-label="读功能码"><option value="3">03 保持寄存器</option><option value="4">04 输入寄存器</option><option value="1">01 线圈</option><option value="2">02 离散量</option></select><label>地址<input id="read-start" type="number" min="0" max="65535" value="0" aria-label="读起始地址"></label><label>数量<input id="read-count" type="number" min="1" max="125" value="2" aria-label="读数量"></label><button id="read-modbus" class="primary">读取</button></div><div class="operation-group"><select id="write-fc" aria-label="写功能码"><option value="6">06 单寄存器</option><option value="5">05 单线圈</option></select><label>地址<input id="write-address" type="number" min="0" max="65535" value="0" aria-label="写地址"></label><label>值<input id="write-value" type="number" min="0" max="65535" value="1" aria-label="写值"></label><button id="write-modbus">写入</button></div>';
  else {box.innerHTML='';box.hidden=true;}
  if(c.protocol.startsWith('modbus')&&c.role==='server')box.innerHTML='<button id="open-auto-data">自动数据</button>';
  if($('open-auto-data'))on('open-auto-data','click',openAutoDataWindow);
  const draft=drafts.get(draftKey())||{};for(const el of box.querySelectorAll('input,select'))if(draft[el.id]!==undefined)el.value=draft[el.id];$('raw-hex').value=draft.hex||'';$('append-crc').checked=!!draft.crc;
  if($('read-clock'))on('read-clock','click',()=>act('ReadClockDevice',selected,selectedDevice));if($('counter-interrogate'))on('counter-interrogate','click',()=>act('CounterInterrogateDevice',selected,selectedDevice));if($('clock-sync'))on('clock-sync','click',()=>act('ClockSyncDevice',selected,selectedDevice));if($('show-events'))on('show-events','click',()=>{$('events-dialog').show();renderEvents();},false);
 if($('interrogate'))on('interrogate','click',()=>act('InterrogateDevice',selected,selectedDevice));if($('publish'))on('publish','click',()=>act('PublishDevice',selected,selectedDevice));
  for(const [id,mode]of [['open-yk','yk'],['open-step','step'],['open-yt','setpoint']])if($(id))on(id,'click',()=>controlDialog(mode),false);
  for(const mode of ['yk','yt'])if($('open-auto-'+mode))on('open-auto-'+mode,'click',()=>openAutoWindow(mode));
  if($('auto-yt-type'))on('auto-yt-type','change',()=>api('SetAutoValueType',selected,selectedDevice,Number($('auto-yt-type').value)),false);
  if($('auto-yt-addresses')&&!draft['auto-yt-addresses'])$('auto-yt-addresses').value=device()?.points.filter(p=>p.kind==='yt').map(p=>p.address).join(',')||'0';
  if($('read-modbus')){updateModbusLimits();on('read-fc','change',updateModbusLimits,false);on('write-fc','change',updateModbusLimits,false);
    on('read-modbus','click',()=>{const fc=Number($('read-fc').value),address=integer('read-start',0,65535),count=integer('read-count',1,fc<=2?2000:125);if(address+count>65536)throw Error('读取范围超出 65535');return act('ReadDeviceModbus',selected,selectedDevice,fc,address,count);});
    on('write-modbus','click',async()=>{const id=selected,deviceID=selectedDevice,fc=Number($('write-fc').value),address=integer('write-address',0,65535),value=integer('write-value',0,fc===5?1:65535);if(await confirmAction('写入确认',`${device().name} · 地址 ${address} → ${value}`)){await act('WriteDeviceModbus',id,deviceID,fc,address,value);toast('写入请求已发送');}});
  }
}
function integer(id,min,max){const el=$(id),n=Number(el.value);if(el.value.trim()===''||!Number.isInteger(n)||n<min||n>max){el.focus();throw Error(`${el.getAttribute('aria-label')||'参数'}：${min}–${max} 整数`);}return n;}
function updateModbusLimits(){const bits=Number($('read-fc').value)<=2;$('read-count').max=bits?'2000':'125';if(Number($('read-count').value)>Number($('read-count').max))$('read-count').value=$('read-count').max;$('write-value').max=$('write-fc').value==='5'?'1':'65535';if(Number($('write-value').value)>Number($('write-value').max))$('write-value').value=$('write-value').max;saveDraft();}
const tabNames={yt:'遥调设值',step:'步调节',yx:'遥信',yc:'遥测',mc:'脉冲',position:'步位置',yk:'遥控',jdq:'继电器'};
function activeRange(){const value=$('point-kind').value;return value.startsWith('tab:')?device()?.configuration?.ranges?.find(r=>r.tab===value.slice(4)):null;}
function applyRangeRequest(){const range=activeRange();if(!range||!$('read-fc'))return;const fc={coil:1,discrete:2,holding:3,input:4}[range.kind];$('read-fc').value=String(fc);$('read-start').value=range.address;const sample=device()?.points.find(p=>p.tab===range.tab||p.kind===range.kind&&p.address===range.address),width=sample?.dataType?.endsWith('32')?2:1,limit=fc<=2?2000:125-125%width;$('read-count').value=Math.max(width,Math.min(range.count*width,limit));$('write-address').value=range.address;updateModbusLimits();}
function renderPointTabs(c){const ranges=device()?.configuration?.ranges,types=!c||c.protocol==='raw'?[]:ranges?.length?[...ranges.map(r=>'tab:'+r.tab),...(c.protocol==='iec104'?['mc','position','yk','yt','step'].filter(k=>c.points.some(p=>p.kind===k)&&!ranges.some(r=>r.kind===k)):[])]:c.protocol==='iec104'?['yx','yc','mc','position','yk','yt','step']:['holding','input','coil','discrete'];for(const kind of types)if(![...$('point-kind').options].some(o=>o.value===kind))(()=>{const o=document.createElement('option');o.value=kind;o.textContent=kind;$('point-kind').add(o);})();if(types.length&&!types.includes($('point-kind').value)){$('point-kind').value=types[0];applyRangeRequest();}if(!types.length)$('point-kind').value='';const key=JSON.stringify([selectedDevice,types,$('point-kind').value]);if(key===pointTabsKey)return;pointTabsKey=key;if(types.some(t=>t.startsWith('tab:')))applyRangeRequest();$('point-tabs').hidden=!types.length;$('point-tabs').innerHTML=types.map(kind=>`<button role="tab" aria-selected="${kind===$('point-kind').value}" class="${kind===$('point-kind').value?'selected':''}" data-point-kind="${kind}">${kind.startsWith('tab:')?tabNames[kind.slice(4)]:kinds[kind]}</button>`).join('');}
function pointMatchesKind(p,kind){if(!kind)return true;if(!kind.startsWith('tab:'))return p.kind===kind;const range=activeRange();if(p.tab)return p.tab===kind.slice(4);return !!range&&p.kind===range.kind&&p.address>=range.address&&p.address<range.address+range.count;}
function pointEncoding(p){const t=p.wireType,dt=p.dataType;let type='uint',bytes=2;if(['yx','yk','coil','discrete'].includes(p.kind))bytes=1;else if(dt){type=dt.startsWith('int')?'signed':dt==='float32'?'float':'uint';bytes=dt.endsWith('32')?4:2;}else if(p.kind==='mc'){bytes=4;type='signed';}else if(p.kind==='position'){bytes=1;type='signed';}else if(p.kind==='step'||[11,35].includes(t))type='signed';else if([9,21,34].includes(t))type='normalized';else if(['yc','yt'].includes(p.kind)){bytes=4;type='float';}return {type,bytes,order:p.byteOrder||''};}
function pointHex(p){const f=pointEncoding(p),b=new ArrayBuffer(f.bytes),v=new DataView(b);if(f.type==='float')v.setFloat32(0,p.value);else if(f.bytes===4)v.setUint32(0,p.value);else if(f.bytes===2)v.setUint16(0,f.type==='normalized'?Math.min(32767,Math.round(p.value*32768)):p.value);else v.setUint8(0,p.value);let raw=[...new Uint8Array(b)];if(f.order){const chars=f.bytes===4?'ABCD':'AB';raw=[...f.order].map(c=>raw[chars.indexOf(c)]);}return raw.map(n=>n.toString(16).padStart(2,'0')).join('').toUpperCase();}
function hexPointValue(p,hex){const f=pointEncoding(p);if(!new RegExp('^[0-9a-fA-F]{1,'+(f.bytes*2)+'}$').test(hex))throw Error('请输入有效的十六进制码值');let raw=hex.padStart(f.bytes*2,'0').match(/../g).map(n=>parseInt(n,16));if(f.order){const out=raw.slice(),chars=f.bytes===4?'ABCD':'AB';[...f.order].forEach((c,i)=>out[chars.indexOf(c)]=raw[i]);raw=out;}const v=new DataView(Uint8Array.from(raw).buffer);if(f.type==='float')return v.getFloat32(0);if(f.type==='normalized')return v.getInt16(0)/32768;if(f.type==='signed')return f.bytes===4?v.getInt32(0):f.bytes===2?v.getInt16(0):v.getInt8(0);return f.bytes===4?v.getUint32(0):f.bytes===2?v.getUint16(0):v.getUint8(0);}
function pointValueError(input,text){input.setAttribute('aria-invalid','true');input.title=text;toast(text,true);}
function parsePointCell(edit,raw){
 const key=edit.target.key,field=edit.field,bit=['yx','yk','coil','discrete'].includes(key.kind)&&![3,31].includes(edit.target.point?.wireType),register=['holding','input'].includes(key.kind),step=key.kind==='step';
 if(field==='name'){if(raw.length>200)throw Error('描述最多200字');return raw;}
 let value=Number(raw.trim());
 if(field==='hex'){
  value=hexPointValue(edit.target.point||{kind:key.kind},raw.trim().replace(/^0x/i,''));
 }
 if(!raw.trim()||!Number.isFinite(value))throw Error('请输入有效数值');
 if(field==='address'){const max=edit.target.protocol.startsWith('modbus')?65535:16777215;if(!Number.isInteger(value)||value<0||value>max)throw Error('地址应为 0–'+max+' 整数');}
 else if(field==='quality'){if(!Number.isInteger(value)||value<0||value>255)throw Error('品质应为 0–255 整数，可用0x十六进制');}
 else if(bit&&![0,1].includes(value))throw Error('位点值只能为 0 或 1');
 else if(register){const type=edit.target.point?.dataType||'uint16',bounds={uint16:[0,65535],int16:[-32768,32767],uint32:[0,4294967295],int32:[-2147483648,2147483647],float32:[-3.4028234663852886e38,3.4028234663852886e38]}[type];if(!bounds||value<bounds[0]||value>bounds[1]||type!=='float32'&&!Number.isInteger(value))throw Error('数值超出 '+type+' 范围');}
 else if(step&&(!Number.isInteger(value)||value< -32768||value>32767))throw Error('步调节应为 -32768–32767 整数');
 else if(Math.abs(value)>3.4028234663852886e38)throw Error('数值超出短浮点范围');
 return value;
}
async function commitPointValue(input){
 const edit=input.pointEdit;if(!edit||edit.pending||!edit.dirty)return;let value;
 try{value=parsePointCell(edit,input.value);}catch(error){pointValueError(input,error.message);return;}
 if(value===edit.base){edit.dirty=false;input.removeAttribute('aria-invalid');return;}
 edit.pending=true;input.readOnly=true;input.setAttribute('aria-busy','true');const target=edit.target;
 target.tail=target.tail.then(async()=>{
  try{
   const key={...target.key},field=edit.field==='hex'?'value':edit.field,telemetry=target.role==='server'&&(target.protocol==='iec104'?['yx','yc'].includes(key.kind):target.protocol.startsWith('modbus'));
   const result=field==='value'&&telemetry?await api('SetPointValue',target.channel,target.device,key,value):await api('EditPointCell',target.channel,target.device,key,field,{[field]:value});
   if(field==='address'){target.key.address=value;target.row.dataset.pointKey=keyOf(target.key);}
   edit.base=value;edit.dirty=false;input.removeAttribute('aria-invalid');
   toast(result?.mode==='reported'?'已提交上送':result?.mode==='polled'?'已更新，等待主站读取':result?.mode==='offline'?'已保存，未连接或104传输未激活，未上送':'已本地保存');
  }catch(error){pointValueError(input,String(error?.message||error));}
  finally{edit.pending=false;input.readOnly=false;input.removeAttribute('aria-busy');pointKey='';await refresh(true);}
 });
 return target.tail;
}
function commitPointRow(row){for(const input of row.querySelectorAll('.point-cell-input'))commitPointValue(input);}
function renderPointCell(cell,target,p,field){
 let input=cell.querySelector('.point-cell-input');
 if(!input||input.pointEdit.target!==target){if(input)commitPointValue(input);input=document.createElement('input');input.type='text';input.className='point-cell-input'+(field==='value'?' point-value-input':'');input.dataset.pointField=field;input.inputMode=field==='name'?'text':field==='hex'?'text':field==='value'?'decimal':'numeric';input.setAttribute('aria-label',({address:'地址',hex:'码值',value:'数值',quality:'品质',name:'描述'})[field]+' '+p.address);if(field==='name')input.maxLength=200;input.pointEdit={target,field,base:field==='hex'?p.value:p[field],dirty:false,pending:false};cell.replaceChildren(input);
  input.addEventListener('input',()=>{input.pointEdit.dirty=true;input.removeAttribute('aria-invalid');});
  input.addEventListener('blur',()=>commitPointValue(input).then(()=>{pointKey='';renderPoints(current());}));
  input.addEventListener('keydown',e=>{if(e.isComposing)return;if(e.key==='Enter'){e.preventDefault();e.stopPropagation();commitPointValue(input);}else if(e.key==='Escape'&&!input.pointEdit.pending){e.preventDefault();e.stopPropagation();input.value=input.pointEdit.displayBase;input.pointEdit.dirty=false;input.removeAttribute('aria-invalid');}});
 }
 if(!input.pointEdit.dirty&&!input.pointEdit.pending&&document.activeElement!==input){input.value=field==='hex'?pointHex(p):String(p[field]??'');input.pointEdit.displayBase=input.value;input.pointEdit.base=field==='hex'?p.value:p[field];input.title=field==='quality'?'0 正常；可输入十进制或0x品质码':field==='value'&&['yx','yk','coil','discrete'].includes(p.kind)?'0 分 / 1 合；回车或离开提交':'回车或离开提交，Esc取消';}
}
function renderPoints(c){
  const query=$('point-search').value.trim().toLowerCase(),kind=$('point-kind').value;const key=JSON.stringify([c?.id,c?.role,c?.protocol,selectedDevice,c?.points,query,kind,pointPage]);if(key===pointKey)return;pointKey=key;
  const all=(c?.points||[]).filter(p=>pointMatchesKind(p,kind)&&`${p.address} ${p.name} ${kinds[p.kind]}`.toLowerCase().includes(query)).sort((a,b)=>a.address-b.address||a.kind.localeCompare(b.kind));
  const pages=Math.max(1,Math.ceil(all.length/100));pointPage=Math.min(pointPage,pages-1);const points=all.slice(pointPage*100,pointPage*100+100);
  $('point-badge').textContent=`${all.length} 点`;$('point-badge').title=`当前列表 ${all.length} 点，装置共 ${c?.points.length||0} 点`;$('point-empty').textContent=query||kind?'无匹配结果':'暂无数据';$('point-empty').hidden=points.length>0;
  $('point-pager').hidden=pages===1;$('point-page').textContent=`${pointPage+1} / ${pages}`;$('point-prev').disabled=pointPage===0;$('point-next').disabled=pointPage===pages-1;
  const scroll=$('point-scroll').scrollTop,tbody=$('point-rows');const existing=new Map([...tbody.children].map(row=>[row.dataset.pointKey,row]));
  points.forEach((p,index)=>{const key=keyOf(p);let row=existing.get(key);
    if(!row){row=document.createElement('tr');row.dataset.pointKey=key;for(let n=0;n<5;n++)row.append(document.createElement('td'));row.children[2].className='point-value';}
    existing.delete(key);
    if(!row.pointTarget||row.pointTarget.channel!==c.id||row.pointTarget.device!==selectedDevice){commitPointRow(row);row.pointTarget={channel:c.id,device:selectedDevice,key:{kind:p.kind,address:p.address},role:c.role,protocol:c.protocol,tail:Promise.resolve(),row};}
    row.pointTarget.point=p;row.title=p.timestamp?`事件时间 ${p.timestamp}${p.timeInvalid?'（时间无效）':''}`:'';
 ['address','hex','value','quality','name'].forEach((field,n)=>renderPointCell(row.children[n],row.pointTarget,p,field));
    if(tbody.children[index]!==row)tbody.insertBefore(row,tbody.children[index]||null);
  });for(const row of existing.values()){commitPointRow(row);row.remove();}$('point-scroll').scrollTop=scroll;
}
function filteredMessages(){const source=paused?frozen:snapshot.messages,query=$('message-search').value.trim().toLowerCase().replace(/\s+/g,''),direction=$('direction-filter').value;return source.filter(m=>($('all-messages').checked||m.channelId===selected)&&(!direction||direction===m.direction)&&(!query||(m.summary+m.hex+m.peer).toLowerCase().replace(/\s+/g,'').includes(query)));}
function renderMessages(){
  const source=paused?frozen:snapshot.messages,query=$('message-search').value.trim().toLowerCase().replace(/\s+/g,''),direction=$('direction-filter').value;
  const all=filteredMessages();
  const list=all.slice(-300);const key=JSON.stringify([selected,paused,query,direction,$('all-messages').checked,list.map(m=>m.sequence)]);if(key===messageKey)return;messageKey=key;
  $('message-badge').textContent=all.length>300?`300 / ${all.length}`:String(all.length);$('message-badge').title='显示最近 300 条匹配报文；导出包含当前缓存全部报文';$('message-empty').hidden=!!list.length;$('message-empty').textContent=query||direction?'无匹配报文':'暂无报文';
  const scroller=$('message-scroll'),oldTop=scroller.scrollTop;const append=shownMessages.length&&list.length>=shownMessages.length&&shownMessages.every((m,i)=>list[i]?.sequence===m.sequence);
  const row=m=>`<tr><td>${escape(m.time)}</td><td><span class="direction ${escape(m.direction)}">${escape({TX:'Send',RX:'Rec'}[m.direction]||m.direction)}</span></td><td><div class="message-line"><span class="message-summary">${escape(m.summary)}${$('all-messages').checked?' · '+escape(snapshot.project.channels.find(c=>c.id===m.channelId)?.name||m.channelId):''}</span>${m.hex?`<button class="row-button copy-message" data-copy-message="${m.sequence}">复制</button>`:''}</div>${m.hex?`<div class="message-hex">${escape(m.hex.match(/.{1,2}/g)?.join(' '))}</div>`:''}</td></tr>`;
  if(append){const rows=document.createElement('tbody');rows.innerHTML=list.slice(shownMessages.length).map(row).join('');$('message-rows').append(...rows.children);}else $('message-rows').innerHTML=list.map(row).join('');shownMessages=list;
  scroller.scrollTop=$('follow-messages').checked?scroller.scrollHeight:oldTop;
}

function ask({title,text='',input=null,choices=[['cancel','取消'],['ok','确认']],danger=false}){
  if(nativeDesktop&&nativeContext?.kind!=='confirm-dialog')return api('AskNative',{title,text,input,choices,danger});
  if(confirmResolver)return Promise.resolve({choice:'cancel'});$('confirm-title').textContent=title;$('confirm-text').textContent=text;$('confirm-text').hidden=!text;$('confirm-input').hidden=input===null;$('confirm-input').required=input!==null;$('confirm-input').value=input??'';$('confirm-input').setAttribute('aria-label',text==='名称'?'名称':title);
  $('confirm-actions').replaceChildren(...choices.map(([value,label],index)=>{const button=document.createElement('button');button.type='submit';button.value=value;button.textContent=label;button.formNoValidate=value==='cancel';if(index===choices.length-1)button.className=danger?'danger-solid':'primary';return button;}));
  $('confirm-dialog').showModal();if(input!==null){$('confirm-input').focus();$('confirm-input').select();}else $('confirm-actions').querySelector('button')?.focus();return new Promise(resolve=>{confirmResolver=resolve;});
}
function finishAsk(choice){if(nativeContext?.kind==='confirm-dialog'){api('ResolveNativeAsk',{choice,value:$('confirm-input').value.trim()}).then(()=>window.go.main.App.CloseNativeWindow()).catch(report);return;}const resolve=confirmResolver;confirmResolver=null;const value=$('confirm-input').value.trim();$('confirm-dialog').close();resolve?.({choice,value});}
$('confirm-form').addEventListener('submit',e=>{e.preventDefault();finishAsk(e.submitter?.value||'cancel');});$('confirm-dialog').addEventListener('cancel',e=>{e.preventDefault();finishAsk('cancel');});
$('confirm-input').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();submitWithButton($('confirm-form'),$('confirm-actions').lastElementChild);}});
async function confirmAction(title,text,danger=false){return(await ask({title,text,danger})).choice==='ok';}
async function prepareReplace(){const dirty=snapshot.dirty,hasActive=snapshot.states.some(active);if(!dirty&&!hasActive)return true;
  const answer=await ask({title:'更换工程',text:dirty?'保存当前更改？更换后会停止现有通道。':'将停止现有通道。',choices:dirty?[['cancel','取消'],['discard','不保存'],['save','保存并继续']]:[['cancel','取消'],['ok','继续']]});
  if(answer.choice==='cancel')return false;if(answer.choice==='save'){if(!await act('SaveProject',false))return false;}return true;
}
function splitEndpoint(value){const m=/^\[([^\]]+)\]:(\d+)$/.exec(value)||/^(.+):(\d+)$/.exec(value);return m?[m[1],m[2]]:[value,'0'];}
function endpoint(host,port,allowZero=false){
 host=String(host??'').trim().replace(/^\[(.*)\]$/,'$1');if(allowZero&&host.toLowerCase()==='any')host='0.0.0.0';port=String(port??'').trim();
 if(!host)throw Error('请填写 IP 或主机名');
 if(!/^\d+$/.test(port)||Number(port)>(65535)||Number(port)<(allowZero?0:1))throw Error('端口应为 '+(allowZero?'0':'1')+'–65535 整数');
 return (host.includes(':')?'['+host+']':host)+':'+Number(port);
}
function originalControl(host,id){return host.querySelector('[data-original-id="'+id+'"]');}
const networkEnabled=new Set(['rbCom','rbTcp','rbUDP','rbClient','rbServer','cbComList','cbBaudRate','cbDataBit','cbParity','cbStopBit','txtClientIP','txtClientPort','cbxServerIP','txtServerPort','ck_AssignIpPort','cb_ClientAssignIp','txt_ClientAssignPort']);
let networkChoicesGeneration=0,configWindowFit=Promise.resolve();
function fitConfigWindow(){
 if(!nativeContext||!['channel-dialog','device-dialog'].includes(nativeContext.kind))return;
 configWindowFit=new Promise(resolve=>requestAnimationFrame(()=>{
  const form=$(nativeContext.kind==='channel-dialog'?'channel-form':'device-form');
  const width=nativeContext.kind==='channel-dialog'?480:!$('device-protocol').value?320:360;
  const height=Math.max(110,Math.min(Math.ceil(form.getBoundingClientRect().height)+20,(screen.availHeight||900)-96));
  window.go.main.App.FitConfigWindow(window.innerWidth,window.innerHeight,width,height).catch(report).finally(resolve);
 }));
 return configWindowFit;
}
async function loadNetworkChoices(){
 const generation=++networkChoicesGeneration;
 const results=await Promise.allSettled([api('SerialPorts'),api('LocalIPs')]);
 if(generation!==networkChoicesGeneration)return;
 const ports=results[0].status==='fulfilled'&&Array.isArray(results[0].value)?results[0].value:[];
 const ips=results[1].status==='fulfilled'&&Array.isArray(results[1].value)?results[1].value:['Any'];
 for(const [id,options]of [['cbComList',ports],['cbxServerIP',ips],['cb_ClientAssignIp',ips.filter(ip=>ip!=='Any')]]){
  const list=$('choices-'+id);if(list)list.innerHTML=options.map(v=>`<option value="${escape(v)}"></option>`).join('');
 }
 const input=originalControl($('original-network-panel'),'cbComList');
 if(input&&!input.value&&ports.length)input.value=ports[0];
 $('network-port-status').textContent=ports.length?'串口 '+ports.length+' 个':'无串口';
}
function channelDialog(edit=true){
 const c=channel();if(!c)return;const f=$('channel-form');f.reset();f.dataset.section='transport';f.dataset.channel=c.id;
 f.querySelector('.form-error').hidden=true;
 for(const el of f.querySelectorAll('.form-grid input,.form-grid select'))el.disabled=true;
 for(const[k,v]of Object.entries(c))if(field(f,k))field(f,k).value=v;
 for(const name of ['unit','commonAddress'])field(f,name).value=1;
 const [host,port]=splitEndpoint(c.address),[local,localPort]=splitEndpoint(c.localAddress||'127.0.0.1:7777');
 const v={cbComList:'',cbBaudRate:String(c.baud),cbDataBit:String(c.dataBits),cbParity:{N:'无校验',O:'奇校验',E:'偶校验'}[c.parity],cbStopBit:String(c.stopBits),txtClientIP:'127.0.0.1',txtClientPort:'2404',cbxServerIP:'Any',txtServerPort:'2404',ck_AssignIpPort:false,cb_ClientAssignIp:local,txt_ClientAssignPort:localPort,...c.transportDraft,
  rbCom:c.transport==='serial',rbTcp:c.transport.startsWith('tcp'),rbUDP:c.transport==='udp',rbClient:c.transport!=='tcp-server',rbServer:c.transport==='tcp-server'};
 if(c.transport==='serial')v.cbComList=c.address;
 else if(c.transport==='tcp-server'){v.cbxServerIP=host==='0.0.0.0'?'Any':host;v.txtServerPort=port;}
 else{v.txtClientIP=host;v.txtClientPort=port;}
 if(c.transport==='udp'){v.cbxServerIP=c.localAddress?(local==='0.0.0.0'?'Any':local):'Any';v.txtServerPort=c.localAddress?localPort:'0';v.rbServer=c.transportDraft?.rbServer==='true';v.rbClient=!v.rbServer;}
 else if(c.transport==='tcp-client'&&c.localAddress){v.ck_AssignIpPort=true;v.cb_ClientAssignIp=local;v.txt_ClientAssignPort=localPort;}
 globalThis.renderOriginalForm($('original-network-panel'),'network',networkEnabled,v);
 const panel=$('original-network-panel'),net=panel.querySelector('[data-group-id="groupBoxEx3"]'),roles=document.createElement('div');
 roles.className='network-role-row';roles.append(originalControl(panel,'rbClient').closest('label'),originalControl(panel,'rbServer').closest('label'));net.querySelector('.original-group-title').after(roles);
 const other=panel.querySelector('[data-group-id="groupControl1"] .original-group-title');if(other)other.textContent='本地绑定';
 panel.querySelector('.original-submit').insertAdjacentHTML('afterbegin','<button type="button" data-close="channel-dialog">取消</button>');
 $('network-ports').textContent='刷新';

 $('channel-dialog-title').textContent='通信配置窗口';updateChannelFields();
 if(!$('channel-dialog').open)$('channel-dialog').show();
 originalControl($('original-network-panel'),'rbTcp')?.focus();loadNetworkChoices().catch(report);fitConfigWindow();
}
function updateChannelFields(){
 const host=$('original-network-panel');if(!host.children.length)return;
 const get=id=>originalControl(host,id),com=get('rbCom').checked,udp=get('rbUDP').checked,server=get('rbServer').checked;
 const bind=!com&&!udp&&!server&&get('ck_AssignIpPort').checked;
 for(const id of ['cbComList','cbBaudRate','cbDataBit','cbParity','cbStopBit'])get(id).disabled=!com;
 get('rbClient').disabled=com;get('rbServer').disabled=com;get('ck_AssignIpPort').disabled=com||udp||server;
 for(const [id,enabled,port] of [
  ['txtClientIP',!com&&(!server||udp),false],['txtClientPort',!com&&(!server||udp),true],
  ['cbxServerIP',!com&&(server||udp),false],['txtServerPort',!com&&(server||udp),true],
  ['cb_ClientAssignIp',bind,false],['txt_ClientAssignPort',bind,true]
 ]){
  const input=get(id);input.disabled=!enabled;input.required=enabled;const type=port?'number':'text';if(input.type!==type)input.type=type;
  if(port){input.min=id==='txtClientPort'?'1':'0';input.max='65535';input.step='1';}
 }
 const c=channel(),st=snapshot.states.find(st=>st.id===c?.id)||{},linked=active(st);
 host.querySelector('[data-group-id="groupBoxEx2"]').hidden=!com;
 host.querySelector('[data-group-id="groupBoxEx3"]').hidden=com;
 host.querySelector('[data-group-id="groupControl1"]').hidden=com||udp||server;
 for(const id of ['txtClientIP','txtClientPort','cbxServerIP','txtServerPort'])get(id).closest('label').hidden=com||get(id).disabled;
 $('network-port-status').hidden=!com;
 setHTML('network-summary','<strong>'+escape(c?.name||'未选择通道')+'</strong><span class="network-state '+(linked?'online':'')+'">'+escape(statuses[st.status]||'已停止')+'</span>');
 const submit=host.querySelector('button[type=submit]');if(submit.textContent!=='应用')submit.textContent='应用';submit.title=linked?'通信参数变化后停止通道，保存后请手动连接；自动任务结束':'保存通信配置';
 if(!c||busy)for(const input of host.querySelectorAll('input,select'))input.disabled=true;
}
function nextDefaultName(prefix,names,start=0){const used=new Set(names);let n=start;while(used.has(prefix+n))n++;return prefix+n;}
on('add-channel','click',async()=>{
 const role=stationMode,defaultName=nextDefaultName('通道',snapshot.project.channels.filter(c=>c.role===role).map(c=>c.name),1);
 const result=await ask({title:'新增通道',text:'名称',input:defaultName});if(result.choice!=='ok')return;
 const name=result.value.trim();if(!name||[...name].length>100)throw Error('名称应为 1–100 字');
 const c={name,protocol:'raw',role,transport:role==='server'?'tcp-server':'tcp-client',address:role==='server'?'0.0.0.0:2404':'127.0.0.1:2404',localAddress:'',timeoutMs:3000,unit:1,commonAddress:1,baud:9600,dataBits:8,parity:'N',stopBits:1,points:[]};
 const id=await act('UpsertChannel',c);selectChannel(id);
});
on('edit-channel','click',()=>channelDialog(true,'transport'),false);on('device-config','click',()=>deviceDialog(true),false);on('add-device','click',()=>deviceDialog(false),false);
on('channel-form','submit',async e=>{
 e.preventDefault();
 if(nativeContext?.kind==='channel-dialog'){const c=await window.go.main.App.GetNativeContext();selected=c.channel;await refresh(true);}
 const c=channel();if(!c)throw Error('通道已被移除');
 const v=globalThis.readOriginalForm($('original-network-panel')),com=v.rbCom,udp=v.rbUDP,server=!com&&!udp&&v.rbServer;
 const transport=com?'serial':udp?'udp':server?'tcp-server':'tcp-client';
 const data={...c,transport,
  address:com?v.cbComList.trim():endpoint(server?v.cbxServerIP:v.txtClientIP,server?v.txtServerPort:v.txtClientPort,server),
  localAddress:udp?endpoint(v.cbxServerIP,v.txtServerPort,true):!com&&!server&&v.ck_AssignIpPort?endpoint(v.cb_ClientAssignIp,v.txt_ClientAssignPort,true):'',
  baud:Number(v.cbBaudRate),dataBits:Number(v.cbDataBit),parity:{'无校验':'N','奇校验':'O','偶校验':'E'}[v.cbParity],stopBits:Number(v.cbStopBit),
  transportDraft:Object.fromEntries(Object.entries(v).map(([key,value])=>[key,String(value)]))};
 try{await act('UpdateTransport',data);}catch(error){await refresh(true);throw error;}$('channel-dialog').close();toast('通信配置已应用；参数变更后请手动连接');
});
on('channel-form','change',e=>{if(!e.target.matches('[type=radio],[type=checkbox]'))return;updateChannelFields();fitConfigWindow();},false);
on('list-ports','click',loadNetworkChoices);
const deviceWindowCache=new Map(),devicePanelCache=new Map();
$('device-protocol').addEventListener('focus',rememberDevicePanel);
$('original-device-panel').addEventListener('input',rememberDevicePanel);
$('original-device-panel').addEventListener('change',rememberDevicePanel);
function rememberDevicePanel(){const f=$('device-form');if(!f.dataset.role||f.dataset.device)return;const role=f.dataset.role,group=$('device-protocol').value;if(group){devicePanelCache.set(role+'/'+group,{...globalThis.readOriginalForm($('original-device-panel')),...Object.fromEntries([...$('original-device-panel').querySelectorAll('[data-extra-setting]')].map(el=>[el.dataset.extraSetting,String(el.type==='checkbox'?el.checked:el.value)]))});deviceWindowCache.set(role,{group,version:$('device-version').value});if(nativeContext?.kind==='device-dialog'&&!nativeContext.edit)api('SaveNativeDraft',{window:deviceWindowCache.get(role),panels:[...devicePanelCache].filter(([k])=>k.startsWith(role+'/'))}).catch(report);}}
function supportedDeviceControls(group){
 const key=$('device-form').dataset.role+'-'+group, controls=globalThis.originalForms[key]?.controls||{};if(group==='modbus'&&controls.cb_ycFormat)controls.cb_ycFormat.options=['0:无符号短整型(Ushort)','1:有符号短整型(Short)','2:无符号整型(UInt)','3:有符号整型(Int)','4:单精度浮点(Float)'];
 const excluded=new Set(['ckbEnableEncrypt','txt_FrameOverTime','cbSOE_Type','txtDZ_InitConfig','txt_ULNum','txt_ULFirstAddr','txt_VYXNum','txt_VYXFirstAdd']);
 return new Set(Object.values(controls).filter(c=>['TextEdit','ComboBoxEdit','CheckEdit'].includes(c.type)&&!excluded.has(c.id)).map(c=>c.id));
}
function renderExtraSettings(values,group,role){
 const host=$('original-device-panel'),submit=host.querySelector('.original-submit');
 const fields=group==='iec104'?[['sendWindow','未确认发送窗口',12],['ackDelayMs','S帧最长等待(ms)',1000],...(role==='server'?[['responseMode','应答策略','normal',['normal','deny','silent']],['responseDelayMs','应答延迟(ms)',0]]:[])]:[['pollEnabled','周期采集',true],['eventCount','事件读取寄存器数',16],...(role==='server'?[['clockEnabled','启用校时寄存器',false],['TimeAddr','校时首地址','1000H']]:[])];
 const section=document.createElement('section');section.className='original-group';section.innerHTML='<div class="original-group-title">'+(group==='iec104'?'链路与应答':'采集与校时')+'</div>'+fields.map(([key,label,initial,options])=>{const v=values[key]??initial;return options?`<label class="original-field"><span>${label}</span><select data-extra-setting="${key}">${options.map(o=>`<option value="${o}" ${o===v?'selected':''}>${{normal:'正常',deny:'否认',silent:'不应答'}[o]}</option>`).join('')}</select></label>`:typeof initial==='boolean'?`<label class="original-check"><input type="checkbox" data-extra-setting="${key}" ${v===true||v==='true'?'checked':''}>${label}</label>`:`<label class="original-field"><span>${label}</span><input data-extra-setting="${key}" type="${typeof initial==='number'?'number':'text'}" value="${escape(v)}"></label>`;}).join('');host.insertBefore(section,submit);
}
function updateDeviceFields(restore=null){const f=$('device-form'),group=$('device-protocol').value,role=f.dataset.role;if(!group){$('original-device-panel').replaceChildren();fitConfigWindow();return;}const protocol=group==='iec104'?'iec104':$('device-version').value==='TCP版'?'modbus-tcp':'modbus-rtu';field(f,'protocol').value=protocol;const values=restore||devicePanelCache.get(role+'/'+group)||{},enabled=supportedDeviceControls(group);globalThis.renderOriginalForm($('original-device-panel'),role+'-'+group,enabled,values,false);renderExtraSettings(values,group,role);updateByteOrders();const addressID=role==='server'&&group==='iec104'?'txt_DeviceAddr':'txtAdd',address=originalControl($('original-device-panel'),addressID)?.value||'1';field(f,protocol==='iec104'?'commonAddress':'unit').value=address;fitConfigWindow();}
function fillDeviceVersions(group,value){const versions=group==='iec104'?['标准版']:group==='modbus'?['标准版(RTU)','TCP版']:[];$('device-version').innerHTML=versions.map(v=>`<option >${v}</option>`).join('');$('device-version').value=versions.includes(value)?value:versions[0]||'';}
function deviceDialog(edit=false){const c=channel(),d=edit?device():null;if(edit&&!d)return;rememberDevicePanel();const f=$('device-form');f.reset();f.querySelector('.form-error').hidden=true;f.dataset.role=stationMode;f.dataset.device=d?.id||'';f.dataset.channel=edit?c.id:'';field(f,'name').value=d?.name||nextDefaultName('装置',(c?.devices||[]).map(d=>d.name),1);field(f,'name').readOnly=false;f.deviceInitial=d?JSON.stringify(d.configuration):null;
 const suffix=stationMode==='server'?'子站':'主站';$('device-dialog-title').textContent=edit?'查看/修改装置配置':suffix+'增加装置';$('device-protocol').innerHTML='<option value="">请选择</option>'+[['iec104','104'+suffix],['modbus','Modbus'+suffix]].map(([key,name])=>`<option value="${key}" >${name}</option>`).join('');
 const saved=deviceWindowCache.get(stationMode),group=edit?(c.protocol==='iec104'?'iec104':'modbus'):saved?.group||'iec104';const version=edit?(c.protocol==='iec104'?'标准版':c.protocol==='modbus-tcp'?'TCP版':'标准版(RTU)'):saved?.version;$('device-protocol').value=group;$('device-protocol').disabled=edit;fillDeviceVersions(group,version);$('device-version').disabled=edit;
 let values=null;if(edit){values={...(d.configuration?.values||{})};values[stationMode==='server'&&group==='iec104'?'txt_DeviceAddr':'txtAdd']=String(group==='iec104'?d.commonAddress:d.unit);if(!d.configuration)for(const [prefix,kind]of Object.entries(group==='iec104'?{YX:'yx',YC:'yc',MC:'mc',YK:'yk'}:{YX:'discrete',YC:'holding',MC:'input',YK:'coil'})){const points=d.points.filter(p=>p.kind===kind);values['txt'+prefix+'_Add']=String(points.length?Math.min(...points.map(p=>p.address)):0);values['txt'+prefix+'_Num']=String(points.length);}}
 if(edit){for(const [prefix,tab,kind] of (group==='iec104'?[['YX','yx','yx'],['YC','yc','yc'],['MC','mc','mc'],['YK','yk','yk']]:[['YX','yx','discrete'],['YC','yc','holding'],['MC','mc','input'],['YK','yk','coil'],['JDQ','jdq','coil']])){const range=d.configuration?.ranges?.find(r=>r.tab===tab),points=d.points.filter(p=>range?p.tab===tab||!p.tab&&p.kind===range.kind:p.tab===tab||!p.tab&&p.kind===kind&&tab!=='jdq');if(values['txt'+prefix+'_Add']===undefined)values['txt'+prefix+'_Add']=String(range?.address??(points.length?Math.min(...points.map(p=>p.address)):0));if(values['txt'+prefix+'_Num']===undefined)values['txt'+prefix+'_Num']=String(range?.count??points.length);}
 if(!d.configuration&&group==='modbus'){for(const [tab,kind] of [['yc','holding'],['mc','input']]){const p=d.points.find(p=>p.kind===kind);if(!p?.dataType)continue;const typeIndex=(tab==='mc'?['uint32','int32','float32']:['uint16','int16','uint32','int32','float32']).indexOf(p.dataType);if(typeIndex>=0)values['cb_'+tab+(stationMode==='server'?'Type':'Format')]=String(typeIndex);const orderIndex=(p.dataType.endsWith('32')?['ABCD','BADC','CDAB','DCBA']:['AB','BA']).indexOf(p.byteOrder||'');if(orderIndex>=0)values['cb_'+tab+'FunWay']=String(orderIndex);}}}
 updateDeviceFields(values);f.deviceInitial=edit?JSON.stringify(configurationFromPanel(field(f,'protocol').value)):null;f.deviceInitialName=d?.name;f.deviceInitialAddress=edit?(c.protocol==='iec104'?d.commonAddress:d.unit):null;if(!$('device-dialog').open)$('device-dialog').show();if(!edit)$('device-protocol').focus();}
on('device-protocol','change',()=>{fillDeviceVersions($('device-protocol').value);updateDeviceFields();},false);on('device-version','change',()=>{rememberDevicePanel();updateDeviceFields();},false);
function configurationFromPanel(protocol){const values=globalThis.readOriginalForm($('original-device-panel')),ranges=[],modbus=protocol.startsWith('modbus');const definitions=modbus?[['YX','yx','遥信'],['YC','yc','遥测'],['MC','mc','脉冲'],['YK','yk','遥控'],...($('device-form').dataset.role==='server'?[['JDQ','jdq','继电器']]:[])]:[['YX','yx','遥信'],['YC','yc','遥测'],['MC','mc','脉冲'],['YK','yk','遥控']];
 for(const[prefix,tab,name]of definitions){const rawAddress=String(values['txt'+prefix+'_Add']??'').trim(),rawCount=String(values['txt'+prefix+'_Num']??'').trim(),address=Number(rawAddress),count=Number(rawCount);if(!rawAddress||!rawCount||!Number.isInteger(address)||!Number.isInteger(count)||address<0||count<0)throw Error(name+'首地址和个数应为非负整数');let kind=tab;if(modbus){const fc=Number(values[(prefix==='YX'?'cbYX_Fun':prefix==='YC'?'cbYC_Fun':'txt'+prefix+'_Fun')]??values['txt'+prefix+'_Fun']??values['txtYK_fun']);const allowed=prefix==='YX'?[1,2]:prefix==='YC'||prefix==='MC'?[3,4]:prefix==='YK'?[5,6]:[1];if(!allowed.includes(fc))throw Error(name+'功能码仅支持 '+allowed.join('/'));kind={1:'coil',2:'discrete',3:'holding',4:'input',5:'coil',6:'holding'}[fc];}ranges.push({tab,kind,address,count,name});}
 const activeValues=Object.fromEntries(Object.entries(values).filter(([key])=>supportedDeviceControls(modbus?'modbus':'iec104').has(key)).map(([key,value])=>[key,String(value)]));for(const el of $('original-device-panel').querySelectorAll('[data-extra-setting]'))activeValues[el.dataset.extraSetting]=String(el.type==='checkbox'?el.checked:el.value);return {version:$('device-version').value,values:activeValues,ranges};}
on('device-form','submit',async e=>{e.preventDefault();if(nativeContext?.kind==='device-dialog'&&!nativeContext.edit){const context=await window.go.main.App.GetNativeContext();selected=context.channel;await refresh(true);}
 const f=$('device-form');const c=f.dataset.device?snapshot.project.channels.find(c=>c.id===f.dataset.channel):channel();if(!c||c.role!==f.dataset.role)throw Error('请先增加并选中通道');const protocol=field(f,'protocol').value;if(!protocol)throw Error('请选择规约和版本');const name=field(f,'name').value.trim();if(!name||[...name].length>100)throw Error('名称应为 1–100 字');const configuration=configurationFromPanel(protocol),values=globalThis.readOriginalForm($('original-device-panel')),address=Number(values[f.dataset.role==='server'&&protocol==='iec104'?'txt_DeviceAddr':'txtAdd']);if(!String(values[f.dataset.role==='server'&&protocol==='iec104'?'txt_DeviceAddr':'txtAdd']??'').trim()||!Number.isInteger(address)||address<0||address>(protocol==='iec104'?65534:protocol==='modbus-rtu'?247:255)||(protocol==='iec104'&&address===0)||(protocol==='modbus-rtu'&&c.role==='server'&&address===0))throw Error('装置地址超出范围');
 if(f.dataset.device&&JSON.stringify(configuration)===f.deviceInitial&&name===f.deviceInitialName&&address===f.deviceInitialAddress){$('device-dialog').close();return;}
 let replace=false;const replace104=protocol==='iec104'&&c.protocol==='iec104'&&!f.dataset.device&&c.devices.length>0,running=snapshot.states.some(st=>st.id===c.id&&active(st));if(replace104||(protocol!==c.protocol&&(c.devices.length||running))){let text=replace104?'再次增加 IEC104 装置将替换原装置及其点表。':c.devices.length?`将移除当前通道的 ${c.devices.length} 个装置及其点表。`:'';if(running)text+='当前通道将停止。';if(!await confirmAction(replace104?'替换 IEC104 装置':'切换规约',text,!!c.devices.length))return;replace=!!c.devices.length;}
 const existing=c.devices.find(d=>d.id===f.dataset.device),d={id:replace?'':f.dataset.device,name,unit:protocol==='iec104'?1:address,commonAddress:protocol==='iec104'?address:1,points:[],configuration};await act('UpsertDevice',c.id,protocol,d,replace);if(f.dataset.device){$('device-dialog').close();}else{if(protocol.startsWith('modbus'))originalControl($('original-device-panel'),'txtAdd').value=String(Math.min(protocol==='modbus-rtu'?247:255,address+1));rememberDevicePanel();}$('device-dialog').close();toast(existing?'配置已保存，通道保持停止':'装置已增加');});
async function renameDevice(){await run('rename-device',async()=>{const d=device(),id=selected;if(!d)return;const result=await ask({title:'修改装置名称',input:d.name});if(result.choice==='ok'&&result.value)await act('RenameDevice',id,d.id,result.value);});}
async function removeDevice(){await run('remove-device',async()=>{const d=device(),id=selected;if(d&&await confirmAction('移除装置',`${d.name}及其点表将被移除。`,true)){await act('DeleteDevice',id,d.id);selectDevice(channel()?.devices[0]?.id||'');}});}
function pointDialog(key=null){const c=current();if(!c)return;const original=key?c.points.find(p=>keyOf(p)===key):null;if(key&&!original){toast('此点已被删除',true);return;}const f=$('point-form');f.reset();f.querySelector('.form-error').hidden=true;
  const allowed=c.protocol==='iec104'?['yx','yc','mc','position','yk','yt','step']:c.protocol.startsWith('modbus')?['coil','discrete','holding','input']:['yx','yc','holding'];field(f,'kind').innerHTML=allowed.map(k=>`<option value="${k}">${kinds[k]}</option>`).join('');
  const initialKind=allowed.includes($('point-kind').value)?$('point-kind').value:activeRange()?.kind||allowed[0];let next=0;const used=new Set(c.points.filter(p=>p.kind===initialKind).map(p=>p.address));while(used.has(next))next++;
  const p=original||{kind:initialKind,address:next,name:'',value:0,quality:0};for(const[k,v]of Object.entries(p))if(field(f,k))field(f,k).value=v;
  pointEditor={channel:c.id,device:selectedDevice,key:original?{kind:original.kind,address:original.address}:null,point:p,changed:new Set()};$('point-dialog-title').textContent=original?'编辑点':'添加点';field(f,'address').max=c.protocol.startsWith('modbus')?65535:16777215;updatePointLimits();$('point-dialog').showModal();field(f,original?'value':'address').focus();}
function updatePointLimits(){if($('point-feedback-label'))$('point-feedback-label').hidden=field($('point-form'),'kind').value!=='yk';const f=$('point-form'),kind=field(f,'kind').value,value=field(f,'value'),p=pointEditor?.point?.kind===kind?pointEditor.point:{kind};let min=-3.4028234663852886e38,max=3.4028234663852886e38,step='any';if(['yx','yk','coil','discrete'].includes(kind)){min=0;max=[3,31].includes(p.wireType)?3:1;step='1';}else if(kind==='step'){min=-32768;max=32767;step='1';}else if(kind==='position'){min=-64;max=63;step='1';}else if(kind==='mc'){min=-2147483648;max=2147483647;step='1';}else if(['holding','input'].includes(kind)){[min,max]=({uint16:[0,65535],int16:[-32768,32767],uint32:[0,4294967295],int32:[-2147483648,2147483647],float32:[min,max]})[p.dataType||'uint16'];step=p.dataType==='float32'?'any':'1';}else if([9,21,34].includes(p.wireType)){min=-1;max=32767/32768;}else if([11,35].includes(p.wireType)){min=-32768;max=32767;step='1';}value.min=String(min);value.max=String(max);value.step=step;}

on('add-point','click',()=>pointDialog(),false);on('point-form','input',e=>{if(e.target.name)pointEditor?.changed.add(e.target.name);},false);on('point-form','change',e=>{if(e.target.name)pointEditor?.changed.add(e.target.name);if(e.target.name==='kind')updatePointLimits();},false);
on('point-form','submit',async e=>{e.preventDefault();if(!pointEditor)return;const f=$('point-form'),editor=pointEditor;const p={kind:field(f,'kind').value,address:Number(field(f,'address').value),name:field(f,'name').value,value:Number(field(f,'value').value),quality:Number(field(f,'quality').value),feedback:field(f,'feedback').value.trim()};
  await act('PatchDevicePoint',editor.channel,editor.device,editor.key,p,[...editor.changed]);$('point-dialog').close();pointEditor=null;toast('已保存');});

document.addEventListener('click',async e=>{const target=e.target.closest('button');if(!target)return;
  if(target.dataset.mode&&!busy){rememberDevicePanel();for(const dialog of document.querySelectorAll('dialog[open]'))dialog.close();const ready=api('SetWorkspaceMode',target.dataset.mode);saveDraft();stationMode=target.dataset.mode;selectChannel(snapshot.project.channels.find(c=>c.role===stationMode)?.id||'');ready.then(()=>refresh(true)).catch(report);return;}
  if(target.dataset.device!==undefined&&!busy){selectDevice(target.dataset.device);return;}
  if(target.dataset.pointKind){$('point-kind').value=target.dataset.pointKind;pointPage=0;renderPointTabs(current());applyRangeRequest();renderPoints(current());return;}
  if(target.dataset.select!==undefined&&!busy){selectChannel(target.dataset.select);return;}
  if(target.dataset.close){if(!busy){if(target.dataset.close==='device-dialog')rememberDevicePanel();$(target.dataset.close).close();}return;}
  if(target.dataset.editPoint!==undefined&&!busy){pointDialog(target.dataset.editPoint);return;}
  if(target.dataset.copyMessage){const m=shownMessages.find(m=>String(m.sequence)===target.dataset.copyMessage);if(m)try{await navigator.clipboard.writeText(m.hex.match(/.{1,2}/g).join(' '));toast('已复制');}catch(error){report(Error('无法访问剪贴板'));}return;}
  if(target.dataset.deletePoint!==undefined){await run('delete-point',async()=>{const c=current(),p=c?.points.find(p=>keyOf(p)===target.dataset.deletePoint);if(p&&await confirmAction('删除点',`${kinds[p.kind]} · ${p.address}${p.name?' · '+p.name:''}`,true))await act('DeleteDevicePoint',c.id,selectedDevice,{kind:p.kind,address:p.address});});}
});
on('delete-channel','click',async()=>{const c=current();if(c&&await confirmAction('删除通道',`${c.name}及其点表将被删除。`,true))await act('DeleteChannel',c.id);});
on('toggle-channel','click',()=>{const id=selected;return act(active(state())?'StopChannel':'StartChannel',id);});on('stop-all','click',()=>act('StopAll'));
on('raw-form','submit',async e=>{e.preventDefault();const id=selected,text=$('raw-hex').value;if(!text.trim()){$('raw-hex').focus();throw Error('请输入十六进制报文');}saveDraft();await act('SendHex',id,text,$('append-crc').checked);});
for(const id of ['point-search','point-kind'])on(id,id==='point-search'?'input':'change',()=>{pointPage=0;renderPoints(current());},false);
on('point-prev','click',()=>{pointPage=Math.max(0,pointPage-1);renderPoints(current());$('point-scroll').scrollTop=0;},false);on('point-next','click',()=>{pointPage++;renderPoints(current());$('point-scroll').scrollTop=0;},false);
for(const id of ['direction-filter','all-messages','message-search'])on(id,id==='message-search'?'input':'change',()=>{if(id==='all-messages')loadPauseView();shownMessages=[];messageKey='';renderMessages();},false);
on('follow-messages','change',()=>{if($('follow-messages').checked)$('message-scroll').scrollTop=$('message-scroll').scrollHeight;},false);
$('message-scroll').addEventListener('scroll',()=>{const el=$('message-scroll');if(el.scrollHeight-el.scrollTop-el.clientHeight>70)$('follow-messages').checked=false;},{passive:true});
on('pause-messages','click',()=>{paused=!paused;if(paused){frozen=cloneBridgeData(snapshot.messages);pausedViews.set(messageScope(),frozen);}else pausedViews.delete(messageScope());$('pause-messages').textContent=paused?'继续':'暂停';$('pause-messages').classList.toggle('is-paused',paused);renderMessages();},false);
on('clear-messages','click',async()=>{const all=$('all-messages').checked,id=selected;if(all){await act('ClearMessages');pausedViews.clear();}else{if(!id)return;await act('ClearChannelMessages',id);pausedViews.delete(id);}if(paused)pausedViews.set(messageScope(),[]);frozen=[];shownMessages=[];messageKey='';renderMessages();});
on('export-messages','click',async()=>{const rows=filteredMessages();if(!rows.length)throw Error('没有匹配报文');if(await act('ExportMessageRows',rows))toast('报文已导出');});
on('import-points','click',async()=>{const c=current();if(c.points.length&&!await confirmAction('导入点表','选择文件后将替换当前点表。'))return;const n=await act('ImportDevicePoints',c.id,selectedDevice);if(n)toast(`已导入 ${n} 个点`);});on('export-points','click',async()=>{if(await act('ExportDevicePoints',selected,selectedDevice))toast('点表已导出');});
function resetProjectView(){paused=false;frozen=[];pausedViews.clear();shownMessages=[];messageKey='';pointPage=0;actionKey='';drafts.clear();$('pause-messages').textContent='暂停';$('pause-messages').classList.remove('is-paused');render();}
let nextProjectNumber=1;
function numberedProject(name){const match=/^工程([1-9]\d*)$/.exec(name||''),n=match?Number(match[1]):0;return Number.isSafeInteger(n)&&n<Number.MAX_SAFE_INTEGER?n:0;}
on('new-project','click',async()=>{
 const number=Math.max(nextProjectNumber,numberedProject(snapshot.project.name)+1);
 const result=await ask({title:'新建工程',input:'工程'+number});if(result.choice!=='ok'||!result.value)return;
 await api('NewProject',result.value,stationMode);
 nextProjectNumber=Math.max(number+1,numberedProject(result.value)+1);
});
on('open-project','click',async()=>{if(await prepareReplace()){const path=await act('OpenProject');if(path)resetProjectView();}});
on('save-project','click',async()=>{if(await act('SaveProject',false))toast('工程已保存');});on('save-as','click',async()=>{if(await act('SaveProject',true))toast('工程已另存为');});
for(const id of ['channel-dialog','point-dialog','device-dialog'])$(id).addEventListener('cancel',e=>{if(busy)e.preventDefault();});
document.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='s'){e.preventDefault();if(!document.querySelector('dialog[open]')&&!busy)$('save-project').click();}});
document.addEventListener('keydown',e=>{if(e.ctrlKey||e.metaKey||e.altKey||e.repeat||document.querySelector('dialog[open]')||e.target.closest('input,textarea,select,[contenteditable=true],#context-menu'))return;if(e.key===' '){e.preventDefault();$('pause-messages').click();}else if(e.key.toLowerCase()==='x'){e.preventDefault();$('clear-messages').click();}else if(e.key==='Escape')hideContext();});
document.addEventListener('dblclick',e=>{if(busy||document.querySelector('dialog[open]'))return;const row=e.target.closest('[data-point-key]');if(row&&!e.target.closest('button')){const p=current()?.points.find(p=>keyOf(p)===row.dataset.pointKey);if(current()?.protocol==='iec104'&&current().role==='client'&&['yk','yt','step'].includes(p?.kind))controlDialog(p.kind==='yt'?'setpoint':p.kind,p);else pointDialog(row.dataset.pointKey);}else if(e.target.closest('[data-device]')){selectDevice(e.target.closest('[data-device]').dataset.device);deviceDialog(true);};});
for(const [listID,selector,getID,select,getSelected]of [['device-list','[data-device]',b=>b.dataset.device,selectDevice,()=>selectedDevice],['channel-list','[data-select]',b=>b.dataset.select,selectChannel,()=>selected]])$(listID).addEventListener('keydown',e=>{if(busy||!['ArrowUp','ArrowDown','Home','End'].includes(e.key))return;e.preventDefault();const buttons=[...$(listID).querySelectorAll(selector)];if(!buttons.length)return;const index=buttons.findIndex(b=>getID(b)===getSelected());const next=e.key==='Home'?0:e.key==='End'?buttons.length-1:Math.max(0,Math.min(buttons.length-1,index+(e.key==='ArrowDown'?1:-1)));const id=getID(buttons[next]);select(id);[...$(listID).querySelectorAll(selector)].find(b=>getID(b)===id)?.focus({preventScroll:true});});
let contextActions=[];
function hideContext(){$('context-menu').hidden=true;contextActions=[];}
function showContext(e,items){e.preventDefault();hideContext();const menu=$('context-menu');contextActions=items;menu.innerHTML=items.map(([label,fn,disabled],i)=>`<button role="menuitem" data-context-action="${i}" ${disabled?'disabled':''}>${escape(label)}</button>`).join('');menu.hidden=false;menu.style.left=Math.max(4,Math.min(e.clientX,window.innerWidth-menu.offsetWidth-8))+'px';menu.style.top=Math.max(4,Math.min(e.clientY,window.innerHeight-menu.offsetHeight-8))+'px';menu.querySelector('button:not(:disabled)')?.focus();}
document.addEventListener('contextmenu',e=>{if(busy||document.querySelector('dialog[open]'))return;const pointRow=e.target.closest('#point-rows tr');if(pointRow){const target=pointRow.pointTarget,key={...target.key};showContext(e,[['详细配置',()=>pointDialog(keyOf(key))],['移除该点',async()=>{if(await confirmAction('删除点','此点将被移除。',true))await act('DeleteDevicePoint',target.channel,target.device,key);}]]);return;}const ch=e.target.closest('[data-select]');if(e.target.closest('[data-device]')){const button=e.target.closest('[data-device]');selectDevice(button.dataset.device);showContext(e,[['增加装置',()=>deviceDialog(false)],['修改装置参数',()=>deviceDialog(true)],['修改装置名称',renameDevice],['移除选中装置',removeDevice]]);}else if(ch){selectChannel(ch.dataset.select);const id=selected,c=current(),running=active(state());showContext(e,[['重命名',()=>run('rename',async()=>{const result=await ask({title:'重命名',input:c.name});if(result.choice==='ok'&&result.value)await act('RenameChannel',id,result.value);})],['复制',()=>run('copy',async()=>{const copy=cloneBridgeData(channel());copy.id='';copy.name=c.name+' 副本';const added=await act('UpsertChannel',copy);selectChannel(snapshot.project.channels.find(ch=>ch.role===stationMode)?.id||added);})],['删除',()=>$('delete-channel').click(),running]]);}else if(e.target.closest('#traffic-panel'))showContext(e,[ [paused?'开始显示报文　Space':'停止显示报文　Space',()=>$('pause-messages').click()],['清除报文　X',()=>$('clear-messages').click(),$('clear-messages').disabled],['导出报文',()=>$('export-messages').click(),$('export-messages').disabled],['显示筛选栏',()=>{$('message-options').hidden=!$('message-options').hidden;}] ]);});
$('context-menu').addEventListener('click',e=>{const button=e.target.closest('[data-context-action]');if(!button||button.disabled)return;const fn=contextActions[Number(button.dataset.contextAction)]?.[1];hideContext();fn?.();});
$('context-menu').addEventListener('keydown',e=>{if(!['ArrowDown','ArrowUp','Escape'].includes(e.key))return;e.preventDefault();if(e.key==='Escape'){hideContext();return;}const buttons=[...$('context-menu').querySelectorAll('button:not(:disabled)')],index=buttons.indexOf(document.activeElement);buttons[(index+(e.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length]?.focus();});
document.addEventListener('pointerdown',e=>{if(!e.target.closest('#context-menu'))hideContext();});window.addEventListener('resize',hideContext);
function resizeNavigationWidth(value){const max=Math.max(144,Math.min(420,(window.innerWidth||1120)-700)),width=Math.max(144,Math.min(max,Number(value)||180));$('sidebar-resizer').setAttribute('aria-valuemax',max);document.querySelector('.original-workspace').style.setProperty('--navigation-width',width+'px');$('sidebar-resizer').setAttribute('aria-valuenow',Math.round(width));try{localStorage.setItem('gylab-navigation-width',width);}catch{}}
function resizeNavigationHeight(value){const ratio=Math.max(.2,Math.min(.8,Number(value)||.5));document.querySelector('.navigation-dock').style.setProperty('--navigation-share',ratio);$('navigation-resizer').setAttribute('aria-valuenow',Math.round(ratio*100));try{localStorage.setItem('gylab-navigation-share',ratio);}catch{}}
let debugHeight=130;
function resizeDebugHeight(value){
 const dock=$('report-dock'),available=dock.clientHeight||dock.getBoundingClientRect().height||Math.max(235,window.innerHeight-200);
 const max=Math.max(65,Math.floor(available-170)),height=Math.max(65,Math.min(max,Number(value)||130));
 debugHeight=height;dock.style.setProperty('--debug-height',height+'px');
 $('debug-resizer').setAttribute('aria-valuemax',max);$('debug-resizer').setAttribute('aria-valuenow',Math.round(height));
 try{localStorage.setItem('gylab-debug-height',height);}catch{}
}
let navigationReady=false;
function initializeNavigationLayout(){navigationReady=true;try{resizeNavigationWidth(localStorage.getItem('gylab-navigation-width')||180);resizeNavigationHeight(localStorage.getItem('gylab-navigation-share')||.5);resizeDebugHeight(localStorage.getItem('gylab-debug-height')||130);}catch{resizeNavigationWidth(180);resizeNavigationHeight(.5);resizeDebugHeight(130);}}
for(const [id,vertical]of [['sidebar-resizer',true],['navigation-resizer',false]]){
 const handle=$(id);handle.addEventListener('pointerdown',e=>{if(e.button!==0)return;e.preventDefault();handle.setPointerCapture(e.pointerId);handle.classList.add('dragging');const start=Number(handle.getAttribute('aria-valuenow')),startX=e.clientX,startY=e.clientY,height=Math.max(1,document.querySelector('.navigation-dock').getBoundingClientRect().height-10);const move=event=>vertical?resizeNavigationWidth(start+event.clientX-startX):resizeNavigationHeight(start/100+(event.clientY-startY)/height);const end=()=>{handle.classList.remove('dragging');handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',end);handle.removeEventListener('pointercancel',end);};handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',end);handle.addEventListener('pointercancel',end);});
 handle.addEventListener('dblclick',()=>vertical?resizeNavigationWidth(180):resizeNavigationHeight(.5));
 handle.addEventListener('keydown',e=>{const previous=vertical?'ArrowLeft':'ArrowUp',next=vertical?'ArrowRight':'ArrowDown';if(![previous,next,'Home'].includes(e.key))return;e.preventDefault();const current=Number(handle.getAttribute('aria-valuenow'));vertical?resizeNavigationWidth(e.key==='Home'?180:current+(e.key===previous?-12:12)):resizeNavigationHeight(e.key==='Home'?.5:current/100+(e.key===previous?-.05:.05));});
}
window.addEventListener('resize',()=>{if(navigationReady){resizeNavigationWidth(Number($('sidebar-resizer').getAttribute('aria-valuenow')));resizeDebugHeight(debugHeight);}});
function resizePanels(value){const ratio=Math.max(.25,Math.min(.75,value));document.querySelector('main').style.setProperty('--point-share',ratio);$('panel-resizer').setAttribute('aria-valuenow',Math.round(ratio*100));try{localStorage.setItem('gylab-point-share',ratio);}catch{} }
try{const saved=Number(localStorage.getItem('gylab-point-share'));resizePanels(saved>=.25&&saved<=.75?saved:.42);}catch{resizePanels(.42);}
$('panel-resizer').addEventListener('pointerdown',e=>{if(e.button!==0)return;const handle=e.currentTarget;handle.setPointerCapture(e.pointerId);handle.classList.add('dragging');const left=document.querySelector('main').getBoundingClientRect().left;const total=document.querySelector('main').clientWidth;const move=event=>resizePanels((event.clientX-left)/total);const end=()=>{handle.classList.remove('dragging');handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',end);handle.removeEventListener('pointercancel',end);};handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',end);handle.addEventListener('pointercancel',end);});
$('panel-resizer').addEventListener('keydown',e=>{if(['ArrowLeft','ArrowRight','Home'].includes(e.key)){e.preventDefault();const value=Number($('panel-resizer').getAttribute('aria-valuenow'))/100;resizePanels(e.key==='Home'?.42:value+(e.key==='ArrowLeft'?-.05:.05));}});
$('debug-resizer').addEventListener('pointerdown',e=>{
 if(e.button!==0)return;e.preventDefault();const handle=e.currentTarget,startY=e.clientY,start=debugHeight;
 handle.setPointerCapture(e.pointerId);handle.classList.add('dragging');
 const move=event=>resizeDebugHeight(start+startY-event.clientY);
 const end=()=>{handle.classList.remove('dragging');handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',end);handle.removeEventListener('pointercancel',end);};
 handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',end);handle.addEventListener('pointercancel',end);
});
$('debug-resizer').addEventListener('dblclick',()=>resizeDebugHeight(130));
$('debug-resizer').addEventListener('keydown',e=>{
 if(!['ArrowUp','ArrowDown','Home'].includes(e.key))return;e.preventDefault();
 resizeDebugHeight(e.key==='Home'?130:debugHeight+(e.key==='ArrowUp'?12:-12));
});
const debugAfterByChannel=new Map();
function renderDebug(){const rows=snapshot.messages.filter(m=>m.channelId===selected&&['SYS','ERR'].includes(m.direction)&&m.sequence>(debugAfterByChannel.get(selected)||0)).slice(-200);$('debug-content').textContent=rows.map(m=>m.time+'  '+m.direction+'  '+m.summary).join('\n');}
const managerTransportNames={'tcp-client':'TCP 客户端','tcp-server':'TCP 服务端',udp:'UDP',serial:'串口'};
function managerField(row,cell,c,key,label,options=null){
 let input=cell.querySelector('[data-manager-field="'+key+'"]');
 if(!input){
  const wrapper=document.createElement('label');wrapper.className='manager-field';
  const caption=document.createElement('span');caption.textContent=label;wrapper.append(caption);
  input=document.createElement(options?'select':'input');if(!options)input.type='text';
  if(options)for(const [value,text]of options){const option=document.createElement('option');option.value=value;option.textContent=text;input.append(option);}
  if(key==='name')input.maxLength=100;input.dataset.managerField=key;input.setAttribute('aria-label',label+' · '+c.name);
  input.title='回车或离开提交，Esc取消';input.managerEdit={row,key,base:'',dirty:false,pending:false};
  input.addEventListener('input',()=>{input.managerEdit.dirty=true;input.removeAttribute('aria-invalid');});
  input.addEventListener('change',()=>{input.managerEdit.dirty=true;if(options)commitManagerField(input);});
  input.addEventListener('blur',()=>commitManagerField(input));
  input.addEventListener('keydown',e=>{if(e.isComposing)return;if(e.key==='Enter'){e.preventDefault();e.stopPropagation();commitManagerField(input);}else if(e.key==='Escape'&&!input.managerEdit.pending){e.preventDefault();e.stopPropagation();input.value=input.managerEdit.base;input.managerEdit.dirty=false;input.removeAttribute('aria-invalid');}});
  wrapper.append(input);cell.append(wrapper);
 }
 const edit=input.managerEdit;
 if(!edit.dirty&&!edit.pending&&document.activeElement!==input){input.value=String(c[key]??'');edit.base=input.value;}
 input.disabled=edit.pending||!!busy;
 return input;
}
async function commitManagerField(input){
 const edit=input.managerEdit;if(!edit||!edit.dirty||edit.pending)return;
 const value=input.value.trim();if(value===edit.base){edit.dirty=false;input.removeAttribute('aria-invalid');return;}
 if(!value&&edit.key!=='localAddress'){pointValueError(input,'请填写'+input.getAttribute('aria-label'));return;}
 edit.pending=true;input.disabled=true;input.setAttribute('aria-busy','true');
 const row=edit.row,key=edit.key,id=row.dataset.managerId;
 // Read fresh configuration inside the queue so edits to adjacent cells and
 // live device/point updates cannot overwrite one another.
 row.managerTail=row.managerTail.then(async()=>{
  try{
   const live=await api('GetSnapshot'),c=live.project.channels.find(c=>c.id===id);
   if(!c)throw Error('通道已被移除');
   if(key==='name')await api('RenameChannel',id,value);
   else{
    const config={...c,[key]:['baud','dataBits','stopBits'].includes(key)?Number(value):value};
    if(['baud','dataBits','stopBits'].includes(key)&&!Number.isInteger(config[key]))throw Error('请输入有效整数');
    if(key==='transport'&&value==='tcp-server')config.localAddress='';
    // The full communication form also restores inactive-field drafts.
    // Keep edited serial settings aligned with those restored values.
    const draftKeys={baud:'cbBaudRate',dataBits:'cbDataBit',parity:'cbParity',stopBits:'cbStopBit'};
    if(draftKeys[key])config.transportDraft={...config.transportDraft,[draftKeys[key]]:key==='parity'?{N:'无校验',E:'偶校验',O:'奇校验'}[value]:value};
    await api('UpdateTransport',config);
   }
   edit.base=value;edit.dirty=false;input.removeAttribute('aria-invalid');
   toast(key==='name'?'已保存':'已保存，通信参数变更后请手动连接');
  }catch(error){pointValueError(input,String(error?.message||error));}
  finally{edit.pending=false;input.disabled=false;input.removeAttribute('aria-busy');await refresh(true);}
 });
 return row.managerTail;
}
function renderManager(){
 if(!$('manager-dialog').open)return;
 const tbody=$('manager-rows'),existing=new Map([...tbody.children].map(row=>[row.dataset.managerId,row]));
 snapshot.project.channels.filter(c=>c.role===stationMode).forEach((c,index)=>{
  let row=existing.get(c.id);
  if(!row){row=document.createElement('tr');row.dataset.managerId=c.id;row.managerTail=Promise.resolve();for(let n=0;n<5;n++)row.append(document.createElement('td'));const button=document.createElement('button');button.dataset.managerChannel=c.id;row.children[4].append(button);}
  existing.delete(c.id);const st=snapshot.states.find(s=>s.id===c.id)||{status:'stopped'};
  managerField(row,row.children[0],c,'name','通道名称');
  managerField(row,row.children[1],c,'transport','通信方式',Object.entries(managerTransportNames));
  managerField(row,row.children[2],c,'address',c.transport==='serial'?'串口路径':'IP / 主机名:端口');
  const local=managerField(row,row.children[2],c,'localAddress','本地绑定（可留空）');local.closest('label').hidden=!['tcp-client','udp'].includes(c.transport);
  for(const [key,label,options]of [
   ['baud','波特率',null],['dataBits','数据位',[['8','8'],['7','7'],['6','6'],['5','5']]],
   ['parity','校验',[['N','无'],['E','偶'],['O','奇']]],['stopBits','停止位',[['1','1'],['2','2']]]
  ])managerField(row,row.children[1],c,key,label,options).closest('label').hidden=c.transport!=='serial';
  row.children[3].textContent=statuses[st.status]||st.status;
  const button=row.children[4].firstElementChild;button.textContent=active(st)?'断开':'连接';button.disabled=!!busy||[...row.querySelectorAll('[data-manager-field]')].some(input=>input.managerEdit.pending||input.managerEdit.dirty);
  if(tbody.children[index]!==row)tbody.insertBefore(row,tbody.children[index]||null);
 });
 for(const row of existing.values())row.remove();
}
on('network-ports','click',()=>$('list-ports').click(),false);
on('communication-manager','click',()=>{$('manager-dialog').show();renderManager();},false);on('clear-debug','click',()=>{debugAfterByChannel.set(selected,Math.max(0,...snapshot.messages.filter(m=>m.channelId===selected).map(m=>m.sequence)));renderDebug();},false);
on('connect-all','click',async()=>{for(const c of snapshot.project.channels.filter(c=>c.role===stationMode)){const st=snapshot.states.find(s=>s.id===c.id);if(!st||!active(st))await api('StartChannel',c.id);}await refresh(true);});
$('manager-rows').addEventListener('click',e=>{const b=e.target.closest('[data-manager-channel]');if(!b||b.disabled||busy||[...b.closest('tr').querySelectorAll('[data-manager-field]')].some(input=>input.managerEdit.pending||input.managerEdit.dirty))return;run('manager-connect',()=>{const id=b.dataset.managerChannel,st=snapshot.states.find(s=>s.id===id)||{status:'stopped'};return act(active(st)?'StopChannel':'StartChannel',id);});});
setInterval(()=>{if(!document.hidden)refresh();},650);document.addEventListener('visibilitychange',()=>{if(!document.hidden)refresh();});initializeDesktop();



for(const [id,formID]of [['device-dialog','device-form'],['channel-dialog','channel-form']]){
 $(id).addEventListener('keydown',e=>{
  if(busy||!['Escape','Enter'].includes(e.key)||e.isComposing)return;
  if(e.key==='Enter'&&e.target.closest('button'))return;
  e.preventDefault();e.stopPropagation();
  if(e.key==='Escape'||id==='device-dialog'){if(id==='device-dialog')rememberDevicePanel();$(id).close();}
  else submitWithButton($(formID),$(formID).querySelector('button[type=submit]'));
 });
}

// Native windows use normal document flow inside their own WebView. No DOM
// overlay is opened in the main workspace.
function installNativeDialogs(){
 for(const dialog of document.querySelectorAll('dialog')){
  const show=()=>{
   if(nativeContext?.kind){dialog.setAttribute('open','');return;}
   const f=$('device-form');const context={kind:dialog.id,role:stationMode,channel:selected,device:selectedDevice,
    edit:dialog.id==='control-dialog'||dialog.id==='auto-data-dialog'?true:dialog.id==='device-dialog'?!!f.dataset.device:['point-dialog','channel-dialog'].includes(dialog.id),
    pointKind:$('point-kind').value,point:dialog.id==='control-dialog'?controlEditor.mode:dialog.id==='point-dialog'&&pointEditor?.key?keyOf(pointEditor.key):'',control:dialog.id==='control-dialog'?controlEditor.request:null};
   api('OpenNativeWindow',context).catch(report);
  };
  dialog.show=show;dialog.showModal=show;
  dialog.close=()=>{if(nativeContext?.kind)window.go.main.App.CloseNativeWindow();else dialog.removeAttribute('open');};
 }
}
async function initializeDesktop(){
 try{
  const c=await window.go?.main?.App?.GetNativeContext?.();
  if(c&&typeof c.kind==='string'){
   nativeDesktop=true;nativeContext=c;if(!c.kind)stationMode=c.role||'client';installNativeDialogs();
   if(c.kind){
    stationMode=c.role||'client';selected=c.channel;selectedDevice=c.device;if(c.draft){deviceWindowCache.set(stationMode,c.draft.window);for(const [key,value]of c.draft.panels||[])devicePanelCache.set(key,value);}
    document.body.classList.add('native-window');$(c.kind).classList.add('native-content');
    await refresh();selected=c.channel;selectedDevice=c.device||channel()?.devices?.[0]?.id||'';
    if(c.kind==='channel-dialog')channelDialog();
    else if(c.kind==='device-dialog')deviceDialog(c.edit);
    else if(c.kind==='point-dialog'){if(c.pointKind)$('point-kind').value=c.pointKind;pointDialog(c.point||null);}
    else if(c.kind==='control-dialog')controlDialog(c.point,c.control);
    else if(c.kind==='auto-data-dialog')await initializeAutoDataWindow(c);
    else if(c.kind==='auto-yk-dialog'||c.kind==='auto-yt-dialog')await initializeAutoWindow(c.kind==='auto-yk-dialog'?'yk':'yt');
    else if(c.kind==='events-dialog'){$('events-dialog').show();renderEvents();}
 else if(c.kind==='auto-bulk-dialog')$('auto-bulk-dialog').show();
    else if(c.kind==='confirm-dialog')ask(c.ask);
    else{$(c.kind).show();if(c.kind==='manager-dialog')renderManager();}
    if(!$(c.kind).open)throw Error('配置目标已不存在，请重新打开窗口');
    document.body.style.visibility='visible';
    // Signal readiness after WebKit has laid out the visible native editor.
    await configWindowFit;
    await new Promise(resolve=>requestAnimationFrame(resolve));
    const dialog=$(c.kind),controls=[...dialog.querySelectorAll('input,select,button,textarea')].filter(el=>el.getClientRects().length&&!el.hidden);
    await api('NativeWindowReady',{viewportWidth:innerWidth,viewportHeight:innerHeight,documentWidth:document.documentElement.scrollWidth,dialogWidth:Math.round(dialog.getBoundingClientRect().width),controls:controls.length});
   }else{initializeNavigationLayout();await api('SetWorkspaceMode',stationMode);await refresh();api('SetNativeSelection',{role:stationMode,channel:selected,device:selectedDevice}).catch(report);}
  }else{initializeNavigationLayout();await api('SetWorkspaceMode',stationMode);await refresh();}
 }catch(error){report(error);}
 document.body.style.visibility='visible';
 if(nativeContext?.kind==='device-dialog')for(const event of ['input','change'])$('device-form').addEventListener(event,rememberDevicePanel);
}

// Kept separate from presentation: a successful send is never reported as a
// successful operation. Results come from matching protocol confirmations.
function controlDialog(mode='yk',point=null){
 const c=channel(),d=device();if(!c||!d||c.protocol!=='iec104'||c.role!=='client')return;
 const kind=mode==='setpoint'?'yt':mode,p=point||d.points.find(p=>p.kind===kind);
 const request={type:mode==='yk'?45:mode==='step'?47:50,address:p?.address??0,value:mode==='step'?2:p?.value??0,...(point?.type?point:{})};
 controlEditor={channel:c.id,device:d.id,mode,request};controlSelection=null;
 $('control-device').value=`${c.name} / ${d.name} · 公共地址 ${d.commonAddress}`;
 $('control-address').value=request.address;
 $('control-type').innerHTML=(mode==='yk'?[[45,'单点'],[46,'双点']]:mode==='step'?[[47,'步调节']]:[[48,'归一化（原码）'],[49,'标度化'],[50,'浮点数']]).map(([value,label])=>`<option value="${value}">${label}</option>`).join('');
 $('control-type').value=String(request.type);$('control-value').value=request.value;
 $('control-state').innerHTML=mode==='step'?'<option value="1">降</option><option value="2">升</option>':'<option value="0">分闸</option><option value="1">合闸</option>';
 $('control-state').value=mode==='step'?(request.value===1?'1':'2'):String(request.type===46?(request.value===2?1:0):(request.value?1:0));
 $('control-direct').checked=mode==='setpoint';$('control-timestamp').checked=false;$('control-result').textContent='';$('control-form').querySelector('.form-error').hidden=true;
 $('control-title').textContent=mode==='yk'?'遥控操作':mode==='step'?'遥调（步调节）':'遥调（设值）';updateControlFields();$('control-dialog').show();
}
function updateControlFields(){
 if(!controlEditor)return;const type=Number($('control-type').value),setpoint=type>=48;
 $('control-value-label').hidden=!setpoint;$('control-value').disabled=!setpoint;
 $('control-state-label').hidden=setpoint;$('control-state').disabled=setpoint;
 const value=$('control-value');value.min=type===50?'-3.4028234663852886e38':'-32768';value.max=type===50?'3.4028234663852886e38':'32767';value.step=type===50?'any':'1';updateControlButtons();
}
function controlParameters(operation){
 const type=Number($('control-type').value),address=integer('control-address',0,16777215),timeoutMs=integer('control-timeout',100,60000);
 let value=Number($('control-state').value);if(type===46)value++;
 if(type>=48){const input=$('control-value');value=Number(input.value);if(!input.value.trim()||!Number.isFinite(value)||type===50&&Math.abs(value)>3.4028234663852886e38||type!==50&&(!Number.isInteger(value)||value< -32768||value>32767))throw Error(type===50?'请输入有效短浮点设值':'请输入 -32768–32767 整数');}
 return {type:$('control-timestamp').checked?type+13:type,address,value,operation,direct:$('control-direct').checked,timeoutMs};
}
function controlFingerprint(){return [$('control-type').value,$('control-address').value,$('control-value').value,$('control-state').value,$('control-timestamp').checked].join('/');}
function updateControlButtons(){
 if(!controlEditor)return;const st=snapshot.states.find(s=>s.id===controlEditor.channel),online=st?.status==='connected'&&st.peers>0;
 const selected=controlSelection?.signature===controlFingerprint()&&Date.now()<controlSelection.until,locked=!!busy;
 for(const input of $('control-form').querySelectorAll('input:not([readonly]),select'))input.disabled=locked;
 $('control-value').disabled=locked||Number($('control-type').value)<48;$('control-state').disabled=locked||Number($('control-type').value)>=48;
 $('control-select').disabled=locked||!online;$('control-execute').disabled=locked||!online||!($('control-direct').checked||selected);$('control-cancel').disabled=locked||!online||!selected;
}
for(const operation of ['select','execute','cancel'])on('control-'+operation,'click',async()=>{
 if(!controlEditor)return;const editor=controlEditor,request=controlParameters(operation),signature=controlFingerprint();
 $('control-result').textContent='等待子站应答…';
 try{
  const result=await act('Command104Device',editor.channel,editor.device,request);
  controlSelection=result.stage==='selected'?{signature,until:Date.now()+30000}:null;
  $('control-result').textContent=result.message;
 }catch(error){controlSelection=null;$('control-result').textContent=String(error?.message||error);throw error;}
});
for(const event of ['input','change'])$('control-form').addEventListener(event,()=>{controlSelection=null;updateControlFields();});

// Automatic jobs live in the parent engine, so hiding/reopening a native window
// preserves its task, rows and counters, as in the original WinForms windows.
let autoEditor=null,autoView=null,autoPollBusy=false,autoEditQueue=Promise.resolve(),autoPending=new Map(),autoErrorSeen='';
const autoIsActive=status=>['starting','running','paused','stopping'].includes(status);
async function openAutoWindow(mode){
 const c=channel(),d=device();if(!c||!d)return;
 const seed=mode==='yt'?{addresses:$('auto-yt-addresses').value,value:$('auto-yt-value').value,valueType:Number($('auto-yt-type').value)}:{};
 const view=await api('PrepareAutoControl',c.id,d.id,mode,seed);
 if(nativeDesktop){await api('OpenNativeWindow',{kind:'auto-'+mode+'-dialog',channel:c.id,device:d.id,role:c.role,point:mode,edit:true});}
 else await initializeAutoWindow(mode,view);
}
async function initializeAutoWindow(mode,initial=null){
 autoEditor={channel:nativeContext?.channel||selected,device:nativeContext?.device||selectedDevice,mode};
 const v=initial||await api('GetAutoControl',autoEditor.channel,autoEditor.device,mode);
 if(!v)throw Error('自动操作数据未就绪');renderAutoState(v);$('auto-'+mode+'-dialog').show();if(nativeContext?.kind)await window.go.main.App.FitAutoWindow(window.innerWidth,window.innerHeight);
 setInterval(()=>pollAutoControl(),200);
}
async function pollAutoControl(){
 if(!autoEditor||autoPollBusy||document.hidden)return;autoPollBusy=true;
 try{renderAutoState(await api('GetAutoControl',autoEditor.channel,autoEditor.device,autoEditor.mode));}catch(error){report(error);}finally{autoPollBusy=false;}
}
function autoWrite(key,fn){
 const marker=Symbol();autoPending.set(key,marker);const operation=autoEditQueue.catch(()=>{}).then(fn);
 autoEditQueue=operation.then(v=>{if(v)renderAutoState(v);}).catch(error=>{report(error,$('auto-'+autoEditor.mode+'-form'));}).finally(()=>{if(autoPending.get(key)===marker)autoPending.delete(key);});
 return autoEditQueue;
}
function autoValue(id,value,pendingKey=id){const input=$(id);if(input&&document.activeElement!==input&&!autoPending.has(pendingKey)){if(input.type==='checkbox')input.checked=!!value;else input.value=String(value??'');}}
function renderAutoState(view){
 if(!view||!autoEditor)return;if(autoView&&view.id===autoView.id&&view.revision<autoView.revision)return;
 const previous=autoView;autoView=view;const mode=autoEditor.mode,o=view.options;
 autoValue('auto-'+mode+'-interval',o.intervalMs);
 if(mode==='yk'){
  for(const [id,key]of [['timeout','timeoutMs'],['feedback','checkFeedback'],['double','double'],['cycle','cycle'],['direct','direct'],['autopause','autoPause']])autoValue('auto-yk-'+id,o[key]);
  $('auto-yk-total').textContent='遥控次数：'+view.total;$('auto-yk-success').textContent='成功次数：'+view.success;$('auto-yk-failure').textContent='失败次数：'+view.failure;
  const log=$('auto-yk-errors'),text='错误输出窗口：\n'+view.errors.join('\n');if(log.textContent!==text){log.textContent=text;log.scrollTop=log.scrollHeight;}
 }
 const tbody=$('auto-'+mode+'-rows');if(previous&&(previous.id!==view.id||previous.generation!==view.generation)){tbody.replaceChildren();autoPending.clear();}const existing=new Map([...tbody.children].map(row=>[Number(row.dataset.autoRow),row]));
 view.rows.forEach((r,index)=>{
  let row=existing.get(index);if(!row){row=document.createElement('tr');row.dataset.autoRow=index;
   row.innerHTML=mode==='yk'?'<td></td><td><input type="checkbox" data-auto-field="enabled" aria-label="启用"></td><td></td><td></td><td><select data-auto-field="action" aria-label="遥控类型"><option>合闸后分闸</option><option>分闸后合闸</option><option>合闸</option><option>分闸</option></select></td><td></td><td></td><td><input type="number" data-auto-field="count" min="1" max="1000000" aria-label="遥控次数"></td><td></td><td></td>':'<td></td><td></td><td></td><td><input type="text" data-auto-field="value" aria-label="遥调数值"></td><td></td>';
   tbody.append(row);
  }existing.delete(index);row.classList.toggle('is-current',index===view.current);if(index===view.current&&previous?.current!==index&&autoIsActive(view.status))row.scrollIntoView?.({block:'nearest'});const fingerprint=JSON.stringify(r);if(row.dataset.fingerprint===fingerprint)return;row.dataset.fingerprint=fingerprint;const cells=row.children;cells[0].textContent=index+1;
  const put=(field,value)=>{const input=row.querySelector('[data-auto-field="'+field+'"]');if(document.activeElement!==input&&!autoPending.has(index+'/'+field)){if(input.type==='checkbox')input.checked=value;else input.value=value;}};
  if(mode==='yk'){
   put('enabled',r.enabled);put('action',r.action);put('count',r.count);cells[2].textContent=r.address;cells[3].textContent=r.name;cells[5].textContent=r.selection;cells[6].textContent=r.execution;cells[8].textContent=r.feedbackName;cells[8].title=r.feedback;cells[9].textContent=r.feedbackResult;
   for(const cell of [cells[3],cells[5],cells[6],cells[9]])cell.title=cell.textContent;
  }else{cells[1].textContent=r.address;cells[2].textContent=r.name;put('value',r.value);cells[4].textContent=r.executed||'否';cells[4].title=r.execution||'';}
  if(index===view.current&&previous?.current!==index&&autoIsActive(view.status))row.scrollIntoView?.({block:'nearest'});
 });for(const row of existing.values())row.remove();updateAutoButtons();
 if(mode==='yt'&&view.status==='error'&&view.lastError){const key=view.id+'/'+view.run+'/'+view.lastError;if(key!==autoErrorSeen){autoErrorSeen=key;report(view.lastError,$('auto-yt-form'));if(nativeDesktop)api('AskNative',{title:'遥调异常',text:view.lastError,input:null,choices:[['ok','确认']]}).catch(report);}}
}
function updateAutoButtons(){
 if(!autoEditor||!autoView)return;const mode=autoEditor.mode,status=autoView.status,active=autoIsActive(status),st=snapshot.states.find(s=>s.id===autoEditor.channel),online=st?.status==='connected'&&st.peers>0;
 const start=$('auto-'+mode+'-start'),stop=$('auto-'+mode+'-stop');
 start.textContent=mode==='yk'?(status==='running'?'暂停遥控':status==='paused'?'继续遥控':'自动遥控'):'自动遥调';
 start.disabled=!!busy||status==='starting'||status==='stopping'||(mode==='yt'&&active)||(!active&&!online);
 stop.disabled=!!busy||!active||status==='stopping';
}
function autoOptionsFromForm(){
 const mode=autoEditor.mode,o={...autoView.options,intervalMs:integer('auto-'+mode+'-interval',0,3600000)};
 if(mode==='yk')Object.assign(o,{timeoutMs:integer('auto-yk-timeout',100,60000),checkFeedback:$('auto-yk-feedback').checked,double:$('auto-yk-double').checked,cycle:$('auto-yk-cycle').checked,direct:$('auto-yk-direct').checked,autoPause:$('auto-yk-autopause').checked});return o;
}
function autoEditsFromForm(){
 return [...$('auto-'+autoEditor.mode+'-rows').children].map(row=>{
  const index=Number(row.dataset.autoRow);if(autoEditor.mode==='yt')return {index,generation:autoView.generation,value:row.querySelector('[data-auto-field="value"]').value};
  const input=row.querySelector('[data-auto-field="count"]'),count=Number(input.value);if(!input.value.trim()||!Number.isInteger(count)||count<1||count>1000000){input.focus();throw Error('遥控次数应为 1–1000000 整数');}
  return {index,generation:autoView.generation,count,enabled:row.querySelector('[data-auto-field="enabled"]').checked,action:row.querySelector('[data-auto-field="action"]').value};
 });
}
for(const mode of ['yk','yt']){
 on('auto-'+mode+'-start','click',async()=>{
  if(!autoEditor)return;await autoEditQueue;const args=[autoEditor.channel,autoEditor.device,mode];
  if(mode==='yk'&&autoView.status==='running'){renderAutoState(await api('PauseAutoControl',...args));return;}
  const options=autoOptionsFromForm(),rows=autoEditsFromForm();renderAutoState(await api('UpdateAutoOptions',...args,options));renderAutoState(await api('UpdateAutoRows',...args,rows));
  renderAutoState(await api(autoView.status==='paused'?'ResumeAutoControl':'StartAutoControl',...args));
 });
 on('auto-'+mode+'-stop','click',async()=>{if(autoEditor)renderAutoState(await api('StopAutoControl',autoEditor.channel,autoEditor.device,mode));});
 $('auto-'+mode+'-form').addEventListener('submit',e=>e.preventDefault());
 $('auto-'+mode+'-rows').addEventListener('change',e=>{
  const input=e.target.closest('[data-auto-field]'),row=input?.closest('[data-auto-row]');if(!row||!autoEditor)return;
  try{const index=Number(row.dataset.autoRow),generation=autoView.generation,field=input.dataset.autoField,value=field==='enabled'?input.checked:field==='count'?Number(input.value):input.value;
   if(field==='count'&&(!input.value.trim()||!Number.isInteger(value)||value<1||value>1000000))throw Error('遥控次数应为 1–1000000 整数');
   autoWrite(index+'/'+field,()=>api('UpdateAutoRows',autoEditor.channel,autoEditor.device,mode,[{index,generation,[field]:value}]));
  }catch(error){report(error,$('auto-'+mode+'-form'));}
 });
 $('auto-'+mode+'-form').addEventListener('change',e=>{if(!autoEditor||!e.target.id.startsWith('auto-'+mode+'-'))return;try{const options=autoOptionsFromForm();autoWrite(e.target.id,()=>api('UpdateAutoOptions',autoEditor.channel,autoEditor.device,mode,options));}catch(error){report(error,$('auto-'+mode+'-form'));}});
}
$('auto-yk-scroll').addEventListener('contextmenu',e=>{if(!autoEditor)return;showContext(e,[['批量修改',()=>{
 const context={kind:'auto-bulk-dialog',channel:autoEditor.channel,device:autoEditor.device,role:'client',point:'yk',edit:true};
 if(nativeDesktop)api('OpenAutoBulk',autoEditor.channel,autoEditor.device).catch(report);else $('auto-bulk-dialog').show();
}]]);});
on('auto-bulk-form','submit',async()=>{
 const c=nativeContext?.kind==='auto-bulk-dialog'?nativeContext:autoEditor;
 if(!c)return;const bulk={changeEnabled:$('auto-bulk-enable-check').checked,enabled:$('auto-bulk-enable').value==='true',changeAction:$('auto-bulk-action-check').checked,action:$('auto-bulk-action').value,changeCount:$('auto-bulk-count-check').checked,count:$('auto-bulk-count-check').checked?integer('auto-bulk-count',1,1000000):1};
 const result=await api('ApplyAutoBulk',c.channel,c.device,bulk);if(autoEditor)renderAutoState(result);$('auto-bulk-dialog').close();
});

for(const mode of ['yk','yt'])$('auto-'+mode+'-rows').addEventListener('keydown',e=>{const input=e.target.closest('[data-auto-field]');if(input&&e.key==='Enter'){e.preventDefault();input.dispatchEvent(new Event('change',{bubbles:true}));input.blur();}});
$('auto-yk-rows').addEventListener('click',e=>{const cell=e.target.closest('td');if(cell?.cellIndex===1&&e.target.tagName!=='INPUT'){const input=cell.querySelector('input');input.checked=!input.checked;input.dispatchEvent(new Event('change',{bubbles:true}));}});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&$('auto-bulk-dialog').open){e.preventDefault();$('auto-bulk-dialog').close();}});


// Multi-rule data generation runs in the Go engine; this window only edits and
// observes its fixed channel/device target.
let dataEditor=null,dataView=null,dataPollBusy=false;
function dataKinds(){return dataEditor?.protocol==='iec104'?['yc','yx']:['holding','input','coil','discrete'];}
function dataIsBit(kind){return ['yx','coil','discrete'].includes(kind);}
function newDataRule(kind){
 const points=dataEditor.points.filter(p=>p.kind===kind).map(p=>p.address),bit=dataIsBit(kind);
 return {enabled:true,kind,start:points.length?Math.min(...points):0,end:points.length?Math.max(...points):0,intervalMs:1000,mode:bit?'toggle':'increment',min:0,max:bit?1:100,step:1};
}
async function openAutoDataWindow(){
 const c=channel(),d=device();if(!c||!d||c.role!=='server')return;
 if(nativeDesktop&&!nativeContext?.kind){$('auto-data-dialog').show();return;}
 await initializeAutoDataWindow({channel:c.id,device:d.id});
}
async function initializeAutoDataWindow(context){
 const c=snapshot.project.channels.find(c=>c.id===context.channel),d=c?.devices.find(d=>d.id===context.device);
 if(!c||!d)throw Error('自动数据目标已不存在');
 dataEditor={channel:c.id,device:d.id,protocol:c.protocol,points:d.points};
 const view=await api('GetAutoData',c.id,d.id);if(!view)throw Error('自动数据服务未就绪');
 $('auto-data-target').textContent=c.name+' / '+d.name;
 $('auto-data-disconnect-label').hidden=c.protocol!=='iec104';
 $('auto-data-note').textContent=c.protocol==='iec104'?'周期内更新并上送范围内已配置点位':'自动更新点值，由主站轮询读取';
 $('auto-data-disconnect').checked=view.config.stopOnDisconnect;
 let rules=view.config.rules;if(!rules.length)rules=dataKinds().filter(kind=>d.points.some(p=>p.kind===kind)).map(newDataRule);
 if(!rules.length)rules=[newDataRule(dataKinds()[0])];
 renderDataRules(rules);renderDataState(view);$('auto-data-dialog').show();
}
function renderDataRules(rules){
 $('auto-data-rows').innerHTML=rules.map((r,index)=>`<tr data-data-row="${index}">
 <td><input type="checkbox" data-data-field="enabled" aria-label="启用第${index+1}组" ${r.enabled?'checked':''}></td>
 <td><select data-data-field="kind" aria-label="数据类型">${dataKinds().map(k=>`<option value="${k}" ${k===r.kind?'selected':''}>${escape(kinds[k])}</option>`).join('')}</select></td>
 ${['start','end','intervalMs'].map(k=>`<td><input data-data-field="${k}" aria-label="${k==='start'?'起始地址':k==='end'?'结束地址':'周期'}" type="number" step="1" min="${k==='intervalMs'?200:0}" max="${k==='intervalMs'?3600000:dataEditor.protocol==='iec104'?16777215:65535}" value="${escape(r[k])}"></td>`).join('')}
 <td><select data-data-field="mode" aria-label="变化方式">${[['fixed','固定值'],['increment','递增循环'],['random','区间随机'],['toggle','翻转']].map(([v,label])=>`<option value="${v}" ${r.mode===v?'selected':''} ${v==='toggle'&&!dataIsBit(r.kind)?'disabled':''}>${label}</option>`).join('')}</select></td>
 ${['min','max','step'].map(k=>`<td><input data-data-field="${k}" type="number" step="any" aria-label="${k==='min'?'下限或固定值':k==='max'?'上限':'步长'}" value="${escape(r[k])}"></td>`).join('')}
 <td data-data-cycles>0</td><td><button type="button" data-data-remove="${index}" aria-label="删除第${index+1}组">×</button></td></tr>`).join('');
 updateDataControls();
}
function readDataRules(validate=true){
 return [...$('auto-data-rows').children].map((row,index)=>{
  const get=k=>row.querySelector('[data-data-field="'+k+'"]'),r={enabled:get('enabled').checked,kind:get('kind').value,mode:get('mode').value};
  for(const k of ['start','end','intervalMs','min','max','step']){
   const input=get(k),raw=input.value;r[k]=Number(raw);
   const needed=k!=='max'&&k!=='step'||k==='max'&&r.mode!=='fixed'||k==='step'&&r.mode==='increment';
   if(validate&&r.enabled&&needed&&(!raw.trim()||!Number.isFinite(r[k]))){input.focus();throw Error('第'+(index+1)+'组：请填写有效数值');}
  }
  if(r.mode==='fixed')r.max=r.min;
  if(r.mode!=='increment')r.step=1;
  if(validate&&r.enabled){
   if(!Number.isInteger(r.start)||!Number.isInteger(r.end)||r.start<0||r.end<r.start)throw Error('第'+(index+1)+'组：点位范围无效');
   if(!Number.isInteger(r.intervalMs)||r.intervalMs<200||r.intervalMs>3600000)throw Error('第'+(index+1)+'组：周期应为200–3600000毫秒');
   if(r.min>r.max)throw Error('第'+(index+1)+'组：下限不能大于上限');
  }
  return r;
 });
}
function renderDataState(view){
 if(!view)return;dataView=view;
 if(dataEditor){const target=snapshot.project.channels.find(c=>c.id===dataEditor.channel)?.devices.find(d=>d.id===dataEditor.device);if(target)dataEditor.points=target.points;}
 $('auto-data-status').textContent=({running:'运行中',paused:'已暂停',stopped:'已停止'}[view.status]||view.status)+(view.lastError?' · '+view.lastError:'');
 $('auto-data-updates').textContent='更新点次 '+view.updates;
 [...$('auto-data-rows').children].forEach((row,index)=>row.querySelector('[data-data-cycles]').textContent=view.cycles[index]||0);
 updateDataControls();
}
function updateDataControls(){
 const status=dataView?.status,active=status==='running'||status==='paused',locked=!!busy||active;
 const transport=snapshot.states.find(s=>s.id===dataEditor?.channel),ready=dataEditor?.protocol==='iec104'?transport?.status==='connected'&&transport.peers>0:!!transport&&['connected','listening'].includes(transport.status);
 for(const row of $('auto-data-rows').children){
  const get=k=>row.querySelector('[data-data-field="'+k+'"]'),enabled=get('enabled').checked,mode=get('mode').value;
  for(const el of row.querySelectorAll('input,select,button'))el.disabled=locked||!enabled&&el!==get('enabled')&&!el.matches('[data-data-remove]');
  get('max').disabled=locked||!enabled||mode==='fixed';get('step').disabled=locked||!enabled||mode!=='increment';
 }
 $('auto-data-add').disabled=locked;$('auto-data-save').disabled=locked;$('auto-data-start').disabled=locked||!ready;
 $('auto-data-disconnect').disabled=locked;$('auto-data-pause').disabled=!!busy||!active;$('auto-data-stop').disabled=!!busy||!active;
 $('auto-data-pause').textContent=status==='paused'?'继续':'暂停';
}
async function saveDataConfiguration(){
 if(!dataEditor)throw Error('请选择装置');
 const config={rules:readDataRules(),stopOnDisconnect:$('auto-data-disconnect').checked};
 const view=await api('SaveAutoData',dataEditor.channel,dataEditor.device,config);renderDataState(view);return view;
}
on('auto-data-add','click',()=>{const rules=readDataRules(false);if(rules.length>=64)throw Error('最多64组规则');rules.push(newDataRule(dataKinds()[0]));renderDataRules(rules);},false);
$('auto-data-rows').addEventListener('click',e=>{const button=e.target.closest('[data-data-remove]');if(!button||button.disabled)return;const rules=readDataRules(false);rules.splice(Number(button.dataset.dataRemove),1);renderDataRules(rules);});
$('auto-data-rows').addEventListener('change',e=>{if(e.target.dataset.dataField==='kind'){const rules=readDataRules(false),index=Number(e.target.closest('tr').dataset.dataRow);rules[index]=newDataRule(e.target.value);renderDataRules(rules);}else updateDataControls();});
on('auto-data-save','click',async()=>{await saveDataConfiguration();toast('自动数据配置已保存');});
on('auto-data-form','submit',async e=>{e.preventDefault();await saveDataConfiguration();renderDataState(await api('StartAutoData',dataEditor.channel,dataEditor.device));});
on('auto-data-pause','click',async()=>renderDataState(await api('SetAutoDataState',dataEditor.channel,dataEditor.device,dataView.status==='paused'?'resume':'pause')));
on('auto-data-stop','click',async()=>renderDataState(await api('SetAutoDataState',dataEditor.channel,dataEditor.device,'stop')));
$('auto-data-dialog').addEventListener('keydown',e=>{if(e.key==='Escape'&&!busy){e.preventDefault();$('auto-data-dialog').close();}});
setInterval(async()=>{
 if(!dataEditor||dataPollBusy||document.hidden||!$('auto-data-dialog').open)return;dataPollBusy=true;
 try{renderDataState(await api('GetAutoData',dataEditor.channel,dataEditor.device));}catch(error){report(error,$('auto-data-form'));}finally{dataPollBusy=false;}
},300);

// Keep numeric validation while disabling native increment/decrement shortcuts.
document.addEventListener('keydown',e=>{
 if(e.target.matches('input[type=number]')&&['ArrowUp','ArrowDown'].includes(e.key))e.preventDefault();
},true);
document.addEventListener('wheel',e=>{
 if(e.target===document.activeElement&&e.target.matches('input[type=number]'))e.preventDefault();
},{passive:false,capture:true});

function renderEvents(){if(!$('events-dialog').open)return;const category=$('event-category').value,events=(snapshot.events||[]).filter(e=>e.channelId===selected&&(!category||e.category===category));$('event-count').textContent=events.length+' 条';$('event-rows').innerHTML=events.slice(-500).map(e=>`<tr><td>${escape(e.category)}</td><td>${escape(e.timestamp||e.received)}</td><td>${e.timeInvalid?'无效':'有效'}</td><td>${e.address}</td><td>${escape(kinds[e.kind]||e.kind)}</td><td>${escape(e.value)}</td><td>${e.quality}</td></tr>`).join('');}
on('event-category','change',renderEvents,false);
on('clear-events','click',async()=>{await act('ClearEvents',selected,$('event-category').value);renderEvents();});

function updateByteOrders(){const host=$('original-device-panel');for(const prefix of ['yc','mc']){const type=originalControl(host,'cb_'+prefix+'Type')||originalControl(host,'cb_'+prefix+'Format'),order=originalControl(host,'cb_'+prefix+'FunWay');if(!type||!order)continue;const wide=prefix==='mc'||Number(type.value.split(':')[0])>=2,items=wide?['ABCD','BADC','CDAB','DCBA']:['AB','BA'],index=Math.max(0,Number(order.value.split(':')[0])||0);order.innerHTML=items.map((o,i)=>`<option value="${i}:${o}" ${i===Math.min(index,items.length-1)?'selected':''}>${i}:${o}</option>`).join('');}}
$('original-device-panel').addEventListener('change',e=>{if(/cb_(yc|mc)(Type|Format)/.test(e.target.dataset.originalId||''))updateByteOrders();});
