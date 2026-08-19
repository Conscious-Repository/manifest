// ---- REAL ESTATE domain views (RE spec §3): Decisions · Intake · Money ·
// Deal page. The decision log is the aion mirror (system/realestate/
// backlog.md via /api/re/backlog); Intake is the RE-scoped lens over pending
// re-backlog proposals (same approval cards as the FEED); Money is the
// read-only transaction feed over the statement workbench + property select.

// ---- BACKLOG — the AION mirror (system/realestate/backlog.md via
// /api/re/backlog). One two-column surface that folds the old Decisions,
// Intake and Outstanding views: an intake lane (transcript extractions
// awaiting confirm), a decisions lane, owner-grouped tasks (re-backlog tasks +
// property todos, mine and owed), and a sticky inspector. Reuses the .aion-*
// backlog classes so the shape is pixel-identical to AION.
let reDecidedOpen = false;
let reDoneOpen = false;
let reBacklogSelId = null;   // selected row id
let reBacklogSelSrc = null;  // "re" (backlog item) | "prop" (property todo)
const reFreshDone = new Set(); // re-tasks checked this session — held in place

// rock helpers over the goals `## Real Estate` area (the org rocks). A task's
// `rock` may instead hold a `property/<slug>` ref — the same field doubles as
// the property tether, so both kinds resolve/label here.
function rePropBySlug(slug) { return (typeof propertyCache !== "undefined" ? propertyCache : []).find((p) => p.slug === slug); }
function rePropLabel(p) { return p.short || p.address || p.slug; }
// reRockLadder — the LIVE tether targets from the goals `## Real Estate` area:
// every open rock and its open child stages (flattened, parent-labelled). The
// picker/resolver read this rather than reOrgRocks() (top-level only) so a rock
// consolidated into a stage stays selectable and never reads as "stale".
function reRockLadder() { return flattenRockLadder(reOrgRocks()); }
function reRockResolved(id) {
  if (!id) return false;
  if (id.startsWith("property/")) return !!rePropBySlug(id.slice(9));
  return !!reRockLadder().find((r) => r.id === id);
}
function reRockLabel(id) {
  if (!id) return "";
  if (id.startsWith("property/")) {
    const p = rePropBySlug(id.slice(9));
    return p ? rePropLabel(p) : id.slice(9).replace(/-/g, " ");
  }
  const rock = reRockLadder().find((r) => r.id === id);
  if (rock) return rock.text;
  return id.replace(/^(realestate|re)\//, "").replace(/-/g, " ");
}

// reRockSuggest — the task inspector's rock typeahead: org rocks first, then
// properties (picked as a `property/<slug>` ref). Empty pick clears the tether.
function reRockSuggest(q, add, ta, onPick) {
  add("— no rock —", "", () => { ta.commit(""); onPick(""); });
  reRockLadder()
    .filter((r) => !r.checked)
    .filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((r) => add(r.label, "rock", () => { ta.commit(r.text); onPick(r.id); }));
  (typeof propertyCache !== "undefined" ? propertyCache : [])
    .filter((p) => !q || (p.address || "").toLowerCase().includes(q) ||
      (p.slug || "").includes(q) || (p.deal || "").toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((p) => add(rePropLabel(p) + (p.deal ? "  · " + p.deal : ""), "property",
      () => { ta.commit(rePropLabel(p)); onPick("property/" + p.slug); }));
}

function reBacklogSelect(src, id) {
  if (reBacklogSelId === id && reBacklogSelSrc === src) { reBacklogSelId = null; reBacklogSelSrc = null; }
  else { reBacklogSelId = id; reBacklogSelSrc = src; }
  renderProperties();
}

// reOwnerSuggest — owner typeahead over the RE roster (the AION mirror). The
// roster (propTodosMeta.assignees.realestate) already unifies people.md
// persons/partners with contractor records — name · trade, keyed by slug.
// "· you" clears ownership back to you (empty owner reads as mine).
function reOwnerSuggest(q, add, ta, onPick) {
  if (!q || "you".includes(q) || "me".includes(q)) {
    add("· you (me)", "", () => { ta.commit(""); onPick(""); });
  }
  const roster = ((propTodosMeta && propTodosMeta.assignees) || {}).realestate || [];
  roster
    .filter((c) => !q || (c.slug || "").toLowerCase().includes(q) ||
      (c.name || "").toLowerCase().includes(q) || (c.trade || "").toLowerCase().includes(q) ||
      (c.aliases || []).some((x) => String(x).toLowerCase().includes(q)))
    .slice(0, 8)
    .forEach((c) => add(c.name + (c.trade ? " · " + c.trade : ""), c.slug || "",
      () => { ta.commit(c.slug || ""); onPick(c.slug || ""); }));
}

function renderREBacklog() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const wrap = el("div", "aion-backlog");
  const list = el("div", "aion-list");
  const insp = el("div", "aion-inspector");
  wrap.append(list, insp);
  host.append(wrap);

  // Pending re-backlog proposals live in the FEED only (owner call
  // 2026-08-12 — the aion pattern). The DOCUMENT intake affordance (overhaul
  // §5) is static — no async insert, so the scroll position stays put; the
  // parsed proposal lands in FEED.
  list.append(reIntakeLane());

  // -- decisions lane --
  // Every decision in the domain, whoever owns it: the RE decision log AND the
  // ones living in a property's rock tree. Tree decisions are absent from the
  // task projection by design (they are not tasks), so without this they were
  // visible only on their own property page.
  const items = reItems();
  const decisions = items.filter((it) => it.kind === "decision").map((it) => ({ src: "re", it }))
    .concat(rePropDecisions().map((d) => ({ src: "propdec", it: d })));
  const isDecided = (d) => (d.src === "re" ? d.it.status === "decided" : !!d.it.checked);
  const openDec = decisions.filter((d) => !isDecided(d));
  const decided = decisions.filter(isDecided);
  const lane = el("div", "aion-dec-lane");
  const lh = el("div", "aion-sec-label");
  lh.append(el("span", "aion-sec-title", "◇ Decisions"),
    el("span", "aion-sec-count", openDec.length + " open · " + decided.length + " decided"));
  lh.append(ghostInput("＋ decision", "aion-add aion-sec-add", (v) =>
    rePost("/api/re/backlog/item", { kind: "decision", title: v }, "Decision added")));
  lane.append(lh);
  openDec.forEach((d) => lane.append(d.src === "re" ? reBacklogDecRow(d.it) : rePropDecRow(d.it)));
  if (decided.length) {
    const t = el("button", "aion-done-toggle", (reDecidedOpen ? "▾" : "▸") + " decided · " + decided.length);
    t.onclick = () => { reDecidedOpen = !reDecidedOpen; renderProperties(); };
    lane.append(t);
    if (reDecidedOpen) decided.forEach((d) => lane.append(d.src === "re" ? reBacklogDecRow(d.it) : rePropDecRow(d.it)));
  }
  list.append(lane);

  // -- owner-grouped tasks (the Outstanding fold): re-backlog tasks + property
  //    todos, mine and owed, grouped by owner exactly like AION. Non-"you"
  //    groups ARE Outstanding (work owed to you by others). --
  const tasks = [];
  items.filter((it) => it.kind === "task").forEach((it) =>
    tasks.push({ src: "re", id: it.id, owner: it.owner || "", done: it.status === "done", it }));
  ((propTodosMeta && propTodosMeta.rows) || []).forEach((r) => {
    if (r.source === "property") tasks.push({ src: "prop", id: r.id, owner: r.owner || "", done: false, text: r.text, container: r.container });
  });
  propOutstandingGroups().forEach((g) => (g.items || []).forEach((r) =>
    tasks.push({ src: "prop", id: r.id, owner: r.owner || "", done: false, text: r.text, container: g.container })));

  const ME = ((propTodosMeta && propTodosMeta.me) || "BA").toUpperCase();
  // reOwnerKey collapses an aliased vendor key onto its person (olga-sobkiv → OS),
  // so one human is one group however a given line spells her.
  const bucket = (owner) => (mineOwner(owner) ? ME : reOwnerKey(owner).toUpperCase());
  const doneInPlace = tasks.filter((t) => t.done && t.src === "re" && reFreshDone.has(t.id));
  const doneTasks = tasks.filter((t) => t.done && !(t.src === "re" && reFreshDone.has(t.id)));
  const openTasks = tasks.filter((t) => !t.done);

  const groups = {};
  const order = [];
  openTasks.concat(doneInPlace).forEach((t) => {
    const key = bucket(t.owner);
    if (!groups[key]) { groups[key] = []; order.push(key); }
    groups[key].push(t);
  });
  const openCount = (key) => groups[key].filter((t) => !t.done).length;
  order.sort((a, b) => openCount(b) - openCount(a));
  order.forEach((key) => {
    const g = el("div", "aion-owner-group");
    const gh = el("div", "aion-owner-head");
    gh.append(el("span", "aion-owner-ini", key || "UNASSIGNED"));
    if (key === ME) gh.append(el("span", "aion-owner-you", "· you"));
    else {
      const name = assigneeName(key);
      if (name && name !== key) gh.append(el("span", "aion-owner-name", name));
    }
    gh.append(el("span", "aion-sec-count", String(openCount(key))));
    g.append(gh);
    groups[key].forEach((t) => g.append(reTaskRow(t)));
    g.append(ghostInput("＋ task for " + key, "aion-add", (v) =>
      rePost("/api/re/backlog/item", { kind: "task", title: v, owner: key }, "Task added → " + key)));
    list.append(g);
  });
  list.append(ghostInput("＋ task", "aion-add", (v) =>
    rePost("/api/re/backlog/item", { kind: "task", title: v }, "Task added")));

  if (doneTasks.length) {
    const t = el("button", "aion-done-toggle", (reDoneOpen ? "▾" : "▸") + " done · " + doneTasks.length);
    t.onclick = () => { reDoneOpen = !reDoneOpen; renderProperties(); };
    list.append(t);
    if (reDoneOpen) doneTasks.forEach((td) => list.append(reTaskRow(td)));
  }

  // sticky inspector — phone gets the same renderer in a bottom sheet (AION)
  if (window.mf && window.mf.phone()) {
    if (reBacklogSelId) {
      window.mfSheet.open((b) => renderREBacklogInspector(b), {
        key: "re-backlog",
        onClose: () => { if (reBacklogSelId) { reBacklogSelId = null; reBacklogSelSrc = null; renderProperties(); } },
        reopen: () => { if (!els.propertiesView.hidden) renderProperties(); },
      });
    } else {
      window.mfSheet.closeIf("re-backlog");
    }
  } else {
    renderREBacklogInspector(insp);
  }
}

function reTaskRow(t) { return t.src === "re" ? reBacklogTaskRow(t.it) : rePropTodoRow(t); }

// rePropDecisions — every [decision::] node across the properties, flattened
// with the context that makes it legible on a domain-wide list.
function rePropDecisions() {
  const out = [];
  (propertyCache || []).forEach((p) => {
    (p.work || []).forEach((st) => {
      const walk = (nodes) => (nodes || []).forEach((n) => {
        if (n.decision) out.push({ property: p, rock: st, n });
        walk(n.children);
      });
      walk(st.tasks);
    });
  });
  return out;
}

// rePropDecRow — the same row as a log decision; its meta carries the property
// and rock instead of a needed-by, and it selects into the same inspector.
function rePropDecRow(d) {
  const n = d.n, decided = !!n.checked;
  const sel = reBacklogSelId === n.id && reBacklogSelSrc === "propdec";
  const row = el("div", "aion-dec-row" + (decided ? " decided" : "") + (sel ? " sel" : ""));
  row.append(el("span", "aion-dec-glyph", "◇"));
  const main = el("div", "aion-main");
  main.append(el("div", "aion-dec-text", n.text));
  const bits = [rePropLabel(d.property)];
  if (d.rock && d.rock.text) bits.push(d.rock.text);
  if (n.owner) bits.push("@" + String(assigneeName(n.owner)).replace(/\s*\(.*\)$/, ""));
  if (decided && n.resolution) bits.push("→ " + n.resolution);
  main.append(el("div", "aion-item-meta", bits.join(" · ")));
  row.append(main);
  row.append(el("span", "aion-status " + (decided ? "closed" : "open"), decided ? "DECIDED" : "OPEN"));
  row.onclick = () => reBacklogSelect("propdec", n.id);
  return row;
}

function reBacklogDecRow(it) {
  const decided = it.status === "decided";
  const sel = reBacklogSelId === it.id && reBacklogSelSrc === "re";
  const row = el("div", "aion-dec-row" + (decided ? " decided" : "") + (sel ? " sel" : ""));
  row.append(el("span", "aion-dec-glyph", "◇"));
  const main = el("div", "aion-main");
  main.append(el("div", "aion-dec-text", it.text));
  const bits = [];
  if (!decided && it.neededBy) bits.push("needed by " + it.neededBy);
  if (it.owner) bits.push("@" + it.owner);
  if (decided) bits.push("decided " + (it.decided || "") + (it.outcome ? " → " + it.outcome : ""));
  main.append(el("div", "aion-item-meta", bits.join(" · ")));
  row.append(main);
  row.append(el("span", "aion-status " + (decided ? "closed" : "open"), decided ? "DECIDED" : "OPEN"));
  row.onclick = () => reBacklogSelect("re", it.id);
  return row;
}

// re-backlog task row — mirror aionTaskRow (check · title/meta · rock · —).
function reBacklogTaskRow(it) {
  const done = it.status === "done";
  const alarmed = !done && !!it.due && it.due < isoToday();
  const sel = reBacklogSelId === it.id && reBacklogSelSrc === "re";
  const row = el("div", "aion-task-row" + (done ? " done" : "") + (alarmed ? " alarm" : "") + (sel ? " sel" : ""));
  const c = el("button", "aion-check" + (done ? " off" : ""), done ? "●" : "○");
  c.title = done ? "unmark — stays in place until PUBLISH" : "mark done (stays here until PUBLISH)";
  c.onclick = (e) => {
    e.stopPropagation();
    if (done) reFreshDone.delete(it.id); else reFreshDone.add(it.id);
    rePost("/api/re/backlog/update/" + it.id, { status: done ? "open" : "done" });
  };
  row.append(c);
  const main = el("div", "aion-main");
  main.append(el("div", "aion-title", it.text));
  const bits = [];
  if (it.due && !done) bits.push((alarmed ? "● overdue " : "due ") + it.due);
  if (it.captured) bits.push(it.captured);
  main.append(el("div", "aion-item-meta", bits.join(" · ")));
  row.append(main);
  const stale = it.rock && !reRockResolved(it.rock);
  const tag = el("span", "aion-rock-tag" + (stale ? " stale" : ""), it.rock ? reRockLabel(it.rock) : "");
  if (stale) tag.title = "closed/historic rock — reattach to a live rock";
  row.append(tag);
  row.onclick = () => reBacklogSelect("re", it.id);
  return row;
}

// property-todo row — the Outstanding fold. Check writes through /api/tasks;
// the property name rides the rock-tag column; the row selects into a light
// inspector that links to the property.
function rePropTodoRow(t) {
  const sel = reBacklogSelId === t.id && reBacklogSelSrc === "prop";
  const row = el("div", "aion-task-row" + (sel ? " sel" : ""));
  const c = el("button", "aion-check", "○");
  c.title = "mark done";
  c.onclick = (e) => { e.stopPropagation(); rePost("/api/tasks/check", { id: t.id, checked: true }, "Marked done"); };
  row.append(c);
  const main = el("div", "aion-main");
  main.append(el("div", "aion-title", t.text));
  main.append(el("div", "aion-item-meta", "property task"));
  row.append(main);
  row.append(el("span", "aion-rock-tag", t.container ? (t.container.name || t.container.slug || "") : ""));
  row.onclick = () => reBacklogSelect("prop", t.id);
  return row;
}

// ---- the inspector (mirrors renderAionInspector; the list never reflows) ----
function renderREBacklogInspector(insp) {
  if (reBacklogSelSrc === "prop") { rePropTodoInspector(insp); return; }
  if (reBacklogSelSrc === "propdec") { rePropDecInspector(insp); return; }
  const it = reItems().find((x) => x.id === reBacklogSelId);
  if (!it) {
    insp.append(el("div", "aion-insp-empty", "select a row — edits save as you go"));
    return;
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Inspector"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; renderProperties(); };
  head.append(x);
  insp.append(head);

  const patch = (set, msg) => rePost("/api/re/backlog/update/" + it.id, set, msg);

  const title = inputEl("");
  title.value = it.text;
  title.className = "aion-insp-title";
  const commitTitle = () => {
    const v = title.value.trim();
    if (v && v !== it.text) { reBacklogSelId = null; reBacklogSelSrc = null; patch({ title: v }); }
  };
  title.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") commitTitle();
    else if (ev.key === "Escape") { title.value = it.text; title.blur(); }
  });
  title.addEventListener("blur", commitTitle);
  insp.append(title);

  const field = (label, node) => {
    const f = el("div", "aion-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    insp.append(f);
  };

  // owner — a typeahead over the RE roster (people.md persons/partners +
  // contractor records, via propTodosMeta.assignees.realestate), mirroring
  // AION's people picker: a pick commits the roster slug; "· you" clears it.
  const setOwner = (v) => { if (v !== (it.owner || "")) patch({ owner: v }); };
  const ownerTa = typeahead({
    placeholder: "person / partner / contractor",
    initial: it.owner || "",
    suggest: (q, add, ta) => reOwnerSuggest(q, add, ta, setOwner),
  });
  field("owner", ownerTa.el);

  // rock: BOTH kinds tether (mirrors AION) — a decision filed without one
  // falls out of every rock-scoped surface, and a decided decision keeps this
  // one editable field: linkage, not content (owner call 2026-08-18).
  const setRock = (v) => { if (v !== (it.rock || "")) patch({ rock: v }); };
  const rockTa = typeahead({
    placeholder: "rock or property",
    initial: it.rock ? reRockLabel(it.rock) : "",
    suggest: (q, add, ta) => reRockSuggest(q, add, ta, setRock),
  });
  field("rock", rockTa.el);

  if (it.kind === "task") {
    const due = inputEl(""); due.type = "date"; due.value = it.due || ""; due.className = "pp-in";
    due.onchange = () => patch({ due: due.value });
    field("due", due);
  } else {
    const nb = inputEl(""); nb.type = "date"; nb.className = "pp-in";
    nb.value = /^\d{4}-\d{2}-\d{2}$/.test(it.neededBy || "") ? it.neededBy : "";
    nb.onchange = () => patch({ needed_by: nb.value });
    field("needed by", nb);
    if (it.status !== "decided") {
      // outcome + decide are ONE quiet control (mirrors AION): Enter files it
      const outcome = inputEl("what was decided…");
      outcome.className = "pp-in aion-insp-outcome";
      field("outcome", outcome);
      const decide = el("button", "aion-decide-inline", "decide ⏎");
      decide.title = "files to the permanent decision log (Enter in the outcome field does the same)";
      decide.disabled = true;
      const doDecide = () => {
        if (!outcome.value.trim()) return;
        reBacklogSelId = null; reBacklogSelSrc = null;
        rePost("/api/re/backlog/decide/" + it.id, { outcome: outcome.value.trim() }, "Decided — permanent log");
      };
      outcome.addEventListener("input", () => { decide.disabled = !outcome.value.trim(); });
      outcome.addEventListener("keydown", (ev) => { if (ev.key === "Enter") doDecide(); });
      decide.onclick = doDecide;
      insp.append(decide);
    } else if (it.outcome) {
      field("outcome", el("span", "aion-insp-ro", it.outcome));
    }
  }
  if (it.captured) field("captured", el("span", "aion-insp-ro", it.captured));
  field("kind", el("span", "aion-insp-ro", it.kind));
  if ((it.sources || []).length) {
    const src = el("a", "aion-insp-src", "⧉ " + it.sources[0]);
    src.href = "#/note/" + encodeURIComponent(it.sources[0].includes("/") ? it.sources[0] + ".md" : it.sources[0] + ".md");
    insp.append(src);
  }
  const del = el("button", "aion-insp-del", "delete item");
  del.onclick = () => {
    const yes = el("button", "aion-insp-del armed", "delete — permanent?");
    yes.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; rePost("/api/re/backlog/delete/" + it.id, {}, "Deleted"); };
    del.replaceWith(yes);
    setTimeout(() => { if (yes.parentNode) yes.replaceWith(del); }, 2500);
  };
  insp.append(del);
  const foot = el("div", "aion-insp-foot");
  foot.append(el("span", "", "edits save as you go"));
  insp.append(foot);
}

// property-todo inspector — read-only text/owner + a link to the property and
// a done button (these are owed items; the property page owns their edits).
// rePropDecInspector — a property-tree decision, decided from here. Same
// controls as the property page's panel (outcome + decide ⏎, owner), so a
// decision reads and resolves the same way wherever you meet it.
function rePropDecInspector(insp) {
  const hit = rePropDecisions().find((d) => d.n.id === reBacklogSelId);
  if (!hit) {
    insp.append(el("div", "aion-insp-empty", "select a row — edits save as you go"));
    return;
  }
  const p = hit.property, n = hit.n;
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Decision"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; renderProperties(); };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "aion-insp-title", n.text));
  const field = (label, node) => {
    const f = el("div", "aion-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    insp.append(f);
  };
  const work = (body, quiet) => postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work", body)
    .then((r) => { if (!quiet) renderProperties(); return r; })
    .catch((e) => showToast("Couldn't save — " + (e.message || "")));
  if (!n.checked) {
    const outcome = inputEl("what was decided…");
    outcome.className = "pp-in aion-insp-outcome";
    const decide = el("button", "aion-decide-inline", "decide ⏎");
    decide.disabled = true;
    const doDecide = async () => {
      const note = outcome.value.trim();
      if (!note) return;
      reBacklogSelId = null; reBacklogSelSrc = null;
      await work({ op: "set-field", id: n.id, field: "resolution", value: note }, true);
      work({ op: "check", id: n.id, checked: true });
    };
    outcome.addEventListener("input", () => { decide.disabled = !outcome.value.trim(); });
    outcome.addEventListener("keydown", (ev) => { if (ev.key === "Enter") doDecide(); });
    decide.onclick = doDecide;
    field("outcome", outcome);
    insp.append(decide);
  } else if (n.resolution) {
    field("outcome", el("span", "aion-insp-ro", n.resolution));
  }
  const ownerTa = typeahead({
    placeholder: "person / partner / contractor", initial: n.owner || "",
    suggest: (q, add, ta) => reOwnerSuggest(q, add, ta, (v) => {
      if (v !== (n.owner || "")) work({ op: "set-field", id: n.id, field: "owner", value: v });
    }),
  });
  field("owner", ownerTa.el);
  field("rock", el("span", "aion-insp-ro", (hit.rock && hit.rock.text) || "—"));
  const src = el("a", "aion-insp-src", "⧉ " + rePropLabel(p));
  src.href = "#/properties/" + encodeURIComponent(p.slug);
  insp.append(src);
  const foot = el("div", "aion-insp-foot");
  foot.append(el("span", "", "edits save as you go"));
  insp.append(foot);
}

function rePropTodoInspector(insp) {
  const id = reBacklogSelId;
  let text = "", owner = "", container = null;
  const mine = ((propTodosMeta && propTodosMeta.rows) || []).find((r) => r.id === id);
  if (mine) { text = mine.text; owner = mine.owner; container = mine.container; }
  else {
    propOutstandingGroups().forEach((g) => (g.items || []).forEach((r) => {
      if (r.id === id) { text = r.text; owner = r.owner; container = g.container; }
    }));
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Task"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; renderProperties(); };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "aion-insp-title", text));
  const field = (label, node) => {
    const f = el("div", "aion-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    insp.append(f);
  };
  field("owner", el("span", "aion-insp-ro", assigneeName(owner)));
  if (container) {
    const src = el("a", "aion-insp-src", "⧉ " + (container.name || container.slug));
    if (container.slug) src.href = "#/properties/" + encodeURIComponent(container.slug);
    insp.append(src);
  }
  const done = el("button", "pill light", "mark done");
  done.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; rePost("/api/tasks/check", { id, checked: true }, "Marked done"); };
  insp.append(done);
  const foot = el("div", "aion-insp-foot");
  foot.append(el("span", "", "edits happen on the property page"));
  insp.append(foot);
}

async function rePost(url, body, msg) {
  setSaveState("saving");
  try {
    await postJSONOk(url, body);
    setSaveState("saved");
    if (msg) showToast(msg);
  } catch (e) { setSaveState("error"); showToast(String(e.message || e).slice(0, 120)); }
  renderProperties();
}


// ---- MONEY — the read-only transaction feed + property assignment ----
// Grid: date · description · amount · property select. Deposits accent;
// expenses ink. Row click opens the inspector (starts CLOSED). Footer:
// per-entity accountant handoffs split personal vs partnered.
let moneyRows = [];
let moneySelId = null;
let moneyPage = 0;
const MONEY_PAGE_SIZE = 50;

// fmtMoneyExact — accounting cells: never rounded, always two decimals
function fmtMoneyExact(v) {
  return "$" + (v || 0).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// the admin lane's assignment slug for a row's paying entity (server routes
// admin:<entity> to entities/<slug>.ledger.csv — entity books, no property)
function moneyAdminSlug(r) { return r.entity ? "admin:" + r.entity : ""; }

// ---- chart of accounts (money-workbench v2): the category autosuggest ----
let moneyCatsCache = null;
async function ensureMoneyCats(force) {
  if (moneyCatsCache && !force) return moneyCatsCache;
  try { moneyCatsCache = (await (await fetch("/api/realestate/categories")).json()).categories || []; }
  catch (e) { moneyCatsCache = moneyCatsCache || []; }
  return moneyCatsCache;
}

// categoryTypeahead — the category field everywhere (inspector + phone
// sheet): suggests from the registry (income rows see income categories,
// expenses see expense ones), quiet `create "internet" →` completion adds to
// the chart of accounts, and picking a category IS a filing gesture.
function categoryTypeahead(r, onResult) {
  const kind = r.inflow ? "income" : "expense";
  const save = async (name) => {
    try {
      const res = await postJSONOk("/api/realestate/statements/row", { id: r.id, category: name, file: true });
      r.category = name;
      onResult(res);
    } catch (e) { showToast("Couldn't save"); }
  };
  const ta = typeahead({
    placeholder: "category…",
    suggest: async (q, add) => {
      const cats = await ensureMoneyCats();
      cats.filter((c) => c.kind === kind && (!q || c.name.includes(q))).slice(0, 10)
        .forEach((c) => add(c.name, c.class, () => { ta.commit(c.name); save(c.name); }));
      const exact = cats.some((c) => c.kind === kind && c.name === q);
      if (q && !exact) {
        add('create "' + ta.value() + '" →', "create", async () => {
          const name = ta.value().toLowerCase();
          // class heuristic: income → operating; expense → operating on a
          // stabilized/leased target, project otherwise. Retype any time by
          // editing system/realestate/categories.md.
          let cls = "operating";
          if (!r.inflow) {
            const slug = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
            const prop = (typeof propertyCache !== "undefined" ? propertyCache : []).find((p) => p.slug === slug);
            cls = prop && ["leased", "stabilized", "closed"].includes(prop.status) ? "operating" : "project";
          }
          try {
            await postJSONOk("/api/realestate/categories", { name, kind, class: cls });
            await ensureMoneyCats(true);
            ta.commit(name);
            showToast("Category added to the chart of accounts (" + cls + ")");
            save(name);
          } catch (e) { showToast("Couldn't create category"); }
        });
      }
    },
  });
  ta.setValue(r.category || "");
  return ta;
}

// ---- splits (money-workbench v2): one bank line across N targets ----
// The server has carried multi-target allocations from day one (Σ±1¢
// validation, admin: lanes, per-target [work::]/[contract::], "split k/n"
// annotations) — this is the missing editor. A deal target fans out an
// even split across its member properties; both pause for confirmation.
let moneySplitSeed = null; // {id, allocs} — set by the split…/deal picks

// evenCents splits total across n rows to the cent (odd cents on the last)
function evenCents(total, n) {
  const cents = Math.round(total * 100);
  const base = Math.floor(cents / n);
  const out = [];
  for (let i = 0; i < n; i++) out.push((i === n - 1 ? cents - base * (n - 1) : base) / 100);
  return out;
}

// dealSplitTargets — active deals with ≥2 member properties (the client-side
// membership idiom: tolerate wikilinks holding the display name or slug)
function dealSplitTargets() {
  return (dealCache || []).map((d) => ({
    deal: d,
    members: (propertyCache || []).filter((p) =>
      !p.hidden && ((p.deal || "") === (d.name || d.slug) || (p.deal || "") === d.slug)),
  })).filter((x) => x.members.length >= 2);
}

// moneySplitSeedFor builds the editor's starting allocation set for a pick
function moneySplitSeedFor(r, value) {
  const total = Math.abs(r.amount || 0);
  if (value.startsWith("deal:")) {
    const slug = value.slice(5);
    const hit = dealSplitTargets().find((x) => x.deal.slug === slug);
    if (!hit) return null;
    const amounts = evenCents(total, hit.members.length);
    return hit.members.map((p, i) => ({ slug: p.slug, amount: amounts[i] }));
  }
  // "split…": current single assignment (or blank) + one empty row to fill
  const cur = ((r.assignments || [])[0] || {});
  return [{ slug: cur.slug || "", amount: cur.amount || total, workId: cur.workId, contract: cur.contract },
    { slug: "", amount: 0 }];
}

// moneySplitEditor — N target+amount rows · ÷ even · Σ check · file. Every
// row can hop to a node/contract on its own property (per-allocation hops —
// the old single-alloc hop editor destroyed splits).
function moneySplitEditor(r, onDone) {
  let allocs = (moneySplitSeed && moneySplitSeed.id === r.id)
    ? moneySplitSeed.allocs.map((a) => ({ ...a }))
    : (r.assignments || []).map((a) => ({ ...a }));
  if (!allocs.length) allocs = [{ slug: "", amount: Math.abs(r.amount || 0) }];
  const total = Math.abs(r.amount || 0);
  const box = el("div", "re-split");
  const render = () => {
    box.innerHTML = "";
    box.append(el("div", "micro-label", "SPLIT " + fmtMoneyExact(total) + " ACROSS"));
    allocs.forEach((a, i) => {
      const row = el("div", "re-split-row");
      const sel = document.createElement("select");
      sel.className = "pp-in re-split-sel";
      const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); };
      opt("", "— target —");
      if (moneyAdminSlug(r)) opt(moneyAdminSlug(r), "admin · " + r.entity);
      activePortfolio().forEach((p) => opt(p.slug, p.short || p.slug));
      sel.value = a.slug || "";
      sel.onchange = () => { a.slug = sel.value; a.workId = ""; a.contract = ""; render(); };
      const amt = inputEl("$");
      amt.type = "number"; amt.step = "0.01"; amt.className = "pp-in re-split-amt";
      amt.value = a.amount || "";
      amt.onchange = () => { a.amount = parseFloat(amt.value) || 0; render(); };
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { allocs.splice(i, 1); render(); };
      row.append(sel, amt, x);
      box.append(row);
      // per-allocation hops: node + contract on THIS target property
      if (a.slug && !a.slug.startsWith("admin:")) {
        const prop = (propertyCache || []).find((p) => p.slug === a.slug);
        if (prop) box.append(splitHopRow(a, prop));
      }
    });
    const acts = el("div", "re-split-acts");
    const add = el("button", "o-ghost", "＋ target");
    add.onclick = () => { allocs.push({ slug: "", amount: 0 }); render(); };
    acts.append(add);
    if (allocs.length > 1) {
      const even = el("button", "o-ghost", "÷ even");
      even.onclick = () => {
        const amounts = evenCents(total, allocs.length);
        allocs.forEach((a, i) => { a.amount = amounts[i]; });
        render();
      };
      acts.append(even);
    }
    const sum = allocs.reduce((s, a) => s + (a.amount || 0), 0);
    const ok = Math.abs(sum - total) <= 0.01 && allocs.every((a) => a.slug);
    acts.append(el("span", "re-split-sum" + (ok ? " ok" : ""),
      "Σ " + fmtMoneyExact(sum) + (ok ? " ✓" : " of " + fmtMoneyExact(total))));
    box.append(acts);
    const file = el("button", "pill-solid re-money-file", "file ✓ → " + allocs.filter((a) => a.slug).length + " ledger row(s)");
    file.disabled = !ok;
    file.onclick = async () => {
      try {
        const res = await postJSONOk("/api/realestate/statements/row", {
          id: r.id,
          assignments: allocs.filter((a) => a.slug),
          state: allocs.filter((a) => a.slug).length > 1 ? "split" : "assigned",
          file: true,
        });
        if (moneyFileToast(r, res, "Not filed — check the split")) {
          moneySplitSeed = null;
          moneySelId = null;
          if (onDone) onDone();
          renderProperties();
        }
      } catch (e) { showToast("Couldn't file — " + (e.message || "")); }
    };
    box.append(file);
  };
  render();
  return box;
}

// splitHopRow — compact node+contract selects for one allocation (the per-
// target refinement; blank = untethered)
function splitHopRow(a, prop) {
  const row = el("div", "re-split-hops");
  const nodeSel = document.createElement("select");
  nodeSel.className = "pp-in";
  const nopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; nodeSel.append(o); };
  nopt("", "— no tether —");
  (prop.work || []).forEach((st) => {
    nopt(st.id, st.text);
    (st.tasks || []).forEach(function walk(n, prefix) {
      const pre = typeof prefix === "string" ? prefix : "· ";
      nopt(n.id, pre + n.text);
      (n.children || []).forEach((c) => walk(c, pre + "· "));
    });
  });
  nodeSel.value = a.workId || "";
  nodeSel.onchange = () => { a.workId = nodeSel.value; };
  const cSel = document.createElement("select");
  cSel.className = "pp-in";
  const copt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; cSel.append(o); };
  copt("", "— no contract —");
  reContracts()
    .filter((c) => c.status === "accepted" && (c.allocations || []).some((al) => al.property === a.slug))
    .forEach((c) => copt(c.slug, c.name + " · " + fmtMoneyShort(c.remaining != null ? c.remaining : c.total) + " left"));
  cSel.value = a.contract || "";
  cSel.onchange = () => {
    a.contract = cSel.value;
    if (cSel.value && !nodeSel.value) {
      const c = reContracts().find((x) => x.slug === cSel.value);
      const al = c && (c.allocations || []).find((x) => x.property === a.slug);
      if (al) { a.workId = al.nodeId; nodeSel.value = al.nodeId; }
    }
  };
  row.append(nodeSel, cSel);
  return row;
}

// moneyFileToast — one voice for every filing gesture's outcome. The server
// PATCH returns {state, fileError}; a non-empty fileError is the reason the
// row could NOT file (e.g. no category) — always surfaced, never swallowed.
function moneyFileToast(r, res, fallback) {
  if (res.state === "applied") {
    showToast("Filed → " + (r.entity || "entity") + " history");
    return true;
  }
  if (res.fileError) {
    showToast("Not filed — " + (/no category/.test(res.fileError) ? "set a category first" : res.fileError));
    return false;
  }
  if (fallback) showToast(fallback);
  return false;
}

async function renderREMoney() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  let last = "";
  try {
    const d = await (await fetch("/api/realestate/statements")).json();
    moneyRows = d.rows || [];
    last = d.lastImport || "";
  } catch (e) { moneyRows = []; }
  await loadReContracts(); // the third hop's picker (accepted contracts + remaining)
  // the split (owner call 2026-08-19): the TOP lane holds only what still
  // needs filing; a filed row (applied — the PATCH auto-applies the moment
  // property + category land) or a dismissed one lives in its entity's
  // history fold below.
  const work = moneyRows.filter((r) => r.state !== "applied" && r.state !== "skipped");
  const filed = moneyRows.filter((r) => r.state === "applied" || r.state === "skipped");
  host.append(el("div", "re-lane-head",
    "MONEY · " + work.length + " to file · " + filed.length + " filed" +
    (last ? " · last import " + last : "")));
  // dead-simple statement upload (overhaul decision 15): drop the bank CSV —
  // the remembered column mapping + entity binding do the rest; a first-seen
  // format opens the one-time mapping strip.
  host.append(moneyUploadLane());
  if (!work.length) {
    host.append(el("div", "pp-empty", moneyRows.length
      ? "Nothing left to file — everything lives in the entity histories below."
      : "No transactions yet — link a bank account or drop a CSV above."));
  }
  const cols = el("div", "re-money-cols");
  ["DATE", "DESCRIPTION", "AMOUNT", "PROPERTY"].forEach((h, i) =>
    cols.append(el("span", i === 2 ? "prop-col-r" : "", h)));
  host.append(cols);
  const shell = el("div", "re-money-shell");
  const list = el("div", "re-money-list");
  // pagination — historic backfills run to hundreds of rows; 50 per page
  const pages = Math.max(1, Math.ceil(work.length / MONEY_PAGE_SIZE));
  if (moneyPage >= pages) moneyPage = pages - 1;
  const start = moneyPage * MONEY_PAGE_SIZE;
  work.slice(start, start + MONEY_PAGE_SIZE).forEach((r) => list.append(moneyRow(r)));
  shell.append(list);
  const insp = el("div", "re-money-insp");
  insp.hidden = true; // starts closed — the list arrives full width
  shell.append(insp);
  host.append(shell);
  // a selected row survives re-renders (the split…/deal picks re-render to
  // open the editor — the inspector must come back up with them)
  const selRow = moneySelId && moneyRows.find((r) => r.id === moneySelId);
  if (selRow) renderMoneyInspector(selRow);
  if (pages > 1) {
    const pager = el("div", "re-money-pager");
    const btn = (label, disabled, go) => {
      const b = el("button", "pill light", label);
      b.disabled = disabled;
      b.onclick = () => { moneyPage = go; moneySelId = null; renderProperties(); };
      pager.append(b);
    };
    btn("← prev", moneyPage === 0, moneyPage - 1);
    pager.append(el("span", "re-money-pageinfo",
      (start + 1) + "–" + Math.min(start + MONEY_PAGE_SIZE, work.length) + " of " + work.length +
      " · page " + (moneyPage + 1) + "/" + pages));
    btn("next →", moneyPage >= pages - 1, moneyPage + 1);
    host.append(pager);
  }
  // HISTORY — filed rows under the entity whose books they hit
  if (filed.length) moneyHistory(host, filed);
  // footer: accountant handoffs by entity books, personal vs partnered
  host.append(moneyFooter());
}

// moneyHistory — one collapsed fold per entity: every filed (applied) and
// dismissed row, newest first, 50 at a time.
const moneyHistShown = {}; // entity → rows revealed
function moneyHistory(host, filed) {
  host.append(el("div", "re-lane-head re-money-hist-head", "HISTORY — FILED BY ENTITY"));
  const groups = {};
  filed.forEach((r) => { (groups[r.entity || "(no entity)"] = groups[r.entity || "(no entity)"] || []).push(r); });
  Object.keys(groups).sort().forEach((ent) => {
    const rows = groups[ent];
    rows.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
    const skipped = rows.filter((r) => r.state === "skipped").length;
    const sum = rows.reduce((s, r) => s + (r.inflow ? 0 : r.amount || 0), 0);
    const body = collapsibleSection(host, ent,
      rows.length + " rows · " + fmtMoneyExact(sum) + " out" + (skipped ? " · " + skipped + " skipped" : ""),
      !!moneyHistShown[ent]);
    const shown = moneyHistShown[ent] || MONEY_PAGE_SIZE;
    rows.slice(0, shown).forEach((r) => body.append(moneyRow(r)));
    if (rows.length > shown) {
      const more = el("button", "o-ghost", "＋ show " + Math.min(MONEY_PAGE_SIZE, rows.length - shown) + " more");
      more.onclick = () => { moneyHistShown[ent] = shown + MONEY_PAGE_SIZE; renderProperties(); };
      body.append(more);
    }
  });
}

// ---- the statement upload lane (overhaul decision 15) ----
// Drop/browse a CSV → POST /statements/upload (pure parse + remembered
// mapping/entity) → when everything is remembered, one confirm ingests; a
// new format opens the mapping strip (date/vendor/amount/note columns +
// paying entity), remembered for next time by header signature.
function moneyUploadLane() {
  const lane = el("div", "re-intake-lane re-money-upload");
  const drop = el("div", "re-intake-drop");
  drop.append(el("span", "re-intake-glyph", "⇪"));
  drop.append(el("span", "", "drop a bank statement CSV — or "));
  const browse = el("button", "pp3-link", "browse");
  drop.append(browse);
  const input = document.createElement("input");
  input.type = "file";
  input.accept = ".csv,text/csv";
  input.hidden = true;
  drop.append(input);
  browse.onclick = () => input.click();
  input.onchange = () => { if (input.files && input.files[0]) moneyUpload(input.files[0], lane); };
  drop.ondragover = (e) => { e.preventDefault(); drop.classList.add("over"); };
  drop.ondragleave = () => drop.classList.remove("over");
  drop.ondrop = (e) => {
    e.preventDefault();
    drop.classList.remove("over");
    if (e.dataTransfer.files && e.dataTransfer.files[0]) moneyUpload(e.dataTransfer.files[0], lane);
  };
  lane.append(drop);
  // the bank-feed pointer (bank plan §4): linked accounts sync in daily with
  // the entity pre-set — the CSV drop stays for one-off statements
  const feedNote = el("div", "re-foot-note");
  feedNote.append(el("span", "", "linked bank accounts sync here automatically — manage them in "));
  const link = el("button", "pp3-link", "SETTINGS → Bank feed");
  link.onclick = () => { reSettingsTab = "bankfeed"; location.hash = "#/properties/settings"; };
  feedNote.append(link);
  lane.append(feedNote);
  return lane;
}

async function moneyUpload(file, lane) {
  const fd = new FormData();
  fd.append("file", file);
  let d;
  try {
    const r = await fetch("/api/realestate/statements/upload", { method: "POST", body: fd });
    if (!r.ok) throw new Error(await r.text());
    d = await r.json();
  } catch (e) { showToast("Upload failed — " + String(e.message || e).slice(0, 120)); return; }
  const mapping = d.mapping || {};
  const ready = mapping.date && mapping.amount && mapping.vendor && d.entity;
  if (ready && d.remembered) {
    moneyIngest(d, mapping, d.entity);
    return;
  }
  // one-time strip: column pickers + the paying entity — remembered after
  lane.querySelectorAll(".re-money-mapstrip").forEach((n) => n.remove());
  const strip = el("div", "re-money-mapstrip");
  strip.append(el("div", "re-uw-label", d.label + " · " + (d.rows || []).length + " rows — map once, remembered by header signature"));
  const grid = el("div", "re-money-mapgrid");
  const sel = {};
  ["date", "vendor", "amount", "note"].forEach((field) => {
    const s = document.createElement("select");
    s.className = "pp-in";
    const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; s.append(o); };
    opt("", field + (field === "note" ? " (optional)" : "") + "…");
    (d.headers || []).forEach((h) => opt(h, h));
    s.value = mapping[field] || "";
    sel[field] = s;
    const f = el("label", "pp3-lform-field");
    f.append(el("span", "pp3-lform-label", field), s);
    grid.append(f);
  });
  // paying entity — required (rides the [paid-by::] token on apply)
  const ent = document.createElement("select");
  ent.className = "pp-in";
  const eopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; ent.append(o); };
  eopt("", "paying entity…");
  (async () => {
    try {
      const es = await (await fetch("/api/realestate/entities")).json();
      (es.entities || []).forEach((x) => eopt(x.name, x.name));
      ent.value = d.entity || "";
    } catch (e) {}
  })();
  const ef = el("label", "pp3-lform-field");
  ef.append(el("span", "pp3-lform-label", "paying entity"), ent);
  grid.append(ef);
  strip.append(grid);
  const actions = el("div", "pp3-uw-actions");
  const cancel = el("button", "pp3-uw-cancel", "cancel");
  cancel.onclick = () => strip.remove();
  const go = el("button", "pp3-compose-go", "ingest ↵");
  go.onclick = () => {
    const m = { date: sel.date.value, vendor: sel.vendor.value, amount: sel.amount.value, note: sel.note.value };
    if (!m.date || !m.vendor || !m.amount) { showToast("Map date, vendor, and amount"); return; }
    if (!ent.value) { showToast("Pick the paying entity"); return; }
    moneyIngest(d, m, ent.value);
  };
  actions.append(cancel, go);
  strip.append(actions);
  lane.append(strip);
}

async function moneyIngest(d, mapping, entity) {
  const idx = {};
  (d.headers || []).forEach((h, i) => { idx[h] = i; });
  const cell = (row, field) => (mapping[field] && idx[mapping[field]] != null ? String(row[idx[mapping[field]]] || "").trim() : "");
  const rows = (d.rows || []).map((row) => ({
    Date: cell(row, "date"), Vendor: cell(row, "vendor"), Note: cell(row, "note"),
    Amount: parseFloat(cell(row, "amount").replace(/[$,()]/g, "")) *
      (/^\(.*\)$/.test(cell(row, "amount")) ? -1 : 1) || 0,
  })).filter((r) => r.Date && r.Amount);
  if (!rows.length) { showToast("No usable rows after mapping"); return; }
  try {
    const res = await postJSONOk("/api/realestate/statements/ingest", {
      label: d.label, entity, signature: d.signature, mapping, rows,
    });
    showToast("Ingested " + (res.added != null ? res.added : rows.length) + " rows — assign below, then apply");
    renderProperties();
  } catch (e) { showToast("Ingest failed — " + String(e.message || e).slice(0, 120)); }
}

function moneyRow(r) {
  const cur = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
  const row = el("div", "re-money-row" + (moneySelId === r.id ? " sel" : ""));
  row.append(el("span", "re-money-date", (r.date || "").slice(5)));
  const desc = el("span", "re-money-desc");
  desc.append(el("span", "", r.vendor || r.note || "(no description)"));
  if (r.entity) desc.append(el("span", "re-money-entity", r.entity));
  if (r.source === "feed") desc.append(el("span", "re-money-src", "feed")); // bank-feed sync vs csv upload
  // phone meta line (desktop hides it): the assigned property, or the file
  // prompt in ink — the row tap opens the assignment sheet
  const curProp = cur ? activePortfolio().find((p) => p.slug === cur) : null;
  const curLabel = cur.startsWith("admin:") ? "admin · " + cur.slice(6)
    : ((curProp && (curProp.short || curProp.slug)) || cur);
  desc.append(el("span", "re-money-meta" + (cur ? "" : " unfiled"),
    cur ? curLabel : "unassigned — tap to file"));
  row.append(desc);
  // exact to the cent — this is accounting, never the k-rounded display
  row.append(el("span", "re-money-amt" + (r.inflow ? " inflow" : ""),
    (r.inflow ? "+" : "") + fmtMoneyExact(Math.abs(r.amount || 0))));
  // property select — assignment in place (single-target; splits via
  // inspector). The admin lane files against the entity's own books, no
  // property (server routes admin:<entity> to its ledger).
  const sel = document.createElement("select");
  sel.className = "pp-in re-money-sel";
  const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); };
  opt("", r.state === "applied" ? "applied" : "unassigned");
  if (moneyAdminSlug(r)) opt(moneyAdminSlug(r), "admin · " + r.entity);
  activePortfolio().forEach((p) => opt(p.slug, p.short || p.slug));
  // split targets: manual split… + one per multi-property deal (even split,
  // pausing in the editor for confirmation — never files on the pick)
  opt("__split", "split…");
  dealSplitTargets().forEach((x) =>
    opt("deal:" + x.deal.slug, "◈ " + (x.deal.name || x.deal.slug) + " (" + x.members.length + "-way)"));
  sel.value = cur;
  sel.disabled = r.state === "applied" || r.state === "skipped";
  sel.onclick = (e) => e.stopPropagation();
  sel.onchange = async () => {
    // split/deal picks open the editor seeded — they never file on the pick
    if (sel.value === "__split" || sel.value.startsWith("deal:")) {
      const seed = moneySplitSeedFor(r, sel.value);
      if (!seed) { showToast("No members found for that deal"); sel.value = cur; return; }
      moneySplitSeed = { id: r.id, allocs: seed };
      moneySelId = r.id;
      if (window.mf && window.mf.phone()) { openMoneyAssignSheet(r); return; }
      renderProperties();
      return;
    }
    try {
      const res = await postJSONOk("/api/realestate/statements/row", {
        id: r.id,
        assignments: sel.value ? [{ slug: sel.value, amount: Math.abs(r.amount || 0) }] : [],
        state: sel.value ? "assigned" : "pending",
        file: true,
      });
      if (moneyFileToast(r, res, sel.value ? "Assigned — set a category to file it" : "Unassigned")) {
        moneySelId = null;
        renderProperties();
      }
    } catch (e) { showToast("Couldn't assign"); }
  };
  row.append(sel);
  row.onclick = () => {
    // phone (RE spec §8): the row tap IS the assignment gesture — a sheet of
    // tap-targets replaces the desktop's inline select + side inspector
    if (window.mf && window.mf.phone()) { openMoneyAssignSheet(r); return; }
    moneySelId = moneySelId === r.id ? null : r.id;
    renderMoneyInspector(r);
  };
  return row;
}

function openMoneyAssignSheet(r) {
  const cur = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
  const done = r.state === "applied" || r.state === "skipped";
  window.mfSheet.open((body) => {
    body.append(el("div", "pp3-insp-text",
      (r.vendor || r.note || "(no description)") + " · " + (r.date || "") + " · " +
      (r.inflow ? "+" : "") + fmtMoneyExact(Math.abs(r.amount || 0))));
    if (done) {
      body.append(el("div", "pp3-insp-note", "already " + r.state + " — edits happen on the property ledger"));
      return;
    }
    const list = el("div", "mf-assign");
    const assign = async (slug) => {
      try {
        const res = await postJSONOk("/api/realestate/statements/row", {
          id: r.id,
          assignments: slug ? [{ slug, amount: Math.abs(r.amount || 0) }] : [],
          state: slug ? "assigned" : "pending",
          file: true,
        });
        moneyFileToast(r, res, slug ? "Assigned — set a category to file it" : "Unassigned");
        window.mfSheet.close();
        renderProperties();
      } catch (e) { showToast("Couldn't assign"); }
    };
    const rowOpt = (v, l, onPick) => {
      const b = el("button", "mf-opt" + (v === cur ? " on" : ""));
      b.append(el("span", "mf-opt-dot", v === cur ? "●" : "○"), el("span", "", l));
      b.onclick = onPick || (() => assign(v));
      list.append(b);
    };
    // split mode: seeded by a split…/deal pick, or an existing multi-target row
    const splitMode = (moneySplitSeed && moneySplitSeed.id === r.id) || (r.assignments || []).length > 1;
    if (splitMode) {
      body.append(moneySplitEditor(r, () => window.mfSheet.close()));
    } else {
      rowOpt("", "unassigned");
      if (moneyAdminSlug(r)) rowOpt(moneyAdminSlug(r), "admin · " + r.entity);
      activePortfolio().forEach((p) => rowOpt(p.slug, p.short || p.slug));
      rowOpt("__split", "split…", () => {
        moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "__split") };
        openMoneyAssignSheet(r);
      });
      dealSplitTargets().forEach((x) => rowOpt("deal:" + x.deal.slug,
        "◈ " + (x.deal.name || x.deal.slug) + " (" + x.members.length + "-way)", () => {
          moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "deal:" + x.deal.slug) };
          openMoneyAssignSheet(r);
        }));
      body.append(list);
    }
    // category — the chart-of-accounts typeahead (phone rows could never
    // complete filing without it); picking a category files the row
    const catTa = categoryTypeahead(r, (res) => {
      moneyFileToast(r, res, "Saved");
      if (res.state === "applied") { window.mfSheet.close(); renderProperties(); }
    });
    body.append(catTa.el);
    if (!splitMode) moneyHopFields(r, body); // split rows hop per-allocation in the editor
    // editable note (bank plan §5) — the phone half of the inspector field
    const noteIn = inputEl("note — lands on the ledger row");
    noteIn.value = r.note || "";
    noteIn.onchange = async () => {
      try {
        await postJSONOk("/api/realestate/statements/row", { id: r.id, note: noteIn.value });
        r.note = noteIn.value;
        showToast("Saved");
      } catch (e) { showToast("Couldn't save"); }
    };
    body.append(noteIn);
  }, { key: "money:" + r.id });
}

function renderMoneyInspector(r) {
  const insp = document.querySelector(".re-money-insp");
  if (!insp) return;
  if (moneySelId !== r.id) { insp.hidden = true; insp.innerHTML = ""; return; }
  insp.hidden = false;
  insp.innerHTML = "";
  const head = el("div", "pp3-insp-head");
  head.append(el("span", "pp3-insp-label", "Transaction"));
  const x = el("button", "pp3-insp-x", "✕");
  x.onclick = () => { moneySelId = null; insp.hidden = true; };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "pp3-insp-text", (r.vendor || "") + " · " + (r.date || "")));
  const fieldRow = (label, val) => {
    const f = el("div", "pp3-insp-field");
    f.append(el("span", "pp3-insp-flabel", label), el("span", "pp3-insp-val", val || "—"));
    return f;
  };
  insp.append(fieldRow("amount", (r.inflow ? "+" : "") + fmtMoneyExact(Math.abs(r.amount || 0))));
  insp.append(fieldRow("entity", r.entity));
  insp.append(fieldRow("statement", r.statement + (r.source === "feed" ? " · feed" : "")));
  insp.append(fieldRow("state", r.state));
  const locked = r.state === "applied" || r.state === "skipped";
  // category + note are owner-editable until apply (bank plan §5): the bank
  // memo arrives as the initial note; the row note IS the ledger note
  const editRow = (label, key, placeholder) => {
    if (locked) return fieldRow(label, r[key]);
    const f = el("div", "pp3-insp-field");
    const inp = inputEl(placeholder);
    inp.value = r[key] || "";
    inp.onchange = async () => {
      try {
        const patch = { id: r.id, file: key === "category" };
        patch[key] = inp.value;
        const res = await postJSONOk("/api/realestate/statements/row", patch);
        r[key] = inp.value;
        if (moneyFileToast(r, res, "Saved")) {
          // the category completed the filing — the row leaves the lot
          moneySelId = null;
          renderProperties();
        }
      } catch (e) { showToast("Couldn't save"); }
    };
    f.append(el("span", "pp3-insp-flabel", label), inp);
    return f;
  };
  if (locked) {
    insp.append(fieldRow("category", r.category));
  } else {
    // the chart-of-accounts typeahead — picking a category files the row
    const f = el("div", "pp3-insp-field");
    const ta = categoryTypeahead(r, (res) => {
      if (moneyFileToast(r, res, "Saved")) {
        moneySelId = null;
        renderProperties();
      }
    });
    f.append(el("span", "pp3-insp-flabel", "category"), ta.el);
    insp.append(f);
  }
  insp.append(editRow("note", "note", "note — lands on the ledger row"));
  const splitting = !locked &&
    ((moneySplitSeed && moneySplitSeed.id === r.id) || (r.assignments || []).length > 1);
  if (splitting) {
    insp.append(moneySplitEditor(r));
  } else {
    moneyHopFields(r, insp);
    if (!locked) {
      const splitLink = el("button", "pp3-link re-split-open", "split across properties…");
      splitLink.onclick = () => {
        moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "__split") };
        renderProperties();
      };
      insp.append(splitLink);
    }
  }
  // explicit file gesture for the flows that patch without filing (hop
  // tethers, vendor-memory prefills awaiting confirmation)
  if (!locked && !splitting && (r.state === "assigned" || r.state === "split")) {
    const fileBtn = el("button", "pill-solid re-money-file", "file ✓ → ledger");
    fileBtn.onclick = async () => {
      try {
        const res = await postJSONOk("/api/realestate/statements/row", { id: r.id, file: true });
        if (moneyFileToast(r, res, "Not filed — check the assignment")) {
          moneySelId = null;
          renderProperties();
        }
      } catch (e) { showToast("Couldn't file — " + (e.message || "")); }
    };
    insp.append(fileBtn);
  }
  insp.append(el("div", "pp3-insp-note",
    "picking a property with a category set files straight to the ledger; receipts + contractor attach on the ledger row (property page → spend)"));
}

// moneyHopFields — the §7 hops on an assigned row: property → NODE (the
// rock-tree tether) → CONTRACT (accepted contracts allocating on that
// property, remaining shown). Writes ride the assignment row; apply turns
// them into [work::] + [contract::] note tokens.
function moneyHopFields(r, host) {
  // split rows edit per-allocation in the split editor — patching alloc 0
  // alone from here used to DESTROY the other allocations
  if ((r.assignments || []).length > 1) return;
  const a = (r.assignments && r.assignments[0]) || null;
  if (!a || !a.slug || a.slug.startsWith("admin:")) return;
  if (r.state === "applied" || r.state === "skipped") return;
  const prop = propertyCache.find((p) => p.slug === a.slug);
  if (!prop) return;
  const save = async (patch) => {
    try {
      const res = await postJSONOk("/api/realestate/statements/row", {
        id: r.id,
        assignments: [{ ...a, ...patch }],
        state: r.state === "split" ? "split" : "assigned",
      });
      Object.assign(a, patch);
      if (moneyFileToast(r, res, "Saved")) {
        moneySelId = null;
        renderProperties();
      }
    } catch (e) { showToast("Couldn't save"); }
  };
  const field = (label, node) => {
    const f = el("div", "pp3-insp-field");
    f.append(el("span", "pp3-insp-flabel", label), node);
    host.append(f);
  };
  // node picker (hop 2)
  const nodeSel = document.createElement("select");
  nodeSel.className = "pp-in";
  const nopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; nodeSel.append(o); };
  nopt("", "— no tether —");
  (prop.work || []).forEach((st) => {
    nopt(st.id, st.text);
    (st.tasks || []).forEach(function walk(n, prefix) {
      const pre = typeof prefix === "string" ? prefix : "· ";
      nopt(n.id, pre + n.text);
      (n.children || []).forEach((c) => walk(c, pre + "· "));
    });
  });
  nodeSel.value = a.workId || "";
  nodeSel.onchange = () => save({ workId: nodeSel.value });
  field("node", nodeSel);
  // contract picker (hop 3)
  const cSel = document.createElement("select");
  cSel.className = "pp-in";
  const copt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; cSel.append(o); };
  copt("", "— no contract —");
  reContracts()
    .filter((c) => c.status === "accepted" && (c.allocations || []).some((al) => al.property === a.slug))
    .forEach((c) => copt(c.slug, c.name + " · " + fmtMoneyShort(c.remaining != null ? c.remaining : c.total) + " left"));
  cSel.value = a.contract || "";
  cSel.onchange = () => {
    const patch = { contract: cSel.value };
    // picking a contract without a node prefills the node from its allocation
    if (cSel.value && !nodeSel.value) {
      const c = reContracts().find((x) => x.slug === cSel.value);
      const al = c && (c.allocations || []).find((x) => x.property === a.slug);
      if (al) { patch.workId = al.nodeId; nodeSel.value = al.nodeId; }
    }
    save(patch);
  };
  field("contract", cSel);
}

function moneyFooter() {
  const foot = el("div", "re-money-foot");
  foot.append(el("div", "re-lane-head", "ACCOUNTANT HANDOFFS — split by the entity the books belong to"));
  // group entities via holdings + partnered flag (from /api/realestate/entities
  // in settings; here we derive from statement rows' entity labels + holdings)
  const names = Object.keys(holdingsCache).sort();
  if (!names.length) {
    foot.append(el("div", "re-foot-note", "no entity holdings yet — set each property's entity (its books) on the property page"));
    return foot;
  }
  names.forEach((name) => {
    const h = holdingsCache[name];
    const row = el("div", "re-handoff-row");
    row.append(el("span", "re-handoff-name", name));
    row.append(el("span", "re-handoff-meta", h.owned + " owned" + (h.acquiring ? " · " + h.acquiring + " acquiring" : "")));
    const exp = pillLight("export tax csv", async () => {
      try {
        const res = await postJSONOk("/api/realestate/export-tax", { entity: name });
        showToast("Exported — " + (res.path || "vault exports/"));
      } catch (e) { showToast("Export failed — " + (e.message || "")); }
    });
    row.append(exp);
    foot.append(row);
  });
  return foot;
}

// ---- DEAL PAGE — stat strip, members + totals, overrides, checklist ----
async function renderDealPage(slug) {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const deal = dealCache.find((d) => d.slug === slug);
  if (!deal) { host.append(el("div", "pp-empty", "No deal record named " + slug + ".")); return; }
  if (!reAssumptionsCache) await loadReAssumptions();
  const head = el("div", "re-deal-head");
  const title = el("h2", "pp3-title", deal.name || deal.slug);
  head.append(title);
  const open = el("button", "pp3-note", "open note →");
  open.onclick = () => { location.hash = "#/note/" + encodeURIComponent(deal.path); };
  head.append(open);
  host.append(head);

  // members: the properties whose deal wikilink names this deal
  const members = propertyCache.filter((p) => (p.deal || "") === (deal.name || deal.slug) || (p.deal || "") === deal.slug);
  // tier-1 inputs live on each member's source sidecar — fetch once per render
  await Promise.all(members.map(async (p) => {
    if (p.__source) return;
    try {
      const d = await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json();
      p.__source = d.source || d; // the endpoint wraps: {source: {...}}
    } catch (e) { p.__source = {}; }
  }));
  // stat strip from member source data (screening tier — the portal runs the
  // full engine; these are the three outputs the spec shows)
  let tdc = 0, noi = 0;
  members.forEach((p) => {
    const uw = reScreeningCalc(p);
    tdc += uw.tdc || 0;
    noi += uw.noi || 0;
  });
  const a = reAssumptions();
  const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
  // takeout-sized DSCR (see reScreeningCalc — the LTV-max loan makes DSCR a
  // constant of the assumptions; the takeout loan is the deal-varying truth)
  const dealPermMax = arv * (a.perm_ltv || 0);
  const dealTakeout = tdc * (a.construction_loan_ltc || 0);
  const dealLoan = dealTakeout > 0 ? Math.min(dealPermMax, dealTakeout) : dealPermMax;
  const dealRefiGap = dealTakeout > dealPermMax ? dealTakeout - dealPermMax : 0;
  const ads = reDebtService(dealLoan, a);
  const dscr = ads ? noi / ads : 0;
  const strip = el("div", "pp3-strip");
  [["TDC", tdc], ["NOI", noi], ["ARV", arv]].forEach(([label, v]) => {
    const c = el("div", "pp3-cell");
    c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val", fmtMoneyShort(v)));
    strip.append(c);
  });
  const dc = el("div", "pp3-cell");
  dc.append(el("div", "pp3-cell-label", "DSCR · at takeout"),
    el("div", "pp3-cell-val" + (dscr && dscr < 1.25 ? " over" : ""), dscr ? dscr.toFixed(2) : "—"));
  if (dealRefiGap > 0) dc.append(el("div", "re-refi-gap", "refi gap " + fmtMoneyShort(dealRefiGap) + " — LTV caps the takeout"));
  strip.append(dc);
  host.append(strip);

  // members table + totals row — RENT/MO reads the frontmatter unit mix when
  // present (accented: a measured figure, not a sidecar assumption)
  const cols = el("div", "prop-cols re-deal-cols");
  ["PROPERTY", "UNITS", "RENT/MO", "TDC", "NOI", "DSCR"].forEach((h, i) => cols.append(el("span", i ? "prop-col-r" : "", h)));
  host.append(cols);
  const memberRent = (p) => p.rentMonthly ||
    (reSrcNum(p.__source, "avg_rent_per_unit") * (((p.unitMix || []).length) || p.units || reSrcNum(p.__source, "total_units")));
  members.forEach((p) => {
    const uw = reScreeningCalc(p);
    const row = el("div", "prop-row re-deal-row");
    row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    row.append(el("span", "prop-addr", p.short || p.slug));
    row.append(el("span", "prop-col-r", String(((p.unitMix || []).length) || p.units || "—")));
    row.append(el("span", "prop-col-r" + (p.rentMonthly ? " re-measured" : ""), memberRent(p) ? fmtMoneyShort(memberRent(p)) : "—"));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.tdc || 0)));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.noi || 0)));
    row.append(el("span", "prop-col-r" + (uw.dscr && uw.dscr < 1.25 ? " over" : ""), uw.dscr ? uw.dscr.toFixed(2) : "—"));
    host.append(row);
  });
  const totals = el("div", "prop-row re-deal-totals");
  totals.append(el("span", "prop-addr", "deal total"));
  totals.append(el("span", "prop-col-r", String(members.reduce((n, p) => n + (((p.unitMix || []).length) || p.units || 0), 0) || "—")));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(members.reduce((n, p) => n + memberRent(p), 0))));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(tdc)));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(noi)));
  totals.append(el("span", "prop-col-r" + (dscr && dscr < 1.25 ? " over" : ""), dscr ? dscr.toFixed(2) : "—"));
  host.append(totals);

  host.append(el("div", "re-foot-note",
    "screening numbers (tier 1) — the portal runs the full engine on PUBLISH; overrides live on the deal's source.json"));

  // bank-ready checklist — derived from what the deal actually has
  const check = el("div", "re-deal-check");
  check.append(el("div", "re-lane-head", "BANK-READY CHECKLIST"));
  const items = [
    ["every member has units + rent inputs", members.length > 0 && members.every((p) => reScreeningCalc(p).complete)],
    ["deal note exists", true],
    ["DSCR ≥ 1.25 at globals", dscr >= 1.25],
    ["published to the portal", false],
  ];
  items.forEach(([label, ok]) => {
    const li = el("div", "re-check-row" + (ok ? " ok" : ""));
    li.append(el("span", "re-check-glyph", ok ? "✓" : "○"), el("span", "", label));
    check.append(li);
  });
  host.append(check);
}

// ---- tier-1 screening math (spec §3 property page: computed NOI · ARV ·
// DSCR). Deliberately the MINIMAL mirror of calcDeal's core — go/no-go
// numbers, labeled as screening; the portal engine remains the pro-forma
// truth on publish. ----
let reAssumptionsCache = null;
function reAssumptions() { return reAssumptionsCache || {}; }
async function loadReAssumptions() {
  try {
    const d = await (await fetch("/api/realestate/assumptions")).json();
    reAssumptionsCache = d.values || {};
    reAssumptionsCache.__labels = d.labels || {};
    reAssumptionsCache.__keys = d.keys || [];
    reAssumptionsCache.__overrides = d.overrides || [];
  } catch (e) { reAssumptionsCache = {}; }
}

function reDebtService(loan, a) {
  const r = (a.perm_interest_rate || 0) / 12;
  const n = (a.perm_amort_years || 25) * 12;
  if (!loan || !r || !n) return 0;
  const m = loan * (r * Math.pow(1 + r, n)) / (Math.pow(1 + r, n) - 1);
  return m * 12;
}

// reScreeningCalc reads a property's tier-1 inputs from its source sidecar
// data (mirrored into the /api payload as units + the source cache the page
// fetches) — here we work off the list payload's units + page-level source
// fetches fill the rest; callers on the deal page get tdc/noi/dscr where the
// inputs exist.
// reSrcNum reads a source-sidecar number, tolerating BOTH shapes: the
// property slice (flat keys) and the single-parcel full-deal object
// (properties[0]) — the same fallback the Go sourceMoney applies.
function reSrcNum(src, key) {
  if (!src) return 0;
  if (src[key] != null) return src[key] || 0;
  return (((src.properties || [])[0] || {})[key]) || 0;
}

function reScreeningCalc(p) {
  const src = p.__source || {};
  const a = reAssumptions();
  // measurables win (overhaul §3.1): the frontmatter unit mix carries the
  // unit count + per-unit rents; the source sidecar is the fallback
  const units = ((p.unitMix || []).length) || p.units || reSrcNum(src, "total_units") || 0;
  const rent = (p.rentMonthly && units) ? p.rentMonthly / units : reSrcNum(src, "avg_rent_per_unit");
  // cost inputs = THE BUDGET's plan figures (owner call 2026-08-19 — the
  // section had its own purchase/hard inputs that disagreed with the budget):
  // hard = Σ rock ests once estimated (else the underwrite figure), plus
  // closing, soft = carry when entered (else the screening approximation)
  const purchase = reSrcNum(src, "purchase_price");
  const closing = reSrcNum(src, "closing_costs");
  const workEst = (p.work || []).reduce((n, st) => n + (st.estTotal || 0), 0);
  const hard = workEst > 0 ? workEst : reSrcNum(src, "hard_costs");
  if (!units || !rent) return { complete: false };
  const gross = units * rent * 12;
  const egi = gross * (1 - (a.vacancy_rate || 0));
  const noi = egi * (1 - (a.opex_rate || 0));
  const carry = reSrcNum(src, "carry_cost");
  const soft = carry > 0 ? carry : hard * 0.15; // budget's soft plan, else the screening approximation
  const contingency = hard * (a.contingency_pct || 0);
  const tdc = purchase + closing + hard + soft + contingency;
  const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
  // Loan sizing (owner report 2026-08-18 "DSCR is ALWAYS 1.22"): at the
  // LTV-max loan DSCR is a CONSTANT of the assumptions — loan = LTV·ARV and
  // ARV = NOI/cap, so NOI cancels: dscr = cap/(ltv·payment). The engine has
  // the same identity; the deal-varying number is DSCR at the TAKEOUT loan —
  // what the perm actually refinances (the construction loan, LTC·TDC),
  // capped by what the appraisal supports (LTV·ARV). When the LTV cap binds
  // the takeout, the shortfall is a refi gap — equity stays in the deal.
  const permMax = arv * (a.perm_ltv || 0);
  const takeout = tdc * (a.construction_loan_ltc || 0);
  const loan = takeout > 0 ? Math.min(permMax, takeout) : permMax;
  const refiGap = takeout > permMax ? takeout - permMax : 0;
  const ads = reDebtService(loan, a);
  return {
    complete: true, gross, egi, noi, tdc, arv, loan, permMax, refiGap,
    purchase, closing, hard, soft, contingency, hardFromWork: workEst > 0,
    dscr: ads ? noi / ads : 0,
  };
}

// ---- PUBLISH → oodagroup — the portal export effector (RE spec §4) ----
// The AION gesture: PREVIEW (nothing written) → CONFIRM carrying the preview
// hash → one commit, pushed. Two contract paths only: deals.json + defaults.js.

async function openRePublishPanel() {
  if (document.getElementById("rePublishModal")) return;
  const overlay = el("div", "cmdbar");
  overlay.id = "rePublishModal";
  const back = el("div", "cmdbar-backdrop");
  const panel = el("div", "cmdbar-card aion-publish-panel");
  overlay.append(back, panel);
  document.body.append(overlay);
  const close = () => overlay.remove();
  back.onclick = close;
  panel.append(el("div", "appr-diff-label", "PUBLISH → oodagroup — preview"));
  const bodyHost = el("div", "aion-publish-body");
  panel.append(bodyHost, el("div", "aion-publish-note", "fetching preview…"));
  let prev;
  try { prev = await (await fetch("/api/re/publish/preview")).json(); }
  catch (e) { panel.lastChild.textContent = "preview failed: " + e; return; }
  panel.lastChild.remove();

  if ((prev.blockers || []).length) {
    prev.blockers.forEach((b) => bodyHost.append(el("div", "appr-blocked", "⚠ " + b)));
    panel.append(pillLight("close", close));
    return;
  }
  const changed = (prev.files || []).filter((f) => f.status !== "unchanged");
  if (!changed.length && !(prev.unpushed > 0)) {
    bodyHost.append(el("div", "aion-publish-note", "nothing to publish — the checkout matches the record."));
    panel.append(pillLight("close", close));
    return;
  }
  if (prev.unpushed > 0) {
    bodyHost.append(el("div", "aion-publish-note", prev.unpushed + " unpushed commit(s) — publish completes the push."));
  }
  if ((prev.kept || []).length) {
    // template deals with no vault record pass through verbatim — say so
    bodyHost.append(el("div", "aion-publish-note", "kept as-is (no vault record): " + prev.kept.join(", ")));
  }
  changed.forEach((f) => {
    const head = el("div", "aion-pub-file");
    head.append(el("code", "", f.path), el("span", "aion-pub-status " + f.status, f.status));
    bodyHost.append(head);
    if (f.diff) bodyHost.append(collapsibleBlock(diffView(f.diff), f.diff.split("\n").length));
  });
  const actions = el("div", "appr-actions");
  const confirmBtn = pill("CONFIRM — commit + push", async () => {
    confirmBtn.disabled = true;
    try {
      const r = await fetch("/api/re/publish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hash: prev.hash }) });
      const res = await r.json().catch(() => ({}));
      if (r.status === 409) { showToast("Vault changed since preview — re-open PUBLISH"); close(); return; }
      if (!r.ok || res.ok === false) {
        showToast("Publish failed at " + (res.stage || "?") + (res.commit ? " (commit " + res.commit.slice(0, 7) + " kept locally)" : ""));
      } else {
        showToast("Published " + (res.commit || "").slice(0, 7) + " → oodagroup");
      }
      close();
      renderProperties();
    } finally { confirmBtn.disabled = false; }
  });
  actions.append(confirmBtn, pillLight("cancel", close));
  panel.append(actions);
}
