// chat-phone-responsive.cjs — the real front end over a stateful stub API in
// headless Chromium, pinning the responsive/accessibility measurements of the
// 2026-09-25 workbench finish pass (row 6). Browser viewports, not a phone:
//   1. 320/390/412 × 844/568/400, both themes: no horizontal overflow, the
//      composer field keeps the 16px iOS-zoom floor, the composer is on screen;
//   2. Back with the phone Chats list open closes the list and stays on the
//      thread (measured defect: it left Chat); closing from the page leaves no
//      extra history entry; Escape closes it and returns focus to ‹ Chats;
//   3. streaming while reading history keeps the first visible turn fixed;
//      at the bottom the transcript follows;
//   4. desktop (1280) never shows the phone toggle.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-phone-responsive.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const caps={adapter:'hermes-oneshot',queue:'durable',cancelQueued:true,interrupt:'request-and-cancel-queued',stop:'request',steer:'unsupported',liveSteering:false,retry:'explicit-resubmit',resume:'fresh-session-per-turn',structuredQuestions:false,answerQuestions:'unsupported',supervision:'delivery-receipt',skillInventory:'on-disk'};
const long=n=>Array.from({length:n},(_,i)=>'## Turn '+(i+1)+' — '+(i%2?'alfred':'user')+' · 2026-09-25T10:'+String(i).padStart(2,'0')+':00Z\n\n'+(i%2?'Reply paragraph '+(i+1)+'. '+'Lorem ipsum dolor sit amet, consectetur adipiscing elit. '.repeat(6):'Question '+(i+1)+'?')).join('\n\n');
function makeStub(){
 const sessions={a:{id:'a',title:'Long research thread',status:'idle',agent:'alfred',turns:40,updated:'2026-09-25T11:00:00Z',created:'2026-09-25T10:00:00Z',spentUsd:0,deliveries:[]},
  b:{id:'b',title:'Second thread',status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T09:00:00Z',created:'2026-09-25T09:00:00Z',spentUsd:0,deliveries:[]}};
 const bodies={a:long(40),b:long(2)};
 const state=new Map();const streams=[];const log=[];
 const json=(res,code,body)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://x');const p=url.pathname;
  if(p==='/__push'){const ev=url.searchParams.get('ev'),data=url.searchParams.get('data')||'{}';streams.forEach(s=>s.write('event: '+ev+'\ndata: '+JSON.stringify({data:JSON.parse(data)})+'\n\n'));return json(res,200,{n:streams.length});}
  if(p==='/__log')return json(res,200,{log});
  if(p.startsWith('/api/')){
   log.push(req.method+' '+p+url.search);
   if(p==='/api/agents/chat/roster')return json(res,200,{agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]});
   if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:Object.values(sessions)});
   const sm=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])(\/stream)?$/);
   if(sm&&sm[2]){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});res.write(':ok\n\n');streams.push(res);req.on('close',()=>streams.splice(streams.indexOf(res),1));return;}
   if(sm)return json(res,200,{session:sessions[sm[1]],body:bodies[sm[1]],conversation:{key:'conv-'+sm[1]},capabilities:caps,supervision:{adapter:'hermes-oneshot',state:'unknown',evidence:'x',capabilities:caps,runs:[]},outputs:[],queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]});
   if(p==='/api/chat/inbox')return json(res,404,{});
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
 return {server,state,log};
}

(async()=>{
 const {server}=makeStub();await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const push=(ev,data)=>fetch(base+'/__push?ev='+ev+'&data='+encodeURIComponent(JSON.stringify(data||{})));
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const phone=(w,h)=>({viewport:{width:w,height:h},isMobile:true,hasTouch:true});
 const openThread=async(page,route='/#/chat/a/alfred/a')=>{await page.goto(base+route);await page.locator('#chatTranscript .chat-turn').first().waitFor();};
 try{
  // 1. viewport grid
  for(const theme of ['light','jarvis'])for(const w of [320,390,412])for(const h of [844,568,400]){
   const ctx=await browser.newContext(phone(w,h));
   if(theme==='jarvis')await ctx.addInitScript(()=>localStorage.setItem('manifest.theme','jarvis'));
   const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await openThread(page);
   const m=await page.evaluate(()=>{const ta=document.querySelector('#chatComposer textarea');const c=document.querySelector('#chatComposer').getBoundingClientRect();return {sw:document.documentElement.scrollWidth,vw:document.documentElement.clientWidth,font:parseFloat(getComputedStyle(ta).fontSize),bottom:c.bottom,vh:innerHeight,theme:document.documentElement.dataset.theme||'light'};});
   const at=theme+' '+w+'x'+h;
   assert.equal(m.theme,theme,at);assert.equal(m.sw,m.vw,at+' horizontal overflow');assert.ok(m.font>=16,at+' composer font '+m.font);assert.ok(m.bottom<=m.vh+0.5,at+' composer off screen');assert.deepEqual(errors,[],at);
   await ctx.close();
  }
  console.log('viewport grid: 18 phone viewports, both themes, no overflow, 16px field, composer on screen');
  // 2. Back / Escape with the Chats list open
  {const ctx=await browser.newContext(phone(390,844));const page=await ctx.newPage();
   await page.goto(base+'/#/settings');await page.waitForTimeout(300);
   await page.evaluate(()=>{location.hash='#/chat/a/alfred/a';});await page.locator('#chatTranscript .chat-turn').first().waitFor();
   const listOpen=()=>page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open'));
   await page.locator('.mf-chat-back').click();assert.equal(await listOpen(),true);
   await page.goBack();await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>location.hash),'#/chat/a/alfred/a','Back must close the list, not leave the thread');
   assert.equal(await listOpen(),false);
   await page.locator('.mf-chat-back').click();await page.locator('.mf-chat-toggle').click();await page.waitForTimeout(200);
   assert.equal(await listOpen(),false);
   await page.locator('.mf-chat-back').click();await page.keyboard.press('Escape');await page.waitForTimeout(200);
   assert.equal(await listOpen(),false,'Escape closes the list');
   assert.equal(await page.evaluate(()=>document.activeElement.className),'mf-chat-back','focus returns to ‹ Chats');
   await page.goBack();await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>location.hash),'#/settings','closing from the page leaves no extra history entry');
   await ctx.close();}
  console.log('phone Back closes the open Chats list; toggle/Escape close it without a stray history entry');
  // 3. streaming reading position
  {const ctx=await browser.newContext(phone(390,844));const page=await ctx.newPage();await openThread(page);await page.waitForTimeout(300);
   await page.evaluate(()=>{const h=document.getElementById('chatTranscript');h.dispatchEvent(new Event('touchstart',{bubbles:true}));h.scrollTop=h.scrollHeight/3;h.dispatchEvent(new Event('scroll'));});
   await page.waitForTimeout(300);
   const anchor=()=>page.evaluate(()=>{const h=document.getElementById('chatTranscript'),hr=h.getBoundingClientRect();const t=[...h.querySelectorAll('.chat-turn')].find(t=>t.getBoundingClientRect().bottom>hr.top+4);return {text:t.textContent.slice(0,40),top:Math.round(t.getBoundingClientRect().top-hr.top)};});
   const before=await anchor();
   await push('turn.started');await page.waitForTimeout(150);
   for(let i=0;i<10;i++){await push('assistant.delta',{text:'streamed words '.repeat(20)+'\n\n'});await page.waitForTimeout(60);}
   await page.waitForTimeout(300);
   assert.deepEqual(await anchor(),before,'streaming moved the reader');
   await page.evaluate(()=>{const h=document.getElementById('chatTranscript');h.scrollTop=h.scrollHeight;});await page.waitForTimeout(200);
   for(let i=0;i<6;i++){await push('assistant.delta',{text:'more '.repeat(40)+'\n\n'});await page.waitForTimeout(60);}
   await page.waitForTimeout(300);
   assert.ok(await page.evaluate(()=>{const h=document.getElementById('chatTranscript');return h.scrollHeight-h.scrollTop-h.clientHeight<4;}),'bottom-follow lost');
   await ctx.close();}
  console.log('streaming keeps the reading position in history and follows at the bottom');
  // 4. desktop never shows the phone toggle
  {const ctx=await browser.newContext({viewport:{width:1280,height:900}});const page=await ctx.newPage();await openThread(page);
   assert.equal(await page.evaluate(()=>getComputedStyle(document.querySelector('.mf-chat-toggle')).display),'none');
   await ctx.close();}
  console.log('desktop 1280: phone toggle hidden');
 }finally{await browser.close();server.close();}
 process.exit(0);
})().catch(e=>{console.error(e);process.exit(1);});
