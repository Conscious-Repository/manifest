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
async function renderDealDiligence(host, slug) {
  const preview=el('div','diligence-preview');
  host.replaceChildren(preview); host=preview;
  const back = el('button','pp3-note','← Deal workspace');
  back.onclick=()=>renderDealPage(slug); host.append(back);
  const loading=el('p','','Loading deal records…');host.append(loading);
  const read=async path=>{const r=await fetch(path,{cache:'no-store'});if(!r.ok)throw Error('Could not load '+path+' ('+r.status+')');return r.json();};
  let data;
  try { data=await read('/api/deals/'+encodeURIComponent(slug)); }
  catch(e){loading.textContent=e.message;const retry=el('button','','Retry');retry.onclick=()=>renderDealDiligence(host,slug);host.append(retry);return;}
  loading.remove();
  const basis=data.source?.lender_diligence;
  host.append(el('h2','pp3-title',basis?.title || data.deal.name),el('p','re-foot-note','Lender view · private preview · no external link has been created'));
  if(!basis){host.append(el('p','','No financing baseline has been recorded for this deal yet. Current property records remain available in the workspace.'));return;}
  const money=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD',minimumFractionDigits:2,maximumFractionDigits:2}):'Not recorded';
  const paragraph=(parent,text,cls='')=>parent.append(el('p',cls,text));
  const section=(id,title)=>{const s=el('section','diligence-section');s.id='diligence-'+id;s.append(el('h3','',title));host.append(s);return s;};
  const table=(parent,headers,rows)=>{const wrap=el('div','diligence-table-wrap');const t=el('table','diligence-table');const head=el('thead'),tr=el('tr');headers.forEach(h=>tr.append(el('th','',h)));head.append(tr);t.append(head);const body=el('tbody');rows.forEach(row=>{const r=el('tr');row.forEach(v=>r.append(el('td','',String(v))));body.append(r);});t.append(body);wrap.append(t);parent.append(wrap);};
  const nav=el('nav','diligence-nav');nav.setAttribute('aria-label','Diligence sections');
  [['overview','Request'],['properties','Properties'],['expenses','Expenses'],['documents','Plans & documents'],['underwriting','Underwriting'],['review','Open items']].forEach(([id,label])=>{const b=el('button','',label);b.onclick=()=>document.getElementById('diligence-'+id)?.scrollIntoView({block:'start',behavior:'smooth'});nav.append(b);});host.append(nav);
  const baseline=basis.properties||[];
  const members=data.members||[];
  const memberById=Object.fromEntries(members.map(p=>[p.slug,p]));
  const stacks=baseline.map(r=>diligenceStack(r,basis));
  const sum=key=>stacks.reduce((n,s)=>n+s[key],0);
  const overview=section('overview','Financing request');
  paragraph(overview,basis.status+' · '+basis.lender+' · communicated '+basis.basisDate);
  paragraph(overview,basis.structure);
  const metrics=el('div','diligence-metrics');
  [['Base construction loans',baseline.reduce((n,r)=>n+r.baseLoan,0)],['12-month reserve',sum('reserve')],['Total proposed request',sum('request')],['Development equity',sum('equity')]].forEach(([label,value])=>{const metric=el('div');metric.append(el('span','',label),el('strong','',money(value)));metrics.append(metric);});overview.append(metrics);
  paragraph(overview,basis.termMonths+' months · '+(basis.constructionRate*100).toFixed(2)+'% interest-only. Reserve assumes full deployment for '+basis.reserveMonths+' months; actual interest is expected on deployed balances. Financing fees remain to be quantified.');
  paragraph(overview,basis.source,'re-foot-note');
  const properties=section('properties',baseline.length+' properties · scope and proposed uses');
  table(properties,['Order / property','Units','Pro forma rent / month','Acquisition','Hard costs incl. contingency','Soft allowance','Development costs','Loan + reserve'],baseline.map((r,i)=>{const p=memberById[r.slug],s=stacks[i];return [r.phase+' · '+(p?.short||r.slug),r.units.length,money(s.monthlyRent),money(r.acquisition),money(r.hardCostsIncludingContingency),money(r.softCosts),money(s.development),money(s.request)];}));
  paragraph(properties,'Hard costs already include the '+(basis.contingencyPct*100)+'% contingency. Development costs exclude the capitalized interest reserve. Equity is '+(basis.fundEquityShare*100)+'% Fund I / '+(basis.partnerEquityShare*100)+'% partners. Rents are proposed asking assumptions, not signed leases.');
  table(properties,['Property','Fund I equity','Partner equity'],baseline.map((r,i)=>[memberById[r.slug]?.short||r.slug,money(stacks[i].fund),money(stacks[i].partners)]));
  baseline.forEach(r=>{const p=memberById[r.slug];const detail=el('details','diligence-property');detail.append(el('summary','',p?.short||r.slug));paragraph(detail,r.reportedStatus+' Reported September 3, 2026.');table(detail,['Proposed unit','Approx. SF','Monthly rent'],r.units.map(u=>[u.label,u.sqft,money(u.rent)]));if(p){paragraph(detail,'Current record: '+p.status.replaceAll('_',' ')+' · '+p.entity);table(detail,['Work phase','Recorded status'],(p.work||[]).map(w=>[w.text,w.checked?'Complete'+(w.done?' · '+w.done:''):'Open']));}else paragraph(detail,'Missing current property record.');properties.append(detail);});
  const expenses=section('expenses','Expenses to date');
  paragraph(expenses,'Current property ledger entries only. Bids and contracts are not cash payments. Shared costs appear at their recorded property allocation; missing receipts are identified below.');
  const expenseRows=members.flatMap(p=>(p.ledger||[]).filter(r=>r.type==='expense').map(r=>({...r,property:p.short}))).sort((a,b)=>String(b.date).localeCompare(String(a.date)));
  paragraph(expenses,'Recorded expense total: '+money(expenseRows.reduce((n,r)=>n+r.amount,0))+' · '+expenseRows.length+' entries · loaded '+new Date().toLocaleDateString());
  table(expenses,['Date','Property','Payee / description','Category','Amount','Evidence'],expenseRows.map(r=>[r.date,r.property,[r.contractor||r.vendor,r.note].filter(Boolean).join(' · '),r.category||r.cat||'Unclassified',money(r.amount),r.doc?'Receipt linked':r.stmt?'Statement reference; receipt not linked':'No receipt linked']));
  const documents=section('documents','Plans and documents');
  paragraph(documents,'Permit-ready plans were targeted for early October. A drawing or an email assertion is not permit approval. Documents below are attached to this deal’s member properties.');
  const docResults=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/docs')));
  members.forEach((p,i)=>{const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));const result=docResults[i];if(result.status==='rejected'){paragraph(sub,'Documents could not be loaded. Return to the workspace and retry; this is not an empty document inventory.');}else {const docs=result.value.docs||[];if(!docs.length)paragraph(sub,'No files attached in the property document folder.');docs.forEach(d=>{const a=el('a','',d.name);a.href='/api/realestate/doc?path='+encodeURIComponent(d.path);a.target='_blank';a.rel='noopener';sub.append(a);});}documents.append(sub);});
  (basis.documentNotes||[]).forEach(note=>paragraph(documents,note));
  if((basis.supportingDocuments||[]).length){
    const refs=el('div','diligence-doc-group');refs.append(el('h4','','Deal reference documents'));
    (basis.supportingDocuments||[]).forEach(d=>{
      if(!d.path?.startsWith('system/realestate/docs/')) return;
      const a=el('a','',d.title);a.href='/api/realestate/doc?path='+encodeURIComponent(d.path);a.target='_blank';a.rel='noopener';refs.append(a);paragraph(refs,d.note||'');
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
      if (/^sha256:[a-f0-9]{64}$/.test(ref)) href='/api/realestate/files/'+ref.slice(7);
      else if (/^https?:\/\//.test(ref)) href=ref;
      else if (ref.startsWith('system/realestate/docs/')) href='/api/realestate/doc?path='+encodeURIComponent(ref);
      if (!href) return;
      const a=el('a','',label);a.href=href;a.target='_blank';a.rel='noopener';documents.append(a,el('br'));
    };
    linked.forEach(c=>addDocument(c.doc,c.name+' · '+c.status));
    expenseRows.forEach(r=>addDocument(r.doc,r.property+' · '+r.date+' · receipt'));
    paragraph(documents,'Accepted contract allocations: '+money(committed.reduce((n,c)=>n+(c.allocations||[]).filter(a=>memberById[a.property]).reduce((k,a)=>k+a.amount,0),0))+'. Contract amounts are commitments, not additional expenses.');
  } catch(e) { paragraph(documents,'Contract inventory could not be loaded: '+e.message); }
  const underwriting=section('underwriting','Underwriting and repayment');
  paragraph(underwriting,basis.repayment);
  paragraph(underwriting,'Email refinance assumptions: 7.0% interest · 25-year amortization · 70–75% LTV. Values below are the current screening model, not the financing baseline or an appraisal.');
  let assumptions=null;
  try {
    const result=await read('/api/realestate/assumptions');
    const needed=['vacancy_rate','opex_rate','exit_cap_rate','perm_interest_rate','perm_amort_years','perm_ltv','contingency_pct','construction_loan_ltc'];
    if (!needed.every(key=>Number.isFinite(result.values?.[key]))) throw Error('Incomplete underwriting assumptions');
    assumptions=result.values;
  } catch(e) { paragraph(underwriting,'Underwriting unavailable: '+e.message+'. No zero-rate defaults have been substituted.');
    const retry=el('button','','Reload diligence data');retry.onclick=()=>renderDealDiligence(host,slug);underwriting.append(retry);
  }
  const sources=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/source')));
  table(underwriting,['Property','Current modeled TDC','Current modeled NOI','Current modeled DSCR','Baseline development costs'],members.map((p,i)=>{if(!assumptions || sources[i].status!=='fulfilled')return [p.short,'Unavailable','Unavailable','Unavailable',''];const source=sources[i].value.source||{};const uw=reScreen(p,source,assumptions);const row=baseline.find(r=>r.slug===p.slug);return [p.short,money(uw.tdc),money(uw.noi),uw.dscr?uw.dscr.toFixed(2):'Not available',row?money(diligenceStack(row,basis).development):'Not recorded'];}));

  const reconciliation=el('details','diligence-property');
  reconciliation.append(el('summary','','Explain budget differences'));
  paragraph(reconciliation,'The email is the proposed financing baseline. The current screening model uses work estimates where available, a separate contingency, and a soft-cost approximation when no carrying budget is entered. Neither column is cash spent.');
  baseline.forEach(row=>{
    const index=members.findIndex(p=>p.slug===row.slug), p=members[index];
    if(!p || !assumptions || sources[index]?.status!=='fulfilled') return;
    const uw=reScreen(p,sources[index].value.source||{},assumptions);
    if(!uw.complete) return;
    const total=diligenceStack(row,basis).development;
    const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));
    table(sub,['Cost component','Email baseline','Current screening'],[
      ['Acquisition',money(row.acquisition),money(uw.purchase)],
      ['Closing costs','A2P closing/legal excluded; amount pending',money(uw.closing)],
      ['Hard costs including contingency',money(row.hardCostsIncludingContingency),money(uw.hard+uw.contingency)],
      ['Soft costs',money(row.softCosts),money(uw.soft)],
      ['Total development costs',money(total),money(uw.tdc)],
      ['Difference from email baseline','—',money(uw.tdc-total)]
    ]);
    paragraph(sub,'Current hard costs: '+money(uw.hard)+' from '+(uw.hardFromWork?'work-stage estimates':'the source budget')+' + '+money(uw.contingency)+' contingency ('+(assumptions.contingency_pct*100).toFixed(1)+'%). Soft costs use '+(reSrcNum(sources[index].value.source||{},'carry_cost')>0?'the recorded carrying budget.':'the screening allowance of 15% of hard costs; this is not a detailed soft-cost budget.'));
    reconciliation.append(sub);
  });
  underwriting.append(reconciliation);

  paragraph(underwriting,'Email-baseline refinance scenario · proposed rents and loan-plus-reserve repayment, using the current vacancy, operating expense ratio and cap-rate assumptions below. This scenario is not an appraisal or a lending commitment.');
  const refinance=baseline.map(row=>diligenceRefinance(row,basis,assumptions));
  table(underwriting,['Property','Annual gross rent','Vacancy allowance','Operating expenses','NOI','Debt service on full request','DSCR on full request'],baseline.map((row,i)=>{const r=refinance[i];return r?[memberById[row.slug]?.short||row.slug,money(r.gross),money(r.vacancy),money(r.expenses),money(r.noi),money(r.debt),r.dscr.toFixed(2)]:[row.slug,'Missing operating assumptions','','','','',''];}));
  table(underwriting,['Property','Income-based value','70% LTV capacity','75% LTV capacity','Loan + reserve to repay','Shortfall at 75%'],baseline.map((row,i)=>{const r=refinance[i];return r?[memberById[row.slug]?.short||row.slug,money(r.value),money(r.low),money(r.high),money(r.repayment),money(r.gap)]:[row.slug,'Unavailable','','','',''];}));
  paragraph(underwriting,'Repayment stress assumes the entire reserve is consumed. Refinance proceeds exclude transaction costs and lender-specific debt-service constraints. Unused reserve could reduce the amount to repay.');
  if (assumptions) table(underwriting,['Current screening input','Value'],[['Vacancy',((assumptions.vacancy_rate||0)*100).toFixed(1)+'%'],['Operating expense ratio',((assumptions.opex_rate||0)*100).toFixed(1)+'%'],['Exit cap rate',((assumptions.exit_cap_rate||0)*100).toFixed(2)+'%'],['Permanent rate',((assumptions.perm_interest_rate||0)*100).toFixed(2)+'%'],['Permanent amortization',assumptions.perm_amort_years+' years'],['Permanent LTV',((assumptions.perm_ltv||0)*100).toFixed(1)+'%'],['Current model contingency',((assumptions.contingency_pct||0)*100).toFixed(1)+'%'],['Current model construction LTC',((assumptions.construction_loan_ltc||0)*100).toFixed(1)+'%']]);
  paragraph(underwriting,'NOI = proposed annual rent × (1 − vacancy) × (1 − operating expense ratio). Screening value = NOI ÷ cap rate. Debt service uses the screening takeout loan and the displayed permanent loan terms. These are simplified screening calculations; detailed tax, insurance, reserve and draw schedules still require reconciliation.');
  paragraph(underwriting,'The original locked underwriting remains a separate dated record. Current expense totals and the financing baseline do not overwrite that history.');

  const review=section('review','Open diligence items');
  const list=el('ul');(basis.openItems||[]).forEach(t=>list.append(el('li','',t)));review.append(list);
  paragraph(review,'Corrections retained from correspondence');const corrections=el('ul');(basis.corrections||[]).forEach(t=>corrections.append(el('li','',t)));review.append(corrections);
  const missing=baseline.filter(r=>!memberById[r.slug]);if(missing.length)paragraph(review,'Missing deal members: '+missing.map(r=>r.slug).join(', '));
}
