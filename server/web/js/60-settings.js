// ================= SETTINGS — the app-wide tab (#/settings/<group>) =================
// Agents plan §4.2 (Phase 1): Connections · Agents · Hosts & paths · Display
// behind the .aion-org inner rail. Every row here is a projection of state
// that already lives somewhere (portal rows, tokens, config.json, ~/.hermes,
// localStorage); the tab never stores anything of its own. The rail counts are
// DERIVED on every render — problems, never row counts.
//
// Phase 0 parked the old Spirits › SETTINGS chip panels (Portals · Chargebook ·
// Harnesses) in this file; they stay reachable at #/agents/settings until a
// later phase removes the chip. The row renderer, the paste-back OAuth flow,
// the Gmail accounts form and the chargebook form are SHARED between the two.

let settingsGroup = "connections";   // rail selection: connections | agents | hosts | display
let settingsArg = "";                // route tail after the group (e.g. "site/<host>" opens the add-site form)
let settingsConnRows = [];           // last /api/settings/connections fetch (rail count + crumb meta)
let settingsEnginesDown = 0;         // derived by the Agents pane (engines/gateway not live)
let settingsRailEl = null;           // the inner rail, rebuilt when a count changes

const SETTINGS_GROUPS = [
  ["connections", "Connections"],
  ["agents", "Agents"],
  ["hosts", "Hosts & paths"],
  ["display", "Display"],
];

function showSettings(h) {
  const tail = h && h.startsWith("#/settings/") ? h.slice("#/settings/".length) : "";
  const seg = tail.split("/");
  settingsGroup = SETTINGS_GROUPS.some(([k]) => k === seg[0]) ? seg[0] : "connections";
  settingsArg = seg.slice(1).join("/");
  renderSettings();
}

function renderSettings() {
  const host = document.getElementById("settingsWrap");
  if (!host) return;
  host.innerHTML = "";
  const wrap = el("div", "aion-org");
  settingsRailEl = el("div", "aion-org-rail");
  const pane = el("div", "aion-org-pane");
  wrap.append(settingsRailEl, pane);
  host.append(wrap);
  renderSettingsRail();
  if (settingsGroup === "connections") renderSettingsConnections(pane);
  else if (settingsGroup === "agents") renderSettingsAgents(pane);
  else if (settingsGroup === "hosts") renderSettingsHosts(pane);
  else renderSettingsDisplay(pane);
}

// renderSettingsRail rebuilds the inner rail from the derived counts: the
// Connections tally is the degraded rows (`n ●`), Agents the engines not live.
function renderSettingsRail() {
  const rail = settingsRailEl;
  if (!rail) return;
  rail.innerHTML = "";
  rail.append(el("div", "aion-org-label", "Settings"));
  const degraded = settingsConnRows.filter((p) => p.state === "degraded").length;
  const counts = { connections: degraded, agents: settingsEnginesDown };
  SETTINGS_GROUPS.forEach(([key, label]) => {
    const b = el("button", "aion-org-item" + (settingsGroup === key ? " active" : ""));
    b.append(el("span", "", label));
    const n = counts[key] || 0;
    if (n) b.append(el("span", "aion-org-count attn", n + " ●"));
    b.onclick = () => { location.hash = "#/settings/" + key; };
    rail.append(b);
  });
  const fileBox = el("div", "aion-org-file");
  const rel = {
    connections: "grimoire/portals/ · <dataDir>/portals/",
    agents: "chargebook.md",
    hosts: "config.json (read-only — edit on metis and restart)",
    display: "localStorage (this browser)",
  }[settingsGroup];
  fileBox.append(el("div", "aion-org-label", "File"), el("div", "aion-org-path", rel));
  rail.append(fileBox);
}

// ---- CONNECTIONS — one .portal-row per service, problems first ----
async function renderSettingsConnections(pane) {
  pane.append(el("div", "pp-section-head", "CONNECTIONS — every external service, seen and repaired here"));
  const list = el("div", "portal-board");
  list.id = "settingsConnList";
  pane.append(list);
  list.append(emptyRow("loading…"));
  await loadSettingsConnections();
}

async function loadSettingsConnections() {
  const list = document.getElementById("settingsConnList");
  if (!list) return;
  let rows;
  try { rows = (await (await fetch("/api/settings/connections")).json()).rows || []; }
  catch (e) { list.innerHTML = ""; list.append(emptyRow("Connections unavailable.")); return; }
  settingsConnRows = rows;
  spPortalRows = rows;      // the Agents chip badge + the rail's Settings count share this
  updateSettingsBadge();
  renderSettingsRail();
  if (typeof setCrumbMeta === "function") {
    const degraded = rows.filter((p) => p.state === "degraded").length;
    setCrumbMeta(degraded ? degraded + " degraded" : rows.length + " services · all reachable");
  }
  list.innerHTML = "";
  const head = el("div", "portal-row portal-head");
  ["SERVICE", "STATE", "LAST CROSSING", "KEY / IDENTITY", ""].forEach((h) => head.append(el("span", "", h)));
  list.append(head);
  connSort(rows).forEach((p) => list.append(portalRowEl(p)));
  // paid-site sign-ins are keyed by host — a new one is added here (Consume's
  // inline "sign in" link lands on this form with the host prefilled)
  const addWrap = el("div", "portal-wrap");
  const ghost = el("button", "sprt-ghost", "＋ paid-site sign-in");
  ghost.onclick = () => toggleSiteForm(addWrap, "");
  addWrap.append(ghost);
  list.append(addWrap);
  if (settingsArg.startsWith("site/")) {
    toggleSiteForm(addWrap, decodeURIComponent(settingsArg.slice(5)));
    settingsArg = "";
  }
}

// connSort — the Zapier rule: problems first, then alphabetical by name.
function connSort(rows) {
  const rank = (p) => (p.state === "degraded" ? 0 : 1);
  return rows.slice().sort((a, b) => rank(a) - rank(b) || (a.name || "").localeCompare(b.name || ""));
}

// reloadConnections re-derives the row list wherever it is showing — the
// Settings tab, or the parked Agents › SETTINGS chip panel.
function reloadConnections() {
  if (els.settingsView && !els.settingsView.hidden) {
    if (document.getElementById("settingsConnList")) loadSettingsConnections();
    return;
  }
  loadPortals();
}

// replaceRow swaps one rendered row for the server's re-derived one.
function replaceRow(wrap, row) {
  if (wrap && wrap.parentNode) wrap.replaceWith(portalRowEl(row));
  spPortalRows = spPortalRows.map((p) => (p.id === row.id ? row : p));
  settingsConnRows = settingsConnRows.map((p) => (p.id === row.id ? row : p));
  updateSettingsBadge();
  renderSettingsRail();
}

// armedPill — the two-click confirm (ui-conventions.md §buttons): the first
// click swaps in the armed label, the second executes; it reverts on its own.
function armedPill(label, armedLabel, onConfirm) {
  let quiet;
  const arm = () => {
    const yes = pillLight(armedLabel, onConfirm);
    yes.classList.add("armed");
    quiet.replaceWith(yes);
    setTimeout(() => { if (yes.parentNode) yes.replaceWith(quiet); }, 2500);
  };
  quiet = pillLight(label, arm);
  return quiet;
}

// ---- the ONE headless paste-back OAuth flow (calendar · Gmail read · Gmail send) ----
// Manifest runs on metis, so Google's loopback redirect never reaches it: the
// owner approves in their own browser and pastes the address the tab lands on.
function connectFlow(wrap, opts) {
  const existing = wrap.querySelector(".portal-form.connect-flow");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form connect-flow");
  const start = el("button", "pill-solid", opts.label || "open Google sign-in");
  start.onclick = async () => {
    start.disabled = true;
    try {
      const r = await postJSONOk(opts.startUrl, {});
      window.open(r.authUrl, "_blank", "noopener");
      start.replaceWith(pasteBackBox(opts.finishUrl, (res) => { form.remove(); opts.onDone(res); }));
      showToast("Approve in the Google tab, then paste the address it lands on", null, "info");
    } catch (e) {
      start.disabled = false;
      showToast("Couldn't start sign-in — " + (e.message || "error"), null, "error");
    }
  };
  form.append(start);
  wrap.append(form);
}

// pasteBackBox is step 2: the paste box + finish. finishUrl takes {redirect}.
function pasteBackBox(finishUrl, onDone) {
  const box = el("div", "gmail-acct-paste");
  box.append(el("div", "portal-note", "after approving, the tab lands on an unreachable 127.0.0.1 page — copy its FULL address and paste it here"));
  const input = el("input", "portal-input");
  input.type = "text";
  input.placeholder = "http://127.0.0.1:8123/oauth/callback?state=…&code=…";
  input.spellcheck = false;
  const fin = el("button", "pill-solid", "finish connect");
  fin.onclick = async () => {
    fin.disabled = true; fin.textContent = "connecting…";
    try { onDone(await postJSONOk(finishUrl, { redirect: input.value })); }
    catch (e) {
      fin.disabled = false; fin.textContent = "finish connect";
      showToast("Connect failed — " + (e.message || "check the pasted URL").slice(0, 140), null, "error");
    }
  };
  box.append(input, fin);
  setTimeout(() => input.focus(), 0);
  return box;
}

// toggleSiteForm — paste a session cookie for a paid publication (stored in
// the secrets tier, scoped to the host, never echoed back).
function toggleSiteForm(wrap, host) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form");
  const hostIn = el("input", "portal-input");
  hostIn.type = "text"; hostIn.placeholder = "site host, e.g. substack.com"; hostIn.value = host || "";
  const cookieIn = el("input", "portal-input");
  cookieIn.type = "password"; cookieIn.placeholder = "the VALUE of substack.sid (name=value also works)";
  const lab = (text, input) => { const l = el("label", "pp3-lform-field"); l.append(el("span", "pp3-lform-label", text), input); return l; };
  form.append(lab("site", hostIn), lab("session cookie", cookieIn));
  form.append(el("div", "portal-note", "DevTools → Application → Cookies → the site → copy the VALUE. It covers every publication on that domain, is stored outside your vault, and is never shared."));
  const save = el("button", "pill-solid", "save");
  save.onclick = async () => {
    const h = hostIn.value.trim(), cookie = cookieIn.value.trim();
    if (!h) { showToast("Which site is this sign-in for?"); return; }
    if (!cookie) { showToast("Paste the session cookie"); return; }
    save.disabled = true;
    try {
      await postJSONOk("/api/consume/sites", { host: h, cookie });
      showToast("signed in to " + h + " — paid posts arrive on the next poll", null, "info");
      reloadConnections();
    } catch (e) { save.disabled = false; showToast("Couldn't save — " + (e.message || "error"), null, "error"); }
  };
  form.append(save);
  wrap.append(form);
  (host ? cookieIn : hostIn).focus();
}

// ---- bank feed (SimpleFIN) — claim / accounts / sync, relocated from RE settings ----
function toggleBankClaimForm(wrap) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form");
  form.append(el("div", "portal-note",
    "Create a SimpleFIN bridge account, generate ONE setup token, and paste it here. The claim is one-time — it mints a read-only access URL stored on this box only."));
  const inp = el("input", "portal-input");
  inp.type = "password"; inp.placeholder = "SimpleFIN setup token…";
  const claim = el("button", "pill-solid", "claim");
  claim.onclick = async () => {
    const token = inp.value.trim();
    if (!token) { showToast("Paste the setup token first"); return; }
    claim.disabled = true;
    try {
      await postJSONOk("/api/bankfeed/claim", { token });
      showToast("Claimed — link each account to its entity under accounts", null, "info");
      reloadConnections();
    } catch (e) { claim.disabled = false; showToast("Claim failed — " + (e.message || ""), null, "error"); }
  };
  form.append(inp, claim);
  wrap.append(form);
  inp.focus();
}

async function toggleBankAccountsPanel(wrap) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form bank-accounts");
  wrap.append(form);
  const ents = ((await ensureEntities()) || {}).entities || [];
  await renderBankFeedPanel(form, ents);
}

// bankFeedRerender — the panel's own refresh after a link/unlink/backfill:
// in place when it is open here, else the whole row list.
async function bankFeedRerender() {
  const form = document.querySelector("#settingsConnList .portal-form.bank-accounts");
  if (form) {
    const ents = ((await ensureEntities(true)) || {}).entities || [];
    await renderBankFeedPanel(form, ents);
    return;
  }
  reloadConnections();
}

// ---- AGENTS — runtime status cards + the one budget knob ----
async function renderSettingsAgents(pane) {
  pane.append(el("div", "pp-section-head", "AGENTS — runtime"));
  const board = el("div", "harness-board");
  pane.append(board);
  board.append(emptyRow("loading…"));
  const j = (u) => fetch(u).then((r) => (r.ok ? r.json() : null)).catch(() => null);
  const [hz, hermes, portals] = await Promise.all([j("/api/harnesses"), j("/api/agents/hermes"), j("/api/portals")]);
  board.innerHTML = "";
  const harnesses = (hz && hz.harnesses) || [];
  const gatewayDown = !!(hermes && hermes.runner && hermes.runner.enabled && (!hermes.gateway || hermes.gateway.state !== "running"));
  settingsEnginesDown = harnesses.filter((h) => !h.engineAlive).length + (gatewayDown ? 1 : 0);
  renderSettingsRail();
  const primary = harnesses.find((h) => h.primary) || harnesses[0] || null;
  board.append(excaliburCard(primary, (portals && portals.rows) || []));
  board.append(alfredCard(hermes));
  board.append(teamAgentsCard(harnesses.filter((h) => h !== primary)));
  if (typeof setCrumbMeta === "function") {
    setCrumbMeta(settingsEnginesDown ? settingsEnginesDown + " engine" + (settingsEnginesDown === 1 ? "" : "s") + " down" : "all engines live");
  }
  pane.append(el("div", "pp-section-head", "BUDGET"));
  renderChargebookPane(pane, { budget: true });
}

function engineChip(alive, heartbeat, queued, word) {
  const chip = el("span", "harness-engine " + (alive ? "on" : "off"), alive ? (word || "engine live") : "down");
  if (heartbeat) chip.textContent += " · heartbeat " + fmtAgo(heartbeat);
  if (queued) chip.textContent += " · " + queued + " queued";
  return chip;
}

function fmtAgo(iso) {
  const t = new Date(iso);
  if (isNaN(t)) return "—";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return s + "s ago";
  if (s < 3600) return Math.round(s / 60) + "m ago";
  if (s < 86400) return Math.round(s / 3600) + "h ago";
  return Math.round(s / 86400) + "d ago";
}

function cardLine(card, key, value) {
  const row = el("div", "harness-line");
  row.append(el("span", "k", key));
  if (value && value.nodeType) row.append(value); else row.append(el("span", "", value));
  card.append(row);
  return row;
}

// 1. Excalibur engine — liveness, path, spirits, model per spirit, conduits.
function excaliburCard(h, portalRows) {
  const card = el("div", "harness-card");
  const head = el("div", "harness-head");
  head.append(el("span", "harness-name", "Excalibur engine"));
  if (!h) {
    head.append(el("span", "harness-engine off", "not configured"));
    card.append(head, emptyRow("no harness in config.json (harnesses[] / excaliburPath)"));
    return card;
  }
  head.append(el("span", "harness-chip", h.name), engineChip(h.engineAlive, h.heartbeat, h.queued));
  card.append(head);
  card.append(el("div", "harness-path", h.path));
  if (!h.engineAlive) {
    // disabled beats hidden (§3.1): the affordance exists, the title says whose
    // action it is. The sudo string stays out of the card.
    const start = el("button", "pill light", "start");
    start.disabled = true;
    start.title = "operator action: systemd on metis";
    const hint = el("div", "harness-hint", "no live engine — delegations queue until one runs. ");
    hint.append(start);
    card.append(hint);
  }
  const spirits = h.spirits || [];
  card.append(el("div", "portal-note", spirits.length + " spirit" + (spirits.length === 1 ? "" : "s") + " · model per spirit (switch it on the agent page)"));
  spirits.forEach((sp) => {
    const row = el("div", "harness-spirit");
    row.append(el("span", "harness-spirit-name", sp.name), el("span", "harness-spirit-model", sp.portal || "—"));
    card.append(row);
  });
  // ＋ spirit moved here from the SCHEDULE board (agents plan §4.3, Phase 2):
  // the board stays a schedule; the scaffold lands on the new spirit's page
  if (h.primary !== false && typeof newSpirit === "function") {
    const add = el("button", "sprt-ghost harness-add", "＋ spirit");
    add.onclick = () => newSpirit();
    card.append(add);
  }
  // the engine's conduits (formerly the Portals pane's "via engine" rows)
  const conduits = portalRows.filter((r) => r.kind === "llm" || /deepseek/i.test(r.id || ""));
  if (conduits.length) {
    cardLine(card, "conduits", conduits.map((r) =>
      r.kind === "llm" ? r.name : r.name + " (" + (r.masked || "unset") + " · " + (r.state || "?") + ")").join(" · "));
  }
  return card;
}

// 2. Alfred (Hermes) — the owner's do-bot: runner, gateway, cron ticker, profiles.
function alfredCard(hz) {
  const card = el("div", "harness-card");
  const head = el("div", "harness-head");
  head.append(el("span", "harness-name", "Alfred (Hermes)"));
  if (!hz) {
    head.append(el("span", "harness-engine off", "unavailable"));
    card.append(head, emptyRow("/api/agents/hermes did not answer"));
    return card;
  }
  const gw = hz.gateway || null;
  head.append(engineChip(!!gw && gw.state === "running", gw && gw.updatedAt, 0, "gateway running"));
  card.append(head);
  card.append(el("div", "harness-path", hz.home || "~/.hermes"));
  const r = hz.runner || {};
  cardLine(card, "runner", r.enabled
    ? "wired · " + (r.bin || "hermes") + " · " + (r.timeoutSeconds ? r.timeoutSeconds + "s per turn" : "default timeout")
    : "not wired — config.json hermes.enabled is off; @hermes falls back to the harness copy");
  if (gw) {
    const plats = (gw.platforms || []).map((p) => p.name + " " + (p.state || "?") + (p.error ? " (" + p.error + ")" : "")).join(" · ");
    cardLine(card, "gateway", (gw.state || "unknown") + (plats ? " · " + plats : "") + (gw.activeAgents ? " · " + gw.activeAgents + " active" : ""));
  } else {
    cardLine(card, "gateway", "no gateway_state.json — the gateway has not run on this box");
  }
  const cron = hz.cron || {};
  cardLine(card, "cron", (cron.heartbeat ? "ticker heartbeat " + fmtAgo(cron.heartbeat) : "no ticker heartbeat")
    + (cron.lastSuccess ? " · last tick ok " + fmtAgo(cron.lastSuccess) : "")
    + (cron.jobs != null ? " · " + cron.jobs + " job" + (cron.jobs === 1 ? "" : "s") + " (" + (cron.enabled || 0) + " enabled)" : ""));
  // the jobs themselves (Phase 4): name · schedule · next · model pin — an
  // unpinned enabled job is the --warn chip (it fails closed on model drift)
  if (cron.outcome === "unknown") {
    card.append(el("div", "portal-note", "jobs unknown — " + (cron.why || "jobs.json unreadable")));
  } else if (cron.source === "cli") {
    card.append(el("div", "portal-note", "jobs.json missing — list read from `hermes cron list`"));
  }
  if (cron.unpinned) {
    const warn = el("div", "harness-hint");
    warn.append(el("span", "run-outcome oc-unpinned", "unpinned"), document.createTextNode(" " + cron.unpinned + " enabled job" + (cron.unpinned === 1 ? " has" : "s have") + " no model pin — Hermes skips the fire whenever the global model drifts (the 08-30..09-01 skips)."));
    card.append(warn);
  }
  (cron.list || []).forEach((j) => {
    const row = el("div", "harness-spirit");
    row.append(el("span", "harness-spirit-name", (j.enabled === false ? "⏸ " : "") + (j.name || j.id)));
    const bits = [j.scheduleHuman || j.schedule || "—"];
    if (j.enabled !== false && j.nextRunAt) bits.push("next " + fmtWhen(j.nextRunAt));
    if (j.enabled === false) bits.push(j.state === "completed" ? "completed" : "paused");
    if (j.lastStatus) bits.push("last " + j.lastStatus);
    row.append(el("span", "harness-spirit-model", bits.join(" · ")));
    if (j.model) { const m = el("span", "harness-spirit-model", j.model); m.title = "model pinned in jobs.json"; row.append(m); }
    else if (j.enabled !== false) { const u = el("span", "run-outcome oc-unpinned", "unpinned"); u.title = "model: null in jobs.json — pin it: hermes cron edit " + j.id + " --model <name>"; row.append(u); }
    const open = el("button", "sprt-quiet", "board →");
    open.title = "the SCHEDULE board carries this job's run / pause / resume controls";
    open.onclick = () => { location.hash = "#/agents"; };
    row.append(open);
    card.append(row);
  });
  // PROFILES (Phase 5): the list is exactly `hermes profile list` — re-asked
  // on every paint through /api/profiles, never held; ＋ profile creates one
  // through `hermes profile create` (immediate-apply, the alias + -p line in
  // the toast). Rows open the profile's agent page.
  const profilesHost = el("div", "harness-profiles");
  card.append(profilesHost);
  paintProfilesSection(profilesHost, hz.profiles || []);
  return card;
}

// paintProfilesSection — the Profiles rows + the ＋ profile flow, refreshed
// from /api/profiles (the `hermes profile list` projection). `seed` paints
// what /api/agents/hermes already reported until the fresh list answers.
async function paintProfilesSection(host, seed) {
  const paint = (list, degraded) => {
    host.innerHTML = "";
    const note = el("div", "portal-note", list.length + " profile" + (list.length === 1 ? "" : "s") + " · from `hermes profile list`" + (degraded ? " · list unavailable: " + degraded : ""));
    host.append(note);
    list.forEach((p) => {
      const row = el("div", "harness-spirit");
      const name = el("span", "harness-spirit-name", (p.active ? "◆ " : "") + p.name);
      name.title = (p.active ? "the active profile · " : "") + "open the agent page";
      row.append(name);
      row.append(el("span", "harness-spirit-model", [p.model || "no model", p.gateway ? "gateway " + p.gateway : "", p.alias ? "alias " + p.alias : "no alias", p.distribution ? "dist " + p.distribution : ""].filter(Boolean).join(" · ")));
      const target = el("span", "harness-spirit-model", "-p " + p.name);
      target.title = "runner targeting: hermes -p " + p.name + " -z …";
      row.append(target);
      const open = el("button", "sprt-quiet", "page →");
      open.onclick = () => { location.hash = "#/agents/" + encodeURIComponent(p.name); };
      row.append(open);
      host.append(row);
    });
    const add = el("button", "sprt-ghost", "＋ profile");
    add.title = "hermes profile create <name> [--clone-from <src>] [--description …]";
    add.onclick = () => { add.hidden = true; host.append(profileCreateForm(list, () => refresh(), () => { add.hidden = false; })); };
    host.append(add);
  };
  const refresh = async () => {
    try {
      const d = await (await fetch("/api/profiles")).json();
      paint(d.data || [], d.degraded || "");
    } catch (e) { paint(seed, "/api/profiles did not answer"); }
  };
  paint(seed, "");
  await refresh();
}

// profileCreateForm — name · clone-from · description → POST /api/profiles.
// The slug rule is the CLI's (lowercase letters, digits, hyphens) and is
// refused here before the server refuses it again.
function profileCreateForm(existing, onDone, onCancel) {
  const form = el("div", "sp-capability");
  const row1 = el("div", "sp-cap-row");
  row1.append(el("span", "cadb-label", "name"));
  const name = inputEl("lowercase, e.g. recruiter");
  name.maxLength = 32;
  row1.append(name);
  const hint = el("span", "cad-raw", "");
  row1.append(hint);
  form.append(row1);
  const row2 = el("div", "sp-cap-row");
  row2.append(el("span", "cadb-label", "clone from"));
  const clone = selectEl(["(fresh — bundled skills, no keys)"].concat(existing.map((p) => p.name)));
  clone.title = "--clone-from <src>: copies config.yaml, .env, SOUL.md and skills from that profile";
  row2.append(clone);
  form.append(row2);
  const row3 = el("div", "sp-cap-row");
  row3.append(el("span", "cadb-label", "description"));
  const desc = inputEl("one or two sentences on what this profile is good at (kanban routing)");
  desc.style.flex = "1";
  row3.append(desc);
  form.append(row3);
  const acts = el("div", "sp-cap-row");
  const slugOk = () => /^[a-z0-9][a-z0-9-]{0,31}$/.test(name.value.trim());
  const check = () => {
    const v = name.value.trim();
    const dup = existing.some((p) => p.name === v);
    hint.textContent = !v ? "" : dup ? "already exists" : slugOk() ? "hermes -p " + v : "lowercase letters, digits, hyphens only";
    create.disabled = !v || dup || !slugOk();
  };
  const create = pill("create profile", async () => {
    if (!slugOk()) return;
    create.disabled = true;
    setSaveState("saving");
    try {
      const body = { name: name.value.trim(), description: desc.value.trim() };
      if (clone.selectedIndex > 0) body.cloneFrom = clone.value;
      const r = await fetch("/api/profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || d.ok === false) throw new Error(d.error || d.output || ("HTTP " + r.status));
      setSaveState("saved");
      showToast((d.command || "hermes profile create") + " — " + (d.alias ? "alias `" + d.alias + "` · " : "") + "target `hermes " + (d.target || "-p " + body.name) + "` — open",
        () => { location.hash = "#/agents/" + encodeURIComponent(body.name); }, "info");
      form.remove();
      onDone();
    } catch (e) {
      setSaveState("error");
      create.disabled = false;
      showToast("Couldn't create profile: " + (e.message || e), null, "error");
    }
  });
  create.disabled = true;
  const cancel = pillLight("cancel", () => { form.remove(); onCancel(); });
  acts.append(create, cancel);
  form.append(acts);
  name.addEventListener("input", check);
  name.addEventListener("keydown", (e) => { if (e.key === "Enter" && !create.disabled) create.click(); if (e.key === "Escape") cancel.click(); });
  setTimeout(() => name.focus(), 0);
  return form;
}

// 3. Team agents (D8): kairos + zeck — status only, never scheduled from here.
function teamAgentsCard(list) {
  const card = el("div", "harness-card");
  const head = el("div", "harness-head");
  head.append(el("span", "harness-name", "Team agents"));
  head.append(el("span", "harness-engine " + (list.every((h) => h.engineAlive) ? "on" : "off"),
    list.length ? list.filter((h) => h.engineAlive).length + " of " + list.length + " live" : "none"));
  card.append(head);
  card.append(el("div", "portal-note", "team chat agents — not scheduled from here."));
  if (!list.length) { card.append(emptyRow("no team harnesses in config.json (kairos, zeck)")); return card; }
  list.forEach((h) => {
    const row = el("div", "harness-spirit");
    row.append(el("span", "harness-spirit-name", h.name));
    row.append(el("span", "harness-spirit-model", h.path));
    row.append(engineChip(h.engineAlive, h.heartbeat, h.queued));
    card.append(row);
  });
  return card;
}

// ---- HOSTS & PATHS — a read-only projection of config.json (D3) ----
async function renderSettingsHosts(pane) {
  let d = null;
  try { d = await (await fetch("/api/settings/hosts")).json(); } catch (e) {}
  if (!d) { pane.append(emptyRow("config projection unavailable")); return; }
  const c = d.config || {};
  const file = d.file || "config.json (read-only — edit on metis and restart)";
  const group = (title, rows, note) => {
    pane.append(el("div", "pp-section-head", title));
    const g = el("div", "set-kv");
    if (!rows.length) g.append(emptyRow("none configured"));
    rows.forEach(([k, v]) => {
      const r = el("div", "set-kv-row");
      const unset = v == null || v === "";
      r.append(el("span", "set-kv-key", k), el("span", "set-kv-val" + (unset ? " dim" : ""), unset ? "—" : String(v)));
      g.append(r);
    });
    pane.append(g);
    pane.append(el("div", "portal-note", "File: " + (note || file)));
  };
  const v = c.vault || {}, data = c.data || {}, files = c.files || {}, l = c.listeners || {}, cs = c.consume || {}, hm = c.hermes || {};
  const env = d.env || [];
  const envVal = (name) => { const e = env.find((x) => x.name === name); return e && e.set ? (e.value || "set") : ""; };
  group("VAULT", [["vaultPath", v.vaultPath], ["systemRoot", v.systemRoot], ["extrinsicRoot", v.extrinsicRoot], ["newDailyDir", v.newDailyDir]]);
  group("DATA", [["dataDir", data.dataDir], ["MANIFEST_CONFIG_DIR", envVal("MANIFEST_CONFIG_DIR") || "unset → ~/.config/manifest"]]);
  group("HARNESSES", (c.harnesses || []).map((h) => [h.name + " · " + (h.surface || "personal"), h.path]));
  group("FILES & TERMINAL", [
    ...((files.roots || []).map((r, i) => ["filesRoots[" + i + "]", r])),
    ...((files.agents || []).map((a) => ["agent " + a.name, a.url])),
    ...((c.terminalDevices || []).map((dv) => ["device " + dv.name,
      dv.user + "@" + dv.host + (dv.port ? ":" + dv.port : "") + (dv.identity ? " · key " + dv.identity : "") + (dv.agent ? " · agent " + dv.agent : "")])),
  ]);
  group("LISTENERS", [["port", l.port], ["portalPort", l.portalPort], ["ooda.port", l.oodaPort || "0 = off"], ["consume.publicPort", l.consumePublicPort || "0 = off"]]);
  group("CONSUME INTERVALS", [
    ["rssIntervalMinutes", (cs.rssIntervalMinutes || 60) + " min"],
    ["xIntervalMinutes", (cs.xIntervalMinutes || 360) + " min — a spending decision (X bills per post read)"],
    ["rsshubBase", cs.rsshubBase || "http://127.0.0.1:1200 (default)"],
  ]);
  group("HERMES", [["enabled", hm.enabled ? "true" : "false"], ["bin", hm.bin], ["timeoutSeconds", hm.timeoutSeconds || "default"], ["HERMES_HOME", d.hermesHome]]);
  group("ENVIRONMENT", env.map((e) => [e.name, e.set ? (e.value || "set") : "unset"]), "the process environment on metis — presence only; values never leave the box");
}

// ---- DISPLAY — this browser's four localStorage prefs, immediate-apply ----
function renderSettingsDisplay(pane) {
  pane.append(el("div", "pp-section-head", "DISPLAY — this browser's preferences (immediate)"));
  const g = el("div", "set-kv");
  const row = (key, ctl) => { const r = el("div", "set-kv-row"); r.append(el("span", "set-kv-key", key), ctl); g.append(r); };
  const store = (k) => { try { return localStorage.getItem(k); } catch (e) { return null; } };
  const drop = (k) => { try { localStorage.removeItem(k); } catch (e) {} };

  // 1. rail collapsed (manifest.rail.collapsed)
  const railLab = el("label", "gmail-acct-toggle");
  const railCb = el("input", ""); railCb.type = "checkbox";
  railCb.checked = typeof railPref === "function" ? railPref() : store("manifest.rail.collapsed") === "1";
  railCb.onchange = () => { if (typeof setRailCollapsed === "function") setRailCollapsed(railCb.checked, true); showToast("Rail " + (railCb.checked ? "collapsed" : "expanded")); };
  railLab.append(railCb, el("span", "", "collapse the rail to its icon strip (desktop)"));
  row("manifest.rail.collapsed", railLab);

  // 2. files browser prefs (manifest.filesPrefs)
  const fp = el("span", "set-kv-val");
  let prefs = {};
  try { prefs = JSON.parse(store("manifest.filesPrefs") || "{}"); } catch (e) {}
  fp.append(el("span", "", Object.keys(prefs).length ? Object.entries(prefs).map(([k, v]) => k + "=" + v).join(" · ") : "defaults"), " ");
  fp.append(pillLight("reset", () => {
    drop("manifest.filesPrefs");
    if (typeof fsPrefs !== "undefined") Object.assign(fsPrefs, { sortKey: "name", sortDir: 1, view: "list", showHidden: false });
    showToast("Files browser preferences reset");
    renderSettings();
  }));
  row("manifest.filesPrefs", fp);

  // 3. terminal stage (manifest.termStage)
  const stage = selectEl(["term", "files", "activity"]);
  stage.value = store("manifest.termStage") || "term";
  stage.onchange = () => { try { localStorage.setItem("manifest.termStage", stage.value); } catch (e) {} showToast("Terminal opens on " + stage.value); };
  row("manifest.termStage", stage);

  // 4. command-bar recents (manifest.cmd.recents)
  const rc = el("span", "set-kv-val");
  let recents = [];
  try { recents = JSON.parse(store("manifest.cmd.recents") || "[]") || []; } catch (e) {}
  rc.append(el("span", "", recents.length + " recent command" + (recents.length === 1 ? "" : "s")), " ");
  rc.append(pillLight("clear", () => { drop("manifest.cmd.recents"); showToast("Command-bar recents cleared"); renderSettings(); }));
  row("manifest.cmd.recents", rc);

  pane.append(g);
  pane.append(el("div", "portal-note", "Theme: not built — the portal SPA has its own."));
}

// ================= parked: the Agents › SETTINGS chip panels (Phase 0) =================
// Reached via #/agents/settings until a later phase removes the chip.
// `spPortalRows` and the chip badge stay in 40-agents.js (shell chrome).

// ---- PORTALS sub-tab: every external realm, (re)connectable in place ----
async function loadPortals() {
  if (els.settingsView && !els.settingsView.hidden) { loadSettingsConnections(); return; } // the tab owns the rows now
  const host = document.getElementById("portalList"); if (!host) return;
  if (!host.children.length) host.textContent = "loading…";
  try {
    const rows = (await (await fetch("/api/portals")).json()).rows || [];
    renderPortals(rows);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Portals unavailable.")); }
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

// renderPortals — the parked Settings→Portals pane: Connections over Conduits.
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

// ---- the parked chip page: Portals · Chargebook · Harnesses behind the inner rail ----
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
  // the app-wide tab is where these live now — say so on the way out
  const link = el("button", "aion-org-item");
  link.append(el("span", "", "app Settings →"));
  link.onclick = () => { location.hash = "#/settings/connections"; };
  rail.append(link);
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

// The chargebook form (SPIRITS.md §4 Settings): the default every keyless
// ritual inherits + one row per price.*/cast.* key. Values compared against
// the record for a derived dirty bar; save = line surgery → the lint-gated
// PUT; the board's inherited ceilings re-derive after.
//
// opts.budget (Settings › Agents › Budget): the default ceiling is level one,
// prices + casts fold behind a collapsed "advanced" section (progressive
// disclosure level two, never level one).
async function renderChargebookPane(pane, opts) {
  opts = opts || {};
  if (!opts.budget) pane.append(el("div", "pp-section-head", "CHARGEBOOK"));
  let raw = "";
  try { raw = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent("chargebook.md"))).json()).content || ""; }
  catch (e) { pane.append(emptyRow("chargebook.md unavailable")); return; }
  const record = splitFM(raw);
  const keys = record.fmLines
    .map((ln) => ln.match(/^([A-Za-z0-9_.-]+):\s*(.*)$/))
    .filter(Boolean)
    .map((m) => ({ key: m[1], val: m[2].trim() }));
  const open = {};
  keys.forEach((k) => { open[k.key] = k.val; });
  const rerender = () => (opts.budget ? renderSettings() : renderSpiritSettings());

  const lint = el("div", "editor-lint");
  lint.hidden = true;
  const bar = derivedDirtyBar(pane, {
    compute: () => {
      const dirty = keys.some((k) => open[k.key] !== k.val);
      return { dirty, blocked: false, msg: dirty ? "unsaved changes · lint runs on save" : "no changes" };
    },
    onSave: async () => {
      let fm = record.fmLines;
      keys.forEach((k) => { if (open[k.key] !== k.val) fm = fmSurgery(fm, k.key, open[k.key]); });
      const content = "---\n" + fm.join("\n") + "\n---\n" + record.body;
      lint.hidden = true; lint.innerHTML = "";
      const r = await fetch("/api/spirits/file?path=" + encodeURIComponent("chargebook.md"), {
        method: "PUT", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content }),
      });
      const res = await r.json();
      if (r.status === 422 || res.ok === false) {
        lint.hidden = false;
        (res.errors || ["save blocked"]).forEach((m) => lint.append(el("div", "lint-err", "✕ " + m)));
        return;
      }
      showToast("Chargebook saved — inherited ceilings re-derive");
      if (typeof loadSpiritRituals === "function") loadSpiritRituals();
      rerender();
    },
    onDiscard: rerender,
  });
  pane.append(lint);

  const section = (host, label) => host.append(el("div", "aion-section-note", label));
  const rowFor = (k, label) => {
    const row = el("div", "cb-row");
    row.append(el("span", "cb-key", label || k.key));
    const input = el("input", "pp-in cb-in");
    input.value = open[k.key];
    input.oninput = () => { open[k.key] = input.value.trim(); bar.refresh(); };
    row.append(input);
    return row;
  };
  const def = keys.find((k) => k.key === "default_run_ceiling_usd");
  const prices = keys.filter((k) => k.key.startsWith("price."));
  const casts = keys.filter((k) => k.key.startsWith("cast."));

  const grid = el("div", "cb-grid");
  if (def) {
    section(pane, "the ceiling every keyless ritual inherits (USD)");
    grid.append(rowFor(def, "default_run_ceiling_usd"));
  } else if (opts.budget) {
    grid.append(emptyRow("chargebook.md has no default_run_ceiling_usd"));
  }
  pane.append(grid);

  // prices + casts: level one on the parked chip page, level two here
  let advGrid = grid;
  if (opts.budget) {
    const body = collapsibleSection(pane, "advanced — model prices & cast costs",
      prices.length + " price" + (prices.length === 1 ? "" : "s") + " · " + casts.length + " cast" + (casts.length === 1 ? "" : "s"), false);
    advGrid = el("div", "cb-grid");
    body.append(advGrid);
    body.append(el("div", "portal-note", "File: chargebook.md — prices in $/mtok, casts in base $ per call"));
  }
  if (prices.length) {
    advGrid.append(el("div", "cb-group", "PRICES — $/mtok"));
    prices.forEach((k) => advGrid.append(rowFor(k)));
  }
  if (casts.length) {
    advGrid.append(el("div", "cb-group", "CASTS — base $ per call"));
    casts.forEach((k) => advGrid.append(rowFor(k)));
  }
  if (!opts.budget) {
    const rawB = el("button", "sprt-quiet", "⌘/ edit raw");
    rawB.onclick = () => openEditor(["chargebook.md"]);
    pane.append(rawB);
  }
  bar.refresh();
}

// ================= the shared row renderer =================
const PORTAL_STATE_LABEL = { open: "open", degraded: "degraded", sealed: "—" };

function portalRowEl(p) {
  const wrap = el("div", "portal-wrap");
  const row = el("div", "portal-row state-" + p.state);
  row.dataset.portalId = p.id;
  row.append(el("span", "portal-name", p.name));
  const st = el("span", "portal-state", PORTAL_STATE_LABEL[p.state] || p.state);
  row.append(st);
  row.append(el("span", "portal-cross", portalCrossing(p)));
  // the key column: a redacted preview, or the environment variable's NAME
  row.append(el("span", "portal-key", p.env && p.kind !== "env" ? "env: " + p.env : (p.masked || (p.kind === "oauth" ? "oauth" : "—"))));
  const acts = el("span", "portal-acts");
  buildPortalActions(p, acts, wrap);
  row.append(acts);
  wrap.append(row);
  if (p.state === "degraded" && p.err) wrap.append(el("div", "portal-err", p.err));
  else if (p.state === "sealed" && p.err) wrap.append(el("div", "portal-err", p.err));
  if ((p.kind === "oauth" || p.kind === "effector" || p.kind === "gmailsend") && (p.accounts || []).length) {
    wrap.append(el("div", "portal-note", "connected: " + p.accounts.join(", ")));
  } else if (p.note && p.state !== "degraded") {
    wrap.append(el("div", "portal-note", p.note));
  }
  return wrap;
}

function portalCrossing(p) {
  if (p.kind === "llm") return "via engine";
  if (p.kind === "env" || p.kind === "info") return p.state === "open" ? "config" : "—";
  if (!p.lastCrossing) return p.state === "sealed" ? "not connected" : "—";
  const d = new Date(p.lastCrossing);
  if (isNaN(d)) return "—";
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  const t = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).replace(" ", "");
  return sameDay ? t + " today" : d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function buildPortalActions(p, acts, wrap) {
  const dim = (text) => acts.append(el("span", "portal-dim", text));
  // read-only kinds: environment-supplied credentials + config projections
  if (p.kind === "env" || (p.env && p.kind === "apikey")) { dim("set via environment (" + p.env + ")"); return; }
  if (p.kind === "info") { dim("config.json"); return; }
  if (p.kind === "apikey") {
    if (p.state === "sealed") {
      acts.append(pillLight("connect", () => togglePortalForm(p, wrap)));
      return;
    }
    acts.append(pillLight("test", () => portalAction("/api/portals/" + p.id + "/test", wrap)));
    // engine-managed portals (heypocket) are polled by the excalibur ritual, not manifest
    if (!p.engine) acts.append(pillLight("poll", () => portalAction("/api/portals/" + p.id + "/poll", wrap)));
    acts.append(
      pillLight("replace", () => togglePortalForm(p, wrap)),
      armedPill("disconnect", p.engine ? "remove key?" : "disconnect — cached items stay?",
        () => portalAction("/api/portals/" + p.id + "/disconnect", wrap)),
    );
    return;
  }
  if (p.kind === "oauth") {
    if (p.id === "gmail") {
      // multi-account, read-only. The accounts panel holds per-account
      // sync/extract/workspace routing + the paste-back connect flow.
      const label = p.state === "degraded" ? "reconnect" : "accounts";
      const b = p.state === "degraded" ? el("button", "pill-solid", label) : pillLight(label, () => toggleGmailAccountsPanel(wrap));
      if (p.state === "degraded") b.onclick = () => toggleGmailAccountsPanel(wrap);
      acts.append(b);
      return;
    }
    // Google Calendar — the ONE paste-back flow; per-account disconnect.
    (p.accounts || []).forEach((email) => {
      acts.append(armedPill("disconnect " + email, "disconnect " + email + "?", () => portalDisconnectCalendar(email)));
    });
    acts.append(pillLight((p.accounts || []).length ? "add account" : "connect", () => connectFlow(wrap, {
      startUrl: "/api/calendar/connect/start", finishUrl: "/api/calendar/connect/finish",
      onDone: (r) => { showToast("Connected " + r.connected, null, "info"); reloadConnections(); },
    })));
    return;
  }
  if (p.kind === "gmailsend") {
    const x = p.extra || {};
    if (!x.hasCreds && !x.sendCapable) { dim("add credentials first"); return; }
    acts.append(pillLight(x.sendCapable ? "reconnect" : "connect", () => connectFlow(wrap, {
      startUrl: "/api/settings/gmail-send/connect/start", finishUrl: "/api/settings/gmail-send/connect/finish",
      onDone: (row) => { showToast(row.state === "open" ? "Sender connected" : "Connected, but not send-capable — " + (row.err || ""), null, "info"); replaceRow(wrap, row); },
    })));
    if (x.sendCapable || p.state === "degraded") {
      acts.append(armedPill("disconnect", "disconnect — outreach sends stop?", () => portalAction("/api/settings/gmail-send/disconnect", wrap)));
    }
    return;
  }
  if (p.kind === "bankfeed") {
    const x = p.extra || {};
    if (!x.available) { dim("unavailable"); return; }
    if (!x.claimed) { acts.append(pillLight("claim token", () => toggleBankClaimForm(wrap))); return; }
    acts.append(pillLight("accounts", () => toggleBankAccountsPanel(wrap)));
    acts.append(pillLight("sync now", async () => {
      try {
        const r = await postJSONOk("/api/bankfeed/sync", {});
        showToast("Synced — " + (r.added || 0) + " new row(s)" + (r.autoApplied ? " · " + r.autoApplied + " reconciled" : "") + ((r.added || 0) > 0 ? " → $ tab" : ""));
        reloadConnections();
      } catch (e) { showToast("Sync failed — " + (e.message || ""), null, "error"); }
    }));
    return;
  }
  if (p.kind === "effector") {
    // acts OUT via a local CLI (errands-aside §1) — nothing to connect here.
    dim("local CLI");
    return;
  }
  // llm — read-only, managed by the engine
  dim("engine");
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
    const label = el("label", "pp3-lform-field");
    label.append(el("span", "pp3-lform-label", f.label));
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
      replaceRow(wrap, row);
      showToast(row.state === "open" ? p.name + " connected" : p.name + " saved — " + (row.err || row.state), null, row.state === "open" ? "info" : undefined);
    } catch (e) { save.disabled = false; save.textContent = "save & test"; showToast("Couldn't save " + p.name); }
  };
  form.append(save);
  wrap.append(form);
  const first = form.querySelector("input"); if (first) first.focus();
}

// portalAction posts an action and swaps the row for the server's re-derived
// one (the wrap when the caller has it; else by id in whichever list is up).
async function portalAction(url, wrap) {
  try {
    const row = await postJSON(url, {});
    if (!wrap) {
      const host = document.getElementById("settingsConnList") || document.getElementById("portalList");
      wrap = host && host.querySelector(`[data-portal-id="${CSS.escape(row.id)}"]`)?.closest(".portal-wrap");
    }
    replaceRow(wrap, row);
    refreshFeedBadge();
  } catch (e) { showToast("Portal action failed", null, "error"); }
}

// Calendar keeps its own OAuth endpoints — the portal row drives disconnect,
// then reloads the rows so the state reflects the change.
async function portalDisconnectCalendar(email) {
  try { await postJSONOk("/api/calendar/disconnect", { account: email }); showToast("Disconnected " + email); }
  catch (e) { showToast("Couldn't disconnect — " + (e.message || "error"), null, "error"); }
  reloadConnections();
}

// Gmail read-only OAuth, multi-account — manifest mints the tokens the
// excalibur email-sync + EA digest read. The panel lists every connected
// mailbox with its routing (sync / extraction workspace) behind ONE derived
// dirty bar (the one form in Connections), hosts disconnect per account, and
// the paste-back connect flow.
async function toggleGmailAccountsPanel(wrap) {
  const existing = wrap.querySelector(".portal-form");
  if (existing) { existing.remove(); return; }
  const form = el("div", "portal-form gmail-accounts");
  wrap.append(form);
  await renderGmailAccounts(form);
}

async function renderGmailAccounts(form) {
  form.innerHTML = "";
  let accounts = [];
  try { accounts = (await (await fetch("/api/gmail/accounts")).json()).accounts || []; }
  catch (e) { form.append(el("div", "portal-err", "couldn't load accounts")); return; }

  const orig = accounts.map((a) => ({ ...a }));
  const open = accounts.map((a) => ({ ...a }));
  const changed = () => open.filter((a, i) =>
    !!a.sync !== !!orig[i].sync || !!a.extract !== !!orig[i].extract || (a.workspace || "") !== (orig[i].workspace || ""));
  const rows = el("div", "");
  form.append(rows);
  const bar = derivedDirtyBar(form, {
    compute: () => {
      const n = changed().length;
      return { dirty: n > 0, blocked: false, msg: n ? n + " account routing change" + (n === 1 ? "" : "s") + " unsaved" : "no changes" };
    },
    onSave: async () => {
      try {
        for (const a of changed()) {
          await postJSONOk("/api/gmail/accounts/set", { email: a.email, sync: !!a.sync, extract: !!a.extract, workspace: a.workspace || "" });
        }
        showToast("Routing saved", null, "info");
        renderGmailAccounts(form);
      } catch (e) { showToast("Couldn't save — " + (e.message || "error"), null, "error"); }
    },
    onDiscard: () => renderGmailAccounts(form),
  });

  open.forEach((a) => {
    const row = el("div", "gmail-acct-row");
    const head = el("div", "gmail-acct-head");
    head.append(el("span", "gmail-acct-email", a.email));
    if (a.primary) head.append(el("span", "gmail-acct-primary", "primary"));
    if (a.needsReauth) head.append(el("span", "portal-err", "sign-in expired"));
    row.append(head);

    const ctl = el("div", "gmail-acct-ctl");
    const mkToggle = (label, key, title) => {
      const lab = el("label", "gmail-acct-toggle");
      const cb = el("input", "");
      cb.type = "checkbox";
      cb.checked = !!a[key];
      cb.title = title;
      cb.onchange = () => { a[key] = cb.checked; bar.refresh(); };
      lab.append(cb, el("span", "", label));
      return lab;
    };
    ctl.append(mkToggle("sync", "sync", "mirror this mailbox's known-contact threads into the vault"));
    ctl.append(mkToggle("extract", "extract", "pre-tag new thread notes with the workspace category so confirming auto-extracts"));
    const wsSel = document.createElement("select");
    wsSel.className = "gmail-acct-ws";
    [["", "— no workspace"], ["aion", "AION"], ["real-estate", "Real Estate"]].forEach(([v, label]) => {
      const o = document.createElement("option");
      o.value = v; o.textContent = label;
      wsSel.append(o);
    });
    wsSel.value = a.workspace || "";
    wsSel.onchange = () => { a.workspace = wsSel.value; bar.refresh(); };
    ctl.append(wsSel);
    // disconnect is an ACTION (immediate, armed), not part of the form
    ctl.append(armedPill("disconnect", a.primary ? "disconnect — digest stops?" : "disconnect — sure?", async () => {
      try { await postJSONOk("/api/gmail/accounts/disconnect", { email: a.email }); showToast("Disconnected " + a.email); }
      catch (e) { showToast("Couldn't disconnect — " + (e.message || "error"), null, "error"); }
      renderGmailAccounts(form);
      reloadConnections();
    }));
    row.append(ctl);
    rows.append(row);
  });
  if (!open.length) rows.append(el("div", "portal-note", "no Google accounts connected yet"));

  // paste-back connect flow
  const add = el("div", "gmail-acct-add");
  const start = pillLight(open.length ? "connect another account" : "connect account", async () => {
    try {
      const r = await postJSONOk("/api/gmail/connect/start", {});
      window.open(r.authUrl, "_blank", "noopener");
      start.replaceWith(pasteBackBox("/api/gmail/connect/finish", (res) => {
        showToast("Connected " + res.connected, null, "info");
        renderGmailAccounts(form);
        reloadConnections();
      }));
      showToast("Approve in the Google tab, then paste the address it lands on", null, "info");
    } catch (e) { showToast("Couldn't start sign-in — " + (e.message || "error"), null, "error"); }
  });
  add.append(start);
  form.append(add);
  bar.refresh();
}

// ---- BANK FEED panel (moved from Properties › Settings, agents plan §5) — claim
// once, then link each bridge account to the entity that owns it. dataDir keeps the machine linkage; the entity record's
// accounts row flips not-connected → live through the normal entity save.
async function renderBankFeedPanel(box, ents) {
  box.innerHTML = "";
  box.append(el("div", "pp-empty", "loading…"));
  let d = null;
  try { d = await (await fetch("/api/bankfeed/accounts")).json(); } catch (e) {}
  box.innerHTML = "";
  if (!d) { box.append(el("div", "pp-empty", "Bank feed unavailable.")); return; }

  // saveEntityAccountState upserts the label row on the entity record (the
  // user-action vault write that makes the binding owner-visible).
  const saveAcctState = async (entitySlug, label, state) => {
    const ent = ents.find((x) => x.slug === entitySlug);
    if (!ent || !label) return;
    const accs = (ent.accounts || []).map((a) => ({ ...a }));
    const hit = accs.find((a) => a.label.toLowerCase() === label.toLowerCase());
    if (hit) hit.state = state;
    else accs.push({ label, kind: "operating", state });
    await postJSONOk("/api/realestate/entities/" + encodeURIComponent(entitySlug) + "/save", { accounts: accs });
    ent.accounts = accs;
  };

  if (!d.claimed) {
    box.append(el("div", "aion-section-note",
      "Link real bank accounts through SimpleFIN: create a bridge account, generate ONE setup token, and paste it here. " +
      "The claim is one-time — it mints a read-only access URL stored on this box only. Transactions land in the $ tab with the entity pre-set."));
    const row = el("div", "set-bind-row");
    const inp = inputEl("SimpleFIN setup token…");
    row.append(inp);
    row.append(pillLight("claim", async () => {
      const token = (inp.value || "").trim();
      if (!token) { showToast("Paste the setup token first"); return; }
      try {
        await postJSONOk("/api/bankfeed/claim", { token });
        showToast("Claimed — link each account to its entity below");
        renderBankFeedPanel(box, ents);
      } catch (err) { showToast("Claim failed — " + (err.message || "")); }
    }));
    box.append(row);
    return;
  }

  const strip = el("div", "set-bind-row");
  const nAcct = (d.accounts || []).length;
  const nLinked = (d.accounts || []).filter((a) => a.link).length;
  strip.append(el("span", "stmt-vendor",
    nAcct + (nAcct === 1 ? " account" : " accounts") + " on the bridge · " + nLinked + " linked · daily sync"));
  strip.append(pillLight("sync now", async () => {
    try {
      const r = await postJSONOk("/api/bankfeed/sync", {});
      showToast("Synced — " + (r.added || 0) + " new row(s)" +
        (r.autoApplied ? " · " + r.autoApplied + " reconciled" : "") +
        ((r.added || 0) > 0 ? " → $ tab" : ""));
      renderBankFeedPanel(box, ents);
    } catch (err) { showToast("Sync failed — " + (err.message || "")); }
  }));
  box.append(strip);

  if (!(d.accounts || []).length) {
    box.append(el("div", "pp-empty", "The bridge reports no accounts yet — connect a bank on the SimpleFIN side, then sync."));
    return;
  }
  if (!propertyCache || !propertyCache.length) await loadProperties();
  const propSlugs = (propertyCache || []).map((p) => p.slug);

  // banks mangle their own names (encoding replacement chars, duplicated
  // last-fours) — display cleans, the link payload keeps the raw name
  const cleanName = (s) => {
    const t = (s || "").replace(/�/g, "").replace(/\s+/g, " ").trim();
    // banks repeat the last-four: "EVERYDAY CHECKING …6631 (6631)" — drop the
    // parenthetical only when its digits already appear in the name
    return t.replace(/\s*\((\d{2,4})\)$/, (m, d) => (t.slice(0, t.length - m.length).includes(d) ? "" : m));
  };
  const fmtBal = (s) => {
    const v = parseFloat(s);
    if (!isFinite(v)) return "";
    return (v < 0 ? "−$" : "$") + Math.abs(v).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  };

  // one account = one compact row; the entity-binding editor folds out
  // behind the row's action so nine accounts read as a table, not nine forms
  const acctRow = (a) => {
    const link = a.link || null;
    const wrap = el("div", "re-bank-acct");
    const row = el("div", "re-bank-row");
    const stack = el("div", "re-bank-stack");
    stack.append(el("span", "re-bank-name", cleanName(a.name)));
    const entName = link ? ((ents.find((x) => x.slug === link.entitySlug) || {}).name || link.entitySlug) : "";
    if (link) stack.append(el("span", "re-bank-ent",
      "→ " + entName + (link.defaultProperty ? " · " + link.defaultProperty : "")));
    row.append(stack);
    row.append(el("span", "re-bank-bal", fmtBal(a.balance)));
    if (link && link.lastError) row.append(el("span", "re-acct-state st-needs-reauth", "needs re-auth"));
    else if (link) row.append(el("span", "re-acct-state " + (link.enabled ? "st-live" : "st-not-connected"), link.enabled ? "live" : "paused"));
    else row.append(el("span", "re-acct-state st-not-connected", "not linked"));

    const buildEditor = () => {
      const ed = el("div", "re-bank-ed");
      let entitySlug = link ? link.entitySlug : "";
      const ac = recordAutocomplete("entity", "owning entity…", (rec) => { entitySlug = rec.slug; });
      if (link) ac.setValue(entName);
      const labelIn = inputEl("bank + last four, e.g. Midwest ····4821");
      labelIn.value = link ? link.accountLabel : cleanName(a.name);
      const propSel = selectEl([""].concat(propSlugs));
      if (link && link.defaultProperty) propSel.value = link.defaultProperty;
      const field = (name, ctl, hint) => {
        const f = el("div", "re-bank-field");
        const l = el("span", "re-bank-flabel", name);
        if (hint) { l.title = hint; ctl.title = hint; }
        f.append(l, ctl);
        return f;
      };
      ed.append(
        field("owning entity", ac.el, "transactions file under this entity's books"),
        field("account row", labelIn, "the row name on the entity record"),
        field("default property", propSel, "prefills the assignment on new rows — never auto-applies by itself"),
      );
      const acts = el("div", "re-bank-acts");
      acts.append(pillLight(link ? "save" : "link", async () => {
        const label = (labelIn.value || "").trim();
        if (!entitySlug) { showToast("Pick the owning entity first"); return; }
        if (!label) { showToast("Name the account row (e.g. Midwest ····4821)"); return; }
        try {
          await postJSONOk("/api/bankfeed/accounts/" + encodeURIComponent(a.id), {
            entitySlug, accountLabel: label, defaultProperty: propSel.value,
            enabled: true, orgName: a.org, accountName: a.name,
          });
          await saveAcctState(entitySlug, label, "live");
          showToast("Linked — transactions land in the $ tab under " + entitySlug);
          bankFeedRerender();
        } catch (err) { showToast("Couldn't link — " + (err.message || "")); }
      }));
      if (link) {
        acts.append(pillLight(link.enabled ? "pause" : "resume", async () => {
          try {
            await postJSONOk("/api/bankfeed/accounts/" + encodeURIComponent(a.id), {
              entitySlug: link.entitySlug, accountLabel: link.accountLabel,
              defaultProperty: link.defaultProperty || "", enabled: !link.enabled,
              orgName: a.org, accountName: a.name,
            });
            bankFeedRerender();
          } catch (err) { showToast("Couldn't update"); }
        }));
        acts.append(pillLight("unlink", async () => {
          try {
            await postJSONOk("/api/bankfeed/accounts/" + encodeURIComponent(a.id), { entitySlug: "" });
            await saveAcctState(link.entitySlug, link.accountLabel, "not-connected");
            showToast("Unlinked");
            bankFeedRerender();
          } catch (err) { showToast("Couldn't unlink"); }
        }));
        // full-history backfill: everything the bridge holds for this account
        // lands in the $ tab for hand categorization — never auto-applied
        const bf = pillLight("pull full history", async () => {
          bf.disabled = true;
          bf.textContent = "pulling…";
          try {
            const r = await postJSONOk("/api/bankfeed/accounts/" + encodeURIComponent(a.id) + "/backfill", {});
            showToast("History pulled — " + (r.added || 0) + " new row(s) of " + (r.fetched || 0) +
              " fetched → $ tab (paged, 50/screen)");
          } catch (err) { showToast("Backfill failed — " + (err.message || "")); }
          bankFeedRerender();
        });
        acts.append(bf);
      }
      ed.append(acts);
      return ed;
    };

    let ed = null;
    const act = el("button", "re-acct-act", link ? "manage" : "link…");
    act.onclick = () => {
      if (!ed) { ed = buildEditor(); row.after(ed); act.textContent = "close"; return; }
      const open = ed.style.display !== "none";
      ed.style.display = open ? "none" : "";
      act.textContent = open ? (link ? "manage" : "link…") : "close";
    };
    row.append(act);
    wrap.append(row);

    if (link && (link.lastSync || link.lastError)) {
      const note = el("div", "re-bank-note" + (link.lastError ? " attn" : ""));
      note.append(document.createTextNode(
        (link.lastSync ? "last sync " + fmtWhen(link.lastSync) : "") +
        (link.lastError ? (link.lastSync ? " · " : "") + link.lastError + " — " : "")));
      if (link.lastError) {
        const fix = document.createElement("a");
        fix.href = "https://beta-bridge.simplefin.org/";
        fix.target = "_blank";
        fix.rel = "noopener";
        fix.textContent = "re-authorize on the bridge →";
        note.append(fix);
      }
      wrap.append(note);
    }
    return wrap;
  };

  // one card per institution (the registry convention: bordered card, name
  // head, compact rows) — attention floats to the top, then linked banks
  const groups = new Map();
  (d.accounts || []).forEach((a) => {
    const k = a.org || "Other";
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(a);
  });
  const attnIn = (as) => as.filter((x) => x.link && x.link.lastError).length;
  const liveIn = (as) => as.filter((x) => x.link).length;
  const cards = el("div", "re-bank-cards");
  [...groups.entries()]
    .sort((x, y) => (attnIn(y[1]) - attnIn(x[1])) || (liveIn(y[1]) - liveIn(x[1])) || x[0].localeCompare(y[0]))
    .forEach(([org, accts]) => {
      const card = el("div", "re-bank-card");
      const head = el("div", "re-bank-head");
      head.append(el("span", "wv-addr", org));
      const linked = liveIn(accts);
      // the row already chips a lone account — the head chip is a rollup
      if (attnIn(accts) && accts.length > 1) head.append(el("span", "re-acct-state st-needs-reauth", "needs re-auth"));
      head.append(el("span", "re-acct-rollup" + (attnIn(accts) ? " attn" : ""),
        linked ? linked + " of " + accts.length + " linked" : "not linked"));
      card.append(head);
      accts
        .sort((x, y) => ((y.link ? 1 : 0) - (x.link ? 1 : 0)) || cleanName(x.name).localeCompare(cleanName(y.name)))
        .forEach((a) => card.append(acctRow(a)));
      cards.append(card);
    });
  box.append(cards);
}

