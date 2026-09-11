const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const chat=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const terminal=fs.readFileSync(path.join(__dirname,'../web/js/73-terminal.js'),'utf8');
const elements=[],requests=[],notices=[];
function el(tag,cls,text){const e={tag,cls,text,children:[],append(...items){this.children.push(...items);}};elements.push(e);return e;}
const ctx=vm.createContext({el,crypto:require('node:crypto').webcrypto,chatTermOpen:{id:'fixture',se:{backend:'herdr'}},
 document:{getElementById:id=>id==='chatComposer'?{}:null,querySelector:()=>({insertBefore(){}})},
 chatTermBase:id=>'/api/terminal/session/'+id,postJSONOk:async(url,body)=>{requests.push({url,body});return {delivery:{state:'unconfirmed'}};},
 showToast:message=>notices.push(message),setTimeout(){},chatTermScreenFetch(){}});
vm.runInContext(terminal.slice(terminal.indexOf('const TERM_KEY_CODES ='),terminal.indexOf('// mobile soft-key bar')),ctx);
vm.runInContext(chat.slice(chat.indexOf('const chatTermQuickKeys ='),chat.indexOf('function chatTermPaintStrip')),ctx);
vm.runInContext(chat.slice(chat.indexOf('async function chatTermKey('),chat.indexOf('// ---- the tail:')),ctx);
(async()=>{
 ctx.chatTermStripEl();const buttons=elements.filter(e=>e.tag==='button');
 assert.deepEqual(buttons.map(e=>e.text),['enter','esc','↑','↓','←','→','ctrl-c']);
 assert.ok(elements.some(e=>e.text?.includes('for Tab / Shift-Tab, open Terminal')));
 for(const b of buttons)await b.onclick();
 assert.equal(requests.length,buttons.length);assert.equal(notices.length,buttons.length,'uncertain keys offer no automatic replay');
 await ctx.chatTermKey('tab');await ctx.chatTermKey('shift-tab');assert.equal(requests.length,buttons.length);
 assert.ok(requests.every(r=>r.url==='/api/terminal/session/fixture/input'&&r.body.requestId));
 assert.ok(!chat.includes('metis · tmux'));assert.ok(!chat.includes('not wired to this row yet'));assert.ok(chat.includes('Send your first message to start in this folder.'));
 process.stdout.write(JSON.stringify(requests.map(r=>r.body.key)));
})().catch(e=>{console.error(e);process.exitCode=1;});
