// ================= TODO PANEL =================
// Click a list row or board card → a sticky right panel (the AION-inspector
// idiom: the list never reflows) carrying the todo's DESCRIPTION, its PLAN
// (the system/todo-plans record — agent-writable via the §12 lane), the
// ASSIGNEE row (the roster picker), and the comment THREAD with the
// Comment / Ask ✦ / Do ✦ composer, @-mentions and content-addressed
// attachments. Under 1100px the panel becomes a sheet.

let todoSelId = null;      // selected todo id ("" = none)
let todoPanelData = null;  // last /api/tasks/panel payload
let todoDeepLink = null;   // #/tasks/<id> → open after load
let todoPanelTimer = null; // live refresh while the panel is open
// a composer preset ({mode, focusAgent}) set by openTodoPanel(…, opts) — the
// ⇢ shortcut opens the panel straight into Do mode (agent-chat plan §3.4f);
// consumed by the next composer render.
let todoComposerPreset = null;

// the panel stays LIVE while open: hermes' plan updates and thread replies
// land without a manual reload. Re-render only when the payload actually
// changed, and never while the owner is typing in the panel.
function ensureTodoPanelPoll() {
  if (todoPanelTimer) return;
  todoPanelTimer = setInterval(async () => {
    if (!todoSelId || document.hidden || !els.todosView || els.todosView.hidden) return;
    const host = document.getElementById("todoPanel");
    if (host && host.contains(document.activeElement) &&
        /^(input|textarea)$/i.test(document.activeElement.tagName)) return;
    try {
      const fresh = await (await fetch("/api/tasks/panel?id=" + encodeURIComponent(todoSelId))).json();
      if (fresh.id !== todoSelId) return;
      if (JSON.stringify(fresh) !== JSON.stringify(todoPanelData)) {
        todoPanelData = fresh;
        renderTodoPanel(false);
      }
    } catch (e) {}
  }, 8000);
}

// openTodoPanel — from a row/card ({id, text, ...}) or an id string.
// opts.mode ("comment" | "ask" | "do") presets the composer; opts.focusAgent
// lands the caret on the agent picker.
function openTodoPanel(rOrId, opts) {
  const id = typeof rOrId === "string" ? rOrId : rOrId.id;
  todoSelId = id;
  todoComposerPreset = opts && opts.mode ? opts : null;
  const suffix = "#/tasks/" + encodeURIComponent(id);
  if (location.hash !== suffix) {
    try { history.replaceState(null, "", suffix); } catch (e) {}
  }
  ensureTodoPanelPoll();
  renderTodoPanel(true);
}

function closeTodoPanel() {
  todoSelId = null;
  todoPanelData = null;
  try { history.replaceState(null, "", "#/tasks"); } catch (e) {}
  renderTodoPanel(false);
}

// todoRowInfo — the row's live projection (text/container/owner), if visible.
function todoRowInfo(id) {
  return ((todosCache && todosCache.rows) || []).find((r) => r.id === id) || null;
}

async function renderTodoPanel(refetch) {
  const host = document.getElementById("todoPanel");
  if (!host) return;
  if (!todoSelId) {
    host.hidden = true;
    host.innerHTML = "";
    document.body.classList.remove("tdo-panel-open");
    return;
  }
  host.hidden = false;
  document.body.classList.add("tdo-panel-open");
  if (refetch || !todoPanelData || todoPanelData.id !== todoSelId) {
    try {
      todoPanelData = await (await fetch("/api/tasks/panel?id=" + encodeURIComponent(todoSelId))).json();
    } catch (e) { todoPanelData = { id: todoSelId, record: {}, thread: [] }; }
  }
  if (todoPanelData.id !== todoSelId) return; // raced a newer selection
  host.innerHTML = "";
  const d = todoPanelData;
  const rec = d.record || {};
  const row = todoRowInfo(todoSelId);

  // --- head: title + container + close ---
  const head = el("div", "tdo-p-head");
  const titleWrap = el("div", "tdo-p-titlewrap");
  titleWrap.append(el("div", "tdo-p-title", row ? row.text : todoSelId));
  const metaBits = [];
  if (row && row.container && row.container.name) metaBits.push(row.container.name);
  if (rec.State) metaBits.push(rec.State);
  if ((d.coord && d.coord.state === "blocked") || (row && row.state === "blocked")) metaBits.push("blocked");
  if (metaBits.length) titleWrap.append(el("div", "tdo-p-meta", metaBits.join(" · ")));
  head.append(titleWrap);
  const x = el("button", "aion-insp-x", "✕");
  x.onclick = closeTodoPanel;
  head.append(x);
  host.append(head);

  // --- delegation state, when the todo is out with an agent ---
  if (d.delegation && typeof delegationChip === "function") {
    const dg = el("div", "tdo-p-deleg");
    dg.append(delegationChip(d.delegation));
    host.append(dg);
  }

  // --- assignee row: the roster picker ---
  const asg = el("div", "tdo-p-sec");
  asg.append(el("div", "tdo-p-sec-label", "assignee"));
  asg.append(todoAssigneeControl(d, row));
  host.append(asg);

  // --- coordination (P1 Phase 1): priority · depends on · blocks ---
  host.append(todoCoordSection(d, row));

  // --- description (plan D2, agent-chat plan gap D): the owner's context.
  // Rides every work order and the plan-context hash; click to edit in place.
  const desc = el("div", "tdo-p-sec");
  desc.append(el("div", "tdo-p-sec-label", "description"));
  const descText = rec.Description || rec.description || "";
  const descBody = el("div", "tdo-p-desc" + (descText ? "" : " empty"),
    descText || "add context — for you, and for the agent's brief");
  descBody.title = "click to edit";
  descBody.onclick = () => {
    const ta = document.createElement("textarea");
    ta.className = "tdo-p-textarea";
    ta.rows = 3;
    ta.value = descText;
    const save = pillLight("save description", async () => {
      try {
        await postJSONOk("/api/tasks/description", { id: todoSelId, text: ta.value });
        renderTodoPanel(true);
      } catch (e) { showToast("Couldn't save description — " + (e.message || "error")); }
    });
    ta.onkeydown = (ev) => {
      if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) save.click();
      else if (ev.key === "Escape") renderTodoPanel(false);
    };
    descBody.replaceWith(ta);
    desc.append(save);
    ta.focus();
  };
  desc.append(descBody);
  host.append(desc);

  // --- plan: rendered preview + ONE action. "open →" goes to the full-page
  // record (which carries its own Edit raw / Obsidian toggles); inline
  // writing exists only while there is no plan yet.
  const plan = el("div", "tdo-p-sec");
  const planHead = el("div", "tdo-p-sec-label");
  planHead.append(document.createTextNode("plan"));
  const planActs = el("span", "tdo-p-sec-acts");
  const planText = rec.Plan || rec.plan || "";
  const openPlan = () => { location.hash = "#/note/" + encodeURIComponent(rec.Rel || rec.rel); };
  let editBtn = null;
  if (planText) {
    const open = el("button", "tdo-p-linky", "open →");
    open.title = "open the plan full-page (edit there)";
    open.onclick = openPlan;
    planActs.append(open);
  } else {
    editBtn = el("button", "tdo-p-linky", "＋ write");
    planActs.append(editBtn);
  }
  planHead.append(planActs);
  plan.append(planHead);
  const planBody = el("div", "tdo-p-plan");
  if (planText) {
    // renderMarkdown returns a DOM fragment — append it (innerHTML would
    // stringify to "[object DocumentFragment]")
    try { planBody.append(renderMarkdown(planText, null, { readOnly: true })); }
    catch (e) { planBody.textContent = planText; }
    planBody.classList.add("clickable");
    planBody.title = "open the plan full-page";
    planBody.onclick = (ev) => { if (!ev.target.closest("a")) openPlan(); };
  } else {
    planBody.append(el("div", "tdo-p-empty", "no plan yet — write one, or assign an agent to draft it"));
  }
  plan.append(planBody);
  // fire — the explicit go (§12: the plan lane never executes on its own).
  // Shown when a plan exists and an agent holds the assignment.
  const assignee = rec.Assignee || rec.assignee || "";
  const st = d.delegation && d.delegation.state;
  if (planText && assignee.startsWith("agent:") && st !== "go-queued" && st !== "running") {
    const fireRow = el("div", "tdo-p-fire");
    const fire = el("button", "term-primary tdo-p-firebtn", "fire → " + assignee.slice(6) + " executes");
    fire.onclick = async () => {
      fire.disabled = true;
      try {
        await postJSONOk("/api/tasks/fire", { id: todoSelId });
        showToast("Fired — " + assignee.slice(6) + " is executing the plan", null, "info");
        renderTodoPanel(true);
      } catch (e) {
        fire.disabled = false;
        showToast("Couldn't fire — " + (e.message || "error"));
      }
    };
    fireRow.append(fire);
    plan.append(fireRow);
  }
  if (editBtn) {
    editBtn.onclick = () => {
      const ta = document.createElement("textarea");
      ta.className = "tdo-p-textarea plan";
      ta.rows = 6;
      const save = pillLight("save plan", async () => {
        try {
          await postJSONOk("/api/tasks/plan", { id: todoSelId, text: ta.value });
          renderTodoPanel(true);
        } catch (e) { showToast("Couldn't save plan — " + (e.message || "error")); }
      });
      planBody.replaceWith(ta);
      plan.append(save);
      editBtn.hidden = true;
      ta.focus();
    };
  }
  host.append(plan);

  // --- thread ---
  const th = el("div", "tdo-p-sec tdo-p-threadsec");
  const thHead = el("div", "tdo-p-sec-label");
  const kindTag = { aion: "team-visible", re: "shared · RE", private: "private" };
  thHead.append(document.createTextNode("thread"));
  const thActs = el("span", "tdo-p-sec-acts");
  // "open conversation" is one gesture: when the task came from chat, land
  // directly in that exact transcript; otherwise land in its agent section.
  if (d.chat && d.chat.agent) {
    const open = el("button", "tdo-p-linky", d.chat.id ? "open conversation ↗" : "open in chat ↗");
    open.title = d.chat.id
      ? "the conversation with " + (d.chat.label || d.chat.agent) + " this task came from" + (d.chat.title ? " — “" + d.chat.title + "”" : "")
      : (d.chat.label || d.chat.agent) + "'s conversations in CHAT";
    open.onclick = () => {
      // Promoted conversations retain their exact source session. Native task
      // threads have no session, so their dedicated CHAT route renders the
      // task thread itself in the full panel.
      location.hash = d.chat.id
        ? "#/chat/a/" + encodeURIComponent(d.chat.agent) + "/" + encodeURIComponent(d.chat.id)
        : "#/chat/task/" + encodeURIComponent(todoSelId);
    };
    thActs.append(open);
  }
  thActs.append(el("span", "tdo-p-thread-kind", kindTag[d.threadKind] || ""));
  thHead.append(thActs);
  th.append(thHead);
  // ⚑ proposals in place (§3.4e): the agent's changes wait in FEED — this is
  // a pointer to those cards, never a second approvals surface
  const props = d.proposals || [];
  if (props.length) {
    const n = props.length;
    const link = el("button", "tdo-p-linky tdo-p-proposals",
      "⚑ " + n + (n === 1 ? " change" : " changes") + " proposed — review");
    link.title = "open the approval cards for this task in FEED";
    link.onclick = () => {
      if (typeof pendingApprovalFocus !== "undefined") pendingApprovalFocus = props[0].id;
      if (typeof state !== "undefined") state.feedFilter = "proposal";
      location.hash = "#/feed";
    };
    th.append(link);
  }
  const list = el("div", "tdo-p-thread");
  (d.thread || []).forEach((c) => list.append(todoThreadEntry(c, todoSelId)));
  if (d.inflight) list.append(todoInflightEntry(d.inflight));
  if (!(d.thread || []).length && !d.inflight) list.append(el("div", "tdo-p-empty", "no comments yet"));
  th.append(list);
  th.append(todoComposer(d, { taskID: todoSelId }));
  host.append(th);
  list.scrollTop = list.scrollHeight;
}

// todoInflightEntry — presence (§3.4d): "✦ Alfred is working… since 14:02
// (plan)", derived from the live delegation index, never stored.
function todoInflightEntry(f) {
  const e = el("div", "tdo-p-comment tdo-p-inflight");
  const since = f.since ? new Date(f.since) : null;
  const hm = since && !isNaN(since) ? since.toTimeString().slice(0, 5) : "";
  e.append(el("span", "tdo-p-inflight-dot", "✦"));
  e.append(el("span", null, (f.name || "agent") + " is working…" +
    (hm ? " since " + hm : "") + (f.phase ? " (" + f.phase + ")" : "")));
  return e;
}

// todoCoordSection — the coordination state (P1 Phase 1). Priority is the
// closed set as a segmented control ("none" clears). "depends on" lists the
// stored [depends::] ids as chips whose look is the DERIVED state (open =
// blocks this · done · unresolved), ✕ arms then confirms; the add picker is
// a typeahead over the live rows, or a pasted id (unknown ids are tolerated
// and surface as unresolved). "blocks" is the reverse edge — read-only,
// click opens that task. Backlog items (aion:/re:) carry neither field yet.
const TODO_PRIORITIES = [["", "none"], ["low", "low"], ["med", "med"], ["high", "high"]];
function todoCoordSection(d, row) {
  const sec = el("div", "tdo-p-sec tdo-p-coord");
  const coord = (d && d.coord) || {};
  const pick = (k) => coord[k] || (row && row[k]) || [];
  const depends = (row && row.depends) || [];
  const blockedBy = pick("blockedBy"), unresolved = pick("unresolved"), dependents = pick("dependents");
  const canEdit = !/^(aion|re):/.test(todoSelId);
  const refresh = async (body) => {
    await todosApi("/api/tasks/depends", body);
    renderTodoPanel(true);
  };

  // priority
  sec.append(el("div", "tdo-p-sec-label", "priority"));
  const seg = el("div", "tdo-p-modes");
  const cur = (row && row.priority) || "";
  TODO_PRIORITIES.forEach(([val, label]) => {
    const b = el("button", "tdo-p-mode" + (cur === val ? " on" : ""), label);
    b.disabled = !canEdit;
    b.title = canEdit ? (val ? "priority " + val : "clear the priority") : "backlog items don't carry priority yet";
    b.onclick = async () => {
      if (val === cur) return;
      await todosApi("/api/tasks/priority", { id: todoSelId, priority: val });
      renderTodoPanel(true);
    };
    seg.append(b);
  });
  sec.append(seg);

  // depends on
  const dh = el("div", "tdo-p-sec-label");
  dh.append(document.createTextNode("depends on"));
  const dActs = el("span", "tdo-p-sec-acts");
  const addBtn = el("button", "tdo-p-linky", "＋ add");
  addBtn.title = "a task that must finish first";
  addBtn.hidden = !canEdit;
  dActs.append(addBtn);
  dh.append(dActs);
  sec.append(dh);
  const chips = el("div", "tdo-p-chips");
  depends.forEach((id) => {
    const state = blockedBy.includes(id) ? "open" : unresolved.includes(id) ? "unresolved" : "done";
    const chip = el("span", "tdo-p-chip dep " + state);
    const name = el("span", "tdo-p-dep-name", coordName(id));
    name.title = id + (state === "open" ? " — still open, blocks this" : state === "unresolved" ? " — no source knows this id" : " — done");
    if (todoRowInfo(id)) { name.classList.add("linky"); name.onclick = () => openTodoPanel(id); }
    chip.append(name);
    if (canEdit) {
      const x = el("button", "tdo-p-dep-x", "✕");
      x.title = "remove this dependency";
      x.onclick = () => {
        const yes = el("button", "tdo-p-dep-x arm", "remove?");
        yes.onclick = () => refresh({ id: todoSelId, remove: id });
        x.replaceWith(yes);
        setTimeout(() => { if (yes.parentNode) yes.replaceWith(x); }, 2500);
      };
      chip.append(x);
    }
    chips.append(chip);
  });
  sec.append(chips);
  if (!depends.length) sec.append(el("div", "tdo-p-empty", "nothing — this can start now"));
  const addHost = el("div", "tdo-p-deps-add");
  sec.append(addHost);
  addBtn.onclick = () => {
    if (addHost.children.length) return;
    let done = false;
    const close = () => { if (!done) { done = true; addHost.innerHTML = ""; } };
    const commit = (id) => { if (done) return; done = true; addHost.innerHTML = ""; refresh({ id: todoSelId, add: id }); };
    const ta = typeahead({
      placeholder: "task this waits on… (or paste an id)",
      minChars: 0,
      onEnter: commit,
      onEscape: close,
      onBlurGone: close,
      suggest: (q, add) => {
        ((todosCache && todosCache.rows) || [])
          .filter((r) => r.id !== todoSelId && !depends.includes(r.id))
          .filter((r) => !q || r.text.toLowerCase().includes(q) || r.id.toLowerCase().includes(q))
          .slice(0, 8)
          .forEach((r) => add(r.text, (r.container && r.container.name) || r.source, () => commit(r.id)));
      },
    });
    addHost.append(ta.el);
    ta.focus();
  };

  // blocks — the reverse edge, derived
  if (dependents.length) {
    sec.append(el("div", "tdo-p-sec-label", "blocks"));
    dependents.forEach((id) => {
      const e = el("div", "tdo-p-dependent", coordName(id));
      e.title = id + " — waits on this";
      e.onclick = () => openTodoPanel(id);
      sec.append(e);
    });
  }
  return sec;
}

// todoAssigneeControl — the uniform roster picker (Phase 3): people from the
// aion/RE groups (record-only assignment) + AGENTS with the hard `agent:`
// prefix; picking an agent kicks the plan-phase delegation server-side.
function todoAssigneeControl(d, row) {
  const rec = d.record || {};
  const cur = (row && row.owner) || rec.Assignee || rec.assignee || "";
  const display = cur.startsWith("agent:") ? "✦ " + cur.slice(6) : cur;
  const wrap = el("div", "tdo-p-assignee-wrap");
  const ta = typeahead({
    placeholder: "unassigned — type to assign…",
    initial: display,
    suggest: (q, add, taRef) => {
      const ql = (q || "").toLowerCase();
      todoRoster().forEach((p) => {
        if (p.id.includes("::")) return; // assignment takes the BASE token only — intent is per-message
        if (ql && !p.name.toLowerCase().includes(ql) && !p.id.toLowerCase().includes(ql)) return;
        add((p.kind === "agent" ? "✦ " : "") + p.name, p.kind.toUpperCase(), () => todoAssign(p.id, taRef));
      });
      if (cur) add("✕ unassign", "", () => todoAssign("", taRef));
    },
  });
  wrap.append(ta.el);
  return wrap;
}

async function todoAssign(ownerToken, taRef) {
  try {
    await postJSONOk("/api/tasks/assign", { id: todoSelId, owner: ownerToken });
    loadTodos(); // rows re-project the owner; panel re-syncs via loadTodos
    renderTodoPanel(true);
  } catch (e) {
    showToast("Couldn't assign — " + (e.message || "error"));
    if (taRef && taRef.el) renderTodoPanel(true);
  }
}

function todoThreadEntry(c, taskID) {
  taskID = taskID || todoSelId;
  const e = el("div", "tdo-p-comment" + (c.action && c.action !== "comment" ? " structural act-" + c.action : ""));
  const head = el("div", "tdo-p-c-head");
  const who = c.author_name || c.authorName || c.author || "?";
  head.append(el("span", "tdo-p-c-author", who));
  if (c.action && c.action !== "comment") head.append(el("span", "tdo-p-c-act", c.action));
  if (c.meta && c.meta.persona) head.append(el("span", "tdo-p-c-persona", c.meta.persona));
  // an entry copied in by "→ task" (§3.4f) says so
  if (c.meta && c.meta.from === "chat") { const f = el("span", "tdo-p-c-persona", "from chat"); f.title = "copied from the conversation this task was promoted from"; head.append(f); }
  head.append(el("span", "tdo-p-c-when", typeof termRelTime === "function" ? termRelTime(c.at) : (c.at || "").slice(0, 10)));
  e.append(head);
  if (c.text) {
    const body = el("div", "tdo-p-c-text");
    // Agent replies carry intentful Markdown (brief/info/plan personas). Keep
    // owner comments literal, but give the agent's answer the same read-only
    // renderer used by CHAT and plan previews.
    if ((c.author || "").startsWith("agent:")) {
      body.classList.add("agent-reply");
      try { body.append(renderMarkdown(c.text, "", { readOnly: true })); }
      catch (e) { body.textContent = c.text; }
    } else body.textContent = c.text;
    e.append(body);
  }
  // an agent comment that references its brief carries a view chip
  if (c.meta && c.meta.artifactRef && typeof openResult === "function") {
    const v = el("button", "tdo-p-c-file", "view brief →");
    v.onclick = () => openResult({ artifactRef: c.meta.artifactRef, harness: c.meta.harness }, c.text || "brief");
    e.append(v);
  }
  (c.files || []).forEach((f) => {
    const a = document.createElement("a");
    a.className = "tdo-p-c-file";
    a.textContent = "⤓ " + f.name;
    a.href = "/api/tasks/thread/file/" + f.hash + "?id=" + encodeURIComponent(taskID);
    a.target = "_blank";
    e.append(a);
  });
  return e;
}

// The composer (agent-chat plan §3.4a): a MODE — Comment (record only, never
// a turn) · Ask ✦ agent (one turn, answered here) · Do ✦ agent (assign → plan
// → fire) — defaulting to the last-used one, plus @-mentions (roster
// typeahead, or typed `@alfred` in the text — the server reads both) and
// attachments (upload first, refs ride the comment POST). Every mode posts
// the text as a thread comment: the thread stays the record.
const TODO_COMPOSER_MODES = [["comment", "Comment"], ["ask", "Ask ✦"], ["do", "Do ✦"]];
function todoComposer(d, opts) {
  opts = opts || {};
  const taskID = opts.taskID || todoSelId;
  const box = el("div", "tdo-p-composer");
  const pendingFiles = [];
  const mentions = [];
  const chips = el("div", "tdo-p-chips");
  const ta = document.createElement("textarea");
  ta.className = "tdo-p-textarea composer";
  ta.rows = 2;

  // --- mode + agent ---
  const rec = (d && d.record) || {};
  const row = todoRowInfo(todoSelId);
  const assignee = (row && row.owner) || rec.Assignee || rec.assignee || "";
  const agents = todoRoster().filter((p) => p.kind === "agent" && !p.id.includes("::"));
  const preset = todoComposerPreset;
  todoComposerPreset = null;
  let mode = (preset && preset.mode) || localStorage.getItem("todoComposerMode") || "comment";
  if (!agents.length || !TODO_COMPOSER_MODES.some(([v]) => v === mode)) mode = "comment";
  // the picker defaults to the task's assignee, else Alfred, else the first
  const defAgent = agents.find((a) => a.id === assignee) ||
    agents.find((a) => /^agent:(alfred|hermes)$/.test(a.id)) || agents[0];
  const agentSel = document.createElement("select");
  agentSel.className = "pp-in tdo-p-agent";
  agents.forEach((a) => {
    const o = document.createElement("option");
    o.value = a.id; o.textContent = "✦ " + a.name;
    if (a.description) o.title = a.description;
    agentSel.append(o);
  });
  if (defAgent) agentSel.value = defAgent.id;
  const seg = el("div", "tdo-p-modes");
  const modeBar = el("div", "tdo-p-modebar");
  const agentName = () => (agentSel.selectedOptions[0] ? agentSel.selectedOptions[0].textContent.replace(/^✦ /, "") : "the agent");
  // the picked agent's profile description is the picker's tooltip (§3.5)
  const agentTip = () => { const a = agents.find((x) => x.id === agentSel.value); agentSel.title = (a && a.description) || "which agent"; };
  // "suggest agent" (§2.5): a NON-BINDING hint when the text reads like
  // another agent's description — click adopts it; nothing routes on its own
  const suggest = el("button", "tdo-p-linky tdo-p-suggest");
  suggest.hidden = true;
  const paintSuggest = () => {
    const hit = mode === "comment" ? null : todoSuggestAgent(ta.value, agents);
    suggest.hidden = !hit || hit.id === agentSel.value;
    if (suggest.hidden) return;
    suggest.textContent = "suggest ✦ " + hit.name;
    suggest.title = "reads like " + hit.name + "'s brief — " + hit.description + " · click to pick (a hint, never automatic)";
    suggest.onclick = () => { agentSel.value = hit.id; paint(); ta.focus(); };
  };
  const paint = () => {
    seg.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b.dataset.mode === mode));
    agentSel.hidden = mode === "comment";
    agentTip();
    ta.placeholder = mode === "ask" ? "ask " + agentName() + " — one turn, answered in this thread…"
      : mode === "do" ? "tell " + agentName() + " what to do — it assigns, drafts the plan; you fire…"
      : "comment… (@ to mention · @alfred asks · @alfred::plan delegates)";
    send.textContent = mode === "ask" ? "ask" : mode === "do" ? "do" : "comment";
    paintSuggest();
  };
  ta.addEventListener("input", paintSuggest);
  TODO_COMPOSER_MODES.forEach(([val, label]) => {
    const b = el("button", "tdo-p-mode", label);
    b.dataset.mode = val;
    b.title = val === "comment" ? "record only — never spends a turn"
      : val === "ask" ? "one turn — the answer lands in this thread"
      : "the lifecycle — assign → plan → fire";
    if (val !== "comment" && !agents.length) b.disabled = true;
    b.onclick = () => { mode = val; localStorage.setItem("todoComposerMode", mode); paint(); if (val !== "comment") agentSel.focus(); };
    seg.append(b);
  });
  agentSel.onchange = paint;
  modeBar.append(seg, agentSel, suggest);
  // "replying to Alfred" — the cue that the last word was the agent's
  const th = (d && d.thread) || [];
  const last = th.length ? th[th.length - 1] : null;
  if (last && last.action === "comment" && (last.author || "").startsWith("agent:")) {
    modeBar.append(el("span", "tdo-p-replying", "replying to " + (last.author_name || last.authorName || last.author.slice(6))));
  }

  const acts = el("div", "tdo-p-c-acts");
  // attach
  const fi = document.createElement("input");
  fi.type = "file"; fi.multiple = true; fi.hidden = true;
  fi.onchange = async () => {
    for (const f of [...fi.files]) {
      try {
        const res = await fetch("/api/tasks/thread/file?id=" + encodeURIComponent(taskID) +
          "&name=" + encodeURIComponent(f.name), { method: "POST", body: f });
        if (!res.ok) throw new Error((await res.text()).slice(0, 120));
        const ref = (await res.json()).file;
        pendingFiles.push(ref);
        chips.append(el("span", "tdo-p-chip", "⤓ " + ref.name));
      } catch (e) { showToast("Upload failed — " + (e.message || "error")); }
    }
    fi.value = "";
  };
  const attach = el("button", "tdo-p-linky", "＋ file");
  attach.onclick = () => fi.click();
  // mention
  const mentionBtn = el("button", "tdo-p-linky", "@ mention");
  mentionBtn.onclick = () => {
    if (box.querySelector(".ta-wrap")) return;
    const ta2 = typeahead({
      placeholder: "who…",
      suggest: (q, add, taRef) => {
        todoRoster().forEach((p) => {
          if (!q || p.name.toLowerCase().includes(q.toLowerCase()) || p.id.toLowerCase().includes(q.toLowerCase())) {
            add(p.name, p.kind.toUpperCase(), () => {
              mentions.push(p.id);
              chips.append(el("span", "tdo-p-chip mention", "@" + p.name));
              taRef.el.remove();
            });
          }
        });
      },
    });
    acts.before(ta2.el);
    ta2.input.focus();
  };
  const send = pillLight("comment", async () => {
    const text = ta.value.trim();
    if (!text && !pendingFiles.length) return;
    if (mode !== "comment" && !text) { showToast("Say what you want " + agentName() + " to do"); return; }
    const agent = mode === "comment" ? "" : agentSel.value;
    try {
      await postJSONOk("/api/tasks/thread", { id: taskID, text, mentions, files: pendingFiles, mode, agent });
      if (mode === "ask") showToast("Asked " + agentName() + " — the answer lands in this thread", null, "info");
      else if (mode === "do") showToast(agentName() + " is drafting the plan — fire it when it's right", null, "info");
      ta.value = "";
      pendingFiles.length = 0;
      mentions.length = 0;
      chips.innerHTML = "";
      loadTodos(); // Ask/Do may have assigned — rows re-project the owner chip
      if (opts.onPosted) opts.onPosted();
      else renderTodoPanel(true);
    } catch (e) { showToast("Couldn't " + (mode === "comment" ? "comment" : mode) + " — " + (e.message || "error")); }
  });
  ta.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) send.click();
  };
  acts.append(attach, mentionBtn, send, fi);
  box.append(modeBar, chips, ta, acts);
  paint();
  if (preset && preset.focusAgent && mode !== "comment") {
    setTimeout(() => { if (agentSel.isConnected) agentSel.focus(); }, 0);
  }
  return box;
}

// todoRoster — merged assignee groups from the /api/tasks payload (people
// from the aion/RE groups, agents from the server-side roster). Agents
// expand into their base token plus one intent-tagged entry per enabled
// persona: `agent:alfred::brief` — the intent rides the mention, never the
// assignment. An agent's description (its `hermes profile describe` text)
// rides along for tooltips and the suggest hint.
function todoRoster() {
  const a = (todosCache && todosCache.assignees) || {};
  const out = [];
  (a.agents || []).forEach((x) => {
    out.push({ id: x.id, name: x.name, kind: "agent", description: x.description || "" });
    (x.personas || []).forEach((pi) =>
      out.push({ id: x.id + "::" + pi, name: x.name + " · " + pi, kind: "agent" }));
  });
  (a.aion || []).forEach((x) => out.push({ id: x.id || x.initials || x.name, name: x.name || x.initials, kind: "aion" }));
  (a.realestate || []).forEach((x) => out.push({ id: x.id || x.name, name: x.name, kind: "re" }));
  return out;
}

// todoSuggestAgent — the §2.5 hint: the agent whose description shares the
// most distinctive words with the text (≥ 2 hits, a clear lead over the
// runner-up), or null. Pure; the caller decides what to show. Descriptions
// are the only signal — an agent without one is never suggested.
const todoSuggestStop = new Set(["this", "that", "with", "from", "have", "what", "when", "will", "your", "about", "into", "them", "then", "than", "they", "were", "been", "does", "make", "just", "like", "also", "over", "some", "more", "most", "very", "here", "there", "which", "their", "would", "could", "should", "please", "need", "want", "find", "agent", "profile", "hermes", "default"]);
function todoSuggestWords(text) {
  const out = new Set();
  String(text || "").toLowerCase().split(/[^a-z0-9]+/).forEach((w) => {
    if (w.length >= 4 && !todoSuggestStop.has(w)) out.add(w.replace(/(ings?|es|s|ed)$/, ""));
  });
  return out;
}
function todoSuggestAgent(text, agents) {
  const words = todoSuggestWords(text);
  if (words.size < 2) return null;
  const scored = (agents || []).filter((a) => a.description).map((a) => {
    let n = 0;
    todoSuggestWords(a.description).forEach((w) => { if (words.has(w)) n++; });
    return { a, n };
  }).sort((x, y) => y.n - x.n);
  if (!scored.length || scored[0].n < 2) return null;
  if (scored.length > 1 && scored[1].n === scored[0].n) return null; // a tie is not a hint
  return scored[0].a;
}
