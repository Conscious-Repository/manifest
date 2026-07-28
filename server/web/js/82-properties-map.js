// ---- PROPERTIES map (Leaflet from cdnjs, lazy-loaded; CartoDB light tiles) ----
// Parcel polygons color-coded by status: active work anchors on the app's blue,
// everything else muted. Tracked (pipeline, not owned) parcels render dashed as
// a distinct tier; background parcels sit underneath in near-invisible gray.
// GeoJSON is [lng,lat] — L.geoJSON handles the flip; nothing constructs LatLngs
// by hand. Bounds are computed from the rendered layers, never hardcoded.
let _leafletLoading = null;
let _propMap = null; // the live Leaflet map instance (rebuilt per render)

const PROP_STATUS_COLOR = {
  construction: "#265ACC", pre_development: "#5b82d9", // active — the app blue
  under_contract: "#8a93a6", negotiating: "#a7aeba",   // pipeline — muted slate
  completed: "#4d9d6a", leased: "#4d9d6a", listed: "#4d9d6a", sold: "#4d9d6a", // complete — ONE quiet green
};

// legend: fixed canonical order, done statuses consolidated into one entry
const MAP_LEGEND = [
  ["construction", "#265ACC"], ["pre-development", "#5b82d9"],
  ["under contract", "#8a93a6"], ["negotiating", "#a7aeba"],
  ["complete", "#4d9d6a"], ["tracked", "#b0a58e"], ["deal", "#8a93a6"],
];
const LEGEND_GROUP = {
  construction: "construction", pre_development: "pre-development",
  under_contract: "under contract", negotiating: "negotiating", opportunity: "negotiating",
  completed: "complete", leased: "complete", listed: "complete", sold: "complete",
};

function loadLeaflet() {
  if (window.L) return Promise.resolve();
  if (_leafletLoading) return _leafletLoading;
  _leafletLoading = new Promise((resolve, reject) => {
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = "https://cdnjs.cloudflare.com/ajax/libs/leaflet/1.9.4/leaflet.min.css";
    document.head.append(css);
    const js = document.createElement("script");
    js.src = "https://cdnjs.cloudflare.com/ajax/libs/leaflet/1.9.4/leaflet.min.js";
    js.onload = () => resolve();
    js.onerror = () => { _leafletLoading = null; reject(new Error("leaflet failed to load")); };
    document.head.append(js);
  });
  return _leafletLoading;
}

async function renderPropertyMap() {
  els.propertyBoard.hidden = true; els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = false;
  try { await loadLeaflet(); } catch (e) {
    // offline — degrade to the list with a quiet notice
    setPropMode("list");
    showToast("Map unavailable offline — showing the list");
    return;
  }
  let geo;
  try { geo = await (await fetch("/api/properties/geo")).json(); }
  catch (e) { setPropMode("list"); return; }

  if (_propMap) { _propMap.remove(); _propMap = null; }
  const map = L.map(els.propertyMap, { zoomControl: true, attributionControl: true });
  _propMap = map;
  L.tileLayer("https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/">CARTO</a>',
    maxZoom: 19,
  }).addTo(map);

  // background parcels — context only, nearly invisible
  if (geo.bg && (geo.bg.features || []).length) {
    L.geoJSON(geo.bg, { style: { color: "#d3d7de", weight: 1, fill: true, fillOpacity: 0.02, interactive: false } }).addTo(map);
  }

  const rendered = [];
  const ownedLayers = []; // zoom anchors — the map opens on the owned cluster
  const unmapped = [];
  const statusesSeen = new Set();
  // tracked first (below), owned/active after (on top so borders aren't clipped)
  const trackedTier = (r) => (r.control === "tracked" && STATUS_BUCKET[r.status] !== "active") ? 0 : 1;
  const recs = (geo.records || []).slice().sort((a, b) => trackedTier(a) - trackedTier(b));
  recs.forEach((rec) => {
    if (!(rec.features || []).length) {
      // pin fallback: frontmatter lat/lng or a cached geocode (no polygon)
      if (rec.lat && rec.lng) {
        const color = PROP_STATUS_COLOR[rec.status] || "#8a93a6";
        const pin = L.circleMarker([rec.lat, rec.lng], { radius: 8, color, fillColor: color, fillOpacity: 0.5, weight: 2 });
        const href = "#/properties/" + encodeURIComponent(rec.slug);
        pin.bindPopup('<a href="' + href + '" class="prop-pop">' + escapeHtml(rec.title + (rec.status ? " · " + rec.status : "")) + "</a>", { closeButton: false });
        pin.addTo(map);
        rendered.push(pin);
        if (rec.status) statusesSeen.add(LEGEND_GROUP[rec.status] || rec.status);
      } else unmapped.push(rec);
      return;
    }
    // active status wins over the tracked tier (same rule as bucketOf): flipping
    // a tracked parcel to pre_development/construction must recolor it
    const tracked = rec.control === "tracked" && STATUS_BUCKET[rec.status] !== "active";
    const color = tracked ? "#b0a58e" : (PROP_STATUS_COLOR[rec.status] || "#8a93a6");
    if (rec.status) statusesSeen.add(tracked ? "tracked" : (LEGEND_GROUP[rec.status] || rec.status));
    else if (rec.type === "deal") statusesSeen.add("deal");
    const style = {
      color, weight: tracked ? 1.5 : 2, dashArray: tracked ? "4 3" : null,
      fillColor: color, fillOpacity: tracked ? 0.06 : 0.14,
    };
    // one layer per record → a multi-parcel deal hovers/selects as one group
    const layer = L.geoJSON({ type: "FeatureCollection", features: rec.features }, { style });
    layer.on("mouseover", () => layer.setStyle({ weight: 3, fillOpacity: 0.24 }));
    layer.on("mouseout", () => layer.setStyle(style));
    const href = rec.type === "deal" ? "#/properties/deal/" + encodeURIComponent(rec.slug) : "#/properties/" + encodeURIComponent(rec.slug);
    const label = (rec.short || rec.title) + (rec.status ? " · " + rec.status : "") + (rec.type === "deal" ? " · bundle" : "");
    layer.bindPopup('<a href="' + href + '" class="prop-pop">' + escapeHtml(label) + "</a>", { closeButton: false });
    layer.addTo(map);
    rendered.push(layer);
    if (!tracked && rec.type === "property") ownedLayers.push(layer);
  });

  // Open zoomed to the OWNED cluster (the actual work); tracked/background are a
  // pan away. maxZoom caps a lone parcel from diving to street level.
  const anchors = ownedLayers.length ? ownedLayers : rendered;
  if (anchors.length) {
    map.fitBounds(L.featureGroup(anchors).getBounds().pad(0.05), { maxZoom: 17 });
  } else {
    map.setView([38.65, -90.26], 16); // nothing mapped yet — the seed's neighborhood
  }

  // quiet legend: canonical order, only the groups actually visible
  const legend = els.propertyMapLegend; legend.innerHTML = "";
  MAP_LEGEND.filter(([name]) => statusesSeen.has(name)).forEach(([name, color]) => {
    const chip = el("span", "map-legend-chip");
    const dot = el("span", "map-legend-dot");
    dot.style.background = color;
    chip.append(dot, el("span", "", name));
    legend.append(chip);
  });

  const um = els.propertyUnmapped;
  if (unmapped.length) {
    um.hidden = false;
    um.textContent = "unmapped: " + unmapped.map((r) => r.title).join(" · ");
  } else um.hidden = true;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
