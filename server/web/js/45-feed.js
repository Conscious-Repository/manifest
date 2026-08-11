// ---- FEED: manifest's one inbox (top-level tab, feed-central §1/§4) ----
// INBOX (default) = items awaiting a verdict (new + lapsed snoozes). Keep endorses
// and moves the item to KEPT. Chips are INBOX/KEPT/ALL.
const FEED_VIEWS = [["inbox", "INBOX"], ["kept", "KEPT"], ["all", "ALL"]];
const SIGNAL_CAP = 8; // most-overdue signals shown; the rest fold behind "N more"
let signalsExpanded = false;
let feedCache = { items: [], signals: [], proposals: [], portalItems: [], receipts: [] };

// ---- the §5 card registry: ONE renderer per attention kind, plus the
// proposals lane (an authorization queue, not an attention kind). renderFeed
// dispatches through this table — adding a kind = one renderer + one lane row.
// Receipts are declared (permanent lifecycle, server slot registered) with no
// machinery yet, so their lane renders nothing until an errand loop exists.
const FEED_CARD = {
  signal: (sg) => signalRow(sg),
  proposal: (p) => approvalCardEl(p),
  notice: (pc) => portalCardEl(pc),
  finding: (it) => feedCard(it),
  receipt: (rc) => receiptCardEl(rc),
};
// main-list lanes in render order (signals render into their own strip above).
const FEED_LANES = [
  { kind: "proposal", slice: (c) => c.proposals, inboxOnly: true },
  { kind: "notice", slice: (c) => c.portalItems, inboxOnly: true },
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
  const view = state.feedView || "inbox";
  try {
    const d = await (await fetch("/api/feed?status=" + view)).json();
    feedCache = { items: d.items || [], signals: d.signals || [], proposals: d.proposals || [], portalItems: d.portalItems || [], receipts: d.receipts || [] };
    setBadge(els.feedNavBadge, d.badge || 0);
    if (view === "inbox") diffDigests(feedCache.items); // catch digests landed while unpolled
  } catch (e) { feedCache = { items: [], signals: [], proposals: [], portalItems: [], receipts: [] }; }
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
    feedCache.signals.slice(0, shown).forEach((sg) => sigHost.appendChild(FEED_CARD.signal(sg)));
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
    if (lane.inboxOnly && view !== "inbox") return;
    lane.slice(feedCache).forEach((c) => host.appendChild(FEED_CARD[lane.kind](c)));
  });
  if (!feedCache.items.length && !feedCache.receipts.length && !host.children.length) {
    host.appendChild(emptyRow(view === "inbox"
      ? "Inbox zero — nothing awaiting a verdict."
      : view === "kept" ? "Nothing kept yet." : "No feed items yet."));
    return;
  }
  FEED_TAIL_LANES.forEach((lane) => {
    if (lane.inboxOnly && view !== "inbox") return;
    lane.slice(feedCache).forEach((c) => host.appendChild(FEED_CARD[lane.kind](c)));
  });
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
  // two verbs only (owner call 2026-08-10): Done ✓ on todo signals, dismiss on
  // everything — the label itself already navigates to the item.
  const act = el("span", "signal-actions");
  if (sg.kind === "todo-stale" || sg.kind === "todo-waiting") {
    act.append(pillLight("Done ✓", () => signalAction("/api/todos/check", { id: sg.goalId, checked: true })));
  }
  act.append(pillLight("dismiss", () => signalAction("/api/feed/signal/dismiss", { id: sg.id, hash: sg.hash })));
  row.append(act);
  return row;
}
async function signalAction(url, body) {
  try { await postJSON(url, body); } catch (e) {}
  loadFeed();
}

// feedVerdict — the moment a card gets its verdict it collapses to a one-line
// stub (§11): the verb, the title struck through, and undo. The zero-inbox
// count stays honest without the item vanishing irreversibly.
async function feedVerdict(card, it, verb, status) {
  setSaveState("saving");
  try {
    await fetch(`/api/feed/${encodeURIComponent(it.id)}/status`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ status }),
    });
    setSaveState("saved");
  } catch (e) { setSaveState("error"); loadFeed(); return; }
  const stub = el("div", "feed-stub");
  stub.append(el("span", "feed-stub-verb", verb), el("span", "feed-stub-title", it.title));
  const undo = el("button", "feed-stub-undo", "undo");
  undo.onclick = () => feedAction(it.id, { status: "new" });
  stub.append(undo);
  card.replaceWith(stub);
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
  if (it.harness) top.append(el("span", "harness-chip", it.harness)); // federation source
  // only a real external URL makes the title a link; an artifact's local
  // `artifacts/library/…` reference opens in the note view via "view →" instead.
  const external = /^https?:\/\//i.test(it.link || "");
  let title;
  if (external) title = linkEl(it.title, it.link);
  else if (it.artifactPath) { title = el("span", "cp-clickable", it.title); title.onclick = () => openArtifact(it.artifactPath); }
  else if (it.artifactRef) { title = el("span", "cp-clickable", it.title); title.onclick = () => openHarnessArtifact(it.artifactRef); }
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
  else if (it.artifactRef) actions.append(pillLight("view →", () => openHarnessArtifact(it.artifactRef)));
  if (it.status !== "discarded") {
    const keep = pillLight("Keep", () => feedVerdict(card, it, "kept", "kept"));
    keep.classList.add("verdict-primary");
    actions.append(keep);
    if (it.status !== "kept") actions.append(pillLight("Discard", () => feedVerdict(card, it, "discarded", "discarded")));
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

// ---- receipts: the fourth attention kind, backed by <dataDir>/errands/
// (errands-aside §5 as amended). App-local records — NEVER kept/discarded,
// no expiry; queued shows its place in line, running shows a live dot,
// failed/cancelled offer retry (no continue — the CLI emits no session ids).
function receiptCardEl(rc) {
  if (rc.cardKind === "aion-publish") return aionPublishReceiptEl(rc);
  const card = el("div", "feed-card receipt status-" + rc.status);
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-receipt", "errand"));
  top.append(el("span", "type-chip type-portal", "aside")); // muted source tag
  const status = el("span", "receipt-status rc-" + rc.status,
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

// aionPublishReceiptEl: an AION publish outcome — permanent trail; failures
// carry Clear (acknowledged read-state), successes are pre-acknowledged.
function aionPublishReceiptEl(rc) {
  const card = el("div", "feed-card receipt status-" + rc.status);
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-receipt", "publish"));
  top.append(el("span", "type-chip type-portal", "aion.bio"));
  top.append(el("span", "receipt-status rc-" + (rc.status === "ok" ? "done" : "failed"),
    rc.status === "ok" ? "pushed" : "failed @ " + (rc.stage || "?")));
  if (rc.at) top.append(el("span", "feed-date", fmtFeedDate(rc.at)));
  card.append(top);
  card.append(el("div", "feed-title",
    rc.status === "ok"
      ? "Published " + ((rc.files || []).length || "no") + " file(s) → portal"
      : "Publish failed" + (rc.commit ? " — commit " + rc.commit.slice(0, 7) + " kept locally" : "")));
  const meta = el("div", "feed-meta");
  const bits = [];
  if (rc.commit) bits.push("commit " + rc.commit.slice(0, 7));
  if ((rc.files || []).length) bits.push(rc.files.map((f) => f.split("/").pop()).join(", "));
  if (bits.length) meta.append(el("span", null, bits.join("  ·  ")));
  card.append(meta);
  if (rc.error) { const e = el("pre", "appr-body"); e.textContent = rc.error; card.append(e); }
  const acts = el("div", "feed-actions");
  acts.append(pillLight("open AION →", () => { location.hash = "#/aion/settings"; }));
  if (!rc.acknowledged) {
    acts.append(pillLight("Clear", async () => {
      try { await fetch("/api/aion/publishes/" + encodeURIComponent(rc.id) + "/ack", { method: "POST" }); } catch (e) {}
      loadFeed();
    }));
  }
  card.append(acts);
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

async function studioPost(path, body) {
  setSaveState("saving");
  try { const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); if (!r.ok) throw new Error(await r.text()); setSaveState("saved"); return await r.json().catch(() => ({})); }
  catch (e) { setSaveState("error"); showToast("Studio action failed: " + (e.message || e), null, "error"); throw e; }
}
