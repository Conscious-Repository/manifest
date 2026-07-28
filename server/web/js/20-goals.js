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

function renderOrient(areas) {
  els.areasRows.innerHTML = "";
  renderOrientMeta(areas);
  if (!areas.length) { els.areasRows.appendChild(emptyRow("No areas yet — add one.")); return; }
  areas.forEach((area) => els.areasRows.appendChild(orientArea(area)));
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
  if (!g.serves) reasons.push("unlinked");
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
    });
    card.appendChild(yr);
  } else {
    yr.appendChild(ghostInput("＋ set a 2026 goal", "o-yr-ghost", (v) =>
      goalsApi("POST", "/api/goals/item", { area: area.name, parentId: "", section: "annual", text: v, owner: "me" })));
    card.appendChild(yr);
  }

  // Rocks
  const rocks = el("div", "o-rocks");
  (area.rocks || []).forEach((g) => rocks.appendChild(rockNode(g, area.name)));
  rocks.appendChild(rockComposer(area));
  card.appendChild(rocks);
  return card;
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

// rockNode: the collapsed row — dot · name · position → next-action — and, when
// expanded, the stage trail + current-stage tasks.
function rockNode(g, areaName) {
  const wrap = el("div", "o-rock-wrap");
  wrap.dataset.goalId = g.id; // #/goals/<id> deep-link anchor
  const open = goalsUI.expanded.has(g.id);

  const row = el("div", "o-rock" + (open ? " open" : ""));
  row.append(el("span", "o-dot"));
  const name = el("span", "o-rk-name" + (g.checked ? " done" : ""), g.text);
  row.appendChild(name);
  // §3 lint: one muted dot after the name when a condition fires; reasons on hover.
  if (!g.checked) {
    const reasons = rockLint(g);
    if (reasons.length) {
      const dot = el("span", "o-lint-dot", "·");
      dot.title = reasons.join(" · ");
      row.appendChild(dot);
    }
  }

  const stages = g.children || [];
  const cur = stages.find((c) => !c.checked);
  const next = el("span", "o-rk-next");
  if (g.checked || (!cur && stages.length)) {
    next.appendChild(el("span", "o-done-hint", "done — complete it"));
  } else if (cur) {
    // next-action = the OLDEST open tethered todo; the stage name alone is
    // the fallback — which is also the stall face (task-substrate §1)
    const t = rockTodos(g.id)[0];
    if (t) {
      next.appendChild(document.createTextNode(cur.text + " → "));
      next.appendChild(el("span", "cur", t.text));
    } else {
      next.appendChild(el("span", "cur", cur.text));
    }
  }
  row.appendChild(next);
  row.addEventListener("click", () => {
    if (goalsUI.expanded.has(g.id)) goalsUI.expanded.delete(g.id);
    else goalsUI.expanded.add(g.id);
    renderOrient((state.goalsDoc && state.goalsDoc.areas) || []);
  });
  wrap.appendChild(row);

  if (open) {
    // expanded: rename is click-to-edit on the name (don't toggle)
    name.addEventListener("click", (e) => e.stopPropagation());
    clickToEdit(name, () => g.text, (v) =>
      goalsApi("PATCH", "/api/goals/item", { id: g.id, text: v }));
    wrap.appendChild(rockExpand(g, stages, cur, areaName));
  }
  return wrap;
}

// rockExpand: the trail (✓ done / → current / plain future) + the WINDOW into
// the task substrate: the Rock's open tethered todos as live checkboxes and a
// "+ task" ghost that writes to do.md (goals.md never stores a task). Frozen
// pre-split history renders collapsed/muted per stage.
function rockExpand(g, stages, cur, areaName) {
  const box = el("div", "o-expand");

  // §4 finish-line block: UNTIL + PROOF (with kpi right-aligned), above the trail.
  const patch = (field) => (v) => goalsApi("PATCH", "/api/goals/item", { id: g.id, [field]: v });
  box.appendChild(finishRow("UNTIL", g.until, "＋ done when…", patch("until")));
  const kpiCell = finishKpi(g.kpi, patch("kpi"));
  box.appendChild(finishRow("PROOF", g.verify, "＋ proven by…", patch("verify"), kpiCell));

  stages.forEach((st) => {
    const isCur = st === cur;
    const open = isCur || goalsUI.expanded.has(st.id);
    const line = el("div", "o-st" + (st.checked ? " done" : isCur ? " cur" : "") + (isCur ? "" : " toggle"));
    line.dataset.goalId = st.id;
    // Stage checkbox — the way to mark a stage complete (checking advances the
    // trail: the next open stage becomes current). Same glyph as task rows.
    const check = el("button", "check o-st-check" + (st.checked ? " on" : ""), st.checked ? "✓" : "○");
    check.title = st.checked ? "reopen this stage" : "mark this stage complete";
    check.addEventListener("click", (e) => {
      e.stopPropagation(); // never toggle expansion
      goalsApi("POST", "/api/goals/check", { id: st.id, checked: !st.checked });
    });
    line.appendChild(check);
    const label = el("span", "o-st-label", (isCur ? "→ " : "") + st.text);
    line.appendChild(label);
    if (!isCur) {
      line.append(el("span", "o-st-caret", open ? "▾" : "▸"));
      line.title = "click to add or view tasks";
      line.addEventListener("click", () => {
        if (goalsUI.expanded.has(st.id)) goalsUI.expanded.delete(st.id);
        else goalsUI.expanded.add(st.id);
        renderOrient((state.goalsDoc && state.goalsDoc.areas) || []);
      });
    }
    box.appendChild(line);
    if (open) {
      // editing the stage text stops propagation (clickToEdit already does), so
      // clicking the text edits while clicking the rest of the line toggles.
      clickToEdit(label, () => st.text, (v) =>
        goalsApi("PATCH", "/api/goals/item", { id: st.id, text: v }));
      // §4: stages show verify/kpi when non-empty (no ghosts at stage level).
      const stPatch = (field) => (v) => goalsApi("PATCH", "/api/goals/item", { id: st.id, [field]: v });
      if (st.verify) box.appendChild(finishRow("PROOF", st.verify, "", stPatch("verify"), finishKpi(st.kpi, stPatch("kpi")), true));
      else if (st.kpi) box.appendChild(finishRow("", "", "", null, finishKpi(st.kpi, stPatch("kpi")), true));
      // frozen pre-split history — collapsed, muted, read-only
      if ((st.frozen || []).length) {
        const key = st.id + "#history";
        const h = el("button", "tdo-wait-foot", (goalsUI.expanded.has(key) ? "▾" : "▸") + " history · " + st.frozen.length);
        h.onclick = (e) => {
          e.stopPropagation();
          if (goalsUI.expanded.has(key)) goalsUI.expanded.delete(key);
          else goalsUI.expanded.add(key);
          renderOrient((state.goalsDoc && state.goalsDoc.areas) || []);
        };
        box.appendChild(h);
        if (goalsUI.expanded.has(key)) st.frozen.forEach((ln) =>
          box.appendChild(el("div", "o-frozen-line", ln.trim().replace(/^[-*]\s*/, ""))));
      }
    }
  });
  box.appendChild(ghostInput("+ stage", "o-st-ghost", (v) =>
    goalsApi("POST", "/api/goals/item", { parentId: g.id, text: v, owner: "me" }),
    "what state will you have reached?"));

  // the WINDOW: open todos tethered to this Rock — live checkboxes reading and
  // writing to do.md; checking stamps the Rock's moved:: server-side.
  if (!g.checked) {
    const tethered = rockTodos(g.id);
    if (tethered.length) box.appendChild(el("div", "o-todo-head", "OPEN TASKS · to do.md"));
    tethered.forEach((t) => {
      const row = el("div", "o-tk");
      const check = el("button", "check wk-check", "○");
      check.addEventListener("click", async (e) => {
        e.stopPropagation();
        try { await postJSONOk("/api/todos/check", { id: t.id, checked: true }); } catch (err) {}
        loadGoals();
      });
      row.append(check, el("span", "o-tk-text", t.text));
      if (t.state === "waiting") row.append(el("span", "tdo-tag", "⏳ " + t.waiting));
      row.append(el("span", "tdo-age", t.ageDays > 0 ? t.ageDays + "d" : ""));
      box.appendChild(row);
    });
    box.appendChild(ghostInput("+ task", "o-tk-ghost", async (v) => {
      try { await postJSONOk("/api/todos/item", { text: v, domain: areaName, rock: g.id }); } catch (err) {}
      loadGoals();
    }, "what advances this rock…"));
  }
  if (!g.checked) box.appendChild(completeControl(g));
  return box;
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
