// Robustness of Alfred's suggestions in the Plan pane, against the REAL
// backend with the conversation scripted (web_construction_test.go,
// TestConstructionPlanQABrowser). Found by QA on 2026-10-09:
//   1. a malformed reply (options as a string, null operations) renders,
//      normalised, and the pane keeps working;
//   2. a suggestion applied in another browser shows as applied here (the
//      problem itself says so), and its next step doesn't ask for review;
//   3. undo refuses — and says why — when the approach changed after;
//   4. a suggestion that creates an approach can be applied, undone and
//      applied again (the approach comes back).
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const cfg = JSON.parse(process.argv[2] || '{}');
const hex = () => [...Array(32)].map(() => '0123456789abcdef'[Math.floor(Math.random() * 16)]).join('');
const block = (p) => '```construction\n' + (typeof p === 'string' ? p : JSON.stringify(p)) + '\n```';
(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [];
  try {
    const page0 = await browser.newPage();
    await page0.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
    await page0.waitForFunction(() => typeof cxApi === 'function');
    const created = await page0.evaluate(async () => cxApi('POST', cxBase({kind: 'property', id: 'fixture-ooda-house'}), '/problems', {schemaVersion: 1, requestId: cxRequestId(), title: 'QA fixture', template: 'roof-masonry-junction'}));
    const id = created.view.problem.id, asm = created.view.problem.alternatives[0];
    await page0.close();
    const q1 = 'dq-' + hex(), variant = 'asm-' + hex();
    const turns = [{n: 1, who: 'user', at: '', text: 'go'},
      {n: 2, who: 'alfred', at: '', text: 'A malformed one:\n\n' + block({summary: 7, changes: [null, {operations: [null, {op: 'AddQuestion', id: 'dq-' + hex(), text: 'Odd options?', options: 'Solid / Cavity'}]}]})},
      {n: 3, who: 'alfred', at: '', text: 'A question:\n\n' + block({summary: 'one question', changes: [{operations: [{op: 'AddQuestion', id: q1, text: 'Which metal?', options: ['galvanized', 'aluminum']}]}]})},
      {n: 4, who: 'alfred', at: '', text: 'An approach:\n\n' + block({summary: 'a third approach', changes: [{assemblyId: asm, operations: [{op: 'CreateVariant', newAssemblyId: variant, name: 'Option Q'}]}, {assemblyId: variant, operations: [{op: 'SetAssemblyText', summary: 'from QA'}]}]})}];
    const chat = {agent: 'alfred', session: 'qa', href: '#/chat/a/alfred/qa', pending: false, turns};
    const open = async () => {
      const ctx = await browser.newContext({viewport: {width: 1440, height: 900}});
      await ctx.route('**/chat', (r) => r.request().method() === 'GET' ? r.fulfill({contentType: 'application/json', body: JSON.stringify({chat})}) : r.abort());
      const page = await ctx.newPage();
      page.on('pageerror', (e) => errors.push(e.message));
      await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + id);
      await page.waitForFunction(() => typeof cxp !== 'undefined' && cxp.chat && document.querySelectorAll('.cx-proposal').length >= 3, null, {timeout: 20000});
      return {ctx, page};
    };
    // 1. malformed: rendered, normalised
    let {ctx, page} = await open();
    const bad = page.locator('.cx-proposal').nth(0);
    assert.match(await bad.innerText(), /Odd options\?/);
    // 2. apply the question in this browser…
    await page.locator('.cx-proposal').nth(1).getByRole('button', {name: 'Apply'}).click();
    await page.waitForFunction((q) => (cx.view.problem.questions || []).some((x) => x.id === q), q1);
    // 4. …and the approach: apply, undo, apply again
    const card = () => page.locator('.cx-proposal').nth(2);
    await card().getByRole('button', {name: 'Apply'}).click();
    await page.waitForFunction((v) => cx.view.assemblies[v] && cx.view.assemblies[v].summary === 'from QA', variant, {timeout: 8000}).catch(async (e) => { throw new Error('apply variant: ' + JSON.stringify(await page.evaluate(() => ({applied: cxp.applied, err: cx.error, alts: cx.view.problem.alternatives})))); });
    await card().getByRole('button', {name: 'Undo'}).click();
    await page.waitForFunction((v) => cx.view.assemblies[v].lifecycle === 'superseded', variant);
    await card().getByRole('button', {name: 'Apply'}).click();
    await page.waitForFunction((v) => cx.view.assemblies[v].lifecycle === 'draft', variant);
    await page.waitForFunction(() => Object.entries(cxp.applied).some(([k, v]) => k.endsWith(':4:0') && v.ok) && !cxp.applying);
    assert.equal(await card().getAttribute('class').then((c) => /is-applied/.test(c)), true, 'applied again');
    // 3. a later hand edit: undo refuses and says why
    await page.evaluate(async (v) => { await cxCommand([{op: 'SetAssemblyText', summary: 'hand edit'}], {assembly: v}); }, variant);
    await card().getByRole('button', {name: 'Undo'}).click();
    await card().getByText(/changed after this was applied/).waitFor();
    assert.equal(await page.evaluate((v) => cx.view.assemblies[v].summary, variant), 'hand edit', 'the later edit is kept');
    await ctx.close();
    // 2. another browser: the question suggestion shows as applied
    ({ctx, page} = await open());
    const qCard = page.locator('.cx-proposal').nth(1);
    assert.match(await qCard.innerText(), /^APPLIED|Applied/i);
    await qCard.getByText(/Applied on another device/).waitFor();
    assert.doesNotMatch(await page.locator('.cx-next').innerText(), /Alfred suggests/, 'nothing left to review');
    await ctx.close();
    assert.deepEqual(errors, [], 'page errors');
    console.log(JSON.stringify({ok: true}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
