'use strict';
(()=>{
 const gate=document.getElementById('gate'),host=document.getElementById('package'),form=document.getElementById('unlock'),status=document.getElementById('status');
 const base=location.pathname,endpoint=base+'underwriting';let dispose;
 async function open(){
  const r=await fetch(endpoint,{cache:'no-store'});
  if(!r.ok){if(r.status===401)return false;throw Error(await r.text());}
  await r.json();gate.hidden=true;host.hidden=false;
  dispose=renderDealDiligence(host,'',{endpoint,hideBack:true});return true;
 }
 form.onsubmit=async e=>{e.preventDefault();const button=form.querySelector('button');button.disabled=true;status.textContent='Opening…';try{const r=await fetch(base+'unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:form.password.value})});if(!r.ok)throw Error(await r.text());form.reset();await open();status.textContent='';}catch(e){status.textContent=e.message;}finally{button.disabled=false;}};
 open().catch(e=>status.textContent=e.message);
 // Remove previously rendered financial data when the share/session stops working.
 const expiry=setInterval(async()=>{if(host.hidden||document.hidden)return;try{const r=await fetch(endpoint,{cache:'no-store'});if(r.status===401||r.status===404){dispose?.();host.replaceChildren();host.hidden=true;gate.hidden=false;status.textContent=await r.text();}}catch{}},15000);
 window.addEventListener('pagehide',()=>{clearInterval(expiry);dispose?.();});
})();
