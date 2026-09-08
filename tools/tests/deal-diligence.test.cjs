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
 options.bundle.assumptions.exit_cap_rate=.0725;
 await c.drawDealUnderwriting(host,'test',options);
 const financed=host.text();assert.match(financed,/Illustrative financing/);assert.match(financed,/70% of development subtotal/);assert.match(financed,/74.38%/);assert.match(financed,/NCF coverage²/);assert.match(financed,/\$214,943.75/);assert.ok(!financed.includes('SECRET LENDER'));assert.ok(!financed.includes('PRIVATE EMAIL'));
});
