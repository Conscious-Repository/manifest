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
    function controls(team, classes, onConfirmed) {
      if(!shared)return null;
      const h=React.createElement;
      return h('div',{style:{padding:'10px 0',borderBottom:'1px solid var(--line, #38505c)',overflowWrap:'anywhere'}},
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
        native && files.length>0 && h('details',null,h('summary',null,'Files'+(selected.length?' · '+selected.length+' selected':'')),
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
    return {shared,ready:!shared||!!value,messages:value?value.messages:stored,terminals,recipient,native,send,refresh,controls};
  }
  window.SHARED_CHAT={useThread};
})();
