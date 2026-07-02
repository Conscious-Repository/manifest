# Manifest

A local, single-binary daily-planning app — modeled on the
[Visualize Value manifest](https://vv.xyz/manifest) UI — that reads and
writes plain markdown inside your Obsidian vault. The schedule, goals,
milestones, and tasks live in your normal daily notes, so you can edit
them in the web UI or directly in Obsidian and they stay in sync.

Your journal is never touched or shown. The app only owns a small,
clearly delimited block inside each note.

## Layout

```
manifest/
├── main.go              entrypoint, flags, server bootstrap
├── config.go            config loading + defaults
├── store.go            markdown parse / serialize, vault read/write, streak
├── server.go            HTTP API + embedded web UI
├── store_test.go        round-trip + journal-preservation tests
├── config.example.json  copy to config.json and edit
└── web/                 the UI (index.html, style.css, app.js)
```

## How your vault is used

Three kinds of notes (paths are configurable):

| Data | File | Why |
| --- | --- | --- |
| Schedule + tasks | `Daily/2026-06-29.md` (your daily note) | Date-specific; lives beside your journal |
| Goals | `Manifest/Goals-2026-Q2.md` | Quarterly; persists across every day in the quarter |
| Milestones | `Manifest/Milestones-2026-06.md` | Monthly; persists across the month |

Inside the **daily note**, the app owns only the region between markers
and leaves everything else (your journal, frontmatter, other headings)
exactly as it was:

```markdown
---
tags: [daily]
---

# 2026-06-29

Woke up early. Felt good about the day ahead.   ← your journal, untouched

<!-- manifest:start -->
## Schedule

| Time | Focus | Focused |
| --- | --- | --- |
| 8:00A |  |  |
| 8:30A |  |  |
| 9:00A | Deep work on the parser | x |
| 9:30A | Deep work, continued |  |
| 10:00A |  |  |
| 10:30A |  |  |
| 11:00A | Standup meeting |  |

## Tasks

- [ ] Ship manifest v1
- [x] Review the PR
<!-- manifest:end -->
```

The schedule runs at **half-hour granularity** — every hour has a `:00`
and a `:30` slot, matching the VV manifest. In the web UI each filled
slot draws a faint vertical connector down to the next filled entry,
labelled with the elapsed duration (`30m`, `1h`, `1.5h`, `2h`), so a
block's length is visible at a glance. The "Was I focused?" circle is one
per hour.

Because the schedule is a normal markdown table and tasks are normal
checkboxes, you can edit them by hand in Obsidian (tick a box, type a
plan, even add a `7:00P` row) and the web UI reads it back on next load.
A bare hour like `9A` is accepted and normalized to `9:00A`.

## Configure

```bash
cp config.example.json config.json
```

Edit `config.json`:

```json
{
  "vaultPath": "~/Obsidian/MyVault",
  "dailyNoteDir": "Daily",
  "dailyNoteFormat": "2006-01-02",
  "periodNoteDir": "Manifest",
  "scheduleStart": 8,
  "scheduleEnd": 18,
  "port": 7777
}
```

- `dailyNoteFormat` is a Go time layout. Match it to your Obsidian Daily
  Notes setting — e.g. `2006-01-02` → `2026-06-29.md`, or
  `2006/01/2006-01-02` for date-foldered notes.
- `scheduleStart`/`scheduleEnd` are 24-hour bounds (8..18 = 8A–6P).

## Run

```bash
go run .
# or build a single binary:
go build -o manifest . && ./manifest
```

Then open <http://127.0.0.1:7777>. Flags override the config:

```bash
./manifest -vault ~/Obsidian/MyVault -port 8080
```

Edits autosave (debounced) as you type and toggle. The streak counts
consecutive days back from the current date whose daily note has a
manifest block with any focused hour or any task.

## Design / fonts

The UI mirrors the original VV manifest's exact design tokens — Hanken
Grotesk for entries, a Carbon-style mono for the time labels and pills,
the `#265ACC` blue for filled entries, `#e5e5e5` hairlines, the dark nav
pills and the rounded streak pill. Hanken Grotesk and the Spline Sans
Mono fallback load from Google Fonts automatically.

The original's exact label font is **Carbon** (self-hosted, not on Google
Fonts). To match it 1:1, drop a `carbon.woff2` into `web/fonts/` — the
`@font-face` in `style.css` already points there and falls back to Spline
Sans Mono if it's absent.

## Test

```bash
go test ./...
```

Covers time-token round-tripping, journal/frontmatter preservation,
no-duplicate re-saves, reading back hand edits made in Obsidian, and the
goals/milestones period notes.

## Where this goes next

The data model is deliberately boring markdown, so a future AI dashboard
can read the same files: summarize the week's focus, draft tomorrow's
schedule from your goals, or surface stale milestones. The HTTP API
(`GET/POST /api/day`, `POST /api/goals`, `POST /api/milestones`) is the
seam to build on.
