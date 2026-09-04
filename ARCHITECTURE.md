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
   inside delimited regions or files the user owns (goals.md, tasks.md,
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
KNOWLEDGE ZONE INCLUDED: goals.md, tasks.md, daily notes, and archives
route through the chokepoint under a knowledge-zone capability, no
exemptions (decided 2026-07-28; goals/tasks/daily's direct writers predate
the boundary and are scheduled for the kernel-extraction pass).

## 5. Attention taxonomy (closed set)

Everything that wants the owner's attention flows through the FEED as one
of exactly four kinds, each with a declared lifecycle behind a single
`AttentionSource` interface:

* **findings** — spirit/engine items; markdown files; verdicts feed the tune
  loop; may be saved to vault. **Amended 2026-08-25:** the Keep BUTTON is gone
  — over eight weeks it was pressed twice against 128 discards, and both
  cornerstone rewrites the loop produced were derived from discard patterns
  alone. The `kept` status remains and is still read by `tuning.evidence`, but
  it is now written only by acting on an item (`→ task`, save-to-vault), which
  is the more honest positive signal. Discard is the primary verb.
* **signals** — app-derived conditions; virtual; auto-clear on resolution;
  Act/Snooze/Dismiss; never enter kept/discarded. A signal card MAY carry
  domain quick-actions that resolve its condition (e.g. a stale task's
  Done/Waiting/→issue/Drop) — those are domain writes, not lifecycle
  verbs; auto-clear still governs. (Clarified 2026-07-28.)
* **notices** — externally-sourced portal items; dismiss; expire (14d);
  never enter kept/discarded.
* **receipts** — outcomes of actions the system took (errands); permanent
  audit trail; never expire, never kept/discarded.
* **consume** — externally-sourced long-form reading the owner subscribed to
  (RSS/Atom feeds, X accounts); polled deterministically, no LLM in the loop.
  Lifecycle read-curate-dismiss: **unread never expires** (an essay not yet
  read is still wanted — this is what disqualified notices), read/dismissed age
  out at 90d, and consume items never enter kept/discarded. Contributes **0**
  to the FEED badge: reading is not attention debt. CURATE is a domain write,
  not a lifecycle verb (the 2026-07-28 quick-action clarification, extended) —
  it writes an `extrinsic/` note, and those notes are the sole contract the
  public curation feed reads. (Added 2026-08-24 — see the amendment below.)

Adding a sixth kind requires amending this file, deliberately.

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

**2026-08-17 — AION is one live composed model, not a publish artifact.**
The Manifest process owns the projection rendered by both the private AION
cockpit and the team portal: owner-authored vault records form the base;
`/shared/apps/aion-portal` contributes `team/` items, comments, activity, and
field overrides; the overlay wins for its bounded fields; archives are kept
with their attributed snapshot and thread but excluded from active views.
Every vault backlog item carries a persisted `aion-bl/…` identity that is
stable across title edits. Portal writes update only the shared team store and
never materialize into Markdown (**superseded 2026-08-24 — see below**). An
explicit owner edit in Manifest still
crosses `vaultwriter`, supersedes only overlapping overlay keys, and records
that resolution in team activity. Multi-store owner operations are journaled
under dataDir and replay idempotently.

**2026-08-24 — the team store materializes into the AION backlog.** Owner
decision: the store stops being a parallel truth. It had become one — a
member's new task lived only in `items.ext.json`, and a member marking the
owner's task done wrote an override that Manifest applied at read time while
`backlog.md` still said `open`. Two surfaces, two answers, and Obsidian — the
actual record — heard about neither.

A reconciler (`server/aion_sync_back.go`) now writes three things into
`system/aion/backlog.md`: portal-created items, field edits on published
items, and approved proposals. It runs after every portal write and on a slow
ticker. Each pass ends with the store surrendering what it handed over — the
item row is dropped once the line exists, the override cleared once the record
carries it — so the overlay becomes a staging area that empties itself.
Comments and the activity trail do **not** cross; they have no line grammar and
remain the portal's own record.

The write is bounded three ways: one new capability `aion-portal`, whose
pattern is the single file rather than the `system/aion/**` subtree its
neighbours hold; a new actor `portal-member`, distinct from
`approved-proposal` because no owner approved the individual write — the
standing consent is the capability itself; and a pre-flight that renders the
candidate corpus and runs the same acceptance gate the projection will. That
last one is load-bearing: an invalid corpus makes the projection serve its
last-known-good snapshot, so without the check a member typing a rock name
that resolves to no goal would take AION offline for everyone. The reconciler
declines to write instead, leaves the state staged, and names the line in the
log.

The same amendment widens what an assignee may edit — title, owner
(reassignment), `decided`, and a real `decided` status — and adds a delete that
archives. **The assignee lock is unchanged and gained no admin override**: the
owner considered one on this date and declined it, so the 2026-08-13 decision
stands in full. A proposal for a teammate now also files an approvable card in
the owner's FEED (`approvals.TypePortalProposal`) instead of a dismiss-only
notice; either the owner or the target may still decide it, and whoever acts
first settles it.

The portal's compatible `data/*.json` and `content/*.md` contract is rendered
and validated in-process. Valid revisions become visible immediately;
coverage warnings remain visible; invalid revisions serve the atomically
persisted last-known-good snapshot and raise an auto-clearing FEED signal.
Both open UIs converge through lightweight visibility-aware revision polling;
git, a deploy, and a PUBLISH gesture are not part of AION synchronization.
This amendment preserves the 2026-08-14 exception exactly: the team portal is
still the one bounded many-writer surface, its assignee/proposal authorization
rules are unchanged, and the vault and every other Manifest surface remain
single-writer. (The 2026-08-24 amendment above widens what that surface may
write and where it lands, and leaves the authorization rules themselves
untouched.)

**2026-08-19 — approved goals placement (the first knowledge-zone proposal
lane).** Owner decision (telegram→feed goals plan, approved 2026-08-19): a
user-CONFIRMED `goals-item` proposal may write `goals.md` — exactly one
placement (one line added, one line edited in place, or one line moved) per
confirm, through vaultwriter under the `goals-approved` capability
(`{ZoneKnowledge, ActorApprovedProposal}` — deliberately the first of its
kind; aion/re/todo-plans are all system-zone). This reconciles both of §2's
absolute clauses for this one lane: the WORDS are owner-sourced (spoken to his
agent on Telegram or in a thread — the agent only places them, never authors
them), and the "never new content" sweep rule yields to §4's user-approved
proposal exactly as §4 always permitted mechanically. Guards: the only writable
path is the vault-root goals.md; the transform is a whole-file fixpoint with a
structural budget (a confirm that would change more than its one line refuses);
edits/moves carry a staleness anchor and refuse when the file moved underneath;
frozen history is byte-identical; never auto-applied; audited and git-trailed.
§4 otherwise holds — agents only propose.

**2026-08-15 — assign-to-agent plan lane (standing-consent materialization).**
Owner decision (todo-panel plan, approved 2026-08-15): assigning a task to an
agent is standing approval for materializing that agent's plan output into
`system/todo-plans/<todo-id>.md` (`## plan` section only), through vaultwriter
under the `todo-plans-agent` capability. Execution beyond the plan still
requires the explicit fire action. §4 otherwise holds — agents only propose;
the materialization is audited (`write-audit.log`) and git-trailed like every
system-zone write, the write is a surgical section swap so hand-edits to the
rest of the record never collide, and the human-is-the-mutex doctrine holds
because fire snapshots the plan bytes at fire time.
*Amended 2026-08-16 (persona plan Q3, owner-resolved 2026-08-15): the standing
consent covers system-initiated re-materialization — when the task's text or
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

**2026-08-24 — CONSUME: the fifth attention kind, and the first public
surface.** Owner decision (consume-feed plan, 2026-08-24): manifest polls the
writing the owner subscribed to — RSS/Atom feeds and X accounts — renders it in
a reader inside the FEED, and gives each item one button, CURATE, that promotes
it to a public feed he can share.

Three parts of this are new, and each is deliberate:

*§5 grows to five kinds.* **consume** is declared above with lifecycle
`read-curate-dismiss`. It could not be absorbed as **notices** (the 2026-08-14
precedent) on two specifics: notices expire at 14 days, and unread reading must
not silently vanish; and notices carry dismiss only, with no read state and no
approve verb. Its badge contribution is **0** — the FEED badge counts what wants
something *from* the owner, and an unread essay does not. The lane shows its own
unread total instead.

*The tier split follows §2 rather than convenience.* What the owner authored is
irreplaceable and lives in the **vault's extrinsic zone** — `extrinsic/feeds.md`
(the subscription list, one record-kernel line per source, under the fixpoint
guarantee) and one `categories: [articles]` note per curated item, beside his
books. What can be re-fetched is disposable and lives in **dataDir** — poll
caches, sanitized article bodies, the X token (0600, secrets tier). Deleting
dataDir costs a re-poll and nothing else. Two capabilities carry the writes:
`consume-feeds` (one exact file) and `consume-curate` (`extrinsic/**`), both
actor `user-action`, because subscribing and curating are the owner clicking in
his own cockpit. **Removing those two grants is the feature's rollback:** the
lane keeps reading and curation goes read-only.

*A public listener exists for the first time.* Everything else in this system is
loopback + Tailscale or OAuth-gated. The curation feed is served on its own
loopback port behind the existing cloudflared tunnel, and it is **opt-in** —
`Consume.PublicPort` is deliberately not backfilled from the defaults, so a
public port never appears because a binary was upgraded. Its handler is
constructed holding `consume.CuratedFeed`, a **one-method interface**
(`Entries()`), and nothing else: no server, no vault, no cache. Serving a
private item is not something it declines to do, it is something it cannot
express. The projection behind that interface selects only extrinsic notes that
declare `categories: [articles]` **and** carry a `curated:` date — reading
something is not publishing it. A compile-time canary plus an isolation test
that stuffs the vault with private notes keep both claims honest.

Mirroring full third-party text was the owner's explicit choice, made against a
stated objection (attribution is not a license). Every entry's `<link>` is the
original, the index is `noindex`, and mirror-vs-excerpt is per-subscription so a
single objecting writer is a one-field edit.

**2026-09-04 — agent chat: one tab, one adapter, three transports.** Owner
decision (agent-chat collaboration plan, Qs 1–8 resolved 2026-09-04): the
CHAT tab is the one place the owner talks to every agent, and the task
board is the one place work is assigned. Neither gained a scheduler or a
transport — §7's "two schedulers, never three" holds, and manifest still
reaches Hermes only through `hermes …` execs. The rail is grouped by agent;
what differs per section is the backend behind one transcript renderer and
one composer:

| Agent | List threads | Send | Reply arrives | Transcript truth | Writer |
|---|---|---|---|---|---|
| concierge (spirit) | `ChatSessions()` | spool `kind: chat` | engine rewrites the session file; SSE + poll | `artifacts/chats/<id>.md` | engine |
| kairos / zeck | `chatthreads.Store.List` | `chatAskFor` → `SpoolRunNow` (one order at a time) | `chatSweep` (60 s ticker + on read); poll | `<portal dir>/chat.json` | manifest |
| alfred + `hermes profile` rows | `agentchat.Store` | `hermes.Runner.Run` on the request goroutine, in-flight marker | the goroutine appends the reply turn; poll | `<primary harness>/artifacts/chats/<agent>/<id>.md` | manifest (one writer, per-thread lock) |

The Hermes turn is honest about continuity: every turn is a fresh `hermes -z`
(no `--resume` — the flag was silently dropped), so manifest composes the
conversation window into the prompt, keeps spend and the Hermes-side
`session_id` in the ledger, and the file stays the truth. Agent sessions use
the spirit session grammar (`## Turn N — who · time`, `### Step N — cast`) so
one parser serves all three; a second send while a turn runs queues in the
file (`queued`), mirroring the spirit contract. Chat turns run on read-only
toolsets — §4 holds, world changes stay `manifest-proposal` blocks in FEED.

**The task board's agent entry points** (plan §3.4): the thread composer has
three modes — Comment (record only, never a turn), Ask ✦ (one turn, answered
in the thread), Do ✦ (assign → plan → fire) — plus `@name` / `@name::intent`
parsed server-side against the roster (unknown names stay prose) and the
capture bar's `… @alfred` / `!do`. The reply guard is the invariant: nothing
spends a turn without an explicit Ask, Do, mention, or reply-to-agent. The
roster is explicit addressing (`agent:alfred` aliasing `agent:hermes`,
`agent:<profile>`, `agent:kairos`, `agent:zeck`); profile descriptions are
tooltips and a non-binding "suggest agent" hint — never silent routing.

**Bridges.** "→ task" on any agent chat turn creates a todo through the
capture path, pins it, copies the conversation window into its thread
(owner turns as the owner, agent turns as the agent, `meta.from: chat`, the
first entry carrying `meta.chat`), assigns the agent record-only, and stamps
`task:` on the session. "Open in chat" reads that link back; an agent's rail
section lists the open todos it holds. dataDir never syncs, so Alfred chats
and personal task threads are metis-only (accepted, Q7).

**2026-08-31 — §11 component list: old click-to-edit money shell deleted, `cardShell` added.**
The legacy click-to-edit money helper family (~80 lines in `05-components.js`)
had zero call sites — every money field in the app edits through `moneyInput`
directly instead — and is
deleted; `fmtMoney`/`fmtPct`/`moneyInput` stay, they have live callers. In its
place, the UI-conventions pass (`plans/manifest-ui-conventions.md` C2) adds
`cardShell`/`cardActions`, the one factory every FEED-rendered card (findings,
signals, portal notices, receipts, approvals) now builds through.
