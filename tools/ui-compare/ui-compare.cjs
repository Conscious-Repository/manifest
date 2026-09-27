#!/usr/bin/env node
// ui-compare — a virtual browser for UI/UX troubleshooting and side-by-side
// comparison, for the owner and for coding agents (Codex, Claude Code).
//
//   ui-compare shot    <url> [options]            capture one page
//   ui-compare compare <ours> <reference> [opts]  capture both, side by side
//   ui-compare compare <ours> <reference.png>     ours against a screenshot you
//                                                 took (logged-in apps, bot-
//                                                 checked sites)
//
// Options
//   --widths 390,768,1280,1440   viewport widths (height --height, default 900)
//   --out DIR                    output folder (default ./.ui-compare/<time>)
//   --label-a ours --label-b reference
//   --theme light|dark           prefers-color-scheme for the capture
//   --wait MS                    extra settle time after load (default 800)
//   --selector CSS               also crop this element per width
//   --before JS                  run this in the page before capturing
//   --viewport-only              viewport shots instead of full page
//
// Output (all files in --out):
//   <label>-<width>.png          full-page screenshots
//   report.html                  side-by-side report (open it in the chat's Files)
//   summary.md                   what an agent reads: paths, design-token deltas,
//                                overflow, console errors, failures
//   tokens.json                  the raw measurements
//
// Measurements are computed styles of visible elements — typefaces, sizes,
// colors, radii, spacing, content width — ranked by how much text (for type)
// or area (for backgrounds) each covers. Phone widths emulate a phone, so a
// page without a viewport meta tag lays out at desktop width, as it would on
// a real phone. They describe what rendered; they are not a pixel diff. Pages that
// need a login render their login page; say so rather than guessing.
const fs = require("node:fs"), path = require("node:path");
let chromium;
try { ({chromium} = require("playwright")); }
catch (e) { console.error("ui-compare: playwright is not installed here. Set NODE_PATH to a node_modules with playwright (metis: /home/benjamin/.cache/manifest-qa/node_modules)."); process.exit(2); }

function parseArgs(argv) {
  const out = {_: []};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--viewport-only") out.viewportOnly = true;
    else if (a.startsWith("--")) out[a.slice(2).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = argv[++i];
    else out._.push(a);
  }
  return out;
}

// measure — runs in the page: design tokens weighted by visible area.
function measure() {
  const tally = (map, key, w) => { if (!key) return; map.set(key, (map.get(key) || 0) + w); };
  const fonts = new Map(), sizes = new Map(), colors = new Map(), bgs = new Map(), radii = new Map(), pads = new Map(), weights = new Map(), lines = new Map();
  // the layout viewport: under phone emulation innerWidth grows to fit wide
  // content, which would hide exactly the overflow this should report
  const doc = document.documentElement, vw = doc.clientWidth || innerWidth;
  let textArea = 0, maxContent = 0;
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
  const overflow = [];
  for (let n = walker.currentNode; n; n = walker.nextNode()) {
    const r = n.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    const cs = getComputedStyle(n);
    if (cs.visibility === "hidden" || cs.display === "none" || Number(cs.opacity) === 0) continue;
    const area = r.width * r.height;
    // text is weighted by how much of it there is (characters), so body
    // copy outweighs one large heading
    const chars = [...n.childNodes].reduce((k, c) => k + (c.nodeType === 3 ? c.textContent.trim().length : 0), 0);
    const ownText = chars > 0;
    if (ownText) {
      const w = Math.min(chars, 4000);
      textArea += w;
      tally(fonts, cs.fontFamily.split(",")[0].replace(/["']/g, "").trim(), w);
      tally(sizes, cs.fontSize, w);
      tally(colors, cs.color, w);
      tally(weights, cs.fontWeight, w);
      tally(lines, cs.lineHeight === "normal" ? "normal" : (parseFloat(cs.lineHeight) / parseFloat(cs.fontSize)).toFixed(2), w);
      maxContent = Math.max(maxContent, Math.min(r.width, vw));
    }
    if (cs.backgroundColor && cs.backgroundColor !== "rgba(0, 0, 0, 0)") tally(bgs, cs.backgroundColor, Math.min(area, 400000));
    if (cs.borderTopLeftRadius !== "0px") tally(radii, cs.borderTopLeftRadius, 1);
    const pad = cs.paddingTop + " " + cs.paddingRight;
    if (pad !== "0px 0px" && (n.matches("button,input,textarea,select,a,[role=button]") || ownText)) tally(pads, pad, 1);
    if (r.right > vw + 1 && cs.position !== "fixed" && overflow.length < 8) overflow.push((n.id ? "#" + n.id : n.tagName.toLowerCase() + (n.className && typeof n.className === "string" ? "." + n.className.trim().split(/\s+/).slice(0, 2).join(".") : "")) + " → " + Math.round(r.right - vw) + "px");
  }
  const top = (m, k = 5) => [...m.entries()].sort((a, b) => b[1] - a[1]).slice(0, k).map(([v, w]) => ({v, share: +(w / ([...m.values()].reduce((s, x) => s + x, 0) || 1)).toFixed(3)}));
  const controls = [...document.querySelectorAll("button,input,textarea,select,[role=button]")].filter(e => { const r = e.getBoundingClientRect(); return r.width && r.height; });
  const heights = controls.map(e => Math.round(e.getBoundingClientRect().height)).sort((a, b) => a - b);
  return {
    title: document.title, url: location.href, width: vw, pageHeight: doc.scrollHeight,
    horizontalOverflow: doc.scrollWidth > vw + 1 ? doc.scrollWidth - vw : 0, overflowing: overflow,
    fonts: top(fonts), fontSizes: top(sizes, 6), fontWeights: top(weights, 4), lineHeights: top(lines, 4),
    textColors: top(colors), backgrounds: top(bgs), radii: top(radii), paddings: top(pads), maxTextWidth: Math.round(maxContent),
    controls: {count: controls.length, medianHeight: heights[Math.floor(heights.length / 2)] || 0, under24: heights.filter(h => h < 24).length, under44: heights.filter(h => h < 44).length},
  };
}

// An owner-supplied screenshot stands in for a page no headless browser may
// see. It is shown at every width; nothing is measured from pixels.
function imageSide(file, label, opt, out) {
  const name = label + path.extname(file).toLowerCase();
  fs.copyFileSync(file, path.join(out, name));
  return opt.widths.map(width => ({label, width, url: path.resolve(file), shot: name, consoleErrors: [], image: true}));
}

async function capture(browser, url, label, opt, out) {
  const results = [];
  for (const width of opt.widths) {
    const ctx = await browser.newContext({viewport: {width, height: opt.height}, colorScheme: opt.theme === "dark" ? "dark" : "light", deviceScaleFactor: 1, ...(width <= 480 ? {isMobile: true, hasTouch: true} : {})});
    const page = await ctx.newPage();
    const consoleErrors = [];
    page.on("console", m => { if (m.type() === "error") consoleErrors.push(m.text().slice(0, 300)); });
    page.on("pageerror", e => consoleErrors.push(String(e.message).slice(0, 300)));
    const shot = path.join(out, label + "-" + width + ".png");
    const r = {label, width, url, shot: path.basename(shot), consoleErrors};
    try {
      const resp = await page.goto(url, {waitUntil: "networkidle", timeout: 45000}).catch(async e => { await page.waitForLoadState("load", {timeout: 15000}).catch(() => {}); if (!page.url() || page.url() === "about:blank") throw e; return null; });
      r.status = resp ? resp.status() : null;
      await page.waitForTimeout(opt.wait);
      if (opt.before) await page.evaluate(opt.before);
      r.tokens = await page.evaluate(measure);
      const text = (await page.evaluate(() => document.body?.innerText || "").catch(() => "")).slice(0, 4000);
      if ((r.status === 403 || r.status === 503 || r.status === 429) && /verify you are human|just a moment|checking (your|if the site connection)|captcha|attention required|access denied|unusual traffic/i.test(text + " " + (await page.title().catch(() => "")))) r.blocked = "the site served a bot check (HTTP " + r.status + "); headless capture cannot pass it";
      else if (/verify you are human|checking your browser|complete the security check/i.test(text) && text.length < 600) r.blocked = "the page is a bot check; headless capture cannot pass it";
      await page.screenshot({path: shot, fullPage: !opt.viewportOnly});
      if (opt.selector) {
        const el = page.locator(opt.selector).first();
        if (await el.count()) { r.crop = label + "-" + width + "-crop.png"; await el.screenshot({path: path.join(out, r.crop)}); }
        else r.cropMissing = opt.selector;
      }
    } catch (e) { r.error = String(e.message || e).split("\n")[0]; }
    results.push(r);
    await ctx.close();
  }
  return results;
}

function tokenLine(t) {
  if (!t) return ["not measured"];
  const f = x => (x || []).map(o => o.v + " " + Math.round(o.share * 100) + "%").join(", ");
  return [
    "fonts: " + f(t.fonts), "sizes: " + f(t.fontSizes), "weights: " + f(t.fontWeights), "line-height: " + f(t.lineHeights),
    "text colors: " + f(t.textColors), "backgrounds: " + f(t.backgrounds), "radii: " + f(t.radii), "paddings: " + f(t.paddings),
    "max text width: " + t.maxTextWidth + "px", "controls: " + t.controls.count + " (median " + t.controls.medianHeight + "px, " + t.controls.under44 + " under 44px)",
    "horizontal overflow: " + (t.horizontalOverflow ? t.horizontalOverflow + "px (" + t.overflowing.join("; ") + ")" : "none"),
  ];
}

function differences(a, b) {
  const out = [];
  if (!a || !b) return out;
  const first = x => x?.[0]?.v;
  const cmp = (name, x, y) => { if (x !== y) out.push(name + ": ours " + (x ?? "—") + " · reference " + (y ?? "—")); };
  cmp("primary typeface", first(a.fonts), first(b.fonts));
  cmp("body text size", first(a.fontSizes), first(b.fontSizes));
  cmp("line height", first(a.lineHeights), first(b.lineHeights));
  cmp("text color", first(a.textColors), first(b.textColors));
  cmp("page background", first(a.backgrounds), first(b.backgrounds));
  cmp("most common radius", first(a.radii), first(b.radii));
  cmp("most common control padding", first(a.paddings), first(b.paddings));
  if (Math.abs(a.maxTextWidth - b.maxTextWidth) > 40) out.push("max text width: ours " + a.maxTextWidth + "px · reference " + b.maxTextWidth + "px");
  if (Math.abs(a.controls.medianHeight - b.controls.medianHeight) > 4) out.push("median control height: ours " + a.controls.medianHeight + "px · reference " + b.controls.medianHeight + "px");
  if (a.horizontalOverflow && !b.horizontalOverflow) out.push("ours overflows horizontally by " + a.horizontalOverflow + "px; the reference does not");
  return out;
}

function esc(s) { return String(s ?? "").replace(/[&<>"]/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"}[c])); }

function report(out, opt, sides) {
  const [A, B] = sides;
  const md = ["# UI capture " + (B ? "comparison" : "") + " — " + new Date().toISOString(), ""];
  const html = ['<!doctype html><meta charset="utf-8"><title>ui-compare</title><style>body{font:14px/1.5 system-ui,sans-serif;margin:24px;color:#222}h2{margin:32px 0 8px}.row{display:grid;grid-template-columns:' + (B ? "1fr 1fr" : "1fr") + ';gap:16px;align-items:start}figure{margin:0;border:1px solid #ddd;border-radius:8px;overflow:hidden}figcaption{padding:6px 10px;background:#f6f6f6;font-size:12px}img{display:block;max-width:100%}pre{white-space:pre-wrap;background:#f6f6f6;padding:10px;border-radius:8px;font-size:12px}.diff{color:#8a4b00}</style>'];
  html.push("<h1>" + (B ? esc(opt.labelA) + " vs " + esc(opt.labelB) : "Capture of " + esc(A[0]?.url)) + "</h1>");
  for (let i = 0; i < opt.widths.length; i++) {
    const a = A[i], b = B?.[i], w = opt.widths[i];
    md.push("## " + w + "px", "");
    html.push("<h2>" + w + "px</h2><div class='row'>");
    for (const r of [a, b].filter(Boolean)) {
      md.push("**" + r.label + "** " + r.url + (r.error ? " — FAILED: " + r.error : "") + (r.blocked ? " — BLOCKED: " + r.blocked + ". Compare against a screenshot instead: ui-compare compare <ours> reference.png" : "") + (r.image ? " — owner-supplied screenshot (not measured; read it visually)" : "") + (r.status && r.status >= 400 && !r.blocked ? " — HTTP " + r.status : ""), "");
      md.push("- screenshot: " + path.join(out, r.shot) + (r.crop ? "\n- element crop: " + path.join(out, r.crop) : "") + (r.cropMissing ? "\n- element " + r.cropMissing + " not found" : ""));
      if (!r.image && !r.blocked) for (const l of tokenLine(r.tokens)) md.push("- " + l);
      if (r.consoleErrors.length) md.push("- console errors: " + r.consoleErrors.slice(0, 5).join(" | "));
      md.push("");
      html.push("<figure><figcaption>" + esc(r.label) + " · " + esc(r.url) + (r.error ? " · FAILED: " + esc(r.error) : "") + (r.blocked ? " · BLOCKED: " + esc(r.blocked) : "") + (r.image ? " · your screenshot" : "") + "</figcaption>" + (r.error ? "" : "<img src='" + esc(r.shot) + "' alt='" + esc(r.label) + " at " + w + "px'>") + "<pre>" + esc(tokenLine(r.tokens).join("\n")) + "</pre></figure>");
    }
    html.push("</div>");
    if (b && (b.image || b.blocked || a.blocked)) {
      md.push("### Differences at " + w + "px", "", "- not measured: " + (b.image ? "the reference is a screenshot" : "a side was blocked") + "; compare the images", "");
    } else if (b) {
      const d = differences(a.tokens, b.tokens);
      md.push("### Differences at " + w + "px", "", ...(d.length ? d.map(x => "- " + x) : ["- no token-level differences found (compare the screenshots)"]), "");
      html.push("<ul class='diff'>" + (d.length ? d.map(x => "<li>" + esc(x) + "</li>").join("") : "<li>No token-level differences found; compare the screenshots.</li>") + "</ul>");
    }
  }
  md.push("", "Measurements are computed styles of visible elements, weighted by text length (type) or area (backgrounds). They are not a pixel diff; read the screenshots for layout and hierarchy. A page behind a login shows its login page.");
  fs.writeFileSync(path.join(out, "summary.md"), md.join("\n") + "\n");
  fs.writeFileSync(path.join(out, "report.html"), html.join("\n"));
  fs.writeFileSync(path.join(out, "tokens.json"), JSON.stringify(sides, null, 1));
}

(async () => {
  const args = parseArgs(process.argv.slice(2));
  const mode = args._[0];
  const urls = args._.slice(1);
  if (!["shot", "compare"].includes(mode) || urls.length !== (mode === "shot" ? 1 : 2)) {
    console.error("usage: ui-compare shot <url> [options]\n       ui-compare compare <ours> <reference> [options]\n(see the header of " + __filename + ")");
    process.exit(2);
  }
  const opt = {
    widths: String(args.widths || "390,768,1280,1440").split(",").map(Number).filter(n => n >= 200 && n <= 3840),
    height: Number(args.height) || 900, wait: args.wait === undefined ? 800 : Number(args.wait),
    theme: args.theme || "light", selector: args.selector || "", before: args.before || "", viewportOnly: !!args.viewportOnly,
    labelA: (args.labelA || (mode === "compare" ? "ours" : "page")).replace(/[^\w.-]/g, "_"), labelB: (args.labelB || "reference").replace(/[^\w.-]/g, "_"),
  };
  if (!opt.widths.length) { console.error("ui-compare: no usable --widths"); process.exit(2); }
  const out = path.resolve(args.out || path.join(".ui-compare", new Date().toISOString().replace(/[:.]/g, "-")));
  fs.mkdirSync(out, {recursive: true});
  const browser = await chromium.launch({headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || "chromium"});
  try {
    const sides = [await capture(browser, urls[0], opt.labelA, opt, out)];
    if (mode === "compare") {
      const ref = urls[1];
      if (/\.(png|jpe?g|webp)$/i.test(ref) && fs.existsSync(ref)) sides.push(imageSide(ref, opt.labelB, opt, out));
      else sides.push(await capture(browser, ref, opt.labelB, opt, out));
    }
    report(out, opt, sides);
    const failed = sides.flat().filter(r => r.error || r.blocked);
    console.log("ui-compare: wrote " + path.join(out, "summary.md") + " and report.html" + (failed.length ? " (" + failed.length + " capture(s) failed or were blocked; see summary)" : ""));
    process.exitCode = failed.length === sides.flat().length ? 1 : 0;
  } finally { await browser.close(); }
})().catch(e => { console.error("ui-compare: " + (e.message || e)); process.exit(1); });
