// ================= CONSTRUCTION — the Plan pane (2026-10-09) =================
// One place to work a problem the way a non-architect would: describe it,
// let Alfred research it, look at the approaches he models, settle the
// decision points that narrow them down, then get into specifics. Steering
// happens in chat (an ordinary Alfred conversation per problem, with web
// search); what's decided lands here, in the problem's records, so nothing
// lives only in a chat thread. Alfred never edits the problem: his replies
// carry proposals the owner applies, and every applied change is a revision
// that can be undone.

["AddQuestion", "AnswerQuestion", "SetQuestionState"].forEach((o) => CX_PROBLEM_OPS.add(o));
// Answers and notes say what they are, not what they replace: when one is
// refused only because something else (often the model's own view save)
// moved the problem revision, load the latest and send it once more.
const CX_RETRY_OPS = new Set(["AddQuestion", "AnswerQuestion", "SetQuestionState", "AddFact", "SetContext"]);
{
  const base = cxCommand;
  cxCommand = async function (ops, opt = {}) {
    if (!ops.every((o) => CX_RETRY_OPS.has(o.op)) || opt.assembly) return base(ops, opt);
    const pid = cx.problemId;
    let res = await base(ops, opt);
    if (res || !/changed since you loaded it/.test(cx.error || "") || pid !== cx.problemId) return res;
    try { const fresh = await cxApi("GET", cxBase(cx.subject), "/problems/" + encodeURIComponent(pid)); cx.error = ""; cxApplyView(fresh); }
    catch (e) { return null; }
    return base(ops, opt);
  };
}
const cxp = { chat: null, chatFor: "", loading: false, sending: false, poll: 0, msg: "", draft: "", applied: {}, open: {} };

// ---- stages -------------------------------------------------------------------
function cxQuestions() { return (cx.view && cx.view.problem.questions) || []; }
function cxOpenQs(stage) { return cxQuestions().filter((q) => q.state === "open" && (!stage || q.stage === stage)); }
function cxAlfredSpoke() { return !!(cxp.chat && cxp.chat.turns.some((t) => t.who !== "user" && t.who !== "system")); }

// built-in decision points the model itself raises (no stored record): each
// says how to answer it here
function cxBuiltinQs() {
  const a = cxAsm(), out = [];
  if (!a) return out;
  const j = a.junction, ro = !!cx.view.readOnly;
  if (j.wallCondition.value === "unknown") out.push({ key: "wall", text: "Is the house wall solid brick or a cavity wall?", why: "A cavity wall needs a through-wall flashing (relaying brick); a solid wall can take a counterflashing set into a mortar joint.",
    options: [["Solid brick", () => cxCommand([{ op: "SetWallCondition", value: "solid-bonded", provenance: "user-assumption", note: "owner, from the Plan pane" }], { assembly: a.id })],
      ["Cavity wall", () => cxCommand([{ op: "SetWallCondition", value: "cavity", provenance: "user-assumption", note: "owner, from the Plan pane", newComponentIds: { cavity: cxNewId("cmp") } }], { assembly: a.id })]], ro });
  if (j.orientation === "unresolved") out.push({ key: "orient", text: "Where does the roof meet the wall — at its high edge, or along its side?", why: "It decides which flashing pieces the detail needs.", go: ["Set it", () => cxGo("assembly")], ro });
  else if (j.strategy === "unresolved") out.push({ key: "strat", text: "How should the flashing hold into the wall?", why: "Fastened to the brick face, set into a mortar joint, or built through the wall — each has its own trade-offs.", go: ["Choose", () => cxGo("assembly")], ro });
  const cl = cx.view.problem.climate;
  if (!cl || cl.state === "unknown") out.push({ key: "climate", text: "What's the climate here?", why: "Where the air/vapour seal goes depends on it.", text1: "e.g. St. Louis — hot humid summers, cold winters",
    answer: (v) => cxCommand([{ op: "SetContext", field: "climate", text: v, state: "known", provenance: "user-assumption" }]), ro });
  return out;
}

function cxStages() {
  const p = cx.view.problem, alts = (p.alternatives || []).length;
  const appQ = cxOpenQs("approach").length + cxBuiltinQs().length, specQ = cxOpenQs("specifics").length;
  const chosen = !!p.selectedAssembly;
  const ran = cxAlfredSpoke() || (cx.view.runs && Object.keys(cx.view.runs).length > 0);
  return [
    { key: "describe", title: "Describe it", done: !!(p.narrative || "").trim(), now: (p.narrative || "").trim() ? "Described" : "Say what the problem is, in your own words" },
    { key: "research", title: "Research", done: ran, now: ran ? "Alfred has looked into it" : "Ask Alfred to research it" },
    { key: "approaches", title: "Approaches", done: alts >= 2, now: alts + " approach" + (alts === 1 ? "" : "es") + " modelled" },
    { key: "decide", title: "Decide", done: chosen, now: chosen ? "Chosen: " + cxAsmName(p.selectedAssembly.id) : appQ ? appQ + " decision" + (appQ === 1 ? "" : "s") + " to make" : "Choose an approach" },
    { key: "specifics", title: "Specifics", done: chosen && specQ === 0 && cxQuestions().some((q) => q.stage === "specifics"), now: chosen ? (specQ ? specQ + " detail" + (specQ === 1 ? "" : "s") + " to settle" : "Materials, products, exact sizes") : "After choosing" },
  ];
}

// The guide under the header becomes the five stages (glossary kept).
cxGuideBlock = function () {
  const st = cxStages(), next = st.find((s) => !s.done);
  const box = el("details", "cx-guide");
  box.open = cxGuideOpen;
  box.ontoggle = () => { cxGuideOpen = box.open; try { localStorage.setItem("cx.guide.open", box.open ? "1" : "0"); } catch (e) {} };
  const sum = el("summary", "cx-guide-sum");
  const n = next ? st.indexOf(next) + 1 : st.length;
  sum.append(el("span", "cx-guide-kicker micro-label", "Step " + n + " of " + st.length), el("span", "cx-guide-next", next ? next.title + " — " + next.now.charAt(0).toLowerCase() + next.now.slice(1) : "Every stage has an answer — review the specifics."));
  box.append(sum);
  const body = el("div", "cx-guide-body");
  const ol = el("ol", "cx-guide-steps cx-stages");
  st.forEach((s) => {
    const li = el("li", "cx-guide-step" + (s.done ? " is-done" : "") + (s === next ? " is-next" : ""));
    const b = el("button", "cx-guide-step-btn");
    b.type = "button";
    b.append(el("span", "cx-guide-tick", s.done ? "✓" : ""), el("span", "cx-guide-title", s.title), el("span", "cx-guide-now", s.now));
    b.onclick = () => cxPlanGo(s.key);
    li.append(b);
    ol.append(li);
  });
  body.append(ol);
  const words = el("details", "cx-guide-words");
  words.append(el("summary", "", "Words you'll see"));
  const dl = el("dl", "");
  CX_GLOSSARY.forEach(([t, d]) => dl.append(el("dt", "", t), el("dd", "", d)));
  words.append(dl);
  body.append(words);
  box.append(body);
  return box;
};

function cxPlanGo(stage) {
  if (stage === "describe") { cx.pane = "research"; cx.tab = "problem"; cxRender(); return; }
  cx.pane = "agent";
  cxRender();
  const target = { research: ".cx-chat", approaches: ".cx-approaches", decide: ".cx-dps", specifics: ".cx-dps" }[stage];
  const n = cx.host && cx.host.querySelector(target);
  if (n) n.scrollIntoView({ block: "start", behavior: "smooth" });
  if (stage === "research") { const ta = cx.host.querySelector(".cx-chat-in"); if (ta) ta.focus(); }
}

// ---- the pane -------------------------------------------------------------------
// the pane holds a conversation now: widen an old narrow layout once
{
  const L = cxLayout();
  if (!L.planWide) { L.a = Math.max(L.a, 380); L.planWide = true; cxSaveLayout(); }
}
cxPaintAgentFull = function (body) {
  const keepDraft = body.querySelector(".cx-chat-in");
  if (keepDraft) cxp.draft = keepDraft.value;
  body.innerHTML = "";
  body.classList.add("cx-plan");
  if (cxp.chatFor !== cx.problemId) { cxp.chat = null; cxp.chatFor = cx.problemId; cxp.applied = cxAppliedLoad(); cxChatLoad(); }
  body.append(cxDecisionPoints(), cxApproaches(), cxChatBlock(), cxPlanMore());
};
// the pane is the Plan now (phone tab and pane title)
{
  const shell = cxWorkbenchShell;
  cxWorkbenchShell = function () {
    const wb = shell();
    const b = wb.querySelector('.cx-switch-btn[data-pane="agent"]');
    if (b) b.textContent = "Plan";
    const t = wb.querySelector(".cx-pane-a .cx-pane-title");
    if (t) t.textContent = "Plan";
    return wb;
  };
}

// ---- decision points --------------------------------------------------------------
function cxDecisionPoints() {
  const box = el("section", "cx-block cx-dps");
  box.append(el("h3", "cx-plan-h", "Decisions to make"));
  const ro = !!cx.view.readOnly;
  const builtin = cxBuiltinQs(), open = cxOpenQs(), p = cx.view.problem;
  const props = (p.decisions || []).map((id) => (cx.view.decisions || {})[id]).filter((d) => d && d.status === "proposed");
  if (!builtin.length && !open.length && !props.length) box.append(el("p", "cx-empty", (p.alternatives || []).length > 1 ? "Nothing open. Ask Alfred what's left to decide, or choose an approach below." : "Nothing open yet. Ask Alfred to research the problem — he'll raise the decisions that matter."));
  builtin.forEach((q) => box.append(cxDpCard(q)));
  open.filter((q) => q.stage === "approach").concat(open.filter((q) => q.stage === "specifics")).forEach((q) => box.append(cxDpCard(cxStoredQ(q, ro))));
  props.forEach((d) => box.append(cxProposedDecisionCard(d, ro)));
  const done = cxQuestions().filter((q) => q.state !== "open");
  if (done.length) {
    const det = el("details", "cx-dp-done");
    det.append(el("summary", "micro-label", done.length + " settled"));
    done.forEach((q) => {
      const r = el("div", "cx-dp-settled");
      r.append(el("span", "cx-dp-q", q.text), el("span", "cx-dp-a", q.state === "dropped" ? "dropped" : q.answer));
      if (!ro) { const c = el("button", "cx-link", "Change"); c.type = "button"; c.onclick = () => cxCommand([{ op: "SetQuestionState", id: q.id, state: "open" }], { label: "Reopen question" }); r.append(c); }
      det.append(r);
    });
    box.append(det);
  }
  if (!ro) {
    const add = el("details", "cx-dp-add");
    add.append(el("summary", "cx-link", "＋ Add a question of your own"));
    const inp = el("input", "cx-in"); inp.placeholder = "e.g. Can we keep the old gutter?"; inp.setAttribute("aria-label", "Your question");
    const go = pillLight("Add", () => { const t = inp.value.trim(); if (t) cxCommand([{ op: "AddQuestion", id: cxNewId("dq"), text: t }], { label: "Add question" }); });
    add.append(inp, go);
    box.append(add);
  }
  return box;
}

function cxStoredQ(q, ro) {
  return { key: q.id, text: q.text, why: q.why, stage: q.stage, by: q.raisedBy, ro,
    options: (q.options || []).map((o) => [o, () => cxCommand([{ op: "AnswerQuestion", id: q.id, answer: o }], { label: "Answer" })]),
    answer: (v) => cxCommand([{ op: "AnswerQuestion", id: q.id, answer: v }], { label: "Answer" }),
    drop: () => cxCommand([{ op: "SetQuestionState", id: q.id, state: "dropped" }], { label: "Drop question" }) };
}

function cxDpCard(q) {
  const c = el("div", "cx-dp");
  c.dataset.q = q.key;
  const head = el("div", "cx-dp-head");
  head.append(el("span", "cx-dp-q", q.text));
  if (q.stage === "specifics") head.append(el("span", "cx-chip", "specifics"));
  c.append(head);
  if (q.why) c.append(el("p", "cx-dp-why", q.why));
  const acts = el("div", "cx-dp-acts");
  (q.options || []).forEach(([label, fn]) => { const b = pillLight(label, fn); b.disabled = q.ro; acts.append(b); });
  if (q.go) acts.append(pillLight(q.go[0], q.go[1]));
  const ask = pillLight("Ask Alfred", () => cxChatSend("Help me decide this: " + q.text + (q.why ? " (" + q.why + ")" : "") + " Research it, lay out the options in plain words with what each one changes, and recommend one."));
  ask.classList.add("cx-ask");
  ask.disabled = q.ro;
  acts.append(ask);
  c.append(acts);
  if (q.answer) {
    const row = el("div", "cx-dp-other");
    const inp = el("input", "cx-in"); inp.placeholder = q.text1 || ((q.options || []).length ? "Or say something else…" : "Your answer"); inp.setAttribute("aria-label", "Answer: " + q.text); inp.disabled = q.ro;
    const save = pillLight("Save", () => { const v = inp.value.trim(); if (v) q.answer(v); });
    save.disabled = q.ro;
    inp.onkeydown = (e) => { if (e.key === "Enter") save.click(); };
    row.append(inp, save);
    if (q.drop && !q.ro) { const d = el("button", "cx-link", "Not relevant"); d.type = "button"; d.onclick = q.drop; row.append(d); }
    c.append(row);
  }
  return c;
}

function cxProposedDecisionCard(d, ro) {
  const c = el("div", "cx-dp cx-dp-decision");
  const st = cxDecisionStates()[d.id] || {};
  c.append(el("div", "cx-dp-q", "Go with “" + cxAsmName(d.assembly.id) + "”?"), el("p", "cx-dp-why", d.proposal + (st.stale ? " — the approach changed since this was proposed." : "")));
  const acts = el("div", "cx-dp-acts");
  const rev = (cx.view.revisions || {})["decision:" + d.id];
  const yes = pillLight("Yes, choose it", () => cxDecisionCommand({ op: "ApproveDecision", decisionId: d.id, expectedDecisionRevision: rev }));
  yes.disabled = ro || !!st.stale;
  const no = pillLight("No", () => cxDecisionCommand({ op: "RejectDecision", decisionId: d.id, expectedDecisionRevision: rev }));
  no.disabled = ro;
  acts.append(yes, no);
  c.append(acts);
  return c;
}

// ---- approaches -------------------------------------------------------------------
function cxPlainApproach(a) {
  const j = a.junction;
  if (j.strategy === "unresolved") return "Flashing method not chosen yet";
  const s = CX_PLAIN_STRAT[j.strategy];
  return s ? s.label : j.strategy;
}
function cxApproachCard(id, compact) {
  const v = cx.view, a = v.assemblies[id];
  const f = cxAltFacts(id), on = id === cx.activeAssembly, chosen = v.problem.selectedAssembly && v.problem.selectedAssembly.id === id;
  const c = el("article", "cx-approach" + (on ? " is-open" : "") + (chosen ? " is-chosen" : ""));
  c.dataset.assembly = id;
  const head = el("div", "cx-approach-head");
  head.append(el("span", "cx-approach-name", a.name));
  if (chosen) head.append(el("span", "cx-chip cx-chip-ok", "chosen"));
  else if (on) head.append(el("span", "cx-chip", "in the model"));
  c.append(head);
  c.append(el("p", "cx-approach-how", cxPlainApproach(a)));
  if (a.summary && !compact) c.append(el("p", "cx-approach-sum", a.summary));
  const s = CX_PLAIN_STRAT[a.junction.strategy];
  if (s && s.good && !compact) {
    const pc = el("ul", "cx-approach-pc");
    pc.append(el("li", "is-good", s.good), el("li", "is-watch", s.watch));
    c.append(pc);
  }
  const facts = [];
  facts.push(f.critical ? f.critical + " thing" + (f.critical === 1 ? "" : "s") + " still to settle" : "nothing critical open");
  facts.push(CX_PLAIN_WALL[a.junction.wallCondition.value] ? "wall: " + (a.junction.wallCondition.value === "unknown" ? "not known yet" : a.junction.wallCondition.value === "solid-bonded" ? "solid brick" : a.junction.wallCondition.value) : "");
  c.append(el("p", "cx-approach-facts", facts.filter(Boolean).join(" · ")));
  // the true section of this revision, drawn by the server (an inert image)
  const fig = el("button", "cx-approach-fig");
  fig.type = "button";
  fig.title = "Show this approach in the model";
  const img = document.createElement("img");
  img.loading = "lazy";
  img.alt = "Section drawing of " + a.name;
  img.src = cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(id) + "/section?revision=" + encodeURIComponent((v.revisions || {})["assembly:" + id] || "") + "&paper=A3&scale=5";
  img.onerror = () => fig.remove();
  fig.append(img);
  fig.onclick = () => cxShowApproach(id);
  c.append(fig);
  const acts = el("div", "cx-dp-acts");
  if (!on) acts.append(pillLight("Show model", () => cxShowApproach(id)));
  if (!chosen && !v.readOnly) acts.append(pillLight("Choose this one", () => cxChooseApproach(id)));
  c.append(acts);
  return c;
}
function cxShowApproach(id) {
  cx.activeAssembly = id; cx.selection = ""; cx.geometry = null;
  if (cxPhone()) cx.pane = "model";
  cxRender(); cxQueueViewSave();
}
// choosing = propose + accept a decision bound to this exact revision
async function cxChooseApproach(id) {
  const v = cx.view, a = v.assemblies[id];
  const decisionId = cxNewId("dec");
  await cxDecisionCommand({ op: "ProposeDecision", decisionId, assemblyId: id, assemblyRevision: (v.revisions || {})["assembly:" + id], title: "Use " + a.name, proposal: "Go with " + a.name + ": " + cxPlainApproach(a) + ".", rationale: "Chosen by the owner in the Plan pane." });
  const rev = (cx.view.revisions || {})["decision:" + decisionId];
  if (rev) await cxDecisionCommand({ op: "ApproveDecision", decisionId, expectedDecisionRevision: rev });
}
function cxApproaches() {
  const box = el("section", "cx-block cx-approaches");
  const ids = cx.view.problem.alternatives || [];
  box.append(el("h3", "cx-plan-h", "Approaches" + (ids.length ? " · " + ids.length : "")));
  if (cxd.msg) { const m = el("p", "cx-form-msg", cxd.msg); m.setAttribute("role", "alert"); box.append(m); }
  if (!ids.length) box.append(el("p", "cx-empty", "No approaches yet — Alfred proposes them after researching."));
  const list = el("div", "cx-approach-list");
  ids.forEach((id) => { if (cx.view.assemblies[id]) list.append(cxApproachCard(id, true)); });
  box.append(list);
  if (ids.length === 1 && !cx.view.readOnly) box.append(pillLight("Ask Alfred for other approaches", () => cxChatSend("Propose two or three other approaches to this problem, each as its own model, and say in plain words what each one is good at and what to watch.")));
  return box;
}

// Research › Alternatives becomes the same cards, with more detail and the
// side-by-side difference kept below.
cxPaintAlternatives = function (b) {
  const v = cx.view, ids = v.problem.alternatives || [];
  if (!ids.length) { b.append(el("p", "cx-empty", "No approaches yet.")); return; }
  const list = el("div", "cx-approach-list is-wide");
  ids.forEach((id) => { if (v.assemblies[id]) list.append(cxApproachCard(id, false)); });
  b.append(list);
  const others = ids.filter((id) => id !== cx.activeAssembly);
  if (others.length && cxAsm()) {
    const det = el("details", "cx-block");
    det.append(el("summary", "cx-label micro-label", "What's different from “" + cxAsm().name + "”"));
    const pick = selectEl([]); pick.className = "cx-in"; pick.setAttribute("aria-label", "Compare the open approach with");
    others.forEach((id) => { const o = document.createElement("option"); o.value = id; o.textContent = "Compare with " + v.assemblies[id].name; pick.append(o); });
    const out = el("div", "cx-diff");
    const run = () => { out.innerHTML = ""; cxDiffList(v.assemblies[pick.value], cxAsm()).forEach((line) => out.append(el("div", "cx-diff-line cx-diff-" + line.kind, line.text))); };
    pick.onchange = run;
    det.append(pick, out);
    run();
    b.append(det);
  }
};
{
  const t = cxResearchTabs.find(([k]) => k === "alternatives");
  if (t) t[1] = "Approaches";
}

// ---- the conversation -------------------------------------------------------------------
function cxChatPath() { return "/problems/" + encodeURIComponent(cx.problemId) + "/chat"; }
async function cxChatLoad() {
  if (cxp.loading || !cx.problemId) return;
  cxp.loading = true;
  const pid = cx.problemId;
  try {
    const res = await cxApi("GET", cxBase(cx.subject), cxChatPath());
    if (pid !== cx.problemId) return;
    const before = cxp.chat ? JSON.stringify(cxp.chat) : "";
    cxp.chat = res.chat;
    if (JSON.stringify(cxp.chat) !== before) cxChatRepaint();
  } catch (e) { cxp.msg = "Couldn't load the conversation: " + e.message; cxChatRepaint(); }
  finally { cxp.loading = false; }
  clearTimeout(cxp.poll);
  if (cxp.chat && cxp.chat.pending) cxp.poll = setTimeout(cxChatLoad, 2500);
}
function cxChatRepaint() {
  const old = cx.host && cx.host.querySelector(".cx-chat");
  if (!old) return;
  const ta = old.querySelector(".cx-chat-in"), focused = ta && document.activeElement === ta;
  if (ta) cxp.draft = ta.value;
  const next = cxChatBlock();
  old.replaceWith(next);
  if (focused) { const t = next.querySelector(".cx-chat-in"); t.focus(); t.selectionStart = t.selectionEnd = t.value.length; }
}
async function cxChatSend(text) {
  text = (text || "").trim();
  if (!text || cxp.sending) return;
  cxp.sending = true; cxp.msg = "";
  if (cx.pane !== "agent") { cx.pane = "agent"; cxRender(); }
  // show the message at once, honestly marked as sending
  if (cxp.chat) cxp.chat.turns.push({ n: 0, who: "user", at: "", text, sending: true });
  cxp.draft = "";
  cxChatRepaint();
  try {
    const res = await cxApi("POST", cxBase(cx.subject), cxChatPath(), { schemaVersion: 1, requestId: cxRequestId(), text });
    cxp.chat = res.chat;
  } catch (e) {
    cxp.msg = "Not sent: " + e.message;
    cxp.draft = text;
    if (cxp.chat) cxp.chat.turns = cxp.chat.turns.filter((t) => !t.sending);
  } finally { cxp.sending = false; cxChatRepaint(); }
  const n = cx.host && cx.host.querySelector(".cx-chat-thread");
  if (n) n.scrollTop = n.scrollHeight;
  clearTimeout(cxp.poll);
  cxp.poll = setTimeout(cxChatLoad, 2500);
}

function cxKickoff() {
  const p = cx.view.problem;
  return "Please research this problem properly: " + p.title + (p.narrative ? " — " + p.narrative : "") + "\n\nLook up the building code, the panel and flashing makers' installation instructions and trade guidance; tell me in plain words what you found (with sources), propose two to four approaches as separate models, and raise the decision points I need to settle to choose between them.";
}

function cxChatBlock() {
  const box = el("section", "cx-block cx-chat");
  const head = el("div", "cx-chat-head");
  head.append(el("h3", "cx-plan-h", "Talk it through with " + (cx.view.problem.steward.agent === "zeck" ? "Zeck" : "Alfred")));
  if (cxp.chat && cxp.chat.href) { const a = el("a", "cx-link", "Open in Chat"); a.href = cxp.chat.href; a.title = "The same conversation in the Chat app"; head.append(a); }
  box.append(head);
  const thread = el("div", "cx-chat-thread");
  thread.setAttribute("aria-live", "polite");
  const turns = (cxp.chat && cxp.chat.turns) || [];
  if (!cxp.chat) thread.append(el("p", "cx-empty", "Loading…"));
  else if (!turns.length) {
    thread.append(el("p", "cx-chat-intro", "Tell Alfred what's going on, or have him start with a proper research pass. He can search the web; what he proposes shows up here for you to apply."));
    const k = pillLight("Have Alfred research this problem", () => cxChatSend(cxKickoff()));
    k.classList.add("cx-ask");
    k.disabled = !!cx.view.readOnly;
    thread.append(k);
  }
  const shown = turns.slice(-12);
  if (turns.length > shown.length) thread.append(el("p", "cx-hint", (turns.length - shown.length) + " earlier messages — Open in Chat to see them."));
  shown.forEach((t) => thread.append(cxChatTurn(t)));
  if (cxp.chat && cxp.chat.pending) thread.append(el("p", "cx-chat-wait", "Alfred is working on it… (research can take a few minutes; you can leave this page)"));
  if (cxp.chat && cxp.chat.error && !cxp.chat.pending) thread.append(el("p", "cx-form-msg", "The last reply failed: " + cxp.chat.error));
  box.append(thread);
  if (cxp.msg) { const m = el("p", "cx-form-msg", cxp.msg); m.setAttribute("role", "alert"); box.append(m); }
  const form = el("div", "cx-chat-form");
  const ta = el("textarea", "cx-in cx-chat-in");
  ta.rows = 2;
  ta.placeholder = "Describe the issue, ask a question, or tell Alfred what you decided…";
  ta.setAttribute("aria-label", "Message to Alfred");
  ta.value = cxp.draft || "";
  ta.disabled = !!cx.view.readOnly;
  ta.oninput = () => { cxp.draft = ta.value; };
  ta.onkeydown = (e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); cxChatSend(ta.value); } };
  const send = pillLight(cxp.sending ? "Sending…" : "Send", () => cxChatSend(ta.value));
  send.classList.add("cx-chat-send");
  send.disabled = cxp.sending || !!cx.view.readOnly;
  form.append(ta, send);
  box.append(form);
  return box;
}

// a turn: plain text (no markup runs; http(s) links only), and any proposal
// block as a card with Apply
const CX_PROPOSAL_RE = /```construction[^\n]*\n([\s\S]*?)```/g;
function cxChatTurn(t) {
  const mine = t.who === "user";
  const row = el("div", "cx-turn " + (mine ? "is-mine" : t.who === "system" ? "is-system" : "is-agent") + (t.sending ? " is-sending" : ""));
  let text = t.text || "", i = 0;
  const props = [];
  text = text.replace(CX_PROPOSAL_RE, (m, json) => { props.push(json); return "\n"; });
  const bubble = el("div", "cx-turn-text");
  cxRichText(bubble, mine && text.length > 600 ? text.slice(0, 600) + "…" : text.trim());
  row.append(bubble);
  props.forEach((json) => row.append(cxProposalCard(json, (cxp.chat.session || "") + ":" + t.n + ":" + (i++))));
  if (t.sending) row.append(el("span", "cx-turn-meta micro-label", "sending…"));
  return row;
}
function cxRichText(host, text) {
  text.split(/\n{2,}/).forEach((para) => {
    const p = el("p", "");
    para.split("\n").forEach((line, k) => {
      if (k) p.append(document.createElement("br"));
      const bold = /^\s*#{1,4}\s+/.test(line);
      line = line.replace(/^\s*#{1,4}\s+/, "").replace(/\*\*(.+?)\*\*/g, "$1");
      const span = bold ? el("strong", "") : document.createDocumentFragment();
      let last = 0;
      line.replace(/https?:\/\/[^\s)<>\]]+/g, (u, at) => {
        span.append(document.createTextNode(line.slice(last, at)));
        const a = el("a", "", u); a.href = u; a.target = "_blank"; a.rel = "noopener noreferrer";
        span.append(a);
        last = at + u.length;
        return u;
      });
      span.append(document.createTextNode(line.slice(last)));
      p.append(span);
    });
    host.append(p);
  });
}

// ---- proposals ---------------------------------------------------------------------
function cxAppliedLoad() { try { return JSON.parse(localStorage.getItem("cx.applied." + cx.problemId) || "{}"); } catch (e) { return {}; } }
function cxAppliedSave() { try { localStorage.setItem("cx.applied." + cx.problemId, JSON.stringify(cxp.applied)); } catch (e) {} }

function cxOpPlain(op, asmId) {
  const a = asmId && cx.view.assemblies[asmId];
  const comp = (id) => { const c = a && (a.components || []).find((x) => x.id === id); return c ? c.name : "a part"; };
  switch (op.op) {
    case "AddQuestion": return "Decision to make: " + op.text + ((op.options || []).length ? " (" + op.options.join(" / ") + ")" : "");
    case "AnswerQuestion": { const q = cxQuestions().find((x) => x.id === op.id); return "Answer “" + (q ? q.text : "a question") + "”: " + op.answer; }
    case "SetQuestionState": return (op.state === "dropped" ? "Drop" : "Reopen") + " a question";
    case "AddFact": return (op.list === "existing" ? "Note about the house: " : "Note about the design: ") + op.text;
    case "SetContext": return "Set the " + op.field + ": " + op.text;
    case "SetProblemText": return op.narrative ? "Rewrite the description" : "Rename the problem: " + op.title;
    case "CreateVariant": return "New approach: " + op.name + (op.summary ? " — " + op.summary : "");
    case "SetAssemblyText": return "Describe " + (a ? "“" + a.name + "”" : "the new approach") + (op.name ? " as “" + op.name + "”" : "") + (op.summary ? ": " + op.summary : "");
    case "SetJunctionStrategy": return "Flashing method" + (a ? " for “" + a.name + "”" : "") + ": " + ((CX_PLAIN_STRAT[op.strategy] || {}).label || op.strategy);
    case "SetWallCondition": return "Wall" + (a ? " in “" + a.name + "”" : "") + ": " + (CX_PLAIN_WALL[op.value] || op.value);
    case "SetPitch": return "Roof slope: " + op.value + " " + op.unit;
    case "SetDimension": return (op.componentId ? comp(op.componentId) + " " : "") + (op.dimension || op.parameter || "size") + " → " + op.value + " " + op.unit;
    case "SetMaterial": { const m = cx.view.catalog && (cx.view.catalog.materials || []).find((x) => x.id === op.materialId); return "Material for " + comp(op.componentId) + ": " + (m ? m.name : op.materialId); }
    case "SetProduct": return "Product for " + comp(op.componentId) + ": " + op.productId;
    case "RemoveComponent": return "Remove " + comp(op.componentId);
    case "ProposeDecision": return "Propose choosing: " + op.title;
    default: return op.op;
  }
}

function cxProposalCard(json, key) {
  const card = el("div", "cx-proposal");
  let prop;
  try { prop = JSON.parse(json); } catch (e) { card.append(el("p", "cx-form-msg", "Alfred's proposal couldn't be read (not valid JSON).")); return card; }
  const changes = Array.isArray(prop.changes) ? prop.changes : [];
  card.append(el("div", "cx-proposal-kicker micro-label", "Alfred proposes"));
  if (prop.summary) card.append(el("p", "cx-proposal-sum", prop.summary));
  const ul = el("ul", "cx-proposal-ops");
  changes.forEach((ch) => (ch.operations || []).forEach((op) => ul.append(el("li", "", cxOpPlain(op, ch.assemblyId)))));
  card.append(ul);
  const done = cxp.applied[key];
  if (done) {
    card.classList.add(done.ok ? "is-applied" : "is-partial");
    card.append(el("p", "cx-proposal-state micro-label", done.ok ? "Applied · undo from History" : "Partly applied: " + done.msg));
  }
  if (!done || !done.ok) {
    const acts = el("div", "cx-dp-acts");
    const go = pillLight(done ? "Try the rest again" : "Apply", () => cxApplyProposal(changes, key, go));
    go.disabled = !!cx.view.readOnly;
    acts.append(go);
    card.append(acts);
  }
  return card;
}

// apply each change as an ordinary owner command, in order; problem-level
// operations, assembly operations and decision proposals go to their own
// endpoints. A refused change stops the rest and says why.
async function cxApplyProposal(changes, key, btn) {
  if (btn) btn.disabled = true;
  const prior = cxp.applied[key] || { done: 0 };
  let n = 0, msg = "";
  for (const ch of changes) {
    if (n++ < (prior.done || 0)) continue;
    const ops = ch.operations || [];
    const dec = ops.filter((o) => o.op === "ProposeDecision");
    const prob = ops.filter((o) => CX_PROBLEM_OPS.has(o.op));
    const asm = ops.filter((o) => o.op !== "ProposeDecision" && !CX_PROBLEM_OPS.has(o.op));
    let ok = true;
    if (prob.length) ok = !!(await cxCommand(prob, { label: "Alfred's proposal" }));
    if (ok && asm.length) {
      const target = ch.assemblyId || cx.activeAssembly;
      if (!cx.view.assemblies[target]) { ok = false; cx.error = "the proposal names an approach that doesn't exist (yet)"; }
      else ok = !!(await cxCommand(asm, { assembly: target, label: "Alfred's proposal" }));
    }
    for (const d of dec) {
      if (!ok) break;
      const v = cx.view;
      await cxDecisionCommand({ ...d, assemblyRevision: (v.revisions || {})["assembly:" + d.assemblyId] });
      ok = !cxd.msg;
      if (!ok) cx.error = cxd.msg;
    }
    if (!ok) { msg = cx.error || "refused"; n--; break; }
  }
  cxp.applied[key] = msg ? { ok: false, done: n, msg } : { ok: true, done: changes.length };
  cxAppliedSave();
  cxRender();
}

// ---- what's below the fold: history and the older research tools -------------------
function cxPlanMore() {
  const det = el("details", "cx-block cx-plan-more");
  det.open = !!cxp.open.more;
  det.ontoggle = () => { cxp.open.more = det.open; };
  det.append(el("summary", "cx-label micro-label", "Changes, steward and research runs"));
  const a = cxAsm();
  const row = el("div", "cx-row-edit");
  row.append(el("span", "cx-hint", "Every change is saved as a new version. "));
  if (a && !cx.view.readOnly) { const u = pillLight("Undo last model change", () => cxUndo(a)); u.disabled = !a.parentRevision; row.append(u); }
  row.append(pillLight("See all changes", () => cxGo("research", "history")));
  det.append(row);
  const p = cx.view.problem, ro = !!cx.view.readOnly;
  const srow = el("div", "cx-field");
  srow.append(el("span", "cx-label micro-label", "Steward"));
  const sel = selectEl(["alfred", "zeck"]);
  sel.value = p.steward.agent; sel.className = "cx-in"; sel.setAttribute("aria-label", "Steward agent"); sel.disabled = ro;
  sel.onchange = () => cxCommand([{ op: "SetSteward", agent: sel.value }]);
  srow.append(sel);
  det.append(srow, el("p", "cx-hint", p.steward.agent === "alfred" ? "Alfred is the default steward." : "Zeck was explicitly selected as the real-estate specialist."));
  det.append(el("p", "cx-hint", "Research runs read only the documents you've added (no web) and check every quote against them; the conversation above can search the web."));
  if (typeof cxPaintResearchRun === "function") cxPaintResearchRun(det);
  return det;
}
