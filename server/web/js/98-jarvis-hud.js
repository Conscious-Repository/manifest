// JARVIS · CINEMATIC HUD (css/98-jarvis-hud.css). Everything here is inert
// unless <html data-theme="jarvis"> and the owner has not chosen the Classic
// jarvis style. It adds no data and fakes no readings: the rail readout shows
// the real connection state and clock; motion happens only on arrival
// (a boot sequence once per browser session) and on changing section (the
// title decodes, the page assembles). Reduced motion turns both off.
//
// Rollback: Settings › Display › manifest.theme › jarvis-og (this browser),
// or remove this file and css/98-jarvis-hud.css with their two tags.
(function () {
  const root = document.documentElement;
  const embedded = window.parent !== window; // tiles and side chats: no boot, no readout
  const reduced = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // The theme owns the attribute (99-boot setTheme: jarvis-cinematic sets
  // data-hud); this layer follows it.
  function hudOn() { return root.getAttribute("data-hud") === "cinematic"; }
  let wasOn = hudOn();
  function hudSync() { readoutSync(); const on = hudOn(); if (on && !wasOn) assemble(true); wasOn = on; }
  new MutationObserver(hudSync).observe(root, {attributes: true, attributeFilter: ["data-hud"]});

  // ── text decode: short labels only; the real text stays the accessible name
  const glyphs = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/<>[]#";
  function decode(node, ms = 360) {
    const text = node.textContent;
    if (!text || text.length > 28 || reduced() || node.dataset.decoding) return;
    node.dataset.decoding = "1";
    node.setAttribute("aria-label", text);
    const start = performance.now();
    const step = now => {
      const t = Math.min(1, (now - start) / ms);
      const fixed = Math.floor(t * text.length);
      let out = text.slice(0, fixed);
      for (let i = fixed; i < text.length; i++) out += text[i] === " " ? " " : glyphs[(Math.random() * glyphs.length) | 0];
      node.textContent = out;
      if (t < 1) requestAnimationFrame(step);
      else { node.textContent = text; node.removeAttribute("aria-label"); delete node.dataset.decoding; }
    };
    requestAnimationFrame(step);
  }

  // ── assemble: once per section change, the visible page draws in
  let lastSection = "";
  function visibleSection() {
    const scroll = document.getElementById("contentScroll");
    return scroll ? [...scroll.children].find(el => el.tagName === "SECTION" && !el.hidden) || null : null;
  }
  function assemble(force) {
    if (!hudOn() || reduced()) return;
    const section = visibleSection();
    if (!section || (!force && section.id === lastSection)) return;
    lastSection = section.id;
    section.classList.remove("hud-assemble");
    void section.offsetWidth;
    section.classList.add("hud-assemble");
    setTimeout(() => section.classList.remove("hud-assemble"), 700);
    const title = section.querySelector(".agent-head .agent-title");
    // titles that carry controls (links, buttons) keep their DOM untouched
    if (title && !title.children.length) decode(title);
  }
  window.addEventListener("hashchange", () => requestAnimationFrame(() => requestAnimationFrame(() => assemble(false))));

  // ── the rail readout: real connection state and the clock
  let readout = null, clock = 0;
  function readoutSync() {
    const foot = document.querySelector(".rail-foot");
    if (!hudOn() || embedded || !foot) { clearInterval(clock); clock = 0; readout?.remove(); readout = null; return; }
    if (!readout) {
      readout = document.createElement("div");
      readout.className = "hud-readout";
      readout.setAttribute("aria-hidden", "true"); // the save state and connection are announced elsewhere
      for (const cls of ["hud-readout-dot", "hud-readout-state", "hud-readout-time"]) { const span = document.createElement("span"); span.className = cls; readout.append(span); }
      foot.prepend(readout);
    }
    const tick = () => {
      if (!readout) return;
      const now = new Date();
      readout.querySelector(".hud-readout-state").textContent = navigator.onLine ? "Online" : "Offline";
      readout.dataset.state = navigator.onLine ? "ok" : "off";
      readout.querySelector(".hud-readout-time").textContent = now.toLocaleTimeString([], {hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false}) + " · " + String(now.getDate()).padStart(2, "0") + "." + String(now.getMonth() + 1).padStart(2, "0");
    };
    tick();
    if (!clock) clock = setInterval(() => { if (!document.hidden) tick(); }, 1000);
  }

  // ── the boot sequence: once per browser session, skippable, never in frames
  function boot() {
    let seen = true;
    try { seen = sessionStorage.getItem("manifest.hudBooted") === "1"; sessionStorage.setItem("manifest.hudBooted", "1"); } catch (e) {}
    if (seen || embedded || !hudOn() || reduced()) return;
    const veil = document.createElement("div");
    veil.className = "hud-boot";
    veil.setAttribute("aria-hidden", "true");
    const NS = "http://www.w3.org/2000/svg", svg = document.createElementNS(NS, "svg");
    svg.setAttribute("viewBox", "-60 -60 120 120");
    const shape = (tag, attrs) => { const n = document.createElementNS(NS, tag); for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v); svg.append(n); };
    shape("circle", {class: "ring", r: 52, pathLength: 1, "stroke-width": 1});
    shape("circle", {class: "ring r2", r: 44, pathLength: 1, "stroke-width": 1});
    shape("circle", {class: "ring r3", r: 30, pathLength: 1});
    shape("path", {class: "ring r2", d: "M-58 0h14M44 0h14M0 -58v14M0 44v14", pathLength: 1});
    shape("circle", {class: "core", r: 12});
    const core = document.createElement("div"); core.className = "hud-boot-core";
    const title = document.createElement("div"); title.className = "hud-boot-title"; title.textContent = "J.A.R.V.I.S.";
    const sub = document.createElement("div"); sub.className = "hud-boot-sub"; sub.textContent = "Manifest · systems online";
    core.append(svg, title, sub); veil.append(core);
    document.body.append(veil);
    decode(veil.querySelector(".hud-boot-title"), 520);
    decode(veil.querySelector(".hud-boot-sub"), 640);
    const done = () => { if (!veil.isConnected || veil.classList.contains("is-done")) return; veil.classList.add("is-done"); setTimeout(() => veil.remove(), 320); assemble(true); };
    veil.addEventListener("click", done);
    window.addEventListener("keydown", done, {once: true});
    setTimeout(done, 1150);
  }

  hudSync();
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", () => { hudSync(); boot(); });
  else { hudSync(); boot(); }
  window.addEventListener("online", readoutSync);
  window.addEventListener("offline", readoutSync);
})();
