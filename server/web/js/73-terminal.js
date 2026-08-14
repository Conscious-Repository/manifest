// ---- TERMINAL: in-app PTY over tmux (cmd-ctr import) ----
// A real terminal on metis (where manifest, the engine, the vault, and the
// claude-sub CLI all live). tmux keeps sessions alive across disconnect and
// navigation; claude/codex presets + a minted resume id make "manage Claude
// Code from inside the app" one click. xterm.js is vendored (UMD, no build).

let termSessions = [];
let termOpenId = "";
let termInst = null;   // { term, fit, ws, id }

function showTerminal() {
  loadTermSessions();
}

async function loadTermSessions() {
  let d = { sessions: [], enabled: true };
  try { d = await (await fetch("/api/terminal/sessions")).json(); } catch (e) {}
  termSessions = d.sessions || [];
  renderTermRail(d.enabled !== false);
  if (!termOpenId && termSessions.length) termOpenId = termSessions[0].id;
  if (termOpenId) attachTerm(termOpenId);
  else renderTermEmpty();
}

function renderTermRail(enabled) {
  const host = document.getElementById("termRail");
  if (!host) return;
  host.innerHTML = "";
  if (!enabled) { host.append(emptyRow("Terminal not enabled on this server.")); return; }
  const launch = el("div", "term-launch");
  ["shell", "claude", "codex"].forEach((kind) => {
    const b = el("button", "pill" + (kind === "shell" ? " light" : ""), "＋ " + kind);
    b.onclick = () => termCreate(kind);
    launch.append(b);
  });
  host.append(launch);
  termSessions.forEach((se) => {
    const row = el("div", "term-rail-row" + (se.id === termOpenId ? " open" : ""));
    const top = el("div", "term-rail-top");
    if (se.live) top.append(el("span", "term-live", "●"));
    top.append(el("span", "term-rail-name", se.name || se.kind));
    row.append(top);
    const meta = el("div", "term-rail-meta");
    meta.append(el("span", "term-kind k-" + se.kind, se.kind));
    if (se.resumeId && se.kind === "claude") meta.append(el("span", "term-resume", "resumable"));
    row.append(meta);
    row.onclick = () => { termOpenId = se.id; renderTermRail(true); attachTerm(se.id); };
    const x = el("button", "term-x", "✕");
    x.title = "End session";
    x.onclick = async (e) => {
      e.stopPropagation();
      if (!confirm("End this terminal session?")) return;
      try { await fetch("/api/terminal/session/" + encodeURIComponent(se.id), { method: "DELETE" }); } catch (e2) {}
      if (termOpenId === se.id) { termOpenId = ""; detachTerm(); }
      loadTermSessions();
    };
    row.append(x);
    host.append(row);
  });
}

async function termCreate(kind) {
  try {
    const se = await postJSONOk("/api/terminal/session", { kind });
    termOpenId = se.id;
    await loadTermSessions();
  } catch (e) { showToast("Couldn't create terminal — " + (e.message || "error")); }
}

function renderTermEmpty() {
  const host = document.getElementById("termScreen");
  if (host) { host.innerHTML = ""; host.append(emptyRow("Start a terminal — ＋ shell, ＋ claude, or ＋ codex.")); }
}

function detachTerm() {
  if (termInst) { try { termInst.ws.close(); } catch (e) {} try { termInst.term.dispose(); } catch (e) {} termInst = null; }
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

// ⌘K + palette: jump to / start a terminal
cmdRegistry.register(() => [
  { id: "goto:#/terminal", name: "Terminal", hint: "server · view", keywords: "terminal shell console ssh claude",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; } },
  { id: "act:new-claude-term", name: "New Claude terminal (metis)", hint: "terminal · action", keywords: "claude code resume terminal",
    act: () => { closeCmdbar(); location.hash = "#/terminal"; setTimeout(() => termCreate("claude"), 250); } },
]);
