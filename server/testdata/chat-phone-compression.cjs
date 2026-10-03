// chat-phone-compression.cjs — the phone compression rules
// (docs/ui-conventions.md, 2026-10-03) on the real front end over the stub
// chat API, at 320 and 390, plus desktop unchanged at 1440:
//   rule 2  the head is ‹ Chats · title · ＋ · ··· and the title has the room
//           (the owner's coding head gave it one letter, Stop a letter a line);
//   rule 3  +N −M sits on the status line above the composer, not in the head;
//   rules 4–5, 8  in a conversation the composer at rest is ＋ · field · mic ·
//           send; model · effort fold into ＋, a risky permission stays; the
//           new-chat landing keeps its chips;
//   rule 1  every fold still works from its home: ＋ opens files, model and
//           permissions, and the model row opens the picker;
//   rule 6  ··· names the conversation first, Delete last in --danger, the
//           facts after it.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-phone-compression.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const {server}=makeStub({codex:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const open=async(w,route)=>{const ctx=await browser.newContext({viewport:{width:w,height:844},isMobile:w<861,hasTouch:w<861});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(base+route);await page.locator('#chatComposer textarea').waitFor();await page.waitForTimeout(600);return {ctx,page,errors};};
 const shown=(page,sel)=>page.evaluate(s=>[...document.querySelectorAll(s)].filter(e=>e.getClientRects().length&&getComputedStyle(e).visibility!=='hidden').length,sel);
 try{
  for(const w of [320,390]){
   // a coding conversation: head, status line, composer, ＋, ···
   {const {ctx,page,errors}=await open(w,'/#/chat/a/codex/cx1');const at='codex at '+w;
    await page.locator('#chatStatusLine .chat-changes-chip').waitFor({timeout:5000});
    const g=await page.evaluate(()=>{const R=e=>e.getBoundingClientRect(),head=document.querySelector('#chatThreadHeader .chat-head'),c=document.getElementById('chatComposer'),f=c.querySelector('textarea');
     const vis=[...head.children].filter(e=>e.getClientRects().length);const rowOf=e=>Math.round(R(e).top+R(e).height/2);
     const kids=[...c.children].filter(e=>e.getClientRects().length&&!e.matches('.chat-composer-status,.chat-chip-menu'));
     return {head:vis.map(e=>e.className.split(' ').find(k=>/^(mf-|chat-)/.test(k))),title:R(head.querySelector('.chat-head-title')).width,
      first:kids.filter(e=>rowOf(e)===rowOf(f)).map(e=>e.matches('textarea')?'field':e.matches('.chat-attach')?'attach':e.matches('.mic-btn')?'mic':e.matches('.chat-send')?'send':e.className),
      model:!!c.querySelector('.chat-composer-model')?.getClientRects().length,perm:!!c.querySelector('.chat-composer-permission.is-danger')?.getClientRects().length,
      overflow:document.documentElement.scrollWidth>innerWidth};});
    assert.deepEqual(g.head,['mf-chat-back','chat-head-title','mf-chat-new','chat-details'],at+': head holds ‹ · title · ＋ · ···');
    assert.ok(g.title>=(w<390?60:120),at+': title squeezed to '+g.title);
    assert.equal(g.overflow,false,at+': sideways pan');
    assert.deepEqual(g.first.sort(),['attach','field','mic','send'],at+': first row is ＋ · field · mic · send');
    assert.equal(g.model,false,at+': the model chip folds into ＋');
    assert.equal(g.perm,true,at+': Full access stays visible');
    // ＋ holds the folds and each works
    await page.locator('#chatComposer .chat-attach').click();
    const menu=page.getByRole('dialog',{name:'Add to message'});await menu.waitFor();
    assert.deepEqual(await menu.locator('.chat-chip-row-label').allTextContents(),['Attach files','Model and effort','Permissions'],at);
    await menu.getByRole('option',{name:/Model and effort/}).click();
    await page.locator('.chat-model-picker').waitFor();assert.equal(await shown(page,'.chat-chip-menu'),0,at+': ＋ closes when the picker opens');
    await page.keyboard.press('Escape');
    // ··· is a list of actions
    await page.locator('#chatThreadHeader .chat-details > summary').click();
    const m=await page.evaluate(()=>{const d=document.querySelector('#chatThreadHeader .chat-details[open]'),kids=[...d.children];const acts=d.querySelector(':scope > .chat-head-acts'),del=d.querySelector('.chat-delete-action');
     const danger=getComputedStyle(document.documentElement).getPropertyValue('--danger').trim(),probe=document.createElement('i');probe.style.color=danger;document.body.append(probe);const want=getComputedStyle(probe).color;probe.remove();
     return {first:d.querySelector('summary').innerText.trim(),stopAfterWorkspace:(()=>{const w=d.querySelector(':scope > .mf-chat-ws-more'),st=d.querySelector(':scope > .chat-stop-agent');return !st||!w||kids.indexOf(st)>kids.indexOf(w);})(),lastAct:[...acts.children].filter(e=>e.getClientRects().length).at(-1)?.textContent,delColor:getComputedStyle(del).color,want,
      factsAfter:kids.filter(e=>e.matches('.chat-conversation-info,.chat-head-sub,.chat-head-meta')).every(e=>kids.indexOf(e)>kids.indexOf(acts)),changesInMenu:!!d.querySelector('.chat-changes-chip')?.getClientRects().length};});
    assert.equal(m.first,'Fix the flaky test',at+': ··· names the conversation first');
    assert.equal(m.lastAct,'Delete chat…',at+': Delete is last');assert.equal(m.delColor,m.want,at+': Delete in --danger');
    assert.ok(m.stopAfterWorkspace,at+': Stop after the plain actions');assert.ok(m.factsAfter,at+': facts after the actions');assert.equal(m.changesInMenu,false,at+': +N −M said once');
    assert.deepEqual(errors,[],at);await ctx.close();}
   // a native conversation: no chip row at all
   {const {ctx,page,errors}=await open(w,'/#/chat/a/alfred/b');const at='alfred at '+w;
    const g=await page.evaluate(()=>{const c=document.getElementById('chatComposer'),R=e=>e.getBoundingClientRect(),f=c.querySelector('textarea');
     return {rows:new Set([...c.children].filter(e=>e.getClientRects().length&&R(e).height>0).map(e=>Math.round(R(e).top+R(e).height/2))).size,field:R(f).height,title:R(document.querySelector('#chatThreadHeader .chat-head-title')).width};});
    assert.equal(g.rows,1,at+': the composer at rest is one row');
    assert.ok(g.title>=60,at+': title '+g.title);
    assert.deepEqual(errors,[],at);await ctx.close();}
   // the landing is where the choice is made: chips in view
   {const {ctx,page}=await open(w,'/#/chat/a/claude/new');
    assert.ok(await shown(page,'#chatComposer .chat-composer-model')===1&&await shown(page,'#chatComposer .chat-landing-chip')>=1,'landing at '+w+': chips in view');
    await page.locator('#chatComposer .chat-attach').click();assert.equal(await shown(page,'.chat-chip-menu'),0,'landing at '+w+': ＋ attaches directly');
    await ctx.close();}
  }
  // desktop renders as before: chips in the composer, ＋ attaches, +N −M in the head
  {const {ctx,page}=await open(1440,'/#/chat/a/codex/cx1');
   assert.equal(await shown(page,'#chatComposer .chat-composer-model'),1,'desktop: model chip');
   assert.equal(await shown(page,'#chatStatusLine .chat-status-changes'),0,'desktop: no status-line changes');
   await page.locator('#chatComposer .chat-attach').click();assert.equal(await shown(page,'.chat-chip-menu'),0,'desktop: ＋ attaches directly');
   await ctx.close();}
  console.log('PASS: phone compression — head ‹·title·＋····, +N −M on the status line, one-row composer with folds in ＋ (risky permission stays), landing chips, ··· titled with Delete last in danger; desktop unchanged — 320/390/1440.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
