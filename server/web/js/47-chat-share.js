// Owner-only sharing review. Nothing is published by opening this dialog.
// A saved confirmation keeps its request identity through reloads and lost ACKs.
const CHAT_SHARE = (() => {
  const node=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!=null)e.textContent=text;return e;};
  const validRequest=v=>v&&typeof v.requestId==="string"&&/^[a-f0-9]{64}$/.test(v.revision||"");
  async function read(url,options){
    const response=await fetch(url,{cache:"no-store",...options});
    const text=await response.text();let value;
    try{value=JSON.parse(text);}catch{}
    if(!response.ok)throw new Error(value?.error||text||`Request failed (${response.status})`);
    return value;
  }
  function open({agent,id,title,onShared}){
    if(!["kairos-private","zeck-private"].includes(agent))return;
    const base=`/api/agents/chat/${encodeURIComponent(agent)}/sessions/${encodeURIComponent(id)}`;
    const key=`manifest.chat-share.v1.${agent}.${id}`;
    const audience=agent==="zeck-private"?"OODA team":"AION team";
    const dialog=node("dialog","chat-workstream-dialog chat-share-dialog");
    const heading=node("h3","",`Share with ${audience}`);heading.id="chat-share-title";
    dialog.setAttribute("aria-labelledby",heading.id);
    const body=node("div","chat-share-body"),status=node("p","chat-share-status","Loading review…");status.setAttribute("role","status");
    const actions=node("div","chat-workstream-actions"),close=node("button","sprt-quiet","Close"),refresh=node("button","sprt-quiet","Refresh review"),confirm=node("button","","Share conversation");
    close.type=refresh.type=confirm.type="button";confirm.disabled=true;
    actions.append(close,refresh,confirm);dialog.append(heading,node("p","",title||id),body,status,actions);
    let review=null,receipt=null,pending=null,busy=false,epoch=0,closed=false,ack=null;
    try{const value=JSON.parse(localStorage.getItem(key)||"null");if(validRequest(value))pending=value;}catch{}
    const save=value=>{localStorage.setItem(key,JSON.stringify(value));if(localStorage.getItem(key)!==JSON.stringify(value))throw new Error("Could not save this confirmation. Enable browser storage before sharing.");pending=value;};
    const clear=()=>{try{localStorage.removeItem(key);}catch{}pending=null;};
    function controls(){
      refresh.disabled=busy;
      confirm.textContent=receipt?.state==="shared"?"Open team conversation":pending||receipt?.state==="prepared"?"Retry confirmed share":"Share conversation";
      confirm.disabled=busy||!review||(!pending&&receipt?.state!=="shared"&&(!(ack?.checked)||(review.blockers||[]).length>0));
    }
    function section(label,text){const d=node("details","chat-share-section");d.append(node("summary","",label),node("pre","",text));body.append(d);}
    function render(){
      body.replaceChildren();ack=null;
      body.append(node("p","","The team will see the full history, attached files and future messages. Teammates can also direct the terminal agents listed below."));
      if(receipt?.state==="prepared")body.append(node("p","","Sharing was confirmed but has not finished. Retry to complete the same share; the private conversation is paused until recovery finishes."));
      if(receipt?.state==="shared")body.append(node("p","","This conversation is shared with the team."));
      if(!review){controls();return;}
      const count=(items,label)=>`${(items||[]).length} ${label}${(items||[]).length===1?"":"s"}`;
      body.append(node("p","chat-share-counts",[count(review.timeline,"turn"),count(review.files,"file"),count(review.continuations,"terminal session")].join(" · ")));
      for(const v of review.continuations||[])body.append(node("p","chat-share-runtime",`${v.agent}${v.model?" · "+v.model:""} · ${v.id.slice(-6)}`));
      if((review.files||[]).length){
        const files=node("ul","chat-share-files");
        for(const file of review.files){const li=node("li",""),a=node("a","",file.name);a.target="_blank";a.rel="noopener";
          a.href=file.artifactId?`/api/artifacts/content?id=${encodeURIComponent(file.artifactId)}&rev=${encodeURIComponent(file.hash)}`:`/api/tasks/thread/file/${encodeURIComponent(file.hash)}?id=agentchat`;
          li.append(a);files.append(li);
        }body.append(files);
      }
      section("Review full conversation",(review.timeline||[]).map(t=>`${t.who||"Message"}${t.ts?" · "+t.ts:""}\n${t.text||JSON.stringify(t,null,2)}`).join("\n\n")||review.body||"No messages yet.");
      // Exact envelope includes origin context and original tool records omitted
      // from the readable transcript; no source data is hidden from review.
      section("Full sharing record",JSON.stringify(review,null,2));
      for(const blocker of review.blockers||[])body.append(node("p","chat-share-blocker",blocker));
      if(!pending&&receipt?.state!=="shared"){
        const label=node("label","chat-share-consent");ack=node("input","");ack.type="checkbox";
        label.append(ack,document.createTextNode(`Share this conversation and future messages with the ${audience}, including control of its listed terminal agents.`));ack.onchange=controls;body.append(label);
      }
      controls();
    }
    async function load(){
      const turn=++epoch;busy=true;controls();status.textContent="Checking sharing status…";
      try{
        const state=await read(base+"/share");if(closed||turn!==epoch)return;
        receipt=state;
        if(["prepared","shared"].includes(state.state)){
          if(!validRequest(state))throw new Error("The sharing receipt is incomplete. Reload before continuing.");
          if(state.state==="prepared")save({requestId:state.requestId,revision:state.revision});else clear();
        }
        const next=await read(base+"/share-review");if(closed||turn!==epoch)return;
        if(next.session?.agent!==agent||next.session?.id!==id||!next.futureMessages)throw new Error("The review does not match this conversation. Reload before sharing.");
        review=next;
        // An authoritative private status and changed review means the old
        // attempt never fenced this source. Require review and consent anew.
        if(state.state==="private"&&pending&&pending.revision!==review.revision)clear();
        status.textContent=state.state==="shared"?"Sharing complete.":pending?"Your confirmation is saved. Retry uses the same reviewed version.":"Review what will become visible before sharing.";
        render();
      }catch(e){review=null;status.textContent=e.message||"Could not load sharing review.";}
      finally{if(!closed&&turn===epoch){busy=false;controls();}}
    }
    confirm.onclick=async()=>{
      if(busy||!review)return;
      if(receipt?.state==="shared"){const route=receipt.conversation?.route;if(route){dialog.close();onShared?.(receipt.conversation);}return;}
      if(!pending){
        if(!ack?.checked||(review.blockers||[]).length)return;
        try{save({requestId:crypto.randomUUID(),revision:review.revision});}catch(e){status.textContent=e.message;return;}
      }
      busy=true;controls();status.textContent="Sharing conversation…";
      try{
        const result=await read(base+"/share",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(pending)});
        if(closed)return;
        if(result.state!=="shared")throw new Error("Sharing is not yet complete. Check its status and retry the saved confirmation.");
        receipt=result;clear();status.textContent="Sharing complete.";render();
      }catch(e){if(!closed)status.textContent=(e.message||"Connection lost.")+" Check status before retrying; your confirmation is saved.";}
      finally{if(!closed){busy=false;refresh.textContent="Check status";controls();}}
    };
    refresh.onclick=load;close.onclick=()=>dialog.close();
    dialog.addEventListener("close",()=>{closed=true;++epoch;dialog.remove();},{once:true});
    document.body.append(dialog);dialog.showModal();load();return dialog;
  }
  return {open};
})();
