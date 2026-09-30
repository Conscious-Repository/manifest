// feed-reader-phone.cjs — the new feed reader's shell (sidebar + list) at
// phone widths, with the real stylesheets. Under 1000px the shell becomes a
// column; with align-items:flex-start each child took its content's width, so
// one long line — an approval's diff, a row's actions, an unbroken domain —
// widened the feed to 780px on a 390px phone and the page panned sideways
// (the owner's phone, 2026-09-30). Now, at 320 and 390 in both themes:
//   1. the page never scrolls sideways, and the list is the shell's width;
//   2. the view strip scrolls within itself; a diff scrolls within its box;
//   3. a long unbroken word wraps inside its row.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web'),read=p=>fs.readFileSync(path.join(root,p),'utf8');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
 const nav=(label,n)=>'<a class="rdr-nav" href="#"><span class="rdr-nav-label">'+label+'</span><span class="rdr-nav-n">'+n+'</span></a>';
 const html='<div id="contentScroll" class="content-scroll" style="height:100vh;overflow:auto"><section id="feedView" class="feed-view rdr-has-side"><div class="rdr-shell">'+
  '<nav id="feedSide" class="rdr-side"><div class="rdr-side-sec">'+nav('Inbox',22)+nav('Approvals',6)+nav('Unread',32)+nav('Today',16)+nav('Later','')+nav('All','')+nav('Curated',26)+'</div></nav>'+
  '<div class="feed-split"><div class="feed-col">'+
   '<div class="signal-row"><span class="signal-label">still waiting · excalibur · blog · Loomis invented ultrasound · 50d</span><span class="signal-actions"><button class="pill light">Done ✓</button><button class="pill light">dismiss</button></span></div>'+
   '<div class="consume-sub consume-curated-row"><div class="consume-sub-name">Ring family farm</div><div class="consume-sub-count micro-label" id="longword">michaelringfamilyagriculture.substack.com · [[michael ring]] · curated Sep 2</div></div>'+
   '<div class="appr-card"><div class="appr-title">Create vault note: 2026-09-28 time to catch up - aion 1a0e8dfeedd542a3.md</div><pre class="appr-diff">'+'+ '+'x'.repeat(4000)+'</pre></div>'+
  '</div></div></div></section></div>';
 for(const width of [320,390])for(const theme of ['default','jarvis']){
  const p=await browser.newPage({viewport:{width,height:844},isMobile:true,hasTouch:true});const errors=[];p.on('pageerror',e=>errors.push(e.message));
  await p.setContent('<html'+(theme==='jarvis'?' data-theme="jarvis"':'')+'><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body style="margin:0;padding:0 28px">'+html+'</body></html>');
  for(const f of ['00-core','05-primitives','07-nav','45-feed','46-consume','55-approvals','95-mobile','98-jarvis-hud'])await p.addStyleTag({content:read('css/'+f+'.css')});
  const at=theme+' at '+width;
  const g=await p.evaluate(()=>{const R=e=>e.getBoundingClientRect(),cs=document.getElementById('contentScroll'),shell=document.querySelector('.rdr-shell'),side=document.getElementById('feedSide'),diff=document.querySelector('.appr-diff'),word=document.getElementById('longword');
   return {pan:cs.scrollWidth-cs.clientWidth,doc:document.documentElement.scrollWidth-innerWidth,shell:R(shell).width,split:R(document.querySelector('.feed-split')).width,
    sideScrolls:side.scrollWidth>side.clientWidth&&getComputedStyle(side).overflowX==='auto',diffInside:R(diff).right<=R(shell).right+1&&diff.scrollWidth>diff.clientWidth,wordInside:R(word).right<=R(shell).right+1};});
  assert.equal(g.pan,0,at+': the feed pans sideways by '+g.pan+'px');
  assert.equal(g.doc,0,at+': the page scrolls sideways');
  assert.ok(Math.abs(g.split-g.shell)<1,at+': the list is '+g.split+'px in a '+g.shell+'px shell');
  assert.ok(g.sideScrolls,at+': the view strip does not scroll within itself');
  assert.ok(g.diffInside,at+': the diff does not scroll within its box');
  assert.ok(g.wordInside,at+': a long word runs past the list');
  assert.deepEqual(errors,[]);await p.close();
 }
 console.log('PASS: feed reader on a phone — no sideways pan at 320/390 in both themes; strip and diff scroll in place; long words wrap.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
