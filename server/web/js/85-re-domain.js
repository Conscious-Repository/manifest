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

// reDecisionList / reTaskList — the ONE assembly of real-estate work, shared
// by the BACKLOG page and the rail badge so a count can never drift from the
// rows it claims to describe.
//
// Both lanes merge two sources that spell the same fact differently, which is
// exactly where these counts have gone wrong before: a decision-log item marks
// itself with `status`, while a decision living in a property's rock tree is a
// checked NODE one level down at d.n. Resolve it here, once, where both shapes
// are still in view.
function reDecisionList() {
  const items = reItems();
  return items.filter((it) => it.kind === "decision")
    .map((it) => ({ src: "re", it, decided: it.status === "decided", at: it.decided || "" }))
    .concat(rePropDecisions().map((d) => ({ src: "propdec", it: d, decided: !!d.n.checked, at: "" })));
}

function reTaskList() {
  const out = [];
  reItems().filter((it) => it.kind === "task").forEach((it) =>
    out.push({ src: "re", id: it.id, owner: it.owner || "", done: it.status === "done", it }));
  ((propTodosMeta && propTodosMeta.rows) || []).forEach((r) => {
    if (r.source === "property") out.push({ src: "prop", id: r.id, owner: r.owner || "", done: false, text: r.text, container: r.container });
  });
  propOutstandingGroups().forEach((g) => (g.items || []).forEach((r) =>
    out.push({ src: "prop", id: r.id, owner: r.owner || "", done: false, text: r.text, container: g.container })));
  return out;
}

// reOpenCount is the rail badge: open tasks + open decisions. It counted
// PROPERTIES before (63 tracked parcels), which answered a question nobody
// was asking of a WORK rail.
function reOpenCount() {
  return reTaskList().filter((t) => !t.done).length +
    reDecisionList().filter((d) => !d.decided).length;
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
  // `decided` is normalized HERE, at construction, the way the task lane below
  // normalizes `done` — the two sources keep the flag in different places
  // (a log item's status vs the tree NODE's checkbox, which sits one level
  // down at d.n). Reading it later off the wrapper silently answered
  // undefined for every tree decision, so decided ones sat in the open list
  // wearing a DECIDED badge, and the counts disagreed with the rows.
  const decisions = reDecisionList();
  const openDec = decisions.filter((d) => !d.decided);
  // the archive reads newest-first; tree decisions carry no decided date, so
  // they settle under the dated ones rather than scattering through them
  const decided = decisions.filter((d) => d.decided)
    .sort((a, b) => (b.at || "").localeCompare(a.at || ""));
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
  const tasks = reTaskList();

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
// filter state (fr-toolbar idiom): filter in place via paint(), never a full
// re-render — a re-render replaces the search input and drops the caret
let moneyQuery = "";
let moneyCut = "all"; // all | todo (no category yet) | expense | deposit
let moneyMonth = "all";
// the "do the same for N more like this" offer: armed by any disposition
// gesture (category set, property assigned, split filed) on a row whose
// merchant recurs; dismissed keys stay quiet for the session
let moneyPropose = null; // {key, sourceId, inflow, category, allocs, file}
const moneyProposeDismissed = new Set();
// the current render's paint closure — money mutations repaint in place
// instead of re-rendering the whole tab (the "page refreshes every time I set
// a category" complaint: renderProperties refetches three endpoints and
// rebuilds from scratch, dropping scroll)
let moneyRepaint = null;

// moneyRefresh — refetch the lot, repaint in place, re-render the inspector
async function moneyRefresh() {
  try {
    const d = await (await fetch("/api/realestate/statements")).json();
    moneyRows = d.rows || [];
  } catch (e) {}
  if (moneyRepaint) moneyRepaint(); else { renderProperties(); return; }
  const sel = moneySelId && moneyRows.find((r) => r.id === moneySelId);
  renderMoneyInspector(sel || null);
}

// moneyVisible — the one predicate behind the toolbar (cut → month → text)
function moneyVisible(r) {
  if (moneyCut === "todo" && r.category) return false;
  if (moneyCut === "expense" && r.inflow) return false;
  if (moneyCut === "deposit" && !r.inflow) return false;
  if (moneyMonth !== "all" && !(r.date || "").startsWith(moneyMonth)) return false;
  const q = moneyQuery.trim().toLowerCase();
  if (!q) return true;
  return [r.vendor, r.note, r.category].join(" ").toLowerCase().includes(q);
}

// moneyTargetGroups — which properties a row's money may land on. A row is
// paid by ONE entity (r.entity, the slug); its targets are that entity's own
// properties plus the ones nobody has claimed yet. Another entity's property
// is not a target for this entity's money — intercompany spend exists, but it
// goes through the admin lane deliberately, not through a mispick.
// (Property.entity is the display NAME — bridge via entitySlugFor.)
function moneyTargetGroups(r) {
  const mine = [], untagged = [], other = [];
  activePortfolio().forEach((p) => {
    if (!p.entity) { untagged.push(p); return; }
    if (!r.entity || entitySlugFor(p.entity) === r.entity) mine.push(p);
    else other.push(p);
  });
  return { mine, untagged, other };
}

// moneyDealTargets — deals whose every member passes the same entity test: a
// deal spanning entities is not a coherent target for one entity's money
function moneyDealTargets(r) {
  const g = moneyTargetGroups(r);
  const ok = new Set(g.mine.concat(g.untagged).map((p) => p.slug));
  return dealSplitTargets().filter((x) => x.members.every((p) => ok.has(p.slug)));
}

// moneyTargetOptions — fills a target <select> with the scoped groups. Used
// by the row select, the split editor, and (as groups) the phone sheet.
function moneyTargetOptions(r, sel, withSplit, blankLabel) {
  const opt = (parent, v, l) => {
    const o = document.createElement("option");
    o.value = v; o.textContent = l;
    parent.append(o);
  };
  opt(sel, "", blankLabel || (r.state === "applied" ? "applied" : "unassigned"));
  if (moneyAdminSlug(r)) opt(sel, moneyAdminSlug(r), "admin · " + entityLabel(r.entity));
  const g = moneyTargetGroups(r);
  if (g.mine.length) {
    const og = document.createElement("optgroup");
    og.label = r.entity ? entityLabel(r.entity) : "PROPERTIES";
    g.mine.forEach((p) => opt(og, p.slug, p.short || p.slug));
    sel.append(og);
  }
  if (g.untagged.length) {
    const og = document.createElement("optgroup");
    og.label = "UNASSIGNED OWNER (" + g.untagged.length + ")";
    g.untagged.forEach((p) => opt(og, p.slug, p.short || p.slug));
    sel.append(og);
  }
  if (withSplit) {
    opt(sel, "__split", "split…");
    moneyDealTargets(r).forEach((x) =>
      opt(sel, "deal:" + x.deal.slug, "◈ " + (x.deal.name || x.deal.slug) + " (" + x.members.length + "-way)"));
  }
}

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
function categoryTypeahead(r, onResult, saveFn) {
  const kind = r.inflow ? "income" : "expense";
  const save = saveFn || (async (name) => {
    try {
      const res = await postJSONOk("/api/realestate/statements/row", { id: r.id, category: name, file: true });
      r.category = name;
      onResult(res);
    } catch (e) { showToast("Couldn't save"); }
  });
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

// moneyCatSelect — the category ON the row (workbench v3): filing no longer
// needs the inspector. Registry options grouped by class; "＋ new category…"
// hands off to the inspector's typeahead, so creation stays in one place.
// Picking a category IS a filing gesture (same PATCH the typeahead sends).
function moneyCatSelect(r) {
  const sel = document.createElement("select");
  sel.className = "pp-in re-money-cat";
  const kind = r.inflow ? "income" : "expense";
  const opt = (parent, v, l) => {
    const o = document.createElement("option");
    o.value = v; o.textContent = l;
    parent.append(o);
  };
  opt(sel, "", "category…");
  const cats = (moneyCatsCache || []).filter((c) => c.kind === kind);
  ["operating", "project"].forEach((cls) => {
    const mine = cats.filter((c) => c.class === cls);
    if (!mine.length) return;
    const og = document.createElement("optgroup");
    og.label = cls.toUpperCase();
    mine.forEach((c) => opt(og, c.name, c.name));
    sel.append(og);
  });
  // a category set before the registry loaded (or retyped) still shows
  if (r.category && !cats.some((c) => c.name === r.category)) opt(sel, r.category, r.category);
  opt(sel, "__new", "＋ new category…");
  sel.value = r.category || "";
  sel.disabled = r.state === "applied" || r.state === "skipped";
  sel.onclick = (e) => e.stopPropagation();
  sel.onchange = async () => {
    if (sel.value === "__new") {
      // creation lives in the inspector's typeahead — select the row, focus it
      sel.value = r.category || "";
      moneySelId = r.id;
      renderMoneyInspector(r);
      const ta = document.querySelector(".re-money-insp .ta-in");
      if (ta) ta.focus();
      return;
    }
    try {
      const res = await postJSONOk("/api/realestate/statements/row",
        { id: r.id, category: sel.value, file: true });
      r.category = sel.value;
      if (sel.value) moneyArmSpread(r, res);
      if (moneyFileToast(r, res, sel.value ? "Categorized" : "Category cleared")) moneySelId = null;
      moneyRefresh();
    } catch (e) { showToast("Couldn't save"); }
  };
  return sel;
}

// moneyArmSpread — after any disposition gesture on a row whose merchant
// recurs, arm the offer to replicate it: the category, the assignment (single
// or split — replicated as PROPORTIONS of each row's own amount), and, when
// the source row actually filed, the filing itself. Work/contract tethers are
// deliberately NOT replicated — spreading a contract draw-down across a pile
// of rows could over-draw it. The panel re-derives its matches at paint time,
// so it never goes stale.
function moneyArmSpread(r, res) {
  if (!r.merchantKey || moneyProposeDismissed.has(r.merchantKey)) return;
  const allocs = (r.assignments || []).filter((a) => a.slug);
  if (!r.category && !allocs.length) return; // nothing to spread
  moneyPropose = {
    key: r.merchantKey, sourceId: r.id, inflow: !!r.inflow,
    category: r.category || "",
    allocs: allocs.length ? allocs.map((a) => ({ slug: a.slug, amount: a.amount || 0 })) : null,
    file: !!(res && res.state === "applied"),
  };
}

// moneyProposeMatches — the rows the armed offer could touch: still in play,
// same merchant, same direction (an expense disposition never spreads onto a
// deposit), never the source row itself
function moneyProposeMatches() {
  if (!moneyPropose) return [];
  return moneyRows.filter((r) =>
    r.state !== "applied" && r.state !== "skipped" &&
    !!r.inflow === moneyPropose.inflow && r.id !== moneyPropose.sourceId &&
    r.merchantKey === moneyPropose.key);
}

// moneyScaleAllocs — the source split as fractions of each match's own
// amount, cents-exact (the remainder rides the last slice)
function moneyScaleAllocs(allocs, total) {
  const sum = allocs.reduce((s, a) => s + (a.amount || 0), 0) || 1;
  let acc = 0;
  return allocs.map((a, i) => {
    if (i === allocs.length - 1) return { slug: a.slug, amount: Math.round((total - acc) * 100) / 100 };
    const amt = Math.round((total * (a.amount || 0)) / sum * 100) / 100;
    acc += amt;
    return { slug: a.slug, amount: amt };
  });
}

// moneyDispositionLabel — "4848 50% + 4852 50% · materials" / "736 · electric"
function moneyDispositionLabel(pr) {
  const bits = [];
  if (pr.allocs) {
    const sum = pr.allocs.reduce((s, a) => s + (a.amount || 0), 0) || 1;
    bits.push(pr.allocs.map((a) => {
      const name = a.slug.startsWith("admin:") ? "admin · " + a.slug.slice(6)
        : (((propertyCache || []).find((p) => p.slug === a.slug) || {}).short || a.slug);
      return pr.allocs.length > 1 ? name + " " + Math.round((a.amount || 0) / sum * 100) + "%" : name;
    }).join(" + "));
  }
  if (pr.category) bits.push(pr.category);
  return bits.join(" · ");
}

// moneyRowDisposition — what a match row already carries (shown in the
// preview so an overwrite is a choice, never a surprise)
function moneyRowDisposition(m) {
  const bits = [];
  if ((m.assignments || []).length) {
    bits.push(m.assignments.map((a) => a.slug.startsWith("admin:") ? "admin" :
      (((propertyCache || []).find((p) => p.slug === a.slug) || {}).short || a.slug)).join("+"));
  }
  if (m.category) bits.push(m.category);
  return bits.join(" · ");
}

// ---- bid links (the QuickBooks receipt-attach): expense ↔ contract record ----
// A contract record IS the bid (proposed or accepted); linking writes the
// [contract:: slug] token. Accepted contracts draw down; linking a PROPOSED
// bid offers to accept it (paying against a bid usually means you took it —
// owner call 2026-08-19). A contract's doc (CAS file) is the receipt.

// bidContractOptions fills a contract <select> for one property: accepted
// first (remaining shown), then proposed bids.
function bidContractOptions(copt, propSlug) {
  const mine = reContracts().filter((c) => (c.allocations || []).some((al) => al.property === propSlug));
  mine.filter((c) => c.status === "accepted").forEach((c) =>
    copt(c.slug, c.name + " · " + fmtMoneyShort(c.remaining != null ? c.remaining : c.total) + " left"));
  mine.filter((c) => c.status === "proposed").forEach((c) =>
    copt(c.slug, c.name + " · proposed bid " + fmtMoneyShort(c.total)));
}

// bidReceiptLink — "receipt ↗" when the linked contract carries a doc file
function bidReceiptLink(contractSlug) {
  const c = reContracts().find((x) => x.slug === contractSlug);
  if (!c || !c.doc || !c.doc.startsWith("sha256:")) return null;
  const a = el("a", "pp3-link re-bid-receipt", "receipt ↗");
  a.href = "/api/realestate/files/" + encodeURIComponent(c.doc.slice(7));
  a.target = "_blank"; a.rel = "noopener";
  a.onclick = (e) => e.stopPropagation();
  return a;
}

// offerAcceptBid — inline confirm under the picker when a PROPOSED bid is
// linked: accepting flips the contract (committed money + draw-down turn
// on); declining leaves the link as a plain reference.
function offerAcceptBid(contractSlug, host, onDone) {
  const c = reContracts().find((x) => x.slug === contractSlug);
  if (!c || c.status !== "proposed") { if (onDone) onDone(); return; }
  const strip = el("div", "re-bid-accept");
  strip.append(el("span", "", "accept this bid? (" + fmtMoneyShort(c.total) + " becomes committed)"));
  strip.append(pillLight("accept ✓", async () => {
    try {
      await postJSONOk("/api/realestate/contracts/" + encodeURIComponent(contractSlug) + "/accept", {});
      await loadReContracts();
      showToast("Bid accepted — " + fmtMoneyShort(c.total) + " committed");
      strip.remove();
      if (onDone) onDone();
    } catch (e) { showToast("Couldn't accept — " + (e.message || "")); }
  }));
  strip.append(pillLight("just link", () => { strip.remove(); if (onDone) onDone(); }));
  host.append(strip);
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

// moneySplitLabel — how a multi-target row names itself in the list: the
// deal it was split by when every slice is a member, else "split · N"
function moneySplitLabel(r) {
  const slugs = (r.assignments || []).map((a) => a.slug);
  const hit = dealSplitTargets().find((x) => {
    const members = x.members.map((p) => p.slug);
    return slugs.every((s) => members.includes(s));
  });
  if (hit) return "◈ " + (hit.deal.name || hit.deal.slug) + " · " + slugs.length + "-way";
  return "split · " + slugs.length + (slugs.some((s) => s.startsWith("admin:")) ? " (incl. admin)" : "");
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
      moneyTargetOptions(r, sel, false, "— target —"); // entity-scoped, no split/deal recursion
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
          r.assignments = allocs.filter((a) => a.slug);
          moneyArmSpread(r, res); // offer the same split to the merchant's other rows
          moneySplitSeed = null;
          moneySelId = null;
          if (onDone) onDone();
          moneyRefresh();
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
  copt("", "— no bid / contract —");
  bidContractOptions(copt, a.slug);
  cSel.value = a.contract || "";
  cSel.onchange = () => {
    a.contract = cSel.value;
    if (cSel.value && !nodeSel.value) {
      const c = reContracts().find((x) => x.slug === cSel.value);
      const al = c && (c.allocations || []).find((x) => x.property === a.slug);
      if (al) { a.workId = al.nodeId; nodeSel.value = al.nodeId; }
    }
    if (cSel.value) offerAcceptBid(cSel.value, row.parentNode || row, null);
  };
  row.append(nodeSel, cSel);
  const rec = a.contract && bidReceiptLink(a.contract);
  if (rec) row.append(rec);
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
  await ensureEntities();  // rows carry the entity SLUG — entityLabel needs the registry to read it back
  await ensureMoneyCats(); // the row category select reads the registry synchronously
  const laneHead = el("div", "re-lane-head");
  host.append(laneHead);
  // dead-simple statement upload (overhaul decision 15): drop the bank CSV —
  // the remembered column mapping + entity binding do the rest; a first-seen
  // format opens the one-time mapping strip.
  host.append(moneyUploadLane());
  // the fr-shell triad (PORTFOLIO-LIST handoff): list + sticky right rail
  const wrap = el("div", "aion-backlog fr-shell");
  const main = el("div", "aion-list fr-main");
  const insp = el("aside", "aion-inspector fr-inspector re-money-insp");
  wrap.append(main, insp);
  // toolbar — search · month · cuts. Filter state is module-level; changes
  // repaint in place (a full render would take the caret out of the search).
  const bar = el("div", "fr-toolbar");
  const search = el("input", "pp-in fr-search");
  search.type = "search";
  search.placeholder = "Search description, note, category…";
  search.value = moneyQuery;
  search.oninput = () => { moneyQuery = search.value; moneyPage = 0; paint(); };
  bar.append(search);
  const monthSel = document.createElement("select");
  monthSel.className = "pp-in re-money-month";
  const mopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; monthSel.append(o); };
  mopt("all", "all months");
  [...new Set(moneyRows.filter((r) => r.state !== "applied" && r.state !== "skipped")
    .map((r) => (r.date || "").slice(0, 7)).filter(Boolean))].sort().reverse()
    .forEach((m) => mopt(m, m));
  monthSel.value = moneyMonth;
  if (monthSel.value !== moneyMonth) { moneyMonth = "all"; monthSel.value = "all"; } // stale month filtered away
  monthSel.onchange = () => { moneyMonth = monthSel.value; moneyPage = 0; paint(); };
  bar.append(monthSel);
  const chips = {};
  [["all", "ALL"], ["todo", "TO CATEGORIZE"], ["expense", "EXPENSES"], ["deposit", "DEPOSITS"]]
    .forEach(([key, label]) => {
      const b = el("button", "filter-chip", label);
      b.onclick = () => { moneyCut = key; moneyPage = 0; paint(); };
      chips[key] = b;
      bar.append(b);
    });
  main.append(bar);
  // durable containers — paint() only wipes contents. History lives in its
  // own slot so a filed row moves lanes without a full re-render.
  const proposeSlot = el("div");
  const table = el("div", "fr-table");
  const pager = el("div", "re-money-pager");
  const histSlot = el("div");
  main.append(proposeSlot, table, pager, histSlot);
  const paint = () => {
    // the split (owner call 2026-08-19): the TOP lane holds only what still
    // needs filing; a filed/dismissed row lives in its entity's history fold
    const work = moneyRows.filter((r) => r.state !== "applied" && r.state !== "skipped");
    const filed = moneyRows.filter((r) => r.state === "applied" || r.state === "skipped");
    Object.keys(chips).forEach((k) => chips[k].classList.toggle("on", moneyCut === k));
    const rows = work.filter(moneyVisible);
    laneHead.textContent = "MONEY · " +
      (rows.length === work.length ? work.length : rows.length + " of " + work.length) +
      " to file · " + filed.length + " filed" + (last ? " · last import " + last : "");
    moneyProposePanel(proposeSlot);
    table.innerHTML = "";
    const head = el("div", "fr-row fr-head re-money-grid");
    ["DATE", "DESCRIPTION", "AMOUNT", "PROPERTY", "CATEGORY"].forEach((h, i) =>
      head.append(el("span", "micro-label" + (i === 2 ? " re-money-r" : ""), h)));
    table.append(head);
    if (!rows.length) {
      table.append(emptyRow(moneyRows.length
        ? (work.length ? "No transactions match." : "Nothing left to file — everything lives in the entity histories below.")
        : "No transactions yet — link a bank account or drop a CSV above."));
    }
    // pagination — historic backfills run to hundreds of rows; 50 per page
    const pages = Math.max(1, Math.ceil(rows.length / MONEY_PAGE_SIZE));
    if (moneyPage >= pages) moneyPage = pages - 1;
    const start = moneyPage * MONEY_PAGE_SIZE;
    rows.slice(start, start + MONEY_PAGE_SIZE).forEach((r) => table.append(moneyRow(r)));
    pager.innerHTML = "";
    if (pages > 1) {
      const btn = (label, disabled, go) => {
        const b = el("button", "pill light", label);
        b.disabled = disabled;
        b.onclick = () => { moneyPage = go; moneySelId = null; paint(); };
        pager.append(b);
      };
      btn("← prev", moneyPage === 0, moneyPage - 1);
      pager.append(el("span", "re-money-pageinfo",
        (start + 1) + "–" + Math.min(start + MONEY_PAGE_SIZE, rows.length) + " of " + rows.length +
        " · page " + (moneyPage + 1) + "/" + pages));
      btn("next →", moneyPage >= pages - 1, moneyPage + 1);
    }
    // HISTORY — filed rows under the entity whose books they hit
    histSlot.innerHTML = "";
    if (filed.length) moneyHistory(histSlot, filed);
  };
  paint();
  moneyRepaint = paint; // money mutations repaint in place from here on
  host.append(wrap);
  // a selected row survives re-renders (the split…/deal picks re-render to
  // open the editor — the inspector must come back up with them)
  const selRow = moneySelId && moneyRows.find((r) => r.id === moneySelId);
  renderMoneyInspector(selRow || null);
  // footer: accountant handoffs by entity books, personal vs partnered
  main.append(moneyFooter());
}

// moneyProposePanel — the "do the same for N more like this" offer, rendered
// into its durable slot on every paint. Matches re-derive from the live rows;
// each is a checkbox (rows that would be OVERWRITTEN start unchecked, with
// their current disposition shown); apply replicates the category and the
// assignment as proportions of each row's own amount — and files, when the
// source row filed. Dismiss stays quiet for this merchant for the session.
// It proposes — it never acts alone.
function moneyProposePanel(slot) {
  slot.innerHTML = "";
  const matches = moneyProposeMatches();
  if (!moneyPropose || !matches.length) { moneyPropose = null; return; }
  const pr = moneyPropose;
  const box = el("div", "re-money-propose");
  const head = el("div", "re-money-propose-head");
  const verb = pr.file ? "File" : (pr.allocs ? "Assign" : "Categorize");
  head.append(el("span", "", verb + " " + matches.length + " more like this?"));
  head.append(el("span", "re-money-propose-key", moneyDispositionLabel(pr) + " → " + pr.key));
  box.append(head);
  // default-checked = rows this wouldn't overwrite; a row already carrying a
  // different disposition is offered unchecked
  const defaultOn = (m) => {
    if ((m.assignments || []).length && pr.allocs) return false;
    if (m.category && pr.category && m.category !== pr.category) return false;
    return true;
  };
  const checks = new Map(); // id → checkbox
  const listBox = el("div", "re-money-propose-list");
  const SHOW = 6;
  let revealed = matches.length <= SHOW ? matches.length : SHOW;
  const renderList = () => {
    listBox.innerHTML = "";
    matches.slice(0, revealed).forEach((m) => {
      const line = el("label", "re-money-propose-row");
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = checks.has(m.id) ? checks.get(m.id).checked : defaultOn(m);
      checks.set(m.id, cb);
      line.append(cb,
        el("span", "re-money-propose-date", (m.date || "").slice(5)),
        el("span", "re-money-propose-amt", fmtMoneyExact(Math.abs(m.amount || 0))),
        el("span", "re-money-propose-vendor", m.vendor || ""),
        el("span", "re-money-propose-cur", moneyRowDisposition(m)));
      listBox.append(line);
    });
    if (revealed < matches.length) {
      const more = el("button", "o-ghost", "… show all " + matches.length);
      more.onclick = () => { revealed = matches.length; renderList(); };
      listBox.append(more);
    }
  };
  renderList();
  box.append(listBox);
  const acts = el("div", "re-money-propose-acts");
  const apply = el("button", "pill", "apply");
  apply.onclick = async () => {
    // hidden rows keep their DEFAULT — unseen ≠ unchecked
    const picked = matches.filter((m) => checks.has(m.id) ? checks.get(m.id).checked : defaultOn(m));
    if (!picked.length) { moneyPropose = null; moneyRefresh(); return; }
    apply.disabled = true;
    apply.textContent = "applying…";
    try {
      if (!pr.allocs) {
        // category only — the bulk lane, one save
        const res = await postJSONOk("/api/realestate/statements/categorize",
          { ids: picked.map((m) => m.id), category: pr.category });
        showToast("Categorized " + (res.updated || 0) + " × " + pr.category);
      } else {
        // full disposition — each row through the SAME row PATCH a hand-filed
        // row takes (validation, auto-apply, vendor memory all identical)
        let filed = 0, staged = 0, failed = 0;
        for (const m of picked) {
          const body = {
            id: m.id,
            assignments: moneyScaleAllocs(pr.allocs, Math.abs(m.amount || 0)),
            state: pr.allocs.length > 1 ? "split" : "assigned",
          };
          if (pr.category) body.category = pr.category;
          if (pr.file) body.file = true;
          try {
            const res = await postJSONOk("/api/realestate/statements/row", body);
            if (res.state === "applied") filed++; else staged++;
          } catch (e) { failed++; }
        }
        const bits = [];
        if (filed) bits.push("filed " + filed);
        if (staged) bits.push("staged " + staged);
        if (failed) bits.push(failed + " failed");
        showToast(bits.join(" · ") + " × " + moneyDispositionLabel(pr));
      }
      moneyPropose = null;
      moneyRefresh();
    } catch (e) {
      showToast("Couldn't apply — " + (e.message || ""));
      apply.disabled = false;
      apply.textContent = "apply";
    }
  };
  const dismiss = el("button", "pill light", "dismiss");
  dismiss.onclick = () => {
    moneyProposeDismissed.add(pr.key);
    moneyPropose = null;
    slot.innerHTML = "";
  };
  acts.append(apply, dismiss);
  box.append(acts);
  slot.append(box);
}

// moneyHistory — one collapsed fold per entity: every filed (applied) and
// dismissed row, newest first, 50 at a time.
const moneyHistShown = {}; // entity → rows revealed
function moneyHistory(host, filed) {
  host.append(el("div", "re-lane-head re-money-hist-head", "HISTORY — FILED BY ENTITY"));
  const groups = {};
  filed.forEach((r) => { (groups[r.entity || ""] = groups[r.entity || ""] || []).push(r); });
  Object.keys(groups).sort().forEach((ent) => {
    const rows = groups[ent];
    rows.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
    const skipped = rows.filter((r) => r.state === "skipped").length;
    const sum = rows.reduce((s, r) => s + (r.inflow ? 0 : r.amount || 0), 0);
    const body = collapsibleSection(host, entityLabel(ent) || "(no entity)",
      rows.length + " rows · " + fmtMoneyExact(sum) + " out" + (skipped ? " · " + skipped + " skipped" : ""),
      !!moneyHistShown[ent]);
    const shown = moneyHistShown[ent] || MONEY_PAGE_SIZE;
    rows.slice(0, shown).forEach((r) => body.append(moneyRow(r)));
    if (rows.length > shown) {
      const more = el("button", "o-ghost", "＋ show " + Math.min(MONEY_PAGE_SIZE, rows.length - shown) + " more");
      more.onclick = () => { moneyHistShown[ent] = shown + MONEY_PAGE_SIZE; if (moneyRepaint) moneyRepaint(); else renderProperties(); };
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
  // the sign convention has to be remembered too — a mapping learned before it
  // existed re-opens the strip rather than risking an inverted file
  const ready = mapping.date && mapping.amount && mapping.vendor && d.entity && d.signRemembered;
  if (ready && d.remembered) {
    moneyIngest(d, mapping, d.entity, d.sign);
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
      (es.entities || []).forEach((x) => eopt(x.slug, x.name)); // value = slug, the stored identity
      ent.value = d.entity || "";
    } catch (e) {}
  })();
  const ef = el("label", "pp3-lform-field");
  ef.append(el("span", "pp3-lform-label", "paying entity"), ent);
  grid.append(ef);
  // which sign is a charge — banks disagree, and guessing wrong books every
  // expense as income. Suggested from the file, confirmed here, remembered.
  const sign = document.createElement("select");
  sign.className = "pp-in";
  [["expense-negative", "charges are −, deposits +"], ["expense-positive", "charges are +, deposits −"]]
    .forEach(([v, l]) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sign.append(o); });
  sign.value = d.sign || "expense-negative";
  const sf = el("label", "pp3-lform-field");
  sf.append(el("span", "pp3-lform-label", "amount sign"), sign);
  grid.append(sf);
  strip.append(grid);
  const counts = d.signCounts || {};
  if (counts.negative || counts.positive) {
    strip.append(el("div", "re-foot-note",
      "this file has " + (counts.negative || 0) + " negative and " + (counts.positive || 0) + " positive amounts"));
  }
  const actions = el("div", "pp3-uw-actions");
  const cancel = el("button", "pp3-uw-cancel", "cancel");
  cancel.onclick = () => strip.remove();
  const go = el("button", "pp3-compose-go", "ingest ↵");
  go.onclick = () => {
    const m = { date: sel.date.value, vendor: sel.vendor.value, amount: sel.amount.value, note: sel.note.value };
    if (!m.date || !m.vendor || !m.amount) { showToast("Map date, vendor, and amount"); return; }
    if (!ent.value) { showToast("Pick the paying entity"); return; }
    moneyIngest(d, m, ent.value, sign.value);
  };
  actions.append(cancel, go);
  strip.append(actions);
  lane.append(strip);
}

async function moneyIngest(d, mapping, entity, sign) {
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
      label: d.label, entity, signature: d.signature, sign, mapping, rows,
    });
    // duplicates are dropped silently by the dedupe key; suspects are same-day
    // same-amount rows it could NOT confirm, and they DID land — say so
    let msg = "Ingested " + (res.added != null ? res.added : rows.length) + " rows";
    if (res.duplicates) msg += " · " + res.duplicates + " duplicate" + (res.duplicates === 1 ? "" : "s") + " skipped";
    if (res.suspects) msg += " · " + res.suspects + " may already be here — check the dates";
    if (res.unparsedDates) msg += " · " + res.unparsedDates + " unreadable date" + (res.unparsedDates === 1 ? "" : "s") + " dropped";
    showToast(msg + " — assign below, then apply");
    renderProperties();
  } catch (e) { showToast("Ingest failed — " + String(e.message || e).slice(0, 120)); }
}

function moneyRow(r) {
  const cur = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
  const row = el("div", "fr-row re-money-grid" + (moneySelId === r.id ? " sel" : ""));
  row.append(el("span", "re-money-date", (r.date || "").slice(5)));
  // the fundraising two-line cell: vendor over its mono metadata
  const desc = el("span", "fr-stack");
  desc.append(el("span", "re-money-vendor", r.vendor || r.note || "(no description)"));
  const bits = [];
  if (r.entity) bits.push(entityLabel(r.entity));
  if (r.source === "feed") bits.push("feed"); // bank-feed sync vs csv upload
  if (r.note && r.note !== r.vendor) bits.push(r.note);
  if (bits.length) {
    const sub = el("span", "fr-sub", bits.join(" · "));
    sub.title = bits.join(" · ");
    desc.append(sub);
  }
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
  // multi-target rows name their split instead of pretending one property
  if ((r.assignments || []).length > 1) {
    const lab = el("span", "re-money-split-label", moneySplitLabel(r));
    lab.title = (r.assignments || []).map((a) =>
      (a.slug.startsWith("admin:") ? "admin · " + a.slug.slice(6) : a.slug) + " " + fmtMoneyExact(a.amount)).join(" · ");
    row.append(lab, moneyCatSelect(r));
    row.onclick = () => {
      if (window.mf && window.mf.phone()) { openMoneyAssignSheet(r); return; }
      moneySelId = moneySelId === r.id ? null : r.id;
      renderMoneyInspector(r);
    };
    return row;
  }
  // property select — assignment in place, scoped to the paying entity's own
  // properties + the unclaimed ones (workbench v3). The admin lane files
  // against the entity's own books; splits/deals open the editor seeded.
  const sel = document.createElement("select");
  sel.className = "pp-in re-money-sel";
  moneyTargetOptions(r, sel, true);
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
      if (moneyRepaint) moneyRepaint();
      renderMoneyInspector(r);
      return;
    }
    try {
      const assigns = sel.value ? [{ slug: sel.value, amount: Math.abs(r.amount || 0) }] : [];
      const res = await postJSONOk("/api/realestate/statements/row", {
        id: r.id, assignments: assigns,
        state: sel.value ? "assigned" : "pending",
        file: true,
      });
      r.assignments = assigns;
      if (sel.value) moneyArmSpread(r, res);
      if (moneyFileToast(r, res, sel.value ? "Assigned — set a category to file it" : "Unassigned")) {
        moneySelId = null;
      }
      moneyRefresh();
    } catch (e) { showToast("Couldn't assign"); }
  };
  row.append(sel);
  // category ON the row — filing no longer needs the inspector; the inspector
  // is for splits, hops, and notes
  row.append(moneyCatSelect(r));
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
      if (r.state !== "applied") {
        body.append(el("div", "pp3-insp-note", "skipped — unskip from the desktop workbench"));
        return;
      }
      // filed-edit on phone: category + note write through to the ledger
      // row(s); unfile pulls the transaction back for reassignment
      const refile = async (patch, okMsg) => {
        try {
          await postJSONOk("/api/realestate/statements/" + encodeURIComponent(r.id) + "/refile", patch);
          showToast(okMsg || "Ledger row(s) updated");
        } catch (e) { showToast("Couldn't update — " + (e.message || "")); }
      };
      const ta = categoryTypeahead(r, () => {}, async (name) => { r.category = name; await refile({ category: name }); });
      body.append(ta.el);
      const noteIn2 = inputEl("note — rewrites the ledger row(s)");
      noteIn2.value = r.note || "";
      noteIn2.onchange = () => { r.note = noteIn2.value; refile({ note: noteIn2.value }); };
      body.append(noteIn2);
      const unfile = el("button", "pill light re-money-unfile", "unfile ← back to the lot");
      unfile.onclick = async () => {
        await refile({ unfile: true }, "Unfiled — back in the to-file lane");
        window.mfSheet.close();
        moneyRefresh();
      };
      body.append(unfile);
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
        r.assignments = slug ? [{ slug, amount: Math.abs(r.amount || 0) }] : [];
        if (slug) moneyArmSpread(r, res);
        moneyFileToast(r, res, slug ? "Assigned — set a category to file it" : "Unassigned");
        window.mfSheet.close();
        moneyRefresh();
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
      if (moneyAdminSlug(r)) rowOpt(moneyAdminSlug(r), "admin · " + entityLabel(r.entity));
      // entity-scoped targets (workbench v3): the payer's own properties
      // first, then the unclaimed ones — another entity's never listed
      const groups = moneyTargetGroups(r);
      groups.mine.forEach((p) => rowOpt(p.slug, p.short || p.slug));
      groups.untagged.forEach((p) => rowOpt(p.slug, (p.short || p.slug) + " · unowned"));
      rowOpt("__split", "split…", () => {
        moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "__split") };
        openMoneyAssignSheet(r);
      });
      moneyDealTargets(r).forEach((x) => rowOpt("deal:" + x.deal.slug,
        "◈ " + (x.deal.name || x.deal.slug) + " (" + x.members.length + "-way)", () => {
          moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "deal:" + x.deal.slug) };
          openMoneyAssignSheet(r);
        }));
      body.append(list);
    }
    // category — the chart-of-accounts typeahead (phone rows could never
    // complete filing without it); picking a category files the row
    const catTa = categoryTypeahead(r, (res) => {
      moneyArmSpread(r, res); // the offer shows in the list on repaint
      moneyFileToast(r, res, "Saved");
      if (res.state === "applied") { window.mfSheet.close(); moneyRefresh(); }
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
  if (!r || moneySelId !== r.id) {
    insp.innerHTML = "";
    insp.append(el("div", "aion-insp-empty",
      "select a transaction — splits, work tethers, and notes live here; property + category file straight from the row"));
    return;
  }
  insp.innerHTML = "";
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "TRANSACTION"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { moneySelId = null; renderMoneyInspector(null); };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "re-money-insp-title", (r.vendor || "") + " · " + (r.date || "")));
  const fieldRow = (label, val) => {
    const f = el("div", "aion-insp-field fr-insp-field");
    f.append(el("span", "aion-insp-flabel", label), el("span", "aion-insp-ro", val || "—"));
    return f;
  };
  insp.append(fieldRow("amount", (r.inflow ? "+" : "") + fmtMoneyExact(Math.abs(r.amount || 0))));
  insp.append(fieldRow("entity", entityLabel(r.entity)));
  insp.append(fieldRow("statement", r.statement + (r.source === "feed" ? " · feed" : "")));
  insp.append(fieldRow("state", r.state));
  // three edit modes: to-file rows edit freely; FILED rows edit in place
  // through the refile lane (category/note/bid links rewrite the written
  // ledger rows; unfile pulls them back); skipped rows stay read-only.
  const isApplied = r.state === "applied";
  const locked = r.state === "skipped";
  const refileSave = (patch, okMsg) => async () => {
    try {
      await postJSONOk("/api/realestate/statements/" + encodeURIComponent(r.id) + "/refile", patch());
      showToast(okMsg || "Ledger row(s) updated");
      moneyRefresh();
    } catch (e) { showToast("Couldn't update — " + (e.message || "")); }
  };
  // category + note are owner-editable until apply (bank plan §5): the bank
  // memo arrives as the initial note; the row note IS the ledger note
  const editRow = (label, key, placeholder) => {
    if (locked) return fieldRow(label, r[key]);
    const f = el("div", "aion-insp-field fr-insp-field");
    const inp = inputEl(placeholder);
    inp.className = "pp-in fr-in";
    inp.value = r[key] || "";
    inp.onchange = async () => {
      if (isApplied) {
        r[key] = inp.value;
        await refileSave(() => ({ [key]: inp.value }))();
        return;
      }
      try {
        const patch = { id: r.id, file: key === "category" };
        patch[key] = inp.value;
        const res = await postJSONOk("/api/realestate/statements/row", patch);
        r[key] = inp.value;
        if (moneyFileToast(r, res, "Saved")) {
          // the category completed the filing — the row leaves the lot
          moneySelId = null;
          moneyRefresh();
        }
      } catch (e) { showToast("Couldn't save"); }
    };
    f.append(el("span", "aion-insp-flabel", label), inp);
    return f;
  };
  if (locked) {
    insp.append(fieldRow("category", r.category));
  } else {
    // the chart-of-accounts typeahead — picking a category files the row
    // (or, on a filed row, rewrites the written ledger rows)
    const f = el("div", "aion-insp-field fr-insp-field");
    const ta = categoryTypeahead(r, (res) => {
      moneyArmSpread(r, res); // offer the disposition to the merchant's other rows
      if (moneyFileToast(r, res, "Saved")) moneySelId = null;
      moneyRefresh();
    }, isApplied ? async (name) => {
      r.category = name;
      await refileSave(() => ({ category: name }))();
    } : null);
    f.append(el("span", "aion-insp-flabel", "category"), ta.el);
    insp.append(f);
  }
  insp.append(editRow("note", "note", "note — lands on the ledger row"));
  const splitting = !isApplied && !locked &&
    ((moneySplitSeed && moneySplitSeed.id === r.id) || (r.assignments || []).length > 1);
  if (isApplied) {
    // FILED TO — per-slice bid/node links edit in place; unfile to move money
    insp.append(el("div", "micro-label re-filed-head", "FILED TO"));
    r.assignments.forEach((a, j) => {
      const line = el("div", "re-filed-slice");
      const target = a.slug.startsWith("admin:") ? "admin · " + a.slug.slice(6)
        : (((propertyCache || []).find((p) => p.slug === a.slug) || {}).short || a.slug);
      line.append(el("span", "re-filed-target",
        target + (r.assignments.length > 1 ? " · " + fmtMoneyExact(a.amount) : "")));
      insp.append(line);
      if (!a.slug.startsWith("admin:")) {
        const prop = (propertyCache || []).find((p) => p.slug === a.slug);
        if (prop) {
          const work = { ...a };
          const hopRow = splitHopRow(work, prop);
          // any hop change on slice j refiles the FULL assignment set
          [...hopRow.querySelectorAll("select")].forEach((selEl) => {
            selEl.addEventListener("change", refileSave(() => {
              const assignments = r.assignments.map((x, k) => (k === j ? { ...x, workId: work.workId || "", contract: work.contract || "" } : x));
              r.assignments = assignments;
              return { assignments };
            }, "Bid link updated on the ledger row"));
          });
          insp.append(hopRow);
        }
      }
    });
    const unfile = el("button", "pill light re-money-unfile", "unfile ← back to the lot");
    unfile.title = "deletes the written ledger row(s); the transaction returns to the to-file lane for reassignment";
    unfile.onclick = async () => {
      try {
        await postJSONOk("/api/realestate/statements/" + encodeURIComponent(r.id) + "/refile", { unfile: true });
        showToast("Unfiled — back in the to-file lane");
        moneySelId = r.id;
        moneyRefresh();
      } catch (e) { showToast("Couldn't unfile — " + (e.message || "")); }
    };
    insp.append(unfile);
  } else if (splitting) {
    insp.append(moneySplitEditor(r));
  } else {
    moneyHopFields(r, insp);
    if (!locked) {
      const splitLink = el("button", "pp3-link re-split-open", "split across properties…");
      splitLink.onclick = () => {
        moneySplitSeed = { id: r.id, allocs: moneySplitSeedFor(r, "__split") };
        if (moneyRepaint) moneyRepaint();
        renderMoneyInspector(r);
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
          moneyArmSpread(r, res);
          moneySelId = null;
        }
        moneyRefresh();
      } catch (e) { showToast("Couldn't file — " + (e.message || "")); }
    };
    insp.append(fileBtn);
  }
  // claim a property for the paying entity (workbench v3): the picker only
  // offers this entity's own properties + the unclaimed ones — claiming
  // writes `entity:` into the property record (the same hard-linked /field
  // patch the board uses) and shrinks the unclaimed group for good. An
  // explicit click, one property at a time, never inferred.
  if (!isApplied && !locked && r.entity) moneyClaimField(r, insp);
  insp.append(el("div", "re-money-insp-note",
    "picking a property with a category set files straight to the ledger; receipts + contractor attach on the ledger row (property page → spend)"));
}

// moneyClaimField — "⊕ claim a property for <entity>": a typeahead over the
// owner-unassigned properties; picking one stamps its entity: frontmatter.
function moneyClaimField(r, host) {
  const untagged = moneyTargetGroups(r).untagged;
  if (!untagged.length) return;
  const open = el("button", "pp3-link re-money-claim", "⊕ claim a property for " + entityLabel(r.entity) + " …");
  open.onclick = () => {
    const ta = typeahead({
      placeholder: "property to claim…",
      minChars: 0,
      suggest: async (q, add) => {
        const needle = (q || "").toLowerCase();
        untagged.filter((p) => !needle || (p.short || p.slug).toLowerCase().includes(needle) ||
            (p.address || "").toLowerCase().includes(needle))
          .slice(0, 8)
          .forEach((p) => add(p.short || p.slug, "unowned", async () => {
            try {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field",
                { key: "entity", value: entityLabel(r.entity) });
              showToast((p.short || p.slug) + " → " + entityLabel(r.entity));
              await loadProperties(); // the picker regroups off the fresh cache
              moneyRefresh();
            } catch (e) { showToast("Couldn't claim — " + (e.message || "")); }
          }));
      },
    });
    open.replaceWith(ta.el);
    ta.focus();
  };
  host.append(open);
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
        moneyRefresh();
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
  copt("", "— no bid / contract —");
  bidContractOptions(copt, a.slug);
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
    if (cSel.value) offerAcceptBid(cSel.value, host, null);
  };
  field("bid / contract", cSel);
  const rec = a.contract && bidReceiptLink(a.contract);
  if (rec) host.append(rec);
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

// reScreeningCalc — the cockpit's call shape. The math itself moved to
// 77-re-screening.js so the OODA portal can run the SAME function rather than
// a second implementation that would drift (see that file's header).
// reSrcNum and reDebtService are globals from there too.
function reScreeningCalc(p) {
  return reScreen(p, p.__source || {}, reAssumptions());
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
