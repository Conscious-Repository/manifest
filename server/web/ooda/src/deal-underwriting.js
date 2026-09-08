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
  if(!assumptions || !['vacancy_rate','opex_rate','exit_cap_rate'].every(k=>Number.isFinite(assumptions[k])))return null;
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
  const back = el('button','pp3-note','← Deal workspace');
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
  let basis=data.source?.deal_underwriting||data.source?.lender_diligence;
  host.append(el('h2','pp3-title',basis?.title || data.deal.name),el('p','re-foot-note','Deal underwriting · live records and dated financing scenarios'));
  const live=el('p','re-foot-note','Live · checked '+new Date().toLocaleTimeString());live.dataset.liveStatus='';live.setAttribute('role','status');host.append(live);
  if(!basis){
    basis={properties:[],openItems:['No financing scenario configured for this deal. Add confirmed terms and sources of equity in Manifest before treating this as a financing request.'],corrections:[],status:'Not configured',source:'Live property records; no financing terms assumed.'};
  }

  const money=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD',minimumFractionDigits:2,maximumFractionDigits:2}):'Not recorded';
  const paragraph=(parent,text,cls='')=>parent.append(el('p',cls,text));
  const section=(id,title)=>{const s=el('section','diligence-section');s.id='diligence-'+id;s.append(el('h3','',title));host.append(s);return s;};
  const table=(parent,headers,rows)=>{const wrap=el('div','diligence-table-wrap');const t=el('table','diligence-table');const head=el('thead'),tr=el('tr');headers.forEach(h=>tr.append(el('th','',h)));head.append(tr);t.append(head);const body=el('tbody');rows.forEach(row=>{const r=el('tr');row.forEach(v=>r.append(el('td','',String(v))));body.append(r);});t.append(body);wrap.append(t);parent.append(wrap);};
  const nav=el('nav','diligence-nav');nav.setAttribute('aria-label','Diligence sections');
  [['snapshot','Overview'],['overview','Financing'],['properties','Properties'],['operations','Operating proforma'],['expenses','Expenses'],['documents','Plans & documents'],['underwriting','Underwriting'],['review','Open items']].forEach(([id,label])=>{const b=el('button','',label);b.onclick=()=>document.getElementById('diligence-'+id)?.scrollIntoView({block:'start',behavior:'smooth'});nav.append(b);});host.append(nav);
  const baseline=basis.properties||[];
  const members=data.members||[];
  const memberById=Object.fromEntries(members.map(p=>[p.slug,p]));
  const stacks=baseline.map(r=>diligenceStack(r,basis));
  const sum=key=>stacks.reduce((n,s)=>n+s[key],0);
  const snapshot=section('snapshot','Deal snapshot');
  const current=members.map(p=>diligenceOperating(p,data.sources[p.slug]||{},data.assumptions,basis.operating||{}));
  const liveTotal=key=>current.length&&current.every(p=>p&&Number.isFinite(p[key]))?current.reduce((n,p)=>n+p[key],0):null;
  const totalPaid=members.reduce((n,p)=>n+(p.ledger||[]).filter(r=>r.type==='expense').reduce((n,r)=>n+r.amount,0),0);
  const facts=el('div','diligence-metrics');
  [['Properties',String(members.length)],['Recorded expenses',money(totalPaid)],['Modeled annual NOI',money(liveTotal('noi'))],['Current modeled development cost',money(liveTotal('tdc'))]].forEach(([label,value])=>{const item=el('div');item.append(el('span','',label),el('strong','',value));facts.append(item);});snapshot.append(facts);
  paragraph(snapshot,'Live expenses and model inputs update automatically. Financing terms below are a dated scenario, not an approved commitment. Unmatched payments and incomplete inputs are listed separately.');
  const overview=section('overview','Financing scenario');
  paragraph(overview,basis.status+(basis.lender?' · '+basis.lender:'')+(basis.basisDate?' · dated '+basis.basisDate:''));
  if(basis.structure)paragraph(overview,basis.structure);
  if(baseline.length){
  const metrics=el('div','diligence-metrics');
  [['Base construction loans',baseline.reduce((n,r)=>n+r.baseLoan,0)],['12-month reserve',sum('reserve')],['Total proposed request',sum('request')],['Development equity',sum('equity')]].forEach(([label,value])=>{const metric=el('div');metric.append(el('span','',label),el('strong','',money(value)));metrics.append(metric);});overview.append(metrics);
  paragraph(overview,basis.termMonths+' months · '+(basis.constructionRate*100).toFixed(2)+'% interest-only. Reserve assumes full deployment for '+basis.reserveMonths+' months; actual interest is expected on deployed balances. Financing fees remain to be quantified.');
  }
  paragraph(overview,basis.source||'Source not recorded','re-foot-note');
  if(baseline.length)table(overview,['Sources','Amount','Uses','Amount'],[['Construction loans + reserve',money(sum('request')),'Acquisition',money(baseline.reduce((n,r)=>n+r.acquisition,0))],['Development equity',money(sum('equity')),'Hard costs including contingency',money(baseline.reduce((n,r)=>n+r.hardCostsIncludingContingency,0))],['','', 'Soft costs',money(baseline.reduce((n,r)=>n+r.softCosts,0))],['','', 'Capitalized interest reserve',money(sum('reserve'))],['Known sources',money(sum('request')+sum('equity')),'Known uses, excluding unquantified closing fees',money(sum('development')+sum('reserve'))]]);
  const properties=section('properties',members.length+' properties · scope and proposed uses');
  if(!baseline.length)table(properties,['Property','Entity','Status','Units'],members.map(p=>[p.short,p.entity||'Not recorded',p.status||'Not recorded',p.units||'Not recorded']));
  if(baseline.length)table(properties,['Order / property','Units','Pro forma rent / month','Acquisition','Hard costs incl. contingency','Soft allowance','Development costs','Loan + reserve'],baseline.map((r,i)=>{const p=memberById[r.slug],s=stacks[i];return [r.phase+' · '+(p?.short||r.slug),r.units.length,money(s.monthlyRent),money(r.acquisition),money(r.hardCostsIncludingContingency),money(r.softCosts),money(s.development),money(s.request)];}));
  if(baseline.length)paragraph(properties,'Hard costs already include the '+(basis.contingencyPct*100)+'% contingency. Development costs exclude the capitalized interest reserve. Equity is '+(basis.fundEquityShare*100)+'% Fund I / '+(basis.partnerEquityShare*100)+'% partners. Rents are proposed asking assumptions, not signed leases.');
  if(baseline.length)table(properties,['Property',basis.fundEquityLabel||'Fund equity','Partner equity'],baseline.map((r,i)=>[memberById[r.slug]?.short||r.slug,money(stacks[i].fund),money(stacks[i].partners)]));
  baseline.forEach(r=>{const p=memberById[r.slug];const detail=el('details','diligence-property');detail.append(el('summary','',p?.short||r.slug));paragraph(detail,(r.reportedStatus||'Status not recorded')+(basis.basisDate?' · scenario dated '+basis.basisDate:''));table(detail,['Proposed unit','Approx. SF','Monthly rent'],r.units.map(u=>[u.label,u.sqft,money(u.rent)]));if(p){paragraph(detail,'Current record: '+p.status.replaceAll('_',' ')+' · '+p.entity);table(detail,['Work phase','Recorded status'],(p.work||[]).map(w=>[w.text,w.checked?'Complete'+(w.done?' · '+w.done:''):'Open']));}else paragraph(detail,'Missing current property record.');properties.append(detail);});
  const operations=section('operations','Stabilized operating proforma · live inputs');
  paragraph(operations,'Proposed rents from current property records. These are forecasts, not collected rental income. The financing scenario above retains its dated figures when live records change.');
  const operating=members.map(p=>diligenceOperating(p,data.sources[p.slug]||{},data.assumptions,basis.operating||{}));
  table(operations,['Property','Units','Monthly rent','Annual gross rent','Vacancy / credit loss','Operating expenses','Annual NOI','Replacement reserves','Net cash flow before debt'],members.map((p,i)=>{const u=operating[i];return u?[p.short,u.units,money(u.gross/12),money(u.gross),money(u.gross-u.egi),money(u.egi-u.noi),money(u.noi),u.reserve===null?'Not configured':money(u.reserve),u.ncf===null?'Requires reserve assumption':money(u.ncf)]:[p.short,'Operating inputs incomplete','','','','','','',''];}));
  paragraph(operations,'NOI excludes replacement reserves and debt service. Net cash flow deducts replacement reserves from NOI. Missing reserve inputs are not treated as zero. Current ratio-based expenses are a screening allowance; no tax, insurance or utility breakdown has been invented.');
  const forecast=el('details','diligence-property');forecast.append(el('summary','','Multi-year projections and returns'));
  paragraph(forecast,'Not configured: deal-specific hold period, rent/expense growth, replacement reserves, sale costs, draw timing, and equity distribution terms. IRR, equity multiple and a multi-year cash-flow forecast require those inputs. Figures from the example proforma have not been copied into this deal.');operations.append(forecast);
  const expenses=section('expenses','Expenses to date');
  paragraph(expenses,'Current property ledger entries only. Bids and contracts are not cash payments. Shared costs appear at their recorded property allocation; missing receipts are identified below.');
  const expenseRows=members.flatMap(p=>(p.ledger||[]).filter(r=>r.type==='expense').map(r=>({...r,property:p.short,propertySlug:p.slug}))).sort((a,b)=>String(b.date).localeCompare(String(a.date)));
  paragraph(expenses,'Recorded expense total: '+money(expenseRows.reduce((n,r)=>n+r.amount,0))+' · '+expenseRows.length+' entries · loaded '+new Date().toLocaleDateString());
  table(expenses,['Date','Property','Payee / description','Category','Amount','Evidence'],expenseRows.map(r=>[r.date,r.property,[r.contractor||r.vendor,r.note].filter(Boolean).join(' · '),r.category||r.cat||'Unclassified',money(r.amount),r.doc?'Receipt linked':r.stmt?'Statement reference; receipt not linked':'No receipt linked']));
  if((basis.unmatchedPayments||[]).length){
    expenses.append(el('h4','','Paid · awaiting transaction matching'));
    table(expenses,['Payment','Amount','Reconciliation status'],basis.unmatchedPayments.map(p=>[p.description,money(p.amount),p.status]));
  }
  const documents=section('documents','Plans and documents');
  paragraph(documents,'Member-property files and explicitly included deal references. Draft drawings, executed documents and approvals retain their recorded status.');
  const docResults=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/docs')));
  members.forEach((p,i)=>{const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));const result=docResults[i];if(result.status==='rejected'){paragraph(sub,'Documents could not be loaded. Return to the workspace and retry; this is not an empty document inventory.');}else {const docs=result.value.docs||[];if(!docs.length)paragraph(sub,'No files attached in the property document folder.');docs.forEach(d=>{const a=el('a','',d.name);a.href=docLink(d.path);a.target='_blank';a.rel='noopener';sub.append(a);});}documents.append(sub);});
  (basis.documentNotes||[]).forEach(note=>paragraph(documents,note));
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
  const underwriting=section('underwriting','Underwriting and repayment');
  if(basis.repayment)paragraph(underwriting,basis.repayment);
  paragraph(underwriting,'Current screening uses the live operating inputs below. A dated financing scenario remains separate from current modeled costs. Neither is an appraisal.');
  let assumptions=null;
  try {
    const result=await read('/api/realestate/assumptions');
    const needed=['vacancy_rate','opex_rate','exit_cap_rate','perm_interest_rate','perm_amort_years','perm_ltv','contingency_pct','construction_loan_ltc'];
    if (!needed.every(key=>Number.isFinite(result.values?.[key]))) throw Error('Incomplete underwriting assumptions');
    assumptions=result.values;
  } catch(e) { paragraph(underwriting,'Underwriting unavailable: '+e.message+'. No zero-rate defaults have been substituted.');
    const retry=el('button','','Reload underwriting');retry.onclick=options.reload;underwriting.append(retry);
  }
  const sources=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/source')));
  table(underwriting,['Property','Current modeled TDC','Current modeled NOI','Current modeled DSCR','Scenario development costs'],members.map((p,i)=>{if(!assumptions || sources[i].status!=='fulfilled')return [p.short,'Unavailable','Unavailable','Unavailable',''];const source=sources[i].value.source||{};const uw=reScreen(p,source,assumptions);const row=baseline.find(r=>r.slug===p.slug);return [p.short,money(uw.tdc),money(uw.noi),uw.dscr?uw.dscr.toFixed(2):'Not available',row?money(diligenceStack(row,basis).development):'Not recorded'];}));

  const reconciliation=el('details','diligence-property');
  reconciliation.append(el('summary','','Explain budget differences'));
  paragraph(reconciliation,'The dated scenario is the proposed financing baseline. The current screening model uses work estimates where available, a separate contingency, and a soft-cost approximation when no carrying budget is entered. Neither column is cash spent.');
  baseline.forEach(row=>{
    const index=members.findIndex(p=>p.slug===row.slug), p=members[index];
    if(!p || !assumptions || sources[index]?.status!=='fulfilled') return;
    const uw=reScreen(p,sources[index].value.source||{},assumptions);
    if(!uw.complete) return;
    const total=diligenceStack(row,basis).development;
    const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));
    table(sub,['Cost component','Dated scenario','Current screening'],[
      ['Acquisition',money(row.acquisition),money(uw.purchase)],
      ['Closing costs','Financing closing/legal excluded; amount pending',money(uw.closing)],
      ['Hard costs including contingency',money(row.hardCostsIncludingContingency),money(uw.hard+uw.contingency)],
      ['Soft costs',money(row.softCosts),money(uw.soft)],
      ['Total development costs',money(total),money(uw.tdc)],
      ['Difference from dated scenario','—',money(uw.tdc-total)]
    ]);
    paragraph(sub,'Current hard costs: '+money(uw.hard)+' from '+(uw.hardFromWork?'work-stage estimates':'the source budget')+' + '+money(uw.contingency)+' contingency ('+(assumptions.contingency_pct*100).toFixed(1)+'%). Soft costs use '+(reSrcNum(sources[index].value.source||{},'carry_cost')>0?'the recorded carrying budget.':'the screening allowance of 15% of hard costs; this is not a detailed soft-cost budget.'));
    reconciliation.append(sub);
  });
  if(baseline.length)underwriting.append(reconciliation);
  if(baseline.length){

  paragraph(underwriting,'Dated refinance scenario · proposed rents and loan-plus-reserve repayment, using the current vacancy, operating expense ratio and cap-rate assumptions below. This scenario is not an appraisal or a lending commitment.');
  const refinance=baseline.map(row=>diligenceRefinance(row,basis,assumptions));
  table(underwriting,['Property','Annual gross rent','Vacancy allowance','Operating expenses','NOI','Debt service on full request','DSCR on full request'],baseline.map((row,i)=>{const r=refinance[i];return r?[memberById[row.slug]?.short||row.slug,money(r.gross),money(r.vacancy),money(r.expenses),money(r.noi),money(r.debt),r.dscr.toFixed(2)]:[row.slug,'Missing operating assumptions','','','','',''];}));
  table(underwriting,['Property','Income-based value',(basis.refinanceLtvLow*100)+'% LTV capacity',(basis.refinanceLtvHigh*100)+'% LTV capacity','Loan + reserve to repay','Shortfall at upper LTV'],baseline.map((row,i)=>{const r=refinance[i];return r?[memberById[row.slug]?.short||row.slug,money(r.value),money(r.low),money(r.high),money(r.repayment),money(r.gap)]:[row.slug,'Unavailable','','','',''];}));
  paragraph(underwriting,'Repayment stress assumes the entire reserve is consumed. Refinance proceeds exclude transaction costs and lender-specific debt-service constraints. Unused reserve could reduce the amount to repay.');
  }
  if (assumptions) table(underwriting,['Current screening input','Value'],[['Vacancy',((assumptions.vacancy_rate||0)*100).toFixed(1)+'%'],['Operating expense ratio',((assumptions.opex_rate||0)*100).toFixed(1)+'%'],['Exit cap rate',((assumptions.exit_cap_rate||0)*100).toFixed(2)+'%'],['Permanent rate',((assumptions.perm_interest_rate||0)*100).toFixed(2)+'%'],['Permanent amortization',assumptions.perm_amort_years+' years'],['Permanent LTV',((assumptions.perm_ltv||0)*100).toFixed(1)+'%'],['Current model contingency',((assumptions.contingency_pct||0)*100).toFixed(1)+'%'],['Current model construction LTC',((assumptions.construction_loan_ltc||0)*100).toFixed(1)+'%']]);
  paragraph(underwriting,'NOI = proposed annual rent × (1 − vacancy) × (1 − operating expense ratio). Screening value = NOI ÷ cap rate. Debt service uses the screening takeout loan and the displayed permanent loan terms. These are simplified screening calculations; detailed tax, insurance, reserve and draw schedules still require reconciliation.');
  paragraph(underwriting,'The original locked underwriting remains a separate dated record. Current expense totals and the financing baseline do not overwrite that history.');

  if(baseline.length && assumptions){
    const sensitivity=el('details','diligence-property');sensitivity.append(el('summary','','Explore refinance sensitivity'));
    paragraph(sensitivity,'What-if inputs only; these do not change saved underwriting. Results retain the dated loan-plus-reserve repayment amount.');
    const capLabel=el('label','','Cap rate (%) '),cap=el('input');cap.type='number';cap.min='0.01';cap.step='0.01';cap.value=(assumptions.exit_cap_rate*100).toFixed(2);capLabel.append(cap);
    const rateLabel=el('label','','Refinance rate (%) '),rate=el('input');rate.type='number';rate.min='0';rate.step='0.01';rate.value=(basis.refinanceRate*100).toFixed(2);rateLabel.append(rate);
    const result=el('div');const calculate=()=>{result.replaceChildren();const c=Number(cap.value)/100,r=Number(rate.value)/100;if(!cap.value||!rate.value||!Number.isFinite(c)||!Number.isFinite(r)||c<=0||r<0){paragraph(result,'Enter a positive cap rate and non-negative refinance rate.');return;}table(result,['Property','Income-based value',(basis.refinanceLtvHigh*100)+'% LTV capacity','NOI / debt service','Shortfall'],baseline.map(row=>{const x=diligenceRefinance(row,{...basis,refinanceRate:r},{...assumptions,exit_cap_rate:c});return [memberById[row.slug]?.short||row.slug,money(x.value),money(x.high),x.dscr.toFixed(2),money(x.gap)];}));};
    cap.oninput=calculate;rate.oninput=calculate;sensitivity.append(capLabel,rateLabel,result);calculate();underwriting.append(sensitivity);
  }
  const review=section('review','Reconciliation and missing inputs');
  const list=el('ul');(basis.openItems||[]).forEach(t=>list.append(el('li','',t)));review.append(list);
  paragraph(review,'Corrections retained from correspondence');const corrections=el('ul');(basis.corrections||[]).forEach(t=>corrections.append(el('li','',t)));review.append(corrections);
  const missing=baseline.filter(r=>!memberById[r.slug]);if(missing.length)paragraph(review,'Missing deal members: '+missing.map(r=>r.slug).join(', '));
}
