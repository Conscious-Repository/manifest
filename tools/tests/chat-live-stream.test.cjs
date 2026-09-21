const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const src=fs.readFileSync('server/web/js/48-chat.js','utf8');
const stream=src.slice(src.indexOf('let chatES ='),src.indexOf('async function refetchChatSession'));
test('Hermes reuses live events and falls back to polling; agent identity fences callbacks',()=>{
 const sources=[],calls=[];
 const c={chatAgent:'alfred',chatOpenId:'same',chatIsPortal:()=>false,chatIsTerm:()=>false,chatBase:()=>'/api/agents/chat/'+c.chatAgent+'/sessions',renderChatLive:()=>{},scheduleChatReveal:()=>{},finishChatLive:()=>{},loadChatSessions:async()=>{},renderChatRail:()=>{},refetchChatSession:()=>{},ensureChatPoll:()=>calls.push('poll'),EventSource:class {constructor(url){this.url=url;this.handlers={};sources.push(this)} addEventListener(k,f){this.handlers[k]=f} close(){this.closed=true}}};
 vm.createContext(c);vm.runInContext(stream,c);
 c.ensureChatStream({id:'same'});
 assert.equal(sources[0].url,'/api/agents/chat/alfred/sessions/same/stream?after=-1');
 const emit=(es,k,data={})=>es.handlers[k]({data:JSON.stringify({data})});
 emit(sources[0],'turn.started');emit(sources[0],'thinking.delta',{text:'reasoning'});emit(sources[0],'thinking.tokens',{tokens:17});emit(sources[0],'assistant.delta',{text:'hello'});
 assert.equal(vm.runInContext('chatLive.thinking',c),'reasoning');assert.equal(vm.runInContext('chatLive.thinkTok',c),17);assert.equal(vm.runInContext('chatLive.say',c),'hello');
 c.chatAgent='profile';c.ensureChatStream({id:'same'});assert.ok(sources[0].closed);
 emit(sources[0],'turn.started');assert.equal(vm.runInContext('chatLive',c),null);
 sources[1].onerror();assert.deepEqual(calls,['poll']);assert.ok(sources[1].closed);
});
test('recorded Hermes reasoning uses existing think block with reported tokens',()=>{
 const c={chatAgent:'alfred',chatIsPortal:()=>false,parseChatSteps:()=>[{cast:'thinking',body:'- tokens: 73\n\nUsage only'}]};
 vm.createContext(c);vm.runInContext(src.slice(src.indexOf('function chatTurnBlocks('),src.indexOf('// chatBlockEl')),c);
 const block=c.chatTurnBlocks({text:'fixture'})[0];assert.equal(block.t,'think');assert.equal(block.tokens,73);assert.equal(block.text,'Usage only');
 c.chatAgent='';assert.equal(c.chatTurnBlocks({text:'fixture'})[0].t,'step');
});
