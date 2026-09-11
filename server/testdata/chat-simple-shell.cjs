const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:'chrome'});try{
 const page=await browser.newPage();const root=path.join(__dirname,'../web');
 await page.setContent('<main class="chat-shell"><aside class="chat-rail" id="chatRail"><div class="chat-inbox-controls"><input class="chat-inbox-search" placeholder="Search chats"><div class="chat-inbox-filters"><select><option>All agents</option></select><select><option>All workstreams</option></select></div></div><div class="chat-inbox-rows"><div class="chat-rail-row open"><div class="chat-rail-title">Improve the recruiting experience</div><div class="chat-rail-meta">Codex · today</div></div></div></aside><section class="chat-main term"><div id="chatTranscript" class="chat-transcript"><p>The candidate review is ready. Open Changes to review the implementation, or continue the conversation below.</p></div><div id="chatComposer" class="chat-composer"><textarea class="chat-input" placeholder="Message Codex…"></textarea><button class="chat-send">↑</button></div></section></main>');
 for(const f of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await page.addStyleTag({content:'body{margin:0;padding:16px}.chat-shell{height:calc(100vh - 32px)}#chatTranscript{flex:1;overflow:auto}'});
 await page.evaluate(()=>{
 window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};
 document.documentElement.dataset.theme="jarvis";
 Object.assign(window,{chatTermKinds:{codex:'Codex'},chatRecipients:new Map(),chatTermEnabled:true,chatRoster:[],terminalStateDot:()=>el('span','','●'),terminalStateLabel:()=> 'working',chatAgentLabel:x=>x,fmtWhen:()=> 'today',chatTermEndIsKill:()=>true,armedDelete:()=>el('button','','End session'),chatTermRename:()=>{},chatTermEnd:()=>{},chatTermOpenInTerminal:()=>{},chatChangesButton:()=>el('button','sprt-quiet','Changes'),chatTaskThreadHash:()=> '#/task',chatChooseTerminalRecipient:()=>{},chatTermKey:()=>{}});
 window.o={se:{id:'fixture',kind:'codex',name:'codex · '+('Fix the concrete defects and audit usability. '.repeat(15)),cwd:'/fixture',backend:'herdr'},live:true,conversation:{links:[{kind:'task',id:'fixture'}]},turns:[],screen:['Codex is working','> Review the changes']};window.chatTermOpen=o;
 });
 const source=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 for(const [start,end] of [['function chatMountHeader','const chatDrafts'],['function chatTermHead(o)','function chatTermRepaintHead'],['const chatTermQuickKeys','async function chatTermScreenFetch']])await page.addScriptTag({content:source.slice(source.indexOf(start),source.indexOf(end))});
 await page.evaluate(()=>{chatMountHeader(chatTermHead(o));chatTermPaintStrip();});
 for(const width of [1440,390]){
 await page.setViewportSize({width,height:850});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'overflow '+width);
 const title=await page.locator('.chat-head-title').boundingBox();assert.ok(title.height<40,'title is a scrolling paragraph');
 await page.getByRole('button',{name:'Terminal',exact:true}).click();assert.equal(await page.locator('.chat-main').evaluate(e=>e.classList.contains('terminal-focus')),true);
 await page.getByRole('button',{name:'Conversation',exact:true}).click();assert.equal(await page.locator('.chat-main').evaluate(e=>e.classList.contains('terminal-focus')),false);
 await page.locator('.chat-details > summary').click();await page.getByRole('button',{name:'Rename',exact:true}).waitFor();await page.locator('.chat-details > summary').click();
 await page.screenshot({path:'/tmp/manifest-simple-chat-'+width+'.png'});
 }
 console.log('PASS: long titles, compact header, More actions, in-chat Terminal/Conversation switch and phone bounds.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
