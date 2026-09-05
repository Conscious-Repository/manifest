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
let recOriginSet = false;  // the default follows the data until the owner picks: INBOUND
                           // when applicants wait, else BOTH (an empty-looking tab on
                           // entry taught us the hard way, 2026-09-04)
let recCut = "open";      // open | archived | all
let recSel = null;        // inspector selection (candidate id)
let recQuery = "";        // board search
let recPeopleFacet = "considering"; // considering | known | everyone — the ROLE a person plays
// recRunCleared — a run with no draft still waiting on a decision. Derived,
// never stored: the same rule the run head prints as `cleared`.
function recRunCleared(run) { return !(run.drafts || []).some((d) => d.status === "new"); }
let recShowCleared = false;         // the SOURCES list includes finished runs
let recAdvancedOpen = false;        // the hand-built run form is unfolded
let recKnownOpen = false;   // the contacts picker is open
let recKnown = null;        // its last fetch ({people, available} | {error})
let recKnownQuery = "";
let recPeopleShowArchived = false;  // `who I'd ask` includes the set-aside
let recPersonEdit = null; // the connector id whose row is in edit mode
let recPlaceQuery = "";   // PLACES search
let recPlaceEdit = null;  // the place id whose row is in edit mode
let recNetQuery = "";     // network view search
let recNetTab = "paths";  // paths | people | edges
let recInspOpen = { details: false, evidence: false, network: false, activity: false, ashby: false };

// INTAKE — the one front door (intake plan §5). One paste, resolved by the
// server, shown as a CORRECTABLE scaffold before anything is written. `at`
// names which mount owns the open scaffold, so the rail copy and the board
// copy are the same control rather than two competing boxes.
let recIntake = null; // {at, text, res, name, org, class, dest, known, knownVia, busy}

// sources / scout runs — a run is a cache of a search, never a record
let recSources = null;      // {sources, defaultMax, maxMax, ttlDays} | {unavailable: true}
let recRuns = [];           // every run, newest first, each with its draft queue
let recRunOpen = {};        // run id → queue expanded
let recDraftOpen = {};      // "<run>#<draft>" → that draft's citations expanded
let recDraftTopicOpen = {}; // "<run>#<draft>#<topic>" → that chip's evidence expanded
let recDraftMore = {};      // "<run>#<draft>" → every expertise chip shown, not just four
let recDraftLater = {};     // "<run>#<draft>" → set aside for this sitting (view state only)
let recRunning = false;     // a run is in flight
const recRunForm = { source: "manual", role: "", query: "", max: "", fields: {} };
const REC_RUN_COMMON_FIELDS = ["role", "query", "max"];

// ---- routing (#/aion/recruiting/{board|sources|network|role/<slug>}) ----

// recApplyRoute is called from showAion with everything after "recruiting".
function recApplyRoute(sub) {
  sub = (sub || "").replace(/^\//, "");
  if (sub.startsWith("role/")) { recView = "role"; recRoleView = sub.slice(5); }
  else if (sub === "sources" || sub === "network" || sub === "board" || sub === "places") { recView = sub; }
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
    const r = await fetchJSONRetry("POST", url, body || {}); // survives a deploy-window 502
    if (!r.ok) throw new Error(await r.text());
    const out = await r.json();
    if (out.runs) recRuns = out.runs;
    if (out.view) recCache = out.view;
    if (okMsg) showToast(okMsg);
    renderAion();
    return out;
  } catch (e) {
    showToast(String(e.message || e).slice(0, 140), null, "error");
    // repaint from the state we still hold: a button the caller disabled
    // while the request was out ("looking up…", the armed pass, "undo pass")
    // must not stay dead on a card whose facts did not change
    renderAion();
    return null;
  }
}

async function recPost(url, body, okMsg) {
  try {
    const r = await fetchJSONRetry("POST", url, body || {}); // survives a deploy-window 502
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
    case "places": {
      const places = (recCache.seeds || []).filter((p) => p.class !== "person");
      const sweepable = places.filter(recPlaceSweepable).length;
      return places.length + " places · " + sweepable + " sweepable";
    }
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
      const drafts = recPendingDrafts();
      return (tri ? tri + " to triage · " : "") + open + " candidates" +
        (drafts ? " · " + drafts + " drafts to review" : "");
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
  if (!recOriginSet) recOrigin = recUntriagedCount() > 0 ? "inbound" : "both";
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

  // FOUR VIEWS, named for what they hold (owner, 2026-09-05: "what is the
  // difference between the board, my people and seed?"). They were three
  // ROLES with names that hid them — someone you are DECIDING about, someone
  // you would ROUTE an intro through, and a place you SWEEP FROM. People is
  // one list carrying the role; Places is the sweepable things.
  rail.append(el("div", "micro-label rec-rail-label", "VIEWS"));
  const views = [
    ["board", "People", recUntriagedCount() || ""],
    ["places", "Places", ""],
    ["sources", "Sources", recPendingDrafts() || ""],
    ["network", "Network", ""],
  ];
  views.forEach(([key, label, count]) => {
    const b = el("button", "rec-role" + (recView === key ? " on" : ""));
    b.append(el("span", "rec-role-name", label));
    if (count) b.append(el("span", "rec-role-count rec-count-attn", "● " + count));
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

  rail.append(paintSyncFooter(roles));
}

// The sync footer ALWAYS renders (problem 2 — "sync back" used to appear
// only after a candidate had been opened, because its probe loaded lazily).
// The rail is 190px (150px under 1100px), so a footer label longer than about
// two short words wraps mid-phrase inside its own border and reads as broken
// chrome ("sync back from / ashby"). These say what they do in the width they
// have; the sentence lives in the tooltip.
function paintSyncFooter(roles) {
  const box = el("div", "rec-sync rec-rail-foot");
  const b = el("button", "pill light rec-sync-btn", recSyncing ? "syncing…" : "roles");
  b.disabled = recSyncing;
  b.title = "sync roles — mirror the public Ashby job board onto the role records; never touches criteria";
  b.onclick = () => recSyncRoles();
  box.append(b);
  if (recAshbyProbe && recAshbyProbe.configured && !recAshbyProbe.error) {
    const sb = el("button", "pill light rec-sync-btn", "applicants");
    sb.title = "sync back from Ashby — pull applicants and official stages onto records; a user action, never a poller";
    sb.onclick = () => recAshbySyncBack(false);
    box.append(sb);
    const full = el("button", "rec-linkish rec-sync-full", "full re-sync");
    full.title = "ignore the incremental sync tokens and re-read everything";
    full.onclick = () => recAshbySyncBack(true);
    box.append(full);
  }
  const synced = roles.map((r) => r.synced || "").filter(Boolean).sort().pop();
  if (synced) box.append(el("span", "rec-sync-when", "ashby · " + synced));
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

// ---- INTAKE: paste a thing, approve a scaffold ----
//
// One box replaces three (a rail seed demanding a `class: name` prefix, a
// board paste that made an empty record, a network add promising an editor
// that does not exist). Enter asks the server what the paste IS and, when it
// names ONE thing, fills the scaffold from that source's own record — every
// field carrying the URL it came from. Nothing is written until the owner
// has seen it and can correct it.

// recSeedSweep maps a seed onto the run that sweeps it, or null when nothing
// can. This is also what the intake's "sweep it" uses, so a seed row and a
// fresh paste can never disagree about which source speaks for a thing.
function recSeedSweep(seed) {
  const url = (seed.url || "").trim();
  const name = (seed.name || "").trim();
  const feed = ((seed.unknown || []).find((f) => f.key === "feed") || {}).value || "";
  switch (seed.class) {
    case "work":
      return { source: "openalex", fields: { work: url || name } };
    case "repo":
      return { source: "github", fields: { repo: url || name } };
    case "media":
      return feed || url ? { source: "feed", fields: { feed_url: feed || url } } : null;
    case "lab":
    case "company":
      return url ? { source: "web", fields: { seed_url: url }, query: name } : null;
    case "person":
      return { source: "openalex", query: name };
  }
  return null;
}

// recLoadRun prefills the run form and shows it. It never RUNS: a sweep costs
// somebody else's rate limit, so the last gesture stays the owner's.
function recLoadRun(target) {
  recRunForm.source = target.source;
  recRunForm.query = target.query || "";
  recRunForm.fields = Object.assign({}, target.fields || {});
  recRunForm.role = recRoleId();
  recAdvancedOpen = false; // the pending card is the whole story; the form is not
  recNav("sources");
  // the card at the top of SOURCES says what will be read and by whom, so the
  // toast does not have to teach the next gesture twice
  showToast("ready to sweep — press sweep to run it");
}

function recIntakeReset() { recIntake = null; }

// recIntakeLook asks the server what this is. One call: it resolves the paste
// and, when the paste names one thing, looks that thing up.
async function recIntakeLook(text) {
  recIntake = { text, busy: true, res: null, preview: null, note: "", err: "" };
  if (recPaint) recPaint();
  try {
    const r = await fetch("/api/aion/recruiting/intake/preview", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    });
    if (!r.ok) throw new Error(await r.text());
    const d = await r.json();
    const res = d.resolution || {};
    const p = d.preview || null;
    recIntake = {
      text, busy: false, res, preview: p, note: d.note || "", err: d.error || "",
      // the looked-up name WINS over the one derived from the URL — that was
      // the whole point of looking it up
      name: (p && p.name) || (res.provisional ? "" : res.name) || "",
      org: (p && p.org) || res.org || "",
      url: (p && p.url) || res.url || "",
      feed: (p && p.feed) || "",
      class: res.class || "",
      dest: res.dest || "seed",
      certain: !!d.certain,
      allClasses: false,
      role: recRoleId(),
      known: false, knownVia: "",
      profileField: recIntakeProfileField(res),
    };
  } catch (e) {
    recIntake = { text, busy: false, res: null, err: String(e.message || e).slice(0, 200) };
  }
  if (recPaint) recPaint();
}

// recIntakeProfileField says which profile slot a resolved URL belongs in, so
// an X or LinkedIn link lands somewhere findable instead of only in the note.
function recIntakeProfileField(res) {
  if (!res || !res.url) return "";
  const u = res.url.toLowerCase();
  if (u.includes("x.com/") || u.includes("twitter.com/")) return "x";
  if (u.includes("linkedin.com/")) return "linkedin";
  if (u.includes("github.com/")) return "github";
  if (res.kind === "orcid" || res.kind === "openalex") return "website";
  return "";
}

async function recIntakeCommit() {
  const st = recIntake;
  if (!st || st.busy) return;
  if (st.dest === "seed" && !st.class) { showToast("say what this is first", null, "error"); return; }
  if (!(st.name || "").trim()) { showToast("give it a name — the one from the link is a slug", null, "error"); return; }
  st.busy = true;
  if (recPaint) recPaint();
  try {
    const r = await fetch("/api/aion/recruiting/intake", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        text: st.text, dest: st.dest, class: st.class, name: st.name, org: st.org,
        url: st.url, feed: st.feed, role: st.role || "", profile: st.profileField || "",
        known: !!st.known, knownVia: st.knownVia || "",
      }),
    });
    if (!r.ok) throw new Error(await r.text());
    const d = await r.json();
    if (d.view) recCache = d.view;
    const made = d.created || {};
    const sweep = st.dest === "seed" ? recSeedSweep({
      class: st.class, name: st.name, url: st.url,
      unknown: st.feed ? [{ key: "feed", value: st.feed }] : [],
    }) : null;
    recIntakeReset();
    if (made.kind === "candidate" && made.id) { recSel = made.id; recView = "board"; }
    renderAion();
    if (sweep) showToast(made.name + " added — sweep it", () => recLoadRun(sweep), "info");
    else showToast(made.name + " added");
  } catch (e) {
    st.busy = false;
    st.err = String(e.message || e).slice(0, 200);
    if (recPaint) recPaint();
  }
}

// recScaffoldAsk — Alfred reads what the sources fetched and proposes the
// rest. The server enforces the rule that makes this safe (a filled field
// must appear in the fetched material) and reports what it dropped, so the
// UI can show BOTH: what survived, and what the model said that nothing we
// hold supports. That second list is the point — it is how you learn whether
// to trust the first.
function recScaffoldAsk() {
  const wrap = el("div", "rec-scaffold-ask");
  const st = recIntake || {};
  const ask = st.ask;
  if (!ask) {
    const b = el("button", "rec-linkish", "ask alfred to read it →");
    b.title = "a model pass over the fetched material only — never over what it remembers";
    b.onclick = () => recScaffoldAskStart();
    wrap.append(b);
    return wrap;
  }
  if (ask.status === "running") {
    wrap.append(el("span", "rec-scaffold-note", "✦ alfred is reading what we fetched…"));
    return wrap;
  }
  if (ask.status === "failed" || ask.error) {
    wrap.append(el("span", "rec-scaffold-err", "alfred: " + (ask.error || "the turn failed")));
    const again = el("button", "rec-linkish", "try again");
    again.onclick = () => recScaffoldAskStart();
    wrap.append(again);
    return wrap;
  }
  const sug = ask.suggestion || {};
  const head = el("div", "rec-scaffold-classes");
  head.append(el("span", "micro-label", "ALFRED READ IT"));
  const use = el("button", "rec-linkish", "use this");
  use.onclick = () => {
    if (sug.name) recIntake.name = sug.name;
    if (sug.org) recIntake.org = sug.org;
    if (sug.class) {
      recIntake.class = sug.class;
      recIntake.dest = sug.class === "person" ? "candidate" : "seed";
    }
    if (recPaint) recPaint();
  };
  head.append(use);
  wrap.append(head);
  [["class", sug.class], ["name", sug.name], ["org", sug.org], ["title", sug.title]].forEach(([k, v]) => {
    if (!v) return;
    const row = el("div", "rec-srcfact");
    row.append(el("span", "rec-srcfact-key", k));
    row.append(el("span", "rec-srcfact-val", v));
    wrap.append(row);
  });
  if (sug.note) wrap.append(el("div", "rec-scaffold-note", "“" + sug.note + "” — alfred's words, not a field"));
  if ((sug.people || []).length) {
    wrap.append(el("div", "rec-scaffold-names", "names it found: " + sug.people.join(" · ")));
  }
  (ask.dropped || []).forEach((d) => {
    wrap.append(el("div", "rec-scaffold-dropped", "dropped — " + d));
  });
  if (!sug.name && !sug.org && !sug.class && !(sug.people || []).length) {
    wrap.append(el("div", "rec-scaffold-note", "nothing survived the check — what we fetched does not support it"));
  }
  return wrap;
}

async function recScaffoldAskStart() {
  const st = recIntake;
  if (!st) return;
  st.ask = { status: "running" };
  if (recPaint) recPaint();
  try {
    const r = await fetch("/api/aion/recruiting/intake/ask", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: st.text }),
    });
    if (!r.ok) throw new Error(await r.text());
    const { id } = await r.json();
    recScaffoldPoll(id, st.text);
  } catch (e) {
    if (recIntake && recIntake.text === st.text) {
      recIntake.ask = { status: "failed", error: String(e.message || e).slice(0, 200) };
      if (recPaint) recPaint();
    }
  }
}

// The turn is minutes long, so this polls — the same shape the agent chat and
// the spirits runs use. It stops the moment the scaffold it belongs to is
// gone, so a cancelled intake stops costing anything.
function recScaffoldPoll(id, forText) {
  const tick = async () => {
    if (!recIntake || recIntake.text !== forText || !recIntake.ask) return;
    try {
      const r = await fetch("/api/aion/recruiting/intake/ask/" + encodeURIComponent(id));
      if (!r.ok) throw new Error(await r.text());
      const job = await r.json();
      if (!recIntake || recIntake.text !== forText) return;
      recIntake.ask = job;
      if (recPaint) recPaint();
      if (job.status === "running") setTimeout(tick, 3000);
    } catch (e) {
      if (recIntake && recIntake.text === forText) {
        recIntake.ask = { status: "failed", error: String(e.message || e).slice(0, 200) };
        if (recPaint) recPaint();
      }
    }
  };
  setTimeout(tick, 2500);
}

// recScaffoldClassChips paints the class choice the way the server RANKED it:
// what it decided, then the alternatives it named, then everything else behind
// `other…`. Six equal chips asked the owner to redo the ranking the cascade
// had already done — and made a certain answer look as doubtful as a guess.
function recScaffoldClassChips(res) {
  const all = recCache.seedClasses || [];
  const lead = [];
  const push = (c) => { if (c && all.indexOf(c) >= 0 && lead.indexOf(c) < 0) lead.push(c); };
  push(res.class);
  (res.suggest || []).forEach(push);
  if (!lead.length) all.forEach(push);
  const rest = all.filter((c) => lead.indexOf(c) < 0);

  const row = el("div", "rec-scaffold-classes");
  row.append(el("span", "micro-label", res.class ? "IS A" : "IS IT A"));
  const chip = (cls) => {
    const b = el("button", "filter-chip" + (recIntake.class === cls ? " on" : ""), cls);
    b.onclick = () => {
      recIntake.class = cls;
      recIntake.dest = cls === "person" ? "candidate" : "seed";
      if (recPaint) recPaint();
    };
    return b;
  };
  lead.forEach((c) => row.append(chip(c)));
  if (rest.length && !recIntake.allClasses) {
    const more = el("button", "linkish rec-scaffold-more", "other\u2026");
    more.onclick = () => { recIntake.allClasses = true; if (recPaint) recPaint(); };
    row.append(more);
  } else {
    rest.forEach((c) => row.append(chip(c)));
  }
  return row;
}

function recIntakeBox() {
  const box = el("section", "rec-intake");
  const input = el("input", "pp-in rec-intake-in");
  input.type = "text";
  input.placeholder = "＋ add a person, lab, paper, podcast or profile — paste a link or a name";
  input.value = recIntake && !recIntake.res && !recIntake.busy ? recIntake.text || "" : "";
  input.onkeydown = (e) => {
    if (e.key === "Escape") { recIntakeReset(); if (recPaint) recPaint(); return; }
    if (e.key !== "Enter") return;
    e.preventDefault();
    const v = input.value.trim();
    if (v) recIntakeLook(v);
  };
  box.append(input);
  if (!recIntake) return box;

  const card = el("div", "rec-scaffold");
  if (recIntake.busy && !recIntake.res) {
    card.append(el("div", "rec-scaffold-why", "looking it up…"));
    box.append(card);
    return box;
  }
  if (recIntake.err && !recIntake.res) {
    card.append(el("div", "rec-scaffold-err", recIntake.err));
    const again = el("button", "rec-linkish", "try again");
    again.onclick = () => recIntakeLook(recIntake.text);
    card.append(again);
    box.append(card);
    return box;
  }
  const res = recIntake.res || {};

  // what it decided, why, and WHICH RUNG decided — a guess you can see the
  // basis of is one you can correct; a guess you cannot is one you must catch
  const why = el("div", "rec-scaffold-why");
  why.append(el("span", "rec-scaffold-paste", recIntake.text));
  why.append(el("span", "", res.why || ""));
  // NOT a badge repeating the rung: `why` already names its own basis ("a
  // DOI…", "the page says…", "GitHub says…", "reads as a lab"). The one thing
  // that sentence cannot carry is whether you have to check it, so that is
  // the only thing the marker says — and it says nothing when the answer is
  // settled (docs/ui-conventions.md: an absence shows only when it changes
  // the call).
  if (res.class && !recIntake.certain) why.append(el("span", "rec-rung", "best guess"));
  else if (!res.class) why.append(el("span", "rec-rung", "pick one"));
  card.append(why);
  if (recIntake.err) card.append(el("div", "rec-scaffold-err", recIntake.err));
  else if (recIntake.note) card.append(el("div", "rec-scaffold-note", recIntake.note));

  card.append(recScaffoldClassChips(res));

  // where it lands. Only a person has a choice to make.
  if (recIntake.class === "person") {
    const dest = el("div", "rec-scaffold-classes");
    dest.append(el("span", "micro-label", "LANDS IN"));
    [["candidate", "the board"], ["network", "my people"], ["seed", "seeds"]].forEach(([key, label]) => {
      const b = el("button", "filter-chip" + (recIntake.dest === key ? " on" : ""), label);
      b.title = key === "candidate" ? "a candidate record with fit, evidence and paths"
        : key === "network" ? "someone you know — a node intro paths route through"
        : "a person to sweep FROM, not a candidate";
      b.onclick = () => { recIntake.dest = key; if (recPaint) recPaint(); };
      dest.append(b);
    });
    card.append(dest);
  }

  // the fields, with the provisional name called out for what it is
  const fields = el("div", "rec-scaffold-fields");
  const field = (label, key, hint) => {
    const wrap = el("label", "rec-scaffold-field");
    wrap.append(el("span", "micro-label", label));
    const inp = el("input", "pp-in");
    inp.type = "text";
    inp.value = recIntake[key] || "";
    if (hint) inp.placeholder = hint;
    inp.oninput = () => { recIntake[key] = inp.value; };
    wrap.append(inp);
    return wrap;
  };
  fields.append(field("name", "name", res.provisional && !recIntake.name ? "from the link — name it" : ""));
  fields.append(field("org", "org"));
  if (recIntake.class === "media") fields.append(field("feed", "feed"));
  card.append(fields);
  if (recIntake.url) {
    const link = el("div", "rec-scaffold-link");
    link.append(linkEl(recIntake.url, recIntake.url));
    if (recIntake.profileField && recIntake.dest === "candidate") {
      link.append(el("span", "rec-scaffold-slot", "kept as " + recIntake.profileField));
    }
    card.append(link);
  }

  // what the source actually said, field by field, with where it came from
  const facts = (recIntake.preview || {}).facts || [];
  if (facts.length) {
    const list = el("div", "rec-scaffold-facts");
    list.append(el("span", "micro-label", "FROM " + ((recIntake.preview || {}).facts[0].source || "").toUpperCase()));
    facts.forEach((f) => {
      const row = el("div", "rec-srcfact");
      row.append(el("span", "rec-srcfact-key", f.field));
      row.append(el("span", "rec-srcfact-val", f.value));
      if (f.url) {
        const a = linkEl("↗", f.url);
        a.className = "rec-srcfact-src";
        a.title = f.url;
        row.append(a);
      }
      list.append(row);
    });
    card.append(list);
  }

  // who the thing names — the drafts a sweep would bring in
  const people = (recIntake.preview || {}).people || [];
  if (people.length) {
    const total = (recIntake.preview || {}).total || people.length;
    const who = el("div", "rec-scaffold-people");
    who.append(el("span", "micro-label", total + (total === 1 ? " person on it" : " people on it")));
    who.append(el("span", "rec-scaffold-names",
      people.slice(0, 8).map((p) => p.name + (p.org ? " (" + p.org + ")" : "")).join(" · ") +
      (people.length > 8 ? " · +" + (people.length - 8) + " more" : "")));
    card.append(who);
  }

  // the owner's own word — the strongest edge in the system, and until now
  // the one thing the UI could not say
  if (recIntake.dest === "candidate") {
    const known = el("div", "rec-scaffold-known");
    const box2 = el("input", "");
    box2.type = "checkbox";
    box2.checked = !!recIntake.known;
    box2.onchange = () => { recIntake.known = box2.checked; if (recPaint) recPaint(); };
    const lab = el("label", "rec-scaffold-knownlab");
    lab.append(box2, el("span", "", "I know them"));
    known.append(lab);
    if (recIntake.known) {
      const via = el("select", "pp-in rec-scaffold-via");
      const none = el("option", "", "who knows them…");
      none.value = "";
      via.append(none);
      ((recCache.network || {}).people || []).forEach((p) => {
        const o = el("option", "", p.name);
        o.value = p.id;
        if (recIntake.knownVia === p.id) o.selected = true;
        via.append(o);
      });
      via.onchange = () => { recIntake.knownVia = via.value; };
      known.append(via);
      known.append(el("span", "rec-scaffold-note", "an intro path starts from the person asserting it"));
    }
    card.append(known);

    const roles = (recCache.roles || []);
    if (roles.length) {
      const sel = el("select", "pp-in rec-scaffold-role");
      const none = el("option", "", "no role yet");
      none.value = "";
      sel.append(none);
      roles.forEach((r) => {
        const o = el("option", "", r.title || r.slug);
        o.value = r.id || "role/" + r.slug;
        if ((recIntake.role || "") === o.value) o.selected = true;
        sel.append(o);
      });
      sel.onchange = () => { recIntake.role = sel.value; };
      card.append(sel);
    }
  }

  // the model's pass — over the bytes we fetched, and nothing else
  card.append(recScaffoldAsk());

  const acts = el("div", "rec-scaffold-acts");
  const add = el("button", "pill rec-primary", recIntake.busy ? "adding…" : "add");
  add.disabled = !!recIntake.busy;
  add.onclick = () => recIntakeCommit();
  acts.append(add);
  const sweepTarget = recSeedSweep({
    class: recIntake.class, name: recIntake.name || res.name, url: recIntake.url,
    unknown: recIntake.feed ? [{ key: "feed", value: recIntake.feed }] : [],
  });
  if (sweepTarget) {
    const sw = el("button", "pill light", "sweep without adding →");
    sw.title = "load the " + sweepTarget.source + " run for this, and skip the record";
    sw.onclick = () => { const t = sweepTarget; recIntakeReset(); recLoadRun(t); };
    acts.append(sw);
  }
  const cancel = el("button", "rec-linkish", "cancel");
  cancel.onclick = () => { recIntakeReset(); if (recPaint) recPaint(); };
  acts.append(cancel);
  card.append(acts);

  box.append(card);
  return box;
}

// ---- WHO I'D ASK — the people an introduction can start from ----
//
// This is the ONLY place a derived intro path can begin (OwnerSeeds selects
// `consent: owner`), which is what makes it a different list from the board
// rather than a second copy of it. It had no editor at all: the add box
// promised "org and email are yours to fill in" and there was nowhere to fill
// them in, and no way to remove a duplicate.

function paintConnectors(board, foot) {
  const people = ((recCache.network || {}).people || [])
    .filter((p) => recPeopleShowArchived || !p.archived);
  const q = recQuery.trim().toLowerCase();
  const rows = people.filter((p) => !q || [p.name, p.org, p.title, p.email].join(" ").toLowerCase().includes(q));

  if (!rows.length) {
    board.append(emptyRow(q
      ? "nobody here matches — clear the search"
      : "nobody yet — the people you'd ask for an introduction go here, and every intro path starts from one of them"));
  }
  rows.forEach((p) => board.append(recConnectorRow(p)));

  // THE PEOPLE YOU ALREADY KNOW. This list was hand-typed and two rows long
  // while the app already knew 227 people from your own notes and calendar —
  // which is why no intro path has ever had anywhere to start. Marking is a
  // click on a name you already have, not a name retyped (owner, 2026-09-05).
  board.append(recKnownPicker());
  board.append(ghostInput("＋ someone not in your notes", "aion-add", async (raw) => {
    const name = raw.trim();
    if (!name) return;
    if (await recWrite("/api/aion/recruiting/intake", { dest: "network", name }, "POST", name + " added")) renderAion();
  }, "their name — org, title and email are editable on the row"));

  if (!foot) return;
  const archived = ((recCache.network || {}).people || []).filter((p) => p.archived).length;
  foot.textContent = rows.length + " of " + people.length +
    " · an intro path can only start from someone here";
  const toggle = el("button", "rec-linkish", recPeopleShowArchived ? "hide archived" : "show archived");
  toggle.onclick = () => { recPeopleShowArchived = !recPeopleShowArchived; if (recPaint) recPaint(); };
  if (archived || recPeopleShowArchived) foot.append(" · ", toggle);
}

function recConnectorRow(p) {
  const row = el("div", "rec-net-row" + (p.archived ? " archived" : ""));
  if (recPersonEdit === p.id) return recConnectorEditor(p, row);

  const main = el("div", "rec-net-main");
  main.append(el("span", "rec-net-name", p.name));
  const sub = [p.title, p.org].filter(Boolean).join(" · ");
  if (sub) main.append(el("span", "rec-draft-sub", sub));
  if (p.archived) main.append(el("span", "micro-label", "archived " + p.archived));
  row.append(main);

  const reach = recReachCount(p);
  if (reach) row.append(el("span", "rec-role-count", "reaches " + reach));
  if (p.email) row.append(el("span", "rec-ev-when", p.email));

  const acts = el("div", "rec-place-acts");
  const edit = el("button", "rec-linkish", "edit");
  edit.onclick = () => { recPersonEdit = p.id; if (recPaint) recPaint(); };
  acts.append(edit);
  // a person ARCHIVES — the row is the history of a judgment (owner's rule)
  const arch = el("button", "rec-linkish", p.archived ? "restore" : "archive");
  arch.title = p.archived ? "bring them back as somewhere an intro can start"
    : "set aside — their edges stand, but no new path starts from them";
  arch.onclick = async () => {
    const at = p.archived ? "" : new Date().toISOString().slice(0, 10);
    if (await recWrite("/api/aion/recruiting/network/person/" + encodeURIComponent(p.id),
      { archived: at }, "POST", p.name + (at ? " archived" : " restored"))) renderAion();
  };
  acts.append(arch);
  acts.append(armedDelete("delete", "delete — sure?", async () => {
    if (await recWrite("/api/aion/recruiting/network/person/" + encodeURIComponent(p.id), null, "DELETE")) {
      renderAion();
      showToast(p.name + " deleted — their edges still stand", null, "info");
    }
  }));
  row.append(acts);
  return row;
}

function recConnectorEditor(p, row) {
  row.classList.add("editing");
  const draft = { name: p.name || "", title: p.title || "", org: p.org || "", email: p.email || "", type: p.type || "" };
  const grid = el("div", "rec-place-fields");
  const field = (label, key, hint) => {
    const wrap = el("label", "rec-place-field");
    wrap.append(el("span", "micro-label", label));
    const inp = el("input", "pp-in");
    inp.type = "text";
    inp.value = draft[key];
    if (hint) inp.placeholder = hint;
    inp.oninput = () => { draft[key] = inp.value; };
    inp.onkeydown = (e) => { if (e.key === "Enter") save(); if (e.key === "Escape") cancel(); };
    wrap.append(inp);
    return wrap;
  };
  grid.append(field("name", "name"));
  grid.append(field("title", "title"));
  grid.append(field("org", "org"));
  // D15 lives upstream: no ADAPTER may ever write an address. This is the
  // owner typing one, which is the only way one was ever meant to arrive.
  grid.append(field("email", "email", "typed by you — no source may fill this"));
  row.append(grid);

  const cancel = () => { recPersonEdit = null; if (recPaint) recPaint(); };
  const save = async () => {
    const body = {};
    ["name", "title", "org", "email"].forEach((k) => { if (draft[k] !== (p[k] || "")) body[k] = draft[k]; });
    if (!Object.keys(body).length) { cancel(); return; }
    if (await recWrite("/api/aion/recruiting/network/person/" + encodeURIComponent(p.id), body, "POST", draft.name + " saved")) {
      recPersonEdit = null;
      renderAion();
    }
  };
  const acts = el("div", "rec-place-acts");
  const ok = el("button", "pill light", "save");
  ok.onclick = save;
  const no = el("button", "rec-linkish", "cancel");
  no.onclick = cancel;
  acts.append(ok, no);
  row.append(acts);
  return row;
}

// recKnownPicker — the contacts you have not marked yet. Lazy: the list is
// only fetched when you open it, because it is a read of 227 people and the
// connectors list is useful without it.
function recKnownPicker() {
  const wrap = el("div", "rec-known");
  const head = el("button", "rec-linkish", recKnownOpen ? "▾ from your contacts" : "▸ mark someone from your contacts");
  head.onclick = () => {
    recKnownOpen = !recKnownOpen;
    if (recKnownOpen && recKnown === null) recLoadKnown();
    else if (recPaint) recPaint();
  };
  wrap.append(head);
  if (!recKnownOpen) return wrap;

  if (recKnown === null) { wrap.append(el("div", "rec-foot", "reading your contacts…")); return wrap; }
  if (recKnown.error) { wrap.append(el("div", "rec-scaffold-err", recKnown.error)); return wrap; }
  if (!recKnown.available) {
    wrap.append(el("div", "rec-foot", recKnown.note || "no contacts layer here"));
    return wrap;
  }

  const find = el("input", "pp-in rec-known-find");
  find.type = "search";
  find.placeholder = "search your " + (recKnown.count || 0) + " contacts…";
  find.value = recKnownQuery;
  find.oninput = () => { recKnownQuery = find.value; if (recPaint) recPaint(); };
  wrap.append(find);

  const q = recKnownQuery.trim().toLowerCase();
  const rows = (recKnown.people || [])
    .filter((p) => !p.marked)
    .filter((p) => !q || (p.name || "").toLowerCase().includes(q))
    .slice(0, q ? 40 : 12);
  const list = el("div", "rec-known-list");
  rows.forEach((p) => {
    const b = el("button", "rec-known-row");
    b.append(el("span", "rec-known-name", p.name));
    // last-met is the honest sort key and the honest label: the calendar
    // verified it, and a contact with none says so rather than looking stale
    b.append(el("span", "rec-known-when", p.lastMet ? "met " + p.lastMet : "no meeting on record"));
    b.title = "mark " + p.name + " as someone you'd ask — an intro path can then start from them";
    b.onclick = async () => {
      b.disabled = true;
      if (await recWrite("/api/aion/recruiting/network/mark", { key: p.key, name: p.name }, "POST", p.name + " is someone you'd ask")) {
        recKnown = null;
        renderAion();
      } else { b.disabled = false; }
    };
    list.append(b);
  });
  if (!rows.length) list.append(emptyRow(q ? "nobody matches" : "everyone in your contacts is already marked"));
  wrap.append(list);
  if (!q && (recKnown.people || []).filter((p) => !p.marked).length > rows.length) {
    wrap.append(el("div", "rec-foot", "the " + rows.length + " you saw most recently — search for anyone else"));
  }
  return wrap;
}

async function recLoadKnown() {
  recKnown = null;
  if (recPaint) recPaint();
  try {
    const r = await fetch("/api/aion/recruiting/people/known");
    if (!r.ok) throw new Error((await r.text()).slice(0, 140));
    recKnown = await r.json();
  } catch (e) {
    recKnown = { error: "couldn't read your contacts — " + (e.message || "error") };
  }
  if (recPaint) recPaint();
}

// ---- PLACES — the things you sweep FROM ----
//
// This was a collapsed accordion in a 190px rail, which is why the owner
// could not tell it apart from the board or from "my people": the three
// lists are three ROLES — deciding about, routing through, sweeping from —
// and only the last one is a place. As a view it has room to say what each
// row is FOR, and room for the gestures it never had: edit and delete.
//
// `seed/person` is gone from here on purpose. A person you want to sweep
// from is a person; a bare name already resolves to a candidate, and any
// candidate already carries "more like this".

function recPlaceSweepable(p) { return !!recSeedSweep(p); }

function paintPlacesView(main) {
  const bar = el("div", "rec-toolbar");
  const search = el("input", "pp-in rec-search");
  search.type = "search";
  search.placeholder = "search places…";
  search.value = recPlaceQuery;
  search.oninput = () => { recPlaceQuery = search.value; body(); };
  bar.append(search);
  main.append(bar);

  const host = el("div", "rec-board");
  main.append(host);

  const body = () => {
    host.innerHTML = "";
    const q = recPlaceQuery.trim().toLowerCase();
    const all = (recCache.seeds || []).filter((p) => p.class !== "person")
      .filter((p) => !q || [p.name, p.org, p.url, p.class].join(" ").toLowerCase().includes(q));
    if (!all.length) {
      host.append(emptyRow(q
        ? "no place matches — clear the search to see them all"
        : "nowhere to look yet — paste a lab, a paper, a repo or a show above"));
      return;
    }
    (recCache.seedClasses || []).forEach((cls) => {
      if (cls === "person") return;
      const inClass = all.filter((p) => p.class === cls);
      if (!inClass.length) return;
      host.append(el("div", "micro-label rec-place-class", cls));
      inClass.forEach((p) => host.append(recPlaceRow(p)));
    });
  };
  body();
}

function recPlaceRow(p) {
  const row = el("div", "rec-place");
  if (recPlaceEdit === p.id) return recPlaceEditor(p, row);

  const top = el("div", "rec-place-top");
  if (p.url) {
    const a = linkEl(p.name, p.url);
    a.className = "rec-place-name";
    a.title = p.url;
    top.append(a);
  } else {
    top.append(el("span", "rec-place-name", p.name));
  }
  if (p.org) top.append(el("span", "rec-place-sub", p.org));
  row.append(top);

  const acts = el("div", "rec-place-acts");
  const target = recSeedSweep(p);
  if (target) {
    const go = el("button", "rec-linkish", "sweep →");
    go.title = "load the " + target.source + " run scoped to this place";
    go.onclick = () => recLoadRun(target);
    acts.append(go);
  } else {
    // a place that cannot be swept says WHY and what would fix it, instead of
    // quietly rendering without the button it exists for
    const need = p.class === "media" ? "a feed or a link" : "a link";
    const fix = el("button", "rec-linkish rec-place-need", "needs " + need + " to sweep");
    fix.title = "add one and this place becomes sweepable";
    fix.onclick = () => { recPlaceEdit = p.id; if (recPaint) recPaint(); };
    acts.append(fix);
  }
  const edit = el("button", "rec-linkish", "edit");
  edit.onclick = () => { recPlaceEdit = p.id; if (recPaint) recPaint(); };
  acts.append(edit);
  acts.append(armedDelete("delete", "delete — sure?", () => recPlaceDelete(p)));
  row.append(acts);
  return row;
}

function recPlaceEditor(p, row) {
  row.classList.add("editing");
  const draft = { name: p.name || "", org: p.org || "", url: p.url || "", class: p.class || "" };
  const feedNow = ((p.unknown || []).find((f) => f.key === "feed") || {}).value || "";
  draft.feed = feedNow;

  const grid = el("div", "rec-place-fields");
  const field = (label, key, hint) => {
    const wrap = el("label", "rec-place-field");
    wrap.append(el("span", "micro-label", label));
    const inp = el("input", "pp-in");
    inp.type = "text";
    inp.value = draft[key];
    if (hint) inp.placeholder = hint;
    inp.oninput = () => { draft[key] = inp.value; };
    inp.onkeydown = (e) => { if (e.key === "Enter") save(); if (e.key === "Escape") cancel(); };
    wrap.append(inp);
    return wrap;
  };
  grid.append(field("name", "name"));
  grid.append(field("org", "org"));
  grid.append(field("link", "url", "https://…"));
  if (p.class === "media") grid.append(field("feed", "feed", "the RSS this show publishes"));
  row.append(grid);

  const classes = el("div", "rec-scaffold-classes");
  classes.append(el("span", "micro-label", "is a"));
  (recCache.seedClasses || []).filter((c) => c !== "person").forEach((cls) => {
    const b = el("button", "filter-chip" + (draft.class === cls ? " on" : ""), cls);
    b.onclick = () => { draft.class = cls; if (recPaint) recPaint(); recPlaceEdit = p.id; };
    classes.append(b);
  });
  row.append(classes);

  const cancel = () => { recPlaceEdit = null; if (recPaint) recPaint(); };
  const save = async () => {
    const body = {};
    ["name", "org", "url", "class"].forEach((k) => { if (draft[k] !== (p[k] || "")) body[k] = draft[k]; });
    if (p.class === "media" && draft.feed !== feedNow) body.feed = draft.feed;
    if (!Object.keys(body).length) { cancel(); return; }
    await recWrite("/api/aion/recruiting/place/" + encodeURIComponent(p.id), body, "POST", draft.name + " saved");
    recPlaceEdit = null;
    renderAion();
  };

  const acts = el("div", "rec-place-acts");
  const ok = el("button", "pill light", "save");
  ok.onclick = save;
  const no = el("button", "rec-linkish", "cancel");
  no.onclick = cancel;
  acts.append(ok, no);
  row.append(acts);
  return row;
}

// recPlaceDelete — a cut with an undo, the house idiom (consume's dismiss):
// the row goes at once, and the toast carries the way back because a place is
// three fields and re-adding it by hand is not an undo.
async function recPlaceDelete(p) {
  const feed = ((p.unknown || []).find((f) => f.key === "feed") || {}).value || "";
  if (!await recWrite("/api/aion/recruiting/place/" + encodeURIComponent(p.id), null, "DELETE")) return;
  renderAion();
  showToast(p.name + " deleted · undo", async () => {
    const body = { dest: "seed", class: p.class, name: p.name, org: p.org, url: p.url, text: p.name };
    if (feed) body.feed = feed;
    await recWrite("/api/aion/recruiting/intake", body, "POST", p.name + " is back");
    renderAion();
  }, "info");
}

// recWrite — one write path for the edit/delete gestures: it checks r.ok (a
// fetch does not reject on 4xx), refreshes the cache from the response when
// the server sent a view, and says what refused. Returns false on failure so
// a caller can leave the row alone.
async function recWrite(url, body, method, okMsg) {
  try {
    const opt = { method: method || "POST" };
    if (body) {
      opt.headers = { "Content-Type": "application/json" };
      opt.body = JSON.stringify(body);
    }
    const r = await fetch(url, opt);
    if (!r.ok) throw new Error((await r.text()).slice(0, 160));
    const d = await r.json().catch(() => null);
    if (d && (d.candidates || d.seeds)) recCache = d;
    else if (d && d.view) recCache = d.view;
    if (okMsg) showToast(okMsg);
    return true;
  } catch (e) {
    showToast(String(e.message || e).slice(0, 160), null, "error");
    return false;
  }
}

// ---- the main column, per view ----

function paintMain(main) {
  main.innerHTML = "";
  // the front door is in reach from every view — a lab you want to seed does
  // not care which tab you are standing on. The role console is the exception:
  // it is a rubric editor, not a place things arrive.
  if (recView !== "role") main.append(recIntakeBox());
  if (recView === "places") { paintPlacesView(main); return; }
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
    b.onclick = () => { recOrigin = key; recOriginSet = true; if (recPaint) recPaint(); };
    seg.append(b);
  });
  // the origin cut belongs to the people you are DECIDING about; on any other
  // facet it is a control that does nothing, which is the noise this pass is
  // here to remove
  if (recPeopleFacet === "considering") bar.append(seg);
  main.append(bar);

  // WHO these people are to you. The board and "my people" were two lists of
  // humans in two places with no way to see that one person can be both; the
  // role is a facet on ONE list now (owner, 2026-09-05). `considering` keeps
  // the stages, the gate and the inspector exactly as they were.
  const facets = el("div", "rec-cuts rec-facets");
  const connectors = ((recCache.network || {}).people || []).filter((p) => !p.archived);
  [["considering", "considering", (recCache.candidates || []).filter((c) => c.stage !== "archived").length],
   ["known", "who I'd ask", connectors.length],
   ["everyone", "everyone", 0]].forEach(([key, label, n]) => {
    const b = el("button", "filter-chip" + (recPeopleFacet === key ? " on" : ""), label);
    if (n) b.append(el("span", "rec-role-count", " " + n));
    b.title = key === "considering" ? "people you are deciding about — scored, gated, sendable"
      : key === "known" ? "people who could make an introduction — the only place an intro path starts"
      : "everyone this surface knows about";
    b.onclick = () => { recPeopleFacet = key; if (recPaint) recPaint(); };
    facets.append(b);
  });
  main.append(facets);

  // row 2: the stage cuts. OPEN excludes archived — the old ALL/ACTIVE pair
  // was functionally identical (problem 1), so three cuts, not four.
  if (recPeopleFacet === "considering") {
    const cuts = el("div", "rec-cuts");
    [["open", "OPEN"], ["archived", "ARCHIVED"], ["all", "ALL"]].forEach(([key, label]) => {
      const b = el("button", "filter-chip" + (recCut === key ? " on" : ""), label);
      b.onclick = () => { recCut = key; if (recPaint) recPaint(); };
      cuts.append(b);
    });
    main.append(cuts);
  }

  // (the paste field that used to sit here is the intake now — one front
  // door, mounted by paintMain above the toolbar of every view)

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
  // the `who I'd ask` facet is a different list of humans, so it renders its
  // own rows and returns — the connectors are not candidates and must not
  // borrow the gate, the stages or the origin cut
  if (recPeopleFacet === "known") { paintConnectors(board, foot); return; }
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
    board.append(emptyRow(!all.length
      ? "no candidates yet — paste a link above, or run a source"
      : recOrigin === "inbound"
        ? "no applicants waiting to triage — SOURCED and BOTH show the pipeline"
        : "No candidate matches — the pipeline itself is fine."));
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

// A board row is two facts and one state: name over origin/title · org, the
// state that wants you + the gate chip on the right. Everything else moved
// into the inspector — extra columns that changed no decision while scanning.
function recRoleTitle(roleId) {
  const r = (recCache.roles || []).find((x) => (x.id || "role/" + x.slug) === roleId);
  return r ? (r.title || r.slug) : "";
}
function recCard(c) {
  const p = c.profile || {};
  const origin = c.inbound ? "Applicant — " + (recRoleTitle(c.role) || p.title || "") : (p.title || "");
  const sub = [origin, p.org].filter(Boolean).join(" · ");
  const gate = recGateTable(c);
  const right = el("span", "rec-right");
  right.append(recStateCell(c));
  const chip = el("span", "micro-label rec-gate " + gate.chipCls, gate.chipLabel);
  if (gate.reason) chip.title = gate.reason;
  right.append(chip);
  const card = cardShell({
    kind: "rec-card" + (recSel === c.id ? " sel" : ""),
    title: c.name,
    date: right,
    meta: sub || null,
  });
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
  // YOU POINT AT A THING; THE SOURCE FOLLOWS FROM IT (owner, 2026-09-05:
  // "adding a new source isn't intuitive yet"). The front door is the intake
  // above and the `sweep →` on any place or person — both land here as a
  // PENDING SWEEP, one card naming what will be read and by whom. The form
  // that makes you choose an adapter is still here, one fold down, for the
  // day you want to hand-build a query. Two levels, and no more.
  const pending = recPendingSweep();
  if (pending) main.append(recPendingSweepCard(pending));
  const list = el("div", "rec-run-list");
  // A RUN WITH NOTHING LEFT TO DECIDE is done, and a done run in the way of a
  // live one is the silt the owner asked to be rid of. `cleared` folds them
  // away by default; the toggle says how many, so nothing disappears without
  // saying so.
  const decided = recRuns.filter(recRunCleared);
  const live = recRuns.filter((r) => !recRunCleared(r));
  live.forEach((run) => list.append(recRunCard(run)));
  if (recShowCleared) decided.forEach((run) => list.append(recRunCard(run)));
  if (!recRuns.length) {
    list.append(emptyRow("nothing swept yet — paste a link above, or open PLACES and sweep one"));
  } else if (!live.length && !recShowCleared) {
    list.append(emptyRow("nothing left to review — every run is triaged"));
  }
  main.append(list);
  main.append(recAdvancedRun());
  if (decided.length) {
    const foot = el("div", "rec-foot");
    const t = el("button", "rec-linkish", recShowCleared
      ? "hide the " + decided.length + " cleared run" + (decided.length === 1 ? "" : "s")
      : "show " + decided.length + " cleared run" + (decided.length === 1 ? "" : "s"));
    t.title = "runs with nothing left to decide — kept until they expire, and pinned ones kept past that";
    t.onclick = () => { recShowCleared = !recShowCleared; if (recPaint) recPaint(); };
    foot.append(t);
    main.append(foot);
  }
}

// recPendingSweep — the sweep that has been loaded and not yet run. Derived
// from the run form, so pointing at a place, a person or a pasted link all
// arrive the same way and nothing has to remember which gesture set it.
function recPendingSweep() {
  const f = recRunForm.fields || {};
  const target = (recRunForm.query || f.seed_url || f.work || f.repo || f.feed_url || "").trim();
  if (!target) return null;
  return { source: recRunForm.source, target, role: recRunForm.role || recRoleId() };
}

// recPendingSweepCard — what is about to be read, by whom, and one button.
// The adapter is named but not offered: it FOLLOWED from the thing you
// pointed at, and changing it is the advanced case.
function recPendingSweepCard(p) {
  const card = el("div", "rec-pending");
  const head = el("div", "rec-pending-head");
  head.append(el("span", "micro-label", "ready to sweep"));
  head.append(el("span", "rec-pending-src", p.source));
  card.append(head);
  const what = el("div", "rec-pending-what", p.target.length > 140 ? p.target.slice(0, 137) + "…" : p.target);
  what.title = p.target;
  card.append(what);

  const acts = el("div", "rec-pending-acts");
  const go = el("button", "pill rec-primary", recRunning ? "sweeping…" : "sweep");
  go.disabled = !!recRunning;
  go.onclick = () => recRunSource();
  acts.append(go);

  // the role this sweep files its results under — the one choice worth making
  // before a sweep, because it decides which rubric the drafts are read against
  const roles = recCache.roles || [];
  if (roles.length) {
    const sel = el("select", "pp-in rec-pending-role");
    const none = el("option", "", "no role");
    none.value = "";
    sel.append(none);
    roles.forEach((r) => {
      const id = r.id || "role/" + r.slug;
      const o = el("option", "", r.title || r.slug);
      o.value = id;
      if (p.role === id) o.selected = true;
      sel.append(o);
    });
    sel.onchange = () => { recRunForm.role = sel.value; };
    acts.append(sel);
  }
  const adv = el("button", "rec-linkish", "change…");
  adv.title = "pick a different source, or edit the query by hand";
  adv.onclick = () => { recAdvancedOpen = true; if (recPaint) recPaint(); };
  acts.append(adv);
  const clear = el("button", "rec-linkish", "cancel");
  clear.onclick = () => {
    recRunForm.query = "";
    recRunForm.fields = {};
    if (recPaint) recPaint();
  };
  acts.append(clear);
  card.append(acts);
  return card;
}

// recAdvancedRun — the hand-built run, one fold down. This WAS the front door
// (an adapter select, a role select, a query box and one free-text input per
// adapter field), which is why adding a source did not feel like anything: it
// asked you to know the tool before you could name the thing.
function recAdvancedRun() {
  const wrap = el("div", "rec-advanced");
  const head = el("button", "rec-linkish", recAdvancedOpen ? "▾ advanced — build a run by hand" : "▸ advanced — build a run by hand");
  head.onclick = () => { recAdvancedOpen = !recAdvancedOpen; if (recPaint) recPaint(); };
  wrap.append(head);
  if (!recAdvancedOpen) return wrap;
  wrap.append(recRunFormEl());
  // the rules of this console, stated once under the controls they govern
  wrap.append(el("div", "rec-run-rule",
    "max " + (recSources.maxMax || 100) + " · one record per accept, no accept-all · " +
    "look up asks the other indexes about one name"));
  return wrap;
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
    showToast("run: " + (c.fetched || 0) + " fetched · " +
      (c.new || 0) + " new · " + (c.duplicate || 0) + " duplicate");
  } catch (e) {
    showToast(String(e.message || e).slice(0, 140), null, "error");
  } finally {
    recRunning = false;
    renderAion();
  }
}

// recUntil — an expiry reads as a DISTANCE, not a date: what a run is worth is
// how long you still have to triage it. "in 27d" / "in 6h" / "expired".
function recUntil(iso) {
  const t = Date.parse(iso);
  if (!isFinite(t)) return "";
  const ms = t - Date.now();
  if (ms <= 0) return "expired";
  const h = Math.round(ms / 3600000);
  return h < 48 ? "in " + Math.max(1, h) + "h" : "in " + Math.round(h / 24) + "d";
}

function recRunCard(run) {
  const scope = run.scope || {};
  const c = run.counts || {};
  const open = !!recRunOpen[run.id];
  const card = el("article", "rec-run" + (run.pinned ? " pinned" : ""));

  const head = el("div", "rec-run-head");
  const toggle = el("button", "rec-run-toggle");
  toggle.append(el("span", "sec-caret", open ? "▾" : "▸"));
  toggle.append(el("span", "rec-run-src", run.source));
  if (run.pinned) toggle.append(el("span", "rec-run-chip pinned", "pinned"));
  // what this run was scoped to, in ONE place. A works/repo/feed run is
  // scoped by a FIELD, and the header used to show nothing for those — which
  // is why every card had to repeat the paper's citation to be readable
  // (owner, 2026-09-05). The subject is the drafts' own shared line when they
  // have one, else the query, else the reference that was named.
  const f = scope.fields || {};
  const scopeText = recRunSubject(run) || scope.query || f.seed_url || f.work || f.repo || f.feed_url || "";
  if (scopeText) {
    const sc = el("span", "rec-run-scope", scopeText.length > 120 ? scopeText.slice(0, 117) + "…" : scopeText);
    sc.title = scopeText;
    toggle.append(sc);
  }
  const role = (recCache.roles || []).find((r) => (r.id || "role/" + r.slug) === scope.role);
  if (role) toggle.append(el("span", "rec-draft-sub", role.title || role.slug));
  toggle.onclick = () => { recRunOpen[run.id] = !open; renderAion(); };
  head.append(toggle);
  // what this run still WANTS: the one thing worth reading across nine rows
  const waiting = (run.drafts || []).filter((d) => d.status === "new").length;
  if (waiting) {
    head.append(el("span", "rec-run-status attn", waiting + " to review"));
  } else {
    head.append(el("span", "rec-run-status", "cleared"));
  }
  const pin = el("button", "rec-run-pin", run.pinned ? "unpin" : "pin");
  pin.title = run.pinned ? "let the sweep take this run after it expires" : "keep this run past its expiry";
  pin.onclick = (e) => {
    e.stopPropagation();
    recSourcesPost("/api/aion/recruiting/sources/pin/" + run.id,
      { pinned: !run.pinned }, run.pinned ? "run unpinned" : "run pinned");
  };
  head.append(pin);
  card.append(head);

  const counts = el("div", "rec-run-counts");
  [["fetched", c.fetched], ["new", c.new], ["dup", c.duplicate], ["accepted", c.accepted], ["passed", c.rejected]]
    .forEach(([k, n]) => counts.append(el("span", "rec-run-count" + (n ? " has" : ""), (n || 0) + " " + k)));
  counts.append(el("span", "rec-run-when", fmtWhen(run.startedAt)));
  let expiry = "kept until triaged";
  if (run.expiresAt) expiry = (run.pinned ? "pinned past " : "expires ") + recUntil(run.expiresAt);
  counts.append(el("span", "rec-run-expiry", expiry));
  card.append(counts);
  if (!open) return card;

  const queue = el("div", "rec-draft-list");
  (run.drafts || []).forEach((d) => queue.append(recDraftCard(run, d)));
  if (!(run.drafts || []).length) queue.append(emptyRow("the source returned nothing"));
  card.append(queue);
  return card;
}

// hostname — a citation's ADDRESS is worth reading; the full URL is not. Six
// grant URLs at full length were what turned this queue into a wall of text.
function recHost(u) {
  try { return new URL(u).hostname.replace(/^www\./, ""); } catch (e) { return u; }
}

// recDraftLookup asks the other public indexes about this exact person and
// reports what came back — including nothing, which is an answer.
async function recDraftLookup(run, d) {
  const out = await recSourcesPost("/api/aion/recruiting/sources/lookup/" + run.id + "/" + d.id, {});
  if (!out) return;
  const r = out.lookup || {};
  const bits = [];
  if (r.cites) bits.push(r.cites + " citation" + (r.cites === 1 ? "" : "s"));
  if (r.links) bits.push(r.links + " link" + (r.links === 1 ? "" : "s"));
  if ((r.filled || []).length) bits.push("filled " + r.filled.join(" + "));
  const where = (r.matched || []).length ? " from " + r.matched.join(", ") : "";
  showToast(bits.length
    ? "looked up " + r.name + ": " + bits.join(" · ") + where
    : "looked up " + r.name + " — nothing new under that exact name" +
      ((r.failed || []).length ? " (" + r.failed.join(", ") + " unreachable)" : ""));
}

// ---- the decision card (enrichment Phase 2) ----
// Reading order follows the research's card table: identity → labelled
// presence → inferred expertise → why surfaced → path coverage → evidence →
// action. Every line below is DERIVED from the run JSON Phase 1 wrote
// (homepage/linkedin/github/orcid/site + topics + evidence); nothing here
// scores, ranks, or guesses.

// recDraftContacts — the classified presence strip. Four labelled classes
// (O3) plus `site` standing in for a homepage the classifier would not call
// one. Each is the URL verbatim; the value shown is the part a reader can
// recognize (a handle, an id, a host), never a manufactured one.
function recDraftContacts(dr) {
  const out = [];
  const tail = (u, marker) => {
    const s = (u || "").trim().replace(/\/+$/, "");
    const i = s.toLowerCase().indexOf(marker);
    return i < 0 ? recHost(s) : s.slice(i + marker.length).split("/")[0] || recHost(s);
  };
  if (dr.homepage) out.push({ label: "homepage", value: recHost(dr.homepage), url: dr.homepage });
  else if (dr.site) out.push({ label: "site", value: recHost(dr.site), url: dr.site });
  if (dr.linkedin) {
    // the profile path as LinkedIn spells it (in/…, pub/…): never "in/" glued
    // onto a host when the URL is not a profile
    let v = "";
    try { v = new URL(dr.linkedin.trim()).pathname.replace(/^\/+|\/+$/g, ""); } catch (e) { v = ""; }
    out.push({ label: "linkedin", value: v || recHost(dr.linkedin), url: dr.linkedin });
  }
  if (dr.github) out.push({ label: "github", value: "@" + tail(dr.github, "github.com/"), url: dr.github });
  if (dr.orcid) out.push({ label: "orcid", value: tail(dr.orcid, "orcid.org/"), url: dr.orcid });
  return out;
}

// recExtKey mirrors recruiting.ExtKeyFromURL: the durable graph key an
// identity link names (`ext/orcid/…`, `ext/openalex/A…`, `ext/github/…`), or
// "" for a page that identifies nobody. Same rules, same spelling, so a
// draft can be matched against network/edges.md endpoints client-side.
function recExtKey(raw) {
  const u = (raw || "").trim().toLowerCase();
  if (!u) return "";
  for (const [host, kind] of [["orcid.org/", "orcid"], ["openalex.org/", "openalex"], ["github.com/", "github"]]) {
    const i = u.indexOf(host);
    if (i < 0) continue;
    let id = u.slice(i + host.length).replace(/^\/+|\/+$/g, "");
    if (!id || (id.includes("/") && kind !== "github")) continue;
    if (kind === "github") { id = id.split("/")[0]; if (!id) continue; }
    if (kind === "openalex") { id = id.toUpperCase(); if (!id.startsWith("A")) continue; }
    return "ext/" + kind + "/" + id;
  }
  return "";
}

// recDraftExtKeys — every graph key this draft answers to (source ref +
// identity links), the same set edges_identity.go repoints on accept.
function recDraftExtKeys(dr) {
  const keys = [];
  const src = (dr.sourceId || "").toLowerCase(), ext = (dr.externalId || "").trim();
  if (ext && (src === "openalex" || src === "orcid")) keys.push("ext/" + src + "/" + ext);
  else if (ext && src === "github") keys.push("ext/github/" + ext.toLowerCase());
  [dr.orcid, dr.github].concat(dr.links || []).forEach((u) => { const k = recExtKey(u); if (k) keys.push(k); });
  return keys.filter((k, i, all) => all.indexOf(k) === i);
}

// recDraftWhy — what the source SAID about them, verbatim, and nothing else.
//
// It used to open with "<source> returned them for “<the run's query>”" on
// every card in the run — a sentence the run header above already says, once,
// for all of them. What survives is the only part that differs per person and
// the only part a go/no-go rests on: their own quoted line. "" when the source
// quoted nothing; the citations line then carries that fact.
function recDraftWhy(run, dr) {
  const quote = recDraftQuote(run, dr);
  if (!quote) return "";
  const subject = recRunSubject(run);
  if (!subject || !quote.startsWith(subject)) return quote;
  // the run header carries the shared head; the card keeps what differs
  return quote.slice(subject.length).replace(/^\s*[·,—-]\s*/, "").trim() || quote;
}

// recDraftQuote is the raw verbatim line, before the run's shared subject is
// taken off it (recRunSubject reads these to find that head).
function recDraftQuote(run, dr) {
  const ev = (dr.evidence || []).filter((e) => (e.snippet || "").trim());
  // what they DID outranks where they sit: the works/repo/grant row first,
  // the affiliation row only when nothing else was quoted
  const rank = { publication: 0, repo: 1, grant: 2, conference: 3, page: 4 };
  const byKind = (a, b) => (rank[a.kind] ?? 9) - (rank[b.kind] ?? 9);
  const said = ev.filter((e) => e.sourceId === run.source).sort(byKind)[0] || ev.slice().sort(byKind)[0];
  if (!said) return recDraftTrail(dr) ? "" : (dr.note || "").trim();
  let quote = said.snippet.trim();
  // an author record's "· topics: a; b; c" tail is the chip strip above,
  // already rendered — the quote keeps the counts and drops the repeat
  if ((dr.topics || []).length) quote = quote.replace(/\s*·\s*topics:.*$/i, "").trim() || quote;
  return quote;
}

// recRunSubject — what EVERY draft in a run says identically.
//
// A works run quotes the same paper on all 26 author cards; a repo run quotes
// the same repository on every contributor. That shared head is the RUN's
// subject, not a fact about any one person, and repeating it per card buries
// the two words that actually differ (their position, their institution). It
// is hoisted into the run header and stripped from the cards.
//
// Derived per paint from the drafts themselves, so a run whose drafts share
// nothing keeps every quote whole.
const recRunSubjectMemo = {};
function recRunSubject(run) {
  const drafts = run.drafts || [];
  const memoKey = run.id + "#" + drafts.length;
  if (recRunSubjectMemo[memoKey] !== undefined) return recRunSubjectMemo[memoKey];
  let out = "";
  const quotes = drafts.map((d) => recDraftQuote(run, d.draft || {})).filter(Boolean);
  if (quotes.length >= 2) {
    let p = quotes[0];
    for (const q of quotes.slice(1)) {
      let i = 0;
      while (i < p.length && i < q.length && p[i] === q[i]) i++;
      p = p.slice(0, i);
      if (!p) break;
    }
    // cut back to a separator so the shared head ends on a phrase, never
    // mid-word ("…, 2016, 10.1073/pnas.15" is not a subject)
    const cut = Math.max(p.lastIndexOf(" · "), p.lastIndexOf(", "), p.lastIndexOf(" — "));
    if (cut > 20) out = p.slice(0, cut).trim();
  }
  recRunSubjectMemo[memoKey] = out;
  return out;
}

// recDraftTrail reads the web crawler's traversal note — "found on <url> ·
// discovered from <url> · depth N" — into its parts, or null for any other
// note. That string is PROVENANCE, not prose: printed as-is it put two full
// URLs, wrapping over three lines, above the only content that decides
// anything. The card shows its hosts and keeps the addresses in the fold.
function recDraftTrail(dr) {
  const note = (dr.note || "").trim();
  if (!/^found on\s+https?:\/\//i.test(note)) return null;
  const found = (note.match(/found on\s+(\S+)/i) || [])[1] || "";
  const from = (note.match(/discovered from\s+(\S+)/i) || [])[1] || "";
  const depth = (note.match(/depth\s+(\d+)/i) || [])[1] || "";
  return { found: found.replace(/[·,]$/, ""), from: from.replace(/[·,]$/, ""), depth };
}

// recTopicEvidence — the rows behind one chip: evidence whose verbatim
// snippet names the topic, else the source's publication row (the author
// record the topic came from, O4). Empty means the chip stands on the
// source's word alone — the drill-down says exactly that.
function recTopicEvidence(dr, topic) {
  const t = topic.toLowerCase();
  const ev = dr.evidence || [];
  const named = ev.filter((e) => (e.snippet || "").toLowerCase().includes(t));
  return named.length ? named : ev.filter((e) => e.kind === "publication");
}

// recDraftPathLine — the RELATIONSHIP line, framed as coverage: what this
// network has recorded about the person, never a claim about the world.
//   queued or on the board → the draft's ranked paths (server-derived on
//                            every read, Phase 3: to the record once
//                            accepted, to the external keys while queued),
//                            with the hop the route rests on and how old
//                            its oldest edge is
//   in the graph           → edges name their external id but no seed
//                            reaches them yet
//   nothing                → "no known path" — nobody here is recorded as
//                            linked yet
function recDraftPathLine(d, dr) {
  const row = el("div", "rec-draft-path");
  row.append(el("span", "micro-label", "path"));
  const cand = d.candidateId && ((recCache || {}).candidates || []).find((c) => c.id === d.candidateId);
  const paths = (d.paths || []).length ? d.paths : ((cand && cand.paths) || []);
  if (paths.length) {
    const best = paths[0];
    if (best.kind === REC_PATH_KIND_DERIVED) {
      const chip = el("span", "micro-label rec-derived", "derived");
      chip.title = "computed from the network graph — not owner-confirmed";
      row.append(chip);
    }
    if (best.inferred) row.append(el("span", "micro-label rec-inferred", "inferred"));
    row.append(el("span", "rec-path-hops", best.path));
    if (best.confidence) row.append(el("span", "rec-path-conf", best.confidence));
    if (paths.length > 1) row.append(el("span", "rec-path-conf", "+" + (paths.length - 1) + " more"));
    if (best.weakest || best.observed) {
      // the route is only as good as its weakest hop, and only as fresh as
      // its oldest one — both in words, so a strong-looking route never
      // hides a stale or inferred link
      const weak = el("div", "rec-draft-path-weak");
      if (best.weakest) weak.append(el("span", "", "rests on " + best.weakest));
      if (best.observed) weak.append(el("span", "rec-ev-when", "oldest edge seen " + fmtWhen(best.observed)));
      row.append(weak);
    }
    return row;
  }
  const keys = recDraftExtKeys(dr);
  const net = (recCache || {}).network || {};
  const edges = (net.edges || []).filter((e) => keys.includes((e.from || "").trim()) || keys.includes((e.to || "").trim()));
  if (edges.length) {
    const kinds = edges.map((e) => e.kind).filter((k, i, all) => k && all.indexOf(k) === i);
    row.append(el("span", "rec-draft-path-text",
      edges.length + " edge" + (edges.length === 1 ? " in the network names them" : "s in the network name them") +
      (kinds.length ? " (" + kinds.join(", ") + ")" : "") + " · but no route from you reaches those edges yet"));
    const go = el("button", "rec-linkish", "open network view →");
    go.onclick = () => { recNetQuery = dr.name || ""; recNav("network"); };
    row.append(go);
    return row;
  }
  // No route and no edge naming them — the ordinary case for a stranger off
  // a crawl, and a sentence saying so on every card is noise, not coverage
  // (owner, 2026-09-05). The absence shows as the row not being there; the
  // NETWORK view is where "who can reach whom" is actually read.
  return null;
}

// recDraftTopics — the expertise chips. Every chip says `inferred` in words
// (not a hover), opens its own evidence + date, and four is the default
// depth (O2); the rest sit behind "more". No topics is a sentence, not a
// blank: the source named none, which is not the same as having none.
function recDraftTopics(run, d, dr, key) {
  const wrap = el("div", "rec-draft-topics");
  wrap.append(el("span", "micro-label", "expertise"));
  const topics = (dr.topics || []).map((t) => (t || "").trim()).filter(Boolean);
  if (!topics.length) {
    // NOTHING. An absence that appears on nearly every card teaches nothing
    // and costs four lines of reading (owner, 2026-09-05): the row is simply
    // not there, and `look up` — right below — is what asks the other
    // indexes. The exception that stays loud is "no citations", because
    // that one changes the decision.
    return [];
  }
  const showAll = !!recDraftMore[key];
  const shown = showAll ? topics : topics.slice(0, 4);
  const out = [wrap];
  shown.forEach((t) => {
    const tkey = key + "#" + t;
    const on = !!recDraftTopicOpen[tkey];
    const chip = el("button", "rec-topic-chip" + (on ? " on" : ""));
    chip.append(el("span", "rec-topic-name", t));
    chip.append(el("span", "micro-label rec-topic-mark", "inferred"));
    chip.title = "named by " + run.source + " from this author's own works — open for the evidence and date";
    chip.setAttribute("aria-expanded", on ? "true" : "false");
    chip.onclick = () => { recDraftTopicOpen[tkey] = !on; if (recPaint) recPaint(); };
    wrap.append(chip);
    if (!on) return;
    const body = el("div", "rec-topic-ev");
    const rows = recTopicEvidence(dr, t);
    const src = rows.find((e) => e.sourceId) || {};
    body.append(el("div", "rec-draft-sub",
      "“" + t + "” · reported by " + (src.sourceId || run.source) + " · inferred from attributed works, not confirmed"));
    rows.forEach((e) => {
      const row = el("div", "rec-ev-row");
      const et = el("div", "rec-ev-top");
      if (e.kind) et.append(el("span", "micro-label rec-ev-kind", e.kind));
      if (e.urlOrFile) {
        const a = linkEl(recHost(e.urlOrFile) + " ↗", e.urlOrFile);
        a.className = "rec-draft-link";
        a.title = e.urlOrFile;
        et.append(a);
      }
      if (e.retrievedAt) et.append(el("span", "rec-ev-when", "retrieved " + fmtWhen(e.retrievedAt)));
      row.append(et);
      if (e.snippet) row.append(el("blockquote", "rec-ev-quote", e.snippet));
      body.append(row);
    });
    if (!rows.length) body.append(el("div", "rec-draft-sub", "no attributable work listed for this topic yet — the chip stands on the source's word alone"));
    out.push(body);
  });
  if (topics.length > 4) {
    const more = el("button", "rec-topic-more", showAll ? "fewer" : "+" + (topics.length - 4) + " more");
    more.onclick = () => { recDraftMore[key] = !showAll; if (recPaint) recPaint(); };
    wrap.append(more);
  }
  return out;
}

// recDraftPass — Pass is a decision about THIS SEARCH: arm-then-confirm (no
// native dialog), and the confirm names the scope so a pass never reads as a
// verdict on the person. The draft stays in the run cache; nothing is
// deleted and no record is touched. A confirmed pass is reversible too: the
// passed card carries "undo pass" (recDraftUnpass), which returns the draft
// to `new` exactly as it was (Phase 3) — so the disarm window is a courtesy,
// not the only way back.
function recDraftPass(run, d) {
  const pass = el("button", "pill light rec-draft-reject", "pass");
  pass.title = "pass on this person for this search — stays in the run cache, nothing is deleted";
  pass.onclick = () => {
    // ink, not the red .pill.armed: a pass is reversible in spirit and deletes nothing
    const armed = el("button", "pill light rec-draft-reject rec-draft-armed", "pass on this search?");
    armed.title = "confirms the pass — irrelevant here, too little evidence, or the wrong person";
    const cancel = el("button", "pill light", "keep");
    let timer = setTimeout(() => { armed.replaceWith(pass); cancel.remove(); }, 4000);
    cancel.onclick = () => { clearTimeout(timer); armed.replaceWith(pass); cancel.remove(); };
    armed.onclick = () => {
      clearTimeout(timer);
      armed.disabled = true;
      cancel.remove();
      recSourcesPost("/api/aion/recruiting/sources/reject/" + run.id + "/" + d.id, {},
        "passed on " + ((d.draft || {}).name || "this draft") + " for this search · still in the run cache");
    };
    pass.replaceWith(armed);
    armed.after(cancel);
    armed.focus();
  };
  return pass;
}

// recDraftUnpass — the way back from a confirmed pass: the draft returns to
// `new` as it was (undecided, the run's expiry clock cleared), the reversal
// is ledgered beside the pass, and nothing touches the vault. A misjudged
// pass costs one click, not a re-run of someone else's API.
function recDraftUnpass(run, d) {
  const undo = el("button", "rec-linkish", "undo pass");
  undo.title = "bring this draft back to new for this search — the pass is reversed, nothing was ever deleted";
  undo.onclick = () => {
    undo.disabled = true;
    recSourcesPost("/api/aion/recruiting/sources/unreject/" + run.id + "/" + d.id, {},
      "pass undone · " + ((d.draft || {}).name || "the draft") + " is back in the queue");
  };
  return undo;
}

// ⚠ ONE DRAFT IS ONE CARD. This queue used to render every citation's full
// abstract inline, with each raw URL appended bare to the card — which made
// adjacent links run together into one unreadable string and buried the two
// facts that decide anything (who they are, what backs it) under a page of
// duplicated grant text. Now: the person leads, what backs them is a count
// you open, and the decision is always in reach.
function recDraftCard(run, d) {
  const dr = d.draft || {};
  const key = run.id + "#" + d.id;
  const open = !!recDraftOpen[key];
  const later = d.status === "new" && !!recDraftLater[key];
  const card = el("div", "rec-draft " + d.status + (later ? " later" : ""));

  // 1 · identity
  const head = el("div", "rec-draft-head");
  head.append(el("span", "micro-label rec-draft-status " + d.status, d.status === "rejected" ? "passed" : d.status));
  head.append(el("span", "rec-draft-name", dr.name || "(unnamed)"));
  if (later) head.append(el("span", "micro-label rec-draft-later-mark", "later"));
  if (d.lookedUpAt) head.append(el("span", "rec-draft-flag", "looked up"));
  card.append(head);

  if (later) {
    // set aside for this sitting: one line that says it is NOT a decision
    // (the status chip still reads `new`; a reload brings it back), and the
    // way back
    head.append(el("span", "rec-draft-flag", "this sitting only · still new, not a decision"));
    const back = el("button", "rec-linkish", "bring back");
    back.onclick = () => { delete recDraftLater[key]; if (recPaint) recPaint(); };
    card.append(back);
    return card;
  }

  const sub = [dr.title, dr.org, dr.location].filter(Boolean).join(" · ");
  if (sub) card.append(el("div", "rec-draft-sub", sub));

  if (d.candidateId) {
    // the MATCH matters ("same name" vs "external id" is the difference
    // between a guess and a proof); the id it matched on does not
    const why = (d.reason || "").replace(/\s+\S*:\S+$/, "").trim();
    const on = el("button", "rec-draft-on", "already on the board" + (why ? " · matched by " + why : ""));
    on.onclick = () => { recSel = d.candidateId; recNav("board"); };
    card.append(on);
  }

  // 2 · what they have worked on, and what the source said about them —
  // the two things a go/no-go rests on, so they come before presence,
  // provenance and path (owner, 2026-09-05).
  recDraftTopics(run, d, dr, key).forEach((n) => card.append(n));

  const whyText = recDraftWhy(run, dr);
  if (whyText) {
    const why = el("blockquote", "rec-draft-said");
    why.textContent = whyText.length > 220 ? whyText.slice(0, 217) + "…" : whyText;
    why.title = whyText;
    card.append(why);
  }

  // 3 · classified presence — each class labelled, none folded into a count
  const contacts = recDraftContacts(dr);
  const contactURLs = {};
  if (contacts.length) {
    const strip = el("div", "rec-draft-contacts");
    contacts.forEach((c) => {
      contactURLs[c.url.trim()] = true;
      const a = linkEl("", c.url);
      a.className = "rec-draft-contact";
      a.title = c.url;
      a.append(el("span", "micro-label", c.label), el("span", "rec-draft-contact-val", c.value + " ↗"));
      strip.append(a);
    });
    card.append(strip);
  }

  // 4 · path — ONLY when this network actually records something about them
  const path = recDraftPathLine(d, dr);
  if (path) card.append(path);

  // 6 · evidence — citations, deduped by address AND kind: the same grant
  // listed twice is one fact, but an author record's affiliation row and its
  // works row share one URL and are two. Links already on the presence strip
  // are not repeated.
  const cites = [];
  const seen = {};
  const seenKind = {};
  (dr.evidence || []).forEach((e) => {
    const u = (e.urlOrFile || "").trim();
    const k = u + "\u0000" + (e.kind || "");
    if (u && seenKind[k]) return;
    if (u) { seen[u] = true; seenKind[k] = true; }
    cites.push(e);
  });
  const extra = (dr.links || []).map((u) => (u || "").trim())
    .filter((u, i, all) => u && !seen[u] && !contactURLs[u] && all.indexOf(u) === i);

  if (!cites.length && !extra.length) {
    // THE absence that stays loud: accepting this would put an unbacked
    // record on the board, which is the one thing the fold cannot fix
    card.append(el("div", "rec-draft-cites none", "no citations — nothing on this card is backed yet"));
  } else {
    // provenance rides the SAME line as the count, as a host — the full
    // addresses are one click away in the fold, and on hover
    const bar = el("button", "rec-draft-cites");
    bar.append(el("span", "sec-caret", open ? "▾" : "▸"));
    const trail = recDraftTrail(dr);
    const where = (trail && trail.found) || (cites.find((e) => (e.urlOrFile || "").trim()) || {}).urlOrFile || "";
    if (where) {
      const host = el("span", "rec-draft-prov", recHost(where));
      host.title = trail
        ? "found on " + trail.found + (trail.from ? "\ndiscovered from " + trail.from : "") +
          (trail.depth ? "\n" + trail.depth + " hop" + (trail.depth === "1" ? "" : "s") + " from the seed" : "")
        : where;
      bar.append(host);
    }
    const n = cites.length;
    bar.append(el("span", "rec-draft-sub", (where ? "· " : "") + (n ? n + " citation" + (n === 1 ? "" : "s") : "no citations") +
      (extra.length ? " · " + extra.length + " other link" + (extra.length === 1 ? "" : "s") : "")));
    bar.setAttribute("aria-expanded", open ? "true" : "false");
    bar.onclick = () => { recDraftOpen[key] = !open; if (recPaint) recPaint(); };
    card.append(bar);
  }
  if (open && (cites.length || extra.length)) {
    const body = el("div", "rec-draft-cite-body");
    // the crawl trail lives HERE, in full, where somebody auditing a claim
    // wants it — not on the face, where it was three wrapped lines of URL
    const trail = recDraftTrail(dr);
    if (trail) {
      const row = el("div", "rec-ev-row");
      row.append(el("span", "micro-label rec-ev-kind", "found on"));
      const a = linkEl(trail.found, trail.found);
      a.className = "rec-draft-link";
      row.append(a);
      if (trail.from) {
        row.append(el("span", "rec-ev-when", "discovered from"));
        const b = linkEl(recHost(trail.from) + " ↗", trail.from);
        b.className = "rec-draft-link";
        b.title = trail.from;
        row.append(b);
      }
      if (trail.depth) row.append(el("span", "rec-ev-when", trail.depth + " hop" + (trail.depth === "1" ? "" : "s") + " from the seed"));
      body.append(row);
    }
    cites.forEach((e) => {
      const row = el("div", "rec-ev-row");
      const et = el("div", "rec-ev-top");
      if (e.kind) et.append(el("span", "micro-label rec-ev-kind", e.kind));
      if (e.trust) et.append(el("span", "micro-label rec-ev-kind", e.trust));
      if (e.sourceId && e.sourceId !== run.source) et.append(el("span", "micro-label rec-ev-kind", "via " + e.sourceId));
      if (e.urlOrFile) {
        const a = linkEl(recHost(e.urlOrFile) + " ↗", e.urlOrFile);
        a.className = "rec-draft-link";
        a.title = e.urlOrFile;
        et.append(a);
      } else {
        et.append(el("span", "rec-ev-when", "no address — uncited"));
      }
      if (e.retrievedAt) et.append(el("span", "rec-ev-when", fmtWhen(e.retrievedAt)));
      row.append(et);
      if (e.snippet && e.snippet !== dr.note) row.append(el("blockquote", "rec-ev-quote", e.snippet));
      body.append(row);
    });
    extra.forEach((u) => {
      const a = linkEl(recHost(u) + " ↗", u);
      a.className = "rec-draft-link";
      a.title = u;
      body.append(a);
    });
    card.append(body);
  }

  // 7 · action — accept / pass / later, each scoped to THIS search
  if (d.status === "new") {
    // ⚠ a preview run accepts too (2026-09-04) — the checkbox never protected
    // anything, so the decision is no longer gated behind an identical re-run
    const acts = el("div", "rec-draft-actions");
    const accept = el("button", "pill light rec-draft-accept", "accept");
    accept.title = "add this one draft to the board for this search, citations and all — accepting is not confirming every inferred chip";
    accept.onclick = () => {
      accept.disabled = true; // one record per press: a double-click is not two accepts
      recSourcesPost("/api/aion/recruiting/sources/accept/" + run.id + "/" + d.id, {}, "candidate added from " + run.source);
    };
    const look = el("button", "pill light", d.lookedUpAt ? "look up again" : "look up");
    look.title = "ask the other public indexes (openalex, orcid, github, pubmed) about this exact name";
    look.onclick = () => {
      look.disabled = true;
      look.textContent = "looking up…";
      recDraftLookup(run, d);
    };
    const laterBtn = el("button", "pill light", "later");
    laterBtn.title = "set aside for this sitting — stays new, comes back on reload";
    laterBtn.onclick = () => { recDraftLater[key] = true; if (recPaint) recPaint(); };
    acts.append(accept, look, recDraftPass(run, d), laterBtn);
    card.append(acts);
  } else if (d.decidedAt) {
    // the reassurance ("this search only — nothing was deleted") is the
    // undo's tooltip, not a line on every settled card
    const hint = el("div", "rec-draft-hint", (d.status === "rejected" ? "passed " : d.status + " ") + fmtWhen(d.decidedAt));
    if (d.status === "rejected") hint.append(" · ", recDraftUnpass(run, d));
    card.append(hint);
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
      // one WRITE path: the same intake route the front door uses, with the
      // destination already known (you are standing in MY PEOPLE)
      host.append(ghostInput("＋ someone I know", "aion-add", async (raw) => {
        const name = raw.trim();
        if (!name) return;
        try {
          const r = await fetch("/api/aion/recruiting/intake", {
            method: "POST", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ dest: "network", name }),
          });
          if (!r.ok) throw new Error(await r.text());
          const d = await r.json();
          if (d.view) recCache = d.view;
          showToast(name + " added to your people");
          renderAion();
        } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
      }, "their name — org and email are yours to fill in"));
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
    const cell = el("span", "rec-srcfact");
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
    const r = await fetchJSONRetry("PUT", url, body || {}); // survives a deploy-window 502
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
  const right = el("span", "rec-head-right");
  // the one thing manifest cannot render is Ashby's application FORM — the
  // profile link is deterministic from the candidate id, so it costs nothing
  if (c.ashbyCandidateId) {
    const a = linkEl("view in ashby →",
      "https://app.ashbyhq.com/candidate-searches/new/right-side/candidates/" + encodeURIComponent(c.ashbyCandidateId));
    a.className = "rec-linkish";
    a.title = "the full Ashby profile — application form answers live there";
    right.append(a);
  }
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { recSel = null; if (recPaint) recPaint(); };
  right.append(x);
  head.append(right);
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

  // ONE primary action — it reads the same table the chip does. While an
  // applicant is untriaged the verdict row IS the action, so the gate
  // primary is withheld, not demoted beneath it.
  if (untriaged) {
    host.append(recTriageBlock(c));
  } else if (gate.primary) {
    const primary = el("button", "rec-primary", gate.primary.label);
    primary.onclick = () => gate.primary.run(c);
    host.append(primary);
  }

  // THE VETTING ORDER (owner ask 2026-09-04): what they submitted, then how
  // they score, then everything else. The resume renders as an open section —
  // it is the material the verdict above is actually made on, and a collapsed
  // "not pulled" row was the clutter that hid it.
  if (c.ashbyCandidateId || c.ashbyApplicationId) host.append(recSubmissionSection(c));

  // FIT — the working surface, always expanded; its head carries the gate
  // summary (the design's "FIT unscored — score to unblock a send" line)
  host.append(recFitSection(c, gate));

  // folds
  host.append(recFold("details", recDetailsMeta(c), () => recDetailsBody(c, field, text)));
  host.append(recFold("evidence", (c.evidence || []).length
    ? String((c.evidence || []).length) : { text: "● none yet", alarm: true },
    () => recEvidenceBody(c)));
  host.append(recFold("network", (c.paths || []).length
    ? (c.paths || []).length + " warm path" + ((c.paths || []).length === 1 ? "" : "s")
    : { text: "no paths", alarm: false },
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
  const bits = [];
  if (["website", "linkedin", "github"].some((k) => p[k])) bits.push("links");
  if (p.email || p.phone) bits.push("contact");
  return { text: bits.join(" · ") || "empty", alarm: false };
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
  if (c.inbound) return { text: "applied " + c.inbound, alarm: false };
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
  box.append(el("div", "micro-label rec-triage-label", "● TRIAGE · applied " + (c.inbound || "") +
    (c.ashbyStage ? " · ashby: " + c.ashbyStage : "")));

  const row = el("div", "rec-triage-row");
  const advance = el("button", "rec-primary", "advance");
  advance.title = "into the sourced pipeline at reviewing";
  advance.onclick = () => recPost("/api/aion/recruiting/candidate/stage/" + c.id,
    { stage: "reviewing" }, "advanced to reviewing");
  row.append(advance);
  const ask = el("button", "rec-quiet-btn rec-ask-btn", "ask for more");
  ask.title = "a screening question — drafted and logged like any outreach";
  ask.onclick = () => { recInspOpen.activity = true; if (recPaint) recPaint(); };
  row.append(ask);
  box.append(row);

  // archive & reject — the verdict with a consequence: it hides here AND
  // writes the rejection there, so it says so and stays arm-then-confirm.
  const linked = !!c.ashbyApplicationId && recAshbyProbe && recAshbyProbe.configured;
  if (linked) {
    // one row: the reason picker and the verdict beside it — two stacked
    // full-width controls read as two separate decisions, and are not
    const row2 = el("div", "rec-reject-row");
    const reasons = el("select", "pp-in rec-in rec-reject-reason");
    const none = el("option", "", "reject reason — choose"); none.value = ""; reasons.append(none);
    (recReasons || []).forEach((r) => {
      const o = el("option", "", r.text); o.value = r.id; reasons.append(o);
    });
    if (recReasons === null) recLoadReasons();
    row2.append(reasons);
    const rej = el("button", "rec-quiet-btn", "reject…");
    rej.title = "archive here AND write the rejection in Ashby";
    rej.onclick = () => {
      if (!reasons.value) { showToast("pick the reject reason first"); return; }
      const armed = el("button", "rec-quiet-btn armed", "confirm?");
      armed.onclick = () => recArchive(c, true, { rejectInAshby: true, archiveReasonId: reasons.value });
      rej.replaceWith(armed);
      armed.focus();
    };
    row2.append(rej);
    box.append(row2);
    // ⚠ mono meta, not body copy: a consequence note that renders at reading
    // size outweighs the buttons it qualifies (the .rec-foot class carries it)
    box.append(el("div", "rec-foot rec-archive-note",
      "reversible here, and it writes the rejection there — restoring does not un-reject"));
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
function recFitSection(c, gate) {
  const role = (recCache.roles || []).find((r) => (r.id || "role/" + r.slug) === c.role);
  const sec = el("section", "rec-insp-sec");
  const label = el("div", "aion-sec-label");
  label.append(el("span", "aion-sec-title", "fit"));
  if (gate) label.append(el("span", "rec-fold-meta" + (gate.chipCls === "blocked" ? " alarm" : ""), gate.summary));
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
  const r = await fetchJSONRetry("POST", url, body || {}); // survives a deploy-window 502
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
  const r = await fetchJSONRetry("POST", url, body || {}); // survives a deploy-window 502
  const text = await r.text();
  let out = {};
  try { out = JSON.parse(text); } catch (_) { out = { error: text }; }
  if (!r.ok && !out.proposal) throw new Error(out.error || text);
  return out;
}

// ---- SUBMISSION — what the applicant themself sent (owner ask 2026-09-04).
//
// The resume is pulled on this click, not mirrored by the sync: Ashby's
// application.list omits the file handle entirely, so mirroring would cost one
// API call per applicant per sync AND copy sixty strangers' resumes onto this
// box before anyone opened one. The bytes land in the artifacts pool; the
// record keeps the reference.
let recAshbyDetail = {};  // candidate id → the live application detail
let recResumeText = {};   // artifact hash → extracted text ("" = no text layer)
let recAshbyPulling = {}; // candidate id → a pull ran (or is running) this session

// recAutoPull fetches one candidate's submission by itself: clicking a linked
// candidate to vet them IS the request for their material, so no second click
// is asked for. Once per candidate per session; the stored artifact serves
// every later open without touching Ashby.
async function recAutoPull(c) {
  if (recAshbyPulling[c.id]) return;
  recAshbyPulling[c.id] = true;
  try {
    const out = await recAshbyCall("/api/aion/recruiting/ashby/detail/" + c.id, {});
    recAshbyDetail[c.id] = out.detail || {};
    if (out.resumeError) recAshbyDetail[c.id].resumeError = out.resumeError;
    if (out.view) recCache = out.view;
  } catch (e) {
    recAshbyDetail[c.id] = { error: String(e.message || e) };
  }
  renderAion();
}

// recResumeDisplay makes pdftotext's -layout output readable in a 300px pane.
// That flag preserves the PDF's COLUMN geometry with runs of spaces, which is
// right for an agent reading a table and wrong for a person reading a CV in a
// narrow column: it arrives as ragged indentation. So for DISPLAY only —
// the stored extract stays verbatim — leading indentation goes, column
// gutters collapse to a single space, and runs of blank lines become one.
function recResumeDisplay(raw) {
  const lines = (raw || "").split("\n").map((ln) =>
    ln.replace(/\s+$/, "").replace(/^\s+/, "").replace(/ {2,}/g, "  "));
  const out = [];
  for (const ln of lines) {
    if (!ln && !out.length) continue;              // no leading blank block
    if (!ln && !out[out.length - 1]) continue;     // never two blanks in a row
    out.push(ln);
  }
  while (out.length && !out[out.length - 1]) out.pop();
  return out.join("\n");
}

async function recLoadResumeText(hash) {
  recResumeText[hash] = null; // in flight — never re-request on the next paint
  try {
    const d = await (await fetch("/api/aion/recruiting/resume/" + hash + "/text")).json();
    recResumeText[hash] = d.hasText ? (d.text || "") : "";
  } catch (e) { recResumeText[hash] = ""; }
  if (recPaint) recPaint();
}

function recSubmissionSection(c) {
  const sec = el("section", "rec-insp-sec");
  const r = c.resume || {};
  const det = recAshbyDetail[c.id];

  const label = el("div", "aion-sec-label");
  label.append(el("span", "aion-sec-title", "resume"));
  if (det && (det.jobTitle || det.appliedAt)) {
    label.append(el("span", "rec-fold-meta",
      (det.jobTitle ? "→ " + det.jobTitle : "") + (det.appliedAt ? " · " + fmtWhen(det.appliedAt) : "")));
  }
  const re = el("button", "rec-linkish rec-repull", "↻");
  re.title = "re-pull this application from Ashby";
  re.onclick = () => { delete recAshbyDetail[c.id]; delete recAshbyPulling[c.id]; recAutoPull(c); };
  label.append(re);
  sec.append(label);

  const box = el("div", "rec-submission");
  sec.append(box);

  if (!r.hash && !det) {
    recAutoPull(c);
    box.append(el("div", "rec-foot", "pulling from ashby…"));
    return sec;
  }
  if (det && det.error) box.append(el("div", "rec-foot", "couldn't read ashby — " + det.error.slice(0, 120)));
  if (det && det.resumeError) box.append(el("div", "rec-foot", "resume: " + String(det.resumeError).slice(0, 120)));

  if (r.hash) {
    const row = el("div", "rec-sub-file");
    row.append(el("span", "rec-sub-name", r.name || "resume"));
    const open = linkEl("open →", "/api/aion/recruiting/resume/" + r.hash);
    open.className = "rec-linkish";
    const dl = linkEl("download", "/api/aion/recruiting/resume/" + r.hash + "?dl=1");
    dl.className = "rec-linkish";
    row.append(open, dl);
    box.append(row);

    const txt = recResumeText[r.hash];
    if (txt === undefined) { recLoadResumeText(r.hash); box.append(el("div", "rec-foot", "reading…")); }
    else if (txt === null) box.append(el("div", "rec-foot", "reading…"));
    else if (txt) box.append(el("pre", "rec-resume-text", recResumeDisplay(txt)));
    else box.append(el("div", "rec-foot", "no text layer — a scanned PDF; open it to read"));
  } else if (det && !det.error) {
    box.append(el("div", "rec-foot", "no file on this application"));
  }

  const fields = (det && det.fields) || [];
  fields.forEach((f) => {
    const row = el("div", "rec-sub-field");
    row.append(el("span", "rec-sub-flabel", f.title || "field"));
    row.append(el("span", "rec-sub-fval", f.value || ""));
    box.append(row);
  });
  // ⚠ an empty answers lane must not read as "this applicant answered
  // nothing" — the form answers live in Ashby, one link away
  if (det && !det.error && !fields.length && c.ashbyCandidateId) {
    const note = el("div", "rec-foot");
    note.append(document.createTextNode("form answers aren't exposed by Ashby's API — "));
    const a = linkEl("read them in ashby →",
      "https://app.ashbyhq.com/candidate-searches/new/right-side/candidates/" + encodeURIComponent(c.ashbyCandidateId));
    a.className = "rec-linkish";
    note.append(a);
    box.append(note);
  }
  return sec;
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
      ((s.archived || []).length ? " · " + s.archived.length + " archived ← ashby" : "") +
      // a live applicant nobody can see is worth a sentence, not a silence
      (Object.keys(s.skippedJobs || {}).length
        ? " · skipped " + Object.entries(s.skippedJobs).map(([j, n]) => n + " for " + j + " (no role)").join(", ")
        : "") +
      ((s.updated || []).length ? " · " + s.updated.length + " updated" : "") +
      ((s.conflicts || []).length ? " · " + s.conflicts.length + " conflicts" : ""));
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140), null, "error"); }
}
