// ---- TERMINAL: the cockpit (cmd-ctr terminal-module parity) ----
// One surface: a rail of SESSIONS / NEW SESSION / HISTORY with an icon tab bar
// pinned at its foot, and a stage swapping Terminal (PTY over metis tmux) /
// Files (fleet browser) / Activity (fleet stats). Every session — local or
// remote — is a metis tmux, so keep-alive is inherent. xterm vendored (UMD).

let termSessions = [];
let termOpenId = "";
let termInst = null;   // { term, fit, ws, id, ro }
let termStage = "term";           // term | files | activity
let termLaunch = { kind: "shell", cwd: "" };
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
  loadTermSessions();
  if (!termPollTimer) termPollTimer = setInterval(() => {
    if (els.terminalView && !els.terminalView.hidden && !document.hidden) loadTermSessions(true);
  }, 5000);
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

async function loadTermSessions(quiet) {
  let d = { sessions: [], enabled: true };
  try { d = await (await fetch("/api/terminal/sessions")).json(); } catch (e) { if (quiet) return; }
  termSessions = d.sessions || [];
  renderTermSessions(d.enabled !== false);
  renderTermLauncher(d.enabled !== false);
  renderTermHistory();
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
  const name = el("span", "term-sess-name", se.name || se.kind);
  name.title = (se.name || se.kind) + (se.cwd ? " — " + se.cwd : "");
  name.ondblclick = (e) => { e.stopPropagation(); termRenameInline(row, name, se); };
  row.append(name);
  const badges = el("span", "term-sess-badges");
  if (se.device) badges.append(el("span", "term-badge", se.device));
  if (se.resumeId) badges.append(el("span", "term-badge", "⟳"));
  row.append(badges);
  const x = el("button", "term-x", "✕");
  x.title = se.live ? "End session (kills the tmux)" : "Close";
  x.onclick = (e) => { e.stopPropagation(); termKill(se); };
  row.append(x);
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

async function termKill(se) {
  if (!confirm(se.live ? "End this session? The tmux backend is killed for good." : "Forget this session?")) return;
  try { await fetch("/api/terminal/session/" + encodeURIComponent(se.id), { method: "DELETE" }); } catch (e) {}
  if (termOpenId === se.id) { termOpenId = ""; detachTerm(); renderTermEmpty(); }
  loadTermSessions();
}

// --- rail: NEW SESSION (launcher) ---

function renderTermLauncher(enabled) {
  const host = document.getElementById("termLauncher");
  if (!host || host.dataset.built) { termSyncLauncher(); return; }
  if (!enabled) return;
  host.dataset.built = "1";

  // device slot — metis-only v1; Phase 2 replaces this with the fleet list
  const dev = el("div", "term-dev-row on");
  dev.append(statusDot(true), el("span", "term-dev-name", "metis"), el("span", "term-badge", "this box"));
  host.append(dev);

  // kind seg
  const seg = el("div", "term-seg");
  ["shell", "claude", "codex"].forEach((k) => {
    const c = el("button", "filter-chip" + (termLaunch.kind === k ? " on" : ""), k);
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
  const browse = el("button", "term-browse", "▸ browse folders");
  browse.onclick = () => termToggleBrowse(host, browse, inp);
  host.append(browse);
  host.append(el("div", "term-browse-slot"));

  // actions
  const acts = el("div", "term-launch-acts");
  acts.append(pill("Open", () => termCreate(termLaunch.kind, false)));
  const res = pillLight("Resume", () => termCreate(termLaunch.kind, true));
  res.className += " term-resume-btn";
  res.title = "Reopen a past conversation (interactive picker)";
  acts.append(res);
  host.append(acts);
  termSyncLauncher();
}

function termSyncLauncher() {
  const host = document.getElementById("termLauncher");
  if (!host) return;
  host.querySelectorAll(".term-seg .filter-chip").forEach((c) => {
    c.classList.toggle("on", c.dataset.kind === termLaunch.kind);
  });
  const res = host.querySelector(".term-resume-btn");
  if (res) res.hidden = termLaunch.kind === "shell";
  const inp = host.querySelector("#termCwdInput");
  if (inp) inp.placeholder = termLaunch.kind === "shell" ? "~ (home)" : "~/project";
}

function termToggleRecent(host, inp) {
  let dd = host.querySelector(".term-recent");
  if (dd) { dd.remove(); return; }
  const dirs = [...new Set(termSessions.map((s) => s.cwd).filter(Boolean))].slice(0, 8);
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
  btn.textContent = (termBrowseOpen ? "▾" : "▸") + " browse folders";
  slot.innerHTML = "";
  if (!termBrowseOpen) return;
  const tree = el("div", "term-dirpick");
  slot.append(tree);
  await termRenderTreeLevel(tree, "", inp, 0);
}

async function termRenderTreeLevel(mount, path, inp, depth) {
  let d;
  try {
    const res = await fetch("/api/terminal/ls?path=" + encodeURIComponent(path));
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
    const body = { kind, cwd: termLaunch.cwd };
    if (resume) body.resumePicker = true;
    const se = await postJSONOk("/api/terminal/session", body);
    termOpenId = se.id;
    termSetStage("term");
    await loadTermSessions();
  } catch (e) { showToast("Couldn't create terminal — " + (e.message || "error")); }
}

// --- rail: HISTORY (registry rows; pinned first — smart reopen) ---

function renderTermHistory() {
  const host = document.getElementById("termHistoryRows");
  if (!host) return;
  host.innerHTML = "";
  let search = host.parentElement.querySelector(".term-hist-search");
  if (!search) {
    search = document.createElement("input");
    search.className = "term-hist-search";
    search.placeholder = "search…";
    search.spellcheck = false;
    search.oninput = () => { termHistQuery = search.value.trim().toLowerCase(); renderTermHistory(); };
    host.before(search);
  }
  const liveIds = new Set(termLiveList().map((s) => s.id));
  let rows = termSessions.filter((s) => !liveIds.has(s.id));
  if (termHistQuery) {
    rows = rows.filter((s) =>
      ((s.name || "") + " " + (s.cwd || "") + " " + s.kind + " " + (s.device || "")).toLowerCase().includes(termHistQuery));
  }
  if (!rows.length) { host.append(el("div", "term-none", termHistQuery ? "no matches" : "nothing yet")); return; }
  rows.slice(0, 30).forEach((se) => {
    const row = el("div", "term-hist-row" + (se.pinned ? " pinned" : ""));
    row.append(el("span", "term-dot k-" + se.kind));
    const name = el("span", "term-sess-name", se.name || se.kind);
    name.title = (se.cwd || "") + " · " + (se.lastUsed || "");
    row.append(name);
    row.append(el("span", "term-hist-when", fmtWhen(se.lastUsed)));
    const acts = el("span", "term-hist-acts");
    const pin = el("button", "term-x", se.pinned ? "★" : "☆");
    pin.title = se.pinned ? "Unpin" : "Pin (survives cleanup)";
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
    x.title = "Forget (leaves nothing running)";
    x.onclick = async (e) => {
      e.stopPropagation();
      try { await fetch("/api/terminal/session/" + encodeURIComponent(se.id), { method: "DELETE" }); } catch (e2) {}
      loadTermSessions(true);
    };
    acts.append(pin, ren, x);
    row.append(acts);
    // smart reopen: attaching re-runs launchCmd — claude/codex resume via their
    // minted id, a dead shell simply starts fresh in the same tmux name/cwd.
    row.onclick = () => { termOpenId = se.id; termSetStage("term"); loadTermSessions(); };
    host.append(row);
  });
}

function renderTermEmpty() {
  const host = document.getElementById("termScreen");
  if (host && !termInst) {
    host.innerHTML = "";
    host.append(el("div", "term-blank", "❯ start a session — pick a kind and Open"));
  }
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

  const term = new Terminal({
    fontSize: 13, scrollback: 6000, cursorBlink: true,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    theme: { background: "#0e1116", foreground: "#c8d0da", cursor: "#265ACC" },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(mount);
  try { fit.fit(); } catch (e) {}

  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(proto + "//" + location.host + "/api/terminal/ws?id=" + encodeURIComponent(id) + "&c=" + term.cols + "&r=" + term.rows);
  ws.binaryType = "arraybuffer";
  termInst = { term, fit, ws, id };

  ws.onopen = () => { sendTermResize(); };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") term.write(ev.data);
    else term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (termInst && termInst.id === id && !els.terminalView.hidden) {
      term.write("\r\n\x1b[2m[disconnected — reattaching…]\x1b[0m\r\n");
      setTimeout(() => { if (termOpenId === id) attachTerm(id); }, 1200);
    }
  };
  term.onData((d) => { if (ws.readyState === 1) ws.send(JSON.stringify({ t: "i", d })); });

  const ro = new ResizeObserver(() => { try { fit.fit(); sendTermResize(); } catch (e) {} });
  ro.observe(mount);
  termInst.ro = ro;

  // voice → terminal (reuse the mic component)
  if (typeof micButton === "function") {
    const bar = document.getElementById("termActions");
    if (bar && !bar.querySelector(".mic-btn")) {
      bar.append(micButton((text) => { if (ws.readyState === 1) ws.send(JSON.stringify({ t: "i", d: text })); }));
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
