// ================= Agents · the spirit page =================
// Split from 40-spirits.js + 58-rituals.js (phase 0): the spirit index strip
// over the board, the spirit page (identity + cornerstone), and the raw
// markdown drawer (⌘/ raw — rituals / identity / cornerstone / chargebook).
// (renderRitualEditor — the structured editor — lives in 41-agents-schedule.js)

// The spirit index strip over the SCHEDULE board: one quiet row per spirit
// (name · N rituals — count derived from the board rows), click → the spirit
// page. `＋ spirit` lives on the Settings › Agents card (agents plan §4.3: the
// board stays a schedule); `＋ ritual` on the spirit page.
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
    b.onclick = () => { location.hash = "#/agents/" + encodeURIComponent(name); };
    host.append(b);
  });
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
        location.hash = "#/agents";
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

// ---- markdown editor drawer (rituals / identity / cornerstone / chargebook) ----
let editorState = null; // { files:[{path,loaded,content}], active }
function openSpiritEditor(sp) { openEditor([`spirits/${sp}/identity.md`, `spirits/${sp}/cornerstone.md`], 1); }
async function openEditor(paths, active = 0) {
  editorState = { files: paths.map((p) => ({ path: p, loaded: null })), active };
  els.spiritEditor.hidden = false;
  await selectEditorFile(active);
  els.spiritEditor.scrollIntoView({ behavior: "smooth", block: "nearest" });
}
async function selectEditorFile(i) {
  editorState.active = i;
  const f = editorState.files[i];
  renderEditorTabs();
  els.spiritEditorLint.hidden = true; els.spiritEditorLint.innerHTML = "";
  if (f.loaded == null) {
    els.spiritEditorArea.value = "loading…"; els.spiritEditorArea.disabled = true;
    try { f.loaded = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent(f.path))).json()).content || ""; }
    catch (e) { f.loaded = ""; }
  }
  els.spiritEditorArea.disabled = false;
  els.spiritEditorArea.value = f.loaded;
  updateEditorDirty();
}
function renderEditorTabs() {
  const host = els.spiritEditorTabs; host.innerHTML = "";
  editorState.files.forEach((f, i) => {
    const b = el("button", "editor-tab" + (i === editorState.active ? " active" : ""), f.path.replace(/^spirits\//, ""));
    b.onclick = () => { if (i !== editorState.active) selectEditorFile(i); };
    host.append(b);
  });
}
function currentEditorFile() { return editorState && editorState.files[editorState.active]; }
function updateEditorDirty() {
  const f = currentEditorFile();
  const dirty = f && f.loaded != null && els.spiritEditorArea.value !== f.loaded;
  els.spiritEditorDirty.hidden = !dirty;
  return dirty;
}
async function saveEditor() {
  const f = currentEditorFile();
  if (!f) return;
  setSaveState("saving");
  els.spiritEditorLint.hidden = true;
  try {
    const r = await fetch("/api/spirits/file?path=" + encodeURIComponent(f.path), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content: els.spiritEditorArea.value }),
    });
    const res = await r.json();
    if (r.status === 422 || res.ok === false) {
      setSaveState("error");
      showEditorLint(res.errors || ["save blocked"], res.warnings || [], false);
      return; // keep dirty; do not update loaded
    }
    f.loaded = els.spiritEditorArea.value; // saved
    setSaveState("saved");
    updateEditorDirty();
    if ((res.warnings || []).length) showEditorLint([], res.warnings, true);
    loadSpiritRituals(); // refresh board (cadence/ceiling/validity may have changed)
  } catch (e) { setSaveState("error"); showEditorLint(["save failed: " + (e.message || e)], [], false); }
}
function showEditorLint(errors, warnings, savedOK) {
  const host = els.spiritEditorLint; host.innerHTML = ""; host.hidden = false;
  host.classList.toggle("lint-ok", savedOK && !errors.length);
  errors.forEach((m) => host.append(el("div", "lint-err", "✕ " + m)));
  warnings.forEach((m) => host.append(el("div", "lint-warn", "⚠ " + m)));
  if (savedOK && warnings.length) host.insertBefore(el("div", "lint-note", "saved with warnings:"), host.firstChild);
}
function closeEditor() { els.spiritEditor.hidden = true; editorState = null; }

// ＋ ritual mirrors ScaffoldRitual (on demand · chargebook-default ceiling ·
// 12 steps) and lands in the structured editor; ＋ spirit mirrors
// ScaffoldSpirit (claude-sub · no spellbooks · writes artifacts/runs only)
// and lands on the new spirit's page (SPIRITS.md §4 Creating).
function newRitual(sp) {
  askText(`New ritual for ${sp}`, 'lowercase name, e.g. "weekly-review"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/ritual", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: sp, name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      await loadSpiritRituals();
      location.hash = "#/agents/ritual/" + encodeURIComponent(sp) + "/" + encodeURIComponent(name.trim());
    } catch (e) { showToast("Couldn't create ritual: " + (e.message || e), null, "error"); }
  });
}
function newSpirit() {
  askText("New spirit", 'lowercase name, e.g. "news-scout"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/spirit", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      loadSpiritsStatus();
      location.hash = "#/agents/" + encodeURIComponent(name.trim());
    } catch (e) { showToast("Couldn't create spirit: " + (e.message || e), null, "error"); }
  });
}

if (els.spiritEditorArea) els.spiritEditorArea.addEventListener("input", updateEditorDirty);
if (els.spiritEditorSave) els.spiritEditorSave.addEventListener("click", saveEditor);
if (els.spiritEditorClose) els.spiritEditorClose.addEventListener("click", closeEditor);

