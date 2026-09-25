const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
const requests=[];let response;
const ctx=vm.createContext({AbortController,setTimeout,clearTimeout,chatTermReadHealth(){},chatQuestionPanel(){},chatTermFind:()=>null,renderChatInboxRows(){},document:{querySelector:()=>null},chatTermBase:()=>'/terminal/one',chatTermPaintTurns(){},chatTermRepaintHead(){},chatTermPaintStrip(){},renderChatComposer(){},chatTermComposerSession:()=>({}),chatTermScreenFetch(){},
 fetch:async url=>{requests.push(url);return{ok:true,json:async()=>structuredClone(response)}}});
vm.runInContext(source.slice(source.indexOf('async function chatTermTail('),source.indexOf('// ---- send ----')),ctx);
vm.runInContext(source.slice(source.indexOf('function chatTermLanded('),source.indexOf('// Follow-ups remain editable')),ctx);
(async()=>{
 for(const offset of [5,100,200]){
  const pending={id:'pending:receipt',who:'user',text:'Repeat request',pending:true,since:1,ts:new Date().toISOString()};
  const o=ctx.chatTermOpen={id:'one',se:{backend:'herdr'},offset:100,historyAvailable:false,turns:[{who:'assistant',text:'Old file content'},pending],title:'Old title',cost:12,live:false};
  response={historyAvailable:false,offset:0,turns:[]};await ctx.chatTermTail(o);
  assert.equal(requests.at(-1),'/terminal/one/transcript?after=0');assert.equal(o.offset,100);assert.equal(o.turns.length,2);
  response={historyAvailable:true,offset,turns:[{id:'prior-user',who:'user',text:'Repeat request'}],title:'',cost:0};await ctx.chatTermTail(o);
  assert.equal(requests.at(-1),'/terminal/one/transcript?after=0');assert.equal(o.offset,offset);assert.equal(o.title,'');assert.equal(o.cost,0);
  assert.equal(o.turns.some(t=>t.text==='Old file content'),false,'replacement removes prior file content at every size');
  assert.equal(o.turns[1].id,'pending:receipt');assert.equal(o.turns[1].since,1,'new history establishes pending echo boundary');
  assert.equal(o.turns[1].pending,true,'old identical text cannot acknowledge a pending send');
  response={historyAvailable:true,offset:offset+20,turns:[{id:'new-user',who:'user',text:'Repeat request'}]};await ctx.chatTermTail(o);
  assert.equal(requests.at(-1),'/terminal/one/transcript?after='+offset);assert.equal(o.turns.some(t=>t.pending),false,'subsequent tail can reconcile the pending echo');
 }
 console.log('PASS: unavailable history recovers through a full snapshot at smaller/equal/larger sizes, retaining unconfirmed echoes');
})().catch(e=>{console.error(e);process.exitCode=1});
