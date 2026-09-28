// feed-stub-api.cjs — a stateful stub of the reader API (FEED + CONSUME) for
// browser fixtures that load the real front end (index.html + every script).
//
// Sources in two streams plus one loose source, across all four media types:
// articles (a Substack with one paid post), a YouTube channel, a podcast and
// an X account. Read state, dismissals, the Watch Later queue and the source
// switches are kept in memory, and the list answers with the same shape as
// server/consume.go (paging, nav counts). Test hooks:
//   /__log              every API request, in order
//   /__delay?ms=N       hold every /api/consume list answer N ms
const fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const web = path.join(__dirname, '../web');
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2', '.json': 'application/json', '.webmanifest': 'application/manifest+json' };

function makeFeedStub() {
  const now = Date.now();
  const iso = (hoursAgo) => new Date(now - hoursAgo * 3600e3).toISOString();
  const subs = [
    { id: 'letters', title: 'Letters from Somewhere', kind: 'rss', url: 'https://letters.example/feed', list: 'essays', media: 'article', mirror: 'full', pays: false, shorts: false },
    { id: 'thinker', title: 'The Thinker', kind: 'rss', url: 'https://thinker.example/feed', list: 'essays', media: 'article', mirror: 'full', pays: false, shorts: false },
    { id: 'veritas', title: 'Veritasium', kind: 'rss', url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCx', list: 'science', media: 'video', mirror: 'full', pays: false, shorts: false },
    { id: 'jung', title: 'This Jungian Life', kind: 'rss', url: 'https://jung.example/rss', list: '', media: 'podcast', mirror: 'full', pays: false, shorts: false },
    { id: 'vie', title: '@viemccoy', kind: 'rss', url: 'http://127.0.0.1:1200/twitter/user/viemccoy', list: '', media: 'post', mirror: 'full', pays: false, shorts: false },
  ];
  const items = [];
  const add = (sub, n, extra) => items.push(Object.assign({
    id: 'consume:rss:' + sub + ':' + String(n).padStart(12, '0'), subId: sub, kind: 'consume', type: 'rss',
    source: subs.find((s) => s.id === sub).title, list: subs.find((s) => s.id === sub).list,
    title: sub + ' piece ' + n, url: 'https://' + sub + '.example/p/' + n,
    excerpt: 'An excerpt of ' + sub + ' piece ' + n + ' that runs on for a sentence or two so the row has something to clamp.',
    chars: 6000, minutes: 5, published: iso(n * 3), read: false, seeded: false, curated: false,
    body: '<p>The whole of ' + sub + ' piece ' + n + '.</p><p>' + 'A paragraph of real prose. '.repeat(40) + '</p>',
  }, extra || {}));
  for (let n = 1; n <= 4; n++) add('letters', n, n === 2 ? { preview: 'paid', title: 'A paid letter' } : {});
  for (let n = 1; n <= 3; n++) add('thinker', n, n === 3 ? { read: true } : {});
  for (let n = 1; n <= 2; n++) add('veritas', n, { type: 'video', embed: 'youtube:video:JsBZOcqZer' + n, image: '/icons/icon-192.png', chars: 200, body: '<p>How the machine worked.</p>' });
  add('jung', 1, { type: 'podcast', audio: 'https://jung.example/ep1.mp3', audioType: 'audio/mpeg', duration: 3720, image: '/icons/icon-192.png', body: '<p>Show notes.</p>' });
  add('vie', 1, { type: 'x', title: '', excerpt: 'A long post about multipolar worlds that is really the whole of the post and not an excerpt of it.', body: '<p>A long post about multipolar worlds.</p>' });
  // many older, already-read articles so paging has something to page
  for (let n = 5; n <= 70; n++) add('letters', n, { read: true, published: iso(24 + n * 5) });
  const later = []; // {id, url, title, source, kind, item, added, done}
  const curated = [{ title: 'A curated essay', url: 'https://letters.example/p/c1', source: 'Letters from Somewhere', author: 'L', curated: iso(30), note: 'why this one', itemId: 'consume:rss:letters:000000000001', path: 'extrinsic/a curated essay.md' }];
  const log = [];
  let delay = 0;

  const media = (c) => c.type === 'video' ? 'video' : c.type === 'podcast' ? 'podcast' : c.type === 'x' ? 'post' : 'article';
  const unread = (c) => !c.read && !c.seeded && !c.dismissed;
  const laterFor = (c) => later.find((e) => e.item === c.id || e.url === c.url);
  const card = (c) => {
    const { body, dismissed, ...rest } = c;
    const e = laterFor(c);
    return e ? { ...rest, later: true, laterId: e.id, laterDone: !!e.done } : rest;
  };
  const nav = () => {
    const n = { unread: 0, today: 0, later: later.filter((e) => !e.done).length, curated: curated.length, types: {}, streams: {}, subs: {} };
    subs.forEach((s) => { n.subs[s.id] = 0; });
    items.filter((c) => !c.dismissed).forEach((c) => {
      if (Date.parse(c.published) > now - 24 * 3600e3) n.today++;
      if (!unread(c)) return;
      n.unread++; n.subs[c.subId]++;
      if (c.list) n.streams[c.list] = (n.streams[c.list] || 0) + 1;
      n.types[media(c)] = (n.types[media(c)] || 0) + 1;
    });
    return n;
  };
  const statuses = () => subs.map((s) => {
    const mine = items.filter((c) => c.subId === s.id && !c.dismissed);
    return { ...s, lastOk: iso(1), unread: mine.filter(unread).length, archived: mine.filter((c) => !unread(c)).length, total: mine.length,
      paid: mine.some((c) => c.preview === 'paid'), paidPosts: mine.filter((c) => c.preview === 'paid').length,
      site: s.url.split('/')[2], signedIn: false };
  });
  const list = (q) => {
    const view = q.get('view') || 'unread';
    const needle = (q.get('q') || '').toLowerCase();
    let out;
    if (view === 'later' || view === 'later-done') {
      out = later.slice().reverse().filter((e) => !!e.done === (view === 'later-done')).map((e) => {
        const c = items.find((x) => x.id === e.item);
        const base = c ? card(c) : { id: 'consume:later:_later:' + e.id.slice(6), subId: '_later', kind: 'consume', type: e.kind === 'video' ? 'video' : 'rss', source: e.source, title: e.title, url: e.url, excerpt: 'Saved from a pasted link.', minutes: 3, published: '' };
        return { ...base, id: c ? 'consume:later:_later:' + e.id.slice(6) : base.id, subId: '_later', later: true, laterId: e.id, laterDone: !!e.done, saved: e.added };
      });
    } else {
      out = items.filter((c) => !c.dismissed)
        .filter((c) => view === 'all' ? true : view === 'today' ? Date.parse(c.published) > now - 24 * 3600e3 : unread(c))
        .filter((c) => !q.get('sub') || c.subId === q.get('sub'))
        .filter((c) => !q.get('list') || c.list === q.get('list'))
        .filter((c) => !q.get('type') || media(c) === q.get('type'))
        .filter((c) => !needle || (c.title + ' ' + c.excerpt + ' ' + c.source).toLowerCase().includes(needle))
        .sort((a, b) => b.published.localeCompare(a.published)).map(card);
    }
    const total = out.length, offset = Number(q.get('offset')) || 0, limit = Number(q.get('limit')) || 0;
    let page = out.slice(offset), more = false;
    if (limit > 0 && limit < page.length) { page = page.slice(0, limit); more = true; }
    return { items: page, more, count: total, lists: [...new Set(subs.map((s) => s.list).filter(Boolean))].sort(), unread: nav().unread, total: nav().unread, nav: nav() };
  };
  const itemById = (id) => {
    const direct = items.find((c) => c.id === id);
    if (direct) return direct;
    const e = later.find((x) => 'consume:later:_later:' + x.id.slice(6) === id);
    if (!e) return null;
    const src = items.find((c) => c.id === e.item);
    return src ? { ...src, id, subId: '_later' } : { id, subId: '_later', title: e.title, url: e.url, source: e.source, body: '<p>The pasted piece, read whole.</p>', chars: 3000 };
  };

  const json = (res, code, body) => { res.writeHead(code, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  const readBody = (req) => new Promise((resolve) => { let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => { try { resolve(JSON.parse(b || '{}')); } catch (e) { resolve({}); } }); });

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://x'); const p = url.pathname;
    if (p === '/__log') return json(res, 200, { log });
    if (p === '/__delay') { delay = Number(url.searchParams.get('ms')) || 0; return json(res, 200, { delay }); }
    if (p.startsWith('/api/')) {
      log.push(req.method + ' ' + p + url.search);
      if (p === '/api/consume' && req.method === 'GET') { const body = list(url.searchParams); return setTimeout(() => json(res, 200, body), delay); }
      if (p === '/api/consume/subscriptions' && req.method === 'GET') return json(res, 200, { subscriptions: statuses(), xReady: false, nav: nav() });
      if (p === '/api/consume/curated') return json(res, 200, { entries: curated, public: '' });
      let m = p.match(/^\/api\/consume\/item\/([^/]+)$/);
      if (m && req.method === 'GET') {
        const c = itemById(decodeURIComponent(m[1]));
        if (!c) return json(res, 404, {});
        if (url.searchParams.get('peek') !== '1') c.read = true;
        const e = laterFor(c) || later.find((x) => 'consume:later:_later:' + x.id.slice(6) === c.id);
        return json(res, 200, { ...c, read: c.read || url.searchParams.get('peek') !== '1', later: !!e, laterId: e && e.id, laterDone: !!(e && e.done) });
      }
      m = p.match(/^\/api\/consume\/item\/([^/]+)\/(read|unread|dismiss|undismiss)$/);
      if (m && req.method === 'POST') {
        const c = items.find((x) => x.id === decodeURIComponent(m[1]));
        if (!c) return json(res, 404, {});
        if (m[2] === 'read') c.read = true;
        if (m[2] === 'unread') { c.read = false; c.seeded = false; }
        if (m[2] === 'dismiss') c.dismissed = true;
        if (m[2] === 'undismiss') { c.dismissed = false; c.read = false; }
        return json(res, 200, { ok: true });
      }
      if (p === '/api/consume/read-all' && req.method === 'POST') {
        let n = 0; items.filter((c) => unread(c) && (!url.searchParams.get('list') || c.list === url.searchParams.get('list')) && (!url.searchParams.get('type') || media(c) === url.searchParams.get('type')) && (!url.searchParams.get('sub') || c.subId === url.searchParams.get('sub'))).forEach((c) => { c.read = true; n++; });
        return json(res, 200, { ok: true, marked: n });
      }
      if (p === '/api/consume/later' && req.method === 'POST') {
        const b = await readBody(req);
        const src = b.item && items.find((x) => x.id === b.item);
        const u = src ? src.url : b.url;
        if (!u) return json(res, 400, { error: 'paste a link' });
        let e = later.find((x) => x.url === u);
        if (!e) {
          e = { id: 'later-' + String(later.length + 1).padStart(12, '0'), url: u, title: src ? src.title : 'A pasted essay', source: src ? src.source : 'pasted.example', kind: src ? media(src) : 'article', item: src ? src.id : '', added: new Date(now).toISOString().slice(0, 10), done: '' };
          later.push(e);
        } else e.done = '';
        return json(res, 200, { ok: true, entry: e });
      }
      m = p.match(/^\/api\/consume\/later\/([^/]+)\/(done|undone|remove)$/);
      if (m && req.method === 'POST') {
        const i = later.findIndex((x) => x.id === m[1]);
        if (i < 0) return json(res, 400, { error: 'no such Later entry' });
        if (m[2] === 'done') later[i].done = new Date(now).toISOString().slice(0, 10);
        if (m[2] === 'undone') later[i].done = '';
        if (m[2] === 'remove') later.splice(i, 1);
        return json(res, 200, { ok: true });
      }
      m = p.match(/^\/api\/consume\/subscriptions\/([^/]+)\/update$/);
      if (m && req.method === 'POST') {
        const b = await readBody(req), s = subs.find((x) => x.id === m[1]);
        if (!s) return json(res, 404, {});
        if ('pays' in b) s.pays = b.pays;
        if ('shorts' in b) s.shorts = b.shorts;
        if ('list' in b) s.list = b.list;
        if ('title' in b && b.title) s.title = b.title;
        return json(res, 200, { ok: true });
      }
      if (p === '/api/consume/poll-all') return json(res, 200, { ok: true, polled: subs.length });
      if (p === '/api/feed') return json(res, 200, { items: [], signals: [], proposals: [], portalItems: [], consumeItems: items.filter(unread).map(card), receipts: [], bankPending: [], badge: 0 });
      if (p === '/api/manifest/operations/receipts') return json(res, 200, { receipts: [], total: 0 });
      if (p === '/api/terminal/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' }); return res.end('retry: 86400000\n\n'); }
      const shell = { '/api/feed/badge': { count: 0 }, '/api/tasks': { outstanding: [], assignees: {}, counts: { tasks: 0 } }, '/api/goals': { areas: [] }, '/api/aion': { backlog: [] }, '/api/properties': { properties: [], deals: [], templates: [], holdings: {} }, '/api/re/backlog': { items: [], goalsArea: null }, '/api/settings/connections': { rows: [] }, '/api/chat/inbox': { roster: { agents: [] }, agents: {}, spirits: { sessions: [] }, terminal: { sessions: [], enabled: false } } };
      if (req.method === 'GET' && shell[p]) return json(res, 200, shell[p]);
      return json(res, 404, {});
    }
    const file = path.join(web, p === '/' ? 'index.html' : p);
    if (!file.startsWith(web) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) { res.writeHead(404); return res.end(); }
    res.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream' }); fs.createReadStream(file).pipe(res);
  });
  return { server, log, items, later, subs };
}
module.exports = { makeFeedStub };
