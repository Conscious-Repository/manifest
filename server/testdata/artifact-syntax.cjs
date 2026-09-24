const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill(r.request().url().endsWith('/vendor/artifact-syntax.js')?{contentType:'text/javascript',body:fs.readFileSync(path.join(root,'vendor/artifact-syntax.js'),'utf8')}:{contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.bytes='// exact source\r\nconst markup = "<img src=x onerror=alert(1)>";\r\nfunction square(n) { return n * n; }\r\n';
  window.a={id:'0123456789abcdef',ref:'code.js',head:'a'.repeat(64),content:bytes,revisions:[{n:1,hash:'a'.repeat(64)}]};window.posts=[];
  window.fetch=async(url,opts={})=>{
   if(url.startsWith('/api/artifacts/reviews')){
    if(opts.method==='POST'){const value=JSON.parse(opts.body);posts.push({...value,id:value.request_id,revision:a.head,at:new Date().toISOString()});}
    return {ok:true,json:async()=>({revision:a.head,record_version:String(posts.length),state:posts.at(-1)?.state||'not_requested',entries:posts})};
   }
   return {ok:true,json:async()=>structuredClone(a)};
  };
  window.mount=()=>artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(a),review:true});window.workspace=mount();
 });
 await p.getByText('javascript · exact source text',{exact:true}).waitFor();assert.ok(await p.locator('.hljs-keyword').count());assert.equal(await p.locator('.artifact-source-preview code').textContent(),await p.evaluate(()=>bytes));assert.equal(await p.locator('.artifact-code-preview img').count(),0);
 await p.getByRole('button',{name:'syntax highlighting',exact:true}).click();assert.equal(await p.locator('.hljs-keyword').count(),0);
 const view=await p.evaluate(()=>workspace.getView());await p.evaluate(async view=>{workspace.close();workspace=mount();await workspace.restoreView(view);},view);await p.getByText('javascript · exact source text',{exact:true}).waitFor();assert.equal(await p.getByRole('button',{name:'syntax highlighting',exact:true}).getAttribute('aria-pressed'),'false');
 await p.getByRole('button',{name:'syntax highlighting',exact:true}).click();assert.equal(await p.locator('.artifact-source-preview code').textContent(),await p.evaluate(()=>bytes));
 await p.locator('.artifact-review-controls > summary').click();await p.getByText('Specific lines',{exact:true}).click();await p.getByLabel('First reviewed line').fill('2');await p.getByLabel('Last reviewed line').fill('3');await p.getByRole('button',{name:'record decision',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();assert.equal(await p.evaluate(()=>posts[0].start),2);assert.equal(await p.evaluate(()=>posts[0].end),3);
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.locator('.artifact-code-preview').scrollIntoViewIfNeeded();await p.screenshot({path:'/tmp/manifest-syntax-phone.png'});
 // Every bundled grammar preserves text, including literal markup.
 const examples={js:'const x = 2;',ts:'let x: number = 2;',py:'def f():\n  return "x"',go:'package main\nfunc main() {}',json:'{"x":true}',sh:'echo "$HOME"',css:'body { color: red; }',html:'<script>alert(1)</script>',sql:'SELECT * FROM people;',yaml:'name: example'};
 for(const [ext,text] of Object.entries(examples)){await p.evaluate(({ext,text})=>{document.querySelector('main').replaceChildren(artifactCodePreview(text,ext));},{ext,text});await p.getByText('· exact source',{exact:false}).waitFor();assert.equal(await p.locator('.artifact-source-preview code').textContent(),text);}
 await p.evaluate(()=>document.querySelector('main').replaceChildren(artifactCodePreview('x'.repeat(65537),'js')));await p.getByText('plain text (syntax highlighting supports',{exact:false}).waitFor();assert.equal(await p.locator('.artifact-source-preview code').textContent(), 'x'.repeat(65537));
 await p.evaluate(()=>{window.manifestArtifactSyntax=()=>{throw Error('unavailable');};document.querySelector('main').replaceChildren(artifactCodePreview('const retained = true;','js'));});await p.getByText('syntax highlighting unavailable; original text retained.',{exact:false}).waitFor();assert.equal(await p.locator('.artifact-source-preview code').textContent(),'const retained = true;');
 assert.deepEqual(errors,[]);console.log('PASS: local lazy syntax, exact CRLF/markup, ten grammars, plain toggle restoration, immutable range review, bounded/error fallback and both-theme layout');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
