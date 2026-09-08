const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function setup(){
 const c=vm.createContext({Date:class extends Date { constructor(){super("2026-09-07T12:00:00Z");} },projMoney:p=>p.money||{},openTodoCount:p=>p.open||0});
 vm.runInContext(fs.readFileSync('server/web/js/81-properties-board.js','utf8'),c);
 return c;
}
test('attention checks parallel rocks, including overdue work after the first rock',()=>{
 const c=setup(); const p={status:'construction',open:2,work:[{text:'First'},{text:'Roof',doneBy:'2026-08-01'},{text:'Done',checked:true,doneBy:'2026-01-01'}]};
 assert.equal(c.pfFacts(p).overdue.length,1);
 assert.equal(c.pfFacts(p).unfinished.length,2);
 assert.equal(c.pfAttention(p),true);
});
test('phase and attention intersect, and search finds every rock',()=>{
 const c=setup(); const p={status:'construction',open:2,work:[{text:'First'},{text:'Roof',doneBy:'2026-08-01'}]};
 vm.runInContext('pfCut="attention";pfPhase="construction";pfQuery="roof";',c);
 assert.equal(c.pfVisible(p),true);
 vm.runInContext('pfPhase="pipeline";',c); assert.equal(c.pfVisible(p),false);
 vm.runInContext('pfPhase="construction";pfQuery="absent";',c); assert.equal(c.pfVisible(p),false);
});
