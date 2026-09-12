// chat-mobile-chrome.cjs — the phone conversation chrome from the 2026-09-12
// reference pass against ChatGPT mobile (95-mobile.css Rev 6):
//   1. the composer is one rounded row (56–64px) while empty and unfocused;
//      focused or holding text it becomes textarea-over-controls and grows
//      only as the text wraps, without horizontal overflow;
//   2. an open conversation's head starts with an accessible "Back to chats"
//      control that opens the Chats fold and hands focus to "Close chats";
//   3. the workspace opener is out of the primary phone head and lives in the
//      ··· menu instead, while the desktop head keeps its icon button.
// Static DOM + the real CSS and the real head/fold code, so the checks are
// deterministic and need no server. Run:
//   NODE_PATH=<node_modules with playwright> PLAYWRIGHT_CHANNEL=chromium node server/testdata/chat-mobile-chrome.cjs
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
const read=f=>fs.readFileSync(path.join(root,f),'utf8');
const slice=(src,start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const page=await browser.newPage({viewport:{width:390,height:844},hasTouch:true,isMobile:true});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>route.abort());
  await page.setContent(`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"></head><body><div class="app-shell" id="appShell"><div class="crumb-bar" id="crumbBar"><span class="crumb-path"><span class="crumb-seg">Chat</span></span></div><nav id="railGroups"></nav>
   <section class="chat-page" id="chatView"><div class="chat-shell"><aside class="chat-rail" id="chatRail"><div class="chat-inbox-rows" id="chatInboxRows"><a class="chat-rail-row" href="#/chat/a/claude/x">claude</a></div></aside>
    <div class="chat-main term has-composer-recipient"><div class="chat-transcript" id="chatTranscript"><p>Transcript</p></div>
     <div class="chat-composer input-surface" id="chatComposer" data-built="1"><textarea class="chat-input" rows="1" aria-label="Message" placeholder="Message…"></textarea><button class="chat-attach" aria-label="Attach files" hidden>＋</button><button class="chat-send" aria-label="Send message">↑</button><button class="mic-btn" aria-label="Voice">🎙</button><button class="sprt-quiet chat-composer-recipient" aria-label="Choose agent or model">fable ⌄</button></div>
    </div></div></section></div></body></html>`);
  for(const name of ['00-core','05-primitives','48-chat','95-mobile'])await page.addStyleTag({content:read('css/'+name+'.css')});
  await page.addStyleTag({content:'body{margin:0}.chat-shell{height:700px}'});
  const chat=read('js/48-chat.js'),workspace=read('js/49-chat-workspace.js');
  await page.evaluate(()=>{
   window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text!==undefined)e.textContent=text;return e;};
   window.els={chatView:document.getElementById('chatView'),aionView:{hidden:true}};
   window.chatMarkViewed=()=>{};window.chatRestoreWorkspace=()=>{};window.chatPolishComposer=()=>{};window.chatEmbedded=false;window.chatWorkspaceTabs=null;
   window.opened=0;window.chatEnsureWorkspace=()=>{window.opened++;};
  });
  await page.addScriptTag({content:slice(chat,'function chatFocusKey','\nconst chatDrafts')});
  await page.addScriptTag({content:slice(workspace,'function chatWorkspaceIcon','\nfunction chatEnsureWorkspace')});
  await page.addScriptTag({content:read('js/98-mobile.js')});
  // the composer's auto-grow, exactly as renderChatComposer wires it
  const growLine=slice(chat,'  const grow = () =>','\n');
  await page.addScriptTag({content:`(()=>{const ta=document.querySelector('#chatComposer textarea');${growLine.trim()};ta._grow=grow;ta.addEventListener('input',grow);})();`});
  const mountHead=()=>page.evaluate(()=>{
   const head=el('div','sprt-head chat-head');head.append(el('span','sprt-title chat-head-title','claude'));
   head.append(el('button','sprt-quiet chat-terminal-view','Terminal'));
   const more=el('details','chat-details');more.append(el('summary','','More'),el('div','chat-head-meta','Claude Code · done'));
   const acts=el('span','chat-head-acts');acts.append(el('button','sprt-quiet','Rename'));more.append(acts);head.append(more);
   chatMountHeader(head);
  });
  const box=sel=>page.evaluate(sel=>{const e=document.querySelector(sel);if(!e)return null;const r=e.getBoundingClientRect();return getComputedStyle(e).display==='none'?null:{x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height)};},sel);
  const overflow=()=>page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
  const ta=page.getByLabel('Message',{exact:true});

  for(const width of [360,390,412]){
   await page.setViewportSize({width,height:844});await mountHead();await page.evaluate(()=>document.activeElement?.blur());
   // 1a. compact: one row, 56–64px
   let composer=await box('#chatComposer'),input=await box('#chatComposer textarea'),send=await box('.chat-send'),mic=await box('.mic-btn'),recipient=await box('.chat-composer-recipient');
   assert.ok(composer.h>=56&&composer.h<=64,`${width}: empty composer is ${composer.h}px, expected 56–64`);
   assert.equal(input.y,send.y,`${width}: empty textarea shares the control row`);assert.equal(mic.y,send.y);assert.equal(recipient.y,send.y);
   assert.ok(send.h>=44&&send.w>=44&&mic.h>=44,`${width}: send/mic keep 44px targets`);
   assert.equal(await overflow(),false,`${width}: compact overflow`);
   // 1b. focus: textarea takes its own full-width row above the controls
   await ta.focus();composer=await box('#chatComposer');input=await box('#chatComposer textarea');send=await box('.chat-send');
   assert.ok(input.y<send.y,`${width}: focused textarea sits above the controls`);
   assert.ok(input.w>=composer.w-40,`${width}: focused textarea is full width (${input.w} of ${composer.w})`);
   const focusedH=composer.h;
   // 1c. wrapped text grows the composer; nothing pans sideways
   await page.keyboard.type('A message long enough to wrap onto a second and a third line at phone width so the composer has to grow.');
   composer=await box('#chatComposer');assert.ok(composer.h>focusedH,`${width}: wrapped text grows the composer (${composer.h} vs ${focusedH})`);
   assert.equal(await overflow(),false,`${width}: typed overflow`);
   // 1d. blur keeps the text readable at full width; clearing returns to the pill
   await page.evaluate(()=>document.activeElement.blur());input=await box('#chatComposer textarea');send=await box('.chat-send');assert.ok(input.y<send.y,`${width}: text keeps the expanded layout when unfocused`);
   await ta.fill('');await page.evaluate(()=>{document.querySelector('#chatComposer textarea').dispatchEvent(new Event('input',{bubbles:true}));document.activeElement.blur();});
   composer=await box('#chatComposer');assert.ok(composer.h>=56&&composer.h<=64,`${width}: cleared composer returns to ${composer.h}px`);
  }
  await page.setViewportSize({width:390,height:844});await mountHead();

  // 2. back to chats: first control in the head, 44px, keyboard usable, hands off to Close chats
  const back=page.getByRole('button',{name:'Back to chats'});
  assert.equal(await back.isVisible(),true,'Back to chats is visible in the phone head');
  const backBox=await back.boundingBox(),headBox=await box('#chatThreadHeader .chat-head');
  assert.ok(backBox.height>=44&&backBox.width>=44,'Back to chats is a 44px target');assert.ok(backBox.x<headBox.x+20,'Back to chats leads the head');
  assert.equal(await page.locator('.chat-shell > .mf-chat-toggle').isVisible(),false,'the shell fold toggle is hidden while the head shows the back control');
  await back.focus();await page.keyboard.press('Enter');
  assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),true,'Enter opens the Chats fold');
  const toggle=page.locator('.chat-shell > .mf-chat-toggle');
  assert.equal(await toggle.textContent(),'Close chats');assert.equal(await toggle.evaluate(e=>e===document.activeElement),true,'focus lands on Close chats');
  assert.ok((await toggle.boundingBox()).height>=44,'Close chats is a 44px target');
  assert.equal(await page.locator('.chat-main').isVisible(),false,'the conversation yields to the list');
  await toggle.click();
  assert.equal(await page.evaluate(()=>document.querySelector('.chat-shell').classList.contains('mf-chat-nav-open')),false,'Close chats returns to the conversation');
  assert.equal(await back.isVisible(),true);
  // landing (no head): the shell toggle is the way to the list
  await page.evaluate(()=>chatMountHeader(null));
  assert.equal(await toggle.isVisible(),true,'the landing keeps the shell fold toggle');assert.equal(await toggle.textContent(),'‹ Chats');
  await mountHead();

  // 3. the workspace opener leaves the primary phone head for the ··· menu
  assert.equal(await page.locator('.chat-head > .chat-workspace-toggle').isVisible(),false,'no workspace opener in the primary phone head');
  const entry=page.locator('.chat-details > .mf-chat-ws-more');
  assert.equal(await entry.isVisible(),false,'the menu entry is hidden until ··· opens');
  await page.locator('.chat-details > summary').click();
  assert.equal(await entry.isVisible(),true,'··· lists Workspace');assert.equal(await entry.textContent(),'Workspace');
  assert.equal(await entry.getAttribute('aria-expanded'),'false');assert.ok((await entry.boundingBox()).height>=40);
  await entry.click();assert.equal(await page.evaluate(()=>window.opened),1,'the menu entry opens the workspace');

  // desktop: the head keeps its icon button, the menu entry stays out, the composer keeps its two-row anatomy
  await page.setViewportSize({width:1000,height:900});await mountHead();
  assert.equal(await page.locator('.mf-chat-back').isVisible(),false,'no back control on desktop');
  assert.equal(await page.locator('.chat-head > .chat-workspace-toggle').isVisible(),true,'desktop keeps the workspace icon button');
  await page.locator('.chat-details > summary').click();assert.equal(await entry.isVisible(),false,'desktop ··· has no Workspace entry');
  await page.evaluate(()=>{document.querySelector('.chat-details').open=false;document.activeElement?.blur();});
  const dInput=await box('#chatComposer textarea'),dSend=await box('.chat-send');
  assert.ok(dInput.y<dSend.y&&dInput.h>=72,'desktop composer is unchanged: full-width textarea (min 72px) over the controls');
  assert.equal(await page.locator('.chat-shell > .mf-chat-toggle').isVisible(),false,'no fold toggle on desktop');

  assert.deepEqual(errors,[]);
  console.log('PASS: phone composer pill → expanded → grows with wrapped text; Back to chats in the head with Close chats hand-off; workspace opener behind ···; desktop head and composer unchanged.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
