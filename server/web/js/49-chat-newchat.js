// NEW CHAT — one composer-first flow (owner, 2026-09-27). "New chat" and
// Ctrl+Alt+N open the agent's landing; everything a new conversation needs is
// a chip in the composer: agent, model · effort (49-chat-models.js), project,
// and for a coding agent the folder it starts in (recent folders and the
// repos under ~/src, never free text). Recent chats sit under the
// composer. Nothing is created until the first message is sent;
// the landing's draft slot and the project assignment are the ones the
// landing always used (chatPrepareLandingDraft, chatPendingProject).

// chatNewChat — the one way to start: this agent's landing, or the last
// agent a chat was started with.
function chatNewChat() {
  // the rail's project filter, when one is chosen, is the new chat's project
  chatPendingProject = chatWorkstreams.groups[chatWorkstreamFilter] ? chatWorkstreamFilter : "";
  let agent = chatAgent ? chatPrivateCreationAgent(chatAgent) : chatRecall("manifest.chatNewAgent");
  if (agent && !chatIsTerm(agent) && !chatRosterEntry(agent)?.enabled) agent = "";
  if (!agent) agent = chatRoster.find(a => a.enabled && !chatIsTerm(a.name))?.name || "";
  if (agent && chatIsTerm(agent) && chatPendingProject) chatUseProjectFolder(chatPendingProject, agent);
  const hash = agent ? "#/chat/a/" + encodeURIComponent(agent) + "/new" : "#/chat/new";
  if (location.hash === hash) { chatLandingRefreshChips(); focusChatInput(); return; }
  location.hash = hash;
}

function chatOnLanding() { return !chatOpenId && !chatTaskID && /\/new$|^#\/chat\/new$/.test(location.hash); }

// ---- a small menu anchored to a chip ----
let chatChipMenuEl = null;
function chatCloseChipMenu(refocus = true) {
  const box = chatChipMenuEl; if (!box) return;
  chatChipMenuEl = null; box.remove();
  box._chip?.setAttribute("aria-expanded", "false");
  if (refocus && box._chip?.isConnected) box._chip.focus({preventScroll: true});
}
// items: [{label, sub?, selected?, run}] (run may be async; a rejection is
// shown in the menu and keeps it open)
function chatChipMenu(chip, name, items, footer) {
  const was = chatChipMenuEl?._chip === chip;
  chatCloseChipMenu(false);
  if (was) return;
  const composer = document.getElementById("chatComposer"); if (!composer) return;
  const box = el("div", "chat-chip-menu"); box.setAttribute("role", "dialog"); box.setAttribute("aria-label", name);
  box._chip = chip; chip.setAttribute("aria-expanded", "true");
  const list = el("div", "chat-chip-list"); list.setAttribute("role", "listbox"); list.setAttribute("aria-label", name);
  const status = el("p", "chat-chip-status"); status.setAttribute("role", "status");
  const rows = items.map((item, i) => {
    if (item.group) { const g = el("div", "chat-chip-group micro-label", item.group); list.append(g); return null; }
    const row = el("button", "chat-chip-row"); row.type = "button"; row.setAttribute("role", "option"); row.setAttribute("aria-selected", String(!!item.selected));
    row.append(el("span", "chat-chip-row-label", item.label));
    if (item.sub) row.append(el("span", "chat-chip-row-sub", item.sub));
    row.onclick = async () => {
      row.disabled = true; status.textContent = "";
      try { await item.run(); if (chatChipMenuEl === box) chatCloseChipMenu(); }
      catch (e) { status.textContent = e.message || "Not changed."; }
      finally { row.disabled = false; }
    };
    list.append(row);
    return row;
  }).filter(Boolean);
  box.append(list, status);
  if (footer) box.append(footer);
  composer.append(box);
  chatChipMenuEl = box;
  if (!window.matchMedia("(max-width: 860px)").matches) {
    const c = composer.getBoundingClientRect(), w = Math.min(420, c.width), at = chip.getBoundingClientRect().left - c.left;
    box.style.left = Math.max(0, Math.min(at, c.width - w)) + "px";
  }
  box.addEventListener("keydown", e => {
    const i = rows.indexOf(document.activeElement);
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); chatCloseChipMenu(); }
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); rows[(i + (e.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length]?.focus(); }
  });
  (rows.find(r => r.getAttribute("aria-selected") === "true") || rows[0])?.focus({preventScroll: true});
}
document.addEventListener("pointerdown", e => {
  const box = chatChipMenuEl; if (!box) return;
  if (!box.contains(e.target) && e.target !== box._chip && !box._chip?.contains(e.target)) chatCloseChipMenu(false);
}, true);

// ---- the landing chips ----
// Built once per composer and updated in place (a repaint never replaces a
// chip under the pointer); removed once a conversation exists.
function chatLandingChips(host) {
  const on = chatOnLanding() && !chatIsPortal();
  const kinds = ["agent", "project", "folder"];
  if (!on) { kinds.forEach(k => host.querySelector(".chat-landing-chip-" + k)?.remove()); return; }
  const chip = (kind, before) => {
    let b = host.querySelector(".chat-landing-chip-" + kind);
    if (!b) {
      b = el("button", "sprt-quiet chat-landing-chip chat-landing-chip-" + kind); b.type = "button"; b.setAttribute("aria-haspopup", "dialog"); b.setAttribute("aria-expanded", "false");
      const anchor = before && host.querySelector(before);
      anchor ? host.insertBefore(b, anchor) : host.append(b);
    }
    return b;
  };
  // agent
  const agent = chip("agent", ".chat-composer-model");
  const agentName = chatAgent ? (chatIsTerm() ? chatTermKinds[chatAgent] : chatAgentLabel(chatAgent)) : "Spirits";
  agent.textContent = agentName + " ⌄"; agent.title = "Agent: " + agentName + " · choose who this chat is with";
  agent.setAttribute("aria-label", "Agent: " + agentName);
  agent.onclick = () => chatChipMenu(agent, "Agent", chatLandingAgentItems());
  // project (one that no longer exists is not silently kept)
  if (chatPendingProject && !chatWorkstreams.groups[chatPendingProject]) chatPendingProject = "";
  const project = chip("project");
  const projectName = chatWorkstreams.groups[chatPendingProject] || "";
  project.textContent = (projectName || "No project") + " ⌄";
  project.title = projectName ? "Project: " + projectName + " · the new chat is filed under it" : "No project · a standalone chat";
  project.setAttribute("aria-label", "Project: " + (projectName || "none"));
  project.onclick = () => chatLandingProjectMenu(project);
  // folder (coding agents)
  if (chatIsTerm()) {
    const folder = chip("folder");
    const cwd = chatRecall("manifest.chatTermCwd." + chatAgent);
    folder.textContent = (cwd ? cwd.split("/").filter(Boolean).at(-1) : "Home folder") + " ⌄";
    folder.title = "Starts in " + (cwd || "the home folder") + " · choose a folder";
    folder.setAttribute("aria-label", "Folder: " + (cwd || "home folder"));
    folder.onclick = () => chatLandingFolderMenu(folder);
  } else host.querySelector(".chat-landing-chip-folder")?.remove();
}

function chatLandingAgentItems() {
  const items = [];
  const hermes = chatRoster.filter(a => a.enabled && !chatIsTerm(a.name) && a.backend !== "terminal" && chatPrivateCreationAgent(a.name) === a.name);
  for (const a of hermes) items.push({label: a.label, sub: [a.model ? shortModel(a.model) : "", a.backend === "portal" ? "shared with the team portal" : ""].filter(Boolean).join(" · "), selected: chatAgent === a.name, run: () => chatLandingSwitchAgent(a.name)});
  if (chatTermEnabled) for (const [kind, label] of Object.entries(chatTermKinds)) items.push({label, sub: "coding session in a folder", selected: chatAgent === kind, run: () => chatLandingSwitchAgent(kind)});
  items.push({label: "Spirits", sub: "your chattable spirits", selected: !chatAgent, run: () => chatLandingSwitchAgent("")});
  return items;
}

// Switching agent keeps what was typed: it moves from this landing's draft
// slot to the next one (the old slot is cleared, never left as a duplicate).
let chatLandingCarry = "";
function chatLandingSwitchAgent(agent) {
  const ta = document.querySelector("#chatComposer textarea");
  if (ta && ta.value.trim()) {
    chatLandingCarry = ta.value;
    ta.value = ""; if (typeof chatCaptureSyncedDraft === "function" && chatDraftKey) chatCaptureSyncedDraft(chatDraftKey);
  }
  if (agent && chatIsTerm(agent) && chatPendingProject) chatUseProjectFolder(chatPendingProject, agent);
  try { localStorage.setItem("manifest.chatNewAgent", agent); } catch (e) {}
  location.hash = agent ? "#/chat/a/" + encodeURIComponent(agent) + "/new" : "#/chat/new";
}
// called once the new landing's composer (and its draft) is in place
function chatLandingTakeCarry() {
  if (!chatLandingCarry || !chatOnLanding()) return;
  const ta = document.querySelector("#chatComposer textarea"); if (!ta) return;
  if (!ta.value.trim()) { ta.value = chatLandingCarry; ta.dispatchEvent(new Event("input", {bubbles: true})); if (typeof chatCaptureSyncedDraft === "function" && chatDraftKey) chatCaptureSyncedDraft(chatDraftKey); }
  chatLandingCarry = "";
}

function chatLandingProjectMenu(chip) {
  const items = [{label: "No project", sub: "a standalone chat", selected: !chatPendingProject, run: () => chatLandingSetProject("")}];
  for (const [id, label] of chatProjectOptions()) items.push({label, sub: id.startsWith("folder:") ? "folder " + id.slice(13) : "", selected: chatPendingProject === id, run: () => chatLandingSetProject(id)});
  let footer = null;
  if (chatPendingProject && chatWorkstreams.groups[chatPendingProject] && typeof chatEditProject === "function") {
    footer = el("button", "sprt-quiet chat-chip-foot", "Review project context"); footer.type = "button";
    footer.onclick = () => { chatCloseChipMenu(false); chatEditProject(chatPendingProject); };
  }
  chatChipMenu(chip, "Project", items, footer);
}
async function chatLandingSetProject(selected) {
  // a rail folder becomes a project the first time it is chosen (CAS-safe,
  // chatResolveProject); a coding agent then starts in that folder
  chatPendingProject = await chatResolveProject(selected);
  chatUseProjectFolder(chatPendingProject);
  chatLandingRefreshChips();
}

let chatFoldersCache = null, chatFoldersAt = 0;
async function chatLoadFolders() {
  if (chatFoldersCache && Date.now() - chatFoldersAt < 30000) return chatFoldersCache;
  const r = await fetch("/api/terminal/folders", {cache: "no-store"});
  if (!r.ok) throw Error("Folders are unavailable.");
  chatFoldersCache = await r.json(); chatFoldersAt = Date.now();
  return chatFoldersCache;
}
async function chatLandingFolderMenu(chip) {
  let f;
  try { f = await chatLoadFolders(); } catch (e) { showToast(e.message); return; }
  const kind = chatAgent, current = chatRecall("manifest.chatTermCwd." + kind);
  const set = cwd => { try { localStorage.setItem("manifest.chatTermCwd." + kind, cwd); } catch (e) {} chatLandingRefreshChips(); };
  const row = (path, sub) => ({label: path.split("/").filter(Boolean).at(-1) || path, sub: sub || path, selected: current === path, run: () => set(path)});
  const items = [{label: "Home folder", sub: f.home || "~", selected: !current, run: () => set("")}];
  const recent = (f.recent || []).filter(p => p !== f.home);
  if (recent.length) items.push({group: "Recent"}, ...recent.map(p => row(p)));
  const repos = (f.repos || []).filter(p => !recent.includes(p));
  if (repos.length) items.push({group: "Repositories in ~/src"}, ...repos.map(p => row(p)));
  if (current && current !== f.home && !recent.includes(current) && !repos.includes(current)) items.splice(1, 0, row(current, current + " · chosen earlier"));
  chatChipMenu(chip, "Folder", items);
}
function chatLandingRefreshChips() {
  const host = document.getElementById("chatComposer");
  if (host) { chatLandingChips(host); if (typeof chatModelChips === "function") chatModelChips(host); }
}

// ---- under the composer: recent chats ----
function chatLandingBelow() {
  const main = document.querySelector(".chat-main"), composer = document.getElementById("chatComposer");
  let below = document.getElementById("chatLandingBelow");
  if (!main || !composer || !chatOnLanding() || chatIsPortal() || !chatAgent) { below?.remove(); return; }
  if (!below) { below = el("div", "chat-landing-below"); below.id = "chatLandingBelow"; }
  if (below.previousElementSibling !== composer) composer.after(below);
  // built once per landing: a composer repaint never replaces a recent row
  // under the pointer
  if (below.dataset.route === location.hash && below.childElementCount) return;
  below.dataset.route = location.hash;
  below.replaceChildren();
  const recent = (typeof chatInboxEntries === "function" ? chatInboxEntries() : []).filter(e => !e.taskThread && (e.agent === chatAgent)).slice(0, 5);
  if (recent.length) {
    const list = el("nav", "chat-landing-recent"); list.setAttribute("aria-label", "Recent chats");
    list.append(el("div", "chat-landing-recent-head micro-label", "Recent"));
    for (const entry of recent) {
      const se = entry.session, a = el("a", "chat-landing-recent-row");
      a.href = entry.terminal ? "#/chat/a/" + encodeURIComponent(se.kind || entry.agent) + "/" + encodeURIComponent(se.id) : "#/chat/a/" + encodeURIComponent(entry.agent) + "/" + encodeURIComponent(se.id);
      a.append(el("span", "chat-landing-recent-title", se.title || se.name || "Conversation"), el("span", "chat-landing-recent-when", fmtWhen(se.updated || se.lastUsed || se.created)));
      list.append(a);
    }
    below.append(list);
  }
}
