// chat-mobile-uiux.cjs — the real front end over the stub chat API (with its
// Codex thread, makeStub({codex:true})) at phone widths, pinning the
// 2026-09-27 mobile UI/UX pass. Each number was measured on the rendered
// surface before the change (in parentheses):
//   1. one row of chips: who the next message goes to, the model and the
//      permission share the row under the message; the first row is only
//      + · field · mic · send, so the field keeps ≥45% of the composer at 320
//      (Alfred 26%, the recipient chip sat in row one) and every chip is a
//      44px target; the recipient reads "Codex", not the raw kind "codex";
//   2. the phone head names the conversation: title ≥60px at 320 (0px, the
//      finished run state took the row), no horizontal overflow, the head's
//      Terminal icon 44px wide (36px);
//   3. once the field has its own row, send sits at the row's end (it sat
//      beside the mic, 119px from the left at 390);
//   4. a Codex send is on screen at once, says "sending…" until the server
//      accepts it (nothing for the length of the request), the busy send keeps
//      full ink (faded to 0.4 beside the text still in the field); words typed
//      on during the acknowledgement stay, the accepted ones leave the field
//      ("firstand one more" was re-sent whole); a refusal removes the echo and
//      keeps the draft;
//   5. Activity reads as a control: ≥13px, ≥7:1 contrast (12px, 5.4:1); the
//      context meter ≥13px with a 64px bar (11px, 48px);
//   6. a tail from a different provider run is its own reply, so "Worked for"
//      never spans two runs (chatTermMerge; the owner's phone showed "Worked
//      for 7h 13m" across 75 goal runs).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-mobile-uiux.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const {server}=makeStub({codex:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const phone=w=>({viewport:{width:w,height:844},isMobile:true,hasTouch:true});
 try{
  // 1–3. geometry at every phone width, both thread kinds
  for(const w of [320,390,412])for(const route of ['/#/chat/a/alfred/b','/#/chat/a/codex/cx1']){
   const ctx=await browser.newContext(phone(w));const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+route);await page.locator('#chatComposer .chat-composer-model').waitFor();await page.locator('#chatComposer .mic-btn').waitFor();
   const at=route.split('/')[3]+' at '+w;
   const g=await page.evaluate(()=>{
    const R=e=>e.getBoundingClientRect(),c=document.getElementById('chatComposer'),f=c.querySelector('textarea');
    const kids=[...c.children].filter(e=>e.offsetParent&&R(e).height>0&&!e.matches('.chat-composer-status'));
    const rowOf=e=>Math.round(R(e).top+R(e).height/2);
    const chips=kids.filter(e=>e.matches('.chat-composer-recipient,.chat-composer-model,.chat-composer-permission'));
    const first=kids.filter(e=>rowOf(e)===rowOf(f)).map(e=>e.matches('textarea')?'field':e.matches('.chat-attach')?'attach':e.matches('.mic-btn')?'mic':e.matches('.chat-send')?'send':e.className);
    const head=document.querySelector('#chatThreadHeader .chat-head');
    return {ratio:R(f).width/R(c).width,first,chipRows:[...new Set(chips.map(rowOf))].length,chipsBelow:chips.every(e=>R(e).top>=R(f).bottom-1),chipMinH:Math.min(...chips.map(e=>R(e).height)),
     recipient:c.querySelector('.chat-composer-recipient')?.textContent||'',title:R(head.querySelector('.chat-head-title')).width,
     overflow:document.documentElement.scrollWidth>innerWidth,headRight:Math.max(...[...head.children].filter(e=>e.offsetParent).map(e=>R(e).right)),vw:innerWidth,
     icons:[...head.querySelectorAll('.chat-icon-button')].filter(e=>e.offsetParent).map(e=>[R(e).width,R(e).height])};
   });
   assert.ok(g.ratio>=0.45,at+': the message field is squeezed: '+g.ratio.toFixed(2));
   assert.deepEqual(g.first.sort(),['attach','field','mic','send'],at+': the first row holds only + · field · mic · send');
   assert.equal(g.chipRows,1,at+': the chips share one row');
   assert.ok(g.chipsBelow,at+': the chips sit under the message');
   assert.ok(g.chipMinH>=44,at+': a chip is under 44px');
   assert.equal(g.overflow,false,at+': horizontal overflow');
   assert.ok(g.headRight<=g.vw,at+': the head runs off screen');
   assert.ok(g.title>=60,at+': the conversation title is squeezed to '+g.title+'px');
   for(const [iw,ih] of g.icons)assert.ok(iw>=44&&ih>=44,at+': a head icon is '+iw+'×'+ih);
   if(route.includes('codex')){
    assert.equal(g.recipient,'Codex ⌄',at+': the recipient names the agent');
    // 3. the wrapped shape keeps send at the row's end
    await page.locator('#chatComposer textarea').fill('A message long enough to wrap onto a second line in the phone composer field');
    await page.waitForFunction(()=>document.getElementById('chatComposer').classList.contains('is-wrapped'));
    const gap=await page.evaluate(()=>{const c=document.getElementById('chatComposer'),s=c.querySelector('.chat-send');return c.getBoundingClientRect().right-s.getBoundingClientRect().right;});
    assert.ok(gap<=16,at+': send left the row end by '+gap+'px');
    await page.locator('#chatComposer textarea').fill('');
   }
   assert.deepEqual(errors,[],at);
   await ctx.close();
  }
  console.log('geometry: 320/390/412 × Alfred + Codex — one chip row, field ≥45%, titled head, 44px targets, send at the row end');
  // 4. a Codex send: echo at once, honest states, busy ink, typed-on words kept
  {const ctx=await browser.newContext(phone(390));const page=await ctx.newPage();let refuse=false;
   await page.route('**/api/terminal/session/cx1/input',async r=>{await new Promise(x=>setTimeout(x,1500));if(refuse)return r.fulfill({status:500,contentType:'application/json',body:'{"error":"runtime down"}'});r.fulfill({status:200,contentType:'application/json',body:'{"ok":true,"delivery":{"id":"d1","state":"delivered"}}'});});
   await page.goto(base+'/#/chat/a/codex/cx1');const ta=page.locator('#chatComposer textarea');await ta.waitFor();
   const echo=()=>page.evaluate(()=>[...document.querySelectorAll('#chatTranscript .chat-term-cmd.pending')].map(e=>e.textContent));
   await ta.fill('first');await page.locator('#chatComposer .chat-send').click();
   await page.waitForFunction(()=>document.querySelector('#chatTranscript .chat-term-cmd.pending'),null,{timeout:500});
   assert.deepEqual(await echo(),['❯firstsending…'],'the echo says sending until the server accepts');
   const send=await page.evaluate(()=>{const s=document.querySelector('#chatComposer .chat-send'),cs=getComputedStyle(s);return {busy:s.getAttribute('aria-busy'),op:cs.opacity,bg:cs.backgroundColor,dis:s.disabled};});
   assert.deepEqual(send,{busy:'true',op:'1',bg:'rgb(23, 23, 23)',dis:true},'a send in flight keeps its ink');
   await ta.pressSequentially(' and one more');
   await page.waitForFunction(()=>/delivered/.test(document.querySelector('#chatTranscript .chat-term-cmd.pending')?.textContent||''));
   await page.waitForFunction(()=>!document.querySelector('#chatComposer .chat-send[aria-busy]'));
   assert.equal(await ta.inputValue(),'and one more','the accepted words leave the field; the rest stays');
   assert.deepEqual(await echo(),['❯firstdelivered · waiting for the agent'],'one echo, accepted');
   refuse=true;await ta.fill('doomed');await page.locator('#chatComposer .chat-send').click();
   await page.waitForFunction(()=>document.querySelectorAll('#chatTranscript .chat-term-cmd.pending').length===2);
   await page.waitForFunction(()=>document.querySelectorAll('#chatTranscript .chat-term-cmd.pending').length===1,null,{timeout:5000});
   assert.equal(await ta.inputValue(),'doomed','a refused send keeps its draft');
   await ctx.close();}
  console.log('send: echo in <500ms, "sending…" until accepted, busy send at full ink, typed-on words kept, refusal removes its echo');
  // 5. Activity and the context meter are legible
  {const ctx=await browser.newContext(phone(390));const page=await ctx.newPage();
   await page.goto(base+'/#/chat/a/codex/cx1');await page.locator('.chat-term-activity > summary').waitFor();await page.locator('.chat-status-context').waitFor();
   const m=await page.evaluate(()=>{
    const rgb=s=>s.match(/[\d.]+/g).slice(0,3).map(Number),lum=c=>{const [r,g,b]=c.map(v=>{v/=255;return v<=0.03928?v/12.92:((v+0.055)/1.055)**2.4;});return 0.2126*r+0.7152*g+0.0722*b;};
    const bgOf=e=>{for(let n=e;n;n=n.parentElement){const b=getComputedStyle(n).backgroundColor;if(b&&!/rgba\(0, 0, 0, 0\)|transparent/.test(b))return b;}return 'rgb(255, 255, 255)';};
    const ratio=e=>{const a=lum(rgb(getComputedStyle(e).color)),b=lum(rgb(bgOf(e)));return (Math.max(a,b)+0.05)/(Math.min(a,b)+0.05);};
    const s=document.querySelector('.chat-term-activity > summary'),c=document.querySelector('.chat-status-context');
    return {actFs:parseFloat(getComputedStyle(s).fontSize),actRatio:ratio(s),ctxFs:parseFloat(getComputedStyle(c).fontSize),bar:document.querySelector('.chat-status-meter').getBoundingClientRect().width};
   });
   assert.ok(m.actFs>=13,'Activity is '+m.actFs+'px');assert.ok(m.actRatio>=7,'Activity contrast '+m.actRatio.toFixed(2));
   assert.ok(m.ctxFs>=13,'the context meter is '+m.ctxFs+'px');assert.ok(m.bar>=64,'the context bar is '+m.bar+'px');
   // 6. two provider runs stay two replies (the tail merge)
   const replies=await page.evaluate(()=>{const turns=[{id:'a1',who:'assistant',ts:'2026-09-12T17:50:00Z',end:'2026-09-12T17:58:00Z',run:'run-one',blocks:[{t:'say',text:'fixed'}]}];
    chatTermMerge(turns,[{id:'a2',who:'assistant',ts:'2026-09-24T20:28:00Z',end:'2026-09-24T20:31:00Z',run:'run-two',blocks:[{t:'say',text:'continued'}]}]);
    chatTermMerge(turns,[{id:'a3',who:'assistant',ts:'2026-09-24T20:31:00Z',end:'2026-09-24T20:35:00Z',run:'run-two',blocks:[{t:'say',text:'more'}]}]);
    return turns.map(t=>[t.id,t.end,t.blocks.length,chatDuration(Date.parse(t.end)-Date.parse(t.ts))]);});
   assert.deepEqual(replies,[['a1','2026-09-12T17:58:00Z',1,'8m 00s'],['a2','2026-09-24T20:35:00Z',2,'7m 00s']],'a new run is a new reply; the same run continues it');
   await ctx.close();}
  console.log('legibility: Activity ≥13px at ≥7:1, context ≥13px with a 64px bar; the tail merge keeps runs apart');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
