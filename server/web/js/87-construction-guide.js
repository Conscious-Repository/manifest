// ================= CONSTRUCTION — the plain-language guide =================
// Construction problems are worked by people who are not architects. This
// layer sits over the workbench and says, in plain words: what this problem
// is deciding, which step comes next (with a button that goes there), what
// each technical choice means, and what every open issue asks of you. It
// changes nothing in the model; every edit still goes through the same typed
// commands as before.

// ---- plain labels -------------------------------------------------------------
const CX_PLAIN_WALL = {
  unknown: "Not known yet",
  "solid-bonded": "Solid brick — two or more layers bonded together, no air gap",
  cavity: "Cavity wall — an outer brick layer with an air gap behind it",
  other: "Something else (stone, block, siding…)",
};
const CX_PLAIN_PROV = {
  unknown: "Not known",
  "user-assumption": "We know it / believe it (not inspected)",
  "verified-fact": "Checked on site or documented",
};
const CX_PLAIN_ORIENT = {
  unresolved: "Not set yet",
  headwall: "At the roof's high edge — the top of the slope runs into the wall (headwall)",
  sidewall: "Along the roof's side — the wall runs downhill beside the slope (sidewall)",
};
const CX_PLAIN_STRAT = {
  unresolved: { label: "Not chosen yet", what: "Choose how the flashing is held into the wall. Each option below says what it is, why you'd pick it and what to watch." },
  "apron-surface-counterflashing": {
    label: "Apron + counterflashing fastened to the brick face",
    what: "A bent metal apron laps over the roof panels and turns up the wall. A second strip (the counterflashing) is screwed to the face of the brick and caulked along its top edge, covering the apron's upturn.",
    good: "No cutting into the brick; quick, and easy to redo.",
    watch: "Everything depends on the caulk bead along the top — it needs checking and re-caulking every few years.",
  },
  "apron-reglet-counterflashing": {
    label: "Apron + counterflashing set into a cut mortar joint (reglet)",
    what: "The same apron, but the counterflashing's top edge is tucked into a slot cut into a horizontal mortar joint above the roof, then sealed.",
    good: "Water can't run down behind it — the long-lasting standard detail for solid brick.",
    watch: "Needs a sound mortar joint at the right height and careful cutting (old soft mortar). Only for solid walls. A good one to have a mason do or check.",
  },
  "apron-through-wall-flashing": {
    label: "Through-wall flashing (cavity walls only)",
    what: "A tray built through the outer brick layer that catches water inside the wall's cavity and drains it out above the roof.",
    good: "The right fix when the wall has an air gap that collects water.",
    watch: "Means removing and relaying brick. Not for a solid wall.",
  },
  "sidewall-surface-counterflashing": {
    label: "Side flashing + counterflashing fastened to the brick face",
    what: "A flashing runs along the side of the roof and turns up the wall; a counterflashing fastened to the brick face covers it.",
    good: "No cutting into the brick.",
    watch: "Depends on the caulk bead along the top.",
  },
  "sidewall-reglet-counterflashing": {
    label: "Side flashing + counterflashing set into stepped mortar joints",
    what: "The counterflashing follows the slope, stepped into cut mortar joints.",
    good: "Long-lasting on solid brick.",
    watch: "Fiddly cutting along a slope; solid walls only.",
  },
};

// ---- the glossary -------------------------------------------------------------
const CX_GLOSSARY = [
  ["Headwall / sidewall", "Where the roof meets a wall: at the roof's high edge (headwall) or along its sloping side (sidewall)."],
  ["Apron (base) flashing", "Bent metal laid over the top of the roof panels that turns up the wall, so water runs down onto the roof instead of into the joint."],
  ["Counterflashing", "A second strip of metal on the wall that overlaps the apron's turned-up edge, so water can't get behind it."],
  ["Reglet", "A slot cut into a mortar joint that the counterflashing's top edge tucks into."],
  ["Wythe", "One layer of brick. A 'two-wythe' wall is two bricks thick."],
  ["Solid vs cavity wall", "Solid: brick layers bonded together. Cavity: an outer brick layer with an air gap behind it that needs its own drainage."],
  ["Underlayment", "The waterproof sheet under the metal panels — the second line of defence."],
  ["Air / vapour-control layer", "A membrane that stops warm indoor air (and its moisture) from reaching cold layers where it would condense."],
  ["Closure strip", "A foam strip shaped like the corrugations that plugs the gaps under the flashing."],
  ["Nailbase", "Rigid insulation board with a plywood or OSB top, so roofing can be screwed down on top of insulation."],
  ["Batten", "A strip of wood or metal the panels are screwed to."],
  ["Pitch / slope", "How steep the roof is: degrees, or inches of rise per 12 inches (2.5:12 ≈ 12°). Corrugated panels usually need 3:12 (14°) or more."],
  ["Lap", "How far one piece overlaps the next. Laps always shed water downhill."],
  ["Galvanic corrosion", "Two different metals touching in water eat one another (e.g. copper or lead against galvanized or aluminum)."],
  ["Alternative / variant", "One complete version of the detail. Make a variant to compare options side by side."],
  ["Critical vs advisory", "Critical: must be answered before building. Advisory: worth checking, doesn't block."],
  ["Illustrative", "A placeholder number for the drawing, not a measurement or spec."],
];

// ---- what each open issue asks of you ------------------------------------------
const CX_ISSUE_PLAIN = {
  "wall.condition.unknown": "Say what the wall is (solid brick or a cavity wall) in the Assembly pane.",
  "wall.condition.unverified": "The wall type is what you believe, not inspected. Confirm it on site (look at the brick pattern at a corner, or a hole) when you can.",
  "junction.orientation.unresolved": "Say where the roof meets the wall: at its high edge (headwall) or along its side (sidewall).",
  "junction.strategy.unresolved": "Pick how the flashing is held into the wall — the Assembly pane explains each option.",
  "junction.strategy.conditional": "This flashing method works only if its condition holds (usually: the wall is solid and the mortar is sound). Confirm that before building.",
  "junction.strategy.inapplicable": "This flashing method doesn't fit the wall you set. Pick another one.",
  "masonry.reglet.cutting": "Cutting a mortar joint: check the joint is sound at the right height and use a tool that won't chip old brick. A mason can check one spot first.",
  "fastener.specification": "Pick the actual screws (type, length, coating) from the roof panel maker's instructions.",
  "structure.specification": "Structural bits (fastener spacing, uplift) need the panel maker's table or a qualified person's sign-off.",
  "structure.fastener.embedment": "Screws must reach far enough into what holds them — check the length against the layers they pass through.",
  "structure.fastener.stack-changed": "The layer stack changed, so check the screw lengths again.",
  "moisture.transition.": "The membranes have to join up where the roof meets the wall (water and air seals continuous, taped or lapped). Plan how they turn up behind the flashing.",
  "moisture.vapour-control": "Where the air/vapour-control layer goes depends on the climate. Set the climate (St. Louis: hot-humid summers, cold winters) in the Problem tab.",
  "roof.minimum-pitch": "Check the panel maker's minimum slope against your roof's slope.",
  "material.compatibility": "Make sure the metals touching each other won't corrode one another.",
  "product.manufacturer-reference": "Pick the actual product; its maker's installation detail decides the exact pieces.",
  "evidence.unverified": "Back this up with a source (a maker's instructions or a guide) — add it under Evidence.",
  "dimensions.illustrative": "The numbers are placeholders until measured — replace them as you measure.",
  "thermal.bridging": "Heat leaks around metal that crosses the insulation — note it; it's usually acceptable in a sunroom.",
  "timber.exposure": "Exposed wood needs protecting from weather during the build.",
};
function cxIssuePlain(is) {
  const k = Object.keys(CX_ISSUE_PLAIN).find((p) => is.ruleKey === p || is.ruleKey.startsWith(p));
  return k ? CX_ISSUE_PLAIN[k] : "";
}

// ---- the guide ------------------------------------------------------------------
// open by default on a wide screen, folded to its "Next:" line on a phone,
// until the owner chooses
let cxMoreTools = false; // the model toolbar's second row, open once asked for
const cxPhone = () => matchMedia("(max-width: 860px)").matches;
let cxGuideOpen = (() => { try { const v = localStorage.getItem("cx.guide.open"); return v === null ? !cxPhone() : v !== "0"; } catch (e) { return !cxPhone(); } })();
function cxGo(pane, tab) {
  cx.pane = pane;
  if (tab) cx.tab = tab;
  cxRender();
}
function cxGuideBlock() {
  const v = cx.view, a = cxAsm(), rep = cxReport(), p = v.problem;
  const box = el("details", "cx-guide");
  box.open = cxGuideOpen;
  box.ontoggle = () => { cxGuideOpen = box.open; try { localStorage.setItem("cx.guide.open", box.open ? "1" : "0"); } catch (e) {} };
  const open = (rep && rep.issues || []).filter((i) => i.status !== "resolved" && i.status !== "acknowledged");
  const crit = open.filter((i) => i.severity === "critical-unresolved"), adv = open.filter((i) => i.severity === "advisory");
  const j = a ? a.junction : null;
  const steps = !a ? [] : [
    { done: j.wallCondition.value !== "unknown", title: "Say what the wall is", now: CX_PLAIN_WALL[j.wallCondition.value] + (j.wallCondition.value !== "unknown" ? " · " + CX_PLAIN_PROV[j.wallCondition.provenance] : ""), go: () => cxGo("assembly") },
    { done: j.orientation !== "unresolved", title: "Say where the roof meets the wall", now: CX_PLAIN_ORIENT[j.orientation], go: () => cxGo("assembly") },
    { done: j.strategy !== "unresolved", title: "Pick how the flashing holds into the wall", now: (CX_PLAIN_STRAT[j.strategy] || {}).label || j.strategy, go: () => cxGo("assembly") },
    { done: crit.length === 0, title: "Answer what's still open", now: crit.length + " must answer · " + adv.length + " worth checking", go: () => cxGo("research", "issues") },
    { done: !!p.selectedAssembly, title: "Compare the options and choose one", now: (p.alternatives || []).length + " option" + ((p.alternatives || []).length === 1 ? "" : "s") + (p.selectedAssembly ? " · chosen" : " · none chosen yet"), go: () => cxGo("research", "alternatives") },
  ];
  const next = steps.find((s) => !s.done);
  const sum = el("summary", "cx-guide-sum");
  sum.append(el("span", "cx-guide-kicker micro-label", "Guide"), el("span", "cx-guide-next", next ? "Next: " + next.title.toLowerCase() : "Every step has an answer — review and record the decision."));
  box.append(sum);
  const body = el("div", "cx-guide-body");
  if (p.narrative) {
    const what = el("p", "cx-guide-what");
    what.append(el("strong", "", "What this is deciding: "), document.createTextNode(p.narrative.split(/(?<=\.)\s/)[0]));
    body.append(what);
  }
  const ol = el("ol", "cx-guide-steps");
  steps.forEach((s) => {
    const li = el("li", "cx-guide-step" + (s.done ? " is-done" : "") + (s === next ? " is-next" : ""));
    const b = el("button", "cx-guide-step-btn");
    b.type = "button";
    b.append(el("span", "cx-guide-tick", s.done ? "✓" : ""), el("span", "cx-guide-title", s.title), el("span", "cx-guide-now", s.now));
    b.onclick = s.go;
    li.append(b);
    ol.append(li);
  });
  body.append(ol);
  if (crit.length) {
    const det = el("details", "cx-guide-open");
    det.append(el("summary", "", "What must still be answered, in plain words (" + crit.length + ")"));
    const ul = el("ul", "");
    const seen = new Set();
    crit.forEach((is) => {
      const plain = cxIssuePlain(is) || is.message;
      if (seen.has(plain)) return;
      seen.add(plain);
      ul.append(el("li", "", plain));
    });
    det.append(ul);
    body.append(det);
  }
  const words = el("details", "cx-guide-words");
  words.append(el("summary", "", "Words you'll see"));
  const dl = el("dl", "");
  CX_GLOSSARY.forEach(([t, d]) => dl.append(el("dt", "", t), el("dd", "", d)));
  words.append(dl);
  body.append(words);
  box.append(body);
  return box;
}

// The guide rides under the header on every render.
const cxPaintHeaderBase = cxPaintHeader;
cxPaintHeader = function (wb) {
  cxPaintHeaderBase(wb);
  const h = wb.querySelector(".cx-head");
  if (h && cx.view) h.append(cxGuideBlock());
};

// ---- the junction, as three plain questions ---------------------------------------
cxJunctionBlock = function (a, ro) {
  const j = a.junction;
  const blk = el("div", "cx-block cx-junction");
  blk.append(el("div", "cx-label micro-label", "Where the roof meets the wall"));
  const q = (n, text) => el("div", "cx-q", n + ". " + text);
  const sel = (map, value, label) => {
    const s = document.createElement("select");
    s.className = "cx-in";
    s.setAttribute("aria-label", label);
    Object.entries(map).forEach(([k, t]) => { const o = document.createElement("option"); o.value = k; o.textContent = typeof t === "string" ? t : t.label; s.append(o); });
    s.value = value;
    s.disabled = ro;
    return s;
  };
  // 1. the wall
  blk.append(q(1, "What is the wall?"));
  const r1 = el("div", "cx-row-edit");
  const wall = sel(CX_PLAIN_WALL, j.wallCondition.value, "Wall condition");
  const prov = sel(CX_PLAIN_PROV, j.wallCondition.provenance, "How do we know");
  const cav = inputEl("air gap in mm (blank = unknown)"); cav.className = "cx-in cx-in-num"; cav.setAttribute("aria-label", "Cavity width in mm"); cav.hidden = wall.value !== "cavity";
  wall.onchange = () => { if (wall.value === "unknown") prov.value = "unknown"; else if (prov.value === "unknown") prov.value = "user-assumption"; cav.hidden = wall.value !== "cavity"; };
  const setWall = pillLight("Save", () => {
    const op = { op: "SetWallCondition", value: wall.value, provenance: wall.value === "unknown" ? "unknown" : prov.value };
    if (wall.value === "cavity") { op.newComponentIds = { cavity: cxNewId("cmp") }; if (cav.value.trim()) { op.cavityWidth = Number(cav.value); op.cavityUnit = "mm"; } }
    cxCommand([op], { assembly: a.id });
  });
  setWall.disabled = ro;
  r1.append(wall, prov, cav, setWall);
  blk.append(r1);
  if (j.wallCondition.note) blk.append(el("p", "cx-hint", j.wallCondition.note));
  // 2 + 3. orientation and method
  blk.append(q(2, "Where does the roof meet it?"));
  const ori = sel(CX_PLAIN_ORIENT, j.orientation, "Junction orientation");
  blk.append(ori);
  blk.append(q(3, "How is the flashing held into the wall?"));
  const strat = document.createElement("select");
  strat.className = "cx-in"; strat.setAttribute("aria-label", "Flashing strategy"); strat.disabled = ro;
  const explain = el("div", "cx-explain");
  const showExplain = () => {
    const s = CX_PLAIN_STRAT[strat.value] || {};
    explain.replaceChildren();
    if (s.what) explain.append(el("p", "", s.what));
    if (s.good) { const p = el("p", ""); p.append(el("strong", "", "Why pick it: "), document.createTextNode(s.good)); explain.append(p); }
    if (s.watch) { const p = el("p", ""); p.append(el("strong", "", "Watch for: "), document.createTextNode(s.watch)); explain.append(p); }
    const needsSolid = /reglet/.test(strat.value), needsCavity = /through-wall/.test(strat.value);
    if ((needsSolid && wall.value !== "solid-bonded") || (needsCavity && wall.value !== "cavity")) explain.append(el("p", "cx-explain-warn", needsSolid ? "This one is for solid brick — set the wall first." : "This one is for cavity walls."));
  };
  const fill = () => {
    strat.innerHTML = "";
    (CX_STRATEGIES[ori.value] || []).forEach((s) => { const o = document.createElement("option"); o.value = s; o.textContent = (CX_PLAIN_STRAT[s] || {}).label || s; strat.append(o); });
    if ((CX_STRATEGIES[ori.value] || []).includes(j.strategy)) strat.value = j.strategy;
    showExplain();
  };
  fill();
  ori.onchange = fill;
  strat.onchange = showExplain;
  const apply = pillLight("Save", () => {
    const ids = {};
    if (ori.value === "sidewall") ids["sidewall-flashing"] = cxNewId("cmp");
    if (ori.value === "headwall") ids["apron"] = cxNewId("cmp");
    if (strat.value === "apron-through-wall-flashing") { ids["through-wall"] = cxNewId("cmp"); ids["weeps"] = cxNewId("cmp"); ids["end-dams"] = cxNewId("cmp"); }
    cxCommand([{ op: "SetJunctionStrategy", orientation: ori.value, strategy: strat.value, newComponentIds: ids }], { assembly: a.id });
  });
  apply.disabled = ro;
  const r3 = el("div", "cx-row-edit");
  r3.append(strat, apply);
  blk.append(r3, explain);
  return blk;
};

// Issues read in plain words first, the technical rule second.
if (typeof cxIssueRow === "function") {
  const cxIssueRowBase = cxIssueRow;
  cxIssueRow = function (is, a, compact) {
    const row = cxIssueRowBase(is, a, compact);
    const plain = cxIssuePlain(is);
    if (plain && row) row.append(el("div", "cx-issue-plain", "What to do: " + plain));
    return row;
  };
}

// ---- plain words for the lists and the header --------------------------------------
const CX_PLAIN_LIFECYCLE = { draft: "Draft", investigating: "Researching", alternatives: "Comparing options", "owner-selected": "Option chosen", archived: "Archived" };
const CX_PLAIN_FACT_PROV = { "user-assumption": "we believe", "verified-fact": "checked", "engineering-inference": "reasoned", "directly-applicable-guidance": "from a code or guide", "adapted-precedent": "from a similar case", unknown: "not known" };
const cxPlural = (n, one, many) => n + " " + (n === 1 ? one : many);

cxProblemRow = function (subject, row) {
  const a = el("a", "cx-row");
  a.href = cxHref(subject, row.id);
  a.append(el("span", "cx-row-title", row.title || row.id));
  const meta = [CX_PLAIN_LIFECYCLE[row.lifecycle] || row.lifecycle || "", row.alternatives ? cxPlural(row.alternatives, "option", "options") : "", row.inputs ? cxPlural(row.inputs, "document", "documents") : ""].filter(Boolean).join(" · ");
  a.append(el("span", "cx-row-meta", row.error ? "unreadable: " + row.error : meta));
  return a;
};

{
  const base = cxPaintHeader;
  cxPaintHeader = function (wb) {
    base(wb);
    const m = wb.querySelector(".cx-head-meta"), v = cx.view;
    if (m && v) {
      const n = (v.problem.alternatives || []).length;
      m.textContent = (CX_PLAIN_LIFECYCLE[v.problem.lifecycle] || v.problem.lifecycle) + (n ? " · " + cxPlural(n, "option", "options") : "") + (v.readOnly ? " · read-only (newer format)" : "");
      m.title = "Revision " + v.generation + " — every change is kept in History";
    }
    // facts: the source in plain words (the taxonomy stays in the tooltip)
    wb.querySelectorAll(".cx-fact .cx-chip").forEach((c) => { const k = c.textContent; if (CX_PLAIN_FACT_PROV[k]) { c.title = k; c.textContent = CX_PLAIN_FACT_PROV[k]; } });
  };
}
{
  const base = cxPaintResearch;
  cxPaintResearch = function (body) {
    base(body);
    if (body) body.querySelectorAll(".cx-fact .cx-chip").forEach((c) => { const k = c.textContent; if (CX_PLAIN_FACT_PROV[k]) { c.title = k; c.textContent = CX_PLAIN_FACT_PROV[k]; } });
  };
}

// ---- the model toolbar on a phone ---------------------------------------------------
// The views and Rotate/Move stay in reach; everything else folds under "More
// tools", so the model gets the screen. The hint speaks touch.
{
  const base = cxModelToolbar;
  cxModelToolbar = function () {
    const tb = base();
    const segs = [...tb.querySelectorAll(":scope > .cx-seg")];
    const keep = new Set(segs.filter((s) => s.querySelector("[data-drag]") || /Junction/.test(s.textContent)));
    const more = el("details", "cx-more-tools");
    more.open = cxMoreTools;
    more.ontoggle = () => { cxMoreTools = more.open; };
    more.append(el("summary", "", "More tools"));
    const inner = el("div", "cx-more-body");
    [...tb.children].forEach((n) => { if (!keep.has(n) && !n.classList.contains("cx-model-note")) inner.append(n); });
    more.append(inner);
    const hint = inner.querySelector(".cx-model-hint");
    if (hint) {
      if (cxPhone()) hint.textContent = "Drag to turn · pinch to zoom · two fingers to move · double-tap to look closer";
      tb.append(hint);
    }
    tb.append(more);
    return tb;
  };
}

// ---- new problem: pick the Home task by name ------------------------------------------
{
  const base = cxCreateForm;
  cxCreateForm = function (subject, session) {
    const form = base(subject, session);
    const task = form.querySelector('input[placeholder^="Linked shared Home task id"]');
    if (subject.kind === "home" && task) {
      const id = "cx-home-tasks";
      task.placeholder = "Linked Home task (start typing its name)";
      task.setAttribute("list", id);
      const dl = el("datalist", "");
      dl.id = id;
      form.append(dl);
      fetch("/api/tasks").then((r) => r.ok ? r.json() : null).then((d) => {
        const rows = ((d && d.rows) || []).filter((r) => r.container && /^home$/i.test(r.container.name) && /^home\//.test(r.id));
        rows.forEach((r) => { const o = document.createElement("option"); o.value = r.id; o.label = r.text; o.textContent = r.text; dl.append(o); });
      }).catch(() => {});
    }
    return form;
  };
}

// ---- labels on the model ----------------------------------------------------------------
// Plain names pinned to the parts that matter, following the view. One label
// per kind (the outer brick layer stands for the wall); fasteners are left to
// the Fixings overlay. Toggle: "Labels".
const CX_PART_LABELS = [
  ["masonry:outer-wythe", "Brick wall"],
  ["flashing:counter", "Counterflashing"],
  ["flashing:base-apron", "Apron flashing"],
  ["roof:corrugated-metal", "Metal roof panel"],
  ["roof:profile-closure", "Foam closure"],
  ["seal:", "Sealant"],
  ["control:water-underlayment", "Underlayment"],
  ["thermal:insulation", "Insulation"],
  ["control:air-vapour", "Air / vapour membrane"],
  ["support:battens", "Battens"],
  ["structure:finish-deck", "Plywood deck"],
  ["structure:exposed-rafters", "Steel rafters"],
];
// what gets a name on the model: the fixed parts by role, custom parts by their own name
function cxLabelTargets(a) {
  const out = [];
  for (const [role, name] of CX_PART_LABELS) {
    const c = (a.components || []).find((x) => (x.role || "").startsWith(role) && x.applicability !== "inapplicable");
    if (c) out.push([c.id, name]);
  }
  (a.components || []).forEach((c) => {
    if (c.applicability === "inapplicable") return;
    if (c.type === "profiled-flashing" || c.type === "steel-beam") out.push([c.id, c.name]);
  });
  // rafters say what they're made of
  out.forEach((t) => { const c = (a.components || []).find((x) => x.id === t[0]); if (c && c.type === "rafter-array") t[1] = c.appearance === "steel-channel" ? "Steel rafters" : "Timber rafters"; });
  return out;
}
let cxLabelsOn = (() => { try { return localStorage.getItem("cx.labels") !== "0"; } catch (e) { return true; } })();
// Callouts: a dot on each part, a leader line, and the name in a column at
// the nearer edge of the view, spaced so names never overlap.
function cxLabelsUpdate(host) {
  if (!host || !cx.renderer || !cx.renderer.labelAnchors) return;
  let layer = host.querySelector(".cx-labels");
  if (!layer) {
    layer = el("div", "cx-labels"); layer.setAttribute("aria-hidden", "true");
    layer.innerHTML = '<svg class="cx-label-lines" width="100%" height="100%"></svg>';
    host.append(layer);
  }
  layer.hidden = !cxLabelsOn;
  if (!cxLabelsOn) return;
  const svg = layer.querySelector("svg");
  const a = cxAsm();
  if (!a) { layer.querySelectorAll(".cx-label-tag").forEach((t) => t.remove()); svg.replaceChildren(); return; }
  const names = new Map(cxLabelTargets(a));
  // while the view moves, carry the last anchors along; once it settles,
  // find anchors again on what is actually visible
  clearTimeout(cxLabelsTimer);
  cxLabelsTimer = setTimeout(() => {
    if (!cx.renderer || !host.isConnected) return;
    cxLabelAnchors = cx.renderer.labelAnchors([...names.keys()]);
    cxLabelsDraw(host);
  }, 140);
  cxLabelsDraw(host);
}
let cxLabelsTimer = 0, cxLabelAnchors = {};
function cxLabelsDraw(host) {
  const layer = host.querySelector(".cx-labels"), svg = layer && layer.querySelector("svg"), a = cxAsm();
  if (!layer || !a || !cxLabelsOn) return;
  const w = host.clientWidth, h = host.clientHeight, gap = cxPhone() ? 26 : 24;
  const shown = [];
  for (const [id, name] of cxLabelTargets(a)) {
    const wp = cxLabelAnchors[id];
    const p = wp && cx.renderer.toScreen(wp);
    if (p) shown.push({ id, name, x: p.x, y: p.y, side: p.x < w / 2 ? "l" : "r" });
  }
  // stack each column top to bottom in anchor order
  for (const side of ["l", "r"]) {
    const col = shown.filter((s) => s.side === side).sort((m, n) => m.y - n.y);
    let next = 12;
    for (const s of col) { s.ty = Math.max(s.y, next); next = s.ty + gap; }
    const over = next - gap - (h - 12);
    if (over > 0) for (const s of col) s.ty = Math.max(12, s.ty - over);
  }
  const keep = new Set(), NS = "http://www.w3.org/2000/svg";
  svg.replaceChildren();
  for (const s of shown) {
    let tag = layer.querySelector(`.cx-label-tag[data-part="${s.id}"]`);
    if (!tag) { tag = el("span", "cx-label-tag", s.name); tag.dataset.part = s.id; layer.append(tag); }
    tag.hidden = false;
    tag.classList.toggle("is-selected", cx.selection === s.id);
    tag.classList.toggle("is-right", s.side === "r");
    const tx = s.side === "l" ? 8 : w - 8;
    tag.style.transform = `translate(${Math.round(tx)}px, ${Math.round(s.ty)}px)`;
    const ex = s.side === "l" ? tx + tag.offsetWidth : tx - tag.offsetWidth;
    const line = document.createElementNS(NS, "polyline");
    line.setAttribute("points", `${ex},${s.ty} ${ex + (s.side === "l" ? 10 : -10)},${s.ty} ${s.x},${s.y}`);
    const dot = document.createElementNS(NS, "circle");
    dot.setAttribute("cx", s.x); dot.setAttribute("cy", s.y); dot.setAttribute("r", 3);
    if (cx.selection === s.id) { line.classList.add("is-selected"); dot.classList.add("is-selected"); }
    svg.append(line, dot);
    keep.add(s.id);
  }
  layer.querySelectorAll(".cx-label-tag").forEach((t) => { if (!keep.has(t.dataset.part)) t.hidden = true; });
}
{
  const base = cxModelToolbar;
  cxModelToolbar = function () {
    const tb = base();
    const b = cxToolBtn("Labels", "Show the names of the parts on the model", () => {
      cxLabelsOn = !cxLabelsOn;
      try { localStorage.setItem("cx.labels", cxLabelsOn ? "1" : "0"); } catch (e) {}
      b.classList.toggle("on", cxLabelsOn);
      const host = tb.parentElement && tb.parentElement.querySelector(".cx-canvas-host");
      if (host) cxLabelsUpdate(host);
    });
    b.classList.toggle("on", cxLabelsOn);
    const drag = tb.querySelector(".cx-seg [data-drag]");
    if (drag) drag.parentElement.append(b); else tb.prepend(b);
    return tb;
  };
}
