// Manifest — local daily-planner UI over your Obsidian vault.
// State lives in markdown files; this is a thin editor with autosave.

const state = { date: isoToday(), day: null, cal: null, agentsPoll: null };

const els = {
  dateLabel: document.getElementById("dateLabel"),
  streakText: document.getElementById("streakText"),
  saveState: document.getElementById("saveState"),
  scheduleRows: document.getElementById("scheduleRows"),
  scheduleRange: document.getElementById("scheduleRange"),
  goalsRows: document.getElementById("goalsRows"),
  milestonesRows: document.getElementById("milestonesRows"),
  taskRows: document.getElementById("taskRows"),
  addTask: document.getElementById("addTask"),
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
  agentCounts: document.getElementById("agentCounts"),
  agentsDisabled: document.getElementById("agentsDisabled"),
  approvalRows: document.getElementById("approvalRows"),
  outboxRows: document.getElementById("outboxRows"),
  calGrid: document.getElementById("calGrid"),
  calMonthLabel: document.getElementById("calMonthLabel"),
  calConnect: document.getElementById("calConnect"),
  calConnectBtn: document.getElementById("calConnectBtn"),
  calPrev: document.getElementById("calPrev"),
  calNext: document.getElementById("calNext"),
  plateRows: document.getElementById("plateRows"),
  areasRows: document.getElementById("areasRows"),
  addArea: document.getElementById("addArea"),
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
      refreshStreak();
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
async function refreshStreak() {
  try {
    const r = await fetch(`/api/day?date=${state.date}`);
    renderStreak((await r.json()).streak);
  } catch (e) {}
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

function renderDay() {
  const day = state.day;
  renderStreak(day.streak);
  renderPrep(day);
  if (day.schedule.length) {
    els.scheduleRange.textContent =
      `${hourLabel(Math.floor(slotMin(day.schedule[0].time) / 60))}–` +
      `${hourLabel(Math.floor(slotMin(day.schedule[day.schedule.length - 1].time) / 60))}`;
  }
  renderSchedule(day.schedule);
  renderReadonly(els.goalsRows, day.goals, "No 90-day goals on your plate");
  renderReadonly(els.milestonesRows, day.milestones, "No 30-day goals on your plate");
  renderTasks(day.tasks);
}

function renderStreak(n) {
  els.streakText.textContent = `${n} DAY${n === 1 ? "" : "S"} STREAK`;
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
      if (isCal) input.title = "From your calendar — type to make it your own";
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
  const yOf = (el) => {
    const r = el.getBoundingClientRect();
    return r.top - crect.top + r.height / 2;
  };
  for (let k = 0; k < filled.length - 1; k++) {
    const a = filled[k], b = filled[k + 1];
    const ya = yOf(a.el), yb = yOf(b.el);
    const line = document.createElement("div");
    line.className = "conn-line";
    line.style.top = `${ya}px`;
    line.style.height = `${yb - ya}px`;
    overlay.appendChild(line);

    const label = document.createElement("span");
    label.className = "conn-label";
    label.style.top = `${(ya + yb) / 2}px`;
    label.textContent = fmtDur(b.min - a.min);
    overlay.appendChild(label);
  }
}
window.addEventListener("resize", drawConnectors);

function renderTasks(tasks) {
  els.taskRows.innerHTML = "";
  const list = tasks.length ? tasks : [{ text: "", done: false }];
  list.forEach((t, i) => addTaskRow(t, i + 1));
}
function addTaskRow(task, num) {
  const row = document.createElement("div");
  row.className = "trow";
  if (task.goalId) row.dataset.goalId = task.goalId; // preserve backlink on save
  if (task.owner) row.dataset.owner = task.owner;
  const n = document.createElement("span");
  n.className = "num";
  n.textContent = `${num}.`;
  const input = document.createElement("input");
  input.className = "ttext" + (task.done ? " done" : "");
  input.value = task.text || "";
  input.addEventListener("change", saveDay);
  const cell = document.createElement("div");
  cell.className = "check-cell";
  const check = document.createElement("button");
  check.className = "check" + (task.done ? " on" : "");
  check.textContent = task.done ? "✓" : "○";
  check.addEventListener("click", () => {
    const done = !input.classList.contains("done");
    input.classList.toggle("done", done);
    check.classList.toggle("on", done);
    check.textContent = done ? "✓" : "○";
    saveDay();
  });
  cell.appendChild(check);
  row.append(n, input, cell);
  els.taskRows.appendChild(row);
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

async function loadGoals() {
  const [docR, plateR] = await Promise.all([fetch("/api/goals"), fetch("/api/myplate")]);
  const doc = await docR.json();
  const plate = await plateR.json();
  renderPlate(plate.groups || []);
  renderAreas(doc.areas || []);
}

function renderPlate(groups) {
  els.plateRows.innerHTML = "";
  if (!groups.length) {
    const e = document.createElement("div");
    e.className = "ro-row empty";
    e.textContent = "Nothing on your plate yet";
    els.plateRows.appendChild(e);
    return;
  }
  groups.forEach((g) => {
    const head = document.createElement("div");
    head.className = "plate-area";
    head.textContent = g.area;
    els.plateRows.appendChild(head);
    g.items.forEach((it) => {
      const row = document.createElement("div");
      row.className = "plate-item";
      const txt = document.createElement("span");
      txt.className = "plate-text";
      txt.textContent = it.text;
      row.appendChild(txt);
      if (it.due) {
        const due = document.createElement("span");
        due.className = "plate-due";
        due.textContent = it.due;
        row.appendChild(due);
      }
      els.plateRows.appendChild(row);
    });
  });
}

function renderAreas(areas) {
  els.areasRows.innerHTML = "";
  areas.forEach((area) => els.areasRows.appendChild(areaCard(area)));
}

function areaCard(area) {
  const card = document.createElement("div");
  card.className = "area-card";

  const head = document.createElement("div");
  head.className = "area-head";
  const name = document.createElement("input");
  name.className = "area-name";
  name.value = area.name;
  name.addEventListener("change", () => {
    const v = name.value.trim();
    if (v && v !== area.name) goalsApi("PATCH", "/api/areas", { name: area.name, newName: v });
  });
  const del = document.createElement("button");
  del.className = "icon-btn area-del";
  del.textContent = "✕";
  del.title = "Delete area";
  del.addEventListener("click", () => {
    if (confirm(`Delete area “${area.name}” and its goals?`))
      goalsApi("DELETE", "/api/areas", { name: area.name });
  });
  head.append(name, del);

  const ns = document.createElement("input");
  ns.className = "area-ns";
  ns.placeholder = "North Star…";
  ns.value = area.northStar || "";
  ns.addEventListener("change", () => {
    goalsApi("PATCH", "/api/areas", { name: area.name, northStar: ns.value.trim() });
  });

  card.append(head, ns);
  card.appendChild(horizonSection(area.name, "90-day", "90-DAY", area.goals90));
  card.appendChild(horizonSection(area.name, "30-day", "30-DAY", area.goals30));
  if (area.loose && area.loose.length) {
    card.appendChild(horizonSection(area.name, "", "OTHER", area.loose));
  }
  return card;
}

function horizonSection(areaName, horizon, label, goals) {
  const sec = document.createElement("div");
  sec.className = "horizon";
  const lbl = document.createElement("div");
  lbl.className = "horizon-label";
  lbl.textContent = label;
  sec.appendChild(lbl);
  const list = document.createElement("div");
  list.className = "goal-list";
  (goals || []).forEach((g) => list.appendChild(goalRow(g)));
  sec.appendChild(list);
  const add = document.createElement("button");
  add.className = "add-btn add-goal";
  add.textContent = "+ Add goal";
  add.addEventListener("click", () =>
    goalsApi("POST", "/api/goals/item", { area: areaName, horizon, text: "New goal", owner: "me", due: "" }));
  sec.appendChild(add);
  return sec;
}

function goalRow(g) {
  const row = document.createElement("div");
  row.className = "goal-row";

  const check = document.createElement("button");
  check.className = "check" + (g.checked ? " on" : "");
  check.textContent = g.checked ? "✓" : "○";
  check.addEventListener("click", () =>
    goalsApi("POST", "/api/goals/check", { id: g.id, checked: !g.checked }));

  const text = document.createElement("input");
  text.className = "goal-text" + (g.checked ? " done" : "");
  text.value = g.text;
  text.addEventListener("change", () => {
    const v = text.value.trim();
    if (v && v !== g.text) goalsApi("PATCH", "/api/goals/item", { id: g.id, text: v });
  });

  const owner = document.createElement("select");
  owner.className = "owner-chip owner-" + (g.owner === "me" ? "me" : g.owner === "team" ? "team" : "other");
  ["me", "team"].forEach((o) => owner.appendChild(new Option(o, o)));
  if (g.owner !== "me" && g.owner !== "team") owner.appendChild(new Option(g.owner, g.owner));
  owner.value = g.owner;
  owner.addEventListener("change", () =>
    goalsApi("PATCH", "/api/goals/item", { id: g.id, owner: owner.value }));

  const due = document.createElement("input");
  due.type = "date";
  due.className = "due-pick" + (g.due ? " set" : "");
  due.value = g.due || "";
  due.addEventListener("change", () =>
    goalsApi("PATCH", "/api/goals/item", { id: g.id, due: due.value }));

  const del = document.createElement("button");
  del.className = "icon-btn goal-del";
  del.textContent = "✕";
  del.title = "Delete goal";
  del.addEventListener("click", () => goalsApi("DELETE", "/api/goals/item", { id: g.id }));

  row.append(check, text, owner, due, del);
  return row;
}

els.addArea.addEventListener("click", () => {
  const name = prompt("New area name:");
  if (name && name.trim()) goalsApi("POST", "/api/areas", { name: name.trim() });
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

async function loadCalendar() {
  const { year, month } = ensureCalState();
  els.calMonthLabel.textContent = `${MONTHS[month]} ${year}`.toUpperCase();
  let status = { configured: false };
  try { status = await (await fetch("/api/calendar/status")).json(); } catch (e) {}
  els.calConnect.hidden = !!status.configured;

  let events = [];
  if (status.configured) {
    const first = `${year}-${pad(month + 1)}-01`;
    const lastDay = new Date(year, month + 1, 0).getDate();
    const last = `${year}-${pad(month + 1)}-${pad(lastDay)}`;
    try {
      const r = await (await fetch(`/api/calendar/events?start=${first}&end=${last}`)).json();
      events = r.events || [];
    } catch (e) {}
  }
  renderMonth(year, month, events);
}

function renderMonth(year, month, events) {
  const byDay = new Map();
  events.forEach((e) => {
    const day = (e.start || "").slice(0, 10);
    if (!byDay.has(day)) byDay.set(day, []);
    byDay.get(day).push(e);
  });
  els.calGrid.innerHTML = "";
  const firstDow = (new Date(year, month, 1).getDay() + 6) % 7; // Monday = 0
  const days = new Date(year, month + 1, 0).getDate();
  for (let i = 0; i < firstDow; i++) {
    const blank = document.createElement("div");
    blank.className = "cal-cell blank";
    els.calGrid.appendChild(blank);
  }
  const today = isoToday();
  for (let d = 1; d <= days; d++) {
    const iso = `${year}-${pad(month + 1)}-${pad(d)}`;
    const cell = document.createElement("div");
    cell.className = "cal-cell" + (iso === today ? " today" : "");
    const num = document.createElement("div");
    num.className = "cal-day-num";
    num.textContent = d;
    cell.appendChild(num);
    const evs = byDay.get(iso) || [];
    evs.slice(0, 3).forEach((e) => {
      const chip = document.createElement("div");
      chip.className = "cal-chip" + (e.allDay ? " allday" : "");
      chip.textContent = e.title || "(busy)";
      cell.appendChild(chip);
    });
    if (evs.length > 3) {
      const more = document.createElement("div");
      more.className = "cal-more";
      more.textContent = `+${evs.length - 3} more`;
      cell.appendChild(more);
    }
    cell.addEventListener("click", () => { state.date = iso; location.hash = "#/"; });
    els.calGrid.appendChild(cell);
  }
}

function shiftCalMonth(delta) {
  const c = ensureCalState();
  let m = c.month + delta, y = c.year;
  if (m < 0) { m = 11; y--; }
  else if (m > 11) { m = 0; y++; }
  state.cal = { year: y, month: m };
  loadCalendar();
}

async function connectCalendar() {
  els.calConnectBtn.textContent = "Connecting… (check your browser)";
  try {
    const r = await (await fetch("/api/calendar/connect", { method: "POST" })).json();
    els.calConnectBtn.textContent = "Connect Google Calendar";
    if (r.configured) loadCalendar();
  } catch (e) { els.calConnectBtn.textContent = "Connect failed — retry"; }
}

els.calConnectBtn.addEventListener("click", connectCalendar);
els.calPrev.addEventListener("click", () => shiftCalMonth(-1));
els.calNext.addEventListener("click", () => shiftCalMonth(1));

// ================= Agents panel =================
async function loadAgents() {
  let s = { enabled: false };
  try { s = await (await fetch("/api/agents/status")).json(); } catch (e) {}
  els.agentsDisabled.hidden = !!s.enabled;
  if (!s.enabled) {
    els.agentCounts.textContent = "";
    els.approvalRows.innerHTML = "";
    els.outboxRows.innerHTML = "";
    return;
  }
  const c = s.counts || {};
  els.agentCounts.textContent =
    `INBOX ${c.inbox || 0} · CLAIMED ${c.claimed || 0} · DONE ${c.done || 0} · FAILED ${c.failed || 0}`;
  renderApprovals(s.approvals || []);
  renderOutbox(s.outbox || []);
}

function renderApprovals(list) {
  els.approvalRows.innerHTML = "";
  if (!list.length) {
    const e = document.createElement("div");
    e.className = "ro-row empty";
    e.textContent = "No pending approvals";
    els.approvalRows.appendChild(e);
    return;
  }
  list.forEach((a) => {
    const row = document.createElement("div");
    row.className = "approval-row";
    const info = document.createElement("div");
    info.className = "approval-info";
    const action = document.createElement("span");
    action.className = "approval-action";
    action.textContent = a.action;
    const body = document.createElement("span");
    body.className = "approval-body";
    body.textContent = a.body || "";
    info.append(action, body);
    const ok = document.createElement("button");
    ok.className = "pill approve";
    ok.textContent = "Confirm";
    ok.addEventListener("click", () => agentAction("/api/agents/approvals/confirm", { id: a.id }));
    const no = document.createElement("button");
    no.className = "pill reject";
    no.textContent = "Reject";
    no.addEventListener("click", () => agentAction("/api/agents/approvals/reject", { id: a.id, reason: "rejected from dashboard" }));
    row.append(info, ok, no);
    els.approvalRows.appendChild(row);
  });
}

function renderOutbox(list) {
  els.outboxRows.innerHTML = "";
  if (!list.length) {
    const e = document.createElement("div");
    e.className = "ro-row empty";
    e.textContent = "Nothing in the outbox yet";
    els.outboxRows.appendChild(e);
    return;
  }
  list.forEach((it) => {
    const row = document.createElement("div");
    row.className = "outbox-row";
    const t = document.createElement("span");
    t.className = "outbox-title";
    t.textContent = it.title || it.name;
    const when = document.createElement("span");
    when.className = "outbox-when";
    when.textContent = (it.modTime || "").slice(0, 16).replace("T", " ");
    row.append(t, when);
    els.outboxRows.appendChild(row);
  });
}

async function agentAction(path, body) {
  setSaveState("saving");
  try {
    await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); }
  loadAgents();
}

function startAgentsPoll() {
  stopAgentsPoll();
  state.agentsPoll = setInterval(loadAgents, 5000);
}
function stopAgentsPoll() {
  if (state.agentsPoll) { clearInterval(state.agentsPoll); state.agentsPoll = null; }
}

// ---- router ----
function route() {
  const h = location.hash;
  const goals = h === "#/goals";
  const cal = h === "#/calendar";
  const ag = h === "#/agents";
  const day = !goals && !cal && !ag;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.calendarView.hidden = !cal;
  els.agentsView.hidden = !ag;
  els.dateNav.hidden = !day;
  els.goalsNav.hidden = !day;
  els.calNav.hidden = !day;
  els.agentsNav.hidden = !day;
  els.dayNav.hidden = day;
  if (!ag) stopAgentsPoll();
  if (goals) loadGoals();
  else if (cal) loadCalendar();
  else if (ag) { loadAgents(); startAgentsPoll(); }
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));
els.addTask.addEventListener("click", () => {
  addTaskRow({ text: "", done: false }, els.taskRows.querySelectorAll(".trow").length + 1);
});

route();
