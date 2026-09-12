// The right workspace owns view state only. Each side conversation uses the
// existing chat application in an isolated document, including its outbox,
// approvals, attachments and agent picker; it never shares main-chat globals.
const chatEmbedded = window.parent !== window && new URLSearchParams(location.search).get('chatPane') === '1';
if(chatEmbedded)document.documentElement.classList.add('chat-embedded');
let chatWorkspaceTabs = null;
const chatWorkspaceStates=new Map();
let chatWorkspaceRestoreTicket=0;
function chatWorkspaceState(key){
 if(!key||typeof ChatDraftState==='undefined'||chatIsPortal())return null;
 if(!chatWorkspaceStates.has(key))chatWorkspaceStates.set(key,new ChatDraftState(key,state=>{if(state.conflict)state.resolve(true);},'workspace'));
 return chatWorkspaceStates.get(key);
}
async function chatRestoreWorkspace(){
 const key=document.getElementById('chatTranscript')?.dataset.readKey;
 if(!key||chatEmbedded||chatWorkspaceTabs)return;
 const state=chatWorkspaceState(key);if(!state)return;
 const ticket=++chatWorkspaceRestoreTicket;await state.refresh();
 if(ticket!==chatWorkspaceRestoreTicket||chatWorkspaceTabs||document.getElementById('chatTranscript')?.dataset.readKey!==key)return;
 const saved=state.value;if(!saved||!Array.isArray(saved.tabs))return;
 const w=chatEnsureWorkspace(true);
 for(const tab of saved.tabs){
  if(tab.spec?.kind==='artifact'&&(tab.spec.id||tab.spec.plan&&tab.spec.task))chatOpenWorkingArtifact({...tab.spec,revision:tab.view?.revision||tab.spec.revision});
  else if(tab.spec?.kind==='side-setup'&&tab.spec.source?.id)chatWorkspaceSideSetup(tab.spec.source,{key:tab.key,view:tab.view});
  else if(tab.spec?.kind==='attachment'&&typeof tab.spec.file?.name==='string'&&typeof tab.spec.href==='string'&&tab.spec.href.startsWith('/api/'))chatOpenAttachment(tab.spec.file,tab.spec.href);
  else if(tab.spec?.kind==='side'&&/^#\/chat\//.test(tab.spec.route||''))w.tab(tab.key,'Side chat',host=>chatMountSideFrame(host,tab.spec),tab.spec);
  else if(tab.spec?.kind==='activity')chatOpenActivity();
  else if(tab.spec?.kind==='context')chatOpenContext();
  else if(tab.spec?.kind==='files')chatOpenFiles();
  else if(tab.spec?.kind==='project'&&chatWorkstreams.groups[tab.spec.id])chatEditProject(tab.spec.id);
  const opened=w.entries.get(tab.key);if(opened?.api?.restoreView&&tab.view)opened.restoreView=tab.view;
 }
 if(w.entries.has(saved.active))w.select(saved.active);
 w.show(saved.open===true);w.restoring=false;
}
window.addEventListener('pagehide',()=>{chatWorkspaceTabs?.save?.();for(const state of chatWorkspaceStates.values())if(state.dirty)state.flush();});
function chatWorkspaceSource(){
 if(chatIsTerm()&&chatTermOpen?.se.id===chatOpenId){
  const o=chatTermOpen,se=o.se,tasks=(o.conversation?.links||[]).filter(l=>l.kind==='task');
  return {backend:'terminal',agent:se.kind,id:se.id,title:se.name||se.kind,model:se.model||'',cwd:se.cwd||'',task:tasks.length===1?tasks[0].id:''};
 }
 if(chatCurSession?.id===chatOpenId&&chatOpenId&&!chatIsPortal()&&chatRosterEntry(chatAgent)?.durableSend){
  return {agent:chatAgent,id:chatOpenId,title:chatCurSession.title||'Conversation',model:chatCurSession.model||chatRosterEntry(chatAgent)?.model||'',task:chatCurSession.task||''};
 }
 return null;
}
function chatWorkspaceIcon(kind){
 const paths={folder:'M3 7V5h6l2 2h10v13H3z',panel:'M4 4h16v16H4z M15 4v16',terminal:'M4 4h16v16H4z M7 8l3 3-3 3 M12 15h5',review:'M6 3h9l4 4v14H6z M14 3v5h5 M9 12h7 M9 16h7',chat:'M20 11a8 8 0 0 1-8 8H5l-3 3v-11a9 9 0 0 1 18 0z M8 11h8 M12 7v8',file:'M6 3h9l4 4v14H6z M14 3v5h5'};
 const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('aria-hidden','true');svg.classList.add('chat-workspace-icon');const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d',paths[kind]||paths.file);svg.append(path);return svg;
}
function chatWorkspaceHeader(head){
 queueMicrotask(()=>chatRestoreWorkspace());
 for(const child of Array.from(head.children))if(child.tagName==='BUTTON'&&child.textContent.startsWith('Agent: '))child.classList.add('chat-recipient-control');
 queueMicrotask(()=>{const composer=document.getElementById('chatComposer');if(composer?.dataset.built)chatPolishComposer(composer);});
 if(chatEmbedded){const more=head.querySelector('.chat-details');if(more)for(const button of Array.from(head.children))if(button.matches('.chat-terminal-view')||button.textContent==='Changes')more.append(button);return;}
 const more=head.querySelector('.chat-details');
 for(const child of Array.from(head.children)){
  if(child.textContent==='Changes'&&more)more.append(child);
  if(child.matches('.chat-terminal-view')){child.setAttribute('aria-label','Terminal');child.title='Toggle terminal';child.replaceChildren(chatWorkspaceIcon('terminal'));child.classList.add('chat-icon-button');}
  if(child.tagName==='BUTTON'&&child.textContent.startsWith('Agent: ')){child.setAttribute('aria-label',child.textContent);child.textContent=child.textContent.slice(7);}
 }
 const summary=more?.querySelector(':scope > summary');if(summary){summary.textContent='···';summary.setAttribute('aria-label','Conversation options');summary.title='Conversation options';more.classList.add('chat-options-compact');}
 const button=el('button','sprt-quiet chat-workspace-toggle chat-icon-button');button.append(chatWorkspaceIcon('panel'));
 button.setAttribute('aria-label','Toggle workspace');button.setAttribute('aria-expanded',String(!!chatWorkspaceTabs&&!chatWorkspaceTabs.pane.hidden));
 button.title='Plans, files and side chats · Ctrl+Alt+I';button.setAttribute('aria-keyshortcuts','Control+Alt+i');
 button.onclick=()=>{if(chatWorkspaceTabs)chatWorkspaceTabs.show(chatWorkspaceTabs.pane.hidden);else chatEnsureWorkspace();};head.append(button);
 // Phones keep the opener out of the primary head: the same action sits in the
 // ··· menu (95-mobile.css shows one or the other; show() updates both).
 if(more){const entry=el('button','sprt-quiet chat-workspace-toggle mf-chat-ws-more','Workspace');entry.title='Plans, files and side chats';entry.setAttribute('aria-expanded',button.getAttribute('aria-expanded'));entry.onclick=button.onclick;more.insertBefore(entry,more.querySelector(':scope > .chat-head-acts'));}
}
// A tab label keeps the tail of a long name — a screenshot's time and
// extension, a plan's suffix — by shortening the middle; the full name stays
// on the accessible name and the tooltip (2026-09-12).
function chatWorkspaceTabLabel(name){
 const s=String(name||'').trim();
 if(s.length<=26)return s;
 return s.slice(0,13).trimEnd()+'…'+s.slice(-9).trimStart();
}
function chatEnsureWorkspace(restoring=false){
 if(chatWorkspaceTabs)return chatWorkspaceTabs;
 const shell=document.querySelector('.chat-shell'),pane=el('aside','artifact-workspace chat-tab-workspace');
 pane.setAttribute('aria-label','Chat workspace');pane.hidden=true;
 const bar=el('div','chat-workspace-bar'),tabs=el('div','chat-workspace-tabs'),add=el('button','sprt-quiet','+'),hide=el('button','sprt-quiet','×'),body=el('div','chat-workspace-body');
 tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Workspace tabs');
 add.setAttribute('aria-label','Add workspace tab');hide.setAttribute('aria-label','Hide workspace');hide.title='Hide workspace; keep tabs and drafts';
 bar.append(tabs,add,hide);pane.append(bar,body);shell.append(pane);
 const savedState=chatWorkspaceState(document.getElementById('chatTranscript')?.dataset.readKey);
 const entries=new Map();let active=null,disposed=false,chooserHost=null;
 const clearChooser=()=>{chooserHost?.remove();chooserHost=null;pane.classList.remove('choosing');};
 const revealActive=()=>{const row=entries.get(active)?.row;if(!row||pane.hidden)return;const bounds=tabs.getBoundingClientRect(),selected=row.getBoundingClientRect();if(selected.left<bounds.left)tabs.scrollLeft-=bounds.left-selected.left;else if(selected.right>bounds.right)tabs.scrollLeft+=selected.right-bounds.right;};
 const tabResize=new ResizeObserver(revealActive);tabResize.observe(tabs);
 const w={pane,body,entries,restoring,
  save(){if(disposed||w.restoring||!savedState)return;for(const t of entries.values())if(!pane.hidden&&!t.host.hidden)t.view=t.api?.getView?.()||t.view;savedState.set({open:!pane.hidden,active,tabs:[...entries].filter(([,t])=>t.spec).map(([key,t])=>({key,spec:t.spec,view:t.restoreView||t.view||{}}))});},
  show(open=true){w.save();pane.hidden=!open;shell.classList.toggle('has-artifact',open);shell._refreshPaneWidths?.();document.querySelectorAll('.chat-workspace-toggle').forEach(b=>b.setAttribute('aria-expanded',String(open)));if(!open)document.querySelector('#chatComposer textarea')?.focus();if(open){revealActive();const t=entries.get(active);if(t?.restoreView){const view=t.restoreView;Promise.resolve(t.api?.restoreView?.(view)).then(ok=>{if(ok&&t.restoreView===view)t.restoreView=null;});}}w.save();},
  select(key){w.save();clearChooser();active=key;for(const [id,t] of entries){t.host.hidden=id!==key;t.button.setAttribute('aria-selected',String(id===key));t.button.tabIndex=id===key?0:-1;}w.show();},
  drop(key){const t=entries.get(key);if(!t)return;t.host.remove();t.row.remove();entries.delete(key);if(!disposed&&active===key){const next=Array.from(entries.keys()).at(-1);if(next)w.select(next);else w.chooser();}w.save();},
  tab(key,title,build,spec=null){if(entries.has(key)){w.select(key);return entries.get(key);}
   const row=el('div','chat-workspace-tab'),button=el('button','sprt-quiet',chatWorkspaceTabLabel(title)),close=el('button','sprt-quiet','×'),host=el('div','chat-workspace-tabbody');
   button.setAttribute('role','tab');button.title=title;button.setAttribute('aria-label',title);host.setAttribute('role','tabpanel');
   const uid='workspace-'+crypto.randomUUID();host.id=uid;button.id=uid+'-tab';button.setAttribute('aria-controls',uid);host.setAttribute('aria-labelledby',button.id);
   close.setAttribute('aria-label','Close '+title+' tab');row.append(button,close);tabs.append(row);body.append(host);
   const t={row,button,host,api:null,spec};entries.set(key,t);button.onclick=()=>w.select(key);
   button.onkeydown=e=>{const keys=Array.from(entries.keys());let i=keys.indexOf(key);if(e.key==='ArrowRight')i=(i+1)%keys.length;else if(e.key==='ArrowLeft')i=(i+keys.length-1)%keys.length;else if(e.key==='Home')i=0;else if(e.key==='End')i=keys.length-1;else return;e.preventDefault();w.select(keys[i]);entries.get(keys[i]).button.focus();};
   close.onclick=()=>{t.api?.close?.();w.drop(key);};w.select(key);t.api=build(host,()=>w.drop(key));
   // Back returns to chat while keeping this editor mounted. Explicit tab close
   // still invokes the component's original disposal/flush handler.
   const back=t.api?.element?.querySelector('.artifact-workspace-head button');
   if(back){const dispose=back.onclick;back.onclick=()=>w.show(false);t.api.close=dispose;}
   const heading=t.api?.element?.querySelector('.artifact-workspace-title');
   if(heading){
    const update=()=>{
     const name=heading.textContent&&heading.textContent!=='Loading…'?heading.textContent:title;
     const draft=t.api.element.dataset.draft==='true';
     button.textContent=chatWorkspaceTabLabel(name);button.setAttribute('aria-label',name);button.title=name+(draft?' · Unfinished edit':'');
     button.setAttribute('aria-description',draft?'Unfinished edit':'');row.classList.toggle('has-draft',draft);
     close.setAttribute('aria-label','Close '+name+' tab');close.title=draft?'Close tab; your draft remains saved':'Close tab';
    };
    const observer=new MutationObserver(update);observer.observe(heading,{childList:true,characterData:true,subtree:true});observer.observe(t.api.element,{attributes:true,attributeFilter:['data-draft']});
    const dispose=t.api.close;t.api.close=()=>{observer.disconnect();dispose?.();};update();
   }
   host.addEventListener('scroll',()=>w.save(),true);host.addEventListener('toggle',()=>w.save(),true);host.addEventListener('change',()=>queueMicrotask(()=>w.save()));w.save();
   return t;
  },
  chooser(){clearChooser();chooserHost=el('div','chat-workspace-picker');pane.classList.add('choosing');if(entries.size){chooserHost.classList.add('chat-workspace-popover');pane.append(chooserHost);}else body.append(chooserHost);chatWorkspaceChooser(chooserHost);w.show();},
  close(){if(disposed)return;tabResize.disconnect();w.save();savedState?.flush();++chatWorkspaceRestoreTicket;disposed=true;for(const t of Array.from(entries.values()))t.api?.close?.();entries.clear();pane.remove();shell.classList.remove('has-artifact');chatWorkspaceTabs=null;chatWorkspace=null;shell._refreshPaneWidths?.();}
 };
 chatWorkspaceTabs=w;chatWorkspace=w;add.onclick=()=>{if(chooserHost&&entries.size)clearChooser();else w.chooser();};hide.onclick=()=>w.show(false);
 pane.addEventListener('keydown',e=>{if(e.key==='Escape'){if(chooserHost&&entries.size){clearChooser();add.focus();}else w.show(false);}});
 pane.addEventListener('pointerdown',e=>{if(chooserHost&&entries.size&&!chooserHost.contains(e.target)&&!add.contains(e.target))clearChooser();});
 chatInstallPaneResize(shell);w.chooser();return w;
}
function chatWorkspaceChooser(host){
 const source=chatWorkspaceSource(),chooser=el('div','chat-workspace-chooser');
 const action=(name,description,icon,fn)=>{const b=el('button','chat-workspace-option');b.title=description;b.append(chatWorkspaceIcon(icon),el('span','',name));b.onclick=fn;chooser.append(b);};
 if(source)action('Files','Browse outputs and referenced files','file',()=>chatOpenFiles());
 if(source)action('Activity','Inspect recorded instructions, narration and tool output','review',()=>chatOpenActivity());
 const project=chatCurrentProject();
 if(source)action('Context','Inspect recorded inputs and project instructions','folder',()=>chatOpenContext());
 if(source?.backend==='terminal'){
  action('Review','Review this working folder','review',()=>chatChangesButton({id:source.id}).click());
  action('Terminal','Open the live terminal below chat','terminal',()=>{chatWorkspaceTabs.show(false);chatOpenTerminalPane(chatTermOpen.se);});
 }
 const task=source?.task||chatTaskID;
 if(task){
  fetch('/api/tasks/panel?id='+encodeURIComponent(task)).then(r=>{if(!r.ok)throw Error();return r.json();}).then(data=>{
   if(!host.isConnected)return;
   if(data.record?.plan)action('Plan','Review and edit the linked plan','file',()=>chatOpenWorkingArtifact({plan:true,task}));
   const files=[...(data.artifacts?.outputs||[]),...(data.artifacts?.inputs||[])].filter((a,i,all)=>all.findIndex(b=>b.id===a.id)===i&&!a.unknown&&a.provenance?.source!=='task-plan'&&!(source?.backend==='terminal'&&(a.title==='Working-folder changes'||a.provenance?.source==='terminal-changes')));
   if(files.length){const list=el('details','chat-workspace-files');const summary=el('summary','');summary.append(chatWorkspaceIcon('file'),el('span','','Files'),el('span','chat-workspace-count',String(files.length)));list.append(summary);for(const a of files){const button=el('button','chat-workspace-file',a.title||a.ref||'File');button.title=a.ref||a.title;button.onclick=()=>chatOpenWorkingArtifact({id:a.id,task});list.append(button);}chooser.append(list);}
  }).catch(()=>{const retry=el('button','sprt-quiet','Retry loading files');retry.onclick=()=>{host.replaceChildren();chatWorkspaceChooser(host);};chooser.append(retry);});
 }
 if(source)action('Side chat','Start with this conversation’s context','chat',()=>chatWorkspaceSideSetup(source));
 const keys=el('details','chat-workspace-shortcuts');keys.append(el('summary','','Keyboard shortcuts'),el('p','','Ctrl+Alt: N new chat · F search · M composer · I workspace · ↑/↓ switch chat · J next needing attention · X request stop (Enter confirms)'));chooser.append(keys);
 host.append(chooser);
}
function chatMountSideFrame(host,spec){
 const strip=el('div','chat-side-context'),info=el('details','');
 info.append(el('summary','','Context from '+spec.title),el('p','','Snapshot of recent complete turns at creation. Older history may be omitted; tool traces and attachment contents are excluded. Selected artifact versions are included separately. This is a saved private conversation; closing its tab does not delete it.'));
 const link=el('a','sprt-quiet','Open full chat ↗');link.href=spec.route;link.target='_blank';link.rel='noopener';strip.append(info,link);
 const frame=document.createElement('iframe');frame.title='Side chat · '+spec.title;frame.className='chat-side-frame';frame.src=location.pathname+'?chatPane=1'+spec.route;
 host.append(strip,frame);
 const accepted=new Set();
 const receive=e=>{
  if(e.origin!==location.origin||e.source!==frame.contentWindow||!host.isConnected||e.data?.type!=='manifest-side-finding')return;
  const {id,text}=e.data;if(typeof id!=='string'||id.length>80||typeof text!=='string'||!text.trim()||text.length>32000)return;
  const input=document.querySelector('#chatComposer textarea');if(!input||input.disabled)return;
  if(!accepted.has(id)){input.value=(input.value.trim()?input.value+'\n\n':'')+'From side chat ('+spec.route+'):\n'+text;input.dispatchEvent(new Event('input',{bubbles:true}));accepted.add(id);}
  frame.contentWindow.postMessage({type:'manifest-side-finding-ack',id},location.origin);if(window.matchMedia('(max-width: 900px)').matches)chatWorkspaceTabs?.show(false);input.focus();
 };
 window.addEventListener('message',receive);return {close:()=>window.removeEventListener('message',receive)};
}
function chatWorkspaceSideSetup(source,restore=null){
 const key=restore?.key||'side-setup-'+crypto.randomUUID(),w=chatEnsureWorkspace(),saved=restore?.view||{};
 w.tab(key,'New side chat',(host)=>{
  const wrap=el('div','chat-side-setup'),heading=el('h3','','Side chat'),hint=el('p','','Starts with this conversation’s recent context.');
  const pick=document.createElement('select');pick.setAttribute('aria-label','Side chat agent');
  const options=chatRoster.filter(a=>a.enabled&&a.durableSend).map(a=>({value:a.name,label:a.label,model:a.model||''}));
  if(chatTermEnabled)Object.entries(chatTermKinds).forEach(([value,label])=>options.push({value:'terminal:'+value,label,model:''}));
  options.forEach(a=>{const o=document.createElement('option');o.value=a.value;o.textContent=a.label;pick.append(o);});
  const recipient=chatRecipients.get(source.agent+'/'+source.id),initial=recipient?(recipient.backend==='terminal'?'terminal:':'')+recipient.agent:(source.backend==='terminal'?'terminal:':'')+source.agent;
  pick.value=saved.agent||initial;
  const model=document.createElement('select');model.setAttribute('aria-label','Side chat model');model.value=recipient?.model||source.model;model.placeholder='Configured default';
  const cwd=document.createElement('input');cwd.setAttribute('aria-label','Side chat working folder');cwd.value=saved.cwd??source.cwd??'';cwd.placeholder='Default working folder';
  const field=(label,input)=>{const l=el('label','',label);l.append(input);return l;};
  const folder=field('Working folder',cwd),advanced=el('details','chat-side-options');advanced.append(el('summary','','Working folder'),folder);
  const sync=()=>{folder.hidden=!pick.value.startsWith('terminal:');chatPopulateModelSelect(model,pick.value.startsWith('terminal:')?pick.value.slice(9):pick.value,pick.value===initial?(recipient?.model||source.model):options.find(a=>a.value===pick.value)?.model||'');};sync();if(saved.model)chatPopulateModelSelect(model,pick.value.startsWith('terminal:')?pick.value.slice(9):pick.value,saved.model);pick.onchange=()=>{model.value=pick.value===initial?(recipient?.model||source.model):options.find(a=>a.value===pick.value)?.model||'';sync();};
  const status=el('p','chat-workspace-hint');status.setAttribute('role','status');
  const start=el('button','chat-side-start','Open side chat');start.disabled=!pick.value;
  // Recovery is scoped to this setup tab. Keep the accepted request identity on
  // uncertain responses; keep settings fixed until the result is confirmed.
  let remembered=saved.pending||null;
  const lock=()=>{pick.disabled=model.disabled=cwd.disabled=!!remembered;};lock();if(remembered){status.textContent='Creation is unconfirmed. Retry checks the same request.';start.textContent='Retry creation';}
  start.onclick=async()=>{
   const coding=pick.value.startsWith('terminal:');let payload={agent:coding?pick.value.slice(9):pick.value,model:model.value.trim(),title:Array.from('Side chat · '+source.title).slice(0,240).join(''),task:source.task||'',prompt:source.initialPrompt||'',mode:'side',...(coding?{backend:'terminal',cwd:cwd.value.trim()}:{})};
   const selected=chatArtifactSelections.get('chat:'+source.agent+'/'+source.id);if(selected)payload.artifacts=[{id:selected.id,revision:selected.revision}];
   if(remembered)payload=remembered.payload;else remembered={payload,requestId:crypto.randomUUID()};lock();w.save();
   start.disabled=true;status.textContent='Preparing context…';
   try{
    const endpoint=source.backend==='terminal'?'/api/terminal/'+encodeURIComponent(source.agent)+'/session/'+encodeURIComponent(source.id)+'/related':chatBaseFor(source.agent)+'/'+encodeURIComponent(source.id)+'/related';
    const result=await postJSONOk(endpoint,{...payload,requestId:remembered.requestId});
    if(!result.id||!result.conversation?.route?.startsWith('#/chat/'))throw Error('Creation was not confirmed. Retry to check the same request.');
    if(!host.isConnected)return;
    await chatLoadWorkstreams();
    const sourceKey=(source.backend==='terminal'?'terminal:':'agent:')+source.agent+'/'+source.id;
    const group=chatWorkstreamMember(sourceKey),childKey=(coding?'terminal:':'agent:')+result.agent+'/'+result.id;
    if(group&&!chatWorkstreamMember(childKey))await chatSaveWorkstream(childKey,'',group,'');
    if(!host.isConnected)return;
    host.replaceChildren();
    const spec={kind:'side',title:source.title,route:result.conversation.route};
    w.entries.get(key).api=chatMountSideFrame(host,spec);w.entries.get(key).button.textContent='Side chat';w.entries.get(key).spec=spec;w.save();
   }catch(e){status.textContent=e.message||'Could not create side chat. Retry safely.';start.textContent='Retry creation';}finally{start.disabled=false;}
  };
  if(source.initialPrompt)wrap.append(el('p','chat-workspace-hint',source.initialPrompt));
  wrap.append(heading,hint,field('Agent',pick),field('Model',model),advanced,status,start);host.append(wrap);host.addEventListener('input',()=>w.save());return {getView:()=>({agent:pick.value,model:model.value,cwd:cwd.value,pending:remembered})};
 },{kind:'side-setup',source});
}

// Keep routing beside the instruction, while reusing the existing recipient
// chooser and all of its capability/authorization checks.
function chatPolishComposer(host){
 const main=host.closest('.chat-main'),source=document.querySelector('#chatThreadHeader .chat-recipient-control');
 let picker=host.querySelector('.chat-composer-recipient');
 if(source){
  if(!picker){picker=el('button','sprt-quiet chat-composer-recipient');picker.setAttribute('aria-label','Choose agent or model');picker.onclick=()=>document.querySelector('#chatThreadHeader .chat-recipient-control')?.click();host.append(picker);}
  const recipient=chatRecipients.get((chatAgent||'spirits')+'/'+(chatOpenId||'new'));
  const model=recipient?.model||(chatIsTerm()?chatTermOpen?.se.model:chatCurSession?.model)||'';
  const agent=chatAgentLabel(recipient?.agent||chatAgent);
  picker.textContent=(model?shortModel(model):agent)+' ⌄';picker.title=agent+(model?' · '+model:'')+' · Choose agent or model';
 }else picker?.remove();
 main?.classList.toggle('has-composer-recipient',!!source);
 const input=host.querySelector('textarea'),send=host.querySelector('.chat-send');
 if(send&&send.textContent!=='…'){send.setAttribute('aria-label','Send message');send.title=window.matchMedia('(max-width: 860px)').matches?'Send message · Enter adds a new line':'Send message · Enter (Shift+Enter for a new line)';}
 host.querySelector('.chat-attach')?.setAttribute('aria-label','Attach files');
 let status=host.querySelector('.chat-composer-status');
 if(chatIsTerm()&&input){
  const raw=chatTermPlaceholder();
  const hint=raw.includes('Sending ')?raw.slice(raw.indexOf('Sending ')):raw.includes('unavailable')?'Runtime unavailable · check Terminal':raw==='sending…'?'Sending…':'';
  if(hint){if(!status){status=el('div','chat-composer-status');status.setAttribute('role','status');host.append(status);}status.textContent=hint;}else status?.remove();
  input.placeholder='Message…';
 }else status?.remove();
 input?._grow?.();
}

// Keep the return-to-latest action beside the reading surface, outside its scroll area.
function chatUpdateJump(){
 const transcript=document.getElementById('chatTranscript'),main=transcript?.closest('.chat-main');
 if(!main)return;
 let button=main.querySelector('.chat-jump-latest');
 if(!button){
  button=el('button','chat-jump-latest','↓');button.setAttribute('aria-label','Jump to latest messages');button.title='Jump to latest messages';
  button.onclick=()=>{chatStick=true;chatPin();chatSaveReadingPosition();document.querySelector('#chatComposer textarea')?.focus({preventScroll:true});};
  main.append(button);
  const resize=new ResizeObserver(()=>chatUpdateJump());resize.observe(transcript);
 }
 button.hidden=main.classList.contains('landing')||transcript.clientHeight===0||transcript.scrollHeight-transcript.scrollTop-transcript.clientHeight<=80;
 if(!button.hidden){const bounds=transcript.getBoundingClientRect(),parent=main.getBoundingClientRect();button.style.top=Math.max(0,bounds.bottom-parent.top-52)+'px';}
}

function chatCopyResponseControl(blocks){
 const text=blocks.filter(block=>block.t==='say').map(block=>block.text||'').filter(Boolean).join('\n\n');
 if(!text)return null;
 const recentlyCopied=chatCopyResponseControl.last?.text===text&&Date.now()-chatCopyResponseControl.last.at<2000;
 const button=el('button','chat-copy-response',recentlyCopied?'Copied':'Copy');button.setAttribute('aria-label',recentlyCopied?'Response copied':'Copy response');button.title='Copy response as text';
 if(recentlyCopied)setTimeout(()=>{if(button.isConnected){button.textContent='Copy';button.setAttribute('aria-label','Copy response');}},2000-(Date.now()-chatCopyResponseControl.last.at));
 button.onclick=async()=>{
  button.disabled=true;
  try{await navigator.clipboard.writeText(text);chatCopyResponseControl.last={text,at:Date.now()};button.textContent='Copied';button.setAttribute('aria-label','Response copied');}
  catch(error){button.textContent='Try again';button.title='Clipboard unavailable. Select the response text to copy it.';button.setAttribute('aria-label','Copy failed; try again');}
  finally{button.disabled=false;setTimeout(()=>{if(button.isConnected){button.textContent='Copy';button.setAttribute('aria-label','Copy response');}},2000);}
 };
 if(chatEmbedded){
  const actions=el('span','chat-response-actions'),send=el('button','chat-copy-response','Add to parent draft');send.title='Append this response to the parent composer without sending';const id=crypto.randomUUID();
  send.onclick=()=>{if(text.length>32000){send.textContent='Response too long · copy an excerpt';return;}send.disabled=true;window.parent.postMessage({type:'manifest-side-finding',id,text},location.origin);
   const receive=e=>{if(e.origin!==location.origin||e.source!==window.parent||e.data?.type!=='manifest-side-finding-ack'||e.data.id!==id)return;clearTimeout(timer);window.removeEventListener('message',receive);send.textContent='Added to parent draft';};
   window.addEventListener('message',receive);const timer=setTimeout(()=>{window.removeEventListener('message',receive);send.disabled=false;send.textContent='Retry adding to parent';},5000);
  };actions.append(button,send);return actions;
 }
 return button;
}

let chatCodingCatalogPromise;
function chatPopulateModelSelect(select,kind,requested=''){
 const ticket=Symbol();select._modelTicket=ticket;select.replaceChildren();
 const initial=el('option','',requested||'Configured default');initial.value=requested;select.append(initial);select.value=requested;
 if(!['codex','claude'].includes(kind))return;
 if(!chatCodingCatalogPromise)chatCodingCatalogPromise=fetch('/api/terminal/models').then(r=>{if(!r.ok)throw Error('Models unavailable');return r.json();}).catch(e=>{chatCodingCatalogPromise=null;throw e;});
 chatCodingCatalogPromise.then(all=>{
  if(select._modelTicket!==ticket)return;
  const catalog=all[kind];if(!catalog)return;select.dataset.default=catalog.default||'';const value=requested||catalog.default||'';
  select.replaceChildren();for(const m of catalog.models||[]){const o=el('option','',m.label);o.value=m.id;select.append(o);}
  if(value&&!Array.from(select.options).some(o=>o.value===value)){const o=el('option','',value+' · current');o.value=value;select.prepend(o);}
  select.value=value;select.dispatchEvent(new Event('change'));select.title='Models configured for this server';
 }).catch(()=>{if(select._modelTicket===ticket)select.title='Model list unavailable. Using the displayed configured/current model.';});
}

// Explicit control+option/alt shortcuts avoid ordinary typing and browser tabs.
function chatWorkbenchShortcut(event){
 if(!event.ctrlKey||!event.altKey||event.metaKey||event.shiftKey||event.isComposing||event.repeat||event.defaultPrevented)return;
 if(!location.hash.startsWith('#/chat')||document.querySelector('dialog[open]'))return;
 let target=null;
 switch(event.code){
  case 'KeyN':target=document.querySelector('#chatHeadActions button');break;
  case 'KeyF':target=document.querySelector('.chat-inbox-search');break;
  case 'KeyM':target=document.querySelector('#chatComposer textarea');break;
  case 'KeyI':target=document.querySelector('.chat-head > .chat-workspace-toggle');break;
  case 'KeyX':target=document.querySelector('#chatThreadHeader .chat-stop-agent');break;
  case 'ArrowDown':case 'ArrowUp':case 'KeyJ':{
   const rows=[...document.querySelectorAll('#chatInboxRows .chat-rail-row')].filter(row=>row.getClientRects().length);
   const current=rows.findIndex(row=>row.classList.contains('open')),step=event.code==='ArrowUp'?-1:1;
   for(let n=1;n<=rows.length;n++){const row=rows[(current+step*n+rows.length*2)%rows.length];if(event.code!=='KeyJ'||row.dataset.execution==='waiting_user'||row.dataset.execution==='failed'||row.querySelector('.chat-row-attention')){target=row;break;}}
   break;
  }
  default:return;
 }
 if(!target||target.disabled||!target.getClientRects().length)return;
 event.preventDefault();if(event.code==='KeyX'){target.focus();if(!target.classList.contains('armed'))target.click();return;}if(['KeyF','KeyM'].includes(event.code))target.focus();else target.click();
}
document.addEventListener('keydown',chatWorkbenchShortcut);

// Read-only projection of the same authorized transcript used by the center.
// It subscribes to existing transcript renders; it never starts another poller.
let chatWorkbenchActivity=null;
function chatWorkbenchActivityUpdate(turns,operations=[],proposals=[],context={}){
 if(chatIsPortal())return;
 const next={key:chatAgent+'/'+chatOpenId,turns,operations,proposals,context};
 const signature=JSON.stringify(next);if(chatWorkbenchActivity?.signature===signature)return;
 chatWorkbenchActivity={...structuredClone(next),signature};
 window.dispatchEvent(new Event('chat-workbench-activity'));
}
function chatActivityRows(data){
 const rows=[];
 for(const [i,turn] of (data?.turns||[]).entries()){
  const id=String(turn.id??turn.n??i),at=turn.ts||turn.at||'';
  if(turn.who==='user'||turn.who==='system'){
   rows.push({id:id+':message',kind:turn.who==='user'?'instructions':'narration',title:turn.who==='user'?'Instruction':'System note',text:turn.text||'',at});continue;
  }
  for(const [j,b] of chatTurnBlocks(turn).entries()){
   const kind=b.error?'errors':b.t==='say'||b.t==='think'?'narration':'tools';
   rows.push({id:id+':'+j,kind,title:b.error?'Failed · '+(b.cast||'tool'):b.t==='say'?'Agent':b.t==='think'?'Thinking':b.cast||'Tool',text:b.text||b.input||'',output:b.result||'',at});
  }
 }
 return rows;
}
function chatOpenActivity(){
 if(chatIsPortal())return;
 return chatEnsureWorkspace().tab('activity','Activity',(host,drop)=>{
  const key=chatAgent+'/'+chatOpenId,pane=el('section','chat-activity-inspector');
  const toolbar=el('div','chat-activity-toolbar'),filter=document.createElement('select'),status=el('span','chat-activity-count');
  filter.className='pp-in';filter.setAttribute('aria-label','Filter activity');
  for(const [value,label] of [['all','All activity'],['instructions','Instructions'],['narration','Agent narration'],['tools','Tools and commands'],['errors','Errors'],['approvals','Approvals']]){const option=el('option','',label);option.value=value;filter.append(option);}
  toolbar.append(filter,status);const list=el('div','chat-activity-list');list.tabIndex=0;list.setAttribute('aria-label','Recorded activity');
  pane.append(toolbar,list);host.append(pane);let closed=false;const expanded=new Set();
  const render=()=>{
   if(closed||!host.isConnected||key!==chatAgent+'/'+chatOpenId)return;
   const data=chatWorkbenchActivity?.key===key?chatWorkbenchActivity:null,position=list.scrollTop;
   const rows=chatActivityRows(data).filter(r=>filter.value==='all'||r.kind===filter.value||(filter.value==='tools'&&r.kind==='errors'));
   list.replaceChildren();status.textContent=rows.length+' event'+(rows.length===1?'':'s');
   if(filter.value==='approvals'){
    // Existing cards retain their canonical authorization, confirmation and receipts.
    for(const operation of data?.operations||[])list.append(manifestOperationCard(operation));
    appendTaskApprovals(list,{proposals:data?.proposals||[]});
    status.textContent='';
   }else for(const row of rows){
    const item=el('details','chat-activity-event');item.dataset.eventId=row.id;item.open=expanded.has(row.id);if(row.kind==='errors')item.classList.add('has-error');
    const summary=el('summary',''),title=el('span','chat-activity-event-title',row.title),excerpt=el('span','chat-activity-excerpt',row.text.replace(/\s+/g,' ').slice(0,180));
    summary.append(title,excerpt);if(row.at)summary.append(el('time','chat-activity-time',typeof fmtWhen==='function'?fmtWhen(row.at):row.at));item.append(summary);
    if(row.text)item.append(el('pre','chat-activity-text',row.text));if(row.output)item.append(el('pre','chat-activity-text',row.output));
    item.addEventListener('toggle',()=>{if(!item.isConnected)return;if(item.open)expanded.add(row.id);else expanded.delete(row.id);});list.append(item);
   }
   if(!list.childElementCount)list.append(emptyRow(data?'No matching activity.':'Recorded activity is not available yet.'));
   list.scrollTop=position;
  };
  filter.onchange=()=>{list.scrollTop=0;render();};window.addEventListener('chat-workbench-activity',render);render();
  return {element:pane,close:()=>{closed=true;window.removeEventListener('chat-workbench-activity',render);pane.remove();drop();},getView:()=>{for(const item of list.querySelectorAll('details.chat-activity-event')){if(item.open)expanded.add(item.dataset.eventId);else expanded.delete(item.dataset.eventId);}return {filter:filter.value,scrollTop:list.scrollTop,expanded:[...expanded]};},restoreView:async view=>{if(!host.isConnected||!host.clientHeight)return false;if([...filter.options].some(o=>o.value===view.filter))filter.value=view.filter;expanded.clear();for(const id of view.expanded||[])if(typeof id==='string')expanded.add(id);render();list.scrollTop=Math.max(0,Number(view.scrollTop)||0);return true;}};
 },{kind:'activity'});
}

function chatContextInputs(data){
 return (data?.turns||[]).filter(t=>t.who==='user').map((turn,index)=>{
  const receipt=turn.delivery||(turn.n!==undefined?(data.context?.deliveries||[]).find(d=>d.userTurn===turn.n):null);
  const context=receipt?.context||{};
  return {id:String(turn.id??turn.n??index),text:turn.text||'',recipient:context.recipient||turn.native||null,task:context.task||turn.submission?.task||'',artifacts:context.artifacts||turn.submission?.artifacts||[],files:turn.submission?.files||[],omitted:receipt?.historyOmitted||turn.submission?.historyOmitted||0};
 });
}
function chatOpenContext(){
 if(chatIsPortal())return;
 return chatEnsureWorkspace().tab('context','Context',(host,drop)=>{
  const key=chatAgent+'/'+chatOpenId,pane=el('section','chat-context-inspector'),body=el('div','chat-context-body');body.tabIndex=0;host.append(pane);pane.append(body);
  let closed=false,selected=null,instructionOpen=false;
  const render=()=>{
   if(closed||!host.isConnected||key!==chatAgent+'/'+chatOpenId)return;
   const source=chatWorkspaceSource();if(!source)return;
   const scroll=body.scrollTop;body.replaceChildren();
   const summary=el('dl','chat-context-summary');
   const field=(label,value)=>{if(value){summary.append(el('dt','',label),el('dd','',value));}};
   field('Agent',source.agent);field('Session model',source.model);field('Working folder',source.cwd);body.append(summary);
   const project=chatCurrentProject();
   if(project){const projectRow=el('div','chat-context-section'),edit=el('button','sprt-quiet','edit project instructions');edit.onclick=()=>chatEditProject(project);projectRow.append(el('h3','',chatWorkstreams.groups[project]||'Project'),el('p','chat-workspace-hint','Current project instructions apply to new chats. Recorded inputs below show what was sent here.'),edit);const notes=chatWorkstreams.contexts?.[project]?.instructions;if(notes){const current=el('details','');current.append(el('summary','','Current project instructions'),el('pre','chat-activity-text',notes));projectRow.append(current);}body.append(projectRow);}
   if(source.task){const task=el('button','sprt-quiet','open linked task');task.onclick=()=>openTodoPanel(source.task);body.append(task);}
   const data=chatWorkbenchActivity?.key===key?chatWorkbenchActivity:null,inputs=chatContextInputs(data);
   const section=el('section','chat-context-section');section.append(el('h3','','Recorded inputs'));body.append(section);
   if(!inputs.length){section.append(emptyRow('No recorded inputs available yet.'));body.scrollTop=scroll;return;}
   const selector=document.createElement('select');selector.className='pp-in';selector.setAttribute('aria-label','Recorded instruction');
   inputs.forEach((input,i)=>{const option=el('option','','Instruction '+(i+1)+' · '+input.text.replace(/\s+/g,' ').slice(0,70));option.value=input.id;selector.append(option);});
   const input=inputs.find(x=>x.id===selected)||inputs.at(-1);selected=input.id;selector.value=selected;selector.onchange=()=>{selected=selector.value;instructionOpen=false;render();};section.append(selector);
   const target=input.recipient;if(target)section.append(el('p','chat-context-target','Sent to '+(target.agent||source.agent)+(target.model?' · '+target.model:'')));
   if(input.omitted)section.append(el('p','chat-workspace-hint',input.omitted+' earlier turns omitted from this submission.'));
   if(input.task&&input.task!==source.task){const task=el('button','sprt-quiet','open task supplied with this instruction');task.onclick=()=>openTodoPanel(input.task);section.append(task);}
   const instruction=el('details','chat-context-instruction');instruction.open=instructionOpen;instruction.append(el('summary','','Instruction text'),el('pre','chat-activity-text',input.text));instruction.addEventListener('toggle',()=>{if(instruction.isConnected)instructionOpen=instruction.open;});section.append(instruction);
   for(const artifact of input.artifacts){const open=el('button','chat-context-reference','open referenced artifact');open.title='Revision '+artifact.revision;open.onclick=()=>chatOpenWorkingArtifact({id:artifact.id,revision:artifact.revision,task:input.task});section.append(open,el('span','chat-workspace-hint','Revision '+artifact.revision?.slice(0,12)));}
   for(const file of input.files){const open=el('button','chat-context-reference',file.name||'Attached file');open.onclick=()=>chatOpenAttachment(file,chatFileHref(file.hash));section.append(open);}
   const origin=data?.context?.origin;
   if(origin?.context){const parent=el('details','chat-context-instruction');parent.append(el('summary','','Parent context snapshot'),el('pre','chat-activity-text',origin.context));body.append(parent);}
   body.scrollTop=scroll;
  };
  window.addEventListener('chat-workbench-activity',render);render();
  return {element:pane,close:()=>{closed=true;window.removeEventListener('chat-workbench-activity',render);pane.remove();drop();},getView:()=>({selected,scrollTop:body.scrollTop,instructionOpen:body.querySelector('.chat-context-instruction')?.open||false}),restoreView:async view=>{if(!host.isConnected||!host.clientHeight)return false;selected=view.selected||null;instructionOpen=!!view.instructionOpen;render();body.scrollTop=Math.max(0,Number(view.scrollTop)||0);return true;}};
 },{kind:'context'});
}

function chatOpenFiles(){
 if(chatIsPortal())return;
 const source=chatWorkspaceSource();if(!source)return;
 return chatEnsureWorkspace().tab('files','Files',(host,drop)=>{
  const pane=el('section','chat-files-inspector'),toolbar=el('div','chat-files-toolbar'),search=document.createElement('input'),refresh=el('button','sprt-quiet','refresh'),status=el('p','chat-workspace-hint'),list=el('div','chat-files-list');
  search.type='search';search.placeholder='Filter files';search.setAttribute('aria-label','Filter files');list.tabIndex=0;status.setAttribute('role','status');toolbar.append(search,refresh);pane.append(toolbar,status,list);host.append(pane);
  let rows=[],closed=false,pending=null,ready=Promise.resolve();
  const render=()=>{
   const scroll=list.scrollTop;list.replaceChildren();const q=search.value.trim().toLowerCase();
   for(const {artifact:a,roles,attachment} of rows){
    if(q&&![a.title,a.ref,a.kind].join(' ').toLowerCase().includes(q))continue;
    const item=el('div','chat-file-row'),open=el('button','chat-file-open',a.title||a.ref||'Untitled file');open.disabled=!!a.unknown||!a.id;
    open.onclick=()=>attachment?chatOpenAttachment(attachment,"/api/chat/files/"+attachment.id):chatOpenWorkingArtifact({id:a.id,revision:a.head,task:source.task});
    const metadata=[...roles,a.kind,a.revisions?.length?'v'+a.revisions.length:'',a.provenance?.run?'Run '+a.provenance.run:''].filter(Boolean);
    item.append(open,el('div','chat-file-meta',metadata.join(' · ')));if(a.ref)item.append(el('div','chat-file-path',a.ref));list.append(item);
   }
   if(!list.childElementCount)list.append(emptyRow(rows.length?'No matching files.':'No registered files for this conversation yet.'));
   list.scrollTop=scroll;
  };
  const load=async()=>{
   pending?.abort();const controller=new AbortController();pending=controller;refresh.disabled=true;status.textContent='loading…';
   try{
    const query=new URLSearchParams({conversation_backend:source.backend==='terminal'?'terminal':'agent',conversation_agent:source.agent,conversation_id:source.id});
    const get=async url=>{const r=await fetch(url,{signal:controller.signal,cache:'no-store'});if(!r.ok)throw Error('Files could not be loaded. Refresh to retry.');return r.json();};
    const owner=(source.backend==='terminal'?'terminal:':'agent:')+source.agent+'/'+source.id;
    const [scope,task,uploads]=await Promise.all([get('/api/artifacts?'+query),source.task?get('/api/tasks/panel?id='+encodeURIComponent(source.task)):Promise.resolve(null),get('/api/chat/files?owner='+encodeURIComponent(owner))]);
    if(closed||pending!==controller)return;
    const found=new Map();const add=(a,role)=>{const key=a.id||a.ref;if(!key)return;const row=found.get(key)||{artifact:a,roles:[]};if(!row.roles.includes(role))row.roles.push(role);found.set(key,row);};
    for(const a of scope.artifacts||[])add(a,'Conversation output');for(const a of task?.artifacts?.outputs||[])add(a,'Task output');for(const a of task?.artifacts?.inputs||[])add(a,'Task input');
    rows=[...(uploads.files||[]).map(f=>({artifact:{id:f.id,title:f.name,kind:f.type},attachment:{...f,owned:true},roles:[f.sent?"Attached context":"Draft attachment"]})),...found.values()];status.textContent=rows.length+' file'+(rows.length===1?'':'s');render();
   }catch(e){if(!closed&&pending===controller&&e.name!=='AbortError')status.textContent=e.message;}
   finally{if(!closed&&pending===controller)refresh.disabled=false;}
  };
  search.oninput=render;refresh.onclick=()=>{ready=load();};ready=load();
  return {element:pane,close:()=>{closed=true;pending?.abort();pane.remove();drop();},getView:()=>({query:search.value,scrollTop:list.scrollTop}),restoreView:async view=>{await ready;if(!host.isConnected||!host.clientHeight)return false;search.value=typeof view.query==='string'?view.query:'';render();list.scrollTop=Math.max(0,Number(view.scrollTop)||0);return true;}};
 },{kind:'files'});
}
