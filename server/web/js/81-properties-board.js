// ---- BOARD at scale (design §1): grouped by deal ----

const STATUS_BUCKET = {
  construction: "active", pre_development: "active",
  negotiating: "pipeline", under_contract: "pipeline", opportunity: "pipeline",
  completed: "done", leased: "done", listed: "done", sold: "done",
};

function bucketOf(p) {
  // an explicitly active status wins even on tracked-control records
  if (STATUS_BUCKET[p.status] === "active") return "active";
  return p.control === "tracked" ? "tracked" : (STATUS_BUCKET[p.status] || "pipeline");
}

function matchesBoardFilters(p) {
  if (boardStatus && p.status !== boardStatus) return false;
  if (boardEntity && (p.entity || "").trim() !== boardEntity) return false;
  if (!boardFilter) return true;
  const q = boardFilter.toLowerCase();
  return (p.address || "").toLowerCase().includes(q) || p.slug.includes(q) ||
    (p.deal || "").toLowerCase().includes(q) || (p.entity || "").toLowerCase().includes(q);
}

// canonical status order for the flat board: work first, pipeline, then done
const STATUS_ORDER = ["construction", "pre_development", "under_contract", "negotiating",
  "opportunity", "completed", "leased", "listed", "sold"];

function boardComparator(key) {
  const rank = (p) => { const i = STATUS_ORDER.indexOf(p.status); return i < 0 ? STATUS_ORDER.length : i; };
  const addr = (a, b) => (a.short || a.address || a.slug).localeCompare(b.short || b.address || b.slug);
  const cmp = {
    status: (a, b) => rank(a) - rank(b) || addr(a, b),
    address: addr,
    budget: (a, b) => projMoney(b).budget - projMoney(a).budget || addr(a, b),
    var: (a, b) => projMoney(b).varPct - projMoney(a).varPct || addr(a, b),
  };
  return cmp[key] || cmp.status;
}

function renderBoard() {
  const host = els.propertyBoard; host.innerHTML = ""; host.hidden = false;
  const shown = propertyCache.filter((p) => !p.hidden);
  els.propertiesMeta.textContent = shown.length + " properties · " + dealCache.length + " deals";

  // toolbar (reading-page idiom): search + status/entity dropdowns + sort + composer
  const bar = el("div", "board-bar");
  const search = inputEl("search address / deal / entity…");
  search.classList.add("board-search");
  search.value = boardFilter;
  search.addEventListener("input", () => { boardFilter = search.value; renderBoardBody(body); });
  search.addEventListener("keydown", (e) => { if (e.key === "Escape") { search.value = ""; boardFilter = ""; renderBoardBody(body); } });
  bar.append(search);
  const labeledSelect = (pairs, current, onChange, title) => {
    const s = document.createElement("select");
    s.className = "pp-in board-select";
    s.title = title;
    pairs.forEach(([v, l]) => { const o = document.createElement("option"); o.value = v; o.textContent = l; s.append(o); });
    s.value = current;
    s.onchange = () => { onChange(s.value); renderBoardBody(body); };
    return s;
  };
  const statusesPresent = STATUS_ORDER.filter((st) => shown.some((p) => p.status === st));
  bar.append(labeledSelect(
    [["", "all statuses"], ...statusesPresent.map((st) => [st, st.replace(/_/g, " ")])],
    boardStatus, (v) => { boardStatus = v; }, "Filter by status"));
  const entities = [...new Set(shown.map((p) => (p.entity || "").trim()).filter(Boolean))].sort();
  bar.append(labeledSelect(
    [["", "all entities"], ...entities.map((e) => [e, e])],
    boardEntity, (v) => { boardEntity = v; }, "Filter by entity"));
  bar.append(labeledSelect(
    [["status", "by status"], ["address", "by address"], ["budget", "by budget"], ["var", "by variance"]],
    boardSort, (v) => { boardSort = v; }, "Sort"));
  bar.append(propertyComposer());
  host.append(bar);

  const body = el("div", "board-body");
  host.append(body);
  renderBoardBody(body);

  // EXPORTS — demoted to one quiet line at the bottom
  const exports = el("div", "board-exports");
  exports.append(el("span", "pp-section-head", "EXPORTS"));
  const entSel = selectEl([...new Set(["unassigned", ...shown.map((p) => (p.entity || "").trim()).filter(Boolean)])]);
  const yearIn = inputEl("year"); yearIn.value = String(new Date().getFullYear()); yearIn.classList.add("board-year");
  exports.append(entSel, yearIn, pillLight("accountant csv", async () => {
    try {
      const res = await postJSONOk("/api/realestate/export-tax",
        { entity: entSel.value === "unassigned" ? "" : entSel.value, year: yearIn.value.trim() });
      showToast("Tax csv written — " + res.lines + " lines", () =>
        window.open("/api/realestate/doc?path=" + encodeURIComponent(res.csv), "_blank"), "info");
    } catch (e) { showToast("Export failed"); }
  }));
  if (rePortalEnabled) {
    exports.append(pillLight("publish → ooda site", async () => {
      try {
        const res = await postJSONOk("/api/realestate/publish-deals", {});
        const kept = (res.kept || []).length ? " · kept as-is: " + res.kept.join(", ") : "";
        showToast("deals.json written — " + res.deals + " deals · " + res.properties + " parcels" + kept +
          " — review the diff in re-portal", null, "info");
      } catch (e) { showToast(("Publish failed: " + (e.message || "")).slice(0, 90)); }
    }));
  }
  host.append(exports);
}

// flat list: every property is one row, ordered by the chosen sort (status by
// default); deal bundles are a muted column linking to their pages.
function renderBoardBody(body) {
  body.innerHTML = "";
  const shown = propertyCache.filter((p) => !p.hidden && matchesBoardFilters(p))
    .sort(boardComparator(boardSort));
  if (!shown.length) { body.append(emptyRow("Nothing matches the filter.")); return; }
  body.append(ppCols("cols-board", ["ADDRESS", "STATUS", "DEAL", "STAGE", "UNITS", "BUDGET", "SPENT"]));
  shown.forEach((p) => body.append(boardRow(p, false)));
}

// dealStatusChip mirrors statusChip against the deal endpoint (incl. "opportunity").
function dealStatusChip(d) {
  const chip = el("span", "property-status editable status-" + (d.status || "").replace(/_/g, "-"), d.status || "—");
  chip.title = "click to change deal status";
  chip.onclick = (e) => {
    e.stopPropagation();
    const sel = selectEl([...PROPERTY_STATUSES, "opportunity"]);
    sel.value = d.status || "negotiating";
    sel.onclick = (ev) => ev.stopPropagation();
    sel.onchange = async () => {
      try { await postJSONOk("/api/deals/" + encodeURIComponent(d.slug) + "/field", { key: "status", value: sel.value }); loadProperties(); }
      catch (err) { showToast("Couldn't update status"); sel.replaceWith(chip); }
    };
    sel.onblur = () => { if (sel.parentNode) sel.replaceWith(chip); };
    chip.replaceWith(sel);
    sel.focus();
  };
  return chip;
}

async function exportUnderwrite(slug) {
  try {
    const res = await postJSONOk("/api/deals/" + encodeURIComponent(slug) + "/export-underwrite", {});
    showToast("Underwrite export written (" + res.members + " member records)", () =>
      window.open("/api/realestate/doc?path=" + encodeURIComponent(res.csv), "_blank"), "info");
  } catch (e) { showToast("Export failed"); }
}

const PROPERTY_STATUSES = ["negotiating", "under_contract", "pre_development", "construction", "completed", "leased", "listed", "sold"];
const PROPERTY_KINDS = ["rehab", "new-construction", "mixed", "hold"];

// statusChip renders a click-to-edit status: the chip swaps to a <select> in
// place; picking a value POSTs the field edit and re-renders via onSaved.
function statusChip(p, onSaved) {
  const chip = el("span", "property-status editable status-" + (p.status || "").replace(/_/g, "-"), p.status || "—");
  chip.title = "click to change status";
  chip.onclick = (e) => {
    e.stopPropagation();
    const sel = selectEl(PROPERTY_STATUSES);
    sel.value = p.status || "negotiating";
    sel.onclick = (ev) => ev.stopPropagation();
    sel.onchange = async () => {
      try { onSaved(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "status", value: sel.value })); }
      catch (err) { showToast("Couldn't update status"); sel.replaceWith(chip); }
    };
    sel.onblur = () => { if (sel.parentNode) sel.replaceWith(chip); };
    chip.replaceWith(sel);
    sel.focus();
  };
  return chip;
}

// boardRow: one property. Members render compact + left-ruled inside a deal
// group; loose records render at standard density. Columns:
// address · status · units · budget · paid%/committed%.
function boardRow(p, member) {
  const row = el("div", "property-row" +
    (p.control === "tracked" ? " tracked" : "") + (member ? " compact member" : ""));
  row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
  row.append(el("span", "property-addr", p.short || p.address || p.name));
  row.append(statusChip(p, () => loadProperties()));
  const dealCell = el("span", "property-deal");
  if (!member && p.deal) {
    const d = (dealCache || []).find((x) => x.slug === p.deal);
    const lnk = el("a", "deal-link", (d && d.name) || p.deal);
    lnk.href = "#/properties/deal/" + encodeURIComponent(p.deal);
    lnk.onclick = (e) => e.stopPropagation();
    dealCell.append(lnk);
  }
  row.append(dealCell);
  row.append(el("span", "property-stage", p.currentStage || ""));
  row.append(el("span", "property-units", p.units ? p.units + "u" : ""));
  const pm = projMoney(p);
  row.append(el("span", "property-budget", pm.budget ? fmtMoney(pm.budget) : ""));
  const out = el("span", "property-out" + (pm.over ? " over" : ""));
  if (pm.budget) {
    out.append(el("span", "out-paid", fmtMoney(pm.paid)),
      el("span", "out-committed", " " + fmtPct(pm.paid / pm.budget)));
  }
  row.append(out);
  return row;
}

// propertyComposer: the spec's entire creation form — address · entity · kind ·
// template pick (seeds the budget table). A ghost button expanding inline.
function propertyComposer() {
  const ghost = el("button", "o-ghost property-add", "＋ property");
  ghost.onclick = () => {
    const form = el("div", "prop-composer");
    const addr = inputEl("address…"); addr.classList.add("pc-addr");
    const entityAC = recordAutocomplete("entity", "entity (optional)…");
    const kind = selectEl(PROPERTY_KINDS);
    const tpl = selectEl(["no template", ...templateCache.map((t) => t.slug)]);
    tpl.title = "budget-mix template";
    const dealSel = selectEl(["unattached", ...dealCache.map((d) => d.slug)]);
    dealSel.title = "attach to a deal bundle";
    const create = el("button", "pill", "create");
    create.onclick = async () => {
      if (!addr.value.trim()) { addr.focus(); return; }
      create.disabled = true;
      try {
        await postJSONOk("/api/properties", {
          address: addr.value, entity: entityAC.value(), kind: kind.value,
          template: tpl.value === "no template" ? "" : tpl.value,
          deal: dealSel.value === "unattached" ? "" : dealSel.value,
        });
        loadProperties();
      } catch (e) { showToast("Couldn't create property"); create.disabled = false; }
    };
    const cancel = el("button", "pill light", "✕");
    cancel.onclick = () => form.replaceWith(ghost);
    form.append(addr, entityAC.el, kind, tpl, dealSel, create, cancel);
    ghost.replaceWith(form);
    addr.focus();
  };
  return ghost;
}
