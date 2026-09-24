const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.bytes='name,value\n"multiline\nname","<script>literal()</script>"\nshort,=SUM(A1)\n';
  window.artifact={id:'0123456789abcdef',title:'Records',ref:'records.csv',head:'a'.repeat(64),content:bytes,revisions:[{n:1,hash:'a'.repeat(64)}]};window.entries=[];window.posts=[];window.discussed=[];
  window.fetch=async(url,opts={})=>{
   if(url.startsWith('/api/artifacts/reviews')){
    const revision=new URL(url,location.origin).searchParams.get('revision');
    if(opts.method==='POST'){const body=JSON.parse(opts.body);posts.push({revision,...body});entries.push({...body,id:body.request_id,revision,at:new Date().toISOString()});}
    return {ok:true,json:async()=>({record_version:String(entries.length),revision,state:entries.at(-1)?.state||'not_requested',entries})};
   }
   return {ok:true,json:async()=>structuredClone(artifact)};
  };
  window.mount=()=>artifactWorkspace(document.querySelector('main'),{review:true,load:async()=>structuredClone(artifact),onDiscuss:ref=>discussed.push(ref)});window.workspace=mount();
 });
 await p.getByLabel('Request changes to record 2',{exact:true}).waitFor();
 assert.equal(await p.locator('.artifact-data-table script').count(),0);
 assert.equal(await p.locator('tbody tr').nth(1).locator('td').nth(1).textContent(),'<script>literal()</script>');
 await p.locator('.artifact-table-source summary').click();
 assert.equal(await p.locator('.artifact-table-source code').textContent(),await p.evaluate(()=>bytes));
 const saved=await p.evaluate(()=>workspace.getView());assert.equal(saved.table.sourceOpen,true);
 await p.evaluate(async saved=>{workspace.close();workspace=mount();await workspace.restoreView(saved);},saved);
 assert.equal(await p.locator('.artifact-table-source').evaluate(e=>e.open),true);
 await p.getByLabel('Request changes to record 2',{exact:true}).click();
 assert.equal(await p.getByLabel('First reviewed line').inputValue(),'2');assert.equal(await p.getByLabel('Last reviewed line').inputValue(),'3');
 assert.match(await p.getByLabel('Review notes').inputValue(),/Record: 2/);
 assert.equal(await p.evaluate(()=>posts.length),0);
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 const result=await p.evaluate(()=>({post:posts[0],discussion:discussed[0]}));
 assert.equal(result.post.revision,'a'.repeat(64));assert.equal(result.post.start,2);assert.equal(result.post.end,3);assert.equal(result.discussion.revision,'a'.repeat(64));
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.locator('.artifact-table-scroll').scrollIntoViewIfNeeded();await p.screenshot({path:'/tmp/manifest-table-review-phone.png'});
 await p.evaluate(async()=>{artifact.content=Array.from({length:20},(_,i)=>'field '+i).join(',');workspace.close();workspace=mount();});
 await p.locator('.artifact-table-scroll').waitFor();
 assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 const wide=await p.evaluate(()=>{document.querySelector('.artifact-table-scroll').scrollLeft=300;return workspace.getView();});
 assert.equal(wide.table.scrollLeft,300);
 await p.evaluate(async saved=>{workspace.close();workspace=mount();await workspace.restoreView(saved);},wide);
 assert.equal(await p.locator('.artifact-table-scroll').evaluate(e=>e.scrollLeft),300);
 await p.evaluate(async()=>{artifact.content='"broken';workspace.close();workspace=mount();});
 await p.getByText('Table preview unavailable:',{exact:false}).waitFor();
 assert.equal(await p.locator('.artifact-table-source').evaluate(e=>e.open),true);
 assert.equal(await p.locator('.artifact-table-source code').textContent(),'"broken');
 assert.deepEqual(errors,[]);console.log('PASS: table record review, source restoration, literal values, malformed fallback and both-theme 320/390/1440px layout');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
