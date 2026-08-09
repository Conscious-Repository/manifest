// ---- AION: the program cockpit over system/aion/ records ----
// Backlog (task/decision substrate) · Heuristics (living synthesis) · V/TO ·
// Goals (read-only Aion ladder) · Org (people/hiring/references/finances) ·
// Settings — plus the publish rail (aionbio export effector). Manifest is the
// only edit surface; the team portal is read-only, fed by PUBLISH.
let aionCache = null;
let aionMode = "backlog"; // backlog | heuristics | vto | goals | org | settings
let aionKindFilter = ""; // "" = all | task | decision
let aionStatusFilter = "open"; // open | done | all
let aionExpanded = {}; // heuristic id → sources expanded

function showAion(h) {
  const tail = h.startsWith("#/aion/") ? decodeURIComponent(h.slice("#/aion/".length)) : "";
  aionMode = tail || "backlog";
  els.aionToggle.querySelectorAll(".filter-chip").forEach((b) =>
    b.classList.toggle("on", b.dataset.mode === aionMode));
  loadAion();
}

async function loadAion() {
  try { aionCache = await (await fetch("/api/aion")).json(); }
  catch (e) { aionCache = null; }
  renderAion();
}

function renderAion() {
  renderAionRail();
  const host = els.aionBody;
  host.innerHTML = "";
  if (!aionCache) { host.append(emptyRow("aion unavailable")); return; }
  if (aionMode === "heuristics") renderAionHeuristics(host);
  else if (aionMode === "vto") renderAionVTO(host);
  else if (aionMode === "goals") renderAionGoals(host);
  else if (aionMode === "org") renderAionOrg(host);
  else if (aionMode === "settings") renderAionSettings(host);
  else renderAionBacklog(host);
}

async function aionPost(url, body, okMsg) {
  try {
    const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!r.ok) throw new Error(await r.text());
    if (okMsg) showToast(okMsg);
    await loadAion();
  } catch (e) { showToast(String(e.message || e).slice(0, 120)); }
}

// ---- publish rail: last published + per-section dirty dots + PUBLISH ----
function renderAionRail() {
  const rail = els.aionPublishRail;
  rail.innerHTML = "";
  const pub = (aionCache && aionCache.publish) || {};
  if (pub.lastPublished) {
    rail.append(el("span", "aion-last", "published " + fmtWhen(pub.lastPublished) +
      (pub.lastCommit ? " · " + pub.lastCommit.slice(0, 7) : "")));
  } else if (pub.configured) {
    rail.append(el("span", "aion-last", "never published"));
  }
  const dirty = pub.dirty || {};
  const dots = el("span", "aion-dots");
  ["finances", "vto", "goals", "backlog", "heuristics", "people", "hiring", "references"].forEach((name) => {
    if (name in dirty) dots.append(statusDot(dirty[name], name + (dirty[name] ? " — unpublished changes" : " — clean")));
  });
  if (dots.children.length) rail.append(dots);
  if (pub.configured) rail.append(pill("PUBLISH", openAionPublishPanel));
}

// openAionPublishPanel: PREVIEW (no writes) → blockers or per-file diffs →
// CONFIRM (carries the preview hash so a vault change forces a re-preview).
async function openAionPublishPanel() {
  if (document.getElementById("aionPublishModal")) return;
  const overlay = el("div", "cmdbar");
  overlay.id = "aionPublishModal";
  const back = el("div", "cmdbar-backdrop");
  const panel = el("div", "cmdbar-card aion-publish-panel");
  overlay.append(back, panel);
  document.body.append(overlay);
  const close = () => overlay.remove();
  back.onclick = close;
  panel.append(el("div", "appr-diff-label", "PUBLISH → aion.bio/portal — preview"));
  const bodyHost = el("div", "aion-publish-body");
  panel.append(bodyHost, el("div", "aion-publish-note", "fetching preview…"));
  let prev;
  try { prev = await (await fetch("/api/aion/publish/preview")).json(); }
  catch (e) { panel.lastChild.textContent = "preview failed: " + e; return; }
  panel.lastChild.remove();

  if ((prev.blockers || []).length) {
    prev.blockers.forEach((b) => bodyHost.append(el("div", "appr-blocked", "⚠ " + b)));
    panel.append(pillLight("close", close));
    return;
  }
  if ((prev.untracked || []).length) {
    bodyHost.append(el("div", "aion-publish-warn",
      "⚠ the checkout has portal files git doesn't know about yet — publish never overwrites unpreserved work."));
    prev.untracked.forEach((f) => bodyHost.append(el("div", "aion-preview-row mono", f)));
    bodyHost.append(el("div", "aion-publish-note",
      "\"preserve & continue\" commits everything under public/portal exactly as it is (its own commit — nothing is lost, and the publish diff stays honest), then re-opens this preview."));
    const acts = el("div", "appr-actions");
    const keep = pill("preserve in a baseline commit & continue", async () => {
      keep.disabled = true;
      try {
        const r = await fetch("/api/aion/publish/baseline", { method: "POST" });
        const res = await r.json().catch(() => ({}));
        if (!r.ok || !res.ok) { showToast("baseline failed: " + (res.error || r.status)); return; }
        showToast("Preserved as " + (res.commit || "").slice(0, 7));
        close();
        openAionPublishPanel(); // fresh preview, now clean
      } finally { keep.disabled = false; }
    });
    acts.append(keep, pillLight("close", close));
    panel.append(acts);
    return;
  }
  const changed = (prev.files || []).filter((f) => f.status !== "unchanged");
  if (!changed.length && !(prev.unpushed > 0)) {
    bodyHost.append(el("div", "aion-publish-note", "nothing to publish — the checkout matches the record."));
    panel.append(pillLight("close", close));
    return;
  }
  if (prev.unpushed > 0) {
    bodyHost.append(el("div", "aion-publish-note", prev.unpushed + " unpushed commit(s) — publish completes the push."));
  }
  changed.forEach((f) => {
    const head = el("div", "aion-pub-file");
    head.append(el("code", "", f.path), el("span", "aion-pub-status " + f.status, f.status));
    bodyHost.append(head);
    if (f.diff) bodyHost.append(collapsibleBlock(diffView(f.diff), f.diff.split("\n").length));
  });
  const actions = el("div", "appr-actions");
  const confirmBtn = pill("CONFIRM — commit + push", async () => {
    confirmBtn.disabled = true;
    try {
      const r = await fetch("/api/aion/publish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hash: prev.hash }) });
      const res = await r.json().catch(() => ({}));
      if (r.status === 409) { showToast("Vault changed since preview — re-open PUBLISH"); close(); return; }
      if (!r.ok || res.ok === false) {
        showToast("Publish failed at " + (res.stage || "?") + (res.commit ? " (commit " + res.commit.slice(0, 7) + " kept locally)" : ""));
      } else {
        showToast("Published " + (res.commit || "").slice(0, 7) + " → " + ((aionCache.publish || {}).remote || "origin"));
      }
      close();
      loadAion();
    } finally { confirmBtn.disabled = false; }
  });
  actions.append(confirmBtn, pillLight("cancel", close));
  panel.append(actions);
}

// ---- BACKLOG ----
let aionEditingId = null; // row whose edit drawer is open (survives re-render)

function renderAionBacklog(host) {
  const items = aionCache.backlog || [];
  const chips = el("div", "aion-chips");
  const chip = (label, on, fn) => { const c = el("button", "filter-chip" + (on ? " on" : ""), label); c.onclick = fn; return c; };
  [["ALL", ""], ["TASKS", "task"], ["DECISIONS", "decision"]].forEach(([l, v]) =>
    chips.append(chip(l, aionKindFilter === v, () => { aionKindFilter = v; renderAion(); })));
  chips.append(el("span", "aion-chip-gap", ""));
  [["OPEN", "open"], ["DONE \u00b7 DECIDED", "done"], ["ALL", "all"]].forEach(([l, v]) =>
    chips.append(chip(l, aionStatusFilter === v, () => { aionStatusFilter = v; renderAion(); })));
  host.append(chips);

  const closed = (it) => it.status === "done" || it.status === "decided";
  const visible = items.filter((it) =>
    (!aionKindFilter || it.kind === aionKindFilter) &&
    (aionStatusFilter === "all" || (aionStatusFilter === "done" ? closed(it) : !closed(it))));

  if (!visible.length) host.append(emptyRow("nothing here \u2014 capture with \uff0b below, or approve extraction proposals in FEED"));
  visible.forEach((it) => host.append(aionBacklogRow(it)));

  const adds = el("div", "aion-adds");
  adds.append(ghostInput("\uff0b task", "aion-add", (v) =>
    aionPost("/api/aion/backlog/item", { kind: "task", title: v }, "Task added")));
  adds.append(ghostInput("\uff0b decision", "aion-add", (v) =>
    aionPost("/api/aion/backlog/item", { kind: "decision", title: v }, "Decision added")));
  host.append(adds);
}

// statusChip: the one legible state control — a labeled chip, never an icon.
function aionStatusChip(it) {
  let label, cls;
  if (it.kind === "decision") {
    if (it.status === "decided") { label = "DECIDED" + (it.decided ? " " + it.decided : ""); cls = "closed"; }
    else { label = "OPEN"; cls = "open"; }
  } else if (it.status === "done") { label = "DONE" + (it.doneOn ? " " + it.doneOn : ""); cls = "closed"; }
  else if (it.status === "in_progress") { label = "IN PROGRESS"; cls = "active"; }
  else { label = "OPEN"; cls = "open"; }
  return el("span", "aion-status " + cls, label);
}

function aionBacklogRow(it) {
  const done = it.status === "done" || it.status === "decided";
  const wrap = el("div", "aion-item" + (done ? " done" : ""));
  const row = el("div", "aion-row");

  // done toggle (tasks) / decision glyph \u2014 STATUS is the source of truth
  // (a few imported lines carry a checked mark with status open; the field
  // wins, and the first toggle re-syncs the mark)
  if (it.kind === "task") {
    const isDone = it.status === "done";
    const c = el("button", "aion-check", isDone ? "\u25cf" : "\u25cb");
    c.title = isDone ? "reopen" : "mark done";
    c.onclick = (e) => { e.stopPropagation(); aionPost("/api/aion/backlog/" + it.id + "/update",
      { status: isDone ? "open" : "done" }); };
    row.append(c);
  } else {
    const d = el("span", "aion-check muted", "\u25c7");
    d.title = "decision";
    row.append(d);
  }

  // main: title + meta line
  const main = el("div", "aion-main");
  main.append(el("div", "aion-title", it.text));
  const meta = el("div", "aion-item-meta");
  const bits = [];
  if (it.owner) bits.push("@" + it.owner);
  if (it.kind === "task" && it.rock) bits.push("\u29d7 " + rockLabel(it.rock));
  if (it.kind === "task" && it.due && !done) bits.push((it.due < isoToday() ? "\u26a0 overdue " : "due ") + it.due);
  if (it.kind === "decision" && it.neededBy && !done) bits.push("needed by " + it.neededBy);
  if (it.status === "decided" && it.outcome) bits.push("\u2192 " + it.outcome);
  if (it.captured) bits.push(it.captured);
  meta.textContent = bits.join("  \u00b7  ");
  if ((it.sources || []).length) {
    const src = el("a", "aion-src", " \u2398 " + it.sources[0]);
    src.title = "open source note";
    src.href = "#/note/" + encodeURIComponent(aionSourcePath(it.sources[0]));
    src.onclick = (e) => e.stopPropagation();
    meta.append(src);
  }
  main.append(meta);
  row.append(main);

  row.append(aionStatusChip(it));

  const edit = el("button", "aion-edit-btn", aionEditingId === it.id ? "\u2715" : "edit");
  edit.title = aionEditingId === it.id ? "close editor" : "edit this " + it.kind;
  row.append(edit);

  const toggle = (e) => {
    if (e) e.stopPropagation();
    aionEditingId = aionEditingId === it.id ? null : it.id;
    renderAion();
  };
  edit.onclick = toggle;
  row.onclick = toggle; // the whole row opens the editor
  wrap.append(row);
  if (aionEditingId === it.id) wrap.append(aionRowEditor(it));
  return wrap;
}

// aionRowEditor: the labeled edit drawer — every field visible at once, one
// SAVE for the lot (a single update POST), plus DECIDE for open decisions.
function aionRowEditor(it) {
  const box = el("div", "aion-editor");
  box.onclick = (e) => e.stopPropagation();
  const edits = {};
  const grid = el("div", "aion-editor-grid");
  box.append(grid);
  const field = (label, node) => { grid.append(el("span", "aion-vto-key", label), node); return node; };

  const title = field("title", inputEl("title"));
  title.value = it.text;
  title.classList.add("aion-editor-wide");
  title.oninput = () => { edits.title = title.value; };

  const ownerTa = typeahead({ placeholder: "initials", initial: it.owner || "",
    suggest: aionOwnerSuggest, onChange: (v) => { edits.owner = v; } });
  ownerTa.input.addEventListener("input", () => { edits.owner = ownerTa.value(); });
  field("owner", ownerTa.el);

  if (it.kind === "task") {
    let rockPickedText = rockLabel(it.rock) || null;
    const rockTa = typeahead({
      placeholder: "type to pick a rock\u2026", initial: rockLabel(it.rock),
      suggest: (q, add, ta) => aionRockSuggest(q, add, ta, (id, text) => { edits.rock = id; rockPickedText = text; }),
      // free-typed rock text commits verbatim (his corpus tags free slugs)
      onChange: (v) => { if (v !== rockPickedText) edits.rock = v; },
    });
    field("rock", rockTa.el);
    const due = field("due", inputEl(""));
    due.type = "date"; due.value = it.due || "";
    due.onchange = () => { edits.due = due.value; };
    if (it.status !== "done") {
      const st = field("status", selectEl(["open", "in progress"]));
      st.value = it.status === "in_progress" ? "in progress" : "open";
      st.onchange = () => { edits.status = st.value === "in progress" ? "in_progress" : "open"; };
    }
  } else {
    const nb = field("needed by", inputEl("date or condition"));
    nb.value = it.neededBy || "";
    nb.oninput = () => { edits.needed_by = nb.value; };
  }

  const actions = el("div", "aion-editor-actions");
  actions.append(pill("save", async () => {
    if (!Object.keys(edits).length) { aionEditingId = null; renderAion(); return; }
    aionEditingId = null;
    await aionPost("/api/aion/backlog/" + it.id + "/update", edits, "Saved");
  }));
  actions.append(pillLight("cancel", () => { aionEditingId = null; renderAion(); }));

  if (it.kind === "decision" && it.status !== "decided") {
    const outcome = inputEl("outcome \u2014 what was decided\u2026");
    outcome.classList.add("aion-editor-outcome");
    const decideBtn = pill("decide \u2192 permanent log", () => {
      if (!outcome.value.trim()) { showToast("write the outcome first"); outcome.focus(); return; }
      aionEditingId = null;
      aionPost("/api/aion/backlog/" + it.id + "/decide", { outcome: outcome.value.trim() },
        "Decided \u2014 permanent log");
    });
    const drow = el("div", "aion-editor-decide");
    drow.append(outcome, decideBtn);
    box.append(drow);
  }
  box.append(actions);
  return box;
}

// aionSourcePath resolves a source note name to a vault path: names already
// carrying a folder (log/\u2026) pass through; dated names default to log/.
function aionSourcePath(name) {
  if (name.includes("/")) return name + ".md";
  return (/^\d{4}-\d{2}-\d{2} /.test(name) ? "log/" : "") + name + ".md";
}

function rockLabel(id) {
  if (!id) return "";
  const area = aionCache.goalsArea;
  const all = ((area && area.rocks) || []);
  const rock = all.find((r) => r.id === id);
  return rock ? rock.text : id;
}

function aionOwnerSuggest(q, add, ta) {
  (aionCache.people || [])
    .filter((p) => !q || p.initials.toLowerCase().includes(q) || (p.name || "").toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((p) => add(p.initials + " \u00b7 " + (p.name || ""), "", () => { ta.commit(p.initials); ta.input.dispatchEvent(new Event("change")); }));
}

function aionRockSuggest(q, add, ta, onPick) {
  const area = aionCache.goalsArea;
  ((area && area.rocks) || []).filter((r) => !r.checked)
    .filter((r) => !q || r.text.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((r) => add(r.text, "", () => { ta.commit(r.text); onPick(r.id, r.text); }));
  add("\u2715 no rock (unanchored)", "create", () => { ta.commit(""); onPick("", ""); });
}

// inlineEdit swaps a span for an input; Enter saves, Escape restores.
function inlineEdit(span, value, save) {
  const input = inputEl("");
  input.value = value;
  input.className = "aion-title-in";
  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && input.value.trim()) save(input.value.trim());
    else if (ev.key === "Escape") renderAion();
  });
  input.addEventListener("blur", () => renderAion());
  span.replaceWith(input);
  input.focus();
}

// ---- HEURISTICS ----
function renderAionHeuristics(host) {
  const list = aionCache.heuristics || [];
  host.append(el("div", "aion-section-note",
    "the living synthesis — statements accrete reinforcements; merge and prune so they strengthen, never accumulate"));
  if (!list.length) host.append(emptyRow("no heuristics yet — they arrive as extraction proposals in FEED"));
  list.forEach((h, i) => host.append(aionHeuristicRow(h, i, list)));

  const retired = aionCache.retired || [];
  if (retired.length) {
    const body = collapsibleSection(host, "retired", retired.length + " pruned — provenance kept", false);
    retired.forEach((h) => {
      const row = el("div", "aion-heur-row retired");
      row.append(el("span", "aion-heur-text", h.text), el("span", "aion-heur-count", "×" + (h.sources || []).length));
      body.append(row);
    });
  }
}

function aionHeuristicRow(h, i, list) {
  const wrap = el("div", "aion-heur");
  const row = el("div", "aion-heur-row");
  const caret = el("button", "aion-heur-caret", aionExpanded[h.id] ? "▾" : "▸");
  caret.onclick = () => { aionExpanded[h.id] = !aionExpanded[h.id]; renderAion(); };
  const text = el("span", "aion-heur-text", h.text);
  text.onclick = () => inlineEdit(text, h.text, (v) =>
    aionPost("/api/aion/heuristics/" + h.id + "/edit", { statement: v }));
  const n = (h.sources || []).length;
  row.append(caret, text, el("span", "aion-heur-count", n > 1 ? "×" + n : ""));

  const acts = el("span", "aion-heur-acts");
  const move = (delta) => {
    const ids = list.map((x) => x.id);
    const j = i + delta;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    aionPost("/api/aion/heuristics/reorder", { ids });
  };
  acts.append(
    miniBtn("↑", "move up", () => move(-1)),
    miniBtn("↓", "move down", () => move(1)),
    miniBtn("⇄", "merge another statement into this one", () => aionMergePicker(h, list)),
    miniBtn("✕", "retire (moves under ## retired — never deletes)", () =>
      aionPost("/api/aion/heuristics/" + h.id + "/retire", {}, "Retired — provenance kept")));
  row.append(acts);
  wrap.append(row);

  if (aionExpanded[h.id]) {
    const src = el("div", "aion-heur-sources");
    if (h.first) src.append(el("div", "aion-heur-src", "first · " + h.first));
    (h.sources || []).forEach((r) => {
      const line = el("div", "aion-heur-src");
      const a = el("a", "", "[[" + r.note + "]]");
      a.href = "#/note/" + encodeURIComponent(aionSourcePath(r.note));
      line.append(a, el("span", "", r.date ? " · " + r.date : ""));
      src.append(line);
    });
    if (!(h.sources || []).length) src.append(el("div", "aion-heur-src muted", "no reinforcements recorded"));
    wrap.append(src);
  }
  return wrap;
}

function miniBtn(label, title, fn) {
  const b = el("button", "aion-mini", label);
  b.title = title;
  b.onclick = fn;
  return b;
}

function aionMergePicker(into, list) {
  const others = list.filter((x) => x.id !== into.id);
  if (!others.length) { showToast("nothing to merge"); return; }
  const ta = typeahead({
    placeholder: "merge which statement into “" + into.text.slice(0, 40) + "…”?",
    suggest: (q, add) => {
      others.filter((x) => !q || x.text.toLowerCase().includes(q)).slice(0, 8)
        .forEach((x) => add(x.text, "", () =>
          aionPost("/api/aion/heuristics/merge", { into: into.id, from: x.id }, "Merged — sources unioned")));
    },
    onEscape: () => renderAion(),
  });
  const host = els.aionBody;
  host.prepend(ta.el);
  ta.focus();
}

// ---- V/TO ----
function renderAionVTO(host) {
  const sections = aionCache.vto || [];
  const edited = JSON.parse(JSON.stringify(sections)); // working copy
  const bar = makeDirtyBar(els.aionView, async () => {
    await putJSON("/api/aion/vto", { sections: edited.map((s) => ({ heading: s.heading, entries: s.entries })) });
    showToast("Saved — vto.md updated");
    loadAion();
  }, () => renderAion());

  edited.forEach((sec) => {
    const head = el("div", "pp-section-head", sec.heading.toUpperCase());
    host.append(head);
    const body = el("div", "aion-vto-sec");
    host.append(body);
    const renderRows = () => {
      body.innerHTML = "";
      sec.entries.forEach((entry, i) => {
        const rowEl = el("div", "aion-vto-row");
        if ((entry.fields || []).length) {
          entry.fields.forEach((f) => {
            rowEl.append(el("span", "aion-vto-key", f.key));
            const input = inputEl(f.key);
            input.value = f.value || "";
            input.oninput = () => { f.value = input.value; bar.mark(); };
            rowEl.append(input);
          });
        } else {
          const input = inputEl("");
          input.value = entry.text || "";
          input.classList.add("aion-vto-text");
          input.oninput = () => { entry.text = input.value; bar.mark(); };
          rowEl.append(input);
        }
        const x = el("button", "aion-mini", "✕");
        x.title = "remove line";
        x.onclick = () => { sec.entries.splice(i, 1); bar.mark(); renderRows(); };
        rowEl.append(x);
        body.append(rowEl);
      });
      (sec.raw || []).forEach((r) => body.append(el("div", "aion-vto-raw", r)));
      body.append(ghostInput("＋ line", "aion-add", (v) => {
        sec.entries.push({ text: v, fields: [] });
        bar.mark();
        renderRows();
      }));
    };
    renderRows();
  });
  if (!sections.length) host.append(emptyRow("vto.md is empty — seed sections appear after first run"));
  host.append(el("div", "aion-section-note", "issues live in the backlog — open decisions are the issues list →"));
}

// ---- GOALS (read-only) ----
function renderAionGoals(host) {
  const area = aionCache.goalsArea;
  const head = el("div", "aion-goals-head");
  head.append(el("span", "aion-section-note", "the Aion ladder — read-only here"),
    pillLight("edit in GOALS tab →", () => { location.hash = "#/goals"; }));
  host.append(head);
  if (!area) { host.append(emptyRow("no ## Aion area in goals.md yet")); return; }
  if (area.northStar) host.append(el("div", "aion-northstar", "✦ " + area.northStar));

  const rocksByServes = {};
  (area.rocks || []).forEach((r) => {
    const links = (r.serves || []);
    if (!links.length) (rocksByServes[""] = rocksByServes[""] || []).push(r);
    links.forEach((sv) => { (rocksByServes[sv] = rocksByServes[sv] || []).push(r); });
  });
  const chipRow = (g, kind) => {
    const row = el("div", "aion-goal-row " + kind + (g.checked ? " done" : ""));
    row.append(el("span", "aion-goal-mark", g.checked ? "●" : "○"), el("span", "aion-goal-text", g.text));
    const chips = el("span", "aion-goal-chips");
    (g.serves || []).forEach((sv) => chips.append(el("span", "aion-goal-chip", "serves " + sv)));
    if (g.owner && g.owner !== "me") chips.append(el("span", "aion-goal-chip", "@" + g.owner));
    if (g.quarter) chips.append(el("span", "aion-goal-chip", g.quarter));
    if (g.status && g.status !== "active") chips.append(el("span", "aion-goal-chip", g.status));
    row.append(chips);
    return row;
  };
  (area.annuals || []).forEach((a) => {
    host.append(chipRow(a, "annual"));
    (rocksByServes[a.id] || []).forEach((r) => {
      host.append(chipRow(r, "rock"));
      (r.children || []).forEach((c) => host.append(chipRow(c, "milestone")));
    });
  });
  const unanchored = (rocksByServes[""] || []);
  if (unanchored.length) {
    host.append(el("div", "pp-section-head", "UNANCHORED — no [serves::] chain"));
    unanchored.forEach((r) => host.append(chipRow(r, "rock unlinked")));
  }
}

// parseMoneyShorthand mirrors the exporter's ParseMoney: "1.95M", "85k",
// "$2,480,000" → dollars; NaN when not money-shaped.
function parseMoneyShorthand(s) {
  s = String(s || "").trim().replace(/[$,\s_]/g, "");
  const m = /^([0-9]*\.?[0-9]+)([kKmMbB])?$/.exec(s);
  if (!m) return NaN;
  const mult = { k: 1e3, m: 1e6, b: 1e9 }[(m[2] || "").toLowerCase()] || 1;
  return parseFloat(m[1]) * mult;
}

// ---- ORG: people / hiring / references / finances ----

// aionTableEditor: the one registry-editing idiom — labeled columns, a row
// per record, add/remove, one dirty-bar save (full-list PUT; verbatim lines
// in the file survive server-side). `open` links the raw note for anything
// the table doesn't model.
function aionTableEditor(host, opts) {
  const head = el("div", "pp-section-head");
  head.append(el("span", "", opts.title));
  const open = el("a", "aion-open", "open →");
  open.href = "#/note/" + encodeURIComponent(opts.rel);
  head.append(open);
  host.append(head);

  const rows = opts.rows.map((r) => ({ ...r }));
  const bar = makeDirtyBar(els.aionView, async () => {
    await putJSON(opts.put, { [opts.payloadKey]: rows });
    showToast("Saved — " + opts.rel.split("/").pop() + " updated");
    loadAion();
  }, () => renderAion());

  host.append(ppCols(opts.colsClass, opts.cols.map((c) => c.label).concat([""])));
  const body = el("div", "aion-table");
  host.append(body);
  const renderRows = () => {
    body.innerHTML = "";
    rows.forEach((row, i) => {
      const line = el("div", "aion-row " + opts.colsClass);
      opts.cols.forEach((c) => {
        const input = inputEl(c.label.toLowerCase());
        input.value = row[c.key] || "";
        input.oninput = () => { row[c.key] = input.value; bar.mark(); };
        line.append(input);
      });
      const x = el("button", "aion-mini", "✕");
      x.title = "remove row";
      x.onclick = () => { rows.splice(i, 1); bar.mark(); renderRows(); };
      line.append(x);
      body.append(line);
    });
    body.append(ghostInput("＋ " + opts.addLabel, "aion-add", (v) => {
      rows.push(opts.newRow(v));
      bar.mark();
      renderRows();
    }));
  };
  renderRows();
}

function renderAionOrg(host) {
  aionTableEditor(host, {
    title: "PEOPLE", rel: "system/aion/people.md",
    colsClass: "cols-aion-people",
    cols: [{ key: "initials", label: "INITIALS" }, { key: "name", label: "NAME" }, { key: "role", label: "ROLE" }],
    rows: aionCache.people || [],
    put: "/api/aion/people", payloadKey: "people",
    addLabel: "person", newRow: (v) => ({ initials: v.toUpperCase().slice(0, 3), name: "", role: "" }),
  });

  aionTableEditor(host, {
    title: "HIRING", rel: "system/aion/hiring.md",
    colsClass: "cols-aion-hiring",
    cols: [{ key: "role", label: "ROLE" }, { key: "candidate", label: "CANDIDATE" },
      { key: "stage", label: "STAGE" }, { key: "priority", label: "PRI" }],
    rows: aionCache.hiring || [],
    put: "/api/aion/hiring", payloadKey: "items",
    addLabel: "role", newRow: (v) => ({ role: v, candidate: "", stage: "", priority: "" }),
  });

  aionTableEditor(host, {
    title: "REFERENCES", rel: "system/aion/references.md",
    colsClass: "cols-aion-refs",
    cols: [{ key: "text", label: "TITLE" }, { key: "url", label: "URL" },
      { key: "source", label: "SOURCE" }, { key: "date", label: "DATE" }],
    rows: aionCache.references || [],
    put: "/api/aion/references", payloadKey: "references",
    addLabel: "reference", newRow: (v) => ({ text: v, url: "", source: "", date: "" }),
  });

  // FINANCES — compact; the private body stays hand-edited, runway derived
  host.append(el("div", "pp-section-head", "FINANCES"));
  const fin = { ...(aionCache.finances || {}) };
  const finBar = makeDirtyBar(els.aionView, async () => {
    await putJSON("/api/aion/finances", fin);
    showToast("Saved — finances.md frontmatter updated");
    loadAion();
  }, () => renderAion());
  const finHost = el("div", "aion-fin");
  host.append(finHost);
  ["capital", "monthly_burn", "as_of", "currency", "source", "note"].forEach((key) => {
    const row = el("div", "aion-fin-row");
    row.append(el("span", "aion-vto-key", key.replace("_", " ")));
    const input = inputEl(key);
    input.value = fin[key] || "";
    input.oninput = () => { fin[key] = input.value; finBar.mark(); };
    row.append(input);
    finHost.append(row);
  });
  const cap = parseMoneyShorthand(fin.capital);
  const burn = parseMoneyShorthand(fin.monthly_burn);
  const runwayRow = el("div", "aion-fin-row derived");
  runwayRow.append(el("span", "aion-vto-key", "runway"),
    el("span", "aion-fin-derived", cap > 0 && burn > 0 ? (Math.round((cap / burn) * 10) / 10) + " months (derived)" : "— (needs capital + burn)"));
  finHost.append(runwayRow);
  host.append(el("div", "aion-section-note",
    "these fields publish to the portal (finances.json) on PUBLISH — capital & burn take 1.95M / 85k / $2,480,000 shorthand"));
}

// ---- SETTINGS ----
function renderAionSettings(host) {
  const pub = aionCache.publish || {};
  host.append(el("div", "pp-section-head", "PUBLISH TARGET (config.json → aionPortal; restart to change)"));
  const kv = (k, v) => {
    const row = el("div", "aion-fin-row");
    row.append(el("span", "aion-vto-key", k), el("span", "aion-fin-derived", v || "—"));
    host.append(row);
  };
  kv("checkout", pub.checkout || "(not configured — publish disabled)");
  kv("remote · branch", pub.configured ? (pub.remote + " · " + pub.branch) : "");
  kv("last published", pub.lastPublished ? pub.lastPublished + (pub.lastCommit ? " · " + pub.lastCommit.slice(0, 12) : "") : "never");
  if (pub.renderError) host.append(el("div", "appr-blocked", "⚠ render error: " + pub.renderError));

  host.append(el("div", "pp-section-head", "CONTRACT PATHS — the only files publish may write"));
  ["public/portal/content/hiring.md", "public/portal/content/references.md",
    "public/portal/data/finances.json", "public/portal/data/vto.json", "public/portal/data/goals.json",
    "public/portal/data/backlog.json", "public/portal/data/heuristics.json", "public/portal/data/people.json",
    "public/portal/data/meta.json"].forEach((p) => host.append(el("div", "aion-preview-row mono", p)));
  host.append(el("div", "aion-section-note",
    "dirty check is scoped to these paths — uncommitted human edits inside them block publish; anything else in the checkout is ignored and never committed"));

  host.append(el("div", "pp-section-head", "PUBLISH HISTORY"));
  const hist = (pub.history || []).slice().reverse();
  if (!hist.length) host.append(emptyRow("no publishes yet"));
  hist.forEach((r) => {
    const row = el("div", "aion-preview-row" + (r.status === "failed" ? " failed" : ""));
    row.textContent = fmtWhen(r.at) + " · " + r.status + (r.stage ? " @ " + r.stage : "") +
      (r.commit ? " · " + r.commit.slice(0, 7) : "") + ((r.files || []).length ? " · " + r.files.length + " file(s)" : "");
    if (r.error) row.title = r.error;
    host.append(row);
  });
}
