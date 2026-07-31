// ---- PARCELS: the research-parcel layer (system/realestate/parcels/) ----
// Acquisition-intel over the St. Louis Assessor pull (cmd/parcel-pull): a colored
// map overlay + a spreadsheet, each parcel clickable for its data and a personal
// notes log. SEPARATE from owned properties — read-only research, no money.
let parcelCache = [];

// tax status → color: distressed parcels shout, current parcels recede.
const PARCEL_TAX_COLOR = { delinquent: "#d9534f", lra: "#e0972f", current: "#8fb0a0" };
const PARCEL_TAX_LABEL = { delinquent: "delinquent", lra: "LRA land-bank", current: "current" };

async function loadParcels() {
  if (parcelCache.length) return parcelCache;
  try {
    const d = await (await fetch("/api/parcels")).json();
    parcelCache = d.parcels || [];
  } catch (e) { parcelCache = []; }
  return parcelCache;
}

// addParcelsOverlay draws the research parcels on the properties map as a
// toggleable overlay group, colored by tax status, each clickable for facts +
// its notes log. Called at the end of renderPropertyMap.
async function addParcelsOverlay(map) {
  const parcels = await loadParcels();
  if (!parcels.length || !window.L) return;
  const group = L.featureGroup();
  const counts = { delinquent: 0, lra: 0, current: 0 };
  parcels.forEach((p) => {
    if (!(p.features || []).length) return;
    counts[p.taxStatus] = (counts[p.taxStatus] || 0) + 1;
    const color = PARCEL_TAX_COLOR[p.taxStatus] || "#8fb0a0";
    const base = { color, weight: 1, fillColor: color, fillOpacity: 0.35 };
    const layer = L.geoJSON({ type: "FeatureCollection", features: p.features }, { style: base });
    layer.on("mouseover", () => layer.setStyle({ weight: 2.5, fillOpacity: 0.55 }));
    layer.on("mouseout", () => layer.setStyle(base));
    layer.bindPopup(() => parcelPopup(p, layer), { minWidth: 250, maxWidth: 300, closeButton: true });
    group.addLayer(layer);
  });
  group.addTo(map);

  // toggle + a small tax legend
  L.control.layers(null, { ["Research parcels (" + parcels.length + ")"]: group },
    { collapsed: false, position: "topright" }).addTo(map);
  const legend = els.propertyMapLegend;
  ["delinquent", "lra", "current"].forEach((k) => {
    if (!counts[k]) return;
    const chip = el("span", "map-legend-chip");
    const dot = el("span", "map-legend-dot");
    dot.style.background = PARCEL_TAX_COLOR[k];
    chip.append(dot, el("span", "", PARCEL_TAX_LABEL[k] + " " + counts[k]));
    legend.append(chip);
  });
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

// ---- spreadsheet view (#/properties/parcels) ----
let parcelFilter = ""; // "" | delinquent | lra

async function renderParcelsView() {
  const host = els.propertyParcels;
  host.hidden = false;
  host.innerHTML = "";
  const parcels = await loadParcels();

  const nDelinq = parcels.filter((p) => p.taxStatus === "delinquent").length;
  const nLra = parcels.filter((p) => p.taxStatus === "lra").length;

  const head = el("div", "parcels-head");
  const chips = el("div", "parcels-filters");
  const mkChip = (key, label) => {
    const c = el("a", "filter-chip" + (parcelFilter === key ? " on" : ""), label);
    c.href = "#"; c.onclick = (e) => { e.preventDefault(); parcelFilter = key; renderParcelsView(); };
    return c;
  };
  chips.append(mkChip("", "all " + parcels.length), mkChip("delinquent", "delinquent " + nDelinq), mkChip("lra", "LRA " + nLra));
  const exp = el("a", "pill light parcels-export", "Export CSV ↓");
  exp.href = "/api/parcels/export";
  head.append(chips, exp);
  host.append(head);

  const rows = parcelFilter ? parcels.filter((p) => p.taxStatus === parcelFilter) : parcels;
  const table = el("div", "parcels-table");
  const hdr = el("div", "parcels-row parcels-hdr");
  ["address", "owner", "acquired", "tax status", "balance", "assessed", "notes"].forEach((h, i) =>
    hdr.append(el("div", "pc pc-" + i, h)));
  table.append(hdr);

  rows.forEach((p) => {
    const r = el("div", "parcels-row parcels-r pc-tax-" + p.taxStatus);
    r.append(el("div", "pc pc-0", p.address));
    r.append(el("div", "pc pc-1", p.owner || "—"));
    r.append(el("div", "pc pc-2", p.saleDate || p.recDate || "—"));
    r.append(el("div", "pc pc-3 pc-status", PARCEL_TAX_LABEL[p.taxStatus] || p.taxStatus));
    r.append(el("div", "pc pc-4", p.taxBalDue ? fmtMoney(p.taxBalDue) : "—"));
    r.append(el("div", "pc pc-5", p.assessed ? fmtMoney(p.assessed) : "—"));
    const notesCell = el("div", "pc pc-6 pc-notes");
    notesCell.textContent = (p.log || []).length ? "🗒 " + p.log.length : "";
    notesCell.title = (p.log || []).join("\n");
    r.append(notesCell);
    r.onclick = () => openParcelNote(p);
    table.append(r);
  });
  host.append(table);
}

// openParcelNote jumps to the universal note view for a parcel (raw record +
// the ## log), where notes can also be read/added by hand. postJSON is the
// shared global helper from 60-contacts.js.
function openParcelNote(p) {
  location.hash = "#/note/" + encodeURIComponent(p.path);
}
