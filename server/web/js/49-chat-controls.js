// Workspace controls use the same independent documents as existing side chats.
// Native commands are submitted verbatim; their interactive UI stays native.
const chatNativeCommands={
 codex:[['goal','Set an objective, or view / pause / resume / clear it'],['plan','Plan before implementing'],['model','Choose model and reasoning effort'],['status','Inspect the native session'],['review','Review changes'],['compact','Compact conversation context'],['permissions','Open native permission controls'],['mcp','Inspect connected tools'],['skills','Choose installed skills'],['diff','Inspect the working diff'],['ps','Inspect background terminals'],['help','Native help']],
 claude:[['help','Browse native commands and installed skills'],['model','Choose a Claude model'],['plan','Enter plan mode'],['compact','Compact conversation context'],['context','Inspect context usage'],['cost','Inspect session usage'],['status','Inspect session configuration'],['permissions','Open native permission controls'],['mcp','Manage connected tools'],['memory','Open instruction files'],['tasks','Inspect background tasks'],['doctor','Inspect installation health']]
};
function chatInstallCommands(host,input){
 if(!chatIsTerm())return;
 const box=el('div','chat-command-picker');box.hidden=true;box.id='chatCommandPicker';box.setAttribute('role','listbox');box.setAttribute('aria-label','Native commands');host.append(box);
 let rows=[],index=0;
 const close=()=>{box.hidden=true;input.removeAttribute('aria-activedescendant');input.setAttribute('aria-expanded','false');};
 const choose=()=>{const row=rows[index];if(!row)return;input.value='/'+row[0]+' ';close();input.dispatchEvent(new Event('input',{bubbles:true}));input.focus();};
 const paint=()=>{box.replaceChildren();rows.forEach((row,i)=>{const b=el('button','chat-command-option');b.type='button';b.id='chatCommand-'+i;b.setAttribute('role','option');b.setAttribute('aria-selected',String(i===index));b.append(el('span','', '/'+row[0]),el('span','chat-command-description',row[1]));b.onmousedown=e=>e.preventDefault();b.onclick=()=>{index=i;choose();};box.append(b);});box.append(el('p','chat-command-hint','Native CLI commands · interactive results open in Terminal. Installed custom commands can also be typed.'));input.setAttribute('aria-activedescendant','chatCommand-'+index);box.children[index]?.scrollIntoView({block:'nearest'});};
 input.addEventListener('input',()=>{if(chatRecipients.get(chatDraftKey)){close();return;}const match=input.value.match(/^\/([\w:-]*)$/);if(!match){close();return;}rows=(chatNativeCommands[chatAgent]||[]).filter(r=>r[0].startsWith(match[1]));if(!rows.length){close();return;}index=0;box.hidden=false;input.setAttribute('aria-controls',box.id);input.setAttribute('aria-expanded','true');paint();});
 input.addEventListener('keydown',e=>{if(box.hidden||e.isComposing)return;if(e.key==='Escape'){e.preventDefault();e.stopImmediatePropagation();close();}else if(['ArrowDown','ArrowUp','Enter','Tab'].includes(e.key)){e.preventDefault();e.stopImmediatePropagation();if(e.key==='Enter'||e.key==='Tab')choose();else{index=(index+(e.key==='ArrowDown'?1:-1)+rows.length)%rows.length;paint();}}},true);
 input.addEventListener('blur',close);
}
function chatWorkspaceControls(host){
 if(chatEmbedded)return;
 const shell=document.querySelector('.chat-shell');
 const toggle=el('button','sprt-quiet chat-sidebar-toggle','Chats');toggle.title='Toggle conversation sidebar · Ctrl+Alt+B';toggle.setAttribute('aria-controls','chatRail');
 const apply=()=>{const hidden=chatRecall('manifest.chatSidebarHidden')==='1';shell.classList.toggle('chat-list-hidden',hidden);toggle.setAttribute('aria-expanded',String(!hidden));shell._refreshPaneWidths?.();};
 toggle.onclick=()=>{try{localStorage.setItem('manifest.chatSidebarHidden',shell.classList.contains('chat-list-hidden')?'0':'1');}catch(e){}apply();};host.prepend(toggle);apply();
 const layout=el('select','chat-layout-select');layout.setAttribute('aria-label','Workspace layout');
 for(const [v,label]of [['focus','Focus'],['split','Split · 2 panes'],['workbench','Workbench · 3 panes'],['four','Four panes']]){const o=el('option','',label);o.value=v;layout.append(o);}
 layout.value=chatRecall('manifest.chatLayout')||'focus';
 layout.onchange=()=>{try{localStorage.setItem('manifest.chatLayout',layout.value);}catch(e){}if(layout.value==='focus'){chatWorkspaceTabs?.show(false);}else{chatEnsureWorkspace().show(true);}chatApplyWorkspaceLayout();};host.append(layout);

}
function chatApplyWorkspaceLayout(){
 const w=chatWorkspaceTabs;if(!w)return;
 const mode=document.querySelector('.chat-layout-select')?.value||chatRecall('manifest.chatLayout')||'split';
 const count=mode==='four'?3:mode==='workbench'?2:1;
 w.pane.dataset.layout=count>1?mode:'split';
 const active=[...w.entries.values()].find(t=>t.button.getAttribute('aria-selected')==='true');
 const visible=[active,...[...w.entries.values()].filter(t=>t!==active).reverse()].filter(Boolean).slice(0,count);
 const narrow=window.matchMedia('(max-width: 1100px)').matches;
 for(const t of w.entries.values())t.host.hidden=narrow?t!==active:!visible.includes(t);
 w.body.classList.toggle('chat-workspace-tiled',count>1&&!narrow&&visible.length>1);
 w.body.dataset.panes=String(visible.length);
 for(const t of w.entries.values()){t.host.dataset.paneTitle=t.button.getAttribute('aria-label')||'Workspace';}
}
window.addEventListener('resize',chatApplyWorkspaceLayout);
document.addEventListener('keydown',e=>{if(e.ctrlKey&&e.altKey&&e.key.toLowerCase()==='b'&&!chatEmbedded){const toggle=document.querySelector('.chat-sidebar-toggle');if(toggle){e.preventDefault();toggle.click();}}});
function chatWorkspaceExistingChat(){
 const w=chatEnsureWorkspace();
 w.tab('choose-existing','Open conversation',host=>{
  const label=el('p','chat-workspace-hint','Open an existing conversation beside this one.');host.append(label);
  for(const entry of chatInboxEntries()){
   const se=entry.session;if(entry.taskThread)continue;
   const route=entry.terminal?'#/chat/a/'+encodeURIComponent(se.kind)+'/'+encodeURIComponent(se.id):entry.agent?'#/chat/a/'+encodeURIComponent(entry.agent)+'/'+encodeURIComponent(se.id):'#/chat/'+encodeURIComponent(se.id);
   if(route===location.hash)continue;
   const b=el('button','chat-workspace-option',se.title||se.name||'Conversation');b.onclick=()=>{const spec={kind:'side',route,title:se.title||se.name||'Conversation',existing:true};w.tab('conversation:'+route,spec.title,h=>chatMountSideFrame(h,spec),spec);w.drop('choose-existing');};host.append(b);
  }
 });
}
