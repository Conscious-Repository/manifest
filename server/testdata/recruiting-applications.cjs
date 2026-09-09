const vm=require('node:vm'), fs=require('node:fs'), assert=require('node:assert/strict');
vm.runInThisContext(fs.readFileSync(require('node:path').join(__dirname,'../web/js/96-aion-recruiting.js'),'utf8'));
vm.runInThisContext(`
recCache={roles:[{id:'role/mech',slug:'mech'},{id:'role/mri',slug:'mri'}],stages:['new','ashby','archived'],candidates:[
{id:'cand/a',stage:'reviewing',inbound:'2026-09-01',ashbyApplicationId:'one',role:'role/mech',applications:[{id:'one',role:'role/mech',status:'Hired',stage:'Hired'},{id:'two',role:'role/mri',status:'Active',stage:'Initial Screen'}]},
{id:'cand/b',stage:'ashby',inbound:'2026-09-01',ashbyApplicationId:'three',role:'role/mech',applications:[{id:'three',role:'role/mech',status:'Hired',stage:'Hired'}]}]};
recOrigin='both';
`);
assert.equal(recCandidateCount('role/mech'),0);
assert.equal(recCandidateCount('role/mri'),1);
assert.equal(recCandidateCount(),1);
assert.equal(recCandidateCount('role/mech','both','all'),2);
vm.runInThisContext(`recRole='mech'`);
assert.equal(recReviewCandidates().length,0);
vm.runInThisContext(`recCut='archived'`);
assert.equal(recReviewCandidates().length,2);
vm.runInThisContext(`recRole='mri';recCut='open'`);
const rows=recReviewCandidates();assert.equal(rows.length,1);assert.equal(rows[0].ashbyApplicationId,'two');
assert.equal(rows[0].stage,'ashby');assert.equal(recUntriaged(rows[0]),false);
assert.equal(recGateTable(rows[0]).chipLabel,'Initial Screen');
console.log('Application role filters, history, counts and stage labels passed');
assert.equal(recPipelineStage(rows[0]),'Initial Screen');
vm.runInThisContext(`recCache.candidates[0].applications[1].stage='Second Round'`);
assert.equal(recPipelineStage(recReviewCandidates()[0]),'Second Round');
assert.equal(recUntriaged(recReviewCandidates()[0]),false);
