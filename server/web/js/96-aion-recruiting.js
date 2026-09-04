// ---- AION / RECRUITING ----
// The private sourcing-to-hire cockpit over system/aion/recruiting/. It
// fetches routes outside the AionLive contract that portal.aion.bio renders,
// and it is deliberately never mounted on the portal listener: these records
// carry candidate PII.
//
// Shape (redesign 2026-09-04, context/design_handoff_manifest_redesign 10/
// RECRUITING.md): three URL-addressable views in the rail — Board · Sources ·
// Network — plus a per-role search console (criteria editor, saved searches,
// coverage). The board forks on ORIGIN (inbound applicants vs sourced) and
// the gate is resolved in exactly one table (recGateTable) that the chip, the
// inspector summary and the primary action all read.
let recCache = null;      // {roles, candidates, seeds, network, stages, …}
let recLoadError = "";    // a fetch failure is a STATE, never a silent empty board
let recView = "board";    // board | sources | network | role
let recRoleView = "";     // role slug when recView === "role"
let recRole = null;       // board's role lane filter, null = all lanes
let recOrigin = "inbound"; // inbound | sourced | both — the default cut is the day-to-day
let recCut = "open";      // open | archived | all
let recSel = null;        // inspector selection (candidate id)
let recQuery = "";        // board search
let recSeedsOpen = false; // seeds rail expanded
let recNetQuery = "";     // network view search
let recNetTab = "paths";  // paths | people | edges
let recInspOpen = { details: false, evidence: false, network: false, activity: false, ashby: false };

// sources / scout runs — a run is a cache of a search, never a record
let recSources = null;      // {sources, defaultMax, maxMax, ttlDays} | {unavailable: true}
let recRuns = [];           // every run, newest first, each with its draft queue
let recRunOpen = {};        // run id → queue expanded
let recRunning = false;     // a run is in flight
const recRunForm = { source: "manual", role: "", query: "", max: "", dryRun: true, fields: {} };
const REC_RUN_COMMON_FIELDS = ["role", "query", "max"];

// ---- routing (#/aion/recruiting/{board|sources|network|role/<slug>}) ----

// recApplyRoute is called from showAion with everything after "recruiting".
function recApplyRoute(sub) {
  sub = (sub || "").replace(/^\//, "");
  if (sub.startsWith("role/")) { recView = "role"; recRoleView = sub.slice(5); }
  else if (sub === "sources" || sub === "network" || sub === "board") { recView = sub; }
  else recView = "board";
}

function recNav(path) {
  const want = "#/aion/recruiting" + (path ? "/" + path : "");
  if (location.hash === want) { recApplyRoute(path); renderAion(); }
  else location.hash = want; // showAion re-enters with the parsed view
}

async function loadRecruiting() {
  recLoadError = "";
  try {
    const r = await fetch("/api/aion/recruiting", { cache: "no-store" });
    if (!r.ok) throw new Error(await r.text());
    recCache = await r.json();
  } catch (e) {
    recCache = { roles: [], candidates: [], seeds: [], network: { people: [], edges: [] }, stages: [] };
    recLoadError = String(e.message || e).slice(0, 140);
  }
  // the sync footer and the ashby sections need the probe on a COLD load —
  // lazily loading it from the inspector hid "sync back" until a candidate
  // had been opened once
  await Promise.all([loadRecruitingSources(), recOutreachLoadProbe(), recAshbyLoadProbe(true)]);
}

// The sources panel is its own fetch: a board whose run cache is not wired
// (or fails to read) still paints, and the view says so quietly instead of
// taking the board down with it.
async function loadRecruitingSources() {
  try {
    const [s, r] = await Promise.all([
      fetch("/api/aion/recruiting/sources", { cache: "no-store" }),
      fetch("/api/aion/recruiting/sources/runs", { cache: "no-store" }),
    ]);
    if (!s.ok || !r.ok) throw new Error("sources unavailable");
    recSources = await s.json();
    recRuns = (await r.json()).runs || [];
  } catch (_) {
    recSources = { unavailable: true };
    recRuns = [];
  }
}

// recSourcesPost: every sources route answers with the run list, and only a
// route that wrote a record (accept) adds the board view.
async function recSourcesPost(url, body, okMsg) {
  try {
    const r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) throw new Error(await r.text());
    const out = await r.json();
    if (out.runs) recRuns = out.runs;
    if (out.view) recCache = out.view;
    if (okMsg) showToast(okMsg);
    renderAion();
    return out;
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); return null; }
}

async function recPost(url, body, okMsg) {
  try {
    const r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) throw new Error(await r.text());
    recCache = await r.json();
    if (okMsg) showToast(okMsg);
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}

// ---- derivations (every count derives; never a literal) ----

// An applicant is INBOUND until it is triaged: the sync-back import stamps
// `inbound` and lands the record in the `ashby` column; the triage verdict
// (advance / archive) moves it, and from then on it is an ordinary candidate.
function recUntriaged(c) { return !!c.inbound && c.stage === "ashby"; }

function recRoleCandidates(c) {
  if (!recRole) return true;
  const role = (recCache.roles || []).find((r) => r.slug === recRole);
  const want = role ? role.id || "role/" + role.slug : recRole;
  return c.role === want;
}

function recVisible(c) {
  if (!recRoleCandidates(c)) return false;
  const un = recUntriaged(c);
  if (recOrigin === "inbound" && !un) return false;
  if (recOrigin === "sourced" && un) return false;
  const archived = c.stage === "archived";
  if (recCut === "open" && archived) return false;
  if (recCut === "archived" && !archived) return false;
  const q = recQuery.trim().toLowerCase();
  if (!q) return true;
  const p = c.profile || {};
  return [c.name, c.stage, p.title, p.org, p.location]
    .concat((c.evidence || []).map((e) => (e.snippet || "") + " " + (e.url || "")))
    .join(" ").toLowerCase().includes(q);
}

function recUntriagedCount() {
  return (recCache.candidates || []).filter(recUntriaged).length;
}
function recPendingDrafts() {
  return recRuns.reduce((n, run) => {
    if ((run.scope || {}).dryRun) return n;
    return n + (run.drafts || []).filter((d) => d.status === "new").length;
  }, 0);
}

// recHeaderMeta — RECRUITING owns its own header meta per view; the AION
// backlog's "LIVE · …" contract means nothing here.
function recHeaderMeta() {
  const cs = recCache.candidates || [];
  const net = recCache.network || {};
  switch (recView) {
    case "sources":
      return recRuns.length + " runs · " + recPendingDrafts() + " to review · no poller";
    case "network":
      return (net.people || []).length + " people · " + (net.edges || []).length + " edges";
    case "role": {
      const role = (recCache.roles || []).find((r) => r.slug === recRoleView) || {};
      const crit = role.criteria || [];
      return crit.length + " criteria · " + crit.filter((x) => x.class === "must").length + " musts";
    }
    default: {
      const open = cs.filter((c) => c.stage !== "archived").length;
      const tri = recUntriagedCount();
      return open + " candidates" + (tri ? " · " + tri + " to triage" : "");
    }
  }
}

// ---- entry ----

async function renderAionRecruiting(host) {
  host.innerHTML = "";
  if (!recCache) {
    host.append(emptyRow("loading…"));
    await loadRecruiting();
    if (aionMode === "recruiting") renderAion();
    return;
  }

  const wrap = el("div", "aion-backlog rec-shell");
  const rail = el("nav", "rec-rail");
  const main = el("div", "aion-list rec-main");
  const inspector = el("aside", "aion-inspector rec-inspector");
  wrap.append(rail, main, inspector);
  host.append(wrap);

  // ONE repaint path: view rows, role clicks, search, cuts and the origin
  // control all route through paint() — never a full-tab re-render for a
  // filter (problem 8), and the search caret survives between keystrokes.
  const paint = () => {
    paintRail(rail);
    paintMain(main);
    // the AION header's LIVE meta + dot describe the backlog engine —
    // RECRUITING overwrites them with its own derived meta (problem 13)
    if (els.aionMeta) els.aionMeta.textContent = recHeaderMeta();
    if (els.aionLiveRail) els.aionLiveRail.innerHTML = "";
    if (window.mf && window.mf.phone()) {
      if (recSel && recView === "board") {
        window.mfSheet.open((body) => paintInspector(body), {
          key: "recruiting",
          onClose: () => { if (recSel) { recSel = null; paint(); } },
          reopen: () => { if (!els.aionView.hidden && aionMode === "recruiting") renderAion(); },
        });
      } else {
        window.mfSheet.closeIf("recruiting");
      }
    } else {
      paintInspector(inspector);
    }
  };
  recPaint = paint;
  paint();
}

// module-level handle so deep handlers repaint in place
let recPaint = null;

// ---- the rail: VIEWS over ROLES over SEEDS, with the sync footer ----

function paintRail(rail) {
  rail.innerHTML = "";

  rail.append(el("div", "micro-label rec-rail-label", "VIEWS"));
  const views = [
    ["board", "Board", recUntriagedCount() || ""],
    ["sources", "Sources", recPendingDrafts() || ""],
    ["network", "Network", ""],
  ];
  views.forEach(([key, label, count]) => {
    const b = el("button", "rec-role" + (recView === key ? " on" : ""));
    b.append(el("span", "rec-role-name", label));
    if (count) b.append(el("span", "rec-role-count rec-count-live", String(count)));
    b.onclick = () => recNav(key);
    rail.append(b);
  });

  rail.append(el("div", "micro-label rec-rail-label", "ROLES"));
  const roles = recCache.roles || [];
  const all = el("button", "rec-role" + (recView === "board" && !recRole ? " on" : ""));
  all.append(el("span", "rec-role-name", "all roles"));
  all.append(el("span", "rec-role-count", String(roles.reduce((n, r) => n + (r.openCount || 0), 0))));
  all.onclick = () => {
    recRole = null;
    if (recView !== "board") recNav("board");
    else if (recPaint) recPaint();
  };
  rail.append(all);

  roles.forEach((role) => {
    const on = (recView === "board" && recRole === role.slug) || (recView === "role" && recRoleView === role.slug);
    const b = el("button", "rec-role" + (on ? " on" : ""));
    b.append(el("span", "rec-role-name", role.title || role.slug));
    b.append(el("span", "rec-role-count", String(role.openCount || 0)));
    b.onclick = () => {
      // second click on the already-selected lane opens the role console
      if (recView === "board" && recRole === role.slug) { recNav("role/" + role.slug); return; }
      recRole = role.slug;
      if (recView !== "board") recNav("board");
      else if (recPaint) recPaint();
    };
    rail.append(b);
  });
  if (!roles.length) rail.append(emptyRow("no roles yet"));
  if (recRole) {
    const edit = el("button", "rec-linkish", "edit criteria →");
    edit.onclick = () => recNav("role/" + recRole);
    rail.append(edit);
  }

  rail.append(paintSeeds());
  rail.append(paintSyncFooter(roles));
}

// The sync footer ALWAYS renders (problem 2 — "sync back" used to appear
// only after a candidate had been opened, because its probe loaded lazily).
function paintSyncFooter(roles) {
  const box = el("div", "rec-sync rec-rail-foot");
  const b = el("button", "pill light rec-sync-btn", recSyncing ? "syncing…" : "sync roles");
  b.disabled = recSyncing;
  b.title = "mirror the public Ashby job board onto the role records — never touches criteria";
  b.onclick = () => recSyncRoles();
  box.append(b);
  if (recAshbyProbe && recAshbyProbe.configured && !recAshbyProbe.error) {
    const sb = el("button", "pill light rec-sync-btn", "sync back");
    sb.title = "pull Ashby-owned state (applicants, official stages) onto records — a user action, never a poller";
    sb.onclick = () => recAshbySyncBack(false);
    box.append(sb);
    const full = el("button", "rec-linkish", "full re-sync");
    full.title = "ignore the incremental sync tokens and re-read everything";
    full.onclick = () => recAshbySyncBack(true);
    box.append(full);
  }
  const synced = roles.map((r) => r.synced || "").filter(Boolean).sort().pop();
  if (synced) box.append(el("span", "micro-label rec-sync-when", "ashby · " + synced));
  return box;
}

let recSyncing = false;

async function recSyncRoles() {
  if (recSyncing) return;
  recSyncing = true;
  renderAion();
  try {
    const r = await fetch("/api/aion/recruiting/roles/sync", { method: "POST" });
    if (!r.ok) throw new Error(await r.text());
    const body = await r.json();
    if (body.view) recCache = body.view;
    const s = body.sync || {};
    const parts = [(s.postings || 0) + " posting" + (s.postings === 1 ? "" : "s")];
    if ((s.updated || []).length) parts.push((s.updated || []).length + " updated");
    if ((s.created || []).length) parts.push((s.created || []).length + " new");
    if ((s.unlisted || []).length) parts.push((s.unlisted || []).length + " not on the board");
    showToast("ashby: " + parts.join(" · "));
  } catch (e) {
    showToast(String(e.message || e).slice(0, 140), null, "error");
  } finally {
    recSyncing = false;
    renderAion();
  }
}

function paintSeeds() {
  const seeds = recCache.seeds || [];
  const box = el("section", "rec-seeds");
  const head = el("button", "rec-seeds-head");
  head.append(el("span", "sec-caret", recSeedsOpen ? "▾" : "▸"));
  head.append(el("span", "micro-label", "SEEDS"));
  head.append(el("span", "rec-role-count", String(seeds.length)));
  head.onclick = () => { recSeedsOpen = !recSeedsOpen; if (recPaint) recPaint(); };
  box.append(head);
  if (!recSeedsOpen) return box;

  const list = el("div", "rec-seed-list");
  (recCache.seedClasses || []).forEach((cls) => {
    const inClass = seeds.filter((s) => s.class === cls);
    if (!inClass.length) return;
    list.append(el("div", "micro-label rec-seed-class", cls));
    inClass.forEach((s) => {
      const row = el("div", "rec-seed");
      if (s.url) {
        const a = linkEl(s.name, s.url);
        a.className = "rec-seed-name";
        row.append(a);
      } else {
        row.append(el("span", "rec-seed-name", s.name));
      }
      if (s.org) row.append(el("span", "rec-seed-sub", s.org));
      list.append(row);
    });
  });
  if (!seeds.length) list.append(emptyRow("no seeds yet"));
  box.append(list);

  box.append(ghostInput("＋ seed: person, company, lab, work, or repo", "aion-add rec-seed-add", (raw) => {
    const at = raw.indexOf(":");
    const cls = at > 0 ? raw.slice(0, at).trim().toLowerCase() : "";
    const name = at > 0 ? raw.slice(at + 1).trim() : raw.trim();
    if (!cls) { showToast("prefix the class, e.g. lab: WashU BME"); renderAion(); return; }
    return recPost("/api/aion/recruiting/seed", { class: cls, name }, "seed added");
  }));
  return box;
}

// ---- the main column, per view ----

function paintMain(main) {
  main.innerHTML = "";
  if (recView === "sources") { paintSourcesView(main); return; }
  if (recView === "network") { paintNetworkView(main); return; }
  if (recView === "role") { paintRoleView(main); return; }
  paintBoardView(main);
}

// ---- BOARD ----

function paintBoardView(main) {
  // row 1: search + the ORIGIN control. Two axes, two controls — origin is a
  // segmented control on its own line; stage cuts are the chips beneath.
  const bar = el("div", "rec-toolbar");
  const search = el("input", "pp-in rec-search");
  search.type = "search";
  search.placeholder = "search names, orgs, evidence…";
  search.value = recQuery;
  search.oninput = () => { recQuery = search.value; paintBoardBody(); };
  bar.append(search);
  const seg = el("div", "rec-seg");
  [["inbound", "INBOUND"], ["sourced", "SOURCED"], ["both", "BOTH"]].forEach(([key, label]) => {
    const b = el("button", "rec-seg-btn" + (recOrigin === key ? " on" : ""));
    b.append(document.createTextNode(label));
    if (key === "inbound") {
      const n = recUntriagedCount();
      if (n) b.append(el("span", "rec-seg-count", String(n)));
    }
    b.onclick = () => { recOrigin = key; if (recPaint) recPaint(); };
    seg.append(b);
  });
  bar.append(seg);
  main.append(bar);

  // row 2: the stage cuts. OPEN excludes archived — the old ALL/ACTIVE pair
  // was functionally identical (problem 1), so three cuts, not four.
  const cuts = el("div", "rec-cuts");
  [["open", "OPEN"], ["archived", "ARCHIVED"], ["all", "ALL"]].forEach(([key, label]) => {
    const b = el("button", "filter-chip" + (recCut === key ? " on" : ""), label);
    b.onclick = () => { recCut = key; if (recPaint) recPaint(); };
    cuts.append(b);
  });
  main.append(cuts);

  // paste a person, a lab, or a profile URL — the everyday sourcing gesture,
  // one field on the board instead of three clicks into the run form. The
  // action follows the content: a bare name adds one record; a URL offers to
  // expand into a web-crawl run.
  const paste = el("input", "rec-paste");
  paste.type = "text";
  paste.placeholder = "Paste a person, a lab, or a profile URL…   ↵ adds · a URL offers to expand";
  paste.onkeydown = (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    const v = paste.value.trim();
    if (!v) return;
    if (/^https?:\/\//i.test(v)) {
      recRunForm.source = "web";
      recRunForm.fields.seed_url = v;
      recRunForm.role = recRoleId();
      paste.value = "";
      showToast("seed loaded — scope the crawl and run it");
      recNav("sources");
      return;
    }
    paste.value = "";
    recPost("/api/aion/recruiting/candidate", { text: v, role: recRoleId() }, "candidate added");
  };
  main.append(paste);

  const board = el("div", "rec-board");
  board.id = "recBoard";
  main.append(board);
  const foot = el("div", "rec-foot");
  foot.id = "recBoardFoot";
  main.append(foot);
  paintBoardBody();
}

function paintBoardBody() {
  const board = document.getElementById("recBoard");
  const foot = document.getElementById("recBoardFoot");
  if (!board) return;
  board.innerHTML = "";
  const all = recCache.candidates || [];
  const rows = all.filter(recVisible);

  // empty vs broken are DIFFERENT states (problem 12): a fetch failure names
  // itself and offers a retry — never a silent empty board.
  if (recLoadError) {
    const err = el("div", "rec-error");
    err.append(el("span", "", "recruiting failed to load — " + recLoadError));
    const retry = el("button", "pill light", "retry");
    retry.onclick = async () => { recCache = null; renderAion(); };
    err.append(retry);
    board.append(err);
    if (foot) foot.textContent = "";
    return;
  }
  if (!rows.length) {
    board.append(emptyRow(all.length
      ? "No candidate matches — the pipeline itself is fine."
      : "no candidates yet — paste a link above, or run a source"));
  } else if (recOrigin === "inbound") {
    // the triage queue: every row is an untriaged applicant at stage `ashby`,
    // so stage lanes carry no signal — one queue, oldest application first
    const lane = el("section", "rec-lane");
    const head = el("div", "aion-sec-label");
    head.append(el("span", "aion-sec-title", "to triage"));
    head.append(el("span", "aion-sec-count", String(rows.length)));
    lane.append(head);
    rows.slice().sort((a, b) => (a.inbound || "").localeCompare(b.inbound || ""))
      .forEach((c) => lane.append(recCard(c)));
    board.append(lane);
  } else {
    const stages = (recCache.stages || []).filter((st) => rows.some((c) => c.stage === st));
    stages.forEach((stage) => {
      const lane = el("section", "rec-lane");
      const head = el("div", "aion-sec-label");
      head.append(el("span", "aion-sec-title", stage));
      head.append(el("span", "aion-sec-count", String(rows.filter((c) => c.stage === stage).length)));
      lane.append(head);
      rows.filter((c) => c.stage === stage).forEach((c) => lane.append(recCard(c)));
      board.append(lane);
    });
  }
  if (foot) foot.textContent = rows.length + " of " + all.length + " · archive is reversible, there is no delete";
}

// recStateCell — "the state that wants you": ● to triage for an untriaged
// applicant, the last outreach touch, the replied stage, else a quiet —.
function recStateCell(c) {
  if (recUntriaged(c)) {
    const s = el("span", "rec-state alarm", "● to triage");
    if (c.inbound) s.title = "applied " + c.inbound;
    return s;
  }
  const sent = (c.outreach || []).filter((o) => o.status === "sent");
  const last = sent[sent.length - 1];
  if (c.stage === "replied") return el("span", "rec-state", "replied" + (last && last.last ? " " + last.last.slice(5) : ""));
  if (last) return el("span", "rec-state", "sent" + (last.last ? " " + last.last.slice(5) : ""));
  return el("span", "rec-state dim", "—");
}

// A board row is two facts and one state: name over title · org, the state
// that wants you, and the gate chip. Everything else moved into the
// inspector — extra columns that changed no decision while scanning.
function recCard(c) {
  const p = c.profile || {};
  const sub = [p.title, p.org].filter(Boolean).join(" · ");
  const gate = recGateTable(c);
  const card = cardShell({
    kind: "rec-card" + (recSel === c.id ? " sel" : ""),
    chips: [el("span", "micro-label rec-gate " + gate.chipCls, gate.chipLabel)],
    title: c.name,
    date: recStateCell(c),
    meta: sub || null,
  });
  const chip = card.querySelector(".rec-gate");
  if (chip && gate.reason) chip.title = gate.reason;
  card.onclick = () => { recSel = recSel === c.id ? null : c.id; if (recPaint) recPaint(); };
  return card;
}

// ---- THE gate table — resolved in exactly one place. The chip, the
// inspector summary and the primary action all read this. An override IS the
// unblock (offering "score fit" would ask for finished work again); a
// confirmed disqualifier is clearable only by a recorded override, never by
// scoring.
function recGateTable(c) {
  const g = c.gate || {};
  const out = { reason: g.reason || "" };
  if (c.stage === "archived") {
    out.key = "archived";
    out.chipLabel = "archived"; out.chipCls = "muted";
    out.summary = "archived — restore to work this record";
    out.primary = { label: "restore from archive", run: (cc) => recArchive(cc, false) };
    return out;
  }
  if (g.passed && g.overridden) {
    const ov = c.override || {};
    out.key = "overridden";
    out.chipLabel = "overridden"; out.chipCls = "blocked";
    out.summary = "● overridden" + (ov.by ? " by " + ov.by : "") + (ov.at ? " " + ov.at : "") + " — reason recorded";
    out.primary = { label: "prepare outreach", run: recPrimaryOutreach };
    return out;
  }
  if (g.passed) {
    out.key = "ok";
    out.chipLabel = "gate ok"; out.chipCls = "ok";
    out.summary = (g.satisfied || 0) + "/" + (g.musts || 0) + " musts cited — sends allowed";
    out.primary = { label: "prepare outreach", run: recPrimaryOutreach };
    return out;
  }
  if ((g.disqualifiers || []).length) {
    out.key = "disqualified";
    out.chipLabel = "disqualified"; out.chipCls = "blocked";
    out.summary = "● disqualifier confirmed — sends refused";
    out.primary = { label: "override with a reason…", run: recPrimaryOverride };
    return out;
  }
  if (g.musts && (g.satisfied || 0) > 0) {
    const missing = g.musts - (g.satisfied || 0);
    out.key = "partial";
    out.chipLabel = (g.satisfied || 0) + "/" + g.musts + " musts"; out.chipCls = "blocked";
    out.summary = "● " + (g.satisfied || 0) + "/" + g.musts + " musts — " +
      (missing === 1 ? "one citation missing" : missing + " citations missing");
    out.primary = { label: missing === 1 ? "cite the missing must" : "cite the missing musts", run: recPrimaryCite };
    return out;
  }
  out.key = "unscored";
  out.chipLabel = "unscored"; out.chipCls = "muted";
  out.summary = "unscored — score to unblock a send";
  out.primary = { label: "score fit", run: recPrimaryCite };
  return out;
}

function recPrimaryOutreach(c) {
  recInspOpen.activity = true;
  if (recPaint) recPaint();
  recOutreachPrepare(c);
}
function recPrimaryCite(c) {
  const first = document.querySelector(".rec-fit-row .rec-uncited, .rec-fit-row select");
  if (first) first.focus();
}
function recPrimaryOverride(c) {
  askText("override the fit gate", "why is this candidate through anyway?", (reason) => {
    if (!reason.trim()) { showToast("an override needs a reason"); return; }
    recPost("/api/aion/recruiting/candidate/override/" + c.id,
      { by: recCache.owner || "benjamin", reason: reason.trim() }, "override recorded");
  });
}
function recArchive(c, archived, extra) {
  recPost("/api/aion/recruiting/candidate/archive/" + c.id,
    Object.assign({ archived }, extra || {}), archived ? "candidate archived" : "candidate restored");
}

// ---- SOURCES view (its own body — no longer an accordion that pushed the
// whole board below the fold) ----

function paintSourcesView(main) {
  if (!recSources || recSources.unavailable) {
    main.append(emptyRow("sources unavailable"));
    return;
  }
  main.append(recRunFormEl());
  const list = el("div", "rec-run-list");
  recRuns.forEach((run) => list.append(recRunCard(run)));
  if (!recRuns.length) list.append(emptyRow("no runs yet — a run is a dry run until you say otherwise"));
  main.append(list);
}

function recRunFormEl() {
  const form = el("div", "rec-run-form");
  const adapters = recSources.sources || [];
  if (!adapters.find((a) => a.id === recRunForm.source) && adapters.length) recRunForm.source = adapters[0].id;
  const adapter = adapters.find((a) => a.id === recRunForm.source) || {};
  const queryField = (adapter.fields || []).find((f) => f.key === "query") || {};

  const source = el("select", "pp-in rec-in rec-run-source");
  adapters.forEach((a) => {
    const o = el("option", "", a.id + (a.kind && a.kind !== a.id ? " · " + a.kind : ""));
    o.value = a.id;
    o.selected = recRunForm.source === a.id;
    source.append(o);
  });
  source.disabled = adapters.length < 2;
  source.onchange = () => { recRunForm.source = source.value; renderAion(); };
  form.append(source);

  const role = el("select", "pp-in rec-in rec-run-role");
  const none = el("option", "", "any role"); none.value = ""; role.append(none);
  const roleVal = recRunForm.role || recRoleId();
  (recCache.roles || []).forEach((r) => {
    const id = r.id || "role/" + r.slug;
    const o = el("option", "", r.title || r.slug);
    o.value = id;
    o.selected = roleVal === id;
    role.append(o);
  });
  role.onchange = () => { recRunForm.role = role.value; };
  form.append(role);

  const query = el("input", "pp-in rec-in rec-run-q");
  query.type = "text";
  query.placeholder = queryField.label || queryField.placeholder || "query";
  query.value = recRunForm.query;
  query.oninput = () => { recRunForm.query = query.value; };
  query.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); recRunSource(); } };
  form.append(query);

  const max = el("input", "pp-in rec-in rec-run-max");
  max.type = "number";
  max.min = "1";
  max.max = String(recSources.maxMax || 100);
  max.placeholder = "max " + (recSources.defaultMax || 25);
  max.title = "at most " + (recSources.maxMax || 100) + " per run";
  max.value = recRunForm.max;
  max.oninput = () => { recRunForm.max = max.value; };
  form.append(max);

  (adapter.fields || []).filter((f) => f.key && !REC_RUN_COMMON_FIELDS.includes(f.key)).forEach((f) => {
    const input = el("input", "pp-in rec-in rec-run-extra rec-run-f-" + f.key.replace(/[^a-z0-9_-]/gi, ""));
    input.type = "text";
    input.placeholder = (f.label || f.key) + (f.placeholder ? " · " + f.placeholder : "") + (f.required ? " *" : "");
    input.title = (f.label || f.key) + (f.placeholder ? " — " + f.placeholder : "") + (f.required ? " (required)" : "");
    input.value = recRunForm.fields[f.key] || "";
    input.oninput = () => { recRunForm.fields[f.key] = input.value; };
    input.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); recRunSource(); } };
    form.append(input);
  });

  const dry = el("label", "rec-run-dry");
  const box = el("input", "");
  box.type = "checkbox";
  box.checked = !!recRunForm.dryRun;
  box.onchange = () => { recRunForm.dryRun = box.checked; };
  dry.append(box, el("span", "", "dry run"));
  dry.title = "preview the queue without anything to accept";
  form.append(dry);

  const run = el("button", "pill light rec-run-btn", recRunning ? "running…" : "run source");
  run.disabled = recRunning;
  run.onclick = () => recRunSource();
  form.append(run);
  return form;
}

async function recRunSource() {
  if (recRunning) return;
  const query = (recRunForm.query || "").trim();
  const adapter = ((recSources || {}).sources || []).find((a) => a.id === recRunForm.source) || {};
  const extras = (adapter.fields || []).filter((f) => f.key && !REC_RUN_COMMON_FIELDS.includes(f.key));
  const fields = {};
  for (const f of extras) {
    const v = (recRunForm.fields[f.key] || "").trim();
    if (v) fields[f.key] = v;
    else if (f.required) { showToast("a " + recRunForm.source + " run needs " + (f.label || f.key)); return; }
  }
  if (!query && !Object.keys(fields).length) { showToast("a run needs a query"); return; }
  const body = {
    source: recRunForm.source,
    role: recRunForm.role || recRoleId(),
    query,
    dryRun: !!recRunForm.dryRun,
  };
  const max = parseInt(recRunForm.max, 10);
  if (max > 0) body.max = max;
  if (Object.keys(fields).length) body.fields = fields;
  recRunning = true;
  renderAion();
  try {
    const r = await fetch("/api/aion/recruiting/sources/run", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!r.ok) throw new Error(await r.text());
    const out = await r.json();
    if (out.runs) recRuns = out.runs;
    if (out.run) recRunOpen[out.run.id] = true;
    recRunForm.query = "";
    const c = (out.run || {}).counts || {};
    showToast((body.dryRun ? "dry run: " : "run: ") + (c.fetched || 0) + " fetched · " +
      (c.new || 0) + " new · " + (c.duplicate || 0) + " duplicate");
  } catch (e) {
    showToast(String(e.message || e).slice(0, 140), null, "error");
  } finally {
    recRunning = false;
    renderAion();
  }
}

function recRunCard(run) {
  const scope = run.scope || {};
  const c = run.counts || {};
  const open = !!recRunOpen[run.id];
  const card = el("article", "rec-run" + (run.pinned ? " pinned" : ""));

  const head = el("div", "rec-run-head");
  const toggle = el("button", "rec-run-toggle");
  toggle.append(el("span", "sec-caret", open ? "▾" : "▸"));
  toggle.append(el("span", "micro-label", run.source));
  if (scope.dryRun) toggle.append(el("span", "micro-label rec-run-chip", "dry run"));
  if (run.pinned) toggle.append(el("span", "micro-label rec-run-chip pinned", "pinned"));
  toggle.append(el("span", "rec-run-scope", scope.query || ((scope.fields || {}).seed_url || "")));
  const role = (recCache.roles || []).find((r) => (r.id || "role/" + r.slug) === scope.role);
  if (role) toggle.append(el("span", "rec-draft-sub", role.title || role.slug));
  toggle.append(el("span", "rec-ev-when", fmtWhen(run.startedAt)));
  toggle.onclick = () => { recRunOpen[run.id] = !open; renderAion(); };
  head.append(toggle);
  const pin = el("button", "pill light rec-run-pin", run.pinned ? "unpin" : "pin");
  pin.title = run.pinned ? "let the sweep take this run after it expires" : "keep this run past its expiry";
  pin.onclick = () => recSourcesPost("/api/aion/recruiting/sources/pin/" + run.id,
    { pinned: !run.pinned }, run.pinned ? "run unpinned" : "run pinned");
  head.append(pin);
  card.append(head);

  const counts = el("div", "rec-run-counts");
  [["fetched", c.fetched], ["new", c.new], ["duplicate", c.duplicate], ["accepted", c.accepted], ["rejected", c.rejected]]
    .forEach(([k, n]) => counts.append(el("span", "micro-label rec-run-count" + (n ? " has" : ""), (n || 0) + " " + k)));
  let expiry = "kept until triaged";
  if (run.expiresAt) expiry = (run.pinned ? "pinned past " : "expires ") + fmtWhen(run.expiresAt);
  counts.append(el("span", "rec-ev-when rec-run-expiry", expiry));
  card.append(counts);
  if (!open) return card;

  const queue = el("div", "rec-draft-list");
  (run.drafts || []).forEach((d) => queue.append(recDraftCard(run, d)));
  if (!(run.drafts || []).length) queue.append(emptyRow("the source returned nothing"));
  card.append(queue);
  return card;
}

function recDraftCard(run, d) {
  const dr = d.draft || {};
  const card = el("div", "rec-draft " + d.status);
  const top = el("div", "rec-draft-top");
  top.append(el("span", "micro-label rec-draft-status " + d.status, d.status));
  top.append(el("span", "rec-draft-name", dr.name || "(unnamed)"));
  const sub = [dr.title, dr.org, dr.location].filter(Boolean).join(" · ");
  if (sub) top.append(el("span", "rec-draft-sub", sub));
  card.append(top);

  if (d.candidateId) {
    const on = el("button", "rec-draft-on", "on the board as " + d.candidateId + (d.reason ? " · " + d.reason : ""));
    on.onclick = () => { recSel = d.candidateId; recNav("board"); };
    card.append(on);
  }
  if (dr.note && dr.note !== dr.name) card.append(el("div", "rec-draft-note", dr.note));
  (dr.links || []).forEach((u) => {
    const a = linkEl(u, u);
    a.className = "rec-ev-url";
    card.append(a);
  });
  (dr.evidence || []).forEach((e) => {
    const row = el("div", "rec-ev-row");
    const et = el("div", "rec-ev-top");
    if (e.kind) et.append(el("span", "micro-label rec-ev-kind", e.kind));
    if (e.trust) et.append(el("span", "micro-label rec-ev-kind", e.trust));
    if (e.urlOrFile) { const a = linkEl(e.urlOrFile, e.urlOrFile); a.className = "rec-ev-url"; et.append(a); }
    if (e.retrievedAt) et.append(el("span", "rec-ev-when", fmtWhen(e.retrievedAt)));
    row.append(et);
    if (e.snippet && e.snippet !== dr.note) row.append(el("blockquote", "rec-ev-quote", e.snippet));
    card.append(row);
  });

  if (d.status === "new") {
    if ((run.scope || {}).dryRun) {
      card.append(el("div", "rec-draft-hint", "dry run — run again without dry run to accept"));
      return card;
    }
    const acts = el("div", "rec-draft-actions");
    const accept = el("button", "pill light rec-draft-accept", "accept");
    accept.title = "add this one draft to the board, citation and all";
    accept.onclick = () => recSourcesPost(
      "/api/aion/recruiting/sources/accept/" + run.id + "/" + d.id, {}, "candidate added from " + run.source);
    const reject = el("button", "pill light rec-draft-reject", "reject");
    reject.title = "leave it in the cache; nothing reaches the board";
    reject.onclick = () => recSourcesPost(
      "/api/aion/recruiting/sources/reject/" + run.id + "/" + d.id, {}, "draft rejected");
    acts.append(accept, reject);
    card.append(acts);
  } else if (d.decidedAt) {
    card.append(el("div", "rec-draft-hint", d.status + " " + fmtWhen(d.decidedAt)));
  }
  return card;
}

// ---- NETWORK view — a real view, not a section buried in one inspector.
// The network is curated AND derived: MY PEOPLE is the handful the owner
// actually knows, each carrying how many targets they reach; derived paths
// route through them. Paths only show the route — no intro tracking.

function paintNetworkView(main) {
  const bar = el("div", "rec-toolbar");
  const search = el("input", "pp-in rec-search");
  search.type = "search";
  search.placeholder = "search people, paths, edges…";
  search.value = recNetQuery;
  search.oninput = () => { recNetQuery = search.value; body(); };
  bar.append(search);
  [["paths", "PATHS"], ["people", "MY PEOPLE"], ["edges", "ALL EDGES"]].forEach(([key, label]) => {
    const b = el("button", "filter-chip" + (recNetTab === key ? " on" : ""), label);
    b.onclick = () => { recNetTab = key; if (recPaint) recPaint(); };
    bar.append(b);
  });
  main.append(bar);

  const host = el("div", "rec-board");
  main.append(host);
  const body = () => {
    host.innerHTML = "";
    const q = recNetQuery.trim().toLowerCase();
    const net = recCache.network || {};
    if (recNetTab === "people") {
      const people = (net.people || []).filter((p) =>
        !q || [p.name, p.org, p.title, p.email].join(" ").toLowerCase().includes(q));
      people.forEach((p) => {
        const row = el("div", "rec-net-row");
        const main2 = el("div", "rec-net-main");
        main2.append(el("span", "rec-net-name", p.name));
        const sub = [p.title, p.org].filter(Boolean).join(" · ");
        if (sub) main2.append(el("span", "rec-draft-sub", sub));
        row.append(main2);
        const reach = recReachCount(p);
        row.append(el("span", "rec-role-count", reach ? "reaches " + reach : ""));
        if (p.email) row.append(el("span", "rec-ev-when", p.email));
        host.append(row);
      });
      if (!people.length) host.append(emptyRow("no curated people yet"));
      host.append(ghostInput("＋ someone I know", "aion-add", (raw) => {
        const name = raw.trim();
        if (!name) return;
        return recPost("/api/aion/recruiting/network/person", { name }, "added to your people");
      }, "name — email/org are edited in the record"));
      return;
    }
    if (recNetTab === "edges") {
      const edges = (net.edges || []).filter((e) =>
        !q || [e.from, e.to, e.kind, e.basis].join(" ").toLowerCase().includes(q));
      edges.forEach((e) => {
        const row = el("div", "rec-edge");
        row.append(el("span", "rec-edge-ends", e.from + " → " + e.to));
        row.append(el("span", "micro-label", e.kind));
        if (e.basis) row.append(el("span", "rec-edge-basis", e.basis));
        if (e.confidence) row.append(el("span", "rec-path-conf", e.confidence));
        if (e.inferred) row.append(el("span", "micro-label rec-inferred", "inferred"));
        host.append(row);
      });
      if (!edges.length) host.append(emptyRow("no edges yet"));
      return;
    }
    // PATHS — one row per intro route across every candidate
    let any = false;
    (recCache.candidates || []).forEach((c) => {
      (c.paths || []).forEach((p) => {
        if (q && !((c.name + " " + p.path).toLowerCase().includes(q))) return;
        any = true;
        const row = el("div", "rec-path" + (p.kind === REC_PATH_KIND_DERIVED ? " derived" : ""));
        const top = el("div", "rec-net-main");
        const target = el("button", "rec-net-target", c.name);
        target.onclick = () => { recSel = c.id; recNav("board"); };
        top.append(target);
        if (p.kind === REC_PATH_KIND_DERIVED) {
          const chip = el("span", "micro-label rec-derived", "derived");
          chip.title = "computed from the network graph — not owner-confirmed";
          top.append(chip);
        } else if (p.kind) top.append(el("span", "micro-label", p.kind));
        if (p.inferred) top.append(el("span", "micro-label rec-inferred", "inferred"));
        if (p.confidence) top.append(el("span", "rec-path-conf", p.confidence));
        row.append(top);
        row.append(el("span", "rec-path-hops", p.path));
        host.append(row);
      });
    });
    if (!any) host.append(emptyRow("no intro paths yet — add the people you know and let the graph derive routes"));
  };
  body();
}

// recReachCount — how many candidates this curated person's paths route
// through. Derived from the path hop chains; never stored.
function recReachCount(person) {
  const name = (person.name || "").toLowerCase();
  if (!name) return 0;
  return (recCache.candidates || []).filter((c) =>
    (c.paths || []).some((p) => (p.path || "").toLowerCase().includes(name))).length;
}

// ---- ROLE view — the search console. Criteria are the rubric the whole fit
// gate depends on (problem 5: they had no editing UI at all).

function paintRoleView(main) {
  const role = (recCache.roles || []).find((r) => r.slug === recRoleView);
  if (!role) { main.append(emptyRow("no such role")); return; }
  const roleId = role.id || "role/" + role.slug;

  const head = el("div", "rec-role-head");
  head.append(el("span", "rec-role-title", role.title || role.slug));
  const back = el("button", "rec-linkish", "← board");
  back.onclick = () => { recRole = role.slug; recNav("board"); };
  head.append(back);
  main.append(head);

  // facts strip — mono, inline
  const facts = el("div", "rec-facts");
  [["status", role.status], ["location", role.location], ["type", role.employment],
    ["handoff", role.handoffMode], ["ashby job", role.ashbyJobId]].forEach(([k, v]) => {
    if (!v) return;
    const cell = el("span", "rec-fact");
    cell.append(el("span", "micro-label", k), el("span", "rec-fact-v", v));
    facts.append(cell);
  });
  main.append(facts);

  // criteria editor — whole-list PUT on every change (the server route the
  // client never called)
  const crit = (role.criteria || []).map((x) => ({ criterion: x.criterion, class: x.class, weight: x.weight }));
  const derived = crit.filter((x) => x.class === "must").length + " musts · " +
    crit.filter((x) => x.class === "nice").length + " nice · " +
    crit.filter((x) => x.class === "disqualifier").length + " disqualifier" +
    (crit.filter((x) => x.class === "disqualifier").length === 1 ? "" : "s");
  const sec = el("section", "rec-insp-sec");
  const label = el("div", "aion-sec-label");
  label.append(el("span", "aion-sec-title", "criteria"), el("span", "aion-sec-count", derived));
  sec.append(label);

  const put = (list, okMsg) =>
    recPut("/api/aion/recruiting/roles/" + encodeURIComponent(role.slug) + "/criteria", { criteria: list }, okMsg);

  crit.forEach((x, i) => {
    const row = el("div", "rec-crit-row");
    const cls = el("select", "pp-in rec-crit-class");
    ["must", "nice", "disqualifier"].forEach((v) => {
      const o = el("option", "", v); o.value = v; o.selected = x.class === v; cls.append(o);
    });
    cls.onchange = () => { x.class = cls.value; put(crit, "criteria saved"); };
    const text = el("input", "pp-in rec-crit-text");
    text.type = "text";
    text.value = x.criterion;
    text.onblur = () => {
      if (text.value.trim() === x.criterion) return;
      x.criterion = text.value.trim();
      put(crit.filter((y) => y.criterion), "criteria saved");
    };
    text.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); text.blur(); } };
    const x2 = el("button", "aion-insp-x rec-crit-x", "✕");
    x2.onclick = () => put(crit.filter((_, j) => j !== i), "criterion removed");
    row.append(cls, text, x2);
    sec.append(row);
  });
  sec.append(ghostInput("add ↵", "aion-add", (v) => {
    if (!v.trim()) return;
    return put(crit.concat([{ criterion: v.trim(), class: "must" }]), "criterion added");
  }, "a must, by default — retype the class on the row"));
  sec.append(el("div", "rec-gate-rule",
    "every must scored ≥3 with at least one evidence citation, and no disqualifier confirmed · " +
    "it blocks sends, not stage moves · an override is recorded with its written reason."));
  main.append(sec);

  // saved searches — the role's queries, re-runnable, derived from the runs
  const searches = {};
  recRuns.filter((r) => (r.scope || {}).role === roleId).forEach((r) => {
    const key = r.source + "|" + ((r.scope || {}).query || ((r.scope || {}).fields || {}).seed_url || "");
    if (!searches[key] || (r.startedAt || "") > (searches[key].startedAt || "")) searches[key] = r;
  });
  const ss = el("section", "rec-insp-sec");
  const sl = el("div", "aion-sec-label");
  sl.append(el("span", "aion-sec-title", "saved searches"),
    el("span", "aion-sec-count", String(Object.keys(searches).length)));
  ss.append(sl);
  Object.values(searches).forEach((r) => {
    const scope = r.scope || {};
    const row = el("div", "rec-net-row");
    const m = el("div", "rec-net-main");
    m.append(el("span", "rec-net-name", scope.query || ((scope.fields || {}).seed_url || "(no query)")));
    m.append(el("span", "rec-draft-sub", r.source + " · last ran " + fmtWhen(r.startedAt)));
    row.append(m);
    const fresh = (r.counts || {}).new || 0;
    row.append(el("span", "rec-state" + (fresh ? " alarm" : " dim"), fresh ? fresh + " new" : "no change"));
    const again = el("button", "pill light", "run again");
    again.onclick = () => {
      recRunForm.source = r.source;
      recRunForm.role = roleId;
      recRunForm.query = scope.query || "";
      recRunForm.fields = Object.assign({}, scope.fields || {});
      recNav("sources");
    };
    row.append(again);
    ss.append(row);
  });
  if (!Object.keys(searches).length) ss.append(emptyRow("no searches yet — runs against this role land here, re-runnable"));
  main.append(ss);

  // coverage — where has this role been swept, and where hasn't it. The
  // "where haven't I looked" read that makes a specific search feel finite.
  const cov = el("section", "rec-insp-sec");
  const cl = el("div", "aion-sec-label");
  cl.append(el("span", "aion-sec-title", "coverage"));
  cov.append(cl);
  const swept = {};
  recRuns.filter((r) => (r.scope || {}).role === roleId || !(r.scope || {}).role).forEach((r) => {
    const t = ((r.scope || {}).fields || {}).seed_url || (r.scope || {}).query;
    if (t) swept[t.toLowerCase()] = r.startedAt || "";
  });
  (recCache.seeds || []).filter((s) => s.class === "lab" || s.class === "company").forEach((s) => {
    const row = el("div", "rec-net-row");
    const m = el("div", "rec-net-main");
    m.append(el("span", "rec-net-name", s.name));
    m.append(el("span", "rec-draft-sub", s.class + (s.org ? " · " + s.org : "")));
    row.append(m);
    const hit = Object.keys(swept).find((k) => k.includes((s.url || s.name).toLowerCase()) ||
      (s.url || "").toLowerCase().includes(k));
    row.append(hit
      ? el("span", "rec-state dim", "swept " + fmtWhen(swept[hit]))
      : el("span", "rec-state alarm", "● never swept"));
    cov.append(row);
  });
  if (!(recCache.seeds || []).some((s) => s.class === "lab" || s.class === "company")) {
    cov.append(emptyRow("seed the labs and companies this role should sweep — coverage reads from them"));
  }
  main.append(cov);
}

// recPut mirrors recPost for PUT routes (the criteria editor).
async function recPut(url, body, okMsg) {
  try {
    const r = await fetch(url, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) throw new Error(await r.text());
    recCache = await r.json();
    if (okMsg) showToast(okMsg);
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}

// recRoleId is the role a new candidate is filed under: the selected lane, or
// nothing when the board is showing every lane.
function recRoleId() {
  if (!recRole) return "";
  const role = (recCache.roles || []).find((r) => r.slug === recRole);
  return role ? role.id || "role/" + role.slug : "";
}

// ---- inspector ----
// Face: Profile and Fit — the working surface. Folded, each with a derived
// meta that goes ink when it wants attention: Details (links + the PII
// contact pair), Evidence, Network (summary + link), Activity (next +
// outreach + origin), Ashby.

function paintInspector(host) {
  host.innerHTML = "";
  // selection is scoped to the VISIBLE rows — never offer consequential
  // verdicts on a record the current cut has hidden
  const c = (recCache.candidates || []).find((x) => x.id === recSel && recVisible(x)) || null;
  if (!c) {
    host.append(el("div", "aion-insp-empty", "select a candidate — edits save as you go"));
    return;
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Candidate"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { recSel = null; if (recPaint) recPaint(); };
  head.append(x);
  host.append(head);

  const patch = (set) => recPost("/api/aion/recruiting/candidate/update/" + c.id, set);

  // head block — the name at reading size, then role and stage side by side
  const name = el("input", "rec-name-in");
  name.type = "text";
  name.value = c.name || "";
  name.onblur = () => { if (name.value.trim() && name.value !== c.name) patch({ name: name.value.trim() }); };
  name.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); name.blur(); } };
  host.append(name);

  const pair = el("div", "rec-insp-pair");
  const role = el("select", "pp-in rec-in");
  const none = el("option", "", "— role"); none.value = ""; role.append(none);
  (recCache.roles || []).forEach((r) => {
    const id = r.id || "role/" + r.slug;
    const o = el("option", "", r.title || r.slug);
    o.value = id;
    o.selected = c.role === id;
    role.append(o);
  });
  role.onchange = () => patch({ role: role.value });
  const stage = el("select", "pp-in rec-in");
  (recCache.stages || []).filter((st) => st !== "archived").forEach((st) => {
    const o = el("option", "", st); o.value = st; o.selected = c.stage === st; stage.append(o);
  });
  stage.disabled = c.stage === "archived";
  stage.onchange = () => recPost("/api/aion/recruiting/candidate/stage/" + c.id, { stage: stage.value }, "stage saved");
  pair.append(role, stage);
  host.append(pair);

  // PROFILE — on the face
  const p = c.profile || {};
  const field = (into, label, node) => {
    const f = el("div", "aion-insp-field rec-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    into.append(f);
    return node;
  };
  const text = (into, label, key, value) => {
    const n = el("input", "pp-in rec-in");
    n.type = "text";
    n.value = value || "";
    const old = n.value;
    n.onblur = () => { if (n.value !== old) patch({ [key]: n.value }); };
    n.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); n.blur(); } };
    return field(into, label, n);
  };
  const profile = el("div", "rec-profile");
  text(profile, "title", "title", p.title);
  text(profile, "org", "org", p.org);
  text(profile, "location", "location", p.location);
  host.append(profile);

  const untriaged = recUntriaged(c);
  const gate = recGateTable(c);

  // gate summary + ONE primary action — both read the same table the chip
  // does. While an applicant is untriaged the verdict row IS the action, so
  // the gate primary is withheld, not demoted beneath it.
  host.append(el("div", "rec-gate-summary " + gate.chipCls, gate.summary));
  if (untriaged) {
    host.append(recTriageBlock(c));
  } else if (gate.primary) {
    const primary = el("button", "rec-primary", gate.primary.label);
    primary.onclick = () => gate.primary.run(c);
    host.append(primary);
  }

  // FIT — the working surface, always expanded
  host.append(recFitSection(c));

  // folds
  host.append(recFold("details", recDetailsMeta(c), () => recDetailsBody(c, field, text)));
  host.append(recFold("evidence", (c.evidence || []).length
    ? String((c.evidence || []).length) : { text: "no evidence", alarm: true },
    () => recEvidenceBody(c)));
  host.append(recFold("network", (c.paths || []).length
    ? (c.paths || []).length + " paths" : { text: "no paths", alarm: false },
    () => recNetworkBody(c)));
  host.append(recFold("activity", recActivityMeta(c), () => recActivityBody(c)));
  host.append(recFold("ashby", recAshbyMeta(c), () => recAshbySection(c)));

  // archive — a quiet bordered button, arm-then-confirm; withheld while the
  // verdict block owns the decision, and while archived (restore is the
  // primary then)
  if (!untriaged && c.stage !== "archived") {
    const archive = el("button", "rec-quiet-btn rec-archive-btn", "archive…");
    archive.onclick = () => {
      const armed = el("button", "rec-quiet-btn rec-archive-btn armed", "confirm archive?");
      armed.onclick = () => recArchive(c, false === true ? false : true);
      archive.replaceWith(armed);
      armed.focus();
    };
    host.append(archive);
    host.append(el("div", "rec-foot rec-archive-note", "reversible — the record is retained; there is no delete"));
  }
}

// recFold — a disclosure row: micro-label, a derived meta string (ink when it
// wants attention), a caret. Collapsed by default.
function recFold(key, meta, build) {
  const open = !!recInspOpen[key];
  const sec = el("section", "rec-insp-sec rec-fold");
  const head = el("button", "rec-fold-head");
  head.append(el("span", "sec-caret", open ? "▾" : "▸"));
  head.append(el("span", "micro-label", key.toUpperCase()));
  const m = typeof meta === "object" && meta ? meta : { text: String(meta || "") };
  head.append(el("span", "rec-fold-meta" + (m.alarm ? " alarm" : ""), m.text));
  head.onclick = () => { recInspOpen[key] = !open; if (recPaint) recPaint(); };
  sec.append(head);
  if (open) {
    const body = el("div", "rec-fold-body");
    const built = build();
    (Array.isArray(built) ? built : [built]).forEach((n) => n && body.append(n));
    sec.append(body);
  }
  return sec;
}

function recDetailsMeta(c) {
  const p = c.profile || {};
  const links = ["website", "linkedin", "github"].filter((k) => p[k]).length;
  const contact = ["email", "phone"].filter((k) => p[k]).length;
  const bits = [];
  if (links) bits.push(links + " link" + (links === 1 ? "" : "s"));
  if (contact) bits.push(contact === 2 ? "email · phone" : (p.email ? "email" : "phone"));
  return { text: bits.join(" · ") || "no links", alarm: false };
}

function recDetailsBody(c, field, text) {
  const p = c.profile || {};
  const links = el("div", "rec-profile");
  text(links, "website", "website", p.website);
  text(links, "linkedin", "linkedin", p.linkedin);
  text(links, "github", "github", p.github);
  // the PII pair on an ink dashed rule — the doctrine made visible
  const contact = el("div", "rec-contact");
  contact.append(el("div", "rec-contact-note", "manual entry only · never leaves the vault without the checkbox"));
  text(contact, "email", "email", p.email);
  text(contact, "phone", "phone", p.phone);
  return [links, contact];
}

function recActivityMeta(c) {
  const sent = (c.outreach || []).filter((o) => o.status === "sent");
  const last = sent[sent.length - 1];
  if (last) return { text: "sent " + (last.last || ""), alarm: false };
  if ((c.next || []).length) return { text: (c.next || []).length + " next", alarm: false };
  return { text: "no log yet", alarm: true };
}

function recActivityBody(c) {
  const out = [];
  // where they were found
  if (c.sourceRef || c.inbound) {
    const origin = el("div", "rec-next");
    origin.append(el("span", "micro-label", "origin"));
    origin.append(el("span", "", c.inbound ? "applied via Ashby · " + c.inbound : c.sourceRef));
    out.push(origin);
  }
  // more like this — seeds a run from a record you liked
  const seedUrl = (c.profile || {}).website || (c.profile || {}).linkedin || (c.profile || {}).github;
  if (seedUrl) {
    const more = el("button", "pill light", "more like this");
    more.title = "seed a web-crawl run from this record's own links";
    more.onclick = () => {
      recRunForm.source = "web";
      recRunForm.fields.seed_url = seedUrl;
      recRunForm.role = c.role || "";
      recNav("sources");
    };
    out.push(more);
  }
  (c.next || []).forEach((n) => {
    const row = el("div", "rec-next");
    row.append(el("span", "", n.action));
    if (n.due) row.append(el("span", "rec-ev-when", "due " + n.due));
    if (n.owner) row.append(el("span", "micro-label", n.owner));
    out.push(row);
  });
  out.push(recOutreachSection(c));
  return out;
}

// ---- triage — the verdict block an untriaged applicant carries above the
// primary action, on an ink top rule. Three verdicts; the fit gate applies to
// inbound too, but triage does not require scoring first.
let recReasons = null; // archiveReason.list, fetched once per tab load

function recTriageBlock(c) {
  const box = el("div", "rec-triage");
  box.append(el("div", "micro-label rec-triage-label", "TRIAGE — applied " + (c.inbound || "") +
    (c.ashbyStage ? " · ashby: " + c.ashbyStage : "")));

  const advance = el("button", "rec-primary", "advance");
  advance.title = "into the sourced pipeline at reviewing";
  advance.onclick = () => recPost("/api/aion/recruiting/candidate/stage/" + c.id,
    { stage: "reviewing" }, "advanced to reviewing");
  box.append(advance);

  const ask = el("button", "pill light", "ask for more");
  ask.title = "a screening question — drafted and logged like any outreach";
  ask.onclick = () => { recInspOpen.activity = true; if (recPaint) recPaint(); };
  box.append(ask);

  // archive & reject — the verdict with a consequence: it hides here AND
  // writes the rejection there, so it says so and stays arm-then-confirm.
  const linked = !!c.ashbyApplicationId && recAshbyProbe && recAshbyProbe.configured;
  if (linked) {
    const reasons = el("select", "pp-in rec-in rec-reject-reason");
    const none = el("option", "", "reject reason — choose"); none.value = ""; reasons.append(none);
    (recReasons || []).forEach((r) => {
      const o = el("option", "", r.text); o.value = r.id; reasons.append(o);
    });
    if (recReasons === null) recLoadReasons();
    box.append(reasons);
    const rej = el("button", "rec-quiet-btn", "archive & reject in ashby…");
    rej.onclick = () => {
      if (!reasons.value) { showToast("pick the reject reason first"); return; }
      const armed = el("button", "rec-quiet-btn armed", "confirm — reject in ashby?");
      armed.onclick = () => recArchive(c, true, { rejectInAshby: true, archiveReasonId: reasons.value });
      rej.replaceWith(armed);
      armed.focus();
    };
    box.append(rej);
    box.append(el("div", "rec-archive-note", "reversible here, and it writes the rejection there — restoring does not un-reject"));
  } else {
    const arch = el("button", "rec-quiet-btn", "archive…");
    arch.onclick = () => {
      const armed = el("button", "rec-quiet-btn armed", "confirm archive?");
      armed.onclick = () => recArchive(c, true);
      arch.replaceWith(armed);
      armed.focus();
    };
    box.append(arch);
  }
  return box;
}

async function recLoadReasons() {
  recReasons = [];
  try {
    const r = await fetch("/api/aion/recruiting/ashby/reasons", { cache: "no-store" });
    if (r.ok) recReasons = (await r.json()).reasons || [];
  } catch (_) {}
  if (recPaint) recPaint();
}

// ---- fit — the working surface. Evidence is a PICKER over the candidate's
// collected citations, never a free-text id field (problem 15); an uncited
// must wears the ink border, because that is the thing blocking the send.
function recFitSection(c) {
  const role = (recCache.roles || []).find((r) => (r.id || "role/" + r.slug) === c.role);
  const sec = el("section", "rec-insp-sec");
  const label = el("div", "aion-sec-label");
  label.append(el("span", "aion-sec-title", "fit"));
  sec.append(label);
  if (!role) { sec.append(emptyRow("tether a role to score fit")); return sec; }
  if (!(role.criteria || []).length) {
    const none = el("div", "rec-next");
    none.append(el("span", "", "this role has no criteria yet"));
    const go = el("button", "rec-linkish", "edit criteria →");
    go.onclick = () => recNav("role/" + role.slug);
    none.append(go);
    sec.append(none);
    return sec;
  }

  const byCriterion = {};
  (c.fit || []).forEach((f) => { byCriterion[(f.criterion || "").trim().toLowerCase()] = f; });
  const evidenceIds = (c.evidence || []).map((e) => e.id);

  (role.criteria || []).forEach((crit) => {
    const have = byCriterion[(crit.criterion || "").trim().toLowerCase()] || {};
    const row = el("div", "rec-fit-row");
    row.append(el("span", "micro-label rec-class " + crit.class, crit.class));
    row.append(el("span", "rec-fit-name", crit.criterion));
    if (crit.class === "disqualifier") {
      const box = el("input", "rec-fit-present");
      box.type = "checkbox";
      box.checked = !!have.present;
      box.title = "confirmed present";
      box.onchange = () => recPost("/api/aion/recruiting/candidate/fit/" + c.id,
        { criterion: crit.criterion, score: "unknown", present: box.checked }, "fit saved");
      row.append(box);
      sec.append(row);
      return;
    }
    const cited = (have.evidence || []).filter(Boolean);
    const commit = (score, evidence) => recPost("/api/aion/recruiting/candidate/fit/" + c.id, {
      criterion: crit.criterion, score, evidence,
    }, "fit saved");
    const score = el("select", "pp-in rec-score");
    ["unknown", "0", "1", "2", "3", "4", "5"].forEach((v) => {
      const o = el("option", "", v); o.value = v;
      o.selected = (have.score || "unknown") === v;
      score.append(o);
    });
    score.onchange = () => commit(score.value, cited);
    row.append(score);
    // the citations: current ids as removable chips + a picker to add one
    const cites = el("span", "rec-cites");
    cited.forEach((id) => {
      const chip = el("button", "rec-cite", id + " ✕");
      chip.title = "remove this citation";
      chip.onclick = () => commit(have.score || "unknown", cited.filter((x) => x !== id));
      cites.append(chip);
    });
    const uncitedMust = crit.class === "must" && !cited.length;
    const pick = el("select", "pp-in rec-cite-pick" + (uncitedMust ? " rec-uncited" : ""));
    const ph = el("option", "", cited.length ? "＋ cite" : (evidenceIds.length ? "cite evidence…" : "no evidence to cite"));
    ph.value = ""; pick.append(ph);
    evidenceIds.filter((id) => !cited.includes(id)).forEach((id) => {
      const e = (c.evidence || []).find((x) => x.id === id) || {};
      const o = el("option", "", id + (e.kind ? " · " + e.kind : ""));
      o.value = id; pick.append(o);
    });
    pick.disabled = !evidenceIds.filter((id) => !cited.includes(id)).length;
    if (uncitedMust) pick.title = "this is what blocks the send";
    pick.onchange = () => { if (pick.value) commit(have.score || "unknown", cited.concat([pick.value])); };
    cites.append(pick);
    row.append(cites);
    sec.append(row);
  });

  // the recorded override — never silent: it carries a reason and a date
  const ov = c.override || {};
  if (ov.by) {
    const line = el("div", "rec-override");
    line.append(el("span", "", "overridden by " + ov.by + (ov.at ? " · " + ov.at : "")));
    if (ov.reason) line.append(el("span", "rec-override-why", ov.reason));
    const clear = el("button", "pill light", "clear override");
    clear.onclick = () => recPost("/api/aion/recruiting/candidate/override/" + c.id, { by: "", reason: "" }, "override cleared");
    line.append(clear);
    sec.append(line);
  }
  return sec;
}

// evidence — the citations. A URL, a quote and a date, kept verbatim, because
// a citation is what outlives every cache and every adapter.
function recEvidenceBody(c) {
  const out = [];
  (c.evidence || []).forEach((e) => {
    const row = el("div", "rec-ev-row");
    const top = el("div", "rec-ev-top");
    top.append(el("span", "micro-label", e.id));
    if (e.kind) top.append(el("span", "micro-label rec-ev-kind", e.kind));
    if (e.url) { const a = linkEl(e.url, e.url); a.className = "rec-ev-url"; top.append(a); }
    if (e.collected) top.append(el("span", "rec-ev-when", e.collected));
    row.append(top);
    if (e.snippet) row.append(el("blockquote", "rec-ev-quote", e.snippet));
    out.push(row);
  });
  if (!(c.evidence || []).length) out.push(emptyRow("no evidence yet"));

  const form = el("div", "rec-ev-form");
  const url = inputEl("https://… (or leave blank for a note)");
  url.classList.add("rec-in");
  const kind = selectEl(["publication", "repo", "grant", "affiliation", "page",
    "conference", "ats_record", "contact_published", "owner_note"]);
  kind.classList.add("rec-in");
  const quote = el("textarea", "pp-in rec-in rec-quote");
  quote.placeholder = "verbatim quote — never paraphrased";
  const add = el("button", "pill light", "add evidence");
  add.onclick = () => {
    if (!url.value.trim() && !quote.value.trim()) { showToast("evidence needs a url or a quote"); return; }
    recPost("/api/aion/recruiting/candidate/evidence/" + c.id, {
      url: url.value.trim(), kind: kind.value, snippet: quote.value,
    }, "evidence captured");
  };
  form.append(url, kind, quote, add);
  out.push(form);
  return out;
}

// network fold — a two-line summary plus the link; the full graph lives in
// the Network view now, not in a per-candidate dump.
const REC_PATH_KIND_DERIVED = "derived"; // mirrors recruiting.PathKindDerived
function recNetworkBody(c) {
  const out = [];
  const paths = c.paths || [];
  if (paths.length) {
    const best = paths[0];
    const row = el("div", "rec-path" + (best.kind === REC_PATH_KIND_DERIVED ? " derived" : ""));
    const top = el("div", "rec-ev-top");
    if (best.kind === REC_PATH_KIND_DERIVED) {
      const chip = el("span", "micro-label rec-derived", "derived");
      chip.title = "computed from the network graph — not owner-confirmed";
      top.append(chip);
    }
    if (best.inferred) top.append(el("span", "micro-label rec-inferred", "inferred"));
    if (best.confidence) top.append(el("span", "rec-path-conf", best.confidence));
    row.append(top);
    row.append(el("span", "rec-path-hops", best.path));
    out.push(row);
    if (paths.length > 1) out.push(el("div", "rec-draft-sub", (paths.length - 1) + " more path" + (paths.length === 2 ? "" : "s")));
  } else {
    out.push(emptyRow("no sourced paths yet"));
  }
  const go = el("button", "rec-linkish", "open network view →");
  go.onclick = () => { recNetQuery = c.name || ""; recNav("network"); };
  out.push(go);
  return out;
}

// ---- approval-gated Gmail outreach ----
let recOutreachProbe = null;
let recOutreachLog = { id: null };
let recOutreachReady = {};
let recOutreachForm = {};

async function recOutreachLoadProbe() {
  try {
    const r = await fetch("/api/aion/recruiting/outreach/probe", { cache: "no-store" });
    if (!r.ok) throw new Error(await r.text());
    recOutreachProbe = await r.json();
  } catch (_) {
    recOutreachProbe = { unavailable: true, sendCapable: false, people: [] };
  }
}

async function recOutreachLoadLog(id) {
  recOutreachLog = { id, entries: [], loading: true };
  try {
    const r = await fetch("/api/aion/recruiting/outreach/" + id, { cache: "no-store" });
    if (!r.ok) throw new Error(await r.text());
    recOutreachLog = { id, entries: (await r.json()).entries || [] };
  } catch (e) {
    recOutreachLog = { id, entries: [], error: String(e.message || e) };
  }
  if (recSel === id && aionMode === "recruiting") renderAion();
}

async function recOutreachCall(url, body) {
  const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
  const text = await r.text();
  let out = {};
  try { out = JSON.parse(text); } catch (_) { out = { error: text }; }
  if (!r.ok && !out.readiness) throw new Error(out.error || text);
  return out;
}

function recOutreachCurrentDraft(entries) {
  const last = entries[entries.length - 1];
  return last && (last.status === "draft" || last.status === "ready") ? last : null;
}

async function recOutreachDraft(c, body) {
  try {
    const out = await recOutreachCall("/api/aion/recruiting/outreach/draft/" + c.id, body);
    if (out.view) recCache = out.view;
    if (out.entry && recOutreachLog.id === c.id) recOutreachLog.entries = (recOutreachLog.entries || []).concat([out.entry]);
    delete recOutreachReady[c.id];
    showToast(body.subject || body.body ? "draft captured" : "draft written");
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}

async function recOutreachPrepare(c) {
  try {
    const out = await recOutreachCall("/api/aion/recruiting/outreach/prepare/" + c.id, {});
    recOutreachReady[c.id] = out.readiness;
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}

function recOutreachSection(c) {
  const probe = recOutreachProbe || {};
  const capable = !!probe.sendCapable;
  const sec = recSection("outreach", capable ? "sender " + (probe.sender || "") : "sender not connected");
  const field = (label, node) => {
    const f = el("div", "aion-insp-field rec-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    sec.append(f);
    return node;
  };

  if (!capable) {
    sec.append(emptyRow(probe.unavailable ? "outreach unavailable"
      : (probe.detail || "sender not connected") + " — drafts still work; a send refuses"));
    if (!probe.unavailable) sec.append(recOutreachConnectEl());
  }

  if (recOutreachLog.id !== c.id) { recOutreachLoadLog(c.id); sec.append(emptyRow("loading…")); return sec; }
  if (recOutreachLog.loading) { sec.append(emptyRow("loading…")); return sec; }
  if (recOutreachLog.error) sec.append(emptyRow("log unavailable: " + recOutreachLog.error.slice(0, 120)));
  const entries = recOutreachLog.entries || [];
  entries.forEach((e) => {
    const row = el("div", "rec-next");
    row.append(el("span", "micro-label" + (e.status === "sent" ? " rec-gate ok" : ""), e.status));
    row.append(el("span", "micro-label", e.kind + (e.via ? " via " + e.via : "")));
    row.append(el("span", "", (e.subject || "(no subject)") + ((e.to || []).length ? " → " + e.to.join(", ") : "")));
    row.append(el("span", "rec-ev-when", e.sentAt ? fmtWhen(e.sentAt) : e.at || ""));
    if (e.messageId) row.append(el("span", "rec-ev-when", "message " + e.messageId + (e.threadId ? " · thread " + e.threadId : "")));
    sec.append(row);
  });
  if (!entries.length) sec.append(emptyRow("no outreach yet"));
  if (c.stage === "archived") return sec;

  const form = recOutreachForm[c.id] || (recOutreachForm[c.id] = { kind: "direct", via: "" });
  const kind = el("select", "pp-in rec-in");
  [["direct", "direct — to the candidate"], ["warm", "warm intro — via a mutual"], ["referral", "referral ask — via a mutual"]]
    .forEach(([v, label]) => { const o = el("option", "", label); o.value = v; o.selected = form.kind === v; kind.append(o); });
  kind.onchange = () => { form.kind = kind.value; renderAion(); };
  field("kind", kind);
  if (form.kind !== "direct") {
    const via = el("select", "pp-in rec-in");
    const none = el("option", "", "via — choose a mutual"); none.value = ""; via.append(none);
    (probe.people || []).forEach((p) => {
      const o = el("option", "", p.name + (p.email ? " · " + p.email : " · no address"));
      o.value = p.id; o.selected = form.via === p.id; via.append(o);
    });
    via.onchange = () => { form.via = via.value; };
    field("via", via);
  }
  const draftBtn = el("button", "pill light", "draft");
  draftBtn.title = "generate a draft on the server from the record's evidence — reaches no network";
  draftBtn.onclick = () => {
    const body = { kind: form.kind };
    if (form.kind !== "direct") {
      if (!form.via) { showToast("a " + form.kind + " outreach needs the mutual it goes through"); return; }
      body.via = form.via;
    }
    recOutreachDraft(c, body);
  };
  sec.append(draftBtn);

  const draft = recOutreachCurrentDraft(entries);
  if (draft) {
    const subject = el("input", "pp-in rec-in");
    subject.type = "text";
    subject.value = draft.subject || "";
    const body = el("textarea", "pp-in rec-in rec-quote");
    body.value = draft.body || "";
    const capture = () => {
      if (subject.value === (draft.subject || "") && body.value === (draft.body || "")) return;
      recOutreachDraft(c, { kind: draft.kind, via: draft.via || "", to: draft.to || [], subject: subject.value, body: body.value });
    };
    subject.onblur = capture;
    subject.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); subject.blur(); } };
    body.onblur = capture;
    field("to", el("span", "", (draft.to || []).join(", ") || "— no address on the record"));
    field("subject", subject);
    field("body", body);
  }

  const prepare = el("button", "pill light", "prepare");
  prepare.title = "the send preflight: sender, draft, recipients, evidence, fit gate — writes nothing";
  prepare.onclick = () => recOutreachPrepare(c);
  sec.append(prepare);
  const ready = recOutreachReady[c.id];
  if (ready) {
    if (ready.ready) sec.append(el("div", "rec-next", "ready to send" + (ready.sender ? " as " + ready.sender : "")));
    (ready.reasons || []).forEach((why) => {
      const row = el("div", "rec-next");
      row.append(el("span", "micro-label rec-gate blocked", "not ready"), el("span", "", why));
      sec.append(row);
    });
  }

  if (!capable || !draft) return sec;
  const send = el("button", "rec-quiet-btn rec-send-btn", "send as " + (probe.sender || "sender"));
  send.onclick = () => {
    const armed = el("button", "rec-quiet-btn rec-send-btn armed", "confirm send?");
    armed.onclick = async () => {
      if (armed.disabled) return;
      armed.disabled = true;
      armed.textContent = "sending…";
      try {
        const out = await recOutreachCall("/api/aion/recruiting/outreach/send/" + c.id, { approve: true });
        if (out.readiness && !out.send) {
          recOutreachReady[c.id] = out.readiness;
          showToast(out.error || "send refused");
          renderAion();
          return;
        }
        if (out.view) recCache = out.view;
        if ((out.send || {}).entry && recOutreachLog.id === c.id) recOutreachLog.entries = entries.concat([out.send.entry]);
        delete recOutreachReady[c.id];
        showToast("sent · message " + ((out.send || {}).messageId || ""));
        renderAion();
      } catch (e) {
        showToast(String(e.message || e).slice(0, 140), null, "error");
        armed.disabled = false;
        armed.textContent = "confirm send?";
      }
    };
    send.replaceWith(armed);
    armed.focus();
  };
  sec.append(send);
  return sec;
}

function recSection(title, summary) {
  const sec = el("section", "rec-insp-sec");
  const head = el("div", "aion-sec-label");
  head.append(el("span", "aion-sec-title", title));
  if (summary) head.append(el("span", "aion-sec-count", summary));
  sec.append(head);
  return sec;
}

function recOutreachConnectEl() {
  const box = el("div", "rec-next");
  const a = el("a", "", "connect the sender → Settings › Connections");
  a.href = "#/settings/connections";
  a.title = "sign in as the sender at gmail.send only; the token lives under dataDir, never the vault";
  box.append(a);
  return box;
}

// ---- private Ashby handoff ----
let recAshbyProbe = null;
let recAshbyProposal = {};
let recAshbyChoice = {};

async function recAshbyLoadProbe(quiet) {
  try {
    const r = await fetch("/api/aion/recruiting/ashby/probe", { cache: "no-store" });
    recAshbyProbe = r.ok ? await r.json() : { configured: false, scopes: [], error: await r.text() };
  } catch (e) { recAshbyProbe = { configured: false, scopes: [], error: String(e.message || e) }; }
  if (!quiet && aionMode === "recruiting") renderAion();
}

async function recAshbyCall(url, body) {
  const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
  const text = await r.text();
  let out = {};
  try { out = JSON.parse(text); } catch (_) { out = { error: text }; }
  if (!r.ok && !out.proposal) throw new Error(out.error || text);
  return out;
}

function recAshbyMeta(c) {
  if (c.ashbyCandidateId || c.ashbyApplicationId) {
    return { text: "linked" + (c.ashbyStage ? " · " + c.ashbyStage : ""), alarm: false };
  }
  const choice = recAshbyChoice[c.id] || {};
  const needs = [];
  if (!choice.handoff) needs.push("handoff");
  if (recAshbyProposal[c.id] && (recAshbyProposal[c.id].matches || []).length && !choice.decision) needs.push("decision");
  return { text: "not handed off" + (needs.length ? " · " + needs.join(" + ") + " required" : ""), alarm: true };
}

function recAshbySection(c) {
  const sec = el("div", "rec-ashby");
  if (c.ashbyCandidateId) sec.append(el("div", "rec-next", "candidate " + c.ashbyCandidateId));
  if (c.ashbyApplicationId) sec.append(el("div", "rec-next", "application " + c.ashbyApplicationId));
  if (c.ashbyStage) sec.append(el("div", "rec-next", "official stage: " + c.ashbyStage));
  if (!c.ashbyCandidateId && !c.ashbyApplicationId) sec.append(emptyRow("not handed off"));
  if (recAshbyProbe === null) { recAshbyLoadProbe(); return sec; }
  if (!recAshbyProbe.configured) {
    sec.append(emptyRow("no Ashby key installed — set ASHBY_API_KEY on the server to hand off"));
    return sec;
  }
  if (recAshbyProbe.error) { sec.append(emptyRow("ashby key rejected: " + recAshbyProbe.error.slice(0, 120))); return sec; }
  if (c.stage === "archived") return sec;

  const choice = recAshbyChoice[c.id] || (recAshbyChoice[c.id] = { handoff: "", decision: "", ashbyCandidateId: "", note: "", includeContact: false });
  const prop = recAshbyProposal[c.id];
  const req = () => ({ handoff: choice.handoff, decision: choice.decision, ashbyCandidateId: choice.ashbyCandidateId,
    note: choice.note, includeContact: choice.includeContact });

  const handoff = el("select", "pp-in rec-in" + (choice.handoff ? "" : " rec-uncited"));
  [["", "handoff — choose · required"], ["project", "sourcing project"], ["application", "formal application"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v; o.selected = choice.handoff === v; handoff.append(o);
  });
  handoff.onchange = () => { choice.handoff = handoff.value; if (recPaint) recPaint(); };
  sec.append(handoff);

  const note = el("input", "pp-in rec-in");
  note.type = "text"; note.placeholder = "scout note (posted as Manifest Scout)"; note.value = choice.note;
  note.onblur = () => { choice.note = note.value; };
  sec.append(note);

  const contact = el("label", "rec-next");
  const cb = el("input", ""); cb.type = "checkbox"; cb.checked = choice.includeContact;
  cb.onchange = () => { choice.includeContact = cb.checked; };
  contact.append(cb, el("span", "", "include email/phone in this push"));
  sec.append(contact);

  const preflight = el("button", "pill light", prop ? "re-run preflight" : "preflight push");
  preflight.title = "read Ashby, dedupe by email/name, and render the proposal — writes nothing";
  preflight.onclick = async () => {
    try {
      const out = await recAshbyCall("/api/aion/recruiting/ashby/preflight/" + c.id, req());
      recAshbyProposal[c.id] = out.proposal;
      if (out.proposal && out.proposal.decision) choice.decision = out.proposal.decision;
      renderAion();
    } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
  };
  sec.append(preflight);

  if (!prop) return sec;

  if ((prop.matches || []).length && !prop.linked) {
    sec.append(el("div", "rec-next", "found in Ashby:"));
    prop.matches.forEach((m) => {
      const row = el("div", "rec-next");
      row.append(el("span", "", m.name + (m.primaryEmail ? " · " + m.primaryEmail : "")), el("span", "micro-label", m.id));
      sec.append(row);
    });
    const decision = el("select", "pp-in rec-in" + (choice.decision ? "" : " rec-uncited"));
    [["", "decision — choose · required"], ["link", "link to the found candidate"], ["create", "create anyway (namesake)"]].forEach(([v, label]) => {
      const o = el("option", "", label); o.value = v; o.selected = choice.decision === v; decision.append(o);
    });
    decision.onchange = () => {
      choice.decision = decision.value;
      if (decision.value === "link" && prop.matches.length === 1) choice.ashbyCandidateId = prop.matches[0].id;
    };
    sec.append(decision);
    if (prop.matches.length > 1) {
      const which = el("select", "pp-in rec-in");
      prop.matches.forEach((m) => { const o = el("option", "", m.name + " " + m.id); o.value = m.id; o.selected = choice.ashbyCandidateId === m.id; which.append(o); });
      which.onchange = () => { choice.ashbyCandidateId = which.value; };
      sec.append(which);
    }
  }

  (prop.diff || []).forEach((d) => {
    if (d.action === "keep" || d.action === "skip") return;
    const row = el("div", "rec-next");
    row.append(el("span", "micro-label" + (d.action === "conflict" ? " rec-gate blocked" : ""), d.action));
    row.append(el("span", "", d.field + ": " + (d.manifest || "—") + (d.ashby ? " ⇄ " + d.ashby : "")));
    sec.append(row);
  });
  const writes = el("div", "rec-next", "would call: " + (prop.writes || []).join(" → "));
  sec.append(writes);
  if (prop.conflict) { sec.append(emptyRow("both sides changed — resolve before pushing")); return sec; }
  if ((prop.needsChoice || []).length) { sec.append(emptyRow("choose: " + prop.needsChoice.join(", "))); }

  const approve = el("button", "rec-quiet-btn rec-send-btn", "approve & push to Ashby");
  approve.onclick = () => {
    const armed = el("button", "rec-quiet-btn rec-send-btn armed", "confirm push?");
    armed.onclick = async () => {
      if (armed.disabled) return;
      armed.disabled = true;
      armed.textContent = "pushing…";
      try {
        const out = await recAshbyCall("/api/aion/recruiting/ashby/push/" + c.id, Object.assign(req(), { approve: true }));
        if (out.proposal && !out.push) { recAshbyProposal[c.id] = out.proposal; showToast(out.error || "push refused"); renderAion(); return; }
        if (out.view) recCache = out.view;
        delete recAshbyProposal[c.id];
        showToast("ashby: candidate " + ((out.push || {}).ashbyCandidateId || "") + ((out.push || {}).ashbyApplicationId ? " · application " + out.push.ashbyApplicationId : ""));
        renderAion();
      } catch (e) {
        showToast(String(e.message || e).slice(0, 140), null, "error");
        armed.disabled = false;
        armed.textContent = "confirm push?";
      }
    };
    approve.replaceWith(armed);
    armed.focus();
  };
  sec.append(approve);
  return sec;
}

// The user-actioned sync-back (no poller): pull Ashby-authoritative state
// onto records — applicants, job ids, official stages, base snapshots.
async function recAshbySyncBack(full) {
  try {
    const out = await recAshbyCall("/api/aion/recruiting/ashby/sync", { full: !!full });
    if (out.view) recCache = out.view;
    const s = out.sync || {};
    showToast("ashby sync: " + (s.candidates || 0) + " candidates · " + (s.applications || 0) + " applications" +
      ((s.imported || []).length ? " · " + s.imported.length + " imported ← ashby" : "") +
      ((s.adopted || []).length ? " · " + s.adopted.length + " linked ← ashby" : "") +
      ((s.updated || []).length ? " · " + s.updated.length + " updated" : "") +
      ((s.conflicts || []).length ? " · " + s.conflicts.length + " conflicts" : ""));
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}
