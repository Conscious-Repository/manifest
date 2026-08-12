// ---- PROPERTIES · SETTINGS — restored (owner call 2026-08-09) ----
// The entity/ownership layer the Rev-3 cut removed, back as a rail view:
// ENTITIES (owners + admin categories, editable in place) · ORG CHART ·
// PEOPLE (the RE assignee registry, system/realestate/people.md) ·
// STATEMENT ACCOUNTS · TEMPLATES.

let entitiesCache = null; // {entities:[], contractors:[], bindings:{}}

async function ensureEntities(force) {
  if (entitiesCache && !force) return entitiesCache;
  try { entitiesCache = await (await fetch("/api/realestate/entities")).json(); }
  catch (e) { entitiesCache = { entities: [], contractors: [], bindings: {} }; }
  return entitiesCache;
}

// recordAutocomplete: the typeahead engine over entity/contractor records with
// a quiet `create "<name>" →` completion.
function recordAutocomplete(kind, placeholder, onPick) {
  const records = () => (entitiesCache ? (kind === "contractor" ? entitiesCache.contractors : entitiesCache.entities) : []) || [];
  const ta = typeahead({
    placeholder,
    suggest: async (q, add) => {
      await ensureEntities();
      records().filter((r) => !q || r.name.toLowerCase().includes(q) || r.slug.includes(q)).slice(0, 10)
        .forEach((r) => add(r.name, "", () => { ta.commit(r.name); if (onPick) onPick(r); }));
      const exact = records().some((r) => r.name.toLowerCase() === q);
      if (q && !exact) {
        add('create "' + ta.value() + '" →', "create", async () => {
          try {
            const rec = await postJSONOk("/api/realestate/entities", { name: ta.value(), kind });
            await ensureEntities(true);
            ta.commit(rec.name);
            showToast(kind + " record created: " + rec.name);
            if (onPick) onPick(rec);
          } catch (err) { showToast("Couldn't create " + kind); }
        });
      }
    },
  });
  return ta;
}

// ownerAutocomplete: entity records + person notes — "brian anderson" links
// [[brian anderson]] to his actual note.
function ownerAutocomplete(placeholder, onSet) {
  const ta = typeahead({
    placeholder,
    onChange: onSet,
    suggest: async (q, add) => {
      await ensureEntities();
      let people = [];
      if (q.length >= 2) {
        try {
          const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
          people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 6);
        } catch (e) {}
      }
      ((entitiesCache || {}).entities || [])
        .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 6)
        .forEach((r) => add(r.name, "entity", () => { ta.commit(r.name); onSet(r.name); }));
      people.forEach((r) => add(r.display, "person", () => { ta.commit(r.key); onSet(r.key); }));
    },
  });
  return ta;
}

// The Settings view — the AION ORG mirror: a 176px registry sub-rail
// (Assumptions · Entities · Partners · Contractors · Lenders · Tenants ·
// People, each with a count + a File/⌘-raw box) over ONE pane at a time.
// Reuses the .aion-org* classes so the shape matches AION's Org tab. Existing
// sections fold in: org chart + statement accounts + templates under Entities;
// people.md is now its own registry item.
let reSettingsTab = "assumptions";

async function renderREsettings() {
  const host = els.propertySettings;
  host.hidden = false;
  host.innerHTML = "loading…";
  await ensureEntities(true);
  if (!reAssumptionsCache) await loadReAssumptions();
  host.innerHTML = "";
  const ents = entitiesCache.entities || [];

  const wrap = el("div", "aion-org");
  const rail = el("div", "aion-org-rail");
  const pane = el("div", "aion-org-pane");
  wrap.append(rail, pane);
  host.append(wrap);

  rail.append(el("div", "aion-org-label", "Registries"));
  // Partners + Contractors consolidated into ONE People item (owner call
  // 2026-08-12) — partners on top (aion-style people.md table), contractor
  // records below. Stale sub-tab keys from old links fold in.
  if (reSettingsTab === "partners" || reSettingsTab === "contractors") reSettingsTab = "people";
  const items = [
    ["assumptions", "Assumptions", (reAssumptions().__keys || []).length, "system/realestate/assumptions.md"],
    ["entities", "Entities", ents.length, "system/realestate/entities.md"],
    ["people", "People", (_rePeopleCount == null ? 0 : _rePeopleCount) + (entitiesCache.contractors || []).length, "system/realestate/people.md"],
    ["lenders", "Lenders", (entitiesCache.lenders || []).length, "system/realestate/entities.md"],
    ["tenants", "Tenants", (entitiesCache.tenants || []).length, "system/realestate/entities.md"],
  ];
  let rel = "system/realestate/assumptions.md";
  items.forEach(([key, label, n, path]) => {
    if (key === reSettingsTab) rel = path;
    const b = el("button", "aion-org-item" + (reSettingsTab === key ? " active" : ""));
    b.append(el("span", "", label));
    if (n !== null) b.append(el("span", "aion-org-count", String(n)));
    b.onclick = () => { reSettingsTab = key; renderREsettings(); };
    rail.append(b);
  });
  const fileBox = el("div", "aion-org-file");
  fileBox.append(el("div", "aion-org-label", "File"), el("div", "aion-org-path", rel));
  const rawBtn = el("button", "aion-org-raw", "⌘/ edit raw");
  rawBtn.onclick = () => openRawOverlay(rel);
  fileBox.append(rawBtn);
  rail.append(fileBox);

  if (reSettingsTab === "assumptions") { renderAssumptionsPanel(pane); return; }
  if (reSettingsTab === "lenders") { renderFlatRegistry(pane, "lender", entitiesCache.lenders || []); return; }
  if (reSettingsTab === "tenants") { renderFlatRegistry(pane, "tenant", entitiesCache.tenants || []); return; }
  if (reSettingsTab === "people") { await rePeoplePane(pane); return; }
  renderEntitiesPanel(pane, ents);
}

// ---- ASSUMPTIONS — the single global set, sticky save, derived override index ----
function renderAssumptionsPanel(pane) {
  const a = reAssumptions();
  pane.append(el("div", "pp-section-head", "UNDERWRITING ASSUMPTIONS — the single global set"));
  const grid = el("div", "re-assump-grid");
  const dirty = {};
  const overridesFor = (key) => (a.__overrides || []).filter((o) => o.key === key);
  (a.__keys || []).forEach((key) => {
    const row = el("div", "re-assump-row");
    row.append(el("span", "re-assump-label", (a.__labels || {})[key] || key));
    const inp = inputEl("");
    inp.className = "pp-in re-assump-in";
    inp.value = a[key] != null ? String(a[key]) : "";
    inp.oninput = () => { dirty[key] = inp.value; saveBar.hidden = !Object.keys(dirty).length; };
    row.append(inp);
    const who = overridesFor(key);
    row.append(el("span", "re-assump-who", who.length
      ? who.map((o) => o.name + " (" + o.value + ")").join(" · ")
      : "—"));
    grid.append(row);
  });
  pane.append(grid);
  const saveBar = el("div", "dirty-bar");
  saveBar.hidden = true;
  const save = el("button", "pill-solid", "save assumptions");
  save.onclick = async () => {
    const values = {};
    let bad = "";
    Object.keys(dirty).forEach((k) => {
      const v = parseFloat(dirty[k]);
      if (isNaN(v)) bad = k; else values[k] = v;
    });
    if (bad) { showToast(bad + " isn't a number"); return; }
    try {
      await putJSON("/api/realestate/assumptions", { values });
      reAssumptionsCache = null;
      showToast("Assumptions saved — PUBLISH pushes them to the portal engine");
      renderREsettings();
    } catch (e) { showToast("Couldn't save — " + (e.message || "")); }
  };
  saveBar.append(save);
  pane.append(saveBar);
  pane.append(el("div", "re-foot-note",
    "system/realestate/assumptions.md → published to oodagroup/src/engine/defaults.js"));
}

// ---- flat registries (Partners · Contractors · Lenders · Tenants) ----
function renderFlatRegistry(pane, kind, rows) {
  const list = el("div", "set-entities");
  if (!rows.length) list.append(el("div", "pp-empty", "No " + kind + " records yet."));
  rows.forEach((r) => {
    const row = el("div", "set-bind-row");
    row.append(el("span", "stmt-vendor", r.name + (r.trade ? "  (" + r.trade + ")" : "")));
    row.append(pillLight("open →", () => {
      _noteReturn = "#/properties/settings";
      openNoteByPath(r.path);
    }));
    list.append(row);
  });
  list.append(ghostInput("＋ " + kind, "set-add", async (v) => {
    try { await postJSONOk("/api/realestate/entities", { name: v, kind }); renderREsettings(); }
    catch (err) { showToast("Couldn't create " + kind); }
  }, kind + " name…"));
  pane.append(list);
}

// ---- ENTITIES — per-entity blocks (the entity is the thing that has books) ----
function renderEntitiesPanel(host, ents) {
  // ENTITIES — list, create, owners, admin categories
  host.append(el("div", "pp-section-head", "ENTITIES"));
  const list = el("div", "set-entities");
  ents.forEach((e) => list.append(entityCard(e, ents)));
  list.append(ghostInput("＋ entity", "set-add", async (v) => {
    try { await postJSONOk("/api/realestate/entities", { name: v, kind: "entity" }); renderREsettings(); }
    catch (err) { showToast("Couldn't create entity"); }
  }, "entity name…"));
  host.append(list);

  // ORG CHART — ownership tree read live from the records
  host.append(el("div", "pp-section-head", "ORG CHART"));
  host.append(orgChart(ents));

  // STATEMENT ACCOUNTS — source-label → entity bindings
  host.append(el("div", "pp-section-head", "STATEMENT ACCOUNTS"));
  const bind = el("div", "set-bindings");
  const bindings = entitiesCache.bindings || {};
  const keys = Object.keys(bindings);
  if (!keys.length) bind.append(el("div", "pp-empty", "No bindings yet — they're remembered when you upload statements."));
  keys.sort().forEach((label) => {
    const row = el("div", "set-bind-row");
    row.append(el("span", "stmt-vendor", label));
    const ac = recordAutocomplete("entity", "entity…");
    ac.setValue(bindings[label]);
    row.append(ac.el);
    row.append(pillLight("save", async () => {
      try { await postJSONOk("/api/realestate/bindings", { label, entity: ac.value() }); showToast("Binding saved"); }
      catch (e) { showToast("Couldn't save binding"); }
    }));
    bind.append(row);
  });
  host.append(bind);

  // TEMPLATES — open in the note view; management stays hand-edit
  host.append(el("div", "pp-section-head", "TEMPLATES"));
  const tpl = el("div", "set-templates");
  (templateCache || []).forEach((t) => {
    const row = el("div", "set-bind-row");
    row.append(el("span", "stmt-vendor", t.name + "  (" + (t.stages || []).length + " stages)"));
    row.append(pillLight("open →", () => {
      _noteReturn = "#/properties/settings";
      openNoteByPath("system/realestate/templates/" + t.slug + ".md");
    }));
    tpl.append(row);
  });
  host.append(tpl);
}

// rePeoplePane — the consolidated People registry (owner call 2026-08-12):
// PARTNERS on top (system/realestate/people.md, the aion table shape —
// initials/name/role), CONTRACTORS (records) below. An empty registry
// prefills partner rows from the entity org charts' person owners — shown
// dirty, written only on explicit save. A row whose name matches a person
// note in the vault gets an ↗ link (same mechanism the aion roster uses).
let _rePeopleCount = null;

async function rePeoplePane(host) {
  let data = { people: [], rel: "system/realestate/people.md" };
  try { data = await (await fetch("/api/properties/people")).json(); } catch (e) {}
  const noteIdx = await contactNoteIndex();
  _rePeopleCount = (data.people || []).length;

  const head = el("div", "pp-section-head");
  head.append(el("span", "", "PARTNERS"));
  const open = el("a", "aion-open", "⌘/ edit raw");
  open.href = "#";
  open.onclick = (ev) => { ev.preventDefault(); openRawOverlay(data.rel); };
  head.append(open);
  host.append(head);
  host.append(el("div", "aion-section-note",
    "the RE people registry (people.md) — partners, counsel, lenders; every row is a property-todo assignee. ↗ opens the person's note."));

  const rows = (data.people || []).map((p) => ({ ...p }));
  const bar = makeDirtyBar(els.propertiesView, async () => {
    await putJSON("/api/properties/people", { people: rows });
    showToast("Saved — people.md updated");
    renderREsettings();
  }, () => renderREsettings());

  // seed: person owners from the entity org charts (entity-to-entity refs
  // excluded), role "partner" — dirty until the owner saves
  if (!rows.length) {
    const entNames = new Set(((entitiesCache || {}).entities || []).map((e) => e.name.toLowerCase()));
    const seen = new Set();
    const used = new Set(); // initials are assignee keys — collisions extend
    const initialsFor = (name) => {
      const words = name.split(/\s+/);
      let ini = words.map((w) => w[0] || "").join("").toUpperCase().slice(0, 3);
      for (let n = 2; used.has(ini) && n <= (words[0] || "").length; n++) {
        ini = (words[0].slice(0, n) + words.slice(1).map((w) => w[0] || "").join("")).toUpperCase().slice(0, 4);
      }
      used.add(ini);
      return ini;
    };
    (((entitiesCache || {}).entities) || []).forEach((e) => {
      (e.owners || []).forEach((o) => {
        const ref = (o.ref || "").trim();
        if (!ref || entNames.has(ref.toLowerCase()) || seen.has(ref.toLowerCase())) return;
        seen.add(ref.toLowerCase());
        rows.push({ initials: initialsFor(ref), name: ref, role: "partner" });
      });
    });
    if (rows.length) {
      bar.mark();
      host.append(el("div", "re-foot-note",
        "prefilled from the entity org charts — save writes them to people.md"));
    }
  }

  host.append(ppCols("cols-aion-people cols-people-linked", ["INITIALS", "NAME", "ROLE", ""]));
  const body = el("div", "aion-table");
  host.append(body);
  const renderRows = () => {
    body.innerHTML = "";
    rows.forEach((row, i) => {
      const line = el("div", "aion-row cols-aion-people cols-people-linked");
      [["initials", "INITIALS"], ["name", "NAME"], ["role", "ROLE"]].forEach(([key, label]) => {
        const input = inputEl(label.toLowerCase());
        input.value = row[key] || "";
        input.oninput = () => { row[key] = input.value; bar.mark(); };
        line.append(input);
      });
      const acts = el("span", "aion-row-acts");
      const notePath = noteIdx[(row.name || "").toLowerCase()];
      if (notePath) {
        const link = el("button", "aion-mini", "↗");
        link.title = "open " + notePath;
        link.onclick = () => { _noteReturn = "#/properties/settings"; openNoteByPath(notePath); };
        acts.append(link);
      }
      const x = el("button", "aion-mini", "✕");
      x.title = "remove row";
      x.onclick = () => { rows.splice(i, 1); bar.mark(); renderRows(); };
      acts.append(x);
      line.append(acts);
      body.append(line);
    });
    body.append(ghostInput("＋ partner", "aion-add", (v) => {
      rows.push({ initials: v.toUpperCase().slice(0, 3), name: "", role: "partner" });
      bar.mark();
      renderRows();
    }));
  };
  renderRows();

  host.append(el("div", "pp-section-head", "CONTRACTORS"));
  host.append(el("div", "aion-section-note",
    "contractor records (system/realestate/contractors/) — they join the assignee list automatically"));
  renderFlatRegistry(host, "contractor", entitiesCache.contractors || []);
}

function entityCard(e, ents) {
  const card = el("div", "set-entity");
  const head = el("div", "set-entity-head");
  head.append(el("span", "wv-addr", e.name));
  // partnered toggle — splits the accountant handoff (personal vs partner books)
  const pt = el("button", "re-partnered" + (e.partnered ? " on" : ""), e.partnered ? "partnered" : "personal");
  pt.title = "personal books vs partner entity — drives the accountant handoff split";
  pt.onclick = async () => {
    try { await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save", { partnered: !e.partnered }); renderREsettings(); }
    catch (err) { showToast("Couldn't save"); }
  };
  head.append(pt);
  // derived holdings — owned and acquiring stay separate, never summed
  const h = (holdingsCache || {})[e.name] || { owned: 0, acquiring: 0 };
  head.append(el("span", "re-holdings", h.owned + " owned" + (h.acquiring ? " · " + h.acquiring + " acquiring" : "")));
  // account rollup: attention in ink when any account isn't live
  const accs = e.accounts || [];
  const bad = accs.filter((a) => a.state !== "live").length;
  if (accs.length) {
    head.append(el("span", "re-acct-rollup" + (bad ? " attn" : ""),
      bad ? bad + " needs attention" : accs.length + " connected"));
  }
  head.append(pillLight("open →", () => { _noteReturn = "#/properties/settings"; openNoteByPath(e.path); }));
  card.append(head);

  // bank-account rows (scaffold until a live pull exists — statements bind
  // via STATEMENT ACCOUNTS below; state chips are informational)
  const acctBox = el("div", "re-accts");
  const renderAccts = () => {
    acctBox.innerHTML = "";
    accs.forEach((a, i) => {
      const row = el("div", "re-acct-row");
      row.append(el("span", "re-acct-label", a.label));
      row.append(el("span", "re-acct-kind", a.kind || "operating"));
      row.append(el("span", "re-acct-state st-" + (a.state || "not-connected"), (a.state || "not-connected").replace(/-/g, " ")));
      const act = el("button", "re-acct-act", a.state === "live" ? "disconnect" : a.state === "needs-reauth" ? "re-authorize" : "connect");
      act.title = "live bank pull is a later phase — this row names the account; statements bind by label";
      act.onclick = () => showToast("Live pull comes later — statements bind by label today");
      row.append(act);
      const x = el("button", "uw-x", "✕");
      x.onclick = async () => {
        accs.splice(i, 1);
        try { await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save", { accounts: accs }); renderREsettings(); }
        catch (err) { showToast("Couldn't save"); }
      };
      row.append(x);
      acctBox.append(row);
    });
  };
  renderAccts();
  card.append(acctBox);
  card.append(ghostInput("＋ connect an account", "set-add", async (v) => {
    accs.push({ label: v, kind: "operating", state: "not-connected" });
    try { await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save", { accounts: accs }); renderREsettings(); }
    catch (err) { showToast("Couldn't save"); }
  }, "bank + last four, e.g. Midwest ····4821"));

  // owners editor: owner ref + percent rows, Σ warning, cycle check server-side
  const owners = (e.owners || []).map((o) => ({ ...o }));
  const box = el("div", "uw-rows");
  const render = () => {
    box.innerHTML = "";
    owners.forEach((o, i) => {
      const row = el("div", "uw-row cols-kv");
      const ref = ownerAutocomplete("owner (entity or person)…", (v) => { o.ref = v; });
      ref.setValue(o.ref);
      const pctIn = inputEl("%"); pctIn.type = "number"; pctIn.classList.add("est-in");
      pctIn.value = o.percent || "";
      pctIn.addEventListener("change", () => { o.percent = parseFloat(pctIn.value) || 0; render(); });
      const x = el("button", "uw-x", "✕");
      x.onclick = () => { owners.splice(i, 1); render(); };
      row.append(ref.el, pctIn, x);
      box.append(row);
    });
    const add = el("button", "o-ghost", "＋ owner");
    add.onclick = () => { owners.push({ ref: "", percent: 0 }); render(); };
    box.append(add);
    const sum = owners.reduce((s, o) => s + (o.percent || 0), 0);
    const foot = el("div", "uw-footer", owners.length ? "Σ " + sum + "%" + (Math.abs(sum - 100) < 0.01 ? " ✓" : " ✕ should be 100%") : "");
    const save = pillLight("save owners", async () => {
      try {
        await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save",
          { owners: owners.filter((o) => o.ref) });
        showToast("Owners saved");
        renderREsettings();
      } catch (err) { showToast((err.message || "Save failed").slice(0, 90)); }
    });
    foot.append(save);
    box.append(foot);
  };
  render();
  card.append(box);

  // admin categories — chip editor over the frontmatter list
  const cats = [...(e.adminCategories || [])];
  const catBox = el("div", "set-cats");
  const renderCats = () => {
    catBox.innerHTML = "";
    catBox.append(el("span", "uw-label", "ADMIN CATEGORIES"));
    cats.forEach((c, i) => {
      const chip = el("span", "pp-chip", c + " ");
      const x = el("button", "uw-x", "✕");
      x.onclick = async () => { cats.splice(i, 1); await saveCats(); };
      chip.append(x);
      catBox.append(chip);
    });
    catBox.append(ghostInput("＋ category", "set-cat-add", async (v) => { cats.push(v); await saveCats(); }, "category…"));
  };
  const saveCats = async () => {
    try {
      await postJSONOk("/api/realestate/entities/" + encodeURIComponent(e.slug) + "/save", { adminCategories: cats });
      await ensureEntities(true);
      renderCats();
    } catch (err) { showToast("Couldn't save categories"); }
  };
  renderCats();
  card.append(catBox);
  return card;
}

// orgChart: nested hairline tree — properties under their owning entity,
// parent entities above with percentages on the edges. A read, not a cap table.
function orgChart(ents) {
  const wrap = el("div", "org-chart");
  if (!ents.length) { wrap.append(el("div", "pp-empty", "No entities yet.")); return wrap; }
  const roots = ents.filter((e) => !(e.owners || []).some((o) =>
    ents.some((x) => x.name.toLowerCase() === o.ref.toLowerCase() || x.slug.toLowerCase() === o.ref.toLowerCase())));
  const propsOf = (e) => (propertyCache || []).filter((p) => (p.entity || "").toLowerCase() === e.name.toLowerCase());
  const node = (e, pct, depth, seen) => {
    if (seen.has(e.slug)) return el("div", "org-node", "↺ " + e.name);
    seen.add(e.slug);
    const box = el("div", "org-node");
    box.style.marginLeft = depth * 22 + "px";
    box.append(el("span", "org-name", e.name));
    if (pct) box.append(el("span", "org-pct", pct + "%"));
    const kids = ents.filter((k) => k.slug !== e.slug && (k.owners || []).some((o) =>
      o.ref.toLowerCase() === e.name.toLowerCase() || o.ref.toLowerCase() === e.slug.toLowerCase()));
    const out = el("div", "org-branch");
    out.append(box);
    propsOf(e).forEach((p) => {
      const pr = el("div", "org-prop");
      pr.style.marginLeft = (depth + 1) * 22 + "px";
      pr.textContent = "▪ " + (p.short || p.address || p.slug);
      out.append(pr);
    });
    kids.forEach((k) => {
      const edge = (k.owners || []).find((o) => o.ref.toLowerCase() === e.name.toLowerCase() || o.ref.toLowerCase() === e.slug.toLowerCase());
      out.append(node(k, edge ? edge.percent : 0, depth + 1, seen));
    });
    return out;
  };
  const seen = new Set();
  roots.forEach((r) => wrap.append(node(r, 0, 0, seen)));
  // entities trapped in cycles / non-roots never reached: render flat
  ents.filter((e) => !seen.has(e.slug)).forEach((e) => wrap.append(node(e, 0, 0, seen)));
  return wrap;
}
