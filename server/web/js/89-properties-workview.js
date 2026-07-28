// ---- WORK view (pass-5 §4): kanban default + gantt toggle ----

let workViewMode = "kanban"; // kanban | gantt

async function renderWorkView() {
  const host = els.propertyWork; host.hidden = false; host.innerHTML = "loading…";
  try {
    const d = await (await fetch("/api/properties")).json();
    propertyCache = d.properties || [];
    dealCache = d.deals || [];
    templateCache = d.templates || [];
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Unavailable.")); return; }
  host.innerHTML = "";
  const bar = el("div", "stmt-chips");
  [["kanban", "KANBAN"], ["gantt", "GANTT"]].forEach(([val, label]) => {
    const c = el("button", "filter-chip" + (workViewMode === val ? " on" : ""), label);
    c.onclick = () => { workViewMode = val; renderWorkView(); };
    bar.append(c);
  });
  host.append(bar);
  const active = propertyCache.filter((p) => !p.hidden &&
    (bucketOf(p) === "active" || (p.work || []).length));
  els.propertiesMeta.textContent = active.length + " projects";
  if (!active.length) { host.append(emptyRow("No active projects.")); return; }
  if (workViewMode === "gantt") host.append(ganttView(active));
  else host.append(kanbanView(active));
}

// kanbanView: columns = union of template stage names in canonical order,
// custom names appended. One card per property in its current stage's column.
// No drag — advancement stays an explicit check on the property page.
function kanbanView(props) {
  const colNames = [];
  const seen = new Set();
  (templateCache || []).forEach((t) => (t.stages || []).forEach((st) => {
    const k = st.text.toLowerCase();
    if (!seen.has(k)) { seen.add(k); colNames.push(st.text); }
  }));
  props.forEach((p) => (p.work || []).forEach((st) => {
    const k = st.text.toLowerCase();
    if (!seen.has(k)) { seen.add(k); colNames.push(st.text); }
  }));
  const buckets = new Map(colNames.map((n) => [n.toLowerCase(), []]));
  const noPlan = [];
  const doneAll = [];
  props.forEach((p) => {
    const cur = (p.work || []).find((s) => s.current);
    if (!cur) { ((p.work || []).length ? doneAll : noPlan).push(p); return; }
    const k = cur.text.toLowerCase();
    if (!buckets.has(k)) buckets.set(k, []);
    buckets.get(k).push({ p, cur });
  });
  const board = el("div", "kanban");
  colNames.forEach((name) => {
    const cards = buckets.get(name.toLowerCase()) || [];
    if (!cards.length) return; // empty columns stay out of the way
    const col = el("div", "kanban-col");
    col.append(el("div", "kanban-head", name.toUpperCase() + " · " + cards.length));
    cards.forEach(({ p, cur }) => col.append(kanbanCard(p, cur)));
    board.append(col);
  });
  if (doneAll.length) {
    const col = el("div", "kanban-col");
    col.append(el("div", "kanban-head", "COMPLETE · " + doneAll.length));
    doneAll.forEach((p) => col.append(kanbanCard(p, null)));
    board.append(col);
  }
  if (noPlan.length) {
    const col = el("div", "kanban-col noplan");
    col.append(el("div", "kanban-head", "NO WORK PLAN · " + noPlan.length));
    noPlan.forEach((p) => col.append(kanbanCard(p, null)));
    board.append(col);
  }
  return board;
}

function kanbanCard(p, cur) {
  const card = el("div", "kanban-card");
  card.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
  const head = el("div", "kanban-card-head");
  head.append(el("span", "wv-addr", p.short || p.address || p.slug));
  // stall dot: active property with no open todo queued
  if (cur && !(cur.todos || []).some((t) => !t.checked)) head.append(el("span", "stall-dot", "●"));
  card.append(head);
  if (p.entity) card.append(el("div", "kanban-entity", p.entity));
  if (cur) {
    const next = (cur.todos || []).find((t) => !t.checked);
    if (next) card.append(el("div", "kanban-todo", "› " + next.text));
    else card.append(el("div", "kanban-todo warn", "no next action"));
  }
  const pm = projMoney(p);
  card.append(el("div", "kanban-money" + (pm.over ? " over" : ""),
    fmtMoney(pm.budget) + " budget · " + fmtMoney(pm.paid) + " spent"));
  return card;
}

// ganttView: pure SVG — one row per property, stage bars from the derived
// schedule, a today line; dragging a bar's RIGHT EDGE edits that stage's
// weeks (the one allowed direct manipulation).
function ganttView(props) {
  const withSched = props.filter((p) => (p.schedule || []).length);
  const wrap = el("div", "gantt-wrap");
  if (!withSched.length) {
    wrap.append(emptyRow("No schedules yet — set a work start date on a property page."));
    return wrap;
  }
  let min = Infinity, max = -Infinity;
  withSched.forEach((p) => p.schedule.forEach((sp) => {
    min = Math.min(min, +new Date(sp.start));
    max = Math.max(max, +new Date(sp.end));
  }));
  const today = Date.now();
  min = Math.min(min, today) - 7 * 864e5;
  max = Math.max(max, today) + 14 * 864e5;
  const W = 980, ROW = 34, LABEL = 190, H = withSched.length * ROW + 30;
  const x = (t) => LABEL + ((t - min) / (max - min)) * (W - LABEL - 10);
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "gantt");
  const mk = (tag, attrs, text) => {
    const n = document.createElementNS(svgNS, tag);
    for (const k in attrs) n.setAttribute(k, attrs[k]);
    if (text) n.textContent = text;
    return n;
  };
  // month gridlines
  const d0 = new Date(min); d0.setDate(1);
  for (let d = new Date(d0); +d < max; d.setMonth(d.getMonth() + 1)) {
    svg.append(mk("line", { x1: x(+d), y1: 18, x2: x(+d), y2: H, class: "g-grid" }));
    svg.append(mk("text", { x: x(+d) + 3, y: 12, class: "g-month" }, (d.getMonth() + 1) + "/" + String(d.getFullYear()).slice(2)));
  }
  withSched.forEach((p, i) => {
    const y = 24 + i * ROW;
    const label = mk("text", { x: 0, y: y + 14, class: "g-label" }, (p.short || p.address || p.slug).slice(0, 26));
    label.style.cursor = "pointer";
    label.addEventListener("click", () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); });
    svg.append(label);
    p.schedule.forEach((sp) => {
      const bx = x(+new Date(sp.start)), bw = Math.max(4, x(+new Date(sp.end)) - bx);
      const bar = mk("rect", { x: bx, y: y + 2, width: bw, height: 18, rx: 2,
        class: "g-bar" + (sp.done ? " done" : "") });
      svg.append(bar);
      if (bw > 46) svg.append(mk("text", { x: bx + 4, y: y + 15, class: "g-bartext" }, sp.text.slice(0, Math.floor(bw / 7))));
      if (!sp.done) { // drag the right edge → weeks (quantized, ≥1)
        const grip = mk("rect", { x: bx + bw - 4, y: y + 2, width: 8, height: 18, class: "g-grip" });
        let drag = null;
        grip.addEventListener("pointerdown", (e) => {
          e.preventDefault(); e.stopPropagation();
          drag = { startX: e.clientX, weeks: sp.weeks };
          grip.setPointerCapture(e.pointerId);
        });
        grip.addEventListener("pointermove", (e) => {
          if (!drag) return;
          const scale = (max - min) / (W - LABEL - 10); // ms per px
          const dWeeks = (e.clientX - drag.startX) * scale * (svg.clientWidth ? W / svg.clientWidth : 1) / (7 * 864e5);
          const w = Math.max(1, Math.round(drag.weeks + dWeeks));
          bar.setAttribute("width", Math.max(4, x(+new Date(sp.start) + w * 7 * 864e5) - bx));
          drag.next = w;
        });
        grip.addEventListener("pointerup", async (e) => {
          const w = drag && drag.next;
          drag = null;
          if (w && w !== sp.weeks) {
            try {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
                { op: "set-field", id: sp.id, field: "weeks", value: String(w) });
              renderWorkView();
            } catch (err) { showToast("Couldn't set weeks"); }
          }
        });
        svg.append(grip);
      }
    });
  });
  svg.append(mk("line", { x1: x(today), y1: 18, x2: x(today), y2: H, class: "g-today" }));
  wrap.append(svg);
  return wrap;
}

async function renderPropertyPage(slug) {
  els.propertyBoard.hidden = true; els.propertyMapWrap.hidden = true;
  els.propertyStatements.hidden = true; els.propertyContractors.hidden = true; els.propertyWork.hidden = true;
  const host = els.propertyPage; host.hidden = false; host.textContent = "loading…";
  try {
    const [p, srcRes, geoRes] = await Promise.all([
      (await fetch("/api/properties/" + encodeURIComponent(slug))).json(),
      fetch("/api/properties/" + encodeURIComponent(slug) + "/source").then((r) => r.json()).catch(() => ({})),
      fetch("/api/properties/geo?slug=" + encodeURIComponent(slug)).then((r) => r.json()).catch(() => ({})),
    ]);
    renderProp(p, srcRes.source || null, ((geoRes.records || [])[0] || {}).features || []);
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("Property not found.")); }
}

// renderProp (design §3): money you touch daily on top, reference collapsed
// below, prose last. Async: the TODOS strip reads the task substrate.
async function renderProp(p, src, geoFeatures) {
  const host = els.propertyPage; host.innerHTML = ""; host.hidden = false;
  els.propertyBoard.hidden = true; els.propertyMapWrap.hidden = true;

  const back = el("button", "pill light pp-back", "‹ board");
  back.onclick = () => { location.hash = "#/properties"; };
  host.append(back);

  // single-parcel deals carry the FULL deal object; members carry the slice.
  const isFullDeal = !!(src && Array.isArray(src.properties) && src.properties.length);
  const propSlice = isFullDeal ? src.properties[0] : (src || null);

  const head = el("div", "pp-head");
  const titleRow = el("div", "pp-title-row");
  const tcol = el("div", "pp-title-col");
  const h = el("h2", "pp-title", p.short || p.address || p.name);
  tcol.append(h);
  if (p.address && p.address !== (p.short || "")) tcol.append(el("span", "pp-fulladdr", p.address));
  if (propSlice && propSlice.parcel_id) tcol.append(el("span", "pp-parcel", propSlice.parcel_id));
  const chips = el("div", "pp-chips");
  chips.append(editChip(p, "status", p.status, PROPERTY_STATUSES));
  chips.append(el("span", "pp-chip", p.control));
  chips.append(editChip(p, "kind", p.kind, PROPERTY_KINDS));
  chips.append(entityChip(p));
  if (p.deal) {
    const dchip = el("a", "pp-chip pp-deal", "▸ " + p.deal);
    dchip.href = "#/properties/deal/" + encodeURIComponent(p.deal);
    chips.append(dchip);
  }
  tcol.append(chips);
  titleRow.append(tcol);
  const thumb = parcelThumb(geoFeatures);
  if (thumb) { thumb.onclick = () => { location.hash = "#/properties/map"; }; titleRow.append(thumb); }
  head.append(titleRow);
  host.append(head);

  // BUDGET · SPENT · REMAINING — plan vs spend. The plan (ests + underwriting)
  // IS the budget; over-budget shows red when any category's actuals exceed it.
  const pj = p.project;
  if (pj) {
    const sum = el("div", "pp-rollup");
    sum.append(rollupStat("budget", "", fmtMoney(pj.planTotal)));
    const spent = rollupStat("spent", fmtPct(pj.planTotal > 0 ? pj.paid / pj.planTotal : 0), fmtMoney(pj.paid));
    if (pj.over) spent.classList.add("over");
    sum.append(spent);
    sum.append(rollupStat("remaining", "", fmtMoney(pj.planTotal - pj.paid)));
    host.append(sum);
    const togo = el("div", "pp-togo");
    togo.append(el("span", "", "committed " + fmtMoney(pj.committed)));
    if (pj.unreconciled > 0) {
      const un = el("span", "pp-unrec", "⚑ unreconciled " + fmtMoney(pj.unreconciled));
      un.title = "work marked done whose firm price has no linked bank transaction yet — link payments in the statement workbench";
      togo.append(un);
    }
    if (pj.over) togo.append(el("span", "pp-over-note", "over budget in a category ↓"));
    host.append(togo);
  }

  // WORK — the management core (budget category table retired from the page;
  // over-budget still surfaces via the rollup pair + feed signal).
  host.append(el("div", "pp-section-head", "WORK"));
  host.append(workBlock(p));

  // TODOS strip (task-substrate §6): buckets whose heading [[links]] match
  // this property render their open todos here — read/write of to do.md only
  // (money and SOW stay in the realestate files).
  try {
    const tv = await (await fetch("/api/todos")).json();
    const slugOf = (s2) => String(s2 || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    const mine = [];
    (tv.domains || []).forEach((dm) => (dm.buckets || []).forEach((bk) => {
      const hit = (bk.links || []).some((l) => {
        const ls = slugOf(l);
        return ls === p.slug || p.slug.startsWith(ls) || ls.startsWith(p.slug) ||
          slugOf(p.address).startsWith(ls);
      });
      if (hit) mine.push({ dm, bk });
    }));
    if (mine.length) {
      host.append(el("div", "pp-section-head", "TODOS"));
      mine.forEach(({ dm, bk }) => {
        const box = el("div", "pp-todostrip");
        box.append(el("div", "uw-sub", bk.name + " · " + dm.name.toLowerCase()));
        bk.todos.filter((t) => t.state !== "done").forEach((t) => {
          const row = el("div", "tdo-row");
          const check = el("button", "check wk-check", "○");
          check.onclick = async () => {
            try { await postJSONOk("/api/todos/check", { id: t.id, checked: true }); } catch (e) {}
            renderPropertyPage(p.slug);
          };
          row.append(check, el("span", "tdo-text", t.text));
          if (t.issue) row.append(el("span", "tdo-tag", "⚑ " + t.issue.split("/").pop()));
          if (t.state === "waiting") row.append(el("span", "tdo-tag", "⏳ " + t.waiting));
          box.append(row);
        });
        box.append(ghostInput("＋ todo", "tdo-add", async (v) => {
          try { await postJSONOk("/api/todos/item", { text: v, domain: dm.name, bucket: bk.name }); } catch (e) {}
          renderPropertyPage(p.slug);
        }, "into " + bk.name + "…"));
        host.append(box);
      });
    }
  } catch (e) {}

  // SCHEDULE anchor (§3): one date + per-stage derived spans on the rows
  const sched = el("div", "pp-sched");
  const wsLabel = el("span", "uw-label", "WORK START");
  const wsBtn = el("button", "est-slot" + (p.workStart ? "" : " empty"), p.workStart || "set date");
  wsBtn.onclick = () => {
    const input = inputEl("YYYY-MM-DD");
    input.type = "date";
    if (p.workStart) input.value = p.workStart;
    input.addEventListener("change", async () => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "work-start", value: input.value });
        renderPropertyPage(p.slug);
      } catch (e) { showToast("Couldn't set work start"); }
    });
    wsBtn.replaceWith(input);
    input.focus();
  };
  sched.append(wsLabel, wsBtn);
  host.append(sched);

  // MONEY — read-only (pass-5: money enters ONLY via the statement workbench).
  host.append(el("div", "pp-section-head", "MONEY"));
  host.append(moneyBlock(p));

  // UNDERWRITING — collapsed reference; single-parcel deals get BOTH levels.
  if (src) {
    const summary = propSlice ?
      [propSlice.purchase_price ? "$" + Math.round(propSlice.purchase_price / 1000) + "k purch" : "",
        propSlice.total_units ? propSlice.total_units + "u" : "",
        propSlice.total_sf ? propSlice.total_sf.toLocaleString() + "sf" : ""].filter(Boolean).join(" · ") : "";
    const headEl = el("div", "pp-section-head tag-engine", "UNDERWRITING · FEEDS SITE ENGINE");
    host.append(headEl);
    const body = collapsibleSection(host, propSlice === src ? "PROPERTY FIELDS" : "FIELDS", summary, false);
    const dirty = makeDirtyBar(host,
      async () => {
        await putJSON("/api/properties/" + encodeURIComponent(p.slug) + "/source", src);
        showToast("Saved — source.json updated");
      },
      () => renderPropertyPage(p.slug));
    if (propSlice) propertyLevelSections(body, propSlice, dirty,
      { derivedHard: (p.work || []).some((st) => st.estTotal > 0) });
    if (isFullDeal) {
      const dealBody = collapsibleSection(host, "DEAL-LEVEL (this record IS the deal)", "", false);
      dealLevelSections(dealBody, src, dirty);
      documentsSection(dealBody, src, dirty, p.slug, "single");
    }
  }

  const logSum = (p.log || []).length ? p.log.length + " lines · " + (p.lastLog || "").slice(0, 44) : "empty";
  collapsibleSection(host, "LOG", logSum, false).append(logBlock(p));
  collapsibleSection(host, "DOCS", "click to open", false).append(docsBlock(p));

  const edit = el("button", "pill light pp-editnote", "edit note / prose →");
  edit.onclick = () => { _noteReturn = "#/properties/" + encodeURIComponent(p.slug); openNoteByPath(p.path); };
  host.append(edit);
}

// parcelThumb: the parcel polygon as a quiet inline SVG (no tiles, no Leaflet).
function parcelThumb(features) {
  if (!features || !features.length) return null;
  let pts = [];
  features.forEach((f) => {
    const g = f.geometry || {};
    if (g.type === "Polygon") (g.coordinates || []).forEach((ring) => pts.push(...ring));
  });
  if (pts.length < 3) return null;
  const xs = pts.map((c) => c[0]), ys = pts.map((c) => c[1]);
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
  const W = 180, H = 120, pad = 10;
  const sx = (W - 2 * pad) / (maxX - minX || 1), sy = (H - 2 * pad) / (maxY - minY || 1);
  const s = Math.min(sx, sy);
  const px = (c) => (pad + (c[0] - minX) * s).toFixed(1) + "," + (H - pad - (c[1] - minY) * s).toFixed(1);
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "pp-thumb");
  features.forEach((f) => {
    const g = f.geometry || {};
    if (g.type !== "Polygon") return;
    (g.coordinates || []).forEach((ring) => {
      const poly = document.createElementNS(svgNS, "polygon");
      poly.setAttribute("points", ring.map(px).join(" "));
      svg.append(poly);
    });
  });
  return svg;
}

// entityChip: the property's entity is a HARD LINK to an entity record — the
// chip swaps to the record autocomplete (pick an existing entity, or create one
// via the quiet completion, which makes the record first). Never free text.
function entityChip(p) {
  const chip = el("span", "pp-chip editable", p.entity || "entity: —");
  chip.title = "click to link an entity (from SETTINGS records)";
  chip.onclick = () => {
    const ac = recordAutocomplete("entity", "entity…", async (rec) => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "entity", value: rec.name });
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Couldn't link entity").slice(0, 80)); ac.el.replaceWith(chip); }
    });
    if (p.entity) ac.setValue(p.entity);
    const clear = el("button", "uw-x", "✕");
    clear.title = "unlink entity";
    clear.onclick = async () => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "entity", value: "" });
        renderPropertyPage(p.slug);
      } catch (err) { showToast("Couldn't clear"); }
    };
    const wrap = el("span", "pp-chip-edit-wrap");
    wrap.append(ac.el, clear);
    const input = ac.el.querySelector("input");
    input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") wrap.replaceWith(chip); });
    input.addEventListener("blur", () => setTimeout(() => { if (wrap.parentNode) wrap.replaceWith(chip); }, 200));
    chip.replaceWith(wrap);
    ac.focus();
  };
  return chip;
}

// editChip is a page chip that swaps to a select (enum) or text input (free) and
// POSTs the field edit — the property page's click-to-edit for status/kind/entity.
function editChip(p, key, value, options) {
  const label = value ? (key === "entity" ? value : value) : key + ": —";
  const chip = el("span", "pp-chip editable", label);
  chip.title = "click to edit " + key;
  chip.onclick = () => {
    let ctl;
    if (options) { ctl = selectEl(options); ctl.value = value || options[0]; }
    else { ctl = inputEl(key + "…"); ctl.value = value || ""; }
    ctl.classList.add("pp-chip-edit");
    const save = async () => {
      const v = options ? ctl.value : ctl.value.trim();
      if (v === (value || "") && options) { ctl.replaceWith(chip); return; }
      try { await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key, value: v }); renderPropertyPage(p.slug); }
      catch (err) { showToast("Couldn't update " + key); ctl.replaceWith(chip); }
    };
    if (options) { ctl.onchange = save; ctl.onblur = () => { if (ctl.parentNode) ctl.replaceWith(chip); }; }
    else {
      ctl.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter") save();
        else if (ev.key === "Escape") ctl.replaceWith(chip);
      });
      ctl.addEventListener("blur", save);
    }
    chip.replaceWith(ctl);
    ctl.focus();
  };
  return chip;
}

function rollupStat(label, pct, money) {
  const s = el("div", "pp-stat");
  s.append(el("div", "pp-stat-big", pct || money));
  s.append(el("div", "pp-stat-sub", pct ? money : ""));
  s.append(el("div", "pp-stat-label", label));
  return s;
}

// normDate coerces common bank formats (MM/DD/YYYY, YYYY-MM-DD) to ISO.
function normDate(s) {
  s = String(s).trim();
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10);
  const m = s.match(/^(\d{1,2})\/(\d{1,2})\/(\d{2,4})/);
  if (m) {
    let y = m[3].length === 2 ? "20" + m[3] : m[3];
    return y + "-" + m[1].padStart(2, "0") + "-" + m[2].padStart(2, "0");
  }
  return "";
}

// ---- MONEY block (pass-5 §1): read-only — per-stage triplet + recent
// activity with grouped payments. Money enters ONLY via the workbench. ----
let moneyShowAll = false;

function moneyBlock(p) {
  const wrap = el("div", "pp-money");
  // category table: the four project-cost lines, plan vs spend
  if (p.project && (p.project.categories || []).length) {
    const cats = el("div", "pp-money-stages");
    cats.append(ppCols("cols-cats", ["CATEGORY", "BUDGET", "COMMITTED", "PAID"]));
    p.project.categories.forEach((c) => {
      const row = el("div", "pp-money-row cols-cats" + (c.over ? " over" : ""));
      row.append(el("span", "", c.key),
        el("span", "pp-amt", c.budget ? fmtMoney(c.budget) : "—"),
        el("span", "pp-amt", c.committed ? fmtMoney(c.committed) : ""),
        el("span", "pp-amt", c.paid ? fmtMoney(c.paid) : ""));
      cats.append(row);
    });
    wrap.append(cats);
  }
  // per-stage est · committed · paid
  if ((p.work || []).length) {
    const tbl = el("div", "pp-money-stages");
    tbl.append(ppCols("cols-money", ["STAGE", "EST", "COMMITTED", "PAID"]));
    p.work.forEach((st) => {
      const row = el("div", "pp-money-row cols-money" + (st.estTotal > 0 && st.committed > st.estTotal ? " over" : ""));
      row.append(el("span", "", st.text),
        el("span", "pp-amt", st.estTotal ? fmtMoney(st.estTotal) : "—"),
        el("span", "pp-amt", st.committed ? fmtMoney(st.committed) : ""),
        el("span", "pp-amt", st.paid ? fmtMoney(st.paid) : ""));
      tbl.append(row);
    });
    wrap.append(tbl);
  }

  // recent activity: applied expenses newest-first; rows sharing a workId under
  // an accepted bid GROUP into one expandable line ("merge = grouping, never
  // destruction" — the verbatim transactions live inside).
  const paid = (p.ledger || []).filter((r) => r.type === "expense")
    .sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  const acceptedByWork = {};
  (p.ledger || []).forEach((r) => {
    if (r.type === "bid" && r.status === "accepted" && r.workId) acceptedByWork[r.workId] = r;
  });
  const groups = new Map(); // workId → rows (only when an accepted bid anchors it)
  const singles = [];
  paid.forEach((r) => {
    if (r.workId && acceptedByWork[r.workId]) {
      if (!groups.has(r.workId)) groups.set(r.workId, []);
      groups.get(r.workId).push(r);
    } else singles.push(r);
  });

  const act = el("div", "pp-activity");
  act.append(el("div", "uw-sub", "recent activity"));
  const lines = [];
  for (const [wid, rows] of groups) {
    if (rows.length > 1) {
      const bid = acceptedByWork[wid];
      lines.push({ date: rows[0].date, el: groupedPaymentLine(bid, rows, wid) });
    } else if (rows.length === 1) singles.push(rows[0]);
  }
  singles.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  singles.forEach((r) => lines.push({ date: r.date, el: activityLine(r, p.slug) }));
  lines.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  const shown = moneyShowAll ? lines : lines.slice(0, 10);
  if (!shown.length) act.append(el("div", "pp-empty", "No payments yet — apply statement rows in the workbench."));
  shown.forEach((l) => act.append(l.el));
  if (!moneyShowAll && lines.length > shown.length) {
    const more = el("button", "o-ghost", "show all (" + lines.length + ")");
    more.onclick = () => { moneyShowAll = true; renderPropertyPage(p.slug); };
    act.append(more);
  }
  wrap.append(act);
  return wrap;
}

function activityLine(r, slug) {
  const line = el("div", "pp-act-line");
  line.append(el("span", "import-date", r.date));
  line.append(el("span", "stmt-vendor", r.vendor || r.contractor || ""));
  line.append(el("span", "pp-amt", fmtMoney(r.amount)));
  const tags = el("span", "pp-act-tags");
  if (r.workId) tags.append(el("span", "work-chip lg-chip", "⚲ " + r.workId.split("/").pop()));
  if (r.paidBy) tags.append(el("span", "work-chip", r.paidBy));
  if (r.doc) {
    const a = document.createElement("a");
    a.className = "work-chip doc-chip";
    a.textContent = "📎 " + r.doc;
    if (slug) {
      a.href = "/api/realestate/doc?path=" + encodeURIComponent("system/realestate/docs/" + slug + "/" + r.doc);
      a.target = "_blank";
      a.onclick = (e) => e.stopPropagation();
    }
    tags.append(a);
  }
  line.append(tags);
  return line;
}

// groupedPaymentLine: `$9,000 · 3 payments` expandable to the verbatim rows.
function groupedPaymentLine(bid, rows, wid) {
  const holder = el("div", "pp-act-group");
  const line = el("div", "pp-act-line grouped");
  const total = rows.reduce((s, r) => s + r.amount, 0);
  line.append(el("span", "import-date", rows[0].date));
  line.append(el("span", "stmt-vendor", (bid.contractor || bid.vendor || "") + " — " + wid.split("/").pop()));
  line.append(el("span", "pp-amt", fmtMoney(bid.amount) + " · " + rows.length + " payments" +
    (Math.abs(total - bid.amount) > 0.01 ? " (" + fmtMoney(total) + " so far)" : "")));
  const caret = el("span", "sec-caret", "▸");
  line.append(caret);
  const detail = el("div", "pp-act-detail");
  detail.hidden = true;
  rows.forEach((r) => detail.append(activityLine(r)));
  line.onclick = () => { detail.hidden = !detail.hidden; caret.textContent = detail.hidden ? "▸" : "▾"; };
  holder.append(line, detail);
  return holder;
}

// ---- inline bid flow (pass-5: chips are the only bid surface) ----

// toggleBidForm: contractor + amount inline under the todo → writes a
// requested bid tethered to it (still a ledger row — written by the action).
function toggleBidForm(row, p, st, td) {
  const existing = row.parentElement.querySelector(".bid-form[data-for='" + td.id + "']");
  if (existing) { existing.remove(); return; }
  const form = el("div", "bid-form");
  form.dataset.for = td.id;
  const who = contractorAutocomplete("contractor…");
  const amt = inputEl("amount $"); amt.type = "number"; amt.step = "1"; amt.classList.add("est-in");
  const send = el("button", "pill lg-add", "request bid");
  send.onclick = async () => {
    const amount = parseFloat(amt.value) || 0;
    if (!who.value().trim() || !amount) { showToast("Contractor + amount required"); return; }
    send.disabled = true;
    try {
      await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger", {
        type: "bid", status: "requested", contractor: who.value(), amount,
        category: st.text, workId: td.id,
      });
      renderPropertyPage(p.slug);
    } catch (e) { showToast("Couldn't create bid"); send.disabled = false; }
  };
  const cancel = el("button", "pill light lg-add", "✕");
  cancel.onclick = () => form.remove();
  form.append(who.el, amt, send, cancel);
  row.after(form);
  who.focus();
}

// bidChipEl: a clickable bid chip — requested/received expand accept/decline.
function bidChipEl(p, b) {
  const chip = el("span", "work-chip bid-" + b.status, "bid " + b.status + ": " + (b.who || "?") + " " + fmtMoney(b.amount));
  if (b.status !== "requested" && b.status !== "received") return chip;
  chip.classList.add("clickable");
  chip.onclick = async (e) => {
    e.stopPropagation();
    if (chip.querySelector("button")) return;
    const act = async (status) => {
      try {
        await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate",
          { original: b.row, replacement: { ...b.row, status } });
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Bid update failed").slice(0, 80)); }
    };
    chip.append(quietBtn(" ✓ accept", () => act("accepted")), quietBtn(" ✕ decline", () => act("declined")));
  };
  return chip;
}

function logBlock(p) {
  const wrap = el("div", "pp-log");
  (p.log || []).forEach((l) => wrap.append(el("div", "pp-log-line", l)));
  if (!(p.log || []).length) wrap.append(el("div", "pp-empty", "No log entries yet."));
  wrap.append(ghostInput("＋ log line", "pp-log-add", (v) => addLog(p.slug, v), "what happened…"));
  return wrap;
}

async function addLog(slug, text) {
  try { await postJSONOk("/api/properties/" + encodeURIComponent(slug) + "/log", { text }); renderPropertyPage(slug); }
  catch (e) { showToast("Couldn't add log line"); }
}

// docsBlock: the property's document folder — list (click opens raw), drag-drop
// zone + picker fallback. Files live in the vault at system/realestate/docs/<slug>/.
function docsBlock(p) {
  const wrap = el("div", "pp-docs");
  const list = el("div", "pp-doc-list");
  wrap.append(list);
  const refresh = async () => {
    list.innerHTML = "";
    let docs = [];
    try { docs = (await (await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/docs")).json()).docs || []; }
    catch (e) {}
    if (!docs.length) { list.append(el("div", "pp-empty", "No documents yet — drop files below.")); return; }
    docs.forEach((d) => {
      const a = el("a", "pp-doc", d.name + "  (" + fmtBytes(d.size) + ")");
      a.href = "/api/realestate/doc?path=" + encodeURIComponent(d.path);
      a.target = "_blank";
      list.append(a);
    });
  };
  refresh();

  const drop = el("div", "pp-dropzone", "drop files here — or click to pick");
  const pick = document.createElement("input");
  pick.type = "file"; pick.multiple = true; pick.hidden = true;
  const upload = async (files) => {
    if (!files || !files.length) return;
    const fd = new FormData();
    for (const f of files) fd.append("file", f);
    drop.textContent = "uploading…";
    try {
      const res = await fetch("/api/properties/" + encodeURIComponent(p.slug) + "/docs", { method: "POST", body: fd });
      if (!res.ok) throw new Error((await res.text()).trim());
      refresh();
    } catch (e) { showToast("Upload failed: " + (e.message || "").slice(0, 80)); }
    drop.textContent = "drop files here — or click to pick";
  };
  drop.onclick = () => pick.click();
  pick.onchange = () => { upload(pick.files); pick.value = ""; };
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); upload(e.dataTransfer.files); });
  wrap.append(drop, pick);
  return wrap;
}

function fmtBytes(n) {
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + "MB";
  if (n > 1024) return Math.round(n / 1024) + "KB";
  return n + "B";
}

function inputEl(placeholder) {
  const i = document.createElement("input");
  i.className = "pp-in"; i.placeholder = placeholder; return i;
}
function selectEl(opts) {
  const s = document.createElement("select"); s.className = "pp-in";
  opts.forEach((o) => { const opt = document.createElement("option"); opt.value = o; opt.textContent = o; s.append(opt); });
  return s;
}
