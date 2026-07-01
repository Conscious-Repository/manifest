# Feature Handoff — Goal Cascade + Manifest Pickers

Implements a cascading goal hierarchy (90-day → 30-day → tasks) and changes how
the daily manifest's Goals / Milestones / Tasks panels are filled: from
"auto-reflect" to "click-to-pick from your cascade." Built on the existing
`manifest/` app and the md-as-DB rules. Hand to Claude Code.

> **This revises two earlier decisions** (update the spec/build plan to match):
> 1. Manifest Goals/Milestones panels **no longer auto-populate**. On load they
>    are an empty scaffold; you pick into them (§4).
> 2. `goals.md` changes from flat `### 90-day` / `### 30-day` sections to a
>    **nested cascade** (§2).

> **Write boundary unchanged.** Everything here is *you* writing through the UI
> (authoring goals, picking the day's focus, checking a task) or editing in
> Obsidian. AI never authors goal content. The app persists *your* selections,
> exactly as it already persists schedule/tasks.

---

## 1. Confirmed decisions

- **Manifest Goals/Milestones panels are empty scaffolds** on load — no
  auto-populate.
- **Goal slots are generic** — clicking a Goals slot lets you pick from **all**
  your 90-day goals. (No dimensions/categories on the slots; the row icons are
  purely decorative markers.)
- **Each 90-day goal has exactly one 30-day goal** at a time → the Milestone slot
  auto-fills once the 90-day is picked.
- **Tasks panel = cascade tasks + freeform:** the chosen 30-day's tasks (auto if
  one, picker if several) plus any ad-hoc tasks you type.
- **Storage = nested Markdown lists** (§2).
- Three slots match the current layout; treat the slot count as configurable, not
  hardcoded.

---

## 2. Data model — the cascade in `goals.md`

A 90-day goal owns one 30-day goal, which owns any number of tasks. Represented as
nested Markdown checkboxes (Obsidian-native, Dataview/Tasks-parseable, reads as an
outline). Nesting depth carries the meaning:

```markdown
## Aion
> North Star: Bring aging under biomedical control.

### 90-day
- [ ] Series A fundraise [owner:: me] [due:: 2026-09-30]
    - [ ] Draft deck + diligence materials [owner:: me] [due:: 2026-07-31]
        - [ ] Intro to Founders Fund
        - [ ] Call with Lee
        - [ ] Follow-up with Shoumik
        - [ ] Publish blog on the RJ + Benjamin story
```

Parsing rules (deterministic):

- A checkbox under `### 90-day` = a **90-day goal**.
- The checkbox indented **one level** beneath it = its **30-day goal** (exactly
  one expected; if a user adds more, see §6).
- Checkboxes indented **two levels** beneath the 90-day (i.e. under the 30-day) =
  **tasks** (unlimited).
- **Inline fields** (`[key:: value]`) on any line: `owner`, optional `due`.
- Goals stay grouped by **area** (`## Aion`, `## OODA Group`, … — your existing
  structure, mirroring the vault's `categories`). No other classification.
- Serialization is deterministic and preserves indentation, unknown fields, and
  any prose the app didn't write. Round-trip must be idempotent.

This stays pure markdown: editable by hand in Obsidian and rendered as nested
tasks; the dashboard reads/writes the same structure.

---

## 3. Goals view — authoring the cascade (where you build it)

A dedicated Goals screen (not the manifest) for structured editing — no raw
syntax:

- **Outline per area:** list 90-day goals; expand one → its 30-day goal; expand
  that → its tasks.
- **Add/edit 90-day goal:** title, `owner`, optional `due`.
- **Add/edit the 30-day goal** under a 90-day (one); title, owner, due.
- **Add as many tasks as you want** under the 30-day; check/uncheck; reorder.
- Every edit writes the nested markdown above to `goals.md` (your UI action).
- **My Plate** continues to aggregate `owner:: me` open items across the cascade.

API (extends the goals endpoints): CRUD for 90-day / 30-day / task nodes with
parent references; check/uncheck. All deterministic markdown writes.

---

## 4. Manifest interaction — scaffold + pickers

The daily core page. On load, panels show an **empty scaffold** (no
auto-populate), exactly like the mock.

### Goals panel (3 generic slots)
- Each slot is empty until picked (icon is decorative).
- **Click a slot → modal/dropdown** listing **all your 90-day goals**
  (`owner:: me`, not done), grouped by area for scanability. Choose one → the slot
  fills with the goal title (blue, like a filled entry).
- You can fill up to 3 slots (3 focus goals for the day). Re-clicking changes the
  pick. Picking writes the selection to the day's manifest block (§5).

### Milestones panel (aligned to the filled Goals slots)
- When a Goals slot is filled, the aligned Milestone slot **auto-fills** with that
  90-day goal's single 30-day goal (read-only reflection).
- If a 90-day somehow has >1 30-day (shouldn't, per decision), the slot becomes a
  picker like the Goals slot. Otherwise no interaction needed.

### Tasks panel (cascade tasks + freeform)
- For each filled cascade, surface the chosen 30-day's **open tasks**:
  - **exactly one open task → auto-fill** a task row ("my task for that
    portion");
  - **several → the row is a picker**: click → select from that 30-day's tasks
    (same select UX as the Goals dropdown).
- **Freeform still works:** type ad-hoc tasks (the existing "+ Add new").
  Cascade-linked tasks show their lineage (a small goal tag) so you know which are
  tied to a goal.
- **Checking a task is manual and does NOT auto-check its goal** (per your
  evening-ritual decision). It updates the task's checkbox in `goals.md` (cascade
  tasks) or the daily note (freeform) — your action.

### What does NOT change
- The **schedule** still auto-populates from Google Calendar (timed events, skip
  all-day/multi-day). Separate from this goals/milestones change.
- Day navigation (‹ / ›) unchanged; each day starts as a fresh scaffold.

---

## 5. Persistence in the daily note

The day's manifest block gains a **Focus** subsection recording the picks, in
readable markdown, written only on your pick actions:

```markdown
<!-- manifest:start -->
## Focus
- Series A fundraise
  - milestone: Draft deck + diligence materials     (auto from the cascade)
  - task: Intro to Founders Fund                     (your pick / auto)
- <second 90-day goal, if picked>
- <third 90-day goal, if picked>

## Schedule
...
## Tasks
- [ ] Intro to Founders Fund   [goal:: aion/series-a/draft-deck]
- [ ] <freeform task>
<!-- manifest:end -->
```

- Selections reference the goal by a **stable slug** (e.g. `aion/series-a`) the
  app derives from area + title, so re-resolution survives minor edits; if a title
  changes and the slug breaks, the slot shows "unresolved" rather than guessing.
- Cascade tasks promoted into the day carry a `[goal:: <slug>]` backlink.
- This is the same delimited block that holds schedule/tasks; the app only writes
  between markers and never touches your journal.

---

## 6. Edge cases & rules

- **No 30-day yet** under a picked 90-day → Milestone slot shows an empty/"set a
  30-day goal" state linking to the Goals view; no fabrication.
- **No tasks** under the 30-day → Task row stays freeform.
- **Multiple 30-day goals** under one 90-day (data drift) → surface it (Milestone
  becomes a picker) rather than silently choosing; optionally flag in the Goals
  view. Never auto-merge.
- **Goal text changed** since a day was planned → resolve by slug; if unresolved,
  show the stored text greyed with an "unresolved" badge.
- **Same goal picked in two slots** → allow but warn.
- All matching is **exact**; inconsistencies are surfaced, not normalized
  (consistent with the vault-audit decision).

---

## 7. Acceptance criteria

- Authoring a 90-day → one 30-day → N tasks in the Goals view produces the nested
  markdown in §2; re-parsing is idempotent; hand-edited nesting in Obsidian reads
  back correctly.
- Manifest loads as an empty scaffold (no auto-populate of goals/milestones).
- Clicking a Goals slot lists all 90-day goals (grouped by area); picking one
  auto-fills the aligned Milestone with the single 30-day.
- A 30-day with one open task auto-fills a task row; with several, the row is a
  picker; freeform tasks still addable.
- Checking a task never auto-checks its goal.
- Selections persist to the day's manifest block by slug and re-resolve on reload;
  journal prose untouched.
- Schedule calendar auto-populate still works and is unaffected.
