// Writing owns its editor instances. Route changes detach DOM, never discard a
// buffer; network acknowledgements refer to the captured document and bytes.
const writingUI = {documents:new Map(), files:[], folders:[], active:null, vaultID:'', view:null, request:0};
// A fresh page identity also distinguishes browser-duplicated tabs (which copy sessionStorage).
const writingWindow = crypto.randomUUID();
function writeEOL(raw) {
  const crlf=(raw.match(/\r\n/g)||[]).length, lf=(raw.match(/\n/g)||[]).length;
  return {text:raw.replace(/\r\n/g,'\n'),eol:crlf?'\r\n':'\n',mixed:(crlf>0&&crlf!==lf)||/\r(?!\n)/.test(raw)};
}
function writeBytes(d,text=d.editor.text()){return d.eol==='\r\n'?text.replace(/\n/g,'\r\n'):text}
function writeDirty(d){return writeBytes(d)!==d.base}
async function writeFetch(url,body,method='POST'){
  const r=await fetch(url,{signal:AbortSignal.timeout(30000),...(body===undefined?{}:{method,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})});
  if(!r.ok){const text=await r.text();let detail;try{detail=JSON.parse(text)}catch(e){}const error=new Error(detail?.missing?'This file was deleted or moved outside Manifest.':detail?.revision?'This file changed outside this editor.':text);error.status=r.status;error.detail=detail;throw error}
  return r.json();
}
function writeRecoveryKey(d){return 'manifest.writing.draft.'+writingUI.vaultID+'.'+encodeURIComponent(d.path)+'.'+writingWindow}
function writeRecover(d){
  try{
    if(!writeDirty(d)){localStorage.removeItem(writeRecoveryKey(d));return}
    localStorage.setItem(writeRecoveryKey(d),JSON.stringify({path:d.path,base:d.base,revision:d.revision,text:d.editor.text(),at:Date.now(),window:writingWindow}));
    d.recoveryError='';
  }catch(e){d.recoveryError='Local recovery is unavailable. Keep this tab open or export your draft.'}
}
function writeStatus(d){
  if(writingUI.active!==d)return;
  writingUI.status.textContent=d.saving?'saving…':d.error||d.recoveryError||(d.readOnly?'read only':writeDirty(d)?'saving…':'saved');
  writingUI.save.disabled=d.saving||d.readOnly||!writeDirty(d);
  writingUI.title.textContent=d.path.replace(/^.*\//,'').replace(/\.md$/i,'');
  writingUI.title.hidden=true;writingUI.path.textContent=d.path.replace(/\.md$/i,'').split('/').join(' / ');writingUI.path.title=d.path;writingUI.documentActions?.forEach(b=>b.hidden=false);writingUI.save.hidden=!writeDirty(d)||!d.error||d.saving;
  const count=(d.editor.text().replace(/^---\n[\s\S]*?\n---(?:\n|$)/,'').match(/[\p{L}\p{N}]+(?:['’-][\p{L}\p{N}]+)*/gu)||[]).length;writingUI.count.textContent=count+' '+(count===1?'word':'words');writeTabs();
}
function writeBuild(){
  if(writingUI.view)return;
  const view=document.getElementById('writeView');writingUI.view=view;
  const head=el('header','write-head');
  const identity=el('div','write-identity');const title=el('h1','write-title','Writing');
  const path=el('span','write-path','');identity.append(title,path);
  const actions=el('div','write-actions write-toolbar');
  const open=pillLight('open',()=>writeOpenPicker()),create=pillLight('new file',()=>writeCreate());
  const comments=pillLight('comments',()=>writeToggleMargin());
  const save=pillLight('save',()=>writeSave(writingUI.active));
  const more=pillLight('•••',()=>writeMenu());more.setAttribute('aria-label','Document actions');
  actions.append(open,create,comments,save,more);head.append(identity,actions);
  const tabs=el('div','write-tabs');tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Open documents');
  const work=el('div','write-work');const canvas=el('div','write-canvas');
  const notice=el('div','write-notice');notice.hidden=true;
  const editorHost=el('div','write-editor-host');
  const margin=el('aside','write-margin');margin.hidden=true;margin.setAttribute('aria-label','Document conversations');
  canvas.append(notice,editorHost);work.append(canvas,margin);
  const foot=el('footer','write-foot');const status=el('span','write-status');status.setAttribute('role','status');
  const count=el('span','write-count');foot.append(status,count);
  const selection=el('div','write-selection');selection.hidden=true;
  selection.setAttribute('role','group');selection.setAttribute('aria-label','Selected text actions');
  for(const label of ['comment','ask']){const button=el('button','write-selection-action',label);button.type='button';button.onclick=()=>writeCompose(label);selection.append(button)}
  selection.onkeydown=e=>{if(e.key==='Escape'){e.preventDefault();selection.hidden=true;writingUI.active?.editor.focus()}};canvas.append(selection);
  view.append(tabs,head,work,foot);
  Object.assign(writingUI,{documentActions:[comments,save,more],count,title,path,save,status,tabs,canvas,notice,editorHost,margin,selection});
}
async function showWriting(p){
  writeBuild();const request=++writingUI.request;
  writingUI.view.hidden=false;writingUI.selection.hidden=true;
  try{const list=await writeFetch('/api/writing/files');if(request!==writingUI.request)return;writingUI.files=list.files;writingUI.folders=list.folders;writingUI.vaultID=list.vaultID}
  catch(e){writingUI.status.textContent=e.message}
  if(p){await writeOpen(p);return}
  if(writingUI.active){writeMount(writingUI.active);return}
  writingUI.documentActions.forEach(b=>b.hidden=true);writingUI.title.hidden=false;writingUI.count.textContent='';writingUI.title.textContent='Writing';writingUI.path.textContent='';writingUI.notice.hidden=true;writingUI.margin.hidden=true;writeTabs();writingUI.editorHost.replaceChildren();
  const home=el('div','write-welcome');home.append(el('span','micro-label','YOUR VAULT'),el('h2','','A place to think in words.'),el('p','','Start a note, or pick up where you left off.'));
  const actions=el('div','write-actions');actions.append(pillLight('new file',()=>writeCreate()),pillLight('open a file',()=>writeOpenPicker()));home.append(actions);
  const recent=[...writingUI.files].sort((a,b)=>b.modified-a.modified).slice(0,8);
  if(recent.length){home.append(el('div','micro-label','RECENTLY UPDATED'));recent.forEach(f=>{const b=pillLight(f.name,()=>writeNavigate(f.path));b.classList.add('write-recent');b.title=f.path;home.append(b)})}
  writingUI.editorHost.append(home);writingUI.status.textContent='';writingUI.save.disabled=true;
}
function writeNavigate(p){const hash='#/write/'+encodeURIComponent(p);if(location.hash===hash)writeOpen(p);else location.hash=hash}
async function writeOpen(p){
  if(writingUI.documents.has(p)){writeMount(writingUI.documents.get(p));return}
  const request=++writingUI.request;writingUI.status.textContent='opening…';
  try{
    const note=await writeFetch('/api/note?path='+encodeURIComponent(p));if(request!==writingUI.request)return;
    const eol=writeEOL(note.raw),d={path:note.path,base:note.raw,revision:note.revision,eol:eol.eol,readOnly:note.readOnly||eol.mixed,source:false,comments:null,error:eol.mixed?'Mixed line endings: read only to preserve exact bytes.':'',pending:null};
    writingUI.vaultID=note.vaultID;
    const host=el('div','write-editor');
    d.recovering=true;
    d.editor=ManifestEditor.create(host,{text:eol.text,readOnly:d.readOnly,files:()=>writingUI.files,openComment:id=>{d.expanded=id;writeRenderComments(d);writingUI.margin.hidden=false;if(window.innerWidth<=1100&&window.mfSheet)writeMarginSheet()},openLink:target=>{const p=target.split("#")[0];const found=writingUI.files.find(f=>f.path.replace(/\.md$/i,"").toLowerCase()===p.toLowerCase()||f.name.toLowerCase()===p.toLowerCase());if(found)writeNavigate(found.path);else showToast("Linked note not found",null,"info")},save:()=>writeSave(d),comment:()=>writeCompose(),change:()=>writeChanged(d),selection:range=>writeSelection(d,range)});
    d.host=host;writingUI.documents.set(p,d);
    try{const pref=JSON.parse(localStorage.getItem(writePreferenceKey(d))||'null');if(pref){d.source=!!pref.source;d.editor.setSource(d.source);d.expanded=pref.expanded;if(pref.revision===d.revision){d.scroll=pref.scroll;d.editor.restore(pref.anchor||0,pref.head||0)}}}catch(e){}
    d.editor.view.scrollDOM.addEventListener('scroll',()=>{if(writingUI.active===d)writingUI.selection.hidden=true});
    d.editor.view.scrollDOM.addEventListener('scroll',debounce(()=>writeRemember(d),150));
    writeMount(d);writeLoadComments(d);
    const recoveries=[];try{const prefix='manifest.writing.draft.'+writingUI.vaultID+'.'+encodeURIComponent(d.path)+'.';for(let i=0;i<localStorage.length;i++){const key=localStorage.key(i);if(key.startsWith(prefix)){const draft=JSON.parse(localStorage.getItem(key));if(draft.text!==eol.text)recoveries.push({key,...draft})}}}catch(e){d.recoveryError='Could not read local recovery data.';writeStatus(d)}
    if(recoveries.length&&!d.readOnly){
      const choice=await choosePath({title:'Recover unsaved writing?',items:recoveries.sort((a,b)=>b.at-a.at).map(x=>({label:'recover draft',detail:new Date(x.at).toLocaleString()+(x.window!==writingWindow?' · another tab':''),value:x})).concat([{label:'keep the saved version; retain recovery copies',value:null}])});
      if(choice?.value){const x=choice.value;d.base=x.base;d.revision=x.revision;d.editor.setText(x.text);writeRecover(d);writeStatus(d)}
    }
    d.recovering=false;writeQueueSave(d);writeRefresh(d);writeStartRefresh();
  }catch(e){writingUI.status.textContent=e.message}
}
function writeMount(d){
  const old=writingUI.active;if(old){old.scroll=old.editor.view.scrollDOM.scrollTop;if(old!==d)writeFlush(old)}
  writingUI.active=d;writingUI.editorHost.replaceChildren(d.host);
  d.editor.view.requestMeasure();if(d.scroll!==undefined)d.editor.view.scrollDOM.scrollTop=d.scroll;
  writingUI.notice.hidden=!d.conflict; if(d.conflict)writeConflict(d);
  writingUI.selection.hidden=true;writeStatus(d);writeRenderComments(d);writeMark(d);
  writeRefresh(d);
}
function writeTabs(){
  const signature=JSON.stringify([...writingUI.documents.values()].map(d=>[d.path,writeDirty(d),d===writingUI.active]));
  if(signature===writingUI.tabsSignature)return;writingUI.tabsSignature=signature;
  writingUI.tabs.replaceChildren();
  const docs=[...writingUI.documents.values()];
  docs.forEach((d,i)=>{const row=el('div','write-tab'+(d===writingUI.active?' on':''));
    const b=el('button','write-tab-label',d.path.replace(/^.*\//,'').replace(/\.md$/i,'')+(writeDirty(d)?' •':''));b.onclick=()=>writeNavigate(d.path);b.setAttribute('role','tab');b.setAttribute('aria-selected',String(d===writingUI.active));b.tabIndex=d===writingUI.active?0:-1;b.title=d.path;
    b.onkeydown=e=>{let n;if(e.key==='ArrowRight')n=(i+1)%docs.length;else if(e.key==='ArrowLeft')n=(i+docs.length-1)%docs.length;else if(e.key==='Home')n=0;else if(e.key==='End')n=docs.length-1;else return;e.preventDefault();writeMount(docs[n]);writeNavigate(docs[n].path);writingUI.tabs.querySelector('[aria-selected="true"]')?.focus()};
    const close=writeIcon('×','Close '+d.path,()=>writeClose(d));row.append(b,close);writingUI.tabs.append(row);
  });
}
function writeIcon(text,label,action){const b=el('button','write-icon',text);b.setAttribute('aria-label',label);b.title=label;b.onclick=action;return b}
async function writeClose(d){
  if(d.posting||d.moving)return;
  if(d.pendingBody?.trim()||Object.values(d.replyDrafts||{}).some(v=>v.trim())){const choice=await choosePath({title:'Unsent comment',items:[{label:'keep writing',value:'keep'},{label:'discard comment and close',value:'close'}]});if(choice?.value!=='close')return}
  if(d.saving||writeDirty(d))await writeSave(d);
  if(writeDirty(d)){const choice=await choosePath({title:'Unsaved changes',items:[{label:'keep writing',value:'cancel'},{label:'close; retain a local recovery copy',value:'close'}]});if(choice?.value!=='close')return;writeRecover(d)}
  const docs=[...writingUI.documents.values()],index=docs.indexOf(d),adjacent=docs[index+1]||docs[index-1];
  d.closed=true;writeClearSaveTimers(d);clearTimeout(d.commentPoll);writingUI.documents.delete(d.path);d.editor.destroy();if(writingUI.active===d){writingUI.active=null;const next=adjacent;if(next)writeNavigate(next.path);else{location.hash='#/write';showWriting('')}}else writeTabs();
}
async function writeSave(d){
  if(!d||d.closed||d.readOnly||d.recovering||d.moving||d.conflict)return false;
  if(d.savePromise){if(!await d.savePromise)return false;return writeSave(d)}
  if(!writeDirty(d))return true;
  writeClearSaveTimers(d);
  const raw=writeBytes(d);d.saving=true;d.saveEpoch=(d.saveEpoch||0)+1;d.error='';writeRecover(d);writeStatus(d);
  d.savePromise=(async()=>{
    try{const result=await writeFetch('/api/note',{path:d.path,body:raw,ifRevision:d.revision},'PUT');d.base=raw;d.revision=result.revision;d.retryDelay=0;d.conflict=null;if(writingUI.active===d)writingUI.notice.hidden=true;writeRecover(d);return true}
    catch(e){d.error=e.status?e.message:'Could not save. Retrying…';if(e.status===409&&e.detail){
      // A lost acknowledgement is safe to accept when the server has exactly
      // the bytes we submitted. Never overwrite a different external version.
      if(e.detail.raw===raw&&!e.detail.missing){d.base=raw;d.revision=e.detail.revision;d.error='';writeRecover(d);return true}
      d.conflict=e.detail;writeConflict(d);
    }else if(!e.status||e.status>=500)d.retryDelay=Math.min((d.retryDelay||1000)*2,30000);return false}
    finally{d.saving=false;d.savePromise=null;writeStatus(d);if(!d.error||d.retryDelay)writeQueueSave(d,d.retryDelay||800)}
  })();
  return d.savePromise;
}

function writeClearSaveTimers(d){clearTimeout(d.saveTimer);clearTimeout(d.saveMaxTimer);d.saveTimer=d.saveMaxTimer=null}
function writeQueueSave(d,delay=800){
  if(d.closed||d.readOnly||d.recovering||d.applying||d.moving||d.posting||d.conflict||!writeDirty(d))return;
  clearTimeout(d.saveTimer);d.saveTimer=setTimeout(()=>writeFlush(d),delay);
  if(!d.saveMaxTimer)d.saveMaxTimer=setTimeout(()=>writeFlush(d),Math.max(5000,delay));
}
function writeFlush(d){if(!d||d.closed||d.conflict||d.recovering||d.moving||d.posting)return;writeClearSaveTimers(d);if(d.editor.view?.composing){writeQueueSave(d);return}if(!d.saving)void writeSave(d)}
function writeChanged(d){
  if(d.applying)return;
  d.editEpoch=(d.editEpoch||0)+1;if(!d.conflict)d.error='';writeRecover(d);writeStatus(d);writeQueueSave(d);
  queueMicrotask(()=>{if(!d.closed&&writingUI.documents.has(d.path))writeMark(d)});
}
function writeApplySaved(d,note){
  const eol=writeEOL(note.raw);d.applying=true;
  try{d.base=note.raw;d.revision=note.revision;d.eol=eol.eol;d.readOnly=!!note.readOnly||eol.mixed;d.editor.syncText(eol.text);d.editor.setReadOnly(d.readOnly)}finally{d.applying=false}
  d.conflict=null;d.error=eol.mixed?'Mixed line endings: read only to preserve exact bytes.':'';d.retryDelay=0;d.selection=null;
  if(writingUI.active===d){writingUI.notice.hidden=true;writingUI.selection.hidden=true}
  writeRecover(d);writeStatus(d);writeMark(d);
}
async function writeRefresh(d){
  if(!d||d.closed||d.refreshing||d.saving||d.recovering||d.moving||d.posting||d.editor.view?.composing)return;
  const path=d.path,revision=d.revision,saveEpoch=d.saveEpoch,editEpoch=d.editEpoch;d.refreshing=true;
  try{
    const r=await fetch('/api/note?path='+encodeURIComponent(path),{headers:d.conflict?{}:{'If-None-Match':'"'+revision+'"'},cache:'no-store',signal:AbortSignal.timeout(10000)});
    if(r.status===304)return;
    const note=r.ok?await r.json():null;
    if(d.closed||path!==d.path||d.moving||d.saving||d.recovering||d.posting||d.editor.view?.composing||revision!==d.revision||saveEpoch!==d.saveEpoch||editEpoch!==d.editEpoch)return;
    if(r.status===404){d.conflict={missing:true};d.error='This file was deleted or moved outside Manifest.';writeClearSaveTimers(d);writeConflict(d);writeStatus(d);return}
    if(!note)return;
    if(note.revision===d.revision){if(d.conflict){d.conflict=null;d.error='';if(writingUI.active===d)writingUI.notice.hidden=true;writeStatus(d);writeQueueSave(d)}return}
    if(writeDirty(d)&&note.raw!==writeBytes(d)){d.conflict={raw:note.raw,revision:note.revision};d.error='This file changed outside this editor.';writeClearSaveTimers(d);writeConflict(d);writeStatus(d);return}
    writeApplySaved(d,note);
  }catch(e){/* Keep the editor and its recovery draft usable while offline. */}
  finally{d.refreshing=false}
}
function writeStartRefresh(){
  if(writingUI.refreshTimer)return;
  const tick=()=>{writingUI.refreshTimer=null;if(!writingUI.documents.size)return;if(!document.hidden&&!writingUI.view.hidden)writeRefresh(writingUI.active);writingUI.refreshTimer=setTimeout(tick,2000)};
  writingUI.refreshTimer=setTimeout(tick,2000);
}
function writeConflict(d){
  if(writingUI.active!==d)return;
  const box=writingUI.notice;box.replaceChildren();box.hidden=false;
  box.append(el('strong','','The saved file changed. Your writing is still here.'));
  const other=el('details','write-compare');other.append(el('summary','','compare with the current file'));const pre=el('pre','',d.conflict.missing?'The file was deleted or moved.':d.conflict.raw);other.append(pre);box.append(other);
  const actions=el('div','write-actions');actions.append(pillLight('export my draft',()=>writeExport(d)));
  if(!d.conflict.missing){actions.append(pillLight('save mine over this version',async()=>{d.revision=d.conflict.revision;d.conflict=null;await writeSave(d)}),pillLight('use this saved version',()=>writeApplySaved(d,d.conflict)))}
  box.append(actions);
}
async function writeOpenPicker(){
  try{const data=await writeFetch('/api/writing/files');writingUI.files=data.files;writingUI.folders=data.folders;const choice=await choosePath({title:'Open a file',items:data.files.map(f=>({label:f.name,detail:f.path,value:f.path}))});if(choice)writeNavigate(choice.value)}catch(e){showToast(e.message,null,'error')}
}
async function writeCreate(){
  const choice=await choosePath({title:'New file · vault root',placeholder:'name your note…',items:[],createLabel:'create'});if(!choice)return;
  let p=choice.value;if(!/\.md$/i.test(p))p+='.md';
  if(p.includes('/')||p.includes('\\')){showToast('Use a filename here; move it to a folder after creating it.',null,'error');return}
  try{await writeFetch('/api/writing/note',{path:p,body:''});writeNavigate(p)}catch(e){showToast(e.message,null,'error')}
}
async function writeMenu(){
  const d=writingUI.active;if(!d)return;
  const choice=await chooseActionMenu(document.activeElement,[{label:d.source?'live preview':'source mode',value:'source'},{label:'rename…',value:'rename'},{label:'move file to…',value:'move'},{label:'export Markdown',value:'export'},{label:'find in document',value:'find'}]);if(!choice)return;
  if(choice.value==='source'){d.source=!d.source;d.editor.setSource(d.source);writeRemember(d);d.editor.focus();return}
  if(choice.value==='export'){writeExport(d);return}
  if(choice.value==='find'){d.editor.find();return}
  if(d.readOnly){showToast('This file is read only.',null,'error');return}
  let to;
  if(choice.value==='rename'){
    const name=await choosePath({title:'Rename file',placeholder:d.path.replace(/^.*\//,''),items:[],createLabel:'rename to'});if(!name)return;
    if(/[\/\\]/.test(name.value)){showToast('Enter a filename without a folder.',null,'error');return}
    to=d.path.slice(0,d.path.lastIndexOf('/')+1)+name.value;if(!/\.md$/i.test(to))to+='.md';
  }else{
    const data=await writeFetch('/api/writing/files');const folder=await choosePath({title:'Move file to…',placeholder:'type a folder…',items:data.folders.map(p=>({label:p||'/',value:p}))});if(!folder)return;
    to=(folder.value?folder.value+'/':'')+d.path.replace(/^.*\//,'');
  }
  if(!await writeSave(d)||writeDirty(d))return;
  d.moving=true;d.editor.setReadOnly(true);writeClearSaveTimers(d);
  try{
    const result=await writeFetch('/api/writing/move',{path:d.path,to,ifRevision:d.revision});
    const old=d.path;writingUI.documents.delete(old);d.path=result.path;writingUI.documents.set(d.path,d);writeNavigate(d.path);writeLoadComments(d);
    if(result.warning)showToast(result.warning,null,'error');
  }catch(e){showToast(e.message,null,'error')}finally{d.moving=false;d.editor.setReadOnly(d.readOnly);writeQueueSave(d)}
}
function writeExport(d){const url=URL.createObjectURL(new Blob([writeBytes(d)],{type:'text/markdown;charset=utf-8'}));const a=el('a');a.href=url;a.download=d.path.replace(/^.*\//,'');a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function writeSelection(d,range){
  if(d.applying||writingUI.active!==d||writingUI.view.hidden)return;
  writeRemember(d);
  const popup=writingUI.selection;
  if(range.empty||d.readOnly){d.selection=null;popup.hidden=true;return}
  d.selection={from:range.from,to:range.to};
  const rect=d.editor.view.coordsAtPos(range.to);if(!rect){popup.hidden=true;return}
  const canvas=writingUI.canvas.getBoundingClientRect();popup.style.left=Math.max(8,Math.min(rect.left-canvas.left,canvas.width-170))+'px';popup.style.top=Math.max(0,Math.min(rect.bottom-canvas.top+6,canvas.height-44))+'px';popup.hidden=false;
}
async function writeLoadComments(d){
  const path=d.path,request=d.commentRequest=(d.commentRequest||0)+1;
  try{const data=await writeFetch('/api/writing/comments?path='+encodeURIComponent(path));if(path!==d.path||request!==d.commentRequest)return;d.comments=data.document;d.agentAvailable=data.agentAvailable;d.commentError='';writeRenderComments(d);writeMark(d);writePollComments(d)}catch(e){if(path!==d.path||request!==d.commentRequest)return;d.commentError='Could not load comments.';writeRenderComments(d)}
}
function writeLocate(d,anchor){
  const raw=writeBytes(d);let start=-1;
  if(d.revision===anchor.revision&&raw===d.base&&raw.slice(0).length){const bytes=new TextEncoder().encode(raw);if(new TextDecoder().decode(bytes.slice(anchor.start,anchor.end))===anchor.quote)start=new TextDecoder().decode(bytes.slice(0,anchor.start)).length}
  if(start<0){const pattern=anchor.prefix+anchor.quote+anchor.suffix;const pos=raw.indexOf(pattern);if(pos>=0&&raw.indexOf(pattern,pos+1)<0)start=pos+anchor.prefix.length}
  // Nearby edits do not detach an unchanged, uniquely identifiable passage.
  if(start<0&&anchor.quote){const pos=raw.indexOf(anchor.quote);if(pos>=0&&raw.indexOf(anchor.quote,pos+1)<0)start=pos}
  if(start<0)return null;
  return {from:raw.slice(0,start).replace(/\r\n/g,'\n').length,to:raw.slice(0,start+anchor.quote.length).replace(/\r\n/g,'\n').length};
}
function writeMark(d){if(!d.comments)return;d.editor.mark(d.comments.threads.filter(t=>t.state==='open').flatMap(t=>{const range=writeLocate(d,t.anchor);return range?[{...range,id:t.id}]:[]}))}
function writeToggleMargin(){
  const d=writingUI.active;if(!d)return;
  if(window.innerWidth<=1100&&window.mfSheet){writeRenderComments(d);writeMarginSheet();return}
  writingUI.margin.hidden=!writingUI.margin.hidden;if(!writingUI.margin.hidden)writeRenderComments(d);
}
async function writeCompose(mode='comment'){
  const d=writingUI.active;if(!d||!d.selection||d.composing)return;
  // Keep an unfinished conversation attached to its original passage.
  if(d.pendingBody?.trim()){writeShowComposer(d);return}
  const selection={...d.selection},text=d.editor.text();writingUI.selection.hidden=true;d.composing=true;
  try{
    if(writeDirty(d)&&(!await writeSave(d)||writeDirty(d)))return;
    if(d.editor.text()!==text||writingUI.active!==d)return;
    const quote=text.slice(selection.from,selection.to);if(!quote)return;
    const toRaw=text=>d.eol==='\r\n'?text.replace(/\n/g,'\r\n'):text;
    d.pending={revision:d.revision,start:new TextEncoder().encode(toRaw(text.slice(0,selection.from))).length,end:new TextEncoder().encode(toRaw(text.slice(0,selection.to))).length,quote:toRaw(quote)};
    d.pendingMode=mode;d.askError='';
    if(!d.comments)await writeLoadComments(d);
    if(writingUI.active===d)writeShowComposer(d);
  }finally{d.composing=false}
}
function writeShowComposer(d){writeRenderComments(d);writingUI.margin.hidden=false;if(window.innerWidth<=1100&&window.mfSheet)writeMarginSheet();writingUI.margin.querySelector('textarea')?.focus()}
function writeInput(d,key,value,oninput){
  const input=el('textarea','write-question');input.dataset.draft=key;input.rows=2;input.placeholder=key==='new'?'Add a comment or ask…':'Reply…';input.setAttribute('aria-label',key==='new'?'Comment on selected passage':'Reply to comment');input.value=value||'';
  input.oninput=()=>{oninput(input.value);input.style.height='auto';input.style.height=Math.min(input.scrollHeight,220)+'px';const actions=input.nextElementSibling;actions?.querySelectorAll('button').forEach(b=>b.disabled=d.posting||!input.value.trim())};
  input.onkeydown=e=>{if((e.metaKey||e.ctrlKey)&&e.key==='Enter'){e.preventDefault();input.nextElementSibling?.querySelector('button:not(:disabled)')?.click()}};return input;
}
function writeRenderComments(d){
  if(writingUI.active!==d)return;const margin=writingUI.margin,active=document.activeElement;
  const focus=margin.contains(active)&&active.dataset?.draft?{key:active.dataset.draft,start:active.selectionStart,end:active.selectionEnd}:null;
  const scroll=margin.scrollTop;
  margin.replaceChildren();const head=el('div','write-margin-head');
  head.append(el('span','micro-label','COMMENTS'),writeIcon('×','Close comments',()=>{if(margin.closest('.mf-sheet-wrap'))mfSheet.close();else margin.hidden=true}));margin.append(head);
  if(d.commentError){margin.append(el('p','write-comment-note',d.commentError),pillLight('retry',()=>writeLoadComments(d)))}
  if(d.pending){const draft=el('div','write-comment write-composer');
    const quote=el('div','write-draft-head');quote.append(el('blockquote','write-quote',d.pending.quote.trim()),writeIcon('×','Cancel comment',()=>{d.pending=null;d.pendingBody='';d.askError='';writeRenderComments(d)}));draft.append(quote);
    const input=writeInput(d,'new',d.pendingBody,value=>{d.pendingBody=value;d.askError=''});
    const actions=el('div','write-actions');
    const comment=pillLight('comment',()=>writePost(d,{body:input.value,anchor:d.pending}));
    const ask=pillLight('ask',()=>writeAsk(d,{body:input.value,anchor:d.pending}));
    comment.disabled=ask.disabled=d.posting||!input.value.trim();actions.append(comment,ask);draft.append(input,actions);
    if(d.askError){const error=el('p','write-comment-note',d.askError);error.setAttribute('role','status');draft.append(error)}margin.append(draft);
  }
  const threads=d.comments?.threads||[],visible=threads.filter(t=>t.state==='open'||d.showResolved);
  if(!visible.length&&!d.pending)margin.append(el('p','write-comment-note','Select text to comment or ask.'));
  visible.forEach(t=>{
    const card=el('article','write-comment'+(d.expanded===t.id?' expanded':''));
    const top=el('div','write-draft-head'),quote=el('button','write-comment-summary',t.anchor.quote.trim());
    const range=writeLocate(d,t.anchor);
    quote.onclick=()=>{d.expanded=t.id;writeRenderComments(d);if(range){if(writingUI.margin.closest('.mf-sheet-wrap'))mfSheet.close();d.editor.locate(range.from,range.to)}};
    const more=writeIcon('···','Comment actions',async()=>{const choice=await chooseActionMenu(more,[{label:t.state==='open'?'resolve':'reopen',value:t.state==='open'?'resolved':'open'},{label:'dismiss',value:'dismissed'}]);if(choice)writePost(d,{thread:t.id,state:choice.value})});top.append(quote,more);card.append(top);
    if(!range)card.append(el('span','write-comment-note','Passage changed'));
    if(t.state!=='open')card.append(el('span','write-comment-note',t.state));
    const turns=(d.comments?.turns||[]).filter(turn=>turn.thread===t.id);
    const latest=turns.at(-1);
    if(latest?.state==='running'){const progress=el('p','write-comment-note','Thinking…');progress.setAttribute('role','status');card.append(progress)}
    if(latest?.state==='failed'){card.append(el('p','write-comment-note',latest.error||'Could not get an answer.'),pillLight('retry',()=>writeAskTurn(d,t.id,latest.question)))}
    if(d.askRetry?.thread===t.id&&latest?.state!=='running')card.append(pillLight('retry ask',()=>writeAskTurn(d,t.id,d.askRetry.question)));
    t.replies.forEach(reply=>{const row=el('div','write-reply');row.append(el('span','write-comment-meta',(reply.author==='owner'?'you':reply.author)+' · '+fmtWhen(reply.at)),el('div','write-reply-body',reply.body));card.append(row)});
    if(d.expanded===t.id||d.replyDrafts?.[t.id]){
      const input=writeInput(d,t.id,d.replyDrafts?.[t.id],value=>{(d.replyDrafts||={})[t.id]=value});
      const actions=el('div','write-actions');const reply=pillLight('comment',()=>writePost(d,{thread:t.id,body:input.value}));const ask=pillLight('ask',()=>writeAsk(d,{thread:t.id,body:input.value}));reply.disabled=ask.disabled=d.posting||!input.value.trim();actions.append(reply,ask);card.append(input,actions);
    }else card.append(pillLight('reply',()=>{d.expanded=t.id;writeRenderComments(d);margin.querySelectorAll('textarea').forEach(input=>{if(input.dataset.draft===t.id)input.focus()})}));
    margin.append(card);
  });
  if(threads.some(t=>t.state!=='open'))margin.append(pillLight(d.showResolved?'hide resolved':'show resolved',()=>{d.showResolved=!d.showResolved;writeRenderComments(d)}));
  if(focus)margin.querySelectorAll('textarea').forEach(input=>{if(input.dataset.draft===focus.key){input.focus({preventScroll:true});input.setSelectionRange(focus.start,focus.end)}});
  margin.scrollTop=scroll;
}
async function writePost(d,payload){
  if(d.posting||!d.comments||(!payload.state&&!payload.body?.trim()))return;d.posting=true;writeRenderComments(d);
  const path=d.path,pending=d.pending;
  try{
    if(payload.anchor&&d.editor){
      if(!await writeSave(d)||writeDirty(d))return;
      const range=writeLocate(d,payload.anchor);
      if(!range)throw new Error('The selected passage changed. Select it again; your comment is still here.');
      const text=d.editor.text(),start=new TextEncoder().encode(writeBytes(d,text.slice(0,range.from))).length,end=new TextEncoder().encode(writeBytes(d,text.slice(0,range.to))).length;
      payload={...payload,anchor:{...payload.anchor,revision:d.revision,start,end}};
    }
    const doc=await writeFetch('/api/writing/comments',{path,revision:d.comments.revision,id:crypto.randomUUID(),...payload});
    if(d.path!==path)return;
    d.commentRequest=(d.commentRequest||0)+1;d.comments=doc;
    if(!payload.thread){if(d.pending===pending&&d.pendingBody===payload.body){d.pending=null;d.pendingBody=''}d.expanded=null}
    else if(payload.body&&d.replyDrafts?.[payload.thread]===payload.body)delete d.replyDrafts[payload.thread];
    writeMark(d);return doc;
  }catch(e){showToast(e.message,null,'error');if(e.status===409)await writeLoadComments(d)}finally{d.posting=false;writeRenderComments(d);if(d.editor)writeQueueSave(d)}
}
async function writeAsk(d,payload){
  if(d.posting||d.askSubmitting||!payload.body?.trim())return;
  if(!d.agentAvailable){d.askError='Ask is unavailable on this host.';writeRenderComments(d);return}
  if(d.comments?.turns?.some(t=>t.state==='running')){showToast('An answer is already in progress.',null,'info');return}
  d.askSubmitting=true;
  try{
    const doc=await writePost(d,payload);if(!doc)return;
    const thread=payload.thread?doc.threads.find(t=>t.id===payload.thread):doc.threads.at(-1);
    const question=thread?.replies.at(-1);if(!question)return;
    await writeAskTurn(d,thread.id,question.id);
  }finally{d.askSubmitting=false}
}
async function writeAskTurn(d,thread,question){
  // Keep this ID for network retries, including an uncertain POST response.
  const key=thread+':'+question;
  const attempt=(d.askAttempts||={})[key]||=((crypto.randomUUID()));
  try{const doc=await writeFetch('/api/writing/ask',{path:d.path,thread,question,id:attempt});d.commentRequest=(d.commentRequest||0)+1;d.comments=doc;d.expanded=null;d.askError='';d.askRetry=null;writeRenderComments(d);writePollComments(d)}
  catch(e){d.askError=e.message;d.askRetry={thread,question};showToast(e.message,null,'error');await writeLoadComments(d)}
}
function writePollComments(d){
  clearTimeout(d.commentPoll);
  const turns=d.comments?.turns||[];
  for(const turn of turns){if(turn.state==='failed'&&d.askAttempts)delete d.askAttempts[turn.thread+':'+turn.question]}
  if(turns.some(t=>t.state==='running'))d.commentPoll=setTimeout(()=>{if(writingUI.documents.has(d.path))writeLoadComments(d)},1500);
}
window.addEventListener('beforeunload',e=>{if([...writingUI.documents.values()].some(d=>writeDirty(d)||d.pendingBody?.trim()||Object.values(d.replyDrafts||{}).some(v=>v.trim()))){e.preventDefault();e.returnValue=''}});
window.addEventListener('storage',e=>{const d=writingUI.active;if(d&&e.key?.startsWith('manifest.writing.draft.'+writingUI.vaultID+'.'+encodeURIComponent(d.path)+'.')&&e.key!==writeRecoveryKey(d))writeRefresh(d)});
window.addEventListener('blur',()=>{for(const d of writingUI.documents.values())writeFlush(d)});
window.addEventListener('focus',()=>writeRefresh(writingUI.active));
window.addEventListener('online',()=>{for(const d of writingUI.documents.values())writeFlush(d);writeRefresh(writingUI.active)});
document.addEventListener('visibilitychange',()=>{if(document.hidden){for(const d of writingUI.documents.values())writeFlush(d)}else writeRefresh(writingUI.active)});

function writeMarginSheet(){
 const margin=writingUI.margin;if(margin.closest('.mf-sheet-wrap'))return; margin.hidden=false;let release;
 const restore=hidden=>{release?.();release=null;margin.closest('.mf-sheet-wrap')?.removeAttribute('data-writing');writingUI.canvas.parentElement.append(margin);margin.hidden=hidden};
 mfSheet.open(body=>body.append(margin),{key:'writing-comments',onClose:()=>restore(true),reopen:()=>restore(false)});
 const root=margin.closest('.mf-sheet-wrap');root.dataset.writing='true';root.setAttribute('role','dialog');root.setAttribute('aria-modal','true');root.setAttribute('aria-label','Writing comments');
 release=containDialogFocus(root,margin.querySelector('textarea,button'));
}

function writePreferenceKey(d){return 'manifest.writing.view.'+writingUI.vaultID+'.'+encodeURIComponent(d.path)}
function writeRemember(d){
 if(!d.editor)return;const selection=d.editor.selection();
 try{localStorage.setItem(writePreferenceKey(d),JSON.stringify({source:d.source,revision:d.revision,anchor:selection.anchor,head:selection.head,scroll:d.editor.view.scrollDOM.scrollTop,expanded:d.expanded}))}catch(e){}
}

window.matchMedia?.('(max-width:1100px)').addEventListener('change',e=>{if(e.matches&&writingUI.active&&!writingUI.margin.hidden&&window.mfSheet)writeMarginSheet();if(!e.matches&&window.mfSheet?.openKey()==='writing-comments'){mfSheet.close();writingUI.margin.hidden=false}});
