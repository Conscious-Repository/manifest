// ---- PROPERTY PAGE — Revision 3: one property, one scroll ----
// Address + status select · BUDGET/SPENT strip (BUDGET opens the underwrite
// editor — the plan inputs behind the figure) · TODOS (composer always
// present; selecting a row opens the assignment inspector) · LEDGER (rows
// edit in place, ✕ deletes, a composer adds) · side card: parcel outline +
// OWNER of record (assessor-stamped frontmatter, editable). Money: budget
// derives from source.json underwrite + `## work` est; spent from the ledger.
let propSelTaskId = null; // line-id of the inspector's todo
let propPageSlug = null;  // page-local UI state resets when this changes
let propUWOpen = false;   // underwrite editor visible
let propLedgerEdit = -1;  // index of the ledger row being edited (-1 none)
let propLedgerAdd = false;
let propGeoCache = {};    // slug → geo features (parcel polygon)

async function renderPropertyPage(slug) {
  const host = els.propertyPage;
  host.innerHTML = "";
  if (slug !== propPageSlug) {
    propPageSlug = slug;
    propUWOpen = false; propLedgerEdit = -1; propLedgerAdd = false;
  }
  const p = propertyCache.find((x) => x.slug === slug);
  if (!p) { host.append(el("div", "pp-empty", "Property not found.")); return; }

  // title row: address + status select
  const head = el("div", "pp3-head");
  const title = el("h2", "pp3-title", p.short || p.address || p.slug);
  title.title = p.address || "";
  head.append(title, statusSelect(p, () => renderProperties()));
  const openNote = el("button", "pp3-note", "note ↗");
  openNote.title = "open the record (⌘/ edits raw)";
  openNote.onclick = () => { location.hash = "#/note/" + encodeURIComponent(p.path); };
  head.append(openNote);
  host.append(head);

  // owner line (RE spec §2 OWNER): the books it lands on; the seller while
  // acquiring reads in ink. Click-to-edit both.
  const ownerLine = el("div", "pp3-owner-line" + (p.from ? " acquiring" : ""));
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
  host.append(ownerLine);


  const cols = el("div", "pp3-cols");
  const main = el("div", "pp3-main");
  cols.append(main, propSideCard(p));
  host.append(cols);

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
  sCell.title = "derived from the ledger below";
  strip.append(sCell);
  main.append(strip);

  if (propUWOpen) {
    const uw = el("div", "pp3-uw");
    uw.append(el("div", "pp3-uw-loading", "loading underwrite…"));
    main.append(uw);
    renderUnderwrite(p, uw);
  }

  // DECISIONS lane (overhaul decision 9): every open [decision::] node in the
  // tree surfaces here with its rock context; the lines themselves stay under
  // their rock/milestone in the file. Never ages; resolves with a note.
  const decisions = [];
  const walkNodes = (rock, nodes, fn) => (nodes || []).forEach((n) => { fn(rock, n); walkNodes(rock, n.children, fn); });
  (p.work || []).forEach((st) => walkNodes(st, st.tasks, (rock, n) => { if (n.decision) decisions.push({ rock, n }); }));
  const openDecs = decisions.filter((d) => !d.n.checked);
  if (decisions.length) {
    const lane = el("div", "pp3-sec pp3-dec-lane");
    const dh = el("div", "pp3-sec-head");
    dh.append(el("span", "pp3-sec-title", "◇ DECISIONS"), el("span", "pp3-sec-count", openDecs.length + " open"));
    lane.append(dh);
    openDecs.forEach(({ rock, n }) => {
      const row = el("div", "pp3-todo pp3-dec-row");
      const resolve = el("button", "tdo-check", "◇");
      resolve.title = "resolve with a note";
      resolve.onclick = async (e) => {
        e.stopPropagation();
        const note = prompt("Resolution:", "");
        if (note === null) return;
        if (note.trim()) await propWorkOp(p, { op: "set-field", id: n.id, field: "resolution", value: note.trim() }, true);
        propWorkOp(p, { op: "check", id: n.id, checked: true });
      };
      row.append(resolve, el("span", "pp3-todo-text", n.text));
      row.append(el("span", "pp3-dec-rock", rock.text || ""));
      row.append(el("span", "prop-owner" + (mineOwner(n.owner) ? " mine" : ""), assigneeName(n.owner)));
      lane.append(row);
    });
    const decided = decisions.filter((d) => d.n.checked);
    if (decided.length) {
      const body = collapsibleSection(lane, "decided", String(decided.length), false);
      decided.forEach(({ n }) => {
        const row = el("div", "pp3-todo done pp3-dec-row");
        row.append(el("span", "tdo-check on", "◆"), el("span", "pp3-todo-text", n.text + (n.resolution ? " — " + n.resolution : "")));
        body.append(row);
      });
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
    const line = el("div", "pp3-stage" + (st.checked ? " done" : ""));
    // glyph doubles as the done toggle (server stamps [done:: date] on check)
    const glyph = el("button", "pp3-stage-glyph pp3-stage-check", st.checked ? "✓" : "○");
    glyph.title = st.checked ? "mark rock not done" : "mark rock done";
    glyph.onclick = (e) => { e.stopPropagation(); propWorkOp(p, { op: "check", id: st.id, checked: !st.checked }); };
    line.append(glyph);
    // name — click to rename in place
    const name = el("span", "pp3-stage-name", st.text || "");
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
    // done-by chip — the schedule date (click to set; ink when past due)
    const dbCls = "pp3-doneby" + (!st.checked && st.doneBy && st.doneBy < today ? " late" : "");
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
    stagesSec.append(line);
    // nodes: milestones render as sub-heads with their own children +
    // composer; tasks render as todo rows; decisions live in the lane above
    const nodeRow = (n, depth) => {
      if (n.decision) return;
      if (n.milestone) {
        const ml = el("div", "pp3-milestone" + (n.checked ? " done" : ""));
        const mglyph = el("button", "pp3-stage-glyph pp3-stage-check", n.checked ? "✓" : "◇");
        mglyph.title = n.checked ? "reopen milestone" : "mark milestone done";
        mglyph.onclick = (e) => { e.stopPropagation(); propWorkOp(p, { op: "check", id: n.id, checked: !n.checked }); };
        const mname = el("span", "pp3-milestone-name", n.text || "");
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
        appendContractChips(ml, n);
        ml.append(mdel);
        ml.classList.add("nested");
        stagesSec.append(ml);
        (n.children || []).forEach((c) => nodeRow(c, depth + 1));
        if (!n.checked) {
          const add = el("div", "pp3-compose nested deep");
          add.append(el("span", "pp3-compose-glyph", "○"));
          const input = inputEl("add to " + (n.text || "milestone") + "…");
          input.className = "pp3-compose-in";
          input.addEventListener("keydown", (ev) => {
            if (ev.key !== "Enter" || !input.value.trim()) return;
            propWorkOp(p, { op: "add-task", stageId: n.id, text: input.value.trim() });
          });
          add.append(input);
          stagesSec.append(add);
        }
        return;
      }
      const row = propTodoRow(p, { id: n.taskId, text: n.text, checked: !!n.checked, owner: n.owner || "" });
      row.classList.add("nested");
      if (depth > 0) row.classList.add("deep");
      appendContractChips(row, n);
      stagesSec.append(row);
      (n.children || []).forEach((c) => nodeRow(c, depth + 1));
    };
    (st.tasks || []).forEach((n) => nodeRow(n, 0));
    if (!st.checked) {
      const add = el("div", "pp3-compose nested");
      add.append(el("span", "pp3-compose-glyph", "○"));
      const input = inputEl("add a task to " + (st.text || "this rock") + "…  (milestone: / decision: …)");
      input.className = "pp3-compose-in";
      const submit = () => {
        const text = input.value.trim();
        if (!text) return;
        const m = text.match(/^milestone:\s*(.+)$/i);
        if (m) propWorkOp(p, { op: "add-task", stageId: st.id, text: m[1], kind: "milestone" });
        else propWorkOp(p, { op: "add-task", stageId: st.id, text });
      };
      input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") submit(); });
      add.append(input);
      stagesSec.append(add);
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

  // restore an open inspector across re-renders
  if (propSelTaskId) {
    const t = (p.tasks || []).find((x) => x.id === propSelTaskId);
    if (t) openPropInspector(p, t); else closePropInspector();
  }
}

// ---- side card: located parcel thumb + owner of record ----

let _thumbMap = null; // the live mini-map instance (rebuilt per render)

function propSideCard(p) {
  const side = el("div", "pp3-side");
  const thumbSlot = el("div", "pp-thumb-slot");
  side.append(thumbSlot);
  loadPropGeo(p.slug).then((features) => mountParcelThumb(thumbSlot, features));
  side.append(ownerCard(p));
  return side;
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

  if (p.control === "owned" && p.entity) {
    card.append(el("div", "pp3-owner-name", p.entity));
    // title check: the deed vesting per the assessor, when it disagrees
    if (p.owner && p.owner.toLowerCase() !== p.entity.toLowerCase()) {
      card.append(line("deed: " + p.owner, "warn"));
    }
    if (p.ownerSince) card.append(line("since " + p.ownerSince));
  } else {
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
  row.append(el("span", "pp3-ledger-vendor", r.vendor || r.contractor || ""));
  row.append(el("span", "pp3-ledger-amt", r.amount ? fmtMoney(r.amount) : ""));
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
  const type = selectEl(["expense", "bid"]);
  type.className = "pp-in"; type.value = r && r.type === "bid" ? "bid" : "expense";
  const status = selectEl([]);
  status.className = "pp-in";
  const setStatusOpts = () => {
    status.innerHTML = "";
    (type.value === "bid" ? ["requested", "received", "accepted", "declined"] : ["paid"]).forEach((s) => {
      const o = document.createElement("option"); o.value = s; o.textContent = s; status.append(o);
    });
    if (r && r.status && [...status.options].some((o) => o.value === r.status)) status.value = r.status;
  };
  setStatusOpts();
  type.onchange = setStatusOpts;
  const category = inputEl("category"); category.className = "pp-in"; category.value = r ? (r.category || "") : "";
  const vendor = inputEl("vendor"); vendor.className = "pp-in"; vendor.value = r ? (r.vendor || "") : "";
  const contractor = inputEl("contractor"); contractor.className = "pp-in"; contractor.value = r ? (r.contractor || "") : "";
  const amount = moneyInput("$", r ? r.amount : 0); amount.value = r && r.amount ? r.amount : "";
  const note = inputEl("note"); note.className = "pp-in"; note.value = r ? (r.note || "") : "";
  grid.append(labeled("date", date), labeled("type", type), labeled("status", status),
    labeled("category", category), labeled("vendor", vendor), labeled("contractor", contractor),
    labeled("amount", amount), labeled("note", note));
  // quick-add hops (§7): property → node → contract. The contract list =
  // accepted contracts with allocations on THIS property, showing remaining.
  let nodeSel = null, contractSel = null;
  if (!r) {
    nodeSel = selectEl([]);
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
    contractSel = selectEl([]);
    contractSel.className = "pp-in";
    const copt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; contractSel.append(o); };
    copt("", "— no contract —");
    (typeof reContracts === "function" ? reContracts() : [])
      .filter((c) => c.status === "accepted" && (c.allocations || []).some((a) => a.property === p.slug))
      .forEach((c) => copt(c.slug, c.name + " · " + fmtMoney(c.remaining != null ? c.remaining : c.total) + " left"));
    grid.append(labeled("node", nodeSel), labeled("contract", contractSel));
  }
  form.append(grid);
  if (r && r.workId) form.append(el("div", "pp3-uw-note", "tethered to work [" + r.workId + "] — kept"));
  if (r && r.contract) form.append(el("div", "pp3-uw-note", "draws contract [" + r.contract + "] — kept"));

  const actions = el("div", "pp3-uw-actions");
  const cancel = el("button", "pp3-uw-cancel", "cancel");
  cancel.onclick = () => { propLedgerEdit = -1; propLedgerAdd = false; renderPropertyPage(p.slug); };
  const save = el("button", "pp3-compose-go", r ? "save ↵" : "add ↵");
  save.onclick = async () => {
    const amt = parseFloat(amount.value) || 0;
    if (!amt) { showToast("Amount is required"); return; }
    try {
      if (r) {
        // rebuild the note with the row's hidden tokens intact — the server
        // reconstructs the on-disk note from note+workId only
        let n = note.value.trim();
        if (r.contract) n += " [contract:: " + r.contract + "]";
        if (r.cat) n += " [cat:: " + r.cat + "]";
        if (r.paidBy) n += " [paid-by:: " + r.paidBy + "]";
        if (r.stmt) n += " [stmt:: " + r.stmt + "]";
        const replacement = {
          date: date.value.trim(), type: type.value, category: category.value.trim(),
          vendor: vendor.value.trim(), contractor: contractor.value.trim(), amount: amt,
          status: status.value, note: n.trim(), doc: r.doc || "", workId: r.workId || "",
        };
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate", { original: r, replacement });
      } else {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger", {
          date: date.value.trim(), type: type.value, category: category.value.trim(),
          vendor: vendor.value.trim(), contractor: contractor.value.trim(), amount: amt,
          status: status.value, note: note.value.trim(),
          workId: nodeSel ? nodeSel.value : "", contract: contractSel ? contractSel.value : "",
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

function compositeId(p, t) { return "prop:" + p.slug + "/" + t.id; }

// appendContractChips — a node's accepted-contract slices (the committed
// source) render as chips linking to the contract page.
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
    await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work", body);
    if (!quiet) renderProperties();
  } catch (e) { showToast("Couldn't update — " + (e.message || "")); }
}

function propTodoRow(p, t) {
  const row = el("div", "pp3-todo" + (t.checked ? " done" : "") + (propSelTaskId === t.id ? " sel" : ""));
  const check = el("button", "tdo-check" + (t.checked ? " on" : ""), t.checked ? "✓" : "○");
  check.title = t.checked ? "reopen" : "done";
  check.onclick = async (e) => {
    e.stopPropagation();
    try { await postJSONOk("/api/tasks/check", { id: compositeId(p, t), checked: !t.checked }); renderProperties(); }
    catch (err) { showToast("Couldn't update"); }
  };
  row.append(check, el("span", "pp3-todo-text", t.text));
  row.append(el("span", "prop-owner" + (mineOwner(t.owner) ? " mine" : ""), assigneeName(t.owner)));
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
  row.onclick = () => {
    propSelTaskId = propSelTaskId === t.id ? null : t.id;
    propSelTaskId ? openPropInspector(p, t) : closePropInspector();
    // repaint selection without a full reload
    els.propertyPage.querySelectorAll(".pp3-todo.sel").forEach((n) => n.classList.remove("sel"));
    if (propSelTaskId) row.classList.add("sel");
  };
  return row;
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

// ---- the 280px assignment inspector (Rev 3's accountability half) ----
function openPropInspector(p, t) {
  // phone (Rev 4): the sticky 280px column is display:none — the same builder
  // fills a bottom sheet. Desktop path identical.
  const phone = window.mf && window.mf.phone();
  const host = phone
    ? window.mfSheet.body("prop", closePropInspector, () => openPropInspector(p, t))
    : els.propInspector;
  host.innerHTML = "";
  if (!phone) host.hidden = false;
  const head = el("div", "pp3-insp-head");
  head.append(el("span", "pp3-insp-label", "Inspector"));
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
  // assignee: a real identity from the RE registry ONLY — system/realestate/
  // people.md + contractor records; the aion roster never reaches properties
  const sel = document.createElement("select");
  sel.className = "pp-in";
  const opt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; sel.append(o); };
  opt("", "you");
  const a = (propTodosMeta && propTodosMeta.assignees) || {};
  (a.realestate || []).forEach((c) => opt(c.slug, c.name + (c.trade ? " (" + c.trade + ")" : "")));
  sel.value = mineOwner(t.owner) ? "" : t.owner; // BA/me/empty all read as "you"
  const note = el("div", "pp3-insp-note");
  const setNote = () => {
    note.textContent = sel.value
      ? "Assigned to " + assigneeName(sel.value) + " — tracked here, never in your TASKS. It shows under Outstanding until they close it."
      : "Yours — it shows in TASKS under Real Estate.";
  };
  setNote();
  const assign = async (owner) => {
    try {
      await postJSONOk("/api/tasks/update", { id: compositeId(p, t), owner });
      t.owner = owner;
      setNote();
      renderProperties();
    } catch (e) { showToast("Couldn't assign"); }
  };
  sel.onchange = () => assign(sel.value);
  if (phone) {
    // Rev 4: a tap-list, not a <select> — 48px rows, ● on the current one.
    const current = mineOwner(t.owner) ? "" : t.owner;
    const list = el("div", "mf-assign");
    const rowOpt = (v, l) => {
      const r = el("button", "mf-opt" + (v === current ? " on" : ""));
      r.append(el("span", "mf-opt-dot", v === current ? "●" : "○"), el("span", "", l));
      r.onclick = () => assign(v).then(() => openPropInspector(p, t)); // re-fill in place (same key)
      list.append(r);
    };
    rowOpt("", "you");
    (a.realestate || []).forEach((c) => rowOpt(c.slug, c.name + (c.trade ? " (" + c.trade + ")" : "")));
    host.append(field("owner", list));
  } else {
    host.append(field("owner", sel));
  }
  // rock: move the task under another of THIS property's rocks (the tree IS
  // the placement) — the server refuses names outside the pipeline
  if ((p.work || []).length) {
    const stSel = document.createElement("select");
    stSel.className = "pp-in";
    const sopt = (v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; stSel.append(o); };
    const home = (p.work || []).find((st) =>
      (st.tasks || []).some(function inTree(n) { return n.taskId === t.id || (n.children || []).some(inTree); }));
    sopt("", home ? home.text : "—");
    (p.work || []).filter((st) => !home || st.id !== home.id).forEach((st) => sopt(st.text, st.text));
    stSel.value = "";
    stSel.onchange = async () => {
      if (!stSel.value) return;
      try {
        await postJSONOk("/api/tasks/update", { id: compositeId(p, t), stage: stSel.value });
        renderProperties();
      } catch (e) { showToast("Couldn't move the task — " + (e.message || "")); }
    };
    host.append(field("rock", stSel));
  }
  host.append(field("property", el("span", "pp3-insp-val", p.short || p.address || p.slug)));
  if (t.added) host.append(field("added", el("span", "pp3-insp-val", t.added)));
  host.append(note);
}

function closePropInspector() {
  propSelTaskId = null;
  if (els.propInspector) { els.propInspector.hidden = true; els.propInspector.innerHTML = ""; }
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
      try { src = p.__source = await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/source")).json(); }
      catch (e) { src = p.__source = {}; }
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
    grid.append(
      input("purchase price", "purchase_price"),
      input("hard costs", "hard_costs"),
      input("units", "total_units"),
      input("stabilized rent /unit/mo", "avg_rent_per_unit"),
    );
    host.append(grid);
    const uw = reScreeningCalc(p);
    const outs = el("div", "pp3-strip re-uw-outs");
    const cell = (label, val, cls) => {
      const c = el("div", "pp3-cell");
      c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val" + (cls ? " " + cls : ""), val));
      return c;
    };
    if (uw.complete) {
      outs.append(cell("NOI", fmtMoneyShort(uw.noi)));
      outs.append(cell("ARV", fmtMoneyShort(uw.arv)));
      outs.append(cell("DSCR", uw.dscr ? uw.dscr.toFixed(2) : "—", uw.dscr && uw.dscr < 1.25 ? "over" : ""));
    } else {
      outs.append(el("div", "re-foot-note", "fill units + rent for screening outputs (NOI · ARV · DSCR)"));
    }
    host.append(outs);
  })();
  return sec;
}
