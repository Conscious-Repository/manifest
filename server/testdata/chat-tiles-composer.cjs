// chat-tiles-composer.cjs — the composer inside chat tiles, over the real
// front end and the chat stub. A tile is a narrow frame, so the phone band
// applied: field · + mic send · chips stacked into 190–350px with send beside
// the mic (the owner's screenshot, 2026-09-28). In a tile now:
//   1. the field, then one toolbar ending in mic · send: send is the last
//      control of the box's last row and sits at its right edge, level with +;
//   2. compact: a thread with one chip is ≤100px tall (190 before), and a
//      run-state note has its own line, read whole (it clipped mid-word);
//   3. chips never shrink past reading: in a narrow tile they take their own
//      row whole; a new chat's setup chips do too, and its box starts below
//      the tile's header (it spilled 80px above it, out of reach);
//   4. in both themes, with no page errors.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub({codex:true});const leaf=id=>({t:'leaf',id});
 stub.state.set('inbox/tiles',{key:'inbox',slot:'tiles',revision:1,value:{v:1,active:'1',focus:{'1':'t4'},full:{},
  ws:{'1':{t:'split',dir:'row',ratio:0.5,a:leaf('t1'),b:{t:'split',dir:'col',ratio:0.5,a:leaf('t2'),b:{t:'split',dir:'row',ratio:0.5,a:leaf('t3'),b:leaf('t4')}}}},
  tiles:{t1:{route:'#/chat/a/codex/cx1',title:''},t2:{route:'#/chat/a/alfred/b',title:''},t3:{route:'#/chat/a/claude/new',title:''},t4:{route:'#/chat/a/codex/cx1',title:''}}}});
 await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 try{
  for(const theme of ['default','jarvis']){
   const ctx=await browser.newContext({viewport:{width:1200,height:800}});
   await ctx.addInitScript(t=>{try{localStorage.setItem('manifest.theme',t);}catch(e){}},theme);
   const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+'/#/chat/tiles');
   await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===4);
   await page.waitForFunction(()=>[...document.querySelectorAll('.chat-tile-frame')].every(f=>f.contentDocument?.querySelector('#chatComposer .chat-send')&&f.contentDocument.querySelector('#chatComposer .mic-btn')));
   await page.waitForTimeout(800);
   const tiles=await page.evaluate(()=>[...document.querySelectorAll('.chat-tile-frame')].map(f=>{
    const d=f.contentDocument,c=d.getElementById('chatComposer'),R=e=>e.getBoundingClientRect();
    const kids=[...c.children].filter(e=>e.offsetParent&&R(e).height>0),row=e=>Math.round(R(e).top+R(e).height/2);
    const controls=kids.filter(e=>e.matches('button'));const last=Math.max(...controls.map(row));
    const lastRow=controls.filter(e=>row(e)===last).sort((a,b)=>R(a).left-R(b).left).map(e=>e.matches('.chat-send')?'send':e.matches('.mic-btn')?'mic':e.matches('.chat-attach')?'attach':'chip');
    const status=c.querySelector('.chat-composer-status');const chips=kids.filter(e=>e.matches('.chat-composer-model,.chat-composer-permission,.chat-landing-chip'));
    const head=d.querySelector('#chatThreadHeader')||d.querySelector('.chat-thread-header');
    return {route:f.contentWindow.location.hash,w:f.clientWidth,h:R(c).height,top:R(c).top,headBottom:head?R(head).bottom:0,lastRow,
     sendGap:R(c).right-R(c.querySelector('.chat-send')).right,
     status:status&&status.offsetParent?{clipped:status.scrollWidth>status.clientWidth+1,alone:!kids.some(e=>e!==status&&row(e)===row(status))}:null,
     clipped:chips.filter(e=>e.scrollWidth>e.clientWidth+1).map(e=>e.textContent.trim())};
   }));
   for(const t of tiles){
    const at=theme+' '+t.route+' at '+t.w+'px';
    assert.deepEqual(t.lastRow.slice(-2),['mic','send'],at+': the box ends with mic · send ('+t.lastRow+')');
    assert.equal(t.lastRow[0],'attach',at+': + starts the toolbar row ('+t.lastRow+')');
    assert.ok(t.sendGap<=24,at+': send sits '+t.sendGap+'px from the box edge');
    assert.deepEqual(t.clipped,[],at+': a chip is cut short');
    if(t.status){assert.equal(t.status.clipped,false,at+': the run-state note is clipped');assert.ok(t.status.alone,at+': the run-state note shares a row');}
    if(t.route==='#/chat/a/alfred/b')assert.ok(t.h<=100,at+': a one-chip composer is '+t.h+'px tall');
    if(t.route.endsWith('/new'))assert.ok(t.top>=t.headBottom-1,at+': the new chat starts '+(t.headBottom-t.top)+'px under the tile header');
   }
   assert.deepEqual(errors,[],theme);
   await ctx.close();
  }
  console.log('PASS: tile composers — field, one toolbar ending in mic · send, compact, chips and notes whole, new chat on screen, both themes.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
