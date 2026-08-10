// ================= Calendar (month view) =================
const MONTHS = ["January","February","March","April","May","June","July","August","September","October","November","December"];

function ensureCalState() {
  if (!state.cal) {
    const d = new Date();
    state.cal = { year: d.getFullYear(), month: d.getMonth() };
  }
  return state.cal;
}

// monthGridDays returns the month's cells, Monday-first, sized to EXACTLY the
// rows the month needs (§14): offset is a COUNT of cells before the 1st, and
// the grid is ceil((offset + daysInMonth) / 7) * 7 — never a fixed height, so
// a Saturday-starting 31-day month gets its six rows and a 28-day month
// starting Monday gets four.
function monthGridDays(year, month) {
  const offset = (new Date(year, month, 1).getDay() + 6) % 7; // Monday = 0
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const total = Math.ceil((offset + daysInMonth) / 7) * 7;
  const cells = [];
  for (let i = 0; i < total; i++) {
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
      const r = await (await fetch(`/api/calendar/events?start=${cells[0].iso}&end=${cells[cells.length - 1].iso}`)).json();
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
    cell.addEventListener("click", () => {
      // phone (Rev 4): cells show dots, not titles — a tap opens the day's
      // agenda as a sheet; "open day →" inside it does what desktop click does.
      if (window.mf && window.mf.phone()) { mfCalAgenda(iso, evs); return; }
      state.date = iso; location.hash = "#/";
    });
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

// mfCalAgenda (Rev 4, phone-only): the tapped day's events as a bottom sheet —
// time · title rows plus "open day →" (which does what the desktop click does).
function mfCalAgenda(iso, evs) {
  window.mfSheet.open((body) => {
    const d = new Date(iso + "T00:00:00");
    body.append(el("div", "mf-agenda-day",
      d.toLocaleDateString([], { weekday: "long", month: "long", day: "numeric" })));
    if (!evs.length) body.append(el("div", "mf-agenda-ev", "no events"));
    evs.forEach((e) => {
      const row = el("div", "mf-agenda-ev");
      row.append(el("span", "mf-agenda-time", e.allDay ? "all day" : formatTime(e.start)));
      row.append(el("span", "mf-agenda-title", e.title || "(busy)"));
      body.append(row);
    });
    const open = el("button", "mf-agenda-open", "open day →");
    open.onclick = () => { window.mfSheet.close({ silent: true }); state.date = iso; location.hash = "#/"; };
    body.append(open);
  }, { key: "cal-agenda" });
}
