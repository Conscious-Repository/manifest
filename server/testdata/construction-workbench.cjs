// Construction workbench against a REAL Go backend (web_construction_test.go):
// the page, its assets and every /api call are served by the actual server
// with the real store and access guard. Only loopback requests are allowed.
//
//   node server/testdata/construction-workbench.cjs '{"url":"http://127.0.0.1:PORT"}'
//
// Without a backend URL it runs the UI-only checks against the recorded stub
// (construction-stub-api.cjs): layout, renderer, picking, context loss. Its
// claims about persistence and the boundary come only from the backend run.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
const shots = (cfg && cfg.shots) || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const png = Buffer.from('89504e470d0a1a0a0000000d4948445200000001000000010806000000' + '1f15c4890000000d49444154789c6360f8cfc0f01f0005000201' + 'a5f6e8c70000000049454e44ae426082', 'hex');
const evidence = {};

// pollPage evaluates an async predicate in the page until it is truthy
// (page.waitForFunction would treat the returned Promise itself as truthy).
async function pollPage(page, fn, arg, timeout = 10000) {
  const until = Date.now() + timeout;
  for (;;) {
    const v = await page.evaluate(fn, arg);
    if (v) return v;
    if (Date.now() > until) throw new Error('pollPage timed out: ' + fn.toString().slice(0, 160));
    await new Promise((r) => setTimeout(r, 200));
  }
}

async function waitModel(page) {
  await page.waitForFunction(() => window.__cxRenderer && cx.geometry && cx.renderer && cx.renderer.parts().length > 10, null, {timeout: 30000});
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
}
const api = (page, method, p, body) => page.evaluate(async ([method, p, body]) => {
  const r = await cxApi(method, cxBase(cx.subject), p, body);
  return r;
}, [method, p, body]);
const view = (page) => page.evaluate(() => cxApi('GET', cxBase(cx.subject), '/problems/' + cx.problemId));
const compOf = (v, asm, type) => v.assemblies[asm].components.find((c) => c.type === type);
const thickness = (v, asm) => compOf(v, asm, 'insulation-board').shape.params.thickness.value;

// phoneChrome measures the phone/tablet chrome around the workbench and
// returns the section tabs folded under MORE. An ellipsis renders only on a
// block container that clips its own overflowing text, so that is what is
// checked on the element holding the text (text-overflow on a flex parent
// does nothing):
// - the top-bar title stays in its cell before ⌕ and ＋, on one line, and a
//   title too long for it ends in an ellipsis with the full path in its title
//   (at 320 the 35-character problem id always is);
// - the section tab row shows whole tabs only, the lit one among them, and any
//   tab that does not fit is named by an accessible MORE control;
// - the selection crumb carries its full text as role=status name and title,
//   and its segments ellipsize instead of being clipped.
async function phoneChrome(page, at, problemId, width) {
  const g = await page.evaluate(() => {
    const R = (e) => e.getBoundingClientRect(), cs = (e) => getComputedStyle(e);
    const ellipsizes = (e) => !/^(inline|flex|inline-flex|grid|inline-grid|contents)$/.test(cs(e).display) && cs(e).overflowX === 'hidden' && cs(e).textOverflow === 'ellipsis' && cs(e).whiteSpace === 'nowrap';
    const titleEl = document.getElementById('crumbPath');
    const seg = [...titleEl.querySelectorAll('.crumb-seg')].find((e) => e.getClientRects().length);
    const search = document.querySelector('.crumb-bar .mf-search'), add = document.querySelector('.crumb-bar .mf-add');
    const nav = document.getElementById('reToggle'), nr = R(nav);
    const tabs = [...nav.querySelectorAll('.view-tab:not(.mf-tabs-more)')];
    const shown = tabs.filter((t) => t.getClientRects().length);
    const more = nav.querySelector('.mf-tabs-more');
    const crumb = document.querySelector('.cx-crumb');
    const asm = cx.view.assemblies[cx.activeAssembly], comp = asm && asm.components.find((c) => c.id === cx.selection);
    return {
      title: {right: R(titleEl).right, searchLeft: R(search).left, addLeft: R(add).left, full: titleEl.title || '', box: cs(titleEl).overflowX,
        segRight: R(seg).right, overflowing: seg.scrollWidth > seg.clientWidth + 1, ellipsizes: ellipsizes(seg), wrap: cs(seg).whiteSpace, lines: seg.getClientRects().length},
      nav: {scroll: nav.scrollWidth - nav.clientWidth, all: tabs.map((t) => t.textContent.trim()), shown: shown.map((t) => t.textContent.trim()),
        cut: shown.filter((t) => R(t).left < nr.left - 1 || R(t).right > nr.right + 1).map((t) => t.textContent.trim()),
        lit: (nav.querySelector('.view-tab.on') || {}).textContent,
        more: more && more.getClientRects().length ? {label: more.getAttribute('aria-label') || '', popup: more.getAttribute('aria-haspopup'),
          expanded: more.getAttribute('aria-expanded'), inside: R(more).left >= nr.left - 1 && R(more).right <= nr.right + 1} : null},
      crumb: {want: [cx.view.problem.title, asm && asm.name, comp ? comp.name : 'no part selected'].filter(Boolean).join(' › '),
        text: crumb.textContent.replace(/\s+/g, ' ').trim(), title: crumb.title || '', label: crumb.getAttribute('aria-label') || '', role: crumb.getAttribute('role'),
        clipped: crumb.scrollWidth > crumb.clientWidth + 1, right: R(crumb).right, vw: innerWidth,
        segs: [...crumb.querySelectorAll('.cx-crumb-seg')].map((e) => ({cut: e.scrollWidth > e.clientWidth + 1, ellipsizes: ellipsizes(e)}))},
    };
  });
  const problems = [];
  const want = (ok, msg) => { if (!ok) problems.push(msg); };
  // the top bar: title before ⌕ and ＋, one line, an ellipsis when it is long
  want(g.title.right <= g.title.searchLeft + 0.5 && g.title.right <= g.title.addLeft + 0.5 && g.title.segRight <= g.title.right + 0.5, 'the top-bar title runs into ⌕/＋');
  want(g.title.box === 'hidden' && g.title.wrap === 'nowrap' && g.title.lines === 1, 'the top-bar title wraps or paints past its cell');
  want(!g.title.overflowing || g.title.ellipsizes, 'the long top-bar title is clipped without an ellipsis');
  if (width === 320) want(g.title.overflowing, 'the 320 top bar must exercise the ellipsis (the problem id does not fit)');
  want(g.title.full.startsWith('REAL ESTATE') && g.title.full.endsWith(problemId), 'the top-bar title keeps no full path: "' + g.title.full + '"');
  // the section tabs: whole tabs only, the lit one shown, the rest named by MORE
  const folded = g.nav.all.filter((n) => !g.nav.shown.includes(n));
  want(g.nav.cut.length === 0, 'tabs cut at the edge: ' + g.nav.cut.join(', '));
  want(g.nav.scroll <= 1, 'the tab row hides ' + g.nav.scroll + 'px sideways');
  want(g.nav.shown.includes((g.nav.lit || '').trim()), 'the lit tab is not shown');
  if (folded.length) {
    want(!!g.nav.more, 'tabs ' + folded.join(', ') + ' are folded with no MORE control');
    if (g.nav.more) {
      want(g.nav.more.popup === 'dialog' && g.nav.more.expanded === 'false' && g.nav.more.inside, 'MORE is not an accessible, collapsed, in-row control');
      want(folded.every((n) => g.nav.more.label.includes(n)), 'MORE does not name the folded tabs: ' + g.nav.more.label);
    }
  } else {
    want(!g.nav.more, 'MORE shows although every tab fits');
  }
  // the selection crumb: full accessible text, ellipsized segments, never clipped
  want(g.crumb.text === g.crumb.want, 'crumb text "' + g.crumb.text + '" is not "' + g.crumb.want + '"');
  want(g.crumb.title === g.crumb.want && g.crumb.label === g.crumb.want && g.crumb.role === 'status', 'the crumb lacks its full name (role ' + g.crumb.role + ', title "' + g.crumb.title + '")');
  want(!g.crumb.clipped && g.crumb.right <= g.crumb.vw + 0.5, 'the crumb is clipped');
  want(g.crumb.segs.length >= 2 && g.crumb.segs.every((x) => !x.cut || x.ellipsizes), 'crumb segments clip without an ellipsis');
  if (width === 320) want(g.crumb.segs.some((x) => x.cut), 'the 320 crumb must exercise the ellipsis');
  assert.deepEqual(problems, [], at + ': phone chrome');
  if (folded.length) assert.ok(await page.getByRole('status', {name: g.crumb.want}).isVisible(), at + ': the crumb is reachable by its full name');
  return folded;
}

// tabsMore opens MORE: the dialog it announces lists exactly the folded tabs
// as links, focus moves into it, Escape returns it to MORE, and a folded tab
// still navigates from its home (then shows lit and whole in the row).
async function tabsMore(page, at, folded, shot) {
  const more = page.locator('#reToggle .mf-tabs-more');
  await more.click();
  const list = page.getByRole('dialog', {name: 'More REAL ESTATE sections'}).getByRole('navigation', {name: 'REAL ESTATE sections'});
  await list.waitFor();
  assert.deepEqual((await list.getByRole('link').allTextContents()).map((t) => t.trim()), folded, at + ': MORE lists the folded tabs');
  assert.equal(await more.getAttribute('aria-expanded'), 'true', at + ': MORE reports it is open');
  await page.waitForFunction(() => document.activeElement && document.activeElement.closest('.mf-tabs-sheet'));
  await page.waitForFunction(() => document.querySelector('.mf-sheet').getAnimations().every((a) => a.playState === 'finished')); // the sheet at rest, not mid fade-in
  await page.screenshot({path: shot});
  await page.keyboard.press('Escape');
  await list.waitFor({state: 'hidden'});
  assert.equal(await more.getAttribute('aria-expanded'), 'false', at + ': MORE reports it is closed');
  assert.ok(await more.evaluate((b) => document.activeElement === b), at + ': focus returns to MORE');
  const last = folded[folded.length - 1];
  await more.click();
  await list.getByRole('link', {name: last, exact: true}).click();
  const lit = await page.waitForFunction((n) => { const t = document.querySelector('#reToggle .view-tab.on'); return t && t.textContent.trim() === n && t.getClientRects().length; }, last, {timeout: 5000}).then(() => true, () => false);
  assert.ok(lit, at + ': ' + last + ', chosen from MORE, must show lit in the row');
  const r = await page.evaluate(() => { const nav = document.getElementById('reToggle'), t = nav.querySelector('.view-tab.on'), a = nav.getBoundingClientRect(), b = t.getBoundingClientRect(); return b.left >= a.left - 1 && b.right <= a.right + 1 && !document.querySelector('.mf-sheet-wrap:not([hidden])'); });
  assert.ok(r, at + ': ' + last + ' opens from MORE, lit and whole in the row, the sheet closed');
}

async function backendRun(browser) {
  const errors = [], external = [];
  const watch = (page) => {
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
  };
  const ctx = await browser.newContext({viewport: {width: 1440, height: 900}});
  const page = await ctx.newPage();
  watch(page);
  // ---- P1: the property page offers Construction, from the exact property ----
  await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
  await page.getByText('CONSTRUCTION', {exact: true}).waitFor();
  await page.getByText('none yet', {exact: true}).waitFor();
  await page.getByRole('link', {name: 'Open construction →'}).click();
  await page.waitForURL(/#\/properties\/fixture-ooda-house\/construction$/);
  await page.getByLabel('Problem title').fill('Corrugated roof to masonry wall');
  await page.getByLabel('Problem narrative').fill('SYNTHETIC FIXTURE. Roof meets a two-wythe brick wall; wall type unknown.');
  await page.getByLabel('Linked work scope').selectOption({label: 'Roof · roof'});
  await page.getByRole('button', {name: 'Create problem'}).click();
  await page.waitForURL(/\/construction\/cp-[0-9a-f]{32}$/);
  const problemId = page.url().split('/').pop();
  await page.getByRole('heading', {name: 'Corrugated roof to masonry wall'}).waitFor();
  await page.getByText(/^Part of: Roof$/).waitFor(); // the linked scope, in plain words
  await page.getByText(/not approved for construction/).first().waitFor();
  await page.getByLabel('New existing condition').fill('Two nominal 100 mm masonry wythes (synthetic, unmeasured).');
  await page.getByRole('button', {name: 'Add', exact: true}).first().click();
  await page.getByText('Two nominal 100 mm masonry wythes (synthetic, unmeasured).').waitFor();
  await page.getByLabel('Choose a file').setInputFiles({name: 'synthetic-site-photo.png', mimeType: 'image/png', buffer: png});
  await page.getByLabel('Input role').selectOption('photo');
  await page.getByLabel('Input label').fill('Synthetic fixture photo — not a real site');
  await page.getByRole('button', {name: 'Upload'}).click();
  await page.getByRole('link', {name: 'synthetic-site-photo.png'}).waitFor();
  assert.equal(await page.locator('img.cx-thumb').evaluate((img) => img.complete && img.naturalWidth === 1), true, 'retained photo renders from its private URL');
  await page.evaluate(() => { cx.tab = 'evidence'; cxp.open.runs = true; cxRender(); }); // research runs live under Details › Sources
  await page.getByText('No research run yet.').waitFor();
  await waitModel(page);
  await page.screenshot({path: path.join(shots, 'workbench-1440-default.png')});
  // ---- P3: one selection store across 3D, tree and inspector ----------------
  let v = await view(page);
  const asm = v.problem.alternatives[0];
  const apron = compOf(v, asm, 'apron-flashing').id;
  const target = await page.evaluate((id) => __cxRenderer.project(id), apron);
  const expected = await page.evaluate(([x, y]) => (__cxRenderer.pickAt(x, y) || {}).componentId, [target.x, target.y]);
  assert.ok(expected, 'a part is under the apron centre');
  await page.mouse.click(target.x, target.y);
  await page.waitForFunction((id) => cx.selection === id, expected);
  assert.equal(await page.locator('.cx-tree-row.sel').getAttribute('data-component'), expected, '3D pick selects the tree row');
  const pickedName = v.assemblies[asm].components.find((c) => c.id === expected).name;
  await page.locator('.cx-comp-name', {hasText: pickedName}).waitFor();
  const ins = compOf(v, asm, 'insulation-board').id;
  await page.locator('.cx-tree-row[data-component="' + ins + '"]').click();
  await page.locator('.cx-comp-name', {hasText: 'Above-deck insulation'}).waitFor();
  assert.equal(await page.evaluate(() => cx.selection), ins, 'tree selects the part in the shared store');
  // ---- direct numeric edit 100 → 150 through the typed command path ------------
  const hash0 = await page.evaluate(() => cx.geometry.ir.hash);
  await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).fill('150');
  await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).press('Enter');
  await page.waitForFunction((h) => cx.geometry && cx.geometry.ir.hash !== h, hash0);
  v = await view(page);
  assert.equal(thickness(v, asm), 150, 'backend holds 150 mm');
  const issueKeys = v.validation[asm].issues.filter((i) => i.status !== 'resolved').map((i) => i.ruleKey);
  assert.ok(issueKeys.includes('structure.fastener.stack-changed') && issueKeys.includes('structure.fastener.embedment'), 'fastener review raised after the insulation edit');
  const hash150 = await page.evaluate(() => cx.geometry.ir.hash);
  evidence.geometry = {hash100: hash0, hash150};
  // ---- undo / redo are new revisions ---------------------------------------------
  await page.getByRole('tab', {name: 'History'}).click();
  await page.getByRole('button', {name: 'Undo last edit'}).click();
  await page.waitForFunction((h) => cx.geometry && cx.geometry.ir.hash === h, hash0);
  v = await view(page);
  assert.equal(thickness(v, asm), 100, 'undo restores 100 mm');
  assert.equal(await page.evaluate(() => cx.geometry.ir.hash), hash0, 'undo restores the identical geometry hash');
  await page.getByRole('button', {name: 'Redo'}).click();
  await page.waitForFunction((h) => cx.geometry && cx.geometry.ir.hash === h, hash150);
  v = await view(page);
  assert.equal(thickness(v, asm), 150, 'redo returns to 150 mm');
  // ---- the handle previews tentatively, then commits --------------------------------
  await page.locator('.cx-tree-row[data-component="' + ins + '"]').click();
  const range = page.getByLabel('Drag to preview thickness (tentative until committed)');
  await range.evaluate((el) => { el.value = '180'; el.dispatchEvent(new Event('input', {bubbles: true})); });
  await page.getByText(/TENTATIVE · \d+ critical issues/).waitFor();
  assert.equal(thickness(await view(page), asm), 150, 'a tentative preview writes nothing');
  await page.locator('.cx-handle').getByRole('button', {name: 'Commit'}).click();
  await pollPage(page, async () => (await cxApi('GET', cxBase(cx.subject), '/problems/' + cx.problemId)).assemblies[cx.activeAssembly].components.find((c) => c.type === 'insulation-board').shape.params.thickness.value === 180);
  // ---- hide / isolate / show all, explode, canonical measurement ---------------------
  const sealant = compOf(v, asm, 'sealant-bead').id;
  await page.locator('.cx-tree-row[data-component="' + sealant + '"]').click();
  await page.evaluate(() => { document.querySelector('.cx-more-tools').open = true; }); // the model's second row of tools
  await page.getByRole('button', {name: 'Hide the selected part'}).click();
  assert.equal(await page.evaluate((id) => __cxRenderer.visibleParts().includes(id), sealant), false, 'hidden');
  await page.getByRole('button', {name: 'Show only the selected part'}).click();
  assert.deepEqual(await page.evaluate(() => __cxRenderer.visibleParts()), [], 'isolating a hidden part shows nothing — and says so by state');
  await page.getByRole('button', {name: 'Show every part'}).click();
  assert.equal(await page.evaluate((id) => __cxRenderer.visibleParts().includes(id), sealant), true, 'shown again');
  const wythe = v.assemblies[asm].components.find((c) => c.role === 'masonry:outer-wythe').id;
  await page.getByRole('button', {name: 'Iso view'}).click();
  // isolate the wythe so nothing occludes it, pick its centre, explode, pick
  // again: the canonical point is the same surface point
  await page.locator('.cx-tree-row[data-component="' + wythe + '"]').click();
  await page.getByRole('button', {name: 'Show only the selected part'}).click();
  // parallel rays (orthographic): an offset part is hit at the offset point
  await page.evaluate(() => __cxRenderer.setProjection('orthographic'));
  const pickCentre = () => page.evaluate((id) => { const p = __cxRenderer.project(id); return __cxRenderer.pickAt(p.x, p.y); }, wythe);
  const pick0 = await pickCentre();
  await page.getByLabel('Exploded view').evaluate((el) => { el.value = '60'; el.dispatchEvent(new Event('input', {bubbles: true})); });
  const pick1 = await pickCentre();
  const offsets = await page.evaluate(() => __cxRenderer.offsets());
  assert.ok(Math.hypot(...offsets[wythe]) > 10, 'explode moves the presentation');
  assert.ok(pick0 && pick1 && pick0.componentId === wythe && pick1.componentId === wythe, 'both picks hit the isolated wythe');
  const drift = Math.hypot(pick0.point[0] - pick1.point[0], pick0.point[1] - pick1.point[1], pick0.point[2] - pick1.point[2]);
  assert.ok(drift < 0.5, 'canonical point is unaffected by explode (moved ' + drift.toFixed(3) + ' mm)');
  evidence.explodeInvariance = {wythe, presentationOffsetMm: offsets[wythe].map((x) => Math.round(x)), canonicalDriftMm: Math.round(drift * 100) / 100};
  await page.getByLabel('Exploded view').evaluate((el) => { el.value = '0'; el.dispatchEvent(new Event('input', {bubbles: true})); });
  await page.getByRole('button', {name: 'Show every part'}).click();
  // ---- section plane, realistic mode, context loss --------------------------------
  await page.getByLabel('Section plane on/off').check();
  assert.equal(await page.evaluate(() => cx.vs.section && cx.vs.section.enabled), true);
  await page.getByRole('button', {name: /Material preview/}).click();
  await page.waitForTimeout(200);
  await page.screenshot({path: path.join(shots, 'workbench-section-realistic-1440.png')});
  const lost = await page.evaluate(() => __cxRenderer.loseContext());
  if (lost) {
    await page.getByText(/3D unavailable: the graphics context was lost/).waitFor();
    await page.getByRole('link', {name: 'Download this revision as GLB'}).waitFor();
    await page.evaluate(() => __cxRenderer.restoreContext());
    await page.waitForFunction(() => !document.querySelector('.cx-fallback'));
    evidence.contextLoss = 'fallback shown and cleared on restore';
  }
  // ---- the view persists: reload restores mode, section, selection, camera ------------
  await page.locator('.cx-tree-row[data-component="' + apron + '"]').click();
  await page.getByRole('button', {name: 'Side view'}).click();
  const camBefore = await page.evaluate(() => __cxRenderer.getCamera());
  try {
    await pollPage(page, async () => { const v = await cxApi('GET', cxBase(cx.subject), '/problems/' + cx.problemId); const w = v.views[v.problem.latestView]; return !!(w && w.mode === 'realistic' && w.section && w.section.enabled && w.selection[0] === cx.selection); });
  } catch (e) {
    const diag = await page.evaluate(async () => {
      const v = await cxApi('GET', cxBase(cx.subject), '/problems/' + cx.problemId);
      let err = null;
      try {
        const id = cxViewID();
        await cxApi('POST', cxBase(cx.subject), '/problems/' + cx.problemId + '/commands', {schemaVersion: 1, requestId: cxRequestId(), problemId: cx.problemId,
          operations: [{op: 'SaveView', viewId: id, expectedRevision: (cx.view.revisions || {})['view:' + id] || '', view: cxCurrentViewState()}]});
      } catch (x) { err = {status: x.status, message: x.message, problems: x.problems, current: x.current}; }
      return {latest: v.problem.latestView, views: Object.keys(v.views || {}), viewID: cx.viewID, revs: Object.keys(cx.view.revisions).filter((k) => k.startsWith('view:')), vs: cx.vs, err};
    });
    throw new Error('view persistence: ' + JSON.stringify(diag).slice(0, 3000));
  }
  await page.evaluate(() => { localStorage.removeItem('cx.layout.v1'); sessionStorage.clear(); });
  await page.reload();
  await waitModel(page);
  const restored = await page.evaluate(async () => { const fresh = await cxApi('GET', cxBase(cx.subject), '/problems/' + cx.problemId); return {pid: cx.problemId, url: location.hash, sel: cx.selection, latest: cx.view.problem.latestView || null, freshLatest: fresh.problem.latestView || null, freshGen: fresh.generation, gen: cx.view.generation, viewID: cx.viewID, active: cx.activeAssembly}; });
  assert.equal(restored.sel, apron, 'selection restored: ' + JSON.stringify(restored));
  assert.equal(await page.evaluate(() => cx.vs.mode), 'realistic', 'mode restored');
  assert.equal(await page.getByLabel('Section plane on/off').isChecked(), true, 'section restored');
  const camAfter = await page.evaluate(() => __cxRenderer.getCamera());
  assert.ok(Math.hypot(...camAfter.position.map((x, i) => x - camBefore.position[i])) < 50, 'camera restored (' + JSON.stringify(camAfter.position) + ')');
  assert.equal(camAfter.projection, 'orthographic', 'projection restored');
  // ---- keyboard divider resize persists -------------------------------------------
  const before = await page.evaluate(() => cx.layout.a);
  await page.getByRole('separator', {name: 'Resize agent pane'}).focus();
  for (let i = 0; i < 3; i++) await page.keyboard.press('ArrowRight');
  assert.equal(await page.evaluate(() => cx.layout.a), before + 36, 'arrow keys resize the agent pane');
  assert.equal(JSON.parse(await page.evaluate(() => localStorage.getItem('cx.layout.v1'))).a, before + 36, 'width persisted');
  // ---- measured performance on this machine (software WebGL) ------------------------
  evidence.perf = await page.evaluate(() => ({...__cxRenderer.stats(), heapMB: performance.memory ? Math.round(performance.memory.usedJSHeapSize / 1048576) : null}));
  // ---- stale response from another problem is discarded -----------------------------
  const other = await api(page, 'POST', '/problems', {schemaVersion: 1, requestId: 'cx-race-' + Date.now(), title: 'Second problem (race fixture)', template: 'roof-masonry-junction'});
  const otherId = other.view.problem.id;
  let delayed = false;
  await page.route('**/problems/' + problemId + '/assemblies/*/geometry*', async (route) => { delayed = true; await new Promise((r) => setTimeout(r, 1500)); route.continue(); });
  await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
  await page.getByText('CONSTRUCTION', {exact: true}).waitFor();
  await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
  await page.waitForFunction(() => cx.view && cx.view.problem, null, {timeout: 10000});
  await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + otherId);
  await page.waitForFunction((id) => cx.problemId === id && cx.geometry && cx.geometry.assemblyId === cx.activeAssembly, otherId);
  await page.waitForTimeout(1800);
  assert.equal(await page.evaluate(() => cx.problemId), otherId);
  assert.equal(await page.evaluate(() => cx.geometry.assemblyId), other.view.problem.alternatives[0], 'the late geometry of the first problem never replaced the second');
  assert.ok(delayed, 'the first problem\'s geometry request was delayed');
  evidence.staleDiscard = 'a 1.5 s late geometry response for the previous problem was discarded';
  await page.unroute('**/problems/' + problemId + '/assemblies/*/geometry*');
  // ---- five widths × two themes: usable, no sideways scroll ---------------------------
  for (const theme of ['default', 'jarvis']) {
    for (const width of [320, 390, 768, 1100, 1440]) {
      const c2 = await browser.newContext({viewport: {width, height: width < 800 ? 780 : 900}, isMobile: width < 800, hasTouch: width < 800});
      const tab = await c2.newPage();
      watch(tab);
      await tab.addInitScript((t) => { if (t === 'jarvis') localStorage.setItem('manifest.theme', 'jarvis'); }, theme);
      await tab.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
      await tab.getByRole('heading', {name: 'Corrugated roof to masonry wall'}).waitFor();
      await tab.waitForFunction(() => !document.querySelector('.hud-boot'), null, {timeout: 5000}); // the once-per-session Jarvis boot veil
      if (width <= 860) {
        await tab.getByRole('tab', {name: 'Model', exact: true}).click();
        await waitModel(tab);
        // the part inspector rides under the model on a phone
        await tab.getByLabel('Pitch').waitFor();
        assert.equal(await tab.getByLabel('Pitch').isEditable(), true, 'parameters are editable at ' + width);
        await tab.getByRole('tab', {name: 'Plan', exact: true}).click();
        await tab.getByText(/things? to check before building/).first().waitFor();
        await tab.getByRole('tab', {name: 'Details', exact: true}).click();
        await tab.getByRole('tab', {name: 'History', exact: true}).waitFor();
        await tab.getByRole('tab', {name: 'Model', exact: true}).click(); // the part breadcrumb belongs to the model
        assert.ok(await tab.locator('.cx-crumb').textContent(), 'selection breadcrumb present');
      } else {
        await waitModel(tab);
        if (width < 1280) {
          await tab.getByRole('tab', {name: 'Plan', exact: true}).click();
          await tab.getByLabel('Message to Alfred').waitFor();
          await tab.getByRole('tab', {name: 'Details', exact: true}).click();
          await tab.getByLabel('Pitch').waitFor(); // the part inspector shares the column with Details
        }
      }
      const overflow = await tab.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      assert.ok(overflow <= 1, 'no sideways scroll at ' + width + ' (' + theme + '): ' + overflow);
      const folded = width <= 860 ? await phoneChrome(tab, width + ' (' + theme + ')', problemId, width) : [];
      await tab.screenshot({path: path.join(shots, 'workbench-' + width + '-' + theme + '.png')});
      if (folded.length) await tabsMore(tab, width + ' (' + theme + ')', folded, path.join(shots, 'workbench-' + width + '-' + theme + '-more.png'));
      await c2.close();
    }
  }
  // keyboard-reduced height
  const short = await browser.newContext({viewport: {width: 1100, height: 600}});
  const st = await short.newPage(); watch(st);
  await st.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
  await waitModel(st);
  const canvasH = await st.evaluate(() => document.querySelector('.cx-canvas-host').getBoundingClientRect().height);
  assert.ok(canvasH >= 200, 'model stays usable at 600px height: ' + canvasH);
  await short.close();
  // ---- the Home pilot from TASKS › Home construction -----------------------------------
  await page.goto(cfg.url + '/#/tasks/home-construction');
  await page.getByLabel('Problem title').waitFor();
  assert.equal(await page.getByLabel('Problem title').inputValue(), '761 N Euclid — Back Addition', 'the §12.1 pilot title is offered');
  await page.getByRole('button', {name: 'Create problem'}).click();
  await page.waitForURL(/#\/tasks\/home-construction\/cp-[0-9a-f]{32}$/);
  await page.getByRole('heading', {name: '761 N Euclid — Back Addition'}).waitFor();
  await page.getByText(/^HOME · Home/).waitFor();
  await waitModel(page);
  assert.deepEqual(errors, [], 'page errors');
  assert.deepEqual(external, [], 'requests outside the backend');
  return {problemId, evidence};
}

async function stubRun(browser) {
  const stub = require('./construction-stub-api.cjs');
  const server = await stub.start();
  const url = 'http://127.0.0.1:' + server.address().port;
  const errors = [];
  try {
    for (const width of [390, 1440]) {
      const ctx = await browser.newContext({viewport: {width, height: 860}, isMobile: width < 800, hasTouch: width < 800});
      const page = await ctx.newPage();
      page.on('pageerror', (e) => errors.push(e.message));
      await page.goto(url + '/#/properties/' + stub.slug + '/construction/' + stub.problemId);
      await page.getByRole('heading', {name: stub.title}).waitFor();
      if (width <= 860) await page.getByRole('tab', {name: 'Model', exact: true}).click();
      await waitModel(page);
      const id = stub.apronId;
      const p = await page.evaluate((id) => __cxRenderer.project(id), id);
      const hit = await page.evaluate(([x, y]) => __cxRenderer.pickAt(x, y), [p.x, p.y]);
      assert.ok(hit && hit.componentId, 'semantic pick in stub mode');
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'no sideways scroll');
      await page.screenshot({path: path.join(shots, 'stub-' + width + '.png')});
      await ctx.close();
    }
  } finally { server.close(); }
  assert.deepEqual(errors, []);
  return {mode: 'stub', note: 'UI-only: persistence and the access boundary are proven only by the backend run'};
}

(async () => {
  const browser = await chromium.launch({headless: true});
  try {
    const out = cfg && cfg.url ? await backendRun(browser) : await stubRun(browser);
    console.log(JSON.stringify({ok: true, ...out, shots}));
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });
