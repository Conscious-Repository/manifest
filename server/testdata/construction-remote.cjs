// Construction's boundary in the browser against the REAL backend
// (web_construction_test.go TestConstructionRemoteRefusalBrowser). A context
// whose requests arrive through a public CDN/tunnel (Cf-Connecting-Ip) is
// refused and every construction entry explains why. The owner's tailnet
// path (`tailscale serve`: X-Forwarded-For + Tailscale-* headers) and a
// plain local context both open the same problem, with only the fine-print
// "not approved for construction" note.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
if (!cfg || !cfg.url || !cfg.problemId) { console.error('construction-remote.cjs needs a real backend: go test ./server -run TestConstructionRemoteRefusalBrowser'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const EXPLAIN = /Construction answers this computer and your tailnet .*This request came from somewhere else, so it was refused\./;
const FINE = /not approved for construction/;
(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  const watch = (page) => {
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
  };
  try {
    // a public tunnel: Manifest itself loads; construction is refused and explained
    const relayed = await browser.newContext({viewport: {width: 1440, height: 900}, extraHTTPHeaders: {'Cf-Connecting-Ip': '203.0.113.9'}});
    const page = await relayed.newPage();
    watch(page);
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
    await page.getByText('CONSTRUCTION', {exact: true}).waitFor();
    await page.getByText('not reachable from here', {exact: true}).waitFor();
    await page.locator('.cx-entry .cx-remote-off').filter({hasText: EXPLAIN}).waitFor();
    assert.equal(await page.getByRole('link', {name: 'Open construction →'}).count(), 0, 'no way into a refused feature');
    await page.screenshot({path: path.join(shots, 'remote-disabled-property-1440.png')});
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
    await page.locator('.cx-list .cx-remote-off').filter({hasText: EXPLAIN}).waitFor();
    assert.equal(await page.getByLabel('Problem title').count(), 0, 'no create form when refused');
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + cfg.problemId);
    await page.locator('.cx-list-page .cx-remote-off').filter({hasText: EXPLAIN}).waitFor();
    assert.equal(await page.getByRole('heading', {name: cfg.title}).count(), 0, 'the problem is not shown');
    await page.screenshot({path: path.join(shots, 'remote-disabled-problem-1440.png')});
    await page.goto(cfg.url + '/#/tasks/home-construction');
    await page.locator('#homeConstructionHost .cx-remote-off').filter({hasText: EXPLAIN}).waitFor();
    // the owner's ways in: tailscale serve, and this computer — both open the problem
    for (const [name, headers] of [['tailnet', {'X-Forwarded-For': '100.101.102.103', 'X-Forwarded-Proto': 'https', 'Tailscale-User-Login': 'owner@example.com'}], ['local', {}]]) {
      const ctx = await browser.newContext({viewport: {width: 1440, height: 900}, extraHTTPHeaders: headers});
      const lp = await ctx.newPage();
      watch(lp);
      await lp.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
      await lp.locator('.cx-list-page .cx-fine').filter({hasText: FINE}).waitFor();
      assert.equal(await lp.locator('.cx-local-only').count(), 0, name + ': no local-only banner');
      await lp.screenshot({path: path.join(shots, name + '-list-1440.png')});
      await lp.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + cfg.problemId);
      await lp.getByRole('heading', {name: cfg.title}).waitFor();
      assert.equal(await lp.locator('.cx-remote-off').count(), 0, name + ': no refusal');
      await ctx.close();
    }
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId: cfg.problemId, shots}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
