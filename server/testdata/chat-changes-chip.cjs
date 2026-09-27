// The live +N −M chip (brief §5b.4, 2026-09-27): a coding session's header
// button that opens the existing working-folder review now says what the
// tree holds against HEAD. It reads …/changes/stat at most every 15 s,
// says when it read, renders a failure as unknown (never "no changes"),
// and a press still captures and opens the review.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chromium',headless:true});try{
 const page=await browser.newPage();await page.route('https://chip.test/**',r=>r.fulfill({contentType:'text/html',body:'<header id="head"></header>'}));await page.goto('https://chip.test');
 const root=path.join(__dirname,'../web');
 await page.addStyleTag({content:['00-core','48-chat'].map(f=>fs.readFileSync(path.join(root,'css/'+f+'.css'),'utf8')).join('\n')});
 await page.evaluate(()=>{
  window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
  window.chatTermBase=id=>'/api/terminal/session/'+id;window.chatRouteVersion=1;window.chatAgent='codex';window.chatOpenId='abc123';window.opened=[];window.stats=0;window.showToast=()=>{};
  window.reply={ok:true,body:{added:12,removed:3,files:4,binary:1,untracked:2,head:'0123456789abcdef0123456789abcdef01234567',at:'2026-09-27T10:00:00Z'}};
  window.fetch=async(url)=>{stats++;return {ok:reply.ok,json:async()=>reply.body,text:async()=>reply.body};};
  window.postJSONOk=async(url)=>({id:'snap',revision:'r1',url});window.chatOpenWorkingArtifact=a=>opened.push(a);window.chatPaneShown=()=>true;
 });
 const src=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
 await page.addScriptTag({content:src.slice(src.indexOf('function chatChangesButton('),src.indexOf('// Single-reference drafts'))});
 await page.evaluate(()=>document.getElementById('head').append(chatChangesButton({id:'abc123'})));
 const chip=page.locator('.chat-changes-chip');
 await page.waitForFunction(()=>document.querySelector('.chat-changes-chip').classList.contains('is-changed'));
 assert.equal(await chip.textContent(),'+12−32 new');
 assert.match(await chip.getAttribute('title'),/^4 files changed, 12 added, 3 removed, 1 binary, 2 untracked against 0123456 · read at .+ · open the review$/);
 // at most every 15 s
 await page.evaluate(()=>chatChangesRefresh('abc123'));assert.equal(await page.evaluate(()=>stats),1);
 // a clean tree, then a failure: unknown, never "no changes"
 await page.evaluate(async()=>{reply.body={added:0,removed:0,files:0,binary:0,untracked:0,head:'0123456789abcdef0123456789abcdef01234567',at:'2026-09-27T10:01:00Z'};await chatChangesRefresh('abc123',true);});
 assert.equal(await chip.textContent(),'No changes');
 await page.evaluate(async()=>{reply={ok:false,body:'working-folder Git review unavailable'};await chatChangesRefresh('abc123',true);});
 assert.equal(await chip.textContent(),'Changes ?');assert.match(await chip.getAttribute('title'),/unknown: working-folder Git review unavailable/);
 // a press still opens the review
 await chip.click();await page.waitForFunction(()=>opened.length===1);assert.equal(await page.evaluate(()=>opened[0].selectionKey),'chat:codex/abc123');
 console.log('PASS: live +N −M chip — counts, read time, 15 s floor, clean, unknown on failure, opens the review.');
 }finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
