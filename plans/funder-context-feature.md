# Feature Add — Funder Context & Fundraising CRM (v2)

> **Revised after the vault audit** (`vault-audit-and-revised-recs.md`) **and the
> excalibur harness plan** (`excalibur-path-plan.md`). Your funders are largely
> already `categories: [people]` notes with `alias:` and firm links, so most of
> this is rendering existing structure, not new data entry.

**v2 changes:** write rule updated to **designated vault folders** (§0); the
on-demand brief is now **a cast run by a spirit through the excalibur engine**, not
ad-hoc dashboard AI (§1a); briefs **persist with provenance** to the designated
folder (§1a); pipeline source = **vault frontmatter (Option B), sheet retired after
a one-time user-confirmed import** (§3); transcripts = **vault + Granola live,
deduped** (§4); AI-authored notes are **excluded from interaction timelines** (§4).

A dashboard surface that, because it sits on your whole Obsidian universe, gives
you on-demand context about any current or potential funder: who they are, your
relationship path, when you last spoke, available transcripts, and a synthesized
brief. It slots under your Aion "Series A" Rock as the operational layer beneath
it — and it is the **first killer app of the index layer**
(`obsidian-as-database.md`), which is the kernel the whole personal OS queries.

---

## 0. The write rule (revised)

**The vault has exactly one AI-writable region: the designated folder —
`Agents/`** (already exists at vault root; rename if desired, it's config). Warding
(excalibur §4) allow-lists `vault/Agents/**` for spirit writes; **every other vault
path fails closed**, and the warden ritual audits that the allow-list never widens.
Everything outside `Agents/` is authored only by you — in Obsidian, or through
explicit dashboard UI actions (which are *your* writes, e.g. CRM edits, §3).

Operational surfaces (feed queue, approvals, run reports) stay in the sibling
`excalibur/artifacts/` tree — machine traffic doesn't belong in the brain log.
`Agents/` is for **keepable knowledge** the AI produces: funder briefs, research
digests — content you want versioned, linkable, and visible in Obsidian's graph.

Honesty note: generating a brief sends its source content (including candid pass
quotes and commitments) through the model provider at inference time. Storage
stays local; transit doesn't. Accepted — AI reads the full DB by design.

---

## 1. What it does

Three views, one entity model.

### 1a. Funder context page (the headline)

Open any funder → a single page that aggregates:

- **Identity & pipeline:** firm, the warm-contact path, interest level / stage,
  next step — from frontmatter on the person/firm note (§3).
- **Relationship timeline:** every interaction with that person/firm — meetings,
  transcripts, notes that mention them, calendar events, email threads — sorted
  newest-first. **"Last spoke" is computed** as the most recent of these, never
  read from a freeform field.
- **Transcripts:** direct links to any transcript/meeting notes, with source
  (vault, Granola) and date.
- **Connections:** who intro'd them, shared contacts, other funders reachable
  through them (from the vault's link graph).
- **On-demand brief:** a button that requests a run from the excalibur engine
  (the §7.5 "run now" pattern) — a spirit with vault-read + the index reads the
  timeline and synthesizes "what you need to know before this call." **Not a
  second AI pathway in the dashboard:** because it goes through the engine, it
  inherits charge metering, warding, and a legible run report for free.
  **The brief persists** to `Agents/briefs/<person-slug>/<date>.md` with
  provenance frontmatter: `spirit`, `generated` (timestamp), `sources` (the notes
  and connector items it read), `charge`. Over time this builds a longitudinal
  record per funder. Promoting content into the person's own note remains your
  action.

### 1b. Pipeline view

The CRM rendered as a board or table, grouped by **Interest Level** (Closed ·
High · Medium · Low · Pass · Untriaged), read from note frontmatter (§3). You edit
through the UI (your writes, to the person/firm notes — allowed, they're explicit
dashboard actions). Each card links to the funder context page. Next-step text can
be promoted into a dated task on your manifest.

### 1c. Quick lookup

A command-bar: type a name → instant context card (last spoke, stage, next step,
latest transcript), even mid-conversation.

---

## 2. The data model (imported from your sheet, then vault-native)

The sheet's columns map onto frontmatter fields set by the one-time import (§3):

| Sheet column | Becomes | Notes |
| --- | --- | --- |
| Firm | link to firm note (`firm:: [[1517]]`) | blank (solo angel) → person is the entity |
| Warm Contact | `warm-contact:: [[Person]]` | the intro path / point person |
| Interest Level | `stage:` | Closed/High/Medium/Low/Pass/blank → Untriaged |
| Next Step | `next-step:` | promotable to a manifest task |
| Last Touchpoint | **dropped** | freeform; the derived timeline replaces it |
| Notes | `pipeline-notes:` | your candid notes — confidential |

The **interaction timeline** is derived by the index (§4), never stored in
frontmatter. This join is the whole value: frontmatter says *where a deal
stands*; the graph says *what actually happened between you*.

**Firm resolution:** unchanged from the audit — no firm category required. The
engine treats a firm as a note your `people` notes link to, with a config-driven
role map (`firm/org/company/fund/vc` pre-registered) picked up automatically if
you ever tag one. (See `obsidian-as-database.md` §3 + §5.)

> Privacy: funder data is sensitive. Local-only storage, no syncing to third
> parties; the designated-folder warding keeps AI writes contained (§0).

---

## 3. Pipeline source of truth — vault (Option B, decided)

**The CRM lives in the vault.** Pipeline fields sit in frontmatter on the
person/firm notes, edited through the dashboard UI. Fully part of the Obsidian
DB: version-controlled, graph-joinable, no second system.

**One-time import (user-confirmed, not AI-authored):** a dashboard wizard reads
the Google Sheet once (read-only connector), previews the exact frontmatter diff
per note (creating person notes only where none exists — flagged for your
review), and writes **only on your confirm** — an explicit dashboard action,
i.e. your write. After the import, the sheet is retired.

**Cross-plan note:** `goals-build-spec.md` Phase 2 lists "the fundraising sheet
(read-only)" as an EA signal source — after this import, the EA reads the vault
CRM (via the index) instead. Update that reference when goals Phase 2 kicks off.

Timing risk, accepted deliberately: the migration lands mid-raise. Mitigation is
the wizard's per-note preview + the vault's git history (instant revert).

---

## 4. Where "last spoke / transcripts" actually comes from

Read-only sources, merged by the index, in rough priority:

1. **Vault meeting/transcript notes** — `categories: [sync]` / `[first_meetings]`
   and dated transcripts, with `[[person]]` links in the body (the durable,
   primary source; attendees read from the links).
2. **Granola, live** (decided) — transcripts via the connector, matched to people
   by attendee, **deduped against vault notes by date + attendees** (a meeting you
   exported counts once). Granola's own Obsidian export remains a user/tool
   action, not AI writing.
3. **Google Calendar** — past events with the person = touchpoints.
4. **Gmail** — threads with the person's email = touchpoints (read-only, later).
5. **Plain mentions** — any `[[wikilink]]` or name match in any note (fuzzy
   fallback).

"Last spoke" = the max date across these. Transcripts list = items from 1–2 that
carry a transcript body/source.

**Exclusion rule:** notes under the designated AI folder (`Agents/**`) are indexed
and searchable but **never count as interactions** — a generated brief about Fred
must not update "last spoke with Fred." The index tags designated-folder content
as AI-authored and timeline queries filter it.

---

## 5. Fit with the existing scope

- **Goal spine:** lives under Aion → "Series A" Rock. Next-steps become manifest
  tasks (`owner:: me`), so fundraising work flows into your day.
- **Convention over location:** funder pages are built by *querying metadata*,
  not a funders folder. A transcript counts wherever it lives.
- **Depends on:** the index layer (`obsidian-as-database.md`) — now in `plans/`
  (it was mis-filed in archive). The index is the personal-OS kernel; this
  feature is its first killer app and its acceptance test.

### Suggested milestone — M6: People / CRM / Funder context

(after the index layer + Granola read)

1. Entity + alias resolution for people/firms (read from vault).
2. Interaction timeline query + "last spoke" + transcript list (with the
   AI-authored exclusion and Granola dedupe).
3. Funder context page UI + quick-lookup command bar.
4. **CRM import wizard** (sheet → frontmatter, per-note confirm) + pipeline view.
5. On-demand brief as an engine-run cast, persisted to `Agents/briefs/` with
   provenance.
6. (Later) Gmail touchpoints; next-step → task promotion.

---

## 6. Decisions (resolved)

1. **Pipeline source:** vault frontmatter (Option B) via one-time user-confirmed
   import; sheet retired.
2. **Transcripts:** vault + Granola live, deduped by date + attendees.
3. **Identity map:** mostly already exists (`alias:` on person notes, per the
   audit); gaps surfaced by the Vocabulary view, filled by you.
4. **Brief scope:** the brief may read the full DB, candid notes included.
5. **Brief persistence:** always saved, with provenance, to the designated
   folder.
6. **Designated AI-writable vault folder:** `Agents/` (config-renamable);
   everything else fails closed.
