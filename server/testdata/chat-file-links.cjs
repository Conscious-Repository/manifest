const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true,channel:'chromium'});try{
 const p=await b.newPage({viewport:{width:1440,height:900}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({body:'<main class="chat-shell" style="height:850px"><aside class="chat-rail">Chats</aside><section class="chat-main"><header></header><div id="chatComposer"><textarea></textarea></div></section></main>',contentType:'text/html'}));await p.goto('https://fixture.test/#/chat/a/codex/abcdef12');
 for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};window.renderMarkdown=t=>el('pre','',t);window.chatRenderStateNotice=()=>{};
  window.chatWorkspace=null;window.chatTaskID='';window.chatCurrentProject=()=>'';window.chatOpenId='abcdef12';window.chatAgent='codex';window.chatIsTerm=()=>true;window.chatIsPortal=()=>false;window.chatRosterEntry=()=>({durableSend:true});window.chatTermOpen={se:{id:'abcdef12',kind:'codex',name:'Parent',model:'gpt-6-astra',cwd:'/project'}};window.chatRoster=[{name:'alfred',label:'Alfred',enabled:true,durableSend:true,model:'selected'}];window.chatTermKinds={codex:'Codex',claude:'Claude'};window.chatTermEnabled=true;window.chatRecipients=new Map();window.chatArtifactSelections=new Map();window.chatRenderArtifactContext=()=>{};window.chatCaptureSyncedDraft=()=>{};window.chatLoadWorkstreams=async()=>{};window.chatWorkstreamMember=key=>key==='terminal:codex/abcdef12'?'work-1':'';window.chatSaveWorkstream=async(...args)=>{window.savedWorkstream=args;};window.chatBaseFor=()=>'/api/agents/chat/alfred/sessions';window.postJSONOk=async(endpoint,payload)=>{window.created={endpoint,payload};return {id:'side1234',agent:payload.agent,conversation:{route:'#/chat/a/'+payload.agent+'/side1234'}};};
  window.a={id:'plan',title:'Plan',ref:'plan.md',content:'original',head:'a'.repeat(64),revisions:[{n:1,hash:'a'.repeat(64)}]};window.fetch=async()=>({ok:true,json:async()=>structuredClone(a)});
 });
 const components=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');await p.addScriptTag({content:components.slice(components.indexOf('function artifactLineChanges'),components.indexOf('// A compact, keyboard-accessible'))});
 await p.addScriptTag({content:components.slice(components.indexOf('function artifactWorkingChangesView('))});
 const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');await p.addScriptTag({content:chat.slice(chat.indexOf('let chatPaneResizeCleanup='))});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8')});

 const note=fs.readFileSync(path.join(root,'js/70-note.js'),'utf8');
 await p.addScriptTag({content:note.slice(note.indexOf('function renderMarkdown('),note.indexOf('// ---- [[wikilink]] autocomplete'))});
 await p.addScriptTag({content:components.slice(components.indexOf('function attachmentWorkspace('),components.indexOf('// Exact line comparison'))});
 await p.evaluate(()=>{
  window.requests=[];window.fetch=async(url)=>{requests.push(url);if(url==='/api/files/hosts')return new Response(JSON.stringify({hosts:[{name:'metis',local:true}]}));
   if(url.includes('missing.md'))return new Response('not a readable file',{status:404});
   return new Response('# Full audit\n\nReadable findings. [Related](./related.md)\n\n- [ ] Leave untouched',{headers:{'Content-Type':'text/plain'}});};
  const row=el('div');row.append(renderMarkdown('[Full audit](/home/benjamin/src/manifest/docs/audits/full%20audit.md:12) [Missing](/home/benjamin/missing.md) [External](https://example.com/a.md)','',{readOnly:true}));document.querySelector('.chat-main').prepend(row);
 });
 await p.getByRole('link',{name:'Full audit',exact:true}).click();
 await p.getByRole('heading',{name:'Full audit',exact:true}).waitFor();
 assert.equal(p.url(),'https://fixture.test/#/chat/a/codex/abcdef12');
 assert.equal(await p.getByRole('tab',{name:'full audit.md',exact:true}).count(),1);
 assert.equal(await p.locator('.artifact-workspace-body input').isDisabled(),true);
 assert.equal(await p.evaluate(()=>requests.at(-1)),'/api/files/read?host=metis&path=%2Fhome%2Fbenjamin%2Fsrc%2Fmanifest%2Fdocs%2Faudits%2Ffull%20audit.md');
 await p.getByRole('link',{name:'Full audit',exact:true}).click();
 assert.equal(await p.getByRole('tab').count(),1,'repeat click reuses tab');
 await p.getByRole('link',{name:'Related',exact:true}).click();
 await p.getByRole('tab',{name:'related.md',exact:true}).waitFor();
 await p.getByRole('link',{name:'Missing',exact:true}).click();
 await p.getByText('not a readable file',{exact:true}).waitFor();
 assert.equal(await p.getByRole('link',{name:'External',exact:true}).getAttribute('target'),'_blank');
 await p.evaluate(()=>{
  for(const path of ['https://example.com/a.md','//evil/a.md','javascript:alert(1)','/api/files/read','file://remote/a.md'])if(chatLocalFilePath(path,'/project')!==null)throw Error(path);
  if(chatLocalFilePath('<file:///home/test/a.md#L12>')!=='/home/test/a.md')throw Error('line anchor');
 });
 await p.setViewportSize({width:390,height:844});
 await p.getByRole('button',{name:'Hide workspace',exact:true}).click();
 assert.equal(await p.getByLabel('Chat workspace').isVisible(),false);
 assert.equal(await p.locator('#chatComposer textarea').isVisible(),true);
 assert.deepEqual(errors,[]);console.log('Local file links: preview, reuse, relative links, errors, mobile, URL filtering passed');
 }finally{await b.close();}})().catch(e=>{console.error(e);process.exit(1);});
