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
  // capture can target a property directly (Rev 3) — the line lands in THAT
  // property's file, never as a parallel copy here
  const propSel = document.createElement("select");
  propSel.className = "pp-in board-select";
  propSel.title = "optional: capture onto a property page instead";
  {
    const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; propSel.append(o); };
    opt("", "no property");
    ((todosCache && todosCache.containers) || []).filter((c) => c.kind === "property")
      .forEach((c) => opt(c.slug, "⌂ " + c.name));
  }
  const close = () => overlay.remove();
  const submit = async () => {
    const text = input.value.trim();
    if (!text) { close(); return; }
    const body = { text, domain };
    if (propSel.value) body.container = { kind: "property", slug: propSel.value };
    const tv = tether.value;
    if (tv.startsWith("rock:")) body.rock = tv.slice(5);
    else if (tv.startsWith("issue:")) body.issue = tv.slice(6);
    else if (tv.startsWith("bucket:")) body.bucket = tv.slice(7);
    try {
      await postJSONOk("/api/todos/item", body);
      showToast("Todo captured → " + (propSel.value ? propSel.selectedOptions[0].textContent : (domain || "Inbox")));
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
  propSel.addEventListener("change", () => input.focus());
  chips.append(tether, propSel, add);
  panel.append(input, chips);
  overlay.append(back, panel);
  document.body.append(overlay);
  input.focus();
}

// ---- the shell: left rail + breadcrumbs + in-app history (redesign §2) ----
// The rail and crumbs are just a new rendering of the existing hash routes;
// every counter here is DERIVED from the same models as the surfaces — no
// count is ever a string literal.
const NAV_SECTIONS = [
  { label: "PLAN", items: [
    { key: "day", label: "Day", glyph: "☀", hash: "#/" },
    { key: "goals", label: "Goals", glyph: "◎", hash: "#/goals", counted: true },
    { key: "todos", label: "Todos", glyph: "✓", hash: "#/todos", counted: true },
  ]},
  { label: "WORK", items: [
    { key: "aion", label: "Aion", glyph: "◆", hash: "#/aion", counted: true },
    { key: "properties", label: "Real Estate", glyph: "⌂", hash: "#/properties", counted: true },
    { key: "studio", label: "Studio", glyph: "▤", hash: "#/studio" },
  ]},
  { label: "SIGNAL", items: [
    { key: "feed", label: "Feed", glyph: "≋", hash: "#/feed" }, // count = the inbox badge (feedNavBadge)
    { key: "capture", label: "Capture", glyph: "⊕", hash: "#/capture" }, // count = open tray cards
    { key: "terminal", label: "Terminal", glyph: "❯", hash: "#/terminal" },
    { key: "spirits", label: "Spirits", glyph: "✦", hash: "#/spirits" },
    { key: "contacts", label: "Contacts", glyph: "◍", hash: "#/contacts" },
    { key: "calendar", label: "Calendar", glyph: "▦", hash: "#/calendar" },
    { key: "reading", label: "Reading", glyph: "▢", hash: "#/reading" },
  ]},
];

let uiHistory = [];   // back stack of hashes
let uiForward = [];   // forward stack
let _navInternal = false;             // set by navBack/navForward so route() doesn't re-push
let _curHash = normHash(location.hash);
let _crumbSection = "";               // meta clears when the section changes
const railItems = {};                 // key -> { a, count }

function normHash(h) { return !h || h === "#" ? "#/" : h; }

function sectionOf(h) {
  if (h.startsWith("#/note/")) return "note";
  if (h.startsWith("#/artifact/")) return "artifact";
  const seg = h.replace(/^#\//, "").split("/")[0];
  return ["goals","todos","calendar","feed","chat","capture","terminal","studio","spirits","contacts","reading","properties","aion"].includes(seg) ? seg : "day";
}

function buildRail() {
  const host = els.railGroups;
  host.innerHTML = "";
  NAV_SECTIONS.forEach((g, gi) => {
    // a flexible spacer before every group but the first spreads the sections
    // down the rail's full height (styling in 07-nav.css .rail-spacer)
    if (gi > 0) host.append(el("div", "rail-spacer"));
    const grp = el("div", "rail-group");
    grp.append(el("div", "rail-group-label", g.label));
    g.items.forEach((it) => {
      const a = document.createElement("a");
      a.className = "rail-item";
      a.href = it.hash;
      a.title = it.label;
      const count = el("span", "rail-count");
      a.append(el("span", "rail-glyph", it.glyph), el("span", "rail-label", it.label), count);
      if (it.key === "feed") { count.id = "feedNavBadge"; count.hidden = true; els.feedNavBadge = count; }
      if (it.key === "capture") { count.id = "captureNavBadge"; count.hidden = true; }
      grp.append(a);
      railItems[it.key] = { a, count, counted: !!it.counted };
    });
    host.append(grp);
  });
}

function setRailActive() {
  const active = sectionOf(_curHash);
  Object.entries(railItems).forEach(([key, r]) => r.a.classList.toggle("active", key === active));
  // chat lives BEHIND THE LOGO (todo-panel plan D7): the brand is its only
  // nav entry, so it alone lights up on #/chat — no rail item does.
  const brand = document.getElementById("railBrand");
  if (brand) brand.classList.toggle("brand-active", active === "chat");
}

function railSetCount(key, n) {
  const r = railItems[key];
  if (!r || !r.counted) return;
  r.count.textContent = n > 0 ? String(n) : "";
}

// Boot-time derivation of the Plan/Work rail counts from the same endpoints
// the surfaces read. Each surface keeps them honest as it loads (stages 5–8).
async function refreshRailCounts() {
  const j = (u) => fetch(u).then((r) => (r.ok ? r.json() : null)).catch(() => null);
  const [td, gl, ai, pr] = await Promise.all([j("/api/todos"), j("/api/goals"), j("/api/aion"), j("/api/properties")]);
  if (td && td.counts) railSetCount("todos", td.counts.todos || 0); // same derivation as the surface — one truth
  if (gl && gl.areas) railSetCount("goals", gl.areas.reduce((n, a) => n + ((a.rocks || []).filter((r) => !r.checked).length), 0));
  if (ai && ai.backlog) railSetCount("aion", ai.backlog.filter((b) => !b.checked).length);
  if (pr && pr.properties) railSetCount("properties", pr.properties.length);
}

// ---- in-app history: the crumb ‹ › drive these stacks, not window.history ----
function navBack() {
  if (!uiHistory.length) return;
  uiForward.push(_curHash);
  _navInternal = true;
  location.hash = uiHistory.pop();
}
function navForward() {
  if (!uiForward.length) return;
  uiHistory.push(_curHash);
  _navInternal = true;
  location.hash = uiForward.pop();
}
function updateCrumbNav() {
  els.crumbBack.disabled = !uiHistory.length;
  els.crumbFwd.disabled = !uiForward.length;
}

function renderCrumbs(h) {
  const host = els.crumbPath;
  host.innerHTML = "";
  const parts = [];
  if (h.startsWith("#/note/")) {
    const p = decodeURIComponent(h.slice("#/note/".length));
    parts.push({ label: "note", hash: null });
    parts.push({ label: p.split("/").pop().replace(/\.md$/, "") });
  } else if (h.startsWith("#/artifact/")) {
    const seg = h.slice("#/artifact/".length).split("/");
    const label = seg[0] === "run" ? "run report"
      : decodeURIComponent(seg.slice(2).join("/") || "").split("/").pop().replace(/\.md$/, "") || "artifact";
    parts.push({ label: "artifact", hash: null });
    parts.push({ label });
  } else {
    const sec = sectionOf(h);
    // display label diverges from the route key only for real estate (key stays
    // "properties" so hash/counts/routing are untouched)
    const secLabel = sec === "properties" ? "real estate" : sec;
    parts.push({ label: secLabel, hash: sec === "day" ? "#/" : "#/" + sec });
    if (sec !== "day") {
      h.replace(/^#\//, "").split("/").filter(Boolean).slice(1)
        .forEach((s) => parts.push({ label: decodeURIComponent(s) }));
    }
  }
  parts.forEach((p, i) => {
    if (i) host.append(el("span", "crumb-sep", "/"));
    const last = i === parts.length - 1;
    if (!last && p.hash) {
      const a = document.createElement("a");
      a.className = "crumb-seg";
      a.href = p.hash;
      a.textContent = p.label;
      host.append(a);
    } else {
      host.append(el("span", "crumb-seg", p.label));
    }
  });
  const sec = sectionOf(h);
  if (sec !== _crumbSection) { _crumbSection = sec; setCrumbMeta(""); }
}

// Surfaces call this with their derived context string ("34 open · 3 decisions", …).
function setCrumbMeta(text) { els.crumbMeta.textContent = text || ""; }

// ---- rail collapse (persisted; auto-collapses under 860px) ----
function setRailCollapsed(on, persist) {
  els.appShell.classList.toggle("rail-collapsed", on);
  els.railCollapse.textContent = on ? "››" : "‹‹";
  els.railCollapse.title = on ? "Expand the rail" : "Collapse the rail";
  if (persist) localStorage.setItem("manifest.rail.collapsed", on ? "1" : "0");
}
function railPref() { return localStorage.getItem("manifest.rail.collapsed") === "1"; }
function applyRailWidth() {
  // Phone band (Rev 4): the drawer CSS in 95-mobile.css owns the rail — the
  // icon-strip collapsed mode must not be reachable there. matchMedia (not
  // innerWidth) so JS agrees with CSS at exactly 860. ≥861 unchanged.
  if (window.matchMedia("(max-width: 860px)").matches) { setRailCollapsed(false, false); return; }
  setRailCollapsed(railPref(), false);
}

function route() {
  const h = normHash(location.hash);
  if (h !== _curHash) {
    if (_navInternal) _navInternal = false;
    else { uiHistory.push(_curHash); uiForward.length = 0; }
    _curHash = h;
  }
  updateCrumbNav();
  setRailActive();
  renderCrumbs(h);
  // the note view's Back returns to wherever the user actually was: every
  // non-note route records itself as the return target. Explicit
  // _noteReturn sets (contact page, studio→feed) still win — they happen
  // after the last non-note route, and note routes never overwrite.
  if (!h.startsWith("#/note/") && !h.startsWith("#/artifact/")) _noteReturn = h === "#/" ? "#/" : h;
  const goals = h === "#/goals" || h.startsWith("#/goals/"); // #/goals/<id> deep-links a Rock
  const todosTab = h === "#/todos" || h.startsWith("#/todos/");
  const cal = h === "#/calendar";
  const fd = h === "#/feed";
  const chat = h === "#/chat" || h.startsWith("#/chat/");
  const capture = h === "#/capture";
  // FILES lives inside the terminal cockpit now (its stage tab)
  if (h === "#/files") {
    try { localStorage.setItem("manifest.termStage", "files"); } catch (e) {}
    location.hash = "#/terminal";
    return;
  }
  const terminalTab = h === "#/terminal";
  const studio = h === "#/studio" || h.startsWith("#/studio/");
  if (h === "#/spirits/approvals") { location.hash = "#/feed"; return; } // approvals live in FEED now
  const sp = h === "#/spirits" || h.startsWith("#/spirits/");
  const contacts = h === "#/contacts" || h.startsWith("#/contacts/");
  const reading = h === "#/reading" || h.startsWith("#/reading/");
  const properties = h === "#/properties" || h.startsWith("#/properties/");
  const aionTab = h === "#/aion" || h.startsWith("#/aion/");
  const note = h.startsWith("#/note/");
  const artifact = h.startsWith("#/artifact/");
  const day = !goals && !todosTab && !cal && !fd && !chat && !capture && !terminalTab && !studio && !sp && !contacts && !reading && !properties && !aionTab && !note && !artifact;
  els.dayView.hidden = !day;
  els.goalsView.hidden = !goals;
  els.todosView.hidden = !todosTab;
  els.calendarView.hidden = !cal;
  els.feedView.hidden = !fd;
  if (els.chatView) els.chatView.hidden = !chat;
  if (els.captureView) els.captureView.hidden = !capture;
  if (els.terminalView) els.terminalView.hidden = !terminalTab;
  els.studioView.hidden = !studio;
  els.spiritsView.hidden = !sp;
  els.contactsView.hidden = !contacts;
  els.readingView.hidden = !reading;
  els.propertiesView.hidden = !properties;
  els.aionView.hidden = !aionTab;
  els.noteView.hidden = !note;
  if (els.artifactView) els.artifactView.hidden = !artifact;
  els.dateNav.hidden = !day;
  // the crumb bar is redundant on top-level sections (each carries its own
  // title header); keep it only for the Day home (date-nav) and note/artifact
  // drill-downs (breadcrumb + Back). Desktop-only — on the phone band the crumb
  // bar IS the top bar (☰/⌕/＋), so 07-nav.css gates the hide to ≥861px.
  els.appShell.classList.toggle("crumb-hidden", !(day || note || artifact));
  if (!aionTab && els.aionPublishRail) els.aionPublishRail.innerHTML = ""; // PUBLISH slot is aion-only
  if (!properties && els.rePublishRail) els.rePublishRail.innerHTML = ""; // RE PUBLISH slot is properties-only
  els.contentScroll.scrollTop = 0;
  refreshFeedBadge(); // the rail's Feed count doubles as the inbox badge — keep it honest everywhere
  if (goals) {
    // "#/goals/history" is the archive tab; any other suffix is a goal-id
    // deep-link (safe: real ids always contain "/", so "history" can't collide).
    const suffix = h.startsWith("#/goals/") ? decodeURIComponent(h.slice("#/goals/".length)) : "";
    if (suffix === "history") showGoalsHistory();
    else loadGoals(suffix);
  }
  else if (todosTab) { // the third surface — `to do.md` board (+ panel deep link)
    if (h.startsWith("#/todos/")) todoDeepLink = decodeURIComponent(h.slice("#/todos/".length));
    loadTodos();
  }
  else if (cal) loadCalendar();
  else if (fd) showFeed(); // manifest's one inbox
  else if (chat) showChat(h); // conversations with chattable spirits
  else if (capture) showCapture(); // the tray — triage into todos/chat
  else if (terminalTab) showTerminal(); // the cockpit: terminal / files / activity
  else if (studio) showStudio(); // content studio: draft board + inspiration
  else if (sp) showSpirits(h); // spirits cockpit: rituals / runs / settings / spirit pages
  else if (contacts) showContacts(); // people layer: list / page
  else if (reading) loadReading(); // book shelf over the extrinsic zone
  else if (properties) showProperties(h); // real-estate cockpit: board / property page
  else if (aionTab) showAion(h); // aion program cockpit: backlog / heuristics / vto / …
  else if (note) showNote(decodeURIComponent(h.slice("#/note/".length))); // universal note view
  else if (artifact) showArtifact(h.slice("#/artifact/".length)); // full-page agent-artifact reader
  else load(state.date); // reload so goal/calendar edits reflect in the day
}
window.addEventListener("hashchange", route);

// ---- shell wiring ----
buildRail();
applyRailWidth();
els.railCollapse.addEventListener("click", () => {
  const on = !els.appShell.classList.contains("rail-collapsed");
  setRailCollapsed(on, true);
});
els.railSearch.addEventListener("click", () => openCmdbar());
// the logo IS the chat entry (D7 — hidden, not removed: ⌘K/⌘J still reach it)
document.getElementById("railBrand").addEventListener("click", () => { location.hash = "#/chat"; });
document.getElementById("railRaw").addEventListener("click", () => toggleRawOverlay());
els.crumbBack.addEventListener("click", navBack);
els.crumbFwd.addEventListener("click", navForward);
window.addEventListener("resize", applyRailWidth);

// ---- day events ----
document.getElementById("prevBtn").addEventListener("click", () => load(shiftDate(state.date, -1)));
document.getElementById("nextBtn").addEventListener("click", () => load(shiftDate(state.date, 1)));
document.getElementById("todayBtn").addEventListener("click", () => load(isoToday()));

route();
refreshRailCounts();
