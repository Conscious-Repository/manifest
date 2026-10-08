// Construction workbench against a REAL Go backend (web_construction_test.go):
// the page, its assets and every /api call are served by the actual server
// with the real store and access guard. Only loopback requests are allowed.
//
//   node server/testdata/construction-workbench.cjs '{"url":"http://127.0.0.1:PORT"}'
//
// Run directly (no backend), it exits with a clear message: this fixture's
// claims are about persistence and the boundary, which a stub cannot prove.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
if (!cfg || !cfg.url) {
  console.error('construction-workbench.cjs needs a real backend: run go test ./server -run TestConstructionWorkbenchBrowser');
  process.exit(2);
}
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
// a minimal real PNG (1x1) — synthetic fixture bytes
const png = Buffer.from('89504e470d0a1a0a0000000d4948445200000001000000010806000000' + '1f15c4890000000d49444154789c6360f8cfc0f01f0005000201' + 'a5f6e8c70000000049454e44ae426082', 'hex');

(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  const watch = (page) => {
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
  };
  try {
    const ctx = await browser.newContext({viewport: {width: 1440, height: 900}});
    const page = await ctx.newPage();
    watch(page);
    // 1. the property page offers Construction, from the exact property
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
    await page.getByText('CONSTRUCTION', {exact: true}).waitFor();
    await page.getByText('none yet', {exact: true}).waitFor();
    await page.getByRole('link', {name: 'Open construction →'}).click();
    await page.waitForURL(/#\/properties\/fixture-ooda-house\/construction$/);
    // 2. create a named problem bound to the exact roof work item
    await page.getByLabel('Problem title').fill('Corrugated roof to masonry wall');
    await page.getByLabel('Problem narrative').fill('SYNTHETIC FIXTURE. Roof meets a two-wythe brick wall; wall type unknown.');
    await page.getByLabel('Linked work scope').selectOption({label: 'Roof · roof'});
    await page.getByRole('button', {name: 'Create problem'}).click();
    await page.waitForURL(/\/construction\/cp-[0-9a-f]{32}$/);
    const problemId = page.url().split('/').pop();
    await page.getByRole('heading', {name: 'Corrugated roof to masonry wall'}).waitFor();
    await page.getByText(/Scope: Roof \[roof\] · resolved/).waitFor();
    await page.getByText(/not approved for construction/).first().waitFor();
    // 3. an existing condition and a synthetic photo
    await page.getByLabel('New existing condition').fill('Two nominal 100 mm masonry wythes (synthetic, unmeasured).');
    await page.getByRole('button', {name: 'Add', exact: true}).first().click();
    await page.getByText('Two nominal 100 mm masonry wythes (synthetic, unmeasured).').waitFor();
    await page.getByLabel('Choose a file').setInputFiles({name: 'synthetic-site-photo.png', mimeType: 'image/png', buffer: png});
    await page.getByLabel('Input role').selectOption('photo');
    await page.getByLabel('Input label').fill('Synthetic fixture photo — not a real site');
    await page.getByRole('button', {name: 'Upload'}).click();
    await page.getByRole('link', {name: 'synthetic-site-photo.png'}).waitFor();
    const thumbOk = await page.locator('img.cx-thumb').evaluate((img) => img.complete && img.naturalWidth === 1);
    assert.equal(thumbOk, true, 'the retained photo renders from its private URL');
    // 4. steward selection is inert: no conversation, no run
    await page.getByLabel('Steward agent').selectOption('zeck');
    await page.getByText('Zeck was explicitly selected as the real-estate specialist.').waitFor();
    await page.getByText('No research run yet.').waitFor();
    await page.screenshot({path: path.join(shots, 'p1-workbench-1440.png')});
    // 5. reload: the exact durable record returns (no browser copy involved)
    await page.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
    await page.reload();
    await page.getByRole('heading', {name: 'Corrugated roof to masonry wall'}).waitFor();
    await page.getByText('Two nominal 100 mm masonry wythes (synthetic, unmeasured).').waitFor();
    await page.getByRole('link', {name: 'synthetic-site-photo.png'}).waitFor();
    assert.equal(await page.getByLabel('Steward agent').inputValue(), 'zeck');
    // the property page now lists it
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
    await page.getByRole('link', {name: /Corrugated roof to masonry wall/}).waitFor();
    // 6. the Home pilot from TASKS › Home construction
    await page.goto(cfg.url + '/#/tasks/home-construction');
    await page.getByLabel('Problem title').waitFor();
    assert.equal(await page.getByLabel('Problem title').inputValue(), '761 N Euclid — Back Addition', 'the §12.1 pilot title is offered');
    await page.getByRole('button', {name: 'Create problem'}).click();
    await page.waitForURL(/#\/tasks\/home-construction\/cp-[0-9a-f]{32}$/);
    await page.getByRole('heading', {name: '761 N Euclid — Back Addition'}).waitFor();
    await page.getByText(/^HOME · Home/).waitFor();
    // 7. phone width: one pane at a time behind an explicit switch
    const phone = await browser.newContext({viewport: {width: 390, height: 844}, isMobile: true, hasTouch: true});
    const tab = await phone.newPage();
    watch(tab);
    await tab.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + problemId);
    await tab.getByRole('heading', {name: 'Corrugated roof to masonry wall'}).waitFor();
    for (const pane of ['Agent', 'Assembly', 'Research', 'Model']) {
      await tab.getByRole('tab', {name: pane, exact: true}).click();
      const visible = await tab.locator('.cx-pane.cx-pane-on').getAttribute('aria-label');
      assert.equal(visible, pane, 'switch shows ' + pane);
    }
    await tab.getByRole('tab', {name: 'Research', exact: true}).click();
    await tab.getByText('Two nominal 100 mm masonry wythes (synthetic, unmeasured).').waitFor();
    assert.equal(await tab.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true, 'no sideways scroll at 390px');
    await tab.screenshot({path: path.join(shots, 'p1-workbench-390.png'), fullPage: true});
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId, shots}));
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });
