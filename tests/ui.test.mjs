import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const {Window}=await import(process.env.GYLAB_DOM_MODULE||'happy-dom');
const html=(await readFile(new URL('../frontend/index.html',import.meta.url),'utf8')).replace(/<script\b[^>]*>[\s\S]*?<\/script>/g,'').replace(/<link\b[^>]*>/g,'');
const code=await readFile(new URL('../frontend/app.js',import.meta.url),'utf8');
const formCode=await readFile(new URL('../frontend/original-forms.js',import.meta.url),'utf8');
const rendererCode=await readFile(new URL('../frontend/replica-ui.js',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
// happy-dom implements button activation through requestSubmit internally;
// retain that internal behavior while testing the API's absence in app code.
function legacySubmission(form){const native=form.requestSubmit;form.requestSubmit=undefined;for(const button of form.querySelectorAll('button[type=submit]')){const click=button.click.bind(button);button.click=()=>{form.requestSubmit=native;try{return click();}finally{form.requestSubmit=undefined;}};}}
test('older WebKit keeps project titles and message pause without Array.at or structuredClone',async()=>{
 const h=await setup({messages:[{sequence:1,channelId:'a',direction:'RX',hex:'CAFE',summary:'before',time:'12:00:00'}],path:'/tmp/mac-project.json'});
 try{
  h.w.structuredClone=undefined;h.w.eval('Array.prototype.at=undefined');
  h.$('pause-messages').click();await tick();
  assert.equal(h.$('pause-messages').textContent,'继续');
  h.state.messages[0].summary='changed';h.state.messages.push({sequence:2,channelId:'a',direction:'RX',summary:'after',hex:'BEEF',time:'12:00:01'});await h.refresh();
  assert.equal(h.$('project-path'),null);assert.equal(h.$('project-name').textContent,'test');assert.match(h.$('project-name').title,/\/tmp\/mac-project\.json/);
  assert.match(h.$('message-rows').textContent,/before/);assert.doesNotMatch(h.$('message-rows').textContent,/changed/);
  h.$('pause-messages').click();await tick();assert.match(h.$('message-rows').textContent,/after/);
 }finally{await h.close();}
});
test('older WebKit Enter applies communication settings exactly once without requestSubmit',async()=>{
 const h=await setup();try{
  h.$('edit-channel').click();await tick();legacySubmission(h.$('channel-form'));
  const port=originalField(h,'txtClientPort','original-network-panel');port.value='25045';
  port.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));
  await tick();await tick();
  const edits=h.calls.filter(c=>c[0]==='UpdateTransport');assert.equal(edits.length,1);assert.equal(edits[0][0],'UpdateTransport');assert.match(edits[0][1][0].address,/:25045$/);
 }finally{await h.close();}
});
test('older WebKit submission fallback retains form validation and selected confirmation choice',async()=>{
 const h=await setup();try{
  const form=h.w.document.createElement('form');form.innerHTML='<input required><button type="submit" value="save">Save</button>';h.w.document.body.append(form);legacySubmission(form);
  let submitted=0,choice='';form.addEventListener('submit',e=>{e.preventDefault();submitted++;choice=e.submitter?.value;});
  h.w.eval('submitWithButton')(form,form.querySelector('button'));assert.equal(submitted,0);
  form.querySelector('input').value='valid';h.w.eval('submitWithButton')(form,form.querySelector('button'));assert.equal(submitted,1);assert.equal(choice,'save');
 }finally{await h.close();}
});
test('desktop automatic data opens with a fixed channel and device identity',async()=>{
 const c=dataServer();const h=await setup({project:{name:'data',channels:[c]},states:[]},{kind:''});try{
  h.w.document.querySelector('.mode-switch [data-mode="server"]').click();h.$('open-auto-data').click();await tick();await tick();
  const context=h.calls.find(c=>c[0]==='OpenNativeWindow')[1][0];assert.equal(context.kind,'auto-data-dialog');assert.equal(context.channel,'data');assert.equal(context.device,'target');assert.equal(context.edit,true);
 }finally{await h.close();}
});
function dataServer(protocol='iec104'){
 const points=protocol==='iec104'?[{kind:'yc',address:10,value:0,quality:0},{kind:'yc',address:11,value:0,quality:0},{kind:'yx',address:1,value:0,quality:0}]:[{kind:'holding',address:0,value:0,quality:0},{kind:'coil',address:1,value:0,quality:0}];
 return {...channel('data'),protocol,role:'server',transport:'tcp-server',devices:[{id:'target',name:'data device',unit:1,commonAddress:1,points}]};
}
function dataState(){return {channel:'data',device:'target',status:'stopped',config:{rules:[],stopOnDisconnect:true},cycles:[],updates:0,lastError:''};}
async function setupData(protocol='iec104',native=false){
 const c=dataServer(protocol),view=dataState();
 const h=await setup({project:{name:'data',channels:[c]},states:[{id:c.id,status:protocol==='iec104'?'connected':'listening',peers:protocol==='iec104'?1:0}]},native?{kind:'auto-data-dialog',role:'server',channel:c.id,device:'target'}:null,{GetAutoData:()=>structuredClone(view),SaveAutoData:(channel,device,config)=>{view.config=structuredClone(config);view.cycles=config.rules.map(()=>0);return structuredClone(view);},StartAutoData:()=>{view.status='running';return structuredClone(view);},SetAutoDataState:(channel,device,action)=>{view.status={pause:'paused',resume:'running',stop:'stopped'}[action];return structuredClone(view);}});
 if(!native){h.w.document.querySelector('.mode-switch [data-mode="server"]').click();h.$('open-auto-data').click();await tick();await tick();}
 return {...h,view};
}
test('automatic data supports several data types and independent periods in one configuration',async()=>{
 const h=await setupData();try{
  assert.equal(h.$('auto-data-rows').children.length,2);
  const rows=h.$('auto-data-rows').children;rows[0].querySelector('[data-data-field="intervalMs"]').value='500';rows[1].querySelector('[data-data-field="intervalMs"]').value='1200';
  h.$('auto-data-save').click();await tick();await tick();
  const config=h.calls.find(c=>c[0]==='SaveAutoData')[1][2];assert.deepEqual(Array.from(config.rules,r=>r.kind),['yc','yx']);assert.deepEqual(Array.from(config.rules,r=>r.intervalMs),[500,1200]);assert.equal(h.calls.some(c=>c[0]==='StartAutoData'),false);assert.equal(h.$('auto-data-save').disabled,false);
 }finally{await h.close();}
});
test('automatic data start pause resume stop locks rules and preserves its fixed device target',async()=>{
 const h=await setupData();try{
  h.$('auto-data-form').requestSubmit(h.$('auto-data-start'));await tick();await tick();await tick();assert.equal(h.$('auto-data-start').disabled,true);assert.equal(h.$('auto-data-pause').disabled,false);
  assert.equal(h.$('auto-data-rows').querySelector('[data-data-field="min"]').disabled,true);
  h.$('auto-data-pause').click();await tick();await tick();assert.equal(h.$('auto-data-pause').textContent,'继续');
  h.$('auto-data-pause').click();await tick();await tick();h.$('auto-data-stop').click();await tick();await tick();
  assert.equal(h.$('auto-data-start').disabled,false);assert.equal(h.$('auto-data-stop').disabled,true);
  const calls=h.calls.filter(c=>c[0]==='SetAutoDataState');assert.deepEqual(calls.map(c=>[...c[1]]),[['data','target','pause'],['data','target','resume'],['data','target','stop']]);
 }finally{await h.close();}
});
test('automatic data can add remove and disable groups while rejecting blank numeric input',async()=>{
 const h=await setupData();try{
  h.$('auto-data-add').click();assert.equal(h.$('auto-data-rows').children.length,3);
  h.$('auto-data-rows').lastElementChild.querySelector('[data-data-remove]').click();assert.equal(h.$('auto-data-rows').children.length,2);
  const input=h.$('auto-data-rows').querySelector('[data-data-field="min"]');input.value='';h.$('auto-data-save').click();await tick();await tick();assert.equal(h.calls.some(c=>c[0]==='SaveAutoData'),false);assert.equal(h.$('auto-data-form').querySelector('.form-error').hidden,false);
  input.value='12.25';const mode=h.$('auto-data-rows').querySelector('[data-data-field="mode"]');mode.value='fixed';mode.dispatchEvent(new h.w.Event('change',{bubbles:true}));
  assert.equal(h.$('auto-data-rows').querySelector('[data-data-field="max"]').disabled,true);h.$('auto-data-save').click();await tick();await tick();const saved=h.calls.find(c=>c[0]==='SaveAutoData')[1][2];assert.equal(saved.rules[0].min,12.25);assert.equal(saved.rules[0].max,12.25);
 }finally{await h.close();}
});
test('Modbus automatic data explains polling and offers registers and switches',async()=>{
 const h=await setupData('modbus-tcp');try{
  assert.equal(h.$('auto-data-disconnect-label').hidden,true);assert.match(h.$('auto-data-note').textContent,/轮询/);
  assert.deepEqual([...h.$('auto-data-rows').querySelector('[data-data-field="kind"]').options].map(o=>o.value),['holding','input','coil','discrete']);
 }finally{await h.close();}
});
test('automatic data opens a separate native window and closing it never stops its job',async()=>{
 const h=await setupData('iec104',true);try{
  assert.equal(h.$('auto-data-dialog').classList.contains('native-content'),true);assert.equal(h.$('auto-data-dialog').open,true);
  h.$('auto-data-dialog').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));await tick();
  assert.ok(h.calls.some(c=>c[0]==='CloseNativeWindow'));assert.equal(h.calls.some(c=>c[0]==='SetAutoDataState'),false);
 }finally{await h.close();}
});
test('communication IP and port remain editable while connected and apply the edited endpoint',async()=>{
 const h=await setup();try{
  assert.equal(h.$('edit-channel').disabled,false);h.$('edit-channel').click();
  const ip=originalField(h,'txtClientIP','original-network-panel'),port=originalField(h,'txtClientPort','original-network-panel');
  assert.equal(ip.disabled,false);assert.equal(port.disabled,false);ip.value='192.0.2.55';port.value='15020';
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  assert.equal(h.calls.find(c=>c[0]==='UpdateTransport')[1][0].address,'192.0.2.55:15020');assert.equal(h.$('channel-dialog').open,false);
 }finally{await h.close();}
});
test('communication groups follow transport and failed validation keeps edited fields for correction',async()=>{
 const h=await setup();try{
  h.$('edit-channel').click();const panel=h.$('original-network-panel'),get=id=>originalField(h,id,'original-network-panel');
  assert.equal(panel.querySelector('[data-group-id="groupBoxEx2"]').hidden,true);assert.equal(panel.querySelector('[data-group-id="groupBoxEx3"]').hidden,false);
  get('txtClientIP').value='192.0.2.66';get('txtClientPort').value='16001';h.overrides.UpdateTransport=()=>{throw Error('端口无效');};
  h.$('channel-form').requestSubmit(panel.querySelector('button[type=submit]'));await tick();await tick();await tick();
  assert.equal(h.$('channel-dialog').open,true);assert.equal(get('txtClientIP').value,'192.0.2.66');assert.equal(get('txtClientPort').value,'16001');assert.equal(get('txtClientIP').disabled,false);assert.match(h.$('channel-form').querySelector('.form-error').textContent,/端口无效/);
 }finally{await h.close();}
});
test('UDP allows editing its local IP as well as its local and remote ports',async()=>{
 const c={...channel('a'),protocol:'raw',devices:[]};const h=await setup({project:{name:'udp local',channels:[c]},states:[]});try{
  h.$('edit-channel').click();const get=id=>originalField(h,id,'original-network-panel');get('rbUDP').checked=true;get('rbUDP').dispatchEvent(new h.w.Event('change',{bubbles:true}));
  assert.equal(get('cbxServerIP').disabled,false);get('cbxServerIP').value='127.0.0.2';get('txtServerPort').value='19009';
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  assert.equal(h.calls.find(c=>c[0]==='UpdateTransport')[1][0].localAddress,'127.0.0.2:19009');
 }finally{await h.close();}
});
test('UDP exposes remote and local ports and disables TCP local-assignment options',async()=>{
 const c={...channel('a'),protocol:'raw',devices:[]};const h=await setup({project:{name:'udp',channels:[c]},states:[]});try{
  h.$('edit-channel').click();const get=id=>originalField(h,id,'original-network-panel');
  get('rbUDP').checked=true;get('rbUDP').dispatchEvent(new h.w.Event('change',{bubbles:true}));
  assert.equal(get('txtServerPort').disabled,false);assert.equal(get('txtClientPort').disabled,false);assert.equal(get('ck_AssignIpPort').disabled,true);
  get('txtClientIP').value='127.0.0.1';get('txtClientPort').value='19101';get('txtServerPort').value='19102';
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  const config=h.calls.find(c=>c[0]==='UpdateTransport')[1][0];assert.equal(config.transport,'udp');assert.equal(config.address,'127.0.0.1:19101');assert.equal(config.localAddress,'0.0.0.0:19102');
 }finally{await h.close();}
});
test('communication keeps inactive endpoints and accepts Any when changing network roles',async()=>{
 const c=channel('a');c.transportDraft={txtClientIP:'192.0.2.10',txtClientPort:'1502',cbxServerIP:'Any',txtServerPort:'1503',cbComList:'COM18'};
 const h=await setup({project:{name:'drafts',channels:[c]},states:[]});try{
  h.$('edit-channel').click();const get=id=>originalField(h,id,'original-network-panel');get('rbServer').checked=true;get('rbServer').dispatchEvent(new h.w.Event('change',{bubbles:true}));
  assert.equal(get('txtServerPort').value,'1503');assert.equal(get('cbComList').value,'COM18');
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  const config=h.calls.find(c=>c[0]==='UpdateTransport')[1][0];assert.equal(config.address,'0.0.0.0:1503');assert.equal(config.transportDraft.txtClientIP,c.address.split(':')[0]);assert.equal(config.transportDraft.cbComList,'COM18');
 }finally{await h.close();}
});
test('IP and serial port candidates remain editable and refresh leaves typed values alone',async()=>{
 const h=await setup({states:[]},null,{LocalIPs:()=>['Any','127.0.0.1','192.0.2.1'],SerialPorts:()=>['COM4','COM5']});try{
  h.$('edit-channel').click();await tick();await tick();const input=originalField(h,'cbxServerIP','original-network-panel');
  assert.equal(input.tagName,'INPUT');assert.equal(h.$('choices-cbxServerIP').children.length,3);
  const port=originalField(h,'cbComList','original-network-panel');port.value='COM99';h.$('network-ports').click();await tick();await tick();assert.equal(port.value,'COM99');
 }finally{await h.close();}
});
test('device protocol and version switches preserve edited ranges',async()=>{
 const h=await setup();try{
  h.$('add-device').click();chooseDeviceProtocol(h,'modbus','TCP版');const input=originalField(h,'txtYX_Add');input.value='123';input.dispatchEvent(new h.w.Event('input',{bubbles:true}));
  chooseDeviceProtocol(h,'iec104');chooseDeviceProtocol(h,'modbus','标准版(RTU)');assert.equal(originalField(h,'txtYX_Add').value,'123');
  h.$('device-version').value='TCP版';h.$('device-version').dispatchEvent(new h.w.Event('change',{bubbles:true}));assert.equal(originalField(h,'txtYX_Add').value,'123');
 }finally{await h.close();}
});
test('device Enter hides without adding and communication Enter applies once',async()=>{
 const h=await setup({states:[]});try{
  h.$('add-device').click();chooseDeviceProtocol(h,'modbus','TCP版');originalField(h,'txtAdd').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));await tick();
  assert.equal(h.$('device-dialog').open,false);assert.equal(h.calls.some(c=>c[0]==='UpsertDevice'),false);
  h.$('edit-channel').click();originalField(h,'txtClientPort','original-network-panel').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));await tick();await tick();
  assert.equal(h.calls.filter(c=>c[0]==='UpdateTransport').length,1);
 }finally{await h.close();}
});
test('communication Escape discards edits and applies to current selection as the original does',async()=>{
 const h=await setup({states:[]});try{
  h.$('edit-channel').click();originalField(h,'txtClientPort','original-network-panel').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));await tick();assert.equal(h.calls.some(c=>c[0]==='UpdateTransport'),false);
  h.$('edit-channel').click();h.w.document.querySelector('[data-select="b"]').click();
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  assert.equal(h.calls.find(c=>c[0]==='UpdateTransport')[1][0].id,'b');
 }finally{await h.close();}
});
test('all device panels keep point addresses left of their matching counts',async()=>{
 for(const role of ['client','server'])for(const group of ['iec104','modbus']){
  const c={...channel('a'),role,transport:role==='server'?'tcp-server':'tcp-client'};
  const h=await setup({project:{name:'pairs',channels:[c]},states:[]});try{
   h.w.document.querySelector('.mode-switch [data-mode="'+role+'"]').click();h.$('add-device').click();chooseDeviceProtocol(h,group,group==='modbus'?'TCP版':'标准版');
   const fields=[...h.$('original-device-panel').querySelectorAll('[data-original-id]')].map(el=>el.dataset.originalId);
   for(const prefix of ['YX','YC','YK',...(group==='modbus'?['MC']:[]),...(role==='server'&&group==='modbus'?['JDQ']:[])]){
    const left=fields.indexOf('txt'+prefix+'_Add');assert.ok(left>=0);assert.equal(fields[left+1],'txt'+prefix+'_Num',role+'/'+group+'/'+prefix);
   }
  }finally{await h.close();}
 }
});

test('communication editor saves custom listener IP and port without hidden form validation',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]});try{
  h.$('edit-channel').click();const get=id=>originalField(h,id,'original-network-panel');
  get('rbServer').checked=true;get('rbServer').dispatchEvent(new h.w.Event('change',{bubbles:true}));
  assert.equal(get('txtClientIP').disabled,true);assert.equal(get('cbxServerIP').disabled,false);
  get('cbxServerIP').value=' 0.0.0.0 ';get('txtServerPort').value='15020';
  h.$('channel-form').elements.namedItem('address').value='';
  assert.equal(h.$('channel-form').checkValidity(),true);
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  const config=h.calls.find(c=>c[0]==='UpdateTransport')[1][0];assert.equal(config.address,'0.0.0.0:15020');assert.equal(config.transport,'tcp-server');assert.equal(config.localAddress,'');
 }finally{await h.close();}
});

test('communication editor preserves custom IPv6 remote endpoint and local binding',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]});try{
  h.$('edit-channel').click();const get=id=>originalField(h,id,'original-network-panel');
  get('txtClientIP').value=' [::1] ';get('txtClientPort').value='12404';get('ck_AssignIpPort').checked=true;
  get('ck_AssignIpPort').dispatchEvent(new h.w.Event('change',{bubbles:true}));
  get('cb_ClientAssignIp').value='::1';get('txt_ClientAssignPort').value='0';
  assert.equal(get('cb_ClientAssignIp').disabled,false);assert.equal(h.$('channel-form').checkValidity(),true);
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  const config=h.calls.find(c=>c[0]==='UpdateTransport')[1][0];assert.equal(config.address,'[::1]:12404');assert.equal(config.localAddress,'[::1]:0');
 }finally{await h.close();}
});

test('workspace displays and selects the default channel returned after mode initialization',async()=>{
 const h=await setup({project:{name:'empty',channels:[]},states:[]},null,{SetWorkspaceMode:async()=>null});try{
  let ready;h.overrides.SetWorkspaceMode=()=>new Promise(resolve=>{ready=()=>{h.state.project.channels.push({...channel('default'),role:'server',devices:[]});resolve();};});
  h.w.document.querySelector('.mode-switch [data-mode="server"]').click();await tick();ready();await tick();await tick();
  assert.ok(h.w.document.querySelector('[data-select="default"].selected'));assert.equal(h.$('channel-count').textContent,'1');assert.equal(h.calls.filter(c=>c[0]==='StartChannel').length,0);
 }finally{await h.close();}
});
function channel(id,points=[]){return {id,name:id,protocol:'modbus-tcp',role:'client',transport:'tcp-client',address:'127.0.0.1:502',unit:1,commonAddress:1,timeoutMs:3000,baud:9600,dataBits:8,parity:'N',stopBits:1,localAddress:'',points};}
async function setup(config={},nativeContext=null,initialOverrides={}){
  const w=new Window({url:'http://unit.test',settings:{disableCSSFileLoading:true,disableJavaScriptFileLoading:true}});w.structuredClone=structuredClone;w.setInterval=()=>0;w.setTimeout=()=>0;w.fetch=()=>{throw Error('Network forbidden in unit tests');};w.document.write(html);
  const state={project:{name:'test',channels:[channel('a'),channel('b')]},states:[{id:'a',status:'connected',peers:1},{id:'b',status:'connected',peers:1}],messages:[],path:'',dirty:false,...config};const calls=[];const overrides={...initialOverrides};
  const invoke=async(name,args)=>{calls.push([name,args]);if(overrides[name])return overrides[name](...args);if(name==='GetNativeContext')return structuredClone(nativeContext);if(name==='CallNative')return invoke(args[0],args[1]);if(name==='GetSnapshot')return structuredClone(state);if(name==='OpenProject')return '';return null;};
  w.go={main:{App:new Proxy({}, {get:(_,name)=>(...args)=>invoke(name,args)})}};
  w.eval(formCode);w.eval(rendererCode);w.eval(code);await tick();await w.eval('refresh()');return {w,state,calls,overrides,$:id=>w.document.getElementById(id),refresh:async()=>{await w.eval('refresh(true)');await tick();},close:()=>w.happyDOM.abort()};
}
test('polling preserves channel element focus and prevents a duplicate pending connection',async()=>{
  const h=await setup();try{const button=h.w.document.querySelector('[data-select="a"]');button.focus();await h.refresh();assert.equal(h.w.document.activeElement,button);
    h.state.states[0].status='stopped';h.state.states[0].peers=0;await h.refresh();let done;h.overrides.StartChannel=()=>new Promise(resolve=>{done=resolve;});h.$('toggle-channel').click();await tick();assert.equal(h.$('toggle-channel').disabled,true);await h.refresh();assert.equal(h.$('toggle-channel').disabled,true);h.$('toggle-channel').click();assert.equal(h.calls.filter(c=>c[0]==='StartChannel').length,1);done();await tick();await tick();assert.equal(h.$('toggle-channel').disabled,false);
  }finally{await h.close();}
});
test('channel-specific request drafts survive switching',async()=>{
  const h=await setup();try{h.$('read-start').value='123';h.$('read-count').value='17';h.w.document.querySelector('[data-select="b"]').click();await tick();h.$('read-start').value='8';h.w.document.querySelector('[data-select="a"]').click();assert.equal(h.$('read-start').value,'123');assert.equal(h.$('read-count').value,'17');}finally{await h.close();}
});
test('point editor submits only modified fields against its original identity',async()=>{
  const h=await setup({project:{name:'test',channels:[channel('a',[{address:1,kind:'holding',name:'before',value:5,quality:0}])]}});try{
    openPointDetails(h);h.$('point-form').elements.namedItem('name').value='after';h.$('point-form').elements.namedItem('name').dispatchEvent(new h.w.Event('input',{bubbles:true}));h.state.project.channels[0].points[0].value=77;await h.refresh();h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));await tick();await tick();const call=h.calls.find(c=>c[0]==='PatchDevicePoint');assert.ok(call);assert.deepEqual(JSON.parse(JSON.stringify(call[1][2])),{kind:'holding',address:1});assert.deepEqual([...call[1][4]],['name']);assert.equal(call[1][0],'a');assert.equal(call[1][1],'device-1');
  }finally{await h.close();}
});
test('point pages and empty search results are accurate',async()=>{
  const points=Array.from({length:250},(_,i)=>({address:i,kind:'holding',name:`p${i}`,value:i,quality:0}));const h=await setup({project:{name:'test',channels:[channel('a',points)]}});try{assert.equal(h.$('point-rows').children.length,100);h.$('point-next').click();assert.equal(h.$('point-page').textContent,'2 / 3');h.$('point-search').value='no-match';h.$('point-search').dispatchEvent(new h.w.Event('input',{bubbles:true}));assert.equal(h.$('point-empty').hidden,false);assert.equal(h.$('point-empty').textContent,'无匹配结果');}finally{await h.close();}
});
test('cancelled file selection never sends a stop request',async()=>{
  const h=await setup();try{h.$('open-project').click();await tick();h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='ok'));await tick();await tick();assert.equal(h.calls.filter(c=>c[0]==='OpenProject').length,1);assert.equal(h.calls.filter(c=>c[0]==='StopAll').length,0);assert.equal(h.state.states[0].status,'connected');}finally{await h.close();}
});
test('new messages append without rebuilding existing rows; pause freezes the view',async()=>{
  const message=n=>({sequence:n,time:'12:00:00.000',channelId:'a',direction:'RX',summary:'test',hex:'0102'});const h=await setup({messages:[message(1)]});try{const first=h.$('message-rows').firstElementChild;h.state.messages.push(message(2));await h.refresh();assert.equal(h.$('message-rows').firstElementChild,first);assert.equal(h.$('message-rows').children.length,2);h.$('pause-messages').click();h.state.messages.push(message(3));await h.refresh();assert.equal(h.$('message-rows').children.length,2);h.$('pause-messages').click();assert.equal(h.$('message-rows').children.length,3);}finally{await h.close();}
});
test('live point updates preserve the focused edit action',async()=>{
  const h=await setup({project:{name:'test',channels:[channel('a',[{address:1,kind:'holding',name:'p',value:1,quality:0}])]}});try{const button=h.w.document.querySelector('[data-point-field="name"]');button.focus();h.state.project.channels[0].points[0].value=88;await h.refresh();assert.equal(h.w.document.activeElement,button);assert.equal(h.w.document.querySelector('.point-value-input').value,'88');}finally{await h.close();}
});
test('cancelled save-before-open keeps the current session and never opens another file',async()=>{
  const h=await setup({dirty:true});try{h.overrides.SaveProject=()=>'';h.$('open-project').click();await tick();h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='save'));await tick();await tick();assert.equal(h.calls.filter(c=>c[0]==='SaveProject').length,1);assert.equal(h.calls.filter(c=>c[0]==='OpenProject'||c[0]==='StopAll').length,0);}finally{await h.close();}
});

test('station modes isolate channel lists and channel creation follows the selected station',async()=>{
  const server={...channel('s'),role:'server',transport:'tcp-server'};const h=await setup({project:{name:'test',channels:[channel('a'),server]}});try{
    assert.ok(h.w.document.querySelector('[data-select="a"]'));assert.equal(h.w.document.querySelector('[data-select="s"]'),null);
    h.w.document.querySelector('[data-mode="server"]').click();assert.ok(h.w.document.querySelector('[data-select="s"]'));assert.equal(h.w.document.querySelector('[data-select="a"]'),null);
    h.$('add-channel').click();await tick();h.$('confirm-actions').querySelector('button[value=ok]').click();await settleInline();const config=h.calls.find(c=>c[0]==='UpsertChannel')[1][0];assert.equal(config.role,'server');assert.equal(config.protocol,'raw');assert.equal(config.transport,'tcp-server');assert.equal(h.calls.filter(c=>c[0]==='StartChannel').length,0);
  }finally{await h.close();}
});

test('communication and device configuration reproduce separate original windows',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]});try{h.$('edit-channel').click();assert.equal(h.$('channel-dialog-title').textContent,'通信配置窗口');assert.ok(h.$('original-network-panel').textContent.includes('串口配置'));assert.ok(h.$('original-network-panel').querySelector('[data-original-id="txtClientIP"]'));h.$('channel-dialog').close();h.$('device-config').click();assert.equal(h.$('device-dialog-title').textContent,'查看/修改装置配置');assert.equal(h.$('device-protocol').value,'modbus');assert.equal(h.$('device-version').value,'TCP版');assert.equal(h.$('original-device-panel').querySelector('[data-original-id="txtAdd"]').value,'1');assert.equal(h.$('original-device-panel').querySelector('[data-original-id="txtAdd"]').disabled,false);}finally{await h.close();}
});

test('point double-click edits and new points follow the selected type tab',async()=>{
  const points=[{address:1,kind:'holding',name:'p',value:5,quality:0}];const h=await setup({project:{name:'test',channels:[channel('a',points)]}});try{
    h.$('point-rows').querySelector('td').dispatchEvent(new h.w.MouseEvent('dblclick',{bubbles:true}));assert.equal(h.$('point-dialog').open,true);assert.equal(h.$('point-form').elements.namedItem('value').value,'5');h.$('point-dialog').close();
    h.w.document.querySelector('[data-point-kind="input"]').click();assert.equal(h.$('point-rows').children.length,0);h.$('add-point').click();assert.equal(h.$('point-form').elements.namedItem('kind').value,'input');
  }finally{await h.close();}
});

test('Space pause is channel-scoped and X clears only current channel outside editors',async()=>{
  const m=(n,id)=>({sequence:n,time:'12:00',channelId:id,direction:'RX',summary:'test',hex:'0102'});const h=await setup({messages:[m(1,'a'),m(2,'b')]});try{
    h.w.document.body.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:' ',bubbles:true}));assert.equal(h.$('pause-messages').textContent,'继续');h.w.document.querySelector('[data-select="b"]').click();assert.equal(h.$('pause-messages').textContent,'暂停');h.w.document.querySelector('[data-select="a"]').click();assert.equal(h.$('pause-messages').textContent,'继续');
    h.$('message-search').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'x',bubbles:true}));await tick();assert.equal(h.calls.filter(c=>c[0]==='ClearChannelMessages').length,0);
    h.overrides.ClearChannelMessages=id=>{h.state.messages=h.state.messages.filter(m=>m.channelId!==id);};h.w.document.body.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'x',bubbles:true}));await tick();await tick();assert.equal(h.calls.find(c=>c[0]==='ClearChannelMessages')[1][0],'a');assert.equal(h.state.messages[0].channelId,'b');assert.equal(h.$('message-rows').children.length,0);
  }finally{await h.close();}
});

test('message export uses the current channel and visible filters',async()=>{
  const m=(n,id,direction)=>({sequence:n,time:'12:00',channelId:id,direction,summary:'test',hex:'0102'});const h=await setup({messages:[m(1,'a','RX'),m(2,'a','TX'),m(3,'b','RX')]});try{
    h.$('direction-filter').value='RX';h.$('direction-filter').dispatchEvent(new h.w.Event('change',{bubbles:true}));h.$('export-messages').click();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='ExportMessageRows')[1][0]].map(m=>m.sequence),[1]);
    h.w.document.querySelector('[data-select="a"]').dispatchEvent(new h.w.MouseEvent('contextmenu',{bubbles:true,clientX:20,clientY:30}));assert.equal(h.$('context-menu').hidden,false);assert.deepEqual([...h.$('context-menu').children].map(b=>b.textContent),['重命名','复制','删除']);
  }finally{await h.close();}
});

function multiChannel(){return {...channel('a'),devices:[{id:'one',name:'装置一',unit:1,commonAddress:1,points:[{address:0,kind:'holding',value:111,quality:0,name:'first'}]},{id:'two',name:'装置二',unit:2,commonAddress:2,points:[{address:0,kind:'holding',value:222,quality:0,name:'second'}]}]};}
test('selecting devices switches independent point tables and request drafts without changing channel messages',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel(),channel('b')]},messages:[{sequence:1,time:'12:00',channelId:'a',direction:'RX',summary:'shared',hex:'0102'}]});try{
  assert.equal(h.$('device-count').textContent,'2');assert.equal(h.$('selected-device-name').textContent,'装置一');assert.equal(h.w.document.querySelector('.point-value-input').value,'111');h.$('read-start').value='17';
  h.w.document.querySelector('[data-device="two"]').click();assert.equal(h.$('selected-device-name').textContent,'装置二');assert.equal(h.w.document.querySelector('.point-value-input').value,'222');assert.equal(h.$('message-rows').children.length,1);h.$('read-start').value='28';h.$('read-modbus').click();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='ReadDeviceModbus')[1]].slice(0,4),['a','two',3,28]);
  h.w.document.querySelector('[data-device="one"]').click();assert.equal(h.$('read-start').value,'17');h.w.document.querySelector('[data-device="two"]').click();assert.equal(h.$('read-start').value,'28');h.w.document.querySelector('[data-select="b"]').click();h.w.document.querySelector('[data-select="a"]').click();assert.equal(h.$('selected-device-name').textContent,'装置一');
 }finally{await h.close();}
});
function chooseDeviceProtocol(h,group,version){h.$('device-protocol').value=group;h.$('device-protocol').dispatchEvent(new h.w.Event('change',{bubbles:true}));if(version){h.$('device-version').value=version;h.$('device-version').dispatchEvent(new h.w.Event('change',{bubbles:true}));}}
function originalField(h,id,host='original-device-panel'){return h.$(host).querySelector('[data-original-id="'+id+'"]');}
test('adding devices closes on success and retains the incremented address for reopening',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]}});try{
 h.overrides.UpsertDevice=(id,protocol,d)=>{h.state.project.channels[0].devices.push({...d,id:'three'});return 'three';};h.$('add-device').click();assert.equal(h.$('device-protocol').value,'iec104');assert.equal(h.$('device-version').value,'标准版');assert.ok(originalField(h,'txtYC_Num'));chooseDeviceProtocol(h,'modbus','TCP版');assert.equal(originalField(h,'txtAdd').value,'1');originalField(h,'txtAdd').value='3';h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();await tick();
 assert.equal(h.$('device-count').textContent,'3');assert.equal(h.$('selected-device-name').textContent,'装置一');assert.equal(h.$('device-dialog').open,false);assert.equal(originalField(h,'txtAdd').value,'4');const payload=h.calls.find(c=>c[0]==='UpsertDevice')[1][2];assert.equal(payload.configuration.ranges[0].address,1);assert.equal(payload.configuration.ranges[0].count,50);assert.equal(payload.configuration.ranges[1].address,1024);assert.equal(h.calls.filter(c=>c[0]==='StartChannel'||c[0]==='UpsertChannel').length,0);
 h.w.document.querySelector('[data-close="device-dialog"]').click();h.$('add-device').click();assert.equal(h.$('device-protocol').value,'modbus');assert.equal(originalField(h,'txtAdd').value,'4');
 }finally{await h.close();}
});

test('device context deletion targets the selected device and returns to first remaining device',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]}});try{
  h.overrides.DeleteDevice=(id,did)=>{h.state.project.channels[0].devices=h.state.project.channels[0].devices.filter(d=>d.id!==did);};h.w.document.querySelector('[data-device="two"]').dispatchEvent(new h.w.MouseEvent('contextmenu',{bubbles:true,clientX:20,clientY:20}));const remove=[...h.$('context-menu').children].find(b=>b.textContent==='移除选中装置');remove.click();await tick();h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='ok'));await tick();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='DeleteDevice')[1]],['a','two']);assert.equal(h.$('selected-device-name').textContent,'装置一');assert.equal(h.$('device-count').textContent,'1');
 }finally{await h.close();}
});
test('removing the last device clears its table and disables device operations',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]}});try{h.state.project.channels[0].devices=[];await h.refresh();assert.equal(h.$('point-rows').children.length,0);assert.equal(h.$('device-config').disabled,true);assert.equal(h.$('add-point').disabled,true);assert.equal(h.$('read-modbus').disabled,true);assert.equal(h.$('add-device').disabled,false);}finally{await h.close();}
});
test('device CSV import and export use the selected device identity',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]}});try{h.w.document.querySelector('[data-device="two"]').click();h.$('export-points').click();await tick();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='ExportDevicePoints')[1]],['a','two']);h.$('import-points').click();await tick();h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='ok'));await tick();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='ImportDevicePoints')[1]],['a','two']);}finally{await h.close();}
});
test('cancelling a protocol replacement leaves every existing device untouched',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]},states:[{id:'a',status:'stopped'}]});try{h.$('add-device').click();chooseDeviceProtocol(h,'iec104','标准版');h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();assert.equal(h.$('confirm-dialog').open,true);h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='cancel'));await tick();await tick();assert.equal(h.calls.filter(c=>c[0]==='UpsertDevice').length,0);assert.equal(h.state.project.channels[0].devices.length,2);}finally{await h.close();}
});

test('device list keyboard navigation selects the corresponding point table',async()=>{
 const h=await setup({project:{name:'multi',channels:[multiChannel()]}});try{h.w.document.querySelector('[data-device="one"]').focus();h.$('device-list').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true}));assert.equal(h.$('selected-device-name').textContent,'装置二');assert.equal(h.w.document.activeElement.dataset.device,'two');h.$('device-list').dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Home',bubbles:true}));assert.equal(h.$('selected-device-name').textContent,'装置一');}finally{await h.close();}
});

test('adding IEC104 requires replacement confirmation and replaces the original device',async()=>{
 const c={...channel('a'),protocol:'iec104',devices:[{id:'old',name:'old 104',unit:1,commonAddress:1,points:[]}]};const h=await setup({project:{name:'104',channels:[c]}});try{
 h.overrides.UpsertDevice=(cid,protocol,d,replace)=>{assert.equal(replace,true);h.state.project.channels[0].devices=[{...d,id:'new'}];h.state.states[0].status='stopped';return 'new';};h.$('add-device').click();chooseDeviceProtocol(h,'iec104','标准版');h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();assert.equal(h.$('confirm-title').textContent,'替换 IEC104 装置');assert.ok(h.$('confirm-text').textContent.includes('当前通道将停止'));assert.equal(h.calls.filter(c=>c[0]==='UpsertDevice').length,0);
 h.$('confirm-form').requestSubmit([...h.$('confirm-actions').children].find(b=>b.value==='ok'));await tick();await tick();assert.equal(h.calls.find(c=>c[0]==='UpsertDevice')[1][2].id,'');assert.equal(h.$('device-count').textContent,'1');assert.equal(h.w.document.querySelector('[data-device="old"]'),null);assert.equal(h.state.states[0].status,'stopped');
 }finally{await h.close();}
});

test('unused device address fields cannot block form validation',async()=>{
 const c=multiChannel();c.devices[0].commonAddress=0;const h=await setup({project:{name:'fields',channels:[c]}});try{h.$('device-config').click();assert.equal(h.$('device-form').checkValidity(),true);h.$('device-dialog').close();h.state.states[0].status='stopped';await h.refresh();h.$('edit-channel').click();assert.equal(h.$('channel-form').checkValidity(),true);}finally{await h.close();}
});

test('add channel creates a numbered empty channel without opening a configuration window',async()=>{
 const h=await setup({project:{name:'empty',channels:[]},states:[]});try{h.overrides.UpsertChannel=c=>{h.state.project.channels.push({...c,id:'zero',devices:[]});return 'zero';};h.$('add-channel').click();await tick();assert.equal(h.$('confirm-input').value,'通道1');h.$('confirm-actions').querySelector('button[value=ok]').click();await settleInline();const c=h.calls.find(c=>c[0]==='UpsertChannel')[1][0];assert.equal(c.name,'通道1');assert.equal(c.protocol,'raw');assert.equal(h.w.document.querySelector('dialog[open]'),null);assert.equal(h.$('device-count').textContent,'0');}finally{await h.close();}
});
test('modeless add-device window targets the currently selected channel',async()=>{
 const h=await setup();try{h.$('add-device').click();chooseDeviceProtocol(h,'modbus','TCP版');h.w.document.querySelector('[data-select="b"]').click();h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();await tick();assert.equal(h.calls.find(c=>c[0]==='UpsertDevice')[1][0],'b');assert.equal(h.$('device-dialog').open,false);}finally{await h.close();}
});
test('original semantic tabs filter configured point ranges and set the read target',async()=>{
 const c=multiChannel();c.devices[0].configuration={version:'TCP版',values:{},ranges:[{tab:'yx',kind:'discrete',address:1,count:18,name:'遥信'},{tab:'yc',kind:'holding',address:1024,count:18,name:'遥测'}]};c.devices[0].points=[{kind:'discrete',address:1,name:'遥信1',value:0,quality:0},{kind:'holding',address:1024,name:'遥测1',value:23,quality:0}];const h=await setup({project:{name:'tabs',channels:[c]}});try{assert.equal(h.$('point-rows').children.length,1);assert.equal(h.$('read-start').value,'1');h.w.document.querySelector('[data-point-kind="tab:yc"]').click();assert.equal(h.$('point-rows').firstElementChild.children[0].querySelector('input').value,'1024');assert.equal(h.$('read-fc').value,'3');assert.equal(h.$('read-start').value,'1024');assert.equal(h.$('read-count').value,'18');}finally{await h.close();}
});
test('network dialog saves separate client/server IP and port controls',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]});try{h.$('edit-channel').click();originalField(h,'txtClientIP','original-network-panel').value='192.0.2.9';originalField(h,'txtClientPort','original-network-panel').value='1502';h.$('channel-form').requestSubmit(h.$('channel-form').querySelector('#original-network-panel button[type=submit]'));await tick();await tick();const payload=h.calls.find(c=>c[0]==='UpdateTransport')[1][0];assert.equal(payload.address,'192.0.2.9:1502');assert.equal(payload.devices.length,1);assert.equal(payload.transport,'tcp-client');}finally{await h.close();}
});

test('Modbus defaults to RTU and empty information-body fields are rejected',async()=>{
 const h=await setup({project:{name:'raw',channels:[{...channel('a'),protocol:'raw',devices:[]}]},states:[{id:'a',status:'stopped'}]});try{h.$('add-device').click();chooseDeviceProtocol(h,'modbus');assert.equal(h.$('device-version').value,'标准版(RTU)');originalField(h,'txtYX_Num').value='';h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();assert.equal(h.calls.filter(c=>c[0]==='UpsertDevice').length,0);assert.equal(h.$('device-form').querySelector('.form-error').hidden,false);originalField(h,'txtYX_Num').value='18';h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();await tick();assert.equal(h.calls.find(c=>c[0]==='UpsertDevice')[1][1],'modbus-rtu');assert.equal(h.calls.filter(c=>c[0]==='UpsertChannel').length,0);}finally{await h.close();}
});

test('server Modbus and IEC104 use the original server panels and point defaults',async()=>{
 for(const group of ['modbus','iec104']){const c={...channel('s'),protocol:'raw',role:'server',transport:'tcp-server',address:'127.0.0.1:0',devices:[]};const h=await setup({project:{name:'server',channels:[c]},states:[{id:'s',status:'stopped'}]});try{h.w.document.querySelector('.mode-switch [data-mode="server"]').click();h.$('add-device').click();assert.equal(h.$('device-dialog-title').textContent,'子站增加装置');assert.equal(h.$('device-protocol').value,'iec104');assert.equal(h.$('device-version').value,'标准版');chooseDeviceProtocol(h,group,group==='modbus'?'TCP版':'标准版');if(group==='iec104'){assert.equal(originalField(h,'txt_DeviceAddr').value,'1');assert.equal(originalField(h,'txtAdd').value,'3');}h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();await tick();const payload=h.calls.find(c=>c[0]==='UpsertDevice')[1][2];assert.equal(payload.configuration.ranges.length,group==='modbus'?5:4);assert.equal(payload.configuration.ranges[0].address,1);assert.equal(payload.configuration.ranges[0].count,50);if(group==='modbus')assert.equal(payload.configuration.ranges[4].tab,'jdq');else assert.equal(payload.configuration.ranges[1].address,16385);}finally{await h.close();}}
});


test('desktop workspace opens native windows without opening a DOM overlay',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]}, {kind:''});try{
  for(const id of ['edit-channel','add-device','add-point','communication-manager']){h.$(id).click();await tick();assert.equal(h.w.document.querySelector('dialog[open]'),null);}
  const contexts=h.calls.filter(c=>c[0]==='OpenNativeWindow').map(c=>c[1][0]);
  assert.deepEqual(contexts.map(c=>c.kind),['channel-dialog','device-dialog','point-dialog','manager-dialog']);
  assert.equal(contexts[0].channel,'a');assert.equal(contexts[0].edit,true);assert.equal(contexts[1].edit,false);
  h.overrides.AskNative=()=>({choice:'cancel'});h.$('new-project').click();await tick();assert.ok(h.calls.some(c=>c[0]==='AskNative'));assert.equal(h.$('confirm-dialog').open,false);
 }finally{await h.close();}
});

test('native device editor follows the parent selection immediately on submit',async()=>{
 const context={kind:'device-dialog',role:'client',channel:'a',device:'',edit:false,activate:0};
 const channels=['a','b'].map(id=>({...channel(id),protocol:'raw',devices:[]}));
 const h=await setup({project:{name:'shared',channels},states:[]},context);try{
  assert.equal(h.w.document.body.classList.contains('native-window'),true);assert.equal(h.$('device-dialog').open,true);
  chooseDeviceProtocol(h,'modbus','TCP版');originalField(h,'txtAdd').value='7';context.channel='b';
  h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));for(let i=0;i<5;i++)await tick();
  const call=h.calls.find(c=>c[0]==='UpsertDevice');assert.ok(call);assert.equal(call[1][0],'b');assert.equal(call[1][2].unit,7);assert.equal(originalField(h,'txtAdd').value,'8');assert.ok(h.calls.some(c=>c[0]==='CloseNativeWindow'));
  assert.ok(h.calls.some(c=>c[0]==='SaveNativeDraft'));
 }finally{await h.close();}
});

test('native point editor patches its captured identity and closes only the child window',async()=>{
 const points=[{kind:'holding',address:1,name:'p',value:5,quality:0}];const context={kind:'point-dialog',role:'client',channel:'a',device:'device-1',edit:true,point:'holding:1'};
 const h=await setup({project:{name:'points',channels:[channel('a',points)]}},context);try{
  const input=h.$('point-form').elements.namedItem('value');input.value='9';input.dispatchEvent(new h.w.Event('input',{bubbles:true}));
  h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));for(let i=0;i<4;i++)await tick();
  const call=h.calls.find(c=>c[0]==='PatchDevicePoint');assert.equal(call[1][0],'a');assert.equal(call[1][1],'device-1');assert.equal(call[1][2].address,1);assert.deepEqual([...call[1][4]],['value']);assert.ok(h.calls.some(c=>c[0]==='CloseNativeWindow'));assert.equal(h.calls.some(c=>c[0]==='StopAll'),false);
 }finally{await h.close();}
});

test('native confirmation returns its result over IPC before closing',async()=>{
 const context={kind:'confirm-dialog',ask:{title:'名称',input:'old',choices:[['cancel','取消'],['ok','确认']]}};const h=await setup({},context);try{
  h.$('confirm-input').value='new';h.$('confirm-form').requestSubmit(h.$('confirm-actions').lastElementChild);for(let i=0;i<3;i++)await tick();
  const result=h.calls.find(c=>c[0]==='ResolveNativeAsk');assert.equal(result[1][0].choice,'ok');assert.equal(result[1][0].value,'new');assert.ok(h.calls.findIndex(c=>c[0]==='CloseNativeWindow')>h.calls.findIndex(c=>c[0]==='ResolveNativeAsk'));
 }finally{await h.close();}
});

test('unimplemented commands, protocol choices and parameters are absent',async()=>{
 const h=await setup({states:[{id:'a',status:'stopped'}]});try{
  assert.equal(h.$('tool-menu'),null);assert.equal(h.w.document.body.textContent.includes('特殊功能'),false);assert.equal(h.w.document.querySelector('#launch-screen button:disabled'),null);
  h.$('add-device').click();assert.deepEqual([...h.$('device-protocol').options].map(o=>o.value),['','iec104','modbus']);
  chooseDeviceProtocol(h,'iec104');assert.deepEqual([...h.$('device-version').options].map(o=>o.textContent),['标准版']);
  assert.equal(originalField(h,'ckbEnableEncrypt'),null);for(const id of ['txtASDU_Byte','txtUTest_Time','txtMC_Num'])assert.ok(originalField(h,id));
  assert.equal(h.$('original-device-panel').innerHTML.includes('position:absolute'),false);
  h.$('device-dialog').close();h.$('edit-channel').click();assert.equal(originalField(h,'ck_IsAutoReLink','original-network-panel'),null);assert.equal(originalField(h,'txt_TCPServerMaxConnect','original-network-panel'),null);
  for(const id of ['cbParity','cbStopBit'])assert.equal([...originalField(h,id,'original-network-panel').options].some(o=>['Mark','Space','1.5'].includes(o.value)),false);
 }finally{await h.close();}
});


function iecChannel(id='a',points=[]){return {...channel(id,points),protocol:'iec104'};}

test('IEC104 remote control and adjustment open independent desktop windows',async()=>{
 const h=await setup({project:{name:'controls',channels:[iecChannel('a',[{kind:'yk',address:5000,value:1,quality:0}])]}},{kind:''});try{
  for(const [button,mode,type]of [['open-yk','yk',45],['open-step','step',47],['open-yt','setpoint',50]]){h.$(button).click();await tick();const call=h.calls.filter(c=>c[0]==='OpenNativeWindow').at(-1);assert.equal(call[1][0].kind,'control-dialog');assert.equal(call[1][0].point,mode);assert.equal(call[1][0].control.type,type);assert.equal(call[1][0].channel,'a');assert.equal(h.$('control-dialog').open,false);}
 }finally{await h.close();}
});

test('native remote control waits for selection confirmation before execute',async()=>{
 const context={kind:'control-dialog',role:'client',channel:'a',device:'device-1',point:'yk',control:{type:46,address:5000,value:2}};
 const h=await setup({project:{name:'controls',channels:[iecChannel()]}},context);try{
  assert.equal(h.$('control-dialog').open,true);assert.equal(h.$('control-execute').disabled,true);let reply;
  h.overrides.Command104Device=()=>new Promise(resolve=>{reply=resolve;});h.$('control-select').click();await tick();assert.equal(h.$('control-select').disabled,true);assert.equal(h.$('control-execute').disabled,true);
  let request=h.calls.filter(c=>c[0]==='Command104Device').at(-1)[1];assert.equal(request[0],'a');assert.equal(request[1],'device-1');assert.equal(request[2].type,46);assert.equal(request[2].value,2);assert.equal(request[2].operation,'select');
  reply({stage:'selected',message:'选择确认'});for(let i=0;i<4;i++)await tick();assert.equal(h.$('control-execute').disabled,false);assert.equal(h.$('control-cancel').disabled,false);
  h.overrides.Command104Device=()=>({stage:'completed',message:'执行结束'});h.$('control-execute').click();for(let i=0;i<4;i++)await tick();assert.equal(h.$('control-result').textContent,'执行结束');assert.equal(h.$('control-execute').disabled,true);
 }finally{await h.close();}
});

test('editing control parameters invalidates selection; negative replies never show success',async()=>{
 const context={kind:'control-dialog',role:'client',channel:'a',device:'device-1',point:'yk',control:{type:45,address:5000,value:1}};
 const h=await setup({project:{name:'controls',channels:[iecChannel()]}},context);try{
  h.overrides.Command104Device=()=>({stage:'selected',message:'选择确认'});h.$('control-select').click();for(let i=0;i<4;i++)await tick();assert.equal(h.$('control-execute').disabled,false);
  h.$('control-address').value='5001';h.$('control-address').dispatchEvent(new h.w.Event('input',{bubbles:true}));assert.equal(h.$('control-execute').disabled,true);assert.equal(h.$('control-cancel').disabled,true);
  h.overrides.Command104Device=()=>{throw Error('子站拒绝命令（原因 47）');};h.$('control-select').click();for(let i=0;i<4;i++)await tick();assert.ok(h.$('control-result').textContent.includes('拒绝'));assert.equal(h.$('control-execute').disabled,true);assert.equal(h.$('control-form').querySelector('.form-error').hidden,false);
 }finally{await h.close();}
});

test('setpoint and step controls validate their actual wire values',async()=>{
 for(const mode of ['setpoint','step']){
  const context={kind:'control-dialog',role:'client',channel:'a',device:'device-1',point:mode,control:{type:mode==='step'?47:50,address:123,value:0}};
  const h=await setup({project:{name:'controls',channels:[iecChannel()]}},context);try{
   h.overrides.Command104Device=()=>({stage:'completed',message:'执行结束'});
   if(mode==='setpoint'){
    assert.equal(h.$('control-direct').checked,true);h.$('control-value').value='1.5';h.$('control-execute').click();for(let i=0;i<4;i++)await tick();assert.equal(h.calls.filter(c=>c[0]==='Command104Device').at(-1)[1][2].value,1.5);
    h.$('control-type').value='49';h.$('control-type').dispatchEvent(new h.w.Event('change',{bubbles:true}));h.$('control-value').value='1.5';const count=h.calls.filter(c=>c[0]==='Command104Device').length;h.$('control-execute').click();await tick();assert.equal(h.calls.filter(c=>c[0]==='Command104Device').length,count);
   }else{h.$('control-direct').checked=true;h.$('control-direct').dispatchEvent(new h.w.Event('change',{bubbles:true}));h.$('control-state').value='1';h.$('control-execute').click();for(let i=0;i<4;i++)await tick();const request=h.calls.filter(c=>c[0]==='Command104Device').at(-1)[1][2];assert.equal(request.type,47);assert.equal(request.value,1);}
  }finally{await h.close();}
 }
});

test('read-only device confirmation does not regenerate imported control points',async()=>{
 const c=iecChannel('a',[{kind:'yt',address:123,value:42,quality:0}]);const h=await setup({project:{name:'controls',channels:[c]}});try{h.$('device-config').click();h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));for(let i=0;i<3;i++)await tick();assert.equal(h.calls.some(c=>c[0]==='UpsertDevice'),false);assert.equal(h.state.project.channels[0].points[0].value,42);}finally{await h.close();}
});


test('double-point OFF is encoded as 1 rather than ON when reopening a native command window',async()=>{
 const context={kind:'control-dialog',role:'client',channel:'a',device:'device-1',point:'yk',control:{type:46,address:5000,value:1}};
 const h=await setup({project:{name:'controls',channels:[iecChannel()]}},context);try{
  assert.equal(h.$('control-state').value,'0');h.overrides.Command104Device=()=>({stage:'selected',message:'选择确认'});h.$('control-select').click();for(let i=0;i<4;i++)await tick();assert.equal(h.calls.filter(c=>c[0]==='Command104Device').at(-1)[1][2].value,1);
 }finally{await h.close();}
});


function autoSnapshot(mode='yk'){
 return {id:'job-1',channel:'a',device:'device-1',mode,status:'idle',revision:1,run:0,current:-1,total:0,success:0,failure:0,errors:[],lastError:'',options:{intervalMs:mode==='yk'?2:200,timeoutMs:10000,double:false,cycle:false,direct:false,checkFeedback:false,autoPause:true,valueType:50},rows:[0,1].map(i=>({address:100+i,name:'point '+i,enabled:true,action:'合闸后分闸',count:1,value:'10',feedback:'',feedbackName:'未关联',selection:'',execution:'',feedbackResult:'',executed:'否'}))};
}
async function setupAuto(mode='yk'){
 const data=autoSnapshot(mode),copy=()=>structuredClone(data),mutate=fn=>{fn();data.revision++;return copy();};
 const context={kind:'auto-'+mode+'-dialog',role:'client',channel:'a',device:'device-1',point:mode,edit:true};
 const handlers={GetAutoControl:copy,UpdateAutoOptions:(_c,_d,_m,o)=>mutate(()=>{data.options=structuredClone(o);}),UpdateAutoRows:(_c,_d,_m,edits)=>mutate(()=>{for(const edit of edits)Object.assign(data.rows[edit.index],edit);}),StartAutoControl:()=>mutate(()=>{data.status='running';data.run++;}),PauseAutoControl:()=>mutate(()=>{data.status='paused';}),ResumeAutoControl:()=>mutate(()=>{data.status='running';}),StopAutoControl:()=>mutate(()=>{data.status='stopped';})};
 const h=await setup({project:{name:'auto',channels:[iecChannel('a')]}},context,handlers);for(let i=0;i<3;i++)await tick();return {...h,data,poll:async()=>{await h.w.eval('pollAutoControl()');await tick();}};
}
async function settleAuto(){for(let i=0;i<6;i++)await tick();}

test('automatic command launchers prepare original address sources and open native windows',async()=>{
 const h=await setup({project:{name:'auto',channels:[iecChannel('a',[{kind:'yk',address:100,value:0},{kind:'yt',address:200,value:0}])]}},{kind:''},{PrepareAutoControl:()=>autoSnapshot()});try{
  h.$('open-auto-yk').click();await settleAuto();let call=h.calls.filter(c=>c[0]==='OpenNativeWindow').at(-1);assert.equal(call[1][0].kind,'auto-yk-dialog');assert.equal(call[1][0].device,'device-1');
  h.$('auto-yt-addresses').value='5,1-3';h.$('auto-yt-value').value='10H';h.$('auto-yt-type').value='49';h.$('open-auto-yt').click();await settleAuto();call=h.calls.filter(c=>c[0]==='PrepareAutoControl').at(-1);assert.equal(call[1][2],'yt');assert.deepEqual(JSON.parse(JSON.stringify(call[1][3])),{addresses:'5,1-3',value:'10H',valueType:49});assert.equal(h.calls.filter(c=>c[0]==='OpenNativeWindow').at(-1)[1][0].kind,'auto-yt-dialog');assert.equal(h.w.document.querySelector('dialog[open]'),null);
 }finally{await h.close();}
});

test('automatic YK window reproduces original columns, defaults and pause/resume state machine',async()=>{
 const h=await setupAuto('yk');try{
  assert.deepEqual([...h.$('auto-yk-dialog').querySelectorAll('th')].map(el=>el.textContent),['序号','启用','点地址','点名称','遥控类型','遥控选择','遥控执行','遥控次数','遥信关联','变位结果']);
  assert.equal(h.$('auto-yk-interval').value,'2');assert.equal(h.$('auto-yk-timeout').value,'10000');assert.equal(h.$('auto-yk-autopause').checked,true);assert.equal(h.$('auto-yk-stop').disabled,true);
  h.$('auto-yk-start').click();await settleAuto();assert.equal(h.$('auto-yk-start').textContent,'暂停遥控');assert.equal(h.$('auto-yk-stop').disabled,false);
  h.data.total=3;h.data.success=2;h.data.failure=1;h.data.revision++;await h.poll();h.$('auto-yk-start').click();await settleAuto();assert.equal(h.$('auto-yk-start').textContent,'继续遥控');assert.equal(h.$('auto-yk-total').textContent,'遥控次数：3');
  h.$('auto-yk-start').click();await settleAuto();assert.ok(h.calls.some(c=>c[0]==='ResumeAutoControl'));assert.equal(h.data.run,1);assert.equal(h.data.total,3);
  h.$('auto-yk-stop').click();await settleAuto();assert.equal(h.$('auto-yk-start').textContent,'自动遥控');assert.equal(h.$('auto-yk-stop').disabled,true);assert.equal(h.calls.some(c=>c[0]==='StopChannel'),false);
 }finally{await h.close();}
});

test('automatic polling preserves focused cell edits and updates real row outcomes',async()=>{
 const h=await setupAuto('yk');try{
  const row=h.$('auto-yk-rows').children[0],input=row.querySelector('[data-auto-field="count"]');input.focus();input.value='3';h.data.current=0;h.data.rows[0].selection='第1次合闸选择成功';h.data.revision++;await h.poll();assert.equal(h.w.document.activeElement,input);assert.equal(input.value,'3');assert.equal(row.children[5].textContent,'第1次合闸选择成功');
  input.dispatchEvent(new h.w.Event('change',{bubbles:true}));await settleAuto();const call=h.calls.filter(c=>c[0]==='UpdateAutoRows').at(-1);assert.equal(call[1][3][0].count,3);assert.equal(call[1][0],'a');assert.equal(call[1][1],'device-1');
  h.data.rows[0].execution='第1次合闸执行成功';h.data.rows[0].feedbackResult='变位成功';h.data.revision++;await h.poll();assert.equal(row.children[6].textContent,'第1次合闸执行成功');assert.equal(row.children[9].textContent,'变位成功');
 }finally{await h.close();}
});

test('automatic YK right-click opens the independent original bulk editor',async()=>{
 const h=await setupAuto('yk');try{h.$('auto-yk-rows').dispatchEvent(new h.w.MouseEvent('contextmenu',{bubbles:true,clientX:10,clientY:10}));const button=h.$('context-menu').querySelector('button');assert.equal(button.textContent,'批量修改');button.click();await settleAuto();const call=h.calls.find(c=>c[0]==='OpenAutoBulk');assert.deepEqual([...call[1]],['a','device-1']);}finally{await h.close();}
});

test('bulk changes apply only checked fields to the captured channel/device',async()=>{
 const context={kind:'auto-bulk-dialog',role:'client',channel:'a',device:'device-1',point:'yk'};
 const h=await setup({},context,{ApplyAutoBulk:()=>autoSnapshot()});try{
  h.$('auto-bulk-action-check').checked=true;h.$('auto-bulk-action').value='分闸';h.$('auto-bulk-count-check').checked=true;h.$('auto-bulk-count').value='4';h.$('auto-bulk-form').requestSubmit(h.$('auto-bulk-form').querySelector('button'));await settleAuto();const call=h.calls.find(c=>c[0]==='ApplyAutoBulk');assert.equal(call[1][0],'a');assert.equal(call[1][1],'device-1');assert.equal(call[1][2].changeEnabled,false);assert.equal(call[1][2].action,'分闸');assert.equal(call[1][2].count,4);assert.ok(h.calls.some(c=>c[0]==='CloseNativeWindow'));
 }finally{await h.close();}
});

test('automatic YT has original five columns, interval and start/end controls',async()=>{
 const h=await setupAuto('yt');try{
  assert.deepEqual([...h.$('auto-yt-dialog').querySelectorAll('th')].map(el=>el.textContent),['序号','点地址','点名称','遥调数值','是否已执行']);assert.equal(h.$('auto-yt-interval').value,'200');assert.equal(h.$('auto-yt-dialog').querySelector('input[type="checkbox"]'),null);
  const input=h.$('auto-yt-rows').children[1].querySelector('input');input.value='1.5';input.dispatchEvent(new h.w.Event('change',{bubbles:true}));await settleAuto();h.$('auto-yt-start').click();await settleAuto();assert.equal(h.$('auto-yt-start').disabled,true);assert.equal(h.$('auto-yt-stop').disabled,false);assert.equal(h.data.rows[1].value,'1.5');
  h.data.rows[0].executed='是';h.data.revision++;await h.poll();assert.equal(h.$('auto-yt-rows').children[0].children[4].textContent,'是');assert.equal(h.$('auto-yt-rows').children[1].children[4].textContent,'否');
  h.$('auto-yt-stop').click();await settleAuto();assert.equal(h.$('auto-yt-start').disabled,false);
 }finally{await h.close();}
});

test('closing an automatic window never sends a stop or resets its task',async()=>{
 const h=await setupAuto('yk');try{h.$('auto-yk-start').click();await settleAuto();h.$('auto-yk-dialog').close();await settleAuto();assert.ok(h.calls.some(c=>c[0]==='CloseNativeWindow'));assert.equal(h.calls.some(c=>c[0]==='StopAutoControl'),false);assert.equal(h.calls.some(c=>c[0]==='StopAll'),false);assert.equal(h.data.status,'running');}finally{await h.close();}
});

test('invalid repetition input prevents automatic start rather than using stale row values',async()=>{
 const h=await setupAuto('yk');try{h.$('auto-yk-rows').children[0].querySelector('[data-auto-field="count"]').value='';h.$('auto-yk-start').click();await settleAuto();assert.equal(h.calls.some(c=>c[0]==='StartAutoControl'),false);assert.equal(h.$('auto-yk-form').querySelector('.form-error').hidden,false);}finally{await h.close();}
});

test('point editor exposes and saves the original inverse YX association syntax',async()=>{
 const h=await setup({project:{name:'link',channels:[iecChannel('a',[{kind:'yk',address:100,value:0,quality:0,feedback:'!1'}])]}});try{
  h.w.document.querySelector('[data-point-kind="yk"]').click();openPointDetails(h);const field=h.$('point-form').elements.namedItem('feedback');assert.equal(field.value,'!1');assert.equal(h.$('point-feedback-label').hidden,false);field.value='2';field.dispatchEvent(new h.w.Event('input',{bubbles:true}));h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));await settleAuto();const call=h.calls.find(c=>c[0]==='PatchDevicePoint');assert.equal(call[1][3].feedback,'2');assert.deepEqual([...call[1][4]],['feedback']);
 }finally{await h.close();}
});

 test('failed device creation leaves the editor open for correction',async()=>{
 const h=await setup();try{h.overrides.UpsertDevice=()=>{throw Error('duplicate device');};h.$('add-device').click();chooseDeviceProtocol(h,'modbus','TCP版');h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));for(let i=0;i<5;i++)await tick();assert.equal(h.$('device-dialog').open,true);assert.equal(h.calls.some(c=>c[0]==='CloseNativeWindow'),false);}finally{await h.close();}
 });
 test('waiting connection remains stoppable and communication settings stay editable',async()=>{
 const h=await setup({states:[{id:'a',status:'connecting',lastError:'connection refused'}]});try{assert.ok(h.$('toggle-channel').textContent.includes('停止'));h.$('edit-channel').click();assert.equal(originalField(h,'txtClientIP','original-network-panel').disabled,false);h.$('channel-dialog').close();h.$('toggle-channel').click();await tick();await tick();assert.ok(h.calls.some(c=>c[0]==='StopChannel'));assert.equal(h.calls.some(c=>c[0]==='StartChannel'),false);}finally{await h.close();}
 });

 test('startup enters the client workspace directly and switching modes has no launcher',async()=>{
 for(const context of [null,{kind:''}]){const h=await setup({},context);try{
 assert.equal(h.$('launch-screen'),null);assert.equal(h.$('main-menu'),null);
 assert.ok(h.calls.some(c=>c[0]==='SetWorkspaceMode'&&c[1][0]==='client'));
 assert.equal(h.w.document.querySelector('.mode-switch [data-mode="client"]').getAttribute('aria-selected'),'true');
 assert.ok(h.w.document.querySelector('[data-select="a"].selected'));
 h.w.document.querySelector('.mode-switch [data-mode="server"]').click();await tick();await tick();
 assert.ok(h.calls.some(c=>c[0]==='SetWorkspaceMode'&&c[1][0]==='server'));
 assert.equal(h.w.document.querySelector('.mode-switch [data-mode="server"]').getAttribute('aria-selected'),'true');
 assert.equal(h.calls.some(c=>c[0]==='SetWorkspaceMode'&&c[1][0]===''),false);
 assert.equal(h.calls.some(c=>c[0]==='StartChannel'),false);
 }finally{await h.close();}}
 });

async function setupInline(){const c=dataServer();c.devices.push({...structuredClone(c.devices[0]),id:'other',name:'other device'});const h=await setup({project:{name:'inline',channels:[c]},states:[{id:c.id,status:'connected',peers:1}]});h.w.document.querySelector('.mode-switch [data-mode="server"]').click();await tick();await tick();h.w.document.querySelector('[data-point-kind="yc"]').click();h.overrides.SetPointValue=(channel,device,key,value)=>{h.state.project.channels.find(c=>c.id===channel).devices.find(d=>d.id===device).points.find(p=>p.kind===key.kind&&p.address===key.address).value=value;return {mode:'reported',receivers:1};};return h;}
function editInline(h,input,value){input.value=value;input.dispatchEvent(new h.w.Event('input',{bubbles:true}));}
function inlineKey(h,input,key){input.dispatchEvent(new h.w.KeyboardEvent('keydown',{key,bubbles:true,cancelable:true}));}
async function settleInline(){for(let i=0;i<6;i++)await tick();}

test('inline analog Enter and blur submit once while polling preserves the pending input',async()=>{
 const h=await setupInline();try{const input=h.w.document.querySelector('[data-point-key="yc:10"] .point-value-input');assert.ok(input);input.focus();editInline(h,input,'12.5');let finish;h.overrides.SetPointValue=(channel,device,key,value)=>new Promise(resolve=>{finish=()=>{h.state.project.channels[0].devices[0].points[0].value=value;resolve({mode:'reported',receivers:1});};});inlineKey(h,input,'Enter');input.blur();inlineKey(h,input,'Enter');h.state.project.channels[0].devices[0].points[1].value=55;await h.refresh();assert.equal(h.w.document.querySelector('[data-point-key="yc:10"] .point-value-input'),input);assert.equal(input.value,'12.5');assert.equal(input.readOnly,true);assert.equal(h.calls.filter(c=>c[0]==='SetPointValue').length,1);finish();await settleInline();assert.equal(input.readOnly,false);assert.equal(input.value,'12.5');input.dispatchEvent(new h.w.Event('blur'));await settleInline();assert.equal(h.calls.filter(c=>c[0]==='SetPointValue').length,1);assert.deepEqual(JSON.parse(JSON.stringify(h.calls.find(c=>c[0]==='SetPointValue')[1])),['data','target',{kind:'yc',address:10},12.5]);assert.equal(h.calls.some(c=>c[0]==='PublishDevice'),false);}finally{await h.close();}
});
test('inline drafts survive polling and commit to their captured device on selection change',async()=>{
 const h=await setupInline();try{const input=h.w.document.querySelector('.point-value-input');input.focus();editInline(h,input,'23.75');h.state.project.channels[0].devices[0].points[0].value=99;await h.refresh();assert.equal(input.value,'23.75');h.w.document.querySelector('[data-device="other"]').click();await settleInline();const call=h.calls.find(c=>c[0]==='SetPointValue');assert.equal(call[1][1],'target');assert.equal(call[1][3],23.75);assert.equal(h.state.project.channels[0].devices[1].points[0].value,0);assert.equal(h.$('selected-device-name').textContent,'other device');}finally{await h.close();}
});
test('inline values validate before submitting, Escape cancels, and errors preserve the draft',async()=>{
 const h=await setupInline();try{const input=h.w.document.querySelector('.point-value-input');for(const value of ['', 'abc','Infinity','1e50']){editInline(h,input,value);inlineKey(h,input,'Enter');await settleInline();assert.equal(input.getAttribute('aria-invalid'),'true');}assert.equal(h.calls.some(c=>c[0]==='SetPointValue'),false);inlineKey(h,input,'Escape');assert.equal(input.value,'0');assert.equal(input.hasAttribute('aria-invalid'),false);editInline(h,input,'20');h.overrides.SetPointValue=()=>{throw Error('connection failed');};inlineKey(h,input,'Enter');await settleInline();assert.equal(input.value,'20');assert.equal(input.getAttribute('aria-invalid'),'true');h.w.document.querySelector('[data-point-kind="yx"]').click();await settleInline();const bit=h.w.document.querySelector('.point-value-input');editInline(h,bit,'2');inlineKey(h,bit,'Enter');await settleInline();assert.equal(bit.getAttribute('aria-invalid'),'true');}finally{await h.close();}
});
test('inline blur saves offline with an explicit unsent status and operation buttons stay removed',async()=>{
 const h=await setupInline();try{h.overrides.SetPointValue=(channel,device,key,value)=>{h.state.project.channels[0].devices[0].points[0].value=value;return {mode:'offline',receivers:0};};const input=h.w.document.querySelector('.point-value-input');input.focus();editInline(h,input,'45');input.blur();await settleInline();assert.equal(h.calls.filter(c=>c[0]==='SetPointValue').length,1);assert.ok(h.$('toast').textContent.includes('未上送'));}finally{await h.close();}
 const client=await setup();try{assert.ok(client.w.document.querySelector('#point-rows'));assert.equal(client.w.document.querySelectorAll('[data-edit-point]').length,0);}finally{await client.close();}
});

function openPointDetails(h){h.$('point-rows').firstElementChild.dispatchEvent(new h.w.MouseEvent('contextmenu',{bubbles:true,clientX:10,clientY:10}));[...h.$('context-menu').children].find(b=>b.textContent==='详细配置').click();}

test('navigation separators resize by keyboard and pointer, restore half height and persist preferences',async()=>{
 const h=await setup();try{const width=h.$('sidebar-resizer'),height=h.$('navigation-resizer');assert.ok(width&&height);assert.equal(height.getAttribute('aria-valuenow'),'50');width.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));assert.equal(width.getAttribute('aria-valuenow'),'192');height.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true}));assert.equal(height.getAttribute('aria-valuenow'),'55');assert.equal(h.w.localStorage.getItem('gylab-navigation-share'),'0.55');height.dispatchEvent(new h.w.MouseEvent('dblclick',{bubbles:true}));assert.equal(height.getAttribute('aria-valuenow'),'50');width.setPointerCapture=()=>{};width.dispatchEvent(new h.w.PointerEvent('pointerdown',{button:0,pointerId:1,clientX:200,bubbles:true}));width.dispatchEvent(new h.w.PointerEvent('pointermove',{pointerId:1,clientX:240,bubbles:true}));assert.equal(width.getAttribute('aria-valuenow'),'232');width.dispatchEvent(new h.w.PointerEvent('pointercancel',{pointerId:1,bubbles:true}));assert.equal(width.classList.contains('dragging'),false);width.dispatchEvent(new h.w.MouseEvent('dblclick',{bubbles:true}));assert.equal(width.getAttribute('aria-valuenow'),'180');}finally{await h.close();}
});
test('point grid contains five editable columns without row operation buttons and client commits locally',async()=>{
 const h=await setup({project:{name:'client',channels:[channel('a',[{kind:'holding',address:1,value:2,name:'local',quality:0}])]}});try{assert.equal(h.w.document.querySelectorAll('.point-table th').length,5);assert.equal(h.$('point-rows').querySelectorAll('button').length,0);assert.equal(h.$('point-rows').querySelectorAll('.point-cell-input').length,5);h.overrides.EditPointCell=(channel,device,key,field,patch)=>{Object.assign(h.state.project.channels[0].devices[0].points[0],patch);return {mode:'stored'};};const value=h.w.document.querySelector('[data-point-field="value"]');editInline(h,value,'123');inlineKey(h,value,'Enter');await settleInline();const call=h.calls.find(c=>c[0]==='EditPointCell');assert.equal(call[1][3],'value');assert.equal(call[1][4].value,123);assert.equal(h.calls.some(c=>['SetPointValue','PublishDevice','WriteDeviceModbus','Command104Device'].includes(c[0])),false);}finally{await h.close();}
});
test('hexadecimal telemetry edits use IEEE float values and invalid quality stays uncommitted',async()=>{
 const h=await setupInline();try{const hex=h.w.document.querySelector('[data-point-field="hex"]');editInline(h,hex,'41480000');inlineKey(h,hex,'Enter');await settleInline();assert.equal(h.calls.find(c=>c[0]==='SetPointValue')[1][3],12.5);const quality=h.w.document.querySelector('[data-point-field="quality"]');editInline(h,quality,'256');inlineKey(h,quality,'Enter');await settleInline();assert.equal(quality.getAttribute('aria-invalid'),'true');assert.equal(h.calls.some(c=>c[0]==='EditPointCell'),false);}finally{await h.close();}
});
test('address changes serialize following cell edits to the new key without replacing the focused editor',async()=>{
 const h=await setupInline();try{let finish;h.overrides.EditPointCell=(channel,device,key,field,patch)=>new Promise(resolve=>{finish=()=>{Object.assign(h.state.project.channels[0].devices[0].points[0],patch);resolve({mode:'stored'});};});const address=h.w.document.querySelector('[data-point-field="address"]'),name=h.w.document.querySelector('[data-point-field="name"]');editInline(h,address,'500');inlineKey(h,address,'Enter');await tick();name.focus();editInline(h,name,'renamed');inlineKey(h,name,'Enter');await tick();assert.equal(h.calls.filter(c=>c[0]==='EditPointCell').length,1);finish();await settleInline();assert.equal(h.calls.filter(c=>c[0]==='EditPointCell').length,2);assert.equal(h.calls.filter(c=>c[0]==='EditPointCell')[1][1][2].address,500);assert.equal(h.w.document.querySelector('[data-point-key="yc:500"] [data-point-field="name"]'),name);finish();await settleInline();assert.equal(name.value,'renamed');}finally{await h.close();}
});
test('a configured point keeps its tab when edited beyond the original address range',async()=>{
 const c=dataServer();c.devices[0].points[0].address=1000;c.devices[0].points[0].tab='yc';c.devices[0].configuration={ranges:[{tab:'yc',kind:'yc',address:10,count:2},{tab:'yx',kind:'yx',address:1,count:1}]};const h=await setup({project:{name:'custom',channels:[c]}});try{h.w.document.querySelector('.mode-switch [data-mode="server"]').click();await settleInline();h.w.document.querySelector('[data-point-kind="tab:yc"]').click();assert.ok(h.w.document.querySelector('[data-point-key="yc:1000"]'));}finally{await h.close();}
});

test('native popup initialization does not overwrite main-window navigation preferences',async()=>{
 const h=await setup({}, {kind:'manager-dialog',role:'client',channel:'a'});try{assert.equal(h.w.localStorage.getItem('gylab-navigation-width'),null);assert.equal(h.w.localStorage.getItem('gylab-navigation-share'),null);h.w.dispatchEvent(new h.w.Event('resize'));assert.equal(h.w.localStorage.getItem('gylab-navigation-width'),null);}finally{await h.close();}
});

test('all numeric inputs hide spinners and ignore increment shortcuts',async()=>{
 const css=await readFile(new URL('../frontend/style.css',import.meta.url),'utf8');
 assert.match(css,/input\[type=number\]::-webkit-inner-spin-button/);
 assert.match(css,/-webkit-appearance:none/);
 const h=await setup();try{
  h.$('edit-channel').click();
  const port=originalField(h,'txtClientPort','original-network-panel');port.value='2425';port.focus();
  for(const key of ['ArrowUp','ArrowDown']){
   const event=new h.w.KeyboardEvent('keydown',{key,bubbles:true,cancelable:true});port.dispatchEvent(event);
   assert.equal(event.defaultPrevented,true);assert.equal(port.value,'2425');
  }
  const wheel=new h.w.WheelEvent('wheel',{deltaY:-1,bubbles:true,cancelable:true});port.dispatchEvent(wheel);assert.equal(wheel.defaultPrevented,true);
  assert.equal(port.type,'number');assert.equal(port.max,'65535');
 }finally{await h.close();}
});

async function setupManager(){
 const h=await setup({project:{name:'two',channels:[channel('a'),{...channel('b'),address:'127.0.0.1:2425'}]}},{kind:'manager-dialog',role:'client',channel:'a'});
 h.overrides.UpdateTransport=config=>{const c=h.state.project.channels.find(c=>c.id===config.id);Object.assign(c,config);h.state.states=h.state.states.filter(s=>s.id!==c.id);h.state.states.push({id:c.id,status:'stopped'});};
 h.overrides.RenameChannel=(id,name)=>{h.state.project.channels.find(c=>c.id===id).name=name;};
 return h;
}
const managerInput=(h,id,key)=>h.$('manager-rows').querySelector('[data-manager-id="'+id+'"] [data-manager-field="'+key+'"]');
test('communication manager edits the clicked row directly without editor buttons or window calls',async()=>{
 const h=await setupManager();try{
  assert.equal(h.$('manager-rows').querySelector('[data-manager-edit]'),null);
  assert.equal([...h.$('manager-rows').querySelectorAll('button')].some(b=>b.textContent.includes('编辑')),false);
  const address=managerInput(h,'b','address');editInline(h,address,'127.0.0.1:2500');inlineKey(h,address,'Enter');address.dispatchEvent(new h.w.Event('blur'));await settleInline();
  const edits=h.calls.filter(c=>c[0]==='UpdateTransport');assert.equal(edits.length,1);assert.equal(edits[0][1][0].id,'b');assert.equal(edits[0][1][0].address,'127.0.0.1:2500');
  assert.equal(h.state.project.channels[0].address,'127.0.0.1:502');
  assert.equal(h.calls.some(c=>['OpenNativeWindow','StartChannel'].includes(c[0])),false);
 }finally{await h.close();}
});
test('manager drafts survive polling, Escape cancels and failures preserve input',async()=>{
 const h=await setupManager();try{
  const address=managerInput(h,'b','address');address.focus();editInline(h,address,'bad endpoint');await h.refresh();
  assert.equal(managerInput(h,'b','address'),address);assert.equal(address.value,'bad endpoint');
  inlineKey(h,address,'Escape');assert.equal(address.value,'127.0.0.1:2425');address.blur();await settleInline();
  assert.equal(h.calls.some(c=>c[0]==='UpdateTransport'),false);
  h.overrides.UpdateTransport=()=>{throw Error('地址应为 host:port');};
  editInline(h,address,'invalid');inlineKey(h,address,'Enter');await settleInline();
  assert.equal(address.value,'invalid');assert.equal(address.getAttribute('aria-invalid'),'true');
  assert.equal(h.state.project.channels[1].address,'127.0.0.1:2425');
 }finally{await h.close();}
});
test('queued manager edits merge the latest endpoint, binding and point data',async()=>{
 const h=await setupManager();try{
  let finish;const save=h.overrides.UpdateTransport;let count=0;
  h.overrides.UpdateTransport=config=>{if(++count===1)return new Promise(resolve=>{finish=()=>{save(config);resolve();};});save(config);};
  const address=managerInput(h,'b','address'),local=managerInput(h,'b','localAddress');
  editInline(h,address,'127.0.0.1:2501');inlineKey(h,address,'Enter');await tick();await tick();
  editInline(h,local,'127.0.0.1:0');inlineKey(h,local,'Enter');await tick();
  assert.equal(h.calls.filter(c=>c[0]==='UpdateTransport').length,1);
  finish();await settleInline();
  const edits=h.calls.filter(c=>c[0]==='UpdateTransport');assert.equal(edits.length,2);assert.equal(edits[1][1][0].address,'127.0.0.1:2501');assert.equal(edits[1][1][0].localAddress,'127.0.0.1:0');
  const name=managerInput(h,'b','name');editInline(h,name,'renamed');inlineKey(h,name,'Enter');await settleInline();
  assert.equal(h.state.project.channels[1].name,'renamed');assert.equal(h.calls.some(c=>c[0]==='OpenNativeWindow'),false);
 }finally{await h.close();}
});
test('manager exposes serial settings inline and commits selection without duplicate blur',async()=>{
 const h=await setupManager();try{
  h.state.project.channels[1].protocol='raw';h.state.project.channels[1].transport='serial';h.state.project.channels[1].address='/dev/cu.test';await h.refresh();
  const baud=managerInput(h,'b','baud');assert.equal(baud.closest('label').hidden,false);editInline(h,baud,'19200');baud.dispatchEvent(new h.w.Event('blur'));await settleInline();
  assert.equal(h.state.project.channels[1].baud,19200);assert.equal(h.state.project.channels[1].transportDraft.cbBaudRate,'19200');
  const parity=managerInput(h,'b','parity');parity.value='E';parity.dispatchEvent(new h.w.Event('change',{bubbles:true}));parity.dispatchEvent(new h.w.Event('blur'));await settleInline();
  assert.equal(h.state.project.channels[1].parity,'E');assert.equal(h.state.project.channels[1].transportDraft.cbParity,'偶校验');assert.equal(h.calls.filter(c=>c[0]==='UpdateTransport').length,2);
 }finally{await h.close();}
});
test('connected communication editor applies and closes without a start request',async()=>{
 const h=await setup();try{
  h.$('edit-channel').click();assert.equal(h.$('original-network-panel').querySelector('button[type=submit]').textContent,'应用');
  originalField(h,'txtClientPort','original-network-panel').value='2425';
  h.$('channel-form').requestSubmit(h.$('original-network-panel').querySelector('button[type=submit]'));await tick();await tick();
  assert.equal(h.$('channel-dialog').open,false);assert.equal(h.calls.filter(c=>c[0]==='UpdateTransport').length,1);
  assert.equal(h.calls.some(c=>c[0]==='StartChannel'),false);
 }finally{await h.close();}
});

test('communication blur and polling preserve the apply button during the first click',async()=>{
 const h=await setup();try{
  h.$('edit-channel').click();const panel=h.$('original-network-panel'),button=panel.querySelector('button[type=submit]'),label=button.firstChild;
  const summary=h.$('network-summary').firstChild,port=originalField(h,'txtClientPort','original-network-panel');
  port.focus();port.value='25048';port.dispatchEvent(new h.w.Event('input',{bubbles:true}));
  button.dispatchEvent(new h.w.MouseEvent('mousedown',{bubbles:true}));
  port.dispatchEvent(new h.w.Event('change',{bubbles:true}));port.blur();
  assert.equal(button.firstChild,label);assert.equal(h.$('network-summary').firstChild,summary);
  await h.refresh();assert.equal(button.firstChild,label);assert.equal(h.$('network-summary').firstChild,summary);assert.equal(port.value,'25048');
  button.click();await tick();await tick();
  const edits=h.calls.filter(c=>c[0]==='UpdateTransport');assert.equal(edits.length,1);assert.equal(edits[0][1][0].address,'127.0.0.1:25048');assert.equal(h.$('channel-dialog').open,false);
 }finally{await h.close();}
});

test('sidebar plus buttons replace ribbon additions and custom channel names are saved once',async()=>{
 const h=await setup({project:{name:'names',channels:[{...channel('a'),name:'通道0'},{...channel('b'),name:'通道1'}]}});try{
  assert.equal(h.$('add-channel').closest('.channel-dock')!==null,true);
  assert.equal(h.$('add-device').closest('.channel-device-dock')!==null,true);
  assert.equal(h.w.document.querySelector('.replica-ribbon #add-channel'),null);assert.equal(h.w.document.querySelector('.replica-ribbon #add-device'),null);
  assert.equal([...h.w.document.querySelectorAll('.ribbon-group-name')].some(el=>el.textContent==='通道配置'),false);
  h.$('add-channel').click();await tick();assert.equal(h.$('confirm-input').value,'通道2');
  assert.equal(h.calls.some(c=>c[0]==='UpsertChannel'),false);h.$('confirm-input').value='测试通道';
  h.$('confirm-actions').querySelector('button[value=ok]').click();await settleInline();
  assert.equal(h.calls.filter(c=>c[0]==='UpsertChannel').length,1);assert.equal(h.calls.find(c=>c[0]==='UpsertChannel')[1][0].name,'测试通道');
 }finally{await h.close();}
});
test('new device has a visible unused default name and saves the typed name',async()=>{
 const c={...channel('a'),protocol:'raw',devices:[{id:'first',name:'装置1',points:[],unit:1,commonAddress:1}]};
 const h=await setup({project:{name:'names',channels:[c]},states:[]});try{
  h.$('add-device').click();const name=h.$('device-form').elements.namedItem('name');assert.equal(name.type,'text');assert.equal(name.value,'装置2');assert.equal(name.readOnly,false);
  name.value='自定义装置';h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();
  h.$('confirm-actions').querySelector('button[value=ok]').click();await settleInline();
  assert.equal(h.calls.find(c=>c[0]==='UpsertDevice')[1][2].name,'自定义装置');
 }finally{await h.close();}
});
test('cancelled channel-name window creates nothing',async()=>{
 const h=await setup();try{
  h.$('add-channel').click();await tick();h.$('confirm-actions').querySelector('button[value=cancel]').click();await settleInline();assert.equal(h.calls.some(c=>c[0]==='UpsertChannel'),false);
 }finally{await h.close();}
});

test('new project opens another workspace without replacing or stopping this project',async()=>{
 for(const role of ['client','server']){
  const h=await setup({project:{name:'original',channels:[{...channel('old'),role,transport:role==='server'?'tcp-server':'tcp-client'}]},states:[{id:'old',status:'connected',peers:1}],dirty:true,path:'/tmp/original.json'},{kind:''});
  try{
   if(role==='server'){h.w.document.querySelector('[data-mode=server]').click();await settleInline();}
   h.overrides.AskNative=()=>({choice:'ok',value:'工程2'});
   h.$('new-project').click();await settleInline();
   const call=h.calls.find(c=>c[0]==='NewProject');assert.deepEqual([...call[1]],['工程2',role]);
   assert.equal(h.calls.filter(c=>c[0]==='AskNative').length,1);
   assert.equal(h.state.project.name,'original');assert.equal(h.state.path,'/tmp/original.json');assert.equal(h.state.states[0].status,'connected');
   assert.ok(h.w.document.querySelector('[data-select=old]'));
   assert.equal(h.calls.some(c=>['StartChannel','StopAll','StopChannel','SaveProject'].includes(c[0])),false);
  }finally{await h.close();}
 }
});
test('new project defaults advance from project one and cancelled dialogs do not consume numbers',async()=>{
 const h=await setup({project:{name:'工程1',channels:[]},states:[],dirty:false},{kind:''});
 try{
  const prompts=[];let answer='cancel';
  h.overrides.AskNative=options=>{prompts.push(options.input);return {choice:answer,value:options.input};};
  h.overrides.NewProject=()=>null;
  h.$('new-project').click();await settleInline();assert.equal(prompts.at(-1),'工程2');assert.equal(h.state.project.name,'工程1');
  answer='ok';h.$('new-project').click();await settleInline();assert.equal(prompts.at(-1),'工程2');assert.equal(h.state.project.name,'工程1');
  h.$('new-project').click();await settleInline();assert.equal(prompts.at(-1),'工程3');assert.equal(h.state.project.name,'工程1');
  h.state.project.name='工程8';await h.refresh();h.$('new-project').click();await settleInline();assert.equal(prompts.at(-1),'工程9');
 }finally{await h.close();}
});

test('project title marks unsaved paths and edits, then clears the marker only after a successful save',async()=>{
 const h=await setup({project:{name:'工程1',channels:[channel('a')]},states:[],path:'',dirty:false});try{
  assert.equal(h.$('project-name').textContent,'工程1（未保存）');
  h.overrides.SaveProject=()=>{h.state.path='/tmp/现场.json';h.state.project.name='现场';h.state.dirty=false;return h.state.path;};
  h.$('save-project').click();await settleInline();assert.equal(h.$('project-name').textContent,'现场');
  h.state.states=[{id:'a',status:'connected',peers:1}];h.state.messages.push({sequence:1,channelId:'a',direction:'RX',summary:'received',hex:'01'});await h.refresh();
  assert.equal(h.$('project-name').textContent,'现场');
  h.state.dirty=true;await h.refresh();assert.equal(h.$('project-name').textContent,'现场（未保存）');
  h.overrides.SaveProject=()=>'';h.$('save-project').click();await settleInline();assert.equal(h.$('project-name').textContent,'现场（未保存）');
  h.overrides.SaveProject=()=>{throw Error('save failed');};h.$('save-project').click();await settleInline();assert.equal(h.$('project-name').textContent,'现场（未保存）');
 }finally{await h.close();}
});

test('debug panel resizes from an external separator and remembers keyboard or pointer changes',async()=>{
 const h=await setup();try{
  const handle=h.$('debug-resizer'),dock=h.$('report-dock');
  assert.equal(handle.parentElement,dock);assert.equal(handle.closest('.debug-dock'),null);
  assert.equal(handle.getAttribute('aria-valuenow'),'130');
  handle.setPointerCapture=()=>{};
  handle.dispatchEvent(new h.w.PointerEvent('pointerdown',{button:0,pointerId:1,clientY:400,bubbles:true}));
  handle.dispatchEvent(new h.w.PointerEvent('pointermove',{pointerId:1,clientY:350,bubbles:true}));
  assert.equal(handle.getAttribute('aria-valuenow'),'180');assert.equal(dock.style.getPropertyValue('--debug-height'),'180px');
  handle.dispatchEvent(new h.w.PointerEvent('pointercancel',{pointerId:1,bubbles:true}));
  assert.equal(handle.classList.contains('dragging'),false);
  handle.dispatchEvent(new h.w.PointerEvent('pointermove',{pointerId:1,clientY:300,bubbles:true}));
  assert.equal(handle.getAttribute('aria-valuenow'),'180');
  handle.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true}));
  assert.equal(handle.getAttribute('aria-valuenow'),'168');assert.equal(h.w.localStorage.getItem('gylab-debug-height'),'168');
  handle.dispatchEvent(new h.w.MouseEvent('dblclick',{bubbles:true}));assert.equal(handle.getAttribute('aria-valuenow'),'130');
  handle.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'ArrowUp',bubbles:true}));assert.equal(handle.getAttribute('aria-valuenow'),'142');
  handle.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Home',bubbles:true}));assert.equal(handle.getAttribute('aria-valuenow'),'130');
 }finally{await h.close();}
 const popup=await setup({}, {kind:'manager-dialog',role:'client',channel:'a'});try{
  assert.equal(popup.w.localStorage.getItem('gylab-debug-height'),null);
  popup.w.dispatchEvent(new popup.w.Event('resize'));
  assert.equal(popup.w.localStorage.getItem('gylab-debug-height'),null);
 }finally{await popup.close();}
});
test('point badge reports current type count rather than presenting a quota',async()=>{
 const points=['yx','yc','yk'].flatMap((kind,k)=>Array.from({length:50},(_,i)=>({kind,address:k*1000+i+1,value:0,quality:0,name:''})));
 const h=await setup({project:{name:'counts',channels:[iecChannel('a',points)]}});try{
  assert.equal(h.$('point-badge').textContent,'50 点');assert.match(h.$('point-badge').title,/装置共 150 点/);
  h.state.project.channels[0].points.push({kind:'yx',address:999,value:0,quality:0,name:''});await h.refresh();
  assert.equal(h.$('point-badge').textContent,'51 点');assert.match(h.$('point-badge').title,/装置共 151 点/);
  h.$('point-search').value='no match';h.$('point-search').dispatchEvent(new h.w.Event('input',{bubbles:true}));
  assert.equal(h.$('point-badge').textContent,'0 点');
 }finally{await h.close();}
});

test('editing an existing device saves real parameters once without connecting and preserves drafts during polling',async()=>{
 const c=iecChannel('a');c.devices=[{id:'dev',name:'existing',unit:1,commonAddress:1,points:[{kind:'yx',address:1,value:1,quality:0}],configuration:{version:'标准版',values:{txtAdd:'1',txtYX_Add:'1',txtYX_Num:'1',txtYC_Add:'100',txtYC_Num:'0',txtMC_Add:'200',txtMC_Num:'0',txtYK_Add:'300',txtYK_Num:'0',txtTotal_Time:'60'},ranges:[{tab:'yx',kind:'yx',address:1,count:1}]}}];
 const h=await setup({project:{name:'edit',channels:[c]}});try{
  h.$('device-config').click();const field=originalField(h,'txtTotal_Time');assert.equal(field.disabled,false);field.value='15';await h.refresh();assert.equal(field.value,'15');
  h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));await tick();await tick();const calls=h.calls.filter(c=>c[0]==='UpsertDevice');assert.equal(calls.length,1);assert.equal(calls[0][1][2].id,'dev');assert.equal(calls[0][1][2].configuration.values.txtTotal_Time,'15');assert.equal(h.calls.some(c=>c[0]==='StartChannel'),false);assert.equal(h.$('device-dialog').open,false);
 }finally{await h.close();}
});
test('SOE and COS show device timestamps and invalid quality then clear only the selected category',async()=>{
 const c=iecChannel();const h=await setup({project:{name:'events',channels:[c]},events:[{channelId:'a',category:'SOE',timestamp:'2026-10-22T12:00:00.123+08:00',received:'later',address:1,kind:'yx',value:1,quality:128,timeInvalid:true},{channelId:'a',category:'COS',received:'2026-10-22T13:00:00Z',address:2,kind:'yx',value:0,quality:0}]});try{
  h.$('show-events').click();assert.match(h.$('event-rows').textContent,/12:00:00.123/);assert.match(h.$('event-rows').textContent,/无效/);h.$('event-category').value='SOE';h.$('event-category').dispatchEvent(new h.w.Event('change'));assert.equal(h.$('event-rows').children.length,1);h.$('clear-events').click();await tick();assert.deepEqual([...h.calls.find(c=>c[0]==='ClearEvents')[1]],['a','SOE']);
 }finally{await h.close();}
});
test('typed registers edit signed and floating values with wire-order hexadecimal roundtrips',async()=>{
 const c=channel('a',[{kind:'holding',address:0,value:-123,dataType:'int16',byteOrder:'BA',quality:0},{kind:'holding',address:2,value:12.5,dataType:'float32',byteOrder:'CDAB',quality:0}]);const h=await setup({project:{name:'typed',channels:[c]}});try{
  const rows=h.$('point-rows').children;assert.equal(rows[0].children[1].querySelector('input').value,'85FF');assert.equal(rows[1].children[1].querySelector('input').value,'00004148');const input=rows[0].children[2].querySelector('input');input.value='-32768';input.dispatchEvent(new h.w.Event('input'));input.dispatchEvent(new h.w.KeyboardEvent('keydown',{key:'Enter',bubbles:true}));await settleInline();assert.equal(h.calls.find(c=>c[0]==='EditPointCell')[1][4].value,-32768);
 }finally{await h.close();}
});
test('timed remote controls keep the existing selection workflow and send type 58',async()=>{
 const h=await setup({project:{name:'timed',channels:[iecChannel()]}});try{h.$('open-yk').click();h.$('control-timestamp').checked=true;h.$('control-select').click();await tick();assert.equal(h.calls.find(c=>c[0]==='Command104Device')[1][2].type,58);}finally{await h.close();}
});
test('legacy configuration edits keep absent pulse counts at zero and typed point details accept signed values',async()=>{
 const c=iecChannel('a',[{kind:'yx',address:1,value:1,quality:0}]);c.devices=[{id:'d',name:'legacy',commonAddress:1,unit:1,points:c.points,configuration:{version:'标准版',values:{txtYX_Add:'1',txtYX_Num:'1',txtYC_Add:'100',txtYC_Num:'0',txtYK_Add:'200',txtYK_Num:'0'},ranges:[{tab:'yx',kind:'yx',address:1,count:1}]}}];const h=await setup({project:{name:'old',channels:[c]}});try{h.$('device-config').click();assert.equal(originalField(h,'txtMC_Num').value,'0');}finally{await h.close();}
 const typed=await setup({project:{name:'typed',channels:[channel('a',[{kind:'holding',address:1,value:-12.5,dataType:'float32',quality:0}])]}});try{openPointDetails(typed);const value=typed.$('point-form').elements.namedItem('value');assert.equal(value.step,'any');assert.ok(Number(value.min)<-12.5);assert.equal(typed.$('point-form').checkValidity(),true);}finally{await typed.close();}
});
test('32-bit configured ranges read complete register pairs',async()=>{const c=channel('a');c.devices=[{id:'d',name:'float',unit:1,commonAddress:1,points:[{kind:'holding',tab:'yc',address:100,value:12.5,dataType:'float32',quality:0}],configuration:{version:'TCP版',values:{},ranges:[{tab:'yc',kind:'holding',address:100,count:80}]}}];const h=await setup({project:{name:'wide',channels:[c]}});try{assert.equal(h.$('read-start').value,'100');assert.equal(h.$('read-count').value,'124');}finally{await h.close();}});
