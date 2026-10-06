// WRITING — the library (owner, 2026-10-06, after iA Writer): one pane, the
// same on a desk and a phone. Its organizer lists Recents, All files,
// Favorites, folders, smart folders (saved searches) and #hashtags; a section
// opens its file list (name, excerpt, when); search takes iA's syntax and runs
// on the server (/api/writing/search). On a phone the pane is its own screen:
// library → list → document, each a route, so Back walks them in order.
// Favorites and smart folders live in the server's library document, so a
// phone and a desk see the same ones.

const writeLib = {section:'recent', query:'', results:null, searching:0, tags:null, doc:{}, rev:'', loaded:false, route:'#/write', sort:'modified'};

function writeLibSectionLabel(key){
  if(key==='recent')return 'Recents';if(key==='all')return 'All files';if(key==='fav')return 'Favorites';
  if(key.startsWith('folder:'))return key.slice(7).replace(/^.*\//,'')||'Vault';
  if(key.startsWith('tag:'))return '#'+key.slice(4);
  if(key.startsWith('smart:'))return (writeLib.doc.smart||[]).find(s=>s.id===key.slice(6))?.name||'Smart folder';
  return 'Library';
}
// ---- the library document (favorites, smart folders, style rules) ----
async function writeLibLoad(){
  if(writeLib.loaded)return;
  try{const r=await writeFetch('/api/writing/library');writeLib.doc=r.library||{};writeLib.rev=r.revision||'';writeLib.loaded=true}catch(e){writeLib.doc={}}
}
// apply a change to the server's copy; a conflict re-applies it to theirs
async function writeLibChange(change){
  await writeLibLoad();
  for(let attempt=0;attempt<2;attempt++){
    const next=structuredClone(writeLib.doc||{});change(next);
    try{const r=await writeFetch('/api/writing/library',{library:next,ifRevision:writeLib.rev},'PUT');writeLib.doc=next;writeLib.rev=r.revision;writeLibRender();return true}
    catch(e){if(e.status===409&&e.detail){writeLib.doc=e.detail.library||{};writeLib.rev=e.detail.revision||'';continue}showToast(e.message,null,'error');return false}
  }
  return false;
}
function writeIsFavorite(path){return (writeLib.doc.favorites||[]).includes(path)}
function writeToggleFavorite(path){const on=writeIsFavorite(path);return writeLibChange(doc=>{doc.favorites=(doc.favorites||[]).filter(p=>p!==path);if(!on)doc.favorites.push(path)})}
async function writeLoadTags(){try{const r=await writeFetch('/api/writing/tags');writeLib.tags=r.tags||[];writingUI.tags=writeLib.tags}catch(e){writeLib.tags=writeLib.tags||[]}}

// ---- building the pane ----
function writeLibBuild(){
  if(writingUI.library)return writingUI.library;
  const pane=el('aside','write-library');pane.setAttribute('aria-label','Library');
  const head=el('div','write-lib-head');
  const back=el('button','write-lib-back','‹ Library');back.type='button';back.onclick=()=>writeLibGo('#/write');
  const title=el('h2','write-lib-title','Library');
  const create=el('button','write-icon write-lib-new','＋');create.type='button';create.setAttribute('aria-label','New file here');create.title='New file here';create.onclick=()=>writeCreate(writeLibFolder());
  const more=el('button','write-icon write-lib-more','···');more.type='button';more.setAttribute('aria-label','Library actions');more.onclick=()=>writeLibMenu(more);
  head.append(back,title,create,more);
  const search=el('input','write-lib-search');search.type='search';search.placeholder='Search';search.setAttribute('aria-label','Search the vault: words, "phrases", -exclude, #tag, [ ] open tasks, name:');
  search.oninput=debounce(()=>writeLibSearch(search.value),180);
  search.onkeydown=e=>{if(e.key==='Escape'&&search.value){e.preventDefault();e.stopPropagation();search.value='';writeLibSearch('')}else if(e.key==='ArrowDown'){e.preventDefault();pane.querySelector('.write-lib-row')?.focus()}};
  const body=el('div','write-lib-body');
  body.onkeydown=e=>{if(!['ArrowDown','ArrowUp'].includes(e.key))return;const rows=[...body.querySelectorAll('.write-lib-row')],i=rows.indexOf(document.activeElement);if(i<0)return;e.preventDefault();(rows[i+(e.key==='ArrowDown'?1:-1)]||(e.key==='ArrowUp'?search:null))?.focus()};
  pane.append(head,search,body);
  Object.assign(writingUI,{library:pane,libTitle:title,libBack:back,libSearch:search,libBody:body,libNew:create});
  return pane;
}
function writeLibFolder(){return writeLib.section.startsWith('folder:')?writeLib.section.slice(7):(writingUI.active?.path.includes('/')?'':'')}
function writeLibGo(route){if(location.hash===route)writeLibOpen(route.startsWith('#/write/~list/')?decodeURIComponent(route.slice(14)):'');else location.hash=route}
// route entry: '' is the organizer, a key opens that section's list
function writeLibOpen(key){
  writeLibBuild();writeLibLoad().then(()=>writeLibRender());if(!writeLib.tags)writeLoadTags().then(()=>writeLibRender());
  writeLib.view=key?'list':'organizer';if(key)writeLib.section=key;
  writeLib.route=key?'#/write/~list/'+encodeURIComponent(key):'#/write';
  writeLibRender();
}
function writeApplyLibrary(){
  const v=writingUI.view;if(!v)return;
  if(!writingUI.library){v.prepend(writeLibBuild());writeLibOpen(writeLib.section&&writeLib.view==='list'?writeLib.section:'')}
  v.classList.toggle('has-library',!!writePrefs.library);
  writingUI.head?.querySelector('.write-library-toggle')?.setAttribute('aria-expanded',String(!!writePrefs.library));
}
function writeToggleLibrary(){
  if(window.mf?.phone?.()){location.hash=writeLib.route||'#/write';return true}
  writeSetPref('library',!writePrefs.library);if(writePrefs.library)writingUI.libSearch?.focus();return true;
}

// ---- rendering ----
function writeLibRender(){
  const body=writingUI.libBody;if(!body)return;
  const searching=!!writeLib.query.trim();
  const list=searching||writeLib.view==='list';
  writingUI.library.dataset.view=list?'list':'organizer';
  writingUI.libBack.hidden=!list;
  writingUI.libTitle.textContent=searching?'Search':list?writeLibSectionLabel(writeLib.section):'Library';
  writingUI.libNew.hidden=searching;
  const scroll=body.scrollTop,focusKey=document.activeElement?.dataset?.key;
  body.replaceChildren(searching?writeLibResults():list?writeLibList(writeLib.section):writeLibOrganizer());
  body.scrollTop=scroll;if(focusKey)body.querySelector('[data-key="'+CSS.escape(focusKey)+'"]')?.focus({preventScroll:true});
}
function writeLibEntry(label,key,count,glyph){
  const a=el('a','write-lib-row write-lib-section');a.href='#/write/~list/'+encodeURIComponent(key);a.dataset.key=key;
  if(writeLib.section===key&&writeLib.view==='list')a.setAttribute('aria-current','page');
  a.append(el('span','write-lib-glyph',glyph||''),el('span','write-lib-name',label));if(count!==undefined)a.append(el('span','write-lib-count',String(count)));
  return a;
}
function writeLibOrganizer(){
  const out=document.createDocumentFragment(),files=writingUI.files;
  out.append(writeLibEntry('Recents','recent',Math.min(25,files.length),'◷'),writeLibEntry('All files','all',files.length,'▤'),writeLibEntry('Favorites','fav',(writeLib.doc.favorites||[]).filter(p=>files.some(f=>f.path===p)).length,'☆'));
  const top=(writingUI.folders||[]).filter(f=>f&&!f.includes('/'));
  if(top.length){out.append(el('div','write-lib-group micro-label','Folders'));for(const f of top)out.append(writeLibEntry(f,'folder:'+f,files.filter(x=>x.path.startsWith(f+'/')).length,'▸'))}
  const smart=writeLib.doc.smart||[];
  if(smart.length){out.append(el('div','write-lib-group micro-label','Smart folders'));for(const s of smart)out.append(writeLibEntry(s.name,'smart:'+s.id,undefined,'⌕'))}
  const tags=writeLib.tags||[];
  if(tags.length){out.append(el('div','write-lib-group micro-label','Hashtags'));for(const t of tags.slice(0,60))out.append(writeLibEntry('#'+t.tag,'tag:'+t.tag,t.count,''))}
  return out;
}
function writeLibFilesFor(key){
  const files=writingUI.files;
  const byTime=list=>[...list].sort((a,b)=>b.modified-a.modified),byName=list=>[...list].sort((a,b)=>a.name.localeCompare(b.name,undefined,{numeric:true,sensitivity:'base'}));
  const sorted=list=>writeLib.sort==='name'?byName(list):byTime(list);
  if(key==='recent')return {files:byTime(files).slice(0,25)};
  if(key==='all')return {files:sorted(files)};
  if(key==='fav')return {files:(writeLib.doc.favorites||[]).map(p=>files.find(f=>f.path===p)).filter(Boolean)};
  if(key.startsWith('folder:')){const dir=key.slice(7),prefix=dir?dir+'/':'';
    const sub=(writingUI.folders||[]).filter(f=>f.startsWith(prefix)&&f!==dir&&!f.slice(prefix.length).includes('/'));
    return {folders:sub,files:sorted(files.filter(f=>f.path.startsWith(prefix)&&!f.path.slice(prefix.length).includes('/')))}}
  if(key.startsWith('tag:')){const tag=key.slice(4).toLowerCase();return {files:sorted(files.filter(f=>(f.tags||[]).some(t=>t.toLowerCase()===tag)))}}
  return {files:[]};
}
function writeLibList(key){
  const out=document.createDocumentFragment();
  if(key.startsWith('smart:')){const s=(writeLib.doc.smart||[]).find(x=>x.id===key.slice(6));
    if(!s){out.append(el('p','write-lib-empty','This smart folder was removed.'));return out}
    out.append(el('p','write-lib-query',s.query));writeLibRunSmart(s);
    const r=writeLib.smartResults?.[s.id];if(!r){out.append(el('p','write-lib-empty','Searching…'));return out}
    r.forEach(f=>out.append(writeLibFileRow(f)));if(!r.length)out.append(el('p','write-lib-empty','Nothing matches yet.'));return out}
  const {folders=[],files}=writeLibFilesFor(key);
  for(const f of folders)out.append(writeLibEntry(f.replace(/^.*\//,''),'folder:'+f,writingUI.files.filter(x=>x.path.startsWith(f+'/')).length,'▸'));
  files.forEach(f=>out.append(writeLibFileRow(f)));
  if(!folders.length&&!files.length)out.append(el('p','write-lib-empty',key==='fav'?'Star a file from its ··· to keep it here.':'No files here yet.'));
  return out;
}
function writeLibFileRow(f,snippet){
  const a=el('a','write-lib-row write-lib-file');a.href='#/write/'+encodeURIComponent(f.path);a.dataset.key='file:'+f.path;
  if(writingUI.active?.path===f.path)a.setAttribute('aria-current','page');
  const top=el('div','write-lib-file-top');top.append(el('span','write-lib-name',(writeIsFavorite(f.path)?'★ ':'')+f.name));
  if(f.modified)top.append(el('span','write-lib-when',fmtWhen(new Date(f.modified*1000).toISOString())));
  a.append(top);const text=snippet||f.snippet||f.excerpt;if(text)a.append(el('span','write-lib-excerpt',text));
  if(f.path.includes('/'))a.title=f.path;
  a.oncontextmenu=e=>{e.preventDefault();writeFileMenu(a,f)};
  let press;a.addEventListener('touchstart',()=>{press=setTimeout(()=>{press=null;writeFileMenu(a,f)},550)},{passive:true});
  a.addEventListener('touchend',e=>{if(press)clearTimeout(press);else e.preventDefault()});a.addEventListener('touchmove',()=>clearTimeout(press),{passive:true});
  return a;
}
async function writeFileMenu(anchor,f){
  const choice=await chooseActionMenu(anchor,[{label:writeIsFavorite(f.path)?'Remove from favorites':'Add to favorites',value:'fav'},{label:'Open',value:'open'}],f.name);
  if(choice?.value==='fav')writeToggleFavorite(f.path);
  if(choice?.value==='open')writeNavigate(f.path);
}
// ---- search ----
async function writeLibSearch(q){
  writeLib.query=q;const request=++writeLib.searching;
  if(!q.trim()){writeLib.results=null;writeLibRender();return}
  try{const r=await writeFetch('/api/writing/search?q='+encodeURIComponent(q)+'&limit=50');if(request!==writeLib.searching)return;writeLib.results=r}
  catch(e){if(request!==writeLib.searching)return;writeLib.results={error:e.status===400?'Check the search: '+(e.message||'').trim():'Search is unavailable.'}}
  writeLibRender();
}
function writeLibResults(){
  const out=document.createDocumentFragment(),r=writeLib.results;
  if(!r){out.append(el('p','write-lib-empty','Searching…'));return out}
  if(r.error){out.append(el('p','write-lib-empty',r.error));return out}
  out.append(el('p','write-lib-query',r.total===1?'1 file':(r.total||0).toLocaleString()+' files'));
  r.results.forEach(f=>out.append(writeLibFileRow(f,f.snippet)));
  if(!r.results.length)out.append(el('p','write-lib-empty','No files match.'));
  return out;
}
async function writeLibRunSmart(s){
  writeLib.smartResults=writeLib.smartResults||{};const key=s.id+'\n'+s.query;
  if(writeLib.smartAsked===key)return;writeLib.smartAsked=key;
  try{const r=await writeFetch('/api/writing/search?q='+encodeURIComponent(s.query)+'&limit=50');writeLib.smartResults[s.id]=r.results||[]}catch(e){writeLib.smartResults[s.id]=[]}
  writeLibRender();
}
// ---- the pane's ··· ----
async function writeLibMenu(anchor){
  const items=[],key=writeLib.section,list=writeLib.view==='list'||writeLib.query;
  if(writeLib.query.trim())items.push({label:'Save search as smart folder…',value:'smart'});
  if(list&&!writeLib.query&&(key==='all'||key.startsWith('folder:')||key.startsWith('tag:'))){items.push({header:'Sort'},{label:'Newest first',checked:writeLib.sort==='modified',value:'sort:modified'},{label:'By name',checked:writeLib.sort==='name',value:'sort:name'})}
  items.push({header:'Create'},{label:'New file'+(key.startsWith('folder:')?' in '+writeLibSectionLabel(key):''),value:'file'},{label:'New folder'+(key.startsWith('folder:')?' in '+writeLibSectionLabel(key):''),value:'folder'});
  if(key.startsWith('smart:')&&writeLib.view==='list')items.push({label:'Remove this smart folder',value:'unsmart',danger:true});
  const choice=await chooseActionMenu(anchor,items,'Library');if(!choice)return;
  const v=choice.value;
  if(v.startsWith('sort:')){writeLib.sort=v.slice(5);writeLibRender();return}
  if(v==='file'){writeCreate(writeLibFolder());return}
  if(v==='folder'){writeCreateFolder(writeLibFolder());return}
  if(v==='smart'){const name=await choosePath({title:'Name this smart folder',placeholder:writeLib.query,items:[],createLabel:'save as'});if(!name)return;const id=crypto.randomUUID().slice(0,8);
    if(await writeLibChange(doc=>{doc.smart=[...(doc.smart||[]),{id,name:name.value,query:writeLib.query.trim()}]})){writingUI.libSearch.value='';writeLib.query='';writeLibGo('#/write/~list/'+encodeURIComponent('smart:'+id))}return}
  if(v==='unsmart'){const id=key.slice(6);if(await writeLibChange(doc=>{doc.smart=(doc.smart||[]).filter(s=>s.id!==id)}))writeLibGo('#/write')}
}
async function writeCreateFolder(parent){
  const choice=await choosePath({title:'New folder'+(parent?' in '+parent:''),placeholder:'name the folder…',items:[],createLabel:'create'});if(!choice)return;
  const name=choice.value.trim().replace(/^\/+|\/+$/g,'');if(!name||/[\\]|(^|\/)\.\.?(\/|$)/.test(name)){showToast('Use a plain folder name.',null,'error');return}
  const path=(parent?parent+'/':'')+name;
  try{await writeFetch('/api/writing/folder',{path});writingUI.folders=[...new Set([...(writingUI.folders||[]),path])].sort();writeLibGo('#/write/~list/'+encodeURIComponent('folder:'+path))}catch(e){showToast(e.message,null,'error')}
}
// the list follows the document in view
writeOnMount.push(()=>{if(writingUI.library)writeLibRender()});
