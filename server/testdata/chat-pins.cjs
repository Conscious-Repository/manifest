const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const src=fs.readFileSync(require('node:path').join(__dirname,'../web/js/48-chat.js'),'utf8');
let state={key:'inbox',slot:'pins',revision:0,value:null},race=true,puts=0;
const clone=x=>JSON.parse(JSON.stringify(x));
const context=vm.createContext({fetch:async(url,options={})=>{
 assert.equal(url,'/api/chat/state/inbox/pins');
 if(options.method!=='PUT')return{ok:true,json:async()=>clone(state)};
 puts++;const body=JSON.parse(options.body);
 if(race){race=false;state={...state,revision:1,value:{pins:{'agent:alfred/other':true}}};return{status:409};}
 assert.equal(body.revision,state.revision);
 state={...state,revision:state.revision+1,value:body.value};return{ok:true,json:async()=>clone(state)};
}});
vm.runInContext(src.slice(src.indexOf('let chatPins='),src.indexOf('function chatInboxEntries()')),context);
(async()=>{
 await context.chatSetPinned('terminal:codex/native',true);
 assert.equal(puts,2);assert.equal(state.value.pins['agent:alfred/other'],true);assert.equal(state.value.pins['terminal:codex/native'],true);
 await context.chatSetPinned('terminal:codex/native',false);assert.equal(state.value.pins['terminal:codex/native'],undefined);assert.equal(state.value.pins['agent:alfred/other'],true);
 context.chatApplyPins({key:'inbox',slot:'pins',revision:0,value:{pins:{stale:true}}});
 assert.equal(vm.runInContext('chatPins.stale',context),undefined);
 assert.notEqual(context.chatInboxKey({terminal:true,agent:'codex',session:{id:'same'}}),context.chatInboxKey({agent:'codex',session:{id:'same'}}));
 console.log('Pin persistence merge, concurrent update retry, unpin and stale-read protection passed');
})().catch(e=>{console.error(e);process.exitCode=1});
