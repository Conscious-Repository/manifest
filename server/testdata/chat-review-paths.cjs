const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});try{
const page=await browser.newPage({viewport:{width:390,height:844}});await page.setContent('<main></main>');
const root=path.join(__dirname,'../web');for(const f of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
await page.evaluate(()=>{document.documentElement.dataset.theme='jarvis';window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};});
const source=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');await page.addScriptTag({content:source.slice(source.indexOf('function artifactDiffLine('),source.indexOf('function artifactDiffView('))});await page.addScriptTag({content:source.slice(source.indexOf('function artifactWorkingChangesView'))});
await page.evaluate(()=>document.querySelector('main').append(artifactWorkingChangesView('Working folder: /fixture\nCompared with HEAD: abc\n\ndiff --git a/one.txt b/one.txt\n--- a/one.txt\n+++ b/one.txt\n@@ -4,1 +7,1 @@\n-old\n+new\ndiff --git a/two.txt b/two.txt\n--- a/two.txt\n+++ b/two.txt\n+<script>bad()</script>\n\nUntracked files (contents not included):\n"draft.txt"\n',context=>window.reviewContext=context)));
await page.getByText('2 changed files',{exact:true}).waitFor();await page.getByRole('button',{name:'Request changes to one.txt',exact:true}).click();assert.deepEqual(await page.evaluate(()=>reviewContext),{path:'one.txt',start:4,end:9});assert.equal(await page.locator('[data-before-line="4"] code').textContent(),'-old');assert.equal(await page.locator('[data-after-line="7"] code').textContent(),'+new');assert.equal(await page.locator('.working-file').nth(1).evaluate(e=>e.open),false);await page.locator('.working-file > summary').nth(1).click();await page.getByText('+<script>bad()</script>',{exact:true}).waitFor();
await page.getByRole('button',{name:'collapse all',exact:true}).click();assert.equal(await page.locator('.working-file[open]').count(),0);await page.getByRole('button',{name:'expand all',exact:true}).click();assert.equal(await page.locator('.working-file[open]').count(),2);
await page.screenshot({path:'/tmp/manifest-file-review-phone.png'});
assert.equal(await page.locator('.working-untracked').evaluate(e=>e.open),false);await page.locator('.working-untracked > summary').click();await page.getByText('draft.txt',{exact:true}).waitFor();
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
await page.evaluate(()=>{document.querySelector('main').replaceChildren(artifactWorkingChangesView('Working folder: /fixture\nNo tracked changes against HEAD.\n'));});await page.getByText('No tracked changes',{exact:true}).waitFor();assert.equal(await page.getByRole('combobox').count(),0);
const chat=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8');
// "Choose agent" is retired (2026-09-27): a leftover hand-off in a private
// thread's draft is dropped; a shared thread still chooses.
await page.addScriptTag({content:chat.slice(chat.indexOf('function chatUsableRecipient('),chat.indexOf('function chatSharedRecipientItems('))});
assert.equal(chat.includes('function chatChooseRecipient('),false);assert.equal(chat.includes('function chatChooseTerminalRecipient('),false);
assert.deepEqual(await page.evaluate(()=>{window.chatRecipients=new Map([['codex/a',{backend:'hermes',agent:'alfred'}],['alfred/b',{agent:'alfred',effort:'high'}],['alfred/c',{backend:'terminal',agent:'codex',id:'x'}]]);
 return [chatUsableRecipient('codex/a',false),chatRecipients.has('codex/a'),chatUsableRecipient('alfred/b',false)?.effort,chatUsableRecipient('alfred/c',true)?.id];}),[null,false,'high','x']);
console.log('PASS: per-file review, literal diff text, expandable untracked files, honest empty state, phone bounds, retired hand-off dropped.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
