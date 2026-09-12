const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
function el(tag, cls, text) {
  return {tag, cls, textContent: text, children: [], append(...children) { this.children.push(...children); }, classList: {add() {}}};
}
const context = vm.createContext({el, document: {createTextNode: s => s}, fmtWhen: () => 'fixture time'});
vm.runInContext(fs.readFileSync(path.join(__dirname, '../web/js/41-agents-schedule.js'), 'utf8'), context);
vm.runInContext('ritualRuns = () => []; outcomeStrip = () => el("span", "strip");', context);
for (const [spirit, ritual] of [['concierge','briefing'], ['ea-coordinator','waiting-on'], ['sage','skill-cast']]) {
  const row = {spirit, ritual, valid: true, enabled: false, retired: true, retirementReason: 'retired/paused; see #/agents/ritual/'+spirit+'/'+ritual, pausedReason: 'retired/paused', ceilingUsd: 1};
  assert.equal(context.schedGroupOf(row), 'paused');
  const rendered = context.ritualRow(row);
  const actions = rendered.children.find(c => c.cls === 'ritual-acts');
  assert.equal(actions.children.length, 1);
  assert.equal(actions.children[0].textContent, 'retired');
  assert.equal(actions.children[0].disabled, true);
  assert.match(actions.children[0].title, /#\/agents\/ritual\//);
}
const connector = context.ritualRow({spirit:'ea-coordinator', ritual:'email-sync', valid:true, enabled:true, ceilingUsd:1});
const run = connector.children.find(c => c.cls === 'ritual-acts').children[0];
assert.equal(run.textContent, 'run now');
assert.equal(run.disabled, false);
console.log('Phase 2 retirement controls passed');
// A stale picker must display retirement, not the existing already-running toast.
const spoolSource = fs.readFileSync(path.join(__dirname, '../web/js/40-agents.js'), 'utf8').match(/async function spiritSpool\([\s\S]*?\n\}/)[0];
let toast;
context.location = {hash: '#/feed'};
context.fetch = async () => ({status:409, json:async () => ({retired:true, error:'retired/paused; see Agents'})});
context.showToast = (text, action) => { toast = {text, action}; };
vm.runInContext(spoolSource, context);
context.spiritSpool('sage','skill-cast','').then(() => {
  assert.match(toast.text, /retired\/paused/);
  toast.action();
  assert.equal(context.location.hash, '#/agents/ritual/sage/skill-cast');
}).catch(e => { console.error(e); process.exitCode=1; });
