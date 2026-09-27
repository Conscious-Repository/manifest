// Workspace controls use the same independent documents as existing side chats.
// Two kinds of slash command share one menu (research brief, 2026-09-27):
//  · Manifest's own (surface) commands behave the same on every agent —
//    /model, /effort, /permissions open the one picker (49-chat-models.js),
//    /goal keeps a native agent's standing objective, /new and /tile move you.
//  · Native commands belong to a coding CLI and are submitted verbatim; their
//    interactive UI stays native. Lists track Codex 0.154 and Claude Code 2.1.
const chatNativeCommands={
 codex:[['goal','Set an objective, or view / pause / resume / clear it'],['plan','Plan before implementing'],['review','Review changes'],['diff','Show the working-tree diff'],['compact','Compact conversation context'],['status','Inspect the native session'],['mention','Attach a file to the conversation'],['init','Create an AGENTS.md for this repo'],['new','Start a new native conversation'],['resume','Resume an earlier native conversation'],['fork','Fork this conversation'],['side','Ask a side question'],['approve','Retry an action auto-review denied'],['mcp','Inspect connected tools'],['skills','Choose installed skills'],['memories','Inspect memories'],['copy','Copy the last output'],['title','Rename the native session'],['ps','List background processes'],['stop','Stop background processes'],['fast','Toggle fast mode'],['personality','Choose a personality'],['statusline','Configure the status line'],['hooks','Inspect hooks'],['clear','Clear the screen']],
 claude:[['goal','Set an objective Claude keeps working toward'],['plan','Enter plan mode'],['context','Inspect context usage'],['compact','Compact conversation context'],['usage','Inspect session usage and cost'],['status','Inspect session configuration'],['code-review','Review the current changes'],['security-review','Review changes for security'],['diff','Show uncommitted changes'],['init','Create a CLAUDE.md for this repo'],['memory','Open instruction files'],['agents','Manage subagents'],['tasks','List background tasks'],['btw','Ask a side question'],['rewind','Rewind code and conversation'],['fork','Fork this conversation'],['resume','Resume an earlier conversation'],['loop','Run a prompt on an interval'],['mcp','Manage connected tools'],['hooks','Manage hooks'],['skills','Browse installed skills'],['add-dir','Add a working directory'],['export','Export the conversation'],['config','Open settings'],['doctor','Check the installation'],['fast','Toggle fast mode'],['simplify','Simplify recent changes'],['verify','Verify recent changes'],['clear','Clear the conversation']],
};
// chatSurfaceCommands — Manifest's commands for the conversation in view.
function chatSurfaceCommands(){
 const ctx=typeof chatModelContext==='function'?chatModelContext():null,out=[];
 if(ctx){
  out.push(['model','Choose model · Manifest picker']);
  out.push(['effort','Set reasoning effort']);
  if(ctx.kind!=='hermes')out.push(['permissions','Choose permissions / access']);
  if(ctx.kind==='hermes'&&chatOpenId)out.push(['goal','Standing objective for this chat · pause · resume · clear']);
 }
 if(!chatEmbedded&&chatAgent)out.push(['new','New chat with this agent']);
 if(!chatEmbedded&&chatOpenId)out.push(['tile','Open this chat in tiles']);
 return out;
}
function chatInstallCommands(host,input){
 if(!chatAgent||chatIsPortal())return;
 const box=el('div','chat-command-picker');box.hidden=true;box.id='chatCommandPicker';box.setAttribute('role','listbox');box.setAttribute('aria-label','Commands');host.append(box);
 let rows=[],index=0;
 const close=()=>{box.hidden=true;input.removeAttribute('aria-activedescendant');input.setAttribute('aria-expanded','false');};
 const choose=()=>{const row=rows[index];if(!row)return;input.value='/'+row[0]+' ';close();input.dispatchEvent(new Event('input',{bubbles:true}));input.focus();};
 const paint=()=>{box.replaceChildren();let group='';rows.forEach((row,i)=>{if(row[2]!==group){group=row[2];box.append(el('div','chat-command-group micro-label',group));}const b=el('button','chat-command-option');b.type='button';b.id='chatCommand-'+i;b.setAttribute('role','option');b.setAttribute('aria-selected',String(i===index));b.append(el('span','', '/'+row[0]),el('span','chat-command-description',row[1]));b.onmousedown=e=>e.preventDefault();b.onclick=()=>{index=i;choose();};box.append(b);});box.append(el('div','chat-command-keys','↑↓ choose · Enter or Tab insert · Esc close'));if(rows[index])input.setAttribute('aria-activedescendant','chatCommand-'+index);};
 input.addEventListener('input',()=>{
  if(chatRecipients.get(chatDraftKey)?.agent&&chatRecipients.get(chatDraftKey).agent!==chatAgent){close();return;}
  const match=input.value.match(/^\/([\w:-]*)$/);if(!match){close();return;}
  const surface=chatSurfaceCommands(),taken=new Set(surface.map(r=>r[0]));
  const native=chatIsTerm()?(chatNativeCommands[chatAgent]||[]).filter(r=>!taken.has(r[0])):[];
  rows=[...surface.map(r=>[r[0],r[1],'Manifest']),...native.map(r=>[r[0],r[1],chatAgentLabel(chatAgent)])].filter(r=>r[0].startsWith(match[1]));
  if(!rows.length){close();return;}
  index=0;box.hidden=false;input.setAttribute('aria-controls',box.id);input.setAttribute('aria-expanded','true');paint();
 });
 input.addEventListener('keydown',e=>{if(box.hidden||e.isComposing)return;
  // a fully typed command runs on Enter, as in Codex; a partial one completes
  if(e.key==='Enter'&&!e.shiftKey&&rows[index]&&input.value.trim()==='/'+rows[index][0]){close();return;}
  if(e.key==='Escape'){e.preventDefault();e.stopImmediatePropagation();close();}else if(['ArrowDown','ArrowUp','Enter','Tab'].includes(e.key)){e.preventDefault();e.stopImmediatePropagation();if(e.key==='Enter'||e.key==='Tab')choose();else{index=(index+(e.key==='ArrowDown'?1:-1)+rows.length)%rows.length;paint();}}},true);
 input.addEventListener('blur',close);
}
function chatWorkspaceControls(host){
 if(chatEmbedded)return;
 const shell=document.querySelector('.chat-shell');
 const toggle=el('button','sprt-quiet chat-sidebar-toggle chat-ibtn');toggle.setAttribute('aria-label','Chats');if(typeof chatWorkspaceIcon==='function')toggle.append(chatWorkspaceIcon('sidebar'));else toggle.textContent='Chats';toggle.title='Toggle conversation sidebar · Ctrl+Alt+B';toggle.setAttribute('aria-controls','chatRail');
 const apply=()=>{const hidden=chatRecall('manifest.chatSidebarHidden')==='1';shell.classList.toggle('chat-list-hidden',hidden);toggle.setAttribute('aria-expanded',String(!hidden));shell._refreshPaneWidths?.();};
 toggle.onclick=()=>{try{localStorage.setItem('manifest.chatSidebarHidden',shell.classList.contains('chat-list-hidden')?'0':'1');}catch(e){}apply();};host.prepend(toggle);apply();
 const layout=el('select','chat-layout-select');layout.setAttribute('aria-label','Tool panes');layout.title='Tool panes beside the conversation (files, review, side chats) · Ctrl+Alt+I';
 // named for what they are (owner 2026-09-27): panes of tools beside the one
 // conversation; several whole conversations side by side are Tiles
 for(const [v,label]of [['focus','No tool panes'],['split','Tool panes · 1'],['workbench','Tool panes · 2'],['four','Tool panes · 3']]){const o=el('option','',label);o.value=v;layout.append(o);}
 layout.value=chatRecall('manifest.chatLayout')||'focus';chatLayoutTruth();
 layout.onchange=()=>{try{localStorage.setItem('manifest.chatLayout',layout.value);}catch(e){}if(layout.value==='focus'){chatWorkspaceTabs?.show(false);}else{chatEnsureWorkspace().show(true);}chatApplyWorkspaceLayout();};host.append(layout);

}
// chatLayoutTruth — at ≤1100px the workspace shows one pane whatever layout
// is chosen (the responsive fallback). The select says so on the chosen
// option instead of claiming panes it will not show (audit 2026-09-25).
function chatLayoutTruth(){
 const select=document.querySelector('.chat-layout-select');if(!select)return;
 const narrow=window.matchMedia('(max-width: 1100px)').matches;
 // the chosen option's pane count becomes "1 pane here" (the longer suffix
 // clipped in the 180px select at 861, Phase C 2026-09-26)
 for(const o of select.options){o.dataset.label??=o.textContent;o.textContent=narrow&&o.value!=='focus'&&o.selected?o.dataset.label.split(' · ')[0]+' · 1 pane here':o.dataset.label;}
 select.title=narrow&&select.value!=='focus'?'This width shows one tool pane; widen the window for more.':'Tool panes beside the conversation (files, review, side chats) · Ctrl+Alt+I';
}
function chatApplyWorkspaceLayout(){
 chatLayoutTruth();
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
  const picker=el('div','chat-conversation-picker'),search=el('input','chat-conversation-search'),list=el('div','chat-conversation-list');
  search.type='search';search.placeholder='Find a conversation…';search.setAttribute('aria-label','Find a conversation');
  const entries=chatInboxEntries().filter(entry=>!entry.taskThread);
  const paint=()=>{
   list.replaceChildren();const query=search.value.trim().toLowerCase();
   for(const entry of entries){
    const se=entry.session,title=se.title||se.name||'Conversation';
    const agent=entry.terminal?se.kind:entry.agent||'Spirits';
    const route=entry.terminal?'#/chat/a/'+encodeURIComponent(se.kind)+'/'+encodeURIComponent(se.id):entry.agent?'#/chat/a/'+encodeURIComponent(entry.agent)+'/'+encodeURIComponent(se.id):'#/chat/'+encodeURIComponent(se.id);
    if(route===location.hash||!(title+' '+agent).toLowerCase().includes(query))continue;
    const b=el('button','chat-conversation-choice');b.type='button';b.append(el('span','chat-conversation-name',title),el('span','chat-conversation-agent',agent));
    b.onclick=()=>{const spec={kind:'side',route,title,existing:true};w.tab('conversation:'+route,title,h=>chatMountSideFrame(h,spec),spec);w.drop('choose-existing');};list.append(b);
   }
   if(!list.children.length)list.append(el('p','chat-workspace-hint','No matching conversations.'));
  };
  search.addEventListener('input',paint);picker.append(search,list);host.append(picker);paint();queueMicrotask(()=>search.focus());
 });
}
