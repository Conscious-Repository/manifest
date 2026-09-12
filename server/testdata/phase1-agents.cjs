const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const context = vm.createContext({fmtAgo: () => 'ago'});
vm.runInContext(fs.readFileSync(path.join(__dirname, '../web/js/41-agents-schedule.js'), 'utf8'), context);
for (const state of ['late', 'failed', 'paused', 'stopped', 'unknown', 'unconfigured']) {
  const got = context.ritualHealth({valid: true, observation: {health: state, why: 'evidence'}}, []);
  assert.equal(got.state, state);
  assert.equal(got.why, 'evidence');
}
assert.equal(context.ritualHealth({valid: true}, [{outcome: 'stopped-charge'}]).state, 'stopped');
assert.equal(context.ritualHealth({valid: true}, [{outcome: 'error'}]).state, 'failed');
assert.equal(context.hermesJobHealth({lastError: 'full error', model: 'pin'}, [])[0].why, 'full error');
console.log('phase1 Agents health rendering fixtures passed');

const source = fs.readFileSync(path.join(__dirname, '../web/js/40-agents.js'), 'utf8');
const fetcher = source.match(/async function fetchSpiritRuns\(\) \{[\s\S]*?\n\}/)[0];
context.fetch = async () => ({json: async () => ({observations: [{health: 'late', evidence: 'registry'}]})});
vm.runInContext(fetcher, context);
context.fetchSpiritRuns().then((d) => {
  assert.equal(d.observations[0].health, 'late');
  assert.equal(d.observations[0].evidence, 'registry');
}).catch((e) => { console.error(e); process.exitCode = 1; });

// Each existing surface consumes the same bounded refusal projection.
for (const file of ['41-agents-schedule.js', '42-agents-runs.js', '60-settings.js']) {
  assert.match(fs.readFileSync(path.join(__dirname, '../web/js', file), 'utf8'), /dutyRefusals/);
}
assert.match(fs.readFileSync(path.join(__dirname, '../web/js/60-settings.js'), 'utf8'), /no duty routed; live usage contract unverified/);
