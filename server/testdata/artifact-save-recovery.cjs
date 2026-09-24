const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage(),root=path.join(__dirname,'../web');await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main></main>'}));await p.goto('https://fixture.test');
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/47-chat-state.js'),'utf8')});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.chatRenderStateNotice=()=>{};window.mode='offline';window.saved=[];window.snap={key:'artifact-0123456789abcdef',slot:'edit',revision:0,value:null};
  window.artifact={id:'0123456789abcdef',ref:'file.txt',head:'a'.repeat(64),content:'original',revisions:[{n:1,hash:'a'.repeat(64)}]};
  window.fetch=async(url,opts={})=>{
   if(url.startsWith('/api/chat/state/')){
    if(opts.method==='PUT'){
     if(mode==='offline')throw Error('Offline');
     if(mode==='hold')await new Promise(resolve=>window.release=resolve);
     if(mode==='conflict')return {status:409,ok:false,json:async()=>({...snap,revision:10,value:{text:'Other device'}})};
     const body=JSON.parse(opts.body);snap={...snap,revision:snap.revision+1,value:body.value};
    }
    return {ok:true,json:async()=>structuredClone(snap)};
   }
   return {ok:true,json:async()=>structuredClone(artifact)};
  };
  window.mount=()=>artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(artifact),save:async(text,base)=>{saved.push({text,base,persisted:structuredClone(snap.value)});throw Error('Lost file-save acknowledgment');}});window.workspace=mount();
 });
 await p.getByRole('button',{name:'Edit',exact:true}).click();await p.getByLabel('File content').fill('recoverable edit');
 await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.getByText('The edit has not been confirmed',{exact:false}).waitFor();assert.equal(await p.evaluate(()=>saved.length),0);
 await p.evaluate(()=>mode='ok');await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.getByText('Lost file-save acknowledgment',{exact:true}).waitFor();
 const result=await p.evaluate(()=>saved[0]);assert.equal(result.persisted.text,'recoverable edit');assert.equal(result.persisted.baseRevision,result.base);assert.equal(result.persisted.artifact,'0123456789abcdef');
 // Reconstruct the controller as on a reload: the original text and base survive.
 await p.evaluate(()=>{workspace.close();artifactEditDrafts.clear();workspace=mount();});await p.getByRole('button',{name:'Resume draft',exact:true}).click();assert.equal(await p.getByLabel('File content').inputValue(),'recoverable edit');
 await p.getByLabel('File content').fill('second edit');await p.evaluate(()=>mode='hold');await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.waitForFunction(()=>typeof release==='function');
 assert.equal(await p.getByRole('button',{name:'Back to preview',exact:true}).isDisabled(),true);
 await p.evaluate(()=>document.querySelector('.artifact-primary-action').onclick());assert.equal(await p.evaluate(()=>saved.length),1);
 await p.evaluate(()=>{workspace.close();mode='ok';release();});await p.waitForFunction(()=>!artifactEditDrafts.get(artifact.id).pending);assert.equal(await p.evaluate(()=>saved.length),1);
 // A stale draft conflict prevents a file mutation.
 await p.evaluate(()=>{workspace=mount();});await p.getByRole('button',{name:'Resume draft',exact:true}).click();await p.getByLabel('File content').fill('conflicting edit');await p.evaluate(()=>mode='conflict');await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.getByText('The edit has not been confirmed',{exact:false}).waitFor();assert.equal(await p.evaluate(()=>saved.length),1);
 console.log('PASS: draft persistence before save, lost-ack text/base recovery, duplicate suppression, closed-pane cancellation and stale-draft conflict');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
