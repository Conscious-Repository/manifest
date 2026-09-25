const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
let timeout,reply,fail,errors=[],health=[];
const ctx=vm.createContext({AbortController,chatTerminalLoadTicket:0,chatRouteVersion:1,chatAgent:'codex',chatOpenId:'one',chatTermOpen:null,
 chatTermFind:()=>({id:'one'}),chatTermBase:()=>'/term/one',chatTermSignature:o=>JSON.stringify(o),chatIsTerm:()=>true,els:{chatView:{hidden:false}},
 setTimeout:fn=>{timeout=fn;return 1},clearTimeout:()=>{timeout=null},
 fetch:(_url,opts)=>new Promise((yes,no)=>{reply=yes;fail=no;opts.signal.addEventListener('abort',()=>no(Error('timeout')))}),
 renderChatEmpty:msg=>errors.push(msg),chatTermReadHealth:(o,failed)=>health.push([o,failed]),renderChatLanding:()=>{throw Error('must not render landing')}
});
vm.runInContext(source.slice(source.indexOf('async function loadChatTermSession(id)'),source.indexOf('// chatTermOpenFrom')),ctx);
(async()=>{
 for(const cached of [false,true])for(const kind of ['http','malformed','json','network','timeout']){
  errors=[];health=[];const root={id:'one',se:{run:{state:'completed'}},turns:['retained']};ctx.chatTermOpen=cached?root:null;
  const load=ctx.loadChatTermSession('one');
  if(kind==='http')reply({ok:false,status:503});
  if(kind==='malformed')reply({ok:true,json:async()=>({offset:-1,turns:[]})});
  if(kind==='json')reply({ok:true,json:async()=>{throw Error('JSON')}});
  if(kind==='network')fail(Error('offline'));
  if(kind==='timeout')timeout();
  await load;
  assert.equal(errors.length,cached?0:1);assert.equal(health.length,cached?1:0);assert.equal(ctx.chatOpenId,'one');assert.equal(ctx.chatTermOpen,cached?root:null);assert.equal(root.se.run.state,'completed');assert.equal(timeout,null);
 }
 errors=[];health=[];const old=ctx.loadChatTermSession('one');ctx.chatRouteVersion+=2;reply({ok:false,status:404});await old;
 assert.deepEqual(errors,[]);assert.deepEqual(health,[]);
 console.log('PASS: bounded initial terminal loading, retry errors, cached retention and stale failure isolation');
})().catch(e=>{console.error(e);process.exitCode=1});
