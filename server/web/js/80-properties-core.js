// ---- PROPERTIES — Revision 3: one rail, one pane ----
// A property is an address, a status, one budget number, and todos. The rail
// lists Views (All properties · Outstanding · Map · Parcels) over every
// property with its open-todo count; the pane renders one thing at a time.
// The work kanban, SOW list, accounting, contractors, entities and settings
// surfaces are gone — `## work` stays in the files as the budget source, and
// deep material is reachable with ⌘/ or the note view.
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "all"; // all | outstanding | map | parcels | page
let propSlug = "";    // the open property (page mode)
let propTodosMeta = null; // /api/todos payload — assignees + outstanding (one truth)

function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true;
  if (els.propertyParcels) els.propertyParcels.hidden = true;
  if (els.propertySettings) els.propertySettings.hidden = true;
  closePropInspector();
  // legacy sub-tab routes fold into the rail views; deal pages open the
  // bundle's vault note (the underwrite UI is retired)
  if (["work", "accounting", "statements", "contractors"].includes(tail)) {
    location.hash = "#/properties";
    return;
  }
  if (tail.startsWith("deal/")) {
    location.hash = "#/note/" + encodeURIComponent("system/realestate/deals/" + tail.slice(5) + ".md");
    return;
  }
  propSlug = "";
  if (tail === "outstanding") propMode = "outstanding";
  else if (tail === "map") propMode = "map";
  else if (tail === "parcels") propMode = "parcels";
  else if (tail === "settings") propMode = "settings";
  else if (tail) { propMode = "page"; propSlug = tail; }
  else propMode = "all";
  renderProperties();
}

async function loadProperties() {
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
    rePortalEnabled = !!d.rePortal;
  } catch (e) { propertyCache = []; dealCache = []; templateCache = []; }
}

async function loadPropTodosMeta() {
  try { propTodosMeta = await (await fetch("/api/todos")).json(); }
  catch (e) { propTodosMeta = { outstanding: [], assignees: {}, counts: {} }; }
}

async function renderProperties() {
  await Promise.all([loadProperties(), loadPropTodosMeta()]);
  renderPropRail();
  const outN = propOutstandingGroups().reduce((n, g) => n + g.count, 0); // property-owed only
  if (typeof setCrumbMeta === "function") {
    setCrumbMeta(propertyCache.length + " properties" +
      (outN ? " · " + outN + " outstanding" : ""));
  }
  if (typeof railSetCount === "function") railSetCount("properties", propertyCache.length);
  if (propMode === "map") { els.propertyMapWrap.hidden = false; renderPropertyMap(); }
  else if (propMode === "parcels") renderParcelsView();
  else if (propMode === "settings") renderREsettings();
  else if (propMode === "page") { els.propertyPage.hidden = false; renderPropertyPage(propSlug); }
  else { els.propertyBoard.hidden = false; propMode === "outstanding" ? renderOutstanding() : renderAllProperties(); }
}

// openTodoCount — every open action line on the property (mine or owed).
function openTodoCount(p) {
  return (p.todos || []).filter((t) => !t.checked).length;
}

function renderPropRail() {
  const host = els.propRail;
  if (!host) return;
  host.innerHTML = "";
  const group = (label) => {
    const g = el("div", "prop-rail-group");
    g.append(el("div", "prop-rail-label", label));
    host.append(g);
    return g;
  };
  const item = (g, label, active, onclick, count, cls) => {
    const a = el("button", "prop-rail-item" + (active ? " active" : "") + (cls ? " " + cls : ""));
    a.append(el("span", "prop-rail-name", label));
    if (count !== undefined) a.append(el("span", "prop-rail-count" + (count ? " some" : ""), count ? String(count) : ""));
    a.onclick = onclick;
    g.append(a);
    return a;
  };
  // the rail is a MENU, not a second property list (owner call 2026-08-09) —
  // the all-properties table already lists every property
  const views = group("Views");
  const outN = propOutstandingGroups().reduce((n, g) => n + g.count, 0);
  item(views, "All properties", propMode === "all" || propMode === "page", () => { location.hash = "#/properties"; }, undefined);
  item(views, "Outstanding", propMode === "outstanding", () => { location.hash = "#/properties/outstanding"; }, outN);
  item(views, "Map", propMode === "map", () => { location.hash = "#/properties/map"; });
  item(views, "Parcels", propMode === "parcels", () => { location.hash = "#/properties/parcels"; });
  item(views, "Settings", propMode === "settings", () => { location.hash = "#/properties/settings"; });
  const foot = el("div", "prop-rail-foot");
  const add = el("button", "o-ghost", "＋ property");
  add.onclick = () => { location.hash = "#/properties"; propComposerOpen = true; renderProperties(); };
  foot.append(add);
  host.append(foot);
}

// projMoney: one property's plan-vs-spend numbers (Rev 3's two figures).
function projMoney(p) {
  const pj = p.project || {};
  return { budget: pj.planTotal || 0, committed: pj.committed || 0,
    paid: pj.paid || 0, over: !!pj.over };
}

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
