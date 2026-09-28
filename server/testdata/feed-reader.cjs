// feed-reader.cjs — the reader pass (2026-09-27), against the real front end
// over feed-stub-api.cjs. It proves: every view has an address; the sidebar
// counts follow what you do; a row opens in the pane on a wide screen and on
// its own page on a narrow one; bodies fetched ahead never mark anything
// read; a visited view repaints from memory before the network answers;
// Later takes feed items and pasted links and keeps them until done; paid
// posts never ask to sign in unless you say you pay; videos and episodes
// play; and nothing overflows at 390 or 1440 in either theme.
const { chromium } = require('playwright'), assert = require('node:assert/strict');
const { makeFeedStub } = require('./feed-stub-api.cjs');

(async () => {
  const stub = makeFeedStub();
  await new Promise((r) => stub.server.listen(0, '127.0.0.1', r));
  const base = 'http://127.0.0.1:' + stub.server.address().port;
  const log = async () => (await (await fetch(base + '/__log')).json()).log;
  const browser = await chromium.launch({ headless: true, channel: 'chromium' });
  const errors = [];
  try {
    // ---- wide: sidebar · list · pane ----
    const p = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    p.on('pageerror', (e) => errors.push(e.message));
    await p.goto(base + '/#/feed/unread');
    await p.locator('.consume-row').first().waitFor();
    assert.equal(await p.locator('.rdr-title').textContent(), 'Unread');
    const unreadN = async () => Number(await p.locator('.rdr-nav[data-key="unread"] .rdr-nav-n').textContent());
    await p.waitForFunction(() => document.querySelector('.rdr-nav[data-key="unread"] .rdr-nav-n').textContent !== '');
    const before = await unreadN();
    assert.ok(before >= 7, 'sidebar carries the unread count: ' + before);
    assert.equal(await p.locator('.rdr-nav.on').getAttribute('data-key'), 'unread');
    assert.equal(await p.locator('#feedFilters').isVisible(), false, 'beside the sidebar the header chips are hidden');

    // prefetch: the first rows' bodies are fetched ahead with ?peek=1 …
    await p.waitForTimeout(300);
    const first = await p.locator('.consume-row').first().getAttribute('data-consume-id');
    assert.ok((await log()).some((l) => l.startsWith('GET /api/consume/item/' + encodeURIComponent(first) + '?peek=1')), 'the top row is fetched ahead');
    assert.equal(stub.items.find((c) => c.id === first).read, false, 'fetching ahead marks nothing read');
    // … so opening it is instant, marks it read with one POST, and keeps the address
    await p.locator('.consume-row').first().click();
    await p.locator('#feedPane .rdr-article-title').waitFor();
    assert.equal(await p.evaluate(() => location.hash), '#/feed/unread', 'the pane opens without leaving the list');
    assert.match(await p.locator('#feedPane .consume-body').textContent(), /The whole of/);
    await p.waitForTimeout(150);
    const opened = (await log()).filter((l) => l.includes(encodeURIComponent(first)));
    assert.ok(opened.some((l) => l === 'POST /api/consume/item/' + encodeURIComponent(first) + '/read'), 'a prefetched body is marked read by POST');
    assert.ok(!opened.some((l) => l === 'GET /api/consume/item/' + encodeURIComponent(first)), 'and never fetched twice');
    assert.equal(await unreadN(), before - 1, 'the sidebar count follows the read');
    assert.equal(await p.locator('.consume-row').first().evaluate((r) => r.classList.contains('unread')), false);

    // j moves and opens the next; m keeps it unread; l saves it to Later
    await p.keyboard.press('j');
    await p.waitForFunction((id) => document.querySelector('.consume-row.sel')?.dataset.consumeId !== id, first);
    const second = await p.locator('.consume-row.sel').getAttribute('data-consume-id');
    await p.keyboard.press('l');
    await p.waitForFunction(() => document.querySelector('.rdr-nav[data-key="later"] .rdr-nav-n').textContent === '1');
    assert.equal(stub.later.length, 1);
    assert.equal(stub.later[0].item, second);

    // Videos: a thumbnail in the row, a player in the pane (from the allowlisted template)
    await p.click('.rdr-nav[data-key="type:video"]');
    await p.waitForFunction(() => location.hash === '#/feed/type/video');
    await p.waitForFunction(() => document.querySelector('.rdr-title').textContent === 'Videos');
    await p.waitForFunction(() => document.querySelectorAll('.consume-row').length && [...document.querySelectorAll('.consume-row')].every((r) => r.classList.contains('rdr-video')));
    assert.ok(await p.locator('.consume-row .rdr-thumb').count() > 0, 'video rows carry a thumbnail');
    await p.locator('.consume-row').first().click();
    await p.locator('#feedPane iframe.consume-embed').waitFor();
    assert.match(await p.locator('#feedPane iframe').getAttribute('src'), /^https:\/\/www\.youtube-nocookie\.com\/embed\/[A-Za-z0-9_-]+/);

    // Podcasts: the publisher's own enclosure plays
    await p.click('.rdr-nav[data-key="type:podcast"]');
    await p.waitForFunction(() => document.querySelector('.rdr-title').textContent === 'Podcasts');
    await p.locator('.consume-row').first().click();
    await p.locator('#feedPane audio.read-audio').waitFor();
    assert.equal(await p.locator('#feedPane audio').getAttribute('src'), 'https://jung.example/ep1.mp3');

    // a visited view repaints from memory before the network answers
    await fetch(base + '/__delay?ms=1500');
    await p.click('.rdr-nav[data-key="unread"]');
    await p.waitForFunction(() => document.querySelector('.rdr-title').textContent === 'Unread');
    assert.ok(await p.locator('.consume-row').count() > 0, 'the rows are there at once, from memory');
    await fetch(base + '/__delay?ms=0');
    await p.waitForTimeout(1600);

    // Later: the saved item, then a pasted link; done moves it to Later · done
    await p.click('.rdr-nav[data-key="later"]');
    await p.waitForFunction(() => location.hash === '#/feed/later');
    await p.locator('.consume-row').first().waitFor();
    await p.click('.consume-save-link');
    await p.locator('.consume-savelink input').fill('https://pasted.example/an-essay');
    await p.keyboard.press('Enter');
    await p.waitForFunction(() => document.querySelectorAll('.consume-row').length === 2);
    assert.equal(stub.later.length, 2);
    await p.locator('.consume-row').first().click();
    await p.keyboard.press('e');
    await p.waitForFunction(() => document.querySelectorAll('.consume-row').length === 1);
    assert.ok(stub.later.some((e) => e.done), 'done is recorded');
    await p.click('.consume-view-filters >> text=DONE');
    await p.waitForFunction(() => location.hash === '#/feed/later/done');
    await p.waitForFunction(() => document.querySelectorAll('.consume-row').length === 1);

    // Paid posts: information, not a prompt — until you say you pay
    await p.click('.rdr-nav[data-key="stream:essays"]');
    await p.click('.consume-manage-toggle');
    const letters = p.locator('.consume-sub', { hasText: 'Letters from Somewhere' });
    await letters.waitFor();
    assert.match(await letters.textContent(), /1 paid post shows as a preview/);
    assert.equal(await letters.locator('.consume-signin-link').count(), 0, 'no sign-in prompt for a publication you do not pay for');
    assert.equal(await p.locator('.consume-sub', { hasText: 'The Thinker' }).locator('.consume-switch').count(), 0, 'no paid switch where nothing is paid');
    await letters.locator('.consume-switch input').check();
    await p.waitForFunction(() => [...document.querySelectorAll('.consume-sub')].some((r) => r.textContent.includes('Letters from Somewhere') && r.querySelector('.consume-signin-link')));
    assert.equal(stub.subs.find((s) => s.id === 'letters').pays, true, 'the switch is saved');
    assert.ok(await p.locator('.consume-sub', { hasText: 'Veritasium' }).locator('.consume-switch', { hasText: 'include Shorts' }).count() === 1, 'a video channel offers its Shorts switch');

    // a source's own view carries its settings; its stream changes in one step
    await p.click('.consume-manage-toggle'); // close the all-sources panel
    await p.click('.rdr-nav[data-key="source:jung"]');
    await p.waitForFunction(() => location.hash === '#/feed/source/jung');
    const card = p.locator('.consume-subbar .consume-sub-card');
    await card.waitFor();
    assert.match(await card.textContent(), /This Jungian Life/);
    assert.ok(await card.locator('text=unfollow').count() === 1, 'unfollow is in the source view');
    await card.locator('.consume-stream-select').selectOption('science');
    await p.waitForFunction(() => document.querySelector('.rdr-nav[data-key="stream:science"]') && [...document.querySelectorAll('.rdr-stream-kids .rdr-nav')].some((a) => a.dataset.key === 'source:jung'));
    assert.equal(stub.subs.find((s) => s.id === 'jung').list, 'science', 'the move is saved');

    // Curated is a view of its own
    await p.click('.rdr-nav[data-key="curated"]');
    await p.waitForFunction(() => location.hash === '#/feed/curated');
    await p.getByText('A curated essay').waitFor();
    assert.equal(await p.locator('.rdr-nav[data-key="curated"] .rdr-nav-n').textContent(), '1');
    assert.equal(await p.locator('.consume-curated-toggle').isVisible(), false, 'the header toggle gives way to the view');

    // full view: the article takes the page, and Esc gives the list back
    await p.click('.rdr-nav[data-key="all"]');
    await p.locator('.consume-row').first().click();
    await p.locator('#feedPane .rdr-full-btn').click();
    await p.waitForFunction(() => document.getElementById('feedView').classList.contains('rdr-full'));
    assert.equal(await p.locator('.feed-col').isVisible(), false, 'the list steps aside');
    const paneW = await p.locator('#feedPane').evaluate((e) => e.getBoundingClientRect().width);
    assert.ok(paneW > 900, 'the article gets the width: ' + paneW);
    await p.keyboard.press('j');
    await p.waitForTimeout(200);
    assert.equal(await p.evaluate(() => document.getElementById('feedView').classList.contains('rdr-full')), true, 'j keeps full view');
    await p.keyboard.press('Escape');
    await p.waitForFunction(() => !document.getElementById('feedView').classList.contains('rdr-full'));
    assert.equal(await p.locator('.feed-col').isVisible(), true);
    await p.keyboard.press('f');
    await p.waitForFunction(() => document.getElementById('feedView').classList.contains('rdr-full'));
    await p.keyboard.press('f');
    await p.waitForFunction(() => !document.getElementById('feedView').classList.contains('rdr-full'));

    // the Inbox is one click away, at its own address, and says when it is empty
    await p.click('.rdr-nav[data-key="inbox"]');
    await p.waitForFunction(() => location.hash === '#/feed');
    await p.getByText('Inbox zero — nothing awaiting you.').waitFor();
    assert.equal(await p.locator('#feedPane').isHidden(), true, 'the pane belongs to reading');
    assert.match(await p.locator('.feed-read-link').textContent(), /unread to read/);

    // no overflow, both themes
    for (const theme of ['default', 'jarvis']) {
      await p.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
      await p.goto(base + '/#/feed/unread'); await p.locator('.consume-row').first().waitFor();
      const over = await p.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(over <= 0, 'no horizontal overflow at 1440 (' + theme + '): ' + over);
    }
    await p.close();

    // ---- narrow: a strip of views, and the reading page ----
    const q = await browser.newPage({ viewport: { width: 390, height: 844 } });
    q.on('pageerror', (e) => errors.push(e.message));
    await q.goto(base + '/#/feed/all');
    await q.locator('.consume-row').first().waitFor();
    const strip = await q.evaluate(() => getComputedStyle(document.getElementById('feedSide')).flexDirection);
    assert.equal(strip, 'row', 'the sidebar is a strip on a phone');
    for (const theme of ['default', 'jarvis']) {
      await q.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
      const over = await q.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(over <= 0, 'no horizontal overflow at 390 (' + theme + '): ' + over);
    }
    const id = await q.locator('.consume-row').first().getAttribute('data-consume-id');
    await q.locator('.consume-row .rdr-row-title').first().click();
    await q.waitForFunction((i) => location.hash === '#/read/' + encodeURIComponent(i), id);
    await q.locator('#readBody p').first().waitFor();
    await q.keyboard.press('Escape');
    await q.waitForFunction(() => location.hash === '#/feed/all');
    await q.close();

    assert.deepEqual(errors, []);
    console.log('PASS: reader views by address, pane + page, peek never marks read, memory repaint, Later (item, link, done), paid is information, video + episode play, 390/1440 both themes');
  } finally {
    await browser.close();
    stub.server.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });
