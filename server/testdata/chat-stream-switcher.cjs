// chat-stream-switcher.cjs — the phone Now section, stream switcher and the
// counted ‹ Chats, on the real front end over the stateful chat stub, in
// headless Chromium (browser viewports, not a device):
//   1. Now sits atop the Chats list: Waiting on you · Working · Ready to
//      review · Pinned, only groups with rows, each row with title, explicit
//      state, next action or delivery summary and time; a row files once; the
//      full list stays below, unchanged; nothing active → an intentional empty
//      state;
//   2. the conversation title is a labelled ≥44px button that opens a named
//      bottom sheet (Needs you · Working · Pinned · View all chats) without a
//      route change; the open conversation is not offered; aria-expanded
//      follows the sheet; the first row takes focus;
//   3. Escape, the scrim and the phone's Back each dismiss it back to the same
//      conversation with focus on the title and no extra history entry, and
//      opening it writes nothing (no PUT/POST: nothing marked read);
//   4. a row goes to that exact route (agent, Codex, task) and Back returns to
//      the conversation it left; View all chats opens the Chats list;
//   5. ‹ Chats carries a calm count of the OTHER conversations waiting on the
//      owner or ready, in its accessible name, one action; zero shows none;
//      streamed tokens and typing never move it; a real state change does;
//   6. long titles truncate without hiding the state; 320/390/412 in default,
//      dark and JARVIS: no sideways pan, no overlap, 44px targets;
//   7. desktop (1280) is unchanged: no switcher, no Now section.
// Page errors, console errors and 5xx responses fail the run; screenshots are
// evidence only (MANIFEST_SHOTS, default the OS temp dir).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-stream-switcher.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict'),os=require('node:os'),path=require('node:path');
const {makeStub}=require('./chat-stub-api.cjs');
const shots=process.env.MANIFEST_SHOTS||os.tmpdir();
const ago=m=>new Date(Date.now()-m*60000).toISOString();
const LONG='A very long conversation title that keeps going well past the width of any phone screen, and then some more';
// one conversation per state the Now section must tell apart
function seed(stub){
 const {sessions,addSession,terminalRows,inboxExtra,state}=stub;
 sessions.a.updated=ago(30);sessions.b.updated=ago(40);
 terminalRows[0]={...terminalRows[0],connectivity:'connected',agentState:'blocked',process:'running',live:true,lastUsed:ago(1)};
 inboxExtra.taskThreads=[{id:'inbox/plan',title:'Plan the bathroom tile order',agent:'agent:alfred',state:'plan-ready',updated:ago(2),comments:3,open:true,lastAuthor:'Alfred'}];
 addSession('f1',{title:'Permit check',updated:ago(3),turns:4,deliveries:[{id:'f',state:'failed',text:'Check the permit status',userTurn:3,error:'provider timed out after 420 s'}]});
 addSession('long',{title:LONG,updated:ago(4),turns:4,supervision:{adapter:'hermes-oneshot',state:'disconnected',evidence:'receipt r9',runs:[]}});
 addSession('w1',{title:'Vendor quotes',status:'thinking',updated:ago(5),turns:3,deliveries:[{id:'r',state:'running',text:'Summarise the vendor quotes',userTurn:3}]});
 addSession('q1',{title:'Plumber reply',updated:ago(6),turns:3,deliveries:[{id:'q',state:'queued',text:'Draft the reply to the plumber',userTurn:3}]});
 addSession('r1',{title:'Kitchen report',updated:ago(7),turns:4,conversation:{key:'conv-r1'},deliveries:[{id:'d',state:'completed',text:'Write the report',userTurn:3,replyTurn:4}]});
 inboxExtra.review={by_scope:{'conv-r1':{ready:2,changes:0,accepted:0,unreviewed:0}},by_task:{}};
 addSession('p1',{title:'Reading list',updated:ago(8),turns:6,deliveries:[{id:'p',state:'completed',text:'Add the Benkler book',userTurn:5,replyTurn:6}]});
 // q1 is pinned too: it files once, under Working
 state.set('inbox/pins',{key:'inbox',slot:'pins',revision:1,value:{pins:{'agent:alfred/p1':true,'agent:alfred/q1':true}}});
}
const NOW=[['Waiting on you',['Fix the flaky test','Plan the bathroom tile order','Permit check',LONG]],['Working',['Vendor quotes','Plumber reply']],['Ready to review',['Kitchen report']],['Pinned',['Reading list']]];
const SHEET=[['Needs you',['Fix the flaky test','Plan the bathroom tile order','Permit check',LONG,'Kitchen report']],['Working',['Vendor quotes','Plumber reply']],['Pinned',['Reading list']]];
const TRUTH={'Fix the flaky test':['Needs input','Codex','Answer its prompt'],'Plan the bathroom tile order':['Plan ready · review','Alfred · task','Review the plan'],
 'Permit check':['Run failed','Alfred','provider timed out after 420 s'],[LONG]:['Disconnected · outcome uncertain','Alfred','Check whether it finished'],
 'Vendor quotes':['Working','Alfred','“Summarise the vendor quotes”'],'Plumber reply':['Queued','Alfred','Accepted, not started · “Draft the reply to the plumber”'],
 'Kitchen report':['Run finished','Alfred','Review 2 outputs'],'Reading list':['Run finished','Alfred','Last sent “Add the Benkler book”']};
const THEMES={default:{},dark:{theme:'jarvis-og',colorScheme:'dark'},jarvis:{theme:'jarvis-cinematic'}};
(async()=>{
 const stubs=[];
 const start=async(opts,seeded)=>{const stub=makeStub({codex:true,inboxState:true,...opts});if(seeded)seed(stub);await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));stub.base='http://127.0.0.1:'+stub.server.address().port;stubs.push(stub);return stub;};
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const problems=[];
 // a deliberate outage (/__down) answers 503 by design: only then, and only 503, is allowed
 let outage=false;
 const down=async(stub,on)=>{outage=on;await fetch(stub.base+'/__down?on='+(on?1:0));};
 const open=async(stub,{w=390,h=844,theme='default',from='/#/settings',route='/#/chat/a/alfred/a'}={})=>{
  const t=THEMES[theme];const ctx=await browser.newContext({viewport:{width:w,height:h},isMobile:w<861,hasTouch:w<861,colorScheme:t.colorScheme||'light'});
  if(t.theme)await ctx.addInitScript(v=>localStorage.setItem('manifest.theme',v),t.theme);
  const page=await ctx.newPage();const at=theme+' '+w+'x'+h;
  page.on('pageerror',e=>problems.push(at+' pageerror: '+e.message));
  page.on('console',m=>{if(m.type()==='error'&&!/^Failed to load resource: the server responded with a status of 404/.test(m.text())&&!(outage&&/status of 503/.test(m.text())))problems.push(at+' console: '+m.text());});
  page.on('response',r=>{if(r.status()>=500&&!(outage&&r.status()===503))problems.push(at+' '+r.status()+' '+r.url());});
  await page.goto(stub.base+from);await page.waitForTimeout(150);
  await page.evaluate(r=>{location.hash=r.slice(1);},route);
  await page.locator('#chatTranscript .chat-turn').first().waitFor();
  return {ctx,page,at};
 };
 const writes=stub=>stub.log.filter(l=>!/^GET /.test(l));
 const hash=page=>page.evaluate(()=>location.hash);
 const trigger=page=>page.locator('#chatThreadHeader .mf-stream-switch');
 const dialog=page=>page.getByRole('dialog',{name:'Switch chat'});
 const settle=page=>page.waitForTimeout(250);
 // the bottom sheet slides in (sheetUp, 240ms): wait for it before a screenshot
 const settled=page=>page.waitForFunction(()=>document.getAnimations().every(a=>a.playState!=='running'));
 const groups=(page,root,label,row)=>page.evaluate(([root,label,row])=>[...document.querySelectorAll(root)].filter(g=>g.getClientRects().length).map(g=>[g.querySelector(label).firstChild.textContent.trim(),[...g.querySelectorAll(row)].map(r=>r.querySelector('.chat-now-title').textContent)]),[root,label,row]);
 const rowTruth=(page,sel)=>page.evaluate(sel=>Object.fromEntries([...document.querySelectorAll(sel)].map(r=>[r.querySelector('.chat-now-title').textContent,[r.querySelector('.chat-now-state').textContent,r.querySelector('.chat-now-agent').textContent,r.querySelector('.chat-now-summary').textContent,r.querySelector('time')?.getAttribute('datetime')||'',r.querySelector('time')?.textContent.trim()||'',r.dataset.execution]])),sel);
 try{
  // ---- 1. Now on top of the Chats list ----
  {const stub=await start({},true);const {ctx,page}=await open(stub);
   const back=page.locator('#chatThreadHeader .mf-chat-back');
   await back.locator('.mf-chat-back-count').waitFor({timeout:5000});
   await back.click();await page.locator('#chatNow .chat-now-row').first().waitFor();
   assert.deepEqual(await groups(page,'#chatNow .chat-now-group','.chat-now-label','.chat-now-row'),NOW,'Now groups in priority order, one row each');
   const truth=await rowTruth(page,'#chatNow .chat-now-row');
   for(const [title,[state,agent,summary]] of Object.entries(TRUTH)){const got=truth[title];assert.ok(got,'Now row '+title);assert.deepEqual(got.slice(0,3),[state,agent,summary],title);assert.ok(got[3]&&got[4],title+' shows its update time');}
   assert.notEqual(truth['Vendor quotes'][5],truth['Plumber reply'][5],'working and queued stay distinct states');
   assert.deepEqual(new Set(Object.values(truth).map(t=>t[5])),new Set(['waiting_user','failed','disconnected','running','queued','completed']));
   const layout=await page.evaluate(()=>{const now=document.getElementById('chatNow').getBoundingClientRect(),first=document.querySelector('#chatInboxRows .chat-rail-row').getBoundingClientRect();return {nowBottom:now.bottom,listTop:first.top,rows:document.querySelectorAll('#chatInboxRows .chat-rail-row').length};});
   assert.ok(layout.nowBottom<=layout.listTop+1,'the full list follows Now');
   assert.equal(layout.rows,10,'the full list keeps every conversation');
   await page.screenshot({path:path.join(shots,'manifest-chat-now-390.png')});
   const refresh=()=>page.evaluate(async()=>{chatInboxAt=0;await chatLoadInbox(true);renderChatRail();});
   // a focused row keeps focus through a repaint that changes its state words
   await page.locator('#chatNow .chat-now-row',{hasText:'Plumber reply'}).focus();
   stub.sessions.q1.deliveries=[{id:'q',state:'running',text:'Draft the reply to the plumber',userTurn:3}];
   await refresh();
   assert.equal(await page.evaluate(()=>document.activeElement?.dataset.inboxKey),'agent:alfred/q1','focus stays on the row');
   assert.equal(await page.evaluate(()=>document.activeElement?.querySelector('.chat-now-state')?.textContent),'Working','and it says the new state');
   // an outage: Now keeps the last list and says so; it never claims all clear
   await down(stub,true);await refresh();
   assert.equal(await page.locator('#chatNow .chat-now-note').textContent(),"Couldn't refresh chats · this is the last list");
   assert.equal(await page.locator('#chatNow .chat-now-row').count(),8,'the last list stays');
   await down(stub,false);await refresh();
   assert.equal(await page.locator('#chatNow .chat-now-note').count(),0,'the note leaves with the outage');
   // a Now row goes to its conversation and closes the list
   await page.locator('#chatNow .chat-now-row',{hasText:'Permit check'}).click();
   await page.waitForFunction(()=>location.hash==='#/chat/a/alfred/f1');
   assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),false,'the list closes on a Now row');
   await ctx.close();
   const empty=await start({},false);const e=await open(empty);
   await e.page.locator('#chatThreadHeader .mf-chat-back').click();await e.page.locator('#chatNow').waitFor();
   const blank=await e.page.evaluate(()=>({groups:document.querySelectorAll('#chatNow .chat-now-group').length,text:document.querySelector('#chatNow .chat-now-empty')?.textContent||''}));
   assert.equal(blank.groups,0,'no empty groups');assert.match(blank.text,/Nothing is waiting on you or working/);
   await e.page.screenshot({path:path.join(shots,'manifest-chat-now-empty-390.png')});
   await down(empty,true);await e.page.evaluate(async()=>{chatInboxAt=0;await chatLoadInbox(true);renderChatRail();});
   assert.deepEqual(await e.page.evaluate(()=>({empty:document.querySelectorAll('#chatNow .chat-now-empty').length,note:document.querySelector('#chatNow .chat-now-note')?.textContent})),{empty:0,note:"Couldn't refresh chats · this is the last list"},'an outage is not an all-clear');
   await down(empty,false);
   await e.ctx.close();}
  console.log('PASS Now: priority groups, one row each, truthful state/summary/time, full list below, empty state');
  // ---- 2–3. the switcher: open, no route change, dismissals ----
  {const stub=await start({},true);const {ctx,page}=await open(stub);
   const t=trigger(page);await t.waitFor();
   await page.locator('#chatThreadHeader .mf-chat-back-count').waitFor({timeout:5000});
   const box=await t.boundingBox();assert.ok(box.height>=44,'title button is '+box.height+'px tall');
   assert.equal(await t.getAttribute('aria-haspopup'),'dialog');assert.equal(await t.getAttribute('aria-expanded'),'false');
   assert.equal(await t.getAttribute('aria-label'),'Long research thread, Alfred, switch chat','the name says the title, who, and what it does');
   assert.equal(await page.locator('#chatThreadHeader .chat-head > .chat-head-sub').isVisible(),false,'who rides inside the title button, not squeezed beside it');
   await settle(page);const before=writes(stub).length;const length=await page.evaluate(()=>history.length);
   for(const how of ['escape','scrim','back']){
    await t.click();await dialog(page).waitFor();
    assert.equal(await hash(page),'#/chat/a/alfred/a',how+': opening is not a route change');
    assert.equal(await t.getAttribute('aria-expanded'),'true',how+': expanded while open');
    assert.equal(await page.evaluate(()=>document.activeElement?.closest('.mf-stream-row')?.dataset.inboxKey),'terminal:codex/cx1',how+': the first row has focus');
    if(how==='escape'){
     assert.deepEqual(await groups(page,'.mf-streams-group','.mf-streams-label','.mf-stream-row'),SHEET,'Needs you · Working · Pinned');
     const keys=await page.evaluate(()=>[...document.querySelectorAll('.mf-stream-row')].map(r=>r.getAttribute('href')));
     assert.equal(new Set(keys).size,keys.length,'no row twice');assert.ok(!keys.includes('#/chat/a/alfred/a'),'the open conversation is not offered');
     assert.deepEqual((await rowTruth(page,'.mf-stream-row'))['Plumber reply'].slice(0,3),TRUTH['Plumber reply']);
     assert.match(await page.locator('.mf-streams-current').textContent(),/Long research thread/,'the sheet names the conversation it switches from');
     const all=page.getByRole('button',{name:'View all chats'});assert.ok((await all.boundingBox()).height>=44);
     // the sheet keeps focus: Tab from the last control wraps to the first row
     await all.focus();await page.keyboard.press('Tab');
     assert.equal(await page.evaluate(()=>document.activeElement?.dataset.inboxKey),'terminal:codex/cx1','Tab stays in the sheet');
     await page.keyboard.press('Shift+Tab');assert.equal(await page.evaluate(()=>document.activeElement?.textContent),'View all chats','Shift+Tab stays in the sheet');
     await settled(page);await page.screenshot({path:path.join(shots,'manifest-chat-switcher-390.png')});
     await page.keyboard.press('Escape');
    }else if(how==='scrim')await page.mouse.click(195,60);
    else await page.goBack();
    await dialog(page).waitFor({state:'hidden'});await settle(page);
    assert.equal(await hash(page),'#/chat/a/alfred/a',how+': back on the same conversation');
    assert.equal(await trigger(page).getAttribute('aria-expanded'),'false',how+': collapsed after');
    assert.equal(await page.evaluate(()=>document.activeElement?.classList.contains('mf-stream-switch')),true,how+': focus returns to the title');
    assert.equal(await page.evaluate(()=>history.length),length+1,how+': the sheet entry is reused, not stacked');
   }
   assert.deepEqual(writes(stub).slice(before),[],'opening and dismissing the switcher writes nothing');
   await page.goBack();await page.waitForFunction(()=>location.hash==='#/settings');
   await ctx.close();}
  console.log('PASS switcher: 44px labelled title, named sheet without a route change, Escape/scrim/Back restore focus, no writes, no stray history');
  // ---- 4. switching and View all chats ----
  {const stub=await start({},true);const {ctx,page}=await open(stub);
   await page.locator('#chatThreadHeader .mf-chat-back-count').waitFor({timeout:5000});
   for(const [title,route] of [['Vendor quotes','#/chat/a/alfred/w1'],['Fix the flaky test','#/chat/a/codex/cx1'],['Plan the bathroom tile order','#/chat/task/inbox%2Fplan']]){
    await trigger(page).click();await dialog(page).waitFor();
    await page.locator('.mf-stream-row',{hasText:title}).click();
    await page.waitForFunction(r=>location.hash===r,route);
    await dialog(page).waitFor({state:'hidden'});
    if(!route.includes('/task/'))await page.waitForFunction(()=>document.activeElement?.classList.contains('mf-stream-switch'),null,{timeout:4000}).catch(()=>{throw Error(title+': focus did not move to the new conversation title');});
    await page.goBack();await page.waitForFunction(()=>location.hash==='#/chat/a/alfred/a');
    await page.locator('#chatTranscript .chat-turn').first().waitFor();await trigger(page).waitFor();
    assert.equal(await trigger(page).getAttribute('aria-expanded'),'false',title+': collapsed on return');
   }
   await trigger(page).click();await dialog(page).waitFor();
   await page.getByRole('button',{name:'View all chats'}).click();
   await dialog(page).waitFor({state:'hidden'});
   assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),true,'View all chats opens the Chats list');
   assert.equal(await hash(page),'#/chat/a/alfred/a');await page.locator('#chatNow .chat-now-row').first().waitFor();
   await page.goBack();await settle(page);
   assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),false,'Back closes the list it opened');
   assert.equal(await hash(page),'#/chat/a/alfred/a');
   await page.goBack();await page.waitForFunction(()=>location.hash==='#/settings');
   await ctx.close();}
  console.log('PASS switching: exact agent/Codex/task routes, Back returns, View all chats opens the list');
  // ---- 5. the counted ‹ Chats ----
  {const stub=await start({},true);const {ctx,page}=await open(stub);
   const back=page.locator('#chatThreadHeader .mf-chat-back');
   await back.locator('.mf-chat-back-count').waitFor({timeout:5000});
   assert.equal(await back.locator('.mf-chat-back-count').textContent(),'5','4 waiting + 1 ready, the open one excluded');
   assert.equal(await back.getAttribute('aria-label'),'Back to chats, 5 other chats need you');
   assert.equal(await back.locator('button, a, [tabindex]').count(),0,'one action');
   const push=(ev,data)=>fetch(stub.base+'/__push?ev='+ev+'&data='+encodeURIComponent(JSON.stringify(data||{})));
   await push('turn.started');for(let i=0;i<6;i++){await push('assistant.delta',{text:'streamed words '.repeat(12)});await page.waitForTimeout(40);}
   await page.locator('#chatComposer textarea').fill('typing a reply');await settle(page);
   assert.equal(await back.locator('.mf-chat-back-count').textContent(),'5','tokens and typing never move the count');
   stub.sessions.f1.deliveries=[{id:'f',state:'completed',text:'Check the permit status',userTurn:3,replyTurn:4}];stub.sessions.f1.updated=new Date().toISOString();
   await page.evaluate(async()=>{chatInboxAt=0;await chatLoadInbox(true);renderChatRail();});
   await page.waitForFunction(()=>document.querySelector('#chatThreadHeader .mf-chat-back-count')?.textContent==='4');
   assert.equal(await back.getAttribute('aria-label'),'Back to chats, 4 other chats need you');
   await back.click();assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),true,'‹ Chats still opens the list');
   await ctx.close();
   const empty=await start({},false);const e=await open(empty);await settle(e.page);await e.page.waitForTimeout(400);
   const zero=e.page.locator('#chatThreadHeader .mf-chat-back');
   assert.equal(await zero.locator('.mf-chat-back-count').count(),0,'no count when nothing needs you');
   assert.equal(await zero.getAttribute('aria-label'),'Back to chats');
   await e.ctx.close();}
  console.log('PASS ‹ Chats: calm count of other chats needing you, in its name; zero shows none; tokens/typing never count');
  // ---- 6. long labels, widths and themes ----
  {const stub=await start({},true);
   for(const theme of Object.keys(THEMES))for(const w of [320,390,412]){
    const {ctx,page,at}=await open(stub,{w,theme,route:'/#/chat/a/alfred/long',from:'/#/settings'});
    await page.locator('#chatThreadHeader .mf-chat-back-count').waitFor({timeout:5000});
    const head=await page.evaluate(()=>{const vis=[...document.querySelector('#chatThreadHeader .chat-head').children].filter(e=>e.getClientRects().length);const R=e=>e.getBoundingClientRect();
     const boxes=vis.map(e=>({c:e.className,r:R(e)}));const overlap=[];for(let i=0;i<boxes.length;i++)for(let j=i+1;j<boxes.length;j++){const a=boxes[i].r,b=boxes[j].r;if(a.left<b.right-0.5&&b.left<a.right-0.5&&a.top<b.bottom-0.5&&b.top<a.bottom-0.5)overlap.push(boxes[i].c+' × '+boxes[j].c);}
     const sw=document.querySelector('.mf-stream-switch'),label=sw.querySelector('.mf-stream-switch-label'),who=sw.querySelector('.mf-stream-switch-sub');
     return {overlap,inside:boxes.every(b=>b.r.left>=-0.5&&b.r.right<=innerWidth+0.5),trigger:R(sw).height,who:who&&{text:who.textContent,h:Math.round(R(who).height),w:Math.round(R(who).width)},count:boxes.length,back:R(document.querySelector('#chatThreadHeader .mf-chat-back')).height,truncated:label.scrollWidth>label.clientWidth,
      pan:document.documentElement.scrollWidth>document.documentElement.clientWidth,theme:document.documentElement.dataset.theme||'default',hud:document.documentElement.dataset.hud||''};});
    assert.deepEqual(head.overlap,[],at+': head controls overlap');assert.ok(head.inside,at+': head inside the screen');
    assert.ok(head.trigger>=44&&head.back>=44,at+': 44px head targets ('+head.trigger+', '+head.back+')');
    assert.ok(head.truncated,at+': the long title truncates in the head');
    assert.equal(head.count,4,at+': the head holds ‹ · title · ＋ · ···');
    assert.ok(head.who&&head.who.text==='Alfred'&&head.who.h<=20&&head.who.w>=30,at+': who reads on one line under the title '+JSON.stringify(head.who));assert.equal(head.pan,false,at+': sideways pan');
    assert.equal(head.theme,theme==='default'?'default':'jarvis',at);assert.equal(head.hud,theme==='jarvis'?'cinematic':'',at);
    const sheetFits=async(scope,rowSel)=>page.evaluate(([scope,rowSel])=>{const host=document.querySelector(scope);const rows=[...host.querySelectorAll(rowSel)];
     return {pan:document.documentElement.scrollWidth>document.documentElement.clientWidth||host.scrollWidth>host.clientWidth+1,short:rows.filter(r=>r.getBoundingClientRect().height<44).length,
      hidden:rows.map(r=>r.querySelector('.chat-now-state')).filter(s=>!s.getClientRects().length||s.scrollWidth>s.clientWidth+1||s.getBoundingClientRect().right>innerWidth+0.5).length,rows:rows.length};},[scope,rowSel]);
    await trigger(page).click();await dialog(page).waitFor();
    const sheet=await sheetFits('.mf-sheet-body','.mf-stream-row');
    assert.equal(sheet.pan,false,at+': sheet pans sideways');assert.equal(sheet.short,0,at+': sheet rows under 44px');assert.equal(sheet.hidden,0,at+': a state is clipped or hidden');assert.equal(sheet.rows,7,at+': the open long thread is left out of the 8');
    const all=await page.getByRole('button',{name:'View all chats'}).boundingBox();assert.ok(all&&all.height>=44&&all.y+all.height<=844+0.5,at+': View all chats reachable');
    await settled(page);await page.screenshot({path:path.join(shots,`manifest-chat-switcher-${theme}-${w}.png`)});
    await page.keyboard.press('Escape');await dialog(page).waitFor({state:'hidden'});
    await page.locator('#chatThreadHeader .mf-chat-back').click();await page.locator('#chatNow .chat-now-row').first().waitFor();
    const now=await sheetFits('#chatRail','#chatNow .chat-now-row');
    assert.equal(now.pan,false,at+': Now pans sideways');assert.equal(now.short,0,at+': Now rows under 44px');assert.equal(now.hidden,0,at+': a Now state is clipped');assert.equal(now.rows,8,at+': Now rows');
    const longRow=await page.evaluate(L=>{const r=[...document.querySelectorAll('#chatNow .chat-now-row')].find(r=>r.querySelector('.chat-now-title').textContent===L);const t=r.querySelector('.chat-now-title'),s=r.querySelector('.chat-now-state');return {truncated:t.scrollWidth>t.clientWidth||t.getClientRects().length>1,state:s.textContent,shown:s.scrollWidth<=s.clientWidth+1};},LONG);
    assert.ok(longRow.truncated,at+': the long title is cut, by design');assert.equal(longRow.state,'Disconnected · outcome uncertain');assert.ok(longRow.shown,at+': its state stays whole');
    await page.screenshot({path:path.join(shots,`manifest-chat-now-${theme}-${w}.png`)});
    await ctx.close();
   }}
  console.log('PASS 320/390/412 × default/dark/JARVIS: no pan, no overlap, 44px targets, long titles cut, states whole');
  // ---- 7. desktop unchanged ----
  {const stub=await start({},true);const {ctx,page}=await open(stub,{w:1280,h:900});await settle(page);await page.waitForTimeout(400);
   const d=await page.evaluate(()=>({switcher:document.querySelectorAll('.mf-stream-switch').length,now:[...document.querySelectorAll('#chatNow, .chat-now')].filter(e=>e.getClientRects().length).length,title:document.querySelector('#chatThreadHeader .chat-head-title')?.children.length,rows:document.querySelectorAll('#chatInboxRows .chat-rail-row').length}));
   assert.deepEqual(d,{switcher:0,now:0,title:0,rows:10},'desktop: no switcher, no Now, a plain title, the same rail');
   await ctx.close();}
  console.log('PASS desktop 1280: plain title, no Now section, rail unchanged');
  assert.deepEqual(problems,[],'page errors, console errors or 5xx responses');
 }finally{await browser.close();stubs.forEach(s=>s.server.close());}
 process.exit(0);
})().catch(e=>{console.error(e);process.exit(1);});
