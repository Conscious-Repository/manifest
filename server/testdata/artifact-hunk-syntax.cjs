const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill(r.request().url().endsWith('/vendor/artifact-syntax.js')?{contentType:'text/javascript',body:fs.readFileSync(path.join(root,'vendor/artifact-syntax.js'),'utf8')}:{contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.bytes='Working folder: /fixture\n\ndiff --git a/a.js b/a.js\n--- a/a.js\n+++ b/a.js\n@@ -1 +1 @@ first\n-old\n+new\n@@ -20 +20 @@ second\n-before\n+<script>literal()</script>\ndiff --git a/b.js b/b.js\n--- a/b.js\n+++ b/b.js\n@@ -1 +1 @@\n-old\n+new\n';
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
 await p.getByText('javascript · hunk fragments only; surrounding file context is unavailable.').first().waitFor();
 await p.getByLabel('Request changes to hunk 2 in a.js',{exact:true}).waitFor();
 assert.equal(await p.locator('[data-after-line="20"] code').textContent(),'+<script>literal()</script>');assert.equal(await p.locator('.working-hunk script').count(),0);
 await p.getByLabel('Hunk 1 in a.js',{exact:true}).click();
 assert.equal(await p.locator('.working-hunk').nth(0).evaluate(e=>e.open),false);
 await p.locator('.working-file > summary').nth(1).click();await p.getByLabel('Hunk 1 in b.js',{exact:true}).click();await p.locator('.working-file > summary').nth(1).click();
 await p.getByRole('button',{name:'syntax highlighting',exact:true}).click();
 const saved=await p.evaluate(()=>workspace.getView());assert.equal(saved.syntaxEnabled,false);assert.equal(saved.collapsedHunks.length,2);
 await p.evaluate(async saved=>{workspace.close();workspace=mount();await workspace.restoreView(saved);},saved);assert.equal(await p.getByRole('button',{name:'syntax highlighting',exact:true}).getAttribute('aria-pressed'),'false');
 assert.equal(await p.locator('.working-hunk').nth(0).evaluate(e=>e.open),false);
 await p.locator('.working-file > summary').nth(1).click();await p.getByLabel('Hunk 1 in b.js',{exact:true}).waitFor();assert.equal(await p.locator('.working-file').nth(1).locator('.working-hunk').evaluate(e=>e.open),false,'a hidden lazy file retains hunk state after reopening');
 await p.getByLabel('Request changes to hunk 2 in a.js',{exact:true}).click();
 assert.equal(await p.getByLabel('First reviewed line').inputValue(),'9');assert.equal(await p.getByLabel('Last reviewed line').inputValue(),'11');
 assert.match(await p.getByLabel('Review notes').inputValue(),/Hunk: @@ -20 \+20 @@ second/);
 await p.getByLabel('Review notes').fill((await p.getByLabel('Review notes').inputValue())+'Explain this change.');
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 const result=await p.evaluate(()=>({post:posts[0],discussion:discussed[0],text:bytes.split('\n').slice(posts[0].start-1,posts[0].end).join('\n')}));
 assert.equal(result.post.revision,'a'.repeat(64));assert.equal(result.post.start,9);assert.equal(result.post.end,11);assert.equal(result.discussion.reviewLineKind,'snapshot');assert.equal(result.discussion.revision,'a'.repeat(64));assert.equal(result.text,'@@ -20 +20 @@ second\n-before\n+<script>literal()</script>');
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.locator('.working-file').first().scrollIntoViewIfNeeded();await p.screenshot({path:'/tmp/manifest-hunk-review-phone.png'});
 // Multiline tokens are reconstructed per side, and prefixes/coordinates stay exact.
 await p.evaluate(()=>{const text='diff --git a/test.js b/test.js\n--- a/test.js\n+++ b/test.js\n@@ -1,3 +1,3 @@\n /* comment\n-old value\n+new value\n */\n';document.querySelector('main').replaceChildren(artifactWorkingChangesView(text));});
 await p.getByText('javascript · hunk fragments only; surrounding file context is unavailable.').waitFor();
 assert.deepEqual(await p.locator('.working-hunk .diff-line-text').allTextContents(),[' /* comment','-old value','+new value',' */']);
 assert.equal(await p.locator('[data-before-line="2"] .hljs-comment').textContent(),'old value');
 assert.equal(await p.locator('[data-after-line="2"] .hljs-comment').textContent(),'new value');
 assert.equal(await p.locator('.working-hunk .numbered-diff-row').evaluateAll(rows=>rows.every(row=>row.getBoundingClientRect().height<25)),true,'nested token spans must remain on the same source line');
 await p.screenshot({path:'/tmp/manifest-hunk-syntax-phone.png'});
 await p.getByRole('button',{name:'syntax highlighting',exact:true}).click();assert.equal(await p.locator('.hljs-comment').count(),0);
 assert.deepEqual(errors,[]);console.log('PASS: hunk syntax preserves prefixes, multiline tokens per side, immutable review ranges, toggle restoration and lazy collapse state');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
