// ================= Agents panel =================
// (small DOM helpers live in 05-components.js — the §11 component library)

// ---- AGENTS: the operating surface over both runtimes ----
// SCHEDULE · RUNS over excalibur spirits and Hermes (alfred + profiles), plus
// the agent pages, the ritual editor and the new-agent wizard. The feed (incl.
// the approvals inbox) lives one level up as its own tab; the ENGINE owns
// execution — the only writes toward it are spooled run-now requests and the
// lint-gated file edits.
let spiritStatusCache = null;
let spiritRuns = { data: [], queued: [] }; // last poll of /api/spirits/runs — the ONLY run state; nothing else is held
let openRunId = null;                       // which run's report detail is expanded (for live body refresh)

// Agents plan §4.3: a top tab-bar over one body — SCHEDULE · RUNS — and the
// agent as the rail object: #/agents/<name> is an agent page (spirit or
// profile), #/agents/ritual/<spirit>/<name> the ritual editor, #/agents/new
// the wizard. Legacy tails (#/spirits*, #/agents/settings) redirect in route().
let spMode = "rituals"; // rituals (= SCHEDULE) | runs | new | spirit | editor
let spSpirit = "";      // the open spirit (spirit/editor modes)
let spRitualPath = "";  // the open ritual file (editor mode)

function showSpirits(h) {
  const tail = h && h.startsWith("#/agents/") ? decodeURIComponent(h.slice("#/agents/".length)) : "";
  spSpirit = ""; spRitualPath = "";
  if (tail === "") spMode = "rituals";
  else if (tail === "runs") spMode = "runs";
  else if (tail === "new") spMode = "new";
  else if (tail.startsWith("ritual/")) {
    const rest = tail.slice("ritual/".length);
    const i = rest.indexOf("/");
    if (i <= 0) { location.hash = "#/agents"; return; }
    spMode = "editor";
    spSpirit = rest.slice(0, i);
    spRitualPath = "spirits/" + spSpirit + "/rituals/" + rest.slice(i + 1) + ".md";
  } else { spMode = "spirit"; spSpirit = tail; }
  renderSpirits();
}

// renderSpToggle — chip-active mirror of renderReToggle: the agent page and
// the ritual editor keep SCHEDULE lit (they open from it); the wizard lights none.
function renderSpToggle() {
  const active = (spMode === "spirit" || spMode === "editor") ? "rituals" : spMode === "new" ? "" : spMode;
  const tog = document.getElementById("spToggle");
  if (tog) tog.querySelectorAll(".view-tab").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === active));
}

function renderSpirits() {
  renderSpToggle();
  const show = (id, on) => { const n = document.getElementById(id); if (n) n.hidden = !on; };
  show("spRitualsWrap", spMode === "rituals");
  show("spEditorWrap", spMode === "editor");
  show("spSpiritWrap", spMode === "spirit");
  show("spRunsWrap", spMode === "runs");
  show("spNewWrap", spMode === "new");
  if (typeof closeEditor === "function") closeEditor(); // no stale raw drawer under another view (renderers reopen it deliberately)
  loadSpiritsStatus();
  ensureLivePoll(); // resume watching queued/running runs, derived from files
  loadPortalsBadge();
  if (spMode === "rituals") { loadSchedule(); }
  else if (spMode === "runs") { loadSpiritRuns(); }
  else if (spMode === "new") { renderAgentWizard(); }
  else if (spMode === "spirit") {
    // the page's ritual rows carry the schedule anatomy (strip, health), so
    // the runs + conduit names load first
    Promise.all([loadSpiritRuns(), loadSpiritModels()]).then(() => renderSpiritPage(spSpirit));
  }
  else if (spMode === "editor") { renderRitualEditor(spRitualPath); }
}

// The rail's Settings count (`n ●` degraded connections) — derived from the
// last portal rows on every fetch/action, never stored.
let spPortalRows = []; // last /api/portals fetch (the rail's Settings count)
function updateSettingsBadge() {
  const degraded = spPortalRows.filter((p) => p.state === "degraded").length;
  if (typeof railSetCount === "function") railSetCount("settings", degraded ? degraded + " ●" : "", !!degraded);
}
async function loadPortalsBadge() {
  try { spPortalRows = (await (await fetch("/api/portals")).json()).rows || []; } catch (e) { return; }
  updateSettingsBadge();
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
// A single ~3s poll while the AGENTS or FEED tab is open AND some run is
// queued/running (dig-from-feed needs run-watching without leaving the feed).
// Everything shown derives from the runs+queued files, so a refresh mid-run
// loses nothing. Transitions raise toasts; the open report body refreshes live.
let livePollTimer = null;
let runOutcomes = {};       // runId → last-seen outcome (transition detection)
let liveBaselined = false;  // don't toast runs that were already finished on first look
let liveIdleTicks = 0;      // consecutive polls with nothing active (grace before stop)
let knownDigestIds = null;  // feed digest ids seen, for the digest-landed toast
let liveRunSig = "";        // running run ids at the last tick — a change repaints the SCHEDULE board

function pollScopeOpen() {
  return location.hash.startsWith("#/agents") || location.hash === "#/feed";
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
function stopLivePoll() {
  if (livePollTimer) { clearInterval(livePollTimer); livePollTimer = null; }
  // Re-baseline on the next (re)start: the first tick then records whatever is
  // already finished WITHOUT toasting it, so the isNew-terminal path only fires
  // for runs that actually start and finish inside the fresh watch window.
  liveBaselined = false;
  liveIdleTicks = 0;
}

async function livePoll() {
  if (!pollScopeOpen()) { stopLivePoll(); return; }
  const firstPoll = !liveBaselined;
  spiritRuns = await fetchSpiritRuns();

  // Detect finished runs for the run-finished toast. A run finishes when it
  // transitions running → terminal, OR — for a run fast enough that no poll ever
  // caught it mid-"running" (granola-sync et al. complete in ~9s, inside the 3s
  // poll + engine-pickup latency) — when a brand-new run id first appears already
  // terminal. Without the second case a quick launch spools, runs, and finishes
  // with no closure at all, which reads as "nothing happened". The baseline pass
  // (liveBaselined false on the first tick after each (re)start) records existing
  // runs silently so we never toast history.
  let anyFinished = false;
  (spiritRuns.data || []).forEach((r) => {
    const was = runOutcomes[r.id];
    const isNew = !(r.id in runOutcomes);
    const terminal = r.outcome !== "running";
    if (liveBaselined && terminal && (was === "running" || isNew)) {
      anyFinished = true;
      let detail = "";
      if (r.outcome === "completed") {
        detail = r.itemsWritten
          ? ` · ${r.itemsWritten} item${r.itemsWritten === 1 ? "" : "s"}`
          : " · no changes"; // distinguish a clean no-op from a failure
      } else if (r.outcomeDetail) {
        // failures carry the WHY: "error (protocol)" alone reads as noise —
        // the report's outcome line names the step and the actual error
        let why = r.outcomeDetail;
        const cut = why.indexOf(" — ");
        if (cut >= 0) why = why.slice(cut + 3); // the outcome already leads
        detail = " — " + (why.length > 110 ? why.slice(0, 110) + "…" : why);
      }
      showToast(`${r.spirit}/${r.ritual} — ${r.outcome}${detail}`,
        () => { location.hash = "#/agents"; setTimeout(() => openSpiritRun(r.id), 120); });
    }
    runOutcomes[r.id] = r.outcome;
  });
  liveBaselined = true;

  // re-render whatever is open, from files alone (ONE page now)
  if (location.hash.startsWith("#/agents")) renderSpiritRuns();
  if (openRunId) refreshOpenRun(); // includes the finishing tick, so the report shows the terminal outcome
  // the SCHEDULE board's outcome chips / strip / health follow the run files:
  // repaint when a run starts or finishes (never on a quiet tick)
  const sig = (spiritRuns.data || []).filter((r) => r.outcome === "running").map((r) => r.id).join(",");
  if (spMode === "rituals" && location.hash === "#/agents" && liveBaselined && (anyFinished || sig !== liveRunSig)) loadSpiritRituals();
  liveRunSig = sig;

  if (anyFinished) {
    refreshFeedBadge();                               // nav-pill inbox count
    if (location.hash.startsWith("#/agents")) loadSpiritsStatus();
    // never repaint the feed out from under a field being typed in — the poll
    // fires whenever any agent run finishes (AION's list does the same)
    const typing = els.feedView && els.feedView.contains(document.activeElement) &&
      /^(INPUT|TEXTAREA|SELECT)$/.test((document.activeElement || {}).tagName || "");
    if (location.hash === "#/feed" && !typing) loadFeed(); // new findings land in place
  }
  if (firstPoll || anyFinished) detectNewDigest();   // baseline on first look; then catch a landed digest

  // Stop only after a grace window of quiet. A just-spooled run is invisible for
  // a beat between the engine consuming the spool file and its run report
  // appearing; without the grace the poll would stop in that gap and miss the
  // completion of a fast run entirely.
  if (activeRuns() > 0) liveIdleTicks = 0;
  else if (++liveIdleTicks >= 4) stopLivePoll();     // ~12s of quiet
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

// ── agent-runs + feed action helpers (relocated from 50-studio.js when
//    Content Studio was removed; shared by feed/chat/rituals/todos/bars) ──

// openArtifact opens an artifact's library file in the universal note view —
// legacy path for a harness tree still inside the vault; returns to the feed.
function openArtifact(path) {
  _noteReturn = "#/feed";
  openNoteByPath(path);
}

// openResult — the ONE entry point every surface uses to view delegated work:
// the feed artifact card, the delegation-done card, the board's chip. Prefers
// the deliverable (the library brief) over the narration (the run report), and
// honors the two-media rule: a vault-side brief opens in the note view, while a
// harness brief / run report opens in the full-page artifact reader
// (showArtifact, 70-note.js) — the harness tree lives outside the vault.
function openResult(d, title) {
  if (!d) return;
  if (d.artifactPath) { openArtifact(d.artifactPath); return; }
  if (d.artifactRef) { location.hash = "#/artifact/ref/" + encodeURIComponent(d.harness || "") + "/" + encodeURIComponent(d.artifactRef); return; }
  if (d.runId) { location.hash = "#/artifact/run/" + encodeURIComponent(d.runId); return; }
  showToast("No result file yet for this work", null, "info");
}

// feedDig: "dig →" — spool a deeper run for the originating spirit; findings
// come back as new inbox items. Never navigates away from the feed. A card
// from the do-bot (alfred) answers with `runtime` — that dig is a Hermes turn
// with no run report to watch, so the toast points at the inbox, not RUNS.
async function feedDig(id) {
  let r;
  try { r = await fetch(`/api/feed/${encodeURIComponent(id)}/dig`, { method: "POST" }); }
  catch (e) { showToast("Dig failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    const d = await r.json().catch(() => ({}));
    if (d.runtime) { showToast(`${d.spirit || "alfred"} is already digging this one`, null, "info"); return; }
    showToast(`${d.spirit || "spirit"}/${d.ritual || "ritual"} is already running — view`, () => { location.hash = "#/agents/runs"; }, "info");
    return;
  }
  if (!r.ok) { showToast("Dig failed: " + ((await r.text()) || r.status), null, "error"); return; }
  const d = await r.json().catch(() => ({}));
  if (d.runtime) showToast(`${d.spirit} is digging — the brief lands back in the inbox`, null, "info");
  else showToast(`${d.spirit}/${d.ritual} queued — view`, () => { location.hash = "#/agents/runs"; }, "info");
  ensureLivePoll(); // watch it land back in the inbox
}
async function feedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/feed/${encodeURIComponent(id)}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed(); // re-renders + refreshes the badge from the same response
}
async function feedToTodo(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(id)}/to-task`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "promote failed");
    setSaveState("saved");
    showToast("Caught on the TASKS board → Inbox");
  } catch (e) { setSaveState("error"); showToast("→ task failed: " + e.message, null, "error"); }
  loadFeed();
}

// ---- run now / ask a scout (spooled request; engine picks it up within ~5s) ----
// spiritPick opens the agent/ritual picker (one area per spirit, its rituals
// as items) and calls onPick("spirit","ritual"). askRitual, when given, is
// picked automatically if present so "Ask a scout" lands on options-scout's
// research ritual without a needless second tap.
async function spiritPick(onPick) {
  // the catalog can be needed before AGENTS was ever opened (Ask-a-scout lives
  // in FEED now) — load it lazily.
  if (!spiritStatusCache) await loadSpiritsStatusCacheOnly();
  const spirits = (spiritStatusCache || {}).spirits || {};
  const groups = Object.keys(spirits).sort().map((sp) => ({
    area: sp,
    items: (spirits[sp] || []).map((rit) => ({ id: sp + "/" + rit, text: rit })),
  })).filter((g) => g.items.length);
  if (!groups.length) { showToast("No agent/ritual found in the excalibur tree.", null, "error"); return; }
  openPicker("Run a ritual now", groups, (id) => {
    const [sp, rit] = id.split("/");
    onPick(sp, rit);
  }, "No rituals found.");
}
async function loadSpiritsStatusCacheOnly() {
  try { spiritStatusCache = await (await fetch("/api/spirits/status")).json(); } catch (e) {}
}
// spiritSpool drops a run request. It holds NO button state — the run's status
// lives in the files (queued spool → running report). A 409 means the same
// spirit/ritual is already active (the double-spool guard). From FEED the user
// is never yanked away (feed-central §3: the loop closes in the feed) — a toast
// links to the live row instead; from AGENTS we jump to RUNS as before. The
// SCHEDULE board's per-row "run now" passes {stay: true} — the live strip on
// the board is where that run is watched.
async function spiritSpool(spirit, ritual, request, opts) {
  const stay = location.hash === "#/feed" || !!(opts && opts.stay);
  let r;
  try { r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual, request: request || "" }) }); }
  catch (e) { showToast("Run request failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    showToast(`${spirit}/${ritual} is already running — view`, () => { location.hash = "#/agents/runs"; }, "info");
    if (!stay) location.hash = "#/agents/runs";
    return;
  }
  if (!r.ok) { showToast("Run request failed (" + r.status + ")", null, "error"); return; }
  if (stay) {
    showToast(`${spirit}/${ritual} queued — view`, () => { location.hash = "#/agents/runs"; }, "info");
    if (location.hash.startsWith("#/agents")) loadSpiritRuns(); // the board's live strip shows the queued row
  } else {
    location.hash = "#/agents/runs";
    loadSpiritRuns(); // show the queued row immediately
  }
  ensureLivePoll();   // and watch it through to done
}
function spiritRunNow() {
  spiritPick((sp, rit) => spiritSpool(sp, rit, ""));
}
// spiritAskScout: pick a spirit/ritual, then take a free-form request via an
// inline box (no browser prompt). The request rides the spool into the prompt.
function spiritAskScout() {
  spiritPick((sp, rit) => {
    askText(`Request for ${sp} / ${rit}`,
      'e.g. "buy a mechanical keyboard under $200 — find 5 options"',
      (request) => { if (request.trim()) spiritSpool(sp, rit, request.trim()); });
  });
}
async function fetchSpiritRuns() {
  try {
    const d = await (await fetch("/api/spirits/runs")).json();
    return { data: d.data || [], queued: d.queued || [] };
  } catch (e) { return { data: [], queued: [] }; }
}

// ---- shared: the destructive-action button (spirit page + ritual editor) ----
// armedDelete — the destructive-action pattern: first click ARMS (ink
// "confirm?" label), second click within 4s executes; it disarms itself.
// No browser dialogs (owner call, agents UX pass).
function armedDelete(label, armedLabel, onConfirm) {
  const b = el("button", "sprt-quiet sprt-delete", label);
  let armed = false, timer = null;
  b.onclick = () => {
    if (!armed) {
      armed = true;
      b.textContent = armedLabel;
      b.classList.add("armed");
      timer = setTimeout(() => { armed = false; b.textContent = label; b.classList.remove("armed"); }, 4000);
      return;
    }
    clearTimeout(timer);
    b.disabled = true;
    onConfirm();
  };
  return b;
}

