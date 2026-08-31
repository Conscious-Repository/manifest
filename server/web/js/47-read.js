// ---- READ: one article, its own page (consume plan, 2026-08-25) ----
//
// Reading used to expand inline inside the feed card. That was cramped, and it
// forced the card to grow a second copy of CURATE and `original ↗` at the foot
// of the article because the card's own action row had scrolled far above. A
// dedicated page removes the cause rather than patching the symptom.
//
// Modelled on the artifact reader (70-note.js): a section that borrows the note
// view's chrome, its own route, and one back button.
//
// ⚠ The body is PRE-SANITIZED HTML from consume/sanitize.go, not markdown, so
// it is assigned to innerHTML — never run through renderMarkdown, and never
// sanitized client-side. This is the same single sink the inline reader was.

let readItem = null;   // the article on screen
let readList = [];     // the ordering prev/next walks
let readLoading = null; // the id currently being fetched (race guard)

// showRead is the route entry point.
function showRead(id) {
  els.readView.hidden = false;
  loadRead(id);
}

// readOpen reports whether this view owns the screen — the flag-check
// convention the app's other Escape handlers follow.
function readOpen() { return els.readView && !els.readView.hidden; }

// openRead is what a card calls. Ids carry colons, which encodeURIComponent
// turns into %3A and decodeURIComponent turns back — the same round trip
// #/note/<path> relies on for slashes.
function openRead(id) { location.hash = "#/read/" + encodeURIComponent(id); }

async function loadRead(id) {
  readLoading = id;
  els.readTitle.textContent = "";
  els.readMeta.textContent = "";
  els.readFoot.innerHTML = "";
  els.readBody.innerHTML = "";
  els.readBody.append(el("div", "consume-loading micro-label", "loading…"));

  // The list prev/next walks. Warm from the CONSUME view when we came from it;
  // on a cold load (a reload, or a link) rebuild it so navigation still works.
  if (!readList.length || !readList.some((x) => x.id === id)) {
    readList = (typeof consumeCache === "object" && (consumeCache.items || []).length)
      ? consumeCache.items.slice()
      : await readFetchList();
  }

  let d;
  try {
    d = await (await fetch(`/api/consume/item/${encodeURIComponent(id)}`)).json();
  } catch (e) {
    els.readBody.innerHTML = "";
    els.readBody.append(el("div", "consume-loading micro-label", "could not load this one"));
    return;
  }
  if (readLoading !== id) return; // raced a newer selection
  readItem = d;
  renderRead();
  els.contentScroll.scrollTop = 0;
}

async function readFetchList() {
  try {
    const r = await (await fetch("/api/consume?view=all")).json();
    return r.items || [];
  } catch (e) { return []; }
}

function renderRead() {
  const d = readItem;
  if (!d) return;
  els.readTitle.textContent = d.title || "(untitled)";

  // meta line: who, when, how long, and whether this is only a preview
  els.readMeta.innerHTML = "";
  const bits = [d.source, d.author].filter(Boolean).join(" · ");
  if (bits) els.readMeta.append(el("span", "", bits));
  if (d.published) els.readMeta.append(el("span", "", fmtWhen(d.published)));
  if (d.preview) {
    els.readMeta.append(el("span", "read-preview-chip micro-label",
      d.preview === "paid" ? "paid post" : "preview only"));
  }

  // THE innerHTML sink — server-sanitized at poll time. See the file header.
  els.readBody.innerHTML = d.body || "";
  if (!d.body) {
    els.readBody.append(el("div", "consume-loading micro-label", "no body — open the original"));
  }

  // ⚠ An honest ending. A 367-character stub trailing off into a bare
  // "Read more" reads like a bug in the reader; it is in fact the publisher
  // withholding the rest, and saying so is the whole point of the label.
  els.readFoot.innerHTML = "";
  if (d.preview) {
    const note = el("div", "read-preview-note");
    note.append(el("span", "", d.preview === "paid"
      ? "This post is for the publisher's paying subscribers — what you see above is the whole preview they share."
      : "The publisher shares only a preview of this one."));
    if (d.url) note.append(linkEl("read the rest at the source ↗", d.url));
    els.readFoot.append(note);
  }

  const acts = el("div", "read-actions");
  acts.append(readCurateBtn());
  if (d.url) acts.append(pillLight("original ↗", () => window.open(d.url, "_blank", "noopener")));
  acts.append(pillLight("dismiss", async () => {
    if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/dismiss`))) return;
    showToast("dismissed · undo", () => consumeUndismiss(d.id));
    readAdvance(1, true) || readBack();
  }));
  els.readFoot.append(acts);

  renderReadNav();
}

// readCurateBtn — ONE curate button now, on the page where the reading ends.
function readCurateBtn() {
  const d = readItem;
  if (d.curated) {
    const b = pillLight("curated ✓", async () => {
      if (!(await consumePost(`/api/consume/item/${encodeURIComponent(d.id)}/uncurate`))) return;
      d.curated = false;
      renderRead();
    });
    b.classList.add("consume-curated-on");
    return b;
  }
  const b = pillLight("→ CURATE", () => readCurate());
  b.classList.add("verdict-primary");
  return b;
}

function readCurate() {
  if (els.readFoot.querySelector(".consume-note-row")) return;
  const row = el("div", "consume-note-row");
  const input = inputEl("why this one? (optional)");
  input.className = "consume-note-input";
  const save = async () => {
    const note = input.value.trim();
    row.remove();
    if (!(await consumePost(`/api/consume/item/${encodeURIComponent(readItem.id)}/curate`, { note }))) return;
    readItem.curated = true;
    showToast("curated → public feed");
    renderRead();
  };
  input.onkeydown = (e) => {
    if (e.key === "Enter") { e.preventDefault(); save(); }
    if (e.key === "Escape") { e.stopPropagation(); row.remove(); }
  };
  row.append(input, pillLight("curate", save), pillLight("cancel", () => row.remove()));
  els.readFoot.append(row);
  input.focus();
}

// ---- moving between articles ----

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
  // ⚠ CONSUME is a filter, not a route — without this you land on the
  // unfiltered inbox instead of the reading list you came from.
  state.feedFilter = "consume";
  location.hash = "#/feed";
}

// ---- keyboard ----
//
// The Google Reader bindings every reader still uses. j/k and Escape are free;
// t, /, ⌘K, ⌘J, ⌘I and ⌘/ are already taken (75-bars.js).
window.addEventListener("keydown", (e) => {
  if (!readOpen()) return;
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  // ⚠ Bare keys must never fire while someone is typing — the curate note
  // field lives on this page.
  if (typeof typingInField === "function" && typingInField(e.target)) return;
  switch (e.key) {
    case "j": e.preventDefault(); readAdvance(1); break;
    case "k": e.preventDefault(); readAdvance(-1); break;
    case "o":
      if (readItem && readItem.url) { e.preventDefault(); window.open(readItem.url, "_blank", "noopener"); }
      break;
    case "Escape": e.preventDefault(); readBack(); break;
  }
});

document.addEventListener("DOMContentLoaded", () => {
  if (els.readBackBtn) els.readBackBtn.addEventListener("click", readBack);
  if (els.readPrev) els.readPrev.addEventListener("click", () => readAdvance(-1));
  if (els.readNext) els.readNext.addEventListener("click", () => readAdvance(1));
});
