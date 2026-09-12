const test=require("node:test");
const {readFileSync}=require('node:fs');const vm=require('node:vm');const assert=require('node:assert/strict');
const source=readFileSync(require('node:path').join(__dirname,'../../server/web/olga/olga.js'),'utf8').split('function attachWikilinkAutocomplete')[0];
class Element{constructor(tag,cls,text){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.listeners={};}append(...xs){this.children.push(...xs);}setAttribute(){}addEventListener(k,fn){this.listeners[k]=fn;}showModal(){this.open=true;}close(){this.open=false;}remove(){this.removed=true;}focus(){}}
let authorized=false,dialogs=[],sent=[],attempts=0;
const fetch=async(input,init)=>{const req=typeof input==='string'?new Request('http://olga.test'+input,init):input;
 if(new URL(req.url).pathname==='/api/session'){if((await req.text()).includes('password=right')){authorized=true;return new Response('{}',{status:200})}return new Response('Incorrect password',{status:401});}
 attempts++;const body=await req.text();if(!authorized)return new Response('expired',{status:401});sent.push(body);return new Response('{}',{status:200});};
const ctx={window:{fetch},Request,Response,URL,URLSearchParams,location:{origin:'http://olga.test'},el:(...a)=>new Element(...a),document:{body:{append:d=>dialogs.push(d)}},Error};vm.createContext(ctx);vm.runInContext(source,ctx);
const find=(n,tag)=>n.tag===tag?n:n.children.map(c=>find(c,tag)).find(Boolean);
const tick=()=>new Promise(r=>setImmediate(r));
test('expired API requests share sign-in and replay once without losing their bodies',async()=>{
 const one=ctx.window.fetch('http://olga.test/api/tasks/item',{method:'POST',body:'draft-one'});
 const two=ctx.window.fetch('http://olga.test/api/areas',{method:'POST',body:'draft-two'});
 for(let i=0;i<10&&!dialogs.length;i++)await tick();assert.equal(dialogs.length,1);assert.equal(sent.length,0);
 const form=find(dialogs[0],'form'),password=find(form,'input');password.value='wrong';await form.onsubmit({preventDefault(){}});assert.equal(sent.length,0);assert.equal(dialogs[0].removed,undefined);
 password.value='right';await form.onsubmit({preventDefault(){}});const responses=await Promise.all([one,two]);assert(responses.every(r=>r.status===200));assert.deepEqual(sent.sort(),['draft-one','draft-two']);assert.equal(attempts,4);assert.equal(dialogs[0].removed,true);assert.equal(password.value,'');
 console.log('Session recovery: one prompt, wrong-password retention, exact request replay, and credential cleanup passed');
});
