// Audit reproducer, not a product test: asserts the observed defect until fixed.
// Run from repo root with NODE_PATH pointing to an existing Playwright install.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=process.cwd();
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.AUDIT_CHROMIUM?{executablePath:process.env.AUDIT_CHROMIUM}:{})});
 try{
 const page=await browser.newPage({viewport:{width:1440,height:960}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',route=>route.abort()); // No production or external requests.
 await page.setContent('<section id="terminalView"><div class="term-shell"><div id="termStageTerm"><div id="termScreen"></div></div><div id="termSessionRows"></div><div id="termRuntimeStatus"></div></div></section><section class="chat-shell"><div class="chat-main">Fixture conversation stays here</div></section>');
 for(const file of ['00-core','05-primitives','48-chat','73-terminal','95-mobile'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'server/web/css/'+file+'.css'),'utf8')});
 await page.evaluate(()=>{
  window.cmdRegistry={register(){}};window.els={terminalView:document.getElementById('terminalView')};
  window.showToast=()=>{};window.chatRenderStateNotice=()=>{};window.renderMarkdown=text=>{const p=document.createElement('p');p.textContent=text;return p;};
  window.sockets=[];window.outage=false;
  window.fixture={id:'abcdef123456',name:'Controlled fixture',kind:'codex',backend:'herdr',live:true,cwd:'/fixture',runtime:{host:'fixture',session:'audit',generation:'one',workspace:'w1',pane:'p1',occupant:'t1'}};
  window.fetch=async()=>{if(window.outage)throw Error('fixture outage');return {ok:true,json:async()=>({enabled:true,connectivity:'connected',sessions:[window.fixture]})};};
  window.WebSocket=class{constructor(url){this.url=url;this.readyState=1;sockets.push(this);}send(){}close(){this.readyState=3;}};
  window.Terminal=class{constructor(){this.cols=100;this.rows=30;this.options={};this.parser={registerOscHandler(){}};}loadAddon(){}open(m){m.textContent='FIXTURE PTY OUTPUT';}write(){}onData(){}dispose(){}focus(){}};
  window.FitAddon={FitAddon:class{fit(){}}};
 });
 await page.addScriptTag({content:fs.readFileSync(path.join(root,'server/web/js/05-components.js'),'utf8')});
 await page.addScriptTag({content:fs.readFileSync(path.join(root,'server/web/js/73-terminal.js'),'utf8')});
 await page.evaluate(async()=>{termOpenId=fixture.id;await loadTermSessions();});
 assert.equal(await page.evaluate(()=>sockets.length),1);
 await page.evaluate(()=>{outage=true;termInst.ws.readyState=3;termInst.ws.onclose();});
 await page.waitForTimeout(1500); // Allow the one automatic reconnect timer to expire during outage.
 const before=await page.evaluate(()=>({connectivity:termConnectivity,sockets:sockets.length}));
 assert.equal(before.connectivity,'unavailable');
 await page.evaluate(async()=>{outage=false;await loadTermSessions(true);}); // SSE/visibility recovery path.
 const after=await page.evaluate(()=>({connectivity:termConnectivity,sockets:sockets.length,ws:termInst.ws.readyState,label:document.getElementById('termConnection').textContent}));
 assert.equal(after.connectivity,'connected');assert.equal(after.sockets,1);assert.equal(after.ws,3);
 console.log('REPRODUCED R1: restored inventory leaves closed attachment:',JSON.stringify(after));
 await page.evaluate(async()=>{detachTerm();await loadTermSessions();});
 assert.equal(await page.evaluate(()=>sockets.length),2);
 console.log('PASS: explicit Reconnect recovers the attachment');
 // Exercise actual artifactWorkspace DOM, with immutable fixture versions only.
 await page.evaluate(()=>{
  document.getElementById("terminalView").hidden=true;window.selectedContext=null;window.saved=[];
  window.fetch=async url=>{const old=String(url).includes('rev='+ 'a'.repeat(64));return {ok:true,json:async()=>({id:'fixture-plan',title:'Fixture plan',ref:'fixture.md',head:'b'.repeat(64),content:old?'Old plan':'Current plan',revisions:[{n:1,hash:'a'.repeat(64)},{n:2,hash:'b'.repeat(64)}]})};};
  document.querySelector('.chat-shell').classList.add('has-artifact');
  artifactWorkspace(document.querySelector('.chat-shell'),{load:async()=>({id:'fixture-plan',head:'b'.repeat(64)}),onDiscuss:r=>selectedContext=r,save:async(text,expectedRevision)=>{saved.push({text,expectedRevision});return {id:'fixture-plan',head:'b'.repeat(64)};}});
 });
 await page.getByRole('button',{name:'Discuss this version',exact:true}).waitFor();
 assert.equal(await page.evaluate(()=>selectedContext),null);
 await page.getByRole('button',{name:'Discuss this version',exact:true}).click();
 assert.equal((await page.evaluate(()=>selectedContext)).revision,'b'.repeat(64));
 await page.getByRole('combobox',{name:'Artifact version'}).selectOption('1');
 await page.getByRole('button',{name:'Restore this version',exact:true}).click();
 await page.getByRole('textbox',{name:'Plan content'}).fill('Restored and edited fixture plan');
 await page.getByRole('button',{name:'Review changes',exact:true}).click();
 await page.getByRole('button',{name:'Edit text',exact:true}).click();
 assert.equal(await page.getByRole('textbox',{name:'Plan content'}).inputValue(),'Restored and edited fixture plan');
 await page.getByRole('button',{name:'Save restored version',exact:true}).click();
 assert.deepEqual(await page.evaluate(()=>saved),[{text:'Restored and edited fixture plan',expectedRevision:'b'.repeat(64)}]);
 console.log('PASS: preview sends nothing; exact version selection; plan restore/edit/review/CAS save');
 for(const theme of ['default','jarvis'])for(const width of [1440,1000,390]){
  await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
  await page.screenshot({path:'/tmp/manifest-chat-audit-'+theme+'-'+width+'.png'});
  console.log('LAYOUT',JSON.stringify(await page.evaluate(()=>({conversationVisible:getComputedStyle(document.querySelector(".chat-main")).display!=="none",width:innerWidth,documentWidth:document.documentElement.scrollWidth,paneWidth:document.querySelector('.artifact-workspace').getBoundingClientRect().width,backToChat:!!document.querySelector('.artifact-workspace-head button')}))));
 }
 assert.deepEqual(errors,[]);console.log('PASS: zero browser page errors (isolated component fixture; no live backend/PTY claim)');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
