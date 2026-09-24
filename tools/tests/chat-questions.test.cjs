const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm');
const {readFileSync}=require('node:fs'),{join}=require('node:path'),{webcrypto}=require('node:crypto');
const clone=v=>JSON.parse(JSON.stringify(v));
class Node {
 constructor(tag,cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.attrs={};this.value='';this.disabled=false;this.checked=false;}
 get isConnected(){return this.tag==='body'||!!this.parent?.isConnected;}
 append(...nodes){for(const node of nodes){node.remove();node.parent=this;this.children.push(node);}}
 remove(){if(this.parent){this.parent.children=this.parent.children.filter(n=>n!==this);this.parent=null;}}
 before(node){const p=this.parent,i=p.children.indexOf(this);node.remove();node.parent=p;p.children.splice(i,0,node);}
 replaceWith(node){this.before(node);this.remove();}
 setAttribute(k,v){this.attrs[k]=v;}
}
const all=n=>[n,...n.children.flatMap(all)];
const receipt=(payload,state='sent')=>({ok:true,status:200,json:async()=>({delivery:{id:payload.requestId,state,questionAnswers:payload.questionAnswers}})});
function fixture(send,options={}){
 const body=new Node('body'),composer=new Node('div');composer.id='chatComposer';body.append(composer);
 const q={revision:'a'.repeat(64),id:'["request_user_input_async","call_one",0]',title:'Which approach?',options:['First','Second'],state:'pending',async:true};
 const o={id:options.session||'abcdef12',se:{backend:'herdr'},questions:[q]};
 const remote=options.remote||new Map(),local=options.local||new Map(),requests=[],stateRequests=[],events={};let seq=0;
 const ctx={el:(...args)=>new Node(...args),document:{getElementById:id=>all(body).find(n=>n.id===id)},crypto:{subtle:webcrypto.subtle,randomUUID:()=>`request-${++seq}`},TextEncoder,chatTermOpen:o,chatTermRequestFinalTail:()=>{},chatOpenTerminalPane:()=>{},chatRenderStateNotice:()=>{},console,
  window:{addEventListener:(name,fn)=>events[name]=fn},localStorage:{getItem:k=>local.get(k),setItem:(k,v)=>local.set(k,v)},setTimeout:()=>1,clearTimeout(){},
  fetch:async(url,opts={})=>{
   if(!url.startsWith('/api/chat/state/')){requests.push([url,opts]);return send(url,opts);}
   stateRequests.push([url,opts]);if(options.offline?.())throw Error('offline');
   const parts=url.split('/');let snap=remote.get(url)||{key:parts.at(-2),slot:parts.at(-1),revision:0,value:null};
   if(opts.method==='PUT'){
    const b=JSON.parse(opts.body);if(b.revision!==snap.revision&&JSON.stringify(b.value)!==JSON.stringify(snap.value))return {status:409,json:async()=>clone(snap)};
    snap={...snap,revision:snap.revision+1,value:b.value};remote.set(url,snap);
    if(options.loseSave?.())throw Error('lost save acknowledgement');
   }
   return {ok:true,status:200,json:async()=>clone(snap)};
  }};
 vm.createContext(ctx);
 for(const file of ['47-chat-state.js','48-chat-questions.js'])vm.runInContext(readFileSync(join(__dirname,'../../server/web/js',file),'utf8'),ctx);
 vm.runInContext('globalThis.drafts=chatQuestionDrafts',ctx);
 ctx.chatQuestionPanel(o);
 const state=()=>[...ctx.drafts.values()][0].state;
 const ready=async()=>{await Promise.all([...ctx.drafts.values()].map(e=>e.ready));await new Promise(r=>setImmediate(r));};
 return {ctx,body,o,q,remote,local,requests,stateRequests,events,state,ready,find:tag=>all(body).find(n=>n.tag===tag)};
}
test('options and free text have no default submission; polls preserve typed DOM',async()=>{
 const f=fixture(()=>{});await f.ready();const input=f.find('textarea'),form=f.find('form');assert.equal(f.find('button').disabled,true);assert.equal(f.find('input').checked,false);
 input.value='My own answer';input.oninput();f.ctx.chatQuestionPanel(f.o);
 assert.equal(f.find('textarea'),input);assert.equal(f.find('form'),form);assert.equal(input.value,'My own answer');assert.equal(f.requests.length,0);
 f.q.state='answered';f.q.answer='Elsewhere';f.ctx.chatQuestionPanel(f.o);assert(!f.ctx.document.getElementById('chatQuestions'));assert(!f.find('textarea'));
 f.ctx.chatQuestionPanel(null);assert(!f.ctx.document.getElementById('chatQuestions'));
});
test('saves frozen identity before sending exactly once to captured session after navigation',async()=>{
 let f;f=fixture(async(url,opts)=>{
  const payload=JSON.parse(opts.body),saved=[...f.remote.values()][0].value;
  assert.equal(saved.locked,true);assert.equal(saved.requestId,payload.requestId);assert.equal(saved.text,payload.questionAnswers[0].answer);
  return receipt(payload);
 });await f.ready();
 const option=all(f.body).filter(n=>n.tag==='input')[1];option.onchange();const form=f.find('form');
 f.ctx.chatTermOpen={id:'different'};await form.onsubmit({preventDefault(){}});await form.onsubmit({preventDefault(){}});
 assert.equal(f.requests.length,1);assert.equal(f.requests[0][0],'/api/terminal/session/abcdef12/input');
 const payload=JSON.parse(f.requests[0][1].body);assert.equal(payload.questionAnswers[0].id,f.q.id);assert.equal(payload.questionAnswers[0].revision,f.q.revision);assert.equal(payload.questionAnswers[0].answer,'Second');assert.equal(f.find('button').disabled,true);
});
test('lost response checks exact receipt without resending and keeps uncertain answer locked',async()=>{
 let payload;const f=fixture(async(url,opts)=>{if(url.endsWith('/input')){payload=JSON.parse(opts.body);throw new Error('network');}return receipt(payload,'unconfirmed');});await f.ready();
 const input=f.find('textarea');input.value='Keep this';input.oninput();await f.find('form').onsubmit({preventDefault(){}});
 assert.equal(f.requests.length,2);assert(f.requests[1][0].includes('/delivery?request=request-1'));assert.equal(input.value,'Keep this');assert.equal(f.find('button').disabled,true);
 assert.equal(f.state().recovery,'check');
 const reloaded=fixture(async()=>receipt(payload,'unconfirmed'),{remote:f.remote,local:f.local});await reloaded.ready();
 assert.equal(reloaded.find('textarea').value,'Keep this');assert.equal(reloaded.state().value.requestId,payload.requestId);
 assert(reloaded.requests.every(([url])=>url.includes('/delivery?')));assert.equal(reloaded.state().recovery,'check');
});
test('definitive refusal with no receipt permits correction; synchronous prompts use Terminal',async()=>{
 const f=fixture(async(url)=>url.endsWith('/input')?{ok:false,status:400,text:async()=> 'Nothing sent'}:{ok:false,status:404});await f.ready();
 const input=f.find('textarea');input.value='Answer';input.oninput();await f.find('form').onsubmit({preventDefault(){}});assert.equal(f.find('button').disabled,false);
 f.q.async=false;f.ctx.chatQuestionPanel(f.o);assert.equal(f.find('button').textContent,'open terminal');assert(!f.find('textarea'));
});
test('unfinished answers survive local reload and second device without crossing sessions',async()=>{
 const f=fixture(()=>{});await f.ready();const input=f.find('textarea');input.value='Unfinished answer';input.oninput();await f.state().flush();
 for(const options of [{remote:f.remote,local:f.local},{remote:f.remote}]){
  const next=fixture(()=>{},options);await next.ready();assert.equal(next.find('textarea').value,'Unfinished answer');assert.equal(next.requests.length,0);
 }
 const other=fixture(()=>{},{remote:f.remote,session:'different'});await other.ready();assert.equal(other.find('textarea').value,'');
});
test('offline save blocks submission and keeps a locally recoverable answer',async()=>{
 let offline=false;const f=fixture(()=>{throw Error('must not send');},{offline:()=>offline});await f.ready();
 f.find('textarea').value='Offline answer';f.find('textarea').oninput();offline=true;
 await f.find('form').onsubmit({preventDefault(){}});assert.equal(f.requests.length,0);assert.equal(f.state().value.text,'Offline answer');
 const next=fixture(()=>{},{remote:f.remote,local:f.local,offline:()=>true});await next.ready();assert.equal(next.find('textarea').value,'Offline answer');assert.equal(next.requests.length,0);
});
test('lost draft save acknowledgment never sends; explicit retry uses frozen payload',async()=>{
 let lose=true;const f=fixture(async(url,opts)=>url.includes('/delivery?')?{ok:false,status:404}:receipt(JSON.parse(opts.body)),{loseSave:()=>{const v=lose;lose=false;return v;}});await f.ready();
 f.find('textarea').value='Preserve exactly';f.find('textarea').oninput();await f.state().submit();assert.equal(f.requests.length,0);assert.equal(f.state().value.locked,true);
 const frozen=clone(f.state().value);
 const next=fixture(async(url,opts)=>url.includes('/delivery?')?{ok:false,status:404}:receipt(JSON.parse(opts.body)),{remote:f.remote,local:f.local});await next.ready();
 assert.equal(next.state().recovery,'retry');assert(next.requests.every(([url])=>url.includes('/delivery?')));
 await next.state().submit();const posted=JSON.parse(next.requests.find(([url])=>url.endsWith('/input'))[1].body);
 assert.equal(posted.requestId,frozen.requestId);assert.equal(posted.questionAnswers[0].answer,frozen.text);
});
test('concurrent devices conflict before submission; receipt mismatch never authorizes retry',async()=>{
 const a=fixture(()=>{}),b=fixture(()=>{throw Error('must not submit');},{remote:a.remote});await a.ready();await b.ready();
 a.state().edit('Saved on desktop');await a.state().flush();b.state().edit('Phone draft');await b.state().submit();
 assert(b.state().conflict);assert.equal(b.requests.length,0);assert.equal(b.state().value.text,'Phone draft');
 const wrong=fixture(async()=>receipt({requestId:'wrong-id',questionAnswers:[]}),{remote:a.remote});await wrong.ready();await wrong.state().submit();
 assert.equal(wrong.state().value.locked,true);assert.equal(wrong.state().recovery,'check');
});
test('uncertain answers remain expanded and visibly need attention',()=>{
 const f=fixture(async()=>{});f.q.state='unconfirmed';f.q.answer='Saved answer';f.ctx.chatQuestionPanel(f.o);
 assert.equal(f.find('details').open,true);assert.equal(f.find('summary').textContent,'Answer delivery unconfirmed');assert(all(f.body).some(n=>n.textContent.includes('delivery needs attention')));
});
test('resolved questions retire independently and new questions retain separate drafts',async()=>{
 const f=fixture(()=>{});await f.ready();const next={...f.q,id:'next',title:'Next question'};f.o.questions.push(next);f.ctx.chatQuestionPanel(f.o);await f.ready();
 const nextForm=all(f.body).find(n=>n.dataset.question==='next');f.q.state='answered';f.ctx.chatQuestionPanel(f.o);
 assert.equal(all(f.body).filter(n=>n.tag==='form').length,1);assert.equal(all(f.body).find(n=>n.dataset.question==='next'),nextForm);
 next.state='sent';f.ctx.chatQuestionPanel(f.o);assert(!f.ctx.document.getElementById('chatQuestions'));
 next.state='pending';f.ctx.chatQuestionPanel(f.o);assert(f.ctx.document.getElementById('chatQuestions'));assert.equal(f.requests.length,0);
});

test('a changed question revision cannot inherit the previous answer draft',async()=>{
 const f=fixture(()=>{});await f.ready();f.state().edit('Answer to old wording');await f.state().flush();
 f.q.revision='b'.repeat(64);f.q.title='Different wording under the same question ID';f.ctx.chatQuestionPanel(f.o);await f.ready();
 assert.equal(f.find('textarea').value,'');assert.equal(f.requests.length,0);
});
