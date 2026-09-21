// Approvals card — visibility suggestion on a synced transcript note
// (2026-09-21): the suggested tier is pre-selected as the live chip with the
// other two as one-tap alternatives; the row shows only while the note
// carries the `aion` category and follows the category chips live; Confirm
// sends the tier only when the row was offered. Same chip vocabulary as the
// people/category rows — no new card shape.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');

function el(tag, cls, text) {
  const node = { tag, cls: cls || '', textContent: text == null ? '' : text, children: [], title: '', hidden: false,
    append(...children) { for (const c of children) { if (c && typeof c === 'object') { c.parent = this; this.children.push(c); } } },
    querySelector(selector) { return this.querySelectorAll(selector)[0] || null; },
    querySelectorAll(selector) { const found = []; for (const c of this.children) { if (!c || typeof c !== 'object') continue; if ((c.cls || '').split(' ').includes(selector.slice(1))) found.push(c); found.push(...c.querySelectorAll(selector)); } return found; },
    addEventListener() {}, classList: { add() {}, toggle() {} } };
  Object.defineProperty(node, 'innerHTML', { set(v) { if (v === '') node.children = []; }, get() { return ''; } });
  return node;
}
const js = (f) => fs.readFileSync(path.join(__dirname, '../web/js', f), 'utf8');
const posted = [];
const context = vm.createContext({
  el, els: {}, document: { createTextNode: (s) => s, getElementById: () => null, querySelectorAll: () => [] },
  attachWikilinkAutocomplete() {}, showToast() {}, askText() {},
  postApprovalDecision: (id, kind, body) => posted.push({ id, kind, body }),
});
vm.runInContext(js('55-approvals.js'), context);
// the fixture's stub must win over the file's own definition
vm.runInContext('postApprovalDecision = (id, kind, body) => __posted.push({ id, kind, body });', Object.assign(context, { __posted: posted }));

const chipText = (wrap) => wrap.querySelectorAll('.attendee-chips')[0].children.map((c) => c.textContent || c.children.map((k) => k.textContent).join(''));

// unknown Granola transcript → held pre-selected (the ladder reads open → internal → held), open/internal offered
const categories = ['aion'];
const ref = { value: null, shown: () => false };
const v = context.buildVisibilityEditor({ suggested: 'held', known: false, note: '2026-09-21 x.md', source: 'granola' }, categories, ref);
assert.equal(ref.value, 'held');
assert.equal(v.wrap.hidden, false, 'an aion note shows the row');
assert.equal(ref.shown(), true);
const label = v.wrap.children[0];
assert.match(label.cls, /appr-attendees-label/);
assert.match(label.textContent, /^Visibility — suggested: held/);
assert.match(label.textContent, /Granola transcript not yet tiered/);
assert.deepEqual(chipText(v.wrap), ['＋ open', '＋ internal', 'held · suggested']);
const live = v.wrap.querySelector('.vis-current');
assert.match(live.cls, /attendee-chip/);
assert.match(live.cls, /cat-live/);
assert.match(live.title, /private/);
const picks = v.wrap.querySelectorAll('.vis-pick');
assert.equal(picks.length, 2);
assert.match(picks[0].cls, /attendee-add-btn/);
assert.match(picks[0].cls, /cat-suggest/);

// override: tap another tier → it becomes the live chip, held is offered back
picks[0].onclick();
assert.equal(ref.value, 'open');
assert.deepEqual(chipText(v.wrap), ['open', '＋ internal', '＋ held']);

// the row follows the aion category: removed → hidden, added back → shown
categories.splice(0, 1);
v.sync();
assert.equal(v.wrap.hidden, true, 'a non-aion note hides the row');
assert.equal(ref.shown(), false);
categories.push('AION');
v.sync();
assert.equal(v.wrap.hidden, false, 'the category match is case-insensitive');

// the category editor drives sync through its onChange hook
let synced = 0;
const cats = ['aion'];
const catEditor = context.buildCategoryEditor(cats, () => { synced++; });
assert.ok(synced >= 1, 'first render fires onChange');
const remove = catEditor.querySelector('.attendee-remove');
remove.onclick();
assert.deepEqual(cats, []);
assert.ok(synced >= 2, 'removing a category fires onChange');

// a known transcript says so and pre-selects the data file's tier
const ref2 = { value: null, shown: () => false };
const known = context.buildVisibilityEditor({ suggested: 'internal', known: true, note: '2026-09-20 rj sync.md', source: 'pocket' }, ['aion'], ref2);
assert.equal(ref2.value, 'internal');
assert.match(known.wrap.children[0].textContent, /already tiered as internal/);
assert.deepEqual(chipText(known.wrap), ['＋ open', 'internal · suggested', '＋ held']);

// Confirm carries the tier only when the row was offered
context.spiritApprovalAct('g1', 'confirm', { attendees: ['jane'], title: 't', categories: ['aion'], visibility: 'open' });
assert.equal(posted.length, 1);
assert.equal(posted[0].body.editVisibility, true);
assert.equal(posted[0].body.visibility, 'open');
assert.equal(posted[0].body.editCategories, true);
context.spiritApprovalAct('n1', 'confirm', { attendees: [], title: 't', categories: ['personal'], visibility: null });
assert.equal(posted.length, 2);
assert.equal(posted[1].body.editVisibility, undefined, 'a non-aion note sends no tier');
assert.equal(posted[1].body.visibility, undefined);

// the file never invents a dialog for this: no prompt/confirm/alert
assert.doesNotMatch(js('55-approvals.js'), /\bwindow\.(prompt|confirm|alert)\(/);
console.log('approvals visibility ok');
