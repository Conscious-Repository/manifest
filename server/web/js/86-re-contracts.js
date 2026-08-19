// ---- CONTRACTS + CONTRACTOR surfaces (overhaul pass 2) ----
// The contractor table mirrors the AION fundraising surface (owner amendment
// 2026-08-18): table + aside inspector, row select → inspector, edits save
// as you go, mobile sheet fallback. The contractor HISTORY page follows the
// contact-page conventions — everything on it is DERIVED (contracts by
// status, properties worked, committed/drawn/remaining, open tree tasks
// owned); nothing stored. The CONTRACT page is the committed-money record:
// total · allocations · Σ drawn · remaining, with the document one click away.

let reContractsCache = null; // /api/realestate/contracts → {contracts:[…]}
let reCtrSel = null;         // selected contractor slug (table inspector)

async function loadReContracts() {
  try { reContractsCache = await (await fetch("/api/realestate/contracts")).json(); }
  catch (e) { reContractsCache = { contracts: [] }; }
}

function reContracts() { return (reContractsCache && reContractsCache.contracts) || []; }

// reScopeVocab — the scope vocabulary is DRAWN from the rock templates +
// milestones in use + every scope already on a contractor (§3.7): a join,
// not a guess.
function reScopeVocab() {
  const vocab = new Set();
  (templateCache || []).forEach((t) => (t.stages || []).forEach((st) => vocab.add(st.text.toLowerCase())));
  (propertyCache || []).forEach((p) => (p.work || []).forEach((st) =>
    (st.tasks || []).forEach(function walk(n) {
      if (n.milestone) vocab.add((n.text || "").toLowerCase());
      (n.children || []).forEach(walk);
    })));
  (((entitiesCache || {}).contractors) || []).forEach((c) => (c.scopes || []).forEach((s) => vocab.add(s)));
  return [...vocab].filter(Boolean).sort();
}

// ---- the INTAKE lane (overhaul §5): one affordance on the RE overview ----
// Drop (or browse) a bid/contract/estimate → CAS + text extract + the
// re-intake ritual → an adjustable re-contract proposal in FEED. "enter
// manually" is the same outcome minus the file (the contract form).

function reIntakeLane() {
  const lane = el("div", "re-intake-lane");
  const drop = el("div", "re-intake-drop");
  drop.append(el("span", "re-intake-glyph", "⇪"));
  drop.append(el("span", "", "drop a bid / contract here — or "));
  const browse = el("button", "pp3-link", "browse");
  const manual = el("button", "pp3-link", "enter manually");
  drop.append(browse, el("span", "", " · "), manual);
  const input = document.createElement("input");
  input.type = "file";
  input.accept = ".pdf,.docx,image/*";
  input.hidden = true;
  drop.append(input);
  browse.onclick = () => input.click();
  manual.onclick = () => { location.hash = "#/properties/contract-new"; };
  input.onchange = () => { if (input.files && input.files[0]) reIntakeUpload(input.files[0]); };
  drop.ondragover = (e) => { e.preventDefault(); drop.classList.add("over"); };
  drop.ondragleave = () => drop.classList.remove("over");
  drop.ondrop = (e) => {
    e.preventDefault();
    drop.classList.remove("over");
    if (e.dataTransfer.files && e.dataTransfer.files[0]) reIntakeUpload(e.dataTransfer.files[0]);
  };
  lane.append(drop);
  return lane;
}

async function reIntakeUpload(file) {
  showToast("Uploading " + file.name + "…");
  try {
    const r = await fetch("/api/realestate/intake?name=" + encodeURIComponent(file.name), {
      method: "POST", body: file,
    });
    if (!r.ok) throw new Error(await r.text());
    showToast("Parsing " + file.name + " — the proposal will land in FEED");
  } catch (e) {
    showToast("Intake failed — " + String(e.message || e).slice(0, 120));
  }
}

// ---- the manual contract form (#/properties/contract-new) — same writes,
// no file (overhaul §5). Creates through POST /api/realestate/contracts. ----

function renderContractForm() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const page = el("div", "re-contract-page");
  host.append(page);
  page.append(el("h2", "pp3-title", "New contract"));
  page.append(el("div", "aion-section-note",
    "the manual intake path — a bid or signed contract entered by hand; the record lands in system/realestate/contracts/"));

  const form = el("div", "pp3-lform re-cform");
  const grid = el("div", "pp3-lform-grid");
  const labeled = (label, node) => {
    const wrap = el("label", "pp3-lform-field");
    wrap.append(el("span", "pp3-lform-label", label), node);
    return wrap;
  };
  const name = inputEl("Twisted Brick — masonry, 751 + 753"); name.className = "pp-in";
  // contractor: records only, quiet create-new
  let contractorSlug = "";
  const ctr = recordAutocomplete("contractor", "contractor…", (rec) => { contractorSlug = rec.slug; });
  const status = selectEl(["proposed", "accepted"]);
  const total = moneyInput("$", 0);
  const date = inputEl("YYYY-MM-DD"); date.className = "pp-in";
  date.value = new Date().toISOString().slice(0, 10);
  const expires = inputEl("YYYY-MM-DD (optional)"); expires.className = "pp-in";
  grid.append(labeled("name", name), labeled("contractor", ctr.el), labeled("status", status),
    labeled("total", total), labeled("date", date), labeled("expires", expires));
  form.append(grid);

  // allocations builder: property → node → amount, N rows
  const allocHead = el("div", "pp3-sec-head");
  allocHead.append(el("span", "pp3-sec-title", "ALLOCATIONS"));
  form.append(allocHead);
  const allocRows = [];
  const allocBox = el("div", "re-cform-allocs");
  const addAllocRow = () => {
    const row = { prop: "", node: "", amount: null };
    const line = el("div", "re-cform-alloc");
    const propSel = selectEl([]);
    const popt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; propSel.append(o); };
    popt("", "property…");
    activePortfolio().forEach((p) => popt(p.slug, p.short || p.slug));
    const nodeSel = selectEl([]);
    const fillNodes = () => {
      nodeSel.innerHTML = "";
      const nopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; nodeSel.append(o); };
      nopt("", "node…");
      const p = propertyCache.find((x) => x.slug === propSel.value);
      ((p && p.work) || []).forEach((st) => {
        nopt(st.id, st.text);
        (st.tasks || []).forEach(function walk(n, prefix) {
          const pre = typeof prefix === "string" ? prefix : "· ";
          nopt(n.id, pre + n.text);
          (n.children || []).forEach((c) => walk(c, pre + "· "));
        });
      });
    };
    fillNodes();
    propSel.onchange = () => { row.prop = propSel.value; fillNodes(); };
    nodeSel.onchange = () => { row.node = nodeSel.value; };
    const amt = moneyInput("$", 0);
    amt.oninput = () => { row.amount = parseFloat(amt.value.replace(/[$,]/g, "")) || 0; };
    line.append(propSel, nodeSel, amt);
    allocBox.append(line);
    allocRows.push(row);
  };
  addAllocRow();
  form.append(allocBox);
  form.append(ghostInput("＋ allocation (multi-property contracts split here)", "aion-add", () => addAllocRow()));

  const actions = el("div", "pp3-uw-actions");
  const cancel = el("button", "pp3-uw-cancel", "cancel");
  cancel.onclick = () => { location.hash = "#/properties"; };
  const save = el("button", "pp3-compose-go", "create ↵");
  save.onclick = async () => {
    const t = parseFloat(total.value.replace(/[$,]/g, "")) || 0;
    const allocations = allocRows
      .filter((r) => r.prop && r.node && r.amount > 0)
      .map((r) => r.prop + " | " + r.node + " | " + r.amount);
    if (!contractorSlug) { showToast("Pick (or create) the contractor"); return; }
    if (!allocations.length) { showToast("At least one allocation (property · node · amount)"); return; }
    try {
      const res = await postJSONOk("/api/realestate/contracts", {
        name: name.value.trim() || null, contractor: contractorSlug, status: status.value,
        total: t, date: date.value.trim(), expires: expires.value.trim(), allocations,
      });
      showToast("Contract created");
      location.hash = "#/properties/contract/" + encodeURIComponent(res.slug);
    } catch (e) { showToast("Couldn't create — " + String(e.message || e).slice(0, 140)); }
  };
  actions.append(cancel, save);
  form.append(actions);
  page.append(form);
}

// ---- CONTRACTORS — a top-level RE surface (owner call 2026-08-18) ----
// Graduated out of the Settings registries: the counterparty ledger reads as
// its own object, not a row in a 176px sub-pane. The shape is the AION
// fundraising surface (94-aion-fundraising.js) pointed at a different record —
// search + state chips over a four-column table, an aside inspector where
// every field edits, mobile sheet fallback. One link leaves the row: the name
// opens the derived HISTORY page below.

let reCtrQuery = "";
let reCtrFilter = "all"; // all | working | bidding | quiet

// reContractorRollup — the per-contractor join over the contract records:
// counts, committed/drawn/remaining (ACCEPTED contracts only — a proposal
// commits nothing), the properties worked, the latest contract date. Derived
// every render; none of it is stored on the contractor.
function reContractorRollup() {
  const by = {};
  reContracts().forEach((c) => {
    const k = (c.contractor || "").toLowerCase();
    const a = by[k] || (by[k] = reCtrZero());
    a.n++;
    if (c.status === "proposed") { a.proposed++; a.bid += c.total || 0; }
    if (c.status === "accepted") {
      a.accepted++;
      a.committed += c.total || 0;
      a.drawn += c.drawn || 0;
      a.remaining += c.remaining != null ? c.remaining : (c.total || 0) - (c.drawn || 0);
    }
    (c.allocations || []).forEach((al) => {
      if (al.property && !a.props.includes(al.property)) a.props.push(al.property);
    });
    if ((c.date || "") > a.last) a.last = c.date || "";
  });
  return by;
}

function reCtrZero() {
  return { n: 0, proposed: 0, accepted: 0, committed: 0, drawn: 0, remaining: 0, bid: 0, props: [], last: "" };
}

function reCtrAgg(rollup, slug) { return rollup[(slug || "").toLowerCase()] || reCtrZero(); }

// reCtrState — the one classification the filter chips read. WORKING is money
// committed and not yet drawn (they owe you work); BIDDING is a proposal
// outstanding (you owe them an answer); QUIET is everyone else.
function reCtrState(a) {
  if (a.accepted && a.remaining > 0) return "working";
  if (a.proposed) return "bidding";
  return "quiet";
}

function reCtrPropLabel(slug) {
  const p = rePropBySlug(slug);
  return p ? rePropLabel(p) : slug;
}

async function renderREContractors() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  await ensureEntities();
  if (reContractsCache === null) await loadReContracts();

  const wrap = el("div", "aion-backlog fr-shell");
  const main = el("div", "aion-list fr-main");
  const insp = el("aside", "aion-inspector fr-inspector");
  wrap.append(main, insp);
  host.append(wrap);

  const rows = ((entitiesCache || {}).contractors) || [];
  const rollup = reContractorRollup();

  // toolbar — search + state chips. BOTH filter in place (paint below) rather
  // than re-rendering the tab: a full render replaces the input node and takes
  // the caret out of the search field after the first keystroke.
  const bar = el("div", "fr-toolbar");
  const search = el("input", "pp-in fr-search");
  search.type = "search";
  search.placeholder = "Search contractors, scopes, properties…";
  search.value = reCtrQuery;
  search.oninput = () => { reCtrQuery = search.value; paint(); };
  bar.append(search);
  const chips = {};
  [["all", "ALL"], ["working", "WORKING"], ["bidding", "BIDDING"], ["quiet", "QUIET"]].forEach(([key, label]) => {
    const b = el("button", "filter-chip", label);
    b.onclick = () => { reCtrFilter = key; paint(); };
    chips[key] = b;
    bar.append(b);
  });
  main.append(bar);

  main.append(ghostInput("＋ contractor", "aion-add fr-add", async (v) => {
    try {
      await postJSONOk("/api/realestate/entities", { name: v, kind: "contractor" });
      await ensureEntities(true);
      renderProperties();
    } catch (err) { showToast("Couldn't create contractor"); }
  }, "contractor name…"));

  const table = el("div", "fr-table");
  main.append(table);

  const paint = () => {
    Object.keys(chips).forEach((k) => chips[k].classList.toggle("on", reCtrFilter === k));
    table.innerHTML = "";
    const head = el("div", "fr-row re-ctr-row fr-head");
    ["CONTRACTOR", "SCOPES", "PROPERTIES", "COMMITTED"].forEach((h) => head.append(el("span", "micro-label", h)));
    table.append(head);
    const visible = rows.filter((c) => reCtrVisible(c, reCtrAgg(rollup, c.slug)));
    if (!visible.length) {
      table.append(emptyRow(rows.length ? "No contractors match." : "No contractor records yet."));
    }
    visible.forEach((c) => table.append(reCtrRow(c, reCtrAgg(rollup, c.slug), paint)));

    const sel = rows.find((c) => c.slug === reCtrSel);
    if (window.mf && window.mf.phone()) {
      if (sel) {
        window.mfSheet.open((body) => renderContractorInspector(body, sel, { agg: reCtrAgg(rollup, sel.slug) }), {
          key: "re-contractor",
          onClose: () => { if (reCtrSel) { reCtrSel = null; paint(); } },
          reopen: () => { if (!els.propertiesView.hidden && propMode === "contractors") renderProperties(); },
        });
      } else {
        window.mfSheet.closeIf("re-contractor");
      }
      return;
    }
    insp.innerHTML = "";
    if (sel) {
      renderContractorInspector(insp, sel, {
        agg: reCtrAgg(rollup, sel.slug),
        onClose: () => { reCtrSel = null; paint(); },
      });
    } else {
      insp.append(el("div", "aion-insp-empty", "select a contractor — edits save as you go"));
    }
  };
  paint();
}

function reCtrVisible(c, a) {
  if (reCtrFilter !== "all" && reCtrState(a) !== reCtrFilter) return false;
  const q = reCtrQuery.trim().toLowerCase();
  if (!q) return true;
  return [c.name, c.slug, c.email, c.website]
    .concat(c.scopes || [], a.props.map(reCtrPropLabel))
    .join(" ").toLowerCase().includes(q);
}

function reCtrRow(c, a, paint) {
  const row = el("div", "fr-row re-ctr-row" + (reCtrSel === c.slug ? " sel" : ""));

  // the name is the one link out of the row (fundraising's people buttons
  // navigate the same way); everywhere else selects into the inspector
  const who = el("div", "fr-firm");
  const name = el("button", "fr-firm-name re-ctr-name", c.name);
  name.title = "open " + c.name + " — contracts, properties, money";
  name.onclick = (ev) => {
    ev.stopPropagation();
    location.hash = "#/properties/contractor/" + encodeURIComponent(c.slug);
  };
  who.append(name);
  if (c.website) {
    const site = el("a", "fr-website-link", "↗");
    site.href = c.website;
    site.target = "_blank";
    site.rel = "noopener";
    site.title = "open website";
    site.setAttribute("aria-label", "Open " + c.name + " website");
    site.onclick = (ev) => ev.stopPropagation();
    who.append(site);
  }
  row.append(who);

  const scopes = el("div", "fr-people");
  (c.scopes || []).forEach((sc) => scopes.append(el("span", "re-ctr-scope", sc)));
  if (!scopes.children.length) scopes.append(el("span", "fr-person-empty", "—"));
  row.append(scopes);

  const props = el("div", "fr-people re-ctr-props");
  a.props.forEach((slug) => {
    const b = el("button", "fr-person-name fr-person re-ctr-prop", reCtrPropLabel(slug));
    b.onclick = (ev) => { ev.stopPropagation(); location.hash = "#/properties/" + encodeURIComponent(slug); };
    props.append(b);
  });
  if (!props.children.length) props.append(el("span", "fr-person-empty", "—"));
  row.append(props);

  // money first (the RE convention): committed on top, the shape of it beneath
  const money = el("div", "fr-stack");
  money.append(el("span", "re-ctr-committed", a.committed ? fmtMoney(a.committed) : "—"));
  const bits = [];
  if (a.accepted) bits.push(a.accepted + " accepted");
  if (a.drawn) bits.push(fmtMoneyShort(a.drawn) + " drawn");
  if (a.bid) bits.push(fmtMoneyShort(a.bid) + " bid");
  money.append(el("span", "fr-sub", bits.join(" · ")));
  row.append(money);

  row.onclick = () => { reCtrSel = reCtrSel === c.slug ? null : c.slug; paint(); };
  return row;
}

// renderContractorInspector — the fundraising quick-edit idiom: field(label,
// node); text inputs commit on blur when dirty. opts.onClose adds the ✕ (the
// table's aside closes; the history page's copy has nothing to close).
// opts.agg adds the derived money read — the history page carries the same
// numbers in its own facts strip, so it passes none. opts.history:false drops
// the link when you are already on that page.
function renderContractorInspector(host, c, opts) {
  opts = opts || {};
  host.innerHTML = "";
  const patch = async (set) => {
    try {
      await postJSONOk("/api/realestate/contractors/" + encodeURIComponent(c.slug) + "/update", set);
      await ensureEntities(true);
      await loadReContracts();
      renderProperties();
    } catch (e) { showToast("Couldn't save — " + (e.message || "")); }
  };
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Contractor"));
  if (opts.onClose) {
    const x = el("button", "aion-insp-x", "✕");
    x.onclick = opts.onClose;
    head.append(x);
  }
  host.append(head);

  const field = (label, node) => {
    const f = el("div", "aion-insp-field fr-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    host.append(f);
  };
  const text = (label, key, value, placeholder) => {
    const n = inputEl(placeholder || "");
    n.className = "pp-in fr-in";
    n.value = value || "";
    let old = n.value;
    n.onkeydown = (ev) => { if (ev.key === "Enter") { ev.preventDefault(); n.blur(); } };
    n.onblur = () => { if (n.value !== old) { old = n.value; patch({ [key]: n.value }); } };
    field(label, n);
    return n;
  };

  text("name", "name", c.name);
  text("email", "email", c.email, "email…");
  // website — the field plus the way out of it (fundraising's editor)
  const site = el("div", "fr-website-editor");
  const siteIn = inputEl("https://…");
  siteIn.className = "pp-in fr-in";
  siteIn.value = c.website || "";
  let oldSite = siteIn.value;
  siteIn.onkeydown = (ev) => { if (ev.key === "Enter") { ev.preventDefault(); siteIn.blur(); } };
  siteIn.onblur = () => { if (siteIn.value !== oldSite) { oldSite = siteIn.value; patch({ website: siteIn.value }); } };
  site.append(siteIn);
  if (c.website) {
    const open = el("a", "fr-website-open", "open ↗");
    open.href = c.website;
    open.target = "_blank";
    open.rel = "noopener";
    site.append(open);
  }
  field("website", site);

  // scopes: chips + a typeahead over the template/milestone vocabulary (§3.7),
  // free text accepted — the vocabulary is a join over what exists, not a list
  // to be complete.
  const box = el("div", "re-scope-box");
  const scopes = [...(c.scopes || [])];
  const renderChips = () => {
    box.innerHTML = "";
    scopes.forEach((sc, i) => {
      const chip = el("span", "fr-person-chip", sc);
      const x = el("button", "fr-person-rm", "✕");
      x.title = "remove scope";
      x.onclick = () => { scopes.splice(i, 1); renderChips(); patch({ scopes }); };
      chip.append(x);
      box.append(chip);
    });
    const commit = (v) => {
      if (!v || scopes.includes(v)) return;
      scopes.push(v);
      renderChips();
      patch({ scopes });
    };
    const ta = typeahead({
      placeholder: "add scope…",
      minChars: 1,
      suggest: (q, add) => {
        reScopeVocab().filter((v) => v.includes(q) && !scopes.includes(v))
          .slice(0, 8).forEach((v) => add(v, "", () => commit(v)));
      },
      onEnter: (v) => commit(v.toLowerCase().trim()),
    });
    box.append(ta.el);
  };
  renderChips();
  field("scopes", box);

  // derived money — read-only, and only where nothing else on screen says it
  const a = opts.agg;
  if (a) {
    field("committed", el("span", "aion-insp-ro", a.committed ? fmtMoney(a.committed) : "—"));
    field("drawn", el("span", "aion-insp-ro", a.drawn ? fmtMoney(a.drawn) : "—"));
    field("remaining", el("span", "aion-insp-ro", a.committed ? fmtMoney(a.remaining) : "—"));
    field("contracts", el("span", "aion-insp-ro",
      a.n ? a.accepted + " accepted · " + a.proposed + " proposed" : "none yet"));
    if (a.bid) field("out to bid", el("span", "aion-insp-ro", fmtMoney(a.bid)));
  }

  const links = el("div", "re-ctr-insp-links");
  if (opts.history !== false) {
    const hist = el("button", "pp3-link", "history ↗");
    hist.title = "contracts, properties worked, and what is still open";
    hist.onclick = () => { location.hash = "#/properties/contractor/" + encodeURIComponent(c.slug); };
    links.append(hist);
  }
  if (c.path) {
    const rec = el("button", "pp3-link", "record ↗");
    rec.title = c.path;
    rec.onclick = () => { _noteReturn = "#/properties/contractors"; openNoteByPath(c.path); };
    links.append(rec);
  }
  if (links.children.length) host.append(links);
  host.append(el("div", "aion-insp-foot", "edits save as you go"));
}

// ---- the contractor HISTORY page (contact-page conventions; all derived) ----

async function renderContractorPage(slug) {
  const host = els.propertyBoard;
  host.innerHTML = "";
  let d = null;
  try { d = await (await fetch("/api/realestate/contractors/" + encodeURIComponent(slug) + "/page")).json(); }
  catch (e) {}
  if (!d || !d.contractor) { host.append(el("div", "pp-empty", "Contractor not found.")); return; }
  const c = d.contractor;

  const shell = el("div", "aion-backlog fr-shell re-ctr-shell");
  const main = el("div", "aion-list fr-main re-ctrp");
  const insp = el("aside", "aion-inspector fr-inspector");
  shell.append(main, insp);
  host.append(shell);
  // the aside is the SAME editor as the table's — minus the money block, which
  // the facts strip below already says, and minus the link to this page
  renderContractorInspector(insp, c, { history: false });

  // a contractor page opens from a row, a contract, or a bid; give it the way
  // back (the tab bar lights CONTRACTORS, which is only one of those doors)
  const crumb = el("div", "pp3-crumb");
  const back = el("button", "pp3-link", "← contractors");
  back.onclick = () => { location.hash = "#/properties/contractors"; };
  crumb.append(back);
  main.append(crumb);

  // header: name + scope chips
  const head = el("div", "cp-head re-ctrp-head");
  head.append(el("h2", "cp-name", c.name));
  (c.scopes || []).forEach((s) => head.append(el("span", "cp-role", s)));
  main.append(head);
  if (c.email || c.website) {
    const meta = el("div", "re-meta-row");
    if (c.email) meta.append(el("span", "", c.email));
    if (c.website) {
      const a = el("a", "pp3-link", c.website.replace(/^https?:\/\//, ""));
      a.href = c.website; a.target = "_blank"; a.rel = "noopener";
      meta.append(a);
    }
    main.append(meta);
  }

  // facts strip between hairlines: committed / drawn / remaining / contracts
  const facts = el("div", "cp-facts re-ctrp-facts");
  const fact = (label, val) => {
    const f = el("div", "cp-fact");
    f.append(el("div", "cp-fact-label", label), el("div", "cp-fact-val", val));
    facts.append(f);
  };
  fact("COMMITTED", d.committed ? fmtMoney(d.committed) : "—");
  fact("DRAWN", d.drawn ? fmtMoney(d.drawn) : "—");
  fact("REMAINING", d.committed ? fmtMoney(d.remaining) : "—");
  fact("CONTRACTS", String((d.contracts || []).length));
  main.append(facts);

  // contracts by status
  const secHead = (t, n) => {
    const h = el("div", "pp3-sec-head");
    h.append(el("span", "pp3-sec-title", t));
    if (n != null) h.append(el("span", "pp3-sec-count", String(n)));
    return h;
  };
  const contracts = d.contracts || [];
  main.append(secHead("CONTRACTS", contracts.length));
  if (!contracts.length) main.append(el("div", "pp-empty", "No contracts yet."));
  contracts.forEach((cv) => {
    const row = el("div", "prop-row re-contract-row");
    row.onclick = () => { location.hash = "#/properties/contract/" + encodeURIComponent(cv.slug); };
    row.append(el("span", "re-contract-status s-" + cv.status, cv.status));
    row.append(el("span", "prop-addr", cv.name));
    row.append(el("span", "re-contract-money",
      fmtMoney(cv.total) + (cv.drawn ? " · drawn " + fmtMoney(cv.drawn) : "")));
    main.append(row);
  });

  // properties worked
  if ((d.properties || []).length) {
    main.append(secHead("PROPERTIES", d.properties.length));
    const box = el("div", "re-ctrp-props");
    d.properties.forEach((p) => {
      const chip = el("button", "cp-firm", p.name);
      chip.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
      box.append(chip);
    });
    main.append(box);
  }

  // open tree tasks / decisions owned
  if ((d.owned || []).length) {
    main.append(secHead("OPEN — THEIRS", d.owned.length));
    d.owned.forEach((t) => {
      const row = el("div", "pp3-todo");
      row.append(el("span", "tdo-check", t.decision ? "◇" : "○"),
        el("span", "pp3-todo-text", t.text),
        el("span", "pp3-dec-rock", t.property + " · " + t.rock));
      main.append(row);
    });
  }

  // free prose from the record
  if (d.prose) {
    main.append(secHead("NOTES", null));
    main.append(el("div", "re-ctrp-prose", d.prose));
  }
}

// ---- the CONTRACT page (overhaul §4: total · allocations · drawn · remaining) ----

async function renderContractPage(slug) {
  const host = els.propertyBoard;
  host.innerHTML = "";
  let d = null;
  try { d = await (await fetch("/api/realestate/contracts/" + encodeURIComponent(slug))).json(); }
  catch (e) {}
  if (!d || !d.contract) { host.append(el("div", "pp-empty", "Contract not found.")); return; }
  const c = d.contract;
  const patch = async (set) => {
    try {
      await postJSONOk("/api/realestate/contracts/" + encodeURIComponent(slug) + "/update", set);
      renderProperties();
    } catch (e) { showToast("Couldn't save — " + (e.message || "")); }
  };

  const page = el("div", "re-contract-page");
  host.append(page);
  // Every block is a .pp3-sec — the property page's rhythm and its --measure
  // cap, instead of this page's own width and heads running into each other.
  const section = (title, count) => {
    const sec = el("div", "pp3-sec");
    if (title) {
      const h = el("div", "pp3-sec-head");
      h.append(el("span", "pp3-sec-title", title));
      if (count !== undefined) h.append(el("span", "pp3-sec-count", String(count)));
      sec.append(h);
    }
    page.append(sec);
    return sec;
  };

  const top = section();
  // a contract opens from a property; give it the way back (the tab bar lights
  // PORTFOLIO, which is not where you came from)
  const propSlugs = [...new Set((c.allocations || []).map((a) => a.property))];
  const crumb = el("div", "pp3-crumb");
  const back = el("button", "pp3-link",
    "← " + (propSlugs.length === 1 ? ((d.propertyNames || {})[propSlugs[0]] || propSlugs[0]) : "portfolio"));
  back.onclick = () => {
    location.hash = propSlugs.length === 1
      ? "#/properties/" + encodeURIComponent(propSlugs[0]) : "#/properties/portfolio";
  };
  crumb.append(back);
  top.append(crumb);

  const head = el("div", "pp3-head");
  head.append(el("h2", "pp3-title", c.name));
  // status select — save as you go. The chip control (mono, uppercase) rather
  // than a sans form field; accent only while the contract is live.
  const LIVE_STATUS = ["proposed", "accepted"];
  const sel = selectEl(["proposed", "accepted", "declined", "expired", "closed"]);
  sel.value = c.status;
  const paintStatus = () => {
    sel.className = "prop-status-sel" + (LIVE_STATUS.includes(sel.value) ? "" : " quiet");
  };
  paintStatus();
  sel.onchange = () => { paintStatus(); patch({ status: sel.value }); };
  head.append(sel);
  const openNote = el("button", "pp3-note", "note ↗");
  openNote.onclick = () => { location.hash = "#/note/" + encodeURIComponent(c.path); };
  head.append(openNote);
  top.append(head);

  const meta = el("div", "re-meta-row");
  const ctrBtn = el("button", "pp3-link", "@" + c.contractor);
  ctrBtn.onclick = () => { location.hash = "#/properties/contractor/" + encodeURIComponent(c.contractor); };
  meta.append(ctrBtn);
  if (c.date) meta.append(el("span", "", c.date));
  if (c.expires) meta.append(el("span", "", "expires " + c.expires));
  if (c.doc) {
    if (c.doc.startsWith("sha256:")) {
      const a = el("a", "pp3-link", "document ↗");
      a.href = "/api/realestate/files/" + c.doc.slice(7);
      a.target = "_blank";
      meta.append(a);
    } else {
      meta.append(el("span", "", "doc: " + c.doc));
    }
  }
  top.append(meta);

  // money strip: TOTAL (editable, optional reason — decision 16) · DRAWN · REMAINING
  const strip = el("div", "pp3-strip");
  const cell = (label, val, cls) => {
    const box = el("div", "pp3-cell");
    box.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val" + (cls ? " " + cls : ""), val));
    return box;
  };
  const totalCell = cell("TOTAL", fmtMoney(c.total));
  totalCell.classList.add("click");
  totalCell.title = "edit the committed total (a reason is optional — it logs to ## changes)";
  totalCell.onclick = () => {
    const v = prompt("Committed total:", String(c.total));
    if (v === null) return;
    const num = parseFloat(v.replace(/[$,]/g, ""));
    if (!(num > 0)) { showToast("Total must be a positive number"); return; }
    const reason = prompt("Reason (optional — logs to the change history):", "") || "";
    const set = { total: num };
    // single-allocation contracts re-balance automatically; multi needs the note view
    if ((c.allocations || []).length === 1) {
      const a = c.allocations[0];
      set.allocations = [a.property + " | " + a.nodeId + " | " + num];
    } else {
      showToast("Multi-property contract — adjust allocations in the record (note ↗)");
      return;
    }
    if (reason.trim()) set.changeNote = (num > c.total ? "+" : "") + (num - c.total) + " " + reason.trim();
    patch(set);
  };
  strip.append(totalCell);
  strip.append(cell("DRAWN", d.contract.drawn ? fmtMoney(d.contract.drawn) : "—"));
  strip.append(cell("REMAINING", fmtMoney(d.remaining), d.remaining < 0 ? "over" : ""));
  top.append(strip);

  // allocations — cross-property by construction
  const allocSec = section("ALLOCATIONS", (c.allocations || []).length);
  (c.allocations || []).forEach((a) => {
    const row = el("div", "prop-row re-alloc-row");
    const pname = (d.propertyNames || {})[a.property] || a.property;
    const link = el("span", "prop-addr", pname);
    link.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(a.property); };
    row.append(link);
    row.append(el("span", "re-alloc-node", a.nodeId));
    row.append(el("span", "prop-col-r", fmtMoney(a.amount)));
    allocSec.append(row);
  });

  // draws — expenses carrying [contract:: slug]
  const drawSec = section("DRAWS", (d.draws || []).length);
  if (!(d.draws || []).length) drawSec.append(el("div", "pp-empty", "No draws yet — expenses tethered [contract:: " + slug + "] land here."));
  (d.draws || []).forEach((r) => {
    const row = el("div", "prop-row re-alloc-row");
    row.append(el("span", "re-money-date", r.date), el("span", "prop-addr", r.vendor + (r.note ? " · " + r.note : "")),
      el("span", "prop-col-r", fmtMoney(r.amount)));
    drawSec.append(row);
  });

  // prose sections — display-only extracted terms
  const prose = (title, lines) => {
    if (!(lines || []).length) return;
    const sec = section(title.toUpperCase());
    lines.forEach((ln) => sec.append(el("div", "re-contract-prose", "· " + ln)));
  };
  prose("terms", c.terms);
  prose("exclusions", c.exclusions);
  prose("risk items", c.riskItems);
  // the change log is dated metadata (`- YYYY-MM-DD ±N reason`, contracts.go),
  // not prose — it reads mono with the date in its own column
  if ((c.changes || []).length) {
    const sec = section("CHANGES", c.changes.length);
    c.changes.forEach((ln) => {
      const row = el("div", "re-change-row");
      const m = String(ln).match(/^(\d{4}-\d{2}-\d{2})\s+([\s\S]*)$/);
      if (m) row.append(el("span", "re-money-date", m[1]), el("span", "", m[2]));
      else row.append(el("span", "", String(ln)));
      sec.append(row);
    });
  }
}

