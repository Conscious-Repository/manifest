// chat-now.cjs — the Now projection (49-chat-now.js) over the rail's own
// entries and chatEntryState, in node's vm with no DOM and no network:
//   1. a row files once, in priority order: Waiting on you (needs input,
//      approval, failed, disconnected) · Working (running, queued) · Ready to
//      review (outputs ready, or a finished run with activity since the owner
//      last looked) · Pinned (the existing pin preference); idle, draft,
//      unknown, archived and deleted rows stay out; newest first in a group;
//   2. each row says the state chatEntryState says, word for word (queued,
//      working, interruption requested, failed, disconnected, needs input and
//      plan approval stay distinct), with a next action or delivery summary
//      taken from the delivery/review/task record, and its update time;
//   3. unread needs a seen record: a finished run never opened here is not
//      "ready"; a streaming turn is Working and never counts for attention;
//   4. the switcher's grouping (Needs you · Working · Pinned) leaves the open
//      conversation out, once; ‹ Chats counts the other conversations that
//      wait on the owner or are ready;
//   5. deriving reads only: any write into an entry, fetch or storage write throws.
// Run: node server/testdata/chat-now.cjs
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const web=path.join(__dirname,'../web/js');
const chat=fs.readFileSync(path.join(web,'48-chat.js'),'utf8');
const nowPath=path.join(web,'49-chat-now.js');
assert.ok(fs.existsSync(nowPath),'49-chat-now.js (the Now projection) is missing');
const now=fs.readFileSync(nowPath,'utf8');
const slice=(src,a,b)=>{const i=src.indexOf(a);assert.ok(i>=0,'missing '+a);const j=src.indexOf(b,i+1);assert.ok(j>i,'missing '+b);return src.slice(i,j);};
const banned=()=>{throw Error('the Now projection must not write or fetch');};
const ctx=vm.createContext({window:{addEventListener(){}},fetch:banned,localStorage:{getItem:()=>null,setItem:banned,removeItem:banned},
 chatAttentionFilter:'all',terminalStates:new Map(),chatTermKinds:{claude:'Claude Code',codex:'Codex'},
 chatAgentLabel:n=>({alfred:'Alfred',kairos:'Kairos'}[n]||n),fmtWhen:iso=>'when:'+iso,chatOpenId:'',chatAgent:'',chatTaskID:''});
vm.runInContext(slice(chat,'function chatTaskThreadHash(','function chatSectionHash('),ctx);
vm.runInContext(slice(chat,'function chatTermFolder(se)','// chatTermRename'),ctx);
vm.runInContext(slice(chat,'let chatSeen=','async function chatLoadSeen'),ctx);
vm.runInContext(slice(chat,'let chatPins=','function chatInboxEntries()'),ctx);
vm.runInContext(slice(chat,'// chatEntryRoute','function chatRenderProjectGroups('),ctx);
vm.runInContext(slice(now,'// ---- the projection','// ---- end of the projection'),ctx);
const run=code=>vm.runInContext(code,ctx);
// values built inside the vm carry its own Array/Object prototypes: compare as plain data
const plain=x=>JSON.parse(JSON.stringify(x));
// a write anywhere in an entry throws (vm code is sloppy mode, where a frozen
// object would swallow the write silently)
const guard=o=>o&&typeof o==='object'?new Proxy(o,{get:(t,k)=>guard(t[k]),set(){throw Error('Now wrote into an entry');},defineProperty(){throw Error('Now wrote into an entry');},deleteProperty(){throw Error('Now deleted from an entry');}}):o;

const T=n=>'2026-10-09T0'+n+':00:00Z';
const agent=(id,patch={})=>({agent:'alfred',session:{id,title:'Thread '+id,turns:4,status:'idle',updated:T(1),deliveries:[],conversation:{key:'conv-'+id},...patch}});
const term=(id,patch={})=>({terminal:true,agent:'codex',session:{id,kind:'codex',name:'Codex '+id,cwd:'/home/owner/src/manifest',lastUsed:T(1),connectivity:'connected',process:'running',agentState:'idle',...patch}});
const task=(id,patch={})=>({taskThread:true,agent:'alfred',session:{id,title:'Task '+id,updated:T(1),turns:2,taskState:'',lastAuthor:'Alfred',lastText:'',...patch}});
const entries=[
 agent('idle'),                                                                             // unknown, out
 agent('draft',{turns:0}),                                                                  // draft, out
 agent('queued',{updated:T(3),deliveries:[{id:'q',state:'queued',text:'Draft the reply to the plumber'}]}),
 agent('running',{updated:T(2),status:'thinking',deliveries:[{id:'r',state:'running',text:'Summarise   the vendor\nquotes'}]}),
 agent('stopping',{updated:T(1),deliveries:[{id:'s',state:'running',stopRequested:true,text:'Long job'}]}),
 agent('failed',{updated:T(4),deliveries:[{id:'f',state:'failed',text:'x',error:'provider timed out after 420 s'}]}),
 agent('lost',{updated:T(5),supervision:{state:'disconnected',evidence:'receipt r9',runs:[]}}),
 term('blocked',{lastUsed:T(6),agentState:'blocked'}),
 task('plan',{updated:T(7),taskState:'plan-ready'}),
 agent('review',{updated:T(1),deliveries:[{id:'d',state:'completed',text:'Write the report'}]}),
 agent('unread',{updated:T(8),deliveries:[{id:'u',state:'completed',text:'Check the permit'}]}),
 agent('neverseen',{updated:T(8),deliveries:[{id:'n',state:'completed',text:'Old run'}]}),     // finished, never opened here: out
 agent('pinwait',{updated:T(1),supervision:{state:'failed',evidence:'abandoned',runs:[]}}),     // pinned AND failed: waiting only
 term('pinned',{lastUsed:T(2),agentState:'idle'}),                                            // pinned idle: pinned
 agent('archived',{updated:T(9),status:'thinking',deliveries:[{id:'a',state:'running'}]}),     // archived: out
 agent('trashed',{updated:T(9),supervision:{state:'failed',evidence:'x',runs:[]}}),            // deleted: out
];
run(`chatReviewStatus={'conv-review':{ready:2,changes:1}};
 chatPins={'agent:alfred/pinwait':true,'terminal:codex/pinned':true,'agent:alfred/archived':true};
 chatLifecycle={'agent:alfred/archived':'archived','agent:alfred/trashed':'deleted'};
 chatSeen={'agent:alfred/unread':{marker:'["2026-10-09T00:00:00Z",0,"",3]',at:1}};`);
ctx.entries=guard(entries);
const stateBefore=run('JSON.stringify([chatPins,chatSeen,chatLifecycle,chatReviewStatus])');
const rows=run('chatNowRows(entries)');
const ids=c=>plain(rows.filter(r=>r.category===c).map(r=>r.entry.session.id));
// 1. one category each, in priority order, newest first
assert.deepEqual(plain([...new Set(rows.map(r=>r.category))]),['waiting','working','ready','pinned'],'group order');
assert.deepEqual(ids('waiting'),['plan','blocked','lost','failed','pinwait']);
assert.deepEqual(ids('working'),['queued','running','stopping']);
assert.deepEqual(ids('ready'),['unread','review']);
assert.deepEqual(ids('pinned'),['pinned']);
assert.equal(new Set(rows.map(r=>r.key)).size,rows.length,'a row files once');
for(const out of ['idle','draft','neverseen','archived','trashed'])assert.ok(!rows.some(r=>r.entry.session.id===out),out+' must stay out of Now');
// 2. the state is chatEntryState's own word; summaries come from the records
for(const r of rows){const s=run('chatEntryState')(r.entry);assert.equal(r.state.label,s.label,r.key);assert.equal(r.state.execution,s.execution,r.key);}
const by=id=>rows.find(r=>r.entry.session.id===id);
assert.equal(by('queued').state.label,'Queued');assert.equal(by('running').state.label,'Working');assert.equal(by('stopping').state.label,'Interruption requested');
assert.equal(by('failed').state.label,'Run failed');assert.equal(by('lost').state.label,'Disconnected · outcome uncertain');
assert.equal(by('blocked').state.label,'Needs input');assert.equal(by('plan').state.label,'Plan ready · review');
assert.match(by('queued').summary,/^Accepted, not started · “Draft the reply to the plumber”$/);
assert.equal(by('running').summary,'“Summarise the vendor quotes”','whitespace folds, the instruction is quoted');
assert.match(by('stopping').summary,/Stop requested/);
assert.equal(by('failed').summary,'provider timed out after 420 s','the failure the receipt recorded');
assert.equal(by('lost').summary,'Check whether it finished');
assert.equal(by('blocked').summary,'Answer its prompt');
assert.equal(by('plan').summary,'Review the plan');
assert.equal(by('review').summary,'Review 2 outputs · 1 needs revision');
assert.equal(by('unread').summary,'New since you last looked');
assert.equal(by('pinned').summary,'In manifest');
assert.equal(by('pinwait').summary,'See what failed');
for(const r of rows){assert.ok(r.title&&r.agent&&r.summary&&r.when&&r.at,'row '+r.key+' is complete');assert.equal(r.when,'when:'+r.at);}
assert.equal(by('blocked').agent,'Codex');assert.equal(by('plan').agent,'Alfred · task');assert.equal(by('queued').agent,'Alfred');
assert.equal(by('plan').route,'#/chat/task/plan');assert.equal(by('queued').route,'#/chat/a/alfred/queued');assert.equal(by('blocked').route,'#/chat/a/codex/blocked');
assert.equal(run(`chatEntryRoute({agent:'',session:{id:'sp 1'}})`),'#/chat/sp%201','a spirit thread keeps its bare route');
const long=run(`chatNowRows([{agent:'alfred',session:{id:'l',updated:'${T(1)}',turns:3,deliveries:[{id:'x',state:'running',text:'${'word '.repeat(80)}'}]}}])`)[0];
assert.ok(long.summary.length<=100&&long.summary.endsWith('…”'),'a long instruction is clipped: '+long.summary.length);
console.log('PASS Now rows: priority order, one category each, exclusions, truthful labels and summaries');
// 3. unread needs a seen record; streaming is Working, never attention
assert.equal(run('chatNowAttention(chatNowRows(entries))'),7,'waiting 5 + ready 2');
const stream=run(`chatNowRows([{agent:'alfred',session:{id:'s',updated:'${T(1)}',turns:3,status:'thinking',activityOffset:99,deliveries:[{id:'z',state:'running',text:'go'}]}}])`);
assert.equal(stream[0].category,'working');assert.equal(run('chatNowAttention')(stream),0,'token/typing updates are never counted');
console.log('PASS attention: needs a seen marker for unread; streaming never counts');
// 4. the switcher: Needs you (waiting, then ready) · Working · Pinned, without the open conversation
run(`chatAgent='alfred';chatOpenId='unread'`);
const open=run('chatNowRows(entries)');
assert.equal(open.find(r=>r.entry.session.id==='unread').current,true);
assert.equal(run('chatNowAttention')(open),6,'‹ Chats counts the other conversations');
const sheet=run('chatNowGroups')(open,'switcher');
assert.deepEqual(plain(sheet.map(g=>g.label)),['Needs you','Working','Pinned']);
assert.deepEqual(plain(sheet[0].rows.map(r=>r.entry.session.id)),['plan','blocked','lost','failed','pinwait','review']);
assert.ok(!sheet.some(g=>g.rows.some(r=>r.current)),'the open conversation is not offered');
const rail=run('chatNowGroups')(open,'rail');
assert.deepEqual(plain(rail.map(g=>g.label)),['Waiting on you','Working','Ready to review','Pinned']);
assert.equal(rail[2].rows[0].current,true,'the rail keeps the open row, marked');
run(`chatTaskID='plan'`);
assert.equal(run('chatNowRows(entries)').find(r=>r.entry.session.id==='plan').current,true,'a task stage marks its task row');
assert.equal(run('chatNowRows(entries)').find(r=>r.entry.session.id==='unread').current,false,'a stale chatOpenId under a task stage is not current');
run(`chatTaskID='';chatAgent='';chatOpenId=''`);
assert.deepEqual(plain(run('chatNowGroups')(run('chatNowRows([])'),'switcher')),[],'no rows, no groups');
console.log('PASS switcher grouping and the ‹ Chats count leave the open conversation out');
// 5. read-only: guarded entries never written (a write throws), the owner's
// pin/seen/lifecycle/review state unchanged, no fetch or storage call
assert.equal(run('JSON.stringify([chatPins,chatSeen,chatLifecycle,chatReviewStatus])'),stateBefore);
console.log('PASS derivation is read-only: no entry writes, owner state unchanged, no fetch or storage');
