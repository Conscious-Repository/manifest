// ---- WORK section (pass-4): stages · todos · tethered money ----

let pendingLedgerPrefill = null; // {type, workId} — set by "bid →"/"$ →", consumed by ledgerEntryRow
const workOpen = {}; // per-render expanded non-current stages (slug:stageId → bool)

async function workOp(slug, body) {
  try {
    await postJSONOk("/api/properties/" + encodeURIComponent(slug) + "/work", body);
    renderPropertyPage(slug);
  } catch (e) { showToast((e.message || "Work update failed").slice(0, 80)); }
}

// workBlock renders the management core in the goals-trail idiom: done stages
// struck ✓, the current stage → in blue and open, others collapsed.
function workBlock(p) {
  const wrap = el("div", "work-block");
  if (!(p.work || []).length) {
    const seed = el("div", "work-seed");
    seed.append(el("span", "pp-empty", "No work plan yet — seed stages:"));
    const opts = [["rehab", "rehab (8)"], ["new-build", "new build (9)"], ["phases", "from site phases"], ["empty", "start empty"]];
    opts.forEach(([tpl, label]) => {
      const b = pillLight(label, () => workOp(p.slug, { op: "seed", template: tpl }));
      if ((p.kind === "rehab" && tpl === "rehab") || (p.kind === "new-construction" && tpl === "new-build")) b.classList.add("suggested");
      seed.append(b);
    });
    wrap.append(seed);
    return wrap;
  }

  p.work.forEach((st) => {
    const open = st.current || workOpen[p.slug + ":" + st.id];
    const stageEl = el("div", "work-stage" + (st.checked ? " done" : "") + (st.current ? " cur" : ""));
    const head = el("div", "work-stage-head");
    const check = el("button", "check wk-check" + (st.checked ? " on" : ""), st.checked ? "✓" : "○");
    check.title = st.checked ? "reopen stage" : st.ready ? "stage ready — mark complete" : "mark stage complete";
    if (st.ready && !st.checked) check.classList.add("ready");
    check.onclick = (e) => { e.stopPropagation(); workOp(p.slug, { op: "check", id: st.id, checked: !st.checked }); };
    head.append(check);
    head.append(el("span", "work-stage-name", (st.current ? "→ " : "") + st.text));
    if (st.ready && !st.checked) head.append(el("span", "work-ready", "ready"));
    // stage money triplet: est (click-to-edit the stage's OWN est — the
    // not-yet-broken-down remainder) · committed · paid + unestimated count
    const money = el("span", "work-money");
    money.append(estSlot(p, st.id, st.est, st.estTotal, true));
    if (st.committed > 0 || st.paid > 0) money.append(el("span", "", " · " + fmtMoney(st.committed) + " committed · " + fmtMoney(st.paid) + " paid"));
    if (st.unestimated > 0) money.append(el("span", "work-unest", st.unestimated + " unestimated"));
    money.onclick = (e) => e.stopPropagation();
    head.append(money);
    const span = (p.schedule || []).find((x) => x.id === st.id);
    if (span) head.append(el("span", "work-span", span.start.slice(5) + " → " + span.end.slice(5) + (span.pinned ? " ✓" : "")));
    if (!st.current) {
      head.classList.add("toggle");
      head.append(el("span", "o-st-caret", open ? "▾" : "▸"));
      head.onclick = () => { workOpen[p.slug + ":" + st.id] = !open; renderPropertyPage(p.slug); };
    }
    // hover delete for stages (inline y/n)
    head.append(workDeleteBtn(p, st.id));
    stageEl.append(head);

    if (open) {
      const list = el("div", "work-todos");
      (st.todos || []).forEach((td) => list.append(workTodoRow(p, st, td)));
      list.append(todoComposer(p, st));
      stageEl.append(list);
    }
    wrap.append(stageEl);
  });
  wrap.append(ghostInput("＋ stage", "work-add-stage", (v) => workOp(p.slug, { op: "add-stage", text: v }), "stage name…"));
  return wrap;
}

// todoComposer: the "＋ todo" ghost expands to text + optional $ price +
// optional who — one flow for the common case "task + firm price". Price
// alone = [est::]; price + who = firm (accepted bid tethered to the new task).
function todoComposer(p, st) {
  const ghost = el("button", "o-ghost work-add", "＋ todo");
  ghost.onclick = (e) => {
    e.stopPropagation();
    const box = el("div", "todo-compose");
    const text = inputEl("what needs to happen…");
    text.classList.add("o-edit", "tc-text");
    const amt = moneyInput("$ price");
    const who = contractorAutocomplete("who (firm)…");
    const cancel = () => box.replaceWith(ghost);
    const save = async () => {
      const t = text.value.trim();
      if (!t) { cancel(); return; }
      const amount = parseFloat(amt.value) || 0;
      const name = who.value();
      try {
        const resp = await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
          { op: "add-todo", stageId: st.id, text: t });
        if (amount) {
          // find the new todo's id in the response (last text match in this stage)
          const stage = ((resp && resp.work) || []).find((s) => s.id === st.id);
          const match = ((stage && stage.todos) || []).filter((td) => td.text === t).pop();
          if (match) {
            await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
              { op: "set-field", id: match.id, field: "est", value: String(amount) });
            if (name) {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger",
                { type: "bid", status: "accepted", contractor: name, amount, workId: match.id });
            }
          }
        }
        renderPropertyPage(p.slug);
      } catch (err) { showToast((err.message || "Couldn't add task").slice(0, 80)); }
    };
    [text, amt, who.el.querySelector("input")].forEach((inp) => inp.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") save();
      else if (ev.key === "Escape") cancel();
    }));
    const add = el("button", "pill lg-add", "add");
    add.onclick = save;
    box.append(text, amt, who.el, add);
    ghost.replaceWith(box);
    text.focus();
  };
  return ghost;
}

function workTodoRow(p, st, td) {
  const row = el("div", "work-todo" + (td.checked ? " done" : ""));
  const check = el("button", "check wk-check" + (td.checked ? " on" : ""), td.checked ? "✓" : "○");
  check.onclick = (e) => { e.stopPropagation(); workOp(p.slug, { op: "check", id: td.id, checked: !td.checked }); };
  row.append(check);

  const label = el("span", "work-todo-text", td.text);
  label.title = "click to edit";
  label.onclick = () => {
    const input = inputEl(""); input.value = td.text; input.classList.add("work-edit");
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && input.value.trim()) workOp(p.slug, { op: "edit", id: td.id, text: input.value });
      else if (ev.key === "Escape") input.replaceWith(label);
    });
    input.addEventListener("blur", () => { if (input.parentNode) input.replaceWith(label); });
    label.replaceWith(input);
    input.focus();
  };
  row.append(label);

  // ONE price slot: paid → firm → est → empty (firm = accepted bid under the hood)
  row.append(priceSlot(p, td));

  // requested/received bid chips still render (accept/decline); accepted is the slot's job
  (td.bids || []).forEach((b) => {
    if (b.status === "accepted") return;
    row.append(bidChipEl(p, b));
  });

  const acts = el("span", "work-acts");
  const more = el("button", "uw-x work-more-btn", "⋯");
  more.title = "more…";
  more.onclick = (e) => {
    e.stopPropagation();
    const open = acts.querySelector(".work-more");
    if (open) { open.remove(); return; }
    const m = quietBtn("request bid…", () => { m.remove(); toggleBidForm(row, p, st, td); });
    m.classList.add("work-more");
    more.after(m);
  };
  acts.append(more);
  acts.append(workDeleteBtn(p, td.id));
  row.append(acts);
  return row;
}

// priceSlot: one money field per task. A number alone = estimate (budget); a
// number + name = FIRM price — written as an accepted bid tethered to the
// task, so committed and payment draws work unchanged. Editing a firm price
// mutates the SAME ledger row (fail-closed), never duplicates.
function priceSlot(p, td) {
  const firm = (td.bids || []).find((b) => b.status === "accepted");
  return moneySlot({
    value: {
      paid: td.paid, committed: td.committed, est: td.est,
      firm: firm && { amount: firm.amount, who: firm.who },
      checked: td.checked, unreconciled: td.unreconciled,
    },
    title: () => td.checked && (td.unreconciled || 0) > 0
      ? "done — " + fmtMoney(td.unreconciled) + " unreconciled: link the bank payment in the statement workbench"
      : "price — number alone = estimate · add a name = firm (committed)",
    editor: (_, revert) => {
      const box = el("span", "price-edit");
      const amt = moneyInput("$", firm ? firm.amount : td.est);
      const who = contractorAutocomplete("who (firm)…");
      if (firm && firm.who) who.setValue(firm.who);
      const save = async () => {
        const amount = parseFloat(amt.value) || 0;
        const name = who.value();
        try {
          if (amount && name) {
            if (firm) {
              if (firm.amount !== amount || (firm.who || "") !== name) {
                await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger/mutate",
                  { original: firm.row, replacement: { ...firm.row, contractor: name, amount } });
              }
            } else {
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/ledger",
                { type: "bid", status: "accepted", contractor: name, amount, workId: td.id });
            }
            if (!(td.est > 0)) { // plan defaults to the firm price; never overwrite an estimate
              await postJSONOk("/api/properties/" + encodeURIComponent(p.slug) + "/work",
                { op: "set-field", id: td.id, field: "est", value: String(amount) });
            }
            renderPropertyPage(p.slug);
          } else {
            workOp(p.slug, { op: "set-field", id: td.id, field: "est", value: amt.value.trim() });
          }
        } catch (err) { showToast((err.message || "Couldn't save price").slice(0, 80)); }
      };
      moneyEditKeys(amt, save, revert);
      moneyEditKeys(who.el.querySelector("input"), save, revert);
      const ok = el("button", "pill lg-add", "set");
      ok.onclick = save;
      box.append(amt, who.el, ok);
      return { el: box, focus: () => amt.focus() };
    },
  });
}

// estSlot: the inline estimate field on every work row. Shows the value (or a
// muted "est —"); click → type → Enter writes [est:: N] (empty clears). For
// stages, `total` may include todo sums — display shows the total, editing
// edits the stage's OWN est.
function estSlot(p, id, own, total, isStage) {
  return moneySlot({
    value: { est: total },
    emptyLabel: "est —",
    title: isStage ? "stage estimate (own, on top of todo estimates)" : "estimate",
    editor: (_, revert) => {
      const input = moneyInput("est $", own);
      const save = () => workOp(p.slug, { op: "set-field", id, field: "est", value: input.value.trim() });
      moneyEditKeys(input, save, revert);
      input.addEventListener("blur", () => { if (input.parentNode) revert(); });
      return { el: input, focus: () => input.focus() };
    },
  });
}

function quietBtn(text, onclick) {
  const b = el("button", "stmt-hint", text);
  b.onclick = (e) => { e.stopPropagation(); onclick(); };
  return b;
}

function workDeleteBtn(p, id) {
  const holder = el("span", "work-del");
  const x = el("button", "uw-x", "✕");
  x.title = "remove";
  x.onclick = (e) => {
    e.stopPropagation();
    // cascade preview: tethered bids die with the node; paid expenses survive
    let bids = 0, paid = 0;
    (p.ledger || []).forEach((r) => {
      if (!r.workId || (r.workId !== id && !r.workId.startsWith(id + "/"))) return;
      if (r.type === "bid") bids += r.amount;
      else if (r.type === "expense") paid += r.amount;
    });
    let ask = "delete?";
    if (bids > 0 && paid > 0) ask = "delete? (removes " + fmtMoney(bids) + " bid · " + fmtMoney(paid) + " paid stays)";
    else if (bids > 0) ask = "delete? (removes " + fmtMoney(bids) + " bid)";
    else if (paid > 0) ask = "delete? (" + fmtMoney(paid) + " paid stays in ledger)";
    const yes = quietBtn(ask, () => workOp(p.slug, { op: "delete", id }));
    const no = quietBtn("no", () => { yes.remove(); no.remove(); x.hidden = false; });
    x.hidden = true;
    holder.append(yes, no);
  };
  holder.append(x);
  return holder;
}

// openTodoOptions builds the ledger picker options: open todos first, then the
// current row's tether even if checked (so edits don't lose it).
function openTodoOptions(p, includeId) {
  const opts = [["", "— no work link —"]];
  (p.work || []).forEach((st) => {
    (st.todos || []).forEach((td) => {
      if (!td.checked || td.id === includeId) opts.push([td.id, st.text + " · " + td.text]);
    });
    if (includeId && st.id === includeId) opts.push([st.id, st.text + " (stage)"]);
  });
  return opts;
}
