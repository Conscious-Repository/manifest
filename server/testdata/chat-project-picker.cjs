const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
 const page=await browser.newPage();await page.route('https://picker.test/**',r=>r.fulfill({contentType:'text/html',body:'<div id="chatHeadActions"></div>'}));await page.goto('https://picker.test');
 const root=path.join(__dirname,'../web');await page.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await page.evaluate(()=>{
  window.chatWorkstreams={groups:{},members:{}};window.chatWorkstreamFilter='all';window.chatPendingProject='';window.chatAgent='codex';window.chatTermEnabled=true;window.chatRoster=[];window.chatTermKinds={codex:'Codex'};
  window.chatTermSessions=[{id:'original',kind:'codex',cwd:'/home/benjamin/src/manifest'},{id:'other',kind:'codex',cwd:'/elsewhere/manifest'},{id:'home',kind:'codex',cwd:'/home/benjamin'}];
  window.chatIsTerm=a=>a==='codex';window.chatInboxKey=e=>'terminal:'+e.agent+'/'+e.session.id;window.chatWorkstreamURL='/projects';window.showToast=e=>{throw Error(e)};
  window.saved={revision:0,record_version:'v0',value:chatWorkstreams};window.conflict=true;
  window.chatApplyWorkstreams=s=>{chatWorkstreams=s.value};window.fetch=async(url,opts={})=>{if(opts.method==='PUT'){if(conflict){conflict=false;return{ok:false,status:409}}const body=JSON.parse(opts.body);saved={revision:saved.revision+1,record_version:'v1',value:body.value};}return{ok:true,json:async()=>structuredClone(saved)}};
 });
 const src=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');await page.addScriptTag({content:src.slice(src.indexOf('function chatFolderProjects('),src.indexOf('function chatRenderWorkstreamFilter('))+src.slice(src.indexOf('function renderChatHeadActions('),src.indexOf('// ---- rail: agent sections'))});
 await page.evaluate(()=>renderChatHeadActions());await page.getByRole('button',{name:'New chat',exact:true}).click();
 const picker=page.getByRole('combobox',{name:'New chat project'});assert.deepEqual(await picker.locator('option').allTextContents(),['Standalone chat','manifest · /home/benjamin/src/manifest','manifest · /elsewhere/manifest']);
 await picker.selectOption('folder:local:/home/benjamin/src/manifest');await page.getByRole('button',{name:'Codex',exact:true}).click();await page.waitForFunction(()=>location.hash==='#/chat/a/codex/new');
 const result=await page.evaluate(()=>({id:chatPendingProject,state:saved.value,cwd:localStorage.getItem('manifest.chatTermCwd.codex')}));assert.equal(result.state.groups[result.id],'manifest');assert.equal(result.state.members['terminal:codex/original'],result.id);assert.equal(result.state.members['terminal:codex/other'],undefined);assert.equal(result.cwd,'/home/benjamin/src/manifest');
 await page.evaluate(()=>{chatWorkstreams=structuredClone(saved.value)});assert.equal(await page.evaluate(()=>chatResolveProject('folder:local:/home/benjamin/src/manifest')),result.id);assert.equal(await page.evaluate(()=>Object.keys(saved.value.groups).length),1);
 await page.getByRole('button',{name:'New chat',exact:true}).click();assert.ok((await picker.locator('option').allTextContents()).includes('manifest'));await picker.selectOption('');await page.getByRole('button',{name:'Codex',exact:true}).click();assert.equal(await page.evaluate(()=>chatPendingProject),'');
 console.log('PASS folder project picker: selectable rail folders, distinct same-name paths, CAS retry, durable membership, folder prefill, no duplicates, standalone.');
 }finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
