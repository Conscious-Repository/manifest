const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage({viewport:{width:390,height:844}}),root=path.join(__dirname,'../web'),errors=[];p.on('pageerror',e=>errors.push(e.message));
 await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main></main>'}));await p.goto('https://fixture.test');
 for(const f of ['00-core','05-primitives'])await p.addStyleTag({content:fs.readFileSync(path.join(root,'css',f+'.css'),'utf8')});
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.artifact={id:'0123456789abcdef',title:'Review target',ref:'code.go'};window.revision='a'.repeat(64);window.entries=[];window.posts=[];window.drafts=[];window.mode='drop-after';
  window.fetch=async(url,opts={})=>{
   if(opts.method==='POST'){
    const request=JSON.parse(opts.body);posts.push(request);
    if(mode==='hold')await new Promise(resolve=>window.release=resolve);
    if(mode==='drop-before')throw Error('Connection lost before response');
    if(mode==='conflict')return {status:409,ok:false,json:async()=>({revision,record_version:'new-version',entries,state:'not_requested'})};
    if(!entries.some(e=>e.id===request.request_id))entries.push({...request,id:request.request_id,revision,at:new Date().toISOString()});
    if(mode==='drop-after')throw Error('Connection lost after recording');
   }
   return {ok:true,json:async()=>({revision,record_version:String(entries.length),state:entries.at(-1)?.state||'not_requested',entries})};
  };
  window.mount=()=>{document.querySelector('main').replaceChildren(artifactReviewControls(artifact,revision,1,ref=>drafts.push(ref)));document.querySelector('details').open=true;};mount();
 });
 const notes=p.getByLabel('Review notes'),decision=p.getByLabel('Review decision');
 await decision.selectOption('changes_requested');await notes.fill('Change only the selected version.');
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'retry decision',exact:true}).waitFor();
 assert.equal(await notes.isDisabled(),true);assert.equal(await decision.isDisabled(),true);
 await p.evaluate(()=>mount());await p.getByRole('button',{name:'draft recorded request',exact:true}).waitFor();
 assert.equal(await p.evaluate(()=>posts.length),1);assert.equal(await p.evaluate(()=>drafts.length),0);
 assert.equal(await p.getByRole('button',{name:'recorded',exact:true}).isDisabled(),true);
 await p.evaluate(()=>mount());await p.getByRole('button',{name:'draft recorded request',exact:true}).click();assert.equal(await p.evaluate(()=>drafts[0].revision),'a'.repeat(64));assert.equal(await p.evaluate(()=>drafts.length),1);
 // A request absent from history is retried explicitly with the original bytes and ID.
 await notes.fill('Second request.');await p.evaluate(()=>mode='drop-before');
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'retry decision',exact:true}).waitFor();
 await p.evaluate(()=>mount());await p.getByRole('button',{name:'retry decision',exact:true}).waitFor();
 assert.equal(await p.evaluate(()=>posts.length),2);assert.equal(await notes.inputValue(),'Second request.');assert.equal(await notes.isDisabled(),true);
 await p.evaluate(()=>mode='hold');await p.getByRole('button',{name:'retry decision',exact:true}).click();
 await p.waitForFunction(()=>typeof release==='function');
 await p.evaluate(()=>{document.querySelector('.form-actions button').onclick();document.querySelector('.artifact-review-controls').prepareChange({path:'other.go',start:5,end:6});});
 assert.equal(await p.evaluate(()=>posts.length),3);assert.equal(await notes.inputValue(),'Second request.');
 await p.evaluate(()=>{mode='ok';release();});await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 assert.equal(await p.evaluate(()=>JSON.stringify(posts[1])===JSON.stringify(posts[2])),true);
 assert.equal(await p.evaluate(()=>entries.length),2);assert.equal(await p.evaluate(()=>drafts.length),2);
 // A definitive conflict unlocks correction and the next request gets a new identity.
 await notes.fill('Third request.');await p.evaluate(()=>mode='conflict');await p.getByRole('button',{name:'record and draft request',exact:true}).click();
 await p.getByText('Review changed elsewhere.',{exact:false}).waitFor();assert.equal(await notes.isDisabled(),false);assert.equal(await notes.inputValue(),'Third request.');
 await p.evaluate(()=>mount());await p.getByRole('button',{name:'record and draft request',exact:true}).waitFor();assert.equal(await notes.inputValue(),'Third request.');await p.evaluate(()=>mode='ok');await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByRole('button',{name:'recorded',exact:true}).waitFor();
 assert.equal(await p.evaluate(()=>posts[3].request_id!==posts[4].request_id),true);
 // Local persistence is a prerequisite for submission.
 await notes.fill('Must remain unsent.');await p.evaluate(()=>Storage.prototype.setItem=()=>{throw Error('Storage unavailable');});
 await p.getByRole('button',{name:'record and draft request',exact:true}).click();await p.getByText('Could not preserve the decision',{exact:false}).waitFor();assert.equal(await p.evaluate(()=>posts.length),5);
 assert.deepEqual(errors,[]);console.log('PASS: lost-ack receipt recovery, explicit same-ID retry, in-flight freezing, conflict correction and storage failure without submission');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
