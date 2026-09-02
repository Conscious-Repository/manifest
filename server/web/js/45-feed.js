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
let feedCache = { items: [], signals: [], proposals: [], portalItems: [], consumeItems: [], receipts: [], bankPending: [] };
let feedLoadError = false; // a network failure must render as an error, not a fake "inbox zero" (§C2)

// ⚠ THE STALE-PAINT RACE. /api/feed is the slow endpoint of the two and CONSUME
// is one chip-click away, painting off /api/consume instead. Without a token the
// in-flight inbox response lands AFTER the consume render, sees
// state.feedFilter === "consume", finds the consume lane empty (its unread count
// is 0 the moment you've read everything) and paints "Nothing here." over the
// whole CONSUME surface — with a hard reload the only way back. So: every async
// load CLAIMS a token before it awaits, and a load whose token has since been
// superseded returns without touching feedCache or the DOM. loadConsume takes
// the same token (46-consume.js) so the two surfaces share one ordering.
let feedRenderToken = 0;
function feedClaimRender() { return ++feedRenderToken; }
function feedRenderStale(token) { return token !== feedRenderToken; }

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
  bank: (rows) => bankPendingCardEl(rows),
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
  { kind: "agentQuestions", slice: (c) => (c.signals || []).filter(isAgentQuestions) },
  { kind: "planReady", slice: (c) => (c.signals || []).filter(isPlanReady) },
  { kind: "delegationDone", slice: (c) => (c.signals || []).filter(isDelegationDone) },
  { kind: "proposal", slice: (c) => c.proposals },
  // BANK: unfiled transactions from the linked accounts — ONE card holding
  // the inline pickers (owner ask 2026-08-31: address them from the feed too)
  { kind: "bank", slice: (c) => ((c.bankPending || []).length ? [c.bankPending] : []) },
  { kind: "notice", slice: (c) => c.portalItems },
  // CONSUME: subscribed reading. Capped like the signals strip — a week of
  // newsletters must not bury the things that actually want a decision. The
  // rest live behind the CONSUME view, which is where reading belongs anyway.
  { kind: "consume", slice: (c) => (c.consumeItems || []).filter(consumeQueued).slice(0, CONSUME_CAP) },
];
const FEED_TAIL_LANES = [ // after the empty-state check, like today
  { kind: "finding", slice: (c) => c.items },
  { kind: "receipt", slice: (c) => c.receipts },
];

function showFeed() {
  loadFeed();
  ensureLivePoll(); // a dig/ask spooled from here is watched without leaving the tab
}

async function loadFeed() {
  const token = feedClaimRender();
  // CONSUME is its own surface over its own endpoint — the reading backlog is
  // not an inbox and does not want the inbox's empty states.
  if (feedFilter() === "consume") { renderFeedFilters(); await loadConsume(token); refreshFeedBadge(); return; }
  // Drop the cached approval registries so the next card built pulls the LIVE
  // rock ladder (goals edited elsewhere in-session must not serve a stale
  // rock list into the payload editor's typeahead).
  apprAionReg = null; apprReReg = null;
  let next = null, badge = 0;
  try {
    const r = await fetch("/api/feed?status=" + FEED_STATUS);
    if (!r.ok) throw new Error("HTTP " + r.status);
    const d = await r.json();
    next = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [], portalItems: d.portalItems || [], consumeItems: d.consumeItems || [], receipts: d.receipts || [], bankPending: d.bankPending || [] };
    badge = d.badge || 0;
  } catch (e) {
    next = null;
  }
  // Someone else owns the surface now (the CONSUME chip, another loadFeed).
  // Nothing below this line may run — not the cache write, not the paint.
  if (feedRenderStale(token)) return;
  if (next) {
    feedCache = next;
    feedLoadError = false;
    setBadge(els.feedNavBadge, badge);
    diffDigests(feedCache.items); // catch digests landed while unpolled
  } else {
    feedCache = { items: [], signals: [], proposals: [], portalItems: [], consumeItems: [], receipts: [], bankPending: [] };
    feedLoadError = true;
  }
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
  // CONSUME owns els.feedList whenever its chip is lit — renderConsume paints
  // there instead. Even with the token above, renderFeed must be structurally
  // incapable of writing the inbox's empty state over that surface.
  if (feedFilter() === "consume") return;
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
  // ⚠ the empty check used to look ONLY at the "finding" lane — a receipts-only
  // inbox (nothing else pending, findings empty) still had errand cards waiting
  // in FEED_TAIL_LANES, and this returned before ever painting them: "Inbox
  // zero" while the nav badge disagreed. It must ask whether ANY tail lane has
  // something, not just findings.
  const tailHasCards = FEED_TAIL_LANES.some((lane) => laneVisible(lane.kind) && lane.slice(feedCache).length);
  if (!host.children.length && !tailHasCards) {
    if (feedLoadError) {
      const err = el("div", "ro-row empty feed-load-error");
      err.append(el("span", null, "Couldn't load the feed — check the connection."));
      err.append(pillLight("retry", loadFeed));
      host.appendChild(err);
    } else {
      host.appendChild(emptyRow(
        filter === "proposal" ? "Nothing awaiting approval."
          : filter ? "Nothing here." : "Inbox zero — nothing awaiting you."));
    }
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
  const title = el("span", "cp-clickable", sg.entity || sg.label);
  title.title = "open it on the TASKS board";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  const card = cardShell({
    kind: "artifact delegation-done",
    chips: [el("span", "type-chip micro-label type-artifact", "delegated"),
      sg.harness ? el("span", "harness-chip micro-label", sg.harness) : null],
    title,
    why: sg.artifactRef || sg.artifactPath
      ? "delegated work came back with an artifact — read it, then close the task or send it back out"
      : "delegated work finished — read the run report, then close the task or send it back out",
    meta: ["ready for review", sg.harness].filter(Boolean).join("  ·  "),
  });
  const view = pillLight("view →", () => openResult(sg, sg.entity));
  view.classList.add("verdict-primary");
  card.append(cardActions([
    view,
    pillLight("open task →", () => { location.hash = sg.actHref || "#/tasks"; }),
    pillLight("Done ✓", () => signalAction("/api/tasks/check", { id: sg.goalId, checked: true }, card)),
    pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)),
  ]));
  return card;
}
// planReadyCard (todo-panel Phase 4): the assigned agent's PLAN landed in the
// todo's record — review it in the panel, edit by hand, then fire. The card
// auto-clears when the go-phase run outranks plan-ready (or the todo closes).
function planReadyCard(sg) {
  const title = el("span", "cp-clickable", sg.entity || sg.label);
  title.title = "review the plan in the task panel";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  const card = cardShell({
    kind: "artifact plan-ready",
    chips: [el("span", "type-chip micro-label type-artifact", "plan"),
      sg.harness ? el("span", "harness-chip micro-label", sg.harness) : null],
    title,
    why: "the agent drafted a plan — review it, edit it in place if needed, then fire to execute",
  });
  const review = pillLight("review plan →", () => { location.hash = sg.actHref || "#/tasks"; });
  review.classList.add("verdict-primary");
  card.append(cardActions([review,
    pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)),
  ]));
  return card;
}

// agentQuestionsCard — the agent needs answers before it can plan; the
// questions are IN the todo's thread. Auto-clears when you answer (the ball
// moves) or a newer brief lands.
function agentQuestionsCard(sg) {
  const title = el("span", "cp-clickable", sg.entity || sg.label);
  title.title = "open the thread";
  title.onclick = () => { location.hash = sg.actHref || "#/tasks"; };
  const card = cardShell({
    kind: "artifact agent-questions",
    chips: [el("span", "type-chip micro-label type-artifact", "questions"),
      sg.harness ? el("span", "harness-chip micro-label", sg.harness) : null],
    title,
    why: "the agent has questions before it can plan — answer them in the task's thread",
  });
  const ans = pillLight("answer in the thread →", () => { location.hash = sg.actHref || "#/tasks"; });
  ans.classList.add("verdict-primary");
  card.append(cardActions([
    ans,
    pillLight("snooze 7d", () => signalAction("/api/feed/signal/snooze", { id: sg.id, days: 7 }, card)),
    pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash }, card)),
  ]));
  return card;
}

// signalAction — a signal card's clearing verb (Done ✓ / dismiss / snooze).
// The condition itself is recomputed server-side, so loadFeed() always
// converges to truth; cardDecide only makes a REFUSED write visible instead
// of the card silently vanishing then quietly reappearing on the next poll.
async function signalAction(url, body, cardEl) {
  if (cardEl) await cardDecide(cardEl, url, body);
  loadFeed();
}

// feedVerdict — the moment a card gets its verdict it collapses to a one-line
// stub (§11): the verb, the title struck through, and undo. The zero-inbox
// count stays honest without the item vanishing irreversibly.
async function feedVerdict(card, it, verb, status) {
  // optimistic: the stub swaps in the instant you click — the write follows
  // behind it. feedPost's r.ok check is what fixes the swallowed-4xx bug this
  // used to have: a raw fetch() here never threw on a refused write, so a
  // blocked verdict read as "saved" (the stub stayed, the item was never
  // actually re-statused). A refusal now puts the card back and says why.
  const stub = el("div", "feed-stub");
  stub.append(el("span", "feed-stub-verb micro-label", verb), el("span", "feed-stub-title", it.title));
  const undo = el("button", "feed-stub-undo", "undo");
  undo.onclick = () => feedAction(it.id, { status: "new" });
  stub.append(undo);
  card.replaceWith(stub);
  const ok = await feedPost(`/api/feed/${encodeURIComponent(it.id)}/status`, { status });
  if (!ok) { stub.replaceWith(card); loadFeed(); return; }
  refreshFeedBadge();
}

// portalCardEl renders the third feed card kind: an externally-sourced portal
// notice, built entirely from the deterministic poll cache (no LLM). A ClickUp
// day collapses to one digest (assigned-to-you block first, then per-list
// groups); a Benchling change is one item card with a jump link. Dismiss (and
// jump, for items) are the only actions — portals are read-only to their source.
function portalCardEl(pc) {
  const isDigest = pc.type === "portal-digest";
  const card = cardShell({
    kind: "portal-card" + (pc.pinned ? " pinned" : ""),
    dataset: { portalId: pc.id },
    chips: [
      pc.pinned ? el("span", "pin-chip micro-label", "pinned") : null,
      el("span", "type-chip micro-label type-portal", pc.portal), // muted source tag
      pc.change ? el("span", "portal-change-chip micro-label " + portalChangeClass(pc.change), pc.change) : null,
    ],
    title: pc.title,
    date: pc.date,
    why: !isDigest && pc.detail ? pc.detail : null,
    meta: !isDigest && pc.actor ? "by " + pc.actor : null,
  });

  if (isDigest) {
    if ((pc.forYou || []).length) {
      card.append(el("div", "portal-subhead micro-label", "assigned to you / mentions you"));
      pc.forYou.forEach((ln) => card.append(portalLineRow(ln)));
    }
    (pc.groups || []).forEach((g) => {
      card.append(el("div", "portal-subhead micro-label", g.list));
      (g.lines || []).forEach((ln) => card.append(portalLineRow(ln)));
    });
  }

  const acts = [];
  if (!isDigest && pc.url) acts.push(pillLight("jump →", () => window.open(pc.url, "_blank")));
  acts.push(pillLight("Dismiss", () => portalDismiss(pc.id, card)));
  card.append(cardActions(acts));
  return card;
}

// portalChangeClass — an arbitrary status transition ("Backlog → In Progress")
// must never become a raw CSS class (a literal "change-Backlog → In Progress"
// selector); only the two states this card actually distinguishes get their
// own class, every other change value shares .change-status.
function portalChangeClass(change) {
  return /^(new|edited)$/.test(change) ? "change-" + change : "change-status";
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
  if (cardEl) await cardDecide(cardEl, "/api/portals/item/dismiss", { id });
  loadFeed();
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
  // only a real external URL makes the title a link; an artifact's local
  // `artifacts/library/…` reference opens in the note view via "view →" instead.
  const external = /^https?:\/\//i.test(it.link || "");
  let title;
  if (external) title = linkEl(it.title, it.link);
  else if (it.artifactPath || it.artifactRef) { title = el("span", "cp-clickable", it.title); title.onclick = () => openResult(it, it.title); }
  else title = el("span", null, it.title);

  const meta = el("div", "feed-meta");
  const fav = external ? faviconFor(it.link) : null;
  if (fav) meta.append(fav);
  meta.append(el("span", null, [it.source || it.domain, it.agent].filter(Boolean).join("  ·  ")));

  const card = cardShell({
    kind: [it.type === "artifact" && "artifact", it.type === "digest" && "digest",
      pinned && "pinned", it.status === "discarded" && "discarded"].filter(Boolean).join(" "),
    chips: [
      pinned ? el("span", "pin-chip micro-label", "pinned") : null,
      el("span", "type-chip micro-label type-" + it.type, it.type),
      it.harness ? el("span", "harness-chip micro-label", it.harness) : null, // federation source
      it.confidence ? el("span", "conf micro-label conf-" + it.confidence, it.confidence) : null,
    ],
    title,
    date: it.date,
    why: it.why, // the reason you care — lead with it, emphasized
    body: [
      it.body && (pinned || it.type === "artifact") ? Object.assign(el("pre", "feed-body"), { textContent: it.body }) : null,
      it.vaultNote ? el("div", "feed-saved", "✓ saved to " + it.vaultNote) : null,
    ],
    meta,
  });

  const acts = [];
  // the full brief, rendered legibly (harness file or vault note — openResult
  // picks the medium; a card with neither ref falls through with no "view →")
  if (it.artifactPath || it.artifactRef) acts.push(pillLight("view →", () => openResult(it, it.title)));
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
    acts.push(discard);
    acts.push(pillLight("→ task", () => feedToTodo(it.id))); // catch it on the TASKS board (Inbox)
    if (it.type !== "digest") acts.push(pillLight("dig →", () => feedDig(it.id))); // spool a deeper run
    // curate → the public feed. A NEW action, not a verdict: it says
    // "subscribers should read this", and leaves the card's status alone.
    // Only for a card that points at a real article — there is nothing to
    // fetch, and nothing to link subscribers to, without one.
    if (external) acts.push(curatePill(it, card));
  } else {
    acts.push(pillLight("Restore", () => feedAction(it.id, { status: "new" })));
  }
  card.append(cardActions(acts));
  return card;
}

// curatePill — the bridge into the public curation feed. One click fetches the
// whole article behind the card's link and writes it as an extrinsic/ note,
// exactly as the CONSUME lane's curate does; the note is what feed.xml serves.
// The note is the annotation subscribers read above the piece — asked for via
// the inline .consume-note-row affordance CONSUME already uses (same verb,
// same UI everywhere — replacing FEED's old native dialog; skipping it
// still curates.
function curatePill(it, card) {
  const pill = pillLight("curate", () => {
    if (card.querySelector(".consume-note-row")) return;
    const row = el("div", "consume-note-row");
    const input = inputEl("a note for subscribers? (optional)");
    input.className = "consume-note-input";
    const submit = () => { row.remove(); feedCurate(it, pill, card, input.value.trim()); };
    input.onkeydown = (e) => {
      if (e.key === "Enter") { e.preventDefault(); submit(); }
      if (e.key === "Escape") row.remove();
    };
    row.append(input, pillLight("curate", submit), pillLight("cancel", () => row.remove()));
    card.append(row);
    input.focus();
  });
  return pill;
}

async function feedCurate(it, pill, card, note) {
  pill.disabled = true;
  pill.textContent = "curating…";
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(it.id)}/curate`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ note: note || "" }),
    });
    if (!r.ok) throw new Error((await r.text()) || r.status);
    const d = await r.json().catch(() => ({}));
    // Say WHICH of the two landed: the whole piece, or a link and the note
    // because the fetch came back with a subscribe box instead of an article.
    showToast(d.full ? "Curated in full → " + (d.path || "extrinsic/")
                     : "Curated as a link — the article page didn't yield its text",
      null, "info");
    pill.replaceWith(uncuratePill(it, card));
  } catch (e) {
    pill.disabled = false;
    pill.textContent = "curate";
    showToast("Curate failed: " + (e.message || e), null, "error");
  }
}

// uncuratePill clears the curated marker. The note itself survives — this
// unpublishes, it does not delete.
function uncuratePill(it, card) {
  const pill = pillLight("uncurate", async () => {
    pill.disabled = true;
    try {
      const r = await fetch(`/api/feed/${encodeURIComponent(it.id)}/uncurate`, { method: "POST" });
      if (!r.ok) throw new Error((await r.text()) || r.status);
      showToast("Removed from the public feed — the note stays");
      pill.replaceWith(curatePill(it, card));
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
  const bits = ["account " + rc.account];
  if (rc.durationS) bits.push(rc.durationS + "s");
  if (rc.source === "proposal") bits.push("via approved proposal");

  const card = cardShell({
    kind: "receipt status-" + rc.status,
    chips: [
      el("span", "type-chip micro-label type-receipt", "errand"),
      el("span", "type-chip micro-label type-portal", "aside"), // muted source tag
      el("span", "receipt-status micro-label rc-" + rc.status,
        rc.status === "queued" && rc.queuePos ? "queued · #" + rc.queuePos : rc.status),
    ],
    title: rc.text,
    date: rc.created,
    meta: bits.join("  ·  "),
  });
  if (rc.outcome) card.append(receiptOutcomeEl(rc.outcome));
  if (rc.goalId) {
    const g = el("span", "work-chip cp-clickable", "⚑ " + rc.goalId);
    g.onclick = () => { location.hash = "#/goals/" + encodeURIComponent(rc.goalId); };
    card.append(g);
  }
  const acts = [];
  if (rc.transcript) acts.push(pillLight("transcript →", () => showErrandTranscript(rc)));
  if (rc.status === "queued" || rc.status === "running") {
    acts.push(pillLight("Cancel", () => errandAction("/api/errands/" + rc.id + "/cancel")));
  }
  if (rc.status === "failed" || rc.status === "cancelled") {
    acts.push(pillLight("Retry", () => errandAction("/api/errands/" + rc.id + "/retry")));
  }
  // Clear = acknowledged read-state, not a verdict: the card leaves the inbox
  // and the badge; the record + transcript persist under ALL (§5 audit trail).
  const finished = rc.status === "done" || rc.status === "failed" || rc.status === "cancelled";
  if (finished && !rc.acknowledged) {
    acts.push(pillLight("Clear", () => errandAction("/api/errands/" + rc.id + "/ack")));
  }
  if (acts.length) card.append(cardActions(acts));
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

// composeCurate: the ＋ curate affordance — the SECOND entrance to the curate
// verb the cards already carry. Paste a link from anywhere, say why, and it is
// published to the public feed now: no subscription to the source, no queue in
// between. It lands on the same extrinsic/ note curatePill() writes, through
// the same capability — see consume/curateurl.go.
function composeCurate() {
  els.pickerTitle.textContent = "Curate a link → the public feed";
  const body = els.pickerBody; body.innerHTML = "";
  const fields = el("div", "curate-fields");
  const url = inputEl("paste a link");
  url.type = "url";
  const why = inputEl("why this one? (optional)");
  fields.append(url, why);
  const actions = el("div", "asktext-actions errand-actions");
  const submit = pill("curate →", async () => {
    const link = url.value.trim();
    if (!link) { url.focus(); return; }
    // This call fetches the open web and can take twenty seconds. Say so,
    // rather than leaving a pressed button that looks hung.
    submit.disabled = true; submit.textContent = "curating…";
    try {
      const r = await fetch("/api/consume/curate-url", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: link, note: why.value.trim() }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || r.status);
      const d = await r.json().catch(() => ({}));
      closePicker();
      showToast(curatedToast(d), null, "info");
    } catch (e) {
      submit.disabled = false; submit.textContent = "curate →";
      showToast(("Curate failed: " + (e.message || e)).slice(0, 140), null, "error");
    }
  });
  actions.append(el("span", "asktext-hint", "publishes immediately · full text when the page gives it up"), submit);
  body.append(fields, actions);
  [url, why].forEach((f) => f.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !submit.disabled) { e.preventDefault(); submit.click(); }
    else if (e.key === "Escape") { e.preventDefault(); closePicker(); }
  }));
  els.pickerModal.hidden = false;
  url.focus();
}
if (els.feedCurateBtn) els.feedCurateBtn.addEventListener("click", composeCurate);

// curatedToast says WHICH of the kinds landed — the two-message convention
// feedCurate() uses, extended to the three a pasted link can also be.
function curatedToast(d) {
  const where = " → " + (d.path || "extrinsic/");
  if (d.kind === "paper") return "Curated as a paper — abstract + citation" + where;
  if (d.kind === "episode") return "Curated as an episode — audio attached" + where;
  if (d.kind === "platform") return "Curated as a link with a player" + where;
  if (d.full) return "Curated in full" + where;
  return "Curated as a link — the page didn't yield its text";
}

// ---- BANK lane: unfiled linked-account transactions, addressable in place --
// One card, the $ tab's exact machinery: moneyTargetOptions scopes the
// property picker to the paying entity, the category select reads the chart
// of accounts, and filing goes through the same /statements/row PATCH — so a
// row filed here and a row filed on the $ tab are indistinguishable. The
// caches those pickers read (properties, entities, categories) load lazily
// the first time the card paints on this page.
const BANK_FEED_CAP = 8;
function bankPendingCardEl(rows) {
  const card = cardShell({
    kind: "bank-pending-card",
    title: "Bank feed — " + rows.length + " transaction" + (rows.length === 1 ? "" : "s") + " to file",
  });
  const body = el("div", "bank-pending-rows");
  body.append(el("div", "micro-label", "loading pickers…"));
  card.append(body);
  Promise.all([
    ensureMoneyCats(),
    ensureEntities(),
    (typeof propertyCache !== "undefined" && propertyCache.length) ? null : loadProperties(),
  ]).then(() => {
    body.innerHTML = "";
    rows.slice(0, BANK_FEED_CAP).forEach((r) => body.append(bankPendingRowEl(r)));
    if (rows.length > BANK_FEED_CAP) {
      const more = el("button", "signal-more", "▾ " + (rows.length - BANK_FEED_CAP) + " more in the $ workbench");
      more.onclick = () => { location.hash = "#/properties/money"; };
      body.append(more);
    }
  }).catch(() => {
    // ⚠ this used to have no .catch at all — a picker cache that failed to
    // load left the card stuck on "loading pickers…" forever, with no way to
    // tell the difference between "still loading" and "never going to load".
    body.innerHTML = "";
    body.append(el("div", "micro-label", "couldn't load the pickers — file these from the $ workbench instead"));
  });
  const open = el("button", "linkish", "$ workbench →");
  open.onclick = () => { location.hash = "#/properties/money"; };
  card.append(cardActions([open]));
  return card;
}

function bankPendingRowEl(r) {
  const row = el("div", "bank-pending-row");
  row.append(el("span", "re-money-date", (r.date || "").slice(5)));
  const desc = el("span", "fr-stack");
  desc.append(el("span", "re-money-vendor", r.vendor || "(no description)"));
  if (r.entity) desc.append(el("span", "fr-sub", entityLabel(r.entity)));
  row.append(desc);
  row.append(el("span", "re-money-amt" + (r.inflow ? " inflow" : ""),
    (r.inflow ? "+" : "") + fmtMoneyExact(Math.abs(r.amount || 0))));
  // the $ tab's row shape drives the pickers; state/assignments mirror what
  // moneyTargetOptions and the file PATCH expect
  const m = { id: r.id, entity: r.entity, state: r.state, inflow: r.inflow,
    category: r.category || "", amount: r.amount,
    assignments: r.slug ? [{ slug: r.slug, amount: Math.abs(r.amount || 0) }] : [] };
  const patch = async (bodyPatch, sel) => {
    sel.disabled = true;
    try {
      const res = await postJSONOk("/api/realestate/statements/row", bodyPatch);
      if (res.state === "applied") showToast("Filed → " + (entityLabel(r.entity) || "entity") + " history");
      loadFeed(); // the row leaves the card when it files; counts refresh
    } catch (e) { showToast("Couldn't save — " + String(e.message || e).slice(0, 120)); sel.disabled = false; }
  };
  const propSel = document.createElement("select");
  propSel.className = "pp-in re-money-sel";
  moneyTargetOptions(m, propSel, false);
  propSel.value = r.slug || "";
  propSel.onchange = () => patch({
    id: r.id,
    assignments: propSel.value ? [{ slug: propSel.value, amount: Math.abs(r.amount || 0) }] : [],
    state: propSel.value ? "assigned" : "pending", file: true,
  }, propSel);
  row.append(propSel);
  const catSel = document.createElement("select");
  catSel.className = "pp-in re-money-cat";
  const copt = (parent, v, l) => { const o = document.createElement("option"); o.value = v; o.textContent = l; parent.append(o); };
  copt(catSel, "", "category…");
  const kind = r.inflow ? "income" : "expense";
  ["operating", "project"].forEach((cls) => {
    const mine = (moneyCatsCache || []).filter((c) => c.kind === kind && c.class === cls);
    if (!mine.length) return;
    const og = document.createElement("optgroup");
    og.label = cls.toUpperCase();
    mine.forEach((c) => copt(og, c.name, c.name));
    catSel.append(og);
  });
  if (r.category && !(moneyCatsCache || []).some((c) => c.name === r.category)) copt(catSel, r.category, r.category);
  catSel.value = r.category || "";
  catSel.onchange = () => patch({ id: r.id, category: catSel.value, file: true }, catSel);
  row.append(catSel);
  return row;
}
