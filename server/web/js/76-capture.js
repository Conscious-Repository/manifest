// ---- CAPTURE: the tray (cmd-ctr's Stage, manifest idiom — import P5) ----
// Notes, shared links, and images land as editable cards; the OWNER triages
// each: → todo (existing quick-add), → chat (explicit handoff — the ONLY path
// to an LLM), keep, or bin (30-day trash). Cards live in dataDir until
// promoted; the phone share-sheet posts into /api/capture/share.

let captureItems = [];

function showCapture() {
  loadCapture();
}

async function loadCapture() {
  const host = document.getElementById("captureRows");
  if (!host) return;
  try { captureItems = ((await (await fetch("/api/capture")).json()).items) || []; }
  catch (e) { captureItems = []; }
  renderCapture();
  refreshCaptureBadge();
}

function renderCapture() {
  const host = document.getElementById("captureRows");
  if (!host) return;
  host.innerHTML = "";
  if (!captureItems.length) {
    host.append(emptyRow("Nothing in the tray. Share from your phone, paste an image, or ＋ add."));
    return;
  }
  captureItems.forEach((it) => host.append(captureCardEl(it)));
}

function captureCardEl(it) {
  const card = el("div", "capture-card" + (it.status === "kept" ? " kept" : ""));
  const head = el("div", "capture-head");
  head.append(el("span", "capture-kind", it.kind));
  head.append(el("span", "capture-when", fmtWhen(it.createdAt)));
  if (it.source && it.source !== "manual") head.append(el("span", "capture-src", it.source));
  card.append(head);

  if (it.url) {
    const a = document.createElement("a");
    a.className = "capture-url";
    a.href = it.url;
    a.target = "_blank";
    a.rel = "noopener";
    a.textContent = it.title || it.url;
    card.append(a);
  } else if (it.title) {
    card.append(el("div", "capture-title", it.title));
  }

  (it.media || []).forEach((m) => {
    if ((m.mime || "").startsWith("image/") || /\.(png|jpe?g|gif|webp|heic)$/i.test(m.file)) {
      const img = document.createElement("img");
      img.className = "capture-thumb";
      img.src = "/api/capture/media/" + encodeURIComponent(m.file);
      img.loading = "lazy";
      img.onclick = () => window.open(img.src, "_blank");
      card.append(img);
    } else {
      const a = document.createElement("a");
      a.className = "capture-url";
      a.href = "/api/capture/media/" + encodeURIComponent(m.file);
      a.target = "_blank";
      a.textContent = "📄 " + (m.name || m.file);
      card.append(a);
    }
  });

  // in-place editable text (blur saves)
  const ta = document.createElement("textarea");
  ta.className = "capture-text";
  ta.value = it.text || "";
  ta.rows = Math.min(6, Math.max(1, (it.text || "").split("\n").length));
  ta.placeholder = "notes…";
  ta.addEventListener("blur", async () => {
    if (ta.value === (it.text || "")) return;
    it.text = ta.value;
    try { await postJSONOk("/api/capture/" + encodeURIComponent(it.id) + "/update", { title: it.title || "", text: ta.value }); }
    catch (e) { showToast("Save failed"); }
  });
  card.append(ta);

  const acts = el("div", "capture-acts");
  const summary = () => (it.title || it.text || it.url || "").split("\n")[0].slice(0, 200);
  acts.append(pillLight("→ todo", () => {
    openTodoQuickAdd(summary());
  }));
  acts.append(pillLight("→ chat", () => {
    // the EXPLICIT handoff — nothing reaches an LLM until this press
    const parts = [summary()];
    if (it.url) parts.push(it.url);
    if (it.text && it.text !== summary()) parts.push(it.text);
    chatCompose("concierge", parts.join("\n"));
  }));
  if (it.status !== "kept") {
    acts.append(pillLight("keep", async () => {
      try { await postJSONOk("/api/capture/" + encodeURIComponent(it.id) + "/status", { status: "kept" }); loadCapture(); } catch (e) {}
    }));
  }
  acts.append(pillLight("bin", async () => {
    try { await postJSONOk("/api/capture/" + encodeURIComponent(it.id) + "/dismiss", {}); loadCapture(); } catch (e) {}
  }));
  card.append(acts);
  return card;
}

async function refreshCaptureBadge() {
  const badge = document.getElementById("captureNavBadge");
  if (!badge) return;
  try {
    const d = await (await fetch("/api/capture/badge")).json();
    badge.hidden = !d.open;
    badge.textContent = d.open || "";
  } catch (e) {}
}

// composer: quick add + file picker
function buildCaptureComposer() {
  const host = document.getElementById("captureComposer");
  if (!host || host.dataset.built) return;
  host.dataset.built = "1";
  const input = el("input", "capture-input");
  input.type = "text";
  input.placeholder = "＋ capture a note or paste a link…";
  const add = async () => {
    const v = input.value.trim();
    if (!v) return;
    input.value = "";
    const isURL = /^https?:\/\/\S+$/i.test(v);
    try {
      await postJSONOk("/api/capture/item", isURL ? { url: v } : { text: v });
      loadCapture();
    } catch (e) { showToast("Capture failed"); input.value = v; }
  };
  input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  const file = document.createElement("input");
  file.type = "file";
  file.accept = "image/*,.pdf";
  file.hidden = true;
  file.onchange = () => { if (file.files.length) captureUploadFiles(file.files); file.value = ""; };
  const attach = pillLight("＋ file", () => file.click());
  host.append(input, attach, file);
}

async function captureUploadFiles(files, extra) {
  const fd = new FormData();
  [...files].forEach((f) => fd.append("files", f));
  if (extra && extra.text) fd.append("text", extra.text);
  try {
    const res = await fetch("/api/capture/upload", { method: "POST", body: fd });
    if (!res.ok) throw new Error((await res.text()).slice(0, 120));
    showToast("Captured to tray", null, "info");
    if (!els.captureView.hidden) loadCapture();
    else refreshCaptureBadge();
  } catch (e) { showToast("Capture failed — " + (e.message || "error")); }
}

// desktop: paste an image anywhere (not while typing) → tray
window.addEventListener("paste", (e) => {
  if (typingInField(e.target)) return;
  const files = [...(e.clipboardData ? e.clipboardData.files : [])].filter((f) =>
    f.type.startsWith("image/") || f.type === "application/pdf");
  if (!files.length) return;
  e.preventDefault();
  captureUploadFiles(files);
});

// desktop: drag-drop onto the tray surface
window.addEventListener("dragover", (e) => {
  if (els.captureView && !els.captureView.hidden) e.preventDefault();
});
window.addEventListener("drop", (e) => {
  if (!els.captureView || els.captureView.hidden) return;
  e.preventDefault();
  const files = [...e.dataTransfer.files].filter((f) =>
    f.type.startsWith("image/") || f.type === "application/pdf");
  if (files.length) captureUploadFiles(files);
});

// ⌘K: new capture from the palette query
cmdRegistry.register(() => [{
  id: "act:new-capture", name: "New capture", hint: "tray · action",
  keywords: "capture tray inbox note share",
  act: () => { closeCmdbar(); location.hash = "#/capture"; setTimeout(() => { const i = document.querySelector(".capture-input"); if (i) i.focus(); }, 200); },
}]);

buildCaptureComposer();
refreshCaptureBadge();
