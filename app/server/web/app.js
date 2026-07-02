// Manifest — local daily-planner UI over your Obsidian vault.
// State lives in markdown files; this is a thin editor with autosave.

const state = { date: isoToday(), day: null, cal: null, consoleProfile: "", feedType: "" };

const els = {
  dateLabel: document.getElementById("dateLabel"),
  saveState: document.getElementById("saveState"),
  scheduleRows: document.getElementById("scheduleRows"),
  scheduleRange: document.getElementById("scheduleRange"),
  goalsRows: document.getElementById("goalsRows"),
  goalsRange: document.getElementById("goalsRange"),
  milestonesRows: document.getElementById("milestonesRows"),
  milestonesRange: document.getElementById("milestonesRange"),
  taskRows: document.getElementById("taskRows"),
  prepBanner: document.getElementById("prepBanner"),
  dayView: document.getElementById("dayView"),
  goalsView: document.getElementById("goalsView"),
  calendarView: document.getElementById("calendarView"),
  agentsView: document.getElementById("agentsView"),
  dateNav: document.getElementById("dateNav"),
  goalsNav: document.getElementById("goalsNav"),
  calNav: document.getElementById("calNav"),
  agentsNav: document.getElementById("agentsNav"),
  dayNav: document.getElementById("dayNav"),
  hermesStatus: document.getElementById("hermesStatus"),
  consoleLog: document.getElementById("consoleLog"),
  consoleInput: document.getElementById("consoleInput"),
  consoleSend: document.getElementById("consoleSend"),
  // agents cockpit sub-panels
  ap_console: document.getElementById("ap-console"),
  ap_profiles: document.getElementById("ap-profiles"),
  ap_feed: document.getElementById("ap-feed"),
  ap_jobs: document.getElementById("ap-jobs"),
  ap_approvals: document.getElementById("ap-approvals"),
  apprBadge: document.getElementById("apprBadge"),
  // spirits (excalibur harness) view
  spiritsView: document.getElementById("spiritsView"),
  spiritsNav: document.getElementById("spiritsNav"),
  spiritsStatus: document.getElementById("spiritsStatus"),
  sp_feed: document.getElementById("sp-feed"),
  sp_runs: document.getElementById("sp-runs"),
  spiritFeedFilters: document.getElementById("spiritFeedFilters"),
  spiritFeedList: document.getElementById("spiritFeedList"),
  spiritRunNowBtn: document.getElementById("spiritRunNowBtn"),
  spiritRunsList: document.getElementById("spiritRunsList"),
  spiritRunDetail: document.getElementById("spiritRunDetail"),
  consoleProfileBar: document.getElementById("consoleProfileBar"),
  consoleProfileName: document.getElementById("consoleProfileName"),
  consoleProfileClear: document.getElementById("consoleProfileClear"),
  profileList: document.getElementById("profileList"),
  newProfileBtn: document.getElementById("newProfileBtn"),
  feedFilters: document.getElementById("feedFilters"),
  feedList: document.getElementById("feedList"),
  feedRefreshBtn: document.getElementById("feedRefreshBtn"),
  feedBackfillBtn: document.getElementById("feedBackfillBtn"),
  feedRunBtn: document.getElementById("feedRunBtn"),
  jobsList: document.getElementById("jobsList"),
  sessionsList: document.getElementById("sessionsList"),
  newJobBtn: document.getElementById("newJobBtn"),
  approvalList: document.getElementById("approvalList"),
  apprRunBtn: document.getElementById("apprRunBtn"),
  profileModal: document.getElementById("profileModal"),
  profileBackdrop: document.getElementById("profileBackdrop"),
  profileClose: document.getElementById("profileClose"),
  profileModalTitle: document.getElementById("profileModalTitle"),
  pfName: document.getElementById("pfName"),
  pfModel: document.getElementById("pfModel"),
  pfSchedule: document.getElementById("pfSchedule"),
  pfPerms: document.getElementById("pfPerms"),
  pfTools: document.getElementById("pfTools"),
  pfToolCount: document.getElementById("pfToolCount"),
  pfBrief: document.getElementById("pfBrief"),
  pfSave: document.getElementById("pfSave"),
  pfDelete: document.getElementById("pfDelete"),
  calGrid: document.getElementById("calGrid"),
  calMonthLabel: document.getElementById("calMonthLabel"),
  calConnect: document.getElementById("calConnect"),
  calConnectBtn: document.getElementById("calConnectBtn"),
  calAccounts: document.getElementById("calAccounts"),
  calAccountRows: document.getElementById("calAccountRows"),
  calAddAccount: document.getElementById("calAddAccount"),
  calPrev: document.getElementById("calPrev"),
  calNext: document.getElementById("calNext"),
  addArea: document.getElementById("addArea"),
  goalsAddBtn: document.getElementById("goalsAddBtn"),
  goalsSubnav: document.getElementById("goalsSubnav"),
  goalsSoft: document.getElementById("goalsSoft"),
  gp_goals: document.getElementById("gp-goals"),
  gp_year: document.getElementById("gp-year"),
  gp_review: document.getElementById("gp-review"),
  gp_history: document.getElementById("gp-history"),
  pickerModal: document.getElementById("pickerModal"),
  pickerBackdrop: document.getElementById("pickerBackdrop"),
  pickerClose: document.getElementById("pickerClose"),
  pickerTitle: document.getElementById("pickerTitle"),
  pickerBody: document.getElementById("pickerBody"),
};

// ---- date helpers ----
function isoToday() {
  const d = new Date();
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
function pad(n) { return String(n).padStart(2, "0"); }
function shiftDate(iso, days) {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(y, m - 1, d + days);
  return `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}`;
}
function prettyDate(iso) {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(y, m - 1, d);
  const wd = ["SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"][dt.getDay()];
  const mo = ["JAN","FEB","MAR","APR","MAY","JUN","JUL","AUG","SEP","OCT","NOV","DEC"][m - 1];
  return `${wd} ${mo} ${pad(d)}`;
}

// ---- time helpers (must mirror daily/daily.go) ----
const slotRe = /^(\d{1,2})(?::(\d{2}))?\s*([AaPp])$/;
function slotMin(tok) {
  const m = slotRe.exec((tok || "").trim());
  if (!m) return null;
  let h = +m[1];
  if (h < 1 || h > 12) return null;
  let min = m[2] != null ? +m[2] : 0;
  if (/a/i.test(m[3])) { if (h === 12) h = 0; } else if (h !== 12) h += 12;
  return h * 60 + min;
}
function hourLabel(h24) {
  const suffix = h24 >= 12 ? "P" : "A";
  let h = h24 % 12; if (h === 0) h = 12;
  return `${h}${suffix}`;
}
function fmtDur(min) {
  if (min < 60) return `${min}m`;
  const h = min / 60;
  return (Number.isInteger(h) ? String(h) : h.toFixed(1).replace(/\.0$/, "")) + "h";
}

// ---- save plumbing (debounced per endpoint) ----
const savers = {};
function queueSave(endpoint, payloadFn) {
  setSaveState("saving");
  clearTimeout(savers[endpoint]);
  savers[endpoint] = setTimeout(async () => {
    try {
      await fetch(`/api/${endpoint}?date=${state.date}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payloadFn()),
      });
      setSaveState("saved");
    } catch (e) { setSaveState("error"); }
  }, 500);
}
function setSaveState(s) {
  els.saveState.textContent = s;
  els.saveState.classList.toggle("saving", s === "saving");
}
function saveDay() {
  queueSave("day", () => ({ schedule: scheduleForSave(), tasks: collectTasks() }));
}
// Pristine calendar-sourced slots are not persisted (sent empty) so they never
// become manual text; the live overlay re-applies them on the next load.
function scheduleForSave() {
  return state.day.schedule.map((r) => (r.source === "calendar" ? { ...r, label: "" } : r));
}
// ---- day: load + render ----
async function load(date) {
  state.date = date;
  const today = date === isoToday();
  els.dateLabel.textContent = today ? "TODAY" : prettyDate(date);
  const r = await fetch(`/api/day?date=${date}`);
  state.day = await r.json();
  renderDay();
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
  renderPrep(day);
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

// Open the goal picker for a focus slot: all owner==me, open Rocks by area. The
// picked Rock resolves to its current stage + tasks (goalsadapter).
async function openGoalPicker(slot) {
  const doc = await (await fetch("/api/goals")).json();
  const groups = (doc.areas || [])
    .map((a) => ({
      area: a.name,
      items: (a.rocks || [])
        .filter((g) => !g.checked && g.owner === "me")
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
  const existing = new Set((day.tasks || []).map((t) => t.goalId).filter(Boolean));
  const suggestions = [];
  (day.focus || []).forEach((p) => {
    (p.tasks || []).forEach((t) => {
      if (!existing.has(t.goalId)) suggestions.push({ goalId: t.goalId, text: t.text, goal: p.text });
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
      chip.addEventListener("click", () => pullGoal(s.goalId));
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

// Prep banner: on an unplanned future day, offer the 30-day owner==me pool as
// click-to-add chips. Hidden on planned days and on today/past.
function renderPrep(day) {
  els.prepBanner.innerHTML = "";
  if (!day.unplanned || !(day.pool && day.pool.length)) {
    els.prepBanner.hidden = true;
    return;
  }
  els.prepBanner.hidden = false;
  const head = document.createElement("div");
  head.className = "prep-head";
  head.textContent = `Planning ${prettyDate(day.date)} — pull from your 30-day plate:`;
  const chips = document.createElement("div");
  chips.className = "pool-chips";
  day.pool.forEach((it) => {
    const chip = document.createElement("button");
    chip.className = "pool-chip";
    chip.title = `Add “${it.text}” to ${day.date}`;
    const area = document.createElement("span");
    area.className = "pool-area";
    area.textContent = it.area;
    chip.append(area, document.createTextNode(" " + it.text));
    chip.addEventListener("click", () => pullGoal(it.goalId));
    chips.appendChild(chip);
  });
  els.prepBanner.append(head, chips);
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
  const list = (tasks || []).slice(0, MAX_TASKS);
  for (let i = 0; i < MAX_TASKS; i++) {
    addTaskRow(list[i] || { text: "", done: false }, i + 1);
  }
}
function addTaskRow(task, num) {
  const row = document.createElement("div");
  row.className = "trow";
  if (task.goalId) row.dataset.goalId = task.goalId; // preserve backlink on save
  if (task.owner) row.dataset.owner = task.owner;
  const n = document.createElement("span");
  n.className = "num";
  n.textContent = `${num}.`;

  // Middle column: editable text + a hover-shown remove (✕) on filled rows.
  const mid = document.createElement("div");
  mid.className = "ttext-cell";
  const input = document.createElement("input");
  input.className = "ttext" + (task.done ? " done" : "");
  input.value = task.text || "";
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
  // ✓ when done, ○ when the row has text, blank when empty (matches the reference).
  const sym = () => (input.classList.contains("done") ? "✓" : input.value.trim() ? "○" : "");
  // Keep the row's filled state (drives the ✕ affordance) and check glyph in sync.
  const refresh = () => {
    row.classList.toggle("filled", input.value.trim() !== "");
    check.textContent = sym();
  };
  check.addEventListener("click", () => {
    if (!input.value.trim()) return; // can't complete an empty row
    const done = !input.classList.contains("done");
    input.classList.toggle("done", done);
    check.classList.toggle("on", done);
    check.textContent = sym();
    saveDay();
  });
  input.addEventListener("input", refresh);
  input.addEventListener("change", () => { saveDay(); syncTasksAndCascade(); });
  remove.addEventListener("click", () => {
    input.value = "";
    input.classList.remove("done");
    check.classList.remove("on");
    delete row.dataset.goalId; // dropping the task also drops its cascade backlink
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
      const t = { text: input.value.trim(), done: input.classList.contains("done") };
      if (row.dataset.goalId) t.goalId = row.dataset.goalId;
      if (row.dataset.owner) t.owner = row.dataset.owner;
      return t;
    })
    .filter((t) => t.text.length > 0);
}

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

// ---- goals command center (§2/§3): by-area command view + Year + History ----
let goalsTab = "goals";
let goalsCache = null;   // last /api/goals view (with roll-up)
let archivesCache = null; // last /api/goals/archives

async function loadGoals() {
  try {
    const [doc, arch] = await Promise.all([
      fetch("/api/goals").then((r) => r.json()),
      fetch("/api/goals/archives").then((r) => r.json()).catch(() => ({ quarters: [] })),
    ]);
    goalsCache = doc;
    archivesCache = arch;
  } catch (e) { setSaveState("error"); }
  renderGoalsTab();
}

function setGoalsTab(tab) { goalsTab = tab; renderGoalsTab(); }

function renderGoalsTab() {
  const areas = (goalsCache && goalsCache.areas) || [];
  // Soft-focus guidance: >~5 active Rocks total (never blocks).
  const activeRocks = areas.reduce((n, a) => n + (a.rocks || []).length, 0);
  if (els.goalsSoft) {
    els.goalsSoft.hidden = activeRocks <= 5;
    els.goalsSoft.textContent = `${activeRocks} active Rocks — consider deferring some (aim for ~5).`;
  }
  ["goals", "year", "review", "history"].forEach((t) => { if (els["gp_" + t]) els["gp_" + t].hidden = t !== goalsTab; });
  document.querySelectorAll(".gtab").forEach((b) => b.classList.toggle("active", b.dataset.gtab === goalsTab));
  if (goalsTab === "year") renderYear(areas);
  else if (goalsTab === "review") renderReview(areas);
  else if (goalsTab === "history") renderHistory((archivesCache && archivesCache.quarters) || []);
  else renderCommand(areas);
}

function currentStage(rock) { return (rock.children || []).find((s) => !s.checked) || null; }
function firstOpenTask(stage) { return (stage.children || []).find((t) => !t.checked) || null; }

function renderCommand(areas) {
  const host = els.gp_goals;
  host.innerHTML = "";
  if (!areas.length) { host.appendChild(emptyRow("No areas yet — add one.")); return; }
  areas.forEach((a) => host.appendChild(commandArea(a)));
}

function commandArea(a) {
  const card = el("div", "cmd-area");
  const head = el("div", "cmd-area-head");
  const name = el("input", "area-name");
  name.value = a.name;
  name.addEventListener("change", () => {
    const v = name.value.trim();
    if (v && v !== a.name) goalsApi("PATCH", "/api/areas", { name: a.name, newName: v });
  });
  head.append(name);
  const ns = el("input", "area-ns");
  ns.placeholder = "North Star…";
  ns.value = a.northStar || "";
  ns.addEventListener("change", () => goalsApi("PATCH", "/api/areas", { name: a.name, northStar: ns.value.trim() }));
  head.append(ns);
  card.append(head);

  // 1-year goals with roll-up badges.
  (a.annuals || []).forEach((an) => card.append(annualLine(a, an)));
  card.append(addBtn("+ 1-year goal", () =>
    goalsApi("POST", "/api/goals/item", { area: a.name, parentId: "", section: "annual", text: "New 1-year goal", owner: "me" })));

  const rocks = el("div", "cmd-rocks");
  (a.rocks || []).forEach((r) => rocks.append(rockCard(a, r)));
  rocks.append(addBtn("+ Rock", () =>
    goalsApi("POST", "/api/goals/item", { area: a.name, parentId: "", section: "rock", text: "New Rock", owner: "me" })));
  card.append(rocks);
  return card;
}

function annualLine(area, an) {
  const row = el("div", "cmd-annual");
  row.append(el("span", "cmd-annual-label", "1-YR"));
  row.append(checkBtn(an));
  const text = el("input", "goal-text" + (an.checked ? " done" : ""));
  text.value = an.text;
  text.addEventListener("change", () => {
    const v = text.value.trim();
    if (v && v !== an.text) goalsApi("PATCH", "/api/goals/item", { id: an.id, text: v });
  });
  row.append(text);
  const total = (an.rollupActive || 0) + (an.rollupWon || 0) + (an.rollupLearn || 0);
  row.append(el("span", "cmd-annual-badge", total ? `${total} Rocks · ${an.rollupWon || 0} won` : "no Rocks yet"));
  row.append(delBtn(an));
  return row;
}

function rockCard(area, rock) {
  const card = el("div", "rock-card" + (rock.status ? " st-" + rock.status : ""));
  const top = el("div", "rock-top");
  const title = el("input", "rock-title" + (rock.checked ? " done" : ""));
  title.value = rock.text;
  title.addEventListener("change", () => {
    const v = title.value.trim();
    if (v && v !== rock.text) goalsApi("PATCH", "/api/goals/item", { id: rock.id, text: v });
  });
  top.append(title);
  if (!rock.serves || !(rock.children || []).length) top.append(el("span", "rock-needs", "needs setup"));
  top.append(statusChip(rock));
  if (rock.moved) top.append(el("span", "rock-moved", "moved " + rock.moved.slice(5)));
  const acts = el("span", "rock-acts");
  acts.append(pillLight("Won", () => closeGoal(rock.id, "win")));
  acts.append(pillLight("Drop", () => closeGoal(rock.id, "learn", prompt("Why drop it? (optional)") || "")));
  top.append(acts);
  card.append(top);

  card.append(stageStepper(rock));

  const cur = currentStage(rock);
  const next = cur ? firstOpenTask(cur) : null;
  const na = el("div", "rock-next");
  if (next) { na.append(el("span", "na-label", "NEXT"), checkBtn(next), el("span", "na-text", next.text)); }
  else if (cur) na.append(el("span", "na-muted", "Current stage has no open tasks — add one, or complete the stage."));
  else na.append(el("span", "na-muted", "No stages yet — name the first stage below."));
  card.append(na);

  // Expandable editor for the full stage/task trail (reuses the depth renderer).
  const details = el("details", "rock-details");
  details.append(el("summary", null, "edit stages & tasks"));
  const body = el("div", "rock-edit");
  (rock.children || []).forEach((st) => body.append(goalNode(st, 1)));
  body.append(addBtn("+ stage", () =>
    goalsApi("POST", "/api/goals/item", { parentId: rock.id, text: "New stage", owner: "me" })));
  if (rock.serves) body.append(el("div", "rock-serves-note", "serves " + rock.serves));
  details.append(body);
  card.append(details);
  return card;
}

function statusChip(rock) {
  const sel = document.createElement("select");
  const cur = rock.status || "active";
  sel.className = "rock-chip status status-" + cur;
  ["active", "blocked", "at-risk"].forEach((o) => sel.appendChild(new Option(o, o)));
  sel.value = cur;
  sel.addEventListener("change", () =>
    goalsApi("PATCH", "/api/goals/item", { id: rock.id, status: sel.value === "active" ? "" : sel.value }));
  return sel;
}

function stageStepper(rock) {
  const wrap = el("div", "stepper");
  const stages = rock.children || [];
  if (!stages.length) { wrap.append(el("span", "step-empty", "(no stages)")); return wrap; }
  const curId = (currentStage(rock) || {}).id;
  stages.forEach((s, i) => {
    if (i) wrap.append(el("span", "step-sep", "→"));
    let cls = "step " + (s.checked ? "done" : s.id === curId ? "current" : "future");
    const chip = el("span", cls, (s.checked ? "✓ " : "") + s.text);
    if (s.id === curId) { chip.title = "Complete this stage"; chip.addEventListener("click", () => completeStage(rock, s)); }
    wrap.append(chip);
  });
  return wrap;
}

async function completeStage(rock, stage) {
  setSaveState("saving");
  try {
    await fetch("/api/goals/check", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id: stage.id, checked: true }) });
    const name = (prompt("Stage complete. Name the next stage (blank to skip):") || "").trim();
    if (name) await fetch("/api/goals/item", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ parentId: rock.id, text: name, owner: "me" }) });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  loadGoals();
}

async function closeGoal(id, outcome, note) {
  setSaveState("saving");
  try {
    const r = await fetch("/api/goals/close", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, outcome, note: note || "" }) });
    if (!r.ok) throw new Error(await r.text());
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Close failed: " + (e.message || e)); }
  loadGoals();
}

function pickFrom(title, items, onPick) { openPicker(title, [{ area: "", items }], onPick); }

// addGoalFlow offers Quick capture (title + area) or Full setup (area → annual → stage).
function addGoalFlow() {
  const areas = (goalsCache && goalsCache.areas) || [];
  if (!areas.length) { alert("Add an area first."); return; }
  pickFrom("Add a goal", [
    { id: "quick", text: "Quick capture — title + area" },
    { id: "full", text: "Full setup — annual + first stage" },
  ], (mode) => (mode === "full" ? fullSetup(areas) : quickCapture(areas)));
}

function quickCapture(areas) {
  pickFrom("Quick capture — which area?", areas.map((a) => ({ id: a.name, text: a.name })), (area) => {
    const title = (prompt("Rock title:") || "").trim();
    if (title) goalsApi("POST", "/api/goals/item", { area, parentId: "", section: "rock", text: title, owner: "me" });
  });
}

function fullSetup(areas) {
  pickFrom("Full setup — which area?", areas.map((a) => ({ id: a.name, text: a.name })), (areaName) => {
    const a = areas.find((x) => x.name === areaName);
    const title = (prompt("Rock title:") || "").trim();
    if (!title) return;
    const proceed = (serves) => {
      const stage = (prompt("First stage name (blank to skip):") || "").trim();
      createRockFull(areaName, title, serves, stage);
    };
    const annuals = a.annuals || [];
    if (annuals.length) {
      pickFrom("Which 1-year goal does it serve?",
        [{ id: "", text: "(none — needs setup)" }].concat(annuals.map((an) => ({ id: an.id, text: an.text }))), proceed);
    } else proceed("");
  });
}

async function createRockFull(area, title, serves, stage) {
  setSaveState("saving");
  try {
    const view = await (await fetch("/api/goals/item", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ area, parentId: "", section: "rock", text: title, owner: "me" }) })).json();
    const av = (view.areas || []).find((x) => x.name === area);
    const rock = av && av.rocks[av.rocks.length - 1];
    if (rock) {
      if (serves) await fetch("/api/goals/item", { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id: rock.id, serves }) });
      if (stage) await fetch("/api/goals/item", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ parentId: rock.id, text: stage, owner: "me" }) });
    }
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Setup failed: " + (e.message || e)); }
  loadGoals();
}

function renderYear(areas) {
  const host = els.gp_year;
  host.innerHTML = "";
  let any = false;
  areas.forEach((a) => (a.annuals || []).forEach((an) => {
    any = true;
    const card = el("div", "year-card");
    card.append(el("div", "year-annual", a.name + " · " + an.text));
    card.append(el("span", "cmd-annual-badge", `${an.rollupActive || 0} active · ${an.rollupWon || 0} won · ${an.rollupLearn || 0} learn`));
    (a.rocks || []).filter((r) => r.serves === an.id).forEach((r) =>
      card.append(el("div", "year-rock", "• " + r.text + (r.status ? "  [" + r.status + "]" : ""))));
    host.append(card);
  }));
  if (!any) host.appendChild(emptyRow("No 1-year goals yet — add some in the Command view."));
}

// ---- Quarterly review (§7): Win/Learn/Carry per Rock + retro + next-quarter drafting ----
function renderReview(areas) {
  const host = els.gp_review;
  host.innerHTML = "";
  const q = (areas.flatMap((a) => a.rocks || []).find((r) => r.quarter) || {}).quarter || "";
  const activeRocks = areas.reduce((n, a) => n + (a.rocks || []).length, 0);
  host.append(el("div", "review-head", "Quarterly review" + (q ? " · " + q : "") + " · " + activeRocks + " active Rocks"));
  if (activeRocks > 5) host.append(el("div", "soft-focus", `${activeRocks} active Rocks — consider deferring some (aim for ~5).`));

  areas.forEach((a) => {
    if (!(a.rocks || []).length && !(a.annuals || []).length) return;
    const sec = el("div", "review-area");
    sec.append(el("div", "review-area-name", a.name));
    (a.rocks || []).forEach((r) => {
      const row = el("div", "review-rock");
      const tip = currentStage(r);
      row.append(el("span", "review-rock-title", r.text));
      row.append(el("span", "review-rock-stage", tip ? "@ " + tip.text : "(no stage)"));
      const acts = el("span", "rock-acts");
      acts.append(pillLight("Won", () => closeGoal(r.id, "win")));
      acts.append(pillLight("Learn", () => closeGoal(r.id, "learn", prompt("What did you learn / why drop it? (optional)") || "")));
      acts.append(pillLight("Carry →", () => carryGoal(r.id)));
      row.append(acts);
      sec.append(row);
    });
    (a.annuals || []).forEach((an) =>
      sec.append(addBtn("+ draft Rock for “" + an.text.slice(0, 42) + "”", () => {
        const title = (prompt("New Rock (serves: " + an.text + "):") || "").trim();
        if (title) createRockFull(a.name, title, an.id, "");
      })));
    host.append(sec);
  });

  const retro = el("div", "review-retro");
  retro.append(el("div", "review-area-name", "Retro — Start / Stop / Keep (optional)"));
  const start = retroField("Start doing…"), stop = retroField("Stop doing…"), keep = retroField("Keep doing…");
  retro.append(start.wrap, stop.wrap, keep.wrap);
  retro.append(pill("Save retro", () => saveRetro(start.ta.value, stop.ta.value, keep.ta.value)));
  host.append(retro);
}

function retroField(placeholder) {
  const wrap = el("div", "retro-field");
  const ta = document.createElement("textarea");
  ta.className = "retro-ta";
  ta.placeholder = placeholder;
  ta.rows = 2;
  wrap.append(ta);
  return { wrap, ta };
}

async function carryGoal(id) {
  setSaveState("saving");
  try {
    const r = await fetch("/api/goals/carry", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id }) });
    if (!r.ok) throw new Error(await r.text());
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Carry failed: " + (e.message || e)); }
  loadGoals();
}

async function saveRetro(start, stop, keep) {
  setSaveState("saving");
  try {
    const r = await fetch("/api/goals/retro", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ start, stop, keep }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error("retro save failed");
    setSaveState("saved");
    alert("Retro saved to “goals " + (j.quarter || "") + " review.md”.");
  } catch (e) { setSaveState("error"); alert("Retro save failed."); }
}

function renderHistory(quarters) {
  const host = els.gp_history;
  host.innerHTML = "";
  if (!quarters.length) { host.appendChild(emptyRow("No closed goals yet — Won/dropped Rocks archive here by quarter.")); return; }
  quarters.forEach((q) => {
    const card = el("div", "hist-card");
    const pct = Math.round((q.winRate || 0) * 100);
    card.append(el("div", "hist-head", `${q.quarter} — ${q.wins} won · ${q.learns} learned · ${pct}% win`));
    (q.entries || []).forEach((e) => {
      const row = el("div", "hist-row " + (e.outcome === "win" ? "win" : "learn"));
      row.append(el("span", "hist-outcome", e.outcome === "win" ? "WON" : "LEARN"));
      row.append(el("span", "hist-text", (e.area ? e.area + " · " : "") + e.text));
      if (e.reached) row.append(el("span", "hist-reached", "reached: " + e.reached));
      card.append(row);
    });
    host.append(card);
  });
}

// addBtn is a small "+ …" add button wired to onClick.
function addBtn(label, onClick) {
  const b = el("button", "add-btn add-goal", label);
  b.addEventListener("click", onClick);
  return b;
}

// goalNode renders a Rock (depth 0) and its trail: stages (depth 1) each owning
// tasks (depth 2). The add-child button is "+ stage" under a Rock, "+ task" under a
// stage; tasks are leaves (literal depth rule).
function goalNode(g, depth) {
  const wrap = el("div", "goal-node depth-" + depth);
  wrap.appendChild(goalRow(g, depth));
  const kids = el("div", "goal-children");
  (g.children || []).forEach((c) => kids.appendChild(goalNode(c, depth + 1)));
  if (depth < 2) {
    kids.appendChild(addBtn(depth === 0 ? "+ stage" : "+ task", () =>
      goalsApi("POST", "/api/goals/item", {
        parentId: g.id,
        text: depth === 0 ? "New stage" : "New task",
        owner: depth === 0 ? "me" : "",
      })));
  }
  wrap.appendChild(kids);
  return wrap;
}

function goalRow(g, depth) {
  const row = el("div", "goal-row");
  row.append(checkBtn(g), goalText(g));

  if (depth === 0) { // Rock: quarter / serves / status
    if (g.quarter) row.append(el("span", "rock-chip quarter", g.quarter));
    if (g.serves) row.append(el("span", "rock-chip serves", "↑ " + g.serves));
    const status = document.createElement("select");
    const cur = g.status || "active";
    status.className = "rock-chip status status-" + cur;
    ["active", "blocked", "at-risk"].forEach((o) => status.appendChild(new Option(o, o)));
    status.value = cur;
    status.addEventListener("change", () =>
      goalsApi("PATCH", "/api/goals/item", { id: g.id, status: status.value === "active" ? "" : status.value }));
    row.append(status);
  }
  if (depth < 2) row.append(ownerSelect(g)); // Rock or stage carries an owner

  row.append(delBtn(g));
  return row;
}

// ----- shared goal-row cells -----
function checkBtn(g) {
  const b = el("button", "check" + (g.checked ? " on" : ""), g.checked ? "✓" : "○");
  b.addEventListener("click", () => goalsApi("POST", "/api/goals/check", { id: g.id, checked: !g.checked }));
  return b;
}
function goalText(g) {
  const t = el("input", "goal-text" + (g.checked ? " done" : ""));
  t.value = g.text;
  t.addEventListener("change", () => {
    const v = t.value.trim();
    if (v && v !== g.text) goalsApi("PATCH", "/api/goals/item", { id: g.id, text: v });
  });
  return t;
}
function ownerSelect(g) {
  const owner = document.createElement("select");
  owner.className = "owner-chip owner-" + (g.owner === "me" ? "me" : g.owner === "team" ? "team" : "other");
  ["me", "team"].forEach((o) => owner.appendChild(new Option(o, o)));
  if (g.owner !== "me" && g.owner !== "team") owner.appendChild(new Option(g.owner, g.owner));
  owner.value = g.owner;
  owner.addEventListener("change", () => goalsApi("PATCH", "/api/goals/item", { id: g.id, owner: owner.value }));
  return owner;
}
function delBtn(g) {
  const del = el("button", "icon-btn goal-del", "✕");
  del.title = "Delete";
  del.addEventListener("click", () => goalsApi("DELETE", "/api/goals/item", { id: g.id }));
  return del;
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
if (els.goalsAddBtn) els.goalsAddBtn.addEventListener("click", addGoalFlow);
if (els.goalsSubnav) els.goalsSubnav.addEventListener("click", (e) => {
  const b = e.target.closest(".gtab");
  if (b) setGoalsTab(b.dataset.gtab);
});

els.pickerClose.addEventListener("click", closePicker);
els.pickerBackdrop.addEventListener("click", closePicker);
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !els.pickerModal.hidden) closePicker();
});

// ================= Calendar (month view) =================
const MONTHS = ["January","February","March","April","May","June","July","August","September","October","November","December"];

function ensureCalState() {
  if (!state.cal) {
    const d = new Date();
    state.cal = { year: d.getFullYear(), month: d.getMonth() };
  }
  return state.cal;
}

// monthGridDays returns the 42 cells (6 weeks, Monday-first) covering the month,
// including the leading/trailing days from adjacent months so the grid is always
// complete and the columns stay uniform.
function monthGridDays(year, month) {
  const offset = (new Date(year, month, 1).getDay() + 6) % 7; // Monday = 0
  const cells = [];
  for (let i = 0; i < 42; i++) {
    const dt = new Date(year, month, 1 - offset + i);
    const iso = `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}`;
    cells.push({ iso, day: dt.getDate(), inMonth: dt.getMonth() === month });
  }
  return cells;
}

async function loadCalendar() {
  const { year, month } = ensureCalState();
  els.calMonthLabel.textContent = `${MONTHS[month]} ${year}`.toUpperCase();
  let status = { accounts: [], hasCreds: false };
  try { status = await (await fetch("/api/calendar/status")).json(); } catch (e) {}
  const accounts = status.accounts || [];
  renderCalAccounts(accounts, !!status.hasCreds);

  const cells = monthGridDays(year, month);
  let events = [];
  if (accounts.length) {
    try {
      const r = await (await fetch(`/api/calendar/events?start=${cells[0].iso}&end=${cells[41].iso}`)).json();
      events = r.events || [];
    } catch (e) {}
  }
  renderMonth(cells, events);
}

// Show the accounts list (with per-account Disconnect) when ≥1 account is
// connected; otherwise the connect prompt (adapted for missing credentials).
function renderCalAccounts(accounts, hasCreds) {
  const has = accounts.length > 0;
  els.calAccounts.hidden = !has;
  els.calConnect.hidden = has;
  if (!has) {
    els.calConnectBtn.hidden = !hasCreds;
    els.calConnect.querySelector("p").textContent = hasCreds
      ? "Connect a Google account (read-only) to see your events and auto-fill your schedule."
      : "Add google_credentials.json to ~/.config/manifest/ to connect Google Calendar.";
    return;
  }
  els.calAccountRows.innerHTML = "";
  accounts.forEach((email) => {
    const row = document.createElement("div");
    row.className = "cal-account";
    const name = document.createElement("span");
    name.className = "cal-account-email";
    name.textContent = email;
    const dc = document.createElement("button");
    dc.className = "cal-disconnect";
    dc.textContent = "Disconnect";
    dc.addEventListener("click", () => disconnectAccount(email));
    row.append(name, dc);
    els.calAccountRows.appendChild(row);
  });
}

async function disconnectAccount(email) {
  setSaveState("saving");
  try {
    await fetch("/api/calendar/disconnect", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ account: email }),
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  loadCalendar();
}

const MAX_PER_DAY = 4;

function renderMonth(cells, events) {
  const byDay = new Map();
  events.forEach((e) => {
    const day = (e.start || "").slice(0, 10);
    if (!byDay.has(day)) byDay.set(day, []);
    byDay.get(day).push(e);
  });
  // all-day events first, then timed in ascending start order
  byDay.forEach((list) => list.sort((a, b) => {
    if (a.allDay !== b.allDay) return a.allDay ? -1 : 1;
    return (a.start || "").localeCompare(b.start || "");
  }));

  els.calGrid.innerHTML = "";
  const today = isoToday();
  cells.forEach(({ iso, day, inMonth }) => {
    const cell = document.createElement("div");
    cell.className = "cal-cell" + (inMonth ? "" : " adjacent") + (iso === today ? " today" : "");
    const num = document.createElement("div");
    num.className = "cal-day-num";
    num.textContent = day;
    cell.appendChild(num);

    const evs = byDay.get(iso) || [];
    // a single overflow item is shown rather than a "1 more" line
    const cap = evs.length === MAX_PER_DAY + 1 ? evs.length : MAX_PER_DAY;
    evs.slice(0, cap).forEach((e) => cell.appendChild(eventEl(e)));
    if (evs.length > cap) {
      const more = document.createElement("div");
      more.className = "cal-more";
      more.textContent = `${evs.length - cap} more`;
      cell.appendChild(more);
    }
    cell.addEventListener("click", () => { state.date = iso; location.hash = "#/"; });
    els.calGrid.appendChild(cell);
  });
}

function eventEl(e) {
  const title = e.title || "(busy)";
  if (e.allDay) {
    const bar = document.createElement("div");
    bar.className = "cal-ev allday";
    bar.textContent = title;
    bar.title = title;
    return bar;
  }
  const row = document.createElement("div");
  row.className = "cal-ev";
  row.title = `${formatTime(e.start)} ${title}`.trim();
  const dot = document.createElement("span");
  dot.className = "cal-ev-dot";
  const time = document.createElement("span");
  time.className = "cal-ev-time";
  time.textContent = formatTime(e.start);
  const label = document.createElement("span");
  label.className = "cal-ev-title";
  label.textContent = title;
  row.append(dot, time, label);
  return row;
}

// formatTime reads the clock straight off an RFC3339 string ("…T08:00:00-05:00"
// -> "8:00am"), so the displayed time matches the event's own timezone (already
// normalized server-side) without browser-timezone drift.
function formatTime(rfc3339) {
  const m = /T(\d{2}):(\d{2})/.exec(rfc3339 || "");
  if (!m) return "";
  let h = +m[1];
  const suffix = h < 12 ? "am" : "pm";
  h = h % 12;
  if (h === 0) h = 12;
  return `${h}:${m[2]}${suffix}`;
}

function shiftCalMonth(delta) {
  const c = ensureCalState();
  let m = c.month + delta, y = c.year;
  if (m < 0) { m = 11; y--; }
  else if (m > 11) { m = 0; y++; }
  state.cal = { year: y, month: m };
  loadCalendar();
}

// Connect one Google account; safe to call repeatedly (Google shows the account
// chooser each time so you can pick a different account).
async function connectCalendar(btn) {
  const label = btn ? btn.textContent : "";
  if (btn) btn.textContent = "Connecting… (check your browser)";
  try {
    await fetch("/api/calendar/connect", { method: "POST" });
  } catch (e) {}
  if (btn) btn.textContent = label;
  loadCalendar();
}

els.calConnectBtn.addEventListener("click", () => connectCalendar(els.calConnectBtn));
els.calAddAccount.addEventListener("click", () => connectCalendar(els.calAddAccount));
els.calPrev.addEventListener("click", () => shiftCalMonth(-1));
els.calNext.addEventListener("click", () => shiftCalMonth(1));

// ================= Agents panel =================
// ---- Agents cockpit: small DOM helpers ----
function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}
function pill(text, onclick) { const b = el("button", "pill", text); b.addEventListener("click", onclick); return b; }
function pillLight(text, onclick) { const b = el("button", "pill light", text); b.addEventListener("click", onclick); return b; }
function emptyRow(text) { return el("div", "ro-row empty", text); }
function splitList(s) { return (s || "").split(",").map((x) => x.trim()).filter(Boolean); }
function linkEl(text, href) { const a = el("a", null, text); a.href = href; a.target = "_blank"; a.rel = "noopener"; return a; }
function fmtWhen(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d)) return String(iso).slice(0, 16).replace("T", " ");
  const now = new Date();
  if (Math.abs(d - now) < 86400000 && d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  }
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

// ---- Agents cockpit: sub-tab routing ----
const AGENT_TABS = ["console", "profiles", "feed", "jobs", "approvals"];
function showAgents() {
  const tab = agentTabFromHash();
  AGENT_TABS.forEach((t) => { els["ap_" + t].hidden = t !== tab; });
  document.querySelectorAll("#agentsTabs .atab").forEach((a) => a.classList.toggle("active", a.dataset.tab === tab));
  loadHermes(); // status chip is shown on every sub-tab
  if (tab === "profiles") loadProfiles();
  else if (tab === "feed") loadFeed();
  else if (tab === "jobs") loadJobs();
  else if (tab === "approvals") loadApprovals();
  refreshApprovalBadge();
}
function agentTabFromHash() {
  const t = (location.hash.split("/")[2] || "console");
  return AGENT_TABS.includes(t) ? t : "console";
}

// ---- Profiles (Step 2) ----
let toolsetsCache = null;
async function ensureToolsets() {
  if (toolsetsCache) return toolsetsCache;
  try { toolsetsCache = (await (await fetch("/api/hermes/toolsets")).json()).data || []; }
  catch (e) { toolsetsCache = []; }
  return toolsetsCache;
}
async function loadProfiles() {
  let list = [];
  try { list = (await (await fetch("/api/agents/profiles")).json()).data || []; } catch (e) {}
  els.profileList.innerHTML = "";
  if (!list.length) { els.profileList.appendChild(emptyRow("No profiles yet. Add one — it's a small markdown file outside your vault.")); return; }
  list.forEach((p) => els.profileList.appendChild(profileCard(p)));
}
function profileCard(p) {
  const card = el("div", "profile-card");
  const head = el("div", "profile-head");
  head.append(el("span", "profile-name", p.name), el("span", "profile-tier", p.model || ""));
  const brief = el("div", "profile-brief", (p.brief || "").split("\n")[0].slice(0, 150));
  const sched = p.schedule && p.schedule !== "none" ? p.schedule : "on-demand";
  const meta = el("div", "profile-meta", `${(p.tools || []).length} tools · ${(p.permissions || []).join(", ") || "—"} · ${sched}`);
  const actions = el("div", "profile-actions");
  actions.append(pillLight("Use in console", () => useProfile(p.name)), pillLight("Edit", () => openProfileEditor(p)));
  card.append(head, brief, meta, actions);
  return card;
}
function useProfile(name) {
  state.consoleProfile = name;
  updateProfileBar();
  location.hash = "#/agents";
}
function updateProfileBar() {
  const on = !!state.consoleProfile;
  els.consoleProfileBar.hidden = !on;
  if (on) els.consoleProfileName.textContent = state.consoleProfile;
}
async function openProfileEditor(p) {
  p = p || { name: "", model: "cheap", tools: [], permissions: [], schedule: "none", brief: "" };
  els.profileModalTitle.textContent = p.name ? "Edit profile" : "New profile";
  els.pfName.value = p.name || "";
  els.pfName.disabled = !!p.name; // name is the id — don't rename in place
  els.pfModel.value = p.model || "cheap";
  els.pfSchedule.value = p.schedule || "none";
  els.pfPerms.value = (p.permissions || []).join(", ");
  els.pfBrief.value = p.brief || "";
  els.pfDelete.hidden = !p.name;
  els.pfDelete.onclick = () => deleteProfile(p.name);
  const sets = await ensureToolsets();
  const selected = new Set(p.tools || []);
  els.pfTools.innerHTML = "";
  const countLabel = () => { els.pfToolCount.textContent = selected.size + " selected"; };
  sets.forEach((ts) => {
    const b = el("button", "tool-chip" + (selected.has(ts.name) ? " on" : ""), ts.name);
    b.title = ts.description || ts.label || "";
    b.onclick = () => { selected.has(ts.name) ? selected.delete(ts.name) : selected.add(ts.name); b.classList.toggle("on"); countLabel(); };
    els.pfTools.appendChild(b);
  });
  countLabel();
  els.pfSave.onclick = () => saveProfile({
    name: els.pfName.value.trim(), model: els.pfModel.value,
    schedule: els.pfSchedule.value.trim(), permissions: splitList(els.pfPerms.value),
    tools: [...selected], brief: els.pfBrief.value,
  });
  els.profileModal.hidden = false;
}
async function saveProfile(p) {
  if (!p.name) { els.pfName.focus(); return; }
  setSaveState("saving");
  try { await fetch("/api/agents/profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  els.profileModal.hidden = true;
  loadProfiles();
}
async function deleteProfile(name) {
  if (!confirm(`Delete profile "${name}"?`)) return;
  try { await fetch("/api/agents/profiles/" + encodeURIComponent(name), { method: "DELETE" }); } catch (e) {}
  els.profileModal.hidden = true;
  loadProfiles();
}

// ---- Feed (Step 3) ----
let feedCache = [];
async function loadFeed() {
  try { feedCache = (await (await fetch("/api/feed")).json()).data || []; } catch (e) { feedCache = []; }
  renderFeedFilters();
  renderFeed();
}
function renderFeedFilters() {
  const host = els.feedFilters; host.innerHTML = "";
  const types = [...new Set(feedCache.map((i) => i.type))];
  const mk = (label, val) => {
    const b = el("button", "filter-chip" + ((state.feedType || "") === val ? " on" : ""), label);
    b.onclick = () => { state.feedType = val; renderFeedFilters(); renderFeed(); };
    return b;
  };
  host.appendChild(mk("all", ""));
  types.forEach((t) => host.appendChild(mk(t, t)));
}
function renderFeed() {
  const host = els.feedList; host.innerHTML = "";
  const items = feedCache.filter((i) => !state.feedType || i.type === state.feedType);
  if (!items.length) { host.appendChild(emptyRow("No items yet — hit Refresh to run domain-scout, or Backfill to pull from scheduled cron runs.")); return; }
  items.forEach((it) => host.appendChild(feedCard(it)));
}
function feedCard(it) {
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-" + it.type, it.type));
  const title = it.link ? linkEl(it.title, it.link) : el("span", null, it.title);
  title.classList.add("feed-title");
  top.append(title);
  if (it.confidence) top.append(el("span", "conf conf-" + it.confidence, it.confidence));
  card.append(top);
  if (it.why) card.append(el("div", "feed-why", it.why));
  const metaBits = [it.source, it.domain, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ");
  if (metaBits) card.append(el("div", "feed-meta", metaBits));
  if (it.type === "artifact" && it.body) { const b = el("pre", "feed-body"); b.textContent = it.body; card.append(b); }
  if (it.vaultNote) card.append(el("div", "feed-saved", "✓ saved to " + it.vaultNote));
  const actions = el("div", "feed-actions");
  actions.append(
    pillLight("Keep", () => feedAction(it.id, { status: "kept" })),
    pillLight("Discard", () => feedAction(it.id, { status: "discarded" })),
    pillLight("Snooze 7d", () => feedAction(it.id, { status: "snoozed", days: 7 })),
  );
  if (!it.vaultNote) actions.append(pillLight("Save to vault", () => saveToVault(it.id)));
  card.append(actions);
  return card;
}
async function feedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/feed/${id}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed();
}
async function saveToVault(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/feed/${id}/save-to-vault`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "save failed");
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Save to vault failed: " + e.message); }
  loadFeed();
}
const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

// pollRun waits for an async agent run (feed scan / options-scout / ea-coordinator draft)
// to finish, polling /api/agents/runs/{id} every ~3s. Agent runs can take many minutes and
// dozens of tool calls, so the server backgrounds them; this is how the UI tracks one.
async function pollRun(runId) {
  for (let i = 0; i < 400; i++) { // ~20 min ceiling at 3s
    await sleep(3000);
    let st;
    try { st = await (await fetch("/api/agents/runs/" + encodeURIComponent(runId))).json(); }
    catch (e) { continue; } // transient — keep polling
    if (st && (st.status === "done" || st.status === "error")) return st;
  }
  return { status: "error", error: "timed out waiting for the run" };
}

// startAgentRun POSTs to an async run endpoint, then polls to completion. It surfaces the
// REAL error: failures return a plain-text body (not JSON), so we read text() and throw it
// verbatim instead of a generic "failed". Returns the terminal run state.
async function startAgentRun(url, body) {
  const r = await fetch(url, {
    method: "POST",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const raw = await r.text();
  if (!r.ok) throw new Error(raw.trim() || `HTTP ${r.status}`);
  let started = {};
  try { started = JSON.parse(raw); } catch (e) {}
  if (!started.runId) throw new Error("server did not start a run");
  const st = await pollRun(started.runId);
  if (st.status === "error") throw new Error(st.error || "run failed");
  return st;
}

async function feedRefresh() {
  const btn = els.feedRefreshBtn;
  btn.disabled = true; btn.textContent = "Scanning…";
  setSaveState("saving");
  try {
    await startAgentRun("/api/feed/refresh");
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Refresh failed: " + (e.message || e)); }
  btn.disabled = false; btn.textContent = "↻ Refresh";
  loadFeed();
}
async function feedBackfill() {
  els.feedBackfillBtn.disabled = true;
  setSaveState("saving");
  try { await fetch("/api/feed/backfill", { method: "POST" }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  els.feedBackfillBtn.disabled = false;
  loadFeed();
}
// askScout runs an on-demand scout (e.g. options-scout) with a free-form request and
// materializes its output into the feed (§5.3). Read-only scouts only — propose-only
// profiles (ea-coordinator) draft to Approvals, not the feed.
async function askScout() {
  let profs = [];
  try { profs = (await (await fetch("/api/agents/profiles")).json()).data || []; } catch (e) {}
  const scouts = profs.filter((p) => !(p.permissions || []).includes("propose-only"));
  if (!scouts.length) { alert("No scout profiles available. Add a read-only profile first."); return; }
  const groups = [{ area: "Ask which scout?", items: scouts.map((p) => ({ id: p.name, text: p.name })) }];
  openPicker("Ask a scout to research a request", groups, async (name) => {
    const request = prompt(`Request for "${name}"  (e.g. "buy a 3D printer under $2k — find 5 options")`);
    if (!request || !request.trim()) return;
    const btn = els.feedRunBtn;
    if (btn) { btn.disabled = true; btn.textContent = "Scanning…"; }
    setSaveState("saving");
    try {
      await startAgentRun("/api/feed/run", { profile: name, request: request.trim() });
      setSaveState("saved");
    } catch (e) { setSaveState("error"); alert("Scout run failed: " + (e.message || e)); }
    if (btn) { btn.disabled = false; btn.textContent = "Ask a scout"; }
    loadFeed();
  });
}

// ---- Jobs + observability (Step 4) ----
async function loadJobs() {
  let list = [];
  try { list = (await (await fetch("/api/jobs")).json()).data || []; } catch (e) {}
  els.jobsList.innerHTML = "";
  if (!list.length) els.jobsList.appendChild(emptyRow("No crons scheduled."));
  else list.forEach((j) => els.jobsList.appendChild(jobRow(j)));
  loadSessions();
}
function jobRow(j) {
  const row = el("div", "job-row" + (j.enabled ? "" : " paused"));
  const st = j.last_status === "ok" ? "ok" : (j.last_status ? "err" : "");
  row.append(el("span", "status-dot " + st));
  const main = el("div", "job-main");
  main.append(el("div", "job-name", j.name));
  const sched = (j.schedule_display || (j.schedule && j.schedule.expr) || "—");
  const last = j.last_run_at ? `last ${fmtWhen(j.last_run_at)}${j.last_status ? " · " + j.last_status : ""}` : "never run";
  const next = j.next_run_at && j.enabled ? ` · next ${fmtWhen(j.next_run_at)}` : "";
  main.append(el("div", "job-sub", `${sched}   ·   ${last}${next}`));
  if (j.last_error) main.append(el("div", "job-err", j.last_error));
  row.append(main);
  const actions = el("div", "job-actions");
  actions.append(
    pillLight(j.enabled ? "Pause" : "Resume", () => jobUpdate(j.id, { enabled: !j.enabled })),
    pillLight("Delete", () => { if (confirm(`Delete cron "${j.name}"?`)) jobDelete(j.id); }),
  );
  row.append(actions);
  return row;
}
async function jobUpdate(id, patch) {
  setSaveState("saving");
  try { await fetch("/api/jobs/" + id, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(patch) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadJobs();
}
async function jobDelete(id) {
  setSaveState("saving");
  try { await fetch("/api/jobs/" + id, { method: "DELETE" }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadJobs();
}
async function newJob() {
  let profs = [];
  try { profs = (await (await fetch("/api/agents/profiles")).json()).data || []; } catch (e) {}
  if (!profs.length) { alert("Create a profile first."); return; }
  const groups = [{ area: "Schedule which profile?", items: profs.map((p) => ({ id: p.name, text: p.name })) }];
  openPicker("Schedule a profile as a cron", groups, async (name) => {
    const p = profs.find((x) => x.name === name);
    const expr = prompt(`Cron expression for "${name}"`, (p && p.schedule && p.schedule !== "none") ? p.schedule : "0 7 * * *");
    if (!expr) return;
    setSaveState("saving");
    try {
      await fetch("/api/jobs", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name, prompt: p ? p.brief : "", schedule: expr, skills: (p && p.tools) || [] }),
      });
      setSaveState("saved");
    } catch (e) { setSaveState("error"); }
    loadJobs();
  });
}
async function loadSessions() {
  let list = [];
  try { list = (await (await fetch("/api/agents/sessions")).json()).data || []; } catch (e) {}
  els.sessionsList.innerHTML = "";
  if (!list.length) { els.sessionsList.appendChild(emptyRow("No recent runs.")); return; }
  list.slice(0, 12).forEach((sx) => {
    const row = el("div", "session-row");
    row.append(el("span", "sess-src src-" + sx.source, sx.source));
    row.append(el("span", "sess-title", sx.title || "(untitled)"));
    const tok = (sx.input_tokens || 0) + (sx.output_tokens || 0);
    row.append(el("span", "sess-meta", `${sx.message_count || 0} msgs · ${tok.toLocaleString()} tok`));
    row.append(el("span", "sess-when", fmtWhen(new Date((sx.started_at || 0) * 1000).toISOString())));
    els.sessionsList.appendChild(row);
  });
}

// ---- Approvals (Step 5, record-only) ----
async function loadApprovals() {
  let d = { pending: [], counts: {} };
  try { d = await (await fetch("/api/agents/approvals")).json(); } catch (e) {}
  const list = d.pending || [];
  els.approvalList.innerHTML = "";
  if (!list.length) { els.approvalList.appendChild(emptyRow("No pending approvals. ea-coordinator's drafts land here for your review — nothing sends without you.")); }
  else list.forEach((a) => els.approvalList.appendChild(approvalCard(a)));
  setApprovalBadge((d.counts && d.counts.pending) || 0);
}
function approvalCard(a) {
  const card = el("div", "approval-card");
  const head = el("div", "appr-head");
  head.append(el("span", "appr-action", a.action), el("span", "appr-agent", a.agent || ""));
  card.append(head);
  if (a.body) { const b = el("pre", "appr-body"); b.textContent = a.body; card.append(b); }
  const actions = el("div", "appr-actions");
  actions.append(pill("Confirm", () => approvalAct(a.id, "confirm")), pillLight("Reject", () => approvalAct(a.id, "reject")));
  card.append(actions);
  return card;
}
async function approvalAct(id, kind) {
  setSaveState("saving");
  const body = kind === "reject" ? { reason: "rejected from dashboard" } : {};
  try { await fetch(`/api/agents/approvals/${id}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadApprovals();
}
function setApprovalBadge(n) {
  if (!els.apprBadge) return;
  els.apprBadge.hidden = !n;
  els.apprBadge.textContent = n || "";
}
// draftApproval runs ea-coordinator with a task; its DRAFTED actions land in the pending
// queue (§5.5). Draft-only — confirm/reject stay record-only and nothing ever sends.
async function draftApproval() {
  const request = prompt('Task for ea-coordinator  (e.g. "draft a reply to Lee proposing 3 times next week")');
  if (!request || !request.trim()) return;
  const btn = els.apprRunBtn;
  if (btn) { btn.disabled = true; btn.textContent = "Drafting…"; }
  setSaveState("saving");
  try {
    await startAgentRun("/api/agents/approvals/run", { request: request.trim() });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Draft failed: " + (e.message || e)); }
  if (btn) { btn.disabled = false; btn.textContent = "+ Draft with ea-coordinator"; }
  loadApprovals();
}
async function refreshApprovalBadge() {
  try { const d = await (await fetch("/api/agents/approvals")).json(); setApprovalBadge((d.counts && d.counts.pending) || 0); } catch (e) {}
}

// ---- Hermes live console (MVP1) ----
// The browser talks only to same-origin /api/hermes/*; the Go backend holds the
// API key and proxies to Hermes over Tailscale. Chat is streamed via SSE.
const consoleMsgs = []; // OpenAI-style {role, content}, the running conversation.
let consoleStreaming = false;

async function loadHermes() {
  // Status chip: configured? reachable? model + skill count.
  let st = { configured: false };
  try { st = await (await fetch("/api/hermes/status")).json(); } catch (e) {}
  if (!st.configured) {
    els.hermesStatus.textContent = "NOT CONFIGURED";
    els.hermesStatus.title = st.hint || "";
    els.hermesStatus.classList.remove("ok");
    if (!els.consoleLog.childElementCount) {
      consoleHint(st.hint || "Hermes isn't configured yet. Add your API key and restart.");
    }
    setComposerEnabled(false);
    return;
  }
  setComposerEnabled(true);
  let skills = [];
  try { skills = (await (await fetch("/api/hermes/skills")).json()).data || []; } catch (e) {}
  const dot = st.reachable ? "●" : "○";
  els.hermesStatus.classList.toggle("ok", !!st.reachable);
  els.hermesStatus.textContent = `${dot} ${st.model || "hermes-agent"} · ${skills.length} SKILLS`;
  els.hermesStatus.title = st.reachable ? `Connected to ${st.baseURL}` : `Configured but unreachable: ${st.baseURL}`;
}

function setComposerEnabled(on) {
  els.consoleInput.disabled = !on;
  els.consoleSend.disabled = !on;
}

// Append a chat bubble and return its text node holder (for streaming updates).
function appendBubble(role) {
  const row = document.createElement("div");
  row.className = `cmsg ${role}`;
  const who = document.createElement("span");
  who.className = "cmsg-role";
  who.textContent = role === "user" ? "you" : "hermes";
  const body = document.createElement("div");
  body.className = "cmsg-body";
  row.append(who, body);
  els.consoleLog.appendChild(row);
  els.consoleLog.scrollTop = els.consoleLog.scrollHeight;
  return body;
}

function consoleHint(text) {
  const e = document.createElement("div");
  e.className = "console-hint";
  e.textContent = text;
  els.consoleLog.appendChild(e);
}

async function sendConsole() {
  if (consoleStreaming) return;
  const text = els.consoleInput.value.trim();
  if (!text) return;
  els.consoleInput.value = "";
  autoGrow(els.consoleInput);

  consoleMsgs.push({ role: "user", content: text });
  appendBubble("user").textContent = text;

  // The assistant bubble holds a tool-activity strip (§5.1) above the streamed text,
  // so tool progress and text can update independently without clobbering each other.
  const bubble = appendBubble("assistant");
  bubble.classList.add("streaming");
  const toolsEl = el("div", "cmsg-tools");
  const textEl = el("span", "cmsg-text");
  bubble.append(toolsEl, textEl);
  const toolRows = {}; // toolCallId -> row element
  consoleStreaming = true;
  setSaveState("saving");
  els.consoleSend.disabled = true;

  let acc = "";
  try {
    const res = await fetch("/api/hermes/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messages: consoleMsgs, profile: state.consoleProfile || "" }),
    });
    if (!res.ok || !res.body) {
      const msg = await res.text().catch(() => "");
      throw new Error(msg || `HTTP ${res.status}`);
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let done = false;
    while (!done) {
      const { value, done: rdone } = await reader.read();
      done = rdone;
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      // Process complete lines; keep any partial line in the buffer.
      let nl;
      while ((nl = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, nl).trim();
        buffer = buffer.slice(nl + 1);
        if (!line.startsWith("data:")) continue; // ignore blanks / comments / event: lines
        const payload = line.slice(5).trim();
        if (payload === "[DONE]") { done = true; break; }
        let chunk;
        try { chunk = JSON.parse(payload); } catch (e) { continue; }
        if (chunk.error) throw new Error(chunk.error);
        // Hermes tool-progress event (custom SSE, no choices):
        // {tool, emoji, label, toolCallId, status: "running"|"completed"}.
        if (chunk.tool && chunk.status) {
          renderToolProgress(toolsEl, toolRows, chunk);
          els.consoleLog.scrollTop = els.consoleLog.scrollHeight;
          continue;
        }
        const choice = (chunk.choices || [])[0] || {};
        const piece = (choice.delta && choice.delta.content) || "";
        if (piece) {
          acc += piece;
          textEl.textContent = acc;
          els.consoleLog.scrollTop = els.consoleLog.scrollHeight;
        }
      }
    }
    if (acc) consoleMsgs.push({ role: "assistant", content: acc });
    else if (!toolsEl.children.length) textEl.textContent = "(no content)";
    setSaveState("saved");
  } catch (e) {
    bubble.classList.add("error");
    textEl.textContent = acc ? acc + `\n\n[stream error: ${e.message}]` : `[error: ${e.message}]`;
    setSaveState("error");
  } finally {
    bubble.classList.remove("streaming");
    consoleStreaming = false;
    els.consoleSend.disabled = false;
    els.consoleInput.focus();
  }
}

// renderToolProgress renders/updates one inline tool-activity row (§5.1). Rows are keyed
// by toolCallId so a "completed" event (which omits emoji/label) updates the same row the
// "running" event created. Only fields present in the event overwrite prior values.
function renderToolProgress(container, rows, ev) {
  const id = ev.toolCallId || ev.tool || String(container.children.length);
  let row = rows[id];
  if (!row) {
    row = el("div", "cmsg-tool");
    row.append(el("span", "ct-emoji", ev.emoji || "🔧"), el("span", "ct-name"), el("span", "ct-label"));
    container.appendChild(row);
    rows[id] = row;
  }
  if (ev.emoji) row.children[0].textContent = ev.emoji;
  if (ev.tool) row.children[1].textContent = ev.tool;
  if (ev.label) row.children[2].textContent = ev.label;
  const done = ev.status === "completed";
  row.classList.toggle("running", !done);
  row.classList.toggle("done", done);
}

function autoGrow(ta) {
  ta.style.height = "auto";
  ta.style.height = Math.min(ta.scrollHeight, 160) + "px";
}

if (els.consoleSend) {
  els.consoleSend.addEventListener("click", sendConsole);
  els.consoleInput.addEventListener("input", () => autoGrow(els.consoleInput));
  els.consoleInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); sendConsole(); }
  });
}

// ---- agents cockpit wiring ----
if (els.newProfileBtn) els.newProfileBtn.addEventListener("click", () => openProfileEditor(null));
if (els.feedRefreshBtn) els.feedRefreshBtn.addEventListener("click", feedRefresh);
if (els.feedBackfillBtn) els.feedBackfillBtn.addEventListener("click", feedBackfill);
if (els.feedRunBtn) els.feedRunBtn.addEventListener("click", askScout);
if (els.newJobBtn) els.newJobBtn.addEventListener("click", newJob);
if (els.apprRunBtn) els.apprRunBtn.addEventListener("click", draftApproval);
if (els.profileClose) els.profileClose.addEventListener("click", () => { els.profileModal.hidden = true; });
if (els.profileBackdrop) els.profileBackdrop.addEventListener("click", () => { els.profileModal.hidden = true; });
if (els.consoleProfileClear) els.consoleProfileClear.addEventListener("click", () => { state.consoleProfile = ""; updateProfileBar(); });

// ---- SPIRITS: the excalibur harness console ----
// The dashboard reads the sibling excalibur tree (feed, run reports, prompts)
// and records the user's keep/discard/snooze; the ENGINE owns execution — the
// only write toward it is a spooled run-now request it picks up on its own.
const SPIRIT_TABS = ["feed", "runs"];
let spiritStatusCache = null;
let spiritFeedCache = [];
let spiritRunsCache = [];

function showSpirits() {
  const tab = spiritTabFromHash();
  SPIRIT_TABS.forEach((t) => { els["sp_" + t].hidden = t !== tab; });
  document.querySelectorAll("#spiritsTabs .atab").forEach((a) => a.classList.toggle("active", a.dataset.tab === tab));
  loadSpiritsStatus(); // engine-alive chip shows on every sub-tab
  if (tab === "feed") loadSpiritFeed();
  else if (tab === "runs") loadSpiritRuns();
}
function spiritTabFromHash() {
  const t = (location.hash.split("/")[2] || "feed");
  return SPIRIT_TABS.includes(t) ? t : "feed";
}

async function loadSpiritsStatus() {
  try { spiritStatusCache = await (await fetch("/api/spirits/status")).json(); }
  catch (e) { spiritStatusCache = null; }
  const st = spiritStatusCache;
  if (!st || !st.enabled) { els.spiritsStatus.textContent = "not configured — set excaliburPath"; return; }
  const names = Object.keys(st.spirits || {});
  els.spiritsStatus.textContent = (st.engineAlive ? "engine alive" : "engine down") +
    (names.length ? " · " + names.join(", ") : "");
  els.spiritsStatus.style.color = st.engineAlive ? "" : "#b91c1c";
}

// ---- spirit feed (artifacts/feed/) ----
async function loadSpiritFeed() {
  try { spiritFeedCache = (await (await fetch("/api/spirits/feed")).json()).data || []; } catch (e) { spiritFeedCache = []; }
  renderSpiritFeedFilters();
  renderSpiritFeed();
}
function renderSpiritFeedFilters() {
  const host = els.spiritFeedFilters; host.innerHTML = "";
  const types = [...new Set(spiritFeedCache.map((i) => i.type))];
  const mk = (label, val) => {
    const b = el("button", "filter-chip" + ((state.spiritFeedType || "") === val ? " on" : ""), label);
    b.onclick = () => { state.spiritFeedType = val; renderSpiritFeedFilters(); renderSpiritFeed(); };
    return b;
  };
  host.appendChild(mk("all", ""));
  types.forEach((t) => host.appendChild(mk(t, t)));
}
function renderSpiritFeed() {
  const host = els.spiritFeedList; host.innerHTML = "";
  const items = spiritFeedCache.filter((i) => !state.spiritFeedType || i.type === state.spiritFeedType);
  if (!items.length) { host.appendChild(emptyRow("No items yet — hit Run now, or wait for the daily ritual.")); return; }
  items.forEach((it) => host.appendChild(spiritFeedCard(it)));
}
function spiritFeedCard(it) {
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-" + it.type, it.type));
  const title = it.link ? linkEl(it.title, it.link) : el("span", null, it.title);
  title.classList.add("feed-title");
  top.append(title);
  if (it.confidence) top.append(el("span", "conf conf-" + it.confidence, it.confidence));
  card.append(top);
  if (it.why) card.append(el("div", "feed-why", it.why));
  const metaBits = [it.agent, it.source, it.domain, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ");
  if (metaBits) card.append(el("div", "feed-meta", metaBits));
  if (it.body) { const b = el("pre", "feed-body"); b.textContent = it.body; card.append(b); }
  const actions = el("div", "feed-actions");
  actions.append(
    pillLight("Keep", () => spiritFeedAction(it.id, { status: "kept" })),
    pillLight("Discard", () => spiritFeedAction(it.id, { status: "discarded" })),
    pillLight("Snooze 7d", () => spiritFeedAction(it.id, { status: "snoozed", days: 7 })),
  );
  card.append(actions);
  return card;
}
async function spiritFeedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/spirits/feed/${encodeURIComponent(id)}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadSpiritFeed();
}

// ---- run now (spooled request; engine picks it up within ~5s) ----
async function spiritRunNow() {
  const st = spiritStatusCache || {};
  const spirits = st.spirits || {};
  const spirit = Object.keys(spirits)[0];
  const ritual = spirit ? (spirits[spirit] || [])[0] : null;
  if (!spirit || !ritual) { alert("No spirit/ritual found in the excalibur tree."); return; }
  const btn = els.spiritRunNowBtn;
  btn.disabled = true; btn.textContent = "Requested ✓";
  try {
    const r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual }) });
    if (!r.ok) throw new Error(await r.text());
    watchForNewRun();
  } catch (e) {
    alert("Run request failed: " + (e.message || e));
    btn.disabled = false; btn.textContent = "▶ Run now";
  }
}
// Poll the runs list until a new report lands (runs take a couple of minutes),
// then refresh whichever sub-tab is open. Light: 5s cadence, ~15 min ceiling.
async function watchForNewRun() {
  const before = spiritRunsCache.length ? spiritRunsCache[0].id : (await fetchSpiritRuns())[0]?.id;
  const btn = els.spiritRunNowBtn;
  for (let i = 0; i < 180; i++) {
    await sleep(5000);
    if (!location.hash.startsWith("#/spirits")) break; // stop polling off-tab
    const runs = await fetchSpiritRuns();
    if (runs.length && runs[0].id !== before) {
      spiritRunsCache = runs;
      if (spiritTabFromHash() === "runs") renderSpiritRuns(); else loadSpiritFeed();
      loadSpiritsStatus();
      break;
    }
  }
  btn.disabled = false; btn.textContent = "▶ Run now";
}
async function fetchSpiritRuns() {
  try { return (await (await fetch("/api/spirits/runs")).json()).data || []; } catch (e) { return []; }
}

// ---- run reports (artifacts/runs/) ----
async function loadSpiritRuns() {
  spiritRunsCache = await fetchSpiritRuns();
  renderSpiritRuns();
}
function renderSpiritRuns() {
  const host = els.spiritRunsList; host.innerHTML = "";
  els.spiritRunDetail.hidden = true;
  if (!spiritRunsCache.length) { host.appendChild(emptyRow("No runs yet — the ritual writes a report here every time it fires.")); return; }
  spiritRunsCache.forEach((r) => host.appendChild(spiritRunCard(r)));
}
function spiritRunCard(r) {
  const card = el("div", "run-card");
  const top = el("div", "run-top");
  top.append(el("span", "run-outcome oc-" + (r.outcome || "").replace(/[^a-z-]/g, ""), r.outcome || "?"));
  top.append(el("span", "run-title", `${r.spirit} / ${r.ritual}`));
  top.append(el("span", "run-when", fmtWhen(r.started)));
  card.append(top);
  const pct = r.ceilingUsd > 0 ? Math.min(100, Math.round((r.spentUsd / r.ceilingUsd) * 100)) : 0;
  const bar = el("div", "charge-bar");
  const fill = el("div", "charge-fill" + (pct >= 100 ? " over" : ""));
  fill.style.width = pct + "%";
  bar.appendChild(fill);
  const row = el("div", "charge-row");
  row.append(bar, el("span", "charge-label", `$${r.spentUsd.toFixed(4)} / $${r.ceilingUsd.toFixed(2)}`));
  card.append(row);
  card.append(el("div", "feed-meta", `${r.steps} steps · ${r.itemsWritten} items · ${r.portal} (${r.model})`));
  card.onclick = () => openSpiritRun(r.id);
  return card;
}
async function openSpiritRun(id) {
  let run;
  try { run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(id))).json(); }
  catch (e) { return; }
  const host = els.spiritRunDetail; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "run-detail-head");
  head.append(el("span", "run-title", id));
  const promptBtn = pillLight("Show assembled prompt", () => toggleSpiritPrompts(id, promptBtn));
  const closeBtn = pillLight("✕ Close", () => { host.hidden = true; });
  head.append(promptBtn, closeBtn);
  host.append(head);
  const body = el("pre", "run-report");
  body.textContent = run.body || "";
  host.append(body);
  const prompts = el("div", "run-prompts"); prompts.id = "runPrompts-" + id; prompts.hidden = true;
  host.append(prompts);
  host.scrollIntoView({ behavior: "smooth", block: "start" });
}
// The §6.5 affordance: the EXACT model input per turn, preserved verbatim.
async function toggleSpiritPrompts(id, btn) {
  const box = document.getElementById("runPrompts-" + id);
  if (!box) return;
  if (!box.hidden) { box.hidden = true; btn.textContent = "Show assembled prompt"; return; }
  if (!box.childElementCount) {
    let turns = [];
    try { turns = (await (await fetch("/api/spirits/runs/" + encodeURIComponent(id) + "/prompt")).json()).data || []; }
    catch (e) {}
    if (!turns.length) { box.appendChild(emptyRow("No preserved prompts found for this run.")); }
    turns.forEach((t) => {
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — SYSTEM`));
      const s = el("pre", "run-report prompt"); s.textContent = t.system; box.appendChild(s);
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — USER`));
      const u = el("pre", "run-report prompt"); u.textContent = t.user; box.appendChild(u);
    });
  }
  box.hidden = false; btn.textContent = "Hide assembled prompt";
}

if (els.spiritRunNowBtn) els.spiritRunNowBtn.addEventListener("click", spiritRunNow);

// ---- router ----
function route() {
  const h = location.hash;
  const goals = h === "#/goals";
  const cal = h === "#/calendar";
  const ag = h === "#/agents" || h.startsWith("#/agents/");
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const day = !goals && !cal && !ag && !sp;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.calendarView.hidden = !cal;
  els.agentsView.hidden = !ag;
  els.spiritsView.hidden = !sp;
  els.dateNav.hidden = !day;
  els.goalsNav.hidden = !day;
  els.calNav.hidden = !day;
  els.agentsNav.hidden = !day;
  els.spiritsNav.hidden = !day;
  els.dayNav.hidden = day;
  if (goals) loadGoals();
  else if (cal) loadCalendar();
  else if (ag) showAgents(); // Hermes cockpit: console / profiles / feed / jobs / approvals
  else if (sp) showSpirits(); // excalibur harness: feed / runs
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
