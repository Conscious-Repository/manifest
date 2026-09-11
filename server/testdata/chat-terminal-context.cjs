const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict'),path=require('path');
const source=fs.readFileSync(path.join(__dirname,'../web/js/73-terminal.js'),'utf8'),classes=new Set(['term-nav-open']);
const shell={classList:{add:v=>classes.add(v),remove:v=>classes.delete(v)}};
const ctx=vm.createContext({document:{querySelector:()=>shell},termSessions:[{id:'exact',live:true}],termRuntimeKey:()=> 'identity',termInst:{id:'exact',runtimeKey:'identity',ws:{readyState:1}},Terminal:function(){},renderTermEmpty(){throw Error('unexpected empty state');}});
let start=source.indexOf('function attachTerm('),end=source.indexOf('\nfunction ',start+1);vm.runInContext(source.slice(start,end),ctx);vm.runInContext(source.slice(source.indexOf('function termRevealSession(')),ctx);
vm.runInContext('attachTerm("exact")',ctx);assert.ok(classes.has('term-session-active'));assert.ok(!classes.has('term-nav-open'));assert.equal(ctx.termInst.id,'exact');
console.log('PASS: reopening an already attached exact session reveals its terminal without reconnecting or launching.');
const panel=fs.readFileSync(path.join(__dirname,'../web/js/93-todo-panel.js'),'utf8');
const route={hash:'#/chat/a/codex/exact'},panelCtx=vm.createContext({todoSelId:null,todoPanelOrigin:null,todoPanelData:null,todoPanelReturnRoute:'',todoComposerPreset:null,location:route,history:{replaceState:(_a,_b,url)=>route.hash=url},document:{activeElement:null,querySelectorAll:()=>[]},ensureTodoPanelPoll(){},renderTodoPanel(){}});
for(const name of ['openTodoPanel','closeTodoPanel']){const start=panel.indexOf('function '+name+'('),end=panel.indexOf('\nfunction ',start+1);vm.runInContext(panel.slice(start,end),panelCtx);}
vm.runInContext('openTodoPanel("task-id",{returnRoute:location.hash});closeTodoPanel();',panelCtx);assert.equal(route.hash,'#/chat/a/codex/exact');
console.log('PASS: task details preserves the originating coding-chat route through open and close.');
