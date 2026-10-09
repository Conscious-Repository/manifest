// The Plan pane against a REAL Go backend with the protocol-fake hermes
// (web_construction_test.go, TestConstructionPlanBrowser): the owner asks
// Alfred to research the problem from the pane, the reply (an ordinary chat
// turn) carries a proposal block, Apply turns it into an owner command, the
// decision point is answered in place, and the stages move on. At 1440 and
// 390 px; the page never pans sideways.
//
//   node server/testdata/construction-plan.cjs '{"url":"http://127.0.0.1:PORT"}'
const {chromium} = require('playwright');
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const cfg = JSON.parse(process.argv[2] || '{}');
if (!cfg.url) { console.error('construction-plan.cjs needs the real backend URL'); process.exit(2); }
const shots = cfg.shots || '/tmp/manifest-construction-qa/shots';
fs.mkdirSync(shots, {recursive: true});

(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], external = [];
  try {
    for (const [width, height, mobile] of [[1440, 900, false], [390, 844, true]]) {
      const ctx = await browser.newContext({viewport: {width, height}, isMobile: mobile, hasTouch: mobile});
      await ctx.route('**/*', (r) => { const u = new URL(r.request().url()); if (u.hostname !== '127.0.0.1') { external.push(u.href); return r.abort(); } return r.continue(); });
      const page = await ctx.newPage();
      page.on('pageerror', (e) => errors.push(width + ': ' + e.message));
      await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction');
      await page.waitForFunction(() => typeof cxApi === 'function');
      const created = await page.evaluate(async (w) => cxApi('POST', cxBase({kind: 'property', id: 'fixture-ooda-house'}), '/problems',
        {schemaVersion: 1, requestId: cxRequestId(), title: 'Plan fixture ' + w, template: 'roof-masonry-junction'}), width);
      const id = created.view.problem.id;
      await page.goto(cfg.url + '/#/properties/fixture-ooda-house/construction/' + id);
      await page.getByRole('heading', {name: 'Plan fixture ' + width}).waitFor();
      if (mobile) await page.getByRole('tab', {name: 'Plan', exact: true}).click();
      await page.getByRole('heading', {name: 'Decisions to make'}).waitFor();
      // the model's own open question is answerable here
      await page.locator('.cx-dp[data-q="wall"]').waitFor();
      // one next step at a time: describe it, in the chat
      assert.match(await page.locator('.cx-next').innerText(), /Describe it/);
      await page.locator('.cx-next').getByRole('button', {name: 'Describe it'}).click();
      await page.getByLabel('Message to Alfred').fill('The new roof meets the old brick wall at its high edge. How do we flash it?');
      await page.getByRole('button', {name: 'Send', exact: true}).click();
      await page.locator('.cx-next', {hasText: /Alfred suggests 1 change/}).waitFor({timeout: 30000});
      const card = page.locator('.cx-proposal');
      await card.waitFor({timeout: 30000});
      assert.match(await card.innerText(), /Decision to make: Is the wall solid brick or a cavity wall\?/);
      assert.equal(await page.locator('.cx-pane-a select, .cx-steward').count(), 0, 'no steward picker or direct agent box in the Plan');
      await page.getByRole('link', {name: 'Open in Chat'}).waitFor();
      await card.getByRole('button', {name: 'Apply'}).click();
      await page.waitForFunction(() => (cx.view.problem.questions || []).length === 1);
      await page.locator('.cx-proposal.is-applied').waitFor();
      // one tap undoes the whole suggestion, and it can be applied again
      await page.locator('.cx-proposal.is-applied').getByRole('button', {name: 'Undo'}).click();
      await page.waitForFunction(() => cx.view.problem.questions[0].state === 'dropped');
      await page.locator('.cx-proposal.is-quiet').getByRole('button', {name: 'Apply'}).click();
      await page.waitForFunction(() => cx.view.problem.questions.length === 1 && cx.view.problem.questions[0].state === 'open');
      const q = page.locator('.cx-dp', {hasText: 'Is the wall solid brick or a cavity wall?'}).filter({has: page.getByRole('button', {name: 'cavity', exact: true})});
      await q.getByRole('button', {name: 'solid', exact: true}).click();
      await page.waitForFunction(() => cx.view.problem.questions[0].state === 'answered' && cx.view.problem.questions[0].answer === 'solid');
      await page.locator('.cx-dp-done summary', {hasText: '1 settled'}).waitFor();
      assert.equal(await page.evaluate(() => cx.error), '', 'no refusal');
      // progress reads as five stages; describe and research are done once Alfred replied
      assert.equal(await page.locator('.cx-progress li').count(), 5);
      assert.equal(await page.locator('.cx-progress li.is-done', {hasText: 'Research'}).count(), 1, 'research stage done');
      assert.equal(await page.locator('.cx-guide').count(), 0, 'the header carries no second guide');
      const pan = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      assert.ok(pan <= 1, 'no sideways pan at ' + width + ': ' + pan);
      await page.screenshot({path: path.join(shots, 'plan-' + width + '.png')});
      await ctx.close();
    }
    assert.deepEqual(errors, [], 'page errors');
    assert.deepEqual(external, [], 'requests outside the backend');
    console.log(JSON.stringify({ok: true, shots}));
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
