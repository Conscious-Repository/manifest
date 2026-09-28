// jarvis-hud.cjs — the cinematic HUD layer (css/98-jarvis-hud.css,
// js/98-jarvis-hud.js) is a clean, reversible layer over the real app:
//   1. the default theme never carries it; jarvis does, before first paint;
//   2. one theme setting: jarvis-og removes it at once and persists, so
//      jarvis renders as before the layer existed; jarvis-cinematic restores
//      it; a legacy stored "jarvis" (+ the retired "classic" style) migrates;
//   3. the boot sequence runs once per browser session, is skippable, and
//      never runs under reduced motion; the rail readout is the real clock;
//   4. chrome uses the display face, reading text the reading face; muted
//      text keeps AA contrast on the selected-row surface; no overflow.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const hud=p=>p.evaluate(()=>document.documentElement.getAttribute('data-hud'));
 try{
  // 1. default theme: no layer
  let ctx=await browser.newContext({viewport:{width:1440,height:900}});let page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/b');await page.locator('#chatComposer textarea').waitFor();
  assert.equal(await hud(page),null,'the default theme must not carry the HUD layer');
  assert.equal(await page.locator('.hud-boot').count(),0);
  await ctx.close();
  // 2 + 3. jarvis: the layer from first paint, boot once, classic rollback
  ctx=await browser.newContext({viewport:{width:1440,height:900}});
  await ctx.addInitScript(()=>{if(!sessionStorage.getItem('seeded')){localStorage.setItem('manifest.theme','jarvis');sessionStorage.setItem('seeded','1');}});
  page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
  const firstPaint=[];page.on('domcontentloaded',async()=>firstPaint.push(await hud(page)));
  await page.goto(base+'/#/chat/a/alfred/b');
  await page.locator('.hud-boot').waitFor({timeout:3000});
  assert.equal(firstPaint[0],'cinematic','the layer must be on before scripts run (no flash of the old look)');
  await page.locator('.hud-boot').click();await page.locator('.hud-boot').waitFor({state:'detached'});
  await page.locator('#chatComposer textarea').waitFor();
  const faces=await page.evaluate(()=>({title:getComputedStyle(document.querySelector('.chat-head-title')||document.querySelector('.agent-title')).fontFamily,body:getComputedStyle(document.body).fontFamily}));
  assert.match(faces.title,/Oxanium/,'chrome titles use the display face');assert.match(faces.body,/IBM Plex Sans/,'reading text uses the reading face');
  assert.match(await page.locator('.hud-readout').textContent(),/^(Online|Offline)\d{2}:\d{2}:\d{2} · \d{2}\.\d{2}$/,'the readout is the real state and clock');
  await page.reload();await page.locator('#chatComposer textarea').waitFor();await page.waitForTimeout(300);
  assert.equal(await page.locator('.hud-boot').count(),0,'boot runs once per session');
  await page.evaluate(()=>setTheme('jarvis-og',true));
  assert.equal(await hud(page),null,'jarvis-og removes the layer at once');
  assert.equal(await page.locator('.hud-readout').count(),0);
  assert.doesNotMatch(await page.evaluate(()=>getComputedStyle(document.body).fontFamily),/IBM Plex Sans/,'jarvis-og restores the original faces');
  await page.reload();await page.locator('#chatComposer textarea').waitFor();
  assert.equal(await hud(page),null,'jarvis-og persists across reloads');
  assert.equal(await page.evaluate(()=>document.documentElement.getAttribute('data-theme')),'jarvis','jarvis-og keeps the jarvis token set');
  assert.equal(await page.evaluate(()=>localStorage.getItem('manifest.theme')),'jarvis-og');
  await page.evaluate(()=>setTheme('jarvis-cinematic',true));assert.equal(await hud(page),'cinematic');
  // the retired two-setting form migrates: jarvis + classic → og, from first paint
  await page.evaluate(()=>{localStorage.setItem('manifest.theme','jarvis');localStorage.setItem('manifest.jarvisStyle','classic');});
  await page.reload();await page.locator('#chatComposer textarea').waitFor();
  assert.equal(await hud(page),null,'legacy jarvis + classic reads as jarvis-og');
  assert.equal(await page.evaluate(()=>themePref()),'jarvis-og');
  await page.evaluate(()=>setTheme('jarvis-cinematic',true));
  assert.equal(await page.evaluate(()=>localStorage.getItem('manifest.jarvisStyle')),null,'the retired key is cleared on the next choice');
  // one row in Settings: three choices, no second style row
  await page.goto(base+'/#/settings/display');
  await page.getByRole('button',{name:'jarvis-cinematic',exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'jarvis-og',exact:true}).count(),1);
  assert.equal(await page.getByText('manifest.jarvisStyle').count(),0,'no second theme row');
  await page.goto(base+'/#/chat/a/alfred/b');await page.locator('#chatComposer textarea').waitFor();
  // 4. contrast and overflow
  const cr=await page.evaluate(()=>{const s=getComputedStyle(document.documentElement);const hex=v=>v.trim().replace('#','');const L=h=>{const c=[0,2,4].map(i=>parseInt(h.slice(i,i+2),16)/255).map(x=>x<=0.03928?x/12.92:((x+0.055)/1.055)**2.4);return 0.2126*c[0]+0.7152*c[1]+0.0722*c[2];};const r=(a,b)=>{const[x,y]=[L(a),L(b)].sort((m,n)=>n-m);return (x+0.05)/(y+0.05);};return {meta:r(hex(s.getPropertyValue('--base-40')),hex(s.getPropertyValue('--base-20'))),danger:r(hex(s.getPropertyValue('--danger')),hex(s.getPropertyValue('--base-20')))};});
  assert.ok(cr.meta>=4.5&&cr.danger>=4.5,'AA on the selected row: '+JSON.stringify(cr));
  for(const width of [1440,390]){await page.setViewportSize({width,height:844});await page.waitForTimeout(100);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'overflow at '+width);}
  await ctx.close();
  // reduced motion: never a boot
  ctx=await browser.newContext({viewport:{width:1440,height:900},reducedMotion:'reduce'});
  await ctx.addInitScript(()=>localStorage.setItem('manifest.theme','jarvis'));
  page=await ctx.newPage();await page.goto(base+'/#/chat/a/alfred/b');await page.locator('#chatComposer textarea').waitFor();await page.waitForTimeout(300);
  assert.equal(await page.locator('.hud-boot').count(),0,'no boot sequence under reduced motion');
  await ctx.close();
  assert.deepEqual(errors,[]);
  console.log('PASS: layer only in jarvis-cinematic from first paint, jarvis-og rollback persists, legacy migrates, one settings row, boot once and never with reduced motion, faces, AA, no overflow.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
