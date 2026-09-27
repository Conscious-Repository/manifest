// One composer-first new chat (brief §5a.1, 2026-09-27), over the real front
// end and the chat stub:
//   - "New chat" and Ctrl+Alt+N open the agent's landing, not a modal;
//   - agent, model, project (and for a coding agent the folder) are chips in
//     the composer; no Model select, no project select, no free-text folder;
//   - switching agent keeps what was typed (moved to the new landing's draft,
//     not left behind as a duplicate);
//   - the folder chip offers recent folders and the repos under ~/src;
//   - starters fill the composer without sending; recent chats open;
//   - on a phone the field keeps its row and the menu is a sheet on screen.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const posts=()=>stub.log.filter(l=>l.startsWith('POST ')&&(l.includes('/messages')||/\/sessions(\?|$)/.test(l)||l.includes('/api/terminal/session'))).length;
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/b');await page.locator('#chatTranscript [data-chat-read-turn]').first().waitFor();
  // 1. New chat opens the landing
  await page.getByRole('button',{name:'New chat',exact:true}).click();
  await page.waitForFunction(()=>location.hash==='#/chat/a/alfred/new');
  const agentChip=page.getByRole('button',{name:/^Agent: /});await agentChip.waitFor();
  assert.equal(await agentChip.getAttribute('aria-label'),'Agent: Alfred');
  await page.getByRole('button',{name:'Project: none'}).waitFor();
  assert.equal(await page.locator('#chatComposer .chat-composer-model').count(),1,'the model is a chip');
  assert.equal(await page.locator('#chatView select:not(.chat-layout-select):not(.chat-inbox-filter)').evaluateAll(s=>s.filter(x=>x.closest('.chat-main')).length),0,'no selects on the landing');
  assert.deepEqual(await page.locator('#chatLandingBelow .chat-landing-recent-title').allTextContents(),['Long research thread','Second thread']);
  // 2. a starter fills the composer, it does not send
  const before=posts();
  await page.locator('#chatLandingBelow .chat-landing-starter').first().click();
  assert.equal(await page.locator('#chatComposer textarea').inputValue(),'What is waiting on me today?');
  assert.equal(posts(),before,'a starter must not send');
  // 3. switching agent keeps what was typed, and moves it
  await page.locator('#chatComposer textarea').fill('hello there');
  await agentChip.click();const agents=page.getByRole('dialog',{name:'Agent'});await agents.waitFor();
  assert.deepEqual(await agents.locator('.chat-chip-row-label').allTextContents(),['Alfred','Claude Code','Codex','Spirits']);
  await agents.getByRole('option',{name:/^Codex/}).click();
  await page.waitForFunction(()=>location.hash==='#/chat/a/codex/new');
  await page.waitForFunction(()=>document.querySelector('#chatComposer textarea')?.value==='hello there');
  assert.equal(await page.locator('.chat-main input[aria-label="Working folder"], .chat-main select[aria-label="Model"]').count(),0,'no free-text folder, no duplicate Model select');
  // 4. the folder chip: recent folders and the repos under ~/src
  const folderChip=page.getByRole('button',{name:/^Folder: /});await folderChip.waitFor();
  assert.equal(await folderChip.getAttribute('aria-label'),'Folder: home folder');
  await folderChip.click();const folders=page.getByRole('dialog',{name:'Folder'});await folders.waitFor();
  assert.deepEqual(await folders.locator('.chat-chip-group').allTextContents(),['Recent','Repositories in ~/src']);
  assert.deepEqual(await folders.locator('.chat-chip-row-label').allTextContents(),['Home folder','manifest','lab-apps']);
  await folders.getByRole('option',{name:/^lab-apps/}).click();await folders.waitFor({state:'detached'});
  assert.equal(await page.evaluate(()=>localStorage.getItem('manifest.chatTermCwd.codex')),'/home/owner/src/lab-apps');
  assert.equal(await folderChip.getAttribute('aria-label'),'Folder: /home/owner/src/lab-apps');
  // the text moved: Alfred's landing no longer holds it
  await page.evaluate(()=>{location.hash='#/chat/a/alfred/new';});await page.getByRole('button',{name:'Agent: Alfred'}).waitFor();
  await page.waitForTimeout(400);
  assert.equal(await page.locator('#chatComposer textarea').inputValue(),'','the carried text must not stay behind');
  // 5. Ctrl+Alt+N from a conversation opens the same flow
  await page.evaluate(()=>{location.hash='#/chat/a/alfred/a';});await page.locator('#chatTranscript [data-chat-read-turn]').first().waitFor();
  await page.keyboard.press('Control+Alt+n');await page.waitForFunction(()=>location.hash==='#/chat/a/alfred/new');
  // 6. phone: the field keeps its row, the menu is a sheet on screen
  await page.setViewportSize({width:390,height:844});await page.waitForTimeout(200);
  const m=await page.evaluate(()=>{const c=document.getElementById('chatComposer').getBoundingClientRect(),t=document.querySelector('#chatComposer textarea').getBoundingClientRect();return {ratio:t.width/c.width,overflow:document.documentElement.scrollWidth>innerWidth};});
  assert.ok(m.ratio>=0.45&&!m.overflow,'phone landing: '+JSON.stringify(m));
  await page.getByRole('button',{name:/^Agent: /}).click();
  const sheet=await page.getByRole('dialog',{name:'Agent'}).evaluate(e=>{const b=e.getBoundingClientRect();return b.left>=0&&b.right<=innerWidth&&b.top>=0&&b.bottom<=innerHeight;});
  assert.ok(sheet,'the agent menu stays on screen');
  await page.keyboard.press('Escape');
  assert.deepEqual(errors,[]);
  console.log('PASS: one composer-first new chat — chips for agent/model/project/folder, carried draft, folder choices, starters, Ctrl+Alt+N, phone.');
  await ctx.close();
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
