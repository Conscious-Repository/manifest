// ================= HOME TIMELINE — the shared Home plan =================
// One canonical document (system/home/plan.json, docs/home-plan.md) read by
// both planners. The saved plan is the baseline. Every edit here lands in a
// DRAFT first — kept in this browser, shown as a draft, previewed by the
// server — and only "Save to plan" writes it, against the revision it was
// made on. Scenarios are read-only what-ifs; nothing here sets a real date
// or availability by itself. All text renders as text: task notes never
// execute as HTML.
const HP_DRAFT_KEY = "homePlan.draft.v1";
let hpShowSeq = (() => { try { return localStorage.getItem("homePlan.sequence") === "1"; } catch (e) { return false; } })();
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

// ---------------- the view ----------------
let hpHost = null;
// The list repaints often; the timeline is one node re-mounted each time, so
// an open cell edit survives, and it refetches at most every 30 s.
function homePlanRender(host) {
  if (!hpHost) hpHost = el("div", "hp");
  host.append(hpHost);
  if (!hp.data && !hp.error) { if (!hpHost.childElementCount) hpHost.append(el("p", "hp-empty", "Loading the Home plan…")); if (!hp.loading) homePlanLoad(); return; }
  if (!hpHost.childElementCount) hpPaint();
  if (!hp.loading && Date.now() - (hp.loadedAt || 0) > 30000) homePlanLoad();
}
function hpRepaint() { if (hpHost && hpHost.isConnected) hpPaint(); if (hpEditorRepaint) hpEditorRepaint(); }

function hpPaint() {
  const host = hpHost;
  const focus = document.activeElement && host.contains(document.activeElement) ? document.activeElement.dataset.hpFocus : "";
  host.replaceChildren();
  if (hp.error) { host.append(el("p", "hp-empty", "Couldn't load the Home plan — " + hp.error), pillLight("Retry", homePlanLoad)); return; }
  const v = hp.view;
  if (!v || !v.plan) {
    host.append(el("p", "hp-empty", "No shared Home plan yet. It is created through the plan API (docs/home-plan.md); this view then shows it."));
    return;
  }
  const d = v.derived, p = v.plan, c = d.capacity;
  host.append(hpModeBar());
  // summary: the hard date, then capacity as it really is
  const sum = el("div", "hp-summary");
  const dl = Object.values(p.milestones || {}).find((m) => m.kind === "deadline");
  const card = (label, value, sub, cls) => { const n = el("div", "hp-stat" + (cls ? " " + cls : "")); n.append(el("span", "micro-label", label), el("strong", "", value)); if (sub) n.append(el("span", "hp-stat-sub", sub)); sum.append(n); return n; };
  if (dl) card("Hard deadline", hpShort(dl.date), dl.title + " · " + hpDaysBetween(d.asOf, dl.date) + " days", "hp-stat-deadline");
  card("Shared weekends", String(c.sharedWeekends), hpH(c.sharedHours) + " together · " + p.capacity.weekendDayHours + " h a day");
  card("Held", hpH(c.reservedHours), "reservations, incl. " + hpH(c.contingencyHours) + " contingency");
  card("Open for work", hpH(c.poolHours), "after reservations");
  card("Known work", hpH(c.knownDemand), c.unknownItems.length + " items still unknown");
  const left = card("Left for other work", (c.remainingHours < 0 ? "−" : "") + hpH(Math.abs(c.remainingHours)), c.unknownItems.length ? "before " + c.unknownItems.length + " unknowns" + (c.unknownHigh ? " (allowances " + c.unknownLow + "–" + c.unknownHigh + " h)" : "") : "", c.remainingHours < 0 ? "hp-neg" : "");
  left.title = "Open weekend hours minus known estimates. Unknown estimates are not counted as zero — they are listed below.";
  if (d.budget && d.budget.total != null) card("Budget", "$" + Math.round(d.budget.remaining).toLocaleString() + " left", "of $" + d.budget.total.toLocaleString() + " · " + d.budget.unknown.length + " prices unknown");
  card("Planning evenings", hpH(c.eveningHours), p.capacity.eveningsPerWeek + "×" + p.capacity.eveningHours + " h a week · not physical work");
  host.append(sum);
  // scenario outcomes side by side: what each assumption leaves
  const sc = Object.entries(d.scenarios || hp.data.derived.scenarios || {});
  if (sc.length) {
    const box = el("div", "hp-scenarios");
    box.append(el("span", "micro-label", "Scenarios — what-ifs, never the saved plan"));
    const row = el("div", "hp-scenario-row");
    sc.sort((a, b) => ((p.scenarios[a[0]] || {}).order || 0) - ((p.scenarios[b[0]] || {}).order || 0) || a[0].localeCompare(b[0])).forEach(([id, s]) => {
      const b = el("button", "hp-scenario" + (hp.scenario === id ? " on" : ""));
      b.setAttribute("aria-pressed", String(hp.scenario === id));
      const r = s.capacity.remainingHours;
      b.append(el("span", "", s.label), el("strong", r < 0 ? "hp-neg" : "", s.error ? "invalid" : (r < 0 ? "−" : "") + hpH(Math.abs(r)) + " left"), el("span", "hp-stat-sub", s.basis === "assistant" ? "assistant" : "yours"));
      b.onclick = () => { hp.scenario = hp.scenario === id ? "" : id; hpRefreshView(); };
      row.append(b);
    });
    box.append(row);
    host.append(box);
  }
  host.append(hpSequenceBar(d));
  if (hp.problems.length) {
    const pr = el("div", "hp-problems");
    pr.setAttribute("role", "alert");
    pr.append(el("strong", "", "This draft can't be applied:"));
    hp.problems.forEach((t) => pr.append(el("div", "", t)));
    host.append(pr);
  }
  host.append(window.mf && window.mf.phone && window.mf.phone() || matchMedia("(max-width: 860px)").matches ? hpAgenda(v) : hpGrid(v));
  host.append(hpConflicts(d), hpOpen(v));
  if (focus) { const n = host.querySelector(`[data-hp-focus="${CSS.escape(focus)}"]`); if (n) n.focus(); }
}

// the assistant sequence: a derived what-if the server recomputes on every
// read; toggling it shows ghost bars and never edits the plan
function hpSeqAt(d) {
  const at = {};
  if (!hpShowSeq || !d.sequence) return at;
  d.sequence.placements.forEach((pl) => { (at[pl.ref] = at[pl.ref] || {})[pl.weekend] = pl; });
  return at;
}
function hpSequenceBar(d) {
  const box = el("div", "hp-seq");
  const s = d.sequence;
  const toggle = el("button", "hp-scenario hp-seq-toggle" + (hpShowSeq ? " on" : ""));
  toggle.setAttribute("aria-pressed", String(hpShowSeq));
  toggle.append(el("span", "", "Assistant sequence"), el("strong", s && s.fits ? "" : "hp-neg", !s ? "—" : s.fits ? "fits · done " + hpWeekend(s.finish || d.asOf) : s.unplaced.length + " can't be placed"), el("span", "hp-stat-sub", "a what-if from the estimates · not saved"));
  toggle.onclick = () => { hpShowSeq = !hpShowSeq; try { localStorage.setItem("homePlan.sequence", hpShowSeq ? "1" : "0"); } catch (e) {} hpRepaint(); };
  box.append(toggle);
  if (hpShowSeq && s) {
    const det = el("div", "hp-seq-detail");
    det.append(el("p", "hp-label-sub", "Weekend work in dependency order, around holds and saved placements, skipping weekends too cold for an item's materials (normal high under its minimum + " + 5 + "°F). Allowances use their high end. Dotted bars below; nothing is saved."));
    s.unplaced.forEach((u) => det.append(el("div", "hp-conflict", hpTitle(u.ref) + " — " + u.reason)));
    s.assumptions.forEach((a) => det.append(el("div", "hp-label-sub", "· " + a)));
    if (s.spareHours) det.append(el("div", "hp-label-sub", "· " + hpH(s.spareHours) + " of shared weekend time left over"));
    box.append(det);
  }
  return box;
}

function hpModeBar() {
  const bar = el("div", "hp-mode");
  const n = hpDraftCount();
  if (hp.scenario) {
    const s = hp.data.plan.scenarios[hp.scenario] || {};
    bar.classList.add("is-scenario");
    bar.append(el("span", "", "Viewing scenario “" + (s.label || hp.scenario) + "” — " + (s.basis === "assistant" ? "an assistant what-if" : "a saved what-if") + ", not the plan."), pillLight("Back to the saved plan", () => { hp.scenario = ""; hpRefreshView(); }));
  }
  if (n) {
    bar.classList.add("is-draft");
    const msg = el("span", "", "Draft · " + n + " change" + (n === 1 ? "" : "s") + " not saved — kept in this browser" + (hp.draft.base !== hp.data.revision ? ". The plan was saved elsewhere since this draft began." : "."));
    bar.append(msg);
    const acts = el("span", "hp-mode-acts");
    acts.append(pill("Save to plan", () => hpSaveDraft(false)), hpInlineAsk("Save as scenario", "Scenario name", hpSaveScenario), pillLight("Discard", hpDiscardDraft));
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
  if (!hp.scenario && !n) bar.append(el("span", "hp-mode-saved", "Saved plan · edits start a draft; nothing is committed until you save"));
  return bar;
}

const HP_PHASES = [["planning", "Planning"], ["procurement", "Procurement"], ["execution", "Execution"], ["later", "Later scope"]];

// desktop: a weekly axis through the deadline
function hpGrid(v) {
  const d = v.derived, p = v.plan, weeks = d.weeks;
  const wrap = el("div", "hp-grid-wrap");
  const grid = el("div", "hp-grid");
  grid.style.setProperty("--hp-weeks", String(weeks.length));
  const cell = (cls, text) => el("div", "hp-cell " + (cls || ""), text);
  const rowHead = (title, sub, cls) => { const h = el("div", "hp-label " + (cls || "")); h.append(el("span", "hp-label-title", title)); if (sub) h.append(el("span", "hp-label-sub", sub)); return h; };
  // header
  grid.append(rowHead("Week of", "", "hp-head"));
  weeks.forEach((w) => { const h = cell("hp-head" + (w.past ? " is-past" : ""), ""); h.append(el("span", "hp-week-sat", hpWeekend(w.saturday)), el("span", "hp-week-mon", "wk " + hpShort(w.start))); grid.append(h); });
  // availability
  grid.append(rowHead("Weekend", "elapsed, together"));
  weeks.forEach((w) => {
    const t = w.status === "shared" ? hpH(w.sharedHours) : w.status === "after" ? "" : w.status === "solo" ? (w.present.map(hpPerson).join(", ") + " only") : HP_STATUS[w.status];
    const n = cell("hp-avail is-" + w.status + (w.past ? " is-past" : ""), t);
    (w.events || []).forEach((id) => { const e = (p.events || {})[id]; if (e) n.append(el("span", "hp-event", "−" + hpH(e.hours) + " " + e.title)); });
    n.title = (w.away || []).map((a) => { const x = p.away[a]; return x ? (x.who.map(hpPerson).join(" & ") + " away " + hpShort(x.from) + "–" + hpShort(x.to) + (x.note ? " · " + x.note : "")) : a; }).join("\n") || (w.status === "shared" ? "both home" : "");
    grid.append(n);
  });
  if (weeks.some((w) => w.normalHigh != null)) {
    grid.append(rowHead("Typical weather", "normal high / low °F"));
    weeks.forEach((w) => grid.append(cell("hp-normal" + (w.past ? " is-past" : ""), w.normalHigh == null ? "" : Math.round(w.normalHigh) + "° / " + Math.round(w.normalLow) + "°")));
  }
  grid.append(rowHead("Evenings", "planning & ordering"));
  weeks.forEach((w) => grid.append(cell("hp-evening" + (w.past ? " is-past" : ""), w.status === "after" ? "" : hpH(w.eveningHours))));
  grid.append(rowHead("Held", "provisional reservations"));
  weeks.forEach((w) => {
    const n = cell("hp-res" + (w.past ? " is-past" : ""));
    (w.reserved || []).forEach((id) => { const r = p.reservations[id]; const b = el("span", "hp-res-block is-" + r.kind, r.purpose + " · " + hpH(r.hours)); b.title = r.purpose + " — " + r.status + (r.note ? "\n" + r.note : ""); n.append(b); });
    grid.append(n);
  });
  grid.append(rowHead("Milestones", ""));
  weeks.forEach((w) => {
    const n = cell("hp-ms");
    (w.milestones || []).forEach((id) => { const m = p.milestones[id]; const b = el("span", "hp-ms-mark is-" + m.kind + (m.confirmed ? " is-confirmed" : ""), (m.kind === "deadline" ? "◆ " : "◇ ") + hpShort(m.date) + " " + m.title); b.title = m.title + " — " + (m.kind === "deadline" ? "hard deadline" : m.kind === "target" ? (m.confirmed ? "confirmed target" : "draft target, not confirmed") : m.kind) + (m.note ? "\n" + m.note : ""); n.append(b); });
    grid.append(n);
  });
  const deadlineSat = d.capacity.hardDeadline;
  const seqAt = hpSeqAt(d);
  for (const [phase, label] of HP_PHASES) {
    const items = d.items.filter((it) => it.phase === phase);
    if (!items.length) continue;
    const g = el("div", "hp-group", label);
    g.style.gridColumn = "1 / -1";
    grid.append(g);
    items.forEach((it) => {
      const lab = el("div", "hp-label hp-item" + (it.sub ? " is-sub" : "") + (it.done ? " is-done" : ""));
      const btn = el("button", "hp-item-title", it.title);
      btn.dataset.hpFocus = "t:" + it.ref;
      btn.onclick = () => homePlanOpen(it.task);
      lab.append(btn);
      const bits = [hpEstimate(it)];
      if (it.hours != null && it.draws === "pool" && !it.done) bits.push(it.allocated ? hpH(it.allocated) + " placed" : "not placed");
      if (it.dependsOn && it.dependsOn.length) bits.push("after " + it.dependsOn.map(hpTitle).join(", "));
      if (it.minTempF != null) bits.push("≥ " + it.minTempF + "°F");
      lab.append(el("span", "hp-label-sub" + (it.unknown ? " is-unknown" : ""), bits.filter(Boolean).join(" · ")));
      grid.append(lab);
      const item = hpItemOf(p, it);
      weeks.forEach((w) => {
        const h = item && item.allocations && item.allocations[w.saturday];
        const inWin = it.window && it.window.start && w.start <= (it.window.end || it.window.start) && hpAddDays(w.start, 6) >= it.window.start;
        const n = cell("hp-bar-cell" + (w.past ? " is-past" : "") + (deadlineSat && w.start <= deadlineSat && hpAddDays(w.start, 6) >= deadlineSat ? " is-deadline" : ""));
        const ghost = !h && seqAt[it.ref] && seqAt[it.ref][w.saturday];
        if (ghost) { const g = el("span", "hp-bar is-seq", "≈" + hpH(ghost.hours)); g.title = "Assistant sequence (" + (ghost.basis === "allowance-high" ? "allowance high end" : "estimate") + ") — not saved"; n.append(g); }
        if (it.minTempF != null && w.normalHigh != null && w.normalHigh < it.minTempF + 5 && w.status !== "after") { n.classList.add("is-cold"); n.title = "Normally too cold here for this work (needs " + it.minTempF + "°F)"; }
        if (h) { const b = el("span", "hp-bar" + (hp.draft && hpGet(hp.draft.patch, ["tasks", it.task].concat(it.sub ? ["subtasks", it.sub] : []).concat(["allocations", w.saturday])) !== undefined ? " is-draft" : ""), hpH(h)); n.append(b); }
        else if (inWin) n.append(el("span", "hp-window", it.draws === "outside" ? "crew" : "window"));
        const placeable = !it.done && it.draws !== "evening" && it.draws !== "none" && it.draws !== "outside" && !it.includedIn && w.status !== "after" && !w.past;
        if (placeable) {
          n.classList.add("is-placeable");
          n.tabIndex = 0;
          n.dataset.hpFocus = "c:" + it.ref + ":" + w.saturday;
          n.setAttribute("role", "button");
          n.setAttribute("aria-label", "Hours for " + it.title + " on " + hpWeekend(w.saturday));
          const open = () => hpPlaceInCell(n, it, w);
          n.onclick = open;
          n.onkeydown = (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); open(); } };
        }
        grid.append(n);
      });
    });
  }
  grid.append(rowHead("Free", "after held + placed"));
  weeks.forEach((w) => grid.append(cell("hp-free" + (w.freeHours < 0 ? " hp-neg" : "") + (w.past ? " is-past" : ""), w.status === "after" || (!w.sharedHours && !w.allocatedHours) ? "" : (w.freeHours < 0 ? "−" : "") + hpH(Math.abs(w.freeHours)))));
  wrap.append(grid);
  return wrap;
}
function hpAddDays(s, n) { const { y, m, d } = hpDate(s); const t = new Date(Date.UTC(y, m - 1, d + n)); return t.toISOString().slice(0, 10); }
function hpItemOf(plan, it) { const t = plan.tasks[it.task]; return !t ? null : it.sub ? (t.subtasks || {})[it.sub] : t; }
function hpItemPath(it) { return ["tasks", it.task].concat(it.sub ? ["subtasks", it.sub] : []); }

// a cell becomes a number field: hours on that weekend, into the draft
function hpPlaceInCell(n, it, w) {
  if (n.querySelector("input")) return;
  const item = hpItemOf(hp.view.plan, it) || {};
  const cur = (item.allocations || {})[w.saturday] || "";
  const input = el("input", "hp-cell-input");
  input.type = "number"; input.min = "0"; input.step = "1"; input.inputMode = "decimal"; input.value = cur;
  input.setAttribute("aria-label", "Hours for " + it.title + " on " + hpWeekend(w.saturday) + " (empty removes)");
  n.replaceChildren(input);
  input.focus(); input.select();
  let done = false;
  const commit = () => {
    if (done) return; done = true;
    const val = input.value.trim() === "" ? null : Number(input.value);
    if (val !== null && !(val > 0)) { hpEdit(hpItemPath(it).concat(["allocations", w.saturday]), null); return; }
    if (val === (cur || null)) { hpRepaint(); return; }
    hpEdit(hpItemPath(it).concat(["allocations", w.saturday]), val);
  };
  input.onkeydown = (e) => { if (e.key === "Enter") input.blur(); if (e.key === "Escape") { done = true; hpRepaint(); } };
  input.onblur = commit;
}

// phone: the same plan as a chronological agenda
function hpAgenda(v) {
  const d = v.derived, p = v.plan;
  const list = el("div", "hp-agenda");
  const itemsByWeek = {};
  d.items.forEach((it) => { const item = hpItemOf(p, it); Object.entries((item && item.allocations) || {}).forEach(([sat, h]) => (itemsByWeek[sat] = itemsByWeek[sat] || []).push([it, h])); });
  d.weeks.forEach((w) => {
    if (w.past) return;
    const sec = el("section", "hp-week is-" + w.status);
    const head = el("div", "hp-week-head");
    head.append(el("strong", "", w.status === "after" ? "Week of " + hpShort(w.start) : hpWeekend(w.saturday)));
    if (w.normalHigh != null) head.append(el("span", "hp-label-sub", "typically " + Math.round(w.normalHigh) + "° / " + Math.round(w.normalLow) + "°F"));
    head.append(el("span", "hp-label-sub", w.status === "shared" ? hpH(w.sharedHours) + " together · " + (w.freeHours < 0 ? "over by " + hpH(-w.freeHours) : hpH(w.freeHours) + " free") : w.status === "solo" ? w.present.map(hpPerson).join(", ") + " only — light solo work" : HP_STATUS[w.status] || ""));
    sec.append(head);
    (w.milestones || []).forEach((id) => { const m = p.milestones[id]; sec.append(el("div", "hp-ms-mark is-" + m.kind + (m.confirmed ? " is-confirmed" : ""), (m.kind === "deadline" ? "◆ " : "◇ ") + hpShort(m.date) + " · " + m.title + (m.kind === "target" && !m.confirmed ? " (draft target)" : ""))); });
    (w.events || []).forEach((id) => { const e = (p.events || {})[id]; if (e) sec.append(el("div", "hp-event", hpShort(e.date) + " · " + e.title + " (−" + hpH(e.hours) + ")")); });
    (w.reserved || []).forEach((id) => { const r = p.reservations[id]; sec.append(el("div", "hp-res-block is-" + r.kind, "Held · " + r.purpose + " · " + hpH(r.hours) + " (" + r.status + ")")); });
    (itemsByWeek[w.saturday] || []).forEach(([it, h]) => { const b = el("button", "hp-agenda-item", it.title + " · " + hpH(h)); b.onclick = () => homePlanOpen(it.task); sec.append(b); });
    if (hpShowSeq && d.sequence) d.sequence.placements.filter((pl) => pl.weekend === w.saturday).forEach((pl) => { const b = el("button", "hp-agenda-item is-seq", "≈ " + hpTitle(pl.ref) + " · " + hpH(pl.hours) + " (assistant sequence)"); b.onclick = () => homePlanOpen(pl.ref.split("#")[0]); sec.append(b); });
    if (w.status !== "after") sec.append(el("div", "hp-label-sub", "Evenings · " + hpH(w.eveningHours) + " planning & ordering"));
    list.append(sec);
  });
  for (const [phase, label] of HP_PHASES) {
    const items = d.items.filter((it) => it.phase === phase && !it.allocated);
    if (!items.length) continue;
    const sec = el("section", "hp-week hp-unplaced");
    sec.append(el("div", "hp-week-head", label + " · not on a weekend"));
    items.forEach((it) => {
      const b = el("button", "hp-agenda-item" + (it.sub ? " is-sub" : "") + (it.done ? " is-done" : ""));
      b.append(el("span", "", it.title), el("span", "hp-label-sub" + (it.unknown ? " is-unknown" : ""), [hpEstimate(it), it.dependsOn && it.dependsOn.length ? "after " + it.dependsOn.map(hpTitle).join(", ") : ""].filter(Boolean).join(" · ")));
      b.onclick = () => homePlanOpen(it.task);
      sec.append(b);
    });
    list.append(sec);
  }
  return list;
}

function hpConflicts(d) {
  const box = el("section", "hp-list");
  box.append(el("span", "micro-label", "Conflicts · " + d.conflicts.length));
  if (!d.conflicts.length) box.append(el("p", "hp-label-sub", "None — nothing placed past what the calendar holds."));
  d.conflicts.forEach((c) => box.append(el("div", "hp-conflict", c.message)));
  // long chip lists fold; the count stays visible
  const chips = (label, refs) => {
    if (!refs.length) return;
    const det = el("details", "hp-fold");
    det.open = !!(hp.openSubs && hp.openSubs["fold:" + label]);
    det.ontoggle = () => { hp.openSubs = hp.openSubs || {}; hp.openSubs["fold:" + label] = det.open; };
    det.append(el("summary", "micro-label", label + " · " + refs.length));
    const u = el("div", "hp-unknowns");
    refs.forEach((ref) => { const b = el("button", "hp-chip", hpTitle(ref)); b.onclick = () => homePlanOpen(ref.split("#")[0]); u.append(b); });
    det.append(u);
    box.append(det);
  };
  chips("Unknown effort", d.capacity.unknownItems.concat(d.capacity.eveningUnknown));
  chips("Lead times unknown", d.capacity.waitUnknown);
  return box;
}

// open decisions and later scope, close to the plan
function hpOpen(v) {
  const p = v.plan;
  const box = el("section", "hp-list");
  const open = [];
  Object.entries(p.decisions || {}).forEach(([id, dec]) => open.push([null, id, dec]));
  Object.entries(p.tasks || {}).forEach(([tid, t]) => Object.entries(t.decisions || {}).forEach(([id, dec]) => open.push([tid, id, dec])));
  const live = open.filter(([, , dec]) => dec.status !== "decided");
  box.append(el("span", "micro-label", "Open decisions · " + live.length));
  live.forEach(([tid, did, dec]) => {
    const row = el("details", "hp-decision");
    const key = "dec:" + (tid || "") + ":" + did;
    row.open = !!(hp.openSubs && hp.openSubs[key]);
    row.ontoggle = () => { hp.openSubs = hp.openSubs || {}; hp.openSubs[key] = row.open; };
    const sum = el("summary", "");
    sum.append(el("span", "hp-later-title", dec.question), el("span", "hp-label-sub", [dec.status, tid ? v.tasks[tid] && v.tasks[tid].text : "project", (dec.options || []).length ? (dec.options || []).length + " options" : ""].filter(Boolean).join(" · ")));
    row.append(sum);
    if ((dec.options || []).length) { const ul = el("ul", "hp-options"); dec.options.forEach((o) => ul.append(el("li", "", o))); row.append(ul); }
    if (dec.note) row.append(el("p", "hp-note", dec.note));
    if (dec.answer) row.append(el("p", "hp-note", "Answer: " + dec.answer));
    if (tid) row.append(pillLight("Open task", () => homePlanOpen(tid)));
    box.append(row);
  });
  const later = Object.values(p.milestones || {}).filter((m) => m.kind === "later");
  if (later.length) {
    box.append(el("span", "micro-label", "Later, after weathertight"));
    later.forEach((m) => { const row = el("div", "hp-decision"); row.append(el("span", "hp-later-title", m.title)); if (m.note) row.append(el("span", "hp-label-sub", m.note)); box.append(row); });
  }
  return box;
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
