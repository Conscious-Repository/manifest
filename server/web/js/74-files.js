// ---- FILES: the cockpit's fleet file browser (cmd-ctr parity) ----
// Browse allowlisted slices of each machine — metis directly, remote devices
// through their manifest-agent (tickets minted server-side; the browser only
// ever talks to metis). Full write parity: mkdir / multi-upload / rename /
// move (drag) / delete (confirmed) / download, plus sort, grid⇄list, hidden
// toggle, per-box pinned home, preview lightbox, keyboard nav.

let fsHosts = [], fsHost = "", fsPath = "", fsEntries = [], fsListedPath = "";
let fsSel = "";                       // selected entry name
let fsHomes = {};                     // host → pinned home path
let fsPrefs = { sortKey: "name", sortDir: 1, view: "list", showHidden: false };
try { Object.assign(fsPrefs, JSON.parse(localStorage.getItem("manifest.filesPrefs") || "{}")); } catch (e) {}

function fsSavePrefs() {
  try { localStorage.setItem("manifest.filesPrefs", JSON.stringify(fsPrefs)); } catch (e) {}
}

// showFilesStage mounts the browser into the terminal cockpit's Files pane.
function showFilesStage() {
  const pane = document.getElementById("termStageFiles");
  if (pane && !pane.dataset.built) {
    pane.dataset.built = "1";
    const bar = el("div", "fs-toolbar");
    bar.id = "fsToolbar";
    const body = el("div", "fs-body");
    body.id = "fsBody";
    body.tabIndex = 0;
    body.onkeydown = fsKeydown;
    fsBindDrop(body);
    const ups = el("div", "fs-uplist");
    ups.id = "fsUplist";
    pane.append(bar, body, ups);
  }
  fsLoadHosts();
}

async function fsLoadHosts() {
  try { fsHosts = ((await (await fetch("/api/files/hosts")).json()).hosts) || []; }
  catch (e) { fsHosts = []; }
  if (!fsHost && fsHosts.length) fsHost = fsHosts[0].name;
  if (fsHost && !(fsHost in fsHomes)) {
    try { fsHomes[fsHost] = ((await (await fetch("/api/files/home?host=" + encodeURIComponent(fsHost))).json()).path) || ""; }
    catch (e) { fsHomes[fsHost] = ""; }
    if (!fsPath && fsHomes[fsHost]) fsPath = fsHomes[fsHost];
  }
  fsRenderToolbar();
  fsLoad();
}

// --- toolbar ---------------------------------------------------------------

function fsTool(glyph, title, onclick) {
  const b = el("button", "fs-tool", glyph);
  b.title = title;
  b.onclick = onclick;
  return b;
}

function fsRenderToolbar() {
  const bar = document.getElementById("fsToolbar");
  if (!bar) return;
  bar.innerHTML = "";
  // host chips
  const chips = el("span", "feed-filters");
  fsHosts.forEach((h) => {
    const chip = el("button", "filter-chip" + (h.name === fsHost ? " on" : ""), h.name + (h.local ? " ·local" : ""));
    chip.onclick = async () => {
      fsHost = h.name; fsPath = ""; fsSel = "";
      if (!(fsHost in fsHomes)) {
        try { fsHomes[fsHost] = ((await (await fetch("/api/files/home?host=" + encodeURIComponent(fsHost))).json()).path) || ""; }
        catch (e) { fsHomes[fsHost] = ""; }
      }
      fsPath = fsHomes[fsHost] || "";
      fsRenderToolbar(); fsLoad();
    };
    chips.append(chip);
  });
  if (!fsHosts.length) chips.append(el("span", "panel-meta", "no hosts — set filesRoots / filesAgents in config"));
  bar.append(chips);

  // nav tools
  const pinned = fsHomes[fsHost] || "";
  bar.append(fsTool("⌂", pinned ? "Home (pinned): " + pinned : "Home (roots)", () => { fsPath = pinned; fsSel = ""; fsLoad(); }));
  const atPin = pinned && fsListedPath === pinned;
  const pin = fsTool(atPin ? "★" : "☆", atPin ? "Unpin this folder as home" : "Pin this folder as this box's home", async () => {
    const target = atPin ? "" : fsListedPath;
    try {
      await fetch("/api/files/home?host=" + encodeURIComponent(fsHost), {
        method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: target }),
      });
      fsHomes[fsHost] = target;
      fsRenderToolbar();
    } catch (e) { showToast("Couldn't pin — " + (e.message || "error")); }
  });
  if (!fsPath) pin.disabled = true;
  bar.append(pin);
  const up = fsTool("‹", "Up a level", fsGoUp);
  fsBindMoveTarget(up, () => fsParent(fsListedPath));
  bar.append(up);
  bar.append(fsTool("↻", "Refresh", () => fsLoad()));

  // breadcrumbs
  const crumbs = el("span", "fs-crumbs");
  crumbs.id = "fsCrumbs";
  bar.append(crumbs);

  // right cluster
  const right = el("span", "fs-right");
  const sortLbl = { name: "name", size: "size", mtime: "modified" };
  const sort = fsTool("⇅ " + sortLbl[fsPrefs.sortKey] + (fsPrefs.sortDir > 0 ? " ↑" : " ↓"), "Sort — click to cycle field, same field flips direction", () => {
    const order = ["name", "size", "mtime"];
    fsPrefs.sortDir = -fsPrefs.sortDir;
    if (fsPrefs.sortDir > 0) fsPrefs.sortKey = order[(order.indexOf(fsPrefs.sortKey) + (fsPrefs.sortDir === 1 && fsPrefs._flip ? 1 : 0)) % 3];
    fsPrefs._flip = true;
    fsSavePrefs(); fsRenderToolbar(); fsRenderBody();
  });
  sort.classList.add("fs-sort");
  sort.oncontextmenu = (e) => { // right-click cycles the field directly
    e.preventDefault();
    const order = ["name", "size", "mtime"];
    fsPrefs.sortKey = order[(order.indexOf(fsPrefs.sortKey) + 1) % 3];
    fsSavePrefs(); fsRenderToolbar(); fsRenderBody();
  };
  right.append(sort);
  right.append(fsTool(fsPrefs.view === "grid" ? "☰" : "▦", fsPrefs.view === "grid" ? "List view" : "Grid view", () => {
    fsPrefs.view = fsPrefs.view === "grid" ? "list" : "grid";
    fsSavePrefs(); fsRenderToolbar(); fsRenderBody();
  }));
  const hiddenCount = fsEntries.filter((e) => e.hidden).length;
  const eye = fsTool(fsPrefs.showHidden ? "◉" : "◎", (fsPrefs.showHidden ? "Hide" : "Show") + " hidden files" + (hiddenCount ? " (" + hiddenCount + ")" : ""), () => {
    fsPrefs.showHidden = !fsPrefs.showHidden;
    fsSavePrefs(); fsRenderToolbar(); fsRenderBody();
  });
  if (fsPrefs.showHidden) eye.classList.add("on");
  right.append(eye);
  const mkdir = fsTool("＋⌸", "New folder", fsNewFolderRow);
  mkdir.disabled = !fsPath;
  right.append(mkdir);
  const fi = document.createElement("input");
  fi.type = "file"; fi.multiple = true; fi.hidden = true;
  fi.onchange = () => { if (fi.files.length) fsUpload([...fi.files], fsListedPath); fi.value = ""; };
  const upBtn = pillLight("＋ upload", () => fi.click());
  if (!fsPath) upBtn.disabled = true;
  right.append(upBtn, fi);
  bar.append(right);
}

function fsRenderCrumbs() {
  const host = document.getElementById("fsCrumbs");
  if (!host) return;
  host.innerHTML = "";
  if (!fsListedPath) { host.append(el("span", "fs-crumb-root", "roots")); return; }
  const segs = fsListedPath.split("/").filter(Boolean);
  let acc = "";
  const sl = el("span", "fs-crumb-sep", "/");
  host.append(sl);
  segs.forEach((seg, i) => {
    acc += "/" + seg;
    const p = acc;
    const c = el("button", "fs-crumb" + (i === segs.length - 1 ? " last" : ""), seg);
    c.onclick = () => { fsPath = p; fsSel = ""; fsLoad(); };
    fsBindMoveTarget(c, () => p);
    host.append(c);
    if (i < segs.length - 1) host.append(el("span", "fs-crumb-sep", "/"));
  });
}

// --- listing ---------------------------------------------------------------

async function fsLoad() {
  const body = document.getElementById("fsBody");
  if (!body) return;
  let d;
  try {
    const res = await fetch("/api/files/list?host=" + encodeURIComponent(fsHost) + "&path=" + encodeURIComponent(fsPath));
    if (!res.ok) { body.innerHTML = ""; body.append(emptyRow(await res.text())); return; }
    d = await res.json();
  } catch (e) { body.innerHTML = ""; body.append(emptyRow("unreachable")); return; }
  fsEntries = d.entries || [];
  fsListedPath = d.path || "";
  fsRenderToolbar();
  fsRenderCrumbs();
  fsRenderBody();
}

function fsVisibleEntries() {
  let list = fsEntries.filter((e) => fsPrefs.showHidden || !e.hidden);
  const dir = fsPrefs.sortDir;
  const key = fsPrefs.sortKey;
  list.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    if (key === "size") return (a.size - b.size) * dir || a.name.localeCompare(b.name);
    if (key === "mtime") return (a.mtime - b.mtime) * dir || a.name.localeCompare(b.name);
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) * dir;
  });
  return list;
}

function fsFull(name) {
  return (fsListedPath ? fsListedPath.replace(/\/+$/, "") : "") + "/" + name;
}

function fsParent(p) {
  const parts = (p || "").replace(/\/+$/, "").split("/");
  parts.pop();
  const parent = parts.join("/");
  return parent.length < 2 ? "" : parent;
}

function fsGoUp() {
  fsPath = fsParent(fsListedPath);
  fsSel = "";
  fsLoad();
}

function fsRenderBody() {
  const body = document.getElementById("fsBody");
  if (!body) return;
  body.innerHTML = "";
  const list = fsVisibleEntries();
  if (!list.length) { body.append(emptyRow(fsListedPath ? "empty" : "no browse roots")); return; }
  if (fsPrefs.view === "grid") {
    const grid = el("div", "fs-grid");
    list.forEach((e) => grid.append(fsCard(e)));
    body.append(grid);
  } else {
    list.forEach((e) => body.append(fsRow(e)));
  }
}

const FS_IMG_RE = /\.(png|jpe?g|gif|webp|svg|heic|avif)$/i;
const FS_VIDEO_RE = /\.(mp4|mov|webm|m4v)$/i;
const FS_AUDIO_RE = /\.(mp3|wav|m4a|ogg|flac)$/i;
const FS_TEXT_RE = /\.(md|txt|json|js|mjs|ts|go|py|sh|css|html|xml|yaml|yml|toml|csv|log|conf|ini|service|sql)$/i;

function fsKindGlyph(e) {
  if (e.dir) return "▸";
  if (FS_IMG_RE.test(e.name)) return "▣";
  if (FS_VIDEO_RE.test(e.name)) return "▶";
  if (FS_AUDIO_RE.test(e.name)) return "♪";
  if (/\.pdf$/i.test(e.name)) return "▤";
  if (FS_TEXT_RE.test(e.name)) return "≡";
  return "·";
}

function fsReadURL(name, dl) {
  return "/api/files/read?host=" + encodeURIComponent(fsHost) + "&path=" + encodeURIComponent(fsFull(name)) + (dl ? "&dl=1" : "");
}

function fsThumb(e, cls) {
  if (!e.dir && FS_IMG_RE.test(e.name) && e.size < 12 << 20) {
    const img = document.createElement("img");
    img.className = cls;
    img.loading = "lazy";
    img.src = fsReadURL(e.name);
    return img;
  }
  return el("span", cls + " glyph" + (e.dir ? " dir" : ""), fsKindGlyph(e));
}

function fsRow(e) {
  const row = el("div", "fs-row" + (fsSel === e.name ? " sel" : "") + (e.hidden ? " hid" : ""));
  row.dataset.name = e.name;
  row.append(fsThumb(e, "fs-ico"));
  const name = el("span", "fs-name" + (e.dir ? " dir" : ""), e.name);
  if (e.link) name.append(el("span", "fs-linkmark", " ↗"));
  row.append(name);
  const acts = el("span", "fs-acts");
  if (!e.dir) {
    acts.append(fsTool("⌕", "Preview", (ev) => { ev.stopPropagation(); fsLightbox(e.name); }));
    const dl = document.createElement("a");
    dl.className = "fs-tool"; dl.textContent = "⤓"; dl.title = "Download";
    dl.href = fsReadURL(e.name, true);
    dl.onclick = (ev) => ev.stopPropagation();
    acts.append(dl);
  }
  acts.append(fsTool("✎", "Rename", (ev) => { ev.stopPropagation(); fsRenameInline(row, name, e); }));
  // delete is arm-then-confirm on the row (armedDelete — no native confirm,
  // banned app-wide); the keyboard path asks through a clickable toast
  const x = armedDelete("✕", "delete?", () => fsDelete(e, true));
  x.className = "fs-tool";
  x.title = "Delete";
  acts.append(x);
  row.append(acts);
  if (!e.dir) row.append(el("span", "fs-size", fmtBytes(e.size)));
  row.append(el("span", "fs-mtime", e.mtime ? fmtWhen(new Date(e.mtime * 1000).toISOString()) : ""));
  row.onclick = () => { fsSel = e.name; fsRenderBody(); };
  row.ondblclick = () => fsOpen(e);
  fsBindDrag(row, e);
  return row;
}

function fsCard(e) {
  const card = el("div", "fs-card" + (fsSel === e.name ? " sel" : "") + (e.hidden ? " hid" : ""));
  card.dataset.name = e.name;
  card.append(fsThumb(e, "fs-card-ico"));
  card.append(el("div", "fs-card-name", e.name));
  card.append(el("div", "fs-card-meta", e.dir ? "folder" : fmtBytes(e.size)));
  card.onclick = () => { fsSel = e.name; fsRenderBody(); };
  card.ondblclick = () => fsOpen(e);
  fsBindDrag(card, e);
  return card;
}

function fsOpen(e) {
  if (e.dir) { fsPath = fsFull(e.name); fsSel = ""; fsLoad(); }
  else fsLightbox(e.name);
}

// --- write ops -------------------------------------------------------------

function fsNewFolderRow() {
  const body = document.getElementById("fsBody");
  if (!body || body.querySelector(".fs-newrow")) return;
  const row = el("div", "fs-newrow");
  row.append(el("span", "fs-ico glyph dir", "▸"));
  const inp = document.createElement("input");
  inp.className = "fs-newname";
  inp.placeholder = "folder name";
  inp.spellcheck = false;
  const commit = async () => {
    const v = inp.value.trim();
    row.remove();
    if (!v || /[\/\\]/.test(v)) return;
    try {
      const res = await fetch("/api/files/mkdir?host=" + encodeURIComponent(fsHost) + "&path=" + encodeURIComponent(fsFull(v)), { method: "POST" });
      if (!res.ok) throw new Error((await res.text()).slice(0, 120));
      fsLoad();
    } catch (e) { showToast("Couldn't create folder — " + (e.message || "error")); }
  };
  inp.onkeydown = (e) => { if (e.key === "Enter") commit(); if (e.key === "Escape") row.remove(); };
  inp.onblur = () => { if (inp.value.trim()) commit(); else row.remove(); };
  row.append(inp);
  body.prepend(row);
  inp.focus();
}

// fsRenameInline — the library's inlineRename over the files rename route;
// the selection starts on the stem (the extension is rarely what changes).
function fsRenameInline(row, nameEl, e) {
  const inp = inlineRename(nameEl, e.name, async (v) => {
    if (/[\/\\]/.test(v)) return;
    try {
      const res = await fetch("/api/files/rename?host=" + encodeURIComponent(fsHost) +
        "&from=" + encodeURIComponent(fsFull(e.name)) + "&to=" + encodeURIComponent(fsFull(v)), { method: "POST" });
      if (!res.ok) throw new Error((await res.text()).slice(0, 120));
      fsSel = v;
      fsLoad();
    } catch (err) { showToast("Couldn't rename — " + (err.message || "error")); }
  });
  const dot = inp.value.lastIndexOf(".");
  inp.setSelectionRange(0, dot > 0 ? dot : inp.value.length);
}

// fsDelete — confirmed=true from the row's armed ✕; the keyboard path
// (Delete/Backspace) confirms through a clickable toast instead. A non-empty
// folder asks once more the same way before recursing.
async function fsDelete(e, confirmed) {
  if (!confirmed) { showToast("Delete " + e.name + "? · click to confirm", () => fsDelete(e, true), "info"); return; }
  const q = "/api/files/delete?host=" + encodeURIComponent(fsHost) + "&path=" + encodeURIComponent(fsFull(e.name));
  let res;
  try { res = await fetch(q, { method: "POST" }); } catch (err) { showToast("unreachable"); return; }
  if (!res.ok) {
    const msg = await res.text();
    if (e.dir && /not empty|directory/i.test(msg)) {
      showToast(e.name + " isn't empty — click to delete the folder and everything inside it", () => fsDeleteRecursive(e, q));
      return;
    }
    showToast("Couldn't delete — " + msg.slice(0, 120));
    return;
  }
  if (fsSel === e.name) fsSel = "";
  fsLoad();
}

async function fsDeleteRecursive(e, q) {
  try {
    const res = await fetch(q + "&recursive=1", { method: "POST" });
    if (!res.ok) throw new Error((await res.text()).slice(0, 120));
  } catch (err) { showToast("Couldn't delete — " + (err.message || "error")); return; }
  if (fsSel === e.name) fsSel = "";
  fsLoad();
}

// --- upload (multi, XHR progress) ------------------------------------------

function fsUpload(files, destDir, overwrite) {
  const ups = document.getElementById("fsUplist");
  files.forEach((f) => {
    const rowEl = el("div", "fs-uprow");
    rowEl.append(el("span", "fs-upname", f.name));
    const barBox = el("span", "fs-upbar");
    const fill = el("span", "fs-upfill");
    barBox.append(fill);
    rowEl.append(barBox);
    ups.append(rowEl);
    const xhr = new XMLHttpRequest();
    const dest = destDir.replace(/\/+$/, "") + "/" + f.name;
    xhr.open("POST", "/api/files/upload?host=" + encodeURIComponent(fsHost) + "&path=" + encodeURIComponent(dest) + (overwrite ? "&overwrite=1" : ""));
    xhr.upload.onprogress = (ev) => { if (ev.lengthComputable) fill.style.width = (ev.loaded / ev.total * 100) + "%"; };
    xhr.onload = () => {
      if (xhr.status === 409) {
        rowEl.remove();
        showToast(f.name + " already exists — click to overwrite it", () => fsUpload([f], destDir, true));
        return;
      }
      if (xhr.status >= 300) {
        rowEl.classList.add("err");
        rowEl.append(el("span", "fs-uperr", (xhr.responseText || "failed").slice(0, 80)));
        return;
      }
      fill.style.width = "100%";
      setTimeout(() => rowEl.remove(), 1400);
      if (destDir === fsListedPath) fsLoad();
    };
    xhr.onerror = () => { rowEl.classList.add("err"); rowEl.append(el("span", "fs-uperr", "network error")); };
    xhr.send(f);
  });
}

// --- drag & drop -----------------------------------------------------------

// fsBindDrag: rows/cards are draggable (internal move) and folder rows accept
// both internal items (move) and OS files (upload into).
function fsBindDrag(node, e) {
  node.draggable = true;
  node.ondragstart = (ev) => {
    ev.dataTransfer.setData("application/x-manifest-path", fsFull(e.name));
    ev.dataTransfer.effectAllowed = "move";
  };
  if (e.dir) {
    fsBindMoveTarget(node, () => fsFull(e.name), true);
  }
}

// fsBindMoveTarget makes a node accept internal drags (→ move) and, when
// acceptFiles, OS file drops (→ upload into that dir).
function fsBindMoveTarget(node, destOf, acceptFiles) {
  node.ondragover = (ev) => { ev.preventDefault(); node.classList.add("droptarget"); };
  node.ondragleave = () => node.classList.remove("droptarget");
  node.ondrop = async (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    node.classList.remove("droptarget");
    const dest = destOf();
    const src = ev.dataTransfer.getData("application/x-manifest-path");
    if (src) {
      if (dest === "" || src === dest) return;
      const to = dest.replace(/\/+$/, "") + "/" + src.split("/").pop();
      try {
        const res = await fetch("/api/files/rename?host=" + encodeURIComponent(fsHost) +
          "&from=" + encodeURIComponent(src) + "&to=" + encodeURIComponent(to), { method: "POST" });
        if (!res.ok) throw new Error((await res.text()).slice(0, 120));
        fsLoad();
      } catch (err) { showToast("Couldn't move — " + (err.message || "error")); }
      return;
    }
    if (acceptFiles && ev.dataTransfer.files && ev.dataTransfer.files.length && dest) {
      fsUpload([...ev.dataTransfer.files], dest);
    }
  };
}

// whole-pane OS-file drop → upload into the current dir
function fsBindDrop(body) {
  body.ondragover = (ev) => {
    if ([...ev.dataTransfer.types].includes("application/x-manifest-path")) return;
    ev.preventDefault();
    body.classList.add("droppane");
  };
  body.ondragleave = (ev) => { if (ev.target === body) body.classList.remove("droppane"); };
  body.ondrop = (ev) => {
    body.classList.remove("droppane");
    if ([...ev.dataTransfer.types].includes("application/x-manifest-path")) return;
    ev.preventDefault();
    if (ev.dataTransfer.files && ev.dataTransfer.files.length && fsListedPath) {
      fsUpload([...ev.dataTransfer.files], fsListedPath);
    }
  };
}

// --- preview lightbox ------------------------------------------------------

function fsPreviewable(e) {
  return !e.dir && (FS_IMG_RE.test(e.name) || FS_VIDEO_RE.test(e.name) || FS_AUDIO_RE.test(e.name) ||
    /\.pdf$/i.test(e.name) || FS_TEXT_RE.test(e.name) || e.size < 200000);
}

function fsLightbox(name) {
  const olds = document.querySelector(".fs-lightbox");
  if (olds) olds.remove();
  const files = fsVisibleEntries().filter(fsPreviewable);
  let idx = files.findIndex((f) => f.name === name);
  if (idx < 0) return;

  const box = el("div", "fs-lightbox");
  const panel = el("div", "fs-lb-panel");
  const head = el("div", "fs-lb-head");
  const title = el("code", "fs-lb-title");
  const count = el("span", "fs-lb-count");
  const dl = document.createElement("a");
  dl.className = "fs-tool"; dl.textContent = "⤓"; dl.title = "Download";
  const close = fsTool("✕", "Close (Esc)", () => box.remove());
  head.append(title, count, dl, close);
  const bodyEl = el("div", "fs-lb-body");
  panel.append(head, bodyEl);
  const prev = fsTool("‹", "Previous (←)", () => show(idx - 1));
  prev.classList.add("fs-lb-nav", "left");
  const next = fsTool("›", "Next (→)", () => show(idx + 1));
  next.classList.add("fs-lb-nav", "right");
  box.append(prev, panel, next);
  box.onclick = (ev) => { if (ev.target === box) box.remove(); };

  function show(i) {
    idx = (i + files.length) % files.length;
    const e = files[idx];
    const url = fsReadURL(e.name);
    title.textContent = e.name;
    count.textContent = (idx + 1) + " / " + files.length;
    dl.href = fsReadURL(e.name, true);
    bodyEl.innerHTML = "";
    if (FS_IMG_RE.test(e.name)) {
      const img = document.createElement("img");
      img.src = url;
      bodyEl.append(img);
    } else if (FS_VIDEO_RE.test(e.name)) {
      const v = document.createElement("video");
      v.src = url; v.controls = true; v.autoplay = true;
      bodyEl.append(v);
    } else if (FS_AUDIO_RE.test(e.name)) {
      const a = document.createElement("audio");
      a.src = url; a.controls = true;
      bodyEl.append(a);
    } else if (/\.pdf$/i.test(e.name)) {
      const o = document.createElement("iframe");
      o.className = "fs-lb-pdf";
      o.src = url;
      bodyEl.append(o);
    } else if (FS_TEXT_RE.test(e.name) || e.size < 200000) {
      const pre = el("pre", "fs-lb-text", "loading…");
      fetch(url).then((r) => r.text()).then((t) => { pre.textContent = t.slice(0, 400000); }).catch(() => { pre.textContent = "unreadable"; });
      bodyEl.append(pre);
    } else {
      bodyEl.append(el("div", "panel-meta", "no inline preview — download instead"));
    }
  }
  show(idx);

  const onKey = (ev) => {
    if (ev.key === "Escape") { box.remove(); }
    else if (ev.key === "ArrowLeft") show(idx - 1);
    else if (ev.key === "ArrowRight") show(idx + 1);
    else return;
    ev.preventDefault();
  };
  document.addEventListener("keydown", onKey);
  const obs = new MutationObserver(() => {
    if (!document.body.contains(box)) { document.removeEventListener("keydown", onKey); obs.disconnect(); }
  });
  obs.observe(document.body, { childList: true, subtree: true });
  document.body.append(box);
}

// --- keyboard nav on the list ----------------------------------------------

function fsKeydown(ev) {
  if (ev.target.tagName === "INPUT") return;
  const list = fsVisibleEntries();
  if (!list.length) return;
  const i = list.findIndex((e) => e.name === fsSel);
  const sel = i >= 0 ? list[i] : null;
  switch (ev.key) {
    case "ArrowDown": fsSel = list[Math.min(list.length - 1, i + 1)].name; fsRenderBody(); break;
    case "ArrowUp": fsSel = list[Math.max(0, i - 1)].name; fsRenderBody(); break;
    case "Enter": if (sel) fsOpen(sel); break;
    case "F2": {
      if (!sel) return;
      const row = document.querySelector('.fs-row[data-name="' + CSS.escape(sel.name) + '"]');
      const nameEl = row && row.querySelector(".fs-name");
      if (row && nameEl) fsRenameInline(row, nameEl, sel);
      break;
    }
    case "Delete": case "Backspace": if (sel) fsDelete(sel); break;
    case "Escape": fsSel = ""; fsRenderBody(); break;
    default: return;
  }
  ev.preventDefault();
}

function fmtBytes(n) {
  if (!n) return "";
  if (n > 1 << 30) return (n / (1 << 30)).toFixed(1) + "G";
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + "M";
  if (n > 1 << 10) return (n / (1 << 10)).toFixed(0) + "K";
  return n + "B";
}

// (⌘K entry lives in 73-terminal.js — Files is a cockpit stage now)
