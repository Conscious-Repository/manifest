// ---- spirit approvals (artifacts/approvals/ — the ONE inbox) ----
// Spirits file proposals via the write_approval cast; Confirm/Reject only
// RECORD the decision (a folder move on the excalibur tree). Nothing sends.
let pendingApprovalFocus = null; // approval id to scroll to in FEED (deep-link "review →")

// ---- the proposal draft store ----------------------------------------
// renderFeed() empties the list and rebuilds every card, and a finishing agent
// run triggers exactly that from the 3s poll. The editor's working copy used to
// live in a closure, so an edited total could silently vanish mid-edit. The
// draft outlives the repaint; the server's copy only replaces it when the owner
// has nothing unsaved.
const apprDrafts = new Map(); // approval id → {p, base, dirty, saving, err}
function apprDraft(a) {
  const base = JSON.stringify(a.reContractPayload || {});
  let d = apprDrafts.get(a.id);
  if (!d) { d = { p: JSON.parse(base), base, dirty: false, saving: false, err: "" }; apprDrafts.set(a.id, d); }
  else if (d.base !== base && !d.dirty) { d.p = JSON.parse(base); d.base = base; }
  return d;
}
// apprDraftsKeep — drop drafts whose proposal is gone (confirmed / rejected).
function apprDraftsKeep(ids) {
  const live = new Set(ids || []);
  [...apprDrafts.keys()].forEach((id) => { if (!live.has(id)) apprDrafts.delete(id); });
  if (apprSel && !live.has(apprSel.id)) apprSel = null;
}

// ---- the inspector selection -----------------------------------------
// {id, kind, i} — kind is contract|contractor|milestone|task|alloc. Rows carry
// the same key in data-sel-key, so a wholesale repaint restores the mark the
// way the AION list does (aionSelId).
let apprSel = null;
const apprSelKey = (sel) => sel ? sel.id + "::" + sel.kind + "::" + (sel.i == null ? -1 : sel.i) : "";
function apprSelect(sel) {
  const same = apprSel && apprSelKey(apprSel) === apprSelKey(sel);
  apprSel = same ? null : sel;
  apprPaintSel();
  renderApprovalInspector();
}
function apprPaintSel() {
  const key = apprSelKey(apprSel);
  (els.feedList ? els.feedList.querySelectorAll("[data-sel-key]") : []).forEach((n) => {
    n.classList.toggle("sel", !!key && n.dataset.selKey === key);
  });
}

// approvalCardEl: a pending approval as a first-class FEED card — evidence,
// per-type guards, current-vs-proposed diff, and Confirm/Reject inline
// (approvals-move-to-feed plan; formerly the SPIRITS approvals panel card).
function approvalCardEl(a) {
  const card = el("div", "approval-card pinned");
  card.dataset.approvalId = a.id;
  const actionable = !!a.applyPath;
  const isResolve = a.type === "aion-resolve" || a.type === "re-resolve"; // closed-loop: flip one backlog line
  const isRe = a.type === "re-backlog" || a.type === "re-resolve"; // the real-estate domain twins (````re fence)
  const isAion = a.type === "aion-backlog" || a.type === "aion-heuristic" || isRe || isResolve;
  const isReContract = a.type === "re-contract"; // intake (overhaul §5): adjust-amounts card
  const isGoals = a.type === "goals-item"; // goals placement (§12 2026-08-19)
  // a team-portal proposal (§12 2026-08-24). It has NO applyPath — the effect
  // is a team-store Decide, not a file write — so it renders as prose plus the
  // standard Confirm/Reject, with no diff to show.
  const isPortalProp = a.type === "portal-proposal";
  if (isReContract) {
    // the contract card leads with money, not prose: the head is the kind
    // chip, the provenance, and the document — the action line's content
    // (contractor, scope, amount) is the total block right below it
    card.classList.add("re-intake-card");
    const doc = (a.reContractPayload || {}).doc || "";
    const head = el("div", "appr-head re-intake-head");
    head.append(el("span", "type-chip micro-label type-company", (a.reContractPayload || {}).kind || "contract"));
    head.append(el("span", "appr-agent", [a.agent, a.created ? fmtWhen(a.created) : ""].filter(Boolean).join(" · ")));
    if (a.harness) head.append(el("span", "harness-chip", a.harness)); // federation source
    if (doc.startsWith("sha256:")) {
      const dl = el("a", "pp3-link re-intake-doc", "document ↗");
      dl.href = "/api/realestate/files/" + doc.slice(7);
      dl.target = "_blank";
      head.append(dl);
    }
    card.append(head);
  } else {
    const head = el("div", "appr-head");
    head.append(el("span", "appr-action", a.action), el("span", "appr-agent", a.agent || ""));
    if (a.harness) head.append(el("span", "harness-chip", a.harness)); // federation source
    card.append(head);
    if (a.created) card.append(el("div", "feed-meta", fmtWhen(a.created)));
  }
  // For an actionable proposal the ````proposed payload is rendered as a diff
  // below (and an aion payload as its editable form), so strip the fence from
  // the human-facing evidence body.
  // A proposal whose body carries a payload fence its DECLARED type does not
  // render is misfiled — the spirit named the type/apply_path in prose instead
  // of passing them as write_approval arguments. Printing the raw JSON at the
  // owner reads as a broken card; strip it and say what actually went wrong.
  const strayFence = apprStrayFence(a, isReContract, isAion);
  let bodyText = isReContract ? stripFence(a.body, "re-contract")
    : isPortalProp ? stripFence(a.body, "portal")
    : isGoals ? stripFence(a.body, "goals")
    : isAion ? stripFence(a.body, isRe ? "re" : "aion") : actionable ? stripProposedFence(a.body) : a.body;
  if (strayFence) bodyText = stripFence(bodyText, strayFence);
  if (bodyText && bodyText.trim() && !isReContract) { const b = el("pre", "appr-body"); b.textContent = bodyText.trim(); card.append(b); }
  let blocked = false, blockMsg = "";
  if (isPortalProp && !a.allowed) {
    // the server already worked out why (settled in the portal first, or that
    // portal is not configured here) — say that rather than a generic refusal
    blocked = true;
    blockMsg = a.goalsErr || "this proposal can no longer be decided from here.";
  }
  if (strayFence) {
    // Confirm on a misfiled proposal writes nothing and files it under
    // approved/, burying it — block the button, not just annotate the card.
    blocked = true;
    blockMsg = "misfiled proposal — the body carries a \u2018" + strayFence +
      "\u2019 payload but the record is typed \u2018" + (a.type || "approval") +
      "\u2019 with no apply-path, so Confirm would write nothing. Reject it and re-run the intake; " +
      "the engine now refuses this shape at the source.";
  }
  const isNewNote = a.type === "create-vault-note";
  const isAppendNote = a.type === "append-vault-note"; // email-sync append the auto-apply refused
  let attendees = null; // create-vault-note: the editable people list sent on Confirm
  let categories = null; // create-vault-note: the editable frontmatter categories
  const titleRef = { value: null }; // create-vault-note: the editable filename title
  if (actionable) {
    card.classList.add("actionable");
    // create-vault-note shows its path via the editable title field below, so the
    // static APPLIES-TO chip is redundant (and would show the pre-edit title).
    if (!isNewNote && !isReContract) {
      const chip = el("div", "appr-apply");
      chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
      card.append(chip);
    }

    if (!a.allowed) {
      blocked = true;
      blockMsg = isNewNote
        ? "apply-path is not a vault-root dated note (YYYY-MM-DD <title>.md) — Confirm is disabled."
        : isAppendNote
        ? "append target is not a log/ dated note or the proposal carries no thread id — Confirm is disabled."
        : isGoals
        ? "apply-path is not the vault-root goals.md or the payload is malformed — Confirm is disabled."
        : isReContract
        ? "apply-path is not a system/realestate/contracts/<slug>.md record or the payload is malformed — Confirm is disabled."
        : isRe
        ? "apply-path is not the real-estate decision log (system/realestate/backlog.md) or the payload is malformed — Confirm is disabled."
        : isAion
        ? "apply-path is not the aion record file (system/aion/backlog.md · heuristics.md) or the payload is malformed — Confirm is disabled."
        : "apply-path is outside the allow-list (spirits/*/cornerstone.md, spirits/*/rituals/*.md, chargebook.md) — Confirm is disabled.";
    } else if (/\/cornerstone\.md$/.test(a.applyPath) && frontmatterOf(a.current || "") !== frontmatterOf(a.proposed || "")) {
      // client-side mirror of the server's cornerstone-frontmatter guard
      blocked = true;
      blockMsg = "proposed content changes the cornerstone frontmatter — Confirm will refuse (behavior prose only).";
    }

    // Title + people + category editors: fix the filename, the auto-linked
    // attendees, and the note's categories before confirming. Categories
    // drive automation — `aion` makes the written note auto-extract
    // tasks/decisions/heuristics into FEED.
    if (isNewNote) {
      card.append(buildTitleEditor(a.applyPath, titleRef));
      attendees = parseAttendees(a.proposed || "");
      card.append(buildAttendeeEditor(attendees));
      categories = parseCategories(a.proposed || "");
      card.append(buildCategoryEditor(categories));
    }

    if (isGoals && a.goalsPayload) {
      // a goals placement: the editable payload + the EXACT diff Confirm
      // writes (the server computes Proposed against the live file, so a
      // stale anchor or duplicate reads as a refusal before the click)
      card.append(buildGoalsEditor(a));
    } else if (isReContract && a.reContractPayload) {
      // the intake card: the owner ALWAYS adjusts amounts before confirming
      card.append(buildReContractEditor(a, bodyText));
    } else if (isResolve && a.aionPayload) {
      // a closed-loop resolve: the evidence is the body above; the editable
      // bits are the matching title and the outcome/date — kind is fixed
      card.append(buildResolveEditor(a));
    } else if (isAion && a.aionPayload) {
      // an aion extraction candidate: editable payload + the exact record
      // line Confirm appends (the app renders it — nothing else is written)
      card.append(buildAionEditor(a));
    } else if (isAppendNote) {
      // append-vault-note's proposed is ONLY the new message sections — this
      // card renders only when auto-apply refused (rename/mismatch), so say so
      card.append(el("div", "appr-diff-label", "Appends to " + a.applyPath + " — auto-apply was refused, review before confirming"));
      const pre = el("pre", "appr-body"); pre.textContent = (a.proposed || "").trim();
      card.append(collapsibleBlock(pre, (a.proposed || "").split("\n").length));
    } else {
      card.append(el("div", "appr-diff-label", isNewNote ? "New note — will be created at the vault root"
        : "Proposed change  ·  current → proposed"));
      const diff = renderLineDiff(a.current || "", a.proposed || "");
      card.append(collapsibleBlock(diff, diff.childElementCount));
    }
  }
  if (blocked && blockMsg) card.append(el("div", "appr-blocked", "⚠ " + blockMsg));

  const actions = el("div", "appr-actions");
  const confirmBtn = pill(actionable ? "Confirm & apply" : "Confirm",
    async () => {
      // aion/re payload editors: whatever is in the form RIDES the confirm —
      // an unsaved owner/rock edit must never silently drop (2026-08-12 bug:
      // an assigned owner + rock vanished because "save edit" wasn't clicked)
      if (a.__payloadFlush) {
        confirmBtn.disabled = true;
        try { await a.__payloadFlush(); }
        catch (e) { showToast("Couldn't save the edits — " + String(e.message || e).slice(0, 100)); confirmBtn.disabled = false; return; }
        confirmBtn.disabled = false;
      }
      spiritApprovalAct(a.id, "confirm",
        isNewNote ? { attendees, title: titleRef.value, categories } : null);
    });
  if (blocked) { confirmBtn.disabled = true; confirmBtn.classList.add("disabled"); }
  actions.append(confirmBtn, pillLight("Reject", () => spiritApprovalAct(a.id, "reject")));
  // a card whose payload can be internally inconsistent (the contract split
  // vs its total) says so on the button, not only after the click: the
  // editor's own check drives the disabled state and the note beside it
  if (a.__gateBind) {
    const note = el("span", "appr-gate");
    actions.append(note);
    a.__gateBind((ok, text) => {
      confirmBtn.disabled = blocked || !ok;
      confirmBtn.classList.toggle("disabled", blocked || !ok);
      note.textContent = text;
      note.classList.toggle("off", !ok);
    });
  }
  card.append(actions);
  return card;
}

// parseCategories reads the proposed note's frontmatter categories — both
// YAML shapes: inline `categories: [a, b]` and block `categories:\n  - a`.
function parseCategories(proposed) {
  const m = proposed.match(/^---\n([\s\S]*?)\n---/);
  if (!m) return [];
  const lines = m[1].split("\n");
  for (let i = 0; i < lines.length; i++) {
    const km = lines[i].match(/^categories:\s*(.*)$/i);
    if (!km) continue;
    const inline = km[1].trim();
    if (inline) {
      return inline.replace(/^\[|\]$/g, "").split(",").map((s) => s.trim().replace(/^["']|["']$/g, "")).filter(Boolean);
    }
    const out = [];
    for (let j = i + 1; j < lines.length; j++) {
      const bm = lines[j].match(/^\s+-\s+(.+)$/);
      if (!bm) break;
      out.push(bm[1].trim().replace(/^["']|["']$/g, ""));
    }
    return out;
  }
  return [];
}

// buildCategoryEditor renders the frontmatter-category chips + add box +
// one-tap suggestions, mutating `categories` in place so Confirm sends the
// edited list. `aion` is called out — it wires the note into extraction.
function buildCategoryEditor(categories) {
  const wrap = el("div", "appr-attendees appr-categories");
  wrap.append(el("div", "appr-attendees-label",
    "Categories — aion / real-estate auto-extract tasks + decisions into FEED after confirm"));
  const chips = el("div", "attendee-chips");
  const suggestions = ["aion", "real-estate"]; // the two extraction pipelines
  const LIVE = { aion: "tag aion → the note auto-extracts into the AION backlog pipeline",
    "real-estate": "tag real-estate → the note auto-extracts into the RE backlog pipeline" };
  const renderChips = () => {
    chips.innerHTML = "";
    categories.forEach((name, i) => {
      const c = el("span", "attendee-chip" + (LIVE[name.toLowerCase()] ? " cat-live" : ""));
      c.append(el("span", "attendee-name", name));
      const x = el("button", "attendee-remove", "✕");
      x.title = "Remove category";
      x.onclick = () => { categories.splice(i, 1); renderChips(); };
      c.append(x);
      chips.append(c);
    });
    suggestions.filter((s) => !categories.some((c) => c.toLowerCase() === s)).forEach((s) => {
      const add = el("button", "attendee-add-btn cat-suggest", "＋ " + s);
      add.title = LIVE[s] || ("add " + s);
      add.onclick = () => { categories.push(s); renderChips(); };
      chips.append(add);
    });
  };
  const addRow = el("div", "attendee-add");
  const input = el("input", "attendee-input");
  input.type = "text";
  input.placeholder = "Add a category…";
  const commit = () => {
    const v = input.value.trim();
    if (v && !categories.some((c) => c.toLowerCase() === v.toLowerCase())) {
      categories.push(v);
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

// parseAttendees pulls the [[wikilink]] names from a converted note's
// attendee/participants line (between the frontmatter and the first "## "
// section heading — "## Transcript" or an email note's first message section).
function parseAttendees(proposed) {
  const m = proposed.match(/^---\n[\s\S]*?\n---\n([\s\S]*?)^##\s/m);
  const head = m ? m[1] : "";
  const names = [];
  const re = /\[\[([^\]]+)\]\]/g;
  let x;
  while ((x = re.exec(head))) names.push(x[1].trim());
  return names;
}

// buildTitleEditor renders an editable title field for a create-vault-note. The
// date prefix (from the recording; a date RANGE for email thread notes) stays
// fixed; the owner edits the title portion of the filename. Mutates ref.value
// in place so Confirm sends it; a live preview shows the resulting filename.
function buildTitleEditor(applyPath, ref) {
  const m = /^(\d{4}-\d{2}-\d{2}(?: - \d{4}-\d{2}-\d{2})?) (.+)\.md$/.exec(applyPath || "");
  const date = m ? m[1] : "";
  ref.value = m ? m[2] : "";
  const wrap = el("div", "appr-title");
  wrap.append(el("div", "appr-title-label", "Title — edit before confirming"));
  const row = el("div", "appr-title-row");
  if (date) row.append(el("span", "appr-title-date", date));
  const input = el("input", "appr-title-input");
  input.type = "text";
  input.value = ref.value;
  input.spellcheck = false;
  const preview = el("code", "appr-title-preview");
  const sync = () => {
    ref.value = input.value;
    const clean = input.value.replace(/[\[\]<>:"/\\|?*]/g, "").replace(/\s+/g, " ").trim();
    preview.textContent = (date ? date + " " : "") + (clean || "…") + ".md";
  };
  input.oninput = sync;
  row.append(input, el("span", "appr-title-ext", ".md"));
  wrap.append(row, preview);
  sync();
  return wrap;
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

// apprStrayFence names a payload fence the card's declared type will NOT
// render — the signature of a proposal filed with the wrong type (and so with
// no apply-path). Returns "" when the body and the type agree.
function apprStrayFence(a, isReContract, isAion) {
  if (isReContract || isAion) return "";
  // portal-proposal legitimately has no apply-path — its effect is a team-store
  // write — so the "no apply-path means misfiled" inference does not hold here
  if (a.type === "portal-proposal") return "";
  for (const lang of ["re-contract", "aion", "re"]) {
    if (stripFence(a.body, lang) !== (a.body || "").trim()) return lang;
  }
  return "";
}

// stripFence removes a ````<lang> … ```` block from an approval body (it is
// shown as a diff / form instead). Handles 3+ backtick fences like the server.
function stripFence(body, lang) {
  if (!body) return body || "";
  const lines = body.split("\n"), out = [];
  let skipping = false, fence = 0;
  for (const line of lines) {
    const m = line.match(/^(`{3,})/);
    if (!skipping) {
      if (m && line.slice(m[1].length).trim() === lang) { skipping = true; fence = m[1].length; continue; }
      out.push(line);
    } else if (m && m[1].length >= fence && line.trim() === m[1]) {
      skipping = false;
    }
  }
  return out.join("\n").trim();
}
function stripProposedFence(body) { return stripFence(body, "proposed"); }

// apprAionRegistry / apprReRegistry lazily fetch the DOMAIN registries the
// payload editor suggests from — an aion card must never offer a real-estate
// rock and vice versa. aion: active `## Aion` rocks + aion people.md. real
// estate: active `## Real Estate` rocks + the RE people registry + every
// property and deal (the rock slot tethers those too). Cached per page load.
let apprAionReg = null;
async function apprAionRegistry() {
  if (apprAionReg) return apprAionReg;
  try {
    const d = await (await fetch("/api/aion")).json();
    apprAionReg = {
      rocks: flattenRockLadder(((d.goalsArea || {}).rocks) || []),
      people: d.people || [],
    };
  } catch (e) { apprAionReg = { rocks: [], people: [] }; }
  return apprAionReg;
}

let apprReReg = null;
async function apprReRegistry() {
  if (apprReReg) return apprReReg;
  try {
    const [re, props, people, ents] = await Promise.all([
      (await fetch("/api/re/backlog")).json(),
      (await fetch("/api/properties")).json(),
      (await fetch("/api/properties/people")).json(),
      (await fetch("/api/realestate/entities")).json(),
    ]);
    // owners = the full RE roster: people.md partners (by initials) PLUS
    // contractor records (by slug) — contractors own rocks and tasks too
    const roster = (people.people || []).map((p) => ({ initials: p.initials, name: p.name || "" }));
    (ents.contractors || []).forEach((c) =>
      roster.push({ initials: c.slug, name: c.name + (c.trade ? " (" + c.trade + ")" : "") }));
    apprReReg = {
      rocks: flattenRockLadder(((re.goalsArea || {}).rocks) || []),
      properties: (props.properties || []).filter((p) => !p.hidden),
      deals: props.deals || [],
      people: roster,
      contractors: ents.contractors || [], // the contractor slot matches records, not the owner roster
    };
  } catch (e) { apprReReg = { rocks: [], properties: [], deals: [], people: [], contractors: [] }; }
  return apprReReg;
}

// apprRegistryFor: the domain split — the real-estate cards (a backlog
// candidate, an intake contract) get the RE registry; everything else aion.
function apprRegistryFor(type) {
  return type === "re-backlog" || type === "re-contract" ? apprReRegistry() : apprAionRegistry();
}

// buildAionEditor renders the editable aion payload form: kind flip
// (task⇄decision⇄heuristic incl. new⇄reinforce), title/owner/rock/due/
// outcome, the record-line preview, and a save that rewrites the fence in
// place (the id never changes; Confirm applies whatever was last saved).
// Rock and owner hotload suggestions: active rocks from the goals ladder,
// initials from people.md — free text still commits for one-offs.
// apprCards — the live editor box per approval, so an inspector edit can
// repaint the card it belongs to without re-rendering the whole feed (which
// would blow away a focused field).
const apprCards = new Map(); // id → {a, box, evidence}
function apprRepaintCard(id) {
  const c = apprCards.get(id);
  if (!c || !c.box || !c.box.isConnected) return;
  const next = buildReContractEditor(c.a, c.evidence);
  c.box.replaceWith(next);
  if (c.a.__gateFn) c.a.__gateBind(c.a.__gateFn);
  apprPaintSel();
}

// apprValidate — the client mirror of ReContractPayload.Validate()
// (approvals/recontract.go). It is a WHOLE-document invariant, not a field
// check: the owner passes through invalid states on the way to a valid one
// (clearing a task's text to retype it, re-splitting two allocations). So the
// autosave holds rather than POSTing a payload the server would refuse.
// Returns null when clean, else {msg, sel} pointing at the offending record.
function apprValidate(a, p) {
  const at = (kind, i) => ({ id: a.id, kind, i });
  if (!["bid", "contract", "estimate"].includes(p.kind)) return { msg: "kind must be bid, contract or estimate", sel: at("contract") };
  if (!String(p.contractor || "").trim() && !String(p.contractor_create || "").trim()) return { msg: "needs a contractor", sel: at("contractor") };
  if (!String(p.name || "").trim()) return { msg: "the contract needs a name", sel: at("contract") };
  if (!(p.total > 0)) return { msg: "total must be positive", sel: at("contract") };
  const allocs = p.allocations || [];
  if (!allocs.length) return { msg: "needs at least one allocation", sel: at("contract") };
  for (let i = 0; i < allocs.length; i++) {
    const al = allocs[i];
    if (!String(al.property || "").trim() || !String(al.node || "").trim()) return { msg: "every split row needs a property and a node", sel: at("alloc", i) };
    if (!(al.amount > 0)) return { msg: "split amounts must be positive", sel: at("alloc", i) };
  }
  const sum = allocs.reduce((n, al) => n + (al.amount || 0), 0);
  if (Math.abs(sum - p.total) > 0.01) return { msg: "Σ " + fmtMoney(sum) + " ≠ total " + fmtMoney(p.total || 0), sel: at("contract") };
  const tasks = p.tasks || [];
  for (let i = 0; i < tasks.length; i++) {
    if (!String(tasks[i].text || "").trim() || !String(tasks[i].property || "").trim()) return { msg: "every task needs text and a property", sel: at("task", i) };
  }
  const ms = p.new_milestones || [];
  for (let i = 0; i < ms.length; i++) {
    if (!String(ms[i].name || "").trim()) return { msg: "a milestone needs a name", sel: at("milestone", i) };
  }
  return null;
}

// apprSave — edits save as you go, gated on validity. Debounced so a typed
// field is one write, and never POSTs a payload the server would refuse.
const apprSaveTimers = new Map();
function apprScheduleSave(a) {
  const d = apprDraft(a);
  d.dirty = true;
  clearTimeout(apprSaveTimers.get(a.id));
  apprSaveTimers.set(a.id, setTimeout(() => apprFlush(a), 800));
  renderApprovalInspector();
}
async function apprFlush(a) {
  clearTimeout(apprSaveTimers.get(a.id));
  const d = apprDraft(a);
  if (!d.dirty) return true;
  const bad = apprValidate(a, d.p);
  if (bad) { d.err = ""; renderApprovalInspector(); return false; }
  d.saving = true; d.err = ""; renderApprovalInspector();
  try {
    const r = await fetch("/api/spirits/approvals/" + encodeURIComponent(a.id) + "/recontract", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(d.p),
    });
    if (!r.ok) throw new Error((await r.text()).trim());
    d.dirty = false; d.base = JSON.stringify(d.p);
  } catch (e) {
    d.err = String(e.message || e).slice(0, 120);
  }
  d.saving = false;
  renderApprovalInspector();
  return !d.err && !d.dirty;
}

// apprWorkSlug — recontract.go's node-id slug, mirrored so a converted record
// keeps the id its split rows already point at.
function apprWorkSlug(text) {
  return String(text || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

// apprMilestoneToTask — demote a proposed milestone to a plain task under the
// SAME rock. The written node id is unchanged (<rock>/<slug>), so allocations
// keep tethering to real work; only the shape changes, and with it whether the
// line ever appears as work owed in the backlog.
function apprMilestoneToTask(p, i) {
  const m = (p.new_milestones || [])[i];
  if (!m) return;
  p.new_milestones.splice(i, 1);
  p.tasks = p.tasks || [];
  p.tasks.push({ property: m.property, parent: m.rock, text: m.name });
  apprSel = { id: apprSel && apprSel.id, kind: "task", i: p.tasks.length - 1 };
}

// apprTaskToMilestone — the inverse, offered only for a task sitting directly
// under a rock (milestones are depth-1). Same id, same money.
function apprTaskToMilestone(p, i) {
  const t = (p.tasks || [])[i];
  if (!t || String(t.parent || "").includes("/")) return;
  p.tasks.splice(i, 1);
  p.new_milestones = p.new_milestones || [];
  p.new_milestones.push({ property: t.property, rock: t.parent, name: t.text });
  apprSel = { id: apprSel && apprSel.id, kind: "milestone", i: p.new_milestones.length - 1 };
}

// apprMoney parses a money field the way every RE amount input does.
function apprMoney(v) { return parseFloat(String(v).replace(/[$,\s]/g, "")) || 0; }
// apprDeslug is the immediate label for a slug the registry has not answered
// for yet (it resolves async; the node upgrades in place).
function apprDeslug(v) { return String(v || "").replace(/^property\//, "").replace(/-/g, " "); }

// buildReContractEditor — the intake card (overhaul §5, redesigned per
// FEED-CONTRACT-CARD 1b): money is the headline AND the input; the split
// reconciles right under it against a live Σ that gates Confirm; then the
// consequence — the records Confirm actually writes — and only then the
// terms and the extract, collapsed. Edits flush through the recontract
// endpoint and RIDE the confirm (the __payloadFlush contract).
function buildReContractEditor(a, evidence) {
  const draft = apprDraft(a);
  const p = draft.p; // the draft survives the feed's repaints — see apprDraft
  const box = el("div", "re-intake-editor");
  const allocs = p.allocations || [];

  // slug → label. Every slug renders de-slugged immediately and upgrades in
  // place when the RE registry answers (properties carry the real address,
  // rocks their text, contractors their name) — the card never blocks on it.
  const names = { prop: apprDeslug, rock: apprDeslug, person: apprDeslug };
  const tied = []; // {node, render} — re-run when the registry lands
  const tie = (node, render) => { node.textContent = render(); tied.push({ node, render }); return node; };
  apprRegistryFor(a.type).then((reg) => {
    names.prop = (slug) => {
      const hit = (reg.properties || []).find((x) => x.slug === slug);
      return hit ? rePropLabel(hit) : apprDeslug(slug);
    };
    names.rock = (id) => {
      const hit = (reg.rocks || []).find((r) => r.id === id);
      return hit ? hit.label : apprDeslug(id);
    };
    names.person = (slug) => {
      const hit = (reg.people || []).find((x) => x.initials === slug);
      return hit ? hit.name.replace(/\s*\(.*\)$/, "") : apprDeslug(slug);
    };
    tied.forEach((t) => { t.node.textContent = t.render(); });
  }).catch(() => {});

  // ---- 1. the total: the largest thing on the card, and the field ----
  const totalSec = el("div", "re-intake-total");
  const totalCol = el("div", "re-intake-total-col");
  totalCol.append(el("span", "micro-label", "total"));
  const totalIn = inputEl("total");
  totalIn.className = "re-intake-total-in";
  totalIn.value = fmtMoney(p.total);
  totalIn.oninput = () => { p.total = apprMoney(totalIn.value); dirty(); };
  totalIn.onblur = () => { totalIn.value = fmtMoney(p.total); };
  totalCol.append(totalIn);
  const ident = el("div", "re-intake-ident");
  ident.append(el("div", "re-intake-name", p.name || a.action));
  const metaBits = [];
  if (p.contractor) metaBits.push(tie(el("span"), () => names.person(p.contractor)));
  else if (p.contractor_create) metaBits.push(el("span", "", p.contractor_create));
  metaBits.push(tie(el("span"), propsPhrase));
  if (p.kind) metaBits.push(el("span", "", p.kind));
  if (p.date) metaBits.push(el("span", "", p.date));
  if (p.expires) metaBits.push(el("span", "", "expires " + p.expires));
  const meta = el("div", "re-intake-meta");
  metaBits.forEach((b, i) => { if (i) meta.append(el("span", "re-intake-dot", "·")); meta.append(b); });
  ident.append(meta);
  totalSec.append(totalCol, ident);
  box.append(totalSec);

  // ---- 2. the split: the field the owner always adjusts, with a live Σ ----
  const splitSec = el("div", "re-intake-sec");
  const splitHead = el("div", "re-intake-sec-head");
  splitHead.append(el("span", "micro-label re-intake-sec-label", "split"));
  const sumNote = el("span", "re-intake-sum");
  splitHead.append(sumNote);
  splitSec.append(splitHead);
  const bars = [];
  const reasons = [...new Set(allocs.map((al) => al.reason || ""))];
  const sharedReason = allocs.length > 1 && reasons.length === 1 && reasons[0] ? reasons[0] : "";
  allocs.forEach((al, ai) => {
    const row = el("div", "re-intake-alloc");
    const who = el("div", "re-intake-alloc-who");
    who.append(tie(el("div", "re-intake-alloc-prop"), () => names.prop(al.property)));
    who.append(tie(el("div", "re-intake-alloc-node"), () => nodeLabel(al)));
    row.append(who);
    if (allocs.length > 1) {
      // proportion of the total, so a multi-property split reads at a glance
      const track = el("span", "re-intake-bar");
      const fill = el("i", "");
      track.append(fill);
      row.append(track);
      bars.push({ al, fill });
    }
    const amt = inputEl("amount");
    amt.className = "re-intake-amt-in";
    amt.value = fmtMoney(al.amount);
    amt.oninput = () => { al.amount = apprMoney(amt.value); dirty(); };
    amt.onblur = () => { amt.value = fmtMoney(al.amount); };
    row.append(amt);
    row.dataset.selKey = apprSelKey({ id: a.id, kind: "alloc", i: ai });
    if (apprSel && apprSelKey(apprSel) === row.dataset.selKey) row.classList.add("sel");
    row.onclick = (e) => { if (e.target !== amt) apprSelect({ id: a.id, kind: "alloc", i: ai }); };
    amt.onfocus = () => { if (!apprSel || apprSelKey(apprSel) !== row.dataset.selKey) apprSelect({ id: a.id, kind: "alloc", i: ai }); };
    splitSec.append(row);
    // one shared reasoning (an evenly-split combined scope) is one line, not
    // the same paragraph under every property
    if (al.reason && !sharedReason) splitSec.append(el("div", "re-intake-reason", "· " + al.reason));
  });
  if (sharedReason) splitSec.append(el("div", "re-intake-reason", "· " + sharedReason));
  box.append(splitSec);

  // ---- 3. the consequence: what Confirm writes, in one list ----
  // Derived from the apply lane itself (approvals/recontract.go): the
  // contractor record when it is new, the new milestones, the extracted
  // tasks/decisions, then the contract record. The ledger line is the money
  // effect of those allocations — it re-reads as the split is edited.
  const writesSec = el("div", "re-intake-sec");
  writesSec.append(el("div", "micro-label re-intake-sec-label", "confirm writes"));
  const rows = [];
  const writeRow = (glyph, tag, render, sel) => {
    const row = el("div", "re-intake-write");
    row.append(el("span", "re-intake-write-glyph g-" + tag, glyph));
    row.append(tie(el("span", "re-intake-write-text"), render));
    row.append(el("span", "micro-label re-intake-tag t-" + tag, tag.replace(/-/g, " · ")));
    if (sel) {
      row.dataset.selKey = apprSelKey(sel);
      if (apprSel && apprSelKey(apprSel) === row.dataset.selKey) row.classList.add("sel");
      row.onclick = () => apprSelect(sel);
    }
    writesSec.append(row);
    rows.push(row);
  };
  if (!p.contractor && p.contractor_create) {
    writeRow("＋", "new", () => p.contractor_create + " — contractor record", { id: a.id, kind: "contractor" });
  }
  (p.new_milestones || []).forEach((m, mi) => {
    writeRow("＋", "new", () => m.name + " — milestone under " + names.rock(m.rock) + ", " + names.prop(m.property),
      { id: a.id, kind: "milestone", i: mi });
  });
  (p.tasks || []).forEach((t, ti) => {
    writeRow(t.decision ? "◇" : "○", t.decision ? "decision" : "task-" + (t.owner ? t.owner : "you"),
      () => t.text + " → " + names.prop(t.property), { id: a.id, kind: "task", i: ti });
  });
  writeRow("＋", p.kind || "contract", () => (p.name || "this contract") + " — " + (p.kind || "contract") + " record",
    { id: a.id, kind: "contract" });
  const recordCount = () => rows.length;
  const ledgerText = tie(el("span", "re-intake-write-text"),
    () => fmtMoney(sumOf()) + (p.kind === "contract" ? " committed against " : " proposed against ") + propsPhrase());
  const ledgerRow = el("div", "re-intake-write");
  ledgerRow.append(el("span", "re-intake-write-glyph g-ledger", "$"), ledgerText,
    el("span", "micro-label re-intake-tag t-ledger", "ledger"));
  writesSec.append(ledgerRow);
  box.append(writesSec);

  // ---- 4. evidence, one click away: terms, risk, the extract, the path ----
  const detail = el("div", "re-intake-detail");
  const shutLabel = "terms, risk and the source ▾", openLabel = "hide terms, risk and the source ▴";
  const toggle = el("button", "re-intake-toggle", shutLabel);
  const body = el("div", "re-intake-detail-body");
  body.hidden = true;
  toggle.onclick = () => { body.hidden = !body.hidden; toggle.textContent = body.hidden ? shutLabel : openLabel; };
  detail.append(toggle, body);
  const line = (label, node) => {
    const row = el("div", "re-intake-row");
    row.append(el("span", "micro-label re-intake-row-label", label), node);
    body.append(row);
  };
  ["terms", "exclusions", "risk_items"].forEach((k) => {
    if ((p[k] || []).length) line(k.replace("_", " "), el("span", "re-intake-row-val", p[k].join(" · ")));
  });
  if (evidence && evidence.trim()) {
    const pre = el("pre", "appr-body re-intake-source");
    pre.textContent = evidence.trim();
    body.append(pre);
  }
  const chip = el("div", "appr-apply");
  chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
  body.append(chip);
  box.append(detail);

  // ---- the Σ gate: checkSum()'s rule, visible before the click ----
  function sumOf() { return allocs.reduce((n, al) => n + (al.amount || 0), 0); }
  // an allocation's node id is <rock>/<slug(milestone)>; when that milestone
  // is one this proposal creates, show the name it will be written under
  function nodeLabel(al) {
    const parts = String(al.node || "").split("/");
    const tail = parts.slice(1).join("/");
    const made = (p.new_milestones || []).find((m) =>
      m.property === al.property && tail === String(m.name).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""));
    return [names.rock(parts[0]), made ? made.name : apprDeslug(tail)].filter(Boolean).join(" / ");
  }
  function propsPhrase() {
    const labels = [...new Set(allocs.map((al) => names.prop(al.property)))];
    return labels.length > 2 ? labels.length + " properties" : labels.join(" + ");
  }
  const gate = { push: () => {} };
  const checkSum = () => {
    const sum = sumOf(), total = p.total || 0;
    const off = Math.abs(sum - total) > 0.01;
    sumNote.textContent = off
      ? "Σ " + fmtMoney(sum) + " ≠ total " + fmtMoney(total) + " — fix before confirm"
      : allocs.length > 1 ? "reconciles · " + allocs.length + " properties, full amount"
        : "reconciles · single property, full amount";
    sumNote.classList.toggle("off", off);
    bars.forEach((b) => { b.fill.style.width = (total > 0 ? Math.max(0, Math.min(100, (b.al.amount / total) * 100)) : 0) + "%"; });
    tied.forEach((t) => { t.node.textContent = t.render(); });
    gate.push(!off, off ? "● the split must reconcile" : "writes " + recordCount() + " record" + (recordCount() === 1 ? "" : "s"));
    return !off;
  };
  a.__gateBind = (fn) => { gate.push = fn; a.__gateFn = fn; checkSum(); };
  checkSum();

  const dirty = () => { checkSum(); apprScheduleSave(a); };
  // Confirm still rides the current state: flush now, and refuse if the
  // payload is not one the server would take.
  a.__payloadFlush = async () => {
    const bad = apprValidate(a, p);
    if (bad) throw new Error(bad.msg);
    if (!(await apprFlush(a))) throw new Error(apprDraft(a).err || "the edit did not save");
  };
  apprCards.set(a.id, { a, box, evidence });
  return box;
}

function buildAionEditor(a) {
  const p = Object.assign({}, a.aionPayload);
  p.heuristic = Object.assign({ mode: "", target: "" }, p.heuristic || {});
  const wrap = el("div", "aion-appr");
  wrap.append(el("div", "appr-diff-label", "Appends to " + a.applyPath + " — edit before confirming"));
  const form = el("div", "aion-appr-form");
  wrap.append(form);
  const preview = el("pre", "aion-appr-line");
  const dirtyNote = el("div", "appr-title-label", "");

  const row = (label, node) => {
    form.append(el("span", "aion-vto-key", label), node);
  };
  const textRow = (label, key, obj) => {
    const input = inputEl(label);
    input.value = (obj || p)[key] || "";
    input.oninput = () => { (obj || p)[key] = input.value; sync(); };
    row(label, input);
    return input;
  };
  const rebuild = () => {
    form.innerHTML = "";
    // real estate has no heuristics file — the kind flip is task⇄decision only
    const kinds = a.type === "re-backlog" ? ["task", "decision"] : ["task", "decision", "heuristic"];
    const kindSel = selectEl(kinds);
    kindSel.value = p.kind;
    kindSel.onchange = () => { p.kind = kindSel.value; if (p.kind === "heuristic" && !p.heuristic.mode) p.heuristic.mode = "new"; rebuild(); };
    row("kind", kindSel);
    textRow("title", "title");
    if (p.kind === "heuristic") {
      const modeSel = selectEl(["new", "reinforce"]);
      modeSel.value = p.heuristic.mode || "new";
      modeSel.onchange = () => { p.heuristic.mode = modeSel.value; rebuild(); };
      row("mode", modeSel);
      if (p.heuristic.mode === "reinforce") textRow("target statement", "target", p.heuristic);
    } else {
      const isRe = a.type === "re-backlog";
      // owner: typeahead over THIS domain's registry (aion people.md, or the
      // curated RE registry — the aion roster never reaches an RE card)
      let ownerPicked = null;
      const ownerTa = typeahead({
        placeholder: "initials…", initial: p.owner || "",
        suggest: async (q, add, ta) => {
          const reg = await apprRegistryFor(a.type);
          reg.people
            .filter((pp) => !q || pp.initials.toLowerCase().includes(q) || (pp.name || "").toLowerCase().includes(q))
            .slice(0, 8)
            .forEach((pp) => add(pp.initials + " · " + (pp.name || ""), "", () => {
              p.owner = pp.initials; ownerPicked = pp.initials; ta.commit(pp.initials); sync();
            }));
        },
        onChange: (v) => { if (v !== ownerPicked) p.owner = v; sync(); },
      });
      row("owner", ownerTa.el);
      // rock: BOTH kinds tether — a decision filed without one falls out of
      // every rock-scoped surface. Typeahead over THIS domain's ACTIVE rocks —
      // picking stores the rock ID (displays its title); free text commits
      // verbatim. RE cards also search PROPERTIES and DEALS live as you type —
      // the rock slot tethers those too (renderer nests by slug on the
      // Rocks/property views), so nothing has to be typed from memory.
      const reg0 = isRe ? apprReReg : apprAionReg; // may already be cached for initial label
      let rockPickedText = null;
      const initialRock = (() => {
        if (reg0) {
          const hit = reg0.rocks.find((r) => r.id === p.rock);
          if (hit) { rockPickedText = hit.label; return hit.label; }
          if (isRe) {
            const pr = reg0.properties.find((x) => x.slug === p.rock);
            if (pr) { rockPickedText = pr.short || pr.address || pr.slug; return rockPickedText; }
            const dl = reg0.deals.find((x) => x.slug === p.rock);
            if (dl) { rockPickedText = dl.name || dl.slug; return rockPickedText; }
          }
        }
        return p.rock || "";
      })();
      const rockTa = typeahead({
        placeholder: isRe ? "rock, property, or deal…" : "type to pick an active rock…",
        initial: initialRock,
        suggest: async (q, add, ta) => {
          const reg = await apprRegistryFor(a.type);
          const pick = (id, label) => () => {
            p.rock = id; rockPickedText = label; ta.commit(label); sync();
          };
          reg.rocks
            .filter((r) => !r.checked)
            .filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
            .slice(0, 8)
            .forEach((r) => add(r.label, isRe ? "rock" : "", pick(r.id, r.label)));
          if (isRe) {
            (reg.deals || [])
              .filter((d) => q && ((d.name || "").toLowerCase().includes(q) || d.slug.includes(q)))
              .slice(0, 4)
              .forEach((d) => add(d.name || d.slug, "deal", pick(d.slug, d.name || d.slug)));
            (reg.properties || [])
              .filter((pr) => q && ((pr.address || "").toLowerCase().includes(q) || pr.slug.includes(q)))
              .slice(0, 8)
              .forEach((pr) => add(pr.short || pr.address || pr.slug, "property", pick(pr.slug, pr.short || pr.address || pr.slug)));
          }
          if (p.rock) add("✕ no rock (unanchored)", "create", () => {
            p.rock = ""; rockPickedText = ""; ta.commit(""); sync();
          });
        },
        onChange: (v) => { if (v !== rockPickedText) p.rock = v; sync(); },
      });
      row("rock", rockTa.el);
      if (p.kind === "task") {
        textRow("due", "due");
      } else {
        textRow("needed by", "needed_by");
        textRow("outcome", "outcome");
      }
    }
    sync();
  };
  const sync = () => {
    // client-side line preview mirror (the server's RenderItemLine is
    // authoritative — this is orientation, not the contract)
    let line;
    if (p.kind === "heuristic") {
      line = p.heuristic.mode === "reinforce"
        ? "reinforces: " + (p.heuristic.target || "…")
        : "- " + (p.title || "…") + " [first:: " + (p.captured || "") + "]";
    } else {
      const f = [];
      f.push("[kind:: " + p.kind + "]");
      if (p.status) f.push("[status:: " + p.status + "]");
      if (p.kind === "task") {
        if (p.due) f.push("[due:: " + p.due + "]");
      } else {
        if (p.needed_by) f.push("[needed_by:: " + p.needed_by + "]");
        if (p.outcome) f.push("[outcome:: " + p.outcome + "]");
      }
      if (p.rock) f.push("[rock:: " + p.rock + "]");
      if (p.owner) f.push("[owner:: " + p.owner + "]");
      (p.sources || []).forEach((s) => f.push("[source:: [[" + s + "]]]"));
      if (p.captured) f.push("[captured:: " + p.captured + "]");
      line = (p.kind === "task" ? "- [ ] " : "- ") + (p.title || "…") + " " + f.join(" ");
    }
    preview.textContent = line;
    dirtyNote.textContent = "edits ride Confirm automatically — save edit persists them without confirming";
  };
  rebuild();
  wrap.append(preview, dirtyNote);
  const flush = async () => {
    const r = await fetch("/api/spirits/approvals/" + encodeURIComponent(a.id) + "/aion", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p),
    });
    if (!r.ok) throw new Error(await r.text());
  };
  // Confirm flushes the CURRENT form state first — see the confirm handler
  a.__payloadFlush = flush;
  const save = pillLight("save edit", async () => {
    try {
      await flush();
      showToast("Proposal updated");
      loadFeed();
    } catch (e) { showToast(String(e.message || e).slice(0, 120)); }
  });
  wrap.append(save);
  return wrap;
}

// buildResolveEditor renders the closed-loop resolve form: the matching
// backlog title (must equal the line verbatim — the apply matches by it),
// and the resolution — done_on for a task, decided date + outcome for a
// decision. Kind and the resolving verb are fixed; the fence rewrites via
// the same /aion endpoint (SetAionPayload's resolve lane).
function buildResolveEditor(a) {
  const p = Object.assign({}, a.aionPayload);
  const wrap = el("div", "aion-appr");
  const verb = p.kind === "decision" ? "decided" : "done";
  wrap.append(el("div", "appr-diff-label",
    "Resolves one " + p.kind + " in " + a.applyPath + " → " + verb + " — edit before confirming"));
  const form = el("div", "aion-appr-form");
  const preview = el("pre", "aion-appr-line");
  const row = (label, key) => {
    const input = inputEl(label);
    input.value = p[key] || "";
    input.oninput = () => { p[key] = input.value; sync(); };
    form.append(el("span", "aion-vto-key", label), input);
  };
  row("title (must match the backlog line)", "title");
  if (p.kind === "decision") {
    row("outcome", "outcome");
    row("decided (YYYY-MM-DD)", "decided");
  } else {
    row("done on (YYYY-MM-DD)", "done_on");
  }
  const sync = () => {
    preview.textContent = p.kind === "decision"
      ? "- " + (p.title || "…") + " → decided" + (p.decided ? " " + p.decided : "") + ": " + (p.outcome || "…")
      : "- [x] " + (p.title || "…") + " → done" + (p.done_on ? " " + p.done_on : "");
  };
  sync();
  wrap.append(form, preview);
  const flush = async () => {
    const r = await fetch("/api/spirits/approvals/" + encodeURIComponent(a.id) + "/aion", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p),
    });
    if (!r.ok) throw new Error(await r.text());
  };
  a.__payloadFlush = flush; // Confirm flushes the current form state first
  const save = pillLight("save edit", async () => {
    try { await flush(); showToast("Proposal updated"); loadFeed(); }
    catch (e) { showToast(String(e.message || e).slice(0, 120)); }
  });
  wrap.append(save);
  return wrap;
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
function spiritApprovalAct(id, kind, edits) {
  if (kind === "reject") {
    // inline reason box (no browser prompt); Escape cancels
    askText("Reject — reason (optional)",
      "recorded on the proposal; for warden findings this becomes an accepted exception",
      (reason) => postApprovalDecision(id, "reject", { reason: reason.trim() || "rejected from dashboard" }));
    return;
  }
  let body = {};
  if (kind === "confirm" && edits) {
    // create-vault-note: the edited people list, filename title, categories
    if (edits.attendees !== null && edits.attendees !== undefined) {
      body.editAttendees = true;
      body.attendees = edits.attendees;
    }
    if (edits.title !== null && edits.title !== undefined && String(edits.title).trim() !== "") {
      body.title = String(edits.title).trim();
    }
    if (edits.categories !== null && edits.categories !== undefined) {
      body.editCategories = true;
      body.categories = edits.categories;
    }
  }
  postApprovalDecision(id, kind, body);
}
async function postApprovalDecision(id, kind, body) {
  // optimistic: the card leaves the moment you decide — the pending/ move
  // follows; loadFeed() converges to truth either way (a refused decision
  // brings the card back).
  const card = document.querySelector(`[data-approval-id="${CSS.escape(id)}"]`);
  if (card) card.remove();
  setSaveState("saving");
  try {
    const r = await fetch(`/api/spirits/approvals/${encodeURIComponent(id)}/${kind}`,
      { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    // fetch only rejects on a NETWORK failure, so a refused apply (400 with
    // the reason in the body) used to read as "saved": the card vanished
    // optimistically, loadFeed brought it back, and the reason was thrown
    // away. The owner saw a card blink and nothing land.
    if (!r.ok) {
      const why = (await r.text().catch(() => "")).trim();
      setSaveState("error");
      showToast("Not applied — " + (why.replace(/^apply refused:\s*/i, "") || ("HTTP " + r.status)).slice(0, 160), null, "error");
    } else {
      setSaveState("saved");
    }
  } catch (e) {
    setSaveState("error");
    showToast("Couldn't reach the server — " + String(e.message || e).slice(0, 100), null, "error");
  }
  loadFeed(); // approvals live in FEED — the decided card resolves in place
}

if (els.feedRunNowBtn) els.feedRunNowBtn.addEventListener("click", spiritRunNow);
if (els.feedAskBtn) els.feedAskBtn.addEventListener("click", spiritAskScout);

// ---- the FEED inspector: one proposed record, editable ------------------
// The card says what Confirm will write; this is where a line is corrected
// before it is written. Edits land in the draft and save as you go (see
// apprScheduleSave), so Confirm always applies what is on screen.
function renderApprovalInspector() {
  // phone: the rail is display:none — the same builder fills a bottom sheet.
  // Keyed open re-fills in place, so the 3s feed poll never re-animates it.
  if (window.mf && window.mf.phone() && window.mfSheet) {
    if (apprSel) {
      window.mfSheet.open((b) => apprInspectorInto(b), {
        key: "appr",
        onClose: () => { if (apprSel) { apprSel = null; apprPaintSel(); } },
        reopen: () => { if (els.feedView && !els.feedView.hidden) renderApprovalInspector(); },
      });
    } else {
      window.mfSheet.closeIf("appr");
    }
    return;
  }
  apprInspectorInto(els.feedInspector);
}

function apprInspectorInto(host) {
  if (!host) return;
  host.innerHTML = "";
  const card = apprSel ? apprCards.get(apprSel.id) : null;
  if (!apprSel || !card || !card.box.isConnected) {
    host.append(el("div", "aion-insp-empty", "select a line in a proposal — edits save as you go"));
    return;
  }
  const a = card.a, d = apprDraft(a), p = d.p, sel = apprSel;

  const head = el("div", "aion-insp-head");
  head.append(el("span", "aion-insp-label", sel.kind === "alloc" ? "Split" : sel.kind));
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = () => { apprSel = null; apprPaintSel(); renderApprovalInspector(); };
  head.append(x);
  host.append(head);

  const commit = () => { apprRepaintCard(a.id); apprScheduleSave(a); };
  const field = (label, node) => {
    const f = el("div", "aion-insp-field");
    f.append(el("span", "aion-insp-flabel", label), node);
    host.append(f);
    return node;
  };
  // a text field that writes into the payload on its own commit event
  const textField = (label, get, set, placeholder) => {
    const input = inputEl(placeholder || label);
    input.className = "pp-in";
    input.value = get() || "";
    const save = () => { if ((get() || "") !== input.value) { set(input.value); commit(); } };
    input.addEventListener("keydown", (ev) => { if (ev.key === "Enter") input.blur(); });
    input.addEventListener("blur", save);
    return field(label, input);
  };
  const pickField = (label, get, set, suggest) => {
    const ta = typeahead({
      placeholder: label, initial: get() || "",
      suggest: (q, add, t) => suggest(q, add, t, (v) => { set(v); commit(); }),
      onChange: (v) => { if ((get() || "") !== v) { set(v); commit(); } },
    });
    return field(label, ta.el);
  };
  const propSuggest = async (q, add, ta, pick) => {
    const reg = await apprRegistryFor(a.type);
    (reg.properties || [])
      .filter((x) => !q || (x.slug + " " + (x.short || x.address || "")).toLowerCase().includes(q))
      .slice(0, 8)
      .forEach((x) => add(rePropLabel(x), "property", () => { ta.commit(rePropLabel(x)); pick(x.slug); }));
  };
  const rockSuggest = async (q, add, ta, pick) => {
    const reg = await apprRegistryFor(a.type);
    (reg.rocks || []).filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
      .slice(0, 8).forEach((r) => add(r.label, "rock", () => { ta.commit(r.label); pick(r.id); }));
  };
  const ownerSuggest = async (q, add, ta, pick) => {
    const reg = await apprRegistryFor(a.type);
    add("· you", "", () => { ta.commit(""); pick(""); });
    (reg.people || []).filter((x) => !q || (x.initials + " " + (x.name || "")).toLowerCase().includes(q))
      .slice(0, 8).forEach((x) => add(x.initials + " · " + (x.name || ""), "", () => { ta.commit(x.initials); pick(x.initials); }));
  };
  // the node id a milestone will be written under (recontract.go derives it the
  // same way) — keeping allocations pointed at a renamed milestone depends on it
  const milestoneNode = (m) => String(m.rock || "") + "/" + apprWorkSlug(m.name);

  let removable = null;
  if (sel.kind === "task") {
    const t = (p.tasks || [])[sel.i];
    if (!t) { apprSel = null; return apprInspectorInto(host); }
    textField("text", () => t.text, (v) => { t.text = v; });
    pickField("property", () => t.property, (v) => { t.property = v; }, propSuggest);
    pickField("under", () => t.parent, (v) => { t.parent = v; }, rockSuggest);
    pickField("owner", () => t.owner, (v) => { t.owner = v; }, ownerSuggest);
    // task ⇄ decision ⇄ milestone. A milestone is a CONTAINER of work and is
    // deliberately absent from the task backlog, so which one a proposed line
    // becomes decides whether it ever shows up as work owed (owner call
    // 2026-08-19 — Olga's permit drawings landed as milestones and vanished).
    // Promoting is only offered under a ROCK: milestones are depth-1 only.
    const underRock = !String(t.parent || "").includes("/");
    const kind = selectEl(underRock ? ["task", "decision", "milestone"] : ["task", "decision"]);
    kind.className = "pp-in";
    kind.value = t.decision ? "decision" : "task";
    kind.onchange = () => {
      if (kind.value === "milestone") { apprTaskToMilestone(p, sel.i); commit(); apprPaintSel(); renderApprovalInspector(); return; }
      t.decision = kind.value === "decision";
      commit();
    };
    field("kind", kind);
    if (!underRock) {
      host.append(el("div", "aion-insp-ro",
        "nested under a milestone — move it to a rock to make it one"));
    }
    removable = () => { p.tasks.splice(sel.i, 1); apprSel = null; };
  } else if (sel.kind === "milestone") {
    const m = (p.new_milestones || [])[sel.i];
    if (!m) { apprSel = null; return apprInspectorInto(host); }
    // renaming a milestone moves the node id its allocations point at; re-point
    // them or the money lands on a node that will not exist (recontract.go
    // writes alloc.node verbatim as the contract's NodeID)
    const repoint = (before) => {
      const after = milestoneNode(m);
      if (after === before) return;
      (p.allocations || []).forEach((al) => { if (al.property === m.property && al.node === before) al.node = after; });
    };
    textField("name", () => m.name, (v) => { const b = milestoneNode(m); m.name = v; repoint(b); });
    pickField("rock", () => m.rock, (v) => { const b = milestoneNode(m); m.rock = v; repoint(b); }, rockSuggest);
    pickField("property", () => m.property, (v) => { m.property = v; }, propSuggest);
    // THE fix the owner asked for: a proposed milestone is a container and
    // never reaches the task backlog, so a scope that is really one piece of
    // work has to be demotable BEFORE confirming. The node id is identical
    // either way — a milestone is <rock>/<slug(name)> and a task under the
    // same rock is <rock>/<slug(text)> — so the split rows keep pointing at
    // real work and the money never moves.
    const mkind = selectEl(["milestone", "task"]);
    mkind.className = "pp-in";
    mkind.value = "milestone";
    mkind.onchange = () => {
      if (mkind.value !== "task") return;
      apprMilestoneToTask(p, sel.i);
      commit();
      apprPaintSel();
      renderApprovalInspector();
    };
    field("kind", mkind);
    host.append(el("div", "aion-insp-ro",
      "a milestone groups work; only tasks reach the backlog as work owed"));
    const pointing = (p.allocations || []).filter((al) => al.property === m.property && al.node === milestoneNode(m)).length;
    removable = () => {
      if (pointing) { showToast(pointing + " split row" + (pointing === 1 ? "" : "s") + " point here — re-point them first"); return false; }
      p.new_milestones.splice(sel.i, 1); apprSel = null;
    };
  } else if (sel.kind === "alloc") {
    const al = (p.allocations || [])[sel.i];
    if (!al) { apprSel = null; return apprInspectorInto(host); }
    pickField("property", () => al.property, (v) => { al.property = v; }, propSuggest);
    textField("node", () => al.node, (v) => { al.node = v; });
    const amt = inputEl("amount");
    amt.className = "pp-in";
    amt.value = fmtMoney(al.amount);
    amt.addEventListener("blur", () => { const v = apprMoney(amt.value); amt.value = fmtMoney(v); if (v !== al.amount) { al.amount = v; commit(); } });
    amt.addEventListener("keydown", (ev) => { if (ev.key === "Enter") amt.blur(); });
    field("amount", amt);
    textField("reason", () => al.reason, (v) => { al.reason = v; });
    if ((p.allocations || []).length > 1) removable = () => { p.allocations.splice(sel.i, 1); apprSel = null; };
  } else if (sel.kind === "contractor") {
    if (p.contractor_create) {
      textField("name", () => p.contractor_create, (v) => { p.contractor_create = v; });
      pickField("or match", () => "", (v) => { if (v) { p.contractor = v; p.contractor_create = ""; } }, async (q, add, ta, pick) => {
        const reg = await apprRegistryFor(a.type);
        (reg.contractors || []).filter((c) => !q || (c.slug + " " + (c.name || "")).toLowerCase().includes(q))
          .slice(0, 8).forEach((c) => add(c.name || c.slug, "record", () => { ta.commit(c.name || c.slug); pick(c.slug); }));
      });
      host.append(el("div", "aion-insp-ro", "No record matches — Confirm creates one."));
    } else {
      host.append(el("div", "aion-insp-ro", "Matched to the existing record @" + p.contractor + "."));
      const swap = el("button", "o-ghost", "create a new record instead");
      swap.onclick = () => { p.contractor_create = p.contractor; p.contractor = ""; commit(); renderApprovalInspector(); };
      host.append(swap);
    }
  } else { // the contract record itself
    textField("name", () => p.name, (v) => { p.name = v; });
    const kind = selectEl(["bid", "contract", "estimate"]);
    kind.className = "pp-in";
    kind.value = p.kind || "bid";
    kind.onchange = () => { p.kind = kind.value; commit(); };
    field("kind", kind);
    const total = inputEl("total");
    total.className = "pp-in";
    total.value = fmtMoney(p.total);
    total.addEventListener("blur", () => { const v = apprMoney(total.value); total.value = fmtMoney(v); if (v !== p.total) { p.total = v; commit(); } });
    total.addEventListener("keydown", (ev) => { if (ev.key === "Enter") total.blur(); });
    field("total", total);
    ["date", "expires"].forEach((k) => {
      const dt = inputEl(k);
      dt.type = "date"; dt.className = "pp-in"; dt.value = p[k] || "";
      dt.onchange = () => { p[k] = dt.value; commit(); };
      field(k, dt);
    });
    host.append(el("div", "aion-insp-ro", "Lands at " + a.applyPath));
  }

  if (removable) {
    const del = el("button", "aion-insp-del", "don't write this");
    del.onclick = () => {
      if (del.classList.contains("armed")) {
        if (removable() === false) return;
        apprRepaintCard(a.id); apprScheduleSave(a); renderApprovalInspector();
        return;
      }
      del.classList.add("armed");
      del.textContent = "remove from the proposal?";
      setTimeout(() => { del.classList.remove("armed"); del.textContent = "don't write this"; }, 2500);
    };
    host.append(del);
  }

  const foot = el("div", "aion-insp-foot");
  const bad = apprValidate(a, p);
  if (d.err) { foot.textContent = "save failed — " + d.err; foot.classList.add("off"); }
  else if (d.saving) foot.textContent = "saving…";
  else if (bad) {
    foot.textContent = "unsaved — " + bad.msg;
    foot.classList.add("off");
    if (bad.sel) { foot.style.cursor = "pointer"; foot.onclick = () => apprSelect(bad.sel); }
  } else foot.textContent = "edits save as you go";
  host.append(foot);
}

// ---- the goals placement card (§12 2026-08-19) --------------------------
// One card per placement: mode/level/area, the typeahead-audited target, the
// owner-sourced title, and the EXACT current→proposed diff the server computed
// against the live goals.md. Edits ride Confirm (__payloadFlush), same as the
// aion editor this clones.
let apprGoalsReg = null;
async function apprGoalsRegistry() {
  if (apprGoalsReg) return apprGoalsReg;
  try {
    const d = await (await fetch("/api/goals")).json();
    apprGoalsReg = (d.areas || []).map((ar) => ({
      name: ar.name,
      rocks: (ar.rocks || []).filter((r) => !r.checked).map((r) => ({ id: r.id, text: r.text })),
    }));
  } catch (e) { apprGoalsReg = []; }
  return apprGoalsReg;
}

function buildGoalsEditor(a) {
  const p = Object.assign({}, a.goalsPayload);
  const wrap = el("div", "aion-appr");
  wrap.append(el("div", "appr-diff-label", "Places into goals.md — edit before confirming"));
  const form = el("div", "aion-appr-form");
  wrap.append(form);
  const note = el("div", "appr-title-label", "");

  const row = (label, node) => form.append(el("span", "aion-vto-key", label), node);
  const textRow = (label, key) => {
    const input = inputEl(label);
    input.className = "pp-in";
    input.value = p[key] || "";
    input.oninput = () => { p[key] = input.value; };
    row(label, input);
  };

  const rebuild = () => {
    form.innerHTML = "";
    const modeSel = selectEl(["add", "edit", "move"]);
    modeSel.value = p.mode || "add";
    modeSel.onchange = () => { p.mode = modeSel.value; rebuild(); };
    row("mode", modeSel);
    const levelSel = selectEl(["rock", "milestone"]);
    levelSel.value = p.level || "milestone";
    levelSel.onchange = () => { p.level = levelSel.value; rebuild(); };
    row("level", levelSel);
    const areaSel = selectEl([p.area || ""]);
    areaSel.className = "pp-in";
    apprGoalsRegistry().then((reg) => {
      areaSel.innerHTML = "";
      reg.forEach((ar) => { const o = document.createElement("option"); o.value = ar.name; o.textContent = ar.name; areaSel.append(o); });
      areaSel.value = p.area || (reg[0] && reg[0].name) || "";
    });
    areaSel.onchange = () => { p.area = areaSel.value; };
    row("area", areaSel);

    if (p.mode === "add" || p.mode === "move") {
      if (p.level === "milestone") {
        // the rock it lands under — from the live registry, scoped to the area
        const parentTa = typeahead({
          placeholder: "the rock it goes under…", initial: p.parentId || "",
          suggest: async (q, add, ta) => {
            const reg = await apprGoalsRegistry();
            const ar = reg.find((x) => x.name === (p.area || "")) || reg[0];
            ((ar && ar.rocks) || [])
              .filter((r) => !q || r.text.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
              .slice(0, 8)
              .forEach((r) => add(r.text, "rock", () => { p.parentId = r.id; ta.commit(r.text); }));
            if (p.mode === "move") add("✕ promote to a rock", "", () => { p.parentId = ""; ta.commit(""); });
          },
          onChange: (v) => { if (!v) p.parentId = ""; },
        });
        row("under", parentTa.el);
      }
    }
    if (p.mode === "edit" || p.mode === "move") {
      // the goal being changed — /api/goals/match audits live goals only, and
      // a pick fills BOTH targetId and the staleness anchor
      const targetTa = typeahead({
        placeholder: "which existing goal…", initial: p.anchorText || p.targetId || "",
        suggest: async (q, add, ta) => {
          if (!q) return;
          let d = { matches: [] };
          try { d = await (await fetch("/api/goals/match?q=" + encodeURIComponent(q))).json(); } catch (e) {}
          (d.matches || []).forEach((m) => add(m.text + "  · " + m.area + " " + m.level, "", () => {
            p.targetId = m.id; p.anchorText = m.text; p.area = m.area || p.area;
            ta.commit(m.text);
          }));
        },
      });
      row("target", targetTa.el);
    }
    if (p.mode !== "move") textRow("title", "title");
    textRow("owner", "owner");
    if (p.level === "rock") {
      // no quarter field — a new rock stamps the current quarter on confirm
      const due = inputEl("due");
      due.type = "date"; due.className = "pp-in"; due.value = p.due || "";
      due.onchange = () => { p.due = due.value; };
      row("due", due);
    }
    sync();
  };
  const sync = () => {
    note.textContent = a.goalsErr
      ? "✕ would refuse right now: " + a.goalsErr
      : "edits ride Confirm automatically — save edit persists them without confirming";
    note.classList.toggle("off", !!a.goalsErr);
  };
  // the gate: a placement the server would refuse disables Confirm with the
  // reason beside it (the re-contract card's mechanism); save-edit stays live
  // so the owner can fix the payload and let the next feed load re-enable it
  a.__gateBind = (fn) => fn(!a.goalsErr, a.goalsErr ? "● fix the placement first" : "places one line");
  rebuild();
  wrap.append(note);

  const flush = async () => {
    const r = await fetch("/api/spirits/approvals/" + encodeURIComponent(a.id) + "/goals", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p),
    });
    if (!r.ok) throw new Error(await r.text());
  };
  a.__payloadFlush = flush;
  const save = pillLight("save edit", async () => {
    try { await flush(); showToast("Proposal updated"); loadFeed(); }
    catch (e) { showToast(String(e.message || e).slice(0, 120)); }
  });
  wrap.append(save);
  // the exact write Confirm makes, current → proposed (server-computed)
  if (a.proposed && a.current) {
    const diff = renderLineDiff(a.current, a.proposed);
    wrap.append(collapsibleBlock(diff, diff.childElementCount));
  }
  return wrap;
}
