const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),attempts=[],errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('http://localhost:7342/**',async route=>{
  if(route.request().method()==='POST'){
   attempts.push(route.request().postDataJSON());
   if(attempts.length===1){await route.abort('connectionfailed');return;}
   await route.fulfill({json:{id:'new-native',agent:'claude',model:'fable'}});return;
  }
  await route.fulfill({contentType:'text/html',body:'<main></main>'});
 });
 const all=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
 const source=all.slice(all.indexOf('function chatAddSharedTerminal('),all.indexOf('function chatStartRelated('));
 async function mount(title){
  await page.goto('http://localhost:7342/');
  await page.evaluate(()=>{
   window.el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text)n.textContent=text;return n};
   window.reviewDialog=(title,build)=>{const d=document.createElement('dialog'),body=el('div'),actions=el('div');d.append(el('h2','',title),body,actions);document.body.append(d);d.showModal();build({body,actions,close:()=>d.remove()});};
   window.chatTermKinds={claude:'Claude Code',codex:'Codex'};window.chatRecall=()=>'';
   window.chatBaseFor=agent=>'/api/agents/chat/'+agent+'/sessions';
   window.postJSONOk=async(url,body)=>{const r=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(!r.ok)throw Error('failed');return r.json()};
   window.chatRecipients=new Map();window.chatAgent='kairos';window.chatOpenId='shared-thread';window.chatCaptureSyncedDraft=()=>{};window.refetchChatSession=async()=>{};
  });
  await page.addScriptTag({content:source});
  await page.evaluate(title=>chatAddSharedTerminal({id:'shared-thread',title,task:'PRIVATE_TASK_MUST_NOT_PASS'},'kairos'),title);
 }
 await mount('Original title');
 await page.getByRole('button',{name:'Add to shared conversation'}).click();
 await page.getByRole('status').filter({hasText:/fetch|network|Failed/i}).waitFor();
 assert.equal(attempts.length,1);assert.equal(attempts[0].task,undefined);assert.equal(attempts[0].mode,'continue');
 await mount('Renamed while offline');
 await page.getByRole('button',{name:'Add to shared conversation'}).click();
 await page.waitForFunction(()=>!document.querySelector('dialog'));
 assert.deepEqual(attempts[0],attempts[1],'reload changed creation identity/content');
 const result=await page.evaluate(()=>({recipient:chatRecipients.get('kairos/shared-thread'),pending:Object.keys(localStorage).filter(k=>k.startsWith('manifest.sharedTerminal.'))}));
 assert.equal(result.recipient.id,'new-native');assert.equal(result.pending.length,0);assert.deepEqual(errors,[]);
 console.log('Shared runtime creation: explicit team control, lost-ack/reload exact retry, no private task context, and recipient continuity passed.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1});
