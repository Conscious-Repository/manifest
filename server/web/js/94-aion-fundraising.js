// ---- AION / FUNDRAISING ----
// Private Manifest-only CRM. It intentionally fetches a route outside the
// AionLive contract used by portal.aion.bio.
let frCache = null;
let frStatus = "open";
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
  const search = el("input", "pp-in fr-search"); search.type = "search"; search.placeholder = "Search firms, people, next steps…"; search.value = frQuery;
  search.oninput = () => { frQuery = search.value; renderAion(); };
  bar.append(search);
  const statuses = [["open", "OPEN"], ["all", "ALL"], ["prospect", "PROSPECT"], ["active", "ACTIVE"], ["committed", "COMMITTED"], ["passed", "PASSED"], ["archived", "ARCHIVED"]];
  statuses.forEach(([key, label]) => { const b = el("button", "filter-chip" + (frStatus === key ? " on" : ""), label); b.onclick = () => { frStatus = key; renderAion(); }; bar.append(b); });
  main.append(bar);

  const add = ghostInput("＋ firm or opportunity", "aion-add fr-add", async (firm) => {
    await frPost("/api/aion/fundraising/item", { firm }, "Opportunity added");
  });
  main.append(add);

  let rows = (frCache.opportunities || []).filter(frVisible);
  const table = el("div", "fr-table");
  const head = el("div", "fr-row fr-head");
  ["FIRM", "PEOPLE", "STATUS", "LAST TOUCHPOINT", "NEXT STEP"].forEach((x) => head.append(el("span", "micro-label", x)));
  table.append(head);
  if (!rows.length) table.append(emptyRow("No fundraising opportunities match."));
  rows.forEach((op) => table.append(frRow(op)));
  main.append(table);

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
  const q = frQuery.trim().toLowerCase();
  if (!q) return true;
  return [op.firm, frSourceLabel(op), op.lastTouchpoint, op.nextStep, op.notes].concat((op.people || []).map((p) => p.display)).join(" ").toLowerCase().includes(q);
}

function frRow(op) {
  const row = el("div", "fr-row" + (frSel === op.id ? " sel" : "") + (op.importReview ? " review" : ""));
  const firm = el("div", "fr-firm"); firm.append(el("span", "fr-firm-name", op.firm));
  if (op.importReview) firm.append(el("span", "micro-label fr-review", "REVIEW"));
  const people = el("div", "fr-people");
  (op.people || []).forEach((p) => { const b = el("button", "fr-person-name fr-person", p.display); b.onclick = (e) => { e.stopPropagation(); location.hash = "#/contacts/" + encodeURIComponent(p.key); }; people.append(b); });
  if (!people.children.length) people.append(el("span", "fr-person-empty", "—"));
  const status = el("span", "micro-label fr-status " + op.status, op.status.toUpperCase());
  const touch = el("div", "fr-stack");
  touch.append(el("span", "", op.lastTouchpoint || "—"));
  if (op.lastTouchpointDate) touch.append(el("span", "fr-sub", "manual · " + op.lastTouchpointDate));
  if (op.computedLastTouchpoint) touch.append(el("span", "fr-sub", "contacts · " + op.computedLastTouchpoint));
  const next = el("div", "fr-stack"); next.append(el("span", "", op.nextStep || "—")); if (op.nextStepDue) next.append(el("span", "fr-sub", "due " + op.nextStepDue));
  row.append(firm, people, status, touch, next);
  row.onclick = () => { frSel = frSel === op.id ? null : op.id; renderAion(); };
  return row;
}

function money(v) { try { return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(v); } catch (_) { return "$" + v; } }

function frSourceLabel(op) {
  if (op.source && op.source.contact) return op.source.contact.display || op.source.contact.key || "";
  return (op.source && op.source.text) || "";
}

function frSetSourceText(op, value) {
  value = String(value || "").trim();
  return frPost("/api/aion/fundraising/update/" + op.id, { source: value ? { text: value } : null }, value ? "Source saved" : "Source cleared");
}

function frSetSourceContact(op, contact) {
  return frPost("/api/aion/fundraising/update/" + op.id, { source: { contact: {
    key: contact.key, display: contact.display, notePath: contact.notePath || "",
  } } }, "Source contact linked");
}

async function frContactMatches(query) {
  try {
    const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(query))).json();
    return (d.results || []).filter((p) => p.isPerson);
  } catch (_) { return []; }
}

async function frCommitTypedPerson(op, value) {
  const matches = await frContactMatches(value);
  const lower = value.toLowerCase();
  const exact = matches.find((p) => [p.key, p.display].some((v) => String(v || "").toLowerCase() === lower));
  if (exact) return frPost("/api/aion/fundraising/person/" + op.id, { key: exact.key, display: exact.display, notePath: exact.notePath || "" }, "Contact linked");
  return frPost("/api/aion/fundraising/person/" + op.id, { key: lower, display: value }, "Contact created and linked");
}

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
  const amount = el("input", "pp-in fr-in"); amount.type = "number"; amount.min = "0"; amount.step = "1000"; amount.value = op.amount || ""; amount.onblur = () => patch({ amount: amount.value }); field("amount", amount);

  const people = el("div", "fr-insp-people");
  (op.people || []).forEach((p) => { const chip = el("span", "fr-person-chip linked"); const open = el("button", "fr-person-name fr-person", p.display); open.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(p.key); }; const rm = el("button", "fr-person-rm", "×"); rm.title = "unlink from this opportunity"; rm.onclick = () => frPost("/api/aion/fundraising/person-remove/" + op.id, { key: p.key }); chip.append(open, rm); people.append(chip); });
  const addPerson = typeahead({
    placeholder: "find or add a person…",
    minChars: 1,
    onEnter: (value) => frCommitTypedPerson(op, value),
    suggest: async (q, add) => {
      const matches = await frContactMatches(q);
      const linked = new Set((op.people || []).map((p) => p.key));
      matches.filter((p) => !linked.has(p.key)).slice(0, 6).forEach((p) =>
        add(p.display, "contact", () => frPost("/api/aion/fundraising/person/" + op.id, { key: p.key, display: p.display, notePath: p.notePath || "" }, "Contact linked")));
      if (!matches.some((p) => [p.key, p.display].some((v) => String(v || "").toLowerCase() === addPerson.value().toLowerCase())))
        add("Add “" + addPerson.value() + "” as a new contact", "create", () => frCommitTypedPerson(op, addPerson.value()));
    },
  });
  people.append(addPerson.el);
  field("people", people);

  const source = el("div", "fr-source-editor");
  if (op.source && op.source.contact) {
    const p = op.source.contact;
    const chip = el("span", "fr-person-chip linked");
    const open = el("button", "fr-person-name fr-person", p.display || p.key);
    open.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(p.key); };
    const rm = el("button", "fr-person-rm", "×"); rm.title = "clear source"; rm.onclick = () => frSetSourceText(op, "");
    chip.append(open, rm); source.append(chip);
  } else if (op.source && op.source.text) {
    const chip = el("span", "fr-person-chip plain");
    chip.append(el("span", "fr-person-name fr-person-plain", op.source.text));
    const rm = el("button", "fr-person-rm", "×"); rm.title = "clear source"; rm.onclick = () => frSetSourceText(op, "");
    chip.append(rm); source.append(chip);
  }
  const sourceInput = typeahead({
    placeholder: frSourceLabel(op) ? "replace source…" : "contact, DM, cold intro…",
    minChars: 1,
    onEnter: (value) => frSetSourceText(op, value),
    suggest: async (q, add) => {
      const matches = await frContactMatches(q);
      matches.slice(0, 6).forEach((p) => add(p.display, "contact", () => frSetSourceContact(op, p)));
      add("Use “" + sourceInput.value() + "” as plain text", "text", () => frSetSourceText(op, sourceInput.value()));
    },
  });
  source.append(sourceInput.el);
  field("source", source);
  text("last touch", "lastTouchpoint", op.lastTouchpoint);
  const lastDate = el("input", "pp-in fr-in"); lastDate.type = "date"; lastDate.value = op.lastTouchpointDate || ""; lastDate.onchange = () => patch({ lastTouchpointDate: lastDate.value }); field("touch date", lastDate);
  if (op.computedLastTouchpoint) field("contacts", el("span", "aion-insp-ro", op.computedLastTouchpoint + " · latest linked interaction"));
  text("next step", "nextStep", op.nextStep);
  const due = el("input", "pp-in fr-in"); due.type = "date"; due.value = op.nextStepDue || ""; due.onchange = () => patch({ nextStepDue: due.value }); field("next due", due);
  text("notes", "notes", op.notes, true);
  if (op.importReview) { const done = el("button", "pill light", "Mark import reviewed"); done.onclick = () => patch({ importReview: false }); host.append(done); }
  const archive = el("button", "aion-insp-del", op.archived ? "restore opportunity" : "archive opportunity"); archive.onclick = () => frPost("/api/aion/fundraising/archive/" + op.id, { archived: !op.archived }, op.archived ? "Opportunity restored" : "Opportunity archived"); host.append(archive);
}
