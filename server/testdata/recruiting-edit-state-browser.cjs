// Recruiting edits stay saved on screen: a blur-save's repaint keeps the next
// field's words and focus, a live read that raced a write cannot paint the
// pre-write record back, a refused change snaps back to the record, and the
// place editor keeps its draft across its own repaint.
const {chromium}=require('playwright'),fs=require('fs'),assert=require('node:assert/strict');
const root=require('path').join(__dirname,'../web');
(async()=>{const browser=await chromium.launch({headless:true});try{
const page=await browser.newPage({viewport:{width:1280,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
await page.setContent('<div id="aionView"><div id="aionBody"></div></div>');
await page.addScriptTag({content:fs.readFileSync(root+'/js/05-components.js','utf8')});
await page.addScriptTag({content:`
function fmtWhen(){return '1 Oct';}
function showToast(m,_,kind){(window.toasts=window.toasts||[]).push([kind||'ok',m]);}
var els={aionView:document.getElementById('aionView'),aionBody:document.getElementById('aionBody'),aionMeta:null};
var aionMode='recruiting',aionCache={};
// the server: a view per write, delayed so the owner can type meanwhile
window.server={delay:150,refuse:false,posts:[]};
const clone=o=>JSON.parse(JSON.stringify(o));
async function fetchJSONRetry(method,url,body){
  server.posts.push({method,url,body});
  await new Promise(r=>setTimeout(r,server.delay));
  if(server.refuse)return {ok:false,text:async()=>'refused by test',json:async()=>({})};
  const v=clone(server.view),id=url.split('/candidate/')[1]?.replace(/^[a-z]+\\//,'');
  const c=v.candidates.find(x=>x.id===id);
  if(c&&url.includes('/update/'))Object.entries(body).forEach(([k,val])=>{if(k==='name')c.name=val;else c.profile[k]=val;});
  if(c&&url.includes('/stage/'))c.stage=body.stage;
  server.view=v;
  return {ok:true,text:async()=>'',json:async()=>clone(v)};
}
// GET /api/aion/recruiting for the live poll: answers whatever snapshot the
// test staged, after the staged delay
window.pollAnswer=null;window.pollDelay=0;
window.fetch=async(url)=>{await new Promise(r=>setTimeout(r,pollDelay));return {ok:true,json:async()=>clone(pollAnswer||server.view),text:async()=>''};};
// 92-aion.js renderAion, the recruiting branch, verbatim in shape
function renderAion(){const recruitingFocus=document.activeElement?.dataset?.recNav||'';
  const recruitingEdit=aionMode==='recruiting'&&typeof recEditSnapshot==='function'?recEditSnapshot():null;
  els.aionBody.innerHTML='';renderAionRecruiting(els.aionBody,recruitingFocus,recruitingEdit);}
`});
await page.addScriptTag({content:fs.readFileSync(root+'/js/96-aion-recruiting.js','utf8')});
await page.evaluate(()=>{
  const role={id:'role/eng',slug:'eng',title:'Engineer',criteria:[]};
  const cand=(id,name,stage)=>({id,name,role:'role/eng',stage,profile:{title:'Old title',org:'',location:''},evidence:[],fit:[],paths:[],next:[],outreach:[]});
  server.view={roles:[role],stages:['new','reviewing','shortlist','intro','outreach','replied','ashby','archived'],
    candidates:[cand('cand/ada','Ada','reviewing'),cand('cand/bea','Bea','archived')],
    seeds:[{id:'seed/lab-x',class:'lab',name:'X Lab',display:'X Lab',url:''}],seedClasses:['lab','company','media','person'],network:{people:[],edges:[]}};
  recCache=JSON.parse(JSON.stringify(server.view));recSources={unavailable:true};recRuns=[];
  recOutreachProbe={unavailable:true};recAshbyProbe={configured:false};recOutreachLog={id:'cand/ada',entries:[]};
  recView='board';recOriginSet=true;recOrigin='both';recCut='all';recSel='cand/ada';
  renderAion();
});
// 1. blur-save, then typing in the next field while the save is out
await page.locator('.rec-advanced summary').click();
const title=page.locator('[data-rec-edit="cand/ada#title"]'),org=page.locator('[data-rec-edit="cand/ada#org"]');
await title.fill('New title');await title.press('Tab');
await page.keyboard.type('Acme Rob',{delay:10});
await page.waitForFunction(()=>server.posts.length===1&&recCache.candidates[0].profile.title==='New title');
await page.keyboard.type('otics');
assert.equal(await org.inputValue(),'Acme Robotics','the next field kept every word typed across the save repaint');
assert.equal(await org.evaluate(e=>e===document.activeElement),true,'and kept focus');
assert.equal(await page.locator('.rec-advanced').evaluate(d=>d.open),true,'Edit profile stays open after a save');
assert.equal(await title.inputValue(),'New title');
await org.press('Tab');
await page.waitForFunction(()=>recCache.candidates[0].profile.org==='Acme Robotics');
assert.deepEqual((await page.evaluate(()=>server.posts.map(p=>p.body))),[{title:'New title'},{org:'Acme Robotics'}]);

// 2. a live read that started before a stage save must not paint it back
await page.evaluate(async()=>{
  server.posts=[];pollAnswer=JSON.parse(JSON.stringify(server.view));pollDelay=300;recLastLiveRead=0;
  document.activeElement.blur();
  const poll=recPollLive();
  await new Promise(r=>setTimeout(r,20));
  const stage=document.querySelector('[aria-label="Manifest stage"]');
  stage.value='shortlist';stage.onchange();
  await poll;await new Promise(r=>setTimeout(r,400));
});
assert.equal(await page.evaluate(()=>recCache.candidates[0].stage),'shortlist','the racing poll snapshot was dropped');
assert.equal(await page.locator('[aria-label="Manifest stage"]').inputValue(),'shortlist');

// 3. a refused stage change snaps back to the record, with the error said
await page.evaluate(async()=>{server.refuse=true;toasts=[];
  const stage=document.querySelector('[aria-label="Manifest stage"]');stage.value='intro';stage.onchange();
  await new Promise(r=>setTimeout(r,300));server.refuse=false;});
assert.equal(await page.locator('[aria-label="Manifest stage"]').inputValue(),'shortlist','the refused stage is not left on screen');
assert.equal(await page.evaluate(()=>toasts.some(t=>t[0]==='error')),true);

// 4. an archived record's stage reads as archived, not as the first stage
await page.evaluate(()=>{recSel='cand/bea';recPaint();});
assert.equal(await page.locator('[aria-label="Manifest stage"]').inputValue(),'archived');

// 5. the place editor keeps its draft when picking a class repaints it
await page.evaluate(()=>{recView='places';recPlacesLayout='places';recPlaceEdit='seed/lab-x';recSel=null;renderAion();});
const pname=page.locator('[data-rec-edit="seed/lab-x#place#name"]');
await pname.fill('X Lab (Rome)');
await page.locator('.rec-scaffold-classes .filter-chip',{hasText:'company'}).click();
assert.equal(await page.locator('.rec-scaffold-classes .filter-chip.on').textContent(),'company','the picked class shows');
assert.equal(await pname.inputValue(),'X Lab (Rome)','the typed name survived the repaint');
assert.deepEqual(errors,[]);
console.log('PASS blur-save keeps the next field, racing poll dropped, refused change reverts, archived stage shown, place draft kept.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
