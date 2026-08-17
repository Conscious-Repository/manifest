// ---- TERMINAL: the cockpit (cmd-ctr terminal-module parity) ----
// One surface: a rail of SESSIONS / NEW SESSION / HISTORY with an icon tab bar
// pinned at its foot, and a stage swapping Terminal (PTY over metis tmux) /
// Files (fleet browser) / Activity (fleet stats). Every session — local or
// remote — is a metis tmux, so keep-alive is inherent. xterm vendored (UMD).

let termSessions = [];
let termOpenId = "";
let termInst = null;   // { term, fit, ws, id, ro }
let termStage = "term";           // term | files | activity
let termLaunch = { kind: "shell", cwd: "", device: "" }; // device "" = this box
let termDevices = [];             // fleet rows from /api/terminal/devices
let termDevOpen = false;          // device list expanded?
let termHistQuery = "";
let termPollTimer = null;
let termBrowseOpen = false, termBrowsePath = "";

const TERM_STAGES = [
  { stage: "term", glyph: "❯", label: "term" },
  { stage: "files", glyph: "▤", label: "files" },
  { stage: "activity", glyph: "∿", label: "stats" },
];

function showTerminal() {
  try { termStage = localStorage.getItem("manifest.termStage") || termStage; } catch (e) {}
  if (!TERM_STAGES.some((s) => s.stage === termStage)) termStage = "term";
  renderTermTabbar();
  termApplyStage();
  termFitShell();
  loadTermSessions();
  if (!termPollTimer) {
    termPollTimer = setInterval(() => {
      if (els.terminalView && !els.terminalView.hidden && !document.hidden) loadTermSessions(true);
    }, 5000);
    window.addEventListener("resize", termFitShell);
  }
}

// termFitShell sizes the cockpit to EXACTLY the space below its top edge —
// a fixed vh-calc breaks whenever the chrome above grows (zoom, banners,
// taller crumbs) and the last terminal row falls off-screen.
function termFitShell() {
  const shell = document.querySelector("#terminalView .term-shell");
  if (!shell || els.terminalView.hidden) return;
  if (window.innerWidth <= 860) { shell.style.height = ""; return; } // mobile stacks
  const top = shell.getBoundingClientRect().top;
  shell.style.height = Math.max(320, window.innerHeight - top - 14) + "px";
  if (termInst) { try { termInst.fit.fit(); sendTermResize(); } catch (e) {} }
}

function renderTermTabbar() {
  const bar = document.getElementById("termTabbar");
  if (!bar) return;
  bar.innerHTML = "";
  TERM_STAGES.forEach((t) => {
    const b = el("button", "term-tab" + (t.stage === termStage ? " on" : ""));
    b.append(el("span", "term-tab-glyph", t.glyph), el("span", "term-tab-label", t.label));
    b.onclick = () => termSetStage(t.stage);
    bar.append(b);
  });
}

function termSetStage(stage) {
  termStage = stage;
  try { localStorage.setItem("manifest.termStage", stage); } catch (e) {}
  renderTermTabbar();
  termApplyStage();
}

function termApplyStage() {
  const panes = { term: "termStageTerm", files: "termStageFiles", activity: "termStageActivity" };
  Object.entries(panes).forEach(([k, id]) => {
    const p = document.getElementById(id);
    if (p) p.hidden = k !== termStage;
  });
  // the WS stays attached when leaving the term stage — scrollback keeps warm
  if (termStage === "term" && termInst) { try { termInst.fit.fit(); sendTermResize(); } catch (e) {} }
  if (termStage === "files" && typeof showFilesStage === "function") showFilesStage();
  if (termStage === "activity") {
    if (typeof showActivityStage === "function") showActivityStage();
    else {
      const p = document.getElementById("termStageActivity");
      if (p && !p.childElementCount) p.append(emptyRow("Activity lands in a later phase."));
    }
  }
}

let termLastPayload = "";

// termRailBusy: true while the user is mid-interaction in the rail (typing a
// search / rename) — the quiet poll must NOT rebuild the DOM under them.
function termRailBusy() {
  const a = document.activeElement;
  return !!(a && (a.classList.contains("term-rename") || a.classList.contains("term-hist-search") ||
    a.classList.contains("term-cwd")) );
}

async function loadTermSessions(quiet) {
  let d = { sessions: [], enabled: true };
  try { d = await (await fetch("/api/terminal/sessions")).json(); } catch (e) { if (quiet) return; }
  termSessions = d.sessions || [];
  const payload = JSON.stringify(termSessions) + "|" + termOpenId;
  const changed = payload !== termLastPayload;
  if (!quiet || (changed && !termRailBusy())) {
    termLastPayload = payload;
    renderTermSessions(d.enabled !== false);
    renderTermLauncher(d.enabled !== false);
    renderTermHistory();
  }
  if (quiet) return;
  if (!termOpenId) {
    const first = termSessions.find((s) => s.live);
    if (first) termOpenId = first.id;
  }
  if (termOpenId && termStage === "term") attachTerm(termOpenId);
  else if (termStage === "term") renderTermEmpty();
}

// --- rail: SESSIONS (live tmux + the currently open one) ---

function termLiveList() {
  return termSessions.filter((s) => s.live || s.id === termOpenId);
}

function renderTermSessions(enabled) {
  const host = document.getElementById("termSessionRows");
  if (!host) return;
  host.innerHTML = "";
  if (!enabled) { host.append(el("div", "term-none", "not enabled on this server")); return; }
  const list = termLiveList();
  if (!list.length) { host.append(el("div", "term-none", "no live sessions")); return; }
  list.forEach((se) => host.append(termSessionRow(se)));
}

function termSessionRow(se) {
  const row = el("div", "term-sess" + (se.id === termOpenId ? " open" : ""));
  row.append(el("span", "term-dot k-" + se.kind));
  const name = el("span", "term-sess-name");
  name.append(document.createTextNode(se.name || se.kind));
  name.append(el("span", "term-sess-host", " · " + (se.device || "metis")));
  name.title = (se.name || se.kind) + (se.cwd ? " — " + se.cwd : "");
  name.ondblclick = (e) => { e.stopPropagation(); termRenameInline(row, name, se); };
  row.append(name);
  const badges = el("span", "term-sess-badges");
  if (se.resumeId) badges.append(el("span", "term-badge", "⟳"));
  row.append(badges);
  const pen = el("button", "term-x", "✎");
  pen.title = "Rename";
  pen.onclick = (e) => { e.stopPropagation(); termRenameInline(row, name, se); };
  const x = el("button", "term-x", "✕");
  x.title = se.live ? "End session (kills the tmux)" : "Close";
  x.onclick = (e) => { e.stopPropagation(); termKill(se); };
  row.append(pen, x);
  row.onclick = () => { termOpenId = se.id; termSetStage("term"); renderTermSessions(true); attachTerm(se.id); };
  return row;
}

function termRenameInline(row, nameEl, se) {
  const inp = document.createElement("input");
  inp.className = "term-rename";
  inp.value = se.name || "";
  const commit = async () => {
    const v = inp.value.trim();
    inp.replaceWith(nameEl);
    if (!v || v === se.name) return;
    try {
      await fetch("/api/terminal/session/" + encodeURIComponent(se.id), {
        method: "PUT", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: v }),
      });
      loadTermSessions(true);
    } catch (e) {}
  };
  inp.onblur = commit;
  inp.onkeydown = (e) => {
    if (e.key === "Enter") inp.blur();
    if (e.key === "Escape") { inp.value = se.name || ""; inp.blur(); }
  };
  nameEl.replaceWith(inp);
  inp.focus(); inp.select();
}

// termKill ends a session's backend — the row survives into HISTORY
// (forgetting is history's ✕). A dead-but-open row just closes.
async function termKill(se) {
  if (se.live) {
    if (!confirm("End this session? It moves to history" + (se.resumeId ? " (resumable)" : "") + ".")) return;
    try { await fetch("/api/terminal/session/" + encodeURIComponent(se.id) + "/kill", { method: "POST" }); } catch (e) {}
  }
  if (termOpenId === se.id) { termOpenId = ""; detachTerm(); renderTermEmpty(); }
  loadTermSessions();
}

// --- rail: NEW SESSION (launcher) ---

function renderTermLauncher(enabled) {
  const host = document.getElementById("termLauncher");
  if (!host || host.dataset.built) { termSyncLauncher(); return; }
  if (!enabled) return;
  host.dataset.built = "1";

  // device slot — paint the self row instantly, refine when fleet data lands
  // (the first /devices call cold-probes ssh boxes and can take seconds)
  const devSlot = el("div", "term-dev-slot");
  devSlot.id = "termDevSlot";
  host.append(devSlot);
  renderTermDevices();
  loadTermDevices();

  // kind seg — one connected segmented control (cmd-ctr's form)
  const seg = el("div", "term-seg");
  ["shell", "claude", "codex"].forEach((k) => {
    const c = el("button", "term-seg-btn" + (termLaunch.kind === k ? " on" : ""), k);
    c.dataset.kind = k;
    c.onclick = () => { termLaunch.kind = k; termSyncLauncher(); };
    seg.append(c);
  });
  host.append(seg);

  // cwd + recent + browse
  const dirRow = el("div", "term-dir-row");
  const inp = document.createElement("input");
  inp.className = "term-cwd";
  inp.placeholder = "~ (home)";
  inp.spellcheck = false;
  inp.oninput = () => { termLaunch.cwd = inp.value.trim(); };
  inp.id = "termCwdInput";
  const recent = el("button", "term-tool", "⏱");
  recent.title = "Recent folders";
  recent.onclick = () => termToggleRecent(host, inp);
  dirRow.append(inp, recent);
  host.append(dirRow);
  const browse = el("button", "term-browse", "＋ browse files");
  browse.onclick = () => termToggleBrowse(host, browse, inp);
  host.append(browse);
  host.append(el("div", "term-browse-slot"));

  // actions — Open is the accent primary, Resume its quiet twin
  const acts = el("div", "term-launch-acts");
  acts.id = "termLaunchActs";
  const open = el("button", "term-primary", "Open");
  open.onclick = () => termCreate(termLaunch.kind, false);
  acts.append(open);
  const res = el("button", "term-secondary term-resume-btn", "Resume");
  res.onclick = () => termCreate(termLaunch.kind, true);
  res.title = "Reopen a past conversation (interactive picker)";
  acts.append(res);
  host.append(acts);
  termSyncLauncher();
}

function termSyncLauncher() {
  const host = document.getElementById("termLauncher");
  if (!host) return;
  host.querySelectorAll(".term-seg .term-seg-btn").forEach((c) => {
    c.classList.toggle("on", c.dataset.kind === termLaunch.kind);
  });
  const res = host.querySelector(".term-resume-btn");
  if (res) res.hidden = termLaunch.kind === "shell";
  const inp = host.querySelector("#termCwdInput");
  if (inp) inp.placeholder = termLaunch.kind === "shell" ? "~ (home)" : "~/project";
}

// --- fleet device selector (cmd-ctr model: dot + badges + gear override) ---

async function loadTermDevices() {
  try { termDevices = ((await (await fetch("/api/terminal/devices")).json()).devices) || []; }
  catch (e) { termDevices = []; }
  renderTermDevices();
}

function termSelectedDevice() {
  return termDevices.find((d) => (d.self ? "" : d.name) === termLaunch.device) ||
    termDevices.find((d) => d.self) || null;
}

function renderTermDevices() {
  const slot = document.getElementById("termDevSlot");
  if (!slot) return;
  slot.innerHTML = "";
  const sel = termSelectedDevice();
  // collapsed row: the current pick (+/− toggles the list)
  const cur = el("div", "term-dev-row on");
  cur.append(statusDot(!sel || sel.status === "self" || sel.status === "ok", sel ? sel.status : ""));
  cur.append(el("span", "term-dev-name", sel ? sel.name : "metis"));
  if (!sel || sel.self) cur.append(el("span", "term-badge", "this box"));
  else if (sel.status === "needs-key") cur.append(el("span", "term-badge warn", "needs key"));
  else if (sel.status === "offline") cur.append(el("span", "term-badge", "offline"));
  if (termDevices.length > 1) {
    const tog = el("button", "term-x", termDevOpen ? "−" : "+");
    tog.style.opacity = "1";
    tog.title = "Pick a device";
    tog.onclick = (e) => { e.stopPropagation(); termDevOpen = !termDevOpen; renderTermDevices(); };
    cur.append(tog);
    cur.onclick = () => { termDevOpen = !termDevOpen; renderTermDevices(); };
    cur.style.cursor = "pointer";
  }
  slot.append(cur);
  if (!termDevOpen) { termSyncOpenGate(); return; }

  const list = el("div", "term-dev-list");
  termDevices.forEach((d) => {
    const row = el("div", "term-dev-row pick" + ((d.self ? "" : d.name) === termLaunch.device ? " on" : "") + (d.status === "offline" ? " dim" : ""));
    row.append(statusDot(d.status === "self" || d.status === "ok", d.status));
    row.append(el("span", "term-dev-name", d.name));
    if (d.self) row.append(el("span", "term-badge", "this box"));
    else {
      if (d.status === "needs-key") row.append(el("span", "term-badge warn", "needs key"));
      if (d.status === "offline") row.append(el("span", "term-badge", "offline"));
      if (d.overridden) row.append(el("span", "term-badge", d.user + "@"));
      const gear = el("button", "term-x", "⚙");
      gear.title = "SSH override (user / port / key)";
      gear.onclick = (e) => { e.stopPropagation(); termToggleGear(list, row, d); };
      row.append(gear);
    }
    row.onclick = () => {
      termLaunch.device = d.self ? "" : d.name;
      termDevOpen = false;
      termBrowseOpen = false;
      const bslot = document.querySelector("#termLauncher .term-browse-slot");
      if (bslot) bslot.innerHTML = "";
      const bbtn = document.querySelector("#termLauncher .term-browse");
      if (bbtn) bbtn.textContent = "▸ browse folders";
      renderTermDevices();
    };
    list.append(row);
  });
  slot.append(list);
  termSyncOpenGate();
}

// termToggleGear opens the inline ssh-override form under a device row.
function termToggleGear(list, row, d) {
  const old = list.querySelector(".term-gear");
  if (old) { old.remove(); return; }
  const form = el("div", "term-gear");
  const mk = (ph, val) => {
    const i = document.createElement("input");
    i.className = "term-cwd";
    i.placeholder = ph;
    i.value = val || "";
    i.spellcheck = false;
    return i;
  };
  const user = mk("user", d.user);
  const port = mk("port (22)", d.port && d.port !== 22 ? String(d.port) : "");
  const ident = mk("identity file (optional, abs path on metis)", d.identity);
  form.append(user, port, ident,
    el("div", "term-gear-hint", "blank key = the box's default ssh identity"));
  const acts = el("div", "term-launch-acts");
  acts.append(pillLight("save", async () => {
    try {
      await postPUT("/api/terminal/device/" + encodeURIComponent(d.name), {
        user: user.value.trim(), port: parseInt(port.value, 10) || 0, identity: ident.value.trim(),
      });
      showToast("Saved — probing " + d.name, null, "info");
      loadTermDevices();
    } catch (e) { showToast("Couldn't save — " + (e.message || "error")); }
  }));
  if (d.overridden) {
    acts.append(pillLight("clear", async () => {
      try {
        await postPUT("/api/terminal/device/" + encodeURIComponent(d.name), { clear: true });
        loadTermDevices();
      } catch (e) {}
    }));
  }
  form.append(acts);
  row.after(form);
}

async function postPUT(url, body) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text()).slice(0, 140));
  return res.json();
}

// termSyncOpenGate disables Open/Resume with the reason when the selected
// device isn't reachable.
function termSyncOpenGate() {
  const host = document.getElementById("termLauncher");
  if (!host) return;
  const sel = termSelectedDevice();
  const blocked = sel && !sel.self && sel.status !== "ok";
  host.querySelectorAll("#termLaunchActs button").forEach((b) => {
    b.disabled = !!blocked;
    b.style.opacity = blocked ? ".45" : "";
    b.title = blocked ? (sel.status === "needs-key" ? "No ssh path — set a user/key via ⚙" : "Device is offline") : "";
  });
}

function termToggleRecent(host, inp) {
  let dd = host.querySelector(".term-recent");
  if (dd) { dd.remove(); return; }
  const dirs = [...new Set(termSessions
    .filter((s) => (s.device || "") === termLaunch.device)
    .map((s) => s.cwd).filter(Boolean))].slice(0, 8);
  dd = el("div", "term-recent");
  if (!dirs.length) dd.append(el("div", "term-none", "no recent folders"));
  dirs.forEach((d) => {
    const r = el("button", "term-recent-row", d);
    r.onclick = () => { inp.value = d; termLaunch.cwd = d; dd.remove(); };
    dd.append(r);
  });
  host.querySelector(".term-dir-row").after(dd);
}

// inline lazy directory tree (cmd-ctr's launcher picker): clicking a folder
// sets the cwd; carets expand one level at a time via /api/terminal/ls.
async function termToggleBrowse(host, btn, inp) {
  const slot = host.querySelector(".term-browse-slot");
  termBrowseOpen = !termBrowseOpen;
  btn.textContent = (termBrowseOpen ? "−" : "＋") + " browse files";
  slot.innerHTML = "";
  if (!termBrowseOpen) return;
  const tree = el("div", "term-dirpick");
  slot.append(tree);
  await termRenderTreeLevel(tree, "", inp, 0);
}

async function termRenderTreeLevel(mount, path, inp, depth) {
  let d;
  try {
    const res = await fetch("/api/terminal/ls?device=" + encodeURIComponent(termLaunch.device) + "&path=" + encodeURIComponent(path));
    if (!res.ok) { mount.append(el("div", "term-none", "unreadable")); return; }
    d = await res.json();
  } catch (e) { mount.append(el("div", "term-none", "unreachable")); return; }
  if (depth === 0) {
    termBrowsePath = d.path;
    const head = el("div", "term-dirpick-head");
    const up = el("button", "term-tool", "‹");
    up.title = "Up a level";
    up.onclick = () => {
      const parent = d.path.replace(/\/+$/, "").split("/").slice(0, -1).join("/") || "/";
      mount.innerHTML = "";
      termRenderTreeLevel(mount, parent, inp, 0);
    };
    const lbl = el("code", "term-dirpick-path", d.path);
    lbl.title = "Use this folder";
    lbl.onclick = () => { inp.value = d.path; termLaunch.cwd = d.path; };
    head.append(up, lbl);
    mount.append(head);
  }
  const dirs = (d.dirs || []).filter((x) => !x.hidden);
  if (!dirs.length && depth === 0) mount.append(el("div", "term-none", "no subfolders"));
  dirs.forEach((x) => {
    const full = d.path.replace(/\/+$/, "") + "/" + x.name;
    const row = el("div", "term-dirpick-row");
    row.style.paddingLeft = 8 + depth * 14 + "px";
    const caret = el("button", "term-dirpick-caret", "▸");
    const name = el("button", "term-dirpick-name", x.name);
    name.title = full;
    name.onclick = () => { inp.value = full; termLaunch.cwd = full; };
    let openBelow = null;
    caret.onclick = async () => {
      if (openBelow) { openBelow.remove(); openBelow = null; caret.textContent = "▸"; return; }
      caret.textContent = "▾";
      openBelow = el("div", "term-dirpick-kids");
      row.after(openBelow);
      await termRenderTreeLevel(openBelow, full, inp, depth + 1);
    };
    row.append(caret, name);
    mount.append(row);
  });
}

async function termCreate(kind, resume) {
  try {
    const body = { kind, cwd: termLaunch.cwd, device: termLaunch.device };
    if (resume) body.resumePicker = true;
    const se = await postJSONOk("/api/terminal/session", body);
    termOpenId = se.id;
    termSetStage("term");
    await loadTermSessions();
  } catch (e) { showToast("Couldn't create terminal — " + (e.message || "error")); }
}

// --- rail: HISTORY (registry rows; pinned first — smart reopen) ---

// termRelTime — cmd-ctr's short relative stamp (2m · 4h · 3d).
function termRelTime(iso) {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 90) return "now";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 172800) return Math.floor(s / 3600) + "h";
  return Math.floor(s / 86400) + "d";
}

let termHistOpen = true;
try { termHistOpen = localStorage.getItem("manifest.termHistOpen") !== "0"; } catch (e) {}

function renderTermHistory() {
  const host = document.getElementById("termHistoryRows");
  if (!host) return;
  host.innerHTML = "";
  // collapse toggle lives in the section label (cmd-ctr's − / +)
  const label = document.getElementById("termHistLabel");
  if (label && !label.dataset.built) {
    label.dataset.built = "1";
    const tog = el("button", "term-hist-tog", termHistOpen ? "−" : "＋");
    tog.onclick = () => {
      termHistOpen = !termHistOpen;
      try { localStorage.setItem("manifest.termHistOpen", termHistOpen ? "1" : "0"); } catch (e) {}
      tog.textContent = termHistOpen ? "−" : "＋";
      renderTermHistory();
    };
    label.append(tog);
  }
  let search = host.parentElement.querySelector(".term-hist-search");
  if (!termHistOpen) { if (search) search.hidden = true; return; }
  if (!search) {
    search = document.createElement("input");
    search.className = "term-hist-search";
    search.placeholder = "search sessions…";
    search.spellcheck = false;
    search.oninput = () => { termHistQuery = search.value.trim().toLowerCase(); renderTermHistory(); };
    host.before(search);
  }
  search.hidden = false;
  const liveIds = new Set(termLiveList().map((s) => s.id));
  let rows = termSessions.filter((s) => !liveIds.has(s.id));
  if (termHistQuery) {
    rows = rows.filter((s) =>
      ((s.name || "") + " " + (s.cwd || "") + " " + s.kind + " " + (s.device || "")).toLowerCase().includes(termHistQuery));
  }
  if (!rows.length) { host.append(el("div", "term-none", termHistQuery ? "no matches" : "nothing yet")); return; }
  rows.slice(0, 30).forEach((se) => {
    const row = el("div", "term-hist-row" + (se.pinned ? " pinned" : ""));
    const resumable = se.resumeId && se.kind !== "shell";
    row.append(el("span", "term-dot k-" + se.kind));
    if (resumable) row.append(el("span", "term-hist-resume", "⟳"));
    const name = el("span", "term-sess-name", se.name || se.kind);
    name.ondblclick = (e) => { e.stopPropagation(); termRenameInline(row, name, se); };
    row.append(name);
    row.append(el("span", "term-hist-when", (se.device || "metis") + " · " + termRelTime(se.lastUsed)));
    const acts = el("span", "term-hist-acts");
    const pin = el("button", "term-x" + (se.pinned ? " pinned" : ""), se.pinned ? "★" : "☆");
    pin.title = se.pinned ? "Unpin" : "Pin (floats to top, survives cleanup)";
    pin.onclick = async (e) => {
      e.stopPropagation();
      try {
        await fetch("/api/terminal/session/" + encodeURIComponent(se.id), {
          method: "PUT", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ pinned: !se.pinned }),
        });
        loadTermSessions(true);
      } catch (e2) {}
    };
    const ren = el("button", "term-x", "✎");
    ren.title = "Rename";
    ren.onclick = (e) => { e.stopPropagation(); termRenameInline(row, name, se); };
    const x = el("button", "term-x", "✕");
    x.title = "Forget this session (leaves nothing running)";
    x.onclick = async (e) => {
      e.stopPropagation();
      try { await fetch("/api/terminal/session/" + encodeURIComponent(se.id), { method: "DELETE" }); } catch (e2) {}
      loadTermSessions(true);
    };
    acts.append(pin, ren, x);
    row.append(acts);
    // reopen NOW: attach re-runs launchCmd inside the same tmux name —
    // claude/codex resume their exact conversation, a shell starts fresh there
    row.title = (se.device || "metis") + ":" + (se.cwd || "~") +
      (resumable ? " — resumes this exact conversation — no picker" : " — reopens a shell here");
    row.onclick = () => {
      termOpenId = se.id;
      termSetStage("term");
      attachTerm(se.id);
      loadTermSessions(true);
    };
    host.append(row);
  });
}

function renderTermEmpty() {
  const host = document.getElementById("termScreen");
  if (!host || termInst) return;
  host.innerHTML = "";
  const blank = el("div", "term-blank");
  blank.append(el("div", "term-blank-line", "no session on screen"));
  const quick = el("div", "term-blank-quick");
  ["shell", "claude", "codex"].forEach((k) => {
    const b = el("button", "term-blank-btn", "＋ " + k);
    b.onclick = () => { termLaunch.kind = k; termSyncLauncher(); termCreate(k, false); };
    quick.append(b);
  });
  blank.append(quick);
  host.append(blank);
}

// --- the PTY attach (unchanged core) ---

function detachTerm() {
  if (termInst) {
    try { termInst.ro.disconnect(); } catch (e) {}
    try { termInst.ws.close(); } catch (e) {}
    try { termInst.term.dispose(); } catch (e) {}
    termInst = null;
  }
}

function attachTerm(id) {
  if (typeof Terminal === "undefined") { showToast("terminal library not loaded"); return; }
  if (termInst && termInst.id === id && termInst.ws.readyState === 1) return;
  detachTerm();
  const host = document.getElementById("termScreen");
  if (!host) return;
  host.innerHTML = "";
  const mount = el("div", "term-mount");
  host.append(mount);

  // font matches the app's --mono token (Carbon/Spline) so the terminal reads
  // like the rest of the UI; read from the computed token, fall back to the OS mono.
  const appMono = getComputedStyle(document.documentElement).getPropertyValue("--mono").trim();
  const term = new Terminal({
    fontSize: 13, scrollback: 6000, cursorBlink: true,
    fontFamily: appMono || "ui-monospace, SFMono-Regular, Menlo, monospace",
    theme: { background: "#0e1116", foreground: "#c8d0da", cursor: "#265ACC" },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(mount);
  try { fit.fit(); } catch (e) {}

  // xterm measures its glyph atlas at open(); if the app mono (Spline Sans Mono)
  // hadn't finished loading yet, it locks in the fallback (Menlo) and never
  // reflows — so the terminal would look unchanged. Once fonts are ready, toggle
  // the fontFamily to force a re-measure + refit so it matches the rest of the UI.
  if (appMono && document.fonts && document.fonts.ready) {
    document.fonts.ready.then(() => {
      if (!termInst || termInst.term !== term) return;
      term.options.fontFamily = "monospace";
      term.options.fontFamily = appMono;
      try { fit.fit(); sendTermResize(); } catch (e) {}
    });
  }

  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(proto + "//" + location.host + "/api/terminal/ws?id=" + encodeURIComponent(id) + "&c=" + term.cols + "&r=" + term.rows);
  ws.binaryType = "arraybuffer";
  termInst = { term, fit, ws, id };

  const openedAt = Date.now();
  ws.onopen = () => { sendTermResize(); };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") term.write(ev.data);
    else term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (!(termInst && termInst.id === id && !els.terminalView.hidden)) return;
    // an instant close means the inner program EXITED (bad flag, crash) —
    // reattaching would just respawn it in a loop. Stop and say so.
    if (Date.now() - openedAt < 2500) {
      term.write("\r\n\x1b[2m[session ended — click it in the rail to relaunch]\x1b[0m\r\n");
      loadTermSessions(true);
      return;
    }
    term.write("\r\n\x1b[2m[disconnected — reattaching…]\x1b[0m\r\n");
    setTimeout(() => { if (termOpenId === id) attachTerm(id); }, 1200);
  };
  term.onData((d) => { if (ws.readyState === 1) ws.send(JSON.stringify({ t: "i", d })); });

  const ro = new ResizeObserver(() => { try { fit.fit(); sendTermResize(); } catch (e) {} });
  ro.observe(mount);
  termInst.ro = ro;

  // voice → terminal: the mic lives in the page header's actions slot
  if (typeof micButton === "function") {
    const head = document.getElementById("termHeadActions");
    if (head && !head.querySelector(".mic-btn")) {
      head.append(micButton((text) => {
        if (termInst && termInst.ws.readyState === 1) termInst.ws.send(JSON.stringify({ t: "i", d: text }));
      }));
    }
  }
  buildTermKeys(ws);
  term.focus();
}

function sendTermResize() {
  if (!termInst || termInst.ws.readyState !== 1) return;
  termInst.ws.send(JSON.stringify({ t: "r", c: termInst.term.cols, r: termInst.term.rows }));
}

// mobile soft-key bar (cmd-ctr TermKeyBar shape): keys the touch keyboard lacks
function buildTermKeys(ws) {
  const bar = document.getElementById("termKeys");
  if (!bar || bar.dataset.built) return;
  bar.dataset.built = "1";
  const send = (d) => { if (ws.readyState === 1) ws.send(JSON.stringify({ t: "i", d })); };
  const keys = [
    ["esc", "\x1b"], ["tab", "\t"], ["ctrl-c", "\x03"], ["ctrl-d", "\x04"],
    ["↑", "\x1b[A"], ["↓", "\x1b[B"], ["←", "\x1b[D"], ["→", "\x1b[C"],
    ["|", "|"], ["~", "~"], ["/", "/"],
  ];
  keys.forEach(([label, code]) => {
    const b = el("button", "term-key", label);
    b.onclick = () => { send(code); if (termInst) termInst.term.focus(); };
    bar.append(b);
  });
}

// ⌘K + palette
cmdRegistry.register(() => [
  { id: "goto:#/terminal", name: "Terminal", hint: "cockpit · view", keywords: "terminal shell console ssh claude",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => termSetStage("term"), 50); } },
  { id: "goto:files-stage", name: "Files", hint: "cockpit · view", keywords: "files fleet browse filesystem",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => termSetStage("files"), 50); } },
  { id: "goto:activity-stage", name: "Activity", hint: "cockpit · view", keywords: "activity stats cpu memory fleet monitor",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => termSetStage("activity"), 50); } },
  { id: "act:new-claude-term", name: "New Claude terminal", hint: "terminal · action", keywords: "claude code resume terminal",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => { termLaunch.kind = "claude"; termCreate("claude", false); }, 250); } },
]);
