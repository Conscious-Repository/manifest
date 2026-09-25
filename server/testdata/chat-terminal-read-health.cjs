const assert=require('node:assert/strict'),fs=require('node:fs'),{chromium}=require('playwright');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setContent('<header id="chatThreadHeader"><h1>Conversation</h1></header><main id="chatTranscript">Retained answer</main><textarea aria-label="Instruction">Unfinished instruction</textarea>');
 for(const f of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync('server/web/css/'+f+'.css','utf8')});
 await page.evaluate(()=>{window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls;e.textContent=text;return e};window.chatTermOpen={id:'one',offset:10,turns:[],se:{backend:'herdr',run:{state:'completed'}},live:false};window.original=chatTermOpen;window.chatTermBase=()=>'/read';window.fetch=async()=>({ok:false});window.chatTermFind=()=>null;window.chatQuestionPanel=()=>{};window.chatTermRepaintHead=()=>{};window.renderChatInboxRows=()=>{};window.chatTermPaintTurns=()=>{};});
 const source=fs.readFileSync('server/web/js/48-chat.js','utf8');await page.addScriptTag({content:source.slice(source.indexOf('function chatTermReadHealth('),source.indexOf('function renderChatTermTranscript('))});await page.addScriptTag({content:source.slice(source.indexOf('async function chatTermTail('),source.indexOf('// chatTermMerge —'))});
 await page.getByLabel('Instruction').focus();await page.evaluate(()=>chatTermTail(chatTermOpen));
 await page.getByRole('status').filter({hasText:'Transcript updates unavailable'}).waitFor();
 assert.equal(await page.getByRole('status').getAttribute('aria-live'),'polite');
 await page.evaluate(()=>{document.querySelector('.chat-transcript-read-status').remove();chatTermReadHealth(chatTermOpen)});
 assert.equal(await page.getByRole('status').getAttribute('aria-live'),'off','header redraw does not reannounce the same failure');
 await page.evaluate(()=>chatTermTail(chatTermOpen));assert.equal(await page.getByRole('status').count(),1);
 assert.equal(await page.locator('#chatTranscript').innerText(),'Retained answer');assert.equal(await page.getByLabel('Instruction').inputValue(),'Unfinished instruction');assert.equal(await page.getByLabel('Instruction').evaluate(e=>e===document.activeElement),true);
 assert.equal(await page.evaluate(()=>chatTermOpen.se.run.state),'completed');
 for(const width of [320,390,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);}
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'/tmp/manifest-terminal-read-health.png'});
 await page.evaluate(()=>{fetch=async()=>({ok:true,json:async()=>({offset:10,turns:[],run:{state:'completed'}})});return chatTermTail(chatTermOpen)});
 assert.equal(await page.getByRole('status').count(),0,'successful read clears warning');
 await page.addScriptTag({content:source.slice(source.indexOf('function chatTermPaintTurns()'),source.indexOf('// ---- the terminal painter ----'))});
 await page.evaluate(()=>{const body=document.createElement('div');body.id='chatTermTurns';document.getElementById('chatTranscript').replaceChildren(body);chatTermOpen.historyAvailable=false;chatTermOpen.se.kind='claude';window.chatStick=false;window.chatPin=()=>{};window.chatTermPaintLines=()=>{};window.appendTaskApprovals=()=>{};chatTermPaintTurns();});
 await page.getByText('Transcript history is unavailable. Open Terminal to inspect this session.',{exact:true}).waitFor();
 await page.evaluate(()=>{chatTermOpen.se.launchPhase='draft';chatTermPaintTurns()});
 await page.getByText('Review your draft below. Sending starts the coding session.',{exact:true}).waitFor();
 await page.evaluate(()=>{chatTermOpen={id:'two'};chatTermReadHealth(original,true)});assert.equal(await page.getByRole('status').count(),0,'late failure cannot mark another conversation');assert.deepEqual(errors,[]);
 console.log('PASS: visible transcript read failure, retained work/draft/focus, recovery and thread isolation');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
