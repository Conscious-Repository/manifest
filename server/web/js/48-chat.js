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

function showChat(h) {
  const tail = h && h.startsWith("#/chat/") ? decodeURIComponent(h.slice("#/chat/".length)) : "";
  chatOpenId = tail;
  loadChatSessions().then(() => {
    // no explicit session → open the most recent one
    if (!chatOpenId && chatSessions.length) {
      chatOpenId = chatSessions[0].id;
    }
    renderChatRail();
    renderChatComposer();
    if (chatOpenId) loadChatSession(chatOpenId);
    else renderChatEmpty();
  });
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
  const newBtn = el("button", "pill chat-new", "＋ new conversation");
  newBtn.onclick = () => chatNewSessionPicker(newBtn);
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

// chatNewSessionPicker — a tiny inline spirit picker under the ＋ button.
async function chatNewSessionPicker(anchor) {
  const existing = document.querySelector(".chat-spirit-pick");
  if (existing) { existing.remove(); return; }
  const spirits = (await chatSpiritList()).filter((s) => s.enabled);
  const pick = el("div", "chat-spirit-pick");
  if (!spirits.length) pick.append(emptyRow("No chattable spirits (add a chat.md)."));
  spirits.forEach((s) => {
    const b = el("button", "pill light", s.name);
    b.onclick = async () => {
      pick.remove();
      try {
        const r = await postJSONOk("/api/chat/sessions", { spirit: s.name });
        chatOpenId = r.id;
        location.hash = "#/chat/" + encodeURIComponent(r.id);
      } catch (e) { showToast("Couldn't create the session — " + (e.message || "error")); }
    };
    pick.append(b);
  });
  anchor.after(pick);
}

// ---- transcript ----

function renderChatEmpty() {
  const t = document.getElementById("chatTranscript");
  if (t) { t.innerHTML = ""; t.append(emptyRow("Start a conversation — ＋ new, or just type below (concierge answers).")); }
}

async function loadChatSession(id) {
  let d;
  try {
    const res = await fetch("/api/chat/sessions/" + encodeURIComponent(id));
    if (!res.ok) { renderChatEmpty(); return; }
    d = await res.json();
  } catch (e) { return; }
  if (id !== chatOpenId) return; // navigated away mid-fetch
  renderChatTranscript(d);
  renderChatComposer(d.session);
  ensureChatPoll(d.session, (d.queued || []).length);
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
  const stick = host.scrollTop + host.clientHeight >= host.scrollHeight - 60;
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
  if (s.status === "thinking") {
    host.append(el("div", "chat-thinking", "✦ thinking…"));
  }
  if (stick) host.scrollTop = host.scrollHeight;
}

// ---- composer ----

function renderChatComposer(session) {
  const host = document.getElementById("chatComposer");
  if (!host) return;
  if (host.dataset.built) {
    const ta = host.querySelector("textarea");
    const send = host.querySelector("button");
    const busy = session && session.status === "thinking";
    if (ta) ta.placeholder = busy ? "✦ thinking — messages queue…" : "Message…  (Enter sends, Shift-Enter = newline)";
    if (send) send.disabled = false;
    return;
  }
  host.dataset.built = "1";
  const ta = document.createElement("textarea");
  ta.className = "chat-input";
  ta.rows = 2;
  ta.placeholder = "Message…  (Enter sends, Shift-Enter = newline)";
  const send = el("button", "pill", "send");
  const submit = async () => {
    const text = ta.value.trim();
    if (!text) return;
    ta.value = "";
    try {
      if (chatOpenId) {
        await postJSONOk("/api/chat/sessions/" + encodeURIComponent(chatOpenId) + "/messages", { text });
      } else {
        const spirits = (await chatSpiritList()).filter((s) => s.enabled);
        const spirit = spirits.length ? spirits[0].name : "concierge";
        const r = await postJSONOk("/api/chat/sessions", { spirit, text });
        chatOpenId = r.id;
        location.hash = "#/chat/" + encodeURIComponent(r.id);
        return;
      }
      loadChatSession(chatOpenId);
      loadChatSessions().then(renderChatRail);
    } catch (e) { showToast("Send failed — " + (e.message || "error")); ta.value = text; }
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
