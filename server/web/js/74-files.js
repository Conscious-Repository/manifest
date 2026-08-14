// ---- FILES: fleet file browser (cmd-ctr import P8) ----
// Browse allowlisted slices of each machine: the local host directly, remote
// devices through their manifest-agent over the tailnet (ticket auth minted
// server-side). Read + preview + upload; no delete by design.

let filesHosts = [], filesHost = "", filesPath = "";

function showFiles() {
  loadFilesHosts();
}

async function loadFilesHosts() {
  try { filesHosts = ((await (await fetch("/api/files/hosts")).json()).hosts) || []; }
  catch (e) { filesHosts = []; }
  if (!filesHost && filesHosts.length) filesHost = filesHosts[0].name;
  renderFilesChrome();
  if (filesHost) loadFilesList();
}

function renderFilesChrome() {
  const head = document.getElementById("filesHosts");
  if (!head) return;
  head.innerHTML = "";
  filesHosts.forEach((h) => {
    const chip = el("button", "filter-chip" + (h.name === filesHost ? " on" : ""), h.name + (h.local ? " ·local" : ""));
    chip.onclick = () => { filesHost = h.name; filesPath = ""; renderFilesChrome(); loadFilesList(); };
    head.append(chip);
  });
  if (!filesHosts.length) head.append(el("span", "panel-meta", "no hosts — set filesRoots / filesAgents in config"));
}

async function loadFilesList() {
  const host = document.getElementById("filesRows");
  if (!host) return;
  host.innerHTML = "";
  host.append(emptyRow("loading…"));
  let d;
  try {
    const res = await fetch("/api/files/list?host=" + encodeURIComponent(filesHost) + "&path=" + encodeURIComponent(filesPath));
    if (!res.ok) { host.innerHTML = ""; host.append(emptyRow(await res.text())); return; }
    d = await res.json();
  } catch (e) { host.innerHTML = ""; host.append(emptyRow("unreachable")); return; }
  host.innerHTML = "";

  const bar = el("div", "files-pathbar");
  bar.append(el("code", "files-path", d.path || "(roots)"));
  if (filesPath) {
    const up = el("button", "sprt-quiet", "‹ up");
    up.onclick = () => {
      const parts = filesPath.replace(/\/+$/, "").split("/");
      parts.pop();
      const parent = parts.join("/") || "/";
      // stepping above a root returns to the roots listing
      filesPath = parent.length < 2 ? "" : parent;
      loadFilesList();
    };
    bar.append(up);
  }
  // upload into the current dir
  if (filesPath) {
    const fi = document.createElement("input");
    fi.type = "file";
    fi.hidden = true;
    fi.onchange = async () => {
      if (!fi.files.length) return;
      const f = fi.files[0];
      try {
        const res = await fetch("/api/files/upload?host=" + encodeURIComponent(filesHost) +
          "&path=" + encodeURIComponent(filesPath.replace(/\/+$/, "") + "/" + f.name), { method: "POST", body: f });
        if (!res.ok) throw new Error((await res.text()).slice(0, 120));
        showToast("Uploaded " + f.name, null, "info");
        loadFilesList();
      } catch (e) { showToast("Upload failed — " + (e.message || "error")); }
      fi.value = "";
    };
    const ub = pillLight("＋ upload", () => fi.click());
    bar.append(ub, fi);
  }
  host.append(bar);

  (d.entries || []).forEach((e2) => {
    const row = el("div", "files-row");
    row.append(el("span", "files-glyph", e2.dir ? "▸" : "·"));
    row.append(el("span", "files-name" + (e2.dir ? " dir" : ""), e2.name));
    if (!e2.dir) row.append(el("span", "files-size", fmtBytes(e2.size)));
    row.onclick = () => {
      const full = filesPath ? filesPath.replace(/\/+$/, "") + "/" + e2.name : e2.name;
      if (e2.dir) { filesPath = full; loadFilesList(); }
      else filesPreview(full, e2);
    };
    host.append(row);
  });
  if (!(d.entries || []).length) host.append(emptyRow("empty"));
}

function filesPreview(path, entry) {
  const url = "/api/files/read?host=" + encodeURIComponent(filesHost) + "&path=" + encodeURIComponent(path);
  const isImg = /\.(png|jpe?g|gif|webp|svg|heic)$/i.test(entry.name);
  const isText = /\.(md|txt|json|js|go|py|sh|css|html|yaml|yml|toml|csv|log)$/i.test(entry.name) || entry.size < 200000 && !isImg && !/\.(pdf|zip|bin|exe|dmg|png|jpe?g|mp4|mov|wav)$/i.test(entry.name);
  const host = document.getElementById("filesRows");
  const pane = el("div", "files-preview");
  const head = el("div", "files-pathbar");
  head.append(el("code", "files-path", path));
  const dl = document.createElement("a");
  dl.className = "pill light";
  dl.textContent = "download";
  dl.href = url;
  dl.download = entry.name;
  const close = pillLight("close", () => { pane.remove(); });
  head.append(dl, close);
  pane.append(head);
  if (isImg) {
    const img = document.createElement("img");
    img.className = "files-preview-img";
    img.src = url;
    pane.append(img);
  } else if (isText) {
    const pre = el("pre", "files-preview-text", "loading…");
    fetch(url).then((r) => r.text()).then((t) => { pre.textContent = t.slice(0, 200000); }).catch(() => { pre.textContent = "unreadable"; });
    pane.append(pre);
  } else {
    pane.append(el("div", "panel-meta", "no inline preview — download instead"));
  }
  host.prepend(pane);
}

function fmtBytes(n) {
  if (!n) return "";
  if (n > 1 << 20) return (n / (1 << 20)).toFixed(1) + "M";
  if (n > 1 << 10) return (n / (1 << 10)).toFixed(0) + "K";
  return n + "B";
}

// ⌘K: jump to files
cmdRegistry.register(() => [{
  id: "goto:#/files", name: "Files", hint: "fleet · view",
  keywords: "files fleet browse filesystem",
  act: () => { closeCmdbar(); location.hash = "#/files"; },
}]);
