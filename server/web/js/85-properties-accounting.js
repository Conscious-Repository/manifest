// ---- STATEMENT WORKBENCH (design §4) ----

let stmtRows = [];
let stmtFilter = "pending"; // pending shows pending+assigned+split

async function renderAccounting() {
  const host = els.propertyStatements; host.hidden = false; host.innerHTML = "loading…";
  let d;
  try { d = await (await fetch("/api/realestate/statements")).json(); }
  catch (e) { host.innerHTML = ""; host.append(emptyRow("Statements unavailable.")); return; }
  stmtRows = d.rows || [];
  try { const pd = await (await fetch("/api/properties")).json(); propertyCache = pd.properties || []; } catch (e) {}
  host.innerHTML = "";

  // RECONCILE — every done work item still missing evidence (bank link OR
  // receipt), across all properties. The tab's reason to exist.
  const recon = [];
  (propertyCache || []).forEach((p) => (p.work || []).forEach((st) => (st.todos || []).forEach((td) => {
    if (td.checked && (td.unreconciled || 0) > 0) recon.push({ p, st, td });
  })));
  if (recon.length) {
    host.append(el("div", "pp-section-head", "RECONCILE — " + recon.length + " completed expense" + (recon.length === 1 ? "" : "s") + " need evidence"));
    const box = el("div", "recon-list");
    box.append(ppCols("cols-recon", ["PROPERTY", "TASK", "FIRM PRICE", "UNRECONCILED", ""]));
    recon.forEach(({ p, st, td }) => {
      const firm = (td.bids || []).filter((b) => b.status === "accepted").reduce((a, b) => a + b.amount, 0);
      const row = el("div", "property-row cols-recon");
      const addr = el("span", "property-addr", p.short || p.address || p.slug);
      row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
      const acts = el("span", "recon-acts");
      const rec = el("button", "pill light", "📎 receipt");
      rec.title = "attach a receipt — reconciles without a bank transaction";
      const filePick = document.createElement("input");
      filePick.type = "file"; filePick.hidden = true;
      filePick.onchange = async () => {
        const bid = (td.bids || []).find((b) => b.status === "accepted");
        if (!filePick.files[0] || !bid) return;
        const fd = new FormData();
        fd.append("file", filePick.files[0]);
        fd.append("original", JSON.stringify(bid.row));
        try {
          const res = await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/receipt", { method: "POST", body: fd });
          if (!res.ok) throw new Error(await res.text());
          showToast("Receipt attached — reconciled");
          renderAccounting();
        } catch (e) { showToast(("Receipt failed: " + (e.message || "")).slice(0, 80)); }
      };
      rec.onclick = (e) => { e.stopPropagation(); filePick.click(); };
      const link = el("button", "pill light", "link ↓");
      link.title = "assign the matching bank row in the queue below (tether it to this task)";
      link.onclick = (e) => {
        e.stopPropagation();
        const q = host.querySelector(".stmt-list") || host.lastElementChild;
        if (q) q.scrollIntoView({ behavior: "smooth" });
      };
      acts.append(rec, filePick, link);
      row.append(addr, el("span", "", st.text + " · " + td.text),
        el("span", "pp-amt", fmtMoney(firm)), el("span", "pp-amt unrec-amt", fmtMoney(td.unreconciled)), acts);
      box.append(row);
    });
    host.append(box);
  }

  const counts = { pending: 0, assigned: 0, split: 0, applied: 0, skipped: 0 };
  stmtRows.forEach((r) => { counts[r.state] = (counts[r.state] || 0) + 1; });
  const units = new Map();
  stmtRows.forEach((r) => {
    const k = (r.statement || "earlier") + " · imported " + (r.imported || "");
    const u = units.get(k) || { open: false };
    if (r.state !== "applied" && r.state !== "skipped") u.open = true;
    units.set(k, u);
  });
  const openCount = [...units.values()].filter((u) => u.open).length;
  els.propertiesMeta.textContent = (openCount ? openCount + " statement" + (openCount === 1 ? "" : "s") + " open · " : "") +
    counts.pending + " unassigned" + (d.lastImport ? " · last import " + d.lastImport : "");

  // upload zone (always present)
  const drop = el("div", "pp-dropzone", "drop a bank csv — or click to pick");
  const pick = document.createElement("input");
  pick.type = "file"; pick.accept = ".csv,text/csv"; pick.hidden = true;
  drop.onclick = () => pick.click();
  const mapHost = el("div", "stmt-maphost");
  const doUpload = async (file) => {
    if (!file) return;
    const fd = new FormData();
    fd.append("file", file);
    drop.textContent = "parsing…";
    try {
      const res = await fetch("/api/realestate/statements/upload", { method: "POST", body: fd });
      if (!res.ok) throw new Error(await res.text());
      renderStmtMapping(mapHost, await res.json());
    } catch (e) { showToast("Couldn't parse csv"); }
    drop.textContent = "drop a bank csv — or click to pick";
  };
  pick.onchange = () => { doUpload(pick.files[0]); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); doUpload(e.dataTransfer.files[0]); });
  host.append(drop, pick, mapHost);

  // grouping auto-suggest (pass-5): pending rows from one vendor whose sum
  // matches an accepted bid (±1%) → one-click tether-all. Merge = grouping,
  // never destruction — the rows stay verbatim, they just share the tether.
  const pendingRows = stmtRows.filter((r) => r.state === "pending" && r.vendor);
  const byVendor = new Map();
  pendingRows.forEach((r) => {
    const k = r.vendor.toLowerCase();
    if (!byVendor.has(k)) byVendor.set(k, []);
    byVendor.get(k).push(r);
  });
  for (const [vk, rows] of byVendor) {
    if (rows.length < 2) continue;
    const sum = rows.reduce((s, r) => s + r.amount, 0);
    let match = null;
    (propertyCache || []).forEach((p) => {
      (p.ledger || []).forEach((lr) => {
        if (lr.type === "bid" && lr.status === "accepted" && lr.workId &&
            (lr.contractor || lr.vendor || "").toLowerCase().includes(vk.slice(0, 8)) &&
            Math.abs(lr.amount - sum) <= lr.amount * 0.01) {
          match = { p, lr };
        }
      });
    });
    if (!match) continue;
    const hint = el("div", "stmt-suggest");
    hint.append(el("span", "", rows.length + " rows sum " + fmtMoney(sum) + " = accepted bid on " +
      match.lr.workId.split("/").pop() + " (" + (match.p.short || match.p.address || match.p.slug) + ")"));
    const go = el("button", "stmt-hint stmt-echo", "group →");
    go.onclick = async () => {
      for (const r of rows) {
        await patchStmt(r, { category: r.category || match.lr.category, assignments: [{ slug: match.p.slug, amount: r.amount, workId: match.lr.workId }] }, true);
      }
      renderAccounting();
    };
    hint.append(go);
    host.append(hint);
  }

  // state filter chips
  const chips = el("div", "stmt-chips");
  [["pending", "PENDING"], ["applied", "APPLIED"], ["skipped", "SKIPPED"]].forEach(([val, label]) => {
    const c = el("button", "filter-chip" + (stmtFilter === val ? " on" : ""), label);
    c.onclick = () => { stmtFilter = val; renderAccounting(); };
    chips.append(c);
  });
  host.append(chips);

  // rows grouped by statement label
  const list = el("div", "stmt-list");
  const show = stmtRows.filter((r) =>
    stmtFilter === "pending" ? (r.state === "pending" || r.state === "assigned" || r.state === "split")
      : r.state === stmtFilter);
  const byStmt = new Map();
  show.forEach((r) => {
    const k = (r.statement || "earlier") + " · imported " + (r.imported || "");
    if (!byStmt.has(k)) byStmt.set(k, []);
    byStmt.get(k).push(r);
  });
  if (!show.length) list.append(emptyRow(stmtFilter === "pending" ? "Nothing pending — drop a statement above." : "Nothing here."));
  // statement units: a statement is OPEN until every row is applied or
  // dismissed-with-reason — x/y progress on each group header
  const unitOf = (label) => {
    const all = stmtRows.filter((r) => ((r.statement || "earlier") + " · imported " + (r.imported || "")) === label);
    const done = all.filter((r) => r.state === "applied" || r.state === "skipped").length;
    return { done, total: all.length, open: done < all.length };
  };
  for (const [label, rows] of byStmt) {
    const u = unitOf(label);
    const head = el("div", "pp-section-head stmt-head");
    head.append(el("span", "", label.toUpperCase()));
    head.append(el("span", "stmt-progress" + (u.open ? " open" : ""),
      (u.open ? "OPEN · " : "CLOSED · ") + u.done + "/" + u.total + " reconciled"));
    list.append(head);
    list.append(ppCols("cols-stmt", ["DATE", "DESCRIPTION", "AMOUNT", "CATEGORY", "PROPERTY", "STATE"]));
    rows.forEach((r) => list.append(stmtRowEl(r)));
  }
  host.append(list);

  // sticky apply footer
  const applicable = stmtRows.filter((r) => r.state === "assigned" || r.state === "split");
  if (applicable.length) {
    const bar = el("div", "dirty-bar");
    bar.append(el("span", "dirty-label",
      stmtRows.filter((r) => r.state === "assigned").length + " ASSIGNED · " +
      stmtRows.filter((r) => r.state === "split").length + " SPLIT · " +
      counts.pending + " PENDING"));
    const apply = el("button", "pill", "apply " + applicable.length + " rows");
    apply.onclick = async () => {
      apply.disabled = true;
      try {
        const res = await postJSONOk("/api/realestate/statements/apply", { ids: applicable.map((r) => r.id) });
        showToast("Applied " + res.applied + " rows (" + res.lines + " lines across " + res.properties + " properties)");
        renderAccounting();
      } catch (e) { showToast("Apply failed: " + (e.message || "").slice(0, 80)); apply.disabled = false; }
    };
    bar.append(apply);
    host.append(bar);
  }
}

// renderStmtMapping: column mapping over an uploaded csv → ingest to the lot.
function renderStmtMapping(host, pre) {
  host.innerHTML = "";
  const panel = el("div", "import-panel");
  panel.append(el("div", "pp-section-head", "MAP COLUMNS · " + pre.label + (pre.remembered ? " (remembered)" : "")));
  const selects = {};
  const mapRow = el("div", "import-maprow");
  ["date", "amount", "vendor", "note"].forEach((field) => {
    const lab = el("label", "portal-field");
    lab.append(el("span", "portal-field-label", field));
    const sel = selectEl(field === "note" ? ["—", ...pre.headers] : pre.headers);
    if (pre.mapping && pre.mapping[field]) sel.value = pre.mapping[field];
    selects[field] = sel;
    lab.append(sel);
    mapRow.append(lab);
  });
  const flip = document.createElement("input"); flip.type = "checkbox";
  const flipLab = el("label", "import-flip"); flipLab.append(flip, el("span", "", " debits are negative (flip sign)"));
  mapRow.append(flipLab);
  // pass-5: every upload binds to the paying entity (remembered per source label)
  const entLab = el("label", "portal-field");
  entLab.append(el("span", "portal-field-label", "paying entity"));
  const entAC = recordAutocomplete("entity", "entity account…");
  if (pre.entity) entAC.setValue(pre.entity);
  entLab.append(entAC.el);
  mapRow.append(entLab);
  panel.append(mapRow);
  const ingest = el("button", "pill", "add to workbench");
  const cancel = el("button", "pill light", "✕");
  cancel.onclick = () => { host.innerHTML = ""; };
  ingest.onclick = async () => {
    const col = (f) => pre.headers.indexOf(selects[f].value);
    const di = col("date"), ai = col("amount"), vi = col("vendor");
    const ni = selects.note.value === "—" ? -1 : pre.headers.indexOf(selects.note.value);
    const rows = pre.rows.map((raw) => {
      let amt = parseFloat(String(raw[ai] || "").replace(/[$,]/g, "")) || 0;
      if (flip.checked) amt = -amt;
      return {
        date: normDate(raw[di] || ""), amount: Math.round(amt * 100) / 100,
        vendor: (raw[vi] || "").trim(), note: ni >= 0 ? (raw[ni] || "").trim() : "",
      };
    }).filter((r) => r.date && r.amount !== 0); // negatives = deposits → inflow rows
    if (!entAC.value()) { showToast("Pick the paying entity first"); entAC.focus(); return; }
    ingest.disabled = true;
    try {
      const mapping = { date: selects.date.value, amount: selects.amount.value, vendor: selects.vendor.value, note: selects.note.value };
      const res = await postJSONOk("/api/realestate/statements/ingest",
        { label: pre.label, entity: entAC.value(), signature: pre.signature, mapping, rows });
      showToast("Added " + res.added + " rows (" + res.duplicates + " duplicates skipped)");
      host.innerHTML = "";
      renderAccounting();
    } catch (e) { showToast("Ingest failed"); ingest.disabled = false; }
  };
  const foot = el("div", "import-foot");
  foot.append(ingest, cancel);
  panel.append(foot);
  host.append(panel);
}

// stmtRowEl: one workbench row — category input, property typeahead, split
// block, skip link, vendor-echo bulk hint.
function stmtRowEl(r) {
  const wrap = el("div", "stmt-wrap");
  const row = el("div", "stmt-row state-" + r.state);
  row.append(el("span", "import-date", r.date));
  row.append(el("span", "stmt-vendor", (r.inflow ? "↓ " : "") + r.vendor + (r.note ? "  · " + r.note : "")));
  row.append(el("span", "pp-amt" + (r.inflow ? " inflow" : ""), (r.inflow ? "+" : "") + fmtMoney(r.amount)));

  const readOnly = r.state === "applied";
  if (readOnly) {
    row.append(el("span", "", r.category || ""));
    row.append(el("span", "", (r.assignments || []).map((a) => a.slug).join(" · ")));
    row.append(el("span", "stmt-state", r.state));
    wrap.append(row);
    return wrap;
  }

  const cat = inputEl("category");
  cat.classList.add("import-cat");
  cat.value = r.category || "";
  cat.addEventListener("change", () => patchStmt(r, { category: cat.value }));
  row.append(cat);

  const propCell = el("span", "stmt-prop");
  const single = (r.assignments || []).length === 1 ? r.assignments[0] : null;
  const isAdmin = single && single.slug.startsWith("admin:");
  propCell.append(propertyTypeahead("property…", (p) => {
    patchStmt(r, { assignments: [{ slug: p.slug, amount: r.amount, workId: (single && single.workId) || "",
      cat: (single && single.cat) || (r.inflow ? "rent" : "") }] });
  }, single && !isAdmin ? single.slug : ""));
  // budget category lane: hard (default, tetherable) | soft | acquisition;
  // inflow rows instead pick what KIND of money arrived (rent | capital)
  if (single && !isAdmin) {
    const catSel = document.createElement("select");
    catSel.className = "pp-in lg-cat";
    catSel.title = r.inflow ? "income kind" : "budget category (soft = interest · taxes · insurance · utilities)";
    (r.inflow
      ? [["rent", "rent"], ["capital", "capital"]]
      : [["", "hard"], ["soft", "soft"], ["acquisition", "acquisition"]]).forEach(([v, l]) => {
      const o = document.createElement("option"); o.value = v; o.textContent = l; catSel.append(o);
    });
    catSel.value = single.cat || "";
    catSel.onchange = () => patchStmt(r, { assignments: [{ slug: single.slug, amount: single.amount,
      workId: catSel.value ? "" : (single.workId || ""), cat: catSel.value }] });
    propCell.append(catSel);
  }
  // work tether: once a real property is assigned, offer its open todos (hard lane only)
  if (single && !isAdmin && !single.cat) {
    const prop = propertyCache.find((p) => p.slug === single.slug);
    if (prop && (prop.work || []).length) {
      const sel = document.createElement("select");
      sel.className = "pp-in lg-work";
      sel.title = "work tether";
      const opts = [["", "⚲ —"]];
      (prop.work || []).forEach((st) => {
        (st.todos || []).forEach((td) => { if (!td.checked || td.id === single.workId) opts.push([td.id, st.text + " · " + td.text]); });
        opts.push([st.id, st.text + " (stage)"]);
      });
      opts.forEach(([v, l]) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); });
      sel.value = single.workId || "";
      sel.onchange = () => patchStmt(r, { assignments: [{ slug: single.slug, amount: single.amount, workId: sel.value }] });
      propCell.append(sel);
    }
  }
  const adminBtn = el("button", "stmt-split-btn", isAdmin ? "admin ✓" : "admin");
  adminBtn.title = "assign to an entity's admin ledger instead of a property";
  adminBtn.onclick = () => toggleAdminForm(wrap, r);
  propCell.append(adminBtn);
  const splitBtn = el("button", "stmt-split-btn", "split ⑂");
  splitBtn.onclick = () => renderSplitBlock(wrap, r);
  propCell.append(splitBtn);
  row.append(propCell);

  const stateCell = el("span", "stmt-state st-" + r.state, r.state);
  row.append(stateCell);
  wrap.append(row);

  // quiet second line: remembered hint · vendor echo · skip
  const hints = el("div", "stmt-hints");
  if (r.remembered) hints.append(el("span", "stmt-hint", "↳ remembered"));
  if (r.state === "assigned" || r.state === "split") {
    const others = stmtRows.filter((o) => o.state === "pending" && o.vendor && o.vendor === r.vendor);
    if (others.length) {
      const echo = el("button", "stmt-hint stmt-echo", "apply to " + others.length + " more from " + r.vendor + " →");
      echo.onclick = async () => {
        for (const o of others) {
          await patchStmt(o, { category: r.category, assignments: r.assignments }, true);
        }
        renderAccounting();
      };
      hints.append(echo);
    }
  }
  // dismiss-with-reason: every statement item must end assigned or dismissed
  if (r.state === "skipped") {
    hints.append(el("span", "stmt-hint", "dismissed: " + (r.reason || "personal")));
    const un = el("button", "stmt-hint", "restore");
    un.onclick = () => patchStmt(r, { state: "pending", reason: "" });
    hints.append(un);
  } else {
    const dis = el("button", "stmt-hint", "dismiss…");
    dis.onclick = () => {
      if (hints.querySelector(".dismiss-picker")) { hints.querySelector(".dismiss-picker").remove(); return; }
      const pick = el("span", "dismiss-picker");
      ["personal", "transfer", "duplicate"].forEach((why) => {
        pick.append(quietBtn(why, () => patchStmt(r, { state: "skipped", reason: why })));
      });
      pick.append(quietBtn("other…", () => {
        const why = inputEl("why?");
        why.classList.add("est-in");
        why.addEventListener("keydown", (ev) => {
          if (ev.key === "Enter" && why.value.trim()) patchStmt(r, { state: "skipped", reason: "other: " + why.value.trim() });
          else if (ev.key === "Escape") why.remove();
        });
        pick.append(why);
        why.focus();
      }));
      hints.append(pick);
    };
    hints.append(dis);
  }
  wrap.append(hints);

  if ((r.assignments || []).length > 1) renderSplitBlock(wrap, r, true);
  return wrap;
}

// toggleAdminForm: the admin lane — entity + category (from that entity's
// admin-categories list) → assignment slug "admin:<entity-slug>".
async function toggleAdminForm(wrap, r) {
  let form = wrap.querySelector(".admin-form");
  if (form) { form.remove(); return; }
  await ensureEntities();
  form = el("div", "stmt-splits admin-form");
  const entAC = recordAutocomplete("entity", "entity…", (rec) => {
    catSel.innerHTML = "";
    const ent = (entitiesCache.entities || []).find((e) => e.slug === rec.slug || e.name === rec.name);
    (((ent || {}).adminCategories) || ["admin"]).forEach((c) => {
      const o = document.createElement("option"); o.value = c; o.textContent = c; catSel.append(o);
    });
  });
  if (r.entity) entAC.setValue(r.entity);
  const catSel = document.createElement("select");
  catSel.className = "pp-in";
  const label = inputEl("label (optional)");
  const set = el("button", "pill lg-add", "assign to admin");
  set.onclick = async () => {
    const ent = entAC.value();
    if (!ent) { showToast("Pick the entity"); return; }
    const slugified = ent.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    await patchStmt(r, {
      category: catSel.value || r.category || "admin",
      assignments: [{ slug: "admin:" + slugified, amount: r.amount }],
    }, true);
    if (label.value.trim()) await patchStmt(r, { category: catSel.value + " · " + label.value.trim() }, true);
    renderAccounting();
  };
  form.append(entAC.el, catSel, label, set);
  wrap.append(form);
}

async function patchStmt(r, patch, silent) {
  try {
    await postJSONOk("/api/realestate/statements/row", { id: r.id, ...patch });
    if (!silent) renderAccounting();
  } catch (e) { showToast("Couldn't update row"); }
}

// renderSplitBlock: inline allocation sub-rows — property + amount each, ÷
// evenly, live remainder (nonzero keeps the row pending).
function renderSplitBlock(wrap, r, initialOnly) {
  let block = wrap.querySelector(".stmt-splits");
  if (block) { if (initialOnly) return; block.remove(); return; }
  block = el("div", "stmt-splits");
  const allocs = (r.assignments || []).length ? r.assignments.map((a) => ({ ...a })) : [{ slug: "", amount: r.amount }];
  const render = () => {
    block.innerHTML = "";
    allocs.forEach((a, i) => {
      const line = el("div", "stmt-split-line");
      line.append(propertyTypeahead("property…", (p) => { a.slug = p.slug; commit(); }, a.slug));
      const amt = inputEl("amount");
      amt.type = "number"; amt.step = "0.01"; amt.value = a.amount || "";
      amt.addEventListener("change", () => { a.amount = parseFloat(amt.value) || 0; commit(); });
      line.append(amt);
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { allocs.splice(i, 1); commit(); };
      line.append(x);
      block.append(line);
    });
    const foot = el("div", "stmt-split-foot");
    const add = el("button", "o-ghost", "＋ property");
    add.onclick = () => { allocs.push({ slug: "", amount: 0 }); render(); };
    const even = el("button", "stmt-hint", "÷ evenly");
    even.onclick = () => {
      const per = Math.floor((r.amount / allocs.length) * 100) / 100;
      allocs.forEach((a, i) => { a.amount = i === allocs.length - 1 ? Math.round((r.amount - per * (allocs.length - 1)) * 100) / 100 : per; });
      commit();
    };
    const sum = allocs.reduce((s, a) => s + (a.amount || 0), 0);
    const rem = Math.round((r.amount - sum) * 100) / 100;
    const remEl = el("span", "stmt-remainder" + (Math.abs(rem) < 0.01 ? " ok" : " bad"),
      "$" + rem.toFixed(2) + " remaining" + (Math.abs(rem) < 0.01 ? " ✓" : ""));
    foot.append(add, even, remEl);
    block.append(foot);
  };
  const commit = async () => {
    const clean = allocs.filter((a) => a.slug);
    await patchStmt(r, { assignments: clean }, true);
    r.assignments = clean;
    render();
  };
  render();
  wrap.append(block);
}
