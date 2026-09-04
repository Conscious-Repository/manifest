// ---- CHAT: Agents chat — one tab, a rail grouped by agent ----
// (agent-chat plan §3.1/§3.2; cmd-ctr import P2 underneath.) The rail is a
// list of AGENT SECTIONS, each with its own thread list: ALFRED (the default
// Hermes profile), PROFILES (one per `hermes profile`), KAIROS (AION team) and
// ZECK (OODA) over the portals' own chat stores (Phase 2), SPIRITS (chattable
// excalibur spirits). One transcript renderer + one composer serve all of them;
// what differs per section is the BACKEND — a base URL and whether an SSE
// stream exists:
//   spirits          /api/chat/sessions               engine writes, SSE + poll
//   alfred/profiles  /api/agents/chat/<agent>/sessions manifest writes, poll only
//   kairos/zeck      /api/agents/chat/<agent>/sessions portal store + spool; the
//                    reply returns via the server's chatSweep on read; poll only;
//                    one order at a time (a send while one runs is a 409)
// The poll while a session is thinking is the live progress channel for every
// backend — the file IS the stream (run-report idiom). Globals
// chatOpenSession/chatCompose are the hooks the palette, floating chat (P4),
// and capture handoff (P5) drive.

let chatSessions = [];      // last /api/chat/sessions fetch (spirits)
let chatOpenId = "";        // the open session id ("" = none)
let chatSpiritsCache = null;
let chatPollTimer = null;
let chatLastUpdated = "";   // change-detection for transcript re-render

let chatLanding = false;            // ＋new → lazy landing (session created on first send)
let chatPendingSpirit = "concierge"; // spirit/model the landing's first send uses
let chatPendingModel = "";

// agent sections (Phase 1): "" = the SPIRITS section; else an agent slug
// (alfred | <profile>) whose threads come from /api/agents/chat/<agent>/…
let chatAgent = "";
let chatRoster = [];         // last /api/agents/chat/roster fetch
let chatAgentSessions = {};  // agent slug → its session list
let chatAgentTasks = {};     // agent slug → the open todos it holds (Phase 4 bridge)
let chatCurSession = null;   // the open session object as last fetched (head repaint after rename)

// the last section + last-open thread per section survive a reload
// (manifest.termStage precedent) — bare #/chat restores them; a SECTION switch
// never auto-opens a thread (plan §7 Q9: list + landing, no silent open)
function chatRemember(section, id) {
  try {
    localStorage.setItem("manifest.chatSection", section);
    if (id !== undefined) localStorage.setItem("manifest.chatLast." + (section || "spirits"), id);
  } catch (e) {}
}
function chatRecall(key) { try { return localStorage.getItem(key) || ""; } catch (e) { return ""; } }

// ---- backend switch: everything below asks these, never a literal URL ----
function chatBaseFor(agent) {
  return agent ? "/api/agents/chat/" + encodeURIComponent(agent) + "/sessions" : "/api/chat/sessions";
}
function chatBase() { return chatBaseFor(chatAgent); }
function chatHash(id) {
  return chatAgent ? "#/chat/a/" + encodeURIComponent(chatAgent) + "/" + encodeURIComponent(id) : "#/chat/" + encodeURIComponent(id);
}
function chatSectionHash(agent) { return agent ? "#/chat/a/" + encodeURIComponent(agent) : "#/chat/spirits"; }
function chatNewHash() { return chatAgent ? "#/chat/a/" + encodeURIComponent(chatAgent) + "/new" : "#/chat/new"; }
function chatCurrentSessions() { return chatAgent ? (chatAgentSessions[chatAgent] || []) : chatSessions; }
function chatRosterEntry(name) { return chatRoster.find((a) => a.name === name) || null; }
function chatAgentLabel(name) { const a = chatRosterEntry(name); return a ? a.label : name; }
// portal sections (kairos/zeck): attachments go to the agent's own artifact
// domain, sends carry a ritual, and @-mentions tag a persona intent
function chatIsPortal(name) { const a = chatRosterEntry(name === undefined ? chatAgent : name); return !!(a && a.backend === "portal"); }
function chatAttachBase() { return "/api/agents/chat/" + encodeURIComponent(chatAgent) + "/attach"; }
function chatFileHref(hash) { return chatIsPortal() ? chatAttachBase() + "/" + hash : "/api/tasks/thread/file/" + hash + "?id=agentchat"; }
let chatRitual = "ask"; // portal sends: ask | delegate (the portals' two outcomes)

function showChat(h) {
  const tail = h && h.startsWith("#/chat/") ? decodeURIComponent(h.slice("#/chat/".length)) : "";
  if (tail.startsWith("cmp/")) { renderCompare(tail.slice(4).split(",").filter(Boolean)); return; }
  // route → section + thread. Agent routes: a/<agent>[/new|/<id>]; spirit
  // routes keep their old shapes (new, <id>, "spirits" = the section itself).
  // A section route (a/<agent> or "spirits") shows the section's list + its
  // landing; a thread opens only when the hash names one, or on bare #/chat
  // (the remembered thread of the remembered section).
  let restore = false;
  if (tail.startsWith("a/")) {
    const parts = tail.slice(2).split("/");
    chatAgent = parts[0] || "alfred";
    const sub = parts[1] || "";
    if (sub && sub !== "new") { chatOpenId = sub; chatLanding = false; }
    else { chatOpenId = ""; chatLanding = true; }
  } else if (tail === "spirits" || tail === "new") { chatAgent = ""; chatOpenId = ""; chatLanding = true; }
  else if (tail) { chatAgent = ""; chatOpenId = tail; chatLanding = false; }
  else { restore = true; chatOpenId = ""; chatLanding = false; }
  renderChatHeadActions();
  loadChatRoster().then(async () => {
    if (restore) {
      // bare #/chat → the remembered section, else ALFRED when it can take a
      // turn, else spirits (the pre-Phase-1 behaviour)
      const remembered = chatRecall("manifest.chatSection");
      const alfred = chatRosterEntry("alfred");
      if (remembered === "spirits") chatAgent = "";
      else if (remembered && chatRosterEntry(remembered)) chatAgent = remembered;
      else chatAgent = alfred && alfred.enabled ? "alfred" : "";
    }
    await loadChatSessions();
    const list = chatCurrentSessions();
    if (restore) {
      const last = chatRecall("manifest.chatLast." + (chatAgent || "spirits"));
      if (last && list.some((s) => s.id === last)) chatOpenId = last;
      else chatLanding = true;
    }
    chatRemember(chatAgent || "spirits", chatOpenId || undefined);
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

async function loadChatRoster() {
  try {
    const res = await fetch("/api/agents/chat/roster");
    chatRoster = res.ok ? (((await res.json()).agents) || []) : [];
  } catch (e) { chatRoster = []; }
  if (chatAgent && !chatRosterEntry(chatAgent)) {
    // the section vanished (profile deleted / runner off) → fall back
    chatAgentSessions[chatAgent] = chatAgentSessions[chatAgent] || [];
  }
}

// loadChatSessions refreshes the OPEN section's list (spirits or one agent).
async function loadChatSessions() {
  if (chatAgent) {
    const agent = chatAgent;
    const tasks = fetch("/api/agents/chat/" + encodeURIComponent(agent) + "/tasks")
      .then((r) => (r.ok ? r.json() : { tasks: [] }))
      .then((d) => { chatAgentTasks[agent] = d.tasks || []; })
      .catch(() => { chatAgentTasks[agent] = []; });
    try { chatAgentSessions[agent] = ((await (await fetch(chatBase())).json()).sessions) || []; }
    catch (e) { chatAgentSessions[agent] = []; }
    await tasks;
    return;
  }
  try { chatSessions = ((await (await fetch("/api/chat/sessions")).json()).sessions) || []; }
  catch (e) { chatSessions = []; }
}

async function chatSpiritList() {
  if (chatSpiritsCache) return chatSpiritsCache;
  try { chatSpiritsCache = ((await (await fetch("/api/chat/spirits")).json()).spirits) || []; }
  catch (e) { chatSpiritsCache = []; }
  return chatSpiritsCache;
}

// ---- page head: CHAT · ＋ new · mic (the shared .agent-head anatomy) ----
// ＋ new acts on the SELECTED section and is lazy (cmd-ctr model): it opens
// the landing — nothing is created until the first message sends. The mic
// (voice → chat) dictates into the composer, as the terminal's does into
// the pty.
function renderChatHeadActions() {
  const host = document.getElementById("chatHeadActions");
  if (!host || host.dataset.built) return;
  host.dataset.built = "1";
  const add = el("button", "sprt-ghost", "＋ new");
  add.title = "new conversation in the open section";
  add.onclick = () => { location.hash = chatNewHash(); };
  host.append(add);
  if (typeof micButton === "function") {
    host.append(micButton((text) => {
      const ta = document.querySelector("#chatComposer textarea");
      if (!ta) return;
      ta.value = (ta.value ? ta.value.replace(/\s*$/, " ") : "") + text;
      ta.dispatchEvent(new Event("input"));
      ta.focus();
    }));
  }
}

// ---- rail: agent sections ----

function renderChatRail() {
  const host = document.getElementById("chatRail");
  if (!host) return;
  // a rename in progress must not be rebuilt from under the user (the
  // terminal's termRailBusy idiom); chatRename re-paints once it settles
  const a = document.activeElement;
  if (a && a.classList.contains("inline-rename") && host.contains(a)) return;
  host.innerHTML = "";
  const alfred = chatRosterEntry("alfred");
  const profiles = chatRoster.filter((a) => a.backend !== "portal" && a.name !== "alfred");
  const portals = chatRoster.filter((a) => a.backend === "portal");
  if (alfred) host.append(chatRailSection(alfred.name, alfred.label, alfred));
  if (profiles.length) {
    host.append(el("div", "micro-label chat-rail-group", "profiles"));
    profiles.forEach((p) => host.append(chatRailSection(p.name, p.label, p)));
  }
  // KAIROS · ZECK — their own sections, never the default (Q2/Q5)
  portals.forEach((p) => host.append(chatRailSection(p.name, p.label, p)));
  host.append(chatRailSection("", "spirits", null));
}

// shortModel — one model id shortener for the rail, the head and the landing.
function shortModel(m) { return (m || "").replace(/^claude-/, ""); }

// chatRailSection — one agent identity: a header (name · ✦ while a turn runs ·
// status dot · thread count) that selects the section, and, when open, a
// quiet ＋ new + its threads. Model/domain live in the head's title and on
// the landing's identity line, not in the rail.
function chatRailSection(agent, label, info) {
  const open = agent === chatAgent;
  const sec = el("div", "chat-rail-section" + (open ? " open" : ""));
  const head = el("div", "chat-rail-section-head");
  head.append(el("span", "micro-label chat-rail-section-name", label));
  const sessions = agent ? (chatAgentSessions[agent] || []) : chatSessions;
  // ✦ = a turn is running here (a thread thinking, or a portal order out)
  if (sessions.some((s) => s.status === "thinking") || (info && info.busy)) head.append(el("span", "chat-rail-live", "✦"));
  const enabled = info ? !!info.enabled : true;
  if (info) head.append(statusDot(enabled, enabled ? "ready" : (info.backend === "portal" ? "not configured" : "runner off")));
  const n = open || !info ? sessions.length : (info.sessions || 0);
  if (n) head.append(el("span", "aion-org-count", String(n)));
  if (!enabled) sec.classList.add("off");
  const tip = [];
  if (info && info.description) tip.push(info.description);
  if (info && info.backend === "portal") tip.push(info.domain === "ooda" ? "OODA portal chat" : "AION team portal chat");
  else if (info && info.model) tip.push(shortModel(info.model));
  if (info && !enabled) tip.push(info.backend === "portal" ? "not configured on this box" : "runner off");
  if (tip.length) head.title = tip.join(" · ");
  head.onclick = () => { if (!open) location.hash = chatSectionHash(agent); };
  sec.append(head);
  if (!open) return sec;

  const add = el("button", "o-ghost chat-rail-new", "＋ new");
  add.onclick = () => { location.hash = chatNewHash(); };
  sec.append(add);
  if (!sessions.length) {
    sec.append(emptyRow("no conversations yet"));
    if (agent) sec.append(chatRailTasks(agent));
    return sec;
  }
  sessions.forEach((s) => sec.append(chatRailRow(s, agent)));
  if (agent) sec.append(chatRailTasks(agent));
  return sec;
}

// chatRailRow — one thread row, shared by every section: title (dblclick or
// hover ✎ → inline rename) · ✦ while thinking · ☐ when promoted; meta shows
// who only where it varies (spirits) and always the time.
function chatRailRow(s, agent) {
  const row = el("div", "chat-rail-row" + (s.id === chatOpenId ? " open" : ""));
  const top = el("div", "chat-rail-top");
  const title = el("span", "chat-rail-title", s.title || s.id);
  title.title = "double-click to rename";
  title.ondblclick = (e) => { e.stopPropagation(); chatRename(title, s, agent); };
  top.append(title);
  if (s.status === "thinking") top.append(el("span", "chat-rail-live", "✦"));
  if (s.task) { const t = el("span", "chat-rail-task-mark", "☐"); t.title = "promoted to a task"; top.append(t); }
  const pen = el("button", "chat-rail-x", "✎");
  pen.title = "rename";
  // a spirit rename is refused by the engine mid-turn — say so before the click
  if (!agent && s.status === "thinking") { pen.disabled = true; pen.title = "rename after the turn finishes"; }
  pen.onclick = (e) => { e.stopPropagation(); chatRename(title, s, agent); };
  top.append(pen);
  row.append(top);
  const rm = el("div", "chat-rail-meta");
  if (!agent) rm.append(el("span", "chat-rail-spirit", s.spirit + (s.model ? " · " + shortModel(s.model) : "")));
  rm.append(el("span", "chat-rail-when", fmtWhen(s.updated || s.created)));
  row.append(rm);
  row.onclick = () => { location.hash = chatHash(s.id); };
  return row;
}

// chatRename — the library's inlineRename over the section's rename route
// (spirits: engine file rewrite; alfred/profiles: agentchat store; kairos/
// zeck: the portal thread — all four exist, plan §2.1). After the commit the
// cached list and the open head are patched in place — no loadChat() reload,
// so the transcript's scroll position survives.
function chatRename(nameEl, s, agent) {
  const inp = inlineRename(nameEl, s.title || "", async (v) => {
    try {
      await postJSONOk(chatBaseFor(agent) + "/" + encodeURIComponent(s.id) + "/rename", { title: v });
    } catch (e) { showToast(e.message || "rename failed"); renderChatRail(); return; }
    s.title = v;
    const list = agent ? (chatAgentSessions[agent] || []) : chatSessions;
    const cached = list.find((x) => x.id === s.id);
    if (cached) cached.title = v;
    if (chatCurSession && chatCurSession.id === s.id) { chatCurSession.title = v; chatRepaintHead(); }
    renderChatRail();
  });
  // settled without a change (Escape / unchanged blur): a rail paint that was
  // skipped while the input had focus still owes the current state
  inp.addEventListener("blur", () => setTimeout(renderChatRail, 0));
}

// chatRailTasks — the reverse bridge (§3.4f): the open todos this agent holds,
// under its threads, each a jump to the task's thread on the board. Quiet
// when it holds none.
const chatRailTasksMax = 8;
function chatRailTasks(agent) {
  const wrap = el("div", "chat-rail-tasks");
  const tasks = chatAgentTasks[agent] || [];
  if (!tasks.length) return wrap;
  wrap.append(el("div", "micro-label chat-rail-group", "tasks · " + tasks.length));
  tasks.slice(0, chatRailTasksMax).forEach((t) => {
    const row = el("div", "chat-rail-task");
    row.append(el("span", "chat-rail-task-text", t.text));
    const meta = [];
    if (t.container) meta.push(t.container);
    if (t.state) meta.push(t.state.replace(/-/g, " "));
    if (meta.length) row.append(el("span", "chat-rail-task-meta", meta.join(" · ")));
    row.title = (t.chatId ? "promoted from a conversation here · " : "") + "open the task thread";
    row.onclick = () => { location.hash = "#/tasks/" + encodeURIComponent(t.id); };
    wrap.append(row);
  });
  if (tasks.length > chatRailTasksMax) {
    const more = el("button", "sprt-quiet", "＋" + (tasks.length - chatRailTasksMax) + " more on the board");
    more.onclick = () => { location.hash = "#/tasks"; };
    wrap.append(more);
  }
  return wrap;
}

// chatPromoteTurn — "→ task" (§3.4f): the todo line is asked for (the title
// is the default), then the server creates it through the capture path,
// copies the conversation up to this turn into its thread and assigns the
// agent — record-only, no turn spent.
function chatPromoteTurn(session, turnN) {
  const agent = chatAgent;
  if (!agent) return;
  const title = session.title || "";
  askText("Task from this conversation — " + chatAgentLabel(agent) + " takes it", title ? "the todo line · empty = “" + title + "”" : "the todo line…", async (t) => {
    try {
      const r = await postJSONOk(chatBase() + "/" + encodeURIComponent(session.id) + "/promote", { turn: turnN, text: (t || "").trim() });
      const where = "#/tasks/" + encodeURIComponent(r.created);
      showToast("Task created" + (r.assigned ? " — " + (r.name || chatAgentLabel(agent)) + " holds it" : "") + " · open", () => { location.hash = where; }, "info");
      if (chatOpenId === session.id) { refetchChatSession(session.id); loadChatSessions().then(renderChatRail); }
    } catch (e) { showToast("Couldn't create the task — " + (e.message || "error")); }
  });
}

// renderChatLanding — the lazy new-chat landing (cmd-ctr model): a time-aware
// greeting (small, no accent spark — plan §7 Q7), the identity the FIRST SEND
// creates under (spirit/model chips for the spirits section; the agent's name
// + model for an agent section), and the centered composer. No session exists
// until the first message goes out, so "new conversation" can never feel dead.
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
  host.append(el("div", "chat-greeting", chatGreeting()));

  if (chatAgent) {
    const a = chatRosterEntry(chatAgent);
    const who = el("div", "chat-spirit-pick");
    who.append(el("span", "pill light on", a ? a.label : chatAgent));
    if (a && a.model) who.append(el("span", "sprt-quiet", shortModel(a.model)));
    if (a && a.profile) who.append(el("span", "chat-landing-hint", "hermes -p " + a.profile));
    if (a && a.backend === "portal") who.append(el("span", "chat-landing-hint", a.domain === "ooda" ? "OODA portal chat" : "AION team portal chat"));
    host.append(who);
    // what this agent is for — its `hermes profile describe` text (§3.5)
    if (a && a.description) host.append(el("div", "chat-landing-desc", a.description));
    if (a && !a.enabled) {
      host.append(emptyRow(a.backend === "portal"
        ? (a.label + "'s harness is not configured on this box — orders can't spool here.")
        : "The Hermes runner is not enabled on this box — turns can't run here."));
      return;
    }
    if (a && a.backend === "portal") {
      // the portal rules, said once: the thread is shared with the team, the
      // agent takes one order at a time, @name::intent picks a persona
      host.append(el("div", "chat-landing-hint", "shared with the portal · one order at a time · @" + a.name + "::brief tags an intent"));
      if (a.busy) host.append(el("div", "chat-thinking", "✦ " + a.label + " is running — a send now is refused until it finishes"));
    }
    host.append(el("div", "chat-landing-hint", "type below — Enter sends, the conversation starts then"));
    focusChatInput();
    return;
  }

  const spirits = (await chatSpiritList()).filter((s) => s.enabled);
  if (!spirits.length) {
    host.append(emptyRow("no chattable spirits (add a chat.md)"));
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

// ---- Compare: one prompt → N model lanes (UI-level fan-out; spirits only) ----

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
    back.onclick = () => { location.hash = "#/chat/spirits"; if (comp) comp.hidden = false; };
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
  const base = chatBase();
  try {
    const res = await fetch(base + "/" + encodeURIComponent(id));
    if (!res.ok) { renderChatEmpty(); return; }
    d = await res.json();
  } catch (e) { return; }
  if (id !== chatOpenId || base !== chatBase()) return; // navigated away mid-fetch
  const main = document.querySelector(".chat-main");
  if (main) main.classList.remove("landing");
  chatRemember(chatAgent || "spirits", id);
  renderChatTranscript(d);
  renderChatComposer(d.session);
  ensureChatStream(d.session);
  if ((d.queued || []).length || d.session.status === "thinking") ensureChatPoll(d.session, (d.queued || []).length);
}

// ---- live stream layer (A2): EventSource over the engine's event log ----
// SPIRITS ONLY — the Hermes-family backends have no event log; their turn
// paints from the file poll. The transcript render covers history from the
// session file; this layer paints the CURRENT turn as it happens — tool chips
// lighting up, thinking, and (once the engine streams deltas) the reply typing
// out with constant-speed reveal. On turn.completed the session refetches and
// the layer clears.
let chatES = null, chatESFor = "";
let chatLive = null; // {tools:[], thinking:"", thinkTok:0, say:"", revealed:0, open:true}
let chatRevealTimer = null, chatLastMd = 0;

function ensureChatStream(session) {
  if (chatAgent) { // no SSE for Hermes-family sessions: close any spirit stream
    if (chatES) { chatES.close(); chatES = null; chatESFor = ""; }
    chatLive = null;
    return;
  }
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
    const res = await fetch(chatBase() + "/" + encodeURIComponent(id));
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

// parseChatTurns splits the session body into turn blocks. The grammar is
// shared by spirit sessions (engine-written) and agent sessions (manifest-
// written, agentchat package) — one parser, one renderer.
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

// chatFileTokenRe — an attachment on an agent-chat user turn rides as its own
// line `[file:: <sha256> <name>]` (server agentchat.go); the renderer turns it
// into a download chip. Spirit turns never carry one.
const chatFileTokenRe = /^\[file:: ([0-9a-f]{64}) (.+?)\]$/;

// chatUserTurn renders a user turn: the text, then any attachment chips.
function chatUserTurn(text) {
  const b = el("div", "chat-turn chat-user");
  const lines = (text || "").split("\n");
  const files = [], keep = [];
  lines.forEach((ln) => {
    const m = ln.match(chatFileTokenRe);
    if (m) files.push({ hash: m[1], name: m[2] }); else keep.push(ln);
  });
  b.textContent = keep.join("\n").trim();
  if (files.length) {
    const chips = el("div", "chat-attach-chips");
    files.forEach((f) => {
      const a = document.createElement("a");
      a.className = "chat-attach-chip";
      a.textContent = "⤓ " + f.name;
      a.href = chatFileHref(f.hash);
      a.target = "_blank";
      chips.append(a);
    });
    b.append(chips);
  }
  return b;
}

// chatHead — the thread head in the .sprt-head anatomy every detail head
// follows: title (inline-rename target: dblclick or ✎) · sub (who · model) ·
// meta (fmtWhen(updated) · charge) · trailing actions (☐ task ↗ · delete).
function chatHead(s) {
  const who = s.spirit || (s.agent ? chatAgentLabel(s.agent) : "");
  const portal = chatIsPortal();
  const agent = chatAgent;
  const head = el("div", "sprt-head chat-head");
  const title = el("span", "sprt-title chat-head-title", s.title || s.id);
  title.title = "double-click to rename";
  title.ondblclick = () => chatRename(title, s, agent);
  head.append(title);
  const sub = [who];
  if (s.model) sub.push(shortModel(s.model));
  else if (portal) sub.push(s.domain === "ooda" ? "ooda portal" : "aion portal");
  if (portal && s.busy) sub.push("✦ running");
  head.append(el("span", "sprt-sub chat-head-sub", sub.filter(Boolean).join(" · ")));
  // portal runs are metered in the agent's own ledger, not per thread
  const meta = [fmtWhen(s.updated || s.created)];
  if (!portal) meta.push("$" + (s.spentUsd || 0).toFixed(4) + (s.ceilingUsd ? " / $" + s.ceilingUsd.toFixed(2) : ""));
  head.append(el("span", "sprt-head-meta chat-head-meta", meta.filter(Boolean).join(" · ")));
  const acts = el("span", "chat-head-acts");
  const ren = el("button", "sprt-quiet", "✎");
  ren.title = "rename";
  if (!agent && s.status === "thinking") { ren.disabled = true; ren.title = "rename after the turn finishes"; }
  ren.onclick = () => chatRename(title, s, agent);
  acts.append(ren);
  // the task this conversation became (§3.4f) — the link back to the board
  if (s.task) {
    const task = el("button", "sprt-quiet chat-head-task", "☐ task ↗");
    task.title = "open the task this conversation was promoted into";
    task.onclick = () => { location.hash = "#/tasks/" + encodeURIComponent(s.task); };
    acts.append(task);
  }
  // a portal thread is a shared team object: the cockpit's delete ARCHIVES it
  const base = chatBase();
  acts.append(armedDelete(portal ? "archive" : "delete", portal ? "archive — sure?" : "delete — sure?", async () => {
    try {
      const res = await fetch(base + "/" + encodeURIComponent(s.id), { method: "DELETE" });
      if (!res.ok) { showToast((await res.text()).slice(0, 120) || "delete failed"); return; }
      chatOpenId = "";
      chatRemember(agent || "spirits", "");
      location.hash = chatSectionHash(agent);
      loadChat();
    } catch (e) { showToast("delete failed"); }
  }));
  head.append(acts);
  return head;
}

// chatRepaintHead swaps the open head in place (after a rename) — the
// transcript and its scroll position stay.
function chatRepaintHead() {
  const cur = document.querySelector("#chatTranscript .chat-head");
  if (cur && chatCurSession) cur.replaceWith(chatHead(chatCurSession));
}

function renderChatTranscript(d) {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  bindChatScroll();
  host.innerHTML = "";
  const s = d.session;
  chatCurSession = s;
  chatLastUpdated = s.updated + "|" + s.status + "|" + (d.queued || []).length;
  const who = s.spirit || (s.agent ? chatAgentLabel(s.agent) : "");
  const portal = chatIsPortal();
  host.append(chatHead(s));

  parseChatTurns(d.body || "").forEach((t) => {
    if (t.who === "user") {
      host.append(chatUserTurn(t.text));
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
    const foot = el("div", "chat-turn-foot");
    // when the turn landed — a conversation with no times reads as stalled
    // while Alfred's turn takes minutes
    if (t.ts) foot.append(el("span", "chat-turn-when", fmtWhen(t.ts)));
    if (t.usd) foot.append(el("span", "chat-turn-usd", "$" + t.usd));
    // → task (§3.4f): every agent turn in an agent section can become work
    if (chatAgent) {
      const promote = el("button", "chat-turn-act", "→ task");
      promote.title = "make a task from this conversation (up to this turn) — " + who + " takes it";
      promote.onclick = () => chatPromoteTurn(s, t.n);
      foot.append(promote);
    }
    if (foot.childElementCount) wrap.append(foot);
    host.append(wrap);
  });

  (d.queued || []).forEach((q) => {
    const b = chatUserTurn(q);
    b.classList.add("chat-queued");
    host.append(b);
  });
  // the live layer's mount point (turn.started paints into it); when the ES
  // hasn't caught the turn yet — or there is no ES at all (agent sessions) —
  // the status flag still shows a quiet indicator
  const area = el("div", "chat-live-area");
  area.id = "chatLiveArea";
  host.append(area);
  if (s.status === "thinking" && !chatLive) {
    area.append(el("div", "chat-thinking", portal ? "✦ order spooled — " + who + " answers when its run lands…" : "✦ thinking…"));
  }
  chatStick = true;
  chatPin();
}

// ---- composer ----
// One composer for every section. Attachments (agent sections only) upload
// first and ride the send as `files`: Hermes sections use the todo-thread file
// store, portal sections the agent's own artifact domain (chat_attach.go).
// Both land on the user turn as [file::] tokens. Portal sections add the
// ask|propose ritual pill and the @-mention typeahead (persona intents).

let chatPendingFiles = [];

// chatMentionOptions — the tokens the typeahead offers for the open portal
// agent: @name (it decides how to answer) + @name::intent per persona.
function chatMentionOptions(prefix) {
  const a = chatRosterEntry(chatAgent);
  if (!a || a.backend !== "portal") return [];
  const out = [{ token: "@" + a.name, note: "no intent tag · it decides how to answer" }];
  (a.personas || []).forEach((p) => out.push({ token: "@" + a.name + "::" + p, note: p }));
  const q = (prefix || "").toLowerCase();
  return out.filter((o) => o.token.slice(1).startsWith(q) || o.token.startsWith(q));
}

function renderChatComposer(session) {
  const host = document.getElementById("chatComposer");
  if (!host) return;
  const syncAttach = () => {
    const btn = host.querySelector(".chat-attach");
    if (btn) btn.hidden = !chatAgent;
    const rit = host.querySelector(".chat-ritual");
    if (rit) {
      rit.hidden = !chatIsPortal();
      rit.querySelectorAll(".filter-chip").forEach((c) => c.classList.toggle("on", c.dataset.ritual === chatRitual));
    }
    const chips = host.querySelector(".chat-attach-chips");
    if (chips) {
      chips.innerHTML = "";
      chips.hidden = !chatPendingFiles.length;
      // each pending chip drops with its ✕ — a wrongly picked file never has to send
      chatPendingFiles.forEach((f, i) => {
        const chip = el("span", "chat-attach-chip", "⤓ " + f.name);
        const x = el("button", "chat-attach-x", "✕");
        x.title = "drop this attachment";
        x.onclick = () => { chatPendingFiles.splice(i, 1); syncAttach(); };
        chip.append(x);
        chips.append(chip);
      });
    }
  };
  const placeholder = () => {
    const a = chatRosterEntry(chatAgent);
    if (chatIsPortal()) {
      if (session && session.busy) return "✦ " + (a ? a.label : chatAgent) + " is running — one order at a time";
      if (session && session.status === "thinking") return "✦ waiting on " + (a ? a.label : chatAgent) + "…";
      return "Message… · @ to tag an intent";
    }
    return session && session.status === "thinking" ? "✦ thinking — messages queue…" : "Message…";
  };
  // a portal agent takes one order at a time: a send while it runs 409s, so
  // the button says so instead (the placeholder already says why)
  const busy = !!(session && session.busy);
  if (host.dataset.built) {
    const ta = host.querySelector("textarea");
    const send = host.querySelector(".chat-send");
    if (ta) ta.placeholder = placeholder();
    if (send) send.disabled = busy;
    syncAttach();
    return;
  }
  host.dataset.built = "1";
  const ta = document.createElement("textarea");
  ta.className = "chat-input";
  ta.rows = 1;
  ta.placeholder = placeholder();
  // auto-grow with content (target feel): reset then snap to scrollHeight,
  // clamped so a long paste scrolls inside instead of shoving the transcript.
  const grow = () => { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, window.innerHeight * 0.4) + "px"; };
  ta.addEventListener("input", grow);
  // @-mention typeahead (portal sections): the word at the caret starting
  // with @ opens the list; click/Tab/Enter inserts, Escape closes.
  const mention = el("div", "chat-mention");
  mention.hidden = true;
  const mentionPrefix = () => {
    if (!chatIsPortal()) return null;
    const head = ta.value.slice(0, ta.selectionStart);
    const m = head.match(/(^|\s)@([a-z0-9:-]*)$/i);
    return m ? m[2] : null;
  };
  const pickMention = (tok) => {
    const at = ta.selectionStart;
    const head = ta.value.slice(0, at).replace(/@[a-z0-9:-]*$/i, tok + " ");
    ta.value = head + ta.value.slice(at);
    ta.selectionStart = ta.selectionEnd = head.length;
    mention.hidden = true;
    grow();
    ta.focus();
  };
  const syncMention = () => {
    const prefix = mentionPrefix();
    const opts = prefix === null ? [] : chatMentionOptions(prefix);
    mention.innerHTML = "";
    mention.hidden = !opts.length;
    opts.forEach((o) => {
      const row = el("button", "chat-mention-row");
      row.append(el("span", "chat-mention-tok", o.token), el("span", "chat-mention-note", o.note));
      row.onmousedown = (e) => { e.preventDefault(); pickMention(o.token); };
      mention.append(row);
    });
  };
  ta.addEventListener("input", syncMention);
  ta.addEventListener("blur", () => { mention.hidden = true; });
  const chips = el("div", "chat-attach-chips");
  chips.hidden = true;
  const fi = document.createElement("input");
  fi.type = "file";
  fi.multiple = true;
  fi.hidden = true;
  fi.onchange = async () => {
    for (const f of [...fi.files]) {
      try {
        const url = chatIsPortal()
          ? chatAttachBase() + "?name=" + encodeURIComponent(f.name) + (chatOpenId ? "&thread=" + encodeURIComponent(chatOpenId) : "")
          : "/api/tasks/thread/file?id=agentchat&name=" + encodeURIComponent(f.name);
        const res = await fetch(url, { method: "POST", body: f });
        if (!res.ok) throw new Error((await res.text()).slice(0, 120));
        const d = await res.json();
        chatPendingFiles.push({ hash: d.file.hash, name: d.file.name, size: d.file.size });
      } catch (e) { showToast("Upload failed — " + (e.message || "error")); }
    }
    fi.value = "";
    syncAttach();
  };
  const attach = el("button", "chat-attach", "＋");
  attach.title = "attach a file";
  attach.onclick = () => fi.click();
  // ask | propose — the portals' two outcomes as a filter-chip pair, one on;
  // propose = the delegate ritual (proposals a person approves in the
  // portal), ask = read-only
  const ritual = el("span", "chat-ritual");
  ritual.hidden = true;
  [["ask", "ask", "answers, changes nothing"], ["delegate", "propose", "returns proposals you approve in the portal"]].forEach(([key, label, tip]) => {
    const c = el("button", "filter-chip", label);
    c.dataset.ritual = key;
    c.title = tip;
    c.onclick = () => { chatRitual = key; syncAttach(); ta.focus(); };
    ritual.append(c);
  });
  const send = el("button", "chat-send", "↑");
  send.title = "send · Enter (Shift+Enter for a new line)";
  send.disabled = busy;
  const submit = async () => {
    const text = ta.value.trim();
    const files = chatPendingFiles.slice();
    if (!text && !files.length) return;
    if (send.disabled) return;
    ta.value = "";
    grow();
    mention.hidden = true;
    chatPendingFiles = [];
    syncAttach();
    const payload = chatIsPortal() ? { text, files, ritual: chatRitual } : { text, files };
    try {
      if (chatOpenId) {
        await postJSONOk(chatBase() + "/" + encodeURIComponent(chatOpenId) + "/messages", chatAgent ? payload : { text });
      } else if (chatAgent) {
        // lazy create under the open agent section: the first send creates
        const r = await postJSONOk(chatBase(), payload);
        chatOpenId = r.id;
        chatLanding = false;
        location.hash = chatHash(r.id);
        return;
      } else {
        // lazy create (cmd-ctr model): the landing's chosen spirit/model
        const r = await postJSONOk("/api/chat/sessions", {
          spirit: chatPendingSpirit || "concierge", model: chatPendingModel || "", text,
        });
        chatOpenId = r.id;
        chatLanding = false;
        location.hash = chatHash(r.id);
        return;
      }
      loadChatSession(chatOpenId);
      loadChatSessions().then(renderChatRail);
    } catch (e) { showToast("Send failed — " + (e.message || "error")); ta.value = text; chatPendingFiles = files; grow(); syncAttach(); }
  };
  ta.addEventListener("keydown", (e) => {
    if (!mention.hidden && (e.key === "Escape")) { e.preventDefault(); mention.hidden = true; return; }
    if (!mention.hidden && (e.key === "Tab" || e.key === "Enter")) {
      const first = mention.querySelector(".chat-mention-tok");
      if (first) { e.preventDefault(); pickMention(first.textContent); return; }
    }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  });
  send.onclick = submit;
  host.append(chips, mention, ta, fi, attach, ritual, send);
  syncAttach();
}

// ---- live poll (file-derived; stops when idle; every backend) ----
// Portal sections poll slower (4s, the portals' own cadence): every read
// there runs the server's chatSweep over the agent's run reports.

function ensureChatPoll(session, queued) {
  const active = session && (session.status === "thinking" || queued > 0);
  if (!active) { if (chatPollTimer) { clearInterval(chatPollTimer); chatPollTimer = null; } return; }
  if (chatPollTimer) return;
  const every = chatIsPortal() ? 4000 : 1500;
  chatPollTimer = setInterval(async () => {
    if (els.chatView.hidden || !chatOpenId) {
      clearInterval(chatPollTimer); chatPollTimer = null; return;
    }
    let d;
    try {
      const res = await fetch(chatBase() + "/" + encodeURIComponent(chatOpenId));
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
  }, every);
}

function loadChat() { showChat(location.hash); }

// ---- hooks for the palette / floating chat / capture handoff ----

// chatOpenSession opens a SPIRIT session by id (the palette/floating chat
// callers index spirit sessions); agent threads route through their section.
function chatOpenSession(id) { location.hash = "#/chat/" + encodeURIComponent(id); }
function chatOpenAgentSession(agent, id) { location.hash = "#/chat/a/" + encodeURIComponent(agent) + "/" + encodeURIComponent(id); }

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
  const out = sessions.slice(0, 30).map((s) => ({
    id: "chat:" + s.id,
    name: "Chat · " + (s.title || s.id),
    hint: s.spirit + " · conversation",
    keywords: "chat conversation " + s.spirit,
    act: () => { closeCmdbar(); chatOpenSession(s.id); },
  }));
  Object.keys(chatAgentSessions).forEach((agent) => {
    (chatAgentSessions[agent] || []).slice(0, 20).forEach((s) => out.push({
      id: "chat:" + agent + ":" + s.id,
      name: "Chat · " + (s.title || s.id),
      hint: chatAgentLabel(agent) + " · conversation",
      keywords: "chat conversation " + agent + (chatIsPortal(agent) ? " portal team" : " alfred hermes"),
      act: () => { closeCmdbar(); chatOpenAgentSession(agent, s.id); },
    }));
  });
  return out;
});
cmdRegistry.register(() => [{
  id: "act:new-chat-alfred", name: "New chat with Alfred", hint: "chat · action",
  keywords: "chat talk converse ask alfred hermes agent",
  act: () => { closeCmdbar(); location.hash = "#/chat/a/alfred/new"; },
}, {
  id: "act:new-chat", name: "New chat with a spirit", hint: "chat · action",
  keywords: "chat talk converse ask concierge spirit",
  act: () => { closeCmdbar(); chatCompose("concierge", ""); },
}]);
