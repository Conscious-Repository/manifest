const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const src=fs.readFileSync(require('node:path').join(__dirname,'../web/js/48-chat.js'),'utf8');
let state={revision:0,value:{seen:{other:{marker:'keep',at:1}}}},puts=0,race=true;
const host={clientHeight:100,scrollHeight:500,scrollTop:400};
const ctx=vm.createContext({Date,Set,JSON,chatIsPortal:()=>false,chatIsTerm:()=>true,chatOpenId:'one',chatAgent:'codex',chatTermFind:()=>({id:'one',activityOffset:20}),chatInboxKey:()=> 'terminal:codex/one',document:{hidden:false,getElementById:()=>host,querySelectorAll:()=>[]},fetch:async(url,opts={})=>{
 if(!opts.method)return {ok:true,json:async()=>structuredClone(state)};
 puts++;if(race){race=false;state.revision++;state.value.seen.concurrent={marker:'other',at:1};return {status:409};}
 const body=JSON.parse(opts.body);assert.equal(body.revision,state.revision);state={revision:state.revision+1,value:body.value};return {ok:true,json:async()=>structuredClone(state)};
}});
vm.runInContext(src.slice(src.indexOf('let chatSeen='),src.indexOf('let chatPins=')),ctx);
(async()=>{await ctx.chatMarkViewed();assert.equal(puts,2);assert.equal(state.value.seen.other.marker,'keep');assert.ok(state.value.seen.concurrent);assert.ok(state.value.seen['terminal:codex/one']);await ctx.chatMarkViewed();assert.equal(puts,2);host.scrollTop=0;await ctx.chatMarkViewed();assert.equal(puts,2);ctx.document.hidden=true;host.scrollTop=400;await ctx.chatMarkViewed();assert.equal(puts,2);assert.notEqual(ctx.chatActivityMarker({activityOffset:1}),ctx.chatActivityMarker({activityOffset:2}));console.log('PASS seen marker merge, concurrency, deduplication and visible-latest gating');})().catch(e=>{console.error(e);process.exitCode=1});
