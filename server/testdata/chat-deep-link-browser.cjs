// chat-deep-link-browser.cjs — the real front end over the stub chat API:
//   1. a cold #/chat/<id> for an Alfred thread (the spirits store answers
//      404) lands on #/chat/a/alfred/<id> and paints the transcript — no
//      failure block on the way;
//   2. an id no store holds says so truthfully, offers the way back, and
//      leaves no field to type into;
//   3. an outage while opening a thread shows what happened with Retry,
//      the composer closed; Retry after recovery loads the thread and the
//      composer comes back with the saved draft.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-deep-link-browser.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const hook=p=>fetch(base+p).then(r=>r.json());
 try{
  for(const width of [1440,390]){
   const ctx=await browser.newContext({viewport:{width,height:900},...(width<=860?{hasTouch:true,isMobile:true}:{})});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   // 1. cold deep link, bare route, agent-owned id
   await page.goto(base+'/#/chat/b');
   await page.locator('#chatTranscript .chat-turn').first().waitFor({timeout:5000});
   assert.equal(await page.evaluate(()=>location.hash),'#/chat/a/alfred/b','the bare route follows the owning store');
   assert.equal(await page.locator('#chatTranscript .chat-load-error').count(),0);
   assert.equal(await page.locator('#chatComposer textarea').count(),1,'a loaded thread has its composer');
   const log=(await hook('/__log')).log;
   assert.ok(log.includes('GET /api/chat/sessions/b')&&log.some(l=>l.startsWith('GET /api/chat/resolve?id=b'))&&log.includes('GET /api/agents/chat/alfred/sessions/b'),'spirits 404 → resolve → alfred');
   // 2. an id nobody holds
   await page.evaluate(()=>{location.hash='#/chat/zz-missing';});
   const box=page.locator('#chatTranscript .chat-load-error');await box.waitFor();
   await page.waitForFunction(()=>/No conversation has this id/.test(document.querySelector('#chatTranscript .chat-load-error')?.innerText||''));
   assert.equal(await box.getAttribute('role'),'alert');
   assert.match(await box.innerText(),/Every chat store was checked/);
   assert.doesNotMatch(await box.innerText(),/archived/);
   assert.equal(await page.locator('#chatComposer textarea').count(),0,'nothing to type into');
   assert.match(await page.locator('#chatComposer').innerText(),/did not load/);
   await box.getByRole('button',{name:'Back to Spirits'}).click();
   await page.waitForFunction(()=>location.hash==='#/chat/spirits');
   // 3. outage while opening; draft survives the closed composer
   await page.evaluate(()=>{location.hash='#/chat/a/alfred/a';});await page.locator('#chatComposer textarea').waitFor();
   await page.locator('#chatComposer textarea').fill('draft kept');await page.waitForTimeout(400);
   await page.evaluate(()=>{location.hash='#/chat/a/alfred/b';});await page.locator('#chatComposer textarea').waitFor();
   await page.waitForTimeout(300);
   await hook('/__down?on=1');
   await page.evaluate(()=>{chatStageCache.clear();location.hash='#/chat/a/alfred/a';});
   await page.waitForFunction(()=>/didn't load/.test(document.querySelector('#chatTranscript .chat-load-error')?.innerText||''));
   assert.equal(await page.locator('#chatComposer textarea').count(),0,'a failed load closes the composer');
   const retry=page.locator('#chatTranscript .chat-load-error').getByRole('button',{name:'Retry'});
   const rb=await retry.boundingBox();assert.ok(rb.height>=(width<=860?40:24),'Retry meets the '+(width<=860?'touch':'pointer')+' floor ('+rb.height+')');
   await page.waitForTimeout(600); // the dictation mic mounts 400 ms after a route change
   assert.equal(await page.locator('#chatComposer .mic-btn').count(),0,'a closed composer offers no dictation');
   const detail=await page.locator('#chatTranscript .chat-load-error-detail').evaluate(e=>getComputedStyle(e).color);
   assert.notEqual(detail,await page.locator('#chatTranscript .chat-load-error').evaluate(e=>getComputedStyle(e).backgroundColor),'detail ink differs from its ground');
   await hook('/__down?on=0');await retry.click();
   await page.locator('#chatTranscript .chat-turn').first().waitFor();
   await page.locator('#chatComposer textarea').waitFor();
   assert.equal(await page.locator('#chatComposer textarea').inputValue(),'draft kept','the rebuilt composer restores the draft');
   assert.equal(await page.locator('#chatComposer textarea').count(),1,'one composer, not two');
   await page.locator('#chatComposer .mic-btn').waitFor({timeout:3000});
   assert.equal(await page.locator('#chatComposer .mic-btn').count(),1,'the reopened composer has its mic back, once');
   assert.deepEqual(errors,[]);
   await ctx.close();
   console.log(width+'px: cold deep link follows the owner; not-found is truthful; failed load closes the composer and Retry restores it');
  }
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
