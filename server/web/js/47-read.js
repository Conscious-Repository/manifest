// ---- READ: one article, beside the list or on its own page ----
//
// Reading used to expand inline inside the feed card; the dedicated page
// (2026-08-25) removed the cramped duplicate actions. The reader pass
// (2026-09-27) adds the pane: on a wide screen the article opens BESIDE the
// list, as in every desktop reader, and #/read/<id> remains the page a narrow
// screen, a link or a reload lands on. Both are drawn by the same functions
// below, so they cannot drift.
//
// ⚠ The body is PRE-SANITIZED HTML from consume/sanitize.go, not markdown, so
// it is assigned to innerHTML in exactly one place — readFillBody — and never
// run through renderMarkdown or sanitized client-side.

let readItem = null;   // the article on the reading PAGE
let readList = [];     // the ordering prev/next walks
let readLoading = null; // the id currently being fetched (race guard)
let readPaneItem = null; // the article in the PANE
let readPaneLoading = null;

// showRead is the route entry point.
function showRead(id) {
  els.readView.hidden = false;
  loadRead(id);
}

// readOpen reports whether the reading PAGE owns the screen — the flag-check
// convention the app's other Escape handlers follow.
function readOpen() { return els.readView && !els.readView.hidden; }

// openRead is what a row calls on a narrow screen. Ids carry colons, which
// encodeURIComponent turns into %3A and decodeURIComponent turns back — the
// same round trip #/note/<path> relies on for slashes.
function openRead(id) { location.hash = "#/read/" + encodeURIComponent(id); }

// readFetch returns one item's payload. A body fetched ahead (46-consume.js,
// ?peek=1) opens at once, and the read mark goes as its own small POST; a body
// not yet fetched is fetched plainly, which marks it read on the server.
async function readFetch(id) {
  const cache = typeof consumeBodies === "object" ? consumeBodies : null;
  const cached = cache && cache.get(id);
  if (cached) {
    if (!cached.read) {
      cached.read = true;
      fetch(`/api/consume/item/${encodeURIComponent(id)}/read`, { method: "POST" }).catch(() => {});
    }
    return cached;
  }
  const r = await fetch(`/api/consume/item/${encodeURIComponent(id)}`);
  if (!r.ok) throw new Error("HTTP " + r.status);
  const d = await r.json();
  d.read = true;
  if (cache) cache.set(id, d);
  return d;
}

// ---- the page ----

async function loadRead(id) {
  readLoading = id;
  els.readTitle.textContent = "";
  els.readMeta.replaceChildren();
  els.readFoot.replaceChildren();
  if (els.readPlayer) els.readPlayer.replaceChildren();
  readFillBody(els.readBody, null);
  els.readBody.append(el("div", "consume-loading micro-label", "loading…"));

  // The list prev/next walks. Warm from the reader when we came from it; on a
  // cold load (a reload, or a link) rebuild it so navigation still works.
  if (!readList.length || !readList.some((x) => x.id === id)) {
    readList = (typeof consumeCache === "object" && (consumeCache.items || []).some((x) => x.id === id))
      ? consumeCache.items.slice()
      : await readFetchList();
  }

  let d;
  try {
    d = await readFetch(id);
  } catch (e) {
    readFillBody(els.readBody, null);
    els.readBody.append(el("div", "consume-loading micro-label", "could not load this one"));
    return;
  }
  if (readLoading !== id) return; // raced a newer selection
  readItem = d;
  if (typeof consumeMarkOpened === "function") consumeMarkOpened(id);
  renderRead();
  els.contentScroll.scrollTop = 0;
  readPrefetchNext(id);
}

async function readFetchList() {
  try {
    const r = await (await fetch("/api/consume?view=all&limit=200")).json();
    return r.items || [];
  } catch (e) { return []; }
}

function renderRead() {
  const d = readItem;
  if (!d) return;
  els.readTitle.textContent = d.title || "(untitled)";
  els.readMeta.replaceChildren(...readMetaParts(d));
  if (els.readPlayer) els.readPlayer.replaceChildren(...readPlayerParts(d));
  readFillBody(els.readBody, d);
  els.readFoot.replaceChildren(...readFootParts(d, () => renderRead(), els.readFoot));
  renderReadNav();
}

// ---- the pane ----

// readOpenInPane shows an item beside the list.
async function readOpenInPane(id) {
  const pane = els.feedPane;
  if (!pane) { openRead(id); return; }
  pane.hidden = false;
  readPaneLoading = id;
  const cached = typeof consumeBodies === "object" && consumeBodies.get(id);
  if (!cached) readPaneSkeleton(pane, id);
  let d;
  try { d = await readFetch(id); } catch (e) {
    if (readPaneLoading !== id) return;
    pane.replaceChildren(readPaneBar(), el("div", "rdr-pane-empty micro-label", "could not load this one — try again, or open the original"));
    return;
  }
  if (readPaneLoading !== id) return; // raced a newer selection
  readPaneItem = d;
  if (typeof consumeMarkOpened === "function") consumeMarkOpened(id);
  renderReadPane();
  pane.scrollTop = 0;
}

function readPaneSkeleton(pane, id) {
  const c = typeof consumeFind === "function" ? consumeFind(id) : null;
  const art = el("article", "rdr-doc");
  if (c) {
    art.append(el("div", "read-meta", [c.source, c.author].filter(Boolean).join(" · ")));
    art.append(el("h1", "rdr-article-title", c.title || ""));
  }
  art.append(el("div", "consume-loading micro-label", "loading…"));
  pane.replaceChildren(readPaneBar(), art);
}

function readPaneBar() {
  const bar = el("div", "rdr-pane-bar");
  const up = el("button", "rdr-act", "↑"); up.type = "button"; up.title = "Previous (k)";
  up.onclick = () => (typeof consumeStep === "function" ? consumeStep(-1) : null);
  const down = el("button", "rdr-act", "↓"); down.type = "button"; down.title = "Next (j)";
  down.onclick = () => (typeof consumeStep === "function" ? consumeStep(1) : null);
  const close = el("button", "rdr-act", "close"); close.type = "button"; close.title = "Close the pane (Esc)";
  close.onclick = () => readClosePane();
  bar.append(up, down, close);
  return bar;
}

function renderReadPane() {
  const d = readPaneItem, pane = els.feedPane;
  if (!d || !pane) return;
  const art = el("article", "rdr-doc");
  const meta = el("div", "read-meta");
  meta.append(...readMetaParts(d));
  art.append(meta);
  const h = el("h1", "rdr-article-title");
  if (d.url) {
    const a = el("a", "", d.title || "(untitled)");
    a.href = d.url; a.target = "_blank"; a.rel = "noopener";
    h.append(a);
  } else h.textContent = d.title || "(untitled)";
  art.append(h);
  const player = readPlayerParts(d);
  if (player.length) { const holder = el("div", "read-player"); holder.append(...player); art.append(holder); }
  const body = el("div", "consume-body");
  readFillBody(body, d);
  art.append(body);
  const foot = el("div", "read-foot");
  foot.append(...readFootParts(d, () => renderReadPane(), foot));
  art.append(foot);
  pane.replaceChildren(readPaneBar(), art);
}

// readClosePane hides the pane; true when there was one to close.
function readClosePane() {
  const pane = els.feedPane;
  if (!pane || pane.hidden || !readPaneItem) return false;
  readPaneItem = null; readPaneLoading = null;
  readPaneIdle();
  if (typeof consumeSelect === "function") consumeSelect("", false);
  return true;
}

// readPaneIdle is the pane with nothing chosen yet.
function readPaneIdle() {
  const pane = els.feedPane;
  if (!pane) return;
  pane.replaceChildren(el("div", "rdr-pane-empty micro-label", "choose something to read · j / k to move"));
}

// readSyncLater keeps an open article's Later button true to the list.
function readSyncLater(id, c) {
  for (const [d, repaint] of [[readPaneItem, renderReadPane], [readItem, renderRead]]) {
    if (d && d.id === id) {
      d.later = c.later; d.laterId = c.laterId; d.laterDone = c.laterDone;
      repaint();
    }
  }
}

// ---- the parts both surfaces draw ----

function readMetaParts(d) {
  const parts = [];
  const bits = [d.source, d.author].filter(Boolean).filter((v, i, a) => a.indexOf(v) === i).join(" · ");
  if (bits) parts.push(el("span", "", bits));
  if (d.published) parts.push(el("span", "", fmtWhen(d.published)));
  if (d.duration) parts.push(el("span", "", Math.round(d.duration / 60) + " min"));
  else if (d.chars && !d.embed) parts.push(el("span", "", Math.max(1, Math.round(d.chars / 5 / 235)) + " min read"));
  if (d.preview) {
    parts.push(el("span", "read-preview-chip micro-label", d.preview === "paid" ? "paid post" : "preview only"));
  }
  if (d.later) parts.push(el("span", "read-preview-chip micro-label", d.laterDone ? "Later · done" : "in Later"));
  return parts;
}

// readPlayerParts: a video plays from an allowlisted template (46-consume.js
// embedFrame); an episode plays from the publisher's own enclosure.
function readPlayerParts(d) {
  const out = [];
  if (d.embed && typeof embedFrame === "function") {
    const f = embedFrame(d.embed);
    if (f) { f.loading = "eager"; out.push(f); }
  } else if (d.audio && /^https?:\/\//i.test(d.audio)) {
    const a = document.createElement("audio");
    a.className = "read-audio";
    a.controls = true;
    a.preload = "none";
    a.src = d.audio;
    out.push(a);
  }
  return out;
}

// readFillBody — THE innerHTML sink, server-sanitized at poll time.
function readFillBody(target, d) {
  target.innerHTML = (d && d.body) || "";
  if (d && !d.body) {
    target.append(el("div", "consume-loading micro-label", d.embed || d.audio ? "" : "no text — open the original"));
  }
}

// readFootParts: an honest ending, then the verbs.
function readFootParts(d, repaint, foot) {
  const parts = [];
  // ⚠ An honest ending. A 367-character stub trailing off into a bare
  // "Read more" reads like a bug in the reader; it is in fact the publisher
  // withholding the rest, and saying so is the whole point of the label.
  if (d.preview) {
    const note = el("div", "read-preview-note");
    note.append(el("span", "", d.preview === "paid"
      ? "This post is for the publisher's paying subscribers — what you see above is the whole preview they share."
      : "The publisher shares only a preview of this one."));
    if (d.url) note.append(linkEl("read the rest at the source ↗", d.url));
    parts.push(note);
  }

  const acts = el("div", "read-actions");
  acts.append(readLaterBtn(d, repaint));
  acts.append(readCurateBtn(d, repaint, foot));
  if (d.url) acts.append(pillLight("original ↗", () => window.open(d.url, "_blank", "noopener")));
  acts.append(pillLight("mark unread", async () => {
    if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/unread`))) return;
    d.read = false;
    const c = typeof consumeFind === "function" && consumeFind(d.id);
    if (c) { c.read = false; c.seeded = false; if (typeof consumeRepaint === "function") consumeRepaint(c); if (typeof consumeCountShift === "function") consumeCountShift(c, 1); }
    if (typeof consumeMemo === "object") consumeMemo.clear();
    showToast("kept unread");
  }));
  if (d.subId !== "_later") {
    acts.append(pillLight("dismiss", async () => {
      if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/dismiss`))) return;
      showToast("dismissed · undo", () => consumeUndismiss(d.id));
      if (typeof consumeMemo === "object") consumeMemo.clear();
      if (readOpen()) { readAdvance(1, true) || readBack(); return; }
      const row = typeof consumeRowEl === "function" && consumeRowEl(d.id);
      if (typeof consumeStep === "function") consumeStep(1);
      if (row) row.remove();
      if (typeof consumeForget === "function") consumeForget(d.id);
    }));
  }
  parts.push(acts);
  return parts;
}

function readLaterBtn(d, repaint) {
  const queued = d.later && !d.laterDone;
  const b = pillLight(queued ? "✓ in Later — done" : "◷ Later", async () => {
    if (queued && d.laterId) {
      if (!(await consumePost(`/api/consume/later/${encodeURIComponent(d.laterId)}/done`))) return;
      d.laterDone = true;
      showToast("done — out of the Later queue");
    } else {
      const res = await consumePostJSON("/api/consume/later", d.subId === "_later" && d.url ? { url: d.url } : { item: d.id });
      if (!res) return;
      d.later = true; d.laterDone = false; d.laterId = res.entry && res.entry.id;
      showToast("saved to Later");
    }
    const c = typeof consumeFind === "function" && consumeFind(d.id);
    if (c) { c.later = d.later; c.laterDone = d.laterDone; c.laterId = d.laterId; if (typeof consumeRepaint === "function") consumeRepaint(c); }
    if (typeof consumeMemo === "object") consumeMemo.clear();
    if (typeof loadConsumeSubs === "function") loadConsumeSubs();
    repaint();
  });
  if (!queued) b.classList.add("verdict-primary");
  b.title = queued ? "Mark done in Later (e)" : "Save to Later (l)";
  return b;
}

// readCurateBtn — ONE curate button, where the reading ends.
function readCurateBtn(d, repaint, foot) {
  if (d.curated) {
    const b = pillLight("curated ✓", async () => {
      if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/uncurate`))) return;
      d.curated = false;
      repaint();
    });
    b.classList.add("consume-curated-on");
    return b;
  }
  return pillLight("→ CURATE", () => readCurate(d, repaint, foot));
}

function readCurate(d, repaint, foot) {
  if (foot.querySelector(".consume-note-row")) return;
  const row = el("div", "consume-note-row");
  const input = inputEl("why this one? (optional)");
  input.className = "consume-note-input";
  const save = async () => {
    const note = input.value.trim();
    row.remove();
    if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/curate`, { note }))) return;
    d.curated = true;
    showToast("curated → public feed");
    repaint();
  };
  input.onkeydown = (e) => {
    if (e.key === "Enter") { e.preventDefault(); save(); }
    if (e.key === "Escape") { e.stopPropagation(); row.remove(); }
  };
  row.append(input, pillLight("curate", save), pillLight("cancel", () => row.remove()));
  foot.append(row);
  input.focus();
}

// ---- moving between articles on the page ----

function readIndex() {
  if (!readItem) return -1;
  return readList.findIndex((x) => x.id === readItem.id);
}

function renderReadNav() {
  const i = readIndex();
  els.readPos.textContent = i >= 0 && readList.length ? `${i + 1} of ${readList.length}` : "";
  els.readPrev.disabled = i <= 0;
  els.readNext.disabled = i < 0 || i >= readList.length - 1;
}

// readPrefetchNext warms the next page's body so j is instant.
function readPrefetchNext(id) {
  const i = readList.findIndex((x) => x.id === id);
  const next = readList[i + 1];
  if (next && typeof consumePeek === "function") consumePeek(next.id);
}

// readAdvance moves by delta. Returns false when there is nowhere to go, which
// is how dismiss decides between "next article" and "back to the list".
function readAdvance(delta, drop) {
  const i = readIndex();
  if (i < 0) return false;
  if (drop) readList.splice(i, 1); // the current one is gone; next takes its place
  const target = drop ? (delta > 0 ? i : i - 1) : i + delta;
  if (target < 0 || target >= readList.length) return false;
  openRead(readList[target].id);
  return true;
}

function readBack() {
  // Back to the reading list you came from: the reader's own address, not
  // the Inbox.
  location.hash = typeof consumeHash === "function" ? consumeHash() : "#/feed/unread";
}

// ---- keyboard on the page ----
//
// The Google Reader bindings every reader still uses. j/k and Escape are free;
// t, /, ⌘K, ⌘J, ⌘I and ⌘/ are already taken (75-bars.js).
window.addEventListener("keydown", (e) => {
  if (!readOpen()) return;
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  // ⚠ Bare keys must never fire while someone is typing — the curate note
  // field lives on this page.
  if (typeof typingInField === "function" && typingInField(e.target)) return;
  const d = readItem;
  switch (e.key) {
    case "j": e.preventDefault(); readAdvance(1); break;
    case "k": e.preventDefault(); readAdvance(-1); break;
    case "o": case "v":
      if (d && d.url) { e.preventDefault(); window.open(d.url, "_blank", "noopener"); }
      break;
    case "l": case "e": {
      const b = els.readFoot.querySelector(".read-actions button");
      if (b && d && (e.key === "l" ? !(d.later && !d.laterDone) : d.later && !d.laterDone)) { e.preventDefault(); b.click(); }
      break;
    }
    case "Escape": e.preventDefault(); readBack(); break;
  }
});

document.addEventListener("DOMContentLoaded", () => {
  if (els.readBackBtn) els.readBackBtn.addEventListener("click", readBack);
  if (els.readPrev) els.readPrev.addEventListener("click", () => readAdvance(-1));
  if (els.readNext) els.readNext.addEventListener("click", () => readAdvance(1));
});
