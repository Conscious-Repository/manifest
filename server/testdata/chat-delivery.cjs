const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const storage=new Map();
global.localStorage={getItem:k=>storage.get(k)||null,setItem:(k,v)=>storage.set(k,v)};
global.crypto=require('node:crypto').webcrypto;
global.chatBaseFor=a=>'/api/agents/chat/'+encodeURIComponent(a)+'/sessions';
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
vm.runInThisContext(src.slice(src.indexOf('const chatDeliveryStorageKey =')));
let requests=[],accepted=new Map(),lose=true;
global.fetchJSONRetry=async(method,url,payload)=>{
 requests.push({url,id:payload.requestId});
 if(!accepted.has(payload.requestId))accepted.set(payload.requestId,{id:'same-conversation',status:'thinking'});
 if(lose){lose=false;throw Error('Response lost after acceptance');}
 return {ok:true,json:async()=>accepted.get(payload.requestId)};
};
(async()=>{
 const payload={text:'first instruction',files:[]},url=chatBaseFor('alfred');
 const first=chatRememberDelivery('alfred/new','alfred',url,payload);
 await assert.rejects(()=>chatDeliverRemembered(first));
 assert.equal(chatReadDeliveryOutbox().length,1);
 // Reload-style lookup uses persisted data, not an in-memory request object.
 const recovered=chatRememberDelivery('alfred/new','alfred',url,payload);
 assert.equal(recovered.payload.requestId,first.payload.requestId);
 await chatDeliverRemembered(recovered);
 assert.equal(new Set(requests.map(r=>r.id)).size,1);
 assert.equal(accepted.size,1,'one instruction must create only one conversation');
 assert.equal(chatReadDeliveryOutbox().length,0);
 const intentional=chatRememberDelivery('alfred/new','alfred',url,payload);
 assert.notEqual(intentional.payload.requestId,first.payload.requestId,'a later intentional send is separate');
 global.fetchJSONRetry=async()=>({ok:true,json:async()=>({})});
 await assert.rejects(()=>chatDeliverRemembered(intentional));
 assert.equal(chatReadDeliveryOutbox().length,1,'malformed acknowledgement must not discard text');
 global.fetchJSONRetry=async()=>({ok:false,status:400,text:async()=>'message too large'});
 await assert.rejects(()=>chatDeliverRemembered(intentional),e=>e.rejected===true);
 console.log('Lost create response, reload recovery, safe retry, later intentional send and malformed acknowledgement passed');
})().catch(e=>{console.error(e);process.exitCode=1});
