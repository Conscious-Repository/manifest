const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm');
const {readFileSync}=require('node:fs'),{join}=require('node:path');
class Node {
 constructor(tag,cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.attrs={};this.value='';this.disabled=false;this.checked=false;}
 append(...nodes){for(const node of nodes){node.remove();node.parent=this;this.children.push(node);}}
 remove(){if(this.parent){this.parent.children=this.parent.children.filter(n=>n!==this);this.parent=null;}}
 before(node){const p=this.parent,i=p.children.indexOf(this);node.remove();node.parent=p;p.children.splice(i,0,node);}
 replaceWith(node){this.before(node);this.remove();}
 setAttribute(k,v){this.attrs[k]=v;}
}
const all=n=>[n,...n.children.flatMap(all)];
function fixture(fetch){
 const body=new Node('body'),composer=new Node('div');composer.id='chatComposer';body.append(composer);
 const q={id:'["request_user_input_async","call_one",0]',title:'Which approach?',options:['First','Second'],state:'pending',async:true};
 const o={id:'abcdef12',se:{backend:'herdr'},questions:[q]};
 let requests=[],seq=0;
 const ctx={el:(...args)=>new Node(...args),document:{getElementById:id=>all(body).find(n=>n.id===id)},crypto:{randomUUID:()=>`request-${++seq}`},chatTermOpen:o,chatTermRequestFinalTail:()=>{},chatOpenTerminalPane:()=>{},fetch:async(url,options)=>{requests.push([url,options]);return fetch(url,options);},console};
 vm.createContext(ctx);vm.runInContext(readFileSync(join(__dirname,'../../server/web/js/48-chat-questions.js'),'utf8'),ctx);
 ctx.chatQuestionPanel(o);return {ctx,body,o,q,requests,find:tag=>all(body).find(n=>n.tag===tag)};
}
test('questions expose options and free text without default submission; polls preserve typed DOM',()=>{
 const f=fixture(()=>{}),input=f.find('textarea'),form=f.find('form');assert.equal(f.find('button').disabled,true);assert.equal(f.find('input').checked,false);
 input.value='My own answer';input.oninput();f.ctx.chatQuestionPanel(f.o);
 assert.equal(f.find('textarea'),input);assert.equal(f.find('form'),form);assert.equal(input.value,'My own answer');assert.equal(f.requests.length,0);
 f.q.state='answered';f.q.answer='Elsewhere';f.ctx.chatQuestionPanel(f.o);assert(f.find('details'));assert(!f.find('textarea'));
 f.ctx.chatQuestionPanel(null);assert(!f.ctx.document.getElementById('chatQuestions'));
});
test('sends exact question identity to captured session once even after navigation',async()=>{
 const f=fixture(async()=>({ok:true,json:async()=>({delivery:{state:'sent'}})}));
 const option=all(f.body).filter(n=>n.tag==='input')[1];option.onchange();const form=f.find('form');
 f.ctx.chatTermOpen={id:'different'};await form.onsubmit({preventDefault(){}});await form.onsubmit({preventDefault(){}});
 assert.equal(f.requests.length,1);assert.equal(f.requests[0][0],'/api/terminal/session/abcdef12/input');
 const payload=JSON.parse(f.requests[0][1].body);assert.equal(payload.questionAnswers[0].id,f.q.id);assert.equal(payload.questionAnswers[0].answer,'Second');assert.equal(f.find('button').disabled,true);
});
test('lost response checks receipt without resending and keeps uncertain answer locked',async()=>{
 const f=fixture(async(url)=>{if(url.endsWith('/input'))throw new Error('network');return {ok:true,json:async()=>({delivery:{state:'unconfirmed'}})};});
 const input=f.find('textarea');input.value='Keep this';input.oninput();await f.find('form').onsubmit({preventDefault(){}});
 assert.equal(f.requests.length,2);assert(f.requests[1][0].includes('/delivery?request=request-1'));assert.equal(input.value,'Keep this');assert.equal(f.find('button').disabled,true);
});
test('definitive refusal with no receipt permits correction; synchronous prompts use Terminal',async()=>{
 const f=fixture(async(url)=>url.endsWith('/input')?{ok:false,text:async()=> 'Nothing sent'}:{ok:false,status:404});
 const input=f.find('textarea');input.value='Answer';input.oninput();await f.find('form').onsubmit({preventDefault(){}});assert.equal(f.find('button').disabled,false);
 f.q.async=false;f.ctx.chatQuestionPanel(f.o);assert.equal(f.find('button').textContent,'Open Terminal');assert(!f.find('textarea'));
});
