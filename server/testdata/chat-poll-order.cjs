const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
let tick,timeout,resolve,reject,calls=0,painted=[];
const ctx=vm.createContext({AbortController,
 chatAgent:'alfred',chatOpenId:'one',chatRouteVersion:1,chatPollTimer:null,chatLastUpdated:'before',
 chatIsPortal:()=>false,chatBase:()=>'/native',els:{chatView:{hidden:false}},
 setInterval:fn=>{tick=fn;return 1},clearInterval(){},setTimeout:fn=>{timeout=fn;return 1},clearTimeout(){timeout=null},
 fetch:(url,opts)=>{calls++;return new Promise((yes,no)=>{resolve=yes;reject=no;opts.signal.addEventListener('abort',()=>no(Error('aborted')));});},
 chatTranscriptSignature:d=>d.signature,
 renderChatTranscript:d=>{painted.push(d.signature);ctx.chatLastUpdated=d.signature},renderChatComposer(){},loadChatSessions:async()=>{},renderChatRail(){},location:{hash:'',origin:'http://app'},
 // a tile's frame: the tile manager posts shown/focused (50-chat-tiles.js)
 document:{hidden:false},chatRefreshCurrentDraft(){wakes++;},chatTermWake(){}
});
// a never-settled await ends the process with status 0: that is a failure
let finished=false;process.on('exit',()=>{if(!finished&&!process.exitCode){console.error('FAIL: stopped before the last assertion');process.exitCode=1;}});
let wakes=0,paneMessage=null;const parent={};
ctx.window={parent,frameElement:{getClientRects:()=>ctx.frameRects},addEventListener:(type,fn)=>{if(type==='message')paneMessage=fn;}};ctx.frameRects=[{}];
const settle=()=>new Promise(r=>setImmediate(r));
const pane=data=>paneMessage({source:parent,origin:'http://app',data:{type:'manifest:pane',...data}});
vm.runInContext(source.slice(source.indexOf('function ensureChatPoll('),source.indexOf('function loadChat()')),ctx);
const respond=(signature,extra={})=>resolve({ok:true,json:async()=>({signature,session:{status:'thinking'},...extra})});
(async()=>{
 ctx.ensureChatPoll({status:'thinking'},0);
 const first=tick();await tick();await tick();assert.equal(calls,1,'slow polling must not overlap');
 respond('first');await first;assert.deepEqual(painted,['first']);
 const second=tick();ctx.chatLastUpdated='newer-manual-refresh';respond('stale');await second;
 assert.deepEqual(painted,['first'],'slow poll cannot roll back another refresh');
 const third=tick();respond('current');await third;assert.deepEqual(painted,['first','current']);
 const failed=tick();reject(Error('offline'));await failed;
 const unavailable=tick();resolve({ok:false});await unavailable;
 const stalled=tick();timeout();await stalled;
 const recovery=tick();respond('recovered');await recovery;assert.equal(painted.at(-1),'recovered','failures and timeout release polling');
 const departed=tick();ctx.chatRouteVersion++;respond('wrong-route',{sharedConversation:{route:'#/private-other'}});await departed;
 assert.equal(ctx.location.hash,'');assert.equal(painted.at(-1),'recovered');
 assert.equal(timeout,null,'timeouts are cleaned up');
 ctx.chatRouteVersion=1;
 // a pane nobody can see makes no request: the browser tab hidden, or the tile's box
 let before=calls;ctx.document.hidden=true;await tick();ctx.document.hidden=false;ctx.frameRects=[];await tick();
 assert.equal(calls,before,'a hidden pane polled');
 // shown again: the manager's wake reads at once, without waiting for a tick
 ctx.frameRects=[{}];pane({shown:true,focused:true,woke:true});assert.equal(calls,before+1,'a shown pane must read at once');assert.equal(wakes,1,'and refresh its draft recovery');
 respond('woken');await settle();assert.equal(painted.at(-1),'woken');
 // an unfocused tile waits out the manager cadence; focusing it reads at once
 pane({shown:true,focused:false});before=calls;await tick();await tick();
 assert.equal(calls,before,'an unfocused tile polled at the single-chat cadence');
 pane({shown:true,focused:true});assert.equal(calls,before+1,'a focused tile must read at once');respond('focused');await settle();
 // messages from anything but the parent are ignored
 paneMessage({source:{},origin:'http://app',data:{type:'manifest:pane',shown:true,focused:false}});before=calls;const still=tick();
 assert.equal(calls,before+1,'a foreign message changed the pane state');respond('still-focused');await still;
 finished=true;
 console.log('PASS: serialized polling, newer-refresh fence, bounded failure recovery, route isolation, and pane visibility (hidden silent, shown/focused read at once, unfocused throttled)');
})().catch(e=>{console.error(e);process.exitCode=1});
