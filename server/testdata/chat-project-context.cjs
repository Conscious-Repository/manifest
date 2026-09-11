const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({channel:'chrome',headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}});await p.goto('http://localhost:1').catch(()=>{});
 await p.route('https://project.test/**',route=>route.fulfill({contentType:'text/html',body:'<main>Chat</main>'}));await p.goto('https://project.test');
 const root=path.join(__dirname,'../web');for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/47-chat-state.js'),'utf8')});
 await p.evaluate(()=>{
  window.chatWorkstreamURL='/api/chat/state/inbox/workstreams';window.chatOpenId='';window.chatWorkstreams={groups:{one:'Research'},members:{},contexts:{one:{instructions:'Cite primary sources.'}},recordVersion:'source-v1'};
  window.chatApplyWorkstreams=s=>{chatWorkstreams={...s.value,recordVersion:s.record_version};};window.chatRenderWorkstreamFilter=window.renderChatInboxRows=()=>{};
  window.saved={key:'inbox',slot:'workstreams',revision:1,record_version:'source-v1',value:{groups:{one:'Research'},members:{},contexts:{one:{instructions:'Cite primary sources.'}}}};window.drafts={};window.rejectNext=false;
  window.fetch=async(url,opts={})=>{
   if(url===chatWorkstreamURL){if(opts.method==='PUT'){const request=JSON.parse(opts.body);if(window.rejectNext){window.rejectNext=false;saved.value.contexts.one.instructions='An edit from another device.';saved.record_version='source-v2';saved.revision++;return{ok:false,status:409,json:async()=>structuredClone(saved)};}saved={...saved,value:request.value,revision:saved.revision+1,record_version:'source-v3'};}return{ok:true,json:async()=>structuredClone(saved)};}
   const [,key,slot]=url.match(/state\/([^/]+)\/([^/]+)$/);const value=drafts[key]||{key,slot,revision:0,value:null};if(opts.method==='PUT'){const request=JSON.parse(opts.body);drafts[key]={key,slot,revision:value.revision+1,value:request.value};}return{ok:true,json:async()=>structuredClone(drafts[key]||value)};
  };
 });
 const src=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');await p.addScriptTag({content:src.slice(src.indexOf('function chatProjectInitialText('),src.indexOf('function chatChooseWorkstream('))});
 await p.evaluate(()=>chatEditProject('one'));await p.getByRole('textbox',{name:'Project instructions'}).waitFor();await p.waitForFunction(()=>!document.querySelector('.chat-project-edit-actions button:last-child').disabled);
 const notes=p.getByRole('textbox',{name:'Project instructions'});await notes.fill('New instructions, with a durable draft.');await p.getByRole('button',{name:'close',exact:true}).click();
 await p.evaluate(()=>chatEditProject('one'));await p.waitForFunction(()=>document.querySelector('textarea')?.value==='New instructions, with a durable draft.');
 await p.evaluate(()=>window.rejectNext=true);await p.getByRole('button',{name:'save',exact:true}).click();await p.getByText('An edit from another device.',{exact:false}).waitFor();assert.equal(await notes.inputValue(),'New instructions, with a durable draft.');
 await p.getByRole('button',{name:'keep my edits after review'}).click();await p.getByRole('button',{name:'save',exact:true}).click();await p.waitForFunction(()=>document.querySelector('[role=status]').textContent==='Saved');
 const text=await p.evaluate(()=>chatProjectInitialText('one','Find supporting papers.'));assert.ok(text.includes('source-v3'));assert.ok(text.includes('New instructions, with a durable draft.'));assert.ok(text.endsWith('Current request:\nFind supporting papers.'));
 assert.equal(await p.evaluate(()=>chatProjectInitialText('','Standalone request')),'Standalone request');
 for(const width of [320,390,1440]){await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.evaluate(()=>document.documentElement.dataset.theme='jarvis');await p.screenshot({path:'/tmp/manifest-project-context-phone.png'});
 await notes.fill('Unfinished edit against version three.');await p.getByRole('button',{name:'close',exact:true}).click();
 await p.evaluate(()=>{saved.record_version='source-v4';saved.revision++;saved.value.contexts.one.instructions='New saved version while editor was closed.';chatEditProject('one');});
 await p.getByText('New saved version while editor was closed.',{exact:false}).waitFor();assert.equal(await p.getByRole('button',{name:'save',exact:true}).isDisabled(),true);assert.equal(await notes.inputValue(),'Unfinished edit against version three.');
 console.log('PASS project context: draft recovery, source conflict review, explicit save, snapshot provenance, standalone isolation, responsive bounds.');
}finally{await b.close()}})().catch(e=>{console.error(e);process.exit(1)});
