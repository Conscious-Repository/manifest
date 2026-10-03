// chat-single-view-pass2.cjs — the real front end over the chat stub in
// headless Chromium, pinning the second single-chat UI pass (2026-09-27; an
// internal-consistency and platform-convention pass, no reference capture).
// Browser viewports, not a phone:
//   1. 320/390/412: one header. With a conversation open the phone top bar
//      (☰ CHAT ⌕ ＋) folds away and the head carries ‹ Chats, the title,
//      ＋ New chat and ···, each a 44px target; the shell refits to the
//      viewport (the two rows took 113px); the Chats list brings the bar back;
//   2. 390: the owner's sent words are the replies' body size (were 13px);
//   3. 390: the model chip folds into ＋ (2026-10-03; it was a 350px row, then
//      a ≥44px chip), ＋ is ≥44px;
//   4. 390, after a send: the queued state is stated once — the run-state line
//      "Queued · accepted, not started" — not five times; the cancel is a
//      44×44 target named for what it cancels; while another turn runs the
//      message keeps its own queued row (pass 3 moved the state, Edit — once
//      "↑ to edit" — and Cancel under the bubble: chat-mobile-pass3.cjs) and
//      nothing floats at the transcript's foot;
//   5. a private thread answers as its own agent (the "Choose agent" hand-off
//      was retired 2026-09-27): no recipient chip in the input row and no
//      "Agent:" control in the head, with or without coding agents on;
//   6. 320/390/412 idle and one-recipient: no VISIBLE control under 44px
//      (visible: a box in the viewport, not inert, not inside a closed
//      <details>, and the hit target at its own centre — ui-compare's count
//      includes the closed menu and the off-canvas drawer).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-single-view-pass2.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
const census=()=>{const out=[];for(const e of document.querySelectorAll('button,a[href],summary,input:not([type=hidden]),select,textarea,[role=button],[tabindex]:not([tabindex="-1"])')){
 const r=e.getBoundingClientRect();if(r.width<1||r.height<1)continue;if(r.bottom<=0||r.top>=innerHeight||r.right<=0||r.left>=innerWidth)continue;
 const cs=getComputedStyle(e);if(cs.visibility==='hidden'||+cs.opacity===0||e.closest('[inert]'))continue;
 const d=e.parentElement?.closest('details:not([open])');if(d&&d.querySelector(':scope>summary')!==e)continue;
 const hit=document.elementFromPoint(Math.min(innerWidth-1,Math.max(0,r.left+r.width/2)),Math.min(innerHeight-1,Math.max(0,r.top+r.height/2)));
 if(!hit||!(e===hit||e.contains(hit)||hit.contains(e)))continue;
 out.push({t:(e.getAttribute('aria-label')||e.textContent||e.placeholder||e.tagName).trim().slice(0,40),w:Math.round(r.width),h:Math.round(r.height)});}
 return out;};
(async()=>{
 const stubs=[];let main=null;const serve=async opts=>{const stub=makeStub(opts);main=main||stub;stubs.push(stub.server);await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));return 'http://127.0.0.1:'+stub.server.address().port;};
 const base=await serve(),solo=await serve({terminal:false});
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const phone=width=>({viewport:{width,height:844},isMobile:true,hasTouch:true});
 const open=async(page,url)=>{await page.goto(url);await page.locator('#chatTranscript .chat-turn').first().waitFor();await page.waitForTimeout(400);};
 const box=(page,sel)=>page.locator(sel).first().evaluate(e=>{const r=e.getBoundingClientRect();return {w:r.width,h:r.height,x:r.left,y:r.top};});
 const shown=(page,sel)=>page.evaluate(s=>{const e=document.querySelector(s);return !!e&&e.getBoundingClientRect().height>0&&getComputedStyle(e).display!=='none';},sel);
 try{
  // 1 + 6. one header, and the visible-target census, at three phone widths
  for(const width of [320,390,412]){
   const ctx=await browser.newContext(phone(width)),page=await ctx.newPage();
   await open(page,base+'/#/chat/a/alfred/a');
   assert.equal(await shown(page,'#crumbBar'),false,width+': the top bar folds under an open conversation');
   const head=await box(page,'#chatThreadHeader');
   assert.ok(head.y<=1&&head.y+head.h<=70,width+': chrome above the conversation ends at '+(head.y+head.h)+'px (was 113)');
   for(const sel of ['#chatThreadHeader .mf-chat-back','#chatThreadHeader .mf-chat-new','#chatThreadHeader .chat-options-compact > summary']){
    const b=await box(page,sel);assert.ok(b.w>=44&&b.h>=44,width+' '+sel+' is '+b.w+'×'+b.h);assert.ok(b.x+b.w<=width,width+' '+sel+' off screen');
   }
   const bottom=await page.evaluate(()=>document.querySelector('.chat-shell').getBoundingClientRect().bottom);
   assert.ok(844-bottom<=20,width+': the shell was not refitted when the bar folded ('+Math.round(844-bottom)+'px left under it)');
   const small=(await page.evaluate(census)).filter(c=>c.w<44||c.h<44);
   assert.deepEqual(small,[],width+': visible controls under 44px');
   // the list is where ☰ and ⌕ live: it brings the bar back
   await page.evaluate(()=>mf.openChats());await page.waitForTimeout(300);
   assert.equal(await shown(page,'#crumbBar .mf-menu'),true,width+': the Chats list shows the top bar and its ☰');
   await ctx.close();
  }
  {
   const ctx=await browser.newContext(phone(390)),page=await ctx.newPage();
   await open(page,base+'/#/chat/a/alfred/a');
   // ＋ in the head starts a new chat, as the bar's ＋ did
   await page.locator('#chatThreadHeader .mf-chat-new').click();
   await page.waitForFunction(()=>/\/new$/.test(location.hash));
   await open(page,base+'/#/chat/a/alfred/a');
   // 2. sent words at the body size
   const fs=await page.evaluate(()=>({user:getComputedStyle(document.querySelector('#chatTranscript .chat-user')).fontSize,say:getComputedStyle(document.querySelector('#chatTranscript .chat-say')).fontSize,field:getComputedStyle(document.querySelector('#chatComposer textarea')).fontSize}));
   assert.deepEqual(fs,{user:'16px',say:'16px',field:'16px'},'sent, reply and composer text sizes');
   // 3. the model chip's target was the chip (a 350px row before); since
   // 2026-10-03 a phone conversation folds it into ＋ (docs/ui-conventions.md,
   // phone rule 4), and ＋ is the 44px target
   assert.equal(await page.locator('#chatComposer .chat-composer-model').evaluate(e=>e.getClientRects().length),0,'the model chip folds into ＋');
   const plus=await page.locator('#chatComposer .chat-attach').evaluate(e=>{const r=e.getBoundingClientRect();return {w:r.width,h:r.height};});
   assert.ok(plus.w>=44&&plus.h>=44,'＋ is '+plus.w+'×'+plus.h);
   // 5. the hand-off is retired (2026-09-27): a private thread answers as its
   // own agent, so no recipient chip even with coding agents on
   assert.equal(await page.locator('#chatComposer .chat-composer-recipient').count(),0,'no recipient chip in a private thread');
   await ctx.close();
  }
  // 4. the queued state, stated once
  {
   const ctx=await browser.newContext(phone(390)),page=await ctx.newPage();
   await open(page,base+'/#/chat/a/alfred/b');
   await page.locator('#chatComposer textarea').fill('Please check the build');await page.locator('#chatComposer .chat-send').click();
   await page.locator('#chatTranscript .chat-run-state').waitFor();await page.locator('.chat-queued-control > .chat-queued-cancel').waitFor();await page.waitForTimeout(300);
   const says=await page.evaluate(()=>[...document.querySelectorAll('.chat-shell *')].filter(e=>e.getBoundingClientRect().height>0&&[...e.childNodes].some(n=>n.nodeType===3&&/queued/i.test(n.textContent))).map(e=>e.textContent.trim()));
   assert.deepEqual(says,['Queued · accepted, not started'],'the queued state is stated once');
   assert.equal(await page.locator('#chatComposer textarea').getAttribute('placeholder'),"Can't steer; messages queue…",'capability truth stays in the field');
   assert.equal(await page.locator('#chatTranscript .chat-user.is-queued + .chat-turn-receipt .chat-queued-edit').textContent(),'Edit');
   const cancel=page.getByRole('button',{name:'Cancel queued instruction: Please check the build',exact:true});
   const cb=await cancel.evaluate(e=>{const r=e.getBoundingClientRect();return {w:r.width,h:r.height};});
   assert.ok(cb.w>=44&&cb.h>=44,'cancel is '+cb.w+'×'+cb.h);
   // while another turn runs the thread says Working; the message's own row
   // still says it is queued (the delivery's state, not the thread's)
   {const b=main.sessions.b;b.deliveries=[{id:'run-0',state:'running',text:'earlier'},...b.deliveries];b.supervision={...b.supervision,state:'running',runs:[{id:'run-0',state:'running'}]};b.updated=new Date().toISOString();}
   await page.locator('#chatTranscript .chat-native-stop').waitFor({timeout:8000});
   assert.equal(await page.locator('#chatTranscript .chat-user.is-queued + .chat-turn-receipt .chat-run-state').textContent(),'Queued · accepted, not started','the queued message keeps its state');
   assert.equal(await page.locator('#chatLiveArea .chat-run-state').count(),0,'a running thread has no floating queued line');
   await ctx.close();
  }
  // 5. the only possible recipient: no chip in the input row, the head names it
  for(const width of [320,390,412]){
   const ctx=await browser.newContext(phone(width)),page=await ctx.newPage();
   await open(page,solo+'/#/chat/a/alfred/a');
   assert.equal(await page.locator('#chatComposer .chat-composer-recipient').count(),0,width+': a one-recipient thread spends no row on the chip');
   assert.equal(await page.locator('#chatThreadHeader .chat-recipient-control').count(),0,width+': no "Agent:" hand-off control in the head');
   const small=(await page.evaluate(census)).filter(c=>c.w<44||c.h<44);
   assert.deepEqual(small,[],width+': visible controls under 44px (one recipient)');
   await ctx.close();
  }
  console.log('PASS: single chat view pass 2 — one phone header, body-size sent text, chip-sized target, queued stated once, one-recipient chip, no visible target under 44px');
 }finally{await browser.close();for(const s of stubs)s.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
