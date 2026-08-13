// ---- PROPERTIES pane views: the all-properties table + Outstanding ----
// Rev 3: the whole board is three columns — address · status · open todos.
// Outstanding groups everything owed to you by container (the chasing surface).
let propComposerOpen = false;

const PROPERTY_STATUSES = ["negotiating", "under_contract", "pre_development", "construction", "completed", "leased", "listed", "sold"];
const PROPERTY_KINDS = ["rehab", "new-construction", "mixed", "hold"];

// Portfolio — ONE table, four columns (RE spec §3): property · stage
// (progress bar + name) · open · spent. No entity column — that fact lives
// on the entity registry.
function renderPortfolio() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const cols = el("div", "prop-cols pf-cols");
  cols.append(el("span", "", "PROPERTY"), el("span", "", "STAGE"),
    el("span", "prop-col-r", "OPEN"), el("span", "prop-col-r", "SPENT"));
  host.append(cols);
  activePortfolio().forEach((p) => {
    const row = el("div", "prop-row pf-row");
    row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    row.append(el("span", "prop-addr", p.short || p.address || p.slug));
    // stage cell: 44px progress bar + current stage name (derived from ## stages)
    const stages = p.work || [];
    const done = stages.filter((st) => st.checked).length;
    const cell = el("span", "pf-stage");
    const bar = el("span", "pf-bar");
    const fill = el("span", "pf-bar-fill");
    fill.style.width = stages.length ? Math.round((done / stages.length) * 100) + "%" : "0";
    bar.append(fill);
    cell.append(bar, el("span", "pf-stage-name", p.currentStage || (stages.length ? "done" : (p.status || "").replace(/_/g, " "))));
    row.append(cell);
    const n = openTodoCount(p);
    row.append(el("span", "prop-count" + (n ? " some" : ""), n ? String(n) : "·"));
    const m = projMoney(p);
    row.append(el("span", "pf-spent" + (m.over ? " over" : ""), m.paid ? fmtMoneyShort(m.paid) : "·"));
    host.append(row);
  });
  // tracked-but-inactive records stay reachable behind a quiet foot count
  const tracked = propertyCache.filter((p) => !p.hidden && !(p.control === "owned" || p.entity));
  if (tracked.length) {
    const t = el("button", "pf-tracked-foot", "▸ " + tracked.length + " tracked (research) — see Map");
    t.onclick = () => { location.hash = "#/properties/map"; };
    host.append(t);
  }
  host.append(propertyComposer());

  // Deals fold in here (was the rail's Deals group): each deal → its deal page.
  if (dealCache.length) {
    const dealsSec = el("div", "pf-deals");
    const dh = el("div", "aion-sec-label");
    dh.append(el("span", "aion-sec-title", "◈ Deals"), el("span", "aion-sec-count", String(dealCache.length)));
    dealsSec.append(dh);
    dealCache.forEach((d) => {
      const members = propertyCache.filter((p) => (p.deal || "") === (d.name || d.slug) || (p.deal || "") === d.slug);
      const row = el("div", "prop-row pf-deal-row");
      row.onclick = () => { location.hash = "#/properties/deal/" + encodeURIComponent(d.slug); };
      row.append(el("span", "prop-addr", d.name || d.slug));
      row.append(el("span", "pf-deal-meta", members.length + " propert" + (members.length === 1 ? "y" : "ies")));
      dealsSec.append(row);
    });
    host.append(dealsSec);
  }
}

function fmtMoneyShort(v) {
  if (v >= 1e6) return "$" + (v / 1e6).toFixed(1) + "M";
  if (v >= 1e3) return "$" + Math.round(v / 1e3) + "k";
  return "$" + Math.round(v);
}

// Rocks — org-level 90-day work from the goals `## Real Estate` area,
// rendered rock → stage → task (the same shape a property renders in).
// A task carrying a property/deal tag shows it in accent and also lives
// there; the footer states the rule.
function renderRERocks() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const rocks = reOrgRocks();
  if (!rocks.length) {
    host.append(emptyRow("No org rocks in the Real Estate area yet — add one in GOALS."));
    return;
  }
  rocks.forEach((g) => {
    const wrap = el("div", "re-rock");
    const line = el("div", "re-rock-line");
    line.append(el("span", "re-rock-dot"));
    const name = el("span", "re-rock-name", g.text);
    name.onclick = () => { location.hash = "#/goals/" + encodeURIComponent(g.id); };
    line.append(name);
    wrap.append(line);
    // stage trail with tethered backlog tasks nested (from the RE backlog +
    // the unified todos rows that carry this rock id)
    // a task tethered to this rock OR any of its stages nests here (stages are
    // pickable tether targets now, so match the whole subtree's ids)
    const rockIds = new Set([g.id, ...(g.children || []).map((c) => c.id)]);
    const tethered = reItems().filter((it) => it.kind === "task" && it.status !== "done" && rockIds.has(it.rock));
    // milestones are not sequential (owner call 2026-08-12) — no current marker
    (g.children || []).forEach((st) => {
      const sl = el("div", "re-stage" + (st.checked ? " done" : ""));
      sl.append(el("span", "re-stage-glyph", st.checked ? "✓" : "○"));
      sl.append(el("span", "", st.text));
      wrap.append(sl);
    });
    tethered.forEach((it) => {
      const t = el("div", "re-task");
      t.append(el("span", "re-task-glyph", "○"), el("span", "", it.text));
      if (it.owner) t.append(el("span", "re-task-owner", "@" + it.owner));
      wrap.append(t);
    });
    host.append(wrap);
  });
  host.append(el("div", "re-foot-note", "an untagged task is org-level and lives here — tag a property or deal to file it there too"));
}

// propOutstandingGroups — PROPERTY containers only (owner call 2026-08-09):
// aion items owed to you live on the AION surface, never here.
function propOutstandingGroups() {
  return ((propTodosMeta && propTodosMeta.outstanding) || [])
    .filter((g) => g.container.kind === "property");
}

// (renderOutstanding deleted — Outstanding folded into the Backlog view as
//  non-"you" owner groups. propOutstandingGroups() stays; the Backlog reads it.)

// mineOwner mirrors the substrate's rule: empty · "me" · my initials = me.
function mineOwner(owner) {
  if (!owner || owner === "me") return true;
  const me = ((propTodosMeta && propTodosMeta.me) || "BA").toUpperCase();
  return owner.toUpperCase().split("/").map((s) => s.trim()).includes(me);
}

// assigneeName resolves an owner value against the registries (contractor
// slug → name; aion initials → name); anything that means ME reads "you".
function assigneeName(owner) {
  if (mineOwner(owner)) return "you";
  const a = (propTodosMeta && propTodosMeta.assignees) || {};
  const c = (a.realestate || []).find((e) => e.slug === owner || e.name === owner);
  if (c) return c.name + (c.trade ? " (" + c.trade + ")" : "");
  const p = (a.aion || []).find((e) => e.initials === owner);
  if (p) return p.name;
  return owner;
}

// statusChip / statusSelect — the one in-place edit the table keeps.
function statusSelect(p, onSaved) {
  const sel = selectEl(PROPERTY_STATUSES);
  sel.classList.add("prop-status-sel");
  sel.value = p.status || "negotiating"; // options are mounted above — the value sticks
  sel.onchange = async () => {
    try { onSaved(await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/field", { key: "status", value: sel.value })); }
    catch (err) { showToast("Couldn't update status"); }
  };
  return sel;
}

// propertyComposer: address · entity · kind · template — creation stays.
function propertyComposer() {
  const wrap = el("div", "prop-compose-wrap");
  const ghost = el("button", "o-ghost property-add", "＋ property");
  const openForm = () => {
    const form = el("div", "prop-composer");
    const addr = inputEl("address…"); addr.classList.add("pc-addr");
    const entity = inputEl("entity (optional)…");
    const kind = selectEl(PROPERTY_KINDS);
    const tpl = selectEl(["no template", ...templateCache.map((t) => t.slug)]);
    tpl.title = "budget-mix template";
    const dealSel = selectEl(["unattached", ...dealCache.map((d) => d.slug)]);
    dealSel.title = "attach to a deal bundle";
    const create = el("button", "pill", "create");
    create.onclick = async () => {
      if (!addr.value.trim()) { addr.focus(); return; }
      create.disabled = true;
      try {
        await postJSONOk("/api/properties", {
          address: addr.value, entity: entity.value.trim(), kind: kind.value,
          template: tpl.value === "no template" ? "" : tpl.value,
          deal: dealSel.value === "unattached" ? "" : dealSel.value,
        });
        propComposerOpen = false;
        renderProperties();
      } catch (e) { showToast("Couldn't create property"); create.disabled = false; }
    };
    const cancel = el("button", "pill light", "✕");
    cancel.onclick = () => { propComposerOpen = false; form.replaceWith(ghost); };
    form.append(addr, entity, kind, tpl, dealSel, create, cancel);
    ghost.replaceWith(form);
    addr.focus();
  };
  ghost.onclick = openForm;
  wrap.append(ghost);
  if (propComposerOpen) setTimeout(openForm, 0);
  return wrap;
}
