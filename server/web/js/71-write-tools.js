// WRITING — the editor's iA Writer-style tools (owner, 2026-10-06): the view
// menu (iA's "AA"), focus and typewriter, line length and text size, the
// stats bubble, chrome that fades while you type, and [[links]] that create
// the note they point at. Preferences are per device, as iA's are.

const writePrefsKey = 'manifest.writing.prefs';
const writePrefs = Object.assign({focus:'off', lastFocus:'sentence', typewriter:false, measure:72, size:'md', stat:'words', syntax:false, style:false, authors:false, preview:'off', template:'modern', library:true},
  (()=>{try{return JSON.parse(localStorage.getItem(writePrefsKey)||'{}')}catch(e){return {}}})());
function writeSetPref(key, value){
  writePrefs[key]=value;
  try{localStorage.setItem(writePrefsKey,JSON.stringify(writePrefs))}catch(e){}
  writeApplyPrefs();
}
// the whole view follows the preferences; each open editor follows them too
function writeApplyPrefs(){
  const view=writingUI.view;if(!view)return;
  view.style.setProperty('--write-measure',String(writePrefs.measure));
  view.dataset.size=writePrefs.size;view.dataset.focus=writePrefs.focus;
  view.classList.toggle('write-typewriter',!!writePrefs.typewriter);
  for(const d of writingUI.documents.values())writeApplyEditor(d);
  if(typeof writeApplyLibrary==='function')writeApplyLibrary();
  if(typeof writeApplyPreview==='function')writeApplyPreview();
}
function writeApplyEditor(d){
  if(!d?.editor)return;
  d.editor.setFocus(writePrefs.focus,!!writePrefs.typewriter);
  d.editor.showAuthors(!!writePrefs.authors);
  if(typeof writeLayers==='function')writeLayers(d,true);
}

// ---- the view menu ----
const writeFocusLabels={off:'Off',sentence:'Sentence',paragraph:'Paragraph'};
const writeKeys={focus:'⌘D',syntax:'⇧⌘D',style:'⌥⇧⌘D',authors:'⇧⌘A',preview:'⌥⌘P',quick:'⌘O',palette:'⇧⌘P',library:'⌥⌘L'};
function writeShortcut(key){const k=writeKeys[key];if(!k)return '';if(/Mac|iP(hone|ad)/.test(navigator.platform))return k;return [k.includes('⌘')&&'Ctrl',k.includes('⌥')&&'Alt',k.includes('⇧')&&'Shift',k.replace(/[⌘⌥⇧]/g,'')].filter(Boolean).join('+')}
async function writeViewMenu(trigger){
  const p=writePrefs,items=[{header:'Focus'}];
  for(const scope of ['off','sentence','paragraph'])items.push({label:writeFocusLabels[scope],checked:p.focus===scope,run:()=>writeSetFocus(scope),shortcut:scope==='sentence'?writeShortcut('focus'):''});
  items.push({label:'Typewriter scrolling',checked:!!p.typewriter,run:()=>writeSetPref('typewriter',!p.typewriter)});
  items.push({header:'Text'});
  for(const n of [64,72,80])items.push({label:n+' characters a line',checked:p.measure===n,run:()=>writeSetPref('measure',n)});
  for(const [size,label] of [['sm','Small text'],['md','Default text'],['lg','Large text']])items.push({label,checked:p.size===size,run:()=>writeSetPref('size',size)});
  items.push({header:'Editing'});
  items.push({label:'Syntax highlight',checked:!!p.syntax,shortcut:writeShortcut('syntax'),run:()=>writeToggle('syntax')});
  items.push({label:'Style check',checked:!!p.style,shortcut:writeShortcut('style'),run:()=>writeToggle('style')});
  items.push({label:'Show authors',checked:!!p.authors,shortcut:writeShortcut('authors'),run:()=>writeToggle('authors')});
  if(typeof writeTogglePreview==='function'){items.push({header:'Preview'});for(const [mode,label] of [['off','Editor only'],['split','Side by side'],['full','Preview only']])items.push({label,checked:p.preview===mode,shortcut:mode==='split'?writeShortcut('preview'):'',run:()=>writeSetPref('preview',mode)})}
  const choice=await chooseActionMenu(trigger||document.activeElement,items,'View');
  choice?.run();writingUI.active?.editor.focus();
}
function writeSetFocus(scope){writePrefs.lastFocus=scope==='off'?writePrefs.lastFocus:scope;writeSetPref('focus',scope)}
function writeToggleFocus(){writeSetFocus(writePrefs.focus==='off'?writePrefs.lastFocus||'sentence':'off');return true}
function writeToggle(key){writeSetPref(key,!writePrefs[key]);return true}

// ---- keyboard: the editor's own shortcuts (only while it has focus) ----
function writeEditorKeys(d){
  const wrap=(before,after=before)=>view=>{if(view.state.readOnly)return false;const r=view.state.selection.main,text=view.state.sliceDoc(r.from,r.to);
    if(text.startsWith(before)&&text.endsWith(after)&&text.length>=before.length+after.length)view.dispatch({changes:{from:r.from,to:r.to,insert:text.slice(before.length,text.length-after.length)}});
    else view.dispatch({changes:{from:r.from,to:r.to,insert:before+text+after},selection:{anchor:r.from+before.length,head:r.from+before.length+text.length}});return true};
  return [
    // a shifted letter can arrive lowercase (macOS with ⌘, some layouts), and
    // the editor looks the unshifted name up first: read Shift off the event
    {key:'Mod-d',run:()=>writeShiftHeld?writeToggle('syntax'):writeToggleFocus()},
    {key:'Mod-Shift-d',run:()=>writeToggle('syntax')},
    {key:'Mod-Alt-d',run:()=>writeShiftHeld&&writeToggle('style')},{key:'Mod-Alt-Shift-d',run:()=>writeToggle('style')},
    {key:'Mod-a',run:()=>writeShiftHeld&&writeToggle('authors')},{key:'Mod-Shift-a',run:()=>writeToggle('authors')},
    {key:'Mod-b',run:wrap('**')},{key:'Mod-i',run:wrap('*')},{key:'Mod-Shift-u',run:wrap('==')},
    {key:'Mod-k',run:view=>{const r=view.state.selection.main,text=view.state.sliceDoc(r.from,r.to);view.dispatch({changes:{from:r.from,to:r.to,insert:'['+text+']()'},selection:{anchor:r.from+text.length+3}});return true}},
  ];
}
let writeShiftHeld=false;
document.addEventListener('keydown',e=>{writeShiftHeld=e.shiftKey},true);
// while Writing is on screen, its navigation keys work from anywhere in it
document.addEventListener('keydown',e=>{
  if(!writingUI.view||writingUI.view.hidden||e.defaultPrevented||e.isComposing)return;
  const mod=e.metaKey||e.ctrlKey,k=e.key.toLowerCase();
  let run=null;
  if(mod&&!e.shiftKey&&!e.altKey&&k==='o'&&typeof writeQuickSearch==='function')run=()=>writeQuickSearch();
  else if(mod&&e.shiftKey&&!e.altKey&&k==='p'&&typeof writeQuickSearch==='function')run=()=>writeQuickSearch('>');
  else if(mod&&e.altKey&&!e.shiftKey&&(k==='p'||e.code==='KeyP')&&typeof writeTogglePreview==='function')run=()=>writeTogglePreview();
  else if(mod&&e.altKey&&!e.shiftKey&&(k==='l'||e.code==='KeyL')&&typeof writeToggleLibrary==='function')run=()=>writeToggleLibrary();
  if(!run)return;
  e.preventDefault();e.stopPropagation();run();
},true);

// ---- stats: one number at a time; tap for the next ----
const writeStatKinds=['words','characters','sentences','reading'];
function writeStatsOf(text){
  const body=text.replace(/^---\n[\s\S]*?\n(?:---|\.\.\.)(?:\n|$)/,'');
  const words=(body.match(/[\p{L}\p{N}]+(?:['’-][\p{L}\p{N}]+)*/gu)||[]).length;
  return {words,characters:[...body.replace(/\n/g,'')].length,sentences:(body.match(/[^.!?…\n]+(?:[.!?…]+|$)/gm)||[]).filter(s=>/[\p{L}\p{N}]/u.test(s)).length,reading:Math.max(words?1:0,Math.round(words/238))};
}
function writeStatLabel(kind,n){
  const f=n.toLocaleString();
  return kind==='words'?f+' '+(n===1?'word':'words'):kind==='characters'?f+' '+(n===1?'character':'characters'):kind==='sentences'?f+' '+(n===1?'sentence':'sentences'):f+' min read';
}
function writeStats(d){
  const node=writingUI.count;if(!node)return;
  const kind=writeStatKinds.includes(writePrefs.stat)?writePrefs.stat:'words';
  const text=d.editor.text(),all=writeStatsOf(text),sel=d.editor.selection();
  const part=!sel.empty?writeStatsOf(text.slice(sel.from,sel.to)):null;
  node.textContent=part?part[kind].toLocaleString()+' of '+writeStatLabel(kind,all[kind]):writeStatLabel(kind,all[kind]);
  node.title=writeStatKinds.map(k=>writeStatLabel(k,all[k])).join(' · ')+' — tap for the next';
}
function writeNextStat(){const i=writeStatKinds.indexOf(writePrefs.stat);writeSetPref('stat',writeStatKinds[(i+1)%writeStatKinds.length]);if(writingUI.active)writeStats(writingUI.active)}

// ---- chrome fades while you type; any pointer movement brings it back ----
let writeQuietAt={x:0,y:0};
function writeTyping(e){
  if(e.metaKey||e.ctrlKey||e.altKey||e.key.length!==1&&!['Enter','Backspace','Delete','Tab'].includes(e.key))return;
  writingUI.view?.classList.add('write-quiet');
}
document.addEventListener('pointermove',e=>{
  const v=writingUI.view;if(!v?.classList.contains('write-quiet'))return;
  if(e.pointerType==='mouse'&&Math.hypot(e.clientX-writeQuietAt.x,e.clientY-writeQuietAt.y)<6){return}
  writeQuietAt={x:e.clientX,y:e.clientY};if(e.pointerType==='mouse')v.classList.remove('write-quiet');
},{passive:true});
document.addEventListener('pointerdown',e=>{const v=writingUI.view;if(v?.classList.contains('write-quiet')&&!e.target.closest?.('.cm-content'))v.classList.remove('write-quiet')},true);
document.addEventListener('focusin',e=>{const v=writingUI.view;if(v?.classList.contains('write-quiet')&&v.contains(e.target)&&!e.target.closest('.cm-editor'))v.classList.remove('write-quiet')});

// ---- [[links]]: open, a person's page, or create the note ----
async function writeFollowLink(d,target){
  const want=target.split('#')[0].trim();if(!want)return;
  const lower=want.toLowerCase().replace(/\.md$/i,'');
  // iA resolves to the nearest file: same folder, then anywhere by path or name
  const folder=d.path.includes('/')?d.path.slice(0,d.path.lastIndexOf('/')+1):'';
  const byPath=writingUI.files.find(f=>f.path.replace(/\.md$/i,'').toLowerCase()===lower)||writingUI.files.find(f=>f.path.replace(/\.md$/i,'').toLowerCase()===(folder+lower));
  const byName=writingUI.files.filter(f=>f.name.toLowerCase()===lower.replace(/^.*\//,'')).sort((a,b)=>(b.path.startsWith(folder)?1:0)-(a.path.startsWith(folder)?1:0));
  const found=byPath||byName[0];
  if(found){writeNavigate(found.path);return}
  try{const r=await writeFetch('/api/note/resolve?target='+encodeURIComponent(want));
    if(r.kind==='note'&&r.path){writeNavigate(r.path);return}
    if(r.kind==='contact'&&typeof personHref==='function'){location.hash=personHref(r.key);return}
  }catch(e){}
  if(d.readOnly){showToast('Linked note not found',null,'info');return}
  const name=want.replace(/^.*\//,'')+'.md',path=(want.includes('/')?want:folder+want.replace(/^.*\//,''))+'.md';
  const choice=await chooseActionMenu(document.activeElement,[{label:'Create “'+name.replace(/\.md$/,'')+'”',value:'create'},{label:'Cancel',value:null}],'Linked note not found');
  if(choice?.value!=='create')return;
  try{await writeFetch('/api/writing/note',{path,body:''});writingUI.files.push({path,name:name.replace(/\.md$/,''),modified:Date.now()/1000});writeNavigate(path)}
  catch(e){showToast(e.message,null,'error')}
}

// ---- the phone: what the head folds into ··· (docs/ui-conventions.md, phone
// rules 1–2: the head is ‹ Library · name · Aa · ···) ----
function writeMenuExtras(d){
  const out=[];
  if(window.mf?.phone?.()){
    if(typeof writeQuickSearch==='function')out.push({label:'quick search',run:()=>writeQuickSearch()});
    out.push({label:'new file',run:()=>writeCreate()},{label:'comments',run:()=>writeToggleMargin()});
  }
  for(const hook of writeMenuHooks)out.push(...hook(d));
  return out;
}

// ---- the keyboard bar: Markdown keys above a phone's keyboard (iA's) ----
function writeKeyboardBar(){
  if(writingUI.keybar)return writingUI.keybar;
  const bar=el('div','write-keybar');bar.setAttribute('role','toolbar');bar.setAttribute('aria-label','Markdown keys');
  const view=()=>writingUI.active?.editor.view;
  const insert=(text,select)=>{const v=view();if(!v||v.state.readOnly)return;const r=v.state.selection.main,sel=v.state.sliceDoc(r.from,r.to);
    const t=typeof text==='function'?text(sel):text;const at=typeof select==='function'?select(sel):select;v.dispatch({changes:{from:r.from,to:r.to,insert:t},selection:{anchor:r.from+(at??t.length)},userEvent:'input.type'});v.focus()};
  const lineStart=prefix=>()=>{const v=view();if(!v||v.state.readOnly)return;const line=v.state.doc.lineAt(v.state.selection.main.head);v.dispatch({changes:{from:line.from,insert:prefix},userEvent:'input.type'});v.focus()};
  const keys=[['#','Heading',lineStart('# ')],['*','Emphasis',()=>insert(s=>'*'+s+'*',s=>s?undefined:1)],['-','List item',lineStart('- ')],['☐','Task',lineStart('- [ ] ')],['[[','Link to a note',()=>{insert('[[');writingUI.active?.editor.complete()}],['>','Quote',lineStart('> ')],['`','Code',()=>insert(s=>'`'+s+'`',s=>s?undefined:1)],
    ['↶','Undo',()=>{writingUI.active?.editor.undo();view()?.focus()}],['⌄','Hide keyboard',()=>{document.activeElement?.blur()}]];
  for(const [label,name,run] of keys){const b=el('button','write-key',label);b.type='button';b.setAttribute('aria-label',name);b.onpointerdown=e=>e.preventDefault();b.onclick=run;bar.append(b)}
  writingUI.keybar=bar;return bar;
}
document.addEventListener('focusin',e=>{const v=writingUI.view;if(!v||!e.target.closest?.('.write-editor .cm-content'))return;if(!writingUI.keybar)(v.querySelector('.write-main')||v).append(writeKeyboardBar());v.classList.add('write-editing')});
document.addEventListener('focusout',e=>{const v=writingUI.view;if(v&&e.target.closest?.('.write-editor .cm-content'))setTimeout(()=>{if(!document.activeElement?.closest?.('.write-editor .cm-content'))v.classList.remove('write-editing')},0)});
