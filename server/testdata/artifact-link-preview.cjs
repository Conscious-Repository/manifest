const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[],requests=[];p.on('pageerror',e=>errors.push(e.message));p.on('request',r=>requests.push(r.url()));
 await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.bytes='[InternetShortcut]\r\nURL=https://example.com/path?a=one&b=two#section\r\nIconFile=https://remote.example/icon.ico';
  window.a={id:'0123456789abcdef',kind:'link',title:'Reference',ref:'reference.url',head:'a'.repeat(64),content:bytes,revisions:[{n:1,hash:'a'.repeat(64)}]};window.posts=[];window.discussed=[];window.entries=[];
  window.fetch=async(url,opts={})=>{
   if(url.startsWith('/api/artifacts/reviews')){
    if(opts.method==='POST'){const value=JSON.parse(opts.body);posts.push(value);entries.push({...value,id:value.request_id,revision:a.head,at:new Date().toISOString()});}
    return {ok:true,json:async()=>({revision:a.head,record_version:String(entries.length),state:entries.at(-1)?.state||'not_requested',entries})};
   }
   return {ok:true,json:async()=>structuredClone(a)};
  };
  window.mount=()=>artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(a),review:true,onDiscuss:ref=>discussed.push(ref)});window.workspace=mount();
 });
 const link=p.locator('.artifact-link-destination');await link.waitFor();assert.equal(await link.textContent(),'https://example.com/path?a=one&b=two#section');assert.equal(await link.getAttribute('href'),await link.textContent());assert.equal(await link.getAttribute('rel'),'noopener noreferrer');assert.equal(await link.getAttribute('referrerpolicy'),'no-referrer');
 await p.locator('.artifact-link-source summary').click();assert.equal(await p.locator('.artifact-link-source code').textContent(),await p.evaluate(()=>bytes));
 const view=await p.evaluate(()=>workspace.getView());await p.evaluate(async view=>{workspace.close();workspace=mount();await workspace.restoreView(view);},view);assert.equal(await p.locator('.artifact-link-source').evaluate(e=>e.open),true);
 assert.equal(requests.some(url=>!url.startsWith('https://fixture.test')),false,'preview must not fetch link or icon');
 await p.locator('.artifact-review-controls > summary').click();await p.getByRole('button',{name:'record decision',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();assert.equal(await p.evaluate(()=>posts.length),1);
 await p.getByRole('button',{name:'Discuss',exact:true}).click();assert.equal(await p.evaluate(()=>discussed[0].revision),'a'.repeat(64));
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.locator('.artifact-link-preview').scrollIntoViewIfNeeded();await p.screenshot({path:'/tmp/manifest-link-preview-phone.png'});
 await p.evaluate(()=>{a.content='[InternetShortcut]\nURL=javascript:alert(1)';workspace.close();workspace=mount();});await p.getByText('Link preview unavailable:',{exact:false}).waitFor();assert.equal(await p.locator('.artifact-link-destination').count(),0);assert.equal(await p.locator('.artifact-link-source').evaluate(e=>e.open),true);
 assert.deepEqual(errors,[]);console.log('PASS: exact saved link, no remote fetch, literal source, version review/context, unsupported fallback and both-theme viewport bounds');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
