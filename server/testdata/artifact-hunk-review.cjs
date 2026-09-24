const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.bytes='Working folder: /fixture\n\ndiff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@ first\n-old\n+new\n@@ -20 +20 @@ second\n-before\n+<script>literal()</script>\ndiff --git a/b.txt b/b.txt\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-old\n+new\n';
  window.artifact={id:'0123456789abcdef',title:'Changes',ref:'changes.diff',head:'a'.repeat(64),content:bytes,revisions:[{n:1,hash:'a'.repeat(64)}]};window.entries=[];window.posts=[];window.discussed=[];
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
 await p.getByLabel('Request changes to hunk 2 in a.txt',{exact:true}).waitFor();
 assert.equal(await p.locator('[data-after-line="20"] code').textContent(),'+<script>literal()</script>');assert.equal(await p.locator('.working-hunk script').count(),0);
 await p.getByLabel('Hunk 1 in a.txt',{exact:true}).click();
 assert.equal(await p.locator('.working-hunk').nth(0).evaluate(e=>e.open),false);
 await p.locator('.working-file > summary').nth(1).click();await p.getByLabel('Hunk 1 in b.txt',{exact:true}).click();await p.locator('.working-file > summary').nth(1).click();
 const saved=await p.evaluate(()=>workspace.getView());assert.equal(saved.collapsedHunks.length,2);
 await p.evaluate(async saved=>{workspace.close();workspace=mount();await workspace.restoreView(saved);},saved);
 assert.equal(await p.locator('.working-hunk').nth(0).evaluate(e=>e.open),false);
 await p.locator('.working-file > summary').nth(1).click();await p.getByLabel('Hunk 1 in b.txt',{exact:true}).waitFor();assert.equal(await p.locator('.working-file').nth(1).locator('.working-hunk').evaluate(e=>e.open),false,'a hidden lazy file retains hunk state after reopening');
 await p.getByLabel('Request changes to hunk 2 in a.txt',{exact:true}).click();
 assert.equal(await p.getByLabel('First reviewed line').inputValue(),'9');assert.equal(await p.getByLabel('Last reviewed line').inputValue(),'11');
 assert.match(await p.getByLabel('Review notes').inputValue(),/Hunk: @@ -20 \+20 @@ second/);
 await p.getByLabel('Review notes').fill((await p.getByLabel('Review notes').inputValue())+'Explain this change.');
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 const result=await p.evaluate(()=>({post:posts[0],discussion:discussed[0],text:bytes.split('\n').slice(posts[0].start-1,posts[0].end).join('\n')}));
 assert.equal(result.post.revision,'a'.repeat(64));assert.equal(result.post.start,9);assert.equal(result.post.end,11);assert.equal(result.discussion.reviewLineKind,'snapshot');assert.equal(result.discussion.revision,'a'.repeat(64));assert.equal(result.text,'@@ -20 +20 @@ second\n-before\n+<script>literal()</script>');
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.locator('.working-file').first().scrollIntoViewIfNeeded();await p.screenshot({path:'/tmp/manifest-hunk-review-phone.png'});
 assert.deepEqual(errors,[]);console.log('PASS: immutable hunk review, lazy collapse restoration, literal source text, exact draft handoff and both-theme 320/390/1440px layout');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
