// chat-stub-api.cjs — a stateful stub of the chat API for browser fixtures
// that load the real front end (index.html + every script). Two agent
// threads (a: 40 turns, b: 2), revisioned chat-state slots, an SSE stream for
// the open thread (/__push?ev=&data= sends an event), and test hooks:
//   /__down?on=1          every API call answers 503 (an outage)
//   /__set?id=a&patch={}  merge fields into a session (status, supervision,
//                         deliveries…) — the next read sees them; &append=text
//                         adds a turn (&who=system|user|alfred, default alfred)
//   /__delay?ms=N         hold a send's acknowledgement N ms
//   /__legacy?on=1        404 /api/chat/inbox (an older server)
// makeStub({codex:true}) adds one finished Codex thread, cx1 (#/chat/a/codex/cx1).
// /api/chat/resolve names alfred as the owner of a/b; spirit reads are 404.
// The app shell's reads outside chat answer empty (a fresh vault), not 404.
// A send (POST …/messages) appends the user turn, records a queued delivery
// and a "submitted" supervision state: accepted, not started.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const caps={adapter:'hermes-oneshot',queue:'durable',cancelQueued:true,interrupt:'request-and-cancel-queued',stop:'request',steer:'unsupported',liveSteering:false,retry:'explicit-resubmit',resume:'fresh-session-per-turn',structuredQuestions:false,answerQuestions:'unsupported',supervision:'delivery-receipt',skillInventory:'on-disk'};
const turn=(n,who,text)=>'## Turn '+n+' — '+who+' · 2026-09-25T10:'+String(n%60).padStart(2,'0')+':00Z\n\n'+text;
const long=n=>Array.from({length:n},(_,i)=>turn(i+1,i%2?'alfred':'user',i%2?'Reply paragraph '+(i+1)+'.\nsecond line\n\nthird para. '+'Lorem ipsum dolor sit amet, consectetur adipiscing elit. '.repeat(6):'Question '+(i+1)+'?')).join('\n\n');
// the coding agents as chat_models.go lists them (aliases with what they last
// ran as, pinned ids, efforts, permission modes; Codex defaults to full access)
const codingEfforts=['low','medium','high','xhigh','max'].map(id=>({id}));
const codingCatalog={
 claude:{backend:'terminal',efforts:codingEfforts,liveModel:'command',liveEffort:'command',livePermission:'launch',
  models:[{id:'fable',label:'Fable',provider:'Anthropic',description:'Most capable Claude for complex, long-running work',lastRan:'claude-fable-5-1'},{id:'opus',label:'Opus',provider:'Anthropic',description:'Strong reasoning and coding',lastRan:'claude-opus-5-5'},{id:'sonnet',label:'Sonnet',provider:'Anthropic',description:'Fast, capable everyday coding'},{id:'claude-fable-5-1',label:'claude-fable-5-1',provider:'Anthropic',description:'Pinned model id'},{id:'claude-opus-5-5',label:'claude-opus-5-5',provider:'Anthropic',description:'Pinned model id'}],
  permissions:[{id:'manual',label:'Ask',description:'Reads freely; asks before edits and commands'},{id:'acceptEdits',label:'Accept edits',description:'Edits files without asking; asks before other commands'},{id:'plan',label:'Plan',description:'Read-only exploration; proposes a plan before changing anything'},{id:'auto',label:'Auto',description:'Runs actions a second model judges safe; asks otherwise'},{id:'dontAsk',label:'Allowed tools only',description:'Runs only the tools your Claude settings allow; refuses everything else'},{id:'bypassPermissions',label:'Bypass',description:'Runs everything without asking',danger:true}]},
 codex:{backend:'terminal',default:'gpt-5.6-luna',efforts:codingEfforts,defaultPermission:'full',liveModel:'native-picker',liveEffort:'native-picker',livePermission:'native-picker',
  models:[{id:'gpt-5.6-luna',label:'gpt-5.6-luna',provider:'OpenAI',context:400000}],
  permissions:[{id:'full',label:'Full access',description:'No sandbox and never asks',danger:true},{id:'auto',label:'Auto',description:'Workspace-write sandbox; asks before leaving it'},{id:'read-only',label:'Read only',description:'Reads only; asks before any change'}]}};
// one finished Codex thread (cx1), as terminal_transcript.go projects a
// rollout: an owner turn, a reply whose exec steps fold under Activity, the
// last call's context against the model's window, the settings it ran with
const codexTranscript={offset:4096,title:'Fix the flaky test',conversation:{key:'conv-cx1'},
 run:{id:'turn-2',state:'completed',at:'2026-09-25T10:07:12Z',evidence:'x'},
 context:{used:86000,window:258400,at:'2026-09-25T10:07:10Z'},
 settings:{model:'gpt-5.6-luna',effort:'high',permission:'full'},
 turns:[{id:'u1',who:'user',ts:'2026-09-25T10:00:00Z',text:'The chat fixture flakes one run in twenty. Find why and fix it.'},
  {id:'a1',who:'assistant',ts:'2026-09-25T10:00:04Z',end:'2026-09-25T10:07:10Z',blocks:[
   {t:'say',text:'I will run the fixture in a loop to reproduce it first.'},
   {t:'step',cast:'exec_command',input:'for i in $(seq 20); do node server/testdata/chat-status-line.cjs || break; done',id:'c1',done:true,result:'PASS ×19\nAssertionError: timer did not advance'},
   {t:'step',cast:'exec_command',input:'rg -n "setInterval" server/web/js/49-chat-status.js',id:'c2',done:true,result:'81:  set(".chat-status-time", …)'},
   {t:'step',cast:'apply_patch',input:'server/web/js/49-chat-status.js',id:'c3',done:true,result:'Success'},
   {t:'say',text:'The timer repainted only on a whole second boundary, so a run that started 999 ms before a paint read one second behind. It now repaints from the run start. Twenty runs pass in a row.'}]}]};
const codexRows=[{id:'cx1',kind:'codex',name:'Fix the flaky test',title:'Fix the flaky test',cwd:'/home/owner/src/manifest',backend:'herdr',live:true,process:'running',lastUsed:'2026-09-25T10:07:12Z',agentState:'idle'}];
// makeStub({longCodex:true}) adds cx2: 100 turns, each reply with a tool step
// whose output stays on the server — served by the lite contract
// (transcript_lite.go): ?lite=1&tail=N, …/turns?before=, …/step?id=. The owner
// turn at 1 is a long paste (40 lines) for the Show more fold.
const longPaste=Array.from({length:40},(_,i)=>'line '+(i+1)+' of the pasted log').join('\n');
const longTurns=[];for(let i=0;i<50;i++){const ts='2026-09-25T0'+String(9+Math.floor(i/60)).slice(-1)+':'+String(i%60).padStart(2,'0')+':00Z';
 longTurns.push({id:'lu'+i,who:'user',ts,text:i===1?longPaste:'question '+i});
 longTurns.push({id:'la'+i,who:'assistant',ts,end:ts,blocks:[{t:'step',cast:'exec_command',input:'ls '+i,id:'s'+i,done:true,result:'output of step '+i+' '+'x'.repeat(2000)},{t:'say',text:'answer '+i}]});}
const liteOf=t=>({...t,blocks:t.blocks&&t.blocks.map(b=>b.t==='step'&&b.result?{...b,result:undefined,resultBytes:b.result.length,sid:'cx2',done:true}:b)});
function makeStub({terminal=true,codex=false,longCodex=false}={}){
 const codexSessions=[...(codex?codexRows:[]),...(longCodex?[{id:'cx2',kind:'codex',name:'Long session',title:'Long session',cwd:'/home/owner/src/manifest',backend:'herdr',live:false,process:'stopped',lastUsed:'2026-09-25T12:00:00Z',agentState:'idle'}]:[])];
 const sessions={a:{id:'a',title:'Long research thread',status:'idle',agent:'alfred',turns:40,updated:'2026-09-25T11:00:00Z',created:'2026-09-25T10:00:00Z',spentUsd:0,deliveries:[]},
  b:{id:'b',title:'Second thread',status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T09:00:00Z',created:'2026-09-25T09:00:00Z',spentUsd:0,deliveries:[]}};
 const bodies={a:long(40),b:long(2)};
 const state=new Map();const streams=[];const log=[];const lastSend={value:null};let down=false,delay=0,legacy=false;
 const json=(res,code,body)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
 const roster={agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]};
 const detail=id=>{const s=sessions[id];return {session:s,body:bodies[id],conversation:{key:'conv-'+id},capabilities:caps,supervision:s.supervision||{adapter:'hermes-oneshot',state:'unknown',evidence:'x',capabilities:caps,runs:[]},outputs:[],queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]};};
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://x');const p=url.pathname;
  if(p==='/__push'){const ev=url.searchParams.get('ev'),data=url.searchParams.get('data')||'{}';streams.forEach(s=>s.write('event: '+ev+'\ndata: '+JSON.stringify({data:JSON.parse(data)})+'\n\n'));return json(res,200,{n:streams.length});}
  if(p==='/__log')return json(res,200,{log});
  if(p==='/__last')return json(res,200,{send:lastSend.value});
  if(p==='/__down'){down=url.searchParams.get('on')==='1';return json(res,200,{down});}
  if(p==='/__delay'){delay=Number(url.searchParams.get('ms'))||0;return json(res,200,{delay});}
  if(p==='/__legacy'){legacy=url.searchParams.get('on')==='1';return json(res,200,{legacy});}
  if(p==='/__set'){const s=sessions[url.searchParams.get('id')];Object.assign(s,JSON.parse(url.searchParams.get('patch')||'{}'));if(url.searchParams.has('append')){s.turns++;bodies[s.id]+='\n\n'+turn(s.turns,url.searchParams.get('who')||'alfred',url.searchParams.get('append'));}s.updated=new Date().toISOString();return json(res,200,s);}
  if(p.startsWith('/api/')&&down){log.push('DOWN '+req.method+' '+p);return json(res,503,{error:'unavailable'});}
  if(p.startsWith('/api/')){
   log.push(req.method+' '+p+url.search);
   if(p==='/api/agents/chat/roster')return json(res,200,roster);
   if(p==='/api/chat/models')return json(res,200,{agents:{alfred:{backend:'hermes',default:'claude-x',defaultProvider:'anthropic',liveModel:'recipient',liveEffort:'recipient',
     efforts:['none','minimal','low','medium','high','xhigh','max','ultra'].map(id=>({id})),
     models:[{id:'claude-x',label:'claude-x',provider:'anthropic',description:'Anthropic'},{id:'gpt-5.6-luna',label:'gpt-5.6-luna',provider:'openai-codex',description:'OpenAI'},{id:'grok-4.6',label:'grok-4.6',provider:'xai-oauth',description:'xAI'},{id:'deepseek-v4.1-flash',label:'deepseek-v4.1-flash',provider:'lab-sparks',description:'Lab (192.168.87.11:8000/v1)'}]},
    claude:codingCatalog.claude,codex:codingCatalog.codex}});
   if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:Object.values(sessions)});
   const gm=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([^/]+)\/goal$/);
   if(gm&&!sessions[gm[1]])return json(res,404,{});
   if(gm&&req.method==='POST'){let b='';req.on('data',c=>b+=c);req.on('end',()=>{const v=JSON.parse(b||'{}'),s=sessions[gm[1]];s.goal=(v.goal||'').trim();s.goalState=s.goal?(v.state||'active'):'';json(res,200,{goal:s.goal,goalState:s.goalState});});return;}
   // cancel a queued (not yet dispatched) instruction, as the server does
   const cq=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([^/]+)\/cancel-queued$/);
   if(cq&&req.method==='POST'){let b='';req.on('data',c=>b+=c);req.on('end',()=>{const v=JSON.parse(b||'{}'),s=sessions[cq[1]],d=s&&s.deliveries.find(x=>x.id===v.requestId&&x.state==='queued');if(!d)return json(res,409,{error:'not queued'});d.state='cancelled';s.updated=new Date().toISOString();json(res,200,{ok:true});});return;}
   const sm=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([^/]+)(\/stream|\/messages)?$/);
   if(sm&&!sessions[sm[1]])return json(res,404,{});
   if(sm&&sm[2]==='/stream'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});res.write(':ok\n\n');streams.push(res);req.on('close',()=>streams.splice(streams.indexOf(res),1));return;}
   if(sm&&sm[2]==='/messages'&&req.method==='POST'){let b='';req.on('data',c=>b+=c);req.on('end',()=>setTimeout(()=>{
     const v=JSON.parse(b||'{}'),s=sessions[sm[1]];lastSend.value=v;s.turns++;bodies[s.id]+='\n\n'+turn(s.turns,'user',v.text||'');
     const id=v.requestId||'req-'+s.turns;s.deliveries=[...s.deliveries,{id,state:'queued',userTurn:s.turns,text:v.text}];
     s.supervision={adapter:'hermes-oneshot',state:'submitted',evidence:'delivery receipt '+id,capabilities:caps,runs:[{id,state:'queued'}]};s.updated=new Date().toISOString();
     json(res,200,{ok:true,id:s.id,requestId:id});},delay));return;}
   if(sm)return json(res,200,detail(sm[1]));
   if(p==='/api/chat/inbox'){if(legacy)return json(res,404,{});return json(res,200,{roster,agents:{alfred:{sessions:Object.values(sessions)}},spirits:{sessions:[]},terminal:{sessions:codexSessions,enabled:terminal},state:{},review:{by_scope:{},by_task:{}},taskThreads:{threads:[]}});}
   if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
   // which store owns an id: the agent threads here; spirit ids are never served
   if(p==='/api/chat/resolve'){const id=url.searchParams.get('id');return json(res,200,{id,owners:sessions[id]?[{backend:'hermes',agent:'alfred',route:'#/chat/a/alfred/'+id}]:[],checked:['spirits','alfred','terminal'],unavailable:[]});}
   if(p==='/api/terminal/sessions')return json(res,200,{sessions:codexSessions,enabled:terminal});
   const tt=p.match(/^\/api\/terminal\/session\/([^/]+)\/transcript$/);
   if(tt&&tt[1]==='cx2'&&longCodex){const lite=url.searchParams.get('lite')==='1',tail=Number(url.searchParams.get('tail'))||0,after=Number(url.searchParams.get('after'))||0;
    if(after>=9000)return json(res,200,{offset:9000,turns:[],older:0,historyAvailable:true,conversation:{key:'conv-cx2'}});
    const window=tail?longTurns.slice(-tail):longTurns;return json(res,200,{offset:9000,historyAvailable:true,conversation:{key:'conv-cx2'},title:'Long session',older:longTurns.length-window.length,turns:lite?window.map(liteOf):window,planningTimeline:null,timelineHash:''});}
   const ot=p.match(/^\/api\/terminal\/session\/cx2\/turns$/);
   if(ot&&longCodex){const i=longTurns.findIndex(t=>t.id===url.searchParams.get('before')),lim=Number(url.searchParams.get('limit'))||40;if(i<0)return json(res,404,{});const start=Math.max(0,i-lim);return json(res,200,{turns:longTurns.slice(start,i).map(liteOf),older:start});}
   const st1=p.match(/^\/api\/terminal\/session\/cx2\/step$/);
   if(st1&&longCodex){log.push('STEP '+url.searchParams.get('id'));const id=url.searchParams.get('id');const hit=longTurns.flatMap(t=>t.blocks||[]).find(b=>b.id===id);return hit?json(res,200,{id,result:hit.result}):json(res,404,{});}
   if(tt&&codexSessions.some(s=>s.id===tt[1]))return json(res,200,Number(url.searchParams.get('after'))>=codexTranscript.offset?{offset:codexTranscript.offset,turns:[],run:codexTranscript.run,context:codexTranscript.context,settings:codexTranscript.settings}:codexTranscript);
   if(p==='/api/terminal/folders')return json(res,200,{enabled:true,home:'/home/owner',recent:['/home/owner/src/manifest'],repos:['/home/owner/src/manifest','/home/owner/src/lab-apps']});
   if(p==='/api/chat/review-status')return json(res,200,{by_scope:{},by_task:{}});
   // the app shell's own reads on every page (rail counts, feed badge,
   // connections, the terminal event stream): empty, as a fresh vault answers,
   // so a capture's console carries only the chat's own errors
   // (the terminal stream ends at once with a day-long retry: held open, one
   // per tile frame would exhaust the browser's six connections to the host)
   if(p==='/api/terminal/events'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});return res.end('retry: 86400000\n\n');}
   const shell={'/api/feed/badge':{count:0},'/api/tasks':{outstanding:[],assignees:{},counts:{tasks:0}},'/api/goals':{areas:[]},'/api/aion':{backlog:[]},'/api/properties':{properties:[],deals:[],templates:[],holdings:{}},'/api/re/backlog':{items:[],goalsArea:null},'/api/settings/connections':{rows:[]}};
   if(req.method==='GET'&&shell[p])return json(res,200,shell[p]);
   const st=p.match(/^\/api\/chat\/state\/([^/]+)\/([^/]+)$/);
   if(st){const k=st[1]+'/'+st[2];if(req.method==='PUT'){let b='';req.on('data',c=>b+=c);req.on('end',()=>{const v=JSON.parse(b||'{}');const cur=state.get(k)||{revision:0};const next={key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:cur.revision+1,value:v.value};state.set(k,next);json(res,200,next);});return;}
    return json(res,200,state.get(k)||{key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:0,value:null});}
   return json(res,404,{});
  }
  const file=path.join(web,p==='/'?'index.html':p);
  if(!file.startsWith(web)||!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.writeHead(404);return res.end();}
  res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});fs.createReadStream(file).pipe(res);
 });
 return {server,state,log,sessions,bodies};
}
module.exports={makeStub};
