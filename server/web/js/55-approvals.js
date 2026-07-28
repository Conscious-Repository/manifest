// ---- spirit approvals (artifacts/approvals/ — the ONE inbox) ----
// Spirits file proposals via the write_approval cast; Confirm/Reject only
// RECORD the decision (a folder move on the excalibur tree). Nothing sends.
let pendingApprovalFocus = null; // approval id to scroll to in FEED (Studio tuning panel "review →")

// approvalCardEl: a pending approval as a first-class FEED card — evidence,
// per-type guards, current-vs-proposed diff, and Confirm/Reject inline
// (approvals-move-to-feed plan; formerly the SPIRITS approvals panel card).
function approvalCardEl(a) {
  const card = el("div", "approval-card pinned");
  card.dataset.approvalId = a.id;
  const head = el("div", "appr-head");
  head.append(el("span", "appr-action", a.action), el("span", "appr-agent", a.agent || ""));
  card.append(head);
  if (a.created) card.append(el("div", "feed-meta", fmtWhen(a.created)));

  const actionable = !!a.applyPath;
  // For an actionable proposal the ````proposed payload is rendered as a diff
  // below, so strip it from the human-facing evidence body.
  const bodyText = actionable ? stripProposedFence(a.body) : a.body;
  if (bodyText && bodyText.trim()) { const b = el("pre", "appr-body"); b.textContent = bodyText.trim(); card.append(b); }

  let blocked = false, blockMsg = "";
  const isNewNote = a.type === "create-vault-note";
  const isXQueue = a.type === "append-x-queue";
  const isSkill = a.type === "update-vault-skill";
  let attendees = null; // create-vault-note: the editable people list sent on Confirm
  if (actionable) {
    card.classList.add("actionable");
    const chip = el("div", "appr-apply");
    chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
    card.append(chip);

    if (!a.allowed) {
      blocked = true;
      blockMsg = isNewNote
        ? "apply-path is not a vault-root dated note (YYYY-MM-DD <title>.md) — Confirm is disabled."
        : isXQueue
        ? "apply-path is not the x-posts file — Confirm is disabled."
        : isSkill
        ? "update-vault-skill must target skills/x-content/{SKILL.md, references/<name>.md} and be filed by a tune ritual — Confirm is disabled."
        : "apply-path is outside the allow-list (spirits/*/cornerstone.md, spirits/*/rituals/*.md, chargebook.md) — Confirm is disabled.";
    } else if (/\/cornerstone\.md$/.test(a.applyPath) && frontmatterOf(a.current || "") !== frontmatterOf(a.proposed || "")) {
      // client-side mirror of the server's cornerstone-frontmatter guard
      blocked = true;
      blockMsg = "proposed content changes the cornerstone frontmatter — Confirm will refuse (behavior prose only).";
    }

    // People editor: seed from the auto-linked attendees, let the user fix them.
    if (isNewNote) {
      attendees = parseAttendees(a.proposed || "");
      card.append(buildAttendeeEditor(attendees));
    }

    if (isXQueue) {
      // append-x-queue's proposed is ONLY the bullet — show it, not a whole-file diff
      card.append(el("div", "appr-diff-label", "Appends under # queue in " + a.applyPath));
      const pre = el("pre", "appr-body draft-tweet"); pre.textContent = (a.proposed || "").trim(); card.append(pre);
    } else {
      card.append(el("div", "appr-diff-label", isNewNote ? "New note — will be created at the vault root"
        : isSkill ? "Skill change  ·  current → proposed" : "Proposed change  ·  current → proposed"));
      const diff = renderLineDiff(a.current || "", a.proposed || "");
      card.append(collapsibleBlock(diff, diff.childElementCount));
    }
  }
  if (blocked && blockMsg) card.append(el("div", "appr-blocked", "⚠ " + blockMsg));

  const actions = el("div", "appr-actions");
  const confirmBtn = pill(actionable ? "Confirm & apply" : "Confirm",
    () => spiritApprovalAct(a.id, "confirm", isNewNote ? attendees : null));
  if (blocked) { confirmBtn.disabled = true; confirmBtn.classList.add("disabled"); }
  actions.append(confirmBtn, pillLight("Reject", () => spiritApprovalAct(a.id, "reject")));
  card.append(actions);
  return card;
}

// parseAttendees pulls the [[wikilink]] names from a converted note's attendee
// line (between the frontmatter and "## Transcript").
function parseAttendees(proposed) {
  const m = proposed.match(/^---\n[\s\S]*?\n---\n([\s\S]*?)##\s*Transcript/);
  const head = m ? m[1] : "";
  const names = [];
  const re = /\[\[([^\]]+)\]\]/g;
  let x;
  while ((x = re.exec(head))) names.push(x[1].trim());
  return names;
}

// buildAttendeeEditor renders the people-involved chips + an add box, mutating
// the shared `attendees` array in place so Confirm sends the edited list.
function buildAttendeeEditor(attendees) {
  const wrap = el("div", "appr-attendees");
  wrap.append(el("div", "appr-attendees-label", "People involved — remove or add before confirming"));
  const chips = el("div", "attendee-chips");
  const renderChips = () => {
    chips.innerHTML = "";
    attendees.forEach((name, i) => {
      const c = el("span", "attendee-chip");
      c.append(el("span", "attendee-name", name));
      const x = el("button", "attendee-remove", "✕");
      x.title = "Remove";
      x.onclick = () => { attendees.splice(i, 1); renderChips(); };
      c.append(x);
      chips.append(c);
    });
    if (!attendees.length) chips.append(el("span", "attendee-empty", "none linked"));
  };
  const addRow = el("div", "attendee-add");
  const input = el("input", "attendee-input");
  input.type = "text";
  input.placeholder = "Add a person…  (type [[ to search your vault)";
  attachWikilinkAutocomplete(input); // reuse the vault-aware [[name]] autocomplete
  const commit = () => {
    const v = input.value.trim().replace(/^\[\[/, "").replace(/\]\]$/, "").trim();
    if (v && !attendees.some((n) => n.toLowerCase() === v.toLowerCase())) {
      attendees.push(v);
      renderChips();
    }
    input.value = "";
  };
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); commit(); }
  });
  const addBtn = el("button", "attendee-add-btn", "+ add");
  addBtn.onclick = commit;
  addRow.append(input, addBtn);
  wrap.append(chips, addRow);
  renderChips();
  return wrap;
}

// collapsibleBlock caps a long proposed-note block to a preview, with a toggle
// to expand. Short blocks are returned as-is.
const APPROVAL_COLLAPSE_LINES = 14;
function collapsibleBlock(inner, lineCount) {
  if (lineCount <= APPROVAL_COLLAPSE_LINES) return inner;
  const wrap = el("div", "appr-collapse collapsed");
  wrap.append(inner);
  const toggle = el("button", "appr-expand", `Show full note (${lineCount} lines) ▾`);
  toggle.onclick = () => {
    const collapsed = wrap.classList.toggle("collapsed");
    toggle.textContent = collapsed ? `Show full note (${lineCount} lines) ▾` : "Collapse ▴";
  };
  wrap.append(toggle);
  return wrap;
}

// stripProposedFence removes the ````proposed … ```` block from an approval body
// (it is shown as a diff instead). Handles 3+ backtick fences like the server.
function stripProposedFence(body) {
  if (!body) return body || "";
  const lines = body.split("\n"), out = [];
  let skipping = false, fence = 0;
  for (const line of lines) {
    const m = line.match(/^(`{3,})/);
    if (!skipping) {
      if (m && line.slice(m[1].length).trim() === "proposed") { skipping = true; fence = m[1].length; continue; }
      out.push(line);
    } else if (m && m[1].length >= fence && line.trim() === m[1]) {
      skipping = false;
    }
  }
  return out.join("\n").trim();
}

// frontmatterOf returns the raw text between the leading `---` fences (mirrors
// the server's rawFrontmatter), for the client-side cornerstone guard.
function frontmatterOf(text) {
  if (!text.startsWith("---\n")) return "";
  const idx = text.indexOf("\n---");
  return idx < 0 ? "" : text.slice(4, idx);
}

// renderLineDiff builds a compact LCS line diff (full-file replacement) as a
// scrollable block of +/−/context rows.
function renderLineDiff(oldText, newText) {
  const a = oldText.split("\n"), b = newText.split("\n");
  const n = a.length, m = b.length;
  const dp = Array.from({ length: n + 1 }, () => new Int32Array(m + 1));
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--)
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const wrap = el("div", "appr-diff");
  let i = 0, j = 0, changed = false;
  const push = (kind, text) => {
    const row = el("div", "diff-line diff-" + kind);
    row.append(el("span", "diff-gutter", kind === "add" ? "+" : kind === "del" ? "−" : " "));
    row.append(el("span", "diff-text", text === "" ? " " : text));
    wrap.append(row);
    if (kind !== "ctx") changed = true;
  };
  while (i < n && j < m) {
    if (a[i] === b[j]) { push("ctx", a[i]); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { push("del", a[i]); i++; }
    else { push("add", b[j]); j++; }
  }
  while (i < n) push("del", a[i++]);
  while (j < m) push("add", b[j++]);
  if (!changed) wrap.append(el("div", "diff-line diff-ctx", "(no textual change)"));
  return wrap;
}
function spiritApprovalAct(id, kind, attendees) {
  if (kind === "reject") {
    // inline reason box (no browser prompt); Escape cancels
    askText("Reject — reason (optional)",
      "recorded on the proposal; for warden findings this becomes an accepted exception",
      (reason) => postApprovalDecision(id, "reject", { reason: reason.trim() || "rejected from dashboard" }));
    return;
  }
  const body = (kind === "confirm" && attendees !== null && attendees !== undefined)
    ? { editAttendees: true, attendees } // create-vault-note with the edited people list
    : {};
  postApprovalDecision(id, kind, body);
}
async function postApprovalDecision(id, kind, body) {
  setSaveState("saving");
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(id)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed(); // approvals live in FEED — the decided card resolves in place
}

if (els.feedRunNowBtn) els.feedRunNowBtn.addEventListener("click", spiritRunNow);
if (els.feedAskBtn) els.feedAskBtn.addEventListener("click", spiritAskScout);
