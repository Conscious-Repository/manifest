// chat-live-churn.cjs — the terminal runtime's state tick (~2 s) arrives with
// only observedAt moved most of the time. QA 2026-09-12 found every tick
// rebuilding the rail rows and the thread head: an open row menu (⋯) snapped
// shut and keyboard focus on any header button or rail row dropped to <body>.
// Three guards: a no-op tick repaints nothing structural, a rebuilt subtree
// restores focus to the equivalent control, and a repaint declined mid-
// interaction is retried on the next tick.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};

// ---- a fake DOM small enough to read ----
function matches(n,sel){
 return sel.split(',').some(one=>{one=one.trim();let ok=true;
  const tag=one.match(/^[a-z]+/i);if(tag)ok=ok&&n.tagName.toLowerCase()===tag[0].toLowerCase();
  for(const m of one.matchAll(/\.([\w-]+)/g))ok=ok&&n.classList.contains(m[1]);
  for(const m of one.matchAll(/#([\w-]+)/g))ok=ok&&n.id===m[1];
  for(const m of one.matchAll(/\[([\w-]+)\]/g))ok=ok&&(m[1]==='open'?!!n.open:m[1]==='data-inbox-key'?n.dataset.inboxKey!==undefined:m[1]==='data-terminal-id'?n.dataset.terminalId!==undefined:n.attrs[m[1]]!==undefined);
  if(one.includes(':not(:disabled)'))ok=ok&&!n.disabled;
  return ok;});
}
const doc={activeElement:null};
function node(tag,cls='',text='',o={}){
 const n={tagName:tag.toUpperCase(),className:cls,textContent:text,id:o.id||'',attrs:o.attrs||{},dataset:o.dataset||{},open:!!o.open,children:[],parent:null,title:'',
  classList:{contains:c=>n.className.split(' ').includes(c)},
  getAttribute(k){return k in n.attrs?n.attrs[k]:null;},setAttribute(k,v){n.attrs[k]=v;},
  focus(){doc.activeElement=n;},
  closest(sel){let c=n;while(c){if(matches(c,sel))return c;c=c.parent;}return null;},
  contains(x){return x===n||n.children.some(c=>c.contains(x));},
  all(){return n.children.flatMap(c=>[c,...c.all()]);},
  querySelectorAll(sel){return n.all().filter(x=>matches(x,sel));},
  querySelector(sel){return n.querySelectorAll(sel)[0]||null;},
  append(...kids){kids.forEach(k=>{k.parent=n;n.children.push(k);});return n;},
  replaceChildren(...kids){n.children=[];n.append(...kids);}};
 return n;
}

// ---- 1. a no-op tick repaints nothing structural; a change or a deferred repaint does ----
{
 const calls={rail:0,head:0};let railResult=true;
 const dot=node('span','status-dot terminal-state-dot',' ',{dataset:{terminalId:'s1'}});
 const ctx=vm.createContext({window:{dispatchEvent(){}},CustomEvent:class{},chatTermSessions:[],chatTermApplyState:x=>x,
  document:{querySelectorAll:sel=>sel.includes('terminal-state-dot')?[dot]:[]},terminalStates:new Map(),terminalEventsConnected:true,
  renderChatRail:()=>{calls.rail++;return railResult;},chatTermSyncOpen:()=>{calls.head++;return true;},terminalPaintRunBadge:()=>{},
  terminalStateLabel:ob=>ob.agentState,fmtWhen:t=>'at '+t});
 vm.runInContext(slice('let terminalStateSignature','\n// chatTermSection'),ctx);
 const tick=(patch)=>{ctx.terminalStates=new Map([['s1',Object.assign({manifestId:'s1',agentState:'idle',connectivity:'connected',process:'running',observedAt:'t1',revision:1},patch)]]);ctx.terminalStateRepaint();};
 tick({});assert.deepEqual(calls,{rail:1,head:1},'first snapshot paints');
 tick({observedAt:'t2',revision:2});assert.deepEqual(calls,{rail:1,head:1},'observedAt/revision alone must not rebuild the rail and head');
 assert.equal(dot.title,'agent idle · at t2','the state dot tooltip still follows the observed time');
 tick({observedAt:'t3',agentState:'working'});assert.deepEqual(calls,{rail:2,head:2},'a real state change repaints');
 railResult=false;tick({observedAt:'t4',agentState:'blocked'});assert.deepEqual(calls,{rail:3,head:3});
 railResult=true;tick({observedAt:'t5',agentState:'blocked'});assert.deepEqual(calls,{rail:4,head:4},'a repaint declined mid-interaction is retried on the next tick');
 tick({observedAt:'t6',agentState:'blocked'});assert.deepEqual(calls,{rail:4,head:4},'…and only once');
 ctx.terminalEventsConnected=false;tick({observedAt:'t7',agentState:'blocked'});assert.deepEqual(calls,{rail:5,head:5},'connectivity loss repaints');
 console.log('PASS: a state tick that moved only observedAt/revision leaves the rail and head alone; changes and deferred repaints still paint');
}

// ---- 2. a rebuilt thread head keeps keyboard focus on the equivalent control ----
{
 const slot=node('div','chat-thread-header','',{id:'chatThreadHeader'});
 const textarea=node('textarea','chat-input');
 const ctx=vm.createContext({document:{getElementById:id=>id==='chatTranscript'?{}:id==='chatThreadHeader'?slot:null,get activeElement(){return doc.activeElement;}},
  queueMicrotask:()=>{},chatMarkViewed:()=>{},el:()=>{throw new Error('slot exists');}});
 vm.runInContext(slice('function chatFocusKey','\nconst chatDrafts'),ctx);
 const head=()=>{const h=node('div','sprt-head chat-head');h.append(node('span','sprt-title chat-head-title','claude'),node('button','sprt-quiet chat-terminal-view chat-icon-button','',{attrs:{'aria-label':'Terminal'}}),node('button','sprt-quiet sprt-delete chat-stop-agent','Stop'),node('details','chat-details').append(node('summary','','···')));return h;};
 ctx.chatMountHeader(head());
 doc.activeElement=textarea;ctx.chatMountHeader(head());assert.equal(doc.activeElement,textarea,'a repaint never steals focus from the composer');
 const second=head();ctx.chatMountHeader(second);second.querySelector('.chat-terminal-view').focus();
 const third=head();ctx.chatMountHeader(third);
 assert.equal(doc.activeElement,third.querySelector('.chat-terminal-view'),'focus moves to the rebuilt Terminal button');
 third.querySelector('summary').focus();const fourth=head();ctx.chatMountHeader(fourth);
 assert.equal(doc.activeElement,fourth.querySelector('summary'),'focus moves to the rebuilt ··· summary');
 console.log('PASS: the thread head keeps keyboard focus across a live-state repaint');
}

// ---- 3. the rail declines a repaint while a row menu is open and keeps row focus otherwise ----
{
 const host=node('div','chat-rail','',{id:'chatRail'});
 const rows=node('div','chat-inbox-rows','',{id:'chatInboxRows'});
 const buildRow=key=>{const row=node('div','chat-rail-row','',{dataset:{inboxKey:key},attrs:{tabindex:'0'}});const menu=node('details','chat-row-menu');menu.append(node('summary','','⋯',{attrs:{'aria-label':'Conversation actions'}}));row.append(menu);return row;};
 let paints=0;
 const ctx=vm.createContext({document:{getElementById:id=>id==='chatRail'?host:null,get activeElement(){return doc.activeElement;}},
  renderChatInboxRows:()=>{paints++;rows.replaceChildren(buildRow('terminal:claude/a'),buildRow('terminal:claude/b'));},
  chatRenderWorkstreamFilter:()=>{},chatInstallPaneResize:()=>{}});
 vm.runInContext(slice('function chatFocusKey','\n// Headers occupy'),ctx);
 vm.runInContext(slice('function renderChatRail()','\n// shortModel'),ctx);
 host.append(node('div','chat-inbox-controls'),rows);rows.replaceChildren(buildRow('terminal:claude/a'),buildRow('terminal:claude/b'));
 doc.activeElement=null;
 const menu=rows.children[1].querySelector('.chat-row-menu');menu.open=true;
 assert.equal(ctx.renderChatRail(),false);assert.equal(paints,0,'an open ⋯ menu must not be rebuilt under the pointer');
 menu.open=false;
 const summary=rows.children[1].querySelector('summary');summary.focus();
 assert.equal(ctx.renderChatRail(),true);assert.equal(paints,1);
 assert.notEqual(doc.activeElement,summary,'the rows were rebuilt');
 assert.equal(doc.activeElement,rows.children[1].querySelector('summary'),'focus lands on the same row\'s rebuilt ⋯ control');
 rows.children[0].focus();ctx.renderChatRail();assert.equal(doc.activeElement,rows.children[0],'a focused row (tabindex) stays focused');
 console.log('PASS: the rail keeps an open row menu and row focus across a live-state repaint');
}
