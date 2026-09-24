const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const root=path.join(__dirname,'../web'),key='conversation-'+ '1'.repeat(32),errors=[];let remote={key,slot:'draft',revision:0,value:null},puts=0;
 const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 async function device(){
  const context=await browser.newContext({viewport:{width:390,height:844}}),p=await context.newPage();p.on('pageerror',e=>errors.push(e.message));
  await p.route('https://fixture.test/**',async route=>{
   if(route.request().url().includes('/api/chat/state/')){
    if(route.request().method()==='PUT'){puts++;const b=route.request().postDataJSON();if(b.revision!==remote.revision)return route.fulfill({status:409,json:remote});remote={...remote,revision:remote.revision+1,value:b.value};}
    return route.fulfill({json:remote});
   }
   return route.fulfill({contentType:'text/html',body:'<main><div id="chatComposer" class="chat-composer"><textarea></textarea></div></main>'});
  });await p.goto('https://fixture.test/');
  for(const name of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
  await p.evaluate(()=>{window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};window.chatDrafts=new Map();window.chatArtifactSelections=new Map();window.chatConversationTasks=new Map();window.chatRecipients=new Map();window.chatDraftKey='alfred/private';window.chatCurSession={};window.renderChatComposer=()=>{};});
  await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/47-chat-state.js'),'utf8')});
  await p.addScriptTag({content:chat.slice(chat.indexOf('function chatSelectedArtifacts('),chat.indexOf('function chatOpenWorkingArtifact('))});
  await p.addScriptTag({content:chat.slice(chat.indexOf('function chatApplySyncedDraft('),chat.indexOf('function chatCaptureSyncedDraft('))});
  await p.addScriptTag({content:chat.slice(chat.indexOf('function chatDraftConflictPreview('),chat.indexOf('const chatRecoveryRefreshes'))});
  await p.evaluate(async key=>{window.state=new ChatDraftState(key,(state,apply)=>{if(apply)chatApplySyncedDraft(chatDraftKey,state.value);chatRenderStateNotice(document.getElementById('chatComposer'),state);});await state.refresh();},key);
  return p;
 }
 const first=await device(),second=await device();
 const local={text:'Same instruction',task:'task-one',recipient:{agent:'codex',model:'local-model'},selection:{id:'record',revision:'a'.repeat(64),title:'Same title',task:'task-one'},files:[{name:'same.txt',hash:'c'.repeat(64)}]};
 const saved={text:'Same instruction',task:'',recipient:{agent:'alfred',model:'saved-model'},selection:{items:[{id:'record',revision:'b'.repeat(64),title:'Same title',explicitArtifacts:true},{id:'project',revision:'d'.repeat(64),title:'Project',explicitArtifacts:true}]},files:[{name:'same.txt',hash:'e'.repeat(64)}]};
 await first.evaluate(async value=>{state.set(value);await state.flush();},local);
 await second.evaluate(async value=>{state.set(value);await state.flush();},saved);
 await second.getByText('Draft changed on another device. Choose which to keep.',{exact:true}).waitFor();
 await second.getByText('View saved draft',{exact:true}).click();await second.getByText('View this device’s draft',{exact:true}).click();
 const previews=second.locator('.chat-draft-preview');
 assert.ok((await previews.nth(0).textContent()).includes('codex · local-model'));assert.ok((await previews.nth(0).textContent()).includes('a'.repeat(64)));
 assert.ok((await previews.nth(1).textContent()).includes('alfred · saved-model'));assert.ok((await previews.nth(1).textContent()).includes('b'.repeat(64)));assert.ok((await previews.nth(1).textContent()).includes('d'.repeat(64)));assert.ok((await previews.nth(1).textContent()).includes('e'.repeat(64)));
 assert.ok((await previews.nth(1).textContent()).includes('No draft task selection'));
 for(const theme of ['light','jarvis'])for(const width of [320,390,1440]){await second.setViewportSize({width,height:844});await second.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);assert.equal(await second.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await second.setViewportSize({width:390,height:400});
 assert.ok(await second.locator('.chat-draft-notice').evaluate(e=>e.clientHeight<=innerHeight/2));
 await second.getByRole('button',{name:'Keep this draft',exact:true}).focus();assert.equal(await second.getByRole('button',{name:'Keep this draft',exact:true}).evaluate(e=>{const r=e.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight;}),true,'choice remains reachable on a short screen');
 await second.setViewportSize({width:390,height:844});await second.locator('.chat-draft-notice').evaluate(e=>e.scrollTop=0);await second.screenshot({path:'/tmp/manifest-draft-conflict-phone.png'});
 await second.getByRole('button',{name:'Keep this draft',exact:true}).click();await second.waitForFunction(()=>!state.dirty&&!state.conflict);assert.deepEqual(remote.value,saved);
 // The other device has an unsynced edit while a no-task, different-target draft arrives.
 await first.evaluate(async()=>{state.set({...state.value,text:'Local new typing'});clearTimeout(state.timer);await state.refresh();});
 await first.getByRole('button',{name:'Use saved draft',exact:true}).click();
 assert.deepEqual(await first.evaluate(()=>({value:state.value,task:chatConversationTasks.get('chat:'+chatDraftKey)||'',refs:chatArtifactPayload(chatArtifactSelections.get('chat:'+chatDraftKey)),recipient:chatRecipients.get(chatDraftKey)})),{value:saved,task:'',refs:{artifacts:saved.selection.items.map(({id,revision})=>({id,revision})),explicitArtifacts:true},recipient:saved.recipient});
 // Restore a legacy task draft first, then a task-less draft: old in-memory task must clear.
 await first.evaluate(local=>{chatApplySyncedDraft(chatDraftKey,local);},local);
 assert.equal(await first.evaluate(()=>chatConversationTasks.get('chat:'+chatDraftKey)),'task-one');
 await first.evaluate(saved=>chatApplySyncedDraft(chatDraftKey,saved),saved);
 assert.equal(await first.evaluate(()=>chatConversationTasks.has('chat:'+chatDraftKey)),false);
 assert.equal(await first.evaluate(()=>document.querySelector('#chatComposer textarea').value),'Same instruction');
 const reopened=await device();assert.deepEqual(await reopened.evaluate(()=>state.value),saved);
 assert.deepEqual(errors,[]);console.log('Two isolated devices: complete conflict previews, exact chosen state, cleared task scope and fresh-device recovery passed');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
