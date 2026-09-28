const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
// The side-chat model list reads the same catalog the composer chips use
// (/api/chat/models): labels not raw ids, a configured-default row, the exact
// current model kept, and your last chat's model as the new default.
(async()=>{const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chromium',headless:true});try{const p=await browser.newPage();await p.setContent('<select id="model"></select>');
await p.evaluate(()=>{window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls;e.textContent=text;return e;};
 window.chatLoadModelCatalog=async()=>({codex:{default:'astra',models:[{id:'astra',label:'Astra'},{id:'other',label:'Other'}]},claude:{default:'fable',last:{model:'opus',at:'2026-09-27T10:00:00Z'},models:[{id:'fable',label:'Fable'},{id:'opus',label:'Opus'}]}});});
const source=fs.readFileSync(path.join(__dirname,'../web/js/49-chat-workspace.js'),'utf8');
await p.addScriptTag({content:source.slice(source.indexOf('function chatPopulateModelSelect('),source.indexOf('// Explicit control+option/alt shortcuts'))});
assert.equal(source.includes('/api/terminal/models'),false,'the ids-only endpoint is retired');
await p.evaluate(()=>{chatPopulateModelSelect(document.querySelector('select'),'codex','');chatPopulateModelSelect(document.querySelector('select'),'claude','');});
await p.waitForFunction(()=>document.querySelector('select').options.length===3);
assert.equal(await p.locator('select').inputValue(),'opus','your last chat with the agent is the default');
assert.deepEqual(await p.locator('option').evaluateAll(es=>es.map(e=>e.textContent)),['Configured default','Fable','Opus']);
await p.evaluate(()=>chatPopulateModelSelect(document.querySelector('select'),'codex','custom-current'));await p.waitForFunction(()=>document.querySelector('select').options.length===4);assert.equal(await p.locator('select').inputValue(),'custom-current');
await p.evaluate(()=>chatPopulateModelSelect(document.querySelector('select'),'codex',''));await p.waitForFunction(()=>document.querySelector('select').options.length===3);assert.equal(await p.locator('select').inputValue(),'astra');
console.log('PASS model list: catalog labels, last-used default, exact current model, configured default.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
