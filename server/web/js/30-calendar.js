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
  let status = { accounts: [], hasCreds: false, accountStatuses: [] };
  try { status = await (await fetch("/api/calendar/status")).json(); } catch (e) {}
  const accounts = status.accounts || [];
  const statuses = status.accountStatuses || [];
  renderCalAccounts(accounts, !!status.hasCreds, statuses);

  const cells = monthGridDays(year, month);
  let events = [];
  let eventsError = false;
  if (accounts.length) {
    // A dead refresh token makes this 500 (Google's invalid_grant). Surface it
    // instead of swallowing the error into a silently empty month.
    try {
      const resp = await fetch(`/api/calendar/events?start=${cells[0].iso}&end=${cells[cells.length - 1].iso}`);
      if (!resp.ok) { eventsError = true; }
      else { events = (await resp.json()).events || []; }
    } catch (e) { eventsError = true; }
  }
  renderCalError(eventsError, statuses);
  renderMonth(cells, events);
}

// Show the accounts list (with per-account Disconnect, plus Reconnect when a
// token has gone stale) when ≥1 account is connected; otherwise the connect
// prompt (adapted for missing credentials). `statuses` is the per-account
// reauth verdict from /api/calendar/status.
function renderCalAccounts(accounts, hasCreds, statuses) {
  const has = accounts.length > 0;
  els.calAccounts.hidden = !has;
  els.calConnect.hidden = has;
  document.getElementById("calPasteBack")?.remove();
  if (!has) {
    els.calConnectBtn.hidden = !hasCreds;
    els.calConnect.querySelector("p").textContent = hasCreds
      ? "Connect a Google account (read-only) to see your events and auto-fill your schedule."
      : "Add google_credentials.json to ~/.config/manifest/ to connect Google Calendar.";
    return;
  }
  const byEmail = {};
  (statuses || []).forEach((s) => { byEmail[s.email] = s; });
  els.calAccountRows.innerHTML = "";
  accounts.forEach((email) => {
    const st = byEmail[email] || {};
    const row = document.createElement("div");
    row.className = "cal-account" + (st.needsReauth ? " needs-reauth" : "");

    const main = document.createElement("div");
    main.className = "cal-account-main";
    const name = document.createElement("span");
    name.className = "cal-account-email";
    name.textContent = email;
    main.appendChild(name);
    if (st.needsReauth) {
      const warn = document.createElement("span");
      warn.className = "cal-account-warn";
      warn.textContent = "sign-in expired — reconnect to restore your events";
      main.appendChild(warn);
    }

    const ctl = document.createElement("div");
    ctl.className = "cal-account-ctl";
    if (st.needsReauth) {
      const rc = document.createElement("button");
      rc.className = "cal-reconnect";
      rc.textContent = "Reconnect";
      rc.addEventListener("click", () => startCalConnect(rc));
      ctl.appendChild(rc);
    }
    const dc = document.createElement("button");
    dc.className = "cal-disconnect";
    dc.textContent = "Disconnect";
    dc.addEventListener("click", () => disconnectAccount(email));
    ctl.appendChild(dc);

    row.append(main, ctl);
    els.calAccountRows.appendChild(row);
  });
}

// renderCalError shows a standalone banner when the events fetch failed but the
// throttled per-account check hasn't caught up yet (the first load right after a
// token dies). Once a status row is flagged needsReauth it carries its own
// reconnect affordance, so the banner steps aside to avoid double-nagging.
function renderCalError(eventsError, statuses) {
  document.getElementById("calErrBanner")?.remove();
  const anyReauth = (statuses || []).some((s) => s.needsReauth);
  if (!eventsError || anyReauth) return;
  const banner = el("div", "cal-err-banner",
    "Couldn't load your events — your Google sign-in may have expired. Reconnect below to restore them.");
  banner.id = "calErrBanner";
  const host = els.calAccounts.hidden ? els.calConnect : els.calAccounts;
  host.prepend(banner);
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

// Connect (or reconnect) a Google account via the PASTE-BACK flow. Manifest runs
// headless on metis, so the old loopback listener never reaches the owner's
// browser; instead we open Google's consent URL, the owner approves, and pastes
// the resulting redirect address back. Safe to call repeatedly — Google shows
// the account chooser so you can pick a different account each time.
async function startCalConnect(anchor) {
  if (anchor) anchor.disabled = true;
  try {
    const r = await postJSONOk("/api/calendar/connect/start", {});
    window.open(r.authUrl, "_blank");
    showCalPasteBack();
    showToast("Approve in the Google tab, then paste the address it lands on", null, "info");
  } catch (e) {
    showToast("Couldn't start sign-in — " + (e.message || "error"));
  }
  if (anchor) anchor.disabled = false;
}

// showCalPasteBack renders step 2 of the flow (the paste box) into whichever
// account container is currently visible, replacing any earlier one.
function showCalPasteBack() {
  document.getElementById("calPasteBack")?.remove();
  const box = el("div", "cal-paste");
  box.id = "calPasteBack";
  box.append(el("div", "cal-paste-note",
    "after approving, the tab lands on an unreachable 127.0.0.1 page — copy its FULL address and paste it here"));
  const input = el("input", "cal-paste-input");
  input.type = "text";
  input.placeholder = "http://127.0.0.1:8123/oauth/callback?state=…&code=…";
  input.spellcheck = false;
  const fin = el("button", "cal-paste-finish", "finish connect");
  fin.onclick = async () => {
    fin.disabled = true; fin.textContent = "connecting…";
    try {
      const r = await postJSONOk("/api/calendar/connect/finish", { redirect: input.value });
      showToast("Connected " + r.connected, null, "info");
      loadCalendar();
    } catch (e) {
      fin.disabled = false; fin.textContent = "finish connect";
      showToast("Connect failed — " + (e.message || "check the pasted URL").slice(0, 140));
    }
  };
  box.append(input, fin);
  const host = els.calAccounts.hidden ? els.calConnect : els.calAccounts;
  host.appendChild(box);
  input.focus();
}

els.calConnectBtn.addEventListener("click", () => startCalConnect(els.calConnectBtn));
els.calAddAccount.addEventListener("click", () => startCalConnect(els.calAddAccount));
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
