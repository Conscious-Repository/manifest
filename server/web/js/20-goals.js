// ================= Goals page =================

async function goalsApi(method, path, body) {
  setSaveState("saving");
  try {
    await fetch(path, {
      method,
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  await loadGoals();
}

// ---- ORIENT: the ladder felt top-to-bottom (goals-orient plan) ----
// Vision → 1-year → Rocks; each Rock one collapsed row (name · position →
// next-action), click to expand the stage trail + the CURRENT stage's tasks.
// Expansion lives in JS (never the DOM) so it survives the refetch-re-render
// every mutation triggers via goalsApi.

const goalsUI = { expanded: new Set(), tab: "orient" };

async function loadGoals(focusId) {
  try {
    // GOALS is a WINDOW over the task substrate — fetch both files' views
    const [doc, tv] = await Promise.all([
      (await fetch("/api/goals")).json(),
      fetch("/api/todos").then((r) => r.json()).catch(() => null),
    ]);
    if (tv) todosCache = tv;
    state.goalsDoc = doc;
    // deep-links (#/goals/<id>) auto-expand the rock AND stage containing the target
    if (focusId) expandAncestors(doc.areas || [], focusId);
    setGoalsTab("orient");
    renderOrient(doc.areas || []);
    if (focusId) focusGoal(focusId);
  } catch (e) { setSaveState("error"); }
}

// expandAncestors opens the rock and stage on the path to id, so a deep-linked
// task under any (even collapsed, non-current) stage still renders + flashes.
function expandAncestors(areas, id) {
  const findPath = (node, acc) => {
    if (node.id === id) return acc;
    for (const c of node.children || []) {
      const p = findPath(c, acc.concat(c));
      if (p) return p;
    }
    return null;
  };
  for (const a of areas) {
    for (const rock of a.rocks || []) {
      const path = findPath(rock, [rock]);
      if (path) {
        goalsSelArea = a.name; // the rail selects the area holding the target
        if (path[0]) goalsUI.expanded.add(path[0].id); // the rock
        if (path[1]) goalsUI.expanded.add(path[1].id); // the stage
        return;
      }
    }
  }
}

// setGoalsTab flips ORIENT ↔ HISTORY visibility + the tab chips.
function setGoalsTab(tab) {
  goalsUI.tab = tab;
  const orient = tab === "orient";
  els.areasRows.hidden = !orient;
  const add = document.getElementById("addArea");
  if (add) add.hidden = !orient;
  const hist = document.getElementById("historyRows");
  if (hist) hist.hidden = orient;
  document.getElementById("tabOrient").classList.toggle("on", orient);
  document.getElementById("tabHistory").classList.toggle("on", !orient);
}

// focusGoal scrolls to a goal's node and flashes it — the #/goals/<id> deep
// link (rock-stalled signals, goal-referencing feed cards).
function focusGoal(id) {
  const node = els.areasRows.querySelector(`[data-goal-id="${CSS.escape(id)}"]`);
  if (!node) return;
  node.scrollIntoView({ behavior: "smooth", block: "center" });
  node.classList.add("goal-flash");
  setTimeout(() => node.classList.remove("goal-flash"), 2400);
}

// Rev 2: ONE editable outline per area — a 176px Areas rail selects the
// area; the content shows every rock → stage → task, no expanding, no cards.
let goalsSelArea = ""; // rail selection (survives re-renders)

function renderOrient(areas) {
  els.areasRows.innerHTML = "";
  renderOrientMeta(areas);
  if (!areas.length) { els.areasRows.appendChild(emptyRow("No areas yet — add one.")); return; }
  if (!goalsSelArea || !areas.find((a) => a.name === goalsSelArea)) goalsSelArea = areas[0].name;
  const shell = el("div", "go-shell");
  const rail = el("div", "go-rail");
  rail.append(el("div", "go-rail-label", "Areas"));
  areas.forEach((a) => {
    const open = (a.rocks || []).filter((r) => !r.checked).length;
    const b = el("button", "go-rail-item" + (a.name === goalsSelArea ? " active" : ""));
    b.append(el("span", "go-rail-name", a.name));
    b.append(el("span", "go-rail-count", open ? String(open) : ""));
    b.onclick = () => { goalsSelArea = a.name; renderOrient(areas); };
    rail.append(b);
  });
  shell.append(rail);
  const content = el("div", "go-content");
  content.append(orientArea(areas.find((a) => a.name === goalsSelArea)));
  shell.append(content);
  els.areasRows.append(shell);
  if (typeof railSetCount === "function") {
    railSetCount("goals", areas.reduce((n, a) => n + (a.rocks || []).filter((r) => !r.checked).length, 0));
  }
}

// Header meta: "2026 · Q3" plus a faint load line when >5 rocks are active.
function renderOrientMeta(areas) {
  const meta = document.getElementById("goalsMeta");
  if (!meta) return;
  const now = new Date();
  const q = Math.floor(now.getMonth() / 3) + 1;
  let txt = `${now.getFullYear()} · Q${q}`;
  const rocks = (areas || []).flatMap((a) => (a.rocks || []).filter((r) => !r.checked));
  if (rocks.length > 5) txt += ` · ${rocks.length} rocks · heavy`;
  const noFinish = rocks.filter((r) => !r.until).length;
  if (noFinish > 0) txt += ` · ${noFinish} without finish lines`;
  meta.textContent = txt;
}

// rockLint (§3): deterministic conditions computed from the view, no text
// analysis. Returns the plain reasons a Rock's quiet dot fires (empty = clean).
// rockTodos: the Rock's open tethered todos from the substrate, oldest first.
function rockTodos(rockId) {
  const out = [];
  const scan = (list) => (list || []).forEach((t) => { if (t.rock === rockId && t.state !== "done") out.push(t); });
  ((todosCache && todosCache.domains) || []).forEach((dm) => {
    scan(dm.todos);
    (dm.buckets || []).forEach((bk) => scan(bk.todos));
  });
  out.sort((a, b) => (a.added || "").localeCompare(b.added || ""));
  return out;
}

function rockLint(g) {
  const reasons = [];
  if (!g.until) reasons.push("no finish line");
  if (!g.verify) reasons.push("no check");
  if (!(g.serves || []).length) reasons.push("unlinked");
  const hasOpenTask = rockTodos(g.id).length > 0; // substrate join, not task depth
  let staleDays = 0;
  if (g.moved) {
    const d = Math.floor((Date.now() - new Date(g.moved + "T00:00:00").getTime()) / 86400000);
    if (d >= 14) staleDays = d;
  }
  if (!hasOpenTask || staleDays >= 14) {
    reasons.push(staleDays >= 14 ? `stalled ${staleDays}d` : "stalled");
  }
  return reasons;
}

function orientArea(area) {
  const card = el("div", "o-area");

  // area name — mono uppercase; click to rename (PATCH /api/areas)
  const name = el("div", "o-area-name", area.name);
  clickToEdit(name, () => area.name, (v) =>
    goalsApi("PATCH", "/api/areas", { name: area.name, newName: v }));
  // soft one-Rock-per-domain lint (todos-surface §5a): note + quiet dot, never blocked
  const activeRocks = (area.rocks || []).filter((r) => !r.checked && (!r.status || r.status === "active"));
  if (activeRocks.length > 1) {
    name.append(el("span", "o-rock-lint", " ● " + activeRocks.length + " rocks"));
    name.title = activeRocks.length + " active Rocks in " + area.name + " — EOS says one; consider moving one to TODOS or closing it";
  }
  card.appendChild(name);

  // North Star — one bold line; ghost when unset
  if (area.northStar) {
    const ns = el("div", "o-ns", area.northStar);
    clickToEdit(ns, () => area.northStar, (v) =>
      goalsApi("PATCH", "/api/areas", { name: area.name, northStar: v }));
    card.appendChild(ns);
  } else {
    card.appendChild(ghostInput("＋ set a north star", "o-ns-ghost", (v) =>
      goalsApi("PATCH", "/api/areas", { name: area.name, northStar: v })));
  }

  // 1-year rung — muted one-liners; the empty middle rung invites filling
  const yr = el("div", "o-yr");
  const yearLabel = el("b", "", (area.year || String(new Date().getFullYear())) + ":");
  yr.appendChild(yearLabel);
  const annuals = area.annuals || [];
  if (annuals.length) {
    annuals.forEach((an, i) => {
      if (i > 0) yr.appendChild(document.createTextNode(" · "));
      const span = el("span", "o-yr-goal" + (an.checked ? " done" : ""), an.text);
      clickToEdit(span, () => an.text, (v) =>
        goalsApi("PATCH", "/api/goals/item", { id: an.id, text: v }));
      yr.appendChild(span);
      yr.appendChild(quietDanger("✕", () => goalsApi("DELETE", "/api/goals/item", { id: an.id })));
    });
    // an area with annuals can still add another (the 1-year rung grows)
    yr.appendChild(document.createTextNode(" "));
    yr.appendChild(ghostInput("＋", "o-yr-ghost", (v) =>
      goalsApi("POST", "/api/goals/item", { area: area.name, parentId: "", section: "annual", text: v, owner: "me" }),
      "another 1-year goal…"));
    card.appendChild(yr);
  } else {
    yr.appendChild(ghostInput("＋ set a 2026 goal", "o-yr-ghost", (v) =>
      goalsApi("POST", "/api/goals/item", { area: area.name, parentId: "", section: "annual", text: v, owner: "me" })));
    card.appendChild(yr);
  }

  // Rocks — the outline: every item visible, each line click-to-edit
  const rocks = el("div", "o-rocks");
  (area.rocks || []).forEach((g) => rocks.appendChild(rockOutline(g, area.name)));
  rocks.appendChild(rockComposer(area));
  card.appendChild(rocks);

  // unanchored — the area's domain todos advancing no rock, plus its open
  // issues (they live on the TODOS surface; shown here quietly so they can
  // be tethered — or converted-and-tethered — in place)
  const un = areaUnanchored(area.name);
  if (un.todos.length || un.issues.length) {
    const foot = el("div", "go-unanchored");
    const head = el("div", "go-un-head");
    head.append(el("span", "go-un-title", "unanchored"),
      el("span", "go-un-count", String(un.todos.length + un.issues.length)),
      el("span", "go-un-hint", "in TODOS · advancing no rock — link to place"));
    foot.append(head);
    un.todos.forEach((t) => {
      const row = goTaskRow(t);
      const link = el("button", "go-un-link", "→ rock…");
      link.onclick = () => openTetherPicker(link, { rock: "" }, area.name, async (p) => {
        if (!p.rock) return;
        try { await postJSONOk("/api/todos/update", { id: t.id, rock: p.rock, stage: p.stage }); } catch (err) {}
        loadGoals();
      });
      row.insertBefore(link, row.lastChild); // before the age cell
      foot.append(row);
    });
    un.issues.forEach((is) => {
      const row = el("div", "go-task go-un-issue");
      row.append(el("span", "go-un-flag", "⚑"), el("span", "go-task-text", is.text));
      const link = el("button", "go-un-link", "→ task on rock…");
      link.title = "convert this issue to a task tethered to a rock";
      link.onclick = () => openTetherPicker(link, { rock: "" }, area.name, async (p) => {
        if (!p.rock) return;
        try { await postJSONOk("/api/todos/issue/to-todo", { id: is.id, rock: p.rock, stage: p.stage }); } catch (err) {}
        loadGoals();
      });
      row.append(link);
      foot.append(row);
    });
    card.appendChild(foot);
  }
  return card;
}

// areaUnanchored — open todos in the area's DOMAIN (loose + buckets) that
// carry no [rock::] tether, plus the domain's open issues.
function areaUnanchored(areaName) {
  const todos = [];
  const issues = [];
  const scan = (list) => (list || []).forEach((t) => {
    if (t.state !== "done" && !t.rock) todos.push(t);
  });
  ((todosCache && todosCache.domains) || []).forEach((dm) => {
    if (dm.name !== areaName) return;
    scan(dm.todos);
    (dm.buckets || []).forEach((bk) => scan(bk.todos));
    (dm.issues || []).forEach((is) => { if (!is.checked) issues.push(is); });
  });
  todos.sort((a, b) => (a.added || "").localeCompare(b.added || ""));
  return { todos, issues };
}

// rockComposer (§2): the soft gate. The ＋ rock ghost opens three fields —
// name / done when / proven by. Name alone saves (quick capture survives); a
// skipped finish line just leaves the Rock lint-flagged, nothing blocks.
function rockComposer(area) {
  const ghost = el("button", "o-ghost o-rock-ghost", "＋ rock");
  ghost.addEventListener("click", () => {
    const box = el("div", "o-composer");
    const name = el("input", "o-edit o-composer-name"); name.placeholder = "name the rock…";
    const until = el("input", "o-edit"); until.placeholder = "done when…            (skippable)";
    const verify = el("input", "o-edit"); verify.placeholder = "proven by…            (skippable)";
    const done = () => {
      const t = name.value.trim();
      if (!t) { box.replaceWith(ghost); return; }
      goalsApi("POST", "/api/goals/item", {
        area: area.name, parentId: "", section: "rock", text: t, owner: "me",
        until: until.value.trim(), verify: verify.value.trim(),
      });
    };
    [name, until, verify].forEach((i) => i.addEventListener("keydown", (e) => {
      if (e.key === "Enter") done();
      else if (e.key === "Escape") box.replaceWith(ghost);
    }));
    const save = el("button", "o-composer-save", "add rock");
    save.addEventListener("click", done);
    box.append(name, until, verify, save);
    ghost.replaceWith(box);
    name.focus();
  });
  return ghost;
}

// ---- ladder-connection edits (chain fields + move + delete) ----
// The portal (aion.bio) reads serves/owner/quarter and the rock→30-day
// parentage; all of it is editable HERE, not just lintable.

function areaView(name) {
  return ((state.goalsDoc && state.goalsDoc.areas) || []).find((a) => a.name === name);
}

// quietAct / quietDanger: mono micro-actions; danger arms on first click
// ("sure? click again") instead of a browser dialog.
function quietAct(label, fn) {
  const b = el("button", "o-quiet-act", label);
  b.onclick = (e) => { e.stopPropagation(); fn(); };
  return b;
}
function quietDanger(label, fn) {
  const b = el("button", "o-quiet-act danger", label);
  let armed = false, t;
  b.onclick = (e) => {
    e.stopPropagation();
    if (armed) { clearTimeout(t); fn(); return; }
    armed = true;
    b.textContent = "sure? click again";
    t = setTimeout(() => { armed = false; b.textContent = label; }, 3000);
  };
  return b;
}

// ---- owner editing with the aion people registry ----
// In the Aion area, owner fields autocomplete from system/aion/people.md
// (initials + names); elsewhere they stay plain inputs. The registry is
// fetched once per page load.
let goalsPeopleCache = null;
async function aionPeopleList() {
  if (goalsPeopleCache) return goalsPeopleCache;
  try { goalsPeopleCache = ((await (await fetch("/api/aion")).json()) || {}).people || []; }
  catch (e) { goalsPeopleCache = []; }
  return goalsPeopleCache;
}
function isAionArea(name) { return (name || "").toLowerCase() === "aion"; }

// ownerEditable: click swaps the node for a people typeahead (Aion) or a
// plain input (other areas). save() receives bare initials/name, no "@".
function ownerEditable(node, getValue, save, useRegistry) {
  if (!useRegistry) { clickToEdit(node, getValue, save); return; }
  node.classList.add("o-editable");
  node.title = "owner — type initials or a name (from system/aion/people.md)";
  node.addEventListener("click", async (e) => {
    e.stopPropagation();
    const people = await aionPeopleList();
    const rerender = () => renderOrient((state.goalsDoc && state.goalsDoc.areas) || []);
    const commit = (v) => {
      v = v.replace(/^@/, "").trim();
      if (v && v !== getValue()) save(v);
      else rerender();
    };
    const ta = typeahead({
      placeholder: "initials…",
      initial: getValue(),
      suggest: (q, add) => {
        people
          .filter((p) => !q || p.initials.toLowerCase().includes(q) || (p.name || "").toLowerCase().includes(q))
          .slice(0, 8)
          .forEach((p) => add(p.initials + " · " + (p.name || ""), "", () => commit(p.initials)));
      },
      onEnter: commit,
      onEscape: rerender,
      onBlurGone: () => commit(ta.value()),
    });
    node.replaceWith(ta.el);
    ta.focus();
  });
}

// ownerRow: the rock's OWNER line (with the quarter cell right-aligned).
function ownerRow(g, areaName, right) {
  const row = el("div", "o-fl-row");
  row.append(el("span", "o-fl-label", "OWNER"));
  const has = g.owner && g.owner !== "me";
  const node = has ? el("span", "o-fl-val", "@" + g.owner)
    : el("button", "o-ghost o-fl-ghost", "＋ owner…");
  ownerEditable(node, () => (has ? g.owner : ""),
    (v) => goalsApi("PATCH", "/api/goals/item", { id: g.id, owner: v }), isAionArea(areaName));
  row.append(node);
  if (right) row.append(right);
  return row;
}

// servesRow: the rock → 1-year links (1:many — Series A feeds ALL the
// annuals): one chip per linked goal (✕ removes it), plus a ghost that adds
// another from the annuals not yet linked. Every change PATCHes the full
// list.
function servesRow(g, areaName) {
  const row = el("div", "o-fl-row o-serves");
  row.append(el("span", "o-fl-label", "SERVES"));
  const annuals = (areaView(areaName) || {}).annuals || [];
  const linked = (g.serves || []).slice();
  const save = (list) => goalsApi("PATCH", "/api/goals/item", { id: g.id, serves: list });
  const chips = el("span", "o-serves-chips");
  linked.forEach((sv) => {
    const an = annuals.find((a) => a.id === sv);
    const chip = el("span", "o-serves-chip" + (an ? "" : " broken"));
    chip.append(el("span", "", an ? an.text : sv + " (broken link)"));
    const x = el("button", "o-serves-x", "✕");
    x.title = "unlink from this 1-year goal";
    x.onclick = (e) => { e.stopPropagation(); save(linked.filter((v) => v !== sv)); };
    chip.append(x);
    chips.append(chip);
  });
  row.append(chips);
  const remaining = annuals.filter((an) => !linked.includes(an.id));
  if (remaining.length) {
    const ghost = el("button", "o-ghost o-fl-ghost",
      linked.length ? "＋ also serves…" : "＋ serves a 1-year goal…");
    ghost.onclick = (e) => {
      e.stopPropagation();
      openPicker("serves which 1-year goal?",
        [{ area: areaName, items: remaining.map((an) => ({ id: an.id, text: an.text })) }],
        (id) => save(linked.concat([id])),
        "no 1-year goals in " + areaName + " yet — add one above first");
    };
    row.append(ghost);
  }
  return row;
}

// aliasRow: portal-matcher vocabulary that resolves to this goal — free-text
// chips (a backlog item's rock:: "fundraising" resolves here without
// rewriting the item). ✕ removes; the ghost adds arbitrary text. PATCHes the
// full list. Mirrors servesRow but with a text input, not an annual picker.
function aliasRow(g) {
  const row = el("div", "o-fl-row o-serves");
  row.append(el("span", "o-fl-label", "ALIASES"));
  const linked = (g.aliases || []).slice();
  const save = (list) => goalsApi("PATCH", "/api/goals/item", { id: g.id, aliases: list });
  const chips = el("span", "o-serves-chips");
  linked.forEach((al) => {
    const chip = el("span", "o-serves-chip");
    chip.append(el("span", "", al));
    const x = el("button", "o-serves-x", "✕");
    x.title = "remove this alias";
    x.onclick = (e) => { e.stopPropagation(); save(linked.filter((v) => v !== al)); };
    chip.append(x);
    chips.append(chip);
  });
  row.append(chips);
  row.append(ghostInput(linked.length ? "＋ alias…" : "＋ add a portal alias…", "o-fl-ghost", (v) => {
    v = v.trim();
    if (v && !linked.includes(v)) save(linked.concat([v]));
  }));
  return row;
}

// datesRow: explicit START / DUE ISO dates the portal timeline reads (§7).
// Native date pickers; empty is fine (portal falls back to the quarter window).
// Blanking a field and committing clears it. Rocks only.
function datesRow(g) {
  const row = el("div", "o-fl-row o-dates");
  row.append(el("span", "o-fl-label", "DATES"));
  const field = (label, value, key) => {
    const wrap = el("label", "o-date-field");
    wrap.append(el("span", "o-date-cap", label));
    const inp = document.createElement("input");
    inp.type = "date";
    inp.className = "o-date-in";
    inp.value = value || "";
    inp.addEventListener("change", () =>
      goalsApi("PATCH", "/api/goals/item", { id: g.id, [key]: inp.value.trim() }));
    wrap.append(inp);
    return wrap;
  };
  row.append(field("start", g.start, "start"));
  row.append(field("due", g.due, "due"));
  return row;
}

// quarterCell: right-aligned quarter (like the kpi cell).
function quarterCell(g) {
  const save = (v) => goalsApi("PATCH", "/api/goals/item", { id: g.id, quarter: v });
  if (g.quarter) {
    const v = el("span", "o-fl-kpi", g.quarter);
    clickToEdit(v, () => g.quarter, save);
    return v;
  }
  return ghostInput("＋ quarter", "o-fl-kpi o-fl-ghost", save);
}

// moveGoalPicker: re-parent a rock/stage under another rock (or promote a
// stage to a top-level rock) — the 90↔30-day connection.
function moveGoalPicker(g, areaName) {
  const area = areaView(areaName);
  const contains = (node, id) => (node.children || []).some((c) => c.id === id || contains(c, id));
  const rocks = ((area && area.rocks) || []).filter((r) => r.id !== g.id && !contains(g, r.id));
  const items = rocks.map((r) => ({ id: r.id, text: r.text }));
  const isTop = ((area && area.rocks) || []).some((r) => r.id === g.id);
  if (!isTop) items.push({ id: "", text: "— promote to a top-level rock —" });
  openPicker("move “" + g.text + "” under…", [{ area: areaName, items }],
    (id) => goalsApi("POST", "/api/goals/move", { id: g.id, parentId: id }),
    "no other rocks in " + areaName);
}

// goTaskRow — one substrate task line inside the outline: live checkbox,
// click-to-edit text, age.
function goTaskRow(t) {
  const row = el("div", "go-task");
  const tc = el("button", "go-check", "○");
  tc.title = "done";
  tc.onclick = async (e) => {
    e.stopPropagation();
    try { await postJSONOk("/api/todos/check", { id: t.id, checked: true }); } catch (err) {}
    loadGoals();
  };
  row.append(tc);
  const tt = el("span", "go-task-text", t.text);
  clickToEdit(tt, () => t.text, async (v) => {
    try { await postJSONOk("/api/todos/update", { id: t.id, text: v }); } catch (err) {}
    loadGoals();
  });
  row.append(tt);
  row.append(el("span", "go-task-age", t.ageDays > 0 ? t.ageDays + "d" : ""));
  return row;
}

// rockOutline (Rev 2): the whole rock inline — name (15px/500) with UNTIL as
// a quiet tag and the ● lint meta on the rock's own line, the stage trail
// (→ marks current), and the current stage's tasks from the substrate. Left
// rule: ink when stalled · accent when tasked · accent-soft otherwise.
function rockOutline(g, areaName) {
  const stages = g.children || [];
  const cur = stages.find((c) => !c.checked);
  const reasons = g.checked ? [] : rockLint(g);
  const stalled = reasons.find((r) => r.startsWith("stalled"));
  const tethered = g.checked ? [] : rockTodos(g.id);
  // a task with a [stage::] naming one of this rock's stages nests THERE;
  // the rest ride the current stage (open work by default advances it)
  const byStage = {};
  const looseTasks = [];
  tethered.forEach((t) => {
    const m = t.stage && stages.find((s) => s.text === t.stage);
    if (m) (byStage[m.id] = byStage[m.id] || []).push(t);
    else looseTasks.push(t);
  });
  const rule = stalled ? "stalled" : (tethered.length ? "current" : "quiet");
  const wrap = el("div", "go-rock " + rule + (g.checked ? " done" : ""));
  wrap.dataset.goalId = g.id;

  // line 1 — name · UNTIL tag · lint meta · open-task count right
  const line = el("div", "go-rock-line");
  const name = el("span", "go-rock-name" + (g.checked ? " done" : ""), g.text);
  clickToEdit(name, () => g.text, (v) => goalsApi("PATCH", "/api/goals/item", { id: g.id, text: v }));
  line.append(name);
  const patchUntil = (v) => goalsApi("PATCH", "/api/goals/item", { id: g.id, until: v });
  if (g.until) {
    const tag = el("span", "go-until", "until " + g.until);
    clickToEdit(tag, () => g.until, patchUntil);
    line.append(tag);
  } else if (!g.checked) {
    line.append(ghostInput("until…", "go-until-ghost", patchUntil, "done when…"));
  }
  if (reasons.length) {
    const lint = el("span", "go-lint", "● " + reasons.join(" · "));
    lint.title = "computed from the ladder — set a finish line / capture a task to clear";
    line.append(lint);
  }
  if (tethered.length) line.append(el("span", "go-open-count", tethered.length + " open"));
  wrap.append(line);

  // stage trail — every stage visible, → marks the current one
  stages.forEach((st) => {
    const isCur = st === cur;
    const sl = el("div", "go-stage" + (st.checked ? " done" : "") + (isCur ? " cur" : ""));
    sl.dataset.goalId = st.id;
    const check = el("button", "go-check" + (st.checked ? " on" : ""), st.checked ? "✓" : "○");
    check.title = st.checked ? "reopen this stage" : "mark this stage complete";
    check.onclick = (e) => { e.stopPropagation(); goalsApi("POST", "/api/goals/check", { id: st.id, checked: !st.checked }); };
    sl.append(check);
    if (isCur) sl.append(el("span", "go-stage-cur", "→"));
    const label = el("span", "go-stage-text", st.text);
    clickToEdit(label, () => st.text, (v) => goalsApi("PATCH", "/api/goals/item", { id: st.id, text: v }));
    sl.append(label);
    const hasOwner = st.owner && st.owner !== "me";
    if (hasOwner) sl.append(el("span", "go-stage-owner", "@" + st.owner));
    wrap.append(sl);

    // tasks nest under the stage their [stage::] names; the rest ride the
    // current stage (open work by default advances it)
    (byStage[st.id] || []).forEach((t) => wrap.append(goTaskRow(t)));
    if (isCur && !g.checked) {
      looseTasks.forEach((t) => wrap.append(goTaskRow(t)));
      wrap.append(ghostInput("＋ task", "go-task-ghost", async (v) => {
        try { await postJSONOk("/api/todos/item", { text: v, domain: areaName, rock: g.id }); } catch (err) {}
        loadGoals();
      }, "what advances this rock…"));
      // frozen pre-split history — collapsed, muted, read-only
      if ((st.frozen || []).length) {
        const key = st.id + "#history";
        const h = el("button", "tdo-wait-foot", (goalsUI.expanded.has(key) ? "▾" : "▸") + " history · " + st.frozen.length);
        h.onclick = () => {
          if (goalsUI.expanded.has(key)) goalsUI.expanded.delete(key);
          else goalsUI.expanded.add(key);
          renderOrient((state.goalsDoc && state.goalsDoc.areas) || []);
        };
        wrap.append(h);
        if (goalsUI.expanded.has(key)) st.frozen.forEach((ln) =>
          wrap.append(el("div", "o-frozen-line", ln.trim().replace(/^[-*]\s*/, ""))));
      }
    }
  });
  wrap.append(ghostInput("＋ stage", "go-stage-ghost", (v) =>
    goalsApi("POST", "/api/goals/item", { parentId: g.id, text: v, owner: "me" }),
    "what state will you have reached?"));

  // quiet, hover-revealed rock actions: complete (Win/Learn confirm) · move · delete
  if (!g.checked) {
    const acts = el("div", "go-rock-acts");
    acts.append(completeControl(g));
    acts.append(quietAct("nest under another rock…", () => moveGoalPicker(g, areaName)));
    acts.append(quietDanger("delete", () => goalsApi("DELETE", "/api/goals/item", { id: g.id })));
    wrap.append(acts);
  }
  return wrap;
}

// finishRow: a "LABEL   value" line; empty value renders the ghost (skippable);
// non-empty value is click-to-edit. `right` is an optional right-aligned cell
// (kpi). `sub` marks a stage-level (indented, quieter) row.
function finishRow(label, value, ghostText, onSave, right, sub) {
  const row = el("div", "o-fl-row" + (sub ? " sub" : ""));
  if (label) row.append(el("span", "o-fl-label", label));
  if (value) {
    const v = el("span", "o-fl-val", value);
    if (onSave) clickToEdit(v, () => value, onSave);
    row.append(v);
  } else if (ghostText && onSave) {
    row.append(ghostInput(ghostText, "o-fl-ghost", onSave));
  } else {
    row.append(el("span", "o-fl-val", "")); // spacer so kpi keeps its column
  }
  if (right) row.append(right);
  return row;
}

// finishKpi: the right-aligned gauge cell — value click-to-edit, empty a tiny ghost.
function finishKpi(value, onSave) {
  if (value) {
    const v = el("span", "o-fl-kpi", value);
    clickToEdit(v, () => value, onSave);
    return v;
  }
  return ghostInput("＋ kpi", "o-fl-kpi o-fl-ghost", onSave);
}

// completeControl: §5 — a quiet "complete" that opens an inline confirm showing
// the finish line + check verbatim and demanding evidence for a Win.
function completeControl(g) {
  const wrap = el("div", "o-complete-wrap");
  const btn = el("button", "o-complete", "complete");
  btn.title = "Complete this rock";
  btn.addEventListener("click", () => {
    if (wrap.querySelector(".o-confirm")) return;
    btn.hidden = true;
    const panel = el("div", "o-confirm");
    panel.append(el("div", "o-confirm-q", "is this true?"));
    panel.append(el("div", "o-confirm-line",
      "UNTIL   " + (g.until || "no finish line was set")));
    if (g.verify) panel.append(el("div", "o-confirm-line", "PROOF   " + g.verify));
    const ev = el("input", "o-confirm-ev");
    ev.type = "text";
    ev.placeholder = "evidence — a line of proof or a [[wikilink]] (required to win)";
    attachWikilinkAutocomplete(ev);
    panel.append(ev);
    const acts = el("div", "o-confirm-acts");
    const win = el("button", "o-confirm-win", "win →");
    win.disabled = true;
    ev.addEventListener("input", () => { win.disabled = !ev.value.trim(); });
    win.addEventListener("click", () => closeGoal(g.id, "win", "", ev.value.trim()));
    const learn = el("button", "o-confirm-learn", "learn");
    learn.title = "drop it — no proof needed";
    learn.addEventListener("click", () => {
      const note = prompt(`Drop “${g.text}”? Optional note:`);
      if (note !== null) closeGoal(g.id, "learn", note.trim(), "");
    });
    const cancel = el("button", "o-confirm-cancel", "cancel");
    cancel.addEventListener("click", () => { panel.remove(); btn.hidden = false; });
    acts.append(win, learn, cancel);
    panel.append(acts);
    wrap.append(panel);
    ev.focus();
  });
  wrap.append(btn);
  return wrap;
}

// clickToEdit: calm editing — a span that swaps to an input on click; Enter/blur
// saves (→ refetch re-render), Esc restores. No always-on inputs.
function clickToEdit(span, getValue, save) {
  span.classList.add("o-editable");
  span.title = "Click to edit";
  span.addEventListener("click", (e) => {
    e.stopPropagation();
    const orig = getValue();
    const input = document.createElement("input");
    input.className = "o-edit";
    input.value = orig;
    span.replaceWith(input);
    input.focus();
    input.select();
    let settled = false;
    const settle = (commit) => {
      if (settled) return;
      settled = true;
      const v = input.value.trim();
      if (commit && v && v !== orig) save(v);
      else input.replaceWith(span);
    };
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") settle(true);
      else if (ev.key === "Escape") settle(false);
    });
    input.addEventListener("blur", () => settle(true));
  });
}

// ghostInput: a muted "＋ …" affordance that swaps to an input; Enter commits.
// `placeholder` overrides the default (label minus its ＋).
// (ghostInput lives in 05-components.js — the §11 component library)

// closeGoal moves a Rock to the quarter archive file via the close API.
async function closeGoal(id, outcome, note, evidence) {
  setSaveState("saving");
  try {
    const r = await fetch("/api/goals/close", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, outcome, note: note || "", evidence: evidence || "" }) });
    if (!r.ok) throw new Error(await r.text());
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Archive failed: " + (e.message || e)); }
  loadGoals();
}

// evidenceEl renders evidence text, turning any [[wikilink]] into a clickable
// span that opens the note (via the shared resolver). Plain text passes through.
function evidenceEl(text) {
  const frag = document.createDocumentFragment();
  const re = /\[\[([^\]]+)\]\]/g;
  let last = 0, m;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) frag.append(document.createTextNode(text.slice(last, m.index)));
    const link = el("span", "wikilink", m[1]);
    link.addEventListener("click", () => resolveWikilink(m[1]));
    frag.append(link);
    last = re.lastIndex;
  }
  if (last < text.length) frag.append(document.createTextNode(text.slice(last)));
  return frag;
}

// ---- HISTORY: the archive view (first consumer of /api/goals/archives) ----
async function showGoalsHistory() {
  setGoalsTab("history");
  const host = document.getElementById("historyRows");
  if (!host) return;
  host.innerHTML = "";
  try {
    const res = await (await fetch("/api/goals/archives")).json();
    const quarters = res.quarters || [];
    if (!quarters.length) {
      host.appendChild(emptyRow("no closed rocks yet"));
      return;
    }
    quarters.forEach((q) => {
      const entries = q.entries || [];
      const head = el("div", "hist-quarter",
        `${q.quarter} · ${q.wins || 0} won · ${q.learns || 0} learned`);
      host.appendChild(head);
      entries.forEach((e) => {
        const row = el("div", "hist-row");
        row.appendChild(el("span", "hist-glyph" + (e.outcome === "win" ? " win" : ""),
          e.outcome === "win" ? "✓" : "·"));
        row.appendChild(el("span", "hist-text", e.text));
        row.appendChild(el("span", "hist-meta", `${e.area} · ${e.closed || ""}`));
        host.appendChild(row);
        if (e.evidence) {
          const ln = el("div", "hist-note hist-evidence");
          ln.append(document.createTextNode("proof: "));
          ln.append(evidenceEl(e.evidence));
          host.appendChild(ln);
        }
        const sub = [e.reached ? "reached: " + e.reached : "", e.note || ""].filter(Boolean).join(" — ");
        if (sub) host.appendChild(el("div", "hist-note", sub));
      });
    });
  } catch (e) {
    host.appendChild(emptyRow("could not load the archive"));
  }
}

// ---- reusable picker modal ----
function openPicker(title, groups, onPick, emptyHint) {
  els.pickerTitle.textContent = title;
  els.pickerBody.innerHTML = "";
  if (!groups || !groups.length) {
    const e = document.createElement("div");
    e.className = "ro-row empty";
    e.textContent = emptyHint || "Nothing to pick.";
    els.pickerBody.appendChild(e);
  } else {
    groups.forEach((grp) => {
      const head = document.createElement("div");
      head.className = "plate-area";
      head.textContent = grp.area;
      els.pickerBody.appendChild(head);
      grp.items.forEach((it) => {
        const opt = document.createElement("button");
        opt.className = "picker-item";
        opt.textContent = it.text;
        opt.addEventListener("click", () => {
          closePicker();
          if (onPick) onPick(it.id);
        });
        els.pickerBody.appendChild(opt);
      });
    });
  }
  els.pickerModal.hidden = false;
}
function closePicker() { els.pickerModal.hidden = true; }

if (els.addArea) els.addArea.addEventListener("click", () => {
  const name = prompt("New area name:");
  if (name && name.trim()) goalsApi("POST", "/api/areas", { name: name.trim() });
});
// GOALS tab chips route through the hash (deep-linkable, like every other tab)
const goalsTabO = document.getElementById("tabOrient");
const goalsTabH = document.getElementById("tabHistory");
if (goalsTabO) goalsTabO.addEventListener("click", () => { location.hash = "#/goals"; });
if (goalsTabH) goalsTabH.addEventListener("click", () => { location.hash = "#/goals/history"; });

els.pickerClose.addEventListener("click", closePicker);
els.pickerBackdrop.addEventListener("click", closePicker);
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !els.pickerModal.hidden) closePicker();
});
