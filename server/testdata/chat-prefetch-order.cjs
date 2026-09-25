const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('server/web/js/48-chat.js','utf8');
const pending=[];const row={id:'target',run:{state:'running'},activityOffset:200};
const ctx=vm.createContext({chatRouteVersion:1,chatAgent:'alfred',chatOpenId:'current',chatStageCache:new Map(),chatStageKey:(a,id)=>a+'/'+id,
 chatStageRemember:(key,v)=>ctx.chatStageCache.set(key,v),chatTermBase:id=>'/term/'+id,chatBaseFor:a=>'/chat/'+a,
 fetch:()=>new Promise(resolve=>pending.push(resolve)),chatTermFind:()=>row,chatTermApplyState:r=>r,chatTermOpenFrom:(id,se,d)=>({id,se,turns:d.turns})});
vm.runInContext(source.slice(source.indexOf('const chatPrefetching ='),source.indexOf('// chatTermLeave')),ctx);
const drain=async()=>{for(let i=0;i<10;i++)await Promise.resolve()};
(async()=>{
 for(const terminal of [false,true]){
  const agent=terminal?'codex':'alfred',key=agent+'/target',entry={agent,terminal,session:{id:'target'}};
  const payload=terminal?{turns:['older'],run:{state:'completed'},offset:10}:{session:{id:'target'},body:'older'};
  for(const newer of ['cache','navigation','opened']){
   ctx.chatStageCache.clear();ctx.chatAgent='alfred';ctx.chatOpenId='current';
   ctx.chatPrefetchEntry(entry);const respond=pending.pop();assert.ok(respond);
   if(newer==='cache')ctx.chatStageCache.set(key,{fresh:true});
   if(newer==='navigation')ctx.chatRouteVersion+=2;
   if(newer==='opened'){ctx.chatAgent=agent;ctx.chatOpenId='target';}
   respond({ok:true,json:async()=>payload});await drain();
   assert.equal(ctx.chatStageCache.has(key),newer==='cache',terminal+' '+newer);
   if(newer==='cache')assert.equal(ctx.chatStageCache.get(key).fresh,true);
   assert.deepEqual(row.run,{state:'running'});assert.equal(row.activityOffset,200);
  }
  ctx.chatOpenId='current';ctx.chatStageCache.clear();ctx.chatPrefetchEntry(entry);pending.pop()({ok:true,json:async()=>payload});await drain();
  assert.equal(ctx.chatStageCache.has(key),true,'current background intent still warms cache');
  if(terminal){assert.equal(ctx.chatStageCache.get(key).o.se.run.state,'completed');assert.deepEqual(row.run,{state:'running'});assert.equal(row.activityOffset,200);}
  ctx.chatStageCache.clear();ctx.chatAgent=agent;ctx.chatOpenId='target';ctx.chatPrefetchEntry(entry);assert.equal(pending.length,0,'open target is loaded through foreground path');
 }
 console.log('PASS: prefetch respects newer cache/navigation/open target and never mutates terminal registry evidence');
})().catch(e=>{console.error(e);process.exitCode=1});
