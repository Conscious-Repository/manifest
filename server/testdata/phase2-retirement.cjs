// Retired Excalibur rituals render a disabled "retired" control that names
// the ritual page, a live connector keeps "run now", and a stale picker that
// spools a retired ritual shows the retirement (not "already running").
// ritualRow composes its row with real DOM queries (schedComposeRow), so this
// runs in a real page with the shared el() rather than a hand-rolled fake.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const js=f=>fs.readFileSync(path.join(__dirname,'../web/js',f),'utf8');
(async()=>{const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chromium',headless:true});try{
  const p=await browser.newPage();await p.setContent('<main></main>');
  await p.addScriptTag({content:js('05-components.js')});
  await p.addScriptTag({content:js('41-agents-schedule.js')});
  await p.evaluate(()=>{window.fmtWhen=()=>'fixture time';ritualRuns=()=>[];outcomeStrip=()=>el('span','outcome-strip');});
  for(const [spirit,ritual] of [['concierge','briefing'],['ea-coordinator','waiting-on'],['sage','skill-cast']]){
    const row={spirit,ritual,valid:true,enabled:false,retired:true,retirementReason:'retired/paused; see #/agents/ritual/'+spirit+'/'+ritual,pausedReason:'retired/paused',ceilingUsd:1};
    const got=await p.evaluate(row=>{const r=ritualRow(row);const acts=[...r.querySelector('.ritual-acts').children];return{group:schedGroupOf(row),acts:acts.map(b=>({text:b.textContent,disabled:b.disabled,title:b.title}))};},row);
    assert.equal(got.group,'paused');
    assert.equal(got.acts.length,1);
    assert.equal(got.acts[0].text,'retired');
    assert.equal(got.acts[0].disabled,true);
    assert.match(got.acts[0].title,/#\/agents\/ritual\//);
  }
  const run=await p.evaluate(()=>{const b=ritualRow({spirit:'ea-coordinator',ritual:'email-sync',valid:true,enabled:true,ceilingUsd:1}).querySelector('.ritual-acts').children[0];return{text:b.textContent,disabled:b.disabled};});
  assert.equal(run.text,'run now');
  assert.equal(run.disabled,false);
  console.log('Phase 2 retirement controls passed');
  // A stale picker must display retirement, not the existing already-running toast.
  const spoolSource=js('40-agents.js').match(/async function spiritSpool\([\s\S]*?\n\}/)[0];
  await p.addScriptTag({content:spoolSource});
  const toast=await p.evaluate(async()=>{let toast;window.fetch=async()=>({status:409,json:async()=>({retired:true,error:'retired/paused; see Agents'})});window.showToast=(text,action)=>{toast={text,action};};await spiritSpool('sage','skill-cast','');const text=toast&&toast.text;toast.action();return{text,hash:location.hash};});
  assert.match(toast.text,/retired\/paused/);
  assert.equal(toast.hash,'#/agents/ritual/sage/skill-cast');
  console.log('Phase 2 retirement toast passed');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1);});
