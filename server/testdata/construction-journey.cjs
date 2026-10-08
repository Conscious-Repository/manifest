// The integrated Construction journey in a browser against a REAL Go backend
// (TestConstructionJourneyBrowser): TASKS › Home construction → the 761
// pilot (synthetic stand-in data only) → stand-in drawing → junction and
// insulation edits → research cancelled mid-run and resumed → open a
// research alternative → decision proposed and accepted (not construction
// approval) → detail package and the private recovery bundle downloaded →
// reload shows the same durable state; phone width stays usable. Only
// loopback requests are allowed; the Go side checks the store afterwards.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = JSON.parse(process.argv[2] || '{}');
if (!cfg.url) { console.error('construction-journey.cjs needs the real backend URL'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const evidence = {};
async function pollPage(page, fn, arg, timeout = 30000) {
  const until = Date.now() + timeout;
  for (;;) {
    const v = await page.evaluate(fn, arg);
    if (v) return v;
    if (Date.now() > until) throw new Error('pollPage timed out: ' + fn.toString().slice(0, 200));
    await new Promise((r) => setTimeout(r, 150));
  }
}
const tab = (page, name) => page.getByRole('tab', {name, exact: true}).click();

(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  try {
    const ctx = await browser.newContext({viewport: {width: 1440, height: 900}, acceptDownloads: true});
    const page = await ctx.newPage();
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
    // ---- the pilot from TASKS › Home construction ----
    await page.goto(cfg.url + '/#/tasks/home-construction');
    await page.getByLabel('Problem title').waitFor();
    assert.equal(await page.getByLabel('Problem title').inputValue(), '761 N Euclid — Back Addition');
    await page.getByLabel('Problem narrative').fill('SYNTHETIC journey: back addition roof meets old brick. No real site data; the historical drawing is not used.');
    await page.getByRole('button', {name: 'Create problem'}).click();
    await page.waitForURL(/#\/tasks\/home-construction\/cp-[0-9a-f]{32}$/);
    const problemId = page.url().split('/').pop();
    await page.getByRole('heading', {name: '761 N Euclid — Back Addition'}).waitFor();
    await page.getByText(/not approved for construction/).first().waitFor();
    await page.waitForFunction(() => window.__cxRenderer && cx.geometry && cx.renderer && cx.renderer.parts().length > 10, null, {timeout: 30000});
    // ---- a synthetic stand-in for the historical drawing ----
    await page.getByLabel('Choose a file').setInputFiles({name: 'historical-drawing-standin.pdf', mimeType: 'application/pdf',
      buffer: Buffer.from('%PDF-1.4\n% SYNTHETIC stand-in for the historical drawing; not the real document\n%%EOF\n')});
    await page.getByLabel('Input role').selectOption('drawing');
    await page.getByLabel('Input label').fill('SYNTHETIC stand-in — not the historical drawing, not field-verified');
    await page.getByRole('button', {name: 'Upload'}).click();
    await page.getByRole('link', {name: 'historical-drawing-standin.pdf'}).waitFor();
    // ---- junction and insulation through the typed command path ----
    const base = await page.evaluate(() => cx.activeAssembly);
    const apron = await page.evaluate(() => cxNewId('cmp'));
    await page.evaluate(([a, id]) => cxCommand([{op: 'SetJunctionStrategy', orientation: 'headwall', strategy: 'apron-surface-counterflashing', newComponentIds: {apron: id}}], {assembly: a}), [base, apron]);
    await pollPage(page, (a) => cx.view.assemblies[a].junction.strategy === 'apron-surface-counterflashing', base);
    const ins = await page.evaluate(() => cxAsm().components.find((c) => c.type === 'insulation-board').id);
    await page.evaluate((id) => cxSelect(id), ins);
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).fill('120');
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).press('Enter');
    await pollPage(page, ([a, id]) => cx.view.assemblies[a].components.find((c) => c.id === id).shape.params.thickness.value === 120, [base, ins]);
    // ---- research: cancel mid-run, resume ----
    await page.getByRole('button', {name: 'Start research', exact: true}).click();
    await page.getByRole('button', {name: 'Cancel research'}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'cancelled'; });
    await page.getByRole('button', {name: 'Resume research'}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'completed'; }, null, 60000);
    evidence.run = await page.evaluate(() => { const r = cxLatestRun(); return {state: r.state, epoch: r.epoch, alternatives: r.publication.assemblies.length}; });
    // ---- open a research alternative; decide ----
    await tab(page, 'Alternatives');
    await page.locator('table.cx-compare tr', {hasText: 'Research — apron flashing + surface-held counterflashing'}).getByRole('button', {name: 'Open'}).click();
    await tab(page, 'Decisions');
    await page.getByText(/^Propose a decision for/).click();
    await page.getByLabel('Decision proposal').fill('Working detail for review; every open issue stays open.');
    await page.getByRole('button', {name: 'Propose decision'}).click();
    await page.locator('.cx-dec-proposed').first().getByRole('button', {name: 'Accept for project'}).click();
    await page.getByText(/^Selected for the project:/).waitFor();
    await page.screenshot({path: path.join(shots, 'journey-decision-1440.png')});
    // ---- exports: the detail package and the private recovery bundle ----
    await tab(page, 'Export');
    await page.getByRole('button', {name: 'Export Detail package'}).click();
    await page.getByText(/^Exported .*detail-package\.zip/).waitFor();
    const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', {name: 'Download private recovery bundle'}).click()]);
    const file = path.join(shots, 'journey-recovery.zip');
    await download.saveAs(file);
    const zip = fs.readFileSync(file);
    assert.equal(zip.subarray(0, 2).toString(), 'PK', 'the recovery bundle is a zip');
    evidence.bundleBytes = zip.length;
    // ---- reload: the same durable state ----
    const before = await page.evaluate(() => ({gen: cx.view.generation, sel: cx.view.problem.selectedAssembly, run: cxLatestRun().id}));
    await page.reload();
    await page.getByRole('heading', {name: '761 N Euclid — Back Addition'}).waitFor();
    const after = await page.evaluate(() => ({gen: cx.view.generation, sel: cx.view.problem.selectedAssembly, run: cxLatestRun().id}));
    assert.deepEqual(after, before, 'reload shows the same durable problem');
    // ---- phone width ----
    const phone = await browser.newContext({viewport: {width: 390, height: 800}, isMobile: true, hasTouch: true});
    const p2 = await phone.newPage();
    p2.on('pageerror', (e) => errors.push(e.message));
    await p2.goto(cfg.url + '/#/tasks/home-construction/' + problemId);
    await p2.getByRole('heading', {name: '761 N Euclid — Back Addition'}).waitFor();
    await p2.waitForFunction(() => !document.querySelector('.hud-boot'), null, {timeout: 5000});
    await p2.getByRole('tab', {name: 'Research', exact: true}).first().click();
    await p2.getByRole('tab', {name: 'Decisions', exact: true}).click();
    await p2.getByText(/^Selected for the project:/).waitFor();
    const overflow = await p2.evaluate(() => document.documentElement.scrollWidth - innerWidth);
    assert.ok(overflow <= 1, 'no sideways scroll at 390: ' + overflow);
    await p2.screenshot({path: path.join(shots, 'journey-decisions-390.png')});
    await phone.close();
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId, evidence, shots}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
