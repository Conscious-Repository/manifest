// ================= HOME TIMELINE — the shared Home plan =================
// One canonical document (system/home/plan.json, docs/home-plan.md) read by
// both planners. The saved plan is the baseline. Every edit here lands in a
// DRAFT first — kept in this browser, shown as a draft, previewed by the
// server — and only "Save to plan" writes it, against the revision it was
// made on. Scenarios are read-only what-ifs; nothing here sets a real date
// or availability by itself. All text renders as text: task notes never
// execute as HTML.
const HP_DRAFT_KEY = "homePlan.draft.v1";
let hp = { data: null, view: null, error: "", scenario: "", draft: null, problems: [], previewing: false, saveError: "", stale: [] };

function hpLoadDraft() {
  try { const d = JSON.parse(localStorage.getItem(HP_DRAFT_KEY) || "null"); if (d && d.patch) return d; } catch (e) {}
  return null;
}
function hpStoreDraft() {
  try {
    if (hp.draft && Object.keys(hp.draft.patch).length) localStorage.setItem(HP_DRAFT_KEY, JSON.stringify(hp.draft));
    else localStorage.removeItem(HP_DRAFT_KEY);
  } catch (e) {}
}
hp.draft = hpLoadDraft();

function hpClone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }
// RFC 7396, the server's own rule: null deletes, objects merge, rest replaces
function hpMerge(doc, patch) {
  if (patch === null || typeof patch !== "object" || Array.isArray(patch)) return hpClone(patch);
  const out = doc && typeof doc === "object" && !Array.isArray(doc) ? hpClone(doc) : {};
  for (const [k, v] of Object.entries(patch)) {
    if (v === null) delete out[k]; else out[k] = hpMerge(out[k], v);
  }
  return out;
}
function hpGet(obj, path) { return path.reduce((o, k) => (o == null ? undefined : o[k]), obj); }
function hpPathKey(path) { return JSON.stringify(path); }

async function hpFetch(method, url, body) {
  const r = await fetch(url, method === "GET" ? undefined : { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const text = await r.text();
  let json = null;
  try { json = JSON.parse(text); } catch (e) {}
  if (!r.ok) {
    const err = new Error((json && json.error) || text.trim() || "HTTP " + r.status);
    err.status = r.status; err.problems = (json && json.problems) || []; err.revision = json && json.revision;
    throw err;
  }
  return json;
}

async function homePlanLoad() {
  hp.loading = true;
  try { hp.data = await hpFetch("GET", "/api/home/plan"); hp.error = ""; hp.loadedAt = Date.now(); }
  catch (e) { hp.error = e.message; }
  hp.loading = false;
  await hpRefreshView();
}

// the shown plan: saved, or saved + scenario + draft as the server derives it
function hpActivePatch() {
  let patch = {};
  const sc = hp.scenario && hp.data && hp.data.plan && (hp.data.plan.scenarios || {})[hp.scenario];
  if (sc) patch = hpMerge(patch, sc.patch);
  if (hp.draft) patch = hpMerge(patch, hp.draft.patch);
  return patch;
}
async function hpRefreshView() {
  hp.problems = [];
  const patch = hpActivePatch();
  if (!hp.data || !hp.data.plan || !Object.keys(patch).length) { hp.view = hp.data; hpRepaint(); return; }
  hp.previewing = true;
  try { hp.view = await hpFetch("POST", "/api/home/plan/preview", { patch }); }
  catch (e) { hp.view = hp.data; hp.problems = e.problems && e.problems.length ? e.problems : [e.message]; }
  hp.previewing = false;
  hpRepaint();
}
const hpRefreshSoon = debounce(hpRefreshView, 250);
// crossing the phone breakpoint swaps the grid for the agenda
matchMedia("(max-width: 860px)").addEventListener("change", () => hpRepaint());

// hpEdit records one field change in the draft. The value the saved plan held
// when the draft first touched it is remembered, so saving onto a plan someone
// else changed meanwhile shows exactly which fields collided.
function hpEdit(path, value) {
  if (!hp.data || !hp.data.plan) return;
  if (!hp.draft) hp.draft = { base: hp.data.revision, patch: {}, old: {} };
  const key = hpPathKey(path);
  if (!(key in hp.draft.old)) hp.draft.old[key] = hpGet(hp.data.plan, path) === undefined ? null : hpClone(hpGet(hp.data.plan, path));
  let node = hp.draft.patch;
  path.slice(0, -1).forEach((k) => { if (!node[k] || typeof node[k] !== "object") node[k] = {}; node = node[k]; });
  node[path[path.length - 1]] = value === undefined ? null : value;
  hpStoreDraft();
  hpRefreshSoon();
  hpRepaint();
}
function hpDiscardDraft() { hp.draft = null; hp.saveError = ""; hp.stale = []; hpStoreDraft(); hpRefreshView(); }
function hpDraftCount() { return hp.draft ? Object.keys(hp.draft.old).length : 0; }

// Save: a stale draft is never silently replayed over newer edits. Fields
// whose saved value moved since the draft touched them are listed first.
async function hpSaveDraft(force) {
  if (!hp.draft) return;
  hp.saveError = "";
  try {
    const fresh = await hpFetch("GET", "/api/home/plan");
    if (!force) {
      hp.stale = Object.entries(hp.draft.old).filter(([key, was]) => JSON.stringify(hpGet(fresh.plan, JSON.parse(key)) ?? null) !== JSON.stringify(was ?? null)).map(([key]) => JSON.parse(key));
      if (hp.stale.length) { hp.data = fresh; hp.saveError = "The plan changed on another device since this draft started."; hpRepaint(); return; }
    }
    typeof setSaveState === "function" && setSaveState("saving");
    hp.data = await hpFetch("POST", "/api/home/plan", { revision: fresh.revision, patch: hp.draft.patch });
    hp.draft = null; hp.stale = []; hpStoreDraft();
    typeof setSaveState === "function" && setSaveState("saved");
    showToast("Plan saved");
  } catch (e) {
    typeof setSaveState === "function" && setSaveState("error");
    hp.saveError = e.status === 409 ? "Someone saved the plan at the same moment. Your draft is kept — save again to review." : (e.problems && e.problems.length ? e.problems.join("; ") : e.message);
  }
  await hpRefreshView();
}

async function hpSaveScenario(label) {
  if (!hp.draft || !hp.data) return;
  await (async () => {
    const id = "user-" + label.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40);
    try {
      hp.data = await hpFetch("POST", "/api/home/plan", { revision: hp.data.revision, patch: { scenarios: { [id]: { label, basis: "user", patch: hp.draft.patch } } } });
      hp.draft = null; hpStoreDraft(); hp.scenario = id; showToast("Saved as a scenario — the baseline is unchanged");
    } catch (e) { showToast(e.message); }
    await hpRefreshView();
  })();
}

// ---------------- formatting ----------------
const HP_MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
function hpDate(s) { const [y, m, d] = s.split("-").map(Number); return { y, m, d }; }
function hpShort(s) { if (!s) return ""; const { m, d } = hpDate(s); return HP_MONTHS[m - 1] + " " + d; }
function hpWeekend(sat) { const { m, d } = hpDate(sat); const sun = new Date(Date.UTC(hpDate(sat).y, m - 1, d + 1)); return HP_MONTHS[m - 1] + " " + d + "–" + (sun.getUTCMonth() === m - 1 ? sun.getUTCDate() : HP_MONTHS[sun.getUTCMonth()] + " " + sun.getUTCDate()); }
function hpDaysBetween(a, b) { const A = hpDate(a), B = hpDate(b); return Math.round((Date.UTC(B.y, B.m - 1, B.d) - Date.UTC(A.y, A.m - 1, A.d)) / 864e5); }
function hpH(n) { return (Math.round(n * 10) / 10) + " h"; }
function hpPerson(id) { const p = hp.view && hp.view.plan && hp.view.plan.people && hp.view.plan.people[id]; return p ? p.name : id; }
function hpEstimate(it) {
  if (it.includedIn) return "in " + hpTitle(it.includedIn);
  if (it.draws === "outside") return "outside crew";
  if (it.hours != null) {
    const e = it.estimate || {};
    return hpH(it.hours) + (e.basis ? " · " + e.basis : "") + (e.confidence ? " " + e.confidence : "");
  }
  if (it.low != null || it.high != null) return "unknown · " + (it.low ?? "?") + "–" + (it.high ?? "?") + " h allowance";
  return it.draws === "none" ? "" : "unknown";
}
function hpTitle(ref) {
  if (!ref) return "";
  if (ref.startsWith("milestone:")) { const m = hp.view.plan.milestones[ref.slice(10)]; return m ? m.title : ref; }
  const it = (hp.view.derived.items || []).find((x) => x.ref === ref);
  return it ? it.title : ref;
}
const HP_STATUS = { shared: "together", partial: "partly together", solo: "one away", away: "both away", after: "" };

// ---------------- the view: lanes on one date axis ----------------
// One row per lane — away weekends, events, holds, work — over a single axis
// from the horizon start to the deadline, and one sentence on what is left.
// Everything unresolved lives in the "To resolve" feed, not here.
let hpHost = null, hpFeedHost = null;
// The list repaints often; each view is one node re-mounted each time, so an
// open field survives, and the plan refetches at most every 30 s.
function hpMount(host, cls, which) {
  let node = which === "feed" ? hpFeedHost : hpHost;
  if (!node) { node = el("div", cls); if (which === "feed") hpFeedHost = node; else hpHost = node; }
  host.append(node);
  if (!hp.data && !hp.error) { if (!node.childElementCount) node.append(el("p", "hp-empty", "Loading the Home plan…")); if (!hp.loading) homePlanLoad(); return; }
  if (!node.childElementCount) hpRepaint();
  if (!hp.loading && Date.now() - (hp.loadedAt || 0) > 30000) homePlanLoad();
}
function homePlanRender(host) { hpMount(host, "hp", "chart"); }
function homePlanFeedRender(host) { hpMount(host, "hp hp-feed-view", "feed"); }
function hpRepaint() {
  if (hpHost && hpHost.isConnected) hpPaint();
  if (hpFeedHost && hpFeedHost.isConnected) hpFeedPaint();
  if (hpEditorRepaint) hpEditorRepaint();
}
function hpAddDays(s, n) { const { y, m, d } = hpDate(s); const t = new Date(Date.UTC(y, m - 1, d + n)); return t.toISOString().slice(0, 10); }
function hpItemOf(plan, it) { const t = plan.tasks[it.task]; return !t ? null : it.sub ? (t.subtasks || {})[it.sub] : t; }
function hpCap(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : s; }

function hpUnavailable(host) {
  if (hp.error) { host.append(el("p", "hp-empty", "Couldn't load the Home plan — " + hp.error), pillLight("Retry", homePlanLoad)); return true; }
  if (!hp.view || !hp.view.plan) { host.append(el("p", "hp-empty", "No shared Home plan yet. It is created through the plan API (docs/home-plan.md); this view then shows it.")); return true; }
  return false;
}

// hpLanes turns the plan into rows of dated blocks. Work lanes show saved
// placements solid and the estimates' suggested sequence as lighter blocks.
function hpLanes(v) {
  const p = v.plan, d = v.derived, lanes = [];
  const day = (s) => s;
  // away weekends
  const away = Object.values(p.away || {}).sort((a, b) => a.from.localeCompare(b.from));
  if (away.length) {
    const noShared = d.weeks.filter((w) => !w.past && w.status !== "shared" && w.status !== "after").length;
    lanes.push({ kind: "away", title: "Travel", meta: noShared ? noShared + " weekend" + (noShared === 1 ? "" : "s") + " without shared work" : "",
      blocks: away.map((a) => ({ from: a.from, to: a.to, cls: "is-away", tip: a.who.map(hpPerson).join(" & ") + " away " + hpShort(a.from) + "–" + hpShort(a.to) + (a.note ? " · " + a.note : "") })) });
  }
  // events, one lane each
  Object.values(p.events || {}).sort((a, b) => a.date.localeCompare(b.date)).forEach((e) => lanes.push({ kind: "event", title: e.title.replace(/,\s*\d.*$/, ""), meta: hpShort(e.date) + " · −" + hpH(e.hours) + " of work time", first: e.date,
    blocks: [{ from: e.date, to: e.date, cls: "is-event", tip: e.title + (e.note ? "\n" + e.note : "") }] }));
  const rest = [];
  // holds, grouped by purpose ("Prep and coating" + its spill)
  const holds = {};
  Object.values(p.reservations || {}).forEach((r) => {
    const name = r.purpose.replace(/\s*\((spill|cont\.?)\)\s*$/i, "");
    const g = holds[name] = holds[name] || { kind: "hold", title: name, hours: 0, blocks: [], contingency: false, status: r.status };
    g.hours += r.hours; g.contingency = g.contingency || r.kind === "contingency";
    g.blocks.push({ from: r.weekend, to: hpAddDays(r.weekend, 1), cls: r.kind === "contingency" ? "is-buffer" : "is-hold", tip: r.purpose + " · " + hpH(r.hours) + " · " + r.status + (r.note ? "\n" + r.note : "") });
  });
  Object.values(holds).forEach((g) => { g.meta = hpH(g.hours) + " " + (g.contingency ? "protected" : "reserved"); g.first = g.blocks.map((b) => b.from).sort()[0]; rest.push(g); });
  // work, one lane per task with weekend work
  const seq = d.sequence ? d.sequence.placements : [];
  const byTask = {};
  d.items.forEach((it) => {
    if (it.done || it.draws !== "pool") return;
    const t = byTask[it.task] = byTask[it.task] || { kind: "work", task: it.task, items: [], blocks: [] };
    t.items.push(it);
    const item = hpItemOf(p, it) || {};
    Object.entries(item.allocations || {}).forEach(([sat, h]) => t.blocks.push({ from: sat, to: hpAddDays(sat, 1), cls: "is-work", tip: it.title + " · " + hpH(h) + " placed" }));
  });
  seq.forEach((pl) => { const task = pl.ref.split("#")[0]; if (byTask[task]) byTask[task].blocks.push({ from: pl.weekend, to: hpAddDays(pl.weekend, 1), cls: "is-suggested", tip: hpTitle(pl.ref) + " · ≈" + hpH(pl.hours) + " suggested by the estimates" }); });
  const unestimated = [];
  Object.values(byTask).forEach((t) => {
    const own = t.items.filter((it) => !it.includedIn);
    if (!own.length) return; // fully inside another task's estimate
    const known = own.filter((it) => it.hours != null).reduce((n, it) => n + it.hours, 0);
    const allow = own.filter((it) => it.hours == null && it.high != null);
    const missing = own.filter((it) => it.hours == null && it.high == null);
    if (!known && !allow.length) { unestimated.push(...missing); return; }
    const inside = d.items.filter((x) => x.includedIn && x.includedIn.split("#")[0] === t.task && !x.sub).map((x) => x.title);
    const title = hpCap((v.tasks[t.task] || {}).text || t.task) + (inside.length ? " + " + inside.join(", ") : "");
    const bits = [];
    if (known) bits.push(hpH(known) + " estimated");
    allow.forEach((it) => bits.push((it.low ?? "?") + "–" + it.high + " h allowance"));
    if (missing.length) bits.push(missing.length + " part" + (missing.length === 1 ? "" : "s") + " without hours");
    rest.push({ kind: "work", task: t.task, title, meta: bits.join(" · "), blocks: t.blocks, first: t.blocks.map((b) => b.from).sort()[0] || "9999" });
  });
  rest.sort((a, b) => (a.first || "9999").localeCompare(b.first || "9999"));
  lanes.push(...rest);
  if (unestimated.length) lanes.push({ kind: "unestimated", title: "Not yet estimated", meta: (unestimated.length <= 3 ? unestimated.map((it) => it.title).join(" · ") : unestimated.length + " items") + " — hours in To resolve", blocks: [], items: unestimated });
  return lanes;
}

function hpPaint() {
  const host = hpHost;
  host.replaceChildren();
  if (hpUnavailable(host)) return;
  const v = hp.view, p = v.plan, d = v.derived, c = d.capacity;
  if (hp.scenario || hpDraftCount()) host.append(hpModeBar());
  // what-if chips: the roof estimate drives everything else
  const sc = Object.entries(hp.data.plan.scenarios || {}).sort((a, b) => (a[1].order || 0) - (b[1].order || 0) || a[0].localeCompare(b[0]));
  if (sc.length) {
    const row = el("div", "hp-whatif");
    row.append(el("span", "hp-whatif-label", "What if"));
    const chip = (id, label) => { const b = el("button", "hp-whatif-chip" + (hp.scenario === id ? " on" : ""), label); b.setAttribute("aria-pressed", String(hp.scenario === id)); b.onclick = () => { hp.scenario = id; hpRefreshView(); }; row.append(b); };
    chip("", "Saved plan");
    sc.forEach(([id, s]) => chip(id, s.label));
    host.append(row);
  }
  // the chart
  const start = p.horizon.start, end = c.hardDeadline || p.horizon.end, span = Math.max(1, hpDaysBetween(start, end) + 1);
  const x = (s) => Math.min(100, Math.max(0, hpDaysBetween(start, s) / span * 100));
  const chart = el("div", "hp-chart");
  const axis = el("div", "hp-axis");
  const ticks = [start];
  for (let m = hpDate(start).m + 1, y = hpDate(start).y; ; m++) { if (m > 12) { m = 1; y++; } const s = y + "-" + String(m).padStart(2, "0") + "-01"; if (s >= end) break; ticks.push(s); }
  ticks.push(end);
  ticks.forEach((s, i) => { const t = el("span", "hp-tick", hpShort(s)); t.style.left = x(s) + "%"; if (i === ticks.length - 1) t.classList.add("is-end"); if (i === 0) t.classList.add("is-start"); axis.append(t); });
  chart.append(axis);
  hpLanes(v).forEach((lane) => {
    const row = el("div", "hp-lane is-" + lane.kind);
    const head = el("div", "hp-lane-head");
    const title = lane.task ? el("button", "hp-lane-title", lane.title) : el("span", "hp-lane-title", lane.title);
    if (lane.task) { title.onclick = () => homePlanOpen(lane.task); title.title = "Open the task"; }
    head.append(title, el("span", "hp-lane-meta", lane.meta || ""));
    const track = el("div", "hp-track");
    const merged = {};
    lane.blocks.forEach((b) => { const k = b.from + "|" + b.cls; if (merged[k]) merged[k].tip += "\n" + b.tip; else merged[k] = { ...b }; });
    Object.values(merged).forEach((b) => {
      const blk = el("span", "hp-blk " + b.cls);
      const l = x(b.from), r = x(hpAddDays(b.to, 1));
      blk.style.left = l + "%"; blk.style.width = Math.max(0.8, r - l) + "%";
      blk.title = b.tip || "";
      track.append(blk);
    });
    const today = x(d.asOf); if (today > 0 && today < 100) { const t = el("span", "hp-today"); t.style.left = today + "%"; track.append(t); }
    row.append(head, track);
    chart.append(row);
  });
  host.append(chart);
  // one sentence on what is left, then the fine print
  const unknown = c.unknownItems.length;
  const sent = el("p", "hp-sentence");
  sent.textContent = hpH(c.knownDemand) + " of estimated work " + (c.remainingHours < 0 ? "is " + hpH(-c.remainingHours) + " more than" : "leaves " + hpH(c.remainingHours) + " of") + " the " + hpH(c.poolHours) + " of open weekend time" +
    (unknown ? " — before " + unknown + " item" + (unknown === 1 ? "" : "s") + " without hours" + (c.unknownHigh ? " (allowances " + c.unknownLow + "–" + c.unknownHigh + " h)" : "") : "") + ".";
  if (c.remainingHours < 0) sent.classList.add("hp-neg");
  host.append(sent);
  const dl = Object.values(p.milestones || {}).filter((m) => m.date).sort((a, b) => a.date.localeCompare(b.date)).map((m) => (m.kind === "deadline" ? "Deadline " : m.confirmed ? "Target " : "Draft target ") + hpShort(m.date) + ": " + m.title.toLowerCase());
  const later = Object.values(p.milestones || {}).filter((m) => m.kind === "later").map((m) => m.title.toLowerCase());
  const fine = el("p", "hp-fine");
  fine.textContent = [
    "Blocks hold capacity; lighter blocks are a suggested order from the estimates, not a confirmed sequence.",
    "Weekends " + p.capacity.weekendDayHours + " h a day together; planning " + p.capacity.eveningsPerWeek + " × " + p.capacity.eveningHours + "-hour evenings weekly.",
    dl.join(". ") + ".",
    later.length ? "Later: " + later.join(", ") + "." : "",
  ].filter(Boolean).join(" ");
  host.append(fine);
  const n = hpFeedItems(v).length;
  if (n) { const a = el("button", "hp-feed-link", n + " to resolve — decisions, hours, lead times, prices →"); a.onclick = () => homePlanShowFeed(); host.append(a); }
}

// the feed is its own view; each shell decides how to switch to it
function homePlanShowFeed() {
  if (typeof todosMode !== "undefined") { todosMode = "homefeed"; try { localStorage.setItem(typeof olgaTaskArea !== "undefined" ? "olga.tasks.view" : "todosMode", "homefeed"); } catch (e) {} renderTodos(); }
}

function hpModeBar() {
  const bar = el("div", "hp-mode");
  const n = hpDraftCount();
  if (hp.scenario) {
    const s = hp.data.plan.scenarios[hp.scenario] || {};
    bar.classList.add("is-scenario");
    bar.append(el("span", "", "What if: “" + (s.label || hp.scenario) + "” — " + (s.basis === "assistant" ? "an assistant what-if" : "a saved what-if") + ", not the plan."), pillLight("Back to the saved plan", () => { hp.scenario = ""; hpRefreshView(); }));
  }
  if (n) {
    bar.classList.add("is-draft");
    bar.append(el("span", "", "Draft · " + n + " change" + (n === 1 ? "" : "s") + " not saved — kept in this browser" + (hp.draft.base !== hp.data.revision ? ". The plan was saved elsewhere since this draft began." : ".")));
    const acts = el("span", "hp-mode-acts");
    acts.append(pill("Save to plan", () => hpSaveDraft(false)), hpInlineAsk("Save as what-if", "Name", hpSaveScenario), pillLight("Discard", hpDiscardDraft));
    bar.append(acts);
    if (hp.saveError) {
      const err = el("div", "hp-save-error", hp.saveError);
      err.setAttribute("role", "alert");
      if (hp.stale.length) {
        const list = el("ul", "");
        hp.stale.forEach((path) => list.append(el("li", "", path.join(" › ") + ": saved " + JSON.stringify(hpGet(hp.data.plan, path) ?? null) + " · yours " + JSON.stringify(hpGet(hp.draft.patch, path) ?? null))));
        err.append(list, pill("Keep mine", () => hpSaveDraft(true)), pillLight("Discard mine", hpDiscardDraft));
      }
      bar.append(err);
    }
  }
  if (!hp.scenario && !n) bar.append(el("span", "hp-mode-saved", "Saved plan · schedule edits start a draft; nothing is committed until you save"));
  if (hp.problems.length) { const pr = el("div", "hp-problems"); pr.setAttribute("role", "alert"); pr.append(el("strong", "", "This draft can't be applied: ")); pr.append(hp.problems.join("; ")); bar.append(pr); }
  return bar;
}

// ---------------- the feed: what is still unresolved ----------------
// One card per open question — a decision, missing hours, an unknown lead
// time or price, a conflict — answered in place. A card's Save writes just
// that field against the revision it was read at; if someone changed the same
// field meanwhile it says so instead of overwriting.
let hpFeedFilter = "all";
const HP_FEED_KINDS = [["all", "All"], ["decision", "Decisions"], ["hours", "Hours"], ["wait", "Lead times"], ["price", "Prices"], ["conflict", "Conflicts"]];
function hpFeedItems(v) {
  if (!v || !v.plan || !v.derived) return [];
  const p = v.plan, d = v.derived, out = [];
  d.conflicts.forEach((c) => out.push({ kind: "conflict", key: "c:" + c.kind + ":" + c.ref, task: c.ref.split("#")[0].startsWith("home/") ? c.ref.split("#")[0] : "", title: c.message }));
  const decs = [];
  Object.entries(p.decisions || {}).forEach(([id, dec]) => decs.push({ path: ["decisions", id], task: "", dec }));
  Object.entries(p.tasks || {}).forEach(([tid, t]) => Object.entries(t.decisions || {}).forEach(([id, dec]) => decs.push({ path: ["tasks", tid, "decisions", id], task: tid, dec })));
  decs.filter((x) => x.dec.status === "open").forEach((x) => out.push({ kind: "decision", key: "d:" + x.path.join("/"), task: x.task, path: x.path, title: x.dec.question, dec: x.dec }));
  const hoursRefs = d.capacity.unknownItems.concat(d.capacity.eveningUnknown); // weekend work first
  hoursRefs.forEach((ref) => { const it = d.items.find((x) => x.ref === ref) || {}; out.push({ kind: "hours", key: "h:" + ref, task: it.task, ref, it, path: ["tasks", it.task].concat(it.sub ? ["subtasks", it.sub] : []).concat(["estimate", "hours"]), title: it.title }); });
  d.capacity.waitUnknown.forEach((ref) => { const it = d.items.find((x) => x.ref === ref) || {}; const item = hpItemOf(p, it) || {}; out.push({ kind: "wait", key: "w:" + ref, task: it.task, ref, it, wait: item.wait || {}, path: ["tasks", it.task].concat(it.sub ? ["subtasks", it.sub] : []).concat(["wait", "days"]), title: it.title }); });
  if (d.budget) d.budget.unknown.forEach((id) => { const l = (p.budget.lines || {})[id] || {}; out.push({ kind: "price", key: "p:" + id, path: ["budget", "lines", id, "amount"], title: l.label || id, line: l }); });
  const rank = { conflict: 0, decision: 1, hours: 2, wait: 3, price: 4 };
  return out.sort((a, b) => rank[a.kind] - rank[b.kind]);
}

async function hpQuickSave(card, patch, path, expect) {
  card.classList.add("is-saving");
  try {
    let cur = await hpFetch("GET", "/api/home/plan");
    if (JSON.stringify(hpGet(cur.plan, path) ?? null) !== JSON.stringify(expect ?? null)) {
      hp.data = cur; await hpRefreshView();
      showToast("Changed on another device — the card now shows the saved value");
      return;
    }
    hp.data = await hpFetch("POST", "/api/home/plan", { revision: cur.revision, patch });
    showToast("Saved");
    await hpRefreshView();
  } catch (e) {
    card.classList.remove("is-saving");
    const err = card.querySelector(".hp-card-error") || card.appendChild(el("p", "hp-card-error"));
    err.textContent = e.status === 409 ? "Someone saved at the same moment — try again." : (e.problems && e.problems.length ? e.problems.join("; ") : e.message);
  }
}
function hpPatchAt(path, value) { const root = {}; let node = root; path.slice(0, -1).forEach((k) => { node = node[k] = {}; }); node[path[path.length - 1]] = value; return root; }
function hpMergePatches(...ps) { return ps.reduce((a, b) => hpMerge(a, b), {}); }

function hpFeedPaint() {
  const host = hpFeedHost;
  const keepFocus = document.activeElement && host.contains(document.activeElement) ? document.activeElement.dataset.hpFocus : "";
  host.replaceChildren();
  if (hpUnavailable(host)) return;
  const v = hp.data, items = hpFeedItems(v);
  const head = el("div", "hp-feed-head");
  head.append(el("p", "hp-sentence", items.length ? items.length + " things to resolve in the Home plan." : "Nothing left to resolve — every decision, estimate, lead time and price is in."));
  const chips = el("div", "hp-whatif");
  HP_FEED_KINDS.forEach(([k, label]) => {
    const n = k === "all" ? items.length : items.filter((x) => x.kind === k).length;
    if (k !== "all" && !n) return;
    const b = el("button", "hp-whatif-chip" + (hpFeedFilter === k ? " on" : ""), label + " · " + n);
    b.setAttribute("aria-pressed", String(hpFeedFilter === k));
    b.onclick = () => { hpFeedFilter = k; hpFeedPaint(); };
    chips.append(b);
  });
  head.append(chips);
  host.append(head);
  const list = el("div", "hp-feed");
  let lastKind = "";
  items.filter((x) => hpFeedFilter === "all" || x.kind === hpFeedFilter).forEach((x) => {
    if (hpFeedFilter === "all" && x.kind !== lastKind) { lastKind = x.kind; const label = HP_FEED_KINDS.find(([k]) => k === x.kind)[1]; list.append(el("h2", "hp-feed-section", label + " · " + items.filter((y) => y.kind === x.kind).length)); }
    list.append(hpFeedCard(v, x));
  });
  host.append(list);
  if (keepFocus) { const n = host.querySelector(`[data-hp-focus="${CSS.escape(keepFocus)}"]`); if (n) n.focus(); }
}

function hpFeedCard(v, x) {
  const card = el("article", "hp-card is-" + x.kind);
  const label = { conflict: "Conflict", decision: "Decision", hours: "Hours", wait: "Lead time", price: "Price" }[x.kind];
  const where = x.task ? hpCap((v.tasks[x.task] || {}).text || x.task) : x.kind === "price" ? "Budget" : "Project";
  card.append(el("div", "hp-card-kicker", hpFeedFilter === "all" ? where : label + " · " + where)); // the section header already names the kind
  const field = (input, onSave, extra) => {
    const f = el("form", "hp-card-form");
    f.append(input);
    if (extra) f.append(extra);
    const ok = el("button", "pill", "Save"); ok.type = "submit"; f.append(ok);
    f.onsubmit = (e) => { e.preventDefault(); onSave(); };
    return f;
  };
  const num = (ph, focus, unit) => { const i = inputEl(ph); i.type = "number"; i.min = "0"; i.step = "0.5"; i.inputMode = "decimal"; i.dataset.hpFocus = focus; i.setAttribute("aria-label", ph); const w = el("label", "hp-num"); w.append(i); if (unit) w.append(el("span", "", unit)); return [w, i]; };
  if (x.kind === "conflict") {
    card.append(el("h3", "hp-card-title", x.title));
  } else if (x.kind === "decision") {
    card.append(el("h3", "hp-card-title", x.dec.question));
    if (x.dec.note) { const det = el("details", "hp-card-note"); det.append(el("summary", "", "Details"), el("p", "hp-note", x.dec.note)); card.append(det); }
    const decide = (answer) => hpQuickSave(card, hpPatchAt(x.path, { status: "decided", answer }), x.path.concat(["status"]), "open");
    if ((x.dec.options || []).length) {
      const opts = el("div", "hp-card-options");
      x.dec.options.forEach((o) => { const b = el("button", "hp-option", o); b.onclick = () => decide(o); opts.append(b); });
      card.append(opts);
    }
    const [w, i] = (() => { const i = inputEl((x.dec.options || []).length ? "Or write the answer" : "The answer"); i.dataset.hpFocus = "f:" + x.key; i.setAttribute("aria-label", "Answer: " + x.dec.question); return [i, i]; })();
    const defer = el("button", "pill light", "Later"); defer.type = "button"; defer.title = "Mark deferred — it leaves this feed";
    defer.onclick = () => hpQuickSave(card, hpPatchAt(x.path, { status: "deferred" }), x.path.concat(["status"]), "open");
    card.append(field(w, () => { if (i.value.trim()) decide(i.value.trim()); }, defer));
  } else if (x.kind === "hours") {
    card.append(el("h3", "hp-card-title", "How many hours for " + x.title.charAt(0).toLowerCase() + x.title.slice(1) + "?"));
    const it = x.it, hint = [it.draws === "evening" ? "planning-evening time" : "weekend time, both of you together"];
    if (it.low != null || it.high != null) hint.push("allowance " + (it.low ?? "?") + "–" + (it.high ?? "?") + " h");
    if (it.estimate && it.estimate.excludes) hint.push(it.estimate.excludes);
    card.append(el("p", "hp-card-hint", hint.join(" · ")));
    const [w, i] = num("Hours", "f:" + x.key, "h");
    card.append(field(w, () => { if (i.value === "") return; const base = x.path.slice(0, -1); hpQuickSave(card, hpMergePatches(hpPatchAt(x.path, Number(i.value)), hpPatchAt(base.concat(["basis"]), "user")), x.path, null); }));
  } else if (x.kind === "wait") {
    card.append(el("h3", "hp-card-title", "How long is the wait — " + (x.wait.label || "lead time") + "?"));
    card.append(el("p", "hp-card-hint", "Calendar days nobody works on it · for " + x.title.charAt(0).toLowerCase() + x.title.slice(1)));
    const [w, i] = num("Days", "f:" + x.key, "days");
    card.append(field(w, () => { if (i.value !== "") hpQuickSave(card, hpPatchAt(x.path, Number(i.value)), x.path, null); }));
  } else if (x.kind === "price") {
    card.append(el("h3", "hp-card-title", "What will " + x.title.charAt(0).toLowerCase() + x.title.slice(1) + " cost?"));
    card.append(el("p", "hp-card-hint", "Out of the shared enclosure budget" + (x.line.note ? " · " + x.line.note : "")));
    const [w, i] = num("Amount", "f:" + x.key, "$");
    w.prepend(w.lastChild);
    const status = el("select", "pp-in"); [["estimate", "estimate"], ["quote", "quote"], ["committed", "committed"]].forEach(([val, lab]) => { const o = el("option", "", lab); o.value = val; status.append(o); });
    status.setAttribute("aria-label", "Kind of price");
    card.append(field(w, () => { if (i.value !== "") hpQuickSave(card, hpMergePatches(hpPatchAt(x.path, Number(i.value)), hpPatchAt(x.path.slice(0, -1).concat(["status"]), status.value)), x.path, null); }, status));
  }
  if (x.task) { const open = el("button", "hp-card-open", "Open task"); open.onclick = () => homePlanOpen(x.task); card.append(open); }
  return card;
}

// ---------------- the plan editor (one task) ----------------
let hpEditorRepaint = null;
// homePlanOpen opens a task's existing details. Olga's details embed the plan
// editor; the main inspector shows a summary with an "Edit schedule" button.
function homePlanOpen(taskId) {
  if (typeof openTodoPanel === "function") openTodoPanel(taskId);
}
// homePlanEditorModal: the editor in the shared picker modal (main app)
function homePlanEditorModal(taskId) {
  els.pickerTitle.textContent = "Schedule · " + ((hp.data && hp.data.tasks[taskId] || {}).text || taskId);
  const body = el("div", "hp-modal");
  els.pickerBody.replaceChildren(body);
  els.pickerModal.hidden = false;
  homePlanEditorInto(body, taskId);
}

async function homePlanEditorInto(host, taskId) {
  if (!hp.data) await homePlanLoad();
  let painted = false;
  const paint = () => {
    // the first paint may land before the caller mounts host; later ones stop once it is gone
    if (painted && !host.isConnected) { if (hpEditorRepaint === paint) hpEditorRepaint = null; return; }
    painted = true;
    const focus = document.activeElement && host.contains(document.activeElement) ? document.activeElement.dataset.hpFocus : "";
    host.replaceChildren(hpEditor(taskId));
    if (focus) { const n = host.querySelector(`[data-hp-focus="${CSS.escape(focus)}"]`); if (n) n.focus(); }
  };
  hpEditorRepaint = paint;
  paint();
}

function hpEditor(taskId) {
  const sec = el("section", "hp-editor");
  if (!hp.data || !hp.data.plan) { sec.append(el("p", "hp-label-sub", hp.error || "No shared Home plan yet.")); return sec; }
  const v = hp.view || hp.data, p = v.plan, t = p.tasks[taskId];
  sec.append(el("h3", "hp-editor-title", "Schedule"));
  sec.append(hpModeBar());
  if (!t) {
    if (!(taskId in (hp.data.tasks || {}))) { sec.append(el("p", "hp-label-sub", "Only shared Home tasks can join the Home plan.")); return sec; }
    sec.append(el("p", "hp-label-sub", "This task is not in the Home plan yet."), pillLight("Add to the plan (draft)", () => hpEdit(["tasks", taskId], { phase: "planning" })));
    return sec;
  }
  sec.append(hpItemFields(["tasks", taskId], t, v, true));
  // subtasks
  const subs = el("div", "hp-subtasks");
  subs.append(el("span", "micro-label", "Subtasks"));
  Object.entries(t.subtasks || {}).sort((a, b) => (a[1].order || 0) - (b[1].order || 0) || a[0].localeCompare(b[0])).forEach(([sid, st]) => {
    const det = el("details", "hp-sub");
    const sum = el("summary", "");
    const ref = taskId + "#" + sid;
    const iv = (v.derived.items || []).find((x) => x.ref === ref) || {};
    sum.append(el("span", st.done ? "is-done" : "", st.title), el("span", "hp-label-sub" + (iv.unknown ? " is-unknown" : ""), hpEstimate(iv)));
    det.append(sum);
    const path = ["tasks", taskId, "subtasks", sid];
    det.open = !!(hp.openSubs && hp.openSubs[ref]);
    det.ontoggle = () => { hp.openSubs = hp.openSubs || {}; hp.openSubs[ref] = det.open; };
    const titleIn = hpText(path.concat(["title"]), st.title, "Subtask");
    const doneBox = el("label", "hp-check");
    const cb = el("input"); cb.type = "checkbox"; cb.checked = !!st.done; cb.dataset.hpFocus = hpPathKey(path.concat(["done"]));
    cb.onchange = () => hpEdit(path.concat(["done"]), cb.checked || null);
    doneBox.append(cb, el("span", "", "Done"));
    det.append(hpField("Title", titleIn), doneBox, hpItemFields(path, st, v, false), pillLight("Remove subtask (draft)", () => hpEdit(path, null)));
    subs.append(det);
  });
  subs.append(hpInlineAsk("＋ Subtask", "What part of this task?", (title) => {
    const id = title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "sub";
    hpEdit(["tasks", taskId, "subtasks", id], { title, phase: "execution", draws: "pool", estimate: { hours: null, basis: "unknown" } });
  }));
  sec.append(subs);
  // decisions
  const decs = el("div", "hp-decisions");
  decs.append(el("span", "micro-label", "Decisions"));
  Object.entries(t.decisions || {}).forEach(([did, dec]) => {
    const path = ["tasks", taskId, "decisions", did];
    const box = el("details", "hp-sub");
    box.open = dec.status === "open" && !!(hp.openSubs && hp.openSubs["d:" + did]);
    box.ontoggle = () => { hp.openSubs = hp.openSubs || {}; hp.openSubs["d:" + did] = box.open; };
    const s = el("summary", "");
    s.append(el("span", "", dec.question), el("span", "hp-label-sub", dec.status + (dec.answer ? " · " + dec.answer : "")));
    box.append(s);
    if ((dec.options || []).length) { const ul = el("ul", "hp-options"); dec.options.forEach((o) => ul.append(el("li", "", o))); box.append(ul); }
    if (dec.note) box.append(el("p", "hp-note", dec.note));
    box.append(hpField("Status", hpSelect(path.concat(["status"]), dec.status, ["open", "decided", "deferred"])), hpField("Answer", hpText(path.concat(["answer"]), dec.answer || "", "Decided answer")));
    decs.append(box);
  });
  decs.append(hpInlineAsk("＋ Decision", "What needs deciding?", (q) => {
    const id = q.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "decision";
    hpEdit(["tasks", taskId, "decisions", id], { question: q, status: "open" });
  }));
  sec.append(decs);
  // research links: rendered as links, never as markup
  const links = el("div", "hp-links");
  links.append(el("span", "micro-label", "Research links"));
  Object.entries(t.links || {}).forEach(([lid, l]) => {
    const a = el("a", "", l.label || l.href);
    if (/^https?:\/\//.test(l.href)) { a.href = l.href; a.target = "_blank"; a.rel = "noopener noreferrer"; }
    const x = el("button", "hp-x", "✕"); x.setAttribute("aria-label", "Remove link " + (l.label || l.href)); x.onclick = () => hpEdit(["tasks", taskId, "links", lid], null);
    const row = el("div", "hp-link"); row.append(a, x); links.append(row);
  });
  links.append(hpInlineAsk("＋ Link", "https://…", (href) => {
    if (!/^https?:\/\//.test(href.trim())) { showToast("Links must start with https://"); return; }
    hpEdit(["tasks", taskId, "links", "l" + Date.now().toString(36)], { label: href.trim().replace(/^https?:\/\//, "").slice(0, 60), href: href.trim() });
  }));
  sec.append(links);
  if (t.source) sec.append(el("p", "hp-note", "Source: " + t.source));
  return sec;
}

// hpInlineAsk swaps a button for a field in place — the picker modal may
// already hold the task's details, so nothing here opens a second dialog.
function hpInlineAsk(label, placeholder, onSubmit) {
  const wrap = el("span", "hp-ask");
  const b = pillLight(label, () => {
    const f = el("form", "hp-ask-form");
    const i = inputEl(placeholder); i.setAttribute("aria-label", placeholder);
    const ok = el("button", "pill", "Add"); ok.type = "submit";
    const no = el("button", "pill light", "Cancel"); no.type = "button"; no.onclick = () => f.replaceWith(b);
    f.onsubmit = (e) => { e.preventDefault(); const val = i.value.trim(); if (!val) return; f.replaceWith(b); onSubmit(val); };
    f.append(i, ok, no);
    b.replaceWith(f); i.focus();
  });
  wrap.append(b);
  return wrap;
}
function hpField(label, node) { const f = el("label", "hp-field"); f.append(el("span", "", label), node); return f; }
function hpText(path, value, placeholder, multiline) {
  const n = multiline ? el("textarea", "pp-in") : inputEl(placeholder || "");
  if (multiline) { n.rows = 2; n.placeholder = placeholder || ""; }
  n.value = value || ""; n.dataset.hpFocus = hpPathKey(path);
  n.onchange = () => hpEdit(path, n.value.trim() ? n.value.trim() : null);
  return n;
}
function hpNumber(path, value, placeholder) {
  const n = inputEl(placeholder || "unknown");
  n.type = "number"; n.min = "0"; n.step = "0.5"; n.inputMode = "decimal"; n.value = value == null ? "" : value; n.dataset.hpFocus = hpPathKey(path);
  n.onchange = () => hpEdit(path, n.value.trim() === "" ? null : Number(n.value));
  return n;
}
function hpSelect(path, value, options, labels) {
  const s = el("select", "pp-in");
  options.forEach((o, i) => { const opt = el("option", "", labels ? labels[i] : o || "—"); opt.value = o; s.append(opt); });
  s.value = value || ""; s.dataset.hpFocus = hpPathKey(path);
  s.onchange = () => hpEdit(path, s.value || null);
  return s;
}

function hpItemFields(path, it, v, isTask) {
  const box = el("div", "hp-fields");
  const e = it.estimate || {};
  const draws = Object.keys(v.plan.reservations || {}).map((r) => "reservation:" + r);
  box.append(
    hpField("Phase", hpSelect(path.concat(["phase"]), it.phase, ["planning", "procurement", "execution", "later"])),
    hpField("Time comes from", hpSelect(path.concat(["draws"]), it.draws || "", ["", "pool", "evening", "outside", "none"].concat(draws), ["default", "open weekends", "planning evenings", "outside crew", "no household time"].concat(draws.map((r) => "held: " + ((v.plan.reservations[r.slice(12)] || {}).purpose || r))))),
    hpField("Owner", hpText(path.concat(["owner"]), it.owner, "who leads")),
    hpField("Next action", hpText(path.concat(["nextAction"]), it.nextAction, "the very next step", true)),
  );
  const est = el("fieldset", "hp-est");
  est.append(el("legend", "", "Estimate · elapsed hours together (empty = unknown)"));
  est.append(
    hpField("Hours", hpNumber(path.concat(["estimate", "hours"]), e.hours)),
    hpField("Low", hpNumber(path.concat(["estimate", "low"]), e.low, "—")),
    hpField("High", hpNumber(path.concat(["estimate", "high"]), e.high, "—")),
    hpField("Crew", hpSelect(path.concat(["estimate", "crew"]), e.crew ? String(e.crew) : "", ["", "1", "2"], ["—", "1 person", "2 people"])),
    hpField("Basis", hpSelect(path.concat(["estimate", "basis"]), e.basis || "", ["", "user", "assistant", "supplier", "crew", "unknown"], ["—", "ours", "assistant", "supplier", "crew", "unknown"])),
  );
  est.querySelector('[data-hp-focus$="\\"crew\\"]"]').onchange = (ev) => hpEdit(path.concat(["estimate", "crew"]), ev.target.value ? Number(ev.target.value) : null);
  if (!e.basis) est.querySelectorAll("input").forEach((n) => { const f = n.onchange; n.onchange = () => { f(); if (!hpGet(hp.draft && hp.draft.patch, path.concat(["estimate", "basis"]))) hpEdit(path.concat(["estimate", "basis"]), "user"); }; });
  if (e.excludes) est.append(el("p", "hp-note", "Excludes: " + e.excludes));
  if (e.source) est.append(el("p", "hp-note", "Source: " + e.source));
  box.append(est);
  const w = it.wait || null;
  const wait = el("fieldset", "hp-est");
  wait.append(el("legend", "", "Waiting · supplier or crew time, not household work"));
  wait.append(hpField("Days", hpNumber(path.concat(["wait", "days"]), w && w.days)), hpField("For", hpText(path.concat(["wait", "label"]), w && w.label, "e.g. glass lead time")));
  box.append(wait);
  // dependencies: choose among the plan's tasks, subtasks and milestones
  const deps = el("div", "hp-deps");
  deps.append(el("span", "micro-label", "Waits on"));
  const cur = it.dependsOn || [];
  cur.forEach((ref) => { const chip = el("span", "hp-chip", hpTitle(ref)); const x = el("button", "hp-x", "✕"); x.setAttribute("aria-label", "Remove dependency " + hpTitle(ref)); x.onclick = () => hpEdit(path.concat(["dependsOn"]), cur.filter((r) => r !== ref)); chip.append(x); deps.append(chip); });
  const self = path[1] + (path[3] ? "#" + path[3] : "");
  const refs = (v.derived.items || []).map((x) => x.ref).concat(Object.keys(v.plan.milestones || {}).map((m) => "milestone:" + m)).filter((r) => r !== self && !cur.includes(r));
  const add = el("select", "pp-in");
  add.append(el("option", "", "＋ add…"));
  refs.forEach((r) => { const o = el("option", "", hpTitle(r)); o.value = r; add.append(o); });
  add.setAttribute("aria-label", "Add a dependency");
  add.onchange = () => { if (add.value && add.value !== "＋ add…") hpEdit(path.concat(["dependsOn"]), cur.concat([add.value])); };
  deps.append(add);
  box.append(deps);
  // placement: weekend hours, saved only with the draft
  const alloc = el("div", "hp-alloc");
  alloc.append(el("span", "micro-label", "On weekends"));
  const weeks = (v.derived.weeks || []).filter((w) => !w.past && w.status !== "after");
  Object.entries(it.allocations || {}).sort().forEach(([sat, h]) => {
    const wk = weeks.find((w) => w.saturday === sat);
    const row = el("div", "hp-alloc-row");
    row.append(el("span", "", hpWeekend(sat) + (wk && wk.status !== "shared" ? " · " + HP_STATUS[wk.status] : "")), hpNumber(path.concat(["allocations", sat]), h, "h"));
    const x = el("button", "hp-x", "✕"); x.setAttribute("aria-label", "Remove " + hpWeekend(sat)); x.onclick = () => hpEdit(path.concat(["allocations", sat]), null);
    row.append(x);
    alloc.append(row);
  });
  if (it.draws === "evening") alloc.append(el("p", "hp-note", "Evening work is planning and ordering — it is not placed on weekends."));
  else {
    const pick = el("select", "pp-in");
    pick.append(el("option", "", "＋ place on a weekend…"));
    weeks.filter((w) => !(it.allocations || {})[w.saturday]).forEach((w) => { const o = el("option", "", hpWeekend(w.saturday) + " · " + (w.status === "shared" ? hpH(Math.max(0, w.freeHours)) + " free" : HP_STATUS[w.status])); o.value = w.saturday; pick.append(o); });
    pick.setAttribute("aria-label", "Place on a weekend");
    pick.onchange = () => { if (/^\d{4}-/.test(pick.value)) hpEdit(path.concat(["allocations", pick.value]), Math.min(8, (it.estimate && it.estimate.hours) || 8)); };
    alloc.append(pick);
  }
  box.append(alloc);
  if (it.window) box.append(el("p", "hp-note", "Planned window " + (it.window.start ? hpShort(it.window.start) : "?") + "–" + (it.window.end ? hpShort(it.window.end) : "?") + " — planned, not proof of completion."));
  if (it.note) box.append(el("p", "hp-note", it.note));
  if (isTask && it.includedIn) box.append(el("p", "hp-note", "Its effort is counted inside " + hpTitle(it.includedIn) + "."));
  return box;
}

// ---------------- task notes, readable ----------------
// homePlanNotesView turns a long Markdown description into collapsible
// sections of plain text. No HTML is ever interpreted; bare URLs become links.
function homePlanNotesView(text) {
  const box = el("div", "hp-notes");
  const parts = [];
  let cur = { title: "", lines: [] };
  (text || "").split("\n").forEach((line) => {
    const m = /^#{1,4}\s+(.*)$/.exec(line);
    if (m) { if (cur.title || cur.lines.join("").trim()) parts.push(cur); cur = { title: m[1].trim(), lines: [] }; }
    else cur.lines.push(line);
  });
  if (cur.title || cur.lines.join("").trim()) parts.push(cur);
  parts.forEach((part) => {
    const det = el("details", "hp-note-sec");
    det.append(el("summary", "", part.title || "Notes"));
    const body = el("div", "hp-note-body");
    part.lines.join("\n").trim().split(/(https?:\/\/[^\s)<>"]+)/).forEach((chunk, j) => {
      if (j % 2) { const a = el("a", "", chunk); a.href = chunk; a.target = "_blank"; a.rel = "noopener noreferrer"; body.append(a); }
      else body.append(document.createTextNode(chunk));
    });
    det.append(body);
    box.append(det);
  });
  if (!parts.length) box.append(el("p", "hp-label-sub", "No notes yet."));
  return box;
}

// main inspector: a read-only summary; the editor opens in the picker modal
function homePlanPanelHook(anchor, taskId) {
  if (!hp.data) { homePlanLoad().then(() => { if (anchor.isConnected && hp.data && hp.data.plan && hp.data.plan.tasks[taskId]) anchor.prepend(hpSummary(taskId)); }); return; }
  if (hp.data.plan && hp.data.plan.tasks[taskId]) anchor.prepend(hpSummary(taskId));
}
function hpSummary(taskId) {
  const v = hp.view || hp.data;
  const box = el("div", "tdo-p-sec hp-summary-box");
  box.append(el("div", "tdo-p-sec-label", "schedule · home timeline"));
  const items = (v.derived.items || []).filter((x) => x.task === taskId);
  items.forEach((it) => box.append(el("div", "hp-label-sub" + (it.unknown ? " is-unknown" : ""), (it.sub ? "· " + it.title + " — " : "") + [hpEstimate(it), it.allocated ? hpH(it.allocated) + " placed" : ""].filter(Boolean).join(" · "))));
  const t = v.plan.tasks[taskId];
  if (t.nextAction) box.append(el("div", "hp-note", "Next: " + t.nextAction));
  box.append(pillLight("Edit schedule", () => homePlanEditorModal(taskId)));
  return box;
}
