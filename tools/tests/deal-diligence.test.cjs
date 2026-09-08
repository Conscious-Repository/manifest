const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const vm=require('node:vm');
const c=vm.createContext({});vm.runInContext(fs.readFileSync('server/web/js/86-deal-diligence.js','utf8'),c);
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
