// fundraising-touch-rows.cjs — the tracker's touch lines (2026-09-25): a row
// shows one quiet "kind · date · person" line under each of last touch and
// next touch, nothing when a touch has no source; a name typed into the
// Sheet reads as pending, not as a contact; the inspector's date editor shows
// the winning touch and keeps a losing hand-typed date visible as "manual".
// Run: node server/testdata/fundraising-touch-rows.cjs
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
function el(tag, cls, text) {
  const node = { tag, cls: cls || '', textContent: text == null ? '' : String(text), children: [], title: '', hidden: false, type: '', value: '',
    append(...items) { for (const c of items) if (c && typeof c === 'object') this.children.push(c); },
    text() { return (this.textContent + ' ' + this.children.map((c) => c.text()).join(' ')).replace(/\s+/g, ' ').trim(); },
    find(cls) { const out = []; const walk = (n) => { for (const c of n.children) { if ((c.cls || '').split(' ').includes(cls)) out.push(c); walk(c); } }; walk(this); return out; },
    setAttribute() {}, classList: { add() {}, toggle() {} } };
  return node;
}
const src = fs.readFileSync(path.join(__dirname, '../web/js/94-aion-fundraising.js'), 'utf8');
const slice = (start, end) => { const s = src.indexOf(start); assert.ok(s >= 0, 'missing ' + start); const e = src.indexOf(end, s + 1); assert.ok(e > s, 'missing ' + end); return src.slice(s, e); };
const ctx = vm.createContext({ el, frSel: null, location: { hash: '' }, renderAion() {} });
vm.runInContext(slice('function frRow(op, paint)', '\nfunction money(v)'), ctx);

const op = {
  id: 'fr/a16z', firm: 'a16z', lastTouchpoint: 'call schedule', lastTouchpointDate: '2026-09-18', nextStep: 'call',
  people: [{ key: 'daisy wolf', display: 'daisy wolf', notePath: 'daisy wolf.md' }], unlinkedPeople: ['Typed Person'],
  lastTouch: { date: '2026-09-18', kind: 'manual' },
  nextTouch: { date: '2026-10-02', kind: 'upcoming', person: 'daisy wolf', title: 'a16z · follow-up' },
};
const row = ctx.frRow(op, () => {});
const touches = row.find('fr-touch');
assert.equal(touches.length, 2, 'one line under last touch, one under next touch');
assert.equal(touches[0].text(), 'manual 2026-09-18', 'a hand-typed date that won reads as manual');
assert.equal(touches[1].text(), 'upcoming 2026-10-02 daisy wolf', 'kind · date · person');
assert.equal(touches[1].title, 'a16z · follow-up', 'the event title rides in the tooltip');
assert.equal(row.find('fr-touch-kind').map((k) => k.cls.includes('micro-label')).every(Boolean), true, 'kinds are micro-labels');
const pending = row.find('fr-person-pending');
assert.equal(pending.length, 1); assert.equal(pending[0].textContent, 'Typed Person');
assert.equal(row.find('fr-person').length, 1, 'the linked contact is the only person button');

// a touch with no source renders no line
const bare = ctx.frRow({ id: 'x', firm: 'Quiet', people: [] }, () => {});
assert.equal(bare.find('fr-touch').length, 0, 'an absence shows only when it changes the call: no line');
assert.equal(bare.find('fr-person-empty').length, 1);

// the inspector's touch editor: winning touch beneath the input; a losing manual date stays visible
const fields = [];
const field = (label, node) => fields.push({ label, node });
ctx.frTouchField(null, field, () => {}, 'last touch date', 'lastTouchpointDate', '2026-09-01', { date: '2026-09-15', kind: 'met', person: 'Ethan' });
assert.equal(fields[0].label, 'last touch date');
const lines = fields[0].node.find('fr-touch');
assert.deepEqual(lines.map((l) => l.text()), ['met 2026-09-15 Ethan', 'manual 2026-09-01'], 'the override is never hidden');
assert.equal(fields[0].node.find('fr-person-rm').length, 1, 'a typed date can be cleared');
ctx.frTouchField(null, field, () => {}, 'next touch date', 'nextStepDue', '', null);
assert.equal(fields[1].node.find('fr-touch').length, 0);
assert.equal(fields[1].node.find('fr-person-rm').length, 0, 'nothing to clear');
console.log('PASS: touch lines read kind · date · person, pending names stay pending, overrides stay visible.');
