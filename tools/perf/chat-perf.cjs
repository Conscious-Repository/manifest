#!/usr/bin/env node
// chat-perf.cjs — a repeatable measurement of the chat surface in a real
// browser, over fixture data (never the owner's): the real front end and the
// app's own cache headers (webserve → server.WebHandler) in front of the chat
// stub (server/testdata/chat-stub-api.cjs), seeded with one long thread,
// short ones, and a tiles arrangement of 4 visible tiles + 1 in a visited,
// hidden workspace.
//
//   node tools/perf/chat-perf.cjs [--json out.json] [--idle 30] [--webserve /path/to/binary]
//
// Measures (printed as a table; --json writes the numbers):
//   cold open    first paint of a long thread's turns in a fresh browser
//                (ms from navigation start), the requests the page made and
//                how many reached the server (the rest the HTTP cache answered)
//   warm open    the same in a second tab of the same browser (HTTP cache warm)
//   switch       hash change to another thread until its turns paint
//   idle         requests per minute with one chat open and nothing happening
//   heap/nodes   JS heap and DOM nodes with the long thread open
//   tiles        idle requests per minute with 4 tiles + 1 hidden workspace,
//                split by frame visibility; heap of the whole page
//
// Needs node + Playwright (NODE_PATH=~/.cache/manifest-qa/node_modules) and Go
// (webserve is built on first use unless --webserve names a binary).
// perf-budget.cjs runs this and fails when a number leaves its budget.
const {chromium}=require('playwright');
const path=require('node:path'),fs=require('node:fs'),os=require('node:os'),{spawn,execFileSync}=require('node:child_process');
const root=path.resolve(__dirname,'../..');
const {makeStub}=require(path.join(root,'server/testdata/chat-stub-api.cjs'));

const arg=(name,def)=>{const i=process.argv.indexOf('--'+name);return i>0?process.argv[i+1]:def;};

// the fixture: thread a is long (LONG_TURNS), b/c/d/e short
const LONG_TURNS=300;
function seed(stub){
 const turn=(n,who,text)=>'## Turn '+n+' — '+who+' · 2026-09-25T'+String(8+Math.floor(n/60)).padStart(2,'0')+':'+String(n%60).padStart(2,'0')+':00Z\n\n'+text;
 const para='The measured cost sits in the parse, not the path walk; '.repeat(8);
 stub.bodies.a=Array.from({length:LONG_TURNS},(_,i)=>turn(i+1,i%2?'alfred':'user',i%2?'Reply '+(i+1)+'.\n\n- one point\n- another point\n\n'+para+'\n\n```\ncode line '+(i+1)+'\n```':'Question '+(i+1)+'?')).join('\n\n');
 stub.sessions.a.turns=LONG_TURNS;
 for(const id of ['c','d','e']){
  stub.sessions[id]={id,title:'Thread '+id,status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T08:00:00Z',created:'2026-09-25T08:00:00Z',spentUsd:0,deliveries:[]};
  stub.bodies[id]=turn(1,'user','Question for '+id+'?')+'\n\n'+turn(2,'alfred','Answer for '+id+'.');
 }
 // tiles: workspace 1 holds four tiles, workspace 2 one (visited, then hidden)
 const leaf=id=>({t:'leaf',id});
 const tiles={v:1,active:'2',focus:{'1':'t1','2':'t5'},full:{},
  ws:{'1':{t:'split',dir:'row',ratio:0.5,a:{t:'split',dir:'col',ratio:0.5,a:leaf('t1'),b:leaf('t2')},b:{t:'split',dir:'col',ratio:0.5,a:leaf('t3'),b:leaf('t4')}},'2':leaf('t5')},
  tiles:{t1:{route:'#/chat/a/alfred/a',title:''},t2:{route:'#/chat/a/alfred/b',title:''},t3:{route:'#/chat/a/alfred/c',title:''},t4:{route:'#/chat/a/alfred/d',title:''},t5:{route:'#/chat/a/alfred/e',title:''}}};
 stub.state.set('inbox/tiles',{key:'inbox',slot:'tiles',revision:1,value:tiles});
}

async function startWebserve(apiOrigin,bin){
 if(!bin){bin=path.join(os.tmpdir(),'manifest-perf-webserve');execFileSync('go',['build','-o',bin,'./tools/perf/webserve'],{cwd:root,stdio:'inherit'});}
 const proc=spawn(bin,['-addr','127.0.0.1:0','-api',apiOrigin],{stdio:['ignore','pipe','inherit']});
 const origin=await new Promise((resolve,reject)=>{let buf='';proc.stdout.on('data',d=>{buf+=d;const m=buf.match(/listening (\S+)/);if(m)resolve(m[1]);});proc.on('exit',c=>reject(new Error('webserve exited '+c)));});
 return {proc,origin};
}

// counts requests by the frame that made them; hidden = the frame's box has no layout
function recorder(page){
 const log=[];
 page.on('request',req=>{const f=req.frame();log.push({url:req.url(),at:Date.now(),frame:f===page.mainFrame()?'main':f.url(),type:req.resourceType()});});
 return log;
}
const median=a=>{const s=[...a].sort((x,y)=>x-y);return s[Math.floor(s.length/2)];};
async function metrics(page){const c=await page.context().newCDPSession(page);await c.send('Performance.enable');const {metrics}=await c.send('Performance.getMetrics');await c.detach();const m=Object.fromEntries(metrics.map(x=>[x.name,x.value]));return {heapMB:+(m.JSHeapUsedSize/1048576).toFixed(1),nodes:m.Nodes};}
const painted=(page,n)=>page.waitForFunction(n=>document.querySelectorAll('#chatTranscript [data-chat-read-turn]').length>=n?performance.now():0,n,{timeout:30000}).then(h=>h.jsonValue());

async function measure({idle=30,webserve=''}={}){
 const IDLE_S=idle;
 const stub=makeStub();seed(stub);
 await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));
 const ws=await startWebserve('http://127.0.0.1:'+stub.server.address().port,webserve);
 const base=ws.origin;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const out={};
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});
  // cold open (empty HTTP cache), three runs in fresh contexts
  const served=async()=>(await (await fetch(base+'/__perf/requests')).json()).n;
  const colds=[],coldReqs=[],coldNet=[];
  for(let i=0;i<3;i++){
   const c=await browser.newContext({viewport:{width:1440,height:900}});const p=await c.newPage();const log=recorder(p);
   const n0=await served();await p.goto(base+'/#/chat/a/alfred/a');colds.push(await painted(p,LONG_TURNS));coldReqs.push(log.length);coldNet.push(await served()-n0);await c.close();
  }
  out.coldOpenMs=Math.round(median(colds));out.coldOpenRequests=median(coldReqs);out.coldOpenNetwork=median(coldNet);
  // warm open: a second tab in one context, the first having filled the cache
  const page=await ctx.newPage();await page.goto(base+'/#/chat/a/alfred/b');await painted(page,2);
  const warms=[],warmReqs=[],warmNet=[];
  for(let i=0;i<3;i++){const p=await ctx.newPage();const log=recorder(p);const n0=await served();await p.goto(base+'/#/chat/a/alfred/a');warms.push(await painted(p,LONG_TURNS));warmReqs.push(log.length);warmNet.push(await served()-n0);await p.close();}
  out.warmOpenMs=Math.round(median(warms));out.warmOpenRequests=median(warmReqs);out.warmOpenNetwork=median(warmNet);
  // switch between a long and a short thread, both directions
  await page.goto(base+'/#/chat/a/alfred/a');await painted(page,LONG_TURNS);await page.waitForTimeout(500);
  const switches=[];
  for(let i=0;i<4;i++){
   const [to,n]=i%2?['a',LONG_TURNS]:['c',2];
   const ms=await page.evaluate(([to,n])=>new Promise(resolve=>{const t0=performance.now();location.hash='#/chat/a/alfred/'+to;const tick=()=>{const turns=document.querySelectorAll('#chatTranscript [data-chat-read-turn]');if(turns.length===n&&location.hash.endsWith('/'+to))resolve(performance.now()-t0);else requestAnimationFrame(tick);};tick();}),[to,n]);
   switches.push(ms);await page.waitForTimeout(300);
  }
  out.switchMs=Math.round(median(switches));
  // idle with the long thread open
  await page.goto(base+'/#/chat/a/alfred/a');await painted(page,LONG_TURNS);await page.waitForTimeout(3000);
  Object.assign(out,await metrics(page));
  {const log=recorder(page);await page.waitForTimeout(IDLE_S*1000);out.idleRequestsPerMin=Math.round(log.length*60/IDLE_S);}
  await page.close();
  // tiles: open on workspace 2 (one tile), then switch to 1 (four tiles); the
  // workspace-2 frame stays mounted and hidden
  const tp=await ctx.newPage();
  await tp.goto(base+'/#/chat/tiles');await tp.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length>=1,null,{timeout:30000});
  await tp.waitForTimeout(1500);
  await tp.keyboard.press('Alt+1');
  await tp.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length>=5,null,{timeout:30000});
  await tp.waitForFunction(()=>[...document.querySelectorAll('.chat-tile-frame')].every(f=>{try{return f.contentDocument.querySelectorAll('#chatTranscript [data-chat-read-turn]').length>0;}catch(e){return false;}}),null,{timeout:60000}).catch(()=>{});
  await tp.waitForTimeout(3000);
  const frameVis=await tp.evaluate(()=>[...document.querySelectorAll('.chat-tile-frame')].map(f=>({url:f.contentWindow.location.href,visible:f.getClientRects().length>0})));
  const visible=new Map(frameVis.map(f=>[f.url,f.visible]));
  Object.assign(out,Object.fromEntries(Object.entries(await metrics(tp)).map(([k,v])=>['tiles'+k[0].toUpperCase()+k.slice(1),v])));
  {const log=recorder(tp);await tp.waitForTimeout(IDLE_S*1000);
   const per=k=>Math.round(log.filter(k).length*60/IDLE_S);
   out.tilesFrames=frameVis.length;out.tilesVisibleFrames=frameVis.filter(f=>f.visible).length;
   out.tilesIdleRequestsPerMin=per(()=>true);
   out.tilesIdleParentPerMin=per(r=>r.frame==='main');
   out.tilesIdleVisiblePerMin=per(r=>r.frame!=='main'&&visible.get(r.frame)===true);
   out.tilesIdleHiddenPerMin=per(r=>r.frame!=='main'&&visible.get(r.frame)===false);
   out.tilesIdleByPath=Object.entries(log.reduce((m,r)=>{const k=new URL(r.url).pathname.replace(/\/sessions\/[^/]+/,'/sessions/:id');m[k]=(m[k]||0)+1;return m;},{})).sort((a,b)=>b[1]-a[1]).slice(0,8);
  }
  await tp.close();
  await ctx.close();
 }finally{await browser.close();stub.server.close();ws.proc.kill();}
 return out;
}

function report(out){
 const rows=[['cold open → first paint (long thread)',out.coldOpenMs+' ms, '+out.coldOpenRequests+' requests, '+out.coldOpenNetwork+' reached the server'],
  ['warm open (second tab)',out.warmOpenMs+' ms, '+out.warmOpenRequests+' requests, '+out.warmOpenNetwork+' reached the server'],
  ['chat switch',out.switchMs+' ms'],
  ['idle, one chat open',out.idleRequestsPerMin+' req/min'],
  ['heap / DOM nodes, long thread',out.heapMB+' MB / '+out.nodes],
  ['tiles: frames (visible)',out.tilesFrames+' ('+out.tilesVisibleFrames+')'],
  ['tiles idle: all / parent / visible / hidden',[out.tilesIdleRequestsPerMin,out.tilesIdleParentPerMin,out.tilesIdleVisiblePerMin,out.tilesIdleHiddenPerMin].join(' / ')+' req/min'],
  ['tiles heap / nodes',out.tilesHeapMB+' MB / '+out.tilesNodes]];
 for(const [k,v] of rows)console.log(k.padEnd(46)+v);
 console.log('tiles idle by path: '+out.tilesIdleByPath.map(([p,n])=>p+' '+n).join(', '));
}

module.exports={measure,report,LONG_TURNS};
if(require.main===module){
 measure({idle:Number(arg('idle','30')),webserve:arg('webserve','')}).then(out=>{report(out);const j=arg('json','');if(j)fs.writeFileSync(j,JSON.stringify(out,null,1));}).catch(e=>{console.error(e);process.exitCode=1;});
}
