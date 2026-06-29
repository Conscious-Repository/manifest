# Personal Dashboard — Scope & Plan

A markdown-native personal operating system that sits on top of your existing
Obsidian vault. It does not own your data; the vault does. The dashboard is a
local app (an extension of the manifest we already built) that reads and writes
plain `.md` files and renders them as a fast, focused control surface for four
things, in priority order:

1. **Goals & task planning** — the canon goal spine (North Star → 90-day → 30-day → today)
2. **Scheduling** — a calendar you can edit, feeding the daily manifest
3. **Agent coordination** — directing AI agents from one place
4. **Communications** — Telegram / Signal / email in one inbox *(deferred past v1)*

Confirmed decisions: Google Calendar backend; a single master `goals.md`;
owner tags + a "My Plate" view; v1 = goals + manifest + two-way calendar +
agent coordination.

---

## 1. First principles

- **The vault is the source of truth.** Every piece of state is a markdown file
  in your Obsidian vault. If the dashboard disappears tomorrow, you still have
  everything, readable and editable in Obsidian. This is the whole point.
- **The dashboard is a lens, not a database.** It may keep a local SQLite index
  for speed, but that index is a *derived, rebuildable cache* — never the
  source. (This is exactly how Obsidian's Dataview works.)
- **Editable from both sides.** Anything the dashboard writes is clean,
  human-readable markdown you can also edit by hand in Obsidian, and it reads
  your hand edits back. No lock-in, no hidden format.
- **One ritual drives the system.** Each evening you plan tomorrow in the
  manifest. That act pulls from your calendar and your 30-day goals. Everything
  else exists to make that ritual fast and honest.
- **No syntax, ever.** You never edit raw markdown to use this. Areas, goals,
  schedules, and tasks are edited through structured UI — the way you'd expect a
  page to work, not a text file. The markdown underneath stays clean and
  hand-editable in Obsidian if you ever want it, but the app never requires it.
- **Convention over location.** The app finds things by naming/frontmatter
  convention, scanning the whole vault — not by hardcoded folders. A
  `YYYY-MM-DD` note is a daily note wherever it lives.

---

## 2. How the app finds things (convention, not location)

Point the app at your **whole vault**. It does not need to know that your daily
notes live in `/intrinsic` — it discovers files by convention and renders them
by *type*, wherever they sit. Move a note to a different folder and nothing
breaks.

The conventions (all overridable in settings):

- **Daily note** — any file named `YYYY-MM-DD.md`, anywhere in the vault. This is
  the manifest/journal for that date. Your `/intrinsic` notes already qualify;
  new ones can be created beside them or anywhere else.
- **Goals** — the file named `goals.md`, or any note with `type: goals` in its
  frontmatter. One per vault.
- **Areas** — the `##` sections inside `goals.md` (Aion, OODA Group, …).
- **Agents** — notes with `type: agent` in frontmatter, plus the message-queue
  folders described in §6.

A lightweight `type:` frontmatter field is the escape hatch when a filename isn't
enough; otherwise the filename is plenty. The app keeps a derived index of "what
is where" so lookups are instant, and rebuilds it by re-scanning whenever files
change (it watches the vault). Your journal and the manifest coexist in the daily
note via a delimited block — the app only rewrites between its markers and never
touches your prose.

---

## 3. The goals system (the hard part)

Your stated pain: too many goal-bearing contexts (Aion, OODA Group, the house
with Olga, personal, sidequests), and within Aion a split between *company*
goals and *your* next actions. The model below is one file, queryable, and
designed so "what's actually on Benjamin right now" is one click.

### 3.1 Structure of `goals.md`

One file. Each **life area** is a section with a North Star (the canon, rarely
changes), then 90-day and 30-day goals as task lines. Daily tasks are NOT stored
here — they live in the day's manifest and *link back* to a goal.

Goals carry inline fields (`[key:: value]`) that are both hand-editable and
machine-queryable (Dataview reads them too):

```markdown
# Goals

## Aion
> North Star: Bring aging under biomedical control.

### 90-day
- [ ] Go/no-go decision on IPR/ICR [owner:: me] [due:: 2026-09-15]
- [ ] Complete animal data package [owner:: team]
- [ ] Close Series A [owner:: me] [due:: 2026-09-30]

### 30-day
- [ ] Stand up thymus organoids [owner:: team]
- [ ] Hire another engineer [owner:: team]
- [ ] Draft Murugan/Picard partnership contract [owner:: me] [due:: 2026-07-14]

## OODA Group
> North Star: <...>
### 90-day
- [ ] ...
### 30-day
- [ ] ...

## House (Olga + Benjamin)
> North Star: <...>

## Personal
> North Star: <...>

## Sidequests
- [ ] ...
```

**You never type this markdown.** In the app, areas are managed like a simple
settings page — add an area, rename it, reorder them, set a North Star — and
goals are added, edited, and checked through the UI. The block above is just what
the app writes to disk so Obsidian (and you) can read it. Editing a North Star in
the app is as easy as editing any field; you never see `[owner:: me]` unless you
open the raw file in Obsidian yourself.

### 3.2 The owner tag — solving company-vs-Benjamin

Every goal/task gets `[owner:: me | team | <name>]`. This is the key that
untangles your Aion problem:

- *"Complete animal data package"* is `owner:: team` — it appears in the Aion
  area so you see the whole picture, but it never lands on your personal plate.
- *"Draft Murugan/Picard contract"* is `owner:: me` — it surfaces in **My Plate**
  and is eligible to be pulled into a day.

### 3.3 The "My Plate" view

A dashboard panel (and a generated `Manifest/my-plate.md` you can read in
Obsidian) that aggregates, across every area, all open items where
`owner:: me`, sorted by due date, grouped by area. This is your real to-do
universe — the honest answer to "what's on me." Everything else (team-owned
goals) stays visible as context but out of your task stream.

### 3.4 How daily tasks relate

You don't manage a separate master task list. The flow is:
**30-day goal (owner:: me) → pulled into a day → becomes a manifest task.**
A manifest task can carry `[goal:: aion/murugan-contract]` so completing it can
optionally check progress back at the source. "Literally just a list" is
preserved: on any given day you see a short list, drawn deliberately from the
30-day layer the night before.

---

## 4. The daily manifest (the "today" surface)

The manifest we built is the daily cockpit. Mapping the panels to the goal spine:

- **Schedule** (half-hour) — your time plan, pre-populated from calendar (§5).
- **Goals panel** — shows the current **90-day** goals (owner:: me) from
  `goals.md`, read-mostly, as standing context while you plan.
- **Milestones panel** — shows the current **30-day** goals (owner:: me). These
  are the pool you pull tomorrow's tasks from.
- **Tasks** — today's short list. Hand-typed or pulled from the 30-day pool.

The dashboard **home is always a day** — today by default — showing its schedule,
goals, milestones, and tasks. You move through time with the same ‹ / › arrows as
the manifest. There is no separate "planner" screen: planning tomorrow is just
pressing › and filling in the day (§5.3).

So 90/30-day goals are "hard set" in `goals.md` and merely *reflected* in the
manifest; the only thing you author per-day is the schedule and the task list.

---

## 5. Calendar (Google, two-way)

### 5.1 Capabilities

- **Read** your Google Calendar (OAuth) and render a month/week view in the
  dashboard.
- **Click a day → load that day's manifest** (the planning view for that date).
- **Create events** from the dashboard, Vimcal-style (quick-add, keyboard-first),
  written straight to Google Calendar.
- **Offline mirror:** each day's timed events are snapshotted to
  `Manifest/cache/2026-06-29.md` so the vault is self-contained offline. Google
  remains the source of truth for events; the mirror is read-only.

### 5.2 Auto-populating the schedule (the smart part)

When you open a day to plan it, the app pulls that day's calendar events into the
schedule's half-hour blocks, **with filters so it stays useful:**

- Skip **all-day** events (birthdays, reminders).
- Skip **multi-day** events (travel blocks, vacations) — anything whose span
  ≥ 24h or crosses midnight boundaries as a block.
- Only place **timed events with a real start/end** into the matching slots.
- Calendar-sourced slots are marked distinctly and **never overwrite** something
  you typed manually; your plan and your meetings coexist.

### 5.3 Planning by navigation (no button)

There is no "Plan tomorrow" button. The home view is a day; you press › to land
on tomorrow. When you land on a **future day that hasn't been planned yet**, the
app quietly does the prep for you:

- pulls that day's timed calendar events into the schedule (skipping all-day and
  multi-day, §5.2),
- surfaces your 30-day (owner:: me) pool so you can drop a few into the task list.

So the evening ritual is simply: open today, check off what got done, press › ,
and fill in tomorrow. The streak rewards consistency. Nothing to remember, no mode
to enter — it's the same arrow you already use to move between days.

---

## 6. Agent coordination

You asked me to look into current best practices. The short version of the 2026
state of the art, and how it maps onto your markdown constraint:

### 6.1 What the field has settled on

- Production multi-agent systems converge on a few patterns: **supervisor /
  hub-and-spoke** (one orchestrator delegates to specialized subagents),
  **pipeline**, and **swarm**. Choreography-via-shared-state is favored for loose
  coupling. ([amux guide](https://amux.io/guides/ai-agent-orchestration-2026/),
  [digitalapplied](https://www.digitalapplied.com/blog/multi-agent-orchestration-5-patterns-that-work))
- A live research thread is the **blackboard pattern**: a shared workspace where a
  main agent posts requests and subordinate agents pick up what matches their
  skills — shown to outperform rigid orchestration on heterogeneous work.
  ([arXiv 2510.01285](https://arxiv.org/pdf/2510.01285))
- The most concrete 2026 practice: **agents coordinated through markdown files** —
  subagents are *defined* as markdown-with-frontmatter, and todo/plan **state is
  persisted as markdown on disk**; supervisors read a board, subagents claim
  tasks and write results back. ([Claude subagents](https://code.claude.com/docs/en/sub-agents),
  [Agent Kanban / markdown board](https://dev.to/battyterm/i-let-ai-agents-manage-themselves-with-a-markdown-file-5547))
- Cost practice: **model tiering** — cheap model for triage/routing, capable
  model for reasoning (40–60% savings). Caution: ~40% of multi-agent setups fail
  in production from over-engineering the orchestration.
  ([beam.ai](https://beam.ai/agentic-insights/multi-agent-orchestration-patterns-production))

### 6.2 The recommendation: the vault *is* the blackboard

This is the lucky part — the markdown-everything constraint you already want is
*also* the current best practice for agent coordination. So:

- **No new infrastructure.** Agents coordinate by leaving traces in `.md`
  (stigmergy): you post a request, an agent acts, it writes back. The dashboard
  is just a nicer window onto the same files.
- **Agents are markdown.** `Agents/hermes.md` has frontmatter (`model`,
  `schedule`, `tools`, `permissions`) and a prose brief. Adding an agent = adding
  a file.
- **Work flows through `Agents/inbox/` → `Agents/outbox/`.** The dashboard posts
  a task as a markdown file; the agent claims it, works, drops a result note;
  the dashboard shows a small "Agents" panel with status and recent outputs.
- **One supervisor, few specialists.** Start with a single orchestrator
  (repoint Hermes at this vault folder). Don't build a swarm; the 40% failure
  rate is from premature orchestration.
- **Tiering:** Hermes (cheap) triages and routes; escalate to a stronger model
  only for real reasoning tasks.

### 6.3 Hermes, concretely

Hermes already runs on DigitalOcean with crons. Repoint him at a synced copy of
the `Agents/` folder (Obsidian Sync, git, or syncthing). His cron pulls become
"write a digest note to `outbox/`," which the dashboard surfaces. You stop
checking a server and start seeing agent output in the same place you plan your
day. If even this feels like too much for v1, it cleanly defers — the inbox/outbox
folders are just files.

### 6.4 Making it bulletproof

You asked for the most bulletproof approach, and "coordinate agents through
markdown files" is only safe if the file mechanics are crash-proof. The proven
design is **Maildir** — the 1995 mail-delivery format built for exactly this:
move messages around a filesystem with no locks, no corruption, and no loss if a
process dies mid-write. It is being reused for agent-to-agent queues today.
([Maildir](https://en.wikipedia.org/wiki/Maildir),
[agent-message-queue](https://github.com/avivsinai/agent-message-queue))

```
Agents/
├── hermes.md                 agent definition (frontmatter: model, schedule, tools)
├── tmp/                      task being written (never read from here)
├── inbox/                    fully-written, unclaimed tasks
├── claimed/<agent>/          a task an agent is working on
├── done/                     completed tasks (kept, not deleted)
├── failed/                   gave up; needs a human look
├── outbox/                   results / digests the dashboard surfaces
└── approvals/                proposed irreversible actions awaiting your yes
```

The rules that make it safe:

- **Atomic handoff via rename.** A task is written fully into `tmp/`, then
  atomically `rename`d into `inbox/`. A reader never sees a half-written file —
  rename is atomic on the filesystem.
- **State is the folder, not a flag.** A task moves `inbox/ → claimed/ → done/`
  (or `failed/`) by atomic rename. The folder *is* the status, so it can't get
  out of sync. You can read the whole system's state with `ls`.
- **Claiming is a race-free rename.** An agent claims a task by renaming it from
  `inbox/` into `claimed/<agent>/`. Exactly one agent can win that rename, so no
  two agents ever do the same job — and no locks are needed.
- **Idempotency keys.** Every task carries a stable `id`. Agents record the ids
  they've finished; replaying or retrying a task is a no-op the second time.
  ([why jobs run twice](https://medium.com/@surajs78/why-is-my-job-running-twice-understanding-idempotency-and-deduplication-in-distributed-systems-d56edbcad051))
- **Append-only, never destructive.** Nothing is deleted or overwritten. Results
  are new files in `outbox/`; the run log is append-only. Your vault's git /
  Obsidian Sync history is the audit trail and undo.
- **Automatic crash recovery.** On startup the supervisor sweeps `claimed/` for
  tasks older than a timeout and moves them back to `inbox/` (the worker died).
  At-least-once delivery + idempotency = effectively-once execution.
- **Side effects are gated.** Agents may freely *read* the vault and *write
  proposals* (draft notes, suggested events), but anything irreversible — sending
  an email, creating a real calendar event, spending money — lands in
  `approvals/` for you to confirm from the dashboard. Agents never take
  irreversible action unattended.
- **One supervisor, few workers.** Hermes is the sole orchestrator to start. No
  swarm, no agent-spawns-agent — that is precisely where ~40% of multi-agent
  systems fail in production.

Net effect: the worst case is a task that gets retried, never one that corrupts
your vault or fires twice. Every transition is a single atomic rename, every
action is logged, and every dangerous action waits for you.

---

## 7. Communications (deferred past v1)

You flagged this as the most sacrificeable, and it's also the hardest
(Signal in particular). When it comes back, the same pattern holds: aggregate
into a read-only markdown inbox (`Comms/inbox.md`), newest first, one line per
message with a deep link back to the real app to reply.

- **Email:** Gmail API or IMAP, read + draft.
- **Telegram:** Bot API (read your own via a userbot, or a bot you forward to).
- **Signal:** `signal-cli` linked as a secondary device — workable but fiddly.

Reply/send stays in the native apps initially; the dashboard is a unified
*reading* surface first. This keeps it honest and low-risk.

---

## 8. Technical shape

- **One Go binary**, extending the current manifest server. Embeds the web UI,
  reads/writes the vault, talks to Google Calendar, watches files (`fsnotify`)
  so hand edits in Obsidian reflect live.
- **Derived index (optional):** SQLite cache of parsed goals/tasks/events for
  instant "My Plate" and calendar queries — rebuildable from the vault at any
  time, never authoritative. (Aligns with a local-first / SQLite stack.)
- **Secrets:** Google OAuth tokens live *outside* the vault (e.g.
  `~/.config/manifest/`), never in markdown.
- **Web UI:** the existing Hanken-Grotesk/mono design language, extended with a
  calendar view, a goals/My-Plate view, and an agents panel.

Data flow: `Obsidian vault  ⇄  Go app (index + parsers)  ⇄  web dashboard`,
with `Go app ⇄ Google Calendar` as the one external sync.

---

## 9. Roadmap

**Phase 0 — done.** Daily manifest over the daily note (schedule + tasks),
half-hour granularity, duration connectors, exact VV design.

**Phase 1 — the spine (v1).**
- Whole-vault scan + convention index; find daily notes (`YYYY-MM-DD`) anywhere.
- `goals.md` schema + parser; owner tags; **My Plate** view; structured in-app
  editing of areas/goals (no syntax).
- Navigation home: any day via ‹ / › ; manifest Goals/Milestones read 90/30-day
  (owner:: me) from `goals.md`.
- Google Calendar **read** + month/week view; click-a-day → that day's manifest.
- Auto-populate schedule from timed events (skip all-day/multi-day) when landing
  on an unplanned future day.
- Agent **Maildir scaffold**: the `Agents/` queue (tmp/inbox/claimed/done/failed/
  outbox/approvals), supervisor crash-sweep, a dashboard agents panel; repoint
  Hermes as the sole orchestrator.

**Phase 2 — write + deepen.**
- Calendar **create/edit** from the dashboard (Vimcal-style quick add).
- `approvals/` flow for agent-proposed irreversible actions.
- Pull-goal-into-day and goal ↔ task back-linking.

**Phase 3 — optional.**
- Communications read-inbox (email → Telegram → Signal, in that order of ease).
- Richer agent supervisor (only if Phase 2 proves valuable).

---

## 10. Decisions locked (this round)

1. **Vault scope** — whole vault; find files by convention, not location (§2).
2. **Daily notes** — `YYYY-MM-DD.md`, discovered anywhere (e.g. your `/intrinsic`).
3. **Areas** — locked set for now, but fully editable in-app with no syntax (§3.1).
4. **Planning** — no button; navigate with ‹ / › ; future days auto-prep on
   landing (§5.3).
5. **Agents** — in v1, on the crash-proof Maildir design (§6.4).
6. **Goal progress** — manual; check off what's done as part of the evening ritual.

### Immediate next steps

- I turn this into a **phased build plan** with concrete engineering tasks
  (vault scanner + index, `goals.md` parser, structured editors, Google OAuth,
  calendar view, My-Plate, the agent Maildir queue + supervisor).
- Confirm the **area list** so `goals.md` seeds correctly (Aion, OODA Group,
  House, Personal, Sidequests — add or remove any).
- Pick the **first slice to build**. Suggested order: extend the working manifest
  into the goals + navigation home, then wire Google Calendar read, then land the
  agent Maildir scaffold — each independently useful.
```
