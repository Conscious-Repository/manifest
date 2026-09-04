// ================= Agents · RUNS (the global run log) =================
// Split from 40-spirits.js (phase 0); made legible in phase 3 (agents plan
// §4.3 RUNS): a 7-day window grouped by day, an `internal` chip that hides the
// sink-driven spirits, a why-line on every row, duration + cost figures, the
// failing step auto-expanded in the trace, and a "deliverable" link on the
// report when the run wrote a feed card or a library brief. Phase 4 merges
// the alfred runtime in: Hermes cron fires (usage_audit.jsonl joined to the
// output file) and manifest's ledger run.* turns, from
// /api/agents/hermes/runs — tokens instead of dollars unless the chargebook
// prices the model, the fire's Error/Response first line as the why, and a
// week-spend header split by runtime. `spiritRuns` / `openRunId` (the only
// run state) and the live poll stay in 40-agents.js; `hermesRuns` lives in
// 41-agents-schedule.js (the board's strip reads it too).

// ---- run reports (artifacts/runs/) — live strip + finished list ----
async function loadSpiritRuns() {
  const [runs, hr] = await Promise.all([fetchSpiritRuns(), fetchHermesRuns(spRunWindow)]);
  spiritRuns = runs;
  hermesRuns = hr.data; hermesRunsDegraded = hr.degraded;
  renderSpiritRuns();
  ensureLivePoll();
}
// spiritWeekSpend — Σ spentUsd over excalibur runs started in the last 7 days
// (the section head + crumb meta both read this; no endpoint involved).
function spiritWeekSpend() {
  const cutoff = Date.now() - 7 * 86400000;
  let sum = 0;
  (spiritRuns.data || []).forEach((r) => {
    const t = new Date(r.started).getTime();
    if (!isNaN(t) && t >= cutoff) sum += r.spentUsd || 0;
  });
  return sum;
}
// hermesWeekFigures — the alfred side of the header: priced dollars (only
// where the chargebook prices the model) and raw tokens, last 7 days.
function hermesWeekFigures() {
  const cutoff = Date.now() - 7 * 86400000;
  let usd = 0, tokens = 0, unknown = 0;
  (hermesRuns || []).forEach((f) => {
    const t = new Date(f.started).getTime();
    if (isNaN(t) || t < cutoff) return;
    if (f.usd != null) usd += f.usd;
    if (f.tokens != null) tokens += f.tokens; else if (f.source !== "ledger") unknown++;
  });
  return { usd, tokens, unknown };
}
function fmtTokens(n) {
  if (n == null) return "?";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return Math.round(n / 1e3) + "k";
  return String(n);
}
// hermesRunRows — the alfred fires in the log's row shape (spirit · ritual ·
// outcome · outcomeDetail · started · spentUsd), the fire itself as `hermes`.
function hermesRunRows() {
  return (hermesRuns || []).map((f) => ({
    id: f.id, hermes: f, runtime: "alfred", harness: "alfred", spirit: "alfred",
    ritual: f.jobName || f.job || "turn",
    outcome: f.outcome || "unknown", outcomeDetail: f.why || "",
    started: f.started, finished: f.finished || "",
    spentUsd: f.usd != null ? f.usd : 0,
    itemsWritten: f.itemsWritten != null ? f.itemsWritten : 0,
  }));
}

// ---- the log's view state (derived filters; nothing persists) ----
let spRunWindow = "7d";        // 7d | 30d | all — the default is the week (§3.2)
let spRunInternal = false;     // OFF hides the sink-driven spirits + chat turns
let spRunFilterSpirit = "";
let spRunFilterOutcome = "";   // "" | error | stopped
const runDayOpen = {};         // day carets survive a repaint (all open by default)

// runIsInternal — the log's noise rule, derived from names alone (plan §4.3):
// extractor/* and sage/* are spooled by the transcript sinks and the / bar;
// kairos/zeck are the team-chat spirits whose turns are not "runs" a person
// scheduled. Retired extractor spirits (re-extractor, aion-extractor) count.
function runIsInternal(r) {
  if (r.hermes) return false; // a person scheduled the cron job or asked for the dig
  const sp = r.spirit || "";
  if (sp === "extractor" || sp === "sage" || sp === "kairos" || sp === "zeck") return true;
  if (/-extractor$/.test(sp)) return true;
  return (r.ritual || "") === "chat";
}
function runWindowCutoff() {
  if (spRunWindow === "7d") return Date.now() - 7 * 86400000;
  if (spRunWindow === "30d") return Date.now() - 30 * 86400000;
  return 0; // all — no recency cap
}

// Re-renders the LIST only; never touches the open report detail (so a live
// re-render doesn't close what you're reading).
function renderSpiritRuns() {
  const host = els.spiritRunsList; host.innerHTML = "";
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  const queued = spiritRuns.queued || [];
  // excalibur reports ∪ alfred fires, newest first (both arrive newest-first)
  const finished = (spiritRuns.data || []).filter((r) => r.outcome !== "running").concat(hermesRunRows())
    .sort((a, b) => String(b.started || "").localeCompare(String(a.started || "")));

  // the live strip: one line per running/queued item, at the very top (§12)
  const liveHost = document.getElementById("spiritLive");
  if (liveHost) {
    liveHost.innerHTML = "";
    running.forEach((r) => liveHost.append(liveRunRow(r, true)));
    queued.forEach((q) => liveHost.append(liveRunRow(q, false)));
  }

  // week spend — the 7-day total split by runtime (§4.3): dollars per
  // runtime, tokens for alfred (dollars only where the chargebook prices the model)
  const ws = document.getElementById("spiritWeekSpend");
  if (ws) {
    const hf = hermesWeekFigures();
    const bits = ["$" + spiritWeekSpend().toFixed(2) + " excalibur", "$" + hf.usd.toFixed(2) + " alfred", fmtTokens(hf.tokens) + " tokens alfred"];
    if (hf.unknown) bits.push(hf.unknown + " fire" + (hf.unknown === 1 ? "" : "s") + " untallied");
    ws.textContent = bits.join(" · ") + " · last 7 days";
    ws.title = hf.unknown ? "untallied: fires with no usage_audit line (a drift skip makes no inference call; or the audit file is missing)" : "";
  }
  if (typeof updateSpiritsCrumb === "function") updateSpiritsCrumb();

  // the schedule board also calls this for the live strip; the log itself
  // only paints when RUNS is the open view
  const wrap = document.getElementById("spRunsWrap");
  if (wrap && wrap.hidden) return;
  // what the alfred projection could not read (D4) — one quiet line, never an error
  (hermesRunsDegraded || []).forEach((n) => host.append(el("div", "runs-degraded", "alfred: " + n)));

  // window → internal → spirit → outcome; chips derive from what survives
  // the first two so no chip ever filters to nothing
  const cutoff = runWindowCutoff();
  const inWindow = finished.filter((r) => {
    if (!cutoff) return true;
    const t = new Date(r.started).getTime();
    return isNaN(t) || t >= cutoff;
  });
  const visible = spRunInternal ? inWindow : inWindow.filter((r) => !runIsInternal(r));
  renderRunFilters(visible, inWindow.length - visible.length);
  let list = visible;
  if (spRunFilterSpirit) list = list.filter((r) => r.spirit === spRunFilterSpirit);
  if (spRunFilterOutcome === "error") list = list.filter((r) => (r.outcome || "").startsWith("error"));
  if (spRunFilterOutcome === "stopped") list = list.filter((r) => (r.outcome || "").startsWith("stopped"));
  if (!list.length) {
    if (running.length || queued.length) return; // the live strip is the content
    host.appendChild(emptyRow(
      !finished.length ? "No runs yet — cast a skill (press /) or wait for a scheduled ritual."
      : visible.length ? "No runs match the filter."
      : inWindow.length ? "Only internal runs in this window — turn on internal to see them."
      : spRunWindow === "all" ? "No runs match the filter."
      : "No runs in the last " + spRunWindow.replace("d", " days") + " — widen to 30d or all."));
    return;
  }
  // grouped by day, newest day first (the list arrives newest-first)
  const days = [];
  const byDay = {};
  list.forEach((r) => {
    const k = runDayKey(r.started);
    if (!byDay[k]) { byDay[k] = []; days.push(k); }
    byDay[k].push(r);
  });
  days.forEach((k) => runDayGroup(host, k, byDay[k]));
}

// runDayKey — the local calendar day of the start timestamp; unparsable
// timestamps group under the report's own date prefix.
function runDayKey(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return String(iso || "").slice(0, 10) || "undated";
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate());
}
function runDayLabel(key) {
  const today = runDayKey(new Date().toISOString());
  if (key === today) return "TODAY";
  const y = new Date(); y.setDate(y.getDate() - 1);
  if (key === runDayKey(y.toISOString())) return "YESTERDAY";
  const d = new Date(key + "T12:00:00");
  if (isNaN(d)) return key.toUpperCase();
  return d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" }).replace(",", "").toUpperCase();
}
// runDayGroup — the schedule board's .aion-sec-label heading (caret · day ·
// count · the day's spend + failures) over the day's rows.
function runDayGroup(host, key, list) {
  if (!(key in runDayOpen)) runDayOpen[key] = true;
  const head = el("div", "aion-sec-label sched-group run-day");
  const caret = el("span", "sec-caret", runDayOpen[key] ? "▾" : "▸");
  head.append(caret, el("span", "aion-sec-title", runDayLabel(key)), el("span", "aion-sec-count", String(list.length)));
  const spent = list.reduce((s, r) => s + (r.spentUsd || 0), 0);
  const failed = list.filter((r) => (r.outcome || "").startsWith("error")).length;
  const tokens = list.reduce((s, r) => s + ((r.hermes && r.hermes.tokens) || 0), 0);
  const bits = ["$" + spent.toFixed(2)];
  if (tokens) bits.push(fmtTokens(tokens) + " tokens");
  if (failed) bits.push(failed + " failed");
  head.append(el("span", "sched-group-note", bits.join(" · ")));
  const body = el("div", "runs-day-body");
  body.hidden = !runDayOpen[key];
  head.onclick = () => {
    runDayOpen[key] = !runDayOpen[key];
    body.hidden = !runDayOpen[key];
    caret.textContent = runDayOpen[key] ? "▾" : "▸";
  };
  list.forEach((r) => body.append(spiritRunRow(r)));
  host.append(head, body);
}

// liveRunRow — ONE line (prototype): dot · RUNNING · spirit · ritual — request · elapsed
function liveRunRow(item, running) {
  const row = el("div", "sprt-live" + (running ? " running" : ""));
  row.append(el("span", "live-dot " + (running ? "on" : "wait")));
  row.append(el("span", "sprt-live-state", running ? "running" : "queued"));
  if (item.harness) row.append(el("span", "harness-chip", item.harness));
  row.append(el("span", "sprt-live-text",
    item.spirit + " · " + item.ritual + (item.request ? " — " + item.request : "")));
  if (running) {
    row.append(el("span", "sprt-live-elapsed", elapsedSince(item.started)));
    row.classList.add("clicky");
    row.onclick = () => openSpiritRun(item.id);
    row.title = "watch the report append";
  }
  return row;
}
function elapsedSince(iso) {
  const d = new Date(iso); if (isNaN(d)) return "";
  return fmtRunSeconds(Math.max(0, Math.round((Date.now() - d.getTime()) / 1000)));
}
function fmtRunSeconds(s) {
  const h = Math.floor(s / 3600); s -= h * 3600;
  const m = Math.floor(s / 60); s = s % 60;
  if (h) return `${h}h ${String(m).padStart(2, "0")}m`;
  return m ? `${m}m ${s}s` : `${s}s`;
}
// runDuration — derived from started/finished per paint; no stored state.
function runDuration(r) {
  if (r.hermes && r.hermes.durationMs != null) return fmtRunSeconds(Math.max(0, Math.round(r.hermes.durationMs / 1000)));
  const a = new Date(r.started), b = new Date(r.finished);
  if (isNaN(a) || isNaN(b)) return "";
  return fmtRunSeconds(Math.max(0, Math.round((b - a) / 1000)));
}
// runWhy — the first line of "## Outcome" without its "<outcome> — " echo.
function runWhy(r) {
  let why = r.outcomeDetail || "";
  const oc = r.outcome || "";
  if (oc && why.startsWith(oc + " — ")) why = why.slice(oc.length + 3);
  else if (oc && why === oc) why = "";
  return why.trim();
}
function outcomeClass(outcome) {
  const oc = outcome || "";
  let cls = "oc-" + oc.replace(/[^a-z-]/g, "");
  if (oc.startsWith("error")) cls += " oc-error";
  return cls;
}

// ---- the filters: 7d · 30d · all │ all · <spirit>… · error · stopped │ internal ----
// (a ceiling stop is the system working — STOPPED folds stopped-charge/-steps)
function renderRunFilters(visible, hiddenInternal) {
  const host = document.getElementById("spiritRunFilters");
  if (!host) return;
  host.innerHTML = "";
  const chip = (label, on, cb, title) => {
    const b = el("button", "cadb-chip" + (on ? " on" : ""), label);
    b.onclick = cb;
    if (title) b.title = title;
    host.append(b);
  };
  // a window change refetches the alfred fires for that window (the
  // excalibur list is already complete; the fires are read per window)
  ["7d", "30d", "all"].forEach((w) =>
    chip(w, spRunWindow === w, () => { spRunWindow = w; loadSpiritRuns(); },
      w === "all" ? "every run report, no recency cap" : "runs started in the last " + w.replace("d", " days")));
  host.append(el("span", "run-filter-sep"));
  chip("all", !spRunFilterSpirit && !spRunFilterOutcome, () => { spRunFilterSpirit = ""; spRunFilterOutcome = ""; renderSpiritRuns(); });
  const spirits = new Set(visible.map((r) => r.spirit));
  if (spRunFilterSpirit) spirits.add(spRunFilterSpirit); // the picked chip stays until unpicked
  [...spirits].sort().forEach((sp) =>
    chip(sp, spRunFilterSpirit === sp, () => { spRunFilterSpirit = spRunFilterSpirit === sp ? "" : sp; renderSpiritRuns(); }));
  chip("error", spRunFilterOutcome === "error", () => { spRunFilterOutcome = spRunFilterOutcome === "error" ? "" : "error"; renderSpiritRuns(); });
  chip("stopped", spRunFilterOutcome === "stopped", () => { spRunFilterOutcome = spRunFilterOutcome === "stopped" ? "" : "stopped"; renderSpiritRuns(); });
  host.append(el("span", "run-filter-sep"));
  chip("internal" + (!spRunInternal && hiddenInternal ? " · " + hiddenInternal : ""), spRunInternal,
    () => { spRunInternal = !spRunInternal; if (!spRunInternal && spRunFilterSpirit && runIsInternal({ spirit: spRunFilterSpirit })) spRunFilterSpirit = ""; renderSpiritRuns(); },
    "extractor/*, sage/* and team-chat turns — run by the transcript sinks and the / bar, not by you");
}

// parseRunSteps — the client-side step trace over the report's stable shape
// ("### Step N — <cast>" + rationale/args/result/summary bullets + the charge
// ledger table). Returns [] on any miss → callers fall back to the full body.
// Each step also carries its raw block and whether its result FAILED, so the
// trace can open the failing step expanded (plan §3.2).
function parseRunSteps(body) {
  const steps = [];
  const re = /^### Step (\d+) — (.+)$/gm;
  let m;
  while ((m = re.exec(body))) {
    const start = m.index + m[0].length;
    const next = body.slice(start).search(/^### |^## /m);
    const block = body.slice(start, next < 0 ? undefined : start + next);
    const line = (key) => {
      const mm = block.match(new RegExp("^- " + key + ": (.*)$", "m"));
      return mm ? mm[1].trim() : "";
    };
    const result = line("result");
    steps.push({
      n: parseInt(m[1], 10), cast: m[2].trim(),
      detail: result || line("summary") || line("rationale"),
      failed: /^FAILED\b/.test(result),
      block: block.trim(),
    });
  }
  if (!steps.length) return [];
  // join per-step cost from the charge-ledger table
  const costs = {};
  const lg = body.match(/## Charge ledger\n([\s\S]*?)(\n## |$)/);
  if (lg) {
    lg[1].split("\n").forEach((ln) => {
      const c = ln.split("|").map((x) => x.trim());
      if (c.length >= 6 && /^\d+$/.test(c[1])) costs[parseInt(c[1], 10)] = c[4];
    });
  }
  steps.forEach((s) => { s.usd = costs[s.n] || ""; });
  return steps;
}
// parseRunWrites — the "## Writes" list; the report's own line is dropped.
function parseRunWrites(body) {
  const m = (body || "").match(/\n## Writes\n([\s\S]*?)(\n## |$)/);
  if (!m) return [];
  return m[1].split("\n")
    .map((ln) => ln.replace(/^- /, "").trim())
    .filter((ln) => ln && !/\(this report\)$/.test(ln))
    .map((ln) => ln.replace(/\s+\(.*\)$/, ""));
}
// runDeliverables — feed cards + library briefs the run wrote (the things a
// person reads; approvals and memories are not deliverables).
function runDeliverables(body) {
  return parseRunWrites(body).filter((p) => /^artifacts\/(feed|library)\/[^/]+\.md$/.test(p));
}

// spiritRunRow — one log line (plan §4.3): outcome word · runtime chip ·
// agent / job · figures (wrote · duration · $cost) · when, then the WHY line
// (the first line of "## Outcome") on every run that has one. The charge bar
// only paints when something was actually spent — an empty bar says nothing.
// Click expands the step trace inline; "open full report" keeps the prompt
// affordance behind the detail pane.
function spiritRunRow(r) {
  const row = el("div", "sprt-run");
  const top = el("div", "sprt-run-top");
  top.append(el("span", "run-outcome " + outcomeClass(r.outcome), r.outcome || "never run"));
  const chip = el("span", "harness-chip" + (r.hermes ? " alfred" : ""), r.harness || "excalibur"); // runtime
  if (r.hermes) chip.title = r.hermes.source === "ledger" ? "an in-process Hermes turn (manifest's ledger)" : "Hermes cron fire · " + (r.hermes.source || "") + (r.hermes.model ? " · " + r.hermes.model : "");
  top.append(chip);
  top.append(el("span", "sprt-run-title", r.hermes ? `agent:alfred / ${r.ritual}` : `${r.spirit} / ${r.ritual}`));
  const figs = el("span", "sprt-run-figs");
  figs.append(el("span", "sprt-run-wrote", r.itemsWritten ? "wrote " + r.itemsWritten : "—"));
  const dur = runDuration(r);
  if (dur) figs.append(el("span", "sprt-run-dur", dur));
  if (r.hermes) {
    // tokens are the honest figure; dollars only when the chargebook prices the model
    const f = r.hermes;
    const cost = el("span", "sprt-run-cost", f.usd != null ? "$" + f.usd.toFixed(4) : f.tokens != null ? fmtTokens(f.tokens) + " tokens" : "tokens unknown");
    cost.title = f.tokens != null ? (f.promptTokens || 0) + " in · " + (f.completionTokens || 0) + " out" + (f.usd != null ? " · priced by the chargebook" : " · " + (f.model || "model") + " not priced in chargebook.md")
      : (f.source === "ledger" ? "an in-process turn — no token count" : "no usage_audit line for this fire");
    figs.append(cost);
  } else {
    figs.append(el("span", "sprt-run-cost", "$" + (r.spentUsd || 0).toFixed(4)));
  }
  top.append(figs);
  top.append(el("span", "sprt-run-when", fmtWhen(r.started)));
  row.append(top);
  const why = runWhy(r);
  if (why) { const w = el("div", "sprt-run-why", why); w.title = why; row.append(w); }
  if (r.spentUsd > 0 && r.ceilingUsd > 0) {
    const pct = Math.min(100, Math.round((r.spentUsd / r.ceilingUsd) * 100));
    const bar = el("span", "charge-bar");
    const fill = el("span", "charge-fill" + (pct >= 100 ? " over" : ""));
    fill.style.width = pct + "%";
    bar.appendChild(fill);
    const cr = el("div", "charge-row");
    cr.append(bar, el("span", "charge-label", `$${r.spentUsd.toFixed(4)} / $${r.ceilingUsd.toFixed(2)}`));
    row.append(cr);
  }
  row.onclick = () => toggleRunTrace(row, r);
  return row;
}

// toggleRunTrace — inline expand: the parsed step trace (fallback: full body).
// Failed steps open with their full block showing and the first one scrolls
// into view; any other step's block toggles on click.
async function toggleRunTrace(row, r) {
  const open = row.nextElementSibling && row.nextElementSibling.classList.contains("run-trace");
  if (open) { row.nextElementSibling.remove(); return; }
  if (r.hermes) { row.after(await hermesFireTrace(r)); return; }
  let body = "";
  try { body = ((await (await fetch("/api/spirits/runs/" + encodeURIComponent(r.id))).json()) || {}).body || ""; }
  catch (e) {}
  const box = el("div", "run-trace");
  box.onclick = (e) => e.stopPropagation(); // reading the trace never collapses it
  const steps = parseRunSteps(body);
  let firstFailed = null;
  if (steps.length) {
    steps.forEach((s) => {
      const ln = el("div", "run-trace-step" + (s.failed ? " failed" : ""));
      ln.append(el("span", "run-trace-n", String(s.n)));
      ln.append(el("span", "run-trace-cast", s.cast));
      ln.append(el("span", "run-trace-detail", s.detail || ""));
      if (s.usd) ln.append(el("span", "run-trace-usd", "$" + s.usd));
      const block = el("pre", "run-report run-trace-block");
      block.textContent = s.block;
      block.hidden = !s.failed;
      ln.onclick = () => { block.hidden = !block.hidden; };
      ln.title = block.hidden ? "show this step's rationale · args · result" : "";
      box.append(ln, block);
      if (s.failed && !firstFailed) firstFailed = ln;
    });
  } else if (body) {
    const pre = el("pre", "run-report");
    pre.textContent = body;
    box.append(pre);
  } else {
    box.append(emptyRow("No report yet."));
  }
  // a run that died before its first decision still says why, up front
  const why = runWhy(r);
  if (why && (!steps.length || (r.outcome || "").startsWith("error") || (r.outcome || "").startsWith("stopped"))) {
    box.prepend(el("div", "run-trace-why " + outcomeClass(r.outcome), (r.outcome || "") + " — " + why));
  }
  const full = pillLight("open full report", () => openSpiritRun(r.id));
  box.append(full);
  row.after(box);
  if (firstFailed) firstFailed.scrollIntoView({ behavior: "smooth", block: "nearest" });
}
// hermesFireTrace — the inline detail for an alfred row: the why up front,
// then the fire's narration from its output file (the Response or Error
// section first; the prompt dump stays behind a button — level two). A
// ledger turn has no file, so it shows its ledger line only.
async function hermesFireTrace(r) {
  const f = r.hermes;
  const box = el("div", "run-trace");
  box.onclick = (e) => e.stopPropagation();
  const why = r.outcomeDetail || "";
  if (why) box.append(el("div", "run-trace-why " + outcomeClass(r.outcome), (r.outcome || "") + " — " + why));
  const meta = [f.model ? "model " + f.model : "", f.tokens != null ? fmtTokens(f.tokens) + " tokens" : "", f.job ? "job " + f.job : "", f.source ? "source " + f.source : ""].filter(Boolean);
  if (meta.length) box.append(el("div", "sprt-run-why", meta.join(" · ")));
  if (!f.job || !f.file) {
    box.append(emptyRow(f.source === "ledger" ? "An in-process Hermes turn — manifest's ledger line is the whole record." : "No output file for this fire (usage_audit line only)."));
    return box;
  }
  let d = null;
  try { d = await (await fetch("/api/agents/hermes/run?job=" + encodeURIComponent(f.job) + "&file=" + encodeURIComponent(f.file))).json(); } catch (e) {}
  const body = (d && d.body) || "";
  if (!body) { box.append(emptyRow("Output file unreadable" + (d && d.why ? " — " + d.why : "") + ".")); return box; }
  // the narration: everything from the first Response / Error heading; the prompt is level two
  const cut = body.search(/^## (Response|Error|Output)\b/m);
  const pre = el("pre", "run-report");
  pre.textContent = cut >= 0 ? body.slice(cut) : body;
  box.append(pre);
  if (cut > 0) {
    const promptBtn = pillLight("show prompt", () => {
      const showing = pre.textContent.length === body.length;
      pre.textContent = showing ? body.slice(cut) : body;
      promptBtn.textContent = showing ? "show prompt" : "hide prompt";
    });
    box.append(promptBtn);
  }
  return box;
}
async function openSpiritRun(id) {
  openRunId = id;
  let run;
  try { run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(id))).json(); }
  catch (e) { return; }
  const host = els.spiritRunDetail; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "run-detail-head");
  head.append(el("span", "run-title", id));
  const promptBtn = pillLight("Show assembled prompt", () => toggleSpiritPrompts(id, promptBtn));
  const closeBtn = pillLight("✕ Close", () => { host.hidden = true; openRunId = null; });
  head.append(promptBtn, closeBtn);
  host.append(head);
  // the deliverable first (plan §4.3): the feed card / library brief the run
  // wrote, opened through openResult — the narration below is level two
  const outs = runDeliverables(run.body || "");
  if (outs.length) {
    const box = el("div", "run-deliverables");
    box.append(el("span", "cadb-label", outs.length === 1 ? "deliverable" : "deliverables"));
    outs.forEach((p) => {
      const b = pillLight(p.replace(/^artifacts\//, "") + " →",
        () => openResult({ artifactRef: p, harness: run.harness || "" }, artifactTitleFromRef(p)));
      box.append(b);
    });
    host.append(box);
  }
  const body = el("pre", "run-report"); body.id = "runReportBody";
  body.textContent = run.body || "";
  host.append(body);
  const prompts = el("div", "run-prompts"); prompts.id = "runPrompts-" + id; prompts.hidden = true;
  host.append(prompts);
  host.scrollIntoView({ behavior: "smooth", block: "start" });
  ensureLivePoll(); // a running report will keep appending
}
// Refresh the open report body in place. Called each poll tick (which only runs
// while a run is active), so the finishing tick pulls the terminal report too.
async function refreshOpenRun() {
  if (!openRunId) return;
  try {
    const run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(openRunId))).json();
    const body = document.getElementById("runReportBody");
    if (body) body.textContent = run.body || "";
  } catch (e) {}
}
// The §6.5 affordance: the EXACT model input per turn, preserved verbatim.
async function toggleSpiritPrompts(id, btn) {
  const box = document.getElementById("runPrompts-" + id);
  if (!box) return;
  if (!box.hidden) { box.hidden = true; btn.textContent = "Show assembled prompt"; return; }
  if (!box.childElementCount) {
    let turns = [];
    try { turns = (await (await fetch("/api/spirits/runs/" + encodeURIComponent(id) + "/prompt")).json()).data || []; }
    catch (e) {}
    if (!turns.length) { box.appendChild(emptyRow("No preserved prompts found for this run.")); }
    turns.forEach((t) => {
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — SYSTEM`));
      const s = el("pre", "run-report prompt"); s.textContent = t.system; box.appendChild(s);
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — USER`));
      const u = el("pre", "run-report prompt"); u.textContent = t.user; box.appendChild(u);
    });
  }
  box.hidden = false; btn.textContent = "Hide assembled prompt";
}
