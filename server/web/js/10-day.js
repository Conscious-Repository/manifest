// ---- day: load + render ----
async function load(date) {
  state.date = date;
  const today = date === isoToday();
  els.dateLabel.textContent = today ? "TODAY" : prettyDate(date);
  const r = await fetch(`/api/day?date=${date}`);
  state.day = await r.json();
  renderDay();
  loadBriefingCard(today);
}

// ---- morning briefing headline (cmd-ctr import P3) ----
// The concierge `briefing` ritual publishes ONE feed digest (profile:
// briefing) each morning; TODAY leads with it. Dismiss = the feed verdict
// (discarded), so the card and the FEED agree on lifecycle.
async function loadBriefingCard(today) {
  const host = document.getElementById("briefingCard");
  if (!host) return;
  host.hidden = true;
  host.innerHTML = "";
  if (!today) return;
  let items = [];
  try { items = ((await (await fetch("/api/feed?status=inbox")).json()).items) || []; } catch (e) { return; }
  const iso = isoToday();
  const brief = items.find((it) => it.profile === "briefing" && (it.date || "").slice(0, 10) === iso);
  if (!brief) return;
  const head = el("div", "briefing-head");
  head.append(el("span", "briefing-label", "BRIEFING"), el("span", "briefing-title", brief.title || ""));
  const dismiss = el("button", "sprt-quiet", "dismiss");
  dismiss.onclick = async () => {
    host.hidden = true;
    try {
      await fetch("/api/feed/" + encodeURIComponent(brief.id) + "/status", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: "discarded" }),
      });
    } catch (e) {}
  };
  head.append(dismiss);
  host.append(head);
  const body = el("div", "briefing-body");
  try { body.append(renderMarkdown(brief.body || brief.why || "", "", { readOnly: true })); }
  catch (e) { body.textContent = brief.body || brief.why || ""; }
  host.append(body);
  host.hidden = false;
}

// Decorative per-row markers for the Goals / Milestones slots (mood, image,
// clock), ported from the vv.xyz design. Purely cosmetic.
const SLOT_ICONS = [
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M8.5 14.5c.9 1.2 2.1 1.8 3.5 1.8s2.6-.6 3.5-1.8"/><circle cx="9" cy="10" r=".6" fill="currentColor"/><circle cx="15" cy="10" r=".6" fill="currentColor"/></svg>',
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3.5" y="5.5" width="17" height="13" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="M5 17l4.5-4 3 2.5L16 12l3 3.5"/></svg>',
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 2"/></svg>',
];
const MONTHS_FULL = ["JANUARY","FEBRUARY","MARCH","APRIL","MAY","JUNE","JULY","AUGUST","SEPTEMBER","OCTOBER","NOVEMBER","DECEMBER"];

function renderDay() {
  const day = state.day;
  // (the "pull from your todos" prep banner is retired — owner call 2026-08-09:
  // planning a day goes through the GOALS / MILESTONES / TASKS pickers only)
  if (day.schedule.length) {
    els.scheduleRange.textContent =
      `${hourLabel(Math.floor(slotMin(day.schedule[0].time) / 60))}–` +
      `${hourLabel(Math.floor(slotMin(day.schedule[day.schedule.length - 1].time) / 60))}`;
  }
  // Rolling windows from the viewed date: 90-day goals = this month → +3 months,
  // 30-day milestone = next month.
  const cur = (+(day.date || "0-1").split("-")[1] - 1 + 12) % 12;
  els.goalsRange.textContent = `${MONTHS_FULL[cur]} – ${MONTHS_FULL[(cur + 3) % 12]}`;
  els.milestonesRange.textContent = MONTHS_FULL[(cur + 1) % 12];
  renderSchedule(day.schedule);
  renderFocus(day);
  renderTasks(day.tasks);
  renderCascadeTasks(day);
}

// ---- Focus: click-to-pick 90-day goals + their auto-filled 30-day milestone.
// Rendered as a unified bordered box of slot rows (vv.xyz layout). ----
function renderFocus(day) {
  const slots = day.focusSlots || 3;
  const focus = day.focus || [];
  els.goalsRows.innerHTML = "";
  els.milestonesRows.innerHTML = "";
  for (let i = 0; i < slots; i++) {
    const pick = focus[i];
    els.goalsRows.appendChild(goalSlot(i, pick));
    els.milestonesRows.appendChild(milestoneSlot(i, pick));
  }
}

function focusRow(i) {
  const row = document.createElement("div");
  row.className = "focus-row";
  const marker = document.createElement("span");
  marker.className = "marker";
  marker.innerHTML = SLOT_ICONS[i % SLOT_ICONS.length];
  row.appendChild(marker);
  return row;
}

function goalSlot(i, pick) {
  const row = focusRow(i);
  if (pick) {
    const txt = document.createElement("span");
    txt.className = "focus-text" + (pick.resolved ? "" : " unresolved");
    txt.textContent = pick.text || pick.goalId;
    txt.title = "Change this focus goal";
    txt.addEventListener("click", () => openGoalPicker(i));
    row.appendChild(txt);
    if (!pick.resolved) {
      const badge = document.createElement("span");
      badge.className = "focus-badge";
      badge.textContent = "unresolved";
      row.appendChild(badge);
    }
    const clear = document.createElement("button");
    clear.className = "icon-btn focus-clear";
    clear.textContent = "✕";
    clear.title = "Clear";
    clear.addEventListener("click", () => setFocus(i, ""));
    row.appendChild(clear);
  } else {
    row.classList.add("empty");
    const ph = document.createElement("span");
    ph.className = "focus-placeholder";
    ph.textContent = "pick a goal";
    row.appendChild(ph);
    row.addEventListener("click", () => openGoalPicker(i));
  }
  return row;
}

function milestoneSlot(i, pick) {
  const row = focusRow(i);
  row.classList.add("milestone");
  if (pick && pick.milestone) {
    const txt = document.createElement("span");
    txt.className = "focus-text milestone-text";
    txt.textContent = pick.milestone.text;
    txt.title = "Change the 30-day milestone";
    txt.addEventListener("click", () => openMilestonePicker(i, pick));
    row.appendChild(txt);
  } else if (pick && pick.resolved) {
    row.classList.add("empty");
    const a = document.createElement("a");
    a.href = "#/goals";
    a.className = "focus-placeholder";
    a.textContent = "set a 30-day goal";
    row.appendChild(a);
  }
  return row;
}

// Pick which 30-day goal is the milestone for a focus slot (its tasks then cascade).
function openMilestonePicker(i, pick) {
  const items = (pick.milestones || []).map((m) => ({ id: m.goalId, text: m.text }));
  if (!items.length) { location.hash = "#/goals"; return; }
  openPicker("Pick a 30-day milestone", [{ area: pick.text, items }], (id) => setMilestone(i, id));
}

async function setFocus(slot, goalId) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/day/focus?date=${state.date}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ slot, goalId }),
    });
    state.day = await r.json();
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  renderDay();
}

async function setMilestone(slot, milestoneId) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/day/focus/milestone?date=${state.date}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ slot, milestoneId }),
    });
    state.day = await r.json();
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  renderDay();
}

// Open the goal picker for a focus slot: EVERY open Rock by area, regardless
// of owner (owner call 2026-08-09) — the ladder in GOALS is the ladder here;
// company rocks owned by others still drive my milestones and tasks.
async function openGoalPicker(slot) {
  const doc = await (await fetch("/api/goals")).json();
  const groups = (doc.areas || [])
    .map((a) => ({
      area: a.name,
      items: (a.rocks || [])
        .filter((g) => !g.checked)
        .map((g) => ({ id: g.id, text: g.text })),
    }))
    .filter((grp) => grp.items.length);
  if (!groups.length) {
    openPicker("Pick a Rock", [], null, "No open Rocks yet — add some on the Goals page.");
    return;
  }
  openPicker("Pick a Rock", groups, (id) => setFocus(slot, id));
}

// Cascade tasks: surface the chosen 30-day's open tasks (not already pulled) as
// quick-add chips that promote into ## Tasks with a [goal:: slug] backlink.
function renderCascadeTasks(day) {
  const host = document.getElementById("focusExtra");
  if (host) host.innerHTML = "";
  // offers are rock-tethered TODOS now (task-substrate) — taskId-keyed
  const existing = new Set((day.tasks || []).map((t) => t.taskId || t.goalId).filter(Boolean));
  const suggestions = [];
  (day.focus || []).forEach((p) => {
    (p.tasks || []).forEach((t) => {
      const key = t.taskId || t.goalId;
      if (!existing.has(key)) suggestions.push({ taskId: t.taskId, goalId: t.goalId, text: t.text, goal: p.text });
    });
  });
  if (!suggestions.length || !host) return;
  const full = (day.tasks || []).filter((t) => t.text).length >= MAX_TASKS;
  const row = document.createElement("div");
  row.id = "cascadeTasks";
  row.className = "cascade-tasks";
  const head = document.createElement("div");
  head.className = "cascade-head";
  head.textContent = full ? "From your focus (tasks full — remove one to add):" : "From your focus:";
  row.appendChild(head);
  const chips = document.createElement("div");
  chips.className = "pool-chips";
  suggestions.forEach((s) => {
    const chip = document.createElement("button");
    chip.className = "pool-chip" + (full ? " disabled" : "");
    const tag = document.createElement("span");
    tag.className = "pool-area";
    tag.textContent = s.goal;
    chip.append(tag, document.createTextNode(" " + s.text));
    if (full) {
      chip.disabled = true;
      chip.title = "Tasks are full — remove one to add this";
    } else {
      chip.title = `Add “${s.text}” to today`;
      chip.addEventListener("click", () => (s.taskId ? pullTodo(s.taskId) : pullGoal(s.goalId)));
    }
    chips.appendChild(chip);
  });
  row.appendChild(chips);
  host.appendChild(row); // #focusExtra — below the even bottom line, under TASKS
}

// Read-only reflection of goals.md (90-/30-day, owner==me). Edited on the
// Goals page, not here.
function renderReadonly(container, items, emptyHint) {
  container.innerHTML = "";
  if (!items || !items.length) {
    const row = document.createElement("div");
    row.className = "ro-row empty";
    row.textContent = emptyHint;
    container.appendChild(row);
    return;
  }
  items.forEach((text) => {
    const row = document.createElement("div");
    row.className = "ro-row";
    row.textContent = text;
    container.appendChild(row);
  });
}

// (renderPrep deleted 2026-08-09 — the unplanned-day "pull from your todos"
// chip pool became a 30-chip wall once the unified projection landed. The
// GOALS / MILESTONES / TASKS pickers are the planning surface; #prepBanner
// stays hidden.)

// openTaskPicker — the task box's picker (goals-orient v2): task row i draws
// from focus slot i's rock (1↔1↔1 column alignment). Options = the rock's open
// substrate tasks (tasks.md tethers + my aion backlog tasks) not already seated
// today; the foot row types a NEW task, captured under the slot's milestone.
function openTaskPicker(pick, stageId) {
  const existing = new Set(((state.day && state.day.tasks) || [])
    .map((t) => t.taskId || t.goalId).filter(Boolean));
  const byId = new Map(); // id → {todo: bool} — route pull by link kind
  const items = (pick.tasks || [])
    .filter((t) => !existing.has(t.taskId || t.goalId))
    .map((t) => {
      const id = t.taskId || t.goalId;
      byId.set(id, !!t.taskId);
      return { id, text: t.text };
    });
  const msLabel = pick.milestone ? pick.milestone.text : "";
  openPicker("Pick a task", [{ area: pick.text + (msLabel ? " · " + msLabel : ""), items }],
    (id) => { byId.get(id) ? pullTodo(id) : pullGoal(id); },
    "No open tasks under this rock yet — type one below.",
    { placeholder: "new task under " + (msLabel || pick.text) + "…",
      onCreate: (txt) => captureTask(stageId, txt) });
}

// captureTask (goals-orient): free-typed day task → appended into goals.md under
// the focus slot's stage with a durable [goal:: id], seated on the day linked.
async function captureTask(stageId, text) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/day/capture?date=${state.date}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ stageId, text }),
    });
    if (!r.ok) throw new Error(await r.text());
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  load(state.date); // reload: the task re-seats linked in its slot row
}

async function pullGoal(goalId) {
  if (collectTasks().length >= MAX_TASKS) return; // hard cap of 3 tasks — remove one first
  setSaveState("saving");
  try {
    await fetch(`/api/day/pull?date=${state.date}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ goalId }),
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  load(state.date); // reload to show the linked task + updated pool
}

async function pullTodo(taskId) {
  if (collectTasks().length >= MAX_TASKS) return;
  setSaveState("saving");
  try {
    await fetch(`/api/day/pull?date=${state.date}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ taskId }),
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  load(state.date);
}

// Schedule: two input lines per hour (:00 / :30), one focus circle per hour,
// and duration connectors drawn from each filled slot to the next.
function renderSchedule(slots) {
  els.scheduleRows.innerHTML = "";
  const overlay = document.createElement("div");
  overlay.className = "connectors";
  overlay.id = "connectors";
  els.scheduleRows.appendChild(overlay);

  const hours = [];
  const byHour = new Map();
  slots.forEach((slot, i) => {
    const h = Math.floor(slotMin(slot.time) / 60);
    if (!byHour.has(h)) { byHour.set(h, []); hours.push(h); }
    byHour.get(h).push({ slot, i });
  });

  hours.forEach((h) => {
    const block = document.createElement("div");
    block.className = "shour";

    const time = document.createElement("span");
    time.className = "shour-time";
    time.textContent = hourLabel(h);

    const body = document.createElement("div");
    body.className = "shour-body";
    const entries = byHour.get(h);
    entries.forEach(({ slot, i }) => {
      const input = document.createElement("input");
      const isCal = slot.source === "calendar";
      input.className = "sslot" + (slot.label ? " filled" : "") + (isCal ? " cal" : "");
      input.value = slot.label || "";
      input.dataset.idx = i;
      if (isCal) {
        input.dataset.eventid = slot.eventId || "";
        input.title = "Click to keep on your schedule; type to edit";
        // Click anywhere in a calendar block → harden the whole event into the note.
        input.addEventListener("click", () => adoptEvent(slot.eventId, i));
      }
      input.addEventListener("input", () => {
        state.day.schedule[i].label = input.value;
        state.day.schedule[i].source = ""; // editing makes a calendar slot manual
        input.classList.remove("cal");
        input.classList.toggle("filled", input.value.trim() !== "");
        drawConnectors();
      });
      input.addEventListener("change", saveDay);
      attachWikilinkAutocomplete(input); // [[name]] autocomplete inline in schedule entries
      attachInlineLinks(input);          // [[name]] live-preview + click-to-open
      body.appendChild(input);
    });

    const focusCell = document.createElement("div");
    focusCell.className = "shour-focus";
    const lead = entries[0].i;
    const dot = document.createElement("button");
    dot.className = "focus-dot" + (state.day.schedule[lead].focused ? " on" : "");
    dot.title = "Was I focused?";
    dot.addEventListener("click", () => {
      const v = !state.day.schedule[lead].focused;
      state.day.schedule[lead].focused = v;
      dot.classList.toggle("on", v);
      saveDay();
    });
    focusCell.appendChild(dot);

    block.append(time, body, focusCell);
    els.scheduleRows.appendChild(block);
  });

  drawConnectors();
}

function drawConnectors() {
  const overlay = document.getElementById("connectors");
  if (!overlay) return;
  overlay.innerHTML = "";
  const inputs = [...els.scheduleRows.querySelectorAll("input.sslot")];
  const filled = inputs
    .map((el) => ({ el, min: slotMin(state.day.schedule[+el.dataset.idx].time) }))
    .filter((x) => x.el.value.trim() !== "");
  const crect = els.scheduleRows.getBoundingClientRect();
  // Anchor on slot *edges*, not centers: the connector spans only the empty rows
  // between two entries — starting just below the originating text and ending just
  // above the next — so it never overlaps any text.
  const edges = (el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top - crect.top, bottom: r.bottom - crect.top };
  };
  for (let k = 0; k < filled.length - 1; k++) {
    const a = filled[k], b = filled[k + 1];
    const ae = edges(a.el), be = edges(b.el);
    const yStart = ae.bottom;   // dot + line top: just below the originating entry
    const yEnd = be.top - 3;    // arrowhead: just above the next entry (3px breathing room)
    if (yEnd <= yStart) continue; // back-to-back entries: no empty gap to span, skip

    const line = document.createElement("div");
    line.className = "conn-line";
    line.style.top = `${yStart}px`;
    line.style.height = `${Math.max(0, yEnd - yStart)}px`;
    overlay.appendChild(line);

    const dot = document.createElement("span");
    dot.className = "conn-dot";
    dot.style.top = `${yStart}px`;
    overlay.appendChild(dot);

    const label = document.createElement("span");
    label.className = "conn-label";
    // Sit in the gap just under the entry; clamp so short hops don't collide with the next.
    label.style.top = `${Math.min(yStart + 11, (yStart + yEnd) / 2)}px`;
    label.textContent = fmtDur(b.min - a.min);
    overlay.appendChild(label);
  }
}
window.addEventListener("resize", drawConnectors);

// Harden a calendar event into the day: every slot of the event becomes manual
// (source ""), so the lead's title darkens to normal and persists to the .md while
// the soft span bars drop away. The backend then suppresses that event on reload.
function adoptEvent(eventId, idx) {
  const sched = state.day.schedule;
  const members = eventId
    ? sched.map((r, i) => i).filter((i) => sched[i].source === "calendar" && sched[i].eventId === eventId)
    : [idx];
  if (!members.length) return;
  members.forEach((i) => { sched[i].source = ""; }); // lead keeps its label; continuations stay empty
  renderSchedule(sched);
  saveDay();
}

// Exactly three persistent task rows (vv.xyz layout) — hard cap, never a 4th.
// Empty rows are blank slots to fill in or pull a cascade option into.
const MAX_TASKS = 3;
function renderTasks(tasks) {
  els.taskRows.innerHTML = "";
  // A task pulled from a focus goal carries that goal's id as its backlink;
  // render it in the TASKS row that lines up with its 90-/30-day slot above,
  // rather than the next free row. Goal ids are path-like slugs
  // (aion/series-a-15m/<milestone>/<task>), so match a task to the slot whose
  // goal/milestone id is a prefix of the task's id — this still aligns a task
  // after it's pulled and dropped from the focus's own suggestion list.
  const focus = (state.day && state.day.focus) || [];
  const list = (tasks || []).filter((t) => t && (t.text || t.goalId));
  const rows = new Array(MAX_TASKS).fill(null);
  const leftover = [];
  list.forEach((t) => {
    const si = t.taskId ? slotForTaskId(t.taskId, focus) : slotForGoalId(t.goalId, focus);
    if (si >= 0 && si < MAX_TASKS && rows[si] === null) rows[si] = t; // seat at its goal's slot
    else leftover.push(t); // manual tasks, or a slot already taken
  });
  let li = 0; // fill the gaps with the rest, in order
  for (let i = 0; i < MAX_TASKS; i++) {
    if (rows[i] === null && li < leftover.length) rows[i] = leftover[li++];
  }
  for (let i = 0; i < MAX_TASKS; i++) {
    addTaskRow(rows[i] || { text: "", done: false }, i + 1, focus[i]);
  }
}
// slotForGoalId returns the focus slot index a task belongs under: the slot
// whose most-specific id (cascade task → milestone → 90-day goal) is a
// segment-boundary prefix of the task's goal id. -1 when the task isn't linked
// to any current focus slot (a manually-typed task). Slug ids like
// "aion/series-a-15m/<milestone>/<task>" make prefix matching exact.
// slotForTaskId seats a substrate task under the focus slot whose Rock offers
// it (the slot's tasks are that Rock's tethered todos).
function slotForTaskId(id, focus) {
  let best = -1;
  (focus || []).forEach((p, i) => {
    if (best < 0 && p && (p.tasks || []).some((t) => t.taskId === id)) best = i;
  });
  return best;
}

function slotForGoalId(g, focus) {
  if (!g) return -1;
  let best = -1, bestLen = -1;
  (focus || []).forEach((p, i) => {
    if (!p) return;
    const bases = [];
    (p.tasks || []).forEach((t) => { if (t.goalId) bases.push(t.goalId); });
    if (p.milestone && p.milestone.goalId) bases.push(p.milestone.goalId);
    if (p.goalId) bases.push(p.goalId);
    bases.forEach((base) => {
      if ((g === base || g.startsWith(base + "/")) && base.length > bestLen) {
        bestLen = base.length;
        best = i;
      }
    });
  });
  return best;
}

function addTaskRow(task, num, pick) {
  const row = document.createElement("div");
  row.className = "trow";
  if (task.goalId) row.dataset.goalId = task.goalId; // preserve backlink on save
  if (task.taskId) row.dataset.taskId = task.taskId; // todos-board backlink, same contract
  if (task.owner) row.dataset.owner = task.owner;
  const n = document.createElement("span");
  n.className = "num";
  n.textContent = `${num}.`;

  // GATE (goals-orient): an EMPTY row only accepts entry when its focus slot has
  // both a rock (the pick) and a stage (the milestone) — the owner's rule: "no
  // task until both the rock and stage are set". A free-typed task then CAPTURES
  // into goals.md under that stage. Existing tasks (incl. old unlinked ones)
  // render and edit exactly as before.
  const stageId = pick && pick.resolved && pick.goalId && pick.milestone && pick.milestone.goalId
    ? pick.milestone.goalId : "";
  const empty = !task.text && !task.goalId && !task.taskId;
  if (empty && !stageId) {
    const hint = document.createElement("span");
    hint.className = "trow-gate";
    hint.textContent = "set focus + milestone to add tasks";
    row.append(n, hint);
    els.taskRows.appendChild(row);
    return;
  }

  // Middle column: editable text + a hover-shown remove (✕) on filled rows.
  const mid = document.createElement("div");
  mid.className = "ttext-cell";
  const input = document.createElement("input");
  input.className = "ttext" + (task.done ? " done" : "");
  input.value = task.text || "";
  if (empty && stageId) input.placeholder = "pick ＋ or type a task…";
  attachWikilinkAutocomplete(input); // [[name]] autocomplete inline in task entries
  attachInlineLinks(input);          // [[name]] live-preview + click-to-open
  const remove = document.createElement("button");
  remove.className = "task-remove";
  remove.textContent = "✕";
  remove.title = "Remove task";
  remove.tabIndex = -1;
  mid.append(input, remove);

  const cell = document.createElement("div");
  cell.className = "check-cell";
  const check = document.createElement("button");
  check.className = "check" + (task.done ? " on" : "");
  // ✓ when done, ○ when the row has text, ＋ on an empty gated row (click →
  // the per-slot task picker), blank otherwise.
  const sym = () => (input.classList.contains("done") ? "✓"
    : input.value.trim() ? "○"
    : (empty && stageId ? "＋" : ""));
  // Keep the row's filled state (drives the ✕ affordance) and check glyph in sync.
  const refresh = () => {
    row.classList.toggle("filled", input.value.trim() !== "");
    check.classList.toggle("pick", !input.value.trim() && empty && !!stageId);
    check.textContent = sym();
  };
  check.addEventListener("click", () => {
    if (!input.value.trim()) {
      // empty gated row: the box opens the slot's task picker — existing
      // substrate tasks under this rock, or type a new one (goals-orient ask)
      if (empty && stageId) openTaskPicker(pick, stageId);
      return;
    }
    const done = !input.classList.contains("done");
    input.classList.toggle("done", done);
    check.classList.toggle("on", done);
    check.textContent = sym();
    saveDay();
  });
  input.addEventListener("input", refresh);
  input.addEventListener("change", () => {
    const txt = input.value.trim();
    // A row that was empty and has no goal link captures its free-typed text into
    // goals.md under the slot's stage (then reloads, which re-seats it linked).
    if (empty && !row.dataset.goalId && txt && stageId) {
      captureTask(stageId, txt);
      return;
    }
    saveDay();
    syncTasksAndCascade();
  });
  remove.addEventListener("click", () => {
    input.value = "";
    input.classList.remove("done");
    check.classList.remove("on");
    delete row.dataset.goalId; // dropping the task also drops its cascade backlink
    delete row.dataset.taskId;
    delete row.dataset.owner;
    refresh();
    saveDay();
    syncTasksAndCascade(); // frees the slot → its cascade chip reappears
  });
  refresh();
  cell.appendChild(check);
  row.append(n, mid, cell);
  els.taskRows.appendChild(row);
}
// Mirror the live task rows into state.day and re-offer cascade chips, so the
// "From your focus" suggestions enable/disable the moment a slot frees or fills.
function syncTasksAndCascade() {
  if (!state.day) return;
  state.day.tasks = collectTasks();
  renderCascadeTasks(state.day);
}
function collectTasks() {
  return [...els.taskRows.querySelectorAll(".trow")]
    .map((row) => {
      const input = row.querySelector(".ttext");
      if (!input) return { text: "" }; // gated row (no input) — filtered below
      const t = { text: input.value.trim(), done: input.classList.contains("done") };
      if (row.dataset.goalId) t.goalId = row.dataset.goalId;
      if (row.dataset.taskId) t.taskId = row.dataset.taskId;
      if (row.dataset.owner) t.owner = row.dataset.owner;
      return t;
    })
    .filter((t) => t.text.length > 0);
}
