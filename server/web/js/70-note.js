// ---- UNIVERSAL NOTE VIEW (contacts power-pass §1) ----
let _note = null; // {path, name, raw, backlinks, vault}
// where Back goes: auto-tracked by route() (the last non-note surface);
// sites may still set it explicitly for a more specific target.
let _noteReturn = "#/";

function showNote(path) {
  els.noteView.hidden = false;
  els.noteSaved.textContent = "";
  loadNote(path);
}

async function loadNote(path) {
  els.noteRendered.innerHTML = "Loading…";
  els.noteBacklinks.innerHTML = "";
  els.noteRaw.hidden = true;
  els.noteRendered.hidden = false;
  els.noteSaveBtn.hidden = true;
  els.noteRawToggle.textContent = "Edit raw";
  try {
    const res = await fetch("/api/note?path=" + encodeURIComponent(path));
    if (!res.ok) { els.noteRendered.textContent = "Note not found."; return; }
    _note = await res.json();
  } catch (e) { els.noteRendered.textContent = "Error loading note."; return; }
  els.noteTitle.textContent = _note.name;
  // quiet zone badge: system-zone notes are app-managed markdown
  if (_note.zone === "system") els.noteTitle.append(el("span", "note-zone-badge", "SYSTEM"));
  // engine-owned notes are read-only (the write guard refuses them) — hide edit
  els.noteRawToggle.hidden = !!_note.readOnly;
  els.noteObsidian.href = "obsidian://open?vault=" + encodeURIComponent(_note.vault) +
    "&file=" + encodeURIComponent(_note.path.replace(/\.md$/, ""));
  renderNoteBody();
  renderNoteBacklinks();
}

function renderNoteBody() {
  els.noteRendered.innerHTML = "";
  els.noteRendered.appendChild(renderMarkdown(_note.raw, _note.path));
}

function renderNoteBacklinks() {
  const host = els.noteBacklinks; host.innerHTML = "";
  const bl = _note.backlinks || [];
  if (!bl.length) return;
  host.appendChild(el("div", "note-bl-head", "Linked from " + bl.length + " note" + (bl.length === 1 ? "" : "s")));
  bl.forEach((b) => {
    const row = el("div", "note-bl-row");
    row.append(el("span", "note-bl-date", b.date || ""), el("span", "note-bl-name", b.name));
    row.onclick = () => openNoteByPath(b.path);
    host.appendChild(row);
  });
}

function openNoteByPath(path) {
  location.hash = "#/note/" + encodeURIComponent(path);
}

async function resolveWikilink(target) {
  let r;
  try { r = await (await fetch("/api/note/resolve?target=" + encodeURIComponent(target))).json(); }
  catch (e) { return; }
  if (r.kind === "contact") location.hash = "#/contacts/" + encodeURIComponent(r.key);
  else if (r.kind === "note") openNoteByPath(r.path);
  else els.noteSaved.textContent = "no note for [[" + target + "]]";
}

async function toggleNoteTask(line, want, box) {
  try {
    const res = await fetch("/api/note/task", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: _note.path, line, want }) });
    if (!res.ok) throw new Error(await res.text());
    // refresh raw so subsequent toggles use correct line state
    const g = await (await fetch("/api/note?path=" + encodeURIComponent(_note.path))).json();
    _note.raw = g.raw; _note.backlinks = g.backlinks;
  } catch (e) { box.checked = !want; els.noteSaved.textContent = "toggle failed — reload"; }
}

// raw-edit toggle + save
if (els.noteRawToggle) els.noteRawToggle.addEventListener("click", () => {
  const editing = !els.noteRaw.hidden;
  if (editing) { // back to rendered
    els.noteRaw.hidden = true; els.noteRendered.hidden = false; els.noteSaveBtn.hidden = true;
    els.noteRawToggle.textContent = "Edit raw";
    renderNoteBody();
  } else {
    els.noteRaw.value = _note.raw; els.noteRaw.hidden = false; els.noteRendered.hidden = true;
    els.noteSaveBtn.hidden = false; els.noteRawToggle.textContent = "Preview";
  }
});
if (els.noteSaveBtn) els.noteSaveBtn.addEventListener("click", async () => {
  els.noteSaved.textContent = "saving…";
  try {
    const res = await fetch("/api/note", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: _note.path, body: els.noteRaw.value }) });
    if (!res.ok) throw new Error(await res.text());
    els.noteSaved.textContent = "saved";
    await loadNote(_note.path); // reindex happened server-side; re-render fresh
  } catch (e) { els.noteSaved.textContent = "save failed"; }
});
if (els.noteBackBtn) els.noteBackBtn.addEventListener("click", () => { location.hash = _noteReturn || "#/contacts"; });

// ---- ARTIFACT READER — a full-page reader for agent artifacts (research
// briefs) and run reports. Reuses the note view's reading surface (.note-view
// + .note-rendered) but reads through the spirits API — the harness tree lives
// OUTSIDE the vault, so this is strictly read-only: no raw edit, no backlinks,
// no Obsidian link (there is no vault path). Routed as
// #/artifact/ref/<harness>/<encRef> and #/artifact/run/<runId>. ----
let _artifactRaw = "";
async function showArtifact(tail) {
  els.artifactView.hidden = false;
  els.artifactRendered.innerHTML = "Loading…";
  els.artifactSource.textContent = "";
  els.artifactTitle.textContent = "";
  _artifactRaw = "";
  const parts = (tail || "").split("/");
  const kind = parts[0];
  let raw = "", title = "artifact", source = "";
  try {
    if (kind === "ref") {
      const harness = decodeURIComponent(parts[1] || "");
      const ref = decodeURIComponent(parts.slice(2).join("/") || "");
      const q = "/api/spirits/file?path=" + encodeURIComponent(ref) + (harness ? "&harness=" + encodeURIComponent(harness) : "");
      const r = await fetch(q);
      if (!r.ok) throw new Error("HTTP " + r.status);
      raw = (await r.json()).content || "";
      title = artifactTitleFromRef(ref);
      source = ref;
    } else if (kind === "run") {
      const runId = decodeURIComponent(parts.slice(1).join("/") || "");
      const r = await fetch("/api/spirits/runs/" + encodeURIComponent(runId));
      if (!r.ok) throw new Error("HTTP " + r.status);
      const run = await r.json();
      const s = run.summary || {};
      raw = run.body || "(empty report)";
      title = (s.spirit || "?") + " / " + (s.ritual || "?");
      source = [s.outcome, s.request].filter(Boolean).join("  ·  ");
    } else { throw new Error("unknown artifact route"); }
  } catch (e) {
    els.artifactRendered.textContent = "Couldn't open this artifact — " + (e.message || e);
    return;
  }
  _artifactRaw = raw;
  els.artifactTitle.textContent = title;
  els.artifactSource.textContent = source;
  els.artifactRendered.innerHTML = "";
  els.artifactRendered.appendChild(renderMarkdown(raw, null, { readOnly: true }));
}

// artifactTitleFromRef: humanize the library filename for the header/crumb.
// The document's own H1 carries the full title; this just labels the chrome.
function artifactTitleFromRef(ref) {
  const base = (ref || "").replace(/^.*\//, "").replace(/\.md$/, "");
  return base.replace(/^\d{4}-\d{2}-\d{2}-/, "").replace(/-/g, " ") || "artifact";
}

if (els.artifactBackBtn) els.artifactBackBtn.addEventListener("click", () => { location.hash = _noteReturn || "#/feed"; });
if (els.artifactCopy) els.artifactCopy.addEventListener("click", async () => {
  try {
    if (!navigator.clipboard) throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(_artifactRaw || "");
    showToast("Copied markdown");
  } catch (e) { showToast("Copy failed — clipboard unavailable", null, "error"); }
});

// --- a compact markdown renderer that returns DOM (so wikilinks + checkboxes
// are interactive). Handles the shapes this vault uses. opts.readOnly renders
// a document that is NOT the open note (an agent artifact, a run report): its
// checkboxes are inert, since there is no vault line behind them to toggle. ---
function renderMarkdown(raw, notePath, opts) {
  const readOnly = !!(opts && opts.readOnly);
  const frag = document.createDocumentFragment();
  const lines = raw.split("\n");
  let i = 0;
  // skip a leading frontmatter block (metadata; editable in raw mode)
  if (lines[0] === "---") {
    let j = 1; while (j < lines.length && lines[j] !== "---") j++;
    if (j < lines.length) i = j + 1;
  }
  let para = [];
  const flushPara = () => {
    if (!para.length) return;
    const p = el("p", "md-p");
    inlineInto(p, para.join(" "), notePath);
    frag.appendChild(p); para = [];
  };
  for (; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();
    // code fence
    if (t.startsWith("```")) {
      flushPara();
      const code = []; i++;
      for (; i < lines.length && !lines[i].trim().startsWith("```"); i++) code.push(lines[i]);
      const pre = el("pre", "md-pre"); pre.textContent = code.join("\n"); frag.appendChild(pre);
      continue;
    }
    // heading
    let hm = line.match(/^(#{1,6})\s+(.*)$/);
    if (hm) { flushPara(); const h = el("h" + hm[1].length, "md-h"); inlineInto(h, hm[2], notePath); frag.appendChild(h); continue; }
    // GFM table: a `| … |` header row immediately followed by a `|---|` rule
    if (t.startsWith("|") && i + 1 < lines.length) {
      const sep = lines[i + 1].trim();
      if (sep.includes("-") && /^\|?[\s:|-]+\|[\s:|-]*$/.test(sep)) {
        flushPara();
        const splitRow = (s) => { let x = s.trim(); if (x.startsWith("|")) x = x.slice(1); if (x.endsWith("|")) x = x.slice(0, -1); return x.split("|").map((c) => c.trim()); };
        const header = splitRow(t);
        let j = i + 2; const bodyRows = [];
        for (; j < lines.length && lines[j].trim().startsWith("|"); j++) bodyRows.push(splitRow(lines[j].trim()));
        const wrap = el("div", "md-table-wrap");
        const table = el("table", "md-table");
        const thead = el("thead"), htr = el("tr");
        header.forEach((c) => { const th = el("th"); inlineInto(th, c, notePath); htr.appendChild(th); });
        thead.appendChild(htr); table.appendChild(thead);
        const tbody = el("tbody");
        bodyRows.forEach((cells) => {
          const tr = el("tr");
          for (let k = 0; k < header.length; k++) { const td = el("td"); inlineInto(td, cells[k] || "", notePath); tr.appendChild(td); }
          tbody.appendChild(tr);
        });
        table.appendChild(tbody); wrap.appendChild(table); frag.appendChild(wrap);
        i = j - 1; continue;
      }
    }
    // checkbox
    let cb = line.match(/^(\s*)[-*]\s+\[([ xX])\]\s?(.*)$/);
    if (cb) {
      flushPara();
      const row = el("label", "md-task");
      const box = el("input"); box.type = "checkbox"; box.checked = cb[2] !== " ";
      const lineNo = i;
      if (readOnly) box.disabled = true;
      else box.addEventListener("change", () => toggleNoteTask(lineNo, box.checked, box));
      const span = el("span", "md-task-text"); inlineInto(span, cb[3], notePath);
      row.append(box, span); frag.appendChild(row); continue;
    }
    // list item
    let li = line.match(/^(\s*)[-*]\s+(.*)$/) || line.match(/^(\s*)\d+\.\s+(.*)$/);
    if (li) { flushPara(); const item = el("div", "md-li"); item.append(el("span", "md-bullet", "•")); const s = el("span"); inlineInto(s, li[2], notePath); item.append(s); frag.appendChild(item); continue; }
    // blockquote
    if (t.startsWith(">")) { flushPara(); const bq = el("blockquote", "md-bq"); inlineInto(bq, t.replace(/^>\s?/, ""), notePath); frag.appendChild(bq); continue; }
    // horizontal rule
    if (t === "---" || t === "***") { flushPara(); frag.appendChild(el("hr", "md-hr")); continue; }
    // blank → paragraph break
    if (t === "") { flushPara(); continue; }
    para.push(t);
  }
  flushPara();
  return frag;
}

// inlineInto parses inline markdown (wikilinks, links, bold/italic/code) into DOM.
function inlineInto(host, text, notePath) {
  // token regex: [[wikilink]] | [text](url) | **bold** | *italic* | `code`
  const re = /\[\[([^\]]+)\]\]|\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`/g;
  let last = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > last) host.appendChild(document.createTextNode(text.slice(last, m.index)));
    if (m[1] != null) { // wikilink
      const parts = m[1].split("|");
      const target = parts[0].trim(), disp = (parts[1] || parts[0]).trim();
      const a = el("span", "wikilink", disp);
      a.onclick = () => resolveWikilink(target);
      host.appendChild(a);
    } else if (m[2] != null) { // [text](url)
      const a = el("a", "md-link", m[2]); a.href = m[3]; a.target = "_blank";
      // external source links (http/doi) read as citations — accent + ↗ glyph
      if (/^https?:\/\//i.test(m[3]) || /doi\.org/i.test(m[3])) { a.classList.add("md-cite"); a.rel = "noopener noreferrer"; }
      host.appendChild(a);
    } else if (m[4] != null) { host.appendChild(el("strong", null, m[4])); }
    else if (m[5] != null) { host.appendChild(el("em", null, m[5])); }
    else if (m[6] != null) { host.appendChild(el("code", "md-code", m[6])); }
    last = re.lastIndex;
  }
  if (last < text.length) host.appendChild(document.createTextNode(text.slice(last)));
}

// ---- [[wikilink]] autocomplete for markdown editors (Obsidian-style) ----
// Typing `[[` opens a popup that searches entities and narrows as you type;
// picking one inserts `[[<lowercase name>]]`. The dropdown shows the plain
// lowercase name (no brackets).
let _wlPopup = null, _wlItems = [], _wlSel = -1, _wlStart = -1, _wlTa = null, _wlTimer = null;

function wlClose() {
  if (_wlPopup) { _wlPopup.remove(); _wlPopup = null; }
  _wlItems = []; _wlSel = -1; _wlStart = -1; _wlTa = null;
}

// wlQuery finds an open, unclosed `[[…` immediately before the caret.
function wlQuery(ta) {
  const pos = ta.selectionStart;
  const before = ta.value.slice(0, pos);
  const open = before.lastIndexOf("[[");
  if (open < 0) return null;
  const between = before.slice(open + 2);
  if (between.includes("]]") || between.includes("\n")) return null;
  return { start: open, query: between };
}

function attachWikilinkAutocomplete(ta) {
  if (!ta || ta._wlBound) return;
  ta._wlBound = true;
  ta.addEventListener("input", () => {
    const q = wlQuery(ta);
    if (!q) { wlClose(); return; }
    _wlTa = ta; _wlStart = q.start;
    clearTimeout(_wlTimer);
    _wlTimer = setTimeout(() => wlSearch(ta, q.query), 100);
  });
  ta.addEventListener("keydown", (e) => {
    if (!_wlPopup || !_wlItems.length) return;
    if (e.key === "ArrowDown") { e.preventDefault(); _wlSel = (_wlSel + 1) % _wlItems.length; wlPaint(); }
    else if (e.key === "ArrowUp") { e.preventDefault(); _wlSel = (_wlSel - 1 + _wlItems.length) % _wlItems.length; wlPaint(); }
    else if (e.key === "Enter" || e.key === "Tab") { e.preventDefault(); if (_wlItems[_wlSel]) wlInsert(_wlItems[_wlSel]); }
    else if (e.key === "Escape") { e.preventDefault(); wlClose(); }
  });
  ta.addEventListener("blur", () => setTimeout(() => { if (_wlTa === ta) wlClose(); }, 150));
  ta.addEventListener("scroll", () => { if (_wlPopup && _wlTa === ta) wlPosition(ta); });
}

async function wlSearch(ta, query) {
  let results = [];
  try { results = (await (await fetch("/api/contacts/search?q=" + encodeURIComponent(query || ""))).json()).results || []; } catch (e) {}
  // drop dated meeting notes (e.g. "2026-05-19 shoumik sync") — you link names, not dates
  results = results.filter((r) => !/^\d{4}-\d{2}-\d{2}/.test(r.key));
  _wlItems = results.slice(0, 8);
  if (!_wlItems.length) { wlClose(); return; }
  _wlSel = 0;
  if (!_wlPopup) { _wlPopup = el("div", "wl-popup"); document.body.appendChild(_wlPopup); }
  _wlPopup.innerHTML = "";
  _wlItems.forEach((it, i) => {
    const row = el("div", "wl-item");
    row.append(el("span", "wl-name", it.key)); // lowercase name, no brackets
    if (it.refCount) row.append(el("span", "wl-refs", it.refCount + " ref" + (it.refCount === 1 ? "" : "s")));
    row.addEventListener("mousedown", (e) => { e.preventDefault(); wlInsert(it); }); // mousedown beats blur
    row.addEventListener("mouseenter", () => { _wlSel = i; wlPaint(); });
    _wlPopup.appendChild(row);
  });
  wlPaint();
  wlPosition(ta);
}

function wlPaint() {
  if (!_wlPopup) return;
  [..._wlPopup.children].forEach((c, i) => c.classList.toggle("sel", i === _wlSel));
}

function wlInsert(it) {
  const ta = _wlTa; if (!ta) return;
  const pos = ta.selectionStart;
  const ins = "[[" + it.key + "]]";
  ta.value = ta.value.slice(0, _wlStart) + ins + ta.value.slice(pos);
  const np = _wlStart + ins.length;
  wlClose();
  ta.focus();
  ta.setSelectionRange(np, np);
  ta.dispatchEvent(new Event("input", { bubbles: true })); // run the field's own state/save handlers
}

function wlPosition(ta) {
  if (!_wlPopup) return;
  const c = caretCoords(ta, ta.selectionStart);
  const maxLeft = window.innerWidth - _wlPopup.offsetWidth - 12;
  _wlPopup.style.left = Math.round(Math.min(c.left, Math.max(8, maxLeft))) + "px";
  _wlPopup.style.top = Math.round(c.top) + "px";
}

// caretCoords returns viewport coords just below the caret (mirror-div technique).
// Textareas wrap; single-line inputs don't, and the popup sits below the field.
function caretCoords(ta, position) {
  const isInput = ta.tagName === "INPUT";
  const s = getComputedStyle(ta);
  const div = document.createElement("div");
  const props = ["boxSizing", "width", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth",
    "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "fontFamily", "fontSize", "fontWeight",
    "fontStyle", "lineHeight", "letterSpacing", "textTransform", "wordSpacing", "tabSize"];
  props.forEach((p) => (div.style[p] = s[p]));
  div.style.position = "absolute";
  div.style.visibility = "hidden";
  div.style.whiteSpace = isInput ? "pre" : "pre-wrap";
  div.style.wordWrap = "break-word";
  div.style.overflow = "hidden";
  if (isInput) div.style.width = "auto";
  div.textContent = ta.value.slice(0, position);
  const span = document.createElement("span");
  span.textContent = ta.value.slice(position) || ".";
  div.appendChild(span);
  document.body.appendChild(div);
  const rect = ta.getBoundingClientRect();
  const lh = parseFloat(s.lineHeight) || parseFloat(s.fontSize) * 1.4;
  const left = rect.left + (span.offsetLeft - ta.scrollLeft);
  const top = isInput ? rect.bottom + 2 : rect.top + (span.offsetTop - ta.scrollTop) + lh;
  document.body.removeChild(div);
  return { left, top };
}

if (els.noteRaw) attachWikilinkAutocomplete(els.noteRaw);
