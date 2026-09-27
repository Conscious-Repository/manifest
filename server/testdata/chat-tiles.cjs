// Chat tiles: whole conversations tiled like a tiling window manager.
// Drives the real 50-chat-tiles.js and 47-chat-state.js against a stub state
// server on a fake origin, with every tile a real same-origin frame, and
// proves: dwindle splits, tiling keys from the page AND from inside a tile,
// frames never reload when tiles swap, resize, flip, go full screen or change
// workspace, a new chat's route is followed into the saved arrangement, the
// arrangement survives a reload, and phone width shows one tile with tabs.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
const read=f=>fs.readFileSync(path.join(root,f),'utf8');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
 const context=await browser.newContext({viewport:{width:1440,height:900}});
 const errors=[];let state={key:'inbox',slot:'tiles',revision:0,value:null},puts=0;
 const css=['00-core','05-primitives','48-chat','50-chat-tiles'].map(n=>read('css/'+n+'.css')).join('\n');
 const stubs=`
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatEmbedded=false;
  window.chatRoster=[{name:'alfred',label:'Alfred',enabled:true,model:'claude-fable-5-1'}];
  window.chatSessions=[];window.chatAgentSessions={alfred:[{id:'a1',title:'Plan the launch',updated:'2026-09-26T10:00:00Z'}]};
  window.chatTermEnabled=true;window.chatTermKinds={codex:'Codex',claude:'Claude Code'};
  window.chatTermList=k=>k==='codex'?[{id:'c1',kind:'codex',name:'Refactor auth',lastUsed:'2026-09-26T11:00:00Z'}]:[];
  window.chatTaskThreads=[];window.chatTaskEntry=t=>({taskThread:true,session:t});
  window.chatEntryLifecycle=()=> 'active';
  window.chatEntryState=e=>e.session.id==='c1'?{execution:'running',label:'Working'}:{execution:'completed',label:'Run finished'};
  window.chatIsTerm=n=>n in chatTermKinds;window.chatPrivateCreationAgent=a=>a;window.chatAgentLabel=a=>a==='alfred'?'Alfred':a;
  window.shortModel=m=>m.replace(/^claude-/,'');window.chatLoadInbox=async()=>{};
  window.reviewDialog=(title,build)=>{const d=document.createElement('dialog');const body=el('div'),actions=el('div');d.append(el('h2','',title),body,actions);document.body.append(d);build({body,actions,close:()=>d.remove()});d.showModal();};`;
 const page_html=`<!doctype html><html><head><meta charset="utf-8"><style>${css}</style><style>body{margin:0;padding:16px}</style></head><body>
  <section class="chat-page" id="chatView"><div class="chat-shell"><div class="chat-main"></div></div></section>
  <script>${stubs}</script><script>${read('js/47-chat-state.js')}</script><script>${read('js/50-chat-tiles.js')}</script></body></html>`;
 const frame_html=`<!doctype html><html><head><meta charset="utf-8"></head><body style="margin:0"><h3 class="chat-head-title"></h3><div id="chatComposer"><textarea aria-label="Message"></textarea></div>
  <script>window.__born=Math.random();const h=document.querySelector('.chat-head-title');const paint=()=>{h.textContent='Frame '+location.hash};paint();addEventListener('hashchange',paint);</script></body></html>`;
 await context.route('http://tiles.test/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname==='/api/chat/state/inbox/tiles'){
   if(route.request().method()==='PUT'){const body=JSON.parse(route.request().postData());if(body.revision!==state.revision)return route.fulfill({status:409,contentType:'application/json',body:JSON.stringify(state)});puts++;state={...state,revision:state.revision+1,value:body.value};}
   return route.fulfill({contentType:'application/json',body:JSON.stringify(state)});
  }
  if(url.searchParams.get('chatPane')==='1')return route.fulfill({contentType:'text/html',body:frame_html});
  return route.fulfill({contentType:'text/html',body:page_html});
 });
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 const open=async()=>{await page.goto('http://tiles.test/');await page.evaluate(()=>chatTilesShow(''));};
 await open();
 await page.getByText('Workspace 1 is empty').waitFor();
 // Alt+Enter: a tile with the launcher; choose an existing conversation.
 await page.keyboard.press('Alt+Enter');
 await page.getByRole('button',{name:/Refactor auth/}).click();
 const frame1=page.frameLocator('.chat-tile-frame').first();
 await frame1.getByText('Frame #/chat/a/codex/c1').waitFor();
 assert.match(await page.locator('.chat-tile').first().getAttribute('aria-label'),/Refactor auth · Codex · Working/);
 // A second tile splits the focused one along its longer side (a row).
 await page.getByRole('button',{name:'New tile'}).click();
 await page.getByRole('button',{name:'Claude Code',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===2);
 const boxes=async()=>page.locator('.chat-tile:not([hidden])').evaluateAll(l=>l.map(b=>{const r=b.getBoundingClientRect();return {id:b.dataset.tile,x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height),focused:b.classList.contains('is-focused')};}));
 let b=await boxes();
 assert.equal(b.length,2);assert.equal(b[0].y,b[1].y,'second tile should sit beside the first');assert.ok(b[1].x>b[0].x+b[0].w,'gap between tiles');
 assert.equal(b[1].focused,true,'the new tile takes focus');
 // The new chat becomes a session: the frame's own navigation is saved.
 await page.evaluate(()=>{const f=[...document.querySelectorAll('.chat-tile-frame')][1];f.contentWindow.location.hash='#/chat/a/claude/s9';});
 await page.waitForFunction(()=>Object.values(chatTilesState.tiles).some(t=>t.route==='#/chat/a/claude/s9'));
 const born=await page.evaluate(()=>[...document.querySelectorAll('.chat-tile-frame')].map(f=>f.contentWindow.__born));
 // Keys from INSIDE a tile: focus the right tile's composer, Alt+← moves focus left.
 await page.frameLocator('.chat-tile-frame').nth(1).getByRole('textbox',{name:'Message'}).focus();
 await page.keyboard.press('Alt+ArrowLeft');
 b=await boxes();assert.equal(b[0].focused,true,'Alt+← inside a tile focuses its left neighbour');
 const left=b[0].id;
 // Swap, grow, flip, full screen, workspace moves: no frame ever reloads.
 await page.keyboard.press('Alt+Shift+ArrowRight');
 b=await boxes();assert.equal(b.find(x=>x.id===left).x>b.find(x=>x.id!==left).x,true,'swap moves the focused tile right');
 const before=b.find(x=>x.id===left).w;await page.keyboard.press('Alt+Equal');
 b=await boxes();assert.ok(b.find(x=>x.id===left).w>before+20,'Alt+= grows the focused tile');
 await page.keyboard.press('Alt+KeyT');b=await boxes();assert.equal(b[0].x,b[1].x,'Alt+T stacks the pair');
 await page.keyboard.press('Alt+KeyF');b=await boxes();assert.equal(b.length,1,'full screen shows only the focused tile');
 await page.keyboard.press('Alt+KeyF');b=await boxes();assert.equal(b.length,2);
 await page.keyboard.press('Alt+Shift+Digit2');
 await page.waitForFunction(()=>chatTilesState.active==='1'&&!!chatTilesState.ws['2']);
 b=await boxes();assert.equal(b.length,1,'moved tile leaves workspace 1');
 await page.keyboard.press('Alt+Digit2');b=await boxes();assert.equal(b.length,1);assert.equal(b[0].id,left);
 await page.keyboard.press('Alt+Digit1');
 const after=await page.evaluate(()=>[...document.querySelectorAll('.chat-tile-frame')].map(f=>f.contentWindow.__born));
 assert.deepEqual([...after].sort(),[...born].sort(),'a frame reloaded during layout changes');
 // Mouse: drag a gutter to resize.
 await page.keyboard.press('Alt+Digit2');await page.keyboard.press('Alt+Shift+Digit1');await page.keyboard.press('Alt+Digit1');
 await page.waitForFunction(()=>document.querySelectorAll('.chat-tile:not([hidden])').length===2);
 const g=await page.locator('.chat-tiles-gutter').boundingBox();
 const pre=await boxes();
 await page.mouse.move(g.x+g.width/2,g.y+g.height/2);await page.mouse.down();await page.mouse.move(g.x+g.width/2+(g.width>g.height?0:-120),g.y+g.height/2+(g.width>g.height?-120:0),{steps:5});await page.mouse.up();
 const post=await boxes();assert.notDeepEqual(post.map(x=>[x.w,x.h]),pre.map(x=>[x.w,x.h]),'dragging the gutter resizes');
 // Persisted: the arrangement reaches the server and comes back on reload.
 await page.evaluate(()=>chatTilesStore.flush());
 assert.ok(puts>0,'arrangement never saved');
 const saved=JSON.stringify(state.value.tiles);
 await open();
 await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===2);
 assert.equal(JSON.stringify(await page.evaluate(()=>chatTilesSnapshot().tiles)),saved,'arrangement not restored');
 // Close keeps the conversation: the tile leaves; the other fills the stage.
 await page.keyboard.press('Alt+KeyW');
 b=await boxes();assert.equal(b.length,1);
 const stage=await page.locator('.chat-tiles-stage').boundingBox();assert.ok(Math.abs(b[0].w-stage.width)<3,'remaining tile fills the stage');
 // Help lists the keys; phone width shows one tile and names the rest.
 await page.keyboard.press('Alt+Enter');await page.getByRole('button',{name:/Plan the launch/}).click();
 await page.frameLocator('.chat-tile-frame').last().getByText('Frame #/chat/a/alfred/a1').waitFor();
 await page.keyboard.press('Alt+Slash');await page.getByText('Swap with a neighbour').waitFor();await page.getByRole('button',{name:'Close',exact:true}).click();
 for(const theme of ['default','jarvis'])for(const width of [1440,390]){
  await page.setViewportSize({width,height:844});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
  await page.waitForTimeout(50);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
  if(width===390){
   b=await boxes();assert.equal(b.length,1,'phone shows one tile');
   assert.equal(await page.locator('.chat-tiles-tab').count(),2,'phone names every tile');
   assert.ok((await page.locator('.chat-tiles-tab').allTextContents()).every(t=>t&&!/^New /.test(t)),'a tile kept its stale "New" title');
   const other=page.locator('.chat-tiles-tab[aria-selected="false"]').first();const name=await other.textContent();await other.click();
   assert.equal(await page.locator('.chat-tiles-tab[aria-selected="true"]').textContent(),name,'tapping a tab focuses that tile');
   assert.ok(await page.locator('.chat-tile:not([hidden]) .chat-tile-act').last().evaluate(e=>e.getBoundingClientRect().height)>=44,'tile controls too small on phone');
  }
  if(process.env.MANIFEST_FIXTURE_SHOTS)await page.screenshot({path:path.join(process.env.MANIFEST_FIXTURE_SHOTS,'tiles-'+theme+'-'+width+'.png')});
 }
 assert.deepEqual(errors,[]);console.log('PASS: dwindle tiling, keys from page and tile, no frame reloads, followed routes, saved arrangement, gutter drag, phone tabs.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
