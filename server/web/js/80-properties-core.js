// ---- REAL ESTATE — the domain surface, MIRRORING AION ----
// A top tab-bar (BACKLOG · PORTFOLIO · GOALS · MONEY · CONTRACTORS · MAP ·
// SETTINGS) over one body. BACKLOG consolidates intake + decisions +
// owner-grouped tasks (aion's backlog shape); PORTFOLIO folds in Deals;
// CONTRACTORS is the counterparty ledger (graduated out of the Settings
// registries, owner call 2026-08-18); SETTINGS is the org-style registry
// sub-rail. PUBLISH rides the breadcrumb. Decision log = system/realestate/
// backlog.md, the aion mirror. Bare #/properties = BACKLOG (like #/aion).
let propertyCache = [];
let dealCache = [];
let templateCache = [];
let holdingsCache = {};      // entity name → {owned, acquiring} (derived server-side)
let reBacklogCache = null;   // /api/re/backlog — {items, goalsArea, publish}
let rePortalEnabled = false; // deals.json publish configured server-side
let propMode = "backlog"; // backlog | portfolio | goals | money | contractors | map | settings | page | deal | contract | contractor
let propSlug = "";    // the open property (page mode)
let propDealSlug = ""; // the open deal (deal mode)
let propTodosMeta = null; // /api/tasks payload — assignees + outstanding (one truth)

function showProperties(h) {
  const tail = h.startsWith("#/properties/") ? decodeURIComponent(h.slice("#/properties/".length)) : "";
  els.propertyPage.hidden = true;
  els.propertyMapWrap.hidden = true;
  els.propertyBoard.hidden = true;
  if (els.propertySettings) els.propertySettings.hidden = true;
  closePropInspector();
  // legacy sub-tab routes fold into the current tabs
  if (["work", "accounting", "statements"].includes(tail)) { location.hash = "#/properties"; return; }
  if (["decisions", "intake", "outstanding"].includes(tail)) { location.hash = "#/properties"; return; } // → BACKLOG
  if (["parcels", "all"].includes(tail)) { location.hash = "#/properties/portfolio"; return; }
  if (tail === "rocks") { location.hash = "#/properties/goals"; return; } // renamed tab (overhaul)
  propSlug = "";
  propDealSlug = "";
  const VIEWS = ["backlog", "portfolio", "goals", "money", "contractors", "settings", "map"];
  if (tail.startsWith("deal/")) { propMode = "deal"; propDealSlug = tail.slice(5); }
  else if (tail === "contract-new") { propMode = "contract-new"; }
  else if (tail.startsWith("contract/")) { propMode = "contract"; propSlug = tail.slice(9); }
  else if (tail.startsWith("contractor/")) { propMode = "contractor"; propSlug = tail.slice(11); }
  // bare #/properties = BACKLOG, always — the aion mirror (showAion: tail ||
  // "backlog"). The old sticky-mode read made the BACKLOG chip (href
  // #/properties) a no-op from any other tab.
  else if (tail === "") propMode = "backlog";
  else if (VIEWS.includes(tail)) propMode = tail;
  else { propMode = "page"; propSlug = tail; }
  renderProperties();
}

// renderReToggle — mirror showAion's chip-active toggle. The record pages keep
// the tab they open FROM lit: property/deal/contract come off PORTFOLIO, a
// contractor record off CONTRACTORS. Without this a record page lights nothing
// and the tab bar reads as "you are nowhere".
const RE_TAB_OF = { page: "portfolio", deal: "portfolio", contract: "portfolio", "contract-new": "portfolio", contractor: "contractors" };
function renderReToggle() {
  const active = RE_TAB_OF[propMode] || propMode;
  els.reToggle && els.reToggle.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === active));
}

// renderRePublishRail — mirror renderAionRail: the deals.json PUBLISH badge in
// the page header actions (next to the tabs), plus the header's status meta.
function renderRePublishRail() {
  const rail = els.rePublishRail;
  if (!rail) return;
  rail.innerHTML = "";
  if (els.propertiesView.hidden) return;
  const pub = (reBacklogCache && reBacklogCache.publish) || {};
  if (els.reMeta) {
    els.reMeta.textContent = activePortfolio().length + " active · " + propertyCache.length + " tracked";
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
  try { propTodosMeta = await (await fetch("/api/tasks")).json(); }
  catch (e) { propTodosMeta = { outstanding: [], assignees: {}, counts: {} }; }
}

async function renderProperties() {
  await Promise.all([loadProperties(), loadPropTodosMeta(), loadReBacklog()]);
  renderReToggle();
  renderRePublishRail();
  // the WORK rail counts WORK — open tasks + open decisions, the same
  // derivation the BACKLOG page renders from (reOpenCount in 85-re-domain.js)
  if (typeof railSetCount === "function") railSetCount("properties", reOpenCount());
  if (propMode === "map") { els.propertyMapWrap.hidden = false; renderPropertyMap(); }
  else if (propMode === "settings") renderREsettings();
  else if (propMode === "page") { els.propertyPage.hidden = false; renderPropertyPage(propSlug); }
  else if (propMode === "deal") { els.propertyBoard.hidden = false; renderDealPage(propDealSlug); }
  else if (propMode === "contract") { els.propertyBoard.hidden = false; renderContractPage(propSlug); }
  else if (propMode === "contract-new") { els.propertyBoard.hidden = false; renderContractForm(); }
  else if (propMode === "contractor") { els.propertyBoard.hidden = false; renderContractorPage(propSlug); }
  else if (propMode === "goals") { els.propertyBoard.hidden = false; renderREGoals(); }
  else if (propMode === "money") { els.propertyBoard.hidden = false; renderREMoney(); }
  else if (propMode === "contractors") { els.propertyBoard.hidden = false; renderREContractors(); }
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

// openTodoCount — every open action line on the property (mine or owed).
function openTodoCount(p) {
  return (p.tasks || []).filter((t) => !t.checked).length;
}

// (renderPropRail deleted — the left rail became the AION-style top tab-bar;
//  #reToggle + renderRePublishRail replace it. Deals live in Portfolio, the
//  per-property list is redundant with the Portfolio board, Parcels is gone.)

// projMoney: one property's plan-vs-spend numbers.
//
// paid is CASH — money the ledger can prove left a bank account, plus the
// purchase price of a deal that actually closed. recognized adds the accrual
// (work done at a firm price with no expense row yet). They differ, and SPENT
// showing the accrual is what made it unauditable.
function projMoney(p) {
  const pj = p.project || {};
  return { budget: pj.planTotal || 0, committed: pj.committed || 0,
    paid: pj.paid || 0, recognized: pj.recognized || pj.paid || 0, over: !!pj.over };
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
