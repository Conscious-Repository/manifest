const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const b=await chromium.launch({headless:true});try{
 const p=await b.newPage(),root=path.join(__dirname,'../web');await p.route('https://fixture.test/**',r=>r.fulfill({contentType:'text/html',body:'<main></main>'}));await p.goto('https://fixture.test');
 await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/47-chat-state.js'),'utf8')});await p.addScriptTag({content:fs.readFileSync(path.join(root,'js/05-components.js'),'utf8')});
 await p.evaluate(()=>{
  window.chatRenderStateNotice=()=>{};window.mode='ok';window.saved=[];window.snap={key:'artifact-0123456789abcdef',slot:'edit',revision:0,value:null};
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
   const rev=new URL(url,location.origin).searchParams.get('rev');return {ok:true,json:async()=>({...structuredClone(artifact),content:rev==='b'.repeat(64)?'saved text':artifact.content})};
  };
  window.mount=()=>artifactWorkspace(document.querySelector('main'),{load:async()=>structuredClone(artifact),receiptSave:true,save:async(text,base,requestID)=>{saved.push({text,base,requestID,persisted:structuredClone(snap.value)});if(saved.length===1){artifact.revisions.push({n:2,hash:'b'.repeat(64)});artifact.head='b'.repeat(64);throw Error('Lost file-save acknowledgment');}return {...structuredClone(artifact),savedRevision:'b'.repeat(64),savedVersion:2,saveRequestID:requestID};}});window.workspace=mount();
 });
 await p.getByRole('button',{name:'Edit',exact:true}).click();await p.getByLabel('File content').fill('saved text');
 await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.getByText('Lost file-save acknowledgment',{exact:true}).waitFor();
 const first=await p.evaluate(()=>saved[0]);assert.ok(first.requestID);assert.equal(first.persisted.saveRequestID,first.requestID);
 await p.evaluate(()=>{workspace.close();artifactEditDrafts.clear();artifact.revisions.push({n:3,hash:'c'.repeat(64)});artifact.head='c'.repeat(64);artifact.content='newer work';workspace=mount();});
 await p.getByRole('button',{name:'Resume draft',exact:true}).click();assert.equal(await p.getByLabel('File content').inputValue(),'saved text');
 await p.getByRole('button',{name:'Save new version',exact:true}).click();await p.getByText('Saved version recovered.',{exact:false}).waitFor();
 const retry=await p.evaluate(()=>saved[1]);assert.equal(retry.requestID,first.requestID);assert.equal(retry.base,first.base);assert.equal(retry.text,first.text);
 assert.equal(await p.getByLabel('Artifact version').inputValue(),'2');assert.equal(await p.locator('.artifact-source-preview').textContent(),'saved text');
 assert.equal(await p.evaluate(()=>artifact.head),'c'.repeat(64));assert.equal(await p.evaluate(()=>snap.value),null);
 console.log('PASS: persisted save identity survives reload, original save receipt opens exact version while newer work remains current');
}finally{await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
