// chat-context-relationships.cjs — the real front end (index.html + every
// script) over a stub API, covering the relationships row's UI (2026-09-25):
//   1. `[[` in a private composer searches records across kinds through the
//      owner-only records API, keeps unavailable kinds visible, and choosing a
//      row opens that exact record's reviewed snapshot in Records — nothing is
//      inserted into the message, nothing is retained, nothing is sent;
//   2. explicit "use in this private chat" retains the reviewed version once
//      and the composer shows the selected chip;
//   3. Context › Adapter capabilities › Skills on disk reads the inventory
//      once when opened, restores its disclosure, and rereads on request;
//   4. a shared conversation never shows the typeahead and never queries the
//      records API; the popup fits a 390px phone width.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/chat-context-relationships.cjs
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const caps=inv=>({adapter:'hermes-oneshot',queue:'durable',cancelQueued:true,interrupt:'request-and-cancel-queued',stop:'request',steer:'unsupported',liveSteering:false,retry:'explicit-resubmit',resume:'fresh-session-per-turn',structuredQuestions:false,answerQuestions:'unsupported',supervision:'delivery-receipt',skillInventory:inv});
const session=(id,extra)=>({id,title:'Thread '+id.toUpperCase(),status:'idle',agent:'alfred',turns:2,updated:'2026-09-25T10:01:00Z',created:'2026-09-25T10:00:00Z',spentUsd:0,deliveries:[],...extra});
const thread=id=>({session:session(id,id==='b'?{shared:true,sharing:{state:'shared',agent:'kairos',thread:'shared-x'}}:{}),
 body:'## Turn 1 — user · 2026-09-25T10:00:00Z\n\nhello from '+id.toUpperCase()+'\n\n## Turn 2 — alfred · 2026-09-25T10:01:00Z\n\nreply from '+id.toUpperCase(),
 conversation:{key:'conv-'+id},capabilities:caps('on-disk'),outputs:[],queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]});
const records=[{kind:'task',id:'inbox/first',title:'Same title',detail:'Inbox · inbox/first',route:'#/tasks/inbox%2Ffirst'},{kind:'task',id:'work/second',title:'Same title',detail:'Work · work/second',route:'#/tasks/work%2Fsecond'},{kind:'person',id:'alice',title:'Same Titleholder',detail:'alice',route:'#/contacts/alice'}];
const revision='a'.repeat(64);
const log=[];let retained=0,skillReads=0,messages=0;
const json=(res,code,body)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://x');const p=url.pathname;
 if(p.startsWith('/api/')){
  log.push({p,q:url.search,m:req.method});
  if(p==='/api/agents/chat/roster')return json(res,200,{agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]});
  if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:[thread('a').session,thread('b').session]});
  const m=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])$/);if(m)return json(res,200,thread(m[1]));
  if(p==='/api/agents/chat/alfred/sessions/a/skills'){skillReads++;return json(res,200,{adapter:'hermes-oneshot',source:'on-disk',note:'Skills present on disk for this Hermes profile at read time.',limit:500,roots:[{label:'Hermes default profile skills',path:'/home/owner/.hermes/skills',available:true,count:2},{label:'Missing folder',path:'/home/owner/.hermes/profiles/x/skills',available:false,error:'folder does not exist',count:0}],skills:[{name:'email-inbox-triage',description:'Triage an inbox: prioritize threads.',root:'Hermes default profile skills',path:'email/triage/SKILL.md'},{name:'plain',root:'Hermes default profile skills',path:'plain/SKILL.md'}]});}
  if(p.endsWith('/messages')&&req.method==='POST'){messages++;return json(res,200,{ok:true});}
  if(p==='/api/chat/records'){const q=(url.searchParams.get('q')||'').toLowerCase();return json(res,200,{records:records.filter(r=>(r.title+' '+r.detail).toLowerCase().includes(q)),limit:50,unavailable:[{kind:'goal',error:'goal records unavailable'}]});}
  if(p==='/api/chat/records/preview'){const r=records.find(r=>r.id===url.searchParams.get('id')&&r.kind===url.searchParams.get('kind'));return r?json(res,200,{record:r,content:'# '+r.title+'\n\nRecord type: '+r.kind+'\nRecord ID: '+r.id+'\nPriority: high\nDepends on: work/second\n',revision}):json(res,400,{error:'record no longer available'});}
  if(p==='/api/chat/records/retain'){retained++;return json(res,200,{id:'ctx-artifact',revision,title:'Same title · inbox/first',version:1,explicitArtifacts:true});}
  if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
  if(p==='/api/terminal/sessions')return json(res,200,{sessions:[],enabled:true});
  if(p==='/api/chat/review-status')return json(res,200,{by_scope:{},by_task:{}});
  const st=p.match(/^\/api\/chat\/state\/([^/]+)\/([^/]+)$/);if(st){if(req.method==='PUT'){let b='';req.on('data',c=>b+=c);req.on('end',()=>{const v=JSON.parse(b||'{}');json(res,200,{key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:(v.revision||0)+1,value:v.value});});return;}return json(res,200,{key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:0,value:null});}
  return json(res,404,{});
 }
 if(p==='/__log')return json(res,200,{log,retained,skillReads,messages});
 const file=path.join(web,p==='/'?'index.html':p);
 if(!file.startsWith(web)||!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.writeHead(404);return res.end();}
 res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const state=async()=>(await fetch(base+'/__log')).json();
 const recordQueries=async()=>(await state()).log.filter(r=>r.p==='/api/chat/records').length;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const page=await browser.newPage({viewport:{width:1200,height:900}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/a');
  await page.locator('#chatTranscript').getByText('reply from A').waitFor();
  const ta=page.locator('#chatComposer textarea'),popup=page.locator('.chat-record-mention');
  // 1. [[ searches across kinds; equal titles stay separate exact IDs.
  await ta.click();await ta.type('Look at [[same');
  await popup.waitFor({state:'visible'});
  const rows=popup.locator('[role=option]');
  assert.equal(await rows.count(),3,'three matching records');
  assert.equal(await rows.nth(0).textContent(),'Same titletask · Inbox · inbox/first');
  assert.equal(await rows.nth(1).textContent(),'Same titletask · Work · work/second');
  assert.equal(await popup.locator('.chat-record-mention-note').textContent(),'Not searched: goal','unavailable kinds stay visible');
  assert.equal(await rows.nth(0).getAttribute('aria-selected'),'true');
  await ta.press('ArrowDown');assert.equal(await rows.nth(1).getAttribute('aria-selected'),'true');
  await ta.press('ArrowUp');assert.equal(await rows.nth(0).getAttribute('aria-selected'),'true');
  const before=await state();
  await ta.press('Enter');
  await popup.waitFor({state:'hidden'});
  assert.equal(await ta.inputValue(),'Look at ','the trigger text leaves the message; no reference is inserted');
  await page.getByRole('tab',{name:'Records',exact:true}).waitFor();
  const preview=page.locator('.chat-notes-preview');
  await preview.getByRole('heading',{name:'Same title'}).waitFor();
  assert.equal(await preview.locator('.chat-workspace-hint').first().textContent(),'task · inbox/first','the exact record, not a title match');
  assert.match(await preview.locator('pre').textContent(),/Depends on: work\/second/);
  assert.equal(await page.getByLabel('Record kind').inputValue(),'task');
  const after=await state();
  assert.equal(after.messages,before.messages,'Enter on a suggestion sends nothing');
  assert.equal(after.retained,0,'choosing a suggestion retains nothing');
  assert.ok(after.log.some(r=>r.p==='/api/chat/records/preview'&&r.q.includes('kind=task')&&r.q.includes('id=inbox%2Ffirst')),'preview requested by exact kind and ID');
  // 2. Explicit selection retains once and shows the chip.
  await preview.getByRole('button',{name:'use in this private chat'}).click();
  await page.locator('.chat-artifact-context').getByText('Same title · inbox/first').waitFor();
  assert.equal((await state()).retained,1,'retained exactly once');
  // Escape closes; an empty result names the gap.
  await ta.click();await ta.press('End');await ta.type('[[zzz');
  await popup.waitFor({state:'visible'});
  assert.equal(await popup.locator('.chat-record-mention-note').textContent(),'No matching records · not searched: goal');
  await ta.press('Escape');await popup.waitFor({state:'hidden'});
  assert.equal(await ta.inputValue(),'Look at [[zzz','Escape keeps the typed text');
  await ta.fill('');
  // 3. Skills on disk in Context.
  await page.evaluate(()=>{chatOpenContext();chatEnsureWorkspace().show(true);});
  await page.getByText('Adapter capabilities',{exact:true}).click();
  assert.equal(await page.locator('.chat-context-capabilities').getByText('Not reported by this adapter').count(),0,'an on-disk inventory is not labelled not reported');
  assert.equal((await state()).skillReads,0,'nothing is read before the disclosure opens');
  await page.getByText('Skills on disk',{exact:true}).click();
  const skills=page.locator('.chat-context-skills');
  await skills.getByText('email-inbox-triage').waitFor();
  assert.match(await skills.textContent(),/Triage an inbox: prioritize threads\./);
  assert.match(await skills.textContent(),/\/home\/owner\/\.hermes\/skills · 2 skills/);
  assert.match(await skills.textContent(),/folder does not exist/,'an unreadable root stays visible');
  assert.match(await skills.textContent(),/loads a skill only when it names it/,'the listing is not runtime telemetry');
  assert.equal((await state()).skillReads,1);
  await page.getByText('Skills on disk',{exact:true}).click();await page.getByText('Skills on disk',{exact:true}).click();
  await skills.getByText('email-inbox-triage').waitFor();
  assert.equal((await state()).skillReads,1,'reopening does not reread');
  await skills.getByRole('button',{name:'read again'}).click();
  await page.waitForFunction(()=>fetch('/__log').then(r=>r.json()).then(s=>s.skillReads===2));
  const view=await page.evaluate(()=>chatWorkspaceTabs.entries.get('context').api.getView());
  assert.equal(view.skillsOpen,true,'the skills disclosure is part of the saved view');
  await page.evaluate(()=>chatWorkspaceTabs.entries.get('context').api.restoreView({skillsOpen:false,capabilitiesOpen:true}));
  assert.equal(await page.locator('.chat-context-skills').evaluate(e=>e.open),false,'restoreView applies the disclosure');
  // 4. A shared conversation never offers the typeahead or queries records.
  const queriesBefore=await recordQueries();
  await page.goto(base+'/#/chat/a/alfred/b');
  await page.locator('#chatTranscript').getByText('reply from B').waitFor();
  // The shared composer waits for a recipient choice, so drive the textarea
  // through the same input event the keyboard raises.
  assert.equal(await page.evaluate(()=>chatCanSelectNoteContext()),false,'the private-records gate is closed for a shared conversation');
  await page.evaluate(()=>{const t=document.querySelector('#chatComposer textarea');t.focus();t.value='[[same';t.selectionStart=t.selectionEnd=t.value.length;t.dispatchEvent(new Event('input',{bubbles:true}));});
  await page.waitForTimeout(500);
  assert.equal(await page.locator('.chat-record-mention').isVisible(),false,'no typeahead in a shared conversation');
  assert.equal(await recordQueries(),queriesBefore,'shared conversation never queries private records');
  assert.equal(await page.locator('#chatComposer textarea').inputValue(),'[[same');
  // Phone width: the popup fits. The workspace opened above would cover a
  // phone's composer, so close it first.
  await page.evaluate(()=>{window.chatWorkspace?.close?.();});
  await page.setViewportSize({width:390,height:844});
  await page.goto(base+'/#/chat/a/alfred/a');
  await page.locator('#chatTranscript').getByText('reply from A').waitFor();
  // A restored open workspace covers the phone composer; hide it as the owner would.
  await page.evaluate(()=>{if(document.querySelector('.chat-shell.has-artifact'))chatEnsureWorkspace().show(false);});
  await page.locator('#chatComposer textarea').waitFor({state:'visible'});
  const tp=page.locator('#chatComposer textarea');await tp.click();await tp.type('[[same');
  await page.locator('.chat-record-mention').waitFor({state:'visible'});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'no horizontal overflow at 390px');
  const box=await page.locator('.chat-record-mention').boundingBox();
  assert.ok(box&&box.x>=0&&box.x+box.width<=390.5,'popup within the phone viewport: '+JSON.stringify(box));
  await page.screenshot({path:'/tmp/manifest-context-relationships-phone.png'});
  assert.deepEqual(errors,[]);
  console.log('PASS: [[ record typeahead (exact IDs, unavailable kinds, review-before-select, nothing sent or retained), explicit retention chip, Skills on disk read/restore/reread, shared conversation gate, 390px fit.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1);});
