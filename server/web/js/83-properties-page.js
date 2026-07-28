// ---- underwriting editor engine (design §2): quiet inputs over the
// engine-shaped source object. The client mutates ONE object in memory and PUTs
// the whole thing on save — unknown fields ride along untouched (fidelity). ----

// uwInput: a quiet input bound to obj[key]. opts: pct (0.75↔75), money, int,
// suffix ("%", "$", "mo", "yr", "%/yr").
function uwInput(obj, key, opts, dirty) {
  const wrap = el("span", "uw-val");
  const input = document.createElement("input");
  input.className = "uw-in";
  const show = (v) => {
    if (v === undefined || v === null || v === "") return "";
    if (opts.pct) return String(Math.round(v * 10000) / 100);
    return String(v);
  };
  input.value = show(obj[key]);
  input.addEventListener("focus", () => input.select());
  input.addEventListener("change", () => {
    const raw = input.value.trim();
    let v;
    if (raw === "") v = undefined;
    else if (opts.text) v = raw;
    else {
      v = parseFloat(raw.replace(/[$,%]/g, ""));
      if (isNaN(v)) { input.value = show(obj[key]); return; }
      if (opts.pct) v = v / 100;
      if (opts.int) v = Math.round(v);
    }
    if (v === undefined) delete obj[key]; else obj[key] = v;
    dirty.mark();
  });
  wrap.append(input);
  if (opts.suffix) wrap.append(el("span", "uw-suffix", opts.suffix));
  return wrap;
}

// uwGrid: label/value pairs in a two-column definition grid.
function uwGrid(host, obj, fields, dirty) {
  const grid = el("div", "uw-grid");
  fields.forEach(([key, label, opts]) => {
    grid.append(el("span", "uw-label", label));
    grid.append(uwInput(obj, key, opts || {}, dirty));
  });
  host.append(grid);
  return grid;
}

// uwRows: the shared array editor — aligned rows of quiet inputs, hover ✕,
// ghost ＋ row, optional Σ footer.
function uwRows(host, obj, key, cols, dirty, opts) {
  opts = opts || {};
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    const arr = obj[key] || [];
    box.append(ppCols("cols-" + cols.length, cols.map((c) => c.label.toUpperCase())));
    arr.forEach((item, i) => {
      const row = el("div", "uw-row cols-" + cols.length);
      cols.forEach((c) => row.append(uwInput(item, c.key, c.opts || {}, dirty)));
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { arr.splice(i, 1); obj[key] = arr; dirty.mark(); render(); };
      row.append(x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ " + (opts.addLabel || "row"));
    add.onclick = () => {
      const item = {};
      cols.forEach((c) => { if (c.init !== undefined) item[c.key] = c.init; });
      obj[key] = [...arr, item];
      dirty.mark();
      render();
    };
    box.append(add);
    if (opts.footer) box.append(el("div", "uw-footer", opts.footer(arr)));
  };
  render();
  host.append(box);
}

// uwKV: an object's key→number rows (soft_cost_items, opex_items).
function uwKV(host, obj, key, dirty, opts) {
  opts = opts || {};
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    const m = obj[key] || {};
    Object.keys(m).forEach((k) => {
      const row = el("div", "uw-row cols-kv");
      const kIn = document.createElement("input");
      kIn.className = "uw-in uw-key";
      kIn.value = k;
      if (opts.keyList) kIn.setAttribute("list", opts.keyList);
      kIn.addEventListener("change", () => {
        const nk = kIn.value.trim();
        if (!nk || nk === k) { kIn.value = k; return; }
        m[nk] = m[k]; delete m[k]; dirty.mark(); render();
      });
      row.append(kIn);
      const holder = { v: m[k] };
      const vIn = uwInput(holder, "v", { suffix: opts.suffixFor ? opts.suffixFor(k) : "" }, dirty);
      vIn.querySelector("input").addEventListener("change", () => { m[k] = holder.v; });
      row.append(vIn);
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { delete m[k]; dirty.mark(); render(); };
      row.append(x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ " + (opts.addLabel || "item"));
    add.onclick = () => {
      const m2 = obj[key] || {};
      let n = "new item", i = 2;
      while (n in m2) n = "new item " + i++;
      m2[n] = 0; obj[key] = m2; dirty.mark(); render();
    };
    box.append(add);
  };
  render();
  host.append(box);
}

const OPEX_SUFFIX = (k) => /per_unit_year/.test(k) ? "$/unit/yr" : /per_unit_month/.test(k) ? "$/unit/mo" : "%";
const OPEX_KEYS = ["management_rate", "maintenance_rate", "reserves_rate", "property_tax_rate",
  "property_tax_pct_of_value", "insurance_rate", "insurance_per_unit_year", "utilities_per_unit_month"];
(function () {
  const dl = document.getElementById("opexKeys");
  if (dl) OPEX_KEYS.forEach((k) => { const o = document.createElement("option"); o.value = k; dl.append(o); });
})();

// dealLevelSections renders the deal-level engine groups into host.
function dealLevelSections(host, src, dirty) {
  let sec = collapsibleSection(host, "RENTS & UNITS", "", true);
  uwGrid(sec, src, [
    ["vacancy_rate", "vacancy rate", { pct: true, suffix: "%" }],
    ["rent_growth", "rent growth", { pct: true, suffix: "%/yr" }],
    ["market_cap_rate", "market cap rate", { pct: true, suffix: "%" }],
    ["lease_up_months", "lease-up", { int: true, suffix: "mo" }],
    ["lease_up_vacancy_rate", "lease-up vacancy", { pct: true, suffix: "%" }],
  ], dirty);

  sec = collapsibleSection(host, "FINANCING", "", true);
  uwGrid(sec, src, [
    ["construction_loan_ltc", "construction ltc", { pct: true, suffix: "%" }],
    ["construction_interest_rate", "construction rate", { pct: true, suffix: "%" }],
    ["perm_ltv", "perm ltv", { pct: true, suffix: "%" }],
    ["perm_interest_rate", "perm rate", { pct: true, suffix: "%" }],
    ["perm_amort_years", "perm amort", { int: true, suffix: "yr" }],
  ], dirty);

  sec = collapsibleSection(host, "OPEX", "", true);
  if (!src.opex_items) src.opex_items = {};
  uwKV(sec, src, "opex_items", dirty, { suffixFor: OPEX_SUFFIX, keyList: "opexKeys", addLabel: "opex item" });
  uwGrid(sec, src, [["opex_growth", "opex growth", { pct: true, suffix: "%/yr" }]], dirty);

  sec = collapsibleSection(host, "EXIT & HOLD", "", true);
  uwGrid(sec, src, [
    ["hold_years", "hold", { int: true, suffix: "yr" }],
    ["exit_cap_rate", "exit cap", { pct: true, suffix: "%" }],
    ["selling_cost_pct", "selling costs", { pct: true, suffix: "%" }],
  ], dirty);
  sec.append(el("div", "uw-sub", "capex schedule"));
  uwRows(sec, src, "capex_schedule", [
    { key: "through_year", label: "through yr", opts: { int: true } },
    { key: "amount", label: "$ /unit/yr", opts: {} , init: 500 },
  ], dirty, { addLabel: "tier" });
  sec.append(el("div", "uw-sub", "equity"));
  uwRows(sec, src, "equity_structure", [
    { key: "role", label: "role", opts: { text: true }, init: "General Partner (GP)" },
    { key: "share", label: "share %", opts: { pct: true } , init: 1 },
    { key: "entity", label: "entity", opts: { text: true }, init: "OODA Group" },
  ], dirty, {
    addLabel: "partner",
    footer: (arr) => {
      const sum = arr.reduce((a, e) => a + (e.share || 0), 0);
      return "Σ " + Math.round(sum * 100) + "%" + (Math.abs(sum - 1) < 0.001 ? " ✓" : " ✕ should be 100%");
    },
  });

  sec = collapsibleSection(host, "NARRATIVE", "", true);
  const ta = document.createElement("textarea");
  ta.className = "uw-narrative";
  ta.value = src.narrative_note || "";
  ta.addEventListener("change", () => { src.narrative_note = ta.value.trim(); dirty.mark(); });
  sec.append(ta);
}

// documentsSection: the documents[] list editor + drop-to-add.
function documentsSection(host, src, dirty, docSlug, docKind) {
  const sec = collapsibleSection(host, "DOCUMENTS", "", true);
  uwRows(sec, src, "documents", [
    { key: "name", label: "name", opts: { text: true } },
    { key: "category", label: "category", opts: { text: true } },
    { key: "file", label: "file", opts: { text: true } },
  ], dirty, { addLabel: "document" });
  const drop = el("div", "pp-dropzone", "drop a file to upload + add a row");
  const pick = document.createElement("input");
  pick.type = "file"; pick.hidden = true;
  const upload = async (files) => {
    if (!files || !files.length) return;
    const fd = new FormData();
    for (const f of files) fd.append("file", f);
    try {
      const res = await fetch("/api/properties/" + encodeURIComponent(docSlug) + "/docs", { method: "POST", body: fd });
      if (!res.ok) throw new Error(await res.text());
      const saved = (await res.json()).saved || [];
      saved.forEach((rel) => {
        const fn = rel.split("/").pop();
        src.documents = [...(src.documents || []), { name: fn.replace(/\.[^.]+$/, ""), category: "", file: fn }];
      });
      dirty.mark();
      showToast("Uploaded — document row added (save to keep)");
    } catch (e) { showToast("Upload failed"); }
  };
  drop.onclick = () => pick.click();
  pick.onchange = () => { upload(pick.files); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); upload(e.dataTransfer.files); });
  sec.append(drop, pick);
  sec.append(el("div", "uw-footnote", "file syncs to the site repo on publish"));
}

// propertyLevelSections: the per-parcel engine fields.
function propertyLevelSections(host, prop, dirty, opts) {
  opts = opts || {};
  const sec = host;
  uwGrid(sec, prop, [
    ["purchase_price", "purchase price", { suffix: "$" }],
    ["year_built", "year built", { int: true }],
    ["total_units", "total units", { int: true }],
    ["total_sf", "total sf", { int: true }],
    ["contingency_pct", "contingency", { pct: true, suffix: "%" }],
    ["closing_costs", "closing costs", { suffix: "$" }],
    ["carry_cost", "soft costs", { suffix: "$" }], // interest · taxes · insurance · utilities during construction
    ["construction_period_months", "constr. period", { int: true, suffix: "mo" }],
  ], dirty);
  // hard_costs: derived from the work plan once stage ests exist (syncHardCosts
  // rewrites it server-side); editable only while the plan is unestimated.
  const hardGrid = el("div", "uw-grid");
  hardGrid.append(el("span", "uw-label", "hard costs"));
  if (opts.derivedHard) {
    hardGrid.append(el("span", "uw-derived",
      "$" + Math.round(prop.hard_costs || 0).toLocaleString() + " · derived from work plan"));
  } else {
    uwGrid(sec, prop, [["hard_costs", "hard costs", { suffix: "$" }]], dirty);
  }
  if (opts.derivedHard) sec.append(hardGrid);
  sec.append(el("div", "uw-sub", "unit mix"));
  uwRows(sec, prop, "unit_mix", [
    { key: "type", label: "type", opts: { text: true }, init: "1 BR" },
    { key: "count", label: "count", opts: { int: true }, init: 1 },
    { key: "rent", label: "rent $/mo", opts: {}, init: 1200 },
  ], dirty, {
    addLabel: "unit type",
    footer: (arr) => {
      const u = arr.reduce((a, e) => a + (e.count || 0), 0);
      const r = arr.reduce((a, e) => a + (e.count || 0) * (e.rent || 0), 0);
      return "Σ " + u + "u · $" + Math.round(r).toLocaleString() + "/mo";
    },
  });
  sec.append(el("div", "uw-sub", "soft cost items (site engine only — budget these as Pre-development tasks)"));
  uwKV(sec, prop, "soft_cost_items", dirty, { suffixFor: () => "$", addLabel: "item" });
  // construction_phases: retired from the editor (§3 — the stage list IS the
  // schedule); the field itself survives untouched in source.json for the site.
}

// ---- DEAL PAGE (design §2) ----

async function renderDealPage(slug) {
  const host = els.propertyPage; host.hidden = false; host.textContent = "loading…";
  let d;
  try { d = await (await fetch("/api/deals/" + encodeURIComponent(slug))).json(); }
  catch (e) { host.innerHTML = ""; host.append(emptyRow("Deal not found.")); return; }
  host.innerHTML = "";

  const back = el("button", "pill light pp-back", "‹ board");
  back.onclick = () => { location.hash = "#/properties"; };
  host.append(back);

  const head = el("div", "pp-head");
  head.append(el("h2", "pp-title", d.deal.name));
  const chips = el("div", "pp-chips");
  chips.append(dealStatusChip(d.deal));
  const src = d.source || {};
  [src.project_type, src.tier, src.default_strategy].filter(Boolean).forEach((c) => chips.append(el("span", "pp-chip", String(c))));
  head.append(chips);
  host.append(head);

  // ACTUALS — manifest's own number; the site can't show this.
  host.append(el("div", "pp-section-head tag-manifest", "ACTUALS · MANIFEST ONLY"));
  const dm = (d.members || []).reduce((a, p) => {
    const pm = projMoney(p);
    a.budget += pm.budget; a.committed += pm.committed; a.paid += pm.paid;
    return a;
  }, { budget: 0, committed: 0, paid: 0 });
  const sum = el("div", "pp-rollup");
  sum.append(rollupStat("budget", "", fmtMoney(dm.budget)));
  sum.append(rollupStat("spent", fmtPct(dm.budget > 0 ? dm.paid / dm.budget : 0), fmtMoney(dm.paid)));
  sum.append(rollupStat("remaining", "", fmtMoney(dm.budget - dm.paid)));
  sum.append(rollupStat("ledgers", "", d.membersWithLedgers + "/" + (d.members || []).length));
  host.append(sum);
  const dtogo = el("div", "pp-togo");
  dtogo.append(el("span", "", "committed " + fmtMoney(dm.committed)));
  host.append(dtogo);

  // MEMBERS
  host.append(el("div", "pp-section-head", "MEMBERS"));
  const mbox = el("div", "pp-members");
  mbox.append(ppCols("cols-board", ["ADDRESS", "STATUS", "", "STAGE", "UNITS", "BUDGET", "SPENT"]));
  (d.members || []).forEach((p) => mbox.append(boardRow(p, true)));
  host.append(mbox);

  if (!d.source) {
    host.append(emptyRow("No source sidecar — run the -expand migration."));
    return;
  }

  // UNDERWRITING — the full engine editor over the deal-level source.
  host.append(el("div", "pp-section-head tag-engine", "UNDERWRITING · FEEDS SITE ENGINE"));
  const uw = el("div", "uw-block");
  host.append(uw);
  const orig = JSON.stringify(d.source);
  const dirty = makeDirtyBar(host,
    async () => {
      await putJSON("/api/deals/" + encodeURIComponent(slug) + "/source", d.source);
      showToast("Saved — source.json updated");
    },
    () => { renderDealPage(slug); });
  dealLevelSections(uw, d.source, dirty);
  documentsSection(uw, d.source, dirty, (d.members[0] || {}).slug || slug, "deal");

  const foot = el("div", "pp-foot");
  foot.append(pillLight("underwrite ↓", () => exportUnderwrite(slug)));
  const noteBtn = el("button", "pill light", "open note →");
  noteBtn.onclick = () => { _noteReturn = "#/properties/deal/" + encodeURIComponent(slug); openNoteByPath(d.deal.path); };
  foot.append(noteBtn);
  host.append(foot);
}

async function putJSON(url, body) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}
