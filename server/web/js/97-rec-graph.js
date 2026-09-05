// THE EGO GRAPH — the Network view (surface plan §5).
//
// Three passes to get here. It was three lists of node ids ("really unclear to
// me how to use this rn"), then a deterministic radial picture ("too static…
// doesn't feel very useful"), then a force simulation in a small bordered box,
// which was alive and still did not feel good. So I went and read how Obsidian
// actually builds this view, and the gap was never the physics:
//
//   1. THE CANVAS IS THE VIEW. Obsidian's graph fills its pane edge to edge.
//      Mine was an 890x620 box in a column with a sidebar beside it — a
//      diagram embedded in a page, which is a thing you glance at.
//   2. THE CONTROLS FLOAT ON IT. One card in the corner with sections that
//      collapse — filters, display, forces — not a toolbar above the picture.
//   3. YOU TUNE THE FORCES YOURSELF. Center force, repel force, link force,
//      link distance: four sliders, and the body reorganises under your finger
//      while you drag them. THIS is what makes a graph feel like an object
//      rather than an image — not that it moves once, but that it answers you.
//   4. LABELS FADE ON A CURVE, with the threshold as a setting, instead of
//      snapping on at whatever zoom I happened to pick.
//
// What survives, because it was right: this is an EGO view. You are pinned at
// the centre, hops bound what is FETCHED (a hairball is unreachable by
// construction), and nodes are coloured by what you are DOING about the
// person — the operational state a topology-only graph never has, and the
// reason the usual one is wallpaper after the first week.
//
// Hand-rolled: velocity Verlet, no library, nothing from a CDN.

const RG_NS = "http://www.w3.org/2000/svg";

// The simulation's own coordinate space. The viewBox is fitted to the
// element's real aspect at build and on resize, so nothing is ever stretched.
let RG_W = 1200;
let RG_H = 800;

const RG_ALPHA_DECAY = 0.022;
const RG_ALPHA_MIN = 0.002;
const RG_VELOCITY_DECAY = 0.62;

// The BASE forces. Each is multiplied by its slider, so the defaults are the
// middle of a range rather than the whole story.
const RG_REPEL = 4200;
const RG_LINK_DIST = 84;
const RG_LINK_K = 0.6;
const RG_GRAVITY = 0.010;

// rgOpts is what the owner has tuned, and it persists — a graph you have set
// up the way you like and then have to set up again is not your graph.
const RG_DEFAULTS = {
  hops: 2, center: 1, repel: 1, link: 1, dist: 1,
  nodeSize: 1, linkWidth: 1, fade: 1.15, open: "forces",
};
let rgOpts = null;

function rgLoadOpts() {
  if (rgOpts) return rgOpts;
  rgOpts = Object.assign({}, RG_DEFAULTS);
  try {
    const raw = localStorage.getItem("manifest.recgraph");
    if (raw) Object.assign(rgOpts, JSON.parse(raw) || {});
  } catch (e) { /* a browser that refuses storage still gets the defaults */ }
  return rgOpts;
}

function rgSaveOpts() {
  try { localStorage.setItem("manifest.recgraph", JSON.stringify(rgOpts)); } catch (e) {}
}

let rgState = null;

function rgInit() {
  if (!rgState) {
    rgState = {
      center: "", kinds: new Set(), q: "", data: null,
      sel: "", busy: false, err: "", matches: [],
      sim: null, svgEl: null, key: "", raf: 0,
      view: { x: 0, y: 0, k: 1 }, hover: "", menu: null, fitScale: 1,
    };
    rgLoadOpts();
  }
  return rgState;
}

function rgSVG(tag, cls, attrs) {
  const n = document.createElementNS(RG_NS, tag);
  if (cls) n.setAttribute("class", cls);
  Object.keys(attrs || {}).forEach((k) => n.setAttribute(k, attrs[k]));
  return n;
}

// rgLoad is the ONE fetch. Hops and edge-kind filters are query parameters
// rather than client-side filters because the whole point of a bounded view is
// that the unbounded answer is never sent.
async function rgLoad() {
  const st = rgInit();
  st.busy = true;
  st.err = "";
  try {
    const q = new URLSearchParams();
    if (st.center) q.set("center", st.center);
    q.set("degree", String(rgOpts.hops));
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

// A dot lives in graph units, so it magnifies with the camera. Framing a
// small body therefore had a choice between a picture that fills the frame
// and dots that are not balloons — the fit was capped at 1.35 to keep the
// dots, and the picture stayed small in a big empty box.
//
// rgFitScale breaks that tie: the fit may zoom as far as it likes, and the
// base radius is divided back down so the dots stay in a readable band on
// SCREEN. The owner's node-size slider still multiplies on top of it, and
// zooming by hand still magnifies, because that is what zooming is for.
function rgRadius(n) {
  const st = rgInit();
  const base = n.kind === "you" ? 12 : Math.max(4.5, Math.min(11, 4.5 + Math.sqrt(n.deg || 0)));
  return base * rgOpts.nodeSize * (st.fitScale || 1);
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

function rgNode(id) {
  const st = rgInit();
  return (st.data && (st.data.nodes || []).find((n) => n.id === id)) || null;
}

// ============================================================================
// THE SIMULATION
// ============================================================================

// The starting arrangement is NOT random: nodes are seeded on their hop ring,
// in label order. A random start makes the same graph settle differently every
// time, and then the picture teaches you nothing you can carry between
// sessions. It relaxes from the same place — and you can still shove it.
function rgBuildSim(data) {
  const nodes = (data.nodes || []).map((n) => Object.assign({}, n));
  const byId = {};
  const rings = {};
  nodes.forEach((n) => { (rings[n.hop] = rings[n.hop] || []).push(n); });
  Object.keys(rings).forEach((hop) => {
    const ring = rings[hop].sort((a, b) => (a.label || "").localeCompare(b.label || ""));
    const r = Number(hop) * 150;
    ring.forEach((n, i) => {
      const a = (i / ring.length) * Math.PI * 2 - Math.PI / 2;
      n.x = RG_W / 2 + Math.cos(a) * r;
      n.y = RG_H / 2 + Math.sin(a) * r;
      n.vx = 0; n.vy = 0;
      n.r = rgRadius(n);
      byId[n.id] = n;
    });
  });
  // YOU do not drift. The ego view has one fixed point by definition, and a
  // pinned centre also keeps the body from wandering off frame.
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
  // gets 40x the pull inward, and the body collapses into a knot using a tenth
  // of the frame. Normalising each spring by its smaller endpoint's degree
  // (d3-force's trick) makes a hub's many weak ties behave like one strong one.
  links.forEach((l) => { l.norm = 1 / Math.min(deg[l.a.id] || 1, deg[l.b.id] || 1); });
  return { nodes, byId, links, adj, deg, alpha: 1, fitted: false };
}

// rgStep is one frame of velocity Verlet. Repulsion is the honest O(n²) pass:
// a ring is capped at 60 and the whole answer at 240, so ~29k pairs worst
// case, which a browser does in a fraction of a frame. A quadtree is the right
// answer at 10k nodes and the wrong one here.
function rgStep(sim) {
  const n = sim.nodes;
  const a = sim.alpha;
  const repel = RG_REPEL * rgOpts.repel;
  const rest = RG_LINK_DIST * rgOpts.dist;
  const k = RG_LINK_K * rgOpts.link;
  const grav = RG_GRAVITY * rgOpts.center;
  const far = Math.max(90000, (rest * 6) * (rest * 6));
  for (let i = 0; i < n.length; i++) {
    const p = n[i];
    for (let j = i + 1; j < n.length; j++) {
      const q = n[j];
      let dx = q.x - p.x, dy = q.y - p.y;
      let d2 = dx * dx + dy * dy;
      if (d2 === 0) { dx = (i % 7) - 3.5; dy = (j % 7) - 3.5; d2 = dx * dx + dy * dy; }
      if (d2 > far) continue;
      const d = Math.sqrt(d2);
      const f = (repel * a) / d2;
      const ux = (dx / d) * f, uy = (dy / d) * f;
      p.vx -= ux; p.vy -= uy;
      q.vx += ux; q.vy += uy;
    }
  }
  for (const l of sim.links) {
    const dx = l.b.x - l.a.x, dy = l.b.y - l.a.y;
    const d = Math.sqrt(dx * dx + dy * dy) || 0.001;
    const f = (d - rest) * l.norm * k * a;
    const ux = (dx / d) * f, uy = (dy / d) * f;
    l.a.vx += ux; l.a.vy += uy;
    l.b.vx -= ux; l.b.vy -= uy;
  }
  const cx = RG_W / 2, cy = RG_H / 2;
  for (const p of n) {
    p.vx += (cx - p.x) * grav * a;
    p.vy += (cy - p.y) * grav * a;
    if (p.fx != null) { p.x = p.fx; p.y = p.fy; p.vx = 0; p.vy = 0; continue; }
    p.vx *= RG_VELOCITY_DECAY;
    p.vy *= RG_VELOCITY_DECAY;
    p.x += p.vx;
    p.y += p.vy;
  }
  sim.alpha += (0 - sim.alpha) * RG_ALPHA_DECAY;
}

// rgHeat re-warms the simulation. Every gesture and every slider calls it,
// which is the whole difference between a picture and a thing you are holding.
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
    // ends up is not something you can pick a viewBox for in advance.
    if (!sim.fitted) { sim.fitted = true; rgFit(); }
  };
  st.raf = requestAnimationFrame(frame);
}

// rgDraw writes positions onto DOM that already exists. Nothing is created or
// destroyed per frame — only transforms and endpoints move, which is what
// keeps 830 edges at 60fps.
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

// rgSizes re-reads the display sliders without rebuilding anything.
function rgSizes() {
  const st = rgInit();
  if (!st.sim || !st.svgEl) return;
  for (const p of st.sim.nodes) {
    p.r = rgRadius(p);
    p.dot.setAttribute("r", p.r.toFixed(1));
    p.text.setAttribute("y", (p.r + 11).toFixed(1));
  }
  st.svgEl.style.setProperty("--rg-link-w", String(rgOpts.linkWidth));
  rgFade();
}

// ---- THE TEXT FADE. Obsidian's "text fade threshold": labels are not on or
// off, they come up on a curve as you zoom in, and where that curve sits is
// yours to set. A graph you cannot read at a glance and cannot quieten is
// equally useless in both directions.
function rgFade() {
  const st = rgInit();
  if (!st.svgEl) return;
  const k = st.view.k;
  const t = rgOpts.fade;
  const op = Math.max(0, Math.min(1, (k - t * 0.55) / (t * 0.75)));
  st.svgEl.style.setProperty("--rg-label", op.toFixed(3));
  // the people you have a relationship WITH are legible sooner than the
  // strangers around them — a ring of 60 names is the hairball arriving
  // through labels rather than through edges
  st.svgEl.style.setProperty("--rg-label-named", Math.max(op, 0.85).toFixed(3));
}

// rgFit frames everything the body settled into, with a margin. Once per
// layout and on demand — never while you are steering, because a camera that
// keeps re-aiming itself is a camera you cannot aim.
function rgFit() {
  const st = rgInit();
  const sim = st.sim;
  if (!sim || !sim.nodes.length || !st.svgEl) return;
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const p of sim.nodes) {
    x0 = Math.min(x0, p.x - p.r); y0 = Math.min(y0, p.y - p.r);
    x1 = Math.max(x1, p.x + p.r); y1 = Math.max(y1, p.y + p.r);
  }
  const pad = 46;
  const w = Math.max(60, x1 - x0) + pad * 2, h = Math.max(60, y1 - y0) + pad * 2;

  // ⚠ THE VISIBLE REGION IS NOT THE CANVAS. The controls float over the left
  // of it and the detail card over the right, so fitting to the whole element
  // parks the body under a panel and leaves the empty half on show. The frame
  // is the part you can actually see.
  const box = st.svgEl.getBoundingClientRect();
  const wide = box.width > 700;
  const leftPx = wide ? 264 : 0;
  const rightPx = wide && st.sel ? 352 : 0;
  const botPx = wide ? 52 : 0;
  const usableW = Math.max(120, box.width - leftPx - rightPx);
  const usableH = Math.max(120, box.height - botPx);

  const k = Math.max(0.3, Math.min(3.2,
    Math.min((RG_W * usableW / box.width) / w, (RG_H * usableH / box.height) / h)));
  st.view.k = k;
  // the dots come back down by however far the camera went in
  const scale = Math.max(0.42, Math.min(1, 1.15 / k));
  if (Math.abs(scale - (st.fitScale || 1)) > 0.01) {
    st.fitScale = scale;
    rgSizes();
  }
  const cxPx = leftPx + usableW / 2, cyPx = usableH / 2;
  st.view.x = (x0 + x1) / 2 - (cxPx / box.width) * (RG_W / k);
  st.view.y = (y0 + y1) / 2 - (cyPx / box.height) * (RG_H / k);
  rgApplyView();
}

function rgApplyView() {
  const st = rgInit();
  if (!st.svgEl) return;
  const v = st.view;
  const w = RG_W / v.k, h = RG_H / v.k;
  st.svgEl.setAttribute("viewBox", v.x.toFixed(1) + " " + v.y.toFixed(1) + " " + w.toFixed(1) + " " + h.toFixed(1));
  // labels and strokes keep their SIZE on screen however far you zoom, so
  // zooming spreads the names apart instead of magnifying them into each other
  st.svgEl.style.setProperty("--rg-zoom", String(v.k));
  rgFade();
}

// rgMeasure matches the simulation's coordinate space to the element's real
// aspect ratio, so a circle is a circle at any pane width.
function rgMeasure(svg) {
  const box = svg.getBoundingClientRect();
  if (box.width < 40 || box.height < 40) return false;
  const H = 800;
  const W = Math.round(H * (box.width / box.height));
  const changed = W !== RG_W || H !== RG_H;
  RG_W = W; RG_H = H;
  return changed;
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

function rgZoomBy(mult, at) {
  const st = rgInit();
  const c = at || { x: st.view.x + RG_W / (2 * st.view.k), y: st.view.y + RG_H / (2 * st.view.k) };
  const k = Math.max(0.25, Math.min(8, st.view.k * mult));
  st.view.x = c.x - (c.x - st.view.x) * (st.view.k / k);
  st.view.y = c.y - (c.y - st.view.y) * (st.view.k / k);
  st.view.k = k;
  rgApplyView();
}

// ---- hover: the interaction that makes a dense graph readable. Everything
// the person under the cursor does not touch goes quiet, so "who does this
// reach" is answered without a click, a panel or a redraw.
function rgHoverSet(id) {
  const st = rgInit();
  if (st.hover === id) return;
  st.hover = id;
  const sim = st.sim;
  if (!sim || !st.svgEl) return;
  st.svgEl.classList.toggle("rg-hovering", !!id);
  const near = id ? (sim.adj[id] || new Set()) : null;
  for (const p of sim.nodes) p.el.classList.toggle("near", !!id && (p.id === id || near.has(p.id)));
  for (const l of sim.links) l.el.classList.toggle("near", !!id && (l.a.id === id || l.b.id === id));
}

// ============================================================================
// THE CANVAS
// ============================================================================

function rgCanvas(data) {
  const st = rgInit();
  const key = [data.center, data.degree, [...st.kinds].sort().join("|"),
    (data.nodes || []).length, (data.edges || []).length].join("~");
  // ⚠ a repaint must not restart the physics. The recruiting surface rebuilds
  // its whole main column on every state change, so the SVG is built ONCE per
  // answer and re-appended on later paints.
  if (st.svgEl && st.key === key) {
    rgMarkSelection(data);
    rgRun();
    return st.svgEl;
  }
  if (st.raf) { cancelAnimationFrame(st.raf); st.raf = 0; }

  const svg = rgSVG("svg", "rg-canvas", {});
  st.svgEl = svg;
  st.key = key;
  st.hover = "";
  st.sim = null;

  const gEdges = rgSVG("g", "rg-edges", {});
  const gNodes = rgSVG("g", "rg-nodes", {});
  svg.appendChild(gEdges);
  svg.appendChild(gNodes);

  // the element has to be in the document before it can be measured, so the
  // body is built on the next frame — which is also when the pane's real size
  // is finally known
  requestAnimationFrame(() => {
    if (!svg.isConnected || st.svgEl !== svg) return;
    rgMeasure(svg);
    const sim = rgBuildSim(data);
    st.sim = sim;
    st.view = { x: 0, y: 0, k: 1 };

    sim.links.forEach((l) => {
      const e = l.edge;
      l.el = rgSVG("line", "rg-edge" + (e.inferred ? " inferred" : ""), {});
      const title = rgSVG("title", "", {});
      title.textContent = (e.basis || e.kind || "") + (e.confidence ? " · " + e.confidence : "");
      l.el.appendChild(title);
      gEdges.appendChild(l.el);
    });

    // named nodes are appended last so their labels sit over the ring
    const order = sim.nodes.slice().sort((a, b) =>
      (a.kind === "stranger" ? 0 : 1) - (b.kind === "stranger" ? 0 : 1));
    order.forEach((n) => {
      const g = rgSVG("g", "rg-node rg-" + n.kind, {});
      n.dot = rgSVG("circle", "rg-dot", { r: String(n.r) });
      g.appendChild(n.dot);
      n.text = rgSVG("text", "rg-label", { y: String(n.r + 11), "text-anchor": "middle" });
      n.text.textContent = n.label.length > 26 ? n.label.slice(0, 25) + "…" : n.label;
      g.appendChild(n.text);
      const title = rgSVG("title", "", {});
      title.textContent = n.label + " — " + n.kind + (n.stage ? " · " + n.stage : "");
      g.appendChild(title);
      n.el = g;
      g.addEventListener("pointerenter", () => rgHoverSet(n.id));
      g.addEventListener("pointerleave", () => rgHoverSet(""));
      g.addEventListener("dblclick", (ev) => { ev.stopPropagation(); rgStand(n.id); });
      g.addEventListener("contextmenu", (ev) => { ev.preventDefault(); ev.stopPropagation(); rgMenu(n.id, ev); });
      gNodes.appendChild(g);
    });

    rgWirePointer(svg, sim, data);
    rgMarkSelection(data);
    rgSizes();
    rgApplyView();
    rgDraw();
    rgRun();
  });
  return svg;
}

function rgStand(id) {
  const st = rgInit();
  st.center = id;
  st.sel = "";
  rgLoad();
}

// rgWirePointer is drag, pan and zoom on one pointer. Dragging a NODE moves it
// and re-heats the body around it; dragging the background moves the camera; a
// pointer that never really moved is a click, which selects.
function rgWirePointer(svg, sim, data) {
  const st = rgInit();
  let drag = null;

  svg.addEventListener("pointerdown", (ev) => {
    if (ev.button !== 0) return;
    rgCloseMenu();
    svg.focus({ preventScroll: true });
    const g = ev.target.closest ? ev.target.closest(".rg-node") : null;
    const node = g ? sim.nodes.find((n) => n.el === g) : null;
    drag = { node, moved: 0, at: rgPoint(ev) };
    if (node && node.id !== data.center) { node.fx = node.x; node.fy = node.y; rgHeat(0.5); }
    try { svg.setPointerCapture(ev.pointerId); } catch (e) {}
    svg.classList.add("rg-dragging");
  });

  svg.addEventListener("pointermove", (ev) => {
    if (!drag) return;
    const at = rgPoint(ev);
    drag.moved += Math.abs(at.x - drag.at.x) + Math.abs(at.y - drag.at.y);
    if (drag.node && drag.node.id !== data.center) {
      drag.node.fx = at.x; drag.node.fy = at.y;
      rgHeat(0.35);
    } else if (!drag.node) {
      st.view.x -= at.x - drag.at.x;
      st.view.y -= at.y - drag.at.y;
      rgApplyView();
      return; // the camera moved, so the pointer's graph position moved with it
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
      if (drag.moved < 4) { st.sel = st.sel === drag.node.id ? "" : drag.node.id; if (recPaint) recPaint(); }
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
    rgZoomBy(ev.deltaY < 0 ? 1.12 : 1 / 1.12, rgPoint(ev));
  }, { passive: false });

  svg.addEventListener("contextmenu", (ev) => { ev.preventDefault(); rgCloseMenu(); });

  // KEYBOARD, the way Obsidian does it: +/- zoom, arrows pan, shift is faster.
  svg.setAttribute("tabindex", "0");
  svg.addEventListener("keydown", (ev) => {
    const step = (ev.shiftKey ? 160 : 60) / st.view.k;
    let hit = true;
    switch (ev.key) {
      case "+": case "=": rgZoomBy(1.2); break;
      case "-": case "_": rgZoomBy(1 / 1.2); break;
      case "ArrowLeft": st.view.x -= step; rgApplyView(); break;
      case "ArrowRight": st.view.x += step; rgApplyView(); break;
      case "ArrowUp": st.view.y -= step; rgApplyView(); break;
      case "ArrowDown": st.view.y += step; rgApplyView(); break;
      case "0": rgFit(); break;
      case "Escape": rgCloseMenu(); if (st.sel) { st.sel = ""; if (recPaint) recPaint(); } break;
      default: hit = false;
    }
    if (hit) ev.preventDefault();
  });

  // a pane that changes width must not distort what is drawn in it
  if (window.ResizeObserver) {
    const ro = new ResizeObserver(() => {
      if (!svg.isConnected) { ro.disconnect(); return; }
      if (rgMeasure(svg)) rgApplyView();
    });
    ro.observe(svg);
  }
}

// ---- the right-click menu: every node acts, and the actions are where a
// person expects to find them rather than only in a side panel.
function rgCloseMenu() {
  const st = rgInit();
  if (st.menu) { st.menu.remove(); st.menu = null; }
}

function rgMenu(id, ev) {
  const st = rgInit();
  rgCloseMenu();
  const node = rgNode(id);
  if (!node) return;
  const menu = el("div", "rg-menu");
  menu.style.left = Math.min(ev.clientX, window.innerWidth - 200) + "px";
  menu.style.top = Math.min(ev.clientY, window.innerHeight - 200) + "px";
  menu.append(el("div", "rg-menu-head", node.label));
  const item = (label, fn) => {
    const b = el("button", "rg-menu-item", label);
    b.onclick = () => { rgCloseMenu(); fn(); };
    menu.append(b);
  };
  item("stand here", () => rgStand(id));
  item("why connected", () => { st.sel = id; if (recPaint) recPaint(); });
  if (node.kind === "considering") item("open the record", () => { recSel = id; recNav("board"); });
  if (node.kind === "stranger" || node.kind === "considering") {
    item("someone I'd ask", async () => {
      if (await recWrite("/api/aion/recruiting/network/mark",
        { key: id.replace(/^contact\//, ""), name: node.label }, "POST",
        node.label + " is someone you'd ask")) rgLoad();
    });
  }
  document.body.append(menu);
  st.menu = menu;
  setTimeout(() => {
    const away = (e) => {
      if (menu.contains(e.target)) return;
      document.removeEventListener("pointerdown", away);
      rgCloseMenu();
    };
    document.addEventListener("pointerdown", away);
  }, 0);
}

// rgMarkSelection paints the selection and its route WITHOUT rebuilding
// anything — the classes move, the simulation keeps running underneath.
function rgMarkSelection(data) {
  const st = rgInit();
  const sim = st.sim;
  if (!sim || !st.svgEl) return;
  const route = st.sel ? (rgRoute(data, st.sel) || []) : [];
  const lit = new Set();
  route.forEach((e) => { lit.add(e.from + " " + e.to); lit.add(e.to + " " + e.from); });
  for (const p of sim.nodes) p.el.classList.toggle("sel", p.id === st.sel);
  for (const l of sim.links) l.el.classList.toggle("lit", lit.has(l.a.id + " " + l.b.id));
  st.svgEl.classList.toggle("rg-routing", route.length > 0);
}

// ============================================================================
// THE CONTROLS — one card floating on the canvas, sections that collapse.
// Obsidian's shape, because it is the right one: the picture is the view, and
// the settings are something you pull over it when you want them.
// ============================================================================

function rgSlider(label, key, min, max, step, onChange, hint) {
  const row = el("label", "rg-slider");
  const head = el("span", "rg-slider-head");
  head.append(el("span", "rg-slider-label", label));
  const val = el("span", "rg-slider-val", String(rgOpts[key]));
  head.append(val);
  row.append(head);
  const input = el("input", "rg-range");
  input.type = "range";
  input.min = String(min); input.max = String(max); input.step = String(step);
  input.value = String(rgOpts[key]);
  if (hint) row.title = hint;
  // `input`, not `change`: the body must reorganise under your finger. That
  // responsiveness IS the feature — it is what turns a picture into an object.
  input.oninput = () => {
    rgOpts[key] = parseFloat(input.value);
    val.textContent = String(rgOpts[key]);
    onChange();
  };
  input.onchange = () => rgSaveOpts();
  row.append(input);
  return row;
}

function rgSection(name, title, build) {
  const box = el("div", "rg-sec" + (rgOpts.open === name ? " open" : ""));
  const head = el("button", "rg-sec-head", title);
  head.onclick = () => {
    rgOpts.open = rgOpts.open === name ? "" : name;
    rgSaveOpts();
    if (recPaint) recPaint();
  };
  box.append(head);
  if (rgOpts.open === name) {
    const body = el("div", "rg-sec-body");
    build(body);
    box.append(body);
  }
  return box;
}

function rgControls(data) {
  const st = rgInit();
  const card = el("div", "rg-controls");

  const search = el("input", "pp-in rg-search");
  search.type = "search";
  search.placeholder = "find a person…";
  search.value = st.q;
  let t = null;
  search.oninput = () => {
    st.q = search.value;
    clearTimeout(t);
    t = setTimeout(() => rgLoad(), 220);
  };
  card.append(search);
  if (st.matches && st.matches.length) card.append(rgMatches(st.matches));

  card.append(rgSection("filters", "filters", (body) => {
    body.append(rgSlider("hops from you", "hops", 1, 3, 1, () => { rgSaveOpts(); rgLoad(); },
      "1 = the people you know · 2 = and who they reach · 3 = and who those reach"));
    const kinds = el("div", "rg-chips");
    (data.kinds || []).forEach((k) => {
      const on = st.kinds.has(k.kind);
      const b = el("button", "filter-chip" + (on ? " on" : ""), k.kind.replace(/_/g, " ") + " " + k.count);
      b.title = "each chip says how many edges of that kind exist in the WHOLE graph";
      b.onclick = () => { if (on) st.kinds.delete(k.kind); else st.kinds.add(k.kind); rgLoad(); };
      kinds.append(b);
    });
    body.append(kinds);
    if (st.kinds.size) {
      const all = el("button", "linkish", "all kinds");
      all.onclick = () => { st.kinds.clear(); rgLoad(); };
      body.append(all);
    }
    const om = data.omitted || {};
    const total = Object.keys(om).reduce((a, k) => a + om[k], 0);
    // the honest half of a supernode guardrail: a ring that was cut says so
    if (total) body.append(el("div", "rg-note", total + " more not drawn — narrow by edge kind, or stand on one of them"));
  }));

  card.append(rgSection("display", "display", (body) => {
    body.append(rgSlider("node size", "nodeSize", 0.5, 2.5, 0.1, () => rgSizes()));
    body.append(rgSlider("link thickness", "linkWidth", 0.3, 3, 0.1, () => rgSizes()));
    body.append(rgSlider("text fade threshold", "fade", 0.3, 3, 0.05, () => rgFade(),
      "how far in you have to be before names come up"));
  }));

  card.append(rgSection("forces", "forces", (body) => {
    body.append(rgSlider("center force", "center", 0, 3, 0.05, () => rgHeat(0.3),
      "how hard everything is pulled to the middle"));
    body.append(rgSlider("repel force", "repel", 0.1, 4, 0.05, () => rgHeat(0.3),
      "how hard people push each other apart"));
    body.append(rgSlider("link force", "link", 0, 3, 0.05, () => rgHeat(0.3),
      "how tightly a connection pulls two people together"));
    body.append(rgSlider("link distance", "dist", 0.3, 3, 0.05, () => rgHeat(0.3),
      "how long a connection wants to be"));
    const acts = el("div", "rg-acts");
    const reset = el("button", "linkish", "defaults");
    reset.onclick = () => {
      Object.assign(rgOpts, RG_DEFAULTS, { open: rgOpts.open, hops: rgOpts.hops });
      rgSaveOpts();
      rgSizes();
      rgHeat(0.8);
      if (recPaint) recPaint();
    };
    acts.append(reset);
    const shake = el("button", "linkish", "re-settle");
    shake.title = "stir it and let it fall back together";
    shake.onclick = () => { const sim = rgInit().sim; if (sim) sim.fitted = false; rgHeat(0.95); };
    acts.append(shake);
    body.append(acts);
  }));

  return card;
}

// rgViewport is the strip that always shows, whatever is collapsed: where you
// are standing, and the way back.
function rgViewport(data) {
  const st = rgInit();
  const bar = el("div", "rg-viewport");
  bar.append(el("span", "rg-standing", "centred on " + ((data.focus || {}).label || "you")));
  if (st.center) {
    const back = el("button", "linkish", "back to you");
    back.onclick = () => { st.center = ""; st.sel = ""; rgLoad(); };
    bar.append(back);
  }
  const fit = el("button", "linkish", "fit");
  fit.title = "frame the whole graph again (0)";
  fit.onclick = () => rgFit();
  bar.append(fit);
  bar.append(el("span", "rg-note", "drag · scroll to zoom · hover to light a neighbourhood · right-click for actions"));
  return bar;
}

function rgMatches(matches) {
  const st = rgInit();
  const row = el("div", "rg-matches");
  matches.slice(0, 8).forEach((m) => {
    const b = el("button", "filter-chip rg-" + m.kind, m.label);
    b.title = "stand on " + m.label;
    b.onclick = () => { st.center = m.id; st.sel = ""; st.q = ""; rgLoad(); };
    row.append(b);
  });
  return row;
}

// ---- the view
function rgView(main) {
  const st = rgInit();
  if (!st.data && !st.busy && !st.err) { rgLoad(); }

  if (st.err) { main.append(el("div", "rec-scaffold-err", st.err)); return; }
  if (!st.data) { main.append(emptyRow("drawing…")); return; }

  const data = st.data;
  const nodes = data.nodes || [];
  if (!nodes.length || !(data.edges || []).length) {
    st.svgEl = null;
    st.key = "";
    main.append(rgEmpty(data));
    main.append(rgFold(data));
    return;
  }

  // THE CANVAS IS THE VIEW: it fills the pane and everything else floats on
  // top of it. That is the whole difference between a diagram in a page and a
  // place you are standing in.
  const stage = el("div", "rg-stage");
  stage.append(rgCanvas(data));
  stage.append(rgControls(data));
  stage.append(rgViewport(data));
  const panel = rgPanel(data);
  if (panel) stage.append(panel);
  main.append(stage);
  main.append(rgFold(data));
}

// The detail card floats over the canvas and only when you have picked
// somebody. The gestures it used to explain now live in the viewport strip,
// where they are readable before you have clicked anything.
function rgPanel(data) {
  const st = rgInit();
  if (!st.sel) return null;
  const node = (data.nodes || []).find((n) => n.id === st.sel);
  if (!node) return null;
  const box = el("div", "rg-panel");
  const head = el("div", "rg-panel-top");
  head.append(el("div", "rg-panel-name", node.label));
  const close = el("button", "rg-panel-x", "\u00d7");
  close.title = "close (esc)";
  close.onclick = () => { st.sel = ""; if (recPaint) recPaint(); };
  head.append(close);
  box.append(head);
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
