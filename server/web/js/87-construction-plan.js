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
  const described = !!(p.narrative || "").trim() || !!(cxp.chat && cxp.chat.turns.some((t) => t.who === "user"));
  const ran = cxAlfredSpoke() || (cx.view.runs && Object.keys(cx.view.runs).length > 0);
  return [
    { key: "describe", title: "Describe it", done: described, now: described ? "Described" : "Say what the problem is, in your own words" },
    { key: "research", title: "Research", done: ran, now: ran ? "Alfred has looked into it" : "Ask Alfred to research it" },
    { key: "approaches", title: "Approaches", done: alts >= 2, now: alts + " approach" + (alts === 1 ? "" : "es") + " modelled" },
    { key: "decide", title: "Decide", done: chosen, now: chosen ? "Chosen: " + cxAsmName(p.selectedAssembly.id) : appQ ? appQ + " decision" + (appQ === 1 ? "" : "s") + " to make" : "Choose an approach" },
    { key: "specifics", title: "Specifics", done: chosen && specQ === 0 && cxQuestions().some((q) => q.stage === "specifics"), now: chosen ? (specQ ? specQ + " detail" + (specQ === 1 ? "" : "s") + " to settle" : "Materials, products, exact sizes") : "After choosing" },
  ];
}

// The header carries only the title: where things stand and what to do next
// is the first thing in the Plan.
cxGuideBlock = function () { return document.createDocumentFragment(); };

// cxNextStep: the one thing to do now, with its button
function cxNextStep() {
  const st = cxStages(), next = st.find((s) => !s.done), p = cx.view.problem;
  const pending = cxp.chat && cxp.chat.pending;
  const prop = cxLatestProposal();
  const focusChat = () => { const ta = cx.host && cx.host.querySelector(".cx-chat-in"); if (ta) { ta.scrollIntoView({ block: "center", behavior: "smooth" }); ta.focus(); } };
  const scrollTo = (sel) => () => { const n = cx.host && cx.host.querySelector(sel); if (n) n.scrollIntoView({ block: "start", behavior: "smooth" }); };
  if (pending) return { text: "Alfred is working on it. Research can take a few minutes — you can leave this page and come back." };
  if (prop && prop.partial) return { text: "Most of Alfred's suggestion is in; one part couldn't be done. Ask him to fix it, or undo.", label: "See what's left", go: scrollTo(".cx-proposal.is-partial") };
  if (prop) return { text: "Alfred suggests " + prop.count + " change" + (prop.count === 1 ? "" : "s") + ". Review them, then apply or skip.", label: "Review", go: scrollTo(".cx-proposal.is-new") };
  switch (next && next.key) {
    case "describe": return { text: "Start by telling Alfred what's going on, in your own words — what you're building and what worries you.", label: "Describe it", go: focusChat };
    case "research": return { text: "Have Alfred research this properly: the code, the makers' instructions, how trades usually do it. He'll come back with approaches and the decisions to make.", label: "Research this", go: () => cxChatSend(cxKickoff()) };
    case "approaches": return { text: "There's only one approach so far. Ask Alfred for others to compare.", label: "Get more approaches", go: () => cxChatSend("Propose two or three other approaches to this problem, each as its own model, and say in plain words what each one is good at and what to watch.") };
    case "decide": {
      const n = cxOpenQs("approach").length + cxBuiltinQs().length;
      if (n) return { text: n + " decision" + (n === 1 ? "" : "s") + " will narrow it down. Answer what you know; ask Alfred about the rest.", label: "Decide", go: scrollTo(".cx-dps") };
      return { text: "Pick the approach to go with — or ask Alfred which he'd choose and why.", label: "Ask for a recommendation", go: () => cxChatSend("Which approach would you choose for us, and why? Be plain about the trade-offs and anything we'd still need to check.") };
    }
    case "specifics": return { text: "Chosen: " + cxAsmName(p.selectedAssembly.id) + ". Now the specifics — exact materials, products and sizes.", label: "Work out the specifics", go: () => cxChatSend("We've chosen " + cxAsmName(p.selectedAssembly.id) + ". Work out the specifics: exact materials and products (with where to buy), sizes and fasteners. Raise the remaining questions as specifics decisions and propose the material and dimension changes.") };
    default: return { text: "Every stage has an answer. Ask Alfred for a materials and build list when you're ready.", label: "Make a build list", go: () => cxChatSend("Make us a materials and build-order list for the chosen approach, with quantities where you can estimate them.") };
  }
}
function cxNextCard() {
  const st = cxStages(), next = st.find((s) => !s.done), step = cxNextStep();
  const card = el("section", "cx-next");
  const dots = el("ol", "cx-progress");
  dots.setAttribute("aria-label", "Progress");
  st.forEach((s) => {
    const li = el("li", (s.done ? "is-done" : "") + (s === next ? " is-now" : ""), s.title);
    li.title = s.now;
    dots.append(li);
  });
  card.append(dots, el("p", "cx-next-text", step.text));
  if (step.label && !cx.view.readOnly) {
    const b = el("button", "cx-next-btn", step.label);
    b.type = "button";
    b.onclick = step.go;
    card.append(b);
  }
  return card;
}

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
  body.append(cxNextCard());
  const dps = cxDecisionPoints(), apps = cxApproaches();
  if (dps) body.append(dps);
  if (apps) body.append(apps);
  body.append(cxChatBlock());
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
    const asm = wb.querySelector('.cx-switch-btn[data-pane="assembly"]');
    if (asm) asm.classList.add("cx-switch-merged"); // its pane rides under the model on a phone
    const r = wb.querySelector('.cx-switch-btn[data-pane="research"]');
    if (r) r.textContent = "Details";
    const ct = wb.querySelector(".cx-pane-c .cx-pane-title"), dt = wb.querySelector(".cx-pane-d .cx-pane-title");
    if (ct) ct.textContent = "Selected part";
    if (dt) dt.textContent = "Details";
    wb.querySelector(".cx-pane-c").setAttribute("aria-label", "Selected part");
    wb.querySelector(".cx-pane-d").setAttribute("aria-label", "Details");
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
  const done = cxQuestions().filter((q) => q.state !== "open");
  if (!builtin.length && !open.length && !props.length && !done.length) return null;
  builtin.forEach((q) => box.append(cxDpCard(q)));
  open.filter((q) => q.stage === "approach").concat(open.filter((q) => q.stage === "specifics")).forEach((q) => box.append(cxDpCard(cxStoredQ(q, ro))));
  props.forEach((d) => box.append(cxProposedDecisionCard(d, ro)));
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
  // a free answer: shown at once when there are no options, else behind "Other…"
  let other = null;
  if (q.answer) {
    other = el("div", "cx-dp-other");
    const inp = el("input", "cx-in"); inp.placeholder = q.text1 || "Your answer"; inp.setAttribute("aria-label", "Answer: " + q.text); inp.disabled = q.ro;
    const save = pillLight("Save", () => { const v = inp.value.trim(); if (v) q.answer(v); });
    save.disabled = q.ro;
    inp.onkeydown = (e) => { if (e.key === "Enter") save.click(); };
    other.append(inp, save);
    if (q.drop && !q.ro) { const d = el("button", "cx-link", "Not relevant"); d.type = "button"; d.onclick = q.drop; other.append(d); }
    if ((q.options || []).length) {
      other.hidden = true;
      const o = pillLight("Other…", () => { other.hidden = false; o.remove(); inp.focus(); });
      o.disabled = q.ro;
      acts.append(o);
    }
  }
  const ask = el("button", "cx-link cx-dp-ask", "Ask Alfred");
  ask.type = "button";
  ask.disabled = q.ro;
  ask.onclick = () => cxChatSend("Help me decide this: " + q.text + (q.why ? " (" + q.why + ")" : "") + " Research it, lay out the options in plain words with what each one changes, and recommend one.");
  acts.append(ask);
  c.append(acts);
  if (other) c.append(other);
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
function cxApproachCard(id) {
  const v = cx.view, a = v.assemblies[id];
  const on = id === cx.activeAssembly, chosen = v.problem.selectedAssembly && v.problem.selectedAssembly.id === id;
  const c = el("article", "cx-approach" + (on ? " is-open" : "") + (chosen ? " is-chosen" : ""));
  c.dataset.assembly = id;
  const head = el("div", "cx-approach-head");
  head.append(el("span", "cx-approach-name", a.name));
  if (chosen) head.append(el("span", "cx-chip cx-chip-ok", "chosen"));
  else if (on) head.append(el("span", "cx-chip", "in the model"));
  c.append(head);
  c.append(el("p", "cx-approach-how", cxPlainApproach(a)));
  // the approach in the model reads in full; the others fold to a line
  const key = "asm:" + id, unfolded = key in cxp.open ? cxp.open[key] : on;
  const more = el("div", "cx-approach-more");
  more.hidden = !unfolded;
  const fold = el("button", "cx-approach-toggle", unfolded ? "Less" : "More about it");
  fold.type = "button";
  fold.setAttribute("aria-expanded", String(unfolded));
  fold.onclick = () => { cxp.open[key] = !unfolded; cxRender(); };
  head.onclick = (e) => { if (e.target === head || e.target.classList.contains("cx-approach-name")) fold.click(); };
  if (a.summary) more.append(el("p", "cx-approach-sum", a.summary));
  const s = CX_PLAIN_STRAT[a.junction.strategy];
  if (s && s.good) {
    const pc = el("ul", "cx-approach-pc");
    pc.append(el("li", "is-good", s.good), el("li", "is-watch", s.watch));
    more.append(pc);
  }
  // what's still open on this approach, in plain words
  const rep = (v.validation || {})[id];
  const open = ((rep && rep.issues) || []).filter((i) => i.severity === "critical-unresolved" && i.status !== "resolved" && i.status !== "acknowledged");
  const plain = [...new Set(open.map((i) => (typeof cxIssuePlain === "function" && cxIssuePlain(i)) || i.message))];
  if (plain.length) {
    const det = el("details", "cx-approach-open");
    det.append(el("summary", "", plain.length + " thing" + (plain.length === 1 ? "" : "s") + " to check before building"));
    const ul = el("ul", "");
    plain.forEach((t) => ul.append(el("li", "", t)));
    det.append(ul);
    more.append(det);
  } else more.append(el("p", "cx-approach-facts", "Nothing critical left open"));
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
  more.append(fig);
  c.append(fold, more);
  const acts = el("div", "cx-dp-acts");
  if (on && !v.readOnly) { const chk = pillLight("Check it against the drawings", () => cxCheckModel()); chk.classList.add("cx-ask"); acts.append(chk); }
  else acts.append(pillLight("Show model", () => cxShowApproach(id)));
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
  const v = cx.view;
  const all = (v.problem.alternatives || []).filter((id) => v.assemblies[id]);
  if (!all.length) return null;
  const live = all.filter((id) => v.assemblies[id].lifecycle !== "superseded");
  const box = el("section", "cx-block cx-approaches");
  box.append(el("h3", "cx-plan-h", "Approaches" + (live.length > 1 ? " · " + live.length : "")));
  if (cxd.msg) { const m = el("p", "cx-form-msg", cxd.msg); m.setAttribute("role", "alert"); box.append(m); }
  const list = el("div", "cx-approach-list");
  live.forEach((id) => list.append(cxApproachCard(id)));
  box.append(list);
  const gone = all.length - live.length;
  if (gone) box.append(el("p", "cx-hint", gone + " set aside (see Details › History)"));
  return box;
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
    if (JSON.stringify(cxp.chat) !== before || cxp.chat.pending) cxChatRepaint();
  } catch (e) { cxp.msg = "Couldn't load the conversation: " + e.message; cxChatRepaint(); }
  finally { cxp.loading = false; }
  clearTimeout(cxp.poll);
  if (cxp.chat && cxp.chat.pending) cxp.poll = setTimeout(cxChatLoad, 4000);
}
function cxChatRepaint() {
  const old = cx.host && cx.host.querySelector(".cx-chat");
  if (!old) return;
  const ta = old.querySelector(".cx-chat-in"), focused = ta && document.activeElement === ta;
  if (ta) cxp.draft = ta.value;
  const next = cxChatBlock();
  old.replaceWith(next);
  const card = cx.host.querySelector(".cx-next");
  if (card && cx.view) card.replaceWith(cxNextCard()); // the next step follows the conversation
  if (focused) { const t = next.querySelector(".cx-chat-in"); t.focus(); t.selectionStart = t.selectionEnd = t.value.length; }
}
async function cxChatSend(text, images) {
  text = (text || "").trim();
  if (!text || cxp.sending) return;
  cxp.sending = true; cxp.msg = "";
  if (cx.pane !== "agent") { cx.pane = "agent"; cxRender(); }
  // show the message at once, honestly marked as sending
  if (cxp.chat) cxp.chat.turns.push({ n: 0, who: "user", at: "", text: text + (images && images.length ? "\n\n(" + images.length + " pictures of the model attached)" : ""), sending: true });
  cxp.draft = "";
  cxChatRepaint();
  try {
    const body = { schemaVersion: 1, requestId: cxRequestId(), text };
    if (images && images.length) body.images = images;
    const res = await cxApi("POST", cxBase(cx.subject), cxChatPath(), body);
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
  return "Please research “" + p.title + "” properly: the building code, the makers' installation instructions and trade guidance. Tell me in plain words what you found (with sources), propose two to four approaches as separate models, and raise the decisions I need to make to choose between them.";
}

// "Alfred is researching · 3 min", with a pulse; refreshed by the poll
function cxWorking(turns) {
  const mine = [...turns].reverse().find((t) => t.who === "user" && t.at);
  const t0 = mine ? Date.parse(mine.at.replace(" ", "T")) : NaN;
  const min = isNaN(t0) ? 0 : Math.max(0, Math.floor((Date.now() - t0) / 60000));
  const w = el("div", "cx-chat-wait");
  w.setAttribute("role", "status");
  w.append(el("span", "cx-pulse"), el("span", "", "Alfred is " + (turns.length <= 2 ? "researching" : "thinking") + (min ? " · " + min + " min" : "") + ". You can leave — the answer will be here."));
  return w;
}

// what to say next, by stage: a tap sends it
function cxSuggestions() {
  const next = (cxStages().find((x) => !x.done) || {}).key;
  if (!cxp.chat || cxp.chat.pending || !cxp.chat.turns.length) return [];
  return {
    describe: [],
    research: ["Research this properly"],
    approaches: ["Check the model against the drawings", "Give me other approaches", "What are the main risks?"],
    decide: ["What should I decide first?", "Check the model against the drawings", "Which would you choose, and why?"],
    specifics: ["What exactly should I buy?", "What sizes and fasteners?", "What order do we build it in?"],
  }[next] || ["What's left to settle?", "Make a materials list"];
}
const CX_SUGGESTION_TEXT = { "Research this properly": () => cxKickoff() };
const CX_SUGGESTION_ACTION = { "Check the model against the drawings": () => cxCheckModel() };

function cxChatBlock() {
  const box = el("section", "cx-block cx-chat");
  const head = el("div", "cx-chat-head");
  const who = el("h3", "cx-plan-h", cx.view.problem.steward.agent === "zeck" ? "Zeck" : "Alfred");
  if (cxp.chat && cxp.chat.model) who.append(el("span", "cx-chat-model", cxp.chat.model + (cxp.chat.effort ? " · " + cxp.chat.effort : "")));
  head.append(who);
  if (cxp.chat && cxp.chat.href) { const a = el("a", "cx-link", "Open in Chat"); a.href = cxp.chat.href; a.title = "The same conversation, full screen, in the Chat app"; head.append(a); }
  box.append(head);
  const thread = el("div", "cx-chat-thread");
  thread.setAttribute("aria-live", "polite");
  const turns = (cxp.chat && cxp.chat.turns) || [];
  if (!cxp.chat) thread.append(el("p", "cx-empty", "Loading…"));
  else if (!turns.length) thread.append(el("p", "cx-chat-intro", "Talk to Alfred here. He can search the web; what he suggests appears as changes you apply with one tap — and can undo."));
  const keep = cxp.open.all ? turns.length : 4;
  const shown = turns.slice(-keep);
  if (turns.length > shown.length) {
    const more = el("button", "cx-link cx-chat-earlier", "Show " + (turns.length - shown.length) + " earlier");
    more.type = "button";
    more.onclick = () => { cxp.open.all = true; cxChatRepaint(); };
    thread.append(more);
  }
  shown.forEach((t) => thread.append(cxChatTurn(t)));
  if (cxp.chat && cxp.chat.pending) thread.append(cxWorking(turns));
  if (cxp.chat && cxp.chat.error && !cxp.chat.pending) thread.append(el("p", "cx-form-msg", "The last reply failed: " + cxp.chat.error));
  box.append(thread);
  if (cxp.msg) { const m = el("p", "cx-form-msg", cxp.msg); m.setAttribute("role", "alert"); box.append(m); }
  const dock = el("div", "cx-chat-dock");
  const sugg = cxSuggestions();
  if (sugg.length && !cx.view.readOnly) {
    const chips = el("div", "cx-chips");
    sugg.forEach((t) => { const b = el("button", "cx-chip-btn", t); b.type = "button"; b.onclick = () => CX_SUGGESTION_ACTION[t] ? CX_SUGGESTION_ACTION[t]() : cxChatSend(CX_SUGGESTION_TEXT[t] ? CX_SUGGESTION_TEXT[t]() : t); chips.append(b); });
    dock.append(chips);
  }
  const form = el("div", "cx-chat-form");
  const ta = el("textarea", "cx-in cx-chat-in");
  ta.rows = 1;
  ta.placeholder = turns.length ? "Message Alfred…" : "What's the problem? Say it in your own words…";
  ta.setAttribute("aria-label", "Message to Alfred");
  ta.value = cxp.draft || "";
  ta.disabled = !!cx.view.readOnly;
  const grow = () => { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, 180) + "px"; };
  ta.oninput = () => { cxp.draft = ta.value; grow(); };
  ta.onkeydown = (e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); cxChatSend(ta.value); } };
  requestAnimationFrame(grow);
  const send = pillLight(cxp.sending ? "Sending…" : "Send", () => cxChatSend(ta.value));
  send.classList.add("cx-chat-send");
  send.disabled = cxp.sending || !!cx.view.readOnly;
  form.append(ta, send);
  dock.append(form);
  box.append(dock);
  return box;
}

// a turn: plain text (no markup runs; http(s) links only), and any proposal
// block as a card with Apply
const CX_PROPOSAL_RE = /```construction[^\n]*\n([\s\S]*?)```/g;
function cxChatTurn(t) {
  const mine = t.who === "user";
  const row = el("div", "cx-turn " + (mine ? "is-mine" : t.who === "system" ? "is-system" : "is-agent") + (t.sending ? " is-sending" : ""));
  let text = t.text || "", i = 0;
  const files = [...text.matchAll(/^\[context-file:: ([a-f0-9]{32})\]$/gm)].map((m) => m[1]);
  text = text.replace(/^\[context-file:: [a-f0-9]{32}\]$/gm, "").trim();
  const props = [];
  text = text.replace(CX_PROPOSAL_RE, (m, json) => { props.push(json); return "\n"; });
  const bubble = el("div", "cx-turn-text");
  cxRichText(bubble, text.trim());
  row.append(bubble);
  if (mine && text.length > 240) {
    bubble.classList.add("is-clamped");
    bubble.title = "Show all";
    bubble.onclick = () => bubble.classList.toggle("is-clamped");
  } else if (!mine && text.length > 900) {
    const k = "turn:" + t.n;
    if (!cxp.open[k]) bubble.classList.add("is-long");
    const b = el("button", "cx-link cx-read-all", cxp.open[k] ? "Show less" : "Read all");
    b.type = "button";
    b.onclick = () => { cxp.open[k] = !cxp.open[k]; cxChatRepaint(); };
    row.append(b);
  }
  props.forEach((json) => row.append(cxProposalCard(json, (cxp.chat.session || "") + ":" + t.n + ":" + (i++))));
  if (files.length) {
    const strip = el("div", "cx-turn-files");
    files.forEach((id) => { const a = el("a", "cx-turn-file"); a.href = "/api/chat/files/" + id; a.target = "_blank"; a.rel = "noopener"; const im = document.createElement("img"); im.loading = "lazy"; im.alt = "attached picture"; im.src = "/api/chat/files/" + id; im.onerror = () => a.remove(); a.append(im); strip.append(a); });
    row.append(strip);
  }
  if (t.sending) row.append(el("span", "cx-turn-meta micro-label", "sending…"));
  return row;
}
function cxRichText(host, text) {
  const inline = (parent, line) => {
    const bold = /^\s*#{1,4}\s+/.test(line);
    line = line.replace(/^\s*#{1,4}\s+/, "").replace(/\*\*(.+?)\*\*/g, "$1");
    const span = bold ? el("strong", "") : document.createDocumentFragment();
    let last = 0;
    line.replace(/https?:\/\/[^\s)<>\]]+/g, (u, at) => {
      span.append(document.createTextNode(line.slice(last, at)));
      const a = el("a", "", u.length > 48 ? u.replace(/^https?:\/\/(www\.)?/, "").slice(0, 40) + "…" : u); a.href = u; a.target = "_blank"; a.rel = "noopener noreferrer"; a.title = u;
      span.append(a);
      last = at + u.length;
      return u;
    });
    span.append(document.createTextNode(line.slice(last)));
    parent.append(span);
  };
  text.split(/\n{2,}/).forEach((para) => {
    let p = null, ul = null;
    para.split("\n").forEach((line) => {
      const item = line.match(/^\s*(?:[-*•]|\d+[.)])\s+(.*)$/);
      if (item) {
        if (!ul) { ul = el("ul", "cx-rich-list"); host.append(ul); p = null; }
        const li = el("li", "");
        inline(li, item[1]);
        ul.append(li);
        return;
      }
      ul = null;
      if (!p) { p = el("p", ""); host.append(p); } else p.append(document.createElement("br"));
      inline(p, line);
    });
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

function cxParseProposal(json) { try { const p = JSON.parse(json); return Array.isArray(p.changes) ? cxRepairIds(p) : null; } catch (e) { return null; } }
// models miscount hex: an id with the wrong number of digits is completed
// the same way every time (so a re-read, a retry or an undo agrees), and
// every mention of it in the suggestion follows
function cxFnv(s) { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, "0"); }
function cxRepairIds(p) {
  const map = new Map();
  const fix = (v) => {
    const m = /^(dq|clm|asm|cmp|dec|evd|src)-([0-9a-fA-F]+)$/.exec(v);
    if (!m) return v;
    let h = m[2].toLowerCase();
    if (h.length === 32) return m[1] + "-" + h;
    if (!map.has(v)) { const pad = cxFnv(v) + cxFnv(v + "+") + cxFnv(v + "++") + cxFnv(v + "+++"); h = (h + pad).slice(0, 32); map.set(v, m[1] + "-" + h); }
    return map.get(v);
  };
  const walk = (x) => Array.isArray(x) ? x.map(walk) : x && typeof x === "object" ? Object.fromEntries(Object.entries(x).map(([k, v]) => [k, walk(v)])) : typeof x === "string" ? fix(x) : x;
  return walk(p);
}
// the newest proposal still waiting for the owner, if any
function cxLatestProposal() {
  const turns = (cxp.chat && cxp.chat.turns) || [];
  for (let k = turns.length - 1; k >= 0; k--) {
    const t = turns[k];
    if (t.who === "user") return null; // the owner has spoken since
    const blocks = [...(t.text || "").matchAll(CX_PROPOSAL_RE)];
    for (let i = blocks.length - 1; i >= 0; i--) {
      const key = (cxp.chat.session || "") + ":" + t.n + ":" + i, p = cxParseProposal(blocks[i][1]);
      if (p && !cxp.applied[key]) return { key, count: p.changes.reduce((n, c) => n + (c.operations || []).length, 0) };
      if (p && cxp.applied[key].msg && !cxp.applied[key].ok && !cxp.applied[key].undone) return { key, partial: true, count: 0 };
    }
    if (blocks.length) return null;
  }
  return null;
}

function cxProposalCard(json, key) {
  const card = el("div", "cx-proposal");
  card.dataset.key = key;
  const prop = cxParseProposal(json);
  if (!prop) { card.append(el("p", "cx-form-msg", "Alfred's suggestion couldn't be read. Ask him to send it again.")); return card; }
  const changes = prop.changes, st = cxp.applied[key], ro = !!cx.view.readOnly;
  const n = changes.reduce((k, c) => k + (c.operations || []).length, 0);
  card.append(el("div", "cx-proposal-kicker micro-label", st && st.ok ? "Applied" : st && st.undone ? "Undone" : st && st.skipped ? "Skipped" : st && st.msg ? "Partly applied" : "Alfred suggests " + n + " change" + (n === 1 ? "" : "s")));
  if (prop.summary) card.append(el("p", "cx-proposal-sum", prop.summary));
  const ul = cxProposalBody(changes, key);
  const acts = el("div", "cx-dp-acts");
  const btn = (label, fn) => { const b = pillLight(label, fn); b.disabled = ro; acts.append(b); return b; };
  if (!st) {
    card.classList.add("is-new");
    card.append(ul);
    const go = btn("Apply", () => cxApplyProposal(changes, key, go));
    go.classList.add("cx-primary");
    btn("Not now", () => { cxp.applied[key] = { skipped: true }; cxAppliedSave(); cxRender(); });
  } else if (st.ok) {
    card.classList.add("is-applied");
    const det = el("details", "");
    det.append(el("summary", "", "What changed"), ul);
    card.append(det);
    if (st.undo && st.undo.length) btn("Undo", () => cxUndoProposal(key));
  } else if (st.skipped || st.undone) {
    card.classList.add("is-quiet");
    const det = el("details", "");
    det.append(el("summary", "", "What it was"), ul);
    card.append(det);
    const again = btn("Apply", () => cxApplyProposal(changes, key, again));
  } else {
    card.classList.add("is-partial");
    const why = String(st.msg || "").replace(/^invalid request:\s*/i, "");
    card.append(el("p", "cx-proposal-state", "Most of it is in. One part couldn't be done: " + why));
    const det = el("details", "");
    det.append(el("summary", "", "What it was"), ul);
    card.append(det);
    const fix = btn("Ask Alfred to fix it", () => cxChatSend("Part of your suggestion couldn't be applied: \"" + why + "\". The rest is in. Please send a corrected suggestion for just the part that's left — and if the model can't show it, describe it and raise it as a question instead."));
    fix.classList.add("cx-primary");
    if (st.undo && st.undo.length) btn("Undo", () => cxUndoProposal(key));
  }
  card.append(acts);
  return card;
}

// A suggestion, grouped the way a person reads it: the decisions it adds,
// one line per approach (new or changed, with its flashing method), and
// notes cut to their first sentence with their sources as small links.
// Every line opens to its full text.
function cxSourcesOf(text) {
  const urls = [...String(text).matchAll(/https?:\/\/[^\s,;)<>\]]+/g)].map((m) => m[0].replace(/[.,]+$/, ""));
  const prose = String(text).replace(/\s*\(?https?:\/\/[^\s,;)<>\]]+\)?[.,;]?/g, "").replace(/\s*Sources?:\s*/i, " — ").replace(/[\s—,;]+$/, "").trim();
  return { urls: [...new Set(urls)], prose };
}
function cxFirstSentence(t, max = 140) {
  const m = String(t).match(/^(.{20,}?[.;:])\s/);
  let s = m ? m[1] : String(t);
  if (s.length > max) s = s.slice(0, max - 1).replace(/\s+\S*$/, "") + "…";
  return s;
}
function cxSourceChips(urls) {
  const w = el("span", "cx-src-chips");
  urls.forEach((u) => {
    let host = u;
    try { host = new URL(u).hostname.replace(/^www\./, ""); } catch (e) {}
    const a = el("a", "cx-src-chip", host); a.href = u; a.target = "_blank"; a.rel = "noopener noreferrer"; a.title = u;
    a.onclick = (e) => e.stopPropagation();
    w.append(a);
  });
  return w;
}
// one line that opens to its full text on tap
function cxLine(short, full, extra) {
  const li = el("li", "cx-pline");
  const head = el("div", "cx-pline-short", short);
  li.append(head);
  if (extra) li.append(extra);
  if (full && full !== short) {
    const body = el("div", "cx-pline-full");
    body.hidden = true;
    cxRichText(body, full);
    li.append(body);
    li.classList.add("is-openable");
    li.onclick = () => { body.hidden = !body.hidden; li.classList.toggle("is-open", !body.hidden); };
  }
  return li;
}
function cxProposalBody(changes, key) {
  const v = cx.view, wrap = el("div", "cx-proposal-body");
  const qs = [], notes = [], other = [], asms = new Map();
  const asmEntry = (id) => { if (!asms.has(id)) asms.set(id, { id, isNew: false, name: (v.assemblies[id] || {}).name || "", summary: "", bits: [], renamed: false }); return asms.get(id); };
  changes.forEach((ch) => (ch.operations || []).forEach((op) => {
    switch (op.op) {
      case "AddQuestion": qs.push(op); break;
      case "AddFact": notes.push(op); break;
      case "CreateVariant": { const e = asmEntry(op.newAssemblyId); e.isNew = true; e.name = op.name || e.name; e.summary = op.summary || e.summary; break; }
      case "SetAssemblyText": { const e = asmEntry(ch.assemblyId || cx.activeAssembly); if (op.name && op.name !== e.name) { e.renamed = !e.isNew; e.from = e.name; e.name = op.name; } if (op.summary) e.summary = op.summary; break; }
      case "SetJunctionStrategy": { const e = asmEntry(ch.assemblyId || cx.activeAssembly); e.strategy = op.strategy; e.bits.push("Flashing: " + ((CX_PLAIN_STRAT[op.strategy] || {}).label || op.strategy)); break; }
      case "SetWallCondition": { const e = asmEntry(ch.assemblyId || cx.activeAssembly); e.wall = op.value; e.bits.push("Wall: " + (({ "solid-bonded": "solid brick", cavity: "cavity wall" })[op.value] || op.value)); break; }
      default: other.push(cxOpPlain(op, ch.assemblyId));
    }
  }));
  const group = (title, n, items) => {
    if (!n) return;
    const g = el("section", "cx-pgroup");
    g.append(el("h4", "cx-pgroup-h", title + " · " + n));
    const ul = el("ul", "cx-pgroup-list");
    const all = !!cxp.open["prop:" + key + ":" + title] || items.length <= 5;
    (all ? items : items.slice(0, 4)).forEach((li) => ul.append(li));
    if (!all) {
      const b = el("button", "cx-link", "Show all " + items.length);
      b.type = "button";
      b.onclick = () => { cxp.open["prop:" + key + ":" + title] = true; cxChatRepaint(); };
      const li = el("li", "cx-pline-more"); li.append(b); ul.append(li);
    }
    g.append(ul);
    wrap.append(g);
  };
  group("Decisions to make", qs.length, qs.map((q) => cxLine(q.text, [q.why, (q.options || []).length ? "Options: " + q.options.join(" / ") : ""].filter(Boolean).join("\n\n"))));
  group("Approaches", asms.size, [...asms.values()].map((e) => {
    const tag = e.isNew ? "New" : e.renamed ? "Renamed" : "Updated";
    const extra = el("div", "cx-pline-meta");
    extra.append(el("span", "cx-chip" + (e.isNew ? " cx-chip-ok" : ""), tag));
    e.bits.forEach((b) => extra.append(el("span", "cx-pline-bit", b)));
    // say up front what the model will refuse
    const src = v.assemblies[e.id] || v.assemblies[cx.activeAssembly];
    const wall = e.wall || (src && src.junction.wallCondition.value);
    if (e.strategy === "apron-through-wall-flashing" && wall !== "cavity") extra.append(el("span", "cx-pline-warn", "The model can't show through-wall flashing in a solid wall — this part won't apply"));
    return cxLine(e.name, [e.renamed && e.from ? "Was: " + e.from : "", e.summary].filter(Boolean).join("\n\n"), extra);
  }));
  group("Notes", notes.length, notes.map((f) => {
    const { urls, prose } = cxSourcesOf(f.text);
    const extra = el("div", "cx-pline-meta");
    extra.append(el("span", "cx-pline-bit", f.list === "existing" ? "about the house" : "about the plan"));
    if (urls.length) extra.append(cxSourceChips(urls));
    return cxLine(cxFirstSentence(prose), prose, extra);
  }));
  group("Other changes", other.length, other.map((t) => cxLine(cxFirstSentence(t), t)));
  return wrap;
}

// the inverse of one operation, read from the state before it ran
function cxInverse(op, asmId) {
  const p = cx.view.problem;
  switch (op.op) {
    case "AddQuestion": return { ops: [{ op: "SetQuestionState", id: op.id, state: "dropped" }] };
    case "AnswerQuestion": case "SetQuestionState": {
      const q = cxQuestions().find((x) => x.id === op.id);
      if (!q) return null;
      return { ops: [q.state === "answered" ? { op: "AnswerQuestion", id: q.id, answer: q.answer } : { op: "SetQuestionState", id: q.id, state: q.state === "dropped" ? "dropped" : "open" }] };
    }
    case "AddFact": return { ops: [{ op: "RemoveFact", id: op.id }] };
    case "SetContext": { const f = p[op.field] || {}; return { ops: [{ op: "SetContext", field: op.field, text: f.text || "", state: f.state || "unknown", provenance: f.provenance || "unknown" }] }; }
    case "SetProblemText": return { ops: [{ op: "SetProblemText", title: p.title, narrative: p.narrative }] };
    case "CreateVariant": return { assembly: op.newAssemblyId, ops: [{ op: "SetAssemblyLifecycle", lifecycle: "superseded" }] };
    case "ProposeDecision": return { decision: op.decisionId };
    default: return null;
  }
}

// apply each change as an ordinary owner command, in order; problem-level
// operations, assembly operations and decision proposals go to their own
// endpoints. A refused change stops the rest and says why. The inverse of
// what ran is kept so the whole suggestion can be undone in one tap.
async function cxApplyProposal(changes, key, btn) {
  if (btn) btn.disabled = true;
  const prior = cxp.applied[key] && cxp.applied[key].done ? cxp.applied[key] : { done: 0, undo: [] };
  const undo = prior.undo || [];
  let n = 0, msg = "";
  for (const ch of changes) {
    if (n++ < (prior.done || 0)) continue;
    const ops = cxReapplicable(ch.operations || []);
    const dec = ops.filter((o) => o.op === "ProposeDecision");
    const prob = ops.filter((o) => CX_PROBLEM_OPS.has(o.op));
    const asm = ops.filter((o) => o.op !== "ProposeDecision" && !CX_PROBLEM_OPS.has(o.op));
    let ok = true;
    if (prob.length) {
      const inv = prob.map((o) => cxInverse(o)).filter(Boolean).reverse();
      ok = !!(await cxCommand(prob, { label: "Alfred's suggestion" }));
      if (ok) inv.forEach((x) => undo.push(x));
    }
    if (ok && asm.length) {
      const target = ch.assemblyId || cx.activeAssembly;
      if (!cx.view.assemblies[target]) { ok = false; cx.error = "it names an approach that doesn't exist (yet)"; }
      else {
        const before = (cx.view.revisions || {})["assembly:" + target];
        const variants = asm.filter((o) => o.op === "CreateVariant").map((o) => cxInverse(o));
        ok = !!(await cxCommand(asm, { assembly: target, label: "Alfred's suggestion" }));
        // a strategy that adds a part names the slot it needs an id for:
        // supply one and send again (a few times at most)
        for (let k = 0; !ok && k < 5; k++) {
          const m = /newComponentIds\.([\w-]+)/.exec(cx.error || "");
          if (!m) break;
          const op = asm.find((o) => (o.op === "SetJunctionStrategy" || o.op === "SetWallCondition") && !(o.newComponentIds || {})[m[1]]);
          if (!op) break;
          op.newComponentIds = { ...(op.newComponentIds || {}), [m[1]]: cxNewId("cmp") };
          cx.error = "";
          ok = !!(await cxCommand(asm, { assembly: target, label: "Alfred's suggestion" }));
        }
        if (ok) {
          if (asm.some((o) => o.op !== "CreateVariant") && before) undo.push({ assembly: target, ops: [{ op: "RestoreRevision", revision: before }] });
          variants.forEach((x) => undo.push(x));
        }
      }
    }
    for (const d of dec) {
      if (!ok) break;
      await cxDecisionCommand({ ...d, assemblyRevision: (cx.view.revisions || {})["assembly:" + d.assemblyId] });
      ok = !cxd.msg;
      if (ok) undo.push({ decision: d.decisionId }); else cx.error = cxd.msg;
    }
    if (!ok) { msg = cx.error || "refused"; n--; break; }
  }
  cxp.applied[key] = msg ? { ok: false, done: n, msg, undo } : { ok: true, done: changes.length, undo };
  cxAppliedSave();
  cxRender();
}

// after an undo the records are set aside, not deleted: applying again
// brings them back instead of colliding with their ids
function cxReapplicable(ops) {
  const v = cx.view, p = v.problem;
  const facts = new Set([...(p.existing || []), ...(p.proposed || [])].map((f) => f.id));
  return ops.flatMap((o) => {
    if (o.op === "AddQuestion") { const q = cxQuestions().find((x) => x.id === o.id); if (q) return q.state === "dropped" ? [{ op: "SetQuestionState", id: o.id, state: "open" }] : []; }
    if (o.op === "AddFact" && facts.has(o.id)) return [];
    return [o];
  });
}

// undo runs the inverses newest first, each as its own revision
async function cxUndoProposal(key) {
  const st = cxp.applied[key];
  if (!st || !st.undo) return;
  const steps = st.undo.slice().reverse();
  for (const u of steps) {
    let ok = true;
    if (u.decision) {
      const rev = (cx.view.revisions || {})["decision:" + u.decision];
      const d = (cx.view.decisions || {})[u.decision];
      if (rev && d && d.status === "proposed") { await cxDecisionCommand({ op: "RejectDecision", decisionId: u.decision, expectedDecisionRevision: rev }); ok = !cxd.msg; }
    } else ok = !!(await cxCommand(u.ops, { assembly: u.assembly, label: "Undo" }));
    if (!ok) { st.msg = "undo stopped: " + (cx.error || cxd.msg || "refused"); cxAppliedSave(); cxRender(); return; }
  }
  cxp.applied[key] = { undone: true };
  cxAppliedSave();
  cxRender();
}

// ---- Details: About · Sources · Materials · History · Export ----------------------
// Approaches and decisions live in the Plan; open issues ride on each
// approach; research runs (your imported documents, checked quote by quote)
// sit under Sources. Nothing here is needed for the main flow.
{
  const byKey = Object.fromEntries(cxResearchTabs.map((t) => [t[0], t[2]]));
  const about = (b) => {
    byKey.problem(b);
    const words = el("details", "cx-block cx-guide-words");
    words.append(el("summary", "cx-label micro-label", "Words you'll see"));
    const dl = el("dl", "");
    CX_GLOSSARY.forEach(([t, d]) => dl.append(el("dt", "", t), el("dd", "", d)));
    words.append(dl);
    b.append(words);
  };
  const sources = async (b) => {
    b.append(el("p", "cx-hint", "Alfred's research in the Plan searches the web. Here you can add your own documents (drawings, makers' instructions) and run a check that reads only them, quote by quote."));
    byKey.evidence(b);
    const runs = el("details", "cx-block cx-runs");
    runs.append(el("summary", "cx-label micro-label", "Check against your documents"));
    if (typeof cxPaintResearchRun === "function") { cxPaintResearchRun(runs); runs.querySelectorAll(".cx-steward").forEach((n) => n.remove()); }
    runs.open = !!cxp.open.runs;
    runs.ontoggle = () => { cxp.open.runs = runs.open; };
    b.append(runs);
    if (cxLatestRun()) { const d = el("div", ""); runs.append(d); await byKey.research(d); }
  };
  // History is the record: decisions (with staleness and compare), then every change
  const history = async (b) => {
    const dec = el("div", "cx-history-decisions");
    if (byKey.decisions) byKey.decisions(dec);
    b.append(dec);
    const ch = el("div", "");
    ch.append(el("h3", "cx-plan-h", "Changes"));
    b.append(ch);
    await byKey.history(ch);
  };
  const list = [["problem", "About", about], ["evidence", "Sources", sources], ["catalog", "Materials", byKey.catalog], ["history", "History", history], ["export", "Export", byKey.export]];
  cxResearchTabs.length = 0;
  list.forEach((t) => { if (t[2]) cxResearchTabs.push(t); });
}
// panes named for what they hold; on a phone the part inspector rides
// under the model, so "assembly" means the Model tab there
{
  const go = cxGo;
  cxGo = function (pane, tab) { go(pane === "assembly" && cxPhone() ? "model" : pane, tab); };
}

// ---- About, for people: read first, edit on tap -----------------------------------
const CX_SURE = [
  ["checked", "Checked / documented", "known", "verified-fact"],
  ["believe", "We believe it", "assumed", "user-assumption"],
  ["guide", "From a code or guide", "known", "directly-applicable-guidance"],
  ["reasoned", "Reasoned out", "assumed", "engineering-inference"],
  ["unknown", "Not known", "unknown", "unknown"],
];
function cxSureOf(state, prov) {
  if (state === "unknown") return "unknown";
  const hit = CX_SURE.find((x) => x[3] === prov);
  return hit ? hit[0] : "believe";
}
function cxSureSelect(value, label) {
  const s = document.createElement("select");
  s.className = "cx-in"; s.setAttribute("aria-label", label);
  CX_SURE.forEach(([k, t]) => { const o = document.createElement("option"); o.value = k; o.textContent = t; s.append(o); });
  s.value = value;
  return s;
}
function cxSureChip(state, prov) {
  const k = cxSureOf(state, prov), t = CX_SURE.find((x) => x[0] === k)[1];
  return el("span", "cx-sure cx-sure-" + k, t.toLowerCase().replace(" / documented", ""));
}
// a block that reads as text and turns into its editor on "Edit"
function cxEditable(title, readView, editView) {
  const box = el("section", "cx-block cx-about");
  const head = el("div", "cx-about-head");
  head.append(el("h3", "cx-plan-h", title));
  box.append(head);
  if (!editView || cx.view.readOnly) { box.append(readView); return box; }
  const key = "edit:" + title, editing = !!cxp.open[key];
  const t = el("button", "cx-link", editing ? "Done" : "Edit");
  t.type = "button";
  t.onclick = () => { cxp.open[key] = !editing; cxPaintResearch(box.closest(".cx-pane-body")); };
  head.append(t);
  box.append(editing ? editView() : readView);
  return box;
}

cxPaintProblemTab = function (b) {
  const v = cx.view, p = v.problem, ro = !!v.readOnly, ctx = v.context || {};
  // what this is
  const nar = el("div", "cx-about-text");
  if ((p.narrative || "").trim()) cxRichText(nar, p.narrative.trim()); else nar.append(el("p", "cx-empty", "Not described yet — tell Alfred in the Plan, or Edit."));
  const sc = ctx.scope;
  if (sc) nar.append(el("p", "cx-about-meta", "Part of: " + (sc.text || sc.taskId || sc.workId) + (sc.status !== "resolved" ? " (link needs checking)" : "")));
  b.append(cxEditable("What this is", nar, () => {
    const w = el("div", "");
    const ta = el("textarea", "cx-in cx-narrative");
    ta.value = p.narrative || ""; ta.setAttribute("aria-label", "Narrative"); ta.rows = 8;
    w.append(ta, pillLight("Save narrative", async () => { if (await cxCommand([{ op: "SetProblemText", narrative: ta.value }])) { cxp.open["edit:What this is"] = false; cxRender(); } }));
    return w;
  }));
  // where: location, rules, climate
  const where = el("dl", "cx-about-dl");
  const FIELDS = [["location", "Where"], ["jurisdiction", "Rules that apply"], ["climate", "Climate"]];
  FIELDS.forEach(([f, label]) => {
    const cur = p[f] || { text: "", state: "unknown", provenance: "unknown" };
    const dd = el("dd", "");
    dd.append(el("span", "", cur.text || "—"));
    if (cxSureOf(cur.state, cur.provenance) !== "believe") dd.append(cxSureChip(cur.state, cur.provenance));
    where.append(el("dt", "", label), dd);
  });
  b.append(cxEditable("Where", where, () => {
    const w = el("div", "cx-about-edit"), rows = [];
    FIELDS.forEach(([f, label]) => {
      const cur = p[f] || { text: "", state: "unknown", provenance: "unknown" };
      const row = el("div", "cx-about-row");
      const tin = inputEl(label); tin.value = cur.text || ""; tin.className = "cx-in"; tin.setAttribute("aria-label", f);
      const sure = cxSureSelect(cxSureOf(cur.state, cur.provenance), f + " — how sure");
      row.append(el("label", "cx-about-label", label), tin, sure);
      rows.push({ f, cur, tin, sure });
      w.append(row);
    });
    // one save for what changed
    w.append(pillLight("Save", async () => {
      const ops = rows.map(({ f, cur, tin, sure }) => {
        const s = CX_SURE.find((x) => x[0] === sure.value);
        if (tin.value === (cur.text || "") && s[0] === cxSureOf(cur.state, cur.provenance)) return null;
        return { op: "SetContext", field: f, text: tin.value, state: s[2], provenance: s[3] };
      }).filter(Boolean);
      if (!ops.length || await cxCommand(ops)) { cxp.open["edit:Where"] = false; cxRender(); }
    }));
    return w;
  }));
  // what we know about the house, and what we plan
  for (const [list, title, ph] of [["existing", "The house today", "Add something about the house…"], ["proposed", "What we plan", "Add something about the plan…"]]) {
    const facts = p[list] || [];
    const rows = (editing) => {
      const w = el("div", "cx-fact-list");
      if (!facts.length) w.append(el("p", "cx-empty", "Nothing yet."));
      facts.forEach((f) => {
        const row = el("div", "cx-fact");
        const t = el("span", "cx-fact-text", f.text);
        if (cxSureOf("known", f.provenance) !== "believe") t.append(" ", cxSureChip("known", f.provenance));
        row.append(t);
        if (editing) { const x = el("button", "cx-x", "✕"); x.type = "button"; x.title = "Remove"; x.setAttribute("aria-label", "Remove fact"); x.onclick = () => cxCommand([{ op: "RemoveFact", id: f.id }]); row.append(x); }
        w.append(row);
      });
      if (!ro) {
        const add = el("div", "cx-fact-add");
        const tin = inputEl(ph); tin.className = "cx-in"; tin.setAttribute("aria-label", "New " + list + " condition");
        const sure = cxSureSelect("believe", "How sure");
        sure.hidden = true;
        const go = pillLight("Add", () => { const v = tin.value.trim(); if (!v) return; const s2 = CX_SURE.find((x) => x[0] === sure.value); cxCommand([{ op: "AddFact", id: cxNewId("clm"), list, text: v, provenance: s2[3] }]); });
        tin.oninput = () => { sure.hidden = !tin.value.trim(); };
        tin.onkeydown = (e) => { if (e.key === "Enter") go.click(); };
        add.append(tin, sure, go);
        w.append(add);
      }
      return w;
    };
    b.append(cxEditable(title, rows(false), facts.length ? () => rows(true) : null));
  }
  b.append(cxInputsBlock());
};

// photos and drawings: plain meta, one button to add
const CX_ROLE_PLAIN = { photo: "Photo", drawing: "Drawing", document: "Document", other: "File" };
const CX_VERIFY_PLAIN = { "not-field-verified": "not checked on site", "owner-confirmed": "you confirmed it", unknown: "" };
{
  const base = cxInputsBlock;
  cxInputsBlock = function () {
    const blk = base();
    blk.classList.add("cx-about");
    const h = blk.querySelector(":scope > .cx-label");
    if (h) h.replaceWith(el("h3", "cx-plan-h", "Photos & drawings"));
    const inputs = cx.view.problem.inputs || [];
    blk.querySelectorAll(".cx-input").forEach((row, i) => {
      const inp = inputs[i], meta = row.querySelector(".cx-input-meta");
      if (!inp || !meta) return;
      const mb = inp.size >= 1e6 ? (inp.size / 1048576).toFixed(1) + " MB" : Math.max(1, Math.round(inp.size / 1024)) + " KB";
      meta.className = "cx-input-meta";
      meta.textContent = [CX_ROLE_PLAIN[inp.role] || inp.role, mb, CX_VERIFY_PLAIN[inp.verification]].filter(Boolean).join(" · ");
      meta.title = "revision " + inp.revision.slice(0, 12);
    });
    const empty = blk.querySelector(":scope > .cx-empty");
    if (empty) empty.textContent = "None yet. Photos of the spot and the drawings help Alfred most.";
    const form = blk.querySelector(".cx-upload");
    if (form) {
      const file = form.querySelector('input[type="file"]');
      const pick = el("label", "pill light cx-upload-pick", "＋ Add a photo or drawing");
      pick.append(file);
      file.classList.add("cx-file-hidden");
      form.prepend(pick);
      const rest = [...form.children].filter((n) => n !== pick && !n.classList.contains("cx-form-msg"));
      rest.forEach((n) => { n.hidden = true; });
      file.addEventListener("change", () => {
        rest.forEach((n) => { n.hidden = false; });
        const f = file.files && file.files[0];
        if (f) { pick.firstChild.textContent = f.name + " — "; const role = form.querySelector('[aria-label="Input role"]'); if (role) role.value = /^image\//.test(f.type) ? "photo" : "drawing"; }
      });
      const label = form.querySelector('[aria-label="Input label"]');
      if (label) label.placeholder = "What it shows (optional)";
    }
    return blk;
  };
}

// ---- the model view: open assembled; say when it isn't ---------------------------
// Explode is for a moment's look at the layers. Restored from a saved view it
// made a sound detail look broken (parts floating off the wall) with the
// control folded away, so a problem always opens assembled, and any state
// that changes what you see — pulled apart, cut open, parts hidden — shows
// as a chip on the model with its one-tap way back.
{
  const base = cxRestoreView;
  cxRestoreView = function () { base(); if (cx.vs) cx.vs.exploded = 0; };
}
{
  const base = cxSyncToolbar;
  cxSyncToolbar = function (wrap) { base(wrap); cxStateChips(wrap); };
}
function cxStateChips(wrap) {
  const host = wrap && wrap.querySelector(".cx-canvas-host");
  if (!host || !cx.vs) return;
  let bar = host.querySelector(".cx-state-chips");
  if (!bar) { bar = el("div", "cx-state-chips"); host.append(bar); }
  bar.replaceChildren();
  const chip = (text, label, fn) => { const b = el("button", "cx-state-chip"); b.type = "button"; b.append(el("span", "", text), el("strong", "", label)); b.onclick = fn; bar.append(b); };
  if (cx.vs.exploded > 0) chip("Pulled apart", "Put back", () => { cx.vs.exploded = 0; const r = wrap.querySelector('[data-role="explode"]'); if (r) r.value = "0"; cxApplyVS(); });
  if (cx.vs.section && cx.vs.section.enabled) chip("Cut open", "Close", () => { cx.vs.section = { ...cx.vs.section, enabled: false }; cxApplyVS(); });
  if ((cx.vs.hidden || []).length || (cx.vs.isolated || []).length) chip("Some parts hidden", "Show all", () => { cx.vs.hidden = []; cx.vs.isolated = []; cxApplyVS(); });
  bar.hidden = !bar.childElementCount;
}

// ---- workspace settings: the model Alfred uses here ------------------------------
async function cxSettingsBlock(subject) {
  const box = el("section", "cx-block cx-settings");
  box.append(el("h3", "cx-plan-h", "Workspace settings"));
  let d;
  try { d = await cxApi("GET", cxBase(subject), "/settings"); } catch (e) { box.append(el("p", "cx-empty", "Settings unavailable: " + e.message)); return box; }
  const cur = d.settings || {};
  const row = el("div", "cx-settings-row");
  const lab = el("label", "cx-about-label", "Alfred's model for construction");
  const sel = document.createElement("select");
  sel.className = "cx-in"; sel.setAttribute("aria-label", "Model");
  const groups = new Map();
  (d.models || []).forEach((m) => {
    const g = m.description || m.provider || "Other";
    if (!groups.has(g)) { const og = document.createElement("optgroup"); og.label = g; groups.set(g, og); sel.append(og); }
    const o = document.createElement("option"); o.value = m.provider + "|" + m.id; o.textContent = m.label || m.id; groups.get(g).append(o);
  });
  if (!cur.model) { const o = document.createElement("option"); o.value = ""; o.textContent = "Alfred's profile default"; sel.prepend(o); }
  sel.value = cur.model ? cur.provider + "|" + cur.model : "";
  const eff = document.createElement("select");
  eff.className = "cx-in"; eff.setAttribute("aria-label", "Reasoning effort");
  (d.efforts || []).forEach((e) => { const o = document.createElement("option"); o.value = e.id; o.textContent = e.id; eff.append(o); });
  eff.value = cur.effort || "high";
  const msg = el("span", "cx-hint", d.saved ? "" : cur.model ? "Default — not saved yet" : "");
  const save = pillLight("Save", async () => {
    const [provider, model] = sel.value.split("|");
    if (!model) return;
    save.disabled = true;
    try { await cxApi("PUT", cxBase(subject), "/settings", { model, provider, effort: eff.value }); msg.textContent = "Saved — Alfred uses " + model + " · " + eff.value + " from his next message"; }
    catch (e) { msg.textContent = "Not saved: " + e.message; }
    finally { save.disabled = false; }
  });
  row.append(lab, sel, eff, save);
  box.append(row, msg, el("p", "cx-hint", "Used for research, approaches and decision points in every problem's conversation. The 3D models themselves are built exactly from each approach's parts and measurements — the AI chooses those, it doesn't draw."));
  return box;
}
{
  const base = cxListInto;
  cxListInto = async function (host, subject) {
    await base(host, subject);
    const page = host.querySelector(".cx-list-page");
    if (!page || !page.querySelector(".cx-list .cx-problem-row, .cx-list a, .cx-create")) return;
    const blk = await cxSettingsBlock(subject);
    const form = page.querySelector(".cx-create, form");
    if (form) form.before(blk); else page.append(blk);
  };
}

// ---- "Check this model": pictures of the model for Alfred -------------------------
// The model's own views, labelled the way you see them, plus the true section
// drawing, sent with one message asking Alfred (on the workspace model) to
// compare them with the drawing sheets and the code. What he finds comes
// back as an ordinary suggestion to apply or skip.
const cxWait = (ms) => new Promise((r) => setTimeout(r, ms));
function cxLabelledFrame(title) {
  const src = cx.renderer.canvasEl(), host = src.parentElement;
  const W = src.width, H = src.height, k = W / Math.max(1, src.clientWidth);
  const c = document.createElement("canvas");
  c.width = W; c.height = H;
  const g = c.getContext("2d");
  g.fillStyle = "#f3f2ee"; g.fillRect(0, 0, W, H);
  g.drawImage(src, 0, 0);
  // the same callouts the page shows: a dot on the part, a line, the name in a column
  const a = cxAsm(), names = new Map();
  for (const [role, name] of CX_PART_LABELS) { const comp = (a.components || []).find((x) => (x.role || "").startsWith(role)); if (comp) names.set(comp.id, name); }
  const anchors = cx.renderer.labelAnchors([...names.keys()]);
  const shown = [];
  for (const [id, name] of names) { const wp = anchors[id], p = wp && cx.renderer.toScreen(wp); if (p) shown.push({ name, x: p.x * k, y: p.y * k, side: p.x * k < W / 2 ? "l" : "r" }); }
  const fs = Math.round(13 * k), gap = fs * 2;
  g.font = "600 " + fs + "px system-ui, sans-serif"; g.textBaseline = "middle";
  for (const side of ["l", "r"]) {
    let next = fs * 3;
    shown.filter((s) => s.side === side).sort((m, n) => m.y - n.y).forEach((s) => { s.ty = Math.max(s.y, next); next = s.ty + gap; });
  }
  shown.forEach((s) => {
    const tw = g.measureText(s.name).width + fs, th = fs * 1.6;
    const tx = s.side === "l" ? 8 * k : W - 8 * k - tw;
    const ex = s.side === "l" ? tx + tw : tx;
    g.strokeStyle = "#111"; g.lineWidth = 1.5 * k;
    g.beginPath(); g.moveTo(ex, s.ty); g.lineTo(s.x, s.y); g.stroke();
    g.fillStyle = "#fff"; g.beginPath(); g.arc(s.x, s.y, 3.5 * k, 0, Math.PI * 2); g.fill(); g.stroke();
    g.fillStyle = "rgba(15,23,32,0.9)"; g.fillRect(tx, s.ty - th / 2, tw, th);
    g.fillStyle = "#fff"; g.fillText(s.name, tx + fs / 2, s.ty);
  });
  g.fillStyle = "#111"; g.font = "700 " + Math.round(15 * k) + "px system-ui, sans-serif"; g.textBaseline = "top";
  g.fillText(title, 10 * k, 8 * k);
  return c.toDataURL("image/jpeg", 0.88).split(",")[1]; // shaded views: a fraction of the PNG's size on a phone connection
}
async function cxSectionPNG(a) {
  const img = new Image();
  img.src = cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/section?revision=" + encodeURIComponent((cx.view.revisions || {})["assembly:" + a.id] || "") + "&paper=A3&scale=5";
  await new Promise((ok, no) => { img.onload = ok; img.onerror = no; });
  const w = 2400, h = Math.round(w * (img.naturalHeight || 1) / (img.naturalWidth || 1));
  const c = document.createElement("canvas");
  c.width = w; c.height = h;
  const g = c.getContext("2d");
  g.fillStyle = "#fff"; g.fillRect(0, 0, w, h);
  g.drawImage(img, 0, 0, w, h);
  return c.toDataURL("image/png").split(",")[1];
}
async function cxCaptureModel() {
  const a = cxAsm();
  if (!a || !cx.renderer) throw new Error("open the approach in the model first");
  const wb = cx.host.querySelector(".cx-wb"), pane = cx.pane;
  if (cxPhone() && pane !== "model") { cx.pane = "model"; cxPaintSwitch(wb); await cxWait(600); }
  // a fixed, generous stage for the pictures, whatever the pane's size
  const host = cx.host.querySelector(".cx-canvas-host");
  host.classList.add("cx-capturing");
  await cxWait(400);
  const cam = cx.renderer.getCamera(), vs = { exploded: cx.vs.exploded, section: cx.vs.section, hidden: cx.vs.hidden, isolated: cx.vs.isolated };
  cx.vs.exploded = 0; cx.vs.section = null; cx.vs.hidden = []; cx.vs.isolated = [];
  cx.renderer.setState({ exploded: 0, section: null, hidden: [], isolated: [] });
  const out = [];
  try {
    const shots = [
      ["overall", "Overall", () => cx.renderer.view("iso")],
      ["junction", "Where the roof meets the wall", () => { const b = cx.host.querySelector('[aria-label="Zoom in on where the roof meets the wall"]'); if (b) b.click(); }],
      ["side", "From the side", () => cx.renderer.view("side")],
    ];
    for (const [key, label, go] of shots) {
      go();
      await cxWait(700);
      out.push({ name: a.name.slice(0, 40) + " — " + key + ".jpg", data: cxLabelledFrame(a.name + " — " + label) });
    }
    try { out.push({ name: a.name.slice(0, 40) + " — section.png", data: await cxSectionPNG(a) }); } catch (e) {}
  } finally {
    cx.vs.exploded = vs.exploded; cx.vs.section = vs.section; cx.vs.hidden = vs.hidden; cx.vs.isolated = vs.isolated;
    cxApplyVS();
    host.classList.remove("cx-capturing");
    await cxWait(100);
    cx.renderer.setCamera(cam);
    if (cx.pane !== pane) { cx.pane = pane; cxPaintSwitch(wb); }
  }
  return out;
}
async function cxCheckModel() {
  const a = cxAsm();
  if (!a || cxp.sending) return;
  cxp.msg = "Taking pictures of the model…";
  cxChatRepaint();
  let images;
  try { images = await cxCaptureModel(); }
  catch (e) { cxp.msg = "Couldn't picture the model: " + e.message; cxChatRepaint(); return; }
  cxp.msg = "";
  await cxChatSend("Check this model: “" + a.name + "”. Attached are three labelled views of it as the model draws it now and its true section drawing. Compare them with the drawing sheets in your brief (open the sheets that show this junction — sections and elevations — and read their dimensions) and with the code and the makers' instructions. Tell me plainly what's wrong, missing or out of proportion, citing the sheet or source for each point. Propose the fixes the model can take as one construction block (SetPitch, SetDimension, SetMaterial…), and say what it can't show at all.", images);
}
