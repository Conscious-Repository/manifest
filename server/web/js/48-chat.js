// ---- CHAT: conversations with chattable spirits (cmd-ctr import P2) ----
// The ENGINE owns every turn (casts, warding, charge); this surface reads the
// session file it rewrites step-by-step and spools user messages. The 1.5s
// poll while a session is thinking is the live progress channel — the file IS
// the stream (run-report idiom). Globals chatOpenSession/chatCompose are the
// hooks the palette, floating chat (P4), and capture handoff (P5) drive.

let chatSessions = [];      // last /api/chat/sessions fetch
let chatOpenId = "";        // the open session id ("" = none)
let chatSpiritsCache = null;
let chatPollTimer = null;
let chatLastUpdated = "";   // change-detection for transcript re-render

let chatLanding = false;            // ＋new → lazy landing (session created on first send)
let chatPendingSpirit = "concierge"; // spirit/model the landing's first send uses
let chatPendingModel = "";

function showChat(h) {
  const tail = h && h.startsWith("#/chat/") ? decodeURIComponent(h.slice("#/chat/".length)) : "";
  if (tail.startsWith("cmp/")) { renderCompare(tail.slice(4).split(",").filter(Boolean)); return; }
  if (tail === "new") { chatOpenId = ""; chatLanding = true; }
  else { chatOpenId = tail; chatLanding = false; }
  loadChatSessions().then(() => {
    // no explicit session → most recent; none at all → the landing
    if (!chatOpenId && !chatLanding && chatSessions.length) {
      chatOpenId = chatSessions[0].id;
    }
    renderChatRail();
    renderChatComposer();
    if (chatOpenId) loadChatSession(chatOpenId);
    else renderChatLanding();
  });
  requestAnimationFrame(chatFitShell);
  if (!chatFitBound) { window.addEventListener("resize", chatFitShell); chatFitBound = true; }
}

// chatFitShell sizes the shell to exactly the space below its top edge (mirrors
// termFitShell) so it fills correctly whatever chrome sits above it — e.g. with
// the crumb bar hidden on this section, the shell reclaims that height.
let chatFitBound = false;
function chatFitShell() {
  const shell = document.querySelector(".chat-shell");
  if (!shell || els.chatView.hidden) return;
  if (window.innerWidth <= 860) { shell.style.height = ""; return; } // mobile stacks
  const top = shell.getBoundingClientRect().top;
  shell.style.height = Math.max(320, window.innerHeight - top - 14) + "px";
}

async function loadChatSessions() {
  try { chatSessions = ((await (await fetch("/api/chat/sessions")).json()).sessions) || []; }
  catch (e) { chatSessions = []; }
}

async function chatSpiritList() {
  if (chatSpiritsCache) return chatSpiritsCache;
  try { chatSpiritsCache = ((await (await fetch("/api/chat/spirits")).json()).spirits) || []; }
  catch (e) { chatSpiritsCache = []; }
  return chatSpiritsCache;
}

// ---- rail ----

function renderChatRail() {
  const host = document.getElementById("chatRail");
  if (!host) return;
  host.innerHTML = "";
  // ＋ new is LAZY (cmd-ctr model): it opens the greeting landing — nothing
  // is created until the first message sends. The landing carries the
  // spirit/model chooser.
  const newBtn = el("button", "pill chat-new", "＋ new conversation");
  newBtn.onclick = () => { location.hash = "#/chat/new"; };
  host.append(newBtn);
  if (!chatSessions.length) {
    host.append(emptyRow("No conversations yet."));
    return;
  }
  chatSessions.forEach((s) => {
    const row = el("div", "chat-rail-row" + (s.id === chatOpenId ? " open" : ""));
    const top = el("div", "chat-rail-top");
    top.append(el("span", "chat-rail-title", s.title || s.id));
    if (s.status === "thinking") top.append(el("span", "chat-rail-live", "✦"));
    row.append(top);
    const meta = el("div", "chat-rail-meta");
    meta.append(el("span", "chat-rail-spirit", s.spirit));
    meta.append(el("span", "chat-rail-when", fmtWhen(s.updated || s.created)));
    row.append(meta);
    row.onclick = () => { location.hash = "#/chat/" + encodeURIComponent(s.id); };
    host.append(row);
  });
}

// renderChatLanding — the lazy new-chat landing (cmd-ctr model): a time-aware
// greeting, spirit/model chips that set what the FIRST SEND creates, and the
// centered composer. No session exists until the first message goes out, so
// "new conversation" can never feel dead.
function chatGreeting() {
  const h = new Date().getHours();
  const pool = h < 5 ? ["Late one. What's on your mind?", "Still going — what do you need?"]
    : h < 12 ? ["Morning. What are we into?", "Where do we start today?"]
    : h < 18 ? ["What's next?", "What can I dig into?"]
    : ["Evening. What's open?", "What's still on your mind?"];
  return pool[new Date().getMinutes() % pool.length];
}

async function renderChatLanding() {
  const host = document.getElementById("chatTranscript");
  const main = document.querySelector(".chat-main");
  if (!host) return;
  if (main) main.classList.add("landing");
  host.innerHTML = "";
  host.append(el("div", "chat-spark", "✦"));
  host.append(el("div", "chat-greeting", chatGreeting()));

  const spirits = (await chatSpiritList()).filter((s) => s.enabled);
  if (!spirits.length) {
    host.append(emptyRow("No chattable spirits (add a chat.md)."));
    return;
  }
  if (!spirits.some((s) => s.name === chatPendingSpirit)) {
    chatPendingSpirit = spirits[0].name;
    chatPendingModel = "";
  }
  const picks = el("div", "chat-spirit-pick");
  const paint = () => {
    picks.innerHTML = "";
    spirits.forEach((s) => {
      const row = el("div", "chat-pick-row");
      const b = el("button", "pill light" + (s.name === chatPendingSpirit && !chatPendingModel ? " on" : ""), s.name);
      b.onclick = () => { chatPendingSpirit = s.name; chatPendingModel = ""; paint(); focusChatInput(); };
      row.append(b);
      (s.models || []).forEach((m) => {
        const short = m.replace(/^claude-/, "").replace(/-\d{8}$/, "");
        const mb = el("button", "sprt-quiet" + (s.name === chatPendingSpirit && chatPendingModel === m ? " on" : ""), short);
        mb.title = s.name + " · " + m;
        mb.onclick = () => { chatPendingSpirit = s.name; chatPendingModel = m; paint(); focusChatInput(); };
        row.append(mb);
      });
      picks.append(row);
    });
    const cmpSpirit = spirits.find((s) => (s.models || []).length > 1);
    if (cmpSpirit) {
      const cb = el("button", "sprt-quiet", "⇄ compare models");
      cb.onclick = () => { if (main) main.classList.remove("landing"); openComparePrompt(cmpSpirit); };
      picks.append(cb);
    }
  };
  paint();
  host.append(picks);
  host.append(el("div", "chat-landing-hint", "type below — Enter sends, the conversation starts then"));
  focusChatInput();
}

function focusChatInput() {
  const ta = document.querySelector("#chatComposer textarea");
  if (ta) ta.focus();
}

// ---- Compare: one prompt → N model lanes (UI-level fan-out) ----

function openComparePrompt(spirit) {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  host.innerHTML = "";
  host.append(el("div", "chat-head-title", "Compare — one prompt across models"));
  const ta = document.createElement("textarea");
  ta.className = "chat-input";
  ta.rows = 3;
  ta.placeholder = "The prompt to fan out…";
  host.append(ta);
  const picks = el("div", "chat-spirit-pick");
  const chosen = new Set(spirit.models.slice(0, 2));
  spirit.models.forEach((m) => {
    const b = el("button", "pill light" + (chosen.has(m) ? " on" : ""), m.replace(/^claude-/, "").replace(/-\d{8}$/, ""));
    b.onclick = () => {
      if (chosen.has(m)) chosen.delete(m); else chosen.add(m);
      b.classList.toggle("on");
    };
    picks.append(b);
  });
  host.append(picks);
  const go = el("button", "pill", "run compare");
  go.onclick = async () => {
    const text = ta.value.trim();
    if (!text || chosen.size < 2) { showToast("Prompt + at least two models"); return; }
    go.disabled = true;
    try {
      const ids = [];
      for (const m of chosen) {
        const r = await postJSONOk("/api/chat/sessions", {
          spirit: spirit.name, model: m, text,
          title: "cmp · " + m.replace(/^claude-/, "") + " · " + text.slice(0, 30),
        });
        ids.push(r.id);
      }
      location.hash = "#/chat/cmp/" + ids.map(encodeURIComponent).join(",");
    } catch (e) { showToast("Compare failed — " + (e.message || "error")); go.disabled = false; }
  };
  host.append(go);
  ta.focus();
}

let cmpPollTimer = null;
async function renderCompare(ids) {
  const host = document.getElementById("chatTranscript");
  const comp = document.getElementById("chatComposer");
  if (comp) comp.hidden = true;
  const rail = document.getElementById("chatRail");
  if (rail) {
    rail.innerHTML = "";
    const back = el("button", "pill chat-new", "‹ back to chat");
    back.onclick = () => { location.hash = "#/chat"; if (comp) comp.hidden = false; };
    rail.append(back);
  }
  if (!host) return;
  host.innerHTML = "";
  const row = el("div", "chat-cmp-row");
  host.append(row);
  let anyThinking = false;
  for (const id of ids) {
    const lane = el("div", "chat-cmp-lane");
    let d;
    try {
      const res = await fetch("/api/chat/sessions/" + encodeURIComponent(id));
      if (!res.ok) { lane.append(emptyRow("gone")); row.append(lane); continue; }
      d = await res.json();
    } catch (e) { continue; }
    lane.append(el("div", "chat-cmp-head", (d.session.model || d.session.spirit) + " · $" + d.session.spentUsd.toFixed(4)));
    const turns = parseChatTurns(d.body || "");
    const say = turns.filter((t) => t.who !== "user" && t.who !== "system").map((t) => {
      const st = parseChatSteps(t.text).find((s) => s.cast === "say");
      return st ? st.body : t.text;
    }).join("\n\n");
    const bodyEl = el("div", "chat-say");
    if (say) {
      try { bodyEl.append(renderMarkdown(say, "", { readOnly: true })); } catch (e) { bodyEl.textContent = say; }
    } else if (d.session.status === "thinking" || (d.queued || []).length) {
      bodyEl.append(el("div", "chat-thinking", "✦ thinking…"));
      anyThinking = true;
    } else {
      bodyEl.textContent = "—";
    }
    lane.append(bodyEl);
    const open = el("button", "sprt-quiet", "open ↗");
    open.onclick = () => { if (comp) comp.hidden = false; chatOpenSession(id); };
    lane.append(open);
    row.append(lane);
  }
  if (cmpPollTimer) { clearInterval(cmpPollTimer); cmpPollTimer = null; }
  if (anyThinking) {
    cmpPollTimer = setInterval(() => {
      if (!location.hash.startsWith("#/chat/cmp/")) { clearInterval(cmpPollTimer); cmpPollTimer = null; return; }
      renderCompare(ids);
      clearInterval(cmpPollTimer); cmpPollTimer = null;
    }, 2000);
  }
}

// ---- transcript ----

function renderChatEmpty() { renderChatLanding(); }

async function loadChatSession(id) {
  let d;
  try {
    const res = await fetch("/api/chat/sessions/" + encodeURIComponent(id));
    if (!res.ok) { renderChatEmpty(); return; }
    d = await res.json();
  } catch (e) { return; }
  if (id !== chatOpenId) return; // navigated away mid-fetch
  const main = document.querySelector(".chat-main");
  if (main) main.classList.remove("landing");
  renderChatTranscript(d);
  renderChatComposer(d.session);
  ensureChatStream(d.session);
  if ((d.queued || []).length || d.session.status === "thinking") ensureChatPoll(d.session, (d.queued || []).length);
}

// ---- live stream layer (A2): EventSource over the engine's event log ----
// The transcript render covers history from the session file; this layer
// paints the CURRENT turn as it happens — tool chips lighting up, thinking,
// and (once the engine streams deltas) the reply typing out with constant-
// speed reveal. On turn.completed the session refetches and the layer clears.
let chatES = null, chatESFor = "";
let chatLive = null; // {tools:[], thinking:"", thinkTok:0, say:"", revealed:0, open:true}
let chatRevealTimer = null, chatLastMd = 0;

function ensureChatStream(session) {
  if (chatESFor === session.id && chatES) return;
  if (chatES) { chatES.close(); chatES = null; }
  chatESFor = session.id;
  chatLive = null;
  let es;
  try { es = new EventSource("/api/chat/sessions/" + encodeURIComponent(session.id) + "/stream?after=-1"); }
  catch (e) { return; }
  chatES = es;
  const refetch = () => { if (chatOpenId === session.id) { loadChatSessions().then(renderChatRail); refetchChatSession(session.id); } };
  const on = (type, fn) => es.addEventListener(type, (ev) => {
    if (chatOpenId !== session.id) return;
    let d = {};
    try { d = (JSON.parse(ev.data).data) || {}; } catch (e) {}
    fn(d);
  });
  on("turn.started", () => { chatLive = { tools: [], thinking: "", thinkTok: 0, say: "", revealed: 0, open: true }; renderChatLive(); });
  on("tool.started", (d) => { if (!chatLive) chatLive = { tools: [], thinking: "", thinkTok: 0, say: "", revealed: 0, open: true }; chatLive.tools.push({ cast: d.cast, detail: d.rationale || "", done: false }); renderChatLive(); });
  on("tool.completed", (d) => {
    if (!chatLive) return;
    const t = chatLive.tools.slice().reverse().find((x) => x.cast === d.cast && !x.done);
    if (t) { t.done = true; t.detail = d.summary || d.error || t.detail; }
    renderChatLive();
  });
  on("thinking.delta", (d) => { if (chatLive) { chatLive.thinking += d.text || ""; scheduleChatReveal(); } });
  on("thinking.tokens", (d) => { if (chatLive) { chatLive.thinkTok = d.tokens || 0; renderChatLive(); } });
  on("assistant.delta", (d) => { if (chatLive) { chatLive.say += d.text || ""; scheduleChatReveal(); } });
  on("assistant.message", (d) => { if (chatLive) { chatLive.say = d.text || chatLive.say; chatLive.open = false; scheduleChatReveal(); } });
  on("turn.completed", () => { finishChatLive(); refetch(); });
  on("session.error", refetch);
  on("user.message", refetch);
  es.onerror = () => {
    // degraded network / proxy: fall back to the file poll
    if (chatES === es) { es.close(); chatES = null; chatESFor = ""; ensureChatPoll(session, 1); }
  };
}

async function refetchChatSession(id) {
  try {
    const res = await fetch("/api/chat/sessions/" + encodeURIComponent(id));
    if (!res.ok || id !== chatOpenId) return;
    const d = await res.json();
    renderChatTranscript(d);
    renderChatComposer(d.session);
  } catch (e) {}
}

function finishChatLive() {
  if (chatRevealTimer) { clearInterval(chatRevealTimer); chatRevealTimer = null; }
  chatLive = null;
  const area = document.getElementById("chatLiveArea");
  if (area) area.innerHTML = "";
}

// constant-speed reveal (cmd-ctr: ~85 chars/sec, catch up if far behind)
function scheduleChatReveal() {
  if (chatRevealTimer) return;
  chatRevealTimer = setInterval(() => {
    if (!chatLive) { clearInterval(chatRevealTimer); chatRevealTimer = null; return; }
    const target = chatLive.say.length;
    if (chatLive.revealed < target) {
      const behind = target - chatLive.revealed;
      chatLive.revealed += behind > 130 ? Math.ceil(behind / 8) : 5; // ~85cps at 60ms tick
      if (chatLive.revealed > target) chatLive.revealed = target;
    }
    const now = Date.now();
    if (now - chatLastMd >= 90 || chatLive.revealed >= target) {
      chatLastMd = now;
      renderChatLive();
    }
    if (chatLive.revealed >= target && !chatLive.open && chatLive.thinking === chatLive._renderedThinking) {
      clearInterval(chatRevealTimer);
      chatRevealTimer = null;
    }
  }, 60);
}

function renderChatLive() {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  let area = document.getElementById("chatLiveArea");
  if (!area) {
    area = el("div", "chat-live-area");
    area.id = "chatLiveArea";
    host.append(area);
  }
  area.innerHTML = "";
  if (!chatLive) return;
  const wrap = el("div", "chat-turn chat-spirit");
  chatLive.tools.forEach((t) => {
    const ln = el("div", "run-trace-step chat-step" + (t.done ? "" : " running"));
    ln.append(el("span", "chat-step-dot", t.done ? "✓" : "▸"));
    ln.append(el("span", "run-trace-cast", t.cast));
    ln.append(el("span", "run-trace-detail", t.detail || ""));
    wrap.append(ln);
  });
  if (chatLive.thinking) {
    chatLive._renderedThinking = chatLive.thinking;
    const det = document.createElement("details");
    det.className = "chat-thinking-block";
    const sum = document.createElement("summary");
    sum.textContent = "▸ thinking" + (chatLive.thinkTok ? " · " + chatLive.thinkTok + " tok" : "");
    det.append(sum, el("div", "chat-thinking-text", chatLive.thinking));
    wrap.append(det);
  }
  if (chatLive.say) {
    const say = el("div", "chat-say");
    const shown = chatLive.say.slice(0, chatLive.revealed);
    try { say.append(renderMarkdown(shown, "", { readOnly: true })); }
    catch (e) { say.textContent = shown; }
    if (chatLive.revealed < chatLive.say.length || chatLive.open) say.append(el("span", "chat-cursor", "▍"));
    wrap.append(say);
  } else if (chatLive.open && !chatLive.tools.length) {
    wrap.append(el("div", "chat-thinking", "✦ thinking…" + (chatLive.thinkTok ? " · " + chatLive.thinkTok + " tok" : "")));
  }
  area.append(wrap);
  chatPin();
}

// ---- stick-to-bottom (cmd-ctr rules: release on scroll-up, re-arm ≤24px) ----
let chatStick = true, chatLastY = 0, chatScrollBound = false;
function bindChatScroll() {
  if (chatScrollBound) return;
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  chatScrollBound = true;
  host.addEventListener("scroll", () => {
    const y = host.scrollTop;
    if (y < chatLastY - 1) chatStick = false;
    if (host.scrollHeight - y - host.clientHeight <= 24) chatStick = true;
    chatLastY = y;
  });
}
function chatPin() {
  const host = document.getElementById("chatTranscript");
  if (host && chatStick) host.scrollTop = host.scrollHeight;
}

// parseChatTurns splits the session body into turn blocks.
function parseChatTurns(body) {
  const turns = [];
  const re = /^## Turn (\d+) — (.+?) · (\S+)( · \$([\d.]+))?$/gm;
  let m, prev = null;
  while ((m = re.exec(body))) {
    if (prev) prev.text = body.slice(prev.end, m.index).trim();
    prev = { n: parseInt(m[1], 10), who: m[2].trim(), ts: m[3], usd: m[5] || "", end: m.index + m[0].length };
    turns.push(prev);
  }
  if (prev) prev.text = body.slice(prev.end).trim();
  return turns;
}

// parseChatSteps splits a spirit turn's text into cast steps + the say body.
function parseChatSteps(text) {
  const steps = [];
  const re = /^### Step (\d+) — (.+)$/gm;
  let m, prev = null;
  while ((m = re.exec(text))) {
    if (prev) prev.body = text.slice(prev.end, m.index).trim();
    prev = { n: parseInt(m[1], 10), cast: m[2].trim(), end: m.index + m[0].length };
    steps.push(prev);
  }
  if (prev) prev.body = text.slice(prev.end).trim();
  return steps;
}

function renderChatTranscript(d) {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  bindChatScroll();
  host.innerHTML = "";
  const s = d.session;
  chatLastUpdated = s.updated + "|" + s.status + "|" + (d.queued || []).length;

  const head = el("div", "chat-head");
  head.append(el("span", "chat-head-title", s.title || s.id));
  head.append(el("span", "chat-head-spirit", s.spirit + (s.model ? " · " + s.model : "")));
  const spent = el("span", "chat-head-charge", "$" + s.spentUsd.toFixed(4) + (s.ceilingUsd ? " / $" + s.ceilingUsd.toFixed(2) : ""));
  head.append(spent);
  const ren = el("button", "sprt-quiet", "rename");
  ren.onclick = () => askText("Rename conversation", s.title || "", async (t) => {
    if (!t.trim()) return;
    try { await postJSONOk("/api/chat/sessions/" + encodeURIComponent(s.id) + "/rename", { title: t.trim() }); loadChat(); } catch (e) { showToast(e.message || "rename failed"); }
  });
  const del = el("button", "sprt-quiet", "delete");
  del.onclick = async () => {
    if (!confirm("Delete this conversation?")) return;
    try {
      await fetch("/api/chat/sessions/" + encodeURIComponent(s.id), { method: "DELETE" });
      chatOpenId = "";
      location.hash = "#/chat";
    } catch (e) {}
  };
  head.append(ren, del);
  host.append(head);

  parseChatTurns(d.body || "").forEach((t) => {
    if (t.who === "user") {
      const b = el("div", "chat-turn chat-user");
      b.textContent = t.text;
      host.append(b);
      return;
    }
    if (t.who === "system") {
      const b = el("div", "chat-turn chat-system");
      b.textContent = t.text;
      host.append(b);
      return;
    }
    const wrap = el("div", "chat-turn chat-spirit");
    const steps = parseChatSteps(t.text);
    steps.forEach((st) => {
      if (st.cast === "say") {
        const say = el("div", "chat-say");
        try { say.append(renderMarkdown(st.body || "", "", { readOnly: true })); }
        catch (e) { say.textContent = st.body || ""; }
        wrap.append(say);
      } else {
        const ln = el("div", "run-trace-step chat-step");
        ln.append(el("span", "run-trace-cast", st.cast));
        const mres = (st.body || "").match(/^- (?:result|rationale): (.*)$/m);
        ln.append(el("span", "run-trace-detail", mres ? mres[1] : ""));
        wrap.append(ln);
      }
    });
    if (!steps.length && t.text) { const p = el("div", "chat-say"); p.textContent = t.text; wrap.append(p); }
    if (t.usd) wrap.append(el("div", "chat-turn-usd", "$" + t.usd));
    host.append(wrap);
  });

  (d.queued || []).forEach((q) => {
    const b = el("div", "chat-turn chat-user chat-queued");
    b.textContent = q;
    host.append(b);
  });
  // the live layer's mount point (turn.started paints into it); when the ES
  // hasn't caught the turn yet, the status flag still shows a quiet indicator
  const area = el("div", "chat-live-area");
  area.id = "chatLiveArea";
  host.append(area);
  if (s.status === "thinking" && !chatLive) {
    area.append(el("div", "chat-thinking", "✦ thinking…"));
  }
  chatStick = true;
  chatPin();
}

// ---- composer ----

function renderChatComposer(session) {
  const host = document.getElementById("chatComposer");
  if (!host) return;
  if (host.dataset.built) {
    const ta = host.querySelector("textarea");
    const send = host.querySelector(".chat-send");
    const busy = session && session.status === "thinking";
    if (ta) ta.placeholder = busy ? "✦ thinking — messages queue…" : "Message…";
    if (send) send.disabled = false;
    return;
  }
  host.dataset.built = "1";
  const ta = document.createElement("textarea");
  ta.className = "chat-input";
  ta.rows = 1;
  ta.placeholder = "Message…";
  // auto-grow with content (target feel): reset then snap to scrollHeight,
  // clamped so a long paste scrolls inside instead of shoving the transcript.
  const grow = () => { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, window.innerHeight * 0.4) + "px"; };
  ta.addEventListener("input", grow);
  const send = el("button", "chat-send", "↑");
  send.title = "Send  ·  Enter";
  const submit = async () => {
    const text = ta.value.trim();
    if (!text) return;
    ta.value = "";
    grow();
    try {
      if (chatOpenId) {
        await postJSONOk("/api/chat/sessions/" + encodeURIComponent(chatOpenId) + "/messages", { text });
      } else {
        // lazy create (cmd-ctr model): the landing's chosen spirit/model
        const r = await postJSONOk("/api/chat/sessions", {
          spirit: chatPendingSpirit || "concierge", model: chatPendingModel || "", text,
        });
        chatOpenId = r.id;
        chatLanding = false;
        location.hash = "#/chat/" + encodeURIComponent(r.id);
        return;
      }
      loadChatSession(chatOpenId);
      loadChatSessions().then(renderChatRail);
    } catch (e) { showToast("Send failed — " + (e.message || "error")); ta.value = text; grow(); }
  };
  ta.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  });
  send.onclick = submit;
  host.append(ta, send);
}

// ---- live poll (file-derived; stops when idle) ----

function ensureChatPoll(session, queued) {
  const active = session && (session.status === "thinking" || queued > 0);
  if (!active) { if (chatPollTimer) { clearInterval(chatPollTimer); chatPollTimer = null; } return; }
  if (chatPollTimer) return;
  chatPollTimer = setInterval(async () => {
    if (els.chatView.hidden || !chatOpenId) {
      clearInterval(chatPollTimer); chatPollTimer = null; return;
    }
    let d;
    try {
      const res = await fetch("/api/chat/sessions/" + encodeURIComponent(chatOpenId));
      if (!res.ok) return;
      d = await res.json();
    } catch (e) { return; }
    const sig = d.session.updated + "|" + d.session.status + "|" + (d.queued || []).length;
    if (sig !== chatLastUpdated) {
      renderChatTranscript(d);
      renderChatComposer(d.session);
      loadChatSessions().then(renderChatRail);
    }
    if (d.session.status !== "thinking" && !(d.queued || []).length) {
      clearInterval(chatPollTimer); chatPollTimer = null;
    }
  }, 1500);
}

function loadChat() { showChat(location.hash); }

// ---- hooks for the palette / floating chat / capture handoff ----

function chatOpenSession(id) { location.hash = "#/chat/" + encodeURIComponent(id); }

// chatCompose opens (or creates) a session with a spirit and pre-fills text.
async function chatCompose(spirit, prefill) {
  try {
    const r = await postJSONOk("/api/chat/sessions", { spirit: spirit || "concierge" });
    location.hash = "#/chat/" + encodeURIComponent(r.id);
    if (prefill) {
      setTimeout(() => {
        const ta = document.querySelector("#chatComposer textarea");
        if (ta) { ta.value = prefill; ta.focus(); }
      }, 300);
    }
  } catch (e) { showToast("Couldn't open a chat — " + (e.message || "error")); }
}

// ⌘K providers: open a session by title; start a new chat.
cmdRegistry.register(async (q) => {
  if (!q) return [];
  let sessions = chatSessions;
  if (!sessions.length) {
    try { sessions = ((await (await fetch("/api/chat/sessions")).json()).sessions) || []; } catch (e) { sessions = []; }
  }
  return sessions.slice(0, 30).map((s) => ({
    id: "chat:" + s.id,
    name: "Chat · " + (s.title || s.id),
    hint: s.spirit + " · conversation",
    keywords: "chat conversation " + s.spirit,
    act: () => { closeCmdbar(); chatOpenSession(s.id); },
  }));
});
cmdRegistry.register(() => [{
  id: "act:new-chat", name: "New chat with a spirit", hint: "chat · action",
  keywords: "chat talk converse ask concierge",
  act: () => { closeCmdbar(); chatCompose("concierge", ""); },
}]);
