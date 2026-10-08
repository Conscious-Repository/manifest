// The loopback-only boundary in the browser against the REAL backend
// (web_construction_test.go TestConstructionRemoteDisabledBrowser). A context
// whose requests arrive relayed by a proxy (X-Forwarded-For, as `tailscale
// serve` or a reverse proxy adds) is refused by the server, and every
// construction entry explains that remote access is deliberately disabled
// until a verified owner gateway exists. A plain loopback context opens the
// same problem.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
if (!cfg || !cfg.url || !cfg.problemId) { console.error('construction-remote.cjs needs a real backend: go test ./server -run TestConstructionRemoteDisabledBrowser'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const EXPLAIN = /available only on this machine \(loopback\)\. Remote, tailnet and reverse-proxy access is deliberately disabled in this MVP: it needs a verified, authenticated owner gateway/;
(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  const watch = (page) => {
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
  };
  try {
    // relayed: Manifest itself loads; construction is refused and explained
    const relayed = await browser.newContext({viewport: {width: 1440, height: 900}, extraHTTPHeaders: {'X-Forwarded-For': '100.101.102.103'}});
    const page = await relayed.newPage();
    watch(page);
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house');
    await page.getByText('CONSTRUCTION', {exact: true}).waitFor();
    await page.getByText('this machine only', {exact: true}).waitFor();
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
    // loopback: the owner opens the same problem
    const local = await browser.newContext({viewport: {width: 1440, height: 900}});
    const lp = await local.newPage();
    watch(lp);
    await lp.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + cfg.problemId);
    await lp.getByRole('heading', {name: cfg.title}).waitFor();
    assert.equal(await lp.locator('.cx-remote-off').count(), 0, 'no refusal on loopback');
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId: cfg.problemId, shots}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
