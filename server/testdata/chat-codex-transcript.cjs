// A coding reply reads like a top-tier harness: lists hang their bullets and
// keep their numbers and nesting, a fenced block names its language and copies
// exactly its own text, and nothing overflows at desktop or phone width in
// either theme. Uses the real renderer (70-note.js), painter (48-chat.js) and
// stylesheets; no network.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',route=>route.abort());
 await page.setContent('<main class="chat-main term"><div class="chat-transcript" id="transcript"></div></main>');
 for(const name of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
 await page.addStyleTag({content:'body{margin:0;padding:16px}.chat-main{max-width:900px;margin:auto;min-width:0}'});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatTermOpen={id:'fixture',planRevisions:{}};window.chatOpenId='fixture';window.chatEmbedded=false;window.fmtWhen=()=>'10:42 AM';
  window.chatSplitUserMessage=text=>({text,files:[]});window.chatProposalBlocks=t=>t.blocks||[];window.chatQuestionReplyDisplay=t=>t;
  window.resolveWikilink=()=>{};
 });
 const note=fs.readFileSync(path.join(root,'js/70-note.js'),'utf8');
 await page.addScriptTag({content:note.slice(note.indexOf('function renderMarkdown('),note.indexOf('// ---- [[wikilink]] autocomplete'))});
 const library=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');
 await page.addScriptTag({content:library.slice(library.indexOf('const keyedChildrenCache'),library.indexOf('// ---- pill factory'))});
 const source=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 await page.addScriptTag({content:source.slice(source.indexOf('const chatActivityOpen'),source.indexOf('// ---- the live strip:'))});
 const workspace=fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8');
 await page.addScriptTag({content:workspace.slice(workspace.indexOf('function chatCopyResponseControl('))});
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.copied=text;}}}));
 const reply=['Fixed and verified. Two separate faults in the same row.','','**What changed**','','1. The verdict buttons take their label width first.','2. The action row is no longer pinned:','  - each card keeps its own row','  - long labels wrap instead of clipping','3. A guard test locks both fixes.','','Run it with:','','```bash','go test -count=1 -run TestFeedVerdictRow ./server','```','','- Live feed at 390 pixels: nine cards, no sideways scroll.','- Desktop is untouched.'].join('\n');
 await page.evaluate(reply=>{
  window.turns=[{id:'u1',who:'user',text:'Fix the verdict row on the phone.',ts:1},{id:'a1',who:'assistant',ts:1,blocks:[{t:'step',cast:'read',input:'server/web/css/45-feed.css',result:'…'},{t:'say',text:reply}]}];
  chatTermPaintLines(document.getElementById('transcript'),turns);
 },reply);
 // Lists: numbers survive, bullets hang, nesting indents.
 const items=await page.locator('.chat-term-say .md-li').evaluateAll(list=>list.map(li=>({marker:li.querySelector('.md-bullet').textContent,depth:li.dataset.depth||'0',left:li.querySelector('span:last-child').getBoundingClientRect().left,gap:li.querySelector('span:last-child').getBoundingClientRect().left-li.querySelector('.md-bullet').getBoundingClientRect().right})));
 assert.deepEqual(items.map(i=>i.marker),['1.','2.','•','•','3.','•','•'],'ordered items keep their numbers');
 assert.deepEqual(items.map(i=>i.depth),['0','0','1','1','0','0','0'],'nested items carry their depth');
 assert.ok(items.every(i=>i.gap>=4),'bullet runs into its text: '+JSON.stringify(items.map(i=>i.gap)));
 assert.ok(items[2].left>items[1].left+10,'nested item is not indented');
 // Code: the language shows, Copy copies exactly the block.
 await page.getByText('bash',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Copy code',exact:true}).click();
 assert.equal(await page.evaluate(()=>copied),'go test -count=1 -run TestFeedVerdictRow ./server');
 assert.equal(await page.getByRole('button',{name:'Code copied',exact:true}).count(),1);
 for(const theme of ['default','jarvis'])for(const width of [1440,390]){
  await page.setViewportSize({width,height:1000});await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
  const bg=await page.locator('.md-code-block').evaluate(e=>getComputedStyle(e).backgroundColor);
  assert.notEqual(bg,'rgba(0, 0, 0, 0)','code block has no surface in '+theme);
  if(width===390)assert.ok(await page.locator('.md-code-copy').evaluate(e=>e.getBoundingClientRect().height)>=44,'code copy target too small on phone');
  if(process.env.MANIFEST_FIXTURE_SHOTS)await page.screenshot({path:path.join(process.env.MANIFEST_FIXTURE_SHOTS,'codex-transcript-'+theme+'-'+width+'.png'),fullPage:true});
 }
 assert.deepEqual(errors,[]);console.log('PASS: numbered and nested lists, hanging bullets, language-labelled code with exact copy, no overflow in both themes.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
