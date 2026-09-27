// ---- NETWORK — the people around AION (owner decisions 2026-09-27) ----
//
// Future hires, advisors, experts to consult, connectors — plus the vault's
// contacts, the fundraising investors and the team, drawn read-only beside
// them. LIST FIRST: building a network is scanning, tagging and noting; the
// graph is a second view of the same people (Phase 2).
//
// Tracking is deliberately minimal: one note, one last-contact date. Nothing
// here reminds, schedules or scores.
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
let netSource = "";             // "" | kept | contact | investor | team
let netTag = "";                // a tag's TopicID-ish lowercase text
let netSel = null;              // selected person id
let netGraphStale = false;      // an edit landed since the graph last loaded

// list | graph is a per-viewer convenience, so it lives in the browser
function netViewMode() {
  try { return localStorage.getItem("manifest.network.view") === "graph" ? "graph" : "list"; } catch (e) { return "list"; }
}
function netSetViewMode(v) {
  try { localStorage.setItem("manifest.network.view", v); } catch (e) {}
}

const NET_KIND_WORD = { hire: "future hire", advisor: "advisor", expert: "expert", connector: "connector", team: "team" };
const NET_SOURCE_WORD = { kept: "kept", contact: "contacts", investor: "investors", team: "team" };

async function showNetwork() {
  const host = document.getElementById("networkView");
  if (!netCache) {
    host.innerHTML = "";
    host.append(emptyRow("loading…"));
  }
  try {
    const r = await fetch("/api/network");
    if (!r.ok) throw new Error((await r.text()) || "HTTP " + r.status);
    netCache = await r.json();
  } catch (e) {
    host.innerHTML = "";
    host.append(emptyRow("couldn't load the network — " + String(e.message || e).slice(0, 120)));
    return;
  }
  netPaint();
}

function netTagKey(t) { return String(t || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""); }

function netVisible() {
  const q = netQuery.trim().toLowerCase();
  return (netCache.people || []).filter((p) => {
    if (p.archived) return false;
    if (netKind && p.kind !== netKind) return false;
    if (netSource && !(p.sources || []).includes(netSource)) return false;
    if (netTag && !(p.tags || []).some((t) => netTagKey(t) === netTag)) return false;
    if (!q) return true;
    return [p.name, p.org, p.title, p.note, p.role, (p.tags || []).join(" "), (p.firms || []).join(" ")]
      .join(" ").toLowerCase().includes(q);
  });
}

// the newer of the owner's date and the calendar's, with which one it is
function netRecency(p) {
  const own = p.lastContact || "", met = p.lastMet || "";
  if (!own && !met) return null;
  return own >= met ? { date: own, how: "touched" } : { date: met, how: "met" };
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
  const acts = el("div", "agent-actions");
  const search = el("input", "contact-search net-search");
  search.type = "search";
  search.placeholder = "search people, orgs, tags, notes…";
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
  acts.append(toggle);
  const sweeps = el("a", "rec-linkish net-sweeps", "people from sweeps →");
  sweeps.href = "#/aion/recruiting/people";
  sweeps.title = "everyone your recruiting sweeps named, ranked — keep the ones worth knowing";
  acts.append(sweeps);
  head.append(acts);
  host.append(head);

  host.append(netFacets());
  if (netTag) host.append(netLeverage());

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

  const split = el("div", "aion-backlog net-split");
  const list = el("div", "aion-list net-list");
  list.id = "netList";
  const insp = el("aside", "aion-inspector net-insp");
  insp.id = "netInsp";
  split.append(list, insp);
  host.append(split);
  netPaintList();
  netPaintInspector();
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
  ["kept", "contact", "investor", "team"].forEach((s) => {
    const n = people.filter((p) => (p.sources || []).includes(s)).length;
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
  if (!list) return;
  list.innerHTML = "";
  const rows = netVisible();
  if (!rows.length) {
    list.append(emptyRow((netCache.people || []).length
      ? "nobody matches — clear a filter"
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
  const row = el("div", "net-row" + (netSel === p.id ? " sel" : ""));
  row.tabIndex = 0;
  const main = el("div", "net-main");
  const top = el("div", "net-top");
  top.append(el("span", "net-name", p.name));
  if (p.kind) top.append(el("span", "net-kind k-" + p.kind, NET_KIND_WORD[p.kind] || p.kind));
  main.append(top);
  const sub = [];
  if (p.title) sub.push(p.title);
  if (p.org) sub.push(p.org);
  if (p.role) sub.push(p.role);
  if ((p.firms || []).length) sub.push(p.firms.join(", "));
  if (sub.length) main.append(el("div", "net-sub", sub.join(" · ")));
  if ((p.tags || []).length || p.note) {
    const line = el("div", "net-line");
    (p.tags || []).forEach((t) => line.append(el("span", "net-tag", t)));
    if (p.note) line.append(el("span", "net-note", p.note));
    main.append(line);
  }
  row.append(main);
  const side = el("div", "net-side");
  const rec = netRecency(p);
  side.append(el("span", "net-when" + (rec ? "" : " none"), rec ? rec.how + " " + netShortDate(rec.date) : "—"));
  side.append(el("span", "net-src", (p.sources || []).map((s) => NET_SOURCE_WORD[s] || s).join(" · ")));
  row.append(side);
  const pick = () => {
    netSel = p.id;
    document.querySelectorAll(".net-row.sel").forEach((x) => x.classList.remove("sel"));
    row.classList.add("sel");
    netPaintInspector();
  };
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
    p.note = kept.note || "";
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

function netPaintInspector() {
  const insp = document.getElementById("netInsp");
  if (!insp) return;
  insp.innerHTML = "";
  const p = (netCache.people || []).find((x) => x.id === netSel);
  if (!p) {
    insp.append(el("div", "aion-insp-empty", "select someone — tag them, note why they matter, date the last touch"));
    return;
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Person"));
  const x = el("button", "aion-insp-x", "✕");
  x.setAttribute("aria-label", "close");
  x.onclick = () => { netSel = null; netPaintList(); netPaintInspector(); };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "net-insp-name", p.name));
  const sub = [p.title, p.org, p.role].filter(Boolean).join(" · ");
  if (sub) insp.append(el("div", "net-sub", sub));
  if (!p.editable) {
    insp.append(el("div", "net-insp-hint",
      "from your " + (p.sources || []).map((s) => NET_SOURCE_WORD[s] || s).join(" + ") +
      " — editing here keeps a network record linked to them; their note is never touched"));
  }

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

  // note
  const note = el("input", "pp-in");
  note.type = "text";
  note.placeholder = "why they matter, in a line";
  note.value = p.note || "";
  const noteWas = note.value;
  note.onblur = () => { if (note.value !== noteWas) netSave(p, { note: note.value }); };
  note.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); note.blur(); } };
  field("note", note);

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
  if (p.lastMet) insp.append(el("div", "net-insp-hint", "calendar: last met " + netShortDate(p.lastMet)));

  // an explicit team link — the only way a kept row and a team member merge
  if ((p.sources || []).includes("kept") || p.editable) {
    const teamPeople = (netCache.people || []).filter((x) => (x.sources || []).includes("team") && x.team);
    if (teamPeople.length) {
      const team = document.createElement("select");
      team.className = "pp-in";
      const none = el("option", "", "— not on the team"); none.value = ""; team.append(none);
      teamPeople.forEach((t) => {
        const o = el("option", "", t.name + " (" + t.team + ")"); o.value = t.team;
        o.selected = (p.team || "") === t.team; team.append(o);
      });
      team.onchange = async () => { if (await netSave(p, { team: team.value })) showNetwork(); };
      field("team link", team);
    }
  }

  // an explicit contact link — how a team member or a kept person who is ALSO
  // one of your contacts becomes one row. Offered only when there is no link
  // yet; picking one writes `ref` and nothing ever touches the note itself.
  if (!p.contactKey) {
    const contactsOnly = (netCache.people || []).filter((x) => x.id.startsWith("contact/") && x.contactKey);
    if (contactsOnly.length) {
      const inp = el("input", "pp-in");
      inp.type = "text";
      inp.placeholder = "same person as a contact? type their name";
      const dlId = "netContactList";
      let cdl = document.getElementById(dlId);
      if (!cdl) { cdl = document.createElement("datalist"); cdl.id = dlId; document.body.append(cdl); }
      cdl.innerHTML = "";
      contactsOnly.forEach((c) => { const o = document.createElement("option"); o.value = c.name; cdl.append(o); });
      inp.setAttribute("list", dlId);
      inp.onchange = async () => {
        const hit = contactsOnly.find((c) => c.name.toLowerCase() === inp.value.trim().toLowerCase());
        if (!hit) { showToast("pick a contact from the list"); return; }
        if (await netSave(p, { ref: hit.contactKey })) { showToast(p.name + " linked to " + hit.name); showNetwork(); }
      };
      field("contact", inp);
    }
  }

  // where they live elsewhere
  const links = el("div", "net-links");
  const link = (label, href, internal) => {
    const a = el("a", "rec-linkish", label);
    a.href = href;
    if (!internal) { a.target = "_blank"; a.rel = "noopener"; }
    links.append(a);
  };
  if (p.notePath) link("contact note →", "#/note/" + encodeURIComponent(p.notePath), true);
  if (p.orcid) link("orcid ↗", p.orcid);
  if (p.github) link("github ↗", p.github);
  if (p.linkedin) link("linkedin ↗", p.linkedin);
  if (p.editable && p.id.startsWith("aion-net/")) link("in the recruiting graph →", "#/aion/recruiting/network", true);
  if (links.children.length) insp.append(links);

  if ((p.firms || []).length) insp.append(el("div", "net-insp-hint", "investor · " + p.firms.join(", ")));
  if (p.source && p.source !== "owner") insp.append(el("div", "net-insp-hint", "kept from " + p.source + (p.sourceRef ? " · " + p.sourceRef : "")));
}

// ---- the graph's hooks into the list (97-rec-graph.js calls these)

function netHas(id) { return !!netCache && (netCache.people || []).some((p) => p.id === id); }

// netOpen is "edit in the list": the graph hands a person back to the row
// where kind, tags, note and last contact are edited.
function netOpen(id) {
  netSel = id;
  netQuery = "";
  netKind = netSource = netTag = "";
  netSetViewMode("list");
  netPaint();
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
        if (netHas(lp.id)) { netSel = lp.id; netPaintList(); netPaintInspector(); }
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
