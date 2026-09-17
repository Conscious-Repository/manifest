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
    append(...children) { for(const c of children){if(c&&typeof c==='object'){if(c.parent)c.parent.children=c.parent.children.filter(x=>x!==c);c.parent=this;}this.children.push(c);} },
    prepend(...children) {this.append(...children);this.children=[...children,...this.children.filter(x=>!children.includes(x))];},
    querySelector(selector){return this.querySelectorAll(selector)[0]||null;},
    querySelectorAll(selector){const found=[];for(const c of this.children){if(!c||typeof c!=='object')continue;if((c.cls||'').split(' ').includes(selector.slice(1)))found.push(c);found.push(...c.querySelectorAll(selector));}return found;},
    addEventListener(){}, classList: { add() {} } };
}
const js = (f) => fs.readFileSync(path.join(__dirname, '../web/js', f), 'utf8');
const context = vm.createContext({ el, document: { createTextNode: (s) => s, getElementById: () => null }, fmtWhen: () => 'fixture time', els: {} });
vm.runInContext(js('41-agents-schedule.js'), context);
vm.runInContext(js('42-agents-runs.js'), context);
vm.runInContext('ritualRuns = () => []; outcomeStrip = () => el("span", "outcome-strip"); spiritStatusCache = null;', context);

// the runs payload names the primary tree; the chip reads it, never a literal
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "excalibur" };', context);
const ritual = context.ritualRow({ spirit: 'warden', ritual: 'audit', valid: true, enabled: true, cadence: '0 8 * * 1', ceilingUsd: 1 });
const chip = ritual.querySelector('.ritual-runtime');
assert.match(chip.cls, /harness-chip/);
assert.match(chip.cls, /\blegacy\b/);
assert.match(chip.cls, /ritual-runtime/);
assert.equal(chip.textContent, 'legacy engine');
assert.match(chip.title, /^excalibur harness tree/);
assert.match(chip.title, /successor/);

// a renamed primary relabels its tooltip; no payload at all still says what the runtime is
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "" }; spiritPrimaryHarness = "renamed";', context);
assert.match(context.ritualRow({ spirit: 'a', ritual: 'b', valid: true, enabled: true, ceilingUsd: 1 }).querySelector(".ritual-runtime").title, /^renamed harness tree/);
vm.runInContext('spiritPrimaryHarness = "";', context);
assert.equal(context.legacyEngineChip().textContent, 'legacy engine');
assert.doesNotMatch(context.legacyEngineChip().title, /excalibur|renamed/);

// RUNS rows: un-tagged report → legacy engine; team tree → its name; alfred fire → alfred
vm.runInContext('spiritRuns = { data: [], queued: [], primary: "excalibur" };', context);
const engineRow = context.runRuntimeChip({ id: 'r1', spirit: 'warden', ritual: 'audit', outcome: 'completed' });
assert.equal(engineRow.textContent, 'legacy engine');
assert.equal(context.runRuntimeChip({executor:'manifest'}).textContent, 'manifest');
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
assert.match(settings, /"harness-chip legacy", "historical runtime"/);
assert.match(settings, /"harness-chip alfred", "successor runtime"/);
assert.match(settings, /excalibur \(historical harness\)/);
assert.match(settings, /artifacts\/runs\/ \(run history, read-only\)/);
assert.match(settings, /HOSTS & PATHS — config\.json as loaded, read-only/);
// the schedule row never hard-codes the harness name as its runtime again
assert.doesNotMatch(js('41-agents-schedule.js'), /"harness-chip ritual-runtime", "excalibur"/);
assert.doesNotMatch(js('42-agents-runs.js'), /\|\| "excalibur"/);
// the ritual picker's empty-state toast spans every runtime, not the excalibur tree
const agents = js('40-agents.js');
assert.match(agents, /showToast\("No agent\/ritual found in the configured runtimes\.", null, "error"\)/);
assert.doesNotMatch(agents, /found in the excalibur tree/);
console.log('agents legacy-engine labels passed');

// Schedule ownership comes from the server; a configured flag is not a handoff.
const ownershipRow = (extra) => context.ritualRow({spirit:'ea-coordinator', ritual:'pocket-sync', valid:true, enabled:true, ceilingUsd:1, harness:'excalibur', path:'spirits/ea-coordinator/rituals/pocket-sync.md', ...extra});
const legacy = ownershipRow({migrationState:'legacy-retiring', migrationDetail:'State reconciliation required', configuredOwner:'excalibur', legacyActionable:true});
assert.equal(legacy.querySelector('.ritual-runtime').textContent, 'legacy · retiring');
assert.match(legacy.querySelector('.ritual-runtime').title, /excalibur harness tree.*spirits\/ea-coordinator\/rituals\/pocket-sync.md/);
assert.equal(legacy.querySelector('.ritual-acts').children[0].disabled, false);
const conflict = ownershipRow({migrationState:'ownership-conflict', migrationDetail:'Legacy file remains enabled', configuredOwner:'manifest', legacyActionable:false, legacyEnabled:true, cadence:'0 9 * * *'});
assert.match(conflict.querySelector('.ritual-runtime').textContent, /manifest · ownership-conflict/);
assert.equal(conflict.querySelector('.ritual-acts').children[0].disabled, true);
assert.equal(conflict.querySelector('.ritual-acts').children[1].textContent, 'pause');
const pending = ownershipRow({enabled:false, migrationState:'handoff-unverified', migrationDetail:'Handoff evidence required', configuredOwner:'manifest', legacyActionable:false, legacyEnabled:false});
assert.equal(pending.querySelector('.ritual-acts').children.length, 1); // no resume
const retired = ownershipRow({enabled:false, retired:true, migrationState:'retired', legacyActionable:false});
assert.equal(retired.querySelector('.ritual-runtime').textContent, 'retired · history');
assert.equal(retired.querySelector('.ritual-acts').children.length, 1);
assert.equal(retired.querySelector('.ritual-acts').children[0].disabled, true);
assert.ok(context.scheduleRuntimeOrder({hermes:{}}, {}) < 0);
assert.equal(context.scheduleRuntimeOrder({}, {}), 0);

const rowText = node => typeof node === 'string' ? node : node.textContent + node.children.map(rowText).join('');

// Successor ownership takes precedence over retired engine labels and observations.
for (const ritual of ['aion', 'real-estate', 'ooda-email', 'future-duty']) {
  const data = {spirit:'extractor', ritual, valid:true, enabled:true, ceilingUsd:1,
    configuredOwner:'manifest', successorEnabled:true, engineRetired:true, retired:false,
    migrationState:'successor-enabled', legacyActionable:false, legacyEnabled:false,
    cadence:'', observation:{health:'paused', why:'paused in engine registry'}};
  const successor = ownershipRow(data);
  assert.equal(context.schedGroupOf(data), 'internal');
  assert.equal(context.schedGroupOf({...data, cadence:'0 9 * * *'}), 'yours');
  assert.equal(successor.querySelector('.ritual-runtime').textContent, 'Hermes · Manifest');
  assert.ok(!successor.cls.includes('paused'));
  assert.doesNotMatch(rowText(successor), /retired|paused/);
  assert.match(rowText(successor), /no cadence configured/);
  assert.equal(context.ritualHealth(data, [{outcome:'error'}]).state, 'unknown');
  assert.equal(successor.querySelector('.ritual-acts').children.length, 0);
  for (const extra of [{successorEnabled:false}, {configuredOwner:'blocked'}]) {
    assert.doesNotMatch(ownershipRow({...data, ...extra}).querySelector('.ritual-runtime').textContent, /Hermes/);
  }
}
for (const [spirit, ritual] of [['concierge','briefing'], ['ea-coordinator','waiting-on'], ['sage','skill-cast'], ['warden','audit'], ['extractor','re-intake']]) {
  const data = {spirit, ritual, enabled:false, retired:true, engineRetired:true, migrationState:'retired', legacyActionable:false};
  assert.equal(context.schedGroupOf(data), 'paused');
  assert.equal(ownershipRow(data).querySelector('.ritual-runtime').textContent, 'retired · engine unavailable');
  assert.equal(context.ritualHealth(data, []).state, 'paused');
}
console.log('agents Manifest successor projection passed');

vm.runInContext('spiritModels = {extractor: "old-provider"}', context);
const pinned = ownershipRow({spirit:'extractor', successorEnabled:true, configuredOwner:'manifest', model:'deepseek-v4.1-flash', provider:'lab-sparks', legacyEnabled:true, legacyActionable:false, enabled:false, cadence:'0 9 * * *'});
assert.equal(pinned.querySelector('.ceil-model').textContent, 'deepseek-v4.1-flash');
assert.equal(pinned.querySelector('.ritual-acts').children.length, 0);

// Connector successors remain current even when predecessor files are disabled
// and retired. Recorded success must not hide an error on a later attempt.
for (const ritual of ['granola-sync', 'pocket-sync', 'email-sync']) {
  const data = {spirit:'ea-coordinator', ritual, enabled:false, retired:true, valid:true,
    configuredOwner:'manifest', successorEnabled:true, legacyActionable:false,
    legacyEnabled:false, cadence:'0 7 * * *', nextFire:'2026-09-18T07:00:00Z', model:'old-provider', successorHealth:'error', successorLastSuccess:'2026-09-16T10:00:00Z',
    migrationDetail:'Legacy dispatch fenced; recorded attempts do not establish semantic parity.',
    observation:{health:'paused'}, lastOutcome:'completed'};
  assert.equal(context.schedGroupOf(data), 'yours');
  assert.equal(context.schedGroupOf({...data, cadence:''}), 'internal');
  assert.equal(context.schedGroupOf({...data, successorEnabled:false}), 'paused');
  const row = context.ritualRow(data);
  assert.ok(!row.cls.includes('paused'));
  assert.equal(row.querySelector('.ritual-runtime').textContent, 'Manifest · sync');
  assert.equal(context.ritualHealth(data, []).state, 'error');
  assert.equal(context.ritualHealth({...data, successorHealth:'last-success'}, []).state, 'last-success');
  assert.equal(context.ritualHealth({...data, successorHealth:undefined}, []).state, 'unknown');
  assert.match(rowText(row.querySelector('.ritual-outcome')), /last success fixture time/);
  assert.equal(row.querySelector('.ritual-outcome').title, data.successorLastSuccess);
  assert.equal(row.querySelector('.ritual-acts').children.length, 0);
  assert.doesNotMatch(rowText(row), /legacy blocked|successor: unknown|old-provider|0 7 \* \* \*/);
  assert.match(rowText(row), /managed sync/);
  assert.equal(row.querySelector('.ritual-next').textContent, '—');
  const successful = context.ritualRow({...data, successorHealth:'last-success'});
  assert.match(rowText(successful), /last success fixture time/);
  assert.doesNotMatch(rowText(successful), /health unknown|successor: unknown/);
  const unknown = context.ritualRow({...data, successorHealth:undefined, successorLastSuccess:undefined});
  assert.match(rowText(unknown), /health unknown/);
  assert.ok(row.querySelector('.sched-details').querySelector('.successor-history'), 'history is behind Details');
}
assert.equal(vm.runInContext('schedOpen.internal', context), true);
const activeJob = context.hermesRowOf({id:'current-job', name:'Current cron', enabled:true});
assert.equal(context.schedGroupOf(activeJob), 'yours');
assert.equal(context.schedGroupOf(context.hermesRowOf({id:'paused-job', enabled:false})), 'paused');

// Exercise the actual directory renderer with historical API rows and current
// profiles, including a profile whose name also occurs in legacy history.
vm.runInContext(js('43-agents-page.js'), context);
const index = el('div');
index.classList.remove = () => {};
context.document.getElementById = id => id === 'spiritIndex' ? index : null;
context.collapsibleSection = (host, title, count) => {
  host.append(el('button', 'toggle'));
  const directory = el('div', 'directory');
  directory.count = count;
  host.append(directory);
  return directory;
};
vm.runInContext(`
  loadProfileIndex = () => {};
  spiritRitualRows = ['concierge','ea-coordinator','extractor','sage','warden'].map(spirit => ({spirit}));
  spiritStatusCache = {spirits:{concierge:{}, 'ea-coordinator':{}, extractor:{}, sage:{}, warden:{}}};
  profileIndex = [{name:'default',active:true},{name:'kairos'},{name:'zeck'},{name:'warden'}];
  hermesInfo = {cron:{list:[{id:'current-job',enabled:true}]}};
  renderSpiritIndex();
`, context);
const directory = index.querySelector('.directory');
assert.equal(directory.count, '4');
assert.deepEqual(directory.querySelectorAll('.spirit-index-name').map(n => n.textContent), ['alfred · default','kairos','zeck','warden']);
assert.equal(directory.querySelector('.spirit-index-count').textContent, '1');
assert.equal(vm.runInContext('spiritRitualRows.length', context), 5);
console.log('agents current directory and connector successors passed');

// New agents default to the supported runtime; historical spirits are not offered.
assert.match(js('44-agents-new.js'), /runtime: "profile"/);
assert.doesNotMatch(js('44-agents-new.js'), /opts.append\(option\("spirit"/);

// Current schedule and header project history without changing the source rows.
const board = el('div');
const next = el('div');
const currentContext = vm.createContext({
  el: (tag, cls, text) => Object.assign(el(tag, cls, text), {setAttribute() {}}),
  document: {getElementById: id => id === 'spiritNextUp' ? next : null},
  els: {spiritRitualBoard: board, spiritsView: {hidden:false}},
  emptyRow: text => el('div', '', text),
  statusDot: () => el('span'),
  reIntakeStatusRow: () => el('div'),
  setCrumbMeta: text => { currentContext.crumb = text; },
});
vm.runInContext(js('40-agents.js'), currentContext);
vm.runInContext(js('41-agents-schedule.js'), currentContext);
vm.runInContext(`
  const historical = ['briefing','waiting-on','skill-cast','audit','re-intake'].map(ritual => ({
    spirit:'predecessor', ritual, retired:true, engineRetired:true, enabled:false,
    retirementReason:'Excalibur engine retired/unavailable', valid:true,
    nextFire:'2099-01-01T00:00:00Z'
  }));
  const migrated = ['email-sync','granola-sync','pocket-sync','aion','ooda-email','real-estate'].map(ritual => ({
    spirit:ritual.endsWith('-sync') ? 'ea-coordinator' : 'extractor', ritual, configuredOwner:'manifest', successorEnabled:true,
    retired:true, engineRetired:true, enabled:false, valid:true
  }));
  hermesInfo = {runner:{enabled:true}, cron:{list:[]}};
  spiritStatusCache = {enabled:true, engineRetired:true, harnesses:[
    {name:'excalibur',engineRetired:true,engineAlive:false},
    {name:'kairos',engineAlive:true}, {name:'zeck',engineAlive:true},
    {name:'hermes',engineAlive:false}
  ]};
  renderSpiritRituals(historical.concat(migrated));
`, currentContext);
assert.equal(board.querySelectorAll('.ritual-row').length, 6);
assert.equal(board.querySelectorAll('.ritual-runtime').filter(n => n.textContent === 'Manifest · sync').length, 3);
assert.equal(board.querySelectorAll('.ritual-runtime').filter(n => n.textContent === 'Hermes · Manifest').length, 3);
assert.doesNotMatch(rowText(board), /PAUSED|legacy blocked|Excalibur engine retired|engine unavailable/);
assert.equal(next.children.length, 0);
assert.match(currentContext.crumb, /Manifest connected.*kairos ok.*zeck ok.*Hermes runner enabled.*6 rituals/);
assert.doesNotMatch(currentContext.crumb, /excalibur|hermes down/i);
assert.equal(vm.runInContext('spiritRitualRows.length', currentContext), 11);
assert.equal(vm.runInContext('historical.every(r => r.retired && !r.enabled)', currentContext), true);
assert.equal(currentContext.currentScheduleRow({engineRetired:true, enabled:true}), false);
assert.equal(currentContext.currentScheduleRow({enabled:false, harness:'kairos'}), true);
board.children = [];
vm.runInContext(`
  hermesInfo = {runner:{enabled:false},cron:{list:[{id:'paused-current',enabled:false}]}};
  renderSpiritRituals(historical);
`, currentContext);
assert.match(rowText(board), /PAUSED/);
assert.equal(board.querySelectorAll('.ritual-row').length, 1);
assert.match(currentContext.crumb, /Hermes runner disabled/);
vm.runInContext('hermesInfo = null; updateSpiritsCrumb();', currentContext);
assert.match(currentContext.crumb, /Hermes status unknown/);
console.log('agents current schedule and header passed');
