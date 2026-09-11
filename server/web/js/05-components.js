// ---- component library (§11 / Pass B1) ----
// The shared userland primitives every tab builds from. One implementation per
// family: DOM helpers, pill/button factories, the ghost input, the collapsible
// section, and THE typeahead engine (the `ta-wrap` inline dropdown — five
// former copies). The command palettes (cmdbar/castbar) and the textarea
// wikilink autocomplete are separate components by design: an overlay with a
// selection index and a caret-tracking mirror are different interaction
// models, not copies of this dropdown.

// ---- DOM helpers ----
function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
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
function linkEl(text, href) { const a = el("a", null, text); a.href = href; a.target = "_blank"; a.rel = "noopener"; return a; }
function emptyRow(text) { return el("div", "ro-row empty", text); }
function splitList(s) { return (s || "").split(",").map((x) => x.trim()).filter(Boolean); }

// ---- pill factory ----
function pill(text, onclick) { const b = el("button", "pill", text); b.addEventListener("click", onclick); return b; }
// debounce — one call per pause, not per keystroke. Four hand-rolled copies of
// this existed (reading lookup, contact search, wikilink popup, ⌘K) before it
// was worth naming.
function debounce(fn, ms) {
  let t = null;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

function pillLight(text, onclick) { const b = el("button", "pill light", text); b.addEventListener("click", onclick); return b; }

// askText — a small inline text dialog (reuses the picker modal chrome), the
// sanctioned replacement for prompt() app-wide (ui-conventions.md §buttons).
function askText(title, placeholder, onSubmit) {
  els.pickerTitle.textContent = title;
  const body = els.pickerBody; body.innerHTML = "";
  const ta = el("textarea", "asktext-area"); ta.placeholder = placeholder; ta.rows = 3;
  const actions = el("div", "asktext-actions");
  const submit = pill("send →", () => { closePicker(); onSubmit(ta.value); });
  actions.append(el("span", "asktext-hint", "⌘↵ to send"), submit);
  body.append(ta, actions);
  ta.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); closePicker(); onSubmit(ta.value); }
    else if (e.key === "Escape") { e.preventDefault(); closePicker(); }
  });
  els.pickerModal.hidden = false;
  ta.focus();
}

// inlineRename — THE rename idiom (ui-conventions.md §buttons): the name node
// swaps for an input prefilled with the current value; Enter commits, Escape
// restores, blur commits. onCommit(v) fires only when the trimmed value is
// non-empty and changed. Promoted 2026-09-04 from the terminal + files rails
// (two tab-local copies) so the chat head/rows use the same behaviour.
// Returns the input (a caller may adjust the selection, e.g. up to the ext).
function inlineRename(nameEl, value, onCommit) {
  const inp = document.createElement("input");
  inp.className = "inline-rename";
  inp.value = value || "";
  inp.spellcheck = false;
  let settled = false;
  const settle = (commit) => {
    if (settled) return;
    settled = true;
    const v = inp.value.trim();
    inp.replaceWith(nameEl);
    if (commit && v && v !== (value || "")) onCommit(v);
  };
  inp.onkeydown = (e) => {
    e.stopPropagation();
    if (e.key === "Enter") { e.preventDefault(); settle(true); }
    else if (e.key === "Escape") { e.preventDefault(); settle(false); }
  };
  inp.onblur = () => settle(true);
  inp.onclick = (e) => e.stopPropagation();
  nameEl.replaceWith(inp);
  inp.focus();
  inp.select();
  return inp;
}

// armedDelete — the destructive-action pattern: first click ARMS (ink
// "confirm?" label), second click within 4s executes; it disarms itself.
// No browser dialogs (owner call, agents UX pass). Library since 2026-09-04
// (was in 40-agents.js; ten files consume it).
function armedDelete(label, armedLabel, onConfirm) {
  const b = el("button", "sprt-quiet sprt-delete", label);
  let armed = false, timer = null;
  b.onclick = (e) => {
    if (e) e.stopPropagation(); // rows that open on click must not open on arm
    if (!armed) {
      armed = true;
      b.textContent = armedLabel;
      b.classList.add("armed");
      timer = setTimeout(() => { armed = false; b.textContent = label; b.classList.remove("armed"); }, 4000);
      return;
    }
    clearTimeout(timer);
    b.disabled = true;
    onConfirm();
  };
  return b;
}

// ---- relative timestamp ----
function fmtWhen(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d)) return String(iso).slice(0, 16).replace("T", " ");
  const now = new Date();
  if (Math.abs(d - now) < 86400000 && d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  }
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

// ---- cardShell: THE card factory (A1). Every FEED-rendered card is
// .feed-card + a kind modifier, built through this one function. Anatomy in
// order: .feed-top (chips → title → right-aligned .feed-date) · optional
// .feed-why · optional body slot(s) · optional .feed-meta (why always
// precedes meta) · .feed-actions (via cardActions) last. A builder with
// interior content too particular for one declarative call (the re-contract
// and goals-item editors) still uses cardShell for the header/actions and
// appends its own body straight onto the returned card before finishing with
// cardActions — the frame unifies, the unique content stays.
//
//   opts.kind      extra class(es) after "feed-card", e.g. "artifact pinned"
//   opts.dataset   {attr: value} set on the card root
//   opts.chips     elements for .feed-top, before the title (falsy entries skipped)
//   opts.title     element or string for .feed-title
//   opts.date      an ISO string (rendered via fmtWhen) or a pre-built element
//   opts.why       string or element for .feed-why
//   opts.body      element or array of elements, appended after .feed-why
//   opts.meta      string or element for .feed-meta
//   opts.actions   array of button elements → .feed-actions (cardActions)
function cardShell(opts) {
  opts = opts || {};
  // opts.approval (not a plain string in opts.kind) keeps the "approval-card"
  // identity class — like "feed-card" itself — decided in exactly one place.
  const card = el("div", ["feed-card", opts.approval ? "approval-card" : "", opts.kind].filter(Boolean).join(" "));
  if (opts.dataset) Object.keys(opts.dataset).forEach((k) => { card.dataset[k] = opts.dataset[k]; });
  const top = el("div", "feed-top");
  (opts.chips || []).forEach((c) => c && top.append(c));
  if (opts.title != null) {
    const t = opts.title.nodeType ? opts.title : el("span", "", opts.title);
    t.classList.add("feed-title");
    top.append(t);
  }
  if (opts.date) top.append(opts.date.nodeType ? opts.date : el("span", "feed-date", fmtWhen(opts.date)));
  card.append(top);
  if (opts.why) card.append(opts.why.nodeType ? opts.why : el("div", "feed-why", opts.why));
  (Array.isArray(opts.body) ? opts.body : opts.body ? [opts.body] : []).forEach((b) => b && card.append(b));
  if (opts.meta) card.append(opts.meta.nodeType ? opts.meta : el("div", "feed-meta", opts.meta));
  if (opts.actions) card.append(cardActions(opts.actions));
  return card;
}

// cardActions — the .feed-actions row (A1: always last). Exposed separately
// so a card that appends custom content between its header and its actions
// (an editor, a diff, a blocked-reason line) can still finish through the one
// shared row instead of hand-rolling `el("div", "feed-actions")`.
function cardActions(buttons) {
  const actions = el("div", "feed-actions");
  (buttons || []).forEach((b) => b && actions.append(b));
  return actions;
}

// ---- the one feed-card write discipline (A2) ----
// feedPost does the actual write: POST with an r.ok check, an explicit-kind
// error toast on refusal, setSaveState throughout. It replaces raw fetch()
// calls and postJSON (which never checks r.ok, so a refused 4xx silently
// read as success) across the four card decision bodies this stage unifies.
async function feedPost(url, body) {
  setSaveState("saving");
  try {
    const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!r.ok) {
      const why = (await r.text().catch(() => "")).trim();
      setSaveState("error");
      showToast("Not applied — " + (why.replace(/^apply refused:\s*/i, "") || ("HTTP " + r.status)).slice(0, 160), null, "error");
      return false;
    }
    setSaveState("saved");
    return true;
  } catch (e) {
    setSaveState("error");
    showToast("Couldn't reach the server — " + String(e.message || e).slice(0, 100), null, "error");
    return false;
  }
}

// cardDecide — the standard optimistic-write shape a feed card's clearing
// verb takes: pull the card out of the DOM, feedPost the decision, and put it
// right back if the write was refused or the network failed — a refused
// decision must be visible, never a silent no-op (the swallowed-4xx class
// this replaces). onOk runs only once the write is confirmed; the caller
// still decides whether/how to reload the list afterward.
async function cardDecide(card, url, body, onOk) {
  const next = card.nextSibling, parent = card.parentNode;
  card.remove();
  const ok = await feedPost(url, body);
  if (!ok) { if (next) next.before(card); else if (parent) parent.append(card); return false; }
  if (onOk) onOk();
  return true;
}

// ---- ghost input (goals-lineage): a quiet ＋ button that swaps into an input;
// Enter/blur commits, Escape restores the ghost ----
function ghostInput(label, cls, onSubmit, placeholder) {
  const ghost = el("button", "o-ghost " + (cls || ""), label);
  ghost.addEventListener("click", (e) => {
    e.stopPropagation();
    const input = document.createElement("input");
    input.className = "o-edit o-ghost-edit"; // block: the open input gets its own line
    input.placeholder = placeholder || label.replace(/^[＋+]\s*/, "");
    ghost.replaceWith(input);
    input.focus();
    let settled = false;
    const settle = (commit) => {
      if (settled) return;
      settled = true;
      const v = input.value.trim();
      if (commit && v) onSubmit(v);
      else input.replaceWith(ghost);
    };
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") settle(true);
      else if (ev.key === "Escape") settle(false);
    });
    input.addEventListener("blur", () => settle(true));
  });
  return ghost;
}

// ---- collapsible section: pp-section-head with a caret + collapsed summary ----
let sectionSequence = 0;
function collapsibleSection(host, title, summary, open) {
  const head = el("button", "pp-section-head toggle");
  head.type = "button";
  const caret = el("span", "sec-caret", open ? "▾" : "▸");
  caret.setAttribute("aria-hidden", "true");
  head.append(caret, el("span", "", title));
  const sum = el("span", "sec-summary", summary || "");
  head.append(sum);
  const body = el("div", "sec-body");
  body.id = "section-" + (++sectionSequence);
  head.setAttribute("aria-controls", body.id);
  head.setAttribute("aria-expanded", String(!!open));
  body.hidden = !open;
  sum.hidden = open;
  head.onclick = () => {
    body.hidden = !body.hidden;
    head.setAttribute("aria-expanded", String(!body.hidden));
    caret.textContent = body.hidden ? "▸" : "▾";
    sum.hidden = !body.hidden;
  };
  host.append(head, body);
  return body;
}

// Shared modal lifecycle. Keep keyboard focus inside, make the background
// inert, and restore the invoking control without changing its scroll position.
// Call the returned cleanup before hiding/removing the dialog.
function containDialogFocus(root, initial) {
  const previous = document.activeElement;
  const siblings = [...document.body.children].filter(n => n !== root && !n.contains(root));
  const inertBefore = siblings.map(n => n.inert);
  siblings.forEach(n => { n.inert = true; });
  const trap = (ev) => {
    if (ev.key !== "Tab") return;
    const targets = [...root.querySelectorAll('a[href], button, input, textarea, select, [tabindex]')]
      .filter(n => !n.disabled && n.tabIndex >= 0 && n.getClientRects().length && !n.closest('[inert]'));
    const first = targets[0], last = targets[targets.length - 1];
    if (!first) { ev.preventDefault(); return; }
    if (!root.contains(document.activeElement) || (ev.shiftKey ? document.activeElement === first : document.activeElement === last)) {
      ev.preventDefault(); (ev.shiftKey ? last : first).focus();
    }
  };
  root.addEventListener("keydown", trap);
  initial.focus();
  return () => {
    root.removeEventListener("keydown", trap);
    siblings.forEach((n, i) => { n.inert = inertBefore[i]; });
    if (previous && previous.isConnected && previous.getClientRects().length) previous.focus({ preventScroll: true });
  };
}

// ---- money display (§11 family #6) ----
// fmtMoney is the one place display precision is decided: whole dollars,
// thousands-separated. moneyInput is the one numeric money field (.est-in,
// step 1). The old click-to-edit money shell had zero call sites and was
// deleted 2026-08-31; see ARCHITECTURE.md §11.
function fmtMoney(n) { return "$" + Math.round(n || 0).toLocaleString(); }
function fmtPct(x) { return Math.round((x || 0) * 100) + "%"; }

function moneyInput(placeholder, initial) {
  const i = inputEl(placeholder);
  i.type = "number"; i.step = "1"; i.classList.add("est-in");
  if (initial > 0) i.value = initial;
  return i;
}

// ---- ppCols: a one-line row of mono micro-labels sharing the exact grid of
// the rows beneath it — labels live once, every input aligns under them.
// (Promoted from 80-properties-core.js — already a cross-tab primitive.) ----
function ppCols(cls, labels) {
  const row = el("div", "pp-cols " + cls);
  labels.forEach((l) => row.append(el("span", "", l)));
  return row;
}

// ---- makeDirtyBar: the one editing model — quiet inputs mark dirty; a sticky
// bottom bar appears with a single save (one PUT of the whole file).
// (Promoted from 80-properties-core.js.) ----
function makeDirtyBar(host, onSave, onDiscard) {
  const bar = el("div", "dirty-bar");
  bar.hidden = true;
  const label = el("span", "dirty-label", "");
  const save = el("button", "pill", "save");
  const discard = el("button", "pill light", "discard");
  bar.append(label, save, discard);
  host.append(bar);
  let count = 0;
  const api = {
    mark() { count++; label.textContent = count + " UNSAVED CHANGE" + (count === 1 ? "" : "S"); bar.hidden = false; },
    clear() { count = 0; bar.hidden = true; },
    get dirty() { return count > 0; },
  };
  save.onclick = async () => { save.disabled = true; try { await onSave(); api.clear(); } finally { save.disabled = false; } };
  discard.onclick = () => { api.clear(); onDiscard(); };
  return api;
}

// ---- statusDot: the quiet dot as a library function — muted when off,
// accent when on (the AION publish rail's per-section dirty dots). ----
function statusDot(on, title) {
  const d = el("span", "status-dot" + (on ? " on" : ""));
  if (title) d.title = title;
  return d;
}

// ---- diffView: a compact unified diff block from server-rendered diff text
// (lines prefixed "+ " / "- "; everything else context). The first diff
// surface in the app — new idiom, so it lives in the library. ----
function diffView(unifiedText) {
  const wrap = el("div", "appr-diff");
  (unifiedText || "").split("\n").forEach((line) => {
    let kind = "ctx", text = line;
    if (line.startsWith("+ ")) { kind = "add"; text = line.slice(2); }
    else if (line.startsWith("- ")) { kind = "del"; text = line.slice(2); }
    const row = el("div", "diff-line diff-" + kind);
    row.append(el("span", "diff-gutter", kind === "add" ? "+" : kind === "del" ? "−" : " "));
    row.append(el("span", "diff-text", text === "" ? " " : text));
    wrap.append(row);
  });
  return wrap;
}

// ---- THE typeahead engine ----
// typeahead(opts) is the one `ta-wrap` inline-dropdown implementation,
// parameterized by SOURCE: the suggest callback receives (q, add) and appends
// rows; everything else — the wrap/input/drop shell, stale-fetch guarding,
// focus/input refresh, the 150ms blur-hide — lives here once.
//
//   opts.placeholder  input placeholder
//   opts.initial      initial input value
//   opts.suggest      async (q, add, ta) — q is lowercased+trimmed; call
//                     add(label, kind, pick) per row: kind "" renders a plain
//                     row, "create" the quiet create-completion, anything else
//                     a right-aligned ta-kind tag. pick() runs on selection
//                     (mousedown); use ta.commit(v) inside it to set + close.
//   opts.minChars     suggest only fires at >= this many chars (default 0)
//   opts.onEnter      Enter key with a value (free-text commit)
//   opts.onEscape     Escape key
//   opts.onChange     input change event (committed free text)
//   opts.onBlurGone   after blur, INSTEAD of just hiding the drop
//                     (personInput cancels the whole affordance)
//
// Returns { el, input, value(), setValue(), focus(), commit(v) }.
function typeahead(opts) {
  const wrap = el("span", "ta-wrap");
  const input = inputEl(opts.placeholder || "");
  input.classList.add("ta-in");
  if (opts.initial) input.value = opts.initial;
  const drop = el("div", "ta-drop");
  drop.hidden = true;
  let seq = 0;
  const ta = {
    el: wrap,
    input,
    value: () => input.value.trim(),
    setValue: (v) => { input.value = v; },
    focus: () => input.focus(),
    commit: (v) => { input.value = v; drop.hidden = true; },
  };
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    if (opts.minChars && q.length < opts.minChars) { drop.hidden = true; return; }
    const mySeq = ++seq;
    const items = [];
    const add = (label, kind, pick) => items.push({ label, kind: kind || "", pick });
    await opts.suggest(q, add, ta);
    if (mySeq !== seq) return; // a newer keystroke superseded this fetch
    drop.innerHTML = "";
    items.forEach(({ label, kind, pick }) => {
      let it;
      if (kind === "create") it = el("div", "ta-item ta-create", label);
      else if (kind) { it = el("div", "ta-item"); it.append(el("span", "", label), el("span", "ta-kind", kind)); }
      else it = el("div", "ta-item", label);
      it.onmousedown = (e) => { e.preventDefault(); pick(); };
      drop.append(it);
    });
    drop.hidden = !drop.children.length;
  };
  input.addEventListener("input", refresh);
  if (!opts.minChars) input.addEventListener("focus", refresh);
  if (opts.onEnter || opts.onEscape) {
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && opts.onEnter && input.value.trim()) opts.onEnter(input.value.trim());
      else if (ev.key === "Escape" && opts.onEscape) opts.onEscape();
    });
  }
  if (opts.onChange) input.addEventListener("change", () => opts.onChange(input.value.trim()));
  if (opts.onBlurGone) {
    input.addEventListener("blur", () => setTimeout(() => { if (wrap.parentNode) opts.onBlurGone(); }, 200));
  } else {
    input.addEventListener("blur", () => setTimeout(() => { drop.hidden = true; }, 150));
  }
  wrap.append(input, drop);
  return ta;
}

// flattenRockLadder — a goals area's rock list flattened to the set of live
// tether targets: every rock PLUS its child stages, as
// {id, text, label, checked} where label carries parent context
// ("Mechanism discovery › ICR go/no-go"). This is what keeps a rock pickable
// and resolvable after it's consolidated into a stage under a parent — callers
// walk the live ladder rather than a flat top-level snapshot, so no future rock
// reshuffle can silently drop a tether target. Checked (done) rocks/stages are
// INCLUDED (with checked:true) so a tether onto a done-but-still-live rock
// still resolves and isn't mis-flagged historic; a picker filters !checked.
function flattenRockLadder(rocks) {
  const out = [];
  const walk = (list, parent) => (list || []).forEach((r) => {
    out.push({ id: r.id, text: r.text, label: parent ? parent + " › " + r.text : r.text, checked: !!r.checked });
    walk(r.children, r.text);
  });
  walk(rocks, "");
  return out;
}

// contactNoteIndex — name → vault note path, from the contacts layer (people
// notes in the conscious repo). Shared by the aion + RE people registries so
// a registry row can link out to the person's actual note. Read-only; cached
// per page load.
let _contactNoteIdx = null;
async function contactNoteIndex() {
  if (_contactNoteIdx) return _contactNoteIdx;
  _contactNoteIdx = {};
  try {
    const d = await (await fetch("/api/contacts")).json();
    (d.contacts || []).forEach((c) => {
      if (!c.hasNote || !c.notePath) return;
      if (c.display) _contactNoteIdx[c.display.toLowerCase()] = c.notePath;
      if (c.key) _contactNoteIdx[c.key.toLowerCase()] = c.notePath;
    });
  } catch (e) {}
  return _contactNoteIdx;
}

// ---- derivedDirtyBar: the dirty bar whose state is COMPUTED, never counted
// (SPIRITS.md §3 — a hand-incremented counter detaches from the record the
// moment you navigate). compute() returns {dirty, blocked, msg}; call
// refresh() after every input. save runs onSave only when dirty && !blocked.
function derivedDirtyBar(host, opts) {
  const bar = el("div", "dirty-bar derived");
  const label = el("span", "dirty-label", "");
  const save = el("button", "pill", "save");
  const discard = el("button", "pill light", "discard");
  bar.append(label, save, discard);
  host.append(bar);
  const api = {
    refresh() {
      const { dirty, blocked, msg } = opts.compute();
      bar.hidden = false; // always visible: it carries the "no changes" truth too
      bar.classList.toggle("quiet", !dirty && !blocked);
      label.textContent = msg;
      save.disabled = !dirty || !!blocked;
      discard.disabled = !dirty;
      return dirty;
    },
  };
  save.onclick = async () => { save.disabled = true; try { await opts.onSave(); } finally { api.refresh(); } };
  discard.onclick = () => { opts.onDiscard(); api.refresh(); };
  api.refresh();
  return api;
}

// fuzzyScore ranks hay against needle for the ⌘K palette: 4 full prefix,
// 3 word prefix, 2 substring, 1 in-order subsequence, -1 no match. Both
// arguments are expected lowercased; ties break by recency upstream.
function fuzzyScore(needle, hay) {
  if (!needle) return 0;
  if (hay.startsWith(needle)) return 4;
  if (hay.split(/[\s·—\-\/]+/).some((w) => w.startsWith(needle))) return 3;
  if (hay.includes(needle)) return 2;
  let i = 0;
  for (const ch of hay) {
    if (ch === needle[i]) { i++; if (i === needle.length) return 1; }
  }
  return -1;
}

// Searchable file/folder picker: shared keyboard and focus lifecycle. Entries
// are data, never HTML. The caller owns the action after a selection.
function choosePath({title, placeholder, items, createLabel}) {
  return new Promise(resolve => {
    const root = el('div', 'cmdbar');
    const back = el('div', 'cmdbar-backdrop');
    const card = el('div', 'cmdbar-card'); card.setAttribute('role', 'dialog');
    card.setAttribute('aria-modal', 'true'); card.setAttribute('aria-label', title);
    const heading = el('div', 'path-picker-heading');
    const close = pill('close', () => finish(null));
    heading.append(el('span', 'micro-label', title), close);
    const input = inputEl(placeholder || 'type to search…'); input.className = 'cmdbar-input';
    input.setAttribute('aria-label', title); input.setAttribute('role', 'combobox');
    input.setAttribute('aria-expanded', 'true'); input.setAttribute('aria-autocomplete', 'list');
    const list = el('div', 'path-picker-list'); list.id = 'path-picker-' + Math.random().toString(36).slice(2);
    list.setAttribute('role', 'listbox'); input.setAttribute('aria-controls', list.id);
    const hint = el('div', 'path-picker-hint', '↑↓ to navigate · enter to choose · esc to dismiss');
    card.append(heading, input, list, hint); root.append(back, card); document.body.append(root);
    let filtered = [], selected = 0, settled = false;
    const release = containDialogFocus(root, input);
    function finish(value) { if(settled)return;settled=true;release();root.remove();resolve(value); }
    function paint() {
      const q = input.value.trim().toLowerCase();
      filtered = items.filter(i => (i.label + ' ' + (i.detail || '')).toLowerCase().includes(q)).slice(0, 100);
      if(createLabel && input.value.trim()) filtered.unshift({label:createLabel + ' “' + input.value.trim() + '”', value:input.value.trim(), create:true});
      selected = Math.max(0, Math.min(selected, filtered.length - 1)); list.replaceChildren();
      filtered.forEach((item, i) => {
        const row = el('div', 'path-picker-row' + (i===selected?' on':'')); row.id=list.id+'-'+i;
        row.setAttribute('role','option');row.setAttribute('aria-selected',String(i===selected));
        row.append(el('span','',item.label));if(item.detail)row.append(el('span','path-picker-detail',item.detail));
        row.onmousedown=e=>e.preventDefault();row.onclick=()=>finish(item);
        list.append(row);
      });
      if(!filtered.length)list.append(el('div','path-picker-hint','no matching files or folders'));
      input.setAttribute('aria-activedescendant',filtered.length?list.id+'-'+selected:'');
      list.children[selected]?.scrollIntoView({block:'nearest'});
    }
    input.oninput=()=>{selected=0;paint()};
    card.onkeydown=e=>{
      if(e.key==='Escape'){e.preventDefault();e.stopPropagation();finish(null)}
      else if(e.target===input&&['ArrowDown','ArrowUp','Enter'].includes(e.key)){
        e.preventDefault();e.stopPropagation();
        if(e.key==='Enter'){if(filtered[selected])finish(filtered[selected]);return}
        selected=(selected+(e.key==='ArrowDown'?1:-1)+filtered.length)%Math.max(1,filtered.length);paint();
      }
    };
    back.onclick=()=>finish(null);paint();
  });
}

function chooseActionMenu(trigger, items) {
  return new Promise(resolve=>{
    const root=el('div','action-menu-layer'),back=el('div','action-menu-backdrop'),menu=el('div','action-menu');
    menu.setAttribute('role','menu');menu.setAttribute('aria-label','Document actions');
    const rect=trigger.getBoundingClientRect();menu.style.right=Math.max(8,window.innerWidth-rect.right)+'px';menu.style.top=Math.min(rect.bottom+6,window.innerHeight-260)+'px';
    let release,closed=false;
    const close=value=>{if(closed)return;closed=true;release?.();root.remove();resolve(value)};
    items.forEach(item=>{const button=el('button','action-menu-item',item.label);button.setAttribute('role','menuitem');button.onclick=()=>close(item);menu.append(button)});
    root.append(back,menu);document.body.append(root);release=containDialogFocus(root,menu.firstElementChild);
    back.onclick=()=>close(null);menu.onkeydown=e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();close(null)}else if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();const buttons=[...menu.children];const i=buttons.indexOf(document.activeElement);buttons[(i+(e.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length].focus()}};
  });
}

// Read-only original attachment bytes. No artifact identity/version is invented.
function attachmentWorkspace(mount,file,href,onClose){
  const pane=el("aside","artifact-workspace");pane.setAttribute("aria-label","Attachment preview");
  const head=el("div","artifact-workspace-head"),title=el("strong","artifact-workspace-title",file.name);
  const back=el("button","sprt-quiet","Back to chat"),controls=el("div","artifact-workspace-controls");
  const download=el("a","sprt-quiet",file.openLabel||"Open / download original ↗");download.href=href;download.target="_blank";download.rel="noopener";
  const notice=el("div","artifact-workspace-notice",file.notice||"Original attachment · read-only"),body=el("div","artifact-workspace-body","Loading…");body.tabIndex=0;notice.setAttribute("role","status");
  let blobURL=null,closed=false;const abort=new AbortController();
  back.onclick=()=>{closed=true;abort.abort();if(blobURL)URL.revokeObjectURL(blobURL);pane.remove();onClose?.();};
  head.append(title,back);controls.append(download);pane.append(head,controls,notice,body);mount.append(pane);
  (async()=>{
    try{
      const response=await fetch(href,{signal:abort.signal});if(!response.ok){
        if(file.errorLabel){const reader=response.body?.getReader();const chunk=reader?await reader.read():null;await reader?.cancel();const reason=chunk?.value?new TextDecoder().decode(chunk.value.slice(0,500)).trim():"";throw new Error(reason||file.errorLabel);}
        throw new Error("Attachment unavailable ("+response.status+")");
      }
      const mime=(response.headers.get("Content-Type")||"").split(";")[0].trim().toLowerCase();
      const text=/\.(md|txt|csv|tsv|json|yaml|yml|log|go|js|ts|py|css)$/i.test(file.name)||mime.startsWith("text/");
      const media=["application/pdf","image/png","image/jpeg","image/gif","image/webp"].includes(mime);
      if(!text&&!media){body.textContent="Preview is unavailable for this file type. Open or download the original above.";abort.abort();return;}
      const limit=text?2*1024*1024:20*1024*1024;
      if(Number(response.headers.get("Content-Length"))>limit)throw new Error("This file is too large to preview. Open or download the original above.");
      const reader=response.body.getReader(),chunks=[];let size=0;
      while(true){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>limit){await reader.cancel();throw new Error("This file is too large to preview. Open or download the original above.");}chunks.push(value);}
      if(closed)return;
      const blob=new Blob(chunks,{type:mime});
      if(text){const content=await blob.text();if(closed)return;const pre=el("pre","",content);body.replaceChildren(pre);}
      else {blobURL=URL.createObjectURL(blob);const view=document.createElement(mime==="application/pdf"?"iframe":"img");view.src=blobURL;view.title=file.name;view.alt=file.name;body.replaceChildren(view);}
    }catch(e){abort.abort();if(!closed)body.textContent=e.message;}
  })();
  return {element:pane,close:()=>back.click(),isEditing:()=>false};
}

// Exact line comparison; bound the quadratic middle section for large files.
// Large replacements remain exact, but are not claimed to be a minimal diff.
function artifactLineChanges(before,after){
  const lines=text=>text===""?[]:text.split("\n");
  const a=lines(before),b=lines(after),out=[];
  let first=0,endA=a.length,endB=b.length;
  while(first<endA&&first<endB&&a[first]===b[first]){out.push({kind:"same",text:a[first]});first++;}
  while(endA>first&&endB>first&&a[endA-1]===b[endB-1]){endA--;endB--;}
  const n=endA-first,m=endB-first;
  if(n&&m&&n*m<=250000){
    const grid=Array.from({length:n+1},()=>new Uint32Array(m+1));
    for(let i=n-1;i>=0;i--)for(let j=m-1;j>=0;j--)grid[i][j]=a[first+i]===b[first+j]?grid[i+1][j+1]+1:Math.max(grid[i+1][j],grid[i][j+1]);
    let i=0,j=0;
    while(i<n||j<m){
      if(i<n&&j<m&&a[first+i]===b[first+j]){out.push({kind:"same",text:a[first+i]});i++;j++;}
      else if(i<n&&(j===m||grid[i+1][j]>=grid[i][j+1]))out.push({kind:"removed",text:a[first+i++]});
      else out.push({kind:"added",text:b[first+j++]});
    }
  }else{
    for(let i=first;i<endA;i++)out.push({kind:"removed",text:a[i]});
    for(let j=first;j<endB;j++)out.push({kind:"added",text:b[j]});
  }
  for(let i=endA;i<a.length;i++)out.push({kind:"same",text:a[i]});
  return out;
}

function artifactDiffLine(kind,text,before,after,prefix='artifact-diff-'){
 const row=el('span',prefix+kind+' numbered-diff-row');
 for(const [side,value] of [['before',before],['after',after]]){const number=el('span','diff-line-number',value==null?'':String(value));number.setAttribute('aria-hidden','true');row.append(number);if(value!=null)row.dataset[side+'Line']=value;}
 row.append(el('code','diff-line-text',text));return row;
}

function artifactDiffView(before,after,label){
  const changes=artifactLineChanges(before,after);
  const added=changes.filter(x=>x.kind==="added").length,removed=changes.filter(x=>x.kind==="removed").length;
  const view=el("div","artifact-diff-review");
  view.append(el("p","artifact-diff-summary",added||removed?`${added} lines added · ${removed} lines removed`:"No text changes."));
  const diff=el("pre","artifact-diff");diff.setAttribute("aria-label",label);
  let beforeLine=1,afterLine=1;
  for(const line of changes){const old=line.kind!=="added"?beforeLine++:null,next=line.kind!=="removed"?afterLine++:null;diff.append(artifactDiffLine(line.kind,(line.kind==="added"?"+ ":line.kind==="removed"?"− ":"  ")+line.text,old,next));}
  view.append(diff);return view;
}

// Review decisions are immutable, version-bound owner records. They do not
// approve tools, close tasks, or send the drafted revision request.
function artifactReviewControls(artifact,revision,number,onDiscuss){
 const host=el('details','artifact-review-controls'),summary=el('summary','','Review · v'+number),state=el('p','','loading…'),form=el('div','artifact-review-form');host.append(summary,state,form);
 const choice=document.createElement('select');choice.setAttribute('aria-label','Review decision');
 for(const [value,label] of [['accepted','accept this version'],['changes_requested','request changes'],['comment','comment'],['ready_for_review','mark ready for review']]){const option=el('option','',label);option.value=value;choice.append(option);}
 const note=document.createElement('textarea');note.rows=3;note.placeholder='Review notes';note.setAttribute('aria-label','Review notes');
 const range=el('details',''),start=document.createElement('input'),end=document.createElement('input');start.type=end.type='number';start.min=end.min='1';start.placeholder='First line';end.placeholder='Last line';start.setAttribute('aria-label','First reviewed line');end.setAttribute('aria-label','Last reviewed line');range.append(el('summary','','Specific lines'),start,end);range.hidden=/\.(diff|pdf|png|jpe?g|gif|webp)$/i.test(artifact.ref||'');
 const actions=el('div','form-actions'),save=el('button','','record decision');actions.append(save);form.append(choice,note,range,actions,el('p','artifact-review-help',onDiscuss?'Records your review. A change request is placed in the composer for you to send.':'Records your review of this version. No message is sent.'));
 const history=el('div','artifact-review-history');host.append(history);let snapshot=null,recorded=false;
 const endpoint='/api/artifacts/reviews?id='+encodeURIComponent(artifact.id)+'&revision='+encodeURIComponent(revision),storage='manifest.artifactReview.v1.'+artifact.id+'.'+revision;
 try{const draft=JSON.parse(localStorage.getItem(storage)||'null');if(draft){choice.value=draft.state;note.value=draft.note||'';start.value=draft.start||'';end.value=draft.end||'';}}catch(e){}
 const clearPending=()=>{try{localStorage.removeItem(storage);}catch(e){}};
 const label=value=>({not_requested:'Not reviewed',ready_for_review:'Ready for review',accepted:'Accepted',changes_requested:'Changes requested',comment:'Comment'})[value]||value;
 const show=value=>{if(!value||value.revision!==revision||typeof value.record_version!=='string'||!Array.isArray(value.entries))throw Error('Invalid review response; reload before continuing.');snapshot=value;state.textContent=label(value.state)+' · version '+number;summary.textContent='Review · '+label(value.state);history.replaceChildren();
  for(const item of value.entries.filter(e=>e.revision===revision).slice().reverse()){const row=el('div','artifact-review-entry');row.append(el('small','',label(item.state)+' · '+new Date(item.at).toLocaleString()+(item.start?' · lines '+item.start+'–'+item.end:'')));if(item.note)row.append(el('p','',item.note));history.append(row);}
 };
 const load=async()=>{save.disabled=true;try{const r=await fetch(endpoint,{cache:'no-store'});if(!r.ok)throw Error('Reviews could not be loaded.');show(await r.json());}catch(e){state.textContent=e.message;}finally{save.disabled=!snapshot;}};
 const changed=()=>{recorded=false;save.disabled=!snapshot;save.textContent=choice.value==='changes_requested'&&onDiscuss?'record and draft request':'record decision';};choice.onchange=note.oninput=start.oninput=end.oninput=changed;
 save.onclick=async()=>{if(!snapshot||recorded)return;if(['changes_requested','comment'].includes(choice.value)&&!note.value.trim()){note.focus();return;}
  const content={state:choice.value,note:note.value,start:Number(start.value)||0,end:Number(end.value||start.value)||0};let request={...content,request_id:crypto.randomUUID(),record_version:snapshot.record_version};
  try{const previous=JSON.parse(localStorage.getItem(storage)||'null');if(previous&&['state','note','start','end'].every(k=>previous[k]===content[k]))request=previous;localStorage.setItem(storage,JSON.stringify(request));}catch(e){}
  save.disabled=true;try{const r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});
   if(r.status===409){show(await r.json());clearPending();throw Error('Review changed elsewhere. Check the history, then record your decision again.');}
   if(!r.ok)throw Error(await r.text());const result=await r.json();if(!result.entries?.some(e=>e.id===request.request_id&&e.revision===revision&&e.state===content.state&&(e.note||'')===content.note&&(e.start||0)===content.start&&(e.end||0)===content.end))throw Error('Review acknowledgement is incomplete. Retry uses the same decision ID.');show(result);clearPending();recorded=true;save.textContent='recorded';window.dispatchEvent(new CustomEvent('artifact-review-recorded',{detail:{id:artifact.id,revision}}));
   if(host.isConnected&&content.state==='changes_requested'&&onDiscuss)onDiscuss({id:artifact.id,revision,title:artifact.title||'Artifact',version:number,reviewNote:content.note,reviewStart:content.start,reviewEnd:content.end});
  }catch(e){state.textContent=e.message||'Review not confirmed. Retry uses the same decision ID.';}finally{save.disabled=!snapshot||recorded;}
 };
 load();return host;
}

// A shared, version-aware workspace. The caller owns placement and discussion
// context; opening it never navigates, edits a file, or starts an agent.
const artifactEditDrafts = new Map();
window.addEventListener("pagehide",()=>{for(const state of artifactEditDrafts.values())if(state.dirty)state.flush();});
function artifactWorkspace(mount, options) {
  const opts = options || {};
  const pane = el("aside", "artifact-workspace");
  pane.setAttribute("aria-label", "Artifact workspace");
  const header = el("div", "artifact-workspace-head");
  const title = el("strong", "artifact-workspace-title", "Loading…");
  const close = el("button", "sprt-quiet", "Back to chat");
  close.onclick = () => { if(editState?.dirty)editState.flush(); pane.remove(); if (opts.onClose) opts.onClose(); };
  header.append(title, close);
  const controls = el("div", "artifact-workspace-controls");
  const body = el("div", "artifact-workspace-body");
  body.tabIndex = 0;
  const notice = el("div", "artifact-workspace-notice");
  notice.setAttribute("role", "status");
  pane.append(header, controls, notice, body);
  mount.append(pane);
  let current, selected, selectedNumber, generation = 0, editing = false, editState, editor;
  const recovery = el("div", "artifact-edit-recovery");
  pane.insertBefore(recovery,body);
  async function prepareEditState(id){
    if(!opts.save || typeof ChatDraftState==="undefined")return;
    if(!artifactEditDrafts.has(id))artifactEditDrafts.set(id,new ChatDraftState("artifact-"+id,null,"edit"));
    editState=artifactEditDrafts.get(id);
    editState.changed=(state,apply)=>{
      if(!pane.isConnected)return;
      if(apply&&editing&&editor){
        if(state.value){editor.value=state.value.text||"";}
        else {render();return;}
      }
      pane.dataset.draft=String(!!state.value);
      chatRenderStateNotice(recovery,state);
    };
    await editState.refresh();
  }
  const url = (a, hash) => "/api/artifacts/get?id="+encodeURIComponent(a.id)+"&content=1&rev="+encodeURIComponent(hash);
  const fetchJSON = async (path) => { const r = await fetch(path); if (!r.ok) throw new Error(await r.text()); return r.json(); };
  async function show(a, hash, number) {
    const ticket = ++generation;
    try {
      const mediaRef = /\.(pdf|png|jpe?g|gif|webp)$/i.test(a.ref || "");
      const d = await fetchJSON(mediaRef ? "/api/artifacts/get?id="+encodeURIComponent(a.id) : url(a, hash || a.head));
      if (ticket !== generation || !pane.isConnected) return;
      current = d; selected = hash || d.head; selectedNumber = number || [...d.revisions].reverse().find(r=>r.hash===selected)?.n; editing = false;
      await prepareEditState(current.id);
      if(ticket!==generation || !pane.isConnected)return;
      render();
    } catch (e) { notice.textContent = "Could not open this version: " + e.message; }
  }
  function render() {
    generation++;
    editing = false;
    pane.dataset.draft=String(!!editState?.value);
    title.textContent = current.title || current.ref || "Artifact";
    controls.replaceChildren(); body.replaceChildren(); notice.textContent = "";
    const versions = document.createElement("select");
    versions.className = "pp-in";
    versions.setAttribute("aria-label", "Artifact version");
    [...current.revisions].reverse().forEach(r => {
      const o = document.createElement("option"); o.value = String(r.n);
      o.textContent = "Version " + r.n + (r.n === current.revisions.length ? " · latest" : "") + (r.note ? " · " + r.note : "");
      versions.append(o);
    });
    versions.value = String(selectedNumber); versions.onchange = () => { const r=current.revisions.find(r=>String(r.n)===versions.value); show(current,r.hash,r.n); };
    controls.append(versions);
    const ext = (current.ref || "").split(".").pop().toLowerCase();
    const contentURL = "/api/artifacts/content?id="+encodeURIComponent(current.id)+"&rev="+encodeURIComponent(selected);
    const binary = ["pdf", "png", "jpg", "jpeg", "gif", "webp"].includes(ext);
    if (binary) {
      const media = document.createElement(ext === "pdf" ? "iframe" : "img");
      media.src = contentURL; media.title = title.textContent; media.alt = title.textContent;
      body.append(media);
      notice.textContent = "Preview only. This file has not been sent to the agent.";
    } else if(ext==="diff"){
      body.append(artifactWorkingChangesView(current.content||""));
    } else if(ext && !['md','markdown','mdown'].includes(ext)) {
      const source=el('pre','artifact-source-preview');source.append(el('code','',current.content||''));body.append(source);
    } else {
      try { body.append(renderMarkdown(current.content || "", "", {readOnly:true})); }
      catch(e) { body.textContent = current.content || ""; }
    }
    const download = el("a", "sprt-quiet", "Open file ↗");
    download.href = contentURL; download.target = "_blank"; download.rel = "noopener";
    controls.append(download);
    if(opts.review)body.prepend(artifactReviewControls(current,selected,selectedNumber,binary?null:opts.onDiscuss));
    const previous=current.revisions.find(r=>r.n===selectedNumber-1);
    if(previous&&!binary){
      const compare=el("button","sprt-quiet","Compare v"+previous.n);
      compare.onclick=async()=>{
        const ticket=++generation,versionText=current.content||"";
        compare.disabled=true;
        try{
          const old=await fetchJSON(url(current,previous.hash));
          if(ticket!==generation||!pane.isConnected)return;
          body.replaceChildren(artifactDiffView(old.content||"",versionText,`Changes from version ${previous.n} to version ${selectedNumber}`));
          compare.textContent="Back to preview";compare.onclick=render;
        }catch(e){if(ticket===generation)notice.textContent="Could not compare versions: "+e.message;}
        finally{compare.disabled=false;}
      };
      controls.append(compare);
    }
    if (opts.onDiscuss && !binary) {
      const discuss = el("button", "sprt-quiet", "Discuss");
      discuss.onclick = () => {
        opts.onDiscuss({id:current.id, revision:selected, title:current.title || "Artifact", version:selectedNumber});
      };
      controls.append(discuss);
    }
    if (opts.save && !binary && (!opts.canEdit || opts.canEdit(current))) {
      const edit = el("button", "sprt-quiet artifact-primary-action", selected === current.head ? "Edit" : "Restore this version");
      edit.onclick = () => editVersion(selected !== current.head);
      if(editState?.value){
        edit.textContent="Resume draft";
        edit.onclick=()=>editVersion(!!editState.value.restore,true);
        notice.textContent="An unfinished edit is saved. The saved version is unchanged.";
      }
      controls.append(edit);
    }
  }
  function editVersion(restore,resume=false,proposal=null) {
    generation++;
    editing = true;
    pane.dataset.draft='true';
    const original = current.content || "";
    const started=resume&&editState?.value ? editState.value : {text:proposal?proposal.content:original,artifact:current.id,baseRevision:proposal?proposal.baseRevision:current.head,sourceRevision:selected,restore};
    if(editState&&!resume)editState.set(started);
    const input = document.createElement("textarea");
    input.className = "artifact-workspace-editor"; input.value = started.text; editor=input;
    input.setAttribute("aria-label", "File content");
    input.spellcheck=false;
    body.replaceChildren(input);
    controls.replaceChildren();
    const save = el("button", "sprt-quiet artifact-primary-action", restore ? "Save restored version" : "Save new version");
    const remember=()=>editState?.set({...editState.value||started,text:input.value});
    input.addEventListener("input",remember);
    const review=el("button","sprt-quiet","Review changes");
    let reviewing=false;
    review.onclick=async()=>{
      if(reviewing){body.replaceChildren(input);review.textContent="Review changes";reviewing=false;input.focus();return;}
      remember();const ticket=generation;review.disabled=true;
      try{
        const base=await fetchJSON(url(current,started.baseRevision));
        if(ticket!==generation||!pane.isConnected||!editing)return;
        body.replaceChildren(artifactDiffView(base.content||"",input.value,"Unsaved changes from starting revision"));
        reviewing=true;review.textContent="Edit text";
      }catch(e){if(ticket===generation&&pane.isConnected)notice.textContent="Could not compare the starting revision: "+e.message;}
      finally{review.disabled=false;}
    };
    const cancel = el("button", "sprt-quiet", "Back to preview"); cancel.onclick = ()=>{remember();render();};
    const discard=el("button","sprt-quiet","Discard draft");
    discard.onclick=async()=>{
      if(editState?.conflict){notice.textContent="Resolve the draft conflict before discarding.";return;}
      if(editState){editState.set(null);await editState.flush();}render();
    };
    save.onclick = async () => {
      remember();
      if(editState?.conflict){notice.textContent="Resolve the draft conflict before saving a version.";return;}
      const submitted=editState?.value||{...started,text:input.value};
      if(submitted.artifact!==current.id || !/^[0-9a-f]{64}$/.test(submitted.baseRevision||"")){notice.textContent="This draft has no valid starting revision. Keep its text and review the latest version before saving.";return;}
      save.disabled = true;input.disabled=true;discard.disabled=true;review.disabled=true;
      try {
        await opts.save(submitted.text, submitted.baseRevision);
        if(editState&&chatStateEqual(editState.value,submitted)){editState.set(null);await editState.flush();}
        const a = await opts.load();
        await show(a,a.head);
        notice.textContent = opts.saveNotice || "New version saved. Execution has not started.";
      } catch(e) { notice.textContent = e.message; }
      finally { save.disabled = false;input.disabled=false;discard.disabled=false;review.disabled=false; }
    };
    controls.append(save,review,cancel,discard); chatRenderStateNotice(recovery,editState);input.focus();
  }
  async function refresh(revision,proposal) { try {
    if(editing){notice.textContent="Your edit is preserved. Return to preview before opening another version.";return;}
    const a = await opts.load(); await show(a,revision || a.head);
    if(proposal&&current&&pane.isConnected){
      const p=proposal;
      if(p.artifactId!==current.id||p.baseRevision!==selected||typeof p.content!=="string")notice.textContent="Proposed revision does not match this artifact version.";
      else if(editState?.value||editState?.conflict||editState?.error)notice.textContent="Finish or discard your existing edit before reviewing this proposal. Your draft is preserved.";
      else{editVersion(false,false,p);notice.textContent="Proposed revision · review before saving. Execution will not start.";}
    }
  } catch(e) { notice.textContent=e.message; title.textContent="Artifact unavailable"; } }
  const ready=refresh(opts.revision,opts.proposal);
  return {element:pane, close:()=>close.click(), isEditing:()=>editing, refresh,
    getView:()=>({revision:selected,scrollTop:body.scrollTop}),
    restoreView:async view=>{await ready;if(!pane.isConnected||!body.clientHeight)return false;if(Number.isFinite(view.scrollTop))body.scrollTop=Math.max(0,view.scrollTop);return true;}
  };
}

// A compact, keyboard-accessible review surface for explicit user actions.
function reviewDialog(title,build){
  const dialog=document.createElement("dialog");dialog.className="review-dialog";
  const heading=el("h2","",title);dialog.setAttribute("aria-label",title);
  const body=el("div","review-dialog-body"),actions=el("div","review-dialog-actions");
  const close=()=>{dialog.close();dialog.remove();};
  dialog.append(heading,body,actions);document.body.append(dialog);
  dialog.addEventListener("cancel",()=>dialog.remove());
  build({body,actions,close});dialog.showModal();
  return dialog;
}


// Presentation only: immutable snapshot bytes remain available through Open file.
function artifactWorkingChangesView(text){
 const view=el("div","working-changes-view");
 const marker="\nUntracked files (contents not included):\n",cut=text.indexOf(marker);
 const tracked=cut<0?text:text.slice(0,cut),untracked=cut<0?[]:text.slice(cut+marker.length).trim().split("\n");
 const starts=[...tracked.matchAll(/^diff --git /gm)].map(m=>m.index);
 const files=starts.map((start,i)=>{const content=tracked.slice(start,starts[i+1]??tracked.length);const path=content.match(/^\+\+\+ b\/(.+)$/m)?.[1]||content.match(/^--- a\/(.+)$/m)?.[1]||content.split("\n")[0].replace("diff --git ","");return {path,content};});
 view.append(el("p","working-changes-summary",files.length?files.length+" changed "+(files.length===1?"file":"files"):"No tracked changes"));
 if(files.length){
  const pick=document.createElement("select");pick.className="pp-in";pick.setAttribute("aria-label","Changed file");
  files.forEach((f,i)=>{const option=el("option","",f.path);option.value=i;pick.append(option);});
  const content=el("pre","working-file-diff");
  const show=()=>{content.replaceChildren();let before=null,after=null;for(const line of files[Number(pick.value)].content.split("\n")){
      const hunk=line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if(hunk){before=Number(hunk[1]);after=Number(hunk[2]);content.append(artifactDiffLine('context',line,null,null,'working-diff-'));continue;}
      const cls=line.startsWith("+")&&(before!==null||!line.startsWith("+++"))?"added":line.startsWith("-")&&(before!==null||!line.startsWith("---"))?"removed":"context";
      const inHunk=before!==null&&after!==null&&(cls!=='context'||line.startsWith(' ')),old=inHunk&&cls!=='added'?before++:null,next=inHunk&&cls!=='removed'?after++:null;
      content.append(artifactDiffLine(cls,line,old,next,'working-diff-'));
    }};
  pick.onchange=show;show();view.append(pick,content);
 }
 if(untracked.length){const details=el("details","working-untracked");details.append(el("summary","",untracked.length+" untracked files · contents not included"));const list=el("ul","");for(const raw of untracked){let name=raw;try{name=JSON.parse(raw);}catch(e){}const row=el("li","working-untracked-file");row.title=name;row.append(el("span","",name.split("/").at(-1)));const folder=name.includes("/")?name.slice(0,name.lastIndexOf("/")):"";if(folder)row.append(el("small","",folder));list.append(row);}details.append(list);view.append(details);}
 const meta=el("details","working-snapshot-meta");meta.append(el("summary","","Snapshot details"),el("pre","",tracked.slice(0,starts[0]??tracked.length).replace("No tracked changes against HEAD.","").trim()));view.append(meta);
 return view;
}
