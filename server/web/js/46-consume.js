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
let consumeSubs = { subscriptions: [], xReady: false };
let consumeManageOpen = false;
let consumeSub = "";      // one subscription's archive ("" = all feeds)
let consumeQuery = "";    // search text
let consumeShowAll = false; // the ▾ N more expander
let consumeSearchEl = null; // ⚠ built ONCE — see renderConsume

const CONSUME_PAGE = 50; // rows before the expander

// consumeQueued — is this card still waiting to be read? Read and archived are
// different states and BOTH sit outside the queue.
const consumeQueued = (c) => !c.read && !c.seeded;

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
  // "archived" is not "read" — it arrived before you followed this feed.
  if (c.seeded) top.append(el("span", "consume-list-chip micro-label", "archived"));
  // The publisher withholds the rest of this one — say so rather than letting
  // a stub trail off into a bare "Read more".
  if (c.preview) {
    top.append(el("span", "read-preview-chip micro-label",
      c.preview === "paid" ? "paid post" : "preview only"));
  }
  if (c.published) top.append(el("span", "feed-date", fmtFeedDate(c.published)));
  card.append(top);

  const title = el("div", "feed-title consume-title", c.title || "(untitled)");
  title.onclick = () => openRead(c.id);
  card.append(title);

  const meta = el("div", "feed-meta");
  if (c.author) meta.append(el("span", "", c.author));
  if (c.minutes) meta.append(el("span", "", c.minutes + " min read"));
  card.append(meta);

  if (c.excerpt) card.append(el("div", "feed-why consume-excerpt", c.excerpt));

  const acts = el("div", "feed-actions");
  const readBtn = pillLight("read →", () => openRead(c.id));
  readBtn.classList.add("verdict-primary");
  acts.append(readBtn);
  acts.append(consumeCurateBtn(c, card));
  // Anything out of the queue can be pulled back into it.
  if (c.read || c.seeded) {
    acts.append(pillLight("→ unread", async () => {
      if (!(await consumePost(`/api/consume/item/${encodeURIComponent(c.id)}/unread`))) return;
      showToast("moved to unread");
      await loadConsume();
    }));
  }
  if (c.url) acts.append(pillLight("original ↗", () => window.open(c.url, "_blank", "noopener")));
  acts.append(pillLight("dismiss", () => consumeDismiss(c.id, card)));
  card.append(acts);
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

// consumeDismiss — gone from every view, with a brief undo.
//
// ⚠ The bug this replaces: it removed the card, POSTed, then called
// renderConsume(), which repaints from consumeCache — a cache that still held
// the item. The card came straight back, which read as "dismiss does nothing
// but flicker". Anything that removes an item must remove it from the CACHE
// too, or the next repaint undoes the work.
async function consumeDismiss(id, card) {
  if (card) card.remove(); // optimistic
  consumeForget(id);
  const ok = await consumePost(`/api/consume/item/${encodeURIComponent(id)}/dismiss`);
  if (!ok) { await loadConsume(); return; } // the write failed — show the truth
  // showToast makes the WHOLE toast the click target, so the label has to say
  // what clicking it does.
  showToast("dismissed · undo", () => consumeUndismiss(id));
  if (consumeIsActiveView()) renderConsume();
}

// consumeForget drops an item from the in-memory lane so a repaint cannot
// resurrect it.
function consumeForget(id) {
  consumeCache.items = (consumeCache.items || []).filter((x) => x.id !== id);
  consumeCache.unread = (consumeCache.items || []).filter(consumeQueued).length;
  feedCache.consumeItems = (feedCache.consumeItems || []).filter((x) => x.id !== id);
}

async function consumeUndismiss(id) {
  if (!(await consumePost(`/api/consume/item/${encodeURIComponent(id)}/undismiss`))) return;
  await loadConsume();
  showToast("restored");
}

// Reading happens on its own page (47-read.js), not inline. The inline
// expansion lived here until 2026-08-25; it was cramped, and because the
// article pushed the card's action row far above the fold it forced a SECOND
// copy of CURATE and `original ↗` at the foot of the text. Both duplicates went
// with it.

// ---- the CONSUME view ----

function consumeIsActiveView() { return feedFilter() === "consume"; }

async function loadConsume() {
  const q = new URLSearchParams({ view: consumeView });
  if (consumeList) q.set("list", consumeList);
  if (consumeSub) q.set("sub", consumeSub);
  if (consumeQuery.trim()) q.set("q", consumeQuery.trim());
  try {
    consumeCache = await (await fetch("/api/consume?" + q)).json();
  } catch (e) { consumeCache = { items: [], lists: [], unread: 0, total: 0 }; }
  renderConsume();
}

// consumeFilterChanged resets the expander and reloads. Every filter goes
// through here so "show more" can never survive into a different list.
function consumeFilterChanged() {
  consumeShowAll = false;
  loadConsume();
}

// consumeSearch is debounced so a query costs one request per pause.
const consumeSearch = debounce(() => consumeFilterChanged(), 200);

function renderConsume() {
  const host = els.feedList; host.innerHTML = "";
  els.feedSignals.innerHTML = "";

  host.append(consumeHeader());
  if (consumeManageOpen) host.append(consumeManagePanel());
  if (consumeSub) host.append(consumeSubBanner());

  const items = consumeCache.items || [];
  if (!items.length) {
    // ⚠ "nothing matches" and "nothing exists" are different messages, and
    // with a search box the difference IS the message.
    const filtered = consumeQuery.trim() || consumeSub || consumeList;
    host.append(emptyRow(
      filtered ? "Nothing matches — clear the filters to see everything."
        : consumeView === "unread" ? "Nothing unread. Older posts live under ALL."
          : "Nothing here yet. Add a feed with MANAGE."));
    return;
  }
  const shown = consumeShowAll ? items.length : Math.min(CONSUME_PAGE, items.length);
  items.slice(0, shown).forEach((c) => host.append(consumeCardEl(c)));
  if (items.length > shown) {
    const more = el("button", "signal-more", `▾ ${items.length - shown} more`);
    more.onclick = () => { consumeShowAll = true; renderConsume(); };
    host.append(more);
  }
}

// consumeSubBanner names the feed whose archive is open, with the way out.
function consumeSubBanner() {
  const sub = (consumeSubs.subscriptions || []).find((x) => x.id === consumeSub);
  const bar = el("div", "consume-subbar");
  bar.append(el("span", "micro-label", (sub ? sub.title : consumeSub) + " · everything we have"));
  bar.append(pillLight("× all feeds", () => { consumeSub = ""; consumeFilterChanged(); }));
  return bar;
}

function consumeHeader() {
  const head = el("div", "consume-head");

  const left = el("div", "consume-head-left");
  [["unread", "UNREAD"], ["all", "ALL"]].forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (consumeView === val ? " on" : ""), label);
    b.onclick = () => { consumeView = val; consumeFilterChanged(); };
    left.append(b);
  });
  (consumeCache.lists || []).forEach((l) => {
    const b = el("button", "filter-chip" + (consumeList === l ? " on" : ""), l);
    b.onclick = () => { consumeList = consumeList === l ? "" : l; consumeFilterChanged(); };
    left.append(b);
  });
  head.append(left);

  // ⚠ THE CARET TRAP. renderConsume wipes els.feedList on every repaint, so a
  // freshly built input would be replaced mid-typing and the caret would jump
  // out after the first character — the bug the contractors tab and the
  // properties board both had to fix. The node is built ONCE and re-appended.
  if (!consumeSearchEl) {
    consumeSearchEl = el("input", "pp-in consume-search");
    consumeSearchEl.type = "search";
    consumeSearchEl.placeholder = "search titles and excerpts…";
    consumeSearchEl.oninput = () => { consumeQuery = consumeSearchEl.value; consumeSearch(); };
  }
  left.append(consumeSearchEl);

  const right = el("div", "consume-head-right");
  // Scoped to the active group, matching the "mark all read" beside it.
  const unread = consumeCache.unread || 0;
  const label = consumeList && consumeCache.total > unread
    ? unread + " unread in " + consumeList
    : unread + " unread";
  right.append(el("span", "micro-label consume-count", label));

  right.append(pillLight("refresh", async (e) => {
    const btn = e && e.currentTarget;
    if (btn) { btn.disabled = true; btn.textContent = "refreshing…"; }
    await consumePost("/api/consume/poll-all");
    await loadConsumeSubs();
    await loadConsume();
  }));

  // The escape hatch after a week away. Scoped to the active group when one is
  // filtered, so "mark all read" never quietly clears more than you can see.
  if (unread > 0) {
    right.append(pillLight("mark all read", async () => {
      const q = consumeList ? "?list=" + encodeURIComponent(consumeList) : "";
      if (!(await consumePost("/api/consume/read-all" + q))) return;
      await loadConsume();
    }));
  }

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
    const res = await consumePostJSON("/api/consume/subscriptions",
      { input: value, list: list.value.trim(), mirror: "full" });
    input.disabled = list.disabled = false;
    if (!res) return;
    input.value = ""; list.value = "";
    // ⚠ A new subscription is deliberately EMPTY of unread — everything the
    // feed already published is archived. Without saying so, zero unread reads
    // exactly like the bug this rule was written to fix.
    const name = (res.subscription && res.subscription.title) || "the feed";
    showToast(res.archived
      ? `following ${name} — ${res.archived} earlier posts archived; new ones arrive as they publish`
      : `following ${name}`);
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

  // Clicking the name opens this feed's whole history.
  const name = el("button", "consume-sub-name", s.title || s.id);
  name.onclick = () => {
    consumeSub = consumeSub === s.id ? "" : s.id;
    consumeView = "all"; // an archive you cannot see is not an archive
    consumeFilterChanged();
  };
  row.append(name);
  row.append(el("span", "consume-sub-kind micro-label", s.kind === "x" ? "X" : "rss"));
  // ⚠ "0/14" read as a failure. Say what the numbers mean.
  const counts = s.unread + " unread" + (s.archived ? " · " + s.archived + " archived" : "");
  row.append(el("span", "consume-sub-count micro-label", counts));
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
  // Full text: what to do when this publisher only ships a teaser.
  const ft = el("select", "consume-mirror");
  [["auto", "full text: when truncated"], ["on", "full text: always fetch"], ["off", "full text: never fetch"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v;
    if ((s.fulltext || "auto") === v) o.selected = true;
    ft.append(o);
  });
  const save = async () => {
    await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/update`,
      { title: title.value.trim(), list: list.value.trim(), mirror: mirror.value, fulltext: ft.value, minChars: s.minChars || 0 });
    await loadConsumeSubs(); await loadConsume();
  };
  box.append(title, list, mirror, ft, pillLight("save", save), pillLight("cancel", () => box.remove()));
  row.append(box);
  title.focus();
}

// consumePost is the shared write: it surfaces a server refusal as a toast
// instead of letting a failed fetch look like success (the FEED-confirm class
// of bug — fetch does not reject on 4xx).
// consumePostJSON is consumePost for the calls whose RESPONSE matters.
async function consumePostJSON(url, body) {
  try {
    const r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) {
      showToast((await r.text()).trim().slice(0, 160) || "that didn't work");
      return null;
    }
    return await r.json();
  } catch (e) {
    showToast("network error");
    return null;
  }
}

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
