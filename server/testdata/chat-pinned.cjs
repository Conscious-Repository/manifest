// chat-pinned.cjs — a conversation reads like a frontier chat (owner
// 2026-10-10): it opens on its latest message, coming back to it after new
// turns lands on the latest message (not on a cached copy's old offset), and
// a send never makes the composer shrink and regrow — the field is at its
// resting height from the send on, and the transcript stays pinned. At 1440
// and 390 over the chat stub.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  for(const [w,h,mob] of [[1440,900,false],[390,844,true]]){
   const ctx=await browser.newContext({viewport:{width:w,height:h},isMobile:mob,hasTouch:mob});
   const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.addInitScript(()=>{window.__f=[];const tick=()=>{const t=document.getElementById('chatTranscript'),c=document.getElementById('chatComposer');if(t&&t.clientHeight)window.__f.push({fromBottom:Math.round(t.scrollHeight-t.scrollTop-t.clientHeight),comp:c?Math.round(c.getBoundingClientRect().height):0,n:t.querySelectorAll('.chat-turn').length});requestAnimationFrame(tick);};requestAnimationFrame(tick);});
   const frames=async(fn,ms)=>{await page.evaluate(()=>{window.__f=[];});await fn();await page.waitForTimeout(ms);return page.evaluate(()=>window.__f);};
   const settled=f=>f.filter(x=>x.n>0);
   await page.goto(base+'/#/chat/a/alfred/a');await page.waitForFunction(()=>document.querySelectorAll('#chatTranscript .chat-turn').length>10);await page.waitForTimeout(800);
   assert.ok(settled(await page.evaluate(()=>window.__f)).slice(-1)[0].fromBottom<=1,w+': opens on the latest message');
   // a saved reading position mid-thread does not move the reader on open
   await page.evaluate(()=>{const t=document.getElementById('chatTranscript');t.scrollTop=200;t.dispatchEvent(new Event('wheel'));t.dispatchEvent(new Event('scroll'));});
   await page.waitForTimeout(300);
   await page.evaluate(()=>{location.hash='#/chat/a/alfred/b';});await page.waitForTimeout(800);
   await new Promise(r=>require('http').get(base+'/__set?id=a&patch=%7B%7D&append=NEW+while+away',res=>{res.resume();res.on('end',r);}));
   const back=settled(await frames(()=>page.evaluate(()=>{location.hash='#/chat/a/alfred/a';}),1500)).filter(x=>x.n>2);
   assert.ok(back.length&&back.every(x=>x.fromBottom<=1),w+': coming back never shows the thread off its latest message: '+JSON.stringify(back.map(x=>x.fromBottom)));
   // a long draft, then send: the composer goes straight to rest and stays there
   const ta=page.locator('#chatComposer textarea');await ta.click();
   await ta.fill('A long enough draft '+w+' to wrap the field onto more than one line at phone width, so it grows.');await page.waitForTimeout(200);
   const rest=mob?54:null;
   await new Promise(r=>require('http').get(base+'/__delay?ms=400',res=>{res.resume();res.on('end',r);}));
   await page.evaluate(()=>{window.__lt=[];const tick=()=>{const t=document.getElementById('chatTranscript'),u=[...t.querySelectorAll(':scope > .chat-user')].pop();if(u)window.__lt.push(Math.round(u.getBoundingClientRect().top));if(window.__lt.length<400)requestAnimationFrame(tick);};requestAnimationFrame(tick);});
   const sendF=await frames(()=>page.locator('#chatComposer .chat-send').first().click(),1500);
   const comps=[...new Set(sendF.map(x=>x.comp))];
   const after=sendF.slice(sendF.findIndex(x=>x.comp!==sendF[0].comp)>=0?sendF.findIndex(x=>x.comp!==sendF[0].comp):0);
   assert.ok(new Set(after.map(x=>x.comp)).size===1,w+': the composer changes height once on send, never regrows: '+JSON.stringify(comps));
   if(rest)assert.equal(after[0].comp,rest,w+': back to its resting height');
   assert.ok(sendF.slice(-1)[0].fromBottom<=1,w+': pinned after the send');
   // the confirmed message lands exactly where its echo was: one move, then still
   const lt=await page.evaluate(()=>window.__lt);const moves=lt.filter((v,i)=>i&&Math.abs(v-lt[i-1])>2).length;
   assert.ok(moves<=1,w+': your message moves '+moves+' times after sending: '+JSON.stringify([...new Set(lt)]));
   // once the send is answered the field is itself again: what you type shows
   await page.waitForFunction(()=>!document.getElementById('chatComposer').classList.contains('is-sending'));
   await ta.fill('next words');
   assert.notEqual(await ta.evaluate(t=>getComputedStyle(t).color),'rgba(0, 0, 0, 0)',w+': typed text is visible after a send');
   // a send whose answer never comes (the box stays is-sending): typing still
   // shows the words (owner 2026-10-10: only the spellcheck underline showed)
   await ta.fill('');await page.evaluate(()=>document.getElementById('chatComposer').classList.add('is-sending'));
   await ta.pressSequentially('typed while sending');
   assert.notEqual(await ta.evaluate(t=>getComputedStyle(t).color),'rgba(0, 0, 0, 0)',w+': words typed during a send are visible');
   assert.deepEqual(errors,[],w+' page errors');
   await ctx.close();
  }
  console.log('PASS: chat opens on the latest message, comes back to it after new turns, and a send settles the composer once while the transcript stays pinned (1440, 390).');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
