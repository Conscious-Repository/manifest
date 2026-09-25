const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return{promise,resolve,reject}};
const requests=[],paints=[],errors=[],cached=[],drafts=[];
const ctx=vm.createContext({chatSessionLoadTicket:0,chatRouteVersion:1,chatLastUpdated:'initial',chatAgent:'alfred',chatOpenId:'one',chatCurSession:null,
 chatIsTerm:()=>false,chatBase:()=>'/alfred',els:{chatView:{hidden:false}},location:{hash:''},
 fetch:()=>{const d=deferred();requests.push(d);return d.promise},
 chatStageCache:{delete:key=>errors.push('evicted '+key)},chatStageKey:()=> 'one',renderChatEmpty:message=>errors.push(message),
 chatPrepareDraft:async(...args)=>drafts.push(args),chatPrepareReadingPosition:async()=>{},chatRemember(){},chatConversationTasks:new Map(),
 document:{querySelector:()=>null,getElementById:()=>null},chatStageRemember:(_key,data)=>cached.push(data.d.signature),
 renderChatTranscript:d=>{paints.push(d.signature);ctx.chatLastUpdated=d.signature},renderChatComposer(){},chatPendingWorkspace:null,ensureChatStream(){},ensureChatPoll(){},chatTranscriptSignature:d=>d.signature
});
vm.runInContext(source.slice(source.indexOf('async function loadChatSession(id)'),source.indexOf('// ---- live stream layer')),ctx);
const data=signature=>({signature,session:{id:'one',turns:2,status:'idle'},conversation:{key:'one'}});
const ok=d=>({ok:true,json:async()=>d});
(async()=>{
 for(const oldOutcome of ['success','404','network-error','redirect']){
  const old=ctx.loadChatSession('one'),oldRequest=requests.at(-1);
  const fresh=ctx.loadChatSession('one');requests.at(-1).resolve(ok(data('fresh-'+oldOutcome)));await fresh;
  if(oldOutcome==='network-error')oldRequest.reject(Error('late error'));
  else if(oldOutcome==='404')oldRequest.resolve({ok:false,status:404});
  else oldRequest.resolve(ok(oldOutcome==='redirect'?{sharedConversation:{route:'#/wrong'}}:data('stale')));
  await old;
  assert.equal(paints.at(-1),'fresh-'+oldOutcome);assert.equal(cached.at(-1),'fresh-'+oldOutcome);
  assert.deepEqual(errors,[]);assert.equal(ctx.location.hash,'');
 }
 const old=ctx.loadChatSession('one');ctx.chatRouteVersion+=2;requests.at(-1).resolve(ok(data('before-away-and-back')));await old;
 assert.equal(paints.includes('before-away-and-back'),false,'same route after navigation is a new visit');
 const polled=ctx.loadChatSession('one');ctx.chatLastUpdated='newer-poll';requests.at(-1).resolve(ok(data('older-load')));await polled;
 assert.equal(paints.includes('older-load'),false,'explicit load cannot replace a newer poll');
 const body=deferred(),started=deferred();
 const loading=ctx.loadChatSession('one');requests.at(-1).resolve({ok:true,json:()=>{started.resolve();return body.promise}});await started.promise;
 ctx.chatRouteVersion+=2;body.resolve({sharedConversation:{route:'#/stale-body'}});await loading;
 assert.equal(ctx.location.hash,'','route fence also follows asynchronous JSON parsing');
 assert.equal(drafts.length,4,'only four current snapshots prepare drafts');
 console.log('PASS: latest explicit load, round-trip route identity, stale errors/redirects, JSON and poll fences');
})().catch(e=>{console.error(e);process.exitCode=1});
