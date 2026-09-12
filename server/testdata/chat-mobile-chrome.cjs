// chat-mobile-chrome.cjs — the phone conversation chrome from the 2026-09-12
// reference passes against ChatGPT mobile (95-mobile.css Rev 6 + Rev 7):
//   1. the composer is one low rounded row (50–58px) whether empty, focused
//      or holding a one-line message; once the text wraps (or Enter adds a
//      line) the textarea takes its own full-width row and the composer grows
//      only with the text, without horizontal overflow, until it is cleared.
//      The model label is quiet text (12px, no pill) and send is a 36px
//      neutral circle inside a 44px hit box that turns to ink once there is
//      text; the mic keeps its 44px box;
//   2. an open conversation's head starts with an accessible "Back to chats"
//      control that opens the Chats fold and hands focus to "Close chats";
//   3. the workspace opener is out of the primary phone head and lives in the
//      ··· menu instead, while the desktop head keeps its icon button.
// Static DOM + the real CSS and the real head/fold/mic/grow code, so the
// checks are deterministic and need no server. Run:
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
     <div class="chat-composer input-surface" id="chatComposer" data-built="1"><textarea class="chat-input" rows="1" aria-label="Message" placeholder="Message…"></textarea><button class="chat-attach" aria-label="Attach files" hidden>＋</button><span class="chat-ritual" hidden><button class="filter-chip">ask</button><button class="filter-chip">propose</button></span><button class="chat-send" aria-label="Send message">↑</button><button class="sprt-quiet chat-composer-recipient" aria-label="Choose agent or model">gpt-6-astra ⌄</button></div>
    </div></div></section></div></body></html>`);
  for(const name of ['00-core','05-primitives','48-chat','75-bars','95-mobile'])await page.addStyleTag({content:read('css/'+name+'.css')});
  await page.addStyleTag({content:'body{margin:0}.chat-shell{height:700px}'});
  const chat=read('js/48-chat.js'),workspace=read('js/49-chat-workspace.js'),mic=read('js/79-mic.js');
  await page.evaluate(()=>{
   window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text!==undefined)e.textContent=text;return e;};
   window.els={chatView:document.getElementById('chatView'),aionView:{hidden:true}};
   window.chatMarkViewed=()=>{};window.chatRestoreWorkspace=()=>{};window.chatPolishComposer=()=>{};window.chatEmbedded=false;window.chatWorkspaceTabs=null;
   window.opened=0;window.chatEnsureWorkspace=()=>{window.opened++;};
  });
  await page.addScriptTag({content:slice(chat,'function chatFocusKey','\nconst chatDrafts')});
  await page.addScriptTag({content:slice(chat,'document.addEventListener("click"','\n')});
  await page.addScriptTag({content:slice(read('js/05-components.js'),'function inlineRename','\n}\n')+'\n}'});
  await page.addScriptTag({content:slice(workspace,'function chatWorkspaceIcon','\nfunction chatEnsureWorkspace')});
  await page.addScriptTag({content:slice(mic,'function micButton','\nasync function micToggle')});
  await page.addScriptTag({content:read('js/98-mobile.js')});
  // the real mic control (79-mic.js mounts it after the send) and the
  // composer's auto-grow + shape sync, exactly as renderChatComposer wires them
  await page.evaluate(()=>{const host=document.getElementById('chatComposer');host.insertBefore(micButton(()=>{}),host.querySelector('.chat-composer-recipient'));});
  await page.addScriptTag({content:slice(read('js/05-components.js'),'const textareaMeasureCache','// ---- pill factory')});
  const growLine=slice(chat,'  const grow = () =>','\n');
  await page.addScriptTag({content:`(()=>{const host=document.getElementById('chatComposer'),ta=host.querySelector('textarea');${growLine.trim()};ta._grow=grow;ta.addEventListener('input',grow);grow();})();`});
  const mountHead=()=>page.evaluate(()=>{
   const head=el('div','sprt-head chat-head');head.append(el('span','sprt-title chat-head-title','claude'));
   head.append(el('button','sprt-quiet chat-terminal-view','Terminal'));
   const more=el('details','chat-details');more.append(el('summary','','More'),el('div','chat-head-meta','Claude Code · done'));
   const ren=el('button','sprt-quiet','Rename');ren.onclick=e=>{e.stopPropagation();inlineRename(head.querySelector('.chat-head-title'),'claude',()=>{});};
   const acts=el('span','chat-head-acts');acts.append(ren);more.append(acts);head.append(more);
   chatMountHeader(head);
  });
  const box=sel=>page.evaluate(sel=>{const e=document.querySelector(sel);if(!e)return null;const r=e.getBoundingClientRect();return getComputedStyle(e).display==='none'?null:{x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height)};},sel);
  const overflow=()=>page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
  const ta=page.getByLabel('Message',{exact:true});

  // visible geometry and paint, separate from the hit box: the padding box
  // (the transparent border is hit area only), background, colour, type
  const paint=sel=>page.evaluate(sel=>{const e=document.querySelector(sel),cs=getComputedStyle(e),r=e.getBoundingClientRect();const b=s=>parseFloat(cs['border'+s+'Width'])||0;
   return {visualW:Math.round(r.width-b('Left')-b('Right')),visualH:Math.round(r.height-b('Top')-b('Bottom')),bg:cs.backgroundColor,color:cs.color,fontSize:parseFloat(cs.fontSize),fontFamily:cs.fontFamily,radius:cs.borderRadius,borderStyle:cs.borderTopStyle,opacity:parseFloat(cs.opacity),outline:cs.outlineStyle,
    accent:getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()};},sel);
  const hexToRgb=h=>'rgb('+[1,3,5].map(i=>parseInt(h.slice(i,i+2),16)).join(', ')+')';
  const clear=async()=>{await ta.fill('');await page.evaluate(()=>{document.querySelector('#chatComposer textarea').dispatchEvent(new Event('input',{bubbles:true}));document.activeElement?.blur();});};
  const oneRow=async(label)=>{const composer=await box('#chatComposer'),input=await box('#chatComposer textarea'),send=await box('.chat-send'),mic=await box('.mic-btn'),recipient=await box('.chat-composer-recipient');
   assert.ok(composer.h>=50&&composer.h<=58,`${label}: composer is ${composer.h}px, expected 50–58`);
   assert.equal(input.y,send.y,`${label}: the textarea shares the control row`);assert.equal(mic.y,send.y,`${label}: mic on the row`);assert.equal(recipient.y,send.y,`${label}: model on the row`);
   assert.ok(input.w>=90,`${label}: the field keeps ${input.w}px of the row`);
   assert.equal(await overflow(),false,`${label}: overflow`);return composer.h;};

  for(const width of [360,390,412]){
   await page.setViewportSize({width,height:844});await mountHead();await clear();
   // 1a. compact: one low row; 44px hit boxes; quiet paint
   const emptyH=await oneRow(width+' empty');
   const send=await box('.chat-send'),mic=await box('.mic-btn'),recipient=await box('.chat-composer-recipient');let input=await box('#chatComposer textarea');
   assert.ok(send.h>=44&&send.w>=44&&mic.h>=44&&mic.w>=44&&recipient.h>=44,`${width}: send/mic/model keep 44px targets`);
   const sendPaint=await paint('.chat-send'),modelPaint=await paint('.chat-composer-recipient');
   assert.ok(sendPaint.visualW>=32&&sendPaint.visualW<=38&&sendPaint.visualH===sendPaint.visualW,`${width}: send paints a ${sendPaint.visualW}×${sendPaint.visualH} circle inside its hit box`);
   assert.equal(sendPaint.radius,'50%');assert.notEqual(sendPaint.bg,hexToRgb(sendPaint.accent),`${width}: empty send is not the accent fill`);assert.equal(sendPaint.opacity,1,`${width}: empty send is full opacity (distinct from :disabled)`);
   assert.ok(modelPaint.fontSize<=12&&!/mono|Carbon/i.test(modelPaint.fontFamily),`${width}: model label is small sans text (${modelPaint.fontSize}px ${modelPaint.fontFamily})`);
   assert.equal(modelPaint.bg,'rgba(0, 0, 0, 0)',`${width}: model label has no fill`);assert.equal(modelPaint.borderStyle,'none',`${width}: model label has no border`);
   assert.ok(recipient.w<=Math.floor((await box('#chatComposer')).w*0.34)+1,`${width}: model label stays within a third of the row (${recipient.w}px)`);
   assert.ok((await paint('#chatComposer textarea')).fontSize>=16,`${width}: 16px input text (no iOS zoom)`);
   // 1b. focus alone changes nothing (iOS honours the programmatic focus on open)
   await ta.focus();assert.equal(await oneRow(width+' focused'),emptyH,`${width}: focus keeps the row height`);
   // 1c. a one-line message stays on the row; send turns to ink; keyboard focus ring on send is visible
   await page.keyboard.type('ok');assert.equal(await oneRow(width+' short text'),emptyH);
   const inkPaint=await paint('.chat-send');assert.notEqual(inkPaint.bg,sendPaint.bg,`${width}: send fill changes once there is text`);assert.notEqual(inkPaint.bg,hexToRgb(inkPaint.accent));
   await page.keyboard.press('Tab');
   assert.equal(await page.evaluate(()=>document.activeElement.className),'chat-send',`${width}: Tab reaches send`);
   assert.equal(await page.evaluate(()=>getComputedStyle(document.activeElement).outlineStyle!=='none'&&parseFloat(getComputedStyle(document.activeElement).outlineWidth)>=2),true,`${width}: send shows a focus ring`);
   // 1d. wrapped text: the textarea takes its own full-width row above the controls; the composer grows only with the text
   await ta.focus();await page.keyboard.type(' — and a message long enough to wrap onto a second line at phone width.');
   let composer=await box('#chatComposer');input=await box('#chatComposer textarea');const send2=await box('.chat-send');
   assert.ok(input.y<send2.y,`${width}: wrapped textarea sits above the controls`);
   assert.ok(input.w>=composer.w-40,`${width}: wrapped textarea is full width (${input.w} of ${composer.w})`);
   assert.ok(composer.h>emptyH&&composer.h<=emptyH+72,`${width}: two lines grow the composer to ${composer.h}px (from ${emptyH})`);
   assert.equal(await overflow(),false,`${width}: typed overflow`);
   const twoLineH=composer.h;
   await page.keyboard.type(' A third line keeps growing it.');composer=await box('#chatComposer');assert.ok(composer.h>twoLineH,`${width}: a third line grows it further (${composer.h})`);
   // 1e. blur keeps the wrapped layout; deleting back to one line keeps it too (no flip-flop); clearing returns to the row
   await page.evaluate(()=>document.activeElement.blur());input=await box('#chatComposer textarea');assert.ok(input.y<(await box('.chat-send')).y,`${width}: text keeps the wrapped layout when unfocused`);
   await ta.fill('ok');await page.evaluate(()=>document.querySelector('#chatComposer textarea').dispatchEvent(new Event('input',{bubbles:true})));
   input=await box('#chatComposer textarea');assert.ok(input.y<(await box('.chat-send')).y,`${width}: shortened text stays on its own row until cleared`);
   await clear();assert.equal(await oneRow(width+' cleared'),emptyH);
   // 1f. Enter (a new line on phones) wraps too
   await ta.focus();await page.keyboard.type('one');await page.keyboard.press('Enter');await page.keyboard.type('two');
   input=await box('#chatComposer textarea');assert.ok(input.y<(await box('.chat-send')).y,`${width}: Enter moves the field to its own row`);
   await clear();
   // 1g. a visible ask/propose pair (portal threads) keeps the field on its own row
   await page.evaluate(()=>{document.querySelector('.chat-ritual').hidden=false;document.getElementById('chatComposer').classList.add('has-ritual');});
   input=await box('#chatComposer textarea');assert.ok(input.y<(await box('.chat-send')).y,`${width}: the ritual pair never crushes the field`);assert.equal(await overflow(),false);
   await page.evaluate(()=>{document.querySelector('.chat-ritual').hidden=true;document.getElementById('chatComposer').classList.remove('has-ritual');});
   await oneRow(width+' after ritual');
  }
  await page.setViewportSize({width:390,height:844});await mountHead();

  // Filled composer: typing at the height cap must not collapse the focused
  // field, move its box, or issue a page scroll on every character.
  await page.addScriptTag({content:slice(chat,'let chatFitBound =','\nasync function loadChatRoster')});
  await page.evaluate(()=>{
   const vv=window.visualViewport;
   Object.defineProperty(vv,'height',{configurable:true,get:()=>400});
   Object.defineProperty(vv,'offsetTop',{configurable:true,get:()=>30});
   window.scrollCalls=0;window.scrollTo=()=>window.scrollCalls++;
   chatFitShell();
  });
  const filled=page.getByRole('textbox',{name:'Message',exact:true});
  await filled.fill('A long message with multiple paragraphs.\n'.repeat(24));
  const capped=await box('#chatComposer textarea');
  await page.evaluate(()=>{
   window.heightWrites=[];
   window.heightObserver=new MutationObserver(records=>records.forEach(r=>heightWrites.push(r.oldValue)));
   heightObserver.observe(document.querySelector('#chatComposer textarea'),{attributes:true,attributeFilter:['style'],attributeOldValue:true});
  });
  await page.keyboard.type(' Every additional character stays steady.',{delay:5});
  assert.deepEqual(await box('#chatComposer textarea'),capped,'typing at the cap leaves the field geometry unchanged');
  assert.deepEqual(await page.evaluate(()=>heightWrites),[],'same-height typing does not write live textarea styles');
  await page.evaluate(()=>{for(let i=0;i<20;i++)visualViewport.dispatchEvent(new Event('scroll'));});
  assert.equal(await page.evaluate(()=>scrollCalls),0,'viewport scroll does not trigger a competing scrollTo');
  const composerBottom=await page.locator('#chatComposer').evaluate(e=>e.getBoundingClientRect().bottom);
  assert.ok(composerBottom<=430,`composer stays above the simulated keyboard (${composerBottom})`);
  await page.screenshot({path:'/tmp/manifest-chat-filled-phone.png'});
  await page.evaluate(()=>{heightObserver.disconnect();delete visualViewport.height;delete visualViewport.offsetTop;chatFitShell();});
  await filled.fill('');

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
  assert.equal(await page.evaluate(()=>document.querySelector('.chat-details').open),false,'choosing Workspace closes the ··· menu');

  // 4. Rename from ··· closes the menu and leaves the rename input on top, focused
  await page.locator('.chat-details > summary').click();assert.equal(await page.evaluate(()=>document.querySelector('.chat-details').open),true);
  await page.locator('.chat-details .chat-head-acts button',{hasText:'Rename'}).click();
  assert.equal(await page.evaluate(()=>document.querySelector('.chat-details').open),false,'Rename closes the ··· menu');
  const rename=page.locator('.chat-head input.inline-rename');
  assert.equal(await rename.isVisible(),true,'the title became a rename input');
  assert.equal(await page.evaluate(()=>document.activeElement?.classList.contains('inline-rename')),true,'the rename input holds focus');
  assert.equal(await page.evaluate(()=>{const r=document.querySelector('.chat-head input.inline-rename').getBoundingClientRect();return document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.classList.contains('inline-rename');}),true,'nothing covers the rename input');
  await page.keyboard.press('Escape');assert.equal(await page.locator('.chat-head-title').isVisible(),true,'Escape restores the title');
  // the nested details summary inside the menu keeps the menu open
  await page.evaluate(()=>{const more=document.querySelector('.chat-details');const info=el('details','chat-conversation-info');info.append(el('summary','','Conversation details'),el('p','','claude'));more.insertBefore(info,more.querySelector('.chat-head-acts'));});
  await page.locator('.chat-details > summary').click();await page.locator('.chat-conversation-info > summary').click();
  assert.equal(await page.evaluate(()=>[document.querySelector('.chat-details').open,document.querySelector('.chat-conversation-info').open]).then(v=>v.join()),'true,true','expanding Conversation details keeps ··· open');
  await page.evaluate(()=>{document.querySelector('.chat-details').open=false;document.querySelector('.chat-conversation-info').remove();});

  // desktop: the head keeps its icon button, the menu entry stays out, the composer keeps its two-row anatomy
  await page.setViewportSize({width:1000,height:900});await mountHead();
  assert.equal(await page.locator('.mf-chat-back').isVisible(),false,'no back control on desktop');
  assert.equal(await page.locator('.chat-head > .chat-workspace-toggle').isVisible(),true,'desktop keeps the workspace icon button');
  await page.locator('.chat-details > summary').click();assert.equal(await entry.isVisible(),false,'desktop ··· has no Workspace entry');
  await page.evaluate(()=>{document.querySelector('.chat-details').open=false;document.activeElement?.blur();});
  for(const width of [861,1000,1280]){
   await page.setViewportSize({width,height:900});await clear();
   const dInput=await box('#chatComposer textarea'),dSend=await box('.chat-send'),dPaint=await paint('.chat-send'),dModel=await paint('.chat-composer-recipient');
   assert.ok(dInput.y<dSend.y&&dInput.h>=72,`${width}: desktop composer is unchanged: full-width textarea (min 72px) over the controls`);
   assert.equal(dPaint.bg,hexToRgb(dPaint.accent),`${width}: desktop send keeps the accent fill`);assert.equal(dPaint.visualW,dSend.w,`${width}: desktop send has no hit-box border`);
   assert.ok(dModel.fontSize===13&&/mono|Carbon/i.test(dModel.fontFamily),`${width}: desktop model label keeps its mono type`);
   await ta.fill('ok');await page.evaluate(()=>document.querySelector('#chatComposer textarea').dispatchEvent(new Event('input',{bubbles:true})));
   assert.ok((await box('#chatComposer textarea')).y<(await box('.chat-send')).y,`${width}: desktop text keeps the two-row anatomy`);
   assert.equal((await paint('.chat-send')).bg,hexToRgb(dPaint.accent),`${width}: desktop send fill ignores the has-text state`);
   await clear();
  }
  assert.equal(await page.locator('.chat-shell > .mf-chat-toggle').isVisible(),false,'no fold toggle on desktop');

  assert.deepEqual(errors,[]);
  console.log('PASS: phone composer stays one 50–58px row until the text wraps, then grows with it; quiet model label and neutral send with 44px targets; Back to chats in the head with Close chats hand-off; workspace opener behind ···, and choosing Rename/Workspace from ··· closes it with the rename input on top; desktop head and composer unchanged at 861/1000/1280.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
