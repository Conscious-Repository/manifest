// chat-snappy.cjs — the real front end over the stub chat API, a 100-turn
// Codex chat served by the lite contract (server transcript_lite.go):
//   1. opening it reads lite with a tail: only the latest 40 turns paint, no
//      tool output is in the page, and "Show 60 earlier turns" stands on top;
//   2. reaching the top loads earlier turns and keeps the reader's place;
//   3. opening a step fetches its output once (then from memory);
//   4. a long pasted message folds to its first lines with Show more;
//   5. the list is Codex-simple: "New chat" and search on top, one Filters
//      control holding the list choice; the head's actions are icons that
//      keep their names; nothing overflows at 1440 or 390 in either theme.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),path=require('node:path');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub({longCodex:true});await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const log=async()=>(await fetch(base+'/__log').then(r=>r.json())).log;
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/codex/cx2');
  await page.locator('#chatTranscript .chat-term-out').first().waitFor();
  // 1. lite + tail
  const first=(await log()).find(l=>l.includes('/terminal/session/cx2/transcript'));
  assert.match(first,/lite=1/);assert.match(first,/tail=40/);
  assert.equal(await page.locator('#chatTranscript [data-chat-read-turn]').count(),40,'only the latest turns paint');
  assert.equal(await page.locator('#chatTranscript').evaluate(e=>e.textContent.includes('output of step')),false,'tool output must not be in the page until asked for');
  const older=page.getByRole('button',{name:'Show 60 earlier turns'});await older.waitFor({state:'attached'});
  // 2. reaching the top loads earlier turns, keeping the place
  const host=page.locator('#chatTranscript');
  const anchor=await page.evaluate(()=>{const t=document.getElementById('chatTranscript');t.scrollTop=0;const el=document.querySelector('[data-chat-read-turn="lu30"]');return el.getBoundingClientRect().top;});
  await page.waitForFunction(()=>document.querySelectorAll('#chatTranscript [data-chat-read-turn]').length===80,null,{timeout:5000});
  const after=await page.evaluate(()=>document.querySelector('[data-chat-read-turn="lu30"]').getBoundingClientRect().top);
  assert.ok(Math.abs(after-anchor)<4,'loading earlier turns moved the reader: '+anchor+' → '+after);
  assert.ok((await log()).some(l=>l.includes('/terminal/session/cx2/turns')&&l.includes('before=lu30')),'earlier turns asked for the ones before the first shown');
  // 3. step output on demand, once
  await page.evaluate(()=>{document.querySelector('[data-chat-read-turn="la49"]').scrollIntoView();});
  const act=page.locator('[data-chat-read-turn="la49"] .chat-term-activity > summary');await act.click();
  await page.locator('[data-chat-read-turn="la49"] .chat-term-step-details > summary').click();
  await page.locator('[data-chat-read-turn="la49"] .chat-term-result-text').filter({hasText:'output of step 49'}).waitFor();
  await page.locator('[data-chat-read-turn="la49"] .chat-term-step-details > summary').click();
  await page.locator('[data-chat-read-turn="la49"] .chat-term-step-details > summary').click();
  assert.equal((await log()).filter(l=>l==='STEP s49').length,1,'a step output is fetched once');
  // 4. long paste folds (turn lu1 is among the earlier ones: load to the start)
  await page.evaluate(()=>{document.getElementById('chatTranscript').scrollTop=0;});
  await page.waitForFunction(()=>!!document.querySelector('[data-chat-read-turn="lu1"]'),null,{timeout:5000});
  const paste=page.locator('[data-chat-read-turn="lu1"]');
  assert.equal(await paste.evaluate(e=>e.classList.contains('is-collapsed')),true,'a long paste should fold');
  const folded=await paste.evaluate(e=>e.getBoundingClientRect().height);
  await paste.getByRole('button',{name:'Show more'}).click();
  assert.ok(await paste.evaluate(e=>e.getBoundingClientRect().height)>folded*1.5,'Show more reveals the rest');
  await paste.getByRole('button',{name:'Show less'}).waitFor();
  // 5. Codex-simple list and head
  await page.getByRole('button',{name:'Start a new chat'}).waitFor();
  await page.getByRole('searchbox',{name:'Search chats'}).waitFor();
  assert.equal(await page.locator('#chatRail select:visible').count(),0,'no dropdowns on the list; they live behind Filters');
  await page.locator('#chatRail .chat-filter-menu > summary').click();
  await page.getByRole('combobox',{name:'Conversation list'}).waitFor();
  await page.locator('#chatRail .chat-filter-menu > summary').click();
  for(const name of ['New chat','Tiles'])assert.equal(await page.getByRole('button',{name,exact:true}).count(),1,name+' keeps its name as an icon');
  for(const theme of ['default','jarvis'])for(const width of [1440,390]){
   await page.setViewportSize({width,height:844});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.waitForTimeout(100);
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
   if(process.env.MANIFEST_FIXTURE_SHOTS)await page.screenshot({path:path.join(process.env.MANIFEST_FIXTURE_SHOTS,'snappy-'+theme+'-'+width+'.png')});
  }
  assert.deepEqual(errors,[]);
  console.log('PASS: lite tail open, earlier turns keep the place, step output once, long paste folds, Codex-simple list and head.');
  await ctx.close();
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
