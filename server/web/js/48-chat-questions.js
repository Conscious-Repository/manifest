// Native async question cards use the existing terminal input receipt boundary.
// Keep keyed DOM nodes: a transcript poll must never replace a focused answer.
const chatQuestionDrafts = new Map();
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
  const questions=o.questions||[];
  if(!questions.length){panel?.remove();return;}
  if(!panel){
    panel=el('section','chat-questions');panel.id='chatQuestions';panel.dataset.session=o.id;
    panel.setAttribute('aria-label','Agent questions');
    panel.append(el('div','micro-label','Questions for you'));
    composer.before(panel);
  }
  panel.children[0].textContent='Questions for you · '+questions.filter(q=>q.state==='pending').length+' awaiting answer';
  const ids=new Set(questions.map(q=>q.id));
  for(const node of [...panel.children])if(node.dataset.question&&!ids.has(node.dataset.question))node.remove();
  for(const q of questions){
    const key=o.id+':'+q.id;
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
  if(q.state!=='pending'){
    fields.append(el('div','chat-question-answer',q.answer||''));
    status.textContent=q.state==='answered'?'Answered':q.state==='sent'?'Answer sent':'Delivery uncertain — check the conversation before sending again.';
    const history=el('details','chat-question-history'),summary=el('summary','',q.state==='answered'?'Answered question':'Submitted answer');
    history.append(summary,fields,status);card.append(history);return card;
  }
  if(!q.async || o.se.backend!=='herdr' || o.sharedConversation){
    status.textContent='This runtime prompt needs a response in Terminal.';
    const open=el('button','pill','Open Terminal');open.type='button';open.onclick=()=>chatOpenTerminalPane(o.se);
    card.append(status,open);return card;
  }
  let draft=chatQuestionDrafts.get(key);
  if(!draft){draft={answer:'',requestId:crypto.randomUUID(),locked:false};chatQuestionDrafts.set(key,draft);}
  const radios=[];
  const customLabel=el('label','chat-question-custom','Your answer');
  const answer=el('textarea','chat-question-input');answer.rows=2;answer.maxLength=32000;answer.value=draft.answer;
  customLabel.append(answer);
  const submit=el('button','pill','Send answer');submit.type='submit';submit.disabled=!draft.answer.trim()||draft.locked;
  for(const option of q.options||[]){
    const label=el('label','chat-question-option');const radio=el('input','');radio.type='radio';radio.name=draft.requestId;radio.value=option;radio.checked=draft.answer===option;
    radio.onchange=()=>{draft.answer=option;answer.value=option;submit.disabled=false;};radios.push(radio);
    label.append(radio,el('span','',option));fields.append(label);
  }
  answer.oninput=()=>{draft.answer=answer.value;radios.forEach(r=>{r.checked=r.value===draft.answer;});submit.disabled=!draft.answer.trim();};
  fields.append(customLabel);fields.disabled=draft.locked;
  const actions=el('div','feed-actions');actions.append(submit);card.append(actions,status);
  status.textContent=draft.notice||'';
  card.onsubmit=async event=>{
    event.preventDefault();if(draft.locked||!draft.answer.trim())return;
    draft.locked=true;fields.disabled=true;submit.disabled=true;status.textContent='Sending answer…';
    const url='/api/terminal/session/'+encodeURIComponent(o.id);
    let rejected=false;
    try {
      const response=await fetch(url+'/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({requestId:draft.requestId,questionAnswers:[{id:q.id,answer:draft.answer}]})});
      if(!response.ok){const message=await response.text();rejected=true;throw new Error(message||'Answer was not sent.');}
      const result=await response.json();
      draft.notice=result.delivery?.state==='sent'?'Answer sent':'Delivery uncertain — check the conversation before sending again.';
    }catch(error){
      if(draft.locked){
        // A lost HTTP response is not permission to submit again.
        try {const check=await fetch(url+'/delivery?request='+encodeURIComponent(draft.requestId));
          if(check.ok){const result=await check.json();draft.notice=result.delivery?.state==='sent'?'Answer sent':'Delivery uncertain — check the conversation before sending again.';}
          else if(check.status===404&&rejected){draft.locked=false;draft.notice=error.message;}
          else {draft.notice='Could not confirm delivery. Your answer is retained; check the conversation before retrying.';}
        }catch(e){draft.notice='Could not confirm delivery. Your answer is retained; check the conversation before retrying.';}
      }else draft.notice=error.message;
    }
    status.textContent=draft.notice;fields.disabled=draft.locked;submit.disabled=draft.locked||!draft.answer.trim();
    if(chatTermOpen===o)chatTermTail(o);
  };
  return card;
}
