const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
 const page=await browser.newPage({viewport:{width:390,height:844}});const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());
 await page.setContent('<main id="root"></main>');
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.renderMarkdown=text=>el('p','',text);window.actions=[];window.postJSONOk=async(url,body)=>actions.push({url,body});window.loadFeed=()=>{};window.showToast=()=>{};
 });
 const source=fs.readFileSync(path.join(__dirname,'../web/js/55-approvals.js'),'utf8');await page.addScriptTag({content:source.slice(source.indexOf('function manifestOperationCard'),source.indexOf('let manifestRevealTarget'))});
 await page.evaluate(()=>document.getElementById('root').append(manifestOperationCard({record:{operationId:'sha256:fixture',policy:'human_approval',status:'succeeded',arguments:{email:{from:'ben@ooda.group',to:['contractor@example.com'],subject:'Bid request',body:'Please quote'},domain:'ooda'},result:{deliveryStatus:'sent'},emailWatch:{enabled:true,total:1,checkedAt:'2026-09-11T12:00:00Z',replies:[{from:'Contractor',at:'2026-09-11T11:00:00Z',body:'<script>literal message</script>',clipped:true}]}},proposal:{id:'approval-fixture',action:'email',body:'fixture'}})));
 await page.getByText('Email sent',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Approve',exact:true}).count(),0);
 await page.getByText(/Contractor ·/).click();await page.getByText('<script>literal message</script>',{exact:true}).waitFor();assert.equal(await page.locator('script').filter({hasText:'literal message'}).count(),0);
 await page.getByRole('button',{name:'Stop tracking',exact:true}).click();assert.deepEqual(await page.evaluate(()=>actions),[{url:'/api/manifest/operations/sha256%3Afixture/email-watch',body:{enabled:false}}]);
 assert.deepEqual(errors,[]);console.log('PASS: sent email reply status, literal previews, owner stop control and no approval/send action.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
