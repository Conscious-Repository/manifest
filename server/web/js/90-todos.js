// ================= TODOS — everything you've committed to =================
// Redesign §3 + Rev 2/3: ONE ranked list projected over personal + property +
// aion items assigned to me (the stage-4 substrate), a decisions lane on top,
// quiet rows for ideas and this week's done. Order IS the priority — drag a
// row and [rank:: n] lands in the owning file. No waiting surface, no
// parallel lists; every counter derives from the same rows.
let todosCache = null;
let todosTab = localStorage.getItem("todosTab") || "focus"; // focus | aion | realestate | manifest | personal
let todosMode = localStorage.getItem("todosMode") || "list"; // list (default) | board (Phase 8)
let todosLens = localStorage.getItem("todosLens") || "next"; // all | next | agents | attention
let todosQuery = "";
let todosLoadError = "";
let todosQuiet = {};    // ideas / done expanded
// the regret window: a row checked this session stays IN PLACE, struck and
// unmarkable, instead of vanishing into the quiet Done row. id → {row, idx}
let todosFreshDone = {};

async function loadTodos() {
  try {
    const res = await fetch("/api/tasks");
    if (!res.ok) throw new Error("Tasks could not be loaded");
    todosCache = await res.json();
    todosLoadError = "";
  } catch (e) {
    todosLoadError = "Couldn't refresh tasks. " + (todosCache ? "Showing the last loaded tasks." : "Try again to load your tasks.");
    if (!todosCache) todosCache = { rows: [], domains: [], areas: [], counts: {} };
  }
  renderTodos();
  ensureTodoPanelPoll();
  // deep link #/tasks/<id> → open the panel once the rows are here
  if (typeof todoDeepLink !== "undefined" && todoDeepLink) {
    const id = todoDeepLink;
    todoDeepLink = null;
    openTodoPanel(id);
  } else if (typeof todoSelId !== "undefined" && todoSelId) {
    renderTodoPanel(true); // keep the open panel in sync after mutations
  }
}

async function todosApi(path, body) {
  try {
    todosCache = { ...todosCache, ...(await postJSONOk(path, body)) };
    renderTodos();
  } catch (e) { showToast((e.message || "Task update failed").slice(0, 80)); }
}

// tabOf buckets a row into the FOCUS sub-tabs by its container.
function tabOf(r) {
  if (r.source === "aion" || r.container.name === "Aion") return "aion";
  if (r.source === "property" || r.source === "realestate" || /real estate/i.test(r.container.name || "")) return "realestate";
  if (/^manifest$/i.test(r.container.name || "")) return "manifest";
  return "personal";
}
function issueTabOf(domainName) {
  if (/^aion$/i.test(domainName)) return "aion";
  if (/real estate/i.test(domainName)) return "realestate";
  if (/^manifest$/i.test(domainName)) return "manifest";
  return "personal";
}

const TODOS_TABS = [["focus", "ALL DOMAINS"], ["aion", "AION"], ["realestate", "REAL ESTATE"], ["manifest", "MANIFEST"], ["personal", "PERSONAL"]];

// Derived attention and execution are views over the existing task record.
function todoWorkState(r) {
  const d = r.delegation || {};
  const agent = (r.owner || "").startsWith("agent:");
  const labels = { "plan-ready": "Plan ready", done: "Result ready", proposed: "Approval needed", failed: "Run failed", "plan-failed": "Plan failed", running: "Working", "plan-running": "Planning", queued: "Queued", "go-queued": "Queued", "plan-queued": "Plan queued" };
  const review = ["done", "proposed", "plan-ready"].includes(d.state);
  const blocked = r.state === "blocked" || (r.blockedBy || []).length > 0;
  const waiting = r.state === "waiting" || !!r.waiting;
  const attention = review || /failed|error|cancel/.test(d.state || "") || blocked || waiting;
  return { column: review ? "review" : r.delegation || agent ? "delegated" : "open",
    agents: !!r.delegation || agent, attention,
    next: !r.delegation && !agent && !blocked && !waiting,
    label: labels[d.state] || d.state || (blocked ? "Blocked" : waiting ? "Waiting" : agent ? "Assigned · no active run" : "Ready") };
}
function todoSearchMatches(r) {
  return !todosQuery.trim() || [r.text, r.container && r.container.name, r.owner, r.rock].filter(Boolean).join(" ").toLowerCase().includes(todosQuery.trim().toLowerCase());
}
function todoMatches(r, lens = todosLens) {
  if (todosTab !== "focus" && tabOf(r) !== todosTab) return false;
  if (!todoSearchMatches(r)) return false;
  const st = todoWorkState(r);
  return lens === "all" || !!st[lens];
}
function renderTodosToolbar() {
  const tabs = document.getElementById("todosTabs");
  if (tabs) {
    tabs.innerHTML = "";
    TODOS_TABS.forEach(([val, label]) => {
      const b = el("button", "filter-chip" + (todosTab === val ? " on" : ""), label);
      b.setAttribute("aria-pressed", String(todosTab === val));
      b.onclick = () => { todosTab = val; localStorage.setItem("todosTab", val); renderTodos(); };
      tabs.append(b);
    });
  }
  const bar = document.getElementById("todosToolbar");
  if (!bar) return;
  const searchFocused = document.activeElement && document.activeElement.id === "todosSearch";
  const caret = searchFocused ? document.activeElement.selectionStart : null;
  bar.innerHTML = "";
  const lenses = el("div", "tdo-lenses");
  [["all", "All active"], ["next", "Next actions"], ["agents", "With agents"], ["attention", "Needs attention"]].forEach(([value, label]) => {
    const n = (todosCache.rows || []).filter((r) => todoMatches(r, value)).length;
    const b = el("button", "filter-chip" + (todosLens === value ? " on" : ""), label + " · " + n);
    b.setAttribute("aria-pressed", String(todosLens === value));
    b.onclick = () => { todosLens = value; localStorage.setItem("todosLens", value); renderTodos(); };
    lenses.append(b);
  });
  const search = inputEl("Find a task…");
  search.id = "todosSearch"; search.type = "search"; search.value = todosQuery;
  search.setAttribute("aria-label", "Search tasks");
  search.oninput = () => { todosQuery = search.value; renderTodos(); };
  const modes = el("div", "tdo-layouts");
  ["list", "board"].forEach((value) => {
    const b = el("button", "filter-chip" + (todosMode === value ? " on" : ""), value === "list" ? "List" : "Board");
    b.setAttribute("aria-pressed", String(todosMode === value));
    b.onclick = () => { todosMode = value; localStorage.setItem("todosMode", value); renderTodos(); };
    modes.append(b);
  });
  bar.append(lenses, search, modes, pillLight("＋ Add task", () => openTodoQuickAdd()));
  if (searchFocused) { search.focus(); if (caret !== null) search.setSelectionRange(caret, caret); }
}

function renderTodos() {
  const host = els.todosRows; host.innerHTML = "";
  const counts = todosCache.counts || {};
  els.todosMeta.textContent = (counts.tasks || 0) + " open · t to capture";
  if (typeof setCrumbMeta === "function" && !els.todosView.hidden) {
    setCrumbMeta((counts.tasks || 0) + " open" + (counts.outstanding ? " · " + counts.outstanding + " outstanding" : ""));
  }
  if (typeof railSetCount === "function") railSetCount("tasks", counts.tasks || 0);

  renderTodosToolbar();
  if (todosLoadError) {
    const error = el("div", "tdo-load-error", todosLoadError);
    error.setAttribute("role", "status");
    error.append(pillLight("Retry", loadTodos));
    host.append(error);
  }
  // 1. decisions lane — always visible, never collapsed
  const issues = [];
  (todosCache.domains || []).forEach((dom) => (dom.issues || []).forEach((is) => {
    if (!is.checked && (todosTab === "focus" || issueTabOf(dom.name) === todosTab)) {
      issues.push({ is, domain: dom.name });
    }
  }));
  if (issues.length) {
    const lane = el("div", "tdo-decisions");
    const head = el("div", "tdo-sec-label");
    head.append(el("span", "tdo-sec-title", "⚑ Decisions waiting on you"),
      el("span", "tdo-sec-count", String(issues.length)));
    lane.append(head);
    issues.forEach(({ is, domain }) => lane.append(decisionRow(is, domain)));
    host.append(lane);
  }

  if (todosMode === "board") { renderTodosBoard(host); return; }

  // 2. the ranked list (+ this session's freshly-done rows held in place)
  const rows = todosCache.rows || [];
  const liveIds = new Set(rows.map((r) => r.id));
  Object.keys(todosFreshDone).forEach((id) => { if (liveIds.has(id)) delete todosFreshDone[id]; }); // unmarked → live again
  let visible = rows.filter((r) => todoMatches(r));
  Object.values(todosFreshDone).forEach(({ row, idx }) => {
    if (!todoMatches(row)) return;
    visible.splice(Math.min(idx, visible.length), 0, { ...row, _freshDone: true });
  });
  const sec = el("div", "tdo-main");
  const head = el("div", "tdo-sec-label");
  head.append(el("span", "tdo-sec-title", ({ all: "All active tasks", next: "Next actions · in your order", agents: "Work with agents", attention: "Needs your attention" })[todosLens]),
    el("span", "tdo-sec-count", String(visible.length)),
    el("span", "tdo-sec-hint", "⇅ drag to rank"));
  sec.append(head);
  visible.forEach((r, i) => sec.append(rankedRow(r, i)));
  if (!visible.length) sec.append(el("div", "pp-empty", todosQuery || todosLens !== "all" ? "No tasks match these filters. Change the view or clear your search." : "Nothing here — add a task to get started."));
  const capture = el("button", "tdo-capture", "＋ capture · t");
  capture.onclick = () => openTodoQuickAdd();
  sec.append(capture);
  host.append(sec);

  // 3. quiet rows — what used to be collapsed footers
  const ideas = [];
  (todosCache.domains || []).forEach((dom) => (dom.backlog || []).filter(() => todosTab === "focus" || issueTabOf(dom.name) === todosTab).filter((ln) => !todosQuery || ln.toLowerCase().includes(todosQuery.toLowerCase())).forEach((ln) =>
    ideas.push({ domain: dom.name, line: ln })));
  const dones = [];
  (todosCache.domains || []).forEach((dom) => {
    if (todosTab !== "focus" && issueTabOf(dom.name) !== todosTab) return;
    // freshly-done rows are still standing in the list above — no double entry
    (dom.tasks || []).forEach((t) => { if (t.state === "done" && !todosFreshDone[t.id] && todoSearchMatches(t)) dones.push({ t, domain: dom.name }); });
    (dom.buckets || []).forEach((bk) => (bk.tasks || []).forEach((t) => {
      if (t.state === "done" && !todosFreshDone[t.id] && todoSearchMatches(t)) dones.push({ t, domain: dom.name });
    }));
  });
  const quiet = el("div", "tdo-quiet-row");
  const quietBtnEl = (key, label, n) => {
    const b = el("button", "tdo-quiet" + (todosQuiet[key] ? " on" : ""), label + " · " + n);
    b.onclick = () => { todosQuiet[key] = !todosQuiet[key]; renderTodos(); };
    return b;
  };
  if (ideas.length) quiet.append(quietBtnEl("ideas", "◌ Ideas & backlog", ideas.length));
  if (dones.length) quiet.append(quietBtnEl("done", "✓ Recently completed", dones.length));
  if (quiet.children.length) host.append(quiet);
  if (todosQuiet.ideas && ideas.length) {
    const box = el("div", "tdo-quiet-box");
    ideas.forEach(({ domain, line }) => {
      const row = el("div", "tdo-backlog-line", "· " + line.replace(/^[ \t]*[-*]\s*(\[.\]\s*)?/, ""));
      row.title = domain;
      box.append(row);
    });
    host.append(box);
  }
  if (todosQuiet.done && dones.length) {
    const box = el("div", "tdo-quiet-box");
    dones.forEach(({ t, domain }) => {
      const row = el("div", "tdo-row done");
      const check = el("button", "tdo-check on", "✓");
      check.title = "reopen";
      check.onclick = () => todosApi("/api/tasks/check", { id: t.id, checked: false });
      row.append(check, el("span", "tdo-text", t.text), containerPill(domain));
      const dg = delegationFor(t.id); // a completed delegated todo keeps its result link
      if (dg) row.append(delegationChip(dg));
      box.append(row);
    });
    host.append(box);
  }
}

function containerPill(name) {
  return el("span", "tdo-pill", name);
}

// decisionRow — ⚑ text · domain pill · ● age · decide → (inline resolution)
function decisionRow(is, domain) {
  const row = el("div", "tdo-dec-row");
  row.append(el("span", "tdo-dec-flag", "⚑"));
  const text = el("span", "tdo-dec-text", is.text);
  row.append(text, containerPill(domain));
  if (is.openTasks > 0) row.append(el("span", "tdo-pill", is.openTasks + " task" + (is.openTasks === 1 ? "" : "s")));
  const right = el("span", "tdo-dec-right");
  if (is.ageDays > 0) right.append(el("span", "tdo-dec-age", "● " + is.ageDays + "d"));
  const decide = el("button", "tdo-decide", "decide →");
  decide.onclick = () => {
    const input = inputEl("one-line resolution — Enter commits…");
    input.classList.add("tdo-decide-in");
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) {
        todosApi("/api/tasks/issue/resolve", { id: is.id, resolution: input.value });
      } else if (ev.key === "Escape") input.replaceWith(decide);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(decide); });
    decide.replaceWith(input);
    input.focus();
  };
  right.append(decide);
  row.append(right);
  return row;
}

// rankedRow — handle · checkbox · text · container · age. Thin by decree
// (Rev 2): no rock tags, no waiting chips; stale = weight + ● only. A row
// checked this session renders in place, struck, with ✓ to unmark.
let _dragId = null;
function rankedRow(r, idx) {
  if (r._freshDone) {
    const row = el("div", "tdo-row done");
    const check = el("button", "tdo-check on", "✓");
    check.title = "unmark — back to open";
    check.onclick = () => { todosApi("/api/tasks/check", { id: r.id, checked: false }); };
    row.append(el("span", "tdo-handle", ""), check, el("span", "tdo-text", r.text), containerPill(r.container.name));
    return row;
  }
  const stale = r.ageDays >= 14;
  const row = el("div", "tdo-row" + (stale ? " stale" : "") + (r.state === "blocked" ? " blocked" : "") +
    (typeof todoSelId !== "undefined" && todoSelId === r.id ? " panel-sel" : ""));
  row.dataset.id = r.id;
  // background click opens the PANEL; every interactive child stops
  // propagation or is filtered here (text keeps click-to-edit)
  row.onclick = (e) => {
    if (e.target !== row && !e.target.classList.contains("tdo-right")) return;
    openTodoPanel(r);
  };

  const handle = el("span", "tdo-handle", "⠿");
  handle.title = "drag to rank";
  handle.onmousedown = () => { row.draggable = true; };
  row.addEventListener("dragstart", (e) => {
    _dragId = r.id;
    row.classList.add("dragging");
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", r.id);
  });
  row.addEventListener("dragend", () => { row.classList.remove("dragging"); row.draggable = false; _dragId = null; });
  row.addEventListener("dragover", (e) => {
    if (!_dragId || _dragId === r.id) return;
    e.preventDefault();
    row.classList.add("drag-over");
  });
  row.addEventListener("dragleave", () => row.classList.remove("drag-over"));
  row.addEventListener("drop", (e) => {
    e.preventDefault();
    row.classList.remove("drag-over");
    if (!_dragId || _dragId === r.id) return;
    commitRank(_dragId, r.id);
    _dragId = null;
  });
  row.append(handle);

  const check = el("button", "tdo-check", "○");
  check.title = "done (stays here to unmark)";
  check.onclick = () => {
    todosFreshDone[r.id] = { row: r, idx }; // hold it in place for the regret window
    todosApi("/api/tasks/check", { id: r.id, checked: true });
  };
  row.append(check);

  const label = el("button", "tdo-text tdo-task-title", r.text);
  label.title = "Open task and conversation";
  label.onclick = () => openTodoPanel(r);
  row.append(label);

  const pill = containerPill(r.container.name);
  if (r.container.kind === "property") {
    pill.classList.add("linky");
    pill.title = "open the property";
    pill.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(r.container.slug); };
  }
  row.append(pill);
  // agent owner chip (plan D5): raw markdown keeps [owner:: agent:x],
  // display strips the prefix behind the ✦ glyph
  if (r.owner && r.owner.startsWith("agent:")) {
    row.append(el("span", "tdo-agent-chip", "✦ " + r.owner.slice(6)));
  }
  if (!r.delegation && todoWorkState(r).label !== "Ready") row.append(el("span", "tdo-status micro-label", todoWorkState(r).label));
  // coordination (P1 Phase 1): the priority word + the derived blocked chip
  if (r.priority) row.append(prioMark(r.priority));
  const bc = blockedChip(r);
  if (bc) row.append(bc);

  const right = el("span", "tdo-right");
  const age = el("span", "tdo-age" + (stale ? " stale" : ""),
    r.ageDays > 0 ? (stale ? "● " : "") + r.ageDays + "d" : "");
  if (stale) age.title = "aging — do it or drop it";
  right.append(age);
  // ⧗ tether — quiet: hover-revealed when unanchored, tiny accent when set.
  // Property todos aren't rock work; personal + aion rows tether in place.
  if (r.source !== "property") {
    const teth = el("button", "tdo-tether" + (r.rock ? " on" : ""), "⧗");
    teth.title = r.rock ? "advances " + r.rock + " — click to change" : "tether to a rock or stage…";
    teth.onclick = () => openTetherPicker(teth, { rock: r.rock }, r.container.name,
      (p) => todosApi("/api/tasks/update", { id: r.id, rock: p.rock, stage: p.stage }));
    right.append(teth);
  }
  // ⇢ delegate (Phase 6) — dispatch this todo to a harness; the chip tracks
  // the work order through the trace files (and stays clickable when done).
  if (r.delegation) {
    right.append(delegationChip(r.delegation, false, r.id));
  } else {
    const dg = el("button", "tdo-work-action", "Work →");
    dg.title = "Open task to work with an agent";
    dg.onclick = () => openDelegatePicker(r);
    right.append(dg);
  }
  {
    // › opens the panel (plan · thread · assignee)
    const open = el("button", "tdo-open-chevron", "›");
    open.title = "open — plan, thread";
    open.onclick = (e) => { e.stopPropagation(); openTodoPanel(r); };
    right.append(open);
  }
  {
    // ✕ removes the row: personal → archived; property/aion → deleted from
    // its source file (there is no archive for those)
    const isDelete = r.source === "property" || r.source === "aion" || r.source === "realestate";
    const x = el("button", "uw-x", "✕");
    x.title = r.source === "property" ? "delete from the property's tasks"
      : r.source === "aion" ? "delete from the aion backlog"
      : r.source === "realestate" ? "delete from the real-estate backlog"
      : "drop (archived, never deleted)";
    x.onclick = () => {
      const yes = el("button", "tdo-decide", isDelete ? "delete?" : "drop?");
      yes.onclick = () => todosApi("/api/tasks/drop", { id: r.id });
      x.replaceWith(yes);
      setTimeout(() => { if (yes.parentNode) yes.replaceWith(x); }, 2500);
    };
    right.append(x);
  }
  row.append(right);
  return row;
}

// ---- coordination markers (P1 Phase 1) ----
// prioMark: the priority word as a mono chip — importance, not position (rank
// is the drag order). Weight carries "high"; nothing is colored.
function prioMark(p) {
  const m = el("span", "tdo-prio " + p, p);
  m.title = "priority " + p + " — importance, not list position";
  return m;
}
// coordName: a dependency id rendered as its row text when the row is live
// (open, in the projection), else the raw id.
function coordName(id) {
  const r = ((todosCache && todosCache.rows) || []).find((x) => x.id === id);
  return r ? r.text : id;
}
// blockedChip: derived state only — `blocked` while a dependency is still
// open (the tooltip names the blockers); a quiet `depends?` when the only
// dependencies are ids no source knows. Null when neither applies.
function blockedChip(r) {
  const blockedBy = r.blockedBy || [];
  const unresolved = r.unresolved || [];
  if (blockedBy.length) {
    const c = el("span", "tdo-blocked", "blocked");
    c.title = "blocked by " + blockedBy.map(coordName).join(" · ") +
      (unresolved.length ? " · unresolved: " + unresolved.join(", ") : "");
    return c;
  }
  if (unresolved.length) {
    const c = el("span", "tdo-blocked soft", "depends?");
    c.title = "depends on ids no source knows: " + unresolved.join(", ");
    return c;
  }
  return null;
}

// commitRank — reposition draggedId before targetId in the GLOBAL row order
// (the visible list may be a tab subset), then one POST with the full order:
// the server writes [rank:: n] with one write per touched file.
function commitRank(draggedId, targetId) {
  const order = (todosCache.rows || []).map((r) => r.id).filter((id) => id !== draggedId);
  const at = order.indexOf(targetId);
  if (at < 0) return;
  order.splice(at, 0, draggedId);
  // optimistic: re-order the cache rows to match, then persist
  const byId = {};
  (todosCache.rows || []).forEach((r) => { byId[r.id] = r; });
  todosCache.rows = order.map((id) => byId[id]).filter(Boolean);
  renderTodos();
  todosApi("/api/tasks/rank", { order });
}

// ---- the tether picker: one typeahead over every open rock and stage ----
// delegationChip: one shared chip for a delegation state, clickable everywhere
// (open rows, done rows, board cards). A finished delegation opens its RESULT
// through the shared legible viewer — the artifact brief when the run wrote
// one, else the run report; proposed routes to the FEED inbox; queued/running
// → Spirits.
function delegationChip(d, asSpan, taskId) {
  const chip = el(asSpan ? "span" : "button", "delegation-chip dstate-" + d.state, "⇢ " + d.harness + " · " + d.state);
  chip.style.cursor = "pointer";
  const runtimeBadge = typeof terminalRunBadge === "function" ? terminalRunBadge(d) : null;
  const hasResult = !!(d.artifactRef || d.artifactPath || d.runId);
  const planState = (d.state || "").startsWith("plan");
  chip.title = d.state === "proposed" ? "a proposal is waiting in the FEED inbox"
    : d.state === "plan-ready" ? "the plan is in — review it in the panel, then fire"
    : planState ? "the agent is drafting a plan"
    : d.artifactRef || d.artifactPath ? "read the result"
    : d.runId ? "view the result (run report)" : "delegated work — click for the runs board";
  chip.onclick = (e) => {
    e.stopPropagation();
    if (taskId && typeof openTodoPanel === "function") { openTodoPanel(taskId); return; }
    if (d.state === "proposed") { location.hash = "#/feed"; return; }
    if (hasResult) { openResult(d); return; }
    location.hash = "#/agents";
  };
  if (runtimeBadge) { const wrap = el("span", "delegation-runtime"); wrap.append(chip, runtimeBadge); return wrap; }
  return chip;
}

// delegationFor: look up a todo id's delegation state (for DONE rows/cards that
// aren't in the open-rows projection).
function delegationFor(id) {
  return (todosCache && todosCache.delegations && todosCache.delegations[id]) || null;
}

// openDelegatePicker (Phase 6 → agent-chat plan §3.4f): the ⇢ row button and
// the board's → DELEGATED drags are SHORTCUTS into the ONE lifecycle — the
// panel's composer in Do mode with the agent picker focused (assign → plan →
// fire). The old target picker + /api/tasks/delegate spool path stays on the
// API for compatibility; the UI no longer has a second state machine.
//
// redelegate=true is the REVIEW → DELEGATED path (owner ask 2026-08-12): the
// owner read the result and is sending it back out — the Do text is the
// steer, and it lands in the thread as the record before the re-plan.
function openDelegatePicker(r, redelegate) {
  openTodoPanel(r, { mode: "do", focusAgent: true });
  if (redelegate) showToast("Tell the agent what to change — Do sends your instructions to the agent", null, "info");
}

// Shared by the TODOS rows (⧗) and the GOALS unanchored foot. Picking writes
// [rock::] (+ optional [stage::]) through /api/tasks/update — one line, one
// file, both surfaces re-project it.
async function tetherAreas() {
  try { return ((await (await fetch("/api/goals")).json()).areas) || []; }
  catch (e) { return []; }
}

// openTetherPicker swaps `anchor` for the typeahead; restores it on escape /
// blur. preferArea floats that area's rocks to the top. onPick({rock, stage}).
function openTetherPicker(anchor, current, preferArea, onPick) {
  let done = false;
  const restore = () => { if (!done && ta.el.parentNode) ta.el.replaceWith(anchor); };
  const pick = (rock, stage) => { done = true; ta.el.replaceWith(anchor); onPick({ rock, stage }); };
  const ta = typeahead({
    placeholder: "rock, or rock → stage…",
    minChars: 0,
    onEscape: restore,
    onBlurGone: restore,
    suggest: async (q, add) => {
      const areas = await tetherAreas();
      const items = [];
      areas.forEach((a) => (a.rocks || []).filter((r) => !r.checked).forEach((r) => {
        items.push({ label: a.name + " · " + r.text, rock: r.id, stage: "", area: a.name });
        (r.children || []).filter((c) => !c.checked).forEach((c) =>
          items.push({ label: a.name + " · " + r.text + " → " + c.text, rock: r.id, stage: c.text, area: a.name }));
      }));
      items.sort((x, y) => (x.area === preferArea ? 0 : 1) - (y.area === preferArea ? 0 : 1));
      items.filter((it) => !q || it.label.toLowerCase().includes(q)).slice(0, 10)
        .forEach((it) => add(it.label, it.stage ? "stage" : "rock", () => pick(it.rock, it.stage)));
      if (current && current.rock) add("✕ untether", "", () => pick("", ""));
    },
  });
  anchor.replaceWith(ta.el);
  ta.focus();
}

// personInput: free text or a contact — picking a suggestion wraps [[display]]
// so the waiting-on surfaces on that contact's page. (Used by the FEED
// signals lane's waiting quick-action; the todos board itself no longer
// renders waiting — strict Rev 3.)
function personInput(onSet, onCancel) {
  const ta = typeahead({
    placeholder: "who? (name or free text)",
    minChars: 2,
    onEnter: onSet,
    onEscape: onCancel,
    onBlurGone: onCancel,
    suggest: async (q, add) => {
      let people = [];
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).slice(0, 5);
      } catch (e) {}
      people.forEach((p) => add(p.display, "person", () => onSet("[[" + p.display + "]]")));
    },
  });
  return ta;
}

// ================= THE BOARD (big-change Phase 8) =================
// Pure projection over the SAME /api/tasks payload the list renders — four
// active columns: Open · Delegated · Review, with Done folded below. Waiting is
// gone — it was never used, and delegated work already IS waiting). Every drag
// maps to an EXISTING endpoint (check / delegate); no new state anywhere.
//
// REVIEW is the auto-move: a delegation whose result is ready (state done or
// proposed) while the todo is still open lands here by projection, the instant
// the agent finishes — the same condition the FEED's delegation-done card
// keys on. From Review: drag → Done checks it; drag → Delegated sends it back
// out with an owner comment.
function renderTodosBoard(host) {
  const rows = (todosCache.rows || []).filter((r) => todoMatches(r));
  const cols = { open: [], delegated: [], review: [], done: [] };
  rows.forEach((r) => cols[todoWorkState(r).column].push(r));
  // Done: personal todos completed but not yet swept (the domains view keeps
  // them until the weekly sweep) — read-only history plus a drag target.
  (todosCache.domains || []).forEach((dom) => {
    const scan = (list) => (list || []).forEach((t) => {
      if (t.state === "done" && todoSearchMatches(t) && (todosTab === "focus" || issueTabOf(dom.name) === todosTab)) {
        cols.done.push({ id: t.id, text: t.text, container: { name: dom.name }, source: "personal", done: true });
      }
    });
    scan(dom.tasks);
    (dom.buckets || []).forEach((bk) => scan(bk.tasks));
  });

  const board = el("div", "tdo-board");
  const defs = [
    ["open", "OPEN", "drop here to reopen"],
    ["delegated", "DELEGATED", "Assigned work · open a task to steer the agent"],
    ["review", "REVIEW", "delegated work that came back — read it, then Done or send it back out"],
  ];
  const shown = defs.filter(([key]) => (todosLens === "all" && !todosQuery.trim()) || cols[key].length);
  board.style.setProperty("--task-columns", String(Math.max(1, shown.length)));
  if (!shown.length) board.append(emptyRow("No tasks match this view. Change your filters or search."));
  shown.forEach(([key, label, hint]) => {
    const col = el("div", "tdo-col");
    col.dataset.col = key;
    const head = el("div", "tdo-col-head");
    head.append(el("span", "tdo-sec-title", label), el("span", "tdo-sec-count", String(cols[key].length)));
    col.append(head);
    if (!cols[key].length) col.append(el("div", "tdo-col-empty", { open: "No open tasks in this view", delegated: "Agent assignments appear here", review: "Results appear here when ready" }[key]));
    cols[key].forEach((r) => col.append(boardCard(r, key)));
    if (key === "open") col.append(pillLight("＋ Add task", () => openTodoQuickAdd()));
    col.title = hint;
    col.addEventListener("dragover", (e) => { e.preventDefault(); col.classList.add("dragging-over"); });
    col.addEventListener("dragleave", () => col.classList.remove("dragging-over"));
    col.addEventListener("drop", (e) => {
      e.preventDefault();
      col.classList.remove("dragging-over");
      const id = e.dataTransfer.getData("text/todo-id");
      const from = e.dataTransfer.getData("text/todo-col");
      if (id) boardMove(id, from, key);
    });
    board.append(col);
  });
  host.append(board);
  if (cols.done.length) {
    const done = collapsibleSection(host, "Recently completed", String(cols.done.length), !!todosQuiet.done);
    done.previousElementSibling.addEventListener("click", () => { todosQuiet.done = !done.hidden; });
    cols.done.forEach((r) => done.append(boardCard(r, "done")));
  }
}

function boardCard(r, colKey) {
  const card = el("div", "tdo-card" + (r.done ? " done" : "") +
    (typeof todoSelId !== "undefined" && todoSelId === r.id ? " panel-sel" : ""));
  card.dataset.id = r.id;
  card.onclick = (e) => {
    // card background opens the panel; text/buttons keep their own handlers
    if (e.target !== card && !e.target.classList.contains("tdo-card-meta")) return;
    openTodoPanel(r);
  };
  card.draggable = true;
  card.addEventListener("dragend", () => { _dragId = null; });
  card.addEventListener("dragstart", (e) => {
    _dragId = r.id;
    e.dataTransfer.setData("text/todo-id", r.id);
    e.dataTransfer.setData("text/todo-col", colKey);
    e.dataTransfer.effectAllowed = "move";
  });
  const textEl = el("button", "tdo-card-text tdo-task-title", r.text);
  textEl.title = "Open task and conversation";
  textEl.onclick = (e) => { e.stopPropagation(); openTodoPanel(r); };
  card.append(textEl);
  const meta = el("div", "tdo-card-meta");
  meta.append(el("span", "", r.container && r.container.name || ""));
  if (r.owner && r.owner.startsWith("agent:")) meta.append(el("span", "tdo-agent-chip", "✦ " + r.owner.slice(6)));
  if (r.priority) meta.append(prioMark(r.priority));
  { const bc = blockedChip(r); if (bc) meta.append(bc); }
  if (r.waiting) meta.append(el("span", "tdo-card-wait", "⧗ " + r.waiting));
  // delegation: inline for open cards, looked up by id for the Done column
  const dg = r.delegation || delegationFor(r.id);
  if (dg) meta.append(delegationChip(dg, false, r.id));
  if (!dg && !r.done && todoWorkState(r).label !== "Ready") meta.append(el("span", "tdo-status", todoWorkState(r).label));
  if (r.rock) meta.append(el("span", "tdo-card-rock", "⧗ " + r.rock.split("/").pop()));
  const open = el("button", "tdo-open-chevron", "›");
  open.title = "open — plan, thread";
  open.onclick = (e) => { e.stopPropagation(); openTodoPanel(r); };
  open.onmousedown = (e) => e.stopPropagation();
  meta.append(open);
  card.append(meta);
  const acts = el("div", "tdo-card-acts");
  const action = (label, fn) => {
    const b = pillLight(label, (e) => { fn(); });
    b.addEventListener("click", (e) => e.stopPropagation());
    acts.append(b);
  };
  if (r.done) action("Reopen", () => todosApi("/api/tasks/check", { id: r.id, checked: false }));
  else {
    if (colKey === "review" && dg) {
      action(dg.state === "plan-ready" ? "Review plan" : dg.state === "proposed" ? "Review approval" : "Read result", () => dg.state !== "done" || !(dg.artifactRef || dg.artifactPath || dg.runId) ? openTodoPanel(r) : openResult(dg, r.text));
      action("Continue with agent", () => openDelegatePicker(r, true));
    } else action(colKey === "delegated" ? "Open conversation →" : "Work with agent →", () => colKey === "delegated" ? openTodoPanel(r) : openDelegatePicker(r));
    action("Done", () => todosApi("/api/tasks/check", { id: r.id, checked: true }));
  }
  card.append(acts);
  return card;
}

// boardMove maps a drag to the existing endpoints — nothing new writes.
function boardMove(id, from, to) {
  if (from === to) return;
  const row = (todosCache.rows || []).find((r) => r.id === id);
  switch (to) {
    case "done":
      todosApi("/api/tasks/check", { id, checked: true });
      return;
    case "open":
      if (from === "done") { todosApi("/api/tasks/check", { id, checked: false }); return; }
      if (from === "delegated") { showToast(row && !row.delegation ? "Change the assignee in Task details to bring this work back to you." : "Still out with the agent — it lands in REVIEW when it comes back"); return; }
      if (from === "review") { showToast("Read the result first — then Done, or drag back to DELEGATED to send it out again"); return; }
      return;
    case "review":
      showToast("REVIEW fills itself the moment delegated work comes back");
      return;
    case "delegated":
      // from REVIEW this is a RE-delegation: the owner read the result and is
      // sending it back out, so the comment is the point — it rides the work
      // order to the agent (POST /api/tasks/delegate {comment}).
      if (row) openDelegatePicker(row, from === "review");
      return;
  }
}

// Completed tasks still open with a human title, including Manifest history.
function todosCompletedRow(id) {
  for (const dom of (todosCache && todosCache.domains) || []) {
    const tasks = [...(dom.tasks || []), ...(dom.buckets || []).flatMap((b) => b.tasks || [])];
    const t = tasks.find((r) => r.id === id);
    if (t) return { ...t, done: t.state === "done", container: { name: dom.name }, source: "personal" };
  }
  return null;
}
