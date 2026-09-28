// ---- NETWORK — the people you know, and the people around AION ----
//
// ONE place for people (Contacts merged in, 2026-09-27): your vault contacts,
// fundraising investors, the AION team and the people you kept from sweeps.
// The list is on the left; the right is the person's page — what they are to
// you (kind, tags, last contact) above what the vault and calendar know
// (61-person.js: timeline, open loops, emails, location, note). The graph is
// a second view of the same people.
//
// Tracking is deliberately minimal: kind, tags, one last-contact date.
// There is no note field — a contact's note is their vault note (one fact,
// one place), edited on their page. Nothing here reminds or scores.
//
// ⚠ Rows join ONLY by explicit link (a kept row's contact ref / team link),
// never by name — two "Ben"s are two rows until the owner links them.
//
// ⚠ Saving repaints the LIST, not the inspector: a blur-save that rebuilt the
// inspector would steal focus from the field the owner just clicked into (the
// focus-stealing re-render class — "click twice to type").

let netCache = null;            // {people, kinds, tags, contacts, fundraising, team}
let netQuery = "";
let netKind = "";               // "" | hire | advisor | expert | connector | team
let netSource = "";             // "" | sweep | contact | investor | team
let netTag = "";                // a tag's TopicID-ish lowercase text
let netSel = null;              // selected person id
let netGraphStale = false;      // an edit landed since the graph last loaded
let netLens = "";               // "" | cold (going cold) | nearby
let netReviewOpen = false;      // the review queues are expanded

// list | graph is a per-viewer convenience, so it lives in the browser
function netViewMode() {
  try { return localStorage.getItem("manifest.network.view") === "graph" ? "graph" : "list"; } catch (e) { return "list"; }
}
function netSetViewMode(v) {
  try { localStorage.setItem("manifest.network.view", v); } catch (e) {}
}

const NET_KIND_WORD = { hire: "future hire", advisor: "advisor", expert: "expert", connector: "connector" };
const NET_SOURCE_WORD = { sweep: "from sweeps", contact: "contacts", investor: "investors", team: "team" };

// Where a person comes from, in words you'd use. "sweep" = someone you kept
// from a recruiting sweep. The internal "kept" source only means "has a
// network record" (every edited contact gets one, and the install wrote two
// for you and RJ), so it is never shown as a filter or a label.
function netFrom(p) {
  const out = (p.sources || []).filter((s) => s !== "kept");
  if (p.source && p.source !== "owner") out.unshift("sweep");
  return out;
}

// Routes: #/network · #/network/cold · #/network/review · #/network/<id>
// where <id> is a row id (contact/<key>, aion-net/…, team/<INI>). The old
// #/contacts links arrive here through the redirect in 99-boot.js.
function netRoute() {
  const rest = location.hash.replace(/^#\/network\/?/, "");
  netLens = rest === "cold" ? "cold" : netLens === "nearby" ? "nearby" : "";
  if (rest === "review") netReviewOpen = true;
  if (rest && rest !== "cold" && rest !== "review") {
    let id = rest;
    try { id = decodeURIComponent(rest); } catch (e) {}
    netSel = id;
    netSetViewMode("list");
  }
}

async function showNetwork() {
  const host = document.getElementById("networkView");
  netRoute();
  if (netCache) { netPaint(); netRefresh(false); return; }
  host.innerHTML = "";
  host.append(emptyRow("loading…"));
  try {
    const r = await fetch("/api/network");
    if (!r.ok) throw new Error((await r.text()) || "HTTP " + r.status);
    netCache = await r.json();
  } catch (e) {
    host.innerHTML = "";
    host.append(emptyRow("couldn't load people — " + String(e.message || e).slice(0, 120)));
    return;
  }
  netPaint();
  netLoadReviews();
}

// netRefresh re-reads the people and repaints the LIST only — never the page,
// where you may be typing. quiet keeps the review queues as they are.
async function netRefresh(quiet) {
  try {
    const r = await fetch("/api/network");
    if (r.ok) netCache = await r.json();
  } catch (e) { return; }
  netPaintList();
  netPaintLensCounts();
  if (!quiet) netLoadReviews();
}
cpRefresh = (quiet) => netRefresh(!!quiet);

function netLoadReviews() {
  if (!document.getElementById("netReview")) return;
  cpLoadReviews(netPaintLensCounts);
}

// the row a route or a click names — by id, or a contact by its key; a
// contact key no row carries (an org, a new note) still gets its page
function netPerson(id) {
  if (!id || !netCache) return null;
  const people = netCache.people || [];
  const hit = people.find((p) => p.id === id);
  if (hit) return hit;
  if (id.startsWith("contact/")) {
    const key = id.slice(8).toLowerCase();
    return people.find((p) => (p.contactKey || "").toLowerCase() === key) ||
      { id, name: id.slice(8), contactKey: id.slice(8), sources: ["contact"], stub: true };
  }
  return null;
}

// netGo selects a person without a route round-trip (the hash follows)
function netGo(id) {
  netSel = id;
  try { history.replaceState(null, "", "#/network/" + encodeURIComponent(id)); } catch (e) {}
  document.querySelectorAll(".net-row.sel").forEach((x) => x.classList.remove("sel"));
  const row = document.querySelector('.net-row[data-id="' + CSS.escape(id) + '"]');
  if (row) row.classList.add("sel");
  netPaintPage();
}

function netTagKey(t) { return String(t || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""); }

function netVisible() {
  const q = netQuery.trim().toLowerCase();
  const rows = (netCache.people || []).filter((p) => {
    if (p.archived) return false;
    if (netLens === "cold" && !p.cold) return false;
    if (netKind && p.kind !== netKind) return false;
    if (netSource && !netFrom(p).includes(netSource)) return false;
    if (netTag && !(p.tags || []).some((t) => netTagKey(t) === netTag)) return false;
    if (!q) return true;
    return [p.name, p.org, p.title, p.role, p.location, (p.tags || []).join(" "), (p.firms || []).join(" ")]
      .join(" ").toLowerCase().includes(q);
  });
  // going cold reads most-overdue first, as the Contacts lens did
  if (netLens === "cold") rows.sort((a, b) => (b.daysSince || 0) - (a.daysSince || 0));
  return rows;
}

// the newest of your own date, the calendar's and the notes', and which it is
function netRecency(p) {
  const all = [[p.lastContact, "touched"], [p.lastMet, "met"], [p.lastMentioned, "mentioned"]].filter(([d]) => d);
  if (!all.length) return null;
  all.sort((a, b) => b[0].localeCompare(a[0]));
  return { date: all[0][0], how: all[0][1] };
}

function netShortDate(d) {
  if (!d) return "";
  const t = new Date(d + "T12:00:00");
  if (isNaN(t)) return d;
  const now = new Date();
  const opts = t.getFullYear() === now.getFullYear() ? { month: "short", day: "numeric" } : { month: "short", year: "numeric" };
  return t.toLocaleDateString(undefined, opts);
}

function netPaint() {
  const host = document.getElementById("networkView");
  if (!host || !netCache) return;
  host.innerHTML = "";
  const mode = netViewMode();

  const head = el("div", "agent-head");
  head.append(el("span", "agent-title", "NETWORK"));
  const lenses = el("div", "net-lenses");
  const cold = el("button", "filter-chip net-cold-chip" + (netLens === "cold" ? " on" : ""), "going cold");
  cold.title = "people you usually see who are overdue — most overdue first";
  cold.onclick = () => { netLens = netLens === "cold" ? "" : "cold"; try { history.replaceState(null, "", netLens ? "#/network/cold" : "#/network"); } catch (e) {} netPaint(); };
  const near = el("button", "filter-chip" + (netLens === "nearby" ? " on" : ""), "nearby");
  near.title = "people near a city or place";
  near.onclick = () => { netLens = netLens === "nearby" ? "" : "nearby"; netPaint(); };
  const review = el("button", "filter-chip net-review-chip" + (netReviewOpen ? " on" : ""), "review");
  review.title = "names in your notes, calendar emails and pipeline people waiting for a decision";
  review.onclick = () => { netReviewOpen = !netReviewOpen; netPaint(); netLoadReviews(); };
  const add = el("button", "filter-chip", "+ person");
  add.title = "add a person — existing names are checked first";
  add.onclick = () => { netSetViewMode("list"); if (netLens === "nearby") { netLens = ""; netPaint(); } openCreatePanel(document.getElementById("netList")); };
  lenses.append(cold, near, review, add);
  const acts = el("div", "agent-actions");
  const search = el("input", "contact-search net-search");
  search.type = "search";
  search.placeholder = "search people, orgs, places, tags…";
  search.value = netQuery;
  search.oninput = () => { netQuery = search.value; if (netViewMode() === "graph") netDimGraph(); else netPaintList(); };
  acts.append(search);
  const shown = netVisible().length, total = (netCache.people || []).filter((p) => !p.archived).length;
  acts.append(el("span", "panel-meta net-count", shown === total ? total + " people" : shown + " of " + total));
  const toggle = el("div", "net-toggle");
  [["list", "list"], ["graph", "graph"]].forEach(([k, label]) => {
    const b = el("button", "filter-chip" + (mode === k ? " on" : ""), label);
    b.setAttribute("aria-pressed", String(mode === k));
    b.onclick = () => { if (mode !== k) { netSetViewMode(k); netPaint(); } };
    toggle.append(b);
  });
  acts.append(lenses, toggle);
  const sweeps = el("a", "rec-linkish net-sweeps", "people from sweeps →");
  sweeps.href = "#/aion/recruiting/people";
  sweeps.title = "everyone your recruiting sweeps named, ranked — keep the ones worth knowing";
  acts.append(sweeps);
  head.append(acts);
  host.append(head);

  host.append(netFacets());
  if (netTag) host.append(netLeverage());

  // the review queues (the old Contacts strips), one chip away
  const reviewBox = el("div", "net-review");
  reviewBox.id = "netReview";
  reviewBox.hidden = !netReviewOpen;
  ["triage", "email", "people"].forEach((k) => {
    const h = el("div", "contact-triage");
    h.hidden = true;
    cpHosts[k] = h;
    reviewBox.append(h);
  });
  host.append(reviewBox);

  // THE GRAPH is the same people laid out by who ties to whom — the
  // Recruiting renderer through the Network lens (97-rec-graph.js). The list's
  // filters still apply: whoever they leave out goes quiet in the picture.
  if (mode === "graph") {
    rgUseLens("network");
    const st = rgInit();
    if (netGraphStale && st.data && !st.busy) { netGraphStale = false; rgLoad(); }
    const box = el("div", "net-graph");
    host.append(box);
    rgView(box);
    return;
  }

  const split = el("div", "net-split");
  const left = el("div", "net-left");
  if (netLens === "nearby") {
    const panel = el("div", "contact-nearby");
    left.append(panel);
    cpNearby.panel = panel;
  }
  const list = el("div", "net-list");
  list.id = "netList";
  left.append(list);
  const page = el("section", "net-page");
  page.id = "netPage";
  split.append(left, page);
  host.append(split);
  netPaintLensCounts();
  _nearbyMode = netLens === "nearby";
  if (_nearbyMode) {
    cpNearby.list = list;
    cpNearby.open = (key) => netGo("contact/" + key);
    renderNearbyPanel();
  } else netPaintList();
  netPaintPage();
}

// the counts on the lens chips, updated in place
function netPaintLensCounts() {
  const cold = document.querySelector(".net-cold-chip");
  if (cold && netCache) {
    const n = (netCache.people || []).filter((p) => p.cold && !p.archived).length;
    cold.textContent = "going cold" + (n ? " " + n : "");
  }
  const review = document.querySelector(".net-review-chip");
  if (review) {
    const n = cpCounts.triage + cpCounts.email + cpCounts.people;
    review.textContent = "review" + (n ? " " + n : "");
  }
}

function netFacets() {
  const box = el("div", "net-facets");
  const people = (netCache.people || []).filter((p) => !p.archived);
  const chip = (label, on, onclick, count) => {
    const b = el("button", "filter-chip" + (on ? " on" : ""), label + (count != null ? " " + count : ""));
    b.onclick = onclick;
    return b;
  };
  const kinds = el("div", "net-facet-row");
  kinds.append(chip("everyone", !netKind, () => { netKind = ""; netPaint(); }));
  (netCache.kinds || []).forEach((k) => {
    const n = people.filter((p) => p.kind === k).length;
    kinds.append(chip(NET_KIND_WORD[k] || k, netKind === k, () => { netKind = netKind === k ? "" : k; netPaint(); }, n));
  });
  box.append(kinds);

  const srcs = el("div", "net-facet-row");
  srcs.append(el("span", "net-facet-label", "from"));
  ["sweep", "contact", "investor", "team"].forEach((s) => {
    const n = people.filter((p) => netFrom(p).includes(s)).length;
    if (!n) return;
    srcs.append(chip(NET_SOURCE_WORD[s], netSource === s, () => { netSource = netSource === s ? "" : s; netPaint(); }, n));
  });
  box.append(srcs);

  if ((netCache.tags || []).length) {
    const tags = el("div", "net-facet-row");
    tags.append(el("span", "net-facet-label", "tags"));
    (netCache.tags || []).slice(0, 24).forEach((t) => {
      const k = netTagKey(t.tag);
      tags.append(chip(t.tag, netTag === k, () => { netTag = netTag === k ? "" : k; netPaint(); }, t.count));
    });
    box.append(tags);
  }
  return box;
}

function netPaintList() {
  const list = document.getElementById("netList");
  if (!list || netLens === "nearby") return;
  list.innerHTML = "";
  const rows = netVisible();
  if (!rows.length) {
    list.append(emptyRow(netLens === "cold" ? "nobody is going cold"
      : (netCache.people || []).length ? "nobody matches — clear a filter"
      : "no one here yet — keep someone from a recruiting sweep, or tag a contact"));
    return;
  }
  // one render budget: the list is scanned, not scrolled forever
  rows.slice(0, 400).forEach((p) => list.append(netRow(p)));
  if (rows.length > 400) list.append(emptyRow((rows.length - 400) + " more — narrow the search"));
  const count = document.querySelector(".net-count");
  if (count) {
    const total = (netCache.people || []).filter((p) => !p.archived).length;
    count.textContent = rows.length === total ? total + " people" : rows.length + " of " + total;
  }
}

function netRow(p) {
  const row = el("div", "net-row" + (netSel === p.id ? " sel" : "") + (p.cold ? " cold" : ""));
  row.tabIndex = 0;
  row.dataset.id = p.id;
  const main = el("div", "net-main");
  if (p.cold) main.append(el("span", "contact-cold", "● going cold"));
  const top = el("div", "net-top");
  top.append(el("span", "net-name", p.name));
  if (p.kind) top.append(el("span", "net-kind k-" + p.kind, NET_KIND_WORD[p.kind] || p.kind));
  if (p.location) top.append(el("span", "contact-location", p.location));
  if (p.contactKey && p.hasNote === false) top.append(el("span", "contact-dot", "○"));
  if (p.openLoops > 0) top.append(el("span", "contact-loops", p.openLoops + " open"));
  main.append(top);
  const sub = [];
  if (p.title) sub.push(p.title);
  if (p.org) sub.push(p.org);
  if (p.role) sub.push(p.role);
  if ((p.firms || []).length) sub.push(p.firms.join(", "));
  if (sub.length) main.append(el("div", "net-sub", sub.join(" · ")));
  if ((p.tags || []).length) {
    const line = el("div", "net-line");
    p.tags.forEach((t) => line.append(el("span", "net-tag", t)));
    main.append(line);
  }
  row.append(main);
  const side = el("div", "net-side");
  if (p.upcoming) side.append(el("span", "contact-upcoming", "↑ " + netShortDate(p.upcoming)));
  const rec = netRecency(p);
  if (p.cold && p.daysSince >= 0) {
    side.append(el("span", "net-when", (p.neglectBasis === "meetings" ? "met " : "mentioned ") + p.daysSince + "d ago" +
      (p.medianGap ? " · usually every " + p.medianGap + "d" : "")));
  } else side.append(el("span", "net-when" + (rec ? "" : " none"), rec ? rec.how + " " + netShortDate(rec.date) : "—"));
  const srcs = netFrom(p).filter((s) => s !== "contact");
  if (srcs.length) side.append(el("span", "net-src", srcs.map((s) => NET_SOURCE_WORD[s] || s).join(" · ")));
  if ((p.sameName || []).length) side.append(el("span", "net-src net-same", "same name ×" + (p.sameName.length + 1)));
  row.append(side);
  const pick = () => netGo(p.id);
  row.onclick = pick;
  row.onkeydown = (e) => { if (e.key === "Enter") pick(); };
  return row;
}

// netSave writes one set of fields and folds the answer back into the cache.
// The inspector is NOT rebuilt (focus); the list is.
async function netSave(p, set) {
  try {
    const out = await postJSONOk("/api/network/person/" + encodeURIComponent(p.id), { set });
    const kept = out.person || {};
    const wasID = p.id;
    // a contact/team row's first edit ADOPTS it: the id becomes the kept row's
    if (kept.id && kept.id !== p.id) {
      p.id = kept.id;
      p.editable = true;
      if (!(p.sources || []).includes("kept")) p.sources = ["kept"].concat(p.sources || []);
      if (netSel === wasID) netSel = kept.id;
    }
    if ("type" in kept || "kind" in set) p.kind = kept.type || "";
    p.tags = kept.tags || [];
    p.lastContact = kept.lastContact || "";
    if ("team" in set) p.team = kept.team || "";
    netGraphStale = true;
    netPaintList();
    return true;
  } catch (e) {
    showToast("couldn't save — " + String(e.message || e).slice(0, 120), null, "error");
    return false;
  }
}

// THE PERSON PAGE: what they are to you first (the fields you build the
// network with), then what the vault and calendar know (61-person.js).
function netPaintPage() {
  const insp = document.getElementById("netPage");
  if (!insp) return;
  insp.innerHTML = "";
  const p = netPerson(netSel);
  if (!p) {
    cpPageHost = null;
    insp.append(el("div", "aion-insp-empty", "select someone — their page opens here: what they are to you, what they know, and everything the vault and calendar have on them"));
    return;
  }
  const head = el("div", "net-page-head");
  head.append(el("h1", "net-page-name", p.name));
  const x = el("button", "aion-insp-x", "✕");
  x.setAttribute("aria-label", "close");
  x.onclick = () => { netSel = null; try { history.replaceState(null, "", "#/network"); } catch (e) {} netPaintList(); netPaintPage(); };
  head.append(x);
  insp.append(head);
  const sub = [p.title, p.org, p.role].filter(Boolean).join(" · ");
  if (sub) insp.append(el("div", "net-sub", sub));
  const building = el("div", "net-build");
  insp.append(building);
  netPaintBuild(p, building);

  if (p.contactKey) {
    const cp = el("div", "contact-page net-contact-page");
    insp.append(cp);
    cpPageHost = cp;
    cpPageBare = true;
    showContactPage(p.contactKey);
  } else {
    cpPageHost = null;
    netPaintKept(p, insp);
  }
}

// netPaintKept is the page of someone with no contact note yet: where they
// came from, and the one explicit way to make them a contact.
function netPaintKept(p, host) {
  const sec = el("div", "cp-section");
  sec.append(el("div", "cp-section-head", "Not a contact yet"));
  if (p.source && p.source !== "owner") sec.append(el("div", "net-insp-hint", "kept from " + p.source + (p.sourceRef ? " · " + p.sourceRef : "")));
  if ((p.suggest || []).length) sec.append(el("div", "net-insp-hint", "the source says: " + p.suggest.join(" · ")));
  const make = el("button", "pill", "make a contact note");
  make.title = "creates " + p.name + ".md with categories: [people] and links this row to it";
  make.onclick = async () => {
    make.disabled = true;
    try {
      const np = await postJSONOk("/api/contacts/note", { key: p.name.toLowerCase(), display: p.name, body: "" });
      const key = np.key || p.name.toLowerCase();
      if (p.editable || p.team) await netSave(p, { ref: key });
      showToast(p.name + " is a contact");
      netCache = null;
      location.hash = personHref(key);
    } catch (e) { showToast("couldn't create the note — " + String(e.message || e).slice(0, 120), null, "error"); make.disabled = false; }
  };
  sec.append(make);
  host.append(sec);
}

// netPaintBuild: kind, tags, last contact — the building fields — plus the
// same-name links. Saves repaint the list only (focus stays where you are).
function netPaintBuild(p, insp) {
  if (netCache.editable === false || p.stub) return;

  const field = (label, node) => {
    const f = el("div", "aion-insp-field net-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    insp.append(f);
    return node;
  };

  // kind
  const kind = document.createElement("select");
  kind.className = "pp-in";
  [["", "— kind"]].concat((netCache.kinds || []).map((k) => [k, NET_KIND_WORD[k] || k])).forEach(([v, l]) => {
    const o = el("option", "", l); o.value = v; o.selected = (p.kind || "") === v; kind.append(o);
  });
  kind.onchange = () => netSave(p, { kind: kind.value });
  field("kind", kind);

  // tags: comma-separated, autocompleted from the tags already in use
  const tags = el("input", "pp-in");
  tags.type = "text";
  tags.placeholder = "e.g. fda-510k, mri-coils";
  tags.value = (p.tags || []).join(", ");
  const listId = "netTagList";
  let dl = document.getElementById(listId);
  if (!dl) { dl = document.createElement("datalist"); dl.id = listId; document.body.append(dl); }
  dl.innerHTML = "";
  (netCache.vocabulary || (netCache.tags || []).map((t) => t.tag)).forEach((t) => {
    const o = document.createElement("option"); o.value = t; dl.append(o);
  });
  tags.setAttribute("list", listId);
  let tagsWas = tags.value;
  tags.onblur = () => { if (tags.value !== tagsWas) { tagsWas = tags.value; netSave(p, { tags: tags.value }); } };
  tags.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); tags.blur(); } };
  field("tags", tags);

  // what the SOURCE said they know, offered one click at a time — a topic is
  // a suggestion until the owner accepts it; nothing tags itself
  if ((p.suggest || []).length) {
    const sug = el("div", "net-suggest");
    sug.append(el("span", "net-facet-label", "suggested"));
    p.suggest.forEach((t) => {
      const b = el("button", "net-sug", "+ " + t);
      b.title = "named by " + (p.source || "the source") + " — add as a tag";
      b.onclick = async () => {
        const next = (tags.value.trim() ? tags.value.replace(/[,\s]+$/, "") + ", " : "") + t;
        tags.value = next;
        tagsWas = next;
        if (await netSave(p, { tags: next })) {
          p.suggest = (p.suggest || []).filter((x) => x !== t);
          b.remove();
          if (!sug.querySelector(".net-sug")) sug.remove();
        }
      };
      sug.append(b);
    });
    insp.append(sug);
  }

  // last contact: a date, or "today" in one click
  const lc = el("span", "net-lc");
  const date = el("input", "pp-in");
  date.type = "date";
  date.value = p.lastContact || "";
  date.onchange = () => netSave(p, { last_contact: date.value });
  const today = el("button", "pill light", "today");
  today.onclick = () => {
    const d = new Date();
    const iso = d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
    date.value = iso;
    netSave(p, { last_contact: iso });
  };
  lc.append(date, today);
  field("last contact", lc);

  // on the team: a fact the roster states, shown — not a second picker
  if (p.team) insp.append(el("div", "net-insp-hint", "AION team · " + [p.role, p.team].filter(Boolean).join(" · ")));

  // SAME NAME, NOT YET THE SAME PERSON. Rows join only by an explicit link
  // (never by name), so an exact name match is offered for you to confirm —
  // one click writes the contact or team link, nothing is inferred.
  (p.sameName || []).forEach((alias) => {
    const other = (netCache.people || []).find((x) => x.id === alias.id);
    if (!other) return;
    const box = el("div", "net-same-box");
    box.append(el("span", "net-insp-hint", "also in your " +
      netFrom(other).map((s) => NET_SOURCE_WORD[s] || s).join(" + ") + " as " + other.name));
    const yes = el("button", "pill light", "same person — link");
    yes.title = "writes one explicit link; a wrong link can be undone by clearing it";
    yes.onclick = async () => {
      yes.disabled = true;
      // write on whichever side holds (or can hold) the row, naming the other's key
      let target = p, set = null;
      if (!p.contactKey && other.contactKey) set = { ref: other.contactKey };
      else if (!p.team && other.team) set = { team: other.team };
      if (!set || (!p.editable && other.editable)) {
        target = other;
        set = (!other.contactKey && p.contactKey) ? { ref: p.contactKey } : { team: p.team };
      }
      if (await netSave(target, set)) { showToast(p.name + " linked"); showNetwork(); }
      else yes.disabled = false;
    };
    box.append(yes);
    insp.append(box);
  });

  // LINKED — when one row joins two identities (a contact note and a team
  // member, or a sweep and a contact), each link can be undone. A wrong link
  // is one click to fix; nothing was merged but the link itself.
  const ids = [];
  if (p.editable && p.contactKey) ids.push(["contact", p.contactKey, { ref: "" }]);
  if (p.editable && p.team) ids.push(["team", p.team, { team: "" }]);
  if (ids.length > 1 || (ids.length && p.source && p.source !== "owner")) {
    const box = el("div", "net-same-box");
    box.append(el("span", "net-insp-hint", "one person across"));
    ids.forEach(([what, val, clear]) => {
      const b = el("button", "net-sug", what + " " + val + " ✕");
      b.title = "not the same person — undo this link (nothing else changes)";
      b.onclick = async () => { b.disabled = true; if (await netSave(p, clear)) { showToast("unlinked"); netCache = null; showNetwork(); } else b.disabled = false; };
      box.append(b);
    });
    insp.append(box);
  }

  // where they live elsewhere
  const links = el("div", "net-links");
  const link = (label, href, internal) => {
    const a = el("a", "rec-linkish", label);
    a.href = href;
    if (!internal) { a.target = "_blank"; a.rel = "noopener"; }
    links.append(a);
  };
  if (p.orcid) link("orcid ↗", p.orcid);
  if (p.github) link("github ↗", p.github);
  if (p.linkedin) link("linkedin ↗", p.linkedin);
  const inGraph = el("a", "rec-linkish", "in the graph →");
  inGraph.href = "#/network";
  inGraph.onclick = (e) => {
    e.preventDefault();
    netSetViewMode("graph");
    rgUseLens("network");
    const st = rgInit();
    st.sel = p.id; st.reveal = p.id;
    netPaint();
  };
  links.append(inGraph);
  if (links.children.length) insp.append(links);

  if ((p.firms || []).length) insp.append(el("div", "net-insp-hint", "investor · " + p.firms.join(", ")));
  if (p.source && p.source !== "owner") insp.append(el("div", "net-insp-hint", "kept from " + p.source + (p.sourceRef ? " · " + p.sourceRef : "")));
}

// ---- the graph's hooks into the list (97-rec-graph.js calls these)

function netHas(id) { return !!netCache && (netCache.people || []).some((p) => p.id === id); }

// netOpen is "edit in the list": the graph hands a person back to the row
// where kind, tags, note and last contact are edited.
function netOpen(id) {
  netQuery = "";
  netKind = netSource = netTag = "";
  netLens = "";
  netSetViewMode("list");
  netSel = id;
  netPaint();
  netGo(id);
  const row = document.querySelector("#netList .net-row.sel");
  if (row) row.scrollIntoView({ block: "center" });
}

// netDimmed answers, for the graph, whether the list's filters leave a person
// out. Nobody is dimmed while no filter is set.
function netDimmed(id) {
  if (!netCache || !(netQuery.trim() || netKind || netSource || netTag)) return false;
  if (!netHas(id)) return true;
  return !netVisible().some((p) => p.id === id);
}

function netDimGraph() {
  const st = typeof rgState !== "undefined" && rgState;
  if (!st || !st.sim || rgLens !== "network") return;
  const keep = new Set(netVisible().map((p) => p.id));
  const on = !!(netQuery.trim() || netKind || netSource || netTag);
  st.sim.nodes.forEach((n) => n.el.classList.toggle("rg-dim", on && !keep.has(n.id)));
  const count = document.querySelector(".net-count");
  if (count) {
    const total = (netCache.people || []).filter((p) => !p.archived).length;
    count.textContent = keep.size === total ? total + " people" : keep.size + " of " + total;
  }
}

// ---- a tag answers "who can get me there": the leverage ranking
// (recruiting/leverage.go) — expertise in the topic × how tied the person is
// to people you know, every component visible. Read-only; asked once per tag.
const netLevCache = {};

function netLeverage() {
  const box = el("div", "net-lev");
  const tag = (netCache.tags || []).map((t) => t.tag).find((t) => netTagKey(t) === netTag) || netTag;
  box.append(el("span", "net-facet-label", "reach"));
  const body = el("div", "net-lev-body");
  box.append(body);
  const paint = (res) => {
    body.innerHTML = "";
    if (!res) { body.append(el("span", "net-sub", "asking the graph…")); return; }
    if (res.error) { body.append(el("span", "net-sub", "the graph couldn't answer — " + res.error)); return; }
    if (!(res.people || []).length) {
      body.append(el("span", "net-sub", res.known
        ? "the sources know experts in " + tag + ", but none is tied to anyone you know yet"
        : "no source has named expertise in " + tag + " yet — the tags you add are the record"));
      return;
    }
    body.append(el("span", "net-sub", "who the sources say knows " + tag + ", ranked by how close they are to you"));
    const row = el("div", "net-lev-people");
    res.people.slice(0, 8).forEach((lp) => {
      const b = el("button", "filter-chip net-lev-p", lp.name + " · " + (lp.role === "expert" ? "expert" : "adjacent") +
        (lp.tieCount ? " · " + lp.tieCount + (lp.tieCount === 1 ? " tie" : " ties") : ""));
      b.title = "leverage " + lp.leverage + " = knowledge " + lp.knowledge + " × connectivity " + lp.connectivity;
      b.onclick = () => {
        if (netHas(lp.id)) netGo(lp.id);
        else if (String(lp.id).startsWith("cand/")) location.hash = "#/aion/recruiting/candidate/" + encodeURIComponent(lp.id);
        else showToast(lp.name + " isn't in your network yet — keep them from a sweep");
      };
      row.append(b);
    });
    body.append(row);
  };
  const key = netTag;
  if (netLevCache[key]) paint(netLevCache[key]);
  else {
    paint(null);
    fetch("/api/aion/recruiting/leverage?limit=8&topic=" + encodeURIComponent(tag))
      .then(async (r) => r.ok ? r.json() : { error: (await r.text()).slice(0, 120) })
      .catch((e) => ({ error: String(e.message || e).slice(0, 120) }))
      .then((res) => { netLevCache[key] = res; if (netTag === key && box.isConnected) paint(res); });
  }
  return box;
}
