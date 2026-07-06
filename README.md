# Manifest

A local, single-binary dashboard over your Obsidian vault. It turns the plain
markdown you already keep — daily notes, goals, meeting notes — into a planner,
a CRM, a goal tracker, and a calendar view, without ever taking your data out of
the vault. The UI is modeled on the [Visualize Value manifest](https://vv.xyz/manifest).

Everything it shows is read back out of your notes, and the only things it writes
are the edits you make through the dashboard — into a small, clearly delimited
region of your own files. Your journal is never read, shown, or touched.

---

## What it does

The dashboard is a set of tabs, all backed by your vault:

- **Today** — your daily plan: a half-hour **schedule**, a **focus** list pulled
  from your active goals/milestones, and **tasks**. Pull the day's meetings in
  from your calendar with one click.
- **Goals** — your goals as a horizon ladder (yearly → 90-day *Rocks* → monthly
  milestones), organized by life *Area*, all living in one `goals.md`. Close a
  Rock as a Win or a Learn to archive it; run a quarterly review to carry Rocks
  forward and write a retro.
- **Calendar** — a month view of your Google Calendar(s), read-only. Connect one
  or more accounts; events can auto-fill your daily schedule.
- **Contacts** — a people layer over your vault. Everyone you `[[link]]` in a
  meeting note becomes a contact with a timeline, open loops, transcripts, and a
  neglect ("going cold") lens. Link a contact's email(s) to get a
  **calendar-verified "last met"**, distinct from a note-based "last mentioned".
- **Notes** — open, read, and edit *any* vault note in place, with live
  `[[wikilink]]` resolution and backlinks. Jump to Obsidian anytime.
- **Command bar** (`⌘K` / `Ctrl-K`) — look up any contact from anywhere.
- **Spirits** *(optional)* — a console for a separate background agent engine
  ("excalibur"): its feed, run reports, rituals, and an approvals inbox. Disabled
  unless you point at an excalibur tree.

---

## Core principle: your vault stays yours

Manifest treats your knowledge vault as **read-only**, with one exception: the
edits *you* make in the dashboard. Concretely:

- In a **daily note**, the app owns only the block between
  `<!-- manifest:start -->` and `<!-- manifest:end -->`. Your journal,
  frontmatter, and every other heading are preserved byte-for-byte.
- **Goals** are written only to `goals.md`; **contacts** email/notes are written
  only when you click to confirm them. Nothing is ever written in the background,
  and no AI-authored content is ever written into the vault.
- All **derived, rebuildable state** (the search index, calendar cache, triage
  decisions) lives *outside* the vault, under `~/.config/manifest`. The app
  refuses to start if you point its data directory inside the vault.

Because the data is just markdown, you can edit the same files in Obsidian and
the dashboard reads your changes back on the next load.

---

## Quick start

**Prerequisites:** [Go](https://go.dev/dl/) 1.25+. No CGO, no external database —
the index is pure-Go SQLite, so `go build` produces one self-contained binary.

```bash
git clone https://github.com/Conscious-Repository/manifest.git
cd manifest

cp config.example.json config.json      # then edit vaultPath (see below)

go run .                                 # or: go build -o manifest . && ./manifest
```

Open <http://127.0.0.1:7777>.

You can skip the config file entirely and pass flags instead:

```bash
go run . -vault ~/Obsidian/MyVault -port 8080
```

---

## Configuration

Copy `config.example.json` to `config.json` and edit it. Every field is optional
except `vaultPath`; anything omitted falls back to the default below.

| Field | Default | Meaning |
| --- | --- | --- |
| `vaultPath` | *(required)* | Absolute path to your Obsidian vault. `~` is expanded. |
| `newDailyDir` | `intrinsic` | Folder (relative to the vault) where *new* daily notes are created. Existing daily notes are found anywhere in the vault. |
| `dailyNoteFormat` | `2006-01-02` | Go time layout for daily filenames. Match your Obsidian Daily Notes setting — e.g. `2006-01-02` → `2026-07-06.md`, or `2006/01/2006-01-02` for date-foldered notes. |
| `periodNoteDir` | `Manifest` | Folder for legacy monthly/quarterly period notes. |
| `goalsFileName` | `goals.md` | The single file that holds your Areas & Goals. |
| `skipDirs` | `.git, .obsidian, .trash, attachments, Agents, excalibur` | Directory names the scanner ignores (dotfolders are always skipped). |
| `scheduleStart` / `scheduleEnd` | `8` / `18` | First/last hours shown on the schedule (24-hour, inclusive → 8A–6P). |
| `timezone` | *(local)* | IANA name (e.g. `America/Chicago`) for mapping calendar events to slots. |
| `port` | `7777` | Local port the UI is served on. |
| `dataDir` | `~/.config/manifest` | Where all derived state lives. **Must be outside the vault.** |
| `excaliburPath` | *(unset)* | Root of an excalibur harness tree. Unset → the Spirits tab is disabled. |

**Flags** override the config: `-config <path>` (default `config.json`),
`-vault <path>`, `-port <n>`.

**Environment:** `MANIFEST_CONFIG_DIR` overrides both the default `dataDir` and
the location of calendar credentials (default `~/.config/manifest`).

---

## How your notes are used

### Daily notes — the manifest block

Inside a daily note the app manages only the marked region and leaves everything
else exactly as it was:

```markdown
---
tags: [daily]
---

# 2026-07-06

Woke up early. Felt good about the day ahead.   ← your journal, untouched

<!-- manifest:start -->
## Focus

- Series A 15M [goal:: aion/series-a-15m]
- 4848 & 4852 exteriors [goal:: real-estate/new-rock]

## Schedule

| Time  | Focus                  | Focused |
| ----- | ---------------------- | ------- |
| 8:00A | shower                 |         |
| 8:30A | read                   |         |
| 9:00A | Deep work on the parser| x       |
| 9:30A | Deep work, continued   |         |

## Tasks

- [ ] Ship manifest v1
- [x] Review the PR
<!-- manifest:end -->
```

The schedule runs at **half-hour granularity** — every hour has a `:00` and a
`:30` slot. In the UI, consecutive filled slots draw a connector labelled with
the elapsed duration (`30m`, `1h`, `1.5h`), and the "Was I focused?" circle is one
per hour. Because it's a normal markdown table, you can edit it by hand in
Obsidian (tick a box, type a plan, add a `7:00P` row); a bare `9A` is normalized
to `9:00A`. The **Focus** list is linked back to your goals via `[goal:: …]` /
`[milestone:: …]` inline fields.

### Goals — `goals.md`

One file holds every life **Area** (`## Aion`, `## Real Estate`, …), each with a
North Star and a horizon ladder of yearly goals, 90-day **Rocks**, and nested
sub-goals. Each item carries a stable `[goal:: path/id]` inline field so the day
view and archives can reference it:

```markdown
## Aion
> North Star: Bring aging under complete biomedical control.

### 1-year — 2026
- [ ] Prototype consumer MRI for < $100,000 [goal:: aion/new-1-year-goal]

### Rocks (90-day)
- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3]
    - [ ] 10 LOIs [goal:: aion/series-a-15m/10-lois]
```

### The index

On first run the app builds a read-only, rebuildable index of the whole vault
(notes, links, aliases, inline fields, tasks, emails) as a SQLite database at
`~/.config/manifest/index.db`, and keeps it live with a file watcher. It powers
Contacts, Notes, and the command bar. Deleting it is safe — it rebuilds on the
next start. If the index fails to build, the core planner still works; only the
contacts/notes surfaces are disabled.

---

## Using Manifest

### Today

The default view. Fill in schedule slots and tasks — edits **autosave**
(debounced) as you type and toggle. Hit **Pull** to drop the day's calendar
meetings into empty schedule slots. The **streak** counts consecutive days back
from today whose daily note has a manifest block with any focused hour or task.

### Goals

Edit Areas and goals directly; drag to reorder; check items off. Close a Rock as
a **Win** or a **Learn** to move it into the archive (History view). At quarter
boundaries, the **quarterly review** lets you carry unfinished Rocks into the new
quarter and record a retro.

### Calendar (Google)

Calendar is optional and **read-only** (it requests only Google's
`calendar.readonly` scope). To connect:

1. Create an OAuth **Desktop app** client in the
   [Google Cloud Console](https://console.cloud.google.com/apis/credentials) and
   download its JSON.
2. Save it as `~/.config/manifest/google_credentials.json`.
3. In the dashboard, open **Calendar → Connect Google Calendar** and complete the
   browser consent flow. Add more accounts with **+ Add Google account**.

Per-account tokens are stored under `~/.config/manifest/tokens/` (owner-only).
Events across all connected accounts feed the month view, the schedule **Pull**,
and the contacts "last met".

### Contacts

Anyone you write as `[[their name]]` in a dated note becomes a contact — no data
entry required. Each contact page shows a **timeline** of interactions, **open
loops** (unchecked tasks from meeting notes), **transcripts**, and their linked
**emails**.

- **Last met vs. last mentioned.** "Last mentioned" is the newest dated note that
  links them. "Last met" is the most recent *calendar meeting* matched by their
  email — a truer signal. A person can have several emails.
- **Email review queue.** The "Review — N unlinked emails" strip proposes
  calendar attendees to link to existing contacts (matched by full name or by the
  name encoded in the address). Hit **Link** to write the email into that
  contact's note; **Dismiss** to skip it for good.
- **Going cold.** A neglect lens flags contacts you usually see on a cadence but
  haven't lately — using real meeting cadence when an email is linked, else note
  mentions.
- **Triage.** Note-less names you `[[link]]` outside meeting context wait in a
  triage strip until you confirm them as a Person, mark them an Org, or dismiss
  them.

Email is written into note frontmatter as an inline list, matching the vault's
existing convention:

```yaml
---
categories: [people]
email: [dabir@anfavc.com, shoumik.dabir@gmail.com]
---
```

### Notes & wikilinks

Any `[[wikilink]]` opens the target: a person → their contact page, any other
note → a universal note view where you can read the rendered markdown, toggle
checkboxes, edit the raw source, and follow links and backlinks. **Obsidian ↗**
opens the same note in Obsidian.

### Command bar

Press `⌘K` (macOS) or `Ctrl-K` to look up any contact from any tab — last met,
next meeting, latest transcript — and jump to their page.

### Spirits (optional)

If you run the separate **excalibur** agent engine, set `excaliburPath` to its
tree to enable the Spirits tab: a feed of what the engine surfaced, per-run
reports (context, decisions, cost), an editable board of rituals, and a single
approvals inbox. The dashboard only *reads* the engine's artifacts and *records*
your decisions; the engine itself runs as its own process. Leave `excaliburPath`
unset and the tab simply doesn't appear.

---

## Where your data lives

| Location | Contents |
| --- | --- |
| **Your vault** | Everything you author: daily notes, `goals.md`, person/meeting notes. The only files the app writes. |
| `~/.config/manifest/` | Derived + secret state, all disposable/rebuildable: `index.db`, `calendar-cache/`, `contacts.json` (triage decisions), `google_credentials.json`, `tokens/`. Relocate with `dataDir` / `MANIFEST_CONFIG_DIR`. |

---

## HTTP API

The UI is a thin client over a JSON API on the same port. Main groups:

- **Day** — `GET/POST /api/day`, `POST /api/day/pull`, `POST /api/day/focus`,
  `POST /api/day/focus/milestone`
- **Goals** — `GET /api/goals`, `/api/areas`, `/api/goals/item`,
  `/api/goals/check`, `/api/goals/reorder`, `/api/goals/close`,
  `/api/goals/archives`, `/api/goals/carry`, `/api/goals/retro`, `/api/myplate`
- **Calendar** — `GET /api/calendar/status`, `/api/calendar/events`,
  `POST /api/calendar/connect`, `/api/calendar/disconnect`
- **Contacts** — `GET /api/contacts`, `/api/contacts/{triage,page,card,search}`,
  `POST /api/contacts/{confirm,dismiss,dismiss-bulk,org,bind,note,email}`,
  `GET /api/contacts/email-review`, `POST /api/contacts/email-dismiss`
- **Notes** — `GET/PUT /api/note`, `POST /api/note/task`, `GET /api/note/resolve`
- **Spirits** — `GET /api/spirits/{status,feed,runs,rituals,approvals}` and the
  corresponding POST/PUT actions

---

## Architecture

A Go backend serves an embedded vanilla-JS frontend. One binary, no runtime deps.

```
manifest/
├── main.go            entrypoint: flags, config, wiring, server bootstrap
├── config.go          config loading + defaults
├── vault/             daily-note & goals scanner + file watcher
├── daily/             the "Today" service (schedule, focus, tasks, streak)
├── goals/             the horizon-ladder goals model over goals.md
├── calendar/          Google Calendar client + read-only OAuth (multi-account)
├── vaultindex/        read-only SQLite/FTS index of the whole vault
├── vaultwriter/       the guarded vault writes (person notes, frontmatter)
├── contacts/          the people layer (triage, timeline, neglect, last-met)
├── feed/ · spirits/ · approvals/   the optional excalibur console
├── mdfm/              markdown frontmatter parsing
└── server/            HTTP API + embedded web UI (server/web/)
```

The guiding invariant, enforced in code: **the app is read-only on the knowledge
vault except for explicit user saves, and all derived state lives outside it.**

---

## Development & testing

```bash
go build -o manifest .     # single binary
go test ./...              # unit tests across all packages
```

Tests cover time-token round-tripping, journal/frontmatter preservation,
no-duplicate re-saves, reading back hand edits made in Obsidian, the goals
model and archive/review flows, the vault index, and the contacts layer
(last-met vs. last-mentioned, the neglect basis, and email-review matching).

---

## Design & fonts

The UI mirrors the original VV manifest's design tokens — Hanken Grotesk for
entries, a Carbon-style mono for time labels and pills, the `#265ACC` blue for
filled entries, `#e5e5e5` hairlines, and the dark nav pills. Hanken Grotesk and
the Spline Sans Mono fallback load from Google Fonts. To match the original's
label font 1:1, drop a self-hosted `carbon.woff2` into `server/web/fonts/` — the
`@font-face` already points there and falls back to Spline Sans Mono if absent.
