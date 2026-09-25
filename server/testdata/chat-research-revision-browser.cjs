// Full frontend research revision and lost-save-ack recovery at phone width.
const {chromium,devices}=require('playwright'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const LIST_DELAY=300,THREAD_DELAY=400;
const overrides={};
const thread=id=>overrides[id]||({session:{id,title:'Thread '+id.toUpperCase(),status:'idle',agent:'alfred',turns:2,updated:'2026-09-12T10:0'+(id==='a'?1:2)+':00Z',created:'2026-09-12T10:00:00Z',spentUsd:0},
 body:'## Turn 1 — user · 2026-09-12T10:00:00Z\n\nhello from '+id.toUpperCase()+'\n\n## Turn 2 — alfred · 2026-09-12T10:01:00Z\n\nreply from '+id.toUpperCase(),
 conversation:{key:'conv-'+id},queued:[],operations:[],proposals:[],related:[],continuations:[],sharedFiles:[]});
const log=[];
const json=(res,code,body,delay=0)=>setTimeout(()=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));},delay);
const server=http.createServer((req,res)=>{
 const url=new URL(req.url,'http://x');const p=url.pathname;
 if(p.startsWith('/api/')){
  log.push({p,at:Date.now()});
  if(p==='/api/agents/chat/roster')return json(res,200,{agents:[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'claude-x'}]});
  if(p==='/api/agents/chat/alfred/sessions')return json(res,200,{sessions:[thread('a').session,thread('b').session]},LIST_DELAY);
  const m=p.match(/^\/api\/agents\/chat\/alfred\/sessions\/([ab])$/);if(m)return json(res,200,thread(m[1]),THREAD_DELAY);
  if(p==='/api/agents/chat/alfred/sessions/missing')return json(res,404,{});
  if(p==='/api/chat/sessions')return json(res,200,{sessions:[]});
  if(p==='/api/terminal/sessions')return json(res,200,{sessions:[],enabled:true},LIST_DELAY);
  if(p==='/api/chat/review-status')return json(res,200,{by_scope:{},by_task:{}});
  const st=p.match(/^\/api\/chat\/state\/([^/]+)\/([^/]+)$/);if(st)return json(res,200,{key:decodeURIComponent(st[1]),slot:decodeURIComponent(st[2]),revision:0,value:null});
  return json(res,404,{});
 }
 if(p==='/__log')return json(res,200,log);
 const file=path.join(web,p==='/'?'index.html':p);
 if(!file.startsWith(web)||!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.writeHead(404);return res.end();}
 res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true});
 try {
  const context=await browser.newContext({...devices['iPhone 13']});
  const page=await context.newPage(),errors=[],states=new Map(),saves=[];
  page.on('pageerror',e=>errors.push(e.message));
  const id='fedcba9876543210',first='a'.repeat(64),second='b'.repeat(64);
  const original='# Research brief\n\nOriginal findings with a source.',revised='# Research brief\n\nVerified findings and limitations.';
  const artifact={id,title:'Research brief',ref:'research.md',head:first,content:original,preview:{kind:'text'},provenance:{source:'chat',session:'alfred/a'},revisions:[{n:1,hash:first,actor:'alfred',at:'2026-09-25T10:00:00Z'}]};
  let receipt=null,loseAck=true;
  const handleRoute=async route=>{
   const req=route.request(),url=new URL(req.url()),p=url.pathname;
   const state=p.match(/^\/api\/chat\/state\/([^/]+)\/([^/]+)$/);
   if(state){
    const key=decodeURIComponent(state[1]),slot=decodeURIComponent(state[2]);
    let snapshot=states.get(p)||{key,slot,revision:0,value:null};
    if(req.method()==='PUT'){
     const body=req.postDataJSON();
     if(body.revision!==snapshot.revision)return route.fulfill({status:409,json:snapshot});
     snapshot={...snapshot,revision:snapshot.revision+1,value:body.value};states.set(p,snapshot);
    }
    return route.fulfill({json:snapshot});
   }
   if(p==='/api/artifacts/get')return route.fulfill({json:{...artifact,content:url.searchParams.get('rev')===first?original:artifact.content}});
   if(p==='/api/artifacts/reviews')return route.fulfill({json:{revision:url.searchParams.get('revision'),entries:[],state:'not_requested',record_version:'0'}});
   if(p==='/api/artifacts/text'){
    const body=req.postDataJSON();saves.push(body);
    assert.equal(states.get('/api/chat/state/artifact-'+id+'/edit')?.value?.saveRequestID,body.requestID,'save identity is durable before file mutation');
    if(!receipt){
     assert.equal(body.expectedRevision,first);assert.equal(body.content,revised);
     artifact.head=second;artifact.content=body.content;artifact.revisions.push({n:2,hash:second,actor:'owner',at:'2026-09-25T11:00:00Z'});
     receipt={...artifact,savedRevision:second,savedVersion:2,saveRequestID:body.requestID};
    }else assert.deepEqual(body,saves[0],'retry preserves the original save request');
    if(loseAck){loseAck=false;return route.abort('failed');}
    return route.fulfill({json:receipt});
   }
   if(req.method()!=='GET')throw Error('Unexpected mutation: '+p);
   return route.continue();
  };
  await page.route('**/api/**',handleRoute);
  await page.goto('http://127.0.0.1:'+server.address().port+'/#/chat/a/alfred/a');
  await page.getByText('reply from A',{exact:true}).waitFor();
  // Enter through the same workspace action used by Files and result cards.
  await page.evaluate(id=>chatOpenWorkingArtifact({id,selectionKey:'chat:alfred/a'}),id);
  await page.getByRole('button',{name:'Edit',exact:true}).click();
  await page.getByLabel('File content').fill(revised);
  await page.getByRole('button',{name:'Review changes',exact:true}).click();
  await page.getByText('+ Verified findings and limitations.',{exact:true}).waitFor();
  await page.getByRole('button',{name:'Save new version',exact:true}).click();
  await page.getByText('Failed to fetch',{exact:true}).waitFor();
  assert.equal(saves.length,1);assert.equal(artifact.revisions.length,2);
  await page.reload();await page.getByText('reply from A',{exact:true}).waitFor({state:'attached'});
  // Reload can restore the comparison or editor; both retain the saved draft.
  const resume=page.getByRole('button',{name:'Resume draft',exact:true});
  await page.waitForFunction(()=>document.querySelector('[aria-label="File content"]')||Array.from(document.querySelectorAll('button')).some(e=>['Resume draft','Edit text'].includes(e.textContent)));
  const editText=page.getByRole('button',{name:'Edit text',exact:true});
  if(await editText.isVisible())await editText.click();
  if(await resume.isVisible())await resume.click();
  assert.equal(await page.getByLabel('File content').inputValue(),revised);
  await page.getByRole('button',{name:'Save new version',exact:true}).click();
  await page.getByText('Saved as a new artifact version.',{exact:false}).waitFor();
  assert.equal(saves.length,2);assert.equal(artifact.revisions.length,2);
  await page.getByRole('button',{name:'Compare v1',exact:true}).click();
  await page.getByText('+ Verified findings and limitations.',{exact:true}).waitFor();
  await page.getByRole('button',{name:'Back to preview',exact:true}).click();
  await page.getByRole('button',{name:'Discuss',exact:true}).click();
  await page.getByRole('button',{name:'Discussing: Research brief · v2',exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'Discussing: Research brief · v2',exact:true}).getAttribute('title'),'Exact revision '+second);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:'/tmp/manifest-research-revision-phone.png'});
  // Two isolated browser contexts share only the fixture's persisted records.
  // A newer remote draft cannot be silently overwritten by the older editor.
  await page.getByRole('button',{name:'Discussing: Research brief · v2',exact:true}).click();
  await page.getByRole('button',{name:'Edit',exact:true}).click();
  await page.evaluate(id=>artifactEditDrafts.get(id).flush(),id);
  const otherContext=await browser.newContext({viewport:{width:1200,height:900}});
  const other=await otherContext.newPage();other.on('pageerror',e=>errors.push(e.message));
  await other.route('**/api/**',handleRoute);
  await other.goto('http://127.0.0.1:'+server.address().port+'/#/chat/a/alfred/a');
  await other.getByText('reply from A',{exact:true}).waitFor();
  await other.evaluate(id=>chatOpenWorkingArtifact({id,selectionKey:'chat:alfred/a'}),id);
  await other.getByRole('button',{name:'Resume draft',exact:true}).click();
  const remoteText='Desktop research draft: retain the source caveat.',localText='Phone research draft: verify the sample size.';
  await other.getByLabel('File content').fill(remoteText);
  await other.evaluate(id=>artifactEditDrafts.get(id).flush(),id);
  await page.getByLabel('File content').fill(localText);
  await page.getByRole('button',{name:'Save new version',exact:true}).click();
  const conflict=page.locator('.artifact-edit-recovery');
  await conflict.getByRole('button',{name:'Keep this draft',exact:true}).waitFor();
  await conflict.getByText('View saved draft',{exact:true}).click();
  await conflict.getByText('View this device’s draft',{exact:true}).click();
  await conflict.getByText(remoteText,{exact:true}).waitFor();
  await conflict.getByText(localText,{exact:true}).waitFor();
  assert.equal(await page.getByLabel('File content').inputValue(),localText);
  assert.equal(saves.length,2,'unresolved draft conflict must prevent artifact save');
  assert.equal(artifact.content,revised,'draft conflict must not change the saved artifact');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:'/tmp/manifest-research-conflict-phone.png'});
  await conflict.getByRole('button',{name:'Keep this draft',exact:true}).click();
  await page.waitForFunction(id=>!artifactEditDrafts.get(id).dirty&&!artifactEditDrafts.get(id).conflict,id);
  assert.equal(states.get('/api/chat/state/artifact-'+id+'/edit').value.text,localText);
  // The older desktop editor can inspect and deliberately adopt that decision.
  await other.getByLabel('File content').fill(remoteText+' Additional desktop note.');
  await other.getByRole('button',{name:'Save new version',exact:true}).click();
  await other.getByRole('button',{name:'Use saved draft',exact:true}).click();
  assert.equal(await other.getByLabel('File content').inputValue(),localText);
  assert.equal(saves.length,2,'resolving either draft conflict must not save a file');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await otherContext.close();
  assert.deepEqual(errors,[]);
  console.log('PASS: phone research edit, lost-ack reload, exact retry/discussion and two-browser draft conflict resolution');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1);});
