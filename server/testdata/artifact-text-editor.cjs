const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
const page=await browser.newPage({viewport:{width:390,height:844}});await page.setContent('<main></main>');
const root=path.join(__dirname,'../web');for(const f of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
await page.evaluate(()=>{
 window.el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';e.textContent=text||'';return e;};
 window.renderMarkdown=text=>el('pre','',text);window.chatRenderStateNotice=()=>{};
 window.a={id:'fixture',title:'Report',ref:'report.md',head:'a'.repeat(64),content:'original',revisions:[{n:1,hash:'a'.repeat(64)}]};
 window.fetch=async()=>({ok:true,json:async()=>structuredClone(a)});
});
const source=fs.readFileSync(path.join(root,'js/05-components.js'),'utf8');await page.addScriptTag({content:source.slice(source.indexOf('function artifactLineChanges'),source.indexOf('// A compact, keyboard-accessible'))});
await page.evaluate(()=>artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(a),save:async(text,expected)=>{if(expected!==a.head)throw Error('conflict');a.content=text;a.head='b'.repeat(64);a.revisions.push({n:2,hash:a.head});},saveNotice:'New artifact version saved.',onDiscuss:ref=>window.discussed=ref}));
await page.getByRole('button',{name:'Edit',exact:true}).click();await page.getByRole('textbox',{name:'File content'}).fill('revised report');
await page.getByRole('button',{name:'Review changes',exact:true}).click();await page.getByText('+ revised report',{exact:true}).waitFor();
await page.getByRole('button',{name:'Save new version'}).click();await page.getByText('New artifact version saved.',{exact:true}).waitFor();
await page.getByRole('button',{name:'Discuss'}).click();assert.equal(await page.evaluate(()=>discussed.revision),'b'.repeat(64));
assert.equal(await page.evaluate(()=>a.content),'revised report');
await page.evaluate(()=>{
 document.querySelector('main').replaceChildren();a.ref='example.py';a.content='# keep this comment\nif left < right:\n    print("<button>literal</button>")';
 artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(a)});
});
await page.locator('.artifact-source-preview').waitFor();
assert.equal(await page.locator('.artifact-source-preview').innerText(),await page.evaluate(()=>a.content));
assert.equal(await page.locator('.artifact-source-preview button').count(),0);
console.log('PASS: direct artifact text edit, compare, version save and exact-version discussion.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
