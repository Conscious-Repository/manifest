// home-plan-timeline.cjs — the shared Home timeline module (90-home-plan.js)
// over a stubbed /api/home/plan whose body is the Go server's own derivation
// of homeplan/testdata/plan.json (written by TestFixtureHomePlanTimeline):
//   1. the timeline is lanes on one axis (travel, holds, work) and one
//      sentence: 28 h estimated leaves 52 h of 80 h; the Roof 56 h what-if
//      turns it into 4 h more than the open time — negative kept visible;
//   2. the "To resolve" feed lists each open question as a card; a card's
//      Save writes only that field, against the revision it read; a 409
//      keeps the answer on screen with the error;
//   3. task notes and titles render as text: no markup runs, only http(s)
//      URLs become links;
//   4. at 390 px neither view pans sideways; no derived value is missing.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
const plan=JSON.parse(fs.readFileSync(process.env.HP_FIXTURE,'utf8'));
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 for(const width of [1280,390]){
 const p=await browser.newPage({viewport:{width,height:900}});const errors=[];p.on('pageerror',e=>errors.push(e.message));
 // a real origin (not about:blank) so drafts can live in localStorage
 await p.route('http://hp.test/**',r=>r.fulfill({contentType:'text/html',body:'<!doctype html><meta name="viewport" content="width=device-width"><body style="margin:0"><div id="pickerModal" hidden><span id="pickerTitle"></span><div id="pickerBody"></div></div><main id="host" style="padding:0 16px"></main><main id="feed" style="padding:0 16px"></main><div id="toastHost"></div>'}));
 await p.goto('http://hp.test/');
 for(const f of ['00-core','05-primitives','90-home-plan'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.evaluate((plan)=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined&&text!==null)e.textContent=text;return e;};
  window.pill=(label,fn)=>{const b=el('button','pill',label);b.onclick=fn;return b;};window.pillLight=(l,f)=>{const b=pill(l,f);b.classList.add('light');return b;};
  window.inputEl=(ph)=>{const i=document.createElement('input');i.placeholder=ph;return i;};
  window.debounce=(fn)=>fn;window.showToast=()=>{};window.setSaveState=()=>{};
  window.els={pickerTitle:document.getElementById('pickerTitle'),pickerBody:document.getElementById('pickerBody'),pickerModal:document.getElementById('pickerModal')};
  plan.tasks['home/roof-on'].text='<img src=x onerror="window.pwned=1">roof on';
  window.calls=[];window.planBody=plan;window.postStatus=200;
  window.fetch=async(url,init)=>{const body=init&&init.body?JSON.parse(init.body):null;calls.push({url,method:(init&&init.method)||'GET',body});
   const ok=(o)=>new Response(JSON.stringify(o),{status:200});
   if(url==='/api/home/plan/preview'){
    // a what-if preview: answer with that scenario's own derivation
    const hit=Object.entries(planBody.plan.scenarios||{}).find(([,s])=>JSON.stringify(s.patch)===JSON.stringify(body.patch));
    if(hit){const o=planBody.derived.scenarios[hit[0]];return ok({...planBody,derived:{...planBody.derived,capacity:o.capacity,conflicts:o.conflicts,sequence:o.sequence}});}
    return ok(planBody);}
   if(url==='/api/home/plan'&&body){if(postStatus!==200)return new Response(JSON.stringify({error:'the plan changed',revision:'r2'}),{status:postStatus});return ok({...planBody,revision:'r-next'});}
   return ok(planBody);};
 },plan);
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/90-home-plan.js'),'utf8')});
 await p.evaluate(()=>{localStorage.clear();hp.draft=null;homePlanRender(document.getElementById('host'));});
 await p.waitForSelector('.hp-lane');
 // 1. lanes and the one sentence
 const lanes=await p.locator('.hp-lane-title').allInnerTexts();
 assert.equal(lanes[0],'Travel');
 for(const want of ['prep and coating','sealing and checks','contingency'])assert.ok(lanes.some(l=>l.toLowerCase()===want),'no lane for '+want+': '+lanes.join(' | '));
 assert.ok(lanes.some(l=>/roof on \+ flashing-into-house$/.test(l)),'roof lane names what its estimate includes: '+lanes.join(' | '));
 assert.equal(await p.locator('.hp-blk.is-away').count(),3,'three away weekends');
 assert.match(await p.locator('.hp-sentence').innerText(),/28 h of estimated work leaves 52 h of the 80 h of open weekend time/);
 assert.equal(await p.locator('.hp-grid, .hp-stat, .hp-agenda').count(),0,'the old grid and stat cards are gone');
 assert.equal(await p.evaluate(()=>window.pwned),undefined,'a task title ran as markup');
 const shown=await p.evaluate(()=>document.querySelector('.hp').innerText);
 assert.ok(!/NaN|undefined/.test(shown),'a derived value is missing: '+(shown.match(/.{0,40}(NaN|undefined).{0,20}/)||[])[0]);
 let pan=await p.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth);
 assert.ok(pan<=0,'timeline pans sideways by '+pan+' at '+width);
 // a what-if changes the sentence, says so, and writes nothing
 await p.click('.hp-whatif-chip:has-text("Roof 56 h")');await p.waitForTimeout(50);
 assert.match(await p.locator('.hp-sentence').innerText(),/is 4 h more than the 80 h/);
 assert.match(await p.locator('.hp-mode').innerText(),/What if: “Roof 56 h”/);
 assert.ok(!(await p.evaluate(()=>calls.some(x=>x.method==='POST'&&x.url==='/api/home/plan'))),'a what-if wrote the plan');
 await p.click('.hp-whatif-chip:has-text("Saved plan")');await p.waitForTimeout(50);
 // 2. the feed
 await p.evaluate(()=>homePlanFeedRender(document.getElementById('feed')));
 await p.waitForSelector('.hp-card');
 const kinds=await p.locator('.hp-card-kicker').allInnerTexts();
 const sections=await p.locator('.hp-feed-section').allInnerTexts();
 assert.ok(sections.some(k=>/^Hours · \d+/i.test(k))&&sections.some(k=>/^Lead times · \d+/i.test(k)),'feed sections: '+sections.join(' | ')+' / '+kinds.join(' | '));
 const feedText=await p.evaluate(()=>document.getElementById('feed').innerText);
 assert.ok(!/NaN|undefined/.test(feedText),'feed shows a missing value');
 pan=await p.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth);
 assert.ok(pan<=0,'feed pans sideways by '+pan+' at '+width);
 if(width===390){await p.close();assert.deepEqual(errors,[]);continue;}
 // answering one card writes exactly that field against the read revision
 const roofCard=p.locator('.hp-card',{hasText:'How many hours for roof-on'});
 await p.evaluate(()=>{calls.length=0;});
 await roofCard.locator('input').fill('30');await roofCard.locator('button:has-text("Save")').click();await p.waitForTimeout(100);
 let post=(await p.evaluate(()=>calls)).find(x=>x.method==='POST'&&x.url==='/api/home/plan');
 assert.deepEqual(post&&post.body,{revision:plan.revision,patch:{tasks:{'home/roof-on':{estimate:{hours:30,basis:'user'}}}}},JSON.stringify(post));
 // a refused save shows why, on the card, with the answer still there
 await p.evaluate(()=>{postStatus=409;});
 const waitCard=p.locator('.hp-card',{hasText:'How long is the wait'}).first();
 await waitCard.locator('input').fill('21');await waitCard.locator('button:has-text("Save")').click();await p.waitForTimeout(100);
 assert.match(await waitCard.locator('.hp-card-error').innerText(),/same moment/);
 assert.equal(await waitCard.locator('input').inputValue(),'21');
 await p.evaluate(()=>{postStatus=200;});
 // 3. notes: text only, http(s) links only
 const notes=await p.evaluate(()=>{const v=homePlanNotesView('### Glass\nsee https://example.com/glass and javascript:alert(1)\n<script>window.pwned=2</script><img src=x onerror="window.pwned=3">\n### Roof\nslope');document.body.append(v);
  return {sections:v.querySelectorAll('details').length,links:[...v.querySelectorAll('a')].map(a=>[a.href,a.rel]),imgs:v.querySelectorAll('img,script').length,text:v.textContent.includes('<script>')};});
 assert.deepEqual(notes,{sections:2,links:[['https://example.com/glass','noopener noreferrer']],imgs:0,text:true});
 assert.equal(await p.evaluate(()=>window.pwned),undefined);
 // the main inspector's summary offers the editor
 const hook=await p.evaluate(()=>{const a=document.createElement('div');document.body.append(a);homePlanPanelHook(a,'home/roof-on');return a.innerText;});
 assert.match(hook,/schedule · home timeline[\s\S]*24–56 h allowance[\s\S]*Edit schedule/i);
 assert.deepEqual(errors,[]);
 await p.close();
 }
 console.log('PASS: Home timeline — lanes on one axis with one sentence (28 h leaves 52 h of 80 h; Roof 56 h → 4 h over, never written); the To-resolve feed answers one field per card against the read revision and keeps a refused answer; notes never run markup; neither view pans at 390 px.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
