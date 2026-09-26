// chat-stub-api.cjs — a stateful stub of the chat API for browser fixtures
// that load the real front end (index.html + every script). Two agent
// threads (a: 40 turns, b: 2), revisioned chat-state slots, an SSE stream for
// the open thread (/__push?ev=&data= sends an event), and test hooks:
//   /__down?on=1          every API call answers 503 (an outage)
//   /__set?id=a&patch={}  merge fields into a session (status, supervision,
//                         deliveries…) — the next read sees them
//   /__delay?ms=N         hold a send's acknowledgement N ms
//   /__legacy?on=1        404 /api/chat/inbox (an older server)
// A send (POST …/messages) appends the user turn, records a queued delivery
// and a "submitted" supervision state: accepted, not started.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const caps={adapter:'hermes-oneshot',queue:'durable',cancelQueued:true,interrupt:'request-and-cancel-queued',stop:'request',steer:'unsupported',liveSteering:false,retry:'explicit-resubmit',resume:'fresh-session-per-turn',structuredQuestions:false,answerQuestions:'unsupported',supervision:'delivery-receipt',skillInventory:'on-disk'};
const turn=(n,who,text)=>'## Turn '+n+' — '+who+' · 2026-09-25T10:'+String(n%60).padStart(2,'0')+':00Z\n\n'+text;
const long=n=>Array.from({length:n},(_,i)=>turn(i+1,i%2?'alfred':'user',i%2?'Reply paragraph '+(i+1)+'.\nsecond line\n\nthird para. '+'Lorem ipsum dolor sit amet, consectetur adipiscing elit. '.repeat(6):'Question '+(i+1)+'?')).join('\n\n');
function makeStub(){
 const sessions={a:{id:'a',title:'Long research thread',status:'idle',agent:'alfred',turns:40,updated:'2026-09-25T11:00:00Z',created:'2026-09-25T10:00:00Z',spentUsd:0,deliveries:[]},
  b:{id:'b',title:'Second thread',status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T09:00:00Z',created:'2026-09-25T09:00:00Z',spentUsd:0,deliveries:[]}};
 const bodies={a:long(40),b:long(2)};
 const state=new Map();const streams=[];const log=[];let down=false,delay=0,legacy=false;
 const json=(res,code,body)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
 const roster={agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]};
 const detail=id=>{const s=sessions[id];return {session:s,body:bodies[id],conversation:{key:'conv-'+id},capabilities:caps,supervision:s.supervision||{adapter:'hermes-oneshot',state:'unknown',evidence:'x',capabilities:caps,runs:[]},outputs:[],queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]};};
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://x');const p=url.pathname;
  if(p==='/__push'){const ev=url.searchParams.get('ev'),data=url.searchParams.get('data')||'{}';streams.forEach(s=>s.write('event: '+ev+'\ndata: '+JSON.stringify({data:JSON.parse(data)})+'\n\n'));return json(res,200,{n:streams.length});}
  if(p==='/__log')return json(res,200,{log});
  if(p==='/__down'){down=url.searchParams.get('on')==='1';return json(res,200,{down});}
  if(p==='/__delay'){delay=Number(url.searchParams.get('ms'))||0;return json(res,200,{delay});}
  if(p==='/__legacy'){legacy=url.searchParams.get('on')==='1';return json(res,200,{legacy});}
  if(p==='/__set'){const s=sessions[url.searchParams.get('id')];Object.assign(s,JSON.parse(url.searchParams.get('patch')||'{}'));if(url.searchParams.has('append')){s.turns++;bodies[s.id]+='\n\n'+turn(s.turns,'alfred',url.searchParams.get('append'));}s.updated=new Date().toISOString();return json(res,200,s);}
  if(p.startsWith('/api/')&&down){log.push('DOWN '+req.method+' '+p);return json(res,503,{error:'unavailable'});}
  if(p.startsWith('/api/')){
   log.push(req.method+' '+p+url.search);
   if(p==='/api/agents/chat/roster')return json(res,200,roster);
   if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:Object.values(sessions)});
   const sm=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])(\/stream|\/messages)?$/);
   if(sm&&sm[2]==='/stream'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});res.write(':ok\n\n');streams.push(res);req.on('close',()=>streams.splice(streams.indexOf(res),1));return;}
   if(sm&&sm[2]==='/messages'&&req.method==='POST'){let b='';req.on('data',c=>b+=c);req.on('end',()=>setTimeout(()=>{
     const v=JSON.parse(b||'{}'),s=sessions[sm[1]];s.turns++;bodies[s.id]+='\n\n'+turn(s.turns,'user',v.text||'');
     const id=v.requestId||'req-'+s.turns;s.deliveries=[...s.deliveries,{id,state:'queued',userTurn:s.turns,text:v.text}];
     s.supervision={adapter:'hermes-oneshot',state:'submitted',evidence:'delivery receipt '+id,capabilities:caps,runs:[{id,state:'queued'}]};s.updated=new Date().toISOString();
     json(res,200,{ok:true,id:s.id,requestId:id});},delay));return;}
   if(sm)return json(res,200,detail(sm[1]));
   if(p==='/api/chat/inbox'){if(legacy)return json(res,404,{});return json(res,200,{roster,agents:{alfred:{sessions:Object.values(sessions)}},spirits:{sessions:[]},terminal:{sessions:[],enabled:true},state:{},review:{by_scope:{},by_task:{}},taskThreads:{threads:[]}});}
   if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
   if(p==='/api/terminal/sessions')return json(res,200,{sessions:[],enabled:true});
   if(p==='/api/chat/review-status')return json(res,200,{by_scope:{},by_task:{}});
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
