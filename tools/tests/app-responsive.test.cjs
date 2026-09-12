const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const read=name=>fs.readFileSync('server/web/js/'+name+'.js','utf8');
const section=(s,a,b)=>s.slice(s.indexOf(a),s.indexOf(b,s.indexOf(a)+1));
test('Files discards out-of-order results and stale failures while preserving the latest loading state',async()=>{
 const pending=[],body={inert:false,setAttribute(k,v){this[k]=v},replaceChildren(x){this.content=x}};
 const c={fsHost:'local',fsPath:'/old',fsEntries:[],fsListedPath:'',document:{getElementById:()=>body,querySelectorAll:()=>[]},fetch:()=>new Promise(r=>pending.push(r)),fsRenderToolbar:()=>{},fsRenderCrumbs:()=>{},fsRenderBody:()=>{},emptyRow:x=>x};vm.createContext(c);
 vm.runInContext(section(read('74-files'),'let fsLoadVersion','function fsVisibleEntries'),c);
 const first=c.fsLoad();c.fsPath='/new';const second=c.fsLoad();
 pending[0]({ok:false,text:async()=> 'old failure'});await first;assert.equal(body.inert,true);assert.equal(body.content,undefined);
 pending[1]({ok:true,json:async()=>({path:'/new',entries:[{name:'new'}]})});await second;assert.equal(c.fsListedPath,'/new');assert.equal(body.inert,false);
 c.fsPath='/a';const a=c.fsLoad();c.fsPath='/b';const b=c.fsLoad();pending[3]({ok:true,json:async()=>({path:'/b',entries:[]})});await b;pending[2]({ok:true,json:async()=>({path:'/a',entries:[]})});await a;assert.equal(c.fsListedPath,'/b');
});
test('Contacts keeps the most recently opened person when responses arrive backwards',async()=>{
 const pending=[],painted=[];const c={els:{contactsListPane:{},contactPagePane:{},contactPageSaved:{},contactPage:{}},fetch:()=>new Promise(r=>pending.push(r)),renderContactPage:p=>painted.push(p)};vm.createContext(c);vm.runInContext(section(read('60-contacts'),'let contactPageVersion','function cpSection'),c);
 const a=c.showContactPage('a'),b=c.showContactPage('b');pending[1]({ok:true,json:async()=>({key:'b'})});await b;pending[0]({ok:true,json:async()=>({key:'a'})});await a;assert.deepEqual(painted,[{key:'b'}]);
});
test('Nearby contacts does not overwrite the ordinary list after leaving nearby mode',async()=>{
 let finish;const host={innerHTML:'',append(){}},c={_nearbyPlace:{lat:1,lng:2,label:'City'},_nearbyMode:true,_nearbyRadius:50,URLSearchParams,els:{contactNearby:{querySelector:()=>({value:50})},contactList:host,contactsListPane:{hidden:false}},emptyRow:x=>x,fetch:()=>new Promise(r=>finish=r),nearbyContactRow:x=>x};
 vm.createContext(c);vm.runInContext(section(read('60-contacts'),'let nearbySearchVersion','function nearbyContactRow'),c);const request=c.runNearbySearch();c._nearbyMode=false;host.innerHTML='Ordinary contacts';finish({ok:true,json:async()=>({contacts:[]})});await request;assert.equal(host.innerHTML,'Ordinary contacts');
});
test('Agents polling neither overlaps nor writes a late result after leaving the screen',async()=>{
 let calls=0,finish,open=true;
 const c={document:{hidden:true},pollScopeOpen:()=>open,stopLivePoll:()=>{},liveBaselined:false,spiritRuns:{data:['original']},fetchSpiritRuns:()=>{calls++;return new Promise(r=>finish=r)}};vm.createContext(c);vm.runInContext(section(read('40-agents'),'let livePollPending','async function detectNewDigest'),c);
 await c.livePoll();assert.equal(calls,0);c.document.hidden=false;const first=c.livePoll();await c.livePoll();assert.equal(calls,1);open=false;finish({data:['late']});await first;assert.deepEqual(c.spiritRuns,{data:['original']});
});
