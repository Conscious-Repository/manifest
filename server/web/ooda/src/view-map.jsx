// MAP — the parcel study, the same one that runs at ooda.group/parcels and in
// the private cockpit, but drawn from the live vault instead of a hand-built
// static geojson, so it can never drift from the portfolio the other tabs show.
//
// Two stacks: our HOLDINGS on top, colored by acquisition state; the STUDY
// parcels beneath, colored by tax status, each layer independently toggleable.
// Every polygon is clickable — that is the whole point of the tab. A partner
// should be able to point at any lot on the block and learn who owns it.
//
// Raw Leaflet inside a useEffect, not react-leaflet: react-leaflet is an ESM
// npm package and this portal has no build step (React arrives as a UMD bundle
// and JSX compiles in the browser). window.L is a global; that is the seam.

function ViewMap() {
  const hostRef = React.useRef(null);
  const mapRef = React.useRef(null);
  const [pay, setPay] = React.useState(null);
  const [err, setErr] = React.useState("");
  const [tax, setTax] = React.useState({ delinquent: true, lra: true, current: true });
  const [acq, setAcq] = React.useState({ "owned": true, "under-contract": true, "pipeline": true });

  // one lazy fetch on first entry — the payload carries every parcel polygon
  React.useEffect(() => {
    let dead = false;
    Promise.all([loadLeaflet(), getJSON("/api/ooda/map")])
      .then(([, data]) => { if (!dead) setPay(data); })
      .catch((e) => { if (!dead) setErr(String(e.message || e)); });
    return () => { dead = true; };
  }, []);

  React.useEffect(() => {
    if (!pay || !window.L || !hostRef.current) return;
    const L = window.L;
    // a full teardown per draw: Leaflet holds DOM and handlers, and React
    // re-running this effect over a live map is how you get two maps stacked
    if (mapRef.current) { mapRef.current.remove(); mapRef.current = null; }
    const map = L.map(hostRef.current, { zoomSnap: 0.25, scrollWheelZoom: true, minZoom: 13, maxZoom: 19 });
    mapRef.current = map;
    L.tileLayer("https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png", {
      attribution: '&copy; OpenStreetMap &copy; CARTO · Parcel data: City of St. Louis Assessor',
      maxZoom: 19,
    }).addTo(map);

    const bounds = [];
    const collect = (layer) => { try { bounds.push(layer.getBounds()); } catch (e) { /* point layer */ } };

    // ---- study parcels (beneath) ----
    (pay.parcels || []).forEach((p) => {
      if (!tax[p.taxStatus]) return;
      const c = OODA_TAX_COLOR[p.taxStatus] || OODA_TAX_COLOR.current;
      const base = { color: c, weight: 1, fillColor: c, fillOpacity: 0.35 };
      const draw = (layer) => {
        layer.setStyle && layer.setStyle(base);
        layer.on("mouseover", () => layer.setStyle({ weight: 2.5, fillOpacity: 0.55 }));
        layer.on("mouseout", () => layer.setStyle(base));
        layer.bindPopup(parcelPopupHTML(p), { maxWidth: 300 });
        layer.bindTooltip(p.address || p.slug, { sticky: true });
        layer.addTo(map);
        collect(layer);
      };
      if ((p.features || []).length) p.features.forEach((f) => draw(L.geoJSON(f)));
      else if (p.lat && p.lng) {
        const m = L.circleMarker([p.lat, p.lng], { ...base, radius: 6 });
        m.bindPopup(parcelPopupHTML(p), { maxWidth: 300 });
        m.addTo(map);
        bounds.push(L.latLngBounds([[p.lat, p.lng], [p.lat, p.lng]]));
      }
    });

    // ---- our holdings (on top) ----
    (pay.holdings || []).forEach((h) => {
      if (!acq[h.acq]) return;
      const c = OODA_ACQ_COLOR[h.acq] || OODA_ACQ_COLOR.pipeline;
      const base = { color: c, weight: 2, fillColor: c, fillOpacity: 0.5 };
      const html = holdingPopupHTML(h);
      const draw = (layer) => {
        layer.setStyle && layer.setStyle(base);
        layer.on("mouseover", () => layer.setStyle({ weight: 3, fillOpacity: 0.68 }));
        layer.on("mouseout", () => layer.setStyle(base));
        layer.bindPopup(html, { maxWidth: 300 });
        layer.bindTooltip(h.short + " · " + (OODA_ACQ_LABEL[h.acq] || h.acq), { sticky: true });
        layer.addTo(map);
        layer.bringToFront();
        collect(layer);
      };
      if ((h.features || []).length) h.features.forEach((f) => draw(L.geoJSON(f)));
      else if (h.lat && h.lng) {
        const m = L.circleMarker([h.lat, h.lng], { ...base, radius: 8 });
        m.bindPopup(html, { maxWidth: 300 });
        m.bindTooltip(h.short, { sticky: true });
        m.addTo(map);
        bounds.push(L.latLngBounds([[h.lat, h.lng], [h.lat, h.lng]]));
      }
    });

    const fit = () => {
      if (!bounds.length) { map.setView([38.6575, -90.259], 16); return; }
      let b = bounds[0];
      bounds.slice(1).forEach((x) => { b = b.extend(x); });
      map.fitBounds(b.pad(0.04), { maxZoom: 17 });
    };
    fit();
    // the pane settles AFTER L.map() measured it — without this second pass
    // the tiles render against the wrong size and the map reads as blurred
    const t = setTimeout(() => { map.invalidateSize(); fit(); }, 80);
    return () => { clearTimeout(t); };
  }, [pay, tax, acq]);

  React.useEffect(() => () => {
    if (mapRef.current) { mapRef.current.remove(); mapRef.current = null; }
  }, []);

  if (err) {
    return <Empty>{"the map could not load: " + err}</Empty>;
  }
  if (!pay) return <Empty>loading parcels…</Empty>;

  const heldCount = (k) => (pay.holdings || []).filter((h) => h.acq === k).length;
  return (
    <div className="ooda-mapwrap">
      <div ref={hostRef} className="ooda-map" />
      <div className="ooda-legend">
        <div className="ooda-legend-h">HOLDINGS</div>
        {OODA_ACQ_ORDER.map((k) => (
          <label className="ooda-legend-row" key={k}>
            <input type="checkbox" checked={!!acq[k]}
              onChange={() => setAcq((s) => ({ ...s, [k]: !s[k] }))} />
            <i className="ooda-sw" style={{ background: OODA_ACQ_COLOR[k] }} />
            <span>{OODA_ACQ_LABEL[k]}</span>
            <span className="ooda-legend-n">{heldCount(k) || DASH}</span>
          </label>
        ))}
        <div className="ooda-legend-h">OPPORTUNITIES</div>
        {OODA_TAX_ORDER.map((k) => (
          <label className="ooda-legend-row" key={k}>
            <input type="checkbox" checked={!!tax[k]}
              onChange={() => setTax((s) => ({ ...s, [k]: !s[k] }))} />
            <i className="ooda-sw" style={{ background: OODA_TAX_COLOR[k] }} />
            <span>{OODA_TAX_LABEL[k]}</span>
            <span className="ooda-legend-n">{(pay.counts || {})[k] || DASH}</span>
          </label>
        ))}
        {/* these are other people's tax positions; how old the assessor pull
            is changes what the colours mean, so the map says it out loud */}
        {pay.study && pay.study.snapshot_date ? (
          <div className="ooda-legend-src">
            {"assessor " + pay.study.snapshot_date + (pay.studyAge ? " · " + pay.studyAge : "")}
          </div>
        ) : null}
      </div>
    </div>
  );
}

// Leaflet popups take an HTML string. Everything interpolated here comes from
// assessor records, so escape it — an owner name with an ampersand or a stray
// angle bracket must not be able to write markup into the page.
function esc(v) {
  return String(v == null ? "" : v)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function popRow(k, v) {
  if (v == null || v === "" || v === DASH) return "";
  return '<tr><td class="ooda-pk">' + esc(k) + '</td><td class="ooda-pv">' + esc(v) + "</td></tr>";
}

function parcelPopupHTML(p) {
  const chip = p.taxStatus === "delinquent"
    ? "DELINQUENT · " + money(p.taxBalDue) + " owed"
    : p.taxStatus === "lra" ? "LRA land-bank" : "taxes current";
  return '<div class="ooda-pop">' +
    '<div class="ooda-pop-addr">' + esc(p.address || p.slug) + "</div>" +
    '<div class="ooda-pop-tax ooda-tax-' + esc(p.taxStatus) + '">' + esc(chip) + "</div>" +
    '<table class="ooda-pop-facts">' +
    popRow("owner", p.owner) +
    popRow("owner addr", p.ownerAddr) +
    popRow("acquired", p.saleDate || p.recDate) +
    popRow("assessed", p.assessed ? money(p.assessed) : "") +
    popRow("land use", p.landUse) +
    popRow("parcel id", p.parcelId) +
    "</table></div>";
}

function holdingPopupHTML(h) {
  return '<div class="ooda-pop">' +
    '<div class="ooda-pop-addr">' + esc(h.short || h.slug) + "</div>" +
    '<div class="ooda-pop-acq ooda-acq-' + esc(h.acq) + '">' +
    esc(OODA_ACQ_LABEL[h.acq] || h.acq) + "</div>" +
    '<table class="ooda-pop-facts">' +
    popRow("entity", h.entity) +
    popRow("status", statusLabel(h.status)) +
    popRow("deal", h.deal) +
    popRow("budget", h.plan ? money(h.plan) : "") +
    popRow("spent", h.paid ? money(h.paid) : "") +
    popRow("open work", h.open ? String(h.open) : "") +
    popRow("address", h.address) +
    "</table></div>";
}
