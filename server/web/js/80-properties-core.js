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
  if (tail.startsWith("deal/")) { propMode = "page"; renderDealPage(tail.slice(5)); }
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

function fmtPct(x) { return Math.round((x || 0) * 100) + "%"; }
function fmtMoney(n) { return "$" + Math.round(n || 0).toLocaleString(); }

// projMoney: one property's plan-vs-spend numbers.
function projMoney(p) {
  const pj = p.project || {};
  return { budget: pj.planTotal || 0, committed: pj.committed || 0,
    paid: pj.paid || 0, over: !!pj.over };
}

// ---- shared primitives (admin-portal design §0) ----

// ppCols: a one-line row of mono micro-labels sharing the exact grid of the
// rows beneath it — labels live once, every input aligns under them.
function ppCols(cls, labels) {
  const row = el("div", "pp-cols " + cls);
  labels.forEach((l) => row.append(el("span", "", l)));
  return row;
}

// makeDirtyBar: the one editing model — quiet inputs mark dirty; a sticky
// bottom bar appears with a single save (one PUT of the whole file).
function makeDirtyBar(host, onSave, onDiscard) {
  const bar = el("div", "dirty-bar");
  bar.hidden = true;
  const label = el("span", "dirty-label", "");
  const save = el("button", "pill", "save");
  const discard = el("button", "pill light", "discard");
  bar.append(label, save, discard);
  host.append(bar);
  let count = 0;
  const api = {
    mark() { count++; label.textContent = count + " UNSAVED CHANGE" + (count === 1 ? "" : "S"); bar.hidden = false; },
    clear() { count = 0; bar.hidden = true; },
    get dirty() { return count > 0; },
  };
  save.onclick = async () => { save.disabled = true; try { await onSave(); api.clear(); } finally { save.disabled = false; } };
  discard.onclick = () => { api.clear(); onDiscard(); };
  return api;
}

// collapsibleSection: pp-section-head with a caret + optional collapsed summary.
function collapsibleSection(host, title, summary, open) {
  const head = el("div", "pp-section-head toggle");
  const caret = el("span", "sec-caret", open ? "▾" : "▸");
  head.append(caret, el("span", "", title));
  const sum = el("span", "sec-summary", summary || "");
  head.append(sum);
  const body = el("div", "sec-body");
  body.hidden = !open;
  sum.hidden = open;
  head.onclick = () => {
    body.hidden = !body.hidden;
    caret.textContent = body.hidden ? "▸" : "▾";
    sum.hidden = !body.hidden;
  };
  host.append(head, body);
  return body;
}

// propertyTypeahead: input + filtered dropdown over all property records
// (63 items — a select is unusable). Matches address/slug/deal.
function propertyTypeahead(placeholder, onPick, initial) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  if (initial) input.value = initial;
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  const refresh = () => {
    const q = input.value.toLowerCase().trim();
    drop.innerHTML = "";
    const hits = propertyCache.filter((p) =>
      !q || (p.address || "").toLowerCase().includes(q) || p.slug.includes(q) || (p.deal || "").includes(q)).slice(0, 12);
    hits.forEach((p) => {
      const it = el("div", "ta-item", (p.short || p.address || p.slug) + (p.deal ? "  · " + p.deal : ""));
      it.onmousedown = (e) => { e.preventDefault(); input.value = p.short || p.address || p.slug; drop.hidden = true; onPick(p); };
      drop.append(it);
    });
    drop.hidden = !hits.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return wrap;
}
