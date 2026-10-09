// Sections and exports in the browser against the REAL backend
// (web_construction_test.go TestConstructionSectionBrowser): the 2D section is
// the server's true plane cut of the open revision shown as an inert image;
// exports follow the accepted revision and print at a stated scale.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
if (!cfg || !cfg.url) { console.error('construction-sections.cjs needs a real backend: go test ./server -run TestConstructionSectionBrowser'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  try {
    const page = await browser.newPage({viewport: {width: 1440, height: 900}});
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
    await page.getByLabel('Problem title').fill('Corrugated roof to masonry wall');
    await page.getByRole('button', {name: 'Create problem'}).click();
    await page.waitForURL(/\/construction\/cp-[0-9a-f]{32}$/);
    await page.waitForFunction(() => window.__cxRenderer && cx.geometry);
    // the 2D section of the open revision, as an inert image
    await page.evaluate(() => { document.querySelector('.cx-more-tools').open = true; });
    await page.getByRole('button', {name: 'Show the true section drawing of this revision'}).click();
    const img = page.locator('img.cx-section-img');
    await img.waitFor();
    await page.waitForFunction(() => { const i = document.querySelector('img.cx-section-img'); return i && i.complete && i.naturalWidth > 0; });
    const rev0 = await page.evaluate(() => cx.view.revisions['assembly:' + cx.activeAssembly]);
    assert.ok((await img.getAttribute('src')).includes('revision=' + rev0), '2D section is of the open revision');
    await page.screenshot({path: path.join(shots, 'section-2d-1440.png')});
    await page.getByRole('button', {name: 'Back to the 3D model'}).click();
    // exports from the Export tab
    await page.getByRole('tab', {name: 'Export'}).click();
    await page.getByLabel('Export Section SVG').click();
    await page.getByText(/^Exported .*-section\.svg/).waitFor();
    let rec = await page.evaluate(() => cx.lastExport.record);
    assert.equal(rec.assemblyRevision, rev0);
    let bytes = await page.evaluate(async (url) => (await fetch(url)).text(), await page.evaluate(() => cx.lastExport.url));
    assert.ok(bytes.includes('width="420mm" height="297mm" viewBox="0 0 420 297"') && bytes.includes('data-scale="1:5"'), 'A3 at 1:5 in physical units');
    assert.ok(bytes.includes(rec.geometryHash), 'the SVG names its geometry hash');
    await page.getByLabel('Export Section PDF').click();
    await page.getByText(/^Exported .*-section\.pdf/).waitFor();
    const pdfHead = await page.evaluate(async (url) => { const b = new Uint8Array(await (await fetch(url)).arrayBuffer()); return String.fromCharCode(...b.slice(0, 8)); }, await page.evaluate(() => cx.lastExport.url));
    assert.equal(pdfHead, '%PDF-1.7');
    await page.getByLabel('Export Detail package').click();
    await page.getByText(/^Exported .*-detail-package\.zip/).waitFor();
    // edit, then the views and exports follow the accepted revision
    const ins = await page.evaluate(() => cxAsm().components.find((c) => c.type === 'insulation-board').id);
    await page.locator('.cx-tree-row[data-component="' + ins + '"]').click();
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).fill('150');
    await page.locator('.cx-comp').getByLabel('thickness', {exact: true}).press('Enter');
    await page.waitForFunction((r) => cx.view.revisions['assembly:' + cx.activeAssembly] !== r, rev0);
    const rev1 = await page.evaluate(() => cx.view.revisions['assembly:' + cx.activeAssembly]);
    await page.getByRole('tab', {name: 'Export'}).click();
    await page.getByLabel('Export GLB model').click();
    await page.getByText(/^Exported .*\.glb/).waitFor();
    rec = await page.evaluate(() => cx.lastExport.record);
    assert.equal(rec.assemblyRevision, rev1, 'a new export follows the accepted revision');
    assert.ok((await page.locator('.cx-export-row').count()) >= 4, 'export history lists every derivative');
    await page.getByRole('button', {name: 'Show the true section drawing of this revision'}).click();
    await page.waitForFunction((r) => { const i = document.querySelector('img.cx-section-img'); return i && i.src.includes('revision=' + r) && i.complete && i.naturalWidth > 0; }, rev1);
    assert.deepEqual(errors, []);
    assert.deepEqual(external, []);
    console.log(JSON.stringify({ok: true, rev0, rev1, lastExport: rec.name}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
