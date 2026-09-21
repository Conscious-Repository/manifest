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
await page.getByRole('button',{name:'Later this session',exact:true}).click();assert.equal(await page.locator('.rec-focused-title').textContent(),'Cia Example');
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
