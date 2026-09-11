const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const src=fs.readFileSync(require('node:path').join(__dirname,'../web/js/48-chat.js'),'utf8');
let state={revision:3,record_version:'first',value:{groups:{one:'Project'},contexts:{one:{instructions:'Keep me'}},priorities:{other:0}}},race=true,puts=0;
const clone=x=>JSON.parse(JSON.stringify(x));
const ctx=vm.createContext({chatWorkstreamURL:'/projects',chatApplyWorkstreams:s=>{state=s;},fetch:async(url,opts={})=>{
 assert.equal(url,'/projects');if(!opts.method)return {ok:true,json:async()=>clone(state)};
 puts++;const body=JSON.parse(opts.body);assert.equal(body.record_version,state.record_version);
 if(race){race=false;state.revision++;state.record_version='second';state.value.priorities.concurrent=2;return {status:409};}
 state={...body,revision:body.revision+1,record_version:'third'};return {ok:true,json:async()=>clone(state)};
}});
vm.runInContext(src.slice(src.indexOf('async function chatSetPriority('),src.indexOf('function chatInboxEntries(')),ctx);
(async()=>{
 await ctx.chatSetPriority('target',1,3);assert.equal(puts,2);assert.deepEqual(state.value.priorities,{other:0,concurrent:2,target:3});assert.equal(state.value.contexts.one.instructions,'Keep me');
 await assert.rejects(()=>ctx.chatSetPriority('target',1,0),/changed on another device/);assert.equal(state.value.priorities.target,3);
 await ctx.chatSetPriority('target',3,1);assert.equal(state.value.priorities.target,undefined);assert.equal(state.value.priorities.concurrent,2);
 console.log('PASS priority CAS merge, concurrent same-thread conflict, reset and unrelated context preservation');
})().catch(e=>{console.error(e);process.exitCode=1});
