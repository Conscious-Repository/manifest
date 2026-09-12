// chat-stage-switch.cjs — moving between threads turns the stage over
// synchronously (2026-09-12): the previous transcript never waits on the
// network, a thread seen earlier repaints from the stage cache, the thread's
// own fetch starts alongside the list refresh, and a fresh payload that
// matches the primed paint does not repaint. Run: node server/testdata/chat-stage-switch.cjs
(async()=>{
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
const tick=()=>new Promise(r=>setTimeout(r,0));

// ---- A. the router: prime first, eager thread fetch, no second load ----
{
 let releaseRoster;const calls={prime:0,loads:[],composer:0,rail:0,heads:[]};
 const ctx=vm.createContext({
  chatCloseTerminalDock(){},chatCloseWorkspace(){},chatSaveDraft(){},leaveTaskChat(){},renderChatHeadActions(){},ensureTerminalEvents(){},
  document:{getElementById:()=>({dataset:{}}),querySelector:()=>null,querySelectorAll:()=>[]},
  chatMountHeader:h=>calls.heads.push(h),chatStagePrime:()=>{calls.prime++;},loadChatSession:id=>calls.loads.push(id),
  renderChatComposer:()=>calls.composer++,renderChatRail:()=>calls.rail++,renderChatLanding(){},
  loadChatRoster:()=>new Promise(r=>releaseRoster=r),loadChatSessions:async()=>{},loadChatTermSessions:async()=>{},
  chatRosterEntry:name=>ctx.chatRoster.find(a=>a.name===name)||null,chatCurrentSessions:()=>[],chatRecall:()=>'',chatRemember(){},
  requestAnimationFrame(){},window:{addEventListener(){}},chatFitShell(){},clearInterval(){},
  els:{chatView:{hidden:false}},chatIsTerm:name=>(name===undefined?ctx.chatAgent:name)==='claude',chatTermFind:()=>null,
  chatRoster:[{name:'alfred',enabled:true}],chatPollTimer:null,chatRouteVersion:0,chatLanding:false,chatOpenId:'',chatAgent:'',
  chatReadingGestureUntil:0,chatDraftKey:'',chatPendingFiles:[],chatFitBound:true,
 });
 vm.runInContext(slice('function chatRouteSegments(h)','\ndocument.addEventListener("pointerdown"'),ctx);
 vm.runInContext(slice('function chatPrivateCreationAgent(agent)','\nfunction chatNewHash'),ctx);
 vm.runInContext(slice('function showChat(h) {','\nlet chatTaskID'),ctx);
 // warm section: the stage primes and the thread fetch starts before the roster answers
 ctx.showChat('#/chat/a/alfred/b');
 assert.equal(calls.prime,1,'the stage turns over synchronously');
 assert.deepEqual(calls.loads,['b'],'the thread fetch starts alongside the list refresh');
 assert.equal(ctx.chatOpenId,'b');assert.equal(ctx.chatAgent,'alfred');
 releaseRoster();await tick();await tick();
 assert.deepEqual(calls.loads,['b'],'the list refresh does not re-request the thread');
 assert.equal(calls.composer,0,'the list refresh leaves the primed composer alone');
 assert.equal(calls.rail,1,'the rail still repaints after the lists load');
 // cold page: no roster yet → prime still clears the stage, the load waits for the lists
 ctx.chatRoster=[];ctx.showChat('#/chat/a/alfred/c');
 assert.equal(calls.prime,2);assert.deepEqual(calls.loads,['b'],'no eager fetch before the roster is known');
 releaseRoster();await tick();await tick();
 assert.deepEqual(calls.loads,['b','c']);assert.equal(calls.composer,1,'the serial path still renders the composer');
 // a terminal thread the registry has not listed yet is never fetched early
 ctx.chatRoster=[{name:'alfred',enabled:true}];ctx.showChat('#/chat/a/claude/deadbeef');
 assert.equal(calls.prime,3);assert.deepEqual(calls.loads,['b','c'],'an unlisted terminal thread waits for the registry');
 releaseRoster();await tick();await tick();
 assert.deepEqual(calls.loads,['b','c','deadbeef']);
 // a section route (landing) primes nothing and clears the head
 const headsBefore=calls.heads.length;ctx.showChat('#/chat/a/alfred');
 assert.equal(calls.prime,3,'a landing route does not prime a thread');assert.equal(calls.heads[headsBefore],null);
 console.log('A. router: synchronous prime, eager thread fetch, single load');
}

// ---- B. chatStagePrime: empty stage under the provisional head, or the cached paint ----
{
 const host={innerHTML:'<p>previous thread</p>',dataset:{readKey:'previous'}};
 const rows=[{dataset:{inboxKey:'agent:alfred/b'},open:null,classList:{toggle(c,on){this.open=on;}}},{dataset:{inboxKey:'agent:alfred/a'},open:null,classList:{toggle(c,on){this.open=on;}}}];
 rows.forEach(r=>{r.classList.toggle=r.classList.toggle.bind(r);});
 const calls={transcript:[],term:0,composer:[],heads:[],leave:0,finish:0,surface:[]};
 const ctx=vm.createContext({
  document:{getElementById:id=>id==='chatTranscript'?host:null,querySelector:sel=>sel==='.chat-main'?{classList:{remove(){}}}:null,querySelectorAll:()=>rows},
  chatOpenId:'b',chatAgent:'alfred',chatIsTerm:()=>ctx.chatAgent==='claude',chatTermFind:id=>ctx.termRows.find(s=>s.id===id)||null,termRows:[],
  finishChatLive:()=>calls.finish++,chatTermLeave:()=>{calls.leave++;ctx.chatTermOpen=null;},chatTermSurface:on=>calls.surface.push(on),
  chatTermApplyState:se=>se,renderChatTermTranscript:()=>calls.term++,chatTermComposerSession:()=>({term:true}),chatTermHead:o=>({termHead:o.id}),
  renderChatTranscript:d=>{calls.transcript.push(d);ctx.chatCurSession=d.session;},renderChatComposer:s=>calls.composer.push(s),
  chatMountHeader:h=>calls.heads.push(h),chatHead:row=>({head:row.title}),chatCurrentSessions:()=>[{id:'b',title:'Thread B'}],
  chatCurSession:{id:'a'},chatLastUpdated:'sig-a',chatTermOpen:null,
 });
 vm.runInContext(slice('function chatInboxKey(entry)','\n'),ctx);
 vm.runInContext(slice('const chatStageCache = new Map();','\nfunction showChat(h)'),ctx);
 // 1. unseen thread: the previous transcript is gone before any fetch, the head names the target
 assert.equal(ctx.chatStagePrime(),false);
 assert.equal(host.innerHTML,'','the previous thread leaves the stage synchronously');
 assert.equal(host.dataset.readKey,'');assert.equal(ctx.chatCurSession,null);assert.equal(ctx.chatLastUpdated,'');
 assert.deepEqual(calls.heads,[{head:'Thread B'}],'a provisional head from the list row');
 assert.deepEqual(calls.composer,[undefined]);assert.equal(calls.finish,1);assert.equal(calls.leave,1);assert.deepEqual(calls.surface,[false]);
 assert.deepEqual(rows.map(r=>r.open),[true,false],'the rail marks the target row open ahead of the list refresh');
 // 2. seen thread: the cached payload paints synchronously
 const d={conversation:{key:'conv-b'},session:{id:'b',title:'Thread B'}};
 ctx.chatStageRemember('alfred/b',{kind:'agent',d});
 host.innerHTML='<p>previous thread</p>';
 assert.equal(ctx.chatStagePrime(),true);
 assert.deepEqual(calls.transcript,[d],'the cached payload repaints');assert.equal(calls.composer[1],d.session);
 // 3. a cached terminal thread whose registry row vanished falls back to the empty stage
 ctx.chatAgent='claude';ctx.chatOpenId='cc1';
 ctx.chatStageRemember('claude/cc1',{kind:'term',o:{id:'cc1',turns:[],se:{id:'cc1'}}});
 host.innerHTML='<p>previous thread</p>';
 assert.equal(ctx.chatStagePrime(),false);assert.equal(host.innerHTML,'');assert.equal(calls.term,0);
 assert.equal(calls.heads.at(-1),null,'no row, no provisional head');
 // 4. a cached terminal thread with its row repaints and rebinds the row
 ctx.termRows=[{id:'cc1',name:'cc1',backend:'herdr',live:true}];
 assert.equal(ctx.chatStagePrime(),true);assert.equal(calls.term,1);
 assert.equal(ctx.chatTermOpen.se,ctx.termRows[0]);assert.equal(ctx.chatTermOpen.live,true);assert.deepEqual(calls.composer.at(-1),{term:true});
 assert.equal(calls.surface.at(-1),true);
 // 5. the cache keeps only the newest entries
 for(let n=0;n<45;n++)ctx.chatStageRemember('alfred/x'+n,{kind:'agent',d:{}});
 const cache=vm.runInContext('chatStageCache',ctx),max=vm.runInContext('chatStageCacheMax',ctx);
 assert.equal(cache.size,max);assert.equal(cache.has('alfred/x44'),true);assert.equal(cache.has('alfred/b'),false);
 console.log('B. prime: empty stage or cached paint, provisional head, rail marker');
}

// ---- C. loadChatSession: cache fill, skip-if-unchanged, evict on 404 ----
{
 const host={dataset:{readKey:''}};const calls={transcript:0,composer:[],empty:0};
 let payload={conversation:{key:'conv-b'},session:{id:'b',status:'idle',updated:'t1'},proposals:[]},status=200;
 const ctx=vm.createContext({
  chatIsTerm:()=>false,chatAgent:'alfred',chatOpenId:'b',chatBase:()=>'/chat/alfred',els:{chatView:{hidden:false}},
  chatPrepareDraft:async()=>{},chatPrepareReadingPosition:async()=>{},
  fetch:async()=>({ok:status===200,status,json:async()=>payload}),
  document:{getElementById:id=>id==='chatTranscript'?host:null,querySelector:()=>null},chatRemember(){},chatConversationTasks:new Map(),
  renderChatTranscript:d=>{calls.transcript++;ctx.chatCurSession=d.session;ctx.chatLastUpdated=ctx.chatTranscriptSignature(d);host.dataset.readKey=d.conversation.key;},
  renderChatComposer:s=>calls.composer.push(s),chatPendingWorkspace:null,ensureChatStream(){},ensureChatPoll(){},renderChatEmpty:()=>calls.empty++,
  chatCurSession:null,chatLastUpdated:'',
 });
 vm.runInContext(slice('const chatStageCache = new Map();','\n// the parts of an open terminal thread'),ctx);
 vm.runInContext(slice('function chatTranscriptSignature(d)','\nfunction ensureChatPoll'),ctx);
 vm.runInContext(slice('async function loadChatSession(id)','\n// ---- live stream layer'),ctx);
 await ctx.loadChatSession('b');
 const cache=vm.runInContext('chatStageCache',ctx);
 assert.equal(calls.transcript,1,'first load paints');assert.equal(cache.get('alfred/b').d,payload,'the payload is cached for the next switch');
 // the same payload again (a primed paint already on stage) does not repaint
 const primed=ctx.chatCurSession;
 await ctx.loadChatSession('b');
 assert.equal(calls.transcript,1,'an unchanged payload leaves the primed paint alone');
 assert.equal(calls.composer.at(-1),primed,'the composer refreshes against the session on stage');
 // a moved thread repaints
 payload={...payload,session:{...payload.session,updated:'t2'}};
 await ctx.loadChatSession('b');assert.equal(calls.transcript,2,'a changed payload repaints');
 // gone: the cache forgets it and the empty state paints
 status=404;await ctx.loadChatSession('b');
 assert.equal(calls.empty,1);assert.equal(cache.has('alfred/b'),false,'a 404 evicts the cached paint');
 console.log('C. load: cache fill, unchanged payload skips the repaint, 404 evicts');
}
})().catch(e=>{console.error(e);process.exit(1);});
