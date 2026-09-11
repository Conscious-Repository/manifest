// Regression companion to the original September 11 audit reproducer.
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
  window.WebSocket=class{constructor(url){this.url=url;this.readyState=1;sockets.push(this);queueMicrotask(()=>this.onopen?.());}send(){}close(){this.readyState=3;}};
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
 assert.equal(after.connectivity,'connected');assert.equal(after.sockets,2);assert.equal(after.ws,1);assert.equal(after.label,'Connected');
 console.log('PASS: restored inventory reattaches:',JSON.stringify(after));
 await page.evaluate(async()=>{detachTerm();await loadTermSessions();});
 assert.equal(await page.evaluate(()=>sockets.length),3);
 console.log('PASS: explicit Reconnect recovers the attachment');
 assert.deepEqual(errors,[]);
 console.log('PASS: browser recovery after first-retry outage; zero page errors; fixture transport only');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
