// Liber — Olga's assistant: the CHAT screen and the "Ask Liber" section in a
// task. Plan: system/workbench/plans/2026-10-07-olga-chat.md. The server owns
// every turn; this page only shows threads and sends her words and taps.
const liber = { threads: [], open: null, stop: null, listStamp: 0, taskThreads: null };
const LIBER_STARTERS_APP = ['Make the text bigger', 'Change the colours', 'How does the timeline work?'];
const LIBER_STARTERS_TASK = ['What should we decide first?', 'What could this cost?', 'What’s the next step?'];

// Her app restarts for a few seconds after Liber ships a change; meanwhile
// Cloudflare answers with its own HTML error page. Reads wait that out, and a
// page of HTML is never shown as a message.
async function liberApi(path, body) {
  for (let tries = 0; ; tries++) {
    let r = null;
    try { r = await fetch(path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {}); } catch (e) {}
    if (r && r.ok) return r.json();
    const restarting = !r || r.status === 502 || r.status === 503 || r.status === 504;
    if (restarting && !body && tries < 6) { await new Promise(go => setTimeout(go, 1500 + tries * 1000)); continue; }
    if (restarting) throw new Error('Liber is restarting — try again in a moment.');
    throw new Error(liberErrorText(await r.text()));
  }
}
function liberErrorText(t) {
  t = (t || '').trim();
  return !t || t.startsWith('<') ? 'Something went wrong — try again.' : t;
}

function liberRef(th) { return th.kind === 'task' ? { task: th.taskId } : { id: th.id }; }
function liberQuery(ref) { return ref.task ? 'task=' + encodeURIComponent(ref.task) : 'id=' + encodeURIComponent(ref.id); }

// Plain text with paragraphs, simple bullets, tables, links and **bold** —
// never HTML. A block may mix a lead-in line with its bullets ("First:\n- this").
function liberText(text) {
  const box = el('div', 'liber-text');
  const bullet = /^\s*([-*•]|\d+[.)])\s+/, row = /^\s*\|.*\|\s*$/, rule = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;
  const cells = l => l.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(c => c.trim());
  for (const block of String(text || '').split(/\n{2,}/)) {
    let list = null, para = [];
    const flush = () => { if (para.length) box.append(liberInline(el('p'), para.join('\n'))); para = []; };
    const lines = block.split('\n');
    for (let i = 0; i < lines.length; i++) {
      const l = lines[i];
      if (row.test(l) && rule.test(lines[i + 1] || '')) {
        flush(); list = null;
        const wrap = el('div', 'liber-table'), table = el('table'), head = el('tr');
        cells(l).forEach(c => head.append(liberInline(el('th'), c)));
        const thead = el('thead'), tbody = el('tbody'); thead.append(head);
        for (i += 2; i < lines.length && row.test(lines[i]); i++) {
          const tr = el('tr'); cells(lines[i]).forEach(c => tr.append(liberInline(el('td'), c))); tbody.append(tr);
        }
        i--;
        table.append(thead, tbody); wrap.append(table); box.append(wrap);
      } else if (bullet.test(l)) {
        flush();
        if (!list) { list = el(/^\s*\d/.test(l) ? 'ol' : 'ul'); box.append(list); }
        list.append(liberInline(el('li'), l.replace(bullet, '')));
      } else if (l.trim()) { list = null; para.push(l); }
    }
    flush();
  }
  return box;
}
// Bold, links (plain or [label](url), opened in a new tab) and plain text.
function liberInline(node, s) {
  s.split(/(\*\*[^*]+\*\*|\[[^\]]+\]\(https?:\/\/[^\s)]+\)|https?:\/\/[^\s<>"]+[^\s<>".,;:!?)\]])/).forEach(part => {
    if (!part) return;
    let m;
    if (/^\*\*[^*]+\*\*$/.test(part)) node.append(liberInline(el('strong'), part.slice(2, -2)));
    else if ((m = /^\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)$/.exec(part)) || /^https?:\/\//.test(part)) {
      const url = m ? m[2] : part, a = el('a', 'liber-link', m ? m[1] : liberShortURL(url));
      a.href = url; a.target = '_blank'; a.rel = 'noopener noreferrer'; node.append(a);
    } else node.append(document.createTextNode(part.replace(/`([^`]+)`/g, '$1')));
  });
  return node;
}
// A long store URL reads as its site and a bit of the path.
function liberShortURL(u) {
  try { const x = new URL(u); const path = x.pathname.length > 24 ? x.pathname.slice(0, 22) + '…' : x.pathname; return x.hostname.replace(/^www\./, '') + (path === '/' ? '' : path); }
  catch (e) { return u; }
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
    } else {
      if ((t.images || []).length) {
        const grid = el('div', 'liber-photos');
        for (const id of t.images) { const b = el('button', 'liber-photo'); b.type = 'button'; b.setAttribute('aria-label', 'Open photo'); const img = el('img'); img.src = liberPhotoURL(id, liberRef(th)); img.alt = 'Photo'; img.loading = 'lazy'; b.append(img); b.onclick = () => liberViewPhoto(img.src); grid.append(b); }
        msg.append(grid);
      }
      if (t.text) msg.append(t.who === 'liber' ? liberText(t.text) : el('p', 'liber-mine', t.text));
    }
    for (const c of t.cards || []) msg.append(liberCard(th, c, onThread));
    if (t.queued) msg.append(el('p', 'liber-meta', 'Waiting for Liber…'));
    else if (t.at && t.who === 'olga') msg.append(el('p', 'liber-meta', liberWhen(t.at)));
    host.append(msg);
  }
}

// Photos: chosen, pasted or dropped; shrunk on the phone before upload (a
// 12-megapixel photo becomes ~300 KB), uploaded at once, sent with the words.
const LIBER_MAX_PHOTOS = 6;
async function liberShrink(file) {
  if (file.type === 'image/gif' && file.size < 5e6) return file;
  try {
    const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
    const scale = Math.min(1, 1600 / Math.max(bmp.width, bmp.height));
    const c = document.createElement('canvas'); c.width = Math.round(bmp.width * scale); c.height = Math.round(bmp.height * scale);
    c.getContext('2d').drawImage(bmp, 0, 0, c.width, c.height);
    return await new Promise(res => c.toBlob(b => res(b || file), 'image/jpeg', 0.85));
  } catch (e) { return file; }
}
function liberPhotoURL(id, ref) { return '/api/liber/file?id=' + encodeURIComponent(id) + (ref && ref.task ? '&task=' + encodeURIComponent(ref.task) : ''); }
function liberViewPhoto(src) {
  const o = el('div', 'liber-photo-view'); o.setAttribute('role', 'dialog'); o.setAttribute('aria-label', 'Photo');
  const img = el('img'); img.src = src; img.alt = 'Photo';
  const x = el('button', 'liber-photo-close', '✕'); x.type = 'button'; x.setAttribute('aria-label', 'Close photo');
  const close = () => { o.remove(); document.removeEventListener('keydown', key); };
  const key = e => { if (e.key === 'Escape') close(); };
  x.onclick = close; o.onclick = e => { if (e.target === o) close(); };
  document.addEventListener('keydown', key);
  o.append(img, x); document.body.append(o); x.focus();
}

function liberComposer(placeholder, onSend, scope) {
  const form = el('form', 'liber-composer');
  const tray = el('div', 'liber-tray'); tray.hidden = true;
  const row = el('div', 'liber-row-input');
  const pick = el('input'); pick.type = 'file'; pick.accept = 'image/*'; pick.multiple = true; pick.hidden = true;
  const add = el('button', 'liber-attach'); add.type = 'button'; add.setAttribute('aria-label', 'Add photos'); add.title = 'Add photos';
  add.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M4 7h3l2-2.5h6L17 7h3a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1z"/><circle cx="12" cy="13" r="3.6" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>';
  const box = el('textarea', 'liber-input'); box.rows = 1; box.placeholder = placeholder; box.setAttribute('aria-label', placeholder); box.maxLength = 8000; box.enterKeyHint = 'send';
  const send = el('button', 'liber-send', '↑'); send.type = 'submit'; send.setAttribute('aria-label', 'Send'); send.disabled = true;
  const photos = []; // {id, url, state: 'up'|'ok'|'bad', node}
  const ready = () => { const busy = photos.some(p => p.state === 'up'); send.disabled = busy || (!box.value.trim() && !photos.some(p => p.state === 'ok')); tray.hidden = !photos.length; };
  const grow = () => { box.style.height = 'auto'; box.style.height = Math.min(box.scrollHeight, 160) + 'px'; ready(); };
  const addFiles = async files => {
    for (const f of [...files].filter(f => f.type.startsWith('image/'))) {
      if (photos.length >= LIBER_MAX_PHOTOS) { showToast('Up to ' + LIBER_MAX_PHOTOS + ' photos at a time'); break; }
      const p = { state: 'up', url: URL.createObjectURL(f) };
      const chip = el('div', 'liber-chip is-up'); const img = el('img'); img.src = p.url; img.alt = 'Photo to send';
      const x = el('button', 'liber-chip-x', '✕'); x.type = 'button'; x.setAttribute('aria-label', 'Remove this photo');
      x.onclick = () => { photos.splice(photos.indexOf(p), 1); chip.remove(); URL.revokeObjectURL(p.url); ready(); };
      chip.append(img, x); tray.append(chip); p.node = chip; photos.push(p); ready();
      try {
        const blob = await liberShrink(f);
        const r = await fetch('/api/liber/upload' + (scope && scope.task ? '?task=' + encodeURIComponent(scope.task) : ''), { method: 'POST', headers: { 'Content-Type': blob.type || 'application/octet-stream' }, body: blob });
        if (!r.ok) throw new Error(r.status >= 502 ? 'Liber is restarting — try again in a moment.' : liberErrorText(await r.text()).replace('Something went wrong — try again.', 'That photo didn’t upload'));
        p.id = (await r.json()).id; p.state = 'ok'; chip.classList.remove('is-up');
      } catch (e) { p.state = 'bad'; chip.classList.remove('is-up'); chip.classList.add('is-bad'); chip.title = e.message; showToast(e.message); }
      ready();
    }
  };
  add.onclick = () => pick.click();
  pick.onchange = () => { addFiles(pick.files); pick.value = ''; };
  box.addEventListener('paste', e => { const files = [...(e.clipboardData?.files || [])]; if (files.some(f => f.type.startsWith('image/'))) { e.preventDefault(); addFiles(files); } });
  form.addEventListener('dragover', e => { if ([...(e.dataTransfer?.items || [])].some(i => i.kind === 'file')) { e.preventDefault(); form.classList.add('is-drop'); } });
  form.addEventListener('dragleave', () => form.classList.remove('is-drop'));
  form.addEventListener('drop', e => { form.classList.remove('is-drop'); if (e.dataTransfer?.files?.length) { e.preventDefault(); addFiles(e.dataTransfer.files); } });
  box.oninput = grow;
  box.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && matchMedia('(hover:hover)').matches) { e.preventDefault(); form.requestSubmit(); } };
  form.onsubmit = async e => {
    e.preventDefault(); const text = box.value.trim(); const ids = photos.filter(p => p.state === 'ok').map(p => p.id);
    if (send.disabled || (!text && !ids.length)) return;
    send.disabled = true; box.disabled = true; add.disabled = true;
    try {
      await onSend(text, ids);
      box.value = ''; photos.splice(0).forEach(p => { p.node.remove(); URL.revokeObjectURL(p.url); });
    } catch (err) { showToast(err.message); }
    finally { box.disabled = false; add.disabled = false; grow(); }
  };
  row.append(add, box, send);
  form.append(tray, row, pick);
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
  const composer = liberComposer('Message Liber', async (text, images) => { const th = await liberApi('/api/liber/send', { text, images }); location.hash = '#/chat/' + th.id; }, {});
  pane.append(liberStarters(LIBER_STARTERS_APP, s => composer.setText(s)), composer);
}

function liberPaintThread(pane, which) {
  const head = el('div', 'liber-thread-head');
  const back = el('a', 'liber-back', '‹ Chats'); back.href = '#/chat';
  // A task's chat names its task as a link straight back to the task card.
  const title = el('span', 'liber-thread-title', which && which.task ? 'About a task' : which ? 'Conversation' : 'New chat');
  if (which && which.task) {
    const link = el('a', 'liber-task-link'); link.href = '#/tasks/' + encodeURIComponent(which.task);
    link.append(title, el('span', 'liber-task-open', 'Open task ›'));
    head.append(back, link);
  } else head.append(back, title);
  const msgs = el('div', 'liber-msgs'); msgs.setAttribute('aria-live', 'polite');
  let th = null;
  const onThread = next => {
    if (!next) return;
    const atBottom = liberNearBottom();
    th = next; title.textContent = th.kind === 'task' ? 'About: ' + (th.taskTitle || 'a task') : (th.title || 'Conversation');
    if (th.kind === 'task') head.querySelector('.liber-task-link')?.setAttribute('aria-label', 'Open the task “' + (th.taskTitle || 'this task') + '”');
    liberMessages(msgs, th, onThread);
    if (atBottom) liberScrollEnd();
  };
  const composer = liberComposer('Message Liber', async (text, images) => {
    const ref = th ? liberRef(th) : which || {};
    const next = await liberApi('/api/liber/send', { ...ref, text, images });
    if (!th && next.kind === 'app') { location.hash = '#/chat/' + next.id; return; }
    onThread(next); liberScrollEnd(true);
  }, which || {});
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
  sec.append(el('p', 'liber-task-note', shared ? 'Shared with Benjamin, like the task. You can add photos.' : 'Just you and Liber. You can add photos.'));
  // Only the latest exchange shows here; earlier turns open on a tap, and the
  // full conversation has its own screen.
  const msgs = el('div', 'liber-msgs');
  const earlier = el('button', 'olga-more liber-earlier'); earlier.type = 'button'; earlier.hidden = true;
  const full = el('a', 'olga-more liber-full', 'Open the full conversation'); full.href = '#/chat/task/' + encodeURIComponent(taskId); full.hidden = true;
  full.onclick = e => { if (typeof olgaPanelDirty === 'function' && olgaPanelDirty()) { e.preventDefault(); showToast('Save or cancel your changes first.'); return; } closePicker(); };
  let th = null, stop = null, showAll = false;
  const onThread = next => {
    th = next; const turns = th.turns || [];
    let from = 0; for (let i = turns.length - 1; i >= 0; i--) if (turns[i].who === 'olga') { from = i; break; }
    const hiddenN = showAll ? 0 : from;
    liberMessages(msgs, hiddenN ? { ...th, turns: turns.slice(from) } : th, onThread);
    earlier.hidden = !hiddenN; earlier.textContent = 'Show ' + hiddenN + ' earlier message' + (hiddenN === 1 ? '' : 's');
    full.hidden = !turns.length; starters.hidden = !!turns.length;
  };
  earlier.onclick = () => { showAll = true; if (th) onThread(th); };
  const starters = liberStarters(LIBER_STARTERS_TASK, s => composer.setText(s));
  const composer = liberComposer('Ask about this task', async (text, images) => {
    onThread(await liberApi('/api/liber/send', { task: taskId, text, images }));
    if (!stop) stop = liberWatch({ task: taskId }, onThread);
  }, { task: taskId });
  sec.append(earlier, msgs, starters, composer, full);
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

// olga.js routes once before this file loads; a page opened on #/chat routes again now.
if (/^#\/chat/.test(location.hash) && typeof olgaRoute === 'function') olgaRoute();
