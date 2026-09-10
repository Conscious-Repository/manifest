const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const shared=new Map(),receipts=new Map();let sends=0,offline=false,race=true;
const key='conversation-0123456789abcdef0123456789abcdef',scope='alfred/20260909-100000-abcd';
const clone=v=>JSON.parse(JSON.stringify(v));
function device(){
 const storage=new Map();
 const ctx=vm.createContext({crypto:require('node:crypto').webcrypto,chatSyncedDrafts:new Map(),
  localStorage:{getItem:k=>storage.get(k)||null,setItem:(k,v)=>storage.set(k,v)},
  chatBaseFor:a=>'/api/agents/chat/'+a+'/sessions',chatStateEqual:(a,b)=>JSON.stringify(a)===JSON.stringify(b),
  fetch:async(url,options={})=>{
   if(offline)throw Error('offline');
   if(url.includes('/delivery?')){const receipt=receipts.get(url.split('request=')[1]);return {ok:!!receipt,json:async()=>clone(receipt)};}
   assert.equal(url,'/api/chat/state/'+key+'/deliveries');
   const current=shared.get(key)||{key,slot:'deliveries',revision:0,value:null};
   if(options.method!=='PUT')return {ok:true,json:async()=>clone(current)};
   const body=JSON.parse(options.body);
   if(race){race=false;shared.set(key,{...current,revision:1,value:{items:{other:{scope:'other'}}}});return {status:409};}
   if(body.revision!==current.revision)return {status:409};
   const next={...current,revision:current.revision+1,value:body.value};shared.set(key,next);return {ok:true,json:async()=>clone(next)};
  },
  fetchJSONRetry:async(method,url,payload)=>{sends++;receipts.set(payload.requestId,{id:'20260909-100000-abcd',delivery:{id:payload.requestId,state:'queued'}});throw Error('lost acknowledgement');}
 });
 vm.runInContext(src.slice(src.indexOf('const chatDeliveryStorageKey =')),ctx);
 return ctx;
}
(async()=>{
 const first=device(),draft={text:'Research this',files:[]};
 first.chatSyncedDrafts.set(scope,{key,value:draft});
 const item=first.chatRememberDelivery(scope,'alfred','/api/agents/chat/alfred/sessions/20260909-100000-abcd/messages',{text:draft.text});
 await assert.rejects(()=>first.chatDeliverRemembered(item),/lost acknowledgement/);
 assert.equal(sends,1);assert.ok(shared.get(key).value.items[item.payload.requestId]);assert.ok(shared.get(key).value.items.other,'concurrent record survives');
 // A different browser has neither the original outbox nor its in-memory state.
 const second=device();let cleared=0;
 second.chatSyncedDrafts.set(scope,{key,value:draft,reconcileSent:async sent=>{assert.deepEqual(clone(sent),draft);cleared++;return true;}});
 await second.chatLoadDeliveryRecovery(scope);
 assert.equal(cleared,1);assert.equal(sends,1,'recovery must not send');assert.equal(second.chatReadDeliveryOutbox().length,0);
 assert.equal(shared.get(key).value.items[item.payload.requestId],undefined);assert.ok(shared.get(key).value.items.other);
 // A crash before submission leaves a discoverable unsent item, never autoplay.
 const waiting=first.chatRememberDelivery(scope,'alfred',item.url,{text:'Not submitted'});
 await first.chatSaveDeliveryRecovery(waiting);
 await second.chatLoadDeliveryRecovery(scope);
 assert.equal(cleared,1);assert.equal(sends,1);assert.equal(second.chatReadDeliveryOutbox().length,1);
 // Uncertain native receipt remains visible and cannot clear the draft.
 const native=first.chatRememberDelivery(scope,'codex','/api/terminal/session/abcdef123456/input',{text:'Native work'});
 await first.chatSaveDeliveryRecovery(native);receipts.set(native.payload.requestId,{id:'abcdef123456',delivery:{id:native.payload.requestId,state:'unconfirmed'}});
 await second.chatLoadDeliveryRecovery(scope);assert.equal(cleared,1);assert.equal(sends,1);assert.equal(second.chatReadDeliveryOutbox().length,2);
 offline=true;await assert.rejects(()=>first.chatDeliverRemembered(waiting));assert.equal(sends,1,'cannot dispatch before recovery persistence');
 console.log('Cross-device lost acknowledgement recovery, CAS merge, absent/uncertain receipt and offline send protection passed');
})().catch(e=>{console.error(e);process.exitCode=1});
