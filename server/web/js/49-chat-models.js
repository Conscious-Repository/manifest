// MODEL · EFFORT · PERMISSIONS — one picker for every agent (server
// chat_models.go is the catalog). The composer shows what the conversation is
// running with; the picker changes it in the agent's own terms:
//   native agents (Hermes)  the next message names model, provider and effort
//   a coding draft          the launch flags, before the first message
//   Claude Code, running    its own /model and /effort commands, confirmed
//                           from its transcript (never assumed)
//   Codex, running          its own interactive picker, in the live strip
// /model, /effort and /permissions open this picker on every agent.
let chatModelCatalog = null, chatModelCatalogAt = 0, chatModelCatalogPending = null;
const chatModelRequested = new Map(); // coding session id -> {model, effort, at}: sent, not yet observed

async function chatLoadModelCatalog(force) {
  if (!force && chatModelCatalog && Date.now() - chatModelCatalogAt < 60000) return chatModelCatalog;
  if (chatModelCatalogPending) return chatModelCatalogPending;
  chatModelCatalogPending = fetch("/api/chat/models", {cache: "no-store"}).then(r => { if (!r.ok) throw Error("Model list unavailable"); return r.json(); })
    .then(d => { chatModelCatalog = d.agents || {}; chatModelCatalogAt = Date.now(); return chatModelCatalog; })
    .finally(() => { chatModelCatalogPending = null; });
  return chatModelCatalogPending;
}

// chatModelContext — what the composer addresses right now, or null when the
// next message goes somewhere this picker does not own (another agent chosen
// in the header, a portal, spirits).
function chatModelContext() {
  if (chatIsPortal() || !chatAgent) return null;
  if (chatIsTerm()) {
    const o = chatTermOpen;
    if (!o || o.se.id !== chatOpenId) {
      if (chatOpenId) return null;
      return {kind: "landing", agent: chatAgent, launch: {model: chatRecall("manifest.chatTermModel." + chatAgent), effort: chatRecall("manifest.chatTermEffort." + chatAgent), permission: chatRecall("manifest.chatTermPermission." + chatAgent)}};
    }
    const se = o.se, rec = chatRecipients.get(se.kind + "/" + se.id);
    if (rec) return null;
    const draft = se.launchPhase === "draft";
    return {kind: draft ? "draft" : "live", agent: se.kind, id: se.id, observed: o.settings || null, launch: o.launch || {model: se.model || "", effort: se.effort || "", permission: se.permission || ""}, requested: chatModelRequested.get(se.id) || null};
  }
  const entry = chatRosterEntry(chatAgent);
  if (!entry || (entry.backend && entry.backend !== "hermes") || (!entry.backend && !entry.durableSend)) return null;
  const key = chatAgent + "/" + (chatOpenId || "new"), rec = chatRecipients.get(key);
  if (rec && rec.agent !== chatAgent) return null;
  return {kind: "hermes", agent: chatAgent, key, current: {model: rec?.model || chatCurSession?.model || entry.model || "", provider: rec?.provider || "", effort: rec?.effort || ""}};
}

// chatModelState — the model/effort/permission to show, and whether each is
// observed (the agent recorded it), launch (what it started with), requested
// (sent, not yet observed) or default.
function chatModelState(ctx, cat) {
  const out = {model: "", effort: "", permission: "", source: {}};
  if (!ctx) return out;
  const pick = (field, ...pairs) => { for (const [v, src] of pairs) if (v) { out[field] = v; out.source[field] = src; return; } out.source[field] = "default"; };
  if (ctx.kind === "hermes") {
    pick("model", [ctx.current.model, "chosen"], [cat?.default, "default"]);
    pick("effort", [ctx.current.effort, "chosen"]);
    out.provider = ctx.current.provider;
    return out;
  }
  const req = ctx.requested && Date.now() - ctx.requested.at < 120000 ? ctx.requested : null;
  const obs = ctx.observed || {};
  const modelSeen = req?.model && obs.model && chatModelMatches(obs.model, req.model);
  pick("model", [req && !modelSeen ? req.model : "", "requested"], [obs.model, "observed"], [ctx.launch?.model, "launch"], [cat?.default, "default"]);
  pick("effort", [req?.effort && req.effort !== obs.effort ? req.effort : "", "requested"], [obs.effort, "observed"], [ctx.launch?.effort, "launch"]);
  pick("permission", [obs.permission, "observed"], [ctx.launch?.permission, "launch"], [cat?.defaultPermission, "default"]);
  return out;
}
// A requested alias ("opus") is seen once the recorded full id contains it.
function chatModelMatches(seen, wanted) { seen = String(seen).toLowerCase(); wanted = String(wanted).toLowerCase(); return seen === wanted || seen.includes(wanted); }

function chatModelLabel(cat, id) {
  if (!id) return "Default model";
  const m = (cat?.models || []).find(m => m.id === id);
  return m?.label || shortModel(id);
}
function chatPermissionLabel(cat, id) {
  if (!id) return "Default access";
  return (cat?.permissions || []).find(p => p.id === id)?.label || id;
}

// ---- composer chips ----
function chatModelChips(host) {
  const ctx = chatModelContext();
  let chip = host.querySelector(".chat-composer-model"), perm = host.querySelector(".chat-composer-permission");
  if (!ctx) { chip?.remove(); perm?.remove(); return false; }
  const cat = chatModelCatalog?.[ctx.agent];
  if (!chatModelCatalog) chatLoadModelCatalog().then(() => chatModelChipsRefresh()).catch(() => {});
  const st = chatModelState(ctx, cat);
  if (!chip) {
    chip = el("button", "sprt-quiet chat-composer-model"); chip.type = "button";
    chip.onclick = () => chatOpenModelPicker("model");
    host.append(chip);
  }
  const effort = st.effort ? " · " + st.effort : "";
  chip.textContent = chatModelLabel(cat, st.model) + effort + (st.source.model === "requested" || st.source.effort === "requested" ? " …" : "") + " ⌄";
  const how = {observed: "as the agent last recorded it", launch: "as launched", requested: "requested; waiting for the agent to confirm", chosen: "for the next message", default: "the agent's default"};
  chip.title = "Model " + (st.model || "default") + " (" + how[st.source.model] + ")" + (st.effort ? " · effort " + st.effort + " (" + how[st.source.effort] + ")" : "") + " · /model";
  chip.setAttribute("aria-label", "Model and effort: " + chip.title);
  chip.dataset.source = st.source.model;
  if (ctx.kind === "hermes") { perm?.remove(); return true; }
  if (!perm) {
    perm = el("button", "sprt-quiet chat-composer-permission"); perm.type = "button";
    perm.onclick = () => chatOpenModelPicker("permission");
    host.append(perm);
  }
  const danger = (cat?.permissions || []).find(p => p.id === st.permission)?.danger;
  perm.textContent = chatPermissionLabel(cat, st.permission);
  perm.classList.toggle("is-danger", !!danger);
  perm.title = "Permissions: " + chatPermissionLabel(cat, st.permission) + " (" + how[st.source.permission] + ") · /permissions";
  perm.setAttribute("aria-label", perm.title);
  return true;
}
function chatModelChipsRefresh() {
  const host = document.getElementById("chatComposer");
  if (host) chatGoalBar(host);
  if (host?.dataset.built && typeof chatPolishComposer === "function") chatPolishComposer(host);
}

// ---- the picker ----
let chatModelPickerEl = null;
function chatCloseModelPicker() {
  if (!chatModelPickerEl) return;
  const back = chatModelPickerEl._return;
  chatModelPickerEl.remove(); chatModelPickerEl = null;
  (back && back.isConnected ? back : document.querySelector("#chatComposer textarea"))?.focus({preventScroll: true});
}
async function chatOpenModelPicker(focus = "model") {
  const ctx = chatModelContext();
  if (!ctx) { showToast("This message goes to another agent. Choose it above the conversation."); return; }
  chatCloseModelPicker();
  const composer = document.getElementById("chatComposer");
  if (!composer) return;
  const box = el("div", "chat-model-picker"); box.setAttribute("role", "dialog"); box.setAttribute("aria-label", "Model, effort and permissions");
  box._return = document.activeElement;
  chatModelPickerEl = box;
  const status = el("p", "chat-model-status"); status.setAttribute("role", "status");
  box.append(el("p", "chat-model-loading", "Loading models…"));
  composer.append(box);
  let catalog;
  try { catalog = await chatLoadModelCatalog(); } catch (e) { box.replaceChildren(el("p", "chat-model-status", "Model list unavailable. Try again, or type /model with a model name.")); return; }
  if (chatModelPickerEl !== box) return;
  const cat = catalog[ctx.agent];
  if (!cat) { box.replaceChildren(el("p", "chat-model-status", "No model list is available for this agent.")); return; }
  const st = chatModelState(ctx, cat);
  let chosen = {model: st.model || "", provider: st.provider || "", effort: st.effort || "", permission: st.permission || ""};
  box.replaceChildren();
  // model list: search + provider groups
  const search = el("input", "chat-model-search"); search.type = "search"; search.placeholder = "Search models…"; search.setAttribute("aria-label", "Search models");
  const list = el("div", "chat-model-list"); list.setAttribute("role", "listbox"); list.setAttribute("aria-label", "Models"); list.tabIndex = -1;
  const efforts = el("div", "chat-model-efforts"); efforts.setAttribute("role", "radiogroup"); efforts.setAttribute("aria-label", "Effort");
  const perms = el("div", "chat-model-perms"); perms.setAttribute("role", "radiogroup"); perms.setAttribute("aria-label", "Permissions");
  const live = ctx.kind === "live", codexLive = live && ctx.agent === "codex", claudeLive = live && ctx.agent === "claude";
  const modelEditable = !codexLive, effortEditable = !codexLive, permEditable = !live && !!cat.permissions?.length;
  let active = -1, rows = [];
  const modelOf = () => cat.models.find(m => m.id === chosen.model && (!chosen.provider || !m.provider || m.provider === chosen.provider)) || cat.models.find(m => m.id === chosen.model);
  const paintList = () => {
    list.replaceChildren(); rows = [];
    const q = search.value.trim().toLowerCase();
    let group = null;
    for (const m of cat.models) {
      const hay = [m.id, m.label, m.provider, m.description].filter(Boolean).join(" ").toLowerCase();
      if (q && !hay.includes(q)) continue;
      const g = m.description && cat.backend === "hermes" ? m.description : m.provider || "";
      if (g !== group) { group = g; if (g) list.append(el("div", "chat-model-group micro-label", g)); }
      const row = el("div", "chat-model-row"); row.setAttribute("role", "option"); row.id = "chatModelOpt-" + rows.length;
      const selected = m.id === chosen.model && (cat.backend !== "hermes" || !chosen.provider || m.provider === chosen.provider);
      row.setAttribute("aria-selected", String(selected));
      const name = el("span", "chat-model-name", m.label || m.id);
      const meta = [];
      if (m.id !== m.label && m.label) meta.push(m.id);
      if (m.context) meta.push(Math.round(m.context / 1000) + "k context");
      if (cat.backend !== "hermes" && m.description) meta.push(m.description);
      if (m.id === cat.default) meta.push("default");
      row.append(name, el("span", "chat-model-meta", meta.join(" · ")));
      row.onmousedown = e => e.preventDefault();
      row.onclick = () => { if (!modelEditable) return; chosen.model = m.id; chosen.provider = cat.backend === "hermes" ? (m.provider || "") : ""; paintList(); paintEfforts(); paintStatus(); };
      rows.push({row, m});
      list.append(row);
    }
    if (!rows.length) list.append(el("p", "chat-model-empty", "No model matches."));
    active = Math.max(0, rows.findIndex(r => r.row.getAttribute("aria-selected") === "true"));
    rows[active]?.row.classList.add("is-active");
    if (rows[active]) search.setAttribute("aria-activedescendant", rows[active].row.id);
  };
  const effortChoices = () => (modelOf()?.efforts?.length ? modelOf().efforts : cat.efforts || []);
  const paintEfforts = () => {
    efforts.replaceChildren(el("span", "chat-model-section", "Effort"));
    const list = effortChoices();
    if (!list.length) { efforts.append(el("span", "chat-model-meta", "This model has no effort setting.")); return; }
    const def = el("button", "chat-model-seg", "default"); def.type = "button"; def.setAttribute("role", "radio"); def.setAttribute("aria-checked", String(!chosen.effort));
    def.title = modelOf()?.defaultEffort ? "The model's default (" + modelOf().defaultEffort + ")" : "The agent's configured default";
    def.onclick = () => { chosen.effort = ""; paintEfforts(); paintStatus(); efforts.querySelector('[aria-checked="true"]')?.focus(); };
    def.disabled = !effortEditable || claudeLive; efforts.append(def);
    for (const e of list) {
      const b = el("button", "chat-model-seg", e.id); b.type = "button"; b.setAttribute("role", "radio"); b.setAttribute("aria-checked", String(chosen.effort === e.id));
      if (e.description) b.title = e.description;
      b.disabled = !effortEditable;
      b.onclick = () => { chosen.effort = e.id; paintEfforts(); paintStatus(); efforts.querySelector('[aria-checked="true"]')?.focus(); };
      efforts.append(b);
    }
  };
  const paintPerms = () => {
    perms.replaceChildren();
    if (!cat.permissions?.length) return;
    perms.append(el("span", "chat-model-section", "Permissions"));
    if (!permEditable) {
      const note = live ? (ctx.agent === "claude" ? "Set at launch. Currently " + chatPermissionLabel(cat, chosen.permission) + ". Shift+Tab in Terminal cycles Claude's modes." : "Set at launch. Codex changes access in its own /permissions picker.") : "";
      perms.append(el("span", "chat-model-meta", note));
      if (codexLive) { const b = el("button", "sprt-quiet chat-model-native", "Open Codex /permissions"); b.type = "button"; b.onclick = () => chatModelNative("/permissions"); perms.append(b); }
      return;
    }
    const def = el("button", "chat-model-seg", "default"); def.type = "button"; def.setAttribute("role", "radio"); def.setAttribute("aria-checked", String(!chosen.permission)); def.title = "The CLI's configured default";
    def.onclick = () => { chosen.permission = ""; paintPerms(); paintStatus(); perms.querySelector('[aria-checked="true"]')?.focus(); };
    perms.append(def);
    for (const p of cat.permissions) {
      const b = el("button", "chat-model-seg" + (p.danger ? " is-danger" : ""), p.label); b.type = "button"; b.setAttribute("role", "radio"); b.setAttribute("aria-checked", String(chosen.permission === p.id)); b.title = p.description;
      b.onclick = () => { chosen.permission = p.id; paintPerms(); paintStatus(); perms.querySelector('[aria-checked="true"]')?.focus(); };
      perms.append(b);
    }
  };
  const paintStatus = () => {
    const words = {
      hermes: "Applies to your next message in this chat.",
      landing: "Applies when this new coding session launches.",
      draft: "Applies when your first message launches this session.",
      live: claudeLive ? "Sends Claude Code's own /model and /effort. The chip confirms once Claude records the change." : "Codex switches model and effort in its own picker, shown below the conversation.",
    };
    const p = (cat.permissions || []).find(p => p.id === chosen.permission);
    status.textContent = words[ctx.kind] + (p?.danger && permEditable ? " " + p.label + " runs everything without asking." : "");
  };
  const actions = el("div", "chat-model-actions");
  const cancel = el("button", "sprt-quiet", "Cancel"); cancel.type = "button"; cancel.onclick = chatCloseModelPicker;
  const apply = el("button", "sprt-quiet chat-dialog-primary chat-model-apply", codexLive ? "Open Codex /model" : "Apply"); apply.type = "button";
  apply.onclick = async () => {
    apply.disabled = true;
    try { if (await chatApplyModelChoice(ctx, cat, st, chosen)) chatCloseModelPicker(); }
    catch (e) { status.textContent = e.message || "Not applied."; }
    finally { if (apply.isConnected) apply.disabled = false; }
  };
  actions.append(cancel, apply);
  const hint = el("p", "chat-model-hint", "↑↓ model · ←→ effort · Enter apply · Esc close");
  box.append(search, list, efforts, perms, status, actions, hint);
  search.addEventListener("input", paintList);
  box.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); chatCloseModelPicker(); return; }
    if (e.target.closest(".chat-model-seg,.chat-model-actions button,.chat-model-native")) { if (e.key === "Enter" || e.key === " ") return; }
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault(); if (!rows.length) return;
      rows[active]?.row.classList.remove("is-active");
      active = (active + (e.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length;
      const r = rows[active]; r.row.classList.add("is-active"); r.row.scrollIntoView({block: "nearest"}); search.setAttribute("aria-activedescendant", r.row.id);
      if (modelEditable) { chosen.model = r.m.id; chosen.provider = cat.backend === "hermes" ? (r.m.provider || "") : ""; rows.forEach(x => x.row.setAttribute("aria-selected", String(x === r))); paintEfforts(); paintStatus(); }
    } else if ((e.key === "ArrowLeft" || e.key === "ArrowRight") && e.target === search && !search.value && effortEditable) {
      e.preventDefault();
      const ids = ["", ...effortChoices().map(x => x.id)], i = ids.indexOf(chosen.effort);
      chosen.effort = ids[Math.min(ids.length - 1, Math.max(0, i + (e.key === "ArrowRight" ? 1 : -1)))];
      paintEfforts(); paintStatus();
    } else if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); apply.click(); }
  });
  // Outside press closes; a repaint inside (effort, permission) never does.
  const outside = e => { if (chatModelPickerEl !== box) { document.removeEventListener("pointerdown", outside, true); return; } if (!box.contains(e.target) && !e.target.closest?.(".chat-composer-model,.chat-composer-permission")) chatCloseModelPicker(); };
  document.addEventListener("pointerdown", outside, true);
  paintList(); paintEfforts(); paintPerms(); paintStatus();
  (focus === "permission" && permEditable ? perms.querySelector('[aria-checked="true"]') : search).focus({preventScroll: true});
}

async function chatModelNative(command) {
  chatCloseModelPicker();
  const ok = await chatTermSend(command, {command: true});
  if (ok && chatTermOpen) chatOpenTerminalPane(chatTermOpen.se);
  return ok;
}

// chatApplyModelChoice — apply in the agent's own terms. Returns true when
// the picker can close.
async function chatApplyModelChoice(ctx, cat, before, chosen) {
  if (ctx.kind === "hermes") {
    const rec = {agent: ctx.agent, model: chosen.model, provider: chosen.provider, effort: chosen.effort};
    chatRecipients.set(ctx.key, rec);
    if (chatDraftKey === ctx.key) chatCaptureSyncedDraft(ctx.key);
    else { const state = chatSyncedDrafts.get(ctx.key); if (state) state.set({...state.value, recipient: rec}); }
    chatModelChipsRefresh();
    return true;
  }
  if (ctx.kind === "landing") {
    try {
      localStorage.setItem("manifest.chatTermModel." + ctx.agent, chosen.model);
      localStorage.setItem("manifest.chatTermEffort." + ctx.agent, chosen.effort);
      localStorage.setItem("manifest.chatTermPermission." + ctx.agent, chosen.permission);
    } catch (e) { throw Error("This browser cannot keep the choice. The session will launch with its defaults."); }
    const select = document.querySelector('.chat-landing-cwd[aria-label="Model"]');
    if (select && select.value !== chosen.model) { if (![...select.options].some(o => o.value === chosen.model)) { const o = el("option", "", chosen.model); o.value = chosen.model; select.append(o); } select.value = chosen.model; }
    chatModelChipsRefresh();
    return true;
  }
  if (ctx.kind === "draft") {
    const body = {model: chosen.model, effort: chosen.effort, permission: chosen.permission};
    const r = await fetch(chatTermBase(ctx.id), {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
    if (!r.ok) throw Error((await r.text()).trim() || "Launch settings were not saved.");
    const se = await r.json();
    if (chatTermOpen?.se.id === ctx.id) { Object.assign(chatTermOpen.se, {model: se.model, effort: se.effort, permission: se.permission}); chatTermOpen.launch = {model: se.model || "", effort: se.effort || "", permission: se.permission || ""}; }
    chatModelChipsRefresh();
    return true;
  }
  if (ctx.agent === "codex") { await chatModelNative("/model"); return true; }
  // Claude Code, running: its own commands. Recorded as requested until the
  // transcript shows the agent running with them.
  const sends = [];
  if (chosen.model && chosen.model !== before.model) sends.push("/model " + chosen.model);
  if (chosen.effort && chosen.effort !== before.effort) sends.push("/effort " + chosen.effort);
  if (!sends.length) return true;
  for (const command of sends) if (!await chatTermSend(command, {command: true})) throw Error("Claude did not take " + command + ". Nothing else was sent.");
  chatModelRequested.set(ctx.id, {model: sends.some(s => s.startsWith("/model")) ? chosen.model : "", effort: sends.some(s => s.startsWith("/effort")) ? chosen.effort : "", at: Date.now()});
  chatModelChipsRefresh();
  return true;
}

// chatSurfaceCommand — /model, /effort and /permissions on every agent. A bare
// command opens the picker. With an argument: a native agent applies it
// directly; a coding agent receives its own command unchanged.
async function chatSurfaceCommand(text, clear) {
  const nav = /^\/(new|tile)\s*$/i.exec(text);
  if (nav && !chatEmbedded && chatAgent && !chatIsPortal()) {
    clear();
    if (nav[1].toLowerCase() === "new") location.hash = chatNewHash();
    else { if (typeof chatTilesPendingFrom !== "undefined" && chatOpenId) chatTilesPendingFrom = location.hash; location.hash = "#/chat/tiles"; }
    return true;
  }
  const goal = /^\/goal(?:\s+([\s\S]*))?$/i.exec(text);
  if (goal) { const ctx = chatModelContext(); if (ctx?.kind === "hermes") { await chatGoalCommand((goal[1] || "").trim(), clear); return true; } return false; }
  const m = /^\/(model|effort|permissions?)(?:\s+(\S+))?\s*$/i.exec(text);
  if (!m) return false;
  const ctx = chatModelContext();
  if (!ctx) return false;
  const verb = m[1].toLowerCase().replace(/s$/, ""), arg = m[2] || "";
  if (!arg) { clear(); chatOpenModelPicker(verb === "permission" ? "permission" : "model"); return true; }
  if (ctx.kind === "live") return false; // the agent's own command, verbatim
  const catalog = await chatLoadModelCatalog().catch(() => null), cat = catalog?.[ctx.agent];
  if (!cat) { showToast("Model list unavailable; nothing changed."); return true; }
  const st = chatModelState(ctx, cat), chosen = {model: st.model || "", provider: st.provider || "", effort: st.effort || "", permission: st.permission || ""};
  const a = arg.toLowerCase();
  if (verb === "model") {
    const hit = cat.models.find(x => x.id.toLowerCase() === a) || cat.models.find(x => (x.label || "").toLowerCase() === a) || cat.models.filter(x => x.id.toLowerCase().includes(a)).sort((x, y) => x.id.length - y.id.length)[0];
    if (!hit) { showToast("No model named " + arg + ". Type /model to browse."); return true; }
    chosen.model = hit.id; chosen.provider = cat.backend === "hermes" ? hit.provider || "" : "";
  } else if (verb === "effort") {
    const list = (cat.models.find(x => x.id === chosen.model)?.efforts?.length ? cat.models.find(x => x.id === chosen.model).efforts : cat.efforts || []).map(x => x.id);
    if (a !== "default" && !list.includes(a)) { showToast("Effort must be one of: " + ["default", ...list].join(", ") + "."); return true; }
    chosen.effort = a === "default" ? "" : a;
  } else {
    const p = (cat.permissions || []).find(x => x.id.toLowerCase() === a || x.label.toLowerCase() === a);
    if (!p && a !== "default") { showToast(cat.permissions?.length ? "Permission must be one of: " + ["default", ...cat.permissions.map(x => x.id)].join(", ") + "." : "This agent has no permission setting."); return true; }
    chosen.permission = p ? p.id : "";
  }
  try { await chatApplyModelChoice(ctx, cat, st, chosen); clear(); showToast(verb === "model" ? "Model: " + chatModelLabel(cat, chosen.model) : verb === "effort" ? "Effort: " + (chosen.effort || "default") : "Permissions: " + chatPermissionLabel(cat, chosen.permission)); }
  catch (e) { showToast(e.message || "Not applied."); }
  return true;
}

// ---- /goal on a native agent ----
// The objective lives on the conversation (server SetGoal) and rides every
// turn's prompt while active. It never starts a turn by itself.
async function chatSetGoal(goal, state) {
  const agent = chatAgent, id = chatOpenId;
  const r = await fetch(chatBaseFor(agent) + "/" + encodeURIComponent(id) + "/goal", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({goal, state})});
  if (!r.ok) throw Error((await r.text()).trim() || "Goal not saved.");
  const d = await r.json();
  if (chatCurSession?.id === id && chatAgent === agent) { chatCurSession.goal = d.goal || ""; chatCurSession.goalState = d.goalState || ""; }
  chatModelChipsRefresh();
  return d;
}
async function chatGoalCommand(arg, clear) {
  if (!chatOpenId) { showToast("Send a first message, then set this chat's goal."); return; }
  const current = chatCurSession?.goal || "", word = arg.toLowerCase();
  try {
    if (!arg) { showToast(current ? "Goal (" + (chatCurSession.goalState || "active") + "): " + current : "No goal yet. Type /goal and the objective."); clear(); return; }
    if (word === "pause" || word === "resume") {
      if (!current) { showToast("There is no goal to " + word + "."); return; }
      await chatSetGoal(current, word === "pause" ? "paused" : "active"); clear(); showToast(word === "pause" ? "Goal paused. It stays here but is not sent." : "Goal resumed."); return;
    }
    if (word === "clear") { await chatSetGoal("", ""); clear(); showToast("Goal cleared."); return; }
    await chatSetGoal(arg, "active"); clear(); showToast("Goal set. Every message in this chat now carries it.");
  } catch (e) { showToast(e.message || "Goal not saved."); }
}
// chatGoalBar — the conversation's objective above the message, with its
// state and the three controls. Hidden when there is none.
function chatGoalBar(host) {
  const ctx = chatModelContext();
  let bar = host.querySelector(".chat-goal-bar");
  const goal = ctx?.kind === "hermes" && chatCurSession?.id === chatOpenId ? chatCurSession?.goal || "" : "";
  if (!goal) { bar?.remove(); return; }
  if (!bar) { bar = el("div", "chat-goal-bar"); bar.setAttribute("role", "status"); host.prepend(bar); }
  const paused = chatCurSession.goalState === "paused";
  // Rebuild only when the goal changes: a composer repaint must never replace
  // the button under a pointer (focus-stealing re-render class).
  const signature = JSON.stringify([goal, paused]);
  if (bar.dataset.signature === signature) return;
  bar.dataset.signature = signature;
  bar.classList.toggle("is-paused", paused);
  const text = el("span", "chat-goal-text", goal); text.title = goal;
  const label = el("span", "chat-goal-label", paused ? "Goal · paused" : "Goal");
  const toggle = el("button", "chat-goal-act", paused ? "Resume" : "Pause"); toggle.type = "button";
  toggle.onclick = () => chatSetGoal(goal, paused ? "active" : "paused").catch(e => showToast(e.message));
  const clearBtn = el("button", "chat-goal-act", "Clear"); clearBtn.type = "button"; clearBtn.setAttribute("aria-label", "Clear goal");
  clearBtn.onclick = () => chatSetGoal("", "").catch(e => showToast(e.message));
  bar.replaceChildren(label, text, toggle, clearBtn);
}
