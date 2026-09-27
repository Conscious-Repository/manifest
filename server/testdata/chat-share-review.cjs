const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
(async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chromium',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:390,height:844}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  const revision='a'.repeat(64),second='b'.repeat(64),requests=[];
  let state={state:'private'},version=revision,failOnce=true;
  // the server's exact team-edit statements (owner decision D3), read from
  // the Go source so the fixture cannot drift from what the server sends
  const goEdit=fs.readFileSync(path.join(__dirname,'../chat_share_file_edit.go'),'utf8');
  const consentText=JSON.parse(goEdit.match(/const teamFileEditConsent = ("[^"]+")/)[1]);
  const [offMsg,onMsg]=[...goEdit.matchAll(/Message: ("[^"]+")/g)].map(m=>JSON.parse(m[1]));
  const editStates={off:{enabled:false,state:'off',message:offMsg},on:{enabled:true,state:'eligibility-only',message:onMsg,consent:consentText}};
  let edit=editStates.off;
  const review=()=>({session:{agent:'kairos-private',id:'fixture'},audience:'AION team',futureMessages:true,revision:version,timeline:[{who:'user',text:'Review this entire text <script>NO_EXECUTION</script>'}],teamFileEdit:edit,files:[{hash:'f'.repeat(64),name:'Exact plan.md',artifactId:'plan-one'}],continuations:[{id:'terminal-fixture',agent:'codex',model:'astra'}],blockers:[]});
  await page.route('http://localhost:7341/**',async route=>{
   const request=route.request(),u=new URL(request.url());
   if(u.pathname==='/'){return route.fulfill({contentType:'text/html',body:'<style>:root{--text:#c8e9f4;--surface-panel:#0c2535;--line:#205169;--accent:#00ccef}body{background:#092130;color:var(--text)}</style><main>Fixture only</main>'});}
   if(u.pathname.endsWith('/share-review'))return route.fulfill({json:review()});
   if(u.pathname.endsWith('/share')){
    if(request.method()==='GET')return route.fulfill({json:state});
    const payload=request.postDataJSON();requests.push(payload);
    state={state:failOnce?'prepared':'shared',...payload,conversation:{route:'#/chat/kairos/shared-fixture'}};
    if(failOnce){failOnce=false;return route.abort('failed');}
    return route.fulfill({json:state});
   }
   return route.fulfill({status:404,body:'unexpected fixture URL'});
  });
  async function mount(){
   await page.goto('http://localhost:7341/');
   await page.addStyleTag({content:fs.readFileSync(path.join(__dirname,'../web/css/48-chat.css'),'utf8')});
   await page.addScriptTag({content:fs.readFileSync(path.join(__dirname,'../web/js/47-chat-share.js'),'utf8')});
   await page.evaluate(()=>CHAT_SHARE.open({agent:'kairos-private',id:'fixture',title:'My work',onShared:c=>window.fixtureOpened=c.route}));
  }
  await mount();
  await page.getByText('Review what will become visible before sharing.').waitFor();
  assert.equal(requests.length,0);
  const share=page.getByRole('button',{name:'Share conversation',exact:true});
  assert.equal(await share.isDisabled(),true);
  await page.getByText('Review full conversation',{exact:true}).click();
  assert.match(await page.locator('.chat-share-section pre').first().textContent(),/NO_EXECUTION/);
  assert.match(await page.getByRole('link',{name:'Exact plan.md'}).getAttribute('href'),/rev=ffffffff/);
  const bounds=await page.getByRole('dialog').boundingBox();assert.ok(bounds.x>=0&&bounds.x+bounds.width<=390,'dialog overflows phone');
  if(process.env.CHAT_SHARE_SCREENSHOT)await page.screenshot({path:process.env.CHAT_SHARE_SCREENSHOT});
  await page.getByRole('checkbox').check();await share.click();
  await page.getByText(/your confirmation is saved/).waitFor();assert.equal(requests.length,1);
  await mount(); // prepared server state + persistent confirmation survives reload
  await page.getByText('Your confirmation is saved. Retry uses the same reviewed version.').waitFor();
  await page.getByRole('button',{name:'Retry confirmed share',exact:true}).click();
  await page.getByText('Sharing complete.').waitFor();
  assert.equal(requests.length,2);assert.deepEqual(requests[0],requests[1]);
  assert.equal(await page.evaluate(()=>window.fixtureOpened),undefined,'must not redirect before explicit open');
  await page.getByRole('button',{name:'Open team conversation'}).click();
  assert.equal(await page.evaluate(()=>window.fixtureOpened),'#/chat/kairos/shared-fixture');
  assert.equal(await page.evaluate(()=>localStorage.length),0);
  // Private source + changed review invalidates an uncommitted old confirmation.
  state={state:'private'};version=second;
  await page.evaluate(v=>localStorage.setItem('manifest.chat-share.v1.kairos-private.fixture',JSON.stringify({requestId:'old-confirmation',revision:v})),revision);
  await mount();await page.getByText('Review what will become visible before sharing.').waitFor();
  assert.equal(await page.getByRole('button',{name:'Share conversation',exact:true}).isDisabled(),true);
  assert.equal(await page.getByRole('checkbox').isChecked(),false);
  assert.equal(requests.length,2,'changed review automatically published');
  // D3, both states. Off: legible and truthful, no consent shown.
  assert.match(offMsg,/off for this share/);assert.match(offMsg,/cannot write/);
  await page.getByText(offMsg,{exact:true}).waitFor();
  assert.equal(await page.locator('.chat-share-edit-consent').count(),0,'off shows no consent');
  // On: the owner's wording verbatim, beside "not in force" and "cannot write".
  edit=editStates.on;await mount();
  await page.getByText(onMsg,{exact:true}).waitFor();assert.match(onMsg,/not in force/);assert.match(onMsg,/cannot write/);
  assert.equal(await page.locator('blockquote.chat-share-edit-consent').textContent(),'Team members may edit files in this share. Every save is attributed and keeps its prior versions; nothing is overwritten silently.');
  const quote=await page.locator('.chat-share-edit-consent').boundingBox();assert.ok(quote.x>=0&&quote.x+quote.width<=390,'consent fits the phone');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  edit=editStates.off;
  assert.deepEqual(errors,[]);
  console.log('Sharing review: explicit consent, exact file links, phone layout, lost-ACK/reload retry identity, and stale confirmation reset, and both team-edit states passed.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
