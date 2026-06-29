// Manifest — local daily-planner UI over your Obsidian vault.
// State lives in markdown files; this is a thin editor with autosave.

const SLOTS = 3; // goals / milestones slots, matching the vv.xyz layout

// category icons for the goals / milestones slots (mood, image, clock)
const SLOT_ICONS = [
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M8.5 14.5c.9 1.2 2.1 1.8 3.5 1.8s2.6-.6 3.5-1.8"/><circle cx="9" cy="10" r=".6" fill="currentColor"/><circle cx="15" cy="10" r=".6" fill="currentColor"/></svg>',
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3.5" y="5.5" width="17" height="13" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="M5 17l4.5-4 3 2.5L16 12l3 3.5"/></svg>',
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 2"/></svg>',
];

const state = { date: isoToday(), day: null };

const els = {
  dateLabel: document.getElementById("dateLabel"),
  streakText: document.getElementById("streakText"),
  saveState: document.getElementById("saveState"),
  scheduleRows: document.getElementById("scheduleRows"),
  scheduleRange: document.getElementById("scheduleRange"),
  goalsRows: document.getElementById("goalsRows"),
  goalsRange: document.getElementById("goalsRange"),
  milestonesRows: document.getElementById("milestonesRows"),
  milestonesRange: document.getElementById("milestonesRange"),
  taskRows: document.getElementById("taskRows"),
  addTask: document.getElementById("addTask"),
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

// ---- time helpers (must mirror store.go) ----
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
  queueSave("day", () => ({ schedule: state.day.schedule, tasks: collectTasks() }));
}
function saveGoals() { queueSave("goals", () => ({ items: collectSlots(els.goalsRows) })); }
function saveMilestones() { queueSave("milestones", () => ({ items: collectSlots(els.milestonesRows) })); }

async function refreshStreak() {
  try {
    const r = await fetch(`/api/day?date=${state.date}`);
    renderStreak((await r.json()).streak);
  } catch (e) {}
}

// ---- load + render ----
async function load(date) {
  state.date = date;
  const today = date === isoToday();
  els.dateLabel.textContent = today ? "TODAY" : prettyDate(date);
  const r = await fetch(`/api/day?date=${date}`);
  state.day = await r.json();
  render();
}

function render() {
  const day = state.day;
  renderStreak(day.streak);
  els.goalsRange.textContent = (day.quarter || "QUARTER").toUpperCase();
  els.milestonesRange.textContent = (day.month || "MONTH").toUpperCase();
  if (day.schedule.length) {
    els.scheduleRange.textContent =
      `${hourLabel(Math.floor(slotMin(day.schedule[0].time) / 60))}–` +
      `${hourLabel(Math.floor(slotMin(day.schedule[day.schedule.length - 1].time) / 60))}`;
  }
  renderSchedule(day.schedule);
  renderSlots(els.goalsRows, day.goals, "goal", saveGoals);
  renderSlots(els.milestonesRows, day.milestones, "milestone", saveMilestones);
  renderTasks(day.tasks);
}

function renderStreak(n) {
  els.streakText.textContent = `${n} DAY${n === 1 ? "" : "S"} STREAK`;
}

// Schedule: two input lines per hour (:00 / :30), one focus circle per hour,
// and duration connectors drawn from each filled slot to the next.
function renderSchedule(slots) {
  els.scheduleRows.innerHTML = "";
  const overlay = document.createElement("div");
  overlay.className = "connectors";
  overlay.id = "connectors";
  els.scheduleRows.appendChild(overlay);

  // group slots by hour, preserving order
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
      input.className = "sslot" + (slot.label ? " filled" : "");
      input.value = slot.label || "";
      input.dataset.idx = i;
      input.addEventListener("input", () => {
        state.day.schedule[i].label = input.value;
        input.classList.toggle("filled", input.value.trim() !== "");
        drawConnectors();
      });
      input.addEventListener("change", saveDay);
      body.appendChild(input);
    });

    const focusCell = document.createElement("div");
    focusCell.className = "shour-focus";
    const lead = entries[0].i; // focus tracked on the hour's first slot
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

// Draw a vertical line from each filled slot down to the next filled slot,
// labelled with the elapsed duration (30m, 1h, 1.5h, ...).
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

function renderSlots(container, items, kind, onSave) {
  container.innerHTML = "";
  for (let i = 0; i < SLOTS; i++) {
    const slot = document.createElement("div");
    slot.className = "slot";
    const marker = document.createElement("span");
    marker.className = "marker";
    marker.innerHTML = SLOT_ICONS[i % SLOT_ICONS.length];
    const input = document.createElement("input");
    input.className = items[i] ? "filled" : "";
    input.value = items[i] || "";
    input.placeholder = `${kind} ${i + 1}`;
    input.addEventListener("input", () => input.classList.toggle("filled", input.value.trim() !== ""));
    input.addEventListener("change", onSave);
    slot.append(marker, input);
    container.appendChild(slot);
  }
}
function collectSlots(container) {
  return [...container.querySelectorAll("input")].map((i) => i.value.trim()).filter((v) => v);
}

function renderTasks(tasks) {
  els.taskRows.innerHTML = "";
  const list = tasks.length ? tasks : [{ text: "", done: false }];
  list.forEach((t, i) => addTaskRow(t, i + 1));
}
function addTaskRow(task, num) {
  const row = document.createElement("div");
  row.className = "trow";
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
    .map((row) => ({
      text: row.querySelector(".ttext").value.trim(),
      done: row.querySelector(".ttext").classList.contains("done"),
    }))
    .filter((t) => t.text.length > 0);
}

// ---- events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));
els.addTask.addEventListener("click", () => {
  addTaskRow({ text: "", done: false }, els.taskRows.querySelectorAll(".trow").length + 1);
});

load(state.date);
