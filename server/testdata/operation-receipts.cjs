// Owner decision D1: one settled approval receipt across chat, task and Feed.
// The task section and Feed's settled lane render the server receipt through
// operationReceiptEl; chat renders the record through manifestOperationCard.
// All three carry the same data-operation-id, receipts offer no decision,
// a rejection reads as declined, an unreadable lane says so, and the lane
// keeps its fold across the 3 s repaint. 390px and 1440px, both themes.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const js=f=>fs.readFileSync(path.join(__dirname,'../web/js',f),'utf8');
const between=(src,a,b)=>{const i=src.indexOf(a);assert.ok(i>=0,'missing '+a);const j=b?src.indexOf(b,i):src.length;assert.ok(j>i,'missing '+b);return src.slice(i,j);};
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 const page=await browser.newPage({viewport:{width:1440,height:900}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());
 await page.setContent('<main id="root" style="padding:16px"><section id="chat"></section><section id="task"></section><section id="feed"></section></main>');
 const cssDir=path.join(__dirname,'../web/css');await page.addStyleTag({content:fs.readdirSync(cssDir).filter(f=>f.endsWith('.css')).sort().map(f=>fs.readFileSync(path.join(cssDir,f),'utf8')).join('\n')});
 await page.evaluate(()=>{window.fmtWhen=x=>'at '+x;window.renderMarkdown=text=>{const p=document.createElement('p');p.textContent=text;return p;};window.loadFeed=()=>{window.reloaded=(window.reloaded||0)+1;};});
 const components=js('05-components.js');
 await page.addScriptTag({content:between(components,'function el(','function statusDot(')});
 await page.addScriptTag({content:between(js('55-approvals.js'),'// operationReceiptEl renders','let manifestRevealTarget')});
 await page.addScriptTag({content:between(js('93-todo-panel.js'),'function appendTaskApprovals(')});
 await page.addScriptTag({content:'let feedCache={settled:null};'+between(js('45-feed.js'),'let feedSettledOpen','// ---- the §5 card registry')+between(js('45-feed.js'),'// appendFeedSettled paints','// signalRow renders one app-signal')});
 const receipts=[
  {operationId:'sha256:'+'a'.repeat(64),proposalId:'operation-'+'a'.repeat(64),tool:'email.prepare',action:'email',status:'succeeded',conversation:'20260927-000000-0001',decidedBy:'owner:local',decidedAt:'2026-09-27T01:00:00Z',settledAt:'2026-09-27T01:00:02Z',result:{threadId:'thread-fixture'}},
  {operationId:'sha256:'+'b'.repeat(64),proposalId:'operation-'+'b'.repeat(64),tool:'email.prepare',action:'email',status:'rejected',conversation:'20260927-000000-0001',decidedBy:'owner:local',decidedAt:'2026-09-27T01:05:00Z',settledAt:'2026-09-27T01:05:00Z'},
 ];
 await page.evaluate(receipts=>{
  const record=rc=>({operationId:rc.operationId,status:rc.status,policy:'human_approval',arguments:{email:{from:'ben@ooda.group',to:['fixture@example.com'],subject:'Quote',body:'Please quote.'}},result:rc.result||{}});
  for(const rc of receipts)document.getElementById('chat').append(manifestOperationCard({record:record(rc),proposal:{id:rc.proposalId,action:'email',body:''},receipt:rc}));
  appendTaskApprovals(document.getElementById('task'),{proposals:[],receipts});
  feedCache.settled={receipts,total:2};appendFeedSettled(document.getElementById('feed'));
 },receipts);
 const ids=sel=>page.locator(sel).evaluateAll(es=>es.map(e=>e.dataset.operationId));
 const chatIDs=await ids('#chat [data-operation-id]'),taskIDs=await ids('#task [data-operation-id]'),feedIDs=await ids('#feed [data-operation-id]');
 assert.deepEqual(chatIDs,receipts.map(r=>r.operationId));assert.deepEqual(taskIDs,chatIDs,'task shows the chat receipts');assert.deepEqual(feedIDs,chatIDs,'Feed shows the chat receipts');
 assert.equal(await page.locator('#task .micro-label').first().textContent(),'Settled approvals · also in chat and Feed');
 // read-only: no decision control on a receipt, on either surface
 for(const host of ['#task','#feed'])assert.equal(await page.locator(host+' button',{hasText:/approve|reject|confirm/i}).count(),0,'no decision on '+host);
 const task=page.locator('#task .operation-receipt');
 assert.match(await task.nth(0).innerText(),/Email · done[\s\S]*approved by owner:local · \S+[\s\S]*settled /);
 assert.match(await task.nth(1).innerText(),/Email · rejected[\s\S]*declined by owner:local/);
 await task.nth(0).locator('summary').click();
 assert.deepEqual(JSON.parse(await task.nth(0).locator('.operation-receipt-exact pre').innerText()),receipts[0],'the exact receipt is the server receipt');
 await task.nth(0).getByRole('button',{name:'open conversation'}).click();assert.equal(await page.evaluate(()=>location.hash),'#/chat/20260927-000000-0001');
 // Feed: folded by default, counted, and the fold survives a repaint that
 // lands before the toggle event
 const lane=page.locator('#feed .feed-settled');
 assert.equal(await lane.evaluate(e=>e.open),false);assert.equal(await lane.locator(':scope > summary').textContent(),'Settled approvals · 2');
 await page.evaluate(()=>{document.querySelector('#feed .feed-settled > summary').click();const f=document.getElementById('feed');f.replaceChildren();appendFeedSettled(f);});
 assert.equal(await page.locator('#feed .feed-settled').evaluate(e=>e.open),true,'fold survives a repaint');
 // unknown renders as unknown: an unreadable lane is not "none"
 await page.evaluate(()=>{const f=document.getElementById('feed');f.replaceChildren();feedCache.settled={error:'operation receipts unavailable'};appendFeedSettled(f);});
 assert.equal(await page.locator('#feed .feed-settled > summary').textContent(),'Settled approvals · unavailable');
 assert.match(await page.locator('#feed .feed-settled').innerText(),/couldn't be read: operation receipts unavailable/);
 await page.locator('#feed .feed-settled').getByRole('button',{name:'retry'}).click();assert.equal(await page.evaluate(()=>window.reloaded),1);
 await page.evaluate(()=>{const f=document.getElementById('feed');f.replaceChildren();feedCache.settled={receipts:[],total:0};appendFeedSettled(f);});
 assert.match(await page.locator('#feed .feed-settled').innerText(),/No settled approvals yet\./);
 await page.evaluate(()=>{const f=document.getElementById('feed');f.replaceChildren();feedCache.settled=null;appendFeedSettled(f);});
 assert.equal(await page.locator('#feed .feed-settled').count(),0,'not loaded paints nothing rather than a claim');
 for(const theme of ['','jarvis'])for(const width of [390,1440]){
  await page.evaluate(theme=>{if(theme)document.documentElement.dataset.theme=theme;else delete document.documentElement.dataset.theme;},theme);
  await page.setViewportSize({width,height:900});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'overflow '+width+' '+(theme||'default'));
 }
 assert.deepEqual(errors,[]);
 console.log('PASS: one receipt identity across chat, task and Feed; read-only; declined wording; unreadable lane named; fold survives repaint; 390/1440 both themes.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1);});
