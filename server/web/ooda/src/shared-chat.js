/* Shared conversation state for both portals. Native transcripts remain in
   their original sessions; only the selected conversation is polled. */
(function () {
  async function request(path, body) {
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 20000);
    try {
      const response = await fetch(path, {credentials:'same-origin', cache:'no-store', signal:controller.signal,
        ...(body === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})});
      if (response.status === 401 || response.redirected) throw Error('Sign in again to continue this conversation.');
      if (!response.ok) throw Error((await response.text()).slice(0,500) || 'Could not load this conversation.');
      return await response.json();
    } finally { clearTimeout(timeout); }
  }
  function useThread(thread, stored, identity) {
    const id = thread && thread.id || '', shared = !!(thread && thread.sharedSource && !thread.archived);
    const [snapshot,setSnapshot] = React.useState(null), [error,setError] = React.useState('');
    const [recipients,setRecipients] = React.useState({}), [pending,setPending] = React.useState(null), [recovering,setRecovering] = React.useState(false);
    const [selections,setSelections]=React.useState({}),[preview,setPreview]=React.useState(null);
    const [showScreen,setShowScreen]=React.useState(false),[screen,setScreen]=React.useState(null);
    const generation = React.useRef(0), latest = React.useRef(0), fileRequest=React.useRef(0);
    const storage = 'manifest.shared-input.v1.' + (identity && identity.email || 'member') + '.' + id;
    const refresh = React.useCallback(async () => {
      if (!shared) return;
      const scope = generation.current, seq = ++latest.current;
      try {
        const value = await request('/api/chat/threads/'+encodeURIComponent(id)+'/conversation');
        if (scope === generation.current && seq === latest.current) {setSnapshot({id,value});setError('');}
      } catch(e) {if(scope === generation.current && seq === latest.current)setError(e.message || 'Connection interrupted.');}
    },[id,shared]);
    React.useEffect(() => {
      generation.current++;setSnapshot(null);setError('');setPending(null);setRecovering(false);setPreview(null);setShowScreen(false);setScreen(null);
      if (!shared) return;
      try {const value=JSON.parse(localStorage.getItem(storage)||'null');if(value && value.thread===id)setPending(value);}catch(e){setError('Saved delivery could not be read. Do not resend until its status is checked.');}
      refresh();
      const timer=setInterval(()=>{if(document.visibilityState==='visible')refresh();},3500);
      return ()=>{generation.current++;clearInterval(timer);};
    },[id,shared,storage,refresh]);
    const value=snapshot && snapshot.id===id ? snapshot.value : null;
    const terminals=value && value.terminals || [];
    const recipient=recipients[id] || (value && !terminals.length ? 'team' : '');
    const native=terminals.find(t=>t.id===recipient);
    const terminalID=native?.id||'';
    React.useEffect(()=>{
      setScreen(null);
      if(!shared||!showScreen||!terminalID)return;
      let active=true,timer;
      async function poll(){
        if(!active)return;
        if(document.visibilityState==='visible'){
          try{const value=await request('/api/chat/threads/'+encodeURIComponent(id)+'/terminals/'+encodeURIComponent(terminalID)+'/screen');if(active)setScreen({thread:id,terminal:terminalID,value});}
          catch(e){if(active)setScreen({thread:id,terminal:terminalID,error:e.message||'Screen unavailable.'});}
        }
        if(active)timer=setTimeout(poll,2000);
      }
      poll();return()=>{active=false;clearTimeout(timer);};
    },[shared,showScreen,id,terminalID]);
    const currentScreen=screen?.thread===id&&screen?.terminal===terminalID?screen:null;
    const files=value && value.files || [], selected=selections[id]||[];
    const save=value=>{if(value)localStorage.setItem(storage,JSON.stringify(value));else localStorage.removeItem(storage);setPending(value);};
    async function submit(saved) {
      const scope=generation.current;
      const result=await request('/api/chat/threads/'+encodeURIComponent(id)+'/terminals/'+encodeURIComponent(saved.terminal)+'/input',{...(saved.key?{key:saved.key}:{text:saved.text}),...(saved.files?.length?{files:saved.files}:{}),requestId:saved.requestId});
      if(!result.delivery || result.delivery.id!==saved.requestId || result.delivery.state!=='sent')throw Error('Delivery is unconfirmed. Check the terminal before sending anything else; retrying this saved message checks the same receipt.');
      const current=JSON.parse(localStorage.getItem(storage)||'null');
      if(current && current.requestId===saved.requestId)localStorage.removeItem(storage);
      if(scope===generation.current){setPending(current && current.requestId!==saved.requestId ? current : null);setSelections(all=>({...all,[id]:[]}));refresh();}
      return saved.text;
    }
    async function send(text,contextFiles=[]) {
      if(!value || !recipient)throw Error('Choose the agent for this message.');
      if(!native)throw Error('Choose a shared terminal agent.');
      const hashes=[...new Set([...selected,...contextFiles])].sort();
      let saved=JSON.parse(localStorage.getItem(storage)||'null');
      if(saved && (saved.thread!==id || saved.terminal!==native.id || saved.text!==text || saved.key || JSON.stringify(saved.files||[])!==JSON.stringify(hashes)))throw Error('A previous message still needs confirmation. Resolve the saved message first.');
      if(!saved){saved={thread:id,terminal:native.id,text,...(hashes.length?{files:hashes}:{}),requestId:crypto.randomUUID()};save(saved);}
      return submit(saved);
    }
    async function keypress(key,label){
      if(!native||recovering)return;
      const scope=generation.current;setRecovering(true);
      try{
        if(localStorage.getItem(storage))throw Error('Resolve the pending message or key before sending another control.');
        const saved={thread:id,terminal:native.id,key,label,requestId:crypto.randomUUID()};save(saved);await submit(saved);
      }catch(e){if(scope===generation.current)setError(e.message);}
      finally{if(scope===generation.current)setRecovering(false);}
    }
    async function openFile(file){
      const scope=generation.current,seq=++fileRequest.current;
      const href='/api/chat/attach/'+encodeURIComponent(file.hash);
      setPreview({file,href,loading:true});
      try{
        if(/\.(pdf|png|jpe?g|webp|gif)$/i.test(file.name)){setPreview({file,href,embedded:true});return;}
        if(file.size>256000||!(/\.(txt|md|markdown|csv|tsv|json|ya?ml|log|js|jsx|ts|tsx|py|go|css|html|xml|sql|sh)$/i.test(file.name))){setPreview({file,href,note:'Open or download this file to view its complete contents.'});return;}
        const response=await fetch(href,{credentials:'same-origin'});
        if(!response.ok)throw Error('File could not load.');
        const text=await response.text();
        if(scope===generation.current&&seq===fileRequest.current)setPreview({file,href,text});
      }catch(e){if(scope===generation.current&&seq===fileRequest.current)setPreview({file,href,note:e.message});}
    }
    function teamContext(context=[]) {
      if(!shared)return context.slice();
      if(pending)throw Error('Resolve the pending terminal delivery before sending another message.');
      return [...new Set([...context,...selected.map(hash=>'file/'+hash)])];
    }
    function filesConfirmed() {
      // Clear only the versions included in this send, in its original thread.
      // A later selection or navigation must not be reset by an old response.
      setSelections(all=>({...all,[id]:(all[id]||[]).filter(hash=>!selected.includes(hash))}));
    }
    const [plan,setPlan]=React.useState(null), [planBusy,setPlanBusy]=React.useState(false), [planError,setPlanError]=React.useState('');
    const planSeq=React.useRef(0);
    const planKey=pid=>'manifest.shared-plan.v1.'+(identity?.email||'member')+'.'+id+'.'+pid;
    const currentPlan=plan?.thread===id?plan:null;
    React.useEffect(()=>{planSeq.current++;setPlan(null);setPlanBusy(false);setPlanError('');},[id,shared]);
    function keepDraft(next){
      try{localStorage.setItem(planKey(next.id),JSON.stringify({draft:next.draft,expected:next.expected,uncertain:!!next.uncertain}));return true;}
      catch(e){setPlanError('Draft could not be stored on this device. Copy your text before leaving.');return false;}
    }
    async function openPlan(pid,revision){
      const scope=generation.current,seq=++planSeq.current;
      setPlanBusy(true);setPlanError('');
      try{
        const data=await request('/api/chat/threads/'+encodeURIComponent(id)+'/plans/'+encodeURIComponent(pid)+(revision?'?revision='+encodeURIComponent(revision):''));
        if(scope!==generation.current||seq!==planSeq.current)return;
        const saved=JSON.parse(localStorage.getItem(planKey(pid))||'null');
        setPlan({thread:id,...data,draft:saved?.draft??data.content,expected:saved?.expected??data.head,uncertain:!!saved?.uncertain});
      }catch(e){if(scope===generation.current&&seq===planSeq.current)setPlanError(e.message||'Plan could not load. Your saved draft is retained.');}
      finally{if(scope===generation.current&&seq===planSeq.current)setPlanBusy(false);}
    }
    async function savePlan(){
      if(!currentPlan||planBusy||currentPlan.uncertain)return;
      const p=currentPlan,scope=generation.current,seq=++planSeq.current;
      // Persist uncertainty BEFORE the request: navigation or a lost response
      // must never silently turn a save into an automatic retry.
      if(!keepDraft({...p,uncertain:true}))return;
      setPlan({...p,uncertain:true});setPlanBusy(true);setPlanError('');
      try{
        const data=await request('/api/chat/threads/'+encodeURIComponent(id)+'/plans/'+encodeURIComponent(p.id),{content:p.draft,expectedRevision:p.expected});
        // A successful response may show a later external edit. Keep the draft
        // until its normalized bytes match the authoritative head.
        if(data.content.trim()!==p.draft.trim())throw Error('The working plan changed again. Reload and compare your retained draft.');
        const retained=JSON.parse(localStorage.getItem(planKey(p.id))||'null');
        if(retained?.draft===p.draft&&retained?.expected===p.expected)localStorage.removeItem(planKey(p.id));
        if(scope===generation.current&&seq===planSeq.current){setPlan({thread:id,...data,draft:data.content,expected:data.head,uncertain:false});setPlanError('Saved to the original plan.');refresh();}
      }catch(e){if(scope===generation.current&&seq===planSeq.current)setPlanError((e.message||'Save not confirmed.')+' Your draft is retained. Reload current and compare before saving again.');}
      finally{if(scope===generation.current&&seq===planSeq.current)setPlanBusy(false);}
    }
    function planPanel(){
      const p=currentPlan,h=React.createElement;
      if(!p)return null;
      const dirty=p.draft!==p.content;
      return h('section',{className:'shared-plan-panel','aria-label':'Original plan editor'},
        h('h2',null,'Plan · original working plan'),
        h('button',{type:'button',onClick:()=>{planSeq.current++;setPlan(null);setPlanBusy(false);}},'Return to conversation'),
        h('p',null,'Edits update the original plan for everyone. Saving creates a reversible version and does not run the plan.'),
        h('label',null,'Version ',h('select',{'aria-label':'Plan version',value:p.revision,disabled:planBusy,onChange:e=>openPlan(p.id,e.target.value)},...p.versions.map(v=>h('option',{key:v.n,value:v.hash},'Version '+v.n+(v.hash===p.head?' · current':'')+' · '+v.actor)))),
        h('pre',{'aria-label':'Selected plan bytes',style:{whiteSpace:'pre-wrap',overflowWrap:'anywhere',maxHeight:'24vh',overflow:'auto'}},p.content),
        h('button',{type:'button',disabled:planBusy||!!pending,onClick:()=>{setSelections(all=>({...all,[id]:[...new Set([...(all[id]||[]),p.file.hash])]}));setPlan(null);setPlanError('Selected exact plan version for your next message.');}},'Discuss this version'),
        p.revision!==p.head&&h('button',{type:'button',disabled:planBusy,onClick:()=>{const next={...p,draft:p.content};if(keepDraft(next))setPlan(next);}},'Restore as new version'),
        h('label',{style:{display:'block',marginTop:16}},'Edit original plan',h('textarea',{'aria-label':'Edit original plan',value:p.draft,disabled:planBusy,onChange:e=>{const next={...p,draft:e.target.value};setPlan(next);keepDraft(next);},style:{display:'block',width:'100%',boxSizing:'border-box',minHeight:240}})),
        h('details',null,h('summary',null,'Compare selected version and draft'),h('p',null,'Selected version is shown above. Draft to save:'),h('pre',{style:{whiteSpace:'pre-wrap',overflowWrap:'anywhere'}},p.draft)),
        (p.expected!==p.head||p.uncertain)&&h('p',{role:'status'},'Review required. Your draft may differ from the current original. Reload current to check whether it saved.'),
        h('button',{type:'button',disabled:planBusy,onClick:()=>openPlan(p.id)},'Reload current'),
        h('button',{type:'button',disabled:planBusy||p.revision!==p.head,onClick:()=>{const next={...p,expected:p.head,uncertain:false};if(keepDraft(next))setPlan(next);}},'Use current as save base'),
        h('button',{type:'button',disabled:planBusy||p.uncertain||p.expected!==p.head,onClick:savePlan},planBusy?'Saving…':'Save new version'),
        h('button',{type:'button',disabled:planBusy,onClick:()=>{localStorage.removeItem(planKey(p.id));setPlan({...p,draft:p.content,expected:p.head,uncertain:false});}},'Discard draft'),
        dirty&&h('p',null,'Draft retained on this device.'),
        planError&&h('p',{role:'alert'},planError));
    }

    function controls(team, classes, onConfirmed) {
      if(!shared)return null;
      const h=React.createElement;
      return h('div',{style:{padding:'10px 0',borderBottom:'1px solid var(--line, #38505c)',overflowWrap:'anywhere'}},
        h('style',null,'@media(min-width:1000px){.shared-plan-open{margin-right:min(42vw,644px)}}.shared-plan-panel{position:fixed;z-index:100;right:12px;top:70px;bottom:12px;width:min(40vw,620px);box-sizing:border-box;overflow:auto;padding:20px;background:var(--bg,#16232c);color:var(--ink,#eee);border:1px solid var(--line,#567)}.shared-plan-panel button{min-height:44px;margin:4px}.shared-plan-panel select{max-width:100%}@media(max-width:999px){.shared-plan-panel{inset:0;width:100%;padding:16px}}'),
        ...(value?.plans||[]).map((p,i)=>h('button',{key:p.id,type:'button',disabled:planBusy,onClick:()=>openPlan(p.id)},'Edit original plan'+(value.plans.length>1?' '+(i+1):''))),
        !currentPlan&&planError&&h('p',{role:'status'},planError),
        planPanel(),
        h('label',null,'To ',h('select',{className:classes,value:recipient,disabled:recovering,onChange:e=>setRecipients(current=>({...current,[id]:e.target.value})), 'aria-label':'Message recipient'},
          h('option',{value:''},value?'Choose agent…':'Loading agents…'),
          h('option',{value:'team'},team+' · team agent'),
          ...terminals.map(t=>h('option',{key:t.id,value:t.id},t.agent+(t.model?' · '+t.model:'')+' · '+t.id.slice(-6))))),
        native && h('span',{style:{marginLeft:10,fontSize:12}},native.agentState==='not-started'?'Ready to start':native.process==='running'?'Running':native.process==='stopped'?'Stopped · resumes on send':'Status unavailable'),
        native && h('details',{key:id+'/'+native.id,open:showScreen,onToggle:e=>setShowScreen(e.currentTarget.open)},h('summary',null,'Terminal controls'),
          showScreen && (currentScreen?.error?h('p',{role:'status'},currentScreen.error):!currentScreen?h('p',{role:'status'},'Loading screen…'):h('div',null,
            h('p',{role:'status'},currentScreen.value.process==='not-started'?'Send a message to start this agent.':currentScreen.value.live?'Live terminal screen':currentScreen.value.process==='stopped'?'Terminal stopped. Send a message to resume.':'Terminal is reconnecting…'),
            currentScreen.value.lines?.length>0&&h('pre',{'aria-label':'Terminal screen',style:{whiteSpace:'pre',overflow:'auto',maxWidth:'100%',maxHeight:'45vh',padding:'10px 0',fontSize:13,lineHeight:1.4}},currentScreen.value.lines.join('\n')))),
          ...[['Escape','\x1b'],['Enter','\r'],['↑','\x1b[A'],['↓','\x1b[B'],['Interrupt','\x03']].map(([label,key])=>h('button',{key,type:'button',disabled:recovering||!!pending,onClick:()=>keypress(key,label),style:{minHeight:44,minWidth:44,margin:4},'aria-label':'Terminal '+label},label))),
        files.length>0 && h('details',null,h('summary',null,'Files'+(selected.length?' · '+selected.length+' selected':'')),
          ...files.map(file=>h('div',{key:file.hash,style:{display:'flex',gap:8,alignItems:'center',flexWrap:'wrap',padding:'6px 0'}},
            h('label',null,h('input',{type:'checkbox',checked:selected.includes(file.hash),disabled:!!pending||recovering,onChange:e=>setSelections(all=>({...all,[id]:e.target.checked?[...selected,file.hash]:selected.filter(hash=>hash!==file.hash)}))}),' Discuss'),
            h('button',{type:'button',onClick:()=>openFile(file),style:{maxWidth:'100%',whiteSpace:'normal',overflowWrap:'anywhere',textAlign:'left'}},file.name)))),
        preview && h('section',{style:{padding:'12px 0'}},h('strong',null,preview.file.name),
          h('button',{type:'button',onClick:()=>{fileRequest.current++;setPreview(null);},style:{marginLeft:12}},'Close file'),
          h('a',{href:preview.href,target:'_blank',rel:'noopener',style:{marginLeft:12}},'Open file'),
          preview.loading?h('p',null,'Loading file…'):preview.embedded?h('iframe',{title:preview.file.name,src:preview.href,style:{display:'block',width:'100%',height:350,border:0},sandbox:''}):preview.note?h('p',null,preview.note):h('pre',{style:{whiteSpace:'pre-wrap',overflowWrap:'anywhere',maxHeight:350,overflow:'auto'}},preview.text)),
        error && h('div',{role:'alert'},error),
        ...(value && value.warnings || []).map((warning,i)=>h('div',{key:i,role:'status'},warning)),
        pending && h('details',null,h('summary',null,pending.key?'Terminal control awaiting confirmation':'Message awaiting confirmation'),h('p',{style:{whiteSpace:'pre-wrap'}},pending.label||pending.key||pending.text),
          h('button',{type:'button',disabled:recovering,onClick:async()=>{const scope=generation.current;setRecovering(true);try{const sent=await submit(pending);if(scope===generation.current && onConfirmed && !pending.key)onConfirmed(sent);}catch(e){if(scope===generation.current)setError(e.message);}finally{if(scope===generation.current)setRecovering(false);}}},recovering?'Checking…':pending.key?'Check saved control':'Retry saved message')));
    }
    return {planOpen:!!currentPlan,shared,ready:!shared||!!value,messages:value?value.messages:stored,terminals,recipient,native,send,refresh,controls,teamContext,filesConfirmed};
  }
  window.SHARED_CHAT={useThread};
})();
