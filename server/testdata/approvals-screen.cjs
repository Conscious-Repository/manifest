// approvals-screen.cjs — the "Probably not" screen's two surfaces (2026-10-06):
//   1. Reject asks why with one tap: Duplicate · Already done · Not worth
//      tracking · Logistics only (keys 1–4), or a typed reason; no "wrong
//      owner or goal" (that is an edit, tracked on its own); Escape cancels;
//   2. folded cards sit under one collapsed, counted "probably not" section
//      that keeps every card and stays open across the feed's repaints.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 const p=await browser.newPage();const errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.setContent('<div id="pm" hidden><h2 id="pt"></h2><div id="pb"></div></div><main id="feed"></main>');
 for(const f of ['00-core','05-primitives','40-spirits','45-feed'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined&&text!==null)e.textContent=text;return e;};
  window.pill=(label,fn)=>{const b=el('button','pill',label);b.onclick=fn;return b;};window.pillLight=window.pill;
  window.els={pickerTitle:document.getElementById('pt'),pickerBody:document.getElementById('pb'),pickerModal:document.getElementById('pm')};
  window.closePicker=()=>{els.pickerModal.hidden=true;};window.showToast=()=>{};window.fmtWhen=(s)=>s;
 });
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/55-approvals.js'),'utf8')});
 // 1. one tap says why
 await p.evaluate(()=>{window.got=[];apprAskRejectReason((r)=>got.push(r));});
 const chips=await p.locator('.appr-reject-reason').allTextContents();
 assert.deepEqual(chips,['1 · Duplicate','2 · Already done','3 · Not worth tracking','4 · Logistics only']);
 assert.equal(chips.some((c)=>/owner|goal/i.test(c)),false,'wrong owner or goal is an edit, not a reason');
 await p.keyboard.press('2');
 assert.deepEqual(await p.evaluate(()=>got),['Already done']);
 assert.equal(await p.evaluate(()=>els.pickerModal.hidden),true);
 await p.evaluate(()=>{got=[];apprAskRejectReason((r)=>got.push(r));});
 await p.locator('#pb textarea').fill('covered in the board review');await p.keyboard.press('Control+Enter');
 assert.deepEqual(await p.evaluate(()=>got),['covered in the board review'],'a typed reason when none fits');
 await p.evaluate(()=>{got=[];apprAskRejectReason((r)=>got.push(r));});
 await p.keyboard.press('Escape');await p.keyboard.press('1');
 assert.deepEqual(await p.evaluate(()=>got),[],'Escape cancels; a later key does nothing');
 // 2. the fold
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/45-feed.js'),'utf8')});
 await p.evaluate(()=>{window.approvalCardEl=(a)=>el('div','feed-card',a.action);
  const paint=()=>{const host=document.getElementById('feed');host.replaceChildren(feedProbablyNot([{action:'aion: task — Send the update'},{action:'aion: task — Mention the mug'}]));};window.paint=paint;paint();});
 const fold=p.locator('details.feed-probably-not');
 assert.equal(await fold.evaluate(d=>d.open),false,'collapsed by default');
 assert.match(await p.locator('.feed-probably-not-head').textContent(),/probably not · 2/);
 assert.equal(await p.locator('.feed-probably-not .feed-card').count(),2,'every folded card is still there');
 await p.locator('.feed-probably-not-head').click();await p.waitForTimeout(50);await p.evaluate(()=>paint());
 assert.equal(await p.locator('details.feed-probably-not').evaluate(d=>d.open),true,'stays open across a repaint');
 assert.deepEqual(errors,[]);
 console.log('PASS: reject asks why in one tap (no owner/goal reason), typed or cancelled; the fold is collapsed, counted, complete and stays open.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
