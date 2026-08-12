// ---- REAL ESTATE — the domain surface, MIRRORING AION ----
// A top tab-bar (BACKLOG · PORTFOLIO · ROCKS · MONEY · MAP · SETTINGS) over one
// body. BACKLOG consolidates intake + decisions + owner-grouped tasks (aion's
// backlog shape); PORTFOLIO folds in Deals; SETTINGS is the org-style registry
// sub-rail. PUBLISH rides the breadcrumb. Decision log = system/realestate/
// backlog.md, the aion mirror. Bare #/properties = BACKLOG (like #/aion).
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let holdingsCache = {};      // entity name → {owned, acquiring} (derived server-side)
let reBacklogCache = null;   // /api/re/backlog — {items, goalsArea, publish}
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "backlog"; // backlog | portfolio | rocks | money | map | settings | page | deal
let propSlug = "";    // the open property (page mode)
let propDealSlug = ""; // the open deal (deal mode)
let propTodosMeta = null; // /api/todos payload — assignees + outstanding (one truth)

function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true;
  if (els.propertySettings) els.propertySettings.hidden = true;
  closePropInspector();
  // legacy sub-tab routes fold into the current tabs
  if (["work", "accounting", "statements", "contractors"].includes(tail)) { location.hash = "#/properties"; return; }
  if (["decisions", "intake", "outstanding"].includes(tail)) { location.hash = "#/properties"; return; } // → BACKLOG
  if (["parcels", "all"].includes(tail)) { location.hash = "#/properties/portfolio"; return; }
  propSlug = "";
  propDealSlug = "";
  const VIEWS = ["backlog", "portfolio", "rocks", "money", "settings", "map"];
  if (tail.startsWith("deal/")) { propMode = "deal"; propDealSlug = tail.slice(5); }
  else if (tail === "") propMode = (propMode === "page" || propMode === "deal") ? "backlog" : (VIEWS.includes(propMode) ? propMode : "backlog");
  else if (VIEWS.includes(tail)) propMode = tail;
  else { propMode = "page"; propSlug = tail; }
  renderProperties();
}

// renderReToggle — mirror showAion's chip-active toggle. page/deal keep
// PORTFOLIO lit (they open from it).
function renderReToggle() {
  const active = (propMode === "page" || propMode === "deal") ? "portfolio" : propMode;
  els.reToggle && els.reToggle.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === active));
}

// renderRePublishRail — mirror renderAionRail: the deals.json PUBLISH badge in
// the breadcrumb (was the rail foot), plus the crumb meta line.
function renderRePublishRail() {
  const rail = els.rePublishRail;
  if (!rail) return;
  rail.innerHTML = "";
  if (els.propertiesView.hidden) return;
  const pub = (reBacklogCache && reBacklogCache.publish) || {};
  if (typeof setCrumbMeta === "function") {
    setCrumbMeta(activePortfolio().length + " active · " + propertyCache.length + " tracked");
  }
  if (!pub.configured) return;
  const btn = el("button", "aion-publish-btn re-publish-btn", "PUBLISH");
  const n = Object.values(pub.dirty || {}).filter(Boolean).length;
  if (n) {
    const badge = el("span", "aion-publish-badge", String(n));
    badge.title = n + " contract file" + (n === 1 ? "" : "s") + " with unpublished changes";
    btn.append(badge);
  }
  btn.onclick = openRePublishPanel;
  rail.append(btn);
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
  renderReToggle();
  renderRePublishRail();
  if (typeof railSetCount === "function") railSetCount("properties", propertyCache.length);
  if (propMode === "map") { els.propertyMapWrap.hidden = false; renderPropertyMap(); }
  else if (propMode === "settings") renderREsettings();
  else if (propMode === "page") { els.propertyPage.hidden = false; renderPropertyPage(propSlug); }
  else if (propMode === "deal") { els.propertyBoard.hidden = false; renderDealPage(propDealSlug); }
  else if (propMode === "rocks") { els.propertyBoard.hidden = false; renderRERocks(); }
  else if (propMode === "money") { els.propertyBoard.hidden = false; renderREMoney(); }
  else if (propMode === "portfolio") { els.propertyBoard.hidden = false; renderPortfolio(); }
  else { els.propertyBoard.hidden = false; renderREBacklog(); } // default = BACKLOG
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

// (renderPropRail deleted — the left rail became the AION-style top tab-bar;
//  #reToggle + renderRePublishRail replace it. Deals live in Portfolio, the
//  per-property list is redundant with the Portfolio board, Parcels is gone.)

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
