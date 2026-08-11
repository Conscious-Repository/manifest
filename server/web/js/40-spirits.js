// ================= Agents panel =================
// (small DOM helpers live in 05-components.js — the §11 component library)

// ---- SPIRITS: the excalibur harness console ----
// Purely the engine console: RUNS · RITUALS — checking + instigating runs.
// The feed (incl. the approvals inbox) lives one level up as its own tab; the
// ENGINE owns execution — the only write toward it is a spooled run-now request.
let spiritStatusCache = null;
let spiritRuns = { data: [], queued: [] }; // last poll of /api/spirits/runs — the ONLY run state; nothing else is held
let openRunId = null;                       // which run's report detail is expanded (for live body refresh)

// ONE page (redesign §12): live strip → rituals board → recent runs, with
// portals as the settings block at the foot. The RUNS/RITUALS/PORTALS tab
// bar is gone; legacy sub-routes fold back to #/spirits.
function showSpirits() {
  if (location.hash.startsWith("#/spirits/")) {
    // deep link into the Portals section (e.g. the "reconnect Gmail" signal)
    if (location.hash === "#/spirits/portals") spiritPortalsOpen = true;
    location.hash = "#/spirits"; return;
  }
  ["runs", "rituals", "portals"].forEach((t) => { els["sp_" + t].hidden = false; });
  loadSpiritsStatus();
  loadSpiritRituals();
  loadSpiritRuns();
  loadPortals();
  ensureLivePoll(); // resume watching any queued/running runs, derived from files
}

// ---- PORTALS sub-tab: every external realm, (re)connectable in place ----
// The one place a connection is seen and repaired. Api-key portals (clickup,
// benchling) take a pasted key → save → auto-test; the oauth portal (calendar)
// runs its existing sign-in; the engine's LLM conduits are read-only. This is
// also the seed of app-wide settings — the row renderer is generic over the
// server's portal definition (fields drive the form), so github/docusign appear
// later as pure data.
async function loadPortals() {
  const host = els.portalList; if (!host) return;
  if (!host.children.length) host.textContent = "loading…";
  try {
    const rows = (await (await fetch("/api/portals")).json()).rows || [];
    renderPortals(rows);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Portals unavailable.")); }
}

let spiritPortalsOpen = false; // the settings block at the foot, closed by default

function renderPortals(rows) {
  const host = els.portalList; host.innerHTML = "";
  const head = el("div", "portal-row portal-head");
  ["PORTAL", "STATE", "LAST CROSSING", "KEY", ""].forEach((h) => head.append(el("span", "", h)));
  host.append(head);
  rows.forEach((p) => host.append(portalRowEl(p)));
  // the quiet foot toggle (§12: portals live in settings, off the page face)
  const foot = document.getElementById("spiritPortalsFoot");
  if (foot) {
    foot.innerHTML = "";
    const degraded = rows.filter((p) => p.state === "degraded").length;
    const t = el("button", "sprt-portals-toggle",
      (spiritPortalsOpen ? "▾" : "▸") + " portals · " + rows.length + (degraded ? " · ● " + degraded + " degraded" : ""));
    t.onclick = () => { spiritPortalsOpen = !spiritPortalsOpen; els.portalList.hidden = !spiritPortalsOpen; renderPortals(rows); };
    foot.append(t);
    els.portalList.hidden = !spiritPortalsOpen;
  }
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
    const wrap = els.portalList.querySelector(`[data-portal-id="${CSS.escape(row.id)}"]`)?.closest(".portal-wrap");
    if (wrap) wrap.replaceWith(portalRowEl(row));
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
  if (st && st.enabled) bits.push(st.engineAlive ? "engine ok" : "engine down");
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
