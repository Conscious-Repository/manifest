// ================= CONSTRUCTION — research runs and evidence (P5) =================
// The agent pane shows the problem's latest research run as its durable
// record says (state, epoch, stages, counts, events) with explicit owner
// controls: start, cancel, resume, retry, accept an observed runtime. The
// page follows the run by asking for events after the last sequence it saw;
// a refresh never starts work. The Research tab shows what each stage
// retained; the Evidence tab shows sources, verified passages, claims and
// the clickable path Source → Evidence → Claim → part → assembly, and lets
// the owner register a retained document as a source and add a passage
// (verified against its page, or kept as an unverified owner excerpt).

const cxr = { seq: 0, cache: {}, msg: "", evMsg: "", open: new Map() };

function cxEnc(s) { return encodeURIComponent(s); }
function cxRunBase() { return "/problems/" + cxEnc(cx.problemId) + "/research-runs"; }
function cxLatestRun() { const v = cx.view; return v && v.problem.latestRun ? (v.runs || {})[v.problem.latestRun] || null : null; }
function cxRunActive(r) { return !!r && ["queued", "running", "stop-requested"].includes(r.state); }
function cxCaps() { const s = cx.session[cxBase(cx.subject)]; return (s && s.capabilities) || {}; }

function cxEvidenceById(id) {
  const ev = cx.view && cx.view.evidence;
  return ev ? (ev.evidence || []).find((e) => e.id === id) || null : null;
}
function cxSourceById(id) {
  const ev = cx.view && cx.view.evidence;
  return ev ? (ev.sources || []).find((s) => s.id === id) || null : null;
}
function cxClaimsFor(evidenceId) {
  const ev = cx.view && cx.view.evidence;
  return ev ? (ev.claims || []).filter((c) => (c.supporting || []).includes(evidenceId) || (c.contradicting || []).includes(evidenceId)) : [];
}

// cxEvidenceRow: one passage as text (quote, page, match, verification, source).
function cxEvidenceRow(e, relation) {
  const s = cxSourceById(e.sourceId) || {};
  const row = el("div", "cx-ev-row cx-ev-" + e.verification);
  row.setAttribute("data-evidence", e.id);
  const head = el("div", "cx-ev-head");
  head.append(el("span", "cx-ev-badge micro-label", (relation ? relation + " · " : "") + e.verification + " · " + e.quoteMatch));
  if (s.fictional) head.append(el("span", "cx-ev-fiction micro-label", "fictional fixture"));
  head.append(el("span", "cx-ev-src", (s.title || e.sourceId) + " · p." + e.page + (e.pageLabel && e.pageLabel !== String(e.page) ? " (" + e.pageLabel + ")" : "")));
  row.append(head, el("blockquote", "cx-ev-quote", e.quote));
  const meta = [s.class || "", e.method, e.proposedBy ? "proposed by " + e.proposedBy : "", e.normalization ? "normalised: " + e.normalization : ""].filter(Boolean).join(" · ");
  row.append(el("div", "cx-ev-meta micro-label", meta));
  return row;
}

// ---- run controls --------------------------------------------------------------------
async function cxRunPost(path, body) {
  cxr.msg = "";
  try {
    const res = await cxApi("POST", cxBase(cx.subject), path, Object.assign({ schemaVersion: 1, requestId: cxRequestId() }, body || {}));
    if (res && res.view) cxApplyView(res.view);
    cxWatchRun();
    return res;
  } catch (e) {
    cxr.msg = (e.message || "refused") + (e.problems && e.problems.length ? ": " + e.problems.join("; ") : "");
    cxRender();
    return null;
  }
}

function cxStartResearch(mode) {
  const body = { expectedProblemRevision: (cx.view.revisions || {}).problem, agent: { mode } };
  if (cx.activeAssembly) body.baseAssembly = cx.activeAssembly;
  return cxRunPost(cxRunBase(), body);
}

// cxWatchRun follows the run's durable events while it is active (bounded
// long-poll); each change reloads the problem's durable state.
async function cxWatchRun() {
  const seq = ++cxr.seq, pid = cx.problemId, subject = cx.subject;
  for (let i = 0; i < 400; i++) {
    const run = cxLatestRun();
    if (!cxRunActive(run) || seq !== cxr.seq || pid !== cx.problemId || !cxSameSubject(subject, cx.subject)) return;
    let ev;
    try { ev = await cxApi("GET", cxBase(subject), cxRunBase() + "/" + cxEnc(run.id) + "/events?after=" + run.sequence + "&wait=1"); }
    catch (e) { return; }
    if (seq !== cxr.seq || pid !== cx.problemId) return;
    if (ev.sequence !== run.sequence || ev.state !== run.state) {
      try {
        const fresh = await cxApi("GET", cxBase(subject), "/problems/" + cxEnc(pid));
        if (seq !== cxr.seq || pid !== cx.problemId) return;
        cxApplyView(fresh);
      } catch (e) { return; }
    }
  }
}

function cxStageChips(run) {
  const wrap = el("div", "cx-stages");
  wrap.setAttribute("aria-label", "Research stages");
  (run.stages || []).forEach((sg) => {
    const last = (sg.attempts || [])[sg.attempts.length - 1];
    const chip = el("span", "cx-stage cx-stage-" + sg.state, sg.name + (sg.attempts && sg.attempts.length > 1 ? " ×" + sg.attempts.length : ""));
    chip.title = sg.name + ": " + sg.state + (last && last.summary ? " — " + last.summary : "") + (last && last.error ? " (" + last.error.class + ")" : "");
    chip.setAttribute("data-stage", sg.name);
    wrap.append(chip);
  });
  return wrap;
}

function cxPaintResearchRun(body) {
  const blk = el("div", "cx-block cx-research");
  blk.append(el("div", "cx-label micro-label", "Research"));
  const caps = cxCaps();
  const ro = !!cx.view.readOnly;
  const run = cxLatestRun();
  if (run) {
    const line = el("p", "cx-run-state");
    line.setAttribute("role", "status");
    line.append(el("span", "cx-run-badge cx-run-" + run.state, run.state), document.createTextNode(" · epoch " + run.epoch + " · " + run.agent.mode + " · " + run.agent.agent));
    blk.append(line, cxStageChips(run));
    const c = run.counts || {};
    blk.append(el("p", "cx-hint", (c.sourcesConsidered || 0) + " sources considered · " + (c.sourcesRetained || 0) + " retained · " + (c.evidenceVerified || 0) + " passages verified · " + (c.evidenceRejected || 0) + " rejected · " + (c.alternatives || 0) + " alternatives"));
    const evs = (run.events || []).slice(-4).reverse();
    const list = el("ol", "cx-run-events");
    evs.forEach((e) => list.append(el("li", null, "#" + e.seq + " " + (e.message || e.state))));
    blk.append(list);
    const btns = el("div", "cx-row-edit");
    const add = (label, fn) => { const b = pillLight(label, fn); b.disabled = ro; btns.append(b); };
    if (cxRunActive(run)) add("Cancel research", () => cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/cancel"));
    if (run.state === "planned") add("Start this run", () => cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/start"));
    if (["cancelled", "disconnected"].includes(run.state)) add("Resume research", () => cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/resume"));
    if (["failed", "cancelled", "disconnected", "waiting-input"].includes(run.state)) add("Retry research", () => cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/retry"));
    if (run.state === "waiting-input") add("Accept observed runtime and resume", () => cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/resume", { acceptRuntime: true }));
    if (!cxRunActive(run) && run.state !== "planned") add("Start new research", () => cxStartResearch("local-only"));
    blk.append(btns);
    if (run.state === "disconnected") blk.append(el("p", "cx-hint", "The server restarted while this run was active. Nothing was resumed automatically; Resume reuses retained results and never resends an uncertain agent request."));
    if (cxRunActive(run)) cxWatchRun();
  } else {
    blk.append(el("p", "cx-empty", "No research run yet."));
    const b = pillLight("Start research", () => cxStartResearch("local-only"));
    b.disabled = ro;
    blk.append(b);
  }
  if (caps.nativeAgent === "available" && !cxRunActive(run)) {
    const nb = pillLight("Start research with the native agent", () => cxStartResearch("native"));
    nb.disabled = ro;
    blk.append(nb);
  }
  (caps.notes || []).forEach((n) => blk.append(el("p", "cx-hint cx-cap-note", n)));
  if (cxr.msg) { const m = el("p", "cx-form-msg", cxr.msg); m.setAttribute("role", "alert"); blk.append(m); }
  body.append(blk);
}

// ---- Research tab: what each stage retained ------------------------------------------------
async function cxRunDetail(run) {
  const rev = (cx.view.revisions || {})["run:" + run.id];
  const key = run.id + "@" + rev;
  if (cxr.cache[key]) return cxr.cache[key];
  const res = await cxApi("GET", cxBase(cx.subject), cxRunBase() + "/" + cxEnc(run.id));
  cxr.cache = { [key]: res };
  return res;
}

function cxList(title, items, cls) {
  const box = el("div", "cx-block " + (cls || ""));
  box.append(el("div", "cx-label micro-label", title + " (" + items.length + ")"));
  if (!items.length) box.append(el("p", "cx-empty", "None."));
  const ul = el("ul", "cx-plain");
  items.forEach((t) => ul.append(el("li", null, t)));
  box.append(ul);
  return box;
}

async function cxPaintResearchTab(b) {
  const run = cxLatestRun();
  if (!run) { b.append(el("p", "cx-empty", "No research run yet. Start one from the agent pane.")); return; }
  b.append(el("p", "cx-hint", "Run " + run.id.slice(0, 12) + " · " + run.state + " · every stage result below is a retained artifact of this run; nothing here is approved for construction."));
  const holder = el("div", "cx-run-detail");
  holder.append(el("p", "cx-empty", "Loading…"));
  b.append(holder);
  let d;
  try { d = await cxRunDetail(run); } catch (e) { holder.innerHTML = ""; holder.append(el("p", "cx-empty", "Couldn't load the run: " + e.message)); return; }
  holder.innerHTML = "";
  const r = d.results || {};
  const dec = r.decompose;
  if (dec) {
    const box = el("div", "cx-block");
    box.append(el("div", "cx-label micro-label", "Decomposition"));
    (dec.dimensions || []).forEach((dm) => box.append(el("div", "cx-dimension cx-dimension-" + dm.status, dm.label + " — " + dm.status + (dm.facts && dm.facts.length ? ": " + dm.facts.map((f) => f.text).join("; ") : ""))));
    holder.append(box);
  }
  const qbox = el("div", "cx-block");
  qbox.append(el("div", "cx-label micro-label", "Questions"));
  (run.plan.questions || []).forEach((q, i) => {
    const row = el("div", "cx-q-row");
    const inp = el("input", "cx-in cx-q-text");
    inp.value = q.text; inp.setAttribute("aria-label", "Research question " + (i + 1));
    inp.disabled = cxRunActive(run) || !!cx.view.readOnly;
    row.append(el("span", "micro-label cx-q-status", q.status), inp);
    qbox.append(row);
  });
  if (!cxRunActive(run) && (run.plan.questions || []).length) {
    const save = pillLight("Save corrected questions", () => {
      const qs = (run.plan.questions || []).map((q, i) => ({ id: q.id, text: qbox.querySelectorAll(".cx-q-text")[i].value, status: q.status }));
      cxRunPost(cxRunBase() + "/" + cxEnc(run.id) + "/questions", { questions: qs });
    });
    save.disabled = !!cx.view.readOnly;
    qbox.append(save, el("p", "cx-hint", "Correcting questions re-runs decomposition and planning only; retained acquisition is reused when the sources needed are the same."));
  }
  holder.append(qbox);
  const acq = r.acquire;
  if (acq) {
    const box = el("div", "cx-block");
    box.append(el("div", "cx-label micro-label", "Acquisition · " + acq.autonomousAcquisition));
    const t = el("table", "cx-compare cx-acq");
    const h = el("tr");
    ["Source", "Class", "Outcome", "Note"].forEach((x) => h.append(el("th", "micro-label", x)));
    t.append(h);
    (acq.sources || []).forEach((rec) => {
      const tr = el("tr", "cx-acq-" + rec.outcome);
      tr.append(el("td", null, rec.source.title + (rec.source.fictional ? " (fictional)" : "")), el("td", null, rec.source.class),
        el("td", null, rec.outcome + (rec.errorClass ? " · " + rec.errorClass : "")), el("td", null, [rec.message || "", ...(rec.source.warnings || [])].filter(Boolean).join(" · ")));
      t.append(tr);
    });
    box.append(t);
    holder.append(box);
  }
  const ext = r.extract;
  if (ext) {
    holder.append(cxList("Rejected passages (no manufactured citations)", (ext.rejected || []).map((x) => "p." + x.page + " “" + x.quote + "” — " + x.reason), "cx-rejected"));
    holder.append(cxList("Owner excerpts needed", ext.requests || [], "cx-requests"));
    if ((ext.warnings || []).length) holder.append(cxList("Warnings", ext.warnings, "cx-warnings"));
  }
  const syn = r.synthesize;
  if (syn) {
    holder.append(cxList("Conditional alternatives", (syn.alternatives || []).map((a) => a.name + " — " + a.conditions.join("; ")), "cx-syn-alts"));
    holder.append(cxList("Missing research", syn.missing || [], "cx-missing"));
    holder.append(cxList("Evidence found but not used", syn.notUsed || [], "cx-notused"));
  }
  if (run.publication) holder.append(el("p", "cx-hint", "Published " + run.publication.assemblies.length + " alternatives for review at epoch " + run.publication.epoch + " (" + (run.publication.publishedAt || "").slice(0, 19).replace("T", " ") + ")."));
}

// ---- Evidence tab -------------------------------------------------------------------------
async function cxShowPath(box, evidenceId, target) {
  box.innerHTML = "";
  const a = cxAsm();
  if (!a) return;
  const t = target || a.junction.id;
  try {
    const res = await cxApi("GET", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + "/evidence/paths?assembly=" + cxEnc(a.id) + "&target=" + cxEnc(t));
    const paths = (res.paths || []).filter((p) => !evidenceId || p.some((n) => n.id === evidenceId));
    if (!paths.length) { box.append(el("p", "cx-empty", "No path from a source to this alternative through that passage.")); return; }
    paths.forEach((p) => {
      const row = el("div", "cx-path");
      row.setAttribute("aria-label", "Evidence path");
      p.forEach((n, i) => {
        if (i) row.append(el("span", "cx-path-edge micro-label", "→ " + (n.edge || "")));
        const part = n.kind === "construction-component" || n.kind === "construction-junction";
        const node = el(part ? "button" : "span", "cx-path-node cx-path-" + n.kind.replace("construction-", ""), n.label);
        if (part) {
          node.type = "button";
          node.title = "Select " + n.label + " in the model";
          node.onclick = () => { if (n.kind === "construction-component") cxSelect(n.id); };
        }
        row.append(node);
      });
      box.append(row);
    });
  } catch (e) { box.append(el("p", "cx-empty", "Couldn't load the path: " + e.message)); }
}

async function cxEvidenceCommand(path, ops, assembly) {
  const body = { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId, operations: ops };
  if (assembly) { body.assemblyId = assembly; body.expectedAssemblyRevision = (cx.view.revisions || {})["assembly:" + assembly]; }
  try {
    const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + path, body);
    cxr.evMsg = "";
    if (res && res.view) cxApplyView(res.view);
    return true;
  } catch (e) {
    cxr.evMsg = (e.message || "refused") + (e.problems && e.problems.length ? ": " + e.problems.join("; ") : "");
    cxRender();
    return false;
  }
}

function cxPaintEvidenceTab(b) {
  const ev = cx.view.evidence || { sources: [], evidence: [], claims: [] };
  const a = cxAsm(), ro = !!cx.view.readOnly;
  b.append(el("p", "cx-hint", ev.sources.length + " sources · " + ev.evidence.length + " passages · " + ev.claims.length + " claims. A quote is verified only when it is found on its page of the retained source; a URL alone is never evidence."));
  if (cxr.evMsg) { const m = el("p", "cx-form-msg", cxr.evMsg); m.setAttribute("role", "alert"); b.append(m); }
  const pathBox = el("div", "cx-path-box");
  pathBox.setAttribute("aria-live", "polite");
  b.append(pathBox);
  const linked = new Map(((a && a.evidenceLinks) || []).map((l) => [l.evidenceId, l]));
  ev.sources.forEach((s) => {
    const box = el("details", "cx-src");
    const hasLinked = ev.evidence.some((e) => e.sourceId === s.id && linked.has(e.id));
    box.open = cxr.open.has(s.id) ? cxr.open.get(s.id) : (hasLinked || ev.sources.length <= 3);
    box.addEventListener("toggle", () => cxr.open.set(s.id, box.open));
    const sum = el("summary", "cx-src-sum");
    sum.append(el("span", "cx-src-title", s.title), el("span", "micro-label cx-src-meta", s.class + " · " + s.access + " · pages " + (s.extraction.pageMap === "unavailable" ? "unavailable" : s.extraction.pages + " (" + s.extraction.pageMap + ")") + (s.fictional ? " · FICTIONAL FIXTURE" : "")));
    (s.warnings || []).forEach((w) => sum.append(el("span", "cx-src-warn", w)));
    box.append(sum);
    if (s.snapshot) {
      const link = el("a", "cx-input-name", "Retained snapshot " + s.snapshot.revision.slice(0, 10));
      link.href = cxArtifactURL(s.snapshot.id, s.snapshot.revision, true);
      box.append(link);
    }
    ev.evidence.filter((e) => e.sourceId === s.id).forEach((e) => {
      const row = cxEvidenceRow(e, linked.has(e.id) ? linked.get(e.id).relation : "");
      cxClaimsFor(e.id).forEach((c) => row.append(el("div", "cx-claim cx-claim-" + c.verification, "Claim (" + c.provenance + ", " + c.verification + "): " + c.statement)));
      const acts = el("div", "cx-row-edit");
      const show = el("button", "cx-link", "Show path");
      show.type = "button";
      show.onclick = () => cxShowPath(pathBox, e.id, linked.has(e.id) ? linked.get(e.id).target : null);
      acts.append(show);
      if (a && !ro) {
        const target = cx.selection || a.junction.id;
        const tname = cx.selection ? ((cxComp(cx.selection) || {}).name || cx.selection) : "the junction";
        const link = el("button", "cx-link", linked.has(e.id) ? "Unlink" : "Link to " + tname);
        link.type = "button";
        link.onclick = () => cxEvidenceCommand("/evidence", [linked.has(e.id) ? { op: "LinkEvidence", target: linked.get(e.id).target, evidenceId: e.id, relation: linked.get(e.id).relation, remove: true }
          : { op: "LinkEvidence", target, evidenceId: e.id, relation: (cxClaimsFor(e.id)[0] || {}).contradicting && cxClaimsFor(e.id)[0].contradicting.includes(e.id) ? "contradicts" : "supports" }], a.id);
        acts.append(link);
      }
      row.append(acts);
      box.append(row);
    });
    b.append(box);
  });
  if (!ev.sources.length) b.append(el("p", "cx-empty", "No sources yet. Register a retained document below, or run research."));
  if (!ro) b.append(cxSourceForm(), cxPassageForm(ev));
}

function cxSourceForm() {
  const f = el("div", "cx-block cx-form");
  f.append(el("div", "cx-label micro-label", "Register a source"));
  const docs = (cx.view.problem.inputs || []).filter((i) => i.role === "document" || i.role === "drawing");
  const title = el("input", "cx-in"); title.setAttribute("aria-label", "Source title"); title.placeholder = "Title";
  const cls = selectEl(["owner-document", "manufacturer", "trade-association", "engineering", "detail-library", "code", "code-interpretation", "secondary"]);
  cls.className = "cx-in"; cls.setAttribute("aria-label", "Source class");
  const input = selectEl([]); input.className = "cx-in"; input.setAttribute("aria-label", "Retained document");
  const none = document.createElement("option"); none.value = ""; none.textContent = "No retained file (URL metadata only)"; input.append(none);
  docs.forEach((d) => { const o = document.createElement("option"); o.value = d.artifactId + "@" + d.revision; o.textContent = (d.label || d.name) + " · " + d.mime; input.append(o); });
  const url = el("input", "cx-in"); url.setAttribute("aria-label", "Source URL (metadata, never fetched)"); url.placeholder = "https://… (recorded, never fetched)";
  const go = pillLight("Add source", () => {
    const op = { op: "AddSource", id: cxNewId("src"), title: title.value, class: cls.value };
    if (input.value) { const [id, revision] = input.value.split("@"); op.input = { id, revision }; op.access = "open"; }
    if (url.value) op.url = url.value;
    cxEvidenceCommand("/sources", [op]);
  });
  f.append(title, cls, input, url, go);
  return f;
}

function cxPassageForm(ev) {
  const f = el("div", "cx-block cx-form");
  f.append(el("div", "cx-label micro-label", "Add a passage"));
  if (!ev.sources.length) { f.append(el("p", "cx-empty", "Register a source first.")); return f; }
  const src = selectEl([]); src.className = "cx-in"; src.setAttribute("aria-label", "Passage source");
  ev.sources.forEach((s) => { const o = document.createElement("option"); o.value = s.id; o.textContent = s.title; src.append(o); });
  const page = el("input", "cx-in"); page.type = "number"; page.min = "1"; page.value = "1"; page.setAttribute("aria-label", "Page");
  const quote = el("textarea", "cx-in cx-narrative"); quote.setAttribute("aria-label", "Quote (verbatim)");
  const claim = el("input", "cx-in"); claim.setAttribute("aria-label", "Claim it supports");
  const rel = selectEl(["supports", "contradicts"]); rel.className = "cx-in"; rel.setAttribute("aria-label", "Relation");
  const go = pillLight("Add passage", () => cxEvidenceCommand("/evidence", [{ op: "AddEvidence", id: cxNewId("evd"), claimId: cxNewId("clm"), sourceId: src.value,
    page: Number(page.value), quote: quote.value, claim: claim.value, relation: rel.value, confidence: 0.5 }]));
  f.append(src, page, quote, claim, rel, go, el("p", "cx-hint", "With retained page text the quote must be on that page or nothing is added; otherwise it is kept as an unverified owner excerpt."));
  return f;
}

cxResearchTabs.splice(1, 0, ["research", "Runs", (b) => cxPaintResearchTab(b)], ["evidence", "Evidence", (b) => cxPaintEvidenceTab(b)]);
