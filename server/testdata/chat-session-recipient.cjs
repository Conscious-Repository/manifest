// chat-session-recipient.cjs — the real front end over the stub chat API. An
// open native chat keeps sending with the model its messages were sent with
// (its delivery receipts, 2026-09-27), read only from receipts to its own
// agent, and only while the catalog still lists that model:
//   1. the chip and the next send carry the chat's own model and effort, not
//      a later receipt addressed to another agent;
//   2. a model the catalog no longer lists is not sent (the server refuses an
//      unlisted model/provider, and a running chat's model cannot be changed,
//      so the chat would be stuck): the send falls back to the agent's own.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {makeStub}=require('./chat-stub-api.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const hook=p=>fetch(base+p).then(r=>r.json());
 const rec=(agent,model,provider,effort)=>({id:'r-'+agent+model,state:'delivered',userTurn:1,context:{recipient:{agent,model,requestedModel:model,provider,effort}}});
 const set=deliveries=>hook('/__set?id=b&patch='+encodeURIComponent(JSON.stringify({deliveries})));
 const lastSend=async()=>{let s;for(let i=0;i<50&&!(s=(await hook('/__last')).send);i++)await new Promise(r=>setTimeout(r,100));return s;};
 try{
  // 1. its own agent's receipt, not a later one to another agent
  await set([rec('alfred','grok-4.6','xai-oauth','high'),rec('kairos','deepseek-v4.1-flash','lab-sparks','low')]);
  const ctx=await browser.newContext({viewport:{width:1440,height:900}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/chat/a/alfred/b');
  const chip=page.locator('#chatComposer .chat-composer-model'),input=page.locator('#chatComposer textarea');await input.waitFor();
  await page.waitForFunction(()=>/grok-4\.6 · high/.test(document.querySelector('#chatComposer .chat-composer-model')?.textContent||''),null,{timeout:10000}).catch(async()=>assert.fail('chip reads '+await chip.textContent()));
  await input.fill('First');await input.press('Enter');
  let s=await lastSend();
  assert.deepEqual({agent:s.recipient.agent,model:s.recipient.model,provider:s.recipient.provider,effort:s.recipient.effort},{agent:'alfred',model:'grok-4.6',provider:'xai-oauth',effort:'high'},'the send keeps the chat\'s own model');
  await ctx.close();
  // 2. a model the catalog no longer lists is not sent
  await set([rec('alfred','grok-3-retired','xai-oauth','high')]);
  const ctx2=await browser.newContext({viewport:{width:1440,height:900}});const page2=await ctx2.newPage();page2.on('pageerror',e=>errors.push(e.message));
  await page2.goto(base+'/#/chat/a/alfred/b');const input2=page2.locator('#chatComposer textarea');await input2.waitFor();
  await page2.locator('#chatComposer .chat-composer-model').waitFor();await page2.waitForTimeout(500);
  const before=(await hook('/__last')).send;
  await input2.fill('Second');await input2.press('Enter');
  for(let i=0;i<50;i++){s=(await hook('/__last')).send;if(s&&s!==before&&s.text==='Second')break;await page2.waitForTimeout(100);}
  assert.equal(s.text,'Second','the second send did not arrive');
  assert.notEqual(s.recipient?.model,'grok-3-retired','a model the catalog no longer lists was sent');
  assert.equal(s.recipient?.provider||'','','no provider rides a fallback send');
  assert.deepEqual(errors,[]);
  await ctx2.close();
  console.log('PASS: an open native chat sends with its own receipted model; a retired model falls back to the agent\'s own.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
