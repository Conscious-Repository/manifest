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

// ---- the contractor table (Settings → Contractors) ----

function reContractorsPane(host) {
  if (reContractsCache === null) {
    loadReContracts().then(() => renderREsettings());
  }
  const shell = el("div", "aion-backlog fr-shell re-ctr-shell");
  const main = el("div", "aion-list fr-main");
  const insp = el("aside", "aion-inspector fr-inspector");
  shell.append(main, insp);
  host.append(shell);

  const rows = ((entitiesCache || {}).contractors) || [];
  const byCtr = {};
  reContracts().forEach((c) => {
    const k = (c.contractor || "").toLowerCase();
    byCtr[k] = byCtr[k] || { n: 0, committed: 0 };
    byCtr[k].n++;
    if (c.status === "accepted") byCtr[k].committed += c.total || 0;
  });

  const table = el("div", "fr-table");
  table.append(ppCols("fr-cols re-ctr-cols", ["CONTRACTOR", "SCOPES", "CONTRACTS", "COMMITTED"]));
  rows.forEach((c) => {
    const row = el("div", "fr-row re-ctr-cols" + (reCtrSel === c.slug ? " sel" : ""));
    row.append(el("span", "fr-firm", c.name));
    row.append(el("span", "re-ctr-scopes", (c.scopes || []).join(" · ") || "—"));
    const agg = byCtr[c.slug.toLowerCase()] || { n: 0, committed: 0 };
    row.append(el("span", "", agg.n ? String(agg.n) : "·"));
    row.append(el("span", "prop-col-r", agg.committed ? fmtMoney(agg.committed) : "·"));
    row.onclick = () => {
      reCtrSel = reCtrSel === c.slug ? null : c.slug;
      renderREsettings();
    };
    table.append(row);
  });
  table.append(ghostInput("＋ contractor", "set-add", async (v) => {
    try { await postJSONOk("/api/realestate/entities", { name: v, kind: "contractor" }); renderREsettings(); }
    catch (err) { showToast("Couldn't create contractor"); }
  }, "contractor name…"));
  main.append(table);

  const selected = rows.find((c) => c.slug === reCtrSel);
  if (window.mf && window.mf.phone()) {
    if (selected) {
      window.mfSheet.open((body) => renderContractorInspector(body, selected), {
        key: "re-contractor", onClose: () => { reCtrSel = null; }, reopen: true,
      });
    } else { window.mfSheet.closeIf("re-contractor"); }
  } else if (selected) {
    renderContractorInspector(insp, selected);
  } else {
    insp.append(el("div", "fr-empty", "select a contractor — edits save as you go"));
  }
}

// renderContractorInspector — the fundraising quick-edit idiom: field(label,
// node); text inputs commit on blur when dirty.
function renderContractorInspector(host, c) {
  host.innerHTML = "";
  const patch = async (set) => {
    try {
      await postJSONOk("/api/realestate/contractors/" + encodeURIComponent(c.slug) + "/update", set);
      await ensureEntities(true);
      await loadReContracts();
      renderREsettings();
    } catch (e) { showToast("Couldn't save — " + (e.message || "")); }
  };
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
    n.onblur = () => { if (n.value !== old) { old = n.value; patch({ [key]: n.value }); } };
    field(label, n);
    return n;
  };
  host.append(el("div", "fr-insp-head", c.name));
  text("name", "name", c.name);
  text("email", "email", c.email, "email…");
  text("website", "website", c.website, "https://…");
  // scopes: chips + typeahead over the template/milestone vocabulary (+ free)
  const box = el("div", "re-scope-box");
  const scopes = [...(c.scopes || [])];
  const renderChips = () => {
    box.innerHTML = "";
    scopes.forEach((s, i) => {
      const chip = el("span", "fr-person-chip", s);
      const x = el("button", "fr-person-x", "✕");
      x.onclick = () => { scopes.splice(i, 1); renderChips(); patch({ scopes }); };
      chip.append(x);
      box.append(chip);
    });
    const ta = typeahead({
      placeholder: "add scope…", minChars: 1,
      suggest: (q, add) => {
        reScopeVocab().filter((v) => v.includes(q.toLowerCase()) && !scopes.includes(v))
          .slice(0, 8).forEach((v) => add(v, () => commit(v)));
      },
      onEnter: (v) => commit(v.toLowerCase().trim()),
    });
    const commit = (v) => {
      if (!v || scopes.includes(v)) return;
      scopes.push(v);
      renderChips();
      patch({ scopes });
    };
    box.append(ta.el);
  };
  renderChips();
  field("scopes", box);
  const hist = el("button", "pp3-link", "history ↗");
  hist.onclick = () => { location.hash = "#/properties/contractor/" + encodeURIComponent(c.slug); };
  field("", hist);
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
  renderContractorInspector(insp, c);

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

