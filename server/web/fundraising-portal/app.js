let data = { opportunities: [], statuses: [], interests: [] };
let selected = null;
const $ = (id) => document.getElementById(id);

async function api(path, options) {
  const response = await fetch(path, options);
  if (response.status === 401) { location.reload(); throw new Error("Sign in required"); }
  if (!response.ok) throw new Error((await response.text()).trim() || "Request failed");
  return response.json();
}

async function load() {
  try {
    const [me, fundraising] = await Promise.all([api("/api/me"), api("/api/fundraising")]);
    $("identity").textContent = me.name || me.email;
    data = fundraising; render();
  } catch (error) { notice(error.message, true); }
}

function notice(message, error) {
  $("notice").textContent = message || "";
  $("notice").style.color = error ? "#ff8577" : "";
  if (message) setTimeout(() => { if ($("notice").textContent === message) $("notice").textContent = ""; }, 3500);
}

function render() {
  const query = $("search").value.trim().toLowerCase();
  const host = $("rows"); host.innerHTML = "";
  data.opportunities.filter((op) => !query || [op.firm, op.website, op.source, op.lastTouchpoint, op.nextStep, op.notes].concat(op.people || []).join(" ").toLowerCase().includes(query)).forEach((op) => {
    const row = document.createElement("div"); row.className = "row" + (selected === op.id ? " selected" : "");
    row.innerHTML = `<span class="cell firm"></span><span class="cell"></span><span class="cell"></span><span class="cell"></span>`;
    row.children[0].textContent = op.firm;
    if (op.archived) { const sub = document.createElement("span"); sub.className = "sub"; sub.textContent = "ARCHIVED"; row.children[0].append(sub); }
    row.children[1].textContent = (op.people || []).join("; ") || "—";
    row.children[2].textContent = op.lastTouchpoint || "—";
    if (op.lastTouchpointDate) { const sub = document.createElement("span"); sub.className = "sub"; sub.textContent = op.lastTouchpointDate; row.children[2].append(sub); }
    row.children[3].textContent = op.nextStep || "—";
    if (op.nextStepDue) { const sub = document.createElement("span"); sub.className = "sub"; sub.textContent = "due " + op.nextStepDue; row.children[3].append(sub); }
    row.onclick = () => { selected = selected === op.id ? null : op.id; render(); };
    host.append(row);
  });
  renderEditor();
}

function renderEditor() {
  const host = $("editor"); host.innerHTML = "";
  const op = data.opportunities.find((item) => item.id === selected);
  if (!op) { const p = document.createElement("p"); p.textContent = "Select an opportunity to edit it."; host.append(p); return; }
  const head = document.createElement("div"); head.className = "editor-head";
  const title = document.createElement("h2"); title.textContent = op.firm;
  const close = document.createElement("button"); close.className = "quiet"; close.textContent = "×"; close.onclick = () => { selected = null; render(); };
  head.append(title, close); host.append(head);
  field(host, "Firm", "firm", op.firm);
  field(host, "Website", "website", op.website || "", "url");
  field(host, "People (semicolon-separated)", "people", (op.people || []).join("; "));
  field(host, "Source", "source", op.source || "");
  selectField(host, "Status", "status", op.status, data.statuses);
  selectField(host, "Interest", "interest", op.interest, data.interests);
  field(host, "Amount", "amount", op.amount || "", "number");
  field(host, "Currency", "currency", op.currency || "USD");
  field(host, "Last touchpoint", "lastTouchpoint", op.lastTouchpoint || "");
  field(host, "Last touchpoint date", "lastTouchpointDate", op.lastTouchpointDate || "", "date");
  readonly(host, "Computed last touchpoint", op.computedLastTouchpoint || "—");
  field(host, "Next step", "nextStep", op.nextStep || "");
  field(host, "Next step due", "nextStepDue", op.nextStepDue || "", "date");
  field(host, "Notes", "notes", op.notes || "", "textarea");
  readonly(host, "Archived", op.archived ? "yes — only the owner can restore or remove this record" : "no");
}

function field(host, label, key, value, type) {
  const wrap = document.createElement("div"); wrap.className = "field";
  const lab = document.createElement("label"); lab.textContent = label;
  const input = document.createElement(type === "textarea" ? "textarea" : "input");
  if (type && type !== "textarea") input.type = type;
  input.value = value; let old = input.value;
  input.onblur = async () => { if (input.value === old) return; await save(key, key === "people" ? input.value.split(";").map((x) => x.trim()).filter(Boolean) : input.value); old = input.value; };
  wrap.append(lab, input); host.append(wrap);
}

function selectField(host, label, key, value, options) {
  const wrap = document.createElement("div"); wrap.className = "field";
  const lab = document.createElement("label"); lab.textContent = label;
  const select = document.createElement("select");
  options.forEach((name) => { const option = document.createElement("option"); option.value = name; option.textContent = name; option.selected = name === value; select.append(option); });
  select.onchange = () => save(key, select.value); wrap.append(lab, select); host.append(wrap);
}

function readonly(host, label, value) {
  const wrap = document.createElement("div"); wrap.className = "field";
  const lab = document.createElement("label"); lab.textContent = label;
  const text = document.createElement("div"); text.className = "readonly"; text.textContent = value;
  wrap.append(lab, text); host.append(wrap);
}

async function save(key, value) {
  try {
    const updated = await api("/api/fundraising/" + encodeURIComponent(selected), { method:"PATCH", headers:{"Content-Type":"application/json"}, body:JSON.stringify({[key]:value}) });
    const index = data.opportunities.findIndex((op) => op.id === updated.id); data.opportunities[index] = updated; notice("Saved"); render();
  } catch (error) { notice(error.message, true); }
}

$("search").oninput = render;
$("add").onclick = async () => {
  const firm = prompt("Firm or opportunity name"); if (!firm || !firm.trim()) return;
  try { const op = await api("/api/fundraising/item", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({firm:firm.trim()}) }); data.opportunities.push(op); selected = op.id; render(); notice("Opportunity added"); } catch (error) { notice(error.message, true); }
};
$("logout").onclick = () => fetch("/oauth2/logout", { method:"POST" }).finally(() => location.reload());
load();
