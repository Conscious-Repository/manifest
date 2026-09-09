const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
let release,entered;
const gate=new Promise(r=>release=r),started=new Promise(r=>entered=r),urls=[];
const state={value:{text:'private draft',files:[],selection:{id:'plan',revision:'v1'}},set(v){this.value=v;}};
let paints=0,saves=0;
const ctx=vm.createContext({chatDraftKey:'alfred/source',chatTaskID:'',els:{chatView:{hidden:false}},chatCurSession:{},
 chatSyncedDrafts:new Map([['alfred/source',state]]),chatDrafts:new Map(),
 renderChatComposer(){paints++;},chatSaveDraft(){saves++;},showToast(){},
 fetch:async url=>{urls.push(url);if(urls.length===1){entered();await gate;}return {ok:true,json:async()=>({file:{hash:url,name:url.split('name=')[1],size:1}})};}});
vm.runInContext(src.slice(src.indexOf('let chatPendingFiles = []'),src.indexOf('// chatMentionOptions')),ctx);
(async()=>{
 const work=ctx.chatUploadFiles('alfred/source','/private-upload?id=agentchat',[{name:'one'},{name:'two'}]);
 await started;
 assert.equal(vm.runInContext('chatUploads.get("alfred/source")',ctx),1);
 ctx.chatDraftKey='zeck/team';release();await work;
 assert.equal(paints,1,'completion must not repaint the destination composer');
 assert.equal(saves,0);assert.equal(vm.runInContext('chatPendingFiles.length',ctx),0);
 assert.equal(urls.length,2);assert.ok(urls.every(u=>u.startsWith('/private-upload?')));
 assert.equal(state.value.files.length,2);assert.equal(state.value.text,'private draft');assert.equal(state.value.selection.revision,'v1');
 assert.equal(vm.runInContext('chatUploads.size',ctx),0);
 // Returning to the original chat allows subsequent uploads to update its UI.
 ctx.chatDraftKey='alfred/source';await ctx.chatUploadFiles('alfred/source','/private-upload?id=agentchat',[{name:'three'}]);
 assert.equal(saves,1);assert.equal(vm.runInContext('chatPendingFiles.length',ctx),1);
 // Native task navigation can leave the former key around: never touch its composer.
 ctx.chatTaskID='native-task';const before=paints;
 await ctx.chatUploadFiles('alfred/source','/private-upload?id=agentchat',[{name:'four'}]);
 assert.equal(paints,before);assert.equal(state.value.files.length,3);
 ctx.fetch=async()=>{throw Error('offline');};
 await ctx.chatUploadFiles('alfred/source','/private-upload?id=agentchat',[{name:'failed'}]);
 assert.equal(vm.runInContext('chatUploads.size',ctx),0,'failed uploads must release send gating');
 console.log('Upload batches retain their original destination and draft across navigation');
})().catch(e=>{console.error(e);process.exitCode=1;});
