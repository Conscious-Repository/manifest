// fr-shell-data.cjs — Contractors, contracts, properties and Fundraising rows
// for fr-shell-phone.cjs (served through writing-stub-api.cjs's json option).
const props=[['743-n-euclid','743 N Euclid'],['4848-fountain','4848 Fountain Ave'],['760-bayard','760 Bayard Ave'],['4924-fountain','4924 Fountain Ave'],['753-bayard','753 Bayard Ave'],['751-bayard','751 Bayard Ave'],['4852-fountain','4852 Fountain Ave']];
const ctr=[['accredited','Accredited Roofing & Exteriors',['roofing']],['ars','ARS Construction Group',[]],['bastin','Bastin Roofing',['roofing']],['hopke','Hopke Craftsmen',[]],['mw','M&W Services',['demo']],['meyers','Meyers Construction',['roofing']],['minplus','Min+ Architecture',['architecture']],['olga','olga sobkiv',['architecture']],['treecourt','Tree Court Builders',[]]];
const C=(contractor,status,total,drawn,ps)=>({contractor,status,total,drawn,allocations:ps.map(p=>({property:p})),date:'2026-09-01'});
module.exports={
 '/api/realestate/entities':{contractors:ctr.map(([slug,name,scopes])=>({slug,name,scopes,website:slug==='bastin'?'https://bastin.example':''}))},
 '/api/realestate/contracts':{contracts:[C('accredited','proposed',28000,0,['743-n-euclid']),C('ars','proposed',85000,0,['743-n-euclid']),C('bastin','accepted',13906,0,['4848-fountain','760-bayard']),C('bastin','proposed',13000,0,[]),C('hopke','proposed',160000,0,['4924-fountain']),C('mw','accepted',40451,28000,['753-bayard','751-bayard','4848-fountain']),C('meyers','accepted',45000,0,['753-bayard','751-bayard']),C('olga','accepted',16500,0,['751-bayard','753-bayard','760-bayard']),C('treecourt','accepted',20264,21000,['4848-fountain','4852-fountain'])]},
 '/api/properties':{properties:props.map(([slug,address])=>({slug,address,name:address,status:'owned'})),deals:[],templates:[],holdings:{}},
 '/api/aion/fundraising':{opportunities:[{id:'a',firm:'Andreessen Horowitz Bio + Health',people:[{key:'daisy wolf',display:'daisy wolf'}],lastTouchpoint:'intro call',lastTouch:{date:'2026-09-18',kind:'met',person:'daisy wolf'},nextStep:'send the data room link',nextTouch:{date:'2026-10-09',kind:'upcoming'},status:'active'},{id:'b',firm:'Lux Capital',people:[{key:'a',display:'deena shakir'},{key:'b',display:'josh wolfe'}],nextStep:'warm intro via board',status:'prospect'}],schema:4},
 '/api/aion/fundraising/sync':{enabled:false},
};
