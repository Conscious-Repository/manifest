const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());await page.setContent('<main id="root"></main>');
 const cssDir=path.join(__dirname,'../web/css');await page.addStyleTag({content:fs.readdirSync(cssDir).filter(f=>f.endsWith('.css')).sort().map(f=>fs.readFileSync(path.join(cssDir,f),'utf8')).join('\n')});
 await page.evaluate(()=>{
 window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e};window.fmtWhen=x=>x;
 window.renderMarkdown=text=>el('p','',text);window.loadFeed=()=>{};window.setSaveState=()=>{};window.showToast=()=>{};
 window.pillLight=(text,fn)=>{const b=el('button','pill light',text);b.onclick=fn;return b};window.requests=[];window.failRead=true;window.failDismiss=true;
 window.fixture={record:{operationId:'sha256:fixture',status:'succeeded',arguments:{email:{from:'ben@aion.bio',to:['candidate@example.test'],subject:'Opportunity',body:'Invitation'}},emailWatch:{enabled:false,stopAfterReply:true,stoppedReason:'reply_found',replies:[{from:'Candidate',at:'2026-09-24T12:00:00Z',body:'<script>private reply</script>'}]}},proposal:{id:'approval-fixture',action:'email'}};
 window.fetch=async(url,options)=>{requests.push({url,method:options?.method||'GET',body:options?.body});if(options?.method==='POST')return {ok:!failDismiss,text:async()=> 'Newer reply arrived'};return {ok:!failRead,json:async()=>fixture}};
 });
 const components=fs.readFileSync(path.join(__dirname,'../web/js/05-components.js'),'utf8');await page.addScriptTag({content:components.slice(components.indexOf('function cardShell('),components.indexOf('// ---- ghost input'))});
 const approvals=fs.readFileSync(path.join(__dirname,'../web/js/55-approvals.js'),'utf8');await page.addScriptTag({content:approvals.slice(approvals.indexOf('function manifestOperationCard'),approvals.indexOf('let manifestRevealTarget'))});
 const feed=fs.readFileSync(path.join(__dirname,'../web/js/45-feed.js'),'utf8');await page.addScriptTag({content:feed.slice(feed.indexOf('function portalCardEl'),feed.indexOf('function faviconFor'))});
 await page.evaluate(()=>document.getElementById('root').append(portalCardEl({id:'email-reply:fixture:reply',operationId:'sha256:fixture',portal:'email',type:'portal-item',title:'Reply · Opportunity',detail:'Received by ben@aion.bio',actor:'Candidate',date:'2026-09-24',change:'new'})));
 await page.getByRole('button',{name:'view replies',exact:true}).click();await page.getByText('Could not load the sent receipt. Try again.',{exact:true}).waitFor();
 await page.evaluate(()=>failRead=false);await page.getByRole('button',{name:'view replies',exact:true}).click();await page.getByText('Tracking stopped · reply found',{exact:true}).waitFor();
 await page.getByText(/Candidate ·/).click();await page.getByText('<script>private reply</script>',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Approve',exact:true}).count(),0);
 await page.getByRole('button',{name:'Dismiss',exact:true}).click();assert.equal(await page.locator('.portal-card').count(),1,'refused dismissal must restore card');
 for(const width of [320,390,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);}
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'/tmp/manifest-email-reply-notice.png'});
 await page.evaluate(()=>failDismiss=false);await page.getByRole('button',{name:'Dismiss',exact:true}).click();assert.equal(await page.locator('.portal-card').count(),0);
 const requests=await page.evaluate(()=>requests);assert.equal(requests.filter(r=>r.method==='GET').length,2);assert.ok(requests.filter(r=>r.method==='GET').every(r=>r.url==='/api/manifest/operations/sha256%3Afixture/email-receipt'));assert.ok(requests.filter(r=>r.method==='POST').every(r=>r.url==='/api/portals/item/dismiss'));
 assert.deepEqual(errors,[]);console.log('PASS: reply notice → canonical receipt, literal preview, read retry, stale dismissal recovery and viewport bounds.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
