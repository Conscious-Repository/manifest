const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main style="height:760px;display:flex"></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.first='a'.repeat(64);window.second='b'.repeat(64);
  window.artifact={id:'0123456789abcdef',title:'Imported file',ref:'misleading.txt',head:second,revisions:[{n:1,hash:first},{n:2,hash:second}]};window.posts=[];window.entries=[];window.gets=[];
  window.fetch=async(url,opts={})=>{
   const q=new URL(url,location.origin).searchParams;
   if(url.startsWith('/api/artifacts/reviews')){
    const revision=q.get('revision');
    if(opts.method==='POST'){const body=JSON.parse(opts.body);posts.push({revision,...body});entries.push({...body,id:body.request_id,revision,at:new Date().toISOString()});}
    return {ok:true,json:async()=>({record_version:String(entries.length),revision,state:entries.at(-1)?.state||'not_requested',entries})};
   }
   gets.push(url);const rev=q.get('rev')||second;
   return {ok:true,json:async()=>({...artifact,content:rev===second?'new text':undefined,preview:{revision:rev,kind:rev===first?'metadata':'text',mediaType:rev===first?'application/octet-stream':'text/plain; charset=utf-8',size:rev===first?6:8,reason:rev===first?'This file is not UTF-8 text and has no supported inline preview. Open the original file to inspect it.':undefined}})};
  };
  window.workspace=artifactWorkspace(document.querySelector('main'),{review:true,load:async()=>structuredClone(artifact),save:async()=>{throw Error('Unexpected save');},onDiscuss:()=>{throw Error('Unexpected discussion');}});
 });
 await p.getByRole('button',{name:'Compare v1',exact:true}).click();
 await p.getByText('Could not compare versions:',{exact:false}).waitFor();
 assert.equal(await p.locator('.artifact-diff').count(),0);
 await p.getByLabel('Artifact version').selectOption('1');await p.locator('.artifact-metadata-preview').waitFor();
 assert.match(await p.locator('.artifact-metadata-preview').textContent(),/6 bytes/);
 assert.match(await p.getByRole('link',{name:'Open file ↗'}).getAttribute('href'),new RegExp('rev='+'a'.repeat(64)));
 assert.equal(await p.getByRole('button',{name:/^(Edit|Restore this version|Discuss|Compare v)/}).count(),0);
 await p.locator('.artifact-review-controls > summary').click();
 assert.equal(await p.getByLabel('First reviewed line').isVisible(),false);
 await p.getByRole('button',{name:'record decision',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 const post=await p.evaluate(()=>posts[0]);assert.equal(post.revision,'a'.repeat(64));assert.equal(post.start,0);assert.equal(post.end,0);assert.equal(post.state,'accepted');
 for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);}
 await p.setViewportSize({width:390,height:844});await p.screenshot({path:'/tmp/manifest-metadata-review-phone.png'});
 await p.getByLabel('Artifact version').selectOption('2');await p.getByRole('button',{name:'Edit',exact:true}).waitFor();
 assert.equal(await p.locator('.artifact-source-preview').textContent(),'new text');
 assert.equal(await p.evaluate(()=>gets.every(url=>url.includes('preview=1'))),true);
 assert.deepEqual(errors,[]);console.log('PASS: version-specific generic metadata, exact download and review, unavailable comparison, text switching and both-theme phone/desktop bounds');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
