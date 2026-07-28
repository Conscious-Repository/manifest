// ================= TODOS (the third surface — `to do.md` board) =================
// Manifest-quiet: domain groups, open items oldest-first, waiting collapsed at
// each group's foot. A place to pick things off, not a dashboard.
let todosCache = null;
let todosWaitOpen = {}; // domain → expanded

async function loadTodos() {
  try { todosCache = await (await fetch("/api/todos")).json(); }
  catch (e) { todosCache = { domains: [], areas: [] }; }
  renderTodos();
}

async function todosApi(path, body) {
  try {
    todosCache = { ...todosCache, ...(await postJSONOk(path, body)) };
    renderTodos();
  } catch (e) { showToast((e.message || "Todo update failed").slice(0, 80)); }
}

function renderTodos() {
  const host = els.todosRows; host.innerHTML = "";
  const domains = (todosCache.domains || []).slice()
    .sort((a, b) => (a.name.toLowerCase() === "inbox" ? -1 : 0) - (b.name.toLowerCase() === "inbox" ? -1 : 0));
  let open = 0, waiting = 0;
  domains.forEach((dom) => dom.todos.forEach((t) => {
    if (t.state === "open") open++;
    else if (t.state === "waiting") waiting++;
  }));
  els.todosMeta.textContent = open + " open · " + waiting + " waiting · t to capture";

  // one-time task-substrate split banner (until the migration commits)
  if (todosCache.split === false) {
    const banner = el("div", "tdo-triage-banner");
    banner.append(el("span", "", "one task substrate — move every open goals.md task here (tethered to its Rock, or demote the Rock to a bucket)."));
    banner.append(pillLight("run the split →", renderTodosSplit));
    host.append(banner);
  }

  domains.forEach((dom) => {
    const isInbox = dom.name.toLowerCase() === "inbox";
    if (isInbox && !dom.todos.length) return; // silent until something lands
    const sec = el("div", "tdo-section");
    const head = el("div", "tdo-head", dom.name.toUpperCase());
    sec.append(head);

    const opens = dom.todos.filter((t) => t.state === "open")
      .sort((a, b) => (a.added || "").localeCompare(b.added || ""));
    const waits = dom.todos.filter((t) => t.state === "waiting")
      .sort((a, b) => (a.since || a.added || "").localeCompare(b.since || b.added || ""));
    const dones = dom.todos.filter((t) => t.state === "done");

    opens.forEach((t) => sec.append(todoRow(dom, t, isInbox)));
    dones.forEach((t) => sec.append(todoRow(dom, t, false)));
    sec.append(ghostInput("＋ todo", "tdo-add", (v) => todosApi("/api/todos/item", { text: v, domain: dom.name }), "one line — what must happen…"));

    if (waits.length) {
      const foot = el("button", "tdo-wait-foot", (todosWaitOpen[dom.name] ? "▾" : "▸") + " waiting · " + waits.length);
      foot.onclick = () => { todosWaitOpen[dom.name] = !todosWaitOpen[dom.name]; renderTodos(); };
      sec.append(foot);
      if (todosWaitOpen[dom.name]) waits.forEach((t) => sec.append(todoRow(dom, t, false)));
    }

    // buckets — standing containers, rendered at todo-row weight with a
    // collapsible item list beneath (they're peers of the work, not chrome)
    (dom.buckets || []).forEach((bk) => {
      const key = dom.name + "/" + bk.slug;
      const open = !!todosWaitOpen[key];
      const bkOpen = bk.todos.filter((t) => t.state !== "done");
      const row = el("div", "tdo-row tdo-bucket-row");
      row.append(el("span", "sec-caret", open ? "▾" : "▸"));
      const bkName = el("span", "tdo-text tdo-bucket-name", bk.name);
      bkName.title = "click to rename";
      bkName.onclick = (e) => {
        e.stopPropagation(); // edit, don't toggle
        const input = inputEl(""); input.value = bk.name; input.classList.add("work-edit");
        input.onclick = (ev) => ev.stopPropagation();
        input.addEventListener("keydown", (ev) => {
          if (ev.key === "Enter" && input.value.trim()) {
            todosApi("/api/todos/bucket", { domain: dom.name, slug: bk.slug, name: input.value });
          } else if (ev.key === "Escape") input.replaceWith(bkName);
        });
        input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(bkName); });
        bkName.replaceWith(input);
        input.focus();
      };
      row.append(bkName);
      row.append(el("span", "tdo-tag", bkOpen.length + " open"));
      if ((bk.links || []).length) {
        const lk = el("span", "tdo-bucket-links", "⧉ " + bk.links.join(" · "));
        lk.title = "linked records — these todos also show on the linked property pages";
        row.append(lk);
      }
      row.onclick = () => { todosWaitOpen[key] = !todosWaitOpen[key]; renderTodos(); };
      sec.append(row);
      if (open) {
        const items = el("div", "tdo-bucket-items");
        bk.todos.slice().sort((a, b) => (a.added || "").localeCompare(b.added || ""))
          .forEach((t) => items.append(todoRow(dom, t, false)));
        items.append(ghostInput("＋ todo", "tdo-add", (v) =>
          todosApi("/api/todos/item", { text: v, domain: dom.name, bucket: bk.name }), "into " + bk.name + "…"));
        sec.append(items);
      }
    });

    // issues — decisions/blockers, collapsed; a live-tasked issue is being worked
    const openIssues = (dom.issues || []).filter((i) => !i.checked);
    if (openIssues.length || todosWaitOpen[dom.name + "#issues"]) {
      const key = dom.name + "#issues";
      const head = el("button", "tdo-bucket-head",
        (todosWaitOpen[key] ? "▾ " : "▸ ") + "issues · " + openIssues.length);
      head.onclick = () => { todosWaitOpen[key] = !todosWaitOpen[key]; renderTodos(); };
      sec.append(head);
      if (todosWaitOpen[key]) {
        (dom.issues || []).forEach((is) => sec.append(issueRow(dom, is)));
        sec.append(ghostInput("＋ issue", "tdo-add", (v) =>
          todosApi("/api/todos/issue", { text: v, domain: dom.name }), "the decision or blocker…"));
      }
    } else {
      const mk = el("button", "tdo-wait-foot", "＋ issue");
      mk.onclick = () => { todosWaitOpen[dom.name + "#issues"] = true; renderTodos(); };
      sec.append(mk);
    }

    // backlog — ideas, collapsed, never aged, read-only here (hand-edit in Obsidian)
    if ((dom.backlog || []).length) {
      const key = dom.name + "#backlog";
      const head = el("button", "tdo-bucket-head",
        (todosWaitOpen[key] ? "▾ " : "▸ ") + "backlog · " + dom.backlog.length);
      head.onclick = () => { todosWaitOpen[key] = !todosWaitOpen[key]; renderTodos(); };
      sec.append(head);
      if (todosWaitOpen[key]) dom.backlog.forEach((ln) =>
        sec.append(el("div", "tdo-backlog-line", ln.replace(/^[-*]\s*/, "· "))));
    }
    host.append(sec);
  });
  if (!host.children.length) host.append(el("div", "pp-empty", "Nothing here — press t anywhere to capture."));
}

function todoRow(dom, t, isInbox) {
  const row = el("div", "tdo-row" + (t.state === "done" ? " done" : ""));
  const check = el("button", "check wk-check" + (t.state === "done" ? " on" : ""), t.state === "done" ? "✓" : "○");
  check.onclick = () => todosApi("/api/todos/check", { id: t.id, checked: t.state !== "done" });
  row.append(check);

  const label = el("span", "tdo-text", t.text);
  label.title = "click to edit";
  label.onclick = () => {
    const input = inputEl(""); input.value = t.text; input.classList.add("work-edit");
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) todosApi("/api/todos/update", { id: t.id, text: input.value });
      else if (ev.key === "Escape") input.replaceWith(label);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(label); });
    label.replaceWith(input); input.focus();
  };
  row.append(label);

  // tether tags: what this line advances (muted)
  if (t.rock) {
    const tag = el("span", "tdo-tag", "⧗ " + t.rock.split("/").pop());
    tag.title = "services rock " + t.rock + " — ages by the rock's rhythm, not the 14d nudge";
    row.append(tag);
  }
  if (t.issue) {
    const tag = el("span", "tdo-tag", "⚑ " + t.issue.split("/").pop());
    tag.title = "works issue " + t.issue;
    row.append(tag);
  }

  if (t.state === "waiting") {
    const chip = el("span", "tdo-wait-chip", "⏳ " + t.waiting + " · " + t.ageDays + "d");
    chip.title = "waiting since " + (t.since || "?") + " — click to resume (back to open)";
    chip.onclick = () => todosApi("/api/todos/update", { id: t.id, waiting: "" });
    row.append(chip);
  }

  if (isInbox) {
    // one-tap domain assignment for inbox captures
    const chips = el("span", "tdo-domain-chips");
    (todosCache.areas || []).forEach((a) => {
      const c = el("button", "stmt-hint", a);
      c.onclick = () => todosApi("/api/todos/update", { id: t.id, domain: a });
      chips.append(c);
    });
    row.append(chips);
  }

  const age = el("span", "tdo-age" + (t.state === "open" && t.ageDays >= 14 ? " stale" : ""),
    t.ageDays > 0 ? t.ageDays + "d" : "");
  if (t.state === "open" && t.ageDays >= 14) age.title = "aging — do it, mark waiting, or drop it";
  row.append(age);

  const acts = el("span", "tdo-acts");
  if (t.state === "open") {
    const wait = el("button", "stmt-hint", "waiting…");
    wait.onclick = () => {
      const who = personInput((v) => todosApi("/api/todos/update", { id: t.id, waiting: v }),
        () => { who.el.replaceWith(wait); });
      wait.replaceWith(who.el);
      who.focus();
    };
    acts.append(wait);
  }
  const x = el("button", "uw-x", "✕");
  x.title = "drop (archived, never deleted)";
  x.onclick = () => {
    const yes = quietBtn("drop?", () => todosApi("/api/todos/drop", { id: t.id }));
    const no = quietBtn("no", () => { yes.remove(); no.remove(); x.hidden = false; });
    x.hidden = true;
    acts.append(yes, no);
  };
  acts.append(x);
  row.append(acts);
  return row;
}

// renderTodosSplit — the one-time task-substrate migration surface: every
// active Rock with task lines, keep-as-Rock (open tasks move here tethered)
// or demote-to-bucket (Rock closes as a Learn); one previewed commit.
async function renderTodosSplit() {
  const host = els.todosRows; host.innerHTML = "loading…";
  let d = { rocks: [] };
  try { d = await (await fetch("/api/todos/split")).json(); } catch (e) {}
  host.innerHTML = "";
  const rocks = d.rocks || [];
  els.todosMeta.textContent = rocks.length + " rocks carry task lines in goals.md";
  if (!rocks.length) { host.append(el("div", "pp-empty", "goals.md carries no task lines — nothing to split.")); return; }
  const choice = {}; // id → {action, bucket}
  rocks.forEach((r) => { choice[r.id] = { action: "keep", bucket: r.rock }; });

  const refreshBar = () => {
    const demotes = Object.values(choice).filter((c) => c.action === "demote").length;
    const moves = rocks.reduce((a, r) => a + (r.openTasks || []).length, 0);
    barLabel.textContent = moves + " OPEN TASKS MOVE · " + demotes + " ROCK" + (demotes === 1 ? "" : "S") + " DEMOTE · checked history freezes in place";
  };

  rocks.forEach((r) => {
    const sec = el("div", "tdo-section");
    const head = el("div", "tdo-head split-head");
    head.append(el("span", "", r.area.toUpperCase() + " · " + r.rock +
      (r.checkedCount ? "  (" + r.checkedCount + " checked lines stay frozen)" : "")));
    const toggle = el("button", "tdo-verdict keep", "keep as rock");
    toggle.onclick = () => {
      const c = choice[r.id];
      c.action = c.action === "keep" ? "demote" : "keep";
      toggle.textContent = c.action === "keep" ? "keep as rock" : "demote \u2192 bucket \u201c" + c.bucket + "\u201d";
      toggle.className = "tdo-verdict " + (c.action === "keep" ? "keep" : "move");
      refreshBar();
    };
    head.append(toggle);
    sec.append(head);
    (r.openTasks || []).forEach((t) => {
      const row = el("div", "tdo-row");
      row.append(el("span", "tdo-triage-path", "\u2192 " + r.area), el("span", "tdo-text", t));
      sec.append(row);
    });
    if (!(r.openTasks || []).length) sec.append(el("div", "pp-empty", "no open tasks \u2014 only frozen history"));
    host.append(sec);
  });

  const bar = el("div", "dirty-bar");
  const barLabel = el("span", "dirty-label", "");
  const commit = el("button", "pill", "commit split");
  commit.onclick = async () => {
    commit.disabled = true;
    const body = { rocks: rocks.map((r) => ({ id: r.id, action: choice[r.id].action, bucket: choice[r.id].bucket })) };
    try {
      const res = await postJSONOk("/api/todos/split", body);
      showToast("Split committed \u2014 " + (res.moved || []).length + " tasks moved \u00b7 " + (res.demoted || 0) + " demoted (backups: .pre-split)");
      loadTodos();
    } catch (e) { showToast(("Split failed: " + (e.message || "")).slice(0, 90)); commit.disabled = false; }
  };
  const cancel = el("button", "pill light", "later");
  cancel.onclick = () => loadTodos();
  bar.append(barLabel, commit, cancel);
  refreshBar();
  host.append(bar);
}

// issueRow: a decision/blocker line — open-tethered-task count shows it's
// being worked; resolving asks for the one-line note.
function issueRow(dom, is) {
  const row = el("div", "tdo-row" + (is.checked ? " done" : ""));
  row.append(el("span", "tdo-issue-dot", "⚑"));
  const label = el("span", "tdo-text", is.text + (is.checked && is.resolution ? " — " + is.resolution : ""));
  row.append(label);
  if (!is.checked && is.openTasks > 0) {
    row.append(el("span", "tdo-tag", is.openTasks + " task" + (is.openTasks === 1 ? "" : "s")));
  }
  if (!is.checked) {
    const resolve = el("button", "stmt-hint", "resolve…");
    resolve.onclick = () => {
      const input = inputEl("one-line resolution…");
      input.classList.add("work-edit");
      input.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter" && input.value.trim()) {
          todosApi("/api/todos/issue/resolve", { id: is.id, resolution: input.value });
        } else if (ev.key === "Escape") input.replaceWith(resolve);
      });
      input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(resolve); });
      resolve.replaceWith(input);
      input.focus();
    };
    row.append(resolve);
  }
  return row;
}

// personInput: free text or a contact — picking a suggestion wraps [[display]]
// so the waiting-on surfaces on that contact's page.
function personInput(onSet, onCancel) {
  const ta = typeahead({
    placeholder: "who? (name or free text)",
    minChars: 2,
    onEnter: onSet,
    onEscape: onCancel,
    onBlurGone: onCancel,
    suggest: async (q, add) => {
      let people = [];
      try {
        const d = await (await fetch("/api/contacts/search?q=" + encodeURIComponent(q))).json();
        people = (d.results || []).slice(0, 5);
      } catch (e) {}
      people.forEach((p) => add(p.display, "person", () => onSet("[[" + p.display + "]]")));
    },
  });
  return ta;
}
