# ARCHITECTURE — the manifest operating system

Read this before changing anything. This is the constitution: the invariants
every pass must preserve and the doctrines that decide ambiguous calls. The
plans/ folder holds feature history; this file holds the recorded consensus.
Authority runs: the owner's dated decisions → the newest owner-approved
spec → this file. When a newer owner-approved spec conflicts, this file is
AMENDED (§12) — it never silently overrules a more recent decision.
(Amended 2026-07-28; the original supremacy clause inverted this.)

Manifest is an **operating system for one person's work**, not an app. An
app grows by features; this system grows by *kinds*. The standing metric of
architectural health: the cost of adding the next domain (records + views +
signals for a new area of life) should fall, not rise.

## 1. Identity of the system

* **One user, one writer — forever.** No auth, no roles, no concurrent
  human writers. Every design may assume a single human's intent. (Decided
  2026-07-26. Amended 2026-08-14 — §12: the AION team portal is the one
  bounded exception; the vault, the records, and every other surface keep
  this invariant untouched.)
* **Personal system first; publishable artifact eventually.** Code and docs
  are written so another person could someday run it — no hardcoded
  personal paths outside config — but product polish is never prioritized
  over the owner's daily use.
* The UI is manifest-quiet everywhere: mono labels, hairlines, ghost
  inputs, muted dots, `#265ACC` for live/active. Loud color is reserved
  for nothing.

## 2. State tiers (where a byte may live)

1. **Knowledge zone** (vault, outside `system/`) — the owner's authored
   thought. The app reads; it writes only the user's explicit edits, only
   inside delimited regions or files the user owns (goals.md, to do.md,
   daily blocks) and their app-maintained history peers (`goals
   <quarter>.md`, `to do archive.md`, `.pre-*` backups — append/archive
   sweeps of the owner's own lines, never new content). No AI-authored
   content, ever. (Examples updated 2026-07-28 for the task substrate.)
2. **System zone** (`<vault>/system/`) — structured, app-managed markdown
   records + their sidecars. Hand-editable in Obsidian; the app is a
   co-writer under the fixpoint guarantee (§3).
3. **Derived state** (`dataDir`, default `~/.config/manifest`) — indexes,
   caches, portal state, workbench parking, errand records. Rebuildable or
   operational; never inside the vault; deleting it must never lose
   owner-authored data.
4. **Secrets** (`dataDir/portals/`, tokens) — mode 0600, never in the
   vault, never in the repo, never logged, masked in UI.

A store constructor takes its tier; placing state in the wrong tier is a
bug by definition.

## 3. The record kernel

One package (`record/`) owns the mechanics every domain used to hand-roll:

* the inline-field grammar `[key:: value]` — **one regex in one file**;
* checkbox-outline parsing (depth = meaning) and serialization with the
  **fixpoint guarantee**: parse→emit is byte-identical, so Obsidian hand
  edits and app writes coexist without churn;
* frontmatter, slugs/identity, unknown-field and unknown-line preservation;
* sidecar conventions: `<slug>.ledger.csv`, `<slug>.geo.json`,
  `<slug>.source.json` — markdown for narrative, CSV for money rows,
  JSON for geometry/payloads; geometry and bulk data never go in markdown;
* the watcher/store lifecycle (load, watch, reload, save).

A **domain** (goals, real estate, aion, …) is a *declaration* over the
kernel: record kinds, zone paths, section grammars, recognized fields,
derived rollups, views. Domains contain no parsing regexes and no
serialization code. The kernel's property tests (round-trip stability,
hand-edit readback, unknown preservation) run once, centrally, against
every registered kind's corpus.

Derived values (rollups, %, schedules, bid histories) are **computed, never
stored** — with one deliberate exception (2026-07-28): a derived value MAY
be materialized into an **export/portal contract** consumed outside the
system (e.g. `hard_costs` in source.json, written from Σ stage estimates
because the ooda site engine reads it — decided 2026-07-25). Internal
state stays computed; if only manifest reads it, storing it is a bug.

## 4. Write boundary = permission model

All vault writes flow through `vaultwriter` with a declared
**write-capability**: zone + path pattern + actor (user-action | approved
proposal). One enforcement point, byte-contract tested, with an append-only
audit log in dataDir. Every write is an explicit user action or a
user-approved proposal; agents (EA, spirits) only ever *propose*, through
the approvals inbox. No package writes vault files directly — the
KNOWLEDGE ZONE INCLUDED: goals.md, to do.md, daily notes, and archives
route through the chokepoint under a knowledge-zone capability, no
exemptions (decided 2026-07-28; goals/todos/daily's direct writers predate
the boundary and are scheduled for the kernel-extraction pass).

## 5. Attention taxonomy (closed set)

Everything that wants the owner's attention flows through the FEED as one
of exactly four kinds, each with a declared lifecycle behind a single
`AttentionSource` interface:

* **findings** — spirit/engine items; markdown files; Keep/Discard feeds
  the tune loop; may be saved to vault.
* **signals** — app-derived conditions; virtual; auto-clear on resolution;
  Act/Snooze/Dismiss; never enter kept/discarded. A signal card MAY carry
  domain quick-actions that resolve its condition (e.g. a stale todo's
  Done/Waiting/→issue/Drop) — those are domain writes, not lifecycle
  verbs; auto-clear still governs. (Clarified 2026-07-28.)
* **notices** — externally-sourced portal items; dismiss; expire (14d);
  never enter kept/discarded.
* **receipts** — outcomes of actions the system took (errands); permanent
  audit trail; never expire, never kept/discarded.

Adding a fifth kind requires amending this file, deliberately.

The FEED also renders a **proposals lane** — the §4 approvals inbox
surfaced for authorization. It is an authorization queue, not an attention
kind; the closed set above stays four. (Named 2026-07-28.)

## 6. Portals (drivers)

Every external service is a **portal**: `source` (polls in, cache under
dataDir, cursor-based, failure ≠ empty) or `effector` (acts out, always
behind explicit user intent, always leaving a receipt). LLM conduits are
portals too (excalibur canon: `portal:: claude-sub`). The vault and the
engine are home ground, never portals. Credentials live in the secrets
tier; effectors that hold their own credentials (Aside) store nothing here.

## 7. Daemon doctrine (two schedulers, never three)

* **Spirits** (excalibur engine) think on cadence: rituals, agentic work,
  metered spend, proposals.
* **Pollers** (app tickers) fetch on interval: mechanical, unmetered,
  cache-writing.
  External tools' own schedulers (e.g. Aside routines) are deliberately
  unused — scheduling stays sovereign.

The engine is a **separate process** related to the app by file contracts
(feed dir, approvals store, artifacts). Microkernel posture: neither links
the other; the contract is the interface. Keep it that way.

## 8. Topology (two machines, one human)

**Amended 2026-08-11 (big-change Phases 1–3, owner decisions 1–4): the
cockpit is the browser.** Manifest and the excalibur engine run on metis
(the engine room); laptop and phone reach the dashboard over Tailscale
(https://metis.tail8f89de.ts.net). The laptop manifest stays buildable as
dev/fallback, not a daily driver. Two media, two repos: the **vault**
(consciousrepo — the human's permanent medium) and the **harnesses repo**
(the agents' working medium, with decay); the excalibur tree left the
vault entirely. Both sync hands-free via `cmd/manifest-sync` (git; watch →
debounce → commit → pull --rebase → push; conflicts STOP/PARK/MARK into a
FEED signal). Conflict doctrine: the human is the mutex. Interactive
writes happen where the owner is typing and converge through sync; the
engine writes only its own territories (harness artifacts, dataDir);
promotion from harness to vault crosses only through the human gates
(Save-to-vault, approvals Confirm). dataDir is per-machine and never
syncs. Deployment is a repo artifact (`deploy/`, `make deploy`),
operator-owned.

## 9. Identity doctrine

Every named thing is a note; its kind is a category; references are
wikilinks; typeahead-with-create is one shared component. But the
**personal people layer (contacts) is firewalled from business records**
(contractors, entities): separate kinds, separate namespaces, no automatic
unification. A contractor may carry an optional explicit link to a person
note; nothing infers it. (Decided 2026-07-26.)

## 10. Settings doctrine

Two levels, deliberately:

* **SYSTEM level** — portals, external connections, OS function (accounts,
  cache management, audit log). Lives with the SPIRITS/system surfaces.
* **Domain level** — each domain owns settings that are really domain data
  (real-estate entities and admin categories, aion program config). Lives
  as a sub-tab of that domain.
  Rule of thumb: if it's a record, it's domain-level; if it's a conduit or
  a machine concern, it's SYSTEM.

## 11. Userland

A tab is an app: one JS module per tab over the kernel's HTTP projections,
composed from the shared component library (ghost input, quiet dot,
mono-label row, typeahead, hairline table, money slot). No new UI idiom
without touching the library first. No frameworks; vanilla stays.

## 12. Amendments

This file changes only by deliberate owner decision, recorded with a date.
Passes that discover a conflict between code and this file fix the code or
propose an amendment — never silently diverge.

**2026-08-15 — assign-to-agent plan lane (standing-consent materialization).**
Owner decision (todo-panel plan, approved 2026-08-15): assigning a todo to an
agent is standing approval for materializing that agent's plan output into
`system/todo-plans/<todo-id>.md` (`## plan` section only), through vaultwriter
under the `todo-plans-agent` capability. Execution beyond the plan still
requires the explicit fire action. §4 otherwise holds — agents only propose;
the materialization is audited (`write-audit.log`) and git-trailed like every
system-zone write, the write is a surgical section swap so hand-edits to the
rest of the record never collide, and the human-is-the-mutex doctrine holds
because fire snapshots the plan bytes at fire time.
*Amended 2026-08-16 (persona plan Q3, owner-resolved 2026-08-15): the standing
consent covers system-initiated re-materialization — when the todo's text or
description changes under an agent-held plan, manifest may spool a re-plan
without a per-replacement approval; the replacement still lands audited,
section-swapped, and traced as a thread comment. Fire semantics unchanged.*
*Amended 2026-08-16 (kairos plan, owner-approved with it): on AION items the
standing consent extends to TEAM-triggered actions from the portal — any
signed-in @aion.bio member may mention/assign the team-surface agent (plans
materialize through the same audited lane) and FIRE its plan; every fire is
attributed to the member, recorded in the team activity trail, carded into the
owner's FEED, and the result posts back into the item's team-visible thread
(the closed loop). Execution never starts without a fire; REVIEW stays the
ceiling.*

**2026-08-14 — the AION team portal: an authorized many-writers class.**
Owner decision (portal move, 2026-08-13/14): the team portal served on its
own listener (`:7778`, portal.aion.bio) admits a second class of writers —
any Google-verified `@aion.bio` account (wildcard by design, no manual
allow-list) — for **portal team state only**: comments on items, field
overrides on items assigned to them (assignee lock, no admin override
lane), their own `team/`-tagged adds, and proposals for others (decided by
the portal owner or the target). The boundary is tier-shaped, not
role-shaped: team writes land exclusively in the shared derived store
(`/shared/apps/aion-portal` — append-only `activity.log` JSONL +
`items.ext.json`, git-trailable), never the vault, never the system zone,
never the owner's records. Manifest reads that store back as FEED
**notices** (§5's existing kind — no taxonomy change). The vault remains
single-writer; §1 holds everywhere except this named surface. Auth is
Google OAuth (web client in the secrets tier) with signed cookie sessions;
open read stays.
