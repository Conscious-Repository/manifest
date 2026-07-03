// Manifest — local daily-planner UI over your Obsidian vault.
// State lives in markdown files; this is a thin editor with autosave.

const state = { date: isoToday(), day: null, cal: null, spiritFeedType: "" };

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
  dateNav: document.getElementById("dateNav"),
  goalsNav: document.getElementById("goalsNav"),
  calNav: document.getElementById("calNav"),
  dayNav: document.getElementById("dayNav"),
  // contacts (people layer over the vault index)
  contactsView: document.getElementById("contactsView"),
  contactsNav: document.getElementById("contactsNav"),
  contactsListPane: document.getElementById("contactsListPane"),
  contactPagePane: document.getElementById("contactPagePane"),
  contactList: document.getElementById("contactList"),
  contactTriage: document.getElementById("contactTriage"),
  contactSearch: document.getElementById("contactSearch"),
  contactColdToggle: document.getElementById("contactColdToggle"),
  contactAddBtn: document.getElementById("contactAddBtn"),
  contactBackBtn: document.getElementById("contactBackBtn"),
  contactPage: document.getElementById("contactPage"),
  contactPageSaved: document.getElementById("contactPageSaved"),
  // universal note view
  noteView: document.getElementById("noteView"),
  noteTitle: document.getElementById("noteTitle"),
  noteBackBtn: document.getElementById("noteBackBtn"),
  noteObsidian: document.getElementById("noteObsidian"),
  noteRawToggle: document.getElementById("noteRawToggle"),
  noteSaveBtn: document.getElementById("noteSaveBtn"),
  noteSaved: document.getElementById("noteSaved"),
  noteRendered: document.getElementById("noteRendered"),
  noteRaw: document.getElementById("noteRaw"),
  noteBacklinks: document.getElementById("noteBacklinks"),
  // quick-lookup command bar
  cmdbar: document.getElementById("cmdbar"),
  cmdbarBackdrop: document.getElementById("cmdbarBackdrop"),
  cmdbarInput: document.getElementById("cmdbarInput"),
  cmdbarResults: document.getElementById("cmdbarResults"),
  cmdbarCard: document.getElementById("cmdbarCard"),
  // spirits (excalibur harness) view
  spiritsView: document.getElementById("spiritsView"),
  spiritsNav: document.getElementById("spiritsNav"),
  spiritsStatus: document.getElementById("spiritsStatus"),
  sp_feed: document.getElementById("sp-feed"),
  sp_runs: document.getElementById("sp-runs"),
  spiritFeedFilters: document.getElementById("spiritFeedFilters"),
  spiritFeedList: document.getElementById("spiritFeedList"),
  spiritRunNowBtn: document.getElementById("spiritRunNowBtn"),
  spiritAskBtn: document.getElementById("spiritAskBtn"),
  spiritRunsList: document.getElementById("spiritRunsList"),
  spiritRunDetail: document.getElementById("spiritRunDetail"),
  sp_approvals: document.getElementById("sp-approvals"),
  spiritApprovalList: document.getElementById("spiritApprovalList"),
  spiritApprBadge: document.getElementById("spiritApprBadge"),
  sp_rituals: document.getElementById("sp-rituals"),
  spiritRitualBoard: document.getElementById("spiritRitualBoard"),
  spiritNewSpirit: document.getElementById("spiritNewSpirit"),
  spiritEditChargebook: document.getElementById("spiritEditChargebook"),
  spiritEditor: document.getElementById("spiritEditor"),
  spiritEditorTabs: document.getElementById("spiritEditorTabs"),
  spiritEditorDirty: document.getElementById("spiritEditorDirty"),
  spiritEditorSave: document.getElementById("spiritEditorSave"),
  spiritEditorClose: document.getElementById("spiritEditorClose"),
  spiritEditorLint: document.getElementById("spiritEditorLint"),
  spiritEditorArea: document.getElementById("spiritEditorArea"),
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
  areasRows: document.getElementById("areasRows"),
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

// ---- areas & goals: single column, M1 layout over the ladder model ----

async function loadGoals() {
  try {
    const doc = await (await fetch("/api/goals")).json();
    renderAreas(doc.areas || []);
  } catch (e) { setSaveState("error"); }
}

function renderAreas(areas) {
  els.areasRows.innerHTML = "";
  if (!areas.length) { els.areasRows.appendChild(emptyRow("No areas yet — add one.")); return; }
  areas.forEach((area) => els.areasRows.appendChild(areaCard(area)));
}

function areaCard(area) {
  const card = el("div", "area-card");

  const head = el("div", "area-head");
  const name = el("input", "area-name");
  name.value = area.name;
  name.addEventListener("change", () => {
    const v = name.value.trim();
    if (v && v !== area.name) goalsApi("PATCH", "/api/areas", { name: area.name, newName: v });
  });
  const del = el("button", "icon-btn area-del", "✕");
  del.title = "Delete area";
  del.addEventListener("click", () => {
    if (confirm(`Delete area “${area.name}” and its goals?`))
      goalsApi("DELETE", "/api/areas", { name: area.name });
  });
  head.append(name, del);

  const ns = el("input", "area-ns");
  ns.placeholder = "North Star…";
  ns.value = area.northStar || "";
  ns.addEventListener("change", () => goalsApi("PATCH", "/api/areas", { name: area.name, northStar: ns.value.trim() }));
  card.append(head, ns);

  const annual = el("div", "horizon");
  annual.appendChild(el("div", "horizon-label", "1-YEAR"));
  (area.annuals || []).forEach((an) => annual.appendChild(annualNode(an)));
  annual.appendChild(addBtn("+ Add 1-year goal", () =>
    goalsApi("POST", "/api/goals/item", { area: area.name, parentId: "", section: "annual", text: "New 1-year goal", owner: "me" })));
  card.appendChild(annual);

  const sec = el("div", "horizon");
  sec.appendChild(el("div", "horizon-label", "ROCK → STAGE → TASK"));
  (area.rocks || []).forEach((g) => sec.appendChild(goalNode(g, 0)));
  sec.appendChild(addBtn("+ Add rock", () =>
    goalsApi("POST", "/api/goals/item", { area: area.name, parentId: "", section: "rock", text: "New rock", owner: "me" })));
  card.appendChild(sec);
  return card;
}

// annualNode: one goal row per 1-year goal — no children, no rollups.
function annualNode(g) {
  const wrap = el("div", "goal-node depth-0");
  const row = el("div", "goal-row");
  row.append(checkBtn(g), goalText(g), ownerSelect(g), delBtn(g));
  wrap.appendChild(row);
  return wrap;
}

// closeGoal moves a Rock to the quarter archive file via the close API.
async function closeGoal(id, outcome, note) {
  setSaveState("saving");
  try {
    const r = await fetch("/api/goals/close", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, outcome, note: note || "" }) });
    if (!r.ok) throw new Error(await r.text());
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Archive failed: " + (e.message || e)); }
  loadGoals();
}

// addBtn is a small "+ …" add button wired to onClick. Child adds inside the
// cascade pass "add-child" for the compact inline style.
function addBtn(label, onClick, cls) {
  const b = el("button", "add-btn " + (cls || "add-goal"), label);
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
      }), "add-child"));
  }
  wrap.appendChild(kids);
  return wrap;
}

function goalRow(g, depth) {
  const row = el("div", "goal-row");
  row.append(depth === 0 ? rockCheckBtn(g) : checkBtn(g), goalText(g));
  if (depth < 2) row.append(ownerSelect(g)); // Rock or stage carries an owner
  if (depth === 0) row.append(archiveBtn(g));
  row.append(delBtn(g));
  return row;
}

// Checking a Rock completes it: confirm, then close it out — the server records
// the outcome and moves it to the quarter archive file. Unchecking stays a plain
// uncheck (a Rock checked by hand in markdown but not yet closed).
function rockCheckBtn(g) {
  const b = el("button", "check" + (g.checked ? " on" : ""), g.checked ? "✓" : "○");
  b.addEventListener("click", () => {
    if (g.checked) return goalsApi("POST", "/api/goals/check", { id: g.id, checked: false });
    if (confirm(`Archive “${g.text}”?`)) closeGoal(g.id, "win", "");
  });
  return b;
}

// archive: close a Rock without completing it (outcome:: learn). The prompt
// doubles as the confirm — cancel aborts, OK archives; the note is optional.
function archiveBtn(g) {
  const b = el("button", "goal-archive", "archive");
  b.title = "Archive without completing";
  b.addEventListener("click", () => {
    const note = prompt(`Archive “${g.text}”? Optional note:`);
    if (note !== null) closeGoal(g.id, "learn", note.trim());
  });
  return b;
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

// ---- SPIRITS: the excalibur harness console ----
// The dashboard reads the sibling excalibur tree (feed, run reports, prompts)
// and records the user's keep/discard/snooze; the ENGINE owns execution — the
// only write toward it is a spooled run-now request it picks up on its own.
const SPIRIT_TABS = ["feed", "runs", "rituals", "approvals"];
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
  else if (tab === "rituals") loadSpiritRituals();
  else if (tab === "approvals") loadSpiritApprovals();
  refreshSpiritApprovalBadge();
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
  const pinned = it.type === "digest" && it.status === "new";
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : "") + (it.type === "digest" ? " digest" : "") + (pinned ? " pinned" : ""));
  const top = el("div", "feed-top");
  if (pinned) top.append(el("span", "pin-chip", "📌 pinned"));
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
  if (it.vaultNote) card.append(el("div", "feed-saved", "✓ saved to " + it.vaultNote));
  const actions = el("div", "feed-actions");
  actions.append(
    pillLight("Keep", () => spiritFeedAction(it.id, { status: "kept" })),
    pillLight("Discard", () => spiritFeedAction(it.id, { status: "discarded" })),
    pillLight("Snooze 7d", () => spiritFeedAction(it.id, { status: "snoozed", days: 7 })),
  );
  if (!it.vaultNote) actions.append(pillLight("Save to vault", () => spiritSaveToVault(it.id)));
  card.append(actions);
  return card;
}
async function spiritFeedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/spirits/feed/${encodeURIComponent(id)}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadSpiritFeed();
}
async function spiritSaveToVault(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/spirits/feed/${encodeURIComponent(id)}/save-to-vault`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "save failed");
    setSaveState("saved");
  } catch (e) { setSaveState("error"); alert("Save to vault failed: " + e.message); }
  loadSpiritFeed();
}

// ---- run now / ask a scout (spooled request; engine picks it up within ~5s) ----
// spiritPick opens the spirit/ritual picker (one area per spirit, its rituals
// as items) and calls onPick("spirit","ritual"). askRitual, when given, is
// picked automatically if present so "Ask a scout" lands on options-scout's
// research ritual without a needless second tap.
function spiritPick(onPick) {
  const spirits = (spiritStatusCache || {}).spirits || {};
  const groups = Object.keys(spirits).sort().map((sp) => ({
    area: sp,
    items: (spirits[sp] || []).map((rit) => ({ id: sp + "/" + rit, text: rit })),
  })).filter((g) => g.items.length);
  if (!groups.length) { alert("No spirit/ritual found in the excalibur tree."); return; }
  openPicker("Run a ritual now", groups, (id) => {
    const [sp, rit] = id.split("/");
    onPick(sp, rit);
  }, "No rituals found.");
}
async function spiritSpool(spirit, ritual, request) {
  const btn = els.spiritRunNowBtn;
  btn.disabled = true; btn.textContent = "Requested ✓";
  try {
    const r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual, request: request || "" }) });
    if (!r.ok) throw new Error(await r.text());
    watchForNewRun();
  } catch (e) {
    alert("Run request failed: " + (e.message || e));
    btn.disabled = false; btn.textContent = "▶ Run now";
  }
}
function spiritRunNow() {
  spiritPick((sp, rit) => spiritSpool(sp, rit, ""));
}
// spiritAskScout: pick a spirit/ritual, then prompt for a free-form request
// (options-scout/research: "buy X under $Y — find 5 options"). The request
// rides the spool file into the run's prompt.
function spiritAskScout() {
  spiritPick((sp, rit) => {
    const request = prompt(`Request for ${sp}/${rit}  (e.g. "buy a mechanical keyboard under $200 — find 5 options")`);
    if (request && request.trim()) spiritSpool(sp, rit, request.trim());
  });
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
  if (r.request) card.append(el("div", "feed-why", "“" + r.request + "”"));
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

// ---- spirit approvals (artifacts/approvals/ — the ONE inbox) ----
// Spirits file proposals via the write_approval cast; Confirm/Reject only
// RECORD the decision (a folder move on the excalibur tree). Nothing sends.
async function loadSpiritApprovals() {
  let d = { pending: [], counts: {} };
  try { d = await (await fetch("/api/spirits/approvals")).json(); } catch (e) {}
  const host = els.spiritApprovalList; host.innerHTML = "";
  const pending = d.pending || [];
  setSpiritApprovalBadge((d.counts && d.counts.pending) || 0);
  if (!pending.length) { host.appendChild(emptyRow("Nothing pending — warden findings and future EA proposals land here.")); return; }
  pending.forEach((a) => host.appendChild(spiritApprovalCard(a)));
}
function spiritApprovalCard(a) {
  const card = el("div", "approval-card");
  const head = el("div", "appr-head");
  head.append(el("span", "appr-action", a.action), el("span", "appr-agent", a.agent || ""));
  card.append(head);
  if (a.created) card.append(el("div", "feed-meta", fmtWhen(a.created)));

  const actionable = !!a.applyPath;
  // For an actionable proposal the ````proposed payload is rendered as a diff
  // below, so strip it from the human-facing evidence body.
  const bodyText = actionable ? stripProposedFence(a.body) : a.body;
  if (bodyText && bodyText.trim()) { const b = el("pre", "appr-body"); b.textContent = bodyText.trim(); card.append(b); }

  let blocked = false, blockMsg = "";
  if (actionable) {
    card.classList.add("actionable");
    const chip = el("div", "appr-apply");
    chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
    card.append(chip);

    if (!a.allowed) {
      blocked = true;
      blockMsg = "apply-path is outside the allow-list (spirits/*/cornerstone.md, spirits/*/rituals/*.md, chargebook.md) — Confirm is disabled.";
    } else if (/\/cornerstone\.md$/.test(a.applyPath) && frontmatterOf(a.current || "") !== frontmatterOf(a.proposed || "")) {
      // client-side mirror of the server's cornerstone-frontmatter guard
      blocked = true;
      blockMsg = "proposed content changes the cornerstone frontmatter — Confirm will refuse (behavior prose only).";
    }

    card.append(el("div", "appr-diff-label", "Proposed change  ·  current → proposed"));
    card.append(renderLineDiff(a.current || "", a.proposed || ""));
  }
  if (blocked && blockMsg) card.append(el("div", "appr-blocked", "⚠ " + blockMsg));

  const actions = el("div", "appr-actions");
  const confirmBtn = pill(actionable ? "Confirm & apply" : "Confirm", () => spiritApprovalAct(a.id, "confirm"));
  if (blocked) { confirmBtn.disabled = true; confirmBtn.classList.add("disabled"); }
  actions.append(confirmBtn, pillLight("Reject", () => spiritApprovalAct(a.id, "reject")));
  card.append(actions);
  return card;
}

// stripProposedFence removes the ````proposed … ```` block from an approval body
// (it is shown as a diff instead). Handles 3+ backtick fences like the server.
function stripProposedFence(body) {
  if (!body) return body || "";
  const lines = body.split("\n"), out = [];
  let skipping = false, fence = 0;
  for (const line of lines) {
    const m = line.match(/^(`{3,})/);
    if (!skipping) {
      if (m && line.slice(m[1].length).trim() === "proposed") { skipping = true; fence = m[1].length; continue; }
      out.push(line);
    } else if (m && m[1].length >= fence && line.trim() === m[1]) {
      skipping = false;
    }
  }
  return out.join("\n").trim();
}

// frontmatterOf returns the raw text between the leading `---` fences (mirrors
// the server's rawFrontmatter), for the client-side cornerstone guard.
function frontmatterOf(text) {
  if (!text.startsWith("---\n")) return "";
  const idx = text.indexOf("\n---");
  return idx < 0 ? "" : text.slice(4, idx);
}

// renderLineDiff builds a compact LCS line diff (full-file replacement) as a
// scrollable block of +/−/context rows.
function renderLineDiff(oldText, newText) {
  const a = oldText.split("\n"), b = newText.split("\n");
  const n = a.length, m = b.length;
  const dp = Array.from({ length: n + 1 }, () => new Int32Array(m + 1));
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--)
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const wrap = el("div", "appr-diff");
  let i = 0, j = 0, changed = false;
  const push = (kind, text) => {
    const row = el("div", "diff-line diff-" + kind);
    row.append(el("span", "diff-gutter", kind === "add" ? "+" : kind === "del" ? "−" : " "));
    row.append(el("span", "diff-text", text === "" ? " " : text));
    wrap.append(row);
    if (kind !== "ctx") changed = true;
  };
  while (i < n && j < m) {
    if (a[i] === b[j]) { push("ctx", a[i]); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { push("del", a[i]); i++; }
    else { push("add", b[j]); j++; }
  }
  while (i < n) push("del", a[i++]);
  while (j < m) push("add", b[j++]);
  if (!changed) wrap.append(el("div", "diff-line diff-ctx", "(no textual change)"));
  return wrap;
}
async function spiritApprovalAct(id, kind) {
  let body = {};
  if (kind === "reject") {
    const reason = prompt("Reason (optional — recorded on the proposal; for warden findings this becomes an accepted exception):") || "";
    body = { reason: reason.trim() || "rejected from dashboard" };
  }
  setSaveState("saving");
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(id)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadSpiritApprovals();
}
function setSpiritApprovalBadge(n) {
  if (!els.spiritApprBadge) return;
  els.spiritApprBadge.hidden = !n;
  els.spiritApprBadge.textContent = n || "";
}
async function refreshSpiritApprovalBadge() {
  try { const d = await (await fetch("/api/spirits/approvals")).json(); setSpiritApprovalBadge((d.counts && d.counts.pending) || 0); } catch (e) {}
}

if (els.spiritRunNowBtn) els.spiritRunNowBtn.addEventListener("click", spiritRunNow);
if (els.spiritAskBtn) els.spiritAskBtn.addEventListener("click", spiritAskScout);

// ---- RITUALS board + in-app markdown editing ----
// The board reads every ritual (next-fire, last outcome, ceiling, validity);
// clicking a row opens the raw markdown editor. Edits round-trip to the
// excalibur tree via /api/spirits/file (allow-listed); the engine hot-reloads.
async function loadSpiritRituals() {
  let rows = [];
  try { rows = (await (await fetch("/api/spirits/rituals")).json()).data || []; } catch (e) {}
  renderSpiritRituals(rows);
}
function renderSpiritRituals(rows) {
  const host = els.spiritRitualBoard; host.innerHTML = "";
  if (!rows.length) { host.appendChild(emptyRow("No rituals yet — add a spirit, then a ritual.")); return; }
  // group by spirit
  const bySpirit = {};
  rows.forEach((r) => { (bySpirit[r.spirit] ||= []).push(r); });
  Object.keys(bySpirit).sort().forEach((sp) => {
    const head = el("div", "ritual-spirit-head");
    const name = el("button", "ritual-spirit-name", sp);
    name.title = "Edit " + sp + "'s identity + cornerstone";
    name.onclick = () => openSpiritEditor(sp);
    const addBtn = pillLight("+ ritual", () => newRitual(sp));
    head.append(name, addBtn);
    host.append(head);
    bySpirit[sp].forEach((r) => host.append(ritualRow(r)));
  });
}
function ritualRow(r) {
  const row = el("div", "ritual-row" + (r.valid ? "" : " invalid"));
  row.append(el("span", "ritual-name", r.ritual));
  // cadence: human + raw
  const cad = el("span", "ritual-cadence");
  cad.append(el("span", "cad-human", r.cadenceHuman || "—"));
  if (r.cadence && r.cadenceHuman !== r.cadence) cad.append(el("span", "cad-raw", r.cadence));
  row.append(cad);
  // next fire — headline
  const next = el("span", "ritual-next");
  if (!r.valid) next.textContent = "—";
  else if (r.nextFire) { next.textContent = fmtWhen(r.nextFire); next.append(el("span", "next-rel", relFuture(r.nextFire))); }
  else next.textContent = "—";
  row.append(next);
  // last outcome chip → run report
  const oc = el("span", "ritual-outcome");
  if (!r.valid) {
    const chip = el("span", "run-outcome oc-invalid", "invalid");
    chip.title = r.error || "invalid frontmatter";
    oc.append(chip);
  } else if (r.lastOutcome) {
    const chip = el("span", "run-outcome oc-" + r.lastOutcome.replace(/[^a-z-]/g, ""), r.lastOutcome);
    if (r.lastRunId) { chip.classList.add("linky"); chip.onclick = (e) => { e.stopPropagation(); location.hash = "#/spirits/runs"; setTimeout(() => openSpiritRun(r.lastRunId), 150); }; }
    oc.append(chip);
  } else {
    oc.append(el("span", "run-outcome oc-never", "never run"));
  }
  row.append(oc);
  // ceiling
  const ceil = el("span", "ritual-ceiling" + (r.ceilingDefault ? " muted" : ""), "$" + Number(r.ceilingUsd).toFixed(2));
  ceil.title = r.ceilingDefault ? "chargebook default" : "ritual charge_usd";
  row.append(ceil);
  if (!r.valid && r.error) row.append(el("div", "ritual-error", r.error));
  row.onclick = () => openEditor([r.path]);
  return row;
}
// relFuture: "in 9h" / "in 3d" / "now"
function relFuture(iso) {
  const d = new Date(iso), ms = d - new Date();
  if (isNaN(d)) return "";
  if (ms <= 0) return " · due";
  const m = Math.round(ms / 60000);
  if (m < 60) return " · in " + m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return " · in " + h + "h";
  return " · in " + Math.round(h / 24) + "d";
}

// ---- markdown editor drawer (rituals / identity / cornerstone / chargebook) ----
let editorState = null; // { files:[{path,loaded,content}], active }
function openSpiritEditor(sp) { openEditor([`spirits/${sp}/identity.md`, `spirits/${sp}/cornerstone.md`], 1); }
async function openEditor(paths, active = 0) {
  editorState = { files: paths.map((p) => ({ path: p, loaded: null })), active };
  els.spiritEditor.hidden = false;
  await selectEditorFile(active);
  els.spiritEditor.scrollIntoView({ behavior: "smooth", block: "nearest" });
}
async function selectEditorFile(i) {
  editorState.active = i;
  const f = editorState.files[i];
  renderEditorTabs();
  els.spiritEditorLint.hidden = true; els.spiritEditorLint.innerHTML = "";
  if (f.loaded == null) {
    els.spiritEditorArea.value = "loading…"; els.spiritEditorArea.disabled = true;
    try { f.loaded = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent(f.path))).json()).content || ""; }
    catch (e) { f.loaded = ""; }
  }
  els.spiritEditorArea.disabled = false;
  els.spiritEditorArea.value = f.loaded;
  updateEditorDirty();
}
function renderEditorTabs() {
  const host = els.spiritEditorTabs; host.innerHTML = "";
  editorState.files.forEach((f, i) => {
    const b = el("button", "editor-tab" + (i === editorState.active ? " active" : ""), f.path.replace(/^spirits\//, ""));
    b.onclick = () => { if (i !== editorState.active) selectEditorFile(i); };
    host.append(b);
  });
}
function currentEditorFile() { return editorState && editorState.files[editorState.active]; }
function updateEditorDirty() {
  const f = currentEditorFile();
  const dirty = f && f.loaded != null && els.spiritEditorArea.value !== f.loaded;
  els.spiritEditorDirty.hidden = !dirty;
  return dirty;
}
async function saveEditor() {
  const f = currentEditorFile();
  if (!f) return;
  setSaveState("saving");
  els.spiritEditorLint.hidden = true;
  try {
    const r = await fetch("/api/spirits/file?path=" + encodeURIComponent(f.path), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content: els.spiritEditorArea.value }),
    });
    const res = await r.json();
    if (r.status === 422 || res.ok === false) {
      setSaveState("error");
      showEditorLint(res.errors || ["save blocked"], res.warnings || [], false);
      return; // keep dirty; do not update loaded
    }
    f.loaded = els.spiritEditorArea.value; // saved
    setSaveState("saved");
    updateEditorDirty();
    if ((res.warnings || []).length) showEditorLint([], res.warnings, true);
    loadSpiritRituals(); // refresh board (cadence/ceiling/validity may have changed)
  } catch (e) { setSaveState("error"); showEditorLint(["save failed: " + (e.message || e)], [], false); }
}
function showEditorLint(errors, warnings, savedOK) {
  const host = els.spiritEditorLint; host.innerHTML = ""; host.hidden = false;
  host.classList.toggle("lint-ok", savedOK && !errors.length);
  errors.forEach((m) => host.append(el("div", "lint-err", "✕ " + m)));
  warnings.forEach((m) => host.append(el("div", "lint-warn", "⚠ " + m)));
  if (savedOK && warnings.length) host.insertBefore(el("div", "lint-note", "saved with warnings:"), host.firstChild);
}
function closeEditor() { els.spiritEditor.hidden = true; editorState = null; }

async function newRitual(sp) {
  const name = prompt(`New ritual for ${sp} (lowercase name, e.g. "weekly-review"):`);
  if (!name || !name.trim()) return;
  try {
    const r = await fetch("/api/spirits/ritual", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: sp, name: name.trim() }) });
    if (!r.ok) throw new Error(await r.text());
    const { path } = await r.json();
    await loadSpiritRituals();
    openEditor([path]);
  } catch (e) { alert("Couldn't create ritual: " + (e.message || e)); }
}
async function newSpirit() {
  const name = prompt('New spirit (lowercase name, e.g. "news-scout"):');
  if (!name || !name.trim()) return;
  try {
    const r = await fetch("/api/spirits/spirit", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim() }) });
    if (!r.ok) throw new Error(await r.text());
    const { path } = await r.json();
    await loadSpiritRituals();
    loadSpiritsStatus();
    openEditor([`spirits/${name.trim()}/identity.md`, path], 1);
  } catch (e) { alert("Couldn't create spirit: " + (e.message || e)); }
}

if (els.spiritEditorArea) els.spiritEditorArea.addEventListener("input", updateEditorDirty);
if (els.spiritEditorSave) els.spiritEditorSave.addEventListener("click", saveEditor);
if (els.spiritEditorClose) els.spiritEditorClose.addEventListener("click", closeEditor);
if (els.spiritNewSpirit) els.spiritNewSpirit.addEventListener("click", newSpirit);
if (els.spiritEditChargebook) els.spiritEditChargebook.addEventListener("click", () => openEditor(["chargebook.md"]));

// ---- router ----
// ---- CONTACTS (people layer over the vault index) ----
async function postJSON(url, body) {
  const res = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  try { return await res.json(); } catch (e) { return {}; }
}

function showContacts() {
  const rest = location.hash.replace(/^#\/contacts\/?/, "");
  if (rest === "cold") { _coldOnly = true; showContactList(); } // neglect view (deep-linkable)
  else if (rest) { _coldOnly = false; showContactPage(decodeURIComponent(rest)); }
  else { _coldOnly = false; showContactList(); }
}

function showContactList() {
  els.contactsListPane.hidden = false;
  els.contactPagePane.hidden = true;
  loadContactList();
  loadContactTriage();
}

async function loadContactList() {
  let d = { contacts: [] };
  try { d = await (await fetch("/api/contacts")).json(); } catch (e) {}
  window._contacts = d.contacts || [];
  renderContactList(window._contacts, els.contactSearch.value);
}

let _coldOnly = false;

function renderContactList(list, query) {
  const host = els.contactList; host.innerHTML = "";
  const q = (query || "").trim().toLowerCase();
  let rows = q ? list.filter((c) => c.display.toLowerCase().includes(q)) : list.slice();
  const coldCount = list.filter((c) => c.cold).length;
  if (els.contactColdToggle) {
    els.contactColdToggle.textContent = "◆ Cold" + (coldCount ? " " + coldCount : "");
    els.contactColdToggle.classList.toggle("on", _coldOnly);
  }
  if (_coldOnly) {
    rows = rows.filter((c) => c.cold).sort((a, b) => b.daysSince - a.daysSince); // most overdue first
  }
  if (!rows.length) { host.appendChild(emptyRow(_coldOnly ? "No contacts going cold." : q ? "No contacts match." : "No contacts yet.")); return; }
  rows.forEach((c) => host.appendChild(contactRow(c)));
}

function contactRow(c) {
  const row = el("div", "contact-row" + (c.cold ? " cold" : ""));
  row.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(c.key); };
  const left = el("div", "contact-row-left");
  if (c.cold) left.append(el("span", "contact-cold", "◆")); // quiet going-cold marker
  left.append(el("span", "contact-name", c.display));
  if (!c.hasNote) left.append(el("span", "contact-dot", "○")); // quiet no-note indicator
  if (c.openLoops > 0) left.append(el("span", "contact-loops", c.openLoops + " open"));
  const right = el("div", "contact-row-right");
  if (c.upcoming) right.append(el("span", "contact-upcoming", "↑ " + c.upcoming));
  // last-met is BLANK when there is no dated evidence — never a guess
  let meta = c.lastMet ? "last met " + c.lastMet : "";
  if (c.cold && c.daysSince >= 0) meta = "last met " + c.daysSince + "d ago (usually every " + c.medianGap + "d)";
  right.append(el("span", "contact-meta", meta));
  row.append(left, right);
  return row;
}

async function loadContactTriage() {
  let d = { triage: [] };
  try { d = await (await fetch("/api/contacts/triage")).json(); } catch (e) {}
  renderTriage(d.triage || []);
}

function renderTriage(items) {
  const host = els.contactTriage; host.innerHTML = "";
  if (!items.length) { host.hidden = true; return; }
  host.hidden = false;
  window._triage = items;
  // Quiet by default (§4): a one-line summary that expands to a review batch,
  // ranked most-person-like first (deterministic: 2+ caps up, linked-from-people down).
  const head = el("div", "triage-head");
  const label = el("span", "triage-label", "Review — " + items.length + " note-less name" + (items.length === 1 ? "" : "s") + " (ranked by person-likelihood)");
  const headActions = el("span", "triage-head-actions");
  const bulk = pillLight("Dismiss all " + items.length, async () => {
    if (!confirm("Dismiss all " + items.length + " queued names? (remembered — they won't return)")) return;
    await postJSON("/api/contacts/dismiss-bulk", { keys: items.map((t) => t.key) });
    showContactList();
  });
  bulk.hidden = true;
  const toggle = pillLight("Review ▾", () => {
    rows.hidden = !rows.hidden; bulk.hidden = rows.hidden;
    toggle.textContent = rows.hidden ? "Review ▾" : "Hide ▴";
  });
  headActions.append(bulk, toggle);
  head.append(label, headActions);
  const rows = el("div", "triage-rows"); rows.hidden = true;
  items.slice(0, 30).forEach((t) => {
    const r = el("div", "triage-row");
    const nm = el("span", "triage-name", t.display);
    if (t.likelyOrg) nm.append(el("span", "triage-hint", " likely org"));
    r.append(nm, el("span", "triage-refs", t.refCount + " ref" + (t.refCount === 1 ? "" : "s")));
    const act = el("span", "triage-actions");
    act.append(
      pill("Person", async () => { await postJSON("/api/contacts/confirm", { key: t.key, display: t.display }); showContactList(); }),
      pillLight("Org", async () => { await postJSON("/api/contacts/org", { key: t.key }); showContactList(); }),
      pillLight("Dismiss", async () => { await postJSON("/api/contacts/dismiss", { key: t.key }); showContactList(); }),
    );
    r.append(act);
    rows.append(r);
  });
  host.append(head, rows);
}

async function showContactPage(key) {
  els.contactsListPane.hidden = true;
  els.contactPagePane.hidden = false;
  els.contactPageSaved.textContent = "";
  els.contactPage.textContent = "Loading…";
  let p;
  try {
    const res = await fetch("/api/contacts/page?key=" + encodeURIComponent(key));
    if (!res.ok) { els.contactPage.textContent = "No such contact."; return; }
    p = await res.json();
  } catch (e) { els.contactPage.textContent = "Error loading contact."; return; }
  renderContactPage(p);
}

function cpSection(title, count) {
  const s = el("div", "cp-section");
  const h = el("div", "cp-section-head", title);
  if (count != null) h.append(el("span", "cp-count", " " + count));
  s.append(h);
  return s;
}

function renderContactPage(p) {
  const host = els.contactPage; host.innerHTML = "";

  // 1. header — name, aliases, linked firms
  const header = el("div", "cp-header");
  const nameRow = el("div", "cp-name-row");
  nameRow.append(el("h1", "cp-name", p.display));
  if (!p.hasNote) nameRow.append(el("span", "cp-nonote", "no note yet"));
  header.append(nameRow);
  if (p.aliases && p.aliases.length) header.append(el("div", "cp-aliases", "aka " + p.aliases.join(" · ")));
  if (p.firms && p.firms.length) {
    const f = el("div", "cp-firms");
    p.firms.forEach((fr) => {
      const chip = el("span", "cp-firm", fr.display);
      chip.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(fr.key); };
      f.append(chip);
    });
    header.append(f);
  }
  host.append(header);

  // last-met line + neglect (§3)
  let lastLine = p.lastMet ? "Last met " + p.lastMet : "No dated meetings on record";
  if (p.daysSince >= 0 && p.interactions >= 3) {
    lastLine = "Last met " + p.daysSince + "d ago" + (p.medianGap ? " (usually every " + p.medianGap + "d)" : "");
  }
  const lm = el("div", "cp-lastmet", lastLine);
  if (p.cold) lm.append(el("span", "cp-cold", "◆ going cold"));
  host.append(lm);

  // open loops (§2) — unchecked tasks from meeting notes, grouped by source
  if (p.loops && p.loops.length) {
    let n = 0; p.loops.forEach((g) => (n += g.loops.length));
    const sec = cpSection("Open loops", n);
    p.loops.forEach((g) => {
      const gh = el("div", "cp-loop-group");
      const head = el("div", "cp-loop-src");
      head.append(el("span", "cp-date", g.date), el("span", "cp-loop-note", g.name));
      head.onclick = () => { _noteReturn = "#/contacts/" + encodeURIComponent(p.key); openNoteByPath(g.path); };
      gh.append(head);
      g.loops.forEach((it) => {
        const row = el("label", "cp-loop-row");
        if (it.kind === "checkbox") {
          const box = el("input"); box.type = "checkbox";
          box.addEventListener("change", async () => {
            await postJSON("/api/note/task", { path: g.path, line: it.line, want: box.checked });
            showContactPage(p.key);
          });
          row.append(box);
        } else {
          row.append(el("span", "cp-loop-dot", "›"));
        }
        row.append(el("span", "cp-loop-text", it.text));
        gh.append(row);
      });
      sec.append(gh);
    });
    host.append(sec);
  }

  // 2. upcoming (matched calendar events / candidates to confirm)
  if (p.upcoming && p.upcoming.length) {
    const sec = cpSection("Upcoming");
    p.upcoming.forEach((u) => {
      const row = el("div", "cp-upcoming-row");
      row.append(el("span", "cp-date", u.date), el("span", "cp-title", u.title));
      if (!u.confirmed && u.email) {
        row.append(pill("This is " + p.display + " (" + u.email + ")", async () => {
          await postJSON("/api/contacts/email", { key: p.key, display: p.display, email: u.email });
          showContactPage(p.key);
        }));
      } else if (u.confirmed) {
        row.append(el("span", "cp-confirmed", "✓ matched"));
      }
      sec.append(row);
    });
    host.append(sec);
  }

  const openItem = (path) => { _noteReturn = "#/contacts/" + encodeURIComponent(p.key); openNoteByPath(path); };

  // 3. timeline (dated interactions, newest first) — each opens the note view
  const tl = cpSection("Timeline", p.timeline ? p.timeline.length : 0);
  if (!p.timeline || !p.timeline.length) tl.append(el("div", "cp-empty", "No dated interactions."));
  (p.timeline || []).forEach((t) => {
    const row = el("div", "cp-tl-row cp-clickable");
    row.append(el("span", "cp-date", t.date), el("span", "cp-src", t.sourceType), el("span", "cp-tl-name", t.name));
    if (t.isTranscript) row.append(el("span", "cp-badge", "transcript"));
    row.onclick = () => openItem(t.path);
    tl.append(row);
  });
  host.append(tl);

  // 4. transcripts
  if (p.transcripts && p.transcripts.length) {
    const sec = cpSection("Transcripts", p.transcripts.length);
    p.transcripts.forEach((t) => {
      const row = el("div", "cp-tl-row cp-clickable");
      row.append(el("span", "cp-date", t.date), el("span", "cp-tl-name", t.title), el("span", "cp-src", t.source));
      row.onclick = () => openItem(t.path);
      sec.append(row);
    });
    host.append(sec);
  }

  // 5. mentions (undated — never a date claim)
  if (p.mentions && p.mentions.length) {
    const sec = cpSection("Mentions (no date)", p.mentions.length);
    p.mentions.forEach((m) => {
      const row = el("div", "cp-mention cp-clickable", m.name);
      row.onclick = () => openItem(m.path);
      sec.append(row);
    });
    host.append(sec);
  }

  // 6. note pane — raw-markdown editor; blank + placeholder when no note exists
  const note = cpSection("Note");
  const ta = el("textarea", "cp-note-editor");
  ta.value = p.noteBody || "";
  ta.placeholder = "notes about " + p.display + "…  (type [[ to link a name)";
  attachWikilinkAutocomplete(ta);
  note.append(ta);
  const actions = el("div", "cp-note-actions");
  const saveBtn = pill(p.hasNote ? "Save note" : "Create note", async () => {
    els.contactPageSaved.textContent = "saving…";
    const np = await postJSON("/api/contacts/note", { key: p.key, display: p.display, body: ta.value });
    els.contactPageSaved.textContent = "saved";
    renderContactPage(np);
  });
  actions.append(saveBtn);
  if (!p.hasNote) actions.append(el("span", "cp-note-hint", "first save creates " + p.display + ".md with categories: [people]"));
  note.append(actions);
  host.append(note);
}

// create flow — bind to existing links before making a new contact (§5)
function openCreatePanel() {
  if (document.querySelector(".contact-create")) return;
  const box = el("div", "contact-create");
  const head = el("div", "contact-create-head", "Add a contact — existing links are checked first");
  head.append(pillLight("✕", () => box.remove()));
  const input = el("input", "contact-create-input"); input.type = "text"; input.placeholder = "Type a name…";
  const results = el("div", "contact-create-results");
  box.append(head, input, results);
  els.contactList.before(box);
  input.focus();
  let timer;
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => runCreateSearch(input.value.trim(), results), 200); });
}

async function runCreateSearch(q, host) {
  host.innerHTML = "";
  if (!q) return;
  let d = { results: [] };
  try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
  (d.results || []).forEach((r) => {
    const row = el("div", "cc-result");
    row.append(el("span", "cc-name", r.display), el("span", "cc-refs", r.refCount + " ref" + (r.refCount === 1 ? "" : "s") + (r.hasNote ? " · has note" : "")));
    const act = el("span", "cc-actions");
    act.append(
      pillLight("Open", () => { location.hash = "#/contacts/" + encodeURIComponent(r.key); }),
      pill("Bind “" + q + "”", async () => { await postJSON("/api/contacts/bind", { variant: q, canonical: r.key, display: q }); location.hash = "#/contacts/" + encodeURIComponent(r.key); }),
    );
    row.append(act);
    host.append(row);
  });
  const create = el("div", "cc-create");
  create.append(pill("Create new contact “" + q + "”", async () => {
    const p = await postJSON("/api/contacts/note", { key: q.toLowerCase(), display: q, body: "" });
    location.hash = "#/contacts/" + encodeURIComponent(p.key || q.toLowerCase());
  }));
  host.append(create);
}

if (els.contactSearch) els.contactSearch.addEventListener("input", () => renderContactList(window._contacts || [], els.contactSearch.value));
if (els.contactColdToggle) els.contactColdToggle.addEventListener("click", () => { location.hash = _coldOnly ? "#/contacts" : "#/contacts/cold"; });
if (els.contactAddBtn) els.contactAddBtn.addEventListener("click", openCreatePanel);
if (els.contactBackBtn) els.contactBackBtn.addEventListener("click", () => { location.hash = "#/contacts"; });

// ---- UNIVERSAL NOTE VIEW (contacts power-pass §1) ----
let _note = null; // {path, name, raw, backlinks, vault}
let _noteReturn = "#/contacts";

function showNote(path) {
  els.noteView.hidden = false;
  els.noteSaved.textContent = "";
  loadNote(path);
}

async function loadNote(path) {
  els.noteRendered.innerHTML = "Loading…";
  els.noteBacklinks.innerHTML = "";
  els.noteRaw.hidden = true;
  els.noteRendered.hidden = false;
  els.noteSaveBtn.hidden = true;
  els.noteRawToggle.textContent = "Edit raw";
  try {
    const res = await fetch("/api/note?path=" + encodeURIComponent(path));
    if (!res.ok) { els.noteRendered.textContent = "Note not found."; return; }
    _note = await res.json();
  } catch (e) { els.noteRendered.textContent = "Error loading note."; return; }
  els.noteTitle.textContent = _note.name;
  els.noteObsidian.href = "obsidian://open?vault=" + encodeURIComponent(_note.vault) +
    "&file=" + encodeURIComponent(_note.path.replace(/\.md$/, ""));
  renderNoteBody();
  renderNoteBacklinks();
}

function renderNoteBody() {
  els.noteRendered.innerHTML = "";
  els.noteRendered.appendChild(renderMarkdown(_note.raw, _note.path));
}

function renderNoteBacklinks() {
  const host = els.noteBacklinks; host.innerHTML = "";
  const bl = _note.backlinks || [];
  if (!bl.length) return;
  host.appendChild(el("div", "note-bl-head", "Linked from " + bl.length + " note" + (bl.length === 1 ? "" : "s")));
  bl.forEach((b) => {
    const row = el("div", "note-bl-row");
    row.append(el("span", "note-bl-date", b.date || ""), el("span", "note-bl-name", b.name));
    row.onclick = () => openNoteByPath(b.path);
    host.appendChild(row);
  });
}

function openNoteByPath(path) {
  location.hash = "#/note/" + encodeURIComponent(path);
}

async function resolveWikilink(target) {
  let r;
  try { r = await (await fetch("/api/note/resolve?target=" + encodeURIComponent(target))).json(); }
  catch (e) { return; }
  if (r.kind === "contact") location.hash = "#/contacts/" + encodeURIComponent(r.key);
  else if (r.kind === "note") openNoteByPath(r.path);
  else els.noteSaved.textContent = "no note for [[" + target + "]]";
}

async function toggleNoteTask(line, want, box) {
  try {
    const res = await fetch("/api/note/task", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: _note.path, line, want }) });
    if (!res.ok) throw new Error(await res.text());
    // refresh raw so subsequent toggles use correct line state
    const g = await (await fetch("/api/note?path=" + encodeURIComponent(_note.path))).json();
    _note.raw = g.raw; _note.backlinks = g.backlinks;
  } catch (e) { box.checked = !want; els.noteSaved.textContent = "toggle failed — reload"; }
}

// raw-edit toggle + save
if (els.noteRawToggle) els.noteRawToggle.addEventListener("click", () => {
  const editing = !els.noteRaw.hidden;
  if (editing) { // back to rendered
    els.noteRaw.hidden = true; els.noteRendered.hidden = false; els.noteSaveBtn.hidden = true;
    els.noteRawToggle.textContent = "Edit raw";
    renderNoteBody();
  } else {
    els.noteRaw.value = _note.raw; els.noteRaw.hidden = false; els.noteRendered.hidden = true;
    els.noteSaveBtn.hidden = false; els.noteRawToggle.textContent = "Preview";
  }
});
if (els.noteSaveBtn) els.noteSaveBtn.addEventListener("click", async () => {
  els.noteSaved.textContent = "saving…";
  try {
    const res = await fetch("/api/note", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: _note.path, body: els.noteRaw.value }) });
    if (!res.ok) throw new Error(await res.text());
    els.noteSaved.textContent = "saved";
    await loadNote(_note.path); // reindex happened server-side; re-render fresh
  } catch (e) { els.noteSaved.textContent = "save failed"; }
});
if (els.noteBackBtn) els.noteBackBtn.addEventListener("click", () => { location.hash = _noteReturn || "#/contacts"; });

// --- a compact markdown renderer that returns DOM (so wikilinks + checkboxes
// are interactive). Handles the shapes this vault uses. ---
function renderMarkdown(raw, notePath) {
  const frag = document.createDocumentFragment();
  const lines = raw.split("\n");
  let i = 0;
  // skip a leading frontmatter block (metadata; editable in raw mode)
  if (lines[0] === "---") {
    let j = 1; while (j < lines.length && lines[j] !== "---") j++;
    if (j < lines.length) i = j + 1;
  }
  let para = [];
  const flushPara = () => {
    if (!para.length) return;
    const p = el("p", "md-p");
    inlineInto(p, para.join(" "), notePath);
    frag.appendChild(p); para = [];
  };
  for (; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();
    // code fence
    if (t.startsWith("```")) {
      flushPara();
      const code = []; i++;
      for (; i < lines.length && !lines[i].trim().startsWith("```"); i++) code.push(lines[i]);
      const pre = el("pre", "md-pre"); pre.textContent = code.join("\n"); frag.appendChild(pre);
      continue;
    }
    // heading
    let hm = line.match(/^(#{1,6})\s+(.*)$/);
    if (hm) { flushPara(); const h = el("h" + hm[1].length, "md-h"); inlineInto(h, hm[2], notePath); frag.appendChild(h); continue; }
    // checkbox
    let cb = line.match(/^(\s*)[-*]\s+\[([ xX])\]\s?(.*)$/);
    if (cb) {
      flushPara();
      const row = el("label", "md-task");
      const box = el("input"); box.type = "checkbox"; box.checked = cb[2] !== " ";
      const lineNo = i;
      box.addEventListener("change", () => toggleNoteTask(lineNo, box.checked, box));
      const span = el("span", "md-task-text"); inlineInto(span, cb[3], notePath);
      row.append(box, span); frag.appendChild(row); continue;
    }
    // list item
    let li = line.match(/^(\s*)[-*]\s+(.*)$/) || line.match(/^(\s*)\d+\.\s+(.*)$/);
    if (li) { flushPara(); const item = el("div", "md-li"); item.append(el("span", "md-bullet", "•")); const s = el("span"); inlineInto(s, li[2], notePath); item.append(s); frag.appendChild(item); continue; }
    // blockquote
    if (t.startsWith(">")) { flushPara(); const bq = el("blockquote", "md-bq"); inlineInto(bq, t.replace(/^>\s?/, ""), notePath); frag.appendChild(bq); continue; }
    // horizontal rule
    if (t === "---" || t === "***") { flushPara(); frag.appendChild(el("hr", "md-hr")); continue; }
    // blank → paragraph break
    if (t === "") { flushPara(); continue; }
    para.push(t);
  }
  flushPara();
  return frag;
}

// inlineInto parses inline markdown (wikilinks, links, bold/italic/code) into DOM.
function inlineInto(host, text, notePath) {
  // token regex: [[wikilink]] | [text](url) | **bold** | *italic* | `code`
  const re = /\[\[([^\]]+)\]\]|\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`/g;
  let last = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > last) host.appendChild(document.createTextNode(text.slice(last, m.index)));
    if (m[1] != null) { // wikilink
      const parts = m[1].split("|");
      const target = parts[0].trim(), disp = (parts[1] || parts[0]).trim();
      const a = el("span", "wikilink", disp);
      a.onclick = () => resolveWikilink(target);
      host.appendChild(a);
    } else if (m[2] != null) { // [text](url)
      const a = el("a", "md-link", m[2]); a.href = m[3]; a.target = "_blank"; host.appendChild(a);
    } else if (m[4] != null) { host.appendChild(el("strong", null, m[4])); }
    else if (m[5] != null) { host.appendChild(el("em", null, m[5])); }
    else if (m[6] != null) { host.appendChild(el("code", "md-code", m[6])); }
    last = re.lastIndex;
  }
  if (last < text.length) host.appendChild(document.createTextNode(text.slice(last)));
}

// ---- [[wikilink]] autocomplete for markdown editors (Obsidian-style) ----
// Typing `[[` opens a popup that searches entities and narrows as you type;
// picking one inserts `[[<lowercase name>]]`. The dropdown shows the plain
// lowercase name (no brackets).
let _wlPopup = null, _wlItems = [], _wlSel = -1, _wlStart = -1, _wlTa = null, _wlTimer = null;

function wlClose() {
  if (_wlPopup) { _wlPopup.remove(); _wlPopup = null; }
  _wlItems = []; _wlSel = -1; _wlStart = -1; _wlTa = null;
}

// wlQuery finds an open, unclosed `[[…` immediately before the caret.
function wlQuery(ta) {
  const pos = ta.selectionStart;
  const before = ta.value.slice(0, pos);
  const open = before.lastIndexOf("[[");
  if (open < 0) return null;
  const between = before.slice(open + 2);
  if (between.includes("]]") || between.includes("\n")) return null;
  return { start: open, query: between };
}

function attachWikilinkAutocomplete(ta) {
  if (!ta || ta._wlBound) return;
  ta._wlBound = true;
  ta.addEventListener("input", () => {
    const q = wlQuery(ta);
    if (!q) { wlClose(); return; }
    _wlTa = ta; _wlStart = q.start;
    clearTimeout(_wlTimer);
    _wlTimer = setTimeout(() => wlSearch(ta, q.query), 100);
  });
  ta.addEventListener("keydown", (e) => {
    if (!_wlPopup || !_wlItems.length) return;
    if (e.key === "ArrowDown") { e.preventDefault(); _wlSel = (_wlSel + 1) % _wlItems.length; wlPaint(); }
    else if (e.key === "ArrowUp") { e.preventDefault(); _wlSel = (_wlSel - 1 + _wlItems.length) % _wlItems.length; wlPaint(); }
    else if (e.key === "Enter" || e.key === "Tab") { e.preventDefault(); if (_wlItems[_wlSel]) wlInsert(_wlItems[_wlSel]); }
    else if (e.key === "Escape") { e.preventDefault(); wlClose(); }
  });
  ta.addEventListener("blur", () => setTimeout(() => { if (_wlTa === ta) wlClose(); }, 150));
  ta.addEventListener("scroll", () => { if (_wlPopup && _wlTa === ta) wlPosition(ta); });
}

async function wlSearch(ta, query) {
  let results = [];
  try { results = (await (await fetch("/api/contacts/search?q=" + encodeURIComponent(query || ""))).json()).results || []; } catch (e) {}
  // drop dated meeting notes (e.g. "2026-05-19 shoumik sync") — you link names, not dates
  results = results.filter((r) => !/^\d{4}-\d{2}-\d{2}/.test(r.key));
  _wlItems = results.slice(0, 8);
  if (!_wlItems.length) { wlClose(); return; }
  _wlSel = 0;
  if (!_wlPopup) { _wlPopup = el("div", "wl-popup"); document.body.appendChild(_wlPopup); }
  _wlPopup.innerHTML = "";
  _wlItems.forEach((it, i) => {
    const row = el("div", "wl-item");
    row.append(el("span", "wl-name", it.key)); // lowercase name, no brackets
    if (it.refCount) row.append(el("span", "wl-refs", it.refCount + " ref" + (it.refCount === 1 ? "" : "s")));
    row.addEventListener("mousedown", (e) => { e.preventDefault(); wlInsert(it); }); // mousedown beats blur
    row.addEventListener("mouseenter", () => { _wlSel = i; wlPaint(); });
    _wlPopup.appendChild(row);
  });
  wlPaint();
  wlPosition(ta);
}

function wlPaint() {
  if (!_wlPopup) return;
  [..._wlPopup.children].forEach((c, i) => c.classList.toggle("sel", i === _wlSel));
}

function wlInsert(it) {
  const ta = _wlTa; if (!ta) return;
  const pos = ta.selectionStart;
  const ins = "[[" + it.key + "]]";
  ta.value = ta.value.slice(0, _wlStart) + ins + ta.value.slice(pos);
  const np = _wlStart + ins.length;
  wlClose();
  ta.focus();
  ta.setSelectionRange(np, np);
  ta.dispatchEvent(new Event("input", { bubbles: true })); // run the field's own state/save handlers
}

function wlPosition(ta) {
  if (!_wlPopup) return;
  const c = caretCoords(ta, ta.selectionStart);
  const maxLeft = window.innerWidth - _wlPopup.offsetWidth - 12;
  _wlPopup.style.left = Math.round(Math.min(c.left, Math.max(8, maxLeft))) + "px";
  _wlPopup.style.top = Math.round(c.top) + "px";
}

// caretCoords returns viewport coords just below the caret (mirror-div technique).
// Textareas wrap; single-line inputs don't, and the popup sits below the field.
function caretCoords(ta, position) {
  const isInput = ta.tagName === "INPUT";
  const s = getComputedStyle(ta);
  const div = document.createElement("div");
  const props = ["boxSizing", "width", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth",
    "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "fontFamily", "fontSize", "fontWeight",
    "fontStyle", "lineHeight", "letterSpacing", "textTransform", "wordSpacing", "tabSize"];
  props.forEach((p) => (div.style[p] = s[p]));
  div.style.position = "absolute";
  div.style.visibility = "hidden";
  div.style.whiteSpace = isInput ? "pre" : "pre-wrap";
  div.style.wordWrap = "break-word";
  div.style.overflow = "hidden";
  if (isInput) div.style.width = "auto";
  div.textContent = ta.value.slice(0, position);
  const span = document.createElement("span");
  span.textContent = ta.value.slice(position) || ".";
  div.appendChild(span);
  document.body.appendChild(div);
  const rect = ta.getBoundingClientRect();
  const lh = parseFloat(s.lineHeight) || parseFloat(s.fontSize) * 1.4;
  const left = rect.left + (span.offsetLeft - ta.scrollLeft);
  const top = isInput ? rect.bottom + 2 : rect.top + (span.offsetTop - ta.scrollTop) + lh;
  document.body.removeChild(div);
  return { left, top };
}

if (els.noteRaw) attachWikilinkAutocomplete(els.noteRaw);

// ---- inline [[link]] live-preview for single-line fields (Obsidian-style) ----
// A field with [[links]] shows a rendered overlay (names, no brackets, links
// clickable) when not focused; clicking into it reveals the raw [[…]] for
// editing; clicking a link opens the note.
const wikilinkRe2 = /\[\[([^\]]+)\]\]/g;

function renderInlineLinks(host, text) {
  let last = 0, m;
  wikilinkRe2.lastIndex = 0;
  while ((m = wikilinkRe2.exec(text))) {
    if (m.index > last) host.appendChild(document.createTextNode(text.slice(last, m.index)));
    const parts = m[1].split("|");
    const target = parts[0].trim(), disp = (parts[1] || parts[0]).trim();
    const a = el("span", "inline-link", disp);
    a.addEventListener("mousedown", (e) => { e.preventDefault(); e.stopPropagation(); resolveWikilink(target); });
    host.appendChild(a);
    last = wikilinkRe2.lastIndex;
  }
  if (last < text.length) host.appendChild(document.createTextNode(text.slice(last)));
}

function attachInlineLinks(input) {
  if (input._inlineBound) return;
  input._inlineBound = true;
  const parent = input.parentElement;
  parent.classList.add("has-inline-overlay");
  let overlay = null;
  const hasLinks = () => /\[\[[^\]]+\]\]/.test(input.value);
  function render() {
    if (!hasLinks() || document.activeElement === input) {
      if (overlay) overlay.style.display = "none";
      input.classList.remove("inline-hidden");
      return;
    }
    if (!overlay) { overlay = el("div", "inline-overlay"); parent.appendChild(overlay); }
    overlay.innerHTML = "";
    renderInlineLinks(overlay, input.value);
    const cs = getComputedStyle(input); // copy BEFORE hiding the input's text
    ["fontFamily", "fontSize", "fontWeight", "fontStyle", "letterSpacing", "color", "paddingLeft", "paddingRight", "textAlign"].forEach((p) => (overlay.style[p] = cs[p]));
    overlay.style.top = input.offsetTop + "px";
    overlay.style.left = input.offsetLeft + "px";
    overlay.style.width = input.offsetWidth + "px";
    overlay.style.height = input.offsetHeight + "px";
    overlay.style.lineHeight = input.offsetHeight + "px"; // vertically center the single line
    overlay.style.display = "block";
    input.classList.add("inline-hidden");
  }
  input.addEventListener("focus", render);
  input.addEventListener("blur", () => setTimeout(render, 0));
  setTimeout(render, 0); // defer: the input must be laid out for offset positioning
}

// ---- quick-lookup command bar (⌘K / Ctrl-K anywhere) ----
let cmdSel = -1, cmdResults = [];
function openCmdbar() {
  els.cmdbar.hidden = false;
  els.cmdbarInput.value = "";
  els.cmdbarResults.innerHTML = "";
  els.cmdbarCard.hidden = true;
  cmdSel = -1; cmdResults = [];
  els.cmdbarInput.focus();
}
function closeCmdbar() { els.cmdbar.hidden = true; }

let cmdTimer;
if (els.cmdbarInput) {
  els.cmdbarInput.addEventListener("input", () => {
    clearTimeout(cmdTimer);
    els.cmdbarCard.hidden = true;
    const q = els.cmdbarInput.value.trim();
    cmdTimer = setTimeout(() => cmdSearch(q), 150);
  });
  els.cmdbarInput.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); cmdMove(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); cmdMove(-1); }
    else if (e.key === "Enter") { e.preventDefault(); if (cmdResults[cmdSel]) cmdShowCard(cmdResults[cmdSel].key); }
    else if (e.key === "Escape") { closeCmdbar(); }
  });
}
if (els.cmdbarBackdrop) els.cmdbarBackdrop.addEventListener("click", closeCmdbar);
window.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); openCmdbar(); }
  else if (e.key === "Escape" && !els.cmdbar.hidden) { closeCmdbar(); }
});

async function cmdSearch(q) {
  const host = els.cmdbarResults; host.innerHTML = ""; cmdSel = -1; cmdResults = [];
  if (!q) return;
  let d = { results: [] };
  try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
  cmdResults = (d.results || []).slice(0, 8);
  cmdResults.forEach((r, i) => {
    const row = el("div", "cmd-result");
    row.append(el("span", "cmd-name", r.display), el("span", "cmd-refs", (r.hasNote ? "note" : "no note") + " · " + r.refCount + " ref" + (r.refCount === 1 ? "" : "s")));
    row.onclick = () => cmdShowCard(r.key);
    row.onmouseenter = () => { cmdSel = i; paintCmdSel(); };
    host.append(row);
  });
  if (cmdResults.length) { cmdSel = 0; paintCmdSel(); }
}
function cmdMove(d) { if (!cmdResults.length) return; cmdSel = (cmdSel + d + cmdResults.length) % cmdResults.length; paintCmdSel(); }
function paintCmdSel() {
  [...els.cmdbarResults.children].forEach((c, i) => c.classList.toggle("sel", i === cmdSel));
}

async function cmdShowCard(key) {
  let c;
  try {
    const res = await fetch("/api/contacts/card?key=" + encodeURIComponent(key));
    if (!res.ok) return;
    c = await res.json();
  } catch (e) { return; }
  const host = els.cmdbarCard; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "cmd-card-head");
  head.append(el("span", "cmd-card-name", c.display));
  if (!c.hasNote) head.append(el("span", "cmd-card-nonote", "no note"));
  host.append(head);
  const facts = el("div", "cmd-card-facts");
  facts.append(cmdFact("Last met", c.lastMet || "—"));
  facts.append(cmdFact("Next", c.nextUpcoming || "—"));
  if (c.latestTranscript) {
    const f = cmdFact("Latest transcript", c.latestTranscript.date + " · " + c.latestTranscript.title);
    f.classList.add("cmd-fact-link");
    f.onclick = () => { closeCmdbar(); _noteReturn = "#/contacts/" + encodeURIComponent(c.key); openNoteByPath(c.latestTranscript.path); };
    facts.append(f);
  }
  host.append(facts);
  const jump = pill("Open contact page →", () => { closeCmdbar(); location.hash = "#/contacts/" + encodeURIComponent(c.key); });
  host.append(jump);
}
function cmdFact(label, val) {
  const f = el("div", "cmd-fact");
  f.append(el("span", "cmd-fact-label", label), el("span", "cmd-fact-val", val));
  return f;
}

function route() {
  const h = location.hash;
  const goals = h === "#/goals";
  const cal = h === "#/calendar";
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const contacts = h === "#/contacts" || h.startsWith("#/contacts/");
  const note = h.startsWith("#/note/");
  const day = !goals && !cal && !sp && !contacts && !note;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.calendarView.hidden = !cal;
  els.spiritsView.hidden = !sp;
  els.contactsView.hidden = !contacts;
  els.noteView.hidden = !note;
  els.dateNav.hidden = !day;
  els.goalsNav.hidden = !day;
  els.calNav.hidden = !day;
  els.contactsNav.hidden = !day;
  els.spiritsNav.hidden = !day;
  els.dayNav.hidden = day;
  if (goals) loadGoals();
  else if (cal) loadCalendar();
  else if (sp) showSpirits(); // excalibur harness: feed / runs / approvals
  else if (contacts) showContacts(); // people layer: list / page
  else if (note) showNote(decodeURIComponent(h.slice("#/note/".length))); // universal note view
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
