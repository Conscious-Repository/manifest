// chat-composer-models.cjs — the real front end over the stub chat API: the
// composer's model · effort chip and the /model, /effort surface commands on a
// native (Hermes) agent.
//   1. the chip names the conversation's model; the picker groups models by
//      provider, searches, sets effort, and the next message carries exactly
//      that model, provider and effort as its recipient;
//   2. /effort low applies without sending a message; /model with a name the
//      catalog lacks refuses in words and sends nothing; bare /model opens the
//      picker and Esc returns focus to the composer;
//   3. the command menu offers Manifest's commands on a native agent, an
//      exact command runs on Enter, and /goal sets, shows, pauses and clears
//      the chat's standing objective without sending a message;
//   4. the choice survives a reload (it rides the synced draft);
//   5. at phone width the chips wrap under the message, keep 44px targets and
//      nothing overflows, in both themes.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),path=require('node:path');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const hook=p=>fetch(base+p).then(r=>r.json());
 const posts=async()=>(await hook('/__log')).log.filter(l=>l.startsWith('POST ')&&l.includes('/messages')).length;
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/b');
  const input=page.locator('#chatComposer textarea');await input.waitFor();
  const chip=page.locator('#chatComposer .chat-composer-model');
  await page.waitForFunction(()=>/claude-x/.test(document.querySelector('#chatComposer .chat-composer-model')?.textContent||''));
  // a native (Hermes) reply has no recorded span: its footer never claims one
  assert.equal(await page.locator('#chatTranscript').getByText(/Worked for/).count(),0,'a native reply claimed a worked-for time');
  // 1. choose a model under another provider, and an effort
  await chip.click();
  const picker=page.getByRole('dialog',{name:'Model, effort and permissions'});await picker.waitFor();
  assert.deepEqual(await picker.locator('.chat-model-group').allTextContents(),['Anthropic','OpenAI','xAI','Lab (192.168.87.11:8000/v1)'],'models are grouped by provider');
  await picker.getByRole('searchbox',{name:'Search models'}).fill('grok');
  assert.equal(await picker.getByRole('option').count(),1,'search narrows the list');
  await picker.getByRole('option',{name:/grok-4\.6/}).click();
  await picker.getByRole('radio',{name:'high',exact:true}).click();
  await picker.getByRole('button',{name:'Apply'}).click();
  await picker.waitFor({state:'detached'});
  assert.match(await chip.textContent(),/grok-4\.6 · high/);
  await input.fill('What changed?');await input.press('Enter');
  await page.waitForFunction(async()=>true);
  let last;for(let i=0;i<50&&!(last=(await hook('/__last')).send);i++)await page.waitForTimeout(100);
  assert.deepEqual({agent:last.recipient.agent,model:last.recipient.model,provider:last.recipient.provider,effort:last.recipient.effort},{agent:'alfred',model:'grok-4.6',provider:'xai-oauth',effort:'high'},'the message names the chosen model, provider and effort');
  // a surface command while a send is still in flight applies at once (it
  // used to be swallowed until the send settled: the flake behind this step)
  await hook('/__delay?ms=2000');await input.fill('Held send');await input.press('Enter');
  await page.waitForFunction(()=>!!document.querySelector('#chatComposer .chat-send:disabled'));
  await input.fill('/effort medium');await input.press('Enter');
  await page.waitForFunction(()=>/· medium/.test(document.querySelector('#chatComposer .chat-composer-model')?.textContent||''),null,{timeout:1500});
  await hook('/__delay?ms=0');
  for(let i=0;i<50&&(await hook('/__last')).send?.text!=='Held send';i++)await page.waitForTimeout(100);
  await page.waitForFunction(()=>!document.querySelector('#chatComposer .chat-send:disabled'));
  // 2. surface commands never send a message
  const before=await posts();
  await input.fill('/effort low');await input.press('Enter');
  await page.waitForFunction(()=>/· low/.test(document.querySelector('#chatComposer .chat-composer-model')?.textContent||''));
  assert.equal(await input.inputValue(),'','an applied command clears the composer');
  await input.fill('/model nonsense-model');await input.press('Enter');
  await page.getByText('No model named nonsense-model',{exact:false}).waitFor();
  assert.equal(await input.inputValue(),'/model nonsense-model','a refused command keeps what was typed');
  await input.fill('/model');await input.press('Enter');
  await picker.waitFor();
  await page.keyboard.press('Escape');await picker.waitFor({state:'detached'});
  assert.equal(await page.evaluate(()=>document.activeElement===document.querySelector('#chatComposer textarea')),true,'Esc returns focus to the composer');
  // keyboard: ↓ picks the next model, → raises effort, Enter applies
  await input.fill('/model');await input.press('Enter');await picker.waitFor();
  await page.keyboard.press('ArrowRight');
  assert.equal(await picker.locator('[role="radio"][aria-checked="true"]').textContent(),'medium','→ raises effort one step');
  await page.keyboard.press('Enter');await picker.waitFor({state:'detached'});
  assert.match(await chip.textContent(),/grok-4\.6 · medium/);
  assert.equal(await posts(),before,'a surface command sent a message');
  // the command menu: Manifest's own commands on a native agent
  await input.fill('/');
  const menu=page.getByRole('listbox',{name:'Commands'});await menu.waitFor();
  assert.deepEqual((await menu.getByRole('option').allTextContents()).map(t=>t.split(/(?=[A-Z])/)[0].trim().split(' ')[0]),['/model','/effort','/goal','/new','/tile'],'native agents get the surface commands');
  await input.fill('/go');await page.keyboard.press('Enter');
  assert.equal(await input.inputValue(),'/goal ','Enter inserts the chosen command');
  // /goal: set, shown above the message, carried by the session, paused, cleared
  await input.fill('/goal Ship tiles with every test green');await input.press('Enter');
  const bar=page.locator('#chatComposer .chat-goal-bar');await bar.waitFor();
  assert.match(await bar.textContent(),/Goal.*Ship tiles with every test green/);
  assert.equal(stub.sessions.b.goal,'Ship tiles with every test green');
  assert.equal(await posts(),before,'/goal sent a message');
  assert.equal(await page.evaluate(()=>{const b=document.querySelector('.chat-goal-bar .chat-goal-act');chatModelChipsRefresh();chatPolishComposer(document.getElementById('chatComposer'));return b===document.querySelector('.chat-goal-bar .chat-goal-act');}),true,'a composer repaint replaced the goal controls under the pointer');
  await bar.getByRole('button',{name:'Pause'}).click();
  await page.waitForFunction(()=>document.querySelector('.chat-goal-bar')?.classList.contains('is-paused'));
  assert.equal(stub.sessions.b.goalState,'paused');
  await input.fill('/goal clear');await input.press('Enter');
  await bar.waitFor({state:'detached'});
  assert.equal(stub.sessions.b.goal,'');
  // 3. survives a reload
  await page.evaluate(()=>{for(const s of chatSyncedDrafts.values())s.flush?.();});await page.waitForTimeout(800);
  await page.reload();await input.waitFor();
  await page.waitForFunction(()=>/grok-4\.6 · medium/.test(document.querySelector('#chatComposer .chat-composer-model')?.textContent||''),null,{timeout:5000});
  // 4. phone and themes
  for(const theme of ['default','jarvis'])for(const width of [1440,390]){
   await page.setViewportSize({width,height:844});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.waitForTimeout(80);
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' overflow at '+width);
   const r=await chip.evaluate(e=>{const a=e.getBoundingClientRect(),t=document.querySelector('#chatComposer textarea').getBoundingClientRect(),c=document.getElementById('chatComposer').getBoundingClientRect();return {h:a.height,below:a.top>=t.bottom-1,inside:a.right<=c.right+1&&a.left>=c.left-1};});
   assert.ok(r.inside,'chip escapes the composer at '+width);
   if(width===390){assert.ok(r.h>=44,'chip target too small on phone');assert.ok(r.below,'chip should wrap under the message on phone');}
   await chip.click();await picker.waitFor();
   const box=await picker.evaluate(e=>{const b=e.getBoundingClientRect();return {l:b.left,r:b.right,t:b.top,iw:innerWidth};});
   assert.ok(box.l>=0&&box.r<=box.iw&&box.t>=0,'picker leaves the viewport at '+width);
   if(process.env.MANIFEST_FIXTURE_SHOTS)await page.screenshot({path:path.join(process.env.MANIFEST_FIXTURE_SHOTS,'model-picker-'+theme+'-'+width+'.png')});
   await page.keyboard.press('Escape');await picker.waitFor({state:'detached'});
  }
  // 5. a coding agent's picker (Claude Code, new chat): at desktop width a
  // panel of at most 560px inside the composer; aliases say what they last
  // ran as, pinned ids sit beside them; Bypass takes an explicit second step
  await page.evaluate(()=>document.documentElement.dataset.theme='default');
  await page.setViewportSize({width:1440,height:900});await page.goto(base+'/#/chat/a/claude/new');
  await page.locator('#chatComposer .chat-composer-model').waitFor();
  await page.locator('#chatComposer .chat-composer-model').click();await picker.waitFor();
  const geo=await page.evaluate(()=>{const b=document.querySelector('.chat-model-picker').getBoundingClientRect(),c=document.getElementById('chatComposer').getBoundingClientRect();return {w:b.width,inside:b.left>=c.left-1&&b.right<=c.right+1};});
  assert.ok(geo.w<=561&&geo.inside,'the desktop picker is a panel of at most 560px inside the composer: '+JSON.stringify(geo));
  assert.match(await picker.getByRole('option',{name:/^Fable/}).textContent(),/last ran as claude-fable-5-1/);
  assert.equal(await picker.getByRole('option',{name:/claude-opus-5-5/}).count(),2,'the pinned id is offered beside the alias that last ran as it');
  assert.equal(await picker.getByRole('radio',{name:'Allowed tools only'}).count(),1);
  const applyBtn=picker.getByRole('button',{name:'Apply'});
  await picker.getByRole('radio',{name:'Bypass'}).click();
  const warn=picker.locator('.chat-model-confirm');await warn.waitFor();
  assert.match(await warn.textContent(),/runs every tool and command without asking/);
  assert.equal(await applyBtn.isDisabled(),true,'Bypass must wait for an explicit confirmation');
  await picker.getByRole('searchbox').press('Enter');await page.waitForTimeout(200);
  assert.equal(await picker.count(),1,'Enter must not apply Bypass unconfirmed');
  await warn.locator('input').check();assert.equal(await applyBtn.isDisabled(),false);
  await picker.getByRole('radio',{name:'Ask'}).click();assert.equal(await warn.isHidden(),true,'a safe choice needs no step');
  await page.keyboard.press('Escape');await picker.waitFor({state:'detached'});
  // the command form goes through the same step
  await input.fill('/permissions bypassPermissions');await input.press('Enter');await picker.waitFor();
  assert.equal(await picker.getByRole('radio',{name:'Bypass'}).getAttribute('aria-checked'),'true');
  assert.equal(await applyBtn.isDisabled(),true,'/permissions bypass must not apply without the confirmation');
  await page.keyboard.press('Escape');await picker.waitFor({state:'detached'});
  // the phone new-chat composer keeps a usable message field; chips wrap below it
  await page.evaluate(()=>document.documentElement.dataset.theme='default');
  await page.setViewportSize({width:390,height:844});await page.goto(base+'/#/chat/a/alfred/new');
  await page.waitForFunction(()=>!!document.querySelector('#chatComposer .chat-composer-model'));
  // measured before and after the mic mounts (79-mic.js adds it 400 ms after
  // a route change): the chip row must not depend on which controls share row one
  const measure=async stage=>{
   const land=await page.evaluate(()=>{const c=document.getElementById('chatComposer').getBoundingClientRect(),t=document.querySelector('#chatComposer textarea').getBoundingClientRect(),m=document.querySelector('#chatComposer .chat-composer-model').getBoundingClientRect();return {ratio:t.width/c.width,below:m.top>=t.bottom-1};});
   assert.ok(land.ratio>=0.45,'the new-chat message field is squeezed on a phone ('+stage+'): '+land.ratio.toFixed(2));
   assert.ok(land.below,'the model chip should wrap under the message on a phone ('+stage+')');
  };
  await page.evaluate(()=>document.querySelector('#chatComposer .mic-btn')?.remove());await measure('no mic');
  await page.evaluate(()=>window.dispatchEvent(new HashChangeEvent('hashchange')));await page.waitForSelector('#chatComposer .mic-btn');await measure('with mic');
  // 6. "To Alfred · model" shows where a message went only when the
  // recipient or model changed since the previous message
  const to=(n,model)=>({id:'d'+n,state:'delivered',userTurn:n,context:{recipient:{agent:'alfred',model}}});
  await hook('/__set?id=a&patch='+encodeURIComponent(JSON.stringify({deliveries:[to(1,'gpt-5.6-luna'),to(3,'gpt-5.6-luna'),to(5,'grok-4.6')]})));
  await page.setViewportSize({width:1440,height:900});await page.goto(base+'/#/chat/a/alfred/a');
  await page.locator('#chatTranscript [data-chat-read-turn="5"]').waitFor();
  const shown=await page.evaluate(()=>[1,3,5].map(n=>document.querySelector('#chatTranscript [data-chat-read-turn="'+n+'"] .chat-context-attribution')?.textContent||''));
  assert.deepEqual(shown,['To Alfred · gpt-5.6-luna','','To Alfred · grok-4.6'],'the recipient line repeats only on a change');
  assert.deepEqual(errors,[]);
  console.log('PASS: provider-grouped picker, exact recipient on send, /model and /effort without sending, reload, phone and themes.');
  await ctx.close();
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
