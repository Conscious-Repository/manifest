// chat-workspace-layouts-browser.cjs — the four workspace layouts over a
// POPULATED conversation (stub thread "a", 40 turns) in the real front end,
// with four workspace tabs open (Activity, Context, Files, another chat):
//   - Focus / Split · 2 / Workbench · 3 / Four panes show 0/1/2/3 workspace
//     panes beside the stage, none overlapping the stage or each other, no
//     sideways scroll;
//   - switching layouts, and resizing 1440→1024→861→1440 (≤1100 one pane,
//     ≤900 the stage hidden behind it), keeps the draft, the tabs, the active
//     tab and the reader's turn (2026-09-26: the round trip through 861 left
//     the reader five turns off — the hidden scroller came back at its old
//     pixel offset);
//   - a wheel over the transcript scrolls only the transcript, one over a
//     pane never scrolls the transcript or the page;
//   - Tab traversal passes through the stage and the workspace and wraps back
//     to its start (no trap).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-workspace-layouts-browser.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 try{
  const page=await browser.newPage({viewport:{width:1440,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/a');await page.locator('#chatTranscript .chat-turn').first().waitFor();await page.waitForTimeout(600);
  await page.locator('#chatComposer textarea').fill('draft across layouts');
  await page.getByRole('button',{name:'Toggle workspace'}).click();await page.waitForTimeout(300);
  const add=async name=>{if(!(await page.locator('.chat-workspace-option').count()))await page.getByRole('button',{name:'Add workspace tab'}).click();await page.waitForTimeout(150);await page.locator('.chat-workspace-option',{hasText:name}).first().click();await page.waitForTimeout(500);};
  await add('Activity');await add('Context');await add('Files');
  await add('Open conversation');await page.locator('.chat-conversation-choice').first().click();await page.waitForTimeout(1200);
  const T=page.locator('#chatTranscript');
  await T.evaluate(t=>{t.scrollTop=Math.round((t.scrollHeight-t.clientHeight)/2);});await page.waitForTimeout(300);
  const measure=()=>page.evaluate(()=>{
   const r=e=>{const b=e.getBoundingClientRect();return {x:b.x,y:b.y,w:b.width,h:b.height,r:b.right,b:b.bottom};};
   const t=document.getElementById('chatTranscript'),tb=t.getBoundingClientRect();
   const turns=[...t.querySelectorAll('.chat-turn')];
   return {panes:[...document.querySelectorAll('.chat-workspace-tabbody')].filter(h=>!h.hidden&&h.offsetParent).map(r),
    main:document.querySelector('.chat-main').offsetParent?r(document.querySelector('.chat-main')):null,
    tabs:document.querySelectorAll('.chat-workspace-tab').length,active:document.querySelector('.chat-workspace-tab [aria-selected=true]')?.textContent,
    draft:document.querySelector('#chatComposer textarea')?.value,
    topTurn:tb.height?turns.findIndex(x=>x.getBoundingClientRect().bottom>tb.top+1):null,
    sideways:document.documentElement.scrollWidth>innerWidth};});
  const overlap=(a,b)=>a.x<b.r-1&&b.x<a.r-1&&a.y<b.b-1&&b.y<a.b-1;
  const start=await measure();assert.ok(start.topTurn>5,'reader parked mid-history ('+start.topTurn+')');
  const check=(m,label,panes)=>{
   assert.equal(m.panes.length,panes,label+': workspace panes');
   assert.equal(m.tabs,4,label+': tabs kept');assert.equal(m.active,start.active,label+': active tab kept');
   assert.equal(m.draft,'draft across layouts',label+': draft kept');
   assert.equal(m.sideways,false,label+': no sideways scroll');
   if(m.main)for(const p of m.panes)assert.ok(!overlap(m.main,p),label+': a pane overlaps the stage');
   for(let i=0;i<m.panes.length;i++)for(let j=i+1;j<m.panes.length;j++)assert.ok(!overlap(m.panes[i],m.panes[j]),label+': panes overlap');
   if(m.topTurn!==null)assert.equal(m.topTurn,start.topTurn,label+': the reader stays on turn '+start.topTurn);
  };
  const want={focus:0,split:1,workbench:2,four:3};
  for(const L of ['focus','split','workbench','four','split','focus','four']){await page.selectOption('.chat-layout-select',L);await page.waitForTimeout(400);check(await measure(),L,want[L]);}
  for(const [w,panes,stage] of [[1024,1,true],[861,1,false],[1440,3,true]]){
   await page.setViewportSize({width:w,height:900});await page.waitForTimeout(600);const m=await measure();
   assert.equal(!!m.main,stage,w+'px: stage shown');check(m,'four at '+w+'px',panes);
  }
  // scroll regions do not fight
  const pos=()=>page.evaluate(()=>({t:document.getElementById('chatTranscript').scrollTop,d:document.scrollingElement.scrollTop}));
  const before=await pos(),tb=await T.boundingBox();
  await page.mouse.move(tb.x+tb.width/2,tb.y+tb.height/2);await page.mouse.wheel(0,-400);await page.waitForTimeout(300);
  const afterT=await pos();assert.ok(afterT.t<before.t-100,'the transcript scrolls under its wheel');assert.equal(afterT.d,0,'the page never scrolls');
  const pb=await page.locator('.chat-workspace-tabbody:not([hidden])').first().boundingBox();
  await page.mouse.move(pb.x+pb.width/2,pb.y+pb.height/2);await page.mouse.wheel(0,400);await page.waitForTimeout(300);
  const afterP=await pos();assert.equal(afterP.t,afterT.t,'a wheel over a pane leaves the transcript alone');assert.equal(afterP.d,0);
  // Tab traversal: through stage and workspace, and back to the start
  await page.mouse.click(2,2);const seq=[];
  for(let i=0;i<220;i++){await page.keyboard.press('Tab');seq.push(await page.evaluate(()=>{const a=document.activeElement;if(!a||a===document.body)return 'body:';const reg=a.closest('.chat-tab-workspace')?'workspace':a.closest('#chatComposer')?'composer':a.closest('.chat-main')?'stage':'other';return reg+':'+(a.id||a.getAttribute('aria-label')||a.textContent||a.tagName).trim().slice(0,40);}));}
  const regions=new Set(seq.map(s=>s.split(':')[0]));
  for(const r of ['stage','composer','workspace'])assert.ok(regions.has(r),'Tab reaches the '+r);
  assert.ok(seq.findIndex((s,i)=>i>5&&s===seq[0])>0,'Tab wraps back to its first stop (no trap)');
  assert.deepEqual(errors,[]);
  console.log('PASS four layouts over a populated thread: 0/1/2/3 panes, no overlap, draft/tabs/turn kept through layout switches and 1440→1024→861→1440, scroll regions isolated, Tab wraps.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
