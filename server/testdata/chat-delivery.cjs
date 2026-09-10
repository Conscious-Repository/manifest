const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const storage=new Map();
global.chatSyncedDrafts=new Map();
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
 await chatForgetDelivery(intentional);
 let cleared=0;const draft={text:'coding instruction',files:[]};
 let synced=false;
 chatSyncedDrafts.set('codex/new',{value:draft,reconcileSent:async value=>{assert.equal(value.text,draft.text);cleared++;return synced;}});
 const codingURL='/api/terminal/session/abcdef123456/input';
 const coding=chatRememberDelivery('codex/abcdef123456','codex',codingURL,{text:draft.text},'codex/new');
 assert.equal(chatReadDeliveryOutbox().length,1);
 assert.equal(chatRememberDelivery('codex/abcdef123456','codex',codingURL,{text:draft.text}).payload.requestId,coding.payload.requestId);
 global.fetchJSONRetry=async()=>({ok:true,json:async()=>({ok:false,id:'abcdef123456',delivery:{state:'unconfirmed'}})});
 await assert.rejects(()=>chatDeliverRemembered(coding),/unconfirmed/);
 assert.equal(chatReadDeliveryOutbox().length,1,'uncertain CLI submission must remain recoverable');
 assert.equal(cleared,0,'uncertain submission must not erase the draft');
 global.fetchJSONRetry=async()=>({ok:true,json:async()=>({ok:true,id:'abcdef123456',delivery:{state:'sent'}})});
 await chatDeliverRemembered(chatReadDeliveryOutbox()[0]);
 assert.equal(cleared,1,'acknowledgement clears the original landing draft');
 assert.equal(chatReadDeliveryOutbox()[0].accepted.delivery.state,'sent','acceptance survives failed draft sync');
 global.fetchJSONRetry=async()=>{throw Error('accepted message must never be resubmitted');};
 synced=true;
 await chatDeliverRemembered(coding); // stale pre-acknowledgement UI object
 assert.equal(cleared,2,'recovery only reconciles the draft');
 assert.equal(chatReadDeliveryOutbox().length,0);
 assert.equal(chatIsTerminalDelivery({...coding,url:'https://example.com/api/terminal/session/abcdef123456/input'}),false);
 assert.equal(chatIsTerminalDelivery({...coding,url:'/api/terminal/session/../input'}),false);
 let releaseCreate,enteredCreate;
 const creating=new Promise(r=>enteredCreate=r),release=new Promise(r=>releaseCreate=r),sent=[];
 const nav=vm.createContext({chatTermSending:false,chatAgent:'codex',chatOpenId:'',chatRouteVersion:1,chatLanding:true,chatTermSessions:[],chatTermOpen:null,
  location:{hash:'#/chat/a/codex/new'},renderChatComposer(){},chatTermComposerSession(){return{};},chatRecall(){return '/working/folder';},
  chatTermFind:id=>nav.chatTermSessions.find(s=>s.id===id),chatTermBase:id=>'/api/terminal/session/'+id,
  postJSONOk:async()=>{enteredCreate();await release;return{id:'abcdef123456',backend:'herdr'};},
  chatRememberDelivery:(scope,agent,url,payload,draftScope)=>({scope,agent,url,payload,draftScope}),
  chatDeliverRemembered:async item=>{sent.push(item);return{ok:true};},loadChatTermSessions:async()=>{},showToast(){}});
 vm.runInContext(src.slice(src.indexOf('async function chatTermSend('),src.indexOf('// ---- landing: a new session')),nav);
 const sending=nav.chatTermSend('original coding instruction');await creating;
 nav.chatAgent='alfred';nav.chatOpenId='other-chat';nav.chatRouteVersion=2;nav.location.hash='#/chat/a/alfred/other-chat';
 releaseCreate();assert.equal(await sending,true);
 assert.equal(sent[0].agent,'codex');assert.equal(sent[0].draftScope,'codex/new');assert.equal(sent[0].url,codingURL);
 assert.equal(nav.chatOpenId,'other-chat');assert.equal(nav.location.hash,'#/chat/a/alfred/other-chat','late creation must not hijack navigation');
 console.log('Lost create response, reload recovery, safe retry, later intentional send and malformed acknowledgement passed');
})().catch(e=>{console.error(e);process.exitCode=1});
