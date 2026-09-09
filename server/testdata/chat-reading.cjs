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
// A native task uses comment identity, not list position; earlier new comments
// do not change which message the bookmark restores.
const taskView=viewport(0,1800,400);
taskView.querySelectorAll()[0].dataset.chatReadTurn='comment:stable-id';
assert.equal(ctx.chatRestoreReadingPosition(taskView,{following:false,turn:'comment:stable-id',fraction:.25}),true);
assert.equal(taskView.scrollTop,1900);
let taskSaved;
taskView.dataset={readKey:'task-conversation'};
Object.assign(ctx,{document:{getElementById:()=>taskView},els:{chatView:{hidden:false}},chatIsTerm:()=>true,chatTaskID:'task-with-coding-assignee',chatReadingStates:new Map([['task-conversation',{set:v=>taskSaved=v}]])});
ctx.chatSaveReadingPosition();assert.equal(taskSaved.turn,'comment:stable-id');
ctx.chatTaskID='';ctx.els.chatView.hidden=true;taskSaved=null;ctx.chatSaveReadingPosition();assert.equal(taskSaved,null,'a hidden chat must not save a bookmark from the terminal page');
// Rendering/autopin scrolls do not overwrite a stored bookmark. A user gesture
// arms capture only for the ensuing scroll events.
const listeners={},scrollHost={scrollTop:100,scrollHeight:2000,clientHeight:500,addEventListener:(name,fn)=>listeners[name]=fn};
let saves=0;
Object.assign(ctx,{document:{getElementById:()=>scrollHost},chatScrollBound:false,chatReadingGestureUntil:0,chatSaveReadingPosition:()=>saves++,Date:{now:()=>10000}});
vm.runInContext(src.slice(src.indexOf('function bindChatScroll()'),src.indexOf('function chatPin()')),ctx);
ctx.bindChatScroll();listeners.scroll();assert.equal(saves,0);
listeners.wheel();scrollHost.scrollTop=75;listeners.scroll();assert.equal(saves,1);
// A transcript rebuild temporarily collapses the scrolling container. Preserve
// the reader's position, while Latest continues to follow appended output.
const terminalHost={scrollTop:740};
const terminalBody={set innerHTML(value){terminalHost.scrollTop=0;}};
Object.assign(ctx,{document:{getElementById:id=>id==='chatTermTurns'?terminalBody:terminalHost},chatTermOpen:{turns:[{id:'record-1',who:'user',text:'hello'}]},chatStick:false,chatTermPaintLines:()=>{},chatPin:()=>{if(ctx.chatStick)terminalHost.scrollTop=2000;}});
vm.runInContext(src.slice(src.indexOf('function chatTermPaintTurns()'),src.indexOf('\nfunction ',src.indexOf('function chatTermPaintTurns()')+1)),ctx);
ctx.chatTermPaintTurns();assert.equal(terminalHost.scrollTop,740);
ctx.chatStick=true;ctx.chatTermPaintTurns();assert.equal(terminalHost.scrollTop,2000);
vm.runInContext(src.slice(src.indexOf('function chatTermMerge('),src.indexOf('// ---- send ----',src.indexOf('function chatTermMerge('))),ctx);
const turns=[{id:'first-record',who:'assistant',blocks:[{t:'say',text:'Starting'}]}];
ctx.chatTermMerge(turns,[{id:'later-record',who:'assistant',blocks:[{t:'say',text:'Finished'}]}]);
assert.equal(turns.length,1);assert.equal(turns[0].id,'first-record');assert.equal(turns[0].blocks.length,2);
console.log('Reading anchors, terminal tail position, and Latest are safe');
// Continued native turns retain stable bookmarks and trigger source-chat polls.
taskView.querySelectorAll()[0].dataset.chatReadTurn='terminal:session:record';
assert.equal(ctx.chatRestoreReadingPosition(taskView,{following:false,turn:'terminal:session:record',fraction:.5}),true);
assert.equal(taskView.scrollTop,2000);
for(const name of ['chatTranscriptSignature','chatHasCanonicalParent']){
 const start=src.indexOf('function '+name+'(');
 vm.runInContext(src.slice(start,src.indexOf('\n}',start)+2),ctx);
}
const transcript={session:{updated:'unchanged',status:'idle'},continuations:[{id:'native',turns:[]}]};
const beforeNative=ctx.chatTranscriptSignature(transcript);
transcript.continuations[0].turns.push({id:'reply',text:'New coding reply'});
assert.notEqual(ctx.chatTranscriptSignature(transcript),beforeNative);
ctx.chatAgentSessions={alfred:[{id:'parent'}]};
assert.equal(ctx.chatHasCanonicalParent({origin:{mode:'continue',agent:'alfred',id:'parent'}}),true);
assert.equal(ctx.chatHasCanonicalParent({origin:{mode:'related',agent:'alfred',id:'parent'}}),false);
assert.equal(ctx.chatHasCanonicalParent({origin:{mode:'continue',agent:'alfred',id:'missing'}}),false);
