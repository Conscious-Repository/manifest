// Agents / Settings information architecture (2026-09-13): the re-intake lane
// is a one-line status row on the SCHEDULE / RUNS boards (state chip · bits ·
// details →), never the full policy sentence as a bare paragraph; the legacy
// Excalibur card on Settings › Agents is compact by default (title · chip ·
// liveness · path · one-line purpose · tally) with its ritual inventory,
// evidence path, agents and conduits behind a native details fold — every
// evidence string still present, just at level two. Alfred leads the board.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');

// a tiny DOM: enough of an element for el() / append / textContent / attributes
class Node {
  constructor(tag, cls, text) {
    this.tag = tag; this.className = cls || ''; this.children = []; this.attrs = {};
    this._text = text == null ? '' : String(text); this.title = ''; this.href = ''; this.open = false; this.hidden = false;
    this.classList = { add: (c) => { this.className = (this.className + ' ' + c).trim(); }, contains: (c) => this.className.split(/\s+/).includes(c) };
  }
  append(...kids) { for(let k of kids){if(typeof k==='string')k=new Node('#text','',k);if(k.parent)k.parent.children=k.parent.children.filter(x=>x!==k);k.parent=this;this.children.push(k);} }
  prepend(...kids) {this.append(...kids);this.children=[...kids,...this.children.filter(x=>!kids.includes(x))];}
  querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
  querySelectorAll(selector){return this.children.flatMap(c=>c.find(x=>x.has(selector.slice(1))));}
  addEventListener() {}
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k]; }
  get textContent() { return this._text + this.children.map((c) => c.textContent).join(''); }
  find(pred, out = []) { if (pred(this)) out.push(this); this.children.forEach((c) => c.find(pred, out)); return out; }
  has(cls) { return this.classList.contains(cls); }
}
const el = (tag, cls, text) => new Node(tag, cls, text);
const js = (f) => fs.readFileSync(path.join(__dirname, '../web/js', f), 'utf8');
const css = (f) => fs.readFileSync(path.join(__dirname, '../web/css', f), 'utf8');
const context = vm.createContext({
  el, document: { createTextNode: (s) => new Node('#text', '', s), getElementById: () => null, createElement: (t) => new Node(t) },
  fmtWhen: () => 'fixture time', emptyRow: (t) => el('div', 'empty-row', t), els: {}, location: { hash: '#/agents' },
  fetch: async () => { throw new Error('no network in the fixture'); }, setTimeout: () => 0,
});
vm.runInContext(js('40-agents.js').slice(js('40-agents.js').indexOf('// Shared read-only lane summary')), context);
vm.runInContext(js('41-agents-schedule.js'), context);
vm.runInContext(js('60-settings.js'), context);
vm.runInContext('ritualRuns = () => []; outcomeStrip = () => el("span", "outcome-strip"); spiritStatusCache = null; spiritModels = {};', context);

const shadow = { primary: 'local DeepSeek', model: 'deepseek-v4.1-flash', status: 'shadow', productionRoute: 'disabled', productionOwner: 'none (retired)',
  pilotStatus: 'unused; one document maximum', canaryStatus: 'unknown', cost_policy: 'local-zero-marginal', cost_telemetry: 'unavailable',
  provider_binding: 'fixed-local-endpoint', configuredAuthority: 'valid declaration', fallback: 'owner-invoked Claude Code/Codex only; unsupported/unverified; never automatic',
  lastAttempt: 'unknown', lastError: 'unknown' };
const full = context.reIntakePrimarySummary(shadow);
assert.match(full, /provider binding: fixed-local-endpoint/);

// ---- the status row: one line, the fail-closed words visible, the sentence only in the tooltip ----
let row = context.reIntakeStatusRow(shadow);
assert.equal(row.className, 'sched-status');
assert.equal(row.getAttribute('role'), 'status');
assert.equal(row.children[0].textContent, 're-intake');
const chip = row.children[1];
assert.equal(chip.textContent, 'shadow');
assert.match(chip.className, /run-outcome oc-unknown/);
assert.equal(row.find(x=>x.has('sched-status-bits'))[0].textContent, 'route disabled · pilot unused · canary unknown · lane none');
const more = row.children[3];
assert.equal(more.tag, 'a');
assert.equal(more.textContent, 'details →');
assert.equal(more.href, '#/settings/agents/re-intake');
assert.doesNotMatch(row.textContent, /provider binding|cost telemetry|fallback|declaration/);
assert.equal(row.title, full);
assert.ok(row.textContent.length < 120, 'the board row stays one line: ' + row.textContent.length);

// blocked + refused: the danger chip, the refusal on the row, the row marked for attention
row = context.reIntakeStatusRow({ ...shadow, status: 'blocked', productionEnabled: true, productionRoute: 'blocked; owner boundary or authority invalid',
  productionOwner: 'Manifest (configured; deployment ownership requires review)', pilotStatus: 'stopped; owner review/reset required', lastAttempt: '35-deepseek-primary-canary.jsonl', lastError: 'missing usage evidence' });
assert.equal(row.className, 'sched-status attn');
assert.equal(row.children[1].textContent, 'blocked');
assert.match(row.children[1].className, /oc-error/);
assert.equal(row.find(x=>x.has('sched-status-bits'))[0].textContent, 'route blocked · pilot stopped · canary unknown · lane Manifest · last error missing usage evidence');
// the pilot: the open route reads as the owner-upload handoff, warn-toned
row = context.reIntakeStatusRow({ ...shadow, status: 'pilot; owner review required', productionRoute: 'owner upload → candidate → pending approval', canaryStatus: 'passed (synthetic only)', lastError: 'none reported (synthetic canary only)' });
assert.equal(row.children[1].textContent, 'pilot');
assert.match(row.children[1].className, /oc-late/);
assert.equal(row.find(x=>x.has('sched-status-bits'))[0].textContent, 'route owner upload → candidate → pending approval · pilot unused · canary passed (synthetic only) · lane none');
// no projection at all still fails closed, visibly
row = context.reIntakeStatusRow(null);
assert.equal(row.children[1].textContent, 'policy evidence unavailable');
assert.equal(row.find(x=>x.has('sched-status-bits'))[0].textContent, 'fail-closed · not routed');
assert.match(row.className, /attn/);

// ---- the SCHEDULE board: the status row, then the degrade notes, then the groups; no sentence paragraph ----
const board = el('div', 'sched-board');
context.els.spiritRitualBoard = board;
vm.runInContext('hermesInfo = { reIntakePrimary: ' + JSON.stringify(shadow) + ', dutyRefusals: [{ label: "successor refusal: no duty routed" }], cron: { list: [] } }; spiritRuns = { data: [], queued: [], primary: "excalibur" };', context);
context.renderSpiritRituals([{ spirit: 'warden', ritual: 'audit', valid: true, enabled: true, cadence: '0 8 * * 1', ceilingUsd: 1 }]);
assert.equal(board.children[0].className, 'sched-status');
for (const n of board.find((x) => x.has('sched-degraded'))) {
  assert.doesNotMatch(n.textContent, /provider binding|cost telemetry|last attempt receipt/, 'no policy sentence as a degrade paragraph');
  assert.ok(n.textContent.length < 160, n.textContent);
}
assert.equal(board.children[1].textContent, 'successor refusal: no duty routed');
const heads = board.find((x) => x.has('sched-group')).map((x) => x.find((y) => y.has('aion-sec-title'))[0].textContent);
assert.deepEqual(heads, ['SCHEDULED', 'ON DEMAND', 'PAUSED']);
assert.equal(board.children.indexOf(board.find((x) => x.has('sched-group'))[0]), 2, 'the first schedule group is the third child');
assert.doesNotMatch(js('41-agents-schedule.js'), /"sched-degraded", reIntakePrimarySummary\(/);
assert.doesNotMatch(js('42-agents-runs.js'), /"run-why", reIntakePrimarySummary\(/);
assert.match(js('42-agents-runs.js'), /reIntakeStatusRow\(hermesInfo && hermesInfo\.reIntakePrimary\)/);

// ---- Settings › Agents: the legacy card is compact; its inventory is a fold with every evidence string ----
const now = new Date().toISOString();
const h = { name: 'excalibur', path: '/home/benjamin/excalibur', engineAlive: true, heartbeat: now, primary: true,
  spirits: [{ name: 'warden', portal: 'deepseek' }, { name: 'scout' }],
  observation: { health: 'late', evidence: 'ritual-status.json', rituals: [
    { spirit: 'warden', ritual: 'audit', health: 'late', lastAttempt: now, why: 'overdue by 2d' },
    { spirit: 're-extractor', ritual: 'intake', health: 'paused', lastError: 'retired; history read-only' },
  ] } };
const portalRows = [{ id: 'deepseek', name: 'DeepSeek', kind: 'key', masked: 'sk-…', state: 'open' }];
const card = context.excaliburCard(h, portalRows);
const folds = card.find((x) => x.tag === 'details');
assert.equal(folds.length, 1, 'one fold on the legacy card');
const fold = folds[0];
assert.equal(fold.open, false, 'closed by default');
assert.match(fold.className, /harness-fold/);
const summary = fold.children[0];
assert.equal(summary.tag, 'summary');
assert.equal(summary.children[0].textContent, 'details');
assert.equal(summary.children[1].textContent, '2 rituals · evidence · conduits');
// level one: what stays visible when the fold is closed
const levelOne = card.children.filter((c) => c !== fold).map((c) => c.textContent).join('\n') + '\n' + summary.textContent;
assert.match(levelOne, /Excalibur engine/);
assert.match(levelOne, /historical runtime/);
assert.match(levelOne, /engine live/);
assert.match(levelOne, /\/home\/benjamin\/excalibur/);
assert.match(levelOne, /history preserved · current duties run under Manifest\/Hermes/);
assert.match(levelOne, /observation late/);
assert.match(levelOne, /2 rituals \(1 late, 1 paused\) · 2 agents · 1 conduit/);
for (const hidden of ['ritual-status.json', 'warden/audit', 're-extractor/intake', 'artifacts/runs/', 'DeepSeek', 'overdue by 2d', 'switch it on the agent page']) {
  assert.doesNotMatch(levelOne, new RegExp(hidden.replace(/[/()]/g, '\\$&')), hidden + ' is level two');
}
const tally = card.find((x) => x.has('harness-summary'))[0];
assert.match(tally.children[0].className, /run-outcome oc-late/);
// level two: the evidence, verbatim
const body = fold.children[1].textContent;
for (const kept of ['excalibur harness tree', 'read-only history (artifacts/runs/)', 'engine evidence: ritual-status.json', 'warden/audit', 'late · last attempt fixture time · overdue by 2d',
  're-extractor/intake', 'paused · last attempt unknown · retired; history read-only', '2 agents in historical configuration (spirits/)', 'warden', 'scout', 'deepseek', 'DeepSeek (sk-… · open)']) {
  assert.ok(body.includes(kept), 'fold keeps: ' + kept);
}
// an engine that is down still says so at level one
const down = context.excaliburCard({ ...h, engineAlive: false }, []);
assert.match(down.children.filter((c) => c.tag !== 'details').map((c) => c.textContent).join('\n'), /no live engine — delegations queue/);
assert.match(down.find((x) => x.has('harness-engine'))[0].textContent, /^down/);
const retiredCard = context.excaliburCard({ ...h, engineAlive:false, engineRetired:true }, []);
assert.match(retiredCard.find(x => x.has('harness-engine'))[0].textContent, /^retired$/);
assert.doesNotMatch(retiredCard.textContent, /delegations queue|still owns|keep running|runs its existing/);
assert.equal(retiredCard.find(x => x.tag === 'button').length, 0);
// no harness configured: no fold, the empty row
assert.equal(context.excaliburCard(null, []).find((x) => x.tag === 'details').length, 0);

// ---- Alfred: the status row at level one, the receipt folded — opened by #/settings/agents/re-intake ----
const hz = { gateway: null, runner: { enabled: false }, reIntakePrimary: shadow, cron: {}, profiles: [] };
let alfred = context.alfredCard(hz);
assert.equal(alfred.find((x) => x.has('sched-status')).length, 1);
let receipt = alfred.find((x) => x.tag === 'details' && x.has('harness-fold'))[0];
assert.equal(receipt.open, false);
let lines = receipt.children[1].find((x) => x.has('harness-receipt-line')).map((x) => x.textContent);
assert.deepEqual(lines, full.split(' · '));
assert.ok(lines.includes('provider binding: fixed-local-endpoint'));
assert.ok(lines.includes('last error: unknown'));
assert.doesNotMatch(alfred.children.filter((c) => c.tag !== 'details').map((c) => c.textContent).join('\n'), /provider binding/);
vm.runInContext('settingsArg = "re-intake";', context);
alfred = context.alfredCard(hz);
assert.equal(alfred.find((x) => x.tag === 'details' && x.has('harness-fold'))[0].open, true, 'the board link opens the receipt');
vm.runInContext('settingsArg = "";', context);

// the successor card leads the board; the legacy card follows it
const agentsPane = js('60-settings.js');
assert.ok(agentsPane.indexOf('board.append(alfredCard(hermes));') < agentsPane.indexOf('board.append(excaliburCard(primary'), 'alfred first');

// the phone: the row wraps its bits and the link, the fold summary is touch height, nothing pans sideways
const mobile = css('95-mobile.css');
assert.match(mobile, /\.sched-status-bits \{ flex: 1 1 100%; \}/);
assert.match(mobile, /\.harness-fold-summary \{ min-height: var\(--touch-sm\)/);
assert.match(css('60-settings.css'), /\.harness-receipt-line \{[^}]*overflow-wrap: anywhere/);
assert.match(css('40-spirits.css'), /\.sched-status \{ display: flex; flex-wrap: wrap/);
console.log('agents information-architecture fixtures passed');
