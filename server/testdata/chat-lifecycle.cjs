// Owner-only organization, actual row renderer and revisioned lifecycle actions.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:'chrome'});try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',r=>r.abort());
 await page.setContent('<div id="chatInboxRows" style="margin-top:650px"></div>');
 const root=path.join(__dirname,'../web');for(const file of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',file+'.css'),'utf8')});
 await page.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await page.evaluate(()=>{
  document.documentElement.dataset.theme='jarvis';
  window.state={key:'inbox',slot:'lifecycle',revision:0,value:{items:{}}};window.writes=0;window.conflict=true;
  window.fetch=async(url,opts={})=>{if(opts.method==='PUT'){writes++;const body=JSON.parse(opts.body);if(conflict){conflict=false;state.revision++;state.value.items['terminal:codex/other']='archived';return {status:409};}assertRevision=body.revision===state.revision;state={...state,revision:state.revision+1,value:body.value};}return {ok:true,status:200,json:async()=>structuredClone(state)};};
  Object.assign(window,{chatInboxKey:e=>'terminal:'+e.agent+'/'+e.session.id,chatOpenId:'',chatAgent:'codex',chatPins:{},chatWorkstreamFilter:'all',chatInboxFilter:'all',chatSearchQuery:'',chatTermKinds:{codex:'Codex'},chatWorkstreams:{groups:{}},chatWorkstreamMember:()=>'',chatAgentLabel:x=>x,fmtWhen:()=> 'today',terminalStateDot:()=>el('span','','●'),chatTermFolder:()=>'',chatTermRename:()=>{},chatChooseWorkstream:()=>{},chatSetPinned:()=>{},showToast:message=>{throw Error(message)},chatRemember:()=>{},chatSectionHash:()=> '#/chat'});
  window.entry={terminal:true,agent:'codex',session:{id:'fixture',kind:'codex',name:'A conversation'}};
  window.chatInboxEntries=()=>[entry].filter(e=>(chatLifecycle[chatInboxKey(e)]||'active')===chatLifecycleFilter);
 });
 const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 for(const [a,b] of [['let chatLifecycle=','let chatWorkstreams='],['function renderChatInboxRows()','function renderChatRail()'],['function chatTermRow(se)','function chatTermFolder(se)'],['function chatTermEndIsKill(se)','async function chatTermEnd(se)']])await page.addScriptTag({content:chat.slice(chat.indexOf(a),chat.indexOf(b))});
 await page.evaluate(()=>renderChatInboxRows());
 await page.locator('summary').click();
 await page.waitForTimeout(30);
 const bounds=await page.locator('.chat-row-menu-body').boundingBox();assert.ok(bounds.y>=0&&bounds.y+bounds.height<=844);
 await page.getByRole('button',{name:'Archive',exact:true}).click();assert.equal(await page.locator('.chat-rail-row').count(),0);
 assert.equal(await page.evaluate(()=>writes),2);assert.equal(await page.evaluate(()=>state.value.items['terminal:codex/other']),'archived');
 await page.evaluate(()=>{chatLifecycleFilter='archived';renderChatInboxRows();});await page.locator('summary').click();await page.getByRole('button',{name:'Restore to chats'}).click();
 await page.evaluate(()=>{chatLifecycleFilter='active';renderChatInboxRows();});await page.locator('summary').click();await page.getByRole('button',{name:'Delete chat…'}).click();
 await page.getByRole('dialog',{name:'Delete chat?'}).waitFor();await page.getByRole('button',{name:'Move to Trash'}).click();assert.equal(await page.locator('.chat-rail-row').count(),0);
 await page.evaluate(()=>{chatLifecycleFilter='deleted';renderChatInboxRows();});await page.locator('summary').click();await page.getByRole('button',{name:'Restore to chats'}).click();
 assert.equal(await page.evaluate(()=>chatTermEndIsKill({backend:'herdr',process:'unknown'})),false);
 assert.equal(await page.evaluate(()=>chatTermEndIsKill({live:false,process:'stopped'})),false);
 assert.equal(await page.evaluate(()=>chatTermEndIsKill({live:true,process:'running'})),true);
 await page.evaluate(()=>{
  const host=document.getElementById('chatInboxRows');host.replaceChildren();
  chatWorkstreams.groups={research:'Research'};
  chatWorkstreamMember=key=>key.endsWith('/assigned')?'research':'';
  const entries=[...Array.from({length:7},(_,i)=>({terminal:true,agent:'codex',session:{id:'p'+i,cwd:'/src/manifest'}})),
   {terminal:true,agent:'codex',session:{id:'other',cwd:'/elsewhere/manifest'}},
   {terminal:true,agent:'codex',session:{id:'assigned',cwd:'/src/manifest'}},
   {terminal:true,agent:'codex',session:{id:'standalone'}}];
  window.renderProjects=()=>{host.replaceChildren();chatRenderProjectGroups(host,entries,entries.map(e=>el('div','fixture-row',e.session.id)));};
  renderProjects();
 });
 assert.equal(await page.locator('.chat-project-group').count(),3);
 assert.equal(await page.locator('.chat-project-path').count(),2);
 assert.equal(await page.locator('.fixture-row').count(),8);
 assert.equal(await page.locator('#chatInboxRows > .fixture-row').innerText(),'standalone');
 await page.getByRole('button',{name:'Show more',exact:true}).click();
 // Show more uses the production inbox renderer; exercise its saved expansion on the same entries.
 await page.evaluate(()=>renderProjects());
 assert.equal(await page.locator('.fixture-row').count(),10);
 await page.evaluate(()=>{chatSearchQuery='manifest';renderProjects();});
 assert.equal(await page.locator('.chat-project-group:not([open])').count(),0);
 assert.deepEqual(errors,[]);console.log('PASS: archive, restore, Trash confirmation, cross-device conflict merge, viewport menu bounds and positive stop eligibility.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
