const {chromium}=require('playwright'),fs=require('fs'),assert=require('node:assert/strict');
const root=require('path').join(__dirname,'../web');
(async()=>{const browser=await chromium.launch({headless:true});try{
const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
await page.setContent('<main id="review" style="max-width:1100px;margin:auto;padding:12px"></main>');
for(const f of ['00-core','05-primitives','96-aion-recruiting','95-mobile'])await page.addStyleTag({content:fs.readFileSync(root+'/css/'+f+'.css','utf8')});
await page.addScriptTag({content:`
function el(tag,cls,text){const e=document.createElement(tag);e.className=cls||'';if(text!==undefined)e.textContent=text;return e;}
function linkEl(text,url){const a=el('a','',text);a.href=url;return a;}
function emptyRow(text){return el('p','',text);}
function fmtWhen(t){return '21 Sep';}
function manifestRevealElement(){}
function showToast(){}
`});
await page.addScriptTag({content:fs.readFileSync(root+'/js/96-aion-recruiting.js','utf8')});
await page.evaluate(()=>{
recCache={roles:[],candidates:[]};recFocusedReview=true;
const evidence={sourceId:'web',urlOrFile:'https://ada.example/bio',kind:'page',snippet:'Ada Example earned a PhD at Example University and worked at Example Lab.',retrievedAt:'2026-09-21'};
recRuns=[{id:'r1',source:'web',scope:{query:'Example Lab'},drafts:['Ada Example','Bea Example','Cia Example'].map((name,i)=>({id:'d'+i,status:'new',summary:{text:'Competencies: MRI reconstruction. AION relevance: supports instrumentation. Current location not established; no mutual connections recorded.',generatedAt:'2026-09-21',evidence:[evidence]},draft:{name,note:'found on https://ada.example/bio · discovered from seed · depth 0',evidence:[evidence],brief:{model:'DeepSeek',generatedAt:'2026-09-21',items:[{section:'education',text:'PhD at Example University',quote:'earned a PhD at Example University',url:evidence.urlOrFile}],evidence:[evidence]},linkedin:'https://linkedin.com/in/example'}}))}];
recPaint=()=>{const main=document.getElementById('review');main.replaceChildren();paintFocusedSourceReview(main);};recPaint();
});
await page.getByRole('button',{name:'Next',exact:true}).click();assert.equal(await page.locator('.rec-focused-title').textContent(),'Bea Example');
assert.equal(await page.getByRole('button',{name:'Next',exact:true}).evaluate(e=>e===document.activeElement),true);
await page.keyboard.press('Enter');assert.equal(await page.locator('.rec-focused-title').textContent(),'Cia Example');
await page.getByRole('button',{name:'Previous',exact:true}).click();
// the decision (2026-10-03): read, then decide — one primary, Network beside
// it, pass set apart at the end, and what each way in means said once
const bar=await page.locator('.rec-focused-candidate .rec-draft-actions').evaluate(a=>{const R=e=>e.getBoundingClientRect(),btns=[...a.querySelectorAll(':scope > button, :scope > .rec-keep > button')];
 return {labels:btns.map(b=>b.textContent.trim()),primary:btns.filter(b=>getComputedStyle(b).backgroundColor===getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()||b.matches('.rec-draft-accept')).map(b=>b.textContent.trim()),
  passLast:R(a.querySelector('.rec-draft-reject')).right>=Math.max(...btns.map(b=>R(b).right))-1,hint:a.querySelector('.rec-draft-choice-hint')?.textContent||'',
  belowSummary:R(a).top>R(document.querySelector('.rec-summary-text')).top,position:getComputedStyle(a).position};});
assert.deepEqual(bar.labels,['Add as candidate','Save to Network','Enhance','Later','pass'],'the decision buttons, in order');
assert.deepEqual(bar.primary,['Add as candidate'],'one primary');
assert.ok(bar.passLast,'pass sits apart at the end of the row');
assert.match(bar.hint,/Candidate: joins this search's pipeline.*Network: kept as someone around AION, not a candidate/);
assert.ok(bar.belowSummary,'the decision comes after the evidence');
assert.equal(bar.position,'sticky','the decision stays in reach');
assert.match(await page.locator('.rec-focused-head').textContent(),/Bea Example\s*new/);
await page.getByRole('button',{name:'Later',exact:true}).click();assert.equal(await page.locator('.rec-focused-title').textContent(),'Cia Example');
await page.getByText('Enhanced research · experience, education & public work',{exact:true}).click();
await page.getByText('Supporting quote · ada.example',{exact:true}).click();assert.equal(await page.locator('.rec-brief-citation[open] blockquote').first().textContent(),'earned a PhD at Example University');
assert.equal(await page.getByRole('button',{name:'Enhance',exact:true}).count(),1);
assert.equal(await page.locator('a[href="seed"]').count(),0);
assert.match(await page.locator('.rec-summary-text').textContent(),/Competencies:.*AION relevance:.*location/);
assert.equal(await page.locator('.rec-draft-name').isVisible(),false);
assert.match(await page.locator('#review').textContent(),/Not established by the collected evidence/);
for(const theme of ['light','jarvis'])for(const width of [1280,1000,390]){
 await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'overflow '+theme+' '+width);
 await page.screenshot({path:(process.env.RECRUITING_SCREENSHOT_DIR || '/tmp')+'/review-'+theme+'-'+width+'.png',fullPage:true});
}
assert.deepEqual(errors,[]);
console.log('PASS browser navigation and focus, Later progression, quote disclosure, empty sections, desktop/tablet/phone in both themes.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
