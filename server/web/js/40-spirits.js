// ================= Agents panel =================
// (small DOM helpers live in 05-components.js — the §11 component library)

// ---- SPIRITS: the excalibur harness console ----
// Purely the engine console: RUNS · RITUALS — checking + instigating runs.
// The feed (incl. the approvals inbox) lives one level up as its own tab; the
// ENGINE owns execution — the only write toward it is a spooled run-now request.
let spiritStatusCache = null;
let spiritRuns = { data: [], queued: [] }; // last poll of /api/spirits/runs — the ONLY run state; nothing else is held
let openRunId = null;                       // which run's report detail is expanded (for live body refresh)

// SPIRITS.md §1: a top tab-bar over one body — ALL RITUALS · RUNS · SETTINGS —
// and the spirit as the rail object: #/spirits/<name> is a spirit page,
// #/spirits/ritual/<spirit>/<name> the ritual editor. Legacy tails fold in.
let spMode = "rituals"; // rituals | runs | settings | spirit | editor
let spSpirit = "";      // the open spirit (spirit/editor modes)
let spRitualPath = "";  // the open ritual file (editor mode)
let spSettingsTab = "portals"; // settings inner-rail selection

function showSpirits(h) {
  const tail = h && h.startsWith("#/spirits/") ? decodeURIComponent(h.slice("#/spirits/".length)) : "";
  if (tail === "portals") { spSettingsTab = "portals"; location.hash = "#/spirits/settings"; return; } // legacy deep-link
  spSpirit = ""; spRitualPath = "";
  if (tail === "") spMode = "rituals";
  else if (tail === "runs") spMode = "runs";
  else if (tail === "settings") spMode = "settings";
  else if (tail.startsWith("ritual/")) {
    const rest = tail.slice("ritual/".length);
    const i = rest.indexOf("/");
    if (i <= 0) { location.hash = "#/spirits"; return; }
    spMode = "editor";
    spSpirit = rest.slice(0, i);
    spRitualPath = "spirits/" + spSpirit + "/rituals/" + rest.slice(i + 1) + ".md";
  } else { spMode = "spirit"; spSpirit = tail; }
  renderSpirits();
}

// renderSpToggle — chip-active mirror of renderReToggle: spirit page and the
// ritual editor keep ALL RITUALS lit (they open from it).
function renderSpToggle() {
  const active = (spMode === "spirit" || spMode === "editor") ? "rituals" : spMode;
  const tog = document.getElementById("spToggle");
  if (tog) tog.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === active));
}

function renderSpirits() {
  renderSpToggle();
  const show = (id, on) => { const n = document.getElementById(id); if (n) n.hidden = !on; };
  show("spRitualsWrap", spMode === "rituals");
  show("spEditorWrap", spMode === "editor");
  show("spSpiritWrap", spMode === "spirit");
  show("spRunsWrap", spMode === "runs");
  show("spSettingsWrap", spMode === "settings");
  if (typeof closeEditor === "function") closeEditor(); // no stale raw drawer under another view (renderers reopen it deliberately)
  loadSpiritsStatus();
  ensureLivePoll(); // resume watching queued/running runs, derived from files
  loadPortalsBadge();
  if (spMode === "rituals") { loadSpiritRituals(); loadSpiritRuns(); }
  else if (spMode === "runs") { loadSpiritRuns(); }
  else if (spMode === "settings") { renderSpiritSettings(); }
  else if (spMode === "spirit") { renderSpiritPage(spSpirit); }
  else if (spMode === "editor") { renderRitualEditor(spRitualPath); }
}

// The spirit index strip over the ALL RITUALS board: one quiet row per spirit
// (name · N rituals — count derived from the board rows), click → the spirit
// page, `＋ spirit` at the end (SPIRITS.md §1's "SPIRITS" group, chips-era shape).
function renderSpiritIndex() {
  const host = document.getElementById("spiritIndex");
  if (!host) return;
  host.innerHTML = "";
  const counts = {};
  (spiritRitualRows || []).forEach((r) => { counts[r.spirit] = (counts[r.spirit] || 0) + 1; });
  const names = [...new Set([
    ...Object.keys(counts),
    ...Object.keys((spiritStatusCache && spiritStatusCache.spirits) || {}),
  ])].sort();
  names.forEach((name) => {
    const b = el("button", "spirit-index-item");
    b.append(el("span", "spirit-index-name", name));
    b.append(el("span", "spirit-index-count", String(counts[name] || 0)));
    b.onclick = () => { location.hash = "#/spirits/" + encodeURIComponent(name); };
    host.append(b);
  });
  const add = el("button", "sprt-ghost", "＋ spirit");
  add.onclick = () => newSpirit();
  host.append(add);
}

// ---- PORTALS sub-tab: every external realm, (re)connectable in place ----
// The one place a connection is seen and repaired. Api-key portals (clickup,
// benchling) take a pasted key → save → auto-test; the oauth portal (calendar)
// runs its existing sign-in; the engine's LLM conduits are read-only. This is
// also the seed of app-wide settings — the row renderer is generic over the
// server's portal definition (fields drive the form), so github/docusign appear
// later as pure data.
async function loadPortals() {
  const host = document.getElementById("portalList"); if (!host) return;
  if (!host.children.length) host.textContent = "loading…";
  try {
    const rows = (await (await fetch("/api/portals")).json()).rows || [];
    renderPortals(rows);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Portals unavailable.")); }
}

// The SETTINGS chip's degraded count (`n ●`) — derived from the last portal
// rows on every fetch/action, never stored.
let spPortalRows = []; // last /api/portals fetch (badge + settings rail count)
function updateSettingsBadge() {
  const badge = document.getElementById("spSettingsBadge");
  const degraded = spPortalRows.filter((p) => p.state === "degraded").length;
  if (badge) {
    badge.hidden = !degraded;
    badge.textContent = degraded ? degraded + " ●" : "";
  }
}
async function loadPortalsBadge() {
  try { spPortalRows = (await (await fetch("/api/portals")).json()).rows || []; } catch (e) { return; }
  updateSettingsBadge();
}

// ---- HARNESSES settings: each federated tree's engine + which conduit each
// spirit routes to, switchable in place. Renders into the Settings pane. ----
async function loadHarnesses() {
  const board = document.getElementById("harnessBoard");
  if (!board) return;
  let harnesses = [];
  try { harnesses = (await (await fetch("/api/harnesses")).json()).harnesses || []; }
  catch (e) { return; }
  renderHarnesses(harnesses);
}

function renderHarnesses(harnesses) {
  const board = document.getElementById("harnessBoard");
  if (!board) return;
  board.hidden = false;
  board.innerHTML = "";
  harnesses.forEach((h) => {
    const card = el("div", "harness-card");
    const head = el("div", "harness-head");
    head.append(el("span", "harness-name", h.name));
    if (h.primary) head.append(el("span", "harness-chip", "primary"));
    const dot = el("span", "harness-engine " + (h.engineAlive ? "on" : "off"),
      h.engineAlive ? "engine live" : "engine down");
    if (h.queued) dot.textContent += " · " + h.queued + " queued";
    head.append(dot);
    card.append(head);
    card.append(el("div", "harness-path", h.path));
    if (!h.engineAlive && h.engineHint) {
      const hint = el("div", "harness-hint", h.engineHint);
      card.append(hint);
    }
    (h.spirits || []).forEach((sp) => {
      const row = el("div", "harness-spirit");
      row.append(el("span", "harness-spirit-name", sp.name));
      const sel = selectEl(h.portals || []);
      sel.className = "pp-in harness-portal-sel";
      sel.value = sp.portal;
      sel.onchange = async () => {
        try {
          const r = await fetch("/api/harnesses/spirit/portal", {
            method: "POST", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ harness: h.name, spirit: sp.name, portal: sel.value }),
          });
          if (!r.ok) throw new Error(await r.text());
          showToast(sp.name + " → " + sel.value);
        } catch (e) { showToast("Couldn't switch conduit: " + (e.message || e), null, "error"); sel.value = sp.portal; }
      };
      row.append(sel);
      card.append(row);
    });
    board.append(card);
  });
}

// renderPortals — the Settings→Portals pane: Connections (apikey/oauth/effector)
// grouped over Conduits (llm, engine-managed). Always visible inside the pane.
function renderPortals(rows) {
  spPortalRows = rows;
  const host = document.getElementById("portalList");
  if (!host) return;
  host.hidden = false;
  host.innerHTML = "";
  const groups = [
    ["CONNECTIONS", rows.filter((p) => p.kind !== "llm")],
    ["CONDUITS", rows.filter((p) => p.kind === "llm")],
  ];
  groups.forEach(([label, list]) => {
    if (!list.length) return;
    host.append(el("div", "portal-group-label", label));
    const head = el("div", "portal-row portal-head");
    ["PORTAL", "STATE", "LAST CROSSING", "KEY", ""].forEach((h) => head.append(el("span", "", h)));
    host.append(head);
    list.forEach((p) => host.append(portalRowEl(p)));
  });
  updateSettingsBadge(); // repairing a portal clears the chip badge in place
}

// ---- SPIRIT PAGE (step-1 shape; step 5 brings the capability editor) ----
// Head · this spirit's rituals (derived from the board rows) · raw
// identity/cornerstone via the legacy drawer.
async function renderSpiritPage(name) {
  const host = document.getElementById("spSpiritWrap");
  if (!host) return;
  host.innerHTML = "";
  const head = el("div", "sprt-head");
  head.append(el("span", "sprt-title", name), el("span", "sprt-sub", "spirit"));
  const acts = el("span", "sprt-head-acts");
  const addR = el("button", "sprt-ghost", "＋ ritual");
  addR.onclick = () => newRitual(name);
  acts.append(addR);
  head.append(acts);
  host.append(head);

  let rows = [];
  try { rows = ((await (await fetch("/api/spirits/rituals")).json()).data || []).filter((r) => r.spirit === name); }
  catch (e) {}
  host.append(el("div", "pp-section-head", "RITUALS"));
  if (!rows.length) host.append(emptyRow("No rituals yet."));
  else {
    const board = el("div", "ritual-board");
    rows.forEach((r) => board.append(ritualRow(r)));
    host.append(board);
  }

  host.append(el("div", "pp-section-head", "IDENTITY & CORNERSTONE"));
  const open = el("button", "pill light", "edit raw (identity · cornerstone)");
  open.onclick = () => openSpiritEditor(name);
  host.append(open);
}

// (renderRitualEditor — the structured editor — lives in 58-rituals.js)

// ---- SETTINGS — Portals · Chargebook · Harnesses behind the aion-org inner
// rail (SPIRITS.md §4: configuration is never a top-level view) ----
function renderSpiritSettings() {
  const host = document.getElementById("spSettingsWrap");
  if (!host) return;
  host.innerHTML = "";
  const wrap = el("div", "aion-org");
  const rail = el("div", "aion-org-rail");
  const pane = el("div", "aion-org-pane");
  wrap.append(rail, pane);
  host.append(wrap);

  rail.append(el("div", "aion-org-label", "Settings"));
  const degraded = spPortalRows.filter((p) => p.state === "degraded").length;
  const items = [
    ["portals", "Portals", degraded ? degraded + " ●" : String(spPortalRows.length || "")],
    ["chargebook", "Chargebook", ""],
    ["harnesses", "Harnesses", ""],
  ];
  items.forEach(([key, label, n]) => {
    const b = el("button", "aion-org-item" + (spSettingsTab === key ? " active" : ""));
    b.append(el("span", "", label));
    if (n) b.append(el("span", "aion-org-count" + (key === "portals" && degraded ? " attn" : ""), n));
    b.onclick = () => { spSettingsTab = key; renderSpiritSettings(); };
    rail.append(b);
  });
  const fileBox = el("div", "aion-org-file");
  const rel = { portals: "grimoire/portals/", chargebook: "chargebook.md", harnesses: "config.json harnesses[]" }[spSettingsTab];
  fileBox.append(el("div", "aion-org-label", "File"), el("div", "aion-org-path", rel));
  rail.append(fileBox);

  if (spSettingsTab === "portals") {
    pane.append(el("div", "pp-section-head", "PORTALS"));
    const list = el("div", "portal-board");
    list.id = "portalList";
    pane.append(list);
    loadPortals();
  } else if (spSettingsTab === "chargebook") {
    renderChargebookPane(pane);
  } else {
    pane.append(el("div", "pp-section-head", "HARNESSES"));
    const board = el("div", "harness-board");
    board.id = "harnessBoard";
    pane.append(board);
    loadHarnesses();
  }
}

// Step-7 builds the real form; until then the chargebook pane hosts the legacy
// raw drawer scoped to chargebook.md (the ⌘/ escape hatch, lint-gated).
function renderChargebookPane(pane) {
  pane.append(el("div", "pp-section-head", "CHARGEBOOK"));
  pane.append(el("div", "aion-section-note",
    "default_run_ceiling_usd is the ceiling every keyless ritual inherits; price.*/cast.* are the per-cast prices"));
  const open = el("button", "pill light", "edit chargebook.md");
  open.onclick = () => openEditor(["chargebook.md"]);
  pane.append(open);
}

const PORTAL_STATE_LABEL = { open: "open", degraded: "degraded", sealed: "—", dormant: "—" };

function portalRowEl(p) {
  const wrap = el("div", "portal-wrap");
  const row = el("div", "portal-row state-" + p.state);
  row.dataset.portalId = p.id;
  row.append(el("span", "portal-name", p.name));
  const st = el("span", "portal-state", PORTAL_STATE_LABEL[p.state] || p.state);
  row.append(st);
  row.append(el("span", "portal-cross", portalCrossing(p)));
  row.append(el("span", "portal-key", p.masked || (p.kind === "oauth" ? "oauth" : "—")));
  const acts = el("span", "portal-acts");
  buildPortalActions(p, acts, wrap);
  row.append(acts);
  wrap.append(row);
  if (p.state === "degraded" && p.err) wrap.append(el("div", "portal-err", p.err));
  if ((p.kind === "oauth" || p.kind === "effector") && (p.accounts || []).length) {
    wrap.append(el("div", "portal-note", "connected: " + p.accounts.join(", ")));
  } else if (p.note && p.state !== "degraded") {
    wrap.append(el("div", "portal-note", p.note));
  }
  return wrap;
}

function portalCrossing(p) {
  if (p.kind === "llm") return "via engine";
  if (!p.lastCrossing) return p.state === "sealed" ? "not connected" : "—";
  const d = new Date(p.lastCrossing);
  if (isNaN(d)) return "—";
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  const t = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).replace(" ", "");
  return sameDay ? t + " today" : d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function buildPortalActions(p, acts, wrap) {
  if (p.kind === "apikey") {
    if (p.state === "dormant") { acts.append(el("span", "portal-dim", "(v2)")); return; }
    if (p.state === "sealed") {
      acts.append(pillLight("connect", () => togglePortalForm(p, wrap)));
      return;
    }
    acts.append(pillLight("test", () => portalAction("/api/portals/" + p.id + "/test")));
    // engine-managed portals (heypocket) are polled by the excalibur ritual, not manifest
    if (!p.engine) acts.append(pillLight("poll", () => portalAction("/api/portals/" + p.id + "/poll")));
    acts.append(
      pillLight("replace", () => togglePortalForm(p, wrap)),
      pillLight("disconnect", () => { if (confirm((p.engine ? "Remove the " + p.name + " key?" : "Disconnect " + p.name + "? Its cached items stay until they age out."))) portalAction("/api/portals/" + p.id + "/disconnect"); }),
    );
    return;
  }
  if (p.kind === "oauth") {
    if (p.id === "gmail") {
      // single-account, read-only. degraded (needs reauth) leads with a
      // prominent reconnect; connected offers reconnect + disconnect.
      const label = p.state === "degraded" ? "reconnect" : (p.state === "open" ? "reconnect" : "connect");
      const pill = p.state === "degraded" ? el("button", "pill-solid", "reconnect") : pillLight(label, () => portalConnectGmail());
      if (p.state === "degraded") pill.onclick = () => portalConnectGmail();
      acts.append(pill);
      if (p.state === "open" || (p.accounts || []).length) {
        acts.append(pillLight("disconnect", () => { if (confirm("Disconnect Gmail? The waiting-on digest stops until you reconnect.")) portalDisconnectGmail(); }));
      }
      return;
    }
    if ((p.accounts || []).length) {
      acts.append(pillLight("add", () => portalConnectCalendar()));
      p.accounts.forEach((email) => acts.append(pillLight("disconnect", () => portalDisconnectCalendar(email))));
    } else {
      acts.append(pillLight("connect", () => portalConnectCalendar()));
    }
    return;
  }
  if (p.kind === "effector") {
    // acts OUT via a local CLI (errands-aside §1) — nothing to connect here;
    // the executor's actions arrive when it exists.
    acts.append(el("span", "portal-dim", "local CLI"));
    return;
  }
  // llm — read-only, managed by the engine
  acts.append(el("span", "portal-dim", "engine"));
}

// togglePortalForm reveals the paste-key form inline beneath a row. Secret
// fields are password inputs; on save the key posts to the server (0600) and the
// row re-renders from the auto-tested response — the value never comes back.
function togglePortalForm(p, wrap) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form");
  const inputs = {};
  (p.fields || []).forEach((f) => {
    const label = el("label", "portal-field");
    label.append(el("span", "portal-field-label", f.label));
    const input = el("input", "portal-input");
    input.type = f.secret ? "password" : "text";
    input.placeholder = f.hint || "";
    label.append(input);
    inputs[f.key] = input;
    form.append(label);
  });
  const save = el("button", "pill-solid", "save & test");
  save.onclick = async () => {
    const fields = {};
    Object.keys(inputs).forEach((k) => { fields[k] = inputs[k].value.trim(); });
    save.disabled = true; save.textContent = "testing…";
    try {
      const row = await postJSON("/api/portals/" + p.id + "/key", { fields });
      form.remove();
      const wrapNew = portalRowEl(row);
      wrap.replaceWith(wrapNew);
      showToast(row.state === "open" ? p.name + " connected" : p.name + " saved — " + (row.err || row.state), null, row.state === "open" ? "info" : undefined);
    } catch (e) { save.disabled = false; save.textContent = "save & test"; showToast("Couldn't save " + p.name); }
  };
  form.append(save);
  wrap.append(form);
  const first = form.querySelector("input"); if (first) first.focus();
}

async function portalAction(url) {
  try {
    const row = await postJSON(url, {});
    const host = document.getElementById("portalList");
    const wrap = host && host.querySelector(`[data-portal-id="${CSS.escape(row.id)}"]`)?.closest(".portal-wrap");
    if (wrap) wrap.replaceWith(portalRowEl(row));
    spPortalRows = spPortalRows.map((p) => (p.id === row.id ? row : p));
    updateSettingsBadge();
    refreshFeedBadge();
  } catch (e) { showToast("Portal action failed"); }
}

// Calendar keeps its own OAuth endpoints — the portal row just drives them, then
// reloads the panel so its state reflects the new connection.
async function portalConnectCalendar() {
  showToast("Opening Google sign-in… (check your browser)", null, "info");
  try { await postJSON("/api/calendar/connect", {}); } catch (e) {}
  loadPortals();
}
async function portalDisconnectCalendar(email) {
  try { await postJSON("/api/calendar/disconnect", { account: email }); } catch (e) {}
  loadPortals();
}

// Gmail read-only OAuth — manifest mints the token the excalibur EA digest reads.
async function portalConnectGmail() {
  showToast("Opening Google sign-in… (check your browser)", null, "info");
  try {
    const r = await postJSON("/api/gmail/connect", {});
    showToast(r && r.connected ? "Gmail reconnected — " + r.connected : "Gmail reconnected", null, "info");
  } catch (e) { showToast("Couldn't reconnect Gmail — " + (e.message || "sign-in failed")); }
  loadPortals();
}
async function portalDisconnectGmail() {
  try { await postJSON("/api/gmail/disconnect", {}); } catch (e) {}
  loadPortals();
}

async function loadSpiritsStatus() {
  try { spiritStatusCache = await (await fetch("/api/spirits/status")).json(); }
  catch (e) { spiritStatusCache = null; }
  updateSpiritsCrumb();
}

// The page keeps no status banner — engine state, ritual count, and the
// week's spend ride the breadcrumb meta (§12 / prototype).
function updateSpiritsCrumb() {
  if (typeof setCrumbMeta !== "function" || els.spiritsView.hidden) return;
  const st = spiritStatusCache;
  const bits = [];
  if (st && st.enabled && (st.harnesses || []).length > 1) {
    // federation: per-harness liveness ("excalibur ok · hermes down")
    st.harnesses.forEach((h) => bits.push(h.name + (h.engineAlive ? " ok" : " down")));
  } else if (st && st.enabled) bits.push(st.engineAlive ? "engine ok" : "engine down");
  else if (st) bits.push("not configured");
  if (typeof spiritRitualRows !== "undefined" && spiritRitualRows.length) {
    bits.push(spiritRitualRows.length + " ritual" + (spiritRitualRows.length === 1 ? "" : "s"));
  }
  if (typeof spiritWeekSpend === "function") {
    const ws = spiritWeekSpend();
    if (ws > 0) bits.push("$" + ws.toFixed(2) + " this week");
  }
  setCrumbMeta(bits.join(" · "));
}
function setBadge(elm, n) {
  if (!elm) return;
  elm.hidden = !n;
  elm.textContent = n || "";
}

// ---- in-app toasts (run finished → report; digest landed → feed). No OS notifications. ----
function showToast(msg, onClick, kind) {
  const host = els.toastHost;
  if (!host) return;
  const t = el("div", "toast" + (kind ? " toast-" + kind : ""));
  t.append(el("span", "toast-msg", msg));
  if (onClick) { t.classList.add("clickable"); t.onclick = () => { onClick(); t.remove(); }; }
  const x = el("button", "toast-x", "✕");
  x.onclick = (e) => { e.stopPropagation(); t.remove(); };
  t.append(x);
  host.append(t);
  setTimeout(() => t.remove(), 9000); // dismisses itself
}

// ---- file-derived live run polling (replaces watchForNewRun) ----
// A single ~3s poll while the SPIRITS or FEED tab is open AND some run is
// queued/running (dig-from-feed needs run-watching without leaving the feed).
// Everything shown derives from the runs+queued files, so a refresh mid-run
// loses nothing. Transitions raise toasts; the open report body refreshes live.
let livePollTimer = null;
let runOutcomes = {};       // runId → last-seen outcome (transition detection)
let liveBaselined = false;  // don't toast runs that were already finished on first look
let knownDigestIds = null;  // feed digest ids seen, for the digest-landed toast

function pollScopeOpen() {
  return location.hash.startsWith("#/spirits") || location.hash === "#/feed";
}
function activeRuns() {
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  return running.length + (spiritRuns.queued || []).length;
}
function ensureLivePoll() {
  if (livePollTimer || !pollScopeOpen()) return;
  livePollTimer = setInterval(livePoll, 3000);
  livePoll(); // immediate tick
}
function stopLivePoll() { if (livePollTimer) { clearInterval(livePollTimer); livePollTimer = null; } }

async function livePoll() {
  if (!pollScopeOpen()) { stopLivePoll(); return; }
  const firstPoll = !liveBaselined;
  spiritRuns = await fetchSpiritRuns();

  // detect running → terminal transitions for the run-finished toast
  let anyFinished = false;
  (spiritRuns.data || []).forEach((r) => {
    const was = runOutcomes[r.id];
    if (liveBaselined && r.outcome !== "running" && was === "running") {
      anyFinished = true;
      showToast(`${r.spirit}/${r.ritual} — ${r.outcome}` + (r.itemsWritten ? ` · ${r.itemsWritten} item${r.itemsWritten === 1 ? "" : "s"}` : ""),
        () => { location.hash = "#/spirits"; setTimeout(() => openSpiritRun(r.id), 120); });
    }
    runOutcomes[r.id] = r.outcome;
  });
  liveBaselined = true;

  // re-render whatever is open, from files alone (ONE page now)
  if (location.hash.startsWith("#/spirits")) renderSpiritRuns();
  if (openRunId) refreshOpenRun(); // includes the finishing tick, so the report shows the terminal outcome

  if (anyFinished) {
    refreshFeedBadge();                               // nav-pill inbox count
    if (location.hash.startsWith("#/spirits")) loadSpiritsStatus();
    if (location.hash === "#/feed") loadFeed();       // new findings land in place
  }
  if (firstPoll || anyFinished) detectNewDigest();   // baseline on first look; then catch a landed digest
  if (activeRuns() === 0) stopLivePoll();            // nothing left to watch
}

async function detectNewDigest() {
  let items = [];
  try { items = (await (await fetch("/api/feed?status=inbox")).json()).items || []; } catch (e) { return; }
  diffDigests(items);
}

// diffDigests toasts once per newly-seen digest id. Also called from loadFeed
// itself, so entering FEED catches a digest that landed while no poll ran.
function diffDigests(items) {
  const digests = (items || []).filter((i) => i.type === "digest").map((i) => i.id);
  if (knownDigestIds === null) { knownDigestIds = new Set(digests); return; } // baseline
  digests.forEach((id) => {
    if (!knownDigestIds.has(id)) {
      knownDigestIds.add(id);
      showToast("New digest in the feed", () => { location.hash = "#/feed"; }, "digest");
    }
  });
}
