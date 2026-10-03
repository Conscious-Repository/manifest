// LIVE STATUS LINE — one quiet line between the conversation and the composer,
// after the flagship harnesses (Codex "Working (4m 07s · esc to interrupt)",
// Claude Code's spinner): while an agent works it names the step in progress
// and counts the time since the run started; Stop sends the agent's own
// interrupt (Esc for Codex and Claude Code; the native agents keep their
// receipt-backed interrupt controls in the transcript). At rest it shows how
// full the model's context is, when the agent recorded it.
// Every number comes from what the agent or runtime recorded: the run's start
// (transcript run evidence or the delivery's running time), the latest step
// (transcript), the context (the CLI's own token accounting). Nothing is
// estimated; what was not recorded is not shown.

function chatDuration(ms) {
  if (!Number.isFinite(ms) || ms < 1000) return "";
  const s = Math.floor(ms / 1000), h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
  if (h) return h + "h " + String(m).padStart(2, "0") + "m";
  if (m) return m + "m " + String(r).padStart(2, "0") + "s";
  return r + "s";
}
function chatTokens(n) { return n >= 1e6 ? (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + "M" : n >= 1000 ? Math.round(n / 1000) + "k" : String(n); }

// chatStatusModel — what the line should say for the conversation in view.
function chatStatusModel() {
  if (chatIsTerm()) {
    const o = chatTermOpen;
    if (!o || o.se.id !== chatOpenId) return null;
    const listed = chatTermFind(o.se.id) || o.se;
    const running = o.se.run?.state === "running";
    const working = listed.agentState === "working" || (running && listed.agentState !== "idle" && listed.agentState !== "blocked");
    const blocked = listed.agentState === "blocked";
    let start = running ? Date.parse(o.se.run.at || "") : NaN;
    const turns = o.turns || [];
    if (!Number.isFinite(start)) { const user = [...turns].reverse().find(t => t.who === "user" && !t.pending); start = user ? Date.parse(user.ts || "") : NaN; }
    const last = turns.at(-1);
    let step = null;
    if (last?.who === "assistant") step = [...(last.blocks || [])].reverse().find(b => b.t === "step" && !b.result && !b.done) || null;
    return {kind: "terminal", working, blocked, start, step, context: o.context || null, live: !!o.live, agent: o.se.kind};
  }
  const s = chatCurSession;
  if (!s || s.id !== chatOpenId || chatIsPortal()) return null;
  const running = (s.deliveries || []).find(d => d.state === "running");
  return {kind: "native", working: !!running, blocked: false, start: running ? Date.parse(running.updated || running.accepted || "") : NaN, step: null, context: null, live: false};
}

let chatStatusTimer = 0;
// The line's parts are built once and updated in place, so the one-second
// timer never replaces the Stop button under a pointer or focus.
function chatStatusEl() {
  let line = document.getElementById("chatStatusLine");
  if (line?.isConnected) return line;
  const main = document.querySelector(".chat-main"), comp = document.getElementById("chatComposer");
  if (!main || !comp) return null;
  line = el("div", "chat-status-line"); line.id = "chatStatusLine"; line.setAttribute("role", "status"); line.hidden = true;
  const dot = el("span", "chat-status-dot"); dot.setAttribute("aria-hidden", "true");
  const stop = el("button", "chat-status-stop", "Stop"); stop.type = "button";
  stop.title = "Interrupt the agent (sends Esc, its own interrupt key) · Esc in an empty composer";
  stop.setAttribute("aria-keyshortcuts", "Escape");
  stop.onclick = () => chatStatusInterrupt();
  const step = el("span", "chat-status-step");
  step.append(el("span", "chat-status-cast"), el("span", "chat-status-input"));
  const meter = el("span", "chat-status-context"), bar = el("span", "chat-status-meter"), fill = el("span", "");
  bar.setAttribute("aria-hidden", "true"); bar.append(fill);
  meter.append(bar, el("span", "chat-status-context-text"));
  line.append(dot, el("span", "chat-status-verb"), el("span", "chat-status-time"), step, stop, el("span", "chat-status-changes"), meter);
  main.insertBefore(line, document.getElementById("chatTermStrip") || comp);
  return line;
}
function chatStatusPaint() {
  const line = chatStatusEl();
  if (!line) return;
  const m = chatStatusModel();
  clearTimeout(chatStatusTimer);
  // a phone says the working tree's +N −M here, beside Stop, not in the head
  // (docs/ui-conventions.md, phone rule 3; 95-mobile.css shows the slot)
  const phone = !!window.mf?.phone?.(), se = m?.kind === "terminal" ? chatTermOpen?.se : null;
  const tree = phone && se && !se.device && se.cwd && typeof chatChangesButton === "function" ? se : null;
  const slot = line.querySelector(".chat-status-changes");
  slot.hidden = !tree;
  if (!tree) slot.replaceChildren();
  else if (slot.firstChild?.dataset.runtime !== tree.id) slot.replaceChildren(chatChangesButton(tree));
  if (!m || (!m.working && !m.blocked && !m.context?.used && !tree)) { line.hidden = true; line.dataset.state = ""; return; }
  line.hidden = false;
  const state = m.working ? "working" : m.blocked ? "blocked" : "idle";
  if (line.dataset.state !== state) { line.dataset.state = state; line.setAttribute("aria-label", state === "working" ? "Agent working" : state === "blocked" ? "Agent needs your input" : "Context"); }
  const q = c => line.querySelector(c), set = (c, text) => { const n = q(c); n.hidden = !text; if (n.textContent !== text) n.textContent = text; };
  const active = m.working || m.blocked;
  q(".chat-status-dot").hidden = !active;
  set(".chat-status-verb", m.working ? "Working" : m.blocked ? "Needs your input" : "");
  set(".chat-status-time", m.working && Number.isFinite(m.start) ? chatDuration(Date.now() - m.start) : "");
  const step = q(".chat-status-step"), showStep = !!(m.working && m.step);
  step.hidden = !showStep;
  if (showStep) { set(".chat-status-cast", m.step.cast || "step"); const input = m.step.input || ""; q(".chat-status-input").textContent = input ? " " + input : ""; step.title = (m.step.cast || "") + " " + input; }
  const stop = q(".chat-status-stop");
  stop.hidden = !(m.kind === "terminal" && m.live && m.working);
  if (stop.hidden) { stop.disabled = false; stop.textContent = "Stop"; }
  const meter = q(".chat-status-context"), c = m.context;
  meter.hidden = !c?.used;
  if (c?.used) {
    const pct = c.window ? Math.round(c.used / c.window * 100) : 0;
    const text = phone ? (c.window ? pct + "% context" : chatTokens(c.used) + " context")
      : c.window ? "Context " + chatTokens(c.used) + " of " + chatTokens(c.window) + " · " + pct + "%" : chatTokens(c.used) + " tokens in context";
    if (q(".chat-status-context-text").textContent !== text) q(".chat-status-context-text").textContent = text;
    meter.title = c.window ? "Context " + chatTokens(c.used) + " of " + chatTokens(c.window) + " tokens on the model's last call, as the agent recorded them" : "Tokens in the model's context on its last call. The agent does not record its window size, so no percentage is shown.";
    meter.dataset.level = pct >= 85 ? "high" : "";
    q(".chat-status-meter").hidden = !c.window;
    q(".chat-status-meter > span").style.width = Math.min(100, pct) + "%";
  }
  if (m.working) chatStatusTimer = setTimeout(chatStatusPaint, 1000);
}
async function chatStatusInterrupt() {
  const o = chatTermOpen;
  if (!o || !o.live) return;
  const stop = document.querySelector(".chat-status-stop");
  if (stop) { stop.disabled = true; stop.textContent = "Stopping…"; }
  try { await chatTermKey("esc"); }
  finally { setTimeout(chatStatusPaint, 400); }
}
// Esc in an empty composer interrupts a working coding agent, as in Codex and
// Claude Code. With text, or a menu or picker open, Esc keeps its usual job.
document.addEventListener("keydown", e => {
  if (e.key !== "Escape" || e.defaultPrevented || e.isComposing) return;
  const ta = e.target.closest?.("#chatComposer textarea");
  if (!ta || ta.value.trim() || document.querySelector(".chat-command-picker:not([hidden]), .chat-model-picker, dialog[open]")) return;
  if (document.getElementById("chatStatusLine")?.dataset.state !== "working" || !document.querySelector(".chat-status-stop")) return;
  e.preventDefault();
  chatStatusInterrupt();
});
window.addEventListener("manifest-terminal-state", () => chatStatusPaint());

// chatCollapseLong — a long pasted message shows its first lines with Show
// more, as Codex and ChatGPT do; the whole text stays in the page (copy and
// find still see it). host gets .is-long; the toggle follows the text.
function chatCollapseLong(host, text, textEl) {
  const lines = String(text || "").split("\n").length;
  if (String(text || "").length < 900 && lines <= 14) return;
  host.classList.add("is-long", "is-collapsed");
  const toggle = el("button", "chat-show-more", "Show more");
  toggle.type = "button";
  toggle.setAttribute("aria-expanded", "false");
  toggle.onclick = e => {
    e.stopPropagation();
    const open = host.classList.toggle("is-collapsed") === false;
    toggle.textContent = open ? "Show less" : "Show more";
    toggle.setAttribute("aria-expanded", String(open));
  };
  // in a flex bubble the toggle takes its own row at the bottom
  if (textEl) { const brk = el("span", "chat-show-break"); brk.setAttribute("aria-hidden", "true"); textEl.parentNode.append(brk, toggle); }
  else host.append(toggle);
}
