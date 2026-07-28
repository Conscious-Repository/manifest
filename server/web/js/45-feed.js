// ---- FEED: manifest's one inbox (top-level tab, feed-central §1/§4) ----
// INBOX (default) = items awaiting a verdict (new + lapsed snoozes). Keep endorses
// and moves the item to KEPT. Chips are INBOX/KEPT/ALL.
const FEED_VIEWS = [["inbox", "INBOX"], ["kept", "KEPT"], ["all", "ALL"]];
const SIGNAL_CAP = 8; // most-overdue signals shown; the rest fold behind "N more"
let signalsExpanded = false;
let feedCache = { items: [], signals: [], proposals: [], portalItems: [] };

function showFeed() {
  loadFeed();
  ensureLivePoll(); // a dig/ask spooled from here is watched without leaving the tab
}

async function loadFeed() {
  const view = state.feedView || "inbox";
  try {
    const d = await (await fetch("/api/feed?status=" + view)).json();
    feedCache = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [], portalItems: d.portalItems || [] };
    setBadge(els.feedNavBadge, d.badge || 0);
    if (view === "inbox") diffDigests(feedCache.items); // catch digests landed while unpolled
  } catch (e) { feedCache = { items: [], signals: [], proposals: [], portalItems: [] }; }
  renderFeedFilters();
  renderFeed();
}

// refreshFeedBadge keeps the nav pill honest from anywhere (boot, route, verdicts,
// run-finish). Always async — the count can touch the contacts calendar cache.
async function refreshFeedBadge() {
  try {
    const d = await (await fetch("/api/feed/badge")).json();
    setBadge(els.feedNavBadge, d.count || 0);
  } catch (e) {}
}

function renderFeedFilters() {
  const host = els.feedFilters; host.innerHTML = "";
  const cur = state.feedView || "inbox";
  FEED_VIEWS.forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (cur === val ? " on" : ""), label);
    b.onclick = () => { state.feedView = val; loadFeed(); };
    host.appendChild(b);
  });
}
function renderFeed() {
  const host = els.feedList; host.innerHTML = "";
  const sigHost = els.feedSignals; sigHost.innerHTML = ""; // collapses when empty
  const view = state.feedView || "inbox";
  // signals lane: app-derived nudges, INBOX only, tight one-line chips. Never
  // under KEPT/ALL (conditions, not items). Capped so a long neglect backlog
  // doesn't bury the findings — the most-overdue lead, the rest fold away.
  if (view === "inbox" && feedCache.signals.length) {
    const total = feedCache.signals.length;
    sigHost.appendChild(el("div", "reading-strip-head", "Signals — " + total));
    const shown = signalsExpanded ? total : Math.min(SIGNAL_CAP, total);
    feedCache.signals.slice(0, shown).forEach((sg) => sigHost.appendChild(signalRow(sg)));
    if (total > SIGNAL_CAP) {
      const more = el("button", "signal-more", signalsExpanded ? "▴ show fewer" : `▾ ${total - SIGNAL_CAP} more`);
      more.onclick = () => { signalsExpanded = !signalsExpanded; renderFeed(); };
      sigHost.appendChild(more);
    }
  }
  // pinned lane: FULL approval cards (diff + Confirm/Reject inline — the
  // approvals inbox lives HERE now, not in SPIRITS) lead the inbox; digests pin
  // next via the items sort. Approvals derive from pending/ so a decision
  // resolves the card atomically; they never appear under KEPT/ALL.
  if (view === "inbox") feedCache.proposals.forEach((p) => host.appendChild(approvalCardEl(p)));
  // portal-items lane: externally-sourced notices (clickup digest, benchling
  // items), deterministic + script-rendered. INBOX only — they're notices, not
  // kept/discarded items, and never touch the tune loop.
  if (view === "inbox") feedCache.portalItems.forEach((pc) => host.appendChild(portalCardEl(pc)));
  if (!feedCache.items.length && !host.children.length) {
    host.appendChild(emptyRow(view === "inbox"
      ? "Inbox zero — nothing awaiting a verdict."
      : view === "kept" ? "Nothing kept yet." : "No feed items yet."));
    return;
  }
  feedCache.items.forEach((it) => host.appendChild(feedCard(it)));
  if (pendingApprovalFocus) { // deep-linked (Studio tuning panel "review →")
    const target = host.querySelector(`[data-approval-id="${CSS.escape(pendingApprovalFocus)}"]`);
    pendingApprovalFocus = null;
    if (target) {
      target.scrollIntoView({ behavior: "smooth", block: "start" });
      target.classList.add("goal-flash");
      setTimeout(() => target.classList.remove("goal-flash"), 2400);
    }
  }
}

// signalRow renders one app-signal: a quiet one-line chip (kind · entity · age)
// with Act (deep link) · Snooze · Dismiss. A rock signal can also go "→ today".
function signalRow(sg) {
  const row = el("div", "signal-row");
  const label = el("span", "signal-label cp-clickable", sg.label);
  label.onclick = () => { location.hash = sg.actHref; };
  row.append(label);
  const act = el("span", "signal-actions");
  if (sg.kind === "todo-stale" || sg.kind === "todo-waiting") {
    // the stale-todo card asks for a decision: do · mark waiting · drop
    // (state changes auto-clear the condition — no dismissal bookkeeping)
    act.append(
      pillLight("Done ✓", () => signalAction("/api/todos/check", { id: sg.goalId, checked: true })),
    );
    if (sg.kind === "todo-stale") {
      act.append(pillLight("Waiting…", () => {
        const who = personInput((v) => signalAction("/api/todos/update", { id: sg.goalId, waiting: v }),
          () => loadFeed());
        act.replaceWith(who.el);
        who.focus();
      }));
      act.append(pillLight("→ issue", () => signalAction("/api/todos/to-issue", { id: sg.goalId })));
    }
    act.append(pillLight("Drop", () => signalAction("/api/todos/drop", { id: sg.goalId })));
  }
  act.append(
    pillLight("Act", () => { location.hash = sg.actHref; }),
    pillLight("Snooze", () => signalAction("/api/feed/signal/snooze", { id: sg.id, days: 7 })),
    pillLight("Dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash })),
  );
  row.append(act);
  return row;
}
async function signalAction(url, body) {
  try { await postJSON(url, body); } catch (e) {}
  loadFeed();
}

// portalCardEl renders the third feed card kind: an externally-sourced portal
// notice, built entirely from the deterministic poll cache (no LLM). A ClickUp
// day collapses to one digest (assigned-to-you block first, then per-list
// groups); a Benchling change is one item card with a jump link. Dismiss (and
// jump, for items) are the only actions — portals are read-only to their source.
function portalCardEl(pc) {
  const isDigest = pc.type === "portal-digest";
  const card = el("div", "feed-card portal-card" + (pc.pinned ? " pinned" : ""));
  card.dataset.portalId = pc.id;
  const top = el("div", "feed-top");
  if (pc.pinned) top.append(el("span", "pin-chip", "📌 pinned"));
  top.append(el("span", "type-chip type-portal", pc.portal)); // muted source tag
  if (pc.change) top.append(el("span", "portal-change-chip change-" + pc.change, pc.change)); // new / edited
  if (pc.date) top.append(el("span", "feed-date", fmtFeedDate(pc.date)));
  card.append(top);
  card.append(el("div", "feed-title", pc.title));

  if (isDigest) {
    if ((pc.forYou || []).length) {
      card.append(el("div", "portal-subhead", "assigned to you / mentions you"));
      pc.forYou.forEach((ln) => card.append(portalLineRow(ln)));
    }
    (pc.groups || []).forEach((g) => {
      card.append(el("div", "portal-subhead", g.list));
      (g.lines || []).forEach((ln) => card.append(portalLineRow(ln)));
    });
  } else {
    if (pc.detail) card.append(el("div", "feed-why", pc.detail));
    const meta = el("div", "feed-meta");
    if (pc.actor) meta.append(el("span", "", "by " + pc.actor));
    card.append(meta);
  }

  const acts = el("div", "feed-actions");
  if (!isDigest && pc.url) acts.append(pillLight("jump →", () => window.open(pc.url, "_blank")));
  acts.append(pillLight("Dismiss", () => portalDismiss(pc.id)));
  card.append(acts);
  return card;
}

// portalLineRow is one digest line: the task, linking to the source app.
function portalLineRow(ln) {
  const row = el("div", "portal-line");
  const label = ln.url
    ? Object.assign(el("a", "portal-line-text", ln.text), { href: ln.url, target: "_blank" })
    : el("span", "portal-line-text", ln.text);
  row.append(label);
  return row;
}

async function portalDismiss(id) {
  try { await postJSON("/api/portals/item/dismiss", { id }); } catch (e) {}
  loadFeed();
}
function fmtFeedDate(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function faviconFor(link) {
  try {
    const host = new URL(link).hostname;
    const img = el("img", "feed-favicon");
    img.src = "https://www.google.com/s2/favicons?domain=" + encodeURIComponent(host) + "&sz=32";
    img.loading = "lazy";
    img.onerror = () => img.remove();
    return img;
  } catch (e) { return null; }
}
function feedCard(it) {
  if (it.type === "draft") return draftFeedCard(it);
  const pinned = it.type === "digest" && it.status === "new";
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : "") + (it.type === "digest" ? " digest" : "") +
    (pinned ? " pinned" : "") + (it.status === "discarded" ? " discarded" : ""));
  const top = el("div", "feed-top");
  if (pinned) top.append(el("span", "pin-chip", "📌 pinned"));
  top.append(el("span", "type-chip type-" + it.type, it.type));
  // only a real external URL makes the title a link; an artifact's local
  // `artifacts/library/…` reference opens in the note view via "view →" instead.
  const external = /^https?:\/\//i.test(it.link || "");
  let title;
  if (external) title = linkEl(it.title, it.link);
  else if (it.artifactPath) { title = el("span", "cp-clickable", it.title); title.onclick = () => openArtifact(it.artifactPath); }
  else title = el("span", null, it.title);
  title.classList.add("feed-title");
  top.append(title);
  if (it.confidence) top.append(el("span", "conf conf-" + it.confidence, it.confidence));
  card.append(top);
  // the why line is written to be the reason you care — lead with it, emphasized
  if (it.why) card.append(el("div", "feed-why", it.why));
  const meta = el("div", "feed-meta");
  const fav = external ? faviconFor(it.link) : null;
  if (fav) meta.append(fav);
  const bits = [it.source || it.domain, it.agent, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ");
  meta.append(el("span", null, bits));
  card.append(meta);
  if (it.body && (pinned || it.type === "artifact")) { const b = el("pre", "feed-body"); b.textContent = it.body; card.append(b); }
  if (it.vaultNote) card.append(el("div", "feed-saved", "✓ saved to " + it.vaultNote));
  const actions = el("div", "feed-actions");
  if (it.artifactPath) actions.append(pillLight("view →", () => openArtifact(it.artifactPath))); // the full brief
  if (it.status !== "discarded") {
    actions.append(pillLight("Keep", () => feedAction(it.id, { status: "kept" })));
    if (it.status !== "kept") actions.append(pillLight("Discard", () => feedAction(it.id, { status: "discarded" })));
    actions.append(pillLight("Snooze 7d", () => feedAction(it.id, { status: "snoozed", days: 7 })));
    if (!it.vaultNote) actions.append(pillLight("Save to vault", () => feedSaveToVault(it.id)));
    actions.append(pillLight("→ todo", () => feedToTodo(it.id))); // catch it on the TODOS board (Inbox)
    if (it.type !== "digest") actions.append(pillLight("dig →", () => feedDig(it.id))); // spool a deeper run
  } else {
    actions.append(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
  }
  card.append(actions);
  return card;
}

// draftFeedCard renders a Content Studio draft as a tweet-shaped card: the post
// text big, the critic's rationale, and inline approve / edit / dismiss plus a
// "judge" note. Approve confirms the linked append-x-queue approval; dismiss
// rejects it; edit rewrites both the draft and the pending bullet so the edited
// text is what lands.
function draftFeedCard(it) {
  const card = el("div", "feed-card draft" + (it.status === "discarded" ? " discarded" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-draft", "draft"));
  if (it.format && it.format !== "single") top.append(el("span", "draft-format", it.format));
  top.append(el("span", "feed-title", it.title || "draft"));
  card.append(top);

  const tweet = el("div", "draft-tweet");
  tweet.textContent = it.body || "";
  card.append(tweet);
  // quote-tweet variant: render the quoted post beneath (like X)
  if (it.quotedText) {
    const q = el("div", "draft-quote");
    q.append(el("div", "draft-quote-text", it.quotedText));
    if (it.quotedUrl) q.append(linkEl(it.quotedUrl, it.quotedUrl));
    card.append(q);
  }
  if (it.why) card.append(el("div", "feed-why", it.why));
  const meta = el("div", "feed-meta");
  meta.append(el("span", null, [it.agent, (it.date || "").slice(0, 10)].filter(Boolean).join("  ·  ")));
  card.append(meta);

  if (it.status === "discarded") {
    const a = el("div", "feed-actions");
    a.append(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
    card.append(a);
    return card;
  }

  // edit box (hidden until "Edit")
  const editWrap = el("div", "draft-edit"); editWrap.hidden = true;
  const ta = el("textarea", "draft-edit-input"); ta.value = it.body || "";
  const editActions = el("div", "feed-actions");
  editActions.append(
    pill("Save edit", async () => {
      const t = ta.value.trim(); if (!t) return;
      await studioPost(`/api/studio/draft/${encodeURIComponent(it.draftId)}/edit`, { text: t, approvalId: it.approvalId });
      showToast("edit saved — approve to queue the edited version", null, "info");
      loadFeed();
    }),
    pillLight("Cancel", () => { editWrap.hidden = true; }),
  );
  editWrap.append(ta, editActions);

  // feedback: a single "judge" affordance (shared with the board cards)
  const fb = buildDraftFeedback(it.draftId, "");

  const actions = el("div", "feed-actions");
  actions.append(
    pill("Approve → queue", () => draftApproval(it.approvalId, "confirm")),
    pillLight("Edit", () => { editWrap.hidden = !editWrap.hidden; }),
    pillLight("Dismiss", () => studioDismiss(it.draftId, it.approvalId, loadFeed)),
  );
  card.append(editWrap, fb, actions);
  return card;
}

// studioDismiss resolves an owner rejection server-side across all three
// objects (approval + draft file + feed card) — see handleStudioDismiss.
async function studioDismiss(draftId, approvalId, refresh) {
  if (!draftId) { showToast("this card has no draft id", null, "error"); return; }
  await studioPost(`/api/studio/draft/${encodeURIComponent(draftId)}/dismiss`, { approvalId: approvalId || "" });
  showToast("dismissed", null, "info");
  refresh();
}

async function draftApproval(approvalId, kind) {
  if (!approvalId) { showToast("this draft has no linked approval", null, "error"); return; }
  setSaveState("saving");
  const body = kind === "reject" ? { reason: "dismissed from studio" } : {};
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(approvalId)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  showToast(kind === "confirm" ? "queued to x posts.md ✓" : "dismissed", null, "info");
  loadFeed();
}

async function studioPost(path, body) {
  setSaveState("saving");
  try { const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); if (!r.ok) throw new Error(await r.text()); setSaveState("saved"); return await r.json().catch(() => ({})); }
  catch (e) { setSaveState("error"); showToast("Studio action failed: " + (e.message || e), null, "error"); throw e; }
}
