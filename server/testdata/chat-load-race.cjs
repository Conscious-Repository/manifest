const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
let release,requested;
const pending=new Promise(r=>release=r),started=new Promise(r=>requested=r),drafts=[];
const ctx=vm.createContext({chatIsTerm:()=>false,chatAgent:'alfred',chatOpenId:'source',chatBase:()=>'/chat/'+ctx.chatAgent,
 els:{chatView:{hidden:false}},chatPrepareDraft:async(...args)=>drafts.push(args),chatPrepareReadingPosition:async()=>{},
 fetch:async url=>{
  if(url.startsWith('/api/artifacts/')){requested();await pending;return {ok:true,json:async()=>({title:'Plan',revisions:[]})};}
  return {ok:true,json:async()=>({conversation:{key:'original'},session:{turns:0,origin:{task:'task',prompt:'private handoff',artifacts:[{id:'plan',revision:'v1'}]}}})};
 }});
vm.runInContext(source.slice(source.indexOf('async function loadChatSession(id)'),source.indexOf('// ---- live stream layer')),ctx);
(async()=>{
 const loading=ctx.loadChatSession('source');await started;
 ctx.chatAgent='other';ctx.chatOpenId='destination';release();await loading;
 assert.equal(drafts.length,0,'navigating away during artifact load must not seed another agent draft');
 // An idle private chat must start observing approvals even with no queued run.
 let polled=0;
 Object.assign(ctx,{chatAgent:'alfred',chatOpenId:'source',
  fetch:async()=>({ok:true,json:async()=>({conversation:{key:'original'},session:{id:'source',status:'idle'},proposals:[]})}),
  document:{querySelector:()=>null},chatRemember(){},chatConversationTasks:new Map(),
  renderChatTranscript(){},renderChatComposer(){},chatPendingWorkspace:null,ensureChatStream(){},
  ensureChatPoll:(session,queued)=>{assert.equal(session.status,'idle');assert.equal(queued,0);polled++;}});
 await ctx.loadChatSession('source');assert.equal(polled,1,'idle chat did not observe approvals');
 vm.runInContext(source.slice(source.indexOf('function chatTranscriptSignature(d)'),source.indexOf('function ensureChatPoll(session')),ctx);
 const idle={session:{id:'source',status:'idle',updated:'unchanged'},proposals:[]};
 const before=ctx.chatTranscriptSignature(idle);
 idle.proposals=[{id:'approval',body:'Review this'}];const pendingSig=ctx.chatTranscriptSignature(idle);
 assert.notEqual(before,pendingSig,'new approval must trigger repaint without a new message');
 idle.proposals[0].body='Updated evidence';assert.notEqual(pendingSig,ctx.chatTranscriptSignature(idle));
 idle.proposals=[];assert.equal(before,ctx.chatTranscriptSignature(idle),'settlement returns to no pending approvals');
 idle.codingResults=[{id:'run',agent:'codex',body:'Delivered',outcome:'completed'}];
 const resultSig=ctx.chatTranscriptSignature(idle);assert.notEqual(before,resultSig,'coding result must refresh idle planning chat');
 idle.codingResults[0].body='Corrected deliverable';assert.notEqual(resultSig,ctx.chatTranscriptSignature(idle));
 // An opened coding result keeps the source and hash even if navigation occurs
 // while the immutable snapshot is being retained.
 const node=(tag,cls,text)=>({tag,cls,text,dataset:{},children:[],append(...items){this.children.push(...items);}});
 let finishSnapshot,opened=0,captured;
 Object.assign(ctx,{chatAgent:'alfred',chatOpenId:'source',chatRouteVersion:1,el:node,chatAgentLabel:x=>x,
  chatTaskThreadHash:x=>'#/chat/task/'+x,chatBaseFor:x=>'/agents/'+x,
  renderMarkdown:x=>node('markdown','',x),postJSONOk:async(url,payload)=>{captured={url,payload};return new Promise(r=>finishSnapshot=r);},
  chatOpenWorkingArtifact:()=>opened++,showToast:message=>{throw Error(message);}});
 vm.runInContext(source.slice(source.indexOf('function chatPaintCodingResults('),source.indexOf('function renderChatTranscript(')),ctx);
 const host=node('div','','');ctx.chatPaintCodingResults(host,[{id:'run-1',agent:'codex',task:'task-1',hash:'original-hash',body:'Original'}]);
 const button=host.children[0].children.find(x=>x.tag==='button');const opening=button.onclick();
 assert.equal(captured.payload.hash,'original-hash');assert.equal(captured.url,'/agents/alfred/source/coding-result');
 ctx.chatRouteVersion=2;ctx.chatOpenId='other';finishSnapshot({id:'artifact',revision:'original-hash',task:'task-1'});await opening;
 assert.equal(opened,0,'snapshot completion opened in unrelated chat');
 console.log('Artifact handoff navigation race passed');
})().catch(e=>{console.error(e);process.exitCode=1});
