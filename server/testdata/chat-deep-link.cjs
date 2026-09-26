// Cold deep links: a bare #/chat/<id> is the spirit route's shape, but spirit
// and agent ids share one shape, so the route alone cannot name the owner.
// 2026-09-26: #/chat/20260912-130847-8ddb (a live Alfred thread) asked the
// spirits store, got 404, and the stage said the conversation was "deleted or
// archived". The loader now asks /api/chat/resolve and follows the owner; the
// not-found copy is used only when every store was asked and none has it, and
// a failed load closes the composer.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
assert.ok(!/["'`][^"'`\n]*deleted or archived/.test(source),'no copy may call a conversation "deleted or archived" — an archived thread still loads');

const node=(tag,cls,text)=>{const n={tag,className:cls||'',textContent:text==null?'':String(text),dataset:{},attrs:{},childNodes:[],
 classList:{add(c){if(!n.className.split(' ').includes(c))n.className=(n.className+' '+c).trim();},remove(c){n.className=n.className.split(' ').filter(x=>x!==c).join(' ');},contains(c){return n.className.split(' ').includes(c);}},
 setAttribute(k,v){n.attrs[k]=String(v);},removeAttribute(k){delete n.attrs[k];},
 append(...xs){n.childNodes.push(...xs);},replaceChildren(...xs){n.childNodes=xs;}};return n;};
const text=n=>[n.textContent,...n.childNodes.map(text)].join(' ');
const find=(n,pred)=>pred(n)?n:n.childNodes.reduce((hit,c)=>hit||find(c,pred),null);

// the stores, as the server would answer them
const ALFRED='20260912-130847-8ddb',SPIRIT='20260913-090000-aa11',KAIROS='th/new-1786943020701',CLAUDE='0123456789abcdef',GONE='20260101-000000-dead';
const served={['/api/chat/sessions/'+SPIRIT]:1,['/api/agents/chat/alfred/sessions/'+ALFRED]:1,['/api/agents/chat/kairos/sessions/'+encodeURIComponent(KAIROS)]:1};
const owners={[ALFRED]:[{backend:'hermes',agent:'alfred',route:'#/chat/a/alfred/'+ALFRED}],[SPIRIT]:[{backend:'spirit',agent:'',route:'#/chat/'+SPIRIT}],
 [KAIROS]:[{backend:'portal',agent:'kairos',route:'#/chat/a/kairos/'+encodeURIComponent(KAIROS)}],[CLAUDE]:[{backend:'terminal',agent:'claude',route:'#/chat/a/claude/'+CLAUDE}]};
let unavailable=[],fetched=[],replaced=[],painted=[];
const transcript=node('div','chat-transcript'),composer=node('div','chat-composer');
composer.dataset.built='1';composer.append(node('textarea','chat-input'));
const ctx=vm.createContext({console,encodeURIComponent,Error,String,Promise,
 chatSessionLoadTicket:0,chatRouteVersion:0,chatAgent:'',chatOpenId:'',chatLastUpdated:'',chatCurSession:null,chatPendingWorkspace:null,
 els:{chatView:{hidden:false}},chatStageCache:new Map(),chatStageKey:(a,id)=>a+'/'+id,chatStageRemember(){},chatConversationTasks:new Map(),
 chatIsTerm:a=>['claude','codex'].includes(a),chatRemember(){},chatBareFrom:'',chatRosterEntry:a=>['alfred','kairos'].includes(a)?{name:a}:null,chatPrepareDraft:async()=>{},chatPrepareReadingPosition:async()=>{},chatOriginArtifactSelection:async()=>null,
 chatBaseFor:a=>a?'/api/agents/chat/'+encodeURIComponent(a)+'/sessions':'/api/chat/sessions',
 chatAgentLabel:a=>({alfred:'Alfred',kairos:'Kairos · team',claude:'Claude Code'})[a]||a,
 chatSectionHash:a=>a?'#/chat/a/'+encodeURIComponent(a):'#/chat/spirits',
 el:node,document:{getElementById:id=>id==='chatTranscript'?transcript:id==='chatComposer'?composer:null,querySelector:()=>null},
 location:{hash:'',replace(h){replaced.push(h);}},
 renderChatTranscript:d=>painted.push(d.session.id),renderChatComposer(){},ensureChatStream(){},ensureChatPoll(){},
 fetch:async url=>{fetched.push(url);
  if(url.startsWith('/api/chat/resolve?id=')){const id=decodeURIComponent(url.slice(21));return {ok:true,json:async()=>({id,owners:owners[id]||[],checked:['spirits','alfred','kairos','terminal'],unavailable})};}
  const hit=served[url];const id=decodeURIComponent(url.split('/').pop());
  return hit?{ok:true,status:200,json:async()=>({session:{id,turns:2},conversation:{key:'k-'+id}})}:{ok:false,status:404};
 }});
ctx.chatBase=()=>ctx.chatBaseFor(ctx.chatAgent);
ctx.chatHash=id=>ctx.chatAgent?'#/chat/a/'+encodeURIComponent(ctx.chatAgent)+'/'+encodeURIComponent(id):'#/chat/'+encodeURIComponent(id);
vm.runInContext(source.slice(source.indexOf('// Keep failed thread loads'),source.indexOf('// ---- live stream layer')),ctx);
// the route parse, as showChat does it: a/<agent>/<id…> or a bare <id>
const route=h=>{const seg=h.slice('#/chat/'.length).split('/').map(decodeURIComponent);
 if(seg[0]==='a'){ctx.chatAgent=seg[1];ctx.chatOpenId=seg.slice(2).join('/');}else{ctx.chatAgent='';ctx.chatOpenId=seg.join('/');}ctx.chatRouteVersion++;};
const open=async h=>{route(h);await ctx.loadChatSession(ctx.chatOpenId);
 // a location.replace is a hashchange: the router runs again on the owner's route
 while(replaced.length){const next=replaced.shift();route(next);await ctx.loadChatSession(ctx.chatOpenId);}};
const reset=()=>{fetched=[];replaced=[];painted=[];};

(async()=>{
 // 1. the reported case: a cold deep link to a live Alfred thread
 await open('#/chat/'+ALFRED);
 assert.deepEqual(painted,[ALFRED],'a bare deep link to an Alfred thread must load it');
 assert.ok(fetched.includes('/api/agents/chat/alfred/sessions/'+ALFRED),'loaded from the store that owns it');
 assert.ok(!/no conversation|deleted/i.test(text(transcript)),'no not-found copy on the way');
 // 2. a spirit id opens straight from its own route — no resolver round trip
 reset();await open('#/chat/'+SPIRIT);
 assert.deepEqual(painted,[SPIRIT]);assert.ok(!fetched.some(u=>u.startsWith('/api/chat/resolve')),'an owned route never asks the resolver');
 // 3. an agent route naming a spirit id follows the spirit store
 reset();await open('#/chat/a/alfred/'+SPIRIT);
 assert.deepEqual(painted,[SPIRIT]);assert.ok(fetched.includes('/api/chat/sessions/'+SPIRIT));
 // 4. a portal thread (id with its own slash) deep-linked bare
 reset();await open('#/chat/'+encodeURIComponent(KAIROS));
 assert.deepEqual(painted,[KAIROS],'bare kairos thread id must load from the portal store');
 // 5. a claude terminal id: the resolver hands it to the terminal route
 reset();route('#/chat/'+CLAUDE);await ctx.loadChatSession(CLAUDE);
 assert.deepEqual(replaced,['#/chat/a/claude/'+CLAUDE],'a terminal id must route to its terminal section');
 // 6. nobody has it: the not-found copy, and only then
 reset();composer.replaceChildren(node('textarea','chat-input'));composer.dataset.built='1';
 await open('#/chat/'+GONE);
 assert.deepEqual(painted,[]);assert.match(text(transcript),/No conversation has this id/);
 assert.match(text(transcript),/Every chat store was checked/);
 // no section recorded before the link: Back falls back to Spirits, and says why
 const backBtn=()=>find(transcript,n=>n.tag==='button'&&/^Back to /.test(n.textContent));
 assert.equal(backBtn()?.textContent,'Back to Spirits','not found offers the way back');
 assert.match(backBtn().title||'',/No earlier section is recorded/,'the fallback says it is one');
 assert.ok(!find(composer,n=>n.tag==='textarea'),'a failed load leaves nothing to type into');
 assert.ok(composer.classList.contains('closed')&&composer.attrs['aria-disabled']==='true'&&composer.dataset.built==='','the composer is closed and rebuilds on the next load');
 // 7. a store that could not be asked: never "not found"
 reset();unavailable=['terminal'];await open('#/chat/'+GONE);
 assert.doesNotMatch(text(transcript),/No conversation has this id/,'an unasked store may still hold it');
 assert.match(text(transcript),/terminal could not be checked/);
 assert.ok(find(transcript,n=>n.tag==='button'&&n.textContent==='Retry'));
 unavailable=[];
 // 8. owner decision 2026-09-26 "last section": Back from a bare not-found
 //    link returns to the section the reader was in when they opened it
 for(const [from,label,hash] of [['alfred','Alfred','#/chat/a/alfred'],['claude','Claude Code','#/chat/a/claude'],['spirits','Spirits','#/chat/spirits']]){
  reset();ctx.chatBareFrom=from;await open('#/chat/'+GONE);
  assert.equal(backBtn()?.textContent,'Back to '+label,'a bare not-found link goes back to the last section ('+from+')');
  assert.ok(!backBtn().title,'a recorded section is not labelled a guess');
  ctx.location.hash='';backBtn().onclick();assert.equal(ctx.location.hash,hash);
 }
 // a remembered section gone from the roster is not trusted
 reset();ctx.chatBareFrom='retired-agent';await open('#/chat/'+GONE);
 assert.equal(backBtn()?.textContent,'Back to Spirits');assert.match(backBtn().title,/No earlier section/);
 // a route that names its section keeps it, whatever was remembered
 reset();ctx.chatBareFrom='claude';await open('#/chat/a/alfred/'+GONE);
 assert.equal(backBtn()?.textContent,'Back to Alfred','an agent route goes back to its own section');
 ctx.chatBareFrom='';
 console.log('Chat cold deep links passed');
})().catch(e=>{console.error(e);process.exitCode=1});
