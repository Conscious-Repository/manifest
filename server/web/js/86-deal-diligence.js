// Deal-specific diligence preview. Reads current records and a dated financing
// baseline separately; never turns screening projections into a commitment.
function diligenceStack(row, basis) {
  const development = row.acquisition + row.hardCostsIncludingContingency + row.softCosts;
  const reserve = Math.round(row.baseLoan * basis.constructionRate * basis.reserveMonths / 12 * 100) / 100;
  const equity = development - row.baseLoan;
  return {development, reserve, request: row.baseLoan + reserve, equity,
    fund: equity * basis.fundEquityShare, partners: equity * basis.partnerEquityShare,
    monthlyRent: row.units.reduce((n,u)=>n+u.rent,0)};
}
function diligenceRefinance(row, basis, assumptions) {
  if (!assumptions || ![assumptions.vacancy_rate, assumptions.opex_rate, assumptions.exit_cap_rate].every(Number.isFinite) || assumptions.exit_cap_rate <= 0) return null;
  const gross=row.units.reduce((n,u)=>n+u.rent,0)*12;
  const vacancy=gross*assumptions.vacancy_rate;
  const expenses=(gross-vacancy)*assumptions.opex_rate;
  const noi=gross-vacancy-expenses, value=noi/assumptions.exit_cap_rate;
  const repayment=diligenceStack(row,basis).request;
  const low=value*basis.refinanceLtvLow, high=value*basis.refinanceLtvHigh;
  const rate=basis.refinanceRate/12, months=basis.refinanceAmortYears*12;
  const debt=months>0 ? (rate ? repayment*rate/(1-Math.pow(1+rate,-months))*12 : repayment/months*12) : 0;
  return {gross,vacancy,expenses,noi,value,low,high,repayment,debt,dscr:debt?noi/debt:0,gap:Math.max(0,repayment-high)};
}
function diligenceOperating(p, source, assumptions, configuration = {}) {
  if(!assumptions || !['vacancy_rate','opex_rate'].every(k=>Number.isFinite(assumptions[k])))return null;
  const u=reScreen(p,source,assumptions);if(!u.complete)return null;
  const units=(p.unitMix||[]).length||p.units||reSrcNum(source,'total_units');
  const reserve=Number.isFinite(configuration.replacementReservePerUnitYear)?configuration.replacementReservePerUnitYear*units:null;
  return {...u,units,reserve,ncf:reserve===null?null:u.noi-reserve};
}
function renderDealDiligence(host, slug, options = {}) {
  const mount=document.createElement('div');host.replaceChildren(mount);
  const endpoint=options.endpoint||('/api/deals/'+encodeURIComponent(slug)+'/underwriting');
  let stopped=false,busy=false,revision='',controller;
  const dispose=()=>{stopped=true;clearInterval(timer);controller?.abort();document.removeEventListener('visibilitychange',visible);};
  const load=async()=>{
    if(stopped||busy)return;if(!mount.isConnected){dispose();return;}busy=true;
    controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),20000);
    try{
      const response=await fetch(endpoint,{cache:'no-store',credentials:'same-origin',signal:controller.signal,headers:revision?{'If-None-Match':'"'+revision+'"'}:{}});
      if(response.status===304){mount.querySelector('[data-live-status]')?.replaceChildren(document.createTextNode('Live · checked '+new Date().toLocaleTimeString()));return;}
      if(!response.ok||response.redirected)throw Error('Could not refresh underwriting ('+response.status+').');
      const bundle=await response.json();if(stopped||!mount.isConnected)return;
      const open=Array.from(mount.querySelectorAll('details[open]')).map(d=>d.querySelector('summary')?.textContent);
      const scroll=window.scrollY;
      await drawDealUnderwriting(mount,slug,{...options,endpoint,bundle,reload:load});
      mount.querySelectorAll('details').forEach(d=>{if(open.includes(d.querySelector('summary')?.textContent))d.open=true;});
      if(revision)window.scrollTo({top:scroll});revision=bundle.revision;
    }catch(error){if(stopped)return;let status=mount.querySelector('[data-live-status]');if(!status){status=document.createElement('p');status.dataset.liveStatus='';mount.append(status);}status.setAttribute('role','status');status.textContent=(revision?'Showing last loaded data. ':'')+error.message+' Retrying automatically.';}
    finally{clearTimeout(timeout);busy=false;}
  };
  const visible=()=>{if(document.visibilityState==='visible')load();};
  const timer=setInterval(()=>{if(!mount.isConnected){dispose();return;}if(document.visibilityState==='visible')load();},5000);
  document.addEventListener('visibilitychange',visible);load();return dispose;
}
async function drawDealUnderwriting(host, slug, options) {
  const el=(tag,cls='',text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;};
  const preview=el('div','diligence-preview');
  host.replaceChildren(preview); host=preview;
  const back = el('button','pp3-note',options.backLabel||'← Deal workspace');
  back.onclick=()=>options.onBack?options.onBack():renderDealPage(slug); host.append(back);
  const loading=el('p','','Loading deal records…');host.append(loading);
  const data=options.bundle;
  const read=async path=>{
    if(path==='/api/realestate/contracts')return {contracts:data.contracts};
    if(path==='/api/realestate/assumptions')return {values:data.assumptions};
    const match=/^\/api\/properties\/([^/]+)\/(docs|source)$/.exec(path);
    if(match){const id=decodeURIComponent(match[1]);if(match[2]==='docs'){if(!data.docs[id])throw Error('Document inventory unavailable');return {docs:data.docs[id]};}if(!data.sources[id])throw Error('Property source unavailable');return {source:data.sources[id]};}
    throw Error('Unsupported underwriting read');
  };
  const docLink=ref=>options.endpoint+'/document?ref='+encodeURIComponent(ref);
  loading.remove();
  const basis=data.source?.deal_underwriting||data.source?.lender_diligence||{};
  const members=data.members||[], assumptions=data.assumptions||{};
  const baseline=(basis.properties||[]).filter(r=>members.some(p=>p.slug===r.slug));
  const memberById=Object.fromEntries(members.map(p=>[p.slug,p]));
  const money=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD',minimumFractionDigits:0,maximumFractionDigits:0}):'—';
  const exactMoney=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD'}):'—';
  const paragraph=(parent,text,cls='')=>parent.append(el('p',cls,text));
  const section=(id,title)=>{const s=el('section','diligence-section');s.id='diligence-'+id;s.append(el('h3','',title));host.append(s);return s;};
  const table=(parent,headers,rows)=>{const wrap=el('div','diligence-table-wrap');wrap.tabIndex=0;wrap.setAttribute('role','region');wrap.setAttribute('aria-label',headers.join(', '));const t=el('table','diligence-table');const head=el('thead'),tr=el('tr');headers.forEach(h=>{const th=el('th','',h);th.scope='col';tr.append(th);});head.append(tr);t.append(head);const body=el('tbody');rows.forEach(row=>{const r=el('tr');row.forEach(v=>r.append(el('td','',String(v))));body.append(r);});t.append(body);wrap.append(t);parent.append(wrap);};
  const detail=(parent,title)=>{const d=el('details','diligence-property');d.append(el('summary','',title));parent.append(d);return d;};
  const operating=members.map(p=>diligenceOperating(p,data.sources[p.slug]||{},assumptions,basis.operating||{}));
  const total=key=>operating.length&&operating.every(p=>p&&Number.isFinite(p[key]))?operating.reduce((n,p)=>n+p[key],0):null;
  const expenseRows=members.flatMap(p=>(p.ledger||[]).filter(r=>r.type==='expense').map(r=>({...r,property:p.short,propertySlug:p.slug}))).sort((a,b)=>String(b.date).localeCompare(String(a.date)));
  const paid=expenseRows.reduce((n,r)=>n+r.amount,0);
  const units=total('units');
  host.append(el('p','diligence-eyebrow','OODA GROUP · REAL ESTATE'),el('h2','pp3-title',basis.title||data.deal.name),el('p','re-foot-note','Development overview & due diligence'));
  const live=el('p','re-foot-note','Live · checked '+new Date().toLocaleTimeString());live.dataset.liveStatus='';live.setAttribute('role','status');host.append(live);
  const nav=el('nav','diligence-nav');nav.setAttribute('aria-label','Diligence sections');
  [['summary','Summary'],['properties','Properties'],['budget','Development budget'],['operations','Operating proforma'],['progress','Live project records'],['documents','Documents']].forEach(([id,label])=>{const b=el('button','',label);b.onclick=()=>host.querySelector('#diligence-'+id)?.scrollIntoView({block:'start',behavior:'smooth'});nav.append(b);});host.append(nav);
  const summary=section('summary','Project overview');
  const entities=[...new Set(members.map(p=>p.entity).filter(Boolean))];
  paragraph(summary,members.length+' properties'+(units?' · '+units+' planned residences':'')+(entities.length?' · '+entities.join(', '):''),'diligence-lead');
  paragraph(summary,'A property-level view of development costs, proposed rental income, and execution progress. Operating projections use the current property records; supporting plans, contracts, and recorded expenditures are available below.');
  const metrics=el('div','diligence-metrics');
  [['Properties / residences',members.length+' / '+(units||'—')],['Proposed monthly rent',money(total('gross')===null?null:total('gross')/12)],['Stabilized annual NOI',money(total('noi'))],['Recorded expenditures',money(paid)]].forEach(([label,value])=>{const m=el('div');m.append(el('span','',label),el('strong','',value));metrics.append(m);});summary.append(metrics);
  const properties=section('properties','Property schedule');
  table(properties,['Property','Ownership entity','Current stage','Residences','Proposed rent / month'],members.map((p,i)=>[p.short,p.entity||'—',(p.status||'—').replaceAll('_',' '),operating[i]?.units||'—',money(operating[i]?operating[i].gross/12:null)]));
  baseline.forEach(row=>{const d=detail(properties,memberById[row.slug]?.short||row.slug);table(d,['Proposed unit','Approx. area (SF)','Monthly asking rent'],(row.units||[]).map(u=>[u.label,u.sqft||'—',money(u.rent)]));paragraph(d,'Unit areas and asking rents are proposed. See attached plans for the documented building areas.','re-foot-note');});
  const budget=section('budget','Development budget');
  const budgetRows=baseline.map(r=>({name:memberById[r.slug]?.short||r.slug,acquisition:r.acquisition,hard:r.hardCostsIncludingContingency,soft:r.softCosts,total:r.acquisition+r.hardCostsIncludingContingency+r.softCosts}));
  const budgetTotal=k=>budgetRows.length===members.length&&budgetRows.length&&budgetRows.every(r=>Number.isFinite(r[k]))?budgetRows.reduce((n,r)=>n+r[k],0):null;
  table(budget,['Cost category','Budget','Per residence'],[['Acquisition',budgetTotal('acquisition')],['Rehabilitation · hard costs',budgetTotal('hard')],['Soft costs',budgetTotal('soft')],['Development subtotal',budgetTotal('total')]].map(([name,value])=>[name,money(value),money(units&&value!==null?value/units:null)]));
  paragraph(budget,'Hard-cost budgets include contingency'+(Number.isFinite(basis.contingencyPct)?' ('+(basis.contingencyPct*100).toFixed(0)+'%)':'')+'. Financing and closing costs are excluded from this subtotal.','re-foot-note');
  if(budgetRows.length){const byProperty=detail(budget,'Budget by property');table(byProperty,['Property','Acquisition','Hard costs','Soft costs','Subtotal'],budgetRows.map(r=>[r.name,money(r.acquisition),money(r.hard),money(r.soft),money(r.total)]));}
  const operations=section('operations','Stabilized operating proforma');
  paragraph(operations,'Annual and monthly operating projections at the current proposed rents.');
  const gross=total('gross'),egi=total('egi'),noi=total('noi'),reserve=total('reserve'),ncf=total('ncf');
  table(operations,['Operating cash flow','Annual','Monthly'],[['Gross potential rent',gross],['Less: vacancy / credit loss',gross===null||egi===null?null:gross-egi],['Effective gross income',egi],['Less: operating expenses',egi===null||noi===null?null:egi-noi],['Net operating income',noi],['Replacement reserves',reserve],['Net cash flow before financing',ncf]].map(([label,value])=>[label,value===null?'Not established':money(value),value===null?'—':money(value/12)]));
  table(operations,['Operating assumption','Current input'],[['Vacancy / credit loss',Number.isFinite(assumptions.vacancy_rate)?(assumptions.vacancy_rate*100).toFixed(1)+'% of gross rent':'Not established'],['Operating expense allowance',Number.isFinite(assumptions.opex_rate)?(assumptions.opex_rate*100).toFixed(1)+'% of effective gross income':'Not established'],['Replacement reserves',Number.isFinite(basis.operating?.replacementReservePerUnitYear)?money(basis.operating.replacementReservePerUnitYear)+' / residence / year':'Not established']]);
  paragraph(operations,'Proposed rents are not collected income. NOI is before replacement reserves and debt service. Expenses use an aggregate allowance; a detailed operating budget is not yet established.','re-foot-note');
  const operatingDetail=detail(operations,'Operating proforma by property');
  table(operatingDetail,['Property','Annual gross rent','Vacancy','Operating expenses','Annual NOI'],members.map((p,i)=>{const u=operating[i];return [p.short,money(u?.gross),money(u?u.gross-u.egi:null),money(u?u.egi-u.noi:null),money(u?.noi)];}));
  const progress=section('progress','Live project records');
  paragraph(progress,'Recorded expenditures: '+exactMoney(paid)+'. Updated from the property ledgers as entries are added.');
  table(progress,['Property','Recorded expenditures','Work phases complete'],members.map(p=>[p.short,exactMoney((p.ledger||[]).filter(r=>r.type==='expense').reduce((n,r)=>n+r.amount,0)),(p.work||[]).filter(w=>w.checked).length+' / '+(p.work||[]).length]));
  const expenses=detail(progress,'Expense ledger · '+expenseRows.length+' entries');
  table(expenses,['Date','Property','Payee / description','Category','Amount'],expenseRows.map(r=>[r.date,r.property,[r.contractor||r.vendor,r.note].filter(Boolean).join(' · '),r.category||r.cat||'—',exactMoney(r.amount)]));
  paragraph(expenses,'Recorded ledger entries only. Contracts are commitments and are not added to cash expenditures.','re-foot-note');
  if((basis.unmatchedPayments||[]).length)table(expenses,['Additional reported payments · pending allocation','Amount'],basis.unmatchedPayments.map(p=>[p.description,exactMoney(p.amount)]));
  members.forEach(p=>{const d=detail(progress,p.short+' · work progress');table(d,['Work phase','Status'],(p.work||[]).map(w=>[w.text,w.checked?'Complete'+(w.done?' · '+w.done:''):'Open']));});
  const documents=section('documents','Plans and documents');
  paragraph(documents,'Plans, authorizations, and reference material. Draft plans do not establish permit approval; reference appraisals apply only to the property identified.');
  const docResults=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/docs')));
  members.forEach((p,i)=>{const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));const result=docResults[i];if(result.status==='rejected'){paragraph(sub,'Documents could not be loaded. Return to the workspace and retry; this is not an empty document inventory.');}else {const docs=result.value.docs||[];if(!docs.length)paragraph(sub,'Documents not yet available.');docs.forEach(d=>{const a=el('a','',d.name);a.href=docLink(d.path);a.target='_blank';a.rel='noopener';sub.append(a);});}documents.append(sub);});

  if((basis.supportingDocuments||[]).length){
    const refs=el('div','diligence-doc-group');refs.append(el('h4','','Deal reference documents'));
    (basis.supportingDocuments||[]).forEach(d=>{
      if(!d.path?.startsWith('system/realestate/docs/')) return;
      const a=el('a','',d.title);a.href=docLink(d.path);a.target='_blank';a.rel='noopener';refs.append(a);paragraph(refs,d.note||'');
    });documents.append(refs);
  }
  // Contract attachments are a separate store from property-folder files.
  try {
    const contracts = (await read('/api/realestate/contracts')).contracts || [];
    const linked = contracts.filter(c => (c.allocations || []).some(a => memberById[a.property]));
    const committed = linked.filter(c => c.status === 'accepted');
    table(documents,['Contract','Status','Allocated to this deal','Document'],linked.map(c => [c.name,c.status,money((c.allocations||[]).filter(a=>memberById[a.property]).reduce((n,a)=>n+a.amount,0)),c.doc?'Linked below':'No file attached']));
    const seen = new Set();
    const addDocument = (ref,label) => {
      if (!ref || seen.has(ref)) return; seen.add(ref);
      let href='';
      if (/^sha256:[a-f0-9]{64}$/.test(ref)) href=docLink(ref);
      else if (/^https?:\/\//.test(ref)) href=ref;
      else if (ref.startsWith('system/realestate/docs/')) href=docLink(ref);
      if (!href) return;
      const a=el('a','',label);a.href=href;a.target='_blank';a.rel='noopener';documents.append(a,el('br'));
    };
    linked.forEach(c=>addDocument(c.doc,c.name+' · '+c.status));
    expenseRows.forEach(r=>addDocument(r.doc && !r.doc.includes('/') && !r.doc.startsWith('sha256:')?'system/realestate/docs/'+r.propertySlug+'/'+r.doc:r.doc,r.property+' · '+r.date+' · receipt'));
    paragraph(documents,'Accepted contract allocations: '+money(committed.reduce((n,c)=>n+(c.allocations||[]).filter(a=>memberById[a.property]).reduce((k,a)=>k+a.amount,0),0))+'. Contract amounts are commitments, not additional expenses.');
  } catch(e) { paragraph(documents,'Contract inventory could not be loaded: '+e.message); }

}
