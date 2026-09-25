const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{resolve,promise}};
const requests=[],paint=[],landing=[],cached=[],composers=[];
let preparation=null,runtimeName='Named by owner';
const ctx=vm.createContext({chatTermReadHealth(){},chatTerminalLoadTicket:0,chatRouteVersion:1,chatAgent:'codex',chatOpenId:'one',chatTermOpen:null,
 chatTermFind:()=>({id:'one',name:runtimeName}),chatTermApplyState:s=>s,chatTermBase:()=>'/term/one',chatIsTerm:()=>true,els:{chatView:{hidden:false}},
 fetch:(url,opts)=>{assert.equal(opts,undefined,'no stale auto-name write');const d=deferred();requests.push(d);return d.promise},
 renderChatLanding:()=>landing.push(true),chatOriginArtifactSelection:async()=>null,
 chatPrepareDraft:()=>preparation?preparation.promise:Promise.resolve(),chatPrepareReadingPosition:async()=>{},chatReadingGestureUntil:0,
 chatPollTimer:null,chatES:null,chatLive:null,document:{querySelector:()=>null,getElementById:()=>null},chatTermSurface(){},chatRemember(){},
 chatTermOpenFrom:(id,se,d)=>({id,se,signature:d.signature,live:false}),chatTermSignature:o=>o.signature,
 renderChatTermTranscript:()=>paint.push(ctx.chatTermOpen.signature),chatStageRemember:(_key,v)=>cached.push(v.o.signature),chatStageKey:()=>'',
 renderChatComposer:()=>composers.push(ctx.chatTermOpen.signature),chatTermComposerSession:()=>({}),chatTermPlaceholderRe:/^minted$/,
 ensureChatTermFast(){},chatReadingStates:new Map()
});
vm.runInContext(source.slice(source.indexOf('async function loadChatTermSession(id)'),source.indexOf('// chatTermOpenFrom')),ctx);
const response=signature=>({ok:true,json:async()=>({signature,title:'Automatic title',run:{state:'completed'},conversation:{key:'one'}})});
(async()=>{
 for(const outcome of ['success','404']){
  const old=ctx.loadChatTermSession('one'),request=requests.at(-1);
  const next=ctx.loadChatTermSession('one');requests.at(-1).resolve(response('new-'+outcome));await next;
  request.resolve(outcome==='404'?{ok:false}:response('old'));await old;
  assert.equal(paint.at(-1),'new-'+outcome);assert.equal(cached.at(-1),'new-'+outcome);assert.deepEqual(landing,[]);
 }
 const roundtrip=ctx.loadChatTermSession('one');ctx.chatRouteVersion+=2;requests.at(-1).resolve(response('previous-visit'));await roundtrip;
 assert.equal(paint.includes('previous-visit'),false);
 const oldTail=ctx.loadChatTermSession('one');ctx.chatTermOpen.signature='tail-advanced';requests.at(-1).resolve(response('old-full-read'));await oldTail;
 assert.equal(ctx.chatTermOpen.signature,'tail-advanced','full read cannot roll back a newer tail');
 const oldRun=ctx.loadChatTermSession('one');ctx.chatTermOpen.se.run={state:'failed'};requests.at(-1).resolve(response('old-run'));await oldRun;
 assert.equal(ctx.chatTermOpen.se.run.state,'failed','run evidence cannot roll back with unchanged turns');
 runtimeName='minted';preparation=deferred();const delayed=ctx.loadChatTermSession('one');requests.at(-1).resolve(response('painted-before-preparation'));
 while(paint.at(-1)!=='painted-before-preparation')await Promise.resolve();
 const before=composers.length;ctx.chatRouteVersion+=2;preparation.resolve();await delayed;
 assert.equal(composers.length,before,'late preparation cannot touch another visit composer');
 console.log('PASS: terminal latest-load and route fences, stale errors, tail/run evidence and preparation isolation');
})().catch(e=>{console.error(e);process.exitCode=1});
