// The right workspace owns view state only. Each side conversation uses the
// existing chat application in an isolated document, including its outbox,
// approvals, attachments and agent picker; it never shares main-chat globals.
const chatEmbedded = window.parent !== window && new URLSearchParams(location.search).get('chatPane') === '1';
if(chatEmbedded)document.documentElement.classList.add('chat-embedded');
let chatWorkspaceTabs = null;
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
 button.title='Plans, files and side chats';
 button.onclick=()=>{if(chatWorkspaceTabs)chatWorkspaceTabs.show(chatWorkspaceTabs.pane.hidden);else chatEnsureWorkspace();};head.append(button);
}
function chatEnsureWorkspace(){
 if(chatWorkspaceTabs)return chatWorkspaceTabs;
 const shell=document.querySelector('.chat-shell'),pane=el('aside','artifact-workspace chat-tab-workspace');
 pane.setAttribute('aria-label','Chat workspace');pane.hidden=true;
 const bar=el('div','chat-workspace-bar'),tabs=el('div','chat-workspace-tabs'),add=el('button','sprt-quiet','+'),hide=el('button','sprt-quiet','×'),body=el('div','chat-workspace-body');
 tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Workspace tabs');
 add.setAttribute('aria-label','Add workspace tab');hide.setAttribute('aria-label','Hide workspace');hide.title='Hide workspace; keep tabs and drafts';
 bar.append(tabs,add,hide);pane.append(bar,body);shell.append(pane);
 const entries=new Map();let active=null,disposed=false,chooserHost=null;
 const clearChooser=()=>{chooserHost?.remove();chooserHost=null;pane.classList.remove('choosing');};
 const w={pane,body,entries,
  show(open=true){pane.hidden=!open;shell.classList.toggle('has-artifact',open);shell._refreshPaneWidths?.();document.querySelectorAll('.chat-workspace-toggle').forEach(b=>b.setAttribute('aria-expanded',String(open)));if(!open)document.querySelector('#chatComposer textarea')?.focus();},
  select(key){clearChooser();active=key;for(const [id,t] of entries){t.host.hidden=id!==key;t.button.setAttribute('aria-selected',String(id===key));t.button.tabIndex=id===key?0:-1;}w.show();},
  drop(key){const t=entries.get(key);if(!t)return;t.host.remove();t.row.remove();entries.delete(key);if(!disposed&&active===key){const next=Array.from(entries.keys()).at(-1);if(next)w.select(next);else w.chooser();}},
  tab(key,title,build){if(entries.has(key)){w.select(key);return entries.get(key);}
   const row=el('div','chat-workspace-tab'),button=el('button','sprt-quiet',title),close=el('button','sprt-quiet','×'),host=el('div','chat-workspace-tabbody');
   button.setAttribute('role','tab');button.title=title;host.setAttribute('role','tabpanel');
   const uid='workspace-'+crypto.randomUUID();host.id=uid;button.id=uid+'-tab';button.setAttribute('aria-controls',uid);host.setAttribute('aria-labelledby',button.id);
   close.setAttribute('aria-label','Close '+title+' tab');row.append(button,close);tabs.append(row);body.append(host);
   const t={row,button,host,api:null};entries.set(key,t);button.onclick=()=>w.select(key);
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
     button.textContent=name;button.setAttribute('aria-label',name);button.title=name+(draft?' · Unfinished edit':'');
     button.setAttribute('aria-description',draft?'Unfinished edit':'');row.classList.toggle('has-draft',draft);
     close.setAttribute('aria-label','Close '+name+' tab');close.title=draft?'Close tab; your draft remains saved':'Close tab';
    };
    const observer=new MutationObserver(update);observer.observe(heading,{childList:true,characterData:true,subtree:true});observer.observe(t.api.element,{attributes:true,attributeFilter:['data-draft']});
    const dispose=t.api.close;t.api.close=()=>{observer.disconnect();dispose?.();};update();
   }
   return t;
  },
  chooser(){clearChooser();chooserHost=el('div','chat-workspace-picker');pane.classList.add('choosing');if(entries.size){chooserHost.classList.add('chat-workspace-popover');pane.append(chooserHost);}else body.append(chooserHost);chatWorkspaceChooser(chooserHost);w.show();},
  close(){if(disposed)return;disposed=true;for(const t of Array.from(entries.values()))t.api?.close?.();entries.clear();pane.remove();shell.classList.remove('has-artifact');chatWorkspaceTabs=null;chatWorkspace=null;shell._refreshPaneWidths?.();}
 };
 chatWorkspaceTabs=w;chatWorkspace=w;add.onclick=()=>{if(chooserHost&&entries.size)clearChooser();else w.chooser();};hide.onclick=()=>w.show(false);
 pane.addEventListener('keydown',e=>{if(e.key==='Escape'){if(chooserHost&&entries.size){clearChooser();add.focus();}else w.show(false);}});
 pane.addEventListener('pointerdown',e=>{if(chooserHost&&entries.size&&!chooserHost.contains(e.target)&&!add.contains(e.target))clearChooser();});
 chatInstallPaneResize(shell);w.chooser();return w;
}
function chatWorkspaceChooser(host){
 const source=chatWorkspaceSource(),chooser=el('div','chat-workspace-chooser');
 const action=(name,description,icon,fn)=>{const b=el('button','chat-workspace-option');b.title=description;b.append(chatWorkspaceIcon(icon),el('span','',name));b.onclick=fn;chooser.append(b);};
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
 host.append(chooser);
}
function chatWorkspaceSideSetup(source){
 const key='side-setup-'+crypto.randomUUID(),w=chatEnsureWorkspace();
 w.tab(key,'New side chat',(host)=>{
  const wrap=el('div','chat-side-setup'),heading=el('h3','','Side chat'),hint=el('p','','Starts with this conversation’s recent context.');
  const pick=document.createElement('select');pick.setAttribute('aria-label','Side chat agent');
  const options=chatRoster.filter(a=>a.enabled&&a.durableSend).map(a=>({value:a.name,label:a.label,model:a.model||''}));
  if(chatTermEnabled)Object.entries(chatTermKinds).forEach(([value,label])=>options.push({value:'terminal:'+value,label,model:''}));
  options.forEach(a=>{const o=document.createElement('option');o.value=a.value;o.textContent=a.label;pick.append(o);});
  const recipient=chatRecipients.get(source.agent+'/'+source.id),initial=recipient?(recipient.backend==='terminal'?'terminal:':'')+recipient.agent:(source.backend==='terminal'?'terminal:':'')+source.agent;
  pick.value=initial;
  const model=document.createElement('select');model.setAttribute('aria-label','Side chat model');model.value=recipient?.model||source.model;model.placeholder='Configured default';
  const cwd=document.createElement('input');cwd.setAttribute('aria-label','Side chat working folder');cwd.value=source.cwd||'';cwd.placeholder='Default working folder';
  const field=(label,input)=>{const l=el('label','',label);l.append(input);return l;};
  const folder=field('Working folder',cwd),advanced=el('details','chat-side-options');advanced.append(el('summary','','Working folder'),folder);
  const sync=()=>{folder.hidden=!pick.value.startsWith('terminal:');chatPopulateModelSelect(model,pick.value.startsWith('terminal:')?pick.value.slice(9):pick.value,pick.value===initial?(recipient?.model||source.model):options.find(a=>a.value===pick.value)?.model||'');};sync();pick.onchange=()=>{model.value=pick.value===initial?(recipient?.model||source.model):options.find(a=>a.value===pick.value)?.model||'';sync();};
  const status=el('p','chat-workspace-hint');status.setAttribute('role','status');
  const start=el('button','chat-side-start','Open side chat');start.disabled=!pick.value;
  // Recovery is scoped to this setup tab. Keep the accepted request identity on
  // uncertain responses; changing settings creates a distinct intent.
  let remembered=null;
  start.onclick=async()=>{
   const coding=pick.value.startsWith('terminal:'),payload={agent:coding?pick.value.slice(9):pick.value,model:model.value.trim(),title:('Side chat · '+source.title).slice(0,240),task:source.task||'',mode:'side',...(coding?{backend:'terminal',cwd:cwd.value.trim()}:{})};
   const selected=chatArtifactSelections.get('chat:'+source.agent+'/'+source.id);if(selected)payload.artifacts=[{id:selected.id,revision:selected.revision}];
   const signature=JSON.stringify(payload);if(remembered?.signature!==signature)remembered={signature,requestId:crypto.randomUUID()};
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
    const strip=el('div','chat-side-context'),info=el('details','');info.append(el('summary','','Context from '+source.title),el('p','','Snapshot of recent complete turns at creation. Older history may be omitted; tool traces and attachment contents are excluded. Selected artifact versions are included separately. This is a saved private conversation; closing its tab does not delete it.'));
    const link=el('a','sprt-quiet','Open full chat ↗');link.href=result.conversation.route;link.target='_blank';link.rel='noopener';strip.append(info,link);
    const frame=document.createElement('iframe');frame.title='Side chat · '+source.title;frame.className='chat-side-frame';frame.src=location.pathname+'?chatPane=1'+result.conversation.route;
    host.append(strip,frame);w.entries.get(key).button.textContent='Side chat';
   }catch(e){status.textContent=e.message||'Could not create side chat. Retry safely.';}finally{start.disabled=false;}
  };
  wrap.append(heading,hint,field('Agent',pick),field('Model',model),advanced,status,start);host.append(wrap);return {};
 });
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
 if(send){send.setAttribute('aria-label','Send message');send.title=window.matchMedia('(max-width: 860px)').matches?'Send message · Enter adds a new line':'Send message · Enter (Shift+Enter for a new line)';}
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
