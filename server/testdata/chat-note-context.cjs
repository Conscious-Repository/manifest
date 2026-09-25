const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const p=await browser.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({body:'<main><div id="host" style="height:780px"></div><div id="chatComposer"><textarea></textarea></div></main>',contentType:'text/html'}));await p.goto('https://fixture.test/');
 for(const name of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
 await p.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};window.inputEl=placeholder=>{const e=document.createElement('input');e.placeholder=placeholder;return e;};
  window.chatOpenId='private-one';window.chatAgent='alfred';window.chatIsPortal=()=>false;window.chatIsTerm=()=>false;window.chatCurSession={};window.chatRosterEntry=()=>({durableSend:true});window.chatArtifactSelections=new Map();
  window.chatRenderArtifactContext=(task,key)=>{window.rendered={task,key,ref:chatArtifactSelections.get(key)};};window.chatCaptureSyncedDraft=key=>{window.saved={key,ref:chatArtifactSelections.get('chat:'+key)};};
  window.chatEnsureWorkspace=()=>({tab:(key,title,build)=>{window.api=build(document.getElementById('host'),()=>{});return api;}});
  window.previewRevision='a'.repeat(64);window.retainRequests=[];window.failRetain=false;
  window.fetch=async url=>{const params=new URL(url,location.href).searchParams,kind=params.get('kind');const ids=kind==='note'?['one/same.md','two/same.md']:kind==='task'?['inbox/first','work/second']:kind==='person'?['alice','crm/alice']:kind==='organization'?['acme','acme labs']:kind==='candidate'?['cand/one','cand/two']:kind==='project'?['project/one','project/two']:['work/annual','work/stage'];const record=id=>({kind,id,title:kind==='note'?id:'Same title',detail:id,route:'#/'+(kind==='note'?'note':(kind==='person'||kind==='organization')?'contacts':kind==='project'?'chat/project':kind==='candidate'?'aion/recruiting/candidate':kind+'s')+'/'+encodeURIComponent(id)});return {ok:true,json:async()=>url.includes('/preview?')?{record:record(params.get('id')),content:'Exact literal <script>source</script>\n'.repeat(35),revision:previewRevision}:{records:ids.map(record)}};};
  window.postJSONOk=async(url,payload)=>{retainRequests.push({url,payload});if(failRetain)throw Error('note changed; review its current text before selecting it');return {id:'retained-note',title:'same',revision:payload.revision,version:1,explicitArtifacts:true};};
 });
 const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');await p.addScriptTag({content:chat.slice(chat.indexOf('function chatSelectedArtifacts('),chat.indexOf('function chatOpenWorkingArtifact('))});
 const components=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');await p.addScriptTag({content:components.slice(components.indexOf('function typeahead('),components.indexOf('// flattenRockLadder'))});
 const workspace=fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8');await p.addScriptTag({content:workspace.slice(workspace.indexOf('function chatCanSelectNoteContext()'))});
 await p.addScriptTag({content:workspace.slice(workspace.indexOf('function chatContextInputs('),workspace.indexOf('function chatOpenContext('))});
 assert.deepEqual(await p.evaluate(()=>chatContextInputs({turns:[{who:'user',delivery:{context:{explicitArtifacts:true,artifacts:[{id:'note',revision:'a'}]}}},{who:'user',submission:{explicitArtifacts:true,artifacts:[{id:'note',revision:'b'}]}}]}).map(x=>[x.explicitArtifacts,x.artifacts[0].revision])),[[true,'a'],[true,'b']]);
 await p.evaluate(()=>chatOpenNotes());await p.getByRole('combobox',{name:'Find a record'}).fill('same');await p.locator('[role=option]').nth(1).waitFor();
 assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'search grants no context');
 await p.getByRole('combobox',{name:'Find a record'}).press('ArrowUp');await p.getByRole('combobox',{name:'Find a record'}).press('Enter');
 await p.getByRole('heading',{name:'two/same.md'}).waitFor();assert.equal(await p.locator('.chat-notes-preview script').count(),0);assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'preview grants no context');
 for(const theme of ['light','jarvis'])for(const width of [320,390,1440]){await p.setViewportSize({width,height:900});await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),theme+' '+width+' overflow');}
 await p.setViewportSize({width:390,height:844});await p.screenshot({path:'/tmp/manifest-note-context-phone.png'});
 await p.evaluate(()=>failRetain=true);await p.getByRole('button',{name:'use in this private chat'}).click();await p.getByRole('status').filter({hasText:'note changed'}).waitFor();assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'stale retention does not replace context');
 await p.evaluate(()=>failRetain=false);await p.getByRole('button',{name:'use in this private chat'}).click();await p.getByRole('status').filter({hasText:'Selected exact'}).waitFor();
 assert.deepEqual(await p.evaluate(()=>retainRequests.at(-1)),{url:'/api/chat/records/retain',payload:{kind:'note',id:'two/same.md',revision:'a'.repeat(64)}});
 assert.equal(await p.evaluate(()=>saved.ref.revision),'a'.repeat(64));assert.equal(await p.evaluate(()=>saved.ref.explicitArtifacts),true);
 await p.evaluate(async()=>{window.view=api.getView();api.close();previewRevision='b'.repeat(64);chatOpenNotes();await api.restoreView(view);});
 await p.getByRole('status').filter({hasText:'source changed'}).waitFor();assert.equal(await p.evaluate(()=>saved.ref.revision),'a'.repeat(64),'reopening newer source cannot replace selection');

 // Kind changes clear the previous preview and use the selected record ID,
 // independently of equal titles and whatever note was selected before.
 for(const kind of ['task','goal','person','project','candidate','organization']){
  await p.getByLabel('Record kind').selectOption(kind);assert.equal(await p.locator('.chat-notes-preview h3').count(),0);
  await p.getByRole('combobox',{name:'Find a record'}).fill('same');await p.locator('[role=option]').nth(1).waitFor();await p.getByRole('combobox',{name:'Find a record'}).press('ArrowUp');await p.getByRole('combobox',{name:'Find a record'}).press('Enter');
  await p.getByRole('heading',{name:'Same title'}).waitFor();await p.getByRole('button',{name:'use in this private chat'}).click();await p.getByRole('status').filter({hasText:'Selected exact'}).waitFor();
  if(kind==='project'){await p.locator('.chat-notes-preview').evaluate(e=>e.scrollTop=0);await p.screenshot({path:'/tmp/manifest-project-context-phone.png'});}
  if(kind==='organization'){await p.locator('.chat-notes-preview').evaluate(e=>e.scrollTop=0);await p.screenshot({path:'/tmp/manifest-organization-context-phone.png'});}
  if(kind==='candidate'){await p.locator('.chat-notes-preview').evaluate(e=>e.scrollTop=0);await p.screenshot({path:'/tmp/manifest-candidate-context-phone.png'});}
  if(kind==='person'){await p.locator('.chat-notes-preview').evaluate(e=>e.scrollTop=0);await p.screenshot({path:'/tmp/manifest-person-context-phone.png'});}
  const id=kind==='task'?'work/second':kind==='person'?'crm/alice':kind==='project'?'project/two':kind==='candidate'?'cand/two':kind==='organization'?'acme labs':'work/stage';assert.deepEqual(await p.evaluate(()=>retainRequests.at(-1).payload),{kind,id,revision:'b'.repeat(64)});
  await p.evaluate(async()=>{const view=api.getView();api.close();chatOpenRecords();await api.restoreView(view);});assert.equal(await p.getByLabel('Record kind').inputValue(),kind);assert.equal(await p.getByRole('link',{name:'open source '+kind}).getAttribute('href'),'#/'+((kind==='person'||kind==='organization')?'contacts':kind==='project'?'chat/project':kind==='candidate'?'aion/recruiting/candidate':kind+'s')+'/'+encodeURIComponent(id));
 }
 // An older-kind search must not repopulate the picker after switching kinds.
 await p.evaluate(()=>{window.realFetch=fetch;window.fetch=url=>url.includes('&q=delayed')?new Promise(resolve=>{window.finishDelayed=()=>resolve({ok:true,json:async()=>({records:[{kind:'person',id:'old-kind',title:'stale result',detail:'old'}]})});}):realFetch(url);});
 await p.getByRole('combobox',{name:'Find a record'}).fill('delayed');await p.waitForFunction(()=>typeof finishDelayed==='function');await p.getByLabel('Record kind').selectOption('task');await p.evaluate(()=>finishDelayed());await p.waitForTimeout(50);assert.equal(await p.locator('[role=option]').count(),0,'stale kind options stay hidden');
 await p.evaluate(()=>{api.close();chatCurSession={shared:true};chatOpenNotes();});assert.equal(await p.locator('.chat-notes-inspector').count(),0,'shared target hides note selection');
 // The canonical source page opens the exact project, with no chat creation.
 await p.evaluate(async()=>{
  window.chatCurSession={};window.chatRouteVersion=1;window.els={chatView:{hidden:false}};
  window.chatLoadInbox=async()=>{};window.renderChatRail=()=>{};window.chatEditProject=id=>window.editedProject=id;
  document.getElementById('host').id='chatTranscript';window.fetch=realFetch;
  await chatShowProjectRecord('project/two',1);
 });
 await p.getByRole('heading',{name:'Same title'}).waitFor();
 assert.equal(await p.locator('.chat-project-record').textContent().then(s=>s.includes('Project · project/two')),true);
 await p.getByRole('button',{name:'edit project',exact:true}).click();assert.equal(await p.evaluate(()=>editedProject),'project/two');
 assert.equal(await p.locator('#chatComposer textarea').count(),0,'source page has no send composer');
 for(const theme of ['light','jarvis'])for(const width of [320,390,1440]){await p.setViewportSize({width,height:844});await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.evaluate(async()=>{window.fetch=async()=>({ok:false,text:async()=>'Project no longer available'});await chatShowProjectRecord('removed',1);});
 await p.getByRole('status').filter({hasText:'Project no longer available'}).waitFor();
 await p.evaluate(()=>{window.fetch=()=>new Promise(resolve=>window.finishProject=()=>resolve({ok:true,json:async()=>({record:{id:'old',title:'stale project'},content:'stale'})}));window.pendingProject=chatShowProjectRecord('old',1);});
 await p.evaluate(async()=>{chatRouteVersion=2;document.getElementById('chatTranscript').textContent='New destination';finishProject();await pendingProject;});
 assert.equal(await p.locator('#chatTranscript').textContent(),'New destination','late source preview cannot overwrite another route');
 const recruiting=fs.readFileSync(path.join(root,'js/96-aion-recruiting.js'),'utf8');await p.addScriptTag({content:recruiting.slice(recruiting.indexOf('function recApplyRoute('),recruiting.indexOf('function recNav('))});await p.evaluate(()=>{recApplyRoute('candidate/cand%2Ftwo');});assert.deepEqual(await p.evaluate(()=>({id:recSel,view:recView,cut:recCut,origin:recOrigin,query:recQuery,role:recRole})),{id:'cand/two',view:'board',cut:'all',origin:'both',query:'',role:null});
 assert.deepEqual(errors,[]);console.log('Record identity, keyboard selection, explicit retention, stale preview, kind restoration, stale search and responsive bounds passed');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
