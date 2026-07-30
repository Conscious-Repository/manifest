// ---- global quick-add: press t anywhere (or via Cmd+K) — one line + domain, ≤2s ----
function openTodoQuickAdd(prefill) {
  if (document.getElementById("todoQuickAdd")) return;
  const overlay = el("div", "cmdbar");
  overlay.id = "todoQuickAdd";
  const back = el("div", "cmdbar-backdrop");
  const panel = el("div", "cmdbar-card");
  const input = inputEl("what must happen…");
  input.className = "cmdbar-input";
  if (prefill) input.value = prefill;
  const chips = el("div", "tdo-qa-chips");
  let domain = "";
  const names = ["Inbox", ...(todosCache && todosCache.areas ? todosCache.areas : ["Aion", "Real Estate", "Home", "Personal"])];
  names.forEach((n, i) => {
    const c = el("button", "filter-chip" + (i === 0 ? " on" : ""), n.toUpperCase());
    c.onclick = () => {
      domain = n === "Inbox" ? "" : n;
      chips.querySelectorAll(".filter-chip").forEach((b) => b.classList.remove("on"));
      c.classList.add("on");
      fillTether();
      input.focus();
    };
    chips.append(c);
  });
  // optional tether/bucket — ONE picker across the domain's Rocks, issues,
  // and buckets (none required; rebuilt when the domain chip changes)
  const tether = document.createElement("select");
  tether.className = "pp-in board-select";
  tether.title = "optional: what this advances, or where it belongs";
  let goalsAreas = null;
  const fillTether = async () => {
    if (goalsAreas === null) {
      try { goalsAreas = (await (await fetch("/api/goals")).json()).areas || []; } catch (e) { goalsAreas = []; }
    }
    tether.innerHTML = "";
    const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; tether.append(o); };
    opt("", "no tether");
    const area = goalsAreas.find((a) => a.name === domain);
    ((area && area.rocks) || []).filter((r) => !r.checked).forEach((r) => opt("rock:" + r.id, "⧗ " + r.text));
    const dv = (todosCache.domains || []).find((dm) => dm.name === (domain || "Inbox"));
    ((dv && dv.issues) || []).filter((i) => !i.checked).forEach((i) => opt("issue:" + i.id, "⚑ " + i.text));
    ((dv && dv.buckets) || []).forEach((bk) => opt("bucket:" + bk.name, "▸ " + bk.name));
  };
  fillTether();
  const close = () => overlay.remove();
  const submit = async () => {
    const text = input.value.trim();
    if (!text) { close(); return; }
    const body = { text, domain };
    const tv = tether.value;
    if (tv.startsWith("rock:")) body.rock = tv.slice(5);
    else if (tv.startsWith("issue:")) body.issue = tv.slice(6);
    else if (tv.startsWith("bucket:")) body.bucket = tv.slice(7);
    try {
      await postJSONOk("/api/todos/item", body);
      showToast("Todo captured" + (domain ? " → " + domain : " → Inbox"));
      close();
      if (!els.todosView.hidden) loadTodos();
    } catch (e) { showToast("Couldn't capture"); }
  };
  // Enter/Escape work from ANYWHERE in the dialog — picking a domain chip or
  // a tether from the select must never strand the capture without a confirm.
  panel.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") { ev.preventDefault(); submit(); }
    else if (ev.key === "Escape") close();
  });
  tether.addEventListener("change", () => input.focus()); // hands back to the text
  back.onclick = close;
  const add = pill("Add ↵", submit);
  add.classList.add("qa-add");
  chips.append(tether, add);
  panel.append(input, chips);
  overlay.append(back, panel);
  document.body.append(overlay);
  input.focus();
}

function route() {
  const h = location.hash;
  const goals = h === "#/goals" || h.startsWith("#/goals/"); // #/goals/<id> deep-links a Rock
  const todosTab = h === "#/todos" || h.startsWith("#/todos/");
  const cal = h === "#/calendar";
  const fd = h === "#/feed";
  const studio = h === "#/studio" || h.startsWith("#/studio/");
  if (h === "#/spirits/approvals") { location.hash = "#/feed"; return; } // approvals live in FEED now
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const contacts = h === "#/contacts" || h.startsWith("#/contacts/");
  const reading = h === "#/reading" || h.startsWith("#/reading/");
  const properties = h === "#/properties" || h.startsWith("#/properties/");
  const note = h.startsWith("#/note/");
  const day = !goals && !todosTab && !cal && !fd && !studio && !sp && !contacts && !reading && !properties && !note;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.todosView.hidden = !todosTab;
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
  els.todosNav.hidden = !day;
  els.feedNav.hidden = !day;
  els.studioNav.hidden = !day;
  els.calNav.hidden = !day;
  els.contactsNav.hidden = !day;
  els.readingNav.hidden = !day;
  els.propertiesNav.hidden = !day;
  els.spiritsNav.hidden = !day;
  els.moreNav.hidden = !day;
  els.dayNav.hidden = day;
  if (!day) setNavExpanded(false); // leaving the day view folds MORE back up
  if (day) refreshFeedBadge(); // pill only shows on the day view — keep it honest
  if (goals) {
    // "#/goals/history" is the archive tab; any other suffix is a goal-id
    // deep-link (safe: real ids always contain "/", so "history" can't collide).
    const suffix = h.startsWith("#/goals/") ? decodeURIComponent(h.slice("#/goals/".length)) : "";
    if (suffix === "history") showGoalsHistory();
    else loadGoals(suffix);
  }
  else if (todosTab) loadTodos(); // the third surface — `to do.md` board
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

// MORE ▾ — the day-view menu keeps GOALS/FEED/SPIRITS up front; the rest lives in a dropdown
function setNavExpanded(on) {
  els.navMoreMenu.hidden = !on;
  els.moreNav.setAttribute("aria-expanded", String(on));
  els.moreNav.textContent = on ? "MORE ▴" : "MORE ▾";
  if (on) {
    // menu is position:fixed (pill-group clips absolute children) — pin it under the button
    const r = els.moreNav.getBoundingClientRect();
    els.navMoreMenu.style.top = r.bottom + 6 + "px";
    els.navMoreMenu.style.right = window.innerWidth - r.right + "px";
  }
}
els.moreNav.addEventListener("click", () => setNavExpanded(els.navMoreMenu.hidden));
document.addEventListener("click", (e) => {
  if (!els.navMoreMenu.hidden && !els.navMoreWrap.contains(e.target)) setNavExpanded(false);
});
window.addEventListener("scroll", () => { if (!els.navMoreMenu.hidden) setNavExpanded(false); }, true);
window.addEventListener("resize", () => { if (!els.navMoreMenu.hidden) setNavExpanded(false); });

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
