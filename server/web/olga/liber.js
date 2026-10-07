// Liber — Olga's assistant: the CHAT screen and the "Ask Liber" section in a
// task. Plan: system/workbench/plans/2026-10-07-olga-chat.md. The server owns
// every turn; this page only shows threads and sends her words and taps.
const liber = { threads: [], open: null, stop: null, listStamp: 0, taskThreads: null };
const LIBER_STARTERS_APP = ['Make the text bigger', 'Change the colours', 'How does the timeline work?'];
const LIBER_STARTERS_TASK = ['What should we decide first?', 'What could this cost?', 'What’s the next step?'];

async function liberApi(path, body) {
  const r = await fetch(path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {});
  if (!r.ok) throw new Error((await r.text()).trim() || 'Something went wrong — try again.');
  return r.json();
}

function liberRef(th) { return th.kind === 'task' ? { task: th.taskId } : { id: th.id }; }
function liberQuery(ref) { return ref.task ? 'task=' + encodeURIComponent(ref.task) : 'id=' + encodeURIComponent(ref.id); }

// Plain text with paragraphs, simple bullets and **bold** — never HTML.
function liberText(text) {
  const box = el('div', 'liber-text');
  for (const block of String(text || '').split(/\n{2,}/)) {
    const lines = block.split('\n');
    if (lines.every(l => /^\s*([-*•]|\d+[.)])\s+/.test(l))) {
      const list = el(/^\s*\d/.test(lines[0]) ? 'ol' : 'ul');
      for (const l of lines) list.append(liberInline(el('li'), l.replace(/^\s*([-*•]|\d+[.)])\s+/, '')));
      box.append(list);
    } else box.append(liberInline(el('p'), lines.join('\n')));
  }
  return box;
}
function liberInline(node, s) {
  s.split(/(\*\*[^*]+\*\*)/).forEach(part => {
    if (/^\*\*[^*]+\*\*$/.test(part)) node.append(el('strong', '', part.slice(2, -2)));
    else if (part) node.append(document.createTextNode(part.replace(/`([^`]+)`/g, '$1')));
  });
  return node;
}

function liberWhen(iso) {
  const d = new Date(iso), now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

// ---- one card ----
function liberCard(th, c, onThread) {
  const card = el('div', 'liber-card is-' + c.kind + ' is-' + c.state);
  const act = async (action, btn) => {
    card.querySelectorAll('button').forEach(b => b.disabled = true);
    if (btn) btn.textContent = '…';
    try { onThread(await liberApi('/api/liber/act', { ...liberRef(th), card: c.id, action })); }
    catch (e) { card.querySelectorAll('button').forEach(b => b.disabled = false); showToast(e.message); if (btn) btn.textContent = btn.dataset.label; }
  };
  const button = (label, action, cls = 'pill light') => { const b = el('button', cls, label); b.type = 'button'; b.dataset.label = label; b.onclick = () => act(action, b); return b; };
  const row = el('div', 'liber-card-actions');
  if (c.kind === 'proposal') {
    card.append(el('p', 'liber-card-summary', c.summary));
    if (c.state === 'pending') {
      const verb = { 'task.add': 'Add', 'task.note': 'Add to notes', 'plan.patch': 'Update the plan', 'task.update': 'Apply' }[c.proposal?.kind] || 'Apply';
      row.append(button(verb, 'apply', 'pill olga-primary'), button('Not now', 'decline'));
    } else card.append(el('p', 'liber-card-state', (c.state === 'applied' ? '✓ ' : '') + (c.message || '')));
  } else if (c.kind === 'confirm') {
    card.append(el('p', 'liber-card-label', 'Change your app?'), el('p', 'liber-card-summary', c.summary));
    if (c.state === 'pending') row.append(button('Make this change', 'make', 'pill olga-primary'), button('Not now', 'decline'));
    else card.append(el('p', 'liber-card-state', c.state === 'applied' ? '✓ Asked for' : 'Not now'));
  } else if (c.kind === 'change') {
    if (c.state === 'building') {
      card.append(el('p', 'liber-card-summary liber-working', 'Working on your change…'), el('p', 'liber-card-state', 'This usually takes a few minutes. You can leave this page.'));
    } else if (c.state === 'ready') {
      card.append(el('p', 'liber-card-label', 'Ready to look at'), el('p', 'liber-card-summary', c.summary));
      const open = el('a', 'pill light', 'Open preview'); open.href = '/preview/' + c.changeId + '/'; open.target = '_blank'; open.rel = 'noopener';
      row.append(open, button('Use this', 'use', 'pill olga-primary'), button('Not this', 'discard'));
    } else if (c.state === 'deploying') {
      card.append(el('p', 'liber-card-summary liber-working', c.message || 'Putting it live…'));
    } else if (c.state === 'live') {
      card.append(el('p', 'liber-card-summary', c.summary), el('p', 'liber-card-state', '✓ ' + (c.message || 'Live')));
      const refresh = el('button', 'pill olga-primary', 'Refresh to see it'); refresh.type = 'button'; refresh.onclick = () => location.reload();
      row.append(refresh, button('Undo', 'undo'));
    } else card.append(el('p', 'liber-card-summary', c.summary), el('p', 'liber-card-state', c.message || ''));
  } else {
    card.append(el('p', 'liber-card-summary', c.summary || 'Noted for Benjamin'));
  }
  if (row.children.length) card.append(row);
  return card;
}

// ---- a conversation ----
function liberMessages(host, th, onThread) {
  host.replaceChildren();
  for (const t of th.turns || []) {
    const msg = el('div', 'liber-msg is-' + t.who + (t.status ? ' is-' + t.status : ''));
    if (t.who === 'liber' && t.status === 'thinking' && !t.text) {
      const dots = el('div', 'liber-thinking'); dots.setAttribute('aria-label', 'Liber is thinking'); dots.append(el('span'), el('span'), el('span'));
      msg.append(dots);
    } else if (t.text) msg.append(t.who === 'liber' ? liberText(t.text) : el('p', 'liber-mine', t.text));
    for (const c of t.cards || []) msg.append(liberCard(th, c, onThread));
    if (t.queued) msg.append(el('p', 'liber-meta', 'Waiting for Liber…'));
    else if (t.at && t.who === 'olga') msg.append(el('p', 'liber-meta', liberWhen(t.at)));
    host.append(msg);
  }
}

function liberComposer(placeholder, onSend) {
  const form = el('form', 'liber-composer');
  const box = el('textarea', 'liber-input'); box.rows = 1; box.placeholder = placeholder; box.setAttribute('aria-label', placeholder); box.maxLength = 8000; box.enterKeyHint = 'send';
  const send = el('button', 'liber-send', '↑'); send.type = 'submit'; send.setAttribute('aria-label', 'Send'); send.disabled = true;
  const grow = () => { box.style.height = 'auto'; box.style.height = Math.min(box.scrollHeight, 160) + 'px'; send.disabled = !box.value.trim(); };
  box.oninput = grow;
  box.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && matchMedia('(hover:hover)').matches) { e.preventDefault(); form.requestSubmit(); } };
  form.onsubmit = async e => {
    e.preventDefault(); const text = box.value.trim(); if (!text || send.disabled) return;
    send.disabled = true; box.disabled = true;
    try { await onSend(text); box.value = ''; } catch (err) { showToast(err.message); }
    finally { box.disabled = false; grow(); }
  };
  form.append(box, send);
  form.focusInput = () => box.focus();
  form.setText = t => { box.value = t; grow(); box.focus(); };
  return form;
}

function liberStarters(list, onPick) {
  const wrap = el('div', 'liber-starters');
  for (const s of list) { const b = el('button', 'liber-starter', s); b.type = 'button'; b.onclick = () => onPick(s); wrap.append(b); }
  return wrap;
}

// Live updates: Server-Sent Events, with a slow poll as the fallback.
function liberWatch(ref, onThread) {
  let es = null, timer = null, stopped = false;
  const poll = async () => {
    if (stopped) return;
    try { onThread(await liberApi('/api/liber/thread?' + liberQuery(ref))); } catch (e) {}
    timer = setTimeout(poll, document.hidden ? 15000 : 4000);
  };
  try {
    es = new EventSource('/api/liber/events?' + liberQuery(ref));
    es.onmessage = ev => { try { onThread(JSON.parse(ev.data)); } catch (e) {} };
    es.onerror = () => { if (es) { es.close(); es = null; } if (!timer) timer = setTimeout(poll, 3000); };
  } catch (e) { poll(); }
  return () => { stopped = true; if (es) es.close(); clearTimeout(timer); };
}

// ---- the CHAT screen ----
function liberView() {
  let v = document.getElementById('chatView');
  if (!v) { v = el('section', 'liber-view'); v.id = 'chatView'; v.hidden = true; document.getElementById('contentScroll').append(v); }
  return v;
}

async function liberShow(parts) {
  const v = liberView(); v.hidden = false;
  document.body.classList.add('liber-on');
  if (liber.stop) { liber.stop(); liber.stop = null; }
  const which = parts[1] === 'task' ? { task: decodeURIComponent(parts.slice(2).join('/')) } : parts[1] && parts[1] !== 'new' ? { id: parts[1] } : null;
  v.classList.toggle('is-thread', !!which || parts[1] === 'new');
  v.replaceChildren();
  const list = el('div', 'liber-list'), pane = el('div', 'liber-pane');
  v.append(list, pane);
  liberPaintList(list, which);
  if (which || parts[1] === 'new') liberPaintThread(pane, which);
  else liberPaintEmptyPane(pane);
}

function liberHide() {
  const v = document.getElementById('chatView'); if (v) v.hidden = true;
  document.body.classList.remove('liber-on');
  if (liber.stop) { liber.stop(); liber.stop = null; }
}

async function liberPaintList(host, current) {
  const head = el('div', 'liber-list-head');
  head.append(el('h1', 'liber-title', 'Liber'));
  const nw = el('a', 'pill olga-primary liber-new', '＋ New chat'); nw.href = '#/chat/new';
  head.append(nw);
  const note = el('details', 'liber-shared');
  note.append(el('summary', '', 'Home task chats are shared with Benjamin'), el('p', '', 'When you ask Liber about a Home task, that conversation is saved with your shared Home plan so Benjamin can read it. Your other chats are just yours. Liber can suggest changes, but nothing changes until you tap to agree.'));
  host.replaceChildren(head, note);
  const rows = el('div', 'liber-rows'); host.append(rows);
  try { liber.threads = (await liberApi('/api/liber/threads')).threads || []; } catch (e) { rows.append(el('p', 'liber-empty', e.message)); return; }
  if (!liber.threads.length) { rows.append(el('p', 'liber-empty', 'No conversations yet. Start one, or open a task and ask about it.')); return; }
  for (const t of liber.threads) {
    const a = el('a', 'liber-row' + ((current && (t.kind === 'task' ? current.task === t.taskId : current.id === t.id)) ? ' on' : ''));
    a.href = t.kind === 'task' ? '#/chat/task/' + encodeURIComponent(t.taskId) : '#/chat/' + t.id;
    const top = el('div', 'liber-row-top');
    top.append(el('span', 'liber-row-title', (t.kind === 'task' ? 'About: ' : '') + (t.title || 'Conversation')), el('span', 'liber-row-when', liberWhen(t.updated)));
    a.append(top, el('div', 'liber-row-last', t.busy ? 'Liber is answering…' : t.last));
    if (t.pending) a.append(el('span', 'liber-badge', t.pending + (t.pending === 1 ? ' waiting for you' : ' waiting for you')));
    rows.append(a);
  }
}

function liberPaintEmptyPane(pane) {
  pane.replaceChildren(el('div', 'liber-hello'));
  const hello = pane.firstChild;
  hello.append(el('p', 'liber-hello-title', 'Hi Olga — I’m Liber.'), el('p', 'liber-hello-text', 'Ask me anything about your plans, or tell me how you’d like your app to look or work and I’ll show you a preview first.'));
  const composer = liberComposer('Message Liber', async text => { const th = await liberApi('/api/liber/send', { text }); location.hash = '#/chat/' + th.id; });
  pane.append(liberStarters(LIBER_STARTERS_APP, s => composer.setText(s)), composer);
}

function liberPaintThread(pane, which) {
  const head = el('div', 'liber-thread-head');
  const back = el('a', 'liber-back', '‹ Chats'); back.href = '#/chat';
  const title = el('span', 'liber-thread-title', which && which.task ? 'About a task' : which ? 'Conversation' : 'New chat');
  head.append(back, title);
  const msgs = el('div', 'liber-msgs'); msgs.setAttribute('aria-live', 'polite');
  let th = null;
  const onThread = next => {
    if (!next) return;
    const atBottom = liberNearBottom();
    th = next; title.textContent = th.kind === 'task' ? 'About: ' + (th.taskTitle || 'a task') : (th.title || 'Conversation');
    liberMessages(msgs, th, onThread);
    if (atBottom) liberScrollEnd();
  };
  const composer = liberComposer('Message Liber', async text => {
    const ref = th ? liberRef(th) : which || {};
    const next = await liberApi('/api/liber/send', { ...ref, text });
    if (!th && next.kind === 'app') { location.hash = '#/chat/' + next.id; return; }
    onThread(next); liberScrollEnd(true);
  });
  pane.replaceChildren(head, msgs);
  if (!which) { pane.append(liberStarters(LIBER_STARTERS_APP, s => composer.setText(s))); pane.append(composer); composer.focusInput(); return; }
  pane.append(composer);
  liberApi('/api/liber/thread?' + liberQuery(which)).then(t => { onThread(t); liberScrollEnd(true); }).catch(() => { msgs.replaceChildren(el('p', 'liber-empty', 'This conversation isn’t available.')); });
  liber.stop = liberWatch(which, onThread);
}

function liberNearBottom() { const s = document.getElementById('contentScroll'); return !s || s.scrollHeight - s.scrollTop - s.clientHeight < 160; }
function liberScrollEnd(force) { const s = document.getElementById('contentScroll'); if (s && (force || liberNearBottom())) requestAnimationFrame(() => { s.scrollTop = s.scrollHeight; }); }

// ---- the task section ----
function liberTaskSection(taskId, shared) {
  const sec = el('section', 'liber-task');
  sec.append(el('h3', 'liber-task-title', 'Ask Liber about this task'));
  sec.append(el('p', 'liber-task-note', shared ? 'Shared with Benjamin, like the task.' : 'Just you and Liber.'));
  const msgs = el('div', 'liber-msgs');
  let th = null, stop = null;
  const onThread = next => { th = next; liberMessages(msgs, th, onThread); starters.hidden = !!(th.turns || []).length; };
  const starters = liberStarters(LIBER_STARTERS_TASK, s => composer.setText(s));
  const composer = liberComposer('Ask about this task', async text => {
    onThread(await liberApi('/api/liber/send', { task: taskId, text }));
    if (!stop) stop = liberWatch({ task: taskId }, onThread);
  });
  sec.append(msgs, starters, composer);
  liberApi('/api/liber/thread?task=' + encodeURIComponent(taskId)).then(t => {
    onThread(t);
    if ((t.turns || []).length) stop = liberWatch({ task: taskId }, onThread);
  }).catch(() => {});
  sec.liberStop = () => { if (stop) stop(); };
  return sec;
}

// Which tasks have a Liber conversation (a 💬 on their row).
async function liberTaskThreadIds(force) {
  if (!force && liber.taskThreads && Date.now() - liber.listStamp < 30000) return liber.taskThreads;
  try {
    const rows = (await liberApi('/api/liber/threads')).threads || [];
    liber.taskThreads = new Map(rows.filter(t => t.kind === 'task').map(t => [t.taskId, t]));
    liber.listStamp = Date.now();
  } catch (e) { liber.taskThreads = liber.taskThreads || new Map(); }
  return liber.taskThreads;
}
