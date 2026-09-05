// THE EGO GRAPH — the Network view (surface plan §5).
//
// The owner on the old Network tab: "really unclear to me how to use this rn".
// It was three lists of node ids. It became a picture — and the first picture
// was a deterministic radial layout, which he then called "too static… doesn't
// feel very useful", asking for the way Obsidian renders a graph.
//
// He is right, and the plan was wrong. A settled arrangement of dots is a
// diagram: you look at it once. What makes a graph a TOOL is that you can push
// on it — the layout relaxes under its own forces, you grab a node and the
// neighbourhood follows, you zoom into a corner, and hovering one person dims
// everyone they have nothing to do with. The structure is something you feel
// by moving it, not something you read.
//
// So this is a real force simulation, hand-rolled: velocity Verlet with
// repulsion, springs and gravity, cooling on an alpha schedule and re-heating
// whenever you touch it. No library — nothing here is fetched from a CDN.
//
// What survives from the static version, because it was the part that was
// right: this is an EGO view. You are pinned at the centre, the degree control
// bounds what is FETCHED (so a hairball is unreachable by construction, not
// hidden behind a slider), nodes are coloured by what you are DOING about the
// person, and the search box is the entry point rather than a canvas.

const RG_NS = "http://www.w3.org/2000/svg";
const RG_W = 900;
const RG_H = 620;

// ---- the physics. These are the numbers that decide whether the picture
// feels like a structure settling or like a bag of marbles.
const RG_ALPHA_DECAY = 0.022; // how fast it cools; a run is ~300 frames
const RG_ALPHA_MIN = 0.002; // below this it stops burning frames
const RG_VELOCITY_DECAY = 0.62; // friction — lower is springier, and jellier
const RG_REPEL = 3200; // node-node push
const RG_LINK_DIST = 70; // spring rest length
const RG_LINK_K = 0.6; // spring stiffness, BEFORE the degree normalisation below
const RG_GRAVITY = 0.010; // the pull that keeps detached pieces on screen

let rgState = null;

function rgInit() {
  if (!rgState) {
    rgState = {
      center: "", degree: 2, kinds: new Set(), q: "", data: null,
      sel: "", busy: false, err: "", matches: [],
      sim: null, svgEl: null, key: "", raf: 0,
      view: { x: 0, y: 0, k: 1 }, hover: "",
    };
  }
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

function rgRadius(n) {
  if (n.kind === "you") return 12;
  return Math.max(4.5, Math.min(11, 4.5 + Math.sqrt(n.deg || 0)));
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

// ============================================================================
// THE SIMULATION
// ============================================================================

// rgBuildSim turns one server answer into a body of particles.
//
// The starting arrangement is NOT random: nodes are seeded on their hop ring,
// in label order, which is the old deterministic layout used as an initial
// condition. A random start makes the same graph settle differently every
// time, and then the picture teaches you nothing you can carry between
// sessions. This way it relaxes from the same place into roughly the same
// shape — and you can still shove it wherever you like.
function rgBuildSim(data) {
  const nodes = (data.nodes || []).map((n) => Object.assign({}, n));
  const byId = {};
  const rings = {};
  nodes.forEach((n) => { (rings[n.hop] = rings[n.hop] || []).push(n); });
  Object.keys(rings).forEach((hop) => {
    const ring = rings[hop].sort((a, b) => (a.label || "").localeCompare(b.label || ""));
    const r = Number(hop) * 130;
    ring.forEach((n, i) => {
      const a = (i / ring.length) * Math.PI * 2 - Math.PI / 2;
      n.x = RG_W / 2 + Math.cos(a) * r;
      n.y = RG_H / 2 + Math.sin(a) * r;
      n.vx = 0;
      n.vy = 0;
      n.r = rgRadius(n);
      byId[n.id] = n;
    });
  });
  // YOU do not drift. The ego view has one fixed point by definition, and a
  // pinned centre is also what keeps the whole body from wandering off frame.
  const centre = byId[data.center];
  if (centre) { centre.fx = RG_W / 2; centre.fy = RG_H / 2; }

  const links = [];
  const adj = {};
  const deg = {};
  (data.edges || []).forEach((e) => {
    const a = byId[e.from], b = byId[e.to];
    if (!a || !b || a === b) return;
    links.push({ a, b, edge: e });
    deg[e.from] = (deg[e.from] || 0) + 1;
    deg[e.to] = (deg[e.to] || 0) + 1;
    (adj[e.from] = adj[e.from] || new Set()).add(e.to);
    (adj[e.to] = adj[e.to] || new Set()).add(e.from);
  });
  // ⚠ THE ONE THAT MATTERS. A spring per edge means a person with 40 edges
  // gets 40x the pull toward the middle, and the whole graph collapses into a
  // knot that uses a tenth of the frame — which is exactly what the first cut
  // did. Normalising each spring by the smaller endpoint's degree (d3-force's
  // trick) makes a hub's many weak ties behave like one strong one, and the
  // body opens out.
  links.forEach((l) => {
    l.k = RG_LINK_K / Math.min(deg[l.a.id] || 1, deg[l.b.id] || 1);
  });
  return { nodes, byId, links, adj, alpha: 1, fitted: false };
}

// rgStep is one frame of velocity Verlet.
//
// Repulsion is the honest O(n²) pass. At the sizes this view allows — a ring
// is capped at 60 and the whole answer at 240 — that is ~29k pairs worst case,
// which a browser does in well under a frame. A quadtree would be the right
// answer at 10k nodes and the wrong one here: more code to be wrong in, for
// time nobody was going to notice.
function rgStep(sim) {
  const n = sim.nodes;
  const a = sim.alpha;
  for (let i = 0; i < n.length; i++) {
    const p = n[i];
    for (let j = i + 1; j < n.length; j++) {
      const q = n[j];
      let dx = q.x - p.x, dy = q.y - p.y;
      let d2 = dx * dx + dy * dy;
      if (d2 === 0) { dx = (i % 7) - 3.5; dy = (j % 7) - 3.5; d2 = dx * dx + dy * dy; }
      if (d2 > 90000) continue; // far enough to be somebody else's problem
      const d = Math.sqrt(d2);
      const f = (RG_REPEL * a) / d2;
      const ux = (dx / d) * f, uy = (dy / d) * f;
      p.vx -= ux; p.vy -= uy;
      q.vx += ux; q.vy += uy;
    }
  }
  for (const l of sim.links) {
    const dx = l.b.x - l.a.x, dy = l.b.y - l.a.y;
    const d = Math.sqrt(dx * dx + dy * dy) || 0.001;
    const f = (d - RG_LINK_DIST) * l.k * a;
    const ux = (dx / d) * f, uy = (dy / d) * f;
    l.a.vx += ux; l.a.vy += uy;
    l.b.vx -= ux; l.b.vy -= uy;
  }
  const cx = RG_W / 2, cy = RG_H / 2;
  for (const p of n) {
    p.vx += (cx - p.x) * RG_GRAVITY * a;
    p.vy += (cy - p.y) * RG_GRAVITY * a;
    if (p.fx != null) { p.x = p.fx; p.y = p.fy; p.vx = 0; p.vy = 0; continue; }
    p.vx *= RG_VELOCITY_DECAY;
    p.vy *= RG_VELOCITY_DECAY;
    p.x += p.vx;
    p.y += p.vy;
  }
  sim.alpha += (0 - sim.alpha) * RG_ALPHA_DECAY;
}

// rgHeat re-warms the simulation. Every gesture calls it, which is the whole
// difference between a picture and a thing you are holding.
function rgHeat(to) {
  const st = rgInit();
  if (!st.sim) return;
  st.sim.alpha = Math.max(st.sim.alpha, to == null ? 0.45 : to);
  rgRun();
}

function rgRun() {
  const st = rgInit();
  if (st.raf) return;
  const frame = () => {
    st.raf = 0;
    const sim = st.sim;
    if (!sim || !st.svgEl || !st.svgEl.isConnected) return;
    rgStep(sim);
    rgDraw();
    if (sim.alpha > RG_ALPHA_MIN) { st.raf = requestAnimationFrame(frame); return; }
    // it has come to rest: frame what it settled into. Where a force layout
    // ends up is not something you can pick a viewBox for in advance, so the
    // camera is set from the result rather than guessed at from the inputs.
    if (!sim.fitted) { sim.fitted = true; rgFit(); }
  };
  st.raf = requestAnimationFrame(frame);
}

// rgDraw writes positions onto the DOM that already exists. Nothing is created
// or destroyed per frame — the elements are made once and only their transform
// and endpoints move, which is what keeps 830 edges at 60fps.
function rgDraw() {
  const st = rgInit();
  const sim = st.sim;
  if (!sim || !st.svgEl) return;
  for (const l of sim.links) {
    l.el.setAttribute("x1", l.a.x.toFixed(1));
    l.el.setAttribute("y1", l.a.y.toFixed(1));
    l.el.setAttribute("x2", l.b.x.toFixed(1));
    l.el.setAttribute("y2", l.b.y.toFixed(1));
  }
  for (const p of sim.nodes) {
    p.el.setAttribute("transform", "translate(" + p.x.toFixed(1) + "," + p.y.toFixed(1) + ")");
  }
}

// rgFit frames everything the body settled into, with a margin. It runs once
// per layout and on demand — never while you are steering, because a camera
// that keeps re-aiming itself is a camera you cannot aim.
function rgFit() {
  const st = rgInit();
  const sim = st.sim;
  if (!sim || !sim.nodes.length || !st.svgEl) return;
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const p of sim.nodes) {
    x0 = Math.min(x0, p.x - p.r); y0 = Math.min(y0, p.y - p.r);
    x1 = Math.max(x1, p.x + p.r); y1 = Math.max(y1, p.y + p.r);
  }
  const pad = 34;
  const w = Math.max(60, x1 - x0) + pad * 2, h = Math.max(60, y1 - y0) + pad * 2;
  const k = Math.max(0.4, Math.min(4, Math.min(RG_W / w, RG_H / h)));
  st.view.k = k;
  st.view.x = (x0 + x1) / 2 - RG_W / (2 * k);
  st.view.y = (y0 + y1) / 2 - RG_H / (2 * k);
  rgApplyView();
}

// ---- the viewport: zoom and pan, written straight onto the viewBox so the
// whole picture scales as geometry rather than as a bitmap.
function rgApplyView() {
  const st = rgInit();
  const v = st.view;
  const w = RG_W / v.k, h = RG_H / v.k;
  st.svgEl.setAttribute("viewBox", v.x.toFixed(1) + " " + v.y.toFixed(1) + " " + w.toFixed(1) + " " + h.toFixed(1));
  // labels stay the same SIZE on screen however far you zoom, and the whole
  // graph shows its names once you are close enough to read them — Obsidian's
  // trick, and the reason zoom is a filter here and not just magnification
  st.svgEl.style.setProperty("--rg-zoom", String(v.k));
  st.svgEl.classList.toggle("rg-near", v.k >= 1.7);
}

function rgPoint(evt) {
  const st = rgInit();
  const box = st.svgEl.getBoundingClientRect();
  const v = st.view;
  return {
    x: v.x + ((evt.clientX - box.left) / box.width) * (RG_W / v.k),
    y: v.y + ((evt.clientY - box.top) / box.height) * (RG_H / v.k),
  };
}

// ---- hover: the interaction that makes a dense graph readable. Everything
// that is not this person or a person they touch goes quiet, so one hover
// answers "who does this reach" without a click, a panel or a redraw.
function rgHoverSet(id) {
  const st = rgInit();
  if (st.hover === id) return;
  st.hover = id;
  const sim = st.sim;
  if (!sim || !st.svgEl) return;
  st.svgEl.classList.toggle("rg-hovering", !!id);
  const near = id ? (sim.adj[id] || new Set()) : null;
  for (const p of sim.nodes) {
    p.el.classList.toggle("near", !!id && (p.id === id || near.has(p.id)));
  }
  for (const l of sim.links) {
    l.el.classList.toggle("near", !!id && (l.a.id === id || l.b.id === id));
  }
}

function rgCanvas(data) {
  const st = rgInit();
  const key = [data.center, data.degree, [...st.kinds].sort().join("|"),
    (data.nodes || []).length, (data.edges || []).length].join("~");
  // ⚠ a repaint must not restart the physics. The recruiting surface rebuilds
  // its whole main column on every state change, so the SVG is built ONCE per
  // answer and re-appended on later paints — a live simulation that resets
  // every time you click something is not a live simulation.
  if (st.svgEl && st.key === key) {
    rgMarkSelection(data);
    rgRun();
    return st.svgEl;
  }
  if (st.raf) { cancelAnimationFrame(st.raf); st.raf = 0; }

  const sim = rgBuildSim(data);
  st.sim = sim;
  st.view = { x: 0, y: 0, k: 1 };
  st.hover = "";

  const svg = rgSVG("svg", "rg-canvas", { viewBox: "0 0 " + RG_W + " " + RG_H });
  st.svgEl = svg;
  st.key = key;

  const gEdges = rgSVG("g", "rg-edges", {});
  const gNodes = rgSVG("g", "rg-nodes", {});
  svg.appendChild(gEdges);
  svg.appendChild(gNodes);

  sim.links.forEach((l) => {
    const e = l.edge;
    l.el = rgSVG("line", "rg-edge" + (e.inferred ? " inferred" : ""), {});
    const title = rgSVG("title", "", {});
    title.textContent = (e.basis || e.kind || "") + (e.confidence ? " · " + e.confidence : "");
    l.el.appendChild(title);
    gEdges.appendChild(l.el);
  });

  // named nodes are appended last so their labels sit over the anonymous ring
  const order = sim.nodes.slice().sort((a, b) =>
    (a.kind === "stranger" ? 0 : 1) - (b.kind === "stranger" ? 0 : 1));
  order.forEach((n) => {
    const g = rgSVG("g", "rg-node rg-" + n.kind, {});
    g.appendChild(rgSVG("circle", "rg-dot", { r: String(n.r) }));
    const t = rgSVG("text", "rg-label", { y: String(n.r + 11), "text-anchor": "middle" });
    t.textContent = n.label.length > 24 ? n.label.slice(0, 23) + "…" : n.label;
    g.appendChild(t);
    const title = rgSVG("title", "", {});
    title.textContent = n.label + " — " + n.kind + (n.stage ? " · " + n.stage : "");
    g.appendChild(title);
    n.el = g;
    g.addEventListener("pointerenter", () => rgHoverSet(n.id));
    g.addEventListener("pointerleave", () => rgHoverSet(""));
    g.addEventListener("dblclick", (ev) => { ev.stopPropagation(); st.center = n.id; st.sel = ""; rgLoad(); });
    gNodes.appendChild(g);
  });

  rgWirePointer(svg, sim, data);
  rgMarkSelection(data);
  rgApplyView();
  rgDraw();
  rgRun();
  return svg;
}

// rgWirePointer is drag, pan and zoom on one pointer. Dragging a NODE moves
// it and re-heats the body around it; dragging the background moves the
// camera; a pointer that never really moved is a click, which selects.
function rgWirePointer(svg, sim, data) {
  const st = rgInit();
  let drag = null;

  svg.addEventListener("pointerdown", (ev) => {
    if (ev.button !== 0) return;
    const g = ev.target.closest ? ev.target.closest(".rg-node") : null;
    const node = g ? sim.nodes.find((n) => n.el === g) : null;
    const at = rgPoint(ev);
    drag = { node, moved: 0, at, start: { x: st.view.x, y: st.view.y } };
    if (node && node.id !== data.center) {
      node.fx = node.x;
      node.fy = node.y;
      rgHeat(0.5);
    }
    svg.setPointerCapture(ev.pointerId);
    svg.classList.add("rg-dragging");
  });

  svg.addEventListener("pointermove", (ev) => {
    if (!drag) return;
    const at = rgPoint(ev);
    drag.moved += Math.abs(at.x - drag.at.x) + Math.abs(at.y - drag.at.y);
    if (drag.node && drag.node.id !== data.center) {
      drag.node.fx = at.x;
      drag.node.fy = at.y;
      rgHeat(0.35);
    } else if (!drag.node) {
      st.view.x -= at.x - drag.at.x;
      st.view.y -= at.y - drag.at.y;
      rgApplyView();
      return; // the pointer moved the camera, so its graph position moved with it
    }
    drag.at = at;
  });

  const end = (ev) => {
    if (!drag) return;
    svg.classList.remove("rg-dragging");
    try { svg.releasePointerCapture(ev.pointerId); } catch (e) {}
    if (drag.node) {
      // released nodes rejoin the body — a graph you can only pin apart stops
      // telling you anything about how it hangs together
      if (drag.node.id !== data.center) { drag.node.fx = null; drag.node.fy = null; }
      if (drag.moved < 4) {
        st.sel = st.sel === drag.node.id ? "" : drag.node.id;
        if (recPaint) recPaint();
      }
      rgHeat(0.3);
    } else if (drag.moved < 4 && st.sel) {
      st.sel = "";
      if (recPaint) recPaint();
    }
    drag = null;
  };
  svg.addEventListener("pointerup", end);
  svg.addEventListener("pointercancel", end);

  svg.addEventListener("wheel", (ev) => {
    ev.preventDefault();
    const at = rgPoint(ev);
    const k = Math.max(0.4, Math.min(6, st.view.k * (ev.deltaY < 0 ? 1.12 : 1 / 1.12)));
    // zoom about the pointer, so the thing under the cursor stays under it
    st.view.x = at.x - (at.x - st.view.x) * (st.view.k / k);
    st.view.y = at.y - (at.y - st.view.y) * (st.view.k / k);
    st.view.k = k;
    rgApplyView();
  }, { passive: false });
}

// rgMarkSelection paints the selection and its route WITHOUT rebuilding
// anything — the classes move, the simulation keeps running underneath.
function rgMarkSelection(data) {
  const st = rgInit();
  const sim = st.sim;
  if (!sim) return;
  const route = st.sel ? (rgRoute(data, st.sel) || []) : [];
  const lit = new Set();
  route.forEach((e) => { lit.add(e.from + " " + e.to); lit.add(e.to + " " + e.from); });
  for (const p of sim.nodes) p.el.classList.toggle("sel", p.id === st.sel);
  for (const l of sim.links) l.el.classList.toggle("lit", lit.has(l.a.id + " " + l.b.id));
  st.svgEl.classList.toggle("rg-routing", route.length > 0);
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
    st.svgEl = null;
    st.key = "";
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
  const fit = el("button", "linkish", "fit");
  fit.title = "frame the whole graph again";
  fit.onclick = () => rgFit();
  bar.append(fit);
  const shake = el("button", "linkish", "re-settle");
  shake.title = "stir it and let it fall back together — a second arrangement often reads better";
  shake.onclick = () => { const sim = rgInit().sim; if (sim) sim.fitted = false; rgHeat(0.9); };
  bar.append(shake);
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
  row.append(el("span", "rg-howto", "drag · scroll to zoom · hover to light a neighbourhood"));
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

// ---- the panel: who this is, WHY they are connected, and what you can do
// about them from here. A node you cannot act on is a decoration.
function rgPanel(data) {
  const st = rgInit();
  const box = el("div", "rg-panel");
  if (!st.sel) {
    box.append(el("div", "rg-panel-hint",
      "hover a dot to light up who they reach · click for why they are connected"));
    box.append(el("div", "rg-panel-hint",
      "drag one to pull the shape around · drag the space to pan · scroll to zoom, and the names appear"));
    box.append(el("div", "rg-panel-hint", "double-click to stand somewhere else"));
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
