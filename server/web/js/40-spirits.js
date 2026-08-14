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

// ---- SPIRIT PAGE (SPIRITS.md §4): identity + cornerstone capability
// editing against the grimoire catalogs, its rituals, its memories, one
// derived dirty bar. The fail-closed warnings mirror lintCornerstoneFM and
// update LIVE as chips change. ----
let spPg = null; // { name, identity:{record,body}, corner:{record,portal,spellbooks,writable}, catalog }

function fmList(s) {
  return (s || "").replace(/^\[|\]$/g, "").split(",").map((x) => x.trim()).filter(Boolean);
}
function fmListOut(list) { return "[" + list.join(", ") + "]"; }

function parseCorner(raw) {
  const rec = splitFM(raw);
  const portalLine = rec.fmLines.find((ln) => /^portal::?/.test(ln)) || "";
  return {
    record: { raw, fmLines: rec.fmLines, body: rec.body },
    portal: portalLine.replace(/^portal::?\s*/, "").trim(),
    spellbooks: fmList(fmValue(rec.fmLines, "available_spellbooks")),
    writable: fmList(fmValue(rec.fmLines, "writable")),
  };
}
function serializeCorner(c) {
  let fm = c.record.fmLines.map((ln) => (/^portal::?/.test(ln) ? "portal:: " + c.portal : ln));
  if (!fm.some((ln) => /^portal::?/.test(ln))) fm.unshift("portal:: " + c.portal);
  fm = fmSurgery(fm, "available_spellbooks", fmListOut(c.spellbooks));
  fm = fmSurgery(fm, "writable", fmListOut(c.writable));
  return "---\n" + fm.join("\n") + "\n---\n" + c.record.body;
}

async function renderSpiritPage(name) {
  const host = document.getElementById("spSpiritWrap");
  if (!host) return;
  host.innerHTML = "loading…";
  const get = (p) => fetch("/api/spirits/file?path=" + encodeURIComponent(p)).then((r) => (r.ok ? r.json() : null)).catch(() => null);
  const [idF, coF, catalog, mems, rits] = await Promise.all([
    get("spirits/" + name + "/identity.md"),
    get("spirits/" + name + "/cornerstone.md"),
    fetch("/api/spirits/catalog").then((r) => r.json()).catch(() => ({ portals: [], spellbooks: [] })),
    fetch("/api/spirits/memories?spirit=" + encodeURIComponent(name)).then((r) => r.json()).then((d) => d.data || []).catch(() => []),
    fetch("/api/spirits/rituals").then((r) => r.json()).then((d) => (d.data || []).filter((x) => x.spirit === name)).catch(() => []),
  ]);
  host.innerHTML = "";
  if (!idF || !coF) {
    // non-primary-harness spirit (or missing files): read-only stance —
    // writes are primary-only by the federation contract
    host.append(el("div", "sprt-head")).append(el("span", "sprt-title", name));
    host.append(emptyRow("This spirit's files aren't editable from here (non-primary harness or missing records)."));
    return;
  }
  const idRec = splitFM(idF.content || "");
  spPg = {
    name,
    identity: { record: { raw: idF.content || "", fmLines: idRec.fmLines, body: idRec.body }, body: idRec.body },
    corner: parseCorner(coF.content || ""),
    catalog, mems, rits,
  };
  paintSpiritPage(host);
}

function spPageDirty() {
  const idDirty = spPg.identity.body !== spPg.identity.record.body;
  const coDirty = serializeCorner(spPg.corner) !== spPg.corner.record.raw;
  return { idDirty, coDirty, dirty: idDirty || coDirty };
}

function paintSpiritPage(host) {
  host.innerHTML = "";
  const { name, corner, catalog } = spPg;

  const head = el("div", "sprt-head");
  head.append(el("span", "sprt-title", name), el("span", "sprt-sub", "spirits/" + name + "/"));
  const acts = el("span", "sprt-head-acts");
  const addR = el("button", "sprt-ghost", "＋ ritual");
  addR.onclick = () => newRitual(name);
  const rawB = el("button", "sprt-quiet", "⌘/ raw");
  rawB.onclick = () => openSpiritEditor(name);
  const nRits = (spPg.rits || []).length;
  const del = armedDelete("delete spirit",
    "deletes " + nRits + " ritual" + (nRits === 1 ? "" : "s") + " + memories — confirm?",
    async () => {
      try {
        const r = await fetch("/api/spirits/spirit/delete", { method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name }) });
        if (!r.ok) throw new Error(await r.text());
        showToast("Deleted " + name + " — git history is the undo");
        location.hash = "#/spirits";
      } catch (e) { showToast("Couldn't delete: " + (e.message || e), null, "error"); }
    });
  acts.append(addR, rawB, del);
  head.append(acts);
  host.append(head);

  const lint = el("div", "editor-lint");
  lint.hidden = true;
  const bar = derivedDirtyBar(host, {
    compute: () => {
      const d = spPageDirty();
      return {
        dirty: d.dirty, blocked: !corner.portal,
        msg: !corner.portal ? "can't save — the cornerstone must name a conduit"
          : d.dirty ? "unsaved changes · lint runs on save" : "no changes",
      };
    },
    onSave: () => saveSpiritPage(host, lint),
    onDiscard: () => renderSpiritPage(name),
  });
  host.append(lint);

  // IDENTITY — the persona body (frontmatter preserved via line surgery)
  host.append(el("div", "pp-section-head", "IDENTITY"));
  const idTa = el("textarea", "editor-area sp-identity");
  idTa.spellcheck = false;
  idTa.value = spPg.identity.body;
  idTa.addEventListener("input", () => { spPg.identity.body = idTa.value; bar.refresh(); });
  host.append(idTa);

  // CORNERSTONE — the capability: conduit · spellbooks · writable, with the
  // fail-closed warnings recomputed on every change (mirrors lintCornerstoneFM)
  host.append(el("div", "pp-section-head", "CORNERSTONE — capability"));
  const capBox = el("div", "sp-capability");
  host.append(capBox);
  const paintCap = () => {
    capBox.innerHTML = "";
    // conduit
    const row1 = el("div", "sp-cap-row");
    row1.append(el("span", "cadb-label", "conduit"));
    const sel = selectEl(catalog.portals || []);
    sel.className = "pp-in";
    if (corner.portal && !(catalog.portals || []).includes(corner.portal)) {
      const o = document.createElement("option"); o.value = corner.portal; o.textContent = corner.portal + " (missing def)"; sel.append(o);
    }
    sel.value = corner.portal;
    sel.onchange = () => { corner.portal = sel.value; paintCap(); bar.refresh(); };
    row1.append(sel);
    capBox.append(row1);
    // spellbook chips against the catalog
    const row2 = el("div", "sp-cap-row");
    row2.append(el("span", "cadb-label", "spellbooks"));
    corner.spellbooks.forEach((sb) => {
      const c = el("button", "cadb-chip on", sb + " ✕");
      c.onclick = () => { corner.spellbooks = corner.spellbooks.filter((x) => x !== sb); paintCap(); bar.refresh(); };
      row2.append(c);
    });
    const remaining = (catalog.spellbooks || []).filter((sb) => !corner.spellbooks.includes(sb));
    if (remaining.length) {
      const add = document.createElement("select");
      add.className = "pp-in cadb-add";
      const o0 = document.createElement("option"); o0.value = ""; o0.textContent = "＋ spellbook"; add.append(o0);
      remaining.forEach((sb) => { const o = document.createElement("option"); o.value = sb; o.textContent = sb; add.append(o); });
      add.onchange = () => { if (add.value) { corner.spellbooks = [...corner.spellbooks, add.value]; paintCap(); bar.refresh(); } };
      row2.append(add);
    }
    capBox.append(row2);
    // writable chips — widening beyond artifacts/questbook/own-memories reads ink
    const row3 = el("div", "sp-cap-row");
    row3.append(el("span", "cadb-label", "writable"));
    const own = "spirits/" + name + "/memories";
    const widened = (w) => {
      const seg = w.split("/")[0];
      return seg !== "artifacts" && seg !== "questbook" && !(w === own || w.startsWith(own + "/"));
    };
    corner.writable.forEach((w) => {
      const c = el("button", "cadb-chip on" + (widened(w) ? " widened" : ""), w + " ✕");
      c.onclick = () => { corner.writable = corner.writable.filter((x) => x !== w); paintCap(); bar.refresh(); };
      row3.append(c);
    });
    const suggestions = ["artifacts/runs", "artifacts/feed", "artifacts/library", "artifacts/approvals/pending", "questbook", own]
      .filter((w) => !corner.writable.includes(w));
    const add3 = document.createElement("select");
    add3.className = "pp-in cadb-add";
    const o0 = document.createElement("option"); o0.value = ""; o0.textContent = "＋ writable"; add3.append(o0);
    suggestions.forEach((w) => { const o = document.createElement("option"); o.value = w; o.textContent = w; add3.append(o); });
    add3.onchange = () => { if (add3.value) { corner.writable = [...corner.writable, add3.value]; paintCap(); bar.refresh(); } };
    row3.append(add3);
    capBox.append(row3);
    // LIVE fail-closed warnings (the lint's own words — spec §4)
    const warn = el("div", "sp-cap-warnings");
    if (!corner.spellbooks.length) warn.append(el("div", "sp-warn", "no spellbooks — this spirit can run but read nothing."));
    if (!corner.writable.length) warn.append(el("div", "sp-warn", "writable is empty — this spirit can write nothing (fails closed)."));
    corner.writable.filter(widened).forEach((w) =>
      warn.append(el("div", "sp-warn ink", "writable includes " + w + " — outside artifacts/ and questbook/; the warden's next audit reviews this widening.")));
    if (warn.children.length) capBox.append(warn);
  };
  paintCap();

  // RITUALS — this spirit's rows (click → the structured editor)
  host.append(el("div", "pp-section-head", "RITUALS"));
  if (!spPg.rits.length) host.append(emptyRow("No rituals yet — ＋ ritual above."));
  else {
    const board = el("div", "ritual-board");
    spPg.rits.forEach((r) => board.append(ritualRow(r)));
    host.append(board);
  }

  // MEMORIES — names + counts only (they belong to the spirit)
  host.append(el("div", "pp-section-head", "MEMORIES"));
  if (!spPg.mems.length) host.append(emptyRow("No memories yet."));
  else {
    const memBox = el("div", "sp-mems");
    spPg.mems.forEach((m) => {
      const row = el("div", "sp-mem-row");
      row.append(el("span", "sp-mem-name", m.name));
      row.append(el("span", "sp-mem-meta",
        m.name === "long-term" ? (m.bytes + " bytes") : (m.files + " day file" + (m.files === 1 ? "" : "s"))));
      memBox.append(row);
    });
    host.append(memBox);
  }
  bar.refresh();
}

async function saveSpiritPage(host, lint) {
  const d = spPageDirty();
  lint.hidden = true; lint.innerHTML = "";
  const put = async (path, content) => {
    const r = await fetch("/api/spirits/file?path=" + encodeURIComponent(path), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content }),
    });
    const res = await r.json();
    if (r.status === 422 || res.ok === false) throw { lint: res };
    return res;
  };
  setSaveState("saving");
  try {
    let warns = [];
    if (d.idDirty) {
      const content = (spPg.identity.record.fmLines.length ? "---\n" + spPg.identity.record.fmLines.join("\n") + "\n---\n" : "") + spPg.identity.body;
      const res = await put("spirits/" + spPg.name + "/identity.md", content);
      spPg.identity.record = { raw: content, fmLines: spPg.identity.record.fmLines, body: spPg.identity.body };
      warns = warns.concat(res.warnings || []);
    }
    if (d.coDirty) {
      const content = serializeCorner(spPg.corner);
      const res = await put("spirits/" + spPg.name + "/cornerstone.md", content);
      spPg.corner = parseCorner(content);
      warns = warns.concat(res.warnings || []);
    }
    setSaveState("saved");
    if (warns.length) {
      lint.hidden = false; lint.classList.add("lint-ok");
      warns.forEach((m) => lint.append(el("div", "lint-warn", "⚠ " + m)));
    }
    paintSpiritPage(host);
  } catch (e) {
    setSaveState("error");
    if (e && e.lint) {
      lint.hidden = false;
      (e.lint.errors || ["save blocked"]).forEach((m) => lint.append(el("div", "lint-err", "✕ " + m)));
      (e.lint.warnings || []).forEach((m) => lint.append(el("div", "lint-warn", "⚠ " + m)));
    } else showToast("Save failed: " + (e.message || e), null, "error");
  }
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
    const drop = pillLight("disconnect", async () => {
      if (!confirm("Disconnect " + a.email + "?" + (a.primary ? " The waiting-on digest stops until you reconnect." : ""))) return;
      try { await postJSONOk("/api/gmail/accounts/disconnect", { email: a.email }); } catch (e) {}
      renderGmailAccounts(form);
      loadPortals();
    });
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
let liveIdleTicks = 0;      // consecutive polls with nothing active (grace before stop)
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
      }
      showToast(`${r.spirit}/${r.ritual} — ${r.outcome}${detail}`,
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
