// Native agent steps in the workbench against a REAL Go backend wired with
// the agent-chat store and the real hermes Runner pointed at the protocol
// stub (TestConstructionNativeBrowser): a native research run shows its
// requested/observed runtime and tool scope; "Ask the agent" turns an
// instruction into the agent's typed command on the open draft.
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = JSON.parse(process.argv[2] || '{}');
if (!cfg.url) { console.error('needs the real backend URL'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});
async function pollPage(page, fn, arg, timeout = 30000) {
  const until = Date.now() + timeout;
  for (;;) {
    const v = await page.evaluate(fn, arg);
    if (v) return v;
    if (Date.now() > until) throw new Error('pollPage timed out: ' + fn.toString().slice(0, 200));
    await new Promise((r) => setTimeout(r, 150));
  }
}
(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  try {
    const page = await (await browser.newContext({viewport: {width: 1440, height: 900}})).newPage();
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('request', (r) => { if (!r.url().startsWith(cfg.url) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) external.push(r.url()); });
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
    await page.getByLabel('Problem title').waitFor();
    const created = await page.evaluate(async () => cxApi('POST', cxBase({kind: 'property', id: 'fixture-ooda-house'}), '/problems',
      {schemaVersion: 1, requestId: cxRequestId(), title: 'Native fixture', template: 'roof-masonry-junction'}));
    const id = created.view.problem.id;
    await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + id);
    await page.getByRole('heading', {name: 'Native fixture'}).waitFor();
    await page.evaluate(() => { cxp.open.more = true; cxRender(); }); // the Plan pane folds runs and steward under one summary
    await page.getByText(/bounded-tools: .*the runner enforces it as for every native chat turn/).first().waitFor();
    await page.getByRole('button', {name: 'Start research with the native agent'}).click();
    await pollPage(page, () => { const r = cxLatestRun(); return r && r.state === 'completed'; }, null, 60000);
    await page.locator('.cx-native', {hasText: 'tools construction-none'}).waitFor();
    const nat = await page.locator('.cx-native').textContent();
    assert.ok(/observed stub-model \(runner-report\)/.test(nat), 'observed runtime shown: ' + nat);
    const asm = await page.evaluate(() => cx.activeAssembly);
    const before = await page.evaluate(() => cx.view.assemblies[cx.activeAssembly].modelHash);
    await page.getByLabel('Instruction for the agent').fill('Increase the insulation to 150 mm.');
    await page.getByRole('button', {name: 'Send to agent'}).click();
    await page.getByText(/1 command\(s\) applied as the agent/).waitFor({timeout: 30000});
    await pollPage(page, ([a, h]) => cx.view.assemblies[a].modelHash !== h && cx.view.assemblies[a].components.find((c) => c.type === 'insulation-board').shape.params.thickness.value === 150, [asm, before]);
    await page.screenshot({path: path.join(shots, 'native-agent-1440.png')});
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, problemId: id, native: nat.trim(), shots}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
