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
  {id:'manifest/ticked',title:'ticked off, still planning',domain:'manifest',agent:'agent:alfred',updated:'2026-09-21T03:30:00Z',comments:4,lastAuthor:'Alfred',state:'plan-running',phase:'plan',open:false},
  {id:'manifest/finished',title:'ticked off and quiet',domain:'manifest',agent:'agent:alfred',updated:'2026-09-21T03:00:00Z',comments:2,lastAuthor:'Alfred',state:'done',open:false},
 ],
});
vm.runInContext(slice('function chatInboxKey(entry)','\nfunction chatApplyPins'),ctx);
vm.runInContext(slice('function chatEntryState(entry)','\nasync function chatSetPriority'),ctx);
vm.runInContext(slice('function chatInboxEntries()','\nfunction renderChatInboxRows'),ctx);
const entries=ctx.chatInboxEntries();
assert.equal(entries.length,4,'the quiet ticked-off task files itself under Archived; the rest join the one session');
assert.equal(entries.some(e=>e.session.id==='manifest/finished'),false);
assert.equal(entries[1].session.id,'manifest/ticked','ticked off but an agent turn is in flight: still in Chats');
assert.equal(entries[0].taskThread,true);assert.equal(entries[0].session.title,'consider if opencode would be valuable?');
assert.equal(entries[0].agent,'alfred','the assignee token loses its agent: prefix');
assert.equal(ctx.chatInboxKey(entries[0]),'task:alfred/manifest/consider-opencode','task rows key apart from sessions');
assert.equal(entries[2].session.id,'s1','sorted by newest activity: task · task · session · task');
assert.equal(entries[3].session.id,'personal/older');
// the archive holds the quiet ticked-off task; an explicit active brings it back
ctx.chatLifecycleFilter='archived';assert.deepEqual(ctx.chatInboxEntries().map(e=>e.session.id),['manifest/finished']);
ctx.chatLifecycle['task:alfred/manifest/finished']='active';ctx.chatLifecycleFilter='active';
assert.equal(ctx.chatInboxEntries().some(e=>e.session.id==='manifest/finished'),true,'Restore to chats writes an explicit active that wins');
delete ctx.chatLifecycle['task:alfred/manifest/finished'];
const done=ctx.chatEntryState(ctx.chatTaskEntry(ctx.chatTaskThreads[3]));assert.equal(done.execution,'completed');assert.equal(done.label,'Done · last Alfred');
const st=ctx.chatEntryState(entries[0]);assert.equal(st.execution,'running');assert.equal(st.label,'Working (plan)');
const idle=ctx.chatEntryState(entries[3]);assert.equal(idle.execution,'unknown');assert.equal(idle.label,'Benjamin · Idle');
// the attention filter sees a working task as running
ctx.chatAttentionFilter='running';assert.deepEqual(ctx.chatInboxEntries().map(e=>e.session.id),['manifest/consider-opencode','manifest/ticked']);
ctx.chatAttentionFilter='all';
// search reaches the task's words
ctx.chatSearchQuery='opencode';assert.equal(ctx.chatInboxEntries().length,1);ctx.chatSearchQuery='';
console.log('PASS: task conversations are rail entries — keyed, titled, held, sorted, stated and searchable like sessions.');
