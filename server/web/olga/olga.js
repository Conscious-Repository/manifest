// Reauthenticate in place: a rejected API request has not mutated anything,
// so it can resume once after sign-in while the original form stays mounted.
const olgaNativeFetch=window.fetch.bind(window);
let olgaSessionPrompt=null;
function olgaSignInAgain(){
 if(olgaSessionPrompt)return olgaSessionPrompt;
 olgaSessionPrompt=new Promise((resolve,reject)=>{
  const dialog=el('dialog','olga-session-dialog');
  const form=el('form','olga-task-details olga-task-add');
  form.append(el('h2','','Sign in to continue'),el('p','olga-detail-hint','Your draft will stay here.'));
  const label=el('label','olga-detail-field');const password=el('input');password.type='password';password.autocomplete='current-password';password.required=true;label.append(el('span','','Password'),password);
  const error=el('p','olga-add-error');error.setAttribute('role','alert');
  const actions=el('div','olga-add-actions');const cancel=el('button','pill light','Cancel');cancel.type='button';const submit=el('button','pill olga-primary','Sign in');submit.type='submit';actions.append(cancel,submit);form.append(label,error,actions);dialog.append(form);document.body.append(dialog);
  const finish=success=>{dialog.close();dialog.remove();if(success)resolve();else reject(new Error('Sign in to save. Your draft is still here.'));};
  cancel.onclick=()=>finish(false);dialog.addEventListener('cancel',event=>{event.preventDefault();finish(false);});
  form.onsubmit=async event=>{event.preventDefault();submit.disabled=true;cancel.disabled=true;error.textContent='';
   try{const response=await olgaNativeFetch('/api/session',{method:'POST',body:new URLSearchParams({password:password.value})});if(!response.ok)throw new Error(await response.text());password.value='';finish(true);}
   catch(e){error.textContent=e.message;password.focus();}finally{submit.disabled=false;cancel.disabled=false;}
  };
  dialog.showModal();password.focus();
 }).finally(()=>{olgaSessionPrompt=null;});
 return olgaSessionPrompt;
}
window.fetch=async function(input,init){
 const request=new Request(input,init),url=new URL(request.url);
 if(url.origin!==location.origin||!url.pathname.startsWith('/api/')||url.pathname==='/api/session')return olgaNativeFetch(request);
 const response=await olgaNativeFetch(request.clone());
 if(response.status!==401)return response;
 await olgaSignInAgain();
 return olgaNativeFetch(request);
};
function attachWikilinkAutocomplete(){}
function attachInlineLinks(){}
// A small planner shell over the very same DAY, GOALS and TASKS components.
// No main-app boot, chat module, agent panel, calendar connection or polling.
function showToast(message) {
 const n=el('div','toast',message);n.setAttribute('role','status');document.getElementById('toastHost').append(n);setTimeout(()=>n.remove(),5000);
}
async function postJSONOk(path,body){
 const r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
 if(!r.ok)throw new Error(await r.text());return r.json();
}
function ensureTodoPanelPoll(){}
function setCrumbMeta(text){els.crumbMeta.textContent=text;}
function resolveWikilink(text){showToast(text);}
// Shared goal/day controls may offer a task inspector: Olga edits only the task.
// The task panel, simplified for a phone (2026-10-07 audit): Mark done at the
// top, plain sections, one Save that appears only when something changed, and
// Liber for questions. Planning fields stay one fold away for Home tasks.
let olgaPanelDirty=null;
function olgaWhen(taskId){
 if(typeof hp==='undefined'||!hp.data||!hp.data.plan||!hp.data.plan.tasks[taskId])return null;
 const v=hp.data,d=v.derived,p=v.plan,t=p.tasks[taskId];
 const items=(d.items||[]).filter(x=>x.task===taskId&&!x.done);
 const weekends=new Set();
 items.forEach(it=>{const item=hpItemOf(p,it)||{};Object.keys(item.allocations||{}).forEach(w=>weekends.add(w));});
 (d.sequence?.placements||[]).forEach(pl=>{if(pl.ref.split('#')[0]===taskId)weekends.add(pl.weekend);});
 const ws=[...weekends].sort();
 const box=el('div','olga-when');
 const when=ws.length?(ws.length===1?'Planned for the weekend of '+hpWeekend(ws[0]):'Planned over '+ws.length+' weekends, starting '+hpWeekend(ws[0])):'Not on a weekend yet';
 const hours=items.filter(it=>it.hours!=null).reduce((n,it)=>n+it.hours,0),unknown=items.filter(it=>it.hours==null).length;
 const line1=el('p');line1.append(el('strong','',when));box.append(line1);
 const bits=[];if(hours)bits.push('about '+Math.round(hours)+' hours of work');if(unknown)bits.push(unknown+' part'+(unknown===1?'':'s')+' without an hours guess yet');
 if(bits.length)box.append(el('p','',hpCap(bits.join(' · '))));
 if(t.nextAction)box.append(el('p','','Next: '+t.nextAction));
 const subs=Object.values(t.subtasks||{});
 if(subs.length){const ul=el('ul','olga-steps');subs.forEach(st=>ul.append(el('li',st.done?'is-done':'',(st.done?'✓ ':'')+st.title)));box.append(el('p','','Steps:'),ul);}
 return box;
}
async function openTodoPanel(row){
 const r=typeof row==='string'?(todosCache?.rows||[]).find(r=>r.id===row)||todosCompletedRow(row):row;
 if(!r)return;
 els.pickerTitle.textContent=r.text||'Task';els.pickerBody.replaceChildren(el('p','','Loading…'));els.pickerModal.hidden=false;
 let liberSec=null;
 try{
  const response=await fetch('/api/tasks/notes?id='+encodeURIComponent(r.id));if(!response.ok)throw new Error(await response.text());let notes=await response.json();
  if(els.pickerModal.hidden)return;
  const isHome=(r.container?.name||'')==='Home';
  if(isHome&&typeof homePlanLoad==='function'&&(!hp.data||Date.now()-(hp.loadedAt||0)>30000))await homePlanLoad().catch(()=>{});
  const parts=[];
  // 1. done
  const top=el('div','olga-panel-top');
  const done=el('button','olga-done-btn'+(r.done?' is-done':''),r.done?'Not done yet':'✓ Mark done');done.type='button';
  done.onclick=async()=>{done.disabled=true;try{await todosApi('/api/tasks/check',{id:r.id,checked:!r.done});olgaPanelDirty=null;closePicker();
   if(!r.done)olgaToast('Done — “'+r.text+'”',async()=>{await todosApi('/api/tasks/check',{id:r.id,checked:false});});else olgaToast('Back on your list');}
   catch(e){done.disabled=false;showToast(e.message);}};
  top.append(done);parts.push(top);
  // 2. the basics
  const form=el('form','olga-task-details');
  const field=(label,node)=>{const wrap=el('label','olga-detail-field');wrap.append(el('span','',label),node);form.append(wrap);return node;};
  const title=field('Task',inputEl('Task'));title.value=r.text;title.required=true;
  const area=field('Area',selectEl([...new Set([r.container?.name||'Inbox','Inbox',...(todosCache?.areas||[])])]));area.value=r.container?.name||'Inbox';
  area.disabled=area.value==='Home';if(!area.disabled)for(const option of [...area.options])if(option.value==='Home')option.remove();
  if(isHome)form.append(el('p','olga-detail-hint','Home is shared with Benjamin.'));
  const priority=field('Importance',selectEl(['Normal','Low','Medium','High']));priority.value=({low:'Low',med:'Medium',high:'High'})[r.priority]||'Normal';
  parts.push(form);
  // 3. the house plan, in plain words
  if(isHome){
   const when=olgaWhen(r.id);if(when)parts.push(when);
   const qs=typeof olgaQuestions==='function'&&hp.data?olgaQuestions(hp.data).open.filter(x=>x.task===r.id):[];
   if(qs.length){const sec=el('div','olga-panel-qs');sec.append(el('h3','olga-panel-h',qs.length===1?'One question to answer':qs.length+' questions to answer'));
    qs.forEach(x=>{const item=el('div','olga-q');const head=el('button','olga-q-head');head.type='button';head.append(el('span','olga-q-title',olgaQTitle(x)));item.append(head);head.onclick=()=>{if(item.querySelector('.olga-q-card'))return;item.classList.add('is-open');item.append(olgaQuestionCard(hp.data,x));};sec.append(item);});
    parts.push(sec);}
  }
  // 4. Liber
  if(typeof liberTaskSection==='function'){liberSec=liberTaskSection(r.id,isHome);parts.push(liberSec);}
  // 5. notes (folded), comments (folded)
  const hasNotes=!!(notes.description||'').trim();
  const notesFold=el('details','olga-fold');const ns=el('summary');ns.append(el('span','',hasNotes?'Notes':'Add notes'));notesFold.append(ns);
  const notesView=homePlanNotesView(notes.description||'');notesView.hidden=!hasNotes;
  const description=el('textarea');description.rows=hasNotes?12:5;description.value=notes.description||'';description.placeholder='Details, links, or a checklist…';description.setAttribute('aria-label','Notes');description.hidden=hasNotes;description.style.fontSize='16px';
  const editNotes=pillLight('Edit notes',()=>{description.hidden=false;notesView.hidden=true;editNotes.hidden=true;description.focus();});editNotes.type='button';editNotes.hidden=!hasNotes;
  notesFold.append(notesView,editNotes,description);parts.push(notesFold);
  const comFold=el('details','olga-fold');const cs=el('summary');cs.append(el('span','','Comments'+((notes.comments||[]).length?' · '+notes.comments.length:'')));comFold.append(cs);
  const entries=el('div');const renderComments=()=>{entries.replaceChildren();for(const c of notes.comments||[]){const item=el('article','olga-comment');item.append(el('div','olga-detail-hint',(c.author_name||c.author)+' · '+new Date(c.at).toLocaleString()),el('p','',c.text));entries.append(item);}};renderComments();
  const composer=el('form');const comment=el('textarea');comment.rows=2;comment.placeholder='Write a comment for Benjamin…';comment.setAttribute('aria-label','Comment');comment.required=true;comment.style.fontSize='16px';
  const send=el('button','pill','Add comment');send.type='submit';const cerr=el('p','olga-detail-hint');cerr.setAttribute('role','status');composer.append(comment,send,cerr);
  composer.onsubmit=async e=>{e.preventDefault();if(!comment.value.trim())return;send.disabled=true;try{const updated=await postJSONOk('/api/tasks/notes',{id:r.id,kind:'comment',comment:comment.value});notes.comments=updated.comments;comment.value='';renderComments();cs.firstChild.textContent='Comments · '+notes.comments.length;}catch(err){cerr.textContent=err.message;}finally{send.disabled=false;}};
  comFold.append(entries,composer);parts.push(comFold);
  // 6. planning details (Home): the full editor, one fold away
  if(isHome&&typeof homePlanEditorInto==='function'){const det=el('details','olga-fold');const ds=el('summary');ds.append(el('span','','Planning details'));det.append(ds);const box=el('div','olga-schedule');det.append(box);det.ontoggle=()=>{if(det.open&&!box.childElementCount)homePlanEditorInto(box,r.id);};parts.push(det);}
  // 7. remove
  const remove=el('button','olga-remove','Remove this task');remove.type='button';
  remove.onclick=()=>{const ask=el('div','olga-leave');ask.append(el('span','','Remove “'+r.text+'”? It’s archived, not deleted.'));const yes=el('button','pill olga-primary','Remove');yes.type='button';const no=el('button','pill light','Keep it');no.type='button';
   yes.onclick=async()=>{yes.disabled=true;try{await todosApi('/api/tasks/drop',{id:r.id});olgaPanelDirty=null;closePicker();olgaToast('Removed');}catch(e){showToast(e.message);yes.disabled=false;}};no.onclick=()=>ask.remove();ask.append(yes,no);remove.after(ask);};
  parts.push(remove);
  // one Save bar, shown only when something changed
  const footer=el('div','olga-panel-footer');footer.hidden=true;
  const cancel=el('button','pill light','Cancel');cancel.type='button';const save=el('button','pill olga-primary','Save changes');save.type='button';footer.append(cancel,save);
  const original={title:title.value,area:area.value,priority:priority.value,notes:description.value};
  const dirty=()=>title.value.trim()!==original.title||area.value!==original.area||priority.value!==original.priority||description.value!==original.notes;
  const check=()=>{footer.hidden=!dirty();};
  [title,area,priority,description].forEach(n=>{n.addEventListener('input',check);n.addEventListener('change',check);});
  cancel.onclick=()=>{title.value=original.title;area.value=original.area;priority.value=original.priority;description.value=original.notes;check();};
  const doSave=async()=>{if(!title.value.trim()){title.focus();return false;}save.disabled=true;save.textContent='Saving…';try{
   if(description.value!==original.notes)notes=await postJSONOk('/api/tasks/notes',{id:r.id,kind:'description',description:description.value,revision:notes.revision});
   if(title.value.trim()!==original.title||area.value!==original.area)await postJSONOk('/api/tasks/update',{id:r.id,text:title.value.trim(),domain:area.value});
   if(priority.value!==original.priority)await postJSONOk('/api/tasks/priority',{id:r.id,priority:({Low:'low',Medium:'med',High:'high'})[priority.value]||''});
   r.text=title.value.trim();r.container={name:area.value};Object.assign(original,{title:r.text,area:area.value,priority:priority.value,notes:description.value});els.pickerTitle.textContent=r.text;check();olgaToast('Saved');await loadTodos();return true;
  }catch(e){showToast(e.message);return false;}finally{save.disabled=false;save.textContent='Save changes';}};
  save.onclick=doSave;form.onsubmit=e=>{e.preventDefault();doSave();};
  olgaPanelDirty=()=>dirty()?{save:doSave}:null;
  const scroller=el('div','olga-panel-body');scroller.append(...parts);
  els.pickerBody.replaceChildren(scroller,footer);
 }catch(e){els.pickerBody.replaceChildren(el('p','',e.message));}
}
// Leaving a panel with unsaved changes asks first, inside the panel.
function olgaGuardClose(event){
 const pending=olgaPanelDirty&&olgaPanelDirty();
 if(!pending)return;
 event.stopImmediatePropagation();event.preventDefault();
 if(els.pickerBody.querySelector('.olga-leave-unsaved'))return;
 const ask=el('div','olga-leave olga-leave-unsaved');ask.append(el('span','','Save your changes?'));
 const yes=el('button','pill olga-primary','Save');yes.type='button';const no=el('button','pill light','Don’t save');no.type='button';
 yes.onclick=async()=>{if(await pending.save()){olgaPanelDirty=null;closePicker();}};no.onclick=()=>{olgaPanelDirty=null;closePicker();};
 ask.append(yes,no);els.pickerBody.prepend(ask);ask.scrollIntoView({block:'nearest'});
}
els.pickerClose.addEventListener('click',olgaGuardClose,true);
els.pickerBackdrop.addEventListener('click',olgaGuardClose,true);
new MutationObserver(()=>{if(els.pickerModal.hidden){olgaPanelDirty=null;els.pickerBody.querySelectorAll('.liber-task').forEach(n=>n.liberStop&&n.liberStop());}}).observe(els.pickerModal,{attributes:true,attributeFilter:['hidden']});
async function openTodoQuickAdd(prefill='',options={}){
 if(!todosCache){try{const response=await fetch('/api/tasks');if(!response.ok)throw new Error('Could not load task areas');todosCache=await response.json();}catch(e){showToast(e.message);return;}}
 els.pickerTitle.textContent='Add task';
 const form=el('form','olga-task-details olga-task-add');
 const field=(label,node)=>{const wrap=el('label','olga-detail-field');wrap.append(el('span','',label),node);form.append(wrap);return node;};
 const input=field('Task',inputEl('What needs to be done?'));input.value=prefill;input.required=true;input.autocomplete='off';
 const domain=field('Area',selectEl([...new Set(['Inbox',...olgaTaskAreas(todosCache),...(options.domain?[options.domain]:[])])]));domain.value=options.domain||olgaTaskArea||'Inbox';
 const hint=el('p','olga-detail-hint');const updateHint=()=>{hint.textContent=domain.value==='Home'?'Shared with Benjamin.':domain.value==='Inbox'?'Keep it in Inbox until you choose an area.':'';hint.hidden=!hint.textContent;};domain.onchange=updateHint;updateHint();
 const actions=el('div','olga-add-actions');const cancel=el('button','pill light','Cancel');cancel.type='button';cancel.onclick=closePicker;
 const save=el('button','pill olga-primary','Add task');save.type='submit';actions.append(cancel,save);
 const error=el('p','olga-add-error');error.setAttribute('role','alert');error.hidden=true;
 form.append(hint,error,actions);
 form.onsubmit=async event=>{event.preventDefault();if(!input.value.trim()||save.disabled)return;save.disabled=true;cancel.disabled=true;error.hidden=true;
  try{const created=await postJSONOk('/api/tasks/item',{text:input.value.trim(),domain:domain.value==='Inbox'?'':domain.value,...(options.rock?{rock:options.rock}:{})});closePicker();if(typeof olgaSimple!=='undefined'){olgaSimple.justAdded=created.created||'';const g=domain.value||'Inbox';olgaSimple.open[g]=true;olgaToast('Added to '+(g==='Home'?'Home (shared)':g));}await loadTodos();if(!els.goalsView.hidden)await loadGoals();}
  catch(e){error.textContent=e.message;error.hidden=false;}finally{save.disabled=false;cancel.disabled=false;}
 };
 els.pickerBody.replaceChildren(form);els.pickerModal.hidden=false;input.focus();
}
// Keep the existing task rows, ranking, completion, tethering and drop controls.
const olgaRankedRow=rankedRow;
rankedRow=function(r,i){const row=olgaRankedRow(r,i);row.querySelectorAll('.tdo-work-action,.tdo-agent-chip').forEach(n=>n.remove());row.querySelectorAll('.tdo-task-title,.tdo-open-chevron').forEach(n=>n.title='Edit task');return row;};
todosTab='focus';todosLens='all';todosMode=localStorage.getItem('olga.tasks.view')||'list';
let olgaTaskArea=localStorage.getItem('olga.tasks.area')||'';
function olgaTaskAreas(cache){return [...new Set([...(cache?.areas||[]),...(cache?.domains||[]).map(d=>d.name)])].filter(Boolean);}
const olgaTodoMatches=todoMatches;
todoMatches=function(row,lens=todosLens){return (!olgaTaskArea||row.container?.name===olgaTaskArea)&&olgaTodoMatches(row,lens);};
const olgaRenderTodos=renderTodos;
renderTodos=function(){
 const all=todosCache;if(!all)return;
 if(olgaTaskArea&&!olgaTaskAreas(all).includes(olgaTaskArea)){olgaTaskArea='';localStorage.removeItem('olga.tasks.area');}
 // Filter every task surface together, including completed tasks and decisions.
 if(olgaTaskArea){const rows=(all.rows||[]).filter(r=>r.container?.name===olgaTaskArea);todosCache={...all,areas:olgaTaskAreas(all),rows,domains:(all.domains||[]).filter(d=>d.name===olgaTaskArea),counts:{...all.counts,tasks:rows.length,outstanding:0}};}
 try{olgaRenderTodos();}finally{todosCache=all;}
};

renderTodosToolbar=function(){
 const bar=document.getElementById('todosToolbar');const focused=document.activeElement?.id==='todosSearch',caret=focused?document.activeElement.selectionStart:0;
 bar.replaceChildren();const search=inputEl('Find a task…');search.id='todosSearch';search.value=todosQuery;search.type='search';search.setAttribute('aria-label','Search tasks');search.oninput=()=>{todosQuery=search.value;renderTodos();};
 const area=el('select','olga-area-filter');area.setAttribute('aria-label','Filter tasks by area');const allOption=el('option','','All areas');allOption.value='';area.append(allOption);for(const name of olgaTaskAreas(todosCache)){const option=el('option','',name);option.value=name;area.append(option);}area.value=olgaTaskArea;area.onchange=()=>{olgaTaskArea=area.value;localStorage.setItem('olga.tasks.area',olgaTaskArea);renderTodos();};
 const views=el('div','olga-task-views');for(const mode of ['list','board','timeline','homefeed']){const button=pillLight({list:'List',board:'Board',timeline:'Timeline',homefeed:'To resolve'}[mode],()=>{todosMode=mode;localStorage.setItem('olga.tasks.view',mode);renderTodos();});button.setAttribute('aria-pressed',String(todosMode===mode));button.classList.toggle('on',todosMode===mode);views.append(button);}bar.append(search,area,views,pillLight('＋ Add task',()=>openTodoQuickAdd()));if(focused){search.focus();search.setSelectionRange(caret,caret);}
};
// Surface errors instead of reporting a rejected save as successful.
goalsApi=async function(method,path,body){
 setSaveState('saving');try{const r=await fetch(path,{method,headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});if(!r.ok)throw new Error(await r.text());setSaveState('saved');await loadGoals();}catch(e){setSaveState('error');showToast(e.message);}
};
const pendingDaySaves=new Map();
let daySaveChain=Promise.resolve();
queueSave=function(endpoint,payloadFn){
 const date=state.date,key=endpoint+'|'+date,payload=JSON.parse(JSON.stringify(payloadFn()));
 clearTimeout(pendingDaySaves.get(key)?.timer);setSaveState('saving');
 const send=()=>{pendingDaySaves.delete(key);daySaveChain=daySaveChain.catch(()=>{}).then(async()=>{try{await postJSONOk('/api/'+endpoint+'?date='+date,payload);setSaveState('saved');}catch(e){setSaveState('error');showToast(e.message);throw e;}});return daySaveChain;};
 pendingDaySaves.set(key,{send,timer:setTimeout(()=>send().catch(()=>{}),500)});
};
async function flushDay(){for(const p of [...pendingDaySaves.values()]){clearTimeout(p.timer);await p.send();}await daySaveChain;}
window.addEventListener('beforeunload',e=>{if(pendingDaySaves.size||els.saveState.textContent==='saving'){e.preventDefault();e.returnValue='';}});
async function olgaRoute(){
 try{await flushDay();}catch(e){return;}
 const parts=location.hash.replace(/^#\/?/,'').split('/'),view=['day','goals','tasks','chat'].includes(parts[0])?parts[0]:'day';
 els.dayView.hidden=view!=='day';els.goalsView.hidden=view!=='goals';els.todosView.hidden=view!=='tasks';els.dateNav.hidden=view!=='day';
 els.crumbPath.textContent=view==='chat'?'LIBER':view.toUpperCase();els.crumbMeta.textContent='';
 if(view==='chat'){document.querySelectorAll('[data-olga-view]').forEach(n=>n.classList.toggle('active',n.dataset.olgaView==='chat'));if(typeof liberShow==='function')liberShow(parts);return;}
 if(typeof liberHide==='function')liberHide();
 document.querySelectorAll('[data-olga-view]').forEach(n=>n.classList.toggle('active',n.dataset.olgaView===view));
 if(view==='day')await load(state.date);
 else if(view==='goals'){if(parts[1]==='history')await showGoalsHistory();else await loadGoals(parts[1]?decodeURIComponent(parts[1]):undefined);}
 else await loadTodos();
}
for(const [view,glyph]of [['day','◷'],['goals','◎'],['tasks','☑'],['chat','✦']]){
 const a=el('a','rail-item');a.href='#/'+view;a.dataset.olgaView=view;a.append(el('span','rail-glyph',glyph),el('span','rail-label',view==='chat'?'CHAT':view.toUpperCase()));if(view==='chat'){a.title='Liber — your assistant';a.setAttribute('aria-label','Chat with Liber');}els.railGroups.append(a);
}
els.railCollapse.onclick=()=>{els.appShell.classList.toggle('rail-collapsed');};
els.crumbBack.onclick=()=>history.back();els.crumbFwd.onclick=()=>history.forward();els.crumbBack.disabled=false;els.crumbFwd.disabled=false;
for(const[id,delta]of [['prevBtn',-1],['nextBtn',1],['todayBtn',0]])document.getElementById(id).onclick=async()=>{try{await flushDay();await load(delta?shiftDate(state.date,delta):isoToday());}catch(e){showToast(e.message);}};
document.getElementById('logout').onclick=async()=>{try{await flushDay();await fetch('/logout',{method:'POST'});location.href='/';}catch(e){showToast(e.message);}};
window.addEventListener('hashchange',olgaRoute);
window.addEventListener('resize',()=>{if(!els.dayView.hidden)drawConnectors();});
window.addEventListener('keydown',e=>{if(e.key==='t'&&!e.metaKey&&!e.ctrlKey&&!['INPUT','TEXTAREA','SELECT'].includes(document.activeElement?.tagName)){e.preventDefault();openTodoQuickAdd();}});
document.body.classList.remove('booting');olgaRoute();
// Use Manifest's inline picker on mobile and embedded browsers too.
const addAreaButton=els.addArea.cloneNode(true);els.addArea.replaceWith(addAreaButton);els.addArea=addAreaButton;
addAreaButton.onclick=()=>{
 els.pickerTitle.textContent='Add area';
 const form=el('form','olga-task-details olga-task-add');
 const label=el('label','olga-detail-field');const input=inputEl('e.g. Health or Travel');input.required=true;input.autocomplete='off';label.append(el('span','','Area name'),input);
 const hint=el('p','olga-detail-hint','Areas organize your goals and tasks.');
 const error=el('p','olga-add-error');error.setAttribute('role','alert');error.hidden=true;input.oninput=()=>{error.hidden=true;};
 const actions=el('div','olga-add-actions');const cancel=el('button','pill light','Cancel');cancel.type='button';cancel.onclick=closePicker;
 const save=el('button','pill olga-primary','Add area');save.type='submit';actions.append(cancel,save);form.append(label,hint,error,actions);
 form.onsubmit=async event=>{
  event.preventDefault();const name=input.value.trim().replace(/\s+/g,' ');if(!name||save.disabled)return;
  if((state.goalsDoc?.areas||[]).some(a=>a.name.toLowerCase()===name.toLowerCase())){error.textContent='An area with this name already exists.';error.hidden=false;input.focus();return;}
  save.disabled=true;cancel.disabled=true;error.hidden=true;setSaveState('saving');
  try{await postJSONOk('/api/areas',{name});goalsSelArea=name;setSaveState('saved');closePicker();await loadGoals();}
  catch(e){setSaveState('error');error.textContent=e.message;error.hidden=false;}
  finally{save.disabled=false;cancel.disabled=false;}
 };
 els.pickerBody.replaceChildren(form);els.pickerModal.hidden=false;input.focus();
};
const olgaOrientArea=orientArea;
orientArea=function(area){
 const card=olgaOrientArea(area);
 card.querySelectorAll('button').forEach(b=>{
  if(b.textContent==='＋@')b.remove();
 });return card;
};
const mobileSignout=el('button','rail-raw olga-mobile-signout','Sign out');mobileSignout.onclick=()=>document.getElementById('logout').click();document.querySelector('.crumb-right').append(mobileSignout);

// The same task records in two useful columns; no agent workflow in this planner.
renderTodosBoard=function(host){
 const rows=(todosCache.rows||[]).filter(r=>todoMatches(r)),done=[];
 for(const dom of todosCache.domains||[])for(const t of [...(dom.tasks||[]),...(dom.buckets||[]).flatMap(b=>b.tasks||[])])if(t.state==='done'&&todoSearchMatches(t))done.push({...t,done:true,container:{name:dom.name}});
 const board=el('div','tdo-board olga-board');board.style.setProperty('--task-columns','2');
 for(const [key,label,items] of [['open','Open',rows],['done','Done',done]]){
  const col=el('section','tdo-col');const head=el('div','tdo-col-head');head.append(el('span','tdo-sec-title',label),el('span','tdo-sec-count',String(items.length)));col.append(head);
  if(!items.length)col.append(el('p','tdo-col-empty',key==='open'?'No open tasks':'Completed tasks appear here'));
  for(const r of items){const card=el('div','tdo-card');card.draggable=true;card.ondragstart=e=>e.dataTransfer.setData('text/todo-id',r.id);
   const title=el('button','tdo-card-text tdo-task-title',r.text);title.onclick=()=>openTodoPanel(r);
   const meta=el('div','tdo-card-meta',r.container?.name||'Inbox');if(r.priority)meta.append(prioMark(r.priority));
   const action=pillLight(r.done?'Reopen':'Done',()=>todosApi('/api/tasks/check',{id:r.id,checked:!r.done}));card.append(title,meta,action);col.append(card);
  }
  col.ondragover=e=>e.preventDefault();col.ondrop=e=>{e.preventDefault();const id=e.dataTransfer.getData('text/todo-id');if(id)todosApi('/api/tasks/check',{id,checked:key==='done'});};
  if(key==='open')col.append(pillLight('＋ Add task',()=>openTodoQuickAdd()));board.append(col);
 }
 host.append(board);
};
