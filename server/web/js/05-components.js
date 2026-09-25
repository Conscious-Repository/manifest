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

// Measure a writing field without collapsing the focused textarea. Collapsing
// it makes mobile browsers repeatedly reveal the caret and pan the viewport.
const textareaMeasureCache = new WeakMap();
function textareaContentHeight(ta) {
  const cs = getComputedStyle(ta), width = ta.getBoundingClientRect().width;
  if (!width) return 0;
  const properties = ['fontFamily','fontSize','fontWeight','fontStyle','lineHeight',
    'letterSpacing','wordSpacing','textIndent','textTransform','tabSize',
    'paddingTop','paddingBottom','paddingLeft','paddingRight',
    'borderTopWidth','borderBottomWidth','borderLeftWidth','borderRightWidth',
    'boxSizing','whiteSpace','overflowWrap','wordBreak'];
  const signature = [width, ta.value, ...properties.map(p => cs[p])].join('\u0000');
  const prior = textareaMeasureCache.get(ta);
  if (prior?.signature === signature) return prior.height;
  const mirror = document.createElement('textarea');
  mirror.tabIndex = -1; mirror.setAttribute('aria-hidden', 'true');
  for (const p of properties) mirror.style[p] = cs[p];
  Object.assign(mirror.style, {position:'fixed', top:'0', left:'-10000px',
    visibility:'hidden', pointerEvents:'none', width:width+'px', height:'0',
    minHeight:'0', maxHeight:'none', overflow:'hidden', resize:'none'});
  mirror.value = ta.value || ' '; mirror.rows = 1;
  document.body.append(mirror);
  const height = mirror.scrollHeight + (cs.boxSizing === 'border-box'
    ? (parseFloat(cs.borderTopWidth)||0)+(parseFloat(cs.borderBottomWidth)||0)
    : -(parseFloat(cs.paddingTop)||0)-(parseFloat(cs.paddingBottom)||0));
  mirror.remove(); textareaMeasureCache.set(ta, {signature, height});
  return height;
}

// Keep unchanged rows mounted: focus, selection and open disclosures belong
// to the user. Signatures describe render inputs, never live DOM state.
const keyedChildrenCache = new WeakMap();
function reconcileKeyedChildren(host, items, keyOf, signatureOf, render) {
  const previous = keyedChildrenCache.get(host) || new Map(), next = new Map();
  let cursor = host.firstChild;
  items.forEach((item, index) => {
    const key = keyOf(item, index), signature = signatureOf(item, index);
    const prior = previous.get(key);
    const node = prior?.signature === signature ? prior.node : render(item, index);
    if (node !== cursor) host.insertBefore(node, cursor);
    else cursor = cursor.nextSibling;
    next.set(key, {signature, node});
  });
  while (cursor) { const following = cursor.nextSibling; cursor.remove(); cursor = following; }
  keyedChildrenCache.set(host, next);
}

// Filter state and counts change in place; refreshing a list must not replace
// the control that currently owns keyboard or touch interaction.
function renderFilterButtons(host, choices, selected, onChange) {
  reconcileKeyedChildren(host,choices,choice=>choice[0],()=>'',()=>el('button','filter-chip'));
  [...host.children].forEach((button,i)=>{
    const [value,label]=choices[i];
    if(button.textContent!==label)button.textContent=label;
    button.classList.toggle('on',value===selected);
    button.setAttribute('aria-pressed',String(value===selected));
    button.onclick=()=>onChange(value);
  });
}

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
//   opts.keyboard    opt into combobox ArrowUp/Down, Enter and Escape navigation
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
  let active = -1, choices = [];
  const setActive = n => {
    active = n;
    [...drop.children].forEach((row,i)=>row.setAttribute('aria-selected',String(i===n)));
    if(n>=0){input.setAttribute('aria-activedescendant',drop.children[n].id);drop.children[n].scrollIntoView({block:'nearest'});}
    else input.removeAttribute('aria-activedescendant');
  };
  const hide = () => {drop.hidden=true;if(opts.keyboard){input.setAttribute('aria-expanded','false');setActive(-1);}};
  if(opts.keyboard){
    drop.id='ta-'+crypto.randomUUID();drop.setAttribute('role','listbox');
    input.setAttribute('role','combobox');input.setAttribute('aria-autocomplete','list');input.setAttribute('aria-controls',drop.id);input.setAttribute('aria-expanded','false');
    input.addEventListener('keydown',e=>{
      if(!drop.hidden&&(e.key==='ArrowDown'||e.key==='ArrowUp')){e.preventDefault();setActive(active<0?(e.key==='ArrowDown'?0:choices.length-1):(active+(e.key==='ArrowDown'?1:-1)+choices.length)%choices.length);}
      else if(!drop.hidden&&e.key==='Enter'&&active>=0){e.preventDefault();e.stopImmediatePropagation();choices[active].pick();hide();}
      else if(e.key==='Escape'&&!drop.hidden){e.preventDefault();e.stopPropagation();++seq;hide();}
    });
  }
  const ta = {
    el: wrap,
    input,
    value: () => input.value.trim(),
    setValue: (v) => { input.value = v; ++seq; hide(); },
    focus: () => input.focus(),
    commit: (v) => { input.value = v; ++seq; hide(); },
  };
  const refresh = async () => {
    const q = input.value.toLowerCase().trim();
    if (opts.minChars && q.length < opts.minChars) { ++seq; hide(); return; }
    const mySeq = ++seq;
    const items = [];
    const add = (label, kind, pick) => items.push({ label, kind: kind || "", pick });
    await opts.suggest(q, add, ta);
    if (mySeq !== seq) return; // a newer keystroke superseded this fetch
    drop.innerHTML = "";
    choices=items;active=-1;
    items.forEach(({ label, kind, pick },i) => {
      let it;
      if (kind === "create") it = el("div", "ta-item ta-create", label);
      else if (kind) { it = el("div", "ta-item"); it.append(el("span", "", label), el("span", "ta-kind", kind)); }
      else it = el("div", "ta-item", label);
      if(opts.keyboard){it.id=drop.id+'-'+i;it.setAttribute('role','option');it.setAttribute('aria-selected','false');}
      it.onmousedown = (e) => { e.preventDefault(); pick(); };
      drop.append(it);
    });
    drop.hidden = !drop.children.length;
    if(opts.keyboard){input.setAttribute('aria-expanded',String(!drop.hidden));input.removeAttribute('aria-activedescendant');}
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
    input.addEventListener("blur", () => setTimeout(() => { ++seq; hide(); }, 150));
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

function chooseActionMenu(trigger, items, label = "Document actions") {
  return new Promise(resolve=>{
    const root=el('div','action-menu-layer'),back=el('div','action-menu-backdrop'),menu=el('div','action-menu');
    menu.setAttribute('role','menu');menu.setAttribute('aria-label',label);
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
  if(file.resolveHref){download.removeAttribute("href");}
  const notice=el("div","artifact-workspace-notice",file.notice||"Original attachment · read-only"),body=el("div","artifact-workspace-body","Loading…");body.tabIndex=0;notice.setAttribute("role","status");
  let blobURL=null,closed=false;const abort=new AbortController();
  back.onclick=()=>{closed=true;abort.abort();if(blobURL)URL.revokeObjectURL(blobURL);pane.remove();onClose?.();};
  head.append(title,back);controls.append(download);pane.append(head,controls,notice,body);mount.append(pane);
  const ready=(async()=>{
    try{
      if(file.resolveHref){href=await file.resolveHref(abort.signal);if(closed)return;download.href=href;}
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
      if(text){const content=await blob.text();if(closed)return;if(file.markdown&&/\.md$/i.test(file.name))body.replaceChildren(renderMarkdown(content,"",{readOnly:true}));else body.replaceChildren(el("pre","",content));}
      else {blobURL=URL.createObjectURL(blob);const view=document.createElement(mime==="application/pdf"?"iframe":"img");view.src=blobURL;view.title=file.name;view.alt=file.name;body.replaceChildren(view);}
    }catch(e){abort.abort();if(!closed)body.textContent=e.message;}
  })();
  return {element:pane,close:()=>back.click(),isEditing:()=>false,getView:()=>({scrollTop:body.scrollTop}),restoreView:async view=>{await ready;if(closed||!body.clientHeight)return false;body.scrollTop=Math.max(0,Number(view.scrollTop)||0);return true;}};
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
 const range=el('details',''),start=document.createElement('input'),end=document.createElement('input');start.type=end.type='number';start.min=end.min='1';start.placeholder='First line';end.placeholder='Last line';start.setAttribute('aria-label','First reviewed line');end.setAttribute('aria-label','Last reviewed line');range.append(el('summary','','Specific lines'),start,end);range.hidden=artifact.preview?artifact.preview.kind!=='text'||/\.diff$/i.test(artifact.ref||''):/\.(diff|pdf|png|jpe?g|gif|webp)$/i.test(artifact.ref||'');
 const actions=el('div','form-actions'),save=el('button','','record decision');actions.append(save);form.append(choice,note,range,actions,el('p','artifact-review-help',onDiscuss?'Records your review. A change request is placed in the composer for you to send.':'Records your review of this version. No message is sent.'));
 const history=el('div','artifact-review-history');host.append(history);let snapshot=null,recorded=false,busy=false,pending=null;
 const endpoint='/api/artifacts/reviews?id='+encodeURIComponent(artifact.id)+'&revision='+encodeURIComponent(revision),storage='manifest.artifactReview.v1.'+artifact.id+'.'+revision;
 try{pending=JSON.parse(localStorage.getItem(storage)||'null');const draft=pending||JSON.parse(localStorage.getItem(storage+'.draft')||'null');if(draft){choice.value=draft.state;note.value=draft.note||'';start.value=draft.start||'';end.value=draft.end||'';}}catch(e){}
 if(artifact.preview&&artifact.preview.kind!=='text'){start.value='';end.value='';}
 const clearPending=()=>{try{localStorage.removeItem(storage);localStorage.removeItem(storage+'.draft');}catch(e){}};
 const label=value=>({not_requested:'Not reviewed',ready_for_review:'Ready for review',accepted:'Accepted',changes_requested:'Changes requested',comment:'Comment'})[value]||value;
 const show=value=>{if(!value||value.revision!==revision||typeof value.record_version!=='string'||!Array.isArray(value.entries))throw Error('Invalid review response; reload before continuing.');snapshot=value;state.textContent=label(value.state)+' · version '+number;summary.textContent='Review · '+label(value.state);history.replaceChildren();
  for(const item of value.entries.filter(e=>e.revision===revision).slice().reverse()){const row=el('div','artifact-review-entry');row.append(el('small','',label(item.state)+' · '+new Date(item.at).toLocaleString()+(item.start?' · '+(/\.diff$/i.test(artifact.ref||'')?'snapshot lines ':'lines ')+item.start+'–'+item.end:'')));if(item.note)row.append(el('p','',item.note));history.append(row);}
 };
 const content=()=>({state:choice.value,note:note.value,start:Number(start.value)||0,end:Number(end.value||start.value)||0});
 const receipt=(value,request)=>value.entries?.find(e=>e.id===request.request_id&&e.revision===revision&&e.state===request.state&&(e.note||'')===request.note&&(e.start||0)===request.start&&(e.end||0)===request.end);
 const lock=()=>{for(const field of [choice,note,start,end])field.disabled=busy||!!pending;save.disabled=busy||!snapshot||recorded;};
 const draftRequest=request=>onDiscuss?.({id:artifact.id,revision,title:artifact.title||'Artifact',version:number,reviewNote:request.note,reviewStart:request.start,reviewEnd:request.end,reviewLineKind:/\.diff$/i.test(artifact.ref||'')?'snapshot':'file'});
 const recoveredDraft=el('button','','draft recorded request');recoveredDraft.hidden=true;actions.append(recoveredDraft);
 const confirmed=(request,recovered)=>{
  pending=null;recorded=true;save.textContent='recorded';
  if(recovered&&request.state==='changes_requested'&&onDiscuss){recoveredDraft.hidden=false;recoveredDraft.onclick=()=>{draftRequest(request);clearPending();recoveredDraft.hidden=true;};}
  else clearPending();
  lock();
 };
 const load=async()=>{
  busy=true;lock();
  try{
   const r=await fetch(endpoint,{cache:'no-store'});if(!r.ok)throw Error('Reviews could not be loaded.');const value=await r.json();show(value);
   if(pending){if(receipt(value,pending))confirmed(pending,true);else{state.textContent='Previous decision is not confirmed. Retry records the same decision ID.';save.textContent='retry decision';}}
   else save.textContent=choice.value==='changes_requested'&&onDiscuss?'record and draft request':'record decision';
  }catch(e){state.textContent=e.message;}finally{busy=false;lock();}
 };
 const changed=()=>{if(busy||pending)return;if(recorded)clearPending();try{localStorage.setItem(storage+'.draft',JSON.stringify(content()));}catch(e){}recorded=false;recoveredDraft.hidden=true;save.textContent=choice.value==='changes_requested'&&onDiscuss?'record and draft request':'record decision';lock();};choice.onchange=note.oninput=start.oninput=end.oninput=changed;
 save.onclick=async()=>{
  if(busy||!snapshot||recorded)return;
  if(!pending&&['changes_requested','comment'].includes(choice.value)&&!note.value.trim()){note.focus();return;}
  const request=pending||{...content(),request_id:crypto.randomUUID(),record_version:snapshot.record_version};
  try{localStorage.setItem(storage,JSON.stringify(request));}catch(e){state.textContent='Could not preserve the decision for recovery. Nothing was submitted. Allow browser storage and retry.';return;}
  pending=request;busy=true;lock();
  try{
   const r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});
   if(r.status===409){show(await r.json());pending=null;try{localStorage.removeItem(storage);localStorage.setItem(storage+'.draft',JSON.stringify(content()));}catch(e){}throw Error('Review changed elsewhere. Check the history, then record your decision again.');}
   if(!r.ok){const message=await r.text();if([400,404,428].includes(r.status)){pending=null;try{localStorage.removeItem(storage);localStorage.setItem(storage+'.draft',JSON.stringify(content()));}catch(e){}}throw Error(message);}
   const result=await r.json();if(!receipt(result,request))throw Error('Review acknowledgement is incomplete. Retry uses the same decision ID.');show(result);confirmed(request,!host.isConnected);
   window.dispatchEvent(new CustomEvent('artifact-review-recorded',{detail:{id:artifact.id,revision}}));
   if(host.isConnected&&request.state==='changes_requested'&&onDiscuss)draftRequest(request);
  }catch(e){state.textContent=e.message||'Review not confirmed. Retry uses the same decision ID.';save.textContent=pending?'retry decision':choice.value==='changes_requested'&&onDiscuss?'record and draft request':'record decision';}
  finally{busy=false;lock();}
 };
 host.prepareChange=context=>{if(busy||pending){host.open=true;state.textContent='Resolve the pending decision before starting another review.';return;}choice.value='changes_requested';start.value=context.start;end.value=context.end;note.value=(note.value.trim()?note.value+'\n\n':'')+'File: '+context.path+'\n'+(context.record?'Record: '+context.record+'\nSource lines: '+context.start+'–'+context.end+'\n':'')+(context.hunk?'Hunk: '+context.hunk+'\nSnapshot lines: '+context.start+'–'+context.end+'\n':'');host.open=true;changed();note.focus();note.setSelectionRange(note.value.length,note.value.length);};
 load();return host;
}

// A shared, version-aware workspace. The caller owns placement and discussion
// context; opening it never navigates, edits a file, or starts an agent.
const artifactEditDrafts = new Map();
window.addEventListener("pagehide",()=>{for(const state of artifactEditDrafts.values())if(state.dirty)state.flush();});
// Artifact origin is not the authorship of every later revision.
function artifactProvenanceView(artifact,hash,number) {
 const view=el('details','artifact-provenance');view.append(el('summary','','Origin and version details'));
 const revision=(artifact.revisions||[]).find(r=>r.hash===hash&&r.n===number),origin=artifact.provenance||{};
 const facts=el('dl','');
 const add=(label,value)=>{if(value)facts.append(el('dt','',label),el('dd','',String(value)));};
 add('Version',number);add('Revision',hash);add('Version recorded by',revision?.actor||'Not recorded');add('Version recorded at',revision?.at||'Not recorded');
 add('Artifact source',origin.source||'Not recorded');add('Recorded conversation',origin.session);add('Recorded run',origin.run);add('Recorded task',origin.task);
 view.append(facts);
 const links=(artifact.sources||[]).filter(link=>typeof link.route==='string'&&link.route.startsWith('#/'));
 for(const link of links){const row=el('p',''),a=el('a','',link.kind==='conversation'?'Open source conversation':link.kind==='execution'?'Open producing execution':link.kind==='run'?'Open producing run':'Open source '+link.kind);a.href=link.route;a.title=link.label||link.id||'';row.append(a);view.append(row);}
 if((origin.session||origin.run)&&!links.length)view.append(el('p','','Source unavailable; recorded identity retained.'));
 return view;
}

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
  if(opts.contextNotice)pane.insertBefore(el("p","artifact-workspace-notice",opts.contextNotice),body);
  mount.append(pane);
  let current, selected, selectedNumber, generation = 0, editing = false, editState, editor, previewMode="preview", openComparison=null, openDraftReview=null;
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
  const url = (a, hash) => "/api/artifacts/get?id="+encodeURIComponent(a.id)+"&preview=1&sources=1&rev="+encodeURIComponent(hash);
  const fetchJSON = async (path) => { const r = await fetch(path); if (!r.ok) throw new Error(await r.text()); return r.json(); };
  async function show(a, hash, number) {
    const ticket = ++generation;
    try {
      const d = await fetchJSON(url(a, hash || a.head));
      if (ticket !== generation || !pane.isConnected) return;
      current = d; selected = hash || d.head; selectedNumber = number || [...d.revisions].reverse().find(r=>r.hash===selected)?.n; editing = false;
      await prepareEditState(current.id);
      if(ticket!==generation || !pane.isConnected)return;
      render();
    } catch (e) { notice.textContent = "Could not open this version: " + e.message; }
  }
  function render() {
    generation++;
    previewMode="preview";openComparison=null;openDraftReview=null;
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
    const binary = current.preview ? current.preview.kind !== 'text' : ["pdf", "png", "jpg", "jpeg", "gif", "webp"].includes(ext);
    const reviewControls=opts.review?artifactReviewControls(current,selected,selectedNumber,binary?null:opts.onDiscuss):null;
    if (current.preview?.kind === 'metadata') {
      body.append(artifactMetadataPreview(current,selectedNumber));
    } else if (binary) {
      const media = document.createElement((current.preview?.kind||ext) === "pdf" ? "iframe" : "img");
      media.src = contentURL; media.title = title.textContent; media.alt = title.textContent;
      if(media.tagName==='IMG')media.onerror=()=>{if(!media.isConnected)return;media.replaceWith(artifactMetadataPreview({...current,preview:{...current.preview,revision:selected,size:current.preview?.size||0,mediaType:current.preview?.mediaType||ext,reason:'The image could not be decoded. Open the original file to inspect it.'}},selectedNumber));};
      body.append(media);
      notice.textContent = "Preview only. This file has not been sent to the agent.";
    } else if(ext==="diff"){
      body.append(artifactWorkingChangesView(current.content||"",reviewControls?context=>reviewControls.prepareChange(context):null));
    } else if(current.kind==='link'||ext==='url'){
      body.append(artifactLinkPreview(current.content||'',ext));
    } else if(ext==='csv'||ext==='tsv'){
      body.append(artifactTablePreview(current.content||'',ext,reviewControls?record=>reviewControls.prepareChange({path:current.ref,...record}):null));
    } else if(ext && !['md','markdown','mdown'].includes(ext)) {
      body.append(artifactCodePreview(current.content||'',ext));
    } else {
      try { body.append(renderMarkdown(current.content || "", "", {readOnly:true})); }
      catch(e) { body.textContent = current.content || ""; }
    }
    const download = el("a", "sprt-quiet", "Open file ↗");
    download.href = contentURL; download.target = "_blank"; download.rel = "noopener";
    controls.append(download);
    body.prepend(artifactProvenanceView(current,selected,selectedNumber));
    if(reviewControls)body.prepend(reviewControls);
    const previous=current.revisions.find(r=>r.n===selectedNumber-1);
    if(previous&&!binary){
      const compare=el("button","sprt-quiet","Compare v"+previous.n);
      openComparison=compare.onclick=async()=>{
        const ticket=++generation,versionText=current.content||"";
        compare.disabled=true;
        try{
          const old=await fetchJSON(url(current,previous.hash));
          if(ticket!==generation||!pane.isConnected)return;
          if(old.preview&&old.preview.kind!=='text')throw new Error('The previous version has no text preview. Open that version to inspect the original file.');
          body.replaceChildren(artifactDiffView(old.content||"",versionText,`Changes from version ${previous.n} to version ${selectedNumber}`));
          previewMode="compare";compare.textContent="Back to preview";compare.onclick=render;
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
    if(opts.onUseContext&&!binary){const use=el('button','sprt-quiet','use in this private chat');use.onclick=()=>opts.onUseContext({id:current.id,revision:selected,title:current.title||'Artifact',version:selectedNumber});controls.append(use);}
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
  function editVersion(restore,resume=false,proposal=null,focus=true) {
    generation++;
    editing = true;previewMode="edit";
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
    const remember=()=>{const prior=editState?.value||started;editState?.set({...prior,text:input.value,saveRequestID:prior.text===input.value?prior.saveRequestID:undefined});};
    input.addEventListener("input",remember);
    const review=el("button","sprt-quiet","Review changes");
    let reviewing=false;
    openDraftReview=review.onclick=async()=>{
      if(reviewing){body.replaceChildren(input);previewMode="edit";review.textContent="Review changes";reviewing=false;if(focus)input.focus();return;}
      remember();const ticket=generation;review.disabled=true;
      try{
        const base=await fetchJSON(url(current,started.baseRevision));
        if(ticket!==generation||!pane.isConnected||!editing)return;
        if(base.preview&&base.preview.kind!=='text')throw new Error('The starting version has no text preview. Your draft is preserved.');
        body.replaceChildren(artifactDiffView(base.content||"",input.value,"Unsaved changes from starting revision"));
        reviewing=true;previewMode="edit-review";review.textContent="Edit text";
      }catch(e){if(ticket===generation&&pane.isConnected)notice.textContent="Could not compare the starting revision: "+e.message;}
      finally{review.disabled=false;}
    };
    const cancel = el("button", "sprt-quiet", "Back to preview"); cancel.onclick = ()=>{remember();render();};
    const discard=el("button","sprt-quiet","Discard draft");
    discard.onclick=async()=>{
      if(editState?.conflict){notice.textContent="Resolve the draft conflict before discarding.";return;}
      if(editState){editState.set(null);await editState.flush();}render();
    };
    let saving=false;
    save.onclick = async () => {
      if(saving)return;
      remember();
      if(editState?.conflict){notice.textContent="Resolve the draft conflict before saving a version.";return;}
      let submitted=editState?.value||{...started,text:input.value};
      if(opts.receiptSave){
        if(!editState){notice.textContent="Draft recovery is unavailable. Keep this edit open and retry after reloading the application.";return;}
        if(!submitted.saveRequestID){submitted={...submitted,saveRequestID:crypto.randomUUID()};editState.set(submitted);}
      }
      if(submitted.artifact!==current.id || !/^[0-9a-f]{64}$/.test(submitted.baseRevision||"")){notice.textContent="This draft has no valid starting revision. Keep its text and review the latest version before saving.";return;}
      saving=true;save.disabled = true;input.disabled=true;discard.disabled=true;review.disabled=true;cancel.disabled=true;
      try {
        if(editState){
          const synced=await editState.flush();
          if(!synced||editState.conflict||editState.error||editState.dirty||!chatStateEqual(editState.value,submitted)){
            notice.textContent="The edit has not been confirmed in draft storage. Resolve draft recovery before saving a file version.";return;
          }
        }
        if(!pane.isConnected)return;
        const result=await opts.save(submitted.text, submitted.baseRevision,submitted.saveRequestID);
        if(opts.receiptSave&&(!result||result.id!==submitted.artifact||result.saveRequestID!==submitted.saveRequestID||!result.revisions?.some(r=>r.hash===result.savedRevision&&r.n===result.savedVersion)))throw new Error("Save receipt is incomplete. Retry retains the original request identity.");
        if(editState&&chatStateEqual(editState.value,submitted)){editState.set(null);await editState.flush();}
        const a = await opts.load();
        await show(a,opts.receiptSave?result.savedRevision:a.head,opts.receiptSave?result.savedVersion:undefined);
        notice.textContent = opts.receiptSave&&a.head!==result.savedRevision ? "Saved version recovered. A newer version exists; the current file was not overwritten." : opts.saveNotice || "New version saved. Execution has not started.";
      } catch(e) { notice.textContent = e.message; }
      finally { saving=false;save.disabled = false;input.disabled=false;discard.disabled=false;review.disabled=false;cancel.disabled=false; }
    };
    controls.append(save,review,cancel,discard); chatRenderStateNotice(recovery,editState);if(focus)input.focus();
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
    getView:()=>({revision:selected,mode:previewMode,scrollTop:body.scrollTop,syntaxEnabled:body.querySelector('.artifact-code-preview,.working-changes-view')?.getHighlight?.(),linkSourceOpen:!!body.querySelector('.artifact-link-source')?.open,table:{scrollLeft:body.querySelector('.artifact-table-scroll')?.scrollLeft||0,sourceOpen:!!body.querySelector('.artifact-table-source')?.open},editor:editing&&editor?{scrollTop:editor.scrollTop,start:editor.selectionStart,end:editor.selectionEnd,direction:editor.selectionDirection}:null,expandedFiles:[...body.querySelectorAll('.working-file[open]')].map(f=>f.dataset.diffFile),collapsedHunks:[...body.querySelectorAll('.working-changes-view')].flatMap(v=>v.getCollapsedHunks?.()||[])}),
    restoreView:async view=>{await ready;if(!pane.isConnected||!body.clientHeight)return false;if((view.mode==="edit"||view.mode==="edit-review")&&opts.save&&editState?.value&&(!opts.canEdit||opts.canEdit(current))){
      editVersion(!!editState.value.restore,true,null,false);
      if(view.editor){editor.setSelectionRange(Math.max(0,Number(view.editor.start)||0),Math.max(0,Number(view.editor.end)||0),view.editor.direction||"none");editor.scrollTop=Math.max(0,Number(view.editor.scrollTop)||0);}
      if(view.mode==="edit-review"&&openDraftReview)await openDraftReview();
    }else if(view.mode==="compare"&&previewMode!=="compare"&&openComparison)await openComparison();if(!pane.isConnected||!body.clientHeight)return false;if(Array.isArray(view.expandedFiles)){body.querySelectorAll('.working-file').forEach(f=>f.open=view.expandedFiles.includes(f.dataset.diffFile));await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));}if(Array.isArray(view.collapsedHunks)&&(!view.revision||view.revision===selected)){body.querySelectorAll('.working-changes-view').forEach(v=>v.restoreHunks?.(view.collapsedHunks));}if(view.revision===selected&&typeof view.syntaxEnabled==='boolean')body.querySelector('.artifact-code-preview,.working-changes-view')?.setHighlight?.(view.syntaxEnabled);if(view.revision===selected&&typeof view.linkSourceOpen==='boolean'){const source=body.querySelector('.artifact-link-source');if(source)source.open=view.linkSourceOpen;}if(view.table&&view.revision===selected){const table=body.querySelector('.artifact-table-scroll'),source=body.querySelector('.artifact-table-source');if(table)table.scrollLeft=Math.max(0,Number(view.table.scrollLeft)||0);if(source)source.open=!!view.table.sourceOpen;}if(Number.isFinite(view.scrollTop))body.scrollTop=Math.max(0,view.scrollTop);return true;}
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
function artifactWorkingChangesView(text,onReview=null){
 const view=el("div","working-changes-view"),parsed=artifactWorkingDiffFiles(text),{files,untracked}=parsed;
 let collapsed=new Set(),syntaxEnabled=true;
 view.getHighlight=()=>syntaxEnabled;view.setHighlight=value=>{syntaxEnabled=!!value;view.querySelectorAll('.working-file-diff').forEach(node=>node.setHighlight?.(syntaxEnabled));view.querySelector('.working-syntax-toggle')?.setAttribute('aria-pressed',String(syntaxEnabled));};
 view.getCollapsedHunks=()=>{view.querySelectorAll('.working-hunk').forEach(h=>{if(h.open)collapsed.delete(h.dataset.diffHunk);else collapsed.add(h.dataset.diffHunk);});return [...collapsed];};
 view.restoreHunks=keys=>{collapsed=new Set(keys);view.querySelectorAll('.working-hunk').forEach(h=>h.open=!collapsed.has(h.dataset.diffHunk));};
 view.append(el("p","working-changes-summary",files.length?files.length+" changed "+(files.length===1?"file":"files"):!text.trim()||text.includes("No tracked changes against HEAD.")?"No tracked changes":"No supported file diff headers; inspect Snapshot details."));
 if(files.length){
  const tools=el('div','working-file-actions');
  const expand=el('button','sprt-quiet','expand all'),collapse=el('button','sprt-quiet','collapse all');
  tools.append(expand,collapse);view.append(tools);
  if(files.some(file=>artifactSyntaxLanguage(file.path.split('.').pop().toLowerCase())&&file.hunks.some(h=>h.valid))){const toggle=el('button','sprt-quiet working-syntax-toggle','syntax highlighting');toggle.setAttribute('aria-pressed','true');toggle.onclick=()=>view.setHighlight(!syntaxEnabled);tools.append(toggle);}
  for(const [index,file] of files.entries()){
   const section=el('details','working-file'),heading=el('summary',''),name=el('span','working-file-name',file.path);
   section.dataset.diffFile=file.path;name.title=file.path;
   const state=/^new file mode /m.test(file.content)?'added':/^deleted file mode /m.test(file.content)?'deleted':/^rename (from|to) /m.test(file.content)?'renamed':'modified';
   const binary=/^(Binary files |GIT binary patch)/m.test(file.content);
   const added=file.hunks.reduce((n,h)=>n+h.lines.filter(l=>l.startsWith('+')).length,0),removed=file.hunks.reduce((n,h)=>n+h.lines.filter(l=>l.startsWith('-')).length,0);
   heading.append(name,el('span','working-file-stats',state+(binary?' · binary':file.combined?' · combined diff':' · +'+added+' −'+removed)));section.append(heading);view.append(section);
   let rendered=false;
   const render=()=>{
    if(rendered||!section.open)return;rendered=true;
    if(onReview){const review=el('button','sprt-quiet working-file-review','request changes');review.setAttribute('aria-label','Request changes to '+file.path);review.onclick=()=>onReview({path:file.path,start:file.start,end:file.end});section.append(review);}
    const raw=el('pre','working-file-diff');raw.tabIndex=0;raw.setAttribute('aria-label','Diff headers for '+file.path);
    for(const line of file.headers)raw.append(artifactDiffLine('context',line,null,null,'working-diff-'));section.append(raw);
    if(binary||file.combined)section.append(el('p','artifact-review-help',binary?'Binary snapshot; line-by-line review is unavailable.':'Combined diff shown as recorded; hunk review is unavailable.'));
    for(const [i,hunk] of file.hunks.entries()){
     const part=el('details','working-hunk'),summary=el('summary','working-hunk-title',hunk.header);
     part.dataset.diffHunk=JSON.stringify([file.path,file.start,hunk.start]);summary.setAttribute('aria-label','Hunk '+(i+1)+' in '+file.path);part.append(summary);part.open=!collapsed.has(part.dataset.diffHunk);
     if(onReview&&hunk.valid){const review=el('button','sprt-quiet working-file-review','request changes to hunk');review.setAttribute('aria-label','Request changes to hunk '+(i+1)+' in '+file.path);review.onclick=()=>onReview({path:file.path,start:hunk.start,end:hunk.end,hunk:hunk.header});part.append(review);}
     if(!hunk.valid)part.append(el('p','artifact-review-help','Incomplete or unsupported hunk; review the recorded snapshot as a file.'));
     const content=el('pre','working-file-diff');content.tabIndex=0;content.setAttribute('aria-label','Hunk '+(i+1)+' diff for '+file.path);
     let before=hunk.before,after=hunk.after;
     for(const [offset,line] of hunk.lines.entries()){
      const cls=line.startsWith('+')?'added':line.startsWith('-')?'removed':'context';
      const numbered=hunk.valid&&(cls!=='context'||line.startsWith(' ')),old=numbered&&cls!=='added'?before++:null,next=numbered&&cls!=='removed'?after++:null;
      const row=artifactDiffLine(cls,line,old,next,'working-diff-');row.dataset.snapshotLine=String(hunk.start+1+offset);content.append(row);
     }
     part.append(content);section.append(part);
     if(hunk.valid)artifactHunkSyntax(content,hunk.lines,file.path,syntaxEnabled);
    }
   };
   section.addEventListener('toggle',render);section.open=index===0;render();
  }
  expand.onclick=()=>{collapsed.clear();view.querySelectorAll('.working-hunk').forEach(h=>h.open=true);view.querySelectorAll('.working-file').forEach(f=>f.open=true);};
  collapse.onclick=()=>view.querySelectorAll('.working-file').forEach(f=>f.open=false);
 }
 if(untracked.length){const details=el("details","working-untracked");details.append(el("summary","",untracked.length+" untracked files · contents not included"));const list=el("ul","");for(const raw of untracked){let name=raw;try{name=JSON.parse(raw);}catch(e){}const row=el("li","working-untracked-file");row.title=name;row.append(el("span","",name.split("/").at(-1)));const folder=name.includes("/")?name.slice(0,name.lastIndexOf("/")):"";if(folder)row.append(el("small","",folder));list.append(row);}details.append(list);view.append(details);}
 const meta=el("details","working-snapshot-meta");meta.append(el("summary","","Snapshot details"),el("pre","",parsed.metadata.replace("No tracked changes against HEAD.","").trim()));view.append(meta);
 return view;
}

// Hunk ranges are one-based lines in the immutable diff snapshot, never a
// mixture of before/after file coordinates. Keep those coordinates for display.
function artifactWorkingDiffFiles(text){
 const marker="\nUntracked files (contents not included):\n",cut=text.indexOf(marker);
 const tracked=cut<0?text:text.slice(0,cut),untracked=cut<0?[]:text.slice(cut+marker.length).trim().split("\n").filter(Boolean);
 const starts=[...tracked.matchAll(/^diff --(?:git |cc |combined )/gm)].map(m=>m.index);
 const files=starts.map((offset,i)=>{
  const content=tracked.slice(offset,starts[i+1]??tracked.length),start=tracked.slice(0,offset).split('\n').length,lines=content.split('\n');
  while(lines.at(-1)==='')lines.pop();
  const headerPath=content.match(/^\+\+\+ (.+)$/m)?.[1],oldPath=content.match(/^--- (.+)$/m)?.[1];
  let path=headerPath&&headerPath!=='/dev/null'?headerPath:oldPath||lines[0].replace(/^diff --(?:git |cc |combined )/,'');
  try{if(path.startsWith('"'))path=JSON.parse(path);}catch(e){}path=path.replace(/^[ab]\//,'');
  const combined=/^diff --(?:cc|combined) /.test(content),hunks=[];let headers=lines;
  if(!combined){
   const boundaries=lines.map((line,index)=>({line,index,match:line.match(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/)})).filter(h=>h.match);
   if(boundaries.length)headers=lines.slice(0,boundaries[0].index);
   for(const [j,h] of boundaries.entries()){
    const end=boundaries[j+1]?.index??lines.length,body=lines.slice(h.index+1,end),oldCount=Number(h.match[2]??1),newCount=Number(h.match[4]??1);
    const old=body.filter(l=>l.startsWith(' ')||l.startsWith('-')).length,next=body.filter(l=>l.startsWith(' ')||l.startsWith('+')).length;
    const valid=[h.match[1],h.match[3],oldCount,newCount].every(n=>Number.isSafeInteger(Number(n)))&&old===oldCount&&next===newCount&&body.every(l=>/^[ +\-]/.test(l)||l==='\\ No newline at end of file');
    hunks.push({header:h.line,start:start+h.index,end:start+end-1,before:Number(h.match[1]),after:Number(h.match[3]),lines:body,valid});
   }
  }
  return {path,content,start,end:start+lines.length-1,headers,hunks,combined};
 });
 return {files,untracked,metadata:tracked.slice(0,starts[0]??tracked.length)};
}


// Parse delimited artifact bytes for presentation only. The source and review
// anchors always refer to the immutable original; never reserialize for saving.
function artifactDelimitedRows(text,delimiter){
 if(new TextEncoder().encode(text).length>1024*1024)throw Error('Table preview supports text up to 1 MiB.');
 const rows=[];let cells=[],field='',quoted=false,closed=false,line=1,start=1,touched=false,total=0;
 const cell=()=>{cells.push(field);field='';closed=false;if(cells.length>50)throw Error('Table preview supports up to 50 columns.');};
 const row=()=>{cell();total+=cells.length;if(rows.length>=200||total>10000)throw Error('Table preview supports up to 200 records and 10,000 cells.');rows.push({cells,start,end:line});cells=[];touched=false;};
 for(let i=text.charCodeAt(0)===0xfeff?1:0;i<text.length;i++){
  const ch=text[i];
  if(ch==='\0')throw Error('Table preview requires text without binary bytes.');
  if(quoted){
   if(ch==='"'){if(text[i+1]==='"'){field+='"';i++;}else{quoted=false;closed=true;}}
   else{field+=ch;if(ch==='\n')line++;}
  }else if(ch===delimiter){cell();touched=true;}
  else if(ch==='\n'||ch==='\r'){
   if(ch==='\r'){if(text[i+1]!=='\n')throw Error('Table preview requires LF or CRLF line endings.');i++;}
   row();line++;start=line;
  }else if(ch==='"'){
   if(field||closed)throw Error('Unexpected quote in record '+(rows.length+1)+'.');quoted=true;touched=true;
  }else{
   if(closed)throw Error('Unexpected text after a quoted field in record '+(rows.length+1)+'.');field+=ch;touched=true;
  }
  if(field.length>16000)throw Error('A field is too large for table preview.');
 }
 if(quoted)throw Error('Unclosed quoted field in record '+(rows.length+1)+'.');
 if(touched||cells.length||field||closed)row();
 return rows;
}
function artifactTablePreview(text,format,onReview=null){
 const view=el('section','artifact-table-preview'),status=el('p','artifact-review-help');status.setAttribute('role','status');view.append(status);
 const source=el('details','artifact-table-source'),raw=el('pre','artifact-source-preview');raw.append(el('code','',text));source.append(el('summary','','source text'),raw);
 try{
  const rows=artifactDelimitedRows(text,format==='tsv'?'\t':',');
  if(!rows.length){status.textContent='Empty table.';view.append(source);return view;}
  const columns=Math.max(...rows.map(r=>r.cells.length)),scroll=el('div','artifact-table-scroll'),table=el('table','artifact-data-table');
  scroll.tabIndex=0;scroll.setAttribute('role','region');scroll.setAttribute('aria-label',format.toUpperCase()+' table; scroll for more columns');
  table.append(el('caption','',format.toUpperCase()+' records · first record is shown as data'));
  const head=el('thead',''),headRow=el('tr','');
  for(const label of ['record',...Array.from({length:columns},(_,i)=>'column '+(i+1))]){const th=el('th','',label);th.scope='col';headRow.append(th);}head.append(headRow);table.append(head);
  const body=el('tbody','');
  rows.forEach((record,index)=>{
   const tr=el('tr',''),number=el('th','','');number.scope='row';number.title='Source lines '+record.start+'–'+record.end;
   if(onReview){const action=el('button','sprt-quiet',String(index+1));action.setAttribute('aria-label','Request changes to record '+(index+1));action.title=number.title;action.onclick=()=>onReview({record:index+1,start:record.start,end:record.end});number.append(action);}else number.textContent=String(index+1);
   tr.append(number);
   for(let column=0;column<columns;column++){const cell=el('td','',record.cells[column]??'');if(column>=record.cells.length){cell.classList.add('artifact-cell-missing');cell.setAttribute('aria-label','No field in this record');}tr.append(cell);}
   body.append(tr);
  });
  table.append(body);scroll.append(table);view.append(scroll);
  status.textContent=rows.length+' records · '+columns+' columns.'+(onReview?' Select a record number to request changes to its source lines.':'')+' Values are displayed as text; formulas are not evaluated.';
 }catch(error){status.textContent='Table preview unavailable: '+error.message+' The original source is shown below.';source.open=true;}
 view.append(source);return view;
}

function artifactMetadataPreview(artifact,number){
 const view=el('section','artifact-metadata-preview'),data=artifact.preview;
 view.append(el('h3','','File · version '+number),el('p','',data.reason));
 const list=el('dl','');
 for(const [label,value] of [['file',artifact.ref||artifact.title||'Unnamed file'],['type',data.mediaType],['size',data.size.toLocaleString()+' bytes'],['revision',data.revision]]){list.append(el('dt','',label),el('dd','',value));}
 view.append(list,el('p','artifact-review-help','Review applies to this exact version. Use Open file to inspect its contents.'));
 return view;
}

// Only explicit link artifacts and Internet Shortcut files use this surface.
// Inspection never fetches a destination or embeds remote content.
function artifactLinkTarget(text,format){
 if(new TextEncoder().encode(text).length>16384)throw Error('Link previews support source up to 16 KiB.');
 let target=text.replace(/^\uFEFF/,'').trim();
 if(format==='url'){
  let section='',values=[];
  for(const line of target.split(/\r?\n/)){
   const value=line.trim();
   if(!value||value.startsWith(';')||value.startsWith('#'))continue;
   if(/^\[.*\]$/.test(value)){section=value.toLowerCase();continue;}
   if(section==='[internetshortcut]'&&/^url\s*=/i.test(value))values.push(value.slice(value.indexOf('=')+1).trim());
  }
  if(values.length!==1)throw Error('Internet Shortcut files must contain one URL in [InternetShortcut].');
  target=values[0];
 }
 if(!/^https?:\/\//i.test(target)||/[\s\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069\\]/u.test(target))throw Error('A single HTTP or HTTPS URL is required.');
 let url;try{url=new URL(target);}catch(e){throw Error('The destination is not a valid URL.');}
 if(!url.hostname||url.username||url.password)throw Error('Links with embedded credentials are not previewed.');
 return {href:url.href,host:url.host};
}
function artifactLinkPreview(text,format){
 const view=el('section','artifact-link-preview'),source=el('details','artifact-link-source'),raw=el('pre','artifact-source-preview');
 raw.append(el('code','',text));source.append(el('summary','','source text'),raw);
 try{
  const target=artifactLinkTarget(text,format);
  view.append(el('h3','',target.host));
  const link=el('a','artifact-link-destination',target.href);link.href=target.href;link.target='_blank';link.rel='noopener noreferrer';link.referrerPolicy='no-referrer';link.dir='ltr';
  view.append(link,el('p','artifact-review-help','Opens in a new tab. The destination has not been fetched; this review covers the saved link, not the current webpage.'));
 }catch(error){view.append(el('p','artifact-review-help','Link preview unavailable: '+error.message+' The original source is shown below.'));source.open=true;}
 view.append(source);return view;
}

let artifactSyntaxLoading;
function artifactLoadSyntax(){
 if(typeof window.manifestArtifactSyntax==='function')return Promise.resolve(window.manifestArtifactSyntax);
 if(!artifactSyntaxLoading)artifactSyntaxLoading=new Promise((resolve,reject)=>{
  const script=document.createElement('script');script.src='/vendor/artifact-syntax.js';
  script.onload=()=>typeof window.manifestArtifactSyntax==='function'?resolve(window.manifestArtifactSyntax):reject(Error('Highlighter unavailable'));
  script.onerror=()=>{script.remove();reject(Error('Highlighter unavailable'));};document.head.append(script);
 }).catch(error=>{artifactSyntaxLoading=null;throw error;});
 return artifactSyntaxLoading;
}
function artifactSyntaxLanguage(extension){
 const languages={js:'javascript',mjs:'javascript',cjs:'javascript',jsx:'javascript',ts:'typescript',tsx:'typescript',py:'python',go:'go',json:'json',sh:'bash',bash:'bash',css:'css',html:'xml',htm:'xml',xml:'xml',svg:'xml',sql:'sql',yaml:'yaml',yml:'yaml'};
 return languages[extension];
}
function artifactSyntaxFragment(text,language,highlight){
 const template=document.createElement('template');template.innerHTML=highlight(text,language).replace(/\r/g,'&#13;');
 if(template.content.textContent!==text||[...template.content.querySelectorAll('*')].some(node=>node.tagName!=='SPAN'||[...node.attributes].some(a=>a.name!=='class')))throw Error('Highlighting changed source text');
 return template.content;
}
function artifactCodePreview(text,extension){
 const view=el('section','artifact-code-preview'),source=el('pre','artifact-source-preview'),code=el('code','',text),language=artifactSyntaxLanguage(extension);source.append(code);
 if(!language){view.append(source);return view;}
 const status=el('p','artifact-review-help',language+' · loading syntax highlighting…');status.setAttribute('role','status');
 view.append(status,source);
 if(new TextEncoder().encode(text).length>65536||text.split('\n').length>2000){status.textContent=language+' · plain text (syntax highlighting supports up to 64 KiB and 2,000 lines).';return view;}
 let enabled=true,highlighted=null;
 const toggle=el('button','sprt-quiet','syntax highlighting');toggle.setAttribute('aria-pressed','true');view.insertBefore(toggle,source);
 const render=()=>{toggle.setAttribute('aria-pressed',String(enabled));code.replaceChildren(enabled&&highlighted?highlighted.cloneNode(true):document.createTextNode(text));};
 view.getHighlight=()=>enabled;view.setHighlight=value=>{enabled=!!value;render();};toggle.onclick=()=>view.setHighlight(!enabled);
 artifactLoadSyntax().then(highlight=>{
  if(!view.isConnected)return;
  highlighted=artifactSyntaxFragment(text,language,highlight);status.textContent=language+' · exact source text';render();
 }).catch(()=>{if(view.isConnected){status.textContent=language+' · syntax highlighting unavailable; original text retained.';toggle.disabled=true;}});
 return view;
}

// Split generated spans at LF boundaries while retaining nested token classes.
// Each fragment must retain its original line; coordinates remain on the row.
function artifactSyntaxLines(text,language,highlight){
 const fragment=artifactSyntaxFragment(text,language,highlight),lines=[document.createDocumentFragment()];
 const visit=(node,classes=[])=>{
  if(node.nodeType===3){const parts=node.textContent.split('\n');parts.forEach((part,index)=>{if(index)lines.push(document.createDocumentFragment());let leaf=document.createTextNode(part);for(const cls of [...classes].reverse()){const span=el('span',cls);span.append(leaf);leaf=span;}lines.at(-1).append(leaf);});}
  else for(const child of node.childNodes)visit(child,node.nodeType===1?[...classes,node.className]:classes);
 };
 visit(fragment);return lines;
}
function artifactHunkSyntax(content,lines,path,enabled){
 const language=artifactSyntaxLanguage(path.split('.').pop().toLowerCase());if(!language)return;
 const status=el('p','artifact-review-help',language+' · highlighting hunk fragments…');content.before(status);
 const before=lines.filter(line=>line[0]==='-'||line[0]===' ').map(line=>line.slice(1)).join('\n'),after=lines.filter(line=>line[0]==='+'||line[0]===' ').map(line=>line.slice(1)).join('\n');
 if(new TextEncoder().encode(before+after).length>65536||lines.length>2000){status.textContent=language+' · plain hunk (highlighting limit: 64 KiB across both sides and 2,000 lines).';return;}
 let decorated=null;
 const rows=[...content.querySelectorAll('.diff-line-text')];
 const render=()=>rows.forEach((row,index)=>{row.replaceChildren(enabled&&decorated?.[index]?decorated[index].cloneNode(true):document.createTextNode(lines[index]));});
 content.setHighlight=value=>{enabled=!!value;render();};
 artifactLoadSyntax().then(highlight=>{
  if(!content.isConnected)return;
  const old=artifactSyntaxLines(before,language,highlight),next=artifactSyntaxLines(after,language,highlight);let oldIndex=0,nextIndex=0;
  decorated=lines.map(line=>{const prefix=line[0];let fragment;
   if(prefix==='-')fragment=old[oldIndex++];
   else if(prefix==='+')fragment=next[nextIndex++];
   else if(prefix===' '){oldIndex++;fragment=next[nextIndex++];}
   else return null;
   if(!fragment||fragment.textContent!==line.slice(1))throw Error('Hunk source mismatch');
   const result=document.createDocumentFragment();result.append(document.createTextNode(prefix),fragment);return result;
  });
  status.textContent=language+' · hunk fragments only; surrounding file context is unavailable.';render();
 }).catch(()=>{if(content.isConnected)status.textContent=language+' · syntax unavailable; original hunk retained.';});
}

