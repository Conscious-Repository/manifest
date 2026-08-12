// ---- RITUALS board + in-app markdown editing ----
// The board reads every ritual (next-fire, last outcome, ceiling, validity);
// clicking a row opens the raw markdown editor. Edits round-trip to the
// excalibur tree via /api/spirits/file (allow-listed); the engine hot-reloads.
let spiritRitualRows = []; // the crumb meta's ritual count reads this

async function loadSpiritRituals() {
  let rows = [];
  try { rows = (await (await fetch("/api/spirits/rituals")).json()).data || []; } catch (e) {}
  renderSpiritRituals(rows);
}

// ONE flat table (§12 / prototype): spirit · ritual / cadence-over-cron /
// next / outcome chip / ceiling. Row click edits the ritual; the spirit name
// inside the cell edits identity+cornerstone.
function renderSpiritRituals(rows) {
  spiritRitualRows = rows;
  const host = els.spiritRitualBoard; host.innerHTML = "";
  if (!rows.length) {
    host.appendChild(emptyRow("No rituals yet — add a spirit, then a ritual."));
  } else {
    rows.slice().sort((a, b) => (a.spirit + "/" + a.ritual).localeCompare(b.spirit + "/" + b.ritual))
      .forEach((r) => host.append(ritualRow(r)));
  }
  // ＋ ritual — pick the spirit inline, then the existing create path
  const spirits = [...new Set(rows.map((r) => r.spirit))].sort();
  const ghost = el("button", "sprt-ghost sprt-add-ritual", "＋ ritual");
  ghost.onclick = () => {
    if (spirits.length === 1) { newRitual(spirits[0]); return; }
    if (!spirits.length) { showToast("Add a spirit first"); return; }
    const sel = selectEl(spirits);
    sel.className = "pp-in";
    const go = el("button", "sprt-quiet", "add →");
    go.onclick = () => newRitual(sel.value);
    const wrap = el("span", "sprt-add-wrap");
    wrap.append(sel, go);
    ghost.replaceWith(wrap);
    sel.focus();
  };
  host.append(ghost);
  if (typeof renderSpiritIndex === "function") renderSpiritIndex(); // counts derive from these rows
  if (typeof updateSpiritsCrumb === "function") updateSpiritsCrumb();
}

function ritualRow(r) {
  const row = el("div", "ritual-row" + (r.valid ? "" : " invalid"));
  // name — spirit (its own editor) · ritual
  const name = el("span", "ritual-name");
  const sp = el("span", "sprt-spirit", r.spirit);
  sp.title = "Open " + r.spirit + "'s page";
  sp.onclick = (e) => { e.stopPropagation(); location.hash = "#/spirits/" + encodeURIComponent(r.spirit); };
  name.append(sp, document.createTextNode(" · " + r.ritual));
  row.append(name);
  // cadence — human phrase over the raw cron (both visible, prototype)
  const cad = el("span", "ritual-cadence");
  if (r.cadence === "") {
    cad.append(el("span", "cad-human", "on-demand"));
    cad.append(el("span", "cad-raw", "run with /"));
  } else {
    cad.append(el("span", "cad-human", r.cadenceHuman || "custom"));
    cad.append(el("span", "cad-raw", r.cadence));
  }
  row.append(cad);
  // next fire — absolute + quiet relative suffix
  const next = el("span", "ritual-next");
  if (r.valid && r.nextFire) {
    next.append(document.createTextNode(fmtWhen(r.nextFire) + " "));
    next.append(el("span", "next-rel", relPhrase(r.nextFire)));
  } else {
    next.textContent = "—";
  }
  row.append(next);
  // last outcome chip → run report
  const oc = el("span", "ritual-outcome");
  if (!r.valid) {
    const chip = el("span", "run-outcome oc-invalid", "invalid");
    chip.title = r.error || "invalid frontmatter";
    oc.append(chip);
  } else if (r.lastOutcome) {
    const chip = el("span", "run-outcome oc-" + r.lastOutcome.replace(/[^a-z-]/g, ""), r.lastOutcome);
    if (r.lastRunId) { chip.classList.add("linky"); chip.onclick = (e) => { e.stopPropagation(); openSpiritRun(r.lastRunId); }; }
    oc.append(chip);
  } else {
    oc.append(el("span", "run-outcome oc-never", "never run"));
  }
  row.append(oc);
  // ceiling
  const ceil = el("span", "ritual-ceiling" + (r.ceilingDefault ? " muted" : ""), "$" + Number(r.ceilingUsd).toFixed(2));
  ceil.title = r.ceilingDefault ? "chargebook default" : "ritual charge_usd";
  row.append(ceil);
  if (!r.valid && r.error) row.append(el("div", "ritual-error", r.error));
  row.onclick = () => openEditor([r.path]);
  return row;
}
// relFuture: " · in 9h" / " · in 3d" / " · due"
function relFuture(iso) {
  const p = relPhrase(iso);
  return p ? " · " + p : "";
}
// relPhrase: "in 9h" / "in 3d" / "due now"
function relPhrase(iso) {
  const d = new Date(iso), ms = d - new Date();
  if (isNaN(d)) return "";
  if (ms <= 0) return "due now";
  const m = Math.round(ms / 60000);
  if (m < 60) return "in " + m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return "in " + h + "h";
  return "in " + Math.round(h / 24) + "d";
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

function newRitual(sp) {
  askText(`New ritual for ${sp}`, 'lowercase name, e.g. "weekly-review"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/ritual", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: sp, name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      const { path } = await r.json();
      await loadSpiritRituals();
      openEditor([path]);
    } catch (e) { showToast("Couldn't create ritual: " + (e.message || e), null, "error"); }
  });
}
function newSpirit() {
  askText("New spirit", 'lowercase name, e.g. "news-scout"', async (name) => {
    if (!name.trim()) return;
    try {
      const r = await fetch("/api/spirits/spirit", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim() }) });
      if (!r.ok) throw new Error(await r.text());
      const { path } = await r.json();
      await loadSpiritRituals();
      loadSpiritsStatus();
      openEditor([`spirits/${name.trim()}/identity.md`, path], 1);
    } catch (e) { showToast("Couldn't create spirit: " + (e.message || e), null, "error"); }
  });
}

if (els.spiritEditorArea) els.spiritEditorArea.addEventListener("input", updateEditorDirty);
if (els.spiritEditorSave) els.spiritEditorSave.addEventListener("click", saveEditor);
if (els.spiritEditorClose) els.spiritEditorClose.addEventListener("click", closeEditor);
