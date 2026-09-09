const assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs'),path=require('node:path');
vm.runInThisContext(fs.readFileSync(path.join(__dirname,'../web/js/96-aion-recruiting.js'),'utf8'));
class Node {
 constructor(tag,cls,text){this.tag=tag;this.className=cls;this.textContent=text||'';this.children=[];this.dataset={};this.hidden=false;this.disabled=false;this.value='';}
 append(...ns){this.children.push(...ns);}
 replaceChildren(...ns){this.children=ns;}
 setAttribute(k,v){this[k]=v;}
 get options(){return this.children;}
 get selectedOptions(){return this.children.filter(o=>o.value===this.value);}
}
global.el=(...args)=>new Node(...args);
global.showToast=()=>{};
const stageRows=[{id:'screen',title:'Initial Screen',type:'Active',orderInInterviewPlan:1},{id:'round',title:'First Round',type:'Active',orderInInterviewPlan:2},{id:'archive',title:'Archived',type:'Archived',orderInInterviewPlan:3}];
const person={id:'cand/example',ashbyApplicationId:'app-two',ashbyStage:'Initial Screen',applications:[{id:'app-two',stageId:'screen'}]};
let calls=[],fail=false;
global.fetch=async(url,options={})=>{
 calls.push({url,...options});
 if(url.includes('/stages/'))return {ok:true,json:async()=>({stages:stageRows})};
 if(url.endsWith('/reasons'))return {ok:true,json:async()=>({reasons:[{id:'reason',text:'Role requirements'}]})};
 if(fail && url.includes('/stage/'))throw new Error('connection lost');
 return {ok:true,json:async()=>({view:{candidates:[]}})};
};
const settle=()=>new Promise(r=>setImmediate(r));
const all=n=>[n,...n.children.flatMap(all)];
(async()=>{
 const box=recApplicationControls(person);await settle();
 const stage=all(box).find(n=>n['aria-label']==='Candidate stage');
 assert.equal(stage.value,'screen');assert.equal(stage.disabled,false);
 const actions=all(box).find(n=>n.className==='rec-stage-actions');
 const [save,cancel]=actions.children;
 assert.equal(actions.hidden,true);
 stage.value='round';stage.onchange();assert.equal(save.textContent,'Move to First Round');assert.equal(actions.hidden,false);
 assert.equal(calls.filter(c=>c.method==='POST').length,0,'selection must not write');
 cancel.onclick();assert.equal(stage.value,'screen');assert.equal(actions.hidden,true);
 stage.value='archive';stage.onchange();await settle();
 const reason=all(box).find(n=>n.tag==='select'&&n!==stage);
 assert.equal(save.disabled,true,'archive needs a reason');
 reason.value='reason';reason.onchange();assert.equal(save.disabled,false);
 await save.onclick();
 const post=calls.find(c=>c.method==='POST');assert.deepEqual(JSON.parse(post.body),{applicationId:'app-two',interviewStageId:'archive',archiveReasonId:'reason'});
 // An uncertain write can only be verified, never accidentally submitted twice.
 calls=[];fail=true;
 const failed=recApplicationControls(person);await settle();
 const sel=all(failed).find(n=>n['aria-label']==='Candidate stage');
 const action=all(failed).find(n=>n.className==='rec-stage-actions').children[0];
 sel.value='round';sel.onchange();await action.onclick();
 assert.equal(action.textContent,'Check status');assert.equal(action.disabled,false);
 await action.onclick();
 assert.equal(calls.filter(c=>c.url.includes('/stage/')&&c.method==='POST').length,1);
 assert.equal(calls.filter(c=>c.url.endsWith('/sync')).length,1);
 console.log('Stage selection, cancellation, archive validation and uncertain-save recovery passed');
})().catch(e=>{console.error(e);process.exitCode=1;});
