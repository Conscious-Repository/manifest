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
    const generation = React.useRef(0), latest = React.useRef(0);
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
      generation.current++;setSnapshot(null);setError('');setPending(null);setRecovering(false);
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
    const save=value=>{if(value)localStorage.setItem(storage,JSON.stringify(value));else localStorage.removeItem(storage);setPending(value);};
    async function submit(saved) {
      const scope=generation.current;
      const result=await request('/api/chat/threads/'+encodeURIComponent(id)+'/terminals/'+encodeURIComponent(saved.terminal)+'/input',{text:saved.text,requestId:saved.requestId});
      if(!result.delivery || result.delivery.id!==saved.requestId || result.delivery.state!=='sent')throw Error('Delivery is unconfirmed. Check the terminal before sending anything else; retrying this saved message checks the same receipt.');
      const current=JSON.parse(localStorage.getItem(storage)||'null');
      if(current && current.requestId===saved.requestId)localStorage.removeItem(storage);
      if(scope===generation.current){setPending(current && current.requestId!==saved.requestId ? current : null);refresh();}
      return saved.text;
    }
    async function send(text) {
      if(!value || !recipient)throw Error('Choose the agent for this message.');
      if(!native)throw Error('Choose a shared terminal agent.');
      let saved=JSON.parse(localStorage.getItem(storage)||'null');
      if(saved && (saved.thread!==id || saved.terminal!==native.id || saved.text!==text))throw Error('A previous message still needs confirmation. Resolve the saved message first.');
      if(!saved){saved={thread:id,terminal:native.id,text,requestId:crypto.randomUUID()};save(saved);}
      return submit(saved);
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
        error && h('div',{role:'alert'},error),
        ...(value && value.warnings || []).map((warning,i)=>h('div',{key:i,role:'status'},warning)),
        pending && h('details',null,h('summary',null,'Message awaiting confirmation'),h('p',{style:{whiteSpace:'pre-wrap'}},pending.text),
          h('button',{type:'button',disabled:recovering,onClick:async()=>{const scope=generation.current;setRecovering(true);try{const sent=await submit(pending);if(scope===generation.current && onConfirmed)onConfirmed(sent);}catch(e){if(scope===generation.current)setError(e.message);}finally{if(scope===generation.current)setRecovering(false);}}},recovering?'Checking…':'Retry saved message')));
    }
    return {shared,ready:!shared||!!value,messages:value?value.messages:stored,terminals,recipient,native,send,refresh,controls};
  }
  window.SHARED_CHAT={useThread};
})();
