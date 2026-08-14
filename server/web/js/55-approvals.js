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
  if (a.harness) head.append(el("span", "harness-chip", a.harness)); // federation source
  card.append(head);
  if (a.created) card.append(el("div", "feed-meta", fmtWhen(a.created)));

  const actionable = !!a.applyPath;
  const isResolve = a.type === "aion-resolve" || a.type === "re-resolve"; // closed-loop: flip one backlog line
  const isRe = a.type === "re-backlog" || a.type === "re-resolve"; // the real-estate domain twins (````re fence)
  const isAion = a.type === "aion-backlog" || a.type === "aion-heuristic" || isRe || isResolve;
  // For an actionable proposal the ````proposed payload is rendered as a diff
  // below (and an aion payload as its editable form), so strip the fence from
  // the human-facing evidence body.
  const bodyText = isAion ? stripFence(a.body, isRe ? "re" : "aion") : actionable ? stripProposedFence(a.body) : a.body;
  if (bodyText && bodyText.trim()) { const b = el("pre", "appr-body"); b.textContent = bodyText.trim(); card.append(b); }

  let blocked = false, blockMsg = "";
  const isNewNote = a.type === "create-vault-note";
  const isXQueue = a.type === "append-x-queue";
  const isSkill = a.type === "update-vault-skill";
  const isAppendNote = a.type === "append-vault-note"; // email-sync append the auto-apply refused
  let attendees = null; // create-vault-note: the editable people list sent on Confirm
  let categories = null; // create-vault-note: the editable frontmatter categories
  const titleRef = { value: null }; // create-vault-note: the editable filename title
  if (actionable) {
    card.classList.add("actionable");
    // create-vault-note shows its path via the editable title field below, so the
    // static APPLIES-TO chip is redundant (and would show the pre-edit title).
    if (!isNewNote) {
      const chip = el("div", "appr-apply");
      chip.append(el("span", "appr-apply-label", "APPLIES TO"), el("code", "appr-apply-path", a.applyPath));
      card.append(chip);
    }

    if (!a.allowed) {
      blocked = true;
      blockMsg = isNewNote
        ? "apply-path is not a vault-root dated note (YYYY-MM-DD <title>.md) — Confirm is disabled."
        : isXQueue
        ? "apply-path is not the x-posts file — Confirm is disabled."
        : isSkill
        ? "update-vault-skill must target skills/x-content/{SKILL.md, references/<name>.md} and be filed by a tune ritual — Confirm is disabled."
        : isAppendNote
        ? "append target is not a log/ dated note or the proposal carries no thread id — Confirm is disabled."
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

    if (isResolve && a.aionPayload) {
      // a closed-loop resolve: the evidence is the body above; the editable
      // bits are the matching title and the outcome/date — kind is fixed
      card.append(buildResolveEditor(a));
    } else if (isAion && a.aionPayload) {
      // an aion extraction candidate: editable payload + the exact record
      // line Confirm appends (the app renders it — nothing else is written)
      card.append(buildAionEditor(a));
    } else if (isXQueue) {
      // append-x-queue's proposed is ONLY the bullet — show it, not a whole-file diff
      card.append(el("div", "appr-diff-label", "Appends under # queue in " + a.applyPath));
      const pre = el("pre", "appr-body draft-tweet"); pre.textContent = (a.proposed || "").trim(); card.append(pre);
    } else if (isAppendNote) {
      // append-vault-note's proposed is ONLY the new message sections — this
      // card renders only when auto-apply refused (rename/mismatch), so say so
      card.append(el("div", "appr-diff-label", "Appends to " + a.applyPath + " — auto-apply was refused, review before confirming"));
      const pre = el("pre", "appr-body"); pre.textContent = (a.proposed || "").trim();
      card.append(collapsibleBlock(pre, (a.proposed || "").split("\n").length));
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
    };
  } catch (e) { apprReReg = { rocks: [], properties: [], deals: [], people: [] }; }
  return apprReReg;
}

// apprRegistryFor: the domain split — re-backlog cards get the RE registry.
function apprRegistryFor(type) {
  return type === "re-backlog" ? apprReRegistry() : apprAionRegistry();
}

// buildAionEditor renders the editable aion payload form: kind flip
// (task⇄decision⇄heuristic incl. new⇄reinforce), title/owner/rock/due/
// outcome, the record-line preview, and a save that rewrites the fence in
// place (the id never changes; Confirm applies whatever was last saved).
// Rock and owner hotload suggestions: active rocks from the goals ladder,
// initials from people.md — free text still commits for one-offs.
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
      if (p.kind === "task") {
        // rock: typeahead over THIS domain's ACTIVE rocks — picking stores the
        // rock ID (displays its title); free text commits verbatim. RE cards
        // also search PROPERTIES and DEALS live as you type — the rock slot
        // tethers those too (renderer nests by slug on the Rocks/property
        // views), so nothing has to be typed from memory.
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
      if (p.kind === "task") {
        if (p.rock) f.push("[rock:: " + p.rock + "]");
        if (p.due) f.push("[due:: " + p.due + "]");
        if (p.status) f.push("[status:: " + p.status + "]");
      } else {
        if (p.status) f.push("[status:: " + p.status + "]");
        if (p.needed_by) f.push("[needed_by:: " + p.needed_by + "]");
        if (p.outcome) f.push("[outcome:: " + p.outcome + "]");
      }
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
  try { await fetch(`/api/spirits/approvals/${encodeURIComponent(id)}/${kind}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); setSaveState("saved"); }
  catch (e) { setSaveState("error"); }
  loadFeed(); // approvals live in FEED — the decided card resolves in place
}

if (els.feedRunNowBtn) els.feedRunNowBtn.addEventListener("click", spiritRunNow);
if (els.feedAskBtn) els.feedAskBtn.addEventListener("click", spiritAskScout);
