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
  if(!assumptions || !['vacancy_rate','opex_rate'].every(k=>Number.isFinite(assumptions[k])&&assumptions[k]>=0&&assumptions[k]<1))return null;
  const u=reScreen(p,source,assumptions);if(!u.complete)return null;
  const units=(p.unitMix||[]).length||p.units||reSrcNum(source,'total_units');
  const reserve=Number.isFinite(configuration.replacementReservePerUnitYear)&&configuration.replacementReservePerUnitYear>=0?configuration.replacementReservePerUnitYear*units:null;
  return {...u,units,reserve,ncf:reserve===null?null:u.noi-reserve};
}
const diligencePackageFields=[
 ['lease_up_days','Lease-up duration','days',0,3650],
 ['vacancy_rate','Vacancy / credit loss','%',0,99],['opex_rate','Operating expense allowance','%',0,99],['reserve_years_one_three','Reserve / unit / year · years 1–3','$',0,100000],['reserve_years_four_six','Reserve / unit / year · years 4–6','$',0,100000],['reserve_years_seven_eight','Reserve / unit / year · years 7–8','$',0,100000],['reserve_years_nine_plus','Reserve / unit / year · years 9+','$',0,100000],
 ['rent_growth','Annual rent growth','%',-99,100],['opex_growth','Annual expense growth','%',-99,100],['hold_years','Hold period / projection years','years',1,50],['selling_cost_pct','Selling costs','%',0,100],['exit_cap_rate','Valuation / exit cap rate','%',.1,100],
 ['closing_costs','Additional closing / financing costs','$',0,100000000],['construction_ltc','Base construction loan-to-cost','%',0,100],['construction_rate','Construction interest rate','%',0,100],['term_months','Construction loan term','months',1,600],['reserve_months','Financed interest reserve','months',0,600],['refinance_rate','Refinance interest rate','%',0,100],['refinance_years','Refinance amortization','years',1,50],['refinance_ltv','Refinance upper LTV','%',0,100]
];
function diligencePackageInputs(data){
 const b=data.source?.deal_underwriting||data.source?.lender_diligence||{}, f=b.presentationFinancing||{}, saved=b.packageAssumptions?.values;
 const prior={lease_up_days:null,vacancy_rate:data.assumptions?.vacancy_rate,opex_rate:data.assumptions?.opex_rate,exit_cap_rate:data.assumptions?.exit_cap_rate,replacement_reserve:b.operating?.replacementReservePerUnitYear,rent_growth:data.source?.rent_growth??data.assumptions?.rent_growth,opex_growth:data.source?.opex_growth??data.assumptions?.opex_growth,hold_years:data.source?.hold_years??data.assumptions?.hold_years,selling_cost_pct:data.source?.selling_cost_pct??data.assumptions?.selling_cost_pct,reserve_years_one_three:data.assumptions?.reserve_years_one_three,reserve_years_four_six:data.assumptions?.reserve_years_four_six,reserve_years_seven_eight:data.assumptions?.reserve_years_seven_eight,reserve_years_nine_plus:data.assumptions?.reserve_years_nine_plus,closing_costs:null,construction_ltc:f.constructionLtc,construction_rate:f.constructionRate,term_months:f.termMonths,reserve_months:f.reserveMonths,refinance_rate:f.refinanceRate,refinance_years:f.refinanceAmortYears,refinance_ltv:f.refinanceLtvHigh};
 return Object.fromEntries(diligencePackageFields.map(([key])=>[key,{value:saved? saved[key]??null:prior[key]??null,origin:saved?'This lender ask':key.startsWith('reserve_years_')?'Portfolio reserve ladder':['rent_growth','opex_growth','hold_years','selling_cost_pct'].includes(key)?(Number.isFinite(data.source?.[key])?'Existing deal record':'Portfolio setting'):['vacancy_rate','opex_rate','exit_cap_rate'].includes(key)?'Portfolio setting':'Existing package'}]));
}
function diligencePhaseCost(w){return (w.estTotal||0)+(Number((w.fields||[]).find(f=>f.key==='soft-budget')?.value)||0);}
function diligenceSpendingPlan(start,end,total){
 const a=Date.parse(start+'T00:00:00Z'),b=Date.parse(end+'T00:00:00Z');if(!Number.isFinite(a)||!Number.isFinite(b)||b<=a||!Number.isFinite(total))return [];
 const rows=[];let at=a,allocated=0;while(at<b){const d=new Date(at),next=Math.min(b,Date.UTC(d.getUTCFullYear(),d.getUTCMonth()+1,1));const amount=next===b?Math.round((total-allocated)*100)/100:Math.round(total*(next-at)/(b-a)*100)/100;rows.push({month:new Date(at).toISOString().slice(0,7),amount});allocated+=amount;at=next;}return rows;
}
function diligenceProjection(gross,noi,units,v){
 if(![gross,noi,units,v.rent_growth,v.opex_growth,v.hold_years,v.vacancy_rate].every(Number.isFinite))return [];
 const expenses=gross*(1-v.vacancy_rate)-noi;
 return Array.from({length:v.hold_years+1},(_,i)=>{const rent=gross*Math.pow(1+v.rent_growth,i),egi=rent*(1-v.vacancy_rate),opex=expenses*Math.pow(1+v.opex_growth,i),income=egi-opex,reserveRate=v[i<3?'reserve_years_one_three':i<6?'reserve_years_four_six':i<8?'reserve_years_seven_eight':'reserve_years_nine_plus'],reserve=Number.isFinite(reserveRate)?units*reserveRate:null;return {year:i+1,gross:rent,egi,opex,noi:income,reserve,ncf:reserve===null?null:income-reserve};});
}
function renderDealDiligence(host, slug, options = {}) {
  const mount=document.createElement('div');host.replaceChildren(mount);
  const endpoint=options.endpoint||('/api/deals/'+encodeURIComponent(slug)+'/underwriting');
  const viewState={tab:'overview',all:false};
  let stopped=false,busy=false,printing=false,revision='',controller;
  let printClosed=[];const beforePrint=()=>{printing=true;printClosed=Array.from(mount.querySelectorAll('details:not([open])'));printClosed.forEach(d=>d.open=true);};const afterPrint=()=>{printClosed.forEach(d=>d.open=false);printClosed=[];printing=false;};
  window.addEventListener('beforeprint',beforePrint);window.addEventListener('afterprint',afterPrint);
  const dispose=()=>{window.removeEventListener('beforeprint',beforePrint);window.removeEventListener('afterprint',afterPrint);stopped=true;clearInterval(timer);controller?.abort();document.removeEventListener('visibilitychange',visible);};
  const load=async()=>{
    if(stopped||busy||printing||mount.querySelector('form[data-dirty]'))return;if(!mount.isConnected){dispose();return;}busy=true;
    controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),20000);
    try{
      const response=await fetch(endpoint,{cache:'no-store',credentials:'same-origin',signal:controller.signal,headers:revision?{'If-None-Match':'"'+revision+'"'}:{}});
      if(response.status===304){mount.querySelector('[data-live-status]')?.replaceChildren(document.createTextNode('Live · checked '+new Date().toLocaleTimeString()));return;}
      if(!response.ok||response.redirected)throw Error('Could not refresh underwriting ('+response.status+').');
      const bundle=await response.json();if(stopped||printing||!mount.isConnected||mount.querySelector('form[data-dirty]'))return;
      const open=Array.from(mount.querySelectorAll('details[open]')).map(d=>d.querySelector('summary')?.textContent);
      const scroll=window.scrollY;
      await drawDealUnderwriting(mount,slug,{...options,viewState,endpoint,bundle,reload:()=>{revision='';return load();}});
      mount.querySelectorAll('details').forEach(d=>{if(open.includes(d.querySelector('summary')?.textContent))d.open=true;});
      if(revision)window.scrollTo({top:scroll});revision=bundle.revision;
    }catch(error){if(stopped)return;let status=mount.querySelector('[data-live-status]');if(!status){status=document.createElement('p');status.dataset.liveStatus='';mount.append(status);}status.setAttribute('role','status');status.textContent=(revision?'Showing last loaded data. ':'')+error.message+' Retrying automatically.';}
    finally{clearTimeout(timeout);busy=false;}
  };
  const visible=()=>{if(document.visibilityState==='visible')load();};
  const timer=setInterval(()=>{if(!mount.isConnected){dispose();return;}if(document.visibilityState==='visible')load();},5000);
  document.addEventListener('visibilitychange',visible);load();return dispose;
}

// One reading topic at a time; the complete document remains available.
function diligenceNavigation(host,el,state){
 const topics=[['overview','Overview'],['properties','Properties'],['financials','Financials'],['execution','Execution'],['documents','Documents']];
 if(!topics.some(([id])=>id===state.tab))state.tab='overview';
 const toolbar=el('div','diligence-toolbar'),nav=el('div','diligence-nav'),actions=el('div','diligence-actions');nav.setAttribute('role','tablist');nav.setAttribute('aria-label','Deal sections');toolbar.append(nav,actions);host.append(toolbar);
 const panels={},buttons={};
 function activate(id){state.tab=id;state.all=false;update();}
 function update(){topics.forEach(([id])=>{const selected=state.tab===id;buttons[id].setAttribute('aria-selected',String(selected&&!state.all));buttons[id].tabIndex=selected?0:-1;panels[id].hidden=!state.all&&!selected;panels[id].setAttribute('role',state.all?'region':'tabpanel');});all.textContent=state.all?'Section view':'Read all';all.setAttribute('aria-pressed',String(state.all));}
 topics.forEach(([id,label],index)=>{const button=el('button','',label);button.id='diligence-tab-'+id;button.setAttribute('role','tab');button.setAttribute('aria-controls','diligence-panel-'+id);button.onclick=()=>activate(id);button.onkeydown=e=>{let next;if(e.key==='ArrowRight')next=(index+1)%topics.length;else if(e.key==='ArrowLeft')next=(index+topics.length-1)%topics.length;else if(e.key==='Home')next=0;else if(e.key==='End')next=topics.length-1;else return;e.preventDefault();activate(topics[next][0]);buttons[topics[next][0]].focus();};nav.append(button);buttons[id]=button;const panel=el('div','diligence-panel');panel.id='diligence-panel-'+id;panel.setAttribute('aria-labelledby',button.id);panel.tabIndex=0;panels[id]=panel;host.append(panel);});
 const all=el('button','','Read all');all.onclick=()=>{state.all=!state.all;update();};
 const print=el('button','','Print / PDF');print.onclick=()=>window.print();actions.append(all,print);update();panels.activate=activate;return panels;
}

async function drawDealUnderwriting(host, slug, options) {
  const el=(tag,cls='',text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;};
  const preview=el('div','diligence-preview');
  host.replaceChildren(preview); host=preview;
  if(!options.hideBack){
    const back = el('button','pp3-note',options.backLabel||'← Deal workspace');
    back.onclick=()=>options.onBack?options.onBack():renderDealPage(slug); host.append(back);
  }
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
  const original=data.source?.deal_underwriting||data.source?.lender_diligence||{};
  const inputs=diligencePackageInputs(data),v=Object.fromEntries(Object.entries(inputs).map(([k,x])=>[k,x.value]));
  const saved=original.packageAssumptions;
  const basis={...original,operating:{...original.operating,replacementReservePerUnitYear:v.reserve_years_one_three},presentationFinancing:{...original.presentationFinancing,...(saved?{enabled:true,constructionLtc:v.construction_ltc,constructionRate:v.construction_rate,termMonths:v.term_months,reserveMonths:v.reserve_months,refinanceRate:v.refinance_rate,refinanceAmortYears:v.refinance_years,refinanceLtvHigh:v.refinance_ltv,refinanceLtvLow:Math.min(original.presentationFinancing?.refinanceLtvLow??v.refinance_ltv,v.refinance_ltv)}:{})}};
  const members=data.members||[], assumptions={...data.assumptions,vacancy_rate:v.vacancy_rate,opex_rate:v.opex_rate,exit_cap_rate:v.exit_cap_rate};
  const baseline=(basis.properties||[]).filter(r=>members.some(p=>p.slug===r.slug));
  const memberById=Object.fromEntries(members.map(p=>[p.slug,p]));
  const money=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD',minimumFractionDigits:0,maximumFractionDigits:0}):'—';
  const exactMoney=n=>Number.isFinite(n)?n.toLocaleString('en-US',{style:'currency',currency:'USD'}):'—';
  const paragraph=(parent,text,cls='')=>parent.append(el('p',cls,text));
  const sections={};let panels;
  const groups={summary:'overview',properties:'properties',budget:'financials',financing:'financials',operations:'financials',assumptions:'financials',planning:'execution',progress:'execution',documents:'documents'};
  const section=(id,title)=>{const s=el('section','diligence-section');s.id='diligence-'+id;s.append(el('h3','',title));panels[groups[id]].append(s);sections[id]=s;return s;};
  const table=(parent,headers,rows)=>{const wrap=el('div','diligence-table-wrap');wrap.tabIndex=0;wrap.setAttribute('role','region');wrap.setAttribute('aria-label',headers.join(', '));const numericColumns=headers.map((_,i)=>i>0&&rows.some(row=>/^[$\d]/.test(String(row[i]))||/^[-−]\$/.test(String(row[i]))));const t=el('table','diligence-table');const head=el('thead'),tr=el('tr');headers.forEach((h,i)=>{const th=el('th',numericColumns[i]?'diligence-number':'',h);th.scope='col';tr.append(th);});head.append(tr);t.append(head);const body=el('tbody');rows.forEach(row=>{const r=el('tr');if(/^(Total|Development subtotal|Net operating income|Net cash flow)/.test(String(row[0])))r.className='diligence-total';row.forEach((v,i)=>{const cell=el(i===0?'th':'td','',String(v));if(i===0)cell.scope='row';if(numericColumns[i])cell.className='diligence-number';r.append(cell);});body.append(r);});t.append(body);wrap.append(t);parent.append(wrap);return body;};
  const detail=(parent,title)=>{const d=el('details','diligence-property');d.append(el('summary','',title));parent.append(d);return d;};
  const operating=members.map(p=>diligenceOperating(p,data.sources[p.slug]||{},assumptions,basis.operating||{}));
  const total=key=>operating.length&&operating.every(p=>p&&Number.isFinite(p[key]))?operating.reduce((n,p)=>n+p[key],0):null;
  const expenseRows=members.flatMap(p=>(p.ledger||[]).filter(r=>r.type==='expense').map(r=>({...r,property:p.short,propertySlug:p.slug}))).sort((a,b)=>String(b.date).localeCompare(String(a.date)));
  const paid=expenseRows.reduce((n,r)=>n+r.amount,0);
  const counts=members.map(p=>(p.unitMix||[]).length||p.units||reSrcNum(data.sources[p.slug]||{},'total_units'));
  const units=counts.length&&counts.every(n=>Number.isFinite(n)&&n>0)?counts.reduce((n,x)=>n+x,0):null;
  host.append(el('p','diligence-eyebrow','OODA GROUP · REAL ESTATE'),el('h2','pp3-title',data.deal.name),el('p','re-foot-note','Development overview & due diligence'));
  const live=el('p','re-foot-note','Live · checked '+new Date().toLocaleTimeString());live.dataset.liveStatus='';live.setAttribute('role','status');host.append(live);
  panels=diligenceNavigation(host,el,options.viewState||(options.viewState={tab:'overview',all:false}));
  const summary=section('summary','Project overview');
  const entities=[...new Set(members.map(p=>p.entity).filter(Boolean))];
  paragraph(summary,members.length+' properties'+(units?' · '+units+' planned residences':'')+(entities.length?' · '+entities.join(', '):''),'diligence-lead');

  const metrics=el('div','diligence-metrics');
  const overviewBudget=baseline.length===members.length&&baseline.length?baseline.reduce((n,r)=>n+r.acquisition+r.hardCostsIncludingContingency+r.softCosts,0):null;
  const overviewTerms=basis.presentationFinancing;
  const overviewLoan=overviewBudget!==null&&overviewTerms?.enabled&&['constructionLtc','constructionRate','reserveMonths'].every(k=>Number.isFinite(overviewTerms[k]))?baseline.reduce((n,r)=>{const principal=Math.round((r.acquisition+r.hardCostsIncludingContingency+r.softCosts)*overviewTerms.constructionLtc*100)/100;return n+principal+Math.round(principal*overviewTerms.constructionRate*overviewTerms.reserveMonths/12*100)/100;},0):null;
  [['Development budget',money(overviewBudget)],['Illustrative loan · incl. reserve',exactMoney(overviewLoan)],['Proposed monthly rent',money(total('gross')===null?null:total('gross')/12)],['Stabilized annual NOI',money(total('noi'))]].forEach(([label,value])=>{const m=el('div');m.append(el('span','',label),el('strong','',value));metrics.append(m);});summary.append(metrics);
  if(overviewLoan!==null){paragraph(summary,(overviewTerms.constructionRate*100).toFixed(2)+'% interest only · '+overviewTerms.termMonths+' months · '+(overviewTerms.constructionLtc*100).toFixed(0)+'% base loan-to-cost','diligence-terms');paragraph(summary,'Development budget includes hard-cost contingency; additional closing and financing fees '+(v.closing_costs===null?'are unquantified.':'total '+money(v.closing_costs)+'.'),'re-foot-note');}
  const assumptionsSection=section('assumptions','Lender ask assumptions');
  paragraph(assumptionsSection,saved?.name||'Current package');
  const timeline=saved?.dates||{},end=timeline.completion_target&&Number.isFinite(v.lease_up_days)?new Date(Date.parse(timeline.completion_target+'T00:00:00Z')+v.lease_up_days*86400000).toISOString().slice(0,10):null;
  const milestones=el('dl','diligence-timeline');
  const displayDate=value=>value?new Date(value+'T00:00:00Z').toLocaleDateString('en-US',{month:'short',day:'numeric',year:'numeric',timeZone:'UTC'}):'Not established';
  [['Construction started',displayDate(timeline.construction_start)],['Completion target',displayDate(timeline.completion_target)],['Lease-up target',displayDate(end)]].forEach(([label,value])=>{const item=el('div');item.append(el('dt','',label),el('dd','',value));milestones.append(item);});summary.append(el('h4','','Execution targets'),milestones);
  paragraph(summary,'Lease-up allowance: '+(v.lease_up_days===null?'Not established':v.lease_up_days+' days')+'.','re-foot-note');
  if(basis.repayment)paragraph(summary,basis.repayment,'re-foot-note');
  const explore=el('div','diligence-overview-links');
  [['properties','Properties & unit rents'],['financials','Budget, financing & cash flow'],['execution','Schedule & recorded spending'],['documents','Plans & supporting documents']].forEach(([id,label])=>{const button=el('button','',label+' →');button.onclick=()=>panels.activate(id);explore.append(button);});summary.append(explore);
  const assumptionDetails=detail(assumptionsSection,'Assumption schedule');
  const assumptionGroups=[
    ['Leasing & operations',['lease_up_days','vacancy_rate','opex_rate','rent_growth','opex_growth']],
    ['Capital reserves · per residence / year',['reserve_years_one_three','reserve_years_four_six','reserve_years_seven_eight','reserve_years_nine_plus']],
    ['Hold & disposition',['hold_years','selling_cost_pct','exit_cap_rate']],
    ['Construction financing',['construction_ltc','construction_rate','term_months','reserve_months','closing_costs']],
    ['Refinance',['refinance_rate','refinance_years','refinance_ltv']]
  ];
  const assumptionGrid=el('div','diligence-assumption-groups');assumptionDetails.append(assumptionGrid);
  assumptionGroups.forEach(([title,keys])=>{
    const group=el('div');group.append(el('h4','',title));
    table(group,['Assumption','Value'],keys.map(k=>{
      const [,label,unit]=diligencePackageFields.find(f=>f[0]===k);
      const displayLabel=k.startsWith('reserve_years_')?label.split(' · ')[1]:label;
      return [displayLabel,v[k]===null?'Not established':unit==='%'?Number((v[k]*100).toFixed(2))+'%':unit==='$'?money(v[k]):v[k]+' '+unit];
    }));assumptionGrid.append(group);
  });
  if(saved?.valuationBasis?.selectedRate===v.exit_cap_rate){paragraph(assumptionDetails,'Cap-rate status: '+saved.valuationBasis.status);(saved.valuationBasis.sources||[]).forEach(ref=>{if(/^https:\/\//.test(ref.url)){const a=el('a','',ref.title);a.href=ref.url;a.target='_blank';a.rel='noopener';assumptionDetails.append(a,el('br'));}});}
  if(options.endpoint.startsWith('/api/deals/')){
    const sharing=el('button','','Lender links');sharing.onclick=()=>openDealSharing(slug);assumptionsSection.append(sharing);
    const edit=detail(assumptionsSection,'Edit this lender ask');
    paragraph(edit,'Saved values apply only to this deal package. Blank fields remain unestablished. Existing deal values are starting inputs; review before saving.');
    const form=el('form','diligence-assumptions-form'),nameLabel=el('label','','Ask name'),name=el('input');name.type='text';name.required=true;name.maxLength=120;name.value=saved?.name||data.deal.name;nameLabel.append(name);form.append(nameLabel);
    const dateControls={};[['construction_start','Construction start'],['completion_target','Completion target']].forEach(([key,label])=>{const l=el('label','',label),i=el('input');i.type='date';i.value=saved?.dates?.[key]||'';l.append(i);form.append(l);dateControls[key]=i;});
    const controls={};
    diligencePackageFields.forEach(([key,label,unit,min,max])=>{const l=el('label','',label+' ('+unit+')'),input=el('input');input.type='number';input.min=min;input.max=max;input.step=unit==='years'||unit==='months'||unit==='days'?'1':'any';input.value=v[key]===null?'':String(unit==='%'?Number((v[key]*100).toFixed(6)):v[key]);l.append(input,el('small','',inputs[key].origin));form.append(l);controls[key]=input;});
    const status=el('p');status.setAttribute('role','status');
    const save=el('button','','Save assumptions'),cancel=el('button','','Discard edits / reload');cancel.type='button';cancel.onclick=()=>{delete form.dataset.dirty;options.reload();};
    form.oninput=()=>{form.dataset.dirty='true';status.textContent='Unsaved changes · live refresh paused';};
    form.onsubmit=async event=>{event.preventDefault();if(!form.reportValidity())return;save.disabled=true;form.dataset.dirty='true';const values={};diligencePackageFields.forEach(([key,,unit])=>{const value=controls[key].value;values[key]=value===''?null:Number(value)/(unit==='%'?100:1);});try{const r=await fetch(options.endpoint+'/assumptions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({revision:data.revision,name:name.value,values,dates:Object.fromEntries(Object.entries(dateControls).map(([key,i])=>[key,i.value]))})});if(!r.ok)throw Error(await r.text());delete form.dataset.dirty;await options.reload();}catch(e){status.textContent=e.message;}finally{save.disabled=false;}};
    form.append(save,cancel,status);edit.append(form);
  }
  const properties=section('properties','Property schedule');
  table(properties,['Property','Rehab order','Ownership entity','Current stage','Residences','Proposed rent / month'],members.map((p,i)=>[p.short,baseline.find(r=>r.slug===p.slug)?.phase||'—',p.entity||'—',(p.status||'—').replaceAll('_',' '),operating[i]?.units||'—',money(operating[i]?operating[i].gross/12:null)]));
  members.forEach(p=>{const d=detail(properties,p.short+' · proposed unit mix');table(d,['Unit','Beds / baths','Proposed area (SF)','Monthly asking rent'],(p.unitMix||[]).map(u=>[u.label,[u.beds??'—',u.baths??'—'].join(' / '),u.sqft||'—',money(u.rent)]));paragraph(d,'Source: current property unit schedule. Proposed areas; permitted unit count and net rentable area require supporting plans.','re-foot-note');});
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
  const operatingBasis=detail(operations,'Operating assumptions & calculation basis');
  table(operatingBasis,['Operating assumption','Current input'],[['Vacancy / credit loss',Number.isFinite(assumptions.vacancy_rate)?(assumptions.vacancy_rate*100).toFixed(1)+'% of gross rent':'Not established'],['Operating expense allowance',Number.isFinite(assumptions.opex_rate)?(assumptions.opex_rate*100).toFixed(1)+'% of effective gross income':'Not established'],['Replacement reserves · years 1–3',Number.isFinite(basis.operating?.replacementReservePerUnitYear)?money(basis.operating.replacementReservePerUnitYear)+' / residence / year':'Not established']]);
  paragraph(operatingBasis,'Proposed rents are not collected income. NOI is before replacement reserves and debt service. Expenses use an aggregate allowance; a detailed operating budget is not yet established.','re-foot-note');
  const operatingDetail=detail(operations,'Operating proforma by property');
  table(operatingDetail,['Property','Annual gross rent','Vacancy','Operating expenses','Annual NOI'],members.map((p,i)=>{const u=operating[i];return [p.short,money(u?.gross),money(u?u.gross-u.egi:null),money(u?u.egi-u.noi:null),money(u?.noi)];}));
  const projections=detail(operations,'Growth projection and disposition');
  const forecast=saved?diligenceProjection(gross,noi,units,v):[];
  if(!forecast.length)paragraph(projections,'Save the deal growth rates, hold period, and operating assumptions to calculate this schedule.');
  else {
    table(projections,['Projection year','Gross rent','Operating expenses','NOI','Replacement reserve','NCF before debt'],forecast.slice(0,-1).map(r=>[r.year,money(r.gross),money(r.opex),money(r.noi),r.reserve===null?'Not established':money(r.reserve),r.ncf===null?'Not established':money(r.ncf)]));
    paragraph(projections,'Year 1 uses stabilized rents. Rent and operating costs grow independently. Replacement reserves follow the entered age ladder. Development timing, draws and lease-up are excluded; this is not the project investment cash flow.','re-foot-note');
    const exit=forecast.at(-1),value=Number.isFinite(v.exit_cap_rate)&&v.exit_cap_rate>0?exit.noi/v.exit_cap_rate:null,cost=value!==null&&Number.isFinite(v.selling_cost_pct)?value*v.selling_cost_pct:null;
    table(projections,['Disposition calculation','Amount'],[['Forward-year NOI',money(exit.noi)],['Value at selected cap rate',money(value)],['Selling costs',cost===null?'Not established':money(cost)],['Proceeds before loan payoff',cost===null?'Not established':money(value-cost)]]);
    paragraph(projections,'Loan payoff, equity proceeds, IRR, and equity multiple require the dated financing and investment cash flows.','re-foot-note');
  }
  if(basis.presentationFinancing?.enabled && budgetRows.length===members.length){
    const config=basis.presentationFinancing;
    const terms={...basis,constructionRate:config.constructionRate,reserveMonths:config.reserveMonths,refinanceRate:config.refinanceRate,refinanceAmortYears:config.refinanceAmortYears,refinanceLtvLow:config.refinanceLtvLow,refinanceLtvHigh:config.refinanceLtvHigh};
    const valid=['constructionLtc','constructionRate','reserveMonths','termMonths','refinanceRate','refinanceAmortYears','refinanceLtvLow','refinanceLtvHigh'].every(k=>Number.isFinite(config[k]));
    if(valid){
      const financing=section('financing','Illustrative financing');

      const rows=baseline.map(r=>({...r,baseLoan:Math.round((r.acquisition+r.hardCostsIncludingContingency+r.softCosts)*config.constructionLtc*100)/100}));
      const stacks=rows.map(r=>diligenceStack(r,terms));
      const sum=k=>stacks.reduce((n,r)=>n+r[k],0),baseLoan=rows.reduce((n,r)=>n+r.baseLoan,0);
      const loanBasis=detail(financing,'Loan terms & leverage calculations');
      table(loanBasis,['Construction assumptions','Illustrative input'],[['Loan-to-cost',(config.constructionLtc*100).toFixed(0)+'% of development subtotal'],['Interest rate',(config.constructionRate*100).toFixed(2)+'% · interest only'],['Term',config.termMonths+' months'],['Interest reserve',config.reserveMonths+' months on base construction principal'],['Total loan / development subtotal',(sum('request')/budgetTotal('total')*100).toFixed(2)+'% · includes financed reserve'],['Total loan / identified uses',(sum('request')/(budgetTotal('total')+sum('reserve')+(v.closing_costs??0))*100).toFixed(2)+'%']]);
      table(financing,['Sources','Amount','Uses','Amount'],[['Construction principal',money(baseLoan),'Development subtotal',money(budgetTotal('total'))],['Financed interest reserve',money(sum('reserve')),'Interest reserve',money(sum('reserve'))],['Sponsor / partner equity',money(sum('equity')+(v.closing_costs??0)),'Additional closing / financing costs',v.closing_costs===null?'Not established':money(v.closing_costs)],['Total capital',money(sum('request')+sum('equity')+(v.closing_costs??0)),'Total identified uses',money(budgetTotal('total')+sum('reserve')+(v.closing_costs??0))]]);
      paragraph(financing,'Illustrative sizing. '+(v.closing_costs===null?'Closing costs are unquantified and excluded.':'Additional closing costs are funded by equity.')+' Interest carry uses full base principal; actual interest depends on draws.','re-foot-note');
      if(basis.structure)paragraph(financing,basis.structure,'re-foot-note');
      const perProperty=detail(financing,'Capital requirements by property');
      table(perProperty,['Property','Base loan','Interest reserve','Total loan','Equity'],rows.map((r,i)=>[memberById[r.slug]?.short||r.slug,money(r.baseLoan),exactMoney(stacks[i].reserve),exactMoney(stacks[i].request),money(stacks[i].equity)]));
      if(Number.isFinite(basis.fundEquityShare)&&Number.isFinite(basis.partnerEquityShare)&&basis.fundEquityShare>=0&&basis.partnerEquityShare>=0&&Math.abs(basis.fundEquityShare+basis.partnerEquityShare-1)<1e-9){
        const funding=detail(financing,'Equity funding sources');
        table(funding,['Property','Fund equity · '+(basis.fundEquityShare*100).toFixed(0)+'%','Partner equity · '+(basis.partnerEquityShare*100).toFixed(0)+'%','Total required'],[...rows.map((r,i)=>[memberById[r.slug]?.short||r.slug,exactMoney(stacks[i].fund),exactMoney(stacks[i].partners),exactMoney(stacks[i].equity)]),['Total',exactMoney(sum('fund')),exactMoney(sum('partners')),exactMoney(sum('equity'))]]);
        paragraph(funding,'Development equity funding plan; amounts contributed and remaining cash require reconciliation. Additional financing and closing costs are excluded from this split.','re-foot-note');
      }
      const refinance=detail(financing,'Stabilized refinance illustration');
      if(basis.repayment){table(refinance,['Repayment plan','Recorded terms'],[['Refinance',basis.repayment]]);paragraph(refinance,'The lease-up target does not establish that the refinance occupancy requirement has been met.','re-foot-note');}
      table(refinance,['Refinance assumptions','Input'],[['Interest rate',(config.refinanceRate*100).toFixed(2)+'%'],['Amortization',config.refinanceAmortYears+' years'],['Loan-to-value',(config.refinanceLtvLow*100).toFixed(0)+'–'+(config.refinanceLtvHigh*100).toFixed(0)+'%'],['Capitalization rate',Number.isFinite(assumptions.exit_cap_rate)?(assumptions.exit_cap_rate*100).toFixed(2)+'%':'Not established']]);
      table(refinance,['Property','Income-based value','LTV-only capacity','Annual debt service¹','NOI coverage¹','NCF coverage²'],rows.map(r=>{const u=operating[members.findIndex(p=>p.slug===r.slug)];const x=u?diligenceRefinance({...r,units:[{rent:u.gross/12}]},terms,assumptions):null;return [memberById[r.slug]?.short||r.slug,money(x?.value),money(x?.high),money(x?.debt),x?.debt?(x.noi/x.debt).toFixed(2)+'×':'—',x?.debt&&u?.ncf!==null&&Number.isFinite(u?.ncf)?(u.ncf/x.debt).toFixed(2)+'×':'Not established'];}));
      paragraph(refinance,'¹ Debt service amortizes the full construction loan including interest reserve. Coverage uses NOI before replacement reserves. Income-based value is NOI divided by the displayed cap rate, not an appraisal. ² NCF coverage deducts replacement reserves. LTV-only capacity is not available proceeds: coverage limits, closing costs, and other payoff obligations are not deducted.','re-foot-note');
    }
  }
  const planning=section('planning','Construction and draw planning');
  const phases=detail(planning,'Phase-level planning inputs');
  members.forEach(p=>{phases.append(el('h4','',p.short));table(phases,['Phase','Current estimate','Duration','Status'],(p.work||[]).map(w=>[w.text,diligencePhaseCost(w)>0?money(diligencePhaseCost(w)):'Not estimated',w.weeks>0?Number(w.weeks.toFixed(2))+' weeks':'Not entered',w.checked?'Complete':'Open']));});
  const spending=diligenceSpendingPlan(timeline.construction_start,timeline.completion_target,budgetTotal('hard')===null||budgetTotal('soft')===null?null:budgetTotal('hard')+budgetTotal('soft'));
  if(spending.length){
    const schedule=detail(planning,'Monthly construction spending illustration');
    table(schedule,['Month','Planned hard + soft costs','Recorded non-acquisition expenses'],spending.map(r=>{const entries=expenseRows.filter(e=>e.date?.startsWith(r.month)&&(e.category||e.cat)!=='acquisition');return [r.month,money(r.amount),entries.length?exactMoney(entries.reduce((n,e)=>n+e.amount,0)):'No entries'];}));
    paragraph(schedule,'Cost-weighted phase durations distribute the confirmed hard/soft budget across the construction period. This produces a constant daily spending illustration. It excludes acquisition, financing costs and retainage; it is not an approved draw schedule or actual funding history.','re-foot-note');
  }
  const allocation=detail(planning,'Phase allocation and budget check');
  table(allocation,['Property','Current phase estimates','Confirmed hard + soft budget','Difference'],members.map(p=>{const r=baseline.find(b=>b.slug===p.slug),amount=(p.work||[]).reduce((n,w)=>n+diligencePhaseCost(w),0),target=r?r.hardCostsIncludingContingency+r.softCosts:null;return [p.short,money(amount),money(target),target===null?'—':money(amount-target)];}));
  paragraph(planning,'Forecast costs require phase amounts and durations. Lender advances also require funding history, eligible-cost rules, inspection evidence and any agreed retainage.','re-foot-note');
  const progress=section('progress','Live project records');
  paragraph(progress,'Recorded expenditures: '+exactMoney(paid)+'. Updated from the property ledgers as entries are added.');
  table(progress,['Property','Recorded expenditures','Work phases complete'],members.map(p=>[p.short,exactMoney((p.ledger||[]).filter(r=>r.type==='expense').reduce((n,r)=>n+r.amount,0)),(p.work||[]).filter(w=>w.checked).length+' / '+(p.work||[]).length]));
  const expenses=detail(progress,'Expense ledger · '+expenseRows.length+' entries');
  table(expenses,['Date','Property','Payee / description','Category','Amount'],expenseRows.map(r=>[r.date,r.property,[r.contractor||r.vendor,r.note].filter(Boolean).join(' · '),r.category||r.cat||'—',exactMoney(r.amount)]));
  paragraph(expenses,'Recorded ledger entries only. Contracts are commitments and are not added to cash expenditures.','re-foot-note');
  if((basis.unmatchedPayments||[]).length)table(expenses,['Additional reported payments · pending allocation','Amount'],basis.unmatchedPayments.map(p=>[p.description,exactMoney(p.amount)]));
  const documents=section('documents','Plans and documents');
  paragraph(documents,'Plans, authorizations, and reference material. Draft plans do not establish permit approval; reference appraisals apply only to the property identified.');
  const docResults=await Promise.allSettled(members.map(p=>read('/api/properties/'+encodeURIComponent(p.slug)+'/docs')));
  members.forEach((p,i)=>{const sub=el('div','diligence-doc-group');sub.append(el('h4','',p.short));const result=docResults[i];if(result.status==='rejected'){paragraph(sub,'Documents could not be loaded. Return to the workspace and retry; this is not an empty document inventory.');}else {const docs=result.value.docs||[];if(!docs.length)paragraph(sub,'Documents not yet available.');docs.forEach(d=>{const a=el('a','',d.name);a.href=docLink(d.path);a.target='_blank';a.rel='noopener';sub.append(a);});}documents.append(sub);});

  if((basis.supportingDocuments||[]).length){
    const refs=el('div','diligence-doc-group');refs.append(el('h4','','Deal reference documents'));
    (basis.supportingDocuments||[]).forEach(d=>{
      if(!d.path?.startsWith('system/realestate/docs/')) return;
      const a=el('a','',d.title);a.href=docLink(d.path);a.target='_blank';a.rel='noopener';refs.append(a);paragraph(refs,members.some(p=>d.path.includes('/'+p.slug+'/'))?'Member property document':'Reference property outside this deal','re-foot-note');
    });documents.append(refs);
  }
  // Contract attachments are a separate store from property-folder files.
  try {
    const contracts = (await read('/api/realestate/contracts')).contracts || [];
    const linked = contracts.filter(c => (c.allocations || []).some(a => memberById[a.property]));
    const committed = linked.filter(c => c.status === 'accepted');
    const contractDetails=detail(documents,'Contracts & receipts · '+linked.length+' contracts');
    const attachmentLink = (ref,label) => {
      if (!ref) return null;
      let href='';
      if (/^sha256:[a-f0-9]{64}$/.test(ref)) href=docLink(ref);
      else if (/^https?:\/\//.test(ref)) href=ref;
      else if (ref.startsWith('system/realestate/docs/')) href=docLink(ref);
      if (!href) return null;
      const a=el('a','',label);a.href=href;a.target='_blank';a.rel='noopener';return a;
    };
    const contractRows=table(contractDetails,['Contract','Status','Allocated to this deal','Document'],linked.map(c => [c.name,c.status,money((c.allocations||[]).filter(a=>memberById[a.property]).reduce((n,a)=>n+a.amount,0)),c.doc?'Document unavailable':'No file attached']));
    const seen = new Set();
    linked.forEach((c,i)=>{const a=attachmentLink(c.doc,'View document ↗');if(a){a.setAttribute('aria-label','View document for '+c.name+' (opens in a new tab)');contractRows.children[i].children[3].replaceChildren(a);seen.add(c.doc);}});
    const addDocument = (ref,label) => {
      if (!ref || seen.has(ref)) return;
      const a=attachmentLink(ref,label);if(!a)return;
      seen.add(ref);contractDetails.append(a,el('br'));
    };
    expenseRows.forEach(r=>addDocument(r.doc && !r.doc.includes('/') && !r.doc.startsWith('sha256:')?'system/realestate/docs/'+r.propertySlug+'/'+r.doc:r.doc,r.property+' · '+r.date+' · receipt'));
    paragraph(contractDetails,'Accepted contract allocations: '+money(committed.reduce((n,c)=>n+(c.allocations||[]).filter(a=>memberById[a.property]).reduce((k,a)=>k+a.amount,0),0))+'. Contract amounts are commitments, not additional expenses.');
  } catch(e) { paragraph(documents,'Contract inventory could not be loaded: '+e.message); }
  ['budget','financing','operations','assumptions'].forEach(id=>{if(sections[id])panels.financials.append(sections[id]);});
  panels.execution.append(progress,planning);

}

function openDealSharing(slug){
 const dialog=document.createElement('dialog');dialog.className='diligence-share-dialog';
 const node=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
 const title=node('h2','Lender links'),intro=node('p','Each password-gated link opens only this deal, with live expenses, underwriting, work progress and linked plans. Internal correspondence and task chats are excluded. New linked documents and expenses appear automatically.');
 const form=node('form'),label=node('input');label.placeholder='Lender or purpose';label.required=true;label.maxLength=120;label.setAttribute('aria-label','Link label');
 const days=node('input');days.type='number';days.min=1;days.max=365;days.value=90;days.required=true;days.setAttribute('aria-label','Expires in days');
 const daysLabel=node('label','Expires in days ');daysLabel.append(days);
 const create=node('button','Create password-gated link');create.type='submit';form.append(label,daysLabel,create);
 const result=node('div'),status=node('p'),list=node('div'),close=node('button','Done');status.setAttribute('role','status');close.onclick=()=>dialog.close();dialog.addEventListener('close',()=>dialog.remove());
 dialog.append(title,intro,form,result,status,list,close);document.body.append(dialog);dialog.showModal();
 const endpoint='/api/deals/'+encodeURIComponent(slug)+'/shares';
 async function refresh(){const r=await fetch(endpoint,{cache:'no-store'});if(!r.ok)throw Error(await r.text());const data=await r.json();list.replaceChildren();(data.shares||[]).sort((a,b)=>b.created.localeCompare(a.created)).forEach(sh=>{const row=node('div'),expired=new Date(sh.expires)<=new Date();row.className='diligence-share-row';const text=node('span',sh.label+' · '+(sh.revoked?'Revoked':expired?'Expired':'Expires '+new Date(sh.expires).toLocaleDateString()));row.append(text);if(!sh.revoked&&!expired){const link=node('a','Open');link.href='https://portal.ooda.group/lender/'+sh.id+'/';link.target='_blank';link.rel='noopener';const revoke=node('button','Revoke');revoke.onclick=async()=>{revoke.disabled=true;try{const r=await fetch(endpoint+'/'+encodeURIComponent(sh.id),{method:'DELETE'});if(!r.ok)throw Error(await r.text());await refresh();}catch(e){status.textContent=e.message;revoke.disabled=false;}};row.append(link,revoke);}list.append(row);});}
 form.onsubmit=async e=>{e.preventDefault();create.disabled=true;status.textContent='';try{const r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({label:label.value,days:Number(days.value)})});if(!r.ok)throw Error(await r.text());const x=await r.json();result.replaceChildren(node('h3','Save this password now'),node('p','The password is shown once. Send it separately from the link.'));
 for(const [name,value] of [['Link','https://portal.ooda.group'+x.path],['Password',x.password]]){const field=node('label',name+' '),input=node('input');input.value=value;input.readOnly=true;input.setAttribute('aria-label',name);input.onclick=()=>input.select();field.append(input);result.append(field);}await refresh();}catch(e){status.textContent=e.message;}finally{create.disabled=false;}};
 refresh().catch(e=>status.textContent=e.message);
}
