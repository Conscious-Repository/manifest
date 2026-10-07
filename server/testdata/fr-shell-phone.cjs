// fr-shell-phone.cjs — the fr-shell tables (Contractors, Fundraising) on a
// phone, after the owner's Contractors screenshot (2026-10-07): every name
// truncated, property lines printed over the "accepted" line, four state chips
// wrapped into three rows, the lit tab cut off as "CON". At 320 and 390:
//   - nothing scrolls sideways; a name is whole; a row's lines never overlap;
//   - the search has its row and the state chips share one;
//   - the lit tab is inside its scroller's view;
//   - an absent value says nothing (no "—" line);
// and at 1440 the four-column table is unchanged (header shown, no card areas).
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/fr-shell-phone.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./writing-stub-api.cjs');
(async()=>{
 const stub=makeStub({json:require('./fr-shell-data.cjs')});await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
  for(const theme of ['default','jarvis'])for(const w of [320,390,1440])for(const route of ['/#/properties/contractors','/#/aion/fundraising']){
   const ctx=await browser.newContext({viewport:{width:w,height:844},isMobile:w<861,hasTouch:w<861});await ctx.addInitScript(t=>{try{localStorage.setItem('manifest.theme',t)}catch(e){}},theme);
   const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+route);await page.waitForSelector('.fr-table .fr-row:not(.fr-head)');await page.waitForTimeout(300);
   const at=route.split('/').pop()+' '+theme+' '+w;
   const g=await page.evaluate(()=>{
    const R=e=>e.getBoundingClientRect(),main=document.querySelector('.fr-main'),rows=[...document.querySelectorAll('.fr-table .fr-row:not(.fr-head)')];
    const overlap=(a,b)=>{const x=R(a),y=R(b);return x.width&&y.width&&x.left<y.right-1&&y.left<x.right-1&&x.top<y.bottom-1&&y.top<x.bottom-1};
    const clashes=[];for(const row of rows){const kids=[...row.querySelectorAll(':scope > *')].filter(k=>k.getClientRects().length);for(let i=0;i<kids.length;i++)for(let j=i+1;j<kids.length;j++)if(overlap(kids[i],kids[j]))clashes.push(row.querySelector('.fr-firm-name').textContent)}
    const names=rows.map(r=>r.querySelector('.fr-firm-name')).filter(n=>n.scrollWidth>n.clientWidth+1).map(n=>n.textContent);
    const chips=[...document.querySelectorAll('.fr-toolbar .filter-chip')].map(c=>Math.round(R(c).top));
    const tab=document.querySelector('.view-tab.on'),strip=tab?.parentElement;
    const head=document.querySelector('.fr-table .fr-head');
    return {sideways:main.scrollWidth-main.clientWidth,pageSideways:document.documentElement.scrollWidth-innerWidth,clashes,names,chipRows:new Set(chips).size,
     tabIn:tab?R(tab).left>=R(strip).left-1&&R(tab).right<=R(strip).right+1:true,head:head?getComputedStyle(head).display:'none',
     dashes:[...document.querySelectorAll('.fr-table .fr-row:not(.fr-head) *')].filter(e=>e.getClientRects().length&&e.children.length===0&&e.textContent.trim()==='—').length,
     areas:rows.map(r=>getComputedStyle(r).gridTemplateAreas)};
   });
   assert.equal(g.pageSideways,0,at+': the page pans sideways');
   assert.deepEqual(g.clashes,[],at+': row parts overlap');
   assert.ok(g.tabIn,at+': the lit tab is cut off');
   if(w<861){
    assert.ok(g.sideways<=0,at+': the table scrolls sideways by '+g.sideways);
    assert.deepEqual(g.names,[],at+': names truncated');
    assert.equal(g.chipRows,1,at+': state chips in '+g.chipRows+' rows');
    assert.equal(g.head,'none',at+': the column header shows');
    assert.equal(g.dashes,0,at+': an absence prints "—"');
   }else{
    assert.notEqual(g.head,'none',at+': desktop keeps its header');
    assert.ok(g.areas.every(a=>a==='none'),at+': desktop rows keep their columns '+g.areas);
   }
   assert.deepEqual(errors,[],at);await ctx.close();
  }
  // Portfolio and Money: their filters (chips and pickers) share one row too
  for(const w of [320,390])for(const route of ['/#/properties/portfolio','/#/properties/money']){
   const ctx=await browser.newContext({viewport:{width:w,height:844},isMobile:true,hasTouch:true});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+route);await page.waitForSelector('.fr-toolbar .fr-chips');await page.waitForTimeout(300);const at=route.split('/').pop()+' '+w;
   const g=await page.evaluate(()=>({rows:new Set([...document.querySelectorAll('.fr-chips > *')].filter(e=>e.getClientRects().length).map(e=>Math.round(e.getBoundingClientRect().top+e.getBoundingClientRect().height/2))).size,pan:document.documentElement.scrollWidth-innerWidth}));
   assert.equal(g.rows,1,at+': filters in '+g.rows+' rows');assert.equal(g.pan,0,at+': sideways pan');assert.deepEqual(errors,[],at);await ctx.close();
  }
  console.log('PASS: fr-shell tables on a phone — no sideways scroll, whole names, no overlaps, one chip row (Portfolio, Money too), lit tab in view, no "—"; desktop four columns — 320/390/1440 × default/jarvis.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
