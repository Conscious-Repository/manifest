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

// Construction is a local host-trust feature: the server answers loopback
// connections with a loopback Host and no proxy headers, and refuses other
// requests it can recognise as remote with kind "remote-disabled" (its message
// is the explanation shown). A raw TCP forward onto loopback cannot be
// detected, so remote use through any relay or tunnel is unsupported, not
// "blocked"; the local-only note says so.
const CX_REMOTE_MSG = "Construction Intelligence is a local feature for this computer's own browser. This request did not arrive as a loopback connection to a loopback address without proxy headers, so it was refused. Remote use through the tailnet, the LAN or any proxy, relay or tunnel is unsupported: it needs a verified, authenticated owner gateway, which does not exist.";
const CX_LOCAL_ONLY = "Local only: Construction trusts this computer's browser as the owner. Using it through the tailnet, a proxy, a port forward or an SSH or other tunnel is unsupported; a raw forward cannot be detected, so don't set one up. Remote use needs a verified, authenticated owner gateway, which does not exist yet.";
function cxRemoteOff(e) { return !!e && e.status === 403 && e.kind === "remote-disabled"; }
function cxRemoteNotice(e) { return el("p", "cx-notice cx-remote-off", (e && e.message) || CX_REMOTE_MSG); }

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
  try {
    const res = await cxSend(pending);
    if (opt.label !== "Undo" && opt.label !== "Redo") cx.redo = [];
    return res;
  }
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
  const first = !cx.view || !cx.vs;
  cx.view = view;
  const alts = view.problem.alternatives || [];
  if (first && typeof cxRestoreView === "function") cxRestoreView();
  if (!cx.activeAssembly || !alts.includes(cx.activeAssembly)) cx.activeAssembly = view.problem.activeAssembly || alts[0] || "";
  if (cx.selection && !cxSelectionExists()) cx.selection = ""; // a removed part is reported, not kept selected
  cxRender();
}

function cxSelectionExists() {
  const a = cx.view && (cx.view.assemblies || {})[cx.activeAssembly];
  return !!a && (a.components || []).some((c) => c.id === cx.selection);
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
    cx.view = null; cx.remote = cxRemoteOff(e);
    cx.error = cx.remote ? e.message || CX_REMOTE_MSG : e.status === 404 ? "This construction problem does not exist here." : "Couldn't load: " + e.message;
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
  }).catch((e) => {
    if (cxRemoteOff(e)) { count.textContent = "this machine only"; open.remove(); list.append(cxRemoteNotice(e)); return; }
    count.textContent = e.status === 503 ? "unavailable here" : "couldn't load";
  });
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
  cx.vs = null; cx.renderer = null; cx.rendererFailed = false; cx.geometry = null; cx.viewID = "";
}

function cxOpen(host, subject, problemId) {
  const changed = !cxSameSubject(subject, cx.subject) || problemId !== cx.problemId;
  cx.host = host;
  if (changed) {
    if (typeof cxRendererDispose === "function") cxRendererDispose();
    cx.subject = subject; cx.problemId = problemId || ""; cx.view = null; cx.error = ""; cx.remote = false; cx.selection = ""; cx.activeAssembly = ""; cx.seq++;
    cx.vs = null; cx.renderer = null; cx.rendererFailed = false; cx.geometry = null; cx.viewID = ""; cx.redo = []; cx.tab = "problem"; cx.exportMsg = ""; cx.section2D = false;
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
  page.append(el("p", "cx-notice cx-local-only", CX_LOCAL_ONLY));
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
    list.append(cxRemoteOff(e) ? cxRemoteNotice(e) : el("p", "cx-empty", e.status === 503 ? "Construction is not enabled on this server." : "Couldn't load construction problems: " + e.message));
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
    page.append(cxBackLink(cx.subject), el("p", cx.remote ? "cx-notice cx-remote-off" : "cx-empty", cx.error || "Loading…"));
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
  if (typeof cxPaintCrumb === "function") cxPaintCrumb();
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

// ================= P3 — model, inspector, comparison (the editable workbench) =================

const CX_UNITS = ["mm", "cm", "m", "in", "ft"];
const CX_TYPE_GROUPS = [
  ["Structure", ["rafter-array"]],
  ["Roof stack", ["timber-decking", "control-layer", "insulation-board", "underlayment", "counter-batten-array", "batten-array", "corrugated-sheet"]],
  ["Junction", ["profile-closure", "apron-flashing", "sidewall-flashing", "counterflashing", "through-wall-flashing", "sealant-bead"]],
  ["Masonry", ["masonry-wythe", "cavity-space", "weep-set", "end-dams"]],
  ["Attachment", ["fastener-set"]],
];
const CX_STRATEGIES = {
  unresolved: ["unresolved"],
  headwall: ["apron-surface-counterflashing", "apron-reglet-counterflashing", "apron-through-wall-flashing"],
  sidewall: ["sidewall-surface-counterflashing", "sidewall-reglet-counterflashing"],
};
const CX_MOVABLE = new Set(["apron-flashing", "sidewall-flashing", "counterflashing", "through-wall-flashing", "profile-closure", "sealant-bead"]);

function cxAsm() { return cx.view && cx.activeAssembly ? (cx.view.assemblies || {})[cx.activeAssembly] : null; }
function cxReport() { return cx.view && cx.activeAssembly ? (cx.view.validation || {})[cx.activeAssembly] : null; }
function cxComp(id) { const a = cxAsm(); return a ? (a.components || []).find((c) => c.id === id) : null; }
function cxQ(q) { if (!q) return null; return q.value != null ? q.value : q.placeholder; }
function cxQLabel(q) { if (!q) return ""; if (q.value == null) return "unresolved"; if (q.illustrative || q.state === "assumed") return "illustrative"; return q.provenance || ""; }
function cxFmt(v) { return v == null ? "—" : (Math.round(v * 100) / 100).toString(); }

// ---- selection: one store keyed by problem/assembly/revision/component -----
function cxSelect(componentId) {
  cx.selection = componentId || "";
  if (cx.renderer) cx.renderer.setState({ selection: cx.selection });
  cxPaintCrumb();
  const wb = cx.host && cx.host.querySelector(".cx-wb");
  if (!wb) return;
  cxPaintInspector(wb.querySelector(".cx-pane-c .cx-pane-body"));
  if (cx.tab === "issues" || cx.tab === "evidence") cxPaintResearch(wb.querySelector(".cx-pane-d .cx-pane-body"));
  cxQueueViewSave();
}
function cxPaintCrumb() {
  const wb = cx.host && cx.host.querySelector(".cx-wb");
  if (!wb) return;
  const a = cxAsm(), c = cxComp(cx.selection);
  wb.querySelector(".cx-crumb").textContent = [cx.view && cx.view.problem.title, a && a.name, c ? c.name : "no part selected"].filter(Boolean).join(" › ");
}

// ---- working view: restored on open, saved (debounced) as view commits ----
function cxViewID() {
  if (!cx.view) return "";
  if (!cx.viewID) cx.viewID = cx.view.problem.latestView || cxNewId("view");
  return cx.viewID;
}
function cxCurrentViewState() {
  const vs = cx.vs || {};
  return { name: "Working view", assemblyId: cx.activeAssembly, camera: cx.renderer ? cx.renderer.getCamera() : (vs.camera || { projection: "perspective", position: [3000, 4000, 2500], target: [1200, 600, 0], up: [0, 0, 1], fov: 35, zoom: 1 }),
    bookmarks: vs.bookmarks || [], section: vs.section || null, selection: cx.selection ? [cx.selection] : [], hidden: vs.hidden || [], isolated: vs.isolated || [],
    transparent: vs.transparent || [], exploded: vs.exploded || 0, mode: vs.mode || "technical", overlays: vs.overlays || { water: true, attachment: false }, measurements: vs.measurements || [] };
}
let cxViewTimer = 0;
function cxQueueViewSave() {
  if (!cx.view || cx.view.readOnly || !cx.activeAssembly) return;
  clearTimeout(cxViewTimer);
  cxViewTimer = setTimeout(cxSaveViewNow, 1500);
}
async function cxSaveViewNow(retry) {
  if (!cx.view || !cx.activeAssembly) return;
  const id = cxViewID();
  const body = { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId,
    operations: [{ op: "SaveView", viewId: id, expectedRevision: (cx.view.revisions || {})["view:" + id] || "", view: cxCurrentViewState() }] };
  try {
    const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId) + "/commands", body);
    if (res && res.view && res.view.problem.id === cx.problemId) { cx.view.revisions = res.view.revisions; cx.view.views = res.view.views; cx.view.generation = res.view.generation; cx.view.problem.latestView = res.view.problem.latestView; }
  } catch (e) {
    // a newer view revision exists (another tab): adopt its token, keep this state
    if (e.status === 409 && !retry && e.current) { Object.assign(cx.view.revisions, e.current); cxSaveViewNow(true); }
  }
}
function cxRestoreView() {
  const v = cx.view && (cx.view.views || {})[cx.view.problem.latestView];
  cx.vs = { mode: "technical", exploded: 0, hidden: [], isolated: [], transparent: [], section: null, overlays: { water: true, attachment: false }, bookmarks: [], measurements: [] };
  if (!v) return;
  cx.viewID = v.id;
  cx.vs = { camera: v.camera, mode: v.mode, exploded: v.exploded, hidden: v.hidden || [], isolated: v.isolated || [], transparent: v.transparent || [],
    section: v.section, overlays: v.overlays || { water: true, attachment: false }, bookmarks: v.bookmarks || [], measurements: v.measurements || [] };
  if ((cx.view.problem.alternatives || []).includes(v.assembly.id)) cx.activeAssembly = v.assembly.id;
  if (v.selection && v.selection[0]) cx.selection = v.selection[0];
}

// ---- model pane (B) --------------------------------------------------------
async function cxLoadGeometry(force) {
  const a = cxAsm();
  if (!a) return null;
  const rev = (cx.view.revisions || {})["assembly:" + a.id];
  if (!force && cx.geometry && cx.geometry.assemblyId === a.id && cx.geometry.revision === rev) return cx.geometry;
  const seq = ++cx.geoSeq, problemId = cx.problemId;
  const res = await cxApi("GET", cxBase(cx.subject), "/problems/" + encodeURIComponent(problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/geometry?revision=" + rev);
  if (seq !== cx.geoSeq || problemId !== cx.problemId) return null; // superseded by a newer revision or problem
  cx.geometry = { assemblyId: a.id, revision: res.assemblyRevision, ir: res.geometry, generatorChanged: res.generatorChanged };
  return cx.geometry;
}
cx.geoSeq = 0;

function cxPaintModel(body) {
  const a = cxAsm();
  if (!a) { cxRendererDispose(); cx.renderer = null; body.innerHTML = ""; body.append(el("p", "cx-empty cx-pad", "No assembly yet. Create the problem with the roof-to-masonry template to start a draft.")); return; }
  let wrap = body.querySelector(".cx-model");
  if (!wrap) {
    body.innerHTML = "";
    wrap = el("div", "cx-model");
    wrap.append(cxModelToolbar(), el("div", "cx-canvas-host"), el("div", "cx-model-status micro-label"));
    body.append(wrap);
    cx.renderer = null;
  }
  cxSyncToolbar(wrap);
  const host = wrap.querySelector(".cx-canvas-host");
  const status = wrap.querySelector(".cx-model-status");
  cxLoadGeometry().then(async (g) => {
    if (!g) return;
    status.textContent = "geometry " + g.ir.hash.slice(0, 12) + " · " + g.ir.triangles + " triangles · revision " + g.revision.slice(0, 8) + (g.generatorChanged ? " · generator changed since commit" : "") + " · " + (g.ir.notice || "");
    if (!cx.renderer && !cx.rendererFailed) {
      try {
        cx.renderer = await cxRendererCreate(host, {
          onSelect: (id) => cxSelect(id),
          onCamera: () => cxQueueViewSave(),
          onContextLost: () => cxShowFallback(host, "3D unavailable: the graphics context was lost. The inspector, sections and downloads still work; it restores automatically if the browser allows."),
          onContextRestored: () => { const fb = host.querySelector(".cx-fallback"); if (fb) fb.remove(); },
        });
        window.__cxRenderer = cx.renderer;
      } catch (e) {
        cx.rendererFailed = true;
        cxShowFallback(host, "3D unavailable here (" + e.message + "). This technical fallback lists the parts and offers the exact downloads; it does not replace the interactive model.");
        return;
      }
      cx.renderer.setGeometry(g.ir);
      if (cx.vs && cx.vs.camera) cx.renderer.setCamera(cx.vs.camera);
    } else if (cx.renderer && cx.renderer.__rev !== g.revision) {
      cx.renderer.setGeometry(g.ir, true);
    }
    if (cx.renderer) {
      cx.renderer.__rev = g.revision;
      cx.renderer.setState({ mode: cx.vs.mode, exploded: cx.vs.exploded, hidden: cx.vs.hidden, isolated: cx.vs.isolated, transparent: cx.vs.transparent, selection: cx.selection, section: cx.vs.section, overlays: cx.vs.overlays });
    }
  }).catch((e) => { status.textContent = "Couldn't load geometry: " + e.message; });
}

function cxShowFallback(host, msg) {
  if (host.querySelector(".cx-fallback")) return;
  const fb = el("div", "cx-fallback");
  fb.setAttribute("role", "status");
  fb.append(el("p", "cx-fallback-msg", msg));
  const a = cxAsm();
  if (a) {
    const dl = el("a", "cx-fallback-link", "Download this revision as GLB");
    dl.href = cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/glb?revision=" + (cx.view.revisions || {})["assembly:" + a.id];
    fb.append(dl);
    const ul = el("ul", "cx-fallback-list");
    (a.components || []).forEach((c) => { const li = el("li", null, c.name + (c.applicability === "inapplicable" ? " (switched off)" : "")); li.tabIndex = 0; li.onclick = () => cxSelect(c.id); ul.append(li); });
    fb.append(ul);
  }
  host.append(fb);
}

function cxToolBtn(label, title, fn, cls) {
  const b = el("button", "cx-tool" + (cls ? " " + cls : ""), label);
  b.type = "button"; b.title = title; b.setAttribute("aria-label", title);
  b.onclick = fn;
  return b;
}

const CX_SECTION_PRESETS = {
  "across-wall": { label: "Across the wall (X)", normal: [1, 0, 0], up: [0, 0, 1] },
  "along-wall": { label: "Along the wall (Y)", normal: [0, 1, 0], up: [0, 0, 1] },
  "plan": { label: "Plan (Z)", normal: [0, 0, 1], up: [0, 1, 0] },
  "oblique": { label: "Oblique (X 20° Z 10°)", normal: [0.94, 0.34, 0.17], up: [0, 0, 1] },
};

function cxModelToolbar() {
  const tb = el("div", "cx-toolbar");
  tb.setAttribute("role", "toolbar");
  tb.setAttribute("aria-label", "Model tools");
  const mode = el("div", "cx-seg");
  for (const m of ["technical", "realistic"]) {
    const b = cxToolBtn(m === "technical" ? "Technical" : "Material preview", m === "technical" ? "Technical mode: edges, flat colours" : "Material preview: same geometry, physically based materials (not a photoreal render)", () => { cx.vs.mode = m; cxApplyVS(); });
    b.dataset.mode = m;
    mode.append(b);
  }
  const proj = cxToolBtn("Ortho", "Toggle orthographic / perspective", () => { if (!cx.renderer) return; cx.renderer.setProjection(cx.renderer.projection() === "orthographic" ? "perspective" : "orthographic"); cxSyncToolbar(tb.parentElement); });
  proj.dataset.role = "projection";
  const views = el("div", "cx-seg");
  for (const [k, l] of [["front", "Front"], ["side", "Side"], ["top", "Top"], ["iso", "Iso"]]) views.append(cxToolBtn(l, l + " view", () => { if (cx.renderer) { cx.renderer.view(k); cxSyncToolbar(tb.parentElement); } }));
  const sec = el("label", "cx-tool-check");
  const secOn = document.createElement("input"); secOn.type = "checkbox"; secOn.setAttribute("aria-label", "Section plane on/off"); secOn.dataset.role = "section";
  sec.append(secOn, document.createTextNode(" Section"));
  const secKind = selectEl([]); secKind.className = "cx-in cx-in-sm"; secKind.setAttribute("aria-label", "Section plane orientation"); secKind.dataset.role = "section-kind";
  Object.entries(CX_SECTION_PRESETS).forEach(([k, p]) => { const o = document.createElement("option"); o.value = k; o.textContent = p.label; secKind.append(o); });
  const secOff = document.createElement("input"); secOff.type = "range"; secOff.min = "0"; secOff.max = "1000"; secOff.value = "500"; secOff.className = "cx-range"; secOff.setAttribute("aria-label", "Section plane position"); secOff.dataset.role = "section-offset";
  const applySection = () => {
    const ir = cx.geometry && cx.geometry.ir; if (!ir) return;
    const p = CX_SECTION_PRESETS[secKind.value] || CX_SECTION_PRESETS["across-wall"];
    const b = ir.bounds, f = Number(secOff.value) / 1000;
    const lo = [b[0], b[1], b[2]], hi = [b[3], b[4], b[5]];
    const center = [(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2];
    const n = p.normal, nl = Math.hypot(n[0], n[1], n[2]);
    const nn = [n[0] / nl, n[1] / nl, n[2] / nl];
    // slide the plane through the detail along its normal
    const span = Math.abs(nn[0]) * (hi[0] - lo[0]) + Math.abs(nn[1]) * (hi[1] - lo[1]) + Math.abs(nn[2]) * (hi[2] - lo[2]);
    const d = (f - 0.5) * span;
    cx.vs.section = { originMm: [center[0] + nn[0] * d, center[1] + nn[1] * d, center[2] + nn[2] * d].map((x) => Math.round(x * 10) / 10), normal: nn.map((x) => Math.round(x * 1e6) / 1e6), up: p.up, enabled: secOn.checked };
    cx.vs.sectionPreset = secKind.value;
    cxApplyVS();
  };
  secOn.onchange = applySection; secKind.onchange = applySection; secOff.oninput = applySection;
  const exp = document.createElement("input"); exp.type = "range"; exp.min = "0"; exp.max = "100"; exp.value = "0"; exp.className = "cx-range"; exp.setAttribute("aria-label", "Exploded view"); exp.dataset.role = "explode";
  exp.oninput = () => { cx.vs.exploded = Number(exp.value) / 100; cxApplyVS(); };
  const expLab = el("label", "cx-tool-check", "Explode ");
  expLab.append(exp);
  const water = el("label", "cx-tool-check"); const wIn = document.createElement("input"); wIn.type = "checkbox"; wIn.dataset.role = "water"; wIn.setAttribute("aria-label", "Water paths overlay");
  water.append(wIn, document.createTextNode(" Water"));
  wIn.onchange = () => { cx.vs.overlays = { ...cx.vs.overlays, water: wIn.checked }; cxApplyVS(); };
  const att = el("label", "cx-tool-check"); const aIn = document.createElement("input"); aIn.type = "checkbox"; aIn.dataset.role = "attachment"; aIn.setAttribute("aria-label", "Attachment paths overlay");
  att.append(aIn, document.createTextNode(" Fixings"));
  aIn.onchange = () => { cx.vs.overlays = { ...cx.vs.overlays, attachment: aIn.checked }; cxApplyVS(); };
  const vis = el("div", "cx-seg");
  vis.append(
    cxToolBtn("Hide", "Hide the selected part", () => { if (cx.selection) { cx.vs.hidden = [...new Set([...(cx.vs.hidden || []), cx.selection])]; cxApplyVS(); } }),
    cxToolBtn("Isolate", "Show only the selected part", () => { if (cx.selection) { cx.vs.isolated = [cx.selection]; cxApplyVS(); } }),
    cxToolBtn("See-through", "Make the selected part transparent", () => { if (cx.selection) { const t = new Set(cx.vs.transparent || []); t.has(cx.selection) ? t.delete(cx.selection) : t.add(cx.selection); cx.vs.transparent = [...t]; cxApplyVS(); } }),
    cxToolBtn("Show all", "Show every part", () => { cx.vs.hidden = []; cx.vs.isolated = []; cx.vs.transparent = []; cxApplyVS(); }));
  const measure = cxToolBtn("Measure", "Measure between two points (canonical millimetres, unaffected by explode)", async () => {
    if (!cx.renderer) return;
    measure.classList.add("on"); cxModelNote("Click two points on the model…");
    const pts = await cx.renderer.measure();
    measure.classList.remove("on");
    const d = Math.hypot(pts[1][0] - pts[0][0], pts[1][1] - pts[0][1], pts[1][2] - pts[0][2]);
    cx.vs.measurements = [...(cx.vs.measurements || []).slice(-7), { a: pts[0], b: pts[1], distanceMm: Math.round(d * 10) / 10, label: "" }];
    cxModelNote("Measured " + (Math.round(d * 10) / 10) + " mm (canonical coordinates)");
    cxQueueViewSave();
  });
  const bm = cxToolBtn("Bookmark", "Save this camera as a bookmark", () => {
    if (!cx.renderer) return;
    cx.vs.bookmarks = [...(cx.vs.bookmarks || []).slice(-15), { name: "View " + ((cx.vs.bookmarks || []).length + 1), camera: cx.renderer.getCamera() }];
    cxQueueViewSave(); cxSyncToolbar(tb.parentElement);
  });
  const bms = selectEl([]); bms.className = "cx-in cx-in-sm"; bms.setAttribute("aria-label", "Camera bookmarks"); bms.dataset.role = "bookmarks";
  bms.onchange = () => { const b = (cx.vs.bookmarks || [])[Number(bms.value)]; if (b && cx.renderer) cx.renderer.setCamera(b.camera); bms.value = ""; };
  const sec2d = cxToolBtn("2D section", "Show the true section drawing of this revision", () => cxToggleSection2D(!cx.section2D));
  tb.append(mode, proj, views, sec, secKind, secOff, expLab, water, att, vis, measure, bm, bms, sec2d, el("span", "cx-model-note"));
  return tb;
}

function cxModelNote(t) { const n = cx.host && cx.host.querySelector(".cx-model-note"); if (n) n.textContent = t || ""; }

function cxSyncToolbar(wrap) {
  if (!wrap) return;
  const vs = cx.vs || {};
  wrap.querySelectorAll(".cx-seg [data-mode]").forEach((b) => b.classList.toggle("on", b.dataset.mode === (vs.mode || "technical")));
  const proj = wrap.querySelector('[data-role="projection"]');
  if (proj) proj.textContent = cx.renderer && cx.renderer.projection() === "orthographic" ? "Perspective" : "Ortho";
  const q = (r) => wrap.querySelector('[data-role="' + r + '"]');
  if (q("section")) q("section").checked = !!(vs.section && vs.section.enabled);
  if (q("section-kind") && vs.sectionPreset) q("section-kind").value = vs.sectionPreset;
  if (q("explode")) q("explode").value = String(Math.round((vs.exploded || 0) * 100));
  if (q("water")) q("water").checked = !!(vs.overlays || {}).water;
  if (q("attachment")) q("attachment").checked = !!(vs.overlays || {}).attachment;
  const bms = q("bookmarks");
  if (bms) {
    bms.innerHTML = "";
    const o0 = document.createElement("option"); o0.value = ""; o0.textContent = (vs.bookmarks || []).length ? "Bookmarks…" : "No bookmarks"; bms.append(o0);
    (vs.bookmarks || []).forEach((b, i) => { const o = document.createElement("option"); o.value = String(i); o.textContent = b.name; bms.append(o); });
  }
}

function cxApplyVS() {
  if (cx.renderer) cx.renderer.setState({ mode: cx.vs.mode, exploded: cx.vs.exploded, hidden: cx.vs.hidden, isolated: cx.vs.isolated, transparent: cx.vs.transparent, section: cx.vs.section, overlays: cx.vs.overlays });
  const wrap = cx.host && cx.host.querySelector(".cx-model");
  cxSyncToolbar(wrap);
  cxQueueViewSave();
}

// ---- inspector (C) ----------------------------------------------------------
function cxPaintInspector(body) {
  body.innerHTML = "";
  const v = cx.view, a = cxAsm(), ro = !!v.readOnly;
  if (!a) { body.append(el("p", "cx-empty", "No assembly yet.")); return; }
  // alternative switcher
  const top = el("div", "cx-insp-top");
  const sel = selectEl([]); sel.className = "cx-in"; sel.setAttribute("aria-label", "Alternative");
  (v.problem.alternatives || []).forEach((id) => {
    const alt = v.assemblies[id]; if (!alt) return;
    const o = document.createElement("option"); o.value = id; o.textContent = alt.name + " · " + alt.applicability.status; sel.append(o);
  });
  sel.value = a.id;
  sel.onchange = () => { cx.activeAssembly = sel.value; cx.selection = ""; cx.geometry = null; cxRender(); cxQueueViewSave(); };
  const newV = pillLight("New variant", () => {
    const name = prompt("Name for the new alternative:", a.name + " (variant)");
    if (name) cxCommand([{ op: "CreateVariant", newAssemblyId: cxNewId("asm"), name }], { assembly: a.id });
  });
  newV.disabled = ro;
  top.append(sel, newV);
  body.append(top);
  const ap = a.applicability || {};
  body.append(el("div", "cx-applic cx-applic-" + (ap.status || "unknown"), "Applicability: " + (ap.status || "unknown") + (ap.conditions && ap.conditions.length ? " — conditions: " + ap.conditions.join(", ") : "") + (ap.reasons && ap.reasons.length ? " — " + ap.reasons.join("; ") : "")));
  body.append(cxJunctionBlock(a, ro));
  body.append(cxParamsBlock(a, ro));
  body.append(cxTreeBlock(a, ro));
  const c = cxComp(cx.selection);
  if (c) body.append(cxComponentBlock(a, c, ro));
  else body.append(el("p", "cx-hint", "Select a part in the model or the list to inspect and edit it."));
}

function cxJunctionBlock(a, ro) {
  const j = a.junction;
  const blk = el("div", "cx-block cx-junction");
  blk.append(el("div", "cx-label micro-label", "Junction · " + j.type));
  const row1 = el("div", "cx-row-edit");
  const ori = selectEl(["unresolved", "headwall", "sidewall"]); ori.value = j.orientation; ori.className = "cx-in"; ori.setAttribute("aria-label", "Junction orientation");
  const strat = selectEl([]); strat.className = "cx-in"; strat.setAttribute("aria-label", "Flashing strategy");
  const fill = () => { strat.innerHTML = ""; (CX_STRATEGIES[ori.value] || []).forEach((s) => { const o = document.createElement("option"); o.value = s; o.textContent = s; strat.append(o); }); if ((CX_STRATEGIES[ori.value] || []).includes(j.strategy)) strat.value = j.strategy; };
  fill(); ori.onchange = fill;
  const apply = pillLight("Apply", () => {
    const ids = {};
    if (ori.value === "sidewall") ids["sidewall-flashing"] = cxNewId("cmp");
    if (ori.value === "headwall") ids["apron"] = cxNewId("cmp");
    if (strat.value === "apron-through-wall-flashing") { ids["through-wall"] = cxNewId("cmp"); ids["weeps"] = cxNewId("cmp"); ids["end-dams"] = cxNewId("cmp"); }
    cxCommand([{ op: "SetJunctionStrategy", orientation: ori.value, strategy: strat.value, newComponentIds: ids }], { assembly: a.id });
  });
  apply.disabled = ro;
  row1.append(ori, strat, apply);
  blk.append(row1);
  const row2 = el("div", "cx-row-edit");
  const wall = selectEl(["unknown", "solid-bonded", "cavity", "other"]); wall.value = j.wallCondition.value; wall.className = "cx-in"; wall.setAttribute("aria-label", "Wall condition");
  const prov = selectEl(["unknown", "user-assumption", "verified-fact"]); prov.value = j.wallCondition.provenance; prov.className = "cx-in"; prov.setAttribute("aria-label", "Wall condition provenance");
  const cav = inputEl("cavity mm (blank = unknown)"); cav.className = "cx-in cx-in-num"; cav.setAttribute("aria-label", "Cavity width in mm");
  wall.onchange = () => { if (wall.value === "unknown") prov.value = "unknown"; else if (prov.value === "unknown") prov.value = "user-assumption"; };
  const setWall = pillLight("Set wall", () => {
    const op = { op: "SetWallCondition", value: wall.value, provenance: wall.value === "unknown" ? "unknown" : prov.value };
    if (wall.value === "cavity") { op.newComponentIds = { cavity: cxNewId("cmp") }; if (cav.value.trim()) { op.cavityWidth = Number(cav.value); op.cavityUnit = "mm"; } }
    cxCommand([op], { assembly: a.id });
  });
  setWall.disabled = ro;
  row2.append(wall, prov, cav, setWall);
  blk.append(row2);
  blk.append(el("p", "cx-hint", "Wall: " + j.wallCondition.value + " (" + j.wallCondition.provenance + ")" + (j.wallCondition.note ? " — " + j.wallCondition.note : "") + ". Two wythes never establish a cavity."));
  return blk;
}

// cxNumberEditor: value + unit, committed on Enter/blur as a typed command.
function cxNumberEditor(label, q, unitKind, onCommit, ro) {
  const row = el("div", "cx-num-row");
  row.append(el("span", "cx-num-label", label));
  const input = document.createElement("input");
  input.type = "text"; input.inputMode = "decimal"; input.className = "cx-in cx-in-num"; input.value = cxFmt(cxQ(q)); input.setAttribute("aria-label", label);
  input.disabled = ro;
  let unit = null;
  if (unitKind === "mm") { unit = selectEl(CX_UNITS); unit.className = "cx-in cx-in-unit"; unit.setAttribute("aria-label", label + " unit"); unit.disabled = ro; }
  else row.dataset.unit = unitKind;
  const tag = el("span", "cx-chip micro-label", cxQLabel(q) || "");
  const commit = () => {
    const raw = input.value.trim();
    if (raw === cxFmt(cxQ(q)) && (!unit || unit.value === "mm")) return;
    if (raw === "" || raw === "?") { onCommit(null, unit ? unit.value : unitKind); return; }
    const n = Number(raw);
    if (!isFinite(n)) { input.setCustomValidity("Enter a number"); input.reportValidity(); return; }
    onCommit(n, unit ? unit.value : unitKind);
  };
  input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); commit(); } if (e.key === "Escape") { input.value = cxFmt(cxQ(q)); input.blur(); } });
  input.addEventListener("change", commit);
  row.append(input);
  if (unit) row.append(unit); else row.append(el("span", "cx-unit-fixed micro-label", unitKind));
  row.append(tag);
  return row;
}

function cxParamsBlock(a, ro) {
  const blk = el("div", "cx-block");
  blk.append(el("div", "cx-label micro-label", "Assembly parameters"));
  const P = a.parameters || {};
  blk.append(cxNumberEditor("Pitch", P.pitch, "deg", (v) => v != null && cxCommand([{ op: "SetPitch", value: v, unit: "deg" }], { assembly: a.id }), ro));
  for (const [k, l] of [["widthAlongWall", "Width along wall"], ["depthFromWall", "Depth from wall"], ["wallHeightAbove", "Wall above datum"], ["wallDepthBelow", "Wall below rafters"]]) {
    blk.append(cxNumberEditor(l, P[k], "mm", (v, u) => cxCommand([{ op: "SetDimension", parameter: k, value: v, unit: u }], { assembly: a.id }), ro));
  }
  return blk;
}

function cxIssueCounts() {
  const rep = cxReport(), out = {};
  if (!rep) return out;
  for (const is of rep.issues || []) {
    if (is.status === "resolved") continue;
    for (const id of is.componentIds || []) { out[id] = out[id] || { critical: 0, advisory: 0 }; if (is.severity === "critical-unresolved") out[id].critical++; else if (is.severity === "advisory") out[id].advisory++; }
  }
  return out;
}

function cxTreeBlock(a, ro) {
  const blk = el("div", "cx-block cx-tree");
  blk.append(el("div", "cx-label micro-label", "Components (ordered)"));
  const counts = cxIssueCounts();
  const layers = (a.components || []).filter((c) => c.layer).sort((x, y) => x.layer.order - y.layer.order);
  const row = (c, i) => {
    const r = el("div", "cx-tree-row" + (c.id === cx.selection ? " sel" : "") + (c.applicability === "inapplicable" ? " off" : ""));
    r.tabIndex = 0; r.dataset.component = c.id;
    r.setAttribute("role", "button");
    r.setAttribute("aria-pressed", String(c.id === cx.selection));
    r.append(el("span", "cx-tree-name", (c.layer ? c.layer.order + ". " : "") + c.name));
    const n = counts[c.id];
    if (n && n.critical) r.append(el("span", "cx-badge cx-badge-crit", String(n.critical)));
    if (c.applicability !== "applicable") r.append(el("span", "cx-chip micro-label", c.applicability));
    if (c.layer && !ro && i !== undefined) {
      const mv = (d) => (e) => { e.stopPropagation(); const ids = layers.map((x) => x.id); const j = i + d; if (j < 0 || j >= ids.length) return; [ids[i], ids[j]] = [ids[j], ids[i]]; cxCommand([{ op: "SetLayerOrder", componentIds: ids }], { assembly: a.id }); };
      const upB = el("button", "cx-x", "↑"); upB.type = "button"; upB.title = "Move layer up"; upB.setAttribute("aria-label", "Move " + c.name + " up"); upB.onclick = mv(1);
      const dnB = el("button", "cx-x", "↓"); dnB.type = "button"; dnB.title = "Move layer down"; dnB.setAttribute("aria-label", "Move " + c.name + " down"); dnB.onclick = mv(-1);
      r.append(upB, dnB);
    }
    r.onclick = () => cxSelect(c.id);
    r.onkeydown = (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); cxSelect(c.id); } };
    return r;
  };
  blk.append(el("div", "cx-tree-group micro-label", "Roof stack (top = last)"));
  layers.slice().reverse().forEach((c) => blk.append(row(c, layers.indexOf(c))));
  for (const [g, types] of CX_TYPE_GROUPS) {
    const items = (a.components || []).filter((c) => !c.layer && types.includes(c.type));
    if (!items.length) continue;
    blk.append(el("div", "cx-tree-group micro-label", g));
    items.forEach((c) => blk.append(row(c)));
  }
  if ((a.tombstones || []).length) blk.append(el("div", "cx-hint", "Removed: " + a.tombstones.map((t) => t.name).join(", ")));
  return blk;
}

function cxComponentBlock(a, c, ro) {
  const v = cx.view;
  const blk = el("div", "cx-block cx-comp");
  blk.append(el("div", "cx-label micro-label", "Selected part"));
  blk.append(el("div", "cx-comp-name", c.name));
  blk.append(el("div", "cx-hint", c.type + " · " + c.role + " · " + c.applicability));
  // dimensions
  const specs = Object.keys(c.shape.params || {}).sort();
  specs.forEach((k) => {
    const q = c.shape.params[k];
    blk.append(cxNumberEditor(k, q, q.unit === "mm" ? "mm" : q.unit, (val, u) => cxCommand([{ op: "SetDimension", componentId: c.id, dimension: k, value: val, unit: u }], { assembly: a.id }), ro));
  });
  // direct handle: tentative preview → commit / cancel
  const primary = ["thickness", "depth", "length", "upstand", "lap"].find((k) => specs.includes(k));
  if (primary && !ro) blk.append(cxHandle(a, c, primary));
  // material / product / appearance
  const cat = v.catalog || { materials: [], products: [] };
  const matSel = selectEl([]); matSel.className = "cx-in"; matSel.setAttribute("aria-label", "Material");
  cat.materials.forEach((m) => { const o = document.createElement("option"); o.value = m.id; o.textContent = m.name + (m.generic ? " (generic)" : ""); matSel.append(o); });
  if (c.material) matSel.value = c.material.id;
  matSel.disabled = ro;
  matSel.onchange = () => cxCommand([{ op: "SetMaterial", componentId: c.id, materialId: matSel.value }], { assembly: a.id });
  const r1 = el("div", "cx-num-row"); r1.append(el("span", "cx-num-label", "Material"), matSel);
  blk.append(r1);
  const mat = c.material && cat.materials.find((m) => m.id === c.material.id);
  if (mat) blk.append(el("div", "cx-hint", "Pinned revision " + c.material.revision + " · unknown: " + ((mat.unknowns || []).join(", ") || "none") + ((mat.limits || []).length ? " · " + mat.limits.join("; ") : "")));
  const prodSel = selectEl([]); prodSel.className = "cx-in"; prodSel.setAttribute("aria-label", "Product");
  const none = document.createElement("option"); none.value = ""; none.textContent = "No product (generic)"; prodSel.append(none);
  const current = new Map();
  (cat.products || []).forEach((p) => { const have = current.get(p.id); if (!have || p.revision > have.revision) current.set(p.id, p); });
  current.forEach((p) => { const o = document.createElement("option"); o.value = p.id; o.textContent = p.manufacturer + " " + p.model + (p.fictional ? " (fictional fixture)" : "") + " · " + p.lifecycle; prodSel.append(o); });
  prodSel.value = c.product ? c.product.id : "";
  prodSel.disabled = ro;
  const subBox = el("div", "cx-subst");
  prodSel.onchange = () => {
    if (!prodSel.value || typeof cxSubstitutionPreview !== "function") cxCommand([{ op: "SetProduct", componentId: c.id, productId: prodSel.value }], { assembly: a.id });
    else cxSubstitutionPreview(a, c, prodSel.value, subBox, () => { prodSel.value = c.product ? c.product.id : ""; });
  };
  const r2 = el("div", "cx-num-row"); r2.append(el("span", "cx-num-label", "Product"), prodSel);
  blk.append(r2, subBox);
  if (c.product) {
    const pinned = (cat.products || []).find((p) => p.id === c.product.id && p.revision === c.product.revision);
    const head = current.get(c.product.id);
    if (pinned) blk.append(el("div", "cx-hint", "Pinned " + pinned.manufacturer + " " + pinned.model + " revision " + pinned.revision + " · " + pinned.facts.filter((f) => f.verified).length + " of " + pinned.facts.length + " facts verified · " + pinned.lifecycle + (head && head.revision !== pinned.revision ? " · catalog has revision " + head.revision + " (not followed)" : "")));
  }
  const apSel = selectEl(["timber-rafter", "timber-decking", "timber-batten", "membrane-avcl", "membrane-underlayment", "insulation-rigid", "metal-corrugated", "metal-standing-seam", "metal-flashing", "closure-foam", "sealant-generic", "fastener-steel", "masonry-brick", "mortar-generic", "sheathing-generic", "cavity-air"]);
  apSel.className = "cx-in"; apSel.setAttribute("aria-label", "Appearance (visual only)"); apSel.value = c.appearance || ""; apSel.disabled = ro;
  apSel.onchange = () => cxCommand([{ op: "SetAppearance", componentId: c.id, appearance: apSel.value }], { assembly: a.id });
  const r3 = el("div", "cx-num-row"); r3.append(el("span", "cx-num-label", "Appearance"), apSel);
  blk.append(r3);
  // supported positioning
  if (CX_MOVABLE.has(c.type) && !ro) {
    const t = (c.transform && c.transform.translation) || [0, 0, 0];
    const r4 = el("div", "cx-num-row"); r4.append(el("span", "cx-num-label", "Offset x/y/z mm"));
    const ins = [0, 1, 2].map((i) => { const x = document.createElement("input"); x.type = "text"; x.inputMode = "decimal"; x.className = "cx-in cx-in-num"; x.value = cxFmt(t[i]); x.setAttribute("aria-label", "Offset " + "xyz"[i] + " in mm"); return x; });
    const go = pillLight("Move", () => cxCommand([{ op: "SetTransform", componentId: c.id, translation: ins.map((x) => Number(x.value) || 0), unit: "mm" }], { assembly: a.id }));
    r4.append(...ins, go);
    blk.append(r4);
  }
  const host = c.hostId && cxComp(c.hostId);
  if (host) { const h = el("button", "cx-link", "Host: " + host.name); h.type = "button"; h.onclick = () => cxSelect(host.id); blk.append(h); }
  // this part's issues
  const rep = cxReport();
  const mine = rep ? (rep.issues || []).filter((is) => is.status !== "resolved" && (is.componentIds || []).includes(c.id)) : [];
  if (mine.length) {
    blk.append(el("div", "cx-label micro-label", "Issues on this part"));
    mine.forEach((is) => blk.append(cxIssueRow(is, a, true)));
  }
  // this part's evidence
  const links = (a.evidenceLinks || []).filter((l) => l.target === c.id);
  if (links.length && typeof cxEvidenceById === "function") {
    blk.append(el("div", "cx-label micro-label", "Evidence"));
    links.forEach((l) => { const e = cxEvidenceById(l.evidenceId); if (e) blk.append(cxEvidenceRow(e, l.relation)); });
  }
  if (!ro) {
    const rm = armedDelete("Remove part", "Confirm remove (unlinks dependents)", () => cxCommand([{ op: "RemoveComponent", componentId: c.id, dependents: "detach" }], { assembly: a.id }).then(() => cxSelect("")));
    blk.append(rm);
  }
  return blk;
}

// cxHandle: a direct manipulation for the primary dimension. Dragging shows a
// server-computed TENTATIVE preview (nothing written); Commit sends the same
// typed command; Cancel discards it.
function cxHandle(a, c, dim) {
  const q = c.shape.params[dim];
  const spec = { thickness: [1, 400], depth: [10, 400], length: [10, 400], upstand: [50, 400], lap: [25, 200] }[dim] || [1, 400];
  const wrap = el("div", "cx-handle");
  wrap.append(el("span", "cx-num-label", "Handle: " + dim));
  const range = document.createElement("input");
  range.type = "range"; range.min = String(spec[0]); range.max = String(spec[1]); range.step = "1"; range.value = String(Math.round(cxQ(q) || spec[0]));
  range.className = "cx-range"; range.setAttribute("aria-label", "Drag to preview " + dim + " (tentative until committed)");
  const val = el("span", "cx-handle-val micro-label", range.value + " mm");
  const state = el("span", "cx-handle-state micro-label", "");
  const commit = pillLight("Commit", () => { cxCommand([{ op: "SetDimension", componentId: c.id, dimension: dim, value: Number(range.value), unit: "mm" }], { assembly: a.id }); });
  const cancel = pillLight("Cancel", () => { range.value = String(Math.round(cxQ(q) || spec[0])); val.textContent = range.value + " mm"; state.textContent = ""; commit.disabled = true; cancel.disabled = true; if (cx.renderer && cx.geometry) cx.renderer.setGeometry(cx.geometry.ir, true); });
  commit.disabled = true; cancel.disabled = true;
  let seq = 0;
  const preview = debounce(async () => {
    const my = ++seq;
    const body = { schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId, assemblyId: a.id, expectedAssemblyRevision: cx.view.revisions["assembly:" + a.id],
      operations: [{ op: "SetDimension", componentId: c.id, dimension: dim, value: Number(range.value), unit: "mm" }] };
    try {
      const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/preview?geometry=1", body);
      if (my !== seq) return;
      const p = res.preview;
      const crit = (p.findings || []).filter((f) => f.severity === "critical-unresolved").length;
      state.textContent = "TENTATIVE · " + (p.blocking ? "blocked: " + ((p.problems || []).join("; ") || "blocking finding") : crit + " critical issues");
      commit.disabled = !!p.blocking; cancel.disabled = false;
      if (res.geometry && cx.renderer) cx.renderer.setGeometry(res.geometry, true);
    } catch (e) { state.textContent = "Preview failed: " + e.message; }
  }, 250);
  range.oninput = () => { val.textContent = range.value + " mm"; state.textContent = "TENTATIVE · previewing…"; preview(); };
  wrap.append(range, val, state, commit, cancel);
  return wrap;
}

function cxIssueRow(is, a, compact) {
  const r = el("div", "cx-issue cx-issue-" + is.severity + (is.status === "acknowledged" ? " ack" : ""));
  r.append(el("span", "cx-issue-sev micro-label", is.severity.replace("-unresolved", "")), el("span", "cx-issue-msg", is.message));
  if (!compact) r.append(el("span", "cx-issue-meta micro-label", is.ruleKey + " · " + is.status + (is.specialistReview ? " · specialist review" : "")));
  if (is.status === "acknowledged" && is.resolution) r.append(el("span", "cx-issue-meta micro-label", "acknowledged: " + is.resolution.reason));
  if (!compact && is.status === "open" && is.severity !== "blocking" && !cx.view.readOnly) {
    const ack = el("button", "cx-link", "Acknowledge"); ack.type = "button";
    ack.onclick = () => { const reason = prompt("Acknowledge (this never validates the engineering) — reason:"); if (reason) cxCommand([{ op: "AcknowledgeIssue", ruleKey: is.ruleKey, target: is.target, reason }], { assembly: a.id }); };
    r.append(ack);
  }
  if ((is.componentIds || []).length) r.onclick = (e) => { if (e.target.tagName !== "BUTTON") cxSelect(is.componentIds[0]); };
  return r;
}

// ---- research tabs (D): alternatives, issues, history -----------------------
cxResearchTabs.push(["alternatives", "Alternatives", (b) => cxPaintAlternatives(b)]);
cxResearchTabs.push(["issues", "Issues", (b) => cxPaintIssues(b)]);
cxResearchTabs.push(["history", "History", (b) => cxPaintHistory(b)]);

function cxAltFacts(id) {
  const a = cx.view.assemblies[id], rep = (cx.view.validation || {})[id];
  const ins = a && (a.components || []).find((c) => c.type === "insulation-board");
  const counts = rep ? rep.counts : {};
  return { a, rep, ins: ins ? cxQ(ins.shape.params.thickness) : null, critical: counts.critical || 0, advisory: counts.advisory || 0 };
}

function cxPaintAlternatives(b) {
  const v = cx.view;
  const ids = v.problem.alternatives || [];
  if (!ids.length) { b.append(el("p", "cx-empty", "No alternatives yet.")); return; }
  const table = el("table", "cx-compare");
  const head = el("tr");
  ["Alternative", "Junction", "Wall", "Applicability", "Critical", "Insulation", "Model", ""].forEach((h) => head.append(el("th", "micro-label", h)));
  table.append(head);
  ids.forEach((id) => {
    const f = cxAltFacts(id); if (!f.a) return;
    const tr = el("tr", id === cx.activeAssembly ? "on" : "");
    tr.append(el("td", null, f.a.name), el("td", null, f.a.junction.orientation + " / " + f.a.junction.strategy), el("td", null, f.a.junction.wallCondition.value + " (" + f.a.junction.wallCondition.provenance + ")"),
      el("td", "cx-applic-" + f.a.applicability.status, f.a.applicability.status), el("td", null, String(f.critical)), el("td", null, cxFmt(f.ins) + " mm"), el("td", "micro-label", (f.a.modelHash || "").slice(0, 10)));
    const td = el("td");
    if (id !== cx.activeAssembly) { const o = el("button", "cx-link", "Open"); o.type = "button"; o.onclick = () => { cx.activeAssembly = id; cx.selection = ""; cx.geometry = null; cxRender(); cxQueueViewSave(); }; td.append(o); }
    else td.append(el("span", "micro-label", "open"));
    tr.append(td);
    table.append(tr);
  });
  b.append(table);
  // side-by-side difference of the open alternative against another
  const others = ids.filter((id) => id !== cx.activeAssembly);
  if (others.length) {
    const pick = selectEl([]); pick.className = "cx-in"; pick.setAttribute("aria-label", "Compare the open alternative with");
    others.forEach((id) => { const o = document.createElement("option"); o.value = id; o.textContent = "Compare with " + v.assemblies[id].name; pick.append(o); });
    const out = el("div", "cx-diff");
    const run = () => { out.innerHTML = ""; cxDiffList(v.assemblies[pick.value], cxAsm()).forEach((line) => out.append(el("div", "cx-diff-line cx-diff-" + line.kind, line.text))); };
    pick.onchange = run;
    b.append(pick, out);
    run();
  }
}

// cxDiffList compares two assemblies by stable ids: parameters, components
// added/removed, applicability, materials/products, junction.
function cxDiffList(from, to) {
  const out = [];
  if (!from || !to) return out;
  const jf = from.junction, jt = to.junction;
  if (jf.orientation !== jt.orientation || jf.strategy !== jt.strategy) out.push({ kind: "chg", text: "Junction: " + jf.orientation + "/" + jf.strategy + " → " + jt.orientation + "/" + jt.strategy });
  if (jf.wallCondition.value !== jt.wallCondition.value) out.push({ kind: "chg", text: "Wall: " + jf.wallCondition.value + " → " + jt.wallCondition.value });
  for (const k of Object.keys(to.parameters || {})) { const a = cxQ(from.parameters[k]), b = cxQ(to.parameters[k]); if (a !== b) out.push({ kind: "chg", text: k + ": " + cxFmt(a) + " → " + cxFmt(b) }); }
  const fm = new Map((from.components || []).map((c) => [c.id, c]));
  const tm = new Map((to.components || []).map((c) => [c.id, c]));
  for (const [id, c] of tm) {
    const o = fm.get(id);
    if (!o) { out.push({ kind: "add", text: "+ " + c.name }); continue; }
    if (o.applicability !== c.applicability) out.push({ kind: "chg", text: c.name + ": " + o.applicability + " → " + c.applicability });
    for (const k of Object.keys(c.shape.params || {})) { const a = cxQ(o.shape.params[k]), b = cxQ(c.shape.params[k]); if (a !== b) out.push({ kind: "chg", text: c.name + " · " + k + ": " + cxFmt(a) + " → " + cxFmt(b) }); }
    if (JSON.stringify(o.material) !== JSON.stringify(c.material)) out.push({ kind: "chg", text: c.name + ": material changed" });
    if (JSON.stringify(o.product) !== JSON.stringify(c.product)) out.push({ kind: "chg", text: c.name + ": product " + (o.product ? o.product.id : "none") + " → " + (c.product ? c.product.id : "none") });
  }
  for (const [id, c] of fm) if (!tm.has(id)) out.push({ kind: "del", text: "− " + c.name });
  if (from.modelHash !== to.modelHash) out.push({ kind: "info", text: "Geometry " + (from.modelHash || "").slice(0, 10) + " → " + (to.modelHash || "").slice(0, 10) });
  if (!out.length) out.push({ kind: "info", text: "No differences." });
  return out;
}

function cxPaintIssues(b) {
  const rep = cxReport(), a = cxAsm();
  if (!rep) { b.append(el("p", "cx-empty", "No validation report yet.")); return; }
  b.append(el("p", "cx-hint", "Rule set " + rep.ruleSet + " · revision " + (rep.assemblyRevision || "").slice(0, 10) + " · " + rep.counts.critical + " critical · " + rep.counts.advisory + " advisory · geometry validity is not physical approval."));
  const onlySel = el("label", "cx-check");
  const cb = document.createElement("input"); cb.type = "checkbox"; cb.checked = !!cx.issuesOnlySel;
  cb.onchange = () => { cx.issuesOnlySel = cb.checked; cxPaintResearch(b.closest(".cx-pane-body")); };
  onlySel.append(cb, document.createTextNode(" Only the selected part"));
  b.append(onlySel);
  const order = ["critical-unresolved", "advisory", "informational"];
  const issues = (rep.issues || []).filter((is) => !cx.issuesOnlySel || (is.componentIds || []).includes(cx.selection))
    .sort((x, y) => order.indexOf(x.severity) - order.indexOf(y.severity));
  issues.forEach((is) => b.append(cxIssueRow(is, a, false)));
  if (!issues.length) b.append(el("p", "cx-empty", "No issues for this selection."));
}

async function cxPaintHistory(b) {
  b.append(el("p", "cx-hint", "Every change is a new revision; nothing is rewound or deleted. Undo restores the previous revision as a new one."));
  const a = cxAsm();
  if (a && !cx.view.readOnly) {
    const row = el("div", "cx-row-edit");
    const undo = pillLight("Undo last edit", () => cxUndo(a));
    const redo = pillLight("Redo", () => cxRedo(a));
    undo.disabled = !a.parentRevision; redo.disabled = !(cx.redo || []).length;
    row.append(undo, redo);
    b.append(row);
  }
  const list = el("div", "cx-history");
  list.append(el("p", "cx-empty", "Loading…"));
  b.append(list);
  try {
    const res = await cxApi("GET", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId) + "/history");
    list.innerHTML = "";
    (res.history || []).slice(0, 60).forEach((rc) => {
      const r = el("div", "cx-hist-row" + (rc.viewOnly ? " view-only" : ""));
      r.append(el("span", "cx-hist-gen micro-label", "#" + rc.resultGeneration), el("span", "cx-hist-ops", (rc.operations || []).map((o) => o.op).join(", ") || rc.summary),
        el("span", "cx-hist-actor micro-label", rc.actor.principal + (rc.viewOnly ? " · view" : "")), el("span", "cx-hist-at micro-label", (rc.createdAt || "").slice(0, 19).replace("T", " ")));
      list.append(r);
    });
  } catch (e) { list.innerHTML = ""; list.append(el("p", "cx-empty", "Couldn't load history: " + e.message)); }
}

// Undo = RestoreRevision of the parent (a new revision); redo restores the
// undone revision, but only while nothing else changed (else: conflict).
async function cxUndo(a) {
  if (!a.parentRevision) return;
  const cur = cx.view.revisions["assembly:" + a.id];
  const res = await cxCommand([{ op: "RestoreRevision", revision: a.parentRevision }], { assembly: a.id, label: "Undo" });
  if (res) { cx.redo = [...(cx.redo || []), { assembly: a.id, revision: cur, after: res.view.revisions["assembly:" + a.id] }]; cxRender(); }
}
async function cxRedo(a) {
  const r = (cx.redo || []).pop();
  if (!r) return;
  if (cx.view.revisions["assembly:" + a.id] !== r.after) { cx.error = "Redo needs review: the assembly changed since the undo."; cxPaintStatus(); return; }
  await cxCommand([{ op: "RestoreRevision", revision: r.revision }], { assembly: a.id, label: "Redo" });
}

// ---- agent pane (A): steward, open questions, research ----------------------
function cxPaintAgentFull(body) {
  body.innerHTML = "";
  const p = cx.view.problem, a = cxAsm(), ro = !!cx.view.readOnly;
  const row = el("div", "cx-field");
  row.append(el("span", "cx-label micro-label", "Steward"));
  const sel = selectEl(["alfred", "zeck"]);
  sel.value = p.steward.agent; sel.className = "cx-in"; sel.setAttribute("aria-label", "Steward agent"); sel.disabled = ro;
  sel.onchange = () => cxCommand([{ op: "SetSteward", agent: sel.value }]);
  row.append(sel);
  body.append(row);
  body.append(el("p", "cx-hint", p.steward.agent === "alfred" ? "Alfred is the default steward." : "Zeck was explicitly selected as the real-estate specialist."));
  body.append(el("p", "cx-hint", "Assigning a steward does not start a conversation or research."));
  // questions only an owner input can settle
  const rep = cxReport();
  const qs = el("div", "cx-block");
  qs.append(el("div", "cx-label micro-label", "Open questions"));
  let n = 0;
  if (a && a.junction.wallCondition.value === "unknown") {
    n++;
    const q = el("div", "cx-question");
    q.append(el("span", null, "Is the wall solid/bonded or a cavity wall? (two wythes do not establish a cavity)"));
    const btns = el("div", "cx-row-edit");
    btns.append(pillLight("Solid/bonded — verified", () => cxCommand([{ op: "SetWallCondition", value: "solid-bonded", provenance: "verified-fact", note: "owner verified" }], { assembly: a.id })),
      pillLight("Cavity — verified", () => cxCommand([{ op: "SetWallCondition", value: "cavity", provenance: "verified-fact", note: "owner verified", newComponentIds: { cavity: cxNewId("cmp") } }], { assembly: a.id })));
    btns.querySelectorAll("button").forEach((x) => { x.disabled = ro; });
    q.append(btns);
    qs.append(q);
  }
  if (a && a.junction.orientation === "unresolved") {
    n++;
    qs.append(el("div", "cx-question", "Does the roof meet the wall at its top edge (headwall) or along its slope (sidewall)? Set it under Junction in the inspector."));
  }
  if (cx.view.problem.climate && cx.view.problem.climate.state === "unknown") {
    n++;
    qs.append(el("div", "cx-question", "Climate is unknown, so air/vapour-control placement stays a critical question (Problem tab)."));
  }
  if (!n) qs.append(el("p", "cx-empty", "No open owner questions."));
  body.append(qs);
  if (typeof cxPaintResearchRun === "function") cxPaintResearchRun(body);
  else body.append(el("p", "cx-empty", "No research run yet."));
  if (rep) body.append(el("p", "cx-hint", rep.counts.critical + " critical issues remain on the open alternative."));
}

// ================= P4 — 2D section view and exports =================

function cxSectionQuery() {
  const s = cx.vs && cx.vs.section;
  const a = cxAsm();
  let q = "revision=" + encodeURIComponent((cx.view.revisions || {})["assembly:" + a.id]);
  if (s && s.enabled) q += "&ox=" + s.originMm[0] + "&oy=" + s.originMm[1] + "&oz=" + s.originMm[2] + "&nx=" + s.normal[0] + "&ny=" + s.normal[1] + "&nz=" + s.normal[2] + "&ux=" + s.up[0] + "&uy=" + s.up[1] + "&uz=" + s.up[2];
  return q + "&paper=" + encodeURIComponent(cx.exportPaper || "A3") + "&scale=" + encodeURIComponent(cx.exportScale || 5);
}

// cxToggleSection2D shows the true section of the current revision as an
// inert image (an <img> cannot run anything inside the SVG).
function cxToggleSection2D(on) {
  const wrap = cx.host && cx.host.querySelector(".cx-model");
  if (!wrap) return;
  let panel = wrap.querySelector(".cx-section2d");
  if (!on) { if (panel) panel.remove(); cx.section2D = false; return; }
  cx.section2D = true;
  if (!panel) {
    panel = el("div", "cx-section2d");
    panel.setAttribute("role", "region");
    panel.setAttribute("aria-label", "2D section drawing");
    wrap.querySelector(".cx-canvas-host").append(panel);
  }
  panel.innerHTML = "";
  const a = cxAsm();
  const img = document.createElement("img");
  img.className = "cx-section-img";
  img.alt = "Section drawing of " + a.name + " at revision " + (cx.view.revisions["assembly:" + a.id] || "").slice(0, 10) + (cx.vs.section && cx.vs.section.enabled ? " (current section plane)" : " (standard section through a rafter)");
  img.src = cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/section?" + cxSectionQuery();
  img.onerror = () => { panel.innerHTML = ""; panel.append(el("p", "cx-empty cx-pad", "This section cannot be drawn at the chosen paper and scale (nothing is fitted to the page). Choose a larger paper or smaller scale under Export.")); };
  const close = cxToolBtn("Close 2D", "Back to the 3D model", () => cxToggleSection2D(false));
  panel.append(close, img);
}

cxResearchTabs.push(["export", "Export", (b) => cxPaintExport(b)]);

function cxPaintExport(b) {
  const a = cxAsm(), v = cx.view;
  if (!a) { b.append(el("p", "cx-empty", "No assembly to export.")); return; }
  const rev = (v.revisions || {})["assembly:" + a.id];
  b.append(el("p", "cx-hint", "Every export is generated from revision " + rev.slice(0, 10) + " of “" + a.name + "” and records its geometry hash. Drawings print at a true scale; nothing is fitted to the page. PNG is not generated server-side."));
  b.append(el("p", "cx-notice", v.notice));
  const row = el("div", "cx-row-edit");
  const paper = selectEl(["A4", "A3", "A2"]); paper.className = "cx-in"; paper.value = cx.exportPaper || "A3"; paper.setAttribute("aria-label", "Paper size");
  const scale = selectEl(["2", "5", "10", "20"]); scale.className = "cx-in"; scale.value = String(cx.exportScale || 5); scale.setAttribute("aria-label", "Drawing scale 1:N");
  paper.onchange = () => { cx.exportPaper = paper.value; };
  scale.onchange = () => { cx.exportScale = Number(scale.value); };
  row.append(el("span", "cx-num-label", "Paper · scale"), paper, scale);
  b.append(row);
  const msg = el("p", "cx-form-msg cx-export-msg", cx.exportMsg || "");
  msg.setAttribute("role", "status");
  const btns = el("div", "cx-row-edit");
  const go = (format, label) => {
    const btn = pillLight(label, async () => {
      btn.disabled = true; msg.textContent = "Exporting " + format + "…";
      const body = { schemaVersion: 1, requestId: cxRequestId(), revision: rev, format, paper: paper.value, scale: Number(scale.value) };
      if (cx.vs && cx.vs.section && cx.vs.section.enabled && (format === "svg" || format === "pdf" || format === "package")) body.section = { originMm: cx.vs.section.originMm, normal: cx.vs.section.normal, up: cx.vs.section.up, enabled: true };
      try {
        const res = await cxApi("POST", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId) + "/assemblies/" + encodeURIComponent(a.id) + "/exports", body);
        cx.exportMsg = msg.textContent = "Exported " + res.record.name + " (" + Math.round(res.record.size / 1024) + " KB, geometry " + res.record.geometryHash.slice(0, 10) + ")";
        cx.lastExport = res;
        const fresh = await cxApi("GET", cxBase(cx.subject), "/problems/" + encodeURIComponent(cx.problemId));
        cxApplyView(fresh);
      } catch (e) { cx.exportMsg = msg.textContent = "Export refused: " + e.message + (e.problems && e.problems.length ? ": " + e.problems.join("; ") : ""); }
      finally { btn.disabled = false; }
    });
    btn.setAttribute("aria-label", "Export " + label);
    return btn;
  };
  btns.append(go("glb", "GLB model"), go("svg", "Section SVG"), go("pdf", "Section PDF"), go("package", "Detail package"),
    pillLight("Show 2D section", () => { cx.pane = "model"; const wb = cx.host.querySelector(".cx-wb"); if (wb) cxPaintSwitch(wb); cxToggleSection2D(true); }));
  b.append(btns, msg);
  const list = el("div", "cx-exports");
  const recs = ((v.derived && v.derived.artifacts) || []).filter((r) => r.assemblyId === a.id).slice().reverse();
  if (!recs.length) list.append(el("p", "cx-empty", "No exports yet."));
  recs.forEach((r) => {
    const row = el("div", "cx-export-row");
    const link = el("a", "cx-input-name", r.name);
    link.href = cxArtifactURL(r.artifactId, r.revision, true);
    link.setAttribute("download", r.name);
    row.append(link, el("span", "cx-input-meta micro-label", r.format + " · rev " + r.assemblyRevision.slice(0, 8) + " · geometry " + r.geometryHash.slice(0, 8) + (r.parameters && r.parameters.scale ? " · " + r.parameters.scale + " " + (r.parameters.paper || "") : "") + (r.assemblyRevision !== rev ? " · older revision" : "")));
    list.append(row);
  });
  b.append(list);
  // the private recovery bundle: the whole retained closure, for backup only
  const bk = el("div", "cx-block cx-backup");
  bk.append(el("div", "cx-label micro-label", "Private recovery bundle"));
  bk.append(el("p", "cx-hint", "Every revision, input, source snapshot, run result, decision, view and export of this problem, with hashes, for backup and restore into an empty root. Private — not a sharing package."));
  const dl = el("a", "cx-input-name cx-backup-link", "Download private recovery bundle");
  dl.href = cxBase(cx.subject) + "/problems/" + encodeURIComponent(cx.problemId) + "/export";
  dl.setAttribute("download", cx.problemId + "-recovery.zip");
  bk.append(dl);
  b.append(bk);
}
