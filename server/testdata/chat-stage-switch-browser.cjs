// chat-stage-switch-browser.cjs — the real front end (index.html + every
// script) over a stub API, driven through thread switches (2026-09-12):
//   1. leaving thread A for an unseen thread B clears A's turns from the stage
//      within a frame — long before B's payload lands — and B's title heads the
//      stage from the list row; B's fetch starts at once, not behind the lists;
//   2. coming back to A paints A's turns synchronously from the stage cache,
//      and the revalidating fetch, finding nothing changed, leaves that paint
//      (its DOM nodes) untouched.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-stage-switch-browser.cjs
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const LIST_DELAY=300,THREAD_DELAY=400;
const thread=id=>({session:{id,title:'Thread '+id.toUpperCase(),status:'idle',agent:'alfred',turns:2,updated:'2026-09-12T10:0'+(id==='a'?1:2)+':00Z',created:'2026-09-12T10:00:00Z',spentUsd:0},
 body:'## Turn 1 — user · 2026-09-12T10:00:00Z\n\nhello from '+id.toUpperCase()+'\n\n## Turn 2 — alfred · 2026-09-12T10:01:00Z\n\nreply from '+id.toUpperCase(),
 conversation:{key:'conv-'+id},queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]});
const log=[];
const json=(res,code,body,delay=0)=>setTimeout(()=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));},delay);
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://x');const p=url.pathname;
 if(p.startsWith('/api/')){
  log.push({p,at:Date.now()});
  if(p==='/api/agents/chat/roster')return json(res,200,{agents:[{name:'alfred',label:'Alfred',enabled:true,model:'claude-x'}]});
  if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:[thread('a').session,thread('b').session]},LIST_DELAY);
  const m=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])$/);if(m)return json(res,200,thread(m[1]),THREAD_DELAY);
  if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
  if(p==='/api/terminal/sessions')return json(res,200,{sessions:[],enabled:true},LIST_DELAY);
  if(p==='/api/chat/review-status')return json(res,200,{by_scope:{},by_task:{}});
  const st=p.match(/^\/api\/chat\/state\/([^/]+)\/([^/]+)$/);if(st)return json(res,200,{key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:0,value:null});
  return json(res,404,{});
 }
 if(p==='/__log')return json(res,200,log);
 const file=path.join(web,p==='/'?'index.html':p);
 if(!file.startsWith(web)||!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.writeHead(404);return res.end();}
 res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const page=await browser.newPage({viewport:{width:1200,height:900}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/a');
  const transcript=page.locator('#chatTranscript');
  await transcript.getByText('reply from A').waitFor();
  // a snapshot of the stage ~40ms after the hash moves — the network has not answered yet
  const switchTo=id=>page.evaluate(id=>new Promise(res=>{location.hash='#/chat/a/alfred/'+id;setTimeout(()=>{const t=document.getElementById('chatTranscript');res({text:t.textContent,head:document.querySelector('.chat-thread-header .chat-head-title')?.textContent||'',composer:!!document.querySelector('#chatComposer textarea'),kept:!!t.querySelector('[data-mark]')});},40);}),id);
  // 1. A → unseen B
  const leaveAt=Date.now();const toB=await switchTo('b');
  assert.equal(toB.text.includes('reply from A'),false,'the previous thread leaves the stage before the network answers');
  assert.equal(toB.head,'Thread B','the target names the stage from its list row at once');
  assert.equal(toB.composer,true,'the composer stands through the switch');
  await transcript.getByText('reply from B').waitFor();
  const requests=await (await fetch(base+'/__log')).json();
  const bFetch=requests.find(r=>r.p==='/api/agents/chat/alfred/sessions/b');
  assert.ok(bFetch&&bFetch.at-leaveAt<LIST_DELAY,'B is fetched at once, not behind the list refresh ('+(bFetch&&bFetch.at-leaveAt)+'ms)');
  // 2. B → A again: the cached paint, kept by the revalidation
  await page.evaluate(()=>{document.getElementById('chatTranscript').textContent;});
  const toA=await switchTo('a');
  assert.equal(toA.text.includes('reply from A'),true,'a seen thread repaints from the stage cache synchronously');
  assert.equal(toA.text.includes('reply from B'),false);
  assert.equal(toA.head,'Thread A');
  await page.evaluate(()=>{document.getElementById('chatTranscript').firstElementChild.dataset.mark='kept';});
  await page.waitForTimeout(THREAD_DELAY+400);
  assert.equal(await page.evaluate(()=>!!document.querySelector('#chatTranscript [data-mark="kept"]')),true,'an unchanged payload leaves the cached paint in place');
  const aFetches=(await (await fetch(base+'/__log')).json()).filter(r=>r.p==='/api/agents/chat/alfred/sessions/a').length;
  assert.equal(aFetches,2,'A was fetched on first open and once to revalidate');
  assert.deepEqual(errors.filter(e=>!/EventSource|terminal\/events/.test(e)),[]);
  console.log('PASS: switching threads turns the stage over synchronously (unseen: empty stage + provisional head, eager fetch; seen: cached paint kept by an unchanged revalidation).');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1);});
