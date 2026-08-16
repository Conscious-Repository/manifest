// ---- AION: the program cockpit over system/aion/ records ----
// Backlog (task/decision substrate) · Heuristics (living synthesis) · V/TO ·
// Goals (read-only Aion ladder) · Org (people/hiring/references/finances) ·
// Settings — plus the publish rail (export effector → the portal's serving
// checkout, live at portal.aion.bio). Manifest is the only edit surface;
// the team portal's published data is fed by PUBLISH (team writes ride the
// separate /shared overlay).
let aionCache = null;
let aionMode = "backlog"; // backlog | heuristics | vto | goals | org | settings
let aionSelId = null;     // inspector selection (redesign §4 — replaces the drawer)
let aionOrgSel = "people"; // org registry rail selection
let aionDoneOpen = false;    // backlog: done-tasks section expanded
let aionDecidedOpen = false; // backlog: decided-decisions log expanded
let aionFreshDone = new Set(); // tasks checked this session — held in place until PUBLISH
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
  // reconcile + settings tabs retired (owner calls) — inbound lives in FEED
  else if (aionMode === "reconcile" || aionMode === "settings") { location.hash = "#/aion"; return; }
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

// ---- publish → the page header actions (next to the tabs): ONE count badge
// replaces the eight per-section dirty dots; the meta string carries
// last-published. Moved out of the (now section-hidden) breadcrumb bar.
function renderAionRail() {
  const rail = els.aionPublishRail;
  rail.innerHTML = "";
  if (els.aionView.hidden) return;
  const pub = (aionCache && aionCache.publish) || {};
  if (els.aionMeta) {
    els.aionMeta.textContent = pub.lastPublished
      ? "published " + fmtWhen(pub.lastPublished) + (pub.lastCommit ? " · " + pub.lastCommit.slice(0, 7) : "")
      : (pub.configured ? "never published" : "");
  }
  if (!pub.configured) return;
  const dirty = pub.dirty || {};
  const n = Object.values(dirty).filter(Boolean).length;
  const btn = el("button", "aion-publish-btn", "PUBLISH");
  if (n) {
    const badge = el("span", "aion-publish-badge", String(n));
    badge.title = n + " section" + (n === 1 ? "" : "s") + " with unpublished changes";
    btn.append(badge);
  }
  btn.onclick = openAionPublishPanel;
  rail.append(btn);
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
  panel.append(el("div", "appr-diff-label", "PUBLISH → portal.aion.bio — preview"));
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
  // acceptance gate (sync-contract §4): errors block the push; warnings inform.
  const gateErrors = prev.errors || [];
  (prev.warnings || []).forEach((wmsg) => bodyHost.append(el("div", "aion-publish-warn", "△ " + wmsg)));
  gateErrors.forEach((emsg) => bodyHost.append(el("div", "appr-blocked", "⚠ " + emsg)));
  changed.forEach((f) => {
    const head = el("div", "aion-pub-file");
    head.append(el("code", "", f.path), el("span", "aion-pub-status " + f.status, f.status));
    bodyHost.append(head);
    if (f.diff) bodyHost.append(collapsibleBlock(diffView(f.diff), f.diff.split("\n").length));
  });
  const actions = el("div", "appr-actions");
  const confirmBtn = pill("CONFIRM — commit + push", async () => {
    confirmBtn.disabled = true;
    if (gateErrors.length) { confirmBtn.disabled = false; return; } // guard: gate blocks the push
    try {
      const r = await fetch("/api/aion/publish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hash: prev.hash }) });
      const res = await r.json().catch(() => ({}));
      if (r.status === 409) { showToast("Vault changed since preview — re-open PUBLISH"); close(); return; }
      if (!r.ok || res.ok === false) {
        showToast("Publish failed at " + (res.stage || "?") + (res.commit ? " (commit " + res.commit.slice(0, 7) + " kept locally)" : ""));
      } else {
        showToast("Published " + (res.commit || "").slice(0, 7) + " → " + ((aionCache.publish || {}).remote || "origin"));
        aionFreshDone.clear(); // publish is the flush point — done tasks may now collapse
      }
      close();
      loadAion();
    } finally { confirmBtn.disabled = false; }
  });
  if (gateErrors.length) {
    confirmBtn.disabled = true;
    bodyHost.append(el("div", "aion-publish-note", "fix the ⚠ format errors above before publishing (they'd break the portal)."));
  }
  actions.append(confirmBtn, pillLight("cancel", close));
  panel.append(actions);
}

// ---- BACKLOG (redesign §4): decisions lane on top, tasks grouped by owner,
// a 300px sticky inspector on the right. The filter-chip rows are gone —
// their job is done by the structure. Selecting a row never reflows the list.

function renderAionBacklog(host) {
  const items = aionCache.backlog || [];
  const wrap = el("div", "aion-backlog");
  const list = el("div", "aion-list");
  const insp = el("div", "aion-inspector");
  wrap.append(list, insp);
  host.append(wrap);

  // -- decisions lane, always visible --
  const decisions = items.filter((it) => it.kind === "decision");
  const openDec = decisions.filter((it) => it.status !== "decided");
  const lane = el("div", "aion-dec-lane");
  const lh = el("div", "aion-sec-label");
  lh.append(el("span", "aion-sec-title", "◇ Decisions"),
    el("span", "aion-sec-count", openDec.length + " open · " + (decisions.length - openDec.length) + " decided"));
  const decAdd = ghostInput("＋ decision", "aion-add", (v) =>
    aionPost("/api/aion/backlog/item", { kind: "decision", title: v }, "Decision added"));
  decAdd.classList.add("aion-sec-add");
  lh.append(decAdd);
  lane.append(lh);
  openDec.forEach((it) => lane.append(aionDecisionRow(it)));
  const decided = decisions.filter((it) => it.status === "decided");
  if (decided.length) {
    // the permanent log is long — keep the lane about what's OPEN
    const t = el("button", "aion-done-toggle", (aionDecidedOpen ? "▾" : "▸") + " decided · " + decided.length);
    t.onclick = () => { aionDecidedOpen = !aionDecidedOpen; renderAion(); };
    lane.append(t);
    if (aionDecidedOpen) decided.forEach((it) => lane.append(aionDecisionRow(it)));
  }
  list.append(lane);

  // -- tasks, grouped by owner (open count desc) --
  // A task marked done stays IN PLACE — struck through, one click to unmark —
  // until PUBLISH ships it (owner call 2026-08-09: an accidental check must be
  // reversible where it happened, never vanish). "Fresh" = checked this
  // session, or done on/after the last publish while the backlog is dirty.
  const tasks = items.filter((it) => it.kind === "task");
  const openTasks = tasks.filter((it) => it.status !== "done");
  const pub = aionCache.publish || {};
  // LOCAL date of the last publish — doneOn is a local date, and slicing the
  // UTC timestamp shifts an evening publish into tomorrow (hiding today's
  // checks — the exact regret window this exists for)
  let lastPubDate = "";
  if (pub.lastPublished) {
    const d = new Date(pub.lastPublished);
    if (!isNaN(d)) lastPubDate = d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
  }
  const backlogDirty = !!(pub.dirty || {}).backlog;
  const freshDone = (it) => aionFreshDone.has(it.id) ||
    (backlogDirty && lastPubDate && it.doneOn && it.doneOn >= lastPubDate);
  const doneInPlace = tasks.filter((it) => it.status === "done" && freshDone(it));
  const doneTasks = tasks.filter((it) => it.status === "done" && !freshDone(it));
  const people = {};
  (aionCache.people || []).forEach((p) => { people[p.initials] = p.name || ""; });
  const groups = {};
  const order = [];
  openTasks.concat(doneInPlace).forEach((it) => {
    const key = (it.owner || "").toUpperCase() || "—";
    if (!groups[key]) { groups[key] = []; order.push(key); }
    groups[key].push(it);
  });
  const openCount = (key) => groups[key].filter((it) => it.status !== "done").length;
  order.sort((a, b) => openCount(b) - openCount(a));
  order.forEach((key) => {
    const g = el("div", "aion-owner-group");
    const gh = el("div", "aion-owner-head");
    gh.append(el("span", "aion-owner-ini", key === "—" ? "UNASSIGNED" : key));
    const full = key.split("/").map((k) => people[k] || "").filter(Boolean).join(" · ");
    if (full) gh.append(el("span", "aion-owner-name", full));
    const meIni = ((typeof todosCache !== "undefined" && todosCache && todosCache.me) || "BA").toUpperCase();
    if (key.split("/").includes(meIni)) gh.append(el("span", "aion-owner-you", "· you"));
    gh.append(el("span", "aion-sec-count", String(openCount(key))));
    g.append(gh);
    groups[key].forEach((it) => g.append(aionTaskRow(it)));
    if (key !== "—") {
      g.append(ghostInput("＋ task for " + key, "aion-add", (v) =>
        aionPost("/api/aion/backlog/item", { kind: "task", title: v, owner: key }, "Task added → " + key)));
    }
    list.append(g);
  });
  list.append(ghostInput("＋ task", "aion-add", (v) =>
    aionPost("/api/aion/backlog/item", { kind: "task", title: v }, "Task added")));

  // -- done tasks, one quiet collapsible --
  if (doneTasks.length) {
    const t = el("button", "aion-done-toggle", (aionDoneOpen ? "▾" : "▸") + " done · " + doneTasks.length);
    t.onclick = () => { aionDoneOpen = !aionDoneOpen; renderAion(); };
    list.append(t);
    if (aionDoneOpen) doneTasks.forEach((it) => list.append(aionTaskRow(it)));
  }

  // phone (Rev 4): the sticky inspector column is display:none — the same
  // renderer fills a bottom sheet instead. Keyed open = re-fills in place on
  // every renderAion() (field saves re-render) without re-animating.
  if (window.mf && window.mf.phone()) {
    if (aionSelId) {
      window.mfSheet.open((b) => renderAionInspector(b, items), {
        key: "aion",
        onClose: () => { if (aionSelId) { aionSelId = null; renderAion(); } },
        reopen: () => { if (!els.aionView.hidden) renderAion(); }, // desktop restore
      });
    } else {
      window.mfSheet.closeIf("aion");
    }
  } else {
    renderAionInspector(insp, items);
  }
}

// statusChip per the color rules: IN PROGRESS = accent; alarmed OPEN = ink;
// unremarkable OPEN = base-50; DONE/DECIDED = base-40. Never amber/green.
function aionStatusChip(it) {
  let label, cls;
  const alarmed = aionAlarmed(it);
  if (it.kind === "decision") {
    if (it.status === "decided") { label = "DECIDED"; cls = "closed"; }
    else { label = "OPEN"; cls = alarmed ? "alarm" : "open"; }
  } else if (it.status === "done") { label = "DONE"; cls = "closed"; }
  else if (it.status === "in_progress") { label = "IN PROGRESS"; cls = "active"; }
  else { label = "OPEN"; cls = alarmed ? "alarm" : "open"; }
  return el("span", "aion-status " + cls, label);
}

function aionAlarmed(it) {
  if (it.status === "done" || it.status === "decided") return false;
  if (it.kind === "task") return !!it.due && it.due < isoToday();
  return /^\d{4}-\d{2}-\d{2}$/.test(it.neededBy || "") && it.neededBy < isoToday();
}

function aionSelect(it) {
  aionSelId = aionSelId === it.id ? null : it.id;
  renderAion();
}

function aionDecisionRow(it) {
  const decided = it.status === "decided";
  const row = el("div", "aion-dec-row" + (decided ? " decided" : "") + (aionSelId === it.id ? " sel" : ""));
  row.append(el("span", "aion-dec-glyph", "◇"));
  const main = el("div", "aion-main");
  main.append(el("div", "aion-dec-text", it.text));
  const bits = [];
  if (!decided && it.neededBy) bits.push("needed by " + it.neededBy);
  if (it.owner) bits.push("@" + it.owner);
  if (decided) bits.push("decided " + (it.decided || "") + (it.outcome ? " → " + it.outcome : ""));
  main.append(el("div", "aion-item-meta", bits.join(" · ")));
  row.append(main, aionStatusChip(it));
  row.onclick = () => aionSelect(it);
  return row;
}

// task row — 4-col grid: check · title-over-meta · rock (its own column) · chip
function aionTaskRow(it) {
  const done = it.status === "done";
  const alarmed = aionAlarmed(it);
  const row = el("div", "aion-task-row" + (done ? " done" : "") + (alarmed ? " alarm" : "") + (aionSelId === it.id ? " sel" : ""));
  const c = el("button", "aion-check" + (done ? " off" : ""), done ? "●" : "○");
  c.title = done ? "unmark — this stays in place until PUBLISH" : "mark done (stays here, unmarkable, until PUBLISH)";
  c.onclick = (e) => {
    e.stopPropagation();
    if (done) aionFreshDone.delete(it.id);
    else aionFreshDone.add(it.id); // hold it in place for the regret window
    aionPost("/api/aion/backlog/" + it.id + "/update", { status: done ? "open" : "done" });
  };
  row.append(c);
  const main = el("div", "aion-main");
  main.append(el("div", "aion-title", it.text));
  const bits = [];
  if (it.due && !done) bits.push((alarmed ? "● overdue " : "due ") + it.due);
  if (it.captured) bits.push(it.captured);
  const meta = el("div", "aion-item-meta", bits.join(" · "));
  if ((it.sources || []).length) {
    const src = el("a", "aion-src", " ⧉ " + it.sources[0]);
    src.title = "open source note";
    src.href = "#/note/" + encodeURIComponent(aionSourcePath(it.sources[0]));
    src.onclick = (e) => e.stopPropagation();
    meta.append(src);
  }
  main.append(meta);
  row.append(main);
  const rockTag = el("span", "aion-rock-tag" + (it.rock && !rockResolved(it.rock) ? " stale" : ""), it.rock ? rockLabel(it.rock) : "");
  if (it.rock && !rockResolved(it.rock)) rockTag.title = "closed/historic rock — reattach to a live rock";
  row.append(rockTag);
  // no status chip on tasks — the checkbox IS the state (open/in-progress
  // retired); alarm still reads as the ink left rule + weight
  row.onclick = () => aionSelect(it);
  return row;
}

// ---- the inspector (replaces the inline drawer — the list never reflows) ----
function renderAionInspector(insp, items) {
  const it = items.find((x) => x.id === aionSelId);
  if (!it) {
    insp.append(el("div", "aion-insp-empty", "select a row — edits save as you go"));
    return;
  }
  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", "Inspector"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { aionSelId = null; renderAion(); };
  head.append(x);
  insp.append(head);

  const patch = (set, msg) => aionPost("/api/aion/backlog/" + it.id + "/update", set, msg);

  // title — editable in place; a retitle re-derives the id, so selection clears
  const title = inputEl("");
  title.value = it.text;
  title.className = "aion-insp-title";
  const commitTitle = () => {
    const v = title.value.trim();
    if (v && v !== it.text) { aionSelId = null; patch({ title: v }); }
  };
  title.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") commitTitle();
    else if (ev.key === "Escape") { title.value = it.text; title.blur(); }
  });
  title.addEventListener("blur", commitTitle);
  insp.append(title);

  const field = (label, node) => {
    const f = el("div", "aion-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    insp.append(f);
  };

  const ownerTa = typeahead({ placeholder: "initials", initial: it.owner || "",
    suggest: (q, add, ta) => aionOwnerSuggest(q, add, {
      commit: (v) => { ta.commit(v); if (v !== it.owner) patch({ owner: v }); },
      input: ta.input,
    }) });
  field("owner", ownerTa.el);

  if (it.kind === "task") {
    const rockTa = typeahead({
      placeholder: "type to pick a rock…", initial: rockLabel(it.rock),
      suggest: (q, add, ta) => aionRockSuggest(q, add, ta, (id) => { if (id !== it.rock) patch({ rock: id }); }),
    });
    field("rock", rockTa.el);
    const due = inputEl("");
    due.type = "date"; due.value = it.due || ""; due.className = "pp-in";
    due.onchange = () => patch({ due: due.value });
    field("due", due);
    // (no status field — open/in-progress was a distinction without a
    // difference; the checkbox is the state. owner call 2026-08-09)
  } else {
    const nb = inputEl("");
    nb.type = "date"; nb.className = "pp-in";
    nb.value = /^\d{4}-\d{2}-\d{2}$/.test(it.neededBy || "") ? it.neededBy : "";
    nb.onchange = () => patch({ needed_by: nb.value });
    field("needed by", nb);
    if (it.status !== "decided") {
      // outcome + decide are ONE quiet control: type the outcome, press
      // Enter (or the small affordance that wakes with it) — no banner
      // button mid-panel (owner call 2026-08-12)
      const outcome = inputEl("what was decided…");
      outcome.className = "pp-in aion-insp-outcome";
      field("outcome", outcome);
      const decide = el("button", "aion-decide-inline", "decide ⏎");
      decide.title = "files to the permanent decision log (Enter in the outcome field does the same)";
      decide.disabled = true;
      const doDecide = () => {
        if (!outcome.value.trim()) return;
        aionSelId = null;
        aionPost("/api/aion/backlog/" + it.id + "/decide", { outcome: outcome.value.trim() }, "Decided — permanent log");
      };
      outcome.addEventListener("input", () => { decide.disabled = !outcome.value.trim(); });
      outcome.addEventListener("keydown", (ev) => { if (ev.key === "Enter") doDecide(); });
      decide.onclick = doDecide;
      insp.append(decide);
    } else if (it.outcome) {
      field("outcome", el("span", "aion-insp-ro", it.outcome));
    }
  }
  if (it.captured) field("captured", el("span", "aion-insp-ro", it.captured));
  field("kind", el("span", "aion-insp-ro", it.kind));

  if ((it.sources || []).length) {
    const src = el("a", "aion-insp-src", "⧉ " + it.sources[0]);
    src.href = "#/note/" + encodeURIComponent(aionSourcePath(it.sources[0]));
    insp.append(src);
  }
  // delete — arm-to-confirm, hard removal from backlog.md (owner's explicit
  // "remove this"; a done task is different — that stays until PUBLISH)
  const del = el("button", "aion-insp-del", "delete item");
  del.onclick = () => {
    const yes = el("button", "aion-insp-del armed", "delete — permanent?");
    yes.onclick = () => {
      aionSelId = null;
      aionPost("/api/aion/backlog/" + it.id + "/delete", {}, "Deleted");
    };
    del.replaceWith(yes);
    setTimeout(() => { if (yes.parentNode) yes.replaceWith(del); }, 2500);
  };
  insp.append(del);

  const foot = el("div", "aion-insp-foot");
  foot.append(el("span", "", "edits save as you go"));
  const raw = el("button", "aion-insp-raw", "⌘/ raw");
  raw.onclick = () => toggleRawOverlay();
  foot.append(raw);
  insp.append(foot);
}

// aionSourcePath resolves a source note name to a vault path: names already
// carrying a folder (log/\u2026) pass through; dated names default to log/.
function aionSourcePath(name) {
  if (name.includes("/")) return name + ".md";
  return (/^\d{4}-\d{2}-\d{2} /.test(name) ? "log/" : "") + name + ".md";
}

// aionRockLadder: the LIVE tether targets — every open goals aion rock plus its
// open child stages (flattened, parent-labelled). Used by the resolver/labeller
// and the picker so a rock consolidated into a stage under a parent stays a
// valid, selectable, non-stale tether (e.g. "Mechanism discovery › ICR go/no-go").
function aionRockLadder() {
  const area = aionCache && aionCache.goalsArea;
  return flattenRockLadder((area && area.rocks) || []);
}

// rockResolved: does this rock id name a LIVE goals aion rock or stage (vs a
// closed / historic rock that only survives in the archives)?
function rockResolved(id) {
  return !!aionRockLadder().find((r) => r.id === id);
}

function rockLabel(id) {
  if (!id) return "";
  const rock = aionRockLadder().find((r) => r.id === id);
  if (rock) return rock.text;
  // a closed/historic rock (not in live goals) — de-slug so the UI never shows
  // the raw "aion/<slug>"; it renders muted (see .aion-rock-tag.stale)
  return id.replace(/^aion\//, "").replace(/-/g, " ");
}

function aionOwnerSuggest(q, add, ta) {
  (aionCache.people || [])
    .filter((p) => !q || p.initials.toLowerCase().includes(q) || (p.name || "").toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((p) => add(p.initials + " \u00b7 " + (p.name || ""), "", () => { ta.commit(p.initials); ta.input.dispatchEvent(new Event("change")); }));
}

function aionRockSuggest(q, add, ta, onPick) {
  aionRockLadder()
    .filter((r) => !r.checked)
    .filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
    .slice(0, 8)
    .forEach((r) => add(r.label, "", () => { ta.commit(r.text); onPick(r.id, r.text); }));
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
  // reinforcement bar (§6): strength legible without counting — 8px per
  // source, capped at 56px
  const bar = el("span", "aion-heur-bar");
  bar.style.width = Math.min(n * 8, 56) + "px";
  if (!n) bar.style.display = "none";
  row.append(caret, text, bar, el("span", "aion-heur-count", n > 0 ? "×" + n : ""));

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
async function renderAionGoals(host) {
  const area = aionCache.goalsArea;
  if (!area) { host.append(emptyRow("no ## Aion area in goals.md yet")); return; }
  if (area.northStar) host.append(el("div", "aion-northstar", "✦ " + area.northStar));
  // EDITABLE (owner call 2026-08-12): the same outline as the GOALS tab,
  // mounted here for the Aion area. It reads the task substrate for the
  // tethered tasks, so refresh that first.
  try { todosCache = await (await fetch("/api/tasks")).json(); } catch (e) {}
  host.append(el("div", "aion-section-note",
    "editable — the GOALS-tab outline over ## Aion; milestones run in parallel"));
  const rocksHost = el("div", "go-content aion-goals-outline");
  (area.rocks || []).forEach((g) => rocksHost.append(rockOutline(g, area.name || "Aion")));
  host.append(rocksHost);
  // annuals stay a quiet read-only ladder below (chips carry the serves links)
  host.append(el("div", "pp-section-head", "1-YEAR LADDER"));

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
  (area.annuals || []).forEach((a) => host.append(chipRow(a, "annual")));
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

  // opts.noteLink (optional): row → vault note path — a matching row gets an
  // ↗ out to the person's note in the conscious repo (display-only; the
  // registry file itself never changes shape)
  const linked = typeof opts.noteLink === "function";
  const colsClass = opts.colsClass + (linked ? " cols-people-linked" : "");
  host.append(ppCols(colsClass, opts.cols.map((c) => c.label).concat([""])));
  const body = el("div", "aion-table");
  host.append(body);
  const renderRows = () => {
    body.innerHTML = "";
    rows.forEach((row, i) => {
      const line = el("div", "aion-row " + colsClass);
      opts.cols.forEach((c) => {
        const input = inputEl(c.label.toLowerCase());
        input.value = row[c.key] || "";
        input.oninput = () => { row[c.key] = input.value; bar.mark(); };
        line.append(input);
      });
      const acts = el("span", "aion-row-acts");
      const notePath = linked ? opts.noteLink(row) : null;
      if (notePath) {
        const link = el("button", "aion-mini", "↗");
        link.title = "open " + notePath;
        link.onclick = () => { _noteReturn = "#/aion"; openNoteByPath(notePath); };
        acts.append(link);
      }
      const x = el("button", "aion-mini", "✕");
      x.title = "remove row";
      x.onclick = () => { rows.splice(i, 1); bar.mark(); renderRows(); };
      acts.append(x);
      if (linked) line.append(acts); else { acts.remove(); line.append(x); }
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
  // §5: the four stacked tables become a 176px registry rail + ONE table at a
  // time, keeping the aionTableEditor semantics and the sticky dirty bar.
  const wrap = el("div", "aion-org");
  const rail = el("div", "aion-org-rail");
  const pane = el("div", "aion-org-pane");
  wrap.append(rail, pane);
  host.append(wrap);

  rail.append(el("div", "aion-org-label", "Registries"));
  [["people", "People", (aionCache.people || []).length],
   ["hiring", "Hiring", (aionCache.hiring || []).length],
   ["references", "References", (aionCache.references || []).length],
   ["finances", "Finances", null]].forEach(([key, label, n]) => {
    const b = el("button", "aion-org-item" + (aionOrgSel === key ? " active" : ""));
    b.append(el("span", "", label));
    if (n !== null) b.append(el("span", "aion-org-count", String(n)));
    b.onclick = () => { aionOrgSel = key; renderAion(); };
    rail.append(b);
  });
  const rel = { people: "system/aion/people.md", hiring: "system/aion/hiring.md",
    references: "system/aion/references.md", finances: "system/aion/finances.md" }[aionOrgSel];
  const fileBox = el("div", "aion-org-file");
  fileBox.append(el("div", "aion-org-label", "File"), el("div", "aion-org-path", rel));
  const rawBtn = el("button", "aion-org-raw", "⌘/ edit raw");
  rawBtn.onclick = () => openRawOverlay(rel);
  fileBox.append(rawBtn);
  rail.append(fileBox);

  if (aionOrgSel === "people") {
    // rows whose name matches a person note in the vault link out to it —
    // same mechanism as the RE partners table
    contactNoteIndex().then((idx) => aionTableEditor(pane, {
      title: "PEOPLE", rel: "system/aion/people.md",
      colsClass: "cols-aion-people",
      cols: [{ key: "initials", label: "INITIALS" }, { key: "name", label: "NAME" }, { key: "role", label: "ROLE" }],
      rows: aionCache.people || [],
      put: "/api/aion/people", payloadKey: "people",
      addLabel: "person", newRow: (v) => ({ initials: v.toUpperCase().slice(0, 3), name: "", role: "" }),
      noteLink: (row) => idx[(row.name || "").toLowerCase()] || null,
    }));
  } else if (aionOrgSel === "hiring") {
    aionTableEditor(pane, {
      title: "HIRING", rel: "system/aion/hiring.md",
      colsClass: "cols-aion-hiring",
      cols: [{ key: "role", label: "ROLE" }, { key: "candidate", label: "CANDIDATE" },
        { key: "stage", label: "STAGE" }, { key: "priority", label: "PRI" }],
      rows: aionCache.hiring || [],
      put: "/api/aion/hiring", payloadKey: "items",
      addLabel: "role", newRow: (v) => ({ role: v, candidate: "", stage: "", priority: "" }),
    });
  } else if (aionOrgSel === "references") {
    aionTableEditor(pane, {
      title: "REFERENCES", rel: "system/aion/references.md",
      colsClass: "cols-aion-refs",
      cols: [{ key: "text", label: "TITLE" }, { key: "url", label: "URL" },
        { key: "source", label: "SOURCE" }, { key: "date", label: "DATE" }],
      rows: aionCache.references || [],
      put: "/api/aion/references", payloadKey: "references",
      addLabel: "reference", newRow: (v) => ({ text: v, url: "", source: "", date: "" }),
    });
  } else {
    // FINANCES — key/value rows + the derived read-only runway line
    pane.append(el("div", "pp-section-head", "FINANCES"));
    const fin = { ...(aionCache.finances || {}) };
    const finBar = makeDirtyBar(els.aionView, async () => {
      await putJSON("/api/aion/finances", fin);
      showToast("Saved — finances.md frontmatter updated");
      loadAion();
    }, () => renderAion());
    const finHost = el("div", "aion-fin");
    pane.append(finHost);
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
    pane.append(el("div", "aion-section-note",
      "these fields publish to the portal (finances.json) on PUBLISH — capital & burn take 1.95M / 85k / $2,480,000 shorthand"));
  }
}

