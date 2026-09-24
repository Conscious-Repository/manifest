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
  window.fetch=async url=>({ok:true,json:async()=>url.startsWith('/api/chat/notes?')?{notes:[{Path:'one/same.md'},{Path:'two/same.md'}]}:{path:decodeURIComponent(url.split('path=')[1]),content:'Exact literal <script>source</script>\n'.repeat(35),revision:previewRevision,route:'#/note/two%2Fsame.md'}});
  window.postJSONOk=async(url,payload)=>{retainRequests.push({url,payload});if(failRetain)throw Error('note changed; review its current text before selecting it');return {id:'retained-note',title:'same',revision:payload.revision,version:1,explicitArtifacts:true};};
 });
 const components=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');await p.addScriptTag({content:components.slice(components.indexOf('function typeahead('),components.indexOf('// flattenRockLadder'))});
 const workspace=fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8');await p.addScriptTag({content:workspace.slice(workspace.indexOf('function chatCanSelectNoteContext()'))});
 await p.addScriptTag({content:workspace.slice(workspace.indexOf('function chatContextInputs('),workspace.indexOf('function chatOpenContext('))});
 assert.deepEqual(await p.evaluate(()=>chatContextInputs({turns:[{who:'user',delivery:{context:{explicitArtifacts:true,artifacts:[{id:'note',revision:'a'}]}}},{who:'user',submission:{explicitArtifacts:true,artifacts:[{id:'note',revision:'b'}]}}]}).map(x=>[x.explicitArtifacts,x.artifacts[0].revision])),[[true,'a'],[true,'b']]);
 await p.evaluate(()=>chatOpenNotes());await p.getByRole('combobox').fill('same');await p.getByRole('option').nth(1).waitFor();
 assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'search grants no context');
 await p.getByRole('combobox').press('ArrowUp');await p.getByRole('combobox').press('Enter');
 await p.getByRole('heading',{name:'two/same.md'}).waitFor();assert.equal(await p.locator('.chat-notes-preview script').count(),0);assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'preview grants no context');
 for(const theme of ['light','jarvis'])for(const width of [320,390,1440]){await p.setViewportSize({width,height:900});await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),theme+' '+width+' overflow');}
 await p.setViewportSize({width:390,height:844});await p.screenshot({path:'/tmp/manifest-note-context-phone.png'});
 await p.evaluate(()=>failRetain=true);await p.getByRole('button',{name:'use in this private chat'}).click();await p.getByRole('status').filter({hasText:'note changed'}).waitFor();assert.equal(await p.evaluate(()=>chatArtifactSelections.size),0,'stale retention does not replace context');
 await p.evaluate(()=>failRetain=false);await p.getByRole('button',{name:'use in this private chat'}).click();await p.getByRole('status').filter({hasText:'Selected exact'}).waitFor();
 assert.deepEqual(await p.evaluate(()=>retainRequests.at(-1)),{url:'/api/chat/notes/retain',payload:{path:'two/same.md',revision:'a'.repeat(64)}});
 assert.equal(await p.evaluate(()=>saved.ref.revision),'a'.repeat(64));assert.equal(await p.evaluate(()=>saved.ref.explicitArtifacts),true);
 await p.evaluate(async()=>{window.view=api.getView();api.close();previewRevision='b'.repeat(64);chatOpenNotes();await api.restoreView(view);});
 await p.getByRole('status').filter({hasText:'source changed'}).waitFor();assert.equal(await p.evaluate(()=>saved.ref.revision),'a'.repeat(64),'reopening newer source cannot replace selection');
 await p.evaluate(()=>{api.close();chatCurSession={shared:true};chatOpenNotes();});assert.equal(await p.locator('.chat-notes-inspector').count(),0,'shared target hides note selection');
 assert.deepEqual(errors,[]);console.log('Knowledge note identity, keyboard selection, explicit retention, stale preview, restoration and responsive bounds passed');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
