// chat-single-view-pass.cjs — the real front end over the chat stub in
// headless Chromium, pinning the measured fixes of the 2026-09-27 single-chat
// UI pass (an internal-consistency and platform-convention pass: no reference
// capture was obtainable). Browser viewports, not a phone:
//   1. 390, both themes: the page loads with no failed request and no console
//      error; a reply's time reads ≥4.5:1 (was 1.48:1, the disabled ramp);
//      the composer's + and mic glyphs ≥3:1 (were 2.52:1); the head ··· is a
//      44px target (was 29×36); its open menu's live actions are 44px rows in
//      ≥4.5:1 ink (were 40px at 2.52:1, indistinguishable from disabled);
//   2. 390, after a send: the queued-cancel control is a 44px target at
//      ≥4.5:1; the run-state hint ("Can't steer; messages queue…")
//      fits its field instead of clipping mid-word, and the "accepted, not
//      started" line stays in view as the field takes its row; typing and
//      clearing do not move the field;
//   3. 1440 (merged header) and 1024: the shell reaches the viewport bottom
//      less its 14px margin (the merge left 87px empty under the composer).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-single-view-pass.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
// contrast of an element's text (or glyph) against the first opaque
// background up its ancestry, opacity folded in
const contrastOf=(sel,pseudo)=>{const parse=c=>(c.match(/[\d.]+/g)||[]).map(Number);
 const lum=([r,g,b])=>{const f=v=>{v/=255;return v<=.03928?v/12.92:Math.pow((v+.055)/1.055,2.4)};return .2126*f(r)+.7152*f(g)+.0722*f(b);};
 const e=typeof sel==='string'?document.querySelector(sel):sel;if(!e)return 0;
 let bg=[255,255,255];for(let x=e;x;x=x.parentElement){const c=parse(getComputedStyle(x).backgroundColor);if(c.length<4||c[3]>0.5){bg=c.slice(0,3);break;}}
 let op=1;for(let x=e;x;x=x.parentElement)op*=+getComputedStyle(x).opacity;
 const c=parse(getComputedStyle(e,pseudo||null).color),a=(c[3]??1)*op,fg=c.slice(0,3).map((v,i)=>v*a+bg[i]*(1-a));
 const L1=lum(fg),L2=lum(bg);return (Math.max(L1,L2)+.05)/(Math.min(L1,L2)+.05);};
(async()=>{
 const {server}=makeStub();await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const phone={viewport:{width:390,height:844},isMobile:true,hasTouch:true};
 const open=async(page,route)=>{await page.goto(base+route);await page.locator('#chatTranscript .chat-turn').first().waitFor();await page.waitForTimeout(300);};
 const box=(page,sel)=>page.locator(sel).first().evaluate(e=>{const r=e.getBoundingClientRect();return {w:r.width,h:r.height,x:r.left,y:r.top};});
 try{
  // 1. the open conversation at 390, light and JARVIS
  for(const theme of ['light','jarvis']){
   const ctx=await browser.newContext(phone);
   if(theme==='jarvis')await ctx.addInitScript(()=>localStorage.setItem('manifest.theme','jarvis'));
   const page=await ctx.newPage(),errors=[];
   page.on('pageerror',e=>errors.push('pageerror '+e.message));page.on('console',m=>{if(m.type()==='error')errors.push('console '+m.text());});
   page.on('response',r=>{if(r.status()>=400)errors.push(r.status()+' '+r.url());});
   await open(page,'/#/chat/a/alfred/a');
   assert.deepEqual(errors,[],theme+': the chat page loads clean');
   const when=await page.evaluate(contrastOf,'#chatTranscript .chat-turn-when');
   assert.ok(when>=4.5,theme+' reply time contrast '+when.toFixed(2));
   for(const g of ['#chatComposer > .chat-attach','#chatComposer > .mic-btn']){
    if(!await page.locator(g).count())continue;
    const c=await page.evaluate(contrastOf,g);assert.ok(c>=3,theme+' '+g+' glyph contrast '+c.toFixed(2));
   }
   const more=await box(page,'#chatThreadHeader .chat-options-compact > summary');
   assert.ok(more.w>=44&&more.h>=44,theme+' head ··· is '+more.w+'×'+more.h);
   await page.locator('#chatThreadHeader .chat-options-compact > summary').click();
   const rows=await page.evaluate(src=>{const contrastOf=eval(src);return [...document.querySelectorAll('#chatThreadHeader .chat-details[open] .chat-head-acts > button:not(:disabled)')].map(b=>({t:b.textContent,h:b.getBoundingClientRect().height,c:contrastOf(b),right:b.getBoundingClientRect().right}));},'('+contrastOf+')');
   assert.ok(rows.length>=3,theme+' the menu lists its actions');
   for(const r of rows){assert.ok(r.h>=44,theme+' menu row '+r.t+' '+r.h+'px');assert.ok(r.c>=4.5,theme+' menu row '+r.t+' contrast '+r.c.toFixed(2));assert.ok(r.right<=390,theme+' menu row '+r.t+' off screen');}
   await ctx.close();
  }
  // 2. after a send: the queued state at 390
  {
   const ctx=await browser.newContext(phone),page=await ctx.newPage();
   await open(page,'/#/chat/a/alfred/b');
   const ta=page.locator('#chatComposer textarea');
   await ta.fill('Please check the build');await page.locator('#chatComposer .chat-send').click();
   await page.locator('.chat-queued-control > .chat-queued-cancel').waitFor();
   await page.waitForFunction(()=>document.querySelector('#chatComposer textarea').placeholder==="Can't steer; messages queue…");
   const cancel=await box(page,'.chat-queued-control > .chat-queued-cancel');
   assert.ok(cancel.h>=44,'queued cancel is '+cancel.h+'px tall');
   const cc=await page.evaluate(contrastOf,'.chat-queued-control > .chat-queued-cancel');assert.ok(cc>=4.5,'queued cancel contrast '+cc.toFixed(2));
   const fit=await page.evaluate(()=>{const t=document.querySelector('#chatComposer textarea'),cs=getComputedStyle(t),g=document.createElement('canvas').getContext('2d');
    g.font=cs.fontStyle+' '+cs.fontWeight+' '+cs.fontSize+' '+cs.fontFamily;return {need:g.measureText(t.placeholder).width,room:t.clientWidth-parseFloat(cs.paddingLeft)-parseFloat(cs.paddingRight),hint:t.placeholder};});
   assert.ok(fit.need<=fit.room,'the hint "'+fit.hint+'" needs '+fit.need.toFixed(0)+'px in '+fit.room.toFixed(0)+'px');
   // the reshaped field took height from the transcript: the accepted line
   // under the latest turn is still in view (it was 42px below the fold)
   await page.locator('#chatTranscript .chat-run-state').waitFor();await page.waitForTimeout(200);
   const seen=await page.evaluate(()=>{const l=document.querySelector('#chatTranscript .chat-run-state').getBoundingClientRect(),t=document.getElementById('chatTranscript').getBoundingClientRect();return {line:l.bottom,view:t.bottom,text:document.querySelector('#chatTranscript .chat-run-state').textContent};});
   assert.equal(seen.text,'Queued · accepted, not started');
   assert.ok(seen.line<=seen.view+1,'the accepted line is below the fold: '+seen.line+' > '+seen.view);
   const before=await box(page,'#chatComposer textarea');
   await ta.pressSequentially('h');const typed=await box(page,'#chatComposer textarea');
   await ta.press('Backspace');const cleared=await box(page,'#chatComposer textarea');
   for(const [label,b] of [['typing',typed],['clearing',cleared]])assert.deepEqual([b.x,b.y,b.w],[before.x,before.y,before.w],label+' moved the field');
   await ctx.close();
  }
  // 3. desktop: the shell fills the column whichever header it wears
  for(const width of [1440,1024]){
   const ctx=await browser.newContext({viewport:{width,height:900}}),page=await ctx.newPage();
   await open(page,'/#/chat/a/alfred/a');await page.waitForTimeout(200);
   const m=await page.evaluate(()=>({bottom:document.querySelector('.chat-shell').getBoundingClientRect().bottom,merged:document.getElementById('chatView').classList.contains('chat-head-merged')}));
   assert.equal(m.merged,width>1100,width+' header merge');
   assert.ok(900-m.bottom<=20,width+': '+Math.round(900-m.bottom)+'px left empty under the shell');
   await ctx.close();
  }
  console.log('PASS: single chat view — clean load, legible meta and menu, 44px phone targets, whole run-state hint, shell fills the column');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
