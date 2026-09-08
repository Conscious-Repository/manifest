const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function setup(){
 const c=vm.createContext({window:{}});
 vm.runInContext(fs.readFileSync('server/web/portal/src/util.js','utf8'),c);
 const src=fs.readFileSync('server/web/portal/src/agency.jsx','utf8');
 vm.runInContext(src.slice(0,src.indexOf('function AgencyField(')),c);
 const read=n=>JSON.parse(fs.readFileSync('server/web/portal/data/'+n+'.json','utf8'));
 const data={people:read('people'),backlog:read('backlog')}; const goals=read('goals');
 return {c,data,goals,build:()=>c.buildAgencyModel(data,c.window.PORTAL_UTIL.buildGoalIndex(goals))};
}
test('every active annual goal and roster person is represented',()=>{
 const x=setup(); x.data.people.people.push({initials:'NEW',name:'New teammate'});
 const m=x.build();
 for(const g of x.goals.goals.filter(g=>g.horizon==='1yr'&&g.status!=='done')) assert.ok(m.goals.some(n=>n.id===g.id),g.id);
 for(const p of x.data.people.people) assert.ok(m.people.some(n=>n.id===p.initials),p.initials);
 assert.ok(m.goals.some(g=>g.id==='aion/write-candidate'));
});
test('completed featured goals leave the future; removed people leave the field',()=>{
 const x=setup(); x.goals.goals.find(g=>g.id==='aion/series-a-15m').status='done';
 x.data.people.people=x.data.people.people.filter(p=>p.initials!=='BA');
 const m=x.build(); assert.ok(!m.goals.some(g=>g.id==='aion/series-a-15m')); assert.ok(!m.people.some(p=>p.id==='BA'));
});
test('growing history stays within the past cone and uses completion quarters',()=>{
 const x=setup();
 for(let i=0;i<50;i++) x.goals.goals.push({id:'past/'+i,title:'Past '+i,horizon:'rock',status:'done',closed:'2026-08-01',quarter:'2025-Q1'});
 const m=x.build();
 for(const r of m.rocks){assert.ok(r.y>=468&&r.y<=672);assert.ok(Math.abs(r.x-520)<=382*(1-(r.y-374)/330));}
 assert.ok(m.rocks.filter(r=>r.id.startsWith('past/')).every(r=>r.quarter==='2026-Q3'));
});
