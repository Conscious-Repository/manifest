// ---- CHAT · Now: what needs the owner, over the inbox the rail holds (2026-10-09) ----
// Three phone affordances read ONE projection of the conversations the rail
// already loaded. Nothing here writes, stores or runs a timer: the chat
// refresh lifecycle (renderChatInboxRows, chatMountHeader) repaints them, and
// a pressed row prefetches its thread the way a rail row does.
//   · Now, atop the phone Chats list: Waiting on you · Working · Ready to
//     review · Pinned. Each row says its state in chatEntryState's own words,
//     a next action or what the run is doing, and when it last moved.
//   · The stream switcher: an open conversation's title is a button that
//     lists the same rows in the shared bottom sheet (Needs you · Working ·
//     Pinned · View all chats). Opening it is not a route change and marks
//     nothing read; the phone's Back closes it, as Back closes the Chats list.
//   · ‹ Chats counts the OTHER conversations waiting on the owner or ready.
// Pinned is the owner's existing pin preference (the inbox pins slot), read
// here and never written. Above 860px nothing here paints: the desktop rail
// and head are unchanged.

// ---- the projection (reads the rail's state, writes nothing) ----

// chatNowCategory — where a row files; the first match wins. The owner is
// needed (input, an approval, a failed or uncertain run), work is in flight,
// something waits to be looked at, or the owner pinned it. Idle, draft and
// unknown rows have nothing to say here.
function chatNowCategory(state, pinned, unread) {
  if (["waiting_user", "failed", "disconnected"].includes(state.execution)) return "waiting";
  if (["running", "queued"].includes(state.execution)) return "working";
  if ((state.review.ready || 0) > 0 || (state.execution === "completed" && unread)) return "ready";
  return pinned ? "pinned" : "";
}
// chatNowClip — someone's words on one line: whitespace folded, cut at a word
function chatNowClip(text, max = 80) {
  const s = String(text || "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s;
  const cut = s.slice(0, max), space = cut.lastIndexOf(" ");
  return (space > max * 0.6 ? cut.slice(0, space) : cut).trimEnd() + "…";
}
function chatNowQuote(text) { return "“" + chatNowClip(text) + "”"; }
// chatNowSummary — the next action when the owner is needed, else what the
// run is doing or last did, from the delivery, review and task records
function chatNowSummary(entry, state, category) {
  const s = entry.session, review = state.review || {};
  const sent = (s.deliveries || []).filter((d) => d.state !== "cancelled");
  const latest = sent.at(-1), running = sent.find((d) => d.state === "running"), queued = sent.find((d) => d.state === "queued");
  const count = (n, one, many) => n + " " + (n === 1 ? one : many);
  let text;
  if (category === "waiting") {
    if (state.execution === "waiting_user") text = entry.taskThread ? (s.taskState === "plan-ready" ? "Review the plan" : "Review the proposal") : "Answer its prompt";
    else if (state.execution === "failed") text = latest?.state === "failed" && latest.error ? chatNowClip(latest.error) : "See what failed";
    else text = "Check whether it finished";
  } else if (category === "working") {
    if (entry.terminal) text = "In " + chatTermFolder(s);
    else if (entry.taskThread) text = s.lastText ? chatNowClip(s.lastText) : "Agent turn in progress";
    else if (state.label === "Interruption requested") text = "Stop requested" + (running?.text ? " · " + chatNowQuote(running.text) : "");
    else if (state.execution === "queued") text = "Accepted, not started" + ((queued || latest)?.text ? " · " + chatNowQuote((queued || latest).text) : "");
    else text = running?.text ? chatNowQuote(running.text) : "Run in progress";
  } else if (category === "ready") {
    text = review.ready ? "Review " + count(review.ready, "output", "outputs") : "New since you last looked";
  } else if (entry.terminal) text = "In " + chatTermFolder(s);
  else if (entry.taskThread) text = s.lastText ? chatNowClip(s.lastText) : "Open to continue";
  else text = latest?.text ? "Last sent " + chatNowQuote(latest.text) : "Open to continue";
  if (review.changes) text += " · " + count(review.changes, "needs revision", "need revision");
  return text;
}
// chatNowCurrent — the conversation on stage; a task stage names only its task
function chatNowCurrent(entry) {
  if (chatTaskID) return !!entry.taskThread && entry.session.id === chatTaskID;
  return !entry.taskThread && entry.agent === chatAgent && entry.session.id === chatOpenId;
}
// chatNowRows — every active conversation that belongs in Now, as display
// rows: by category, newest first within one
function chatNowRows(entries) {
  const order = { waiting: 0, working: 1, ready: 2, pinned: 3 }, rows = [];
  for (const entry of entries || chatInboxAllEntries()) {
    if (chatEntryLifecycle(entry) !== "active") continue;
    const key = chatInboxKey(entry), state = chatEntryState(entry), seen = chatSeen[key];
    const category = chatNowCategory(state, chatPins[key] === true, !!seen && seen.marker !== chatActivityMarker(entry.session));
    if (!category) continue;
    const s = entry.session, at = s.updated || s.lastUsed || s.created || "";
    rows.push({ entry, key, state, category, at, when: fmtWhen(at), route: chatEntryRoute(entry), current: chatNowCurrent(entry),
      title: entry.terminal ? s.name || s.kind || s.id : s.title || s.name || s.id, agent: chatEntryAgentLabel(entry), summary: chatNowSummary(entry, state, category) });
  }
  const time = (row) => Date.parse(row.at) || 0;
  return rows.sort((a, b) => order[a.category] - order[b.category] || time(b) - time(a));
}
// the rail shows every category; the switcher folds waiting and ready into
// "Needs you" and leaves out the conversation it was opened from
const chatNowSchemes = {
  rail: [["waiting", "Waiting on you", ["waiting"]], ["working", "Working", ["working"]], ["ready", "Ready to review", ["ready"]], ["pinned", "Pinned", ["pinned"]]],
  switcher: [["needs", "Needs you", ["waiting", "ready"]], ["working", "Working", ["working"]], ["pinned", "Pinned", ["pinned"]]],
};
function chatNowGroups(rows, scheme) {
  const offered = scheme === "switcher" ? rows.filter((r) => !r.current) : rows;
  return chatNowSchemes[scheme].map(([id, label, categories]) => ({ id, label, rows: offered.filter((r) => categories.includes(r.category)) })).filter((g) => g.rows.length);
}
// chatNowAttention — ‹ Chats's count: the other conversations that wait on
// the owner or are ready. Streaming and typing never change a category.
function chatNowAttention(rows) { return rows.filter((r) => !r.current && (r.category === "waiting" || r.category === "ready")).length; }
// ---- end of the projection ----

const chatNowSheetKey = "chat-streams";
let chatNowPendingRoute = "";   // a switch waiting for the sheet's history entry to pop
let chatNowFocusTitle = null;   // {route, until}: a switch hands focus to that conversation's title
function chatNowPhone() { return !!window.mf?.phone?.() && !(typeof chatEmbedded !== "undefined" && chatEmbedded); }
// chatNowUnsure — Now speaks only for the list it has: before the inbox first
// loads, or after a refresh failed, it says so instead of "nothing waits"
function chatNowUnsure() { return chatInboxLoadFailed ? "Couldn't refresh chats · this is the last list" : !chatInboxAt ? "Chats are still loading" : ""; }

// chatNowRowEl — one row: title and time, the state and who, then the next
// action or what it is doing; a link to the conversation's own route
function chatNowRowEl(row, cls) {
  const a = el("a", cls + " chat-now-item");
  a.href = row.route;
  a.dataset.inboxKey = row.key;
  a.dataset.execution = row.state.execution;
  if (row.current) a.setAttribute("aria-current", "page");
  a.setAttribute("aria-label", [row.title, row.state.label, row.agent, row.summary, row.when && "updated " + row.when].filter(Boolean).join(", "));
  const when = el("time", "chat-now-when", row.when);
  if (Date.parse(row.at)) { when.dateTime = row.at; when.title = new Date(row.at).toLocaleString(); }
  const top = el("span", "chat-now-top"), meta = el("span", "chat-now-meta"), state = el("span", "chat-now-state", row.state.label);
  if (row.state.evidence) state.title = row.state.evidence;
  top.append(el("span", "chat-now-title", row.title), when);
  meta.append(state, el("span", "chat-now-agent", row.agent));
  a.append(top, meta, el("span", "chat-now-summary", row.summary));
  // the press starts the thread's fetch, so the switch paints from memory
  a.addEventListener("pointerdown", () => chatPrefetchEntry(row.entry), { passive: true });
  return a;
}
function chatNowGroupEl(group, prefix, rowCls) {
  const section = el("section", prefix + "-group"), label = el("h3", "micro-label " + prefix + "-label"), list = el("ul", prefix + "-list");
  section.dataset.now = group.id;
  label.id = prefix + "-" + group.id;
  label.append(group.label + " ", el("span", prefix + "-count", String(group.rows.length)));
  list.setAttribute("aria-labelledby", label.id);
  for (const row of group.rows) { const item = el("li"); item.append(chatNowRowEl(row, rowCls)); list.append(item); }
  section.append(label, list);
  return section;
}

// chatNowPaint — Now above the phone Chats list (renderChatInboxRows calls it
// after every list paint). Search, a filter, Archived or Trash narrow the
// list, so Now steps aside for them.
function chatNowPaint() {
  const list = document.getElementById("chatInboxRows");
  let host = document.getElementById("chatNow");
  const on = chatNowPhone() && list?.parentElement?.id === "chatRail" && chatLifecycleFilter === "active" && !chatSearchQuery.trim()
    && chatInboxFilter === "all" && chatWorkstreamFilter === "all" && chatAttentionFilter === "all";
  const rows = chatNowRows();
  if (!on) { if (host) { host.hidden = true; host.replaceChildren(); } chatNowSyncBack(rows); return; }
  if (!host) { host = el("section", "chat-now"); host.id = "chatNow"; host.setAttribute("aria-labelledby", "chatNowHead"); list.before(host); }
  // a focused row keeps focus through the repaint even when its state words change
  const focused = host.contains(document.activeElement) ? document.activeElement.closest?.(".chat-now-row")?.dataset.inboxKey || "" : "";
  const head = el("h2", "chat-project-section-label chat-now-head", "Now"), unsure = chatNowUnsure(), groups = chatNowGroups(rows, "rail");
  head.id = "chatNowHead";
  host.replaceChildren(head);
  if (unsure) host.append(el("p", "chat-now-note", unsure));
  if (!groups.length && !unsure) host.append(el("p", "chat-now-empty", "Nothing is waiting on you or working right now. Pinned chats show here too."));
  for (const group of groups) host.append(chatNowGroupEl(group, "chat-now", "chat-now-row"));
  host.hidden = false;
  if (focused) [...host.querySelectorAll(".chat-now-row")].find((row) => row.dataset.inboxKey === focused)?.focus({ preventScroll: true });
  chatNowSyncBack(rows);
}

// chatNowBackCount — ‹ Chats stays one action; a calm count of the other
// conversations that need the owner joins it, and its name says so in full
function chatNowBackCount(back, rows) {
  const n = chatNowAttention(rows || chatNowRows());
  let badge = back.querySelector(".mf-chat-back-count");
  if (!n) { badge?.remove(); back.setAttribute("aria-label", "Back to chats"); back.title = "Back to chats"; return; }
  if (!badge) { badge = el("span", "mf-chat-back-count"); badge.setAttribute("aria-hidden", "true"); back.append(badge); }
  badge.textContent = String(n);
  const label = "Back to chats, " + n + (n === 1 ? " other chat needs you" : " other chats need you");
  back.setAttribute("aria-label", label);
  back.title = label;
}
function chatNowSyncBack(rows) {
  const backs = document.querySelectorAll("#chatThreadHeader .mf-chat-back");
  if (!backs.length) return;
  rows = rows || chatNowRows();
  backs.forEach((back) => chatNowBackCount(back, rows));
}

// chatNowTitleSwitch — on a phone the conversation title opens the switcher
// (chatMountHeader calls it for every head). The title element itself stays,
// so Rename still swaps it for its input; a desktop head keeps a plain title.
// Who the conversation is with (an agent head's sub line) rides under the
// title inside the button: the head keeps its four places (‹ · title · ＋ ·
// ···) and the sub is no longer squeezed to a letter a line (it measured
// 7×84px at 320 under a long title before this pass).
function chatNowTitleSwitch(head) {
  const title = head.querySelector(":scope > .chat-head-title");
  if (!title || !chatNowPhone() || title.querySelector(".mf-stream-switch")) return;
  const text = title.textContent.trim(), who = head.querySelector(":scope > .chat-head-sub")?.textContent.trim() || "";
  const button = el("button", "mf-stream-switch"), words = el("span", "mf-stream-switch-text");
  button.type = "button";
  words.append(el("span", "mf-stream-switch-label", text));
  if (who) words.append(el("span", "mf-stream-switch-sub", who));
  button.append(words);
  button.setAttribute("aria-haspopup", "dialog");
  button.setAttribute("aria-expanded", String(typeof mfSheet !== "undefined" && mfSheet.openKey() === chatNowSheetKey));
  button.setAttribute("aria-label", [text, who, "switch chat"].filter(Boolean).join(", "));
  button.title = "Switch chat";
  button.addEventListener("click", (e) => { e.stopPropagation(); chatNowOpenSwitcher(); });
  title.replaceChildren(button);
  const handoff = chatNowFocusTitle;
  if (handoff && handoff.route === location.hash && Date.now() < handoff.until) { chatNowFocusTitle = null; queueMicrotask(() => { if (button.isConnected) button.focus({ preventScroll: true }); }); }
}
function chatNowTitlePlain() {
  document.querySelectorAll("#chatThreadHeader .chat-head-title > .mf-stream-switch").forEach((button) => button.parentElement.replaceChildren(button.querySelector(".mf-stream-switch-label").textContent));
}
function chatNowExpanded(open) {
  document.querySelectorAll(".mf-stream-switch").forEach((button) => button.setAttribute("aria-expanded", String(open)));
}

// chatNowOpenSwitcher — the shared bottom sheet, filled from the same rows.
// It names the conversation it switches from (phone rule 6), holds focus
// while open, and its own history entry makes the phone's Back close it.
function chatNowOpenSwitcher() {
  if (!chatNowPhone() || typeof mfSheet === "undefined") return;
  const rows = chatNowRows(), from = document.querySelector("#chatThreadHeader .mf-stream-switch-label")?.textContent.trim();
  const body = mfSheet.open((host) => {
    const dialog = el("div", "mf-streams"), head = el("div", "mf-streams-head"), title = el("h2", "mf-streams-title", "Switch chat");
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-labelledby", "mfStreamsTitle");
    title.id = "mfStreamsTitle";
    head.append(title);
    if (from) head.append(el("p", "mf-streams-current", "Now open: " + from));
    const unsure = chatNowUnsure(), groups = chatNowGroups(rows, "switcher");
    if (unsure) head.append(el("p", "mf-streams-note", unsure));
    dialog.append(head);
    if (!groups.length && !unsure) dialog.append(el("p", "mf-streams-empty", "No other chat is waiting on you, working or pinned."));
    for (const group of groups) dialog.append(chatNowGroupEl(group, "mf-streams", "mf-stream-row"));
    const all = el("button", "mf-streams-all", "View all chats");
    all.type = "button";
    all.addEventListener("click", chatNowViewAll);
    dialog.append(all);
    dialog.addEventListener("click", (e) => {
      const row = e.target.closest?.(".mf-stream-row");
      if (!row || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      chatNowGo(row.getAttribute("href"));
    });
    dialog.addEventListener("keydown", chatNowSheetKeys);
    host.append(dialog);
  }, { key: chatNowSheetKey, onClose: chatNowDismissed });
  if (!history.state?.mfStreams) history.pushState({ ...(history.state || {}), mfStreams: true }, "");
  chatNowExpanded(true);
  (body.querySelector(".mf-stream-row") || body.querySelector(".mf-streams-all"))?.focus({ preventScroll: true });
}
// Escape closes; Tab and Shift+Tab stay inside the sheet
function chatNowSheetKeys(e) {
  if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); mfSheet.close(); return; }
  if (e.key !== "Tab") return;
  const items = [...e.currentTarget.querySelectorAll("a[href], button:not(:disabled)")].filter((n) => n.getClientRects().length);
  if (!items.length) return;
  const first = items[0], last = items.at(-1);
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
}
// dismissed (Escape, scrim, handle): the same conversation, focus back on its
// title, the sheet's history entry gone
function chatNowDismissed() {
  chatNowExpanded(false);
  if (history.state?.mfStreams) history.back();
  document.querySelector("#chatThreadHeader .mf-stream-switch")?.focus({ preventScroll: true });
}
// chatNowGo — a row: drop the sheet's own history entry, then go to the
// conversation's route, so Back from there returns to the one left
function chatNowGo(route) {
  mfSheet.close({ silent: true });
  chatNowExpanded(false);
  chatNowFocusTitle = { route, until: Date.now() + 4000 };
  if (history.state?.mfStreams) { chatNowPendingRoute = route; history.back(); }
  else location.hash = route;
}
// View all chats: the Chats list takes over the sheet's history entry, so
// Back closes the list and stays in the conversation
function chatNowViewAll() {
  mfSheet.close({ silent: true });
  chatNowExpanded(false);
  if (history.state?.mfStreams) { const { mfStreams, ...rest } = history.state; history.replaceState({ ...rest, mfChats: true }, ""); }
  window.mf?.openChats?.();
}

if (typeof window !== "undefined" && window.matchMedia) {
  window.addEventListener("popstate", () => {
    if (chatNowPendingRoute) { const route = chatNowPendingRoute; chatNowPendingRoute = ""; location.hash = route; return; }
    // the phone's Back while the sheet is open closes it, nothing else
    if (typeof mfSheet !== "undefined" && mfSheet.openKey() === chatNowSheetKey && !history.state?.mfStreams) {
      mfSheet.close({ silent: true });
      chatNowExpanded(false);
      document.querySelector("#chatThreadHeader .mf-stream-switch")?.focus({ preventScroll: true });
    }
  });
  window.addEventListener("hashchange", () => {
    if (typeof mfSheet !== "undefined" && mfSheet.openKey() === chatNowSheetKey) { mfSheet.close({ silent: true }); chatNowExpanded(false); }
  });
  // crossing 860px: 98-mobile.js closes the sheet silently; the title, Now and
  // any history entry the sheet left follow
  window.matchMedia("(max-width: 860px)").addEventListener("change", () => {
    chatNowExpanded(false);
    if (history.state?.mfStreams && !chatNowPendingRoute) history.back();
    const head = document.querySelector("#chatThreadHeader > .chat-head");
    if (chatNowPhone()) { if (head) chatNowTitleSwitch(head); } else chatNowTitlePlain();
    if (document.getElementById("chatInboxRows")) chatNowPaint();
  });
}
