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
//   claude/codex     /api/terminal/sessions + …/session/<id>/{transcript,screen,
//                    input}: the CLI writes its own jsonl, manifest reads it
//                    (Stage S); the runtime owns the process; input uses its adapter
// The poll while a session is thinking is the live progress channel for every
// backend — the file IS the stream (run-report idiom). Globals
// chatOpenSession/chatCompose are the hooks the palette, floating chat (P4),
// and capture handoff (P5) drive.

let chatSessions = [];      // last /api/chat/sessions fetch (spirits)
let chatOpenId = "";        // the open session id ("" = none)
let chatSpiritsCache = null;
let chatPollTimer = null;
let chatRouteVersion = 0;
let chatSending = false;
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
// A task's conversation opens IN CHAT — the dedicated task-thread stage
// (#/chat/task/<id>), not the board's panel. The stage's own head keeps the
// "open task ↗" escape back to TASKS for the record itself.
function chatTaskThreadHash(id) { return "#/chat/task/" + encodeURIComponent(id); }
function chatSectionHash(agent) { return agent ? "#/chat/a/" + encodeURIComponent(agent) : "#/chat/spirits"; }
function chatPrivateCreationAgent(agent){
  if(["kairos","zeck"].includes(agent))return agent+"-private";
  return agent;
}
function chatNewHash() { const agent=chatPrivateCreationAgent(chatAgent);return agent ? "#/chat/a/" + encodeURIComponent(agent) + "/new" : "#/chat/new"; }
function chatCurrentSessions() {
  if (chatIsTerm()) return chatTermList(chatAgent);
  return chatAgent ? (chatAgentSessions[chatAgent] || []) : chatSessions;
}
function chatRosterEntry(name) { return chatRoster.find((a) => a.name === name) || null; }
function chatAgentLabel(name) { const a = chatRosterEntry(name); return a ? a.label : name; }
// portal sections (kairos/zeck): attachments go to the agent's own artifact
// domain, sends carry a ritual, and @-mentions tag a persona intent
function chatIsPortal(name) { const a = chatRosterEntry(name === undefined ? chatAgent : name); return !!(a && a.backend === "portal"); }
function chatAttachBase() { return "/api/agents/chat/" + encodeURIComponent(chatAgent) + "/attach"; }
function chatFileHref(hash) { return chatIsPortal() ? chatAttachBase() + "/" + hash : "/api/tasks/thread/file/" + hash + "?id=agentchat"; }
let chatRitual = "ask"; // portal sends: ask | delegate (the portals' two outcomes)

// chatRouteSegments splits a #/chat/… tail into its ENCODED segments and
// decodes each one on its own. A portal thread id carries a slash of its own
// (`th/new-1786943020701`), so decoding the whole tail first cut those ids in
// half — the route then asked for a session called "th" and painted the empty
// stage. Legacy hashes that spelled such an id raw still work: the caller
// rejoins the segments past the ones it consumed.
function chatRouteSegments(h) {
  const raw = h && h.startsWith("#/chat/") ? h.slice("#/chat/".length) : "";
  if (!raw) return [];
  return raw.split("/").map((s) => { try { return decodeURIComponent(s); } catch (e) { return s; } });
}

// Headers occupy their own flex row; output never scrolls behind them.
function chatMountHeader(head) {
  const transcript = document.getElementById("chatTranscript");
  if (!transcript) return;
  let slot = document.getElementById("chatThreadHeader");
  if (!slot) { slot = el("div", "chat-thread-header"); slot.id = "chatThreadHeader"; transcript.before(slot); }
  if (head && !head.querySelector(".chat-latest")) {
    const latest = el("button", "sprt-quiet chat-latest", "Latest ↓");
    latest.title = "Return to the latest output";
    latest.onclick = () => { chatStick = true; chatPin(); chatSaveReadingPosition(); };
    head.append(latest);
  }
  slot.replaceChildren(...(head ? [head] : []));
  slot.hidden = !head;
}
const chatDrafts = new Map();
let chatDraftKey = "";
function chatSaveDraft() {
  const input = document.querySelector("#chatComposer textarea");
  if (chatDraftKey && input) chatDrafts.set(chatDraftKey, {text: input.value, files: chatPendingFiles.slice()});
  if(chatDraftKey && input)chatCaptureSyncedDraft(chatDraftKey);
}

const chatSyncedDrafts=new Map();
const chatRecipients=new Map();
async function chatPrepareLandingDraft(agent) {
  // A private composer slot, not a conversation: no session is created here.
  const key=(agent||"spirits")+"/new";
  try {
    const digest=await crypto.subtle.digest("SHA-256",new TextEncoder().encode(key));
    const id=Array.from(new Uint8Array(digest).slice(0,16),b=>b.toString(16).padStart(2,"0")).join("");
    await chatPrepareDraft({key:"landing-"+id},key);
  } catch(e) { showToast("New-chat draft sync is unavailable; keep this tab open."); }
}
async function chatPrepareDraft(descriptor,key,initial){
  if(!descriptor?.key || typeof ChatDraftState==="undefined")return;
  if(chatSyncedDrafts.has(key)){await chatSyncedDrafts.get(key).refresh();await chatLoadDeliveryRecovery(key);await chatReconcileAcceptedDrafts(key);return;}
  const state=new ChatDraftState(descriptor.key,(current,apply)=>{
    if(apply)chatApplySyncedDraft(key,current.value);
    if(chatDraftKey===key)chatRenderDraftNotice(document.getElementById("chatComposer"),key);
  });
  chatSyncedDrafts.set(key,state);
  const local=chatDrafts.get(key);
  if(local && (local.text || local.files?.length))state.set({...local,selection:chatArtifactSelections.get("chat:"+key)||null,task:chatConversationTasks.get("chat:"+key)||""});
  await state.refresh();
  await chatLoadDeliveryRecovery(key);
  await chatReconcileAcceptedDrafts(key);
  if(initial&&state.revision===0&&state.value===null&&!state.error){state.set(initial);}
  chatApplySyncedDraft(key,state.value);
}
function chatApplySyncedDraft(key,value){
  const v=value||{text:"",files:[]};
  chatDrafts.set(key,{text:typeof v.text==="string"?v.text:"",files:Array.isArray(v.files)?v.files:[]});
  if(v.selection)chatArtifactSelections.set("chat:"+key,v.selection);else chatArtifactSelections.delete("chat:"+key);
  if(v.task)chatConversationTasks.set("chat:"+key,v.task);
  if(v.recipient)chatRecipients.set(key,v.recipient);else chatRecipients.delete(key);
  if(chatDraftKey!==key)return;
  const input=document.querySelector("#chatComposer textarea");
  if(input){if(typeof chatRepaintHead==="function")chatRepaintHead();input.value=chatDrafts.get(key).text;chatPendingFiles=chatDrafts.get(key).files.slice();renderChatComposer(chatCurSession);input.style.height="auto";input.style.height=Math.min(input.scrollHeight,Math.max(120,innerHeight*.4))+"px";}
}
function chatCaptureSyncedDraft(key){
  const state=chatSyncedDrafts.get(key),input=document.querySelector("#chatComposer textarea");
  if(!state || key!==chatDraftKey || !input)return;
  state.set({text:input.value,files:chatPendingFiles.slice(),selection:chatArtifactSelections.get("chat:"+key)||null,task:chatConversationTasks.get("chat:"+key)||chatCurSession?.task||"",recipient:chatRecipients.get(key)||null});
}
function chatRenderDraftNotice(host,key){chatRenderStateNotice(host,chatSyncedDrafts.get(key));}
function chatRenderStateNotice(host,state){
  if(!host)return;host.querySelector(".chat-draft-notice")?.remove();
  if(!state || (!state.conflict&&!state.error))return;
  const row=el("div","chat-draft-notice");row.setAttribute("role","status");
  row.append(el("span","",state.conflict?"Draft changed on another device. Choose which to keep.":state.error));
  if(state.conflict){
    const preview=el("details","chat-draft-preview");
    preview.append(el("summary","","View saved draft"),el("p","",state.conflict.value?.text||"Empty draft"));
    if(state.conflict.value?.files?.length)preview.append(el("p","",state.conflict.value.files.map(f=>f.name).join(", ")));
    row.append(preview);
    const saved=el("button","sprt-quiet","Use saved draft"),mine=el("button","sprt-quiet","Keep this draft");
    saved.onclick=()=>state.resolve(true);mine.onclick=()=>state.resolve(false);row.append(saved,mine);
  }else{const retry=el("button","sprt-quiet","Retry sync");retry.onclick=()=>state.refresh();row.append(retry);}
  host.prepend(row);
}
const chatRecoveryRefreshes=new Map();
function chatRefreshCurrentDraft(){
 const key=chatDraftKey,state=chatSyncedDrafts.get(key);if(!state)return Promise.resolve();
 if(chatRecoveryRefreshes.has(key))return chatRecoveryRefreshes.get(key);
 const job=Promise.resolve().then(async()=>{
  await state.refresh();await chatLoadDeliveryRecovery(key);await chatReconcileAcceptedDrafts(key);
  const host=document.getElementById("chatComposer");if(chatDraftKey===key&&host)chatRenderDeliveryNotice(host,key);
 }).catch(()=>{}).finally(()=>chatRecoveryRefreshes.delete(key));
 chatRecoveryRefreshes.set(key,job);return job;
}
window.addEventListener("focus",()=>{chatRefreshCurrentDraft();Promise.all([chatLoadPins(),chatLoadWorkstreams()]).then(()=>{chatRenderWorkstreamFilter();renderChatInboxRows();});});
document.addEventListener("visibilitychange",()=>{if(!document.hidden)chatRefreshCurrentDraft();});
window.addEventListener("pagehide",()=>{chatSaveDraft();for(const state of chatSyncedDrafts.values())if(state.dirty)state.flush();});
function showChat(h) {
  chatCloseWorkspace();
  chatSaveDraft();
  const readingHost = document.getElementById("chatTranscript");
  if (readingHost) readingHost.dataset.readKey = "";
  chatReadingGestureUntil = 0;
  chatDraftKey = "";
  chatPendingFiles = [];
  chatMountHeader(null);
  const routeVersion = ++chatRouteVersion;
  if (chatPollTimer) { clearInterval(chatPollTimer); chatPollTimer = null; }
  const seg = chatRouteSegments(h);
  const head = seg[0] || "";
  const rest = seg.slice(1).join("/"); // one id, whether it was encoded or raw
  if (head === "cmp") {
    leaveTaskChat();
    renderCompare(rest.split(",").filter(Boolean));
    return;
  }
  // A task's native thread has no agent-chat session to load. Give it the
  // same full CHAT stage (rather than sending it to an agent landing page).
  if (head === "task") {
    const taskID = rest;
    if (taskID) {
      renderChatHeadActions();
      renderTaskChat(taskID);
      requestAnimationFrame(chatFitShell);
      if (!chatFitBound) { window.addEventListener("resize", chatFitShell); chatFitBound = true; }
      return;
    }
  }
  leaveTaskChat();
  // route → section + thread. Agent routes: a/<agent>[/new|/<id>]; spirit
  // routes keep their old shapes (new, <id>, "spirits" = the section itself).
  // A section route (a/<agent> or "spirits") shows the section's list + its
  // landing; a thread opens only when the hash names one, or on bare #/chat
  // (the remembered thread of the remembered section).
  let restore = false;
  if (head === "a") {
    chatAgent = seg[1] || "alfred";
    const sub = seg.slice(2).join("/");
    if (sub && sub !== "new") { chatOpenId = sub; chatLanding = false; }
    else { chatOpenId = ""; chatLanding = true; }
  } else if (head === "spirits" || head === "new") { chatAgent = ""; chatOpenId = ""; chatLanding = true; }
  else if (head) { chatAgent = ""; chatOpenId = seg.join("/"); chatLanding = false; }
  else { restore = true; chatOpenId = ""; chatLanding = false; }
  renderChatHeadActions();
  loadChatRoster().then(async () => {
    if (routeVersion !== chatRouteVersion || els.chatView.hidden) return;
    if(!restore && chatLanding && chatPrivateCreationAgent(chatAgent)!==chatAgent){
      location.replace("#/chat/a/"+encodeURIComponent(chatPrivateCreationAgent(chatAgent))+"/new");return;
    }
    if (restore) {
      // bare #/chat → the remembered section, else ALFRED when it can take a
      // turn, else spirits (the pre-Phase-1 behaviour)
      const remembered = chatRecall("manifest.chatSection");
      const alfred = chatRosterEntry("alfred");
      if (remembered === "spirits") chatAgent = "";
      else if (remembered && (chatRosterEntry(remembered) || chatIsTerm(remembered))) chatAgent = remembered;
      else chatAgent = alfred && alfred.enabled ? "alfred" : "";
    }
    // the terminal registry feeds the CLAUDE CODE / CODEX section heads
    // whatever section is open, so it loads alongside the section's own list
    await Promise.all([loadChatSessions(), loadChatTermSessions(false)]);
    if (routeVersion !== chatRouteVersion || els.chatView.hidden) return;
    ensureTerminalEvents();
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

let chatTaskID = "";
let chatTaskData = null;
let chatTaskPollTimer = null;
let chatTaskRailKey = "";
const chatConversationTasks = new Map();

function leaveTaskChat() {
  const transcript = document.getElementById("chatTranscript");
  if (transcript) delete transcript.dataset.task;
  chatTaskID = "";
  chatTaskData = null;
  chatTaskRailKey = "";
  if (chatTaskPollTimer) { clearInterval(chatTaskPollTimer); chatTaskPollTimer = null; }
  const shell = document.querySelector(".chat-shell");
  if (shell) shell.classList.remove("task-thread");
  const composer = document.getElementById("chatComposer");
  if (composer) {
    composer.classList.remove("task-composer");
    composer.innerHTML = "";
    composer.dataset.built = "";
  }
}

// A task may be linked to the conversation that created it or simply to its
// assignee's chat section.  In either case, keep the task view in the same
// sectioned rail as ordinary chats, with that agent expanded.
function taskChatAgent(data) {
  return data && data.chat && data.chat.agent ? data.chat.agent : "";
}

async function renderTaskChatRail(data) {
  const agent = taskChatAgent(data);
  const key = chatTaskID + "\u0000" + agent;
  if (key === chatTaskRailKey) return;
  chatTaskRailKey = key;
  chatAgent = agent;
  chatOpenId = "";
  chatTermSurface(false); // a task thread is a chat, whichever section it hangs under
  await loadChatRoster();
  if (chatTaskID === "") return;
  await Promise.all([loadChatSessions(), loadChatTermSessions(false), (async()=>{
    if (!todosCache) { const r=await fetch("/api/tasks"); if(r.ok)todosCache=await r.json(); }
  })()]);
  if (chatTaskID !== "") renderChatRail();
}

async function renderTaskChat(taskID, refetch) {
  if (chatTaskID !== taskID) { leaveTaskChat(); chatTaskID = taskID; }
  const shell = document.querySelector(".chat-shell");
  if (shell) shell.classList.add("task-thread");
  const host = document.getElementById("chatTranscript");
  const composer = document.getElementById("chatComposer");
  if (!host || !composer) return;
  if (refetch || !chatTaskData) {
    try {
      const res = await fetch("/api/tasks/panel?id=" + encodeURIComponent(taskID));
      if (!res.ok) throw new Error("task not found");
      const data = await res.json();
      if (chatTaskID !== taskID) return;
      chatTaskData = data;
    } catch (e) {
      host.innerHTML = "";
      host.append(emptyRow("task conversation unavailable"));
      composer.innerHTML = "";
      return;
    }
  }
  const d = chatTaskData;
  if(d.chat?.canonical && d.chat.id){
    const key="chat:"+d.chat.agent+"/"+d.chat.id;
    chatConversationTasks.set(key,taskID);
    if(chatPendingWorkspace?.task===taskID)chatPendingWorkspace={...chatPendingWorkspace,selectionKey:key};
    location.replace("#/chat/a/"+encodeURIComponent(d.chat.agent)+"/"+encodeURIComponent(d.chat.id));
    return;
  }
  await todoPrepareDraft(d,taskID);
  if(chatTaskID!==taskID)return;
  const readKey = d.conversation?.key || "";
  const restoreReading = host.dataset.readKey !== readKey;
  if (restoreReading) await chatPrepareReadingPosition(d.conversation);
  if(chatTaskID!==taskID)return;
  await renderTaskChatRail(d);
  if (chatTaskID !== taskID) return;
  const sameThread = host.dataset.task === taskID;
  const oldScroll = host.scrollTop;
  const following = !sameThread || host.scrollHeight - host.scrollTop - host.clientHeight < 80;
  host.dataset.task = taskID;
  host.dataset.readKey = readKey;
  if (restoreReading) chatReadingGestureUntil = 0;
  bindChatScroll();
  host.innerHTML = "";
  const rec = d.record || {};
  // Keep the task context in the normal thread-head anatomy: title, agent
  // context and task id, then the task-specific return action.
  const head = el("div", "sprt-head chat-head");
  head.append(el("span", "sprt-title chat-head-title", rec.Title || rec.title || todoRowInfo(taskID)?.text || taskID));
  const agent = taskChatAgent(d);
  head.append(el("span", "sprt-sub chat-head-sub", ["task conversation", agent ? chatAgentLabel(agent) : "unassigned"].join(" · ")));
  head.append(el("span", "sprt-head-meta chat-head-meta", taskID));
  const runtimeBadge = terminalRunBadge(d.delegation);
  if (runtimeBadge) head.append(runtimeBadge);
  const acts = el("span", "chat-head-acts");
  const back = el("button", "sprt-quiet", "open task ↗");
  back.title = "open this task in TASKS";
  back.onclick = () => { location.hash = "#/tasks/" + encodeURIComponent(taskID); };
  acts.append(back);
  head.append(acts);
  head.append(chatArtifactActions(d));
  chatMountHeader(head);
  (d.thread || []).forEach((c) => host.append(chatTaskThreadEntry(c, taskID)));
  if (d.inflight) host.append(el("div", "chat-thinking", "✦ " + (d.inflight.name || "agent") + " is working…"));
  if (!(d.thread || []).length && !d.inflight) host.append(emptyRow("no comments yet"));
  appendTaskApprovals(host, d);
  if (composer.dataset.task !== taskID || !composer.querySelector("textarea")) {
    composer.innerHTML = "";
    composer.dataset.task = taskID;
    composer.classList.add("task-composer");
    composer.append(todoComposer(d, { taskID,
      onPosted: () => renderTaskChat(taskID, true) }));
  }
  if (!chatTaskPollTimer) {
    chatTaskPollTimer = setInterval(async () => {
      if (document.hidden || chatTaskID !== taskID || !location.hash.startsWith("#/chat/task/")) return;
      try {
        const fresh = await (await fetch("/api/tasks/panel?id=" + encodeURIComponent(taskID))).json();
        if (chatTaskID === taskID && JSON.stringify(fresh) !== JSON.stringify(chatTaskData)) {
          chatTaskData = fresh;
          renderTaskChat(taskID, false);
        }
      } catch (e) {}
    }, 2000);
  }
  host.scrollTop = following ? host.scrollHeight : oldScroll;
  chatStick = following;
  if (restoreReading) chatRestoreReadingPosition(host, chatReadingStates.get(readKey)?.value);
  if (chatPendingWorkspace?.task === taskID) { const spec=chatPendingWorkspace;chatPendingWorkspace=null;chatOpenWorkingArtifact(spec); }
}

function chatTaskThreadEntry(c, taskID) {
  const mine = !(c.author || "").startsWith("agent:");
  const wrap = el("div", "chat-turn " + (mine ? "chat-user" : "chat-spirit"));
  if (c.id) wrap.dataset.chatReadTurn = "comment:" + c.id;
  if (c.text) {
    if (mine) wrap.textContent = c.text;
    else {
      const say = el("div", "chat-say");
      try { say.append(renderMarkdown(c.text, "", { readOnly: true })); }
      catch (e) { say.textContent = c.text; }
      wrap.append(say);
    }
  }
  (c.files || []).forEach((f) => {
    const a = document.createElement("a");
    a.className = "chat-attach-chip";
    a.textContent = "⤓ " + f.name;
    a.href = "/api/tasks/thread/file/" + f.hash + "?id=" + encodeURIComponent(taskID);
    a.target = "_blank";
    wrap.append(a);
  });
  for (const ref of c.meta?.context || []) {
    const link=el("button","chat-attach-chip","Referenced version ↗");
    link.onclick=()=>chatOpenWorkingArtifact({id:ref.id,revision:ref.revision,task:taskID});
    wrap.append(link);
  }
  const foot = el("div", "chat-turn-foot");
  foot.append(el("span", "chat-turn-when", (c.author_name || c.authorName || c.author || "?") + " · " + fmtWhen(c.at)));
  if (c.action && c.action !== "comment") foot.append(el("span", "chat-turn-usd", c.action));
  wrap.append(foot);
  return wrap;
}

// chatFitShell sizes the shell to exactly the space below its top edge (mirrors
// termFitShell) so it fills correctly whatever chrome sits above it — e.g. with
// the crumb bar hidden on this section, the shell reclaims that height.
let chatFitBound = false;
function chatFitShell() {
  const shell = document.querySelector(".chat-shell");
  if (!shell || els.chatView.hidden) return;
  const top = shell.getBoundingClientRect().top;
  const phone = window.mf && window.mf.phone();
  const height = phone && window.visualViewport ? window.visualViewport.height : window.innerHeight;
  shell.style.height = Math.max(phone ? 180 : 320, height - top - 14) + "px";
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

// Load conversation summaries together so the inbox can sort across agents.
async function loadChatSessions() {
  const agents = chatRoster.filter(a => !chatIsTerm(a.name)).map(a => a.name);
  await Promise.all([chatLoadPins(),chatLoadWorkstreams(),...["", ...agents].map(async agent => {
    try {
      const res = await fetch(chatBaseFor(agent));
      if (!res.ok) return; // retain the last good directory during an outage
      const rows = (await res.json()).sessions || [];
      if (agent) chatAgentSessions[agent] = rows; else chatSessions = rows;
    } catch (e) {}
  })]);
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
  add.title = "Start a conversation with an agent";
  add.textContent = "New chat";
  add.onclick = () => {
    const existing = host.querySelector(".chat-new-picker");
    if (existing) { existing.remove(); return; }
    const picker = document.createElement("select");
    picker.className = "chat-new-picker"; picker.setAttribute("aria-label", "Choose an agent for a new chat");
    const prompt = el("option", "", "Choose an agent…"); prompt.value = "pick"; picker.append(prompt);
    [...chatRoster.filter(a => !chatIsTerm(a.name) && chatPrivateCreationAgent(a.name)===a.name).map(a => [a.name, a.label]), ...(chatTermEnabled ? Object.entries(chatTermKinds) : []), ["", "Spirits"]].forEach(([value, label]) => {
      const option = el("option", "", label); option.value = value; picker.append(option);
    });
    picker.onchange = () => { if (picker.value === "pick") return; location.hash = picker.value ? "#/chat/a/" + encodeURIComponent(picker.value) + "/new" : "#/chat/new"; picker.remove(); };
    host.append(picker); picker.focus();
  };
  host.append(add);

}

// ---- rail: agent sections ----

let chatSearchQuery = "";
let chatInboxFilter = "all";
let chatPins={};
let chatPinsRevision=-1;
const chatPinURL="/api/chat/state/inbox/pins";
function chatInboxKey(entry){return (entry.terminal?"terminal":entry.agent?"agent":"spirit")+":"+entry.agent+"/"+entry.session.id;}
function chatApplyPins(state){
  if(state.key!=="inbox"||state.slot!=="pins"||!Number.isSafeInteger(state.revision)||state.revision<0||state.revision<chatPinsRevision)return;
  chatPinsRevision=state.revision;chatPins=state.value?.pins||{};
}
async function chatLoadPins(){try{const r=await fetch(chatPinURL,{cache:"no-store"});if(r.ok)chatApplyPins(await r.json());}catch(e){}}
async function chatSetPinned(key,pinned){
  // Read/merge/CAS keeps another device's pins instead of overwriting its list.
  for(let attempt=0;attempt<3;attempt++){
    const r=await fetch(chatPinURL,{cache:"no-store"});if(!r.ok)throw Error("Could not load pinned chats.");
    const state=await r.json();if(state.key!=="inbox"||state.slot!=="pins"||!Number.isSafeInteger(state.revision))throw Error("Invalid pin state.");
    const pins={...state.value?.pins};if(pinned)pins[key]=true;else delete pins[key];
    const saved=await fetch(chatPinURL,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({revision:state.revision,value:{...state.value,pins}})});
    if(saved.status===409)continue;
    if(!saved.ok)throw Error("Pin change was not saved. Try again.");
    chatApplyPins(await saved.json());return;
  }
  throw Error("Pinned chats changed on another device. Try again.");
}
let chatWorkstreams={groups:{},members:{}},chatWorkstreamsRevision=-1,chatWorkstreamFilter="all";
const chatWorkstreamURL="/api/chat/state/inbox/workstreams";
function chatApplyWorkstreams(state){
  if(state.key!=="inbox"||state.slot!=="workstreams"||!Number.isSafeInteger(state.revision)||state.revision<chatWorkstreamsRevision)return;
  chatWorkstreamsRevision=state.revision;chatWorkstreams={groups:state.value?.groups||{},members:state.value?.members||{}};
}
async function chatLoadWorkstreams(){try{const r=await fetch(chatWorkstreamURL,{cache:"no-store"});if(r.ok)chatApplyWorkstreams(await r.json());}catch(e){}}
function chatWorkstreamMember(key){const id=chatWorkstreams.members[key];return chatWorkstreams.groups[id]?id:"";}
function chatRenderWorkstreamFilter(){
  const select=document.getElementById("chatWorkstreamFilter");if(!select||document.activeElement===select)return;
  select.replaceChildren();
  [["all","All workstreams"],["standalone","Standalone chats"],...Object.entries(chatWorkstreams.groups).sort((a,b)=>a[1].localeCompare(b[1]))].forEach(([id,label])=>{const o=el("option","",label);o.value=id;select.append(o);});
  if(!["all","standalone"].includes(chatWorkstreamFilter)&&!chatWorkstreams.groups[chatWorkstreamFilter])chatWorkstreamFilter="all";
  select.value=chatWorkstreamFilter;
}
async function chatSaveWorkstream(key,expected,id,name){
  const newID="ws-"+crypto.randomUUID();
  for(let attempt=0;attempt<3;attempt++){
    const r=await fetch(chatWorkstreamURL,{cache:"no-store"});if(!r.ok)throw Error("Could not load workstreams.");
    const state=await r.json();if(state.key!=="inbox"||state.slot!=="workstreams"||!Number.isSafeInteger(state.revision))throw Error("Invalid workstream state.");
    const groups={...state.value?.groups},members={...state.value?.members};
    if((members[key]||"")!==expected)throw Error("This chat’s workstream changed on another device. Close and reopen this choice.");
    let selected=id;
    if(name){selected=Object.keys(groups).find(k=>groups[k].toLowerCase()===name.toLowerCase())||newID;groups[selected]=groups[selected]||name;}
    if(selected&&!groups[selected])throw Error("That workstream is no longer available.");
    if(selected)members[key]=selected;else delete members[key];
    const saved=await fetch(chatWorkstreamURL,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({revision:state.revision,value:{groups,members}})});
    if(saved.status===409)continue;if(!saved.ok)throw Error("Workstream change was not saved.");
    chatApplyWorkstreams(await saved.json());return;
  }
  throw Error("Workstreams changed on another device. Try again.");
}
function chatChooseWorkstream(entry){
  const key=chatInboxKey(entry),expected=chatWorkstreams.members[key]||"";
  const dialog=el("dialog","chat-workstream-dialog"),form=el("form",""),title=el("h3","","Workstream");
  const select=document.createElement("select");select.setAttribute("aria-label","Choose workstream");
  [["","Standalone chat"],...Object.entries(chatWorkstreams.groups).sort((a,b)=>a[1].localeCompare(b[1])),["new","New workstream…"]].forEach(([id,label])=>{const o=el("option","",label);o.value=id;select.append(o);});select.value=expected;
  const name=document.createElement("input");name.placeholder="Workstream name";name.setAttribute("aria-label","Workstream name");name.maxLength=80;name.hidden=true;
  select.onchange=()=>{name.hidden=select.value!=="new";if(!name.hidden)name.focus();};
  const error=el("p","",""),actions=el("div","chat-workstream-actions"),cancel=el("button","sprt-quiet","Cancel"),save=el("button","","Save");error.setAttribute("role","alert");cancel.type="button";cancel.onclick=()=>dialog.close();save.type="submit";actions.append(cancel,save);
  form.append(title,el("p","",entry.session.title||entry.session.name||"Chat"),select,name,error,actions);
  form.onsubmit=async e=>{e.preventDefault();const creating=select.value==="new",label=name.value.trim();if(creating&&!label){error.textContent="Enter a workstream name.";name.focus();return;}save.disabled=true;try{await chatSaveWorkstream(key,expected,creating?"":select.value,creating?label:"");dialog.close();chatRenderWorkstreamFilter();renderChatInboxRows();}catch(e){error.textContent=e.message;}finally{save.disabled=false;}};
  dialog.append(form);dialog.addEventListener("close",()=>dialog.remove(),{once:true});document.body.append(dialog);dialog.showModal();select.focus();
}
function chatInboxEntries() {
  const entries = chatSessions.map(session => ({agent: "", session}));
  chatRoster.filter(a => !chatIsTerm(a.name)).forEach(agent => (chatAgentSessions[agent.name] || []).filter(session=>!chatHasNativeParent(session)).forEach(session => entries.push({agent: agent.name, session})));
  if (chatTermEnabled) Object.keys(chatTermKinds).forEach(agent => chatTermList(agent).filter(session=>!chatHasCanonicalParent(session)&&!chatHasNativeParent(session)).forEach(session => entries.push({agent, session, terminal: true})));
  const query = chatSearchQuery.trim().toLowerCase();
  return entries.filter(entry => (chatWorkstreamFilter==="all"||(chatWorkstreamFilter==="standalone"?!chatWorkstreamMember(chatInboxKey(entry)):chatWorkstreamMember(chatInboxKey(entry))===chatWorkstreamFilter)) && (chatInboxFilter === "all" || entry.agent === chatInboxFilter || (entry.terminal&&[...(chatAgentSessions[chatInboxFilter]||[]),...chatTermSessions.filter(s=>s.kind===chatInboxFilter)].some(s=>s.origin?.mode==="continue"&&s.origin?.backend==="terminal"&&s.origin?.id===entry.session.id&&s.origin?.agent===entry.agent)))
    && [entry.session.title, entry.session.name, entry.session.cwd, chatAgentLabel(entry.agent), entry.session.spirit].filter(Boolean).join(" ").toLowerCase().includes(query))
    .sort((a, b) => {
      const pinned=Number(chatPins[chatInboxKey(b)]===true)-Number(chatPins[chatInboxKey(a)]===true);if(pinned)return pinned;
      const time = entry => Date.parse(entry.session.updated || entry.session.lastUsed || entry.session.created || "") || 0;
      return time(b) - time(a);
    });
}
function renderChatInboxRows() {
  const host = document.getElementById("chatInboxRows");
  if (!host) return;
  host.replaceChildren();
  const entries = chatInboxEntries();
  if (!entries.length) { host.append(emptyRow(chatSearchQuery || chatWorkstreamFilter!=="all" || chatInboxFilter!=="all" ? "No matching conversations" : "No conversations yet")); return; }
  entries.forEach(entry => {
    const row = entry.terminal ? chatTermRow(entry.session) : chatRailRow(entry.session, entry.agent);
    row.classList.toggle("open", entry.agent === chatAgent && entry.session.id === chatOpenId);
    const meta = row.querySelector(".chat-rail-meta");
    if (meta) meta.prepend(el("span", "chat-inbox-agent", entry.terminal ? chatTermKinds[entry.agent] : entry.agent ? chatAgentLabel(entry.agent) : entry.session.spirit || "Spirits"));
    const key=chatInboxKey(entry),pinned=chatPins[key]===true;
    const pin=el("button","sprt-quiet chat-inbox-pin",pinned?"Unpin":"Pin");pin.setAttribute("aria-label",(pinned?"Unpin ":"Pin ")+(entry.session.title||entry.session.name||entry.session.id));pin.setAttribute("aria-pressed",String(pinned));
    pin.onclick=async e=>{e.stopPropagation();pin.disabled=true;try{await chatSetPinned(key,!pinned);renderChatInboxRows();}catch(error){showToast(error.message);}finally{pin.disabled=false;}};
    pin.onkeydown=e=>e.stopPropagation();
    (row.querySelector(".chat-rail-top")||row).append(pin);
    const group=chatWorkstreamMember(key),workstream=el("button","sprt-quiet chat-workstream-link",group?chatWorkstreams.groups[group]:"Workstream…");
    workstream.setAttribute("aria-label","Change workstream for "+(entry.session.title||entry.session.name||entry.session.id));
    workstream.onclick=e=>{e.stopPropagation();chatChooseWorkstream(entry);};workstream.onkeydown=e=>e.stopPropagation();(meta||row).append(workstream);
    row.onclick = () => { location.hash = entry.agent ? "#/chat/a/" + encodeURIComponent(entry.agent) + "/" + encodeURIComponent(entry.session.id) : "#/chat/" + encodeURIComponent(entry.session.id); };
    host.append(row);
  });
}
function renderChatRail() {
  const host = document.getElementById("chatRail");
  if (!host) return;
  if (host.contains(document.activeElement) && document.activeElement.classList.contains("inline-rename")) return;
  if (!host.querySelector("#chatInboxRows")) {
    host.replaceChildren();
    const controls = el("div", "chat-inbox-controls");
    const search = document.createElement("input");
    search.type = "search"; search.className = "chat-inbox-search";
    search.placeholder = "Search chats"; search.setAttribute("aria-label", "Search chats");
    search.value = chatSearchQuery;
    search.oninput = () => { chatSearchQuery = search.value; renderChatInboxRows(); };
    const select = document.createElement("select");
    select.className = "chat-inbox-filter"; select.setAttribute("aria-label", "Filter chats by agent");
    [["all", "All chats"], ...chatRoster.filter(a => !chatIsTerm(a.name)).map(a => [a.name, a.label]), ...(chatTermEnabled ? Object.entries(chatTermKinds) : []), ["", "Spirits"]].forEach(([value, label]) => {
      const option = el("option", "", label); option.value = value; select.append(option);
    });
    select.value = chatInboxFilter;
    select.onchange = () => { chatInboxFilter = select.value; renderChatInboxRows(); };
    const workstreams=document.createElement("select");workstreams.id="chatWorkstreamFilter";workstreams.className="chat-inbox-filter";workstreams.setAttribute("aria-label","Filter chats by workstream");workstreams.onchange=()=>{chatWorkstreamFilter=workstreams.value;renderChatInboxRows();};
    controls.append(search, select, workstreams);
    host.append(controls, el("div", "chat-inbox-rows"));
    host.lastChild.id = "chatInboxRows";
  }
  chatRenderWorkstreamFilter();
  renderChatInboxRows();
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
  const head = el("button", "chat-rail-section-head");
  head.setAttribute("aria-expanded", String(open));
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
  if (row.onclick) {
    row.tabIndex = 0;
    row.setAttribute("role", "link");
    row.addEventListener("keydown", event => {
      if (event.target === row && event.key === "Enter") { event.preventDefault(); row.click(); }
    });
  }
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
// under its threads, each opening that task's conversation on the CHAT task
// stage (the record itself stays one "open task ↗" away). Quiet when it holds
// none.
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
    row.onclick = () => { location.hash = chatTaskThreadHash(t.id); };
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
// links its source turn and assigns the agent. Discussion stays here.
function chatPromoteTurn(session, turnN) {
  const agent = chatAgent;
  if (!agent) return;
  const title = session.title || "";
  askText("Task from this conversation — " + chatAgentLabel(agent) + " takes it", title ? "the todo line · empty = “" + title + "”" : "the todo line…", async (t) => {
    try {
      const r = await postJSONOk(chatBaseFor(agent) + "/" + encodeURIComponent(session.id) + "/promote", { turn: turnN, text: (t || "").trim() });
      const where = "#/tasks/"+encodeURIComponent(r.created);
      chatConversationTasks.set("chat:"+agent+"/"+session.id,r.created);
      showToast("Task created" + (r.assigned ? " — " + (r.name || chatAgentLabel(agent)) + " holds it" : "") + " · open", () => { location.hash = where; }, "info");
      if (chatOpenId === session.id && chatAgent===agent) { refetchChatSession(session.id); loadChatSessions().then(renderChatRail); }
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
  const route=chatRouteVersion,agent=chatAgent;
  if(!chatOpenId)await chatPrepareLandingDraft(agent);
  if(route!==chatRouteVersion || agent!==chatAgent || els.chatView.hidden)return;
  const host = document.getElementById("chatTranscript");
  const main = document.querySelector(".chat-main");
  if (!host) return;
  chatTermLeave();
  if (main) main.classList.add("landing");
  host.innerHTML = "";
  host.append(el("div", "chat-greeting", chatGreeting()));

  if (chatIsTerm()) { renderChatTermLanding(host); return; }
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
  chatTermLeave();
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

// Keep failed thread loads distinct from a new conversation.
function renderChatEmpty(note) {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  host.replaceChildren(el("div", "chat-load-error", note || "Conversation unavailable"));
  const retry = el("button", "sprt-quiet", "Retry");
  retry.onclick = () => { retry.disabled = true; loadChatSession(chatOpenId); };
  host.append(retry);
  renderChatComposer({ busy: true });
}

async function loadChatSession(id) {
  if (chatIsTerm()) { loadChatTermSession(id); return; }
  let d;
  const base = chatBase(), agent = chatAgent;
  try {
    const res = await fetch(base + "/" + encodeURIComponent(id));
    if (id !== chatOpenId || base !== chatBase() || els.chatView.hidden) return;
    if (!res.ok) {
      renderChatEmpty(res.status === 404
        ? "that conversation is no longer here — it may have been deleted or archived"
        : "that conversation didn't load (" + res.status + ")");
      return;
    }
    d = await res.json();
  } catch (e) {
    if (id === chatOpenId && base === chatBase() && !els.chatView.hidden) renderChatEmpty("Conversation could not load. Check your connection and retry.");
    return;
  }
  if (id !== chatOpenId || base !== chatBase()) return; // navigated away mid-fetch
  if(d.sharedConversation?.route){location.hash=d.sharedConversation.route;return;}
  let originSelection=null;
  const originRef=d.session.origin?.artifacts?.[0];
  if(originRef && d.session.turns===0){
    originSelection={...originRef,task:d.session.origin.task,title:"Artifact",version:"?"};
    try{const r=await fetch("/api/artifacts/get?id="+encodeURIComponent(originRef.id));if(r.ok){const a=await r.json();originSelection.title=a.title||"Artifact";originSelection.version=a.revisions.find(v=>v.hash===originRef.revision)?.n||"?";}}catch(e){}
  }
  if (id !== chatOpenId || base !== chatBase() || els.chatView.hidden) return;
  await chatPrepareDraft(d.conversation,(agent||"spirits")+"/"+id,d.session.origin&&d.session.turns===0?{text:d.session.origin.prompt||"",files:[],task:d.session.origin.task||"",selection:originSelection}:null);
  await chatPrepareReadingPosition(d.conversation);
  if (id !== chatOpenId || base !== chatBase()) return;
  const main = document.querySelector(".chat-main");
  if (main) main.classList.remove("landing");
  chatRemember(chatAgent || "spirits", id);
  const taskContext=chatConversationTasks.get("chat:"+chatAgent+"/"+id);
  if(taskContext)d.session.task=taskContext;
  renderChatTranscript(d);
  renderChatComposer(d.session);
  if(chatPendingWorkspace?.selectionKey === "chat:"+chatAgent+"/"+id){
    const spec=chatPendingWorkspace;chatPendingWorkspace=null;chatOpenWorkingArtifact(spec);
  }
  ensureChatStream(d.session);
  ensureChatPoll(d.session, (d.queued || []).length);
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
  if(id!==chatOpenId)return;
  if(chatIsTerm()){if(chatTermOpen?.id===id)await chatTermRequestFinalTail(chatTermOpen);return;}
  try {
    const res = await fetch(chatBase() + "/" + encodeURIComponent(id));
    if (!res.ok || id !== chatOpenId) return;
    const d = await res.json();
    if(d.sharedConversation?.route){location.hash=d.sharedConversation.route;return;}
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
const chatReadingStates = new Map();
let chatReadingGestureUntil = 0;
async function chatPrepareReadingPosition(descriptor) {
  if (!descriptor?.key || typeof ChatDraftState === "undefined") return;
  if (chatReadingStates.has(descriptor.key)) { await chatReadingStates.get(descriptor.key).refresh(); return; }
  const saved = new ChatDraftState(descriptor.key, state => {
    // Reading position is a convenience bookmark, not an editable document.
    // A competing device's accepted bookmark wins without moving this viewport.
    if (state.conflict) state.resolve(true);
  }, "view");
  chatReadingStates.set(descriptor.key, saved);
  await saved.refresh();
}
function chatReadingAnchor(host) {
  if (host.scrollHeight - host.scrollTop - host.clientHeight <= 24) return {following:true};
  const top = host.getBoundingClientRect().top;
  for (const row of host.querySelectorAll("[data-chat-read-turn]")) {
    const rect = row.getBoundingClientRect();
    if (rect.bottom > top && rect.height > 0) return {following:false,turn:row.dataset.chatReadTurn,fraction:Math.max(0,Math.min(1,(top-rect.top)/rect.height))};
  }
  return null;
}
function chatSaveReadingPosition() {
  const host = document.getElementById("chatTranscript");
  if (!host || els.chatView.hidden) return;
  const saved = chatReadingStates.get(host.dataset.readKey);
  const value = chatReadingAnchor(host);
  if (saved && value) saved.set(value);
}
function chatRestoreReadingPosition(host, value) {
  if (!value || value.following !== false || typeof value.turn !== "string") return false;
  const row = [...host.querySelectorAll("[data-chat-read-turn]")].find(x => x.dataset.chatReadTurn === value.turn);
  if (!row) return false;
  const fraction = Number.isFinite(value.fraction) ? Math.max(0,Math.min(1,value.fraction)) : 0;
  host.scrollTop += row.getBoundingClientRect().top - host.getBoundingClientRect().top + row.getBoundingClientRect().height * fraction;
  chatLastY = host.scrollTop;
  chatStick = false;
  return true;
}
window.addEventListener("pagehide",()=>{for(const saved of chatReadingStates.values())if(saved.dirty)saved.flush();});
function bindChatScroll() {
  if (chatScrollBound) return;
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  chatScrollBound = true;
  const readingGesture = () => { chatReadingGestureUntil = Date.now() + 1500; };
  host.addEventListener("wheel", readingGesture, {passive:true});
  host.addEventListener("touchmove", readingGesture, {passive:true});
  host.addEventListener("pointerdown", readingGesture, {passive:true});
  host.addEventListener("keydown", e => { if (["ArrowUp","ArrowDown","PageUp","PageDown","Home","End"," "].includes(e.key)) readingGesture(); });
  host.addEventListener("scroll", () => {
    const y = host.scrollTop;
    if (y < chatLastY - 1) chatStick = false;
    if (host.scrollHeight - y - host.clientHeight <= 24) chatStick = true;
    chatLastY = y;
    if (Date.now() < chatReadingGestureUntil) chatSaveReadingPosition();
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
// into a preview chip. Spirit turns never carry one.
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
      const a = document.createElement("button");
      a.className = "chat-attach-chip";
      a.textContent = f.name;
      const href=chatFileHref(f.hash);
      a.onclick=()=>chatOpenAttachment(f,href);
      chips.append(a);
    });
    b.append(chips);
  }
  return b;
}

// chatHead — the thread head in the .sprt-head anatomy every detail head
// follows: title (inline-rename target: dblclick or ✎) · sub (who · model) ·
// meta (fmtWhen(updated) · charge) · trailing actions (☐ task ↗ · delete).
function chatSharedFilePicker(session,agent){
  const key=agent+"/"+session.id,base=chatAttachBase(),dialog=el("dialog","chat-workstream-dialog"),list=el("div","");
  dialog.append(el("h3","","Conversation files"),list);
  for(const file of session.sharedFiles||[]){
    const row=el("p",""),open=el("button","sprt-quiet",file.name),discuss=el("button","sprt-quiet","Discuss");
    open.onclick=()=>{dialog.close();if(chatDraftKey===key)chatOpenAttachment(file,base+"/"+file.hash);};
    discuss.onclick=()=>{if(chatDraftKey!==key){dialog.close();return;}if(!chatPendingFiles.some(f=>f.hash===file.hash))chatPendingFiles.push(file);chatSaveDraft();dialog.close();renderChatComposer(chatCurSession);};
    row.append(open,discuss);list.append(row);
  }
  const close=el("button","sprt-quiet","Close");close.onclick=()=>dialog.close();dialog.append(close);
  dialog.addEventListener("close",()=>dialog.remove(),{once:true});document.body.append(dialog);dialog.showModal();
}

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
  if (s.model) head.title = shortModel(s.model);
  else if (portal) sub.push(s.domain === "ooda" ? "ooda portal" : "aion portal");
  if (portal && s.busy) sub.push("✦ running");
  if(s.shared && (s.continuations||[]).length){
    const pick=document.createElement("select"); pick.className="pp-in chat-head-sub"; pick.setAttribute("aria-label","Next message recipient");
    const choices=[{value:"",label:"Choose agent…"},{value:"team",label:chatAgentLabel(agent)+" · team agent"},...(s.continuations||[]).map(v=>({value:v.id,label:chatAgentLabel(v.agent)+(v.model?" · "+shortModel(v.model):"")+" · "+v.id.slice(-6)}))];
    choices.forEach(c=>{const option=document.createElement("option");option.value=c.value;option.textContent=c.label;pick.append(option);});
    const key=agent+"/"+s.id, recipient=chatRecipients.get(key);
    pick.value=recipient?(recipient.backend==="terminal"?recipient.id:"team"):"";
    pick.onchange=()=>{const native=(s.continuations||[]).find(v=>v.id===pick.value);if(native)chatRecipients.set(key,{backend:"terminal",agent:native.agent,id:native.id,model:native.model});else if(pick.value==="team")chatRecipients.set(key,{agent});else chatRecipients.delete(key);chatCaptureSyncedDraft(key);renderChatComposer(s);};
    head.append(pick);
  }else if(chatRosterEntry(agent)?.durableSend){
    const recipient=chatRecipients.get(agent+"/"+s.id)||{agent,model:s.model||""};
    const model=recipient.model||chatRosterEntry(recipient.agent)?.model||"";
    const to=el("button","sprt-quiet chat-head-sub","To "+chatAgentLabel(recipient.agent)+(model?" · "+shortModel(model):""));
    to.title="Choose who receives your next message";to.onclick=()=>chatChooseRecipient(s);head.append(to);
    if(recipient.backend==="terminal"){const native=(s.continuations||[]).find(v=>v.id===recipient.id);if(native){head.append(terminalStateDot(native));if(native.cwd)head.append(chatChangesButton(native));}}
  }else head.append(el("span", "sprt-sub chat-head-sub", sub.filter(Boolean).join(" · ")));
  if(["kairos-private","zeck-private"].includes(agent)){
    const share=el("button","sprt-quiet",s.sharing?"Recover sharing":"Share…");
    share.onclick=()=>CHAT_SHARE.open({agent,id:s.id,title:s.title,onShared:conversation=>{location.hash=conversation.route;}});
    head.append(share);
  }
  if(s.shared && chatTermEnabled){
    const add=el("button","sprt-quiet","Add coding agent");add.onclick=()=>chatAddSharedTerminal(s,agent);head.append(add);
  }
  if(s.shared && s.sharedFiles?.length){
    const files=el("button","sprt-quiet","Files");
    files.onclick=()=>chatSharedFilePicker(s,agent);head.append(files);
  }
  // portal runs are metered in the agent's own ledger, not per thread
  const meta = [fmtWhen(s.updated || s.created)];
  if (!portal) meta.push("$" + (s.spentUsd || 0).toFixed(4) + (s.ceilingUsd ? " / $" + s.ceilingUsd.toFixed(2) : ""));
  const info = el("details", "chat-details");
  info.append(el("summary", "", "Details"), el("div", "chat-head-meta", meta.filter(Boolean).join(" · ")));
  const acts = el("span", "chat-head-acts");
  const ren = el("button", "sprt-quiet", "✎");
  ren.title = "rename";
  if (!agent && s.status === "thinking") { ren.disabled = true; ren.title = "rename after the turn finishes"; }
  ren.onclick = () => chatRename(title, s, agent);
  acts.append(ren);
  if(chatRosterEntry(agent)?.durableSend){
    const related=el("button","sprt-quiet","Start related chat");related.onclick=()=>chatStartRelated(s);acts.append(related);
  }
  for(const item of s.related||[]){const link=el("a","sprt-quiet",(item.relation==="origin"?"From: ":item.relation==="continuation"?"Coding session: ":"Related: ")+item.title);link.href=item.route;acts.append(link);}
  // the task this conversation became (§3.4f) — into its conversation, here
  if (s.task) {
    const plan = el("button", "sprt-quiet", "Plan");
    plan.onclick = () => chatOpenWorkingArtifact({plan:true,task:s.task,selectionKey:"chat:"+agent+"/"+s.id});
    head.append(plan);
    const task = el("button", "sprt-quiet chat-head-task", "Task ↗");
    task.title = "open the associated task";
    task.onclick = () => { location.hash = "#/tasks/"+encodeURIComponent(s.task); };
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
  info.append(acts);
  head.append(info);
  return head;
}

// chatRepaintHead swaps the open head in place (after a rename) — the
// transcript and its scroll position stay.
function chatRepaintHead() {
  const cur = document.querySelector("#chatThreadHeader .chat-head");
  if (cur && chatCurSession && !chatHeadRenaming(cur)) chatMountHeader(chatHead(chatCurSession));
}

// chatHeadRenaming — the head's title is mid-rename (the inline input has
// focus): a poll-driven repaint would pull the input out from under the
// typing (the rail's activeElement guard, for the head)
function chatHeadRenaming(head) {
  const a = document.activeElement;
  return !!(a && a.classList.contains("inline-rename") && head.contains(a));
}

// chatTurnBlocks — one block shape for every backend: a spirit/agent turn's
// text is split by the `### Step` grammar (say → markdown, other casts → a
// step chip whose detail is the `- result:`/`- rationale:` line); a claude/
// codex turn arrives pre-projected from the transcript endpoint (say / think
// / step with input + paired result).
function chatTurnBlocks(t) {
  if (t.blocks) return t.blocks;
  const steps = parseChatSteps(t.text || "");
  if (!steps.length) return t.text ? [{ t: "say", text: t.text, plain: true }] : [];
  return steps.map((st) => {
    if (st.cast === "say") return { t: "say", text: st.body || "" };
    const mres = (st.body || "").match(/^- (?:result|rationale): (.*)$/m);
    return { t: "step", cast: st.cast, input: mres ? mres[1] : "" };
  });
}

// chatBlockEl paints one block: say (markdown), think (collapsed disclosure,
// the live layer's idiom), step (run-trace chip; a paired result opens
// under it as a disclosure).
function chatBlockEl(b) {
  if (b.t === "say") {
    const say = el("div", "chat-say");
    if (b.plain) { say.textContent = b.text || ""; return say; }
    try { say.append(renderMarkdown(b.text || "", "", { readOnly: true })); }
    catch (e) { say.textContent = b.text || ""; }
    return say;
  }
  if (b.t === "think") {
    const det = document.createElement("details");
    det.className = "chat-thinking-block";
    const sum = document.createElement("summary");
    sum.textContent = "▸ thinking";
    det.append(sum, el("div", "chat-thinking-text", b.text || ""));
    return det;
  }
  const ln = el("div", "run-trace-step chat-step");
  ln.append(el("span", "run-trace-cast", b.cast || "step"));
  ln.append(el("span", "run-trace-detail", b.input || ""));
  if (!b.result) return ln;
  const det = document.createElement("details");
  det.className = "chat-step-details";
  const sum = document.createElement("summary");
  sum.append(ln);
  if (b.error) ln.append(el("span", "chat-step-err", "· error"));
  det.append(sum, el("pre", "chat-step-result" + (b.error ? " err" : ""), b.result));
  return det;
}

// chatPaintTurns — THE turn renderer, shared by every section: user turns,
// system notes, agent turns (blocks + a foot with the time, the charge and,
// when ctx.promote is given, the "→ task" act).
function chatProposalBlocks(t,proposal){return chatTurnBlocks(t).map(b=>proposal&&b.t==="say"?{...b,text:(b.text||"").replace(/```(?:manifest-plan-revision|json)[ \t]*\r?\n[\s\S]*?\r?\n```/g,"").trim()}:b).filter(b=>b.t!=="say"||b.text);}
function chatPlanReviewButton(proposal){
 const review=el("button","sprt-quiet","Review proposed plan revision");
 review.onclick=()=>chatOpenWorkingArtifact({plan:true,task:proposal.task,revision:proposal.baseRevision,proposal,selectionKey:"chat:"+chatAgent+"/"+chatOpenId});return review;
}
function chatPaintTurns(host, turns, ctx) {
  turns.forEach((t) => {
    if (t.who === "user") {
      const row=chatUserTurn(t.text);
      row.dataset.chatReadTurn=String(t.n);
      const receipt=t.delivery||(t.submission?{context:{recipient:{agent:t.native.agent,model:t.native.model},task:t.submission.task,artifacts:t.submission.artifacts},historyOmitted:t.submission.historyOmitted}:ctx?.deliveries?.find(d=>d.userTurn===t.n));
      if(receipt?.context?.recipient){
        const target=receipt.context.recipient;
        row.append(el("div","chat-context-attribution","To "+chatAgentLabel(target.agent)+(target.model?" · "+shortModel(target.model):"")+(receipt.historyOmitted?" · "+receipt.historyOmitted+" earlier turns omitted":"")));
      }
      for(const ref of receipt?.context?.artifacts||[]){
        const open=el("button","sprt-quiet","Referenced plan / file");
        open.title="Open the exact version discussed in this message";
        open.onclick=()=>chatOpenWorkingArtifact({id:ref.id,revision:ref.revision,task:receipt.context.task,selectionKey:"chat:"+chatAgent+"/"+chatOpenId});
        row.append(open);
      }
      for(const file of t.submission?.files||[]){
        const open=el("button","chat-attach-chip",file.name),href=chatFileHref(file.hash);
        open.title="Open the exact file sent with this message";
        open.onclick=()=>chatOpenAttachment(file,href);row.append(open);
      }
      host.append(row);
      return;
    }
    if (t.who === "system") {
      const b = el("div", "chat-turn chat-system");
      b.dataset.chatReadTurn=String(t.n);
      b.textContent = t.text;
      host.append(b);
      return;
    }
    const wrap = el("div", "chat-turn chat-spirit");
    wrap.dataset.chatReadTurn=String(t.n);
    const planRevision=t.planRevision||ctx?.planRevisions?.find(p=>p.replyTurn===t.n);
    chatProposalBlocks(t,planRevision).forEach(b=>wrap.append(chatBlockEl(b)));
    if(planRevision)wrap.append(chatPlanReviewButton(planRevision));
    if (ctx && ctx.operations) ctx.operations.filter(item => Number(item.record.turn) + 1 === t.n).forEach(item => wrap.append(manifestOperationCard(item)));
    const foot = el("div", "chat-turn-foot");
    foot.append(el("span","chat-turn-author",chatAgentLabel(t.who.replace(/^agent:/,""))));
    // when the turn landed — a conversation with no times reads as stalled
    // while Alfred's turn takes minutes
    if (t.ts) foot.append(el("span", "chat-turn-when", fmtWhen(t.ts)));
    if (t.usd) foot.append(el("span", "chat-turn-usd", "$" + t.usd));
    if(t.native){const native=el("a","chat-turn-act","Native chat ↗");native.href=t.native.route;foot.append(native);}
    if (ctx && ctx.promote && !t.native) {
      const promote = el("button", "chat-turn-act", "→ task");
      promote.title = "make a task from this conversation (up to this turn) — " + ctx.who + " takes it";
      promote.onclick = () => ctx.promote(t);
      foot.append(promote);
    }
    if (foot.childElementCount) wrap.append(foot);
    host.append(wrap);
  });
}

function chatPaintCodingResults(host,results) {
  const sourceAgent=chatAgent,sourceID=chatOpenId,route=chatRouteVersion;
  for(const result of results||[]) {
    const row=el("div","chat-turn chat-spirit");
    row.dataset.chatReadTurn="run:"+result.agent+":"+result.id;
    const heading=el("div","chat-turn-foot");
    heading.append(el("span","chat-turn-author",chatAgentLabel(result.agent)+" · "+(result.outcome==="completed"?"Coding result":"Coding work needs attention")));
    const task=el("a","sprt-quiet",result.task);task.href=chatTaskThreadHash(result.task);heading.append(task);
    row.append(heading);
    const open=el("button","sprt-quiet","Open result / discuss");
    open.onclick=async()=>{
      open.disabled=true;
      try {
        const ref=await postJSONOk(chatBaseFor(sourceAgent)+"/"+encodeURIComponent(sourceID)+"/coding-result",{agent:result.agent,run:result.id,hash:result.hash});
        if(route!==chatRouteVersion || chatAgent!==sourceAgent || chatOpenId!==sourceID)return;
        chatOpenWorkingArtifact({...ref,selectionKey:"chat:"+sourceAgent+"/"+sourceID});
      }catch(e){showToast(e.message||"Could not open this result.");}
      finally{open.disabled=false;}
    };
    row.append(open);
    try {row.append(renderMarkdown(result.body||"","",{readOnly:true}));}catch(e){row.append(el("pre","",result.body||""));}
    host.append(row);
  }
}

function renderChatTranscript(d) {
  const host = document.getElementById("chatTranscript");
  if (!host) return;
  const readKey = d.conversation?.key || "";
  const changedConversation = host.dataset.readKey !== readKey;
  const keepPosition = !changedConversation && chatCurSession && chatCurSession.id === d.session.id && !chatStick;
  const previousY = host.scrollTop;
  bindChatScroll();
  chatTermLeave();
  host.innerHTML = "";
  host.dataset.readKey = readKey;
  if (changedConversation) chatReadingGestureUntil = 0;
  const s = d.session;
  const activeTask=chatConversationTasks.get("chat:"+chatAgent+"/"+s.id);
  if(activeTask)s.task=activeTask;
  s.related=d.related||[];s.handoffBody=d.body||"";s.continuations=d.continuations||[];s.sharedFiles=d.sharedFiles||[];
  chatCurSession = s;
  chatLastUpdated = chatTranscriptSignature(d);
  const who = s.spirit || (s.agent ? chatAgentLabel(s.agent) : "");
  const portal = chatIsPortal();
  chatMountHeader(chatHead(s));

  // → task (§3.4f): every agent turn in an agent section can become work
  chatPaintTurns(host, d.timeline||parseChatTurns(d.body || ""), chatAgent ? { who, deliveries:s.deliveries||[], planRevisions:d.planRevisions||[],operations: d.operations || [], promote: (t) => chatPromoteTurn(s, t.n) } : null);

  const turnNumbers = new Set(parseChatTurns(d.body || "").filter(t => t.who !== "user" && t.who !== "system").map(t => t.n));
  (d.operations || []).filter(item => !turnNumbers.has(Number(item.record.turn) + 1)).forEach(item => host.append(manifestOperationCard(item)));
  (d.sharedOperations || []).forEach(item => host.append(manifestOperationCard(item)));
  appendTaskApprovals(host, d);
  chatPaintCodingResults(host,d.codingResults);
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
  chatStick = !keepPosition;
  if (keepPosition) host.scrollTop = previousY;
  chatPin();
  if (changedConversation) chatRestoreReadingPosition(host, chatReadingStates.get(readKey)?.value);
}

// ---- composer ----
// One composer for every section. Attachments (agent sections only) upload
// first and ride the send as `files`: Hermes sections use the todo-thread file
// store, portal sections the agent's own artifact domain (chat_attach.go).
// Both land on the user turn as [file::] tokens. Portal sections add the
// ask|propose ritual pill and the @-mention typeahead (persona intents).

let chatPendingFiles = [];
const chatUploads = new Map();
function chatUploadComposerActive(key) {
  return chatDraftKey===key && !chatTaskID && !els.chatView.hidden;
}

// Upload destinations and draft ownership are fixed when the picker submits.
// Navigation must never retarget an in-flight private/team attachment.
function chatStoreUploadedFile(key,file) {
  if(chatUploadComposerActive(key)) {
    chatPendingFiles.push(file);
    chatSaveDraft();
    renderChatComposer(chatCurSession);
    return;
  }
  const state=chatSyncedDrafts.get(key);
  const draft=state?.value || chatDrafts.get(key) || {text:"",files:[]};
  const next={...draft,files:[...(draft.files||[]),file]};
  chatDrafts.set(key,{text:next.text||"",files:next.files});
  if(state)state.set(next);
}

async function chatUploadFiles(key,url,files) {
  chatUploads.set(key,(chatUploads.get(key)||0)+1);
  if(chatUploadComposerActive(key))renderChatComposer(chatCurSession);
  try {
    for(const f of files) {
      try {
        const res=await fetch(url+"&name="+encodeURIComponent(f.name),{method:"POST",body:f});
        if(!res.ok)throw new Error((await res.text()).slice(0,120));
        const d=await res.json();
        chatStoreUploadedFile(key,{hash:d.file.hash,name:d.file.name,size:d.file.size});
      } catch(e) {showToast("Upload failed — "+(e.message||"error"));}
    }
  } finally {
    const remaining=(chatUploads.get(key)||1)-1;
    if(remaining)chatUploads.set(key,remaining);else chatUploads.delete(key);
    if(chatUploadComposerActive(key))renderChatComposer(chatCurSession);
  }
}

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
  if(session?.sharing && session.sharing.state!=="shared"){host.replaceChildren(el("p","chat-load-error","Sharing is awaiting recovery. Use Recover sharing above to finish, then continue in the team conversation."));return;}
  const draftKey = (chatAgent || "spirits") + "/" + (chatOpenId || "new");
  const nativeRecipient = () => chatRecipients.get(draftKey)?.backend === "terminal";
  const syncAttach = () => {
    const btn = host.querySelector(".chat-attach");
    if (btn) btn.hidden = (nativeRecipient()&&!session?.shared) || !chatAgent || (chatIsTerm()&&chatRecipients.get(draftKey)?.backend!=="hermes");
    const rit = host.querySelector(".chat-ritual");
    if (rit) {
      rit.hidden = !chatIsPortal() || nativeRecipient();
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
        x.onclick = () => { chatPendingFiles.splice(i, 1); syncAttach(); chatSaveDraft(); };
        chip.append(x);
        chips.append(chip);
      });
    }
  };
  const placeholder = () => {
    const a = chatRosterEntry(chatAgent);
    if (chatIsTerm()) return chatRecipients.get(draftKey)?.backend==="hermes"?"Message the selected agent…":chatTermPlaceholder();
    if (chatIsPortal()) {
      if(nativeRecipient())return "Message "+chatAgentLabel(chatRecipients.get(draftKey).agent)+"…";
      if(session?.shared && (session.continuations||[]).length && !chatRecipients.get(draftKey))return "Choose an agent above, then write your message…";
      if (session && session.busy) return "✦ " + (a ? a.label : chatAgent) + " is running — one order at a time";
      if (session && session.status === "thinking") return "✦ waiting on " + (a ? a.label : chatAgent) + "…";
      return "Message… · @ to tag an intent";
    }
    return session && session.status === "thinking" ? "✦ thinking — messages queue…" : "Message…";
  };
  // a portal agent takes one order at a time: a send while it runs 409s, so
  // the button says so instead (the placeholder already says why)
  // a claude/codex send may resume the process and wait for its prompt (~10 s):
  // one in flight at a time
  const uploading=!!chatUploads.get(draftKey);
  const busy = uploading || chatSending || !!(session && session.busy && !nativeRecipient()) || (chatIsTerm() && chatTermSending);
  if (host.dataset.built) {
    const ta = host.querySelector("textarea");
    const send = host.querySelector(".chat-send");
    if (ta) ta.placeholder = placeholder();
    if (send) { send.disabled = busy; send.textContent = uploading ? "…" : chatIsTerm() ? "↵" : "↑"; send.title=uploading?"Uploading attachments…":"send · Enter (Shift+Enter for a new line)"; } // a prompt line ends in enter
    syncAttach();
    chatRenderDeliveryNotice(host,draftKey);
    chatRenderArtifactContext(session?.task,"chat:"+draftKey);
    chatRenderDraftNotice(host,draftKey);
    return;
  }
  host.dataset.built = "1";
  const ta = document.createElement("textarea");
  ta.className = "chat-input";
  ta.rows = 1;
  chatDraftKey = draftKey;
  const draft = chatDrafts.get(draftKey);
  ta.value = draft ? draft.text : "";
  chatPendingFiles = draft ? draft.files.slice() : [];
  ta.placeholder = placeholder();
  ta.setAttribute("aria-label", "Message");
  // auto-grow with content (target feel): reset then snap to scrollHeight,
  // clamped so a long paste scrolls inside instead of shoving the transcript.
  const grow = () => { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, window.innerHeight * 0.4) + "px"; };
  ta.addEventListener("input", grow);
  ta.addEventListener("input", chatSaveDraft);
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
    chatSaveDraft();
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
    const files=[...fi.files];
    const url=chatIsPortal()
      ? chatAttachBase()+"?"+(chatOpenId?"thread="+encodeURIComponent(chatOpenId):"")
      : "/api/tasks/thread/file?id=agentchat";
    fi.value = "";
    await chatUploadFiles(draftKey,url,files);
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
  const send = el("button", "chat-send", uploading ? "…" : chatIsTerm() ? "↵" : "↑");
  send.title = uploading ? "Uploading attachments…" : "send · Enter (Shift+Enter for a new line)";
  send.disabled = busy;
  const submit = async () => {
    const text = ta.value.trim();
    const files = chatPendingFiles.slice();
    if (!text && !files.length) return;
    if (send.disabled || chatSending || chatUploads.get(draftKey)) return;
    chatCaptureSyncedDraft(draftKey);
    const draftState=chatSyncedDrafts.get(draftKey),sentDraft=draftState?.value;
    if(draftState?.conflict){showToast("Resolve the draft conflict before sending.");return;}
    const sendAgent=chatAgent, sendSession=chatOpenId, sendRoute=chatRouteVersion;
    const durable=!!chatRosterEntry(sendAgent)?.durableSend;
    chatSending = true;
    send.disabled = true;
    // Keep the submitted draft visible until acceptance. Navigation or a lost
    // acknowledgement must not save an empty replacement on another device.
    const acceptedDraft=()=>{
      if(sentDraft)draftState.clearSent(sentDraft);
      const current=chatDrafts.get(draftKey);
      if(current?.text.trim()===text && chatStateEqual(current.files,files))chatDrafts.delete(draftKey);
      if(chatDraftKey===draftKey && ta.value.trim()===text && chatStateEqual(chatPendingFiles,files)){
        ta.value="";chatPendingFiles=[];grow();mention.hidden=true;syncAttach();
      }
    };
    const payload = chatIsPortal() ? { text, files, ritual: chatRitual } : { text, files };
    const selected=chatArtifactSelections.get("chat:"+draftKey);
    const chosenRecipient=chatRecipients.get(draftKey);
    if(session?.shared && (session.continuations||[]).length && !chosenRecipient){showToast("Choose the agent for this message above the conversation.");chatSending=false;renderChatComposer(chatCurSession);return;}
    if(chatIsTerm()&&chosenRecipient?.backend==="hermes"){
      try{
        const target=chosenRecipient;
        const url=chatBaseFor(target.agent)+"/"+encodeURIComponent(target.id)+"/messages";
        await chatDeliverRemembered(chatRememberDelivery(draftKey,target.agent,url,{text,files,recipient:{agent:target.agent,model:target.model},task:session?.shared?"":selected?.task||session?.task||"",artifacts:selected?[{id:selected.id,revision:selected.revision}]:[]}));
        acceptedDraft();
        if(sendRoute===chatRouteVersion&&chatTermOpen)await chatTermRequestFinalTail(chatTermOpen);
      }catch(e){showToast(e.message||"Send not confirmed. Your draft is retained.");}
      finally{chatSending=false;renderChatComposer(chatCurSession);}
      return;
    }
    if(chosenRecipient?.backend==="terminal"){
      try{
        if(files.length&&!session?.shared)throw new Error("File uploads are not supported by this coding continuation yet. Remove the attachment or choose a planning agent.");
        const url=chatTermBase(chosenRecipient.id)+"/input";
        const item=chatRememberDelivery(draftKey,chosenRecipient.agent,url,{text,...(files.length?{files:files.map(f=>f.hash)}:{}),conversationAgent:sendAgent,conversationId:sendSession,task:session?.shared?"":selected?.task||session?.task||"",artifacts:selected?[{id:selected.id,revision:selected.revision}]:[]});
        await chatDeliverRemembered(item);acceptedDraft();
        if(sendRoute===chatRouteVersion)await refetchChatSession(sendSession);
      }catch(e){showToast(e.message||"Send not confirmed. Your draft is retained.");}
      finally{chatSending=false;renderChatComposer(chatCurSession);}
      return;
    }
    if(durable){
      payload.recipient=chatRecipients.get(draftKey)||{agent:sendAgent,model:session?.model||""};
      payload.task=selected?.task||chatConversationTasks.get("chat:"+draftKey)||session?.task||"";
      if(selected)payload.artifacts=[{id:selected.id,revision:selected.revision}];
    }
    if (chatIsTerm()) {
      // claude/codex: runtime input (explicitly resuming an ended session first); a
      // landing send creates the registry row, then delivers
      try { if (!await chatTermSend(text,selected?{task:selected.task,artifacts:[{id:selected.id,revision:selected.revision}]}:{})) {
        showToast("Send not confirmed. Your draft is retained.");
      }else acceptedDraft(); }
      finally { chatSending = false; renderChatComposer(chatCurSession); }
      return;
    }
    let remembered=null;
    try {
      const endpoint=chatBaseFor(sendAgent)+(sendSession?"/"+encodeURIComponent(sendSession)+"/messages":"");
      if(durable)remembered=chatRememberDelivery(draftKey,sendAgent,endpoint,payload);
      if (sendSession) {
        if(remembered)await chatDeliverRemembered(remembered);
        else await postJSONOk(endpoint, sendAgent ? payload : { text });
        acceptedDraft();
      } else if (sendAgent) {
        // Lazy creation uses the same request ID when its response is lost.
        const r = remembered ? await chatDeliverRemembered(remembered) : await postJSONOk(endpoint, payload);
        acceptedDraft();
        if(sendRoute!==chatRouteVersion)return;
        chatOpenId = r.id;
        chatLanding = false;
        location.hash = chatHash(r.id);
        return;
      } else {
        // lazy create (cmd-ctr model): the landing's chosen spirit/model
        const r = await postJSONOk("/api/chat/sessions", {
          spirit: chatPendingSpirit || "concierge", model: chatPendingModel || "", text,
        });
        acceptedDraft();
        if(sendRoute!==chatRouteVersion)return;
        chatOpenId = r.id;
        chatLanding = false;
        location.hash = chatHash(r.id);
        return;
      }
      if(sendRoute===chatRouteVersion){loadChatSession(sendSession);loadChatSessions().then(renderChatRail);}
    } catch (e) {
      if(remembered && !e.rejected){
        showToast("Delivery is unconfirmed. Check status or retry the saved send.",null,"info");
        if(chatDraftKey===draftKey)chatRenderDeliveryNotice(host,draftKey);
      } else {
        if(remembered)await chatForgetDelivery(remembered);
        showToast("Send failed — " + (e.message || "error"));
        // The submitted text remains in the composer; preserve any newer edits.
      }
    }
    finally { chatSending = false; renderChatComposer(chatCurSession); }
  };
  ta.addEventListener("keydown", (e) => {
    if (e.isComposing || e.keyCode === 229) return;
    if (!mention.hidden && (e.key === "Escape")) { e.preventDefault(); mention.hidden = true; return; }
    if (!mention.hidden && (e.key === "Tab" || e.key === "Enter")) {
      const first = mention.querySelector(".chat-mention-tok");
      if (first) { e.preventDefault(); pickMention(first.textContent); return; }
    }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  });
  send.onclick = submit;
  host.append(chips, mention, ta, fi, attach, ritual, send);
  chatRenderDeliveryNotice(host,draftKey);
  chatRenderArtifactContext(session?.task,"chat:"+draftKey);
  chatRenderDraftNotice(host,draftKey);
  grow();
  syncAttach();
}

// ---- live poll (file-derived; stops when idle; every backend) ----
// Portal sections poll slower (4s, the portals' own cadence): every read
// there runs the server's chatSweep over the agent's run reports.

function chatTranscriptSignature(d) {
  return JSON.stringify((d.session.deliveries || []).map(x=>[x.id,x.state,x.userTurn,x.replyTurn])) + "|" + d.session.updated + "|" + d.session.status + "|" + (d.queued || []).length + "|" + JSON.stringify((d.operations || []).map(x => [x.record.operationId, x.record.status, x.record.result])) + "|" + JSON.stringify(d.session.sharing || null) + "|" + JSON.stringify(d.sharedOperations || []) + "|" + JSON.stringify(d.proposals || []) + "|" + JSON.stringify(d.codingResults || [])+"|"+JSON.stringify(d.continuations||[])+"|"+JSON.stringify(d.sharedFiles||[]);
}
function ensureChatPoll(session, queued) {
  const active = session && (session.status === "thinking" || session.shared || queued > 0 || (chatAgent && !chatIsPortal()));
  if (!active) { if (chatPollTimer) { clearInterval(chatPollTimer); chatPollTimer = null; } return; }
  if (chatPollTimer) return;
  const every = chatIsPortal() ? 4000 : 1500;
  chatPollTimer = setInterval(async () => {
    if (els.chatView.hidden || !chatOpenId) {
      clearInterval(chatPollTimer); chatPollTimer = null; return;
    }
    const id = chatOpenId, base = chatBase(), routeVersion = chatRouteVersion;
    let d;
    try {
      const res = await fetch(base + "/" + encodeURIComponent(id));
      if (!res.ok) return;
      d = await res.json();
    } catch (e) { return; }
    if (routeVersion !== chatRouteVersion || id !== chatOpenId || base !== chatBase() || els.chatView.hidden) return;
    if(d.sharedConversation?.route){location.hash=d.sharedConversation.route;return;}
    const sig = chatTranscriptSignature(d);
    if (sig !== chatLastUpdated) {
      renderChatTranscript(d);
      renderChatComposer(d.session);
      loadChatSessions().then(renderChatRail);
    }
    if (!d.session.shared && d.session.status !== "thinking" && !(d.queued || []).length && !(chatAgent && !chatIsPortal())) {
      clearInterval(chatPollTimer); chatPollTimer = null;
    }
  }, every);
}

function loadChat() { showChat(location.hash); }

// ---- CLAUDE CODE · CODEX sections ----
// Operational associations supply conversation identity and cwd. CLI JSONL is
// the transcript; the shared terminal SSE supplies advisory runtime state.
// The 1.5 s file tail continues after process stop to ingest final records.

const chatTermKinds = { claude: "claude code", codex: "codex" };
function chatIsTerm(name) {
  const n = name === undefined ? chatAgent : name;
  return !!n && Object.prototype.hasOwnProperty.call(chatTermKinds, n);
}
function chatTermBase(id) { return "/api/terminal/session/" + encodeURIComponent(id); }

let chatTermSessions = [];   // last /api/terminal/sessions (every kind)
let chatTermEnabled = true;  // the terminal feature is on for this server
let chatTermPayload = "";    // change detection (the termLastPayload idiom)
// One browser stream serves Chat and task badges. Agent state is advisory;
// transcript JSONL and board run reports retain their own rendering paths.
let terminalEvents = null;
let terminalEventsConnected = false;
let terminalStates = new Map();
let terminalRunStates = new Map();
let terminalMetadataRefresh = null;
let chatTermHistOpen = {};   // kind → the folded history is unfolded
let chatTermSending = false; // one send in flight (a relaunch waits for the prompt)
const chatTermRecent = 10;   // rows shown before "history" folds the rest (§7 Q2)
const chatTermPlaceholderRe = /^(sh|cc|cdx)\d+$/; // the registry's minted names

// the open thread: the registry row, the projected turns (tailed by byte
// offset), the transcript's own title/cost, liveness, the screen tail
let chatTermOpen = null;
let chatTermFast = null;

// chatTermList — the section's rows: this box only (the transcript and the runtime are
// local; remote rows stay the Terminal tab's).
function chatTermList(kind) { return chatTermSessions.filter((s) => s.kind === kind && !s.device); }
function chatHasCanonicalParent(session){const o=session.origin;return o?.mode==="continue"&&!o.backend&&(chatAgentSessions[o.agent]||[]).some(s=>s.id===o.id);}
function chatHasNativeParent(session){const o=session.origin;return o?.mode==="continue"&&o.backend==="terminal"&&chatTermSessions.some(s=>s.id===o.id&&s.kind===o.agent&&!s.device);}

// chatTermOrder — live first, then most recently used (§7 Q2).
function chatTermOrder(list) {
  return list.slice().sort((a, b) => {
    if (!!a.live !== !!b.live) return a.live ? -1 : 1;
    return (b.lastUsed || "").localeCompare(a.lastUsed || "");
  });
}

function chatTermFind(id) { return chatTermSessions.find((s) => s.id === id) || null; }

// Metadata loads on navigation, explicit actions, and new event associations.
// Herdr observations always come from the shared event stream.
async function loadChatTermSessions(quiet) {
  ensureTerminalEvents();
  let d;
  try { d = await (await fetch("/api/terminal/sessions")).json(); } catch (e) { return false; }
  chatTermSessions = (d.sessions || []).map(chatTermApplyState);
  chatTermEnabled = d.enabled !== false;
  const payload = JSON.stringify(chatTermSessions) + "|" + chatTermEnabled;
  const changed = payload !== chatTermPayload;
  chatTermPayload = payload;
  if (quiet && changed) { renderChatRail(); chatTermSyncOpen(); }
  return changed;
}

function ensureTerminalEvents() {
  if (terminalEvents || typeof EventSource === "undefined") return;
  const source = new EventSource("/api/terminal/events");
  terminalEvents = source;
  source.addEventListener("state", (event) => {
    if (terminalEvents !== source) return;
    let data;
    try { data = JSON.parse(event.data); } catch (e) { terminalEventsUnavailable(); return; }
    terminalEventsConnected = data.connected === true;
    const next = new Map(), runs = new Map();
    (data.sessions || []).forEach((ob) => {
      if (!ob.manifestId) return;
      if (!terminalEventsConnected) ob = Object.assign({}, ob, { agentState: "unknown", connectivity: "unavailable", process: "unknown" });
      next.set(ob.manifestId, ob);
      if (ob.runId) runs.set(ob.runId, ob);
    });
    terminalStates = next;
    terminalRunStates = runs;
    terminalStateRepaint();
    // Events discover new associations; metadata supplies names/cwd only.
    if (([...next.keys()].some((id) => !chatTermFind(id)) || chatTermSessions.some((se) => se.backend === "herdr" && !next.has(se.id))) && !terminalMetadataRefresh) {
      terminalMetadataRefresh = loadChatTermSessions(true).finally(() => { terminalMetadataRefresh = null; });
    }
  });
  source.onerror = () => { if (terminalEvents === source) terminalEventsUnavailable(); };
}

function terminalEventsUnavailable() {
  terminalEventsConnected = false;
  terminalStates = new Map([...terminalStates].map(([id, ob]) => [id, Object.assign({}, ob, { agentState: "unknown", connectivity: "unavailable", process: "unknown" })]));
  terminalRunStates = new Map([...terminalRunStates].map(([id, ob]) => [id, Object.assign({}, ob, { agentState: "unknown", connectivity: "unavailable", process: "unknown" })]));
  terminalStateRepaint();
}

function terminalStateLabel(ob) {
  if (ob?.process === "not-started") return "draft";
  if (!ob || ob.connectivity !== "connected") return "unavailable";
  if (ob.process === "stopped") return "stopped";
  return ["working", "blocked", "idle", "done"].includes(ob.agentState) ? ob.agentState : "unknown";
}
function terminalStateDot(ob) {
  const label = terminalStateLabel(ob);
  const title = "agent " + label + (ob && ob.observedAt ? " · " + fmtWhen(ob.observedAt) : "");
  const dot = statusDot(label === "working" || label === "blocked", title);
  dot.classList.add("terminal-state-dot");
  dot.dataset.agentState = label;
  dot.setAttribute("aria-label", title);
  return dot;
}
function chatTermApplyState(se) {
  if (se.backend !== "herdr") return se;
  const ob = terminalStates.get(se.id);
  if (!ob && se.process === "not-started") return se;
  return Object.assign({}, se, {
    live: !!(ob && ob.connectivity === "connected" && ob.process === "running"),
    agentState: ob ? ob.agentState : "unknown", connectivity: ob ? ob.connectivity : "unavailable",
    process: ob ? ob.process : "unknown", observedAt: ob ? ob.observedAt : "",
  });
}
function terminalRunBadge(dg) {
  if (!dg || !dg.runId || !["claude", "codex"].includes((dg.harness || "").replace(/^agent:/, ""))) return null;
  ensureTerminalEvents();
  const badge = el("span", "terminal-run-state");
  badge.dataset.terminalRun = dg.runId;
  terminalPaintRunBadge(badge);
  return badge;
}
function terminalPaintRunBadge(badge) {
  const ob = terminalRunStates.get(badge.dataset.terminalRun);
  badge.replaceChildren(terminalStateDot(ob), el("span", "", "agent " + terminalStateLabel(ob)));
  badge.title = "runtime observation; the run report determines task status";
  if (ob && ob.manifestId && ob.connectivity === "connected" && ob.process === "running") {
    const open = el("button", "sprt-quiet", "open in terminal");
    open.onclick = (event) => { event.preventDefault(); event.stopPropagation(); chatTermOpenInTerminal({ id: ob.manifestId }); };
    badge.append(open);
  }
}
function terminalStateRepaint() {
  window.dispatchEvent(new CustomEvent("manifest-terminal-state", { detail: { connected: terminalEventsConnected } }));
  chatTermSessions = chatTermSessions.map(chatTermApplyState);
  document.querySelectorAll("[data-terminal-run]").forEach(terminalPaintRunBadge);
  renderChatRail();
  chatTermSyncOpen();
}

// chatTermSection — one section head (name · ✦ while any process is live ·
// status dot · count) and, when open, ＋ new + the rows: live first, then
// the last N by lastUsed, the rest folded under "history".
function chatTermSection(kind) {
  const open = kind === chatAgent;
  const sec = el("div", "chat-rail-section" + (open ? " open" : ""));
  const head = el("button", "chat-rail-section-head");
  head.setAttribute("aria-expanded", String(open));
  head.append(el("span", "micro-label chat-rail-section-name", chatTermKinds[kind]));
  const list = chatTermOrder(chatTermList(kind));
  const live = list.filter((s) => s.live).length;
  const working = list.find((se) => se.agentState === "working" && se.connectivity === "connected");
  const blocked = list.find((se) => se.agentState === "blocked" && se.connectivity === "connected");
  head.append(terminalStateDot(working || blocked || list.find((se) => se.connectivity === "connected")));
  if (list.length) head.append(el("span", "aion-org-count", String(list.length)));
  head.title = kind === "claude" ? "Claude Code sessions — the Terminal tab's rows, read as conversations" : "Codex sessions";
  if (list.length) head.title += " · " + live + " live · " + (list.length - live) + " resumable";
  head.onclick = () => { if (!open) location.hash = chatSectionHash(kind); };
  sec.append(head);
  if (!open) return sec;

  const add = el("button", "o-ghost chat-rail-new", "＋ new");
  add.title = "a new " + chatTermKinds[kind] + " session on metis — starts on the first send";
  add.onclick = () => { location.hash = chatNewHash(); };
  sec.append(add);
  if (!list.length) {
    sec.append(emptyRow("no " + chatTermKinds[kind] + " sessions yet"));
    return sec;
  }
  const shown = list.filter((s) => s.live);
  const rest = [];
  list.filter((s) => !s.live).forEach((s) => {
    if (shown.length < chatTermRecent + live || s.id === chatOpenId) shown.push(s); else rest.push(s);
  });
  shown.forEach((s) => sec.append(chatTermRow(s)));
  if (rest.length) {
    const unfolded = !!chatTermHistOpen[kind];
    const tog = el("button", "sprt-quiet chat-rail-hist", (unfolded ? "− " : "＋ ") + "history · " + rest.length);
    tog.title = unfolded ? "fold the older sessions" : "the older sessions (all reachable via ⌘K too)";
    tog.onclick = () => { chatTermHistOpen[kind] = !unfolded; renderChatRail(); };
    sec.append(tog);
    if (unfolded) rest.forEach((s) => sec.append(chatTermRow(s)));
  }
  return sec;
}

// chatTermRow — the shared rail row over a registry row: name (dblclick or ✎
// → inline rename → PUT), ✦ live / ⟳ resumable, ✕ armed → end (live) or
// forget (history); meta = the folder it runs in · when.
function chatTermRow(se) {
  const row = el("div", "chat-rail-row" + (se.id === chatOpenId ? " open" : ""));
  const top = el("div", "chat-rail-top");
  const title = el("span", "chat-rail-title", se.name || se.kind);
  title.title = (se.cwd || "~") + " · double-click to rename";
  title.ondblclick = (e) => { e.stopPropagation(); chatTermRename(title, se); };
  top.append(title);
  top.append(terminalStateDot(se));
  if (!se.live && (se.backend !== "herdr" || se.process === "stopped") && (se.resumeId || se.started)) { const r = el("span", "chat-rail-resume", "⟳"); r.title = "resumable — a send relaunches it"; top.append(r); }
  const pen = el("button", "chat-rail-x", "✎");
  pen.title = "rename";
  pen.onclick = (e) => { e.stopPropagation(); chatTermRename(title, se); };
  const kill = chatTermEndIsKill(se);
  const x = armedDelete("✕", kill ? "end — sure?" : "forget — sure?", () => chatTermEnd(se));
  x.className = "chat-rail-x";
  x.title = kill ? "end the process and keep the conversation" : "forget this conversation association";
  if (se.boardBrief && !kill) { x.disabled = true; x.title = "work-order history stays linked to its task"; }
  top.append(pen, x);
  row.append(top);
  const rm = el("div", "chat-rail-meta");
  rm.append(el("span", "chat-rail-spirit", chatTermFolder(se)));
  rm.append(el("span", "chat-rail-when", fmtWhen(se.lastUsed)));
  row.append(rm);
  row.onclick = () => { location.hash = chatHash(se.id); };
  if (row.onclick) {
    row.tabIndex = 0;
    row.setAttribute("role", "link");
    row.addEventListener("keydown", event => {
      if (event.target === row && event.key === "Enter") { event.preventDefault(); row.click(); }
    });
  }
  return row;
}

function chatTermFolder(se) {
  const c = (se.cwd || "").replace(/\/+$/, "");
  return c ? (c.split("/").pop() || c) : "~";
}

// chatTermRename — inlineRename over the registry's PUT; the cached row and
// the open head are patched in place (no reload).
function chatTermRename(nameEl, se) {
  const inp = inlineRename(nameEl, se.name || "", async (v) => {
    try {
      const res = await fetch(chatTermBase(se.id), {
        method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: v }),
      });
      if (!res.ok) throw new Error((await res.text()).slice(0, 120));
    } catch (e) { showToast("rename failed — " + (e.message || "error")); renderChatRail(); return; }
    se.name = v;
    const cached = chatTermFind(se.id);
    if (cached) cached.name = v;
    if (chatTermOpen && chatTermOpen.id === se.id) { chatTermOpen.se.name = v; chatTermRepaintHead(); }
    renderChatRail();
  });
  inp.addEventListener("blur", () => setTimeout(renderChatRail, 0));
}

// chatTermEnd — live: POST …/kill (the row survives, resumable); history:
// DELETE (forget). Both reached through an armed ✕.
function chatTermEndIsKill(se) { return se.backend === "herdr" ? !["stopped", "not-started"].includes(se.process) : !!se.live; }
async function chatTermEnd(se) {
  const kill = chatTermEndIsKill(se);
  try {
    const res = kill
      ? await fetch(chatTermBase(se.id) + "/kill", { method: "POST" })
      : await fetch(chatTermBase(se.id), { method: "DELETE" });
    if (!res.ok) throw new Error((await res.text()).slice(0, 120));
  } catch (e) { showToast((kill ? "end" : "forget") + " failed — " + (e.message || "error")); renderChatRail(); return; }
  if (!kill && chatOpenId === se.id) {
    chatOpenId = "";
    chatRemember(chatAgent, "");
    location.hash = chatSectionHash(chatAgent);
  }
  await loadChatTermSessions(true);
  renderChatRail();
  chatTermSyncOpen();
}

// chatTermOpenInTerminal — the raw pane, one click away (xterm stays the
// Terminal tab's): remember the stage + the row, then route.
function chatTermOpenInTerminal(se) {
  try { localStorage.setItem("manifest.termStage", "term"); } catch (e) {}
  if (typeof termOpenId !== "undefined") termOpenId = se.id;
  location.hash = "#/terminal/" + encodeURIComponent(se.id);
}

// ---- the open thread ----

async function loadChatTermSession(id) {
  let se = chatTermFind(id);
  if (!se) { chatOpenId = ""; chatLanding = true; renderChatLanding(); return; }
  let d;
  try {
    const res = await fetch(chatTermBase(id) + "/transcript");
    if (!res.ok) { renderChatLanding(); return; }
    d = await res.json();
  } catch (e) { return; }
  if (id !== chatOpenId || !chatIsTerm()) return; // navigated away mid-fetch
  const ref=d.draft&&d.origin?.artifacts?.[0];
  const selection=ref?{...ref,task:d.origin.task,title:"Artifact",version:"?"}:null;
  if(selection){
    try{const r=await fetch("/api/artifacts/get?id="+encodeURIComponent(ref.id));if(r.ok){const a=await r.json();selection.title=a.title||"Artifact";selection.version=a.revisions.find(v=>v.hash===ref.revision)?.n||"?";}}catch(e){}
  }
  if (id !== chatOpenId || !chatIsTerm()) return;
  await chatPrepareDraft(d.conversation,chatAgent+"/"+id,d.draft&&d.origin?{text:d.origin.prompt||"",files:[],task:d.origin.task||"",selection}:null);
  await chatPrepareReadingPosition(d.conversation);
  if (id !== chatOpenId || !chatIsTerm()) return;
  se = chatTermApplyState(chatTermFind(id) || se);
  // the other backends' channels have nothing to say here
  if (chatPollTimer) { clearInterval(chatPollTimer); chatPollTimer = null; }
  if (chatES) { chatES.close(); chatES = null; chatESFor = ""; }
  chatLive = null;
  const main = document.querySelector(".chat-main");
  if (main) main.classList.remove("landing");
  chatTermSurface(true);
  chatRemember(chatAgent, id);
  chatTermOpen = {
    conversation:d.conversation,
    sharedConversation:d.sharedConversation,
    planningTimeline:d.planningTimeline,
    planningRecipients:d.planningRecipients||[],
    planRevisions:d.planRevisions||{},
    proposals:d.proposals||[],
    codingRecipients:d.codingRecipients||[],
    planningOperations:d.planningOperations||[],
    related:d.related||[],
    id, se, turns: d.turns || [], offset: d.offset || 0, title: d.title || "", cost: d.cost || 0,
    live: se.backend === "herdr" ? !!chatTermApplyState(se).live : !!d.live, screen: [], screenSig: "",
  };
  renderChatTermTranscript();
  renderChatComposer(chatTermComposerSession());
  // the CLI's own title names a row still wearing its minted placeholder
  // (the autoName path — an owner-typed name is never overwritten)
  if (d.title && chatTermPlaceholderRe.test(se.name || "")) {
    fetch(chatTermBase(id), {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ autoName: d.title }),
    }).then(() => loadChatTermSessions(true)).catch(() => {});
  }
  if (chatTermOpen.live) chatTermScreenFetch();
  ensureChatTermFast();
}

// chatTermLeave — the stage moved to another section/thread (or the landing):
// stop the tail, hide the strip.
function chatTermLeave() {
  chatTermOpen = null;
  if (chatTermFast) { clearInterval(chatTermFast); chatTermFast = null; }
  const strip = document.getElementById("chatTermStrip");
  if (strip) strip.hidden = true;
  chatTermSurface(false);
}

function chatTermComposerSession() {
  const tasks=(chatTermOpen?.conversation?.links||[]).filter(l=>l.kind==="task");
  return { busy: chatTermSending, live: !!(chatTermOpen && chatTermOpen.live), task:tasks.length===1?tasks[0].id:"" };
}

// the prompt line's hint reads like a shell's, lowercase
function chatTermPlaceholder() {
  if (chatTermSending) return "sending…";
  if (chatTermOpen?.se.process === "not-started") return "enter starts this session and sends · shift+enter for a new line";
  if (!chatOpenId) return "enter starts a new " + chatTermKinds[chatAgent] + " session there and sends";
  if (chatTermOpen && chatTermOpen.se.backend === "herdr" && chatTermOpen.se.connectivity !== "connected") return "runtime unavailable · open in terminal to inspect";
  if (chatTermOpen && chatTermOpen.live) return "enter sends to the live session · shift+enter for a new line";
  return "enter resumes the session, then sends";
}

// chatTermSyncOpen — after metadata loads or an event: the open row's liveness/name may
// have moved under us (ended in the Terminal tab, relaunched by Alfred, …).
function chatTermSyncOpen() {
  const o = chatTermOpen;
  if (!o || !chatIsTerm() || chatOpenId !== o.id) return;
  const se = chatTermFind(o.id);
  if (!se) { // forgotten elsewhere
    chatOpenId = "";
    chatRemember(chatAgent, "");
    location.hash = chatSectionHash(chatAgent);
    return;
  }
  const wasLive = o.live, wasProcess = o.se.process;
  o.se = se;
  o.live = !!se.live;
  chatTermRepaintHead();
  renderChatComposer(chatTermComposerSession());
  if (o.live !== wasLive) {
    chatTermPaintStrip();
    if (o.live) chatTermScreenFetch();
    else chatTermRequestFinalTail(o);
  } else if (se.process === "stopped" && wasProcess !== "stopped") chatTermRequestFinalTail(o);
}

// chatTermHead — the .sprt-head anatomy: name (inline rename) · kind · folder
// · live/resumable · meta fmtWhen(lastUsed) · $cost · actions ✎ · open in
// terminal ↗ · ✕ end (armed → kill) / forget.
function chatTermHead(o) {
  const se = o.se;
  const head = el("div", "sprt-head chat-head");
  const title = el("span", "sprt-title chat-head-title", se.name || se.kind);
  title.title = (o.title && o.title !== se.name ? o.title + " · " : "") + "double-click to rename";
  title.ondblclick = () => chatTermRename(title, se);
  head.append(title);
  const sub = [chatTermKinds[se.kind], se.cwd || "~"];
  if(se.model)sub.push(se.model);
  sub.push(se.backend === "herdr" ? "agent " + terminalStateLabel(se) : (o.live ? "process running" : (se.resumeId || se.started ? "resumable" : "not started")));
  head.append(terminalStateDot(se));
  const status = el("span", "sprt-sub chat-head-sub", chatTermKinds[se.kind] + " · " + (se.backend === "herdr" ? terminalStateLabel(se) : o.live ? "running" : "stopped"));
  status.title = sub.join(" · ");
  head.append(status);
  if(o.sharedConversation){const shared=el("a","sprt-quiet",o.sharedConversation.scope==="team:ooda"?"OODA team conversation":"AION team conversation");shared.href=o.sharedConversation.route;shared.title="This session's history and future messages are shared with the team.";head.append(shared);}
  if(se.backend==="herdr"&&se.origin?.mode!=="continue"&&(chatTermEnabled||chatRoster.some(a=>a.enabled&&a.durableSend))){
    const recipient=chatRecipients.get(se.kind+"/"+se.id);
    const to=el("button","sprt-quiet chat-head-sub","To "+chatAgentLabel(recipient?.agent||se.kind));
    to.onclick=()=>chatChooseTerminalRecipient(o);head.append(to);
    const planning=(o.planningRecipients||[]).find(p=>p.id===recipient?.id&&p.agent===recipient?.agent);
    if(planning?.status==="thinking")head.append(el("span","chat-head-sub",chatAgentLabel(planning.agent)+" is working"));
    const coding=(o.codingRecipients||[]).find(p=>p.id===recipient?.id&&p.agent===recipient?.agent);
    if(coding)head.append(el("span","chat-head-sub",chatAgentLabel(coding.agent)+" · "+(coding.agentState||coding.process||"unknown")));
  }
  const meta = [fmtWhen(se.lastUsed)];
  if (o.cost) meta.push("$" + o.cost.toFixed(2));
  const details = el("details", "chat-details");
  details.append(el("summary", "", "Details"), el("div", "chat-head-meta", sub.join(" · ") + " · " + meta.join(" · ")));
  const taskLinks=(o.conversation?.links||[]).filter(link=>link.kind==="task");
  if(taskLinks.length===1) {
    const task=el("a","sprt-quiet","Task ↗");
    task.href=chatTaskThreadHash(taskLinks[0].id);
    task.title="Open the task for this coding session";
    head.append(task);
  }
  for(const warning of o.conversation?.warnings||[])details.append(el("div","chat-head-meta",warning));
  if(chatRoster.some(a=>a.enabled&&a.durableSend)) {
    const related=el("button","sprt-quiet","Start related chat");
    related.onclick=()=>chatStartRelated({backend:"terminal",agent:se.kind,id:se.id,title:se.name||se.kind,task:taskLinks.length===1?taskLinks[0].id:"",
      handoffExcerpt:o.turns.slice(-2).map(t=>t.who+":\n"+(t.text||(t.blocks||[]).filter(b=>b.t==="say").map(b=>b.text||"").join("\n")).slice(0,2000)).join("\n\n")});
    details.append(related);
  }
  for(const item of o.related||[]) {const link=el("a","sprt-quiet",(item.relation==="origin"?"From: ":"Related: ")+item.title);link.href=item.route;details.append(link);}
  const acts = el("span", "chat-head-acts");
  const ren = el("button", "sprt-quiet", "✎");
  ren.title = "rename";
  ren.onclick = () => chatTermRename(title, se);
  acts.append(ren);
  const addressed=chatRecipients.get(se.kind+"/"+se.id);
  const selectedRuntime=addressed?.backend==="terminal"?(o.codingRecipients||[]).find(p=>p.id===addressed.id&&p.agent===addressed.agent):null;
  const raw = el("button", "sprt-quiet", selectedRuntime?"Open "+chatAgentLabel(selectedRuntime.agent)+" terminal ↗":"open in terminal ↗");
  raw.title = "the raw pane (xterm) in the Terminal tab";
  raw.onclick = () => chatTermOpenInTerminal(selectedRuntime||se);
  if(selectedRuntime||se.launchPhase!=="draft")head.append(raw);
  const reviewRuntime=selectedRuntime||se;
  if(!se.device&&reviewRuntime.cwd){
    head.append(chatChangesButton(reviewRuntime));
  }
  const kill = chatTermEndIsKill(se);
  if (!se.boardBrief || kill) acts.append(armedDelete(kill ? "✕ end" : "forget", kill ? "end — sure?" : "forget — sure?", () => chatTermEnd(se)));
  details.append(acts);
  head.append(details);
  return head;
}

function chatTermRepaintHead() {
  const cur = document.querySelector("#chatThreadHeader .chat-head");
  if (cur && chatTermOpen && !chatHeadRenaming(cur)) chatMountHeader(chatTermHead(chatTermOpen));
}

function renderChatTermTranscript() {
  const host = document.getElementById("chatTranscript");
  const o = chatTermOpen;
  if (!host || !o) return;
  const readKey=o.conversation?.key||"";
  const restoreReading=host.dataset.readKey!==readKey;
  host.dataset.readKey=readKey;
  if(restoreReading)chatReadingGestureUntil=0;
  bindChatScroll();
  host.innerHTML = "";
  chatMountHeader(chatTermHead(o));
  const body = el("div", "chat-term-turns");
  body.id = "chatTermTurns";
  host.append(body);
  chatTermPaintTurns();
  chatTermPaintStrip();
  chatStick = true;
  chatPin();
  if(restoreReading)chatRestoreReadingPosition(host,chatReadingStates.get(readKey)?.value);
}

function chatTermPaintTurns() {
  const body = document.getElementById("chatTermTurns");
  const host = document.getElementById("chatTranscript");
  const o = chatTermOpen;
  if (!body || !o) return;
  const previousScroll=host?.scrollTop||0;
  body.innerHTML = "";
  if(o.planningTimeline)chatPaintTurns(body,o.planningTimeline,null);
  else chatTermPaintLines(body, o.turns);
  for(const operation of o.planningOperations||[])body.append(manifestOperationCard(operation));
  appendTaskApprovals(body,o);
  if (!(o.planningTimeline||o.turns).length) {
    body.append(el("div", "chat-term-line chat-term-sys", o.se.launchPhase === "draft"
      ? "Review your draft below. Sending starts the coding session."
      : o.se.kind === "codex"
      ? "No Codex transcript turns are available yet; check the live screen or open Terminal"
      : (o.live ? "no turns in the session file yet" : "nothing in the session file — a send starts it")));
  }
  if(host&&!chatStick)host.scrollTop=previousScroll;
  chatPin();
}

// ---- the terminal painter ----
// A claude/codex thread is a CLI session, so it paints as one: the user's
// turns are commands behind a prompt glyph, the agent's turns are raw output
// lines, its tool calls are step markers — no bubbles, no cards, mono
// throughout, on the terminal's dark surface (.chat-main.term, 48-chat.css).
// The block grammar is the shared one (chatTurnBlocks); only the paint
// differs, so the Alfred/Kairos/Zeck/spirits sections keep chatPaintTurns.
const chatTermPromptGlyph = "❯";
const chatTermStepGlyph = "→";
const chatTermResultGlyph = "⎿";

// chatTermSurface — the .chat-main.term context class the terminal styling
// hangs on (transcript · screen strip · composer read as one surface).
function chatTermSurface(on) {
  const main = document.querySelector(".chat-main");
  if (main) main.classList.toggle("term", !!on);
}

function chatTermPaintLines(host, turns) {
  turns.forEach((t) => {
    if (t.who === "user") { const row=chatTermCmdLine(t);if(t.id)row.dataset.chatReadTurn=t.id;host.append(row); return; }
    if (t.who === "system") {
      const row=el("div", "chat-term-line chat-term-sys", t.text || "");if(t.id)row.dataset.chatReadTurn=t.id;host.append(row);
      return;
    }
    const out = el("div", "chat-term-out");
    if(t.id)out.dataset.chatReadTurn=t.id;
    const proposal=chatTermOpen?.planRevisions?.[t.id];
    chatProposalBlocks(t,proposal).forEach(b=>out.append(chatTermBlockEl(b)));
    if(proposal)out.append(chatPlanReviewButton(proposal));
    const meta = [];
    if (t.ts) meta.push(fmtWhen(t.ts));
    if (t.usd) meta.push("$" + t.usd);
    if (meta.length) out.append(el("div", "chat-term-meta", meta.join(" · ")));
    host.append(out);
  });
}

// chatTermCmdLine — what you sent, as the command it was: `❯ text`, the time
// it landed as a dim trailing note. A [file::] token (never on these turns —
// a terminal takes keys, not files) would show as its bare text.
function chatTermCmdLine(t) {
  const line = el("div", "chat-term-line chat-term-cmd");
  line.append(el("span", "chat-term-glyph", chatTermPromptGlyph));
  line.append(el("span", "chat-term-cmd-text", (t.text || "").trim()));
  if (t.ts) line.append(el("span", "chat-term-meta", fmtWhen(t.ts)));
  return line;
}

// chatTermBlockEl — one block as terminal lines: say → the output (markdown
// keeps its structure, the surface makes it mono), think → a folded dim
// disclosure, step → `→ cast input`, its paired result folded under `⎿`.
function chatTermBlockEl(b) {
  if (b.t === "say") {
    const say = el("div", "chat-term-say");
    if (b.plain) { say.textContent = b.text || ""; return say; }
    try { say.append(renderMarkdown(b.text || "", "", { readOnly: true })); }
    catch (e) { say.textContent = b.text || ""; }
    return say;
  }
  if (b.t === "think") {
    const det = document.createElement("details");
    det.className = "chat-term-think";
    const sum = document.createElement("summary");
    sum.append(el("span", "chat-term-glyph", "▸"), el("span", "chat-term-cast", "thinking"));
    det.append(sum, el("div", "chat-term-think-text", b.text || ""));
    return det;
  }
  const ln = el("div", "chat-term-step" + (b.error ? " err" : ""));
  ln.append(el("span", "chat-term-glyph", chatTermStepGlyph));
  ln.append(el("span", "chat-term-cast", b.cast || "step"));
  ln.append(el("span", "chat-term-step-input", b.input || ""));
  if (!b.result) return ln;
  const det = document.createElement("details");
  det.className = "chat-term-step-details";
  const sum = document.createElement("summary");
  sum.append(ln);
  ln.append(el("span", "chat-term-step-fold", b.error ? "· error" : "▸"));
  const res = el("div", "chat-term-result" + (b.error ? " err" : ""));
  res.append(el("span", "chat-term-glyph", chatTermResultGlyph));
  res.append(el("pre", "chat-term-result-text", b.result));
  det.append(sum, res);
  return det;
}

// ---- the live strip: the pane's last lines + quick keys ----
// Mounted once between the transcript and the composer (so it stays in view
// while the transcript scrolls); shown only while the process is live. This is
// how a permission prompt or a menu becomes visible and answerable.
// Common keys verified through both runtime adapters. Raw Terminal also supports Tab.
const chatTermQuickKeys = ["enter", "esc", "↑", "↓", "←", "→", "ctrl-c"];

function chatTermStripEl() {
  let strip = document.getElementById("chatTermStrip");
  if (strip) return strip;
  const main = document.querySelector(".chat-main");
  const comp = document.getElementById("chatComposer");
  if (!main || !comp) return null;
  strip = el("div", "chat-term-strip");
  strip.id = "chatTermStrip";
  strip.hidden = true;
  const head = el("div", "chat-term-strip-head");
  head.append(el("span", "micro-label", "Live terminal"));
  head.append(el("span", "chat-landing-hint", "Use these keys for the prompt below · for Tab / Shift-Tab, open Terminal"));
  const screen = el("pre", "chat-term-screen");
  const keys = el("div", "chat-term-keys");
  chatTermQuickKeys.forEach((label) => {
    const b = el("button", "chat-term-key", label);
    // Claude Code's permission dialog is a numbered menu: enter takes the
    // highlighted answer, ↑/↓ move; y/n serve the plain y/n prompts (a
    // shell's, the trust dialog's)
    b.title = label === "y" || label === "n" ? "answer a y/n prompt (Claude Code's menus take enter · ↑ ↓)"
      : label === "enter" ? "take the highlighted answer (a permission dialog's Yes)" : "send " + label;
    b.onclick = () => chatTermKey(label);
    keys.append(b);
  });
  strip.append(head, screen, keys);
  main.insertBefore(strip, comp);
  return strip;
}

function chatTermPaintStrip() {
  const strip = chatTermStripEl();
  const o = chatTermOpen;
  if (!strip) return;
  if (!o || !o.live) { strip.hidden = true; return; }
  strip.hidden = false;
  const screen = strip.querySelector(".chat-term-screen");
  const follow = screen.scrollHeight - screen.clientHeight - screen.scrollTop < 24;
  const previous = screen.scrollTop;
  screen.textContent = o.screen.length ? o.screen.join("\n") : "…";
  screen.scrollTop = follow ? screen.scrollHeight : previous;
}

async function chatTermScreenFetch() {
  const o = chatTermOpen;
  if (!o) return;
  // the tick and a quick key both ask; the newest reply wins — an older one
  // landing later must not paint a stale pane over the current one
  const seq = o.screenSeq = (o.screenSeq || 0) + 1;
  let d;
  try { d = await (await fetch(chatTermBase(o.id) + "/screen")).json(); } catch (e) { return; }
  if (chatTermOpen !== o || seq !== o.screenSeq) return;
  const sig = JSON.stringify(d.lines || []);
  const flipped = o.se.backend !== "herdr" && !!d.live !== o.live;
  if (sig === o.screenSig && !flipped) return;
  o.screen = d.lines || [];
  o.screenSig = sig;
  if (flipped) {
    // the pane went away (ended elsewhere / the CLI exited): the registry
    // poll confirms; say so now
    o.live = !!d.live;
    chatTermRepaintHead();
    renderChatComposer(chatTermComposerSession());
    loadChatTermSessions(true);
  }
  chatTermPaintStrip();
}

// chatTermKey — a quick key: the terminal key bar's codes (TERM_KEY_CODES,
// 73-terminal.js), restricted to the controls supported by both adapters.
async function chatTermKey(label) {
  const o = chatTermOpen;
  if (!o || !chatTermQuickKeys.includes(label)) return;
  const codes = typeof TERM_KEY_CODES !== "undefined" ? TERM_KEY_CODES : {};
  const key = codes[label] || label;
  try {
    const result = await postJSONOk(chatTermBase(o.id) + "/input", { key, ...(o.se.backend === "herdr" ? {requestId: crypto.randomUUID()} : {}) });
    if (result.delivery && result.delivery.state !== "sent") showToast("Key delivery is unconfirmed — check the terminal before pressing it again.");
    setTimeout(chatTermScreenFetch, 350);
  } catch (e) { showToast("key failed — " + (e.message || "error")); }
}

// ---- the tail: transcript (by byte offset) + screen while live ----

function ensureChatTermFast() {
  if (chatTermFast) return;
  chatTermFast = setInterval(chatTermTick, 1500);
}

// one tail in flight: a slow inventory reply must not
// let the next tick re-ask from the same offset — two identical tails would
// merge the same turns twice
let chatTermTailing = false;

async function chatTermTick() {
  const o = chatTermOpen;
  if (!o || !chatIsTerm() || chatOpenId !== o.id) { chatTermLeave(); return; }
  if (!els.chatView || els.chatView.hidden || document.hidden || chatTermTailing) return; // file reconciliation also reads the final records after stop
  chatTermTailing = true;
  try { await chatTermTail(o); } finally {
    chatTermTailing = false;
    if (o.finalTailPending) { o.finalTailPending = false; chatTermRequestFinalTail(o); }
  }
}

// Serialize the stop-triggered final read with any in-flight file tail. A stop
// during an older read cannot hide the final records behind a live-only gate.
async function chatTermRequestFinalTail(o) {
  if (chatTermOpen !== o) return;
  if (chatTermTailing) { o.finalTailPending = true; return; }
  chatTermTailing = true;
  try { await chatTermTail(o); } finally {
    chatTermTailing = false;
    if (o.finalTailPending) { o.finalTailPending = false; chatTermRequestFinalTail(o); }
  }
}

async function chatTermTail(o) {
  let d;
  try { d = await (await fetch(chatTermBase(o.id) + "/transcript?after=" + o.offset)).json(); } catch (e) { return; }
  if (chatTermOpen !== o) return;
  const planningChanged=JSON.stringify([o.planningTimeline,o.planningOperations,o.planRevisions||{},o.proposals||[]])!==JSON.stringify([d.planningTimeline,d.planningOperations,d.planRevisions||{},d.proposals||[]]);
  o.planningTimeline=d.planningTimeline;
  o.planningOperations=d.planningOperations;
  o.planRevisions=d.planRevisions||{};
  o.proposals=d.proposals||[];
  const turns = d.turns || [];
  if (d.offset < o.offset) { // the file was replaced/truncated: the reply is the whole projection
    o.turns = turns;
    o.offset = d.offset || 0;
    chatTermPaintTurns();
  } else if (turns.length) {
    chatTermMerge(o.turns, turns);
    o.offset = d.offset;
    chatTermPaintTurns();
  } else if (d.offset > o.offset) {
    o.offset = d.offset; // records that projected to nothing (cost-state, …)
  }
  if(planningChanged)chatTermPaintTurns();
  let headDirty = false;
  if(JSON.stringify(o.sharedConversation)!==JSON.stringify(d.sharedConversation)){o.sharedConversation=d.sharedConversation;headDirty=true;}
  if(JSON.stringify(o.planningRecipients)!==JSON.stringify(d.planningRecipients||[])){o.planningRecipients=d.planningRecipients||[];headDirty=true;}
  if(JSON.stringify(o.codingRecipients)!==JSON.stringify(d.codingRecipients||[])){o.codingRecipients=d.codingRecipients||[];headDirty=true;}
  if (d.title && d.title !== o.title) { o.title = d.title; headDirty = true; }
  if (d.cost && d.cost !== o.cost) { o.cost = d.cost; headDirty = true; }
  if (o.se.backend !== "herdr" && !!d.live !== o.live) { o.live = !!d.live; headDirty = true; renderChatComposer(chatTermComposerSession()); chatTermPaintStrip(); }
  if (headDirty) chatTermRepaintHead();
  if (o.live) chatTermScreenFetch();
}

// chatTermMerge — a tail's first assistant turn continues the last painted
// assistant turn (the projection merges consecutive assistant records; a
// byte-offset cut can split one), and a result whose call was painted from
// an earlier poll (cast "result" + the call's id) lands on that step
// instead of a chip of its own.
function chatTermMerge(turns, tail) {
  tail.forEach((t) => {
    if (t.who !== "assistant") { turns.push(t); return; }
    const blocks = (t.blocks || []).filter((b) => !(b.t === "step" && b.cast === "result" && b.id && chatTermPairResult(turns, b)));
    const last = turns[turns.length - 1];
    if (last && last.who === "assistant") { last.blocks = (last.blocks || []).concat(blocks); return; }
    if (!blocks.length) return; // nothing left once paired
    t.blocks = blocks;
    turns.push(t);
  });
}

function chatTermPairResult(turns, b) {
  for (let ti = turns.length - 1; ti >= 0; ti--) {
    if (turns[ti].who !== "assistant") continue;
    const st = (turns[ti].blocks || []).find((x) => x.t === "step" && x.id === b.id && x.cast !== "result");
    if (st) { st.result = b.result; st.error = !!b.error; return true; }
  }
  return false;
}

// ---- send ----

// chatTermSend — POST …/input {text}. On the landing the registry row is
// saved as a draft first (kind + the folder typed there); input starts the
// draft or resumes an existing CLI, waits for its prompt, then delivers. Returns false when
// the text should go back into the composer.
async function chatTermSend(text,context={}) {
  if (!text) return true;
  if (chatTermSending) return false;
  chatTermSending = true;
  const agent=chatAgent,route=chatRouteVersion,sourceScope=chatAgent+"/"+(chatOpenId||"new");
  renderChatComposer(chatTermComposerSession());
  try {
    let id = chatOpenId, created = false;
    const wasDraft = chatTermFind(id)?.launchPhase === "draft";
    if (!id) {
      const cwd = chatRecall("manifest.chatTermCwd." + agent);
      const se = await postJSONOk("/api/terminal/session", { kind: agent, cwd, draft: true });
      chatTermSessions.unshift(Object.assign({ live: false }, se));
      id = se.id;
      created = true;
      // the row exists now whatever the delivery does: route to it first so a
      // failed first send leaves the thread open (text back in the composer),
      // not a landing with a hidden open id
      if(route===chatRouteVersion){chatOpenId=id;chatLanding=false;location.hash="#/chat/a/"+encodeURIComponent(agent)+"/"+encodeURIComponent(id);}
    }
    const url=chatTermBase(id)+"/input",payload={text,...context};
    const r = chatTermFind(id)?.backend==="herdr"
      ? await chatDeliverRemembered(chatRememberDelivery(agent+"/"+id,agent,url,payload,sourceScope))
      : await postJSONOk(url,payload);
    // a draft row's first send starts its process — that is a start, not a relaunch
    if (r.relaunched && !created && !wasDraft) showToast("Session relaunched — " + ((chatTermFind(id) || {}).name || id), null, "info");
    await loadChatTermSessions(true);
    if (chatTermOpen && chatTermOpen.id === id) {
      if (chatTermOpen.se.backend !== "herdr" && !chatTermOpen.live) { chatTermOpen.live = true; chatTermRepaintHead(); }
      chatTermScreenFetch();
    }
    return true;
  } catch (e) {
    showToast("Send failed — " + (e.message || "error"));
    return false;
  } finally {
    chatTermSending = false;
    renderChatComposer(chatTermComposerSession());
  }
}

// ---- landing: a new local herdr session, in the folder typed here ----
function renderChatTermLanding(host) {
  const kind = chatAgent;
  chatTermSurface(true); // the composer is the prompt of the session to come
  const who = el("div", "chat-spirit-pick");
  who.append(el("span", "pill light on", chatTermKinds[kind]));
  who.append(el("span", "chat-landing-hint", "this server · herdr"));
  host.append(who);
  if (!chatTermEnabled) {
    host.append(emptyRow("The terminal is not enabled on this server — sessions can't start here."));
    return;
  }
  const cwd = document.createElement("input");
  cwd.className = "chat-landing-cwd";
  cwd.placeholder = "/absolute/path — the folder the session opens in (blank = home)";
  cwd.spellcheck = false;
  cwd.value = chatRecall("manifest.chatTermCwd." + kind);
  cwd.oninput = () => { try { localStorage.setItem("manifest.chatTermCwd." + kind, cwd.value.trim()); } catch (e) {} };
  cwd.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); focusChatInput(); } };
  host.append(cwd);
  host.append(el("div", "chat-landing-hint", "type below — Enter starts the session there and sends; it also appears in the Terminal tab"));
  focusChatInput();
}

// ⌘K: every claude/codex session by name ("Claude session · <name>").
cmdRegistry.register(async (q) => {
  if (!q) return [];
  let list = chatTermSessions;
  if (!list.length) {
    try { list = ((await (await fetch("/api/terminal/sessions")).json()).sessions) || []; } catch (e) { list = []; }
  }
  return list.filter((s) => chatIsTerm(s.kind) && !s.device).slice(0, 60).map((s) => ({
    id: "chat:term:" + s.id,
    name: (s.kind === "codex" ? "Codex session · " : "Claude session · ") + (s.name || s.kind),
    hint: (s.live ? "live" : "resumable") + " · " + (s.cwd || "~"),
    keywords: "chat session conversation terminal " + s.kind + (s.kind === "claude" ? " claude code" : " codex") + " " + (s.cwd || ""),
    act: () => { closeCmdbar(); chatOpenAgentSession(s.kind, s.id); },
  }));
});

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

// Artifact selection belongs to a conversation, never to the global recipient.
const chatArtifactSelections = new Map();
let chatWorkspace = null;
let chatPendingWorkspace = null;
function chatCloseWorkspace() { if (chatWorkspace) { const w=chatWorkspace; chatWorkspace=null; w.close(); } }
function chatOpenAttachment(file,href){
  chatCloseWorkspace();
  const shell=document.querySelector(".chat-shell");shell.classList.add("has-artifact");
  chatWorkspace=attachmentWorkspace(shell,file,href,()=>{shell.classList.remove("has-artifact");chatWorkspace=null;});
}
function chatChangesButton(runtime){
  const button=el("button","sprt-quiet","Changes");button.title="Capture current Git changes in this runtime's working folder";
  button.onclick=async()=>{const route=chatRouteVersion;button.disabled=true;try{const snapshot=await postJSONOk(chatTermBase(runtime.id)+"/changes/snapshot",{});if(route===chatRouteVersion)chatOpenWorkingArtifact({...snapshot,selectionKey:"chat:"+chatAgent+"/"+chatOpenId});}catch(e){showToast(e.message||"Could not capture working-folder changes.");}finally{button.disabled=false;}};
  return button;
}
function chatOpenWorkingArtifact(spec) {
  chatCloseWorkspace();
  const taskID = spec.task || chatTaskID;
  const key = spec.selectionKey || (taskID ? "task:"+taskID : location.hash);
  const shell = document.querySelector(".chat-shell");
  shell.classList.add("has-artifact");
  const load = async () => {
    const path = spec.plan ? "/api/tasks/plan/workspace?id="+encodeURIComponent(taskID) : "/api/artifacts/get?id="+encodeURIComponent(spec.id);
    const r=await fetch(path); if(!r.ok)throw new Error(await r.text());return r.json();
  };
  chatWorkspace=artifactWorkspace(shell,{
    load, revision:spec.revision,proposal:spec.proposal,
    save:spec.plan ? (text,expectedRevision)=>postJSONOk("/api/tasks/plan",{id:taskID,text,expectedRevision}):null,
    onClose:()=>{shell.classList.remove("has-artifact");chatWorkspace=null;},
    onDiscuss: (taskID||spec.discuss) && (key.startsWith("task:") || chatRosterEntry(chatAgent)?.durableSend || chatIsTerm()) ? ref=>{
      chatArtifactSelections.set(key,{...ref,task:taskID,discuss:!!spec.discuss});
      if(key.startsWith("chat:"))chatRenderArtifactContext(taskID,key);
      if(key.startsWith("chat:"))chatCaptureSyncedDraft(key.slice(5));
      else if(key.startsWith("task:"))todoSaveArtifactSelection(taskID);
      if(window.matchMedia("(max-width: 900px)").matches)chatCloseWorkspace();
      document.querySelector("#chatComposer textarea")?.focus();
    }:null
  });
}
function chatRenderArtifactContext(taskID,key,host){
 key=key||"task:"+taskID;
 const composer=host||document.getElementById("chatComposer");if(!composer)return;
 composer.querySelector(".chat-artifact-context")?.remove();
 const ref=chatArtifactSelections.get(key);if(!ref)return;
 taskID=ref.task||taskID;
 const row=el("div","chat-artifact-context");
 const open=el("button","sprt-quiet","Discussing: "+ref.title+" · v"+ref.version);
 open.onclick=()=>{
   const spec={id:ref.id,revision:ref.revision,task:taskID,discuss:ref.discuss,selectionKey:key};
   if(key.startsWith("task:")&&!location.hash.startsWith("#/chat/task/")){chatPendingWorkspace=spec;location.hash=chatTaskThreadHash(taskID);}
   else chatOpenWorkingArtifact(spec);
 };
 const clear=el("button","sprt-quiet","×");clear.setAttribute("aria-label","Remove artifact context");
 clear.onclick=()=>{chatArtifactSelections.delete(key);row.remove();if(key.startsWith("chat:"))chatCaptureSyncedDraft(key.slice(5));else if(key.startsWith("task:"))todoSaveArtifactSelection(taskID);};
 row.append(open,clear);composer.prepend(row);
}
function chatArtifactActions(data){
 const row=el("div","chat-artifact-actions");
 if(data.record?.plan){const b=el("button","sprt-quiet","Plan");b.onclick=()=>chatOpenWorkingArtifact({plan:true,task:data.id});row.append(b);}
 const seen=new Set();
 for(const a of [...(data.artifacts?.outputs||[]),...(data.artifacts?.inputs||[])]){
  if(seen.has(a.id)||a.provenance?.source==="task-plan")continue;seen.add(a.id);
  const b=el("button","sprt-quiet",a.title||a.ref||"Artifact");b.disabled=!!a.unknown;
  if(a.unknown)b.title="Artifact unavailable in this registry";
  b.onclick=()=>chatOpenWorkingArtifact({id:a.id,task:data.id});row.append(b);
 }
 return row;
}

// Persist uncertain Hermes sends before transport. A retry reuses the exact
// request ID and payload, including New chat; it never recreates user intent.
const chatDeliveryStorageKey = "manifest.chatDeliveryOutbox.v1";
function chatIsTerminalDelivery(item){return ["claude","codex"].includes(item.agent)&&/^\/api\/terminal\/session\/[a-f0-9]{8,32}\/input$/.test(item.url);}
function chatReadDeliveryOutbox(){
 try {
  const x=JSON.parse(localStorage.getItem(chatDeliveryStorageKey)||"[]");
  return Array.isArray(x)?x.filter(v=>{
   if(!v || !/^[a-z0-9][a-z0-9-]{0,31}$/.test(v.agent) || !/^[a-zA-Z0-9_-]{8,128}$/.test(v.payload?.requestId) || typeof v.url!=="string")return false;
   if(chatIsTerminalDelivery(v))return true;
   const base=chatBaseFor(v.agent);
   return v.url===base || (v.url.startsWith(base) && /^\/[0-9]{8}-[0-9]{6}-[0-9a-z]{2,8}\/messages$/.test(v.url.slice(base.length)));
  }):[];
 }catch(e){return [];}
}
function chatWriteDeliveryOutbox(items){localStorage.setItem(chatDeliveryStorageKey,JSON.stringify(items));}
async function chatSaveDeliveryRecovery(item,remove=false){
 const key=item.stateKey||chatSyncedDrafts.get(item.draftScope||item.scope)?.key;
 if(!key)return; // Legacy/local-only composers have no synced state descriptor.
 item.stateKey=key;
 const url="/api/chat/state/"+encodeURIComponent(key)+"/deliveries";
 for(let attempt=0;attempt<4;attempt++){
  const r=await fetch(url,{cache:"no-store"});if(!r.ok)throw Error("Send recovery sync unavailable. Your message remains saved on this device.");
  const state=await r.json();
  if(state.key!==key||state.slot!=="deliveries"||!Number.isSafeInteger(state.revision)||state.revision<0)throw Error("Invalid send recovery state.");
  const items={...state.value?.items};
  if(remove)delete items[item.payload.requestId];else items[item.payload.requestId]={...item};
  const value={items};
  const saved=await fetch(url,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({revision:state.revision,value})});
  if(saved.status===409)continue;
  if(!saved.ok)throw Error("Send recovery sync unavailable. Your message remains saved on this device.");
  const ack=await saved.json();
  if(ack.key!==key||ack.slot!=="deliveries"||!Number.isSafeInteger(ack.revision)||ack.revision<state.revision||!chatStateEqual(ack.value,value))throw Error("Send recovery acknowledgement unavailable.");
  return;
 }
 throw Error("Send recovery changed on another device. Try again.");
}
async function chatLoadDeliveryRecovery(scope){
 const state=chatSyncedDrafts.get(scope);if(!state?.key)return;
 try{
  const r=await fetch("/api/chat/state/"+encodeURIComponent(state.key)+"/deliveries",{cache:"no-store"});if(!r.ok)return;
  const remote=await r.json();if(remote.key!==state.key||remote.slot!=="deliveries")return;
  const local=chatReadDeliveryOutbox(),ids=new Set(local.map(x=>x.payload.requestId));
  for(const item of Object.values(remote.value?.items||{})){
   if(!item?.payload?.requestId||(item.draftScope||item.scope)!==scope||item.stateKey!==state.key||ids.has(item.payload.requestId))continue;
   local.push(item);ids.add(item.payload.requestId);
  }
  chatWriteDeliveryOutbox(local);
  // Only read receipts. An absent or uncertain receipt never authorizes replay.
  for(const item of chatReadDeliveryOutbox().filter(x=>(x.draftScope||x.scope)===scope&&!x.accepted)){
   const path=chatIsTerminalDelivery(item)?item.url.replace(/\/input$/,"/delivery"):"/api/agents/chat/"+encodeURIComponent(item.agent)+"/delivery";
   const receipt=await fetch(path+"?request="+encodeURIComponent(item.payload.requestId),{cache:"no-store"});if(!receipt.ok)continue;
   const result=await receipt.json();
   if(result.delivery?.id!==item.payload.requestId||!result.id)continue;
   if(chatIsTerminalDelivery(item)&&result.delivery.state!=="sent")continue;
   await chatAcceptDelivery(item,result);
  }
 }catch(e){/* Retain the local recovery record until sync is available. */}
}
function chatRememberDelivery(scope,agent,url,payload,draftScope=scope){
 const items=chatReadDeliveryOutbox(), signature=JSON.stringify(payload);
 const old=items.find(x=>x.scope===scope&&x.url===url&&x.signature===signature);
 if(old)return old;
 const item={scope,agent,url,signature,payload:{...payload,requestId:crypto.randomUUID()},draft:chatSyncedDrafts.get(draftScope)?.value||null,draftScope,at:new Date().toISOString()};
 items.push(item);chatWriteDeliveryOutbox(items);return item;
}
async function chatForgetDelivery(item){await chatSaveDeliveryRecovery(item,true);chatWriteDeliveryOutbox(chatReadDeliveryOutbox().filter(x=>x.payload.requestId!==item.payload.requestId));}
async function chatAcceptDelivery(item,result){
 const items=chatReadDeliveryOutbox(),saved=items.find(x=>x.payload.requestId===item.payload.requestId);
 if(saved){saved.accepted=result;chatWriteDeliveryOutbox(items);}
 // Persist the acknowledgement before touching the draft. Recovery must only
 // reconcile state, never submit another runtime instruction.
 if(!item.draft){await chatForgetDelivery(item);return result;}
 const state=chatSyncedDrafts.get(item.draftScope||item.scope);
 if(state&&await state.reconcileSent(item.draft))await chatForgetDelivery(item);
 return result;
}
async function chatReconcileAcceptedDrafts(key){
 for(const item of chatReadDeliveryOutbox().filter(x=>x.accepted&&(x.draftScope||x.scope)===key))await chatAcceptDelivery(item,item.accepted);
}
async function chatDeliverRemembered(item){
 const accepted=item.accepted||chatReadDeliveryOutbox().find(x=>x.payload.requestId===item.payload.requestId)?.accepted;
 if(accepted)return chatAcceptDelivery(item,accepted);
 await chatSaveDeliveryRecovery(item);
 const res=await fetchJSONRetry("POST",item.url,item.payload);
 if(!res.ok){const error=new Error((await res.text()).trim()||"Send failed");error.rejected=[400,413,422].includes(res.status);throw error;}
 const result=await res.json();
 if(chatIsTerminalDelivery(item)&&result.delivery?.state!=="sent")throw new Error("Submission is unconfirmed. Check its status or inspect the native conversation before sending another instruction.");
 if(result.ok!==true && !result.id)throw new Error("Delivery acknowledgement unavailable");
 return chatAcceptDelivery(item,result);
}
function chatRenderDeliveryNotice(host,scope){
 host.querySelector(".chat-delivery-notice")?.remove();
 const pending=chatReadDeliveryOutbox().filter(x=>x.scope===scope||(x.draftScope||x.scope)===scope);if(!pending.length)return;
 const notice=el("div","chat-delivery-notice");notice.setAttribute("role","status");
 pending.forEach(item=>{
  const row=el("div","chat-delivery-row");
  const label=el("span","",(item.accepted?"Sent · draft sync pending: ":"Send not confirmed: ")+String(item.payload.text||"Attachment").slice(0,90));
  const check=el("button","sprt-quiet",item.accepted?"Sync draft":"Check status");
  const retry=el("button","sprt-quiet","Retry same send");
  const navigate=result=>{
   if(result.id&&chatDraftKey===scope&&!chatOpenId)location.hash="#/chat/a/"+encodeURIComponent(item.agent)+"/"+encodeURIComponent(result.id);
   else if(chatDraftKey===scope&&chatOpenId)loadChatSession(chatOpenId);
  };
  check.onclick=async()=>{
   check.disabled=true;
   try{
    if(item.accepted){await chatAcceptDelivery(item,item.accepted);chatRenderDeliveryNotice(host,scope);return;}
    const path=chatIsTerminalDelivery(item)?item.url.replace(/\/input$/,"/delivery"):"/api/agents/chat/"+encodeURIComponent(item.agent)+"/delivery";
    const r=await fetch(path+"?request="+encodeURIComponent(item.payload.requestId));
    if(r.status===404){label.textContent="Not recorded yet. Retry the same send to deliver it.";return;}
    if(!r.ok)throw new Error(await r.text());
    const d=await r.json();
    if(chatIsTerminalDelivery(item)&&d.delivery?.state!=="sent"){label.textContent="Submission is unconfirmed. Check again or inspect the native conversation; this request will not be replayed.";return;}
    await chatAcceptDelivery(item,d);row.remove();navigate(d);
    showToast("Message "+d.delivery.state,null,"info");
   }catch(e){label.textContent="Still unable to confirm delivery. Your message is saved here.";}
   finally{check.disabled=false;}
  };
  retry.onclick=async()=>{
   retry.disabled=true;
   try{const d=await chatDeliverRemembered(item);row.remove();navigate(d);}
   catch(e){
    if(e.rejected){label.textContent="Send rejected: "+e.message+". Your original message remains saved here.";}
    else label.textContent="Delivery remains unconfirmed. Retrying this send is safe.";
   }
   finally{retry.disabled=false;}
  };
  row.append(label,check);if(!item.accepted)row.append(retry);notice.append(row);
 });
 host.prepend(notice);
}

function chatAddSharedTerminal(session,agent){
  const key=agent+"/"+session.id,storage="manifest.sharedTerminal.v1."+key;
  let saved;try{saved=JSON.parse(localStorage.getItem(storage)||"null");}catch(e){}
  reviewDialog("Add coding agent",({body,actions,close})=>{
    body.append(el("p","","This agent joins the shared conversation. The team can read its replies and direct it. It starts working when you send a message."));
    const kind=document.createElement("select");kind.className="pp-in";kind.setAttribute("aria-label","Coding agent");
    Object.entries(chatTermKinds).forEach(([value,label])=>{const o=document.createElement("option");o.value=value;o.textContent=label;kind.append(o);});
    if(saved?.payload?.agent)kind.value=saved.payload.agent;
    const cwd=document.createElement("input"),model=document.createElement("input");cwd.className=model.className="pp-in";
    cwd.value=saved?.payload?.cwd||chatRecall("manifest.chatTermCwd."+kind.value)||"";model.value=saved?.payload?.model||"";
    cwd.placeholder="Default home folder";model.placeholder="Installed default";
    for(const [label,input] of [["Agent",kind],["Working folder on Metis",cwd],["Model (optional)",model]]){const field=el("label","",label);input.setAttribute("aria-label",label);field.append(input);body.append(field);}
    const status=el("p","");status.setAttribute("role","status");body.append(status);
    const add=el("button","sprt-quiet","Add to shared conversation"),cancel=el("button","sprt-quiet","Cancel");cancel.onclick=close;actions.append(cancel,add);
    add.onclick=async()=>{
      const payload={agent:kind.value,backend:"terminal",mode:"continue",title:saved?.payload?.title||session.title||"Shared conversation",cwd:cwd.value.trim(),model:model.value.trim()};
      const signature=JSON.stringify(payload),requestId=saved?.signature===signature?saved.requestId:crypto.randomUUID();
      try{
        saved={payload,signature,requestId};localStorage.setItem(storage,JSON.stringify(saved));add.disabled=true;
        const result=await postJSONOk(chatBaseFor(agent)+"/"+encodeURIComponent(session.id)+"/related",{...payload,requestId});
        if(!result.id)throw Error("Creation was not confirmed. Retry to recover the same session.");
        localStorage.removeItem(storage);chatRecipients.set(key,{backend:"terminal",agent:result.agent,id:result.id,model:result.model});close();
        if(chatAgent===agent&&chatOpenId===session.id){chatCaptureSyncedDraft(key);await refetchChatSession(session.id);}
      }catch(e){status.textContent=e.message||"Could not add the agent. Retry safely.";}finally{add.disabled=false;}
    };
  });
}

function chatStartRelated(source,targetAgent){
  const originAgent=source.agent,originID=source.id;
  const storageKey="manifest.relatedDraft.v1."+(source.backend?source.backend+"/":"")+originAgent+"/"+originID;
  let remembered=null;try{remembered=JSON.parse(localStorage.getItem(storageKey)||"null");}catch(e){}
  const selected=chatArtifactSelections.get("chat:"+originAgent+"/"+originID);
  const excerpt=source.handoffExcerpt??parseChatTurns(source.handoffBody||"").slice(-2).map(t=>t.who+":\n"+t.text.slice(0,2000)+(t.text.length>2000?"\n[excerpt shortened]":"")).join("\n\n");
  reviewDialog("Start related chat",({body,actions,close})=>{
    const agents=chatRoster.filter(a=>a.enabled&&a.durableSend);
    if(chatTermEnabled)Object.entries(chatTermKinds).forEach(([name,label])=>agents.push({name:"terminal:"+name,label}));
    const pick=document.createElement("select");pick.className="pp-in";
    agents.forEach(a=>{const o=document.createElement("option");o.value=a.name;o.textContent=a.label;pick.append(o);});
    const desired=remembered?.agent?(remembered.backend==="terminal"?"terminal:":"")+remembered.agent:targetAgent||originAgent;
    pick.value=agents.some(a=>a.name===desired)?desired:agents[0]?.name||"";pick.setAttribute("aria-label","Agent for related chat");
    const title=document.createElement("input");title.className="pp-in";title.value=remembered?.title||(source.title||"Related chat").slice(0,240);title.setAttribute("aria-label","Related chat title");
    const prompt=document.createElement("textarea");prompt.className="pp-in";prompt.setAttribute("aria-label","Handoff draft");
    prompt.value=remembered?.prompt??("Continue work related to “"+source.title+"”.\n\nRecent excerpt from "+chatAgentLabel(originAgent)+" (not the full history):\n\n"+excerpt);
    const field=(name,input)=>{const label=el("label","",name);label.append(input);return label;};
    body.append(el("p","","This creates a separate linked chat. Review the handoff there before sending; current work keeps running."),field("Agent",pick),field("Title",title),field("Handoff draft",prompt));
    const cwd=document.createElement("input");cwd.className="pp-in";cwd.setAttribute("aria-label","Coding working folder");cwd.placeholder="Default home folder";
    cwd.value=remembered?.cwd||"";
    const model=document.createElement("input");model.className="pp-in";model.setAttribute("aria-label","Coding model");model.placeholder="Installed default";model.value=remembered?.model||"";
    const codingFields=el("div","");codingFields.append(field("Working folder on metis",cwd),field("Model (optional)",model));body.append(codingFields);
    const syncCoding=()=>{codingFields.hidden=!pick.value.startsWith("terminal:");if(!codingFields.hidden&&!cwd.value)cwd.value=chatRecall("manifest.chatTermCwd."+pick.value.slice(9))||"";};pick.onchange=syncCoding;syncCoding();
    const ref=remembered?.artifacts?.[0]||selected;
    if(ref)body.append(el("p","","Includes the selected artifact version as context for the next send."));
    const status=el("p","");status.setAttribute("role","status");body.append(status);
    const cancel=el("button","sprt-quiet","Cancel"),create=el("button","sprt-quiet","Create related chat");cancel.onclick=close;
    create.onclick=async()=>{
      const coding=pick.value.startsWith("terminal:");
      const payload={agent:coding?pick.value.slice(9):pick.value,title:title.value,prompt:prompt.value,task:remembered?.task||selected?.task||source.task||"",artifacts:ref?[{id:ref.id,revision:ref.revision}]:[],...(coding?{backend:"terminal",cwd:cwd.value,model:model.value}:{})};
      const signature=JSON.stringify(payload);
      const requestId=remembered?.signature===signature?remembered.requestId:crypto.randomUUID();
      remembered={...payload,signature,requestId};
      try{
        localStorage.setItem(storageKey,JSON.stringify(remembered));create.disabled=true;
        const endpoint=source.backend==="terminal"?"/api/terminal/"+encodeURIComponent(originAgent)+"/session/"+encodeURIComponent(originID)+"/related":chatBaseFor(originAgent)+"/"+encodeURIComponent(originID)+"/related";
        const result=await postJSONOk(endpoint,{...payload,requestId});
        if(!result.id)throw new Error("Creation was not confirmed. Retry to check the same request.");
        localStorage.removeItem(storageKey);close();
        location.hash=result.conversation?.route||"#/chat/a/"+encodeURIComponent(result.agent)+"/"+encodeURIComponent(result.id);
      }catch(e){status.textContent=e.message||"Could not create the related chat. Retry safely.";}
      finally{create.disabled=false;}
    };
    actions.append(cancel,create);
  });
}

function chatChooseTerminalRecipient(source){
  const se=source.se,key=se.kind+"/"+se.id,route=chatRouteVersion,current=chatRecipients.get(key);
  const tasks=(source.conversation?.links||[]).filter(l=>l.kind==="task"),task=tasks.length===1?tasks[0].id:"";
  reviewDialog("Choose agent",({body,actions,close})=>{
    const pick=document.createElement("select");pick.className="pp-in";pick.setAttribute("aria-label","Next message recipient");
    const native=document.createElement("option");native.value="native";native.textContent=chatAgentLabel(se.kind)+(se.model?" · "+shortModel(se.model):"");pick.append(native);
    chatRoster.filter(a=>a.enabled&&a.durableSend).forEach(a=>{const option=document.createElement("option");option.value=a.name;option.textContent=a.label+(a.model?" · "+shortModel(a.model):"");pick.append(option);});
    if(chatTermEnabled)Object.entries(chatTermKinds).forEach(([kind,label])=>{const option=document.createElement("option");option.value="terminal:"+kind;option.textContent=label+" · separate runtime";pick.append(option);});
    pick.value=current?.backend==="terminal"?"terminal:"+current.agent:current?.backend==="hermes"?current.agent:"native";
    body.append(el("p","","Continue in this conversation with its attributed history, or start a separate related chat. This does not interrupt work already running."),pick);
    const cwd=document.createElement("input");cwd.className="pp-in";cwd.setAttribute("aria-label","Continuation working folder");
    const model=document.createElement("input");model.className="pp-in";model.setAttribute("aria-label","Continuation coding model");model.placeholder="Installed default";
    const fields=el("div","");const folderLabel=el("label","","Working folder on metis"),modelLabel=el("label","","Model (optional)");folderLabel.append(cwd);modelLabel.append(model);fields.append(folderLabel,modelLabel);body.append(fields);
    const sync=()=>{fields.hidden=!pick.value.startsWith("terminal:");const prior=(source.codingRecipients||[]).filter(p=>p.agent===pick.value.slice(9)).at(-1);cwd.value=prior?.cwd||se.cwd||"";model.value=prior?.model||"";};pick.onchange=sync;sync();
    const status=el("p","");status.setAttribute("role","status");body.append(status);
    const here=el("button","sprt-quiet","Continue here"),cancel=el("button","sprt-quiet","Cancel"),related=el("button","sprt-quiet","Start related chat");
    cancel.onclick=close;
    here.onclick=async()=>{
      here.disabled=true;
      try{
        let recipient=null;
        if(pick.value!=="native"){
          const coding=pick.value.startsWith("terminal:"),agent=coding?pick.value.slice(9):pick.value,entry=chatRosterEntry(agent);
          let child=coding?(source.codingRecipients||[]).find(p=>p.agent===agent&&p.cwd===cwd.value.trim()&&(!model.value.trim()||p.model===model.value.trim())):(source.planningRecipients||[]).find(p=>p.agent===agent);
          if(!child){
            const payload={agent,model:coding?model.value.trim():entry?.model||"",mode:"continue",title:se.name||se.kind,task,...(coding?{backend:"terminal",cwd:cwd.value.trim()}:{})};
            const storageKey="manifest.nativeContinue.v1."+key,signature=JSON.stringify(payload);
            let saved;try{saved=JSON.parse(localStorage.getItem(storageKey)||"null");}catch(e){}
            const requestId=saved?.signature===signature?saved.requestId:crypto.randomUUID();
            localStorage.setItem(storageKey,JSON.stringify({signature,requestId}));
            child=await postJSONOk("/api/terminal/"+encodeURIComponent(se.kind)+"/session/"+encodeURIComponent(se.id)+"/related",{...payload,requestId});
            if(!coding)child.model=payload.model;localStorage.removeItem(storageKey);
          }
          recipient={backend:coding?"terminal":"hermes",agent,id:child.id,model:child.model||entry?.model||""};
        }
        if(recipient)chatRecipients.set(key,recipient);else chatRecipients.delete(key);
        if(route===chatRouteVersion&&chatDraftKey===key){chatCaptureSyncedDraft(key);close();await chatTermRequestFinalTail(source);chatTermRepaintHead();renderChatComposer(chatTermComposerSession());}
        else{const state=chatSyncedDrafts.get(key);if(state)state.set({...state.value,recipient});close();}
      }catch(e){status.textContent=e.message||"Could not choose this agent.";}
      finally{here.disabled=false;}
    };
    related.onclick=()=>{const chosen=pick.value;close();chatStartRelated({backend:"terminal",agent:se.kind,id:se.id,title:se.name||se.kind},chosen==="native"?"terminal:"+se.kind:chosen);};
    actions.append(cancel,here,related);
  });
}
function chatChooseRecipient(source){
  const key=source.agent+"/"+source.id;
  const current=chatRecipients.get(key)||{agent:source.agent,model:source.model||""};
  const route=chatRouteVersion;
  reviewDialog("Choose agent",({body,actions,close})=>{
    const pick=document.createElement("select");pick.className="pp-in";pick.setAttribute("aria-label","Next message recipient");
    chatRoster.filter(a=>a.enabled&&a.durableSend).forEach(a=>{const o=document.createElement("option");o.value=a.name;o.textContent=a.label+(a.model?" · "+shortModel(a.model):"");pick.append(o);});
    if(chatTermEnabled)Object.entries(chatTermKinds).forEach(([kind,label])=>{const o=document.createElement("option");o.value="terminal:"+kind;o.textContent=label;pick.append(o);});
    pick.value=(current.backend==="terminal"?"terminal:":"")+current.agent;
    body.append(el("p","","Continue in this conversation or open a separate linked chat. Already accepted work keeps its recipient."),pick,
      el("p","","Continuing sends recent conversation history and your selected artifact versions with the next message. Older history may be omitted to fit the model; replies retain their authors."));
    const cwd=document.createElement("input");cwd.className="pp-in";cwd.setAttribute("aria-label","Continuation working folder");cwd.placeholder="Default home folder";
    const model=document.createElement("input");model.className="pp-in";model.setAttribute("aria-label","Continuation coding model");model.placeholder="Installed default";
    const fields=el("div","");const folderLabel=el("label","","Working folder on metis"),modelLabel=el("label","","Model (optional)");folderLabel.append(cwd);modelLabel.append(model);fields.append(folderLabel,modelLabel);body.append(fields);
    const sync=()=>{fields.hidden=!pick.value.startsWith("terminal:");const existing=(source.continuations||[]).filter(v=>v.agent===pick.value.slice(9)).at(-1);cwd.value=existing?.cwd||"";model.value=existing?.model||"";};pick.onchange=sync;sync();
    const status=el("p","");status.setAttribute("role","status");body.append(status);
    const cancel=el("button","sprt-quiet","Cancel"),here=el("button","sprt-quiet","Continue here"),related=el("button","sprt-quiet","Start related chat");
    cancel.onclick=close;
    here.onclick=async()=>{
      here.disabled=true;
      try{
        const chosen=pick.value;let recipient;
        if(chosen.startsWith("terminal:")){
          const kind=chosen.slice(9);
          let existing=(source.continuations||[]).filter(v=>v.agent===kind&&(!model.value||v.model===model.value)&&(!cwd.value||v.cwd===cwd.value)).at(-1);
          if(!existing){
            const payload={backend:"terminal",mode:"continue",agent:kind,title:source.title,model:model.value,cwd:cwd.value,task:source.task||""};
            const storageKey="manifest.continueDraft.v1."+key,signature=JSON.stringify(payload);
            let saved=null;try{saved=JSON.parse(localStorage.getItem(storageKey)||"null");}catch(e){}
            const requestId=saved?.signature===signature?saved.requestId:crypto.randomUUID();
            localStorage.setItem(storageKey,JSON.stringify({signature,requestId}));
            existing=await postJSONOk(chatBaseFor(source.agent)+"/"+encodeURIComponent(source.id)+"/related",{...payload,requestId});
            localStorage.removeItem(storageKey);
          }
          recipient={backend:"terminal",agent:kind,id:existing.id,model:existing.model};
        }else{const entry=chatRosterEntry(chosen);recipient={agent:chosen,model:chosen===current.agent?current.model:(entry?.model||"")};}
        chatRecipients.set(key,recipient);
        if(route===chatRouteVersion&&chatDraftKey===key){chatCaptureSyncedDraft(key);close();chatRepaintHead();await refetchChatSession(source.id);}
        else {const state=chatSyncedDrafts.get(key);if(state)state.set({...state.value,recipient});close();}
      }catch(e){status.textContent=e.message||"Could not select this recipient.";}
      finally{here.disabled=false;}
    };
    related.onclick=()=>{const chosen=pick.value;close();chatStartRelated(source,chosen);};
    actions.append(cancel,here,related);
  });
}
