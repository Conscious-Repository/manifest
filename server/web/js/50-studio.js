// ---- CONTENT STUDIO tab (draft board + inspiration watchlist + runs strip) ----
let studioCache = { board: [], inspiration: [], xPostsFile: "x posts.md" };
let studioQueueCache = null;
const STUDIO_TABS = [["board", "BOARD"], ["queue", "QUEUE"], ["inspiration", "INSPIRATION"]];
function showStudio() {
  if (!state.studioTab) state.studioTab = "board";
  loadStudio();
}
async function loadStudio() {
  try {
    studioCache = await (await fetch("/api/studio")).json();
  } catch (e) { studioCache = { board: [], inspiration: [], xPostsFile: "x posts.md" }; }
  // runs strip: scribe/critic latest outcomes (a quiet "nothing today" ≠ a dead ritual)
  try {
    const runs = await (await fetch("/api/spirits/runs")).json();
    studioCache.runs = (runs.data || runs.runs || runs || []).filter((r) => r.spirit === "scribe" || r.spirit === "critic");
  } catch (e) { studioCache.runs = []; }
  // engine health + next-fire times (§4: a dead engine must look different from a quiet morning)
  try { studioCache.status = await (await fetch("/api/spirits/status")).json(); } catch (e) { studioCache.status = null; }
  try {
    const rits = await (await fetch("/api/spirits/rituals")).json();
    studioCache.nextFire = {};
    (rits.data || rits || []).forEach((rr) => { if (rr.spirit === "scribe" || rr.spirit === "critic") studioCache.nextFire[rr.spirit + "/" + rr.ritual] = rr.nextFire || rr.next || ""; });
  } catch (e) { studioCache.nextFire = {}; }
  // §9: tune proposals — what the system has learned, pending your review
  try {
    const ap = await (await fetch("/api/spirits/approvals")).json();
    studioCache.tuneApprovals = (ap.pending || []).filter((p) => p.ritual === "tune");
  } catch (e) { studioCache.tuneApprovals = []; }
  renderStudio();
}
// §7 commission box — free-text instruction (inline [[note]] refs + URLs); spools
// scribe/commission, then auto-spools the critic when the run lands.
function renderCommissionBox() {
  const wrap = el("div", "commission-box");
  wrap.append(el("div", "reading-strip-head", "Commission a post"));
  const ta = el("textarea", "commission-input");
  ta.placeholder = "reference [[a note]] and https://… — comb for my auxiliary thoughts on the subject and propose a post";
  const btn = pill("Commission →", () => {
    const t = ta.value.trim(); if (!t) return;
    studioPost("/api/studio/commission", { instruction: t }).then(() => {
      showToast("commissioned — scribe is drafting, the critic will audit", null, "info");
      ta.value = "";
      commissionAutoSpool();
    }).catch(() => {});
  });
  wrap.append(ta, btn);
  return wrap;
}
async function commissionAutoSpool() {
  // poll up to ~90s for the commission run to finish, then run the critic (§7)
  for (let i = 0; i < 30; i++) {
    await new Promise((r) => setTimeout(r, 3000));
    try {
      const runs = await (await fetch("/api/spirits/runs")).json();
      const list = runs.data || runs.runs || runs || [];
      const c = list.find((r) => r.spirit === "scribe" && r.ritual === "commission");
      if (c && c.outcome && c.outcome !== "running") {
        await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit: "critic", ritual: "audit-drafts" }) });
        showToast("commission drafted — critic auditing now", null, "info");
        loadStudio();
        return;
      }
    } catch (e) {}
  }
}
function renderTuningPanel() {
  const wrap = el("div", "studio-tuning");
  wrap.append(el("div", "reading-strip-head", "What the system is learning — " + studioCache.tuneApprovals.length + " tune proposal" + (studioCache.tuneApprovals.length === 1 ? "" : "s") + " pending"));
  studioCache.tuneApprovals.forEach((p) => {
    const row = el("div", "tuning-row");
    row.append(el("span", "tuning-what", p.action));
    row.append(el("span", "feed-meta", [p.agent, p.applyPath].filter(Boolean).join(" · ")));
    row.append(pillLight("review →", () => { pendingApprovalFocus = p.id; location.hash = "#/feed"; }));
    wrap.append(row);
  });
  return wrap;
}
async function studioRunNow(spirit, ritual) {
  try {
    const r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual }) });
    if (r.status === 409) { showToast(`${spirit}/${ritual} already running`, null, "info"); return; }
    if (!r.ok) throw new Error(await r.text());
    showToast(`${spirit}/${ritual} queued — the engine runs it within ~5s`, null, "info");
  } catch (e) { showToast("run-now failed: " + (e.message || e), null, "error"); }
}
function renderStudio() {
  // tabs
  els.studioTabs.innerHTML = "";
  STUDIO_TABS.forEach(([val, label]) => {
    const b = el("button", "filter-chip" + (state.studioTab === val ? " on" : ""), label);
    b.onclick = () => { state.studioTab = val; if (val === "queue") studioQueueCache = null; renderStudio(); };
    els.studioTabs.append(b);
  });
  // runs strip: engine health + run-now + recent scribe/critic outcomes (§4)
  els.studioRuns.innerHTML = "";
  const st = studioCache.status;
  if (st && st.engineAlive === false) els.studioRuns.append(el("span", "studio-run-chip srun-down", "⚠ engine down"));
  else if (st) els.studioRuns.append(el("span", "studio-run-chip srun-up", "engine live"));
  [["scribe", "mine-and-draft"], ["critic", "audit-drafts"]].forEach(([sp, rit]) => {
    const b = el("button", "pill light", "▶ run " + sp);
    const nf = (studioCache.nextFire || {})[sp + "/" + rit];
    if (nf) b.title = "next auto-fire: " + nf;
    b.onclick = () => studioRunNow(sp, rit);
    els.studioRuns.append(b);
  });
  (studioCache.runs || []).slice(0, 3).forEach((r) => {
    const chip = el("span", "studio-run-chip");
    chip.append(el("span", "srun-name", r.spirit + "/" + r.ritual));
    chip.append(el("span", "srun-outcome outcome-" + (r.outcome || ""), r.outcome || "—"));
    chip.title = (r.summary || "") + "  ·  " + (r.started || r.finished || "");
    els.studioRuns.append(chip);
  });

  const host = els.studioBody; host.innerHTML = "";
  if (state.studioTab === "queue") return renderStudioQueue(host);
  if (state.studioTab === "inspiration") return renderStudioInspiration(host);
  // board: drafts grouped by the status vocabulary (draft → passed → queued → posted, + killed)
  const board = studioCache.board || [];
  if ((studioCache.tuneApprovals || []).length) host.append(renderTuningPanel());
  host.append(renderCommissionBox());
  if (!board.length) { host.append(emptyRow("No drafts yet — commission one above, or scribe drafts each morning.")); return; }
  // group by the status vocabulary
  const order = ["passed", "pending-audit", "queued", "posted", "killed", "dismissed"];
  const labels = { passed: "Passed — approve to queue", "pending-audit": "Pending audit", queued: "Queued", posted: "Posted", killed: "Killed", dismissed: "Dismissed" };
  const byStatus = {};
  board.forEach((d) => { (byStatus[d.status] = byStatus[d.status] || []).push(d); });
  order.forEach((st) => {
    const items = byStatus[st];
    if (!items || !items.length) return;
    const head = labels[st] + "  —  " + items.length;
    if (st === "killed" || st === "dismissed") {
      const det = el("details", "killed-group");
      det.append(el("summary", "reading-strip-head", head));
      items.forEach((d) => det.append(studioBoardCard(d)));
      host.append(det);
    } else {
      host.append(el("div", "reading-strip-head", head));
      items.forEach((d) => host.append(studioBoardCard(d)));
    }
  });
}
// statusWord maps a draft's lifecycle status to the §4 vocabulary chip.
function statusWord(s) { return ({ "pending-audit": "draft" })[s] || s; }

// buildDraftFeedback is the shared feedback capture used on both feed and board
// cards: a single "judge" affordance that opens a commentary box; the note is
// written to the draft's feedback (steers the next scribe run + feeds tuning).
function buildDraftFeedback(draftId, existing) {
  const wrap = el("div", "draft-feedback");
  if (existing && existing.trim()) wrap.append(el("div", "draft-fb-text", "your note: " + existing.trim()));
  const judge = pillLight("judge", () => {
    if (wrap.querySelector(".draft-judge-box")) return; // toggle guard
    const box = el("div", "draft-judge-box");
    const inp = el("input", "draft-fb-input");
    inp.type = "text";
    inp.placeholder = "your note on this draft — what's off, or do more of…";
    const save = () => {
      const t = inp.value.trim();
      if (!t) { box.remove(); return; }
      studioPost(`/api/studio/draft/${encodeURIComponent(draftId)}/feedback`, { text: t, tags: [] })
        .then(() => { showToast("noted — the next run honors it", null, "info"); box.remove(); });
    };
    inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); save(); } });
    const saveBtn = pillLight("save", save);
    box.append(inp, saveBtn);
    wrap.append(box);
    inp.focus();
  });
  wrap.append(judge);
  return wrap;
}

function studioBoardCard(d) {
  const card = el("div", "feed-card draft status-" + d.status);
  const top = el("div", "feed-top");
  top.append(el("span", "type-chip type-draft", statusWord(d.status)));
  if (d.score) top.append(el("span", "draft-score", d.score + "/10"));
  if (d.format && d.format !== "single") top.append(el("span", "draft-format", d.format));
  if (d.commissioned) top.append(el("span", "draft-format", "commissioned"));
  if (d.overruled) top.append(el("span", "draft-format", "overruled"));
  card.append(top);
  const tweet = el("div", "draft-tweet"); tweet.textContent = d.edited || d.text; card.append(tweet);
  if (d.edited) card.append(el("div", "feed-meta", "✎ edited (original preserved)"));
  if (d.seed) card.append(el("div", "feed-meta", "from your drafts: " + d.seed));
  if (d.scorecard) {
    const sc = el("details", "draft-scorecard");
    sc.append(el("summary", null, "scorecard"));
    const pre = el("pre", "feed-body"); pre.textContent = d.scorecard; sc.append(pre);
    card.append(sc);
  }
  if (d.postedUrl) card.append(el("div", "feed-saved", "✓ posted · " + d.postedUrl));

  // inline edit box (hidden until Edit)
  const editWrap = el("div", "draft-edit"); editWrap.hidden = true;
  const ta = el("textarea", "draft-edit-input"); ta.value = d.edited || d.text;
  const eActs = el("div", "feed-actions");
  eActs.append(
    pill("Save edit", async () => { await studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/edit`, { text: ta.value.trim(), approvalId: d.approvalId || "" }); showToast("edit saved", null, "info"); loadStudio(); }),
    pillLight("Cancel", () => { editWrap.hidden = true; }),
  );
  editWrap.append(ta, eActs);

  const actions = el("div", "feed-actions");
  let consumeCb = null;
  if (d.status === "passed") {
    if (d.seed) { const lbl = el("label", "seed-consume"); consumeCb = el("input"); consumeCb.type = "checkbox"; consumeCb.checked = true; lbl.append(consumeCb, document.createTextNode(" consume the seed from # drafts")); card.append(lbl); }
    actions.append(
      pill("Approve → queue", () => boardApprove(d, consumeCb)),
      pillLight("Edit", () => { editWrap.hidden = !editWrap.hidden; }),
      pillLight("Dismiss", () => studioDismiss(d.id, d.approvalId, loadStudio)),
    );
  } else if (d.status === "killed") {
    actions.append(pillLight("Overrule → queue", () => studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/overrule`, {}).then(() => { showToast("queued (overruled) ✓ — teaches the critic", null, "info"); loadStudio(); })));
  } else if (d.status === "queued") {
    actions.append(pillLight("mark posted", () => askText("Mark posted", "paste the tweet URL (optional)", (url) => studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/mark-posted`, { url: url.trim() }).then(loadStudio))));
  }
  card.append(editWrap, buildDraftFeedback(d.id, d.feedback), actions);
  return card;
}

async function boardApprove(d, consumeCb) {
  if (!d.approvalId) { showToast("no linked approval — overrule or queue from the feed", null, "error"); return; }
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(d.approvalId)}/confirm`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }); }
  catch (e) { showToast("approve failed", null, "error"); return; }
  if (d.seed && consumeCb && consumeCb.checked) { try { await studioPost(`/api/studio/draft/${encodeURIComponent(d.id)}/consume-seed`, {}); } catch (e) {} }
  showToast("queued ✓ — view Queue", () => { state.studioTab = "queue"; studioQueueCache = null; renderStudio(); }, "info");
  loadStudio();
}
function renderStudioInspiration(host) {
  host.append(el("div", "studio-purpose", "Accounts you study. Your commentary and saved posts teach the pattern skill what you admire."));
  // add an account
  const addWrap = el("div", "insp-add");
  const inp = el("input", "queue-add-input"); inp.type = "text"; inp.placeholder = "add an account by handle (e.g. paulg)…";
  const add = () => { const h = inp.value.trim().replace(/^@/, ""); if (!h) return; studioPost("/api/studio/account/add", { handle: h }).then(() => { showToast("@" + h + " queued — the engine backfills it shortly", null, "info"); inp.value = ""; }); };
  inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  addWrap.append(inp, pillLight("add account", add));
  host.append(addWrap);

  const accts = studioCache.inspiration || [];
  if (!accts.length) { host.append(emptyRow("No accounts yet — add one above.")); return; }
  accts.filter((a) => a.isSelf).forEach((a) => host.append(inspAccountCard(a, true)));
  accts.filter((a) => !a.isSelf).forEach((a) => host.append(inspAccountCard(a, false)));
}
function inspAccountCard(a, isSelf) {
  const card = el("div", "feed-card" + (isSelf ? " insp-self" : ""));
  const top = el("div", "feed-top");
  top.append(el("span", "feed-title", "@" + a.handle));
  if (isSelf) top.append(el("span", "type-chip type-draft", "your account"));
  if (a.followers) top.append(el("span", "feed-meta", fmtCount(a.followers) + " followers"));
  if (!isSelf) { const b = el("button", "pill light", "this is me"); b.onclick = () => studioPost(`/api/studio/account/${encodeURIComponent(a.handle)}/self`, { on: true }).then(() => { showToast("marked as your account", null, "info"); loadStudio(); }); top.append(b); }
  card.append(top);
  if (a.bio) card.append(el("div", "feed-why", a.bio));
  // editable commentary (not for the self account — it's not a pattern to admire)
  if (!isSelf) {
    const cw = el("div", "insp-commentary");
    cw.append(el("div", "feed-meta", "your commentary — what you admire about this account:"));
    const ta = el("textarea", "insp-comment-input"); ta.value = a.commentary || ""; ta.placeholder = "e.g. his zoom-out QTs land because…";
    const save = pillLight("save", () => studioPost(`/api/studio/account/${encodeURIComponent(a.handle)}/commentary`, { text: ta.value }).then(() => showToast("commentary saved", null, "info")));
    cw.append(ta, save);
    card.append(cw);
  }
  // top posts collapsed by default (declutter) — expand to view/annotate
  const posts = a.topPosts || [];
  if (posts.length) {
    const det = el("details", "insp-posts");
    det.append(el("summary", "insp-posts-summary", "top posts by views (" + posts.length + ")"));
    posts.forEach((p) => {
      const row = el("div", "insp-post");
      row.append(el("div", "insp-post-text", p.text));
      const m = el("div", "feed-meta insp-post-meta");
      m.append(el("span", null, fmtCount(p.views) + " views · " + fmtCount(p.likes) + " likes"));
      if (p.url) m.append(linkEl("open →", p.url));
      m.append(pillLight("annotate", () => askText("Annotate this post", "why it's worth studying — teaches the pattern skill what you admire", (note) => studioPost("/api/studio/annotate", { postId: p.id, note: note.trim() }).then(() => showToast("annotated", null, "info")))));
      row.append(m);
      det.append(row);
    });
    card.append(det);
  }
  return card;
}
function fmtCount(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, "") + "K";
  return String(n);
}

// ---- Queue tab: live-editable x posts.md (§1/§3) ----
async function loadStudioQueue() {
  try { studioQueueCache = await (await fetch("/api/studio/queue")).json(); }
  catch (e) { studioQueueCache = { sections: { drafts: [], queue: [], posted: [] } }; }
  renderStudio();
}
function renderStudioQueue(host) {
  if (!studioQueueCache) { host.append(emptyRow("loading…")); loadStudioQueue(); return; }
  const q = studioQueueCache;
  const sec = q.sections || { drafts: [], queue: [], posted: [] };
  if (sec.needsMigration) {
    const banner = el("div", "studio-migrate");
    banner.append(el("div", "studio-migrate-msg", "Your x posts.md still uses the old single # queue. Restructure it into # drafts (your scratch ideas) / # queue (ready to post) / # posted — your current bullets move to # drafts, nothing is lost."));
    banner.append(pill("Restructure now", async () => {
      await studioPost("/api/studio/migrate", {});
      showToast("x posts.md restructured ✓", null, "info");
      studioQueueCache = null; loadStudioQueue();
    }));
    host.append(banner);
  }
  const sections = [["drafts", "# drafts", "scratch ideas — the scribe may develop these"], ["queue", "# queue", "approved, ready to post"], ["posted", "# posted", "posted"]];
  sections.forEach(([key, label, hint]) => {
    const bullets = sec[key] || [];
    host.append(el("div", "reading-strip-head", label + " — " + bullets.length + "  ·  " + hint));
    bullets.forEach((b) => host.append(studioBulletRow(key, b)));
    if (key !== "posted") host.append(studioAddRow(key));
  });
}
function studioBulletRow(section, bullet) {
  const row = el("div", "queue-bullet");
  const editable = section !== "posted";
  const textWrap = el("div", "queue-bullet-text");
  textWrap.textContent = bullet.text.replace(/^- /, "");
  if (editable) {
    textWrap.classList.add("cp-clickable");
    textWrap.onclick = () => beginBulletEdit(row, section, bullet);
  }
  row.append(textWrap);
  const acts = el("div", "queue-bullet-acts");
  if (section === "queue") acts.append(pillLight("mark posted", () => {
    askText("Mark posted", "paste the tweet URL (optional)", (url) =>
      studioPost("/api/studio/queue/mark-posted", { bullet: bullet.text, url: url.trim() }).then(() => { studioQueueCache = null; loadStudioQueue(); }));
  }));
  if (editable) acts.append(pillLight("delete", () => {
    if (!confirm("Delete this bullet?")) return;
    studioPost("/api/studio/bullet/delete", { section, bullet: bullet.text }).then(() => { studioQueueCache = null; loadStudioQueue(); });
  }));
  row.append(acts);
  return row;
}
function beginBulletEdit(row, section, bullet) {
  row.innerHTML = "";
  const ta = el("textarea", "queue-edit-input"); ta.value = bullet.text.replace(/^- /, "");
  const acts = el("div", "feed-actions");
  acts.append(
    pill("Save", () => studioPost("/api/studio/bullet/edit", { section, original: bullet.text, replacement: "- " + ta.value.trim() })
      .then(() => { studioQueueCache = null; loadStudioQueue(); })
      .catch(() => { studioQueueCache = null; loadStudioQueue(); })),
    pillLight("Cancel", () => { studioQueueCache = null; loadStudioQueue(); }),
  );
  row.append(ta, acts);
  ta.focus();
}
function studioAddRow(section) {
  const row = el("div", "queue-add");
  const inp = el("input", "queue-add-input"); inp.type = "text"; inp.placeholder = "+ add a " + section.replace(/s$/, "") + " bullet…";
  const add = () => { const v = inp.value.trim(); if (!v) return; studioPost("/api/studio/bullet/add", { section, bullet: "- " + v }).then(() => { studioQueueCache = null; loadStudioQueue(); }); };
  inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  row.append(inp, pillLight("add", add));
  return row;
}

// openArtifact opens an artifact's library file in the universal note view —
// legacy path for a harness tree still inside the vault; returns to the feed.
function openArtifact(path) {
  _noteReturn = "#/feed";
  openNoteByPath(path);
}

// ---- ONE legible artifact viewer (owner ask 2026-08-12) ----
// Delegated work must be READABLE where it lands, not raw markdown in a <pre>.
// docModal renders through the same markdown renderer the note view uses
// (headings, lists, code, wikilinks) into the quiet artifact modal, so a
// harness brief, a vault brief and a run report all read alike.
function docModal(title, subtitle, raw) {
  const overlay = el("div", "cmdbar");
  const back = el("div", "cmdbar-backdrop");
  const panel = el("div", "cmdbar-card harness-artifact");
  overlay.append(back, panel);
  const close = () => overlay.remove();
  back.onclick = close;
  const head = el("div", "appr-diff-label");
  head.append(el("span", "artifact-doc-title", title || "artifact"));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = close;
  head.append(x);
  panel.append(head);
  if (subtitle) panel.append(el("div", "artifact-doc-sub", subtitle));
  const body = el("div", "harness-artifact-body artifact-doc");
  body.appendChild(renderMarkdown(raw || "", null, { readOnly: true }));
  panel.append(body);
  const esc = (e) => { if (e.key === "Escape") { close(); document.removeEventListener("keydown", esc); } };
  document.addEventListener("keydown", esc);
  document.body.append(overlay);
}

// openRunModal — view a run report IN PLACE from any surface (todos chip,
// board card, delegation-done feed card): the report body IS the artifact for
// runs that wrote nothing else. No navigation; rendered, not raw.
async function openRunModal(runId) {
  let run;
  try {
    const r = await fetch("/api/spirits/runs/" + encodeURIComponent(runId));
    if (!r.ok) throw new Error("HTTP " + r.status);
    run = await r.json();
  } catch (e) { showToast("Couldn't open the run: " + (e.message || e), null, "error"); return; }
  const s = run.summary || {};
  docModal((s.spirit || "?") + " / " + (s.ritual || "?"),
    [s.outcome, s.request].filter(Boolean).join("  ·  "),
    run.body || "(empty report)");
}

// openHarnessArtifact — the post-split viewer: the harness tree lives outside
// the vault, so artifact briefs read through the spirits file API (read-only
// allow-list) into the legible modal. Never the vault note view (two-media
// rule). `harness` narrows the federated read to the tree that wrote it.
async function openHarnessArtifact(ref, harness, title) {
  let content = "";
  try {
    const q = "/api/spirits/file?path=" + encodeURIComponent(ref) + (harness ? "&harness=" + encodeURIComponent(harness) : "");
    const r = await fetch(q);
    if (!r.ok) throw new Error("HTTP " + r.status);
    content = (await r.json()).content || "";
  } catch (e) { showToast("Couldn't open artifact: " + (e.message || e), null, "error"); return; }
  docModal(title || ref.replace(/^artifacts\/library\//, ""), ref, content);
}

// openResult — the ONE entry point every surface uses to view delegated work:
// the feed artifact card, the delegation-done card, the board's chip. Prefers
// the deliverable (the library brief) over the narration (the run report), and
// honors the two-media rule: a vault-side brief opens in the note view.
function openResult(d, title) {
  if (!d) return;
  if (d.artifactPath) { openArtifact(d.artifactPath); return; }
  if (d.artifactRef) { openHarnessArtifact(d.artifactRef, d.harness, title); return; }
  if (d.runId) { openRunModal(d.runId); return; }
  showToast("No result file yet for this work", null, "info");
}

// feedDig: "dig →" — spool a deeper run for the originating spirit; findings
// come back as new inbox items. Never navigates away from the feed.
async function feedDig(id) {
  let r;
  try { r = await fetch(`/api/feed/${encodeURIComponent(id)}/dig`, { method: "POST" }); }
  catch (e) { showToast("Dig failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    const d = await r.json().catch(() => ({}));
    showToast(`${d.spirit || "spirit"}/${d.ritual || "ritual"} is already running — view`, () => { location.hash = "#/spirits/runs"; }, "info");
    return;
  }
  if (!r.ok) { showToast("Dig failed: " + ((await r.text()) || r.status), null, "error"); return; }
  const d = await r.json().catch(() => ({}));
  showToast(`${d.spirit}/${d.ritual} queued — view`, () => { location.hash = "#/spirits/runs"; }, "info");
  ensureLivePoll(); // watch it land back in the inbox
}
async function feedAction(id, body) {
  setSaveState("saving");
  try { await fetch(`/api/feed/${encodeURIComponent(id)}/status`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed(); // re-renders + refreshes the badge from the same response
}
async function feedToTodo(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(id)}/to-todo`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "promote failed");
    setSaveState("saved");
    showToast("Caught on the TODOS board → Inbox");
  } catch (e) { setSaveState("error"); showToast("→ todo failed: " + e.message, null, "error"); }
  loadFeed();
}

async function feedSaveToVault(id) {
  setSaveState("saving");
  try {
    const r = await fetch(`/api/feed/${encodeURIComponent(id)}/save-to-vault`, { method: "POST" });
    if (!r.ok) throw new Error((await r.text()) || "save failed");
    setSaveState("saved");
  } catch (e) { setSaveState("error"); showToast("Save to vault failed: " + e.message, null, "error"); }
  loadFeed();
}

// ---- run now / ask a scout (spooled request; engine picks it up within ~5s) ----
// spiritPick opens the spirit/ritual picker (one area per spirit, its rituals
// as items) and calls onPick("spirit","ritual"). askRitual, when given, is
// picked automatically if present so "Ask a scout" lands on options-scout's
// research ritual without a needless second tap.
async function spiritPick(onPick) {
  // the catalog can be needed before SPIRITS was ever opened (Ask-a-scout lives
  // in FEED now) — load it lazily.
  if (!spiritStatusCache) await loadSpiritsStatusCacheOnly();
  const spirits = (spiritStatusCache || {}).spirits || {};
  const groups = Object.keys(spirits).sort().map((sp) => ({
    area: sp,
    items: (spirits[sp] || []).map((rit) => ({ id: sp + "/" + rit, text: rit })),
  })).filter((g) => g.items.length);
  if (!groups.length) { showToast("No spirit/ritual found in the excalibur tree.", null, "error"); return; }
  openPicker("Run a ritual now", groups, (id) => {
    const [sp, rit] = id.split("/");
    onPick(sp, rit);
  }, "No rituals found.");
}
async function loadSpiritsStatusCacheOnly() {
  try { spiritStatusCache = await (await fetch("/api/spirits/status")).json(); } catch (e) {}
}
// spiritSpool drops a run request. It holds NO button state — the run's status
// lives in the files (queued spool → running report). A 409 means the same
// spirit/ritual is already active (the double-spool guard). From FEED the user
// is never yanked away (feed-central §3: the loop closes in the feed) — a toast
// links to the live row instead; from SPIRITS we jump to RUNS as before.
async function spiritSpool(spirit, ritual, request) {
  const onFeed = location.hash === "#/feed";
  let r;
  try { r = await fetch("/api/spirits/run-now", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ spirit, ritual, request: request || "" }) }); }
  catch (e) { showToast("Run request failed: " + (e.message || e), null, "error"); return; }
  if (r.status === 409) {
    showToast(`${spirit}/${ritual} is already running — view`, () => { location.hash = "#/spirits/runs"; }, "info");
    if (!onFeed) location.hash = "#/spirits/runs";
    return;
  }
  if (!r.ok) { showToast("Run request failed (" + r.status + ")", null, "error"); return; }
  if (onFeed) {
    showToast(`${spirit}/${ritual} queued — view`, () => { location.hash = "#/spirits/runs"; }, "info");
  } else {
    location.hash = "#/spirits/runs";
    loadSpiritRuns(); // show the queued row immediately
  }
  ensureLivePoll();   // and watch it through to done
}
function spiritRunNow() {
  spiritPick((sp, rit) => spiritSpool(sp, rit, ""));
}
// spiritAskScout: pick a spirit/ritual, then take a free-form request via an
// inline box (no browser prompt). The request rides the spool into the prompt.
function spiritAskScout() {
  spiritPick((sp, rit) => {
    askText(`Request for ${sp} / ${rit}`,
      'e.g. "buy a mechanical keyboard under $200 — find 5 options"',
      (request) => { if (request.trim()) spiritSpool(sp, rit, request.trim()); });
  });
}
async function fetchSpiritRuns() {
  try {
    const d = await (await fetch("/api/spirits/runs")).json();
    return { data: d.data || [], queued: d.queued || [] };
  } catch (e) { return { data: [], queued: [] }; }
}

// askText — a small inline text dialog (reuses the picker modal chrome), the
// replacement for prompt() in spirits flows (plan §6).
function askText(title, placeholder, onSubmit) {
  els.pickerTitle.textContent = title;
  const body = els.pickerBody; body.innerHTML = "";
  const ta = el("textarea", "asktext-area"); ta.placeholder = placeholder; ta.rows = 3;
  const actions = el("div", "asktext-actions");
  const submit = pill("Send →", () => { closePicker(); onSubmit(ta.value); });
  actions.append(el("span", "asktext-hint", "⌘↵ to send"), submit);
  body.append(ta, actions);
  ta.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); closePicker(); onSubmit(ta.value); }
    else if (e.key === "Escape") { e.preventDefault(); closePicker(); }
  });
  els.pickerModal.hidden = false;
  ta.focus();
}

// ---- run reports (artifacts/runs/) — live strip + finished list ----
async function loadSpiritRuns() {
  spiritRuns = await fetchSpiritRuns();
  renderSpiritRuns();
  ensureLivePoll();
}
// spiritWeekSpend — Σ spentUsd over runs started in the last 7 days (the
// section head + crumb meta both read this; no endpoint involved).
function spiritWeekSpend() {
  const cutoff = Date.now() - 7 * 86400000;
  let sum = 0;
  (spiritRuns.data || []).forEach((r) => {
    const t = new Date(r.started).getTime();
    if (!isNaN(t) && t >= cutoff) sum += r.spentUsd || 0;
  });
  return sum;
}

// Re-renders the LIST only; never touches the open report detail (so a live
// re-render doesn't close what you're reading).
function renderSpiritRuns() {
  const host = els.spiritRunsList; host.innerHTML = "";
  const running = (spiritRuns.data || []).filter((r) => r.outcome === "running");
  const queued = spiritRuns.queued || [];
  const finished = (spiritRuns.data || []).filter((r) => r.outcome !== "running");

  // the live strip: one line per running/queued item, at the very top (§12)
  const liveHost = document.getElementById("spiritLive");
  if (liveHost) {
    liveHost.innerHTML = "";
    running.forEach((r) => liveHost.append(liveRunRow(r, true)));
    queued.forEach((q) => liveHost.append(liveRunRow(q, false)));
  }

  const ws = document.getElementById("spiritWeekSpend");
  if (ws) { const n = spiritWeekSpend(); ws.textContent = n > 0 ? "$" + n.toFixed(2) + " this week" : ""; }
  if (typeof updateSpiritsCrumb === "function") updateSpiritsCrumb();

  if (!finished.length && !running.length && !queued.length) {
    host.appendChild(emptyRow("No runs yet — cast a skill (press /) or wait for a scheduled ritual."));
    return;
  }
  // the five most recent; the rest behind a quiet toggle (owner call)
  const SHOW = 5;
  (spiritRunsExpanded ? finished : finished.slice(0, SHOW)).forEach((r) => host.append(spiritRunRow(r)));
  if (finished.length > SHOW) {
    const t = el("button", "sprt-more",
      spiritRunsExpanded ? "▴ show recent only" : "▸ all runs · " + (finished.length - SHOW) + " more");
    t.onclick = () => { spiritRunsExpanded = !spiritRunsExpanded; renderSpiritRuns(); };
    host.append(t);
  }
}
let spiritRunsExpanded = false;

// liveRunRow — ONE line (prototype): dot · RUNNING · spirit · ritual — request · elapsed
function liveRunRow(item, running) {
  const row = el("div", "sprt-live" + (running ? " running" : ""));
  row.append(el("span", "live-dot " + (running ? "on" : "wait")));
  row.append(el("span", "sprt-live-state", running ? "running" : "queued"));
  if (item.harness) row.append(el("span", "harness-chip", item.harness));
  row.append(el("span", "sprt-live-text",
    item.spirit + " · " + item.ritual + (item.request ? " — " + item.request : "")));
  if (running) {
    row.append(el("span", "sprt-live-elapsed", elapsedSince(item.started)));
    row.classList.add("clicky");
    row.onclick = () => openSpiritRun(item.id);
    row.title = "watch the report append";
  }
  return row;
}
function elapsedSince(iso) {
  const d = new Date(iso); if (isNaN(d)) return "";
  let s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  const m = Math.floor(s / 60); s = s % 60;
  return m ? `${m}m ${s}s` : `${s}s`;
}

// spiritRunRow — the prototype's quiet row: chip · title · when, over a
// 6px charge bar + $spent / $ceiling. The card's request/step/model noise
// lives in the report detail a click away.
function spiritRunRow(r) {
  const row = el("div", "sprt-run");
  const top = el("div", "sprt-run-top");
  top.append(el("span", "run-outcome oc-" + (r.outcome || "").replace(/[^a-z-]/g, ""), r.outcome || "?"));
  if (r.harness) top.append(el("span", "harness-chip", r.harness)); // federation source
  top.append(el("span", "sprt-run-title", `${r.spirit} / ${r.ritual}`));
  top.append(el("span", "sprt-run-when", fmtWhen(r.started)));
  row.append(top);
  const pct = r.ceilingUsd > 0 ? Math.min(100, Math.round((r.spentUsd / r.ceilingUsd) * 100)) : 0;
  const bar = el("span", "charge-bar");
  const fill = el("span", "charge-fill" + (pct >= 100 ? " over" : ""));
  fill.style.width = pct + "%";
  bar.appendChild(fill);
  const cr = el("div", "charge-row");
  cr.append(bar, el("span", "charge-label", `$${r.spentUsd.toFixed(4)} / $${r.ceilingUsd.toFixed(2)}`));
  row.append(cr);
  row.onclick = () => openSpiritRun(r.id);
  return row;
}
async function openSpiritRun(id) {
  openRunId = id;
  let run;
  try { run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(id))).json(); }
  catch (e) { return; }
  const host = els.spiritRunDetail; host.innerHTML = ""; host.hidden = false;
  const head = el("div", "run-detail-head");
  head.append(el("span", "run-title", id));
  const promptBtn = pillLight("Show assembled prompt", () => toggleSpiritPrompts(id, promptBtn));
  const closeBtn = pillLight("✕ Close", () => { host.hidden = true; openRunId = null; });
  head.append(promptBtn, closeBtn);
  host.append(head);
  const body = el("pre", "run-report"); body.id = "runReportBody";
  body.textContent = run.body || "";
  host.append(body);
  const prompts = el("div", "run-prompts"); prompts.id = "runPrompts-" + id; prompts.hidden = true;
  host.append(prompts);
  host.scrollIntoView({ behavior: "smooth", block: "start" });
  ensureLivePoll(); // a running report will keep appending
}
// Refresh the open report body in place. Called each poll tick (which only runs
// while a run is active), so the finishing tick pulls the terminal report too.
async function refreshOpenRun() {
  if (!openRunId) return;
  try {
    const run = await (await fetch("/api/spirits/runs/" + encodeURIComponent(openRunId))).json();
    const body = document.getElementById("runReportBody");
    if (body) body.textContent = run.body || "";
  } catch (e) {}
}
// The §6.5 affordance: the EXACT model input per turn, preserved verbatim.
async function toggleSpiritPrompts(id, btn) {
  const box = document.getElementById("runPrompts-" + id);
  if (!box) return;
  if (!box.hidden) { box.hidden = true; btn.textContent = "Show assembled prompt"; return; }
  if (!box.childElementCount) {
    let turns = [];
    try { turns = (await (await fetch("/api/spirits/runs/" + encodeURIComponent(id) + "/prompt")).json()).data || []; }
    catch (e) {}
    if (!turns.length) { box.appendChild(emptyRow("No preserved prompts found for this run.")); }
    turns.forEach((t) => {
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — SYSTEM`));
      const s = el("pre", "run-report prompt"); s.textContent = t.system; box.appendChild(s);
      box.appendChild(el("div", "panel-subhead", `TURN ${t.turn} — USER`));
      const u = el("pre", "run-report prompt"); u.textContent = t.user; box.appendChild(u);
    });
  }
  box.hidden = false; btn.textContent = "Hide assembled prompt";
}
