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
  todosView: document.getElementById("todosView"),
  todosRows: document.getElementById("todosRows"),
  todosMeta: document.getElementById("todosMeta"),
  calendarView: document.getElementById("calendarView"),
  dateNav: document.getElementById("dateNav"),
  // shell: rail + breadcrumb bar (redesign §2)
  appShell: document.getElementById("appShell"),
  railGroups: document.getElementById("railGroups"),
  railSearch: document.getElementById("railSearch"),
  railCollapse: document.getElementById("railCollapse"),
  crumbBack: document.getElementById("crumbBack"),
  crumbFwd: document.getElementById("crumbFwd"),
  crumbPath: document.getElementById("crumbPath"),
  crumbMeta: document.getElementById("crumbMeta"),
  contentScroll: document.getElementById("contentScroll"),
  // contacts (people layer over the vault index)
  contactsView: document.getElementById("contactsView"),
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
  readingStrip: document.getElementById("readingStrip"),
  bookShelf: document.getElementById("bookShelf"),
  bookSearch: document.getElementById("bookSearch"),
  bookSort: document.getElementById("bookSort"),
  bookFilter: document.getElementById("bookFilter"),
  bookAddBtn: document.getElementById("bookAddBtn"),
  // PROPERTIES — real-estate cockpit
  propertiesView: document.getElementById("propertiesView"),
  propertyBoard: document.getElementById("propertyBoard"),
  propRail: document.getElementById("propRail"),
  propInspector: document.getElementById("propInspector"),
  propertyPage: document.getElementById("propertyPage"),
  propertyParcels: document.getElementById("propertyParcels"),
  propertySettings: document.getElementById("propertySettings"),
  propertyMapWrap: document.getElementById("propertyMapWrap"),
  propertyMap: document.getElementById("propertyMap"),
  propertyMapLegend: document.getElementById("propertyMapLegend"),
  propertyUnmapped: document.getElementById("propertyUnmapped"),
  // AION — program cockpit over system/aion/
  aionView: document.getElementById("aionView"),
  aionMeta: document.getElementById("aionMeta"),
  aionToggle: document.getElementById("aionToggle"),
  aionBody: document.getElementById("aionBody"),
  aionPublishRail: document.getElementById("aionPublishRail"),
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
  feedNavBadge: document.getElementById("feedNavBadge"),
  feedFilters: document.getElementById("feedFilters"),
  feedSignals: document.getElementById("feedSignals"),
  feedList: document.getElementById("feedList"),
  feedErrandBtn: document.getElementById("feedErrandBtn"),
  feedAskBtn: document.getElementById("feedAskBtn"),
  feedRunNowBtn: document.getElementById("feedRunNowBtn"),
  // content studio (draft board + inspiration watchlist)
  studioView: document.getElementById("studioView"),
  studioTabs: document.getElementById("studioTabs"),
  studioRuns: document.getElementById("studioRuns"),
  studioBody: document.getElementById("studioBody"),
  // spirits (excalibur harness) view
  spiritsView: document.getElementById("spiritsView"),
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

// ---- save plumbing (debounced per endpoint+date) ----
// Date AND payload are snapshotted at QUEUE time: navigating to another day
// inside the debounce window must never retarget a pending save (that once
// cloned a whole day's tasks into tomorrow's fresh note).
const savers = {};
function queueSave(endpoint, payloadFn) {
  setSaveState("saving");
  const date = state.date;
  const payload = payloadFn();
  const key = endpoint + "|" + date;
  clearTimeout(savers[key]);
  savers[key] = setTimeout(async () => {
    try {
      await fetch(`/api/${endpoint}?date=${date}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
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
