// chat-phone-responsive.cjs — the real front end over a stateful stub API in
// headless Chromium, pinning the responsive/accessibility measurements of the
// 2026-09-25 workbench finish pass (row 6). Browser viewports, not a phone:
//   1. 320/390/412 × 844/568/400, both themes: no horizontal overflow, the
//      composer field keeps the 16px iOS-zoom floor, the composer is on screen;
//   2. Back with the phone Chats list open closes the list and stays on the
//      thread (measured defect: it left Chat); closing from the page leaves no
//      extra history entry; Escape closes it and returns focus to ‹ Chats;
//   3. streaming while reading history keeps the first visible turn fixed;
//      at the bottom the transcript follows;
//   4. desktop (1280) never shows the phone toggle.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-phone-responsive.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const {server}=makeStub();await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const push=(ev,data)=>fetch(base+'/__push?ev='+ev+'&data='+encodeURIComponent(JSON.stringify(data||{})));
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const phone=(w,h)=>({viewport:{width:w,height:h},isMobile:true,hasTouch:true});
 const openThread=async(page,route='/#/chat/a/alfred/a')=>{await page.goto(base+route);await page.locator('#chatTranscript .chat-turn').first().waitFor();};
 try{
  // 1. viewport grid
  for(const theme of ['light','jarvis'])for(const w of [320,390,412])for(const h of [844,568,400]){
   const ctx=await browser.newContext(phone(w,h));
   if(theme==='jarvis')await ctx.addInitScript(()=>localStorage.setItem('manifest.theme','jarvis'));
   const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await openThread(page);
   const m=await page.evaluate(()=>{const ta=document.querySelector('#chatComposer textarea');const c=document.querySelector('#chatComposer').getBoundingClientRect();return {sw:document.documentElement.scrollWidth,vw:document.documentElement.clientWidth,font:parseFloat(getComputedStyle(ta).fontSize),bottom:c.bottom,vh:innerHeight,theme:document.documentElement.dataset.theme||'light'};});
   const at=theme+' '+w+'x'+h;
   assert.equal(m.theme,theme,at);assert.equal(m.sw,m.vw,at+' horizontal overflow');assert.ok(m.font>=16,at+' composer font '+m.font);assert.ok(m.bottom<=m.vh+0.5,at+' composer off screen');assert.deepEqual(errors,[],at);
   await ctx.close();
  }
  console.log('viewport grid: 18 phone viewports, both themes, no overflow, 16px field, composer on screen');
  // 2. Back / Escape with the Chats list open
  {const ctx=await browser.newContext(phone(390,844));const page=await ctx.newPage();
   await page.goto(base+'/#/settings');await page.waitForTimeout(300);
   await page.evaluate(()=>{location.hash='#/chat/a/alfred/a';});await page.locator('#chatTranscript .chat-turn').first().waitFor();
   const listOpen=()=>page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open'));
   await page.locator('.mf-chat-back').click();assert.equal(await listOpen(),true);
   await page.goBack();await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>location.hash),'#/chat/a/alfred/a','Back must close the list, not leave the thread');
   assert.equal(await listOpen(),false);
   await page.locator('.mf-chat-back').click();await page.locator('.mf-chat-toggle').click();await page.waitForTimeout(200);
   assert.equal(await listOpen(),false);
   await page.locator('.mf-chat-back').click();await page.keyboard.press('Escape');await page.waitForTimeout(200);
   assert.equal(await listOpen(),false,'Escape closes the list');
   assert.equal(await page.evaluate(()=>document.activeElement.className),'mf-chat-back','focus returns to ‹ Chats');
   await page.goBack();await page.waitForTimeout(300);
   assert.equal(await page.evaluate(()=>location.hash),'#/settings','closing from the page leaves no extra history entry');
   await ctx.close();}
  console.log('phone Back closes the open Chats list; toggle/Escape close it without a stray history entry');
  // 3. streaming reading position
  {const ctx=await browser.newContext(phone(390,844));const page=await ctx.newPage();await openThread(page);await page.waitForTimeout(300);
   await page.evaluate(()=>{const h=document.getElementById('chatTranscript');h.dispatchEvent(new Event('touchstart',{bubbles:true}));h.scrollTop=h.scrollHeight/3;h.dispatchEvent(new Event('scroll'));});
   await page.waitForTimeout(300);
   const anchor=()=>page.evaluate(()=>{const h=document.getElementById('chatTranscript'),hr=h.getBoundingClientRect();const t=[...h.querySelectorAll('.chat-turn')].find(t=>t.getBoundingClientRect().bottom>hr.top+4);return {text:t.textContent.slice(0,40),top:Math.round(t.getBoundingClientRect().top-hr.top)};});
   const before=await anchor();
   await push('turn.started');await page.waitForTimeout(150);
   for(let i=0;i<10;i++){await push('assistant.delta',{text:'streamed words '.repeat(20)+'\n\n'});await page.waitForTimeout(60);}
   await page.waitForTimeout(300);
   assert.deepEqual(await anchor(),before,'streaming moved the reader');
   await page.evaluate(()=>{const h=document.getElementById('chatTranscript');h.scrollTop=h.scrollHeight;});await page.waitForTimeout(200);
   for(let i=0;i<6;i++){await push('assistant.delta',{text:'more '.repeat(40)+'\n\n'});await page.waitForTimeout(60);}
   await page.waitForTimeout(300);
   assert.ok(await page.evaluate(()=>{const h=document.getElementById('chatTranscript');return h.scrollHeight-h.scrollTop-h.clientHeight<4;}),'bottom-follow lost');
   await ctx.close();}
  console.log('streaming keeps the reading position in history and follows at the bottom');
  // 4. desktop never shows the phone toggle
  {const ctx=await browser.newContext({viewport:{width:1280,height:900}});const page=await ctx.newPage();await openThread(page);
   assert.equal(await page.evaluate(()=>getComputedStyle(document.querySelector('.mf-chat-toggle')).display),'none');
   await ctx.close();}
  console.log('desktop 1280: phone toggle hidden');
 }finally{await browser.close();server.close();}
 process.exit(0);
})().catch(e=>{console.error(e);process.exit(1);});
