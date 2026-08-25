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
function collapsibleSection(host, title, summary, open) {
  const head = el("div", "pp-section-head toggle");
  const caret = el("span", "sec-caret", open ? "▾" : "▸");
  head.append(caret, el("span", "", title));
  const sum = el("span", "sec-summary", summary || "");
  head.append(sum);
  const body = el("div", "sec-body");
  body.hidden = !open;
  sum.hidden = open;
  head.onclick = () => {
    body.hidden = !body.hidden;
    caret.textContent = body.hidden ? "▸" : "▾";
    sum.hidden = !body.hidden;
  };
  host.append(head, body);
  return body;
}

// ---- THE money slot (§11 family #6) ----
// One place for how money renders and how a money field edits. Display
// precision is decided HERE once: whole dollars, thousands-separated.
// The semantic model is encoded once — est=plan, committed=accepted bid,
// paid=expense, unreconciled=done-but-unlinked — as the value shape
// moneySlot takes; sites declare their SEMANTICS (endpoints, branch
// actions) in callbacks, never the shell.

function fmtMoney(n) { return "$" + Math.round(n || 0).toLocaleString(); }
function fmtPct(x) { return Math.round((x || 0) * 100) + "%"; }

// moneyInput is the one numeric money field (.est-in, step 1).
function moneyInput(placeholder, initial) {
  const i = inputEl(placeholder);
  i.type = "number"; i.step = "1"; i.classList.add("est-in");
  if (initial > 0) i.value = initial;
  return i;
}

// moneySlotState derives the slot's class + label from the semantic value
// shape {paid, committed, est, firm:{amount,who}, checked, unreconciled}.
// The cascade: paid → firm bid → committed → est → empty.
function moneySlotState(v, emptyLabel) {
  let cls = "est-slot", label;
  if (v.paid > 0) { cls += " firm"; label = fmtMoney(v.paid) + " paid / " + fmtMoney(v.committed || v.paid); }
  else if (v.firm && v.firm.amount > 0) { cls += " firm"; label = fmtMoney(v.firm.amount) + (v.firm.who ? " · " + v.firm.who : ""); }
  else if (v.committed > 0) { cls += " firm"; label = fmtMoney(v.committed) + " committed"; }
  else if (v.est > 0) label = "est " + fmtMoney(v.est);
  else { cls += " empty"; label = emptyLabel || "$ —"; }
  if (v.checked && (v.unreconciled || 0) > 0) { cls += " unrec"; label = "⚑ " + label; }
  return { cls, label };
}

// moneySlot: the click-to-edit slot. opts:
//   value       the semantic shape above
//   emptyLabel  what an empty slot reads ("$ —" / "est —")
//   title       hover text (string, or (value) => string)
//   editor      (commit, revert) => {el, focus} — builds the open editor;
//               call revert() to restore the slot, commit is the site's save.
//               Wire Enter/Escape inside via moneyEditKeys.
// Returns the slot button. Sites re-render after save as they do today.
function moneySlot(opts) {
  const st = moneySlotState(opts.value || {}, opts.emptyLabel);
  const slot = el("button", st.cls, st.label);
  if (opts.title) slot.title = typeof opts.title === "function" ? opts.title(opts.value) : opts.title;
  if (opts.editor) {
    slot.onclick = (e) => {
      e.stopPropagation();
      let ed;
      const revert = () => { if (ed.el.parentNode) ed.el.replaceWith(slot); };
      ed = opts.editor(opts.commit, revert);
      slot.replaceWith(ed.el);
      if (ed.focus) ed.focus();
    };
  }
  return slot;
}

// moneyEditKeys wires the standard keys on an editor input: Enter saves,
// Escape reverts. Blur policy stays the site's (estSlot reverts on blur;
// priceSlot deliberately doesn't — blur would kill the typeahead pick).
function moneyEditKeys(input, save, revert) {
  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") save();
    else if (ev.key === "Escape") revert();
  });
}

// moneyTripletRow: one plan/committed/paid table row (.pp-money-row) —
// first amount renders an em-dash when absent, the rest blank, matching
// the MONEY block's tables.
function moneyTripletRow(colsClass, label, a, b, c, over) {
  const row = el("div", "pp-money-row " + colsClass + (over ? " over" : ""));
  row.append(el("span", "", label),
    el("span", "pp-amt", a ? fmtMoney(a) : "—"),
    el("span", "pp-amt", b ? fmtMoney(b) : ""),
    el("span", "pp-amt", c ? fmtMoney(c) : ""));
  return row;
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
