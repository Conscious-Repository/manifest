// ---- entity & contractor records + autocomplete (pass-5 §5) ----

let entitiesCache = null; // {entities:[], contractors:[], bindings:{}}

async function ensureEntities(force) {
  if (entitiesCache && !force) return entitiesCache;
  try { entitiesCache = await (await fetch("/api/realestate/entities")).json(); }
  catch (e) { entitiesCache = { entities: [], contractors: [], bindings: {} }; }
  return entitiesCache;
}

// recordAutocomplete: typeahead over entity/contractor records with a quiet
// `create "<name>" →` completion. Returns {el, value(), setValue(), focus()}.
function recordAutocomplete(kind, placeholder, onPick) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  const records = () => (entitiesCache ? (kind === "contractor" ? entitiesCache.contractors : entitiesCache.entities) : []) || [];
  const refresh = async () => {
    await ensureEntities();
    const q = input.value.toLowerCase().trim();
    drop.innerHTML = "";
    const hits = records().filter((r) => !q || r.name.toLowerCase().includes(q) || r.slug.includes(q)).slice(0, 10);
    hits.forEach((r) => {
      const it = el("div", "ta-item", r.name);
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.name; drop.hidden = true; if (onPick) onPick(r); };
      drop.append(it);
    });
    const exact = records().some((r) => r.name.toLowerCase() === q);
    if (q && !exact) {
      const mk = el("div", "ta-item ta-create", 'create "' + input.value.trim() + '" →');
      mk.onmousedown = async (e) => {
        e.preventDefault();
        try {
          const rec = await postJSONOk("/api/realestate/entities", { name: input.value.trim(), kind });
          await ensureEntities(true);
          input.value = rec.name;
          drop.hidden = true;
          showToast(kind + ' record created: ' + rec.name);
          if (onPick) onPick(rec);
        } catch (err) { showToast("Couldn't create " + kind); }
      };
      drop.append(mk);
    }
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; }, focus: () => input.focus() };
}

// ownerAutocomplete: the SETTINGS owners field — suggests ENTITY records and
// PEOPLE from the vault (contacts search over person notes), so "brian
// anderson" links [[brian anderson]] to his actual note. Person hits are the
// vault's own graph; entity hits come from the records.
function ownerAutocomplete(placeholder, onSet) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  let seq = 0;
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    const mySeq = ++seq;
    await ensureEntities();
    let people = [];
    if (q.length >= 2) {
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 6);
      } catch (e) {}
    }
    if (mySeq !== seq) return; // a newer keystroke superseded this fetch
    drop.innerHTML = "";
    const ents = ((entitiesCache || {}).entities || [])
      .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 6);
    ents.forEach((r) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", r.name), el("span", "ta-kind", "entity"));
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.name; drop.hidden = true; onSet(r.name); };
      drop.append(it);
    });
    people.forEach((r) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", r.display), el("span", "ta-kind", "person"));
      it.onmousedown = (e) => { e.preventDefault(); input.value = r.key; drop.hidden = true; onSet(r.key); };
      drop.append(it);
    });
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("change", () => onSet(input.value.trim()));
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; } };
}

// contractorAutocomplete: the bid form's name field — suggests contractor
// records, PEOPLE from the vault (contacts search), and names already used
// across the ledgers, with the quiet create-record completion for new ones.
function contractorAutocomplete(placeholder, onSet) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(placeholder);
  input.classList.add("ta-in");
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  let seq = 0;
  const ledgerNames = () => {
    const seen = new Set();
    (propertyCache || []).forEach((p) => (p.ledger || []).forEach((r) => {
      const n = (r.contractor || r.vendor || "").trim();
      if (n) seen.add(n);
    }));
    return [...seen];
  };
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    const mySeq = ++seq;
    await ensureEntities();
    let people = [];
    if (q.length >= 2) {
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 5);
      } catch (e) {}
    }
    if (mySeq !== seq) return;
    drop.innerHTML = "";
    const add = (label, kind, value) => {
      const it = el("div", "ta-item");
      it.append(el("span", "", label), el("span", "ta-kind", kind));
      it.onmousedown = (e) => { e.preventDefault(); input.value = value; drop.hidden = true; if (onSet) onSet(value); };
      drop.append(it);
    };
    const dedupe = new Set();
    (((entitiesCache || {}).contractors) || [])
      .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 5)
      .forEach((r) => { dedupe.add(r.name.toLowerCase()); add(r.name, "contractor", r.name); });
    ledgerNames().filter((n) => (!q || n.toLowerCase().includes(q)) && !dedupe.has(n.toLowerCase())).slice(0, 5)
      .forEach((n) => { dedupe.add(n.toLowerCase()); add(n, "history", n); });
    people.filter((r) => !dedupe.has(r.key)).forEach((r) => add(r.display, "person", r.key));
    const exact = dedupe.has(q) || people.some((r) => r.key === q);
    if (q && !exact) {
      const mk = el("div", "ta-item ta-create", 'create contractor "' + input.value.trim() + '" →');
      mk.onmousedown = async (e) => {
        e.preventDefault();
        try {
          const rec = await postJSONOk("/api/realestate/entities", { name: input.value.trim(), kind: "contractor" });
          await ensureEntities(true);
          input.value = rec.name;
          drop.hidden = true;
          showToast("Contractor record created: " + rec.name);
          if (onSet) onSet(rec.name);
        } catch (err) { showToast("Couldn't create contractor"); }
      };
      drop.append(mk);
    }
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  input.addEventListener("focus", refresh);
  input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  wrap.append(input, drop);
  return { el: wrap, value: () => input.value.trim(), setValue: (v) => { input.value = v; }, focus: () => input.focus() };
}
