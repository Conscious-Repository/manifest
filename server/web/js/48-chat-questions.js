// Native async question cards use the existing terminal input receipt boundary.
// Keep keyed DOM nodes: a transcript poll must never replace a focused answer.
const chatQuestionDrafts = new Map();
// One existing private draft snapshot per native question, separate from the
// composer. Frozen request identity is saved before any runtime submission.
class ChatQuestionDraft extends ChatDraftState {
 constructor(key,session,question,revision){super(key,null);this.questionRevision=revision;this.endpoint='/api/terminal/session/'+encodeURIComponent(session);this.question=question;this.busy=false;this.recovery='';this.notice='';}
 edit(text){if(this.value?.locked||this.busy)return;this.set({...this.value,text,questionRevision:this.questionRevision||null,requestId:this.value?.requestId||crypto.randomUUID(),locked:false});}
 async lookup(rejected=''){
  const value=this.value;if(!value?.locked)return;
  this.recovery='check';
  try{
   const response=await fetch(this.endpoint+'/delivery?request='+encodeURIComponent(value.requestId),{cache:'no-store'});
   if(!chatStateEqual(value,this.value))return;
   if(response.status===404){
    if(rejected){this.set({...value,locked:false,requestId:crypto.randomUUID()});this.recovery='';this.notice=rejected;}
    else{this.recovery='retry';this.notice='No receipt found. Retry this saved answer with its original request ID.';}
   }else if(response.ok){this.acceptReceipt((await response.json()).delivery);}
   else throw Error('Receipt unavailable');
  }catch(e){this.notice='Could not confirm delivery. Your saved answer is locked; check delivery before retrying.';}
 }
 acceptReceipt(receipt){
  const value=this.value,answer=receipt?.questionAnswers?.[0];
  if(receipt?.id!==value?.requestId||receipt?.questionAnswers?.length!==1||answer?.id!==this.question||answer?.answer!==value?.text||(value?.questionRevision&&answer?.revision!==value.questionRevision)||!['sent','unconfirmed'].includes(receipt?.state))throw Error('Answer receipt mismatch');
  this.recovery=receipt.state==='sent'?'done':'check';
  this.notice=receipt.state==='sent'?'Answer sent':'Delivery uncertain — check the conversation before sending again.';
 }
 async check(){if(this.busy||!this.value?.locked)return;this.busy=true;this.publish();try{await this.lookup();}finally{this.busy=false;this.publish();}}
 async submit(){
  if(this.busy||!this.value?.text?.trim()||(this.value.locked&&this.recovery!=='retry'))return;
  this.busy=true;this.notice='Saving answer…';this.publish();
  const expected=this.value;
  try{
   await this.refresh();
   if(this.error||this.conflict||!this.loaded||!chatStateEqual(this.value,expected)){this.notice='Review the saved answer and resolve any sync conflict before sending.';return;}
   this.set({...this.value,locked:true});
   if(!await this.flush()||(this.dirty&&!await this.flush())||this.conflict||!chatStateEqual(this.base,this.value)){this.notice='Answer not submitted. Resolve draft sync, then check delivery to retry.';this.recovery='check';return;}
   const value=this.value;this.notice='Sending answer…';this.publish();
   let rejected='';
   try{
    const response=await fetch(this.endpoint+'/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({requestId:value.requestId,questionAnswers:[{id:this.question,answer:value.text,...(value.questionRevision?{revision:value.questionRevision}:{})}]})});
    if(!response.ok){const message=await response.text();if([400,403,404,409,413].includes(response.status))rejected=message||'Answer was not sent.';throw Error(message);}
    this.acceptReceipt((await response.json()).delivery);
   }catch(e){await this.lookup(rejected);}
  }finally{this.busy=false;this.publish();}
 }
}
function chatQuestionState(o,q,key){
 if(!chatQuestionDrafts.has(key)){
  const entry={state:null,ready:null};
  entry.ready=(async()=>{
   const digest=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(JSON.stringify(['terminal',o.id,q.id,q.revision||'',q.title,q.options||[]])));
   const id=Array.from(new Uint8Array(digest).slice(0,16),b=>b.toString(16).padStart(2,'0')).join('');
   const state=new ChatQuestionDraft('question-'+id,o.id,q.id,q.revision);entry.state=state;await state.refresh();return state;
  })();chatQuestionDrafts.set(key,entry);
 }
 return chatQuestionDrafts.get(key).ready;
}
window.addEventListener('pagehide',()=>{for(const entry of chatQuestionDrafts.values())if(entry.state?.dirty)entry.state.flush();});
window.addEventListener('focus',()=>{for(const entry of chatQuestionDrafts.values())if(entry.state?.active?.())entry.state.refresh().then(()=>{if(entry.state.value?.locked)entry.state.check();});});
function chatQuestionReplyDisplay(text) {
  const value=(text||'').trim(),open='<send_user_message_question_reply>',close='</send_user_message_question_reply>';
  if(!value.startsWith(open)||!value.endsWith(close))return text;
  try {const rows=JSON.parse(value.slice(open.length,-close.length));return rows.map(r=>r.question+'\n'+r.answer).join('\n\n');}catch(e){return text;}
}
function chatQuestionPanel(o) {
  let panel=document.getElementById('chatQuestions');
  if(!o){panel?.remove();return;}
  const composer=document.getElementById('chatComposer');
  if(!composer)return;
  if(panel?.dataset.session!==o.id){panel?.remove();panel=null;}
  // The composer is an action surface; resolved answers live in the transcript.
  // Keep uncertain deliveries visible because they still need attention.
  // A stale question (its asking process is gone) stays visible so the owner
  // learns it cannot be answered, rather than the card silently vanishing.
  const questions=(o.questions||[]).filter(q=>q.state==='pending'||q.state==='unconfirmed'||q.state==='stale');
  if(!questions.length){panel?.remove();return;}
  if(!panel){
    panel=el('section','chat-questions');panel.id='chatQuestions';panel.dataset.session=o.id;
    panel.setAttribute('aria-label','Agent questions');
    panel.append(el('div','micro-label','Questions for you'));
    composer.before(panel);
  }
  panel.children[0].textContent='Questions for you · '+questions.filter(q=>q.state==='pending').length+' awaiting answer'+(questions.some(q=>q.state==='unconfirmed')?' · delivery needs attention':'')+(questions.some(q=>q.state==='stale')?' · '+questions.filter(q=>q.state==='stale').length+' stale':'');
  const ids=new Set(questions.map(q=>q.id));
  for(const node of [...panel.children])if(node.dataset.question&&!ids.has(node.dataset.question))node.remove();
  for(const q of questions){
    const key=JSON.stringify([o.id,q.id,q.revision||'',q.title,q.options||[]]);
    let card=[...panel.children].find(n=>n.dataset.question===q.id);
    const signature=JSON.stringify(q);
    if(card?.dataset.signature===signature)continue;
    const next=chatQuestionCard(o,q,key);next.dataset.question=q.id;next.dataset.signature=signature;
    if(card)card.replaceWith(next);else panel.append(next);
  }
}
function chatQuestionCard(o,q,key) {
  const card=el('form','chat-question');
  const legend=el('legend','chat-question-title',q.title);
  const fields=el('fieldset','chat-question-fields');fields.append(legend);card.append(fields);
  const status=el('div','chat-question-status');status.setAttribute('role','status');
  if(q.state==='stale'){
    status.textContent='This question’s run is no longer live; it cannot be answered. Send a new message to continue.';
    card.classList.add('chat-question-stale');card.append(status);return card;
  }
  if(q.state!=='pending'){
    fields.append(el('div','chat-question-answer',q.answer||''));
    status.textContent=q.state==='answered'?'Answered':q.state==='sent'?'Answer sent':'Delivery uncertain — check the conversation before sending again.';
    const history=el('details','chat-question-history'),summary=el('summary','',q.state==='answered'?'Answered question':q.state==='sent'?'Submitted answer':'Answer delivery unconfirmed');
    history.open=q.state==='unconfirmed';history.append(summary,fields,status);card.append(history);return card;
  }
  if(!q.async || o.se.backend!=='herdr' || o.sharedConversation){
    status.textContent='This runtime prompt needs a response in Terminal.';
    const open=el('button','pill','open terminal');open.type='button';open.onclick=()=>chatOpenTerminalPane(o.se);
    card.append(status,open);return card;
  }
  let draft=null;
  const radios=[];
  const customLabel=el('label','chat-question-custom','Your answer');
  const answer=el('textarea','chat-question-input');answer.rows=2;answer.maxLength=32000;
  customLabel.append(answer);
  const submit=el('button','pill','send answer');submit.type='submit';submit.disabled=true;fields.disabled=true;
  const recovery=el('button','pill','check delivery');recovery.type='button';recovery.hidden=true;
  for(const option of q.options||[]){
    const label=el('label','chat-question-option');const radio=el('input','');radio.type='radio';radio.name=key;radio.value=option;
    radio.onchange=()=>{answer.value=option;draft?.edit(option);};radios.push(radio);
    label.append(radio,el('span','',option));fields.append(label);
  }
  answer.oninput=()=>draft?.edit(answer.value);
  fields.append(customLabel);
  const actions=el('div','feed-actions');actions.append(submit,recovery);card.append(actions,status);
  const sync=el('div','chat-question-sync');card.append(sync);status.textContent='loading…';
  const paint=(state,apply=false)=>{
    if(!card.isConnected)return;
    if(apply&&answer.value!==(state.value?.text||''))answer.value=state.value?.text||'';
    radios.forEach(r=>{r.checked=r.value===answer.value;});
    fields.disabled=state.busy||!!state.value?.locked;
    submit.disabled=state.busy||!!state.value?.locked||!!state.conflict||!state.value?.text?.trim();
    status.textContent=state.notice||'';
    recovery.hidden=!state.value?.locked||state.recovery==='done';recovery.disabled=state.busy||!!state.conflict;
    recovery.textContent=state.recovery==='retry'?'retry answer':'check delivery';
    chatRenderStateNotice(sync,state);
  };
  chatQuestionState(o,q,key).then(async state=>{
   if(!card.isConnected)return;draft=state;state.active=()=>card.isConnected;state.changed=paint;paint(state,true);if(state.value?.locked)await state.check();
  }).catch(()=>{if(card.isConnected)status.textContent='Answer recovery unavailable. Reopen this conversation to retry.';});
  recovery.onclick=async()=>{if(!draft)return;if(draft.recovery==='retry')await draft.submit();else await draft.check();if(chatTermOpen===o)chatTermRequestFinalTail(o);};
  card.onsubmit=async event=>{event.preventDefault();if(!draft)return;await draft.submit();if(chatTermOpen===o)chatTermRequestFinalTail(o);};
  return card;
}
