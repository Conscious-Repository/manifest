// ================= CONSTRUCTION — problems inside a property or the Home =================
// A construction problem lives inside the existing context that owns it: a
// property page (#/properties/<slug>/construction/<id>) or the shared Home in
// TASKS (#/tasks/home-construction/<id>). Records are the server's private
// construction store; the property/Home records are only read. Every write is
// a typed command with a request id and the exact revision it was made on, so
// a lost answer is retried, never re-applied, and a stale edit is shown, never
// merged. All text renders as text.

const CX_PILOT_TITLE = "761 N Euclid — Back Addition"; // §12.1 owner-facing pilot (title only; no site data)
const cx = { subject: null, problemId: "", view: null, session: {}, seq: 0, host: null, error: "", busy: false,
  layout: null, activeAssembly: "", selection: "", tab: "problem", pane: "model" };

function cxBase(subject) {
  return subject.kind === "home" ? "/api/home/construction" : "/api/properties/" + encodeURIComponent(subject.id) + "/construction";
}
function cxHref(subject, problemId) {
  const tail = problemId ? "/" + encodeURIComponent(problemId) : "";
  return subject.kind === "home" ? "#/tasks/home-construction" + tail : "#/properties/" + encodeURIComponent(subject.id) + "/construction" + tail;
}
function cxSameSubject(a, b) { return !!a && !!b && a.kind === b.kind && a.id === b.id; }
function cxHex(bytes) { const b = new Uint8Array(bytes); crypto.getRandomValues(b); return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join(""); }
function cxRequestId() { return "cx-" + cxHex(16); }
function cxNewId(kind) { return kind + "-" + cxHex(16); }

// cxSession fetches (and caches) the per-process mutation nonce for a base.
async function cxSession(base, force) {
  if (!force && cx.session[base]) return cx.session[base];
  const r = await fetch(base + "/session", { credentials: "same-origin" });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw cxErr(r.status, j);
  cx.session[base] = j;
  return j;
}
function cxErr(status, j) {
  const e = new Error((j && j.error) || "HTTP " + status);
  e.status = status; e.kind = j && j.kind; e.problems = (j && j.problems) || []; e.current = j && j.current;
  return e;
}

// cxApi: JSON GET/POST. Mutations carry the session nonce; a stale nonce
// (server restarted) refreshes once and resends the SAME body — the request
// id makes that resend a replay, never a second edit.
async function cxApi(method, base, path, body, raw) {
  const opts = { method, credentials: "same-origin", headers: {} };
  if (method !== "GET") {
    const s = await cxSession(base);
    opts.headers["X-Construction-Nonce"] = s.nonce;
    if (raw) opts.body = raw;
    else { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
  }
  let r = await fetch(base + path, opts);
  let j = await r.json().catch(() => null);
  if (r.status === 403 && j && j.kind === "nonce" && method !== "GET") {
    const s = await cxSession(base, true);
    opts.headers["X-Construction-Nonce"] = s.nonce;
    r = await fetch(base + path, opts);
    j = await r.json().catch(() => null);
  }
  if (!r.ok) throw cxErr(r.status, j);
  return j;
}

// ---- lost-ACK recovery: a mutation is remembered before it is sent ----------
function cxPendingKey(problemId) { return "cx.pending." + problemId; }
function cxPendingGet(problemId) { try { return JSON.parse(sessionStorage.getItem(cxPendingKey(problemId)) || "null"); } catch (e) { return null; } }
function cxPendingSet(problemId, p) { try { if (p) sessionStorage.setItem(cxPendingKey(problemId), JSON.stringify(p)); else sessionStorage.removeItem(cxPendingKey(problemId)); } catch (e) {} }

// cxSend sends one remembered mutation; success applies its view.
async function cxSend(pending) {
  const base = cxBase(cx.subject);
  const res = await cxApi("POST", base, pending.path, pending.body);
  cxPendingSet(cx.problemId, null);
  if (res && res.view) cxApplyView(res.view);
  return res;
}

// cxCommand: one typed command on the current problem (optionally an
// assembly). Expected revisions are the ones this page last loaded.
async function cxCommand(ops, opt = {}) {
  if (!cx.view || cx.busy) return null;
  const body = { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId, operations: ops };
  const revs = cx.view.revisions || {};
  if (ops.some((o) => CX_PROBLEM_OPS.has(o.op))) body.expectedProblemRevision = revs.problem;
  if (opt.assembly) { body.assemblyId = opt.assembly; body.expectedAssemblyRevision = revs["assembly:" + opt.assembly]; }
  const pending = { path: "/problems/" + encodeURIComponent(cx.problemId) + "/commands", body, label: opt.label || ops.map((o) => o.op).join(", ") };
  cxPendingSet(cx.problemId, pending);
  cx.busy = true; cx.error = "";
  try { return await cxSend(pending); }
  catch (e) { cxHandleError(e, pending); return null; }
  finally { cx.busy = false; cxPaintStatus(); }
}
const CX_PROBLEM_OPS = new Set(["SetProblemText", "SetContext", "AddFact", "RemoveFact", "SetSteward", "SetScope", "SetInputMeta", "SetLifecycle", "LinkExternal"]);

function cxHandleError(e, pending) {
  if (e.status === 409 || e.status === 422 || e.status === 403 || e.status === 404 || e.status === 413) {
    cxPendingSet(cx.problemId, null); // refused: nothing was applied, nothing to retry
    cx.error = e.status === 409
      ? "Not saved — this changed since you loaded it (" + (e.message || "conflict") + "). Your draft is kept; reload the latest to review."
      : (e.message || "Refused") + (e.problems.length ? ": " + e.problems.join("; ") : "");
  } else {
    cx.error = "Not confirmed (" + (e.message || "network") + "). The edit is remembered; Retry sends the same request.";
  }
  cxPaintStatus();
}

// ---- view state --------------------------------------------------------------------
function cxApplyView(view) {
  if (!view || !view.problem || view.problem.id !== cx.problemId) return; // stale answer for another problem
  if (cx.view && view.generation < cx.view.generation) return;            // older than what is shown
  cx.view = view;
  const alts = view.problem.alternatives || [];
  if (!cx.activeAssembly || !alts.includes(cx.activeAssembly)) cx.activeAssembly = view.problem.activeAssembly || alts[0] || "";
  cxRender();
}

async function cxLoad() {
  const seq = ++cx.seq, problemId = cx.problemId, subject = cx.subject;
  try {
    const view = await cxApi("GET", cxBase(subject), "/problems/" + encodeURIComponent(problemId));
    if (seq !== cx.seq || problemId !== cx.problemId || !cxSameSubject(subject, cx.subject)) return; // superseded
    cx.view = null;
    cxApplyView(view);
  } catch (e) {
    if (seq !== cx.seq) return;
    cx.view = null; cx.error = e.status === 404 ? "This construction problem does not exist here." : "Couldn't load: " + e.message;
    cxRender();
  }
}

// ---- entry points --------------------------------------------------------------------

// The property page's CONSTRUCTION section: its problems + a way in.
function constructionPropertySection(p) {
  const subject = { kind: "property", id: p.slug };
  const sec = el("div", "pp3-sec cx-entry");
  const head = el("div", "pp3-sec-head");
  head.append(el("span", "pp3-sec-title", "CONSTRUCTION"));
  const count = el("span", "pp3-sec-count", "…");
  head.append(count);
  const open = el("a", "cx-entry-open", "Open construction →");
  open.href = cxHref(subject, "");
  head.append(open);
  sec.append(head);
  const list = el("div", "cx-entry-list");
  sec.append(list);
  cxApi("GET", cxBase(subject), "/problems").then((d) => {
    const rows = (d && d.problems) || [];
    count.textContent = rows.length ? rows.length + (rows.length === 1 ? " problem" : " problems") : "none yet";
    rows.slice(0, 4).forEach((row) => list.append(cxProblemRow(subject, row)));
  }).catch((e) => { count.textContent = e.status === 503 ? "unavailable here" : "couldn't load"; });
  return sec;
}

function cxProblemRow(subject, row) {
  const a = el("a", "cx-row");
  a.href = cxHref(subject, row.id);
  a.append(el("span", "cx-row-title", row.title || row.id));
  const meta = [row.lifecycle || "", row.alternatives ? row.alternatives + (row.alternatives === 1 ? " alternative" : " alternatives") : "", row.inputs ? row.inputs + " inputs" : ""].filter(Boolean).join(" · ");
  a.append(el("span", "cx-row-meta micro-label", row.error ? "unreadable: " + row.error : meta));
  return a;
}

// showConstructionProperty — #/properties/<slug>/construction[/<id>]
function cxHideOther(keep) {
  for (const id of ["constructionHost", "homeConstructionHost"]) {
    if (id === keep) continue;
    const h = document.getElementById(id);
    if (h && !h.hidden) { h.hidden = true; h.innerHTML = ""; }
  }
}

function showConstructionProperty(slug, problemId) {
  const host = document.getElementById("constructionHost");
  if (!host) return;
  cxHideOther("constructionHost");
  host.hidden = false;
  els.propertiesView.classList.add("cx-wide");
  cxOpen(host, { kind: "property", id: slug }, problemId);
}

// homeConstructionRender — TASKS › Home construction (list + create)
function homeConstructionRender(host) {
  const wrap = el("div", "cx-home");
  host.append(wrap);
  cxListInto(wrap, { kind: "home", id: "home" });
}

// showConstructionHome — #/tasks/home-construction[/<id>]
function showConstructionHome(problemId) {
  const host = document.getElementById("homeConstructionHost");
  if (!host) return;
  cxHideOther("homeConstructionHost");
  host.hidden = false;
  els.todosView.classList.add("cx-wide");
  cxOpen(host, { kind: "home", id: "home" }, problemId);
}

// constructionLeave — called by routing when a non-construction view shows.
function constructionLeave() {
  for (const id of ["constructionHost", "homeConstructionHost"]) { const h = document.getElementById(id); if (h) { h.hidden = true; h.innerHTML = ""; } }
  els.propertiesView && els.propertiesView.classList.remove("cx-wide");
  els.todosView && els.todosView.classList.remove("cx-wide");
  if (typeof cxRendererDispose === "function") cxRendererDispose();
  cx.subject = null; cx.problemId = ""; cx.view = null; cx.seq++;
}

function cxOpen(host, subject, problemId) {
  const changed = !cxSameSubject(subject, cx.subject) || problemId !== cx.problemId;
  cx.host = host;
  if (changed) {
    if (typeof cxRendererDispose === "function") cxRendererDispose();
    cx.subject = subject; cx.problemId = problemId || ""; cx.view = null; cx.error = ""; cx.selection = ""; cx.activeAssembly = ""; cx.seq++;
  }
  host.innerHTML = "";
  if (!problemId) { cxListInto(host, subject); return; }
  host.append(el("p", "cx-empty", "Loading the construction problem…"));
  cxLoad();
}

// ---- list + create ---------------------------------------------------------------------

function cxBackLink(subject) {
  const a = el("a", "cx-back");
  if (subject.kind === "home") { a.href = "#/tasks"; a.textContent = "‹ Home (Tasks)"; }
  else { a.href = "#/properties/" + encodeURIComponent(subject.id); a.textContent = "‹ Property"; }
  return a;
}

async function cxListInto(host, subject) {
  host.innerHTML = "";
  const page = el("div", "cx-list-page");
  const head = el("div", "cx-list-head");
  head.append(cxBackLink(subject), el("h2", "cx-list-title", subject.kind === "home" ? "Home · Construction" : "Construction"));
  page.append(head);
  page.append(el("p", "cx-notice", "Research/design assistance; not approved for construction; field, code, structural and manufacturer verification required."));
  const list = el("div", "cx-list");
  list.append(el("p", "cx-empty", "Loading…"));
  page.append(list);
  host.append(page);
  let session = null;
  try {
    session = await cxSession(cxBase(subject));
    const d = await cxApi("GET", cxBase(subject), "/problems");
    list.innerHTML = "";
    const rows = (d && d.problems) || [];
    if (!rows.length) list.append(el("p", "cx-empty", "No construction problems yet."));
    rows.forEach((row) => list.append(cxProblemRow(subject, row)));
    if (session.subject && session.subject.title) head.querySelector(".cx-list-title").textContent = (session.subject.title || "") + " · Construction";
  } catch (e) {
    list.innerHTML = "";
    list.append(el("p", "cx-empty", e.status === 503 ? "Construction is not enabled on this server." : "Couldn't load construction problems: " + e.message));
    return;
  }
  page.append(cxCreateForm(subject, session));
}

function cxScopeOptions(subject) {
  const out = [["", "No linked scope"]];
  if (subject.kind === "property" && typeof propertyCache !== "undefined") {
    const p = (propertyCache || []).find((x) => x.slug === subject.id);
    (p && p.work || []).forEach((st) => {
      out.push([JSON.stringify({ workId: st.id }), st.text + " · " + st.id]);
      (st.tasks || []).forEach((n) => out.push([JSON.stringify({ workId: n.id }), "  " + n.text + " · " + n.id]));
    });
  }
  return out;
}

function cxCreateForm(subject, session) {
  const form = el("form", "cx-create");
  form.append(el("div", "pp3-sec-title", "NEW CONSTRUCTION PROBLEM"));
  const title = inputEl("Title, e.g. Corrugated roof to masonry wall");
  title.className = "cx-in"; title.setAttribute("aria-label", "Problem title");
  if (subject.kind === "home") title.value = CX_PILOT_TITLE;
  const narrative = el("textarea", "cx-in cx-narrative");
  narrative.placeholder = "Describe the junction: what exists, what is proposed, what must stay visible. Unknowns can stay unknown.";
  narrative.setAttribute("aria-label", "Problem narrative");
  form.append(title, narrative);
  let scopeSel = null, taskIn = null;
  if (subject.kind === "property") {
    scopeSel = selectEl([]);
    scopeSel.className = "cx-in"; scopeSel.setAttribute("aria-label", "Linked work scope");
    cxScopeOptions(subject).forEach(([v, label]) => { const o = document.createElement("option"); o.value = v; o.textContent = label; scopeSel.append(o); });
    form.append(scopeSel);
  } else {
    taskIn = inputEl("Linked shared Home task id (optional, exact)");
    taskIn.className = "cx-in"; taskIn.setAttribute("aria-label", "Linked Home task id");
    form.append(taskIn);
  }
  const templates = (session && session.templates) || [];
  let tpl = null;
  if (templates.length) {
    const lab = el("label", "cx-check");
    tpl = document.createElement("input"); tpl.type = "checkbox"; tpl.checked = true;
    lab.append(tpl, document.createTextNode(" Start a draft roof-to-masonry assembly (illustrative numbers, all labelled unverified)"));
    form.append(lab);
  }
  const msg = el("p", "cx-form-msg");
  const go = el("button", "pill", "Create problem");
  go.type = "submit";
  form.append(go, msg);
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    if (!title.value.trim()) { msg.textContent = "A title is required."; return; }
    const body = { schemaVersion: 1, requestId: form.dataset.requestId || cxRequestId(), title: title.value.trim(), narrative: narrative.value };
    form.dataset.requestId = body.requestId; // a retry of this form reuses its identity
    if (scopeSel && scopeSel.value) body.scope = JSON.parse(scopeSel.value);
    if (taskIn && taskIn.value.trim()) body.scope = { taskId: taskIn.value.trim() };
    if (tpl && tpl.checked) body.template = templates[0];
    go.disabled = true; msg.textContent = "Creating…";
    try {
      const res = await cxApi("POST", cxBase(subject), "/problems", body);
      delete form.dataset.requestId;
      location.hash = cxHref(subject, res.view.problem.id);
    } catch (e) {
      msg.textContent = (e.status ? "" : "Not confirmed — submitting again is safe. ") + e.message + (e.problems.length ? ": " + e.problems.join("; ") : "");
      if (e.status && e.status !== 409) delete form.dataset.requestId;
    } finally { go.disabled = false; }
  };
  return form;
}

// ---- workbench shell -----------------------------------------------------------------

const CX_LAYOUT_KEY = "cx.layout.v1";
function cxLayout() {
  if (cx.layout) return cx.layout;
  let l = null;
  try { l = JSON.parse(localStorage.getItem(CX_LAYOUT_KEY) || "null"); } catch (e) {}
  cx.layout = Object.assign({ a: 300, right: 400, cFrac: 0.5, aCollapsed: false }, l || {});
  return cx.layout;
}
function cxSaveLayout() { try { localStorage.setItem(CX_LAYOUT_KEY, JSON.stringify(cx.layout)); } catch (e) {} }

function cxRender() {
  const host = cx.host;
  if (!host || !cx.subject || !cx.problemId) return;
  if (!cx.view) {
    host.innerHTML = "";
    const page = el("div", "cx-list-page");
    page.append(cxBackLink(cx.subject), el("p", "cx-empty", cx.error || "Loading…"));
    host.append(page);
    return;
  }
  let wb = host.querySelector(".cx-wb");
  if (!wb || wb.dataset.problem !== cx.problemId) {
    host.innerHTML = "";
    wb = cxWorkbenchShell();
    host.append(wb);
  }
  cxPaintHeader(wb);
  cxPaintStatus();
  cxPaintAgent(wb.querySelector(".cx-pane-a .cx-pane-body"));
  if (typeof cxPaintModel === "function") cxPaintModel(wb.querySelector(".cx-pane-b .cx-pane-body"));
  else cxPaintNoModel(wb.querySelector(".cx-pane-b .cx-pane-body"));
  if (typeof cxPaintInspector === "function") cxPaintInspector(wb.querySelector(".cx-pane-c .cx-pane-body"));
  else cxPaintNoInspector(wb.querySelector(".cx-pane-c .cx-pane-body"));
  cxPaintResearch(wb.querySelector(".cx-pane-d .cx-pane-body"));
}

function cxWorkbenchShell() {
  const L = cxLayout();
  const wb = el("div", "cx-wb");
  wb.dataset.problem = cx.problemId;
  wb.style.setProperty("--cx-a", L.a + "px");
  wb.style.setProperty("--cx-right", L.right + "px");
  wb.style.setProperty("--cx-cfrac", String(L.cFrac));
  wb.classList.toggle("a-collapsed", !!L.aCollapsed);
  const header = el("div", "cx-head");
  const status = el("div", "cx-status");
  status.setAttribute("role", "status");
  const sw = el("div", "cx-switch");
  sw.setAttribute("role", "tablist");
  sw.setAttribute("aria-label", "Construction panes");
  for (const [k, label] of [["agent", "Agent"], ["model", "Model"], ["assembly", "Assembly"], ["research", "Research"]]) {
    const b = el("button", "cx-switch-btn", label);
    b.type = "button"; b.dataset.pane = k; b.setAttribute("role", "tab");
    b.onclick = () => { cx.pane = k; cxPaintSwitch(wb); };
    sw.append(b);
  }
  const crumb = el("div", "cx-crumb micro-label");
  crumb.setAttribute("aria-live", "polite");
  const grid = el("div", "cx-grid");
  const pane = (cls, title, key) => {
    const p = el("section", "cx-pane " + cls);
    p.dataset.pane = key;
    p.setAttribute("aria-label", title);
    const h = el("div", "cx-pane-head");
    h.append(el("span", "cx-pane-title micro-label", title));
    p.append(h, el("div", "cx-pane-body"));
    return p;
  };
  const a = pane("cx-pane-a", "Agent", "agent");
  const collapse = el("button", "cx-collapse", "‹");
  collapse.type = "button"; collapse.title = "Collapse the agent pane (its draft and conversation stay)";
  collapse.setAttribute("aria-label", collapse.title);
  collapse.onclick = () => { L.aCollapsed = !L.aCollapsed; wb.classList.toggle("a-collapsed", L.aCollapsed); collapse.textContent = L.aCollapsed ? "›" : "‹"; cxSaveLayout(); window.dispatchEvent(new Event("resize")); };
  collapse.textContent = L.aCollapsed ? "›" : "‹";
  a.querySelector(".cx-pane-head").append(collapse);
  const b = pane("cx-pane-b", "Model", "model");
  const c = pane("cx-pane-c", "Assembly", "assembly");
  const d = pane("cx-pane-d", "Research", "research");
  const right = el("div", "cx-right");
  right.append(c, cxDivider("cfrac", "Resize assembly and research panes"), d);
  grid.append(a, cxDivider("a", "Resize agent pane"), b, cxDivider("right", "Resize the right-hand panes"), right);
  wb.append(header, status, sw, crumb, grid);
  cxPaintSwitch(wb);
  return wb;
}

// cxDivider: a focusable separator. Pointer drag or arrow keys resize; the
// widths persist per browser.
function cxDivider(which, label) {
  const dv = el("div", "cx-divider cx-divider-" + which);
  dv.setAttribute("role", "separator");
  dv.setAttribute("tabindex", "0");
  dv.setAttribute("aria-label", label);
  dv.setAttribute("aria-orientation", which === "cfrac" ? "horizontal" : "vertical");
  const L = cxLayout();
  const apply = () => {
    const wb = dv.closest(".cx-wb");
    if (!wb) return;
    L.a = Math.max(220, Math.min(560, L.a)); L.right = Math.max(280, Math.min(720, L.right)); L.cFrac = Math.max(0.2, Math.min(0.8, L.cFrac));
    wb.style.setProperty("--cx-a", L.a + "px"); wb.style.setProperty("--cx-right", L.right + "px"); wb.style.setProperty("--cx-cfrac", String(L.cFrac));
    dv.setAttribute("aria-valuenow", String(which === "cfrac" ? Math.round(L.cFrac * 100) : which === "a" ? L.a : L.right));
    cxSaveLayout();
    window.dispatchEvent(new Event("resize"));
  };
  dv.addEventListener("keydown", (e) => {
    const step = e.shiftKey ? 40 : 12;
    const dir = { ArrowLeft: -1, ArrowUp: -1, ArrowRight: 1, ArrowDown: 1 }[e.key];
    if (!dir) return;
    e.preventDefault();
    if (which === "a") L.a += dir * step;
    else if (which === "right") L.right -= dir * step;
    else L.cFrac += dir * (step / 600);
    apply();
  });
  dv.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    dv.setPointerCapture(e.pointerId);
    const start = { x: e.clientX, y: e.clientY, a: L.a, right: L.right, c: L.cFrac };
    const rightEl = dv.closest(".cx-wb").querySelector(".cx-right");
    const h = rightEl ? rightEl.getBoundingClientRect().height : 600;
    const move = (ev) => {
      if (which === "a") L.a = start.a + (ev.clientX - start.x);
      else if (which === "right") L.right = start.right - (ev.clientX - start.x);
      else L.cFrac = start.c + (ev.clientY - start.y) / Math.max(200, h);
      apply();
    };
    const up = () => { dv.removeEventListener("pointermove", move); dv.removeEventListener("pointerup", up); };
    dv.addEventListener("pointermove", move);
    dv.addEventListener("pointerup", up);
  });
  return dv;
}

function cxPaintSwitch(wb) {
  wb.dataset.active = cx.pane;
  wb.querySelectorAll(".cx-switch-btn").forEach((b) => { const on = b.dataset.pane === cx.pane; b.classList.toggle("on", on); b.setAttribute("aria-selected", String(on)); });
  wb.querySelectorAll(".cx-pane").forEach((p) => p.classList.toggle("cx-pane-on", p.dataset.pane === cx.pane));
  window.dispatchEvent(new Event("resize"));
}

function cxPaintHeader(wb) {
  const h = wb.querySelector(".cx-head");
  const v = cx.view, p = v.problem, ctx = v.context || {};
  h.innerHTML = "";
  const top = el("div", "cx-head-top");
  top.append(cxBackLink(cx.subject));
  const subj = ctx.subject || {};
  top.append(el("span", "cx-head-subject micro-label", (subj.kind === "home" ? "HOME" : "PROPERTY") + " · " + (subj.title || subj.id || "") + (subj.status && subj.status !== "resolved" ? " · " + subj.status : "")));
  const t = el("h2", "cx-title", p.title);
  t.title = "Click to rename";
  t.tabIndex = 0;
  const rename = () => {
    if (v.readOnly) return;
    inlineRename(t, p.title, (val) => { if (val && val !== p.title) cxCommand([{ op: "SetProblemText", title: val }]); });
  };
  t.onclick = rename;
  t.onkeydown = (e) => { if (e.key === "Enter") rename(); };
  const meta = el("span", "cx-head-meta micro-label", p.lifecycle + " · generation " + v.generation + (v.readOnly ? " · READ-ONLY (newer schema)" : ""));
  h.append(top, t, meta);
  h.append(el("p", "cx-notice", v.notice || "Research/design assistance; not approved for construction."));
}

function cxPaintStatus() {
  const wb = cx.host && cx.host.querySelector(".cx-wb");
  const s = wb ? wb.querySelector(".cx-status") : null;
  if (!s) return;
  s.innerHTML = "";
  const pending = cxPendingGet(cx.problemId);
  if (cx.busy) s.append(el("span", "cx-status-busy", "Saving…"));
  if (cx.error) s.append(el("span", "cx-status-err", cx.error));
  if (pending && !cx.busy) {
    s.append(el("span", "cx-status-pending", "Unconfirmed edit: " + pending.label));
    s.append(pillLight("Retry", async () => { cx.busy = true; cx.error = ""; cxPaintStatus(); try { await cxSend(pending); } catch (e) { cxHandleError(e, pending); } finally { cx.busy = false; cxPaintStatus(); } }));
    s.append(pillLight("Discard", () => { cxPendingSet(cx.problemId, null); cx.error = ""; cxPaintStatus(); }));
  }
  if (cx.error && !pending) s.append(pillLight("Reload latest", () => { cx.error = ""; cxLoad(); }));
}

function cxPaintNoModel(body) {
  body.innerHTML = "";
  body.append(el("p", "cx-empty", (cx.view.problem.alternatives || []).length ? "Loading the model…" : "No assembly yet. A draft assembly can be started from the roof-to-masonry template when the problem is created."));
}
function cxPaintNoInspector(body) {
  body.innerHTML = "";
  body.append(el("p", "cx-empty", "Select an assembly to inspect its ordered components."));
}

// Agent pane (A): steward identity and the research state. Assigning a
// steward never starts a conversation or a run; nothing here is simulated.
function cxPaintAgent(body) {
  if (typeof cxPaintAgentFull === "function") return cxPaintAgentFull(body);
  body.innerHTML = "";
  const p = cx.view.problem;
  const row = el("div", "cx-field");
  row.append(el("span", "cx-label micro-label", "Steward"));
  const sel = selectEl(["alfred", "zeck"]);
  sel.value = p.steward.agent; sel.className = "cx-in"; sel.setAttribute("aria-label", "Steward agent");
  sel.disabled = !!cx.view.readOnly;
  sel.onchange = () => cxCommand([{ op: "SetSteward", agent: sel.value }]);
  row.append(sel);
  body.append(row);
  body.append(el("p", "cx-hint", p.steward.agent === "alfred" ? "Alfred is the default steward." : "Zeck was explicitly selected as the real-estate specialist."));
  body.append(el("p", "cx-hint", "Assigning a steward does not start a conversation or research."));
  body.append(el("p", "cx-empty", "No research run yet."));
}

// Research pane (D): tabs. P1 ships the Problem tab (facts, context, inputs,
// linked source records); later tabs register through cxResearchTabs.
const cxResearchTabs = [["problem", "Problem", (b) => cxPaintProblemTab(b)]];
function cxPaintResearch(body) {
  body.innerHTML = "";
  const tabs = el("div", "cx-tabs");
  tabs.setAttribute("role", "tablist");
  const inner = el("div", "cx-tab-body");
  if (!cxResearchTabs.some(([k]) => k === cx.tab)) cx.tab = cxResearchTabs[0][0];
  cxResearchTabs.forEach(([k, label, paint]) => {
    const b = el("button", "cx-tab" + (k === cx.tab ? " on" : ""), label);
    b.type = "button"; b.setAttribute("role", "tab"); b.setAttribute("aria-selected", String(k === cx.tab));
    b.onclick = () => { cx.tab = k; cxPaintResearch(body); };
    tabs.append(b);
    if (k === cx.tab) paint(inner);
  });
  body.append(tabs, inner);
}

const CX_PROVENANCES = ["user-assumption", "verified-fact", "directly-applicable-guidance", "adapted-precedent", "engineering-inference", "unknown"];

function cxPaintProblemTab(b) {
  const v = cx.view, p = v.problem, ro = !!v.readOnly;
  // narrative
  const nar = el("div", "cx-block");
  nar.append(el("div", "cx-label micro-label", "Narrative"));
  const ta = el("textarea", "cx-in cx-narrative");
  ta.value = p.narrative || ""; ta.disabled = ro; ta.setAttribute("aria-label", "Narrative");
  const save = pillLight("Save narrative", () => cxCommand([{ op: "SetProblemText", narrative: ta.value }]));
  save.disabled = ro;
  nar.append(ta, save);
  b.append(nar);
  // linked source records (read-only projection)
  b.append(cxContextBlock(v.context || {}));
  // existing / proposed conditions
  for (const [list, label] of [["existing", "Existing conditions"], ["proposed", "Proposed conditions"]]) {
    const blk = el("div", "cx-block");
    blk.append(el("div", "cx-label micro-label", label));
    (p[list] || []).forEach((f) => {
      const row = el("div", "cx-fact");
      row.append(el("span", "cx-fact-text", f.text), el("span", "cx-chip micro-label", f.provenance));
      if (!ro) { const x = el("button", "cx-x", "✕"); x.type = "button"; x.title = "Remove"; x.setAttribute("aria-label", "Remove fact"); x.onclick = () => cxCommand([{ op: "RemoveFact", id: f.id }]); row.append(x); }
      blk.append(row);
    });
    if (!ro) {
      const add = el("div", "cx-fact-add");
      const tin = inputEl(list === "existing" ? "Add an existing condition" : "Add a proposed condition"); tin.className = "cx-in"; tin.setAttribute("aria-label", "New " + list + " condition");
      const prov = selectEl(CX_PROVENANCES); prov.className = "cx-in"; prov.setAttribute("aria-label", "Provenance");
      const go = pillLight("Add", () => { if (tin.value.trim()) cxCommand([{ op: "AddFact", id: cxNewId("clm"), list, text: tin.value.trim(), provenance: prov.value }]); });
      add.append(tin, prov, go);
      blk.append(add);
    }
    b.append(blk);
  }
  // location / jurisdiction / climate
  const ctxb = el("div", "cx-block");
  ctxb.append(el("div", "cx-label micro-label", "Location · jurisdiction · climate (never inferred)"));
  for (const field of ["location", "jurisdiction", "climate"]) {
    const cur = p[field] || { text: "", state: "unknown", provenance: "unknown" };
    const row = el("div", "cx-ctx-row");
    const tin = inputEl(field); tin.value = cur.text || ""; tin.className = "cx-in"; tin.setAttribute("aria-label", field); tin.disabled = ro;
    const st = selectEl(["unknown", "assumed", "known"]); st.value = cur.state; st.className = "cx-in"; st.setAttribute("aria-label", field + " state"); st.disabled = ro;
    const prov = selectEl(CX_PROVENANCES); prov.value = cur.provenance; prov.className = "cx-in"; prov.setAttribute("aria-label", field + " provenance"); prov.disabled = ro;
    st.onchange = () => { if (st.value === "unknown") prov.value = "unknown"; };
    const go = pillLight("Set", () => cxCommand([{ op: "SetContext", field, text: tin.value, state: st.value, provenance: st.value === "unknown" ? "unknown" : prov.value }]));
    go.disabled = ro;
    row.append(el("span", "cx-ctx-field micro-label", field), tin, st, prov, go);
    ctxb.append(row);
  }
  b.append(ctxb);
  b.append(cxInputsBlock());
}

function cxContextBlock(ctx) {
  const blk = el("div", "cx-block cx-context");
  blk.append(el("div", "cx-label micro-label", "Linked records (read-only)"));
  const subj = ctx.subject || {};
  blk.append(el("div", "cx-ctx-line", (subj.kind === "home" ? "Home" : "Property") + ": " + (subj.title || subj.id || "") + (subj.address ? " — " + subj.address : "") + " · " + (subj.status || "")));
  const sc = ctx.scope;
  if (sc) {
    const line = el("div", "cx-ctx-line" + (sc.status !== "resolved" ? " cx-unresolved" : ""));
    line.textContent = "Scope: " + (sc.text || sc.workId || sc.taskId) + " [" + (sc.workId || sc.taskId) + "] · " + sc.status + (sc.changed ? " · the source changed since it was linked" : "");
    blk.append(line);
    (ctx.tasks || []).forEach((t) => blk.append(el("div", "cx-ctx-task", (t.checked ? "✓ " : "○ ") + t.text)));
  } else blk.append(el("div", "cx-ctx-line cx-dim", "No linked scope."));
  const bud = ctx.budget;
  if (bud) {
    const fmt = (n) => (typeof fmtMoney === "function" ? fmtMoney(n) : "$" + Math.round(n || 0));
    blk.append(el("div", "cx-ctx-line", "Budget (property, read-only): plan " + fmt(bud.planTotal) + " · committed " + fmt(bud.committed) + " · paid " + fmt(bud.paid)));
  }
  return blk;
}

function cxArtifactURL(artifactId, revision, download) {
  return cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/artifacts/" + encodeURIComponent(artifactId) + "?revision=" + encodeURIComponent(revision) + (download ? "&download=1" : "");
}

function cxInputsBlock() {
  const v = cx.view, p = v.problem, ro = !!v.readOnly;
  const blk = el("div", "cx-block cx-inputs");
  blk.append(el("div", "cx-label micro-label", "Inputs — photos, drawings, documents (kept byte-exact, private)"));
  (p.inputs || []).forEach((inp) => {
    const row = el("div", "cx-input");
    if ((inp.mime || "").startsWith("image/")) {
      const img = document.createElement("img");
      img.className = "cx-thumb"; img.alt = inp.name; img.loading = "lazy";
      img.src = cxArtifactURL(inp.artifactId, inp.revision);
      row.append(img);
    } else row.append(el("span", "cx-thumb cx-thumb-doc", (inp.mime || "").includes("pdf") ? "PDF" : "TXT"));
    const col = el("div", "cx-input-col");
    const a = el("a", "cx-input-name", inp.name);
    a.href = cxArtifactURL(inp.artifactId, inp.revision, true);
    a.setAttribute("download", inp.name);
    col.append(a, el("span", "cx-input-meta micro-label", inp.role + " · " + Math.max(1, Math.round(inp.size / 1024)) + " KB · " + inp.verification + " · " + inp.revision.slice(0, 12)));
    if (inp.label) col.append(el("span", "cx-input-label", inp.label));
    row.append(col);
    blk.append(row);
  });
  if (!(p.inputs || []).length) blk.append(el("p", "cx-empty", "No inputs yet."));
  if (ro) return blk;
  const form = el("form", "cx-upload");
  const file = document.createElement("input");
  file.type = "file"; file.accept = "image/png,image/jpeg,image/webp,image/gif,application/pdf,text/plain"; file.setAttribute("aria-label", "Choose a file");
  const role = selectEl(["photo", "drawing", "document", "other"]); role.className = "cx-in"; role.setAttribute("aria-label", "Input role");
  const label = inputEl("Label (e.g. historical drawing — not field-verified)"); label.className = "cx-in"; label.setAttribute("aria-label", "Input label");
  const go = el("button", "pill light", "Upload"); go.type = "submit";
  const msg = el("span", "cx-form-msg");
  form.append(file, role, label, go, msg);
  form.onsubmit = async (e) => {
    e.preventDefault();
    if (!file.files || !file.files[0]) { msg.textContent = "Choose a file first."; return; }
    const fd = new FormData();
    const requestId = form.dataset.requestId || cxRequestId();
    form.dataset.requestId = requestId;
    fd.append("requestId", requestId);
    fd.append("expectedProblemRevision", cx.view.revisions.problem);
    fd.append("role", role.value);
    fd.append("label", label.value);
    fd.append("file", file.files[0], file.files[0].name);
    go.disabled = true; msg.textContent = "Uploading…";
    try {
      const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId) + "/inputs", null, fd);
      delete form.dataset.requestId;
      msg.textContent = "";
      cxApplyView(res.view);
    } catch (err) {
      msg.textContent = (err.status ? "" : "Not confirmed — uploading again is safe. ") + err.message + (err.problems.length ? ": " + err.problems.join("; ") : "");
      if (err.status) delete form.dataset.requestId;
    } finally { go.disabled = false; }
  };
  blk.append(form);
  return blk;
}
