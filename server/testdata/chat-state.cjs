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
 const context=vm.createContext({fetch,TextEncoder,localStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)},setTimeout:()=>1,clearTimeout(){}});
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
 // Accepted-send recovery clears the exact saved draft, but never a newer
 // remote edit. Offline recovery must keep the acceptance record pending.
 a.set({text:'accepted instruction',files:[]});await a.flush();const acceptedDraft=clone(a.value);
 offline=true;assert.equal(await a.reconcileSent(acceptedDraft),false);offline=false;
 await b.refresh();b.set({text:'new phone instruction',files:[]});await b.flush();
 assert.equal(await a.reconcileSent(acceptedDraft),true);assert.equal(remote.value.text,'new phone instruction');
 a.set(acceptedDraft);await a.flush();
 assert.equal(await a.reconcileSent(acceptedDraft),true);assert.equal(remote.value.text,'');
 await a.refresh();await b.refresh();a.set({text:'unsaved desktop edit',files:[]});
 b.set({text:'saved phone edit',files:[]});await b.flush();
 assert.equal(await a.reconcileSent(acceptedDraft),false,'a conflict keeps acknowledgement recovery pending');
 assert.equal(a.value.text,'unsaved desktop edit');assert.equal(remote.value.text,'saved phone edit');
 await a.resolve(true);
 // Local recovery survives offline reload; a failed flush never spins retries.
 const storage=new Map(),c=device(storage);await c.refresh();offline=true;c.set({text:'offline draft',files:[]});const before=puts;await Promise.all([c.flush(),c.flush()]);assert.equal(puts,before);
 const restored=device(storage);await restored.refresh();assert.equal(restored.value.text,'offline draft');offline=false;await restored.refresh();await restored.flush();assert.equal(remote.value.text,'offline draft');
 // Task drafts retain routing preferences while clearing sent mentions/files.
 const task=device();await task.refresh();
 task.set({text:'task ask',files:[{hash:'file',name:'plan.md'}],mentions:['agent:alfred'],mode:'ask',agent:'agent:alfred',selection:{id:'plan:task',revision:'exact-old-version',version:1}});await task.flush();
 const taskSnapshot=task.value;task.clearSent(taskSnapshot);await task.flush();
 assert.equal(task.value.mode,'ask');assert.equal(task.value.agent,'agent:alfred');assert.equal(task.value.mentions.length,0);assert.equal(task.value.files.length,0);
 const taskAgain=device();await taskAgain.refresh();assert.equal(taskAgain.value.text,'');assert.equal(taskAgain.value.mode,'ask');assert.equal(taskAgain.value.selection.revision,'exact-old-version');
 // Side findings commit text and receipt together and survive response rerenders,
 // local reload, another device, Send and ordinary composer replacement.
 const parentStorage=new Map(),parent=device(parentStorage);await parent.refresh();
 parent.set({text:'owner draft',files:[{hash:'keep'}],recipient:{agent:'claude'}});await parent.flush();
 lose=true;assert.equal(await parent.addSideFinding('side:turn1','Finding one'),false);
 assert.equal(remote.value.text,'owner draft\n\nFinding one');
 const reload=device(parentStorage);assert.equal(await reload.addSideFinding('side:turn1','Finding one'),true);
 assert.equal(reload.value.text,'owner draft\n\nFinding one');assert.equal(reload.value.files[0].hash,'keep');
 const other=device();assert.equal(await other.addSideFinding('side:turn1','Finding one'),true);
 assert.equal(other.value.text,reload.value.text);
 other.clearSent(other.value);await other.flush();other.set({text:'new typing',files:[]});await other.flush();
 assert.equal(await reload.addSideFinding('side:turn1','Finding one'),true);assert.equal(reload.value.text,'new typing');
 assert.equal(await reload.addSideFinding('side:turn2','Finding one'),true);assert.equal(reload.value.text,'new typing\n\nFinding one','identical text in a different turn remains distinct');
 // Navigation while refreshing cancels the append; offline never claims success.
 const unchanged=remote.value.text;
 assert.equal(await reload.addSideFinding('side:turn3','wrong destination',()=>false),false);assert.equal(remote.value.text,unchanged);
 offline=true;assert.equal(await reload.addSideFinding('side:turn3','offline'),false);offline=false;assert.equal(remote.value.text,unchanged);
 // Concurrent drafts demand explicit resolution. Keeping local content must
 // retain receipts already committed remotely, even when discarding that text.
 await other.refresh();other.set({text:'local competing draft',files:[]});
 assert.equal(await reload.addSideFinding('side:turn3','remote finding'),true);
 assert.equal(await other.addSideFinding('side:turn4','pending finding'),false);assert.ok(other.conflict);
 await other.resolve(false);assert.equal(await other.addSideFinding('side:turn3','remote finding'),true);
 assert.equal(other.value.text,'local competing draft');
 // Clearing a sent draft reaches the server at once, not after the typing
 // debounce (whose timer never fires in this harness): a second device that
 // reads meanwhile must not adopt and resend the instruction.
 const sender=device();await sender.refresh();sender.set({text:'about to send',files:[]});await sender.flush();
 const sentNow=sender.value;assert.equal(sender.clearSent(sentNow),true);assert.ok(sender.pending,'clearSent writes immediately');await sender.pending;
 assert.equal(remote.value.text,'','cleared draft persisted without an explicit flush');
 const laterDevice=device();await laterDevice.refresh();assert.equal(laterDevice.value.text,'');
 // A flush requested while a write is in flight lands the newest value.
 sender.set({text:'first version',files:[]});const first=sender.flush();
 sender.set({text:'second version',files:[]});const second=sender.flush();await first;await second;
 assert.equal(remote.value.text,'second version','queued flush sends the newest value');
 // A return that would push the draft over the server's size limit is refused
 // before anything is appended, with a reason the child can show.
 const bounded=device();await bounded.refresh();bounded.set({text:'small',files:[]});await bounded.flush();
 const beforeBig=remote.revision;
 assert.equal(await bounded.addSideFinding('side:big','x'.repeat(95000)),false);
 assert.match(bounded.sideReturnError,/size limit/);assert.equal(bounded.value.text,'small');assert.equal(remote.revision,beforeBig);
 assert.equal(await bounded.addSideFinding('side:fits','fits'),true);assert.equal(bounded.sideReturnError,'');
 // A server refusal (400) is reported as such, not as a transient outage.
 const refusing=vm.createContext({fetch:async(url,opts={})=>opts.method==='PUT'?{ok:false,status:400,json:async()=>({})}:{ok:true,json:async()=>({key,slot:'draft',revision:0,value:null})},TextEncoder,localStorage:{getItem:()=>null,setItem(){}},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(code+';globalThis.Draft=ChatDraftState;',refusing);const refused=new refusing.Draft(key);await refused.refresh();refused.set({text:'too big',files:[]});
 assert.equal(await refused.flush(),false);assert.match(refused.error,/size limit/);assert.equal(refused.value.text,'too big');
 // The task UI restores/clears exact context independently for each task,
 // retaining its typed draft when a context chip changes.
 const selections=new Map(),painted=[];
 const taskUI=vm.createContext({chatArtifactSelections:selections,chatRenderArtifactContext:(...args)=>painted.push(args)});
 const panel=fs.readFileSync('web/js/93-todo-panel.js','utf8');
 vm.runInContext(panel.slice(panel.indexOf('const todoComposerDrafts'),panel.indexOf('window.addEventListener("focus"'))+';globalThis.drafts=todoComposerDrafts;globalThis.states=todoSyncedDrafts;',taskUI);
 taskUI.drafts.set('one',{text:'still typing',mode:'ask'});
 taskUI.todoApplyDraftSelection('one',{selection:{id:'old-plan',revision:'v1'}});
 taskUI.todoApplyDraftSelection('two',{selection:{id:'other-plan',revision:'v3'}});
 taskUI.todoSaveArtifactSelection('one');
 assert.equal(taskUI.drafts.get('one').text,'still typing');assert.equal(taskUI.drafts.get('one').selection.revision,'v1');
 taskUI.todoApplyDraftSelection('one',null);taskUI.todoSaveArtifactSelection('one');
 assert.equal(taskUI.drafts.get('one').selection,null);assert.equal(selections.get('task:two').revision,'v3');
 // Artifact edit slots preserve the original save precondition across reload.
 const edits=new Map();let editRemote={key:'artifact-0123456789abcdef',slot:'edit',revision:0,value:null};
 const editContext=vm.createContext({fetch:async(url,opts={})=>{
  assert.equal(url,'/api/chat/state/artifact-0123456789abcdef/edit');
  if(opts.method==='PUT'){const b=JSON.parse(opts.body);editRemote={...editRemote,revision:editRemote.revision+1,value:b.value};}
  return {ok:true,json:async()=>clone(editRemote)};
 },TextEncoder,localStorage:{getItem:k=>edits.get(k),setItem:(k,v)=>edits.set(k,v)},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(code+';globalThis.Draft=ChatDraftState;',editContext);
 const edit=new editContext.Draft(editRemote.key,null,'edit');await edit.refresh();edit.set({text:'revision in progress',baseRevision:'old-head',sourceRevision:'older-version'});await edit.flush();
 const reopen=new editContext.Draft(editRemote.key,null,'edit');await reopen.refresh();assert.equal(reopen.value.baseRevision,'old-head');assert.equal(reopen.value.text,'revision in progress');
 // Malformed responses must not replace the draft or count as saved.
 const ctx=vm.createContext({fetch:async()=>({ok:true,json:async()=>({revision:999,value:null})}),TextEncoder,localStorage:{getItem:()=>null,setItem(){}},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(code+';globalThis.Draft=ChatDraftState;',ctx);const bad=new ctx.Draft(key);bad.set({text:'keep'});assert.equal(await bad.flush(),false);assert.equal(bad.value.text,'keep');assert.equal(bad.revision,0);
 console.log('Independent drafts, conflict resolution, stale reads, lost acknowledgements, safe clearing, offline recovery and malformed responses passed');
})().catch(e=>{console.error(e);process.exitCode=1});
