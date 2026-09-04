// ================= Agents · RUNS (the global run log) =================
// Split from 40-spirits.js (phase 0): the finished list, its filters, the inline
// step trace, the full report and the assembled-prompt viewer. `spiritRuns` /
// `openRunId` (the only run state) and the live poll stay in 40-agents.js.

// ---- run reports (artifacts/runs/) — live strip + finished list ----
async function loadSpiritRuns() {
  spiritRuns = await fetchSpiritRuns();
  renderSpiritRuns();
  ensureLivePoll();
}
// spiritWeekSpend — Σ spentUsd over runs started in the last 7 days (the
// section head + crumb meta both read this; no endpoint involved).
function spiritWeekSpend() {
  const cutoff = Date.now() - 7 * 86400000;
  let sum = 0;
  (spiritRuns.data || []).forEach((r) => {
    const t = new Date(r.started).getTime();
    if (!isNaN(t) && t >= cutoff) sum += r.spentUsd || 0;
  });
  return sum;
}

// Re-renders the LIST only; never touches the open report detail (so a live
// re-render doesn't close what you're reading).
function renderSpiritRuns() {
  const host = els.spiritRunsList; host.innerHTML = "";
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  const queued = spiritRuns.queued || [];
  const finished = (spiritRuns.data || []).filter((r) => r.outcome !== "running");

  // the live strip: one line per running/queued item, at the very top (§12)
  const liveHost = document.getElementById("spiritLive");
  if (liveHost) {
    liveHost.innerHTML = "";
    running.forEach((r) => liveHost.append(liveRunRow(r, true)));
    queued.forEach((q) => liveHost.append(liveRunRow(q, false)));
  }

  const ws = document.getElementById("spiritWeekSpend");
  if (ws) { const n = spiritWeekSpend(); ws.textContent = n > 0 ? "$" + n.toFixed(2) + " this week" : ""; }
  if (typeof updateSpiritsCrumb === "function") updateSpiritsCrumb();

  // the RUNS view is the one global log (SPIRITS.md §4): every run, newest
  // first, filterable by spirit and outcome — no recency cap
  renderRunFilters(finished);
  let list = finished;
  if (spRunFilterSpirit) list = list.filter((r) => r.spirit === spRunFilterSpirit);
  if (spRunFilterOutcome === "error") list = list.filter((r) => r.outcome === "error");
  if (spRunFilterOutcome === "stopped") list = list.filter((r) => (r.outcome || "").startsWith("stopped"));
  if (!list.length && !running.length && !queued.length) {
    host.appendChild(emptyRow(finished.length
      ? "No runs match the filter."
      : "No runs yet — cast a skill (press /) or wait for a scheduled ritual."));
    return;
  }
  list.forEach((r) => host.append(spiritRunRow(r)));
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
  let s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  const m = Math.floor(s / 60); s = s % 60;
  return m ? `${m}m ${s}s` : `${s}s`;
}

// ---- the runs-view filters: ALL · one chip per spirit · ERROR · STOPPED ----
// (a ceiling stop is the system working — STOPPED folds stopped-charge/-steps)
let spRunFilterSpirit = "";
let spRunFilterOutcome = "";
function renderRunFilters(finished) {
  const host = document.getElementById("spiritRunFilters");
  if (!host) return;
  host.innerHTML = "";
  const chip = (label, on, cb) => {
    const b = el("button", "cadb-chip" + (on ? " on" : ""), label);
    b.onclick = cb;
    host.append(b);
  };
  chip("all", !spRunFilterSpirit && !spRunFilterOutcome, () => { spRunFilterSpirit = ""; spRunFilterOutcome = ""; renderSpiritRuns(); });
  [...new Set(finished.map((r) => r.spirit))].sort().forEach((sp) =>
    chip(sp, spRunFilterSpirit === sp, () => { spRunFilterSpirit = spRunFilterSpirit === sp ? "" : sp; renderSpiritRuns(); }));
  chip("error", spRunFilterOutcome === "error", () => { spRunFilterOutcome = spRunFilterOutcome === "error" ? "" : "error"; renderSpiritRuns(); });
  chip("stopped", spRunFilterOutcome === "stopped", () => { spRunFilterOutcome = spRunFilterOutcome === "stopped" ? "" : "stopped"; renderSpiritRuns(); });
}

// parseRunSteps — the client-side step trace over the report's stable shape
// ("### Step N — <cast>" + rationale/result/summary bullets + the charge
// ledger table). Returns [] on any miss → callers fall back to the full body.
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
    steps.push({ n: parseInt(m[1], 10), cast: m[2].trim(), detail: line("result") || line("summary") || line("rationale") });
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

// spiritRunRow — the prototype's quiet row: chip · title · when · what it
// wrote, over a 6px charge bar + $spent / $ceiling. Click expands the step
// trace inline; "open full report" keeps the prompt affordance.
function spiritRunRow(r) {
  const row = el("div", "sprt-run");
  const top = el("div", "sprt-run-top");
  top.append(el("span", "run-outcome oc-" + (r.outcome || "").replace(/[^a-z-]/g, ""), r.outcome || "never run"));
  if (r.harness) top.append(el("span", "harness-chip", r.harness)); // federation source
  top.append(el("span", "sprt-run-title", `${r.spirit} / ${r.ritual}`));
  top.append(el("span", "sprt-run-wrote", r.itemsWritten ? "wrote " + r.itemsWritten : "—"));
  top.append(el("span", "sprt-run-when", fmtWhen(r.started)));
  row.append(top);
  // a failed run says WHY on the row itself — the outcome chip alone
  // ("error (protocol)") forced a trip into the report to learn anything
  if (r.outcome !== "completed" && r.outcome !== "running" && r.outcomeDetail) {
    let why = r.outcomeDetail;
    const cut = why.indexOf(" — ");
    if (cut >= 0) why = why.slice(cut + 3);
    row.append(el("div", "sprt-run-why", why));
  }
  const pct = r.ceilingUsd > 0 ? Math.min(100, Math.round((r.spentUsd / r.ceilingUsd) * 100)) : 0;
  const bar = el("span", "charge-bar");
  const fill = el("span", "charge-fill" + (pct >= 100 ? " over" : ""));
  fill.style.width = pct + "%";
  bar.appendChild(fill);
  const cr = el("div", "charge-row");
  cr.append(bar, el("span", "charge-label", `$${r.spentUsd.toFixed(4)} / $${r.ceilingUsd.toFixed(2)}`));
  row.append(cr);
  row.onclick = () => toggleRunTrace(row, r);
  return row;
}

// toggleRunTrace — inline expand: the parsed step trace (fallback: full body).
async function toggleRunTrace(row, r) {
  const open = row.nextElementSibling && row.nextElementSibling.classList.contains("run-trace");
  if (open) { row.nextElementSibling.remove(); return; }
  let body = "";
  try { body = ((await (await fetch("/api/spirits/runs/" + encodeURIComponent(r.id))).json()) || {}).body || ""; }
  catch (e) {}
  const box = el("div", "run-trace");
  const steps = parseRunSteps(body);
  if (steps.length) {
    steps.forEach((s) => {
      const ln = el("div", "run-trace-step");
      ln.append(el("span", "run-trace-n", String(s.n)));
      ln.append(el("span", "run-trace-cast", s.cast));
      ln.append(el("span", "run-trace-detail", s.detail || ""));
      if (s.usd) ln.append(el("span", "run-trace-usd", "$" + s.usd));
      box.append(ln);
    });
  } else if (body) {
    const pre = el("pre", "run-report");
    pre.textContent = body;
    box.append(pre);
  } else {
    box.append(emptyRow("No report yet."));
  }
  const full = pillLight("open full report", () => openSpiritRun(r.id));
  box.append(full);
  row.after(box);
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
