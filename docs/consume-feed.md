# CONSUME — subscribed reading, and the public curation feed

The lane that replaces newsletter email: manifest polls the writing you follow,
you read it in the FEED, and one button promotes a piece to a public feed you
can share.

| | |
|---|---|
| Private surface | FEED → **CONSUME** view (and a capped strip in INBOX) |
| Public surface | `GET /feed.xml` + a plain index, on its own loopback port |
| Subscriptions | `extrinsic/feeds.md` in the vault — hand-editable in Obsidian |
| Curated items | `extrinsic/<title>.md`, `categories: [articles]` + `curated:` |
| Caches | `<dataDir>/consume/` — disposable, rebuildable by re-polling |
| X token | `<dataDir>/consume/x-creds`, 0600 (or `MANIFEST_PORTAL_X_TOKEN`) |

## Following something

In FEED → CONSUME → **MANAGE**, paste any of:

- a feed URL (`https://blog.example/feed.xml`)
- a site address (`blog.example`) — the feed is auto-discovered from the page's
  `<link rel="alternate">`
- a Substack URL — `/feed` is appended automatically
- `@handle` — an X account (needs a bearer token; see below)

The optional second box is a **group** (`essays`, `ai`, …), which becomes a
`## heading` in `extrinsic/feeds.md` and a filter chip in the view.

### What happens when you follow something

**Subscribing does not fill your queue.** Everything the feed has already
published is *archived* — kept, browsable and searchable, but never counted as
unread. Only posts published after you subscribed arrive as unread.

So a brand-new subscription reading `0 unread · 14 archived` is correct, not
broken. Following WIRED would otherwise drop fifty articles on you at once.

Those 14 are not marked "read" — that would be a lie, since you never opened
them. They carry their own *archived* state and say so on the card.

### Finding something in the archive

Two ways, both in FEED → CONSUME:

- **Per feed** — open MANAGE and click a subscription's name. That feed's whole
  history opens, with a banner naming it and an `× all feeds` way out.
- **Search** — the box in the CONSUME header matches titles, excerpts, authors
  and sources across every feed, case-insensitively. (Article *bodies* are
  stored separately and are not searched.)

Anything out of the queue — archived or read — carries **→ unread**, which puts
it back at the top of your reading list. A later poll will not undo that.

Dismissed items are the exception: they are terminal and never appear in the
archive or in search.

### Editing by hand

`extrinsic/feeds.md` is a normal vault note under the fixpoint guarantee — edit
it in Obsidian and the app preserves what it does not recognize:

```markdown
## essays
- Astral Codex Ten [id:: acx] [kind:: rss] [url:: https://…/feed] [mirror:: full]
- Melissa [id:: melissa] [kind:: x] [handle:: melissa] [min-chars:: 350] [tag:: favourite]
```

`[tag:: favourite]` is not a field this build knows, and it will still be there
after you rename the subscription in the UI. A line with no `[id::]` works too —
the app stamps one on its next write.

## Curating

**→ CURATE** on a card (or at the end of the reader, where the decision
actually happens) writes a note into `extrinsic/` and adds it to the public
feed. The optional one-line note becomes the `<description>` — it is the reason
someone would subscribe to your feed rather than to the sources directly.

Two guarantees worth knowing:

- **Re-curating never clobbers your writing.** If you have added your own
  thoughts under the mirrored article, a second CURATE updates the metadata
  lines only.
- **Un-curating never deletes.** It clears the `curated:` field; the note stays
  as your archive and the feed stops selecting it.

Per subscription, `[mirror:: full]` (default) carries the whole body into the
public feed and `[mirror:: excerpt]` carries only an excerpt and the link. If a
writer ever asks you not to mirror them, change that one field.

## Turning on the public feed

The feed is **opt-in**. It does not exist until you set a port — a public
listener must never appear because a binary was upgraded.

1. In `config.json`:

   ```json
   "consume": {
     "publicPort": 7780,
     "feedTitle": "reading",
     "feedURL": "https://reading.example.com",
     "description": "What I'm paying attention to."
   }
   ```

   `feedURL` is the public address, used for the channel link and the
   `atom:link rel="self"`. Setting `publicPort` back to `0` disables the whole
   public surface; the private lane is unaffected.

2. Restart manifest. It prints:

   ```
   curation feed (PUBLIC) → http://127.0.0.1:7780/feed.xml
   ```

3. **Operator step, out of band** (the same two steps behind
   `portal.ooda.group` — no tunnel config lives in this repo): add an ingress
   rule to the metis cloudflared tunnel mapping your hostname →
   `127.0.0.1:7780`, and add the DNS record in Cloudflare.

Verify from outside: `curl https://<host>/feed.xml`, then subscribe to it in a
real reader.

### What can and cannot be served there

The handler is constructed holding one interface with one method
(`consume.CuratedFeed.Entries()`) and no reference to the server, the vault, or
the item cache. What it returns is only extrinsic notes that declare
`categories: [articles]` **and** carry a `curated:` date. Reading something,
saving a book, or writing a private research note can never reach it — that is
asserted by `TestPublicFeedServesOnlyCuratedItems`, which fills a vault with
private notes and checks every one of them is unreachable.

The index page is served `X-Robots-Tag: noindex` so a mirror never competes
with its source in search results.

## Following X accounts

X has no free API tier for new developers, and no flat-rate plan. It is
pay-per-use with **no minimum spend**: about **$0.005 per post returned** plus
$0.010 for the one-time handle→ID lookup. Five accounts at a few posts a day is
roughly **$4/month**.

1. X developer console → Keys and tokens → **Bearer Token**.
2. Paste it in SPIRITS → Settings → **Portals** → *X (reading)*.
3. Follow accounts with `@handle` in the CONSUME manage panel.

Only original posts over `min-chars` (default 350) are kept — retweets and
replies are excluded before they are ever requested, and short remarks are
filtered out. Long-form posts are read from `note_tweet`, so nothing is
truncated at 280 characters.

**Why the cost discipline is in the code, not just the docs:** billing is per
post *returned*, so `since_id` is mandatory after the first poll, the first
poll is capped at 20 posts rather than 100, and the cursor advances even when
every post was filtered out. Three tests assert exactly those properties.
The Portals panel's *poll* button deliberately does **not** make a live call
for this row — it shows what the last real poll found.

## When something goes wrong

**A feed stops updating.** Open MANAGE: each row has a status dot, and a
degraded one shows the reason inline (a 404, a parse failure, an expired
token). A failed poll never empties the lane — the previous items stay, because
no data is not the same as no news.

**A feed will not subscribe.** The site may not advertise a feed. Find the feed
URL yourself and paste that directly.

**An item looks wrong or unreadable.** Bodies are allowlist-sanitized
server-side at poll time; a publisher wrapping its prose in unusual markup can
lose formatting. The original link is always on the card.

**The public feed is missing something you curated.** Check the note still has
its `curated:` field. If `<dataDir>` was wiped, entries survive with their
metadata and note but degrade to excerpt-and-link until the item is re-polled —
losing a cache never drops something you published.

## Rollback

Three independent switches, in increasing order of severity:

1. `"publicPort": 0` — the public feed disappears, the reading lane keeps
   working.
2. Remove the `consume-feeds` / `consume-curate` capability grants in
   `main.go` — the lane still polls and reads, but writing to the vault fails
   loudly instead of silently doing nothing.
3. Drop `r.Register(consumeSource{s})` in `server/attention.go` — the kind
   disappears from the FEED entirely.

Nothing already written to `extrinsic/` is touched by any of them.
