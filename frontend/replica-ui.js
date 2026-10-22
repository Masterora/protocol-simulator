// Captions and defaults follow the original designer. Only implemented controls
// are rendered; grid flow replaces fragile absolute designer coordinates.
globalThis.renderOriginalForm=function(host,key,enabled,values={},edit=false){
 const spec=globalThis.originalForms[key],escape=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 if(!spec){host.replaceChildren();return;}
 const controls=Object.values(spec.controls),order=(a,b)=>a.location[1]-b.location[1]||a.location[0]-b.location[0];
 // Designer rows can differ by one pixel. Keep each address/count pair on
 // one row instead of sorting the two controls independently by that pixel.
 const rangeID=/^txt(YX|YC|MC|YK|JDQ)_(Add|Num)$/;
 const fieldOrder=(a,b)=>{
  const ar=rangeID.exec(a.id),br=rangeID.exec(b.id);
  if(ar&&br){
   const row=c=>Math.min(...controls.filter(v=>v.parent===c.parent&&rangeID.exec(v.id)?.[1]===rangeID.exec(c.id)?.[1]).map(v=>v.location[1]));
   return row(a)-row(b)||(ar[2]==='Add'?0:1)-(br[2]==='Add'?0:1);
  }
  return order(a,b);
 };
 const render=c=>{
  const id=c.id,v=values[id]??c.editvalue??c.options?.[c.defaultindex||0]??'',data=`data-original-id="${id}"`,name=`name="original-${id}"`,disabled='';
  const prefixes={YX:'遥信',YC:'遥测',MC:'脉冲',YK:'遥控',JDQ:'继电器'};const range=/^txt(YX|YC|MC|YK|JDQ)_(Add|Num)$/.exec(id);
  const networkLabels={cb_ClientAssignIp:'本地IP',txt_ClientAssignPort:'本地端口',txtClientPort:'远方端口',txtServerPort:'监听端口',cbxServerIP:'监听IP'};
  const label=range?prefixes[range[1]]+(range[2]==='Add'?'首地址':'个数'):networkLabels[id]||c.label||c.text||id;
  if(c.type==='CheckEdit'||c.type==='RadioButton'){
   const radio=c.type==='RadioButton',group=id==='rbClient'||id==='rbServer'?'network-role':'network-type';
   return `<label class="original-check"><input type="${radio?'radio':'checkbox'}" ${data} ${radio?`name="${group}"`:name} ${v===true||v==='true'?'checked':''} ${disabled}>${escape(c.text)}</label>`;
  }
  let input;
  if(key==='network'&&['cbComList','cbxServerIP','cb_ClientAssignIp'].includes(id))input=`<input type="text" ${data} ${name} value="${escape(v)}" list="choices-${id}" ${disabled} autocomplete="off"><datalist id="choices-${id}"></datalist>`;
  else if(c.options?.length){let options=c.options.filter(o=>!['1.5','Mark','Space'].includes(o));if(v!==''&&!options.includes(String(v)))options=[String(v),...options];input=`<select ${data} ${name} ${disabled}>${options.map(o=>`<option ${String(o)===String(v)?'selected':''}>${escape(o)}</option>`).join('')}</select>`;}
  else input=`<input type="text" ${data} ${name} value="${escape(v)}" ${disabled} autocomplete="off">`;
  return `<label class="original-field" data-original-field="${id}"><span>${escape(label)}</span>${input}</label>`;
 };
 const groups=controls.filter(c=>c.type==='GroupControl').sort(order);
 host.style.width='';host.style.height='';host.dataset.form=key;
 host.innerHTML=groups.map(g=>{
  const fields=controls.filter(c=>c.parent===g.id&&enabled.has(c.id)).sort(fieldOrder);
  return fields.length?`<section class="original-group" data-group-id="${g.id}"><div class="original-group-title">${escape(g.text)}</div>${fields.map(render).join('')}</section>`:'';
 }).join('')+`<div class="original-submit"><button type="submit" data-original-id="simpleButton1">确认</button></div>`;
};
globalThis.readOriginalForm=function(host){return Object.fromEntries([...host.querySelectorAll('[data-original-id]')].filter(el=>el.tagName!=='BUTTON').map(el=>[el.dataset.originalId,el.type==='checkbox'||el.type==='radio'?el.checked:el.value]));};
