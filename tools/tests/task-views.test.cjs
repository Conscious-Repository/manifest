const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const src = fs.readFileSync(path.join(__dirname, '../../server/web/js/90-todos.js'), 'utf8');
function context() {
  const c = vm.createContext({localStorage: {getItem: () => null}});
  vm.runInContext(src, c);
  vm.runInContext('todosLens="all";',c);
  return c;
}
const row = {id: 'a', text: 'Draft investor update', source: 'aion', container: {name: 'Aion'}, state: 'open'};
test('next actions exclude waiting, blocked, assigned and executing work', () => {
  const c = context();
  assert.equal(c.todoWorkState(row).next, true);
  for (const extra of [{waiting:'someone'}, {state:'blocked'}, {blockedBy:['other']}, {owner:'agent:codex'}, {delegation:{state:'running'}}]) {
    assert.equal(c.todoWorkState({...row,...extra}).next, false);
  }
});
test('attention includes plans, results, failures and dependencies but not active execution', () => {
  const c = context();
  for (const state of ['plan-ready','done','proposed','failed','plan-failed','cancelled']) assert.equal(c.todoWorkState({...row,delegation:{state}}).attention, true);
  assert.equal(c.todoWorkState({...row,delegation:{state:'running'}}).attention, false);
  assert.equal(c.todoWorkState({...row,owner:'agent:codex'}).label, 'Assigned · no active run');
  assert.equal(c.todoWorkState({...row,delegation:{state:'plan-ready'}}).column, 'review');
});
test('domain and query intersect with each work view without losing rank order', () => {
  const c = context();c.rows = [row, {...row,id:'b',container:{name:'manifest'},source:'personal'}, {...row,id:'c',owner:'agent:codex'}];
  assert.equal(vm.runInContext('rows.filter((r) => todoMatches(r)).length',c),3);
  vm.runInContext('todosTab="aion"; todosLens="next"; todosQuery="INVESTOR";',c);
  assert.equal(vm.runInContext('rows.filter((r) => todoMatches(r)).map(r=>r.id).join(",")',c),'a');
  vm.runInContext('todosLens="agents";',c);
  assert.equal(vm.runInContext('rows.filter((r) => todoMatches(r)).map(r=>r.id).join(",")',c),'c');
  assert.equal(c.issueTabOf('manifest'),'manifest');
  assert.equal(c.tabOf({...row,source:'realestate',container:{name:'Property'}}),'realestate');
});
