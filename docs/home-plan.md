# Shared Home plan

The Home timeline (TASKS › Timeline in Olga's Manifest, TASKS › Home timeline
in the main app) reads one document: `system/home/plan.json`. It holds the
schedule for the shared Home tasks: availability, reservations, milestones,
estimates, dependencies, allocations, decisions and scenarios. It sits beside
`system/home/tasks.md` and refers to tasks only by ID.

The task system stays the authority for titles, completion, descriptions and
comments. The plan never copies them. Notes (`system/home/notes/<hash>/`) are
untouched by the plan; the timeline reads them as collapsible text.

## Calls

Both servers mount the same handlers. The owner's Manifest is at
`https://metis.tail8f89de.ts.net`. Olga's is at `https://manifest.olgasobkiv.com`
and needs her sign-in cookie; cross-origin writes are refused there.

| Call | Body | Returns |
| --- | --- | --- |
| `GET /api/home/plan[?asOf=YYYY-MM-DD]` | — | `{revision, plan, derived, tasks}` |
| `POST /api/home/plan` | `{revision, patch}` | the same, after the write |
| `POST /api/home/plan/preview` | `{patch}` | the same for saved + patch; **writes nothing** |
| `GET /api/home/plan/history` | — | `{revision, history: [file names, newest first]}` |
| `POST /api/home/plan/restore` | `{revision, name}` | the plan, restored from history |

- `revision` is the SHA-256 of the file's bytes, and `""` means there is no plan yet.
- `tasks` is every shared Home task as `{text, done}`, keyed by ID. Private
  areas never appear in it.
- `derived` is computed on every read and never stored:
  - `weeks`: Monday-first.
  - `capacity` totals, including `unknownItems` and `waitUnknown`.
  - `items`: tasks and subtasks, flattened.
  - `conflicts`, `budget`, and `scenarios`, where each scenario has its own capacity and conflicts.
- `asOf` defaults to today in the plan's timezone. Weekends that are already
  past leave the pool.

### Writing: a JSON merge patch against a revision

`patch` is an RFC 7396 merge patch:
- Objects merge key by key, so you send only what changes.
- `null` deletes a key. For an estimate, deleting `hours` makes it unknown again.
- Arrays (`dependsOn`, `options`, `who`) are replaced whole.

The merged document is decoded strictly, so an unknown field is refused. It is
then validated as a whole before it is written.

| Status | Meaning | What to do |
| --- | --- | --- |
| 200 | Written | The response carries the new revision. |
| 409 `{error, revision}` | The plan changed since your revision | Re-read, re-apply your change, send again. Nothing was written. |
| 400 `{error, problems: [...]}` | Refused | Fix every listed problem. Nothing was written. |

Every write takes the interprocess lock on `system/home/plan.json` (the same
`flock` the shared Home files use). Olga's server, the owner's server and any
other process therefore serialize, and one revision can be won only once.
Before each write, the replaced bytes are saved to
`system/home/plan-history/<UTC time>-<old revision prefix>.json`. A write that
changes nothing keeps the revision and adds no history.

### Examples

```sh
API=https://metis.tail8f89de.ts.net
# read
curl -s "$API/api/home/plan" | jq '{revision, capacity: .derived.capacity}'

# change one estimate (36 h) — send only the field
REV=$(curl -s "$API/api/home/plan" | jq -r .revision)
curl -s -X POST "$API/api/home/plan" -H 'Content-Type: application/json' -d '{
  "revision": "'"$REV"'",
  "patch": {"tasks": {"home/roof-on": {"estimate": {"hours": 36, "basis": "user", "confidence": "confirmed"}}}}}'

# back to unknown
… -d '{"revision": "…", "patch": {"tasks": {"home/roof-on": {"estimate": {"hours": null}}}}}'

# place 8 h of pane work on the Nov 7–8 weekend; remove it again with null
… -d '{"revision": "…", "patch": {"tasks": {"home/plan-windows": {"subtasks": {"panes": {"allocations": {"2026-11-07": 8}}}}}}}'

# record a decision
… -d '{"revision": "…", "patch": {"tasks": {"home/plan-windows": {"decisions": {"pane-supplier": {"status": "decided", "answer": "D&J"}}}}}}'

# a what-if, never the baseline
… -d '{"revision": "…", "patch": {"scenarios": {"roof-44": {"label": "Roof 44 h", "basis": "assistant", "patch": {"tasks": {"home/roof-on": {"estimate": {"hours": 44}}}}}}}}'

# preview a change without writing it
curl -s -X POST "$API/api/home/plan/preview" -H 'Content-Type: application/json' \
  -d '{"patch": {"tasks": {"home/roof-on": {"estimate": {"hours": 44}}}}}' | jq .derived.capacity
```

To add a task to the plan, create it in Home first: `POST /api/tasks/item
{text, domain: "Home", rock: "home/backyard"}` returns its ID. Then patch
`tasks["<id>"]`. The plan refuses an ID that is not a shared Home task. If a task
is later removed from Home, the plan keeps its entry and flags it as `missing-task`.

## Schema `manifest.home-plan/1`

All dates are `YYYY-MM-DD`. All arithmetic is date-only, so a daylight-saving
change never adds or loses an hour. Hours are **elapsed** household hours with
the people working together: two people working 8 hours is 8, never 16.
Unknown is `null`, never `0`.

```jsonc
{
  "schema": "manifest.home-plan/1",
  "project": "home/backyard",                 // the goals rock served
  "timezone": "America/Chicago",              // display + default asOf
  "horizon": {"start": "2026-10-05", "end": "2026-12-21"},
  "people": {"benjamin": {"name": "Benjamin"}, "olga": {"name": "Olga"}},
  "capacity": {
    "weekendDayHours": 8,                     // each Saturday and Sunday, together
    "eveningsPerWeek": 2, "eveningHours": 3,  // planning/ordering only
    "note": "…", "solo": "what a one-person weekend may hold (never baseline)"
  },
  "away":   {"<id>": {"from": "…", "to": "…", "who": ["benjamin"], "note": "…"}},
  "events": {"<id>": {"date": "…", "hours": 4, "title": "…", "task": "<home task id>", "note": "…"}},
                                              // hours taken out of one day
  "reservations": {"<id>": {"weekend": "<Saturday>", "hours": 16, "purpose": "…",
                    "kind": "work|checks|contingency", "status": "provisional|confirmed", "note": "…"}},
  "milestones": {"<id>": {"date": "… or omitted", "title": "…",
                  "kind": "deadline|target|external|later", "confirmed": false,
                  "dependsOn": ["<ref>"], "note": "…", "source": "…"}},
  "tasks": {"<home task id>": {
      "phase": "planning|procurement|execution|later",
      "draws": "pool|evening|outside|none|reservation:<id>",  // where its hours come from
      "includedIn": "<ref>",                  // effort counted inside another item's estimate
      "owner": "…", "nextAction": "…", "order": 1, "note": "…", "source": "…",
      "estimate": {"hours": null, "low": 24, "high": 56, "crew": 2,
                   "basis": "user|assistant|supplier|crew|unknown",
                   "confidence": "confirmed|allowance|guess|unknown", "excludes": "…", "source": "…"},
      "wait": {"days": null, "label": "supplier lead time"},  // elapsed, not household work
      "window": {"start": "…", "end": "…"},   // outside/crew dates: planned, never proof
      "dependsOn": ["<ref>"],
      "allocations": {"<Saturday>": 8},       // baseline weekend placement
      "minTempF": 40,                         // lowest application/cure temperature of its materials
      "subtasks": {"<id>": {"title": "…", "done": false, /* the same item fields */}},
      "decisions": {"<id>": {"question": "…", "status": "open|decided|deferred",
                     "options": ["…"], "answer": "…", "note": "…"}},
      "links": {"<id>": {"label": "…", "href": "https://…"}}
  }},
  "decisions": {"<id>": { /* project-wide, same shape */ }},
  "budget": {"total": 10000, "currency": "USD", "note": "…",
             "lines": {"<id>": {"label": "…", "amount": null, "status": "estimate|quote|committed|paid|unknown"}}},
  "scenarios": {"<id>": {"label": "…", "basis": "assistant|user", "order": 1, "note": "…",
                 "patch": { /* a merge patch over this plan */ }}},
  "climate": {"source": "…", "normals": {"<Saturday>": {"high": 59, "low": 41}}},  // °F normals, not a forecast
  "source": "provenance of the import"
}
```

A `<ref>` is `"<task id>"`, `"<task id>#<subtask id>"` or `"milestone:<id>"`.

### Rules the server enforces (400)

- The schema string and valid dates; the horizon runs start ≤ end.
- A task key must be a shared Home task when it is added.
- `draws: "evening"` is never execution work and is never allocated to weekends.
- Allocations are on Saturdays inside the horizon, with hours > 0.
- No negative hours; `high ≥ low`.
- Every reference resolves. Dependencies may not form a cycle; the error names it.
- Links are `http(s)` only.
- Scenarios need a label and a basis.

### What the derivation counts

- **A weekend's shared hours** count only the days when every person is home.
  Each such day contributes `weekendDayHours` minus any `events` that day.
  Days where only some people are home appear as `soloHours`, outside the
  baseline.
- **The pool** is the shared hours of upcoming weekends minus their reservations.
- **Known demand** is open (not done) items with `draws: "pool"` and a known
  `hours`, not `includedIn` another item. Items with no estimate are listed
  in `unknownItems`, with allowance totals, and are never counted as zero.
- **Remaining** is pool minus known demand. A negative value stays negative
  and also appears as an `overdemand` conflict.
- **Person-hours** are shown (`hours × crew`) but never used as capacity.
- **Conflicts:**
  - `overallocated`: a weekend's reservations plus allocations exceed its
    shared hours (the contingency included), or an item has more placed than
    estimated.
  - `unavailable`: placement on a weekend that is not shared. A one-person
    task (crew 1) may use a solo weekend.
  - `order`: placed before something it waits on.
  - `deadline`: placed after the hard deadline.
  - `missing-task`: the task left Home.
  - `cold`: an item with `minTempF` is placed on a weekend whose normal high is below `minTempF + 5` °F.
    The high lasts only a few hours, and mornings are colder.
- **Scenarios** are derived separately and never change the saved plan.
- **`derived.sequence`** is the assistant sequence: open weekend work
  (`draws: "pool"`, not done, not `includedIn`) laid onto shared weekends in
  dependency order. It works around reservations and saved placements, and
  skips weekends that are `cold` for the item.
  - **Hours:** known hours are used where they exist; otherwise the allowance's
    high end.
  - **Dependencies:** a dependency with no date (an outside milestone, evening
    work) is treated as ready, and listed in `assumptions`.
  - **Result:** `placements`, `unplaced` (with reasons), `finish`, `fits` and
    `spareHours`.

  It is recomputed on every read, for each scenario too, and never written.

## In the UI

- **Drafts:** every edit in the timeline or a task's schedule goes into a
  draft first. The draft is kept in that browser (it survives reloads and a
  re-sign-in), previewed through `/preview`, and drawn dashed. "Save to plan"
  re-reads the plan first. If any field the draft touched has changed since,
  each collision is listed and nothing is written until the person chooses
  "Keep mine" or "Discard mine". "Save as scenario" stores the draft as a user
  scenario instead.
- **Scenarios** are read-only what-ifs. While one is shown, the view says so.
- **Where the editor lives:**
  - Olga's task details hold the schedule editor (fields, subtasks, decisions,
    links), with the notes as collapsible text.
  - The main inspector shows a schedule summary, with "Edit schedule".
- **Layout:** desktop shows a weekly grid through the deadline; phones show a
  chronological agenda with the same data.

## Import (2026-10-07) and rollback

**The import.** The plan was created once, through the API itself (revision
`""`), from the planning-chat handoff, after checking it against the live
task notes. The notes' "CURRENT calendar exception — neighborhood planting
October 17" is newer than the handoff and was applied:
- **The planting event:** 4 h out of Oct 17.
- **Capacity:** 124 h together.
- **Prep/coating reservation:** 12 h on Oct 17–18 plus a 4 h spill onto Oct 31.
- **Pool:** 76 h.
- **Roof scenarios:** 24/12/0/−8 h left.

**What the import did not touch.** No note, comment, task or goal was changed.
Historical statements stay in each task's notes; active fields cite the note
section they came from (`source`). Re-running the import is a 409, never a
duplicate.

**Rollback.**
- **Undo one change:** `POST /api/home/plan/restore {revision, name}` with a
  name from `/history`.
- **Remove the plan:** delete `system/home/plan.json`. The tasks and notes are
  unaffected; the timeline then shows "No shared Home plan yet".
- `plan-history/` keeps every replaced revision and is never pruned
  automatically.
