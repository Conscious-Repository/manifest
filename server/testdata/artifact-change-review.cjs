// Version comparison → exact-line change request → stale range/base evidence,
// with the working-file projection separating saved versions from the tree.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('node:assert/strict');
const v1=Array.from({length:30},(_,i)=>'line '+(i+1)).join('\n');
const v2=v1.replace('line 10\n','line ten\n').replace('line 20\n','line 20\ninserted a\ninserted b\n');
const h1='1'.repeat(64),h2='2'.repeat(64),sha=text=>crypto.createHash('sha256').update(text).digest('hex');
(async()=>{const b=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHANNEL?{channel:process.env.PLAYWRIGHT_CHANNEL}:{})});try{
const p=await b.newPage({viewport:{width:390,height:844}});const errors=[];p.on('pageerror',e=>errors.push(e.message));
await p.route('https://review.test/**',r=>r.fulfill({contentType:'text/html',body:'<main style="height:800px;display:flex"></main>'}));await p.goto('https://review.test');
const root=path.join(__dirname,'../web');for(const f of ['00-core','05-primitives','48-chat'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
await p.evaluate(({v1,v2,h1,h2})=>{
 window.renderMarkdown=text=>{const e=document.createElement('pre');e.textContent=text;return e;};
 window.artifact={id:'0123456789abcdef',title:'Brief',ref:'artifacts/library/brief.txt',head:h2,revisions:[{n:1,hash:h1,actor:'agent:hermes'},{n:2,hash:h2,actor:'owner',note:'Edited in chat'}],workingFile:{state:'differs',harness:'excalibur',ref:'artifacts/library/brief.txt',hash:h1,version:1}};
 window.bytes={[h1]:v1,[h2]:v2};window.posts=[];window.records=[];window.refuse=0;
 window.fetch=async(url,options={})=>{
  const u=new URL(url,location.href),rev=u.searchParams.get('rev')||u.searchParams.get('revision');
  if(u.pathname==='/api/artifacts/get')return{ok:true,json:async()=>({...structuredClone(artifact),content:bytes[rev],preview:{kind:'text',revision:rev}})};
  if(u.pathname==='/api/artifacts/reviews'){
   if(options.method==='POST'){const body=JSON.parse(options.body);posts.push({revision:rev,body});
    if(refuse){refuse--;return{ok:false,status:412,text:async()=>'selected lines do not match this version; reselect them'};}
    records.push({id:body.request_id,revision:rev,state:body.state,note:body.note,start:body.start,end:body.end,range_hash:body.range_hash,at:new Date().toISOString()});}
   const anchors={};for(const r of records)if(r.start&&r.revision!==artifact.head)anchors[r.id]={state:'moved',start:r.start+2,end:r.end+2};
   const n=artifact.revisions.find(r=>r.hash===rev).n;
   return{ok:true,status:200,json:async()=>({record_version:String(records.length),revision:rev,state:records.filter(r=>r.revision===rev).at(-1)?.state||'not_requested',entries:structuredClone(records),version:n,head:artifact.head,head_version:artifact.revisions.length,anchors})};
  }
  throw Error('unexpected '+url);
 };
},{v1,v2,h1,h2});
await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
await p.evaluate(()=>{window.ws=artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(artifact),review:true});});

// Saving a version is not changing the tree: the file still holds v1.
const working=p.locator('.artifact-working-file');await working.waitFor();
assert.equal(await working.getAttribute('data-state'),'differs');
assert.match(await working.innerText(),/holds version 1, not the latest version 2\. Saved versions have not been written to it\./);
assert.equal(await p.locator('.artifact-review-stale').isHidden(),true,'the latest version is not stale');

// Compare folds unchanged context and offers exact newer-line review.
await p.getByRole('button',{name:'Compare v1',exact:true}).click();
await p.locator('.artifact-diff').waitFor();
const folds=p.locator('.artifact-diff-fold');assert.equal(await folds.count(),3);
assert.equal(await p.locator('.artifact-diff .numbered-diff-row').count(),16,'context rows around each change only');
await folds.first().click();assert.equal(await folds.count(),2);
assert.equal(await p.locator('.artifact-diff .numbered-diff-row').count(),22);
await p.getByRole('button',{name:'Request changes to lines 21–22 of version 2',exact:true}).waitFor();
await p.getByRole('button',{name:'Request changes to line 10 of version 2',exact:true}).click();
assert.equal(await p.locator('.artifact-review-controls').evaluate(d=>d.open),true);
assert.equal(await p.getByLabel('Review decision').inputValue(),'changes_requested');
assert.equal(await p.getByLabel('First reviewed line').inputValue(),'10');
assert.match(await p.getByLabel('Review notes').inputValue(),/^File: artifacts\/library\/brief\.txt\n/);
await p.getByLabel('Review notes').fill('Spell the number consistently.');

// A refused range unlocks the form with the notes preserved; nothing records.
await p.evaluate(()=>refuse=1);
await p.getByRole('button',{name:'record decision',exact:true}).click();
await p.getByText('selected lines do not match this version; reselect them').waitFor();
assert.equal(await p.getByLabel('Review notes').isDisabled(),false);assert.equal(await p.getByLabel('Review notes').inputValue(),'Spell the number consistently.');
assert.equal(await p.evaluate(()=>records.length),0);
await p.getByRole('button',{name:'record decision',exact:true}).click();
await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
const posts=await p.evaluate(()=>posts);assert.equal(posts.length,2);
for(const post of posts){assert.equal(post.revision,h2);assert.equal(post.body.start,10);assert.equal(post.body.end,10);assert.equal(post.body.range_hash,sha('line ten'),'fingerprint of the exact displayed line');}
assert.notEqual(posts[0].body.request_id,posts[1].body.request_id,'a refused request is not replayed under its identity');

// A newer head makes the recorded decision visibly version-bound, with the
// range's position in the latest version.
await p.evaluate(({v2})=>{const h3='3'.repeat(64);bytes[h3]=v2.replace('line 1\n','line 0\nline 1\nline 1b\n');artifact.revisions.push({n:3,hash:h3,actor:'agent:codex'});artifact.head=h3;},{v2});
await p.evaluate(h2=>{ws.close();window.ws=artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(artifact),review:true,revision:h2});},h2);
await p.getByText('Review · Changes requested · v3 is newer',{exact:true}).click();
assert.equal(await p.locator('.artifact-review-stale').innerText(),'Version 3 is newer. Decisions here apply to version 2 only.');
assert.equal(await p.locator('.artifact-review-anchor').innerText(),'Now line 12 in latest version 3.');
assert.equal(await p.locator('.artifact-review-anchor').getAttribute('data-anchor'),'moved');

for(const theme of ['','jarvis'])for(const width of [320,390,1440]){await p.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await p.setViewportSize({width,height:844});assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,theme+' '+width);}
await p.setViewportSize({width:390,height:844});await p.evaluate(()=>document.documentElement.dataset.theme='');
await p.getByRole('button',{name:'Compare v1',exact:true}).click();await p.locator('.artifact-diff-fold').first().waitFor();
const touch=await p.locator('.artifact-diff-fold,.artifact-diff-review-block').evaluateAll(els=>els.map(e=>e.getBoundingClientRect().height));assert.ok(touch.every(h=>h>=44),'touch floor '+touch);
await p.screenshot({path:'/tmp/manifest-artifact-change-review-phone.png'});
assert.deepEqual(errors,[]);
console.log('PASS: working-file state, folded compare, exact newer-line change request with range fingerprint, refused stale range, version-bound decision after a newer head, both-theme 320/390/1440 bounds');
}finally{await b.close()}})().catch(e=>{console.error(e);process.exit(1)});
