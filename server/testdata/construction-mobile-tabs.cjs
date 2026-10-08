// Phone section-tab folding against a REAL backend
// (web_construction_test.go TestConstructionMobileTabsMoreBrowser). At the
// widths where the REAL ESTATE tab row folds, every folded tab is visited
// from MORE, in both themes. Each must open lit and whole in the row with no
// page error and no failed backend request. A handler panic reaches a page
// only as a dropped connection; the Go harness also fails the test on any
// panic in the server log. MAP needs Leaflet from its CDN; offline, as here,
// its designed fallback returns to the list with a notice, which is checked
// instead, and its CDN/tile requests are the only ones allowed off the
// backend.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = process.argv[2] ? JSON.parse(process.argv[2]) : null;
if (!cfg || !cfg.url || !cfg.problemId) { console.error('construction-mobile-tabs.cjs needs a real backend: go test ./server -run TestConstructionMobileTabsMoreBrowser'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
const MAP_HOSTS = /^https:\/\/(cdnjs\.cloudflare\.com|tile\.openstreetmap\.org)\//;
const folded = (page) => page.evaluate(() => [...document.querySelectorAll('#reToggle > .view-tab.mf-tab-folded')].map((t) => t.textContent.trim()));
// the row's invariant after each visit: no tab cut at the edge, the lit one shown
const row = (page) => page.evaluate(() => {
  const nav = document.getElementById('reToggle'), n = nav.getBoundingClientRect();
  const shown = [...nav.querySelectorAll('.view-tab')].filter((t) => t.getClientRects().length);
  const lit = nav.querySelector('.view-tab.on');
  return {cut: shown.filter((t) => { const r = t.getBoundingClientRect(); return r.left < n.left - 1 || r.right > n.right + 1; }).map((t) => t.textContent.trim()),
    lit: lit && lit.getClientRects().length ? lit.textContent.trim() : null};
});

(async () => {
  const browser = await chromium.launch({headless: true});
  const visited = [];
  try {
    for (const theme of ['default', 'jarvis']) {
      for (const width of [320, 390]) {
        const at = width + ' (' + theme + ')';
        const ctx = await browser.newContext({viewport: {width, height: 780}, isMobile: true, hasTouch: true});
        const page = await ctx.newPage();
        const errors = [], failed = [], external = [];
        let inflight = 0;
        const api = (r) => r.url().startsWith(cfg.url) && ['fetch', 'xhr'].includes(r.resourceType());
        page.on('pageerror', (e) => errors.push(e.message));
        page.on('request', (r) => {
          if (api(r)) inflight++;
          else if (!r.url().startsWith(cfg.url) && !/^(data|blob):/.test(r.url())) external.push(r.url());
        });
        page.on('requestfinished', (r) => { if (api(r)) inflight--; });
        page.on('requestfailed', (r) => {
          if (api(r)) inflight--;
          const why = (r.failure() || {}).errorText || '';
          // a page cancelling its own fetch is not a server failure
          if (r.url().startsWith(cfg.url) && why !== 'net::ERR_ABORTED') failed.push(r.method() + ' ' + new URL(r.url()).pathname + ': ' + why);
        });
        // settle: no backend fetch in flight for 400 ms (at most 5 s)
        const settle = async () => { for (let quiet = 0, i = 0; quiet < 4 && i < 50; i++) { await page.waitForTimeout(100); quiet = inflight > 0 ? 0 : quiet + 1; } };
        await page.addInitScript((t) => { if (t === 'jarvis') localStorage.setItem('manifest.theme', 'jarvis'); }, theme);
        await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + cfg.problemId);
        await page.getByRole('heading', {name: cfg.title}).waitFor();
        await page.waitForFunction(() => !document.querySelector('.hud-boot'), null, {timeout: 5000});
        await settle();
        const start = await folded(page);
        assert.ok(start.length >= 3, at + ': the row folds (folded: ' + start.join(', ') + ')');
        for (const name of start) {
          const before = external.length;
          assert.ok((await folded(page)).includes(name), at + ': ' + name + ' is still folded under MORE');
          await page.locator('#reToggle .mf-tabs-more').click();
          const list = page.getByRole('dialog', {name: 'More REAL ESTATE sections'}).getByRole('navigation', {name: 'REAL ESTATE sections'});
          await list.getByRole('link', {name, exact: true}).click();
          if (name === 'MAP') {
            await page.getByText('Map unavailable offline — showing the list').first().waitFor();
            await page.waitForFunction(() => location.hash === '#/properties');
            assert.ok(external.slice(before).every((u) => MAP_HOSTS.test(u)), at + ': MAP reached beyond its CDN: ' + external.slice(before).join(', '));
          } else {
            const lit = await page.waitForFunction((n) => { const t = document.querySelector('#reToggle .view-tab.on'); return t && t.textContent.trim() === n && t.getClientRects().length && !document.querySelector('.mf-sheet-wrap:not([hidden])'); }, name, {timeout: 10000}).then(() => true, () => false);
            assert.ok(lit, at + ': ' + name + ', chosen from MORE, must open lit in the row');
          }
          await settle();
          const r = await row(page);
          assert.deepEqual(r.cut, [], at + ' after ' + name + ': tabs cut at the edge');
          assert.ok(r.lit, at + ' after ' + name + ': the lit tab is not shown');
          visited.push(at + ' ' + name);
        }
        await page.screenshot({path: path.join(shots, 'tabs-more-' + width + '-' + theme + '.png')});
        assert.deepEqual(errors, [], at + ': page errors');
        assert.deepEqual(failed, [], at + ': failed backend requests (a handler panic drops the connection)');
        assert.ok(external.every((u) => MAP_HOSTS.test(u)), at + ': requests outside the backend: ' + external.filter((u) => !MAP_HOSTS.test(u)).join(', '));
        await ctx.close();
      }
    }
    console.log(JSON.stringify({ok: true, visited}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
