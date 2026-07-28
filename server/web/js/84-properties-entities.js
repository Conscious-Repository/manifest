// ---- entity & contractor records + autocomplete (pass-5 §5) ----

let entitiesCache = null; // {entities:[], contractors:[], bindings:{}}

async function ensureEntities(force) {
  if (entitiesCache && !force) return entitiesCache;
  try { entitiesCache = await (await fetch("/api/realestate/entities")).json(); }
  catch (e) { entitiesCache = { entities: [], contractors: [], bindings: {} }; }
  return entitiesCache;
}

// recordAutocomplete: the typeahead engine over entity/contractor records with
// a quiet `create "<name>" →` completion. Returns {el, value(), setValue(), focus()}.
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
            showToast(kind + ' record created: ' + rec.name);
            if (onPick) onPick(rec);
          } catch (err) { showToast("Couldn't create " + kind); }
        });
      }
    },
  });
  return ta;
}

// ownerAutocomplete: the SETTINGS owners field — suggests ENTITY records and
// PEOPLE from the vault (contacts search over person notes), so "brian
// anderson" links [[brian anderson]] to his actual note. Person hits are the
// vault's own graph; entity hits come from the records.
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

// contractorAutocomplete: the bid form's name field — suggests contractor
// records, PEOPLE from the vault (contacts search), and names already used
// across the ledgers, with the quiet create-record completion for new ones.
function contractorAutocomplete(placeholder, onSet) {
  const ledgerNames = () => {
    const seen = new Set();
    (propertyCache || []).forEach((p) => (p.ledger || []).forEach((r) => {
      const n = (r.contractor || r.vendor || "").trim();
      if (n) seen.add(n);
    }));
    return [...seen];
  };
  const ta = typeahead({
    placeholder,
    suggest: async (q, add) => {
      await ensureEntities();
      let people = [];
      if (q.length >= 2) {
        try {
          const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
          people = (d.results || []).filter((r) => r.isPerson && r.hasNote).slice(0, 5);
        } catch (e) {}
      }
      const pick = (label, kind, value) =>
        add(label, kind, () => { ta.commit(value); if (onSet) onSet(value); });
      const dedupe = new Set();
      (((entitiesCache || {}).contractors) || [])
        .filter((r) => !q || r.name.toLowerCase().includes(q)).slice(0, 5)
        .forEach((r) => { dedupe.add(r.name.toLowerCase()); pick(r.name, "contractor", r.name); });
      ledgerNames().filter((n) => (!q || n.toLowerCase().includes(q)) && !dedupe.has(n.toLowerCase())).slice(0, 5)
        .forEach((n) => { dedupe.add(n.toLowerCase()); pick(n, "history", n); });
      people.filter((r) => !dedupe.has(r.key)).forEach((r) => pick(r.display, "person", r.key));
      const exact = dedupe.has(q) || people.some((r) => r.key === q);
      if (q && !exact) {
        add('create contractor "' + ta.value() + '" →', "create", async () => {
          try {
            const rec = await postJSONOk("/api/realestate/entities", { name: ta.value(), kind: "contractor" });
            await ensureEntities(true);
            ta.commit(rec.name);
            showToast("Contractor record created: " + rec.name);
            if (onSet) onSet(rec.name);
          } catch (err) { showToast("Couldn't create contractor"); }
        });
      }
    },
  });
  return ta;
}
