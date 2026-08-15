// ================= TODOS — everything you've committed to =================
// Redesign §3 + Rev 2/3: ONE ranked list projected over personal + property +
// aion items assigned to me (the stage-4 substrate), a decisions lane on top,
// quiet rows for ideas and this week's done. Order IS the priority — drag a
// row and [rank:: n] lands in the owning file. No waiting surface, no
// parallel lists; every counter derives from the same rows.
let todosCache = null;
let todosTab = "focus"; // focus | aion | realestate | personal
let todosMode = localStorage.getItem("todosMode") || "list"; // list (default) | board (Phase 8)
let todosQuiet = {};    // ideas / done expanded
// the regret window: a row checked this session stays IN PLACE, struck and
// unmarkable, instead of vanishing into the quiet Done row. id → {row, idx}
let todosFreshDone = {};

async function loadTodos() {
  try { todosCache = await (await fetch("/api/todos")).json(); }
  catch (e) { todosCache = { rows: [], domains: [], areas: [], counts: {} }; }
  renderTodos();
  // deep link #/todos/<id> → open the panel once the rows are here
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
  } catch (e) { showToast((e.message || "Todo update failed").slice(0, 80)); }
}

// tabOf buckets a row into the FOCUS sub-tabs by its container.
function tabOf(r) {
  if (r.source === "aion" || r.container.name === "Aion") return "aion";
  if (r.source === "property" || /real estate/i.test(r.container.name || "")) return "realestate";
  return "personal";
}
function issueTabOf(domainName) {
  if (/^aion$/i.test(domainName)) return "aion";
  if (/real estate/i.test(domainName)) return "realestate";
  return "personal";
}

const TODOS_TABS = [["focus", "FOCUS"], ["aion", "AION"], ["realestate", "REAL ESTATE"], ["personal", "PERSONAL"]];

function renderTodos() {
  const host = els.todosRows; host.innerHTML = "";
  const counts = todosCache.counts || {};
  els.todosMeta.textContent = (counts.todos || 0) + " open · t to capture";
  if (typeof setCrumbMeta === "function" && !els.todosView.hidden) {
    setCrumbMeta((counts.todos || 0) + " open" + (counts.outstanding ? " · " + counts.outstanding + " outstanding" : ""));
  }
  if (typeof railSetCount === "function") railSetCount("todos", counts.todos || 0);

  // tab chips + the list/board mode toggle (Phase 8 — List stays the default)
  const tabsHost = document.getElementById("todosTabs");
  if (tabsHost) {
    tabsHost.innerHTML = "";
    TODOS_TABS.forEach(([val, label]) => {
      const b = el("button", "filter-chip" + (todosTab === val ? " on" : ""), label);
      b.onclick = () => { todosTab = val; renderTodos(); };
      tabsHost.append(b);
    });
    const mode = el("button", "tdo-mode-toggle", todosMode === "board" ? "☰ list" : "▦ board");
    mode.title = todosMode === "board" ? "back to the ranked list" : "the board: Open · Delegated · Review · Done";
    mode.onclick = () => {
      todosMode = todosMode === "board" ? "list" : "board";
      localStorage.setItem("todosMode", todosMode);
      renderTodos();
    };
    tabsHost.append(mode);
  }
  if (todosMode === "board") { renderTodosBoard(host); return; }

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

  // 2. the ranked list (+ this session's freshly-done rows held in place)
  const rows = todosCache.rows || [];
  const liveIds = new Set(rows.map((r) => r.id));
  Object.keys(todosFreshDone).forEach((id) => { if (liveIds.has(id)) delete todosFreshDone[id]; }); // unmarked → live again
  let visible = todosTab === "focus" ? rows.slice() : rows.filter((r) => tabOf(r) === todosTab);
  Object.values(todosFreshDone).forEach(({ row, idx }) => {
    if (todosTab !== "focus" && tabOf(row) !== todosTab) return;
    visible.splice(Math.min(idx, visible.length), 0, { ...row, _freshDone: true });
  });
  const sec = el("div", "tdo-main");
  const head = el("div", "tdo-sec-label");
  head.append(el("span", "tdo-sec-title", "Everything you've committed to"),
    el("span", "tdo-sec-count", String(visible.length)),
    el("span", "tdo-sec-hint", "⇅ drag to rank"));
  sec.append(head);
  visible.forEach((r, i) => sec.append(rankedRow(r, i)));
  if (!visible.length) sec.append(el("div", "pp-empty", "Nothing here — press t anywhere to capture."));
  const capture = el("button", "tdo-capture", "＋ capture · t");
  capture.onclick = () => openTodoQuickAdd();
  sec.append(capture);
  host.append(sec);

  // 3. quiet rows — what used to be collapsed footers
  const ideas = [];
  (todosCache.domains || []).forEach((dom) => (dom.backlog || []).forEach((ln) =>
    ideas.push({ domain: dom.name, line: ln })));
  const dones = [];
  (todosCache.domains || []).forEach((dom) => {
    // freshly-done rows are still standing in the list above — no double entry
    (dom.todos || []).forEach((t) => { if (t.state === "done" && !todosFreshDone[t.id]) dones.push({ t, domain: dom.name }); });
    (dom.buckets || []).forEach((bk) => (bk.todos || []).forEach((t) => {
      if (t.state === "done" && !todosFreshDone[t.id]) dones.push({ t, domain: dom.name });
    }));
  });
  const quiet = el("div", "tdo-quiet-row");
  const quietBtnEl = (key, label, n) => {
    const b = el("button", "tdo-quiet" + (todosQuiet[key] ? " on" : ""), label + " · " + n);
    b.onclick = () => { todosQuiet[key] = !todosQuiet[key]; renderTodos(); };
    return b;
  };
  if (ideas.length) quiet.append(quietBtnEl("ideas", "◌ Ideas & backlog", ideas.length));
  if (dones.length) quiet.append(quietBtnEl("done", "✓ Done this week", dones.length));
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
      check.onclick = () => todosApi("/api/todos/check", { id: t.id, checked: false });
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
        todosApi("/api/todos/issue/resolve", { id: is.id, resolution: input.value });
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
    check.onclick = () => { todosApi("/api/todos/check", { id: r.id, checked: false }); };
    row.append(el("span", "tdo-handle", ""), check, el("span", "tdo-text", r.text), containerPill(r.container.name));
    return row;
  }
  const stale = r.ageDays >= 14;
  const row = el("div", "tdo-row" + (stale ? " stale" : "") +
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
  row.addEventListener("dragend", () => { row.classList.remove("dragging"); row.draggable = false; });
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
    todosApi("/api/todos/check", { id: r.id, checked: true });
  };
  row.append(check);

  const label = el("span", "tdo-text", r.text);
  label.title = "click to edit";
  label.onclick = () => {
    const input = inputEl(""); input.value = r.text; input.classList.add("work-edit");
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) todosApi("/api/todos/update", { id: r.id, text: input.value });
      else if (ev.key === "Escape") input.replaceWith(label);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(label); });
    label.replaceWith(input); input.focus();
  };
  row.append(label);

  const pill = containerPill(r.container.name);
  if (r.container.kind === "property") {
    pill.classList.add("linky");
    pill.title = "open the property";
    pill.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(r.container.slug); };
  }
  row.append(pill);

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
      (p) => todosApi("/api/todos/update", { id: r.id, rock: p.rock, stage: p.stage }));
    right.append(teth);
  }
  // ⇢ delegate (Phase 6) — dispatch this todo to a harness; the chip tracks
  // the work order through the trace files (and stays clickable when done).
  if (r.delegation) {
    right.append(delegationChip(r.delegation));
  } else {
    const dg = el("button", "uw-x tdo-delegate", "⇢");
    dg.title = "delegate to a harness…";
    dg.onclick = () => openDelegatePicker(r);
    right.append(dg);
  }
  {
    // › opens the panel (description · plan · thread · assignee)
    const open = el("button", "tdo-open-chevron", "›");
    open.title = "open — description, plan, thread";
    open.onclick = (e) => { e.stopPropagation(); openTodoPanel(r); };
    right.append(open);
  }
  {
    // ✕ removes the row: personal → archived; property/aion → deleted from
    // its source file (there is no archive for those)
    const isDelete = r.source === "property" || r.source === "aion";
    const x = el("button", "uw-x", "✕");
    x.title = r.source === "property" ? "delete from the property's todos"
      : r.source === "aion" ? "delete from the aion backlog"
      : "drop (archived, never deleted)";
    x.onclick = () => {
      const yes = el("button", "tdo-decide", isDelete ? "delete?" : "drop?");
      yes.onclick = () => todosApi("/api/todos/drop", { id: r.id });
      x.replaceWith(yes);
      setTimeout(() => { if (yes.parentNode) yes.replaceWith(x); }, 2500);
    };
    right.append(x);
  }
  row.append(right);
  return row;
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
  todosApi("/api/todos/rank", { order });
}

// ---- the tether picker: one typeahead over every open rock and stage ----
// delegationChip: one shared chip for a delegation state, clickable everywhere
// (open rows, done rows, board cards). A finished delegation opens its RESULT
// through the shared legible viewer — the artifact brief when the run wrote
// one, else the run report; proposed routes to the FEED inbox; queued/running
// → Spirits.
function delegationChip(d, asSpan) {
  const chip = el(asSpan ? "span" : "button", "delegation-chip dstate-" + d.state, "⇢ " + d.harness + " · " + d.state);
  chip.style.cursor = "pointer";
  const hasResult = !!(d.artifactRef || d.artifactPath || d.runId);
  chip.title = d.state === "proposed" ? "a proposal is waiting in the FEED inbox"
    : d.artifactRef || d.artifactPath ? "read the result"
    : d.runId ? "view the result (run report)" : "delegated work — click for the runs board";
  chip.onclick = (e) => {
    e.stopPropagation();
    if (d.state === "proposed") { location.hash = "#/feed"; return; }
    if (hasResult) { openResult(d); return; }
    location.hash = "#/spirits";
  };
  return chip;
}

// delegationFor: look up a todo id's delegation state (for DONE rows/cards that
// aren't in the open-rows projection).
function delegationFor(id) {
  return (todosCache && todosCache.delegations && todosCache.delegations[id]) || null;
}

// openDelegatePicker (Phase 6): pick a dispatch target (harness · on-demand
// ritual), then an optional brief; POST /api/todos/delegate spools the work
// order and stamps [waiting::]. The chip appears on the next render from the
// trace projection.
//
// redelegate=true is the REVIEW → DELEGATED path (owner ask 2026-08-12): the
// owner read the result and is sending it back out, so the box asks for the
// comment and the comment travels as `comment` — labelled for the agent as a
// steer on the previous result, never confused with the original brief.
async function openDelegatePicker(r, redelegate) {
  let targets = [];
  try { targets = (await (await fetch("/api/todos/delegate/targets")).json()).targets || []; }
  catch (e) { showToast("Couldn't load delegate targets"); return; }
  if (!targets.length) { showToast("No dispatch targets — no harness has an on-demand ritual"); return; }
  const byId = new Map(targets.map((t, i) => [String(i), t]));
  const cur = r.delegation || delegationFor(r.id);
  const title = (redelegate ? "Send back out: " : "Delegate: ") + r.text.slice(0, 60);
  openPicker(title, [{ area: redelegate ? "send it back to…" : "dispatch to…", items:
    targets.map((t, i) => ({ id: String(i), text: t.label + (cur && cur.harness === t.harness ? "  ·  last run here" : "") })) }],
    (id) => {
      const t = byId.get(id);
      if (!t) return;
      const send = async (note) => {
        const text = (note || "").trim();
        if (redelegate && !text) { showToast("A comment is what makes this a re-delegation — nothing sent", null, "error"); return; }
        const body = { id: r.id, harness: t.harness, spirit: t.spirit, ritual: t.ritual, brief: "", comment: "" };
        if (redelegate) body.comment = text; else body.brief = text;
        try {
          await postJSONOk("/api/todos/delegate", body);
          showToast((redelegate ? "Sent back with your comment → " : "Delegated → ") + t.label);
          loadTodos();
        } catch (e) { showToast("Delegate failed: " + (e.message || e), null, "error"); }
      };
      if (redelegate) askText("Your comment for " + t.label, "what to fix, change or go deeper on — this goes to the agent with the todo", send);
      else askText("Brief for " + t.label, "optional — defaults to the todo text", send);
    });
}

// Shared by the TODOS rows (⧗) and the GOALS unanchored foot. Picking writes
// [rock::] (+ optional [stage::]) through /api/todos/update — one line, one
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
// Pure projection over the SAME /api/todos payload the list renders — four
// columns: Open · Delegated · Review · Done (owner call 2026-08-12: Waiting is
// gone — it was never used, and delegated work already IS waiting). Every drag
// maps to an EXISTING endpoint (check / delegate); no new state anywhere.
//
// REVIEW is the auto-move: a delegation whose result is ready (state done or
// proposed) while the todo is still open lands here by projection, the instant
// the agent finishes — the same condition the FEED's delegation-done card
// keys on. From Review: drag → Done checks it; drag → Delegated sends it back
// out with an owner comment.
const DELEG_REVIEW = { done: true, proposed: true }; // result ready → Review
function renderTodosBoard(host) {
  const rows = (todosCache.rows || []).filter((r) => todosTab === "focus" || tabOf(r) === todosTab);
  const cols = { open: [], delegated: [], review: [], done: [] };
  rows.forEach((r) => {
    if (r.delegation) (DELEG_REVIEW[r.delegation.state] ? cols.review : cols.delegated).push(r);
    else cols.open.push(r);
  });
  // Done: personal todos completed but not yet swept (the domains view keeps
  // them until the weekly sweep) — read-only history plus a drag target.
  (todosCache.domains || []).forEach((dom) => {
    const scan = (list) => (list || []).forEach((t) => {
      if (t.state === "done" && (todosTab === "focus" || issueTabOf(dom.name) === todosTab)) {
        cols.done.push({ id: t.id, text: t.text, container: { name: dom.name }, source: "personal", done: true });
      }
    });
    scan(dom.todos);
    (dom.buckets || []).forEach((bk) => scan(bk.todos));
  });

  const board = el("div", "tdo-board");
  const defs = [
    ["open", "OPEN", "drop here to reopen"],
    ["delegated", "DELEGATED", "drop here to dispatch to a harness"],
    ["review", "REVIEW", "delegated work that came back — read it, then Done or send it back out"],
    ["done", "DONE", "drop here to complete"],
  ];
  defs.forEach(([key, label, hint]) => {
    const col = el("div", "tdo-col");
    col.dataset.col = key;
    const head = el("div", "tdo-col-head");
    head.append(el("span", "tdo-sec-title", label), el("span", "tdo-sec-count", String(cols[key].length)));
    col.append(head);
    cols[key].forEach((r) => col.append(boardCard(r, key)));
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
}

function boardCard(r, colKey) {
  const card = el("div", "tdo-card" + (r.done ? " done" : "") +
    (typeof todoSelId !== "undefined" && todoSelId === r.id ? " panel-sel" : ""));
  card.onclick = (e) => {
    // card background opens the panel; text/buttons keep their own handlers
    if (e.target !== card && !e.target.classList.contains("tdo-card-meta")) return;
    openTodoPanel(r);
  };
  card.draggable = true;
  card.addEventListener("dragstart", (e) => {
    e.dataTransfer.setData("text/todo-id", r.id);
    e.dataTransfer.setData("text/todo-col", colKey);
    e.dataTransfer.effectAllowed = "move";
  });
  // text — click to edit in place (mirrors the list's rankedRow); dragging is
  // suspended while the input is open so text selection doesn't start a drag.
  const textEl = el("div", "tdo-card-text", r.text);
  textEl.title = "click to edit";
  textEl.onclick = (e) => {
    e.stopPropagation();
    const input = inputEl(""); input.value = r.text; input.className = "work-edit tdo-card-edit";
    const restore = () => { if (input.parentNode) input.replaceWith(textEl); card.draggable = true; };
    card.draggable = false;
    input.addEventListener("mousedown", (ev) => ev.stopPropagation());
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) todosApi("/api/todos/update", { id: r.id, text: input.value });
      else if (ev.key === "Escape") restore();
    });
    input.addEventListener("blur", restore);
    textEl.replaceWith(input); input.focus();
  };
  card.append(textEl);
  const meta = el("div", "tdo-card-meta");
  meta.append(el("span", "", r.container && r.container.name || ""));
  if (r.waiting) meta.append(el("span", "tdo-card-wait", "⧗ " + r.waiting));
  // delegation: inline for open cards, looked up by id for the Done column
  const dg = r.delegation || delegationFor(r.id);
  if (dg) meta.append(delegationChip(dg, true));
  if (r.rock) meta.append(el("span", "tdo-card-rock", "⧗ " + r.rock.split("/").pop()));
  const open = el("button", "tdo-open-chevron", "›");
  open.title = "open — description, plan, thread";
  open.onclick = (e) => { e.stopPropagation(); openTodoPanel(r); };
  open.onmousedown = (e) => e.stopPropagation();
  meta.append(open);
  card.append(meta);
  // REVIEW cards carry the result up front — reading it IS the column's job
  if (colKey === "review" && dg) {
    const acts = el("div", "tdo-card-acts");
    const read = pillLight("read the result →", () => openResult(dg, r.text));
    read.onmousedown = (e) => e.stopPropagation();
    acts.append(read);
    card.append(acts);
  }
  return card;
}

// boardMove maps a drag to the existing endpoints — nothing new writes.
function boardMove(id, from, to) {
  if (from === to) return;
  const row = (todosCache.rows || []).find((r) => r.id === id);
  switch (to) {
    case "done":
      todosApi("/api/todos/check", { id, checked: true });
      return;
    case "open":
      if (from === "done") { todosApi("/api/todos/check", { id, checked: false }); return; }
      if (from === "delegated") { showToast("Still out with the harness — it lands in REVIEW when it comes back"); return; }
      if (from === "review") { showToast("Read the result first — then Done, or drag back to DELEGATED to send it out again"); return; }
      return;
    case "review":
      showToast("REVIEW fills itself the moment delegated work comes back");
      return;
    case "delegated":
      // from REVIEW this is a RE-delegation: the owner read the result and is
      // sending it back out, so the comment is the point — it rides the work
      // order to the agent (POST /api/todos/delegate {comment}).
      if (row) openDelegatePicker(row, from === "review");
      return;
  }
}
