// chat-mobile-pass3.cjs — the real front end over the chat stub at phone
// widths, pinning the third mobile pass (2026-09-27). Each number was
// measured on the rendered surface before the change (in parentheses; the
// captures are .ui-compare/pass3-before and pass3-after):
//   1. a queued send's run state lives with its message: one row directly
//      under the greyed bubble, ending on its edge, saying
//      "Queued · accepted, not started" once and holding Edit and Cancel
//      (the state floated 67px below the bubble at the transcript's foot, the
//      Cancel 10px below in another container, an 11px mono word);
//   2. Edit is the ↑ shortcut as a control, ≥44px, 13px type ("↑ to edit",
//      12px, 17px tall, not a control); it takes the message out of the queue
//      and puts its words back in the field;
//   3. a reply's meta row has one shape: ··· sits with who · when (208px of
//      empty row between them at 390) and its menu opens on screen;
//   4. one rhythm: 24–32px between turns (10px), a reply's meta 8–12px under
//      its text (19px, more than the gap to the next turn);
//   5. the composer after a send (a long run-state hint gives the field its
//      own row): the chips join the controls row, two rows, ≤100px at 390
//      (a phone conversation folds model · effort into ＋ since 2026-10-03;
//      what stays visible still shares the row)
//      (three rows, 148px), every target 44px;
//   6. one primary per region: send with text is the accent (ink #171717),
//      ＋ New chat in the head is not a second filled button (accent fill);
//      the composer's radius is the --radius-message token (26px).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-mobile-pass3.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
const phone=w=>({viewport:{width:w,height:844},isMobile:true,hasTouch:true});
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  for(const w of [320,390,412]){
   const stub=makeStub({codex:true});await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
   const ctx=await browser.newContext(phone(w)),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
   const at=' at '+w;
   await page.goto(base+'/#/chat/a/alfred/b');const ta=page.locator('#chatComposer textarea');await ta.waitFor();
   await page.locator('#chatTranscript .chat-turn-foot .chat-turn-more').waitFor();await page.waitForTimeout(300);
   // 3–4. the reply's meta row and the transcript's rhythm
   const idle=await page.evaluate(()=>{
    const R=e=>e.getBoundingClientRect(),ink=e=>{const g=document.createRange();g.selectNodeContents(e);const rs=[...g.getClientRects()].filter(r=>r.height>0);return {top:Math.min(...rs.map(r=>r.top)),bottom:Math.max(...rs.map(r=>r.bottom)),right:Math.max(...rs.map(r=>r.right))};};
    const tr=document.getElementById('chatTranscript'),reply=tr.querySelector('.chat-turn.chat-spirit'),foot=reply.querySelector('.chat-turn-foot');
    const text=Math.max(...[...reply.children].filter(e=>e!==foot&&e.offsetParent).map(e=>ink(e).bottom)),meta=ink(foot.querySelector('.chat-turn-meta'));
    const turns=[...tr.querySelectorAll(':scope > .chat-turn')].filter(e=>e.offsetParent);
    const composer=document.getElementById('chatComposer'),add=document.querySelector('#chatThreadHeader .mf-chat-new');
    return {textToMeta:meta.top-text,metaToMore:R(foot.querySelector('.chat-turn-more')).left-meta.right,
     gaps:turns.slice(1).map((e,i)=>R(e).top-R(turns[i]).bottom),radius:getComputedStyle(composer).borderTopLeftRadius,
     token:getComputedStyle(document.documentElement).getPropertyValue('--radius-message').trim(),addBg:getComputedStyle(add).backgroundColor,addColor:getComputedStyle(add).color};
   });
   assert.ok(idle.metaToMore>=0&&idle.metaToMore<=16,'··· sits '+idle.metaToMore+'px from who · when'+at);
   assert.ok(idle.textToMeta>=8&&idle.textToMeta<=12,'the meta is '+idle.textToMeta+'px under its text'+at);
   for(const g of idle.gaps)assert.ok(g>=24&&g<=32,'turns are '+g+'px apart'+at);
   assert.equal(idle.radius,idle.token,'the composer radius is --radius-message'+at);
   assert.equal(idle.addBg,'rgba(0, 0, 0, 0)','＋ New chat is not a second filled primary'+at);
   assert.equal(idle.addColor,'rgb(38, 90, 204)','＋ New chat keeps the accent glyph'+at);
   // the reply menu opens from the same edge, on screen
   await page.locator('#chatTranscript .chat-turn-more').click();
   const menu=await page.evaluate(()=>{const m=document.querySelector('#chatTranscript .chat-turn-foot.is-open .chat-turn-actions').getBoundingClientRect();return {l:m.left,r:m.right,vw:innerWidth};});
   assert.ok(menu.l>=0&&menu.r<=menu.vw,'the reply menu runs off screen '+JSON.stringify(menu)+at);
   await page.locator('#chatTranscript .chat-turn-more').click();
   // 6. send with text is the accent
   await ta.fill('x');
   assert.equal(await page.locator('#chatComposer .chat-send').evaluate(e=>getComputedStyle(e).backgroundColor),'rgb(38, 90, 204)','send with text is the accent'+at);
   // 1. a send: the run state under its own bubble
   await ta.fill('Please check the build');await page.locator('#chatComposer .chat-send').click();
   await page.locator('#chatTranscript .chat-user.is-queued + .chat-turn-receipt').waitFor();await page.waitForTimeout(400);
   const q=await page.evaluate(()=>{
    const R=e=>e.getBoundingClientRect(),row=document.querySelector('#chatTranscript .chat-turn-receipt'),bubble=row.previousElementSibling;
    const kids=[...row.children].filter(e=>e.offsetParent);const mid=e=>R(e).top+R(e).height/2;
    const says=[...document.querySelectorAll('.chat-shell *')].filter(e=>R(e).height>0&&[...e.childNodes].some(n=>n.nodeType===3&&/queued/i.test(n.textContent))).map(e=>e.textContent.trim());
    const btn=t=>{const b=kids.find(e=>e.tagName==='BUTTON'&&e.textContent===t);return b&&{w:R(b).width,h:R(b).height,fs:parseFloat(getComputedStyle(b).fontSize)};};
    return {bubble:bubble.matches('.chat-user.is-queued'),gap:R(row).top-R(bubble).bottom,rows:new Set(kids.map(e=>Math.round(mid(e)))).size,spread:Math.max(...kids.map(mid))-Math.min(...kids.map(mid)),
     edge:Math.abs(Math.max(...kids.map(e=>R(e).right))-R(bubble).right),state:row.querySelector('.chat-run-state')?.textContent,says,
     floating:document.querySelectorAll('#chatLiveArea .chat-run-state, .chat-native-interruption .chat-queued-control').length,
     edit:btn('Edit'),cancel:btn('Cancel'),oldNote:!!document.querySelector('#chatTranscript .chat-turn-queue-note')};
   });
   assert.ok(q.bubble,'the row follows its bubble'+at);
   assert.ok(q.gap>=0&&q.gap<=12,'the row is '+q.gap+'px under its bubble'+at);
   assert.ok(q.spread<=2,'the row is one line (centres spread '+q.spread+'px)'+at);
   assert.ok(q.edge<=2,'the row ends on the bubble\'s edge (off by '+q.edge+'px)'+at);
   assert.equal(q.state,'Queued · accepted, not started',at);
   assert.deepEqual(q.says,['Queued · accepted, not started'],'the queued state is stated once'+at);
   assert.equal(q.floating,0,'no run-state line or cancel floats apart from the message'+at);
   assert.equal(q.oldNote,false,'no "↑ to edit" note'+at);
   for(const [name,b] of [['Edit',q.edit],['Cancel',q.cancel]]){assert.ok(b,name+' is a control in the row'+at);assert.ok(b.w>=44&&b.h>=44,name+' is '+b.w+'×'+b.h+at);assert.ok(b.fs>=13,name+' is '+b.fs+'px'+at);}
   await page.getByRole('button',{name:'Edit queued instruction: Please check the build',exact:true}).waitFor();
   await page.getByRole('button',{name:'Cancel queued instruction: Please check the build',exact:true}).waitFor();
   // 5. the composer after a send: two rows, the chips beside + · mic · send
   await page.waitForFunction(()=>document.querySelector('#chatComposer textarea').placeholder==="Can't steer; messages queue…");
   await page.locator('#chatComposer .mic-btn').waitFor();await page.waitForTimeout(200);
   const c=await page.evaluate(()=>{
    const R=e=>e.getBoundingClientRect(),host=document.getElementById('chatComposer'),mid=e=>Math.round(R(e).top+R(e).height/2);
    const kids=[...host.children].filter(e=>e.offsetParent&&R(e).height>0&&!e.matches('.chat-composer-status'));
    const send=host.querySelector('.chat-send');
    return {h:R(host).height,rows:new Set(kids.map(mid)).size,chipsWithSend:[...host.querySelectorAll('.chat-composer-recipient,.chat-composer-model')].filter(e=>e.offsetParent).every(e=>Math.abs(mid(e)-mid(send))<=2),
     small:kids.filter(e=>e.matches('button')&&(R(e).width<44||R(e).height<44)).map(e=>e.className),overflow:document.documentElement.scrollWidth>innerWidth};
   });
   assert.equal(c.rows,2,'the composer holds '+c.rows+' rows'+at);
   assert.ok(c.chipsWithSend,'the chips share the controls row'+at);
   if(w===390)assert.ok(c.h<=100,'the composer is '+c.h+'px'+at);
   assert.deepEqual(c.small,[],'a composer control under 44px'+at);
   assert.equal(c.overflow,false,'horizontal overflow'+at);
   // 2. Edit: out of the queue, back into the field
   await page.getByRole('button',{name:'Edit queued instruction: Please check the build',exact:true}).click();
   await page.waitForFunction(()=>document.querySelector('#chatComposer textarea').value==='Please check the build');
   assert.equal(stub.sessions.b.deliveries.at(-1).state,'cancelled','Edit cancels before it hands the words back'+at);
   await page.locator('#chatTranscript .chat-user.is-cancelled').waitFor();
   assert.equal(await page.locator('#chatTranscript .chat-turn-receipt').count(),0,'the receipt leaves with the queue'+at);
   assert.deepEqual(errors,[],at);
   await ctx.close();stub.server.close();
  }
  console.log('PASS: queued state under its own bubble with 44px Edit/Cancel, meta row grouped, 24px turns / meta under its text, two-row composer after a send, accent send, quiet ＋, token radius — 320/390/412');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
