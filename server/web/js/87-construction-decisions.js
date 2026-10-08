// ================= CONSTRUCTION — decisions, comparison, revisions (P7) =================
// A decision binds one exact alternative revision and keeps its unresolved
// issues. Anyone may propose; only the owner accepts (for the project —
// never as approval for construction) or rejects, naming the exact decision
// revision they reviewed. A later change to the bound alternative shows the
// acceptance as stale; the selection never follows the head. Comparison and
// revision history come from the server's exact revisions; restoring a
// revision is a new revision.

const cxd = { msg: "", cmp: null };

function cxDecisionStates() { return (cx.view && cx.view.decisionStates) || {}; }

async function cxDecisionCommand(op) {
  try {
    const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + "/decisions",
      { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId, operations: [op] });
    cxd.msg = "";
    if (res && res.view) cxApplyView(res.view);
  } catch (e) {
    cxd.msg = (e.message || "refused") + (e.problems && e.problems.length ? ": " + e.problems.join("; ") : "");
    cxRender();
  }
}

function cxAsmName(id) { const a = (cx.view.assemblies || {})[id]; return a ? a.name : id; }

function cxPaintDecisionsTab(b) {
  const v = cx.view, ro = !!v.readOnly, states = cxDecisionStates();
  b.append(el("p", "cx-notice", "Accepted for project is a human project decision, never approval for construction. " + v.notice));
  if (cxd.msg) { const m = el("p", "cx-form-msg", cxd.msg); m.setAttribute("role", "alert"); b.append(m); }
  if (v.problem.selectedAssembly) {
    const s = v.problem.selectedAssembly;
    b.append(el("p", "cx-hint cx-selected", "Selected for the project: " + cxAsmName(s.id) + " at revision " + s.revision.slice(0, 10) + ((v.revisions || {})["assembly:" + s.id] !== s.revision ? " (the alternative has changed since — the selection stays on this revision)" : "")));
  }
  const ids = (v.problem.decisions || []).slice().reverse();
  if (!ids.length) b.append(el("p", "cx-empty", "No decisions yet."));
  ids.forEach((id) => {
    const d = (v.decisions || {})[id]; if (!d) return;
    const st = states[id] || {};
    const row = el("div", "cx-dec cx-dec-" + d.status + (st.stale ? " cx-dec-stale" : ""));
    row.setAttribute("data-decision", id);
    const head = el("div", "cx-ev-head");
    head.append(el("span", "cx-src-title", d.title), el("span", "cx-run-badge", d.status + (st.stale ? " · stale" : "")));
    row.append(head);
    row.append(el("div", "cx-hint", cxAsmName(d.assembly.id) + " @ " + d.assembly.revision.slice(0, 10) + " · proposed by " + d.proposedBy.principal + (d.reviewedBy ? " · reviewed by " + d.reviewedBy.principal : "")));
    row.append(el("p", "cx-claim", d.proposal));
    if (st.stale) row.append(el("p", "cx-form-msg", st.reason));
    if ((d.unresolvedIssues || []).length) {
      const ul = el("ul", "cx-plain");
      d.unresolvedIssues.forEach((is) => ul.append(el("li", null, is.severity + " · " + is.ruleKey + ": " + is.message)));
      const det = el("details", "cx-dec-issues");
      det.append(el("summary", "micro-label", d.unresolvedIssues.length + " unresolved issues kept with this decision"), ul);
      row.append(det);
    }
    if (!ro && d.status === "proposed") {
      const acts = el("div", "cx-row-edit");
      const rev = (v.revisions || {})["decision:" + id];
      const acc = pillLight("Accept for project", () => cxDecisionCommand({ op: "ApproveDecision", decisionId: id, expectedDecisionRevision: rev }));
      acc.disabled = !!st.stale;
      acc.title = st.stale ? "The alternative changed since this proposal: propose again on the current revision" : "Accepts this exact decision revision for the project (not construction approval)";
      acts.append(acc, pillLight("Reject", () => cxDecisionCommand({ op: "RejectDecision", decisionId: id, expectedDecisionRevision: rev })));
      row.append(acts);
    }
    b.append(row);
  });
  const a = cxAsm();
  if (a && !ro) {
    const f = el("details", "cx-block cx-form");
    f.append(el("summary", "cx-label micro-label", "Propose a decision for “" + a.name + "”"));
    const title = el("input", "cx-in"); title.setAttribute("aria-label", "Decision title"); title.value = "Use " + a.name;
    const prop = el("textarea", "cx-in cx-narrative"); prop.setAttribute("aria-label", "Decision proposal");
    const rat = el("textarea", "cx-in cx-narrative"); rat.setAttribute("aria-label", "Decision rationale");
    f.append(title, prop, rat, pillLight("Propose decision", () => cxDecisionCommand({ op: "ProposeDecision", decisionId: cxNewId("dec"), assemblyId: a.id,
      assemblyRevision: (cx.view.revisions || {})["assembly:" + a.id], title: title.value, proposal: prop.value || title.value, rationale: rat.value })),
      el("p", "cx-hint", "The proposal binds the current revision and keeps its unresolved issues."));
    b.append(f);
  }
  b.append(cxCompareBlock());
}

function cxCompareBlock() {
  const v = cx.view;
  const box = el("div", "cx-block cx-compare-box");
  box.append(el("div", "cx-label micro-label", "Compare exact revisions"));
  const opts = [];
  (v.problem.alternatives || []).forEach((id) => opts.push([id, cxAsmName(id) + " (current)"]));
  Object.values(v.decisions || {}).forEach((d) => opts.push([d.assembly.id + "@" + d.assembly.revision, cxAsmName(d.assembly.id) + " @ " + d.assembly.revision.slice(0, 10) + " (" + d.title + ")"]));
  const mk = (label) => { const s = selectEl([]); s.className = "cx-in"; s.setAttribute("aria-label", label); opts.forEach(([val, text]) => { const o = document.createElement("option"); o.value = val; o.textContent = text; s.append(o); }); return s; };
  const sa = mk("Compare from"), sb = mk("Compare to");
  if (opts.length > 1) sb.selectedIndex = 1;
  const out = el("div", "cx-cmp-out");
  out.setAttribute("aria-live", "polite");
  const run = async () => {
    out.innerHTML = "";
    try {
      const res = await cxApi("GET", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + "/compare?a=" + cxEnc(sa.value) + "&b=" + cxEnc(sb.value));
      const c = res.comparison;
      out.append(el("p", "cx-hint", c.aName + " @ " + c.a.revision.slice(0, 10) + " → " + c.bName + " @ " + c.b.revision.slice(0, 10) + (c.same ? " · no differences" : "")));
      const line = (cls, t) => out.append(el("div", "cx-diff-line cx-diff-" + cls, t));
      (c.junction || []).forEach((x) => line("chg", "Junction " + x.key + ": " + x.from + " → " + x.to));
      (c.parameters || []).forEach((x) => line("chg", x.key + ": " + x.from + " → " + x.to));
      (c.components || []).forEach((x) => line(x.change === "added" ? "add" : x.change === "removed" ? "del" : "chg", (x.change === "added" ? "+ " : x.change === "removed" ? "− " : "") + x.name + (x.details.length ? ": " + x.details.join("; ") : "")));
      (c.issues.onlyA || []).forEach((x) => line("add", "Resolved in " + c.bName + ": " + x.ruleKey));
      (c.issues.onlyB || []).forEach((x) => line("del", "New in " + c.bName + ": " + x.severity + " " + x.ruleKey));
      (c.evidence.onlyB || []).forEach((x) => line("info", "Evidence only in " + c.bName + ": " + x));
      if (c.geometry && c.geometry.from) line("info", "Geometry " + c.geometry.from.slice(0, 10) + " → " + c.geometry.to.slice(0, 10));
    } catch (e) { out.append(el("p", "cx-form-msg", "Couldn't compare: " + e.message)); }
  };
  box.append(sa, sb, pillLight("Compare", run), out);
  return box;
}

// History tab: the open alternative's own revisions, with who made each
// (owner / agent / system from the receipts) and a restore that is a new
// revision.
async function cxAssemblyRevisions(b) {
  const a = cxAsm();
  if (!a) return;
  const box = el("div", "cx-block cx-asm-revs");
  box.append(el("div", "cx-label micro-label", "Revisions of “" + a.name + "”"));
  b.append(box);
  try {
    const res = await cxApi("GET", cxBase(cx.subject), "/problems/" + cxEnc(cx.problemId) + "/assemblies/" + cxEnc(a.id) + "/history");
    const head = (cx.view.revisions || {})["assembly:" + a.id];
    (res.history || []).slice(0, 40).forEach((h) => {
      const r = el("div", "cx-hist-row cx-asm-rev");
      r.setAttribute("data-revision", h.revision);
      r.append(el("span", "cx-hist-gen micro-label", "r" + h.number), el("span", "cx-hist-ops", (h.operations || []).join(", ") || h.summary || "created"),
        el("span", "cx-hist-actor micro-label", h.actor.principal), el("span", "cx-hist-at micro-label", (h.createdAt || "").slice(0, 19).replace("T", " ")));
      if (h.revision !== head && !cx.view.readOnly) {
        const btn = el("button", "cx-link", "Restore");
        btn.type = "button";
        btn.setAttribute("aria-label", "Restore revision " + h.number);
        btn.onclick = () => cxCommand([{ op: "RestoreRevision", revision: h.revision }], { assembly: a.id, label: "Restore" });
        r.append(btn);
      } else r.append(el("span", "micro-label", h.revision === head ? "current" : ""));
      box.append(r);
    });
  } catch (e) { box.append(el("p", "cx-empty", "Couldn't load revisions: " + e.message)); }
}

(function () {
  const i = cxResearchTabs.findIndex(([k]) => k === "history");
  if (i >= 0) {
    const paint = cxResearchTabs[i][2];
    cxResearchTabs[i] = ["history", "History", async (b) => { await paint(b); await cxAssemblyRevisions(b); }];
  }
  const j = cxResearchTabs.findIndex(([k]) => k === "alternatives");
  cxResearchTabs.splice(j + 1, 0, ["decisions", "Decisions", (b) => cxPaintDecisionsTab(b)]);
})();
