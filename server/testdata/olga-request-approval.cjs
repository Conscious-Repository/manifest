// olga-request-approval.cjs — Olga's requests for Benjamin arrive in the
// Approvals tab (2026-10-10, approvals.TypeOlgaRequest): her words as the
// title, Liber's write-up as the body, and Done / Won't do in place of
// Confirm / Reject, on a phone and on a desktop; an optional note to her rides
// either verdict (Done on the phone run, Won't do on the desktop run).
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./writing-stub-api.cjs');
const card={id:'4d1c',type:'olga-request',action:'Olga asked: Could would create a direct chat option?',agent:'olga',status:'pending',created:'2026-10-10T03:28:00Z',allowed:true,applyPath:'',
 body:'Olga asked Liber on 2026-10-09 22:28:\n\nCould would create a direct chat option?\n\nWhat it would take:\nA direct chat with Alfred needs a server change.\n'};
(async()=>{const stub=makeStub({json:{'/api/feed':{items:[],signals:[],proposals:[card],portalItems:[],consumeItems:[],receipts:[],bankPending:[],badge:1},'/api/feed/badge':{count:1}}});
 await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});let failed=0;
 try{for(const vp of [{width:390,height:844},{width:1440,height:900}]){
  const p=await browser.newPage({viewport:vp});const errors=[];p.on('pageerror',e=>errors.push(e.message));
  await p.goto(base+'/#/feed/approvals');
  const c=p.locator('[data-approval-id="4d1c"]');await c.waitFor({timeout:15000});
  const text=await c.innerText();
  assert.match(text,/Olga asked: Could would create a direct chat option\?/);
  assert.match(text,/What it would take:\s*A direct chat with Alfred needs a server change\./);
  const buttons=(await c.locator('button').allTextContents()).map(s=>s.trim()).filter(Boolean);
  assert.ok(buttons.includes('Done')&&buttons.includes('Won’t do'),'buttons: '+buttons.join(', '));
  assert.ok(!buttons.includes('Confirm')&&!buttons.includes('Reject'),'buttons: '+buttons.join(', '));
  assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),true,'no sideways scroll at '+vp.width);
  // the note to Olga rides the verdict; Won't do sends it as the reason (no picker)
  const posts=[];await p.route('**/api/spirits/approvals/**',r=>{posts.push([new URL(r.request().url()).pathname,r.request().postDataJSON()]);r.fulfill({status:200,contentType:'application/json',body:'{"ok":true}'})});
  const note=c.locator('textarea.appr-olga-note');
  assert.match(await note.getAttribute('placeholder'),/Note to Olga/);
  assert.ok(parseFloat(await note.evaluate(e=>getComputedStyle(e).fontSize))>=(vp.width<860?16:12),'note text size');
  await note.fill('Liber can look this up itself now.');
  await c.getByRole('button',{name:vp.width<860?'Done':'Won’t do'}).click();
  await p.waitForFunction(()=>true);await p.waitForTimeout(200);
  assert.deepEqual(posts[0],vp.width<860?['/api/spirits/approvals/4d1c/confirm',{note:'Liber can look this up itself now.'}]:['/api/spirits/approvals/4d1c/reject',{reason:'Liber can look this up itself now.'}]);
  assert.deepEqual(errors,[]);
  await p.close();console.log('ok',vp.width);
 }}catch(e){failed=1;console.error(e.message)}finally{await browser.close();stub.server.close()}
 process.exit(failed)})();
