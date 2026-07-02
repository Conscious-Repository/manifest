# Obsidian as a Database — Design & Migration Recs

How to treat your existing wealth of Obsidian content as a backend the dashboard
can query for insight — **without a big reorg**, and **without ever letting AI
write to it**. This is the layer the funder-context feature (and most future
features) sits on.

> **Revised after auditing your vault** (see `vault-audit-and-revised-recs.md`).
> You already do this: `categories:` is your type system, `alias:` your identity
> map, and your `_index_*` notes already query it with Dataview. The conventions
> below now reflect your real vault — `categories`, not a new `type:` — so there
> is essentially nothing to adopt.

---

## 1. The mental shift: folders → a queryable metadata graph

Today you organize by *location* (`/intrinsic`, etc.). A database doesn't care
where a row physically sits — it cares about *what the row is* and *what it links
to*. So stop thinking "which folder," start thinking:

- **Entities** — the nouns you query (people, firms, projects, meetings).
- **Attributes** — frontmatter fields on a note (`type`, `date`, `aliases`).
- **Relationships** — `[[wikilinks]]` and typed fields (`attendees::`,
  `firm::`) that connect notes.

The vault already *is* a graph (links). We add a thin, optional layer of
attributes so the graph becomes *queryable*, and we build an index that reads it.
Crucially, this is **convention over location** (same principle as the dashboard
core): a note counts as a "meeting with Justin" because of its metadata/links,
not because it's filed in a particular folder.

---

## 2. The write boundary (this is foundational, not a footnote)

> Revised July 2026 to the **designated-folder** rule (see
> `vault-audit-and-revised-recs.md` §5, the authoritative statement).

- **Outside the designated folder, the vault is written by exactly two actors:
  you, in Obsidian; and you, through explicit dashboard UI actions.** That's it.
- **AI reads the full vault but writes only to the designated folder
  (`Agents/`, config-renamable).** Enforced by excalibur warding: `Agents/**` is
  the only vault path in any spirit's write allow-list; everything else fails
  closed, audited by the warden.
- **Keepable AI outputs persist to `Agents/` with provenance** (spirit, sources,
  date) — briefs, digests. Operational machine surfaces (feed, approvals, run
  reports) stay in the sibling `excalibur/artifacts/` tree, out of the brain log.
- **The index is a derived projection, not the DB.** The Go app builds a SQLite
  index *from* your markdown to make queries fast. It is rebuildable from the
  vault at any time and is never authoritative. Deleting it changes nothing.
  It tags `Agents/**` content as AI-authored and excludes it from interaction
  timelines / "last spoke".

This means the "database" is your hand-authored markdown plus one clearly-fenced
AI shelf; the AI is a librarian with read access to everything and write access
to its own shelf only — never a co-author of your notes.

---

## 3. Conventions — you already have them (`categories`, not `type`)

These are not new asks; they are what your vault already does. The query layer
keys off them and degrades gracefully where they're absent (§6).

**Person note** — already your norm (89+ at root):
```yaml
---
categories: [people]
alias: [RJ, "@justinmares"]    # you already use alias / aliases
---
- General Partner at [[Long Journey]]   # body: facts + [[links]] to firms/people
```

**Firm/org note — no category today, and you don't need to make one now.** The
engine resolves an **org role** two ways, so it works today *and* upgrades itself
the moment you ever declare a category:

- **Declared (future-proof):** the role map (below) pre-registers candidate
  category values — `firm`, `org`, `company`, `fund`, `vc`. The instant you tag
  any note `categories: [firm]`, it's recognized as an org with zero code change.
- **Inferred (works now):** until then, a firm = a note that one or more
  `people` notes link to and that isn't itself a person/interaction. That already
  resolves `[[1517]]`, `[[Long Journey]]`, etc. from your link graph — no tagging.

Declared always beats inferred, and inferred entities are labelled as such in the
UI, so you can adopt a `firm` category later (or never) and everything stays
connected dynamically. CRM fields (funder doc Option B) live in that note's
frontmatter if/when you want them in-vault; otherwise the pipeline stays in the
sheet.

**Meeting / transcript note** — already `categories: [sync]` or
`[first_meetings]`, dated, with `[[person]]` links in the body. The dashboard
reads attendees from those links; no separate `attendees::` field is required.

**Mentions** — any `[[wikilink]]` to a person/firm in any note (especially your
daily notes) is a soft interaction, even with no frontmatter. Your graph is
already dense — which is exactly what makes "no reorg" real.

The whole vocabulary is what you already use: `categories`, `alias`/`aliases`,
`date`, and `[[links]]`. Nothing new to adopt.

---

## 4. Entity resolution — the genuinely hard part

Your data refers to people as "Justin Mares," "RJ," a Twitter handle, or just a
first name. A useful funder page needs all of these to resolve to one entity.

- **Alias map.** The `aliases:` list on a person note is the source of truth for
  identity. The index builds `alias → entity` from these.
- **Fuzzy fallback.** For names with no person note yet, the index does
  best-effort matching (exact, then first-name + firm context, then full-text
  mention) and labels them "unresolved."
- **You confirm merges.** When the index is unsure ("is 'RJ' = Justin Mares?"),
  the dashboard *asks you*; you click to confirm, which writes the alias **as
  your action through the UI** — not AI authoring content. (You can also just add
  aliases in Obsidian.) If you'd rather the app never prompt, it stays purely
  read-only and shows "unresolved" instead.
- **Disambiguation.** Two "Michael"s? The firm/role context and link graph keep
  them apart; ambiguous cases surface rather than guess.

Resolving your ~50 funders is a one-time afternoon of adding `aliases:` — and it
makes every downstream query reliable.

---

## 5. The query / index layer (Dataview-as-a-service, in Go, read-only)

The Go app maintains a SQLite projection of the vault:

- **Tables:** `entities(id, type, aliases…)`, `notes(path, type, date, frontmatter)`,
  `links(src, dst)`, and an **FTS5 full-text** index of note bodies.
- **Built by:** the same vault scanner from M0 — parse frontmatter, inline
  fields, wikilinks; index full text. Rebuilt incrementally on file change
  (`fsnotify`); fully rebuildable from scratch.
- **Entity roles = a config map (category → role), not hardcoded.** e.g.
  `people → person`, `sync, first_meetings → interaction`, `aion, parium →
  project`, `firm, org, company, fund, vc → org` (pre-registered, unused today).
  Adding a category to the map — or tagging a note with a pre-registered one —
  changes behavior with no code change. The `org` role also has the link-graph
  fallback (§3) so it works before any category exists.
- **Category matching is exact; inconsistencies are surfaced, never rewritten.**
  Per your call, the engine does **not** silently normalize `sync`↔`syncs` or
  `people`↔`person`. Instead it ships a **Vocabulary view**: it clusters
  near-duplicate category values (by stem/edit-distance) with counts and example
  notes, so *you* standardize them in Obsidian. The engine only reads; you write.
  (If you ever want a synonym treated as equal without renaming notes, you can
  declare it in config yourself — your authored rule, not an AI guess.)
- **Query API (read-only):**
  - `interactions(entity)` → notes linking the entity, by date desc
  - `lastSpoke(entity)` → max date across interactions + calendar + Granola
  - `transcripts(entity)` → interactions with a transcript body/source
  - `mentions(text)` → FTS fallback for unstructured matches
  - `context(entity)` → the bundle the funder page renders
- **Never writes to the vault.** It only reads `.md` and writes its own cache.

This is exactly how Obsidian's Dataview works (a queryable index over markdown) —
we just run it in the app, headless and read-only, so the dashboard (and future
features) can ask structured questions.

---

## 6. Tolerant querying = no reorg required

The layer is designed to work on the vault you have *today* and improve as you
add structure:

- **Tier 0 (zero structure):** pure full-text + existing wikilinks. A funder page
  already shows "notes mentioning this name," plus calendar/Granola touchpoints.
  Rough, but useful day one.
- **Tier 1 (aliases added):** nickname/handle resolution becomes reliable;
  "last spoke" stops missing interactions filed under a nickname.
- **Tier 2 (meeting frontmatter on new notes):** clean, dated, attendee-accurate
  timelines and transcript lists.

You never migrate files or restructure folders. You *enrich* incrementally, top
funders first, and the dashboard gets sharper as you go. Old notes keep working
via full-text; you're never blocked on a backfill.

---

## 7. Live connectors as read-only supplements

The vault is primary, but these fill gaps without writing to it:

- **Granola** — live transcripts + attendees (your richest "what was said"
  source). Read live, or rely on your own Granola→Obsidian exports.
- **Google Calendar** — past/future events as touchpoints and "last/next spoke."
- **Gmail** — email threads as touchpoints (later).

None of them push AI-authored content into your vault. If you want a transcript
*in* the vault, that's a deterministic export you run — not the AI.

---

## 8. The AI-suggestion boundary (optional, and off-able)

A subtle line worth drawing explicitly, given your rule:

- **Allowed:** AI *reads* and *surfaces* — "these 4 notes seem to mention this
  funder," "here's a brief from your last 3 calls." Display only.
- **Allowed, but it's you writing:** the UI offering a one-click "add this alias
  / link this note," where *you* commit the write. The model proposes; the human
  authors. (Think of it as autocomplete you accept, not an agent acting.)
- **Never:** the app writing notes, summaries, or metadata on its own, in the
  background, or in bulk.
- **Escape hatch:** a "purely read-only, no suggestions" mode that disables even
  the one-click proposals, if you want zero AI involvement in writes.

If even one-click suggestions feel like too much, default to the escape-hatch
mode; the system loses convenience, not capability.

---

## 9. Risks & edge cases

- **Alias collisions / wrong merges** → always require your confirmation; show
  provenance ("matched because…"); make merges reversible (they're just your
  frontmatter edits).
- **Stale index** → index is disposable; a rebuild command + file-watching keeps
  it honest. Source of truth never drifts because it's the markdown.
- **Privacy** → funder/CRM data is sensitive; keep the index local, and the
  read-only-AI rule keeps candid notes out of any model's writes. Consider a
  per-note `confidential: true` flag the brief generator must respect (§ funder
  doc decision 4).
- **Performance at vault scale** → SQLite + FTS5 handles tens of thousands of
  notes comfortably; incremental indexing avoids full re-scans.

---

## 10. Recommended path

1. Build the **read-only index** (extends M0): frontmatter + links + FTS5.
2. Ship **Tier 0** funder pages immediately (mentions + calendar + Granola).
3. Add **`aliases:`** for your top ~50 funders → Tier 1 resolution.
4. Adopt the **meeting/transcript frontmatter** for new notes (a Templater
   template) → Tier 2 timelines. Backfill only if/when you feel like it.
5. Keep the **write boundary** as a hard architectural invariant the whole way.

## Open decisions

1. Comfortable maintaining `type`/`aliases`/`attendees` frontmatter going
   forward (via a template), or want the system to lean harder on full-text so
   you add *nothing*?
2. One-click AI suggestions for linking/aliasing — on, or strict read-only mode?
3. A `confidential:` flag so certain notes are queryable by you but never fed to
   the brief generator — worth having?
4. Should the index live inside the vault (a hidden folder) or outside it (e.g.
   `~/.config/manifest/index.db`)? (Recommend outside — it's derived, not data.)
