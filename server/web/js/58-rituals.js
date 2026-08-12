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

// ---- SPIRITS.md §2: the cadence builder ----
// State: { kind, days:[0..6], hours:[0..23], min:0..59, n:int }
// kinds: ondemand | daily | weekdays | weekends | days | everyMin | everyHour | hourly
// The option set is EXACTLY what humanCadence() (spirits/rituals.go) can
// phrase — "custom" cron is deliberately unreachable through the form. The
// compiled cron renders under the phrase as the receipt; an incomplete
// cadence blocks the save. `on demand` writes NO cadence key.

const CAD_DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const CAD_KINDS = [
  ["ondemand", "on demand"], ["daily", "daily"], ["weekdays", "weekdays"],
  ["weekends", "weekends"], ["days", "named days"],
  ["everyMin", "every N min"], ["everyHour", "every N hours"], ["hourly", "hourly"],
];

function cadDefault(kind) {
  return {
    kind,
    days: [],
    hours: ["daily", "weekdays", "weekends", "days"].includes(kind) ? [9] : [],
    min: 0,
    n: kind === "everyMin" ? 30 : 2,
  };
}

function cadValidate(cad) {
  const errs = [];
  if (!cad) return ["custom cron — edit through raw"];
  if (cad.kind === "days" && !cad.days.length) errs.push("pick at least one day");
  if (["daily", "weekdays", "weekends", "days"].includes(cad.kind) && !cad.hours.length) errs.push("pick at least one time");
  if ((cad.kind === "everyMin" || cad.kind === "everyHour") && !(cad.n >= 1)) errs.push("interval must be at least 1");
  return errs;
}

function cadClock(hr, mn) {
  let ap = "a", h = hr;
  if (hr === 0) h = 12;
  else if (hr === 12) ap = "p";
  else if (hr > 12) { h = hr - 12; ap = "p"; }
  return h + ":" + String(mn).padStart(2, "0") + ap;
}

// cadCompile — builder state → canonical cron (hours/days sorted ascending)
// + the phrase humanCadence() will echo back for it.
function cadCompile(cad) {
  if (!cad) return { cron: null, phrase: "custom" };
  if (cad.kind === "ondemand") return { cron: "", phrase: "on demand" };
  if (cadValidate(cad).length) return { cron: null, phrase: "incomplete" };
  if (cad.kind === "everyMin") return { cron: "*/" + cad.n + " * * * *", phrase: "every " + cad.n + " min" };
  if (cad.kind === "everyHour") return { cron: "0 */" + cad.n + " * * *", phrase: "every " + cad.n + " hours" };
  if (cad.kind === "hourly") return { cron: cad.min + " * * * *", phrase: "hourly at :" + String(cad.min).padStart(2, "0") };
  const hours = [...cad.hours].sort((a, b) => a - b);
  const days = [...cad.days].sort((a, b) => a - b);
  const dow = { daily: "*", weekdays: "1-5", weekends: "0,6" }[cad.kind] || days.join(",");
  const prefix = { daily: "daily", weekdays: "weekdays", weekends: "weekends" }[cad.kind]
    || days.map((d) => CAD_DAY_NAMES[d]).join(", ");
  return {
    cron: cad.min + " " + hours.join(",") + " * * " + dow,
    phrase: prefix + " " + hours.map((h) => cadClock(h, cad.min)).join(", "),
  };
}

// cadParse — the inverse, mirroring humanCadence()'s decision order EXACTLY
// (spirits/rituals.go:466). null = a custom cron the builder can't express
// (raw-only editing). Stricter than Go only in bounding minute ≤ 59.
function cadValueList(s, lo, hi) {
  const out = [];
  for (const p of s.split(",")) {
    if (!/^\d+$/.test(p.trim())) return null;
    const n = parseInt(p.trim(), 10);
    if (n < lo || n > hi) return null;
    out.push(n);
  }
  return out.length ? out : null;
}

function cadParse(cron) {
  const s = (cron || "").trim();
  if (s === "") return { kind: "ondemand", days: [], hours: [], min: 0, n: 0 };
  const f = s.split(/\s+/);
  if (f.length !== 5) return null;
  const [min, hour, dom, mon, dow] = f;
  const intOf = (x) => (/^\d+$/.test(x) ? parseInt(x, 10) : null);
  if (min.startsWith("*/") && hour === "*" && dom === "*" && mon === "*" && dow === "*") {
    const n = intOf(min.slice(2));
    return n != null && n > 0 ? { kind: "everyMin", days: [], hours: [], min: 0, n } : null;
  }
  if (min === "0" && hour.startsWith("*/") && dom === "*" && mon === "*" && dow === "*") {
    const n = intOf(hour.slice(2));
    return n != null && n > 0 ? { kind: "everyHour", days: [], hours: [], min: 0, n } : null;
  }
  if (dom !== "*" || mon !== "*") return null;
  const mn = intOf(min);
  if (mn == null || mn > 59) return null;
  if (hour === "*") return { kind: "hourly", days: [], hours: [], min: mn, n: 0 };
  const hours = cadValueList(hour, 0, 23);
  if (!hours) return null;
  if (dow === "*") return { kind: "daily", days: [], hours, min: mn, n: 0 };
  if (dow === "1-5") return { kind: "weekdays", days: [], hours, min: mn, n: 0 };
  if (dow === "0,6" || dow === "6,0") return { kind: "weekends", days: [], hours, min: mn, n: 0 };
  const days = cadValueList(dow, 0, 6);
  return days ? { kind: "days", days, hours, min: mn, n: 0 } : null;
}

// canonCron — the dirty-compare key: canonical-vs-canonical, never raw strings
// ("0 18,8 * * *" must open clean).
function canonCron(cad) { return cad ? cadCompile(cad).cron : null; }

// cadNextFire — client next-fire for the receipt's `next:` line. Minute-scan
// over the builder vocabulary (exact / list / */N / 1-5 range); 8-day horizon.
function cadNextFire(cron) {
  if (!cron) return null;
  const f = cron.trim().split(/\s+/);
  if (f.length !== 5) return null;
  const match = (field, v) => {
    if (field === "*") return true;
    if (field.startsWith("*/")) { const n = parseInt(field.slice(2), 10); return n > 0 && v % n === 0; }
    return field.split(",").some((p) => {
      const r = p.split("-");
      if (r.length === 2) return v >= parseInt(r[0], 10) && v <= parseInt(r[1], 10);
      return parseInt(p, 10) === v;
    });
  };
  const d = new Date();
  d.setSeconds(0, 0);
  d.setMinutes(d.getMinutes() + 1);
  for (let i = 0; i < 60 * 24 * 8; i++) {
    if (match(f[0], d.getMinutes()) && match(f[1], d.getHours()) &&
        match(f[2], d.getDate()) && match(f[3], d.getMonth() + 1) && match(f[4], d.getDay())) return d;
    d.setMinutes(d.getMinutes() + 1);
  }
  return null;
}

// renderCadenceBuilder(host, cad, {custom, rawCron, onEdit}) — kind chips →
// conditional rows → the receipt. Every control routes through onEdit(next)
// (which hands authority back to the form — the raw pane's contract).
function renderCadenceBuilder(host, cad, opts) {
  host.innerHTML = "";
  const edit = (next) => opts.onEdit(next);
  if (opts.custom) {
    const note = el("div", "cadb-custom");
    note.append(el("span", "cadb-custom-msg", "custom cron — the builder can't express this; edit through raw below"));
    note.append(el("code", "cadb-custom-cron", opts.rawCron || ""));
    host.append(note);
  }
  // kind chips (picking one from custom mode reseeds the form — form wins)
  const kinds = el("div", "cadb-row");
  CAD_KINDS.forEach(([k, label]) => {
    const b = el("button", "cadb-chip" + (!opts.custom && cad && cad.kind === k ? " on" : ""), label);
    b.onclick = () => edit(cad && cad.kind === k ? { ...cad } : cadDefault(k));
    kinds.append(b);
  });
  host.append(kinds);
  if (opts.custom || !cad) return;

  const chipRow = (label, chips) => {
    const row = el("div", "cadb-row");
    row.append(el("span", "cadb-label", label));
    chips.forEach((c) => row.append(c));
    host.append(row);
    return row;
  };
  const toggleChip = (label, on, cb) => {
    const b = el("button", "cadb-chip" + (on ? " on" : ""), label);
    b.onclick = cb;
    return b;
  };

  if (cad.kind === "days") {
    chipRow("days", CAD_DAY_NAMES.map((nm, d) =>
      toggleChip(nm, cad.days.includes(d), () => {
        const days = cad.days.includes(d) ? cad.days.filter((x) => x !== d) : [...cad.days, d];
        edit({ ...cad, days });
      })));
  }
  if (["daily", "weekdays", "weekends", "days"].includes(cad.kind)) {
    // time list: one chip per chosen hour (✕ removes), ＋ time appends
    const chips = [...cad.hours].sort((a, b) => a - b).map((h) =>
      toggleChip(cadClock(h, cad.min) + " ✕", true, () =>
        edit({ ...cad, hours: cad.hours.filter((x) => x !== h) })));
    const add = document.createElement("select");
    add.className = "pp-in cadb-add";
    const o0 = document.createElement("option"); o0.value = ""; o0.textContent = "＋ time"; add.append(o0);
    for (let h = 0; h < 24; h++) {
      if (cad.hours.includes(h)) continue;
      const o = document.createElement("option"); o.value = String(h); o.textContent = cadClock(h, cad.min); add.append(o);
    }
    add.onchange = () => { if (add.value !== "") edit({ ...cad, hours: [...cad.hours, parseInt(add.value, 10)] }); };
    chipRow("times", [...chips, add]);
  }
  if (cad.kind === "hourly" || ["daily", "weekdays", "weekends", "days"].includes(cad.kind)) {
    const presets = [0, 15, 30, 45];
    if (!presets.includes(cad.min)) presets.push(cad.min); // parsed off-preset value stays representable
    chipRow("minute", presets.sort((a, b) => a - b).map((m) =>
      toggleChip(":" + String(m).padStart(2, "0"), cad.min === m, () => edit({ ...cad, min: m }))));
  }
  if (cad.kind === "everyMin" || cad.kind === "everyHour") {
    const presets = cad.kind === "everyMin" ? [5, 10, 15, 30, 45] : [1, 2, 3, 4, 6, 12];
    if (!presets.includes(cad.n)) presets.push(cad.n);
    chipRow("every", presets.sort((a, b) => a - b).map((n) =>
      toggleChip(String(n) + (cad.kind === "everyMin" ? " min" : " h"), cad.n === n, () => edit({ ...cad, n }))));
  }

  // the receipt: phrase left, the compiled cron right — how you confirm the
  // builder wrote what you meant
  const { cron, phrase } = cadCompile(cad);
  const errs = cadValidate(cad);
  const receipt = el("div", "cadb-receipt" + (errs.length ? " incomplete" : ""));
  receipt.append(el("span", "cadb-phrase", errs.length ? errs[0] : phrase));
  receipt.append(el("code", "cadb-cron",
    cad.kind === "ondemand" ? "no cadence key" : (cron == null ? "nothing to write yet" : "cadence: " + cron)));
  host.append(receipt);
  if (cron) {
    const nx = cadNextFire(cron);
    if (nx) host.append(el("div", "cadb-next", "next: " + nx.toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })));
  }
}
