const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const ctx=vm.createContext({chatLastY:0,chatStick:true});
vm.runInContext(src.slice(src.indexOf('function chatReadingAnchor(host)'),src.indexOf('window.addEventListener("pagehide",()=>{for(const saved of chatReadingStates')),ctx);
function viewport(scrollTop,start,height){
 const host={scrollTop,scrollHeight:3000,clientHeight:500,getBoundingClientRect:()=>({top:100})};
 const row={dataset:{chatReadTurn:'7'},getBoundingClientRect:()=>({top:100+start-host.scrollTop,bottom:100+start-host.scrollTop+height,height})};
 host.querySelectorAll=()=>[row];return host;
}
// Desktop: reading a quarter of the way into turn seven.
const desktop=viewport(550,500,200),saved=ctx.chatReadingAnchor(desktop);
assert.equal(saved.turn,'7');assert.equal(saved.fraction,.25);assert.equal(saved.following,false);
// On a narrower layout both preceding content and this turn grow.
const phone=viewport(0,900,800);
assert.equal(ctx.chatRestoreReadingPosition(phone,saved),true);
assert.equal(phone.scrollTop,1100);assert.equal(ctx.chatStick,false);
assert.equal(ctx.chatReadingAnchor(phone).fraction,.25);
// Missing or invalid anchors do not jump into an unrelated message.
const before=phone.scrollTop;
assert.equal(ctx.chatRestoreReadingPosition(phone,{following:false,turn:'missing',fraction:.5}),false);
assert.equal(phone.scrollTop,before);
assert.equal(ctx.chatRestoreReadingPosition(phone,{following:false,turn:'7',fraction:Infinity}),true);
assert.equal(phone.scrollTop,900);
phone.scrollTop=2500;assert.equal(ctx.chatReadingAnchor(phone).following,true);
assert.equal(ctx.chatRestoreReadingPosition(phone,{following:true}),false);
console.log('Reading anchors survive viewport changes; missing anchors and Latest are safe');
