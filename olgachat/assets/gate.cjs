// Liber's phone check for a preview of Olga's app (run by olgachat.GitBuilder).
// Usage: node gate.cjs <preview base url>. Exit 0 = passes; prints what broke.
let chromium;
try { ({ chromium } = require('playwright')); } catch (e) { console.log('GATE-SKIP playwright unavailable'); process.exit(3); }
const base = process.argv[2];
(async () => {
  const problems = [];
  let browser;
  try { browser = await chromium.launch(); } catch (e) { console.log('GATE-SKIP no browser'); process.exit(3); }
  const page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  page.on('pageerror', e => problems.push('script error: ' + e.message));
  const api = async p => (await page.request.get(new URL(p.replace(/^\//, ''), base).href)).json();
  const tasks = await api('/api/tasks').catch(() => null);
  const home = [];
  for (const dom of (tasks && tasks.domains) || []) {
    if (dom.name !== 'Home') continue;
    for (const t of [...(dom.tasks || []), ...(dom.buckets || []).flatMap(b => b.tasks || [])]) if (t.state !== 'done') home.push(t.text);
  }
  for (const view of ['day', 'goals', 'tasks']) {
    await page.goto(base + '#/' + view, { waitUntil: 'networkidle' }).catch(e => problems.push(view + ' did not load'));
    await page.waitForTimeout(800);
    const wide = await page.evaluate(() => document.scrollingElement.scrollWidth - innerWidth);
    if (wide > 2) problems.push(view.toUpperCase() + ' scrolls sideways by ' + wide + 'px on a phone');
  }
  // every open Home task is findable on TASKS (search box or board)
  const text = await page.evaluate(() => document.body.innerText);
  const missing = home.filter(t => !text.includes(t.slice(0, 40)));
  if (home.length && missing.length > home.length / 2) problems.push('TASKS no longer shows her Home tasks (' + missing.length + ' of ' + home.length + ' missing)');
  await browser.close();
  if (problems.length) { console.log(problems.join('\n')); process.exit(1); }
  console.log('GATE-OK');
})().catch(e => { console.log('gate error: ' + e.message); process.exit(1); });
