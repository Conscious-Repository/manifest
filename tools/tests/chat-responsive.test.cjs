const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const src=fs.readFileSync('server/web/js/48-chat.js','utf8');
test('stream paints all received text, skips unchanged frames, and retains throttled updates',()=>{
 let tick,paints=0,time=120,cleared=false;
 const c={chatRevealTimer:null,chatLastMd:0,chatLive:{say:'A'.repeat(5000),revealed:0,thinking:'',tools:[],open:true},Date:{now:()=>time},setInterval:fn=>{tick=fn;return 1},clearInterval:()=>cleared=true,renderChatLive:()=>paints++};
 vm.createContext(c);vm.runInContext(src.slice(src.indexOf('function scheduleChatReveal()'),src.indexOf('function renderChatLive()')),c);
 c.scheduleChatReveal();tick();assert.equal(c.chatLive.revealed,5000);assert.equal(paints,1);
 time=240;tick();assert.equal(paints,1,'idle frames do not rebuild markdown');
 c.chatLive.say+='new';time=250;tick();assert.equal(paints,2);
 c.chatLive.say+=' pending';time=280;tick();assert.equal(paints,2);
 time=370;tick();assert.equal(c.chatLive.revealed,c.chatLive.say.length);assert.equal(paints,3,'throttled text is eventually painted');
 c.chatLive.open=false;tick();assert.equal(cleared,true);assert.equal(paints,4);
});
test('slow delivery recovery does not hold transcript polling',async()=>{
 let recoveries=0,tails=0,finish;
 const c={chatDraftKey:'codex/one',chatTermOpen:{id:'one'},chatIsTerm:()=>true,chatOpenId:'one',els:{chatView:{hidden:false}},document:{hidden:false,getElementById:()=>null},chatTermTailing:false,chatTermTail:async()=>tails++,chatLoadDeliveryRecovery:()=>{recoveries++;return new Promise(r=>finish=r)},chatTermLeave:()=>{},chatTermRequestFinalTail:()=>{}};
 vm.createContext(c);vm.runInContext(src.slice(src.indexOf('async function chatTermTick()'),src.indexOf('// Serialize the stop-triggered final read')),c);
 await c.chatTermTick();assert.equal(c.chatTermTailing,false);await c.chatTermTick();assert.equal(tails,2);assert.equal(recoveries,1,'only one receipt refresh per conversation');finish();
});
