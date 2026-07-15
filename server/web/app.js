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
  contactEmailReview: document.getElementById("contactEmailReview"),
  contactSearch: document.getElementById("contactSearch"),
  contactColdToggle: document.getElementById("contactColdToggle"),
  contactAddBtn: document.getElementById("contactAddBtn"),
  contactBackBtn: document.getElementById("contactBackBtn"),
  contactPage: document.getElementById("contactPage"),
  contactPageSaved: document.getElementById("contactPageSaved"),
  // reading (book shelf over the extrinsic zone)
  readingView: document.getElementById("readingView"),
  readingNav: document.getElementById("readingNav"),
  readingStrip: document.getElementById("readingStrip"),
  bookShelf: document.getElementById("bookShelf"),
  bookSearch: document.getElementById("bookSearch"),
  bookSort: document.getElementById("bookSort"),
  bookFilter: document.getElementById("bookFilter"),
  bookAddBtn: document.getElementById("bookAddBtn"),
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
  // cast command bar (press /)
  castbar: document.getElementById("castbar"),
  castbarBackdrop: document.getElementById("castbarBackdrop"),
  castbarInput: document.getElementById("castbarInput"),
  castbarResults: document.getElementById("castbarResults"),
  castbarArg: document.getElementById("castbarArg"),
  castbarArgLabel: document.getElementById("castbarArgLabel"),
  castbarArgInput: document.getElementById("castbarArgInput"),
  castbarArgHint: document.getElementById("castbarArgHint"),
  castbarCast: document.getElementById("castbarCast"),
  // feed (manifest's one inbox — top-level surface)
  feedView: document.getElementById("feedView"),
  feedNav: document.getElementById("feedNav"),
  feedNavBadge: document.getElementById("feedNavBadge"),
  feedFilters: document.getElementById("feedFilters"),
  feedSignals: document.getElementById("feedSignals"),
  feedList: document.getElementById("feedList"),
  feedAskBtn: document.getElementById("feedAskBtn"),
  feedRunNowBtn: document.getElementById("feedRunNowBtn"),
  // content studio (draft board + inspiration watchlist)
  studioView: document.getElementById("studioView"),
  studioNav: document.getElementById("studioNav"),
  studioTabs: document.getElementById("studioTabs"),
  studioRuns: document.getElementById("studioRuns"),
  studioBody: document.getElementById("studioBody"),
  // spirits (excalibur harness) view
  spiritsView: document.getElementById("spiritsView"),
  spiritsNav: document.getElementById("spiritsNav"),
  spiritsStatus: document.getElementById("spiritsStatus"),
  sp_runs: document.getElementById("sp-runs"),
  spiritRunsList: document.getElementById("spiritRunsList"),
  spiritRunDetail: document.getElementById("spiritRunDetail"),
  sp_approvals: document.getElementById("sp-approvals"),
  spiritApprovalList: document.getElementById("spiritApprovalList"),
  spiritApprBadge: document.getElementById("spiritApprBadge"),
  toastHost: document.getElementById("toastHost"),
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
    const si = slotForGoalId(t.goalId, focus);
    if (si >= 0 && si < MAX_TASKS && rows[si] === null) rows[si] = t; // seat at its goal's slot
    else leftover.push(t); // manual tasks, or a slot already taken
  });
  let li = 0; // fill the gaps with the rest, in order
  for (let i = 0; i < MAX_TASKS; i++) {
    if (rows[i] === null && li < leftover.length) rows[i] = leftover[li++];
  }
  for (let i = 0; i < MAX_TASKS; i++) {
    addTaskRow(rows[i] || { text: "", done: false }, i + 1);
  }
}
// slotForGoalId returns the focus slot index a task belongs under: the slot
// whose most-specific id (cascade task → milestone → 90-day goal) is a
// segment-boundary prefix of the task's goal id. -1 when the task isn't linked
// to any current focus slot (a manually-typed task). Slug ids like
// "aion/series-a-15m/<milestone>/<task>" make prefix matching exact.
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

async function loadGoals(focusId) {
  try {
    const doc = await (await fetch("/api/goals")).json();
    renderAreas(doc.areas || []);
    if (focusId) focusGoal(focusId);
  } catch (e) { setSaveState("error"); }
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
  if (g.id) wrap.dataset.goalId = g.id; // #/goals/<id> deep-link anchor
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
// Purely the engine console since feed-central: RUNS · RITUALS · APPROVALS.
// The feed lives one level up as its own tab; the ENGINE owns execution — the
// only write toward it is a spooled run-now request it picks up on its own.
const SPIRIT_TABS = ["runs", "rituals", "approvals"];
let spiritStatusCache = null;
let spiritRuns = { data: [], queued: [] }; // last poll of /api/spirits/runs — the ONLY run state; nothing else is held
let openRunId = null;                       // which run's report detail is expanded (for live body refresh)

function showSpirits() {
  const tab = spiritTabFromHash();
  SPIRIT_TABS.forEach((t) => { els["sp_" + t].hidden = t !== tab; });
  document.querySelectorAll("#spiritsTabs .atab").forEach((a) => a.classList.toggle("active", a.dataset.tab === tab));
  loadSpiritsStatus(); // engine-alive chip + approvals badge show on every sub-tab
  if (tab === "runs") loadSpiritRuns();
  else if (tab === "rituals") loadSpiritRituals();
  else if (tab === "approvals") loadSpiritApprovals();
  refreshSpiritApprovalBadge();
  ensureLivePoll(); // resume watching any queued/running runs, derived from files
}
function spiritTabFromHash() {
  const t = (location.hash.split("/")[2] || "runs");
  return SPIRIT_TABS.includes(t) ? t : "runs";
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
function setBadge(elm, n) {
  if (!elm) return;
  elm.hidden = !n;
  elm.textContent = n || "";
}

// ---- in-app toasts (run finished → report; digest landed → feed). No OS notifications. ----
function showToast(msg, onClick, kind) {
  const host = els.toastHost;
  if (!host) return;
  const t = el("div", "toast" + (kind ? " toast-" + kind : ""));
  t.append(el("span", "toast-msg", msg));
  if (onClick) { t.classList.add("clickable"); t.onclick = () => { onClick(); t.remove(); }; }
  const x = el("button", "toast-x", "✕");
  x.onclick = (e) => { e.stopPropagation(); t.remove(); };
  t.append(x);
  host.append(t);
  setTimeout(() => t.remove(), 9000); // dismisses itself
}

// ---- file-derived live run polling (replaces watchForNewRun) ----
// A single ~3s poll while the SPIRITS or FEED tab is open AND some run is
// queued/running (dig-from-feed needs run-watching without leaving the feed).
// Everything shown derives from the runs+queued files, so a refresh mid-run
// loses nothing. Transitions raise toasts; the open report body refreshes live.
let livePollTimer = null;
let runOutcomes = {};       // runId → last-seen outcome (transition detection)
let liveBaselined = false;  // don't toast runs that were already finished on first look
let knownDigestIds = null;  // feed digest ids seen, for the digest-landed toast

function pollScopeOpen() {
  return location.hash.startsWith("#/spirits") || location.hash === "#/feed";
}
function activeRuns() {
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  return running.length + (spiritRuns.queued || []).length;
}
function ensureLivePoll() {
  if (livePollTimer || !pollScopeOpen()) return;
  livePollTimer = setInterval(livePoll, 3000);
  livePoll(); // immediate tick
}
function stopLivePoll() { if (livePollTimer) { clearInterval(livePollTimer); livePollTimer = null; } }

async function livePoll() {
  if (!pollScopeOpen()) { stopLivePoll(); return; }
  const firstPoll = !liveBaselined;
  spiritRuns = await fetchSpiritRuns();

  // detect running → terminal transitions for the run-finished toast
  let anyFinished = false;
  (spiritRuns.data || []).forEach((r) => {
    const was = runOutcomes[r.id];
    if (liveBaselined && r.outcome !== "running" && was === "running") {
      anyFinished = true;
      showToast(`${r.spirit}/${r.ritual} — ${r.outcome}` + (r.itemsWritten ? ` · ${r.itemsWritten} item${r.itemsWritten === 1 ? "" : "s"}` : ""),
        () => { location.hash = "#/spirits/runs"; setTimeout(() => openSpiritRun(r.id), 120); });
    }
    runOutcomes[r.id] = r.outcome;
  });
  liveBaselined = true;

  // re-render whatever is open, from files alone
  if (location.hash.startsWith("#/spirits") && spiritTabFromHash() === "runs") renderSpiritRuns();
  if (openRunId) refreshOpenRun(); // includes the finishing tick, so the report shows the terminal outcome

  if (anyFinished) {
    refreshFeedBadge();                               // nav-pill inbox count
    if (location.hash.startsWith("#/spirits")) loadSpiritsStatus();
    if (location.hash === "#/feed") loadFeed();       // new findings land in place
  }
  if (firstPoll || anyFinished) detectNewDigest();   // baseline on first look; then catch a landed digest
  if (activeRuns() === 0) stopLivePoll();            // nothing left to watch
}

async function detectNewDigest() {
  let items = [];
  try { items = (await (await fetch("/api/feed?status=inbox")).json()).items || []; } catch (e) { return; }
  diffDigests(items);
}

// diffDigests toasts once per newly-seen digest id. Also called from loadFeed
// itself, so entering FEED catches a digest that landed while no poll ran.
function diffDigests(items) {
  const digests = (items || []).filter((i) => i.type === "digest").map((i) => i.id);
  if (knownDigestIds === null) { knownDigestIds = new Set(digests); return; } // baseline
  digests.forEach((id) => {
    if (!knownDigestIds.has(id)) {
      knownDigestIds.add(id);
      showToast("New digest in the feed", () => { location.hash = "#/feed"; }, "digest");
    }
  });
}

// ---- FEED: manifest's one inbox (top-level tab, feed-central §1/§4) ----
// INBOX (default) = items awaiting a verdict (new + lapsed snoozes). Keep endorses
// and moves the item to KEPT. Chips are INBOX/KEPT/ALL.
const FEED_VIEWS = [["inbox", "INBOX"], ["kept", "KEPT"], ["all", "ALL"]];
const SIGNAL_CAP = 8; // most-overdue signals shown; the rest fold behind "N more"
let signalsExpanded = false;
let feedCache = { items: [], signals: [], proposals: [] };

function showFeed() {
  loadFeed();
  ensureLivePoll(); // a dig/ask spooled from here is watched without leaving the tab
}

async function loadFeed() {
  const view = state.feedView || "inbox";
  try {
    const d = await (await fetch("/api/feed?status=" + view)).json();
    feedCache = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [] };
    setBadge(els.feedNavBadge, d.badge || 0);
    if (view === "inbox") diffDigests(feedCache.items); // catch digests landed while unpolled
  } catch (e) { feedCache = { items: [], signals: [], proposals: [] }; }
  renderFeedFilters();
  renderFeed();
}

// refreshFeedBadge keeps the nav pill honest from anywhere (boot, route, verdicts,
// run-finish). Always async — the count can touch the contacts calendar cache.
async function refreshFeedBadge() {
  try {
    const d = await (await fetch("/api/feed/badge")).json();
    setBadge(els.feedNavBadge, d.count || 0);
  } catch (e) {}
}

function renderFeedFilters() {
  const host = els.feedFilters; host.innerHTML = "";
  const cur = state.feedView || "inbox";
  FEED_VIEWS.forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (cur === val ? " on" : ""), label);
    b.onclick = () => { state.feedView = val; loadFeed(); };
    host.appendChild(b);
  });
}
function renderFeed() {
  const host = els.feedList; host.innerHTML = "";
  const sigHost = els.feedSignals; sigHost.innerHTML = ""; // collapses when empty
  const view = state.feedView || "inbox";
  // signals lane: app-derived nudges, INBOX only, tight one-line chips. Never
  // under KEPT/ALL (conditions, not items). Capped so a long neglect backlog
  // doesn't bury the findings — the most-overdue lead, the rest fold away.
  if (view === "inbox" && feedCache.signals.length) {
    const total = feedCache.signals.length;
    sigHost.appendChild(el("div", "reading-strip-head", "Signals — " + total));
    const shown = signalsExpanded ? total : Math.min(SIGNAL_CAP, total);
    feedCache.signals.slice(0, shown).forEach((sg) => sigHost.appendChild(signalRow(sg)));
    if (total > SIGNAL_CAP) {
      const more = el("button", "signal-more", signalsExpanded ? "▴ show fewer" : `▾ ${total - SIGNAL_CAP} more`);
      more.onclick = () => { signalsExpanded = !signalsExpanded; renderFeed(); };
      sigHost.appendChild(more);
    }
  }
  // pinned lane: virtual tune-proposal cards (pending approvals) lead the inbox;
  // digests pin next via the items sort. Proposals are pointers, not items —
  // they never appear under KEPT/ALL.
  if (view === "inbox") feedCache.proposals.forEach((p) => host.appendChild(proposalCardEl(p)));
  if (!feedCache.items.length && !host.children.length) {
    host.appendChild(emptyRow(view === "inbox"
      ? "Inbox zero — nothing awaiting a verdict."
      : view === "kept" ? "Nothing kept yet." : "No feed items yet."));
    return;
  }
  feedCache.items.forEach((it) => host.appendChild(feedCard(it)));
}

// signalRow renders one app-signal: a quiet one-line chip (kind · entity · age)
// with Act (deep link) · Snooze · Dismiss. A rock signal can also go "→ today".
function signalRow(sg) {
  const row = el("div", "signal-row");
  const label = el("span", "signal-label cp-clickable", sg.label);
  label.onclick = () => { location.hash = sg.actHref; };
  row.append(label);
  const act = el("span", "signal-actions");
  act.append(
    pillLight("Act", () => { location.hash = sg.actHref; }),
    pillLight("Snooze", () => signalAction("/api/feed/signal/snooze", { id: sg.id, days: 7 })),
    pillLight("Dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash })),
  );
  row.append(act);
  return row;
}
async function signalAction(url, body) {
  try { await postJSON(url, body); } catch (e) {}
  loadFeed();
}

// proposalCardEl renders a virtual tune-proposal card (feed-central §4 lane 1 +
// ea-digest Part-2 amendment): summary + evidence, ONE affordance — review the
// diff in APPROVALS. Confirm/Reject there resolves the card by construction.
function proposalCardEl(p) {
  const card = el("div", "feed-card digest pinned proposal");
  const top = el("div", "feed-top");
  top.append(el("span", "pin-chip", "📌 pinned"));
  top.append(el("span", "type-chip type-proposal", "proposal"));
  top.append(el("span", "feed-title", p.title));
  card.append(top);
  card.append(el("div", "feed-why", "proposes a change to " + p.applyPath));
  if (p.body) { const b = el("pre", "feed-body"); b.textContent = p.body; card.append(b); }
  const meta = el("div", "feed-meta");
  meta.append(el("span", null, [p.agent, (p.created || "").slice(0, 10)].filter(Boolean).join("  ·  ")));
  card.append(meta);
  const actions = el("div", "feed-actions");
  actions.append(pillLight("review diff →", () => {
    pendingApprovalFocus = p.approvalId;
    location.hash = "#/spirits/approvals";
  }));
  card.append(actions);
  return card;
}
function faviconFor(link) {
  try {
    const host = new URL(link).hostname;
    const img = el("img", "feed-favicon");
    img.src = "https://www.google.com/s2/favicons?domain=" + encodeURIComponent(host) + "&sz=32";
    img.loading = "lazy";
    img.onerror = () => img.remove();
    return img;
  } catch (e) { return null; }
}
function feedCard(it) {
  if (it.type === "draft") return draftFeedCard(it);
  const pinned = it.type === "digest" && it.status === "new";
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : "") + (it.type === "digest" ? " digest" : "") +
    (pinned ? " pinned" : "") + (it.status === "discarded" ? " discarded" : ""));
  const top = el("div", "feed-top");
  if (pinned) top.append(el("span", "pin-chip", "📌 pinned"));
  top.append(el("span", "type-chip type-" + it.type, it.type));
  // only a real external URL makes the title a link; an artifact's local
  // `artifacts/library/…` reference opens in the note view via "view →" instead.
  const external = /^https?:\/\//i.test(it.link || "");
  let title;
  if (external) title = linkEl(it.title, it.link);
  else if (it.artifactPath) { title = el("span", "cp-clickable", it.title); title.onclick = () => openArtifact(it.artifactPath); }
  else title = el("span", null, it.title);
  title.classList.add("feed-title");
  top.append(title);
  if (it.confidence) top.append(el("span", "conf conf-" + it.confidence, it.confidence));
  card.append(top);
  // the why line is written to be the reason you care — lead with it, emphasized
  if (it.why) card.append(el("div", "feed-why", it.why));
  const meta = el("div", "feed-meta");
  const fav = external ? faviconFor(it.link) : null;
  if (fav) meta.append(fav);
  const bits = [it.source || it.domain, it.agent, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ");
  meta.append(el("span", null, bits));
  card.append(meta);
  if (it.body && (pinned || it.type === "artifact")) { const b = el("pre", "feed-body"); b.textContent = it.body; card.append(b); }
  if (it.vaultNote) card.append(el("div", "feed-saved", "✓ saved to " + it.vaultNote));
  const actions = el("div", "feed-actions");
  if (it.artifactPath) actions.append(pillLight("view →", () => openArtifact(it.artifactPath))); // the full brief
  if (it.status !== "discarded") {
    actions.append(pillLight("Keep", () => feedAction(it.id, { status: "kept" })));
    if (it.status !== "kept") actions.append(pillLight("Discard", () => feedAction(it.id, { status: "discarded" })));
    actions.append(pillLight("Snooze 7d", () => feedAction(it.id, { status: "snoozed", days: 7 })));
    if (!it.vaultNote) actions.append(pillLight("Save to vault", () => feedSaveToVault(it.id)));
    if (it.type !== "digest") actions.append(pillLight("dig →", () => feedDig(it.id))); // spool a deeper run
  } else {
    actions.append(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
  }
  card.append(actions);
  return card;
}

// draftFeedCard renders a Content Studio draft as a tweet-shaped card: the post
// text big, the critic's rationale, and inline approve / edit / dismiss plus a
// "judge" note. Approve confirms the linked append-x-queue approval; dismiss
// rejects it; edit rewrites both the draft and the pending bullet so the edited
// text is what lands.
function draftFeedCard(it) {
  const card = el("div", "feed-card draft" + (it.status === "discarded" ? " discarded" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-draft", "draft"));
  if (it.format && it.format !== "single") top.append(el("span", "draft-format", it.format));
  top.append(el("span", "feed-title", it.title || "draft"));
  card.append(top);

  const tweet = el("div", "draft-tweet");
  tweet.textContent = it.body || "";
  card.append(tweet);
  // quote-tweet variant: render the quoted post beneath (like X)
  if (it.quotedText) {
    const q = el("div", "draft-quote");
    q.append(el("div", "draft-quote-text", it.quotedText));
    if (it.quotedUrl) q.append(linkEl(it.quotedUrl, it.quotedUrl));
    card.append(q);
  }
  if (it.why) card.append(el("div", "feed-why", it.why));
  const meta = el("div", "feed-meta");
  meta.append(el("span", null, [it.agent, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ")));
  card.append(meta);

  if (it.status === "discarded") {
    const a = el("div", "feed-actions");
    a.append(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
    card.append(a);
    return card;
  }

  // edit box (hidden until "Edit")
  const editWrap = el("div", "draft-edit"); editWrap.hidden = true;
  const ta = el("textarea", "draft-edit-input"); ta.value = it.body || "";
  const editActions = el("div", "feed-actions");
  editActions.append(
    pill("Save edit", async () => {
      const t = ta.value.trim(); if (!t) return;
      await studioPost(`/api/studio/draft/${encodeURIComponent(it.draftId)}/edit`, { text: t, approvalId: it.approvalId });
      showToast("edit saved — approve to queue the edited version", null, "info");
      loadFeed();
    }),
    pillLight("Cancel", () => { editWrap.hidden = true; }),
  );
  editWrap.append(ta, editActions);

  // feedback: a single "judge" affordance (shared with the board cards)
  const fb = buildDraftFeedback(it.draftId, "");

  const actions = el("div", "feed-actions");
  actions.append(
    pill("Approve → queue", () => draftApproval(it.approvalId, "confirm")),
    pillLight("Edit", () => { editWrap.hidden = !editWrap.hidden; }),
    pillLight("Dismiss", () => draftApproval(it.approvalId, "reject")),
  );
  card.append(editWrap, fb, actions);
  return card;
}

async function draftApproval(approvalId, kind) {
  if (!approvalId) { showToast("this draft has no linked approval", null, "error"); return; }
  setSaveState("saving");
  const body = kind === "reject" ? { reason: "dismissed from studio" } : {};
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(approvalId)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  showToast(kind === "confirm" ? "queued to x posts.md ✓" : "dismissed", null, "info");
  loadFeed();
}

async function studioPost(path, body) {
  setSaveState("saving");
  try { const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); if (!r.ok) throw new Error(await r.text()); setSaveState("saved"); return await r.json().catch(() => ({})); }
  catch (e) { setSaveState("error"); showToast("Studio action failed: " + (e.message || e), null, "error"); throw e; }
}

// ---- CONTENT STUDIO tab (draft board + inspiration watchlist + runs strip) ----
let studioCache = { board: [], inspiration: [], xPostsFile: "x posts.md" };
let studioQueueCache = null;
const STUDIO_TABS = [["board", "BOARD"], ["queue", "QUEUE"], ["inspiration", "INSPIRATION"]];
function showStudio() {
  if (!state.studioTab) state.studioTab = "board";
  loadStudio();
}
async function loadStudio() {
  try {
    studioCache = await (await fetch("/api/studio")).json();
  } catch (e) { studioCache = { board: [], inspiration: [], xPostsFile: "x posts.md" }; }
  // runs strip: scribe/critic latest outcomes (a quiet "nothing today" ≠ a dead ritual)
  try {
    const runs = await (await fetch("/api/spirits/runs")).json();
    studioCache.runs = (runs.data || runs.runs || runs || []).filter((r) => r.spirit === "scribe" || r.spirit === "critic");
  } catch (e) { studioCache.runs = []; }
  // engine health + next-fire times (§4: a dead engine must look different from a quiet morning)
  try { studioCache.status = await (await fetch("/api/spirits/status")).json(); } catch (e) { studioCache.status = null; }
  try {
    const rits = await (await fetch("/api/spirits/rituals")).json();
    studioCache.nextFire = {};
    (rits.data || rits || []).forEach((rr) => { if (rr.spirit === "scribe" || rr.spirit === "critic") studioCache.nextFire[rr.spirit + "/" + rr.ritual] = rr.nextFire || rr.next || ""; });
  } catch (e) { studioCache.nextFire = {}; }
  // §9: tune proposals — what the system has learned, pending your review
  try {
    const ap = await (await fetch("/api/spirits/approvals")).json();
    studioCache.tuneApprovals = (ap.pending || []).filter((p) => p.ritual === "tune");
  } catch (e) { studioCache.tuneApprovals = []; }
  renderStudio();
}
// §7 commission box — free-text instruction (inline [[note]] refs + URLs); spools
// scribe/commission, then auto-spools the critic when the run lands.
function renderCommissionBox() {
  const wrap = el("div", "commission-box");
  wrap.append(el("div", "reading-strip-head", "Commission a post"));
  const ta = el("textarea", "commission-input");
  ta.placeholder = "reference [[a note]] and https://… — comb for my auxiliary thoughts on the subject and propose a post";
  const btn = pill("Commission →", () => {
    const t = ta.value.trim(); if (!t) return;
    studioPost("/api/studio/commission", { instruction: t }).then(() => {
      showToast("commissioned — scribe is drafting, the critic will audit", null, "info");
      ta.value = "";
      commissionAutoSpool();
    }).catch(() => {});
  });
  wrap.append(ta, btn);
  return wrap;
}
async function commissionAutoSpool() {
  // poll up to ~90s for the commission run to finish, then run the critic (§7)
  for (let i = 0; i < 30; i++) {
    await new Promise((r) => setTimeout(r, 3000));
    try {
      const runs = await (await fetch("/api/spirits/runs")).json();
      const list = runs.data || runs.runs || runs || [];
      const c = list.find((r) => r.spirit === "scribe" && r.ritual === "commission");
      if (c && c.outcome && c.outcome !== "running") {
        await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: "critic", ritual: "audit-drafts" }) });
        showToast("commission drafted — critic auditing now", null, "info");
        loadStudio();
        return;
      }
    } catch (e) {}
  }
}
function renderTuningPanel() {
  const wrap = el("div", "studio-tuning");
  wrap.append(el("div", "reading-strip-head", "What the system is learning — " + studioCache.tuneApprovals.length + " tune proposal" + (studioCache.tuneApprovals.length === 1 ? "" : "s") + " pending"));
  studioCache.tuneApprovals.forEach((p) => {
    const row = el("div", "tuning-row");
    row.append(el("span", "tuning-what", p.action));
    row.append(el("span", "feed-meta", [p.agent, p.applyPath].filter(Boolean).join(" · ")));
    row.append(pillLight("review →", () => { pendingApprovalFocus = p.id; location.hash = "#/spirits/approvals"; }));
    wrap.append(row);
  });
  return wrap;
}
async function studioRunNow(spirit, ritual) {
  try {
    const r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual }) });
    if (r.status === 409) { showToast(`${spirit}/${ritual} already running`, null, "info"); return; }
    if (!r.ok) throw new Error(await r.text());
    showToast(`${spirit}/${ritual} queued — the engine runs it within ~5s`, null, "info");
  } catch (e) { showToast("run-now failed: " + (e.message || e), null, "error"); }
}
function renderStudio() {
  // tabs
  els.studioTabs.innerHTML = "";
  STUDIO_TABS.forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (state.studioTab === val ? " on" : ""), label);
    b.onclick = () => { state.studioTab = val; if (val === "queue") studioQueueCache = null; renderStudio(); };
    els.studioTabs.append(b);
  });
  // runs strip: engine health + run-now + recent scribe/critic outcomes (§4)
  els.studioRuns.innerHTML = "";
  const st = studioCache.status;
  if (st && st.engineAlive === false) els.studioRuns.append(el("span", "studio-run-chip srun-down", "⚠ engine down"));
  else if (st) els.studioRuns.append(el("span", "studio-run-chip srun-up", "engine live"));
  [["scribe", "mine-and-draft"], ["critic", "audit-drafts"]].forEach(([sp, rit]) => {
    const b = el("button", "pill light", "▶ run " + sp);
    const nf = (studioCache.nextFire || {})[sp + "/" + rit];
    if (nf) b.title = "next auto-fire: " + nf;
    b.onclick = () => studioRunNow(sp, rit);
    els.studioRuns.append(b);
  });
  (studioCache.runs || []).slice(0, 3).forEach((r) => {
    const chip = el("span", "studio-run-chip");
    chip.append(el("span", "srun-name", r.spirit + "/" + r.ritual));
    chip.append(el("span", "srun-outcome outcome-" + (r.outcome || ""), r.outcome || "—"));
    chip.title = (r.summary || "") + "  ·  " + (r.started || r.finished || "");
    els.studioRuns.append(chip);
  });

  const host = els.studioBody; host.innerHTML = "";
  if (state.studioTab === "queue") return renderStudioQueue(host);
  if (state.studioTab === "inspiration") return renderStudioInspiration(host);
  // board: drafts grouped by the status vocabulary (draft → passed → queued → posted, + killed)
  let board = studioCache.board || [];
  if ((studioCache.tuneApprovals || []).length) host.append(renderTuningPanel());
  host.append(renderCommissionBox());
  if (!board.length) { host.append(emptyRow("No drafts yet — commission one above, or scribe drafts each morning.")); return; }
  // §5 format filter chips
  const fmt = state.studioFormat || "all";
  const fmtRow = el("div", "draft-chips");
  [["all", "all"], ["aphorism", "aphorisms"], ["single", "long-form"]].forEach(([v, label]) => {
    const b = el("button", "draft-chip" + (fmt === v ? " on" : ""), label);
    b.onclick = () => { state.studioFormat = v; renderStudio(); };
    fmtRow.append(b);
  });
  host.append(fmtRow);
  if (fmt !== "all") board = board.filter((d) => (d.format || "single") === fmt);
  // §4 group order + vocabulary
  const order = ["passed", "pending-audit", "queued", "posted", "killed"];
  const labels = { passed: "Passed — approve to queue", "pending-audit": "Pending audit", queued: "Queued", posted: "Posted", killed: "Killed" };
  const byStatus = {};
  board.forEach((d) => { (byStatus[d.status] = byStatus[d.status] || []).push(d); });
  order.forEach((st) => {
    const items = byStatus[st];
    if (!items || !items.length) return;
    const ap = items.filter((d) => (d.format || "single") === "aphorism").length;
    const sg = items.length - ap;
    const head = labels[st] + "  —  " + items.length + " (" + ap + " aphorism · " + sg + " long-form)";
    if (st === "killed") {
      const det = el("details", "killed-group");
      det.append(el("summary", "reading-strip-head", head));
      items.forEach((d) => det.append(studioBoardCard(d)));
      host.append(det);
    } else {
      host.append(el("div", "reading-strip-head", head));
      items.forEach((d) => host.append(studioBoardCard(d)));
    }
  });
}
// statusWord maps a draft's lifecycle status to the §4 vocabulary chip.
function statusWord(s) { return ({ "pending-audit": "draft" })[s] || s; }

// buildDraftFeedback is the shared feedback capture used on both feed and board
// cards: a single "judge" affordance that opens a commentary box; the note is
// written to the draft's feedback (steers the next scribe run + feeds tuning).
function buildDraftFeedback(draftId, existing) {
  const wrap = el("div", "draft-feedback");
  if (existing && existing.trim()) wrap.append(el("div", "draft-fb-text", "your note: " + existing.trim()));
  const judge = pillLight("judge", () => {
    if (wrap.querySelector(".draft-judge-box")) return; // toggle guard
    const box = el("div", "draft-judge-box");
    const inp = el("input", "draft-fb-input");
    inp.type = "text";
    inp.placeholder = "your note on this draft — what's off, or do more of…";
    const save = () => {
      const t = inp.value.trim();
      if (!t) { box.remove(); return; }
      studioPost(`/api/studio/draft/${encodeURIComponent(draftId)}/feedback`, { text: t, tags: [] })
        .then(() => { showToast("noted — the next run honors it", null, "info"); box.remove(); });
    };
    inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); save(); } });
    const saveBtn = pillLight("save", save);
    box.append(inp, saveBtn);
    wrap.append(box);
    inp.focus();
  });
  wrap.append(judge);
  return wrap;
}

function studioBoardCard(d) {
  const card = el("div", "feed-card draft status-" + d.status);
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-draft", statusWord(d.status)));
  if (d.score) top.append(el("span", "draft-score", d.score + "/10"));
  if (d.format && d.format !== "single") top.append(el("span", "draft-format", d.format));
  if (d.commissioned) top.append(el("span", "draft-format", "commissioned"));
  if (d.overruled) top.append(el("span", "draft-format", "overruled"));
  card.append(top);
  const tweet = el("div", "draft-tweet"); tweet.textContent = d.edited || d.text; card.append(tweet);
  if (d.edited) card.append(el("div", "feed-meta", "✎ edited (original preserved)"));
  if (d.seed) card.append(el("div", "feed-meta", "from your drafts: " + d.seed));
  if (d.scorecard) {
    const sc = el("details", "draft-scorecard");
    sc.append(el("summary", null, "scorecard"));
    const pre = el("pre", "feed-body"); pre.textContent = d.scorecard; sc.append(pre);
    card.append(sc);
  }
  if (d.postedUrl) card.append(el("div", "feed-saved", "✓ posted · " + d.postedUrl));

  // inline edit box (hidden until Edit)
  const editWrap = el("div", "draft-edit"); editWrap.hidden = true;
  const ta = el("textarea", "draft-edit-input"); ta.value = d.edited || d.text;
  const eActs = el("div", "feed-actions");
  eActs.append(
    pill("Save edit", async () => { await studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/edit`, { text: ta.value.trim(), approvalId: d.approvalId || "" }); showToast("edit saved", null, "info"); loadStudio(); }),
    pillLight("Cancel", () => { editWrap.hidden = true; }),
  );
  editWrap.append(ta, eActs);

  const actions = el("div", "feed-actions");
  let consumeCb = null;
  if (d.status === "passed") {
    if (d.seed) { const lbl = el("label", "seed-consume"); consumeCb = el("input"); consumeCb.type = "checkbox"; consumeCb.checked = true; lbl.append(consumeCb, document.createTextNode(" consume the seed from # drafts")); card.append(lbl); }
    actions.append(
      pill("Approve → queue", () => boardApprove(d, consumeCb)),
      pillLight("Edit", () => { editWrap.hidden = !editWrap.hidden; }),
      pillLight("Dismiss", () => d.approvalId ? draftApproval(d.approvalId, "reject") : showToast("no linked approval", null, "error")),
    );
  } else if (d.status === "killed") {
    actions.append(pillLight("Overrule → queue", () => studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/overrule`, {}).then(() => { showToast("queued (overruled) ✓ — teaches the critic", null, "info"); loadStudio(); })));
  } else if (d.status === "queued") {
    actions.append(pillLight("mark posted", () => askText("Mark posted", "paste the tweet URL (optional)", (url) => studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/mark-posted`, { url: url.trim() }).then(loadStudio))));
  }
  card.append(editWrap, buildDraftFeedback(d.id, d.feedback), actions);
  return card;
}

async function boardApprove(d, consumeCb) {
  if (!d.approvalId) { showToast("no linked approval — overrule or queue from the feed", null, "error"); return; }
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(d.approvalId)}/confirm`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }); }
  catch (e) { showToast("approve failed", null, "error"); return; }
  if (d.seed && consumeCb && consumeCb.checked) { try { await studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/consume-seed`, {}); } catch (e) {} }
  showToast("queued ✓ — view Queue", () => { state.studioTab = "queue"; studioQueueCache = null; renderStudio(); }, "info");
  loadStudio();
}
function renderStudioInspiration(host) {
  host.append(el("div", "studio-purpose", "Accounts you study. Your commentary and saved posts teach the pattern skill what you admire."));
  // add an account
  const addWrap = el("div", "insp-add");
  const inp = el("input", "queue-add-input"); inp.type = "text"; inp.placeholder = "add an account by handle (e.g. paulg)…";
  const add = () => { const h = inp.value.trim().replace(/^@/, ""); if (!h) return; studioPost("/api/studio/account/add", { handle: h }).then(() => { showToast("@" + h + " queued — the engine backfills it shortly", null, "info"); inp.value = ""; }); };
  inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  addWrap.append(inp, pillLight("add account", add));
  host.append(addWrap);

  const accts = studioCache.inspiration || [];
  if (!accts.length) { host.append(emptyRow("No accounts yet — add one above.")); return; }
  accts.filter((a) => a.isSelf).forEach((a) => host.append(inspAccountCard(a, true)));
  accts.filter((a) => !a.isSelf).forEach((a) => host.append(inspAccountCard(a, false)));
}
function inspAccountCard(a, isSelf) {
  const card = el("div", "feed-card" + (isSelf ? " insp-self" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "feed-title", "@" + a.handle));
  if (isSelf) top.append(el("span", "type-chip type-draft", "your account"));
  if (a.followers) top.append(el("span", "feed-meta", fmtCount(a.followers) + " followers"));
  if (!isSelf) { const b = el("button", "pill light", "this is me"); b.onclick = () => studioPost(`/api/studio/account/${encodeURIComponent(a.handle)}/self`, { on: true }).then(() => { showToast("marked as your account", null, "info"); loadStudio(); }); top.append(b); }
  card.append(top);
  if (a.bio) card.append(el("div", "feed-why", a.bio));
  // editable commentary (not for the self account — it's not a pattern to admire)
  if (!isSelf) {
    const cw = el("div", "insp-commentary");
    cw.append(el("div", "feed-meta", "your commentary — what you admire about this account:"));
    const ta = el("textarea", "insp-comment-input"); ta.value = a.commentary || ""; ta.placeholder = "e.g. his zoom-out QTs land because…";
    const save = pillLight("save", () => studioPost(`/api/studio/account/${encodeURIComponent(a.handle)}/commentary`, { text: ta.value }).then(() => showToast("commentary saved", null, "info")));
    cw.append(ta, save);
    card.append(cw);
  }
  // top posts collapsed by default (declutter) — expand to view/annotate
  const posts = a.topPosts || [];
  if (posts.length) {
    const det = el("details", "insp-posts");
    det.append(el("summary", "insp-posts-summary", "top posts by views (" + posts.length + ")"));
    posts.forEach((p) => {
      const row = el("div", "insp-post");
      row.append(el("div", "insp-post-text", p.text));
      const m = el("div", "feed-meta insp-post-meta");
      m.append(el("span", null, fmtCount(p.views) + " views · " + fmtCount(p.likes) + " likes"));
      if (p.url) m.append(linkEl("open →", p.url));
      m.append(pillLight("annotate", () => askText("Annotate this post", "why it's worth studying — teaches the pattern skill what you admire", (note) => studioPost("/api/studio/annotate", { postId: p.id, note: note.trim() }).then(() => showToast("annotated", null, "info")))));
      row.append(m);
      det.append(row);
    });
    card.append(det);
  }
  return card;
}
function fmtCount(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, "") + "K";
  return String(n);
}

// ---- Queue tab: live-editable x posts.md (§1/§3) ----
async function loadStudioQueue() {
  try { studioQueueCache = await (await fetch("/api/studio/queue")).json(); }
  catch (e) { studioQueueCache = { sections: { drafts: [], queue: [], posted: [] } }; }
  renderStudio();
}
function renderStudioQueue(host) {
  if (!studioQueueCache) { host.append(emptyRow("loading…")); loadStudioQueue(); return; }
  const q = studioQueueCache;
  const sec = q.sections || { drafts: [], queue: [], posted: [] };
  if (sec.needsMigration) {
    const banner = el("div", "studio-migrate");
    banner.append(el("div", "studio-migrate-msg", "Your x posts.md still uses the old single # queue. Restructure it into # drafts (your scratch ideas) / # queue (ready to post) / # posted — your current bullets move to # drafts, nothing is lost."));
    banner.append(pill("Restructure now", async () => {
      await studioPost("/api/studio/migrate", {});
      showToast("x posts.md restructured ✓", null, "info");
      studioQueueCache = null; loadStudioQueue();
    }));
    host.append(banner);
  }
  const sections = [["drafts", "# drafts", "scratch ideas — the scribe may develop these"], ["queue", "# queue", "approved, ready to post"], ["posted", "# posted", "posted"]];
  sections.forEach(([key, label, hint]) => {
    const bullets = sec[key] || [];
    host.append(el("div", "reading-strip-head", label + " — " + bullets.length + "  ·  " + hint));
    bullets.forEach((b) => host.append(studioBulletRow(key, b)));
    if (key !== "posted") host.append(studioAddRow(key));
  });
}
function studioBulletRow(section, bullet) {
  const row = el("div", "queue-bullet");
  const editable = section !== "posted";
  const textWrap = el("div", "queue-bullet-text");
  textWrap.textContent = bullet.text.replace(/^- /, "");
  if (editable) {
    textWrap.classList.add("cp-clickable");
    textWrap.onclick = () => beginBulletEdit(row, section, bullet);
  }
  row.append(textWrap);
  const acts = el("div", "queue-bullet-acts");
  if (section === "queue") acts.append(pillLight("mark posted", () => {
    askText("Mark posted", "paste the tweet URL (optional)", (url) =>
      studioPost("/api/studio/queue/mark-posted", { bullet: bullet.text, url: url.trim() }).then(() => { studioQueueCache = null; loadStudioQueue(); }));
  }));
  if (editable) acts.append(pillLight("delete", () => {
    if (!confirm("Delete this bullet?")) return;
    studioPost("/api/studio/bullet/delete", { section, bullet: bullet.text }).then(() => { studioQueueCache = null; loadStudioQueue(); });
  }));
  row.append(acts);
  return row;
}
function beginBulletEdit(row, section, bullet) {
  row.innerHTML = "";
  const ta = el("textarea", "queue-edit-input"); ta.value = bullet.text.replace(/^- /, "");
  const acts = el("div", "feed-actions");
  acts.append(
    pill("Save", () => studioPost("/api/studio/bullet/edit", { section, original: bullet.text, replacement: "- " + ta.value.trim() })
      .then(() => { studioQueueCache = null; loadStudioQueue(); })
      .catch(() => { studioQueueCache = null; loadStudioQueue(); })),
    pillLight("Cancel", () => { studioQueueCache = null; loadStudioQueue(); }),
  );
  row.append(ta, acts);
  ta.focus();
}
function studioAddRow(section) {
  const row = el("div", "queue-add");
  const inp = el("input", "queue-add-input"); inp.type = "text"; inp.placeholder = "+ add a " + section.replace(/s$/, "") + " bullet…";
  const add = () => { const v = inp.value.trim(); if (!v) return; studioPost("/api/studio/bullet/add", { section, bullet: "- " + v }).then(() => { studioQueueCache = null; loadStudioQueue(); }); };
  inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  row.append(inp, pillLight("add", add));
  return row;
}

// openArtifact opens an artifact's library file in the universal note view (the
// excalibur tree is inside the vault, so it renders like any note), returning to
// the feed on back.
function openArtifact(path) {
  _noteReturn = "#/feed";
  openNoteByPath(path);
}

// feedDig: "dig →" — spool a deeper run for the originating spirit; findings
// come back as new inbox items. Never navigates away from the feed.
async function feedDig(id) {
  let r;
  try { r = await fetch(`/api/feed/${encodeURIComponent(id)}/dig`, { method: "POST" }); }
  catch (e) { showToast("Dig failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    const d = await r.json().catch(() => ({}));
    showToast(`${d.spirit || "spirit"}/${d.ritual || "ritual"} is already running — view`, () => { location.hash = "#/spirits/runs"; }, "info");
    return;
  }
  if (!r.ok) { showToast("Dig failed: " + ((await r.text()) || r.status), null, "error"); return; }
  const d = await r.json().catch(() => ({}));
  showToast(`${d.spirit}/${d.ritual} queued — view`, () => { location.hash = "#/spirits/runs"; }, "info");
  ensureLivePoll(); // watch it land back in the inbox
}
async function feedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/feed/${encodeURIComponent(id)}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed(); // re-renders + refreshes the badge from the same response
}
async function feedSaveToVault(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(id)}/save-to-vault`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "save failed");
    setSaveState("saved");
  } catch (e) { setSaveState("error"); showToast("Save to vault failed: " + e.message, null, "error"); }
  loadFeed();
}

// ---- run now / ask a scout (spooled request; engine picks it up within ~5s) ----
// spiritPick opens the spirit/ritual picker (one area per spirit, its rituals
// as items) and calls onPick("spirit","ritual"). askRitual, when given, is
// picked automatically if present so "Ask a scout" lands on options-scout's
// research ritual without a needless second tap.
async function spiritPick(onPick) {
  // the catalog can be needed before SPIRITS was ever opened (Ask-a-scout lives
  // in FEED now) — load it lazily.
  if (!spiritStatusCache) await loadSpiritsStatusCacheOnly();
  const spirits = (spiritStatusCache || {}).spirits || {};
  const groups = Object.keys(spirits).sort().map((sp) => ({
    area: sp,
    items: (spirits[sp] || []).map((rit) => ({ id: sp + "/" + rit, text: rit })),
  })).filter((g) => g.items.length);
  if (!groups.length) { showToast("No spirit/ritual found in the excalibur tree.", null, "error"); return; }
  openPicker("Run a ritual now", groups, (id) => {
    const [sp, rit] = id.split("/");
    onPick(sp, rit);
  }, "No rituals found.");
}
async function loadSpiritsStatusCacheOnly() {
  try { spiritStatusCache = await (await fetch("/api/spirits/status")).json(); } catch (e) {}
}
// spiritSpool drops a run request. It holds NO button state — the run's status
// lives in the files (queued spool → running report). A 409 means the same
// spirit/ritual is already active (the double-spool guard). From FEED the user
// is never yanked away (feed-central §3: the loop closes in the feed) — a toast
// links to the live row instead; from SPIRITS we jump to RUNS as before.
async function spiritSpool(spirit, ritual, request) {
  const onFeed = location.hash === "#/feed";
  let r;
  try { r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual, request: request || "" }) }); }
  catch (e) { showToast("Run request failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    showToast(`${spirit}/${ritual} is already running — view`, () => { location.hash = "#/spirits/runs"; }, "info");
    if (!onFeed) location.hash = "#/spirits/runs";
    return;
  }
  if (!r.ok) { showToast("Run request failed (" + r.status + ")", null, "error"); return; }
  if (onFeed) {
    showToast(`${spirit}/${ritual} queued — view`, () => { location.hash = "#/spirits/runs"; }, "info");
  } else {
    location.hash = "#/spirits/runs";
    loadSpiritRuns(); // show the queued row immediately
  }
  ensureLivePoll();   // and watch it through to done
}
function spiritRunNow() {
  spiritPick((sp, rit) => spiritSpool(sp, rit, ""));
}
// spiritAskScout: pick a spirit/ritual, then take a free-form request via an
// inline box (no browser prompt). The request rides the spool into the prompt.
function spiritAskScout() {
  spiritPick((sp, rit) => {
    askText(`Request for ${sp} / ${rit}`,
      'e.g. "buy a mechanical keyboard under $200 — find 5 options"',
      (request) => { if (request.trim()) spiritSpool(sp, rit, request.trim()); });
  });
}
async function fetchSpiritRuns() {
  try {
    const d = await (await fetch("/api/spirits/runs")).json();
    return { data: d.data || [], queued: d.queued || [] };
  } catch (e) { return { data: [], queued: [] }; }
}

// askText — a small inline text dialog (reuses the picker modal chrome), the
// replacement for prompt() in spirits flows (plan §6).
function askText(title, placeholder, onSubmit) {
  els.pickerTitle.textContent = title;
  const body = els.pickerBody; body.innerHTML = "";
  const ta = el("textarea", "asktext-area"); ta.placeholder = placeholder; ta.rows = 3;
  const actions = el("div", "asktext-actions");
  const submit = pill("Send →", () => { closePicker(); onSubmit(ta.value); });
  actions.append(el("span", "asktext-hint", "⌘↵ to send"), submit);
  body.append(ta, actions);
  ta.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); closePicker(); onSubmit(ta.value); }
    else if (e.key === "Escape") { e.preventDefault(); closePicker(); }
  });
  els.pickerModal.hidden = false;
  ta.focus();
}

// ---- run reports (artifacts/runs/) — live strip + finished list ----
async function loadSpiritRuns() {
  spiritRuns = await fetchSpiritRuns();
  renderSpiritRuns();
  ensureLivePoll();
}
// Re-renders the LIST only; never touches the open report detail (so a live
// re-render doesn't close what you're reading).
function renderSpiritRuns() {
  const host = els.spiritRunsList; host.innerHTML = "";
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  const queued = spiritRuns.queued || [];
  const finished = (spiritRuns.data || []).filter((r) => r.outcome !== "running");

  if (running.length || queued.length) {
    const strip = el("div", "live-strip");
    strip.append(el("div", "live-strip-label", "LIVE"));
    running.forEach((r) => strip.append(liveRunRow(r, true)));
    queued.forEach((q) => strip.append(liveRunRow(q, false)));
    host.append(strip);
  }
  if (!finished.length && !running.length && !queued.length) {
    host.appendChild(emptyRow("No runs yet — cast a skill (press /) or wait for a scheduled ritual."));
    return;
  }
  finished.forEach((r) => host.append(spiritRunCard(r)));
}
function liveRunRow(item, running) {
  const row = el("div", "live-row " + (running ? "running" : "queued"));
  const head = el("div", "live-head");
  head.append(el("span", "live-dot " + (running ? "on" : "wait")));
  head.append(el("span", "run-title", `${item.spirit} / ${item.ritual}`));
  head.append(el("span", "live-state", running ? "running" : "queued"));
  if (running) head.append(el("span", "live-elapsed", elapsedSince(item.started)));
  row.append(head);
  if (item.request) row.append(el("div", "feed-why", "“" + item.request + "”"));
  if (running) {
    const pct = item.ceilingUsd > 0 ? Math.min(100, Math.round((item.spentUsd / item.ceilingUsd) * 100)) : 0;
    const bar = el("div", "charge-bar"); const fill = el("div", "charge-fill" + (pct >= 100 ? " over" : "")); fill.style.width = pct + "%"; bar.append(fill);
    const cr = el("div", "charge-row"); cr.append(bar, el("span", "charge-label", `$${(item.spentUsd || 0).toFixed(4)} / $${(item.ceilingUsd || 0).toFixed(2)}`));
    row.append(cr);
    row.append(el("div", "feed-meta", `${item.steps || 0} step${item.steps === 1 ? "" : "s"} so far · click to watch the report append`));
    row.onclick = () => openSpiritRun(item.id);
  } else {
    row.append(el("div", "feed-meta", "waiting for the engine to pick it up…"));
  }
  return row;
}
function elapsedSince(iso) {
  const d = new Date(iso); if (isNaN(d)) return "";
  let s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  const m = Math.floor(s / 60); s = s % 60;
  return m ? `${m}m ${s}s` : `${s}s`;
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
  openRunId = id;
  let run;
  try { run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(id))).json(); }
  catch (e) { return; }
  const host = els.spiritRunDetail; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "run-detail-head");
  head.append(el("span", "run-title", id));
  const promptBtn = pillLight("Show assembled prompt", () => toggleSpiritPrompts(id, promptBtn));
  const closeBtn = pillLight("✕ Close", () => { host.hidden = true; openRunId = null; });
  head.append(promptBtn, closeBtn);
  host.append(head);
  const body = el("pre", "run-report"); body.id = "runReportBody";
  body.textContent = run.body || "";
  host.append(body);
  const prompts = el("div", "run-prompts"); prompts.id = "runPrompts-" + id; prompts.hidden = true;
  host.append(prompts);
  host.scrollIntoView({ behavior: "smooth", block: "start" });
  ensureLivePoll(); // a running report will keep appending
}
// Refresh the open report body in place. Called each poll tick (which only runs
// while a run is active), so the finishing tick pulls the terminal report too.
async function refreshOpenRun() {
  if (!openRunId) return;
  try {
    const run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(openRunId))).json();
    const body = document.getElementById("runReportBody");
    if (body) body.textContent = run.body || "";
  } catch (e) {}
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
let pendingApprovalFocus = null; // approval id to scroll to (set by a proposal card's "review diff →")

async function loadSpiritApprovals() {
  let d = { pending: [], counts: {} };
  try { d = await (await fetch("/api/spirits/approvals")).json(); } catch (e) {}
  const host = els.spiritApprovalList; host.innerHTML = "";
  const pending = d.pending || [];
  setSpiritApprovalBadge((d.counts && d.counts.pending) || 0);
  if (!pending.length) { host.appendChild(emptyRow("Nothing pending — warden findings and future EA proposals land here.")); return; }
  pending.forEach((a) => host.appendChild(spiritApprovalCard(a)));
  if (pendingApprovalFocus) { // deep-linked from a FEED proposal card
    const target = host.querySelector(`[data-approval-id="${CSS.escape(pendingApprovalFocus)}"]`);
    pendingApprovalFocus = null;
    if (target) {
      target.scrollIntoView({ behavior: "smooth", block: "start" });
      target.classList.add("goal-flash");
      setTimeout(() => target.classList.remove("goal-flash"), 2400);
    }
  }
}
function spiritApprovalCard(a) {
  const card = el("div", "approval-card");
  card.dataset.approvalId = a.id;
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
  const isNewNote = a.type === "create-vault-note";
  const isXQueue = a.type === "append-x-queue";
  const isSkill = a.type === "update-vault-skill";
  let attendees = null; // create-vault-note: the editable people list sent on Confirm
  if (actionable) {
    card.classList.add("actionable");
    const chip = el("div", "appr-apply");
    chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
    card.append(chip);

    if (!a.allowed) {
      blocked = true;
      blockMsg = isNewNote
        ? "apply-path is not a vault-root dated note (YYYY-MM-DD <title>.md) — Confirm is disabled."
        : isXQueue
        ? "apply-path is not the x-posts file — Confirm is disabled."
        : isSkill
        ? "update-vault-skill must target skills/x-content/{SKILL.md, references/<name>.md} and be filed by a tune ritual — Confirm is disabled."
        : "apply-path is outside the allow-list (spirits/*/cornerstone.md, spirits/*/rituals/*.md, chargebook.md) — Confirm is disabled.";
    } else if (/\/cornerstone\.md$/.test(a.applyPath) && frontmatterOf(a.current || "") !== frontmatterOf(a.proposed || "")) {
      // client-side mirror of the server's cornerstone-frontmatter guard
      blocked = true;
      blockMsg = "proposed content changes the cornerstone frontmatter — Confirm will refuse (behavior prose only).";
    }

    // People editor: seed from the auto-linked attendees, let the user fix them.
    if (isNewNote) {
      attendees = parseAttendees(a.proposed || "");
      card.append(buildAttendeeEditor(attendees));
    }

    if (isXQueue) {
      // append-x-queue's proposed is ONLY the bullet — show it, not a whole-file diff
      card.append(el("div", "appr-diff-label", "Appends under # queue in " + a.applyPath));
      const pre = el("pre", "appr-body draft-tweet"); pre.textContent = (a.proposed || "").trim(); card.append(pre);
    } else {
      card.append(el("div", "appr-diff-label", isNewNote ? "New note — will be created at the vault root"
        : isSkill ? "Skill change  ·  current → proposed" : "Proposed change  ·  current → proposed"));
      const diff = renderLineDiff(a.current || "", a.proposed || "");
      card.append(collapsibleBlock(diff, diff.childElementCount));
    }
  }
  if (blocked && blockMsg) card.append(el("div", "appr-blocked", "⚠ " + blockMsg));

  const actions = el("div", "appr-actions");
  const confirmBtn = pill(actionable ? "Confirm & apply" : "Confirm",
    () => spiritApprovalAct(a.id, "confirm", isNewNote ? attendees : null));
  if (blocked) { confirmBtn.disabled = true; confirmBtn.classList.add("disabled"); }
  actions.append(confirmBtn, pillLight("Reject", () => spiritApprovalAct(a.id, "reject")));
  card.append(actions);
  return card;
}

// parseAttendees pulls the [[wikilink]] names from a converted note's attendee
// line (between the frontmatter and "## Transcript").
function parseAttendees(proposed) {
  const m = proposed.match(/^---\n[\s\S]*?\n---\n([\s\S]*?)##\s*Transcript/);
  const head = m ? m[1] : "";
  const names = [];
  const re = /\[\[([^\]]+)\]\]/g;
  let x;
  while ((x = re.exec(head))) names.push(x[1].trim());
  return names;
}

// buildAttendeeEditor renders the people-involved chips + an add box, mutating
// the shared `attendees` array in place so Confirm sends the edited list.
function buildAttendeeEditor(attendees) {
  const wrap = el("div", "appr-attendees");
  wrap.append(el("div", "appr-attendees-label", "People involved — remove or add before confirming"));
  const chips = el("div", "attendee-chips");
  const renderChips = () => {
    chips.innerHTML = "";
    attendees.forEach((name, i) => {
      const c = el("span", "attendee-chip");
      c.append(el("span", "attendee-name", name));
      const x = el("button", "attendee-remove", "✕");
      x.title = "Remove";
      x.onclick = () => { attendees.splice(i, 1); renderChips(); };
      c.append(x);
      chips.append(c);
    });
    if (!attendees.length) chips.append(el("span", "attendee-empty", "none linked"));
  };
  const addRow = el("div", "attendee-add");
  const input = el("input", "attendee-input");
  input.type = "text";
  input.placeholder = "Add a person…  (type [[ to search your vault)";
  attachWikilinkAutocomplete(input); // reuse the vault-aware [[name]] autocomplete
  const commit = () => {
    const v = input.value.trim().replace(/^\[\[/, "").replace(/\]\]$/, "").trim();
    if (v && !attendees.some((n) => n.toLowerCase() === v.toLowerCase())) {
      attendees.push(v);
      renderChips();
    }
    input.value = "";
  };
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); commit(); }
  });
  const addBtn = el("button", "attendee-add-btn", "+ add");
  addBtn.onclick = commit;
  addRow.append(input, addBtn);
  wrap.append(chips, addRow);
  renderChips();
  return wrap;
}

// collapsibleBlock caps a long proposed-note block to a preview, with a toggle
// to expand. Short blocks are returned as-is.
const APPROVAL_COLLAPSE_LINES = 14;
function collapsibleBlock(inner, lineCount) {
  if (lineCount <= APPROVAL_COLLAPSE_LINES) return inner;
  const wrap = el("div", "appr-collapse collapsed");
  wrap.append(inner);
  const toggle = el("button", "appr-expand", `Show full note (${lineCount} lines) ▾`);
  toggle.onclick = () => {
    const collapsed = wrap.classList.toggle("collapsed");
    toggle.textContent = collapsed ? `Show full note (${lineCount} lines) ▾` : "Collapse ▴";
  };
  wrap.append(toggle);
  return wrap;
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
function spiritApprovalAct(id, kind, attendees) {
  if (kind === "reject") {
    // inline reason box (no browser prompt); Escape cancels
    askText("Reject — reason (optional)",
      "recorded on the proposal; for warden findings this becomes an accepted exception",
      (reason) => postApprovalDecision(id, "reject", { reason: reason.trim() || "rejected from dashboard" }));
    return;
  }
  const body = (kind === "confirm" && attendees !== null && attendees !== undefined)
    ? { editAttendees: true, attendees } // create-vault-note with the edited people list
    : {};
  postApprovalDecision(id, kind, body);
}
async function postApprovalDecision(id, kind, body) {
  setSaveState("saving");
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(id)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadSpiritApprovals();
  loadSpiritsStatus();
}
function setSpiritApprovalBadge(n) {
  if (!els.spiritApprBadge) return;
  els.spiritApprBadge.hidden = !n;
  els.spiritApprBadge.textContent = n || "";
}
async function refreshSpiritApprovalBadge() {
  try { const d = await (await fetch("/api/spirits/approvals")).json(); setSpiritApprovalBadge((d.counts && d.counts.pending) || 0); } catch (e) {}
}

if (els.feedRunNowBtn) els.feedRunNowBtn.addEventListener("click", spiritRunNow);
if (els.feedAskBtn) els.feedAskBtn.addEventListener("click", spiritAskScout);

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
  // cadence: human phrase primary; raw cron demoted to a tooltip. On-demand
  // rows say how to run them; "custom" carries the raw string in the tooltip.
  const cad = el("span", "ritual-cadence");
  if (r.cadence === "") {
    cad.append(el("span", "cad-human", "on-demand"));
    cad.append(el("span", "cad-hint", " · run with /"));
  } else {
    const h = el("span", "cad-human", r.cadenceHuman || "custom");
    if (r.cadence) h.title = r.cadence; // raw cron on hover only
    cad.append(h);
  }
  row.append(cad);
  // next fire — relative + absolute ("in 2h · 1:00p")
  const next = el("span", "ritual-next");
  if (r.valid && r.nextFire) {
    next.append(el("span", "next-rel", relPhrase(r.nextFire)));
    next.append(el("span", "next-abs", " · " + fmtWhen(r.nextFire)));
  } else {
    next.textContent = "—";
  }
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
// relFuture: " · in 9h" / " · in 3d" / " · due"
function relFuture(iso) {
  const p = relPhrase(iso);
  return p ? " · " + p : "";
}
// relPhrase: "in 9h" / "in 3d" / "due now"
function relPhrase(iso) {
  const d = new Date(iso), ms = d - new Date();
  if (isNaN(d)) return "";
  if (ms <= 0) return "due now";
  const m = Math.round(ms / 60000);
  if (m < 60) return "in " + m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return "in " + h + "h";
  return "in " + Math.round(h / 24) + "d";
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

function newRitual(sp) {
  askText(`New ritual for ${sp}`, 'lowercase name, e.g. "weekly-review"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/ritual", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: sp, name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      const { path } = await r.json();
      await loadSpiritRituals();
      openEditor([path]);
    } catch (e) { showToast("Couldn't create ritual: " + (e.message || e), null, "error"); }
  });
}
function newSpirit() {
  askText("New spirit", 'lowercase name, e.g. "news-scout"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/spirit", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      const { path } = await r.json();
      await loadSpiritRituals();
      loadSpiritsStatus();
      openEditor([`spirits/${name.trim()}/identity.md`, path], 1);
    } catch (e) { showToast("Couldn't create spirit: " + (e.message || e), null, "error"); }
  });
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

// postJSONOk throws on a non-2xx response so callers can signal real failures
// (postJSON swallows them, which hid write errors behind an optimistic UI).
async function postJSONOk(url, body) {
  const res = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
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
  loadContactEmailReview();
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
  // "met" = calendar-verified (email-matched); "mentioned" = note-based. Distinct
  // signals: met is headlined when present; the going-cold line names its basis.
  if (c.cold && c.daysSince >= 0) {
    const verb = c.neglectBasis === "meetings" ? "met" : "mentioned";
    right.append(el("span", "contact-meta", verb + " " + c.daysSince + "d ago (usually every " + c.medianGap + "d)"));
  } else if (c.lastMet) {
    right.append(el("span", "contact-meta", "met " + c.lastMet));
    if (c.lastMentioned && c.lastMentioned !== c.lastMet) right.append(el("span", "contact-submeta", "mentioned " + c.lastMentioned));
  } else if (c.lastMentioned) {
    right.append(el("span", "contact-meta muted", "mentioned " + c.lastMentioned));
  }
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

// ---- email-linking review queue (§4) — mirrors the triage strip ----
async function loadContactEmailReview() {
  let d = { candidates: [] };
  try { d = await (await fetch("/api/contacts/email-review")).json(); } catch (e) {}
  renderEmailReview(d.candidates || []);
}

let _emailReviewOpen = false; // preserve expand/collapse across in-place updates

function renderEmailReview(items) {
  const host = els.contactEmailReview; if (!host) return;
  host.innerHTML = "";
  if (!items.length) { host.hidden = true; return; }
  host.hidden = false;
  const head = el("div", "triage-head");
  const label = el("span", "triage-label", "");
  const rows = el("div", "triage-rows");
  const toggle = pillLight("", () => setOpen(!_emailReviewOpen));
  const setOpen = (open) => {
    _emailReviewOpen = open;
    rows.hidden = !open;
    toggle.textContent = open ? "Hide ▴" : "Review ▾";
  };
  const setCount = (n) => {
    label.textContent = "Review — " + n + " unlinked email" + (n === 1 ? "" : "s") + " (link calendar attendees to contacts)";
    if (n === 0) host.hidden = true; // last one linked/dismissed → strip disappears
  };
  // ctx lets a row remove itself and update the count WITHOUT re-rendering the whole
  // strip (which would collapse it and lose the user's place).
  const ctx = { remove: (row) => { row.remove(); setCount(rows.children.length); } };
  const headActions = el("span", "triage-head-actions"); headActions.append(toggle);
  head.append(label, headActions);
  items.forEach((c) => rows.append(emailReviewRow(c, ctx)));
  host.append(head, rows);
  setCount(rows.children.length);
  setOpen(_emailReviewOpen);
}

function emailReviewRow(c, ctx) {
  const r = el("div", "triage-row");
  const who = el("span", "triage-name", c.attendeeName || c.email);
  who.append(el("span", "triage-hint", " " + c.email));
  r.append(who, el("span", "er-arrow", "→"), el("span", "er-target", c.contactDisplay));
  r.lastChild.append(el("span", "triage-hint", c.via === "email" ? " email match" : " name match"));
  const flash = el("span", "er-flash"); flash.hidden = true;
  const act = el("span", "triage-actions");
  const link = pill("Link", () => doLink(c.contactKey, c.contactDisplay, c.email));
  const dismiss = pillLight("Dismiss", async () => {
    dismiss.disabled = true;
    try { await postJSONOk("/api/contacts/email-dismiss", { email: c.email, key: c.contactKey }); }
    catch (e) { dismiss.disabled = false; showFlash(flash, "✕ " + errMsg(e), true); return; }
    ctx.remove(r);
  });
  // link the email to `key`, then signal + fade the row out in place (strip stays open)
  async function doLink(key, display, email) {
    link.disabled = true; showFlash(flash, "linking…", false);
    try { await postJSONOk("/api/contacts/email", { key, display, email }); }
    catch (e) { link.disabled = false; showFlash(flash, "✕ " + errMsg(e), true); return; }
    showFlash(flash, "✓ linked " + email + " → " + display, false);
    r.classList.add("er-done");
    loadContactList(); // the contact's list row now shows a calendar "met" date
    setTimeout(() => ctx.remove(r), 1000);
  }
  act.append(link, pillLight("Different contact", () => openEmailReassign(r, c, doLink)), dismiss);
  r.append(act, flash);
  return r;
}

function showFlash(node, msg, isError) {
  node.textContent = msg; node.hidden = false;
  node.classList.toggle("error", !!isError);
}
function errMsg(e) { return (e && e.message) ? e.message : "failed"; }

// openEmailReassign lets the user link this email to a DIFFERENT contact than the
// suggested one (inline search — same shape as the create-contact search).
function openEmailReassign(row, c, doLink) {
  if (row.querySelector(".er-search")) return;
  const box = el("div", "er-search");
  const input = el("input", "contact-create-input"); input.type = "text";
  input.placeholder = "Link " + c.email + " to another contact…";
  const results = el("div", "contact-create-results");
  box.append(input, results);
  row.append(box);
  input.focus();
  let timer;
  input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      results.innerHTML = "";
      const q = input.value.trim();
      if (!q) return;
      let d = { results: [] };
      try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
      (d.results || []).forEach((rf) => {
        const rr = el("div", "cc-result");
        rr.append(el("span", "cc-name", rf.display));
        rr.append(pill("Link here", () => { box.remove(); doLink(rf.key, rf.display, c.email); }));
        results.append(rr);
      });
    }, 200);
  });
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

  // "last met" (calendar, email-matched) is DISTINCT from "last mentioned" (notes)
  const dates = el("div", "cp-dates");
  const met = el("div", "cp-lastmet", p.lastMet ? "Last met " + p.lastMet : "No calendar meeting on record");
  met.append(el("span", "cp-date-src", " · calendar"));
  if (!p.lastMet && !(p.emails && p.emails.length)) met.append(el("span", "cp-date-hint", " — link an email below"));
  dates.append(met);
  if (p.lastMentioned) {
    const men = el("div", "cp-lastmentioned", "last mentioned " + p.lastMentioned);
    men.append(el("span", "cp-date-src", " · notes"));
    dates.append(men);
  }
  // going-cold marker names its basis (meeting cadence when email-linked, else mentions)
  if (p.cold && p.daysSince >= 0) {
    const verb = p.neglectBasis === "meetings" ? "met" : "mentioned";
    dates.append(el("div", "cp-cold", "◆ going cold — " + verb + " " + p.daysSince + "d ago" + (p.medianGap ? " (usually every " + p.medianGap + "d)" : "")));
  }
  host.append(dates);

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

  // Meetings (calendar-verified, email-matched) — the true "last met", distinct
  // from the note Timeline below.
  if (p.meetings && p.meetings.length) {
    const sec = cpSection("Meetings", p.meetings.length);
    p.meetings.forEach((m) => {
      const row = el("div", "cp-tl-row");
      row.append(el("span", "cp-date", m.date), el("span", "cp-tl-name", m.title), el("span", "cp-src", "calendar"));
      sec.append(row);
    });
    host.append(sec);
  }

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

  // Emails — the contact's linked calendar identities (these drive "last met").
  // Add more by hand, and act on any pending suggestions for THIS person.
  const esec = cpSection("Emails", p.emails ? p.emails.length : 0);
  (p.emails || []).forEach((em) => esec.append(el("div", "cp-email", em)));
  if (!p.emails || !p.emails.length) esec.append(el("div", "cp-empty", "No linked emails — calendar meetings match once you link one."));
  const addRow = el("div", "cp-email-add");
  const einp = el("input", "cp-email-input"); einp.type = "email"; einp.placeholder = "add an email…";
  const doAdd = async () => {
    const email = einp.value.trim();
    if (!email) return;
    const np = await postJSON("/api/contacts/email", { key: p.key, display: p.display, email });
    renderContactPage(np);
  };
  einp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); doAdd(); } });
  addRow.append(einp, pill("Link email", doAdd));
  esec.append(addRow);
  host.append(esec);
  // pending suggestions for THIS contact (from the review queue, async)
  fetch("/api/contacts/email-review").then((r) => r.json()).then((d) => {
    (d.candidates || []).filter((c) => c.contactKey === p.key).forEach((c) => {
      const sug = el("div", "cp-email-suggest");
      sug.append(el("span", "cp-email-suggest-text", "You met " + p.display + " on " + c.metOn + " — link " + c.email + "?"));
      sug.append(
        pill("Link", async () => { renderContactPage(await postJSON("/api/contacts/email", { key: p.key, display: p.display, email: c.email })); }),
        pillLight("Dismiss", async () => { await postJSON("/api/contacts/email-dismiss", { email: c.email, key: p.key }); sug.remove(); }),
      );
      esec.append(sug);
    });
  }).catch(() => {});

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

// ---- READING (book shelf over the extrinsic zone) ----
let _books = [];

async function loadReading() {
  let d = { books: [] };
  try { d = await (await fetch("/api/reading")).json(); } catch (e) {}
  _books = d.books || [];
  renderShelf();
}

function renderShelf() {
  const strip = els.readingStrip, shelf = els.bookShelf;
  strip.innerHTML = ""; shelf.innerHTML = "";
  const q = (els.bookSearch.value || "").trim().toLowerCase();
  const match = (b) => !q || b.title.toLowerCase().includes(q) ||
    (b.authors || []).some((a) => a.display.toLowerCase().includes(q));

  // reading strip: currently-reading, always on top (independent of the filter)
  const reading = _books.filter((b) => b.status === "reading" && match(b));
  if (reading.length) {
    strip.append(el("div", "reading-strip-head", "Currently reading — " + reading.length));
    const row = el("div", "reading-strip-cards");
    reading.forEach((b) => row.append(readingCard(b)));
    strip.append(row);
  }

  // shelf: apply the status filter + search, then the chosen sort
  const filter = els.bookFilter.value;
  let rows = _books.filter((b) => (filter === "all" || b.status === filter) && match(b));
  rows.sort(shelfComparator(els.bookSort.value));
  shelf.append(shelfHeader());
  if (!rows.length) { shelf.append(el("div", "cp-empty", "No books match.")); return; }
  rows.forEach((b) => shelf.append(bookRow(b)));
  els.readingNav && (document.title = "Reading — " + _books.length);
}

function shelfComparator(key) {
  const cmp = {
    date: (a, b) => (b.dateRead || "").localeCompare(a.dateRead || "") || a.title.localeCompare(b.title),
    rating: (a, b) => (b.rating - a.rating) || (b.dateRead || "").localeCompare(a.dateRead || ""),
    title: (a, b) => a.title.localeCompare(b.title),
    year: (a, b) => (b.yearWritten || "").localeCompare(a.yearWritten || "") || a.title.localeCompare(b.title),
  };
  return cmp[key] || cmp.date;
}

function shelfHeader() {
  const h = el("div", "book-row book-head");
  h.append(el("span", "bk-title", "TITLE"), el("span", "bk-authors", "AUTHOR"),
    el("span", "bk-year", "YEAR"), el("span", "bk-rating", "RATING"), el("span", "bk-date", "READ"));
  return h;
}

function bookRow(b) {
  const row = el("div", "book-row");
  const title = el("span", "bk-title cp-clickable", b.title);
  title.onclick = () => { _noteReturn = "#/reading"; openNoteByPath(b.path); };
  row.append(title, authorsEl(b), el("span", "bk-year", b.yearWritten || ""));
  row.append(starsEl(b), el("span", "bk-date", b.dateRead || (b.status === "reading" ? "reading" : "")));
  return row;
}

function readingCard(b) {
  const c = el("div", "reading-card");
  const t = el("span", "reading-card-title cp-clickable", b.title);
  t.onclick = () => { _noteReturn = "#/reading"; openNoteByPath(b.path); };
  c.append(t, authorsEl(b));
  c.append(pill("✓ Mark read", async () => {
    await postJSON("/api/reading/finish", { path: b.path, rating: 0 });
    loadReading();
  }));
  return c;
}

function authorsEl(b) {
  const wrap = el("span", "bk-authors");
  (b.authors || []).forEach((a, i) => {
    if (i) wrap.append(document.createTextNode(", "));
    const link = el("span", "bk-author-link", a.display);
    link.onclick = (ev) => { ev.stopPropagation(); resolveAndOpen(a.key); };
    wrap.append(link);
  });
  return wrap;
}

// interactive 5-star rating; click sets, click the current value clears it
function starsEl(b) {
  const s = el("span", "bk-rating");
  for (let i = 1; i <= 5; i++) {
    const star = el("span", "bk-star" + (i <= b.rating ? " on" : ""), i <= b.rating ? "★" : "☆");
    star.onclick = async (ev) => {
      ev.stopPropagation();
      const val = b.rating === i ? 0 : i; // re-clicking the current rating clears it
      const nb = await postJSON("/api/reading/rating", { path: b.path, rating: val });
      Object.assign(b, nb);
      renderShelf();
    };
    s.append(star);
  }
  return s;
}

// resolve a wikilink target then open where it points (person → contact, else note)
async function resolveAndOpen(target) {
  try {
    const r = await (await fetch("/api/note/resolve?target=" + encodeURIComponent(target))).json();
    if (r.kind === "contact") location.hash = "#/contacts/" + encodeURIComponent(r.key);
    else if (r.kind === "note") { _noteReturn = "#/reading"; openNoteByPath(r.path); }
  } catch (e) {}
}

async function addBook() {
  const title = prompt("Book title (a book you're starting):");
  if (!title || !title.trim()) return;
  const author = prompt("Author (optional):") || "";
  const nb = await postJSON("/api/reading/book", { title: title.trim(), authors: author.trim() ? [author.trim()] : [], status: "reading" });
  await loadReading();
  if (nb && nb.path) { _noteReturn = "#/reading"; openNoteByPath(nb.path); } // open to add notes
}

if (els.bookSearch) els.bookSearch.addEventListener("input", renderShelf);
if (els.bookSort) els.bookSort.addEventListener("change", renderShelf);
if (els.bookFilter) els.bookFilter.addEventListener("change", renderShelf);
if (els.bookAddBtn) els.bookAddBtn.addEventListener("click", addBook);

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
  // quiet zone badge: system-zone notes are app-managed markdown
  if (_note.zone === "system") els.noteTitle.append(el("span", "note-zone-badge", "SYSTEM"));
  // engine-owned notes are read-only (the write guard refuses them) — hide edit
  els.noteRawToggle.hidden = !!_note.readOnly;
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
  let overlay = null;
  const hasLinks = () => /\[\[[^\]]+\]\]/.test(input.value);
  function render() {
    const parent = input.parentElement; // read lazily — may be attached after this call
    if (!parent) return;
    parent.classList.add("has-inline-overlay");
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
  else if (e.key === "/" && els.castbar && els.castbar.hidden && !typingInField(e.target)) { e.preventDefault(); openCastbar(); }
});

// ---- cast command bar (press / anywhere): run a vault skill or on-demand ritual ----
// A skill is cast through the sage spirit; a ritual runs on its own spirit. The
// argument box becomes the summoner's request (skills) or free-form ask (rituals).
let castItems = [], castFiltered = [], castSel = -1, castChosen = null;

function typingInField(t) {
  if (!t) return false;
  const tag = (t.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || t.isContentEditable;
}

async function openCastbar() {
  els.castbar.hidden = false;
  els.castbarInput.value = "";
  els.castbarResults.innerHTML = "";
  els.castbarArg.hidden = true;
  castSel = -1; castChosen = null; castFiltered = [];
  els.castbarInput.focus();
  try {
    const d = await (await fetch("/api/spirits/castables")).json();
    castItems = d.data || [];
  } catch (e) { castItems = []; }
  renderCastResults("");
}
function closeCastbar() { els.castbar.hidden = true; }

function renderCastResults(q) {
  const host = els.castbarResults; host.innerHTML = ""; castSel = -1;
  const needle = q.trim().toLowerCase();
  castFiltered = castItems.filter(c =>
    !needle || c.label.toLowerCase().includes(needle) || (c.description || "").toLowerCase().includes(needle)
  ).slice(0, 10);
  if (!castItems.length) {
    host.append(el("div", "cmd-empty", "No castable skills or rituals found."));
    return;
  }
  castFiltered.forEach((c, i) => {
    const row = el("div", "cmd-result");
    const kind = el("span", "cast-kind cast-kind-" + c.kind, c.kind === "skill" ? "skill" : "ritual");
    const name = el("span", "cmd-name", c.label);
    row.append(kind, name);
    if (c.description) row.append(el("span", "cast-desc", c.description));
    row.onclick = () => castChoose(c);
    row.onmouseenter = () => { castSel = i; paintCastSel(); };
    host.append(row);
  });
  if (castFiltered.length) { castSel = 0; paintCastSel(); }
}
function paintCastSel() {
  [...els.castbarResults.children].forEach((c, i) => c.classList.toggle("sel", i === castSel));
}
function castMove(d) {
  if (!castFiltered.length) return;
  castSel = (castSel + d + castFiltered.length) % castFiltered.length;
  paintCastSel();
}
function castChoose(c) {
  castChosen = c;
  els.castbarResults.innerHTML = "";
  els.castbarArg.hidden = false;
  els.castbarArgLabel.textContent = (c.kind === "skill" ? "Cast skill: " : "Run ritual: ") + c.label;
  els.castbarArgInput.value = "";
  els.castbarArgHint.textContent = c.kind === "skill"
    ? "sage · skill-cast"
    : c.spirit + " · " + c.ritual;
  els.castbarArgInput.focus();
}

async function castSubmit() {
  if (!castChosen) return;
  const body = {
    spirit: castChosen.spirit,
    ritual: castChosen.ritual,
    request: els.castbarArgInput.value.trim(),
    skill: castChosen.skill || "",
  };
  els.castbarCast.disabled = true;
  let res;
  try {
    res = await fetch("/api/spirits/run-now", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
    });
  } catch (e) { els.castbarArgHint.textContent = "spool failed"; els.castbarCast.disabled = false; return; }
  els.castbarCast.disabled = false;
  if (res.status === 409) { els.castbarArgHint.textContent = "already running — jumping to its live row"; }
  else if (!res.ok) { els.castbarArgHint.textContent = "spool failed — is the engine configured?"; return; }
  closeCastbar();
  // Jump to the runs board; the file-derived live poll picks it up (no watcher).
  location.hash = "#/spirits/runs";
  loadSpiritRuns();
  ensureLivePoll();
}

if (els.castbarInput) {
  els.castbarInput.addEventListener("input", () => renderCastResults(els.castbarInput.value));
  els.castbarInput.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); castMove(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); castMove(-1); }
    else if (e.key === "Enter") { e.preventDefault(); if (castFiltered[castSel]) castChoose(castFiltered[castSel]); }
    else if (e.key === "Escape") { e.preventDefault(); closeCastbar(); }
  });
}
if (els.castbarArgInput) {
  els.castbarArgInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); castSubmit(); }
    else if (e.key === "Escape") {
      e.preventDefault(); // back to the list, not all the way out
      els.castbarArg.hidden = true; castChosen = null;
      renderCastResults(els.castbarInput.value); els.castbarInput.focus();
    }
  });
}
if (els.castbarCast) els.castbarCast.addEventListener("click", castSubmit);
if (els.castbarBackdrop) els.castbarBackdrop.addEventListener("click", closeCastbar);

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
  facts.append(cmdFact("Last met", c.lastMet ? c.lastMet + " · calendar" : "—"));
  facts.append(cmdFact("Last mentioned", c.lastMentioned ? c.lastMentioned + " · notes" : "—"));
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
  const goals = h === "#/goals" || h.startsWith("#/goals/"); // #/goals/<id> deep-links a Rock
  const cal = h === "#/calendar";
  const fd = h === "#/feed";
  const studio = h === "#/studio" || h.startsWith("#/studio/");
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const contacts = h === "#/contacts" || h.startsWith("#/contacts/");
  const reading = h === "#/reading" || h.startsWith("#/reading/");
  const note = h.startsWith("#/note/");
  const day = !goals && !cal && !fd && !studio && !sp && !contacts && !reading && !note;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.calendarView.hidden = !cal;
  els.feedView.hidden = !fd;
  els.studioView.hidden = !studio;
  els.spiritsView.hidden = !sp;
  els.contactsView.hidden = !contacts;
  els.readingView.hidden = !reading;
  els.noteView.hidden = !note;
  els.dateNav.hidden = !day;
  els.goalsNav.hidden = !day;
  els.feedNav.hidden = !day;
  els.studioNav.hidden = !day;
  els.calNav.hidden = !day;
  els.contactsNav.hidden = !day;
  els.readingNav.hidden = !day;
  els.spiritsNav.hidden = !day;
  els.dayNav.hidden = day;
  if (day) refreshFeedBadge(); // pill only shows on the day view — keep it honest
  if (goals) loadGoals(h.startsWith("#/goals/") ? decodeURIComponent(h.slice("#/goals/".length)) : "");
  else if (cal) loadCalendar();
  else if (fd) showFeed(); // manifest's one inbox
  else if (studio) showStudio(); // content studio: draft board + inspiration
  else if (sp) showSpirits(); // engine console: runs / rituals / approvals
  else if (contacts) showContacts(); // people layer: list / page
  else if (reading) loadReading(); // book shelf over the extrinsic zone
  else if (note) showNote(decodeURIComponent(h.slice("#/note/".length))); // universal note view
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
