// The new chat's project chip (49-chat-newchat.js) over the real project
// helpers (48-chat.js): rail folders are selectable projects, same-named
// folders stay distinct by path, choosing one survives a concurrent write
// (CAS retry), records durable membership, pre-fills a coding agent's
// folder chip, never duplicates a project, and "No project" is standalone.
// (Was the "New chat" modal's project select; the owner asked for one
// composer-first flow, 2026-09-27. Same behaviours, driven through the chip.)
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chromium',headless:true});try{
 const page=await browser.newPage();await page.route('https://picker.test/**',r=>r.fulfill({contentType:'text/html',body:'<div id="chatHeadActions"></div><div class="chat-main"><div class="chat-composer" id="chatComposer"><textarea></textarea></div></div>'}));await page.goto('https://picker.test');
 const root=path.join(__dirname,'../web');await page.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await page.evaluate(()=>{
  window.chatWorkstreams={groups:{},members:{}};window.chatWorkstreamFilter='all';window.chatPendingProject='';window.chatAgent='codex';window.chatTermEnabled=true;window.chatRoster=[];window.chatTermKinds={codex:'Codex'};
  window.chatTermSessions=[{id:'original',kind:'codex',cwd:'/home/benjamin/src/manifest'},{id:'other',kind:'codex',cwd:'/elsewhere/manifest'},{id:'home',kind:'codex',cwd:'/home/benjamin'}];
  window.chatIsTerm=(a=chatAgent)=>a==='codex';window.chatInboxKey=e=>'terminal:'+e.agent+'/'+e.session.id;window.chatWorkstreamURL='/projects';window.showToast=e=>{throw Error(e)};
  window.chatOpenId='';window.chatTaskID='';window.chatIsPortal=()=>false;window.chatRecall=k=>localStorage.getItem(k)||'';window.chatPrivateCreationAgent=a=>a;window.chatRosterEntry=()=>null;window.chatAgentLabel=a=>a;window.focusChatInput=()=>{};window.chatEditProject=()=>{};
  window.saved={revision:0,record_version:'v0',value:chatWorkstreams};window.conflict=true;
  window.chatApplyWorkstreams=s=>{chatWorkstreams=s.value};window.fetch=async(url,opts={})=>{if(opts.method==='PUT'){if(conflict){conflict=false;return{ok:false,status:409}}const body=JSON.parse(opts.body);saved={revision:saved.revision+1,record_version:'v1',value:body.value};}return{ok:true,json:async()=>structuredClone(saved)}};
 });
 const src=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 await page.addScriptTag({content:src.slice(src.indexOf('function chatFolderProjects('),src.indexOf('function chatRenderWorkstreamFilter('))+src.slice(src.indexOf('function renderChatHeadActions('),src.indexOf('// chatHeadActionsHome'))});
 await page.addScriptTag({content:fs.readFileSync(path.join(root,'js/49-chat-newchat.js'),'utf8')});
 const landing=()=>page.evaluate(()=>chatLandingChips(document.getElementById('chatComposer')));
 const projectRows=async()=>{await page.getByRole('button',{name:/^Project:/}).click();const menu=page.getByRole('dialog',{name:'Project'});await menu.waitFor();return menu;};
 // New chat opens the agent's landing, with the project as a chip
 await page.evaluate(()=>renderChatHeadActions());await page.getByRole('button',{name:'New chat',exact:true}).click();
 await page.waitForFunction(()=>location.hash==='#/chat/a/codex/new');await landing();
 let menu=await projectRows();
 assert.deepEqual(await menu.locator('.chat-chip-row-label').allTextContents(),['No project','manifest · /home/benjamin/src/manifest','manifest · /elsewhere/manifest']);
 await menu.getByRole('option',{name:/manifest · \/home\/benjamin\/src\/manifest/}).click();await menu.waitFor({state:'detached'});
 const result=await page.evaluate(()=>({id:chatPendingProject,state:saved.value,cwd:localStorage.getItem('manifest.chatTermCwd.codex'),chip:document.querySelector('.chat-landing-chip-project').textContent,folder:document.querySelector('.chat-landing-chip-folder').textContent}));
 assert.equal(result.state.groups[result.id],'manifest');assert.equal(result.state.members['terminal:codex/original'],result.id);assert.equal(result.state.members['terminal:codex/other'],undefined);
 assert.equal(result.cwd,'/home/benjamin/src/manifest');assert.match(result.chip,/^manifest/);assert.match(result.folder,/^manifest/,'the folder chip follows the project');
 await page.evaluate(()=>{chatWorkstreams=structuredClone(saved.value)});assert.equal(await page.evaluate(()=>chatResolveProject('folder:local:/home/benjamin/src/manifest')),result.id);assert.equal(await page.evaluate(()=>Object.keys(saved.value.groups).length),1);
 // a second new chat: the project is offered by name, and "No project" is standalone
 await page.evaluate(()=>{location.hash='';});await page.getByRole('button',{name:'New chat',exact:true}).click();await landing();
 menu=await projectRows();assert.ok((await menu.locator('.chat-chip-row-label').allTextContents()).includes('manifest'));
 await menu.getByRole('option',{name:/^No project/}).click();assert.equal(await page.evaluate(()=>chatPendingProject),'');
 console.log('PASS folder project chip: selectable rail folders, distinct same-name paths, CAS retry, durable membership, folder prefill, no duplicates, standalone.');
 }finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
