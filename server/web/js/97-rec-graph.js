// THE EGO GRAPH — the Network view (surface plan §5).
//
// The owner on the old Network tab: "really unclear to me how to use this rn".
// It was three lists of node ids. The failure mode to design against is the
// global-graph view everyone builds and nobody opens twice — a hairball that
// encodes topology and no operational state, "wallpaper after the first week".
//
// So: one centre (you), rings by hop, a degree control that BOUNDS what is
// fetched rather than what is shown, a deterministic radial layout so the
// picture is the same every time you open it, and nodes coloured by what you
// are doing about the person rather than by how many lines touch them. The
// search box is the entry point, not a canvas — van Ham & Perer's "Search,
// Show Context, Expand on Demand", in that order.
//
// No force simulation and no library: positions are a function of the data, so
// the picture is memorable, and nothing here is fetched from a CDN.

const RG_NS = "http://www.w3.org/2000/svg";
const RG_W = 760;
const RG_H = 520;

// rgOuter is the radius of the outermost ring. A one-hop picture of three
// people should not be three dots on the rim of an empty circle, so a shallow
// graph draws tighter — the layout is still a pure function of the data.
function rgOuter(maxHop) {
  const room = Math.min(RG_W, RG_H) / 2 - 46;
  return maxHop === 1 ? room * 0.62 : room;
}

let rgState = null; // {center, degree, kinds:Set, q, data, sel, busy, err, matches}

function rgInit() {
  if (!rgState) rgState = { center: "", degree: 2, kinds: new Set(), q: "", data: null, sel: "", busy: false, err: "" };
  return rgState;
}

function rgSVG(tag, cls, attrs) {
  const n = document.createElementNS(RG_NS, tag);
  if (cls) n.setAttribute("class", cls);
  Object.keys(attrs || {}).forEach((k) => n.setAttribute(k, attrs[k]));
  return n;
}

// rgLoad is the ONE fetch. Degree and edge-kind filters are query parameters
// rather than client-side filters because the whole point of a bounded view is
// that the unbounded answer is never sent.
async function rgLoad() {
  const st = rgInit();
  st.busy = true;
  st.err = "";
  try {
    const q = new URLSearchParams();
    if (st.center) q.set("center", st.center);
    q.set("degree", String(st.degree));
    if (st.kinds.size) q.set("kind", [...st.kinds].join(","));
    if (st.q.trim()) q.set("q", st.q.trim());
    const r = await fetch("/api/aion/recruiting/graph?" + q.toString());
    if (!r.ok) throw new Error(await r.text());
    st.data = await r.json();
    st.matches = st.data.search || [];
    if (st.sel && !(st.data.nodes || []).some((n) => n.id === st.sel)) st.sel = "";
  } catch (e) {
    st.err = String(e.message || e).slice(0, 200);
  }
  st.busy = false;
  if (recPaint) recPaint();
}

// ---- the layout. Deterministic: a node's position is a function of its hop
// and its rank within that ring, so the same graph draws the same picture
// every time — which is what makes it something you can remember rather than
// something you have to re-read.
function rgLayout(nodes) {
  const cx = RG_W / 2, cy = RG_H / 2;
  const rings = {};
  nodes.forEach((n) => { (rings[n.hop] = rings[n.hop] || []).push(n); });
  const maxHop = Math.max(1, ...Object.keys(rings).map(Number));
  const step = rgOuter(maxHop) / maxHop;
  const pos = {};
  Object.keys(rings).forEach((hopKey) => {
    const hop = Number(hopKey);
    const ring = rings[hopKey].slice().sort((a, b) => (a.label || "").localeCompare(b.label || ""));
    if (hop === 0) { ring.forEach((n) => { pos[n.id] = { x: cx, y: cy }; }); return; }
    const r = step * hop;
    // the half-turn offset per ring keeps hop-2 from hiding directly behind
    // hop-1, without any of the randomness that would move the picture
    const off = (hop % 2 ? 0 : Math.PI / ring.length) - Math.PI / 2;
    ring.forEach((n, i) => {
      const a = off + (i / ring.length) * Math.PI * 2;
      pos[n.id] = { x: cx + Math.cos(a) * r, y: cy + Math.sin(a) * r };
    });
  });
  return pos;
}

function rgRadius(n) {
  if (n.kind === "you") return 11;
  return Math.max(4, Math.min(10, 4 + Math.sqrt(n.deg || 0)));
}

// ---- WHY IS THIS CONNECTED: the bounded path from the centre to one node,
// computed over the drawn edges. It is the same question the PATHS pane
// answers for a candidate, asked of any node you click.
function rgRoute(data, id) {
  if (!data || !data.center || id === data.center) return null;
  const adj = {};
  (data.edges || []).forEach((e) => {
    (adj[e.from] = adj[e.from] || []).push([e.to, e]);
    (adj[e.to] = adj[e.to] || []).push([e.from, e]);
  });
  const prev = { [data.center]: null };
  let frontier = [data.center];
  while (frontier.length) {
    const next = [];
    for (const from of frontier) {
      for (const [to, e] of adj[from] || []) {
        if (to in prev) continue;
        prev[to] = [from, e];
        if (to === id) { frontier = []; next.length = 0; break; }
        next.push(to);
      }
      if (id in prev) break;
    }
    if (id in prev) break;
    frontier = next;
  }
  if (!(id in prev)) return null;
  const hops = [];
  let at = id;
  while (prev[at]) { hops.unshift(prev[at][1]); at = prev[at][0]; }
  return hops;
}

function rgLabel(data, id) {
  const n = (data.nodes || []).find((x) => x.id === id);
  return (n && n.label) || String(id || "").replace(/^contact\//, "");
}

// ---- the view
function rgView(main) {
  const st = rgInit();
  if (!st.data && !st.busy && !st.err) { rgLoad(); }

  main.append(rgToolbar());
  if (st.err) { main.append(el("div", "rec-scaffold-err", st.err)); return; }
  if (!st.data) { main.append(emptyRow("drawing…")); return; }

  const data = st.data;
  if (st.matches && st.matches.length) main.append(rgMatches(st.matches));

  const nodes = data.nodes || [];
  if (!nodes.length || !(data.edges || []).length) {
    main.append(rgEmpty(data));
    main.append(rgFold(data));
    return;
  }

  const host = el("div", "rg-host");
  const wrap = el("div", "rg-wrap");
  wrap.append(rgCanvas(data));
  wrap.append(rgPanel(data));
  host.append(wrap);
  main.append(host);
  main.append(rgLegend(data));
  main.append(rgFold(data));
}

function rgToolbar() {
  const st = rgInit();
  const bar = el("div", "rec-toolbar");
  const search = el("input", "pp-in rec-search");
  search.type = "search";
  search.placeholder = "search a person — or leave it empty for you at the centre";
  search.value = st.q;
  let t = null;
  search.oninput = () => {
    st.q = search.value;
    clearTimeout(t);
    t = setTimeout(() => rgLoad(), 220);
  };
  bar.append(search);

  const deg = el("span", "rg-degree");
  deg.append(el("span", "micro-label", "HOPS"));
  [1, 2, 3].forEach((d) => {
    const b = el("button", "filter-chip" + (st.degree === d ? " on" : ""), String(d));
    b.title = d === 1 ? "the people you know" : d === 2 ? "and who they reach" : "and who those reach";
    b.onclick = () => { st.degree = d; rgLoad(); };
    deg.append(b);
  });
  bar.append(deg);
  if (st.center) {
    const back = el("button", "linkish", "back to you");
    back.onclick = () => { st.center = ""; st.sel = ""; rgLoad(); };
    bar.append(back);
  }
  return bar;
}

// the kind chips are a FILTER on what is walked, and each says how much it
// would add — a chip you can decide about rather than toggle blindly
function rgLegend(data) {
  const st = rgInit();
  const row = el("div", "rg-legend");
  row.append(el("span", "micro-label", "EDGES"));
  (data.kinds || []).forEach((k) => {
    const on = st.kinds.has(k.kind);
    const b = el("button", "filter-chip" + (on ? " on" : ""), k.kind.replace(/_/g, " ") + " " + k.count);
    b.onclick = () => {
      if (on) st.kinds.delete(k.kind); else st.kinds.add(k.kind);
      rgLoad();
    };
    row.append(b);
  });
  if (st.kinds.size) {
    const all = el("button", "linkish", "all kinds");
    all.onclick = () => { st.kinds.clear(); rgLoad(); };
    row.append(all);
  }
  const om = data.omitted || {};
  const total = Object.keys(om).reduce((a, k) => a + om[k], 0);
  if (total) {
    // the honest half of a supernode guardrail: a ring that was cut says so
    row.append(el("span", "rg-omitted", total + " more not drawn — narrow by edge kind, or open one of them"));
  }
  return row;
}

function rgMatches(matches) {
  const st = rgInit();
  const row = el("div", "rg-matches");
  row.append(el("span", "micro-label", "CENTRE ON"));
  matches.slice(0, 12).forEach((m) => {
    const b = el("button", "filter-chip rg-" + m.kind, m.label);
    b.onclick = () => { st.center = m.id; st.sel = ""; st.q = ""; rgLoad(); };
    row.append(b);
  });
  return row;
}

function rgCanvas(data) {
  const st = rgInit();
  const nodes = data.nodes || [];
  const pos = rgLayout(nodes);
  const svg = rgSVG("svg", "rg-canvas", { viewBox: "0 0 " + RG_W + " " + RG_H, role: "img" });

  // the rings themselves, so "two hops out" is a thing you can see
  const maxHop = Math.max(1, ...nodes.map((n) => n.hop || 0));
  const step = rgOuter(maxHop) / maxHop;
  for (let h = 1; h <= maxHop; h++) {
    svg.appendChild(rgSVG("circle", "rg-ring", { cx: RG_W / 2, cy: RG_H / 2, r: String(step * h) }));
  }

  const route = st.sel ? (rgRoute(data, st.sel) || []) : [];
  const onRoute = new Set(route.map((e) => e.from + " " + e.to));
  (data.edges || []).forEach((e) => {
    const a = pos[e.from], b = pos[e.to];
    if (!a || !b) return;
    const lit = onRoute.has(e.from + " " + e.to) || onRoute.has(e.to + " " + e.from);
    const cls = "rg-edge" + (e.inferred ? " inferred" : "") + (lit ? " lit" : "");
    const line = rgSVG("line", cls, { x1: a.x.toFixed(1), y1: a.y.toFixed(1), x2: b.x.toFixed(1), y2: b.y.toFixed(1) });
    line.appendChild(rgSVG("title", "", {})).textContent = e.basis || e.kind;
    svg.appendChild(line);
  });

  // named nodes are drawn LAST so their labels sit over the anonymous ring
  // rather than under it
  const drawOrder = nodes.slice().sort((a, b) =>
    (a.kind === "stranger" ? 0 : 1) - (b.kind === "stranger" ? 0 : 1));
  drawOrder.forEach((n) => {
    const p = pos[n.id];
    if (!p) return;
    const g = rgSVG("g", "rg-node rg-" + n.kind + (st.sel === n.id ? " sel" : ""), {
      transform: "translate(" + p.x.toFixed(1) + "," + p.y.toFixed(1) + ")",
    });
    g.appendChild(rgSVG("circle", "rg-dot", { r: String(rgRadius(n)) }));
    const t = rgSVG("text", "rg-label", { y: String(rgRadius(n) + 11), "text-anchor": "middle" });
    t.textContent = n.label.length > 22 ? n.label.slice(0, 21) + "…" : n.label;
    g.appendChild(t);
    const title = rgSVG("title", "", {});
    title.textContent = n.label + " — " + n.kind + (n.stage ? " · " + n.stage : "");
    g.appendChild(title);
    g.onclick = () => { st.sel = st.sel === n.id ? "" : n.id; if (recPaint) recPaint(); };
    g.ondblclick = () => { st.center = n.id; st.sel = ""; rgLoad(); };
    svg.appendChild(g);
  });
  return svg;
}

// ---- the panel: who this is, WHY they are connected, and what you can do
// about them from here. A node you cannot act on is a decoration.
function rgPanel(data) {
  const st = rgInit();
  const box = el("div", "rg-panel");
  if (!st.sel) {
    box.append(el("div", "rg-panel-hint",
      "click a dot to see why it is connected · double-click to stand there"));
    return box;
  }
  const node = (data.nodes || []).find((n) => n.id === st.sel);
  if (!node) return box;

  box.append(el("div", "rg-panel-name", node.label));
  const meta = [node.kind === "considering" ? "on the board" : node.kind === "connector" ? "someone you'd ask"
    : node.kind === "you" ? "you" : "not on the board", node.stage, node.role].filter(Boolean).join(" · ");
  box.append(el("div", "rec-draft-sub", meta));

  const hops = rgRoute(data, st.sel);
  if (hops && hops.length) {
    const why = el("div", "rg-why");
    why.append(el("span", "micro-label", "WHY CONNECTED"));
    let at = data.center;
    hops.forEach((e) => {
      const to = e.from === at ? e.to : e.from;
      const line = el("div", "rg-hop");
      line.append(el("span", "rg-hop-name", rgLabel(data, at) + " → " + rgLabel(data, to)));
      line.append(el("span", "micro-label", (e.kind || "").replace(/_/g, " ")));
      if (e.basis) line.append(el("span", "rec-edge-basis", e.basis));
      if (e.inferred) line.append(el("span", "micro-label rec-inferred", "inferred"));
      why.append(line);
      at = to;
    });
    // the weakest hop is the honest summary of a route: a chain is worth what
    // its worst link is worth
    const weak = hops.reduce((w, e) => {
      const c = parseFloat(e.confidence || "0");
      return !w || c < w.c ? { c, e } : w;
    }, null);
    if (weak && weak.e.confidence) {
      why.append(el("div", "rg-weak", "weakest hop: " + weak.e.confidence + " · " + (weak.e.kind || "").replace(/_/g, " ")));
    }
    box.append(why);
  } else if (node.kind !== "you") {
    box.append(el("div", "rg-panel-hint", "no route from you inside " + st.degree + " hops"));
  }

  const acts = el("div", "rg-acts");
  if (node.kind === "considering") {
    const open = el("button", "pill light", "open the record");
    open.onclick = () => { recSel = node.id; recNav("board"); };
    acts.append(open);
  }
  if (node.kind === "stranger" || node.kind === "considering") {
    const ask = el("button", "pill light", "someone I'd ask");
    ask.title = "mark them as a connector — intro paths start from these people";
    ask.onclick = async () => {
      const key = node.id.replace(/^contact\//, "");
      if (await recWrite("/api/aion/recruiting/network/mark",
        { key, name: node.label }, "POST", node.label + " is someone you'd ask")) rgLoad();
    };
    acts.append(ask);
  }
  const here = el("button", "pill light", "stand here");
  here.onclick = () => { st.center = node.id; st.sel = ""; rgLoad(); };
  acts.append(here);
  box.append(acts);
  return box;
}

// The empty state IS the design: it names the two gestures that would fill it
// and takes you to them, rather than saying "no data".
function rgEmpty(data) {
  const box = el("div", "rg-empty");
  const totals = data.totals || {};
  box.append(el("div", "rg-empty-head",
    totals.edges ? "nothing to draw from here" : "the graph has no edges yet"));
  (data.missing || []).forEach((m) => box.append(el("div", "rg-empty-do", m)));
  const acts = el("div", "rg-acts");
  const people = el("button", "pill light", "open PEOPLE →");
  people.onclick = () => recNav("board");
  acts.append(people);
  const places = el("button", "pill light", "open PLACES →");
  places.onclick = () => recNav("places");
  acts.append(places);
  box.append(acts);
  return box;
}

// The three lists the Network tab used to BE are kept, folded: they answer
// "show me everything" once you already know what you are looking at.
function rgFold(data) {
  const box = el("details", "rec-advanced rg-fold");
  const sum = el("summary", "", "the lists — "
    + ((data.totals || {}).people || 0) + " people · "
    + ((data.totals || {}).edges || 0) + " edges");
  box.append(sum);
  const host = el("div", "rec-board");
  box.append(host);
  let painted = false;
  box.ontoggle = () => {
    if (!box.open || painted) return;
    painted = true;
    recNetLists(host);
  };
  return box;
}
