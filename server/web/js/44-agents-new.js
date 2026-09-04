// ================= Agents · the "new agent" wizard (#/agents/new) =================
// Agents plan §4.3 (Phase 6): replaces the bare newSpirit() prompt. Four
// screens — name + purpose → runtime → the runtime's own details → review —
// then a done screen. An excalibur spirit arrives WITH a first ritual (the
// cadence builder + a spellbook picker) through POST /api/spirits/spirit →
// ScaffoldSpiritWith, so a blank scaffold is no longer a reachable outcome; a
// Hermes profile goes through POST /api/profiles (`hermes profile create`)
// and the done screen surfaces the alias + `-p` targeting line. State is the
// one `wiz` object; every screen repaints from it (no DOM-held state).

let wiz = null;
const WIZ_SLUG = /^[a-z0-9][a-z0-9-]{0,31}$/;

function newAgent() { location.hash = "#/agents/new"; }

async function renderAgentWizard() {
  const host = document.getElementById("spNewWrap");
  if (!host) return;
  host.innerHTML = "loading…";
  const j = (u) => fetch(u).then((r) => (r.ok ? r.json() : null)).catch(() => null);
  const [catalog, status, profiles] = await Promise.all([
    j("/api/spirits/catalog"), j("/api/spirits/status"), j("/api/profiles"),
  ]);
  wiz = {
    step: 1, name: "", purpose: "", runtime: "spirit",
    ritualName: "", cad: cadDefault("daily"), spellbooks: [], instructions: "",
    cloneFrom: "", description: "",
    catalog: catalog || { portals: [], spellbooks: [] },
    spirits: Object.keys((status && status.spirits) || {}),
    engineOn: !!(status && status.enabled),
    profiles: (profiles && profiles.data) || [],
    profilesDegraded: profiles ? (profiles.degraded || "") : "/api/profiles did not answer",
    busy: false,
    done: null, // {kind, …} after a successful create
  };
  paintWizard(host);
}

// ---- shared bits ----
function wizNameTaken(name) {
  return wiz.spirits.includes(name) || wiz.profiles.some((p) => p.name === name);
}
function wizRow(label, ctl, hint) {
  const row = el("div", "sp-cap-row");
  row.append(el("span", "cadb-label", label));
  row.append(ctl);
  if (hint) row.append(el("span", "cad-raw", hint));
  return row;
}
// wizNav — back / next (or create) + a right-aligned hint; returns {refresh}
// so an input can re-derive the next button's state without a repaint.
function wizNav(body, host, o) {
  const nav = el("div", "wiz-nav");
  if (o.back) nav.append(pillLight("← back", () => { wiz.step -= 1; paintWizard(host); }));
  const next = pill(o.nextLabel || "next →", () => { if (!next.disabled) o.onNext(); });
  nav.append(next);
  const hint = el("span", "wiz-hint", "");
  nav.append(hint);
  body.append(nav);
  const api = {
    refresh() {
      const why = o.blocked();
      next.disabled = !!why || wiz.busy;
      hint.textContent = why || o.hint || "";
    },
  };
  api.refresh();
  return api;
}

function paintWizard(host) {
  host.innerHTML = "";
  const head = el("div", "sprt-head");
  head.append(el("span", "sprt-title", "New agent"),
    el("span", "sprt-sub", wiz.done ? "created" : "step " + wiz.step + " of 4"));
  const acts = el("span", "sprt-head-acts");
  const cancel = el("button", "sprt-quiet", wiz.done ? "close" : "cancel");
  cancel.onclick = () => { location.hash = "#/agents"; };
  acts.append(cancel);
  head.append(acts);
  host.append(head);

  // the step strip — you can step back, never skip ahead
  const strip = el("div", "wiz-steps");
  ["Name", "Runtime", wiz.runtime === "spirit" ? "First ritual" : "Profile", "Review"].forEach((label, i) => {
    const b = el("button", "cadb-chip" + (!wiz.done && wiz.step === i + 1 ? " on" : ""), (i + 1) + " · " + label);
    b.disabled = !!wiz.done || i + 1 > wiz.step;
    b.onclick = () => { wiz.step = i + 1; paintWizard(host); };
    strip.append(b);
  });
  host.append(strip);

  const body = el("div", "wiz-body");
  host.append(body);
  if (wiz.done) wizDone(body, host);
  else if (wiz.step === 1) wizName(body, host);
  else if (wiz.step === 2) wizRuntime(body, host);
  else if (wiz.step === 3) (wiz.runtime === "spirit" ? wizSpiritDetails : wizProfileDetails)(body, host);
  else wizReview(body, host);
}

// ---- 1 · name + purpose ----
function wizName(body, host) {
  body.append(el("div", "pp-section-head", "NAME — the slug every surface will use"));
  const box = el("div", "sp-capability");
  const name = inputEl('lowercase, e.g. "news-scout"');
  name.classList.add("wiz-in");
  name.maxLength = 32;
  name.value = wiz.name;
  const nameHint = el("span", "cad-raw", "");
  box.append(wizRow("name", name), nameHint);
  const purpose = inputEl("one line — what it is for, e.g. \"watches the trade press for AION-relevant moves\"");
  purpose.classList.add("wiz-in");
  purpose.style.maxWidth = "640px";
  purpose.value = wiz.purpose;
  box.append(wizRow("purpose", purpose));
  body.append(box);
  const nav = wizNav(body, host, {
    back: false,
    blocked: () => {
      const v = wiz.name.trim();
      if (!v) return "a name first";
      if (!WIZ_SLUG.test(v)) return "lowercase letters, digits, hyphens only";
      if (wizNameTaken(v)) return v + " already exists";
      if (!wiz.purpose.trim()) return "one line of purpose";
      return "";
    },
    hint: "the purpose becomes the identity line (spirit) or the description (profile)",
    onNext: () => { wiz.step = 2; paintWizard(host); },
  });
  const check = () => {
    wiz.name = name.value.trim().toLowerCase();
    const v = wiz.name;
    nameHint.textContent = !v ? "" : wizNameTaken(v) ? "already exists" : WIZ_SLUG.test(v) ? "#/agents/" + v : "lowercase letters, digits, hyphens only";
    nav.refresh();
  };
  name.addEventListener("input", check);
  purpose.addEventListener("input", () => { wiz.purpose = purpose.value; nav.refresh(); });
  name.addEventListener("keydown", (e) => { if (e.key === "Enter") purpose.focus(); });
  purpose.addEventListener("keydown", (e) => { if (e.key === "Enter" && !nav_disabled(body)) { wiz.step = 2; paintWizard(host); } });
  check();
  setTimeout(() => (wiz.name ? purpose : name).focus(), 0);
}
function nav_disabled(body) { const b = body.querySelector(".wiz-nav .pill:not(.light)"); return !b || b.disabled; }

// ---- 2 · runtime — each option says exactly what it creates ----
function wizRuntime(body, host) {
  body.append(el("div", "pp-section-head", "RUNTIME — where " + wiz.name + " runs"));
  const opts = el("div", "wiz-options");
  const option = (key, title, text, note) => {
    const b = el("button", "wiz-option" + (wiz.runtime === key ? " on" : ""));
    b.append(el("span", "wiz-option-title", title), el("span", "wiz-option-text", text));
    if (note) b.append(el("span", "wiz-option-note", note));
    b.onclick = () => { wiz.runtime = key; paintWizard(host); };
    return b;
  };
  opts.append(option("spirit", "excalibur spirit",
    "Creates spirits/" + wiz.name + "/ in the excalibur tree: identity (your purpose line), cornerstone (conduit claude-sub, writes artifacts/runs only, the spellbooks you pick), memories, and a first ritual on the cadence you choose. The engine runs it; reports land in RUNS and findings in the feed.",
    wiz.engineOn ? "" : "the excalibur engine is not configured on this box — the files are written, nothing will run them"));
  opts.append(option("profile", "Hermes profile",
    "Runs `hermes profile create " + wiz.name + "`: a separate Hermes home with its own config, skills, SOUL.md and cron. Nothing is scheduled by this; you target it with `hermes -p " + wiz.name + "` (or its alias) and add cron jobs from the profile page.",
    wiz.profilesDegraded ? "profile list unavailable: " + wiz.profilesDegraded : ""));
  body.append(opts);
  wizNav(body, host, {
    back: true, blocked: () => "", onNext: () => { wiz.step = 3; if (wiz.runtime === "profile" && !wiz.description) wiz.description = wiz.purpose; paintWizard(host); },
  });
}

// ---- 3a · spirit: the first ritual (name · cadence builder · spellbooks · instructions) ----
function wizSpiritDetails(body, host) {
  body.append(el("div", "pp-section-head", "FIRST RITUAL — " + wiz.name + " / …"));
  const box = el("div", "sp-capability");
  const rname = inputEl('lowercase, e.g. "morning-scan"');
  rname.classList.add("wiz-in");
  rname.maxLength = 32;
  rname.value = wiz.ritualName;
  box.append(wizRow("ritual", rname, "spirits/" + wiz.name + "/rituals/<name>.md"));
  body.append(box);

  body.append(el("div", "pp-section-head", "CADENCE"));
  const cadHost = el("div", "cadb");
  body.append(cadHost);
  const paintCad = () => renderCadenceBuilder(cadHost, wiz.cad, {
    custom: false, rawCron: "",
    onEdit: (next) => { wiz.cad = next; paintCad(); nav.refresh(); },
  });
  paintCad();

  body.append(el("div", "pp-section-head", "SPELLBOOKS — what it may read (the cornerstone's available_spellbooks)"));
  const sbRow = el("div", "cadb-row");
  const books = wiz.catalog.spellbooks || [];
  if (!books.length) sbRow.append(el("span", "cad-raw", "no spellbooks in grimoire/spellbooks — it will run but read nothing until one exists"));
  books.forEach((sb) => {
    const c = el("button", "cadb-chip" + (wiz.spellbooks.includes(sb) ? " on" : ""), sb);
    c.onclick = () => {
      wiz.spellbooks = wiz.spellbooks.includes(sb) ? wiz.spellbooks.filter((x) => x !== sb) : [...wiz.spellbooks, sb];
      c.classList.toggle("on", wiz.spellbooks.includes(sb));
      sbNote.textContent = sbNoteText();
    };
    sbRow.append(c);
  });
  body.append(sbRow);
  const sbNoteText = () => (wiz.spellbooks.length ? wiz.spellbooks.length + " granted · widen later on the agent page" : "none — the agent can run but read nothing (fails closed); pick at least one for real work");
  const sbNote = el("div", "sp-warn", sbNoteText());
  body.append(sbNote);

  body.append(el("div", "pp-section-head", "INSTRUCTIONS — what the ritual does (optional now, required to run)"));
  const ta = el("textarea", "editor-area rit-body wiz-ta");
  ta.spellcheck = false;
  ta.placeholder = "e.g. Read the feed spellbook for anything new since the last run; write one card per item worth the owner's attention to artifacts/feed.";
  ta.value = wiz.instructions;
  ta.addEventListener("input", () => { wiz.instructions = ta.value; nav.refresh(); });
  body.append(ta);

  const nav = wizNav(body, host, {
    back: true,
    blocked: () => {
      const v = wiz.ritualName.trim();
      if (!v) return "name the ritual";
      if (!WIZ_SLUG.test(v)) return "ritual: lowercase letters, digits, hyphens only";
      const errs = cadValidate(wiz.cad);
      if (errs.length) return errs[0];
      return "";
    },
    hint: "",
    onNext: () => { wiz.step = 4; paintWizard(host); },
  });
  const hintFor = () => {
    const { cron } = cadCompile(wiz.cad);
    if (!wiz.instructions.trim()) return cron ? "no instructions yet — it will be created paused" : "no instructions yet — on demand, nothing fires";
    return cron ? "scheduled and ready" : "on demand — run it from the board";
  };
  const origRefresh = nav.refresh;
  nav.refresh = () => { origRefresh(); if (!body.querySelector(".wiz-hint").textContent) body.querySelector(".wiz-hint").textContent = hintFor(); };
  rname.addEventListener("input", () => { wiz.ritualName = rname.value.trim().toLowerCase(); nav.refresh(); });
  nav.refresh();
  setTimeout(() => rname.focus(), 0);
}

// ---- 3b · profile: clone-from + description (the Phase 5 create, in the wizard) ----
function wizProfileDetails(body, host) {
  body.append(el("div", "pp-section-head", "PROFILE — hermes profile create " + wiz.name));
  const box = el("div", "sp-capability");
  const clone = selectEl(["(fresh — bundled skills, no keys)"].concat(wiz.profiles.map((p) => p.name)));
  clone.title = "--clone-from <src>: copies config.yaml, .env, SOUL.md and skills from that profile";
  if (wiz.cloneFrom) clone.value = wiz.cloneFrom;
  clone.onchange = () => { wiz.cloneFrom = clone.selectedIndex > 0 ? clone.value : ""; };
  box.append(wizRow("clone from", clone));
  const desc = inputEl("one or two sentences on what this profile is good at (kanban routing)");
  desc.classList.add("wiz-in");
  desc.style.maxWidth = "640px";
  desc.value = wiz.description;
  desc.addEventListener("input", () => { wiz.description = desc.value; });
  box.append(wizRow("description", desc, "--description"));
  body.append(box);
  body.append(el("div", "sp-warn", "The alias (a `" + wiz.name + "` wrapper on PATH) and the `-p` targeting line show after create — Hermes assigns them."));
  wizNav(body, host, { back: true, blocked: () => "", onNext: () => { wiz.step = 4; paintWizard(host); } });
  setTimeout(() => desc.focus(), 0);
}

// ---- 4 · review — what will be written, in plain words, then create ----
function wizSummary(rows) {
  const box = el("div", "wiz-summary");
  rows.forEach(([k, v]) => {
    const r = el("div", "wiz-sum-row");
    r.append(el("span", "wiz-sum-key", k));
    const val = el("span", "wiz-sum-val");
    if (v && v.nodeType) val.append(v); else val.textContent = v;
    r.append(val);
    box.append(r);
  });
  return box;
}
function wizReview(body, host) {
  body.append(el("div", "pp-section-head", "REVIEW — " + wiz.name));
  if (wiz.runtime === "spirit") {
    const { cron, phrase } = cadCompile(wiz.cad);
    const hasInstr = !!wiz.instructions.trim();
    body.append(wizSummary([
      ["creates", "spirits/" + wiz.name + "/ — identity.md · cornerstone.md · memories/ · rituals/" + wiz.ritualName + ".md"],
      ["purpose", wiz.purpose.trim()],
      ["capability", "conduit claude-sub · " + (wiz.spellbooks.length ? "spellbooks " + wiz.spellbooks.join(", ") : "no spellbooks") + " · writes artifacts/runs only"],
      ["first ritual", wiz.ritualName + " · " + phrase + (cron ? " (cadence: " + cron + ")" : "") + " · ceiling: chargebook default · 12 steps"],
      ["instructions", hasInstr ? wiz.instructions.trim().split("\n")[0].slice(0, 100) + (wiz.instructions.trim().length > 100 ? "…" : "") : "none yet"],
    ]));
    body.append(el("div", "wiz-plain", hasInstr
      ? "It runs " + phrase + " once the engine hot-reloads. You can change the instructions any time from the ritual editor."
      : "It will not run until the ritual has instructions" + (cron ? " — it is created paused; the editor opens next so you can write them and flip it to enabled." : " — it is on demand, so nothing fires; the editor opens next so you can write them.")));
  } else {
    const cmd = "hermes profile create " + wiz.name + (wiz.cloneFrom ? " --clone-from " + wiz.cloneFrom : "") + (wiz.description.trim() ? " --description " + JSON.stringify(wiz.description.trim()) : "");
    body.append(wizSummary([
      ["runs", cmd],
      ["creates", "a separate Hermes home for " + wiz.name + (wiz.cloneFrom ? " cloned from " + wiz.cloneFrom + " (config, .env, SOUL.md, skills)" : " (bundled skills, no keys)")],
      ["targeting", "hermes -p " + wiz.name + ' -z "…"  ·  runner Request.Profile = ' + JSON.stringify(wiz.name)],
      ["alias", "assigned by Hermes — shown after create"],
    ]));
    body.append(el("div", "wiz-plain", "This schedules nothing. Add cron jobs from the profile page (or `" + wiz.name + " cron add …`) once it exists."));
  }
  const nav = wizNav(body, host, {
    back: true, nextLabel: "create " + (wiz.runtime === "spirit" ? "agent" : "profile"),
    blocked: () => (wiz.busy ? "creating…" : ""),
    onNext: () => wizCreate(host, nav),
  });
}

async function wizCreate(host, nav) {
  wiz.busy = true;
  nav.refresh();
  setSaveState("saving");
  try {
    if (wiz.runtime === "spirit") {
      const { cron } = cadCompile(wiz.cad);
      const order = {
        name: wiz.name, purpose: wiz.purpose.trim(), spellbooks: wiz.spellbooks,
        ritual: { name: wiz.ritualName, cadence: cron || "", instructions: wiz.instructions.trim() },
      };
      const r = await fetch("/api/spirits/spirit", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(order) });
      if (!r.ok) throw new Error((await r.text()) || ("HTTP " + r.status));
      const d = await r.json().catch(() => ({}));
      wiz.done = { kind: "spirit", ritualPath: d.ritualPath || "", cron, paused: !!cron && !wiz.instructions.trim() };
      if (typeof loadSpiritsStatus === "function") loadSpiritsStatus();
      if (typeof loadSpiritRituals === "function") loadSpiritRituals(); // the board carries it now
    } else {
      const body = { name: wiz.name, description: wiz.description.trim() };
      if (wiz.cloneFrom) body.cloneFrom = wiz.cloneFrom;
      const r = await fetch("/api/profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || d.ok === false) throw new Error(d.error || d.output || ("HTTP " + r.status));
      wiz.done = { kind: "profile", alias: d.alias || "", aliasPath: d.aliasPath || "", target: d.target || ("-p " + wiz.name), command: d.command || "hermes profile create " + wiz.name };
      if (typeof loadProfileIndex === "function") loadProfileIndex();
    }
    setSaveState("saved");
    wiz.busy = false;
    paintWizard(host);
  } catch (e) {
    setSaveState("error");
    wiz.busy = false;
    nav.refresh();
    showToast("Couldn't create " + wiz.name + ": " + (e.message || e), null, "error");
  }
}

// ---- done — the plain outcome + where to go next ----
function wizDone(body, host) {
  const d = wiz.done;
  if (d.kind === "spirit") {
    const { phrase } = cadCompile(wiz.cad);
    body.append(el("div", "pp-section-head", wiz.name + " IS ON THE BOARD"));
    body.append(wizSummary([
      ["written", "spirits/" + wiz.name + "/ — identity.md · cornerstone.md · memories/ · " + (d.ritualPath ? d.ritualPath.replace(/^spirits\/[^/]+\//, "") : "rituals/" + wiz.ritualName + ".md")],
      ["ritual", wiz.name + " / " + wiz.ritualName + " · " + phrase + (d.paused ? " · paused" : "")],
      ["spellbooks", wiz.spellbooks.length ? wiz.spellbooks.join(", ") : "none"],
    ]));
    body.append(el("div", "wiz-plain", d.paused
      ? "It will not run until the ritual has instructions. Write them in the editor and set it to enabled."
      : wiz.instructions.trim() ? "It runs " + phrase + " once the engine hot-reloads." : "It will not run until the ritual has instructions. It is on demand — write them in the editor, then run it from the board."));
    const nav = el("div", "wiz-nav");
    nav.append(pill("open the ritual editor →", () => { location.hash = "#/agents/ritual/" + encodeURIComponent(wiz.name) + "/" + encodeURIComponent(wiz.ritualName); }));
    nav.append(pillLight("agent page", () => { location.hash = "#/agents/" + encodeURIComponent(wiz.name); }));
    nav.append(pillLight("schedule", () => { location.hash = "#/agents"; }));
    body.append(nav);
  } else {
    body.append(el("div", "pp-section-head", wiz.name + " IS A HERMES PROFILE"));
    body.append(wizSummary([
      ["ran", d.command],
      ["alias", d.alias ? d.alias + (d.aliasPath ? "  (" + d.aliasPath + ")" : "") : "none (--no-alias)"],
      ["targeting", (d.alias ? d.alias + " chat   ·   " : "") + "hermes " + d.target + ' -z "…"   ·   runner Request.Profile = ' + JSON.stringify(wiz.name)],
    ]));
    body.append(el("div", "wiz-plain", "Nothing is scheduled yet — its cron jobs live on the profile page, and its SOUL.md is edited where Hermes keeps it."));
    const nav = el("div", "wiz-nav");
    nav.append(pill("open the agent page →", () => { location.hash = "#/agents/" + encodeURIComponent(wiz.name); }));
    nav.append(pillLight("Settings › Agents", () => { location.hash = "#/settings/agents"; }));
    body.append(nav);
  }
}

// the header's ＋ agent button (index.html) opens the wizard
(() => { const b = document.getElementById("spNewAgent"); if (b) b.onclick = newAgent; })();
