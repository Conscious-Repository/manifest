// ---- PROPERTIES: the real-estate cockpit over system/realestate/ records ----
// Board (grouped by entity, paid%/committed% rollups) + a property page (rollup,
// budget, ledger with quick-add, log with quick-add, prose via the note view).
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "board"; // board | map | statements | page — derived from the hash
let boardFilter = ""; // search-as-you-type
let boardStatus = ""; // status dropdown ("" = all)
let boardEntity = ""; // entity dropdown ("" = all)
let boardSort = "status"; // status | address | budget | var

// showProperties routes the PROPERTIES sub-views off the hash:
//   #/properties · /map · /statements · /deal/<slug> · /<slug>
function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true; els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true; els.propertyStatements.hidden = true;
  els.propertyContractors.hidden = true;
  els.propertyWork.hidden = true; els.propertySettings.hidden = true;
  if (els.propertyParcels) els.propertyParcels.hidden = true;
  if (tail.startsWith("deal/")) { propMode = "page"; renderDealPage(tail.slice(5)); }
  else if (tail === "parcels") { propMode = "parcels"; syncPropChips(); renderParcelsView(); }
  else if (tail === "map") { propMode = "map"; syncPropChips(); loadProperties(); }
  else if (tail === "work") { propMode = "work"; syncPropChips(); renderWorkView(); }
  else if (tail === "statements") { location.hash = "#/properties/accounting"; return; } // legacy
  else if (tail === "accounting") { propMode = "accounting"; syncPropChips(); renderAccounting(); }
  else if (tail === "contractors") { propMode = "contractors"; syncPropChips(); renderContractors(); }
  else if (tail === "settings") { propMode = "settings"; syncPropChips(); renderREsettings(); }
  else if (tail) { propMode = "page"; renderPropertyPage(tail); }
  else { propMode = "board"; syncPropChips(); loadProperties(); }
}

function syncPropChips() {
  els.propToggle.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === propMode));
}

async function loadProperties() {
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
    rePortalEnabled = !!d.rePortal;
  } catch (e) { propertyCache = []; dealCache = []; templateCache = []; }
  if (propMode === "map") renderPropertyMap();
  else if (propMode === "board") renderBoard();
}

// (fmtMoney / fmtPct live in 05-components.js — the money slot owns display policy)

// projMoney: one property's plan-vs-spend numbers.
function projMoney(p) {
  const pj = p.project || {};
  return { budget: pj.planTotal || 0, committed: pj.committed || 0,
    paid: pj.paid || 0, over: !!pj.over };
}

// ---- shared primitives (admin-portal design §0) ----
// (ppCols / makeDirtyBar / collapsibleSection live in 05-components.js — the
// §11 component library; promoted there with the AION tab.)

// propertyTypeahead: the typeahead engine over all property records
// (63 items — a select is unusable). Matches address/slug/deal.
function propertyTypeahead(placeholder, onPick, initial) {
  const ta = typeahead({
    placeholder, initial,
    suggest: (q, add) => {
      propertyCache.filter((p) =>
        !q || (p.address || "").toLowerCase().includes(q) || p.slug.includes(q) || (p.deal || "").includes(q))
        .slice(0, 12)
        .forEach((p) => add((p.short || p.address || p.slug) + (p.deal ? "  · " + p.deal : ""), "",
          () => { ta.commit(p.short || p.address || p.slug); onPick(p); }));
    },
  });
  return ta.el;
}
