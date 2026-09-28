// ---- CONSUME: the reader (§5 fifth kind, amended 2026-08-24; reader pass
// 2026-09-27) ----
//
// FEED is one reader with the Inbox inside it (owner decision 2026-09-27):
//   · a sidebar (#feedSide) of smart views (Inbox, Approvals, Unread, Today,
//     Later, All), media types (Articles, Videos, Podcasts, Posts) and the
//     owner's STREAMS with their sources;
//   · a compact list of rows for the chosen view;
//   · a reading pane beside the list on a wide screen, the #/read/<id> page
//     on a narrow one (47-read.js renders both).
//
// Every view has its own address (#/feed/unread, #/feed/later,
// #/feed/type/video, #/feed/stream/<name>, #/feed/source/<id>), so the back
// button, a tile and a bookmark all land where they should.
//
// ⚠ The reader body is the ONLY innerHTML sink in the FEED (47-read.js,
// readBodyEl). Every row here is built from el(tag, cls, text) text nodes,
// which is why untrusted feed titles are safe. Never widen the sink.

const CONSUME_CAP = 5; // unread cards the inbox lane may show (kept for the registry)
let consumeCache = { items: [], lists: [], unread: 0 };
let consumeList = ""; // the active STREAM ("" = every stream)
let consumeView = "unread"; // unread | all | today | later | later-done
let consumeType = ""; // "" | article | video | podcast | post
let consumeSubs = { subscriptions: [], xReady: false };
let consumeManageOpen = false;
let consumeCuratedOpen = false;
let consumeCurated = { entries: [], public: "" }; // the private mirror of the public feed
let consumeSub = "";      // one source's history ("" = all sources)
let consumeQuery = "";    // search text
let consumeShowAll = false; // kept for the fixtures' contract; paging replaced the expander
let consumeSearchEl = null; // ⚠ built ONCE — see renderConsume
let consumeSel = "";      // the selected row's item id
let consumeNav = null;    // sidebar counts (Service.Nav)
let consumeMoreBusy = false;

const CONSUME_PAGE = 60; // rows per page; more load as the list scrolls

// consumeMemo remembers each view's last answer, so switching views paints at
// once from memory and the network only corrects it (stale-while-revalidate).
const consumeMemo = new Map();
// consumeBodies holds article payloads fetched ahead (peek) or already read,
// so opening the next item is instant.
const consumeBodies = new Map();

// consumeQueued — is this card still waiting to be read? Read and archived are
// different states and BOTH sit outside the queue.
const consumeQueued = (c) => !c.read && !c.seeded;

// ---- addresses ----

// consumeHashFor names a reader state as a URL.
function consumeHashFor(st) {
  const v = st.view || "unread";
  if (v === "later") return "#/feed/later";
  if (v === "later-done") return "#/feed/later/done";
  if (v === "today") return "#/feed/today";
  const tail = v === "all" ? "/all" : "";
  if (st.sub) return "#/feed/source/" + encodeURIComponent(st.sub) + tail;
  if (st.list) return "#/feed/stream/" + encodeURIComponent(st.list) + tail;
  if (st.type) return "#/feed/type/" + encodeURIComponent(st.type) + tail;
  return v === "all" ? "#/feed/all" : "#/feed/unread";
}

function consumeHash() {
  return consumeHashFor({ view: consumeView, list: consumeList, sub: consumeSub, type: consumeType });
}

// consumeApplyHash reads a #/feed… address into the FEED state. Bare #/feed is
// the Inbox. Returns nothing; the caller loads.
function consumeApplyHash(h) {
  const parts = (h || "").replace(/^#\/feed\/?/, "").split("/").filter(Boolean).map((p) => {
    try { return decodeURIComponent(p); } catch (e) { return p; }
  });
  if (!parts.length) { state.feedFilter = ""; return; }
  if (parts[0] === "approvals") { state.feedFilter = "proposal"; return; }
  state.feedFilter = "consume";
  consumeList = ""; consumeSub = ""; consumeType = "";
  const all = parts[parts.length - 1] === "all";
  switch (parts[0]) {
    case "later": consumeView = parts[1] === "done" ? "later-done" : "later"; break;
    case "today": consumeView = "today"; break;
    case "all": consumeView = "all"; break;
    case "type": consumeType = parts[1] || ""; consumeView = all ? "all" : "unread"; break;
    case "stream": consumeList = parts[1] || ""; consumeView = all ? "all" : "unread"; break;
    case "source": consumeSub = parts[1] || ""; consumeView = all ? "all" : "unread"; break;
    default: consumeView = "unread";
  }
  consumeShowAll = false;
}

// consumeGo moves the reader to a state through its address.
function consumeGo(st) {
  const h = consumeHashFor(st);
  if (location.hash === h) { consumeApplyHash(h); consumeFilterChanged(); return; }
  location.hash = h;
}

// ---- the list row ----

// consumeCardEl renders one row. The inbox registry and the reader share it so
// the two surfaces never drift apart.
function consumeCardEl(c) {
  const media = consumeMedia(c);
  const row = el("article", "rdr-row consume-row rdr-" + media +
    (consumeQueued(c) ? " unread" : "") + (c.id === consumeSel ? " sel" : "") + (c.pending ? " pending" : ""));
  row.dataset.consumeId = c.id;
  row.tabIndex = -1;

  row.append(el("span", "rdr-dot"));
  if (c.image && (media === "video" || media === "podcast")) {
    const thumb = el("img", "rdr-thumb");
    thumb.loading = "lazy"; thumb.decoding = "async"; thumb.alt = ""; thumb.referrerPolicy = "no-referrer";
    thumb.src = c.image;
    thumb.onerror = () => thumb.remove();
    row.append(thumb);
  }

  const main = el("div", "rdr-row-main");
  const meta = el("div", "rdr-row-meta");
  meta.append(el("span", "rdr-src", c.source || "feed"));
  if (c.author && c.author !== c.source) meta.append(el("span", "rdr-author", c.author));
  const when = c.published || c.saved;
  if (when) meta.append(el("time", "rdr-when", fmtWhen(when)));
  const cost = consumeCost(c);
  if (cost) meta.append(el("span", "rdr-cost", cost));
  if (c.preview === "paid" && media !== "post") meta.append(el("span", "rdr-flag", "paid"));
  else if (c.preview && media !== "post") meta.append(el("span", "rdr-flag", "preview"));
  if (c.curated) meta.append(el("span", "rdr-flag rdr-flag-curated", "curated"));
  if (c.later && consumeView !== "later") meta.append(el("span", "rdr-flag rdr-flag-later", c.laterDone ? "done" : "later"));
  if (c.pending) meta.append(el("span", "rdr-flag", "reading the link…"));
  main.append(meta);

  const title = el("a", "rdr-row-title", media === "post"
    ? (c.excerpt || c.title || "(empty post)") : (c.title || "(untitled)"));
  title.href = "#/read/" + encodeURIComponent(c.id);
  title.onclick = (e) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; // a new tab is the browser's
    e.preventDefault();
    consumeOpen(c.id);
  };
  main.append(title);
  if (media !== "post" && c.excerpt) main.append(el("div", "rdr-row-excerpt", c.excerpt));
  row.append(main);

  const acts = el("div", "rdr-row-acts");
  acts.append(consumeIconBtn(c.later && !c.laterDone ? "◷ later ✓" : "◷ later",
    c.later && !c.laterDone ? "In Later (l)" : "Save to Later (l)", () => consumeToggleLater(c)));
  if (c.later && c.laterId && consumeView === "later") {
    acts.append(consumeIconBtn("✓ done", "Done — out of the queue (e)", () => consumeLaterDone(c, true)));
  } else if (c.later && c.laterId && consumeView === "later-done") {
    acts.append(consumeIconBtn("↺ back", "Back into the queue", () => consumeLaterDone(c, false)));
  }
  acts.append(consumeIconBtn(consumeQueued(c) ? "mark read" : "mark unread", "Toggle read (m)", () => consumeToggleRead(c)));
  if (c.url) acts.append(consumeIconBtn("↗", "Open the original (v)", () => window.open(c.url, "_blank", "noopener")));
  if (c.later && c.laterId && (consumeView === "later" || consumeView === "later-done")) {
    acts.append(consumeIconBtn("remove", "Remove from Later", () => consumeLaterRemove(c, row)));
  } else if (!c.later || !c.laterId || c.subId !== "_later") {
    acts.append(consumeIconBtn("×", "Dismiss", () => consumeDismiss(c.id, row)));
  }
  row.append(acts);

  row.addEventListener("click", (e) => {
    if (e.target.closest("button, a, input")) return;
    consumeOpen(c.id);
  });
  row.addEventListener("pointerenter", () => consumeHoverPrefetch(c.id));
  return row;
}

function consumeIconBtn(text, title, onclick) {
  const b = el("button", "rdr-act", text);
  b.type = "button";
  b.title = title;
  b.setAttribute("aria-label", title.replace(/\s*\([a-z]\)$/i, ""));
  b.addEventListener("click", (e) => { e.stopPropagation(); onclick(e); });
  return b;
}

// consumeMedia folds a card's flavour into the four media types.
function consumeMedia(c) {
  switch (c.type) {
    case "video": return "video";
    case "podcast": return "podcast";
    case "x": return "post";
  }
  return "article";
}

// consumeCost is what a piece asks of you: minutes to read, or its length.
function consumeCost(c) {
  if (c.duration) {
    const m = Math.round(c.duration / 60);
    return m >= 60 ? Math.floor(m / 60) + " h " + (m % 60) + " min" : m + " min";
  }
  const media = consumeMedia(c);
  if (media === "article" && c.minutes && !c.pending) return c.minutes + " min read";
  return "";
}

// ---- verbs on a row ----

function consumeRowEl(id) {
  return els.feedList && els.feedList.querySelector(`.consume-row[data-consume-id="${CSS.escape(id)}"]`);
}

function consumeFind(id) {
  return (consumeCache.items || []).find((x) => x.id === id);
}

// consumeRepaint swaps one row in place. A full re-render would lose the
// scroll position and the selection.
function consumeRepaint(c, card) {
  const node = card || consumeRowEl(c.id);
  if (node) node.replaceWith(consumeCardEl(c));
}

// consumeCountShift keeps the sidebar honest without a round trip when an
// item's read state flips.
function consumeCountShift(c, delta) {
  if (!consumeNav) return;
  consumeNav.unread = Math.max(0, (consumeNav.unread || 0) + delta);
  const t = consumeMedia(c);
  consumeNav.types = consumeNav.types || {};
  consumeNav.types[t] = Math.max(0, (consumeNav.types[t] || 0) + delta);
  if (c.subId && consumeNav.subs) consumeNav.subs[c.subId] = Math.max(0, (consumeNav.subs[c.subId] || 0) + delta);
  if (c.list && consumeNav.streams && c.list in consumeNav.streams) consumeNav.streams[c.list] = Math.max(0, consumeNav.streams[c.list] + delta);
  if (typeof renderFeedSide === "function") renderFeedSide();
}

async function consumeToggleRead(c) {
  const queued = consumeQueued(c);
  const url = `/api/consume/item/${encodeURIComponent(c.id)}/${queued ? "read" : "unread"}`;
  if (!(await consumePost(url))) return;
  if (queued) { c.read = true; } else { c.read = false; c.seeded = false; }
  consumeCountShift(c, queued ? -1 : 1);
  consumeMemo.clear();
  consumeRepaint(c);
  consumeHeaderRefresh();
}

// consumeMarkOpened records an open locally: the server marked it read when
// the reader fetched it (or on the explicit POST for a prefetched body).
function consumeMarkOpened(id) {
  const c = consumeFind(id);
  if (!c || !consumeQueued(c)) return;
  c.read = true;
  consumeCountShift(c, -1);
  consumeMemo.clear();
  consumeRepaint(c);
  consumeHeaderRefresh();
}

async function consumeToggleLater(c) {
  if (c.later && !c.laterDone && c.laterId) {
    // Already queued: from anywhere but the queue itself, the button says so
    // and a second press finishes it rather than silently removing it.
    await consumeLaterDone(c, true);
    return;
  }
  const res = await consumePostJSON("/api/consume/later", c.subId === "_later" && c.url ? { url: c.url } : { item: c.id });
  if (!res) return;
  c.later = true; c.laterDone = false; c.laterId = res.entry && res.entry.id;
  if (consumeNav) consumeNav.later = (consumeNav.later || 0) + 1;
  consumeMemo.clear();
  consumeRepaint(c);
  showToast("saved to Later");
  if (typeof renderFeedSide === "function") renderFeedSide();
  if (typeof readSyncLater === "function") readSyncLater(c.id, c);
}

async function consumeLaterDone(c, done) {
  if (!c.laterId) return;
  if (!(await consumePost(`/api/consume/later/${encodeURIComponent(c.laterId)}/${done ? "done" : "undone"}`))) return;
  c.laterDone = done;
  if (consumeNav) consumeNav.later = Math.max(0, (consumeNav.later || 0) + (done ? -1 : 1));
  consumeMemo.clear();
  const leaves = (done && consumeView === "later") || (!done && consumeView === "later-done");
  if (leaves) {
    consumeForget(c.id);
    const row = consumeRowEl(c.id);
    if (row) row.remove();
    showToast(done ? "done · undo" : "back in Later", done ? () => consumeLaterDone(c, false).then(() => loadConsume()) : null);
  } else {
    consumeRepaint(c);
    if (done) showToast("marked done in Later");
  }
  if (typeof renderFeedSide === "function") renderFeedSide();
  consumeHeaderRefresh();
}

async function consumeLaterRemove(c, row) {
  if (!c.laterId) return;
  if (!(await consumePost(`/api/consume/later/${encodeURIComponent(c.laterId)}/remove`))) return;
  if (!c.laterDone && consumeNav) consumeNav.later = Math.max(0, (consumeNav.later || 0) - 1);
  consumeForget(c.id);
  consumeMemo.clear();
  if (row) row.remove();
  showToast("removed from Later");
  if (typeof renderFeedSide === "function") renderFeedSide();
  consumeHeaderRefresh();
}

// consumeDismiss — gone from every view, with a brief undo.
//
// ⚠ Anything that removes an item must remove it from the CACHE too, or the
// next repaint undoes the work (the 2026-08-25 "dismiss does nothing but
// flicker" bug). Routed through cardDecide/feedPost: consumeForget runs only
// once the write is confirmed, and a refused dismiss restores the row.
async function consumeDismiss(id, card) {
  const c = consumeFind(id);
  const ok = await cardDecide(card, `/api/consume/item/${encodeURIComponent(id)}/dismiss`, {}, () => consumeForget(id));
  if (!ok) { await loadConsume(); return; } // the write failed — show the truth
  if (c && consumeQueued(c)) consumeCountShift(c, -1);
  consumeMemo.clear();
  // showToast makes the WHOLE toast the click target, so the label has to say
  // what clicking it does.
  showToast("dismissed · undo", () => consumeUndismiss(id));
  consumeHeaderRefresh();
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
  consumeMemo.clear();
  await loadConsume();
  showToast("restored");
}

// ---- opening ----

// consumePaneMode: a wide screen reads beside the list; a narrow one reads on
// its own page.
function consumePaneMode() {
  return !!(els.feedPane && window.matchMedia && window.matchMedia("(min-width: 1100px)").matches);
}

// consumeSelect marks one row selected and keeps it in view.
function consumeSelect(id, scroll) {
  consumeSel = id;
  if (!els.feedList) return;
  els.feedList.querySelectorAll(".consume-row.sel").forEach((r) => r.classList.remove("sel"));
  const row = consumeRowEl(id);
  if (row) {
    row.classList.add("sel");
    if (scroll) row.scrollIntoView({ block: "nearest" });
  }
}

// consumeOpen shows one item: in the pane beside the list, or on its page.
function consumeOpen(id) {
  consumeSelect(id, true);
  if (typeof readList !== "undefined") readList = (consumeCache.items || []).slice();
  if (consumePaneMode() && typeof readOpenInPane === "function") {
    readOpenInPane(id);
    consumePrefetchAround(id);
  } else {
    openRead(id);
  }
}

// consumeStep moves the selection (j/k), opening in the pane when there is one.
function consumeStep(delta) {
  const items = consumeCache.items || [];
  if (!items.length) return;
  let i = items.findIndex((x) => x.id === consumeSel);
  i = i < 0 ? (delta > 0 ? 0 : items.length - 1) : Math.min(items.length - 1, Math.max(0, i + delta));
  const next = items[i];
  if (!next) return;
  if (consumePaneMode()) consumeOpen(next.id);
  else consumeSelect(next.id, true);
  if (i >= items.length - 5) consumeLoadMore();
}

// ---- prefetch ----

// consumePeek fetches a body WITHOUT marking it read (?peek=1).
async function consumePeek(id) {
  if (consumeBodies.has(id)) return consumeBodies.get(id);
  try {
    const r = await fetch(`/api/consume/item/${encodeURIComponent(id)}?peek=1`);
    if (!r.ok) return null;
    const d = await r.json();
    consumeBodies.set(id, d);
    if (consumeBodies.size > 80) consumeBodies.delete(consumeBodies.keys().next().value);
    return d;
  } catch (e) { return null; }
}

// consumePrefetchAround warms the next few items after an open, one at a time
// so a slow link never competes with what is on screen.
async function consumePrefetchAround(id) {
  const items = consumeCache.items || [];
  const i = items.findIndex((x) => x.id === id);
  for (const x of items.slice(i + 1, i + 4)) {
    if (!consumeBodies.has(x.id)) await consumePeek(x.id);
  }
}

// consumePrefetchTop warms the first unread rows of a freshly painted list.
async function consumePrefetchTop() {
  if (!consumePaneMode()) return;
  for (const x of (consumeCache.items || []).slice(0, 3)) {
    if (!consumeIsActiveView()) return;
    await consumePeek(x.id);
  }
}

let consumeHoverTimer = null;
function consumeHoverPrefetch(id) {
  clearTimeout(consumeHoverTimer);
  consumeHoverTimer = setTimeout(() => consumePeek(id), 120);
}

// ---- the CONSUME view ----

function consumeIsActiveView() { return feedFilter() === "consume"; }

function consumeQueryString(offset) {
  const q = new URLSearchParams({ view: consumeView, limit: String(CONSUME_PAGE) });
  if (consumeList) q.set("list", consumeList);
  if (consumeSub) q.set("sub", consumeSub);
  if (consumeType) q.set("type", consumeType);
  if (consumeQuery.trim()) q.set("q", consumeQuery.trim());
  if (offset) q.set("offset", String(offset));
  return q.toString();
}

// consumeSignature: what the list shows, so an identical answer repaints
// nothing (and cannot steal the scroll position or the selection).
function consumeSignature(d) {
  return (d.items || []).map((x) => x.id + (x.read ? "r" : "") + (x.seeded ? "s" : "") + (x.later ? "l" + (x.laterDone ? "d" : "") : "") + (x.curated ? "c" : "") + (x.pending ? "p" : "")).join(",") + "|" + (d.more ? 1 : 0);
}

// `token` is the FEED render token (45-feed.js). loadFeed passes its own so the
// two surfaces order as one; every other caller claims a fresh one. Either way a
// response that lands after the user has left CONSUME paints nothing.
let consumeLoadError = "";
let consumeShownSig = "";
async function loadConsume(token) {
  if (token === undefined) token = feedClaimRender();
  const key = consumeQueryString(0);
  let next, error = "";
  try {
    const response = await fetch("/api/consume?" + key); if (!response.ok) throw Error("Reading feed unavailable");
    next = await response.json();
  } catch (e) { error = "Could not load the reading feed. Try again."; }
  if (feedRenderStale(token)) return;
  consumeLoadError = error;
  if (error) { if (!(consumeCache.items || []).length) renderConsume(); return; }
  consumeMemo.set(key, next);
  if (next.nav) consumeNav = next.nav;
  const sig = consumeSignature(next);
  const same = sig === consumeShownSig && els.feedList && els.feedList.querySelector(".consume-content");
  consumeCache = next;
  if (same) { consumeHeaderRefresh(); if (typeof renderFeedSide === "function") renderFeedSide(); return; }
  renderConsume();
  consumePrefetchTop();
}

// consumeLoadMore appends the next page in place.
async function consumeLoadMore() {
  if (consumeMoreBusy || !consumeCache.more || !consumeIsActiveView()) return;
  consumeMoreBusy = true;
  const key = consumeQueryString(0);
  try {
    const r = await fetch("/api/consume?" + consumeQueryString((consumeCache.items || []).length));
    if (!r.ok) return;
    const d = await r.json();
    if (consumeQueryString(0) !== key || !consumeIsActiveView()) return; // the view changed meanwhile
    const have = new Set((consumeCache.items || []).map((x) => x.id));
    const fresh = (d.items || []).filter((x) => !have.has(x.id));
    consumeCache.items = (consumeCache.items || []).concat(fresh);
    consumeCache.more = !!d.more;
    const host = els.feedList.querySelector(".consume-content");
    if (host) fresh.forEach((c) => host.append(consumeCardEl(c)));
    consumeShownSig = consumeSignature(consumeCache);
    consumeMemo.set(key, consumeCache);
    consumeRenderMore();
  } catch (e) {} finally { consumeMoreBusy = false; }
}

// consumeFilterChanged repaints from memory at once, then asks the server.
// Every view change goes through here.
function consumeFilterChanged() {
  const header = els.feedList.querySelector(".consume-head"); if (header) consumeHeader(header);
  consumeShowAll = false;
  const memo = consumeMemo.get(consumeQueryString(0));
  if (memo) { consumeCache = memo; renderConsume(); }
  else if (els.feedList.querySelector(".consume-content")) {
    // Nothing remembered for this view: say it is loading rather than show the
    // previous view's rows under the new name.
    const host = els.feedList.querySelector(".consume-content");
    host.replaceChildren(el("div", "consume-loading micro-label", "loading…"));
    consumeShownSig = "";
  }
  if (typeof renderFeedSide === "function") renderFeedSide();
  loadConsume();
}

// consumeSearch is debounced so a query costs one request per pause.
const consumeSearch = debounce(() => consumeFilterChanged(), 200);

function renderConsume() {
  if (!consumeIsActiveView()) return; // the chip is off — FEED owns the list now
  const surface = els.feedList;
  let head = surface.querySelector(".consume-head"), host = surface.querySelector(".consume-content");
  if (!head || !host) { surface.replaceChildren(); head = consumeHeader(); host = el("div", "consume-content"); surface.append(head, host); }
  else consumeHeader(head);
  host.replaceChildren();
  els.feedSignals.innerHTML = "";
  renderApprovalInspector(); // release the proposal column/sheet after replacing its cards
  if (consumeLoadError) { const notice = emptyRow(consumeLoadError); notice.setAttribute("role", "status"); host.append(notice, pillLight("Retry", () => loadConsume())); return; }

  // The panels (manage, curated) live in their own slot ABOVE the list and
  // survive its repaints (consumeRenderPanels).
  let panels = surface.querySelector(".consume-panels");
  if (!panels) { panels = el("div", "consume-panels"); surface.insertBefore(panels, host); }
  consumeRenderPanels();
  const oldBanner = surface.querySelector(".consume-subbar");
  if (oldBanner) oldBanner.remove();
  if (consumeSub) surface.insertBefore(consumeSubBanner(), host);

  const items = consumeCache.items || [];
  consumeShownSig = typeof consumeSignature === "function" ? consumeSignature(consumeCache) : "";
  if (!items.length) {
    host.append(consumeEmptyEl());
    consumeRenderMore();
    return;
  }
  items.forEach((c) => host.append(consumeCardEl(c)));
  consumeRenderMore();
  if (typeof renderFeedSide === "function") renderFeedSide();
}

// consumeEmptyEl says which kind of empty this is. "Nothing matches" and
// "nothing exists" are different messages, and with a search box the
// difference IS the message.
function consumeEmptyEl() {
  const ctype = typeof consumeType === "string" ? consumeType : "";
  const filtered = consumeQuery.trim() || consumeSub || consumeList || ctype;
  let text;
  if (consumeQuery.trim()) text = "Nothing matches that search.";
  else if (consumeView === "later") text = "Later is empty. Paste any link with “＋ save link”, press l on anything in the feed, or share a link from your phone.";
  else if (consumeView === "later-done") text = "Nothing finished yet. Items you mark done in Later land here.";
  else if (consumeView === "today") text = "Nothing published in the last day.";
  else if (consumeView === "unread") text = filtered ? "All caught up here. Older posts are under ALL." : "All caught up. Older posts are under ALL.";
  else text = filtered ? "Nothing here yet." : "Nothing here yet. Add a source with ＋ follow.";
  return emptyRow(text);
}

// consumeRenderMore keeps the "more" affordance and its scroll sentinel AFTER
// the list, never inside it: the list's last child is always a row.
function consumeRenderMore() {
  const surface = els.feedList;
  let more = surface.querySelector(".rdr-more");
  const want = !!(consumeCache && consumeCache.more);
  if (!want) { if (more) more.remove(); return; }
  if (!more) {
    more = el("button", "signal-more rdr-more", "▾ more");
    more.type = "button";
    more.onclick = () => (typeof consumeLoadMore === "function" ? consumeLoadMore() : null);
    surface.append(more);
    if (typeof IntersectionObserver === "function") {
      const io = new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting) && typeof consumeLoadMore === "function") consumeLoadMore();
      }, { rootMargin: "600px" });
      io.observe(more);
    }
  }
  const shown = (consumeCache.items || []).length, total = consumeCache.count || 0;
  more.textContent = total > shown ? `▾ ${total - shown} more` : "▾ more";
}

// consumeSubBanner names the source whose history is open, with the way out.
function consumeSubBanner() {
  const sub = (consumeSubs.subscriptions || []).find((x) => x.id === consumeSub);
  const bar = el("div", "consume-subbar");
  bar.append(el("span", "micro-label", (sub ? sub.title : consumeSub) + " · everything we have"));
  bar.append(pillLight("× all sources", () => (typeof consumeGo === "function" ? consumeGo({ view: "unread" }) : null)));
  return bar;
}

// consumeTitle names the current view for the list header.
function consumeTitle() {
  const ctype = typeof consumeType === "string" ? consumeType : "";
  const types = { article: "Articles", video: "Videos", podcast: "Podcasts", post: "Posts" };
  if (consumeView === "later") return "Later";
  if (consumeView === "later-done") return "Later · done";
  if (consumeView === "today") return "Today";
  if (consumeSub) {
    const sub = (consumeSubs.subscriptions || []).find((x) => x.id === consumeSub);
    return sub ? sub.title : consumeSub;
  }
  if (consumeList) return consumeList;
  if (ctype) return types[ctype] || ctype;
  return consumeView === "all" ? "All" : "Unread";
}

function consumeHeader(head) {
  if (!head) {
    head = el("div", "consume-head");
    const left = el("div", "consume-head-left");
    const title = el("h2", "rdr-title");
    const views = el("span", "feed-filters consume-view-filters"), lists = el("span", "feed-filters consume-list-filters");
    if (!consumeSearchEl) { consumeSearchEl = el("input", "pp-in consume-search"); consumeSearchEl.type = "search"; consumeSearchEl.placeholder = "search titles and excerpts…"; consumeSearchEl.setAttribute("aria-label", "Search reading feed"); consumeSearchEl.oninput = () => { consumeQuery = consumeSearchEl.value; feedClaimRender(); consumeSearch(); }; }
    left.append(title, views, lists, consumeSearchEl);
    const right = el("div", "consume-head-right"), count = el("span", "micro-label consume-count");
    const save = pillLight("＋ save link", () => (typeof consumeSaveLinkRow === "function" ? consumeSaveLinkRow(head) : null)); save.classList.add("consume-save-link");
    const refresh = pillLight("refresh", async () => {
      refresh.disabled = true; refresh.textContent = "refreshing…";
      try { await consumePost("/api/consume/poll-all"); if (typeof consumeMemo === "object") consumeMemo.clear(); await loadConsumeSubs(); await loadConsume(); }
      finally { refresh.disabled = false; refresh.textContent = "refresh"; }
    });
    const mark = pillLight("mark all read", async () => {
      const q = new URLSearchParams();
      if (consumeList) q.set("list", consumeList);
      if (consumeSub) q.set("sub", consumeSub);
      if (typeof consumeType === "string" && consumeType) q.set("type", consumeType);
      if (consumeQuery.trim()) q.set("q", consumeQuery.trim());
      const s = q.toString();
      const res = await consumePostJSON("/api/consume/read-all" + (s ? "?" + s : ""));
      if (res) { if (typeof consumeMemo === "object") consumeMemo.clear(); showToast((res.marked || 0) + " marked read"); await loadConsume(); }
    }); mark.classList.add("consume-mark-all");
    const curated = pillLight("CURATED", () => consumeTogglePanel("curated")); curated.classList.add("consume-curated-toggle");
    const manage = pillLight("MANAGE", () => consumeTogglePanel("subscriptions")); manage.classList.add("consume-manage-toggle");
    right.append(count, save, refresh, mark, curated, manage); head.append(left, right);
  }
  const title = head.querySelector(".rdr-title");
  if (title) title.textContent = typeof consumeTitle === "function" ? consumeTitle() : "";
  // The chips are this view's own toggles: unread/all within a scope, queue/
  // done within Later, none for Today.
  const inLater = consumeView === "later" || consumeView === "later-done";
  const viewChoices = inLater ? [["later", "QUEUE"], ["later-done", "DONE"]]
    : consumeView === "today" ? [] : [["unread", "UNREAD"], ["all", "ALL"]];
  renderFilterButtons(head.querySelector(".consume-view-filters"), viewChoices, consumeView, (value) => {
    consumeView = value;
    if (typeof consumeGo === "function") consumeGo({ view: value, list: consumeList, sub: consumeSub, type: typeof consumeType === "string" ? consumeType : "" });
    else consumeFilterChanged();
  });
  // Stream chips stand in for the sidebar on a narrow screen (CSS hides them
  // beside it).
  renderFilterButtons(head.querySelector(".consume-list-filters"), inLater ? [] : (consumeCache.lists || []).map((value) => [value, value]), consumeList, (value) => {
    const list = consumeList === value ? "" : value;
    if (typeof consumeGo === "function") consumeGo({ view: consumeView === "today" ? "unread" : consumeView, list });
    else { consumeList = list; consumeFilterChanged(); }
  });
  const unread = consumeCache.unread || 0;
  const shown = (consumeCache.items || []).length, total = consumeCache.count;
  head.querySelector(".consume-count").textContent = inLater || consumeView === "today" || consumeView === "all"
    ? (total != null ? total : shown) + (inLater ? " saved" : " items")
    : (consumeList && consumeCache.total > unread ? unread + " unread in " + consumeList : (total != null ? total : unread) + " unread");
  head.querySelector(".consume-mark-all").hidden = inLater || consumeView === "today" || !(total != null ? total : unread);
  head.querySelector(".consume-curated-toggle").textContent = consumeCuratedOpen ? "close curated" : "CURATED";
  head.querySelector(".consume-manage-toggle").textContent = consumeManageOpen ? "close" : "MANAGE";
  head.querySelector(".consume-manage-toggle").setAttribute("aria-expanded", String(consumeManageOpen));
  head.querySelector(".consume-curated-toggle").setAttribute("aria-expanded", String(consumeCuratedOpen));
  return head;
}

// consumeHeaderRefresh updates the header's counts without touching the list.
function consumeHeaderRefresh() {
  const head = els.feedList && els.feedList.querySelector(".consume-head");
  if (head) consumeHeader(head);
}

// consumeSaveLinkRow: paste any link into Later.
function consumeSaveLinkRow(head) {
  const host = head.parentNode;
  if (!host || host.querySelector(".consume-savelink")) { const i = host && host.querySelector(".consume-savelink input"); if (i) i.focus(); return; }
  const row = el("div", "consume-savelink consume-note-row");
  const input = inputEl("paste any link — article, video, podcast, post");
  input.className = "consume-note-input";
  input.setAttribute("aria-label", "Link to save to Later");
  const save = async () => {
    const url = input.value.trim();
    if (!url) return;
    input.disabled = true; go.disabled = true; go.textContent = "saving…";
    const res = await consumePostJSON("/api/consume/later", { url });
    input.disabled = false; go.disabled = false; go.textContent = "save to Later";
    if (!res) return;
    row.remove();
    consumeMemo.clear();
    showToast("saved to Later — " + ((res.entry && res.entry.title) || url).slice(0, 80), () => consumeGo({ view: "later" }));
    if (consumeView === "later") loadConsume(); else loadConsumeSubs();
  };
  const go = pillLight("save to Later", save);
  input.onkeydown = (e) => {
    if (e.key === "Enter") { e.preventDefault(); save(); }
    if (e.key === "Escape") { e.stopPropagation(); row.remove(); }
  };
  row.append(input, go, pillLight("cancel", () => row.remove()));
  head.after(row);
  input.focus();
}

// ---- the manage panel ----

// Panel visibility is local UI state: never wait for the network to open or close.
const consumePanelState={subscriptions:{request:0,loading:false,error:''},curated:{request:0,loading:false,error:''}};
function consumeRenderPanels(){
 const host=els.feedList.querySelector('.consume-panels');if(!host)return;
 for(const kind of ['subscriptions','curated']){
  let slot=host.querySelector('[data-panel="'+kind+'"]');
  if(!(kind==='curated'?consumeCuratedOpen:consumeManageOpen)){slot?.remove();continue;}
  if(!slot){slot=el('div');slot.dataset.panel=kind;host.append(slot);}
  const status=consumePanelState[kind],signature=JSON.stringify(status);
  if(slot.dataset.signature===signature)continue;
  slot.dataset.signature=signature;slot.replaceChildren();
  if(status.loading||status.error){
   const panel=el('div','consume-manage');panel.setAttribute('role','status');
   panel.append(el('span','micro-label',status.loading?'Loading '+(kind==='curated'?'curated items':'subscriptions')+'…':status.error));
   if(status.error)panel.append(pillLight('Retry',()=>consumeLoadPanel(kind)));
   slot.append(panel);
  }else slot.append(kind==='curated'?consumeCuratedPanel():consumeManagePanel());
 }
}
function consumeTogglePanel(kind){
 if(kind==='curated')consumeCuratedOpen=!consumeCuratedOpen;else consumeManageOpen=!consumeManageOpen;
 const head=els.feedList.querySelector('.consume-head');if(head)consumeHeader(head);
 if(kind==='curated'?consumeCuratedOpen:consumeManageOpen)consumeLoadPanel(kind);
 else consumeRenderPanels();
}
async function consumeLoadPanel(kind){
 const status=consumePanelState[kind],request=++status.request;
 status.loading=true;status.error='';consumeRenderPanels();
 try{
  const response=await fetch('/api/consume/'+kind);if(!response.ok)throw Error('HTTP '+response.status);
  const data=await response.json();if(request!==status.request)return;
  if(kind==='curated')consumeCurated=data;else{consumeSubs=data;if(data.nav)consumeNav=data.nav;}
 }catch(e){if(request!==status.request)return;status.error='Could not load '+(kind==='curated'?'curated items':'subscriptions')+'. Try again.';}
 if(request!==status.request)return;status.loading=false;
 consumeRenderPanels();
 if(kind==='subscriptions'&&typeof renderFeedSide==='function')renderFeedSide();
}
async function loadConsumeSubs(){await consumeLoadPanel('subscriptions');}

// consumeStreams: every stream the owner has named, for the pickers.
function consumeStreams() {
  const seen = new Set();
  (consumeSubs.subscriptions || []).forEach((s) => { if (s.list && s.list.toLowerCase() !== "unfiled") seen.add(s.list); });
  (consumeCache.lists || []).forEach((l) => seen.add(l));
  return [...seen].sort((a, b) => a.localeCompare(b));
}

// consumeStreamInput: a text box that suggests the existing streams.
function consumeStreamInput(value) {
  const input = inputEl("stream (optional)");
  input.value = value || "";
  const id = "consume-streams-" + Math.random().toString(36).slice(2, 8);
  const list = el("datalist");
  list.id = id;
  consumeStreams().forEach((s) => { const o = el("option"); o.value = s; list.append(o); });
  input.setAttribute("list", id);
  input.setAttribute("aria-label", "Stream");
  const wrap = el("span", "consume-stream-input");
  wrap.append(input, list);
  wrap.input = input;
  return wrap;
}

const CONSUME_MEDIA_LABEL = { article: "articles", video: "videos", podcast: "podcast", post: "posts" };

function consumeManagePanel() {
  const panel = el("div", "consume-manage");
  panel.append(el("div", "consume-manage-head micro-label", "sources"));
  panel.append(consumeAddRow());

  const subs = consumeSubs.subscriptions || [];
  const groups = {};
  subs.forEach((s) => { (groups[s.list && s.list.toLowerCase() !== "unfiled" ? s.list : ""] ||= []).push(s); });
  Object.keys(groups).sort((a, b) => (a === "") - (b === "") || a.localeCompare(b)).forEach((g) => {
    const head = el("div", "consume-group");
    head.append(el("span", "micro-label", g || "not in a stream"));
    if (g) head.append(consumeIconBtn("rename", "Rename this stream", () => consumeRenameStream(g, head)));
    panel.append(head);
    groups[g].forEach((s) => panel.append(consumeSubRow(s)));
  });
  if (!subs.length) panel.append(emptyRow("Nothing followed yet."));
  return panel;
}

function consumeRenameStream(name, head) {
  if (head.querySelector("input")) return;
  const input = inputEl("new name"); input.value = name;
  const save = async () => {
    const to = input.value.trim();
    if (!to || to === name) { form.remove(); return; }
    if (!(await consumePost("/api/consume/streams/rename", { from: name, to }))) return;
    if (consumeList === name) consumeList = to;
    consumeMemo.clear();
    showToast("stream renamed");
    await loadConsumeSubs(); await loadConsume();
  };
  const form = el("span", "consume-rename");
  input.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); save(); } if (e.key === "Escape") form.remove(); };
  form.append(input, pillLight("save", save), pillLight("cancel", () => form.remove()));
  head.append(form);
  input.focus(); input.select();
}

function consumeAddRow() {
  const row = el("div", "consume-add");
  const input = inputEl("feed, site, Substack, YouTube channel, podcast, or @handle");
  input.className = "consume-add-input";
  input.setAttribute("aria-label", "Source to follow");
  const stream = consumeStreamInput("");
  stream.input.className = "consume-add-list";

  const add = async () => {
    const value = input.value.trim();
    if (!value) return;
    input.disabled = stream.input.disabled = true;
    btn.textContent = "finding the feed…";
    const res = await consumePostJSON("/api/consume/subscriptions",
      { input: value, list: stream.input.value.trim(), mirror: "full" });
    input.disabled = stream.input.disabled = false;
    btn.textContent = "+ follow";
    if (!res) return;
    input.value = ""; stream.input.value = "";
    // ⚠ A new subscription is deliberately EMPTY of unread — everything the
    // feed already published is archived. Without saying so, zero unread reads
    // exactly like the bug this rule was written to fix.
    const name = (res.subscription && res.subscription.title) || "the feed";
    showToast(res.archived
      ? `following ${name} — ${res.archived} earlier posts archived; new ones arrive as they publish`
      : `following ${name}`);
    consumeMemo.clear();
    await loadConsumeSubs();
    await loadConsume();
  };
  const btn = pillLight("+ follow", add);
  input.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } };
  row.append(input, stream, btn);
  row.append(el("div", "consume-hint micro-label",
    "a site address finds its feed · a Substack needs no sign-in · a YouTube channel follows its videos · @handle follows an X account"));
  return row;
}

function consumeSubRow(s) {
  const row = el("div", "consume-sub");

  const dot = el("span", "consume-dot " + (s.lastErr ? "bad" : s.lastOk ? "ok" : "idle"));
  dot.title = s.lastErr || (s.lastOk ? "last checked " + fmtWhen(s.lastOk) : "not checked yet");
  row.append(dot);

  // Clicking the name opens this source's whole history.
  const name = el("button", "consume-sub-name", s.title || s.id);
  name.onclick = () => consumeGo({ view: "all", sub: s.id });
  row.append(name);
  row.append(el("span", "consume-sub-kind micro-label", CONSUME_MEDIA_LABEL[s.media] || (s.kind === "x" ? "posts" : "articles")));
  // ⚠ "0/14" read as a failure. Say what the numbers mean.
  const counts = s.unread + " unread" + (s.archived ? " · " + s.archived + " older" : "");
  row.append(el("span", "consume-sub-count micro-label", counts));
  if (s.mirror === "excerpt") row.append(el("span", "consume-sub-kind micro-label", "excerpt"));

  const acts = el("div", "consume-sub-acts");
  acts.append(pillLight("check now", async (e) => {
    const b = e && e.currentTarget; if (b) { b.disabled = true; b.textContent = "checking…"; }
    await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/poll`);
    consumeMemo.clear();
    await loadConsumeSubs(); await loadConsume();
  }));
  acts.append(pillLight("edit", () => consumeEditSub(s, row)));
  // Unfollowing forgets the cached history, so it asks once, in place.
  const unfollow = pillLight("unfollow", async () => {
    if (!unfollow.dataset.armed) {
      unfollow.dataset.armed = "1"; unfollow.textContent = "unfollow — sure?";
      setTimeout(() => { if (unfollow.isConnected) { delete unfollow.dataset.armed; unfollow.textContent = "unfollow"; } }, 4000);
      return;
    }
    if (!(await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/remove`))) return;
    if (consumeSub === s.id) consumeSub = "";
    consumeMemo.clear();
    showToast("unfollowed " + (s.title || s.id) + " — curated notes stay in your vault");
    await loadConsumeSubs(); await loadConsume();
  });
  acts.append(unfollow);
  row.append(acts);

  if (s.lastErr) row.append(el("div", "consume-sub-err micro-label", s.lastErr));

  // Paid posts are information, not a prompt. Sign-in is offered only once the
  // owner says he pays for this publication (owner decision 2026-09-27).
  if (s.kind !== "x" && s.media !== "post") row.append(consumePaidRow(s));
  if (s.media === "video") row.append(consumeSwitchRow(s, "shorts", "include Shorts", !!s.shorts));
  return row;
}

// consumeSwitchRow: one yes/no setting, saved on change.
function consumeSwitchRow(s, key, label, on) {
  const wrap = el("label", "consume-switch micro-label");
  const box = el("input");
  box.type = "checkbox"; box.checked = on;
  box.onchange = async () => {
    box.disabled = true;
    const ok = await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/update`, { [key]: box.checked });
    box.disabled = false;
    if (!ok) { box.checked = !box.checked; return; }
    s[key] = box.checked;
    showToast(key === "shorts" ? (box.checked ? "Shorts will arrive from the next check" : "Shorts are left out from the next check") : "saved");
    if (key === "pays") { const row = box.closest(".consume-sub"); if (row) row.replaceWith(consumeSubRow(s)); }
  };
  wrap.append(box, document.createTextNode(" " + label));
  return wrap;
}

// consumePaidRow: what the owner needs to know about paid posts, and sign-in
// only for a publication he pays for.
function consumePaidRow(s) {
  const wrap = el("div", "consume-signin");
  const pays = !!(s.pays || s.signedIn);
  if (s.paidPosts && !pays) {
    wrap.append(el("span", "micro-label consume-signin-why",
      s.paidPosts === 1 ? "1 paid post shows as a preview · free posts arrive whole"
        : s.paidPosts + " paid posts show as previews · free posts arrive whole"));
  }
  if (!s.paidPosts && !pays) return wrap;
  wrap.append(consumeSwitchRow(s, "pays", "I pay for this", pays));
  if (pays) wrap.append(consumeSignInRow(s));
  return wrap;
}

// consumeSignInRow — paste a session cookie so paid posts arrive whole.
//
// The cookie is stored server-side in the secrets tier and scoped to the
// publication's DOMAIN, so one paste covers every publication there. It is
// never written to the vault and never comes back in a response.
function consumeSignInRow(s) {
  const wrap = el("span", "consume-signin-state");
  if (s.signedIn) {
    wrap.append(el("span", "micro-label",
      (s.signInExpired ? "⚠ sign-in not working for " : "signed in to ") + (s.site || "this site")));
    // ⚠ A pasted cookie either works or silently does nothing. Answer that
    // directly instead of leaving it to be inferred from a label after a poll.
    wrap.append(pillLight("check", async (e) => {
      const btn = e && e.currentTarget;
      if (btn) { btn.disabled = true; btn.textContent = "checking…"; }
      const res = await consumePostJSON(`/api/consume/sites/${encodeURIComponent(s.site)}/verify`);
      if (btn) { btn.disabled = false; btn.textContent = "check"; }
      if (!res) return;
      showToast((res.ok ? "✓ " : "✗ ") + res.reason);
      await loadConsumeSubs();
    }));
    // sign-out (clearing the cookie) lives with every other credential in
    // Settings › Connections (agents plan §5)
    const manage = el("a", "consume-signin-link", "manage →");
    manage.href = "#/settings/connections/site/" + encodeURIComponent(s.site || "");
    wrap.append(manage);
    return wrap;
  }
  wrap.append(el("span", "micro-label consume-signin-why", s.signInExpired
    ? "sign-in expired — paid posts are previews again"
    : "sign in to read paid posts here"));
  // the cookie is pasted in Settings › Connections; this inline link opens the
  // add-site form there with the host prefilled
  const go = el("a", "consume-signin-link", s.signInExpired ? "paste a fresh cookie → Settings" : "sign in → Settings");
  go.href = "#/settings/connections/site/" + encodeURIComponent(s.site || "");
  wrap.append(go);
  return wrap;
}

function consumeEditSub(s, row) {
  if (row.querySelector(".consume-edit")) return;
  const box = el("div", "consume-edit");
  const title = inputEl("name"); title.value = s.title || "";
  title.setAttribute("aria-label", "Name");
  const stream = consumeStreamInput(s.list && s.list.toLowerCase() !== "unfiled" ? s.list : "");
  const mirror = el("select", "consume-mirror");
  [["full", "curating mirrors the full text"], ["excerpt", "curating shares an excerpt + link"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v;
    if ((s.mirror || "full") === v) o.selected = true;
    mirror.append(o);
  });
  // Full text: what to do when this publisher only ships a teaser.
  const ft = el("select", "consume-mirror");
  [["auto", "full text: when cut short"], ["on", "full text: always fetch"], ["off", "full text: never fetch"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v;
    if ((s.fulltext || "auto") === v) o.selected = true;
    ft.append(o);
  });
  const save = async () => {
    const ok = await consumePost(`/api/consume/subscriptions/${encodeURIComponent(s.id)}/update`,
      { title: title.value.trim(), list: stream.input.value.trim(), mirror: mirror.value, fulltext: ft.value, minChars: s.minChars || 0 });
    if (!ok) return;
    consumeMemo.clear();
    await loadConsumeSubs(); await loadConsume();
  };
  box.append(title, stream, mirror, ft, pillLight("save", save), pillLight("cancel", () => box.remove()));
  row.append(box);
  title.focus();
}

// ---- the curated panel ----
//
// The owner's audit surface: exactly what the public feed serves, as a list,
// with the note editable in place. It reads /api/consume/curated (the private
// mirror) and writes ONLY through the existing curate/uncurate endpoints — the
// note is frontmatter in the vault note, and re-curating with a new note is
// the edit path; the body is never touched.

async function loadConsumeCurated(){await consumeLoadPanel('curated');}

function consumeCuratedPanel() {
  const panel = el("div", "consume-manage consume-curated");
  panel.append(el("div", "consume-manage-head micro-label", "curated → public feed"));

  if (consumeCurated.public) {
    const pub = el("div", "consume-public");
    pub.append(el("span", "micro-label", "public feed"));
    pub.append(el("span", "consume-public-url", consumeCurated.public));
    pub.append(pillLight("open public feed ↗", () => window.open(consumeCurated.public, "_blank", "noopener")));
    panel.append(pub);
  } else {
    // publicPort off ≠ nothing curated — say which one it is.
    panel.append(el("div", "consume-hint micro-label",
      "the public feed is not being served yet — these entries are staged for it"));
  }

  const entries = consumeCurated.entries || [];
  entries.forEach((en) => panel.append(consumeCuratedRow(en)));
  if (!entries.length) panel.append(emptyRow("Nothing curated yet — → CURATE on any card puts it here."));
  return panel;
}

function consumeCuratedRow(en) {
  const row = el("div", "consume-sub consume-curated-row");
  row.append(el("span", "consume-curated-title", en.title || "(untitled)"));
  const meta = [en.source, en.author, en.curated ? "curated " + fmtWhen(en.curated) : ""]
    .filter(Boolean).join(" · ");
  if (meta) row.append(el("span", "consume-sub-count micro-label", meta));

  const acts = el("div", "consume-sub-acts");
  if (en.url) acts.append(pillLight("original ↗", () => window.open(en.url, "_blank", "noopener")));
  // A curated platform link plays HERE, on the private side. The public feed
  // carries the link and the note and no third-party markup — feed readers
  // strip frames, and public.go's isolation argument is worth more than a
  // player nobody would see (plan §5.5).
  if (en.embed) acts.append(playPill(en.embed, row));
  acts.append(pillLight("edit note", () => consumeCuratedEditNote(en, row)));
  acts.append(pillLight("un-curate", async () => {
    if (!(await consumePost(`/api/consume/item/${encodeURIComponent(en.itemId)}/uncurate`))) return;
    showToast("un-curated — the note stays in your vault");
    consumeMemo.clear();
    await loadConsumeCurated();
    await loadConsume(); // the row's "curated" flag changes too
  }));
  row.append(acts);

  row.append(el("div", "consume-curated-note micro-label",
    en.note ? "“" + en.note + "”" : "(no note)"));
  return row;
}

// EMBED_TEMPLATES is the whole allowlist. The server parses a
// `provider:kind:id` descriptor out of the canonical URL (consume/linkmeta.go,
// consume/rss.go for channel feeds) and never carries the provider's own
// `html`; the frame address is built here from these templates, so what loads
// is an origin this file names.
const EMBED_TEMPLATES = {
  spotify: (kind, id) => "https://open.spotify.com/embed/" + kind + "/" + id,
  youtube: (kind, id) => "https://www.youtube-nocookie.com/embed/" + id + "?rel=0",
  vimeo: (kind, id) => "https://player.vimeo.com/video/" + id,
};

function embedFrame(descriptor) {
  const parts = String(descriptor || "").split(":");
  if (parts.length !== 3) return null;
  const [provider, kind, id] = parts;
  const tmpl = EMBED_TEMPLATES[provider];
  if (!tmpl || !/^[a-z]{1,16}$/.test(kind) || !/^[A-Za-z0-9_-]{1,64}$/.test(id)) return null;
  const f = document.createElement("iframe");
  f.className = provider === "spotify" ? "consume-embed" : "consume-embed consume-embed-video";
  f.src = tmpl(kind, id);
  f.loading = "lazy";
  f.allow = "encrypted-media; clipboard-write; picture-in-picture; fullscreen";
  f.allowFullscreen = true;
  f.referrerPolicy = "strict-origin-when-cross-origin";
  f.title = provider + " player";
  return f;
}

// playPill loads the player on demand — a curated list is an audit surface,
// and forty frames phoning four providers on open is not one.
function playPill(descriptor, row) {
  const pill = pillLight("▶ play", () => {
    const open = row.querySelector(".consume-embed");
    if (open) { open.remove(); pill.textContent = "▶ play"; return; }
    const frame = embedFrame(descriptor);
    if (!frame) { showToast("no player for that link — open the original"); return; }
    row.append(frame);
    pill.textContent = "▾ hide";
  });
  return pill;
}

function consumeCuratedEditNote(en, row) {
  if (row.querySelector(".consume-note-row")) return;
  const form = el("div", "consume-note-row");
  const input = inputEl("why this one? (optional)");
  input.className = "consume-note-input";
  input.value = en.note || "";
  const save = async () => {
    const note = input.value.trim();
    // ⚠ An empty save is refused, here and at the endpoint: clearing a note
    // deletes the owner's own words, which is a vault edit rather than a save,
    // and a silent no-op must not be reported as "saved" either.
    if (!note) {
      showToast(en.note
        ? "an empty save keeps the note — to clear it, edit the note file in your vault"
        : "type a note, or cancel");
      return;
    }
    // The curated panel edits NOTES, not items: an entry curated from a pasted
    // link or an external bridge carries an `ext-…` item id no live store
    // holds, and the item route answered `no item "ext-…"`. The note's path is
    // the identity the projection actually names.
    if (!(await consumePost("/api/consume/curated/note", { path: en.path, item: en.itemId, note }))) return;
    showToast("note saved");
    await loadConsumeCurated();
  };
  input.onkeydown = (e) => {
    if (e.key === "Enter") { e.preventDefault(); save(); }
    if (e.key === "Escape") form.remove();
  };
  form.append(input, pillLight("save", save), pillLight("cancel", () => form.remove()));
  row.append(form);
  input.focus();
}

// ---- the sidebar ----
//
// Built once and updated in place: counts and the active entry change without
// replacing the buttons, so a click or keyboard focus is never lost to a
// repaint. The structure is rebuilt only when the streams or sources change.

const FEED_SIDE_TYPES = [["article", "Articles"], ["video", "Videos"], ["podcast", "Podcasts"], ["post", "Posts"]];
let feedSideShape = "";
let feedSideCollapsed = (() => { try { return JSON.parse(localStorage.getItem("manifest.feedSideCollapsed") || "{}"); } catch (e) { return {}; } })();

// feedSideActiveKey names the entry the current state corresponds to.
function feedSideActiveKey() {
  const f = feedFilter();
  if (f === "") return "inbox";
  if (f === "proposal") return "approvals";
  if (consumeView === "later" || consumeView === "later-done") return "later";
  if (consumeView === "today") return "today";
  if (consumeSub) return "source:" + consumeSub;
  if (consumeList) return "stream:" + consumeList;
  if (consumeType) return "type:" + consumeType;
  return consumeView === "all" ? "all" : "unread";
}

function feedSideHashFor(key) {
  if (key === "inbox") return "#/feed";
  if (key === "approvals") return "#/feed/approvals";
  if (key === "later") return "#/feed/later";
  if (key === "today") return "#/feed/today";
  if (key === "all") return "#/feed/all";
  if (key === "unread") return "#/feed/unread";
  const [kind, ...rest] = key.split(":");
  const val = rest.join(":");
  if (kind === "type") return consumeHashFor({ view: "unread", type: val });
  if (kind === "stream") return consumeHashFor({ view: "unread", list: val });
  if (kind === "source") return consumeHashFor({ view: "unread", sub: val });
  return "#/feed/unread";
}

function feedSideEntry(key, label, cls) {
  const b = el("a", "rdr-nav" + (cls ? " " + cls : ""));
  b.dataset.key = key;
  b.href = feedSideHashFor(key);
  b.append(el("span", "rdr-nav-label", label), el("span", "rdr-nav-n"));
  return b;
}

function renderFeedSide() {
  const side = els.feedSide;
  if (!side) return;
  const subs = consumeSubs.subscriptions || [];
  const streams = {};
  const loose = [];
  subs.forEach((s) => {
    if (s.list && s.list.toLowerCase() !== "unfiled") (streams[s.list] ||= []).push(s);
    else loose.push(s);
  });
  const names = Object.keys(streams).sort((a, b) => a.localeCompare(b));
  const shape = JSON.stringify([names.map((n) => [n, streams[n].map((s) => s.id + "|" + s.title + "|" + s.media)]), loose.map((s) => s.id + "|" + s.title + "|" + s.media), feedSideCollapsed]);
  if (shape !== feedSideShape || !side.firstChild) {
    feedSideShape = shape;
    side.replaceChildren();
    const sec = (label) => { const s = el("div", "rdr-side-sec"); if (label) s.append(el("div", "rdr-side-label micro-label", label)); side.append(s); return s; };
    const inbox = sec("");
    inbox.append(feedSideEntry("inbox", "Inbox"), feedSideEntry("approvals", "Approvals"));
    const read = sec("Read");
    read.append(feedSideEntry("unread", "Unread"), feedSideEntry("today", "Today"), feedSideEntry("later", "Later"), feedSideEntry("all", "All"));
    const media = sec("Media");
    FEED_SIDE_TYPES.forEach(([t, label]) => media.append(feedSideEntry("type:" + t, label, "rdr-nav-type rdr-" + t)));
    const st = sec("Streams");
    names.forEach((n) => {
      const open = !feedSideCollapsed[n];
      const headRow = el("div", "rdr-stream" + (open ? " open" : ""));
      const caret = el("button", "rdr-caret", open ? "▾" : "▸");
      caret.type = "button";
      caret.setAttribute("aria-label", (open ? "Collapse " : "Expand ") + n);
      caret.setAttribute("aria-expanded", String(open));
      caret.onclick = () => {
        feedSideCollapsed[n] = open;
        if (!open) delete feedSideCollapsed[n];
        try { localStorage.setItem("manifest.feedSideCollapsed", JSON.stringify(feedSideCollapsed)); } catch (e) {}
        renderFeedSide();
      };
      headRow.append(caret, feedSideEntry("stream:" + n, n, "rdr-nav-stream"));
      st.append(headRow);
      if (open) {
        const kids = el("div", "rdr-stream-kids");
        streams[n].forEach((s) => kids.append(feedSideEntry("source:" + s.id, s.title || s.id, "rdr-nav-source rdr-" + (s.media || "article"))));
        st.append(kids);
      }
    });
    if (loose.length) {
      if (names.length) st.append(el("div", "rdr-side-sublabel micro-label", "not in a stream"));
      loose.forEach((s) => st.append(feedSideEntry("source:" + s.id, s.title || s.id, "rdr-nav-source rdr-" + (s.media || "article"))));
    }
    if (!subs.length) st.append(el("div", "rdr-side-empty micro-label", "nothing followed yet"));
    const foot = el("div", "rdr-side-foot");
    const follow = el("button", "rdr-side-btn", "＋ follow a source");
    follow.type = "button";
    follow.onclick = () => {
      const go = () => {
        if (!consumeManageOpen) consumeTogglePanel("subscriptions");
        setTimeout(() => { const i = els.feedList.querySelector(".consume-add-input"); if (i) i.focus(); }, 60);
      };
      if (!consumeIsActiveView()) { consumeGo({ view: "unread" }); setTimeout(go, 150); } else go();
    };
    const keys = el("div", "rdr-side-keys micro-label", "j/k next · o open · m read · l later · e done · v original · ⇧A all read");
    foot.append(follow, keys);
    side.append(foot);
  }
  // counts and the active entry, in place
  const nav = consumeNav || {};
  const inboxN = els.feedNavBadge && !els.feedNavBadge.hidden ? Number(els.feedNavBadge.textContent) || 0 : 0;
  const approvalsN = (feedCache.proposals || []).length;
  const countOf = (key) => {
    if (key === "inbox") return inboxN;
    if (key === "approvals") return approvalsN;
    if (key === "unread") return nav.unread;
    if (key === "today") return nav.today;
    if (key === "later") return nav.later;
    if (key === "all") return null;
    const [kind, ...rest] = key.split(":"); const val = rest.join(":");
    if (kind === "type") return (nav.types || {})[val];
    if (kind === "stream") return (nav.streams || {})[val];
    if (kind === "source") return (nav.subs || {})[val];
    return null;
  };
  const active = feedSideActiveKey();
  side.querySelectorAll(".rdr-nav").forEach((a) => {
    const n = countOf(a.dataset.key);
    const slot = a.querySelector(".rdr-nav-n");
    const text = n ? String(n) : "";
    if (slot.textContent !== text) slot.textContent = text;
    a.classList.toggle("empty", !n && a.dataset.key !== "all" && a.dataset.key !== "inbox");
    const on = a.dataset.key === active;
    a.classList.toggle("on", on);
    if (on) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current");
  });
  if (els.feedView) els.feedView.classList.add("rdr-has-side");
}

// ---- keyboard ----
//
// The Google Reader bindings every reader inherited, on the list itself. They
// never fire while typing, with a modifier, or while another layer (⌘K, a
// sheet, the reading page) owns the keyboard.
window.addEventListener("keydown", (e) => {
  if (!els.feedView || els.feedView.hidden || !consumeIsActiveView()) return;
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  if (typeof typingInField === "function" && typingInField(e.target)) return;
  if (document.querySelector(".cmdbar:not([hidden]), .mf-sheet.open")) return;
  const c = consumeFind(consumeSel);
  switch (e.key) {
    case "j": case "ArrowDown": e.preventDefault(); consumeStep(1); break;
    case "k": case "ArrowUp": e.preventDefault(); consumeStep(-1); break;
    case "o": case "Enter": if (c) { e.preventDefault(); consumeOpen(c.id); } break;
    case "v": if (c && c.url) { e.preventDefault(); window.open(c.url, "_blank", "noopener"); } break;
    case "m": if (c) { e.preventDefault(); consumeToggleRead(c); } break;
    case "l": if (c) { e.preventDefault(); consumeToggleLater(c); } break;
    case "e": if (c && c.later && c.laterId) { e.preventDefault(); consumeLaterDone(c, !c.laterDone); } break;
    case "A": if (e.shiftKey) { e.preventDefault(); const b = els.feedList.querySelector(".consume-mark-all"); if (b && !b.hidden) b.click(); } break;
    case "Escape": if (typeof readClosePane === "function" && readClosePane()) e.preventDefault(); break;
  }
});

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
