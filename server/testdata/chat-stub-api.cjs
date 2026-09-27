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
function makeStub({terminal=true}={}){
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
   if(p==='/api/chat/inbox'){if(legacy)return json(res,404,{});return json(res,200,{roster,agents:{alfred:{sessions:Object.values(sessions)}},spirits:{sessions:[]},terminal:{sessions:[],enabled:terminal},state:{},review:{by_scope:{},by_task:{}},taskThreads:{threads:[]}});}
   if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
   // which store owns an id: the agent threads here; spirit ids are never served
   if(p==='/api/chat/resolve'){const id=url.searchParams.get('id');return json(res,200,{id,owners:sessions[id]?[{backend:'hermes',agent:'alfred',route:'#/chat/a/alfred/'+id}]:[],checked:['spirits','alfred','terminal'],unavailable:[]});}
   if(p==='/api/terminal/sessions')return json(res,200,{sessions:[],enabled:terminal});
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
