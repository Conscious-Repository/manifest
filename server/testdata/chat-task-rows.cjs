// chat-task-rows.cjs — task conversations are inbox entries of the CHAT rail
// (2026-09-21): keyed apart from sessions, titled by the task's words, held
// by their assignee, sorted by their newest comment, stated by their
// delegation, and filed under the project named like their domain.
// Run: node server/testdata/chat-task-rows.cjs
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
const ctx=vm.createContext({
 chatSessions:[],chatRoster:[{name:'alfred',enabled:true}],chatAgentSessions:{alfred:[{id:'s1',title:'a session',updated:'2026-09-20T20:00:00Z'}]},
 chatTermEnabled:false,chatTermKinds:{},chatTermList:()=>[],chatHasNativeParent:()=>false,chatHasCanonicalParent:()=>false,chatIsTerm:()=>false,
 chatSearchQuery:'',chatLifecycle:{},chatLifecycleFilter:'active',chatWorkstreamFilter:'all',chatInboxFilter:'all',chatAttentionFilter:'all',
 chatReviewTaskStatus:{},terminalStates:new Map(),chatPins:{},chatWorkstreams:{priorities:{},groups:{ws1:'manifest'},members:{}},chatWorkstreamMember:()=>'',chatReviewStatus:{},chatAgentLabel:a=>a,
 chatTaskThreads:[
  {id:'manifest/consider-opencode',title:'consider if opencode would be valuable?',domain:'manifest',agent:'agent:alfred',updated:'2026-09-21T04:00:00Z',comments:3,lastAuthor:'Benjamin',state:'plan-running',phase:'plan'},
  {id:'personal/older',title:'older task',domain:'personal',agent:'',updated:'2026-09-19T04:00:00Z',comments:1,lastAuthor:'Benjamin'},
 ],
});
vm.runInContext(slice('function chatInboxKey(entry)','\nfunction chatApplyPins'),ctx);
vm.runInContext(slice('function chatEntryState(entry)','\nasync function chatSetPriority'),ctx);
vm.runInContext(slice('function chatInboxEntries()','\nfunction renderChatInboxRows'),ctx);
const entries=ctx.chatInboxEntries();
assert.equal(entries.length,3,'two task conversations join the one session');
assert.equal(entries[0].taskThread,true);assert.equal(entries[0].session.title,'consider if opencode would be valuable?');
assert.equal(entries[0].agent,'alfred','the assignee token loses its agent: prefix');
assert.equal(ctx.chatInboxKey(entries[0]),'task:alfred/manifest/consider-opencode','task rows key apart from sessions');
assert.equal(entries[1].session.id,'s1','sorted by newest activity: task · session · task');
assert.equal(entries[2].session.id,'personal/older');
const st=ctx.chatEntryState(entries[0]);assert.equal(st.execution,'running');assert.equal(st.label,'Working (plan)');
const idle=ctx.chatEntryState(entries[2]);assert.equal(idle.execution,'unknown');assert.equal(idle.label,'Benjamin · Idle');
// the attention filter sees a working task as running
ctx.chatAttentionFilter='running';assert.deepEqual(ctx.chatInboxEntries().map(e=>e.session.id),['manifest/consider-opencode']);
ctx.chatAttentionFilter='all';
// search reaches the task's words
ctx.chatSearchQuery='opencode';assert.equal(ctx.chatInboxEntries().length,1);ctx.chatSearchQuery='';
console.log('PASS: task conversations are rail entries — keyed, titled, held, sorted, stated and searchable like sessions.');
