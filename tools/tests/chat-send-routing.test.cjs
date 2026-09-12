const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const src=fs.readFileSync('server/web/js/48-chat.js','utf8'),state=fs.readFileSync('server/web/js/47-chat-state.js','utf8');
const send=src.slice(src.indexOf('async function chatTermSend('),src.indexOf('// ---- landing:',src.indexOf('async function chatTermSend(')));
test('blocked and idle follow-ups deliver directly; only working stages',async()=>{
 for(const status of ['blocked','idle','working']){
  let staged=0,sent=0;
  const c={chatTermSending:false,chatOpenId:'abc',chatAgent:'codex',chatRouteVersion:1,chatPendingProject:null,chatTermOpen:null,chatTermEcho:()=>{},chatTermFind:()=>({agentState:status,backend:'herdr'}),chatStageMessage:async()=>staged++,chatTermBase:()=>'/input',renderChatComposer:()=>{},chatTermComposerSession:()=>({}),chatRememberDelivery:(...x)=>x,chatDeliverRemembered:async()=>{sent++;return{}},loadChatTermSessions:async()=>{},showToast:()=>{}};
  vm.createContext(c);vm.runInContext(send,c);assert.equal(await c.chatTermSend('follow-up'),true);assert.equal(staged,status==='working'?1:0);assert.equal(sent,status==='working'?0:1);
 }
});
test('pending dispatch accepts reordered JSON keys but rejects changed content',async()=>{
 for(const changed of [false,true]){
  const item={stateKey:'test',staged:true,payload:{text:'hello',requestId:'req',artifacts:[{id:'x',revision:1}]}};
  const current={...item,payload:{artifacts:[{revision:1,id:'x'}],requestId:'req',text:changed?'changed':'hello'}};
  let writes=0;
  const c={fetch:async(url,opts)=>{if(opts?.method==='PUT'){writes++;return{ok:true}};return{ok:true,json:async()=>({revision:1,value:{items:{req:current}}})}},chatReadDeliveryOutbox:()=>[item],chatWriteDeliveryOutbox:()=>{}};
  vm.createContext(c);vm.runInContext(state.slice(0,state.indexOf('class ')),c);
  vm.runInContext(src.slice(src.indexOf('async function chatUpdateStaged('),src.indexOf('function chatRenderStagedMessages(')),c);
  if(changed)await assert.rejects(()=>c.chatUpdateStaged(item,{...item,staged:false}),/changed on another device/);else await c.chatUpdateStaged(item,{...item,staged:false});
  assert.equal(writes,changed?0:1);
 }
});
test('queue refresh replaces stale edits and claims, and removes dispatched rows without resending',async()=>{
 for(const status of ['edited','claimed','removed']){
  const old={scope:'codex/abc',stateKey:'key',staged:true,payload:{requestId:'req',text:'old'}};
  const remote={...old,staged:status!=='claimed',payload:{...old.payload,text:status==='edited'?'edited':'old'}};
  let local=[old],requests=[];
  const c={chatSyncedDrafts:new Map([['codex/abc',{key:'key'}]]),chatReadDeliveryOutbox:()=>local,chatWriteDeliveryOutbox:items=>local=items,chatIsTerminalDelivery:()=>true,chatAcceptDelivery:()=>{throw Error('unexpected acknowledgement')},fetch:async(url,opts)=>{requests.push({url,opts});return url.includes('/deliveries')?{ok:true,json:async()=>({key:'key',slot:'deliveries',value:{items:status==='removed'?{}:{req:remote}}})}:{ok:false,status:404}}};
  vm.createContext(c);
  vm.runInContext(src.slice(src.indexOf('async function chatLoadDeliveryRecovery('),src.indexOf('function chatRememberDelivery(')),c);
  await c.chatLoadDeliveryRecovery('codex/abc');
  assert.equal(local.length,status==='removed'?0:1);
  if(status==='edited')assert.equal(local[0].payload.text,'edited');
  if(status==='claimed')assert.equal(local[0].staged,false);
  assert.ok(requests.every(r=>!r.opts?.method||r.opts.method==='GET'),'refresh never sends');
 }
});
