// ================= CONSTRUCTION RENDERER — pinned three.js over the canonical IR =================
// Draws the server-compiled GeometryIR (world mm, Z up) with the vendored,
// pinned three.js r180 (vendor/three-0.180.0), loaded by dynamic import only
// when a workbench opens. The IR is the only geometry: this file never
// generates or edits shapes. World → three is the GLB map, (x, z, -y)/1000,
// applied once as a group transform. Picking resolves a mesh to its semantic
// component id (never a triangle index); exploded offsets are presentation
// only and measurements are taken back in canonical coordinates.

const CXR_THREE_URL = "/vendor/three-0.180.0/three.module.min.js";
let cxrThree = null;           // the loaded module
let cxrCurrent = null;         // the live renderer instance

async function cxrLoadThree() {
  if (!cxrThree) cxrThree = await import(CXR_THREE_URL);
  return cxrThree;
}

function cxRendererDispose() {
  if (cxrCurrent) { try { cxrCurrent.dispose(); } catch (e) {} cxrCurrent = null; }
}

// world mm (Z up) ↔ three metres (Y up)
function cxrToThree(T, p) { return new T.Vector3(p[0] / 1000, p[2] / 1000, -p[1] / 1000); }
function cxrToWorld(v) { return [v.x * 1000, -v.z * 1000, v.y * 1000]; }
function cxrDir(T, d) { return new T.Vector3(d[0], d[2], -d[1]).normalize(); }

function cxrCssColor(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

// cxRendererCreate builds a renderer inside host. Throws {fallback:true} when
// WebGL cannot run here; the caller then shows the technical fallback.
async function cxRendererCreate(host, opts) {
  const T = await cxrLoadThree();
  const canvas = document.createElement("canvas");
  canvas.className = "cx-canvas";
  canvas.tabIndex = 0;
  canvas.setAttribute("role", "img");
  canvas.setAttribute("aria-label", "3D assembly model — drag to orbit, shift-drag to pan, wheel to zoom, arrows rotate, 1–4 set views, click a part to select it");
  let renderer;
  try {
    renderer = new T.WebGLRenderer({ canvas, antialias: true, alpha: true, preserveDrawingBuffer: true });
  } catch (e) {
    const err = new Error("WebGL is unavailable: " + e.message); err.fallback = true; throw err;
  }
  host.append(canvas);
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.localClippingEnabled = true;
  const scene = new T.Scene();
  const world = new T.Group();           // IR space: mm, Z up
  world.rotation.x = -Math.PI / 2;
  world.scale.setScalar(1 / 1000);
  scene.add(world);
  const hemi = new T.HemisphereLight(0xffffff, 0x7a7a7a, 2.1);
  const sun = new T.DirectionalLight(0xffffff, 1.6);
  sun.position.set(2, 4, -3);
  scene.add(hemi, sun);
  const persp = new T.PerspectiveCamera(35, 1, 0.01, 200);
  const ortho = new T.OrthographicCamera(-1, 1, 1, -1, -100, 200);
  let camera = persp;
  const clip = new T.Plane(new T.Vector3(1, 0, 0), 0);
  const st = {
    ir: null, parts: new Map(), overlayGroup: new T.Group(), sectionHelper: null,
    mode: "technical", exploded: 0, hidden: new Set(), isolated: new Set(), transparent: new Set(),
    selection: "", section: null, overlays: { water: true, attachment: false }, measuring: null, ghost: null,
    center: new T.Vector3(), radius: 2, stats: { firstRenderMs: 0, buildMs: 0, frames: 0, totalFrameMs: 0, maxFrameMs: 0, triangles: 0 },
    disposed: false, lost: false, pending: false, created: performance.now(),
  };
  world.add(st.overlayGroup);
  const gl = renderer.getContext();
  const dbg = gl.getExtension("WEBGL_debug_renderer_info");
  st.stats.gpu = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
  st.stats.webgl2 = renderer.capabilities.isWebGL2;

  // ---- controls: orbit / pan / zoom, pointer + touch + keyboard ----------
  const ctl = { target: new T.Vector3(0.9, 0.1, -0.5), radius: 4, theta: Math.PI * 0.82, phi: Math.PI * 0.32, zoom: 1 };
  function applyCamera() {
    const sinP = Math.sin(ctl.phi);
    const off = new T.Vector3(ctl.radius * sinP * Math.sin(ctl.theta), ctl.radius * Math.cos(ctl.phi), ctl.radius * sinP * Math.cos(ctl.theta));
    for (const cam of [persp, ortho]) { cam.position.copy(ctl.target).add(off); cam.up.set(0, 1, 0); cam.lookAt(ctl.target); }
    ortho.zoom = ctl.zoom;
    resizeOrtho();
    requestRender();
    if (opts.onCamera) opts.onCamera(api.getCamera());
  }
  function resizeOrtho() {
    const w = canvas.clientWidth || 1, h = canvas.clientHeight || 1;
    const half = Math.max(0.2, ctl.radius * 0.45);
    ortho.left = -half * w / h; ortho.right = half * w / h; ortho.top = half; ortho.bottom = -half;
    ortho.updateProjectionMatrix();
  }
  function orbit(dx, dy) { ctl.theta -= dx * 0.008; ctl.phi = Math.max(0.05, Math.min(Math.PI - 0.05, ctl.phi - dy * 0.008)); applyCamera(); }
  function pan(dx, dy) {
    const k = ctl.radius * 0.0016 / (camera === ortho ? ctl.zoom : 1);
    const right = new T.Vector3().setFromMatrixColumn(camera.matrix, 0).multiplyScalar(-dx * k);
    const up = new T.Vector3().setFromMatrixColumn(camera.matrix, 1).multiplyScalar(dy * k);
    ctl.target.add(right).add(up); applyCamera();
  }
  function dolly(f) {
    if (camera === ortho) ctl.zoom = Math.max(0.05, Math.min(80, ctl.zoom / f));
    else ctl.radius = Math.max(0.05, Math.min(60, ctl.radius * f));
    applyCamera();
  }
  const ptrs = new Map();
  let down = null, pinch = 0;
  canvas.addEventListener("pointerdown", (e) => {
    canvas.focus({ preventScroll: true });
    canvas.setPointerCapture(e.pointerId);
    ptrs.set(e.pointerId, { x: e.clientX, y: e.clientY });
    down = { x: e.clientX, y: e.clientY, button: e.button, shift: e.shiftKey, moved: 0 };
    if (ptrs.size === 2) { const [a, b] = [...ptrs.values()]; pinch = Math.hypot(a.x - b.x, a.y - b.y); }
  });
  canvas.addEventListener("pointermove", (e) => {
    const p = ptrs.get(e.pointerId);
    if (!p || !down) return;
    const dx = e.clientX - p.x, dy = e.clientY - p.y;
    p.x = e.clientX; p.y = e.clientY;
    down.moved += Math.abs(dx) + Math.abs(dy);
    if (ptrs.size === 2) {
      const [a, b] = [...ptrs.values()];
      const d = Math.hypot(a.x - b.x, a.y - b.y);
      if (pinch > 0 && d > 0) dolly(pinch / d);
      pinch = d;
      pan(dx / 2, dy / 2);
      return;
    }
    if (down.button === 2 || down.shift) pan(dx, dy); else orbit(dx, dy);
  });
  const up = (e) => {
    ptrs.delete(e.pointerId);
    if (down && down.moved < 5 && ptrs.size === 0 && e.type === "pointerup") pickAt(e.clientX, e.clientY);
    if (ptrs.size === 0) down = null;
  };
  canvas.addEventListener("pointerup", up);
  canvas.addEventListener("pointercancel", up);
  canvas.addEventListener("contextmenu", (e) => e.preventDefault());
  canvas.addEventListener("wheel", (e) => { e.preventDefault(); dolly(e.deltaY > 0 ? 1.1 : 1 / 1.1); }, { passive: false });
  canvas.addEventListener("keydown", (e) => {
    const step = 18;
    const handled = { ArrowLeft: () => (e.shiftKey ? pan(-step, 0) : orbit(-step, 0)), ArrowRight: () => (e.shiftKey ? pan(step, 0) : orbit(step, 0)),
      ArrowUp: () => (e.shiftKey ? pan(0, -step) : orbit(0, -step)), ArrowDown: () => (e.shiftKey ? pan(0, step) : orbit(0, step)),
      "+": () => dolly(1 / 1.15), "=": () => dolly(1 / 1.15), "-": () => dolly(1.15),
      "1": () => api.view("front"), "2": () => api.view("side"), "3": () => api.view("top"), "4": () => api.view("iso"), r: () => api.view("iso") }[e.key];
    if (handled) { e.preventDefault(); handled(); }
  });

  // ---- picking and measurement ---------------------------------------------
  const ray = new T.Raycaster();
  function hitAt(cx, cy) {
    scene.updateMatrixWorld(); // a pick right after a state change must not use last frame's matrices
    const r = canvas.getBoundingClientRect();
    const ndc = new T.Vector2(((cx - r.left) / r.width) * 2 - 1, -((cy - r.top) / r.height) * 2 + 1);
    ray.setFromCamera(ndc, camera);
    const meshes = [];
    for (const part of st.parts.values()) if (part.group.visible) part.meshes.forEach((m) => { if (!part.void) meshes.push(m); });
    let hits = ray.intersectObjects(meshes, false);
    if (st.section && st.section.enabled) hits = hits.filter((h) => clip.distanceToPoint(h.point) >= -1e-6);
    return hits[0] || null;
  }
  function canonicalPoint(hit) {
    const part = st.parts.get(hit.object.userData.componentId);
    const w = cxrToWorld(hit.point);
    const off = part ? part.offset : [0, 0, 0];
    return [w[0] - off[0], w[1] - off[1], w[2] - off[2]];
  }
  function pickAt(cx, cy) {
    const hit = hitAt(cx, cy);
    if (st.measuring) {
      if (!hit) return;
      st.measuring.points.push(canonicalPoint(hit));
      if (st.measuring.points.length === 2) { const m = st.measuring; st.measuring = null; m.done(m.points); }
      return;
    }
    if (opts.onSelect) opts.onSelect(hit ? hit.object.userData.componentId : "");
  }

  // ---- building the scene from IR --------------------------------------------
  function clearParts() {
    for (const part of st.parts.values()) {
      part.meshes.forEach((m) => { m.geometry.dispose(); m.material.dispose(); });
      part.edges.forEach((l) => { l.geometry.dispose(); l.material.dispose(); });
      world.remove(part.group);
    }
    st.parts.clear();
    st.overlayGroup.children.slice().forEach((c) => { c.geometry && c.geometry.dispose(); c.material && c.material.dispose(); st.overlayGroup.remove(c); });
  }
  function makeMaterial(part) {
    const ap = part.ap, col = new T.Color(ap.color || "#999999");
    const transparent = part.void || st.transparent.has(part.id) || (ap.opacity > 0 && ap.opacity < 1);
    const opacity = part.void ? Math.min(0.22, ap.opacity || 0.22) : st.transparent.has(part.id) ? 0.28 : (ap.opacity || 1);
    const common = { color: col, transparent, opacity, side: T.DoubleSide, clippingPlanes: st.section && st.section.enabled ? [clip] : [], depthWrite: !transparent };
    const selected = part.id === st.selection;
    if (st.mode === "realistic") {
      const m = new T.MeshStandardMaterial({ ...common, roughness: ap.roughness ?? 0.8, metalness: ap.metalness ?? 0 });
      if (selected) m.emissive = new T.Color(cxrCssColor("--accent", "#265acc")).multiplyScalar(0.35);
      return m;
    }
    const m = new T.MeshLambertMaterial(common);
    if (selected) m.emissive = new T.Color(cxrCssColor("--accent", "#265acc")).multiplyScalar(0.45);
    return m;
  }
  function edgeColor() { return document.documentElement.getAttribute("data-theme") === "jarvis" ? 0xb8d8ec : 0x2b2b2b; }
  function build(ir) {
    const t0 = performance.now();
    clearParts();
    st.ir = ir;
    st.stats.triangles = ir.triangles;
    const b = ir.bounds || [0, 0, 0, 1, 1, 1];
    const cmm = [(b[0] + b[3]) / 2, (b[1] + b[4]) / 2, (b[2] + b[5]) / 2];
    st.centerMM = cmm;
    st.center.copy(cxrToThree(T, cmm));
    st.radius = Math.max(0.5, Math.hypot(b[3] - b[0], b[4] - b[1], b[5] - b[2]) / 1000);
    for (const p of ir.parts) {
      if (!p.solids || !p.solids.length) continue;
      const group = new T.Group();
      const part = { id: p.component, type: p.type, ap: p.appearance || {}, void: !!p.void, group, meshes: [], edges: [], offset: [0, 0, 0], center: [0, 0, 0] };
      let n = 0; const c = [0, 0, 0];
      for (const s of p.solids) {
        const geo = new T.BufferGeometry();
        geo.setAttribute("position", new T.Float32BufferAttribute(s.positions, 3));
        geo.setIndex(s.indices);
        geo.computeVertexNormals();
        const mesh = new T.Mesh(geo, null);
        mesh.userData.componentId = p.component;
        mesh.userData.solidKey = s.key;
        part.meshes.push(mesh);
        group.add(mesh);
        if (!p.void) {
          const lines = new T.LineSegments(new T.EdgesGeometry(geo, 28), new T.LineBasicMaterial({ color: edgeColor() }));
          lines.userData.componentId = p.component;
          part.edges.push(lines);
          group.add(lines);
        }
        for (let i = 0; i < s.positions.length; i += 3) { c[0] += s.positions[i]; c[1] += s.positions[i + 1]; c[2] += s.positions[i + 2]; n++; }
      }
      part.center = n ? [c[0] / n, c[1] / n, c[2] / n] : cmm;
      world.add(group);
      st.parts.set(p.component, part);
    }
    for (const o of ir.overlays || []) {
      const geo = new T.BufferGeometry().setFromPoints(o.points.map((q) => new T.Vector3(q[0], q[1], q[2])));
      const color = o.network === "roof" ? 0x1f6feb : o.network === "attachment" ? 0xc0304a : o.network === "interface" ? 0xc47f1a : 0x0e7490;
      const line = new T.Line(geo, new T.LineBasicMaterial({ color, depthTest: false }));
      line.renderOrder = 10;
      line.userData.kind = o.kind; line.userData.network = o.network;
      st.overlayGroup.add(line);
    }
    applyState();
    st.stats.buildMs = Math.round(performance.now() - t0);
  }
  function applyState() {
    const iso = st.isolated.size > 0;
    for (const part of st.parts.values()) {
      part.group.visible = !st.hidden.has(part.id) && (!iso || st.isolated.has(part.id));
      part.meshes.forEach((m) => { if (m.material) m.material.dispose(); m.material = makeMaterial(part); });
      part.edges.forEach((l) => {
        l.visible = st.mode === "technical" || part.id === st.selection;
        l.material.color.set(part.id === st.selection ? cxrCssColor("--accent", "#265acc") : edgeColor());
        l.material.clippingPlanes = st.section && st.section.enabled ? [clip] : [];
      });
      // exploded: a reversible presentation offset away from the centre
      const k = st.exploded * 0.9;
      part.offset = [(part.center[0] - st.centerMM[0]) * k, (part.center[1] - st.centerMM[1]) * k, (part.center[2] - st.centerMM[2]) * k];
      part.group.position.set(part.offset[0], part.offset[1], part.offset[2]);
    }
    st.overlayGroup.children.forEach((l) => { l.visible = l.userData.network === "attachment" ? st.overlays.attachment : st.overlays.water; });
    st.overlayGroup.visible = st.exploded === 0;
    if (st.section && st.section.enabled) {
      const n = cxrDir(T, st.section.normal);
      clip.setFromNormalAndCoplanarPoint(n, cxrToThree(T, st.section.originMm));
    }
    requestRender();
  }

  // ---- rendering (on demand) --------------------------------------------------
  function resize() {
    const w = host.clientWidth || 1, h = host.clientHeight || 1;
    renderer.setSize(w, h, false);
    canvas.style.width = w + "px"; canvas.style.height = h + "px";
    persp.aspect = w / h; persp.updateProjectionMatrix();
    resizeOrtho();
    requestRender();
  }
  function requestRender() {
    if (st.pending || st.disposed) return;
    st.pending = true;
    requestAnimationFrame(() => { st.pending = false; renderNow(); });
  }
  function renderNow() {
    if (st.disposed || st.lost) return;
    const t0 = performance.now();
    renderer.render(scene, camera);
    const dt = performance.now() - t0;
    st.stats.frames++; st.stats.totalFrameMs += dt; st.stats.maxFrameMs = Math.max(st.stats.maxFrameMs, dt);
    if (!st.stats.firstRenderMs && st.ir) st.stats.firstRenderMs = Math.round(performance.now() - st.created);
  }
  const ro = new ResizeObserver(resize);
  ro.observe(host);
  canvas.addEventListener("webglcontextlost", (e) => { e.preventDefault(); st.lost = true; if (opts.onContextLost) opts.onContextLost(); });
  canvas.addEventListener("webglcontextrestored", () => { st.lost = false; if (st.ir) build(st.ir); if (opts.onContextRestored) opts.onContextRestored(); });

  const views = {
    front: { theta: Math.PI, phi: Math.PI / 2 - 0.0001 },   // looking at the wall from outside (−Y), horizontal
    side: { theta: -Math.PI / 2, phi: Math.PI / 2 - 0.0001 }, // looking along +X at the end section
    top: { theta: Math.PI, phi: 0.0001 },
    iso: { theta: Math.PI * 0.82, phi: Math.PI * 0.32 },
  };
  const api = {
    setGeometry(ir, keepCamera) {
      const first = !st.ir;
      build(ir);
      if (first && !keepCamera) { ctl.target.copy(st.center); ctl.radius = st.radius * 1.4; applyCamera(); }
      requestRender();
    },
    setState(s) {
      if (s.mode) st.mode = s.mode;
      if (typeof s.exploded === "number") st.exploded = Math.max(0, Math.min(1, s.exploded));
      if (s.hidden) st.hidden = new Set(s.hidden);
      if (s.isolated) st.isolated = new Set(s.isolated);
      if (s.transparent) st.transparent = new Set(s.transparent);
      if (typeof s.selection === "string") st.selection = s.selection;
      if ("section" in s) st.section = s.section;
      if (s.overlays) st.overlays = { ...st.overlays, ...s.overlays };
      applyState();
    },
    setProjection(p) { camera = p === "orthographic" ? ortho : persp; applyCamera(); },
    projection() { return camera === ortho ? "orthographic" : "perspective"; },
    view(name) {
      const v = views[name] || views.iso;
      ctl.theta = v.theta; ctl.phi = v.phi; ctl.target.copy(st.center); ctl.radius = st.radius * 1.4; ctl.zoom = 1;
      if (name !== "iso") camera = ortho;
      applyCamera();
    },
    getCamera() {
      const cam = camera;
      return { projection: cam === ortho ? "orthographic" : "perspective", position: cxrToWorld(cam.position).map((x) => Math.round(x * 10) / 10),
        target: cxrToWorld(ctl.target).map((x) => Math.round(x * 10) / 10), up: [0, 0, 1], fov: 35, zoom: Math.round(ctl.zoom * 1000) / 1000 };
    },
    setCamera(c) {
      if (!c || !c.position || !c.target) return;
      const pos = cxrToThree(T, c.position), tgt = cxrToThree(T, c.target);
      const off = pos.clone().sub(tgt);
      ctl.target.copy(tgt); ctl.radius = Math.max(0.05, off.length());
      ctl.phi = Math.acos(Math.max(-1, Math.min(1, off.y / ctl.radius)));
      ctl.theta = Math.atan2(off.x, off.z);
      ctl.zoom = c.zoom || 1;
      camera = c.projection === "orthographic" ? ortho : persp;
      applyCamera();
    },
    measure() { return new Promise((resolve) => { st.measuring = { points: [], done: resolve }; }); },
    cancelMeasure() { st.measuring = null; },
    // canvas position of a component's centre (tests and labels)
    project(componentId) {
      const part = st.parts.get(componentId);
      if (!part) return null;
      scene.updateMatrixWorld();
      camera.updateMatrixWorld();
      const p = cxrToThree(T, [part.center[0] + part.offset[0], part.center[1] + part.offset[1], part.center[2] + part.offset[2]]).project(camera);
      const r = canvas.getBoundingClientRect();
      return { x: r.left + (p.x + 1) / 2 * r.width, y: r.top + (1 - p.y) / 2 * r.height };
    },
    pickAt(cx, cy) { const h = hitAt(cx, cy); return h ? { componentId: h.object.userData.componentId, point: canonicalPoint(h) } : null; },
    parts() { return [...st.parts.keys()]; },
    visibleParts() { return [...st.parts.values()].filter((p) => p.group.visible).map((p) => p.id); },
    offsets() { const o = {}; for (const p of st.parts.values()) o[p.id] = p.offset.slice(); return o; },
    stats() { return { ...st.stats, avgFrameMs: st.stats.frames ? Math.round(st.stats.totalFrameMs / st.stats.frames * 100) / 100 : 0, maxFrameMs: Math.round(st.stats.maxFrameMs * 100) / 100, totalFrameMs: undefined }; },
    lost() { return st.lost; },
    // test/diagnostic hooks; a lost context returns null from getExtension,
    // so the extension object is kept from before the loss
    loseContext() { st.loseExt = st.loseExt || gl.getExtension("WEBGL_lose_context"); if (st.loseExt) st.loseExt.loseContext(); return !!st.loseExt; },
    restoreContext() { if (st.loseExt) st.loseExt.restoreContext(); },
    renderNow,
    dispose() {
      st.disposed = true;
      ro.disconnect();
      clearParts();
      renderer.dispose();
      canvas.remove();
    },
  };
  resize();
  applyCamera();
  return api;
}
