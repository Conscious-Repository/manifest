// Olga's simpler planner (2026-10-07 phone audit). Everything here is her own
// layer: it re-renders the shared task, timeline and to-resolve views for a
// non-technical phone user, and leaves Benjamin's Manifest untouched. Data is
// read and written through the same APIs as before.
const olgaSimple = { open: JSON.parse(localStorage.getItem('olga.groups') || 'null') || { important: true, Inbox: true, Home: true }, justAdded: '', qOpen: '', undo: null };
const olgaPhone = () => matchMedia('(max-width: 860px)').matches;
const OLGA_VIEWS = [['list', 'Tasks'], ['timeline', 'Timeline'], ['homefeed', 'Questions']];
if (todosMode === 'board') todosMode = 'list';

// ---- the toolbar: three views, search only where it works, one add button ----
renderTodosToolbar = function () {
  const bar = document.getElementById('todosToolbar');
  const focused = document.activeElement?.id === 'todosSearch', caret = focused ? document.activeElement.selectionStart : 0;
  bar.replaceChildren();
  const views = el('div', 'olga-task-views'); views.setAttribute('role', 'tablist');
  const qCount = typeof hpFeedItems === 'function' && typeof hp !== 'undefined' && hp.data ? olgaQuestions(hp.data).open.length : 0;
  for (const [mode, label] of OLGA_VIEWS) {
    const b = el('button', 'olga-view' + (todosMode === mode ? ' on' : ''), label);
    if (mode === 'homefeed' && qCount) b.append(el('span', 'olga-view-count', String(qCount)));
    b.type = 'button'; b.setAttribute('role', 'tab'); b.setAttribute('aria-selected', String(todosMode === mode));
    b.onclick = () => { todosMode = mode; localStorage.setItem('olga.tasks.view', mode); renderTodos(); document.getElementById('contentScroll')?.scrollTo(0, 0); };
    views.append(b);
  }
  bar.append(views);
  if (todosMode === 'list') {
    const row = el('div', 'olga-find');
    const search = inputEl('Find a task'); search.id = 'todosSearch'; search.value = todosQuery; search.type = 'search'; search.setAttribute('aria-label', 'Find a task');
    search.oninput = () => { todosQuery = search.value; renderTodos(); };
    const area = el('select', 'olga-area-filter'); area.setAttribute('aria-label', 'Show one area');
    const all = el('option', '', 'All areas'); all.value = ''; area.append(all);
    for (const name of olgaTaskAreas(todosCache)) { const o = el('option', '', name); o.value = name; area.append(o); }
    area.value = olgaTaskArea; area.onchange = () => { olgaTaskArea = area.value; localStorage.setItem('olga.tasks.area', olgaTaskArea); renderTodos(); };
    row.append(search, area);
    bar.append(row);
    if (focused) { search.focus(); search.setSelectionRange(caret, caret); }
  }
  const add = el('button', 'olga-add-fab', '＋'); add.type = 'button'; add.setAttribute('aria-label', 'Add a task'); add.title = 'Add a task';
  add.append(el('span', 'olga-add-label', ' Add task'));
  add.onclick = () => openTodoQuickAdd('', olgaTaskArea ? { domain: olgaTaskArea } : {});
  bar.append(add);
};

// ---- the list: what matters first, then one fold per area ----
const olgaPrevRenderTodos = renderTodos;
renderTodos = function () {
  if (!todosCache) return;
  if (todosMode !== 'list') return olgaPrevRenderTodos();
  const host = els.todosRows; host.replaceChildren();
  const counts = todosCache.counts || {};
  if (typeof setCrumbMeta === 'function' && !els.todosView.hidden) setCrumbMeta((counts.tasks || 0) + ' open');
  renderTodosToolbar();
  if (todosLoadError) { const e = el('div', 'tdo-load-error', todosLoadError); e.append(pillLight('Try again', loadTodos)); host.append(e); }
  const rows = (todosCache.rows || []).filter(r => todoMatches(r));
  if (!rows.length) host.append(el('p', 'olga-empty', todosQuery ? 'No tasks match “' + todosQuery + '”.' : 'Nothing here yet — tap ＋ to add a task.'));
  const important = rows.filter(r => r.priority === 'high' || r.priority === 'med');
  const groups = new Map();
  for (const r of rows) { const a = r.container?.name || 'Inbox'; if (!groups.has(a)) groups.set(a, []); groups.get(a).push(r); }
  const order = [...groups.keys()].sort((a, b) => (a === 'Inbox' ? -1 : b === 'Inbox' ? 1 : a === 'Home' ? -1 : b === 'Home' ? 1 : a.localeCompare(b)));
  const searching = !!todosQuery;
  const fold = (key, title, list, hint) => {
    const det = el('details', 'olga-group'); det.open = searching || !!olgaSimple.open[key] || list.some(r => r.id === olgaSimple.justAdded);
    const sum = el('summary', 'olga-group-head'); sum.append(el('span', 'olga-group-title', title), el('span', 'olga-group-count', String(list.length)));
    det.append(sum);
    if (hint) det.append(el('p', 'olga-group-hint', hint));
    det.ontoggle = () => { if (searching) return; olgaSimple.open[key] = det.open; localStorage.setItem('olga.groups', JSON.stringify(olgaSimple.open)); };
    list.forEach((r, i) => det.append(rankedRow(r, i)));
    host.append(det);
  };
  if (important.length && !olgaTaskArea && !searching) fold('important', 'Important', important, 'Tasks marked high or medium priority.');
  for (const a of order) fold(a, a === 'Home' ? 'Home · shared' : a, groups.get(a));
  // done today stays reachable
  const dones = [];
  (todosCache.domains || []).forEach(dom => [...(dom.tasks || []), ...(dom.buckets || []).flatMap(b => b.tasks || [])].forEach(t => { if (t.state === 'done' && todoSearchMatches(t)) dones.push({ t, domain: dom.name }); }));
  if (dones.length) {
    const det = el('details', 'olga-group olga-done'); const sum = el('summary', 'olga-group-head');
    sum.append(el('span', 'olga-group-title', 'Done'), el('span', 'olga-group-count', String(dones.length))); det.append(sum);
    dones.slice(0, 40).forEach(({ t, domain }) => {
      const row = el('div', 'tdo-row done olga-done-row');
      const check = el('button', 'tdo-check on', '✓'); check.setAttribute('aria-label', 'Not done yet'); check.onclick = () => todosApi('/api/tasks/check', { id: t.id, checked: false });
      const text = el('div', 'olga-done-text'); text.append(el('span', 'tdo-text', t.text), el('span', 'olga-row-area', domain));
      row.append(check, text); det.append(row);
    });
    host.append(det);
  }
  if (typeof liberTaskThreadIds === 'function') liberTaskThreadIds().then(map => {
    host.querySelectorAll('.tdo-row[data-id]').forEach(row => { if (map.has(row.dataset.id) && !row.querySelector('.tdo-liber-mark')) { const m = el('span', 'tdo-liber-mark', '💬'); m.title = 'You talked with Liber about this'; row.querySelector('.tdo-task-title')?.append(m); } });
  });
  if (olgaSimple.justAdded) {
    const id = olgaSimple.justAdded; olgaSimple.justAdded = '';
    const row = host.querySelector('.tdo-row[data-id="' + CSS.escape(id) + '"]');
    if (row) { row.classList.add('olga-new'); row.scrollIntoView({ block: 'center', behavior: 'smooth' }); setTimeout(() => row.classList.remove('olga-new'), 2600); }
  }
};

// quieter rows: no age, no hourglass, no ✕ (remove lives in the task's panel)
const olgaSimpleRow = rankedRow;
rankedRow = function (r, i) {
  const row = olgaSimpleRow(r, i);
  row.dataset.id = r.id;
  row.querySelectorAll('.tdo-age,.tdo-tether,.uw-x,.tdo-handle').forEach(n => n.remove());
  row.draggable = false;
  const check = row.querySelector('.tdo-check'); if (check) check.setAttribute('aria-label', 'Mark “' + r.text + '” done');
  if (!olgaTaskArea && r.container?.name && !row.querySelector('.olga-row-area')) {
    const meta = row.querySelector('.tdo-right') || row;
    row.querySelectorAll('.tdo-pill').forEach(n => n.remove());
    const area = el('span', 'olga-row-area', r.container.name);
    row.querySelector('.tdo-task-title')?.after(area) || meta.append(area);
  }
  return row;
};

// ---- the timeline: a list of weekends, this one first ----
const olgaChartPaint = typeof hpPaint === 'function' ? hpPaint : null;
const HP_WEEKDAY = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
function olgaWeekendLabel(sat) { return typeof hpWeekend === 'function' ? hpWeekend(sat) : sat; }
function olgaPeople(ids, p) { return (ids || []).map(id => (p.people?.[id]?.name) || (id.charAt(0).toUpperCase() + id.slice(1))).join(' & '); }
hpPaint = function () {
  const host = hpHost; host.replaceChildren();
  if (hpUnavailable(host)) return;
  const v = hp.data, p = v.plan, d = v.derived, c = d.capacity;
  const deadline = c.hardDeadline ? hpShort(c.hardDeadline) : '';
  const qs = olgaQuestions(v).open.length;
  const goal = Object.values(p.milestones || {}).filter(m => m.kind === 'deadline' && m.date).sort((a, b) => a.date.localeCompare(b.date))[0];
  const lead = el('div', 'olga-tl-lead');
  const ok = d.sequence ? d.sequence.fits : c.remainingHours >= 0;
  lead.append(el('p', 'olga-tl-sentence', (ok ? 'On track' : 'Tight') + (deadline ? ' for ' + deadline : '') + (goal ? ' — ' + goal.title.charAt(0).toLowerCase() + goal.title.slice(1) : '') + '.'));
  const sub = [];
  if (c.unknownItems.length) sub.push(c.unknownItems.length + ' job' + (c.unknownItems.length === 1 ? '' : 's') + ' still need an hours guess');
  if (qs) sub.push(qs + ' question' + (qs === 1 ? '' : 's') + ' to answer');
  if (sub.length) { const s = el('p', 'olga-tl-sub', sub.join(' · ') + '.'); if (qs) { const go = el('button', 'olga-link', 'Answer them ›'); go.type = 'button'; go.onclick = () => homePlanShowFeed(); s.append(' ', go); } lead.append(s); }
  host.append(lead);
  const placed = {};
  (d.items || []).forEach(it => { if (it.done) return; const item = hpItemOf(p, it) || {}; Object.entries(item.allocations || {}).forEach(([sat, h]) => (placed[sat] = placed[sat] || []).push({ title: it.title, hours: h, sure: true })); });
  (d.sequence?.placements || []).forEach(pl => (placed[pl.weekend] = placed[pl.weekend] || []).push({ title: hpTitle(pl.ref), hours: pl.hours, sure: false }));
  const list = el('ol', 'olga-weekends');
  const upcoming = d.weeks.filter(w => !w.past);
  upcoming.forEach((w, i) => {
    const li = el('li', 'olga-weekend' + (i === 0 ? ' is-now' : '') + (w.status === 'away' ? ' is-away' : ''));
    const head = el('div', 'olga-wk-head');
    head.append(el('span', 'olga-wk-date', (i === 0 ? 'This weekend · ' : i === 1 ? 'Next weekend · ' : '') + olgaWeekendLabel(w.saturday)));
    if (w.normalHigh != null) head.append(el('span', 'olga-wk-temp', Math.round(w.normalHigh) + '°'));
    li.append(head);
    const lines = [];
    (w.away || []).forEach(id => { const a = p.away?.[id]; if (a) lines.push(['away', (a.note || olgaPeople(a.who, p) + ' away')]); });
    (w.events || []).forEach(id => { const e = p.events?.[id]; if (e) lines.push(['event', e.title]); });
    (w.milestones || []).forEach(id => { const m = p.milestones?.[id]; if (m) lines.push(['goal', '🏁 ' + m.title]); });
    (w.reserved || []).forEach(id => { const r = p.reservations?.[id]; if (r) lines.push([r.kind === 'contingency' ? 'spare' : 'work', (r.kind === 'contingency' ? 'Spare time, kept free' : r.purpose) + ' · about ' + Math.round(r.hours) + ' h']); });
    (placed[w.saturday] || []).forEach(x => lines.push(['work', hpCap(x.title) + ' · about ' + Math.round(x.hours) + ' h' + (x.sure ? '' : ' (suggested)')]));
    if (!lines.length) lines.push(['free', w.status === 'shared' ? 'Nothing planned yet' : w.status === 'solo' ? 'Only one of you is around' : 'Not a work weekend']);
    const ul = el('ul', 'olga-wk-lines');
    lines.forEach(([kind, text]) => ul.append(el('li', 'is-' + kind, text)));
    li.append(ul);
    list.append(li);
  });
  host.append(list);
  if (!olgaPhone() && olgaChartPaint) {
    const det = el('details', 'olga-chart-fold'); det.append(el('summary', '', 'Show the chart'));
    const box = el('div', 'hp'); det.append(box);
    det.ontoggle = () => { if (!det.open || box.childElementCount) return; const keep = hpHost; hpHost = box; try { olgaChartPaint(); } finally { hpHost = keep; } };
    host.append(det);
  }
};

// ---- questions: one at a time, nothing final without "Save answer" ----
function olgaQuestions(v) {
  const all = typeof hpFeedItems === 'function' ? hpFeedItems(v) : [];
  const putOff = [];
  if (v && v.plan) {
    const add = (path, task, dec) => { if (dec.status === 'deferred') putOff.push({ path, task, dec }); };
    Object.entries(v.plan.decisions || {}).forEach(([id, dec]) => add(['decisions', id], '', dec));
    Object.entries(v.plan.tasks || {}).forEach(([tid, t]) => Object.entries(t.decisions || {}).forEach(([id, dec]) => add(['tasks', tid, 'decisions', id], tid, dec)));
  }
  // what the coming weekends need first: conflicts, then decisions, then hours …
  return { open: all.filter(x => x.kind !== 'conflict' || true), putOff };
}
function olgaQTitle(x) {
  const lc = s => s.charAt(0).toLowerCase() + s.slice(1);
  return { decision: x.title, hours: 'About how many hours will ' + lc(x.title) + ' take?', wait: 'How long is the wait for ' + lc(x.it?.title || x.title) + '?', price: 'What will ' + lc(x.title) + ' cost?', conflict: x.title }[x.kind] || x.title;
}
async function olgaSaveAnswer(card, patch, path, expect, undoPatch, message) {
  card.classList.add('is-saving');
  try {
    const cur = await hpFetch('GET', '/api/home/plan');
    if (JSON.stringify(hpGet(cur.plan, path) ?? null) !== JSON.stringify(expect ?? null)) { hp.data = cur; await hpRefreshView(); showToast('Benjamin answered this a moment ago — have a look.'); return; }
    hp.data = await hpFetch('POST', '/api/home/plan', { revision: cur.revision, patch });
    olgaSimple.qOpen = '';
    olgaToast(message || 'Saved', undoPatch ? async () => {
      const now = await hpFetch('GET', '/api/home/plan');
      hp.data = await hpFetch('POST', '/api/home/plan', { revision: now.revision, patch: undoPatch });
      await hpRefreshView(); renderTodosToolbar();
    } : null);
    await hpRefreshView(); renderTodosToolbar();
  } catch (e) {
    card.classList.remove('is-saving');
    const err = card.querySelector('.olga-q-error') || card.appendChild(el('p', 'olga-q-error'));
    err.textContent = e.status === 409 ? 'Someone saved at the same moment — try again.' : (e.problems?.length ? e.problems.join('; ') : e.message);
  }
}
hpFeedPaint = function () {
  const host = hpFeedHost; host.replaceChildren();
  if (hpUnavailable(host)) return;
  const v = hp.data, { open, putOff } = olgaQuestions(v);
  host.append(el('p', 'olga-q-lead', open.length ? (open.length === 1 ? 'One question about the house.' : open.length + ' questions about the house.') + ' Tap one to answer it.' : 'Nothing to answer right now.'));
  const list = el('div', 'olga-q-list');
  open.forEach(x => {
    const isOpen = olgaSimple.qOpen === x.key;
    const item = el('div', 'olga-q' + (isOpen ? ' is-open' : ''));
    const head = el('button', 'olga-q-head'); head.type = 'button'; head.setAttribute('aria-expanded', String(isOpen));
    head.append(el('span', 'olga-q-title', olgaQTitle(x)));
    if (x.task) head.append(el('span', 'olga-q-for', 'For: ' + hpCap((v.tasks?.[x.task] || {}).text || x.task)));
    head.onclick = () => { olgaSimple.qOpen = isOpen ? '' : x.key; hpFeedPaint(); };
    item.append(head);
    if (isOpen) item.append(olgaQuestionCard(v, x));
    list.append(item);
  });
  host.append(list);
  if (putOff.length) {
    const det = el('details', 'olga-group olga-putoff'); const sum = el('summary', 'olga-group-head');
    sum.append(el('span', 'olga-group-title', 'Put off'), el('span', 'olga-group-count', String(putOff.length))); det.append(sum);
    putOff.forEach(x => {
      const row = el('div', 'olga-putoff-row'); row.append(el('span', '', x.dec.question));
      const back = el('button', 'pill light', 'Bring back'); back.type = 'button';
      back.onclick = () => olgaSaveAnswer(row, hpPatchAt(x.path, { status: 'open' }), x.path.concat(['status']), 'deferred', null, 'Back in your questions');
      row.append(back); det.append(row);
    });
    host.append(det);
  }
};
function olgaQuestionCard(v, x) {
  const card = el('form', 'olga-q-card');
  const save = el('button', 'pill olga-primary', 'Save answer'); save.type = 'submit'; save.disabled = true;
  const later = el('button', 'pill light', 'Ask me later'); later.type = 'button';
  const actions = el('div', 'olga-q-actions');
  let value = null;
  const ready = ok => { save.disabled = !ok; };
  if (x.kind === 'decision') {
    if (x.dec.note) { const det = el('details', 'olga-q-more'); det.append(el('summary', '', 'More about this'), el('p', '', x.dec.note)); card.append(det); }
    const opts = el('div', 'olga-q-options'); const input = inputEl((x.dec.options || []).length ? 'Or write your own answer' : 'Your answer');
    input.setAttribute('aria-label', 'Your answer');
    (x.dec.options || []).forEach(o => { const b = el('button', 'olga-q-option', o); b.type = 'button'; b.setAttribute('aria-pressed', 'false'); b.onclick = () => { opts.querySelectorAll('.olga-q-option').forEach(n => n.setAttribute('aria-pressed', 'false')); b.setAttribute('aria-pressed', 'true'); input.value = ''; value = o; ready(true); }; opts.append(b); });
    input.oninput = () => { opts.querySelectorAll('.olga-q-option').forEach(n => n.setAttribute('aria-pressed', 'false')); value = input.value.trim(); ready(!!value); };
    if ((x.dec.options || []).length) card.append(opts);
    card.append(input);
    card.onsubmit = e => { e.preventDefault(); if (!value) return; olgaSaveAnswer(card, hpPatchAt(x.path, { status: 'decided', answer: value }), x.path.concat(['status']), 'open', hpPatchAt(x.path, { status: 'open', answer: null }), 'Saved: ' + value); };
    later.onclick = () => olgaSaveAnswer(card, hpPatchAt(x.path, { status: 'deferred' }), x.path.concat(['status']), 'open', hpPatchAt(x.path, { status: 'open' }), 'Put off — find it under “Put off”');
    actions.append(save, later);
  } else if (x.kind === 'hours' || x.kind === 'wait' || x.kind === 'price') {
    const hint = x.kind === 'hours' ? (x.it?.low != null || x.it?.high != null ? 'Our guess so far: ' + (x.it.low ?? '?') + '–' + (x.it.high ?? '?') + ' hours, both of you together.' : 'Hours for both of you together.') : x.kind === 'wait' ? 'Days of waiting (delivery, drying, a crew) — not your own work time.' : 'Out of the shared house budget.' + (x.line?.note ? ' ' + x.line.note : '');
    card.append(el('p', 'olga-q-hint', hint));
    const i = inputEl(x.kind === 'hours' ? 'Hours' : x.kind === 'wait' ? 'Days' : 'Amount in $'); i.type = 'number'; i.min = '0'; i.step = x.kind === 'price' ? '1' : '0.5'; i.inputMode = 'decimal'; i.setAttribute('aria-label', i.placeholder);
    i.oninput = () => ready(i.value !== '' && Number(i.value) >= 0);
    card.append(i);
    card.onsubmit = e => {
      e.preventDefault(); if (i.value === '') return; const n = Number(i.value);
      let patch = hpPatchAt(x.path, n);
      if (x.kind === 'hours') patch = hpMergePatches(patch, hpPatchAt(x.path.slice(0, -1).concat(['basis']), 'user'));
      if (x.kind === 'price') patch = hpMergePatches(patch, hpPatchAt(x.path.slice(0, -1).concat(['status']), 'estimate'));
      olgaSaveAnswer(card, patch, x.path, null, hpPatchAt(x.path, null), 'Saved');
    };
    actions.append(save);
  } else {
    card.append(el('p', 'olga-q-hint', 'This is a clash in the plan. Open the task to sort it out, or ask Liber about it.'));
  }
  if (x.task) { const openT = el('button', 'pill light', 'Open the task'); openT.type = 'button'; openT.onclick = () => homePlanOpen(x.task); actions.append(openT); }
  card.append(actions);
  setTimeout(() => card.querySelector('input,button.olga-q-option')?.focus({ preventScroll: true }), 0);
  return card;
}

// ---- a toast that can undo ----
function olgaToast(message, undo) {
  let host = document.querySelector('.olga-toast');
  if (!host) { host = el('div', 'olga-toast'); host.setAttribute('role', 'status'); document.body.append(host); }
  clearTimeout(olgaSimple.toastTimer);
  host.replaceChildren(el('span', '', message));
  if (undo) { const b = el('button', 'olga-toast-undo', 'Undo'); b.type = 'button'; b.onclick = async () => { host.hidden = true; try { await undo(); olgaToast('Undone'); } catch (e) { showToast(e.message); } }; host.append(b); }
  host.hidden = false;
  olgaSimple.toastTimer = setTimeout(() => { host.hidden = true; }, undo ? 10000 : 4000);
}
