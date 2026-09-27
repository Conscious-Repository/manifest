// Steer vs queue on a native agent (brief §5b.1, 2026-09-27), over the real
// front end and the chat stub. Alfred's adapter queues durably and cannot
// steer (TestChatAdapterCapabilityMatrix: hermes-oneshot steer "unsupported"),
// so while a turn runs the field says so in words, Tab queues like Enter,
// and ↑ in the empty composer takes the queued message out of the queue
// (cancel before dispatch) and back into the composer. With nothing running,
// Tab moves focus as usual.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const count=re=>stub.log.filter(l=>re.test(l)).length;
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/b');const ta=page.locator('#chatComposer textarea');await ta.waitFor();
  // idle: Tab moves focus, nothing is sent
  await ta.fill('idle text');await ta.press('Tab');
  assert.equal(await ta.evaluate(e=>document.activeElement===e),false,'Tab moves focus when nothing runs');
  assert.equal(count(/^POST .*\/messages/),0);
  // a turn runs: the field says Alfred cannot steer
  await fetch(base+'/__set?id=b&patch='+encodeURIComponent(JSON.stringify({status:'thinking'})));
  await page.waitForFunction(()=>/can.t steer/.test(document.querySelector('#chatComposer textarea')?.placeholder||''),null,{timeout:8000});
  assert.equal(await ta.getAttribute('placeholder'),'✦ Working — can\'t steer; messages queue…');
  // Tab queues (the durable queue: accepted, not started)
  await ta.fill('queue this');await ta.press('Tab');
  for(let i=0;i<40&&!count(/^POST .*\/sessions\/b\/messages/);i++)await page.waitForTimeout(100);
  assert.equal(count(/^POST .*\/sessions\/b\/messages/),1,'Tab queues the message');
  assert.equal(stub.sessions.b.deliveries.at(-1).state,'queued');
  await page.waitForFunction(()=>document.querySelector('#chatComposer textarea')?.value==='');
  // ↑ in the empty composer: out of the queue first, then back to edit
  await page.waitForTimeout(1800); // the poll brings the queued receipt into view
  const queued=page.locator('#chatTranscript .chat-user.is-queued');await queued.waitFor();
  // not sent yet, stated once: the row under the greyed bubble carries the
  // state and Edit, the ↑ shortcut as a control (pass 3; was a "↑ to edit" note)
  const edit=page.locator('#chatTranscript .chat-user.is-queued + .chat-turn-receipt .chat-queued-edit');
  assert.equal(await edit.textContent(),'Edit','a queued message reads as not sent yet');
  assert.equal(await edit.getAttribute('aria-keyshortcuts'),'ArrowUp','Edit names its shortcut');
  assert.equal(await page.locator('#chatTranscript .chat-run-state').textContent(),'Queued · accepted, not started','the thread says the message is accepted, not started');
  assert.ok(Number(await queued.evaluate(e=>getComputedStyle(e).opacity))<1,'and is greyed');
  await ta.focus();await ta.press('ArrowUp');
  await page.waitForFunction(()=>document.querySelector('#chatComposer textarea')?.value==='queue this');
  assert.equal(count(/^POST .*\/sessions\/b\/cancel-queued/),1);
  assert.equal(stub.sessions.b.deliveries.at(-1).state,'cancelled','the pulled-back message is no longer queued');
  await page.locator('#chatTranscript .chat-user.is-cancelled').getByText('Cancelled before dispatch').waitFor();
  assert.deepEqual(errors,[]);
  console.log('PASS: native steer vs queue — cannot-steer said in words, Tab queues, ↑ pulls back (cancelled first), Tab moves focus when idle.');
  await ctx.close();
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
