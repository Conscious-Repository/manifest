# Feature Add — Funder Context & Fundraising CRM

> **Revised after the vault audit** (`vault-audit-and-revised-recs.md`). Your
> funders are largely already `categories: [people]` notes with `alias:` and
> firm links, so most of this is rendering existing structure, not new data entry.

A dashboard surface that, because it sits on your whole Obsidian universe, gives
you on-demand context about any current or potential funder: who they are, your
relationship path, when you last spoke, available transcripts, and a synthesized
brief — all pulled from your vault (plus read-only live sources). It slots under
your Aion "Close Series A" goal as the operational layer beneath it.

---

## 0. The non-negotiable rule

**AI never writes to the vault.** This feature is read-and-surface only. Any
brief, summary, timeline, or "last spoke" value the AI computes is rendered in
the UI and is *ephemeral* — never written back to a note. You (and only you)
author content, in Obsidian or through explicit dashboard forms. If you want to
keep an AI-generated brief, you copy it in yourself. This rule is enforced at the
architecture level (see the companion doc, §2).

---

## 1. What it does

Three views, one entity model.

### 1a. Funder context page (the headline)
Open any funder → a single page that aggregates:

- **Identity & pipeline:** firm, the warm-contact path, interest level / stage,
  next step — from your CRM (the sheet, or its vault equivalent; §3).
- **Relationship timeline:** every interaction with that person/firm — meetings,
  transcripts, notes that mention them, calendar events, email threads — sorted
  newest-first. **"Last spoke" is computed** as the most recent of these, not
  read from a freeform cell.
- **Transcripts:** direct links to any transcript/meeting notes, with source
  (Granola, manual, etc.) and date.
- **Connections:** who intro'd them, shared contacts, other funders reachable
  through them (from the vault's link graph).
- **On-demand brief:** a button that runs a **read-only** AI synthesis over all
  the above → an ephemeral "what you need to know before this call" brief. Never
  saved.

### 1b. Pipeline view
The fundraising sheet rendered as a board or table in the dashboard, grouped by
**Interest Level** (Closed · High · Medium · Low · Pass · Untriaged). You edit it
through the UI (your writes, not AI's). Each card links to the funder context
page. Next-step text can be promoted into a dated task on your manifest.

### 1c. Quick lookup
A command-bar: type a name → instant context card (last spoke, stage, next step,
latest transcript), even mid-conversation.

---

## 2. The data model (mapped from your sheet)

Your columns map cleanly onto two entity types plus a derived interaction stream:

| Sheet column | Goes to | Notes |
| --- | --- | --- |
| Firm | `firm` entity | sometimes blank (solo angel) → person is the entity |
| Warm Contact | relationship edge | the intro path / point person |
| Interest Level | pipeline `stage` | Closed/High/Medium/Low/Pass/blank |
| Next Step | pipeline `next_step` | promotable to a manifest task |
| Last Touchpoint | **ignored for "last spoke"** | freeform; use derived timeline instead |
| Notes | pipeline `notes` | your candid notes — confidential |

The **interaction timeline** is *not* in the sheet — it's derived from the vault
+ connectors (see companion doc). This join is the whole value: the sheet says
*where a deal stands*; the vault says *what actually happened between you*.

**Firm resolution:** you have no firm category today and don't need one. The
engine treats a firm as a note your `people` notes link to — so `1517`,
`Long Journey` resolve now from the graph — and will automatically prefer a
declared `categories: [firm]` (or `org`/`fund`) the instant you ever add one. No
restructuring required, and it upgrades itself later. (See DB doc §3 + §5.)

> Privacy: this data is sensitive (candid firm assessments, pass quotes,
> commitments). Treat funder data as confidential — local-only, no syncing to
> third parties, and the AI-read-only rule keeps it out of any model's writes.

---

## 3. Pipeline source of truth — two paths

**Option A — Keep the Google Sheet, read it live (recommended for v1).**
The dashboard reads the sheet read-only via the Google connector and overlays
vault context. Zero disruption; you keep editing the sheet you already trust.
Downside: the sheet isn't part of the Obsidian DB or its version history.

**Option B — Graduate the CRM into the vault.**
Your people are already `categories: [people]` notes (89+), and firms are already
notes (`[[1517]]`). Option B just adds pipeline fields to the relevant firm/person
note's frontmatter, edited through the dashboard UI. Now it's fully part of the
Obsidian DB, version-controlled, and joinable without a second system.
*Migration is user-driven* — a one-time import you confirm, never AI-authored
content.

Recommendation: **A now, B when the entity/index layer (companion doc) is solid.**
Build the funder page to read from an abstraction so the source can swap without
UI changes.

---

## 4. Where "last spoke / transcripts" actually comes from

Read-only sources, merged by the index, in rough priority:

1. **Vault meeting/transcript notes** — your `categories: [sync]` /
   `[first_meetings]` notes and dated transcripts, with `[[person]]` links in the
   body (the durable, primary source; attendees read from the links).
2. **Granola** — live transcripts via the connector; matched to people by
   attendee. (Granola can also export to Obsidian via its own export — that's a
   user/tool action, not AI writing.)
3. **Google Calendar** — past events with the person = touchpoints (already in
   the plan).
4. **Gmail** — threads with the person's email = touchpoints (read-only, later).
5. **Plain mentions** — any `[[wikilink]]` or name match in any note (fuzzy
   fallback when no structured metadata exists yet).

"Last spoke" = the max date across these. Transcripts list = items from 1–2 that
carry a transcript body/source.

---

## 5. Fit with the existing scope

- **Goal spine:** this lives under Aion → 90-day "Close Series A." Next-steps
  become manifest tasks (`owner:: me`), so fundraising work flows into your day.
- **Convention over location:** funder pages are built by *querying metadata*,
  not by a funders folder — consistent with M0. A transcript counts wherever it
  lives, as long as it carries (or can be fuzzily matched to) the person.
- **Depends on:** the Obsidian-as-DB index layer (companion doc). That index is
  the prerequisite; this feature is its first killer app.

### Suggested new milestone

**M6 — People / CRM / Funder context** (after M0 index + at least Granola read):
1. Entity + alias resolution for people/firms (read from vault; §2 companion).
2. Interaction timeline query + "last spoke" + transcript list.
3. Funder context page UI + quick-lookup command bar.
4. Pipeline view (Option A: read the sheet live), cards → context pages.
5. On-demand brief (read-only, ephemeral) over the timeline.
6. (Later) Option B migration; Gmail touchpoints; next-step → task promotion.

---

## 6. Open decisions

1. **Pipeline source:** keep the Google Sheet (A) or migrate into the vault (B)?
2. **Transcripts:** are your meeting transcripts already in the vault, only in
   Granola, or both? Do you want Granola read live, or rely on your own exports?
3. **Identity map:** OK to maintain `type: person` notes with `aliases:` for your
   ~50 funders (one-time, manual) so nickname/handle resolution is reliable?
4. **Brief scope:** should the on-demand brief ever be allowed to read your
   private candid notes, or only meeting/transcript content?
