// ---- REAL ESTATE domain views (RE spec §3): Decisions · Intake · Money ·
// Deal page. The decision log is the aion mirror (system/realestate/
// backlog.md via /api/re/backlog); Intake is the RE-scoped lens over pending
// re-backlog proposals (same approval cards as the FEED); Money is the
// read-only transaction feed over the statement workbench + property select.

// ---- DECISIONS — one lane for the portfolio ----
// Open decisions: 2px ink left rule, --base-05 background, 500 weight.
// Decided: flat, outcome in the meta line. One `open` boolean drives count,
// weight, rule, and background together.
let reDecidedOpen = false;

function renderREDecisions() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const items = reItems().filter((it) => it.kind === "decision");
  const open = items.filter((it) => it.status !== "decided");
  const decided = items.filter((it) => it.status === "decided");
  host.append(el("div", "re-lane-head", "DECISIONS · " + open.length + " open · " + decided.length + " decided"));
  if (!open.length && !decided.length) {
    host.append(emptyRow("No decisions yet — capture one below, or confirm intake proposals."));
  }
  open.forEach((it) => host.append(reDecisionRow(it, true)));
  host.append(ghostInput("＋ decision", "re-add", (v) =>
    rePost("/api/re/backlog/item", { kind: "decision", title: v }, "Decision added")));
  if (decided.length) {
    const t = el("button", "re-decided-toggle", (reDecidedOpen ? "▾" : "▸") + " decided · " + decided.length);
    t.onclick = () => { reDecidedOpen = !reDecidedOpen; renderProperties(); };
    host.append(t);
    if (reDecidedOpen) decided.forEach((it) => host.append(reDecisionRow(it, false)));
  }
}

function reDecisionRow(it, isOpen) {
  const row = el("div", "re-decision" + (isOpen ? " open" : ""));
  const main = el("div", "re-decision-main");
  main.append(el("div", "re-decision-title", it.text));
  const meta = el("div", "re-decision-meta");
  const bits = [];
  if (it.owner) bits.push("@" + it.owner);
  if (it.rock) bits.push("⧗ " + it.rock);
  if (isOpen && it.neededBy) bits.push("needed by " + it.neededBy);
  if (!isOpen && it.decided) bits.push("decided " + it.decided);
  if (!isOpen && it.outcome) bits.push("→ " + it.outcome);
  if (it.sources && it.sources.length) bits.push("[[" + it.sources[0] + "]]");
  meta.textContent = bits.join(" · ");
  main.append(meta);
  row.append(main);
  if (isOpen) {
    const acts = el("span", "re-decision-acts");
    const outcome = inputEl("outcome — what was decided…");
    outcome.className = "re-outcome-in";
    const decide = pillLight("decide", () => {
      if (!outcome.value.trim()) { showToast("write the outcome first"); outcome.focus(); return; }
      rePost("/api/re/backlog/" + it.id + "/decide", { outcome: outcome.value.trim() }, "Decided — permanent log");
    });
    acts.append(outcome, decide);
    row.append(acts);
  }
  return row;
}

async function rePost(url, body, msg) {
  setSaveState("saving");
  try {
    await postJSONOk(url, body);
    setSaveState("saved");
    if (msg) showToast(msg);
  } catch (e) { setSaveState("error"); showToast(String(e.message || e).slice(0, 120)); }
  renderProperties();
}

// ---- INTAKE — pending re-backlog proposals, the RE-scoped approvals lens ----
// Each card is the SAME approval card the FEED renders (edit-before-confirm,
// Confirm & apply, Reject) — filtered to this domain, with a `→ lands` line.
async function renderREIntake() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  host.append(el("div", "re-lane-head", "INTAKE — transcript extractions awaiting your confirm"));
  let proposals = [];
  try {
    const d = await (await fetch("/api/feed?status=inbox")).json();
    proposals = (d.proposals || []).filter((p) => p.type === "re-backlog");
  } catch (e) { /* fall through to empty */ }
  reIntakeCache = proposals;
  if (!proposals.length) {
    host.append(emptyRow("Nothing pending. Tag a transcript note real-estate / ooda and the extractor files candidates here."));
    host.append(el("div", "re-foot-note",
      "granola + heypocket transcripts with a real-estate or ooda category flow: note → re-extractor → proposal → your confirm → one line in the decision log"));
    renderPropRail(); // count derives from this fetch
    return;
  }
  proposals.forEach((p) => {
    const wrap = el("div", "re-intake-card");
    // `→ lands` line: exactly where a confirm writes
    const lands = el("div", "re-lands");
    const payload = p.aionPayload || {};
    const bits = ["→ lands: system/realestate/backlog.md"];
    if (payload.rock) bits.push(payload.rock);
    if (payload.owner) bits.push("@" + payload.owner);
    lands.textContent = bits.join(" · ");
    wrap.append(lands);
    wrap.append(approvalCardEl(p));
    host.append(wrap);
  });
  renderPropRail();
}

// ---- MONEY — the read-only transaction feed + property assignment ----
// Grid: date · description · amount · property select. Deposits accent;
// expenses ink. Row click opens the inspector (starts CLOSED). Footer:
// per-entity accountant handoffs split personal vs partnered.
let moneyRows = [];
let moneySelId = null;

async function renderREMoney() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  let last = "";
  try {
    const d = await (await fetch("/api/realestate/statements")).json();
    moneyRows = d.rows || [];
    last = d.lastImport || "";
  } catch (e) { moneyRows = []; }
  const unfiled = moneyRows.filter((r) => r.state === "pending").length;
  host.append(el("div", "re-lane-head",
    "MONEY · " + moneyRows.length + " transactions" + (unfiled ? " · " + unfiled + " uncategorized" : "") +
    (last ? " · last import " + last : "")));
  if (!moneyRows.length) {
    host.append(emptyRow("No transactions — import a bank statement CSV (Settings → Entities names the accounts)."));
  }
  const cols = el("div", "re-money-cols");
  ["DATE", "DESCRIPTION", "AMOUNT", "PROPERTY"].forEach((h, i) =>
    cols.append(el("span", i === 2 ? "prop-col-r" : "", h)));
  host.append(cols);
  const shell = el("div", "re-money-shell");
  const list = el("div", "re-money-list");
  moneyRows.forEach((r) => list.append(moneyRow(r)));
  shell.append(list);
  const insp = el("div", "re-money-insp");
  insp.hidden = true; // starts closed — the list arrives full width
  shell.append(insp);
  host.append(shell);
  // footer: accountant handoffs by entity books, personal vs partnered
  host.append(moneyFooter());
}

function moneyRow(r) {
  const row = el("div", "re-money-row" + (moneySelId === r.id ? " sel" : ""));
  row.append(el("span", "re-money-date", (r.date || "").slice(5)));
  const desc = el("span", "re-money-desc");
  desc.append(el("span", "", r.vendor || r.note || "(no description)"));
  if (r.entity) desc.append(el("span", "re-money-entity", r.entity));
  row.append(desc);
  row.append(el("span", "re-money-amt" + (r.inflow ? " inflow" : ""),
    (r.inflow ? "+" : "") + fmtMoneyShort(Math.abs(r.amount || 0))));
  // property select — assignment in place (single-target; splits via inspector)
  const sel = document.createElement("select");
  sel.className = "pp-in re-money-sel";
  const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); };
  opt("", r.state === "applied" ? "applied" : "unassigned");
  activePortfolio().forEach((p) => opt(p.slug, p.short || p.slug));
  const cur = (r.assignments && r.assignments[0] && r.assignments[0].slug) || "";
  sel.value = cur;
  sel.disabled = r.state === "applied" || r.state === "skipped";
  sel.onclick = (e) => e.stopPropagation();
  sel.onchange = async () => {
    try {
      await postJSONOk("/api/realestate/statements/row", {
        id: r.id,
        assignments: sel.value ? [{ slug: sel.value, amount: Math.abs(r.amount || 0) }] : [],
        state: sel.value ? "assigned" : "pending",
      });
      showToast(sel.value ? "Assigned — apply writes it to the ledger" : "Unassigned");
    } catch (e) { showToast("Couldn't assign"); }
  };
  row.append(sel);
  row.onclick = () => { moneySelId = moneySelId === r.id ? null : r.id; renderMoneyInspector(r); };
  return row;
}

function renderMoneyInspector(r) {
  const insp = document.querySelector(".re-money-insp");
  if (!insp) return;
  if (moneySelId !== r.id) { insp.hidden = true; insp.innerHTML = ""; return; }
  insp.hidden = false;
  insp.innerHTML = "";
  const head = el("div", "pp3-insp-head");
  head.append(el("span", "pp3-insp-label", "Transaction"));
  const x = el("button", "pp3-insp-x", "✕");
  x.onclick = () => { moneySelId = null; insp.hidden = true; };
  head.append(x);
  insp.append(head);
  insp.append(el("div", "pp3-insp-text", (r.vendor || "") + " · " + (r.date || "")));
  const fieldRow = (label, val) => {
    const f = el("div", "pp3-insp-field");
    f.append(el("span", "pp3-insp-flabel", label), el("span", "pp3-insp-val", val || "—"));
    return f;
  };
  insp.append(fieldRow("amount", (r.inflow ? "+" : "") + "$" + Math.abs(r.amount || 0).toLocaleString()));
  insp.append(fieldRow("entity", r.entity));
  insp.append(fieldRow("statement", r.statement));
  insp.append(fieldRow("state", r.state));
  insp.append(fieldRow("category", r.category));
  if (r.note) insp.append(fieldRow("note", r.note));
  insp.append(el("div", "pp3-insp-note",
    "assignment writes to the property ledger on apply; receipts + contractor attach on the ledger row (property page → spend)"));
}

function moneyFooter() {
  const foot = el("div", "re-money-foot");
  foot.append(el("div", "re-lane-head", "ACCOUNTANT HANDOFFS — split by the entity the books belong to"));
  // group entities via holdings + partnered flag (from /api/realestate/entities
  // in settings; here we derive from statement rows' entity labels + holdings)
  const names = Object.keys(holdingsCache).sort();
  if (!names.length) {
    foot.append(el("div", "re-foot-note", "no entity holdings yet — set each property's entity (its books) on the property page"));
    return foot;
  }
  names.forEach((name) => {
    const h = holdingsCache[name];
    const row = el("div", "re-handoff-row");
    row.append(el("span", "re-handoff-name", name));
    row.append(el("span", "re-handoff-meta", h.owned + " owned" + (h.acquiring ? " · " + h.acquiring + " acquiring" : "")));
    const exp = pillLight("export tax csv", async () => {
      try {
        const res = await postJSONOk("/api/realestate/export-tax", { entity: name });
        showToast("Exported — " + (res.path || "vault exports/"));
      } catch (e) { showToast("Export failed — " + (e.message || "")); }
    });
    row.append(exp);
    foot.append(row);
  });
  return foot;
}

// ---- DEAL PAGE — stat strip, members + totals, overrides, checklist ----
async function renderDealPage(slug) {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const deal = dealCache.find((d) => d.slug === slug);
  if (!deal) { host.append(emptyRow("No deal record named " + slug + ".")); return; }
  if (!reAssumptionsCache) await loadReAssumptions();
  const head = el("div", "re-deal-head");
  const title = el("h2", "pp3-title", deal.name || deal.slug);
  head.append(title);
  const open = el("button", "pp3-note", "open note →");
  open.onclick = () => { location.hash = "#/note/" + encodeURIComponent(deal.path); };
  head.append(open);
  host.append(head);

  // members: the properties whose deal wikilink names this deal
  const members = propertyCache.filter((p) => (p.deal || "") === (deal.name || deal.slug) || (p.deal || "") === deal.slug);
  // tier-1 inputs live on each member's source sidecar — fetch once per render
  await Promise.all(members.map(async (p) => {
    if (p.__source) return;
    try { p.__source = await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json(); }
    catch (e) { p.__source = {}; }
  }));
  // stat strip from member source data (screening tier — the portal runs the
  // full engine; these are the three outputs the spec shows)
  let tdc = 0, noi = 0;
  members.forEach((p) => {
    const uw = reScreeningCalc(p);
    tdc += uw.tdc || 0;
    noi += uw.noi || 0;
  });
  const a = reAssumptions();
  const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
  const ads = reDebtService(arv * (a.perm_ltv || 0), a);
  const dscr = ads ? noi / ads : 0;
  const strip = el("div", "pp3-strip");
  [["TDC", tdc], ["NOI", noi], ["ARV", arv]].forEach(([label, v]) => {
    const c = el("div", "pp3-cell");
    c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val", fmtMoneyShort(v)));
    strip.append(c);
  });
  const dc = el("div", "pp3-cell");
  dc.append(el("div", "pp3-cell-label", "DSCR"),
    el("div", "pp3-cell-val" + (dscr && dscr < 1.25 ? " over" : ""), dscr ? dscr.toFixed(2) : "—"));
  strip.append(dc);
  host.append(strip);

  // members table + totals row
  const cols = el("div", "prop-cols re-deal-cols");
  ["PROPERTY", "UNITS", "TDC", "NOI", "DSCR"].forEach((h, i) => cols.append(el("span", i ? "prop-col-r" : "", h)));
  host.append(cols);
  members.forEach((p) => {
    const uw = reScreeningCalc(p);
    const row = el("div", "prop-row re-deal-row");
    row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    row.append(el("span", "prop-addr", p.short || p.slug));
    row.append(el("span", "prop-col-r", String(p.units || "—")));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.tdc || 0)));
    row.append(el("span", "prop-col-r", fmtMoneyShort(uw.noi || 0)));
    row.append(el("span", "prop-col-r" + (uw.dscr && uw.dscr < 1.25 ? " over" : ""), uw.dscr ? uw.dscr.toFixed(2) : "—"));
    host.append(row);
  });
  const totals = el("div", "prop-row re-deal-totals");
  totals.append(el("span", "prop-addr", "deal total"));
  totals.append(el("span", "prop-col-r", String(members.reduce((n, p) => n + (p.units || 0), 0) || "—")));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(tdc)));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(noi)));
  totals.append(el("span", "prop-col-r" + (dscr && dscr < 1.25 ? " over" : ""), dscr ? dscr.toFixed(2) : "—"));
  host.append(totals);

  host.append(el("div", "re-foot-note",
    "screening numbers (tier 1) — the portal runs the full engine on PUBLISH; overrides live on the deal's source.json"));

  // bank-ready checklist — derived from what the deal actually has
  const check = el("div", "re-deal-check");
  check.append(el("div", "re-lane-head", "BANK-READY CHECKLIST"));
  const items = [
    ["every member has units + rent inputs", members.length > 0 && members.every((p) => reScreeningCalc(p).complete)],
    ["deal note exists", true],
    ["DSCR ≥ 1.25 at globals", dscr >= 1.25],
    ["published to the portal", false],
  ];
  items.forEach(([label, ok]) => {
    const li = el("div", "re-check-row" + (ok ? " ok" : ""));
    li.append(el("span", "re-check-glyph", ok ? "✓" : "○"), el("span", "", label));
    check.append(li);
  });
  host.append(check);
}

// ---- tier-1 screening math (spec §3 property page: computed NOI · ARV ·
// DSCR). Deliberately the MINIMAL mirror of calcDeal's core — go/no-go
// numbers, labeled as screening; the portal engine remains the pro-forma
// truth on publish. ----
let reAssumptionsCache = null;
function reAssumptions() { return reAssumptionsCache || {}; }
async function loadReAssumptions() {
  try {
    const d = await (await fetch("/api/realestate/assumptions")).json();
    reAssumptionsCache = d.values || {};
    reAssumptionsCache.__labels = d.labels || {};
    reAssumptionsCache.__keys = d.keys || [];
    reAssumptionsCache.__overrides = d.overrides || [];
  } catch (e) { reAssumptionsCache = {}; }
}

function reDebtService(loan, a) {
  const r = (a.perm_interest_rate || 0) / 12;
  const n = (a.perm_amort_years || 25) * 12;
  if (!loan || !r || !n) return 0;
  const m = loan * (r * Math.pow(1 + r, n)) / (Math.pow(1 + r, n) - 1);
  return m * 12;
}

// reScreeningCalc reads a property's tier-1 inputs from its source sidecar
// data (mirrored into the /api payload as units + the source cache the page
// fetches) — here we work off the list payload's units + page-level source
// fetches fill the rest; callers on the deal page get tdc/noi/dscr where the
// inputs exist.
function reScreeningCalc(p) {
  const src = p.__source || {};
  const a = reAssumptions();
  const units = p.units || src.total_units || 0;
  const rent = src.avg_rent_per_unit || 0;
  const purchase = src.purchase_price || 0;
  const hard = src.hard_costs || 0;
  if (!units || !rent) return { complete: false };
  const gross = units * rent * 12;
  const egi = gross * (1 - (a.vacancy_rate || 0));
  const noi = egi * (1 - (a.opex_rate || 0));
  const soft = hard * 0.15; // screening approximation; the engine itemizes
  const contingency = hard * (a.contingency_pct || 0);
  const tdc = purchase + hard + soft + contingency;
  const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
  const loan = arv * (a.perm_ltv || 0);
  const ads = reDebtService(loan, a);
  return { complete: true, gross, egi, noi, tdc, arv, loan, dscr: ads ? noi / ads : 0 };
}

// ---- PUBLISH → oodagroup — the portal export effector (RE spec §4) ----
// The AION gesture: PREVIEW (nothing written) → CONFIRM carrying the preview
// hash → one commit, pushed. Two contract paths only: deals.json + defaults.js.

async function openRePublishPanel() {
  if (document.getElementById("rePublishModal")) return;
  const overlay = el("div", "cmdbar");
  overlay.id = "rePublishModal";
  const back = el("div", "cmdbar-backdrop");
  const panel = el("div", "cmdbar-card aion-publish-panel");
  overlay.append(back, panel);
  document.body.append(overlay);
  const close = () => overlay.remove();
  back.onclick = close;
  panel.append(el("div", "appr-diff-label", "PUBLISH → oodagroup — preview"));
  const bodyHost = el("div", "aion-publish-body");
  panel.append(bodyHost, el("div", "aion-publish-note", "fetching preview…"));
  let prev;
  try { prev = await (await fetch("/api/re/publish/preview")).json(); }
  catch (e) { panel.lastChild.textContent = "preview failed: " + e; return; }
  panel.lastChild.remove();

  if ((prev.blockers || []).length) {
    prev.blockers.forEach((b) => bodyHost.append(el("div", "appr-blocked", "⚠ " + b)));
    panel.append(pillLight("close", close));
    return;
  }
  const changed = (prev.files || []).filter((f) => f.status !== "unchanged");
  if (!changed.length && !(prev.unpushed > 0)) {
    bodyHost.append(el("div", "aion-publish-note", "nothing to publish — the checkout matches the record."));
    panel.append(pillLight("close", close));
    return;
  }
  if (prev.unpushed > 0) {
    bodyHost.append(el("div", "aion-publish-note", prev.unpushed + " unpushed commit(s) — publish completes the push."));
  }
  if ((prev.kept || []).length) {
    // template deals with no vault record pass through verbatim — say so
    bodyHost.append(el("div", "aion-publish-note", "kept as-is (no vault record): " + prev.kept.join(", ")));
  }
  changed.forEach((f) => {
    const head = el("div", "aion-pub-file");
    head.append(el("code", "", f.path), el("span", "aion-pub-status " + f.status, f.status));
    bodyHost.append(head);
    if (f.diff) bodyHost.append(collapsibleBlock(diffView(f.diff), f.diff.split("\n").length));
  });
  const actions = el("div", "appr-actions");
  const confirmBtn = pill("CONFIRM — commit + push", async () => {
    confirmBtn.disabled = true;
    try {
      const r = await fetch("/api/re/publish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hash: prev.hash }) });
      const res = await r.json().catch(() => ({}));
      if (r.status === 409) { showToast("Vault changed since preview — re-open PUBLISH"); close(); return; }
      if (!r.ok || res.ok === false) {
        showToast("Publish failed at " + (res.stage || "?") + (res.commit ? " (commit " + res.commit.slice(0, 7) + " kept locally)" : ""));
      } else {
        showToast("Published " + (res.commit || "").slice(0, 7) + " → oodagroup");
      }
      close();
      renderProperties();
    } finally { confirmBtn.disabled = false; }
  });
  actions.append(confirmBtn, pillLight("cancel", close));
  panel.append(actions);
}
