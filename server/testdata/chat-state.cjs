const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const code=fs.readFileSync(require('node:path').join(__dirname,'../web/js/47-chat-state.js'),'utf8');
const key='conversation-0123456789abcdef0123456789abcdef';
let remote={key,slot:'draft',revision:0,value:null},offline=false,lose=false,puts=0,delayGet=null;
const clone=v=>JSON.parse(JSON.stringify(v));
async function fetch(url,options={}){
 if(offline)throw Error('offline');
 if(options.method!=='PUT'){
  const snapshot=clone(remote);if(delayGet)await delayGet;
  return {ok:true,json:async()=>snapshot};
 }
 puts++;const b=JSON.parse(options.body);
 if(b.revision!==remote.revision && JSON.stringify(b.value)!==JSON.stringify(remote.value))return {status:409,json:async()=>clone(remote)};
 if(JSON.stringify(b.value)!==JSON.stringify(remote.value))remote={...remote,revision:remote.revision+1,value:clone(b.value)};
 if(lose){lose=false;throw Error('lost acknowledgement');}
 return {ok:true,json:async()=>clone(remote)};
}
function device(storage=new Map()){
 const context=vm.createContext({fetch,localStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(code+';globalThis.Draft=ChatDraftState;',context);
 return new context.Draft(key);
}
(async()=>{
 const a=device(),b=device();await a.refresh();await b.refresh();
 a.set({text:'desktop',files:[]});await a.flush();
 b.set({text:'phone',files:[]});assert.equal(await b.flush(),false);assert.equal(b.conflict.value.text,'desktop');assert.equal(b.value.text,'phone');
 await b.resolve(false);assert.equal(remote.value.text,'phone');
 await a.refresh();assert.equal(a.value.text,'phone');
 // A stale read started before a save cannot overwrite the acknowledged save.
 let release;delayGet=new Promise(r=>release=r);const refreshing=a.refresh();
 a.set({text:'newer',files:[]});await a.flush();release();delayGet=null;await refreshing;assert.equal(a.value.text,'newer');
 // An acknowledgement lost after persistence is recovered without duplication.
 lose=true;a.set({text:'lost ack',files:[]});assert.equal(await a.flush(),false);const rev=remote.revision;await a.flush();assert.equal(remote.revision,rev);
 // Accepted old message cannot erase subsequent typing.
 const sent=a.value;a.set({text:'next message',files:[]});assert.equal(a.clearSent(sent),false);await a.flush();
 const current=a.value;assert.equal(a.clearSent(current),true);await a.flush();await b.refresh();assert.equal(b.value.text,'');
 // Local recovery survives offline reload; a failed flush never spins retries.
 const storage=new Map(),c=device(storage);await c.refresh();offline=true;c.set({text:'offline draft',files:[]});const before=puts;await Promise.all([c.flush(),c.flush()]);assert.equal(puts,before);
 const restored=device(storage);await restored.refresh();assert.equal(restored.value.text,'offline draft');offline=false;await restored.refresh();await restored.flush();assert.equal(remote.value.text,'offline draft');
 // Malformed responses must not replace the draft or count as saved.
 const ctx=vm.createContext({fetch:async()=>({ok:true,json:async()=>({revision:999,value:null})}),localStorage:{getItem:()=>null,setItem(){}},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(code+';globalThis.Draft=ChatDraftState;',ctx);const bad=new ctx.Draft(key);bad.set({text:'keep'});assert.equal(await bad.flush(),false);assert.equal(bad.value.text,'keep');assert.equal(bad.revision,0);
 console.log('Independent drafts, conflict resolution, stale reads, lost acknowledgements, safe clearing, offline recovery and malformed responses passed');
})().catch(e=>{console.error(e);process.exitCode=1});
