// ================= SETTINGS (parked behind the agents SETTINGS chip) =================
// Split from 40-spirits.js (phase 0): renderSpiritSettings + the Portals,
// Chargebook, Harnesses and Gmail-accounts panels. Reached via #/spirits/settings
// until the app-wide SETTINGS tab lands (phase 1). `spPortalRows` and the chip
// badge stay in 40-agents.js (shell chrome).

// ---- PORTALS sub-tab: every external realm, (re)connectable in place ----
// The one place a connection is seen and repaired. Api-key portals (clickup,
// benchling) take a pasted key → save → auto-test; the oauth portal (calendar)
// runs its existing sign-in; the engine's LLM conduits are read-only. This is
// also the seed of app-wide settings — the row renderer is generic over the
// server's portal definition (fields drive the form), so a new source portal
// appears later as pure data.
async function loadPortals() {
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

// The chargebook form (SPIRITS.md §4 Settings): the default every keyless
// ritual inherits + one row per price.*/cast.* key. Values compared against
// the record for a derived dirty bar; save = line surgery → the lint-gated
// PUT; the board's inherited ceilings re-derive after.
async function renderChargebookPane(pane) {
  pane.append(el("div", "pp-section-head", "CHARGEBOOK"));
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
      loadSpiritRituals();
      renderSpiritSettings();
    },
    onDiscard: () => renderSpiritSettings(),
  });
  pane.append(lint);

  const section = (label) => pane.append(el("div", "aion-section-note", label));
  const grid = el("div", "cb-grid");
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
  if (def) {
    section("the ceiling every keyless ritual inherits (USD)");
    grid.append(rowFor(def, "default_run_ceiling_usd"));
  }
  const prices = keys.filter((k) => k.key.startsWith("price."));
  if (prices.length) {
    grid.append(el("div", "cb-group", "PRICES — $/mtok"));
    prices.forEach((k) => grid.append(rowFor(k)));
  }
  const casts = keys.filter((k) => k.key.startsWith("cast."));
  if (casts.length) {
    grid.append(el("div", "cb-group", "CASTS — base $ per call"));
    casts.forEach((k) => grid.append(rowFor(k)));
  }
  pane.append(grid);
  const rawB = el("button", "sprt-quiet", "⌘/ edit raw");
  rawB.onclick = () => openEditor(["chargebook.md"]);
  pane.append(rawB);
  bar.refresh();
}

const PORTAL_STATE_LABEL = { open: "open", degraded: "degraded", sealed: "—" };

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
    if (p.state === "sealed") {
      acts.append(pillLight("connect", () => togglePortalForm(p, wrap)));
      return;
    }
    acts.append(pillLight("test", () => portalAction("/api/portals/" + p.id + "/test")));
    // engine-managed portals (heypocket) are polled by the excalibur ritual, not manifest
    if (!p.engine) acts.append(pillLight("poll", () => portalAction("/api/portals/" + p.id + "/poll")));
    // arm-then-confirm (ui-conventions.md §buttons): no confirm(), the button
    // itself swaps to a second-click confirm state and auto-reverts.
    let disconnect;
    const armDisconnect = () => {
      const yes = pillLight(p.engine ? "remove key?" : "disconnect — cached items stay?",
        () => portalAction("/api/portals/" + p.id + "/disconnect"));
      yes.classList.add("armed");
      disconnect.replaceWith(yes);
      setTimeout(() => { if (yes.parentNode) yes.replaceWith(disconnect); }, 2500);
    };
    disconnect = pillLight("disconnect", armDisconnect);
    acts.append(
      pillLight("replace", () => togglePortalForm(p, wrap)),
      disconnect,
    );
    return;
  }
  if (p.kind === "oauth") {
    if (p.id === "gmail") {
      // multi-account, read-only. The accounts panel holds per-account
      // sync/extract/workspace routing + the paste-back connect flow.
      const label = p.state === "degraded" ? "reconnect" : "accounts";
      const pill = p.state === "degraded"
        ? el("button", "pill-solid", label)
        : pillLight(label, () => toggleGmailAccountsPanel(wrap));
      if (p.state === "degraded") pill.onclick = () => toggleGmailAccountsPanel(wrap);
      acts.append(pill);
      return;
    }
    // calendar connects from the CALENDAR tab (the headless paste-back flow);
    // this row only shows what is connected and lets one account go.
    if ((p.accounts || []).length) {
      p.accounts.forEach((email) => acts.append(pillLight("disconnect", () => portalDisconnectCalendar(email))));
    } else {
      acts.append(el("span", "portal-dim", "connect from CALENDAR"));
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

// Calendar keeps its own OAuth endpoints — the portal row drives disconnect,
// then reloads the panel so its state reflects the change.
async function portalDisconnectCalendar(email) {
  try { await postJSON("/api/calendar/disconnect", { account: email }); } catch (e) {}
  loadPortals();
}

// Gmail read-only OAuth, multi-account — manifest mints the tokens the
// excalibur email-sync + EA digest read. The panel lists every connected
// mailbox with its routing (sync / extraction workspace) and hosts the
// paste-back connect flow (manifest runs headless, so Google's localhost
// redirect can't reach it — the owner approves in their own browser and
// pastes the resulting URL back).
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

  accounts.forEach((a) => {
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
      cb.onchange = () => { a[key] = cb.checked; saveAcct(); };
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
    wsSel.onchange = () => { a.workspace = wsSel.value; saveAcct(); };
    ctl.append(wsSel);
    const saveAcct = async () => {
      try {
        await postJSONOk("/api/gmail/accounts/set", {
          email: a.email, sync: !!a.sync, extract: !!a.extract, workspace: a.workspace || "",
        });
        showToast(a.email + " routing saved", null, "info");
      } catch (e) { showToast("Couldn't save — " + (e.message || "error")); }
    };
    let drop;
    const armDrop = () => {
      const yes = pillLight(a.primary ? "disconnect — digest stops?" : "disconnect — sure?", async () => {
        try { await postJSONOk("/api/gmail/accounts/disconnect", { email: a.email }); } catch (e) {}
        renderGmailAccounts(form);
        loadPortals();
      });
      yes.classList.add("armed");
      drop.replaceWith(yes);
      setTimeout(() => { if (yes.parentNode) yes.replaceWith(drop); }, 2500);
    };
    drop = pillLight("disconnect", armDrop);
    ctl.append(drop);
    row.append(ctl);
    form.append(row);
  });
  if (!accounts.length) form.append(el("div", "portal-note", "no Google accounts connected yet"));

  // paste-back connect flow
  const add = el("div", "gmail-acct-add");
  const start = pillLight(accounts.length ? "connect another account" : "connect account", async () => {
    try {
      const r = await postJSONOk("/api/gmail/connect/start", {});
      window.open(r.authUrl, "_blank");
      start.replaceWith(buildPasteBack(form));
      showToast("Approve in the Google tab, then paste the address it lands on", null, "info");
    } catch (e) { showToast("Couldn't start sign-in — " + (e.message || "error")); }
  });
  add.append(start);
  form.append(add);
}

// buildPasteBack renders step 2 of the connect flow: the paste box + finish.
function buildPasteBack(form) {
  const box = el("div", "gmail-acct-paste");
  box.append(el("div", "portal-note", "after approving, the tab lands on an unreachable 127.0.0.1 page — copy its FULL address and paste it here"));
  const input = el("input", "portal-input");
  input.type = "text";
  input.placeholder = "http://127.0.0.1:8123/oauth/callback?state=…&code=…";
  input.spellcheck = false;
  const fin = el("button", "pill-solid", "finish connect");
  fin.onclick = async () => {
    fin.disabled = true; fin.textContent = "connecting…";
    try {
      const r = await postJSONOk("/api/gmail/connect/finish", { redirect: input.value });
      showToast("Connected " + r.connected, null, "info");
      renderGmailAccounts(form);
      loadPortals();
    } catch (e) {
      fin.disabled = false; fin.textContent = "finish connect";
      showToast("Connect failed — " + (e.message || "check the pasted URL").slice(0, 140));
    }
  };
  box.append(input, fin);
  return box;
}

