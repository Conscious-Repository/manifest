const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.join(__dirname,'../web');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 try{
 const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',route=>route.abort());
 await page.setContent('<main class="chat-main term"><header class="chat-head"><h2 class="chat-head-title">Improve the candidate review</h2></header><div class="chat-transcript" id="transcript"></div><div class="chat-composer"><textarea class="chat-input" aria-label="Message" placeholder="Message Codex…"></textarea><button class="chat-send" aria-label="Send">↑</button></div></main>');
 for(const name of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',name+'.css'),'utf8')});
 await page.addStyleTag({content:'body{margin:0;padding:16px}.chat-main{max-width:900px;margin:auto;min-width:0}.chat-transcript{min-height:0}'});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatTermOpen={id:'fixture',planRevisions:{}};window.chatOpenId='fixture';window.chatEmbedded=false;window.fmtWhen=()=> '10:42 AM';
  window.chatProposalBlocks=t=>t.blocks||[];window.chatQuestionReplyDisplay=t=>t;
  window.renderMarkdown=text=>{const p=el('p','md-p',text);return p;};
 });
 const source=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 const library=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');
 await page.addScriptTag({content:library.slice(library.indexOf('const keyedChildrenCache'),library.indexOf('// ---- pill factory'))});
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
 await page.evaluate(()=>{window.originalRow=document.querySelector('.chat-term-out');window.originalDetails=document.querySelector('.chat-term-step-details');originalDetails.querySelector('summary').focus();chatTermPaintLines(document.getElementById('transcript'),turns);});
 assert.equal(await page.evaluate(()=>originalRow===document.querySelector('.chat-term-out')),true,'unchanged transcript row is retained');
 assert.equal(await page.evaluate(()=>originalDetails.open&&originalDetails.contains(document.activeElement)),true,'nested disclosure and focus survive refresh');
 // A long history must not reparse every message when just the tail changes.
 const perf=await page.evaluate(()=>{
   const host=document.getElementById('transcript'),originalRender=renderMarkdown;
   let parses=0;window.renderMarkdown=(...args)=>{parses++;return originalRender(...args);};
   const history=Array.from({length:300},(_,i)=>({id:'history-'+i,who:'assistant',blocks:[{t:'say',text:'A substantial historical reply. '.repeat(40)}]}));
   chatTermPaintLines(host,history);const first=host.firstChild;parses=0;
   const start=performance.now();history.push({id:'tail',who:'assistant',blocks:[{t:'say',text:'New output'}]});chatTermPaintLines(host,history);
   const result={parses,retained:first===host.firstChild,ms:performance.now()-start};
   chatTermPaintLines(host,turns);return result;
 });
 assert.equal(perf.parses,1,'new output parses only its own markdown');assert.equal(perf.retained,true);console.log('300-turn append:',JSON.stringify(perf));
 assert.equal(await page.locator('.chat-term-activity').evaluate(e=>e.open),true,'activity collapsed on transcript refresh');
 assert.equal(await page.getByRole('button',{name:'Response copied',exact:true}).count(),1,'copy feedback survives repaint');
 for(const theme of ['default','jarvis'])for(const width of [1440,390]){
  await page.setViewportSize({width,height:900});await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
  const style=await page.locator('.chat-term-say').evaluate(e=>({font:getComputedStyle(e).fontFamily,size:parseFloat(getComputedStyle(e).fontSize)}));
  assert.ok(style.size>=16,'coding prose too small');assert.ok(!/mono/i.test(style.font),'coding prose still monospace');
  if(width===390)assert.ok(await page.getByRole('button',{name:'Send',exact:true}).evaluate(e=>e.getBoundingClientRect().height)>=44,'send target too small');
 }
 // Exercise the actual transcript container too: updates must preserve both
 // the history rows and unchanged approval controls outside them.
 await page.addScriptTag({content:source.slice(source.indexOf('function chatTermPaintTurns()'),source.indexOf('// ---- the terminal painter'))});
 await page.evaluate(()=>{
   const transcript=document.getElementById('transcript');transcript.id='chatTranscript';transcript.replaceChildren();
   const body=el('div');body.id='chatTermTurns';transcript.append(body);
   window.chatStick=false;window.chatPin=()=>{};window.chatWorkbenchActivityUpdate=()=>{};window.approvalBuilds=0;
   window.appendTaskApprovals=host=>{approvalBuilds++;host.append(el('button','','Review approval'));};
   chatTermOpen.turns=turns;chatTermOpen.proposals=[];chatTermOpen.se={};
   chatTermPaintTurns();window.keptHistory=body.querySelector('.chat-term-out');window.keptApproval=body.querySelector('.chat-native-extra button');
   keptApproval.focus();chatTermOpen.turns=[...turns,{id:'new-turn',who:'user',text:'Another message'}];chatTermPaintTurns();
 });
 assert.equal(await page.evaluate(()=>keptHistory===document.querySelector('.chat-native-rows .chat-term-out')),true,'full transcript update retains history');
 assert.equal(await page.evaluate(()=>document.activeElement===keptApproval&&approvalBuilds===1),true,'full transcript update retains approval focus');
 await page.screenshot({path:'/tmp/manifest-chat-conversation-phone.png',fullPage:true});
 assert.deepEqual(errors,[]);console.log('PASS: readable coding conversation, expandable failed activity, refresh state, desktop/phone bounds and touch targets.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
