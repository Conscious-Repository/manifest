// ---- FEED: manifest's one inbox (top-level tab, feed-central §1/§4) ----
//
// The chips are KIND filters, not status filters (owner decision 2026-08-25).
// The old row mixed the two — INBOX/KEPT/ALL selected a status while CONSUME
// selected a kind — which is what made it read as incoherent. Now: nothing lit
// means every lane; a lit chip means only that lane; clicking a lit chip clears
// it. There is no status the user can pick.
//
// ⚠ The request status is therefore PINNED to "inbox". It used to be whatever
// the chip said, piped straight into ?status= — and feed.Store.List's default
// branch does an EXACT status match, so any value outside its vocabulary
// returns zero findings silently, with no error (feed/feed.go:108). Sending a
// kind name down that path would have emptied the feed and looked like a bug in
// the data.
const FEED_FILTERS = [["proposal", "APPROVALS"], ["consume", "CONSUME"]];
const FEED_STATUS = "inbox"; // never user-selectable — see above
const SIGNAL_CAP = 8; // most-overdue signals shown; the rest fold behind "N more"
let signalsExpanded = false;
let feedCache = { items: [], signals: [], proposals: [], portalItems: [], consumeItems: [], receipts: [] };

// ---- the §5 card registry: ONE renderer per attention kind, plus the
// proposals lane (an authorization queue, not an attention kind). renderFeed
// dispatches through this table — adding a kind = one renderer + one lane row.
// Receipts are declared (permanent lifecycle, server slot registered) with no
// machinery yet, so their lane renders nothing until an errand loop exists.
const FEED_CARD = {
  signal: (sg) => signalRow(sg),
  delegationDone: (sg) => delegationDoneCard(sg),
  planReady: (sg) => planReadyCard(sg),
  agentQuestions: (sg) => agentQuestionsCard(sg),
  proposal: (p) => approvalCardEl(p),
  notice: (pc) => portalCardEl(pc),
  consume: (c) => consumeCardEl(c),
  finding: (it) => feedCard(it),
  receipt: (rc) => receiptCardEl(rc),
};
// delegation-done is a SIGNAL server-side (an app-derived condition that
// auto-clears when the todo is checked — §5 stays four kinds), but the owner
// wants completed delegations to read as full, actionable cards rather than
// one-line chips. So it splits off the strip and renders into the main list.
const isDelegationDone = (sg) => sg.kind === "delegation-done";
const isPlanReady = (sg) => sg.kind === "plan-ready";
const isAgentQuestions = (sg) => sg.kind === "agent-questions";
// main-list lanes in render order (other signals render into their own strip).
const FEED_LANES = [
  { kind: "agentQuestions", slice: (c) => (c.signals || []).filter(isAgentQuestions), inboxOnly: true },
  { kind: "planReady", slice: (c) => (c.signals || []).filter(isPlanReady), inboxOnly: true },
  { kind: "delegationDone", slice: (c) => (c.signals || []).filter(isDelegationDone), inboxOnly: true },
  { kind: "proposal", slice: (c) => c.proposals, inboxOnly: true },
  { kind: "notice", slice: (c) => c.portalItems, inboxOnly: true },
  // CONSUME: subscribed reading. Capped like the signals strip — a week of
  // newsletters must not bury the things that actually want a decision. The
  // rest live behind the CONSUME view, which is where reading belongs anyway.
  { kind: "consume", slice: (c) => (c.consumeItems || []).filter(consumeQueued).slice(0, CONSUME_CAP), inboxOnly: true },
];
const FEED_TAIL_LANES = [ // after the empty-state check, like today
  { kind: "finding", slice: (c) => c.items, inboxOnly: false },
  { kind: "receipt", slice: (c) => c.receipts, inboxOnly: false },
];

function showFeed() {
  loadFeed();
  ensureLivePoll(); // a dig/ask spooled from here is watched without leaving the tab
}

async function loadFeed() {
  // CONSUME is its own surface over its own endpoint — the reading backlog is
  // not an inbox and does not want the inbox's empty states.
  if (feedFilter() === "consume") { renderFeedFilters(); await loadConsume(); refreshFeedBadge(); return; }
  // Drop the cached approval registries so the next card built pulls the LIVE
  // rock ladder (goals edited elsewhere in-session must not serve a stale
  // rock list into the payload editor's typeahead).
  apprAionReg = null; apprReReg = null;
  try {
    const d = await (await fetch("/api/feed?status=" + FEED_STATUS)).json();
    feedCache = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [], portalItems: d.portalItems || [], consumeItems: d.consumeItems || [], receipts: d.receipts || [] };
    setBadge(els.feedNavBadge, d.badge || 0);
    diffDigests(feedCache.items); // catch digests landed while unpolled
  } catch (e) { feedCache = { items: [], signals: [], proposals: [], portalItems: [], consumeItems: [], receipts: [] }; }
  renderFeedFilters();
  renderFeed();
}

// feedFilter is the active lane kind ("" = show everything).
function feedFilter() { return state.feedFilter || ""; }

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
  const cur = feedFilter();
  FEED_FILTERS.forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (cur === val ? " on" : ""), label);
    // A lit chip is a filter you can take off by clicking it again — no
    // separate "ALL" button to reach for.
    b.onclick = () => { state.feedFilter = cur === val ? "" : val; loadFeed(); };
    host.appendChild(b);
  });
}
function renderFeed() {
  const host = els.feedList; host.innerHTML = "";
  const sigHost = els.feedSignals; sigHost.innerHTML = ""; // collapses when empty
  const filter = feedFilter();
  // laneVisible replaces the old `inboxOnly && view !== "inbox"` gate: with no
  // filter every lane paints, and with one only that lane does.
  const laneVisible = (kind) => !filter || filter === kind;
  // signals lane: app-derived nudges, tight one-line chips. Capped so a long
  // neglect backlog doesn't bury the findings — the most-overdue lead, the rest
  // fold away.
  const stripSignals = feedCache.signals.filter((sg) => !isDelegationDone(sg) && !isPlanReady(sg) && !isAgentQuestions(sg));
  if (!filter && stripSignals.length) {
    const total = stripSignals.length;
    sigHost.appendChild(el("div", "reading-strip-head", "Signals — " + total));
    const shown = signalsExpanded ? total : Math.min(SIGNAL_CAP, total);
    stripSignals.slice(0, shown).forEach((sg) => sigHost.appendChild(FEED_CARD.signal(sg)));
    if (total > SIGNAL_CAP) {
      const more = el("button", "signal-more", signalsExpanded ? "▴ show fewer" : `▾ ${total - SIGNAL_CAP} more`);
      more.onclick = () => { signalsExpanded = !signalsExpanded; renderFeed(); };
      sigHost.appendChild(more);
    }
  }
  // pinned lanes lead the inbox: proposals (FULL approval cards — diff +
  // Confirm/Reject inline; they derive from pending/ so a decision resolves
  // the card atomically) then notices (externally-sourced portal cards,
  // deterministic + script-rendered). Both INBOX only — they are not
  // kept/discarded items and never touch the tune loop.
  FEED_LANES.forEach((lane) => {
    if (!laneVisible(lane.kind)) return;
    lane.slice(feedCache).forEach((c) => host.appendChild(FEED_CARD[lane.kind](c)));
  });
  // the tail button for the capped consume lane, before the empty-state check
  const unreadConsume = (feedCache.consumeItems || []).filter(consumeQueued).length;
  if (!filter && unreadConsume > CONSUME_CAP) {
    const more = el("button", "signal-more", `▾ ${unreadConsume - CONSUME_CAP} more in CONSUME`);
    more.onclick = () => { state.feedFilter = "consume"; loadFeed(); };
    host.appendChild(more);
  }
  if (!host.children.length && (!laneVisible("finding") || !feedCache.items.length)) {
    host.appendChild(emptyRow(
      filter === "proposal" ? "Nothing awaiting approval."
        : filter ? "Nothing here." : "Inbox zero — nothing awaiting you."));
    return;
  }
  FEED_TAIL_LANES.forEach((lane) => {
    if (!laneVisible(lane.kind)) return;
    lane.slice(feedCache).forEach((c) => host.appendChild(FEED_CARD[lane.kind](c)));
  });
  // the rail: drafts and the selection outlive this repaint (the 3s poll can
  // rebuild the list under an open edit), so re-mark and re-fill from them
  apprDraftsKeep((feedCache.proposals || []).map((x) => x.id));
  apprPaintSel();
  renderApprovalInspector();
  if (pendingApprovalFocus) { // deep-linked ("review →")
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
// whose label deep-links to the item; verbs are Done ✓ (todo signals) · Dismiss.
function signalRow(sg) {
  const row = el("div", "signal-row");
  const label = el("span", "signal-label cp-clickable", sg.label);
  // the label ALWAYS navigates to the thing the signal is about (owner fix
  // 2026-08-12: a runId used to hijack this into an unrelated run report —
  // viewing a result is its own explicit action on the card, never the label).
  label.onclick = () => { location.hash = sg.actHref || "#/feed"; };
  row.append(label);
  // two verbs only (owner call 2026-08-10): Done ✓ on todo signals, dismiss on
  // everything — the label itself already navigates to the item.
  const act = el("span", "signal-actions");
  if (sg.kind === "todo-stale" || sg.kind === "todo-waiting") {
    act.append(pillLight("Done ✓", () => signalAction("/api/tasks/check", { id: sg.goalId, checked: true }, row)));
  }
  act.append(pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, row)));
  row.append(act);
  return row;
}

// delegationDoneCard — a completed delegation as a FULL feed card (owner ask
// 2026-08-12): the work you delegated, its result one click away, and the todo
// one click away. Signal lifecycle governs the verbs — dismiss, plus the
// Done ✓ quick-action that resolves the condition (§5 amendment C4). Never
// Keep/Discard: a condition must not pollute the findings quality signal.
function delegationDoneCard(sg) {
  const card = el("div", "feed-card artifact delegation-done");
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip micro-label type-artifact", "delegated"));
  if (sg.harness) top.append(el("span", "harness-chip", sg.harness));
  const title = el("span", "feed-title cp-clickable", sg.entity || sg.label);
  title.title = "open it on the TASKS board";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  top.append(title);
  card.append(top);
  card.append(el("div", "feed-why", sg.artifactRef || sg.artifactPath
    ? "delegated work came back with an artifact — read it, then close the task or send it back out"
    : "delegated work finished — read the run report, then close the task or send it back out"));
  const meta = el("div", "feed-meta");
  meta.append(el("span", null, ["ready for review", sg.harness].filter(Boolean).join("  ·  ")));
  card.append(meta);
  const actions = el("div", "feed-actions");
  const view = pillLight("view →", () => openResult(sg, sg.entity));
  view.classList.add("verdict-primary");
  actions.append(view);
  actions.append(pillLight("open task →", () => { location.hash = sg.actHref || "#/tasks"; }));
  actions.append(pillLight("Done ✓", () => signalAction("/api/tasks/check", { id: sg.goalId, checked: true }, card)));
  actions.append(pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)));
  card.append(actions);
  return card;
}
// planReadyCard (todo-panel Phase 4): the assigned agent's PLAN landed in the
// todo's record — review it in the panel, edit by hand, then fire. The card
// auto-clears when the go-phase run outranks plan-ready (or the todo closes).
function planReadyCard(sg) {
  const card = el("div", "feed-card artifact plan-ready");
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip micro-label type-artifact", "plan"));
  if (sg.harness) top.append(el("span", "harness-chip", sg.harness));
  const title = el("span", "feed-title cp-clickable", sg.entity || sg.label);
  title.title = "review the plan in the task panel";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  top.append(title);
  card.append(top);
  card.append(el("div", "feed-why", "the agent drafted a plan — review it, edit it in place if needed, then fire to execute"));
  const actions = el("div", "feed-actions");
  const review = pillLight("review plan →", () => { location.hash = sg.actHref || "#/tasks"; });
  review.classList.add("verdict-primary");
  actions.append(review);
  actions.append(pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)));
  card.append(actions);
  return card;
}

// agentQuestionsCard — the agent needs answers before it can plan; the
// questions are IN the todo's thread. Auto-clears when you answer (the ball
// moves) or a newer brief lands.
function agentQuestionsCard(sg) {
  const card = el("div", "feed-card artifact agent-questions");
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip micro-label type-artifact", "questions"));
  if (sg.harness) top.append(el("span", "harness-chip", sg.harness));
  const title = el("span", "feed-title cp-clickable", sg.entity || sg.label);
  title.title = "open the thread";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  top.append(title);
  card.append(top);
  card.append(el("div", "feed-why", "the agent has questions before it can plan — answer them in the task's thread"));
  const actions = el("div", "feed-actions");
  const ans = pillLight("answer in the thread →", () => { location.hash = sg.actHref || "#/tasks"; });
  ans.classList.add("verdict-primary");
  actions.append(ans);
  actions.append(pillLight("Snooze 7d", () => signalAction("/api/feed/signal/snooze", { id: sg.id, days: 7 }, card)));
  actions.append(pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)));
  card.append(actions);
  return card;
}

async function signalAction(url, body, cardEl) {
  if (cardEl) cardEl.remove(); // optimistic — the condition clears server-side next
  try { await postJSON(url, body); } catch (e) {}
  loadFeed();
}

// feedVerdict — the moment a card gets its verdict it collapses to a one-line
// stub (§11): the verb, the title struck through, and undo. The zero-inbox
// count stays honest without the item vanishing irreversibly.
async function feedVerdict(card, it, verb, status) {
  // optimistic: the stub swaps in the instant you click — the write follows
  // behind it; a failed write reloads the list so the card comes back honest.
  const stub = el("div", "feed-stub");
  stub.append(el("span", "feed-stub-verb micro-label", verb), el("span", "feed-stub-title", it.title));
  const undo = el("button", "feed-stub-undo", "undo");
  undo.onclick = () => feedAction(it.id, { status: "new" });
  stub.append(undo);
  card.replaceWith(stub);
  setSaveState("saving");
  try {
    await fetch(`/api/feed/${encodeURIComponent(it.id)}/status`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ status }),
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); loadFeed(); return; }
  refreshFeedBadge();
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
  if (pc.pinned) top.append(el("span", "pin-chip", "pinned"));
  top.append(el("span", "type-chip micro-label type-portal", pc.portal)); // muted source tag
  if (pc.change) top.append(el("span", "portal-change-chip micro-label change-" + pc.change, pc.change)); // new / edited
  if (pc.date) top.append(el("span", "feed-date", fmtFeedDate(pc.date)));
  card.append(top);
  card.append(el("div", "feed-title", pc.title));

  if (isDigest) {
    if ((pc.forYou || []).length) {
      card.append(el("div", "portal-subhead micro-label", "assigned to you / mentions you"));
      pc.forYou.forEach((ln) => card.append(portalLineRow(ln)));
    }
    (pc.groups || []).forEach((g) => {
      card.append(el("div", "portal-subhead micro-label", g.list));
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
  acts.append(pillLight("Dismiss", () => portalDismiss(pc.id, card)));
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

async function portalDismiss(id, cardEl) {
  if (cardEl) cardEl.remove(); // optimistic — the dismissal lands server-side next
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
  const pinned = it.type === "digest" && it.status === "new";
  const card = el("div", "feed-card" + (it.type === "artifact" ? " artifact" : "") + (it.type === "digest" ? " digest" : "") +
    (pinned ? " pinned" : "") + (it.status === "discarded" ? " discarded" : ""));
  const top = el("div", "feed-top");
  if (pinned) top.append(el("span", "pin-chip", "pinned"));
  top.append(el("span", "type-chip micro-label type-" + it.type, it.type));
  if (it.harness) top.append(el("span", "harness-chip", it.harness)); // federation source
  // only a real external URL makes the title a link; an artifact's local
  // `artifacts/library/…` reference opens in the note view via "view →" instead.
  const external = /^https?:\/\//i.test(it.link || "");
  let title;
  if (external) title = linkEl(it.title, it.link);
  else if (it.artifactPath || it.artifactRef) { title = el("span", "cp-clickable", it.title); title.onclick = () => openResult(it, it.title); }
  else title = el("span", null, it.title);
  title.classList.add("feed-title");
  top.append(title);
  if (it.confidence) top.append(el("span", "conf micro-label conf-" + it.confidence, it.confidence));
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
  // the full brief, rendered legibly (harness file or vault note — openResult
  // picks the medium; a card with neither ref falls through with no "view →")
  if (it.artifactPath || it.artifactRef) actions.append(pillLight("view →", () => openResult(it, it.title)));
  if (it.status !== "discarded") {
    // ⚠ No Keep button (owner decision 2026-08-25). The `kept` STATUS still
    // exists and the weekly tune ritual still reads it — but it is now written
    // only by "→ task" and save-to-vault, i.e. by acting on the item rather
    // than by tapping approval at it. Over eight weeks Keep was pressed twice
    // against 128 discards, and both cornerstone rewrites the loop produced
    // were derived from discard patterns alone.
    //
    // Discard is now the ONLY way to clear something you don't want, and
    // findings never age out — so it is the primary verb and never hidden.
    const discard = pillLight("Discard", () => feedVerdict(card, it, "discarded", "discarded"));
    discard.classList.add("verdict-primary");
    actions.append(discard);
    actions.append(pillLight("→ task", () => feedToTodo(it.id))); // catch it on the TASKS board (Inbox)
    if (it.type !== "digest") actions.append(pillLight("dig →", () => feedDig(it.id))); // spool a deeper run
    // curate → the public feed. A NEW action, not a verdict: it says
    // "subscribers should read this", and leaves the card's status alone.
    // Only for a card that points at a real article — there is nothing to
    // fetch, and nothing to link subscribers to, without one.
    if (external) actions.append(curatePill(it));
  } else {
    actions.append(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
  }
  card.append(actions);
  return card;
}

// curatePill — the bridge into the public curation feed. One click fetches the
// whole article behind the card's link and writes it as an extrinsic/ note,
// exactly as the CONSUME lane's curate does; the note is what feed.xml serves.
// The prompt is the annotation subscribers read above the piece — skipping it
// still curates, and Cancel means cancel.
function curatePill(it) {
  const pill = pillLight("curate", () => feedCurate(it, pill));
  return pill;
}

async function feedCurate(it, pill) {
  const note = prompt("A note for subscribers (optional):", "");
  if (note === null) return; // cancelled — nothing published
  pill.disabled = true;
  pill.textContent = "curating…";
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(it.id)}/curate`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ note: note.trim() }),
    });
    if (!r.ok) throw new Error((await r.text()) || r.status);
    const d = await r.json().catch(() => ({}));
    // Say WHICH of the two landed: the whole piece, or a link and the note
    // because the fetch came back with a subscribe box instead of an article.
    showToast(d.full ? "Curated in full → " + (d.path || "extrinsic/")
                     : "Curated as a link — the article page didn't yield its text",
      null, "info");
    pill.replaceWith(uncuratePill(it));
  } catch (e) {
    pill.disabled = false;
    pill.textContent = "curate";
    showToast("Curate failed: " + (e.message || e), null, "error");
  }
}

// uncuratePill clears the curated marker. The note itself survives — this
// unpublishes, it does not delete.
function uncuratePill(it) {
  const pill = pillLight("uncurate", async () => {
    pill.disabled = true;
    try {
      const r = await fetch(`/api/feed/${encodeURIComponent(it.id)}/uncurate`, { method: "POST" });
      if (!r.ok) throw new Error((await r.text()) || r.status);
      showToast("Removed from the public feed — the note stays");
      pill.replaceWith(curatePill(it));
    } catch (e) {
      pill.disabled = false;
      showToast("Uncurate failed: " + (e.message || e), null, "error");
    }
  });
  return pill;
}

// ---- receipts: the fourth attention kind, backed by <dataDir>/errands/
// (errands-aside §5 as amended). App-local records — NEVER kept/discarded,
// no expiry; queued shows its place in line, running shows a live dot,
// failed/cancelled offer retry (no continue — the CLI emits no session ids).
function receiptCardEl(rc) {
  const card = el("div", "feed-card receipt status-" + rc.status);
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip micro-label type-receipt", "errand"));
  top.append(el("span", "type-chip micro-label type-portal", "aside")); // muted source tag
  const status = el("span", "receipt-status micro-label rc-" + rc.status,
    rc.status === "queued" && rc.queuePos ? "queued · #" + rc.queuePos : rc.status);
  top.append(status);
  if (rc.created) top.append(el("span", "feed-date", fmtFeedDate(rc.created)));
  card.append(top);
  card.append(el("div", "feed-title", rc.text));
  const meta = el("div", "feed-meta");
  const bits = ["account " + rc.account];
  if (rc.durationS) bits.push(rc.durationS + "s");
  if (rc.source === "proposal") bits.push("via approved proposal");
  meta.append(el("span", null, bits.join("  ·  ")));
  card.append(meta);
  if (rc.outcome) card.append(receiptOutcomeEl(rc.outcome));
  if (rc.goalId) {
    const g = el("span", "work-chip cp-clickable", "⚑ " + rc.goalId);
    g.onclick = () => { location.hash = "#/goals/" + encodeURIComponent(rc.goalId); };
    card.append(g);
  }
  const acts = el("div", "feed-actions");
  if (rc.transcript) acts.append(pillLight("transcript →", () => showErrandTranscript(rc)));
  if (rc.status === "queued" || rc.status === "running") {
    acts.append(pillLight("Cancel", () => errandAction("/api/errands/" + rc.id + "/cancel")));
  }
  if (rc.status === "failed" || rc.status === "cancelled") {
    acts.append(pillLight("Retry", () => errandAction("/api/errands/" + rc.id + "/retry")));
  }
  // Clear = acknowledged read-state, not a verdict: the card leaves the inbox
  // and the badge; the record + transcript persist under ALL (§5 audit trail).
  const finished = rc.status === "done" || rc.status === "failed" || rc.status === "cancelled";
  if (finished && !rc.acknowledged) {
    acts.append(pillLight("Clear", () => errandAction("/api/errands/" + rc.id + "/ack")));
  }
  if (acts.children.length) card.append(acts);
  return card;
}

// receiptOutcomeEl renders the agent's answer block: line breaks preserved,
// markdown links clickable, **bold** honored — no raw markdown on the card.
function receiptOutcomeEl(outcome) {
  const box = el("div", "receipt-outcome");
  const mdLink = /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g;
  const bold = /\*\*([^*]+)\*\*/g;
  outcome.split("\n").forEach((line) => {
    const row = el("div", "receipt-outcome-line");
    let rest = line;
    const pushText = (t) => {
      // render **bold** inside plain text runs
      let i = 0, m;
      bold.lastIndex = 0;
      while ((m = bold.exec(t)) !== null) {
        if (m.index > i) row.append(t.slice(i, m.index));
        row.append(el("strong", "", m[1]));
        i = m.index + m[0].length;
      }
      if (i < t.length) row.append(t.slice(i));
    };
    let m;
    mdLink.lastIndex = 0;
    while ((m = mdLink.exec(rest)) !== null) {
      const pre = rest.slice(0, m.index);
      if (pre) pushText(pre);
      row.append(linkEl(m[1], m[2]));
      rest = rest.slice(m.index + m[0].length);
      mdLink.lastIndex = 0;
    }
    if (rest) pushText(rest);
    box.append(row);
  });
  return box;
}

async function errandAction(url) {
  try { await postJSONOk(url, {}); } catch (e) { showToast((e.message || "errand action failed").slice(0, 80), null, "error"); }
  loadFeed();
}

// showErrandTranscript opens the transcript in the picker modal — a dataDir
// file, not a vault note (§5). A RUNNING errand's transcript is a live
// console: it tails every 2s and takes a reply — Aside's ask-gate questions
// stream out here and the answer goes back down the same pty, echoing into
// the transcript so the exchange stays on the audit trail.
const stripAnsi = (s) => s.replace(/\x1b\[[0-9;]*[A-Za-z]/g, "");
async function showErrandTranscript(rc) {
  els.pickerTitle.textContent = "Errand transcript — " + rc.text.slice(0, 60);
  const body = els.pickerBody; body.innerHTML = "";
  const pre = el("pre", "errand-transcript");
  body.append(pre);
  const load = async () => {
    try {
      const t = await (await fetch("/api/errands/" + rc.id + "/transcript")).text();
      const atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8;
      pre.textContent = stripAnsi(t) || "(no output yet)";
      if (atBottom) pre.scrollTop = pre.scrollHeight;
    } catch (e) { if (!pre.textContent) pre.textContent = "transcript unavailable"; }
  };
  await load();
  if (rc.status === "queued" || rc.status === "running") {
    const row = el("div", "errand-reply");
    const input = inputEl("answer the agent's question… (Enter sends into the session)");
    input.classList.add("errand-reply-in");
    const send = async () => {
      const t = input.value.trim();
      if (!t) return;
      input.value = "";
      try { await postJSONOk("/api/errands/" + rc.id + "/input", { text: t }); }
      catch (e) { showToast((e.message || "not running anymore").slice(0, 80), null, "error"); }
      setTimeout(load, 600);
    };
    input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") send(); });
    row.append(input, pillLight("send ↵", send));
    body.append(row);
    pre.scrollTop = pre.scrollHeight;
    const tick = setInterval(() => {
      if (els.pickerModal.hidden) { clearInterval(tick); return; }
      load();
    }, 2000);
  }
  els.pickerModal.hidden = false;
}

// composeErrand: the ＋ errand affordance (§4 user path) — task text +
// account picker; submitting IS the authorization. Guard mode is the app's
// per-task default (no CLI flag exists — §0).
async function composeErrand() {
  let accs = [], cliOK = false;
  try {
    const d = await (await fetch("/api/errands/accounts")).json();
    accs = d.accounts || []; cliOK = !!d.cli;
  } catch (e) {}
  if (!cliOK) { showToast("aside CLI not installed — see the PORTALS tab", null, "error"); return; }
  if (!accs.length) { showToast("no aside accounts signed in", null, "error"); return; }
  els.pickerTitle.textContent = "Run an errand (Aside, guard mode)";
  const body = els.pickerBody; body.innerHTML = "";
  const ta = el("textarea", "asktext-area");
  ta.placeholder = 'what should the browser do? e.g. "cancel the X subscription on the ooda account"';
  ta.rows = 3;
  // account picker: labels only (ids ride in option values); a single
  // signed-in account needs no picker at all — the hint names it.
  const sel = document.createElement("select");
  sel.className = "pp-in errand-acct";
  accs.forEach((a) => {
    const opt = document.createElement("option");
    opt.value = a.id; opt.textContent = a.label;
    if (a.current) opt.selected = true;
    sel.append(opt);
  });
  const actions = el("div", "asktext-actions errand-actions");
  const submit = pill("Run →", async () => {
    const text = ta.value.trim();
    if (!text) return;
    closePicker();
    try {
      await postJSONOk("/api/errands", { text, account: sel.value });
      showToast("errand queued — receipt in the feed", null, "info");
    } catch (e) { showToast((e.message || "couldn't queue errand").slice(0, 80), null, "error"); }
    loadFeed();
  });
  actions.append(el("span", "asktext-hint", "runs serially · " + (accs.length === 1 ? accs[0].label : accs.length + " accounts")));
  if (accs.length > 1) actions.append(sel);
  actions.append(submit);
  body.append(ta, actions);
  ta.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit.click(); }
    else if (e.key === "Escape") { e.preventDefault(); closePicker(); }
  });
  els.pickerModal.hidden = false;
  ta.focus();
}
if (els.feedErrandBtn) els.feedErrandBtn.addEventListener("click", composeErrand);
