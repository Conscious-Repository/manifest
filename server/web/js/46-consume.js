// ---- CONSUME: subscribed reading (§5 fifth kind, amended 2026-08-24) ----
//
// Two surfaces over one lane:
//   · a capped strip of unread cards in the FEED's INBOX, so new writing is
//     noticed without being nagged about (the kind contributes 0 to the badge);
//   · a CONSUME view — the whole backlog, the reader, and the manage panel.
//
// ⚠ The reader is the ONLY innerHTML sink in the FEED. Every other card here is
// built from el(tag, cls, text) text nodes, which is why untrusted feed titles
// are safe. The body is allowed through innerHTML solely because it was
// allowlist-sanitized server-side at poll time (consume/sanitize.go) — never
// sanitize on the client, and never widen this to another field.

const CONSUME_CAP = 5; // unread cards shown in the INBOX strip
let consumeCache = { items: [], lists: [], unread: 0 };
let consumeList = ""; // active group filter in the CONSUME view
let consumeView = "unread"; // unread | all
let consumeOpen = {}; // item id → true while its reader is expanded
let consumeSubs = { subscriptions: [], xReady: false };
let consumeManageOpen = false;

// ---- the FEED lane ----

// consumeCardEl renders one card. `compact` is the INBOX strip; the CONSUME
// view uses the same card so the two surfaces never drift apart.
function consumeCardEl(c) {
  const card = el("div", "feed-card consume-card" + (c.read ? " consume-read" : ""));
  card.dataset.consumeId = c.id;

  const top = el("div", "feed-top");
  top.append(el("span", "type-chip micro-label type-consume", c.source || "feed"));
  if (c.type === "x") top.append(el("span", "type-chip micro-label type-x", "X"));
  if (c.list) top.append(el("span", "consume-list-chip micro-label", c.list));
  if (c.curated) top.append(el("span", "consume-curated-chip micro-label", "curated"));
  if (c.published) top.append(el("span", "feed-date", fmtFeedDate(c.published)));
  card.append(top);

  const title = el("div", "feed-title consume-title", c.title || "(untitled)");
  title.onclick = () => consumeToggleReader(c, card);
  card.append(title);

  const meta = el("div", "feed-meta");
  if (c.author) meta.append(el("span", "", c.author));
  if (c.minutes) meta.append(el("span", "", c.minutes + " min read"));
  card.append(meta);

  if (c.excerpt) card.append(el("div", "feed-why consume-excerpt", c.excerpt));

  const body = el("div", "consume-body-wrap");
  body.hidden = !consumeOpen[c.id];
  card.append(body);

  const acts = el("div", "feed-actions");
  const readBtn = pillLight(consumeOpen[c.id] ? "close" : "read →", () => consumeToggleReader(c, card));
  readBtn.classList.add("verdict-primary");
  acts.append(readBtn);
  acts.append(consumeCurateBtn(c, card));
  if (c.url) acts.append(pillLight("original ↗", () => window.open(c.url, "_blank", "noopener")));
  acts.append(pillLight("dismiss", () => consumeDismiss(c.id, card)));
  card.append(acts);

  if (consumeOpen[c.id]) consumeFillReader(c, card);
  return card;
}

// consumeCurateBtn is THE button: it writes a note into extrinsic/ and the
// public feed is a projection of those notes.
function consumeCurateBtn(c, card) {
  if (c.curated) {
    const b = pillLight("curated ✓", async () => {
      await consumePost(`/api/consume/item/${encodeURIComponent(c.id)}/uncurate`);
      c.curated = false;
      consumeRepaint(c, card);
    });
    b.classList.add("consume-curated-on");
    return b;
  }
  return pillLight("→ CURATE", () => consumeCurate(c, card));
}

// consumeCurate asks for the optional one-line note inline — the note is the
// reason someone subscribes to a curation feed, so it is offered at the moment
// of the decision rather than buried in an edit screen.
function consumeCurate(c, card) {
  if (card.querySelector(".consume-note-row")) return;
  const row = el("div", "consume-note-row");
  const input = inputEl("why this one? (optional)");
  input.className = "consume-note-input";
  const save = async () => {
    const note = input.value.trim();
    row.remove();
    const ok = await consumePost(`/api/consume/item/${encodeURIComponent(c.id)}/curate`, { note });
    if (!ok) return;
    c.curated = true;
    showToast("curated → public feed");
    consumeRepaint(c, card);
  };
  input.onkeydown = (e) => {
    if (e.key === "Enter") { e.preventDefault(); save(); }
    if (e.key === "Escape") row.remove();
  };
  row.append(input);
  row.append(pillLight("curate", save));
  row.append(pillLight("cancel", () => row.remove()));
  card.append(row);
  input.focus();
}

// consumeRepaint swaps one card in place. A full re-render would fight the 3s
// FEED poll and close an open reader mid-sentence.
function consumeRepaint(c, card) {
  const fresh = consumeCardEl(c);
  card.replaceWith(fresh);
}

async function consumeDismiss(id, card) {
  if (card) card.remove(); // optimistic
  await consumePost(`/api/consume/item/${encodeURIComponent(id)}/dismiss`);
  delete consumeOpen[id];
  if (state.tab === "consume" || consumeIsActiveView()) renderConsume();
}

// ---- the reader ----

function consumeToggleReader(c, card) {
  consumeOpen[c.id] = !consumeOpen[c.id];
  if (!consumeOpen[c.id]) { consumeRepaint(c, card); return; }
  c.read = true; // opening IS reading; the server agrees on the same request
  consumeRepaint(c, card);
}

async function consumeFillReader(c, card) {
  const wrap = card.querySelector(".consume-body-wrap");
  if (!wrap || wrap.dataset.filled) return;
  wrap.hidden = false;
  wrap.dataset.filled = "1";
  wrap.append(el("div", "consume-loading micro-label", "loading…"));
  let d;
  try {
    d = await (await fetch(`/api/consume/item/${encodeURIComponent(c.id)}`)).json();
  } catch (e) {
    wrap.innerHTML = "";
    wrap.append(el("div", "consume-loading micro-label", "could not load this one"));
    return;
  }
  wrap.innerHTML = "";

  const head = el("div", "consume-read-head micro-label");
  head.append(el("span", "", [d.source, d.author].filter(Boolean).join(" · ")));
  wrap.append(head);

  // THE innerHTML sink. Server-sanitized at poll time — see the file header.
  const body = el("div", "consume-body");
  body.innerHTML = d.body || "";
  if (!d.body) body.append(el("div", "consume-loading micro-label", "no body — open the original"));
  wrap.append(body);

  // The decision happens where the reading ends, so CURATE is repeated here.
  const foot = el("div", "consume-read-foot");
  c.curated = !!d.curated;
  foot.append(consumeCurateBtn(c, card));
  if (d.url) foot.append(pillLight("original ↗", () => window.open(d.url, "_blank", "noopener")));
  wrap.append(foot);
}

// ---- the CONSUME view ----

function consumeIsActiveView() { return (state.feedView || "inbox") === "consume"; }

async function loadConsume() {
  const q = new URLSearchParams({ view: consumeView });
  if (consumeList) q.set("list", consumeList);
  try {
    consumeCache = await (await fetch("/api/consume?" + q)).json();
  } catch (e) { consumeCache = { items: [], lists: [], unread: 0 }; }
  renderConsume();
}

function renderConsume() {
  const host = els.feedList; host.innerHTML = "";
  els.feedSignals.innerHTML = "";

  host.append(consumeHeader());
  if (consumeManageOpen) host.append(consumeManagePanel());

  const items = consumeCache.items || [];
  if (!items.length) {
    host.append(emptyRow(consumeView === "unread"
      ? "Nothing unread — everything you follow is read."
      : "Nothing here yet. Add a feed with MANAGE."));
    return;
  }
  items.forEach((c) => host.append(consumeCardEl(c)));
}

function consumeHeader() {
  const head = el("div", "consume-head");

  const left = el("div", "consume-head-left");
  [["unread", "UNREAD"], ["all", "ALL"]].forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (consumeView === val ? " on" : ""), label);
    b.onclick = () => { consumeView = val; loadConsume(); };
    left.append(b);
  });
  (consumeCache.lists || []).forEach((l) => {
    const b = el("button", "filter-chip" + (consumeList === l ? " on" : ""), l);
    b.onclick = () => { consumeList = consumeList === l ? "" : l; loadConsume(); };
    left.append(b);
  });
  head.append(left);

  const right = el("div", "consume-head-right");
  right.append(el("span", "micro-label consume-count", (consumeCache.unread || 0) + " unread"));
  const manage = pillLight(consumeManageOpen ? "close" : "MANAGE", async () => {
    consumeManageOpen = !consumeManageOpen;
    if (consumeManageOpen) await loadConsumeSubs();
    renderConsume();
  });
  right.append(manage);
  head.append(right);
  return head;
}

// ---- the manage panel ----

async function loadConsumeSubs() {
  try {
    consumeSubs = await (await fetch("/api/consume/subscriptions")).json();
  } catch (e) { consumeSubs = { subscriptions: [], xReady: false }; }
}

function consumeManagePanel() {
  const panel = el("div", "consume-manage");
  panel.append(el("div", "consume-manage-head micro-label", "following"));
  panel.append(consumeAddRow());

  const subs = consumeSubs.subscriptions || [];
  const groups = {};
  subs.forEach((s) => { (groups[s.list || "unfiled"] ||= []).push(s); });
  Object.keys(groups).sort().forEach((g) => {
    panel.append(el("div", "consume-group micro-label", g));
    groups[g].forEach((s) => panel.append(consumeSubRow(s)));
  });
  if (!subs.length) panel.append(emptyRow("Nothing followed yet."));
  return panel;
}

function consumeAddRow() {
  const row = el("div", "consume-add");
  const input = inputEl("feed URL, site address, or @handle");
  input.className = "consume-add-input";
  const list = inputEl("group (optional)");
  list.className = "consume-add-list";

  const add = async () => {
    const value = input.value.trim();
    if (!value) return;
    input.disabled = list.disabled = true;
    const ok = await consumePost("/api/consume/subscriptions",
      { input: value, list: list.value.trim(), mirror: "full" });
    input.disabled = list.disabled = false;
    if (!ok) return;
    input.value = ""; list.value = "";
    showToast("subscribed");
    await loadConsumeSubs();
    await loadConsume();
  };
  input.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } };
  row.append(input, list, pillLight("+ follow", add));

  if (!consumeSubs.xReady) {
    row.append(el("div", "consume-hint micro-label",
      "to follow an X account, add a bearer token in SPIRITS → Settings → Portals"));
  }
  return row;
}

function consumeSubRow(s) {
  const row = el("div", "consume-sub");

  const dot = el("span", "consume-dot " + (s.lastErr ? "bad" : s.lastOk ? "ok" : "idle"));
  dot.title = s.lastErr || (s.lastOk ? "last polled " + fmtFeedDate(s.lastOk) : "not polled yet");
  row.append(dot);

  const name = el("span", "consume-sub-name", s.title || s.id);
  row.append(name);
  row.append(el("span", "consume-sub-kind micro-label", s.kind === "x" ? "X" : "rss"));
  row.append(el("span", "consume-sub-count micro-label", s.unread + "/" + s.total));
  if (s.mirror === "excerpt") row.append(el("span", "consume-sub-kind micro-label", "excerpt"));

  const acts = el("div", "consume-sub-acts");
  acts.append(pillLight("poll", async () => {
    await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/poll`);
    await loadConsumeSubs(); await loadConsume();
  }));
  acts.append(pillLight("edit", () => consumeEditSub(s, row)));
  acts.append(pillLight("unfollow", async () => {
    await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/remove`);
    await loadConsumeSubs(); await loadConsume();
  }));
  row.append(acts);

  if (s.lastErr) row.append(el("div", "consume-sub-err micro-label", s.lastErr));
  return row;
}

function consumeEditSub(s, row) {
  if (row.querySelector(".consume-edit")) return;
  const box = el("div", "consume-edit");
  const title = inputEl("name"); title.value = s.title || "";
  const list = inputEl("group"); list.value = s.list || "";
  const mirror = el("select", "consume-mirror");
  [["full", "mirror full text"], ["excerpt", "excerpt + link only"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v;
    if ((s.mirror || "full") === v) o.selected = true;
    mirror.append(o);
  });
  const save = async () => {
    await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/update`,
      { title: title.value.trim(), list: list.value.trim(), mirror: mirror.value, minChars: s.minChars || 0 });
    await loadConsumeSubs(); await loadConsume();
  };
  box.append(title, list, mirror, pillLight("save", save), pillLight("cancel", () => box.remove()));
  row.append(box);
  title.focus();
}

// consumePost is the shared write: it surfaces a server refusal as a toast
// instead of letting a failed fetch look like success (the FEED-confirm class
// of bug — fetch does not reject on 4xx).
async function consumePost(url, body) {
  try {
    const r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) {
      showToast((await r.text()).trim().slice(0, 160) || "that didn't work");
      return false;
    }
    return true;
  } catch (e) {
    showToast("network error");
    return false;
  }
}
