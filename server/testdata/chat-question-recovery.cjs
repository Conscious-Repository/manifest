const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),snapshots=new Map(),receipts=new Map(),errors=[];let sends=0;
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('https://fixture.test/**',async route=>{
  const req=route.request(),url=new URL(req.url());
  if(url.pathname.startsWith('/api/chat/state/')){
   const parts=url.pathname.split('/');let s=snapshots.get(url.pathname)||{key:parts.at(-2),slot:parts.at(-1),revision:0,value:null};
   if(req.method()==='PUT'){const b=req.postDataJSON();if(b.revision!==s.revision)return route.fulfill({status:409,json:s});s={...s,revision:s.revision+1,value:b.value};snapshots.set(url.pathname,s);}
   return route.fulfill({json:s});
  }
  if(url.pathname.endsWith('/input')){
   const b=req.postDataJSON();const saved=[...snapshots.values()].find(s=>s.value?.requestId===b.requestId);
   assert.equal(saved.value.locked,true);assert.equal(saved.value.text,b.questionAnswers[0].answer);
   sends++;receipts.set(b.requestId,{id:b.requestId,state:'sent',questionAnswers:b.questionAnswers});return route.abort('failed');
  }
  if(url.pathname.endsWith('/delivery')){const receipt=receipts.get(url.searchParams.get('request'));return route.fulfill(receipt?{json:{delivery:receipt}}:{status:404,body:'missing'});}
  return route.fulfill({contentType:'text/html',body:'<main><div id="chatComposer"><textarea aria-label="Composer"></textarea></div></main>'});
 });
 async function boot(){
  await page.goto('https://fixture.test/');
  for(const f of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
  await page.evaluate(()=>{
   window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};
   window.chatTermOpen={id:'abcdef12',se:{backend:'herdr'},questions:[{revision:'a'.repeat(64),id:'native-question-one',title:'Which approach should I use?',options:['First','Second'],state:'pending',async:true}]};window.chatTermRequestFinalTail=()=>{};window.chatOpenTerminalPane=()=>{};
  });
  const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');await page.addScriptTag({content:chat.slice(chat.indexOf('function chatDraftConflictPreview('),chat.indexOf('const chatRecoveryRefreshes'))});
  for(const f of ['47-chat-state','48-chat-questions'])await page.addScriptTag({content:fs.readFileSync(path.join(root,'js',f+'.js'),'utf8')});
  await page.evaluate(()=>chatQuestionPanel(chatTermOpen));await page.waitForFunction(()=>!document.querySelector('.chat-question-fields').disabled||document.querySelector('.chat-question-status').textContent==='Answer sent');
 }
 await boot();await page.getByLabel('Your answer',{exact:true}).fill('Keep this unfinished answer');
 await page.waitForFunction(()=>[...chatQuestionDrafts.values()][0].state.dirty===false);
 const field=await page.getByLabel('Your answer',{exact:true}).elementHandle();await page.evaluate(()=>chatQuestionPanel(chatTermOpen));assert.equal(await field.evaluate(e=>e===document.activeElement),true,'poll keeps focused answer');
 await boot();assert.equal(await page.getByLabel('Your answer',{exact:true}).inputValue(),'Keep this unfinished answer');assert.equal(sends,0);
 const savedEntry=[...snapshots.entries()][0];snapshots.set(savedEntry[0],{...savedEntry[1],revision:savedEntry[1].revision+1,value:{...savedEntry[1].value,text:'Answer edited on another device'}});
 await page.getByLabel('Your answer',{exact:true}).fill('My competing edit');
 await page.getByText('Draft changed on another device. Choose which to keep.',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'send answer',exact:true}).isDisabled(),true);assert.equal(sends,0);
 await page.getByRole('button',{name:'Use saved draft',exact:true}).click();
 assert.equal(await page.getByLabel('Your answer',{exact:true}).inputValue(),'Answer edited on another device');
 await page.getByRole('button',{name:'send answer',exact:true}).click();await page.getByText('Answer sent',{exact:true}).waitFor();assert.equal(sends,1);
 await boot();await page.getByText('Answer sent',{exact:true}).waitFor();assert.equal(sends,1,'reload reconciles receipt without resending');assert.equal(await page.getByLabel('Your answer',{exact:true}).isDisabled(),true);
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 await page.screenshot({path:'/tmp/manifest-question-recovery-phone.png'});
 assert.deepEqual(errors,[]);console.log('PASS: typed answer, focus, persistence, lost response and reload receipt reconciliation at 390px');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
