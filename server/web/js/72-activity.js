// ---- ACTIVITY: the cockpit's fleet vitals (cmd-ctr parity) ----
// One card per box: status dot, os/cores/uptime, temp/battery/disk chips,
// four sparkline metrics (CPU / MEM / SWAP / NET) over 5m/1h/24h, and a
// click-to-expand top-processes table. All polling stops the moment the
// stage or the tab is hidden (3s live, history refetched with it).

let actDevices = [];
let actRange = localStorage.getItem("manifest.actRange") || "1h";
let actHist = {};                 // device → points
let actOpen = "";                 // expanded card (top-processes)
let actTimer = null, actTopTimer = null;

function showActivityStage() {
  const pane = document.getElementById("termStageActivity");
  if (pane && !pane.dataset.built) {
    pane.dataset.built = "1";
    pane.innerHTML = "";
    const head = el("div", "act-head");
    const ranges = el("span", "feed-filters");
    ranges.id = "actRanges";
    const meta = el("span", "act-reach");
    meta.id = "actReach";
    head.append(ranges, meta);
    const grid = el("div", "act-grid");
    grid.id = "actGrid";
    pane.append(head, grid);
  }
  renderActRanges();
  actTick();
  if (!actTimer) actTimer = setInterval(() => {
    if (actVisible()) actTick();
  }, 3000);
}

function actVisible() {
  const pane = document.getElementById("termStageActivity");
  return pane && !pane.hidden && els.terminalView && !els.terminalView.hidden && !document.hidden;
}

function renderActRanges() {
  const host = document.getElementById("actRanges");
  if (!host) return;
  host.innerHTML = "";
  ["5m", "1h", "24h"].forEach((r) => {
    const chip = el("button", "filter-chip" + (r === actRange ? " on" : ""), r);
    chip.onclick = () => {
      actRange = r;
      try { localStorage.setItem("manifest.actRange", r); } catch (e) {}
      actHist = {};
      renderActRanges();
      actTick();
    };
    host.append(chip);
  });
}

async function actTick() {
  try { actDevices = ((await (await fetch("/api/activity")).json()).devices) || []; }
  catch (e) { return; }
  // histories in parallel (≤120 pts each — cheap)
  await Promise.all(actDevices.map(async (d) => {
    try {
      const res = await fetch("/api/activity/history?device=" + encodeURIComponent(d.name) + "&range=" + actRange);
      actHist[d.name] = ((await res.json()).points) || [];
    } catch (e) { actHist[d.name] = []; }
  }));
  renderActGrid();
}

function renderActGrid() {
  const reach = document.getElementById("actReach");
  if (reach) {
    const up = actDevices.filter((d) => d.status === "ok").length;
    reach.textContent = up + " of " + actDevices.length + " boxes reachable";
  }
  const grid = document.getElementById("actGrid");
  if (!grid) return;
  grid.innerHTML = "";
  if (!actDevices.length) { grid.append(emptyRow("no devices — the collector warms up on first launch")); return; }
  actDevices.forEach((d) => grid.append(actCard(d)));
}

function actCard(d) {
  const card = el("div", "act-card" + (d.status !== "ok" ? " off" : "") + (actOpen === d.name ? " open" : ""));
  // head
  const head = el("div", "act-card-head");
  head.append(statusDot(d.status === "ok", d.status));
  head.append(el("span", "act-name", d.name));
  const meta = [];
  if (d.os) meta.push(d.os);
  if (d.cores) meta.push(d.cores + " cores");
  if (d.uptime) meta.push("up " + actFmtDur(d.uptime));
  head.append(el("span", "act-meta", meta.join(" · ")));
  card.append(head);
  // chips
  const chips = el("div", "act-chips");
  if (d.temp) chips.append(el("span", "act-chip", Math.round(d.temp) + "°C"));
  if (d.battery && d.battery.pct != null) chips.append(el("span", "act-chip", (d.battery.charging ? "⚡" : "") + d.battery.pct + "%"));
  (d.disks || []).forEach((disk) => {
    const pct = disk.total ? Math.round(disk.used / disk.total * 100) : 0;
    const c = el("span", "act-chip" + (pct > 90 ? " over" : ""), (disk.mount === "/" ? "disk" : disk.mount.split("/").pop()) + " " + pct + "%");
    c.title = disk.mount + " — " + fmtBytes(disk.total - disk.used) + " free of " + fmtBytes(disk.total);
    chips.append(c);
  });
  if (chips.childElementCount) card.append(chips);
  // metrics
  const pts = actHist[d.name] || [];
  const memPct = d.mem && d.mem.total ? d.mem.used / d.mem.total * 100 : 0;
  const swapPct = d.swap && d.swap.total ? d.swap.used / d.swap.total * 100 : 0;
  const mets = el("div", "act-mets");
  mets.append(actMetric("CPU", pts.map((p) => p.cpu), 100, (d.cpu || 0).toFixed(0) + "%"));
  mets.append(actMetric("MEM", pts.map((p) => p.mem), 100, d.mem && d.mem.total ? fmtBytes(d.mem.used) + " / " + fmtBytes(d.mem.total) : "–", memPct));
  mets.append(actMetric("SWAP", pts.map((p) => p.swap), 100, d.swap && d.swap.total ? fmtBytes(d.swap.used) : "0", swapPct));
  const netNow = (d.rx || 0) + (d.tx || 0);
  mets.append(actMetric("NET", pts.map((p) => p.net), null, actFmtRate(d.rx) + " ↓ " + actFmtRate(d.tx) + " ↑", null, netNow));
  card.append(mets);
  // expand → top processes
  card.onclick = () => {
    actOpen = actOpen === d.name ? "" : d.name;
    renderActGrid();
    if (actOpen) actLoadTop(actOpen);
  };
  if (actOpen === d.name) {
    const procs = el("div", "act-procs");
    procs.id = "actProcs";
    procs.append(el("div", "term-none", "loading processes…"));
    procs.onclick = (e) => e.stopPropagation();
    card.append(procs);
  }
  return card;
}

function actMetric(label, series, fixedMax, valueText) {
  const row = el("div", "act-metric");
  row.append(el("span", "act-met-label", label));
  row.append(actSpark(series, fixedMax));
  row.append(el("span", "act-met-val", valueText));
  return row;
}

// actSpark — a hand-rolled inline-SVG polyline sparkline (no libraries).
function actSpark(series, fixedMax) {
  const W = 120, H = 26;
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 " + W + " " + H);
  svg.setAttribute("class", "act-spark");
  const base = document.createElementNS("http://www.w3.org/2000/svg", "line");
  base.setAttribute("x1", 0); base.setAttribute("y1", H - 0.5);
  base.setAttribute("x2", W); base.setAttribute("y2", H - 0.5);
  base.setAttribute("class", "act-spark-base");
  svg.append(base);
  const vals = (series || []).filter((v) => v != null && !isNaN(v));
  if (vals.length > 1) {
    const max = fixedMax || Math.max(...vals, 1) * 1.15;
    const pl = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
    pl.setAttribute("points", vals.map((v, i) =>
      (i / (vals.length - 1) * W).toFixed(1) + "," + (H - 1.5 - Math.min(v / max, 1) * (H - 3)).toFixed(1)
    ).join(" "));
    pl.setAttribute("class", "act-spark-line");
    svg.append(pl);
    const last = document.createElementNS("http://www.w3.org/2000/svg", "circle");
    last.setAttribute("cx", W); last.setAttribute("r", 1.8);
    last.setAttribute("cy", (H - 1.5 - Math.min(vals[vals.length - 1] / max, 1) * (H - 3)).toFixed(1));
    last.setAttribute("class", "act-spark-dot");
    svg.append(last);
  }
  return svg;
}

async function actLoadTop(name) {
  clearTimeout(actTopTimer);
  const host = document.getElementById("actProcs");
  if (!host || actOpen !== name) return;
  let procs = [];
  try { procs = ((await (await fetch("/api/activity/top?device=" + encodeURIComponent(name))).json()).procs) || []; }
  catch (e) {}
  if (actOpen !== name) return;
  const cur = document.getElementById("actProcs");
  if (!cur) return;
  cur.innerHTML = "";
  if (!procs.length) { cur.append(el("div", "term-none", "no process data")); return; }
  const tbl = el("div", "act-proc-tbl");
  const hd = el("div", "act-proc-row head");
  ["process", "cpu", "mem"].forEach((h) => hd.append(el("span", "act-proc-" + h.slice(0, 3), h)));
  tbl.append(hd);
  procs.forEach((p) => {
    const row = el("div", "act-proc-row");
    row.append(el("span", "act-proc-pro", p.cmd || ""));
    row.append(el("span", "act-proc-cpu", (p.cpu || 0).toFixed(1) + "%"));
    row.append(el("span", "act-proc-mem", p.rss ? fmtBytes(p.rss) : (p.memPct || 0).toFixed(1) + "%"));
    tbl.append(row);
  });
  cur.append(tbl);
  // refresh every 10s only while open + visible (the one fork-heavy probe)
  actTopTimer = setTimeout(() => { if (actVisible() && actOpen === name) actLoadTop(name); }, 10000);
}

function actFmtDur(sec) {
  if (sec > 172800) return Math.floor(sec / 86400) + "d";
  if (sec > 7200) return Math.floor(sec / 3600) + "h";
  return Math.floor(sec / 60) + "m";
}

function actFmtRate(bps) {
  if (!bps || bps < 1) return "0";
  if (bps > 1 << 20) return (bps / (1 << 20)).toFixed(1) + "M/s";
  if (bps > 1 << 10) return (bps / (1 << 10)).toFixed(0) + "K/s";
  return bps.toFixed(0) + "B/s";
}
