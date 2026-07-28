// ---- CONTRACTORS sub-tab: the contractor DB, searchable by trade ----

const TRADES = ["masonry", "roofing", "plumbing", "electrical", "hvac", "framing",
  "drywall", "flooring", "painting", "concrete", "windows", "gc", "landscaping", "other"];
let contractorSearch = "";
let contractorTrade = "";

async function renderContractors() {
  const host = els.propertyContractors; host.hidden = false; host.innerHTML = "loading…";
  await ensureEntities(true);
  if (!propertyCache.length) {
    try { const d = await (await fetch("/api/properties")).json(); propertyCache = d.properties || []; } catch (e) {}
  }
  host.innerHTML = "";
  const list = (entitiesCache || {}).contractors || [];
  els.propertiesMeta.textContent = list.length + " contractors";

  const bar = el("div", "board-bar");
  const search = inputEl("search name…");
  search.classList.add("board-search");
  search.value = contractorSearch;
  search.addEventListener("input", () => { contractorSearch = search.value; body.replaceWith(body = table()); });
  bar.append(search);
  const tradesPresent = [...new Set(list.map((c) => c.trade).filter(Boolean))].sort();
  const sel = document.createElement("select");
  sel.className = "pp-in board-select";
  [["", "all trades"], ...tradesPresent.map((t) => [t, t])].forEach(([v, l]) => {
    const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o);
  });
  sel.value = contractorTrade;
  sel.onchange = () => { contractorTrade = sel.value; body.replaceWith(body = table()); };
  bar.append(sel);
  host.append(bar);

  // derived money per contractor from the ledgers (same source as autocomplete history)
  const stats = (name) => {
    const s = { props: new Set(), paid: 0, openBids: 0 };
    (propertyCache || []).forEach((p) => (p.ledger || []).forEach((r) => {
      const who = (r.contractor || r.vendor || "").trim().toLowerCase();
      if (who !== name.toLowerCase()) return;
      s.props.add(p.short || p.address || p.slug);
      if (r.type === "expense") s.paid += r.amount;
      if (r.type === "bid" && (r.status === "requested" || r.status === "received")) s.openBids++;
    }));
    return s;
  };

  const table = () => {
    const box = el("div", "contractor-table");
    const rows = list.filter((c) =>
      (!contractorSearch || c.name.toLowerCase().includes(contractorSearch.toLowerCase())) &&
      (!contractorTrade || c.trade === contractorTrade));
    if (!rows.length) { box.append(emptyRow("No contractors match.")); return box; }
    box.append(ppCols("cols-contractors", ["NAME", "TRADE", "PROPERTIES", "TOTAL PAID", "OPEN BIDS"]));
    rows.forEach((c) => {
      const s = stats(c.name);
      const row = el("div", "property-row cols-contractors");
      const nm = el("span", "property-addr", c.name);
      row.onclick = () => { _noteReturn = "#/properties/contractors"; openNoteByPath(c.path); };
      // inline trade picker — the backfill surface
      const tr = document.createElement("select");
      tr.className = "pp-in ct-trade";
      [["", "—"], ...TRADES.map((t) => [t, t])].forEach(([v, l]) => {
        const o = document.createElement("option"); o.value = v; o.textContent = l; tr.append(o);
      });
      if (c.trade && !TRADES.includes(c.trade)) { const o = document.createElement("option"); o.value = c.trade; o.textContent = c.trade; tr.append(o); }
      tr.value = c.trade || "";
      tr.onclick = (e) => e.stopPropagation();
      tr.onchange = async () => {
        try {
          await postJSONOk("/api/realestate/contractors/" + encodeURIComponent(c.slug), { trade: tr.value });
          c.trade = tr.value;
          showToast("Trade set: " + (tr.value || "—"));
        } catch (e) { showToast("Couldn't set trade"); }
      };
      row.append(nm, tr,
        el("span", "", [...s.props].slice(0, 3).join(" · ") + (s.props.size > 3 ? " +" + (s.props.size - 3) : "")),
        el("span", "pp-amt", s.paid ? fmtMoney(s.paid) : ""),
        el("span", "pp-amt", s.openBids ? String(s.openBids) : ""));
      box.append(row);
    });
    return box;
  };
  let body = table();
  host.append(body);
}
