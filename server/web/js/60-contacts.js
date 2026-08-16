// ---- router ----
// ---- CONTACTS (people layer over the vault index) ----
async function postJSON(url, body) {
  const res = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  try { return await res.json(); } catch (e) { return {}; }
}

// postJSONOk throws on a non-2xx response so callers can signal real failures
// (postJSON swallows them, which hid write errors behind an optimistic UI).
async function postJSONOk(url, body) {
  const res = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}

function showContacts() {
  const rest = location.hash.replace(/^#\/contacts\/?/, "");
  if (rest === "cold") { _coldOnly = true; showContactList(); } // neglect view (deep-linkable)
  else if (rest) { _coldOnly = false; showContactPage(decodeURIComponent(rest)); }
  else { _coldOnly = false; showContactList(); }
}

function showContactList() {
  els.contactsListPane.hidden = false;
  els.contactPagePane.hidden = true;
  loadContactList();
  loadContactTriage();
  loadContactEmailReview();
}

async function loadContactList() {
  let d = { contacts: [] };
  try { d = await (await fetch("/api/contacts")).json(); } catch (e) {}
  window._contacts = d.contacts || [];
  renderContactList(window._contacts, els.contactSearch.value);
}

let _coldOnly = false;

function renderContactList(list, query) {
  const host = els.contactList; host.innerHTML = "";
  const q = (query || "").trim().toLowerCase();
  let rows = q ? list.filter((c) => c.display.toLowerCase().includes(q)) : list.slice();
  const coldCount = list.filter((c) => c.cold).length;
  if (els.contactColdToggle) {
    els.contactColdToggle.textContent = "◆ Cold" + (coldCount ? " " + coldCount : "");
    els.contactColdToggle.classList.toggle("on", _coldOnly);
  }
  if (_coldOnly) {
    rows = rows.filter((c) => c.cold).sort((a, b) => b.daysSince - a.daysSince); // most overdue first
  }
  if (!rows.length) { host.appendChild(emptyRow(_coldOnly ? "No contacts going cold." : q ? "No contacts match." : "No contacts yet.")); return; }
  rows.forEach((c) => host.appendChild(contactRow(c)));
}

function contactRow(c) {
  const row = el("div", "contact-row" + (c.cold ? " cold" : ""));
  row.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(c.key); };
  const left = el("div", "contact-row-left");
  if (c.cold) left.append(el("span", "contact-cold", "● going cold")); // §13: ink, never amber
  left.append(el("span", "contact-name", c.display));
  if (!c.hasNote) left.append(el("span", "contact-dot", "○")); // quiet no-note indicator
  if (c.openLoops > 0) left.append(el("span", "contact-loops", c.openLoops + " open"));
  const right = el("div", "contact-row-right");
  if (c.upcoming) right.append(el("span", "contact-upcoming", "↑ " + c.upcoming));
  // "met" = calendar-verified (email-matched); "mentioned" = note-based. Distinct
  // signals: met is headlined when present; the going-cold line names its basis.
  if (c.cold && c.daysSince >= 0) {
    const verb = c.neglectBasis === "meetings" ? "met" : "mentioned";
    right.append(el("span", "contact-meta", verb + " " + c.daysSince + "d ago (usually every " + c.medianGap + "d)"));
  } else if (c.lastMet) {
    right.append(el("span", "contact-meta", "met " + c.lastMet));
    if (c.lastMentioned && c.lastMentioned !== c.lastMet) right.append(el("span", "contact-submeta", "mentioned " + c.lastMentioned));
  } else if (c.lastMentioned) {
    right.append(el("span", "contact-meta muted", "mentioned " + c.lastMentioned));
  }
  row.append(left, right);
  return row;
}

async function loadContactTriage() {
  let d = { triage: [] };
  try { d = await (await fetch("/api/contacts/triage")).json(); } catch (e) {}
  renderTriage(d.triage || []);
}

function renderTriage(items) {
  const host = els.contactTriage; host.innerHTML = "";
  if (!items.length) { host.hidden = true; return; }
  host.hidden = false;
  window._triage = items;
  // Quiet by default (§4): a one-line summary that expands to a review batch,
  // ranked most-person-like first (deterministic: 2+ caps up, linked-from-people down).
  const head = el("div", "triage-head");
  const label = el("span", "triage-label", "Review — " + items.length + " note-less name" + (items.length === 1 ? "" : "s") + " (ranked by person-likelihood)");
  const headActions = el("span", "triage-head-actions");
  const bulk = pillLight("Dismiss all " + items.length, async () => {
    if (!confirm("Dismiss all " + items.length + " queued names? (remembered — they won't return)")) return;
    await postJSON("/api/contacts/dismiss-bulk", { keys: items.map((t) => t.key) });
    showContactList();
  });
  bulk.hidden = true;
  const toggle = pillLight("Review ▾", () => {
    rows.hidden = !rows.hidden; bulk.hidden = rows.hidden;
    toggle.textContent = rows.hidden ? "Review ▾" : "Hide ▴";
  });
  headActions.append(bulk, toggle);
  head.append(label, headActions);
  const rows = el("div", "triage-rows"); rows.hidden = true;
  items.slice(0, 30).forEach((t) => {
    const r = el("div", "triage-row");
    const nm = el("span", "triage-name", t.display);
    if (t.likelyOrg) nm.append(el("span", "triage-hint", " likely org"));
    r.append(nm, el("span", "triage-refs", t.refCount + " ref" + (t.refCount === 1 ? "" : "s")));
    const act = el("span", "triage-actions");
    act.append(
      pill("Person", async () => { await postJSON("/api/contacts/confirm", { key: t.key, display: t.display }); showContactList(); }),
      pillLight("Org", async () => { await postJSON("/api/contacts/org", { key: t.key }); showContactList(); }),
      pillLight("Dismiss", async () => { await postJSON("/api/contacts/dismiss", { key: t.key }); showContactList(); }),
    );
    r.append(act);
    rows.append(r);
  });
  host.append(head, rows);
}

// ---- email-linking review queue (§4) — mirrors the triage strip ----
async function loadContactEmailReview() {
  let d = { candidates: [] };
  try { d = await (await fetch("/api/contacts/email-review")).json(); } catch (e) {}
  renderEmailReview(d.candidates || []);
}

let _emailReviewOpen = false; // preserve expand/collapse across in-place updates

function renderEmailReview(items) {
  const host = els.contactEmailReview; if (!host) return;
  host.innerHTML = "";
  if (!items.length) { host.hidden = true; return; }
  host.hidden = false;
  const head = el("div", "triage-head");
  const label = el("span", "triage-label", "");
  const rows = el("div", "triage-rows");
  const toggle = pillLight("", () => setOpen(!_emailReviewOpen));
  const setOpen = (open) => {
    _emailReviewOpen = open;
    rows.hidden = !open;
    toggle.textContent = open ? "Hide ▴" : "Review ▾";
  };
  const setCount = (n) => {
    label.textContent = "Review — " + n + " unlinked email" + (n === 1 ? "" : "s") + " (link calendar attendees to contacts)";
    if (n === 0) host.hidden = true; // last one linked/dismissed → strip disappears
  };
  // ctx lets a row remove itself and update the count WITHOUT re-rendering the whole
  // strip (which would collapse it and lose the user's place).
  const ctx = { remove: (row) => { row.remove(); setCount(rows.children.length); } };
  const headActions = el("span", "triage-head-actions"); headActions.append(toggle);
  head.append(label, headActions);
  items.forEach((c) => rows.append(emailReviewRow(c, ctx)));
  host.append(head, rows);
  setCount(rows.children.length);
  setOpen(_emailReviewOpen);
}

function emailReviewRow(c, ctx) {
  const r = el("div", "triage-row");
  const who = el("span", "triage-name", c.attendeeName || c.email);
  who.append(el("span", "triage-hint", " " + c.email));
  r.append(who, el("span", "er-arrow", "→"), el("span", "er-target", c.contactDisplay));
  r.lastChild.append(el("span", "triage-hint", c.via === "email" ? " email match" : " name match"));
  const flash = el("span", "er-flash"); flash.hidden = true;
  const act = el("span", "triage-actions");
  const link = pill("Link", () => doLink(c.contactKey, c.contactDisplay, c.email));
  const dismiss = pillLight("Dismiss", async () => {
    dismiss.disabled = true;
    try { await postJSONOk("/api/contacts/email-dismiss", { email: c.email, key: c.contactKey }); }
    catch (e) { dismiss.disabled = false; showFlash(flash, "✕ " + errMsg(e), true); return; }
    ctx.remove(r);
  });
  // link the email to `key`, then signal + fade the row out in place (strip stays open)
  async function doLink(key, display, email) {
    link.disabled = true; showFlash(flash, "linking…", false);
    try { await postJSONOk("/api/contacts/email", { key, display, email }); }
    catch (e) { link.disabled = false; showFlash(flash, "✕ " + errMsg(e), true); return; }
    showFlash(flash, "✓ linked " + email + " → " + display, false);
    r.classList.add("er-done");
    loadContactList(); // the contact's list row now shows a calendar "met" date
    setTimeout(() => ctx.remove(r), 1000);
  }
  act.append(link, pillLight("Different contact", () => openEmailReassign(r, c, doLink)), dismiss);
  r.append(act, flash);
  return r;
}

function showFlash(node, msg, isError) {
  node.textContent = msg; node.hidden = false;
  node.classList.toggle("error", !!isError);
}
function errMsg(e) { return (e && e.message) ? e.message : "failed"; }

// openEmailReassign lets the user link this email to a DIFFERENT contact than the
// suggested one (inline search — same shape as the create-contact search).
function openEmailReassign(row, c, doLink) {
  if (row.querySelector(".er-search")) return;
  const box = el("div", "er-search");
  const input = el("input", "contact-create-input"); input.type = "text";
  input.placeholder = "Link " + c.email + " to another contact…";
  const results = el("div", "contact-create-results");
  box.append(input, results);
  row.append(box);
  input.focus();
  let timer;
  input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      results.innerHTML = "";
      const q = input.value.trim();
      if (!q) return;
      let d = { results: [] };
      try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
      (d.results || []).forEach((rf) => {
        const rr = el("div", "cc-result");
        rr.append(el("span", "cc-name", rf.display));
        rr.append(pill("Link here", () => { box.remove(); doLink(rf.key, rf.display, c.email); }));
        results.append(rr);
      });
    }, 200);
  });
}

async function showContactPage(key) {
  els.contactsListPane.hidden = true;
  els.contactPagePane.hidden = false;
  els.contactPageSaved.textContent = "";
  els.contactPage.textContent = "Loading…";
  let p;
  try {
    const res = await fetch("/api/contacts/page?key=" + encodeURIComponent(key));
    if (!res.ok) { els.contactPage.textContent = "No such contact."; return; }
    p = await res.json();
  } catch (e) { els.contactPage.textContent = "Error loading contact."; return; }
  renderContactPage(p);
}

function cpSection(title, count) {
  const s = el("div", "cp-section");
  const h = el("div", "cp-section-head", title);
  if (count != null) h.append(el("span", "cp-count", " " + count));
  s.append(h);
  return s;
}

function renderContactPage(p) {
  const host = els.contactPage; host.innerHTML = "";

  // 1. header — name, aliases, linked firms
  const header = el("div", "cp-header");
  const nameRow = el("div", "cp-name-row");
  nameRow.append(el("h1", "cp-name", p.display));
  if (p.role) nameRow.append(el("span", "cp-role", p.role.toUpperCase())); // §13: neutral role chip from the note's role:
  if (!p.hasNote) nameRow.append(el("span", "cp-nonote", "no note yet"));
  header.append(nameRow);
  if (p.aliases && p.aliases.length) header.append(el("div", "cp-aliases", "aka " + p.aliases.join(" · ")));
  if (p.firms && p.firms.length) {
    const f = el("div", "cp-firms");
    p.firms.forEach((fr) => {
      const chip = el("span", "cp-firm", fr.display);
      chip.onclick = () => { location.hash = "#/contacts/" + encodeURIComponent(fr.key); };
      f.append(chip);
    });
    header.append(f);
  }
  host.append(header);

  // facts strip first (redesign §13) — four columns between hairlines, the
  // same anatomy as the property stat strip. "Last met" (calendar-verified)
  // stays DISTINCT from "last mentioned" (notes).
  const strip = el("div", "cp-facts");
  const cell = (label, val, sub) => {
    const c = el("div", "cp-fact");
    c.append(el("div", "cp-fact-label", label), el("div", "cp-fact-val", val || "—"));
    if (sub) c.append(el("div", "cp-fact-sub", sub));
    return c;
  };
  strip.append(cell("LAST MET", p.lastMet, p.lastMet ? "calendar-verified" : "no meeting on record"));
  strip.append(cell("LAST MENTIONED", p.lastMentioned, p.lastMentioned ? "notes" : ""));
  strip.append(cell("CADENCE", p.medianGap ? "every " + p.medianGap + "d" : "", p.medianGap ? "median gap" : ""));
  strip.append(cell("EMAILS", (p.emails && p.emails.length) ? String(p.emails.length) : "", (p.emails && p.emails.length) ? "linked" : "link one below"));
  host.append(strip);
  // going-cold: weight + a dot, never a color — names its basis
  if (p.cold && p.daysSince >= 0) {
    const verb = p.neglectBasis === "meetings" ? "met" : "mentioned";
    host.append(el("div", "cp-cold", "● going cold — " + verb + " " + p.daysSince + "d ago" + (p.medianGap ? " (usually every " + p.medianGap + "d)" : "")));
  }

  // open loops (§2) — unchecked tasks from meeting notes, grouped by source
  if (p.loops && p.loops.length) {
    let n = 0; p.loops.forEach((g) => (n += g.loops.length));
    const sec = cpSection("Open loops", n);
    p.loops.forEach((g) => {
      const gh = el("div", "cp-loop-group");
      const head = el("div", "cp-loop-src");
      head.append(el("span", "cp-date", g.date), el("span", "cp-loop-note", g.name));
      head.onclick = () => { _noteReturn = "#/contacts/" + encodeURIComponent(p.key); openNoteByPath(g.path); };
      gh.append(head);
      g.loops.forEach((it) => {
        const row = el("label", "cp-loop-row");
        if (it.kind === "checkbox") {
          const box = el("input"); box.type = "checkbox";
          box.addEventListener("change", async () => {
            await postJSON("/api/note/task", { path: g.path, line: it.line, want: box.checked });
            showContactPage(p.key);
          });
          row.append(box);
        } else {
          row.append(el("span", "cp-loop-dot", "›"));
        }
        row.append(el("span", "cp-loop-text", it.text));
        gh.append(row);
      });
      sec.append(gh);
    });
    host.append(sec);
  }

  // waiting on them — todos-board delegations tracked on this person
  if (p.waitingOn && p.waitingOn.length) {
    const sec = cpSection("Waiting on them", p.waitingOn.length);
    p.waitingOn.forEach((wo) => {
      const row = el("div", "cp-loop-row");
      row.append(el("span", "cp-loop-dot", "⏳"),
        el("span", "cp-loop-text", wo.text),
        el("span", "cp-date", wo.domain + " · " + wo.ageDays + "d"));
      row.style.cursor = "pointer";
      row.onclick = () => { location.hash = "#/tasks"; };
      sec.append(row);
    });
    host.append(sec);
  }

  // 2. upcoming (matched calendar events / candidates to confirm)
  if (p.upcoming && p.upcoming.length) {
    const sec = cpSection("Upcoming");
    p.upcoming.forEach((u) => {
      const row = el("div", "cp-upcoming-row");
      row.append(el("span", "cp-date", u.date), el("span", "cp-title", u.title));
      if (!u.confirmed && u.email) {
        row.append(pill("This is " + p.display + " (" + u.email + ")", async () => {
          await postJSON("/api/contacts/email", { key: p.key, display: p.display, email: u.email });
          showContactPage(p.key);
        }));
      } else if (u.confirmed) {
        row.append(el("span", "cp-confirmed", "✓ matched"));
      }
      sec.append(row);
    });
    host.append(sec);
  }

  const openItem = (path) => { _noteReturn = "#/contacts/" + encodeURIComponent(p.key); openNoteByPath(path); };

  // 3. ONE timeline (§13): meetings + notes + transcripts merged newest-first.
  // The CALENDAR badge (accent on accent-soft) marks calendar-verified rows
  // ONLY; everything else says what it is in the src column. Deduped by path
  // (a transcript indexed into the timeline never doubles).
  const merged = [];
  const seenPath = new Set();
  (p.meetings || []).forEach((m) => merged.push({ date: m.date, name: m.title, src: "meeting", calendar: true, path: null }));
  (p.timeline || []).forEach((t) => {
    if (t.path) seenPath.add(t.path);
    merged.push({ date: t.date, name: t.name, src: t.isTranscript ? "transcript" : (t.sourceType || "note"), path: t.path });
  });
  (p.transcripts || []).forEach((t) => {
    if (t.path && seenPath.has(t.path)) return;
    merged.push({ date: t.date, name: t.title, src: "transcript", path: t.path });
  });
  merged.sort((a, b) => (b.date || "").localeCompare(a.date || ""));
  const tl = cpSection("Timeline", merged.length);
  if (!merged.length) tl.append(el("div", "cp-empty", "No dated interactions."));
  merged.forEach((t) => {
    const row = el("div", "cp-tl-row" + (t.path ? " cp-clickable" : ""));
    row.append(el("span", "cp-date", t.date), el("span", "cp-src", t.src), el("span", "cp-tl-name", t.name));
    if (t.calendar) row.append(el("span", "cp-badge", "CALENDAR"));
    if (t.path) row.onclick = () => openItem(t.path);
    tl.append(row);
  });
  host.append(tl);

  // 5. mentions (undated — never a date claim)
  if (p.mentions && p.mentions.length) {
    const sec = cpSection("Mentions (no date)", p.mentions.length);
    p.mentions.forEach((m) => {
      const row = el("div", "cp-mention cp-clickable", m.name);
      row.onclick = () => openItem(m.path);
      sec.append(row);
    });
    host.append(sec);
  }

  // Emails — the contact's linked calendar identities (these drive "last met").
  // Add more by hand, and act on any pending suggestions for THIS person.
  const esec = cpSection("Emails", p.emails ? p.emails.length : 0);
  (p.emails || []).forEach((em) => esec.append(el("div", "cp-email", em)));
  if (!p.emails || !p.emails.length) esec.append(el("div", "cp-empty", "No linked emails — calendar meetings match once you link one."));
  const addRow = el("div", "cp-email-add");
  const einp = el("input", "cp-email-input"); einp.type = "email"; einp.placeholder = "add an email…";
  const doAdd = async () => {
    const email = einp.value.trim();
    if (!email) return;
    const np = await postJSON("/api/contacts/email", { key: p.key, display: p.display, email });
    renderContactPage(np);
  };
  einp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); doAdd(); } });
  addRow.append(einp, pill("Link email", doAdd));
  esec.append(addRow);
  host.append(esec);
  // pending suggestions for THIS contact (from the review queue, async)
  fetch("/api/contacts/email-review").then((r) => r.json()).then((d) => {
    (d.candidates || []).filter((c) => c.contactKey === p.key).forEach((c) => {
      const sug = el("div", "cp-email-suggest");
      sug.append(el("span", "cp-email-suggest-text", "You met " + p.display + " on " + c.metOn + " — link " + c.email + "?"));
      sug.append(
        pill("Link", async () => { renderContactPage(await postJSON("/api/contacts/email", { key: p.key, display: p.display, email: c.email })); }),
        pillLight("Dismiss", async () => { await postJSON("/api/contacts/email-dismiss", { email: c.email, key: p.key }); sug.remove(); }),
      );
      esec.append(sug);
    });
  }).catch(() => {});

  // 6. note pane — raw-markdown editor; blank + placeholder when no note exists
  const note = cpSection("Note");
  const ta = el("textarea", "cp-note-editor");
  ta.value = p.noteBody || "";
  ta.placeholder = "notes about " + p.display + "…  (type [[ to link a name)";
  attachWikilinkAutocomplete(ta);
  note.append(ta);
  const actions = el("div", "cp-note-actions");
  const saveBtn = pill(p.hasNote ? "Save note" : "Create note", async () => {
    els.contactPageSaved.textContent = "saving…";
    const np = await postJSON("/api/contacts/note", { key: p.key, display: p.display, body: ta.value });
    els.contactPageSaved.textContent = "saved";
    renderContactPage(np);
  });
  actions.append(saveBtn);
  if (!p.hasNote) actions.append(el("span", "cp-note-hint", "first save creates " + p.display + ".md with categories: [people]"));
  note.append(actions);
  host.append(note);
}

// create flow — bind to existing links before making a new contact (§5)
function openCreatePanel() {
  if (document.querySelector(".contact-create")) return;
  const box = el("div", "contact-create");
  const head = el("div", "contact-create-head", "Add a contact — existing links are checked first");
  head.append(pillLight("✕", () => box.remove()));
  const input = el("input", "contact-create-input"); input.type = "text"; input.placeholder = "Type a name…";
  const results = el("div", "contact-create-results");
  box.append(head, input, results);
  els.contactList.before(box);
  input.focus();
  let timer;
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => runCreateSearch(input.value.trim(), results), 200); });
}

async function runCreateSearch(q, host) {
  host.innerHTML = "";
  if (!q) return;
  let d = { results: [] };
  try { d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json(); } catch (e) {}
  (d.results || []).forEach((r) => {
    const row = el("div", "cc-result");
    row.append(el("span", "cc-name", r.display), el("span", "cc-refs", r.refCount + " ref" + (r.refCount === 1 ? "" : "s") + (r.hasNote ? " · has note" : "")));
    const act = el("span", "cc-actions");
    act.append(
      pillLight("Open", () => { location.hash = "#/contacts/" + encodeURIComponent(r.key); }),
      pill("Bind “" + q + "”", async () => { await postJSON("/api/contacts/bind", { variant: q, canonical: r.key, display: q }); location.hash = "#/contacts/" + encodeURIComponent(r.key); }),
    );
    row.append(act);
    host.append(row);
  });
  const create = el("div", "cc-create");
  create.append(pill("Create new contact “" + q + "”", async () => {
    const p = await postJSON("/api/contacts/note", { key: q.toLowerCase(), display: q, body: "" });
    location.hash = "#/contacts/" + encodeURIComponent(p.key || q.toLowerCase());
  }));
  host.append(create);
}

if (els.contactSearch) els.contactSearch.addEventListener("input", () => renderContactList(window._contacts || [], els.contactSearch.value));
if (els.contactColdToggle) els.contactColdToggle.addEventListener("click", () => { location.hash = _coldOnly ? "#/contacts" : "#/contacts/cold"; });
if (els.contactAddBtn) els.contactAddBtn.addEventListener("click", openCreatePanel);
if (els.contactBackBtn) els.contactBackBtn.addEventListener("click", () => { location.hash = "#/contacts"; });
