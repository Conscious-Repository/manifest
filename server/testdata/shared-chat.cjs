const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const modulePath=path.join(__dirname,'../web/portal/src/shared-chat.js');
assert.equal(fs.readFileSync(modulePath,'utf8'),fs.readFileSync(path.join(__dirname,'../web/ooda/src/shared-chat.js'),'utf8'),'portals must use identical shared-session behavior');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:390,height:844}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  let attempts=[],teamAttempts=[],releaseA=null,holdA=false,holdScreen=false,releaseScreen=null;
  await page.route('http://localhost:7341/**',async route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/'){await route.fulfill({contentType:'text/html',body:'<main id="root"></main>'});return;}
   if(url.pathname.endsWith('/conversation')){
    const id=url.pathname.split('/')[4];
    if(id==='a'&&holdA)await new Promise(resolve=>releaseA=resolve);
    await route.fulfill({json:{thread:id,messages:[{id:id+'-msg',text:'History '+id}],terminals:[{id:'0123456789abcdef',agent:'codex',model:'astra',process:'running'}],warnings:[],files:[{hash:'f'.repeat(64),name:'plan.md',size:24}]}});return;
   }
   if(url.pathname.endsWith('/screen')){if(holdScreen)await new Promise(resolve=>releaseScreen=resolve);await route.fulfill({json:{live:true,process:'running',lines:['Allow this command?','> Yes','  No','<script>literal screen text</script>','long screen row '+ 'x'.repeat(200)]}});return;}
   if(url.pathname.startsWith('/api/chat/attach/')){await route.fulfill({contentType:'text/plain',body:'EXACT_PLAN_CONTENT'});return;}
   if(url.pathname==='/api/chat/ask'){teamAttempts.push(route.request().postDataJSON());await route.fulfill({json:{ok:true}});return;}
   if(url.pathname.endsWith('/input')){
    const body=route.request().postDataJSON();attempts.push(body);
    await route.fulfill({status:attempts.length===1?202:200,json:{delivery:{id:body.requestId,state:attempts.length===1?'unconfirmed':'sent'}}});return;
   }
   await route.fulfill({status:404,body:'unexpected'});
  });
  async function mount(){
   await page.goto('http://localhost:7341/');
   await page.addScriptTag({url:'https://unpkg.com/react@18.3.1/umd/react.development.js'});
   await page.addScriptTag({url:'https://unpkg.com/react-dom@18.3.1/umd/react-dom.development.js'});
   await page.addScriptTag({content:fs.readFileSync(modulePath,'utf8')});
   await page.evaluate(()=>{
    function Harness(){
     const [id,setID]=React.useState('a'),[text,setText]=React.useState(''),[status,setStatus]=React.useState('');
     const shared=SHARED_CHAT.useThread({id,sharedSource:{agent:'kairos-private',id:'private'}},[],{email:'member@aion.bio'});
     const h=React.createElement;
     return h('div',null,h('button',{onClick:()=>{setID(id==='a'?'b':'a');setText('');setStatus('');}},'Switch thread'),
      h('button',{onClick:shared.refresh},'Refresh'),shared.controls('Kairos','',sent=>setStatus('Recovered '+sent)),
      ...shared.messages.map(m=>h('p',{key:m.id},m.text)),h('textarea',{'aria-label':'Message',value:text,onChange:e=>setText(e.target.value)}),
      h('button',{onClick:async()=>{try{if(shared.native){await shared.send(text);}else{await fetch('/api/chat/ask',{method:'POST',body:JSON.stringify({text,context:shared.teamContext([])})});shared.filesConfirmed();}setText('');setStatus('Sent');}catch(e){setStatus(e.message);}}},'Send'),h('p',{role:'status'},status));
    }
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(Harness));
   });
   await page.getByText('History a',{exact:true}).waitFor();
  }
  await mount();
  await page.getByLabel('Message recipient').selectOption('team');
  await page.getByText('Files',{exact:true}).click();
  await page.getByRole('button',{name:'plan.md',exact:true}).click();
  await page.getByText('EXACT_PLAN_CONTENT',{exact:true}).waitFor();
  await page.getByRole('checkbox').check();
  await page.getByLabel('Message',{exact:true}).fill('Discuss this plan with Kairos');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  await page.getByText('Sent',{exact:true}).waitFor();
  assert.deepEqual(teamAttempts,[{text:'Discuss this plan with Kairos',context:['file/'+'f'.repeat(64)]}]);
  assert.equal(await page.getByRole('checkbox').isChecked(),false,'team send left a stale file selection');
  await page.getByLabel('Message recipient').selectOption('0123456789abcdef');
  await page.getByRole('checkbox').check();
  await page.getByLabel('Message',{exact:true}).fill('Keep this instruction');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  await page.getByText(/Delivery is unconfirmed/).waitFor();
  assert.equal(await page.getByLabel('Message',{exact:true}).inputValue(),'Keep this instruction','uncertain send lost draft');
  assert.equal(attempts.length,1);assert.deepEqual(attempts[0].files,['f'.repeat(64)]);
  await page.getByLabel('Message recipient').selectOption('team');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  await page.getByText('Resolve the pending terminal delivery before sending another message.',{exact:true}).waitFor();
  assert.equal(teamAttempts.length,1,'team send bypassed pending terminal recovery');
  await mount(); // browser navigation/reload retains the exact request
  await page.getByText('Message awaiting confirmation',{exact:true}).click();
  await page.getByRole('button',{name:'Retry saved message'}).click();
  await page.getByText('Recovered Keep this instruction',{exact:true}).waitFor();
  assert.equal(attempts.length,2);assert.deepEqual(attempts[0],attempts[1],'retry changed request identity/body');
  assert.equal(await page.evaluate(()=>Object.keys(localStorage).filter(k=>k.startsWith('manifest.shared-input.')).length),0);
  await page.getByLabel('Message recipient').selectOption('0123456789abcdef');
  await page.getByText('Terminal controls',{exact:true}).click();
  await page.getByLabel('Terminal screen',{exact:true}).filter({hasText:'Allow this command?'}).waitFor();
  assert.equal(await page.locator('script').filter({hasText:'literal screen text'}).count(),0,'screen text executed as markup');
  await page.getByRole('button',{name:'Terminal Escape',exact:true}).click();
  await page.waitForFunction(()=>Object.keys(localStorage).filter(k=>k.startsWith('manifest.shared-input.')).length===0);
  assert.equal(attempts[2].key,'\x1b');assert.equal(attempts[2].text,undefined);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'screen caused phone overflow');
  holdScreen=true;for(let i=0;i<500&&!releaseScreen;i++)await new Promise(r=>setTimeout(r,10));assert.ok(releaseScreen,'screen poll did not start');
  // A slow old-thread read must never replace the selected conversation.
  holdA=true;await page.getByRole('button',{name:'Refresh',exact:true}).click();
  for(let i=0;i<100&&!releaseA;i++)await new Promise(r=>setTimeout(r,10));
  assert.ok(releaseA,'read did not start');
  await page.getByRole('button',{name:'Switch thread'}).click();
  await page.getByText('History b',{exact:true}).waitFor();
  releaseA();releaseScreen();await page.waitForTimeout(100);
  assert.equal(await page.getByLabel("Terminal screen",{exact:true}).count(),0,"late screen crossed thread boundary");
  assert.equal(await page.getByText('History a',{exact:true}).count(),0,'late response crossed thread boundary');
  await page.addScriptTag({url:'https://unpkg.com/@babel/standalone@7.29.0/babel.min.js'});
  for(const relative of ['portal/src/chat.jsx','ooda/src/view-chat.jsx']) {
    const source=fs.readFileSync(path.join(__dirname,'../web',relative),'utf8');
    assert.ok(source.includes('context:shared.teamContext(ctx)')||source.includes('context: shared.teamContext(ctx)'),relative+' must send the selected files');
    await page.evaluate(source=>Babel.transform(source,{presets:['react']}).code,source);
  }
  assert.deepEqual(errors,[]);
  console.log('Shared portal chat: exact files for team and native agents, delivery recovery, literal live screen, phone bounds, and transcript/screen thread-switch isolation passed at 390px.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
