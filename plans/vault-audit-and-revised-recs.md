# Vault Audit & Revised Recommendations

Read this **before** the other artifacts — it corrects them against your actual
vault. Headline: your vault is already a mature, Dataview-driven knowledge graph,
so the dashboard needs **almost no new structure**. The earlier docs proposed a
`type:` convention; you already have a richer one (`categories:`), already query
it as a database, and already link people densely. We read what exists.

---

## 1. What your vault actually is

- **Scale:** ~1,187 notes. Top level: `categories/`, `intrinsic/`, `extrinsic/`,
  `transcript loops/`, `Agents/`, `Manifest/`, `skills/`, `attachments/`, plus
  ~561 loose notes at root.
- **Daily notes:** `intrinsic/YYYY-MM-DD.md` (531 of them). **No frontmatter** —
  they open with `#tags`, then prose, with **dense `[[wikilinks]]` to people**
  (`[[olga sobkiv|Olga]]`, `[[Kenneth Eversole]]`).
- **Classification = `categories:`** (inline YAML list), present on ~80% of
  notes. It is your universal "type" system. Observed values:
  `people, sync, first_meetings, aion, parium, books, papers, essays, talks,
  events, guilds, poetry, research, tools, writing, atoms, index`.
- **You already run the vault as a DB.** The `categories/_index_*` notes are
  Dataview dashboards, e.g. `_index_people` is literally
  `WHERE contains(categories, "people")`; `_index_syncs` is
  `contains(categories, "sync")`.
- **Entity resolution already exists.** `alias:` / `aliases:` frontmatter is
  common (`alias: [Leon]`, `alias: [Iosif]`). People are first-class notes
  (`categories: [people]`, 89+ at root) linked to firms (`[[1517]]`) and to each
  other.
- **Meetings/transcripts:** `categories: [sync]` and `[first_meetings]`. Raw
  transcripts are dated notes (`YYYY-MM-DD <topic>`), and a "granola loop closer"
  digest links them and extracts commitments per person.
- **Projects are categories too:** `aion`, `parium` — so an area/project is just
  `contains(categories, "aion")`.

---

## 2. What changes in the recommendations (the important part)

1. **Drop `type:` entirely. Key off your existing `categories:`.** Every entity
   the dashboard needs already has a category. No new field, no retagging.
2. **The index layer should be Dataview-semantics-compatible**, not a new schema.
   You already think in `contains(categories, X)`; the Go index mirrors exactly
   that (categories, aliases, inline fields, links) so the dashboard renders the
   same graph Dataview already sees — just headless and fast.
3. **Entity resolution is mostly done.** Build the alias map from your existing
   `alias:`/`aliases:` plus person notes; resolve `[[wikilink|display]]`. The
   one-time "add aliases for 50 funders" task from the funder doc largely
   evaporates — most already exist.
4. **"Last spoke" / timelines come free.** A person's interactions = notes that
   link them across `sync`, `first_meetings`, and the daily notes. That graph is
   already dense, so even Tier 0 is strong. No structure to add.
5. **The migration/enrichment path collapses.** The earlier docs framed a
   3-tier enrichment effort. In reality you're already at Tier 1–2. "Migration"
   becomes *optional cleanup of inconsistencies* (below), not new work — exactly
   the "don't make me create a bunch of connections" outcome you asked for.

### Convention mapping: earlier rec → your reality

| Earlier doc said | Your vault already has |
| --- | --- |
| `type: person` | `categories: [people]` |
| `type: meeting` | `categories: [sync]` / `[first_meetings]` |
| `type: firm` | firm notes exist, linked from people (`[[1517]]`); confirm a category or treat firm = note that people link to |
| `attendees:: [[Person]]` | existing `[[wikilinks]]` in the note body |
| `aliases: [...]` | your `alias:` (and some `aliases:`) |
| `type: agent` | keep as-is (new `Agents/` area, not part of legacy notes) |

---

## 3. Tolerances the engine MUST handle (real inconsistencies found)

These are why the index has to be *tolerant*, matching your wish to render off
existing structure rather than forcing cleanup:

- **Singular/plural category values:** `people` (plural) but `sync` (singular),
  etc. **Your call: surface, don't normalize.** The engine matches categories
  exactly and never auto-rewrites; it presents a Vocabulary view of near-duplicate
  values (with counts + examples) so you standardize them yourself in Obsidian.
- **`alias` vs `aliases`:** read both; `alias` (singular) is your more common
  form, though Obsidian's native field is `aliases`.
- **Daily notes have no frontmatter:** classify them by filename
  (`YYYY-MM-DD`) + leading `#tags`, not by `categories`.
- **Aliases are used beyond people** (e.g. concept spelling variants), so the
  alias→note map is general, not person-only.
- **`categories:` is an inline list** (`[people]`, `[books]`) — parse as YAML
  list, occasionally with a redundant body `#tag`.

---

## 4. Revised approach — "render the graph you already have"

- **Index = headless Dataview.** Parse `categories`, `alias`/`aliases`, inline
  fields, and `[[links]]` into the SQLite projection; expose
  `contains(categories, X)`-style queries plus link/backlink traversal and FTS.
  This is the single change that makes both the funder feature and My-Plate fall
  out almost for free.
- **Funder page = a saved query, not a new data model.** A funder is a `people`
  note (optionally a firm note). Their timeline = backlinks from `sync` /
  `first_meetings` / daily notes, sorted by date; "last spoke" = the latest;
  transcripts = linked transcript notes (+ Granola live). The pipeline sheet
  joins on name/alias.
- **My Plate / goals** similarly read existing structure where it exists and only
  add the thin manifest block to daily notes (which already have none — clean).
- **Zero forced reorg.** You enrich only if *you* want sharper results (e.g.
  standardize a category value). The dashboard never requires it and never writes
  it for you.

---

## 5. AI-write rule (resolved)

The old `transcript loops/` "granola loop closer" notes were agent-generated; you
**deleted them**. So the slate is clean and the rule going forward is simple and
absolute: **AI is read-only on the vault.** Claude Code should enforce this as a
hard invariant — the index, the funder briefs, and any future agent only ever
*read* `.md`; writes come exclusively from you (in Obsidian) or from explicit
dashboard UI actions you take. No background or bulk AI writes, ever.

---

## 6. Net effect on the plan

- The build gets **easier**, not harder: less new schema, more "render existing
  graph." The funder feature is mostly UI + queries over data you already keep.
- **Update both companion docs** to be `categories`-based (I've revised their
  convention sections to match this audit).
- **Suggested first index task for Claude Code:** a Dataview-semantics reader
  (categories + aliases + links + FTS) over the whole vault. Validate it by
  reproducing your existing `_index_people` / `_index_syncs` results headlessly —
  if it matches Dataview, the foundation is correct.

### Decisions (resolved)

1. **Firm/org category:** none today, and none required. The engine uses a
   config-driven role map with `firm/org/company/fund/vc` **pre-registered** plus
   a link-graph fallback (a note your `people` notes link to). Firms resolve now
   from the graph; the day you ever tag `categories: [firm]`, it's picked up
   automatically with no code change. (See `obsidian-as-database.md` §3 + §5.)
2. **Normalization:** surface, don't normalize. Exact category matching + a
   Vocabulary view of near-duplicates for *you* to standardize. (§3 above.)
3. **Loop-closer:** deleted. AI is read-only on the vault going forward (§5).

All three are baked into the companion docs — they're ready to hand to Claude
Code as-is.
