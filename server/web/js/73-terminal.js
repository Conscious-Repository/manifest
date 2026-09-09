// ---- TERMINAL: open and attach to live processes ----
// Conversation associations belong in Chats; run history belongs to the board.
// Inventory comes from the runtime, while Files and stats retain their stages.
let termSessions = [];
let termOpenId = "";
let termInst = null;
let termStage = "term";
let termLaunch = { kind: "shell", cwd: "", device: "" };
let termDevices = [];
let termKeepPref = true;
let termConnectivity = "unavailable";
let termInventoryRequest = null;
let termInventoryAgain = false;
let termListenersReady = false;
try { termKeepPref = localStorage.getItem("manifest.termKeep") !== "0"; } catch (e) {}

const TERM_STAGES = [
  { stage: "term", glyph: "❯", label: "terminal" },
  { stage: "files", glyph: "▤", label: "files" },
  { stage: "activity", glyph: "∿", label: "activity" },
];
function showTerminal() {
  if (location.hash.startsWith("#/terminal/")) {
    try { termOpenId = decodeURIComponent(location.hash.slice("#/terminal/".length)); termStage = "term"; } catch (e) {}
  }
  try { termStage = localStorage.getItem("manifest.termStage") || termStage; } catch (e) {}
  if (!TERM_STAGES.some((s) => s.stage === termStage)) termStage = "term";
  if (location.hash.startsWith("#/terminal/")) termStage = "term";
  renderTermTabbar(); termApplyStage(); termFitShell();
  if (typeof ensureTerminalEvents === "function") ensureTerminalEvents();
  if (!termListenersReady) {
    termListenersReady = true;
    window.addEventListener("resize", termFitShell);
    window.visualViewport?.addEventListener("resize", termFitShell);
    window.addEventListener("manifest-terminal-state", () => {
      if (els.terminalView && !els.terminalView.hidden && !document.hidden) loadTermSessions(true);
    });
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden && els.terminalView && !els.terminalView.hidden) loadTermSessions(true);
    });
  }
  loadTermSessions();
}
function termRenderControls() {
  const pane = document.getElementById("termStageTerm");
  if (!pane) return;
  let toolbar = document.getElementById("termControls");
  if (!toolbar) {
    toolbar = el("div", "term-controls"); toolbar.id = "termControls";
    const sessions = document.createElement("select"); sessions.id = "termSessionSelect";
    sessions.setAttribute("aria-label", "Switch running session");
    sessions.onchange = () => { termOpenId = sessions.value; attachTerm(termOpenId); renderTermSessions(true); history.replaceState(null, "", "#/terminal/" + encodeURIComponent(termOpenId)); };
    const launcher = el("button", "term-key", "New / sessions");
    launcher.onclick = () => { document.querySelector(".term-shell").classList.toggle("term-nav-open"); termFitShell(); };
    const keyboard = el("button", "term-key", "Keyboard");
    keyboard.onclick = () => { if (termInst) termInst.term.focus(); };
    const latest = el("button", "term-key", "Latest ↓");
    latest.onclick = () => { if (termInst) termInst.term.scrollToBottom(); };
    const reconnect = el("button", "term-key", "Reconnect");
    reconnect.onclick = async () => { detachTerm(); await loadTermSessions(); };
    const separate = el("a", "term-key", "New tab ↗"); separate.id = "termSeparate"; separate.target = "_blank"; separate.rel = "noopener";
    const status = el("span", "term-connection", "Select a session"); status.id = "termConnection"; status.setAttribute("role", "status");
    toolbar.append(sessions, launcher, keyboard, latest, reconnect, separate, status);
    pane.prepend(toolbar);
  }
  const select = toolbar.querySelector("select");
  select.replaceChildren();
  if (!termSessions.length) select.append(el("option", "", "No running sessions"));
  termSessions.forEach(session => { const option = el("option", "", session.name || session.kind); option.value = session.id; select.append(option); });
  select.value = termOpenId;
  const separate = document.getElementById("termSeparate");
  separate.href = "#/terminal/" + encodeURIComponent(termOpenId);
  separate.hidden = !termOpenId;
}
function termFitShell() {
  const shell = document.querySelector("#terminalView .term-shell");
  if (!shell || els.terminalView.hidden) return;
  if (window.innerWidth <= 860) {
    const height = window.visualViewport ? window.visualViewport.height : window.innerHeight;
    shell.style.height = Math.max(200, height - shell.getBoundingClientRect().top - 8) + "px";
    if (termInst) { try { termInst.fit.fit(); sendTermResize(); } catch (e) {} }
    return;
  }
  shell.style.height = Math.max(320, window.innerHeight - shell.getBoundingClientRect().top - 14) + "px";
  if (termInst) { try { termInst.fit.fit(); sendTermResize(); } catch (e) {} }
}
function renderTermTabbar() {
  const bar = document.getElementById("termTabbar"); if (!bar) return;
  bar.replaceChildren();
  TERM_STAGES.forEach((t) => {
    const b = el("button", "term-tab" + (t.stage === termStage ? " on" : ""));
    b.append(el("span", "term-tab-glyph", t.glyph), el("span", "term-tab-label", t.label));
    b.onclick = () => termSetStage(t.stage); bar.append(b);
  });
}
function termSetStage(stage) {
  termStage = stage;
  try { localStorage.setItem("manifest.termStage", stage); } catch (e) {}
  renderTermTabbar(); termApplyStage();
}
function termApplyStage() {
  Object.entries({ term: "termStageTerm", files: "termStageFiles", activity: "termStageActivity" }).forEach(([k, id]) => {
    const pane = document.getElementById(id); if (pane) pane.hidden = k !== termStage;
  });
  if (termStage === "term" && termInst) { try { termInst.fit.fit(); sendTermResize(); } catch (e) {} }
  if (termStage === "files" && typeof showFilesStage === "function") showFilesStage();
  if (termStage === "activity") showActivityStage();
}
async function loadTermSessions(quiet) {
  if (termInventoryRequest) { termInventoryAgain = true; return termInventoryRequest; }
  termInventoryRequest = (async () => {
    let data;
    try {
      const response = await fetch("/api/terminal/live");
      if (!response.ok) throw new Error("live inventory unavailable");
      data = await response.json();
    } catch (e) { data = { sessions: [], enabled: true, connectivity: "unavailable" }; }
    termSessions = (data.sessions || []).filter((se) => se.live);
    termConnectivity = data.connectivity || "unavailable";
    renderTermSessions(data.enabled !== false);
    renderTermLauncher(data.enabled !== false);
    const selected = termSessions.find((se) => se.id === termOpenId);
    if (termOpenId && !selected && termConnectivity === "connected") {
      // A known stopped pane is never reopened from Terminal history.
      detachTerm();
      renderTermEmpty("pane ended · resume conversations in Chats");
    } else if (!els.terminalView.hidden && selected && termStage === "term" && (!quiet || !termInst)) attachTerm(selected.id);
    else if (!quiet && termStage === "term") renderTermEmpty();
  })();
  try { await termInventoryRequest; } finally {
    termInventoryRequest = null;
    if (termInventoryAgain) { termInventoryAgain = false; loadTermSessions(true); }
  }
}
function renderTermSessions(enabled) {
  termRenderControls();
  const host = document.getElementById("termSessionRows"); if (!host) return;
  host.replaceChildren();
  const status = document.getElementById("termRuntimeStatus");
  if (status) status.textContent = !enabled ? "disabled" : termConnectivity === "connected" ? String(termSessions.length) + " live" : "unavailable";
  if (!enabled) { host.append(el("div", "term-none", "not enabled on this server")); return; }
  if (!termSessions.length) { host.append(el("div", "term-none", termConnectivity === "connected" ? "no live panes" : "runtime unavailable")); return; }
  termSessions.forEach((se) => host.append(termSessionRow(se)));
}
function termSessionRow(se) {
  const row = el("div", "term-sess" + (se.id === termOpenId ? " open" : ""));
  row.append(statusDot(true, "process available"));
  const name = el("span", "term-sess-name", se.name || se.kind || "shell");
  const host = se.device || (se.runtime && se.runtime.host) || "local";
  name.append(el("span", "term-sess-host", " · " + host));
  name.title = (se.cwd || "~") + (se.runtime && se.runtime.pane ? " · " + se.runtime.pane : "");
  row.append(name);
  if (se.keep) { const keep = el("span", "term-badge", "kept"); keep.title = "survives remote connection loss"; row.append(keep); }
  const end = armedDelete("✕", "end — sure?", () => termKill(se));
  end.className = "term-x"; end.title = "end this process"; row.append(end);
  row.onclick = () => {
    termOpenId = se.id; termSetStage("term"); renderTermSessions(true); attachTerm(se.id);
    history.replaceState(null, "", "#/terminal/" + encodeURIComponent(se.id));
  };
  if (row.onclick) {
    row.tabIndex = 0;
    row.setAttribute("role", "link");
    row.addEventListener("keydown", event => {
      if (event.target === row && event.key === "Enter") { event.preventDefault(); row.click(); }
    });
  }
  return row;
}
function termRuntimeKey(se) {
  const id = se.runtime || {};
  return [se.backend || "tmux", se.id, id.host, id.generation, id.session, id.workspace, id.pane, id.occupant, id.agentSession].join("|");
}
function termAttachQuery(se) {
  return se.id.startsWith("live:") ? "handle=" + encodeURIComponent(se.handle) : "id=" + encodeURIComponent(se.id);
}
async function termKill(se) {
  try {
    const orphan = se.id.startsWith("live:");
    const response = await fetch(orphan ? "/api/terminal/live/close" : "/api/terminal/session/" + encodeURIComponent(se.id) + "/kill", orphan
      ? { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ handle: se.handle }) }
      : { method: "POST" });
    if (!response.ok) throw new Error((await response.text()).slice(0, 160));
  } catch (e) { showToast("end failed — " + (e.message || "error")); return; }
  if (termOpenId === se.id) { termOpenId = ""; detachTerm(); renderTermEmpty(); }
  await loadTermSessions(true);
}
function renderTermLauncher(enabled) {
  const host = document.getElementById("termLauncher"); if (!host) return;
  if (host.dataset.built) { termSyncLauncher(); return; }
  if (!enabled) return;
  host.dataset.built = "1";
  const seg = el("div", "term-seg");
  ["shell", "claude", "codex"].forEach((kind) => {
    const button = el("button", "term-seg-btn", kind); button.dataset.kind = kind;
    button.onclick = () => { termLaunch.kind = kind; termSyncLauncher(); }; seg.append(button);
  });
  host.append(seg);
  const cwd = document.createElement("input");
  cwd.id = "termCwdInput"; cwd.className = "term-cwd"; cwd.spellcheck = false;
  cwd.placeholder = "Working folder · home by default"; cwd.value = termLaunch.cwd;
  cwd.setAttribute("aria-label", "working directory");
  cwd.oninput = () => { termLaunch.cwd = cwd.value.trim(); };
  cwd.onkeydown = (event) => { if (event.key === "Enter") { event.preventDefault(); termCreate(termLaunch.kind); } };
  host.append(cwd);
  const devices = document.createElement("select"); devices.id = "termDeviceSelect"; devices.className = "term-device-select";
  devices.setAttribute("aria-label", "host");
  devices.onchange = () => { termLaunch.device = devices.value; termSyncLauncher(); };
  host.append(devices);
  renderTermDevices(); loadTermDevices();
  const keep = el("button", "term-keep"); keep.id = "termKeepToggle";
  keep.onclick = () => {
    termKeepPref = !termKeepPref;
    try { localStorage.setItem("manifest.termKeep", termKeepPref ? "1" : "0"); } catch (e) {}
    termSyncLauncher();
  };
  host.append(keep);
  const open = el("button", "term-primary", "open"); open.id = "termOpenButton";
  open.onclick = () => termCreate(termLaunch.kind); host.append(open);
  termSyncLauncher();
}
async function loadTermDevices() {
  try { termDevices = (await (await fetch("/api/terminal/devices")).json()).devices || []; } catch (e) { termDevices = []; }
  renderTermDevices(); termSyncLauncher();
}
function termSelectedDevice() {
  return termDevices.find((d) => (d.self ? "" : d.name) === termLaunch.device) || termDevices.find((d) => d.self) || null;
}
function renderTermDevices() {
  const select = document.getElementById("termDeviceSelect"); if (!select) return;
  select.replaceChildren();
  const devices = termDevices.length ? termDevices : [{ name: "This server", self: true }];
  devices.forEach((d) => {
    const option = el("option", "", d.name + (d.self ? " · local" : d.status !== "ok" ? " · " + d.status : ""));
    option.value = d.self ? "" : d.name; select.append(option);
  });
  select.value = termLaunch.device;
}
function termSyncLauncher() {
  const host = document.getElementById("termLauncher"); if (!host) return;
  host.querySelectorAll(".term-seg-btn").forEach((button) => button.classList.toggle("on", button.dataset.kind === termLaunch.kind));
  const selected = termSelectedDevice(), remote = !!(selected && !selected.self);
  const keep = document.getElementById("termKeepToggle");
  if (keep) {
    keep.hidden = !remote; keep.disabled = !remote || !selected.caffeinate;
    keep.classList.toggle("on", remote && selected.caffeinate && termKeepPref);
    keep.textContent = selected && selected.caffeinate ? "keep alive · " + (termKeepPref ? "on" : "off") : "keep alive unavailable";
  }
  const open = document.getElementById("termOpenButton");
  if (open) {
    open.disabled = termCreating || (remote ? selected.status !== "ok" : termConnectivity !== "connected");
    open.textContent = termCreating ? "Opening…" : "Open terminal";
    open.title = open.disabled ? "host unavailable" : "open a new process";
  }
}
let termCreating = false;
async function termCreate(kind) {
  if (termCreating) return;
  termCreating = true;
  termSyncLauncher();
  try {
    const selected = termSelectedDevice();
    const body = { kind, cwd: termLaunch.cwd, device: termLaunch.device };
    if (selected && !selected.self && selected.caffeinate && termKeepPref) body.keep = true;
    const session = await postJSONOk("/api/terminal/session", body);
    termOpenId = session.id; termSetStage("term"); await loadTermSessions();
  } catch (e) { showToast("open failed — " + (e.message || "error")); }
  finally { termCreating = false; termSyncLauncher(); }
}
function renderTermEmpty(message) {
  const host = document.getElementById("termScreen"); if (!host || termInst) return;
  host.replaceChildren();
  const blank = el("div", "term-blank");
  blank.append(el("div", "term-blank-line", message || "Open a terminal or select a running session")); host.append(blank);
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
  const session = termSessions.find((se) => se.id === id && se.live);
  if (!session) { renderTermEmpty("pane unavailable · resume conversations in Chats"); return; }
  const runtimeKey = termRuntimeKey(session);
  if (typeof Terminal === "undefined") { showToast("terminal library not loaded"); return; }
  if (termInst && termInst.id === id && termInst.runtimeKey === runtimeKey && termInst.ws.readyState === 1) return;
  detachTerm();
  const shell = document.querySelector(".term-shell");
  if (shell) { shell.classList.add("term-session-active"); shell.classList.remove("term-nav-open"); }
  termFitShell();
  termRenderControls();
  const host = document.getElementById("termScreen");
  if (!host) return;
  host.innerHTML = "";
  const connection = document.getElementById("termConnection");
  if (connection) connection.textContent = "Connecting…";
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

  // OSC 52 → clipboard (cmd-ctr): tmux advertises set-clipboard, so an inner
  // app's copy (Claude Code's `c`, vim "+y) reaches the system clipboard.
  term.parser.registerOscHandler(52, (data) => {
    try {
      const b64 = data.slice(data.indexOf(";") + 1);
      const text = decodeURIComponent(escape(atob(b64)));
      if (text && navigator.clipboard && window.isSecureContext) navigator.clipboard.writeText(text);
    } catch (e) {}
    return true;
  });

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
  const ws = new WebSocket(proto + "//" + location.host + "/api/terminal/ws?" + termAttachQuery(session) + "&c=" + term.cols + "&r=" + term.rows);
  ws.binaryType = "arraybuffer";
  termInst = { term, fit, ws, id, runtimeKey };

  ws.onopen = () => {
    if (termInst && termInst.ws === ws && connection) connection.textContent = "Connected";
    sendTermResize();
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") term.write(ev.data);
    else term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (!(termInst && termInst.ws === ws && !els.terminalView.hidden)) return;
    if (connection) connection.textContent = "Disconnected · retrying";
    term.write("\r\n\x1b[2m[attachment disconnected]\x1b[0m\r\n");
    // A browser socket closing says nothing about the process. Reattach only
    // after fresh live inventory proves the exact same pane still exists.
    setTimeout(async () => {
      if (els.terminalView.hidden || termStage !== "term" || !termInst || termInst.ws !== ws) return;
      await loadTermSessions(true);
      const current = termSessions.find((se) => se.id === id && se.live);
      if (!els.terminalView.hidden && termStage === "term" && termOpenId === id && termInst && termInst.ws === ws && current && termRuntimeKey(current) === runtimeKey) attachTerm(id);
    }, 1200);
  };
  term.onData((d) => { if (ws.readyState === 1) ws.send(JSON.stringify({ t: "i", d })); });

  // a hidden view collapses the mount to 0×0: fitting then would shrink the
  // tmux window to a few columns (the CLI redraws at that width for every
  // other reader — the chat's live strip, Alfred's capture-pane) — so only a
  // mount with real size drives the pty size
  const ro = new ResizeObserver(() => {
    if (!mount.clientWidth || !mount.clientHeight) return;
    try { fit.fit(); sendTermResize(); } catch (e) {}
  });
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
  buildTermKeys();
  if (window.innerWidth > 860) term.focus();
}

function sendTermResize() {
  if (!termInst || termInst.ws.readyState !== 1) return;
  termInst.ws.send(JSON.stringify({ t: "r", c: termInst.term.cols, r: termInst.term.rows }));
}

// TERM_KEY_CODES — the raw bytes behind the soft keys: the terminal's key bar
// below and the chat surface's live-strip quick keys (48-chat.js, Stage S)
// send the same codes, one definition.
const TERM_KEY_CODES = {
  esc: "\x1b", tab: "\t", enter: "\r", "ctrl-c": "\x03", "ctrl-d": "\x04", "shift-tab": "\x1b[Z",
  "↑": "\x1b[A", "↓": "\x1b[B", "←": "\x1b[D", "→": "\x1b[C",
};

// mobile soft-key bar (cmd-ctr TermKeyBar shape): keys the touch keyboard lacks.
// Sends via the LIVE termInst.ws — the bar builds once, sessions come and go
// (wiring it to the first socket left the keys dead after any reattach).
function buildTermKeys() {
  const bar = document.getElementById("termKeys");
  if (!bar || bar.dataset.built) return;
  bar.dataset.built = "1";
  const send = (d) => {
    if (termInst && termInst.ws.readyState === 1) termInst.ws.send(JSON.stringify({ t: "i", d }));
  };
  const keys = [
    ["enter", TERM_KEY_CODES.enter], ["esc", TERM_KEY_CODES.esc], ["tab", TERM_KEY_CODES.tab], ["shift-tab", TERM_KEY_CODES["shift-tab"]], ["ctrl-c", TERM_KEY_CODES["ctrl-c"]], ["ctrl-d", TERM_KEY_CODES["ctrl-d"]],
    ["↑", TERM_KEY_CODES["↑"]], ["↓", TERM_KEY_CODES["↓"]], ["←", TERM_KEY_CODES["←"]], ["→", TERM_KEY_CODES["→"]],
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
  { id: "act:new-claude-term", name: "open Claude terminal", hint: "terminal · action", keywords: "claude code open terminal",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => { termLaunch.kind = "claude"; termCreate("claude"); }, 250); } },
]);
