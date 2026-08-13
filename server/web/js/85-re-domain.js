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
      (c.name || "").toLowerCase().includes(q) || (c.trade || "").toLowerCase().includes(q))
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
  // 2026-08-12 — the aion pattern): no intake lane here. The async 90-card
  // insert was also what yanked the scroll position back to the top.

  // -- decisions lane --
  const items = reItems();
  const decisions = items.filter((it) => it.kind === "decision");
  const openDec = decisions.filter((it) => it.status !== "decided");
  const decided = decisions.filter((it) => it.status === "decided");
  const lane = el("div", "aion-dec-lane");
  const lh = el("div", "aion-sec-label");
  lh.append(el("span", "aion-sec-title", "◇ Decisions"),
    el("span", "aion-sec-count", openDec.length + " open · " + decided.length + " decided"));
  lh.append(ghostInput("＋ decision", "aion-add aion-sec-add", (v) =>
    rePost("/api/re/backlog/item", { kind: "decision", title: v }, "Decision added")));
  lane.append(lh);
  openDec.forEach((it) => lane.append(reBacklogDecRow(it)));
  if (decided.length) {
    const t = el("button", "aion-done-toggle", (reDecidedOpen ? "▾" : "▸") + " decided · " + decided.length);
    t.onclick = () => { reDecidedOpen = !reDecidedOpen; renderProperties(); };
    lane.append(t);
    if (reDecidedOpen) decided.forEach((it) => lane.append(reBacklogDecRow(it)));
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
  const bucket = (owner) => (mineOwner(owner) ? ME : owner.toUpperCase());
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
    rePost("/api/re/backlog/" + it.id + "/update", { status: done ? "open" : "done" });
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

// property-todo row — the Outstanding fold. Check writes through /api/todos;
// the property name rides the rock-tag column; the row selects into a light
// inspector that links to the property.
function rePropTodoRow(t) {
  const sel = reBacklogSelId === t.id && reBacklogSelSrc === "prop";
  const row = el("div", "aion-task-row" + (sel ? " sel" : ""));
  const c = el("button", "aion-check", "○");
  c.title = "mark done";
  c.onclick = (e) => { e.stopPropagation(); rePost("/api/todos/check", { id: t.id, checked: true }, "Marked done"); };
  row.append(c);
  const main = el("div", "aion-main");
  main.append(el("div", "aion-title", t.text));
  main.append(el("div", "aion-item-meta", "property todo"));
  row.append(main);
  row.append(el("span", "aion-rock-tag", t.container ? (t.container.name || t.container.slug || "") : ""));
  row.onclick = () => reBacklogSelect("prop", t.id);
  return row;
}

// ---- the inspector (mirrors renderAionInspector; the list never reflows) ----
function renderREBacklogInspector(insp) {
  if (reBacklogSelSrc === "prop") { rePropTodoInspector(insp); return; }
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

  const patch = (set, msg) => rePost("/api/re/backlog/" + it.id + "/update", set, msg);

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

  if (it.kind === "task") {
    const setRock = (v) => { if (v !== (it.rock || "")) patch({ rock: v }); };
    const rockTa = typeahead({
      placeholder: "rock or property",
      initial: it.rock ? reRockLabel(it.rock) : "",
      suggest: (q, add, ta) => reRockSuggest(q, add, ta, setRock),
    });
    field("rock", rockTa.el);
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
        rePost("/api/re/backlog/" + it.id + "/decide", { outcome: outcome.value.trim() }, "Decided — permanent log");
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
    yes.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; rePost("/api/re/backlog/" + it.id + "/delete", {}, "Deleted"); };
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
  head.append(el("span", "aion-insp-label", "Todo"));
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
  done.onclick = () => { reBacklogSelId = null; reBacklogSelSrc = null; rePost("/api/todos/check", { id, checked: true }, "Marked done"); };
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

async function renderREMoney() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  let last = "";
  try {
    const d = await (await fetch("/api/realestate/statements")).json();
    moneyRows = d.rows || [];
    last = d.lastImport || "";
  } catch (e) { moneyRows = []; }
  const unfiled = moneyRows.filter((r) => r.state === "pending").length;
  host.append(el("div", "re-lane-head",
    "MONEY · " + moneyRows.length + " transactions" + (unfiled ? " · " + unfiled + " uncategorized" : "") +
    (last ? " · last import " + last : "")));
  if (!moneyRows.length) {
    host.append(emptyRow("No transactions — import a bank statement CSV (Settings → Entities names the accounts)."));
  }
  const cols = el("div", "re-money-cols");
  ["DATE", "DESCRIPTION", "AMOUNT", "PROPERTY"].forEach((h, i) =>
    cols.append(el("span", i === 2 ? "prop-col-r" : "", h)));
  host.append(cols);
  const shell = el("div", "re-money-shell");
  const list = el("div", "re-money-list");
  moneyRows.forEach((r) => list.append(moneyRow(r)));
  shell.append(list);
  const insp = el("div", "re-money-insp");
  insp.hidden = true; // starts closed — the list arrives full width
  shell.append(insp);
  host.append(shell);
  // footer: accountant handoffs by entity books, personal vs partnered
  host.append(moneyFooter());
}

function moneyRow(r) {
  const cur = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
  const row = el("div", "re-money-row" + (moneySelId === r.id ? " sel" : ""));
  row.append(el("span", "re-money-date", (r.date || "").slice(5)));
  const desc = el("span", "re-money-desc");
  desc.append(el("span", "", r.vendor || r.note || "(no description)"));
  if (r.entity) desc.append(el("span", "re-money-entity", r.entity));
  // phone meta line (desktop hides it): the assigned property, or the file
  // prompt in ink — the row tap opens the assignment sheet
  const curProp = cur ? activePortfolio().find((p) => p.slug === cur) : null;
  desc.append(el("span", "re-money-meta" + (cur ? "" : " unfiled"),
    cur ? ((curProp && (curProp.short || curProp.slug)) || cur) : "unassigned — tap to file"));
  row.append(desc);
  row.append(el("span", "re-money-amt" + (r.inflow ? " inflow" : ""),
    (r.inflow ? "+" : "") + fmtMoneyShort(Math.abs(r.amount || 0))));
  // property select — assignment in place (single-target; splits via inspector)
  const sel = document.createElement("select");
  sel.className = "pp-in re-money-sel";
  const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); };
  opt("", r.state === "applied" ? "applied" : "unassigned");
  activePortfolio().forEach((p) => opt(p.slug, p.short || p.slug));
  sel.value = cur;
  sel.disabled = r.state === "applied" || r.state === "skipped";
  sel.onclick = (e) => e.stopPropagation();
  sel.onchange = async () => {
    try {
      await postJSONOk("/api/realestate/statements/row", {
        id: r.id,
        assignments: sel.value ? [{ slug: sel.value, amount: Math.abs(r.amount || 0) }] : [],
        state: sel.value ? "assigned" : "pending",
      });
      showToast(sel.value ? "Assigned — apply writes it to the ledger" : "Unassigned");
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
      (r.inflow ? "+" : "") + fmtMoneyShort(Math.abs(r.amount || 0))));
    if (done) {
      body.append(el("div", "pp3-insp-note", "already " + r.state + " — edits happen on the property ledger"));
      return;
    }
    const list = el("div", "mf-assign");
    const assign = async (slug) => {
      try {
        await postJSONOk("/api/realestate/statements/row", {
          id: r.id,
          assignments: slug ? [{ slug, amount: Math.abs(r.amount || 0) }] : [],
          state: slug ? "assigned" : "pending",
        });
        showToast(slug ? "Assigned — apply writes it to the ledger" : "Unassigned");
        window.mfSheet.close();
        renderProperties();
      } catch (e) { showToast("Couldn't assign"); }
    };
    const rowOpt = (v, l) => {
      const b = el("button", "mf-opt" + (v === cur ? " on" : ""));
      b.append(el("span", "mf-opt-dot", v === cur ? "●" : "○"), el("span", "", l));
      b.onclick = () => assign(v);
      list.append(b);
    };
    rowOpt("", "unassigned");
    activePortfolio().forEach((p) => rowOpt(p.slug, p.short || p.slug));
    body.append(list);
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
  insp.append(fieldRow("amount", (r.inflow ? "+" : "") + "$" + Math.abs(r.amount || 0).toLocaleString()));
  insp.append(fieldRow("entity", r.entity));
  insp.append(fieldRow("statement", r.statement));
  insp.append(fieldRow("state", r.state));
  insp.append(fieldRow("category", r.category));
  if (r.note) insp.append(fieldRow("note", r.note));
  insp.append(el("div", "pp3-insp-note",
    "assignment writes to the property ledger on apply; receipts + contractor attach on the ledger row (property page → spend)"));
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
  if (!deal) { host.append(emptyRow("No deal record named " + slug + ".")); return; }
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
    try { p.__source = await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json(); }
    catch (e) { p.__source = {}; }
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
  const ads = reDebtService(arv * (a.perm_ltv || 0), a);
  const dscr = ads ? noi / ads : 0;
  const strip = el("div", "pp3-strip");
  [["TDC", tdc], ["NOI", noi], ["ARV", arv]].forEach(([label, v]) => {
    const c = el("div", "pp3-cell");
    c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val", fmtMoneyShort(v)));
    strip.append(c);
  });
  const dc = el("div", "pp3-cell");
  dc.append(el("div", "pp3-cell-label", "DSCR"),
    el("div", "pp3-cell-val" + (dscr && dscr < 1.25 ? " over" : ""), dscr ? dscr.toFixed(2) : "—"));
  strip.append(dc);
  host.append(strip);

  // members table + totals row
  const cols = el("div", "prop-cols re-deal-cols");
  ["PROPERTY", "UNITS", "TDC", "NOI", "DSCR"].forEach((h, i) => cols.append(el("span", i ? "prop-col-r" : "", h)));
  host.append(cols);
  members.forEach((p) => {
    const uw = reScreeningCalc(p);
    const row = el("div", "prop-row re-deal-row");
    row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    row.append(el("span", "prop-addr", p.short || p.slug));
    row.append(el("span", "prop-col-r", String(p.units || "—")));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.tdc || 0)));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.noi || 0)));
    row.append(el("span", "prop-col-r" + (uw.dscr && uw.dscr < 1.25 ? " over" : ""), uw.dscr ? uw.dscr.toFixed(2) : "—"));
    host.append(row);
  });
  const totals = el("div", "prop-row re-deal-totals");
  totals.append(el("span", "prop-addr", "deal total"));
  totals.append(el("span", "prop-col-r", String(members.reduce((n, p) => n + (p.units || 0), 0) || "—")));
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
function reScreeningCalc(p) {
  const src = p.__source || {};
  const a = reAssumptions();
  const units = p.units || src.total_units || 0;
  const rent = src.avg_rent_per_unit || 0;
  const purchase = src.purchase_price || 0;
  const hard = src.hard_costs || 0;
  if (!units || !rent) return { complete: false };
  const gross = units * rent * 12;
  const egi = gross * (1 - (a.vacancy_rate || 0));
  const noi = egi * (1 - (a.opex_rate || 0));
  const soft = hard * 0.15; // screening approximation; the engine itemizes
  const contingency = hard * (a.contingency_pct || 0);
  const tdc = purchase + hard + soft + contingency;
  const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
  const loan = arv * (a.perm_ltv || 0);
  const ads = reDebtService(loan, a);
  return { complete: true, gross, egi, noi, tdc, arv, loan, dscr: ads ? noi / ads : 0 };
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
