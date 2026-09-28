// chat-codex-effort.cjs — the real front end over the stub chat API (its
// running Codex thread cx1). Codex 0.154 has no /effort: it answers
// "Unrecognized command" and leaves the text in its input, where the next
// message joins it (checked in a throwaway Codex TUI, 2026-09-28). So in a
// running Codex chat:
//   1. the picker shows the model fixed and effort read-only, and its action
//      opens Codex's own /model picker (model and effort) — "/model" is the
//      only thing sent;
//   2. a typed "/effort low" is not sent: it opens the picker instead.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const {server}=makeStub({codex:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[],sent=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/api/terminal/session/cx1/input',r=>{sent.push(r.request().postDataJSON().text);r.fulfill({status:200,contentType:'application/json',body:'{"ok":true,"delivery":{"id":"d1","state":"delivered"}}'});});
  await page.goto(base+'/#/chat/a/codex/cx1');
  const chip=page.locator('#chatComposer .chat-composer-model');await chip.waitFor();
  const picker=page.getByRole('dialog',{name:'Model, effort and permissions'});
  // 1. the picker: effort read-only, the action is Codex's own picker
  await chip.click();await picker.waitFor();
  assert.match(await picker.locator('.chat-model-fixed').textContent(),/set when this chat started.*New chat with this context/);
  const radios=picker.getByRole('radiogroup',{name:'Effort'}).getByRole('radio');
  assert.ok(await radios.count()>0,'the effort levels are shown');
  for(const r of await radios.all())assert.equal(await r.isEnabled(),false,'an effort is offered that Codex cannot take by command');
  assert.match(await picker.locator('.chat-model-status').textContent(),/Codex changes effort in its own \/model picker/);
  await picker.getByRole('button',{name:'Open Codex /model'}).click();
  await picker.waitFor({state:'detached'});
  await page.waitForFunction(()=>true);for(let i=0;i<30&&!sent.length;i++)await page.waitForTimeout(100);
  assert.deepEqual(sent,['/model'],'only Codex\'s own /model is sent');
  // 2. a typed /effort never reaches Codex
  const input=page.locator('#chatComposer textarea');
  await input.fill('/effort low');await input.press('Enter');
  await picker.waitFor();
  assert.equal(await input.inputValue(),'','the command leaves the field');
  await page.waitForTimeout(500);
  assert.deepEqual(sent,['/model'],'/effort was sent to Codex');
  await page.keyboard.press('Escape');await picker.waitFor({state:'detached'});
  assert.deepEqual(errors,[]);
  await ctx.close();
  console.log('PASS: a running Codex chat changes effort in its own /model picker; /effort is never sent to it.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
