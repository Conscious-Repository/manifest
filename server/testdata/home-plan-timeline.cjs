// home-plan-timeline.cjs — the shared Home timeline module (90-home-plan.js)
// over a stubbed /api/home/plan whose body is the Go server's own derivation
// of homeplan/testdata/plan.json (written by TestFixtureHomePlanTimeline):
//   1. the summary and scenarios show 128 h / 80 h pool / 28 h known / 52 h
//      left, and 28/16/4/−4 for the roof allowances — negative kept visible;
//   2. a cell edit becomes a DRAFT (previewed, never posted to the plan) until
//      Save, which sends {revision, patch}; a 409 keeps the draft;
//   3. task notes and titles render as text: no markup runs, only http(s)
//      URLs become links;
//   4. at 390 px the same plan is an agenda that never pans sideways.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
const plan=JSON.parse(fs.readFileSync(process.env.HP_FIXTURE,'utf8'));
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 for(const width of [1280,390]){
 const p=await browser.newPage({viewport:{width,height:900}});const errors=[];p.on('pageerror',e=>errors.push(e.message));
 // a real origin (not about:blank) so the draft can live in localStorage
 await p.route('http://hp.test/**',r=>r.fulfill({contentType:'text/html',body:'<!doctype html><meta name="viewport" content="width=device-width"><body style="margin:0"><div id="pickerModal" hidden><span id="pickerTitle"></span><div id="pickerBody"></div></div><main id="host" style="padding:0 16px"></main><div id="toastHost"></div>'}));
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
   if(url==='/api/home/plan/preview')return ok(planBody);
   if(url==='/api/home/plan'&&body){if(postStatus!==200)return new Response(JSON.stringify({error:'the plan changed',revision:'r2'}),{status:postStatus});return ok({...planBody,revision:'r-next'});}
   return ok(planBody);};
 },plan);
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/90-home-plan.js'),'utf8')});
 await p.evaluate(()=>{localStorage.clear();hp.draft=null;homePlanRender(document.getElementById('host'));});
 await p.waitForSelector('.hp-stat');
 const stats=(await p.locator('.hp-stat').allInnerTexts()).join(' / ');
 for(const want of ['128 h together','80 h','28 h','52 h'])assert.ok(stats.includes(want),'summary lacks '+want+': '+stats);
 const scen=(await p.locator('.hp-scenario').allInnerTexts()).map(s=>s.replace(/\s+/g,' '));
 assert.deepEqual(scen.map(s=>s.match(/(−?\d+ h) left/)[1]),['28 h','16 h','4 h','−4 h'],scen.join(' | '));
 assert.equal(await p.evaluate(()=>window.pwned),undefined,'a task title ran as markup');
 const pan=await p.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth);
 assert.ok(pan<=0,'page pans sideways by '+pan+' at '+width);
 if(width===390){assert.equal(await p.locator('.hp-agenda').count(),1,'phones get the agenda');assert.equal(await p.locator('.hp-grid').count(),0);await p.close();assert.deepEqual(errors,[]);continue;}
 // 2. a cell edit is a draft, previewed, not saved
 await p.click('[data-hp-focus="c:home/plan-windows#panes:2026-11-07"]');await p.keyboard.type('6');await p.keyboard.press('Enter');await p.waitForTimeout(100);
 assert.match(await p.locator('.hp-mode').first().innerText(),/Draft · 1 change not saved/);
 let c=await p.evaluate(()=>calls.splice(0));
 assert.ok(c.some(x=>x.url==='/api/home/plan/preview'&&x.body.patch.tasks['home/plan-windows'].subtasks.panes.allocations['2026-11-07']===6),'draft previewed');
 assert.ok(!c.some(x=>x.url==='/api/home/plan'&&x.method==='POST'),'a draft edit must not write the plan');
 // a refused save keeps the draft
 await p.evaluate(()=>{postStatus=409;});
 await p.click('.hp-mode button:has-text("Save to plan")');await p.waitForTimeout(100);
 assert.match(await p.locator('.hp-save-error').innerText(),/draft is kept/);
 assert.ok(await p.evaluate(()=>!!JSON.parse(localStorage.getItem('homePlan.draft.v1')).patch.tasks),'draft survives a 409');
 // Save sends the revision it read and the patch, then clears the draft
 await p.evaluate(()=>{postStatus=200;calls.length=0;});
 await p.click('.hp-mode button:has-text("Save to plan")');await p.waitForTimeout(100);
 c=await p.evaluate(()=>calls.splice(0));
 const post=c.find(x=>x.url==='/api/home/plan'&&x.method==='POST');
 assert.ok(post&&post.body.revision===plan.revision&&post.body.patch.tasks['home/plan-windows'].subtasks.panes.allocations['2026-11-07']===6,JSON.stringify(post));
 assert.equal(await p.evaluate(()=>localStorage.getItem('homePlan.draft.v1')),null);
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
 console.log('PASS: Home timeline — 128/80/28/52 and 28/16/4/−4 from the server derivation; cell edits are previewed drafts until Save (revision + patch), a 409 keeps the draft; notes and titles never run markup; phones get a non-panning agenda; the inspector summary opens the editor.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
