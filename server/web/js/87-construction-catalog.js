// ================= CONSTRUCTION — catalog and substitution (P6) =================
// The problem-local catalog: generic material families with their unknowns,
// and the actual manufacturer products this problem uses, each with checked
// documents, facts (verified only by evidence from its own documents),
// geography, check date and lifecycle. Choosing a product in the inspector
// first previews the substitution — fact diff, dimension changes and the
// re-run validation on a copy — and pins only when the owner applies it.
// Nothing here is procurement, stock, price or certified performance.

const cxc = { msg: "", units: null };

function cxSevCounts(findings) {
  const n = { "critical-unresolved": 0, advisory: 0, blocking: 0 };
  (findings || []).forEach((f) => { if (n[f.severity] !== undefined) n[f.severity]++; });
  return n;
}

async function cxSubstitutionPreview(a, c, productId, box, onCancel) {
  box.innerHTML = "";
  box.append(el("p", "cx-hint", "Previewing substitution…"));
  let res;
  try {
    res = await cxApi("GET", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + "/assemblies/" + cxEnc(a.id) + "/substitution?component=" + cxEnc(c.id) + "&product=" + cxEnc(productId));
  } catch (e) { box.innerHTML = ""; box.append(el("p", "cx-form-msg", "Preview refused: " + e.message)); return; }
  box.innerHTML = "";
  const s = res.substitution;
  const panel = el("div", "cx-subst-panel");
  panel.setAttribute("role", "region");
  panel.setAttribute("aria-label", "Substitution preview");
  panel.append(el("div", "cx-label micro-label", "Substitution preview · tentative"));
  panel.append(el("p", "cx-subst-line", s.fromLabel + " → " + s.toLabel));
  (s.dimensions || []).forEach((d) => {
    if (d.to == null && d.from == null) return;
    panel.append(el("div", "cx-diff-line " + (d.applies ? "cx-diff-chg" : "cx-diff-info"), d.key + ": " + cxFmt(d.from) + " → " + cxFmt(d.to) + " " + d.unit + (d.applies ? " (applies to this part)" : "") + (d.provenance && d.provenance !== "verified-fact" ? " · " + d.provenance : "")));
  });
  (s.factsAdded || []).forEach((f) => panel.append(el("div", "cx-diff-line cx-diff-add", "+ " + f)));
  (s.factsRemoved || []).forEach((f) => panel.append(el("div", "cx-diff-line cx-diff-del", "− " + f)));
  (s.notes || []).forEach((n) => panel.append(el("p", "cx-hint", n)));
  if (res.refused) panel.append(el("p", "cx-form-msg", "Refused: " + res.refused));
  if (res.preview) {
    const after = cxSevCounts(res.preview.findings), cur = res.current || {};
    panel.append(el("p", "cx-subst-counts", "Critical " + (cur.critical || 0) + " → " + after["critical-unresolved"] + " · advisory " + (cur.advisory || 0) + " → " + after.advisory + (res.preview.blocking ? " · BLOCKING" : "")));
    const keys = [...new Set((res.preview.findings || []).filter((f) => f.severity !== "informational").map((f) => f.ruleKey))].sort();
    panel.append(el("div", "cx-hint", "Checks after substitution: " + keys.join(", ")));
  }
  const btns = el("div", "cx-row-edit");
  const apply = pillLight("Apply substitution", async () => {
    apply.disabled = true;
    await cxCommand([{ op: "SetProduct", componentId: c.id, productId, applyDimensions: true }], { assembly: a.id, label: "Substitution" });
  });
  apply.disabled = !!res.refused || (res.preview && res.preview.blocking);
  const cancel = pillLight("Cancel", () => { box.innerHTML = ""; if (onCancel) onCancel(); });
  btns.append(apply, cancel);
  panel.append(btns);
  box.append(panel);
}

async function cxCatalogCommand(path, ops) {
  const body = { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId, operations: ops };
  try {
    const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + path, body);
    cxc.msg = "";
    if (res && res.view) cxApplyView(res.view);
  } catch (e) {
    cxc.msg = (e.message || "refused") + (e.problems && e.problems.length ? ": " + e.problems.join("; ") : "");
    cxRender();
  }
}

function cxCurrentProducts() {
  const cur = new Map();
  ((cx.view.catalog && cx.view.catalog.products) || []).forEach((p) => { const h = cur.get(p.id); if (!h || p.revision > h.revision) cur.set(p.id, p); });
  return [...cur.values()];
}

function cxPaintCatalogTab(b) {
  const cat = cx.view.catalog || { materials: [], products: [] }, ro = !!cx.view.readOnly;
  b.append(el("p", "cx-hint", "Project-local, evidence-pinned catalog. Unknown stays unknown; a product fact is verified only by a passage from that product's own retained document. No procurement, stock, price or certified performance."));
  if (cxc.msg) { const m = el("p", "cx-form-msg", cxc.msg); m.setAttribute("role", "alert"); b.append(m); }
  const pbox = el("div", "cx-block");
  pbox.append(el("div", "cx-label micro-label", "Products (" + cxCurrentProducts().length + ")"));
  const products = cxCurrentProducts();
  if (!products.length) pbox.append(el("p", "cx-empty", "No products. Generic materials only until a sourced product is added."));
  products.forEach((p) => {
    const row = el("div", "cx-prod cx-prod-" + p.lifecycle);
    row.setAttribute("data-product", p.id);
    const head = el("div", "cx-ev-head");
    head.append(el("span", "cx-src-title", p.manufacturer + " " + p.model), el("span", "micro-label", p.family + " · rev " + p.revision + " · " + p.lifecycle + " · checked " + p.checkedAt + " · " + p.geography));
    if (p.fictional) head.append(el("span", "cx-ev-fiction micro-label", "fictional fixture"));
    row.append(head);
    (p.documents || []).forEach((d) => row.append(el("div", "cx-hint", "Document: " + d.title + (d.revision ? " · revision " + d.revision.slice(0, 10) : " · not retained"))));
    Object.keys(p.dimensions || {}).sort().forEach((k) => { const d = p.dimensions[k]; row.append(el("div", "cx-hint", k + ": " + cxFmt(d.value) + " " + d.unit + " · " + d.provenance + (d.note ? " · " + d.note : ""))); });
    (p.facts || []).forEach((f) => row.append(el("div", "cx-claim " + (f.verified ? "" : "cx-claim-unverified"), (f.verified ? "verified · " : "unverified · ") + f.kind + ": " + f.text + (f.note ? " (" + f.note + ")" : ""))));
    if (!ro) {
      const acts = el("div", "cx-row-edit");
      ["active", "stale", "withdrawn"].filter((l) => l !== p.lifecycle).forEach((l) => acts.append(pillLight("Mark " + l, () => cxCatalogCommand("/products", [{ op: "SetProductLifecycle", productId: p.id, lifecycle: l }]))));
      row.append(acts);
    }
    pbox.append(row);
  });
  b.append(pbox);
  if (!ro) b.append(cxProductForm());
  const mbox = el("details", "cx-block cx-materials");
  mbox.append(el("summary", "cx-label micro-label", "Materials (" + cat.materials.length + " · generic families with explicit unknowns)"));
  cat.materials.forEach((m) => {
    const known = Object.entries(m.properties || {}).filter(([, v]) => v.value != null);
    mbox.append(el("div", "cx-mat-row", m.name + " · " + m.family + " · rev " + m.revision + " · " + (m.unknowns || []).length + " unknown" + (known.length ? " · " + known.map(([k, v]) => k + " " + v.value + " " + v.unit + " (" + v.provenance + ")").join("; ") : "")));
  });
  b.append(mbox);
  if (!ro) b.append(cxMaterialForm(cat));
}

function cxProductForm() {
  const f = el("details", "cx-block cx-form");
  f.append(el("summary", "cx-label micro-label", "Add a sourced product"));
  const field = (label, ph) => { const i = el("input", "cx-in"); i.setAttribute("aria-label", label); i.placeholder = ph || ""; f.append(i); return i; };
  const man = field("Product manufacturer"), model = field("Product model"), geo = field("Product geography (where it is sold/approved)"), checked = field("Documents checked on (YYYY-MM-DD)");
  checked.value = new Date().toISOString().slice(0, 10);
  const fam = selectEl(["corrugated-metal-roof", "standing-seam-roof", "sheet-flashing", "insulation", "membrane", "underlayment", "sealant", "closure", "fastener", "timber", "sheathing", "masonry", "mortar"]);
  fam.className = "cx-in"; fam.setAttribute("aria-label", "Product family");
  const srcs = ((cx.view.evidence && cx.view.evidence.sources) || []);
  const doc = selectEl([]); doc.className = "cx-in"; doc.setAttribute("aria-label", "Product document (retained source)");
  const none = document.createElement("option"); none.value = ""; none.textContent = "No document (stays unverified)"; doc.append(none);
  srcs.forEach((s) => { const o = document.createElement("option"); o.value = s.id; o.textContent = s.title; doc.append(o); });
  const factText = field("Product fact (as the document states it)");
  const factEv = selectEl([]); factEv.className = "cx-in"; factEv.setAttribute("aria-label", "Fact evidence");
  const nf = document.createElement("option"); nf.value = ""; nf.textContent = "No evidence"; factEv.append(nf);
  ((cx.view.evidence && cx.view.evidence.evidence) || []).forEach((e) => { const o = document.createElement("option"); o.value = e.id; o.textContent = "p." + e.page + " " + e.quote.slice(0, 60); factEv.append(o); });
  const thick = field("Product thickness (optional)"); const unit = selectEl(["mm", "cm", "in"]); unit.className = "cx-in"; unit.setAttribute("aria-label", "Product thickness unit");
  f.append(fam, doc, factEv, unit);
  f.append(pillLight("Add product", () => {
    const op = { op: "AddProduct", id: cxNewId("prd"), manufacturer: man.value, model: model.value, family: fam.value, geography: geo.value, checkedAt: checked.value, documents: doc.value ? [doc.value] : [] };
    if (factText.value) op.facts = [{ kind: "limit", text: factText.value, evidenceId: factEv.value || undefined }];
    if (thick.value) op.dimensions = { thickness: { value: Number(thick.value), unit: unit.value } };
    cxCatalogCommand("/products", [op]);
  }));
  return f;
}

function cxMaterialForm(cat) {
  const f = el("details", "cx-block cx-form");
  f.append(el("summary", "cx-label micro-label", "State a material property"));
  const mat = selectEl([]); mat.className = "cx-in"; mat.setAttribute("aria-label", "Material to edit");
  cat.materials.forEach((m) => { const o = document.createElement("option"); o.value = m.id; o.textContent = m.name; mat.append(o); });
  const prop = selectEl([]); prop.className = "cx-in"; prop.setAttribute("aria-label", "Property");
  const val = el("input", "cx-in"); val.setAttribute("aria-label", "Value"); val.inputMode = "decimal";
  const unit = el("input", "cx-in"); unit.setAttribute("aria-label", "Unit");
  const cond = el("input", "cx-in"); cond.setAttribute("aria-label", "Test condition");
  const fill = () => { prop.innerHTML = ""; const m = cat.materials.find((x) => x.id === mat.value); Object.keys((m && m.properties) || {}).sort().forEach((k) => { const o = document.createElement("option"); o.value = k; o.textContent = k; prop.append(o); }); };
  mat.onchange = fill; fill();
  f.append(mat, prop, val, unit, cond, pillLight("Save property (owner assumption)", () => cxCatalogCommand("/materials", [{ op: "SetMaterialProperty", materialId: mat.value, property: prop.value, value: Number(val.value), unit: unit.value, testCondition: cond.value, provenance: "user-assumption" }])),
    el("p", "cx-hint", "Saved as a new material revision; parts keep the revision they pinned. Verified values need a verified passage."));
  return f;
}

cxResearchTabs.splice(3, 0, ["catalog", "Catalog", (b) => cxPaintCatalogTab(b)]);
