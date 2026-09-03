// ---- AION / RECRUITING ----
// The private scout board over system/aion/recruiting/. It fetches routes
// outside the AionLive contract that portal.aion.bio renders, and it is
// deliberately never mounted on the portal listener: these records carry
// candidate PII.
//
// Shape follows 94-aion-fundraising.js exactly — module-level state, an async
// loader with {cache: "no-store"}, an entry that paints "loading…" then
// re-renders, and an in-place paint() for filter/search so a render never
// replaces the focused input node.
let recCache = null;      // {roles, candidates, seeds, network, stages, …}
let recRole = null;       // selected role slug, null = all lanes
let recStage = "active";  // stage filter chip
let recSel = null;        // inspector selection (candidate id)
let recQuery = "";        // search box
let recSeedsOpen = false; // seeds rail expanded

// sources / scout runs (Phase 3a) — a run is a cache of a search, never a
// record, so its state lives beside the board's rather than inside it
let recSources = null;      // {sources, defaultMax, maxMax, ttlDays} | {unavailable: true}
let recRuns = [];           // every run, newest first, each with its draft queue
let recSourcesOpen = false; // sources panel expanded
let recRunOpen = {};        // run id → queue expanded
let recRunning = false;     // a run is in flight
// the form survives a re-render: every action repaints the whole tab, and a
// half-typed query must not vanish because a draft was rejected
const recRunForm = { source: "manual", role: "", query: "", max: "", dryRun: true, fields: {} };
// scope fields every adapter shares; anything else an adapter declares is
// rendered generically from its metadata and sent as `fields`
const REC_RUN_COMMON_FIELDS = ["role", "query", "max"];

async function loadRecruiting() {
  try {
    const r = await fetch("/api/aion/recruiting", { cache: "no-store" });
    if (!r.ok) throw new Error(await r.text());
    recCache = await r.json();
  } catch (_) {
    recCache = { roles: [], candidates: [], seeds: [], network: { people: [], edges: [] }, stages: [] };
  }
  await loadRecruitingSources();
}

// The sources panel is its own fetch: a board whose run cache is not wired
// (or fails to read) still paints, and the panel says so quietly instead of
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

// recSourcesPost is recPost for the sources routes: every one answers with
// the run list, and only a route that wrote a record (accept) adds the board
// view — so the board updates exactly when something reached it.
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
  } catch (e) { showToast(String(e.message || e).slice(0, 140)); return null; }
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
  } catch (e) { showToast(String(e.message || e).slice(0, 140)); }
}

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

  // toolbar — search + stage chips filter IN PLACE, so the caret stays in the
  // search field between keystrokes
  const bar = el("div", "rec-toolbar");
  const search = el("input", "pp-in rec-search");
  search.type = "search";
  search.placeholder = "search names, orgs, evidence…";
  search.value = recQuery;
  search.oninput = () => { recQuery = search.value; paint(); };
  bar.append(search);
  const chips = {};
  [["active", "ACTIVE"], ["all", "ALL"], ["shortlist", "SHORTLIST"], ["archived", "ARCHIVED"]]
    .forEach(([key, label]) => {
      const b = el("button", "filter-chip", label);
      b.onclick = () => { recStage = key; paint(); };
      chips[key] = b;
      bar.append(b);
    });
  main.append(bar);

  main.append(ghostInput("＋ candidate url, name, or note", "aion-add rec-add", (text) =>
    recPost("/api/aion/recruiting/candidate", { text, role: recRoleId() }, "candidate added")));

  main.append(paintSources());

  const board = el("div", "rec-board");
  main.append(board);

  const paint = () => {
    Object.keys(chips).forEach((k) => chips[k].classList.toggle("on", recStage === k));
    paintRail(rail);
    paintBoard(board);
    // on a phone the aside is display:none (92-aion.css), so the inspector
    // goes into the bottom sheet — the same route fundraising takes
    if (window.mf && window.mf.phone()) {
      if (recSel) {
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
  paint();
}

// recRoleId is the role a new candidate is filed under: the selected lane, or
// nothing when the board is showing every lane.
function recRoleId() {
  if (!recRole) return "";
  const role = (recCache.roles || []).find((r) => r.slug === recRole);
  return role ? role.id || "role/" + role.slug : "";
}

// ---- roles rail ----

function paintRail(rail) {
  rail.innerHTML = "";
  const roles = recCache.roles || [];
  const all = el("button", "rec-role" + (recRole ? "" : " on"));
  all.append(el("span", "rec-role-name", "all roles"));
  all.append(el("span", "rec-role-count", String(roles.reduce((n, r) => n + (r.openCount || 0), 0))));
  all.onclick = () => { recRole = null; renderAion(); };
  rail.append(all);

  roles.forEach((role) => {
    const b = el("button", "rec-role" + (recRole === role.slug ? " on" : ""));
    b.append(el("span", "rec-role-name", role.title || role.slug));
    // the count comes from the SAME predicate the board filters on — one
    // derivation, so the badge and the page cannot disagree
    b.append(el("span", "rec-role-count", String(role.openCount || 0)));
    b.onclick = () => { recRole = recRole === role.slug ? null : role.slug; renderAion(); };
    rail.append(b);
  });
  if (!roles.length) rail.append(emptyRow("no roles yet"));

  rail.append(paintRoleSync(roles));
  rail.append(paintSeeds());
}

// ---- public Ashby role mirror (Phase 2) ----
// One button, one explicit user action: mirror the public AION job board
// onto the role records. No key, no poller, nothing written toward Ashby.
// The server owns which fields move; `## criteria` is never touched.
let recSyncing = false;

function paintRoleSync(roles) {
  const box = el("div", "rec-sync");
  const b = el("button", "pill light rec-sync-btn", recSyncing ? "syncing…" : "sync roles");
  b.disabled = recSyncing;
  b.title = "mirror the public Ashby job board onto the role records";
  b.onclick = () => recSyncRoles();
  box.append(b);
  const synced = roles.map((r) => r.synced || "").filter(Boolean).sort().pop();
  if (synced) box.append(el("span", "micro-label rec-sync-when", "ashby · " + synced));
  if (recAshbyProbe && recAshbyProbe.configured && !recAshbyProbe.error) {
    const sb = el("button", "pill light rec-sync-btn", "sync back");
    sb.title = "pull Ashby-owned state (job ids, official stages) onto linked records — a user action, never a poller";
    sb.onclick = () => recAshbySyncBack(false);
    box.append(sb);
  }
  return box;
}

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
    showToast(String(e.message || e).slice(0, 140));
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
  head.onclick = () => { recSeedsOpen = !recSeedsOpen; renderAion(); };
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

  // a seed is typed as `class: name` so all four D11 classes share one input
  box.append(ghostInput("＋ seed: person, company, lab, work, or repo", "aion-add rec-seed-add", (raw) => {
    const at = raw.indexOf(":");
    const cls = at > 0 ? raw.slice(0, at).trim().toLowerCase() : "";
    const name = at > 0 ? raw.slice(at + 1).trim() : raw.trim();
    // ghostInput has already settled the input, so a refused entry must
    // repaint or the dead input stays on screen
    if (!cls) { showToast("prefix the class, e.g. lab: WashU BME"); renderAion(); return; }
    return recPost("/api/aion/recruiting/seed", { class: cls, name }, "seed added");
  }));
  return box;
}

// ---- sources / scout runs (Phase 3a) ----
// One adapter over one explicit scope → a draft queue in dataDir, reviewed
// one draft at a time. Dry run is checked by default and the server reads
// an absent flag the same way. There is no accept-all button because there
// is no accept-all route: each accept names ONE draft, and only that answer
// carries a board view.

function recRunPending(run) {
  if ((run.scope || {}).dryRun) return 0;
  return (run.drafts || []).filter((d) => d.status === "new").length;
}

function paintSources() {
  const box = el("section", "rec-sources");
  const head = el("button", "rec-sources-head");
  head.append(el("span", "sec-caret", recSourcesOpen ? "▾" : "▸"));
  head.append(el("span", "micro-label", "SOURCES"));
  const pending = recRuns.reduce((n, r) => n + recRunPending(r), 0);
  let count = recRuns.length + " run" + (recRuns.length === 1 ? "" : "s");
  if (pending) count += " · " + pending + " to review";
  if (recSources && recSources.unavailable) count = "unavailable";
  head.append(el("span", "rec-role-count", count));
  head.onclick = () => { recSourcesOpen = !recSourcesOpen; renderAion(); };
  box.append(head);
  if (!recSourcesOpen) return box;

  if (!recSources || recSources.unavailable) {
    box.append(emptyRow("sources unavailable"));
    return box;
  }
  box.append(recRunFormEl());
  const list = el("div", "rec-run-list");
  recRuns.forEach((run) => list.append(recRunCard(run)));
  if (!recRuns.length) list.append(emptyRow("no runs yet — a run is a dry run until you say otherwise"));
  box.append(list);
  return box;
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

  // adapter-specific scope fields (e.g. web: seed_url / max_pages / depth),
  // one text input each, keyed by the adapter's own field key
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
  if (!query) { showToast("a run needs a query"); return; }
  const adapter = ((recSources || {}).sources || []).find((a) => a.id === recRunForm.source) || {};
  const extras = (adapter.fields || []).filter((f) => f.key && !REC_RUN_COMMON_FIELDS.includes(f.key));
  const fields = {};
  for (const f of extras) {
    const v = (recRunForm.fields[f.key] || "").trim();
    if (v) fields[f.key] = v;
    else if (f.required) { showToast("a " + recRunForm.source + " run needs " + (f.label || f.key)); return; }
  }
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
    showToast(String(e.message || e).slice(0, 140));
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
  toggle.append(el("span", "rec-run-scope", scope.query || ""));
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

  // a duplicate or an accepted draft points at the record it is — one click
  // opens it in the inspector
  if (d.candidateId) {
    const on = el("button", "rec-draft-on", "on the board as " + d.candidateId + (d.reason ? " · " + d.reason : ""));
    on.onclick = () => { recSel = d.candidateId; renderAion(); };
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

// ---- candidate board ----

function recVisible(c) {
  if (recRole) {
    const role = (recCache.roles || []).find((r) => r.slug === recRole);
    const want = role ? role.id || "role/" + role.slug : recRole;
    if (c.role !== want) return false;
  }
  const archived = c.stage === "archived";
  if (recStage === "active" && archived) return false;
  if (recStage === "all" && archived) return false;
  if (recStage === "archived" && !archived) return false;
  if (recStage === "shortlist" && c.stage !== "shortlist") return false;
  const q = recQuery.trim().toLowerCase();
  if (!q) return true;
  const p = c.profile || {};
  return [c.name, c.stage, p.title, p.org, p.location]
    .concat((c.evidence || []).map((e) => (e.snippet || "") + " " + (e.url || "")))
    .join(" ").toLowerCase().includes(q);
}

function paintBoard(board) {
  board.innerHTML = "";
  const rows = (recCache.candidates || []).filter(recVisible);
  if (!rows.length) { board.append(emptyRow("no candidates match.")); return; }
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

function recCard(c) {
  const p = c.profile || {};
  const sub = [p.title, p.org, p.location].filter(Boolean).join(" · ");
  const card = cardShell({
    kind: "rec-card" + (recSel === c.id ? " sel" : ""),
    chips: [recGateChip(c), c.evidence && c.evidence.length ? el("span", "micro-label rec-ev", c.evidence.length + " EV") : null],
    title: c.name,
    // created is a date-only string; through fmtWhen it would parse as UTC
    // midnight and print the day before west of Greenwich, so it goes raw
    date: c.created ? el("span", "feed-date", c.created) : null,
    meta: sub || null,
  });
  card.onclick = () => { recSel = recSel === c.id ? null : c.id; renderAion(); };
  return card;
}

// The gate chip renders the D6 state and says WHICH musts are unevidenced —
// a chip that only said "blocked" would send the owner back to the record to
// find out why.
function recGateChip(c) {
  const g = c.gate || {};
  let label = "unscored";
  let cls = "rec-gate";
  if (g.passed && g.overridden) { label = "overridden"; cls += " overridden"; }
  else if (g.passed) { label = "gate ok"; cls += " ok"; }
  else if (g.disqualifiers && g.disqualifiers.length) { label = "disqualified"; cls += " blocked"; }
  else if (g.musts) { label = (g.satisfied || 0) + "/" + g.musts + " musts"; }
  const chip = el("span", "micro-label " + cls, label);
  if (g.reason) chip.title = g.reason;
  return chip;
}

// ---- inspector ----
// Sections in the order the card has to answer: fit + evidence → network
// paths → next action → outreach → ashby state.

function paintInspector(host) {
  host.innerHTML = "";
  const c = (recCache.candidates || []).find((x) => x.id === recSel);
  if (!c) {
    host.append(el("div", "aion-insp-empty", "select a candidate — edits save as you go"));
    return;
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Candidate"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { recSel = null; renderAion(); };
  head.append(x);
  host.append(head);

  const patch = (set) => recPost("/api/aion/recruiting/candidate/update/" + c.id, set);
  const field = (label, node) => {
    const f = el("div", "aion-insp-field rec-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    host.append(f);
    return node;
  };
  const text = (label, key, value) => {
    const n = el("input", "pp-in rec-in");
    n.type = "text";
    n.value = value || "";
    const old = n.value;
    n.onblur = () => { if (n.value !== old) patch({ [key]: n.value }); };
    n.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); n.blur(); } };
    return field(label, n);
  };

  const p = c.profile || {};
  text("name", "name", c.name);
  const role = el("select", "pp-in rec-in");
  const none = el("option", "", "—"); none.value = ""; role.append(none);
  (recCache.roles || []).forEach((r) => {
    const id = r.id || "role/" + r.slug;
    const o = el("option", "", r.title || r.slug);
    o.value = id;
    o.selected = c.role === id;
    role.append(o);
  });
  role.onchange = () => patch({ role: role.value });
  field("role", role);

  const stage = el("select", "pp-in rec-in");
  (recCache.stages || []).filter((st) => st !== "archived").forEach((st) => {
    const o = el("option", "", st); o.value = st; o.selected = c.stage === st; stage.append(o);
  });
  stage.disabled = c.stage === "archived";
  stage.onchange = () => recPost("/api/aion/recruiting/candidate/stage/" + c.id, { stage: stage.value }, "stage saved");
  field("stage", stage);

  text("title", "title", p.title);
  text("org", "org", p.org);
  text("location", "location", p.location);
  text("website", "website", p.website);
  text("linkedin", "linkedin", p.linkedin);
  text("github", "github", p.github);
  // contact is manual-only (D15): nothing in this app ever fills these in
  text("email", "email", p.email);
  text("phone", "phone", p.phone);

  host.append(recFitSection(c));
  host.append(recEvidenceSection(c));
  host.append(recNetworkSection(c));
  host.append(recNextSection(c));
  host.append(recOutreachSection(c));
  host.append(recAshbySection(c));

  const archive = el("button", "aion-insp-del rec-archive",
    c.stage === "archived" ? "restore candidate" : "archive");
  archive.onclick = () => {
    if (c.stage === "archived") {
      recPost("/api/aion/recruiting/candidate/archive/" + c.id, { archived: false }, "candidate restored");
      return;
    }
    // archiving retains the record but takes it off the board — arm, then
    // confirm (native confirm() is banned app-wide)
    const armed = el("button", "aion-insp-del rec-archive armed", "confirm archive?");
    armed.onclick = () => recPost("/api/aion/recruiting/candidate/archive/" + c.id, { archived: true }, "candidate archived");
    archive.replaceWith(armed);
    armed.focus();
  };
  host.append(archive);
}

function recSection(title, summary) {
  const sec = el("section", "rec-insp-sec");
  const head = el("div", "aion-sec-label");
  head.append(el("span", "aion-sec-title", title));
  if (summary) head.append(el("span", "aion-sec-count", summary));
  sec.append(head);
  return sec;
}

// fit — the role's criteria, each scored against this candidate, with the
// evidence ids that back the score.
function recFitSection(c) {
  const role = (recCache.roles || []).find((r) => (r.id || "role/" + r.slug) === c.role);
  const g = c.gate || {};
  const sec = recSection("fit", g.reason || "");
  if (!role) { sec.append(emptyRow("tether a role to score fit")); return sec; }
  if (!(role.criteria || []).length) { sec.append(emptyRow("this role has no criteria yet")); return sec; }

  const byCriterion = {};
  (c.fit || []).forEach((f) => { byCriterion[(f.criterion || "").trim().toLowerCase()] = f; });

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
    const score = el("select", "pp-in rec-score");
    ["unknown", "0", "1", "2", "3", "4", "5"].forEach((v) => {
      const o = el("option", "", v); o.value = v;
      o.selected = (have.score || "unknown") === v;
      score.append(o);
    });
    const ev = el("input", "pp-in rec-ev-in");
    ev.type = "text";
    ev.placeholder = "ev1, ev2";
    ev.value = (have.evidence || []).join(", ");
    const commit = () => recPost("/api/aion/recruiting/candidate/fit/" + c.id, {
      criterion: crit.criterion,
      score: score.value,
      evidence: splitList(ev.value),
    }, "fit saved");
    score.onchange = commit;
    ev.onblur = () => { if ((have.evidence || []).join(", ") !== ev.value) commit(); };
    ev.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); ev.blur(); } };
    row.append(score, ev);
    sec.append(row);
  });

  // the recorded override (D6) — never silent: it carries a reason and a date
  const ov = c.override || {};
  if (ov.by) {
    const line = el("div", "rec-override");
    line.append(el("span", "", "overridden by " + ov.by + (ov.at ? " · " + ov.at : "")));
    if (ov.reason) line.append(el("span", "rec-override-why", ov.reason));
    const clear = el("button", "pill light", "clear override");
    clear.onclick = () => recPost("/api/aion/recruiting/candidate/override/" + c.id, { by: "", reason: "" }, "override cleared");
    line.append(clear);
    sec.append(line);
  } else if (!g.passed) {
    const b = el("button", "pill light", "override gate");
    b.onclick = () => askText("override the fit gate", "why is this candidate through anyway?", (reason) => {
      if (!reason.trim()) { showToast("an override needs a reason"); return; }
      recPost("/api/aion/recruiting/candidate/override/" + c.id,
        { by: recCache.owner || "benjamin", reason: reason.trim() }, "override recorded");
    });
    sec.append(b);
  }
  return sec;
}

// evidence — the citations. A URL, a quote and a date, kept verbatim, because
// a citation is what outlives every cache and every adapter.
function recEvidenceSection(c) {
  const sec = recSection("evidence", String((c.evidence || []).length));
  (c.evidence || []).forEach((e) => {
    const row = el("div", "rec-ev-row");
    const top = el("div", "rec-ev-top");
    top.append(el("span", "micro-label", e.id));
    if (e.kind) top.append(el("span", "micro-label rec-ev-kind", e.kind));
    if (e.url) { const a = linkEl(e.url, e.url); a.className = "rec-ev-url"; top.append(a); }
    if (e.collected) top.append(el("span", "rec-ev-when", e.collected));
    row.append(top);
    if (e.snippet) row.append(el("blockquote", "rec-ev-quote", e.snippet));
    sec.append(row);
  });
  if (!(c.evidence || []).length) sec.append(emptyRow("no evidence yet"));

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
  sec.append(form);
  return sec;
}

// network — intro paths, with inferred edges labelled at the PATH level too,
// never presented as a real relationship.
//
// Three kinds of row, each labelled in words so none reads as a confirmed
// relationship it isn't:
//   hand-authored        owner evidence from `## network`, shown as written;
//   derived              a route the server computed over the graph
//                        (recruiting.PathKindDerived) — the owner has not
//                        confirmed it;
//   derived + inferred   a computed route that crosses at least one inferred
//                        edge — weakest of the three, both chips shown.
const REC_PATH_KIND_DERIVED = "derived"; // mirrors recruiting.PathKindDerived
function recNetworkSection(c) {
  const paths = c.paths || [];
  const sec = recSection("network", String(paths.length));
  paths.forEach((p) => {
    const derived = p.kind === REC_PATH_KIND_DERIVED;
    const row = el("div", "rec-path" + (derived ? " derived" : ""));
    row.append(el("span", "rec-path-hops", p.path));
    if (derived) {
      const chip = el("span", "micro-label rec-derived", "derived");
      chip.title = p.inferred
        ? "computed from the network graph over at least one inferred edge — not owner-confirmed"
        : "computed from the network graph — not owner-confirmed";
      row.append(chip);
    } else if (p.kind) {
      row.append(el("span", "micro-label", p.kind));
    }
    if (p.confidence) row.append(el("span", "rec-path-conf", p.confidence));
    if (p.inferred) row.append(el("span", "micro-label rec-inferred", "inferred"));
    sec.append(row);
  });
  const edges = ((recCache.network || {}).edges || []).filter((e) => e.to === c.id || e.from === c.id);
  edges.forEach((e) => {
    const row = el("div", "rec-edge");
    row.append(el("span", "rec-edge-ends", e.from + " → " + e.to));
    row.append(el("span", "micro-label", e.kind));
    if (e.basis) row.append(el("span", "rec-edge-basis", e.basis));
    if (e.confidence) row.append(el("span", "rec-path-conf", e.confidence));
    if (e.inferred) row.append(el("span", "micro-label rec-inferred", "inferred"));
    sec.append(row);
  });
  if (!paths.length && !edges.length) sec.append(emptyRow("no sourced paths yet"));
  return sec;
}

function recNextSection(c) {
  const sec = recSection("next", "");
  (c.next || []).forEach((n) => {
    const row = el("div", "rec-next");
    row.append(el("span", "", n.action));
    if (n.due) row.append(el("span", "rec-ev-when", "due " + n.due));
    if (n.owner) row.append(el("span", "micro-label", n.owner));
    sec.append(row);
  });
  if (!(c.next || []).length) sec.append(emptyRow("no next action"));
  return sec;
}

// outreach and ashby are read-only placeholders in this phase: drafting and
// sending are approval-gated (D5) and the private Ashby client is later. The
// sections exist so the inspector's order is the one the record answers in.
function recOutreachSection(c) {
  const sec = recSection("outreach", "");
  (c.outreach || []).forEach((o) => {
    const row = el("div", "rec-next");
    row.append(el("span", "", o.log));
    if (o.status) row.append(el("span", "micro-label", o.status));
    if (o.last) row.append(el("span", "rec-ev-when", o.last));
    sec.append(row);
  });
  if (!(c.outreach || []).length) sec.append(emptyRow("no outreach yet"));
  return sec;
}

// ---- private Ashby handoff (Phase 6) ----
// The approved write path, in the inspector: preflight renders the proposal
// (matches → link vs create; the diff; conflicts), the owner picks the
// handoff (project vs application — never preset) and approves, then the
// push runs and the record carries the returned ids. Without a key the
// probe says so and the buttons stay away.
let recAshbyProbe = null;   // {configured, scopes, error} once fetched
let recAshbyProposal = {};  // candidate id → last rendered proposal
let recAshbyChoice = {};    // candidate id → {handoff, decision, ashbyCandidateId, note, includeContact}

async function recAshbyLoadProbe() {
  try {
    const r = await fetch("/api/aion/recruiting/ashby/probe", { cache: "no-store" });
    recAshbyProbe = r.ok ? await r.json() : { configured: false, scopes: [], error: await r.text() };
  } catch (e) { recAshbyProbe = { configured: false, scopes: [], error: String(e.message || e) }; }
  if (aionMode === "recruiting") renderAion();
}

async function recAshbyCall(url, body) {
  const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
  const text = await r.text();
  let out = {};
  try { out = JSON.parse(text); } catch (_) { out = { error: text }; }
  if (!r.ok && !out.proposal) throw new Error(out.error || text);
  return out;
}

function recAshbySection(c) {
  const sec = recSection("ashby", recAshbyProbe && recAshbyProbe.configured ? "key installed" : "");
  if (c.ashbyCandidateId) sec.append(el("div", "rec-next", "candidate " + c.ashbyCandidateId));
  if (c.ashbyApplicationId) sec.append(el("div", "rec-next", "application " + c.ashbyApplicationId));
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

  // the explicit per-candidate handoff choice — no default
  const handoff = el("select", "pp-in rec-in");
  [["", "handoff — choose"], ["project", "sourcing project"], ["application", "formal application"]].forEach(([v, label]) => {
    const o = el("option", "", label); o.value = v; o.selected = choice.handoff === v; handoff.append(o);
  });
  handoff.onchange = () => { choice.handoff = handoff.value; };
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
    } catch (e) { showToast(String(e.message || e).slice(0, 140)); }
  };
  sec.append(preflight);

  if (!prop) return sec;

  // matches → link vs create, explicit
  if ((prop.matches || []).length && !prop.linked) {
    sec.append(el("div", "rec-next", "found in Ashby:"));
    prop.matches.forEach((m) => {
      const row = el("div", "rec-next");
      row.append(el("span", "", m.name + (m.primaryEmail ? " · " + m.primaryEmail : "")), el("span", "micro-label", m.id));
      sec.append(row);
    });
    const decision = el("select", "pp-in rec-in");
    [["", "decision — choose"], ["link", "link to the found candidate"], ["create", "create anyway (namesake)"]].forEach(([v, label]) => {
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

  // the diff
  (prop.diff || []).forEach((d) => {
    if (d.action === "keep" || d.action === "skip") return;
    const row = el("div", "rec-next");
    // a conflict chip borrows the gate's blocked recipe (danger border +
    // text); rec-archive is the block-level button, not a chip
    row.append(el("span", "micro-label" + (d.action === "conflict" ? " rec-gate blocked" : ""), d.action));
    row.append(el("span", "", d.field + ": " + (d.manifest || "—") + (d.ashby ? " ⇄ " + d.ashby : "")));
    sec.append(row);
  });
  const writes = el("div", "rec-next", "would call: " + (prop.writes || []).join(" → "));
  sec.append(writes);
  if (prop.conflict) { sec.append(emptyRow("both sides changed — resolve before pushing")); return sec; }
  if ((prop.needsChoice || []).length) { sec.append(emptyRow("choose: " + prop.needsChoice.join(", "))); }

  // approval, armed then confirmed (native confirm() is banned app-wide)
  const approve = el("button", "aion-insp-del rec-archive", "approve & push to Ashby");
  approve.onclick = () => {
    const armed = el("button", "aion-insp-del rec-archive armed", "confirm push?");
    armed.onclick = async () => {
      // the push is not idempotent (candidate.create → project/application →
      // note), so the confirmed button goes inert for the whole round-trip
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
        showToast(String(e.message || e).slice(0, 140));
        // a failed push leaves the proposal on screen; re-arm so the owner
        // can retry after reading the toast
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
// onto linked records — job ids, official stages, base snapshots.
async function recAshbySyncBack(full) {
  try {
    const out = await recAshbyCall("/api/aion/recruiting/ashby/sync", { full: !!full });
    if (out.view) recCache = out.view;
    const s = out.sync || {};
    showToast("ashby sync: " + (s.candidates || 0) + " candidates · " + (s.applications || 0) + " applications" +
      ((s.updated || []).length ? " · " + s.updated.length + " updated" : "") +
      ((s.conflicts || []).length ? " · " + s.conflicts.length + " conflicts" : ""));
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140)); }
}
