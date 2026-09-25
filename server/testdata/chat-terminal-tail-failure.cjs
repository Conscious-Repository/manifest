const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
let response,timeout,resolve,reads=0;
const root={id:'one',offset:12,turns:[{who:'assistant',text:'Retained result'}],se:{backend:'herdr',run:{state:'completed'}},live:false,planningTimeline:['retained'],questions:['retained']};
const ctx=vm.createContext({chatTermReadHealth(){},AbortController,chatTermOpen:root,chatTermTailing:false,chatTermBase:id=>'/term/'+id,
 setTimeout:fn=>{timeout=fn;return 1},clearTimeout:()=>{timeout=null},
 fetch:async(_url,opts)=>{reads++;if(response)return response;return new Promise((yes,no)=>{resolve=yes;opts.signal.addEventListener('abort',()=>no(Error('timeout')))});},
 chatTermFind:()=>null,document:{querySelector:()=>null},renderChatInboxRows(){},chatQuestionPanel(){},chatTermPaintTurns(){},chatTermRepaintHead(){}
});
vm.runInContext(source.slice(source.indexOf('async function chatTermRequestFinalTail('),source.indexOf('// chatTermMerge —')),ctx);
(async()=>{
 for(const bad of [{ok:false,json:async()=>({})},{ok:true,json:async()=>null},{ok:true,json:async()=>({offset:-1})},{ok:true,json:async()=>({offset:12,turns:{bad:true}})},{ok:true,json:async()=>{throw Error('bad json')}}]){
  response=bad;const before=JSON.stringify(root);await ctx.chatTermRequestFinalTail(root);
  assert.equal(JSON.stringify(root),before,'failed reads preserve all retained evidence');assert.equal(ctx.chatTermTailing,false);assert.equal(timeout,null);
 }
 response=null;const beforeReads=reads,first=ctx.chatTermRequestFinalTail(root);
 await ctx.chatTermRequestFinalTail(root);assert.equal(root.finalTailPending,true);assert.equal(reads,beforeReads+1,'final read waits behind active read');
 timeout();await first;
 assert.equal(reads,beforeReads+2,'timeout releases queued final read');
 resolve({ok:true,json:async()=>({offset:12,turns:[],run:{state:'completed'},planningTimeline:['final reply']})});
 for(let i=0;i<12;i++)await Promise.resolve();
 assert.equal(ctx.chatTermTailing,false);assert.deepEqual(root.planningTimeline,['final reply']);assert.equal(root.turns[0].text,'Retained result');assert.equal(timeout,null);
 console.log('PASS: failed terminal tails preserve evidence; stalled reads release serialized final-read recovery');
})().catch(e=>{console.error(e);process.exitCode=1});
