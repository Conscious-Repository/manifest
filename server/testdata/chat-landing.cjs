const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const prepared=[];
const ctx=vm.createContext({crypto:require('node:crypto').webcrypto,TextEncoder,Uint8Array,
 chatPrepareDraft:async(descriptor,key)=>prepared.push({descriptor,key}),showToast:message=>{throw Error(message);}});
vm.runInContext(src.slice(src.indexOf('async function chatPrepareLandingDraft('),src.indexOf('async function chatPrepareDraft(')),ctx);
(async()=>{
 await ctx.chatPrepareLandingDraft('alfred');await ctx.chatPrepareLandingDraft('zeck');await ctx.chatPrepareLandingDraft('alfred');
 assert.equal(prepared[0].key,'alfred/new');
 assert.match(prepared[0].descriptor.key,/^landing-[a-f0-9]{32}$/);
 assert.notEqual(prepared[0].descriptor.key,prepared[1].descriptor.key);
 assert.equal(prepared[0].descriptor.key,prepared[2].descriptor.key,'same landing must restore the same device-independent slot');
 await ctx.chatPrepareLandingDraft('');assert.equal(prepared[3].key,'spirits/new');
 // Do not paint the old landing after draft retrieval resolves on another route.
 let release,started;
 const gate=new Promise(r=>release=r),entered=new Promise(r=>started=r);
 Object.assign(ctx,{chatRouteVersion:1,chatAgent:'alfred',chatOpenId:'',els:{chatView:{hidden:false}},
 chatPrepareLandingDraft:async()=>{started();await gate;},document:{getElementById(){throw Error('painted stale landing');}}});
 vm.runInContext(src.slice(src.indexOf('async function renderChatLanding()'),src.indexOf('function focusChatInput()')),ctx);
 const loading=ctx.renderChatLanding();await entered;ctx.chatRouteVersion=2;ctx.chatAgent='zeck';release();await loading;
 console.log('Landing drafts have stable isolated identities and safe navigation');
})().catch(e=>{console.error(e);process.exitCode=1});
