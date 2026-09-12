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
async function openTodoPanel(row){
 const r=typeof row==='string'?(todosCache?.rows||[]).find(r=>r.id===row)||todosCompletedRow(row):row;
 if(!r)return;
 els.pickerTitle.textContent='Task details';els.pickerBody.replaceChildren(el('p','','Loading…'));els.pickerModal.hidden=false;
 try{
  const response=await fetch('/api/tasks/notes?id='+encodeURIComponent(r.id));if(!response.ok)throw new Error(await response.text());let notes=await response.json();
  if(els.pickerModal.hidden)return;
  const form=el('form','olga-task-details');
  const field=(label,node)=>{const wrap=el('label','olga-detail-field');wrap.append(el('span','',label),node);form.append(wrap);return node;};
  const title=field('Task',inputEl('Task'));title.value=r.text;title.required=true;
  const area=field('Area',selectEl([...new Set([r.container?.name||'Inbox','Inbox',...(todosCache?.areas||[])])]));area.value=r.container?.name||'Inbox';
  area.disabled=area.value==='Home';if(!area.disabled)for(const option of [...area.options])if(option.value==='Home')option.remove();
  if(area.value==='Home')form.append(el('p','olga-detail-hint','Home is shared with Benjamin.'));
  const priority=field('Priority',selectEl(['None','Low','Medium','High']));priority.value=({low:'Low',med:'Medium',high:'High'})[r.priority]||'None';
  const description=field('Description',el('textarea'));description.rows=5;description.value=notes.description||'';description.placeholder='Add details, links, or a checklist…';
  const status=el('p','olga-detail-hint');status.setAttribute('role','status');
  const save=el('button','pill','Save changes');save.type='submit';form.append(save,status);
  form.onsubmit=async e=>{e.preventDefault();save.disabled=true;status.textContent='Saving…';try{
   notes=await postJSONOk('/api/tasks/notes',{id:r.id,kind:'description',description:description.value,revision:notes.revision});
   await postJSONOk('/api/tasks/update',{id:r.id,text:title.value.trim(),domain:area.value});
   await postJSONOk('/api/tasks/priority',{id:r.id,priority:({Low:'low',Medium:'med',High:'high'})[priority.value]||''});
   r.text=title.value.trim();r.container={name:area.value};status.textContent='Saved';await loadTodos();
  }catch(e){status.textContent=e.message;}finally{save.disabled=false;}};
  const comments=el('section','olga-comments');comments.append(el('h3','','Comments'));
  const entries=el('div');const renderComments=()=>{entries.replaceChildren();for(const c of notes.comments||[]){const item=el('article','olga-comment');item.append(el('div','olga-detail-hint',(c.author_name||c.author)+' · '+new Date(c.at).toLocaleString()),el('p','',c.text));entries.append(item);}if(!entries.childElementCount)entries.append(el('p','olga-detail-hint','No comments yet.'));};renderComments();
  const composer=el('form');const comment=el('textarea');comment.rows=3;comment.placeholder='Add a comment…';comment.setAttribute('aria-label','Comment');comment.required=true;
  const send=el('button','pill','Add comment');send.type='submit';const error=el('p','olga-detail-hint');error.setAttribute('role','status');composer.append(comment,send,error);
  composer.onsubmit=async e=>{e.preventDefault();if(!comment.value.trim())return;send.disabled=true;try{const updated=await postJSONOk('/api/tasks/notes',{id:r.id,kind:'comment',comment:comment.value});notes.comments=updated.comments;comment.value='';renderComments();error.textContent='';}catch(e){error.textContent=e.message;}finally{send.disabled=false;}};
  comments.append(entries,composer);els.pickerBody.replaceChildren(form,comments);title.focus();
 }catch(e){els.pickerBody.replaceChildren(el('p','',e.message));}
}
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
  try{await postJSONOk('/api/tasks/item',{text:input.value.trim(),domain:domain.value==='Inbox'?'':domain.value,...(options.rock?{rock:options.rock}:{})});closePicker();await loadTodos();if(!els.goalsView.hidden)await loadGoals();}
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
 const views=el('div','olga-task-views');for(const mode of ['list','board']){const button=pillLight(mode==='list'?'List':'Board',()=>{todosMode=mode;localStorage.setItem('olga.tasks.view',mode);renderTodos();});button.setAttribute('aria-pressed',String(todosMode===mode));button.classList.toggle('on',todosMode===mode);views.append(button);}bar.append(search,area,views,pillLight('＋ Add task',()=>openTodoQuickAdd()));if(focused){search.focus();search.setSelectionRange(caret,caret);}
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
 const parts=location.hash.replace(/^#\/?/,'').split('/'),view=['day','goals','tasks'].includes(parts[0])?parts[0]:'day';
 els.dayView.hidden=view!=='day';els.goalsView.hidden=view!=='goals';els.todosView.hidden=view!=='tasks';els.dateNav.hidden=view!=='day';
 els.crumbPath.textContent=view.toUpperCase();els.crumbMeta.textContent='';
 document.querySelectorAll('[data-olga-view]').forEach(n=>n.classList.toggle('active',n.dataset.olgaView===view));
 if(view==='day')await load(state.date);
 else if(view==='goals'){if(parts[1]==='history')await showGoalsHistory();else await loadGoals(parts[1]?decodeURIComponent(parts[1]):undefined);}
 else await loadTodos();
}
for(const [view,glyph]of [['day','◷'],['goals','◎'],['tasks','☑']]){
 const a=el('a','rail-item');a.href='#/'+view;a.dataset.olgaView=view;a.append(el('span','rail-glyph',glyph),el('span','rail-label',view.toUpperCase()));els.railGroups.append(a);
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
addAreaButton.onclick=()=>askText('Add area','Area name',name=>{if(name.trim())goalsApi('POST','/api/areas',{name:name.trim()});});
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
