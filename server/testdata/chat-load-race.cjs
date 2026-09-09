const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
let release,requested;
const pending=new Promise(r=>release=r),started=new Promise(r=>requested=r),drafts=[];
const ctx=vm.createContext({chatIsTerm:()=>false,chatAgent:'alfred',chatOpenId:'source',chatBase:()=>'/chat/'+ctx.chatAgent,
 els:{chatView:{hidden:false}},chatPrepareDraft:async(...args)=>drafts.push(args),
 fetch:async url=>{
  if(url.startsWith('/api/artifacts/')){requested();await pending;return {ok:true,json:async()=>({title:'Plan',revisions:[]})};}
  return {ok:true,json:async()=>({conversation:{key:'original'},session:{turns:0,origin:{task:'task',prompt:'private handoff',artifacts:[{id:'plan',revision:'v1'}]}}})};
 }});
vm.runInContext(source.slice(source.indexOf('async function loadChatSession(id)'),source.indexOf('// ---- live stream layer')),ctx);
(async()=>{
 const loading=ctx.loadChatSession('source');await started;
 ctx.chatAgent='other';ctx.chatOpenId='destination';release();await loading;
 assert.equal(drafts.length,0,'navigating away during artifact load must not seed another agent draft');
 console.log('Artifact handoff navigation race passed');
})().catch(e=>{console.error(e);process.exitCode=1});
