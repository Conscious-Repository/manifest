// chat-handoff.cjs — the real front end over the stub chat API: the model is
// chosen when a chat starts (2026-09-27), so a different agent or model is a
// new chat. "New chat with this context" is a panel over the composer in the
// model picker's shape — no native <select>, no modal — reached from the
// running chat's model picker:
//   1. it offers agents, a coding agent's folders (recent and ~/src repos),
//      the model list with effort, a title and the handoff draft;
//   2. Create posts the linked chat (agent, folder, model, the draft as its
//      prompt), sets a coding chat's effort on its unlaunched draft, and opens
//      it — nothing is sent;
//   3. at phone width the panel stays on screen.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const hook=p=>fetch(base+p).then(r=>r.json());
 try{
  for(const width of [1440,390]){
   const ctx=await browser.newContext({viewport:{width,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+'/#/chat/a/alfred/b');
   const chip=page.locator('#chatComposer .chat-composer-model');await chip.waitFor({state:'attached'});
   assert.equal(await page.locator('#chatThreadHeader .chat-recipient-control, #chatComposer .chat-composer-recipient').count(),0,'no "Choose agent" hand-off control');
   // a phone folds the chip into ＋ (docs/ui-conventions.md, phone rule 4)
   if(width===390){await page.locator('#chatComposer .chat-attach').click();await page.getByRole('option',{name:/Model and effort/}).click();}else await chip.click();
   const picker=page.getByRole('dialog',{name:'Model, effort and permissions'});await picker.waitFor();
   await picker.getByRole('button',{name:/New chat with this context/}).click();
   const panel=page.getByRole('dialog',{name:'New chat with this context'});await panel.waitFor();
   await panel.getByRole('listbox',{name:'Model'}).waitFor();
   assert.equal(await page.locator('#chatComposer select, dialog[open]').count(),0,'a composer panel, not a native select or a modal');
   const box=await panel.evaluate(e=>{const b=e.getBoundingClientRect();return {l:b.left,r:b.right,t:b.top,iw:innerWidth};});
   assert.ok(box.l>=0&&box.r<=box.iw&&box.t>=0,'the panel leaves the viewport at '+width+': '+JSON.stringify(box));
   if(width===390){await page.keyboard.press('Escape');await panel.waitFor({state:'detached'});await ctx.close();continue;}
   // a coding agent: folder, model, effort
   await panel.getByRole('radio',{name:'Codex',exact:true}).click();
   const folders=panel.getByRole('listbox',{name:'Working folder'});await folders.getByRole('option',{name:/lab-apps/}).click();
   await panel.getByRole('listbox',{name:'Model'}).getByRole('option',{name:'gpt-5.6-luna',exact:true}).click();
   await panel.getByRole('radiogroup',{name:'Effort'}).getByRole('radio',{name:'high',exact:true}).click();
   const draft=panel.getByRole('textbox',{name:'Handoff draft'});
   assert.match(await draft.inputValue(),/Continue work related to “Second thread”/);
   await draft.fill('Pick up the parser fix.');
   await panel.getByRole('button',{name:'Create chat'}).click();
   await page.waitForFunction(()=>/#\/chat\/a\/codex\/rel1$/.test(location.hash));
   const {related,launches}=await hook('/__related');
   assert.deepEqual({agent:related[0].agent,backend:related[0].backend,cwd:related[0].cwd,model:related[0].model,prompt:related[0].prompt},
    {agent:'codex',backend:'terminal',cwd:'/home/owner/src/lab-apps',model:'gpt-5.6-luna',prompt:'Pick up the parser fix.'},'the linked chat carries agent, folder, model and the draft');
   assert.deepEqual(launches,[{id:'rel1',effort:'high'}],'the coding draft launches at the chosen effort');
   assert.equal((await hook('/__log')).log.filter(l=>l.startsWith('POST ')&&l.includes('/messages')).length,0,'nothing was sent');
   assert.deepEqual(errors,[]);
   await ctx.close();
  }
  console.log('PASS: New chat with this context — composer panel, folder/model/effort, linked draft created, effort on launch, nothing sent, phone bounds.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
