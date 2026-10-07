// network-phone.cjs — Network's facet rows at phone width with the real
// stylesheets: a long experience topic wraps inside its chip instead of
// widening the page (a 62px sideways pan at 390, 2026-10-07).
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web'),read=p=>fs.readFileSync(path.join(root,p),'utf8');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 for(const width of [320,390])for(const theme of ['default','jarvis']){
  const p=await browser.newPage({viewport:{width,height:844},isMobile:true,hasTouch:true});
  await p.setContent('<html'+(theme==='jarvis'?' data-theme="jarvis"':'')+'><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body style="margin:0;padding:0 28px"><div id="contentScroll" class="content-scroll" style="overflow:auto"><div class="net-facet-row"><span class="micro-label">experience</span><button class="filter-chip">Advanced Neuroimaging Techniques and Applications 1</button><button class="filter-chip">Atomic and Subatomic Physics Research 1</button></div></div></body></html>');
  for(const f of ['00-core','05-primitives','62-network','95-mobile','98-jarvis-hud'])await p.addStyleTag({content:read('css/'+f+'.css')});
  const g=await p.evaluate(()=>{const c=document.getElementById('contentScroll');return {pan:c.scrollWidth-c.clientWidth,doc:document.documentElement.scrollWidth-innerWidth};});
  assert.equal(g.pan,0,theme+' at '+width+': a long topic widens the page by '+g.pan+'px');
  assert.equal(g.doc,0,theme+' at '+width+': the page scrolls sideways');
  await p.close();
 }
 console.log('PASS: Network facet chips wrap inside the row at 320/390 in both themes.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
