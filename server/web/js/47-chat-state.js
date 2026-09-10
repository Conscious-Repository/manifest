// Owner-only draft synchronization. Persistence cannot execute a message.
function chatStateEqual(a,b) {
  const stable=v=>Array.isArray(v)?v.map(stable):v&&typeof v==="object"?Object.fromEntries(Object.keys(v).sort().map(k=>[k,stable(v[k])])):v;
  return JSON.stringify(stable(a))===JSON.stringify(stable(b));
}
class ChatDraftState {
  constructor(key,changed,slot="draft") {
    this.slot=slot;this.storageKey="manifest.chatDraft.v1."+key+(slot==="draft"?"":"."+slot);
    this.key=key;this.changed=changed;this.revision=0;this.base=null;this.value=null;
    this.dirty=false;this.loaded=false;this.conflict=null;this.error="";this.pending=null;this.timer=null;
    try {
      const saved=JSON.parse(localStorage.getItem(this.storageKey)||"null");
      if(saved && Number.isSafeInteger(saved.revision) && saved.revision>=0){
        this.revision=saved.revision;this.base=saved.base;this.value=saved.value;this.dirty=!chatStateEqual(this.value,this.base);
      }
    } catch(e) {}
  }
  url(){return "/api/chat/state/"+encodeURIComponent(this.key)+"/"+encodeURIComponent(this.slot);}
  publish(apply=false){
    try { localStorage.setItem(this.storageKey,JSON.stringify({revision:this.revision,base:this.base,value:this.value})); }
    catch(e){this.error="Local draft recovery is unavailable. Keep this tab open until the draft is saved.";}
    this.changed?.(this,apply);
  }
  snapshot(v){
    if(!v || v.key!==this.key || v.slot!==this.slot || !Number.isSafeInteger(v.revision) || v.revision<0 || !(v.value===null || (typeof v.value==="object"&&!Array.isArray(v.value))))throw new Error("Invalid draft response");
    return v;
  }
  async refresh(){
    if(this.pending)return this.pending;
    try {
      const r=await fetch(this.url(),{cache:"no-store"});if(!r.ok)throw new Error("Draft sync unavailable");
      const remote=this.snapshot(await r.json());
      if(this.pending || remote.revision<this.revision)return this;
      if(this.dirty && remote.revision!==this.revision && !chatStateEqual(remote.value,this.value))this.conflict=remote;
      else {
        this.revision=remote.revision;this.base=remote.value;
        if(!this.dirty || chatStateEqual(this.value,remote.value)){this.value=remote.value;this.dirty=false;}
        this.conflict=null;
      }
      this.error="";this.loaded=true;this.publish(!this.dirty);
      if(this.dirty&&!this.conflict)this.schedule();
    }catch(e){this.error="Draft saved on this device; sync is unavailable.";this.publish();}
    return this;
  }
  set(value){
    if(chatStateEqual(value,this.value))return;
    this.value=JSON.parse(JSON.stringify(value));this.dirty=!chatStateEqual(this.value,this.base);
    this.publish();this.schedule();
  }
  schedule(){clearTimeout(this.timer);this.timer=setTimeout(()=>this.flush(),600);}
  async flush(){
    clearTimeout(this.timer);this.timer=null;
    if(this.pending)return this.pending;
    if(!this.dirty)return true;
    if(this.conflict)return false;
    const value=this.value,revision=this.revision;
    const job=Promise.resolve().then(async()=>{
      try {
        const body=JSON.stringify({revision,value});
        const r=await fetch(this.url(),{method:"PUT",headers:{"Content-Type":"application/json"},body,keepalive:body.length<15000});
        if(r.status===409){this.conflict=this.snapshot(await r.json());return false;}
        if(!r.ok)throw new Error("Draft sync unavailable");
        const saved=this.snapshot(await r.json());
        if(saved.revision<revision || !chatStateEqual(saved.value,value))throw new Error("Invalid draft acknowledgement");
        this.revision=saved.revision;this.base=saved.value;
        this.dirty=!chatStateEqual(this.value,saved.value);this.error="";this.loaded=true;
        return true;
      }catch(e){this.error="Draft saved on this device; sync is unavailable.";return false;}
    });
    this.pending=job;
    const ok=await job;
    this.pending=null;this.publish();
    if(ok&&this.dirty)this.schedule();return ok;
  }
  async resolve(useSaved){
    if(!this.conflict)return;
    const remote=this.conflict;this.conflict=null;this.revision=remote.revision;this.base=remote.value;
    if(useSaved)this.value=remote.value;
    this.dirty=!chatStateEqual(this.value,this.base);this.publish(useSaved);
    if(this.dirty)await this.flush();
  }
  clearSent(value){
    if(!chatStateEqual(this.value,value))return false;
    this.set({...value,text:"",files:[],...(Array.isArray(value?.mentions)?{mentions:[]}: {})});return true;
  }
  async reconcileSent(value){
    // Never discard an acknowledgement while an older draft write is in flight.
    if(this.pending)await this.pending;
    await this.refresh();
    if(this.error||this.conflict||!this.loaded)return false;
    this.clearSent(value); // Exact match only: newer typing belongs to the owner.
    if(this.dirty&&!await this.flush())return false;
    return !this.error&&!this.conflict&&!chatStateEqual(this.base,value);
  }
}
