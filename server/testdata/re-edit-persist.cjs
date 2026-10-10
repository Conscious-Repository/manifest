// re-edit-persist.cjs — property-page edits that showed on screen and then
// did not hold (owner report: "appears temporarily and then doesn't persist"),
// over a stateful stub of the property endpoints:
//   1. the task inspector's owner select names Olga for a line owned by her
//      ALIAS ("olga-sobkiv" — the option values are roster slugs, OS);
//   2. a waiting value saved from the inspector is still there after the
//      re-render, and it was written to the tree NODE id (the flat task id
//      404s on /work) — the restored panel used to blank it;
//   3. an est typed into the chip and then clicked away from is saved (blur
//      used to throw it away);
//   4. a list read taken before a save, landing after it, must not paint the
//      pre-save value back.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/re-edit-persist.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict'),http=require('node:http');
const {makeStub}=require('./writing-stub-api.cjs');
const node=(id,taskId,text,extra)=>({id,taskId,text,checked:false,est:0,estTotal:0,committed:0,paid:0,recognized:0,...extra});
const state={work:[{id:'rock-a',text:'Rough-in',checked:false,est:0,estTotal:0,tasks:[node('rock-a/call-inspector','real-estate/call-inspector','call the inspector',{owner:'olga-sobkiv'})]}]};
const prop=()=>({path:'p/753 bayard.md',slug:'753-bayard',name:'753-bayard',address:'753 Bayard Ave',short:'753 Bayard Ave',status:'construction',kind:'rehab',control:'owned',entity:'',deal:'',
 work:JSON.parse(JSON.stringify(state.work)),tasks:[{id:'real-estate/call-inspector',text:'call the inspector',checked:false,owner:state.work[0].tasks[0].owner}],ledger:[],log:[],project:{}});
const tasksMeta={me:'BA',outstanding:[],rows:[],counts:{},assignees:{realestate:[{slug:'BA',name:'benjamin anderson',trade:'partner'},{slug:'OS',name:'olga sobkiv',trade:'partner',aliases:['olga-sobkiv']},{slug:'m-w-services',name:'M&W Services'}]}};
(async()=>{
 const stub=makeStub({json:{'/api/tasks':tasksMeta,'/api/re/backlog':{items:[],goalsArea:null},'/api/realestate/contracts':{contracts:[]}}});
 const posts=[];let listDelay=null; // listDelay(n) → ms to hold the n-th /api/properties read
 let listReads=0;
 const server=http.createServer(async(req,res)=>{
  const url=new URL(req.url,'http://x');const send=(o)=>{res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(o));};
  if(req.method==='GET'&&url.pathname==='/api/properties'){
   const n=++listReads,snap={properties:[prop()],deals:[],templates:[],holdings:{}};
   const ms=listDelay?listDelay(n):0;return setTimeout(()=>send(snap),ms); // the body is read NOW; delivery may lag
  }
  if(req.method==='POST'&&url.pathname==='/api/properties/753-bayard/work'){
   let b='';for await(const c of req)b+=c;const body=JSON.parse(b);posts.push(body);
   const find=(id)=>{for(const st of state.work){if(st.id===id)return st;for(const t of st.tasks)if(t.id===id)return t;}return null;};
   const n=find(body.id);if(!n){res.writeHead(404);return res.end('work item not found');}
   if(body.op==='set-field'){if(body.field==='est'){n.est=Number(body.value)||0;n.estTotal=n.est;}else if(body.value)n[body.field]=body.value;else delete n[body.field];}
   return send(prop());
  }
  if(req.method==='POST'&&url.pathname==='/api/tasks/check'){
   let b='';for await(const c of req)b+=c;const body=JSON.parse(b);posts.push(body);state.work[0].tasks[0].checked=!!body.checked;return send({});
  }
  stub.server.emit('request',req,res);
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/properties/753-bayard');await page.waitForSelector('.go-task');
  // 1. the alias owner reads as Olga in the inspector
  await page.click('.go-task .go-task-text');await page.waitForSelector('.pp3-insp-slot select');
  assert.equal(await page.$eval('.pp3-insp-slot select',s=>s.options[s.selectedIndex]?.textContent),'olga sobkiv (partner)','alias owner shows as Olga');
  // 2. waiting survives the re-render, written to the node id
  const wIn=page.locator('.pp3-insp-slot input[placeholder^="who / what"]');
  await wIn.fill('city inspector');await wIn.press('Enter');
  await page.waitForFunction(()=>[...document.querySelectorAll('.pp3-insp-slot .pp3-insp-flabel')].some(l=>l.textContent==='since'));
  assert.equal(await wIn.inputValue(),'city inspector','the restored inspector keeps the saved waiting value');
  assert.ok(posts.filter(b=>b.field==='waiting').every(b=>b.id==='rock-a/call-inspector'),'waiting is written to the tree node id: '+JSON.stringify(posts));
  // 3. an est typed, then clicked away from, saves
  await page.click('.go-task .re-est-chip');await page.fill('.re-est-edit','1200');
  await page.click('.pp3-sec-title >> text=ROCKS');
  await page.waitForFunction(()=>[...document.querySelectorAll('.go-task .re-est-chip')].some(c=>c.textContent==='$1k'));
  assert.ok(posts.some(b=>b.field==='est'&&b.value==='1200'),'the blurred est was posted');
  // 4. out-of-order reads: the check's list read is held; an est saved
  // meanwhile answers with the fresh record — the late, older read (taken
  // before the est write) must not paint the old est back
  const before=listReads;
  listDelay=(n)=>n===before+1?700:0;
  await page.click('.go-task .go-check');           // check → renderProperties (read held)
  await page.waitForTimeout(100);
  await page.click('.go-task .re-est-chip');await page.fill('.re-est-edit','4000');await page.press('.re-est-edit','Enter');
  await page.waitForFunction(()=>[...document.querySelectorAll('.go-task .re-est-chip')].some(c=>c.textContent==='$4k'));
  await page.waitForTimeout(1000);                  // the held read lands
  assert.equal(await page.$eval('.go-task .re-est-chip',c=>c.textContent),'$4k','the older list read repainted the old est');
  listDelay=null;
  assert.deepEqual(errors,[]);
  console.log('PASS: property page edits hold — alias owner shown, waiting kept + written to the node, blurred est saved, stale list read dropped.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
