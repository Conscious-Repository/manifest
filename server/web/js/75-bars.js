// ---- inline [[link]] live-preview for single-line fields (Obsidian-style) ----
// A field with [[links]] shows a rendered overlay (names, no brackets, links
// clickable) when not focused; clicking into it reveals the raw [[…]] for
// editing; clicking a link opens the note.
const wikilinkRe2 = /\[\[([^\]]+)\]\]/g;

function renderInlineLinks(host, text) {
  let last = 0, m;
  wikilinkRe2.lastIndex = 0;
  while ((m = wikilinkRe2.exec(text))) {
    if (m.index > last) host.appendChild(document.createTextNode(text.slice(last, m.index)));
    const parts = m[1].split("|");
    const target = parts[0].trim(), disp = (parts[1] || parts[0]).trim();
    const a = el("span", "inline-link", disp);
    a.addEventListener("mousedown", (e) => { e.preventDefault(); e.stopPropagation(); resolveWikilink(target); });
    host.appendChild(a);
    last = wikilinkRe2.lastIndex;
  }
  if (last < text.length) host.appendChild(document.createTextNode(text.slice(last)));
}

function attachInlineLinks(input) {
  if (input._inlineBound) return;
  input._inlineBound = true;
  let overlay = null;
  const hasLinks = () => /\[\[[^\]]+\]\]/.test(input.value);
  function render() {
    const parent = input.parentElement; // read lazily — may be attached after this call
    if (!parent) return;
    parent.classList.add("has-inline-overlay");
    if (!hasLinks() || document.activeElement === input) {
      if (overlay) overlay.style.display = "none";
      input.classList.remove("inline-hidden");
      return;
    }
    if (!overlay) { overlay = el("div", "inline-overlay"); parent.appendChild(overlay); }
    overlay.innerHTML = "";
    renderInlineLinks(overlay, input.value);
    const cs = getComputedStyle(input); // copy BEFORE hiding the input's text
    ["fontFamily", "fontSize", "fontWeight", "fontStyle", "letterSpacing", "color", "paddingLeft", "paddingRight", "textAlign"].forEach((p) => (overlay.style[p] = cs[p]));
    overlay.style.top = input.offsetTop + "px";
    overlay.style.left = input.offsetLeft + "px";
    overlay.style.width = input.offsetWidth + "px";
    overlay.style.height = input.offsetHeight + "px";
    overlay.style.lineHeight = input.offsetHeight + "px"; // vertically center the single line
    // the input's own text-decoration doesn't reach the overlay — mirror the
    // done (line-through) state so a completed task with a [[wikilink]] still
    // reads as struck through, matching a plain done task
    overlay.classList.toggle("done", input.classList.contains("done"));
    overlay.style.display = "block";
    input.classList.add("inline-hidden");
  }
  input.addEventListener("focus", render);
  input.addEventListener("blur", () => setTimeout(render, 0));
  setTimeout(render, 0); // defer: the input must be laid out for offset positioning
}

// ---- quick-lookup command bar (⌘K / Ctrl-K anywhere) ----
let cmdSel = -1, cmdResults = [];
function openCmdbar() {
  els.cmdbar.hidden = false;
  els.cmdbarInput.value = "";
  els.cmdbarResults.innerHTML = "";
  els.cmdbarCard.hidden = true;
  cmdSel = -1; cmdResults = [];
  els.cmdbarInput.focus();
  cmdSearch(""); // destinations show before a query is typed
}
function closeCmdbar() { els.cmdbar.hidden = true; }

let cmdTimer;
if (els.cmdbarInput) {
  els.cmdbarInput.addEventListener("input", () => {
    clearTimeout(cmdTimer);
    els.cmdbarCard.hidden = true;
    const q = els.cmdbarInput.value.trim();
    cmdTimer = setTimeout(() => cmdSearch(q), 150);
  });
  els.cmdbarInput.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); cmdMove(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); cmdMove(-1); }
    else if (e.key === "Enter") { e.preventDefault(); if (cmdResults[cmdSel]) cmdResults[cmdSel].act(); }
    else if (e.key === "Escape") { closeCmdbar(); }
  });
}
if (els.cmdbarBackdrop) els.cmdbarBackdrop.addEventListener("click", closeCmdbar);
window.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); openCmdbar(); }
  else if ((e.metaKey || e.ctrlKey) && e.key === "/") { e.preventDefault(); toggleRawOverlay(); }
  else if (e.key === "Escape" && rawOpen) { closeRawOverlay(); }
  else if (e.key === "Escape" && !els.cmdbar.hidden) { closeCmdbar(); }
  else if (e.key === "/" && els.castbar && els.castbar.hidden && !typingInField(e.target)) { e.preventDefault(); openCastbar(); }
  else if (e.key === "t" && !e.metaKey && !e.ctrlKey && !e.altKey && !typingInField(e.target) &&
    els.cmdbar.hidden && (!els.castbar || els.castbar.hidden)) {
    e.preventDefault();
    openTodoQuickAdd(); // ubiquitous capture — one line + domain, gone
  }
});

// ---- cast command bar (press / anywhere): run a vault skill or on-demand ritual ----
// A skill is cast through the sage spirit; a ritual runs on its own spirit. The
// argument box becomes the summoner's request (skills) or free-form ask (rituals).
let castItems = [], castFiltered = [], castSel = -1, castChosen = null;

function typingInField(t) {
  if (!t) return false;
  const tag = (t.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || t.isContentEditable;
}

async function openCastbar() {
  els.castbar.hidden = false;
  els.castbarInput.value = "";
  els.castbarResults.innerHTML = "";
  els.castbarArg.hidden = true;
  castSel = -1; castChosen = null; castFiltered = [];
  els.castbarInput.focus();
  try {
    const d = await (await fetch("/api/spirits/castables")).json();
    castItems = d.data || [];
  } catch (e) { castItems = []; }
  renderCastResults("");
}
function closeCastbar() { els.castbar.hidden = true; }

function renderCastResults(q) {
  const host = els.castbarResults; host.innerHTML = ""; castSel = -1;
  const needle = q.trim().toLowerCase();
  castFiltered = castItems.filter(c =>
    !needle || c.label.toLowerCase().includes(needle) || (c.description || "").toLowerCase().includes(needle)
  ).slice(0, 10);
  if (!castItems.length) {
    host.append(el("div", "cmd-empty", "No castable skills or rituals found."));
    return;
  }
  castFiltered.forEach((c, i) => {
    const row = el("div", "cmd-result");
    const kind = el("span", "cast-kind cast-kind-" + c.kind, c.kind === "skill" ? "skill" : "ritual");
    const name = el("span", "cmd-name", c.label);
    row.append(kind, name);
    if (c.description) row.append(el("span", "cast-desc", c.description));
    row.onclick = () => castChoose(c);
    row.onmouseenter = () => { castSel = i; paintCastSel(); };
    host.append(row);
  });
  if (castFiltered.length) { castSel = 0; paintCastSel(); }
}
function paintCastSel() {
  [...els.castbarResults.children].forEach((c, i) => c.classList.toggle("sel", i === castSel));
}
function castMove(d) {
  if (!castFiltered.length) return;
  castSel = (castSel + d + castFiltered.length) % castFiltered.length;
  paintCastSel();
}
function castChoose(c) {
  castChosen = c;
  els.castbarResults.innerHTML = "";
  els.castbarArg.hidden = false;
  els.castbarArgLabel.textContent = (c.kind === "skill" ? "Cast skill: " : "Run ritual: ") + c.label;
  els.castbarArgInput.value = "";
  els.castbarArgHint.textContent = c.kind === "skill"
    ? "sage · skill-cast"
    : c.spirit + " · " + c.ritual;
  els.castbarArgInput.focus();
}

async function castSubmit() {
  if (!castChosen) return;
  const body = {
    spirit: castChosen.spirit,
    ritual: castChosen.ritual,
    request: els.castbarArgInput.value.trim(),
    skill: castChosen.skill || "",
  };
  els.castbarCast.disabled = true;
  let res;
  try {
    res = await fetch("/api/spirits/run-now", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
    });
  } catch (e) { els.castbarArgHint.textContent = "spool failed"; els.castbarCast.disabled = false; return; }
  els.castbarCast.disabled = false;
  if (res.status === 409) { els.castbarArgHint.textContent = "already running — jumping to its live row"; }
  else if (!res.ok) { els.castbarArgHint.textContent = "spool failed — is the engine configured?"; return; }
  closeCastbar();
  // Jump to the runs board; the file-derived live poll picks it up (no watcher).
  location.hash = "#/spirits/runs";
  loadSpiritRuns();
  ensureLivePoll();
}

if (els.castbarInput) {
  els.castbarInput.addEventListener("input", () => renderCastResults(els.castbarInput.value));
  els.castbarInput.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); castMove(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); castMove(-1); }
    else if (e.key === "Enter") { e.preventDefault(); if (castFiltered[castSel]) castChoose(castFiltered[castSel]); }
    else if (e.key === "Escape") { e.preventDefault(); closeCastbar(); }
  });
}
if (els.castbarArgInput) {
  els.castbarArgInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); castSubmit(); }
    else if (e.key === "Escape") {
      e.preventDefault(); // back to the list, not all the way out
      els.castbarArg.hidden = true; castChosen = null;
      renderCastResults(els.castbarInput.value); els.castbarInput.focus();
    }
  });
}
if (els.castbarCast) els.castbarCast.addEventListener("click", castSubmit);
if (els.castbarBackdrop) els.castbarBackdrop.addEventListener("click", closeCastbar);

// ---- palette result set (redesign §2): every nav destination, every AION
// sub-tab, every property, and every contact — one keyboard-driven list.
let _cmdDests = null, _cmdProps = null;
function cmdDestinations() {
  if (_cmdDests) return _cmdDests;
  _cmdDests = [];
  NAV_SECTIONS.forEach((g) => g.items.forEach((it) =>
    _cmdDests.push({ name: it.label, hint: g.label.toLowerCase() + " · view", hash: it.hash })));
  [["Backlog", "#/aion"], ["Heuristics", "#/aion/heuristics"], ["V/TO", "#/aion/vto"],
   ["Goals", "#/aion/goals"], ["Org", "#/aion/org"], ["Reconcile", "#/aion/reconcile"],
   ["Settings", "#/aion/settings"]].forEach(([n, h]) =>
    _cmdDests.push({ name: "Aion · " + n, hint: "aion tab", hash: h }));
  [["Map", "#/properties/map"], ["Parcels", "#/properties/parcels"]].forEach(([n, h]) =>
    _cmdDests.push({ name: "Properties · " + n, hint: "properties view", hash: h }));
  return _cmdDests;
}
async function cmdProperties() {
  if (_cmdProps) return _cmdProps;
  try {
    const d = await (await fetch("/api/properties")).json();
    _cmdProps = (d.properties || []).map((p) => ({
      name: p.name || p.address || p.slug,
      hint: "property" + (p.status ? " · " + p.status : ""),
      hash: "#/properties/" + encodeURIComponent(p.slug),
    }));
  } catch (e) { _cmdProps = []; }
  return _cmdProps;
}

async function cmdSearch(q) {
  const host = els.cmdbarResults; host.innerHTML = ""; cmdSel = -1; cmdResults = [];
  const needle = q.toLowerCase();
  if (q) {
    // quick-add lives here too: first row turns the query into a todo
    const addRow = el("div", "cmd-result");
    addRow.append(el("span", "cmd-name", "＋ todo “" + q + "”"), el("span", "cmd-refs", "capture"));
    addRow.onclick = () => { closeCmdbar(); openTodoQuickAdd(q); };
    host.append(addRow);
  }
  const dests = cmdDestinations().concat(await cmdProperties())
    .filter((d) => !needle || d.name.toLowerCase().includes(needle))
    .slice(0, q ? 6 : 12)
    .map((d) => ({ name: d.name, hint: d.hint, act: () => { closeCmdbar(); location.hash = d.hash; } }));
  let contacts = [];
  if (q) {
    let d = { results: [] };
    try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
    contacts = (d.results || []).slice(0, 8).map((r) => ({
      name: r.display,
      hint: (r.hasNote ? "note" : "no note") + " · " + r.refCount + " ref" + (r.refCount === 1 ? "" : "s"),
      act: () => cmdShowCard(r.key),
    }));
  }
  if (q !== els.cmdbarInput.value.trim()) return; // a newer keystroke owns the list
  cmdResults = dests.concat(contacts);
  cmdResults.forEach((r, i) => {
    const row = el("div", "cmd-result");
    row.append(el("span", "cmd-name", r.name), el("span", "cmd-refs", r.hint));
    row.onclick = r.act;
    row.onmouseenter = () => { cmdSel = i; paintCmdSel(); };
    host.append(row);
  });
  if (cmdResults.length) { cmdSel = 0; paintCmdSel(); }
}
function cmdMove(d) { if (!cmdResults.length) return; cmdSel = (cmdSel + d + cmdResults.length) % cmdResults.length; paintCmdSel(); }
function paintCmdSel() {
  const kids = [...els.cmdbarResults.children];
  const offset = kids.length - cmdResults.length; // non-result rows (the ＋todo row) sit on top
  kids.forEach((c, i) => c.classList.toggle("sel", i - offset === cmdSel));
}

async function cmdShowCard(key) {
  let c;
  try {
    const res = await fetch("/api/contacts/card?key=" + encodeURIComponent(key));
    if (!res.ok) return;
    c = await res.json();
  } catch (e) { return; }
  const host = els.cmdbarCard; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "cmd-card-head");
  head.append(el("span", "cmd-card-name", c.display));
  if (!c.hasNote) head.append(el("span", "cmd-card-nonote", "no note"));
  host.append(head);
  const facts = el("div", "cmd-card-facts");
  facts.append(cmdFact("Last met", c.lastMet ? c.lastMet + " · calendar" : "—"));
  facts.append(cmdFact("Last mentioned", c.lastMentioned ? c.lastMentioned + " · notes" : "—"));
  facts.append(cmdFact("Next", c.nextUpcoming || "—"));
  if (c.latestTranscript) {
    const f = cmdFact("Latest transcript", c.latestTranscript.date + " · " + c.latestTranscript.title);
    f.classList.add("cmd-fact-link");
    f.onclick = () => { closeCmdbar(); _noteReturn = "#/contacts/" + encodeURIComponent(c.key); openNoteByPath(c.latestTranscript.path); };
    facts.append(f);
  }
  host.append(facts);
  const jump = pill("Open contact page →", () => { closeCmdbar(); location.hash = "#/contacts/" + encodeURIComponent(c.key); });
  host.append(jump);
}
function cmdFact(label, val) {
  const f = el("div", "cmd-fact");
  f.append(el("span", "cmd-fact-label", label), el("span", "cmd-fact-val", val));
  return f;
}

// ---- ⌘/ raw-markdown overlay (redesign §2): the file behind the current view ----
// Opens the markdown backing whatever is showing; saving goes through the same
// guarded PUT /api/note as the note view — never around vaultwriter.
let rawOpen = false, _rawEls = null;

// Which vault file(s) back the current route, most-likely first.
// Empty = no raw file for this view.
function rawPathsForRoute() {
  const h = normHash(location.hash);
  const sec = sectionOf(h);
  const sub = h.replace(/^#\//, "").split("/").filter(Boolean).slice(1).map(decodeURIComponent);
  if (sec === "day") return ["intrinsic/" + state.date + ".md", state.date + ".md"]; // newDailyDir first, legacy root second
  if (sec === "todos") return ["to do.md"];
  if (sec === "goals") return ["goals.md"];
  if (sec === "note") return [decodeURIComponent(h.slice("#/note/".length))];
  if (sec === "aion") {
    const orgFile = "system/aion/" + ((typeof aionOrgSel !== "undefined" && aionOrgSel) || "people") + ".md";
    const map = { "": "system/aion/backlog.md", heuristics: "system/aion/heuristics.md",
                  vto: "system/aion/vto.md", goals: "goals.md", // the aion GOALS tab reads the goals area
                  org: orgFile, reconcile: "system/aion/backlog.md" };
    const f = map[sub[0] || ""];
    return f ? [f] : [];
  }
  if (sec === "properties") {
    const tabs = ["work", "map", "parcels", "accounting", "contractors", "settings"];
    if (sub[0] && !tabs.includes(sub[0])) return ["system/realestate/properties/" + sub[0] + ".md"];
    return [];
  }
  return [];
}

function buildRawOverlay() {
  if (_rawEls) return _rawEls;
  const overlay = el("div", "cmdbar rawbar");
  overlay.hidden = true;
  const back = el("div", "cmdbar-backdrop");
  const card = el("div", "rawbar-card");
  const head = el("div", "rawbar-head");
  const path = el("span", "rawbar-path");
  const note = el("span", "rawbar-note", "raw markdown · edits write straight to the vault");
  const close = el("button", "rawbar-close", "✕ esc");
  head.append(path, note, close);
  const area = document.createElement("textarea");
  area.className = "rawbar-area";
  area.spellcheck = false;
  const foot = el("div", "rawbar-foot");
  const fix = el("span", "rawbar-fixpoint", "fixpoint guaranteed — parse→emit is byte-identical");
  const save = el("button", "rawbar-save", "save");
  foot.append(fix, save);
  card.append(head, area, foot);
  overlay.append(back, card);
  document.body.append(overlay);
  back.onclick = closeRawOverlay;
  close.onclick = closeRawOverlay;
  save.onclick = saveRawOverlay;
  area.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { e.preventDefault(); closeRawOverlay(); }
    else if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "Enter")) { e.preventDefault(); saveRawOverlay(); }
  });
  _rawEls = { overlay, path, note, area, save };
  return _rawEls;
}

async function openRawOverlay(explicitPath) {
  const candidates = explicitPath ? [explicitPath] : rawPathsForRoute();
  if (!candidates.length) { showToast("No raw file behind this view"); return; }
  const ui = buildRawOverlay();
  ui.path.textContent = candidates[0];
  ui.note.textContent = "raw markdown · edits write straight to the vault";
  ui.area.value = "";
  ui.overlay.hidden = false;
  rawOpen = true;
  let d = null;
  for (const p of candidates) {
    try {
      const res = await fetch("/api/note?path=" + encodeURIComponent(p));
      if (res.status === 404) continue;
      if (!res.ok) { ui.note.textContent = "couldn't load"; ui.save.disabled = true; return; }
      d = await res.json();
      ui.path.textContent = p;
      break;
    } catch (e) { ui.note.textContent = "couldn't load"; ui.save.disabled = true; return; }
  }
  if (!d) { ui.note.textContent = "note not found — nothing to edit yet"; ui.save.disabled = true; return; }
  ui.area.value = d.raw || "";
  ui.save.disabled = !!d.readOnly;
  if (d.readOnly) ui.note.textContent = "read-only — engine-owned note";
  ui.area.focus();
  ui.area.setSelectionRange(0, 0);
}

function closeRawOverlay() {
  if (_rawEls) _rawEls.overlay.hidden = true;
  rawOpen = false;
}

async function saveRawOverlay() {
  const ui = buildRawOverlay();
  const p = ui.path.textContent;
  ui.save.disabled = true;
  try {
    await putJSON("/api/note", { path: p, body: ui.area.value });
    showToast("Saved — " + p);
    closeRawOverlay();
    route(); // re-dispatch the current loader so the surface reflects the edit
  } catch (e) {
    ui.note.textContent = "save failed — " + (e && e.message ? e.message : "write refused");
    ui.save.disabled = false;
  }
}

function toggleRawOverlay() { rawOpen ? closeRawOverlay() : openRawOverlay(); }
