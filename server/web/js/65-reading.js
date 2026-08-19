// ---- READING (book shelf over the extrinsic zone) ----
let _books = [];

async function loadReading() {
  let d = { books: [] };
  try { d = await (await fetch("/api/reading")).json(); } catch (e) {}
  _books = d.books || [];
  renderShelf();
}

function renderShelf() {
  const strip = els.readingStrip, shelf = els.bookShelf;
  strip.innerHTML = ""; shelf.innerHTML = "";
  const q = (els.bookSearch.value || "").trim().toLowerCase();
  const match = (b) => !q || b.title.toLowerCase().includes(q) ||
    (b.authors || []).some((a) => a.display.toLowerCase().includes(q));

  // reading strip: currently-reading, always on top (independent of the filter)
  const reading = _books.filter((b) => b.status === "reading" && match(b));
  if (reading.length) {
    strip.append(el("div", "reading-strip-head", "Currently reading — " + reading.length));
    const row = el("div", "reading-strip-cards");
    reading.forEach((b) => row.append(readingCard(b)));
    strip.append(row);
  }

  // shelf: apply the status filter + search, then the chosen sort
  const filter = els.bookFilter.value;
  let rows = _books.filter((b) => (filter === "all" || b.status === filter) && match(b));
  rows.sort(shelfComparator(els.bookSort.value));
  shelf.append(shelfHeader());
  if (!rows.length) { shelf.append(el("div", "cp-empty", "No books match.")); return; }
  rows.forEach((b) => shelf.append(bookRow(b)));
  document.title = "Reading — " + _books.length;
}

function shelfComparator(key) {
  const cmp = {
    date: (a, b) => (b.dateRead || "").localeCompare(a.dateRead || "") || a.title.localeCompare(b.title),
    rating: (a, b) => (b.rating - a.rating) || (b.dateRead || "").localeCompare(a.dateRead || ""),
    title: (a, b) => a.title.localeCompare(b.title),
    year: (a, b) => (b.yearWritten || "").localeCompare(a.yearWritten || "") || a.title.localeCompare(b.title),
  };
  return cmp[key] || cmp.date;
}

function shelfHeader() {
  const h = el("div", "book-row book-head");
  h.append(el("span", "bk-title", "TITLE"), el("span", "bk-authors", "AUTHOR"),
    el("span", "bk-year", "YEAR"), el("span", "bk-rating", "RATING"), el("span", "bk-date", "READ"));
  return h;
}

function bookRow(b) {
  const row = el("div", "book-row");
  const title = el("span", "bk-title cp-clickable", b.title);
  title.onclick = () => { _noteReturn = "#/reading"; openNoteByPath(b.path); };
  row.append(title, authorsEl(b), el("span", "bk-year", b.yearWritten || ""));
  row.append(starsEl(b), el("span", "bk-date", b.dateRead || (b.status === "reading" ? "reading" : "")));
  return row;
}

function readingCard(b) {
  const c = el("div", "reading-card");
  const t = el("span", "reading-card-title cp-clickable", b.title);
  t.onclick = () => { _noteReturn = "#/reading"; openNoteByPath(b.path); };
  c.append(t, authorsEl(b));
  c.append(pill("✓ Mark read", async () => {
    await postJSON("/api/reading/finish", { path: b.path, rating: 0 });
    loadReading();
  }));
  return c;
}

function authorsEl(b) {
  const wrap = el("span", "bk-authors");
  (b.authors || []).forEach((a, i) => {
    if (i) wrap.append(document.createTextNode(", "));
    const link = el("span", "bk-author-link", a.display);
    link.onclick = (ev) => { ev.stopPropagation(); resolveAndOpen(a.key); };
    wrap.append(link);
  });
  return wrap;
}

// interactive 5-star rating; click sets, click the current value clears it
function starsEl(b) {
  const s = el("span", "bk-rating");
  for (let i = 1; i <= 5; i++) {
    const star = el("span", "bk-star" + (i <= b.rating ? " on" : ""), i <= b.rating ? "★" : "☆");
    star.onclick = async (ev) => {
      ev.stopPropagation();
      const val = b.rating === i ? 0 : i; // re-clicking the current rating clears it
      const nb = await postJSON("/api/reading/rating", { path: b.path, rating: val });
      Object.assign(b, nb);
      renderShelf();
    };
    s.append(star);
  }
  return s;
}

// resolve a wikilink target then open where it points (person → contact, else note)
async function resolveAndOpen(target) {
  try {
    const r = await (await fetch("/api/note/resolve?target=" + encodeURIComponent(target))).json();
    if (r.kind === "contact") location.hash = "#/contacts/" + encodeURIComponent(r.key);
    else if (r.kind === "note") { _noteReturn = "#/reading"; openNoteByPath(r.path); }
  } catch (e) {}
}

// ---- + book: name it, the catalogue fills the rest ----
// The contacts location picker's shape: type, get candidates from an open
// catalogue through our own server, pick one and the record is written with
// its author attached. Typing something the catalogue has never heard of is
// not a dead end — the last row always adds exactly what you typed.
function addBook() {
  const open = document.querySelector(".book-add");
  if (open) { open.querySelector("input").focus(); return; }
  const box = el("div", "book-add");
  const head = el("div", "book-add-head");
  head.append(el("span", "micro-label", "add a book"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => box.remove();
  head.append(x);
  const input = el("input", "book-add-input");
  input.type = "search";
  input.placeholder = "Title — or title and author…";
  const results = el("div", "book-add-results");
  const status = el("div", "book-add-status");
  box.append(head, input, results, status);
  els.bookShelf.before(box);
  input.focus();

  const create = async (title, authors, year, pages) => {
    status.textContent = "adding…";
    try {
      const nb = await postJSON("/api/reading/book", {
        title: title, authors: authors || [], year: year || "", pages: pages || 0, status: "reading",
      });
      box.remove();
      await loadReading();
      if (nb && nb.path) { _noteReturn = "#/reading"; openNoteByPath(nb.path); } // open to write in it
    } catch (e) { status.textContent = "✕ " + ((e && e.message) || "couldn't add the book"); }
  };

  const manualRow = (q) => {
    const row = el("button", "book-cand book-cand-manual");
    row.append(el("span", "book-cand-title", "Add “" + q + "”"),
      el("span", "book-cand-meta", "as typed — no catalogue match needed"));
    row.onclick = () => create(q, []);
    return row;
  };

  let seq = 0;
  const run = async (q) => {
    const mine = ++seq;
    results.innerHTML = "";
    if (q.length < 3) { status.textContent = ""; return; }
    status.textContent = "searching the catalogue…";
    let d = null;
    try {
      const res = await fetch("/api/reading/search?q=" + encodeURIComponent(q));
      if (!res.ok) throw new Error((await res.text()).trim() || "lookup failed");
      d = await res.json();
    } catch (e) {
      if (mine !== seq) return;
      // a failed lookup is not "no such book" — say so, and still let it land
      status.textContent = "✕ " + ((e && e.message) || "lookup failed");
      results.append(manualRow(q));
      return;
    }
    if (mine !== seq) return; // a later keystroke already owns the list
    const found = (d && d.books) || [];
    found.forEach((b) => {
      const row = el("button", "book-cand");
      row.append(el("span", "book-cand-title", b.title));
      const bits = [];
      if ((b.authors || []).length) bits.push(b.authors.join(", "));
      if (b.year) bits.push(b.year);
      if (b.pages) bits.push(b.pages + "pp");
      row.append(el("span", "book-cand-meta", bits.join(" · ")));
      row.onclick = () => create(b.title, b.authors || [], b.year, b.pages);
      results.append(row);
    });
    results.append(manualRow(q));
    status.textContent = found.length ? "" : "Nothing in the catalogue — add it as typed.";
    if (found.length && d.attribution) {
      const attr = el("a", "book-add-attr", d.attribution);
      attr.href = "https://openlibrary.org/";
      attr.target = "_blank"; attr.rel = "noopener";
      results.append(attr);
    }
  };

  let timer;
  input.addEventListener("input", () => {
    clearTimeout(timer);
    const q = input.value.trim();
    timer = setTimeout(() => run(q), 300); // one lookup per pause, not per key
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Escape") box.remove();
    if (e.key !== "Enter") return;
    e.preventDefault();
    const first = results.querySelector(".book-cand");
    if (first) first.click(); else if (input.value.trim()) create(input.value.trim(), []);
  });
}

if (els.bookSearch) els.bookSearch.addEventListener("input", renderShelf);
if (els.bookSort) els.bookSort.addEventListener("change", renderShelf);
if (els.bookFilter) els.bookFilter.addEventListener("change", renderShelf);
if (els.bookAddBtn) els.bookAddBtn.addEventListener("click", addBook);
