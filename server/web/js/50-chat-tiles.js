// CHAT TILES — several whole conversations side by side, tiled the way a
// tiling window manager (Omarchy/Hyprland "dwindle") tiles windows.
//
// Route: #/chat/tiles. Every tile is one complete conversation in its own
// isolated document (the same ?chatPane=1 frame side chats use), so each keeps
// its own composer, draft, outbox, approvals and agent. Tiles never share
// chat globals and never send anything themselves.
//
// Layout is a binary tree per workspace (1–9). A new tile splits the focused
// tile along its longer side; closing a tile gives its space to its sibling.
// Frames are positioned, never re-parented, so moving, swapping, resizing or
// switching workspace never reloads a conversation.
//
// State (tree, routes, focus, workspace) is owner UI state in the existing
// chatstate store (inbox/tiles), so the arrangement follows the owner across
// devices; it is view state only and cannot execute anything.
//
// Keys (Alt = Option on a Mac), from the page or from inside any tile:
//   Alt+Enter new tile · Alt+W close · Alt+←↑↓→ / Alt+H J K L focus
//   Alt+Shift+direction swap · Alt+T flip split · Alt+F full screen
//   Alt+= / Alt+- grow / shrink · Alt+1…9 workspace · Alt+Shift+1…9 move tile
//   Alt+O open focused conversation in the full view · Alt+/ keys
//   Alt+N next tile that needs you (then errors, then finished)
const chatTilesMax = 6;          // live frames per workspace; each is a full app
const chatTilesGap = 8;          // px between tiles (Omarchy gaps_in)
const chatTilesState = {v:1, active:"1", ws:{}, focus:{}, full:{}, tiles:{}};
let chatTilesRoot = null, chatTilesStore = null, chatTilesFrames = new Map(), chatTilesTimer = 0, chatTilesShown = false, chatTilesSeq = 0;

function chatTilesId() { return "t" + Date.now().toString(36) + (chatTilesSeq++).toString(36); }
function chatTilesLeaves(node, out = []) {
  if (!node) return out;
  if (node.t === "leaf") out.push(node.id); else { chatTilesLeaves(node.a, out); chatTilesLeaves(node.b, out); }
  return out;
}
function chatTilesTree(ws = chatTilesState.active) { return chatTilesState.ws[ws] || null; }
function chatTilesFocused(ws = chatTilesState.active) {
  const ids = chatTilesLeaves(chatTilesTree(ws));
  return ids.includes(chatTilesState.focus[ws]) ? chatTilesState.focus[ws] : ids[0] || "";
}
function chatTilesParentOf(node, id, parent = null) {
  if (!node) return null;
  if (node.t === "leaf") return node.id === id ? {node, parent} : null;
  return chatTilesParentOf(node.a, id, node) || chatTilesParentOf(node.b, id, node);
}

// ---- persistence ----
function chatTilesSnapshot() {
  // Only the arrangement is saved: tile ids, routes, titles and the trees.
  const tiles = {};
  for (const ws of Object.keys(chatTilesState.ws)) for (const id of chatTilesLeaves(chatTilesState.ws[ws])) if (chatTilesState.tiles[id]) tiles[id] = chatTilesState.tiles[id];
  return {v:1, active:chatTilesState.active, ws:chatTilesState.ws, focus:chatTilesState.focus, full:chatTilesState.full, tiles};
}
function chatTilesSave() {
  if (!chatTilesStore) return;
  chatTilesStore.set(JSON.parse(JSON.stringify(chatTilesSnapshot())));
}
function chatTilesValid(v) {
  return !!v && typeof v === "object" && v.v === 1 && v.ws && typeof v.ws === "object" && v.tiles && typeof v.tiles === "object";
}
function chatTilesAdopt(v) {
  if (!chatTilesValid(v)) return;
  const tiles = {};
  for (const [id, t] of Object.entries(v.tiles)) if (/^t[a-z0-9]{1,24}$/.test(id) && t && (t.route === "" || /^#\/chat\//.test(t.route || ""))) tiles[id] = {route:t.route || "", title:String(t.title || "").slice(0, 200)};
  const ws = {};
  const clean = node => {
    if (!node || typeof node !== "object") return null;
    if (node.t === "leaf") return tiles[node.id] ? {t:"leaf", id:node.id} : null;
    const a = clean(node.a), b = clean(node.b);
    if (!a || !b) return a || b;
    const ratio = Number(node.ratio);
    return {t:"split", dir:node.dir === "col" ? "col" : "row", ratio:ratio > 0.1 && ratio < 0.9 ? ratio : 0.5, a, b};
  };
  for (const [k, tree] of Object.entries(v.ws)) if (/^[1-9]$/.test(k)) { const c = clean(tree); if (c) ws[k] = c; }
  chatTilesState.ws = ws; chatTilesState.tiles = tiles;
  chatTilesState.focus = {...(v.focus || {})}; chatTilesState.full = {...(v.full || {})};
  chatTilesState.active = /^[1-9]$/.test(v.active) ? v.active : "1";
}
async function chatTilesLoad() {
  if (chatTilesStore || typeof ChatDraftState === "undefined") return;
  // Another device's arrangement wins a conflict: layout is last-writer-wins
  // view state, the same rule the workspace tabs follow.
  chatTilesStore = new ChatDraftState("inbox", (state, apply) => {
    if (state.conflict) { state.resolve(true); return; }
    if (apply && chatTilesShown && state.value && !chatStateEqual(state.value, chatTilesSnapshot())) { chatTilesAdopt(state.value); chatTilesPaint(); }
  }, "tiles");
  if (chatTilesStore.value) chatTilesAdopt(chatTilesStore.value);
  await chatTilesStore.refresh();
  if (chatTilesStore.value) chatTilesAdopt(chatTilesStore.value);
}

// ---- conversations the launcher offers ----
function chatTilesEntryRoute(entry) {
  const se = entry.session;
  if (entry.taskThread) return "#/chat/task/" + encodeURIComponent(se.id);
  if (entry.terminal) return "#/chat/a/" + encodeURIComponent(se.kind || entry.agent) + "/" + encodeURIComponent(se.id);
  if (entry.agent) return "#/chat/a/" + encodeURIComponent(entry.agent) + "/" + encodeURIComponent(se.id);
  return "#/chat/" + encodeURIComponent(se.id);
}
function chatTilesEntries() {
  // Every active conversation, independent of the rail's current filters.
  const entries = (typeof chatSessions !== "undefined" ? chatSessions : []).map(session => ({agent:"", session}));
  chatRoster.filter(a => !chatIsTerm(a.name)).forEach(agent => (chatAgentSessions[agent.name] || []).forEach(session => entries.push({agent:agent.name, session})));
  if (chatTermEnabled) Object.keys(chatTermKinds).forEach(agent => chatTermList(agent).forEach(session => entries.push({agent, session, terminal:true})));
  (chatTaskThreads || []).forEach(t => entries.push(chatTaskEntry(t)));
  const time = e => Date.parse(e.session.updated || e.session.lastUsed || e.session.created || "") || 0;
  return entries.filter(e => chatEntryLifecycle(e) === "active").sort((a, b) => time(b) - time(a));
}
function chatTilesEntryFor(route) {
  if (!route) return null;
  return chatTilesEntries().find(e => chatTilesEntryRoute(e) === route) || null;
}
function chatTilesNewChoices() {
  return [
    ...chatRoster.filter(a => a.enabled && !chatIsTerm(a.name) && chatPrivateCreationAgent(a.name) === a.name).map(a => ({agent:a.name, label:a.label, model:a.model || ""})),
    ...(chatTermEnabled ? Object.entries(chatTermKinds).map(([agent, label]) => ({agent, label, model:""})) : []),
  ];
}

// ---- tree operations ----
function chatTilesAdd(route = "", title = "") {
  const ws = chatTilesState.active, tree = chatTilesTree(ws);
  const count = chatTilesLeaves(tree).length;
  if (count >= chatTilesMax) { chatTilesNotice("This workspace holds " + chatTilesMax + " tiles. Use another workspace (Alt+2…9)."); return ""; }
  const id = chatTilesId();
  chatTilesState.tiles[id] = {route, title};
  const leaf = {t:"leaf", id};
  if (!tree) chatTilesState.ws[ws] = leaf;
  else {
    // dwindle: split the focused tile along its longer side
    const focused = chatTilesFocused(ws), found = chatTilesParentOf(tree, focused);
    const rect = chatTilesFrames.get(focused)?.box.getBoundingClientRect();
    const dir = rect && rect.height > rect.width * 1.1 ? "col" : "row";
    const split = {t:"split", dir, ratio:0.5, a:{t:"leaf", id:focused}, b:leaf};
    if (!found || !found.parent) chatTilesState.ws[ws] = split;
    else if (found.parent.a === found.node) found.parent.a = split; else found.parent.b = split;
  }
  chatTilesState.focus[ws] = id; chatTilesState.full[ws] = false;
  chatTilesSave(); chatTilesPaint();
  return id;
}
function chatTilesRemoveFrom(ws, id) {
  const tree = chatTilesTree(ws), found = chatTilesParentOf(tree, id);
  if (!found) return;
  if (!found.parent) delete chatTilesState.ws[ws];
  else {
    const sibling = found.parent.a === found.node ? found.parent.b : found.parent.a;
    const grand = chatTilesGrandparent(tree, found.parent);
    if (!grand) chatTilesState.ws[ws] = sibling;
    else if (grand.a === found.parent) grand.a = sibling; else grand.b = sibling;
  }
}
function chatTilesGrandparent(node, target) {
  if (!node || node.t === "leaf") return null;
  if (node.a === target || node.b === target) return node;
  return chatTilesGrandparent(node.a, target) || chatTilesGrandparent(node.b, target);
}
function chatTilesClose(id = chatTilesFocused()) {
  if (!id) return;
  const ws = chatTilesState.active;
  const order = chatTilesLeaves(chatTilesTree(ws)), at = order.indexOf(id);
  chatTilesRemoveFrom(ws, id);
  delete chatTilesState.tiles[id];
  const rest = chatTilesLeaves(chatTilesTree(ws));
  chatTilesState.focus[ws] = rest[Math.max(0, at - 1)] || rest[0] || "";
  // A closed tile only leaves the layout: its conversation, draft and runs
  // remain where they always were.
  const frame = chatTilesFrames.get(id);
  if (frame) { frame.box.remove(); chatTilesFrames.delete(id); }
  chatTilesSave(); chatTilesPaint();
}
function chatTilesMove(id, target) {
  const from = chatTilesState.active;
  if (!id || target === from) return;
  if (chatTilesLeaves(chatTilesTree(target)).length >= chatTilesMax) { chatTilesNotice("Workspace " + target + " is full."); return; }
  chatTilesRemoveFrom(from, id);
  const rest = chatTilesLeaves(chatTilesTree(from));
  chatTilesState.focus[from] = rest[0] || "";
  const tree = chatTilesTree(target), leaf = {t:"leaf", id};
  chatTilesState.ws[target] = tree ? {t:"split", dir:"row", ratio:0.5, a:tree, b:leaf} : leaf;
  chatTilesState.focus[target] = id;
  chatTilesSave(); chatTilesPaint();
}
function chatTilesNeighbor(dir) {
  const id = chatTilesFocused(), me = chatTilesFrames.get(id)?.rect;
  if (!me) return "";
  const cx = me.x + me.w / 2, cy = me.y + me.h / 2;
  let best = "", bestScore = Infinity;
  for (const other of chatTilesLeaves(chatTilesTree())) {
    if (other === id) continue;
    const r = chatTilesFrames.get(other)?.rect; if (!r) continue;
    const ox = r.x + r.w / 2, oy = r.y + r.h / 2;
    const ahead = dir === "left" ? r.x + r.w <= me.x + 1 : dir === "right" ? r.x >= me.x + me.w - 1 : dir === "up" ? r.y + r.h <= me.y + 1 : r.y >= me.y + me.h - 1;
    if (!ahead) continue;
    const along = dir === "left" || dir === "right" ? Math.abs(ox - cx) : Math.abs(oy - cy);
    const across = dir === "left" || dir === "right" ? Math.abs(oy - cy) : Math.abs(ox - cx);
    const score = along + across * 2;
    if (score < bestScore) { bestScore = score; best = other; }
  }
  return best;
}
function chatTilesFocus(id, {frame = true} = {}) {
  if (!id) return;
  chatTilesState.focus[chatTilesState.active] = id;
  chatTilesMarkFocus();
  if (frame) chatTilesFocusFrame(id);
  chatTilesSave();
}
function chatTilesFocusFrame(id) {
  const t = chatTilesFrames.get(id);
  if (!t) return;
  if (t.frame && !t.frame._wired) {
    // a frame still loading has no key wiring yet: keys pressed now would be
    // lost inside it. Hold focus on the tile (the manager's document, where
    // the tiling keys work) and move in once the frame is wired.
    t.frame._focusOnWire = true;
    if (!t.box.hasAttribute("tabindex")) t.box.tabIndex = -1;
    t.box.focus({preventScroll: true});
  } else if (t.frame) {
    t.frame.focus();
    try { t.frame.contentDocument?.querySelector("#chatComposer textarea:not([disabled])")?.focus({preventScroll:true}); } catch (e) {}
  } else t.box.querySelector("input,button")?.focus();
}
function chatTilesSwap(dir) {
  const id = chatTilesFocused(), other = chatTilesNeighbor(dir);
  if (!other) return;
  const tree = chatTilesTree(), a = chatTilesParentOf(tree, id).node, b = chatTilesParentOf(tree, other).node;
  a.id = other; b.id = id;
  chatTilesSave(); chatTilesPaint();
}
function chatTilesFlip() {
  const found = chatTilesParentOf(chatTilesTree(), chatTilesFocused());
  if (!found?.parent) return;
  found.parent.dir = found.parent.dir === "row" ? "col" : "row";
  chatTilesSave(); chatTilesPaint();
}
function chatTilesResize(delta) {
  const found = chatTilesParentOf(chatTilesTree(), chatTilesFocused());
  if (!found?.parent) return;
  const p = found.parent, grow = p.a === found.node ? delta : -delta;
  p.ratio = Math.min(0.85, Math.max(0.15, p.ratio + grow));
  chatTilesSave(); chatTilesLayout();
}
function chatTilesWorkspace(ws) {
  if (!/^[1-9]$/.test(ws) || ws === chatTilesState.active) return;
  chatTilesState.active = ws;
  chatTilesSave(); chatTilesPaint();
  const id = chatTilesFocused();
  if (id) chatTilesFocusFrame(id);
}
function chatTilesToggleFull() {
  const ws = chatTilesState.active;
  chatTilesState.full[ws] = !chatTilesState.full[ws];
  chatTilesSave(); chatTilesLayout();
}

// ---- keys ----
const chatTilesDirs = {ArrowLeft:"left", KeyH:"left", ArrowRight:"right", KeyL:"right", ArrowUp:"up", KeyK:"up", ArrowDown:"down", KeyJ:"down"};
function chatTilesKey(e) {
  if (!chatTilesShown || !e.altKey || e.ctrlKey || e.metaKey || e.isComposing) return false;
  const code = e.code, shift = e.shiftKey;
  const digit = /^Digit([1-9])$/.exec(code);
  if (code === "Enter" && !shift) chatTilesAdd();
  else if (code === "KeyW" && !shift) chatTilesClose();
  else if (chatTilesDirs[code]) { if (shift) chatTilesSwap(chatTilesDirs[code]); else chatTilesFocus(chatTilesNeighbor(chatTilesDirs[code])); }
  else if (code === "KeyT" && !shift) chatTilesFlip();
  else if (code === "KeyF" && !shift) chatTilesToggleFull();
  else if ((code === "Equal" || code === "NumpadAdd") && !shift) chatTilesResize(0.05);
  else if ((code === "Minus" || code === "NumpadSubtract") && !shift) chatTilesResize(-0.05);
  else if (digit) { if (shift) chatTilesMove(chatTilesFocused(), digit[1]); else chatTilesWorkspace(digit[1]); }
  else if (code === "KeyO" && !shift) { const t = chatTilesState.tiles[chatTilesFocused()]; if (t?.route) location.hash = t.route; }
  else if (code === "Slash") chatTilesHelp();
  else if (code === "KeyN" && !shift) chatTilesNextAttention();
  else return false;
  e.preventDefault(); e.stopPropagation();
  return true;
}
document.addEventListener("keydown", e => { chatTilesKey(e); }, true);

// ---- painting ----
function chatTilesMount() {
  if (chatTilesRoot) return chatTilesRoot;
  const view = document.getElementById("chatView");
  if (!view) return null;
  const root = el("div", "chat-tiles"); root.id = "chatTiles"; root.hidden = true;
  const bar = el("div", "chat-tiles-bar"); bar.setAttribute("role", "toolbar"); bar.setAttribute("aria-label", "Tiles");
  const spaces = el("div", "chat-tiles-spaces"); spaces.setAttribute("role", "tablist"); spaces.setAttribute("aria-label", "Workspaces");
  for (let n = 1; n <= 9; n++) {
    const b = el("button", "chat-tiles-space", String(n)); b.type = "button"; b.dataset.ws = String(n);
    b.setAttribute("role", "tab"); b.title = "Workspace " + n + " · Alt+" + n; b.setAttribute("aria-keyshortcuts", "Alt+" + n);
    b.onclick = () => chatTilesWorkspace(String(n));
    spaces.append(b);
  }
  const add = el("button", "chat-tiles-add", "New tile"); add.type = "button"; add.title = "Split the focused tile · Alt+Enter"; add.setAttribute("aria-keyshortcuts", "Alt+Enter");
  add.onclick = () => chatTilesAdd();
  const full = el("button", "chat-tiles-full", "Full screen"); full.type = "button"; full.title = "Show only the focused tile · Alt+F"; full.setAttribute("aria-keyshortcuts", "Alt+F");
  full.onclick = () => chatTilesToggleFull();
  const keys = el("button", "chat-tiles-keys", "Keys"); keys.type = "button"; keys.title = "Tiling keys · Alt+/"; keys.onclick = () => chatTilesHelp();
  const exit = el("a", "chat-tiles-exit", "Single chat"); exit.href = "#/chat"; exit.title = "Back to one conversation with the list";
  const attn = el("button", "chat-tiles-attn"); attn.type = "button"; attn.hidden = true; attn.setAttribute("aria-keyshortcuts", "Alt+N");
  attn.onclick = () => chatTilesNextAttention();
  const notify = el("button", "chat-tiles-notify", "Notify me"); notify.type = "button"; notify.hidden = true;
  notify.title = "Show a browser notification when a tile you are not looking at finishes or needs you";
  notify.onclick = async () => { try { await Notification.requestPermission(); } catch (e) {} chatTilesAttentionPaint(); };
  const note = el("span", "chat-tiles-note"); note.setAttribute("role", "status"); note.setAttribute("aria-live", "polite");
  const tabs = el("div", "chat-tiles-tabs"); tabs.setAttribute("role", "tablist"); tabs.setAttribute("aria-label", "Tiles in this workspace");
  bar.append(spaces, attn, notify, add, full, keys, note, exit, tabs);
  const stage = el("div", "chat-tiles-stage");
  root.append(bar, stage);
  view.append(root);
  chatTilesRoot = root;
  new ResizeObserver(() => chatTilesLayout()).observe(stage);
  return root;
}
function chatTilesNotice(text) {
  const note = chatTilesRoot?.querySelector(".chat-tiles-note");
  if (!note) return;
  note.textContent = text;
  clearTimeout(note._t); note._t = setTimeout(() => { note.textContent = ""; }, 5000);
}
function chatTilesHelp() {
  const rows = [["Alt+Enter", "New tile (splits the focused one)"], ["Alt+W", "Close tile (the conversation stays)"], ["Alt+← ↑ ↓ → or H J K L", "Focus a neighbour"], ["Alt+Shift+direction", "Swap with a neighbour"], ["Alt+T", "Flip the split"], ["Alt+F", "Full screen"], ["Alt+= / Alt+-", "Grow / shrink"], ["Alt+1…9", "Switch workspace"], ["Alt+Shift+1…9", "Move tile to workspace"], ["Alt+O", "Open in the single-chat view"], ["Alt+N", "Next tile that needs you, then errors, then finished"]];
  reviewDialog("Tiling keys", ({body, actions, close}) => {
    const list = el("dl", "chat-tiles-help");
    for (const [k, v] of rows) list.append(el("dt", "", k), el("dd", "", v));
    body.append(list, el("p", "chat-workspace-hint", "Alt is Option on a Mac. Keys work from inside any tile."));
    const ok = el("button", "sprt-ghost", "Close"); ok.onclick = close; actions.append(ok);
  });
}
function chatTilesTileEl(id) {
  let t = chatTilesFrames.get(id);
  if (t) return t;
  const box = el("section", "chat-tile"); box.dataset.tile = id;
  const head = el("header", "chat-tile-head");
  const dot = el("span", "chat-tile-dot"); dot.setAttribute("aria-hidden", "true");
  const title = el("span", "chat-tile-title", "New tile");
  const state = el("span", "chat-tile-state");
  const open = el("a", "chat-tile-act", "↗"); open.title = "Open in the single-chat view · Alt+O"; open.setAttribute("aria-label", "Open in the single-chat view");
  const close = el("button", "chat-tile-act", "×"); close.type = "button"; close.title = "Close tile · Alt+W"; close.setAttribute("aria-label", "Close tile");
  close.onclick = e => { e.stopPropagation(); chatTilesClose(id); };
  head.append(dot, title, state, open, close);
  const body = el("div", "chat-tile-body");
  box.append(head, body);
  box.addEventListener("pointerdown", () => { if (chatTilesFocused() !== id) chatTilesFocus(id, {frame:false}); });
  box.addEventListener("focusin", () => { if (chatTilesFocused() !== id) chatTilesFocus(id, {frame:false}); });
  chatTilesRoot.querySelector(".chat-tiles-stage").append(box);
  t = {box, head, title, state, dot, open, body, frame:null, route:null, rect:null};
  chatTilesFrames.set(id, t);
  return t;
}
function chatTilesMountBody(id) {
  const t = chatTilesTileEl(id), spec = chatTilesState.tiles[id] || {route:""};
  if (t.route === spec.route && t.body.childElementCount) return;
  t.route = spec.route;
  t.open.href = spec.route || "#/chat";
  t.open.hidden = !spec.route;
  if (!spec.route) { t.frame = null; t.body.replaceChildren(chatTilesLauncher(id)); return; }
  // The frame's own navigation (a new chat becoming its session) is reflected
  // back here, so the saved arrangement names the conversation, not "new".
  if (t.frame && t.frame.isConnected) {
    try { if (t.frame.contentWindow.location.hash !== spec.route) t.frame.contentWindow.location.hash = spec.route; return; } catch (e) {}
  }
  const frame = document.createElement("iframe");
  frame.className = "chat-tile-frame"; frame.title = "Conversation tile";
  frame.src = location.pathname + "?chatPane=1&tile=1" + spec.route;
  frame.addEventListener("load", () => chatTilesWireFrame(id, frame));
  t.frame = frame;
  t.body.replaceChildren(frame);
}
function chatTilesWireFrame(id, frame) {
  let win;
  try { win = frame.contentWindow; win.document; } catch (e) { return; }
  // Tiling keys reach the manager from inside the conversation too.
  // a (re)loaded frame starts out assuming it is shown and focused
  const t = chatTilesFrames.get(id); if (t) { t.paneShown = undefined; t.paneFocused = undefined; chatTilesNotifyPanes(); }
  win.addEventListener("keydown", e => { chatTilesKey(e); }, true);
  frame._wired = true;
  if (frame._focusOnWire) { frame._focusOnWire = false; if (chatTilesFocused() === id && document.activeElement === chatTilesFrames.get(id)?.box) chatTilesFocusFrame(id); }
  win.addEventListener("focus", () => { if (chatTilesFocused() !== id) chatTilesFocus(id, {frame:false}); });
  win.document.addEventListener("pointerdown", () => { if (chatTilesFocused() !== id) chatTilesFocus(id, {frame:false}); }, true);
  win.addEventListener("hashchange", () => {
    const route = win.location.hash, tile = chatTilesState.tiles[id];
    if (!tile || !/^#\/chat\//.test(route) || route === tile.route) return;
    // the old title named the previous route ("New · Codex"); the entry or the
    // frame's own head names the conversation it became
    tile.route = route; tile.title = ""; const t = chatTilesFrames.get(id); if (t) { t.route = route; t.open.href = route; t.open.hidden = false; }
    chatTilesSave(); chatTilesHeads();
  });
}
function chatTilesLauncher(id) {
  const box = el("div", "chat-tile-launcher");
  const search = el("input", "chat-conversation-search"); search.type = "search"; search.placeholder = "Open a conversation…"; search.setAttribute("aria-label", "Find a conversation for this tile");
  const fresh = el("div", "chat-tile-new");
  fresh.append(el("span", "chat-tile-new-label", "New chat with"));
  for (const c of chatTilesNewChoices()) {
    const b = el("button", "chat-tile-new-choice", c.label); b.type = "button";
    if (c.model) b.title = c.label + " · " + shortModel(c.model);
    b.onclick = () => chatTilesSetRoute(id, "#/chat/a/" + encodeURIComponent(c.agent) + "/new", "New · " + c.label);
    fresh.append(b);
  }
  const list = el("div", "chat-conversation-list");
  const taken = new Set(Object.values(chatTilesState.tiles).map(t => t.route));
  const paint = () => {
    list.replaceChildren();
    const q = search.value.trim().toLowerCase();
    for (const entry of chatTilesEntries()) {
      const se = entry.session, route = chatTilesEntryRoute(entry), title = se.title || se.name || "Conversation";
      const agent = entry.terminal ? chatTermKinds[se.kind] || se.kind : entry.taskThread ? "Task" : entry.agent ? chatAgentLabel(entry.agent) : "Spirits";
      if (!(title + " " + agent).toLowerCase().includes(q)) continue;
      const b = el("button", "chat-conversation-choice"); b.type = "button";
      const st = chatEntryState(entry);
      b.append(el("span", "chat-conversation-name", title), el("span", "chat-conversation-agent", agent + " · " + st.label + (taken.has(route) ? " · already tiled" : "")));
      b.onclick = () => chatTilesSetRoute(id, route, title);
      list.append(b);
      if (list.childElementCount >= 60) break;
    }
    if (!list.childElementCount) list.append(el("p", "chat-workspace-hint", q ? "No matching conversations." : "No conversations yet. Start one above."));
  };
  search.addEventListener("input", paint);
  search.addEventListener("keydown", e => { if (e.key === "Enter") { const first = list.querySelector("button"); if (first) { e.preventDefault(); first.click(); } } });
  box.append(fresh, search, list);
  paint();
  return box;
}
function chatTilesSetRoute(id, route, title) {
  const tile = chatTilesState.tiles[id]; if (!tile) return;
  tile.route = route; tile.title = title || "";
  chatTilesSave(); chatTilesPaint(); chatTilesFocusFrame(id);
}
function chatTilesRects(node, x, y, w, h, out) {
  if (!node) return out;
  if (node.t === "leaf") { out[node.id] = {x, y, w, h}; return out; }
  const g = chatTilesGap;
  if (node.dir === "row") {
    const aw = Math.round((w - g) * node.ratio);
    chatTilesRects(node.a, x, y, aw, h, out); chatTilesRects(node.b, x + aw + g, y, w - aw - g, h, out);
    out["gutter:" + chatTilesGutterKey(node)] = {x:x + aw, y, w:g, h, node};
  } else {
    const ah = Math.round((h - g) * node.ratio);
    chatTilesRects(node.a, x, y, w, ah, out); chatTilesRects(node.b, x, y + ah + g, w, h - ah - g, out);
    out["gutter:" + chatTilesGutterKey(node)] = {x, y:y + ah, w, h:g, node};
  }
  return out;
}
function chatTilesGutterKey(node) { return chatTilesLeaves(node.a)[0] + "|" + chatTilesLeaves(node.b)[0]; }
function chatTilesNarrow() { return window.matchMedia("(max-width: 860px)").matches; }
function chatTilesFit() {
  if (!chatTilesRoot || !chatTilesShown) return;
  const top = chatTilesRoot.getBoundingClientRect().top;
  const h = Math.max(260, (window.visualViewport?.height || window.innerHeight) - top - 14) + "px";
  if (chatTilesRoot.style.height !== h) chatTilesRoot.style.height = h;
}
window.addEventListener("resize", () => { chatTilesFit(); chatTilesLayout(); });
function chatTilesLayout() {
  if (!chatTilesRoot || !chatTilesShown) return;
  chatTilesFit();
  const stage = chatTilesRoot.querySelector(".chat-tiles-stage");
  const W = stage.clientWidth, H = stage.clientHeight, ws = chatTilesState.active;
  const tree = chatTilesTree(ws), focused = chatTilesFocused(ws);
  // Full screen, and every phone-width view, shows the focused tile alone;
  // the others stay alive, hidden, with their drafts and streams intact.
  const mono = !!chatTilesState.full[ws] || chatTilesNarrow();
  const rects = mono ? (focused ? {[focused]:{x:0, y:0, w:W, h:H}} : {}) : chatTilesRects(tree, 0, 0, W, H, {});
  for (const [id, t] of chatTilesFrames) {
    const r = rects[id];
    t.rect = r || null;
    t.box.hidden = !r;
    if (r) Object.assign(t.box.style, {left:r.x + "px", top:r.y + "px", width:r.w + "px", height:r.h + "px"});
  }
  stage.querySelectorAll(".chat-tiles-gutter").forEach(g => { if (!rects["gutter:" + g.dataset.key]) g.remove(); });
  for (const [key, r] of Object.entries(rects)) {
    if (!key.startsWith("gutter:")) continue;
    const k = key.slice(7);
    let g = stage.querySelector('.chat-tiles-gutter[data-key="' + CSS.escape(k) + '"]');
    if (!g) { g = el("div", "chat-tiles-gutter"); g.dataset.key = k; g.setAttribute("aria-hidden", "true"); stage.append(g); }
    g.dataset.dir = r.node.dir; g._node = r.node;
    Object.assign(g.style, {left:r.x + "px", top:r.y + "px", width:r.w + "px", height:r.h + "px"});
    g.onpointerdown = e => chatTilesDrag(e, g);
  }
  chatTilesRoot.classList.toggle("is-mono", mono);
  chatTilesNotifyPanes();
  chatTilesRoot.querySelector(".chat-tiles-full")?.setAttribute("aria-pressed", String(!!chatTilesState.full[ws]));
}
function chatTilesDrag(e, gutter) {
  const node = gutter._node; if (!node) return;
  e.preventDefault();
  const stage = chatTilesRoot.querySelector(".chat-tiles-stage");
  const first = chatTilesFrames.get(chatTilesLeaves(node.a)[0])?.rect, last = chatTilesFrames.get(chatTilesLeaves(node.b).at(-1))?.rect;
  // the parent's extent: from its first leaf's origin to its last leaf's far edge
  const a = node.a, b = node.b;
  const rA = chatTilesBounds(a), rB = chatTilesBounds(b);
  if (!rA || !rB || !first || !last) return;
  const start = node.dir === "row" ? rA.x : rA.y, end = node.dir === "row" ? rB.x + rB.w : rB.y + rB.h;
  const origin = stage.getBoundingClientRect();
  chatTilesRoot.classList.add("is-dragging");
  gutter.setPointerCapture(e.pointerId);
  const move = ev => {
    const pos = node.dir === "row" ? ev.clientX - origin.left : ev.clientY - origin.top;
    node.ratio = Math.min(0.85, Math.max(0.15, (pos - start) / Math.max(1, end - start)));
    chatTilesLayout();
  };
  const up = () => { gutter.removeEventListener("pointermove", move); chatTilesRoot.classList.remove("is-dragging"); chatTilesSave(); };
  gutter.addEventListener("pointermove", move);
  gutter.addEventListener("pointerup", up, {once:true});
  gutter.addEventListener("pointercancel", up, {once:true});
}
function chatTilesBounds(node) {
  const rs = chatTilesLeaves(node).map(id => chatTilesFrames.get(id)?.rect).filter(Boolean);
  if (!rs.length) return null;
  const x = Math.min(...rs.map(r => r.x)), y = Math.min(...rs.map(r => r.y));
  return {x, y, w:Math.max(...rs.map(r => r.x + r.w)) - x, h:Math.max(...rs.map(r => r.y + r.h)) - y};
}
function chatTilesMarkFocus() {
  const focused = chatTilesFocused();
  if (chatTilesAttn.get(focused)?.kind === "done" && chatTilesShown) { chatTilesAttn.delete(focused); chatTilesAttentionPaint(); }
  if (chatTilesNarrow()) { chatTilesLayout(); }
  chatTilesRoot?.querySelectorAll(".chat-tiles-tab").forEach((b, i) => b.setAttribute("aria-selected", String(chatTilesLeaves(chatTilesTree())[i] === focused)));
  for (const [id, t] of chatTilesFrames) { t.box.classList.toggle("is-focused", id === focused); t.box.setAttribute("aria-current", id === focused ? "true" : "false"); }
  chatTilesNotifyPanes();
}
// chatTilesNotifyPanes — each frame learns whether it is shown and focused
// (48-chat.js chatPaneShown/chatPaneFocused): a hidden tile makes no poll
// requests, an unfocused one polls at this manager's cadence, and a tile that
// comes into view or takes focus reads its conversation at once ("woke").
function chatTilesNotifyPanes(force) {
  const focused = chatTilesFocused();
  for (const [id, t] of chatTilesFrames) {
    const shown = chatTilesShown && !!t.rect, isFocused = id === focused;
    if (!force && t.paneShown === shown && t.paneFocused === isFocused) continue;
    const woke = shown && t.paneShown === false;
    t.paneShown = shown; t.paneFocused = isFocused;
    try { t.frame?.contentWindow?.postMessage({type:"manifest:pane", shown, focused:isFocused, woke}, location.origin); } catch (e) {}
  }
}
function chatTilesHeads() {
  for (const [id, t] of chatTilesFrames) {
    const spec = chatTilesState.tiles[id]; if (!spec) continue;
    const entry = chatTilesEntryFor(spec.route);
    const st = entry ? chatEntryState(entry) : null;
    let name = entry ? (entry.session.title || entry.session.name || spec.title) : spec.title;
    if (!name && spec.route) { try { name = t.frame?.contentDocument?.querySelector(".chat-head-title")?.textContent || ""; } catch (e) {} }
    const agent = entry ? (entry.terminal ? chatTermKinds[entry.session.kind] || entry.session.kind : entry.taskThread ? "Task" : entry.agent ? chatAgentLabel(entry.agent) : "Spirits") : "";
    t.title.textContent = spec.route ? (name || "Conversation") : "New tile";
    t.state.textContent = [agent, st?.label].filter(Boolean).join(" · ");
    t.dot.dataset.execution = st?.execution || "";
    t.box.setAttribute("aria-label", t.title.textContent + (t.state.textContent ? " · " + t.state.textContent : ""));
    if (t.frame) t.frame.title = "Conversation · " + t.title.textContent;
    if (name && spec.title !== name && spec.route && entry) { spec.title = name; }
  }
  chatTilesTabs();
  chatTilesAttentionUpdate();
}

// ---- attention across tiles (2026-09-27) ----
// Every tile, in every workspace, from the same inbox state the heads show:
// needs you (waiting on the owner) and error (failed / disconnected) hold
// while the state does; done is a run that finished since it was last
// looked at, cleared by focusing the tile. Alt+N (or the bar button) jumps
// to the next one: needs you first, then errors, then done. A tile that is
// not in view announces a new state with a browser notification, only when
// the owner has allowed notifications.
const chatTilesAttn = new Map(), chatTilesExec = new Map();
const chatTilesAttnOrder = {needs: 0, error: 1, done: 2};
function chatTilesWorkspaceOf(id) { return Object.keys(chatTilesState.ws).find(k => chatTilesLeaves(chatTilesState.ws[k]).includes(id)) || ""; }
function chatTilesInView(id) { return chatTilesShown && !document.hidden && chatTilesState.active === chatTilesWorkspaceOf(id) && chatTilesFocused() === id; }
function chatTilesAttentionUpdate() {
  const seen = new Set();
  for (const [id, spec] of Object.entries(chatTilesState.tiles)) {
    if (!spec.route) continue;
    seen.add(id);
    const entry = chatTilesEntryFor(spec.route), st = entry ? chatEntryState(entry) : null, exec = st?.execution || "";
    const prev = chatTilesExec.get(id), known = chatTilesExec.has(id);
    chatTilesExec.set(id, exec);
    const had = chatTilesAttn.get(id);
    let kind = exec === "waiting_user" ? "needs" : exec === "failed" || exec === "disconnected" ? "error" : "";
    if (!kind && had?.kind === "done" && exec !== "running") kind = "done";
    if (!kind && known && prev === "running" && exec !== "running" && exec !== "queued" && !chatTilesInView(id)) kind = "done";
    if (!kind) { chatTilesAttn.delete(id); continue; }
    if (had?.kind === kind) continue;
    const title = (entry && (entry.session.title || entry.session.name)) || spec.title || "Conversation";
    chatTilesAttn.set(id, {kind, since: Date.now(), title, label: st?.label || ""});
    // announce a change, never the state found on load
    if (known && prev !== exec && !chatTilesInView(id)) chatTilesAnnounce(id, kind, title, st?.label || "");
  }
  for (const id of [...chatTilesAttn.keys()]) if (!seen.has(id)) chatTilesAttn.delete(id);
  chatTilesAttentionPaint();
}
function chatTilesAnnounce(id, kind, title, label) {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
  const body = kind === "needs" ? "Needs you" : kind === "error" ? (label || "Something went wrong") : "Finished";
  try {
    const n = new Notification(title, {body, tag: "manifest-tile-" + id});
    n.onclick = () => { window.focus(); chatTilesJumpTo(id); n.close(); };
  } catch (e) {}
}
function chatTilesAttentionList() {
  return [...chatTilesAttn.entries()].sort((a, b) => chatTilesAttnOrder[a[1].kind] - chatTilesAttnOrder[b[1].kind] || a[1].since - b[1].since);
}
function chatTilesAttentionPaint() {
  const root = chatTilesRoot; if (!root) return;
  const attn = root.querySelector(".chat-tiles-attn"), notify = root.querySelector(".chat-tiles-notify");
  const count = k => [...chatTilesAttn.values()].filter(a => a.kind === k).length;
  const parts = [[count("needs"), "need", "needs"], [count("error"), "error", "errors"], [count("done"), "done", "done"]].filter(p => p[0]).map(([n, one, many]) => n + " " + (n === 1 ? (one === "need" ? "needs you" : one) : (many === "needs" ? "need you" : many)));
  if (attn) {
    attn.hidden = !parts.length;
    attn.textContent = parts.join(" · ") + " · Next";
    const next = chatTilesAttentionList()[0];
    attn.title = next ? "Go to " + next[1].title + " · Alt+N" : "";
    attn.dataset.kind = next ? next[1].kind : "";
    attn.setAttribute("aria-label", parts.join(", ") + ". Go to the next · Alt+N");
  }
  if (notify) notify.hidden = typeof Notification === "undefined" || Notification.permission !== "default";
}
function chatTilesJumpTo(id) {
  const ws = chatTilesWorkspaceOf(id); if (!ws) return;
  if (location.hash !== "#/chat/tiles") location.hash = "#/chat/tiles";
  if (chatTilesState.active !== ws) chatTilesWorkspace(ws);
  chatTilesFocus(id);
}
function chatTilesNextAttention() {
  const current = chatTilesFocused(), list = chatTilesAttentionList().filter(([id]) => id !== current);
  if (!list.length) { chatTilesNotice(chatTilesAttn.size ? "Only this tile needs you" : "Nothing needs you right now"); return; }
  chatTilesJumpTo(list[0][0]);
}

function chatTilesPaint() {
  if (!chatTilesRoot) return;
  const ws = chatTilesState.active;
  for (const b of chatTilesRoot.querySelectorAll(".chat-tiles-space")) {
    const n = b.dataset.ws, count = chatTilesLeaves(chatTilesTree(n)).length;
    b.setAttribute("aria-selected", String(n === ws));
    b.classList.toggle("has-tiles", count > 0);
    b.hidden = count === 0 && n !== ws && Number(n) > 1 + Math.max(0, ...Object.keys(chatTilesState.ws).map(Number));
  }
  // frames for every workspace stay mounted; only the active one is shown
  const live = new Set();
  for (const k of Object.keys(chatTilesState.ws)) for (const id of chatTilesLeaves(chatTilesState.ws[k])) live.add(id);
  for (const [id, t] of chatTilesFrames) if (!live.has(id)) { t.box.remove(); chatTilesFrames.delete(id); }
  for (const id of chatTilesLeaves(chatTilesTree(ws))) chatTilesMountBody(id);
  if (!chatTilesTree(ws)) chatTilesEmpty(); else chatTilesRoot.querySelector(".chat-tiles-empty")?.remove();
  chatTilesLayout(); chatTilesMarkFocus(); chatTilesHeads(); chatTilesTabs();
}
// chatTilesTabs — at phone width only the focused tile shows; these name every
// tile in the workspace so another can be chosen without a keyboard.
function chatTilesTabs() {
  const host = chatTilesRoot?.querySelector(".chat-tiles-tabs"); if (!host) return;
  const focused = chatTilesFocused();
  host.replaceChildren();
  for (const id of chatTilesLeaves(chatTilesTree())) {
    const t = chatTilesFrames.get(id), b = el("button", "chat-tiles-tab", t?.title.textContent || "Tile");
    b.type = "button"; b.setAttribute("role", "tab"); b.setAttribute("aria-selected", String(id === focused));
    if (t?.dot.dataset.execution) b.dataset.execution = t.dot.dataset.execution;
    b.onclick = () => { chatTilesFocus(id); chatTilesLayout(); chatTilesTabs(); };
    host.append(b);
  }
}
function chatTilesEmpty() {
  const stage = chatTilesRoot.querySelector(".chat-tiles-stage");
  if (stage.querySelector(".chat-tiles-empty")) return;
  const box = el("div", "chat-tiles-empty");
  box.append(el("h2", "", "Workspace " + chatTilesState.active + " is empty"), el("p", "", "Tile conversations side by side. Each tile is a whole chat with its own composer, model and run."));
  const add = el("button", "sprt-ghost", "New tile · Alt+Enter"); add.type = "button"; add.onclick = () => chatTilesAdd();
  box.append(add);
  stage.append(box);
}

// ---- route entry and exit (48-chat.js showChat) ----
async function chatTilesShow(from) {
  const root = chatTilesMount(); if (!root) return;
  document.getElementById("chatView")?.classList.add("tiles-mode");
  if (typeof chatHeadActionsHome === "function") chatHeadActionsHome();
  root.hidden = false; chatTilesShown = true;
  await chatTilesLoad();
  if (!chatTilesShown) return;
  // Arriving from a conversation (Alt+Enter in the single view, or the Tile
  // button) brings that conversation along as a tile.
  if (from && /^#\/chat\//.test(from) && !Object.values(chatTilesState.tiles).some(t => t.route === from)) {
    const ws = chatTilesState.active;
    if (!chatTilesTree(ws)) { const id = chatTilesId(); chatTilesState.tiles[id] = {route:from, title:""}; chatTilesState.ws[ws] = {t:"leaf", id}; chatTilesState.focus[ws] = id; chatTilesSave(); }
    else { chatTilesPaint(); chatTilesAdd(from, ""); }
  }
  if (!chatRoster.length) { try { await chatLoadInbox(true); } catch (e) {} }
  chatTilesPaint();
  clearInterval(chatTilesTimer);
  chatTilesTimer = setInterval(async () => {
    if (!chatTilesShown || document.hidden) return;
    try { await chatLoadInbox(true); } catch (e) {}
    chatTilesHeads();
  }, 8000);
  const id = chatTilesFocused(); if (id) setTimeout(() => chatTilesFocusFrame(id), 60);
}
function chatTilesHide() {
  if (!chatTilesShown) return;
  chatTilesShown = false; clearInterval(chatTilesTimer);
  chatTilesNotifyPanes();
  document.getElementById("chatView")?.classList.remove("tiles-mode");
  if (typeof chatHeadActionsHome === "function") chatHeadActionsHome();
  if (chatTilesRoot) chatTilesRoot.hidden = true;
  chatTilesStore?.flush();
}
// From the single view: Alt+Enter tiles the open conversation.
document.addEventListener("keydown", e => {
  if (chatTilesShown || chatEmbedded || !e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || e.code !== "Enter") return;
  if (!/^#\/chat(\/|$)/.test(location.hash)) return;
  e.preventDefault();
  const current = /^#\/chat\/(a\/[^/]+\/[^/]+|task\/.+|[^/]+)$/.test(location.hash) && !/\/new$/.test(location.hash) && !/^#\/chat\/(spirits|new|tiles|project\/)/.test(location.hash) ? location.hash : "";
  chatTilesPendingFrom = current;
  location.hash = "#/chat/tiles";
});
let chatTilesPendingFrom = "";
function chatTilesTakePending() { const f = chatTilesPendingFrom; chatTilesPendingFrom = ""; return f; }
window.addEventListener("pagehide", () => chatTilesStore?.flush());
