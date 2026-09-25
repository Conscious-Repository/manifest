// chat-stage-switch-browser.cjs — the real front end (index.html + every
// script) over a stub API, driven through thread switches (2026-09-12):
//   1. leaving thread A for an unseen thread B clears A's turns from the stage
//      within a frame — long before B's payload lands — and B's title heads the
//      stage from the list row; B's fetch starts at once, not behind the lists;
//   2. coming back to A paints A's turns synchronously from the stage cache,
//      and the revalidating fetch, finding nothing changed, leaves that paint
//      (its DOM nodes) untouched.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-stage-switch-browser.cjs
const {chromium,devices}=require('playwright'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const LIST_DELAY=300,THREAD_DELAY=400;
const overrides={};
const thread=id=>overrides[id]||({session:{id,title:'Thread '+id.toUpperCase(),status:'idle',agent:'alfred',turns:2,updated:'2026-09-12T10:0'+(id==='a'?1:2)+':00Z',created:'2026-09-12T10:00:00Z',spentUsd:0},
 body:'## Turn 1 — user · 2026-09-12T10:00:00Z\n\nhello from '+id.toUpperCase()+'\n\n## Turn 2 — alfred · 2026-09-12T10:01:00Z\n\nreply from '+id.toUpperCase(),
 conversation:{key:'conv-'+id},outputs:id==='a'?[{delivery:'native-output-request',replyTurn:2,hash:'c'.repeat(64)}]:[],queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]});
const log=[];
const json=(res,code,body,delay=0)=>setTimeout(()=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));},delay);
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://x');const p=url.pathname;
 if(p.startsWith('/api/')){
  log.push({p,at:Date.now()});
  if(p==='/api/agents/chat/roster')return json(res,200,{agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]});
  if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:[thread('a').session,thread('b').session]},LIST_DELAY);
  const m=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])$/);if(m)return json(res,200,thread(m[1]),THREAD_DELAY);
  if(p==='/api/agents/chat/alfred/sessions/missing')return json(res,404,{});
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
  const retainedOutputs=[];
  await page.route('**/api/agents/chat/alfred/sessions/a/output',route=>{retainedOutputs.push(route.request().postDataJSON());return route.fulfill({json:{id:'cccccccccccccccc',revision:'c'.repeat(64)}});});
  await page.route('**/api/artifacts/get?**',route=>route.fulfill({json:{id:'cccccccccccccccc',title:'Saved native output',ref:'output.md',head:'c'.repeat(64),content:'reply from A',preview:{kind:'text'},provenance:{source:'chat-output',session:'conv-a',delivery:'native-output-request'},revisions:[{n:1,hash:'c'.repeat(64),actor:'alfred'}]}}));
  await page.getByRole('button',{name:'Save output',exact:true}).click();
  const outputPane=page.getByRole('complementary',{name:'Artifact workspace',exact:true});
  await outputPane.getByText('reply from A',{exact:true}).waitFor();
  assert.deepEqual(retainedOutputs,[{delivery:'native-output-request',hash:'c'.repeat(64)}]);
  await outputPane.getByText('Origin and version details',{exact:true}).click();
  await outputPane.getByText('native-output-request',{exact:true}).waitFor();
  await page.screenshot({path:'/tmp/manifest-native-output-artifact.png'});
  await page.getByRole('button',{name:'Hide workspace',exact:true}).click();
  // Receipt state drives both waiting indicator and composer guidance.
  const queued=thread('a');queued.session.status='thinking';queued.session.deliveries=[{id:'queued-request',state:'queued',text:'Pending instruction'}];
  await page.evaluate(d=>{clearInterval(chatPollTimer);chatPollTimer=null;window.pendingFixture=d;chatLive=null;renderChatTranscript(d);renderChatComposer(d.session);},queued);
  await page.getByText('✦ Queued…',{exact:true}).waitFor();
  await page.getByPlaceholder('✦ Queued — messages queue…',{exact:true}).waitFor();
  await page.evaluate(()=>{pendingFixture.session.deliveries.unshift({id:'running-request',state:'running'});renderChatTranscript(pendingFixture);renderChatComposer(pendingFixture.session);});
  await page.getByText('✦ Working…',{exact:true}).waitFor();
  await page.getByPlaceholder('✦ Working — messages queue…',{exact:true}).waitFor();
  const interruptions=[];
  await page.route('**/api/agents/chat/alfred/sessions/a/interrupt',route=>{interruptions.push(route.request().postDataJSON());return route.fulfill({status:409,body:'Fixture interruption target changed'});});
  await page.locator('#chatComposer textarea').focus();
  await page.keyboard.press('Control+Alt+x');await page.keyboard.press('Control+Alt+x');
  const interrupt=page.getByRole('button',{name:'Interrupt turn and cancel 1 queued',exact:true});
  assert.equal(await interrupt.evaluate(e=>e===document.activeElement),true);
  assert.equal(await interrupt.getAttribute('aria-keyshortcuts'),'Control+Alt+x');
  assert.equal(interruptions.length,0,'stop shortcut focuses without submission');
  await page.keyboard.press('Enter');
  await page.getByText('Fixture interruption target changed',{exact:true}).waitFor();
  assert.deepEqual(interruptions,[{requestId:'running-request'}]);
  await page.evaluate(()=>{pendingFixture.session.deliveries[0].stopRequested=true;renderChatTranscript(pendingFixture);renderChatComposer(pendingFixture.session);});
  await page.getByText('✦ Interruption requested…',{exact:true}).waitFor();
  await page.getByPlaceholder('✦ Interruption requested — messages queue…',{exact:true}).waitFor();
  assert.deepEqual(errors.filter(e=>!/EventSource|terminal\/events/.test(e)),[]);
  // Polling must notice metadata-only changes from another device, even when
  // second-resolution timestamps and the running state are unchanged.
  const observed=thread('a');observed.session.status='thinking';observed.session.deliveries=[{id:'observed-run',state:'running',userTurn:1}];overrides.a=observed;
  await page.evaluate(d=>{renderChatTranscript(d);renderChatComposer(d.session);chatOpenContext();ensureChatPoll(d.session,0);},observed);
  const draft=page.locator('#chatComposer textarea');await draft.fill('Keep this unfinished instruction');
  await draft.evaluate(e=>{window.originalComposer=e;e.focus();e.setSelectionRange(5,9);});
  observed.session.deliveries[0].toolScope={source:'request',toolsets:'web,files'};
  await page.getByText('Toolset scope at dispatch (request): web,files',{exact:true}).waitFor();
  observed.session.deliveries[0].stopRequested=true;
  await page.getByText('Interruption requested; waiting for the runner to return.',{exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'Interrupt turn',exact:true}).isDisabled(),true);
  assert.equal(await draft.inputValue(),'Keep this unfinished instruction');
  assert.deepEqual(await draft.evaluate(e=>[e===originalComposer,e===document.activeElement,e.selectionStart,e.selectionEnd]),[true,true,5,9]);
  await page.evaluate(()=>{clearInterval(chatPollTimer);chatPollTimer=null;});
  assert.deepEqual(errors.filter(e=>!/EventSource|terminal\/events/.test(e)),[]);
  // 3. on a phone the way back is chrome: a thread that fails to load still shows "‹ Chats" (2026-09-21)
  const phone=await browser.newContext({...devices['iPhone 13']});const pp=await phone.newPage();
  const phoneErrors=[];pp.on('pageerror',e=>phoneErrors.push(e.message));
  await pp.goto(base+'/#/chat/a/alfred/missing',{waitUntil:'networkidle'}).catch(()=>{});await pp.waitForTimeout(600);
  assert.equal(await pp.locator('.chat-load-error').count()>0,true,'the missing thread paints its error state');
  assert.equal(await pp.locator('#chatThreadHeader .mf-chat-back').isVisible(),true,'the back control stands on a bare head when the thread failed');
  // A coding result stays attributed to its run while its exact captured
  // version becomes an unsent discussion context in the planning thread.
  const revision='a'.repeat(64),artifactID='0123456789abcdef',captures=[],mutations=[];
  let captureFailure=false,releaseCapture;
  const result={id:'coding-run',agent:'codex',task:'inbox/fence',outcome:'completed',body:'Coding deliverable from its own run.',hash:revision};
  const planning=thread('b');planning.codingResults=[result];overrides.b=planning;
  const artifact={id:artifactID,title:'Codex coding result',ref:'artifacts/runs/coding-run.md',head:'b'.repeat(64),provenance:{source:'run',run:result.id,task:result.task},revisions:[{n:1,hash:revision,actor:'codex',at:'2026-09-25T10:00:00Z'},{n:2,hash:'b'.repeat(64),actor:'owner',at:'2026-09-25T11:00:00Z'}]};
  await pp.route('**/api/**',async route=>{
   const req=route.request(),url=new URL(req.url());
   if(req.method()!=='GET')mutations.push(url.pathname);
   if(url.pathname.endsWith('/coding-result')){
    captures.push(req.postDataJSON());
    if(captureFailure){await new Promise(resolve=>releaseCapture=resolve);return route.fulfill({status:409,body:'The coding result changed. Reopen its current version.'});}
    return route.fulfill({json:{id:artifactID,revision,task:result.task}});
   }
   if(url.pathname==='/api/artifacts/get')return route.fulfill({json:{...artifact,content:url.searchParams.get('rev')===revision?'Captured original coding result.':'Later unselected result.',preview:{kind:'text'}}});
   if(url.pathname==='/api/artifacts/content')return route.fulfill({json:{...artifact,content:url.searchParams.get('rev')===revision?'Captured original coding result.':'Later unselected result.',preview:{kind:'text'}}});
   if(url.pathname==='/api/artifacts/reviews')return route.fulfill({json:{revision,entries:[],state:'not_requested',record_version:'0'}});
   return route.continue();
  });
  await pp.goto(base+'/#/chat/a/alfred/b');
  await pp.getByText(result.body,{exact:true}).waitFor();
  const phoneDraft=pp.locator('#chatComposer textarea');await phoneDraft.fill('Check this result before accepting it.');
  await pp.getByRole('button',{name:'Open result / discuss',exact:true}).click();
  await pp.getByText('Captured original coding result.',{exact:true}).waitFor();
  assert.deepEqual(captures,[{agent:'codex',run:'coding-run',hash:revision}]);
  assert.equal(await pp.getByRole('combobox',{name:'Artifact version'}).inputValue(),'1');
  assert.equal(await pp.getByText('Later unselected result.',{exact:true}).count(),0);
  await pp.getByRole('button',{name:'Discuss',exact:true}).click();
  await pp.getByRole('button',{name:'Discussing: Codex coding result · v1',exact:true}).waitFor();
  assert.equal(await phoneDraft.inputValue(),'Check this result before accepting it.');
  assert.equal(await phoneDraft.evaluate(e=>e===document.activeElement),true);
  assert.equal(await pp.getByRole('button',{name:'Discussing: Codex coding result · v1',exact:true}).getAttribute('title'),'Exact revision '+revision);
  assert.equal(mutations.some(p=>p.endsWith('/messages')||p.endsWith('/input')||p==='/api/artifacts/text'),false,'review and discussion must not send or save');
  assert.equal(await pp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await pp.screenshot({path:'/tmp/manifest-coding-result-discussion-phone.png'});
  // An old capture failure cannot announce an error in another conversation.
  captureFailure=true;
  await pp.getByRole('button',{name:'Open result / discuss',exact:true}).click();
  for(let attempt=0;!releaseCapture&&attempt<100;attempt++)await new Promise(resolve=>setTimeout(resolve,10));
  assert.equal(typeof releaseCapture,'function','capture request reached the fixture');
  await pp.evaluate(()=>location.hash='#/chat/a/alfred/a');
  await pp.getByText('reply from A',{exact:true}).waitFor();
  const failedResponse=pp.waitForResponse(r=>r.url().endsWith('/coding-result'));
  releaseCapture();await failedResponse;
  await pp.waitForTimeout(150);
  assert.equal(await pp.getByText('The coding result changed. Reopen its current version.',{exact:true}).count(),0,'obsolete capture error leaked into another conversation');
  releaseCapture=null;
  await pp.evaluate(()=>location.hash='#/chat/a/alfred/b');
  await pp.getByText(result.body,{exact:true}).waitFor();
  await pp.getByRole('button',{name:'Open result / discuss',exact:true}).click();
  for(let attempt=0;!releaseCapture&&attempt<100;attempt++)await new Promise(resolve=>setTimeout(resolve,10));
  assert.equal(typeof releaseCapture,'function');releaseCapture();
  await pp.getByText('The coding result changed. Reopen its current version.',{exact:true}).waitFor();
  assert.equal(await pp.getByRole('button',{name:'Open result / discuss',exact:true}).isEnabled(),true,'current failure allows another attempt');
  assert.deepEqual(phoneErrors,[]);
  await phone.close();
  console.log('PASS: thread switching, receipt polling, phone result capture/exact discussion, and current-versus-obsolete capture failures.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1);});
