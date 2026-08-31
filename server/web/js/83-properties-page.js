// ---- PROPERTY PAGE — Revision 3: one property, one scroll ----
// Address + status select · BUDGET/SPENT strip (BUDGET opens the underwrite
// editor — the plan inputs behind the figure) · TODOS (composer always
// present; selecting a row opens the assignment inspector) · LEDGER (rows
// edit in place, ✕ deletes, a composer adds) · side card: parcel outline +
// OWNER of record (assessor-stamped frontmatter, editable). Money: budget
// derives from source.json underwrite + `## work` est; spent from the ledger.
// the inspector's selection. Tasks key on their task id (that is what
// /api/tasks/* takes); decisions key on the work-tree node id — decisions are
// not in the flat p.tasks projection, so they restore by walking p.work.
let propSel = null;       // {kind:"task"|"decision", id}
let propDecidedOpen = false; // the "decided · N" fold
let propPageSlug = null;  // page-local UI state resets when this changes
let propPageEls = null;   // the page's stable frame — see propPageSkeleton
const propBidsOpen = {};  // node id → its open-bids list is expanded
let propUWOpen = false;   // underwrite editor visible
let propLedgerEdit = -1;  // index of the ledger row being edited (-1 none)
let propLedgerAdd = false;
let propGeoCache = {};    // slug → geo features (parcel polygon)

// propPageSkeleton — the page's stable frame. It is rebuilt only when the
// property changes (or something tore the DOM out from under us); a data
// re-render refills the head and the main column and leaves the RAIL alone.
// Every edit on this page re-renders it, and the rail holds the map and the
// inspector: rebuilding it meant tearing down and re-creating Leaflet on every
// keystroke-commit, and would destroy an open inspector mid-edit.
function propPageSkeleton(host, slug) {
  if (propPageEls && propPageEls.slug === slug && propPageEls.host === host && propPageEls.cols.isConnected) {
    return propPageEls;
  }
  host.innerHTML = "";
  const headWrap = el("div", "pp3-headwrap");
  const cols = el("div", "pp3-cols");
  const main = el("div", "pp3-main");
  const side = el("div", "pp3-side");
  const thumbHost = el("div", "pp3-thumb-host");
  const ownerHost = el("div", "pp3-owner-host");
  const inspSlot = el("div", "pp3-insp-slot");
  side.append(thumbHost, ownerHost, inspSlot);
  cols.append(main, side);
  host.append(headWrap, cols);
  propPageEls = { slug, host, headWrap, cols, main, side, thumbHost, ownerHost, inspSlot };
  return propPageEls;
}

async function renderPropertyPage(slug) {
  const host = els.propertyPage;
  if (slug !== propPageSlug) {
    propPageSlug = slug;
    propUWOpen = false; propLedgerEdit = -1; propLedgerAdd = false;
  }
  // the ledger editor's bid/contract picker + receipt links need the
  // contracts cache — warm it before first paint
  if (typeof reContractsCache !== "undefined" && reContractsCache === null) await loadReContracts();
  const p = propertyCache.find((x) => x.slug === slug);
  if (!p) { propPageEls = null; host.innerHTML = ""; host.append(el("div", "pp-empty", "Property not found.")); return; }
  const pg = propPageSkeleton(host, slug);
  pg.headWrap.innerHTML = "";
  pg.main.innerHTML = "";
  pg.ownerHost.innerHTML = "";
  pg.ownerHost.append(ownerCard(p));
  mountThumbOnce(pg.thumbHost, slug);

  // title row: address + status select
  const head = el("div", "pp3-head");
  const title = el("h2", "pp3-title", p.short || p.address || p.slug);
  title.title = p.address || "";
  head.append(title, statusSelect(p, () => renderProperties()));
  const openNote = el("button", "pp3-note", "note ↗");
  openNote.title = "open the record (⌘/ edits raw)";
  openNote.onclick = () => { location.hash = "#/note/" + encodeURIComponent(p.path); };
  head.append(openNote);
  pg.headWrap.append(head);

  // owner line (RE spec §2 OWNER): the books it lands on; the seller while
  // acquiring reads in ink. Click-to-edit both.
  const ownerLine = el("div", "pp3-owner-entline" + (p.from ? " acquiring" : ""));
  const entLabel = el("span", "pp3-owner-ent", p.entity ? p.entity : "＋ set the entity (its books)");
  entLabel.title = "the entity whose books this lands on";
  entLabel.onclick = () => {
    const v = prompt("Entity (its books):", p.entity || "");
    if (v !== null) propFieldSave(p.slug, "entity", v.trim());
  };
  ownerLine.append(entLabel);
  if (p.from) {
    ownerLine.append(el("span", "pp3-owner-from", " · acquiring from " + p.from));
  }
  const fromBtn = el("button", "pp3-owner-frombtn", p.from ? "edit" : "＋ acquiring from…");
  fromBtn.onclick = () => {
    const v = prompt("Seller (empty once closed):", p.from || "");
    if (v !== null) propFieldSave(p.slug, "from", v.trim());
  };
  ownerLine.append(fromBtn);
  pg.headWrap.append(ownerLine);

  const main = pg.main;

  // BUDGET · SPENT — two mono figures between hairlines
  const pm = projMoney(p);
  const strip = el("div", "pp3-strip");
  const cell = (label, val, cls) => {
    const c = el("div", "pp3-cell");
    c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val" + (cls ? " " + cls : ""), val));
    return c;
  };
  const bCell = cell("BUDGET", pm.budget ? fmtMoney(pm.budget) : "—");
  bCell.classList.add("click");
  bCell.title = "edit the underwrite (plan inputs)";
  bCell.onclick = () => { propUWOpen = !propUWOpen; renderPropertyPage(slug); };
  strip.append(bCell);
  const sCell = cell("SPENT", pm.paid ? fmtMoney(pm.paid) : "—", pm.over ? "over" : "");
  sCell.title = p.acq === "under-contract"
    ? "cash out the door. This deal has not closed, so its purchase price is committed, not spent."
    : "cash out the door — ledger expenses, plus the purchase price once the deal closed";
  strip.append(sCell);
  // the accrual only earns a cell when it actually differs from cash
  if (pm.recognized > pm.paid) {
    const rCell = cell("RECOGNIZED", fmtMoney(pm.recognized));
    rCell.title = "spent plus work done at a firm price that has no expense row yet";
    strip.append(rCell);
  }
  main.append(strip);

  if (propUWOpen) {
    const uw = el("div", "pp3-uw");
    uw.append(el("div", "pp3-uw-loading", "loading underwrite…"));
    main.append(uw);
    renderUnderwrite(p, uw);
  }

  // OPERATING — the stabilized lane (money-workbench v2): actual rent in vs
  // operating expenses out, monthly, fed by income rows + [cat:: operating]
  // expenses filed from the $ tab. Renders whenever the ledger has operating
  // money (phone included — this is the leased property's living surface).
  if (p.operating) main.append(operatingSection(p));

  // DECISIONS lane (overhaul decision 9): every open [decision::] node in the
  // tree surfaces here with its rock context; the lines themselves stay under
  // their rock/milestone in the file. Never ages; resolves with a note.
  const decisions = [];
  (p.work || []).forEach((st) => walkNodes(st, st.tasks, (rock, n) => { if (n.decision) decisions.push({ rock, n }); }));
  const openDecs = decisions.filter((d) => !d.n.checked);
  const decidedDecs = decisions.filter((d) => d.n.checked);
  if (decisions.length) {
    // the same row the RE backlog uses — one decision shape in the app. The ◇
    // is a glyph, not a button: a decision is resolved in the inspector, where
    // the outcome is typed, not through a browser prompt.
    const lane = el("div", "pp3-sec");
    const dh = el("div", "pp3-sec-head");
    dh.append(el("span", "pp3-sec-title", "◇ DECISIONS"),
      el("span", "pp3-sec-count", openDecs.length + " open · " + decidedDecs.length + " decided"));
    lane.append(dh);
    const decRow = ({ rock, n }) => {
      const row = el("div", "aion-dec-row" + (n.checked ? " decided" : "") + (propSelIs("decision", n.id) ? " sel" : ""));
      row.dataset.selKey = "decision:" + n.id;
      row.append(el("span", "aion-dec-glyph", "◇"));
      const col = el("div", "aion-main");
      col.append(el("div", "aion-dec-text", n.text));
      const bits = [];
      if (rock && rock.text) bits.push(rock.text);
      if (n.owner) bits.push("@" + assigneeName(n.owner));
      if (n.checked && n.resolution) bits.push("→ " + n.resolution);
      col.append(el("div", "aion-item-meta", bits.join(" · ")));
      row.append(col);
      row.append(el("span", "aion-status " + (n.checked ? "closed" : "open"), n.checked ? "DECIDED" : "OPEN"));
      row.onclick = () => propSelect(p, { kind: "decision", id: n.id, node: n, rock });
      return row;
    };
    openDecs.forEach((d) => lane.append(decRow(d)));
    if (decidedDecs.length) {
      const fold = el("button", "aion-done-toggle", (propDecidedOpen ? "▾" : "▸") + " decided · " + decidedDecs.length);
      fold.onclick = () => { propDecidedOpen = !propDecidedOpen; renderPropertyPage(slug); };
      lane.append(fold);
      if (propDecidedOpen) decidedDecs.forEach((d) => lane.append(decRow(d)));
    }
    main.append(lane);
  }

  // ROCKS — the tree: rock → milestone → task/decision (overhaul decision 1).
  // Rocks carry [done-by::] dates — the rock list IS the schedule. Rocks are
  // NOT sequential (owner call 2026-08-12): no current marker, any rock takes
  // tasks, rocks progress in parallel.
  const stagesSec = el("div", "pp3-sec");
  const sh = el("div", "pp3-sec-head");
  let openCount = 0;
  (p.work || []).forEach((st) => walkNodes(st, st.tasks, (_, n) => { if (!n.checked && !n.milestone && !n.decision) openCount++; }));
  sh.append(el("span", "pp3-sec-title", "ROCKS"), el("span", "pp3-sec-count", openCount + " open"));
  stagesSec.append(sh);
  const stages = p.work || [];
  const today = new Date().toISOString().slice(0, 10);
  stages.forEach((st) => {
    // the goals page's rock block: rock 14/500 over 13px milestones over 13px
    // tasks, one indent ladder. `current` = has open work, `stalled` = past its
    // done-by date — goals' own state semantics rather than a local late chip.
    let stOpen = 0;
    walkNodes(st, st.tasks, (_, n) => { if (!n.checked && !n.milestone && !n.decision) stOpen++; });
    const stalled = !st.checked && st.doneBy && st.doneBy < today;
    const block = el("div", "go-rock" + (st.checked ? " done" : "") + (stalled ? " stalled" : stOpen ? " current" : ""));
    const line = el("div", "go-rock-line");
    // glyph doubles as the done toggle (server stamps [done:: date] on check)
    const glyph = el("button", "go-check" + (st.checked ? " on" : ""), st.checked ? "✓" : "○");
    glyph.title = st.checked ? "mark rock not done" : "mark rock done";
    glyph.onclick = (e) => { e.stopPropagation(); propWorkOp(p, { op: "check", id: st.id, checked: !st.checked }); };
    line.append(glyph);
    // name — click to rename in place
    const name = el("span", "go-rock-name" + (st.checked ? " done" : ""), st.text || "");
    name.title = "click to rename";
    name.onclick = () => {
      const input = inputEl(""); input.value = st.text || ""; input.classList.add("work-edit");
      input.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter" && input.value.trim()) propWorkOp(p, { op: "edit", id: st.id, text: input.value.trim() });
        else if (ev.key === "Escape") input.replaceWith(name);
      });
      input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(name); });
      name.replaceWith(input); input.focus();
    };
    line.append(name);
    // est money slot — the hard budget edits where it lives (audit fix)
    line.append(estChip(p, st, (st.tasks || []).length > 0));
    // done-by chip — the schedule date (click to set; ink when past due)
    // an unset date is not information — the chip appears on hover to be set
    // (goals does the same with its until chip)
    const dbCls = "pp3-doneby" + (!st.checked && st.doneBy && st.doneBy < today ? " late" : "")
      + (!st.checked && !st.doneBy ? " unset" : "");
    const db = el("button", dbCls, st.checked ? (st.done || "") : (st.doneBy ? "by " + st.doneBy : "by —"));
    db.title = st.checked ? "done date" : "done-by date (click to set)";
    if (!st.checked) db.onclick = (e) => {
      e.stopPropagation();
      const input = document.createElement("input");
      input.type = "date"; input.className = "pp3-doneby-edit"; input.value = st.doneBy || "";
      input.onchange = () => propWorkOp(p, { op: "set-field", id: st.id, field: "done-by", value: input.value });
      input.onblur = () => { if (input.parentNode) input.replaceWith(db); };
      db.replaceWith(input); input.focus();
    };
    line.append(db);
    // delete — arm-to-confirm (cascades tethered bids server-side)
    const del = el("button", "pp3-stage-x", "✕");
    del.title = "delete rock";
    del.onclick = (e) => {
      e.stopPropagation();
      const yes = el("button", "pp3-stage-x armed", "delete?");
      yes.onclick = () => propWorkOp(p, { op: "delete", id: st.id });
      del.replaceWith(yes);
      setTimeout(() => { if (yes.parentNode) yes.replaceWith(del); }, 2500);
    };
    line.append(del);
    block.append(line);
    // bids allocated at the ROCK id itself (no milestone under it yet) hang
    // directly under the rock line — same affordance as a node's bids
    appendBidLine(block, p, st);
    stagesSec.append(block);
    // nodes: milestones render as sub-heads with their own children +
    // composer; tasks render as todo rows; decisions live in the lane above
    const nodeRow = (n, depth) => {
      if (n.decision) return;
      if (n.milestone) {
        const ml = el("div", "go-stage" + (n.checked ? " done" : ""));
        const mglyph = el("button", "go-check" + (n.checked ? " on" : ""), n.checked ? "✓" : "◇");
        mglyph.title = n.checked ? "reopen milestone" : "mark milestone done";
        mglyph.onclick = (e) => { e.stopPropagation(); propWorkOp(p, { op: "check", id: n.id, checked: !n.checked }); };
        const mname = el("span", "go-stage-text", n.text || "");
        mname.title = "click to rename";
        mname.onclick = () => {
          const input = inputEl(""); input.value = n.text || ""; input.classList.add("work-edit");
          input.addEventListener("keydown", (ev) => {
            if (ev.key === "Enter" && input.value.trim()) propWorkOp(p, { op: "edit", id: n.id, text: input.value.trim() });
            else if (ev.key === "Escape") input.replaceWith(mname);
          });
          input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(mname); });
          mname.replaceWith(input); input.focus();
        };
        const mdel = el("button", "pp3-stage-x", "✕");
        mdel.title = "delete milestone";
        mdel.onclick = (e) => {
          e.stopPropagation();
          const yes = el("button", "pp3-stage-x armed", "delete?");
          yes.onclick = () => propWorkOp(p, { op: "delete", id: n.id });
          mdel.replaceWith(yes);
          setTimeout(() => { if (yes.parentNode) yes.replaceWith(mdel); }, 2500);
        };
        ml.append(mglyph, mname);
        ml.append(estChip(p, n, (n.children || []).length > 0));
        appendContractChips(ml, n);
        ml.append(mdel);
        block.append(ml);
        appendBidLine(block, p, n);
        (n.children || []).forEach((c) => nodeRow(c, depth + 1));
        if (!n.checked) {
          const adds = el("div", "pp3-adds deep");
          adds.append(ghostInput("＋ task", "go-task-ghost", (v) =>
            propWorkOp(p, { op: "add-task", stageId: n.id, text: v }), "task…"));
          block.append(adds);
        }
        return;
      }
      const row = propTodoRow(p, {
        id: n.taskId, text: n.text, checked: !!n.checked, owner: n.owner || "",
        waiting: n.waiting || "", since: n.since || "", workId: n.id,
      }, "tree");
      if (depth > 0) row.classList.add("deep");
      row.append(estChip(p, n, false));
      appendContractChips(row, n);
      block.append(row);
      appendBidLine(block, p, n);
      (n.children || []).forEach((c) => nodeRow(c, depth + 1));
    };
    (st.tasks || []).forEach((n) => nodeRow(n, 0));
    if (!st.checked) {
      // the goals page's affordance: two ghost buttons that open an input,
      // rather than an always-open field repeating the rock's own name
      const adds = el("div", "pp3-adds");
      adds.append(ghostInput("＋ task", "go-task-ghost", (v) =>
        propWorkOp(p, { op: "add-task", stageId: st.id, text: v }), "task…"));
      adds.append(ghostInput("＋ milestone", "go-stage-ghost", (v) =>
        propWorkOp(p, { op: "add-task", stageId: st.id, text: v, kind: "milestone" }), "milestone…"));
      adds.append(ghostInput("＋ decision", "go-stage-ghost", (v) =>
        propWorkOp(p, { op: "add-task", stageId: st.id, text: v, kind: "decision" }), "decision…"));
      block.append(adds);
    }
  });
  if (stages.length) {
    stagesSec.append(ghostInput("＋ rock", "pp3-stage-add", (v) => propWorkOp(p, { op: "add-stage", text: v }), "rock name…"));
  }
  if (!stages.length) {
    // no rock plan yet — flat legacy list, plus a seed action from the template
    (p.tasks || []).filter((t) => !t.checked).forEach((t) => stagesSec.append(propTodoRow(p, t)));
    const seed = el("button", "o-ghost", "＋ seed rocks from the " + (p.kind === "new-construction" ? "new build" : "gut rehab") + " template");
    seed.onclick = async () => {
      try { await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work", { op: "seed", template: p.kind === "new-construction" ? "new-build" : "rehab" }); renderProperties(); }
      catch (e) { showToast("Couldn't seed rocks — " + (e.message || "")); }
    };
    stagesSec.append(seed);
  }
  const composer = propTodoComposer(p);
  stagesSec.append(composer);
  main.append(stagesSec);

  // UNDERWRITING — override chips, the four tier-1 inputs, computed outputs
  main.append(underwritingSection(p));

  // SPEND — last 3 ledger rows + link to Money (full ledger stays below)
  main.append(ledgerSection(p));

  // LOG — the record's running history (## log) + quick-add
  main.append(logSection(p));

  // LOOK-BACK (pass 5): a locked property that finished its plan reads
  // initial-vs-final + unit costs
  const finished = ["completed", "leased", "listed", "sold"].includes(p.status) ||
    ((p.work || []).length > 0 && (p.work || []).every((st) => st.checked));
  if (p.underwrite && finished) main.append(lookbackSection(p));

  // LINKS — artifacts are linked, never mirrored
  const links = el("div", "pp3-links");
  const linkBtn = (label, key, url) => {
    if (url) {
      const a = el("a", "pp3-link", label + " ↗");
      a.href = url; a.target = "_blank"; a.rel = "noopener";
      links.append(a);
    } else {
      const b = el("button", "pp3-link ghost", "＋ " + label);
      b.onclick = () => {
        const v = prompt(label + " URL:", "");
        if (v !== null) propFieldSave(p.slug, key, v.trim());
      };
      links.append(b);
    }
  };
  linkBtn("Drive", "drive", p.drive);
  linkBtn("AGC", "agc", p.agc);
  main.append(links);

  // restore an open inspector across re-renders. A decision is not in the flat
  // p.tasks projection, so it resolves against the tree it lives in.
  if (propSel && propSel.kind === "task") {
    const t = (p.tasks || []).find((x) => x.id === propSel.id);
    if (t) openPropInspector(p, { kind: "task", id: t.id, task: t }); else closePropInspector();
  } else if (propSel && propSel.kind === "decision") {
    let hit = null;
    (p.work || []).forEach((st) => walkNodes(st, st.tasks, (rock, n) => { if (n.id === propSel.id) hit = { rock, n }; }));
    if (hit) openPropInspector(p, { kind: "decision", id: hit.n.id, node: hit.n, rock: hit.rock });
    else closePropInspector();
  }
}

// ---- side card: located parcel thumb + owner of record ----

let _thumbMap = null;  // the live mini-map instance
let _thumbSlug = null; // the property it is showing

// mountThumbOnce — the map is per-property, not per-render: re-mounting it on
// every edit rebuilt Leaflet (and made the rail flash) for no new information.
function mountThumbOnce(hostEl, slug) {
  if (_thumbSlug === slug && hostEl.firstChild) return;
  _thumbSlug = slug;
  hostEl.innerHTML = "";
  const thumbSlot = el("div", "pp-thumb-slot");
  hostEl.append(thumbSlot);
  loadPropGeo(slug).then((features) => mountParcelThumb(thumbSlot, features));
}

async function loadPropGeo(slug) {
  if (slug in propGeoCache) return propGeoCache[slug];
  try {
    const d = await (await fetch("/api/properties/geo?slug=" + encodeURIComponent(slug))).json();
    propGeoCache[slug] = ((d.records || [])[0] || {}).features || [];
  } catch (e) { propGeoCache[slug] = []; }
  return propGeoCache[slug];
}

function ringsFromFeatures(features) {
  const rings = [];
  (features || []).forEach((f) => {
    const g = (f && f.geometry) || {};
    if (g.type === "Polygon") (g.coordinates || []).forEach((r) => rings.push(r));
    else if (g.type === "MultiPolygon") (g.coordinates || []).forEach((poly) => (poly || []).forEach((r) => rings.push(r)));
  });
  return rings;
}

// mountParcelThumb: a LOCATED thumbnail — a small CARTO-tiled mini-map (same
// basemap as the full map) zoomed to show the parcel in its block, with the
// parcel highlighted. A bare outline floating on white doesn't tell you where
// the property is; the streets and neighbors around it do. Non-interactive;
// click → the full map. Falls back to the quiet SVG outline when offline.
async function mountParcelThumb(slot, features) {
  const rings = ringsFromFeatures(features);
  if (rings.flat().length < 3) { slot.remove(); return; }
  try { await loadLeaflet(); }
  catch (e) {
    const svg = parcelThumbSVG(rings);
    if (svg) slot.replaceWith(svg); else slot.remove();
    return;
  }
  if (!slot.isConnected) return;
  const box = el("div", "pp-thumb");
  slot.replaceWith(box);
  if (_thumbMap) { try { _thumbMap.remove(); } catch (e) {} _thumbMap = null; }
  const map = L.map(box, {
    zoomControl: false, attributionControl: false, dragging: false,
    scrollWheelZoom: false, doubleClickZoom: false, boxZoom: false,
    keyboard: false, touchZoom: false,
  });
  _thumbMap = map;
  L.tileLayer("https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png", { maxZoom: 19 }).addTo(map);
  const layer = L.geoJSON({ type: "FeatureCollection", features }, {
    style: { color: "#265ACC", weight: 2, fillColor: "#265ACC", fillOpacity: 0.2 },
  }).addTo(map);
  const bounds = layer.getBounds();
  // fit tight to the parcel, then back off two zoom levels so a block or two of
  // context (streets, neighboring lots) is always in frame
  const fit = () => {
    map.fitBounds(bounds, { maxZoom: 19 });
    map.setZoom(Math.max(map.getZoom() - 2, 15), { animate: false });
  };
  fit();
  setTimeout(() => { map.invalidateSize(); fit(); }, 80);
  map.on("click", () => { location.hash = "#/properties/map"; });
  box.title = "open the map";
  box.style.cursor = "pointer";
}

// parcelThumbSVG: the offline fallback — a quiet outline, no tiles.
function parcelThumbSVG(rings) {
  const pts = rings.flat();
  if (pts.length < 3) return null;
  const xs = pts.map((q) => q[0]), ys = pts.map((q) => q[1]);
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minY = Math.min(...ys), maxY = Math.max(...ys);
  const W = 240, H = 150, pad = 14;
  const s = Math.min((W - 2 * pad) / ((maxX - minX) || 1e-9), (H - 2 * pad) / ((maxY - minY) || 1e-9));
  const ox = (W - (maxX - minX) * s) / 2, oy = (H - (maxY - minY) * s) / 2;
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "pp-thumb");
  rings.forEach((r) => {
    const poly = document.createElementNS("http://www.w3.org/2000/svg", "polygon");
    poly.setAttribute("points",
      r.map((q) => (((q[0] - minX) * s + ox).toFixed(1)) + "," + ((H - ((q[1] - minY) * s + oy)).toFixed(1))).join(" "));
    svg.appendChild(poly);
  });
  svg.onclick = () => { location.hash = "#/properties/map"; };
  return svg;
}

// ownerCard: who owns this parcel. Owned → the holding entity (with a "deed:"
// check line when the assessor's vesting reads differently). Otherwise → the
// owner of record (cmd/owner-pull stamps it from the assessor; click to
// correct by hand), mailing address, tenure, and tax intel when the research
// layer has this parcel.
function ownerCard(p) {
  const card = el("div", "pp3-owner");
  card.append(el("div", "pp3-owner-label", "OWNER"));
  const intel = p.intel || null;
  const line = (txt, cls) => el("div", "pp3-owner-line" + (cls ? " " + cls : ""), txt);

  // `acq === "owned"` is the CLOSED test. control:"owned" is set at signing,
  // so reading it here named the entity as owner of parcels it has only
  // agreed to buy — and then flagged the actual seller's deed as a warning.
  if (p.acq === "owned" && p.entity) {
    card.append(el("div", "pp3-owner-name", p.entity));
    // title check: the deed vesting per the assessor, when it disagrees
    if (p.owner && p.owner.toLowerCase() !== p.entity.toLowerCase()) {
      card.append(line("deed: " + p.owner, "warn"));
    }
    if (p.ownerSince) card.append(line("since " + p.ownerSince));
  } else {
    if (p.acq === "under-contract" && p.entity) {
      card.append(line("under contract to " + p.entity, "acq"));
    }
    const name = p.owner || (intel && intel.owner) || "";
    card.append(editableOwnerLine(p, "owner", name || "no owner on record", "pp3-owner-name" + (name ? "" : " missing")));
    const addr = p.ownerAddr || (intel && intel.ownerAddr) || "";
    if (addr || name) card.append(editableOwnerLine(p, "owner-addr", addr || "add mailing address…", "pp3-owner-line" + (addr ? "" : " missing")));
    const since = p.ownerSince || (intel && intel.saleDate) || "";
    if (since) card.append(line("theirs since " + since));
  }
  if (intel) {
    if (intel.taxStatus === "delinquent") card.append(line("tax: DELINQUENT $" + Math.round(intel.taxBalDue || 0).toLocaleString(), "warn"));
    else if (intel.taxStatus === "lra") card.append(line("LRA land-bank", "warn"));
    else if (intel.taxStatus) card.append(line("tax: " + intel.taxStatus));
    if (intel.assessed) card.append(line("assessed $" + Math.round(intel.assessed).toLocaleString()));
  }
  return card;
}

// editableOwnerLine: click the value → inline input → Enter writes the
// frontmatter field (`owner` / `owner-addr`) via /field.
function editableOwnerLine(p, key, display, cls) {
  const v = el("div", cls, display);
  v.title = "click to edit";
  v.onclick = () => {
    const input = inputEl("");
    input.className = "pp3-owner-in";
    input.value = (key === "owner" ? p.owner : p.ownerAddr) || "";
    const save = async () => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key, value: input.value.trim() });
        renderProperties();
      } catch (e) { showToast("Couldn't save"); }
    };
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      if (ev.key === "Escape") input.replaceWith(v);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(v); });
    v.replaceWith(input);
    input.focus();
  };
  return v;
}

// ---- underwrite editor: the plan inputs behind the BUDGET figure ----
// purchase + closing (acquisition) · hard (Σ work est once estimated, else the
// underwrite number) · soft/carry · contingency %. Writes source.json money
// keys in place (read-modify-write, same slice the server derives from).

async function renderUnderwrite(p, uwHost) {
  let src = null;
  try { src = (await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json()).source; } catch (e) {}
  if (propPageSlug !== p.slug || !uwHost.isConnected) return;
  uwHost.innerHTML = "";
  const root = src && typeof src === "object" ? src : {};
  // mirror the server's sourceMoney pick: top-level slice unless it's empty
  // and a properties[0] full-deal shape exists
  let money = root;
  if (!(root.purchase_price > 0) && !(root.hard_costs > 0) &&
      Array.isArray(root.properties) && root.properties.length > 0 &&
      typeof root.properties[0] === "object") {
    money = root.properties[0];
  }
  const workEst = (p.work || []).reduce((n, st) => n + (st.estTotal || 0), 0);

  uwHost.append(el("div", "pp3-uw-title", "UNDERWRITE — the plan behind the budget"));
  const fields = [];
  const row = (label, key, val, note) => {
    const r = el("div", "pp3-uw-row");
    r.append(el("span", "pp3-uw-label", label));
    if (note) r.append(el("span", "pp3-uw-note", note));
    const input = moneyInput("0", val);
    input.value = val || "";
    r.append(input);
    fields.push({ key, input });
    uwHost.append(r);
    return input;
  };
  row("purchase price", "purchase_price", money.purchase_price || 0);
  row("closing costs", "closing_costs", money.closing_costs || 0);
  if (workEst > 0) {
    const r = el("div", "pp3-uw-row");
    r.append(el("span", "pp3-uw-label", "hard costs"));
    r.append(el("span", "pp3-uw-note", "Σ work est — edit in the record's ## work"));
    r.append(el("span", "pp3-uw-derived", fmtMoney(workEst)));
    uwHost.append(r);
  } else {
    row("hard costs", "hard_costs", money.hard_costs || 0, "until the work list is estimated");
  }
  row("soft / carry", "carry_cost", money.carry_cost || 0, "interest · taxes · ins · util");
  // contingency: stored as a fraction, edited as a %
  const cr = el("div", "pp3-uw-row");
  cr.append(el("span", "pp3-uw-label", "contingency"));
  cr.append(el("span", "pp3-uw-note", "% of hard"));
  const pct = moneyInput("0", 0);
  pct.step = "0.5";
  pct.value = money.contingency_pct ? +(money.contingency_pct * 100).toFixed(2) : "";
  cr.append(pct);
  uwHost.append(cr);

  const actions = el("div", "pp3-uw-actions");
  const save = el("button", "pp3-compose-go", "save ↵");
  save.onclick = async () => {
    fields.forEach((f) => { money[f.key] = parseFloat(f.input.value) || 0; });
    money.contingency_pct = (parseFloat(pct.value) || 0) / 100;
    try {
      await putJSON("/api/properties/" + encodeURIComponent(p.slug) + "/source", root);
      showToast("Underwrite saved");
      renderProperties();
    } catch (e) { showToast("Couldn't save: " + (e.message || e)); }
  };
  const cancel = el("button", "pp3-uw-cancel", "close");
  cancel.onclick = () => { propUWOpen = false; renderPropertyPage(p.slug); };
  actions.append(cancel, save);
  uwHost.append(actions);
}

// ---- OPERATING — monthly income vs operating expenses (stabilized lane) ----

function operatingSection(p) {
  const o = p.operating;
  const sec = el("div", "pp3-sec pp3-operating-sec");
  const head = el("div", "pp3-sec-head");
  head.append(el("span", "pp3-sec-title", "OPERATING"),
    el("span", "pp3-sec-count", fmtMoney(o.income) + " in · " + fmtMoney(o.expenses) + " out"));
  sec.append(head);
  const cols = el("div", "pp3-op-cols");
  ["MONTH", "INCOME", "OPEX", "NET"].forEach((h, i) =>
    cols.append(el("span", "micro-label" + (i ? " pp3-op-r" : ""), h)));
  sec.append(cols);
  const months = (o.months || []).slice().reverse(); // newest first
  months.forEach((m) => {
    const row = el("div", "pp3-op-row");
    row.append(el("span", "", m.month || "undated"));
    row.append(el("span", "pp3-op-r pp3-op-in", m.income ? "+" + fmtMoney(m.income) : "—"));
    row.append(el("span", "pp3-op-r", m.expenses ? fmtMoney(m.expenses) : "—"));
    row.append(el("span", "pp3-op-r" + (m.net < 0 ? " over" : ""), fmtMoney(m.net)));
    sec.append(row);
    // the expense breakdown rides as a quiet sub-line of category chips
    if (m.byCategory) {
      const cats = Object.keys(m.byCategory).sort();
      row.title = cats.map((c) => c + " " + fmtMoney(m.byCategory[c])).join(" · ");
      const sub = el("div", "pp3-op-sub");
      cats.forEach((c) => sub.append(el("span", "pp3-op-chip", c + " " + fmtMoney(m.byCategory[c]))));
      sec.append(sub);
    }
  });
  // actual rent vs the unit-mix screening estimate, when both exist
  if (p.rentMonthly) {
    const latest = months.find((m) => m.month && m.income > 0);
    if (latest) {
      sec.append(el("div", "pp3-uw-note",
        "collecting " + fmtMoney(latest.income) + "/mo vs " + fmtMoney(p.rentMonthly) + "/mo unit-mix estimate"));
    }
  }
  return sec;
}

// ---- ledger: rows edit in place, ✕ deletes, a composer adds ----

function ledgerSection(p) {
  const ledger = el("div", "pp3-sec pp3-ledger-sec"); // phone drops it — reach spend from Money
  const lh = el("div", "pp3-sec-head");
  lh.append(el("span", "pp3-sec-title", "LEDGER"), el("span", "pp3-sec-count", String((p.ledger || []).length)));
  ledger.append(lh);
  (p.ledger || []).forEach((r, i) => {
    ledger.append(i === propLedgerEdit ? ledgerForm(p, r, i) : ledgerRow(p, r, i));
  });
  if (!(p.ledger || []).length) ledger.append(el("div", "pp-empty", "No money facts yet."));
  if (propLedgerAdd) {
    ledger.append(ledgerForm(p, null, -1));
  } else {
    const add = el("button", "pp3-ledger-add", "＋ money fact");
    add.onclick = () => { propLedgerAdd = true; renderPropertyPage(p.slug); };
    ledger.append(add);
  }
  return ledger;
}

function ledgerRow(p, r, i) {
  const row = el("div", "pp3-ledger-row");
  row.append(el("span", "", r.date || ""));
  const bidTag = r.type === "bid" ? "bid " + (r.status || "") : "";
  row.append(el("span", "", r.category ? r.category + (bidTag ? " · " + bidTag : "") : (bidTag || r.type || "")));
  const vend = el("span", "pp3-ledger-vendor", r.vendor || r.contractor || "");
  // the bid link (receipt-attach): a linked contract shows as a quiet chip;
  // when the contract carries a doc, the chip opens the receipt file
  if (r.contract) {
    const c = (typeof reContracts === "function" ? reContracts() : []).find((x) => x.slug === r.contract);
    if (c && c.doc && c.doc.startsWith("sha256:")) {
      const a = el("a", "pp3-ledger-bidchip", "⎘ " + (c.name || r.contract));
      a.href = "/api/realestate/files/" + encodeURIComponent(c.doc.slice(7));
      a.target = "_blank"; a.rel = "noopener"; a.title = "open the bid's receipt file";
      a.onclick = (e) => e.stopPropagation();
      vend.append(a);
    } else {
      vend.append(el("span", "pp3-ledger-bidchip", "⎘ " + ((c && c.name) || r.contract)));
    }
  }
  row.append(vend);
  // income reads as income — signed and accented, never expense-identical
  const isIncome = r.type === "income";
  row.append(el("span", "pp3-ledger-amt" + (isIncome ? " inflow" : ""),
    r.amount ? (isIncome ? "+" : "") + fmtMoney(r.amount) : ""));
  // hover ✕ — arm to confirm, then delete the csv row
  const x = el("button", "uw-x pp3-todo-x", "✕");
  x.title = "delete this row";
  x.onclick = (e) => {
    e.stopPropagation();
    const yes = el("button", "pp3-compose-go", "delete?");
    yes.onclick = async (ev) => {
      ev.stopPropagation();
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate", { original: r, replacement: null });
        renderProperties();
      } catch (err) { showToast("Couldn't delete"); }
    };
    x.replaceWith(yes);
    setTimeout(() => { if (yes.parentNode) yes.replaceWith(x); }, 2500);
  };
  row.append(x);
  row.title = "click to edit";
  row.onclick = () => { propLedgerEdit = i; propLedgerAdd = false; renderPropertyPage(p.slug); };
  return row;
}

// ledgerForm: one inline editor for a row (r set) or the add composer (r null).
function ledgerForm(p, r, i) {
  const form = el("div", "pp3-lform");
  const grid = el("div", "pp3-lform-grid");
  const labeled = (label, node) => {
    const wrap = el("label", "pp3-lform-field");
    wrap.append(el("span", "pp3-lform-label", label), node);
    return wrap;
  };
  const date = inputEl("YYYY-MM-DD");
  const now = new Date();
  const localToday = now.getFullYear() + "-" + String(now.getMonth() + 1).padStart(2, "0") + "-" + String(now.getDate()).padStart(2, "0");
  date.className = "pp-in"; date.value = r ? (r.date || "") : localToday;
  const type = selectEl(["expense", "income", "bid"]);
  // preserve the row's real type on open — coercing income → expense here
  // silently converted rent rows into spend (the stray-click hazard)
  type.className = "pp-in";
  type.value = r && ["expense", "income", "bid"].includes(r.type) ? r.type : "expense";
  const status = selectEl([]);
  status.className = "pp-in";
  const setStatusOpts = () => {
    status.innerHTML = "";
    const opts = type.value === "bid" ? ["requested", "received", "accepted", "declined"]
      : type.value === "income" ? ["received"] : ["paid"];
    opts.forEach((s) => {
      const o = document.createElement("option"); o.value = s; o.textContent = s; status.append(o);
    });
    if (r && r.status && [...status.options].some((o) => o.value === r.status)) status.value = r.status;
  };
  setStatusOpts();
  type.onchange = setStatusOpts;
  const category = inputEl("category"); category.className = "pp-in"; category.value = r ? (r.category || "") : "";
  const vendor = inputEl("vendor"); vendor.className = "pp-in"; vendor.value = r ? (r.vendor || "") : "";
  // contractor autofills from the contractor records (owner ask 2026-08-19)
  const contractorTa = recordAutocomplete("contractor", "contractor…");
  contractorTa.setValue(r ? (r.contractor || "") : "");
  const amount = moneyInput("$", r ? r.amount : 0); amount.value = r && r.amount ? r.amount : "";
  const note = inputEl("note"); note.className = "pp-in"; note.value = r ? (r.note || "") : "";
  grid.append(labeled("date", date), labeled("type", type), labeled("status", status),
    labeled("category", category), labeled("vendor", vendor), labeled("contractor", contractorTa.el),
    labeled("amount", amount), labeled("note", note));
  // hops (§7): node tether + bid/contract link — on ADDS and EDITS alike
  // (the QuickBooks receipt-attach: linking an expense to the bid it paid;
  // a proposed bid offers to accept, a doc-carrying one shows its receipt)
  const nodeSel = selectEl([]);
  nodeSel.className = "pp-in";
  const nopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; nodeSel.append(o); };
  nopt("", "— no tether —");
  (p.work || []).forEach((st) => {
    nopt(st.id, st.text);
    (st.tasks || []).forEach(function walk(n, prefix) {
      const pre = typeof prefix === "string" ? prefix : "· ";
      nopt(n.id, pre + n.text);
      (n.children || []).forEach((c) => walk(c, pre + "· "));
    });
  });
  if (r && r.workId) nodeSel.value = r.workId;
  const contractSel = selectEl([]);
  contractSel.className = "pp-in";
  const copt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; contractSel.append(o); };
  copt("", "— no bid / contract —");
  const cs = typeof reContracts === "function" ? reContracts() : [];
  cs.filter((c) => c.status === "accepted" && (c.allocations || []).some((a) => a.property === p.slug))
    .forEach((c) => copt(c.slug, c.name + " · " + fmtMoney(c.remaining != null ? c.remaining : c.total) + " left"));
  cs.filter((c) => c.status === "proposed" && (c.allocations || []).some((a) => a.property === p.slug))
    .forEach((c) => copt(c.slug, c.name + " · proposed bid " + fmtMoney(c.total)));
  if (r && r.contract) contractSel.value = r.contract;
  contractSel.onchange = () => {
    // picking a bid without a node prefills the node from its allocation
    if (contractSel.value && !nodeSel.value) {
      const c = cs.find((x) => x.slug === contractSel.value);
      const al = c && (c.allocations || []).find((x) => x.property === p.slug);
      if (al) nodeSel.value = al.nodeId;
    }
    ledgerBidExtras();
  };
  grid.append(labeled("node", nodeSel), labeled("bid / contract", contractSel));
  form.append(grid);
  // receipt link + proposed-accept offer under the grid, live to the pick
  const bidExtras = el("div", "pp3-lform-bid");
  form.append(bidExtras);
  const ledgerBidExtras = () => {
    bidExtras.innerHTML = "";
    const c = cs.find((x) => x.slug === contractSel.value);
    if (!c) return;
    if (c.doc && c.doc.startsWith("sha256:")) {
      const a = el("a", "pp3-link", "receipt ↗ (" + c.name + ")");
      a.href = "/api/realestate/files/" + encodeURIComponent(c.doc.slice(7));
      a.target = "_blank"; a.rel = "noopener";
      bidExtras.append(a);
    }
    if (c.status === "proposed") {
      const strip = el("div", "re-bid-accept");
      strip.append(el("span", "", "accept this bid? (" + fmtMoney(c.total) + " becomes committed)"));
      const yes = el("button", "pill light", "accept ✓");
      yes.onclick = async () => {
        try {
          await postJSONOk("/api/realestate/contracts/" + encodeURIComponent(c.slug) + "/accept", {});
          if (typeof loadReContracts === "function") await loadReContracts();
          showToast("Bid accepted — " + fmtMoney(c.total) + " committed");
          strip.remove();
        } catch (e) { showToast("Couldn't accept — " + (e.message || "")); }
      };
      strip.append(yes, el("span", "pp3-uw-note", "or save to link without accepting"));
      bidExtras.append(strip);
    }
  };
  ledgerBidExtras();

  const actions = el("div", "pp3-uw-actions");
  const cancel = el("button", "pp3-uw-cancel", "cancel");
  cancel.onclick = () => { propLedgerEdit = -1; propLedgerAdd = false; renderPropertyPage(p.slug); };
  const save = el("button", "pp3-compose-go", r ? "save ↵" : "add ↵");
  save.onclick = async () => {
    const amt = parseFloat(amount.value) || 0;
    if (!amt) { showToast("Amount is required"); return; }
    try {
      if (r) {
        // rebuild the note with the row's hidden tokens intact — the bid/
        // contract link and node tether come from the pickers (editable now),
        // the rest of the token set is preserved verbatim
        let n = note.value.trim();
        if (contractSel.value) n += " [contract:: " + contractSel.value + "]";
        if (r.cat) n += " [cat:: " + r.cat + "]";
        if (r.paidBy) n += " [paid-by:: " + r.paidBy + "]";
        if (r.stmt) n += " [stmt:: " + r.stmt + "]";
        const replacement = {
          date: date.value.trim(), type: type.value, category: category.value.trim(),
          vendor: vendor.value.trim(), contractor: contractorTa.value(), amount: amt,
          status: status.value, note: n.trim(), doc: r.doc || "", workId: nodeSel.value || "",
        };
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate", { original: r, replacement });
      } else {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger", {
          date: date.value.trim(), type: type.value, category: category.value.trim(),
          vendor: vendor.value.trim(), contractor: contractorTa.value(), amount: amt,
          status: status.value, note: note.value.trim(),
          workId: nodeSel.value, contract: contractSel.value,
        });
      }
      propLedgerEdit = -1; propLedgerAdd = false;
      renderProperties();
    } catch (e) { showToast("Couldn't save: " + (e.message || e)); }
  };
  actions.append(cancel, save);
  form.append(actions);
  return form;
}

// walkNodes — depth-first over a rock's node tree (shared by the page + the
// look-back derivations).
function walkNodes(rock, nodes, fn) {
  (nodes || []).forEach((n) => { fn(rock, n); walkNodes(rock, n.children, fn); });
}

function compositeId(p, t) { return "prop:" + p.slug + "/" + t.id; }

// estChip — the [est::] money slot on any tree line (the hard budget edits
// where it lives — audit fix: the page used to say "edit in the record").
// Parents display the ROLLED estTotal; the click edits the line's OWN est.
function estChip(p, node, isParent) {
  const rolled = node.estTotal != null ? node.estTotal : (node.est || 0);
  const own = node.est || 0;
  const chip = el("button", "re-est-chip" + (rolled ? "" : " unset"), rolled ? fmtMoneyShort(rolled) : "est —");
  chip.title = isParent && rolled !== own
    ? "Σ own + children — click edits this line's OWN est (" + fmtMoneyShort(own) + ")"
    : "estimate — click to edit";
  chip.onclick = (e) => {
    e.stopPropagation();
    const inp = inputEl("est $");
    inp.className = "re-est-edit";
    inp.value = own || "";
    const commit = () => {
      const v = inp.value.trim();
      const n = parseFloat(v.replace(/[,$]/g, ""));
      propWorkOp(p, { op: "set-field", id: node.id, field: "est", value: v === "" || isNaN(n) ? "" : String(n) });
    };
    inp.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") commit();
      else if (ev.key === "Escape") inp.replaceWith(chip);
    });
    inp.addEventListener("blur", () => { if (inp.parentNode) inp.replaceWith(chip); });
    chip.replaceWith(inp);
    inp.focus();
  };
  return chip;
}

// logSection — the record's `## log` (audit fix: parsed + a dedicated writer
// existed with zero rendering). Newest-first; quick-add prepends with today's
// date; long histories fold.
let propLogOpen = false;
function logSection(p) {
  const sec = el("div", "pp3-sec");
  const head = el("div", "pp3-sec-head");
  head.append(el("span", "pp3-sec-title", "LOG"), el("span", "pp3-sec-count", String((p.log || []).length)));
  sec.append(head);
  const lines = p.log || [];
  const cap = propLogOpen ? lines.length : 5;
  lines.slice(0, cap).forEach((ln) => sec.append(el("div", "re-log-line", ln)));
  if (lines.length > 5) {
    const t = el("button", "aion-done-toggle", (propLogOpen ? "▾" : "▸") + " " + (lines.length - 5) + " older");
    t.onclick = () => { propLogOpen = !propLogOpen; renderPropertyPage(p.slug); };
    sec.append(t);
  }
  sec.append(ghostInput("＋ log line", "re-log-add", async (v) => {
    try {
      applyFreshProperty(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/log", { text: v }));
    } catch (e) { showToast("Couldn't log — " + (e.message || "")); }
  }, "what happened…"));
  return sec;
}

// lookbackSection — pass 5 (§4 look-back): a finished property's initial-vs-
// final picture. Per rock: est at lock · final committed · final paid; unit
// costs derive from the frontmatter measurables (node totals ÷ measure) —
// everything computed here, nothing stored.
function lookbackSection(p) {
  const lock = p.underwrite;
  const sec = el("div", "pp3-sec re-lookback");
  const head = el("div", "pp3-sec-head");
  head.append(el("span", "pp3-sec-title", "LOOK-BACK"),
    el("span", "pp3-sec-count", "locked " + (p.locked || "") + " → final"));
  sec.append(head);
  const cols = el("div", "re-lookback-cols");
  ["ROCK", "EST @ LOCK", "COMMITTED", "PAID"].forEach((h, i) => cols.append(el("span", i ? "prop-col-r" : "", h)));
  sec.append(cols);
  const lockByID = {};
  (lock.rocks || []).forEach((r) => { lockByID[r.id] = r.estTotal; });
  let tLock = 0, tCom = 0, tPaid = 0;
  (p.work || []).forEach((st) => {
    const was = lockByID[st.id] != null ? lockByID[st.id] : 0;
    tLock += was; tCom += st.committed || 0; tPaid += st.paid || 0;
    if (!was && !st.committed && !st.paid) return;
    const row = el("div", "re-lookback-row");
    row.append(el("span", "", st.text));
    row.append(el("span", "prop-col-r", fmtMoneyShort(was)));
    row.append(el("span", "prop-col-r" + ((st.committed || 0) > was && was ? " over" : ""), fmtMoneyShort(st.committed || 0)));
    row.append(el("span", "prop-col-r", fmtMoneyShort(st.paid || 0)));
    sec.append(row);
  });
  const totals = el("div", "re-lookback-row re-lookback-totals");
  totals.append(el("span", "", "total"));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(tLock)));
  totals.append(el("span", "prop-col-r" + (tCom > tLock && tLock ? " over" : ""), fmtMoneyShort(tCom)));
  totals.append(el("span", "prop-col-r", fmtMoneyShort(tPaid)));
  sec.append(totals);
  if (tLock) {
    const acc = ((tCom - tLock) / tLock) * 100;
    sec.append(el("div", "re-foot-note", "estimate accuracy: final committed " +
      (acc >= 0 ? "+" : "") + acc.toFixed(1) + "% vs the locked underwrite"));
  }
  // unit costs — measurable-matched scopes ($/roof-square when a rock or
  // milestone matches the measurable's stem) + the property-level figures
  const costs = [];
  const units = ((p.unitMix || []).length) || p.units || 0;
  if (units && tCom) costs.push("$" + Math.round(tCom / units).toLocaleString() + "/unit");
  const sqft = (p.unitMix || []).reduce((n, u) => n + (u.sqft || 0), 0);
  if (sqft && tCom) costs.push("$" + (tCom / sqft).toFixed(0) + "/sqft");
  Object.entries(p.measurables || {}).forEach(([k, v]) => {
    if (!v) return;
    const stem = k.split("-")[0];
    let matched = null;
    (p.work || []).forEach((st) => {
      if ((st.text || "").toLowerCase().includes(stem) || st.id.includes(stem)) matched = matched || st;
      walkNodes(st, st.tasks, (_, n) => {
        if (!matched && n.milestone && ((n.text || "").toLowerCase().includes(stem) || (n.id || "").includes(stem))) matched = n;
      });
    });
    if (matched && (matched.committed || 0) > 0) {
      costs.push("$" + Math.round(matched.committed / v).toLocaleString() + "/" + k.replace(/s$/, "") +
        " (" + (matched.text || "") + ")");
    }
  });
  if (costs.length) {
    const line = el("div", "re-uw-measurables");
    line.append(el("span", "re-uw-label", "UNIT COSTS "), el("span", "", costs.join(" · ")));
    sec.append(line);
  }
  return sec;
}

// appendContractChips — a node's accepted-contract slices (the committed
// source) render as chips linking to the contract page.
// appendBidLine — the open bids on a node. A bid is an option, not money: it
// gets its own quiet line under the work it is for, with the range across the
// bids, and each one accepts from there (accepting declines the others — one
// decision, one call: /accept).
function appendBidLine(host, p, n) {
  const bids = n.openBids || [];
  if (!bids.length) return;
  const amounts = bids.map((b) => b.amount).sort((x, y) => x - y);
  const range = amounts.length > 1
    ? fmtMoney(amounts[0]) + "–" + fmtMoney(amounts[amounts.length - 1])
    : fmtMoney(amounts[0]);
  const wrap = el("div", "pp3-bids");
  const head = el("button", "pp3-bids-head",
    (propBidsOpen[n.id] ? "▾" : "▸") + " " + bids.length + " bid" + (bids.length === 1 ? "" : "s") + " · " + range);
  head.title = "quoted, not committed — accept one to commit it";
  head.onclick = (e) => {
    e.stopPropagation();
    propBidsOpen[n.id] = !propBidsOpen[n.id];
    renderPropertyPage(p.slug);
  };
  wrap.append(head);
  if (propBidsOpen[n.id]) {
    bids.forEach((b) => {
      const row = el("div", "pp3-bid-row");
      const who = el("button", "pp3-bid-who", assigneeName(b.contractor) || b.contractor);
      who.title = "open the record";
      who.onclick = (e) => { e.stopPropagation(); location.hash = "#/properties/contract/" + encodeURIComponent(b.slug); };
      row.append(who);
      row.append(el("span", "pp3-bid-meta", [b.date, b.expires ? "expires " + b.expires : ""].filter(Boolean).join(" · ")));
      row.append(el("span", "pp3-bid-amt", fmtMoney(b.amount)));
      const take = el("button", "pp3-bid-accept", "accept");
      take.title = bids.length > 1 ? "commit this bid and decline the others on this work" : "commit this bid";
      take.onclick = async (e) => {
        e.stopPropagation();
        try {
          const r = await postJSONOk("/api/realestate/contracts/" + encodeURIComponent(b.slug) + "/accept", {});
          const n2 = (r && (r.declined || []).length) || 0;
          showToast("Accepted" + (n2 ? " — " + n2 + " other bid" + (n2 === 1 ? "" : "s") + " declined" : ""));
          renderProperties();
        } catch (err) { showToast("Couldn't accept — " + (err.message || "")); }
      };
      row.append(take);
      wrap.append(row);
    });
  }
  host.append(wrap);
}

function appendContractChips(row, n) {
  (n.contracts || []).forEach((cc) => {
    const chip = el("button", "re-node-contract", cc.contractor + " " + fmtMoneyShort(cc.amount));
    chip.title = "contract " + cc.slug;
    chip.onclick = (e) => {
      e.stopPropagation();
      location.hash = "#/properties/contract/" + encodeURIComponent(cc.slug);
    };
    const x = row.querySelector(".pp3-todo-x, .pp3-stage-x");
    if (x) row.insertBefore(chip, x); else row.append(chip);
  });
}

// propWorkOp — the one op endpoint for the `## rocks` tree
// (check · edit · delete · add-stage · add-task · set-field). Re-renders the
// property on success so the rock list, progress bar and hard-cost rollup
// all re-derive from the fresh record. quiet=true skips the re-render (for
// chained ops like resolve-then-check).
async function propWorkOp(p, body, quiet) {
  try {
    const fresh = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work", body);
    if (!quiet) applyFreshProperty(fresh);
  } catch (e) { showToast("Couldn't update — " + (e.message || "")); }
}

// applyFreshProperty — the property save endpoints answer with the freshly
// re-parsed record: swap it into the cache and repaint ONLY the page (the
// old path re-fetched the whole tab — three requests and a jarring full
// redraw for a one-field edit; owner report 2026-08-18).
function applyFreshProperty(p) {
  if (!p || !p.slug) { renderProperties(); return; }
  const i = propertyCache.findIndex((x) => x.slug === p.slug);
  if (i >= 0) {
    p.__source = propertyCache[i].__source; // keep the page's source cache warm
    propertyCache[i] = p;
  }
  if (propMode === "page" && propSlug === p.slug) renderPropertyPage(p.slug);
  else renderProperties();
}

// propTodoRow — variant "tree" wears the goals task shape (this page's rock
// tree); the default keeps .pp3-todo for the flat lists that still use it.
function propTodoRow(p, t, variant) {
  const tree = variant === "tree";
  const row = el("div", (tree ? "go-task" : "pp3-todo") + (t.checked ? " done" : "") + (propSelIs("task", t.id) ? " sel" : ""));
  row.dataset.selKey = "task:" + t.id;
  const check = el("button", (tree ? "go-check" : "tdo-check") + (t.checked ? " on" : ""), t.checked ? "✓" : "○");
  check.title = t.checked ? "reopen" : "done";
  check.onclick = async (e) => {
    e.stopPropagation();
    try { await postJSONOk("/api/tasks/check", { id: compositeId(p, t), checked: !t.checked }); renderProperties(); }
    catch (err) { showToast("Couldn't update"); }
  };
  row.append(check, el("span", tree ? "go-task-text" : "pp3-todo-text", t.text));
  row.append(el("span", (tree ? "go-stage-owner" : "prop-owner") + (mineOwner(t.owner) ? " mine" : ""), assigneeName(t.owner)));
  // hover ✕ — delete the line from the property's ## todos (arm to confirm)
  const x = el("button", "uw-x pp3-todo-x", "✕");
  x.title = "delete this task";
  x.onclick = (e) => {
    e.stopPropagation();
    const yes = el("button", "pp3-compose-go", "delete?");
    yes.onclick = async (ev) => {
      ev.stopPropagation();
      try { await postJSONOk("/api/tasks/drop", { id: compositeId(p, t) }); renderProperties(); }
      catch (err) { showToast("Couldn't delete"); }
    };
    x.replaceWith(yes);
    setTimeout(() => { if (yes.parentNode) yes.replaceWith(x); }, 2500);
  };
  row.append(x);
  row.onclick = () => propSelect(p, { kind: "task", id: t.id, task: t });
  return row;
}

// propSelIs / propSelect — one selection for the rail inspector, shared by the
// tree's tasks and the decisions lane. Selecting never re-renders the page (an
// edit in flight would lose its field); it repaints the marks and re-fills the
// panel, the way the AION list does.
function propSelIs(kind, id) { return !!propSel && propSel.kind === kind && propSel.id === id; }

function propSelect(p, sel) {
  const same = propSelIs(sel.kind, sel.id);
  propSel = same ? null : { kind: sel.kind, id: sel.id };
  els.propertyPage.querySelectorAll(".go-task.sel, .pp3-todo.sel, .aion-dec-row.sel")
    .forEach((n) => n.classList.remove("sel"));
  if (!propSel) { closePropInspector(); return; }
  els.propertyPage.querySelectorAll(".go-task, .pp3-todo, .aion-dec-row").forEach((n) => {
    if (n.dataset.selKey === sel.kind + ":" + sel.id) n.classList.add("sel");
  });
  openPropInspector(p, sel);
}

// the always-present composer row — adding a todo is the page's primary action
function propTodoComposer(p) {
  const row = el("div", "pp3-compose");
  row.append(el("span", "pp3-compose-glyph", "○"));
  const input = inputEl("add a task for this property…");
  input.className = "pp3-compose-in";
  const submit = async () => {
    const text = input.value.trim();
    if (!text) return;
    try {
      await postJSONOk("/api/tasks/item", { text, container: { kind: "property", slug: p.slug } });
      renderProperties();
    } catch (e) { showToast("Couldn't add"); }
  };
  input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") submit(); });
  const go = el("button", "pp3-compose-go", "add ↵");
  go.onclick = submit;
  row.append(input, go);
  return row;
}

// ---- the rail inspector (Rev 3's accountability half) ----
// It docks in the property page's own rail, UNDER the map — the map stays put
// and the panel opens beneath it. sel is {kind:"task"|"decision", …}.
function openPropInspector(p, sel) {
  const t = sel.kind === "decision" ? { id: sel.id, text: sel.node.text, owner: sel.node.owner || "" } : sel.task;
  // phone (Rev 4): the rail is stacked and the panel goes to a bottom sheet.
  // The builder is the same either way.
  const phone = window.mf && window.mf.phone();
  const host = phone
    ? window.mfSheet.body("prop", closePropInspector, () => openPropInspector(p, sel))
    : (propPageEls && propPageEls.inspSlot);
  if (!host) return;
  host.innerHTML = "";
  if (!phone) host.hidden = false;
  const head = el("div", "pp3-insp-head");
  head.append(el("span", "pp3-insp-label", sel.kind === "decision" ? "Decision" : "Inspector"));
  const x = el("button", "pp3-insp-x", "✕");
  x.onclick = closePropInspector;
  head.append(x);
  host.append(head);
  host.append(el("div", "pp3-insp-text", t.text));

  const field = (label, node) => {
    const f = el("div", "pp3-insp-field");
    f.append(el("span", "pp3-insp-flabel", label), node);
    return f;
  };

  // a decision is decided HERE — type the outcome, press Enter. Same two
  // writes the ◇ prompt used to make, in the place the backlog makes them.
  if (sel.kind === "decision" && !sel.node.checked) {
    const outcome = inputEl("what was decided…");
    outcome.className = "pp-in aion-insp-outcome";
    const decide = el("button", "aion-decide-inline", "decide ⏎");
    decide.title = "files the resolution and closes the decision";
    decide.disabled = true;
    const doDecide = async () => {
      const note = outcome.value.trim();
      if (!note) return;
      propSel = null;
      await propWorkOp(p, { op: "set-field", id: sel.id, field: "resolution", value: note }, true);
      propWorkOp(p, { op: "check", id: sel.id, checked: true });
    };
    outcome.addEventListener("input", () => { decide.disabled = !outcome.value.trim(); });
    outcome.addEventListener("keydown", (ev) => { if (ev.key === "Enter") doDecide(); });
    decide.onclick = doDecide;
    host.append(field("outcome", outcome));
    host.append(decide);
  } else if (sel.kind === "decision" && sel.node.resolution) {
    host.append(field("outcome", el("span", "pp3-insp-val", sel.node.resolution)));
  }
  // assignee: a real identity from the RE registry ONLY — system/realestate/
  // people.md + contractor records; the aion roster never reaches properties
  const ownerSel = document.createElement("select");
  ownerSel.className = "pp-in";
  const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; ownerSel.append(o); };
  opt("", "you");
  const a = (propTodosMeta && propTodosMeta.assignees) || {};
  (a.realestate || []).forEach((c) => opt(c.slug, c.name + (c.trade ? " (" + c.trade + ")" : "")));
  ownerSel.value = mineOwner(t.owner) ? "" : t.owner; // BA/me/empty all read as "you"
  const note = el("div", "pp3-insp-note");
  const setNote = () => {
    if (sel.kind === "decision") { note.textContent = "Decided here — the line stays under its rock in the record."; return; }
    note.textContent = ownerSel.value
      ? "Assigned to " + assigneeName(ownerSel.value) + " — tracked here, never in your TASKS. It shows under Outstanding until they close it."
      : "Yours — it shows in TASKS under Real Estate.";
  };
  setNote();
  const assign = async (owner) => {
    try {
      if (sel.kind === "decision") await propWorkOp(p, { op: "set-field", id: sel.id, field: "owner", value: owner }, true);
      else await postJSONOk("/api/tasks/update", { id: compositeId(p, t), owner });
      t.owner = owner;
      setNote();
      renderProperties();
    } catch (e) { showToast("Couldn't assign"); }
  };
  ownerSel.onchange = () => assign(ownerSel.value);
  if (phone) {
    // Rev 4: a tap-list, not a <select> — 48px rows, ● on the current one.
    const current = mineOwner(t.owner) ? "" : t.owner;
    const list = el("div", "mf-assign");
    const rowOpt = (v, l) => {
      const r = el("button", "mf-opt" + (v === current ? " on" : ""));
      r.append(el("span", "mf-opt-dot", v === current ? "●" : "○"), el("span", "", l));
      r.onclick = () => assign(v).then(() => openPropInspector(p, sel)); // re-fill in place (same key)
      list.append(r);
    };
    rowOpt("", "you");
    (a.realestate || []).forEach((c) => rowOpt(c.slug, c.name + (c.trade ? " (" + c.trade + ")" : "")));
    host.append(field("owner", list));
  } else {
    host.append(field("owner", ownerSel));
  }
  // rock: move the task under another of THIS property's rocks (the tree IS
  // the placement) — the server refuses names outside the pipeline
  if ((p.work || []).length) {
    const stSel = document.createElement("select");
    stSel.className = "pp-in";
    const sopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; stSel.append(o); };
    const matchNode = (n) => (sel.kind === "decision" ? n.id === sel.id : n.taskId === t.id);
    const home = (p.work || []).find((st) =>
      (st.tasks || []).some(function inTree(n) { return matchNode(n) || (n.children || []).some(inTree); }));
    sopt("", home ? home.text : "—");
    (p.work || []).filter((st) => !home || st.id !== home.id).forEach((st) => sopt(st.text, st.text));
    stSel.value = "";
    stSel.onchange = async () => {
      if (!stSel.value) return;
      try {
        // rocks only: the re-parent is a remove+append of the line, which would
        // drop a node's children — decisions and tasks are leaves, milestones
        // are not, and are never offered here
        const id = sel.kind === "decision" ? "prop:" + p.slug + "/" + sel.id : compositeId(p, t);
        await postJSONOk("/api/tasks/update", { id, stage: stSel.value });
        renderProperties();
      } catch (e) { showToast("Couldn't move it — " + (e.message || "")); }
    };
    host.append(field("rock", stSel));
  }
  // waiting — [waiting:: who] + [since:: today] on the tree line (audit fix:
  // the board showed waiting state; nothing here could set it). Clearing
  // re-opens; the aging fuse re-anchors to since (tasks conventions).
  if (sel.kind !== "decision" && !t.checked) {
    const wIn = inputEl("who / what it waits on…");
    wIn.className = "pp-in";
    wIn.value = t.waiting || "";
    wIn.onblur = async () => {
      const v = wIn.value.trim();
      if (v === (t.waiting || "")) return;
      const today = new Date().toISOString().slice(0, 10);
      await propWorkOp(p, { op: "set-field", id: t.workId || t.id, field: "waiting", value: v }, true);
      propWorkOp(p, { op: "set-field", id: t.workId || t.id, field: "since", value: v ? today : "" });
    };
    wIn.addEventListener("keydown", (ev) => { if (ev.key === "Enter") wIn.blur(); });
    host.append(field("waiting on", wIn));
    if (t.waiting && t.since) host.append(field("since", el("span", "pp3-insp-val", t.since)));
  }
  host.append(field("property", el("span", "pp3-insp-val", p.short || p.address || p.slug)));
  if (t.added) host.append(field("added", el("span", "pp3-insp-val", t.added)));
  host.append(note);
}

function closePropInspector() {
  propSel = null;
  if (propPageEls && propPageEls.inspSlot) propPageEls.inspSlot.innerHTML = "";
  // phone: the ✕ inside the sheet routes here — close the sheet too. Safe both
  // ways: mfSheet.close() nulls its state before invoking onClose, so the
  // re-entrant call no-ops on closeIf.
  if (window.mfSheet) window.mfSheet.closeIf("prop");
}

async function putJSON(url, body) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}


// propFieldSave — the one-scalar frontmatter save the page's click-to-edit
// affordances share.
async function propFieldSave(slug, key, value) {
  try {
    await postJSONOk("/api/properties/" + encodeURIComponent(slug) + "/field", { key, value });
    renderProperties();
  } catch (e) { showToast("Couldn't save " + key + " — " + (e.message || "")); }
}

// underwritingSection — override chips first (when any), then the FOUR
// property-specific tier-1 inputs, then computed outputs (NOI · ARV · DSCR,
// screening math — the portal engine is the pro-forma truth). Header links
// to Settings with the derived inherit/override count.
function underwritingSection(p) {
  const sec = el("div", "pp3-sec pp3-uw-sec");
  const head = el("div", "pp3-sec-head");
  head.append(el("span", "pp3-sec-title", "UNDERWRITING"));
  // the estimate-vintage lock (overhaul §3.6): everything is an ESTIMATE
  // until the owner's one deliberate lock; after it, initial-vs-real reads
  const LOCKABLE = ["under_contract", "pre_development", "construction"];
  if (p.locked) {
    const relock = el("button", "pp3-uw-lock locked", "locked " + p.locked + " · re-lock");
    relock.title = "re-locking OVERWRITES the frozen snapshot — a deliberate do-over";
    relock.onclick = async () => {
      // no confirm dialog (owner call 2026-08-19) — the button just does it;
      // the snapshot overwrites and initial-vs-real re-anchors to today
      try { applyFreshProperty(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/underwrite-lock", { relock: true })); showToast("Re-locked — initial-vs-real re-anchored to today"); }
      catch (e) { showToast("Couldn't re-lock — " + (e.message || "")); }
    };
    head.append(relock);
  } else if (LOCKABLE.includes(p.status)) {
    const lock = el("button", "pp3-uw-lock", "lock underwriting");
    lock.title = "freeze measurables + rock ests + inputs as the initial underwrite (one deliberate moment)";
    lock.onclick = async () => {
      // no confirm dialog (owner call 2026-08-19) — one click freezes today's
      // measurables + rock ests + inputs as the initial underwrite
      try { applyFreshProperty(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/underwrite-lock", {})); showToast("Locked — the page now shows initial vs real"); }
      catch (e) { showToast("Couldn't lock — " + (e.message || "")); }
    };
    head.append(lock);
  }
  const a = reAssumptions();
  const overrides = (a.__overrides || []).filter((o) => o.kind === "property" && o.slug === p.slug);
  const inherit = (a.__keys || []).length - overrides.length;
  const settingsLink = el("button", "pp3-uw-settings", "inherits " + Math.max(inherit, 0) + " · overrides " + overrides.length + " →");
  settingsLink.onclick = () => { location.hash = "#/properties/settings"; };
  head.append(settingsLink);
  sec.append(head);
  if (overrides.length) {
    const chips = el("div", "re-override-chips");
    overrides.forEach((o) => chips.append(el("span", "re-override-chip", o.key + " · " + o.value)));
    sec.append(chips);
  }
  const host = el("div", "pp3-uw-body");
  sec.append(host);
  (async () => {
    if (!reAssumptionsCache) await loadReAssumptions();
    let src = p.__source;
    if (!src) {
      try {
        const d = await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json();
        src = p.__source = d.source || d; // the endpoint wraps: {source: {...}}
      } catch (e) { src = p.__source = {}; }
    }
    host.innerHTML = "";
    const grid = el("div", "re-uw-grid");
    const input = (label, key) => {
      const f = el("label", "re-uw-field");
      f.append(el("span", "re-uw-label", label));
      const inp = inputEl("");
      inp.className = "pp-in re-uw-in";
      inp.value = src[key] != null ? String(src[key]) : "";
      inp.onchange = async () => {
        const v = parseFloat(inp.value.replace(/[,$]/g, ""));
        const next = Object.assign({}, src);
        if (isNaN(v) || inp.value.trim() === "") delete next[key]; else next[key] = v;
        try {
          await putJSON("/api/properties/" + encodeURIComponent(p.slug) + "/source", next);
          p.__source = next;
          renderPropertyPage(p.slug); // recompute outputs
        } catch (e) { showToast("Couldn't save " + key); }
      };
      f.append(inp);
      return f;
    };
    if (!(p.unitMix || []).length) {
      // no measured mix yet — the sidecar figures still drive screening;
      // the unit-mix editor below graduates them into the record
      grid.append(input("units", "total_units"), input("stabilized rent /unit/mo", "avg_rent_per_unit"));
      host.append(grid);
    }
    host.append(unitMixEditor(p));
    host.append(measurablesEditor(p));
    const uw = reScreeningCalc(p);
    // cost inputs come FROM THE BUDGET (owner call 2026-08-19 — no duplicate
    // purchase/hard fields here): one read-only line names the figures in
    // play; clicking it opens the budget's underwrite editor
    if (uw.complete || uw.purchase || uw.hard) {
      const inputsLine = el("button", "re-uw-frombudget");
      inputsLine.append(el("span", "re-uw-label", "INPUTS · FROM THE BUDGET  "));
      inputsLine.append(el("span", "", "purchase " + fmtMoneyShort(uw.purchase || 0) +
        (uw.closing ? " + closing " + fmtMoneyShort(uw.closing) : "") +
        " · hard " + fmtMoneyShort(uw.hard || 0) + (uw.hardFromWork ? " (Σ rock ests)" : " (underwrite)") +
        " · soft " + fmtMoneyShort(uw.soft || 0) +
        " · contingency " + fmtMoneyShort(uw.contingency || 0) + "  → edit ↗"));
      inputsLine.onclick = () => { propUWOpen = true; renderPropertyPage(p.slug); };
      host.append(inputsLine);
    }
    const outs = el("div", "pp3-strip re-uw-outs");
    const cell = (label, val, cls) => {
      const c = el("div", "pp3-cell");
      c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val" + (cls ? " " + cls : ""), val));
      return c;
    };
    if (uw.complete) {
      outs.append(cell("NOI", fmtMoneyShort(uw.noi)));
      outs.append(cell("ARV", fmtMoneyShort(uw.arv)));
      const dcell = cell("DSCR · at takeout", uw.dscr ? uw.dscr.toFixed(2) : "—", uw.dscr && uw.dscr < 1.25 ? "over" : "");
      dcell.title = "NOI ÷ debt service on the loan the perm actually refinances (LTC·TDC, capped by LTV·ARV) — at the LTV-max loan DSCR is a constant of the assumptions";
      if (uw.refiGap > 0) dcell.append(el("div", "re-refi-gap", "refi gap " + fmtMoneyShort(uw.refiGap) + " — LTV caps the takeout"));
      outs.append(dcell);
    } else {
      outs.append(el("div", "re-foot-note", "fill units + rent for screening outputs (NOI · ARV · DSCR)"));
    }
    host.append(outs);
    // initial vs real (decision 13): once locked, the frozen snapshot reads
    // against today's canon — hard total, drifted rocks, drifted inputs
    if (p.underwrite) {
      host.append(underwriteDeltaBlock(p));
    }
    // the deal worksheet is the underwriting home (decision 11) — a solo
    // property underwrites as its own deal
    const dealFoot = el("div", "re-uw-dealfoot");
    if (p.deal) {
      const d = dealCache.find((x) => x.slug === p.deal || (x.name || "") === p.deal);
      const go = el("button", "pp3-link", "deal worksheet: " + (d ? (d.name || d.slug) : p.deal) + " ↗");
      go.onclick = () => { location.hash = "#/properties/deal/" + encodeURIComponent(d ? d.slug : p.deal); };
      dealFoot.append(go);
    } else {
      const mk = el("button", "pp3-link ghost", "＋ underwrite as its own deal");
      mk.title = "a solo property is its own deal (creates the deal record + tethers this property)";
      mk.onclick = async () => {
        try {
          const res = await postJSONOk("/api/deals", { property: p.slug });
          location.hash = "#/properties/deal/" + encodeURIComponent(res.slug);
        } catch (e) { showToast("Couldn't create the deal — " + (e.message || "")); }
      };
      dealFoot.append(mk);
    }
    host.append(dealFoot);
  })();
  return sec;
}

// unitMixEditor — the frontmatter `units:` list, editable IN PLACE (owner
// ask 2026-08-18: "# of units and cost/unit/mo — list is better"). Rows of
// label · bd · ba · sqft · rent/mo; edits save on blur; the record stays
// hand-editable in Obsidian (the endpoint writes the same line).
function unitMixEditor(p) {
  const box = el("div", "re-unitmix");
  const head = el("div", "re-uw-label", "UNIT MIX");
  box.append(head);
  const rows = (p.unitMix || []).map((u) => ({ ...u }));
  const save = async () => {
    const clean = rows.filter((u) => (u.label || "").trim() || u.beds || u.sqft || u.rent);
    try {
      const fresh = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/measurables",
        { setUnits: true, units: clean });
      applyFreshProperty(fresh);
    } catch (e) { showToast("Couldn't save the unit mix — " + (e.message || "")); }
  };
  const body = el("div", "re-unitmix-rows");
  const renderRows = () => {
    body.innerHTML = "";
    if (rows.length) {
      const cols = el("div", "re-unitmix-cols");
      ["UNIT", "BD", "BA", "SQFT", "RENT/MO", ""].forEach((h) => cols.append(el("span", "", h)));
      body.append(cols);
    }
    rows.forEach((u, i) => {
      const line = el("div", "re-unitmix-row");
      const cell = (key, placeholder, numeric) => {
        const inp = inputEl(placeholder);
        inp.className = "pp-in re-unitmix-in";
        inp.value = u[key] ? String(u[key]) : "";
        inp.onblur = () => {
          const raw = inp.value.trim();
          const next = numeric ? (parseFloat(raw.replace(/[,$]/g, "")) || 0) : raw;
          if (next === (u[key] || (numeric ? 0 : ""))) return;
          u[key] = next;
          save();
        };
        return inp;
      };
      line.append(cell("label", "A", false), cell("beds", "", true), cell("baths", "", true),
        cell("sqft", "", true), cell("rent", "", true));
      const x = el("button", "pp3-stage-x re-unitmix-x", "✕");
      x.title = "remove unit";
      x.onclick = () => { rows.splice(i, 1); save(); };
      line.append(x);
      body.append(line);
    });
    if (rows.length) {
      body.append(el("div", "re-uw-unit re-uw-unitsum",
        rows.length + " units · " + fmtMoney(rows.reduce((n, u) => n + (u.rent || 0), 0)) + "/mo — drives screening"));
    }
  };
  renderRows();
  box.append(body);
  box.append(ghostInput("＋ unit", "re-unitmix-add", (v) => {
    rows.push({ label: v.trim() || String.fromCharCode(65 + rows.length) });
    save();
  }, "unit label (A, B, garden studio…)"));
  return box;
}

// measurablesEditor — the free numeric frontmatter keys as editable chips
// (windows 14 · roof-squares 22 · …). "name value" adds one; the value is
// click-to-edit; ✕ removes the line from the record.
function measurablesEditor(p) {
  const box = el("div", "re-uw-measurables");
  box.append(el("span", "re-uw-label", "MEASURABLES "));
  const post = async (body, failMsg) => {
    try {
      applyFreshProperty(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/measurables", body));
    } catch (e) { showToast(failMsg + " — " + (e.message || "")); }
  };
  Object.entries(p.measurables || {}).sort((a, b) => a[0].localeCompare(b[0])).forEach(([k, v]) => {
    const chip = el("span", "re-meas-chip");
    chip.append(el("span", "re-meas-key", k));
    const val = el("button", "re-meas-val", String(v));
    val.title = "click to edit";
    val.onclick = () => {
      const inp = inputEl("");
      inp.className = "pp-in re-meas-edit";
      inp.value = String(v);
      inp.onblur = () => {
        const n = parseFloat(inp.value.replace(/[,$]/g, ""));
        if (isNaN(n) || n === v) { if (inp.parentNode) inp.replaceWith(val); return; }
        post({ set: { [k]: n } }, "Couldn't save " + k);
      };
      inp.addEventListener("keydown", (ev) => { if (ev.key === "Enter") inp.blur(); });
      val.replaceWith(inp);
      inp.focus();
    };
    const x = el("button", "re-meas-x", "✕");
    x.title = "remove " + k;
    x.onclick = () => post({ remove: [k] }, "Couldn't remove " + k);
    chip.append(val, x);
    box.append(chip);
  });
  box.append(ghostInput("＋ measurable", "re-meas-add", (v) => {
    const m = v.trim().match(/^([a-z][a-z0-9-]*)\s+\$?([\d,.]+)$/i);
    if (!m) { showToast("Format: name value — e.g. windows 14"); return; }
    post({ set: { [m[1].toLowerCase()]: parseFloat(m[2].replace(/,/g, "")) } }, "Couldn't add " + m[1]);
  }, "windows 14…"));
  return box;
}

// underwriteDeltaBlock — the look-back seed: locked est vs today's canon.
// Rows render only where something moved; committed reads beside the ests.
function underwriteDeltaBlock(p) {
  const lock = p.underwrite;
  const box = el("div", "re-uw-lockblock");
  const head = el("div", "re-lane-head", "INITIAL UNDERWRITE — locked " + (p.locked || (lock.locked_at || "").slice(0, 10)));
  box.append(head);
  const row = (label, was, now, extra) => {
    const r = el("div", "re-uw-lockrow");
    r.append(el("span", "re-uw-lockname", label));
    const drift = Math.abs((now || 0) - (was || 0)) > 0.5;
    r.append(el("span", "re-uw-lockwas", fmtMoneyShort(was || 0)));
    r.append(el("span", "re-uw-locknow" + (drift ? (now > was ? " up" : " down") : ""),
      drift ? "→ " + fmtMoneyShort(now || 0) : "· held"));
    if (extra) r.append(el("span", "re-uw-lockextra", extra));
    box.append(r);
  };
  const hardNow = (p.work || []).reduce((n, st) => n + (st.estTotal || 0), 0);
  const committed = (p.rollup || {}).committed || 0;
  row("hard costs (Σ rock ests)", lock.hard_total, hardNow, committed ? "committed " + fmtMoneyShort(committed) : "");
  // per-rock drift, changed rows only
  const nowByID = {};
  (p.work || []).forEach((st) => { nowByID[st.id] = st.estTotal || 0; });
  (lock.rocks || []).forEach((r0) => {
    const now = nowByID[r0.id];
    if (now === undefined || Math.abs(now - r0.estTotal) < 0.5) return;
    row("· " + r0.text, r0.estTotal, now);
  });
  const srcNow = p.__source || {};
  if (lock.source) {
    const now = reSrcNum(srcNow, "purchase_price");
    if (Math.abs(now - (lock.source.PurchasePrice || 0)) > 0.5) {
      row("purchase price", lock.source.PurchasePrice, now);
    }
  }
  return box;
}
