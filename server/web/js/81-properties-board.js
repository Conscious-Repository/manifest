// ---- PROPERTIES pane views: the all-properties table + Outstanding ----
// Rev 3: the whole board is three columns — address · status · open todos.
// Outstanding groups everything owed to you by container (the chasing surface).
let propComposerOpen = false;

const PROPERTY_STATUSES = ["negotiating", "under_contract", "pre_development", "construction", "completed", "leased", "listed", "sold"];
const PROPERTY_KINDS = ["rehab", "new-construction", "mixed", "hold"];

function renderAllProperties() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const cols = el("div", "prop-cols");
  cols.append(el("span", "", "ADDRESS"), el("span", "", "STATUS"), el("span", "prop-col-r", "TODOS"));
  host.append(cols);
  propertyCache.filter((p) => !p.hidden).forEach((p) => {
    const row = el("div", "prop-row");
    row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(p.slug); };
    row.append(el("span", "prop-addr", p.short || p.address || p.slug));
    row.append(el("span", "prop-status", (p.status || "").replace(/_/g, " ")));
    const n = openTodoCount(p);
    row.append(el("span", "prop-count" + (n ? " some" : ""), n ? String(n) : "·"));
    host.append(row);
  });
  host.append(propertyComposer());
}

// propOutstandingGroups — PROPERTY containers only (owner call 2026-08-09):
// aion items owed to you live on the AION surface, never here.
function propOutstandingGroups() {
  return ((propTodosMeta && propTodosMeta.outstanding) || [])
    .filter((g) => g.container.kind === "property");
}

function renderOutstanding() {
  const host = els.propertyBoard;
  host.innerHTML = "";
  const groups = propOutstandingGroups();
  if (!groups.length) {
    host.append(el("div", "pp-empty", "Nothing outstanding — nobody owes you anything right now."));
    return;
  }
  groups.forEach((g) => {
    const sec = el("div", "prop-out-group");
    const head = el("div", "prop-out-head");
    const name = el("span", "prop-out-name", g.container.name);
    if (g.container.kind === "property") {
      name.classList.add("linky");
      name.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(g.container.slug); };
    }
    head.append(name, el("span", "prop-out-count", g.count + " owed to you"));
    sec.append(head);
    (g.items || []).forEach((r) => {
      const row = el("div", "prop-row prop-out-row");
      row.append(el("span", "prop-out-glyph", "○"), el("span", "prop-out-text", r.text));
      row.append(el("span", "prop-owner", assigneeName(r.owner)));
      if (g.container.kind === "property") {
        row.onclick = () => { location.hash = "#/properties/" + encodeURIComponent(g.container.slug); };
      }
      sec.append(row);
    });
    host.append(sec);
  });
}

// assigneeName resolves an owner value against the registries (contractor
// slug → name; aion initials → name); unknown values render as written.
function assigneeName(owner) {
  if (!owner) return "you";
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
