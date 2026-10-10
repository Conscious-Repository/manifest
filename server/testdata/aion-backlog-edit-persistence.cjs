// aion-backlog-edit-persistence.cjs — an edit made in the AION backlog stays
// made (2026-10-10 state audit, after "it appears temporarily and then doesn't
// persist" on a Feed card):
//   1. initials TYPED into the inspector's owner field save (they used to sit
//      there unsaved until the next repaint put the old owner back);
//   2. a save landing while the next field is being typed keeps those words,
//      the caret's field focus, and never commits a half-typed title;
//   3. a refused check toasts and repaints the server's state;
//   4. a poll held off by an edit picks the change up once the edit is done;
//   5. a goals.md task's check paints at once;
//   6. V/TO dirty bars don't pile up one per repaint.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 const p=await browser.newPage();const errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.setContent('<div id="aionView"><div id="aionToggle"></div><div id="aionMeta"></div><div id="aionBody"></div></div><button id="away">away</button>');
 await p.evaluate(()=>{
  window.els={aionView:document.getElementById('aionView'),aionToggle:document.getElementById('aionToggle'),aionMeta:document.getElementById('aionMeta'),aionBody:document.getElementById('aionBody')};
  window.isoToday=()=>'2026-10-10';window.railSetCount=()=>{};window.todosCache={me:'BA'};window.mf={phone:()=>false};
  window.toasts=[];window.showToast=(m,_,k)=>toasts.push([m,k]);window.setSaveState=()=>{};
  window.server={items:[{id:'aion-bl/t1',kind:'task',text:'Send deck',owner:'BA',rock:'',due:'',status:'open',sources:[]}],
   goalsArea:{rocks:[{id:'aion/r',text:'Rock',checked:false,children:[{id:'aion/r/m',text:'Mile',checked:false,children:[{id:'aion/r/m/g',text:'Goal task',checked:false,owner:'BA'}]}]}]},
   rev:'r1',refuse:false,delay:0};
  window.posts=[];window.gets=0;
  const json=(o)=>new Response(JSON.stringify(o),{status:200,headers:{'Content-Type':'application/json'}});
  window.fetch=async(url,opt)=>{
   if(opt&&opt.method==='POST'){
    const body=JSON.parse(opt.body||'{}');posts.push({url,body});
    if(server.delay)await new Promise(r=>setTimeout(r,server.delay));
    if(server.refuse)return new Response('a decided decision is permanent',{status:400});
    const m=url.match(/backlog\/update\/(.+)$/);
    if(m){const it=server.items.find(x=>x.id===decodeURIComponent(m[1]));Object.assign(it,body.title?{text:body.title}:{},body.owner!=null?{owner:body.owner}:{},body.due!=null?{due:body.due}:{},body.status?{status:body.status}:{});server.rev+='x';}
    return json({ok:true});
   }
   if(url==='/api/aion'){gets++;if(server.delay)await new Promise(r=>setTimeout(r,server.delay));return json({backlog:JSON.parse(JSON.stringify(server.items)),people:[{initials:'BA',name:'Ben'},{initials:'RT',name:'RJ'}],goalsArea:JSON.parse(JSON.stringify(server.goalsArea)),vto:[{heading:'Core values',entries:[{text:'Be kind',fields:[]}],raw:[]}]});}
   if(url.startsWith('/api/aion/revision'))return json({effectiveRevision:server.rev});
   return json({});
  };
 });
 for(const f of ['05-components','92-aion'])await p.addScriptTag({content:fs.readFileSync(path.join(root,'js',f+'.js'),'utf8')});
 await p.evaluate(async()=>{clearTimeout(aionPollTimer);aionSelId='aion-bl/t1';await loadAion();});

 // 1. typed owner initials save
 const owner=p.locator('.aion-inspector .ta-in').first();
 await owner.fill('rt');await p.click('#away');
 await p.waitForFunction(()=>posts.length===1);await p.waitForTimeout(50);
 assert.deepEqual(await p.evaluate(()=>posts[0].body),{owner:'RT'},'typed initials save, uppercased');
 assert.equal(await p.evaluate(()=>server.items[0].owner),'RT');
 assert.equal(await owner.inputValue(),'RT');

 // 2. a save's reload lands while the title is being typed
 await p.evaluate(()=>{server.delay=150;});
 await p.locator('.aion-inspector input[type=date]').fill('2026-11-01'); // change → save → slow reload
 const title=p.locator('.aion-inspector .aion-insp-title');
 await title.click();await title.press('End');await p.keyboard.type(' now');
 await p.waitForTimeout(450); // POST + reload + repaint
 assert.equal(await p.evaluate(()=>document.activeElement.dataset.aionField),'aion-bl/t1|title','the title keeps focus across the repaint');
 assert.equal(await title.inputValue(),'Send deck now','the half-typed words survive');
 assert.equal(await p.evaluate(()=>posts.filter(x=>x.body.title).length),0,'the detach-blur commits nothing');
 await p.keyboard.type('!');await p.keyboard.press('Enter');
 await p.waitForTimeout(400);
 assert.equal(await p.evaluate(()=>server.items[0].text),'Send deck now!');
 await p.evaluate(()=>{server.delay=0;document.activeElement.blur();});
 await p.waitForTimeout(50);

 // 3. a refused check toasts and converges
 await p.evaluate(()=>{server.refuse=true;toasts.length=0;});
 await p.locator('.aion-task-row .aion-check').first().click();
 await p.waitForTimeout(100);
 assert.equal(await p.evaluate(()=>server.items[0].status),'open');
 assert.equal(await p.locator('.aion-task-row').first().evaluate(n=>n.classList.contains('done')),false,'the refused check is repainted away');
 assert.equal(await p.evaluate(()=>toasts.some(([m,k])=>k==='error'&&/permanent/.test(m))),true,'the refusal is said');
 await p.evaluate(()=>{server.refuse=false;});

 // 4. a poll held by an edit still picks the change up afterwards
 await p.evaluate(async()=>{aionRevision='';await pollAionLive();clearTimeout(aionPollTimer);});
 await p.locator('.aion-inspector .aion-insp-title').focus();
 const before=await p.evaluate(()=>gets);
 await p.evaluate(async()=>{server.items[0].owner='HZ';server.rev='changed';await pollAionLive();clearTimeout(aionPollTimer);});
 assert.equal(await p.evaluate(()=>gets),before,'no reload while editing');
 await p.evaluate(async()=>{document.activeElement.blur();await new Promise(r=>setTimeout(r,20));await pollAionLive();clearTimeout(aionPollTimer);});
 assert.equal(await p.evaluate(()=>gets),before+1,'the held change loads once the edit is done');

 // 5. a goals.md task's check paints at once (and stays in place)
 await p.evaluate(()=>{server.delay=200;});
 const goalRow=p.locator('.aion-task-row',{hasText:'Goal task'});
 await goalRow.locator('.aion-check').click();
 assert.equal(await goalRow.evaluate(n=>n.classList.contains('done')),true,'checked at once, held in place');
 await p.evaluate(()=>{server.delay=0;});
 await p.waitForTimeout(450);

 // 6. V/TO dirty bars don't accumulate
 await p.evaluate(()=>{aionMode='vto';renderAion();renderAion();renderAion();});
 assert.equal(await p.locator('#aionView > .dirty-bar').count(),1,'one dirty bar, not one per repaint');
 assert.deepEqual(errors,[]);
 console.log('aion-backlog-edit-persistence ok');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1);});
