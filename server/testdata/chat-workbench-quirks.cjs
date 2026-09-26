// chat-workbench-quirks.cjs — the real front end over the stub chat API in
// headless Chromium, pinning the UI quirk-screen fixes (2026-09-25 finish
// pass + Phase C 2026-09-26):
//   1. an outage keeps the last conversation list (in memory, and from the
//      snapshot after a reload) and says so, with retry — never "No
//      conversations yet";
//   2. at ≤1100px the layout select names the one pane the workspace shows,
//      short enough not to clip at 861;
//   3. with the workspace open (rail hidden) Ctrl+Alt+↓ still switches
//      conversation and Ctrl+Alt+F explains where search went; Ctrl+Alt+I
//      and Ctrl+Alt+J do not also open the sticky / quick chat;
//   4. the dense head toggles and the turn acts meet the 24px pointer floor;
//      on touch the hover-revealed "→ task" is visible at the touch floor;
//   5. a plain (step-less) agent turn keeps its line breaks;
//   6. Enter paints the message at once as "Sending…", acceptance says
//      "Accepted", and the stage names queued / disconnected / failed —
//      at phone and desktop widths, both themes (the rail is folded on
//      phones, so the rail row cannot be the only place);
//   7. the closed phone nav drawer is not a Tab stop;
//   8. desktop streaming keeps a reader's place in history.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-workbench-quirks.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const serve=async()=>{const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
  return {...stub,base,hook:p=>fetch(base+p).then(r=>r.json()),push:(ev,data)=>fetch(base+'/__push?ev='+ev+'&data='+encodeURIComponent(JSON.stringify(data||{})))};};
 const open=async(ctx,base,route='/#/chat/a/alfred/b')=>{const page=await ctx.newPage();await page.goto(base+route);await page.locator('#chatTranscript .chat-turn').first().waitFor();await page.waitForTimeout(300);return page;};
 const live=page=>page.evaluate(()=>document.getElementById('chatLiveArea')?.innerText.trim()||'');
 const servers=[];
 try{
  // 1. outage after a good load
  {const s=await serve();servers.push(s);const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await open(ctx,s.base);
   const rows=()=>page.locator('#chatInboxRows .chat-rail-row').count();
   const notice=()=>page.locator('#chatInboxRows [role=status]');
   assert.equal(await rows(),2);
   await s.hook('/__down?on=1');
   await page.evaluate(()=>{chatInboxAt=0;});await page.locator('#chatInboxRows .chat-rail-row:not(.open)').first().click();await page.waitForTimeout(800);
   assert.equal(await rows(),2,'the last good list is kept through an outage');
   assert.match(await notice().innerText(),/Couldn't refresh conversations/);
   await page.reload();await page.waitForTimeout(1200);
   assert.equal(await rows(),2,'a reload during the outage paints the last snapshot');
   assert.match(await notice().innerText(),/Couldn't refresh conversations/);
   assert.equal(await page.locator('#chatInboxRows').getByText('No conversations yet').count(),0,'an outage is not an empty inbox');
   await s.hook('/__down?on=0');await notice().getByRole('button',{name:'retry'}).click();await page.waitForTimeout(600);
   assert.equal(await notice().count(),0,'retry clears the notice');
   await ctx.close();}
  console.log('outage keeps the last list (memory and snapshot), says so, and retries');
  // 2. layout truth at 861/1024 vs 1440
  {for(const [w,suffix] of [[861,true],[1024,true],[1440,false]]){
    const s=await serve();servers.push(s);
    const ctx=await browser.newContext({viewport:{width:w,height:900}});const page=await open(ctx,s.base);
    await page.keyboard.press('Control+Alt+i');await page.waitForTimeout(300);
    const m=await page.evaluate(()=>{const s=document.querySelector('.chat-layout-select');const o=s.options[s.selectedIndex].textContent;const probe=document.createElement('span');probe.style.cssText='position:absolute;visibility:hidden;white-space:nowrap;font:'+getComputedStyle(s).font;probe.textContent=o;document.body.append(probe);const need=probe.getBoundingClientRect().width;probe.remove();return {text:o,need,have:s.clientWidth-24};});
    assert.equal(/1 pane here/.test(m.text),suffix,w+': '+m.text);
    assert.ok(m.need<=m.have,w+': layout label clipped '+JSON.stringify(m));
    await ctx.close();
   }}
  console.log('layout select names the single pane at 861/1024, unclipped, and not at 1440');
  // 3. shortcuts with the workspace open
  {const s=await serve();servers.push(s);const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await open(ctx,s.base,'/#/chat/a/alfred/a');
   await page.keyboard.press('Control+Alt+i');await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>!!document.getElementById('chatRail').getClientRects().length),false,'rail hidden with the workspace open');
   const floating=()=>page.evaluate(()=>[...document.querySelectorAll('.float-pane, [class*="float"]')].filter(e=>e.getClientRects().length&&/sticky|quick/i.test(e.textContent.slice(0,40))).length);
   assert.equal(await floating(),0,'Ctrl+Alt+I toggles the workspace only, not the sticky');
   await page.keyboard.press('Control+Alt+j');await page.waitForTimeout(300);
   assert.equal(await floating(),0,'Ctrl+Alt+J does not open quick chat');
   await page.keyboard.press('Control+Alt+f');await page.waitForTimeout(200);
   assert.match(await page.locator('.toast').last().innerText(),/Ctrl\+Alt\+I closes the workspace/);
   // the workspace is per conversation (restored from its own slot), so the
   // switch is checked last
   const before=await page.evaluate(()=>location.hash);
   await page.keyboard.press('Control+Alt+ArrowDown');await page.waitForTimeout(500);
   assert.notEqual(await page.evaluate(()=>location.hash),before,'Ctrl+Alt+↓ switches conversation with the rail hidden');
   await ctx.close();}
  console.log('rail shortcuts act or explain while the workspace hides the rail; no global chord collides');
  // 4 + 5. target floors and plain line breaks
  {const s=await serve();servers.push(s);const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await open(ctx,s.base);
   const m=await page.evaluate(()=>({toggle:document.querySelector('.chat-sidebar-toggle').getBoundingClientRect().height,newChat:document.querySelector('#chatHeadActions > .sprt-ghost').getBoundingClientRect().height,
    acts:[...document.querySelectorAll('#chatTranscript .chat-turn-act')].map(e=>e.getBoundingClientRect().height),
    plain:[...document.querySelectorAll('#chatTranscript .chat-say-plain')].map(e=>({ws:getComputedStyle(e).whiteSpace,h:e.getBoundingClientRect().height,lh:parseFloat(getComputedStyle(e).lineHeight)}))}));
   assert.ok(m.toggle>=24&&m.newChat>=24,'24px target floor: '+JSON.stringify(m));
   assert.ok(m.acts.length&&m.acts.every(h=>h>=24),'turn acts meet 24px: '+m.acts);
   assert.ok(m.plain.length>0);for(const p of m.plain){assert.equal(p.ws,'pre-wrap');assert.ok(p.h>=p.lh*3,'line breaks kept: '+JSON.stringify(p));}
   await ctx.close();
   const touch=await browser.newContext({viewport:{width:390,height:844},hasTouch:true,isMobile:true});const phone=await open(touch,s.base);
   const t=await phone.evaluate(()=>[...document.querySelectorAll('#chatTranscript .chat-turn-act')].map(e=>({o:getComputedStyle(e).opacity,h:e.getBoundingClientRect().height})));
   assert.ok(t.length&&t.every(x=>x.o==='1'&&x.h>=44),'touch shows "→ task" at the touch floor: '+JSON.stringify(t));
   await touch.close();}
  console.log('head toggles and turn acts meet their floors; plain agent turns keep their line breaks');
  // 6. send feedback and stage run state, phone + desktop, both themes
  for(const [w,theme] of [[390,'default'],[1440,'jarvis']]){
   const s=await serve();servers.push(s);
   const ctx=await browser.newContext(w<700?{viewport:{width:w,height:844},hasTouch:true,isMobile:true}:{viewport:{width:w,height:900}});
   await ctx.addInitScript(t=>{try{localStorage.setItem('manifest.theme',t);}catch(e){}},theme);const page=await open(ctx,s.base);
   await s.hook('/__delay?ms=900');
   const ta=page.locator('#chatComposer textarea');await ta.fill('hello there');await ta.press(w<700?'Control+Enter':'Enter');
   await page.waitForTimeout(120);
   assert.match(await live(page),/hello there\s*Sending…/,w+': Enter paints the message and "Sending…" at once');
   await page.locator('.chat-run-state',{hasText:'Queued · accepted, not started'}).waitFor({timeout:4000});
   assert.equal(await page.locator('.chat-send-echo').count(),0,w+': the refetch replaced the echo');
   assert.equal(await ta.inputValue(),'','draft cleared on acceptance');
   await s.hook('/__delay?ms=0');
   const state=async(patch,want)=>{await s.hook('/__set?id=b&patch='+encodeURIComponent(JSON.stringify(patch)));await page.evaluate(()=>refetchChatSession(chatOpenId));await page.waitForTimeout(250);assert.equal(await live(page),want,w+' '+JSON.stringify(patch.supervision?.state));};
   await state({status:'thinking',deliveries:[],supervision:{state:'running',evidence:'e',runs:[{state:'running'}]}},'✦ Working…');
   await state({status:'thinking',supervision:{state:'disconnected',evidence:'e',runs:[]}},'Disconnected · outcome uncertain');
   assert.equal(await ta.getAttribute('placeholder'),'Message…','a stale "thinking" under a disconnected projection is not live');
   assert.equal(await page.locator('.chat-thinking').count(),0,'no live accent for a disconnected run');
   await state({status:'error',supervision:{state:'failed',evidence:'e',runs:[]},deliveries:[{id:'f',state:'failed',error:'runner exited 2'}]},'Run failed · runner exited 2');
   await state({status:'idle',deliveries:[],supervision:{state:'ready_for_review',evidence:'e',runs:[]}},'');
   await page.screenshot({path:'/tmp/chat-workbench-quirks-'+w+'-'+theme+'.png'});
   await ctx.close();
  }
  console.log('send shows Sending → Accepted at once; the stage names queued/running/disconnected/failed, ready says nothing');
  // 7. closed phone drawer is inert
  {const s=await serve();servers.push(s);const ctx=await browser.newContext({viewport:{width:390,height:844},hasTouch:true,isMobile:true});const page=await open(ctx,s.base);
   await page.focus('#chatComposer textarea');const xs=[];for(let i=0;i<14;i++){await page.keyboard.press('Shift+Tab');xs.push(await page.evaluate(()=>Math.round(document.activeElement.getBoundingClientRect().right)));}
   assert.ok(xs.every(x=>x>0),'no Tab stop off screen: '+xs);
   await page.getByRole('button',{name:'Menu'}).click();await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>document.getElementById('rail').inert),false,'the open drawer is reachable');
   await ctx.close();
   const desk=await browser.newContext({viewport:{width:1440,height:900}});const d=await open(desk,s.base);
   assert.equal(await d.evaluate(()=>document.getElementById('rail').inert),false,'desktop rail untouched');await desk.close();}
  console.log('the closed phone drawer is not a Tab stop; open drawer and desktop rail are');
  // 8. desktop streaming keeps a reader's place
  {const s=await serve();servers.push(s);const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await open(ctx,s.base,'/#/chat/a/alfred/a');
   await page.mouse.move(900,400);await page.mouse.wheel(0,-2500);await page.waitForTimeout(400);
   const anchor=()=>page.evaluate(()=>{const h=document.getElementById('chatTranscript'),hr=h.getBoundingClientRect();const t=[...h.querySelectorAll('.chat-turn')].find(t=>t.getBoundingClientRect().bottom>hr.top+4);return {text:t.textContent.slice(0,40),top:Math.round(t.getBoundingClientRect().top-hr.top)};});
   const before=await anchor();
   await s.push('turn.started');await page.waitForTimeout(150);
   for(let i=0;i<10;i++){await s.push('assistant.delta',{text:'streamed words '.repeat(20)+'\n\n'});await page.waitForTimeout(60);}
   await page.waitForTimeout(300);
   assert.deepEqual(await anchor(),before,'desktop streaming moved the reader');
   await ctx.close();}
  console.log('desktop streaming keeps the reading position');
 }finally{await browser.close();servers.forEach(s=>s.server.close());}
 process.exit(0);
})().catch(e=>{console.error(e);process.exit(1);});
