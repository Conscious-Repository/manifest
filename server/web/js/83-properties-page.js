// ---- PROPERTY PAGE — Revision 3: one property, one scroll ----
// Address + status select · BUDGET/SPENT strip · TODOS (composer always
// present; selecting a row opens the assignment inspector) · LEDGER.
// Nothing else — docs, contractors, the log and the work plan come off the
// page; reach them with ⌘/ or the note view. Money: budget/spent still
// derive from `## work` est + the ledger (the files keep their sections).
let propSelTodoId = null; // line-id of the inspector's todo

async function renderPropertyPage(slug) {
  const host = els.propertyPage;
  host.innerHTML = "";
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

  // BUDGET · SPENT — two mono figures between hairlines
  const pm = projMoney(p);
  const strip = el("div", "pp3-strip");
  const cell = (label, val, cls) => {
    const c = el("div", "pp3-cell");
    c.append(el("div", "pp3-cell-label", label), el("div", "pp3-cell-val" + (cls ? " " + cls : ""), val));
    return c;
  };
  strip.append(cell("BUDGET", pm.budget ? fmtMoney(pm.budget) : "—"));
  strip.append(cell("SPENT", pm.paid ? fmtMoney(pm.paid) : "—", pm.over ? "over" : ""));
  host.append(strip);

  // TODOS — the primary section; adding is the page's first-class action
  const todosSec = el("div", "pp3-sec");
  const th = el("div", "pp3-sec-head");
  const open = (p.todos || []).filter((t) => !t.checked);
  th.append(el("span", "pp3-sec-title", "TODOS"), el("span", "pp3-sec-count", String(open.length)));
  todosSec.append(th);
  open.forEach((t) => todosSec.append(propTodoRow(p, t)));
  (p.todos || []).filter((t) => t.checked).forEach((t) => todosSec.append(propTodoRow(p, t)));
  todosSec.append(propTodoComposer(p));
  host.append(todosSec);

  // LEDGER — date · category · vendor · amount, read-only
  const ledger = el("div", "pp3-sec");
  const lh = el("div", "pp3-sec-head");
  lh.append(el("span", "pp3-sec-title", "LEDGER"), el("span", "pp3-sec-count", String((p.ledger || []).length)));
  ledger.append(lh);
  (p.ledger || []).forEach((r) => {
    const row = el("div", "pp3-ledger-row");
    row.append(el("span", "", r.date || ""));
    row.append(el("span", "", r.category || r.type || ""));
    row.append(el("span", "pp3-ledger-vendor", r.vendor || r.contractor || ""));
    row.append(el("span", "pp3-ledger-amt", r.amount ? fmtMoney(r.amount) : ""));
    ledger.append(row);
  });
  if (!(p.ledger || []).length) ledger.append(el("div", "pp-empty", "No money facts yet."));
  host.append(ledger);

  // restore an open inspector across re-renders
  if (propSelTodoId) {
    const t = (p.todos || []).find((x) => x.id === propSelTodoId);
    if (t) openPropInspector(p, t); else closePropInspector();
  }
}

function compositeId(p, t) { return "prop:" + p.slug + "/" + t.id; }

function propTodoRow(p, t) {
  const row = el("div", "pp3-todo" + (t.checked ? " done" : "") + (propSelTodoId === t.id ? " sel" : ""));
  const check = el("button", "tdo-check" + (t.checked ? " on" : ""), t.checked ? "✓" : "○");
  check.title = t.checked ? "reopen" : "done";
  check.onclick = async (e) => {
    e.stopPropagation();
    try { await postJSONOk("/api/todos/check", { id: compositeId(p, t), checked: !t.checked }); renderProperties(); }
    catch (err) { showToast("Couldn't update"); }
  };
  row.append(check, el("span", "pp3-todo-text", t.text));
  row.append(el("span", "prop-owner" + (mineOwner(t.owner) ? " mine" : ""), assigneeName(t.owner)));
  row.onclick = () => {
    propSelTodoId = propSelTodoId === t.id ? null : t.id;
    propSelTodoId ? openPropInspector(p, t) : closePropInspector();
    // repaint selection without a full reload
    els.propertyPage.querySelectorAll(".pp3-todo.sel").forEach((n) => n.classList.remove("sel"));
    if (propSelTodoId) row.classList.add("sel");
  };
  return row;
}

// the always-present composer row — adding a todo is the page's primary action
function propTodoComposer(p) {
  const row = el("div", "pp3-compose");
  row.append(el("span", "pp3-compose-glyph", "○"));
  const input = inputEl("add a todo for this property…");
  input.className = "pp3-compose-in";
  const submit = async () => {
    const text = input.value.trim();
    if (!text) return;
    try {
      await postJSONOk("/api/todos/item", { text, container: { kind: "property", slug: p.slug } });
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
  const host = els.propInspector;
  host.innerHTML = "";
  host.hidden = false;
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
      ? "Assigned to " + assigneeName(sel.value) + " — tracked here, never in your TODOS. It shows under Outstanding until they close it."
      : "Yours — it shows in TODOS under Real Estate.";
  };
  setNote();
  sel.onchange = async () => {
    try {
      await postJSONOk("/api/todos/update", { id: compositeId(p, t), owner: sel.value });
      t.owner = sel.value;
      setNote();
      renderProperties();
    } catch (e) { showToast("Couldn't assign"); }
  };
  host.append(field("owner", sel));
  host.append(field("property", el("span", "pp3-insp-val", p.short || p.address || p.slug)));
  if (t.added) host.append(field("added", el("span", "pp3-insp-val", t.added)));
  host.append(note);
}

function closePropInspector() {
  propSelTodoId = null;
  if (els.propInspector) { els.propInspector.hidden = true; els.propInspector.innerHTML = ""; }
}

async function putJSON(url, body) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}
