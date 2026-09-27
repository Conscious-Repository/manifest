// chat-status-line.cjs — the live status line (49-chat-status.js) and the
// reply footer's "Worked for", with the real stylesheets:
//   1. a working coding agent: verb, active time since the run started, the
//      step in progress, Stop; the one-second timer never replaces the Stop
//      button; Stop and Esc in an empty composer send the agent's own Esc,
//      Esc with text does not;
//   2. at rest: the context meter from the CLI's own accounting — a
//      percentage only when the window was recorded;
//   3. a native agent's running delivery shows time but no Stop (its receipt-
//      backed interrupt lives in the transcript);
//   4. a finished reply says "Worked for 4m 12s"; nothing overflows at 390px.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
 const page=await browser.newPage({viewport:{width:1440,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',route=>route.abort());
 await page.setContent('<meta charset="utf-8"><main class="chat-main term"><div class="chat-transcript" id="chatTranscript"></div><div class="chat-composer" id="chatComposer"><textarea class="chat-input" aria-label="Message"></textarea></div></main>');
 for(const name of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.keys=[];window.chatTermKey=async k=>{keys.push(k);};
  window.chatIsTerm=()=>window.term;window.chatIsPortal=()=>false;window.chatOpenId='s1';window.term=true;
  window.chatTermFind=()=>window.listed;window.fmtWhen=()=>'10:04 AM';window.chatEmbedded=false;
  window.chatSplitUserMessage=text=>({text,files:[]});window.chatProposalBlocks=t=>t.blocks||[];window.chatQuestionReplyDisplay=t=>t;window.renderMarkdown=t=>el('p','md-p',t);
  const started=new Date(Date.now()-65000).toISOString();
  window.listed={id:'s1',agentState:'working'};
  window.chatTermOpen={id:'s1',live:true,se:{id:'s1',kind:'codex',run:{state:'running',at:started}},context:null,planRevisions:{},
   turns:[{id:'u',who:'user',ts:started,text:'run the tests'},{id:'a',who:'assistant',ts:started,blocks:[{t:'step',cast:'exec_command',input:'go test ./server',id:'c1'}]}]};
 });
 const lib=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');
 await page.addScriptTag({content:lib.slice(lib.indexOf('const keyedChildrenCache'),lib.indexOf('// ---- pill factory'))});
 const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 await page.addScriptTag({content:chat.slice(chat.indexOf('const chatActivityOpen'),chat.indexOf('// ---- the live strip:'))});
 await page.addScriptTag({content:fs.readFileSync(path.join(root,'js/49-chat-status.js'),'utf8')});
 await page.evaluate(()=>chatStatusPaint());
 const line=page.getByRole('status',{name:'Agent working'});await line.waitFor();
 assert.equal(await line.locator('.chat-status-verb').textContent(),'Working');
 assert.match(await line.locator('.chat-status-time').textContent(),/^1m 0[5-9]s$/);
 assert.equal((await line.locator('.chat-status-step').textContent()).trim(),'exec_command go test ./server');
 const stop=line.getByRole('button',{name:'Stop'});
 await page.evaluate(()=>{window.stopNode=document.querySelector('.chat-status-stop');});
 await page.waitForTimeout(1200);
 assert.equal(await page.evaluate(()=>window.stopNode===document.querySelector('.chat-status-stop')),true,'the timer replaced the Stop button');
 assert.match(await line.locator('.chat-status-time').textContent(),/^1m (0[6-9]|1\d)s$/,'the timer did not advance');
 await stop.click();assert.deepEqual(await page.evaluate(()=>keys),['esc'],'Stop sends the agent its own Esc');
 await page.evaluate(()=>{keys.length=0;chatStatusPaint();});
 const ta=page.getByRole('textbox',{name:'Message'});
 await ta.fill('half-typed');await ta.press('Escape');assert.deepEqual(await page.evaluate(()=>keys),[],'Esc with text must not interrupt');
 await ta.fill('');await ta.press('Escape');assert.deepEqual(await page.evaluate(()=>keys),['esc'],'Esc in an empty composer interrupts');
 // 2. at rest: context
 await page.evaluate(()=>{listed.agentState='idle';chatTermOpen.se.run={state:'completed'};chatTermOpen.context={used:81709,window:258400};chatStatusPaint();});
 const rest=page.getByRole('status',{name:'Context'});await rest.waitFor();
 assert.equal(await rest.locator('.chat-status-context').textContent(),'Context 82k of 258k · 32%');
 assert.equal(await rest.locator('.chat-status-stop').isVisible(),false);
 await page.evaluate(()=>{chatTermOpen.context={used:876601};chatStatusPaint();});
 assert.equal(await rest.locator('.chat-status-context').textContent(),'877k tokens in context','no window recorded: no percentage');
 assert.equal(await rest.locator('.chat-status-meter').isVisible(),false);
 // 3. native agent
 await page.evaluate(()=>{window.term=false;window.chatCurSession={id:'s1',deliveries:[{state:'running',updated:new Date(Date.now()-42000).toISOString()}]};chatStatusPaint();});
 await line.waitFor();
 assert.match(await line.locator('.chat-status-time').textContent(),/^4[2-4]s$/);
 assert.equal(await line.locator('.chat-status-stop').isVisible(),false,'native agents keep their own interrupt controls');
 // 4. worked for
 await page.evaluate(()=>{const host=document.getElementById('chatTranscript');chatTermPaintLines(host,[{id:'a2',who:'assistant',ts:'2026-09-26T10:00:05Z',end:'2026-09-26T10:04:17Z',blocks:[{t:'say',text:'done'}]}]);});
 assert.match(await page.locator('.chat-response-footer').textContent(),/Worked for 4m 12s/);
 for(const theme of ['default','jarvis']){
  await page.setViewportSize({width:390,height:844});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
  await page.evaluate(()=>{window.term=true;listed.agentState='working';chatTermOpen.se.run={state:'running',at:new Date(Date.now()-3723000).toISOString()};chatTermOpen.turns[1].blocks[0].input='a very long command '.repeat(12);chatStatusPaint();});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at 390');
  assert.equal(await line.locator('.chat-status-time').textContent(),'1h 02m');
  assert.ok(await stop.evaluate(e=>e.getBoundingClientRect().height)>=44,'Stop target too small on phone');
 }
 assert.deepEqual(errors,[]);console.log('PASS: working line with time, step and stable Stop; Esc rules; context with and without window; native time; worked-for; phone.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
