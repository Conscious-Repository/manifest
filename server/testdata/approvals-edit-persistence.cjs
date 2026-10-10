// approvals-edit-persistence.cjs — an edit made on an approval card stays made
// (2026-10-10, owner: "when I change the owner here, it appears temporarily
// and then doesn't persist"):
//   1. an aion card's picked owner survives the card being rebuilt (every feed
//      load rebuilds every card), survives a poll read begun before its save,
//      and a clean card still takes a later server change;
//   2. the inspector's property pick keeps the slug — the browser's `change`
//      on blur (carrying the picked LABEL) no longer overwrites it;
//   3. a save landing does not rebuild the inspector field being typed in, and
//      Tab after an edit keeps the focus it moved;
//   4. a re-contract draft is not reset by a feed read begun before its save.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 const p=await browser.newPage();const errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.setContent('<div id="wrap"><main id="feed"></main><aside id="insp"></aside></div><button id="away">away</button>');
 await p.evaluate(()=>{
  window.els={feedInspector:document.getElementById('insp')};window.state={};
  window.setSaveState=()=>{};window.showToast=()=>{};window.fmtWhen=(s)=>s;window.loadFeed=()=>{};
  window.rePropLabel=(x)=>x.short||x.address||x.slug; // 85-re-domain.js
  window.posts=[];
  const json=(o)=>Promise.resolve(new Response(JSON.stringify(o),{status:200,headers:{'Content-Type':'application/json'}}));
  window.fetch=(url,opt)=>{
   if(opt&&opt.method==='POST'){posts.push({url,body:JSON.parse(opt.body)});return new Promise((r)=>setTimeout(()=>r(new Response('{"ok":true}',{status:200})),60));}
   if(url==='/api/aion')return json({goalsArea:{rocks:[]},people:[{initials:'HZ',name:'Hong'},{initials:'BA',name:'Ben'}]});
   if(url==='/api/properties')return json({properties:[{slug:'12-main',address:'12 Main St',short:'12 Main'},{slug:'9-elm',address:'9 Elm St',short:'9 Elm'}],deals:[]});
   if(url==='/api/properties/people')return json({people:[{initials:'OL',name:'Olga'}]});
   if(url==='/api/realestate/entities')return json({contractors:[]});
   return json({});
  };
 });
 for(const f of ['05-components','55-approvals'])await p.addScriptTag({content:fs.readFileSync(path.join(root,'js',f+'.js'),'utf8')});
 await p.evaluate(()=>{
  window.aionRow=(owner,readAt)=>({id:'a1',type:'aion-backlog',applyPath:'system/aion/backlog.md',allowed:true,action:'aion: task — Send deck',body:'',
   aionPayload:{kind:'task',title:'Send deck',owner,rock:'',sources:[]},__readAt:readAt});
  window.paint=(row)=>{document.getElementById('feed').replaceChildren(approvalCardEl(row));};
  window.ownerInput=()=>document.querySelectorAll('.aion-appr-form .ta-in')[0];
 });

 // 1. the aion card
 await p.evaluate(()=>paint(aionRow('BA',Date.now())));
 await p.locator('.aion-appr-form .ta-in').first().fill('HZ');
 await p.click('#away'); // blur → change commits the typed owner
 await p.evaluate(()=>paint(aionRow('BA',Date.now()))); // a decision elsewhere reloads the feed
 assert.equal(await p.evaluate(()=>ownerInput().value),'HZ','an unsaved owner survives the rebuild');
 assert.match(await p.locator('.aion-appr-line').textContent(),/\[owner:: HZ\]/);
 await p.locator('button',{hasText:'save edit'}).click();
 await p.waitForFunction(()=>posts.length===1);await p.waitForTimeout(100);
 assert.equal(await p.evaluate(()=>posts[0].body.owner),'HZ');
 await p.evaluate(()=>paint(aionRow('BA',Date.now()-5000))); // a poll read begun before the save
 assert.equal(await p.evaluate(()=>ownerInput().value),'HZ','a read older than the save does not put the old owner back');
 await p.evaluate(()=>paint(aionRow('HZ',Date.now())));
 assert.equal(await p.evaluate(()=>ownerInput().value),'HZ');
 await p.evaluate(()=>paint(aionRow('RJ',Date.now()))); // changed elsewhere, nothing unsaved here
 assert.equal(await p.evaluate(()=>ownerInput().value),'RJ','a clean card takes a newer server copy');

 // 2–4. the re-contract inspector
 await p.evaluate(()=>{
  window.rcPayload=()=>({kind:'bid',contractor:'acme',name:'Roof',total:100,allocations:[{property:'12-main',node:'roof/x',amount:100}],
   tasks:[{text:'Do it',property:'9-elm',parent:'roof'}]});
  window.rcRow=(payload,readAt)=>({id:'rc1',type:'re-contract',applyPath:'system/realestate/contracts/x.md',allowed:true,action:'Roof bid',body:'',reContractPayload:payload,__readAt:readAt});
  apprDraftsKeep(['rc1']);
  paint(rcRow(rcPayload(),Date.now()));
  apprSelect({id:'rc1',kind:'task',i:0});
 });
 const prop=p.locator('#insp .ta-in').nth(0);
 await prop.click();await prop.fill('main');
 await p.locator('#insp .ta-item',{hasText:'12 Main'}).click();
 await p.click('#away');
 assert.equal(await p.evaluate(()=>apprDrafts.get('rc1').p.tasks[0].property),'12-main','the pick keeps the slug, not the label');
 await p.waitForFunction(()=>posts.length===2);
 assert.equal(await p.evaluate(()=>posts[1].body.tasks[0].property),'12-main');
 await p.waitForTimeout(150);

 // 3. Tab after an edit keeps the focus; typing in the next field survives the save
 const text=p.locator('#insp input.pp-in').first();
 await text.click();await text.press('End');await p.keyboard.type(' now');await p.keyboard.press('Tab');
 await p.waitForTimeout(30);
 assert.equal(await p.evaluate(()=>document.activeElement===document.querySelectorAll('#insp .ta-in')[0]),true,'Tab lands in the next field');
 await p.keyboard.press('Escape');await p.keyboard.type('xyz'); // (Tab selected the field: this replaces it)
 await p.waitForFunction(()=>posts.length===3);await p.waitForTimeout(150); // the debounced save went and landed
 assert.equal(await p.evaluate(()=>posts[2].body.tasks[0].text),'Do it now');
 const still=await p.evaluate(()=>{const i=document.querySelectorAll('#insp .ta-in')[0];return {focused:document.activeElement===i,value:i.value,foot:document.querySelector('#insp .aion-insp-foot').textContent};});
 assert.deepEqual(still,{focused:true,value:'xyz',foot:'edits save as you go'},'the save redrew the status line only');

 // 4. a read begun before the save does not reset the draft
 await p.click('#away');await p.waitForTimeout(950); // (the blur saved the typed text as-is; let it land)
 await p.evaluate(()=>paint(rcRow(rcPayload(),Date.now()-5000)));
 assert.equal(await p.evaluate(()=>apprDrafts.get('rc1').p.tasks[0].text),'Do it now','the stale read is not taken for a server change');
 assert.deepEqual(errors,[]);
 console.log('PASS: an aion owner survives rebuilds, stale reads and adopts newer server copies when clean; inspector picks keep the slug; a landing save leaves the field being typed in alone; a stale read never resets a saved re-contract draft.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
