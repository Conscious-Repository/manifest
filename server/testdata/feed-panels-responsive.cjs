const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web'),read=p=>fs.readFileSync(path.join(root,p),'utf8');
const part=(s,a,b)=>s.slice(s.indexOf(a),s.indexOf(b,s.indexOf(a)+1));
(async()=>{const browser=await chromium.launch({headless:true,channel:'chromium'});try{
const p=await browser.newPage({viewport:{width:390,height:844}}),errors=[];p.on('pageerror',e=>errors.push(e.message));
await p.setContent('<main><div id="todosTabs" class="feed-filters"></div><div id="todosToolbar" class="tdo-toolbar"></div><div id="feedFilters"></div><div class="feed-view"><div id="feedSignals"></div><div id="feedList"></div></div><div id="termSessionRows"></div><div id="writing"></div><div id="bookShelf"></div><div id="contactList"></div></main>');
for(const f of ['00-core','05-primitives','45-feed','46-consume','65-reading','71-write','90-todos','95-mobile'])await p.addStyleTag({content:read('css/'+f+'.css')});
await p.addScriptTag({content:read('js/05-components.js')});
await p.evaluate(()=>{
 window.els=Object.fromEntries(['feedFilters','feedList','feedSignals','bookShelf','contactList'].map(id=>[id,document.getElementById(id)]));
 window.todosTab='focus';window.todosLens='all';window.todosMode='list';window.todosQuery='';window.todosCache={rows:[{}]};window.TODOS_TABS=[['focus','ALL DOMAINS'],['aion','AION']];window.todoMatches=()=>true;window.openTodoQuickAdd=()=>{};window.renderTodos=()=>renderTodosToolbar();
 window.state={feedFilter:''};window.feedFilter=()=>state.feedFilter;window.FEED_FILTERS=[['consume','Read'],['signal','Signals']];window.loadFeed=()=>{};
 window.consumeLoadError="";window.consumeIsActiveView=()=>true;window.consumeCache={items:[{}],lists:['reading'],unread:1,total:1};window.consumeView='unread';window.consumeList='';window.consumeQuery='';window.consumeSearchEl=null;window.feedClaimRender=()=>{};window.consumeSearch=()=>renderConsume();window.consumeFilterChanged=()=>renderConsume();window.consumeManageOpen=false;window.consumeCuratedOpen=false;window.consumeSub='';window.consumeShowAll=false;window.CONSUME_PAGE=50;window.consumeCardEl=()=>el('div','','A reading item');window.renderApprovalInspector=()=>{};
 window.requests=[];window.fetch=url=>new Promise(resolve=>requests.push({url,resolve}));
});
for(const [f,a,b] of [['90-todos','function renderTodosToolbar()','function renderTodos()'],['45-feed','function renderFeedFilters()','function renderFeed()'],['71-write','function writeInput(','function writeRenderComments('],['46-consume','function renderConsume()','// ---- the manage panel'],['65-reading','function addBook()','if (els.bookSearch)'],['60-contacts','function openCreatePanel()','if (els.contactSearch)'],['73-terminal','function renderTermSessions(enabled)','function termRuntimeKey']])await p.addScriptTag({content:part(read('js/'+f+'.js'),a,b)});
await p.addScriptTag({content:part(read('js/46-consume.js'),'const consumePanelState=','function consumeManagePanel()')});
await p.evaluate(()=>{renderTodosToolbar();renderFeedFilters();renderConsume();const input=writeInput({posting:false},'new','',()=>{});document.getElementById('writing').append(input);});

await p.addScriptTag({content:part(read('js/46-consume.js'),'function consumeCuratedPanel()','function consumeCuratedRow(')});
await p.evaluate(()=>{window.consumeSubs={subscriptions:[]};window.consumeCurated={entries:[],public:''};window.consumeManagePanel=()=>el('div','consume-manage','Following');window.consumeCuratedRow=en=>el('div','',en.title);window.emptyRow=t=>el('div','',t);window.card=document.querySelector('.consume-content').lastElementChild;window.search=document.querySelector('.consume-search');});
for(const width of [390,1280]){
 await p.setViewportSize({width,height:900});
 await p.locator('.consume-curated-toggle').click();
 assert.equal(await p.locator('.consume-curated-toggle').textContent(),'close curated');
 await p.getByText('Loading curated items…',{exact:true}).waitFor();
 await p.locator('.consume-curated-toggle').click();
 assert.equal(await p.locator('.consume-panels').textContent(),'','closes before response');
 await p.evaluate(()=>requests.shift().resolve({ok:true,json:async()=>({entries:[{title:'Loaded item'}]})}));
 await p.waitForTimeout(10);assert.equal(await p.locator('.consume-panels').textContent(),'','late response cannot reopen');
 await p.locator('.consume-curated-toggle').click();
 await p.evaluate(()=>requests.shift().resolve({ok:false,status:503}));
 await p.getByText('Could not load curated items. Try again.',{exact:true}).waitFor();
 await p.getByRole('button',{name:'Retry',exact:true}).click();
 await p.evaluate(()=>requests.shift().resolve({ok:true,json:async()=>({entries:[{title:'Loaded item'}]})}));
 await p.getByText('Loaded item',{exact:true}).waitFor();
 await p.locator('.consume-curated-toggle').click();
 await p.locator('.consume-manage-toggle').click();await p.locator('.consume-manage-toggle').click();
 await p.evaluate(()=>requests.shift().resolve({ok:true,json:async()=>({subscriptions:[]})}));
 assert.equal(await p.evaluate(()=>card===document.querySelector('.consume-content').lastElementChild&&search===document.querySelector('.consume-search')),true,'panels retain cards and search');
}
await p.locator('.consume-manage-toggle').click();
await p.evaluate(()=>requests.shift().resolve({ok:true,json:async()=>({subscriptions:[]})}));
await p.getByText('Following',{exact:true}).waitFor();
await p.evaluate(()=>{window.following=document.querySelector('[data-panel="subscriptions"] .consume-manage');following.append(el('input'));following.querySelector('input').value='unfinished subscription';});
await p.locator('.consume-curated-toggle').click();
await p.evaluate(()=>requests.shift().resolve({ok:true,json:async()=>({entries:[]})}));
await p.waitForTimeout(10);
assert.equal(await p.evaluate(()=>following===document.querySelector('[data-panel="subscriptions"] .consume-manage')&&following.querySelector('input').value==='unfinished subscription'),true,'loading another panel preserves unfinished form');
await p.evaluate(()=>{const card=el('div','consume-x-text','Post');document.querySelector('.feed-view').append(card);document.body.append(el('div','consume-body','Full post'));});
for(const width of [390,1280])for(const theme of ['default','jarvis']){
 await p.setViewportSize({width,height:900});await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
 assert.equal(await p.evaluate(()=>{const card=getComputedStyle(document.querySelector('.consume-x-text')),body=getComputedStyle(document.querySelector('.consume-body'));return card.fontFamily===body.fontFamily&&card.fontSize===body.fontSize;}),true,'card and reader typography match');
}
assert.deepEqual(errors,[]);console.log('Feed panels: immediate open/close, late responses, retry, stable cards, matching mobile/desktop typography passed');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
