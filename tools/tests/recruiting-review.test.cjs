const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
function context() {
  const c = vm.createContext({});
  vm.runInContext(fs.readFileSync(path.join(__dirname,'../../server/web/js/96-aion-recruiting.js'),'utf8'),c);
  return c;
}
test('bibliographic rendering extracts only labeled source facts, preserving punctuation', () => {
  const c = context();
  const result = c.recEvidenceExcerpt({snippet:'author: Test AB · title: A study: MRI. Two methods. · journal: Journal · pubdate: 2025 · pmid: 123 · doi: x'});
  assert.equal(result.text,'A study: MRI. Two methods.');
  assert.equal(result.detail,'Journal · 2025');
  assert.equal(result.label,'Listed publication');
  assert.equal(c.recEvidenceExcerpt({snippet:'Worked on a device; current job unknown.'}).text,'Worked on a device; current job unknown.');
  assert.equal(c.recEvidenceExcerpt({snippet:'title: Software Engineer'}).label,'Source excerpt');
});
test('review queue intersects role, query and decision state without mutating records', () => {
  const c = context();
  c.runs = [{id:'one',source:'pubmed',scope:{role:'role/mri',query:'MRI'},drafts:[
    {id:'a',status:'new',draft:{name:'Alex',evidence:[{snippet:'Built coils'}]}},
    {id:'b',status:'new',draft:{name:'Sam'}},
    {id:'c',status:'accepted',draft:{name:'Pat'}}
  ]},{id:'two',source:'web',scope:{role:'role/me'},drafts:[{id:'d',status:'new',draft:{name:'Alex'}}]}];
  const before = JSON.stringify(c.runs);
  vm.runInContext('recRuns=runs; recDraftLater["one#b"]=true;',c);
  assert.equal(c.recSourceEntries().length,2);
  vm.runInContext('recSourceRole="role/mri"; recSourceQuery="COILS";',c);
  assert.equal(c.recSourceEntries()[0].draft.id,'a');
  vm.runInContext('recSourceQuery=""; recSourceStatus="later";',c);
  assert.equal(c.recSourceEntries()[0].draft.id,'b');
  vm.runInContext('recSourceStatus="decided";',c);
  assert.equal(c.recSourceEntries()[0].draft.id,'c');
  vm.runInContext('recSourceStatus="all";',c);
  assert.equal(c.recSourceEntries().length,3);
  assert.equal(JSON.stringify(c.runs),before);
});
test('everyone is not silently restricted by hidden inbound, role or archived filters', () => {
  const c = context();
  vm.runInContext('recCache={roles:[]}; recRole="role/a"; recOrigin="inbound"; recCut="archived"; recPeopleFacet="everyone";',c);
  assert.equal(c.recVisible({name:'Alex',role:'role/b',stage:'new'}),true);
  assert.equal(c.recVisible({name:'Alex',role:'role/b',stage:'archived'}),false);
});
test('resume outline preserves body lines and recognizes only standalone section names', () => {
  const c = context();
  const sections = c.recResumeSections('Alex\nExperience\nEngineer at Example\nExperience with Python\nEducation:\nBSc, 2020');
  assert.equal(sections.length,3);
  assert.equal(sections[1].title,'Experience');
  assert.equal(sections[1].lines.join('|'),'Engineer at Example|Experience with Python');
  assert.equal(sections[2].lines[0],'BSc, 2020');
});
test('next candidate order matches visible stage groups and inbound application order', () => {
  const c=context();
  c.fixture={stages:['new','reviewing','ashby'],roles:[],candidates:[
    {id:'b',name:'B',stage:'reviewing'}, {id:'a',name:'A',stage:'new'},
    {id:'d',name:'D',stage:'ashby',inbound:'2026-09-04'}, {id:'c',name:'C',stage:'ashby',inbound:'2026-09-03'}]};
  vm.runInContext('recCache=fixture;recOrigin="both";',c);
  assert.equal(c.recReviewCandidates().map(x=>x.id).join(','),'a,b,d,c');
  vm.runInContext('recOrigin="inbound";',c);
  assert.equal(c.recReviewCandidates().map(x=>x.id).join(','),'c,d');
});
