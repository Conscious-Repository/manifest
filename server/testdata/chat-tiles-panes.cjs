// Tiles and polling: every tile is a whole copy of the app in a frame, and
// document.hidden inside a frame follows the top page, not the tile. Over the
// real front end and the chat stub this proves the tile manager's contract
// with its frames (48-chat.js chatPaneShown/chatPaneFocused, 50-chat-tiles.js
// chatTilesNotifyPanes):
//   - a tile in a hidden workspace makes no requests for its conversation;
//   - the focused tile polls at the single-chat cadence, an unfocused one at
//     the manager's own (its event stream still pushes live turns);
//   - showing a hidden tile reads its conversation at once, so a change made
//     while it was hidden is on screen right away, never an old view shown as
//     current; focusing an unfocused tile does the same.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();
 const turn=(n,who,text)=>'## Turn '+n+' — '+who+' · 2026-09-25T08:0'+n+':00Z\n\n'+text;
 stub.sessions.e={id:'e',title:'Hidden thread',status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T08:00:00Z',created:'2026-09-25T08:00:00Z',spentUsd:0,deliveries:[]};
 stub.bodies.e=turn(1,'user','Question for e?')+'\n\n'+turn(2,'alfred','Answer for e.');
 const leaf=id=>({t:'leaf',id});
 stub.state.set('inbox/tiles',{key:'inbox',slot:'tiles',revision:1,value:{v:1,active:'2',focus:{'1':'t1','2':'t5'},full:{},
  ws:{'1':{t:'split',dir:'row',ratio:0.5,a:leaf('t1'),b:leaf('t2')},'2':leaf('t5')},
  tiles:{t1:{route:'#/chat/a/alfred/a',title:''},t2:{route:'#/chat/a/alfred/b',title:''},t5:{route:'#/chat/a/alfred/e',title:''}}}});
 await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const reads=(id,from)=>stub.log.slice(from).filter(l=>l==='GET /api/agents/chat/alfred/sessions/'+id).length;
 const frameText=(page,route)=>page.evaluate(route=>{const f=[...document.querySelectorAll('.chat-tile-frame')].find(f=>f.contentWindow.location.hash===route);return f?.contentDocument?.getElementById('chatTranscript')?.textContent||'';},route);
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  // workspace 2 first (its frame mounts), then workspace 1: the e frame stays, hidden
  await page.goto(base+'/#/chat/tiles');
  await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===1);
  await page.waitForFunction(()=>/Answer for e/.test(document.querySelector('.chat-tile-frame').contentDocument?.getElementById('chatTranscript')?.textContent||''));
  await page.keyboard.press('Alt+1');
  await page.waitForFunction(()=>document.querySelectorAll('.chat-tile-frame').length===3);
  await page.waitForFunction(()=>[...document.querySelectorAll('.chat-tile-frame')].filter(f=>f.getClientRects().length).every(f=>f.contentDocument?.querySelector('#chatTranscript [data-chat-read-turn]')));
  await page.waitForTimeout(1500);
  // 1. idle: hidden silent, focused at 1.5 s, unfocused at 8 s
  let mark=stub.log.length;
  await page.waitForTimeout(9000);
  assert.equal(reads('e',mark),0,'a tile in a hidden workspace polled its conversation');
  assert.ok(reads('a',mark)>=4,'the focused tile should poll at the single-chat cadence: '+reads('a',mark));
  assert.ok(reads('b',mark)<=2,'an unfocused tile should poll at the manager cadence: '+reads('b',mark));
  // 2. a change while hidden is read the moment the tile is shown
  await fetch(base+'/__set?id=e&append='+encodeURIComponent('News while hidden.'));
  assert.doesNotMatch(await frameText(page,'#/chat/a/alfred/e'),/News while hidden/);
  mark=stub.log.length;const shownAt=Date.now();
  await page.keyboard.press('Alt+2');
  await page.waitForFunction(()=>{const f=[...document.querySelectorAll('.chat-tile-frame')].find(f=>f.getClientRects().length);return /News while hidden/.test(f?.contentDocument?.getElementById('chatTranscript')?.textContent||'');},null,{timeout:1400});
  assert.ok(Date.now()-shownAt<1400,'the shown tile waited for a poll tick');
  assert.ok(reads('e',mark)>=1);
  // 3. focusing an unfocused tile reads it at once
  await page.keyboard.press('Alt+1');await page.waitForTimeout(300);
  await fetch(base+'/__set?id=b&append='+encodeURIComponent('Fresh on b.'));
  await page.keyboard.press('Alt+l');
  await page.waitForFunction(()=>{const f=[...document.querySelectorAll('.chat-tile-frame')].find(f=>f.contentWindow.location.hash==='#/chat/a/alfred/b');return /Fresh on b/.test(f?.contentDocument?.getElementById('chatTranscript')?.textContent||'');},null,{timeout:1400});
  assert.deepEqual(errors,[]);
  console.log('PASS: hidden tiles are silent, unfocused tiles poll gently, shown or focused tiles read at once.');
  await ctx.close();
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
