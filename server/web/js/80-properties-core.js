// ---- REAL ESTATE — the domain surface (RE spec §3: eight screens) ----
// One rail, one pane. Views: Rocks (org work) · Portfolio · Decisions ·
// Intake · Money · Settings, plus the quiet Outstanding/Map/Parcels lenses;
// then the Deals group and the Properties group. A property is a rock
// structurally (stages with tasks nested); the decision log is the aion
// mirror at system/realestate/backlog.md. Every rail count derives from the
// list it labels — no literals.
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let holdingsCache = {};      // entity name → {owned, acquiring} (derived server-side)
let reBacklogCache = null;   // /api/re/backlog — {items, goalsArea}
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "portfolio"; // rocks | portfolio | decisions | intake | money | settings | outstanding | map | parcels | page | deal
let propSlug = "";    // the open property (page mode)
let propDealSlug = ""; // the open deal (deal mode)
let propTodosMeta = null; // /api/todos payload — assignees + outstanding (one truth)

function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true;
  if (els.propertyParcels) els.propertyParcels.hidden = true;
  if (els.propertySettings) els.propertySettings.hidden = true;
  closePropInspector();
  // legacy sub-tab routes fold into the rail views
  if (["work", "accounting", "statements", "contractors"].includes(tail)) {
    location.hash = "#/properties";
    return;
  }
  propSlug = "";
  propDealSlug = "";
  const VIEWS = ["rocks", "portfolio", "decisions", "intake", "money", "settings", "outstanding", "map", "parcels"];
  if (tail.startsWith("deal/")) { propMode = "deal"; propDealSlug = tail.slice(5); }
  else if (tail === "all" || tail === "") propMode = tail === "all" ? "portfolio" : propMode === "page" || propMode === "deal" ? "portfolio" : (VIEWS.includes(propMode) ? propMode : "portfolio");
  else if (VIEWS.includes(tail)) propMode = tail;
  else { propMode = "page"; propSlug = tail; }
  renderProperties();
}

async function loadProperties() {
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
    holdingsCache = d.holdings || {};
    rePortalEnabled = !!d.rePortal;
  } catch (e) { propertyCache = []; dealCache = []; templateCache = []; holdingsCache = {}; }
}

async function loadReBacklog() {
  try { reBacklogCache = await (await fetch("/api/re/backlog")).json(); }
  catch (e) { reBacklogCache = { items: [], goalsArea: null }; }
}

async function loadPropTodosMeta() {
  try { propTodosMeta = await (await fetch("/api/todos")).json(); }
  catch (e) { propTodosMeta = { outstanding: [], assignees: {}, counts: {} }; }
}

async function renderProperties() {
  await Promise.all([loadProperties(), loadPropTodosMeta(), loadReBacklog()]);
  renderPropRail();
  const openDecisions = reOpenDecisions().length;
  if (typeof setCrumbMeta === "function") {
    setCrumbMeta(activePortfolio().length + " active · " + propertyCache.length + " tracked" +
      (openDecisions ? " · " + openDecisions + " open decisions" : ""));
  }
  if (typeof railSetCount === "function") railSetCount("properties", propertyCache.length);
  if (propMode === "map") { els.propertyMapWrap.hidden = false; renderPropertyMap(); }
  else if (propMode === "parcels") renderParcelsView();
  else if (propMode === "settings") renderREsettings();
  else if (propMode === "page") { els.propertyPage.hidden = false; renderPropertyPage(propSlug); }
  else if (propMode === "deal") { els.propertyBoard.hidden = false; renderDealPage(propDealSlug); }
  else if (propMode === "rocks") { els.propertyBoard.hidden = false; renderRERocks(); }
  else if (propMode === "decisions") { els.propertyBoard.hidden = false; renderREDecisions(); }
  else if (propMode === "intake") { els.propertyBoard.hidden = false; renderREIntake(); }
  else if (propMode === "money") { els.propertyBoard.hidden = false; renderREMoney(); }
  else { els.propertyBoard.hidden = false; propMode === "outstanding" ? renderOutstanding() : renderPortfolio(); }
}

// ---- derived helpers (RE spec §2: every count computed from its list) ----

// activePortfolio: the properties the Portfolio table shows — ours (owned
// control or an entity destination), not hidden.
function activePortfolio() {
  return propertyCache.filter((p) => !p.hidden && (p.control === "owned" || p.entity));
}

// reItems / reOpenDecisions: the decision-log projections.
function reItems() { return (reBacklogCache && reBacklogCache.items) || []; }
function reOpenDecisions() {
  return reItems().filter((it) => it.kind === "decision" && it.status !== "decided");
}

// reOrgRocks: the goals `## Real Estate` area's rocks (org-level 90-day work).
function reOrgRocks() {
  const area = reBacklogCache && reBacklogCache.goalsArea;
  return (area && area.rocks) || [];
}

// rePendingIntake: pending re-backlog proposals (filled by the intake view's
// own fetch; the rail count reads the last fetch).
let reIntakeCache = [];

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
  // Views group (RE spec §3) — counts derive from the lists they label
  const views = group("Views");
  const outN = propOutstandingGroups().reduce((n, g) => n + g.count, 0);
  item(views, "Rocks", propMode === "rocks", () => { location.hash = "#/properties/rocks"; }, reOrgRocks().length);
  item(views, "Portfolio", propMode === "portfolio" || propMode === "page", () => { location.hash = "#/properties/portfolio"; }, activePortfolio().length);
  item(views, "Decisions", propMode === "decisions", () => { location.hash = "#/properties/decisions"; }, reOpenDecisions().length);
  item(views, "Intake", propMode === "intake", () => { location.hash = "#/properties/intake"; }, reIntakeCache.length || undefined);
  item(views, "Money", propMode === "money", () => { location.hash = "#/properties/money"; });
  item(views, "Settings", propMode === "settings", () => { location.hash = "#/properties/settings"; });
  // quiet lenses
  item(views, "Outstanding", propMode === "outstanding", () => { location.hash = "#/properties/outstanding"; }, outN, "quiet");
  item(views, "Map", propMode === "map", () => { location.hash = "#/properties/map"; }, undefined, "quiet");
  item(views, "Parcels", propMode === "parcels", () => { location.hash = "#/properties/parcels"; }, undefined, "quiet");
  // Deals group
  if (dealCache.length) {
    const dg = group("Deals");
    dealCache.forEach((d) => {
      item(dg, d.name || d.slug, propMode === "deal" && propDealSlug === d.slug,
        () => { location.hash = "#/properties/deal/" + encodeURIComponent(d.slug); });
    });
  }
  // Properties group — the active portfolio for one-tap page access
  const pg = group("Properties");
  activePortfolio().forEach((p) => {
    item(pg, p.short || p.address || p.slug, propMode === "page" && propSlug === p.slug,
      () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); }, openTodoCount(p) || undefined);
  });
  const foot = el("div", "prop-rail-foot");
  const add = el("button", "o-ghost", "＋ property");
  add.onclick = () => { location.hash = "#/properties/portfolio"; propComposerOpen = true; renderProperties(); };
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
