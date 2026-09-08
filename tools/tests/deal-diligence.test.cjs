const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const vm=require('node:vm');
const c=vm.createContext({});c.window=c;vm.runInContext(fs.readFileSync('server/web/js/77-re-screening.js','utf8'),c);vm.runInContext(fs.readFileSync('server/web/js/86-deal-diligence.js','utf8'),c);
test('email financing baseline includes contingency once and corrected reserve',()=>{
 const basis={constructionRate:.0625,reserveMonths:12,fundEquityShare:.9,partnerEquityShare:.1};
 const hard=[256000,277000,284000,292500],loans=[202300,217000,221900,239750];
 const rows=loans.map((loan,i)=>c.diligenceStack({acquisition:i===3?35000:18000,hardCostsIncludingContingency:hard[i],softCosts:15000,baseLoan:loan,units:[{rent:1750},{rent:i===2?1950:1750},...(i===2?[]:[{rent:1200}])]},basis));
 assert.equal(rows.reduce((n,r)=>n+r.development,0),1258500);
 assert.equal(Math.round(rows.reduce((n,r)=>n+r.reserve,0)*100)/100,55059.38);
 assert.equal(Math.round(rows.reduce((n,r)=>n+r.request,0)*100)/100,936009.38);
 assert.equal(rows[0].partners,8670);
 assert.equal(rows.reduce((n,r)=>n+r.monthlyRent,0),17800);
});
test('refinance scenario uses full request, email rate, and identifies a shortfall',()=>{
 const row={acquisition:18000,hardCostsIncludingContingency:256000,softCosts:15000,baseLoan:202300,units:[{rent:1750},{rent:1750},{rent:1200}]};
 const b={constructionRate:.0625,reserveMonths:12,fundEquityShare:.9,partnerEquityShare:.1,refinanceLtvLow:.7,refinanceLtvHigh:.75,refinanceRate:.07,refinanceAmortYears:25};
 const r=c.diligenceRefinance(row,b,{vacancy_rate:.08,opex_rate:.35,exit_cap_rate:.15});
 assert.equal(r.repayment,214943.75);assert.equal(r.gross,56400);assert.equal(r.noi,33727.2);
 assert.ok(r.gap>0);assert.ok(r.dscr>0);assert.equal(c.diligenceRefinance(row,b,{}),null);
});

test('live operating forecast preserves missing reserves and follows current rents',()=>{
 const a={vacancy_rate:.08,opex_rate:.35,exit_cap_rate:.0725,capex_per_unit_year:350};
 const p={units:3,rentMonthly:4700};
 const initial=c.diligenceOperating(p,{hard_costs:100000},a,{});
 assert.equal(Math.round(initial.noi*100)/100,33727.2);assert.equal(initial.reserve,null);assert.equal(initial.ncf,null);
 const configured=c.diligenceOperating(p,{hard_costs:100000},a,{replacementReservePerUnitYear:350});
 assert.equal(configured.reserve,1050);assert.equal(Math.round(configured.ncf*100)/100,32677.2);
 assert.ok(c.diligenceOperating({...p,rentMonthly:4800},{},a).noi>initial.noi);
 assert.equal(c.diligenceOperating(p,{},{}),null);
 assert.equal(c.diligenceOperating(p,{},a,{replacementReservePerUnitYear:-350}).reserve,null);
 assert.equal(c.diligenceOperating(p,{}, {...a,vacancy_rate:1.2}),null);
});

test('presentation renders operating facts and budget without negotiation history',async()=>{
 class Element {
  constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.className='';this.textContent='';}
  append(...nodes){this.children.push(...nodes);}
  replaceChildren(...nodes){this.children=nodes;}
  setAttribute(){}
  insertBefore(node,before){this.children.splice(this.children.indexOf(before),0,node);}
  remove(){}
  text(){return this.textContent+' '+this.children.map(n=>typeof n==='string'?n:n.text()).join(' ');}
 }
 c.document={createElement:tag=>new Element(tag)};
 const host=new Element('div');
 const options={endpoint:'/test',bundle:{
  deal:{name:'Rehab'},source:{deal_underwriting:{title:'Rehabilitation portfolio',lender:'SECRET LENDER',source:'PRIVATE EMAIL',openItems:['INTERNAL FOLLOWUP'],corrections:['PRIVATE CORRECTION'],constructionRate:.0625,termMonths:36,contingencyPct:.2,properties:[{slug:'one',acquisition:18000,hardCostsIncludingContingency:256000,softCosts:15000,baseLoan:202300,units:[{label:'A',rent:4700}]}]}},
  members:[{slug:'one',short:'One',unitMix:[{label:'Live unit label',rent:4700}],units:3,rentMonthly:4700,ledger:[{type:'expense',amount:1234.56,date:'2026-09-08'}]}],sources:{one:{}},docs:{one:[]},contracts:[],assumptions:{vacancy_rate:.08,opex_rate:.35}
 }};
 await c.drawDealUnderwriting(host,'test',options);
 const text=host.text().replace(/\s+/g,' ');
 assert.match(text,/Live unit label/);assert.match(text,/\$289,000/);assert.match(text,/\$1,234.56/);assert.match(text,/\$33,727/);
 assert.match(text,/Net cash flow before financing Not established/);
 for(const forbidden of ['SECRET LENDER','PRIVATE EMAIL','INTERNAL FOLLOWUP','PRIVATE CORRECTION','interest-only','Loan + reserve','DSCR'])assert.ok(!text.includes(forbidden),forbidden);
 options.bundle.source.deal_underwriting.presentationFinancing={enabled:true,constructionLtc:.7,constructionRate:.0625,reserveMonths:12,termMonths:36,refinanceRate:.07,refinanceAmortYears:25,refinanceLtvLow:.7,refinanceLtvHigh:.75};
 options.bundle.source.deal_underwriting.fundEquityShare=.9; options.bundle.source.deal_underwriting.partnerEquityShare=.1; options.bundle.source.deal_underwriting.repayment='90 days at 90%+ occupancy'; options.bundle.source.deal_underwriting.properties[0].phase=1;
 options.bundle.assumptions.exit_cap_rate=.0725;
 await c.drawDealUnderwriting(host,'test',options);
 const financed=host.text();assert.match(financed,/Illustrative financing/);assert.match(financed,/70% of development subtotal/);assert.match(financed,/74.38%/);assert.match(financed,/NCF coverage²/);assert.match(financed,/\$214,943.75/);assert.ok(!financed.includes('SECRET LENDER'));assert.ok(!financed.includes('PRIVATE EMAIL'));assert.match(financed,/Equity funding sources/);assert.match(financed,/\$78,030.00/);assert.match(financed,/\$8,670.00/);assert.match(financed,/90 days at 90%\+ occupancy/);assert.match(financed,/Rehab order/);
});

test('growth projection uses independent expense growth and exact reserve boundaries',()=>{
 const v={rent_growth:.03,opex_growth:.02,hold_years:10,vacancy_rate:.08,reserve_years_one_three:250,reserve_years_four_six:500,reserve_years_seven_eight:750,reserve_years_nine_plus:1000};
 const rows=c.diligenceProjection(213600,127732.8,11,v);
 assert.equal(rows.length,11);assert.equal(rows[0].reserve,2750);assert.equal(rows[2].reserve,2750);assert.equal(rows[3].reserve,5500);assert.equal(rows[5].reserve,5500);assert.equal(rows[6].reserve,8250);assert.equal(rows[7].reserve,8250);assert.equal(rows[8].reserve,11000);
 assert.ok(Math.abs(rows[1].gross-213600*1.03)<.001);assert.ok(Math.abs(rows[1].opex-(213600*.92-127732.8)*1.02)<.001);
 assert.equal(c.diligenceProjection(100,50,1,{}).length,0);
});

test('package nulls override inherited values and scopes do not mutate globals',()=>{
 const data={source:{rent_growth:.03,deal_underwriting:{packageAssumptions:{values:{exit_cap_rate:null,rent_growth:.02}}}},assumptions:{exit_cap_rate:.0725}};
 const resolved=c.diligencePackageInputs(data);assert.equal(resolved.exit_cap_rate.value,null);assert.equal(resolved.rent_growth.value,.02);assert.equal(data.assumptions.exit_cap_rate,.0725);
});

test('construction spending totals balance and financed contingency is not counted twice',()=>{
 const rows=c.diligenceSpendingPlan('2026-08-01','2027-03-01',1169500);
 assert.equal(rows.length,7);assert.equal(rows[0].month,'2026-08');assert.equal(rows[6].month,'2027-02');assert.ok(Math.abs(rows.reduce((n,r)=>n+r.amount,0)-1169500)<.001);
 const u=c.reScreen({units:3,rentMonthly:4700,work:[{estTotal:277000}]},{purchase_price:18000,carry_cost:15000,phase_costs_include_contingency:true},{vacancy_rate:.08,opex_rate:.35,contingency_pct:.05});assert.equal(u.tdc,310000);assert.equal(u.contingency,0);
 assert.equal(c.diligencePhaseCost({estTotal:0,fields:[{key:'soft-budget',value:'15000'}]}),15000);
});


test('property record overrides outrank defaults without changing a named lender scenario',()=>{
 const defaults={construction_interest_rate:.1,construction_loan_ltc:.679,perm_interest_rate:.0625,perm_amort_years:25,perm_ltv:.75,exit_cap_rate:.0725,vacancy_rate:.08,opex_rate:.35};
 const source={construction_interest_rate:.0625,construction_loan_ltc:.7,perm_interest_rate:.07,exit_cap_rate:.085};
 const effective=c.reScreeningAssumptions(defaults,source);
 assert.equal(effective.construction_interest_rate,.0625);assert.equal(effective.construction_loan_ltc,.7);assert.equal(effective.perm_interest_rate,.07);assert.equal(effective.exit_cap_rate,.085);
 assert.equal(defaults.perm_interest_rate,.0625);
 const property={units:3,rentMonthly:4700};
 assert.notEqual(c.reScreen(property,source,effective).dscr,c.reScreen(property,source,defaults).dscr);
 const ask={...defaults,exit_cap_rate:.09};
 assert.equal(c.reScreen(property,source,ask).arv,c.reScreen(property,{},ask).arv);
});

test('section navigation supports keyboard, full reading, and refresh retention',()=>{
 class Node {
  constructor(tag,cls,text){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.attrs={};}
  append(...nodes){this.children.push(...nodes);}
  setAttribute(k,v){this.attrs[k]=v;}
  focus(){this.focused=true;}
 }
 const el=(...args)=>new Node(...args),host=el('div'),state={tab:'overview',all:false};
 let panels=c.diligenceNavigation(host,el,state);
 const visible=()=>Object.keys(panels).filter(k=>k!=='activate'&&!panels[k].hidden);
 assert.deepEqual(visible(),['overview']);
 const [nav,actions]=host.children[0].children;
 nav.children[2].onclick();assert.deepEqual(visible(),['financials']);
 nav.children[2].onkeydown({key:'ArrowRight',preventDefault(){}});
 assert.deepEqual(visible(),['execution']);assert.equal(nav.children[3].focused,true);
 actions.children[0].onclick();assert.equal(visible().length,5);
 panels=c.diligenceNavigation(el('div'),el,state);assert.equal(visible().length,5);
 panels.activate('documents');assert.deepEqual(visible(),['documents']);
 panels=c.diligenceNavigation(el('div'),el,state);assert.deepEqual(visible(),['documents']);
});

test('carry bridge balances costs, debt and equity without treating actuals as draws',()=>{
 const v={construction_ltc:.7,construction_rate:.0625,reserve_months:12,term_months:36,lease_up_days:45,vacancy_rate:.08,opex_rate:.35,reserve_years_one_three:250,reserve_years_four_six:500,reserve_years_seven_eight:750,reserve_years_nine_plus:1000,refinance_rate:.07,refinance_years:25,refinance_ltv:.75,exit_cap_rate:.085,carry_per_unit_month:100,reimburse_prior_costs:1,minimum_dscr:1.25};
 const p={unitMix:[{rent:1750},{rent:1750},{rent:1200}],ledger:[{type:'expense',date:'2026-07-01',amount:30000},{type:'expense',date:'2026-09-01',amount:5000}]};
 const budget={acquisition:30000,hardCostsIncludingContingency:255000,softCosts:15000};
 const dates={financing_start:'2026-08-01',completion_target:'2027-03-01'};
 const r=c.diligenceCarry(p,budget,v,dates,18);assert.ok(!r.missing);
 const sum=k=>r.rows.reduce((n,m)=>n+m[k],0);
 assert.ok(Math.abs(sum('cost')+r.prior-300000)<.01);
 assert.ok(Math.abs(sum('draw')+r.openingReimbursement-210000)<.01);
 assert.ok(Math.abs(sum('draw')+r.openingReimbursement+r.reserveUsed-r.balance)<.01);
 assert.ok(Math.abs(r.openingEquity+sum('equity')+sum('draw')+r.openingReimbursement+r.reserveUsed+sum('rent')-300000-sum('opex')-sum('replacement')-sum('interest')-r.cash)<.01);
 assert.ok(r.reserveUsed<=r.reserveLimit);assert.equal(r.end,'2028-02-01');assert.equal(r.fees,null);
 const extra=c.diligenceCarry({...p,ledger:[...p.ledger,{type:'expense',date:'2026-09-02',amount:5000}]},budget,v,dates,18);
 assert.equal(extra.balance,r.balance);assert.equal(extra.rows[1].actual,r.rows[1].actual+5000);
 const delay=c.diligenceCarry(p,budget,v,dates,24);assert.ok(delay.interest>r.interest);
 const noReimburse=c.diligenceCarry(p,budget,{...v,reimburse_prior_costs:0},dates,18);assert.equal(noReimburse.openingReimbursement,0);assert.ok(noReimburse.balance<r.balance);
 const noRent=c.diligenceCarry({...p,unitMix:[{rent:0},{rent:0},{rent:0}]},budget,{...v,reserve_months:1},dates,24);assert.ok(noRent.depleted);assert.ok(noRent.equity>r.equity);assert.ok(noRent.reserveUsed<=noRent.reserveLimit+.001);
 assert.ok(c.diligenceCarry(p,budget,{...v,carry_per_unit_month:null},dates,18).missing.includes('carry_per_unit_month'));
 assert.ok(c.diligenceCarry(p,budget,v,dates,40).missing);
});
