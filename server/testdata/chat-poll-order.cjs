const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
let tick,timeout,resolve,reject,calls=0,painted=[];
const ctx=vm.createContext({AbortController,
 chatAgent:'alfred',chatOpenId:'one',chatRouteVersion:1,chatPollTimer:null,chatLastUpdated:'before',
 chatIsPortal:()=>false,chatBase:()=>'/native',els:{chatView:{hidden:false}},
 setInterval:fn=>{tick=fn;return 1},clearInterval(){},setTimeout:fn=>{timeout=fn;return 1},clearTimeout(){timeout=null},
 fetch:(url,opts)=>{calls++;return new Promise((yes,no)=>{resolve=yes;reject=no;opts.signal.addEventListener('abort',()=>no(Error('aborted')));});},
 chatTranscriptSignature:d=>d.signature,
 renderChatTranscript:d=>{painted.push(d.signature);ctx.chatLastUpdated=d.signature},renderChatComposer(){},loadChatSessions:async()=>{},renderChatRail(){},location:{hash:''}
});
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
 console.log('PASS: serialized polling, newer-refresh fence, bounded failure recovery and route isolation');
})().catch(e=>{console.error(e);process.exitCode=1});
