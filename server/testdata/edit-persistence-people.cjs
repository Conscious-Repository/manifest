// edit-persistence-people.cjs — edits in Fundraising and Network stay put
// (2026-10-10, the "shows then reverts" pass). On the real front end:
//   Fundraising inspector
//   - a blur-save's answer lands while the owner types in the next field: that
//     field keeps its focus and its typing (the rebuild used to drop both);
//   - a typed date saves ONCE, on blur, as the whole date (every keystroke
//     used to save — year 0002, 0020… — and throw the caret out);
//   - tabbing through an unchanged amount posts nothing;
//   Network page
//   - a tag saved after Network re-read its people (every return to it) shows
//     in the list and on the reopened page (it used to land on a stale copy);
//   - a typed last-contact date saves once, on blur;
//   Person page
//   - a typed, unsaved note survives the repaint a ticked loop causes;
//   - a refused note save says so and gives the button back (it said "note
//     saved" and stuck at "saving…").
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/edit-persistence-people.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict'),http=require('node:http');
const {makeStub}=require('./writing-stub-api.cjs');
const json=(res,body)=>{res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(body))};
const read=req=>new Promise(r=>{let b='';req.on('data',c=>b+=c);req.on('end',()=>r(JSON.parse(b||'{}')))});
(async()=>{
 const ops=[{id:'lux',firm:'Lux Capital',status:'prospect',people:[],unlinkedPeople:[],nextStep:'',notes:'',amount:0}];
 const person={id:'aion-net/ann',name:'Ann Lee',editable:true,sources:['kept'],tags:[],lastContact:''};
 const posts=[];
 const stub=makeStub({json:{'/api/aion/fundraising/sync':{enabled:false,conflicts:[]}}});
 const server=http.createServer(async(req,res)=>{
  const p=new URL(req.url,'http://x').pathname;
  if(p==='/api/aion/fundraising'&&req.method==='GET')return json(res,{opportunities:ops,resources:[]});
  if(p.startsWith('/api/aion/fundraising/update/')){
   const set=await read(req);posts.push({url:p,set});
   Object.assign(ops.find(o=>'/api/aion/fundraising/update/'+o.id===p),set);
   await new Promise(r=>setTimeout(r,350)); // the answer arrives while the owner is in the next field
   return json(res,{opportunities:JSON.parse(JSON.stringify(ops)),resources:[]});
  }
  if(p==='/api/network')return json(res,{people:[JSON.parse(JSON.stringify(person)),{id:'contact/bo',name:'Bo Diaz',contactKey:'bo',hasNote:true,sources:['contact']}],tags:[],editable:true});
  if(p==='/api/contacts/page')return json(res,{key:'bo',display:'Bo Diaz',hasNote:true,noteBody:'old note',loops:[{date:'2026-10-01',name:'sync',path:'sync.md',loops:[{kind:'checkbox',line:3,text:'send terms'}]}]});
  if(p==='/api/note/task')return json(res,{ok:true});
  if(p==='/api/contacts/note'){res.writeHead(500);return res.end('disk full');}
  if(p.startsWith('/api/network/person/')){
   const b=await read(req);posts.push({url:p,set:b.set});
   if('tags' in b.set)person.tags=b.set.tags.split(',').map(t=>t.trim().toLowerCase()).filter(Boolean);
   if('last_contact' in b.set)person.lastContact=b.set.last_contact;
   return json(res,{person:{id:person.id,tags:person.tags,lastContact:person.lastContact}});
  }
  stub.server.emit('request',req,res);
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  const page=await (await browser.newContext({viewport:{width:1440,height:900}})).newPage();
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/aion/fundraising');
  await page.click('.fr-table .fr-row:not(.fr-head)');
  await page.waitForSelector('[data-fr-field="nextStep"]');

  // 1 — type in "next step", move to "notes" and keep typing through the save
  await page.click('[data-fr-field="nextStep"]');await page.keyboard.type('send deck');
  await page.click('[data-fr-field="notes"]');await page.keyboard.type('met at JPM');
  await page.waitForTimeout(700); // the next-step answer has landed and repainted
  let s=await page.evaluate(()=>({focus:document.activeElement.dataset.frField,notes:document.querySelector('[data-fr-field="notes"]').value,next:document.querySelector('[data-fr-field="nextStep"]').value}));
  assert.equal(s.next,'send deck','the saved field shows what was saved');
  assert.equal(s.focus,'notes','the field the owner moved to keeps its focus');
  assert.equal(s.notes,'met at JPM','what was typed in it survives the repaint');
  await page.keyboard.type(' — warm');await page.click('.fr-toolbar .fr-search');await page.waitForTimeout(700);
  assert.equal(ops[0].notes,'met at JPM — warm','its blur saves the whole text');
  assert.equal(ops[0].nextStep,'send deck');

  // 2 — a typed date saves once, whole, on blur
  posts.length=0;
  await page.click('[data-fr-field="nextStepDue"]');await page.keyboard.type('10152026');
  await page.waitForTimeout(500);
  assert.equal(posts.length,0,'nothing saves while the date is being typed: '+JSON.stringify(posts));
  await page.keyboard.press('Enter');await page.waitForTimeout(700);
  assert.deepEqual(posts.map(x=>x.set),[{nextStepDue:'2026-10-15'}],'one save of the whole date');
  assert.equal(await page.inputValue('[data-fr-field="nextStepDue"]'),'2026-10-15');

  // 3 — tabbing through an unchanged amount posts nothing
  posts.length=0;
  await page.click('[data-fr-field="amount"]');await page.click('.fr-toolbar .fr-search');await page.waitForTimeout(500);
  assert.equal(posts.length,0,'an unchanged amount does not post');

  // 4 — Network: an edit after a re-read lands on the rows the list draws
  await page.goto(base+'/#/network/'+encodeURIComponent('aion-net/ann'));
  await page.waitForSelector('#netPage .net-field input[type=text]');
  await page.evaluate(()=>showNetwork()); // a return to Network: page from the old cache, then a re-read swaps it
  await page.waitForTimeout(300);
  posts.length=0;
  await page.fill('#netPage .net-field input[type=text]','MRI coils');
  await page.click('.net-search');await page.waitForTimeout(400);
  assert.equal(posts.length,1,'the tag saved');
  assert.match(await page.textContent('#netList .net-row'),/mri coils/,'the list shows the saved tag');
  await page.click('#netList .net-row');await page.waitForTimeout(200);
  assert.equal(await page.inputValue('#netPage .net-field input[type=text]'),'mri coils','the reopened page shows it, as stored');

  // 5 — Network: a typed last-contact date saves once
  posts.length=0;
  await page.click('#netPage .net-lc input[type=date]');await page.keyboard.type('09302026');
  await page.waitForTimeout(300);
  assert.equal(posts.length,0,'nothing saves while the date is being typed: '+JSON.stringify(posts));
  await page.click('.net-search');await page.waitForTimeout(400);
  assert.deepEqual(posts.map(x=>x.set),[{last_contact:'2026-09-30'}]);

  // 6 — the person page: an unsaved note outlives another write's repaint
  await page.click('#netList .net-row[data-id="contact/bo"]');
  await page.waitForSelector('.cp-note-editor');
  await page.fill('.cp-note-editor','old note, plus what I just learned');
  await page.click('.cp-loop-row input[type=checkbox]');await page.waitForTimeout(400);
  assert.equal(await page.inputValue('.cp-note-editor'),'old note, plus what I just learned','the draft survives the repaint');
  await page.click('.cp-note-actions .pill');await page.waitForTimeout(400);
  const btn=await page.textContent('.cp-note-actions .pill');
  assert.equal(btn,'Save note','a refused save gives the button back');
  assert.match(await page.textContent('body'),/couldn't save the note/,'and says so');
  assert.equal(await page.inputValue('.cp-note-editor'),'old note, plus what I just learned','keeping the text');
  assert.deepEqual(errors,[]);
  console.log('PASS: Fundraising keeps the next field\'s focus and typing through a save, saves a typed date once, skips unchanged blurs; Network edits land on the live rows and a typed date saves once; a person\'s unsaved note survives repaints and a refused save says so.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
