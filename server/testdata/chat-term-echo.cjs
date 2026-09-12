// chat-term-echo.cjs — the pending "delivered · waiting for the agent" echo
// of a terminal send (2026-09-12): it is skipped when the tail already landed
// the real turn while the delivery waited for the prompt, it is tagged with
// where the send began so the tail reconciles against everything since, and
// a genuine resend of the same text still echoes. Run: node server/testdata/chat-term-echo.cjs
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
let tail={turns:[],offset:1},paints=0;
const ctx=vm.createContext({
 fetch:async()=>({json:async()=>tail}),chatTermFind:()=>null,renderChatInboxRows(){},document:{querySelector:()=>null},
 chatQuestionPanel(){},chatTermPaintTurns:()=>paints++,chatTermRepaintHead(){},renderChatComposer(){},chatTermComposerSession:()=>({}),
 chatTermPaintStrip(){},chatTermScreenFetch(){},chatTermBase:id=>'/t/'+id,chatTermOpen:null,
});
vm.runInContext(slice('function chatTermLanded(o,text,since)','\n// Follow-ups remain editable'),ctx);
vm.runInContext(slice('async function chatTermTail(o)','\n// chatTermMerge'),ctx);
vm.runInContext(slice('function chatTermMerge(turns, tail)','\n// ---- send ----'),ctx);
const open=turns=>ctx.chatTermOpen={id:'s',se:{},turns,offset:0,planRevisions:{},proposals:[],planningOperations:[]};
(async()=>{
 // 1. the race: the tail landed the real turn (and the reply) before the send resolved
 let o=open([{who:'user',text:'Are there lessons here?',ts:'t1'},{who:'assistant',ts:'t2',blocks:[{t:'say',text:'I will audit.'}]}]);
 ctx.chatTermEcho('s','Are there lessons here?',{delivery:{id:'d1'}},0);
 assert.equal(o.turns.length,2,'no echo once the real turn already stands');assert.equal(paints,0);
 // 2. a genuine resend of the same text echoes, and the tail swaps the real turn in
 o=open([{who:'user',text:'continue',ts:'t1'},{who:'assistant',ts:'t2',blocks:[]}]);
 ctx.chatTermEcho('s','continue',{delivery:{id:'d2'}},2);
 assert.equal(o.turns.length,3);assert.equal(o.turns[2].pending,true);assert.equal(o.turns[2].since,2);assert.equal(paints,1);
 tail={turns:[{who:'user',text:'continue',ts:'t3'}],offset:2};await ctx.chatTermTail(o);
 assert.deepEqual(o.turns.map(t=>[t.who,!!t.pending]),[['user',false],['assistant',false],['user',false]],'the landed resend replaces its echo');
 // 3. the ordinary path: echo first, the real turn and reply arrive later
 o=open([]);ctx.chatTermEcho('s','hello',{},0);assert.equal(o.turns[0].pending,true);
 tail={turns:[{who:'assistant',ts:'t1',blocks:[{t:'say',text:'thinking'}]}],offset:3};await ctx.chatTermTail(o);
 assert.equal(o.turns.filter(t=>t.pending).length,1,'an unrelated tail keeps the echo');
 tail={turns:[{who:'user',text:'hello',ts:'t2'},{who:'assistant',ts:'t3',blocks:[{t:'say',text:'hi'}]}],offset:4};await ctx.chatTermTail(o);
 assert.deepEqual(o.turns.map(t=>[t.who,!!t.pending]),[['assistant',false],['user',false],['assistant',false]],'the echo falls when its turn lands');
 // 4. the same text sent earlier in the thread never satisfies a later echo
 o=open([{who:'user',text:'ok',ts:'t1'},{who:'assistant',ts:'t2',blocks:[]}]);
 ctx.chatTermEcho('s','ok',{},2);assert.equal(o.turns.length,3,'an older identical turn does not stand in for this send');
 tail={turns:[{who:'assistant',ts:'t4',blocks:[]}],offset:5};await ctx.chatTermTail(o);
 assert.equal(o.turns.filter(t=>t.pending).length,1,'and the tail keeps waiting for it');
 console.log('PASS: terminal echo skips an already-landed turn, tags its origin, and the tail reconciles against everything since the send.');
})().catch(e=>{console.error(e);process.exit(1);});
