# Personal Dashboard — Build Plan

A concrete, phased engineering plan that turns `manifest-dashboard-spec.md` into
code. It extends the existing `manifest DRAFT/` Go app. Hand this to Claude Code with
the repo open and your vault mounted; work it milestone by milestone.

---

## Where to build this

**Build in Claude Code, not Cowork.** This is a multi-session project that needs
your real vault, the Go toolchain (`go build`/`go test`), Google OAuth secrets,
and `fsnotify` against live files. Cowork is for the next round of *design/spec*
work; Claude Code is for *implementation*.

Setup for Claude Code:
1. Put the existing `manifest/` project in a git repo.
2. Mount/point it at your Obsidian vault (read/write).
3. Give it `manifest-dashboard-spec.md` + this plan as context.
4. Work one milestone per branch; `go test ./...` gates each merge.

Secrets never go in the vault or the repo: store Google OAuth tokens in
`~/.config/manifest/`.

---

## Architecture recap (target)

```
manifest/ (Go, single binary)
├── main.go            flags, bootstrap
├── config.go          vault path, conventions, port
├── vault/             scanner + index + file watcher  (NEW)
├── daily/             daily-note manifest block parse/serialize  (from store.go)
├── goals/             goals.md parse/serialize, owner model, My Plate  (NEW)
├── calendar/          Google OAuth + events + auto-populate  (NEW)
├── agents/            Maildir queue + supervisor  (NEW)
├── server/            HTTP API + embedded web UI
└── web/               UI: day home, goals, calendar, agents panel
```

The vault is the source of truth. An optional SQLite file is a **rebuildable
index only** (fast My-Plate / calendar queries), never authoritative.

---

## Milestone 0 — Foundation: whole-vault scanner + index

Generalize the current app from "a daily-notes folder" to "the whole vault, by
convention."

Tasks
- `vault.Scanner`: walk the vault once, classify each `.md` by convention:
  daily (`YYYY-MM-DD.md`), goals (`goals.md` or frontmatter `type: goals`),
  agent (`type: agent`). Build an in-memory index `map[date]path`, `goalsPath`,
  `[]agentPath`.
- `vault.Index`: lookups (`DailyNote(date) → path`, creating in a configurable
  default dir if absent). Resolve a `YYYY-MM-DD` note **anywhere** in the vault.
- File watcher (`fsnotify`): on change, incrementally update the index.
- Optional `vault.Cache` (SQLite): persisted index for instant startup; rebuilt
  by re-scan; never the source of truth.
- Config: `vaultPath` (whole vault), `newDailyDir` (where new daily notes are
  created, default to where existing ones cluster, e.g. `intrinsic/`),
  conventions overridable.

Acceptance
- `GET /api/day?date=2026-06-29` finds the note regardless of folder.
- Moving a daily note to another folder still resolves; index updates live.
- `go test ./vault` covers classification + anywhere-resolution.

---

## Milestone 1 — Goals system + My Plate + structured editing

The goal spine. One `goals.md`, owner tags, edited entirely through UI.

### 1a. `goals.md` grammar (parser/serializer, round-trip)

```markdown
# Goals

## Aion
> North Star: Bring aging under biomedical control.
### 90-day
- [ ] Go/no-go on IPR/ICR [owner:: me] [due:: 2026-09-15]
- [ ] Animal data package [owner:: team]
### 30-day
- [ ] Draft Murugan/Picard contract [owner:: me] [due:: 2026-07-14]
```

- Areas = `##` sections. North Star = the `>` line. Horizons = `### 90-day` /
  `### 30-day`. Goals = task lines with inline `[key:: value]` fields
  (`owner`, `due`, optional `goal` id).
- Parser tolerant of hand edits; serializer deterministic and stable (no diff
  churn). Preserve unknown fields and any prose the app didn't write.

### 1b. Owner model + My Plate
- `owner ∈ {me, team, <name>}`. Default `me` if absent.
- `GET /api/myplate`: all open items where `owner == me`, across `goals.md`
  (+ daily tasks carrying `[owner:: me]`), sorted by `due`, grouped by area.
- Optionally render `my-plate.md` for reading in Obsidian.

### 1c. Manifest panels read goals
- Manifest **Goals** panel = current 90-day (`owner:: me`). **Milestones** panel
  = 30-day (`owner:: me`). Read-mostly reflections of `goals.md`.

### 1d. Structured editors (no syntax)
- API: area CRUD (add/rename/reorder/set North Star), goal CRUD + check/uncheck,
  set owner/due. All write clean markdown.
- UI: an Areas settings-style page; goal rows with checkbox, owner chip, due
  picker. User never sees `[owner:: me]`.

Acceptance
- Round-trip test: parse → serialize is idempotent; hand edits survive.
- Owner filtering correct; team items never appear in My Plate.
- Editing an area name / North Star in the API rewrites only that section.
- Seed `goals.md` with areas: Aion, OODA Group, House, Personal, Sidequests.

---

## Milestone 2 — Navigation home + planning-by-navigation

Make the day the home surface and make planning a side effect of navigating.

Tasks
- Home route = current day's manifest (schedule + goals + milestones + tasks),
  `‹ / ›` to move days (extend existing nav).
- "Unplanned future day" detection: manifest block empty/absent for a date `>`
  today → on landing, run prep:
  - pull timed calendar events (M3) into the schedule,
  - surface the 30-day (`owner:: me`) pool for quick add.
- Pull-goal-into-day: click/drag a 30-day goal → creates a manifest task with
  `[goal:: <id>]` backlink. Goal progress stays **manual**.
- Evening ritual is implicit: check off today, press ›, fill tomorrow.

Acceptance
- Landing on an unplanned tomorrow auto-prefills events + shows the pool.
- Landing on an already-planned day does **not** clobber it.
- Pulling a goal into a day creates a linked task; goal is not auto-checked.

---

## Milestone 3 — Google Calendar (read) + view + auto-populate

Tasks
- Google OAuth installed-app flow; token cached in `~/.config/manifest/`.
  Scope: read-only first (`calendar.readonly`).
- `calendar.Client.Events(dateRange)` → normalized events
  (`start`, `end`, `allDay bool`, `title`, `id`).
- Month/week calendar view in the UI; click a day → that day's manifest.
- Auto-populate schedule from timed events:
  - **skip all-day** (date-only start, no time),
  - **skip multi-day** (`end - start ≥ 24h` or spans a midnight as a block),
  - map remaining events onto half-hour slots by start/end,
  - mark calendar-sourced slots distinctly; **never overwrite** manual text.
- Optional offline mirror: snapshot the day's timed events into a cache note.

Acceptance
- Birthdays / multi-day travel never land in the schedule.
- A 9:30–10:00 meeting fills the 9:30 slot, flagged as calendar-sourced.
- Manual entries are preserved across re-sync.
- `go test ./calendar` covers the all-day / multi-day filter logic.

---

## Milestone 4 — Agent Maildir queue + supervisor + panel (bulletproof)

The crash-proof coordination substrate. Atomic renames only; folder = status.

### 4a. Queue library (`agents/queue.go`)
```
Agents/{tmp,inbox,claimed/<agent>,done,failed,outbox,approvals}
```
- `Post(task)`: write full file to `tmp/`, then atomic `rename` → `inbox/`.
- `Claim(agent)`: atomic `rename` `inbox/<id>` → `claimed/<agent>/<id>`
  (exactly one winner; no locks).
- `Complete(id)` / `Fail(id, reason)`: atomic `rename` → `done/` / `failed/`.
- Every task has a stable `id`; agents keep a completed-id set → idempotent
  replay.
- Append-only `Agents/run.log`. Nothing deleted or overwritten.

### 4b. Supervisor (`agents/supervisor.go`)
- Startup **crash-sweep**: any `claimed/*` older than `timeout` → `rename` back
  to `inbox/` (worker died). At-least-once + idempotency = effectively-once.
- Poll `inbox/`; route a task to an agent per its `type: agent` definition
  (frontmatter: `model`, `schedule`, `tools`, `permissions`; prose brief).
- One supervisor, few workers. No agent-spawns-agent.

### 4c. Approvals gate
- Irreversible actions (send email, create real calendar event, spend money)
  are written as proposals to `approvals/`; the dashboard shows them for
  confirm/reject. Agents may freely read the vault and write draft proposals.

### 4d. Dashboard agents panel + Hermes
- Panel: queue counts, recent `outbox/` items, pending `approvals/`.
- Repoint Hermes: sync `Agents/` to the DO box (git / Obsidian Sync / syncthing);
  Hermes reads `inbox/`, writes `outbox/`. His crons become "write a digest to
  `outbox/`," surfaced in the dashboard.

Acceptance (tests)
- Atomic handoff: a reader never sees a partial file.
- Concurrency: N goroutines claiming the same `inbox/` item → exactly one wins.
- Crash recovery: a stale `claimed/` task returns to `inbox/` on sweep.
- Idempotency: replaying a completed `id` is a no-op.
- No irreversible action executes without an `approvals/` confirm.

---

## Milestone 5 — Phase 2 polish (after v1 is solid)

- Google Calendar **write**: create/edit events from the dashboard (Vimcal-style
  quick add); upgrade OAuth scope.
- `approvals/` → real side effects (the dashboard executes on confirm).
- Goal ↔ task backlink reporting (which day did the work toward goal X).
- Begin Communications read-inbox (email → Telegram → Signal), per spec §7.

---

## Cross-cutting

- **Testing:** Go tests per package; the riskiest logic (goals round-trip,
  calendar filters, queue atomicity/concurrency) gets focused unit tests. With
  the Go toolchain in Claude Code, the Node-mirror trick from Cowork is no longer
  needed.
- **Index integrity:** the SQLite cache must be droppable at any time and rebuild
  from a vault re-scan with identical results.
- **Migration:** the current `manifest/` keeps working throughout; M0–M2 are
  pure extensions, so you always have a usable app.

---

## Suggested first slice

Do **M0 + M1** first (whole-vault scan + the goals/My-Plate spine with structured
editing) — it extends what already works and immediately gives you the goal
system you most want. Then **M2** (navigation home), then **M3** (calendar read),
then **M4** (agent Maildir scaffold). Each milestone is independently useful and
shippable.