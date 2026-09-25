const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHANNEL?{channel:process.env.PLAYWRIGHT_CHANNEL}:{})});try{
 const page=await browser.newPage({viewport:{width:390,height:844}});const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());
 await page.setContent('<main id="root"></main>');
 const cssDir=path.join(__dirname,'../web/css');await page.addStyleTag({content:fs.readdirSync(cssDir).filter(f=>f.endsWith('.css')).sort().map(f=>fs.readFileSync(path.join(cssDir,f),'utf8')).join('\n')});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.renderMarkdown=text=>el('p','',text);window.actions=[];window.postJSONOk=async(url,body)=>actions.push({url,body});window.loadFeed=()=>{};window.showToast=()=>{};
 });
 const source=fs.readFileSync(path.join(__dirname,'../web/js/55-approvals.js'),'utf8');await page.addScriptTag({content:source.slice(source.indexOf('function manifestOperationCard'),source.indexOf('let manifestRevealTarget'))});
 await page.evaluate(()=>{window.fixture={record:{operationId:'sha256:fixture',policy:'human_approval',status:'partial',arguments:{email:{from:'ben@ooda.group',to:['contractor@example.com'],subject:'Bid request',body:'Please quote'},domain:'ooda'},result:{deliveryStatus:'sent'},emailWatch:{enabled:true,total:1,checkedAt:'2026-09-11T12:00:00Z',replies:[{from:'Contractor',at:'2026-09-11T11:00:00Z',body:'<script>literal message</script>',clipped:true}]}},proposal:{id:'approval-fixture',action:'email',body:'fixture'}};document.getElementById('root').append(manifestOperationCard(fixture));});

 await page.evaluate(()=>{window.refreshes=[];window.chatOpenId='chat-one';window.chatTaskID='task-one';window.refetchChatSession=id=>refreshes.push(['chat',id]);window.renderTaskChat=(id,force)=>refreshes.push(['task',id,force]);window.loadTodos=()=>refreshes.push(['todos']);window.loadFeed=()=>refreshes.push(['feed']);window.addEventListener('manifest-approval-updated',e=>refreshes.push(['event',e.detail.id]));});
 await page.getByRole('button',{name:'Check delivery',exact:true}).waitFor();
 await page.evaluate(()=>{window.postJSONOk=async(url,body)=>{actions.push({url,body});throw new Error('Delivery remains uncertain')};});
 await page.getByRole('button',{name:'Check delivery',exact:true}).click();
 await page.getByRole('status').filter({hasText:'Delivery remains uncertain'}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Check delivery',exact:true}).isEnabled(),true);
 assert.deepEqual(await page.evaluate(()=>refreshes),[]);
 await page.evaluate(()=>{window.postJSONOk=async(url,body)=>{actions.push({url,body});return {record:{...fixture.record,status:'succeeded'}}};});
 await page.getByRole('button',{name:'Check delivery',exact:true}).click();
 await page.getByText('Email sent',{exact:true}).waitFor();
 assert.deepEqual(await page.evaluate(()=>refreshes),[['chat','chat-one'],['task','task-one',true],['todos'],['event','approval-fixture'],['feed']]);
 assert.equal(await page.getByRole('button',{name:'Check delivery',exact:true}).count(),0);
 assert.deepEqual(await page.evaluate(()=>actions),[1,2].map(()=>({url:'/api/manifest/operations/sha256%3Afixture/email-reconcile',body:{}})));
 for(const status of ['pending_approval','failed','succeeded']){await page.evaluate(status=>{fixture.record.status=status;document.getElementById('root').replaceChildren(manifestOperationCard(fixture));},status);assert.equal(await page.getByRole('button',{name:'Check delivery',exact:true}).count(),0);}
 await page.evaluate(()=>{fixture.record.status='executing';document.getElementById('root').replaceChildren(manifestOperationCard(fixture));});
 for(const width of [320,390,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);}
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'/tmp/manifest-email-reconcile-phone.png'});
 assert.deepEqual(errors,[]);console.log('PASS: explicit recovery, failed-read retry, canonical replacement, status gating and responsive bounds');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
