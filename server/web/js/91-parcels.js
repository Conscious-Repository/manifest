// ---- PARCELS: the research-parcel layer (system/realestate/parcels/) ----
// Acquisition-intel over the St. Louis Assessor pull (cmd/parcel-pull): a colored
// map overlay + a spreadsheet, each parcel clickable for its data and a personal
// notes log. SEPARATE from owned properties — read-only research, no money.
let parcelCache = [];
let studyCache = null; // {parcels, meta, age} — the wide layer, loaded once

// tax status → color: distressed parcels shout, current parcels recede.
// Identical to ooda.group/parcels and the OODA portal — three surfaces, one
// palette, so a partner who has seen any of them recognizes the others.
const PARCEL_TAX_COLOR = { delinquent: "#d9534f", lra: "#e0972f", current: "#8fb0a0" };
const PARCEL_TAX_LABEL = { delinquent: "Tax delinquent", lra: "LRA land-bank", current: "Privately owned" };
const PARCEL_TAX_ORDER = ["delinquent", "lra", "current"];

async function loadParcels() {
  if (parcelCache.length) return parcelCache;
  try {
    const d = await (await fetch("/api/parcels")).json();
    parcelCache = d.parcels || [];
  } catch (e) { parcelCache = []; }
  return parcelCache;
}

// loadStudyParcels fetches the WIDE assessor layer — every lot in the three
// study neighborhoods, the same set ooda.group/parcels renders. It is ~1.5 MB,
// so it is its own lazy fetch and the map draws the records first.
async function loadStudyParcels() {
  if (studyCache) return studyCache;
  try {
    const d = await (await fetch("/api/parcels/study")).json();
    studyCache = { parcels: d.parcels || [], meta: d.meta || {}, age: d.age || "" };
  } catch (e) { studyCache = { parcels: [], meta: {}, age: "" }; }
  return studyCache;
}

// addParcelsOverlay draws the parcel study on the properties map: the owner's
// own research records (which carry his notes) plus every other lot on the
// block from the wide snapshot, colored by tax status with ONE TOGGLE PER
// STATUS — the shape ooda.group/parcels has, replacing a single combined
// checkbox that could only turn the whole layer on or off.
async function addParcelsOverlay(map) {
  if (!window.L) return;
  const [records, study] = await Promise.all([loadParcels(), loadStudyParcels()]);
  const all = records.concat(study.parcels);
  if (!all.length) return;

  // one feature group per status, so each toggles independently
  const groups = {};
  const counts = { delinquent: 0, lra: 0, current: 0 };
  PARCEL_TAX_ORDER.forEach((k) => { groups[k] = L.featureGroup(); });
  all.forEach((p) => {
    if (!(p.features || []).length) return;
    const status = PARCEL_TAX_COLOR[p.taxStatus] ? p.taxStatus : "current";
    counts[status]++;
    const color = PARCEL_TAX_COLOR[status];
    const base = { color, weight: 1, fillColor: color, fillOpacity: 0.35 };
    const layer = L.geoJSON({ type: "FeatureCollection", features: p.features }, { style: base });
    layer.on("mouseover", () => layer.setStyle({ weight: 2.5, fillOpacity: 0.55 }));
    layer.on("mouseout", () => layer.setStyle(base));
    layer.bindPopup(() => parcelPopup(p, layer), { minWidth: 250, maxWidth: 300, closeButton: true });
    layer.bindTooltip(p.address || "", { sticky: true });
    groups[status].addLayer(layer);
  });

  const overlays = {};
  PARCEL_TAX_ORDER.forEach((k) => {
    if (!counts[k]) return;
    groups[k].addTo(map);
    overlays[PARCEL_TAX_LABEL[k] + " · " + counts[k]] = groups[k];
  });
  L.control.layers(null, overlays, { collapsed: false, position: "topright" }).addTo(map);

  const legend = els.propertyMapLegend;
  PARCEL_TAX_ORDER.forEach((k) => {
    if (!counts[k]) return;
    const chip = el("span", "map-legend-chip");
    const dot = el("span", "map-legend-dot");
    dot.style.background = PARCEL_TAX_COLOR[k];
    chip.append(dot, el("span", "", PARCEL_TAX_LABEL[k].toLowerCase() + " " + counts[k]));
    legend.append(chip);
  });
  // the assessor pull has a date, and how old it is changes what the tax
  // colours mean — say so rather than implying the map is live
  if (study.meta && study.meta.snapshot_date) {
    legend.append(el("span", "map-legend-note",
      "assessor " + study.meta.snapshot_date + (study.age ? " · " + study.age : "")));
  }
}

// parcelPopup builds the click popup: parcel facts + the owner notes log + an
// add-note input (POSTs to /api/parcels/{slug}/log, no vault authorship by AI).
function parcelPopup(p, layer) {
  const box = el("div", "parcel-pop");
  box.append(el("div", "pp-addr", p.address));
  const chip = el("div", "pp-tax pp-" + p.taxStatus,
    p.taxStatus === "delinquent" ? "DELINQUENT · " + fmtMoney(p.taxBalDue)
      : p.taxStatus === "lra" ? "LRA land-bank" : "taxes current");
  box.append(chip);
  const facts = el("table", "pp-facts");
  const row = (k, v) => { if (v) { const tr = el("tr"); tr.append(el("td", "pp-k", k), el("td", "pp-v", v)); facts.append(tr); } };
  row("owner", p.owner);
  row("owner addr", p.ownerAddr);
  row("acquired", p.saleDate || p.recDate);
  row("assessed", p.assessed ? fmtMoney(p.assessed) : "");
  row("land use", p.landUse);
  row("parcel id", p.parcelId);
  box.append(facts);

  // No slug means no vault record behind this lot — it comes from the wide
  // study snapshot. Facts only: there is nothing to append a note to, and
  // offering an input that silently fails is worse than not offering one.
  if (!p.slug) {
    box.append(el("div", "pp-note pp-empty", "study parcel — not in your research set"));
    return box;
  }

  const notes = el("div", "pp-notes");
  const renderNotes = () => {
    notes.innerHTML = "";
    (p.log || []).forEach((ln) => notes.append(el("div", "pp-note", ln)));
    if (!(p.log || []).length) notes.append(el("div", "pp-note pp-empty", "no notes yet"));
  };
  renderNotes();
  box.append(el("div", "pp-notes-h", "notes"), notes);

  const add = el("div", "pp-add");
  const input = el("input", "pp-input"); input.placeholder = "add a note…";
  const btn = el("button", "pill pp-addbtn", "Add");
  const submit = async () => {
    const text = input.value.trim();
    if (!text) return;
    btn.disabled = true;
    try {
      const fresh = await postJSON("/api/parcels/" + encodeURIComponent(p.slug) + "/log", { text });
      p.log = fresh.log || p.log; p.lastLog = fresh.lastLog || p.lastLog;
      const i = parcelCache.findIndex((x) => x.slug === p.slug);
      if (i >= 0) parcelCache[i] = { ...parcelCache[i], log: p.log, lastLog: p.lastLog };
      input.value = ""; renderNotes();
    } catch (e) { showToast("Could not save note"); }
    btn.disabled = false;
  };
  btn.onclick = submit;
  input.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); submit(); } };
  add.append(input, btn);
  box.append(add);
  return box;
}

// (The standalone parcels spreadsheet view was removed — parcels now live only
//  as the map overlay above (addParcelsOverlay) + the property-page intel join.
//  loadParcels + parcelPopup + PARCEL_TAX_* stay as the overlay's dependencies.)
