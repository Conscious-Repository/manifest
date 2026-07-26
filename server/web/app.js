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
  // PROPERTIES — real-estate cockpit
  propertiesView: document.getElementById("propertiesView"),
  propertiesNav: document.getElementById("propertiesNav"),
  propertiesMeta: document.getElementById("propertiesMeta"),
  propertyBoard: document.getElementById("propertyBoard"),
  propertyPage: document.getElementById("propertyPage"),
  propToggle: document.getElementById("propToggle"),
  propertyStatements: document.getElementById("propertyStatements"),
  propertyContractors: document.getElementById("propertyContractors"),
  propertyWork: document.getElementById("propertyWork"),
  propertySettings: document.getElementById("propertySettings"),
  propertyMapWrap: document.getElementById("propertyMapWrap"),
  propertyMap: document.getElementById("propertyMap"),
  propertyMapLegend: document.getElementById("propertyMapLegend"),
  propertyUnmapped: document.getElementById("propertyUnmapped"),
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
  toastHost: document.getElementById("toastHost"),
  sp_rituals: document.getElementById("sp-rituals"),
  spiritRitualBoard: document.getElementById("spiritRitualBoard"),
  sp_portals: document.getElementById("sp-portals"),
  portalList: document.getElementById("portalList"),
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
    addTaskRow(rows[i] || { text: "", done: false }, i + 1, focus[i]);
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

function addTaskRow(task, num, pick) {
  const row = document.createElement("div");
  row.className = "trow";
  if (task.goalId) row.dataset.goalId = task.goalId; // preserve backlink on save
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
  const empty = !task.text && !task.goalId;
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

// ---- ORIENT: the ladder felt top-to-bottom (goals-orient plan) ----
// Vision → 1-year → Rocks; each Rock one collapsed row (name · position →
// next-action), click to expand the stage trail + the CURRENT stage's tasks.
// Expansion lives in JS (never the DOM) so it survives the refetch-re-render
// every mutation triggers via goalsApi.

const goalsUI = { expanded: new Set(), tab: "orient" };

async function loadGoals(focusId) {
  try {
    const doc = await (await fetch("/api/goals")).json();
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
function rockLint(g) {
  const reasons = [];
  if (!g.until) reasons.push("no finish line");
  if (!g.verify) reasons.push("no check");
  if (!g.serves) reasons.push("unlinked");
  const stages = g.children || [];
  const cur = stages.find((s) => !s.checked);
  const hasOpenTask = cur && (cur.children || []).some((t) => !t.checked);
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
  (area.rocks || []).forEach((g) => rocks.appendChild(rockNode(g)));
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
function rockNode(g) {
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
    const t = (cur.children || []).find((c) => !c.checked);
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
    wrap.appendChild(rockExpand(g, stages, cur));
  }
  return wrap;
}

// rockExpand: the trail (✓ done / → current / plain future). The current stage is
// always open (the active work). Every OTHER stage is click-to-expand, so tasks
// can be added under any stage — not just the current one. An open stage shows
// its tasks as checkboxes + a "+ task" ghost.
function rockExpand(g, stages, cur) {
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
      (st.children || []).forEach((tk) => box.appendChild(taskRowEl(tk)));
      box.appendChild(ghostInput("+ task", "o-tk-ghost", (v) =>
        goalsApi("POST", "/api/goals/item", { parentId: st.id, text: v, owner: "" })));
    }
  });
  box.appendChild(ghostInput("+ stage", "o-st-ghost", (v) =>
    goalsApi("POST", "/api/goals/item", { parentId: g.id, text: v, owner: "me" }),
    "what state will you have reached?"));
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

function taskRowEl(tk) {
  const row = el("div", "o-tk" + (tk.checked ? " done" : ""));
  row.dataset.goalId = tk.id;
  const check = el("button", "check" + (tk.checked ? " on" : ""), tk.checked ? "✓" : "○");
  check.addEventListener("click", () =>
    goalsApi("POST", "/api/goals/check", { id: tk.id, checked: !tk.checked }));
  const label = el("span", "o-tk-text", tk.text);
  clickToEdit(label, () => tk.text, (v) =>
    goalsApi("PATCH", "/api/goals/item", { id: tk.id, text: v }));
  row.append(check, label);
  return row;
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
function ghostInput(label, cls, onSubmit, placeholder) {
  const ghost = el("button", "o-ghost " + (cls || ""), label);
  ghost.addEventListener("click", (e) => {
    e.stopPropagation();
    const input = document.createElement("input");
    input.className = "o-edit o-ghost-edit"; // block: the open input gets its own line
    input.placeholder = placeholder || label.replace(/^[＋+]\s*/, "");
    ghost.replaceWith(input);
    input.focus();
    let settled = false;
    const settle = (commit) => {
      if (settled) return;
      settled = true;
      const v = input.value.trim();
      if (commit && v) onSubmit(v);
      else input.replaceWith(ghost);
    };
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") settle(true);
      else if (ev.key === "Escape") settle(false);
    });
    input.addEventListener("blur", () => settle(true));
  });
  return ghost;
}

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
// Purely the engine console: RUNS · RITUALS — checking + instigating runs.
// The feed (incl. the approvals inbox) lives one level up as its own tab; the
// ENGINE owns execution — the only write toward it is a spooled run-now request.
const SPIRIT_TABS = ["runs", "rituals", "portals"];
let spiritStatusCache = null;
let spiritRuns = { data: [], queued: [] }; // last poll of /api/spirits/runs — the ONLY run state; nothing else is held
let openRunId = null;                       // which run's report detail is expanded (for live body refresh)

function showSpirits() {
  const tab = spiritTabFromHash();
  SPIRIT_TABS.forEach((t) => { els["sp_" + t].hidden = t !== tab; });
  document.querySelectorAll("#spiritsTabs .atab").forEach((a) => a.classList.toggle("active", a.dataset.tab === tab));
  loadSpiritsStatus(); // engine-alive chip shows on every sub-tab
  if (tab === "runs") loadSpiritRuns();
  else if (tab === "rituals") loadSpiritRituals();
  else if (tab === "portals") loadPortals();
  ensureLivePoll(); // resume watching any queued/running runs, derived from files
}
function spiritTabFromHash() {
  const t = (location.hash.split("/")[2] || "runs");
  return SPIRIT_TABS.includes(t) ? t : "runs";
}

// ---- PORTALS sub-tab: every external realm, (re)connectable in place ----
// The one place a connection is seen and repaired. Api-key portals (clickup,
// benchling) take a pasted key → save → auto-test; the oauth portal (calendar)
// runs its existing sign-in; the engine's LLM conduits are read-only. This is
// also the seed of app-wide settings — the row renderer is generic over the
// server's portal definition (fields drive the form), so github/docusign appear
// later as pure data.
async function loadPortals() {
  const host = els.portalList; if (!host) return;
  if (!host.children.length) host.textContent = "loading…";
  try {
    const rows = (await (await fetch("/api/portals")).json()).rows || [];
    renderPortals(rows);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Portals unavailable.")); }
}

function renderPortals(rows) {
  const host = els.portalList; host.innerHTML = "";
  const head = el("div", "portal-row portal-head");
  ["PORTAL", "STATE", "LAST CROSSING", "KEY", ""].forEach((h) => head.append(el("span", "", h)));
  host.append(head);
  rows.forEach((p) => host.append(portalRowEl(p)));
}

const PORTAL_STATE_LABEL = { open: "open", degraded: "degraded", sealed: "—", dormant: "—" };

function portalRowEl(p) {
  const wrap = el("div", "portal-wrap");
  const row = el("div", "portal-row state-" + p.state);
  row.dataset.portalId = p.id;
  row.append(el("span", "portal-name", p.name));
  const st = el("span", "portal-state", PORTAL_STATE_LABEL[p.state] || p.state);
  row.append(st);
  row.append(el("span", "portal-cross", portalCrossing(p)));
  row.append(el("span", "portal-key", p.masked || (p.kind === "oauth" ? "oauth" : "—")));
  const acts = el("span", "portal-acts");
  buildPortalActions(p, acts, wrap);
  row.append(acts);
  wrap.append(row);
  if (p.state === "degraded" && p.err) wrap.append(el("div", "portal-err", p.err));
  if (p.kind === "oauth" && (p.accounts || []).length) {
    wrap.append(el("div", "portal-note", "connected: " + p.accounts.join(", ")));
  } else if (p.note && p.state !== "degraded") {
    wrap.append(el("div", "portal-note", p.note));
  }
  return wrap;
}

function portalCrossing(p) {
  if (p.kind === "llm") return "via engine";
  if (!p.lastCrossing) return p.state === "sealed" ? "not connected" : "—";
  const d = new Date(p.lastCrossing);
  if (isNaN(d)) return "—";
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  const t = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).replace(" ", "");
  return sameDay ? t + " today" : d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function buildPortalActions(p, acts, wrap) {
  if (p.kind === "apikey") {
    if (p.state === "dormant") { acts.append(el("span", "portal-dim", "(v2)")); return; }
    if (p.state === "sealed") {
      acts.append(pillLight("connect", () => togglePortalForm(p, wrap)));
      return;
    }
    acts.append(
      pillLight("test", () => portalAction("/api/portals/" + p.id + "/test")),
      pillLight("poll", () => portalAction("/api/portals/" + p.id + "/poll")),
      pillLight("replace", () => togglePortalForm(p, wrap)),
      pillLight("disconnect", () => { if (confirm("Disconnect " + p.name + "? Its cached items stay until they age out.")) portalAction("/api/portals/" + p.id + "/disconnect"); }),
    );
    return;
  }
  if (p.kind === "oauth") {
    if ((p.accounts || []).length) {
      acts.append(pillLight("add", () => portalConnectCalendar()));
      p.accounts.forEach((email) => acts.append(pillLight("disconnect", () => portalDisconnectCalendar(email))));
    } else {
      acts.append(pillLight("connect", () => portalConnectCalendar()));
    }
    return;
  }
  // llm — read-only, managed by the engine
  acts.append(el("span", "portal-dim", "engine"));
}

// togglePortalForm reveals the paste-key form inline beneath a row. Secret
// fields are password inputs; on save the key posts to the server (0600) and the
// row re-renders from the auto-tested response — the value never comes back.
function togglePortalForm(p, wrap) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form");
  const inputs = {};
  (p.fields || []).forEach((f) => {
    const label = el("label", "portal-field");
    label.append(el("span", "portal-field-label", f.label));
    const input = el("input", "portal-input");
    input.type = f.secret ? "password" : "text";
    input.placeholder = f.hint || "";
    label.append(input);
    inputs[f.key] = input;
    form.append(label);
  });
  const save = el("button", "pill-solid", "save & test");
  save.onclick = async () => {
    const fields = {};
    Object.keys(inputs).forEach((k) => { fields[k] = inputs[k].value.trim(); });
    save.disabled = true; save.textContent = "testing…";
    try {
      const row = await postJSON("/api/portals/" + p.id + "/key", { fields });
      form.remove();
      const wrapNew = portalRowEl(row);
      wrap.replaceWith(wrapNew);
      showToast(row.state === "open" ? p.name + " connected" : p.name + " saved — " + (row.err || row.state), null, row.state === "open" ? "info" : undefined);
    } catch (e) { save.disabled = false; save.textContent = "save & test"; showToast("Couldn't save " + p.name); }
  };
  form.append(save);
  wrap.append(form);
  const first = form.querySelector("input"); if (first) first.focus();
}

async function portalAction(url) {
  try {
    const row = await postJSON(url, {});
    const wrap = els.portalList.querySelector(`[data-portal-id="${CSS.escape(row.id)}"]`)?.closest(".portal-wrap");
    if (wrap) wrap.replaceWith(portalRowEl(row));
    refreshFeedBadge();
  } catch (e) { showToast("Portal action failed"); }
}

// Calendar keeps its own OAuth endpoints — the portal row just drives them, then
// reloads the panel so its state reflects the new connection.
async function portalConnectCalendar() {
  showToast("Opening Google sign-in… (check your browser)", null, "info");
  try { await postJSON("/api/calendar/connect", {}); } catch (e) {}
  loadPortals();
}
async function portalDisconnectCalendar(email) {
  try { await postJSON("/api/calendar/disconnect", { account: email }); } catch (e) {}
  loadPortals();
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
let feedCache = { items: [], signals: [], proposals: [], portalItems: [] };

function showFeed() {
  loadFeed();
  ensureLivePoll(); // a dig/ask spooled from here is watched without leaving the tab
}

async function loadFeed() {
  const view = state.feedView || "inbox";
  try {
    const d = await (await fetch("/api/feed?status=" + view)).json();
    feedCache = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [], portalItems: d.portalItems || [] };
    setBadge(els.feedNavBadge, d.badge || 0);
    if (view === "inbox") diffDigests(feedCache.items); // catch digests landed while unpolled
  } catch (e) { feedCache = { items: [], signals: [], proposals: [], portalItems: [] }; }
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
  // pinned lane: FULL approval cards (diff + Confirm/Reject inline — the
  // approvals inbox lives HERE now, not in SPIRITS) lead the inbox; digests pin
  // next via the items sort. Approvals derive from pending/ so a decision
  // resolves the card atomically; they never appear under KEPT/ALL.
  if (view === "inbox") feedCache.proposals.forEach((p) => host.appendChild(approvalCardEl(p)));
  // portal-items lane: externally-sourced notices (clickup digest, benchling
  // items), deterministic + script-rendered. INBOX only — they're notices, not
  // kept/discarded items, and never touch the tune loop.
  if (view === "inbox") feedCache.portalItems.forEach((pc) => host.appendChild(portalCardEl(pc)));
  if (!feedCache.items.length && !host.children.length) {
    host.appendChild(emptyRow(view === "inbox"
      ? "Inbox zero — nothing awaiting a verdict."
      : view === "kept" ? "Nothing kept yet." : "No feed items yet."));
    return;
  }
  feedCache.items.forEach((it) => host.appendChild(feedCard(it)));
  if (pendingApprovalFocus) { // deep-linked (Studio tuning panel "review →")
    const target = host.querySelector(`[data-approval-id="${CSS.escape(pendingApprovalFocus)}"]`);
    pendingApprovalFocus = null;
    if (target) {
      target.scrollIntoView({ behavior: "smooth", block: "start" });
      target.classList.add("goal-flash");
      setTimeout(() => target.classList.remove("goal-flash"), 2400);
    }
  }
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

// portalCardEl renders the third feed card kind: an externally-sourced portal
// notice, built entirely from the deterministic poll cache (no LLM). A ClickUp
// day collapses to one digest (assigned-to-you block first, then per-list
// groups); a Benchling change is one item card with a jump link. Dismiss (and
// jump, for items) are the only actions — portals are read-only to their source.
function portalCardEl(pc) {
  const isDigest = pc.type === "portal-digest";
  const card = el("div", "feed-card portal-card" + (pc.pinned ? " pinned" : ""));
  card.dataset.portalId = pc.id;
  const top = el("div", "feed-top");
  if (pc.pinned) top.append(el("span", "pin-chip", "📌 pinned"));
  top.append(el("span", "type-chip type-portal", pc.portal)); // muted source tag
  if (pc.change) top.append(el("span", "portal-change-chip change-" + pc.change, pc.change)); // new / edited
  if (pc.date) top.append(el("span", "feed-date", fmtFeedDate(pc.date)));
  card.append(top);
  card.append(el("div", "feed-title", pc.title));

  if (isDigest) {
    if ((pc.forYou || []).length) {
      card.append(el("div", "portal-subhead", "assigned to you / mentions you"));
      pc.forYou.forEach((ln) => card.append(portalLineRow(ln)));
    }
    (pc.groups || []).forEach((g) => {
      card.append(el("div", "portal-subhead", g.list));
      (g.lines || []).forEach((ln) => card.append(portalLineRow(ln)));
    });
  } else {
    if (pc.detail) card.append(el("div", "feed-why", pc.detail));
    const meta = el("div", "feed-meta");
    if (pc.actor) meta.append(el("span", "", "by " + pc.actor));
    card.append(meta);
  }

  const acts = el("div", "feed-actions");
  if (!isDigest && pc.url) acts.append(pillLight("jump →", () => window.open(pc.url, "_blank")));
  acts.append(pillLight("Dismiss", () => portalDismiss(pc.id)));
  card.append(acts);
  return card;
}

// portalLineRow is one digest line: the task, linking to the source app.
function portalLineRow(ln) {
  const row = el("div", "portal-line");
  const label = ln.url
    ? Object.assign(el("a", "portal-line-text", ln.text), { href: ln.url, target: "_blank" })
    : el("span", "portal-line-text", ln.text);
  row.append(label);
  return row;
}

async function portalDismiss(id) {
  try { await postJSON("/api/portals/item/dismiss", { id }); } catch (e) {}
  loadFeed();
}
function fmtFeedDate(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
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
    pillLight("Dismiss", () => studioDismiss(it.draftId, it.approvalId, loadFeed)),
  );
  card.append(editWrap, fb, actions);
  return card;
}

// studioDismiss resolves an owner rejection server-side across all three
// objects (approval + draft file + feed card) — see handleStudioDismiss.
async function studioDismiss(draftId, approvalId, refresh) {
  if (!draftId) { showToast("this card has no draft id", null, "error"); return; }
  await studioPost(`/api/studio/draft/${encodeURIComponent(draftId)}/dismiss`, { approvalId: approvalId || "" });
  showToast("dismissed", null, "info");
  refresh();
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
    row.append(pillLight("review →", () => { pendingApprovalFocus = p.id; location.hash = "#/feed"; }));
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
  const board = studioCache.board || [];
  if ((studioCache.tuneApprovals || []).length) host.append(renderTuningPanel());
  host.append(renderCommissionBox());
  if (!board.length) { host.append(emptyRow("No drafts yet — commission one above, or scribe drafts each morning.")); return; }
  // group by the status vocabulary
  const order = ["passed", "pending-audit", "queued", "posted", "killed", "dismissed"];
  const labels = { passed: "Passed — approve to queue", "pending-audit": "Pending audit", queued: "Queued", posted: "Posted", killed: "Killed", dismissed: "Dismissed" };
  const byStatus = {};
  board.forEach((d) => { (byStatus[d.status] = byStatus[d.status] || []).push(d); });
  order.forEach((st) => {
    const items = byStatus[st];
    if (!items || !items.length) return;
    const head = labels[st] + "  —  " + items.length;
    if (st === "killed" || st === "dismissed") {
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
      pillLight("Dismiss", () => studioDismiss(d.id, d.approvalId, loadStudio)),
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
let pendingApprovalFocus = null; // approval id to scroll to in FEED (Studio tuning panel "review →")

// approvalCardEl: a pending approval as a first-class FEED card — evidence,
// per-type guards, current-vs-proposed diff, and Confirm/Reject inline
// (approvals-move-to-feed plan; formerly the SPIRITS approvals panel card).
function approvalCardEl(a) {
  const card = el("div", "approval-card pinned");
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
  loadFeed(); // approvals live in FEED — the decided card resolves in place
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

// ---- PROPERTIES: the real-estate cockpit over system/realestate/ records ----
// Board (grouped by entity, paid%/committed% rollups) + a property page (rollup,
// budget, ledger with quick-add, log with quick-add, prose via the note view).
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "board"; // board | map | statements | page — derived from the hash
let boardFilter = ""; // search-as-you-type
let boardStatus = ""; // status dropdown ("" = all)
let boardEntity = ""; // entity dropdown ("" = all)
let boardSort = "status"; // status | address | budget | var

// showProperties routes the PROPERTIES sub-views off the hash:
//   #/properties · /map · /statements · /deal/<slug> · /<slug>
function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true; els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true; els.propertyStatements.hidden = true;
  els.propertyContractors.hidden = true;
  els.propertyWork.hidden = true; els.propertySettings.hidden = true;
  if (tail.startsWith("deal/")) { propMode = "page"; renderDealPage(tail.slice(5)); }
  else if (tail === "map") { propMode = "map"; syncPropChips(); loadProperties(); }
  else if (tail === "work") { propMode = "work"; syncPropChips(); renderWorkView(); }
  else if (tail === "statements") { location.hash = "#/properties/accounting"; return; } // legacy
  else if (tail === "accounting") { propMode = "accounting"; syncPropChips(); renderAccounting(); }
  else if (tail === "contractors") { propMode = "contractors"; syncPropChips(); renderContractors(); }
  else if (tail === "settings") { propMode = "settings"; syncPropChips(); renderREsettings(); }
  else if (tail) { propMode = "page"; renderPropertyPage(tail); }
  else { propMode = "board"; syncPropChips(); loadProperties(); }
}

function syncPropChips() {
  els.propToggle.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === propMode));
}

async function loadProperties() {
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
    rePortalEnabled = !!d.rePortal;
  } catch (e) { propertyCache = []; dealCache = []; templateCache = []; }
  if (propMode === "map") renderPropertyMap();
  else if (propMode === "board") renderBoard();
}

function fmtPct(x) { return Math.round((x || 0) * 100) + "%"; }
function fmtMoney(n) { return "$" + Math.round(n || 0).toLocaleString(); }

// projMoney: one property's plan-vs-spend numbers.
function projMoney(p) {
  const pj = p.project || {};
  return { budget: pj.planTotal || 0, committed: pj.committed || 0,
    paid: pj.paid || 0, over: !!pj.over };
}

// ---- shared primitives (admin-portal design §0) ----

// ppCols: a one-line row of mono micro-labels sharing the exact grid of the
// rows beneath it — labels live once, every input aligns under them.
function ppCols(cls, labels) {
  const row = el("div", "pp-cols " + cls);
  labels.forEach((l) => row.append(el("span", "", l)));
  return row;
}

// makeDirtyBar: the one editing model — quiet inputs mark dirty; a sticky
// bottom bar appears with a single save (one PUT of the whole file).
function makeDirtyBar(host, onSave, onDiscard) {
  const bar = el("div", "dirty-bar");
  bar.hidden = true;
  const label = el("span", "dirty-label", "");
  const save = el("button", "pill", "save");
  const discard = el("button", "pill light", "discard");
  bar.append(label, save, discard);
  host.append(bar);
  let count = 0;
  const api = {
    mark() { count++; label.textContent = count + " UNSAVED CHANGE" + (count === 1 ? "" : "S"); bar.hidden = false; },
    clear() { count = 0; bar.hidden = true; },
    get dirty() { return count > 0; },
  };
  save.onclick = async () => { save.disabled = true; try { await onSave(); api.clear(); } finally { save.disabled = false; } };
  discard.onclick = () => { api.clear(); onDiscard(); };
  return api;
}

// collapsibleSection: pp-section-head with a caret + optional collapsed summary.
function collapsibleSection(host, title, summary, open) {
  const head = el("div", "pp-section-head toggle");
  const caret = el("span", "sec-caret", open ? "▾" : "▸");
  head.append(caret, el("span", "", title));
  const sum = el("span", "sec-summary", summary || "");
  head.append(sum);
  const body = el("div", "sec-body");
  body.hidden = !open;
  sum.hidden = open;
  head.onclick = () => {
    body.hidden = !body.hidden;
    caret.textContent = body.hidden ? "▸" : "▾";
    sum.hidden = !body.hidden;
  };
  host.append(head, body);
  return body;
}

// propertyTypeahead: input + filtered dropdown over all property records
// (63 items — a select is unusable). Matches address/slug/deal.
function propertyTypeahead(placeholder, onPick, initial) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  if (initial) input.value = initial;
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  const refresh = () => {
    const q = input.value.toLowerCase().trim();
    drop.innerHTML = "";
    const hits = propertyCache.filter((p) =>
      !q || (p.address || "").toLowerCase().includes(q) || p.slug.includes(q) || (p.deal || "").includes(q)).slice(0, 12);
    hits.forEach((p) => {
      const it = el("div", "ta-item", (p.short || p.address || p.slug) + (p.deal ? "  · " + p.deal : ""));
      it.onmousedown = (e) => { e.preventDefault(); input.value = p.short || p.address || p.slug; drop.hidden = true; onPick(p); };
      drop.append(it);
    });
    drop.hidden = !hits.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return wrap;
}

// ---- BOARD at scale (design §1): grouped by deal ----

const STATUS_BUCKET = {
  construction: "active", pre_development: "active",
  negotiating: "pipeline", under_contract: "pipeline", opportunity: "pipeline",
  completed: "done", leased: "done", listed: "done", sold: "done",
};

function bucketOf(p) {
  // an explicitly active status wins even on tracked-control records
  if (STATUS_BUCKET[p.status] === "active") return "active";
  return p.control === "tracked" ? "tracked" : (STATUS_BUCKET[p.status] || "pipeline");
}

function matchesBoardFilters(p) {
  if (boardStatus && p.status !== boardStatus) return false;
  if (boardEntity && (p.entity || "").trim() !== boardEntity) return false;
  if (!boardFilter) return true;
  const q = boardFilter.toLowerCase();
  return (p.address || "").toLowerCase().includes(q) || p.slug.includes(q) ||
    (p.deal || "").toLowerCase().includes(q) || (p.entity || "").toLowerCase().includes(q);
}

// canonical status order for the flat board: work first, pipeline, then done
const STATUS_ORDER = ["construction", "pre_development", "under_contract", "negotiating",
  "opportunity", "completed", "leased", "listed", "sold"];

function boardComparator(key) {
  const rank = (p) => { const i = STATUS_ORDER.indexOf(p.status); return i < 0 ? STATUS_ORDER.length : i; };
  const addr = (a, b) => (a.short || a.address || a.slug).localeCompare(b.short || b.address || b.slug);
  const cmp = {
    status: (a, b) => rank(a) - rank(b) || addr(a, b),
    address: addr,
    budget: (a, b) => projMoney(b).budget - projMoney(a).budget || addr(a, b),
    var: (a, b) => projMoney(b).varPct - projMoney(a).varPct || addr(a, b),
  };
  return cmp[key] || cmp.status;
}

function renderBoard() {
  const host = els.propertyBoard; host.innerHTML = ""; host.hidden = false;
  const shown = propertyCache.filter((p) => !p.hidden);
  els.propertiesMeta.textContent = shown.length + " properties · " + dealCache.length + " deals";

  // toolbar (reading-page idiom): search + status/entity dropdowns + sort + composer
  const bar = el("div", "board-bar");
  const search = inputEl("search address / deal / entity…");
  search.classList.add("board-search");
  search.value = boardFilter;
  search.addEventListener("input", () => { boardFilter = search.value; renderBoardBody(body); });
  search.addEventListener("keydown", (e) => { if (e.key === "Escape") { search.value = ""; boardFilter = ""; renderBoardBody(body); } });
  bar.append(search);
  const labeledSelect = (pairs, current, onChange, title) => {
    const s = document.createElement("select");
    s.className = "pp-in board-select";
    s.title = title;
    pairs.forEach(([v, l]) => { const o = document.createElement("option"); o.value = v; o.textContent = l; s.append(o); });
    s.value = current;
    s.onchange = () => { onChange(s.value); renderBoardBody(body); };
    return s;
  };
  const statusesPresent = STATUS_ORDER.filter((st) => shown.some((p) => p.status === st));
  bar.append(labeledSelect(
    [["", "all statuses"], ...statusesPresent.map((st) => [st, st.replace(/_/g, " ")])],
    boardStatus, (v) => { boardStatus = v; }, "Filter by status"));
  const entities = [...new Set(shown.map((p) => (p.entity || "").trim()).filter(Boolean))].sort();
  bar.append(labeledSelect(
    [["", "all entities"], ...entities.map((e) => [e, e])],
    boardEntity, (v) => { boardEntity = v; }, "Filter by entity"));
  bar.append(labeledSelect(
    [["status", "by status"], ["address", "by address"], ["budget", "by budget"], ["var", "by variance"]],
    boardSort, (v) => { boardSort = v; }, "Sort"));
  bar.append(propertyComposer());
  host.append(bar);

  const body = el("div", "board-body");
  host.append(body);
  renderBoardBody(body);

  // EXPORTS — demoted to one quiet line at the bottom
  const exports = el("div", "board-exports");
  exports.append(el("span", "pp-section-head", "EXPORTS"));
  const entSel = selectEl([...new Set(["unassigned", ...shown.map((p) => (p.entity || "").trim()).filter(Boolean)])]);
  const yearIn = inputEl("year"); yearIn.value = String(new Date().getFullYear()); yearIn.classList.add("board-year");
  exports.append(entSel, yearIn, pillLight("tax csv", async () => {
    try {
      const res = await postJSONOk("/api/realestate/export-tax",
        { entity: entSel.value === "unassigned" ? "" : entSel.value, year: yearIn.value.trim() });
      showToast("Tax csv written — " + res.lines + " lines", () =>
        window.open("/api/realestate/doc?path=" + encodeURIComponent(res.csv), "_blank"), "info");
    } catch (e) { showToast("Export failed"); }
  }));
  if (rePortalEnabled) {
    exports.append(pillLight("publish → ooda site", async () => {
      try {
        const res = await postJSONOk("/api/realestate/publish-deals", {});
        const kept = (res.kept || []).length ? " · kept as-is: " + res.kept.join(", ") : "";
        showToast("deals.json written — " + res.deals + " deals · " + res.properties + " parcels" + kept +
          " — review the diff in re-portal", null, "info");
      } catch (e) { showToast(("Publish failed: " + (e.message || "")).slice(0, 90)); }
    }));
  }
  host.append(exports);
}

// flat list: every property is one row, ordered by the chosen sort (status by
// default); deal bundles are a muted column linking to their pages.
function renderBoardBody(body) {
  body.innerHTML = "";
  const shown = propertyCache.filter((p) => !p.hidden && matchesBoardFilters(p))
    .sort(boardComparator(boardSort));
  if (!shown.length) { body.append(emptyRow("Nothing matches the filter.")); return; }
  body.append(ppCols("cols-board", ["ADDRESS", "STATUS", "DEAL", "STAGE", "UNITS", "BUDGET", "SPENT"]));
  shown.forEach((p) => body.append(boardRow(p, false)));
}

// dealStatusChip mirrors statusChip against the deal endpoint (incl. "opportunity").
function dealStatusChip(d) {
  const chip = el("span", "property-status editable status-" + (d.status || "").replace(/_/g, "-"), d.status || "—");
  chip.title = "click to change deal status";
  chip.onclick = (e) => {
    e.stopPropagation();
    const sel = selectEl([...PROPERTY_STATUSES, "opportunity"]);
    sel.value = d.status || "negotiating";
    sel.onclick = (ev) => ev.stopPropagation();
    sel.onchange = async () => {
      try { await postJSONOk("/api/deals/" + encodeURIComponent(d.slug) + "/field", { key: "status", value: sel.value }); loadProperties(); }
      catch (err) { showToast("Couldn't update status"); sel.replaceWith(chip); }
    };
    sel.onblur = () => { if (sel.parentNode) sel.replaceWith(chip); };
    chip.replaceWith(sel);
    sel.focus();
  };
  return chip;
}

async function exportUnderwrite(slug) {
  try {
    const res = await postJSONOk("/api/deals/" + encodeURIComponent(slug) + "/export-underwrite", {});
    showToast("Underwrite export written (" + res.members + " member records)", () =>
      window.open("/api/realestate/doc?path=" + encodeURIComponent(res.csv), "_blank"), "info");
  } catch (e) { showToast("Export failed"); }
}

const PROPERTY_STATUSES = ["negotiating", "under_contract", "pre_development", "construction", "completed", "leased", "listed", "sold"];
const PROPERTY_KINDS = ["rehab", "new-construction", "mixed", "hold"];

// statusChip renders a click-to-edit status: the chip swaps to a <select> in
// place; picking a value POSTs the field edit and re-renders via onSaved.
function statusChip(p, onSaved) {
  const chip = el("span", "property-status editable status-" + (p.status || "").replace(/_/g, "-"), p.status || "—");
  chip.title = "click to change status";
  chip.onclick = (e) => {
    e.stopPropagation();
    const sel = selectEl(PROPERTY_STATUSES);
    sel.value = p.status || "negotiating";
    sel.onclick = (ev) => ev.stopPropagation();
    sel.onchange = async () => {
      try { onSaved(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "status", value: sel.value })); }
      catch (err) { showToast("Couldn't update status"); sel.replaceWith(chip); }
    };
    sel.onblur = () => { if (sel.parentNode) sel.replaceWith(chip); };
    chip.replaceWith(sel);
    sel.focus();
  };
  return chip;
}

// boardRow: one property. Members render compact + left-ruled inside a deal
// group; loose records render at standard density. Columns:
// address · status · units · budget · paid%/committed%.
function boardRow(p, member) {
  const row = el("div", "property-row" +
    (p.control === "tracked" ? " tracked" : "") + (member ? " compact member" : ""));
  row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
  row.append(el("span", "property-addr", p.short || p.address || p.name));
  row.append(statusChip(p, () => loadProperties()));
  const dealCell = el("span", "property-deal");
  if (!member && p.deal) {
    const d = (dealCache || []).find((x) => x.slug === p.deal);
    const lnk = el("a", "deal-link", (d && d.name) || p.deal);
    lnk.href = "#/properties/deal/" + encodeURIComponent(p.deal);
    lnk.onclick = (e) => e.stopPropagation();
    dealCell.append(lnk);
  }
  row.append(dealCell);
  row.append(el("span", "property-stage", p.currentStage || ""));
  row.append(el("span", "property-units", p.units ? p.units + "u" : ""));
  const pm = projMoney(p);
  row.append(el("span", "property-budget", pm.budget ? fmtMoney(pm.budget) : ""));
  const out = el("span", "property-out" + (pm.over ? " over" : ""));
  if (pm.budget) {
    out.append(el("span", "out-paid", fmtMoney(pm.paid)),
      el("span", "out-committed", " " + fmtPct(pm.paid / pm.budget)));
  }
  row.append(out);
  return row;
}

// propertyComposer: the spec's entire creation form — address · entity · kind ·
// template pick (seeds the budget table). A ghost button expanding inline.
function propertyComposer() {
  const ghost = el("button", "o-ghost property-add", "＋ property");
  ghost.onclick = () => {
    const form = el("div", "prop-composer");
    const addr = inputEl("address…"); addr.classList.add("pc-addr");
    const entityAC = recordAutocomplete("entity", "entity (optional)…");
    const kind = selectEl(PROPERTY_KINDS);
    const tpl = selectEl(["no template", ...templateCache.map((t) => t.slug)]);
    tpl.title = "budget-mix template";
    const dealSel = selectEl(["unattached", ...dealCache.map((d) => d.slug)]);
    dealSel.title = "attach to a deal bundle";
    const create = el("button", "pill", "create");
    create.onclick = async () => {
      if (!addr.value.trim()) { addr.focus(); return; }
      create.disabled = true;
      try {
        await postJSONOk("/api/properties", {
          address: addr.value, entity: entityAC.value(), kind: kind.value,
          template: tpl.value === "no template" ? "" : tpl.value,
          deal: dealSel.value === "unattached" ? "" : dealSel.value,
        });
        loadProperties();
      } catch (e) { showToast("Couldn't create property"); create.disabled = false; }
    };
    const cancel = el("button", "pill light", "✕");
    cancel.onclick = () => form.replaceWith(ghost);
    form.append(addr, entityAC.el, kind, tpl, dealSel, create, cancel);
    ghost.replaceWith(form);
    addr.focus();
  };
  return ghost;
}

// ---- PROPERTIES map (Leaflet from cdnjs, lazy-loaded; CartoDB light tiles) ----
// Parcel polygons color-coded by status: active work anchors on the app's blue,
// everything else muted. Tracked (pipeline, not owned) parcels render dashed as
// a distinct tier; background parcels sit underneath in near-invisible gray.
// GeoJSON is [lng,lat] — L.geoJSON handles the flip; nothing constructs LatLngs
// by hand. Bounds are computed from the rendered layers, never hardcoded.
let _leafletLoading = null;
let _propMap = null; // the live Leaflet map instance (rebuilt per render)

const PROP_STATUS_COLOR = {
  construction: "#265ACC", pre_development: "#5b82d9", // active — the app blue
  under_contract: "#8a93a6", negotiating: "#a7aeba",   // pipeline — muted slate
  completed: "#4d9d6a", leased: "#4d9d6a", listed: "#4d9d6a", sold: "#4d9d6a", // complete — ONE quiet green
};

// legend: fixed canonical order, done statuses consolidated into one entry
const MAP_LEGEND = [
  ["construction", "#265ACC"], ["pre-development", "#5b82d9"],
  ["under contract", "#8a93a6"], ["negotiating", "#a7aeba"],
  ["complete", "#4d9d6a"], ["tracked", "#b0a58e"], ["deal", "#8a93a6"],
];
const LEGEND_GROUP = {
  construction: "construction", pre_development: "pre-development",
  under_contract: "under contract", negotiating: "negotiating", opportunity: "negotiating",
  completed: "complete", leased: "complete", listed: "complete", sold: "complete",
};

function loadLeaflet() {
  if (window.L) return Promise.resolve();
  if (_leafletLoading) return _leafletLoading;
  _leafletLoading = new Promise((resolve, reject) => {
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = "https://cdnjs.cloudflare.com/ajax/libs/leaflet/1.9.4/leaflet.min.css";
    document.head.append(css);
    const js = document.createElement("script");
    js.src = "https://cdnjs.cloudflare.com/ajax/libs/leaflet/1.9.4/leaflet.min.js";
    js.onload = () => resolve();
    js.onerror = () => { _leafletLoading = null; reject(new Error("leaflet failed to load")); };
    document.head.append(js);
  });
  return _leafletLoading;
}

async function renderPropertyMap() {
  els.propertyBoard.hidden = true; els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = false;
  try { await loadLeaflet(); } catch (e) {
    // offline — degrade to the list with a quiet notice
    setPropMode("list");
    showToast("Map unavailable offline — showing the list");
    return;
  }
  let geo;
  try { geo = await (await fetch("/api/properties/geo")).json(); }
  catch (e) { setPropMode("list"); return; }

  if (_propMap) { _propMap.remove(); _propMap = null; }
  const map = L.map(els.propertyMap, { zoomControl: true, attributionControl: true });
  _propMap = map;
  L.tileLayer("https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/">CARTO</a>',
    maxZoom: 19,
  }).addTo(map);

  // background parcels — context only, nearly invisible
  if (geo.bg && (geo.bg.features || []).length) {
    L.geoJSON(geo.bg, { style: { color: "#d3d7de", weight: 1, fill: true, fillOpacity: 0.02, interactive: false } }).addTo(map);
  }

  const rendered = [];
  const ownedLayers = []; // zoom anchors — the map opens on the owned cluster
  const unmapped = [];
  const statusesSeen = new Set();
  // tracked first (below), owned/active after (on top so borders aren't clipped)
  const trackedTier = (r) => (r.control === "tracked" && STATUS_BUCKET[r.status] !== "active") ? 0 : 1;
  const recs = (geo.records || []).slice().sort((a, b) => trackedTier(a) - trackedTier(b));
  recs.forEach((rec) => {
    if (!(rec.features || []).length) {
      // pin fallback: frontmatter lat/lng or a cached geocode (no polygon)
      if (rec.lat && rec.lng) {
        const color = PROP_STATUS_COLOR[rec.status] || "#8a93a6";
        const pin = L.circleMarker([rec.lat, rec.lng], { radius: 8, color, fillColor: color, fillOpacity: 0.5, weight: 2 });
        const href = "#/properties/" + encodeURIComponent(rec.slug);
        pin.bindPopup('<a href="' + href + '" class="prop-pop">' + escapeHtml(rec.title + (rec.status ? " · " + rec.status : "")) + "</a>", { closeButton: false });
        pin.addTo(map);
        rendered.push(pin);
        if (rec.status) statusesSeen.add(LEGEND_GROUP[rec.status] || rec.status);
      } else unmapped.push(rec);
      return;
    }
    // active status wins over the tracked tier (same rule as bucketOf): flipping
    // a tracked parcel to pre_development/construction must recolor it
    const tracked = rec.control === "tracked" && STATUS_BUCKET[rec.status] !== "active";
    const color = tracked ? "#b0a58e" : (PROP_STATUS_COLOR[rec.status] || "#8a93a6");
    if (rec.status) statusesSeen.add(tracked ? "tracked" : (LEGEND_GROUP[rec.status] || rec.status));
    else if (rec.type === "deal") statusesSeen.add("deal");
    const style = {
      color, weight: tracked ? 1.5 : 2, dashArray: tracked ? "4 3" : null,
      fillColor: color, fillOpacity: tracked ? 0.06 : 0.14,
    };
    // one layer per record → a multi-parcel deal hovers/selects as one group
    const layer = L.geoJSON({ type: "FeatureCollection", features: rec.features }, { style });
    layer.on("mouseover", () => layer.setStyle({ weight: 3, fillOpacity: 0.24 }));
    layer.on("mouseout", () => layer.setStyle(style));
    const href = rec.type === "deal" ? "#/properties/deal/" + encodeURIComponent(rec.slug) : "#/properties/" + encodeURIComponent(rec.slug);
    const label = (rec.short || rec.title) + (rec.status ? " · " + rec.status : "") + (rec.type === "deal" ? " · bundle" : "");
    layer.bindPopup('<a href="' + href + '" class="prop-pop">' + escapeHtml(label) + "</a>", { closeButton: false });
    layer.addTo(map);
    rendered.push(layer);
    if (!tracked && rec.type === "property") ownedLayers.push(layer);
  });

  // Open zoomed to the OWNED cluster (the actual work); tracked/background are a
  // pan away. maxZoom caps a lone parcel from diving to street level.
  const anchors = ownedLayers.length ? ownedLayers : rendered;
  if (anchors.length) {
    map.fitBounds(L.featureGroup(anchors).getBounds().pad(0.05), { maxZoom: 17 });
  } else {
    map.setView([38.65, -90.26], 16); // nothing mapped yet — the seed's neighborhood
  }

  // quiet legend: canonical order, only the groups actually visible
  const legend = els.propertyMapLegend; legend.innerHTML = "";
  MAP_LEGEND.filter(([name]) => statusesSeen.has(name)).forEach(([name, color]) => {
    const chip = el("span", "map-legend-chip");
    const dot = el("span", "map-legend-dot");
    dot.style.background = color;
    chip.append(dot, el("span", "", name));
    legend.append(chip);
  });

  const um = els.propertyUnmapped;
  if (unmapped.length) {
    um.hidden = false;
    um.textContent = "unmapped: " + unmapped.map((r) => r.title).join(" · ");
  } else um.hidden = true;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---- underwriting editor engine (design §2): quiet inputs over the
// engine-shaped source object. The client mutates ONE object in memory and PUTs
// the whole thing on save — unknown fields ride along untouched (fidelity). ----

// uwInput: a quiet input bound to obj[key]. opts: pct (0.75↔75), money, int,
// suffix ("%", "$", "mo", "yr", "%/yr").
function uwInput(obj, key, opts, dirty) {
  const wrap = el("span", "uw-val");
  const input = document.createElement("input");
  input.className = "uw-in";
  const show = (v) => {
    if (v === undefined || v === null || v === "") return "";
    if (opts.pct) return String(Math.round(v * 10000) / 100);
    return String(v);
  };
  input.value = show(obj[key]);
  input.addEventListener("focus", () => input.select());
  input.addEventListener("change", () => {
    const raw = input.value.trim();
    let v;
    if (raw === "") v = undefined;
    else if (opts.text) v = raw;
    else {
      v = parseFloat(raw.replace(/[$,%]/g, ""));
      if (isNaN(v)) { input.value = show(obj[key]); return; }
      if (opts.pct) v = v / 100;
      if (opts.int) v = Math.round(v);
    }
    if (v === undefined) delete obj[key]; else obj[key] = v;
    dirty.mark();
  });
  wrap.append(input);
  if (opts.suffix) wrap.append(el("span", "uw-suffix", opts.suffix));
  return wrap;
}

// uwGrid: label/value pairs in a two-column definition grid.
function uwGrid(host, obj, fields, dirty) {
  const grid = el("div", "uw-grid");
  fields.forEach(([key, label, opts]) => {
    grid.append(el("span", "uw-label", label));
    grid.append(uwInput(obj, key, opts || {}, dirty));
  });
  host.append(grid);
  return grid;
}

// uwRows: the shared array editor — aligned rows of quiet inputs, hover ✕,
// ghost ＋ row, optional Σ footer.
function uwRows(host, obj, key, cols, dirty, opts) {
  opts = opts || {};
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    const arr = obj[key] || [];
    box.append(ppCols("cols-" + cols.length, cols.map((c) => c.label.toUpperCase())));
    arr.forEach((item, i) => {
      const row = el("div", "uw-row cols-" + cols.length);
      cols.forEach((c) => row.append(uwInput(item, c.key, c.opts || {}, dirty)));
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { arr.splice(i, 1); obj[key] = arr; dirty.mark(); render(); };
      row.append(x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ " + (opts.addLabel || "row"));
    add.onclick = () => {
      const item = {};
      cols.forEach((c) => { if (c.init !== undefined) item[c.key] = c.init; });
      obj[key] = [...arr, item];
      dirty.mark();
      render();
    };
    box.append(add);
    if (opts.footer) box.append(el("div", "uw-footer", opts.footer(arr)));
  };
  render();
  host.append(box);
}

// uwKV: an object's key→number rows (soft_cost_items, opex_items).
function uwKV(host, obj, key, dirty, opts) {
  opts = opts || {};
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    const m = obj[key] || {};
    Object.keys(m).forEach((k) => {
      const row = el("div", "uw-row cols-kv");
      const kIn = document.createElement("input");
      kIn.className = "uw-in uw-key";
      kIn.value = k;
      if (opts.keyList) kIn.setAttribute("list", opts.keyList);
      kIn.addEventListener("change", () => {
        const nk = kIn.value.trim();
        if (!nk || nk === k) { kIn.value = k; return; }
        m[nk] = m[k]; delete m[k]; dirty.mark(); render();
      });
      row.append(kIn);
      const holder = { v: m[k] };
      const vIn = uwInput(holder, "v", { suffix: opts.suffixFor ? opts.suffixFor(k) : "" }, dirty);
      vIn.querySelector("input").addEventListener("change", () => { m[k] = holder.v; });
      row.append(vIn);
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { delete m[k]; dirty.mark(); render(); };
      row.append(x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ " + (opts.addLabel || "item"));
    add.onclick = () => {
      const m2 = obj[key] || {};
      let n = "new item", i = 2;
      while (n in m2) n = "new item " + i++;
      m2[n] = 0; obj[key] = m2; dirty.mark(); render();
    };
    box.append(add);
  };
  render();
  host.append(box);
}

const OPEX_SUFFIX = (k) => /per_unit_year/.test(k) ? "$/unit/yr" : /per_unit_month/.test(k) ? "$/unit/mo" : "%";
const OPEX_KEYS = ["management_rate", "maintenance_rate", "reserves_rate", "property_tax_rate",
  "property_tax_pct_of_value", "insurance_rate", "insurance_per_unit_year", "utilities_per_unit_month"];
(function () {
  const dl = document.getElementById("opexKeys");
  if (dl) OPEX_KEYS.forEach((k) => { const o = document.createElement("option"); o.value = k; dl.append(o); });
})();

// dealLevelSections renders the deal-level engine groups into host.
function dealLevelSections(host, src, dirty) {
  let sec = collapsibleSection(host, "RENTS & UNITS", "", true);
  uwGrid(sec, src, [
    ["vacancy_rate", "vacancy rate", { pct: true, suffix: "%" }],
    ["rent_growth", "rent growth", { pct: true, suffix: "%/yr" }],
    ["market_cap_rate", "market cap rate", { pct: true, suffix: "%" }],
    ["lease_up_months", "lease-up", { int: true, suffix: "mo" }],
    ["lease_up_vacancy_rate", "lease-up vacancy", { pct: true, suffix: "%" }],
  ], dirty);

  sec = collapsibleSection(host, "FINANCING", "", true);
  uwGrid(sec, src, [
    ["construction_loan_ltc", "construction ltc", { pct: true, suffix: "%" }],
    ["construction_interest_rate", "construction rate", { pct: true, suffix: "%" }],
    ["perm_ltv", "perm ltv", { pct: true, suffix: "%" }],
    ["perm_interest_rate", "perm rate", { pct: true, suffix: "%" }],
    ["perm_amort_years", "perm amort", { int: true, suffix: "yr" }],
  ], dirty);

  sec = collapsibleSection(host, "OPEX", "", true);
  if (!src.opex_items) src.opex_items = {};
  uwKV(sec, src, "opex_items", dirty, { suffixFor: OPEX_SUFFIX, keyList: "opexKeys", addLabel: "opex item" });
  uwGrid(sec, src, [["opex_growth", "opex growth", { pct: true, suffix: "%/yr" }]], dirty);

  sec = collapsibleSection(host, "EXIT & HOLD", "", true);
  uwGrid(sec, src, [
    ["hold_years", "hold", { int: true, suffix: "yr" }],
    ["exit_cap_rate", "exit cap", { pct: true, suffix: "%" }],
    ["selling_cost_pct", "selling costs", { pct: true, suffix: "%" }],
  ], dirty);
  sec.append(el("div", "uw-sub", "capex schedule"));
  uwRows(sec, src, "capex_schedule", [
    { key: "through_year", label: "through yr", opts: { int: true } },
    { key: "amount", label: "$ /unit/yr", opts: {} , init: 500 },
  ], dirty, { addLabel: "tier" });
  sec.append(el("div", "uw-sub", "equity"));
  uwRows(sec, src, "equity_structure", [
    { key: "role", label: "role", opts: { text: true }, init: "General Partner (GP)" },
    { key: "share", label: "share %", opts: { pct: true } , init: 1 },
    { key: "entity", label: "entity", opts: { text: true }, init: "OODA Group" },
  ], dirty, {
    addLabel: "partner",
    footer: (arr) => {
      const sum = arr.reduce((a, e) => a + (e.share || 0), 0);
      return "Σ " + Math.round(sum * 100) + "%" + (Math.abs(sum - 1) < 0.001 ? " ✓" : " ✕ should be 100%");
    },
  });

  sec = collapsibleSection(host, "NARRATIVE", "", true);
  const ta = document.createElement("textarea");
  ta.className = "uw-narrative";
  ta.value = src.narrative_note || "";
  ta.addEventListener("change", () => { src.narrative_note = ta.value.trim(); dirty.mark(); });
  sec.append(ta);
}

// documentsSection: the documents[] list editor + drop-to-add.
function documentsSection(host, src, dirty, docSlug, docKind) {
  const sec = collapsibleSection(host, "DOCUMENTS", "", true);
  uwRows(sec, src, "documents", [
    { key: "name", label: "name", opts: { text: true } },
    { key: "category", label: "category", opts: { text: true } },
    { key: "file", label: "file", opts: { text: true } },
  ], dirty, { addLabel: "document" });
  const drop = el("div", "pp-dropzone", "drop a file to upload + add a row");
  const pick = document.createElement("input");
  pick.type = "file"; pick.hidden = true;
  const upload = async (files) => {
    if (!files || !files.length) return;
    const fd = new FormData();
    for (const f of files) fd.append("file", f);
    try {
      const res = await fetch("/api/properties/" + encodeURIComponent(docSlug) + "/docs", { method: "POST", body: fd });
      if (!res.ok) throw new Error(await res.text());
      const saved = (await res.json()).saved || [];
      saved.forEach((rel) => {
        const fn = rel.split("/").pop();
        src.documents = [...(src.documents || []), { name: fn.replace(/\.[^.]+$/, ""), category: "", file: fn }];
      });
      dirty.mark();
      showToast("Uploaded — document row added (save to keep)");
    } catch (e) { showToast("Upload failed"); }
  };
  drop.onclick = () => pick.click();
  pick.onchange = () => { upload(pick.files); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); upload(e.dataTransfer.files); });
  sec.append(drop, pick);
  sec.append(el("div", "uw-footnote", "file syncs to the site repo on publish"));
}

// propertyLevelSections: the per-parcel engine fields.
function propertyLevelSections(host, prop, dirty, opts) {
  opts = opts || {};
  const sec = host;
  uwGrid(sec, prop, [
    ["purchase_price", "purchase price", { suffix: "$" }],
    ["year_built", "year built", { int: true }],
    ["total_units", "total units", { int: true }],
    ["total_sf", "total sf", { int: true }],
    ["contingency_pct", "contingency", { pct: true, suffix: "%" }],
    ["closing_costs", "closing costs", { suffix: "$" }],
    ["carry_cost", "soft costs", { suffix: "$" }], // interest · taxes · insurance · utilities during construction
    ["construction_period_months", "constr. period", { int: true, suffix: "mo" }],
  ], dirty);
  // hard_costs: derived from the work plan once stage ests exist (syncHardCosts
  // rewrites it server-side); editable only while the plan is unestimated.
  const hardGrid = el("div", "uw-grid");
  hardGrid.append(el("span", "uw-label", "hard costs"));
  if (opts.derivedHard) {
    hardGrid.append(el("span", "uw-derived",
      "$" + Math.round(prop.hard_costs || 0).toLocaleString() + " · derived from work plan"));
  } else {
    uwGrid(sec, prop, [["hard_costs", "hard costs", { suffix: "$" }]], dirty);
  }
  if (opts.derivedHard) sec.append(hardGrid);
  sec.append(el("div", "uw-sub", "unit mix"));
  uwRows(sec, prop, "unit_mix", [
    { key: "type", label: "type", opts: { text: true }, init: "1 BR" },
    { key: "count", label: "count", opts: { int: true }, init: 1 },
    { key: "rent", label: "rent $/mo", opts: {}, init: 1200 },
  ], dirty, {
    addLabel: "unit type",
    footer: (arr) => {
      const u = arr.reduce((a, e) => a + (e.count || 0), 0);
      const r = arr.reduce((a, e) => a + (e.count || 0) * (e.rent || 0), 0);
      return "Σ " + u + "u · $" + Math.round(r).toLocaleString() + "/mo";
    },
  });
  sec.append(el("div", "uw-sub", "soft cost items (site engine only — budget these as Pre-development tasks)"));
  uwKV(sec, prop, "soft_cost_items", dirty, { suffixFor: () => "$", addLabel: "item" });
  // construction_phases: retired from the editor (§3 — the stage list IS the
  // schedule); the field itself survives untouched in source.json for the site.
}

// ---- DEAL PAGE (design §2) ----

async function renderDealPage(slug) {
  const host = els.propertyPage; host.hidden = false; host.textContent = "loading…";
  let d;
  try { d = await (await fetch("/api/deals/" + encodeURIComponent(slug))).json(); }
  catch (e) { host.innerHTML = ""; host.append(emptyRow("Deal not found.")); return; }
  host.innerHTML = "";

  const back = el("button", "pill light pp-back", "‹ board");
  back.onclick = () => { location.hash = "#/properties"; };
  host.append(back);

  const head = el("div", "pp-head");
  head.append(el("h2", "pp-title", d.deal.name));
  const chips = el("div", "pp-chips");
  chips.append(dealStatusChip(d.deal));
  const src = d.source || {};
  [src.project_type, src.tier, src.default_strategy].filter(Boolean).forEach((c) => chips.append(el("span", "pp-chip", String(c))));
  head.append(chips);
  host.append(head);

  // ACTUALS — manifest's own number; the site can't show this.
  host.append(el("div", "pp-section-head tag-manifest", "ACTUALS · MANIFEST ONLY"));
  const dm = (d.members || []).reduce((a, p) => {
    const pm = projMoney(p);
    a.budget += pm.budget; a.committed += pm.committed; a.paid += pm.paid;
    return a;
  }, { budget: 0, committed: 0, paid: 0 });
  const sum = el("div", "pp-rollup");
  sum.append(rollupStat("budget", "", fmtMoney(dm.budget)));
  sum.append(rollupStat("spent", fmtPct(dm.budget > 0 ? dm.paid / dm.budget : 0), fmtMoney(dm.paid)));
  sum.append(rollupStat("remaining", "", fmtMoney(dm.budget - dm.paid)));
  sum.append(rollupStat("ledgers", "", d.membersWithLedgers + "/" + (d.members || []).length));
  host.append(sum);
  const dtogo = el("div", "pp-togo");
  dtogo.append(el("span", "", "committed " + fmtMoney(dm.committed)));
  host.append(dtogo);

  // MEMBERS
  host.append(el("div", "pp-section-head", "MEMBERS"));
  const mbox = el("div", "pp-members");
  mbox.append(ppCols("cols-board", ["ADDRESS", "STATUS", "", "STAGE", "UNITS", "BUDGET", "SPENT"]));
  (d.members || []).forEach((p) => mbox.append(boardRow(p, true)));
  host.append(mbox);

  if (!d.source) {
    host.append(emptyRow("No source sidecar — run the -expand migration."));
    return;
  }

  // UNDERWRITING — the full engine editor over the deal-level source.
  host.append(el("div", "pp-section-head tag-engine", "UNDERWRITING · FEEDS SITE ENGINE"));
  const uw = el("div", "uw-block");
  host.append(uw);
  const orig = JSON.stringify(d.source);
  const dirty = makeDirtyBar(host,
    async () => {
      await putJSON("/api/deals/" + encodeURIComponent(slug) + "/source", d.source);
      showToast("Saved — source.json updated");
    },
    () => { renderDealPage(slug); });
  dealLevelSections(uw, d.source, dirty);
  documentsSection(uw, d.source, dirty, (d.members[0] || {}).slug || slug, "deal");

  const foot = el("div", "pp-foot");
  foot.append(pillLight("underwrite ↓", () => exportUnderwrite(slug)));
  const noteBtn = el("button", "pill light", "open note →");
  noteBtn.onclick = () => { _noteReturn = "#/properties/deal/" + encodeURIComponent(slug); openNoteByPath(d.deal.path); };
  foot.append(noteBtn);
  host.append(foot);
}

async function putJSON(url, body) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}

// ---- entity & contractor records + autocomplete (pass-5 §5) ----

let entitiesCache = null; // {entities:[], contractors:[], bindings:{}}

async function ensureEntities(force) {
  if (entitiesCache && !force) return entitiesCache;
  try { entitiesCache = await (await fetch("/api/realestate/entities")).json(); }
  catch (e) { entitiesCache = { entities: [], contractors: [], bindings: {} }; }
  return entitiesCache;
}

// recordAutocomplete: typeahead over entity/contractor records with a quiet
// `create "<name>" →` completion. Returns {el, value(), setValue(), focus()}.
function recordAutocomplete(kind, placeholder, onPick) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  const records = () => (entitiesCache ? (kind === "contractor" ? entitiesCache.contractors : entitiesCache.entities) : []) || [];
  const refresh = async () => {
    await ensureEntities();
    const q = input.value.toLowerCase().trim();
    drop.innerHTML = "";
    const hits = records().filter((r) => !q || r.name.toLowerCase().includes(q) || r.slug.includes(q)).slice(0, 10);
    hits.forEach((r) => {
      const it = el("div", "ta-item", r.name);
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.name; drop.hidden = true; if (onPick) onPick(r); };
      drop.append(it);
    });
    const exact = records().some((r) => r.name.toLowerCase() === q);
    if (q && !exact) {
      const mk = el("div", "ta-item ta-create", 'create "' + input.value.trim() + '" →');
      mk.onmousedown = async (e) => {
        e.preventDefault();
        try {
          const rec = await postJSONOk("/api/realestate/entities", { name: input.value.trim(), kind });
          await ensureEntities(true);
          input.value = rec.name;
          drop.hidden = true;
          showToast(kind + ' record created: ' + rec.name);
          if (onPick) onPick(rec);
        } catch (err) { showToast("Couldn't create " + kind); }
      };
      drop.append(mk);
    }
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; }, focus: () => input.focus() };
}

// ownerAutocomplete: the SETTINGS owners field — suggests ENTITY records and
// PEOPLE from the vault (contacts search over person notes), so "brian
// anderson" links [[brian anderson]] to his actual note. Person hits are the
// vault's own graph; entity hits come from the records.
function ownerAutocomplete(placeholder, onSet) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  let seq = 0;
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    const mySeq = ++seq;
    await ensureEntities();
    let people = [];
    if (q.length >= 2) {
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 6);
      } catch (e) {}
    }
    if (mySeq !== seq) return; // a newer keystroke superseded this fetch
    drop.innerHTML = "";
    const ents = ((entitiesCache || {}).entities || [])
      .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 6);
    ents.forEach((r) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", r.name), el("span", "ta-kind", "entity"));
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.name; drop.hidden = true; onSet(r.name); };
      drop.append(it);
    });
    people.forEach((r) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", r.display), el("span", "ta-kind", "person"));
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.key; drop.hidden = true; onSet(r.key); };
      drop.append(it);
    });
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("change", () => onSet(input.value.trim()));
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; } };
}

// contractorAutocomplete: the bid form's name field — suggests contractor
// records, PEOPLE from the vault (contacts search), and names already used
// across the ledgers, with the quiet create-record completion for new ones.
function contractorAutocomplete(placeholder, onSet) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  let seq = 0;
  const ledgerNames = () => {
    const seen = new Set();
    (propertyCache || []).forEach((p) => (p.ledger || []).forEach((r) => {
      const n = (r.contractor || r.vendor || "").trim();
      if (n) seen.add(n);
    }));
    return [...seen];
  };
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    const mySeq = ++seq;
    await ensureEntities();
    let people = [];
    if (q.length >= 2) {
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 5);
      } catch (e) {}
    }
    if (mySeq !== seq) return;
    drop.innerHTML = "";
    const add = (label, kind, value) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", label), el("span", "ta-kind", kind));
      it.onmousedown = (e) => { e.preventDefault(); input.value = value; drop.hidden = true; if (onSet) onSet(value); };
      drop.append(it);
    };
    const dedupe = new Set();
    (((entitiesCache || {}).contractors) || [])
      .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 5)
      .forEach((r) => { dedupe.add(r.name.toLowerCase()); add(r.name, "contractor", r.name); });
    ledgerNames().filter((n) => (!q || n.toLowerCase().includes(q)) && !dedupe.has(n.toLowerCase())).slice(0, 5)
      .forEach((n) => { dedupe.add(n.toLowerCase()); add(n, "history", n); });
    people.filter((r) => !dedupe.has(r.key)).forEach((r) => add(r.display, "person", r.key));
    const exact = dedupe.has(q) || people.some((r) => r.key === q);
    if (q && !exact) {
      const mk = el("div", "ta-item ta-create", 'create contractor "' + input.value.trim() + '" →');
      mk.onmousedown = async (e) => {
        e.preventDefault();
        try {
          const rec = await postJSONOk("/api/realestate/entities", { name: input.value.trim(), kind: "contractor" });
          await ensureEntities(true);
          input.value = rec.name;
          drop.hidden = true;
          showToast("Contractor record created: " + rec.name);
          if (onSet) onSet(rec.name);
        } catch (err) { showToast("Couldn't create contractor"); }
      };
      drop.append(mk);
    }
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; }, focus: () => input.focus() };
}

// ---- STATEMENT WORKBENCH (design §4) ----

let stmtRows = [];
let stmtFilter = "pending"; // pending shows pending+assigned+split

async function renderAccounting() {
  const host = els.propertyStatements; host.hidden = false; host.innerHTML = "loading…";
  let d;
  try { d = await (await fetch("/api/realestate/statements")).json(); }
  catch (e) { host.innerHTML = ""; host.append(emptyRow("Statements unavailable.")); return; }
  stmtRows = d.rows || [];
  if (!propertyCache.length) { try { const pd = await (await fetch("/api/properties")).json(); propertyCache = pd.properties || []; } catch (e) {} }
  host.innerHTML = "";

  const counts = { pending: 0, assigned: 0, split: 0, applied: 0, skipped: 0 };
  stmtRows.forEach((r) => { counts[r.state] = (counts[r.state] || 0) + 1; });
  els.propertiesMeta.textContent = counts.pending + " unassigned · " + counts.skipped + " skipped" +
    (d.lastImport ? " · last import " + d.lastImport : "");

  // upload zone (always present)
  const drop = el("div", "pp-dropzone", "drop a bank csv — or click to pick");
  const pick = document.createElement("input");
  pick.type = "file"; pick.accept = ".csv,text/csv"; pick.hidden = true;
  drop.onclick = () => pick.click();
  const mapHost = el("div", "stmt-maphost");
  const doUpload = async (file) => {
    if (!file) return;
    const fd = new FormData();
    fd.append("file", file);
    drop.textContent = "parsing…";
    try {
      const res = await fetch("/api/realestate/statements/upload", { method: "POST", body: fd });
      if (!res.ok) throw new Error(await res.text());
      renderStmtMapping(mapHost, await res.json());
    } catch (e) { showToast("Couldn't parse csv"); }
    drop.textContent = "drop a bank csv — or click to pick";
  };
  pick.onchange = () => { doUpload(pick.files[0]); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); doUpload(e.dataTransfer.files[0]); });
  host.append(drop, pick, mapHost);

  // grouping auto-suggest (pass-5): pending rows from one vendor whose sum
  // matches an accepted bid (±1%) → one-click tether-all. Merge = grouping,
  // never destruction — the rows stay verbatim, they just share the tether.
  const pendingRows = stmtRows.filter((r) => r.state === "pending" && r.vendor);
  const byVendor = new Map();
  pendingRows.forEach((r) => {
    const k = r.vendor.toLowerCase();
    if (!byVendor.has(k)) byVendor.set(k, []);
    byVendor.get(k).push(r);
  });
  for (const [vk, rows] of byVendor) {
    if (rows.length < 2) continue;
    const sum = rows.reduce((s, r) => s + r.amount, 0);
    let match = null;
    (propertyCache || []).forEach((p) => {
      (p.ledger || []).forEach((lr) => {
        if (lr.type === "bid" && lr.status === "accepted" && lr.workId &&
            (lr.contractor || lr.vendor || "").toLowerCase().includes(vk.slice(0, 8)) &&
            Math.abs(lr.amount - sum) <= lr.amount * 0.01) {
          match = { p, lr };
        }
      });
    });
    if (!match) continue;
    const hint = el("div", "stmt-suggest");
    hint.append(el("span", "", rows.length + " rows sum " + fmtMoney(sum) + " = accepted bid on " +
      match.lr.workId.split("/").pop() + " (" + (match.p.short || match.p.address || match.p.slug) + ")"));
    const go = el("button", "stmt-hint stmt-echo", "group →");
    go.onclick = async () => {
      for (const r of rows) {
        await patchStmt(r, { category: r.category || match.lr.category, assignments: [{ slug: match.p.slug, amount: r.amount, workId: match.lr.workId }] }, true);
      }
      renderAccounting();
    };
    hint.append(go);
    host.append(hint);
  }

  // state filter chips
  const chips = el("div", "stmt-chips");
  [["pending", "PENDING"], ["applied", "APPLIED"], ["skipped", "SKIPPED"]].forEach(([val, label]) => {
    const c = el("button", "filter-chip" + (stmtFilter === val ? " on" : ""), label);
    c.onclick = () => { stmtFilter = val; renderAccounting(); };
    chips.append(c);
  });
  host.append(chips);

  // rows grouped by statement label
  const list = el("div", "stmt-list");
  const show = stmtRows.filter((r) =>
    stmtFilter === "pending" ? (r.state === "pending" || r.state === "assigned" || r.state === "split")
      : r.state === stmtFilter);
  const byStmt = new Map();
  show.forEach((r) => {
    const k = (r.statement || "earlier") + " · imported " + (r.imported || "");
    if (!byStmt.has(k)) byStmt.set(k, []);
    byStmt.get(k).push(r);
  });
  if (!show.length) list.append(emptyRow(stmtFilter === "pending" ? "Nothing pending — drop a statement above." : "Nothing here."));
  for (const [label, rows] of byStmt) {
    list.append(el("div", "pp-section-head", label.toUpperCase()));
    list.append(ppCols("cols-stmt", ["DATE", "DESCRIPTION", "AMOUNT", "CATEGORY", "PROPERTY", "STATE"]));
    rows.forEach((r) => list.append(stmtRowEl(r)));
  }
  host.append(list);

  // sticky apply footer
  const applicable = stmtRows.filter((r) => r.state === "assigned" || r.state === "split");
  if (applicable.length) {
    const bar = el("div", "dirty-bar");
    bar.append(el("span", "dirty-label",
      stmtRows.filter((r) => r.state === "assigned").length + " ASSIGNED · " +
      stmtRows.filter((r) => r.state === "split").length + " SPLIT · " +
      counts.pending + " PENDING"));
    const apply = el("button", "pill", "apply " + applicable.length + " rows");
    apply.onclick = async () => {
      apply.disabled = true;
      try {
        const res = await postJSONOk("/api/realestate/statements/apply", { ids: applicable.map((r) => r.id) });
        showToast("Applied " + res.applied + " rows (" + res.lines + " lines across " + res.properties + " properties)");
        renderAccounting();
      } catch (e) { showToast("Apply failed: " + (e.message || "").slice(0, 80)); apply.disabled = false; }
    };
    bar.append(apply);
    host.append(bar);
  }
}

// renderStmtMapping: column mapping over an uploaded csv → ingest to the lot.
function renderStmtMapping(host, pre) {
  host.innerHTML = "";
  const panel = el("div", "import-panel");
  panel.append(el("div", "pp-section-head", "MAP COLUMNS · " + pre.label + (pre.remembered ? " (remembered)" : "")));
  const selects = {};
  const mapRow = el("div", "import-maprow");
  ["date", "amount", "vendor", "note"].forEach((field) => {
    const lab = el("label", "portal-field");
    lab.append(el("span", "portal-field-label", field));
    const sel = selectEl(field === "note" ? ["—", ...pre.headers] : pre.headers);
    if (pre.mapping && pre.mapping[field]) sel.value = pre.mapping[field];
    selects[field] = sel;
    lab.append(sel);
    mapRow.append(lab);
  });
  const flip = document.createElement("input"); flip.type = "checkbox";
  const flipLab = el("label", "import-flip"); flipLab.append(flip, el("span", "", " debits are negative (flip sign)"));
  mapRow.append(flipLab);
  // pass-5: every upload binds to the paying entity (remembered per source label)
  const entLab = el("label", "portal-field");
  entLab.append(el("span", "portal-field-label", "paying entity"));
  const entAC = recordAutocomplete("entity", "entity account…");
  if (pre.entity) entAC.setValue(pre.entity);
  entLab.append(entAC.el);
  mapRow.append(entLab);
  panel.append(mapRow);
  const ingest = el("button", "pill", "add to workbench");
  const cancel = el("button", "pill light", "✕");
  cancel.onclick = () => { host.innerHTML = ""; };
  ingest.onclick = async () => {
    const col = (f) => pre.headers.indexOf(selects[f].value);
    const di = col("date"), ai = col("amount"), vi = col("vendor");
    const ni = selects.note.value === "—" ? -1 : pre.headers.indexOf(selects.note.value);
    const rows = pre.rows.map((raw) => {
      let amt = parseFloat(String(raw[ai] || "").replace(/[$,]/g, "")) || 0;
      if (flip.checked) amt = -amt;
      return {
        date: normDate(raw[di] || ""), amount: Math.round(amt * 100) / 100,
        vendor: (raw[vi] || "").trim(), note: ni >= 0 ? (raw[ni] || "").trim() : "",
      };
    }).filter((r) => r.date && r.amount > 0);
    if (!entAC.value()) { showToast("Pick the paying entity first"); entAC.focus(); return; }
    ingest.disabled = true;
    try {
      const mapping = { date: selects.date.value, amount: selects.amount.value, vendor: selects.vendor.value, note: selects.note.value };
      const res = await postJSONOk("/api/realestate/statements/ingest",
        { label: pre.label, entity: entAC.value(), signature: pre.signature, mapping, rows });
      showToast("Added " + res.added + " rows (" + res.duplicates + " duplicates skipped)");
      host.innerHTML = "";
      renderAccounting();
    } catch (e) { showToast("Ingest failed"); ingest.disabled = false; }
  };
  const foot = el("div", "import-foot");
  foot.append(ingest, cancel);
  panel.append(foot);
  host.append(panel);
}

// stmtRowEl: one workbench row — category input, property typeahead, split
// block, skip link, vendor-echo bulk hint.
function stmtRowEl(r) {
  const wrap = el("div", "stmt-wrap");
  const row = el("div", "stmt-row state-" + r.state);
  row.append(el("span", "import-date", r.date));
  row.append(el("span", "stmt-vendor", r.vendor + (r.note ? "  · " + r.note : "")));
  row.append(el("span", "pp-amt", fmtMoney(r.amount)));

  const readOnly = r.state === "applied";
  if (readOnly) {
    row.append(el("span", "", r.category || ""));
    row.append(el("span", "", (r.assignments || []).map((a) => a.slug).join(" · ")));
    row.append(el("span", "stmt-state", r.state));
    wrap.append(row);
    return wrap;
  }

  const cat = inputEl("category");
  cat.classList.add("import-cat");
  cat.value = r.category || "";
  cat.addEventListener("change", () => patchStmt(r, { category: cat.value }));
  row.append(cat);

  const propCell = el("span", "stmt-prop");
  const single = (r.assignments || []).length === 1 ? r.assignments[0] : null;
  const isAdmin = single && single.slug.startsWith("admin:");
  propCell.append(propertyTypeahead("property…", (p) => {
    patchStmt(r, { assignments: [{ slug: p.slug, amount: r.amount, workId: (single && single.workId) || "", cat: (single && single.cat) || "" }] });
  }, single && !isAdmin ? single.slug : ""));
  // budget category lane: hard (default, tetherable) | soft | acquisition
  if (single && !isAdmin) {
    const catSel = document.createElement("select");
    catSel.className = "pp-in lg-cat";
    catSel.title = "budget category (soft = interest · taxes · insurance · utilities)";
    [["", "hard"], ["soft", "soft"], ["acquisition", "acquisition"]].forEach(([v, l]) => {
      const o = document.createElement("option"); o.value = v; o.textContent = l; catSel.append(o);
    });
    catSel.value = single.cat || "";
    catSel.onchange = () => patchStmt(r, { assignments: [{ slug: single.slug, amount: single.amount,
      workId: catSel.value ? "" : (single.workId || ""), cat: catSel.value }] });
    propCell.append(catSel);
  }
  // work tether: once a real property is assigned, offer its open todos (hard lane only)
  if (single && !isAdmin && !single.cat) {
    const prop = propertyCache.find((p) => p.slug === single.slug);
    if (prop && (prop.work || []).length) {
      const sel = document.createElement("select");
      sel.className = "pp-in lg-work";
      sel.title = "work tether";
      const opts = [["", "⚲ —"]];
      (prop.work || []).forEach((st) => {
        (st.todos || []).forEach((td) => { if (!td.checked || td.id === single.workId) opts.push([td.id, st.text + " · " + td.text]); });
        opts.push([st.id, st.text + " (stage)"]);
      });
      opts.forEach(([v, l]) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); });
      sel.value = single.workId || "";
      sel.onchange = () => patchStmt(r, { assignments: [{ slug: single.slug, amount: single.amount, workId: sel.value }] });
      propCell.append(sel);
    }
  }
  const adminBtn = el("button", "stmt-split-btn", isAdmin ? "admin ✓" : "admin");
  adminBtn.title = "assign to an entity's admin ledger instead of a property";
  adminBtn.onclick = () => toggleAdminForm(wrap, r);
  propCell.append(adminBtn);
  const splitBtn = el("button", "stmt-split-btn", "split ⑂");
  splitBtn.onclick = () => renderSplitBlock(wrap, r);
  propCell.append(splitBtn);
  row.append(propCell);

  const stateCell = el("span", "stmt-state st-" + r.state, r.state);
  row.append(stateCell);
  wrap.append(row);

  // quiet second line: remembered hint · vendor echo · skip
  const hints = el("div", "stmt-hints");
  if (r.remembered) hints.append(el("span", "stmt-hint", "↳ remembered"));
  if (r.state === "assigned" || r.state === "split") {
    const others = stmtRows.filter((o) => o.state === "pending" && o.vendor && o.vendor === r.vendor);
    if (others.length) {
      const echo = el("button", "stmt-hint stmt-echo", "apply to " + others.length + " more from " + r.vendor + " →");
      echo.onclick = async () => {
        for (const o of others) {
          await patchStmt(o, { category: r.category, assignments: r.assignments }, true);
        }
        renderAccounting();
      };
      hints.append(echo);
    }
  }
  const skip = el("button", "stmt-hint", r.state === "skipped" ? "unskip" : "skip");
  skip.onclick = () => patchStmt(r, { state: r.state === "skipped" ? "pending" : "skipped" });
  hints.append(skip);
  wrap.append(hints);

  if ((r.assignments || []).length > 1) renderSplitBlock(wrap, r, true);
  return wrap;
}

// toggleAdminForm: the admin lane — entity + category (from that entity's
// admin-categories list) → assignment slug "admin:<entity-slug>".
async function toggleAdminForm(wrap, r) {
  let form = wrap.querySelector(".admin-form");
  if (form) { form.remove(); return; }
  await ensureEntities();
  form = el("div", "stmt-splits admin-form");
  const entAC = recordAutocomplete("entity", "entity…", (rec) => {
    catSel.innerHTML = "";
    const ent = (entitiesCache.entities || []).find((e) => e.slug === rec.slug || e.name === rec.name);
    (((ent || {}).adminCategories) || ["admin"]).forEach((c) => {
      const o = document.createElement("option"); o.value = c; o.textContent = c; catSel.append(o);
    });
  });
  if (r.entity) entAC.setValue(r.entity);
  const catSel = document.createElement("select");
  catSel.className = "pp-in";
  const label = inputEl("label (optional)");
  const set = el("button", "pill lg-add", "assign to admin");
  set.onclick = async () => {
    const ent = entAC.value();
    if (!ent) { showToast("Pick the entity"); return; }
    const slugified = ent.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    await patchStmt(r, {
      category: catSel.value || r.category || "admin",
      assignments: [{ slug: "admin:" + slugified, amount: r.amount }],
    }, true);
    if (label.value.trim()) await patchStmt(r, { category: catSel.value + " · " + label.value.trim() }, true);
    renderAccounting();
  };
  form.append(entAC.el, catSel, label, set);
  wrap.append(form);
}

async function patchStmt(r, patch, silent) {
  try {
    await postJSONOk("/api/realestate/statements/row", { id: r.id, ...patch });
    if (!silent) renderAccounting();
  } catch (e) { showToast("Couldn't update row"); }
}

// renderSplitBlock: inline allocation sub-rows — property + amount each, ÷
// evenly, live remainder (nonzero keeps the row pending).
function renderSplitBlock(wrap, r, initialOnly) {
  let block = wrap.querySelector(".stmt-splits");
  if (block) { if (initialOnly) return; block.remove(); return; }
  block = el("div", "stmt-splits");
  const allocs = (r.assignments || []).length ? r.assignments.map((a) => ({ ...a })) : [{ slug: "", amount: r.amount }];
  const render = () => {
    block.innerHTML = "";
    allocs.forEach((a, i) => {
      const line = el("div", "stmt-split-line");
      line.append(propertyTypeahead("property…", (p) => { a.slug = p.slug; commit(); }, a.slug));
      const amt = inputEl("amount");
      amt.type = "number"; amt.step = "0.01"; amt.value = a.amount || "";
      amt.addEventListener("change", () => { a.amount = parseFloat(amt.value) || 0; commit(); });
      line.append(amt);
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { allocs.splice(i, 1); commit(); };
      line.append(x);
      block.append(line);
    });
    const foot = el("div", "stmt-split-foot");
    const add = el("button", "o-ghost", "＋ property");
    add.onclick = () => { allocs.push({ slug: "", amount: 0 }); render(); };
    const even = el("button", "stmt-hint", "÷ evenly");
    even.onclick = () => {
      const per = Math.floor((r.amount / allocs.length) * 100) / 100;
      allocs.forEach((a, i) => { a.amount = i === allocs.length - 1 ? Math.round((r.amount - per * (allocs.length - 1)) * 100) / 100 : per; });
      commit();
    };
    const sum = allocs.reduce((s, a) => s + (a.amount || 0), 0);
    const rem = Math.round((r.amount - sum) * 100) / 100;
    const remEl = el("span", "stmt-remainder" + (Math.abs(rem) < 0.01 ? " ok" : " bad"),
      "$" + rem.toFixed(2) + " remaining" + (Math.abs(rem) < 0.01 ? " ✓" : ""));
    foot.append(add, even, remEl);
    block.append(foot);
  };
  const commit = async () => {
    const clean = allocs.filter((a) => a.slug);
    await patchStmt(r, { assignments: clean }, true);
    r.assignments = clean;
    render();
  };
  render();
  wrap.append(block);
}

// ---- WORK section (pass-4): stages · todos · tethered money ----

let pendingLedgerPrefill = null; // {type, workId} — set by "bid →"/"$ →", consumed by ledgerEntryRow
const workOpen = {}; // per-render expanded non-current stages (slug:stageId → bool)

async function workOp(slug, body) {
  try {
    await postJSONOk("/api/properties/" + encodeURIComponent(slug) + "/work", body);
    renderPropertyPage(slug);
  } catch (e) { showToast((e.message || "Work update failed").slice(0, 80)); }
}

// workBlock renders the management core in the goals-trail idiom: done stages
// struck ✓, the current stage → in blue and open, others collapsed.
function workBlock(p) {
  const wrap = el("div", "work-block");
  if (!(p.work || []).length) {
    const seed = el("div", "work-seed");
    seed.append(el("span", "pp-empty", "No work plan yet — seed stages:"));
    const opts = [["rehab", "rehab (8)"], ["new-build", "new build (9)"], ["phases", "from site phases"], ["empty", "start empty"]];
    opts.forEach(([tpl, label]) => {
      const b = pillLight(label, () => workOp(p.slug, { op: "seed", template: tpl }));
      if ((p.kind === "rehab" && tpl === "rehab") || (p.kind === "new-construction" && tpl === "new-build")) b.classList.add("suggested");
      seed.append(b);
    });
    wrap.append(seed);
    return wrap;
  }

  p.work.forEach((st) => {
    const open = st.current || workOpen[p.slug + ":" + st.id];
    const stageEl = el("div", "work-stage" + (st.checked ? " done" : "") + (st.current ? " cur" : ""));
    const head = el("div", "work-stage-head");
    const check = el("button", "check wk-check" + (st.checked ? " on" : ""), st.checked ? "✓" : "○");
    check.title = st.checked ? "reopen stage" : st.ready ? "stage ready — mark complete" : "mark stage complete";
    if (st.ready && !st.checked) check.classList.add("ready");
    check.onclick = (e) => { e.stopPropagation(); workOp(p.slug, { op: "check", id: st.id, checked: !st.checked }); };
    head.append(check);
    head.append(el("span", "work-stage-name", (st.current ? "→ " : "") + st.text));
    if (st.ready && !st.checked) head.append(el("span", "work-ready", "ready"));
    // stage money triplet: est (click-to-edit the stage's OWN est — the
    // not-yet-broken-down remainder) · committed · paid + unestimated count
    const money = el("span", "work-money");
    money.append(estSlot(p, st.id, st.est, st.estTotal, true));
    if (st.committed > 0 || st.paid > 0) money.append(el("span", "", " · " + fmtMoney(st.committed) + " committed · " + fmtMoney(st.paid) + " paid"));
    if (st.unestimated > 0) money.append(el("span", "work-unest", st.unestimated + " unestimated"));
    money.onclick = (e) => e.stopPropagation();
    head.append(money);
    const span = (p.schedule || []).find((x) => x.id === st.id);
    if (span) head.append(el("span", "work-span", span.start.slice(5) + " → " + span.end.slice(5) + (span.pinned ? " ✓" : "")));
    if (!st.current) {
      head.classList.add("toggle");
      head.append(el("span", "o-st-caret", open ? "▾" : "▸"));
      head.onclick = () => { workOpen[p.slug + ":" + st.id] = !open; renderPropertyPage(p.slug); };
    }
    // hover delete for stages (inline y/n)
    head.append(workDeleteBtn(p, st.id));
    stageEl.append(head);

    if (open) {
      const list = el("div", "work-todos");
      (st.todos || []).forEach((td) => list.append(workTodoRow(p, st, td)));
      list.append(todoComposer(p, st));
      stageEl.append(list);
    }
    wrap.append(stageEl);
  });
  wrap.append(ghostInput("＋ stage", "work-add-stage", (v) => workOp(p.slug, { op: "add-stage", text: v }), "stage name…"));
  return wrap;
}

// todoComposer: the "＋ todo" ghost expands to text + optional $ price +
// optional who — one flow for the common case "task + firm price". Price
// alone = [est::]; price + who = firm (accepted bid tethered to the new task).
function todoComposer(p, st) {
  const ghost = el("button", "o-ghost work-add", "＋ todo");
  ghost.onclick = (e) => {
    e.stopPropagation();
    const box = el("div", "todo-compose");
    const text = inputEl("what needs to happen…");
    text.classList.add("o-edit", "tc-text");
    const amt = inputEl("$ price"); amt.type = "number"; amt.step = "1"; amt.classList.add("est-in");
    const who = contractorAutocomplete("who (firm)…");
    const cancel = () => box.replaceWith(ghost);
    const save = async () => {
      const t = text.value.trim();
      if (!t) { cancel(); return; }
      const amount = parseFloat(amt.value) || 0;
      const name = who.value();
      try {
        const resp = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
          { op: "add-todo", stageId: st.id, text: t });
        if (amount) {
          // find the new todo's id in the response (last text match in this stage)
          const stage = ((resp && resp.work) || []).find((s) => s.id === st.id);
          const match = ((stage && stage.todos) || []).filter((td) => td.text === t).pop();
          if (match) {
            await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
              { op: "set-field", id: match.id, field: "est", value: String(amount) });
            if (name) {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger",
                { type: "bid", status: "accepted", contractor: name, amount, workId: match.id });
            }
          }
        }
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Couldn't add task").slice(0, 80)); }
    };
    [text, amt, who.el.querySelector("input")].forEach((inp) => inp.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      else if (ev.key === "Escape") cancel();
    }));
    const add = el("button", "pill lg-add", "add");
    add.onclick = save;
    box.append(text, amt, who.el, add);
    ghost.replaceWith(box);
    text.focus();
  };
  return ghost;
}

function workTodoRow(p, st, td) {
  const row = el("div", "work-todo" + (td.checked ? " done" : ""));
  const check = el("button", "check wk-check" + (td.checked ? " on" : ""), td.checked ? "✓" : "○");
  check.onclick = (e) => { e.stopPropagation(); workOp(p.slug, { op: "check", id: td.id, checked: !td.checked }); };
  row.append(check);

  const label = el("span", "work-todo-text", td.text);
  label.title = "click to edit";
  label.onclick = () => {
    const input = inputEl(""); input.value = td.text; input.classList.add("work-edit");
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) workOp(p.slug, { op: "edit", id: td.id, text: input.value });
      else if (ev.key === "Escape") input.replaceWith(label);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(label); });
    label.replaceWith(input);
    input.focus();
  };
  row.append(label);

  // ONE price slot: paid → firm → est → empty (firm = accepted bid under the hood)
  row.append(priceSlot(p, td));

  // requested/received bid chips still render (accept/decline); accepted is the slot's job
  (td.bids || []).forEach((b) => {
    if (b.status === "accepted") return;
    row.append(bidChipEl(p, b));
  });

  const acts = el("span", "work-acts");
  const more = el("button", "uw-x work-more-btn", "⋯");
  more.title = "more…";
  more.onclick = (e) => {
    e.stopPropagation();
    const open = acts.querySelector(".work-more");
    if (open) { open.remove(); return; }
    const m = quietBtn("request bid…", () => { m.remove(); toggleBidForm(row, p, st, td); });
    m.classList.add("work-more");
    more.after(m);
  };
  acts.append(more);
  acts.append(workDeleteBtn(p, td.id));
  row.append(acts);
  return row;
}

// priceSlot: one money field per task. A number alone = estimate (budget); a
// number + name = FIRM price — written as an accepted bid tethered to the
// task, so committed and payment draws work unchanged. Editing a firm price
// mutates the SAME ledger row (fail-closed), never duplicates.
function priceSlot(p, td) {
  const firm = (td.bids || []).find((b) => b.status === "accepted");
  let cls = "est-slot", label;
  if (td.paid > 0) { cls += " firm"; label = fmtMoney(td.paid) + " paid / " + fmtMoney(td.committed || td.paid); }
  else if (firm) { cls += " firm"; label = fmtMoney(firm.amount) + (firm.who ? " · " + firm.who : ""); }
  else if (td.committed > 0) { cls += " firm"; label = fmtMoney(td.committed) + " committed"; }
  else if (td.est > 0) label = "est " + fmtMoney(td.est);
  else { cls += " empty"; label = "$ —"; }
  if (td.checked && (td.unreconciled || 0) > 0) { cls += " unrec"; label = "⚑ " + label; }
  const slot = el("button", cls, label);
  slot.title = td.checked && (td.unreconciled || 0) > 0
    ? "done — " + fmtMoney(td.unreconciled) + " unreconciled: link the bank payment in the statement workbench"
    : "price — number alone = estimate · add a name = firm (committed)";
  slot.onclick = (e) => {
    e.stopPropagation();
    const box = el("span", "price-edit");
    const amt = inputEl("$"); amt.type = "number"; amt.step = "1"; amt.classList.add("est-in");
    if (firm) amt.value = firm.amount; else if (td.est > 0) amt.value = td.est;
    const who = contractorAutocomplete("who (firm)…");
    if (firm && firm.who) who.setValue(firm.who);
    const save = async () => {
      const amount = parseFloat(amt.value) || 0;
      const name = who.value();
      try {
        if (amount && name) {
          if (firm) {
            if (firm.amount !== amount || (firm.who || "") !== name) {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate",
                { original: firm.row, replacement: { ...firm.row, contractor: name, amount } });
            }
          } else {
            await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger",
              { type: "bid", status: "accepted", contractor: name, amount, workId: td.id });
          }
          if (!(td.est > 0)) { // plan defaults to the firm price; never overwrite an estimate
            await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
              { op: "set-field", id: td.id, field: "est", value: String(amount) });
          }
          renderPropertyPage(p.slug);
        } else {
          workOp(p.slug, { op: "set-field", id: td.id, field: "est", value: amt.value.trim() });
        }
      } catch (err) { showToast((err.message || "Couldn't save price").slice(0, 80)); }
    };
    amt.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      else if (ev.key === "Escape") box.replaceWith(slot);
    });
    who.el.querySelector("input").addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      else if (ev.key === "Escape") box.replaceWith(slot);
    });
    const ok = el("button", "pill lg-add", "set");
    ok.onclick = save;
    box.append(amt, who.el, ok);
    slot.replaceWith(box);
    amt.focus();
  };
  return slot;
}

// estSlot: the inline estimate field on every work row. Shows the value (or a
// muted "est —"); click → type → Enter writes [est:: N] (empty clears). For
// stages, `total` may include todo sums — display shows the total, editing
// edits the stage's OWN est.
function estSlot(p, id, own, total, isStage) {
  const label = total > 0 ? "est " + fmtMoney(total) : "est —";
  const slot = el("button", "est-slot" + (total > 0 ? "" : " empty"), label);
  slot.title = isStage ? "stage estimate (own, on top of todo estimates)" : "estimate";
  slot.onclick = (e) => {
    e.stopPropagation();
    const input = inputEl("est $");
    input.type = "number"; input.step = "1"; input.classList.add("est-in");
    if (own > 0) input.value = own;
    const save = () => workOp(p.slug, { op: "set-field", id, field: "est", value: input.value.trim() });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      else if (ev.key === "Escape") input.replaceWith(slot);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(slot); });
    slot.replaceWith(input);
    input.focus();
  };
  return slot;
}

function quietBtn(text, onclick) {
  const b = el("button", "stmt-hint", text);
  b.onclick = (e) => { e.stopPropagation(); onclick(); };
  return b;
}

function workDeleteBtn(p, id) {
  const holder = el("span", "work-del");
  const x = el("button", "uw-x", "✕");
  x.title = "remove";
  x.onclick = (e) => {
    e.stopPropagation();
    // cascade preview: tethered bids die with the node; paid expenses survive
    let bids = 0, paid = 0;
    (p.ledger || []).forEach((r) => {
      if (!r.workId || (r.workId !== id && !r.workId.startsWith(id + "/"))) return;
      if (r.type === "bid") bids += r.amount;
      else if (r.type === "expense") paid += r.amount;
    });
    let ask = "delete?";
    if (bids > 0 && paid > 0) ask = "delete? (removes " + fmtMoney(bids) + " bid · " + fmtMoney(paid) + " paid stays)";
    else if (bids > 0) ask = "delete? (removes " + fmtMoney(bids) + " bid)";
    else if (paid > 0) ask = "delete? (" + fmtMoney(paid) + " paid stays in ledger)";
    const yes = quietBtn(ask, () => workOp(p.slug, { op: "delete", id }));
    const no = quietBtn("no", () => { yes.remove(); no.remove(); x.hidden = false; });
    x.hidden = true;
    holder.append(yes, no);
  };
  holder.append(x);
  return holder;
}

// openTodoOptions builds the ledger picker options: open todos first, then the
// current row's tether even if checked (so edits don't lose it).
function openTodoOptions(p, includeId) {
  const opts = [["", "— no work link —"]];
  (p.work || []).forEach((st) => {
    (st.todos || []).forEach((td) => {
      if (!td.checked || td.id === includeId) opts.push([td.id, st.text + " · " + td.text]);
    });
    if (includeId && st.id === includeId) opts.push([st.id, st.text + " (stage)"]);
  });
  return opts;
}

// ---- CONTRACTORS sub-tab: the contractor DB, searchable by trade ----

const TRADES = ["masonry", "roofing", "plumbing", "electrical", "hvac", "framing",
  "drywall", "flooring", "painting", "concrete", "windows", "gc", "landscaping", "other"];
let contractorSearch = "";
let contractorTrade = "";

async function renderContractors() {
  const host = els.propertyContractors; host.hidden = false; host.innerHTML = "loading…";
  await ensureEntities(true);
  if (!propertyCache.length) {
    try { const d = await (await fetch("/api/properties")).json(); propertyCache = d.properties || []; } catch (e) {}
  }
  host.innerHTML = "";
  const list = (entitiesCache || {}).contractors || [];
  els.propertiesMeta.textContent = list.length + " contractors";

  const bar = el("div", "board-bar");
  const search = inputEl("search name…");
  search.classList.add("board-search");
  search.value = contractorSearch;
  search.addEventListener("input", () => { contractorSearch = search.value; body.replaceWith(body = table()); });
  bar.append(search);
  const tradesPresent = [...new Set(list.map((c) => c.trade).filter(Boolean))].sort();
  const sel = document.createElement("select");
  sel.className = "pp-in board-select";
  [["", "all trades"], ...tradesPresent.map((t) => [t, t])].forEach(([v, l]) => {
    const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o);
  });
  sel.value = contractorTrade;
  sel.onchange = () => { contractorTrade = sel.value; body.replaceWith(body = table()); };
  bar.append(sel);
  host.append(bar);

  // derived money per contractor from the ledgers (same source as autocomplete history)
  const stats = (name) => {
    const s = { props: new Set(), paid: 0, openBids: 0 };
    (propertyCache || []).forEach((p) => (p.ledger || []).forEach((r) => {
      const who = (r.contractor || r.vendor || "").trim().toLowerCase();
      if (who !== name.toLowerCase()) return;
      s.props.add(p.short || p.address || p.slug);
      if (r.type === "expense") s.paid += r.amount;
      if (r.type === "bid" && (r.status === "requested" || r.status === "received")) s.openBids++;
    }));
    return s;
  };

  const table = () => {
    const box = el("div", "contractor-table");
    const rows = list.filter((c) =>
      (!contractorSearch || c.name.toLowerCase().includes(contractorSearch.toLowerCase())) &&
      (!contractorTrade || c.trade === contractorTrade));
    if (!rows.length) { box.append(emptyRow("No contractors match.")); return box; }
    box.append(ppCols("cols-contractors", ["NAME", "TRADE", "PROPERTIES", "TOTAL PAID", "OPEN BIDS"]));
    rows.forEach((c) => {
      const s = stats(c.name);
      const row = el("div", "property-row cols-contractors");
      const nm = el("span", "property-addr", c.name);
      row.onclick = () => { _noteReturn = "#/properties/contractors"; openNoteByPath(c.path); };
      // inline trade picker — the backfill surface
      const tr = document.createElement("select");
      tr.className = "pp-in ct-trade";
      [["", "—"], ...TRADES.map((t) => [t, t])].forEach(([v, l]) => {
        const o = document.createElement("option"); o.value = v; o.textContent = l; tr.append(o);
      });
      if (c.trade && !TRADES.includes(c.trade)) { const o = document.createElement("option"); o.value = c.trade; o.textContent = c.trade; tr.append(o); }
      tr.value = c.trade || "";
      tr.onclick = (e) => e.stopPropagation();
      tr.onchange = async () => {
        try {
          await postJSONOk("/api/realestate/contractors/" + encodeURIComponent(c.slug), { trade: tr.value });
          c.trade = tr.value;
          showToast("Trade set: " + (tr.value || "—"));
        } catch (e) { showToast("Couldn't set trade"); }
      };
      row.append(nm, tr,
        el("span", "", [...s.props].slice(0, 3).join(" · ") + (s.props.size > 3 ? " +" + (s.props.size - 3) : "")),
        el("span", "pp-amt", s.paid ? fmtMoney(s.paid) : ""),
        el("span", "pp-amt", s.openBids ? String(s.openBids) : ""));
      box.append(row);
    });
    return box;
  };
  let body = table();
  host.append(body);
}

// ---- SETTINGS sub-tab (pass-5 §6) ----

async function renderREsettings() {
  const host = els.propertySettings; host.hidden = false; host.innerHTML = "loading…";
  await ensureEntities(true);
  if (!propertyCache.length) { try { const d = await (await fetch("/api/properties")).json(); propertyCache = d.properties || []; templateCache = d.templates || []; } catch (e) {} }
  host.innerHTML = "";
  const ents = entitiesCache.entities || [];

  // ENTITIES — list, create, owners, admin categories
  host.append(el("div", "pp-section-head", "ENTITIES"));
  const list = el("div", "set-entities");
  ents.forEach((e) => list.append(entityCard(e, ents)));
  list.append(ghostInput("＋ entity", "set-add", async (v) => {
    try { await postJSONOk("/api/realestate/entities", { name: v, kind: "entity" }); renderREsettings(); }
    catch (err) { showToast("Couldn't create entity"); }
  }, "entity name…"));
  host.append(list);

  // ORG CHART — ownership tree read live from the records
  host.append(el("div", "pp-section-head", "ORG CHART"));
  host.append(orgChart(ents));

  // STATEMENT ACCOUNTS — source-label → entity bindings
  host.append(el("div", "pp-section-head", "STATEMENT ACCOUNTS"));
  const bind = el("div", "set-bindings");
  const bindings = entitiesCache.bindings || {};
  const keys = Object.keys(bindings);
  if (!keys.length) bind.append(el("div", "pp-empty", "No bindings yet — they're remembered when you upload statements."));
  keys.sort().forEach((label) => {
    const row = el("div", "set-bind-row");
    row.append(el("span", "stmt-vendor", label));
    const ac = recordAutocomplete("entity", "entity…");
    ac.setValue(bindings[label]);
    row.append(ac.el);
    row.append(pillLight("save", async () => {
      try { await postJSONOk("/api/realestate/bindings", { label, entity: ac.value() }); showToast("Binding saved"); }
      catch (e) { showToast("Couldn't save binding"); }
    }));
    bind.append(row);
  });
  host.append(bind);

  // TEMPLATES — open in the note view; management stays hand-edit
  host.append(el("div", "pp-section-head", "TEMPLATES"));
  const tpl = el("div", "set-templates");
  (templateCache || []).forEach((t) => {
    const row = el("div", "set-bind-row");
    row.append(el("span", "stmt-vendor", t.name + "  (" + (t.stages || []).length + " stages)"));
    row.append(pillLight("open →", () => {
      _noteReturn = "#/properties/settings";
      openNoteByPath("system/realestate/templates/" + t.slug + ".md");
    }));
    tpl.append(row);
  });
  host.append(tpl);
}

function entityCard(e, ents) {
  const card = el("div", "set-entity");
  const head = el("div", "set-entity-head");
  head.append(el("span", "wv-addr", e.name));
  head.append(pillLight("open →", () => { _noteReturn = "#/properties/settings"; openNoteByPath(e.path); }));
  card.append(head);

  // owners editor: owner ref + percent rows, Σ warning, cycle check server-side
  const owners = (e.owners || []).map((o) => ({ ...o }));
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    owners.forEach((o, i) => {
      const row = el("div", "uw-row cols-kv");
      const ref = ownerAutocomplete("owner (entity or person)…", (v) => { o.ref = v; });
      ref.setValue(o.ref);
      const pctIn = inputEl("%"); pctIn.type = "number"; pctIn.classList.add("est-in");
      pctIn.value = o.percent || "";
      pctIn.addEventListener("change", () => { o.percent = parseFloat(pctIn.value) || 0; render(); });
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { owners.splice(i, 1); render(); };
      row.append(ref.el, pctIn, x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ owner");
    add.onclick = () => { owners.push({ ref: "", percent: 0 }); render(); };
    box.append(add);
    const sum = owners.reduce((s, o) => s + (o.percent || 0), 0);
    const foot = el("div", "uw-footer", owners.length ? "Σ " + sum + "%" + (Math.abs(sum - 100) < 0.01 ? " ✓" : " ✕ should be 100%") : "");
    const save = pillLight("save owners", async () => {
      try {
        await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save",
          { owners: owners.filter((o) => o.ref) });
        showToast("Owners saved");
        renderREsettings();
      } catch (err) { showToast((err.message || "Save failed").slice(0, 90)); }
    });
    foot.append(save);
    box.append(foot);
  };
  render();
  card.append(box);

  // admin categories — chip editor over the frontmatter list
  const cats = [...(e.adminCategories || [])];
  const catBox = el("div", "set-cats");
  const renderCats = () => {
    catBox.innerHTML = "";
    catBox.append(el("span", "uw-label", "ADMIN CATEGORIES"));
    cats.forEach((c, i) => {
      const chip = el("span", "pp-chip", c + " ");
      const x = el("button", "uw-x", "✕");
      x.onclick = async () => { cats.splice(i, 1); await saveCats(); };
      chip.append(x);
      catBox.append(chip);
    });
    catBox.append(ghostInput("＋ category", "set-cat-add", async (v) => { cats.push(v); await saveCats(); }, "category…"));
  };
  const saveCats = async () => {
    try {
      await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save", { adminCategories: cats });
      await ensureEntities(true);
      renderCats();
    } catch (err) { showToast("Couldn't save categories"); }
  };
  renderCats();
  card.append(catBox);
  return card;
}

// orgChart: nested hairline tree — properties under their owning entity,
// parent entities above with percentages on the edges. A read, not a cap table.
function orgChart(ents) {
  const wrap = el("div", "org-chart");
  if (!ents.length) { wrap.append(el("div", "pp-empty", "No entities yet.")); return wrap; }
  const owned = new Set();
  ents.forEach((e) => (e.owners || []).forEach(() => {})); // owners point UP; children = entities owned by X
  const childrenOf = (name) => ents.filter((e) => (e.owners || []).some((o) =>
    o.ref.toLowerCase() === name.toLowerCase() || o.ref.toLowerCase() === (nameToSlug(name) || "")));
  const nameToSlug = (n) => { const e = ents.find((x) => x.name.toLowerCase() === n.toLowerCase()); return e && e.slug; };
  ents.forEach((e) => (e.owners || []).forEach((o) => {
    const child = ents.find((x) => x.slug.toLowerCase() === "" + e.slug.toLowerCase());
    if (child) owned.add(e.slug);
  }));
  const roots = ents.filter((e) => !(e.owners || []).some((o) =>
    ents.some((x) => x.name.toLowerCase() === o.ref.toLowerCase() || x.slug.toLowerCase() === o.ref.toLowerCase())));
  const propsOf = (e) => (propertyCache || []).filter((p) => (p.entity || "").toLowerCase() === e.name.toLowerCase());
  const node = (e, pct, depth, seen) => {
    if (seen.has(e.slug)) return el("div", "org-node", "↺ " + e.name);
    seen.add(e.slug);
    const box = el("div", "org-node");
    box.style.marginLeft = depth * 22 + "px";
    box.append(el("span", "org-name", e.name + (pct ? "" : "")));
    if (pct) box.append(el("span", "org-pct", pct + "%"));
    const kids = ents.filter((k) => k.slug !== e.slug && (k.owners || []).some((o) =>
      o.ref.toLowerCase() === e.name.toLowerCase() || o.ref.toLowerCase() === e.slug.toLowerCase()));
    const out = el("div", "org-branch");
    out.append(box);
    propsOf(e).forEach((p) => {
      const pr = el("div", "org-prop");
      pr.style.marginLeft = (depth + 1) * 22 + "px";
      pr.textContent = "▪ " + (p.short || p.address || p.slug);
      out.append(pr);
    });
    kids.forEach((k) => {
      const edge = (k.owners || []).find((o) => o.ref.toLowerCase() === e.name.toLowerCase() || o.ref.toLowerCase() === e.slug.toLowerCase());
      out.append(node(k, edge ? edge.percent : 0, depth + 1, seen));
    });
    return out;
  };
  const seen = new Set();
  roots.forEach((r) => wrap.append(node(r, 0, 0, seen)));
  // entities trapped in cycles / non-roots never reached: render flat
  ents.filter((e) => !seen.has(e.slug)).forEach((e) => wrap.append(node(e, 0, 0, seen)));
  return wrap;
}

// ---- WORK view (pass-5 §4): kanban default + gantt toggle ----

let workViewMode = "kanban"; // kanban | gantt

async function renderWorkView() {
  const host = els.propertyWork; host.hidden = false; host.innerHTML = "loading…";
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Unavailable.")); return; }
  host.innerHTML = "";
  const bar = el("div", "stmt-chips");
  [["kanban", "KANBAN"], ["gantt", "GANTT"]].forEach(([val, label]) => {
    const c = el("button", "filter-chip" + (workViewMode === val ? " on" : ""), label);
    c.onclick = () => { workViewMode = val; renderWorkView(); };
    bar.append(c);
  });
  host.append(bar);
  const active = propertyCache.filter((p) => !p.hidden &&
    (bucketOf(p) === "active" || (p.work || []).length));
  els.propertiesMeta.textContent = active.length + " projects";
  if (!active.length) { host.append(emptyRow("No active projects.")); return; }
  if (workViewMode === "gantt") host.append(ganttView(active));
  else host.append(kanbanView(active));
}

// kanbanView: columns = union of template stage names in canonical order,
// custom names appended. One card per property in its current stage's column.
// No drag — advancement stays an explicit check on the property page.
function kanbanView(props) {
  const colNames = [];
  const seen = new Set();
  (templateCache || []).forEach((t) => (t.stages || []).forEach((st) => {
    const k = st.text.toLowerCase();
    if (!seen.has(k)) { seen.add(k); colNames.push(st.text); }
  }));
  props.forEach((p) => (p.work || []).forEach((st) => {
    const k = st.text.toLowerCase();
    if (!seen.has(k)) { seen.add(k); colNames.push(st.text); }
  }));
  const buckets = new Map(colNames.map((n) => [n.toLowerCase(), []]));
  const noPlan = [];
  const doneAll = [];
  props.forEach((p) => {
    const cur = (p.work || []).find((s) => s.current);
    if (!cur) { ((p.work || []).length ? doneAll : noPlan).push(p); return; }
    const k = cur.text.toLowerCase();
    if (!buckets.has(k)) buckets.set(k, []);
    buckets.get(k).push({ p, cur });
  });
  const board = el("div", "kanban");
  colNames.forEach((name) => {
    const cards = buckets.get(name.toLowerCase()) || [];
    if (!cards.length) return; // empty columns stay out of the way
    const col = el("div", "kanban-col");
    col.append(el("div", "kanban-head", name.toUpperCase() + " · " + cards.length));
    cards.forEach(({ p, cur }) => col.append(kanbanCard(p, cur)));
    board.append(col);
  });
  if (doneAll.length) {
    const col = el("div", "kanban-col");
    col.append(el("div", "kanban-head", "COMPLETE · " + doneAll.length));
    doneAll.forEach((p) => col.append(kanbanCard(p, null)));
    board.append(col);
  }
  if (noPlan.length) {
    const col = el("div", "kanban-col noplan");
    col.append(el("div", "kanban-head", "NO WORK PLAN · " + noPlan.length));
    noPlan.forEach((p) => col.append(kanbanCard(p, null)));
    board.append(col);
  }
  return board;
}

function kanbanCard(p, cur) {
  const card = el("div", "kanban-card");
  card.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
  const head = el("div", "kanban-card-head");
  head.append(el("span", "wv-addr", p.short || p.address || p.slug));
  // stall dot: active property with no open todo queued
  if (cur && !(cur.todos || []).some((t) => !t.checked)) head.append(el("span", "stall-dot", "●"));
  card.append(head);
  if (p.entity) card.append(el("div", "kanban-entity", p.entity));
  if (cur) {
    const next = (cur.todos || []).find((t) => !t.checked);
    if (next) card.append(el("div", "kanban-todo", "› " + next.text));
    else card.append(el("div", "kanban-todo warn", "no next action"));
  }
  const pm = projMoney(p);
  card.append(el("div", "kanban-money" + (pm.over ? " over" : ""),
    fmtMoney(pm.budget) + " budget · " + fmtMoney(pm.paid) + " spent"));
  return card;
}

// ganttView: pure SVG — one row per property, stage bars from the derived
// schedule, a today line; dragging a bar's RIGHT EDGE edits that stage's
// weeks (the one allowed direct manipulation).
function ganttView(props) {
  const withSched = props.filter((p) => (p.schedule || []).length);
  const wrap = el("div", "gantt-wrap");
  if (!withSched.length) {
    wrap.append(emptyRow("No schedules yet — set a work start date on a property page."));
    return wrap;
  }
  let min = Infinity, max = -Infinity;
  withSched.forEach((p) => p.schedule.forEach((sp) => {
    min = Math.min(min, +new Date(sp.start));
    max = Math.max(max, +new Date(sp.end));
  }));
  const today = Date.now();
  min = Math.min(min, today) - 7 * 864e5;
  max = Math.max(max, today) + 14 * 864e5;
  const W = 980, ROW = 34, LABEL = 190, H = withSched.length * ROW + 30;
  const x = (t) => LABEL + ((t - min) / (max - min)) * (W - LABEL - 10);
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "gantt");
  const mk = (tag, attrs, text) => {
    const n = document.createElementNS(svgNS, tag);
    for (const k in attrs) n.setAttribute(k, attrs[k]);
    if (text) n.textContent = text;
    return n;
  };
  // month gridlines
  const d0 = new Date(min); d0.setDate(1);
  for (let d = new Date(d0); +d < max; d.setMonth(d.getMonth() + 1)) {
    svg.append(mk("line", { x1: x(+d), y1: 18, x2: x(+d), y2: H, class: "g-grid" }));
    svg.append(mk("text", { x: x(+d) + 3, y: 12, class: "g-month" }, (d.getMonth() + 1) + "/" + String(d.getFullYear()).slice(2)));
  }
  withSched.forEach((p, i) => {
    const y = 24 + i * ROW;
    const label = mk("text", { x: 0, y: y + 14, class: "g-label" }, (p.short || p.address || p.slug).slice(0, 26));
    label.style.cursor = "pointer";
    label.addEventListener("click", () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); });
    svg.append(label);
    p.schedule.forEach((sp) => {
      const bx = x(+new Date(sp.start)), bw = Math.max(4, x(+new Date(sp.end)) - bx);
      const bar = mk("rect", { x: bx, y: y + 2, width: bw, height: 18, rx: 2,
        class: "g-bar" + (sp.done ? " done" : "") });
      svg.append(bar);
      if (bw > 46) svg.append(mk("text", { x: bx + 4, y: y + 15, class: "g-bartext" }, sp.text.slice(0, Math.floor(bw / 7))));
      if (!sp.done) { // drag the right edge → weeks (quantized, ≥1)
        const grip = mk("rect", { x: bx + bw - 4, y: y + 2, width: 8, height: 18, class: "g-grip" });
        let drag = null;
        grip.addEventListener("pointerdown", (e) => {
          e.preventDefault(); e.stopPropagation();
          drag = { startX: e.clientX, weeks: sp.weeks };
          grip.setPointerCapture(e.pointerId);
        });
        grip.addEventListener("pointermove", (e) => {
          if (!drag) return;
          const scale = (max - min) / (W - LABEL - 10); // ms per px
          const dWeeks = (e.clientX - drag.startX) * scale * (svg.clientWidth ? W / svg.clientWidth : 1) / (7 * 864e5);
          const w = Math.max(1, Math.round(drag.weeks + dWeeks));
          bar.setAttribute("width", Math.max(4, x(+new Date(sp.start) + w * 7 * 864e5) - bx));
          drag.next = w;
        });
        grip.addEventListener("pointerup", async (e) => {
          const w = drag && drag.next;
          drag = null;
          if (w && w !== sp.weeks) {
            try {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
                { op: "set-field", id: sp.id, field: "weeks", value: String(w) });
              renderWorkView();
            } catch (err) { showToast("Couldn't set weeks"); }
          }
        });
        svg.append(grip);
      }
    });
  });
  svg.append(mk("line", { x1: x(today), y1: 18, x2: x(today), y2: H, class: "g-today" }));
  wrap.append(svg);
  return wrap;
}

async function renderPropertyPage(slug) {
  els.propertyBoard.hidden = true; els.propertyMapWrap.hidden = true;
  els.propertyStatements.hidden = true; els.propertyContractors.hidden = true; els.propertyWork.hidden = true;
  const host = els.propertyPage; host.hidden = false; host.textContent = "loading…";
  try {
    const [p, srcRes, geoRes] = await Promise.all([
      (await fetch("/api/properties/" + encodeURIComponent(slug))).json(),
      fetch("/api/properties/" + encodeURIComponent(slug) + "/source").then((r) => r.json()).catch(() => ({})),
      fetch("/api/properties/geo?slug=" + encodeURIComponent(slug)).then((r) => r.json()).catch(() => ({})),
    ]);
    renderProp(p, srcRes.source || null, ((geoRes.records || [])[0] || {}).features || []);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Property not found.")); }
}

// renderProp (design §3): money you touch daily on top, reference collapsed
// below, prose last.
function renderProp(p, src, geoFeatures) {
  const host = els.propertyPage; host.innerHTML = ""; host.hidden = false;
  els.propertyBoard.hidden = true; els.propertyMapWrap.hidden = true;

  const back = el("button", "pill light pp-back", "‹ board");
  back.onclick = () => { location.hash = "#/properties"; };
  host.append(back);

  // single-parcel deals carry the FULL deal object; members carry the slice.
  const isFullDeal = !!(src && Array.isArray(src.properties) && src.properties.length);
  const propSlice = isFullDeal ? src.properties[0] : (src || null);

  const head = el("div", "pp-head");
  const titleRow = el("div", "pp-title-row");
  const tcol = el("div", "pp-title-col");
  const h = el("h2", "pp-title", p.short || p.address || p.name);
  tcol.append(h);
  if (p.address && p.address !== (p.short || "")) tcol.append(el("span", "pp-fulladdr", p.address));
  if (propSlice && propSlice.parcel_id) tcol.append(el("span", "pp-parcel", propSlice.parcel_id));
  const chips = el("div", "pp-chips");
  chips.append(editChip(p, "status", p.status, PROPERTY_STATUSES));
  chips.append(el("span", "pp-chip", p.control));
  chips.append(editChip(p, "kind", p.kind, PROPERTY_KINDS));
  chips.append(entityChip(p));
  if (p.deal) {
    const dchip = el("a", "pp-chip pp-deal", "▸ " + p.deal);
    dchip.href = "#/properties/deal/" + encodeURIComponent(p.deal);
    chips.append(dchip);
  }
  tcol.append(chips);
  titleRow.append(tcol);
  const thumb = parcelThumb(geoFeatures);
  if (thumb) { thumb.onclick = () => { location.hash = "#/properties/map"; }; titleRow.append(thumb); }
  head.append(titleRow);
  host.append(head);

  // BUDGET · SPENT · REMAINING — plan vs spend. The plan (ests + underwriting)
  // IS the budget; over-budget shows red when any category's actuals exceed it.
  const pj = p.project;
  if (pj) {
    const sum = el("div", "pp-rollup");
    sum.append(rollupStat("budget", "", fmtMoney(pj.planTotal)));
    const spent = rollupStat("spent", fmtPct(pj.planTotal > 0 ? pj.paid / pj.planTotal : 0), fmtMoney(pj.paid));
    if (pj.over) spent.classList.add("over");
    sum.append(spent);
    sum.append(rollupStat("remaining", "", fmtMoney(pj.planTotal - pj.paid)));
    host.append(sum);
    const togo = el("div", "pp-togo");
    togo.append(el("span", "", "committed " + fmtMoney(pj.committed)));
    if (pj.unreconciled > 0) {
      const un = el("span", "pp-unrec", "⚑ unreconciled " + fmtMoney(pj.unreconciled));
      un.title = "work marked done whose firm price has no linked bank transaction yet — link payments in the statement workbench";
      togo.append(un);
    }
    if (pj.over) togo.append(el("span", "pp-over-note", "over budget in a category ↓"));
    host.append(togo);
  }

  // WORK — the management core (budget category table retired from the page;
  // over-budget still surfaces via the rollup pair + feed signal).
  host.append(el("div", "pp-section-head", "WORK"));
  host.append(workBlock(p));

  // SCHEDULE anchor (§3): one date + per-stage derived spans on the rows
  const sched = el("div", "pp-sched");
  const wsLabel = el("span", "uw-label", "WORK START");
  const wsBtn = el("button", "est-slot" + (p.workStart ? "" : " empty"), p.workStart || "set date");
  wsBtn.onclick = () => {
    const input = inputEl("YYYY-MM-DD");
    input.type = "date";
    if (p.workStart) input.value = p.workStart;
    input.addEventListener("change", async () => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "work-start", value: input.value });
        renderPropertyPage(p.slug);
      } catch (e) { showToast("Couldn't set work start"); }
    });
    wsBtn.replaceWith(input);
    input.focus();
  };
  sched.append(wsLabel, wsBtn);
  host.append(sched);

  // MONEY — read-only (pass-5: money enters ONLY via the statement workbench).
  host.append(el("div", "pp-section-head", "MONEY"));
  host.append(moneyBlock(p));

  // UNDERWRITING — collapsed reference; single-parcel deals get BOTH levels.
  if (src) {
    const summary = propSlice ?
      [propSlice.purchase_price ? "$" + Math.round(propSlice.purchase_price / 1000) + "k purch" : "",
        propSlice.total_units ? propSlice.total_units + "u" : "",
        propSlice.total_sf ? propSlice.total_sf.toLocaleString() + "sf" : ""].filter(Boolean).join(" · ") : "";
    const headEl = el("div", "pp-section-head tag-engine", "UNDERWRITING · FEEDS SITE ENGINE");
    host.append(headEl);
    const body = collapsibleSection(host, propSlice === src ? "PROPERTY FIELDS" : "FIELDS", summary, false);
    const dirty = makeDirtyBar(host,
      async () => {
        await putJSON("/api/properties/" + encodeURIComponent(p.slug) + "/source", src);
        showToast("Saved — source.json updated");
      },
      () => renderPropertyPage(p.slug));
    if (propSlice) propertyLevelSections(body, propSlice, dirty,
      { derivedHard: (p.work || []).some((st) => st.estTotal > 0) });
    if (isFullDeal) {
      const dealBody = collapsibleSection(host, "DEAL-LEVEL (this record IS the deal)", "", false);
      dealLevelSections(dealBody, src, dirty);
      documentsSection(dealBody, src, dirty, p.slug, "single");
    }
  }

  const logSum = (p.log || []).length ? p.log.length + " lines · " + (p.lastLog || "").slice(0, 44) : "empty";
  collapsibleSection(host, "LOG", logSum, false).append(logBlock(p));
  collapsibleSection(host, "DOCS", "click to open", false).append(docsBlock(p));

  const edit = el("button", "pill light pp-editnote", "edit note / prose →");
  edit.onclick = () => { _noteReturn = "#/properties/" + encodeURIComponent(p.slug); openNoteByPath(p.path); };
  host.append(edit);
}

// parcelThumb: the parcel polygon as a quiet inline SVG (no tiles, no Leaflet).
function parcelThumb(features) {
  if (!features || !features.length) return null;
  let pts = [];
  features.forEach((f) => {
    const g = f.geometry || {};
    if (g.type === "Polygon") (g.coordinates || []).forEach((ring) => pts.push(...ring));
  });
  if (pts.length < 3) return null;
  const xs = pts.map((c) => c[0]), ys = pts.map((c) => c[1]);
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
  const W = 180, H = 120, pad = 10;
  const sx = (W - 2 * pad) / (maxX - minX || 1), sy = (H - 2 * pad) / (maxY - minY || 1);
  const s = Math.min(sx, sy);
  const px = (c) => (pad + (c[0] - minX) * s).toFixed(1) + "," + (H - pad - (c[1] - minY) * s).toFixed(1);
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "pp-thumb");
  features.forEach((f) => {
    const g = f.geometry || {};
    if (g.type !== "Polygon") return;
    (g.coordinates || []).forEach((ring) => {
      const poly = document.createElementNS(svgNS, "polygon");
      poly.setAttribute("points", ring.map(px).join(" "));
      svg.append(poly);
    });
  });
  return svg;
}

// entityChip: the property's entity is a HARD LINK to an entity record — the
// chip swaps to the record autocomplete (pick an existing entity, or create one
// via the quiet completion, which makes the record first). Never free text.
function entityChip(p) {
  const chip = el("span", "pp-chip editable", p.entity || "entity: —");
  chip.title = "click to link an entity (from SETTINGS records)";
  chip.onclick = () => {
    const ac = recordAutocomplete("entity", "entity…", async (rec) => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "entity", value: rec.name });
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Couldn't link entity").slice(0, 80)); ac.el.replaceWith(chip); }
    });
    if (p.entity) ac.setValue(p.entity);
    const clear = el("button", "uw-x", "✕");
    clear.title = "unlink entity";
    clear.onclick = async () => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "entity", value: "" });
        renderPropertyPage(p.slug);
      } catch (err) { showToast("Couldn't clear"); }
    };
    const wrap = el("span", "pp-chip-edit-wrap");
    wrap.append(ac.el, clear);
    const input = ac.el.querySelector("input");
    input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") wrap.replaceWith(chip); });
    input.addEventListener("blur", () => setTimeout(() => { if (wrap.parentNode) wrap.replaceWith(chip); }, 200));
    chip.replaceWith(wrap);
    ac.focus();
  };
  return chip;
}

// editChip is a page chip that swaps to a select (enum) or text input (free) and
// POSTs the field edit — the property page's click-to-edit for status/kind/entity.
function editChip(p, key, value, options) {
  const label = value ? (key === "entity" ? value : value) : key + ": —";
  const chip = el("span", "pp-chip editable", label);
  chip.title = "click to edit " + key;
  chip.onclick = () => {
    let ctl;
    if (options) { ctl = selectEl(options); ctl.value = value || options[0]; }
    else { ctl = inputEl(key + "…"); ctl.value = value || ""; }
    ctl.classList.add("pp-chip-edit");
    const save = async () => {
      const v = options ? ctl.value : ctl.value.trim();
      if (v === (value || "") && options) { ctl.replaceWith(chip); return; }
      try { await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key, value: v }); renderPropertyPage(p.slug); }
      catch (err) { showToast("Couldn't update " + key); ctl.replaceWith(chip); }
    };
    if (options) { ctl.onchange = save; ctl.onblur = () => { if (ctl.parentNode) ctl.replaceWith(chip); }; }
    else {
      ctl.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter") save();
        else if (ev.key === "Escape") ctl.replaceWith(chip);
      });
      ctl.addEventListener("blur", save);
    }
    chip.replaceWith(ctl);
    ctl.focus();
  };
  return chip;
}

function rollupStat(label, pct, money) {
  const s = el("div", "pp-stat");
  s.append(el("div", "pp-stat-big", pct || money));
  s.append(el("div", "pp-stat-sub", pct ? money : ""));
  s.append(el("div", "pp-stat-label", label));
  return s;
}

// normDate coerces common bank formats (MM/DD/YYYY, YYYY-MM-DD) to ISO.
function normDate(s) {
  s = String(s).trim();
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10);
  const m = s.match(/^(\d{1,2})\/(\d{1,2})\/(\d{2,4})/);
  if (m) {
    let y = m[3].length === 2 ? "20" + m[3] : m[3];
    return y + "-" + m[1].padStart(2, "0") + "-" + m[2].padStart(2, "0");
  }
  return "";
}

// ---- MONEY block (pass-5 §1): read-only — per-stage triplet + recent
// activity with grouped payments. Money enters ONLY via the workbench. ----
let moneyShowAll = false;

function moneyBlock(p) {
  const wrap = el("div", "pp-money");
  // category table: the four project-cost lines, plan vs spend
  if (p.project && (p.project.categories || []).length) {
    const cats = el("div", "pp-money-stages");
    cats.append(ppCols("cols-cats", ["CATEGORY", "BUDGET", "COMMITTED", "PAID"]));
    p.project.categories.forEach((c) => {
      const row = el("div", "pp-money-row cols-cats" + (c.over ? " over" : ""));
      row.append(el("span", "", c.key),
        el("span", "pp-amt", c.budget ? fmtMoney(c.budget) : "—"),
        el("span", "pp-amt", c.committed ? fmtMoney(c.committed) : ""),
        el("span", "pp-amt", c.paid ? fmtMoney(c.paid) : ""));
      cats.append(row);
    });
    wrap.append(cats);
  }
  // per-stage est · committed · paid
  if ((p.work || []).length) {
    const tbl = el("div", "pp-money-stages");
    tbl.append(ppCols("cols-money", ["STAGE", "EST", "COMMITTED", "PAID"]));
    p.work.forEach((st) => {
      const row = el("div", "pp-money-row cols-money" + (st.estTotal > 0 && st.committed > st.estTotal ? " over" : ""));
      row.append(el("span", "", st.text),
        el("span", "pp-amt", st.estTotal ? fmtMoney(st.estTotal) : "—"),
        el("span", "pp-amt", st.committed ? fmtMoney(st.committed) : ""),
        el("span", "pp-amt", st.paid ? fmtMoney(st.paid) : ""));
      tbl.append(row);
    });
    wrap.append(tbl);
  }

  // recent activity: applied expenses newest-first; rows sharing a workId under
  // an accepted bid GROUP into one expandable line ("merge = grouping, never
  // destruction" — the verbatim transactions live inside).
  const paid = (p.ledger || []).filter((r) => r.type === "expense")
    .sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  const acceptedByWork = {};
  (p.ledger || []).forEach((r) => {
    if (r.type === "bid" && r.status === "accepted" && r.workId) acceptedByWork[r.workId] = r;
  });
  const groups = new Map(); // workId → rows (only when an accepted bid anchors it)
  const singles = [];
  paid.forEach((r) => {
    if (r.workId && acceptedByWork[r.workId]) {
      if (!groups.has(r.workId)) groups.set(r.workId, []);
      groups.get(r.workId).push(r);
    } else singles.push(r);
  });

  const act = el("div", "pp-activity");
  act.append(el("div", "uw-sub", "recent activity"));
  const lines = [];
  for (const [wid, rows] of groups) {
    if (rows.length > 1) {
      const bid = acceptedByWork[wid];
      lines.push({ date: rows[0].date, el: groupedPaymentLine(bid, rows, wid) });
    } else if (rows.length === 1) singles.push(rows[0]);
  }
  singles.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  singles.forEach((r) => lines.push({ date: r.date, el: activityLine(r) }));
  lines.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  const shown = moneyShowAll ? lines : lines.slice(0, 10);
  if (!shown.length) act.append(el("div", "pp-empty", "No payments yet — apply statement rows in the workbench."));
  shown.forEach((l) => act.append(l.el));
  if (!moneyShowAll && lines.length > shown.length) {
    const more = el("button", "o-ghost", "show all (" + lines.length + ")");
    more.onclick = () => { moneyShowAll = true; renderPropertyPage(p.slug); };
    act.append(more);
  }
  wrap.append(act);
  return wrap;
}

function activityLine(r) {
  const line = el("div", "pp-act-line");
  line.append(el("span", "import-date", r.date));
  line.append(el("span", "stmt-vendor", r.vendor || r.contractor || ""));
  line.append(el("span", "pp-amt", fmtMoney(r.amount)));
  const tags = el("span", "pp-act-tags");
  if (r.workId) tags.append(el("span", "work-chip lg-chip", "⚲ " + r.workId.split("/").pop()));
  if (r.paidBy) tags.append(el("span", "work-chip", r.paidBy));
  line.append(tags);
  return line;
}

// groupedPaymentLine: `$9,000 · 3 payments` expandable to the verbatim rows.
function groupedPaymentLine(bid, rows, wid) {
  const holder = el("div", "pp-act-group");
  const line = el("div", "pp-act-line grouped");
  const total = rows.reduce((s, r) => s + r.amount, 0);
  line.append(el("span", "import-date", rows[0].date));
  line.append(el("span", "stmt-vendor", (bid.contractor || bid.vendor || "") + " — " + wid.split("/").pop()));
  line.append(el("span", "pp-amt", fmtMoney(bid.amount) + " · " + rows.length + " payments" +
    (Math.abs(total - bid.amount) > 0.01 ? " (" + fmtMoney(total) + " so far)" : "")));
  const caret = el("span", "sec-caret", "▸");
  line.append(caret);
  const detail = el("div", "pp-act-detail");
  detail.hidden = true;
  rows.forEach((r) => detail.append(activityLine(r)));
  line.onclick = () => { detail.hidden = !detail.hidden; caret.textContent = detail.hidden ? "▸" : "▾"; };
  holder.append(line, detail);
  return holder;
}

// ---- inline bid flow (pass-5: chips are the only bid surface) ----

// toggleBidForm: contractor + amount inline under the todo → writes a
// requested bid tethered to it (still a ledger row — written by the action).
function toggleBidForm(row, p, st, td) {
  const existing = row.parentElement.querySelector(".bid-form[data-for='" + td.id + "']");
  if (existing) { existing.remove(); return; }
  const form = el("div", "bid-form");
  form.dataset.for = td.id;
  const who = contractorAutocomplete("contractor…");
  const amt = inputEl("amount $"); amt.type = "number"; amt.step = "1"; amt.classList.add("est-in");
  const send = el("button", "pill lg-add", "request bid");
  send.onclick = async () => {
    const amount = parseFloat(amt.value) || 0;
    if (!who.value().trim() || !amount) { showToast("Contractor + amount required"); return; }
    send.disabled = true;
    try {
      await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger", {
        type: "bid", status: "requested", contractor: who.value(), amount,
        category: st.text, workId: td.id,
      });
      renderPropertyPage(p.slug);
    } catch (e) { showToast("Couldn't create bid"); send.disabled = false; }
  };
  const cancel = el("button", "pill light lg-add", "✕");
  cancel.onclick = () => form.remove();
  form.append(who.el, amt, send, cancel);
  row.after(form);
  who.focus();
}

// bidChipEl: a clickable bid chip — requested/received expand accept/decline.
function bidChipEl(p, b) {
  const chip = el("span", "work-chip bid-" + b.status, "bid " + b.status + ": " + (b.who || "?") + " " + fmtMoney(b.amount));
  if (b.status !== "requested" && b.status !== "received") return chip;
  chip.classList.add("clickable");
  chip.onclick = async (e) => {
    e.stopPropagation();
    if (chip.querySelector("button")) return;
    const act = async (status) => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate",
          { original: b.row, replacement: { ...b.row, status } });
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Bid update failed").slice(0, 80)); }
    };
    chip.append(quietBtn(" ✓ accept", () => act("accepted")), quietBtn(" ✕ decline", () => act("declined")));
  };
  return chip;
}

function logBlock(p) {
  const wrap = el("div", "pp-log");
  (p.log || []).forEach((l) => wrap.append(el("div", "pp-log-line", l)));
  if (!(p.log || []).length) wrap.append(el("div", "pp-empty", "No log entries yet."));
  wrap.append(ghostInput("＋ log line", "pp-log-add", (v) => addLog(p.slug, v), "what happened…"));
  return wrap;
}

async function addLog(slug, text) {
  try { await postJSONOk("/api/properties/" + encodeURIComponent(slug) + "/log", { text }); renderPropertyPage(slug); }
  catch (e) { showToast("Couldn't add log line"); }
}

// docsBlock: the property's document folder — list (click opens raw), drag-drop
// zone + picker fallback. Files live in the vault at system/realestate/docs/<slug>/.
function docsBlock(p) {
  const wrap = el("div", "pp-docs");
  const list = el("div", "pp-doc-list");
  wrap.append(list);
  const refresh = async () => {
    list.innerHTML = "";
    let docs = [];
    try { docs = (await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/docs")).json()).docs || []; }
    catch (e) {}
    if (!docs.length) { list.append(el("div", "pp-empty", "No documents yet — drop files below.")); return; }
    docs.forEach((d) => {
      const a = el("a", "pp-doc", d.name + "  (" + fmtBytes(d.size) + ")");
      a.href = "/api/realestate/doc?path=" + encodeURIComponent(d.path);
      a.target = "_blank";
      list.append(a);
    });
  };
  refresh();

  const drop = el("div", "pp-dropzone", "drop files here — or click to pick");
  const pick = document.createElement("input");
  pick.type = "file"; pick.multiple = true; pick.hidden = true;
  const upload = async (files) => {
    if (!files || !files.length) return;
    const fd = new FormData();
    for (const f of files) fd.append("file", f);
    drop.textContent = "uploading…";
    try {
      const res = await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/docs", { method: "POST", body: fd });
      if (!res.ok) throw new Error((await res.text()).trim());
      refresh();
    } catch (e) { showToast("Upload failed: " + (e.message || "").slice(0, 80)); }
    drop.textContent = "drop files here — or click to pick";
  };
  drop.onclick = () => pick.click();
  pick.onchange = () => { upload(pick.files); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); upload(e.dataTransfer.files); });
  wrap.append(drop, pick);
  return wrap;
}

function fmtBytes(n) {
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + "MB";
  if (n > 1024) return Math.round(n / 1024) + "KB";
  return n + "B";
}

function inputEl(placeholder) {
  const i = document.createElement("input");
  i.className = "pp-in"; i.placeholder = placeholder; return i;
}
function selectEl(opts) {
  const s = document.createElement("select"); s.className = "pp-in";
  opts.forEach((o) => { const opt = document.createElement("option"); opt.value = o; opt.textContent = o; s.append(opt); });
  return s;
}

function route() {
  const h = location.hash;
  const goals = h === "#/goals" || h.startsWith("#/goals/"); // #/goals/<id> deep-links a Rock
  const cal = h === "#/calendar";
  const fd = h === "#/feed";
  const studio = h === "#/studio" || h.startsWith("#/studio/");
  if (h === "#/spirits/approvals") { location.hash = "#/feed"; return; } // approvals live in FEED now
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const contacts = h === "#/contacts" || h.startsWith("#/contacts/");
  const reading = h === "#/reading" || h.startsWith("#/reading/");
  const properties = h === "#/properties" || h.startsWith("#/properties/");
  const note = h.startsWith("#/note/");
  const day = !goals && !cal && !fd && !studio && !sp && !contacts && !reading && !properties && !note;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.calendarView.hidden = !cal;
  els.feedView.hidden = !fd;
  els.studioView.hidden = !studio;
  els.spiritsView.hidden = !sp;
  els.contactsView.hidden = !contacts;
  els.readingView.hidden = !reading;
  els.propertiesView.hidden = !properties;
  els.noteView.hidden = !note;
  els.dateNav.hidden = !day;
  els.goalsNav.hidden = !day;
  els.feedNav.hidden = !day;
  els.studioNav.hidden = !day;
  els.calNav.hidden = !day;
  els.contactsNav.hidden = !day;
  els.readingNav.hidden = !day;
  els.propertiesNav.hidden = !day;
  els.spiritsNav.hidden = !day;
  els.dayNav.hidden = day;
  if (day) refreshFeedBadge(); // pill only shows on the day view — keep it honest
  if (goals) {
    // "#/goals/history" is the archive tab; any other suffix is a goal-id
    // deep-link (safe: real ids always contain "/", so "history" can't collide).
    const suffix = h.startsWith("#/goals/") ? decodeURIComponent(h.slice("#/goals/".length)) : "";
    if (suffix === "history") showGoalsHistory();
    else loadGoals(suffix);
  }
  else if (cal) loadCalendar();
  else if (fd) showFeed(); // manifest's one inbox
  else if (studio) showStudio(); // content studio: draft board + inspiration
  else if (sp) showSpirits(); // engine console: runs / rituals / approvals
  else if (contacts) showContacts(); // people layer: list / page
  else if (reading) loadReading(); // book shelf over the extrinsic zone
  else if (properties) showProperties(h); // real-estate cockpit: board / property page
  else if (note) showNote(decodeURIComponent(h.slice("#/note/".length))); // universal note view
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
