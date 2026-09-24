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
 await page.evaluate(()=>{window.fixture={record:{operationId:'sha256:fixture',policy:'human_approval',status:'succeeded',arguments:{email:{from:'ben@ooda.group',to:['contractor@example.com'],subject:'Bid request',body:'Please quote'},domain:'ooda'},result:{deliveryStatus:'sent'},emailWatch:{enabled:true,total:1,checkedAt:'2026-09-11T12:00:00Z',replies:[{from:'Contractor',at:'2026-09-11T11:00:00Z',body:'<script>literal message</script>',clipped:true}]}},proposal:{id:'approval-fixture',action:'email',body:'fixture'}};document.getElementById('root').append(manifestOperationCard(fixture));});
 await page.getByText('Email sent',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Approve',exact:true}).count(),0);
 await page.getByText(/Contractor ·/).click();await page.getByText('<script>literal message</script>',{exact:true}).waitFor();assert.equal(await page.locator('script').filter({hasText:'literal message'}).count(),0);
 await page.getByRole('button',{name:'Stop tracking',exact:true}).click();assert.deepEqual(await page.evaluate(()=>actions),[{url:'/api/manifest/operations/sha256%3Afixture/email-watch',body:{enabled:false}}]);
 await page.getByText('Continues until you stop tracking.',{exact:true}).waitFor();
 await page.evaluate(()=>{fixture.record.emailWatch={enabled:false,stopAfterReply:true,stoppedReason:'reply_found',replies:[]};document.getElementById('root').replaceChildren(manifestOperationCard(fixture));});
 await page.getByText('Tracking stopped · reply found',{exact:true}).waitFor();
 const rule=page.getByRole('combobox',{name:'Reply tracking end condition'});
 assert.equal(await rule.inputValue(),'reply');
 await page.getByRole('button',{name:'Track replies',exact:true}).click();
 assert.deepEqual((await page.evaluate(()=>actions)).at(-1).body,{enabled:true,stopAfterReply:true});
 await page.evaluate(()=>{window.postJSONOk=async(url,body)=>{actions.push({url,body});throw new Error('offline')};document.getElementById('root').replaceChildren(manifestOperationCard(fixture));});
 await rule.selectOption('manual');await page.getByRole('button',{name:'Track replies',exact:true}).click();
 assert.deepEqual((await page.evaluate(()=>actions)).at(-1).body,{enabled:true,stopAfterReply:false});
 assert.equal(await rule.isEnabled(),true);assert.equal(await rule.inputValue(),'manual');assert.equal(await page.getByRole('button',{name:'Track replies',exact:true}).isEnabled(),true);
 await page.evaluate(()=>{fixture.record.emailWatch={enabled:true,stopAfterReply:true,replies:[]};document.getElementById('root').replaceChildren(manifestOperationCard(fixture));});
 await page.getByText('Stops when a reply is found. You can stop sooner.',{exact:true}).waitFor();
 assert.equal(await rule.count(),0);
 for(const width of [320,390,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);assert.equal(await page.locator('.chat-email-watch .sprt-sub').first().evaluate(e=>getComputedStyle(e).whiteSpace),'normal');}
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'/tmp/manifest-email-watch-end.png'});
 assert.deepEqual(errors,[]);console.log('PASS: sent email reply previews, explicit end rules, stop/restart, failed-request recovery and viewport bounds.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
