// ================= Agents · SCHEDULE (the board) =================
// Split from 58-rituals.js (phase 0): board rows, the cadence builder and the
// structured ritual editor. The shell (route, poll, toasts) is 40-agents.js.
//
// Phase 2 (agents plan §4.3): the ALL RITUALS table became the SCHEDULE — a
// next-up line, then three DERIVED groups (Yours · Internal · Paused) of the
// same .ritual-row grid; every row carries a runtime chip, the last-5 outcome
// strip, a health chip and run / pause actions. Nothing here is stored:
// groups, health (late / silent / invalid — plan §3.2) and the strip are
// computed from /api/spirits/rituals + /api/spirits/runs on every paint.
//
// Phase 4 (§4.3 + D4/D5): Alfred's Hermes cron jobs sit on the same board —
// enabled jobs in Yours, paused ones in Paused — projected from ~/.hermes/cron
// by /api/agents/hermes (files first, `hermes cron list` only as fallback).
// Their controls are REAL: run now / pause / resume shell out to `hermes cron
// <cmd> <id>` through /api/agents/hermes/job/*, and the row's tooltip shows
// the verbatim command. Manifest never edits jobs.json.

let spiritRitualRows = []; // the crumb meta's ritual count reads this
let spiritModels = {};     // spirit → conduit name (from /api/harnesses; display only)
let hermesInfo = null;     // last /api/agents/hermes — the jobs list + degrade notes (display only)
let hermesRuns = [];       // last /api/agents/hermes/runs rows — the strip + health on Hermes rows
let hermesRunsDegraded = []; // what the fires projection could not read (usage_audit.jsonl gone…)
const schedOpen = { yours: true, internal: false, paused: false }; // group carets survive a repaint

async function fetchSpiritRituals() {
  try { return (await (await fetch("/api/spirits/rituals")).json()).data || []; } catch (e) { return []; }
}
async function fetchHermes() {
  try { return await (await fetch("/api/agents/hermes")).json(); } catch (e) { return null; }
}
async function fetchHermesRuns(since) {
  try {
    const d = await (await fetch("/api/agents/hermes/runs?since=" + encodeURIComponent(since || "7d"))).json();
    return { data: d.data || [], degraded: d.degraded || [] };
  } catch (e) { return { data: [], degraded: ["/api/agents/hermes/runs did not answer"] }; }
}
function hermesJobList() {
  return ((hermesInfo && hermesInfo.cron && hermesInfo.cron.list) || []);
}
// hermesRowOf — a Hermes job in the board's row shape (the group / sort /
// next-up code reads spirit · ritual · cadence · nextFire · enabled · valid);
// the job itself rides along as `hermes` for the row painter.
function hermesRowOf(j) {
  return {
    runtime: "alfred", hermes: j, spirit: "alfred", ritual: j.name || j.id,
    cadence: j.scheduleKind === "cron" ? j.schedule : "", nextFire: j.nextRunAt || "",
    enabled: j.enabled !== false, valid: j.outcome !== "unknown", path: "",
  };
}
async function loadSpiritModels() {
  try {
    const hz = (await (await fetch("/api/harnesses")).json()).harnesses || [];
    const h = hz.find((x) => x.primary) || hz[0];
    const out = {};
    ((h && h.spirits) || []).forEach((sp) => { out[sp.name] = sp.portal || ""; });
    spiritModels = out;
  } catch (e) {}
}
// loadSchedule — entering #/agents: rituals + runs together (the strip, the
// health chips and the next-up line read both), then one paint.
async function loadSchedule() {
  const [rows, runs, hz, hr] = await Promise.all([
    fetchSpiritRituals(), fetchSpiritRuns(), fetchHermes(), fetchHermesRuns("30d"), loadSpiritModels(),
  ]);
  spiritRuns = runs;
  hermesInfo = hz; hermesRuns = hr.data; hermesRunsDegraded = hr.degraded;
  renderSpiritRuns();        // the live strip (and the hidden RUNS list — harmless)
  renderSpiritRituals(rows);
  ensureLivePoll();
}
// loadSpiritRituals — rituals + the Hermes jobs list (a pause / resume / run
// repaints through here); the runs are whatever the last poll saw.
async function loadSpiritRituals() {
  const [rows, hz] = await Promise.all([fetchSpiritRituals(), fetchHermes()]);
  hermesInfo = hz;
  renderSpiritRituals(rows);
}

// ---- classification (derived, never stored — plan §4.3) ----
// paused   enabled: false (a Hermes job: enabled false / state paused)
// internal no cadence — spooled by a transcript sink or the / bar
// yours    on a clock (every enabled Hermes job counts — a person scheduled it)
function schedGroupOf(r) {
  if (r.enabled === false) return "paused";
  if (r.hermes) return "yours";
  if (!r.cadence) return "internal";
  return "yours";
}
function internalNote(r) {
  if (r.spirit === "extractor") return "run by the transcript sinks";
  if (r.spirit === "sage") return "run by the / bar";
  return "on demand — run with /";
}
// ritualRuns — this ritual's run reports, newest first (spiritRuns is the
// only run state; nothing is held per row).
function ritualRuns(r) {
  return (spiritRuns.data || []).filter((x) => x.spirit === r.spirit && x.ritual === r.ritual);
}

// ---- health (plan §3.2) ----
// invalid  the existing lint / engine verdict
// paused   enabled: false (the file's paused_reason line, if any, is the why)
// late     enabled, on a clock, has run: now − lastRun > 1.5 × interval, where
//          interval = the gap between the two most recent scheduled fires (the
//          0.5 is the grace; an irregular cadence gets the gap that just
//          passed). A running run is never late.
// silent   the three newest runs all completed with itemsWritten 0
// Both late and silent are --warn, never --danger.
function ritualHealth(r, runs) {
  if (!r.valid) return { state: "invalid", why: r.error || "invalid frontmatter" };
  if (r.enabled === false) return { state: "paused", why: r.pausedReason || "enabled: false — run now stays a manual override" };
  const last = runs[0];
  if (r.cadence && last && last.outcome !== "running") {
    const lastAt = new Date(last.started).getTime();
    const fires = cronPrevFires(r.cadence, new Date(), 2);
    if (!isNaN(lastAt) && fires.length === 2) {
      const interval = fires[0] - fires[1];
      if (Date.now() - lastAt > 1.5 * interval) {
        return { state: "late", why: "last run " + fmtAgo(last.started) + " · expected every " + schedDur(interval) + " (1.5× grace passed)" };
      }
    }
  }
  if (runs.length >= 3 && runs.slice(0, 3).every((x) => x.outcome === "completed" && !(x.itemsWritten > 0))) {
    return { state: "silent", why: "the last three runs completed with nothing written" };
  }
  return { state: "ok", why: "" };
}
function schedDur(ms) {
  const m = Math.round(ms / 60000);
  if (m < 60) return m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return h + "h";
  return Math.round(h / 24) + "d";
}
// outcomeClass — the strip's dot vocabulary (completed / error / stopped /
// running / other), folding "error (protocol)" and stopped-charge/-steps.
function outcomeClass(outcome) {
  const o = outcome || "";
  if (o === "completed" || o === "running") return o;
  if (o.startsWith("error")) return "error";
  if (o.startsWith("stopped")) return "stopped";
  return "other";
}
// openRunOnRuns — the run detail lives on RUNS; from the board, go there first
// (the same two-step the completion toast uses).
function openRunOnRuns(id) {
  location.hash = "#/agents/runs";
  setTimeout(() => openSpiritRun(id), 120);
}
// openHermesOnRuns — a Hermes fire has no report pane; RUNS filtered to the
// alfred runtime is where its rows (and their inline narration) live.
function openHermesOnRuns() {
  if (typeof spRunFilterSpirit !== "undefined") { spRunFilterSpirit = "alfred"; spRunFilterOutcome = ""; }
  location.hash = "#/agents/runs";
}

// ---- the board ----
function renderSpiritRituals(rows) {
  spiritRitualRows = rows;
  const host = els.spiritRitualBoard; host.innerHTML = "";
  const all = rows.concat(hermesJobList().map(hermesRowOf));
  renderNextUp(all);
  const byName = (a, b) => (a.spirit + "/" + a.ritual).localeCompare(b.spirit + "/" + b.ritual);
  const fireAt = (r) => { const t = new Date(r.nextFire || "").getTime(); return isNaN(t) ? Infinity : t; };
  const groups = { yours: [], internal: [], paused: [] };
  all.slice().sort(byName).forEach((r) => groups[schedGroupOf(r)].push(r));
  groups.yours.sort((a, b) => (fireAt(a) - fireAt(b)) || byName(a, b)); // soonest first; invalid (no fire) last
  // what the Hermes projection could not read (D4 graceful degrade) — said once, quietly
  const cron = (hermesInfo && hermesInfo.cron) || null;
  if (hermesInfo === null) host.append(el("div", "sched-degraded", "alfred: /api/agents/hermes did not answer — Hermes jobs not shown"));
  else if (cron && cron.outcome === "unknown") host.append(el("div", "sched-degraded", "alfred: jobs unknown — " + (cron.why || "jobs.json unreadable")));
  else if (cron && cron.source === "cli") host.append(el("div", "sched-degraded", "alfred: jobs.json missing — read from `hermes cron list`"));
  schedGroup(host, "yours", "YOURS", groups.yours, {
    empty: "Nothing on a clock — give a ritual a cadence from its editor.",
  });
  schedGroup(host, "internal", "INTERNAL", groups.internal, {
    note: "no cadence — spooled by the transcript sinks or the / bar",
    empty: "No sink-driven rituals.",
  });
  schedGroup(host, "paused", "PAUSED", groups.paused, {
    empty: "Nothing paused. domain scouting moved to Alfred (Hermes cron 0 7 * * *) on 2026-08-24.",
  });
  if (typeof renderSpiritIndex === "function") renderSpiritIndex(); // counts derive from these rows
  if (typeof updateSpiritsCrumb === "function") updateSpiritsCrumb();
}

// renderNextUp — the soonest three next-fires across the board, one line.
function renderNextUp(rows) {
  const host = document.getElementById("spiritNextUp");
  if (!host) return;
  host.innerHTML = "";
  const soon = rows.filter((r) => r.valid && r.enabled !== false && r.nextFire)
    .map((r) => ({ r, t: new Date(r.nextFire).getTime() })).filter((x) => !isNaN(x.t))
    .sort((a, b) => a.t - b.t).slice(0, 3);
  if (!soon.length) return;
  host.append(el("span", "sched-nextup-label", "next up"));
  soon.forEach(({ r }) => {
    const item = el("span", "sched-nextup-item");
    // a Hermes fire reads "alfred/<job>" so the runtime is never ambiguous
    item.append(el("span", "sched-nextup-when", nextUpWhen(r.nextFire)), document.createTextNode(" " + (r.hermes ? "alfred/" : "") + r.ritual));
    item.title = r.spirit + "/" + r.ritual + " · " + relPhrase(r.nextFire);
    item.onclick = r.hermes ? () => openHermesOnRuns()
      : () => { location.hash = "#/agents/ritual/" + encodeURIComponent(r.spirit) + "/" + encodeURIComponent(r.ritual); };
    host.append(item);
  });
}
function nextUpWhen(iso) {
  const d = new Date(iso);
  const t = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  return d.toDateString() === new Date().toDateString() ? t : d.toLocaleDateString([], { weekday: "short" }) + " " + t;
}

// schedGroup — an .aion-sec-label heading with a caret and the count over a
// .ritual-board body; Internal and Paused start collapsed (plan §4.3).
function schedGroup(host, key, title, list, opts) {
  const head = el("div", "aion-sec-label sched-group");
  const caret = el("span", "sec-caret", schedOpen[key] ? "▾" : "▸");
  head.append(caret, el("span", "aion-sec-title", title), el("span", "aion-sec-count", String(list.length)));
  if (opts.note) head.append(el("span", "sched-group-note", opts.note));
  const body = el("div", "ritual-board sched-group-body");
  body.hidden = !schedOpen[key];
  head.onclick = () => {
    schedOpen[key] = !schedOpen[key];
    body.hidden = !schedOpen[key];
    caret.textContent = schedOpen[key] ? "▾" : "▸";
  };
  if (!list.length) body.append(emptyRow(opts.empty));
  else list.forEach((r) => body.append(r.hermes ? hermesJobRow(r.hermes) : ritualRow(r)));
  host.append(head, body);
}

// ---- Hermes rows (Phase 4) ----
// hermesJobRuns — this job's fires, newest first, in the strip's shape.
function hermesJobRuns(j) {
  return (hermesRuns || []).filter((x) => x.job === j.id)
    .map((x) => ({ id: x.id, runtime: "alfred", outcome: x.outcome, started: x.started, itemsWritten: x.itemsWritten }));
}
// hermesJobHealth — the same ladder as ritualHealth plus `unpinned` (Hermes
// only): a job with no model pin fails CLOSED when the global inference
// config drifts — the 08-30..09-01 skips — so it's a --warn chip, never a
// silent detail. Both may show; late is the current state, unpinned the risk.
function hermesJobHealth(j, runs) {
  const out = [];
  if (j.outcome === "unknown") return [{ state: "invalid", why: "a reshaped jobs.json entry — id or name missing" }];
  if (j.enabled === false) return [{ state: "paused", why: j.pausedReason || (j.state === "completed" ? "a one-shot job that already ran" : "paused in Hermes") }];
  if (j.scheduleKind === "cron" && j.lastRunAt) {
    const lastAt = new Date(j.lastRunAt).getTime();
    const fires = cronPrevFires(j.schedule, new Date(), 2);
    if (!isNaN(lastAt) && fires.length === 2) {
      const interval = fires[0] - fires[1];
      if (Date.now() - lastAt > 1.5 * interval) {
        out.push({ state: "late", why: "last fire " + fmtAgo(j.lastRunAt) + " · expected every " + schedDur(interval) + " (1.5× grace passed)" });
      }
    }
  }
  if (!j.model) out.push({ state: "unpinned", why: "model: null in jobs.json — Hermes skips the fire (fails closed) whenever the global model drifts. Pin it: hermes cron edit " + j.id + " --model <name>" });
  return out;
}
// hermesCmd — the verbatim command a control runs (shown in the tooltip so
// the operator sees exactly what will happen before it does — D5).
function hermesCmd(j, action) {
  const bin = (hermesInfo && hermesInfo.runner && hermesInfo.runner.bin) || "hermes";
  return bin.split("/").pop() + " cron " + action + " " + j.id;
}
// hermesJobRow — the ritual row anatomy for a Hermes job: alfred chip · name
// · cadence phrase over cron (or Hermes' own display) · next · last outcome
// (last_status + last_error) · last-5 strip · health · model pin · actions.
function hermesJobRow(j) {
  const paused = j.enabled === false;
  const runs = hermesJobRuns(j);
  const health = hermesJobHealth(j, runs);
  const row = el("div", "ritual-row hermes" + (paused ? " paused" : "") + (j.outcome === "unknown" ? " invalid" : ""));
  const chip = el("span", "harness-chip ritual-runtime alfred", "alfred");
  chip.title = "Hermes cron job " + j.id + " — projected from ~/.hermes/cron/jobs.json";
  row.append(chip);
  // name — alfred (its Settings card) · job name; the prompt rides the tooltip
  const name = el("span", "ritual-name");
  const sp = el("span", "sprt-spirit", "alfred");
  sp.title = "Alfred (Hermes) — Settings › Agents";
  sp.onclick = (e) => { e.stopPropagation(); location.hash = "#/settings/agents"; };
  name.append(sp, document.createTextNode(" · " + (j.name || j.id)));
  name.title = [j.prompt ? "prompt: " + j.prompt : "", (j.skills || []).length ? "skills: " + j.skills.join(", ") : "", j.deliver ? "deliver: " + j.deliver : ""].filter(Boolean).join("\n");
  row.append(name);
  // cadence — the builder's phrase when the cron is one it can say, else Hermes' display
  const cad = el("span", "ritual-cadence");
  const parsed = j.scheduleKind === "cron" ? cadParse(j.schedule) : null;
  const phrase = parsed ? cadCompile(parsed).phrase : (j.scheduleHuman || j.schedule || "custom");
  if (paused) {
    cad.append(el("span", "cad-human", "paused" + (phrase ? " · " + phrase : "")));
  } else {
    cad.append(el("span", "cad-human", phrase));
  }
  cad.append(el("span", "cad-raw", j.scheduleKind === "cron" ? j.schedule : (j.scheduleKind || "schedule") + (j.repeatTimes != null ? " · " + (j.repeatCompleted || 0) + "/" + j.repeatTimes : "")));
  row.append(cad);
  // next fire
  const next = el("span", "ritual-next");
  if (!paused && j.nextRunAt) {
    next.append(document.createTextNode(fmtWhen(j.nextRunAt) + " "));
    next.append(el("span", "next-rel", relPhrase(j.nextRunAt)));
  } else {
    next.textContent = "—";
  }
  row.append(next);
  // last outcome — last_status ok/error, last_error as the why (→ RUNS)
  const oc = el("span", "ritual-outcome");
  if (j.lastStatus) {
    const word = j.lastStatus === "ok" ? "completed" : j.lastStatus === "error" ? "error" : j.lastStatus;
    const c = el("span", "run-outcome linky oc-" + word.replace(/[^a-z-]/g, ""), word);
    c.title = (j.lastError ? j.lastError + "\n" : "") + (j.lastRunAt ? "last fire " + fmtAgo(j.lastRunAt) : "") + " · open on RUNS";
    c.onclick = (e) => { e.stopPropagation(); openHermesOnRuns(); };
    oc.append(c);
  } else {
    oc.append(el("span", "run-outcome oc-never", "never run"));
  }
  row.append(oc);
  row.append(outcomeStrip(runs));
  // health chips (late / unpinned / paused / invalid); an enabled, healthy job says ok
  const hc = el("span", "ritual-health");
  if (health.length) {
    health.forEach((h) => { const c = el("span", "run-outcome oc-" + h.state, h.state); c.title = h.why; hc.append(c); });
  } else {
    hc.append(el("span", "sched-ok", "ok"));
  }
  row.append(hc);
  // the model pin column (Hermes' answer to the ceiling): pinned model, or unpinned
  const pin = el("span", "ritual-ceiling" + (j.model ? "" : " muted"));
  pin.append(el("span", "ceil-usd", j.model || "unpinned"));
  pin.append(el("span", "ceil-model", (j.deliver ? "→ " + j.deliver : "") + ((j.skills || []).length ? " · " + j.skills.length + " skill" + (j.skills.length === 1 ? "" : "s") : "")));
  pin.title = (j.model ? "model pinned in jobs.json: " + j.model : "no model pin — last fire used " + (j.modelSnapshot || "the global default"))
    + (j.provider ? " · provider " + j.provider : "");
  row.append(pin);
  // actions — the D5 controls; each tooltip is the verbatim command
  const acts = el("span", "ritual-acts");
  const run = el("button", "sprt-quiet", "run now");
  run.title = hermesCmd(j, "run") + " — fires on the next scheduler tick";
  run.onclick = (e) => { e.stopPropagation(); hermesJobAction(j, "run"); };
  acts.append(run);
  const tog = el("button", "sprt-quiet", paused ? "resume" : "pause");
  tog.title = hermesCmd(j, paused ? "resume" : "pause");
  tog.onclick = (e) => { e.stopPropagation(); hermesJobAction(j, paused ? "resume" : "pause"); };
  acts.append(tog);
  row.append(acts);
  if (paused && (j.pausedReason || j.state === "completed")) {
    row.append(el("div", "ritual-note", j.pausedReason || ("completed — ran " + (j.repeatCompleted || 0) + " of " + (j.repeatTimes != null ? j.repeatTimes : "∞"))));
  } else if (j.lastStatus === "error" && j.lastError) {
    row.append(el("div", "ritual-error", j.lastError));
  }
  return row;
}
// hermesJobAction — POST /api/agents/hermes/job/<id>/<action>: the server
// runs `hermes cron <action> <id>` and echoes the command; the board repaints
// from files (jobs.json is what the CLI wrote — no local state).
async function hermesJobAction(j, action) {
  setSaveState("saving");
  let d = {};
  try {
    const r = await fetch("/api/agents/hermes/job/" + encodeURIComponent(j.id) + "/" + action, { method: "POST" });
    d = await r.json().catch(() => ({}));
    if (!r.ok || d.ok === false) throw new Error(d.output || d.error || ("HTTP " + r.status));
    setSaveState("saved");
    const cmd = d.command || hermesCmd(j, action);
    if (action === "run") showToast(cmd + " — fires on the next scheduler tick; watch RUNS", () => openHermesOnRuns(), "info");
    else showToast(cmd + " — " + (j.name || j.id) + (action === "pause" ? " paused" : " resumed"));
  } catch (e) {
    setSaveState("error");
    showToast("Couldn't " + action + " " + (j.name || j.id) + ": " + (e.message || e), null, "error");
  }
  loadSpiritRituals();
}

// ritualRow — runtime chip · name · cadence over cron · next · last outcome
// chip (→ the run) · last-5 strip · health chip · ceiling/model · actions.
// Row click edits the ritual; the spirit name inside the cell opens its page.
function ritualRow(r) {
  const paused = r.enabled === false;
  const runs = ritualRuns(r);
  const health = ritualHealth(r, runs);
  const row = el("div", "ritual-row" + (r.valid ? "" : " invalid") + (paused ? " paused" : ""));
  // runtime — excalibur only until Phase 4 puts Hermes jobs on the board
  row.append(el("span", "harness-chip ritual-runtime", "excalibur"));
  // name — spirit (its own page) · ritual
  const name = el("span", "ritual-name");
  const sp = el("span", "sprt-spirit", r.spirit);
  sp.title = "Open " + r.spirit + "'s page";
  sp.onclick = (e) => { e.stopPropagation(); location.hash = "#/agents/" + encodeURIComponent(r.spirit); };
  name.append(sp, document.createTextNode(" · " + r.ritual));
  row.append(name);
  // cadence — human phrase over the raw cron (both visible)
  const cad = el("span", "ritual-cadence");
  if (paused) {
    cad.append(el("span", "cad-human", "paused" + (r.cadenceHuman && r.cadence ? " · " + r.cadenceHuman : "")));
    cad.append(el("span", "cad-raw", r.cadence || internalNote(r)));
  } else if (!r.cadence) {
    cad.append(el("span", "cad-human", "on demand"));
    cad.append(el("span", "cad-raw", internalNote(r)));
  } else {
    cad.append(el("span", "cad-human", r.cadenceHuman || "custom"));
    cad.append(el("span", "cad-raw", r.cadence));
  }
  row.append(cad);
  // next fire — absolute + quiet relative suffix
  const next = el("span", "ritual-next");
  if (r.valid && r.nextFire) {
    next.append(document.createTextNode(fmtWhen(r.nextFire) + " "));
    next.append(el("span", "next-rel", relPhrase(r.nextFire)));
  } else {
    next.textContent = "—";
  }
  row.append(next);
  // last outcome chip → the run (on RUNS)
  const oc = el("span", "ritual-outcome");
  if (!r.valid) {
    const chip = el("span", "run-outcome oc-invalid", "invalid");
    chip.title = r.error || "invalid frontmatter";
    oc.append(chip);
  } else if (r.lastOutcome) {
    const chip = el("span", "run-outcome oc-" + r.lastOutcome.replace(/[^a-z-]/g, ""), r.lastOutcome);
    if (r.lastRunId) { chip.classList.add("linky"); chip.title = "open the run"; chip.onclick = (e) => { e.stopPropagation(); openRunOnRuns(r.lastRunId); }; }
    oc.append(chip);
  } else {
    oc.append(el("span", "run-outcome oc-never", "never run"));
  }
  row.append(oc);
  // last-5 outcomes
  row.append(outcomeStrip(runs));
  // health — a chip only when there is something to say; scheduled rows say ok
  const hc = el("span", "ritual-health");
  if (health.state !== "ok") {
    const chip = el("span", "run-outcome oc-" + health.state, health.state);
    chip.title = health.why;
    hc.append(chip);
  } else if (r.cadence) {
    hc.append(el("span", "sched-ok", "ok"));
  }
  row.append(hc);
  // ceiling / model
  const ceil = el("span", "ritual-ceiling" + (r.ceilingDefault ? " muted" : ""));
  ceil.append(el("span", "ceil-usd", "$" + Number(r.ceilingUsd).toFixed(2)));
  if (spiritModels[r.spirit]) ceil.append(el("span", "ceil-model", spiritModels[r.spirit]));
  ceil.title = (r.ceilingDefault ? "chargebook default" : "ritual charge_usd") + (spiritModels[r.spirit] ? " · conduit " + spiritModels[r.spirit] : "");
  row.append(ceil);
  // actions — run now (the spool), pause / resume (enabled: line surgery)
  const acts = el("span", "ritual-acts");
  const run = el("button", "sprt-quiet", "run now");
  run.title = "spool a run — the engine picks it up within ~5s";
  run.onclick = (e) => { e.stopPropagation(); spiritSpool(r.spirit, r.ritual, "", { stay: true }); };
  acts.append(run);
  if (r.cadence || paused) {
    const tog = el("button", "sprt-quiet", paused ? "resume" : "pause");
    tog.title = paused ? "delete the enabled: false line — the engine reschedules it"
      : "write enabled: false — the engine unschedules it; run now stays a manual override";
    tog.onclick = (e) => { e.stopPropagation(); setRitualEnabled(r, paused); };
    acts.append(tog);
  }
  row.append(acts);
  if (!r.valid && r.error) row.append(el("div", "ritual-error", r.error));
  else if (paused && r.pausedReason) row.append(el("div", "ritual-note", r.pausedReason));
  row.onclick = () => { location.hash = "#/agents/ritual/" + encodeURIComponent(r.spirit) + "/" + encodeURIComponent(r.ritual); };
  return row;
}

// outcomeStrip — five 8px dots, oldest → newest, hollow where no run exists
// yet; each dot is titled and opens its run.
function outcomeStrip(runs) {
  const strip = el("span", "outcome-strip");
  const five = runs.slice(0, 5).reverse();
  for (let i = five.length; i < 5; i++) {
    const d = statusDot(false, "no run yet");
    d.classList.add("od-none");
    strip.append(d);
  }
  five.forEach((x) => {
    // a Hermes fire has no item count unless the ledger recorded one
    const items = x.outcome === "completed" && (x.itemsWritten != null || !x.runtime)
      ? " · " + (x.itemsWritten || 0) + " item" + (x.itemsWritten === 1 ? "" : "s") : "";
    const d = statusDot(false, x.outcome + " · " + fmtWhen(x.started) + items);
    d.classList.add("od-" + outcomeClass(x.outcome));
    d.onclick = (e) => { e.stopPropagation(); if (x.runtime === "alfred") openHermesOnRuns(); else openRunOnRuns(x.id); };
    strip.append(d);
  });
  return strip;
}

// setRitualEnabled — the pause toggle: `enabled:` line surgery on the ritual
// file through the lint-gated PUT (the writer the editor uses); the engine
// hot-reloads and reports paused: true in ritual-status.json. Absent = enabled
// (canonical), so resume DELETES the line (and any paused_reason) rather than
// writing true.
async function setRitualEnabled(r, enabled) {
  setSaveState("saving");
  try {
    const raw = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent(r.path))).json()).content || "";
    const { fmLines, body, hasFM } = splitFM(raw);
    if (!hasFM) throw new Error("no frontmatter in " + r.path);
    let fm = fmSurgery(fmLines, "enabled", enabled ? null : "false");
    if (enabled) fm = fmSurgery(fm, "paused_reason", null);
    const content = "---\n" + fm.join("\n") + "\n---\n" + body;
    const res = await fetch("/api/spirits/file?path=" + encodeURIComponent(r.path), {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ content }),
    });
    const j = await res.json().catch(() => ({}));
    if (res.status === 422 || j.ok === false) throw new Error((j.errors || ["lint blocked the save"]).join("; "));
    setSaveState("saved");
    showToast(r.spirit + "/" + r.ritual + (enabled ? " resumed" : " paused — run now stays a manual override"));
  } catch (e) {
    setSaveState("error");
    showToast("Couldn't " + (enabled ? "resume" : "pause") + ": " + (e.message || e), null, "error");
  }
  loadSpiritRituals();
}
// relPhrase: "in 9h" / "in 3d" / "due now"
function relPhrase(iso) {
  const d = new Date(iso), ms = d - new Date();
  if (isNaN(d)) return "";
  if (ms <= 0) return "due now";
  const m = Math.round(ms / 60000);
  if (m < 60) return "in " + m + "m";
  const h = Math.round(m / 60);
  if (h < 48) return "in " + h + "h";
  return "in " + Math.round(h / 24) + "d";
}

// ---- SPIRITS.md §2: the cadence builder ----
// State: { kind, days:[0..6], hours:[0..23], min:0..59, n:int }
// kinds: ondemand | daily | weekdays | weekends | days | everyMin | everyHour | hourly
// The option set is EXACTLY what humanCadence() (spirits/rituals.go) can
// phrase — "custom" cron is deliberately unreachable through the form. The
// compiled cron renders under the phrase as the receipt; an incomplete
// cadence blocks the save. `on demand` writes NO cadence key.

const CAD_DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const CAD_KINDS = [
  ["ondemand", "on demand"], ["daily", "daily"], ["weekdays", "weekdays"],
  ["weekends", "weekends"], ["days", "named days"],
  ["everyMin", "every N min"], ["everyHour", "every N hours"], ["hourly", "hourly"],
];

function cadDefault(kind) {
  return {
    kind,
    days: [],
    hours: ["daily", "weekdays", "weekends", "days"].includes(kind) ? [9] : [],
    min: 0,
    n: kind === "everyMin" ? 30 : 2,
  };
}

function cadValidate(cad) {
  const errs = [];
  if (!cad) return ["custom cron — edit through raw"];
  if (cad.kind === "days" && !cad.days.length) errs.push("pick at least one day");
  if (["daily", "weekdays", "weekends", "days"].includes(cad.kind) && !cad.hours.length) errs.push("pick at least one time");
  if ((cad.kind === "everyMin" || cad.kind === "everyHour") && !(cad.n >= 1)) errs.push("interval must be at least 1");
  return errs;
}

function cadClock(hr, mn) {
  let ap = "a", h = hr;
  if (hr === 0) h = 12;
  else if (hr === 12) ap = "p";
  else if (hr > 12) { h = hr - 12; ap = "p"; }
  return h + ":" + String(mn).padStart(2, "0") + ap;
}

// cadCompile — builder state → canonical cron (hours/days sorted ascending)
// + the phrase humanCadence() will echo back for it.
function cadCompile(cad) {
  if (!cad) return { cron: null, phrase: "custom" };
  if (cad.kind === "ondemand") return { cron: "", phrase: "on demand" };
  if (cadValidate(cad).length) return { cron: null, phrase: "incomplete" };
  if (cad.kind === "everyMin") return { cron: "*/" + cad.n + " * * * *", phrase: "every " + cad.n + " min" };
  if (cad.kind === "everyHour") return { cron: "0 */" + cad.n + " * * *", phrase: "every " + cad.n + " hours" };
  if (cad.kind === "hourly") return { cron: cad.min + " * * * *", phrase: "hourly at :" + String(cad.min).padStart(2, "0") };
  const hours = [...cad.hours].sort((a, b) => a - b);
  const days = [...cad.days].sort((a, b) => a - b);
  const dow = { daily: "*", weekdays: "1-5", weekends: "0,6" }[cad.kind] || days.join(",");
  const prefix = { daily: "daily", weekdays: "weekdays", weekends: "weekends" }[cad.kind]
    || days.map((d) => CAD_DAY_NAMES[d]).join(", ");
  return {
    cron: cad.min + " " + hours.join(",") + " * * " + dow,
    phrase: prefix + " " + hours.map((h) => cadClock(h, cad.min)).join(", "),
  };
}

// cadParse — the inverse, mirroring humanCadence()'s decision order EXACTLY
// (spirits/rituals.go:466). null = a custom cron the builder can't express
// (raw-only editing). Stricter than Go only in bounding minute ≤ 59.
function cadValueList(s, lo, hi) {
  const out = [];
  for (const p of s.split(",")) {
    if (!/^\d+$/.test(p.trim())) return null;
    const n = parseInt(p.trim(), 10);
    if (n < lo || n > hi) return null;
    out.push(n);
  }
  return out.length ? out : null;
}

function cadParse(cron) {
  const s = (cron || "").trim();
  if (s === "") return { kind: "ondemand", days: [], hours: [], min: 0, n: 0 };
  const f = s.split(/\s+/);
  if (f.length !== 5) return null;
  const [min, hour, dom, mon, dow] = f;
  const intOf = (x) => (/^\d+$/.test(x) ? parseInt(x, 10) : null);
  if (min.startsWith("*/") && hour === "*" && dom === "*" && mon === "*" && dow === "*") {
    const n = intOf(min.slice(2));
    return n != null && n > 0 ? { kind: "everyMin", days: [], hours: [], min: 0, n } : null;
  }
  if (min === "0" && hour.startsWith("*/") && dom === "*" && mon === "*" && dow === "*") {
    const n = intOf(hour.slice(2));
    return n != null && n > 0 ? { kind: "everyHour", days: [], hours: [], min: 0, n } : null;
  }
  if (dom !== "*" || mon !== "*") return null;
  const mn = intOf(min);
  if (mn == null || mn > 59) return null;
  if (hour === "*") return { kind: "hourly", days: [], hours: [], min: mn, n: 0 };
  const hours = cadValueList(hour, 0, 23);
  if (!hours) return null;
  if (dow === "*") return { kind: "daily", days: [], hours, min: mn, n: 0 };
  if (dow === "1-5") return { kind: "weekdays", days: [], hours, min: mn, n: 0 };
  if (dow === "0,6" || dow === "6,0") return { kind: "weekends", days: [], hours, min: mn, n: 0 };
  const days = cadValueList(dow, 0, 6);
  return days ? { kind: "days", days, hours, min: mn, n: 0 } : null;
}

// canonCron — the dirty-compare key: canonical-vs-canonical, never raw strings
// ("0 18,8 * * *" must open clean).
function canonCron(cad) { return cad ? cadCompile(cad).cron : null; }

// cron matching over the builder vocabulary (exact / list / */N / a-b range),
// shared by the receipt's next-fire and the board's late derivation.
function cronFieldMatch(field, v) {
  if (field === "*") return true;
  if (field.startsWith("*/")) { const n = parseInt(field.slice(2), 10); return n > 0 && v % n === 0; }
  return field.split(",").some((p) => {
    const r = p.split("-");
    if (r.length === 2) return v >= parseInt(r[0], 10) && v <= parseInt(r[1], 10);
    return parseInt(p, 10) === v;
  });
}
function cronMatches(f, d) {
  return cronFieldMatch(f[0], d.getMinutes()) && cronFieldMatch(f[1], d.getHours()) &&
    cronFieldMatch(f[2], d.getDate()) && cronFieldMatch(f[3], d.getMonth() + 1) && cronFieldMatch(f[4], d.getDay());
}

// cadNextFire — client next-fire for the receipt's `next:` line. Minute-scan;
// 8-day horizon.
function cadNextFire(cron) {
  if (!cron) return null;
  const f = cron.trim().split(/\s+/);
  if (f.length !== 5) return null;
  const d = new Date();
  d.setSeconds(0, 0);
  d.setMinutes(d.getMinutes() + 1);
  for (let i = 0; i < 60 * 24 * 8; i++) {
    if (cronMatches(f, d)) return d;
    d.setMinutes(d.getMinutes() + 1);
  }
  return null;
}

// cronPrevFires — the newest `n` scheduled fires at or before `from`, newest
// first, scanning back at most 15 days (a weekly cadence needs two fires).
// Hours that can't match are skipped whole, so the scan stays cheap.
function cronPrevFires(cron, from, n) {
  const f = (cron || "").trim().split(/\s+/);
  if (f.length !== 5) return [];
  const d = new Date(from);
  d.setSeconds(0, 0);
  const floor = d.getTime() - 15 * 86400000;
  const out = [];
  while (out.length < n && d.getTime() >= floor) {
    if (!cronFieldMatch(f[1], d.getHours())) { d.setMinutes(0); d.setMinutes(-1); continue; }
    if (cronMatches(f, d)) out.push(new Date(d));
    d.setMinutes(d.getMinutes() - 1);
  }
  return out;
}

// renderCadenceBuilder(host, cad, {custom, rawCron, onEdit}) — kind chips →
// conditional rows → the receipt. Every control routes through onEdit(next)
// (which hands authority back to the form — the raw pane's contract).
function renderCadenceBuilder(host, cad, opts) {
  host.innerHTML = "";
  const edit = (next) => opts.onEdit(next);
  if (opts.custom) {
    const note = el("div", "cadb-custom");
    note.append(el("span", "cadb-custom-msg", "custom cron — the builder can't express this; edit through raw below"));
    note.append(el("code", "cadb-custom-cron", opts.rawCron || ""));
    host.append(note);
  }
  // kind chips (picking one from custom mode reseeds the form — form wins)
  const kinds = el("div", "cadb-row");
  CAD_KINDS.forEach(([k, label]) => {
    const b = el("button", "cadb-chip" + (!opts.custom && cad && cad.kind === k ? " on" : ""), label);
    b.onclick = () => edit(cad && cad.kind === k ? { ...cad } : cadDefault(k));
    kinds.append(b);
  });
  host.append(kinds);
  if (opts.custom || !cad) return;

  const chipRow = (label, chips) => {
    const row = el("div", "cadb-row");
    row.append(el("span", "cadb-label", label));
    chips.forEach((c) => row.append(c));
    host.append(row);
    return row;
  };
  const toggleChip = (label, on, cb) => {
    const b = el("button", "cadb-chip" + (on ? " on" : ""), label);
    b.onclick = cb;
    return b;
  };

  if (cad.kind === "days") {
    chipRow("days", CAD_DAY_NAMES.map((nm, d) =>
      toggleChip(nm, cad.days.includes(d), () => {
        const days = cad.days.includes(d) ? cad.days.filter((x) => x !== d) : [...cad.days, d];
        edit({ ...cad, days });
      })));
  }
  if (["daily", "weekdays", "weekends", "days"].includes(cad.kind)) {
    // time list: one chip per chosen hour (✕ removes), ＋ time appends
    const chips = [...cad.hours].sort((a, b) => a - b).map((h) =>
      toggleChip(cadClock(h, cad.min) + " ✕", true, () =>
        edit({ ...cad, hours: cad.hours.filter((x) => x !== h) })));
    const add = document.createElement("select");
    add.className = "pp-in cadb-add";
    const o0 = document.createElement("option"); o0.value = ""; o0.textContent = "＋ time"; add.append(o0);
    for (let h = 0; h < 24; h++) {
      if (cad.hours.includes(h)) continue;
      const o = document.createElement("option"); o.value = String(h); o.textContent = cadClock(h, cad.min); add.append(o);
    }
    add.onchange = () => { if (add.value !== "") edit({ ...cad, hours: [...cad.hours, parseInt(add.value, 10)] }); };
    chipRow("times", [...chips, add]);
  }
  if (cad.kind === "hourly" || ["daily", "weekdays", "weekends", "days"].includes(cad.kind)) {
    const presets = [0, 15, 30, 45];
    if (!presets.includes(cad.min)) presets.push(cad.min); // parsed off-preset value stays representable
    chipRow("minute", presets.sort((a, b) => a - b).map((m) =>
      toggleChip(":" + String(m).padStart(2, "0"), cad.min === m, () => edit({ ...cad, min: m }))));
  }
  if (cad.kind === "everyMin" || cad.kind === "everyHour") {
    const presets = cad.kind === "everyMin" ? [5, 10, 15, 30, 45] : [1, 2, 3, 4, 6, 12];
    if (!presets.includes(cad.n)) presets.push(cad.n);
    chipRow("every", presets.sort((a, b) => a - b).map((n) =>
      toggleChip(String(n) + (cad.kind === "everyMin" ? " min" : " h"), cad.n === n, () => edit({ ...cad, n }))));
  }

  // the receipt: phrase left, the compiled cron right — how you confirm the
  // builder wrote what you meant
  const { cron, phrase } = cadCompile(cad);
  const errs = cadValidate(cad);
  const receipt = el("div", "cadb-receipt" + (errs.length ? " incomplete" : ""));
  receipt.append(el("span", "cadb-phrase", errs.length ? errs[0] : phrase));
  receipt.append(el("code", "cadb-cron",
    cad.kind === "ondemand" ? "no cadence key" : (cron == null ? "nothing to write yet" : "cadence: " + cron)));
  host.append(receipt);
  if (cron) {
    const nx = cadNextFire(cron);
    if (nx) host.append(el("div", "cadb-next", "next: " + nx.toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })));
  }
}

// ---- SPIRITS.md §3: the structured ritual editor ----
// Four sections — Cadence (builder) · Limits (derived ceiling + steps +
// read-only capability summary) · Instructions (body) · Raw (escape hatch).
// TWO objects only: `record` (the fetched file) and `open` (the form). Dirty
// is DERIVED by comparing them; save writes the record and the record becomes
// the baseline — no second `saved` store (§7 parallel truth).

let ritEd = null; // { path, spirit, name, record, open, bar, hosts }

// splitFM — leading `---` fence block + body, kept as LINES so serialization
// is line-surgery on the original (unsurfaced keys survive verbatim).
function splitFM(content) {
  const lines = content.split("\n");
  if (lines[0] !== "---") return { fmLines: [], body: content, hasFM: false };
  const end = lines.indexOf("---", 1);
  if (end < 0) return { fmLines: [], body: content, hasFM: false };
  return { fmLines: lines.slice(1, end), body: lines.slice(end + 1).join("\n"), hasFM: true };
}
function fmValue(fmLines, key) {
  const re = new RegExp("^" + key + ":\\s*(.*)$");
  for (const ln of fmLines) { const m = ln.match(re); if (m) return m[1].trim(); }
  return null;
}
// fmSurgery — replace / insert (after ritual:, else at top) / delete one key's
// line in the original fm block. Returns new lines; everything else verbatim.
// The key is escaped for the match (chargebook keys carry dots) but written raw.
function fmSurgery(fmLines, key, value) {
  const re = new RegExp("^" + key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ":");
  const idx = fmLines.findIndex((ln) => re.test(ln));
  const out = [...fmLines];
  if (value === null) { if (idx >= 0) out.splice(idx, 1); return out; }
  const line = key + ": " + value;
  if (idx >= 0) out[idx] = line;
  else {
    const after = out.findIndex((ln) => /^ritual:/.test(ln));
    out.splice(after >= 0 ? after + 1 : 0, 0, line);
  }
  return out;
}

function parseRitualRecord(raw) {
  const { fmLines, body } = splitFM(raw);
  const cadence = fmValue(fmLines, "cadence"); // null = no key = on demand
  return {
    raw, fmLines, body,
    cadence: cadence === null ? "" : cadence,
    charge: fmValue(fmLines, "charge_usd"),      // string|null (null = inherited)
    maxSteps: fmValue(fmLines, "max_steps"),     // string|null
    enabled: (fmValue(fmLines, "enabled") || "true").trim().toLowerCase() !== "false",
  };
}

// serializeRitual — the record's own frontmatter with ONLY the edited keys
// surgically changed; ondemand removes the cadence line, an inherited ceiling
// removes charge_usd (the raw pane is the verifiable receipt of both).
function serializeRitual(record, open) {
  let fm = record.fmLines;
  if (open.custom) {
    // custom cron: the builder can't express it — cadence stays whatever raw
    // editing made it; no surgery on the cadence line from the form side
  } else {
    const cron = cadCompile(open.cad).cron;
    fm = fmSurgery(fm, "cadence", open.cad.kind === "ondemand" ? null : cron);
  }
  fm = fmSurgery(fm, "charge_usd", open.charge === null ? null : open.charge);
  fm = fmSurgery(fm, "max_steps", open.maxSteps === null ? null : open.maxSteps);
  fm = fmSurgery(fm, "enabled", open.enabled ? null : "false"); // absent = enabled (canonical)
  return "---\n" + fm.join("\n") + "\n---\n" + open.body;
}

function ritEditorDirty() {
  const { record, open } = ritEd;
  if (open.raw !== undefined) return open.raw !== record.raw;
  if (open.body !== record.body) return true;
  if ((open.charge === null ? null : String(open.charge)) !== record.charge) return true;
  if ((open.maxSteps === null ? null : String(open.maxSteps)) !== record.maxSteps) return true;
  if (open.enabled !== record.enabled) return true;
  if (!open.custom) {
    const recCad = cadParse(record.cadence);
    if (recCad === null) return true; // form took over a custom cron
    if (canonCron(open.cad) !== canonCron(recCad)) return true;
  }
  return false;
}

// the chargebook default the inherited figure renders at — derived from the
// board row (Store.ritualRow already computes it); cold path parses
// chargebook.md. Never a JS constant (the real value is 0.50, not the spec's
// 0.25 fixture).
async function chargebookDefault() {
  const row = (spiritRitualRows || []).find((r) => r.ceilingDefault);
  if (row) return Number(row.ceilingUsd);
  try {
    const raw = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent("chargebook.md"))).json()).content || "";
    const m = raw.match(/^default_run_ceiling_usd:\s*([\d.]+)/m);
    if (m) return parseFloat(m[1]);
  } catch (e) {}
  return null;
}

async function renderRitualEditor(path) {
  const host = document.getElementById("spEditorWrap");
  if (!host) return;
  host.innerHTML = "loading…";
  let raw = "";
  try { raw = (await (await fetch("/api/spirits/file?path=" + encodeURIComponent(path))).json()).content || ""; }
  catch (e) { host.innerHTML = ""; host.append(emptyRow("Couldn't load " + path)); return; }
  const record = parseRitualRecord(raw);
  const cad = cadParse(record.cadence);
  ritEd = {
    path,
    spirit: spSpirit,
    name: path.split("/").pop().replace(/\.md$/, ""),
    record,
    open: {
      cad: cad || cadDefault("ondemand"),
      custom: cad === null,
      charge: record.charge === null ? null : record.charge,
      maxSteps: record.maxSteps === null ? null : record.maxSteps,
      body: record.body,
      enabled: record.enabled,
      raw: undefined,
    },
    showRaw: cad === null, // custom cron: raw auto-opens — it IS the edit path
  };
  const defCeil = await chargebookDefault();
  ritEd.defCeil = defCeil;
  paintRitualEditor(host);
}

function paintRitualEditor(host) {
  host.innerHTML = "";
  const { record, open } = ritEd;

  const head = el("div", "sprt-head");
  head.append(el("span", "sprt-title", ritEd.spirit + " / " + ritEd.name));
  head.append(el("span", "sprt-sub", ritEd.path + " · the engine hot-reloads on save"));
  const acts = el("span", "sprt-head-acts");
  const pause = el("button", "sprt-quiet rit-pause" + (open.enabled ? "" : " paused"),
    open.enabled ? "enabled" : "paused");
  pause.title = "pause without deleting — the engine unschedules it; run now stays a manual override";
  pause.onclick = () => { open.enabled = !open.enabled; delete open.raw; paintRitualEditor(host); };
  const run = el("button", "sprt-quiet", "run now");
  run.onclick = () => spiritSpool(ritEd.spirit, ritEd.name, "");
  const rawT = el("button", "sprt-quiet", ritEd.showRaw ? "hide raw" : "show raw");
  rawT.onclick = () => { ritEd.showRaw = !ritEd.showRaw; paintRitualEditor(host); };
  const del = armedDelete("delete", "confirm delete?", async () => {
    try {
      const r = await fetch("/api/spirits/ritual/delete", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ spirit: ritEd.spirit, name: ritEd.name }) });
      if (!r.ok) throw new Error(await r.text());
      showToast("Deleted " + ritEd.spirit + "/" + ritEd.name + " — git history is the undo");
      location.hash = "#/agents";
    } catch (e) { showToast("Couldn't delete: " + (e.message || e), null, "error"); }
  });
  acts.append(pause, run, rawT, del);
  head.append(acts);
  host.append(head);

  const lint = el("div", "editor-lint");
  lint.hidden = true;
  const bar = derivedDirtyBar(host, {
    compute: () => {
      const errs = open.raw !== undefined || open.custom ? [] : cadValidate(open.cad);
      const dirty = ritEditorDirty();
      return {
        dirty, blocked: errs.length > 0,
        msg: errs.length ? "can't save — " + errs[0]
          : dirty ? "unsaved changes · lint runs on save"
          : "no changes",
      };
    },
    onSave: () => saveRitualEditor(host, lint),
    onDiscard: () => renderRitualEditor(ritEd.path),
  });
  ritEd.bar = bar;
  host.append(lint);

  const section = (label) => { host.append(el("div", "pp-section-head", label)); };

  // 1 · CADENCE
  section("CADENCE");
  const cadHost = el("div", "cadb");
  host.append(cadHost);
  const paintCad = () => renderCadenceBuilder(cadHost, open.custom ? null : open.cad, {
    custom: open.custom,
    rawCron: record.cadence,
    onEdit: (next) => {
      open.cad = next;
      open.custom = false;      // touching any cadence control hands authority to the form
      delete open.raw;          // …and the raw pane re-derives (§3 precedence)
      paintCad();
      paintRaw();
      bar.refresh();
    },
  });
  paintCad();

  // 2 · LIMITS — inline figures + the read-only capability summary
  section("LIMITS");
  const lim = el("div", "rit-limits");
  const figure = (label, valText, muted, note, onCommit) => {
    const f = el("div", "rit-figure");
    f.append(el("span", "rit-fig-label", label));
    const v = el("button", "rit-fig-val" + (muted ? " muted" : ""), valText);
    v.title = "click to edit" + (muted ? " — inheriting the chargebook default" : "");
    v.onclick = () => {
      const input = el("input", "pp-in rit-fig-in");
      input.value = muted ? "" : valText.replace(/^\$/, "");
      input.placeholder = muted ? valText.replace(/^\$/, "") : "";
      const commit = () => { onCommit(input.value.trim()); };
      input.onblur = commit;
      input.onkeydown = (ev) => { if (ev.key === "Enter") input.blur(); if (ev.key === "Escape") paintRitualEditor(host); };
      v.replaceWith(input);
      input.focus();
    };
    f.append(v);
    f.append(el("span", "rit-fig-note", note));
    return f;
  };
  const ceilText = open.charge !== null ? "$" + Number(open.charge).toFixed(2)
    : ritEd.defCeil !== null ? "$" + ritEd.defCeil.toFixed(2) : "$—";
  lim.append(figure("charge ceiling", ceilText, open.charge === null,
    open.charge === null ? "chargebook default" : "charge_usd",
    (v) => {
      open.charge = v === "" ? null : v;   // blank = inherit (no key in raw)
      delete open.raw;
      paintRitualEditor(host);
    }));
  lim.append(figure("max steps", open.maxSteps !== null ? String(open.maxSteps) : "—", open.maxSteps === null,
    open.maxSteps === null ? "engine default" : "max_steps",
    (v) => {
      open.maxSteps = v === "" ? null : v;
      delete open.raw;
      paintRitualEditor(host);
    }));
  // capability summary — inherited from the cornerstone; editing it here would
  // duplicate the spirit page, so it links there instead (spec §3.2)
  const cap = el("div", "rit-capability");
  cap.textContent = "…";
  lim.append(cap);
  host.append(lim);
  fetch("/api/spirits/file?path=" + encodeURIComponent("spirits/" + ritEd.spirit + "/cornerstone.md"))
    .then((r) => r.json()).then((d) => {
      const { fmLines } = splitFM(d.content || "");
      const portal = (fmValue(fmLines, "portal") || "").replace(/^:?\s*/, "") || "—";
      const sbRaw = fmValue(fmLines, "available_spellbooks") || "[]";
      const wrRaw = fmValue(fmLines, "writable") || "[]";
      const count = (s) => (s.replace(/[\[\]\s]/g, "") ? s.replace(/[\[\]]/g, "").split(",").length : 0);
      cap.innerHTML = "";
      cap.append(el("span", "rit-cap-sum",
        portal + " · " + count(sbRaw) + " spellbook" + (count(sbRaw) === 1 ? "" : "s") + " · writes " + (wrRaw.replace(/[\[\]]/g, "") || "nothing")));
      const link = el("a", "aion-open", "edit " + ritEd.spirit + " →");
      link.href = "#/agents/" + encodeURIComponent(ritEd.spirit);
      cap.append(link);
    }).catch(() => { cap.textContent = ""; });

  // 3 · INSTRUCTIONS — the markdown body, a real textarea
  section("INSTRUCTIONS");
  const bodyTa = el("textarea", "editor-area rit-body");
  bodyTa.spellcheck = false;
  bodyTa.value = open.body;
  bodyTa.addEventListener("input", () => { open.body = bodyTa.value; delete open.raw; paintRaw(); bar.refresh(); });
  host.append(bodyTa);

  // 4 · RAW — behind show raw; live-derived until typed in (typing keeps what
  // you typed; any cadence control hands authority back to the form)
  const rawWrap = el("div", "rit-raw");
  rawWrap.hidden = !ritEd.showRaw;
  rawWrap.append(el("div", "pp-section-head", "RAW"));
  const rawTa = el("textarea", "editor-area rit-raw-ta");
  rawTa.spellcheck = false;
  rawTa.addEventListener("input", () => { open.raw = rawTa.value; bar.refresh(); });
  rawWrap.append(rawTa);
  host.append(rawWrap);
  const paintRaw = () => { rawTa.value = open.raw !== undefined ? open.raw : serializeRitual(record, open); };
  paintRaw();

  bar.refresh();
}

async function saveRitualEditor(host, lint) {
  const { record, open } = ritEd;
  const content = open.raw !== undefined ? open.raw : serializeRitual(record, open);
  lint.hidden = true; lint.innerHTML = "";
  setSaveState("saving");
  try {
    const r = await fetch("/api/spirits/file?path=" + encodeURIComponent(ritEd.path), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content }),
    });
    const res = await r.json();
    if (r.status === 422 || res.ok === false) {
      setSaveState("error");
      lint.hidden = false;
      (res.errors || ["save blocked"]).forEach((m) => lint.append(el("div", "lint-err", "✕ " + m)));
      (res.warnings || []).forEach((m) => lint.append(el("div", "lint-warn", "⚠ " + m)));
      return; // dirty persists — the record was not written
    }
    setSaveState("saved");
    if ((res.warnings || []).length) {
      lint.hidden = false;
      lint.classList.add("lint-ok");
      res.warnings.forEach((m) => lint.append(el("div", "lint-warn", "⚠ " + m)));
    }
    // the record IS the baseline: reparse what was written, reseed the form
    ritEd.record = parseRitualRecord(content);
    const cad = cadParse(ritEd.record.cadence);
    ritEd.open = {
      cad: cad || cadDefault("ondemand"), custom: cad === null,
      charge: ritEd.record.charge, maxSteps: ritEd.record.maxSteps,
      body: ritEd.record.body, enabled: ritEd.record.enabled, raw: undefined,
    };
    loadSpiritRituals(); // board shows the new schedule
    paintRitualEditor(host);
  } catch (e) { setSaveState("error"); showToast("Save failed: " + (e.message || e), null, "error"); }
}
