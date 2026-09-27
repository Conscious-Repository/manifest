// Attention across tiles (brief §5b.2, 2026-09-27): the real 50-chat-tiles.js
// over stub state. Every tile in every workspace is watched from the inbox
// state the tile heads show:
//   - needs you and error hold while the state does; done is a run that
//     finished while the tile was not in view, cleared by focusing it;
//   - the bar says what waits; Alt+N (or the button) jumps to the next one,
//     needs you first, then errors, then done, switching workspace;
//   - a state found on load is listed but never announced; a change on a
//     tile out of view is announced as a browser notification, only when
//     notifications are allowed; "Notify me" asks, and only when undecided.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
const read=f=>fs.readFileSync(path.join(root,f),'utf8');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
 const context=await browser.newContext({viewport:{width:1440,height:900}});
 const errors=[];
 const leaf=id=>({t:'leaf',id});
 let state={key:'inbox',slot:'tiles',revision:1,value:{v:1,active:'1',focus:{'1':'t1','2':'t3'},full:{},
  ws:{'1':{t:'split',dir:'row',ratio:0.5,a:leaf('t1'),b:leaf('t2')},'2':leaf('t3')},
  tiles:{t1:{route:'#/chat/a/alfred/a1',title:''},t2:{route:'#/chat/a/alfred/a2',title:''},t3:{route:'#/chat/a/alfred/a3',title:''}}}};
 const css=['00-core','05-primitives','48-chat','50-chat-tiles'].map(n=>read('css/'+n+'.css')).join('\n');
 const stubs=`
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatEmbedded=false;
  window.chatRoster=[{name:'alfred',label:'Alfred',enabled:true,model:'claude-fable-5-1'}];
  window.chatSessions=[];window.chatAgentSessions={alfred:[{id:'a1',title:'Plan the launch'},{id:'a2',title:'Fix the build'},{id:'a3',title:'Draft the memo'}]};
  window.chatTermEnabled=false;window.chatTermKinds={};window.chatTermList=()=>[];window.chatTaskThreads=[];window.chatTaskEntry=t=>({taskThread:true,session:t});
  window.chatEntryLifecycle=()=>'active';
  window.execs={a1:'running',a2:'running',a3:'failed'};
  window.chatEntryState=e=>({execution:execs[e.session.id],label:{running:'Working',waiting_user:'Needs input',failed:'Run failed',completed:'Run finished'}[execs[e.session.id]]||''});
  window.chatIsTerm=n=>false;window.chatPrivateCreationAgent=a=>a;window.chatAgentLabel=a=>a==='alfred'?'Alfred':a;
  window.shortModel=m=>m;window.chatLoadInbox=async()=>{};window.showToast=()=>{};
  window.notes=[];window.Notification=class{static permission='granted';static async requestPermission(){Notification.permission='granted';return 'granted';}constructor(title,o){notes.push([title,o.body]);}close(){}};
  window.reviewDialog=()=>{};`;
 const page_html=`<!doctype html><html><head><meta charset="utf-8"><style>${css}</style></head><body>
  <section class="chat-page" id="chatView"><div class="chat-shell"><div class="chat-main"></div></div></section>
  <script>${stubs}</script><script>${read('js/47-chat-state.js')}</script><script>${read('js/50-chat-tiles.js')}</script></body></html>`;
 const frame_html=`<!doctype html><html><head><meta charset="utf-8"></head><body><h3 class="chat-head-title"></h3></body></html>`;
 await context.route('http://tiles.test/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname==='/api/chat/state/inbox/tiles'){
   if(route.request().method()==='PUT'){const body=JSON.parse(route.request().postData());state={...state,revision:state.revision+1,value:body.value};}
   return route.fulfill({contentType:'application/json',body:JSON.stringify(state)});
  }
  if(url.searchParams.get('chatPane')==='1')return route.fulfill({contentType:'text/html',body:frame_html});
  return route.fulfill({contentType:'text/html',body:page_html});
 });
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.goto('http://tiles.test/');await page.evaluate(()=>{location.hash='#/chat/tiles';return chatTilesShow('');});
 await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===2);
 const attn=page.locator('.chat-tiles-attn'),focused=()=>page.evaluate(()=>[chatTilesState.active,chatTilesFocused()]);
 const heads=()=>page.evaluate(()=>chatTilesHeads());
 // on load: the failed tile in workspace 2 is listed, not announced
 await heads();
 assert.equal(await attn.textContent(),'1 error · Next');
 assert.deepEqual(await page.evaluate(()=>notes),[],'a state found on load is not announced');
 // changes: a2 (visible, not focused) now needs you; a1 (focused, in view) finishes
 await page.evaluate(()=>{execs.a2='waiting_user';execs.a1='completed';});await heads();
 assert.equal(await attn.textContent(),'1 needs you · 1 error · Next');
 assert.deepEqual(await page.evaluate(()=>notes),[['Fix the build','Needs you']],'only the tile out of view is announced; the one in view is not "done"');
 // Alt+N: needs you first
 await page.keyboard.press('Alt+n');assert.deepEqual(await focused(),['1','t2']);
 // then the error, in the other workspace
 await page.keyboard.press('Alt+n');assert.deepEqual(await focused(),['2','t3']);
 // a run that finishes while its tile is out of view is done, and announced
 await page.evaluate(()=>{execs.a2='running';});await heads();
 await page.evaluate(()=>{execs.a2='completed';execs.a3='running';});await heads();
 assert.equal(await attn.textContent(),'1 done · Next');
 assert.deepEqual((await page.evaluate(()=>notes)).at(-1),['Fix the build','Finished']);
 // focusing it clears "done"
 await page.keyboard.press('Alt+n');assert.deepEqual(await focused(),['1','t2']);
 assert.equal(await attn.isHidden(),true,'focusing the finished tile clears it');
 await page.keyboard.press('Alt+n');
 assert.match(await page.locator('.chat-tiles-note').textContent(),/Nothing needs you/);
 // notifications only when allowed: "Notify me" shows while undecided and asks on press
 await page.evaluate(()=>{Notification.permission='default';chatTilesAttentionPaint();});
 const notify=page.getByRole('button',{name:'Notify me'});await notify.waitFor();
 await page.evaluate(()=>{execs.a1='running';});await heads();await page.evaluate(()=>{execs.a1='failed';});await page.keyboard.press('Alt+2');await heads();
 assert.equal((await page.evaluate(()=>notes)).length,2,'nothing is announced without permission');
 await notify.click();await notify.waitFor({state:'hidden'});
 assert.deepEqual(errors,[]);
 console.log('PASS: attention across tiles — needs you / error / done, Alt+N order across workspaces, done cleared on focus, notifications only when allowed and never for load state.');
 await context.close();
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
