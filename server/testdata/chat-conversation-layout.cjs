const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});
 try{
 const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',route=>route.abort());
 await page.setContent('<main class="chat-main term"><header class="chat-head"><h2 class="chat-head-title">Improve the candidate review</h2></header><div class="chat-transcript" id="transcript"></div><div class="chat-composer"><textarea class="chat-input" aria-label="Message" placeholder="Message Codex…"></textarea><button class="chat-send" aria-label="Send">↑</button></div></main>');
 for(const name of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
 await page.addStyleTag({content:'body{margin:0;padding:16px}.chat-main{max-width:900px;margin:auto;min-width:0}.chat-transcript{min-height:0}'});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatTermOpen={id:'fixture',planRevisions:{}};window.chatOpenId='fixture';window.fmtWhen=()=> '10:42 AM';
  window.chatProposalBlocks=t=>t.blocks||[];
  window.renderMarkdown=text=>{const p=el('p','md-p',text);return p;};
 });
 const source=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 await page.addScriptTag({content:source.slice(source.indexOf('const chatActivityOpen'),source.indexOf('// ---- the live strip:'))});
 const workspace=fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8');
 await page.addScriptTag({content:workspace.slice(workspace.indexOf('function chatCopyResponseControl('))});
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.copied=text;}}}));
 await page.evaluate(()=>{
  window.turns=[{id:'u1',who:'user',text:'Make candidate review easier to use on my phone.',ts:1},{id:'a1',who:'assistant',ts:1,blocks:[{t:'step',cast:'read',input:'/fixture/candidates.js',result:'original file'},{t:'step',cast:'test',input:'focused checks',result:'one failed check',error:true},{t:'say',text:'I found the source of the cramped layout. The candidate summary now comes first, followed by the stage control and supporting evidence. You can keep reviewing without losing your place.'}]}];
  chatTermPaintLines(document.getElementById('transcript'),turns);
 });
 await page.getByRole('button',{name:'Copy response',exact:true}).click();assert.equal(await page.evaluate(()=>copied),await page.evaluate(()=>turns[1].blocks.at(-1).text));
 assert.equal(await page.locator('.chat-term-activity').getAttribute('open'),null);
 await page.getByText('Activity · 2 steps · 1 failed',{exact:true}).click();
 await page.locator('.chat-term-step-details').first().locator('summary').click();
 await page.getByText('original file',{exact:true}).waitFor();
 await page.evaluate(()=>{document.getElementById('transcript').replaceChildren();chatTermPaintLines(document.getElementById('transcript'),turns);});
 assert.equal(await page.locator('.chat-term-activity').evaluate(e=>e.open),true,'activity collapsed on transcript refresh');
 for(const theme of ['default','jarvis'])for(const width of [1440,390]){
  await page.setViewportSize({width,height:900});await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
  const style=await page.locator('.chat-term-say').evaluate(e=>({font:getComputedStyle(e).fontFamily,size:parseFloat(getComputedStyle(e).fontSize)}));
  assert.ok(style.size>=16,'coding prose too small');assert.ok(!/mono/i.test(style.font),'coding prose still monospace');
  if(width===390)assert.ok(await page.getByRole('button',{name:'Send',exact:true}).evaluate(e=>e.getBoundingClientRect().height)>=44,'send target too small');
 }
 await page.screenshot({path:'/tmp/manifest-chat-conversation-phone.png',fullPage:true});
 assert.deepEqual(errors,[]);console.log('PASS: readable coding conversation, expandable failed activity, refresh state, desktop/phone bounds and touch targets.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
