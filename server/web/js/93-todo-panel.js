// ================= TODO PANEL (todo-panel plan Phase 2) =================
// Click a list row or board card → a sticky right panel (the AION-inspector
// idiom: the list never reflows) carrying the todo's DESCRIPTION, its PLAN
// (the system/todo-plans record — agent-writable via the §12 lane), the
// ASSIGNEE row (Phase 3), and the comment THREAD with @-mentions and
// content-addressed attachments. Under 1100px the panel becomes a sheet.

let todoSelId = null;      // selected todo id ("" = none)
let todoPanelData = null;  // last /api/todos/panel payload
let todoDeepLink = null;   // #/todos/<id> → open after load
let todoPanelTimer = null; // live refresh while the panel is open

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
      const fresh = await (await fetch("/api/todos/panel?id=" + encodeURIComponent(todoSelId))).json();
      if (fresh.id !== todoSelId) return;
      if (JSON.stringify(fresh) !== JSON.stringify(todoPanelData)) {
        todoPanelData = fresh;
        renderTodoPanel(false);
      }
    } catch (e) {}
  }, 8000);
}

// openTodoPanel — from a row/card ({id, text, ...}) or an id string.
function openTodoPanel(rOrId) {
  const id = typeof rOrId === "string" ? rOrId : rOrId.id;
  todoSelId = id;
  const suffix = "#/todos/" + encodeURIComponent(id);
  if (location.hash !== suffix) {
    try { history.replaceState(null, "", suffix); } catch (e) {}
  }
  ensureTodoPanelPoll();
  renderTodoPanel(true);
}

function closeTodoPanel() {
  todoSelId = null;
  todoPanelData = null;
  try { history.replaceState(null, "", "#/todos"); } catch (e) {}
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
      todoPanelData = await (await fetch("/api/todos/panel?id=" + encodeURIComponent(todoSelId))).json();
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

  // --- assignee row (Phase 3 fills the roster picker; owner text now) ---
  const asg = el("div", "tdo-p-sec");
  asg.append(el("div", "tdo-p-sec-label", "assignee"));
  asg.append(todoAssigneeControl(d, row));
  host.append(asg);

  // --- description ---
  const desc = el("div", "tdo-p-sec");
  desc.append(el("div", "tdo-p-sec-label", "description"));
  const dta = document.createElement("textarea");
  dta.className = "tdo-p-textarea";
  dta.placeholder = "why this matters, context, links…";
  dta.value = rec.Description || rec.description || "";
  dta.rows = Math.max(2, Math.min(8, (dta.value.match(/\n/g) || []).length + 2));
  let descDirty = false;
  dta.oninput = () => { descDirty = true; };
  dta.onblur = async () => {
    if (!descDirty) return;
    descDirty = false;
    try {
      await postJSONOk("/api/todos/description", { id: todoSelId, text: dta.value });
    } catch (e) { showToast("Couldn't save description — " + (e.message || "error")); }
  };
  desc.append(dta);
  host.append(desc);

  // --- plan (rendered md · edit toggle · open the record file) ---
  const plan = el("div", "tdo-p-sec");
  const planHead = el("div", "tdo-p-sec-label");
  planHead.append(document.createTextNode("plan"));
  const planActs = el("span", "tdo-p-sec-acts");
  const planText = rec.Plan || rec.plan || "";
  if (rec.Exists || rec.exists) {
    const openFile = el("button", "tdo-p-linky", "file →");
    openFile.title = "open the plan record";
    openFile.onclick = () => { location.hash = "#/note/" + encodeURIComponent(rec.Rel || rec.rel); };
    planActs.append(openFile);
  }
  const editBtn = el("button", "tdo-p-linky", planText ? "edit" : "＋ write");
  planActs.append(editBtn);
  planHead.append(planActs);
  plan.append(planHead);
  const planBody = el("div", "tdo-p-plan");
  if (planText) {
    try { planBody.innerHTML = renderMarkdown(planText, null, { inline: false }); }
    catch (e) { planBody.textContent = planText; }
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
        await postJSONOk("/api/todos/fire", { id: todoSelId });
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
  editBtn.onclick = () => {
    const ta = document.createElement("textarea");
    ta.className = "tdo-p-textarea plan";
    ta.value = planText;
    ta.rows = Math.max(6, Math.min(18, (planText.match(/\n/g) || []).length + 3));
    const save = pillLight("save plan", async () => {
      try {
        await postJSONOk("/api/todos/plan", { id: todoSelId, text: ta.value });
        renderTodoPanel(true);
      } catch (e) { showToast("Couldn't save plan — " + (e.message || "error")); }
    });
    planBody.replaceWith(ta);
    plan.append(save);
    editBtn.hidden = true;
    ta.focus();
  };
  host.append(plan);

  // --- thread ---
  const th = el("div", "tdo-p-sec tdo-p-threadsec");
  const thHead = el("div", "tdo-p-sec-label");
  const kindTag = { aion: "team-visible", re: "shared · RE", private: "private" };
  thHead.append(document.createTextNode("thread"));
  thHead.append(el("span", "tdo-p-sec-acts tdo-p-thread-kind", kindTag[d.threadKind] || ""));
  th.append(thHead);
  const list = el("div", "tdo-p-thread");
  (d.thread || []).forEach((c) => list.append(todoThreadEntry(c)));
  if (!(d.thread || []).length) list.append(el("div", "tdo-p-empty", "no comments yet"));
  th.append(list);
  th.append(todoComposer());
  host.append(th);
  list.scrollTop = list.scrollHeight;
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
    await postJSONOk("/api/todos/assign", { id: todoSelId, owner: ownerToken });
    loadTodos(); // rows re-project the owner; panel re-syncs via loadTodos
    renderTodoPanel(true);
  } catch (e) {
    showToast("Couldn't assign — " + (e.message || "error"));
    if (taRef && taRef.el) renderTodoPanel(true);
  }
}

function todoThreadEntry(c) {
  const e = el("div", "tdo-p-comment" + (c.action && c.action !== "comment" ? " structural act-" + c.action : ""));
  const head = el("div", "tdo-p-c-head");
  const who = c.author_name || c.authorName || c.author || "?";
  head.append(el("span", "tdo-p-c-author", who));
  if (c.action && c.action !== "comment") head.append(el("span", "tdo-p-c-act", c.action));
  head.append(el("span", "tdo-p-c-when", typeof termRelTime === "function" ? termRelTime(c.at) : (c.at || "").slice(0, 10)));
  e.append(head);
  if (c.text) e.append(el("div", "tdo-p-c-text", c.text));
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
    a.href = "/api/todos/thread/file/" + f.hash + "?id=" + encodeURIComponent(todoSelId);
    a.target = "_blank";
    e.append(a);
  });
  return e;
}

// The composer: text + @-mentions (roster typeahead — structural, no new
// syntax) + attachments (upload first, refs ride the comment POST).
function todoComposer() {
  const box = el("div", "tdo-p-composer");
  const pendingFiles = [];
  const mentions = [];
  const chips = el("div", "tdo-p-chips");
  const ta = document.createElement("textarea");
  ta.className = "tdo-p-textarea composer";
  ta.placeholder = "comment… (@ to mention)";
  ta.rows = 2;

  const acts = el("div", "tdo-p-c-acts");
  // attach
  const fi = document.createElement("input");
  fi.type = "file"; fi.multiple = true; fi.hidden = true;
  fi.onchange = async () => {
    for (const f of [...fi.files]) {
      try {
        const res = await fetch("/api/todos/thread/file?id=" + encodeURIComponent(todoSelId) +
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
    try {
      await postJSONOk("/api/todos/thread", { id: todoSelId, text, mentions, files: pendingFiles });
      ta.value = "";
      pendingFiles.length = 0;
      mentions.length = 0;
      chips.innerHTML = "";
      renderTodoPanel(true);
    } catch (e) { showToast("Couldn't comment — " + (e.message || "error")); }
  });
  ta.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) send.click();
  };
  acts.append(attach, mentionBtn, send, fi);
  box.append(chips, ta, acts);
  return box;
}

// todoRoster — merged assignee groups from the /api/todos payload
// (Phase 3 adds the agents group server-side; this reads whatever is there).
function todoRoster() {
  const a = (todosCache && todosCache.assignees) || {};
  const out = [];
  (a.agents || []).forEach((x) => out.push({ id: x.id, name: x.name, kind: "agent" }));
  (a.aion || []).forEach((x) => out.push({ id: x.id || x.initials || x.name, name: x.name || x.initials, kind: "aion" }));
  (a.realestate || []).forEach((x) => out.push({ id: x.id || x.name, name: x.name, kind: "re" }));
  return out;
}
