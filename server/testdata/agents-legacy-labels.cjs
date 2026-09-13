// Agents/Settings legacy-engine labelling (excalibur retirement, 2026-09-13):
// a ritual row and an un-tagged run row chip the primary tree's engine as
// "legacy engine" with the tree's real name in the tooltip; a team tree keeps
// its own name; an Alfred fire keeps the alfred chip; the RUNS week-spend line
// names the legacy engine and the successor, never a bare harness name as a
// runtime; the Settings cards say which runtime is legacy and which succeeds.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');

function el(tag, cls, text) {
  return { tag, cls: cls || '', textContent: text == null ? '' : text, children: [], title: '',
    append(...children) { this.children.push(...children); }, classList: { add() {} } };
}
const js = (f) => fs.readFileSync(path.join(__dirname, '../web/js', f), 'utf8');
const context = vm.createContext({ el, document: { createTextNode: (s) => s, getElementById: () => null }, fmtWhen: () => 'fixture time', els: {} });
vm.runInContext(js('41-agents-schedule.js'), context);
vm.runInContext(js('42-agents-runs.js'), context);
vm.runInContext('ritualRuns = () => []; outcomeStrip = () => el("span", "strip"); spiritStatusCache = null;', context);

// the runs payload names the primary tree; the chip reads it, never a literal
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "excalibur" };', context);
const ritual = context.ritualRow({ spirit: 'warden', ritual: 'audit', valid: true, enabled: true, cadence: '0 8 * * 1', ceilingUsd: 1 });
const chip = ritual.children[0];
assert.match(chip.cls, /harness-chip/);
assert.match(chip.cls, /\blegacy\b/);
assert.match(chip.cls, /ritual-runtime/);
assert.equal(chip.textContent, 'legacy engine');
assert.match(chip.title, /^excalibur harness tree/);
assert.match(chip.title, /successor/);

// a renamed primary relabels its tooltip; no payload at all still says what the runtime is
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "" }; spiritPrimaryHarness = "renamed";', context);
assert.match(context.ritualRow({ spirit: 'a', ritual: 'b', valid: true, enabled: true, ceilingUsd: 1 }).children[0].title, /^renamed harness tree/);
vm.runInContext('spiritPrimaryHarness = "";', context);
assert.equal(context.legacyEngineChip().textContent, 'legacy engine');
assert.doesNotMatch(context.legacyEngineChip().title, /excalibur|renamed/);

// RUNS rows: un-tagged report → legacy engine; team tree → its name; alfred fire → alfred
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "excalibur" };', context);
const engineRow = context.runRuntimeChip({ id: 'r1', spirit: 'warden', ritual: 'audit', outcome: 'completed' });
assert.equal(engineRow.textContent, 'legacy engine');
assert.match(engineRow.cls, /\blegacy\b/);
assert.match(engineRow.title, /excalibur harness tree/);
const teamRow = context.runRuntimeChip({ id: 'r2', harness: 'kairos', spirit: 'kairos', ritual: 'chat', outcome: 'completed' });
assert.equal(teamRow.textContent, 'kairos');
assert.doesNotMatch(teamRow.cls, /legacy/);
const fire = context.runRuntimeChip({ id: 'f1', hermes: { source: 'usage_audit', model: 'm' }, harness: 'alfred', spirit: 'alfred', ritual: 'scout', outcome: 'completed' });
assert.equal(fire.textContent, 'alfred');
assert.match(fire.cls, /alfred/);
assert.match(fire.title, /Hermes cron fire/);
const profileFire = context.runRuntimeChip({ id: 'f2', hermes: { source: 'ledger' }, harness: 'warden', spirit: 'warden', ritual: 'turn' });
assert.equal(profileFire.textContent, 'warden');

// the week-spend head: legacy engine + team trees split, alfred figures, successor named in the tooltip
const now = new Date().toISOString();
vm.runInContext('spiritRuns = { data: [' +
  '{ started: "' + now + '", spentUsd: 1.5 },' +
  '{ started: "' + now + '", spentUsd: 0.25, harness: "kairos" },' +
  '{ started: "2000-01-01T00:00:00Z", spentUsd: 9 }' +
  '], queued: [], primary: "excalibur" }; hermesRuns = [{ started: "' + now + '", usd: 0.5, tokens: 1200 }];', context);
const line = context.weekSpendLine();
assert.equal(line.text, '$1.50 legacy engine · $0.25 team trees · $0.50 alfred · 1k tokens alfred · last 7 days');
assert.match(line.title, /legacy engine: run reports in the excalibur harness tree/);
assert.match(line.title, /alfred: Hermes fires, the successor runtime/);
assert.doesNotMatch(line.text, /excalibur/);
assert.equal(context.spiritWeekSpend(), 1.75); // the crumb's total is unchanged
vm.runInContext('spiritRuns = { data: [{ started: "' + now + '", spentUsd: 2 }], queued: [], primary: "excalibur" }; hermesRuns = [];', context);
assert.equal(context.weekSpendLine().text, '$2.00 legacy engine · $0.00 alfred · 0 tokens alfred · last 7 days');

// Settings › Agents: the two cards say which runtime is legacy and which succeeds;
// Hosts & paths marks the excalibur roots as legacy/engine evidence, still read-only.
const settings = js('60-settings.js');
assert.match(settings, /"harness-chip legacy", "legacy · retiring"/);
assert.match(settings, /"harness-chip alfred", "successor runtime"/);
assert.match(settings, /excalibur \(legacy engine · retiring\)/);
assert.match(settings, /artifacts\/runs\/ \(run history, read-only\)/);
assert.match(settings, /HOSTS & PATHS — config\.json as loaded, read-only/);
// the schedule row never hard-codes the harness name as its runtime again
assert.doesNotMatch(js('41-agents-schedule.js'), /"harness-chip ritual-runtime", "excalibur"/);
assert.doesNotMatch(js('42-agents-runs.js'), /\|\| "excalibur"/);
console.log('agents legacy-engine labels passed');
