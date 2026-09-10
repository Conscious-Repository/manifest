const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
let release;const wait=new Promise(r=>release=r),events=[];
const ctx=vm.createContext({chatDraftKey:'first',chatSyncedDrafts:new Map([['first',{refresh:async()=>{events.push('draft');await wait;}}]]),
 chatLoadDeliveryRecovery:async k=>events.push('recovery:'+k),chatReconcileAcceptedDrafts:async k=>events.push('reconcile:'+k),
 document:{getElementById:()=>({})},chatRenderDeliveryNotice:()=>events.push('paint')});
vm.runInContext(src.slice(src.indexOf('const chatRecoveryRefreshes='),src.indexOf('window.addEventListener("focus"')),ctx);
(async()=>{
 const first=ctx.chatRefreshCurrentDraft(),same=ctx.chatRefreshCurrentDraft();assert.equal(first,same,'focus and visibility share one refresh');
 ctx.chatDraftKey='other';release();await first;
 assert.deepEqual(events,['draft','recovery:first','reconcile:first'],'late refresh must not paint another conversation');
 ctx.chatDraftKey='first';await ctx.chatRefreshCurrentDraft();assert.equal(events.at(-1),'paint');
 console.log('Return-to-app recovery coalesces events and respects navigation');
})().catch(e=>{console.error(e);process.exitCode=1});
