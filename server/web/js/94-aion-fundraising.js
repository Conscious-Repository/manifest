// ---- AION / FUNDRAISING ----
// Private Manifest-only CRM. It intentionally fetches a route outside the
// AionLive contract used by portal.aion.bio.
let frCache = null;
let frStatus = "open";
let frInterest = "all";
let frQuery = "";
let frSel = null;

async function loadFundraising() {
  try {
    const r = await fetch("/api/aion/fundraising", { cache: "no-store" });
    if (!r.ok) throw new Error(await r.text());
    frCache = await r.json();
  } catch (_) { frCache = { opportunities: [], resources: [] }; }
}

async function renderAionFundraising(host) {
  host.innerHTML = "";
  if (!frCache) {
    host.append(emptyRow("loading fundraising…"));
    await loadFundraising();
    if (aionMode === "fundraising") renderAion();
    return;
  }
  const wrap = el("div", "aion-backlog fr-shell");
  const main = el("div", "aion-list fr-main");
  const inspector = el("aside", "aion-inspector fr-inspector");
  wrap.append(main, inspector); host.append(wrap);

  const bar = el("div", "fr-toolbar");
  const search = el("input", "fr-search"); search.type = "search"; search.placeholder = "Search firms, people, next steps…"; search.value = frQuery;
  search.oninput = () => { frQuery = search.value; renderAion(); };
  bar.append(search);
  const statuses = [["open", "OPEN"], ["all", "ALL"], ["prospect", "PROSPECT"], ["active", "ACTIVE"], ["committed", "COMMITTED"], ["passed", "PASSED"], ["archived", "ARCHIVED"]];
  statuses.forEach(([key, label]) => { const b = el("button", "filter-chip" + (frStatus === key ? " on" : ""), label); b.onclick = () => { frStatus = key; renderAion(); }; bar.append(b); });
  const interest = el("select", "fr-filter");
  [["all", "ALL INTEREST"], ["high", "HIGH"], ["medium", "MEDIUM"], ["low", "LOW"], ["unknown", "UNKNOWN"]].forEach(([v, label]) => { const o = el("option", "", label); o.value = v; o.selected = frInterest === v; interest.append(o); });
  interest.onchange = () => { frInterest = interest.value; renderAion(); }; bar.append(interest);
  main.append(bar);

  const add = ghostInput("＋ firm or opportunity", "aion-add fr-add", async (firm) => {
    await frPost("/api/aion/fundraising/item", { firm }, "Opportunity added");
  });
  main.append(add);

  let rows = (frCache.opportunities || []).filter(frVisible);
  const table = el("div", "fr-table");
  const head = el("div", "fr-row fr-head");
  ["FIRM", "PEOPLE", "STATUS", "INTEREST", "AMOUNT", "LAST TOUCHPOINT", "NEXT STEP"].forEach((x) => head.append(el("span", "", x)));
  table.append(head);
  if (!rows.length) table.append(emptyRow("No fundraising opportunities match."));
  rows.forEach((op) => table.append(frRow(op)));
  main.append(table);

  if ((frCache.resources || []).length) {
    const res = el("div", "fr-resources"); res.append(el("span", "fr-mini-label", "RESOURCES"));
    frCache.resources.forEach((r) => { const a = el("a", "aion-src", r.title || r.url); a.href = r.url; a.target = "_blank"; a.rel = "noreferrer"; res.append(a); });
    main.append(res);
  }

  const selected = (frCache.opportunities || []).find((x) => x.id === frSel);
  if (window.mf && window.mf.phone()) {
    if (selected) {
      window.mfSheet.open((body) => renderFundraisingInspector(body, selected), {
        key: "fundraising",
        onClose: () => { if (frSel) { frSel = null; renderAion(); } },
        reopen: () => { if (!els.aionView.hidden && aionMode === "fundraising") renderAion(); },
      });
    } else {
      window.mfSheet.closeIf("fundraising");
    }
  } else if (selected) {
    renderFundraisingInspector(inspector, selected);
  } else {
    inspector.append(el("div", "aion-insp-empty", "select an opportunity — edits save as you go"));
  }
}

function frVisible(op) {
  if (frStatus === "open" && (op.archived || !["prospect", "active"].includes(op.status))) return false;
  if (frStatus === "archived" && !op.archived) return false;
  if (!["open", "all", "archived"].includes(frStatus) && (op.archived || op.status !== frStatus)) return false;
  if (frStatus === "all" && op.archived) return false;
  if (frInterest !== "all" && op.interest !== frInterest) return false;
  const q = frQuery.trim().toLowerCase();
  if (!q) return true;
  return [op.firm, op.introVia, op.lastTouchpoint, op.nextStep, op.notes].concat((op.people || []).map((p) => p.display)).join(" ").toLowerCase().includes(q);
}

function frRow(op) {
  const row = el("div", "fr-row" + (frSel === op.id ? " sel" : "") + (op.importReview ? " review" : ""));
  const firm = el("div", "fr-firm"); firm.append(el("span", "fr-firm-name", op.firm));
  if (op.importReview) firm.append(el("span", "fr-review", "REVIEW"));
  const people = el("div", "fr-people");
  (op.people || []).forEach((p) => { const b = el("button", "fr-person", p.display); b.onclick = (e) => { e.stopPropagation(); location.hash = "#/contacts/" + encodeURIComponent(p.key); }; people.append(b); });
  if (!(op.people || []).length && op.introVia) people.append(el("span", "fr-muted", op.introVia));
  const status = el("span", "fr-status " + op.status, op.status.toUpperCase());
  const interest = el("span", "fr-interest", (op.interest || "unknown").toUpperCase());
  const amount = el("span", "fr-money", op.amount ? money(op.amount) : "—");
  const touch = el("div", "fr-stack");
  touch.append(el("span", "", op.lastTouchpoint || "—"));
  if (op.lastTouchpointDate) touch.append(el("span", "fr-sub", "manual · " + op.lastTouchpointDate));
  if (op.computedLastTouchpoint) touch.append(el("span", "fr-sub", "contacts · " + op.computedLastTouchpoint));
  const next = el("div", "fr-stack"); next.append(el("span", "", op.nextStep || "—")); if (op.nextStepDue) next.append(el("span", "fr-sub", "due " + op.nextStepDue));
  row.append(firm, people, status, interest, amount, touch, next);
  row.onclick = () => { frSel = frSel === op.id ? null : op.id; renderAion(); };
  return row;
}

function money(v) { try { return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(v); } catch (_) { return "$" + v; } }

async function frPost(url, body, msg) {
  try {
    const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!r.ok) throw new Error(await r.text());
    frCache = await r.json();
    if (msg) showToast(msg);
    renderAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 140)); }
}

function renderFundraisingInspector(host, op) {
  const head = el("div", "aion-insp-head"); head.append(el("span", "aion-insp-label", "Fundraising"));
  const x = el("button", "aion-insp-x", "✕"); x.onclick = () => { frSel = null; renderAion(); }; head.append(x); host.append(head);
  const patch = (set) => frPost("/api/aion/fundraising/update/" + op.id, set);
  const field = (label, node) => { const f = el("div", "aion-insp-field fr-insp-field"); f.append(el("span", "aion-insp-flabel", label), node); host.append(f); };
  const text = (label, key, value, multiline) => { const n = el(multiline ? "textarea" : "input", "pp-in fr-in"); if (!multiline) n.type = "text"; n.value = value || ""; let old = n.value; n.onblur = () => { if (n.value !== old) patch({ [key]: n.value }); }; field(label, n); return n; };

  text("firm", "firm", op.firm);
  const status = el("select", "pp-in fr-in"); ["prospect", "active", "committed", "passed"].forEach((v) => { const o = el("option", "", v); o.value = v; o.selected = op.status === v; status.append(o); }); status.onchange = () => patch({ status: status.value }); field("status", status);
  const interest = el("select", "pp-in fr-in"); ["unknown", "high", "medium", "low"].forEach((v) => { const o = el("option", "", v); o.value = v; o.selected = op.interest === v; interest.append(o); }); interest.onchange = () => patch({ interest: interest.value }); field("interest", interest);
  const amount = el("input", "pp-in fr-in"); amount.type = "number"; amount.min = "0"; amount.step = "1000"; amount.value = op.amount || ""; amount.onblur = () => patch({ amount: amount.value }); field("amount", amount);

  const people = el("div", "fr-insp-people");
  (op.people || []).forEach((p) => { const chip = el("span", "fr-person-chip"); const open = el("button", "fr-person", p.display); open.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(p.key); }; const rm = el("button", "fr-person-rm", "×"); rm.title = "unlink from this opportunity"; rm.onclick = () => frPost("/api/aion/fundraising/person-remove/" + op.id, { key: p.key }); chip.append(open, rm); people.append(chip); });
  const addPerson = el("input", "pp-in fr-in"); addPerson.placeholder = "link or create a person…"; people.append(addPerson); const results = el("div", "fr-person-results"); people.append(results); let timer;
  addPerson.oninput = () => { clearTimeout(timer); timer = setTimeout(() => frPersonSearch(op, addPerson, results), 180); };
  field("people", people);
  text("intro via", "introVia", op.introVia);
  text("last touch", "lastTouchpoint", op.lastTouchpoint);
  const lastDate = el("input", "pp-in fr-in"); lastDate.type = "date"; lastDate.value = op.lastTouchpointDate || ""; lastDate.onchange = () => patch({ lastTouchpointDate: lastDate.value }); field("touch date", lastDate);
  if (op.computedLastTouchpoint) field("contacts", el("span", "aion-insp-ro", op.computedLastTouchpoint + " · latest linked interaction"));
  text("next step", "nextStep", op.nextStep);
  const due = el("input", "pp-in fr-in"); due.type = "date"; due.value = op.nextStepDue || ""; due.onchange = () => patch({ nextStepDue: due.value }); field("next due", due);
  text("notes", "notes", op.notes, true);
  if (op.importReview) { const done = el("button", "pill light", "Mark import reviewed"); done.onclick = () => patch({ importReview: false }); host.append(done); }
  const archive = el("button", "aion-insp-del", op.archived ? "restore opportunity" : "archive opportunity"); archive.onclick = () => frPost("/api/aion/fundraising/archive/" + op.id, { archived: !op.archived }, op.archived ? "Opportunity restored" : "Opportunity archived"); host.append(archive);
}

async function frPersonSearch(op, input, host) {
  host.innerHTML = ""; const q = input.value.trim(); if (!q) return;
  let d = { results: [] }; try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (_) {}
  (d.results || []).slice(0, 6).forEach((p) => { const b = el("button", "fr-person-result", p.display + (p.hasNote ? " · note" : " · no note")); b.onclick = () => frPost("/api/aion/fundraising/person/" + op.id, { key: p.key, display: p.display, notePath: p.hasNote ? (p.notePath || "") : "" }, "Person linked"); host.append(b); });
  const create = el("button", "fr-person-result create", "Create note-less contact “" + q + "”"); create.onclick = () => frPost("/api/aion/fundraising/person/" + op.id, { key: q.toLowerCase(), display: q }, "Note-less contact created"); host.append(create);
}
