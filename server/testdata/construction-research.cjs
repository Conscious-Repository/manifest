// Research runs and evidence in the workbench against a REAL Go backend
// (web_construction_test.go, TestConstructionResearchBrowser): the synthetic
// fixture source adapter is wired, every /api call goes to the actual
// server with the real store and guard, and only loopback requests are
// allowed. The Go side checks the store afterwards.
//
//   node server/testdata/construction-research.cjs '{"url":"http://127.0.0.1:PORT"}'
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = JSON.parse(process.argv[2] || '{}');
if (!cfg.url) { console.error('construction-research.cjs needs the real backend URL'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const evidence = {};

async function pollPage(page, fn, arg, timeout = 20000) {
  const until = Date.now() + timeout;
  for (;;) {
    const v = await page.evaluate(fn, arg);
    if (v) return v;
    if (Date.now() > until) throw new Error('pollPage timed out: ' + fn.toString().slice(0, 200));
    await new Promise((r) => setTimeout(r, 150));
  }
}
const latest = (page) => page.evaluate(() => { const r = cxLatestRun(); return r ? {id: r.id, state: r.state, epoch: r.epoch, stages: r.stages.map((s) => s.state)} : null; });
const tab = (page, name) => page.getByRole('tab', {name, exact: true}).click();

(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  try {
    const ctx = await browser.newContext({viewport: {width: 1440, height: 900}});
    const page = await ctx.newPage();
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
    await page.getByLabel('Problem title').waitFor();
    const created = await page.evaluate(async () => cxApi('POST', cxBase({kind: 'property', id: 'fixture-ooda-house'}), '/problems',
      {schemaVersion: 1, requestId: cxRequestId(), title: 'Research fixture — corrugated roof to masonry', template: 'roof-masonry-junction'}));
    const problemId = created.view.problem.id;
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
    await page.getByRole('heading', {name: 'Research fixture — corrugated roof to masonry'}).waitFor();
    // observed capabilities are shown, not assumed
    await page.evaluate(() => { cx.pane = 'research'; cx.tab = 'evidence'; cxp.open.runs = true; cxRender(); }); // research runs live under Details › Sources
    await page.getByText(/synthetic fixture sources \(test only\)/).first().waitFor();
    await page.getByText(/PDF\/OCR page extraction is not bundled/).first().waitFor();
    // ---- start: progress comes from the durable run record ----
    await page.getByRole('button', {name: 'Start research', exact: true}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'running'; });
    await page.locator('.cx-stage-running').first().waitFor();
    await page.screenshot({path: path.join(shots, 'research-running-1440.png')});
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'completed'; }, null, 60000);
    await pollPage(page, () => document.querySelectorAll('.cx-stage-completed').length === 8);
    const run1 = await latest(page);
    evidence.run1 = run1;
    await page.getByText(/3 alternatives/).first().waitFor();
    // ---- Research tab: every stage's retained result ----
    await tab(page, 'Sources');
    await page.getByText(/Acquisition · fixture/).waitFor();
    for (const outcome of ['not-found · source', 'rate-limited · rate-limit', 'timeout · timeout', 'blocked · source', 'extraction-unavailable · capability']) {
      await page.locator('.cx-acq td', {hasText: outcome}).first().waitFor();
    }
    await page.locator('.cx-rejected li', {hasText: 'quote not found on page 1'}).waitFor();
    await page.locator('.cx-rejected li', {hasText: 'page 9 does not exist'}).waitFor();
    await page.locator('.cx-requests li', {hasText: 'Supply the excerpt and page'}).waitFor();
    await page.locator('.cx-notused li', {hasText: 'lead only'}).first().waitFor();
    assert.equal(await page.locator('.cx-syn-alts li').count(), 3, 'three conditional alternatives');
    await page.screenshot({path: path.join(shots, 'research-tab-1440.png')});
    // ---- open the surface-counterflashing research alternative ----
    const row = page.locator('.cx-pane-a .cx-approach', {hasText: 'Research — apron flashing + surface-held counterflashing'});
    await row.getByRole('button', {name: 'Show model'}).click();
    await pollPage(page, () => cxAsm() && cxAsm().name.startsWith('Research — apron flashing + surface'));
    // ---- Evidence tab: fictional sources, injection warning, contradiction, path ----
    await tab(page, 'Sources');
    await page.getByText(/FICTIONAL FIXTURE/).first().waitFor();
    await page.locator('.cx-src-warn', {hasText: 'never followed'}).first().waitFor();
    await page.locator('.cx-claim-contradicted').first().waitFor();
    const linkedEv = await page.evaluate(() => cxAsm().evidenceLinks.find((l) => l.relation === 'supports').evidenceId);
    await page.locator('.cx-ev-row[data-evidence="' + linkedEv + '"]').getByRole('button', {name: 'Show path'}).click();
    const pathRow = page.getByLabel('Evidence path').first();
    await pathRow.waitFor();
    const nodes = await pathRow.locator('.cx-path-node').allTextContents();
    assert.ok(nodes.length === 5, 'Source → Evidence → Claim → Junction → Assembly: ' + JSON.stringify(nodes));
    evidence.path = nodes;
    // link a passage to a selected part, then follow the path back to the part
    const asm = await page.evaluate(() => cxAsm().id);
    const insID = await page.evaluate(() => cxAsm().components.find((c) => c.type === 'insulation-board').id);
    await page.evaluate((id) => cxSelect(id), insID);
    await tab(page, 'Sources');
    const free = await page.evaluate(() => { const linked = new Set(cxAsm().evidenceLinks.map((l) => l.evidenceId)); return cx.view.evidence.evidence.find((e) => !linked.has(e.id)).id; });
    const expand = async (evd) => {
      const det = page.locator('details.cx-src', {has: page.locator('.cx-ev-row[data-evidence="' + evd + '"]')});
      if (!(await det.evaluate((d) => d.open))) await det.locator('summary').click();
    };
    await expand(free);
    await page.locator('.cx-ev-row[data-evidence="' + free + '"]').getByRole('button', {name: 'Link to Above-deck insulation'}).click();
    await pollPage(page, ([a, id, e]) => cx.view.assemblies[a].evidenceLinks.some((l) => l.target === id && l.evidenceId === e), [asm, insID, free]);
    await page.evaluate(() => cxSelect(''));
    await tab(page, 'Sources');
    await expand(free);
    await page.locator('.cx-ev-row[data-evidence="' + free + '"]').getByRole('button', {name: 'Show path'}).click();
    await page.getByRole('button', {name: 'Above-deck insulation'}).first().click();
    await pollPage(page, (id) => cx.selection === id, insID);
    evidence.selectionSync = 'evidence path → part selection';
    // ---- owner source + passage: verified against the retained page ----
    await tab(page, 'About');
    await page.getByLabel('Choose a file').setInputFiles({name: 'owner-note.txt', mimeType: 'text/plain',
      buffer: Buffer.from('SYNTHETIC FIXTURE owner note, page 1.\fPage 2: the apron upstand is covered by a counterflashing (fictional figure).')});
    await page.getByLabel('Input role').selectOption('document');
    await page.getByRole('button', {name: 'Upload'}).click();
    await page.getByRole('link', {name: 'owner-note.txt'}).waitFor();
    await tab(page, 'Sources');
    await page.getByLabel('Source title').fill('Owner note (synthetic)');
    await page.getByLabel('Retained document').selectOption({index: 1});
    await page.getByRole('button', {name: 'Add source'}).click();
    await page.locator('.cx-src-title', {hasText: 'Owner note (synthetic)'}).waitFor();
    const srcID = await page.evaluate(() => cx.view.evidence.sources.find((s) => s.title === 'Owner note (synthetic)').id);
    await page.getByLabel('Passage source').selectOption(srcID);
    await page.getByLabel('Page', {exact: true}).fill('2');
    await page.getByLabel('Quote (verbatim)').fill('the apron upstand is covered by a counterflashing');
    await page.getByLabel('Claim it supports').fill('The apron upstand is covered by a counterflashing (owner note).');
    await page.getByRole('button', {name: 'Add passage'}).click();
    await pollPage(page, () => cx.view.evidence.evidence.some((e) => e.quote === 'the apron upstand is covered by a counterflashing' && e.verification === 'verified'));
    await page.getByLabel('Passage source').selectOption(srcID);
    await page.getByLabel('Page', {exact: true}).fill('1');
    await page.getByLabel('Quote (verbatim)').fill('the apron upstand is covered by a counterflashing');
    await page.getByLabel('Claim it supports').fill('Wrong page.');
    await page.getByRole('button', {name: 'Add passage'}).click();
    await page.getByRole('alert').filter({hasText: 'nothing was added'}).waitFor();
    // ---- P6 catalog: generic families with unknowns, a sourced product, a previewed substitution ----
    await tab(page, 'Materials');
    await page.getByText('No products. Generic materials only until a sourced product is added.').waitFor();
    await page.getByText(/Materials \(16 · generic families with explicit unknowns\)/).waitFor();
    await page.getByText('Add a sourced product').click();
    await page.getByLabel('Product manufacturer').fill('Placeholder Insulation Co (fictional)');
    await page.getByLabel('Product model').fill('Placeholder Board 120 (fictional)');
    await page.getByLabel('Product family').selectOption('insulation');
    await page.getByLabel('Product geography (where it is sold/approved)').fill('Fictional Region (synthetic)');
    await page.getByLabel('Product fact (as the document states it)').fill('120 mm rigid board (no document retained)');
    await page.getByLabel('Product thickness (optional)').fill('120');
    await page.getByRole('button', {name: 'Add product'}).click();
    await page.locator('.cx-prod', {hasText: 'Placeholder Board 120 (fictional)'}).waitFor();
    await page.locator('.cx-prod .cx-claim-unverified', {hasText: 'no evidence: unverified'}).waitFor();
    const prodID = await page.evaluate(() => cx.view.catalog.products.find((p) => p.model === 'Placeholder Board 120 (fictional)').id);
    await page.evaluate((id) => cxSelect(id), insID);
    const hashBefore = await page.evaluate(() => cx.view.assemblies[cx.activeAssembly].modelHash);
    await page.locator('.cx-comp').getByLabel('Product', {exact: true}).selectOption(prodID);
    const panel = page.getByRole('region', {name: 'Substitution preview'});
    await panel.waitFor();
    await panel.getByText(/thickness: 100 → 120 mm \(applies to this part\)/).waitFor();
    await panel.getByText(/Critical \d+ → \d+/).waitFor();
    assert.equal(await page.evaluate(() => cx.view.assemblies[cx.activeAssembly].modelHash), hashBefore, 'a preview writes nothing');
    await page.screenshot({path: path.join(shots, 'catalog-substitution-preview-1440.png')});
    await panel.getByRole('button', {name: 'Apply substitution'}).click();
    await pollPage(page, ([id, h]) => { const a = cx.view.assemblies[cx.activeAssembly]; const c = a.components.find((x) => x.id === id); return a.modelHash !== h && c.product && c.shape.params.thickness.value === 120; }, [insID, hashBefore]);
    await pollPage(page, () => (cx.view.validation[cx.activeAssembly].issues || []).some((i) => i.ruleKey === 'product.dimension-unverified' && i.status !== 'resolved'));
    await tab(page, 'Materials');
    await page.locator('.cx-prod[data-product="' + prodID + '"]').getByRole('button', {name: 'Mark stale'}).click();
    await pollPage(page, () => (cx.view.validation[cx.activeAssembly].issues || []).some((i) => i.ruleKey === 'product.stale' && i.status !== 'resolved'));
    await page.screenshot({path: path.join(shots, 'catalog-tab-1440.png')});
    evidence.catalog = {product: prodID, substitution: 'previewed then applied; thickness 120 mm on the same component; stale flagged'};
    // ---- P7 decisions: propose, owner accepts the exact revision, an edit stales it, compare, restore ----
    await tab(page, 'History');
    await page.getByText(/^Propose a decision for/).click();
    await page.getByLabel('Decision proposal').fill('Working detail for the synthetic fixture, conditional on its open issues.');
    await page.getByRole('button', {name: 'Propose decision'}).click();
    const decRow = page.locator('.cx-dec-proposed').first();
    await decRow.waitFor();
    await decRow.getByText(/unresolved issues kept with this decision/).waitFor();
    await decRow.getByRole('button', {name: 'Accept for project'}).click();
    await page.locator('.cx-dec-accepted-for-project').first().waitFor();
    await page.getByText(/^Selected for the project:/).waitFor();
    const accepted = await page.evaluate(() => ({rev: cx.view.problem.selectedAssembly.revision, hash: cx.view.assemblies[cx.activeAssembly].modelHash}));
    await page.evaluate((id) => cxSelect(id), insID);
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).fill('130');
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).press('Enter');
    await pollPage(page, (h) => cx.view.assemblies[cx.activeAssembly].modelHash !== h, accepted.hash);
    await tab(page, 'History');
    await page.locator('.cx-dec-stale').first().waitFor();
    await page.getByText(/the selection stays on this revision/).waitFor();
    assert.equal(await page.evaluate(() => cx.view.problem.selectedAssembly.revision), accepted.rev, 'the selection never follows the head');
    await page.getByLabel('Compare from').selectOption({index: (await page.getByLabel('Compare from').locator('option').count()) - 1});
    await page.getByLabel('Compare to').selectOption(await page.evaluate(() => cx.activeAssembly));
    await page.getByRole('button', {name: 'Compare', exact: true}).click();
    await page.locator('.cx-cmp-out .cx-diff-line', {hasText: 'thickness'}).first().waitFor();
    await page.screenshot({path: path.join(shots, 'decisions-compare-1440.png')});
    await tab(page, 'History');
    await page.locator('.cx-asm-rev').first().waitFor();
    await page.locator('.cx-asm-rev').nth(1).getByRole('button', {name: /^Restore revision/}).click();
    await pollPage(page, (h) => cx.view.assemblies[cx.activeAssembly].modelHash === h, accepted.hash);
    evidence.decisions = {accepted: accepted.rev.slice(0, 12), stale: 'edit staled the acceptance; selection pinned; compare shows thickness; restore returned the accepted geometry'};
    // ---- cancel a running research run, then resume it ----
    await tab(page, 'Sources');
    await page.getByRole('button', {name: 'Start new research'}).click();
    await page.getByRole('button', {name: 'Cancel research'}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'cancelled'; }, null, 30000);
    await page.getByRole('button', {name: 'Resume research'}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'completed'; }, null, 60000);
    const run2 = await latest(page);
    assert.equal(run2.epoch, 2, 'resume started a new epoch');
    evidence.run2 = run2;
    // ---- reload: the same durable runs and evidence come back; refresh starts nothing ----
    await page.reload();
    await page.getByRole('heading', {name: 'Research fixture — corrugated roof to masonry'}).waitFor();
    const after = await latest(page);
    assert.deepEqual(after, run2, 'reload shows the same durable run');
    // ---- phone width: the research tab is reachable and nothing scrolls sideways ----
    const phone = await browser.newContext({viewport: {width: 390, height: 800}, isMobile: true, hasTouch: true});
    const p2 = await phone.newPage();
    p2.on('pageerror', (e) => errors.push(e.message));
    await p2.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
    await p2.getByRole('heading', {name: 'Research fixture — corrugated roof to masonry'}).waitFor();
    await p2.getByRole('tab', {name: 'Details', exact: true}).first().click();
    await p2.getByRole('tab', {name: 'Sources', exact: true}).click();
    await p2.getByText(/FICTIONAL FIXTURE/).first().waitFor();
    const overflow = await p2.evaluate(() => document.documentElement.scrollWidth - innerWidth);
    assert.ok(overflow <= 1, 'no sideways scroll at 390: ' + overflow);
    await p2.screenshot({path: path.join(shots, 'research-evidence-390.png')});
    await phone.close();
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId, evidence, shots}));
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });
