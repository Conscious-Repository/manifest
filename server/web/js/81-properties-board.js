// ---- PROPERTIES pane views: the all-properties table + Outstanding ----
// Rev 3: the whole board is three columns — address · status · open todos.
// Outstanding groups everything owed to you by container (the chasing surface).
let propComposerOpen = false;

const PROPERTY_STATUSES = ["negotiating", "under_contract", "pre_development", "construction", "completed", "leased", "listed", "sold"];
const PROPERTY_KINDS = ["rehab", "new-construction", "mixed", "hold"];

// Portfolio — rebuilt on the fundraising shape (PORTFOLIO-LIST handoff,
// 2026-08-19): search + cut chips over ONE flat four-column table
// (PROPERTY · ROCK · OPEN · SPENT), a right inspector that edits save-as-
// you-go, `—` for every missing value, every count derived. The chips are
// the grouping — no phase sections, no progress bars, no status column.
let pfQuery = "";
let pfCut = "open"; // open | all | attention | construction | pre-dev | pipeline | stabilized
let pfSel = null;   // selected property slug (inspector)

// the 8 statuses collapse to 5 phases; settled phases leave OPEN + attention
const PF_PHASE = {
  construction: "construction", pre_development: "pre-dev",
  under_contract: "pipeline", negotiating: "pipeline",
  completed: "stabilized", leased: "stabilized", listed: "stabilized",
  sold: "closed",
};
const PF_SETTLED = ["stabilized", "closed"];
const pfToday = () => new Date().toISOString().slice(0, 10);

// pfFacts — the row's derived picture (never stored; every figure computed
// from the record, RE spec §2).
function pfFacts(p) {
  const stages = p.work || [];
  const cur = stages.find((st) => !st.checked) || null;
  const done = stages.filter((st) => st.checked).length;
  const m = projMoney(p);
  return {
    cur,
    doneBy: cur ? (cur.doneBy || "") : "",
    open: openTodoCount(p),
    pctDone: stages.length ? (done / stages.length) * 100 : 0,
    hasStages: stages.length > 0,
    plan: m.budget || 0,
    paid: m.paid || 0,
    phase: PF_PHASE[p.status] || "pipeline",
  };
}

// pfAttention — three derived rules: over plan, past a done-by, or an
// unfinished active property with nothing queued. Settled phases are
// excluded or every leased hold would light the filter forever.
function pfAttention(p) {
  const f = pfFacts(p);
  const over = f.plan > 0 && f.paid > f.plan;
  const late = !!f.doneBy && f.doneBy < pfToday();
  // stalled needs a WORK PLAN to be stalled against — a pipeline parcel with
  // no rocks yet isn't "nothing queued", it's "not started" (without this,
  // every under-contract lot lit the filter — the noise it exists to cut)
  const stalled = f.hasStages && f.open === 0 && f.pctDone < 100 &&
    p.status !== "negotiating" && !PF_SETTLED.includes(f.phase);
  return over || late || stalled;
}

function pfVisible(p) {
  const f = pfFacts(p);
  if (pfCut === "open" && PF_SETTLED.includes(f.phase)) return false;
  if (pfCut === "attention" && !pfAttention(p)) return false;
  if (["construction", "pre-dev", "pipeline", "stabilized"].includes(pfCut) && f.phase !== pfCut) return false;
  const q = pfQuery.trim().toLowerCase();
  if (!q) return true;
  return [p.address, p.short, p.entity, p.deal, p.status, p.kind, f.cur ? f.cur.text : ""]
    .join(" ").toLowerCase().includes(q);
}

// pfPatch — one store (the handoff's rule): every inspector edit writes
// through the server, the response IS the fresh record, the list re-renders.
async function pfPatch(p, key, value) {
  try {
    const fresh = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key, value });
    pfSwap(fresh);
  } catch (e) { showToast("Couldn't save " + key + " — " + (e.message || "")); }
}

async function pfWorkOp(p, body) {
  try {
    const fresh = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work", body);
    pfSwap(fresh);
  } catch (e) { showToast("Couldn't save — " + (e.message || "")); }
}

function pfSwap(fresh) {
  if (fresh && fresh.slug) {
    const i = propertyCache.findIndex((x) => x.slug === fresh.slug);
    if (i >= 0) { fresh.__source = propertyCache[i].__source; propertyCache[i] = fresh; }
  }
  renderPortfolio();
}

function renderPortfolio() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const wrap = el("div", "aion-backlog fr-shell");
  const main = el("div", "aion-list fr-main");
  const inspector = el("aside", "aion-inspector fr-inspector");
  wrap.append(main, inspector);
  host.append(wrap);

  // toolbar: search first, then the cut chips (the grouping)
  const bar = el("div", "fr-toolbar");
  const search = el("input", "pp-in fr-search");
  search.type = "search";
  search.placeholder = "Search address, entity, deal, stage…";
  search.value = pfQuery;
  search.oninput = () => { pfQuery = search.value; renderPortfolio(); };
  bar.append(search);
  const attn = activePortfolio().filter(pfAttention).length;
  const cuts = [["open", "OPEN"], ["all", "ALL"]];
  if (attn) cuts.push(["attention", "ATTENTION " + attn]);
  cuts.push(["construction", "CONSTRUCTION"], ["pre-dev", "PRE-DEV"], ["pipeline", "PIPELINE"], ["stabilized", "HELD"]);
  cuts.forEach(([key, label]) => {
    const b = el("button", "filter-chip" + (pfCut === key ? " on" : ""), label);
    b.onclick = () => { pfCut = key; renderPortfolio(); };
    bar.append(b);
  });
  main.append(bar);
  main.append(propertyComposer());

  // the table — four columns, fr-stack cells, no bars, no sections
  const all = activePortfolio();
  const rows = all.filter(pfVisible);
  const table = el("div", "fr-table");
  const head = el("div", "fr-row fr-head pf4-grid");
  ["PROPERTY", "ROCK", "OPEN", "SPENT"].forEach((h, i) =>
    head.append(el("span", "micro-label" + (i >= 2 ? " pf4-r" : ""), h)));
  table.append(head);
  if (!rows.length) table.append(emptyRow("No properties match."));
  rows.forEach((p) => table.append(pfRow(p)));
  main.append(table);

  // foot: N of M + the research tail — every count derived
  const tracked = propertyCache.filter((p) => !p.hidden && !(p.control === "owned" || p.entity));
  const foot = el("button", "pf-tracked-foot",
    rows.length + " of " + all.length +
    (tracked.length ? " · " + tracked.length + " tracked (research) — see Map" : ""));
  foot.onclick = () => { location.hash = "#/properties/map"; };
  main.append(foot);

  // Deals keep their quiet fold (the deal pages open from here)
  if (dealCache.length) {
    const dealsSec = el("div", "pf-deals");
    const dh = el("div", "aion-sec-label");
    dh.append(el("span", "aion-sec-title", "◈ Deals"), el("span", "aion-sec-count", String(dealCache.length)));
    dealsSec.append(dh);
    dealCache.forEach((d) => {
      const members = propertyCache.filter((p) => (p.deal || "") === (d.name || d.slug) || (p.deal || "") === d.slug);
      const row = el("div", "prop-row pf-deal-row");
      row.onclick = () => { location.hash = "#/properties/deal/" + encodeURIComponent(d.slug); };
      row.append(el("span", "prop-addr", d.name || d.slug));
      row.append(el("span", "pf-deal-meta", members.length + " propert" + (members.length === 1 ? "y" : "ies")));
      dealsSec.append(row);
    });
    main.append(dealsSec);
  }

  // inspector — the fundraising contract: select a row, edits save as you go
  const selected = all.find((p) => p.slug === pfSel);
  if (window.mf && window.mf.phone()) {
    if (selected) {
      window.mfSheet.open((body) => renderPortfolioInspector(body, selected), {
        key: "portfolio",
        onClose: () => { if (pfSel) { pfSel = null; renderPortfolio(); } },
        reopen: () => { if (propMode === "portfolio") renderPortfolio(); },
      });
    } else {
      window.mfSheet.closeIf("portfolio");
    }
  } else if (selected) {
    renderPortfolioInspector(inspector, selected);
  } else {
    inspector.append(el("div", "aion-insp-empty", "select a property — edits save as you go"));
  }
}

function pfRow(p) {
  const f = pfFacts(p);
  const row = el("div", "fr-row pf4-grid" + (pfSel === p.slug ? " sel" : ""));
  // PROPERTY: address over entity · status
  const c1 = el("span", "fr-stack");
  c1.append(el("span", "pf4-addr", p.short || p.address || p.slug));
  c1.append(el("span", "fr-sub",
    (p.entity || "—") + " · " + ((p.status || "").replace(/_/g, " ") || "—")));
  row.append(c1);
  // ROCK: current rock over its schedule state
  const c2 = el("span", "fr-stack");
  c2.append(el("span", "pf4-rock", f.cur ? f.cur.text : (f.hasStages ? "done" : "—")));
  let sub = "—";
  if (f.cur && f.doneBy && f.doneBy < pfToday()) sub = "● late — was due " + f.doneBy.slice(5);
  else if (f.cur && f.open === 0 && !PF_SETTLED.includes(f.phase) && p.status !== "negotiating") sub = "● no open task";
  else if (f.cur && f.doneBy) sub = "by " + f.doneBy.slice(5);
  c2.append(el("span", "fr-sub" + (sub.startsWith("●") ? " pf4-ink" : ""), sub));
  row.append(c2);
  // OPEN
  row.append(el("span", "pf4-num", f.open ? String(f.open) : "—"));
  // SPENT: paid over "of plan"
  const c4 = el("span", "fr-stack pf4-money");
  c4.append(el("span", "pf4-num pf-spent" + (f.plan > 0 && f.paid > f.plan ? " over" : ""),
    f.paid ? fmtMoneyShort(f.paid) : "—"));
  c4.append(el("span", "fr-sub", f.plan ? "of " + fmtMoneyShort(f.plan) : "no plan"));
  row.append(c4);
  row.onclick = () => {
    pfSel = pfSel === p.slug ? null : p.slug;
    renderPortfolio();
  };
  return row;
}

// renderPortfolioInspector — field for field on the fundraising idiom:
// aion-insp-field rows, patch on blur/change through /field (one store).
function renderPortfolioInspector(host, p) {
  host.innerHTML = "";
  const f = pfFacts(p);
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Property"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { pfSel = null; renderPortfolio(); };
  head.append(x);
  host.append(head);

  const field = (label, node) => {
    const fl = el("div", "aion-insp-field fr-insp-field");
    fl.append(el("span", "aion-insp-flabel", label), node);
    host.append(fl);
  };
  const textField = (label, value, onCommit) => {
    const inp = inputEl("");
    inp.className = "pp-in fr-in";
    inp.value = value || "";
    let old = inp.value;
    inp.onblur = () => { if (inp.value.trim() !== old && inp.value.trim()) { old = inp.value.trim(); onCommit(old); } };
    inp.addEventListener("keydown", (ev) => { if (ev.key === "Enter") inp.blur(); });
    field(label, inp);
  };
  const selectField = (label, options, current, onCommit) => {
    const sel = document.createElement("select");
    sel.className = "pp-in fr-in";
    options.forEach(([v, l]) => {
      const o = document.createElement("option");
      o.value = v; o.textContent = l;
      if (v === current) o.selected = true; // value-before-options fallback guard
      sel.append(o);
    });
    sel.onchange = () => onCommit(sel.value);
    field(label, sel);
  };

  textField("address", p.address, (v) => pfPatch(p, "address", v));
  selectField("status", PROPERTY_STATUSES.map((s) => [s, s.replace(/_/g, " ")]), p.status,
    (v) => pfPatch(p, "status", v));
  // entity: hard link to a record — options load from the registry
  const entSel = document.createElement("select");
  entSel.className = "pp-in fr-in";
  const eopt = (v, l, on) => { const o = document.createElement("option"); o.value = v; o.textContent = l; if (on) o.selected = true; entSel.append(o); };
  eopt("", "—", !p.entity);
  ensureEntities().then((es) => {
    (es.entities || []).forEach((x) => eopt(x.name, x.name, x.name === p.entity));
  });
  entSel.onchange = () => pfPatch(p, "entity", entSel.value);
  field("entity", entSel);
  selectField("kind", PROPERTY_KINDS.map((k) => [k, k]), p.kind, (v) => pfPatch(p, "kind", v));
  // control drives the money model: owned = the purchase happened, so the
  // acquisition plan reads as spent before closing rows land in the ledger
  selectField("control", [["owned", "owned"], ["tracked", "tracked"]], p.control,
    (v) => pfPatch(p, "control", v));
  selectField("deal", [["", "—"]].concat(dealCache.map((d) => [d.slug, d.name || d.slug])),
    p.deal || "", (v) => pfPatch(p, "deal", v));
  if (f.cur) {
    textField("current rock", f.cur.text, (v) => pfWorkOp(p, { op: "edit", id: f.cur.id, text: v }));
    const db = document.createElement("input");
    db.type = "date";
    db.className = "pp-in fr-in";
    db.value = f.doneBy || "";
    db.onchange = () => pfWorkOp(p, { op: "set-field", id: f.cur.id, field: "done-by", value: db.value });
    field("done by", db);
  }
  // plan is DERIVED (owner call 2026-08-19): read-only spent line, click
  // opens the budget editor behind the figure
  const spent = el("button", "pf4-spentline",
    "spent " + (f.paid ? fmtMoneyShort(f.paid) : "—") +
    (f.plan ? " · of " + fmtMoneyShort(f.plan) : " · no plan") + " → budget");
  spent.onclick = () => {
    propUWOpen = true;
    location.hash = "#/properties/" + encodeURIComponent(p.slug);
  };
  host.append(spent);
  const open = el("button", "pf4-openpage", "open the property page →");
  open.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
  host.append(open);
}

function fmtMoneyShort(v) {
  if (v >= 1e6) return "$" + (v / 1e6).toFixed(1) + "M";
  if (v >= 1e3) return "$" + Math.round(v / 1e3) + "k";
  return "$" + Math.round(v);
}

// GOALS — the merged view (owner call 2026-08-18): org-level 90-day rocks
// from the goals `## Real Estate` area on top, then every active property's
// own rock tree beneath. Same grammar at two altitudes, one destination.
function renderREGoals() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  renderREOrgRocks(host);
  renderREPropertyRocks(host);
}

// Org rocks — goals-ladder work, rendered rock → stage → task. A task
// carrying a property/deal tag shows it in accent and also lives there;
// the footer states the rule.
function renderREOrgRocks(host) {
  const rocks = reOrgRocks();
  const head = el("div", "aion-sec-label");
  head.append(el("span", "aion-sec-title", "● Org rocks"), el("span", "aion-sec-count", String(rocks.length)));
  host.append(head);
  if (!rocks.length) {
    host.append(el("div", "pp-empty", "No org rocks in the Real Estate area yet — add one in GOALS."));
    return;
  }
  rocks.forEach((g) => {
    const wrap = el("div", "re-rock");
    const line = el("div", "re-rock-line");
    line.append(el("span", "re-rock-dot"));
    const name = el("span", "re-rock-name", g.text);
    name.onclick = () => { location.hash = "#/goals/" + encodeURIComponent(g.id); };
    line.append(name);
    wrap.append(line);
    // stage trail with tethered backlog tasks nested (from the RE backlog +
    // the unified todos rows that carry this rock id)
    // a task tethered to this rock OR any of its stages nests here (stages are
    // pickable tether targets now, so match the whole subtree's ids)
    const rockIds = new Set([g.id, ...(g.children || []).map((c) => c.id)]);
    const tethered = reItems().filter((it) => it.kind === "task" && it.status !== "done" && rockIds.has(it.rock));
    // milestones are not sequential (owner call 2026-08-12) — no current marker
    (g.children || []).forEach((st) => {
      const sl = el("div", "re-stage" + (st.checked ? " done" : ""));
      sl.append(el("span", "re-stage-glyph", st.checked ? "✓" : "○"));
      sl.append(el("span", "", st.text));
      wrap.append(sl);
    });
    tethered.forEach((it) => {
      const t = el("div", "re-task");
      t.append(el("span", "re-task-glyph", "○"), el("span", "", it.text));
      if (it.owner) t.append(el("span", "re-task-owner", "@" + it.owner));
      wrap.append(t);
    });
    host.append(wrap);
  });
  host.append(el("div", "re-foot-note", "an untagged task is org-level and lives here — tag a property or deal to file it there too"));
}

// Property rocks — each active property's own rock tree, compact: rock line
// (done glyph · name · done-by, ink when late) + open task count; milestones
// read as sub-points. Click-through to the property page for the full lanes.
function renderREPropertyRocks(host) {
  const today = new Date().toISOString().slice(0, 10);
  const withRocks = activePortfolio().filter((p) => (p.work || []).length);
  const head = el("div", "aion-sec-label re-proprocks-label");
  head.append(el("span", "aion-sec-title", "◈ Property rocks"), el("span", "aion-sec-count", String(withRocks.length)));
  host.append(head);
  withRocks.forEach((p) => {
    const wrap = el("div", "re-rock re-proprock");
    const line = el("div", "re-rock-line");
    line.append(el("span", "re-rock-dot"));
    const name = el("span", "re-rock-name", p.short || p.address || p.slug);
    name.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    line.append(name);
    const openN = openTodoCount(p);
    if (openN) line.append(el("span", "aion-sec-count", openN + " open"));
    wrap.append(line);
    (p.work || []).forEach((st) => {
      const sl = el("div", "re-stage" + (st.checked ? " done" : ""));
      sl.append(el("span", "re-stage-glyph", st.checked ? "✓" : "○"));
      sl.append(el("span", "", st.text));
      const when = st.checked ? (st.done || "") : (st.doneBy || "");
      if (when) {
        sl.append(el("span", "re-stage-doneby" + (!st.checked && st.doneBy && st.doneBy < today ? " late" : ""),
          st.checked ? when : "by " + when));
      }
      const openHere = countOpenNodes(st.tasks);
      if (openHere) sl.append(el("span", "re-stage-open", openHere + ""));
      wrap.append(sl);
    });
    host.append(wrap);
  });
}

function countOpenNodes(nodes) {
  let n = 0;
  (nodes || []).forEach((t) => {
    if (!t.checked && !t.milestone && !t.decision) n++;
    n += countOpenNodes(t.children);
  });
  return n;
}

// propOutstandingGroups — PROPERTY containers only (owner call 2026-08-09):
// aion items owed to you live on the AION surface, never here.
function propOutstandingGroups() {
  return ((propTodosMeta && propTodosMeta.outstanding) || [])
    .filter((g) => g.container.kind === "property");
}

// (renderOutstanding deleted — Outstanding folded into the Backlog view as
//  non-"you" owner groups. propOutstandingGroups() stays; the Backlog reads it.)

// mineOwner mirrors the substrate's rule: empty · "me" · my initials = me.
function mineOwner(owner) {
  if (!owner || owner === "me") return true;
  const me = ((propTodosMeta && propTodosMeta.me) || "BA").toUpperCase();
  return owner.toUpperCase().split("/").map((s) => s.trim()).includes(me);
}

// assigneeName resolves an owner value against the registries (contractor
// slug → name; aion initials → name); anything that means ME reads "you".
// reAssignee — the roster entry an owner key belongs to, resolving ALIASES: a
// partner who is also a vendor carries [contractor:: <slug>] on her people.md
// row, so both keys land on one entry (owner call 2026-08-19).
function reAssignee(owner) {
  const a = (propTodosMeta && propTodosMeta.assignees) || {};
  const key = String(owner || "").toLowerCase();
  return (a.realestate || []).find((e) =>
    String(e.slug || "").toLowerCase() === key ||
    String(e.name || "").toLowerCase() === key ||
    (e.aliases || []).some((x) => String(x).toLowerCase() === key));
}

// reOwnerKey — the CANONICAL key for an owner: an alias resolves to the roster
// entry's own slug, so work owned as "olga-sobkiv" and work owned as "OS"
// group together instead of reading as two people.
function reOwnerKey(owner) {
  const e = reAssignee(owner);
  return e ? e.slug : owner;
}

function assigneeName(owner) {
  if (mineOwner(owner)) return "you";
  const c = reAssignee(owner);
  if (c) return c.name + (c.trade ? " (" + c.trade + ")" : "");
  const p = (a.aion || []).find((e) => e.initials === owner);
  if (p) return p.name;
  return owner;
}

// statusChip / statusSelect — the one in-place edit the table keeps.
function statusSelect(p, onSaved) {
  const sel = selectEl(PROPERTY_STATUSES);
  // .pp-in re-declares --sans later in the sheet than the mono-label group, so
  // it silently won and every status chip in the app rendered sans. The status
  // control is a chip, not a form field — it carries its own box.
  sel.className = "prop-status-sel";
  sel.value = p.status || "negotiating"; // options are mounted above — the value sticks
  sel.onchange = async () => {
    try { onSaved(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "status", value: sel.value })); }
    catch (err) { showToast("Couldn't update status"); }
  };
  return sel;
}

// propertyComposer: address · entity · kind · template — creation stays.
function propertyComposer() {
  const wrap = el("div", "prop-compose-wrap");
  const ghost = el("button", "o-ghost property-add", "＋ property");
  const openForm = () => {
    const form = el("div", "prop-composer");
    const addr = inputEl("address…"); addr.classList.add("pc-addr");
    const entity = inputEl("entity (optional)…");
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
          address: addr.value, entity: entity.value.trim(), kind: kind.value,
          template: tpl.value === "no template" ? "" : tpl.value,
          deal: dealSel.value === "unattached" ? "" : dealSel.value,
        });
        propComposerOpen = false;
        renderProperties();
      } catch (e) { showToast("Couldn't create property"); create.disabled = false; }
    };
    const cancel = el("button", "pill light", "✕");
    cancel.onclick = () => { propComposerOpen = false; form.replaceWith(ghost); };
    form.append(addr, entity, kind, tpl, dealSel, create, cancel);
    ghost.replaceWith(form);
    addr.focus();
  };
  ghost.onclick = openForm;
  wrap.append(ghost);
  if (propComposerOpen) setTimeout(openForm, 0);
  return wrap;
}
