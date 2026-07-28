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
