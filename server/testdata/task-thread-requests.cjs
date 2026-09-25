// Task-thread posts carry a request ID remembered with the exact payload: a
// retry after a lost acknowledgment reuses it, a changed payload does not,
// and a confirmed post forgets it. Re-dispatch runs from the server's turn
// projection interleave with the thread as system lines.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/93-todo-panel.js'),'utf8');
const store=new Map();
const posts=[];
let failNext=false;
const ctx=vm.createContext({
  crypto:require('node:crypto').webcrypto,Uint8Array,Array,JSON,Date,
  localStorage:{getItem:k=>store.has(k)?store.get(k):null,setItem:(k,v)=>store.set(k,String(v)),removeItem:k=>store.delete(k)},
  postJSONOk:async(url,body)=>{posts.push(body);if(failNext){failNext=false;throw new Error('network: acknowledgment lost');}return {ok:true};},
});
vm.runInContext(src.slice(src.indexOf('async function postTaskThread('),src.indexOf('function todoInflightEntry(')),ctx);
(async()=>{
  const body={id:'inbox/a',text:'ask once',mentions:[],files:[],mode:'ask',agent:'agent:alfred',context:[]};
  failNext=true;
  await assert.rejects(ctx.postTaskThread(body));
  await ctx.postTaskThread(body);
  assert.equal(posts.length,2);
  assert.match(posts[0].requestId,/^[0-9a-f]{32}$/);
  assert.equal(posts[1].requestId,posts[0].requestId,'retry of the same payload reuses the request ID');
  assert.equal(store.size,0,'a confirmed post forgets its request');
  failNext=true;
  await assert.rejects(ctx.postTaskThread(body));
  await ctx.postTaskThread({...body,text:'ask twice'});
  assert.notEqual(posts[3].requestId,posts[2].requestId,'a changed payload is a new request');
  console.log('request IDs: reused on identical retry, fresh on change, forgotten on success');

  const d={supervision:{runs:[
    {requestId:'c-1',attempt:0,state:'disconnected',updated:'2026-09-25T10:00:01Z',evidence:'turn-open marker c-1: the process ended'},
    {requestId:'c-2',attempt:2,state:'ready_for_review',updated:'2026-09-25T10:05:00Z',evidence:'re-dispatch attempt 2 of 3'},
    {requestId:'c-3',attempt:0,state:'ready_for_review',updated:'2026-09-25T11:00:00Z',evidence:'answered first time'},
  ]}};
  const thread=[{id:'a',at:'2026-09-25T10:00:00Z'},{id:'b',at:'2026-09-25T10:06:00Z'}];
  const out=ctx.taskThreadWithRuns(d,thread,c=>'comment '+c.id,l=>'line '+l.text);
  assert.deepEqual(Array.from(out),['comment a','line turn interrupted','line re-dispatched after an interruption · attempt 2 · answered','comment b']);
  assert.deepEqual(Array.from(ctx.taskThreadWithRuns({},thread,c=>c.id,l=>l)),['a','b']);
  console.log('re-dispatch runs interleave by time; a first answered attempt adds nothing');
})().catch(e=>{console.error(e);process.exit(1);});
