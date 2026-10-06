// On-screen prompts — the CLI's own chooser (a Claude Code permission dialog,
// an AskUserQuestion tab, a Codex approval, a folder-trust check) read off the
// pane by terminal_prompt.go and answered here with the keys Terminal would
// take. The card sits above the composer like Claude Code's own dialog:
// numbered choices (press the number), a typed answer where the chooser takes
// one, and an optional "what to do instead" note after a refusal.
//
// The server refuses an answer drawn from a screen that has since changed
// (revision) and reads the cursor back before pressing Enter, so the worst a
// stale card can do is say "the prompt changed".
let chatPromptBusy=false;

function chatPromptPaint(o) {
  let card=document.getElementById('chatPrompt');
  const p=o&&o.live&&!o.sharedConversation&&o.se?.backend==='herdr'?o.prompt:null;
  if(!p){card?.remove();return;}
  const composer=document.getElementById('chatComposer');
  if(!composer)return;
  // same chooser, same cursor: keep the node (a half-typed answer, focus)
  if(card&&card.dataset.session===o.id&&card.dataset.revision===p.revision&&card.dataset.selected===String(p.selected))return;
  const hadFocus=card?.contains(document.activeElement);
  const next=chatPromptCard(o,p);
  if(card)card.replaceWith(next);else composer.before(next);
  const active=document.activeElement;
  // take focus only from nothing in particular — never from the composer or a field
  if(hadFocus||!active||active===document.body)next.querySelector('.chat-prompt-option.is-cursor,.chat-prompt-option')?.focus({preventScroll:true});
}

function chatPromptAgentName(o){return o.se?.kind==='codex'?'Codex':o.se?.kind==='claude'?'Claude':'the agent';}

function chatPromptCard(o,p) {
  const card=el('section','chat-prompt');card.id='chatPrompt';
  card.dataset.session=o.id;card.dataset.revision=p.revision;card.dataset.selected=String(p.selected);
  card.setAttribute('role','group');card.setAttribute('aria-label','Agent needs input');
  const head=el('div','chat-prompt-head');
  head.append(el('span','chat-prompt-badge','Needs input'));
  if(p.header)head.append(el('span','chat-prompt-header',p.header));
  if(p.tabs?.length){
    const tabs=el('span','chat-prompt-tabs');
    for(const t of p.tabs){const tab=el('span','chat-prompt-tab'+(t.done?' is-done':''),(t.done?'✓ ':'')+t.label);tabs.append(tab);}
    head.append(tabs);
  }
  card.append(head,el('div','chat-prompt-title',p.title));
  if(p.body?.length){
    const body=el('div','chat-prompt-body');
    for(const line of p.body)body.append(line.code?el('pre','chat-prompt-code',line.text):el('p','chat-prompt-text',line.text));
    card.append(body);
  }
  const list=el('div','chat-prompt-options');list.setAttribute('role','list');
  const status=el('div','chat-prompt-status');status.setAttribute('role','status');
  const extra=el('div','chat-prompt-extra');extra.hidden=true;
  const buttons=[];
  const checks=new Map();
  const send=async(answer,note='')=>{
    if(chatPromptBusy)return;
    chatPromptBusy=true;card.classList.add('is-busy');buttons.forEach(b=>b.disabled=true);status.textContent='Answering…';
    try{
      const r=await postJSONOk(chatTermBase(o.id)+'/prompt',{revision:p.revision,...answer});
      status.textContent='';
      o.prompt=r.prompt||null;
      if(chatTermOpen===o)chatPromptPaint(o);
      if(note.trim()){
        // the refusal returns the CLI to its prompt; the note is an ordinary
        // message (held by the outbox if the agent is not ready yet)
        setTimeout(()=>{if(chatTermOpen===o)chatTermSend(note.trim());},700);
      }
    }catch(e){
      status.textContent=(e.message||'Not answered')+'';
      card.classList.add('is-error');
    }finally{
      chatPromptBusy=false;card.classList.remove('is-busy');buttons.forEach(b=>b.disabled=false);
      if(chatTermOpen===o){chatTermScreenFetch();chatTermRequestFinalTail(o);}
    }
  };
  // an inline field under a row: a typed answer, or the note after a refusal
  const openField=(option,kind)=>{
    extra.replaceChildren();extra.hidden=false;
    const field=el(kind==='text'?'input':'textarea','chat-prompt-input');
    if(kind==='text'){field.type='text';field.maxLength=4000;field.placeholder='Type your answer';}
    else{field.rows=2;field.placeholder='Tell '+chatPromptAgentName(o)+' what to do instead (optional)';}
    field.setAttribute('aria-label',field.placeholder);
    const go=el('button','pill chat-prompt-go',kind==='text'?'Send answer':option.label.length<=24?option.label:'Refuse');go.type='button';
    const back=el('button','sprt-quiet','Back');back.type='button';
    const submit=()=>{
      if(kind==='text'){if(!field.value.trim()){field.focus();return;}send({option:option.index,text:field.value.trim(),checked:chatPromptChecked(checks)});}
      else send({option:option.index},field.value);
    };
    go.onclick=submit;back.onclick=()=>{extra.hidden=true;extra.replaceChildren();list.querySelector('[data-index="'+option.index+'"]')?.focus();};
    field.onkeydown=e=>{
      if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();submit();}
      else if(e.key==='Escape'){e.preventDefault();e.stopPropagation();back.onclick();}
    };
    const row=el('div','chat-prompt-extra-actions');row.append(go,back);
    extra.append(field,row);buttons.push(go);
    field.focus();
  };
  for(const option of p.options){
    if(p.multi&&option.text)continue; // typing into a multi-select row: Terminal
    if(p.multi&&option.checked!=null){
      // a tickable row: toggled locally, sent with Submit
      const row=el('label','chat-prompt-option chat-prompt-check'+(option.index===p.selected?' is-cursor':''));
      row.dataset.index=option.index;
      const box=el('input','');box.type='checkbox';box.checked=!!option.checked;checks.set(option.index,box);
      const text=el('span','chat-prompt-label');text.append(el('span','',option.label));if(option.detail)text.append(el('span','chat-prompt-detail',option.detail));
      row.append(box,el('kbd','chat-prompt-key',option.number||''),text);list.append(row);continue;
    }
    const b=el('button','chat-prompt-option'+(option.index===p.selected?' is-cursor':'')+(option.followup?' is-refuse':'')+(option.submit?' is-submit':''));
    b.type='button';b.dataset.index=option.index;b.setAttribute('role','listitem');
    if(option.number)b.dataset.number=option.number;
    const label=el('span','chat-prompt-label');
    label.append(el('span','',option.text?'Type an answer…':option.label));
    if(option.detail)label.append(el('span','chat-prompt-detail',option.detail));
    b.append(el('kbd','chat-prompt-key',option.number||(option.submit?'↵':'·')),label);
    b.onclick=()=>{
      if(option.text)openField(option,'text');
      else if(option.followup)openField(option,'note');
      else send({option:option.index,checked:chatPromptChecked(checks)});
    };
    buttons.push(b);list.append(b);
  }
  if(p.multi&&!p.options.some(x=>x.submit)){
    status.textContent='This chooser has no Submit row here — open Terminal to finish it.';
  }
  card.append(list,extra);
  const foot=el('div','chat-prompt-foot');
  const nums=p.options.filter(x=>x.number).map(x=>x.number);
  foot.append(el('span','chat-prompt-hint',(nums.length?'Press '+nums[0]+'–'+nums[nums.length-1]+' to choose · ':'')+'Esc to cancel'));
  const cancel=el('button','sprt-quiet','Cancel');cancel.type='button';cancel.onclick=()=>send({dismiss:true});
  const terminal=el('button','sprt-quiet','Open terminal');terminal.type='button';terminal.onclick=()=>chatOpenTerminalPane(o.se);
  buttons.push(cancel);
  foot.append(cancel,terminal);
  card.append(foot,status);
  card.onkeydown=e=>{
    if(e.target.closest('input[type=text],textarea')||e.metaKey||e.ctrlKey||e.altKey||chatPromptBusy)return;
    const rows=[...list.querySelectorAll('.chat-prompt-option')];
    const at=rows.indexOf(e.target.closest('.chat-prompt-option'));
    if(/^[1-9]$/.test(e.key)){
      const target=list.querySelector('[data-index][data-number="'+e.key+'"]')||[...list.children].find(n=>n.querySelector('.chat-prompt-key')?.textContent===e.key);
      if(target){e.preventDefault();target.click();}
    }else if(e.key==='ArrowDown'||e.key==='ArrowUp'){
      e.preventDefault();const n=rows.length;if(!n)return;
      rows[((at<0?0:at+(e.key==='ArrowDown'?1:-1))+n)%n].focus();
    }else if(e.key==='Escape'){e.preventDefault();e.stopPropagation();send({dismiss:true});}
  };
  return card;
}

function chatPromptChecked(checks){
  if(!checks.size)return undefined;
  return [...checks].filter(([,box])=>box.checked).map(([index])=>index);
}
