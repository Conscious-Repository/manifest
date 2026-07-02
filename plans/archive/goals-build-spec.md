# Goals — Consolidated Build Spec (authoritative, v2)

The single source of truth for the goals system. Supersedes `goal-cascade-feature.md`, `goals-progress-spec.md`, `goals-lifecycle-archive-spec.md`, and the v1 consolidated spec — hand Claude Code this one document for goals.

**v2 changes:** added §0 (codebase reality + silent auto-migration), split the build into two phases, pinned the stage/task depth rule (no implicit stages), defined task write-back identity, made annual roll-up's archive dependency explicit, fixed the `reached::` format, and defined canonical field emission for idempotent round-trips.

## Executive framing (the whole point)

* The manifest stays exactly as it is. You love it; don't touch its UI. Goal richness lives on the goals page; agent proposals live in an approvals inbox. Nothing about goals clutters the daily cockpit.
* The goals page is the command center — see everything, add goals, track progress.
* Markdown is the source of truth (`goals.md` + archive files). The EA proposes; the user commits. AI never writes goals directly.

## 0. Codebase reality & migration (read first)

This is a **rework of the existing `goals` package**, not a greenfield build. What exists today and what changes:

* `goals/model.go` models a 90-day → 30-day → task cascade. Rework to: Area → North Star → 1-year → Rock → stage → task (§1). `Goal`/`Area`/`Doc` and the round-trip machinery (preamble, `extra` lines, unrecognized-field preservation) are good foundations — keep them.
* `goals/fields.go` canonicalizes `owner` / `due` / `goal`. **Retire `due::`** (deliberate — no dates on goals). Recognized fields become: `owner`, `goal`, `quarter`, `serves`, `status`, `rolled-from`. Emission rules in §1.
* `daily.go:169` still builds `Manifest/Goals-<quarter>.md` period-note paths. **Remove this path**; the single `goals.md` (already what `goals/store.go` resolves via the vault index) is the only live file. Quarterly files exist only as close-time archives (§6).
* `server/goals.go` + `server/goalsadapter.go` adapt goals to the manifest cascade picker — update for the new hierarchy (§4), no manifest UI change.
* The `approvals` package (pending/approved/rejected folder store) is the inbox backend. Reuse as-is; Phase 2 fills it.

**Migration (silent, one-time):** on first load, if `goals.md` parses as the old format (90-day/30-day headings, `due::` fields), auto-convert in place: old 90-day roots → Rocks (`quarter::` = current quarter), their 30-day children → stages, grandchildren → tasks; strip `due::`; create empty `### 1-year` sections per area (Rocks get no `serves::` — the needs-setup nudge handles linking later). Before the first migrated save, write a one-time backup beside it (`goals.md.pre-migration`). No prompts, no migration UI.

## Phasing

One spec, two independently testable phases:

* **Phase 1** — model rework + migration (§0–§1), progress model (§2), goals page (§3), manifest tie-in (§4), lifecycle/archive + history view (§6), manual quarterly review UI (§7 minus EA prep). Approvals inbox UI ships here (backend exists) but sits empty.
* **Phase 2** — the EA loop (§5): signal reading (Calendar, Gmail, vault + research feed, fundraising sheet), evidence-cited proposals into the inbox, and EA-prepped quarterly review (§7 step drafting).

## 1. Structure

Horizons per life-area, laddering up. Stages are emergent (added as you go).

```
Area (## Aion / ## OODA Group / ## Home / ## Personal)
  North Star            timeless vision (already written)
  1-year goal           annual objective; progress rolls up from its Rocks
    Rock (90-day)        the priority; ~5 total active across all areas
      Stage             a milestone, added as you reach it — a growing trail
        Task            the work under the current stage
```

`goals.md` holds the live active set (active annual goals + active Rocks + stages/tasks). It is what the manifest reads. It is never partitioned by quarter.

```markdown
# Goals

## Aion
> North Star: Bring aging under complete biomedical control.

### 1-year — 2026
- [ ] Series A closed + first program in vivo [goal:: aion/2026]

### Rocks (90-day)
- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [serves:: aion/2026]
    - [x] Soft lead identified            # completed stage (trail)
    - [ ] Term sheet                        # current stage
        - [ ] Send updated deck
        - [ ] FF partner meeting
```

**Depth rule (literal, no exceptions):** a checkbox one level under a Rock is a stage; two levels under is a task. There are no implicit or placeholder stages. A hand-typed to-do directly under a Rock parses as a (small) stage — acceptable degradation, never data loss. The UI only offers "add task" under a stage; a stageless Rock's primary affordance is "name the first stage."

**Fields & canonical emission (idempotent round-trip):**

* `[goal:: slug]` — identity; always written.
* `[owner:: …]` — written only when not "me" (existing behavior).
* `[quarter:: 2026-Q3]` — always written on Rocks; set to current calendar quarter at creation; updated at review on carry.
* `[serves:: <annual-slug>]` — written when linked; absence = needs setup.
* `[status:: blocked|at-risk]` — written only when not active; absence = active.
* `[rolled-from:: 2026-Q2]` — set on carry.
* No metrics, no `done::` — deliberate. Unrecognized fields round-trip untouched.

## 2. Progress model (stage-based, emergent)

* Progress = where you are in the trail: completed stages + the current stage's name (first unchecked stage). No fixed % or denominator — the stepper simply grows.
* Done is explicit. A Rock is marked **Won** when its intent is met (by the user, or an approved EA proposal). Dropping it is a **Learn**. Reaching any particular stage never auto-completes.
* Status (`active` / `blocked` / `at-risk`) shown per Rock; the EA may propose changes (Phase 2).
* **Annual roll-up:** an annual goal's progress = Rocks that `serves::` it — active ones from `goals.md` **plus Won/Learn ones from the current year's archive files**. The roll-up computation must read the archives; `goals.md` alone is not sufficient for it (it is sufficient for everything the manifest reads).

## 3. The goals page — command center (grouped by area)

Primary view: sections per area. Each area shows its North Star (quiet header), its 1-year goal with rolled-up progress, and its Rocks — each a row with: current stage, completed-stage trail (growing stepper), next action (first unchecked task of the current stage), status chip, last-movement date.

Interactions:

* **Add a goal** — both modes side by side: Quick capture (title + area → in, marked "needs setup" until it has a stage and a `serves::` link) and Full setup (area → annual goal it serves → first stage → owner).
* **Add the next stage as you go:** finishing the current stage prompts naming the next; the trail grows. No pre-planned pipeline.
* **Track progress:** check tasks, complete a stage (checks it, prompts the next), mark a Rock Won.
* **Soft focus guidance:** >~5 active Rocks total flags "consider deferring" — never blocks.
* **Secondary views:** Year view (1-year goals with Rocks laddered beneath) and Archive/History (§6).

All editing is no-syntax UI over the markdown; hand edits in Obsidian read back.

## 4. Manifest tie-in (no UI change)

The manifest is unchanged. Invisible plumbing only:

* Picking a Rock in the manifest (existing cascade picker, via `goalsadapter.go`) fills its **current stage** into the milestone slot and offers that stage's tasks.
* A goal-linked task carries `[goal:: slug]` (the Rock's slug). Ticking it in the manifest flows back into `goals.md`. **Write-back identity:** match by exact task text under the Rock's current stage. On no match (task was reworded/moved in Obsidian), the write-back is a **no-op and a note lands in the approvals inbox** ("couldn't sync tick: …") — never guess, never write elsewhere.
* Completing the last task of a stage does **not** auto-advance the stage; the EA proposes "advance to next stage" (Phase 2) or the user does it on the goals page. Stage-moves stay deliberate.

## 5. The EA loop + approvals inbox (Phase 2)

The approvals inbox (existing `approvals` store; its own place in the dashboard — not the manifest) is where every EA proposal is triaged. The EA reads Google Calendar, Gmail, the vault + research feed, and the fundraising sheet (read-only) and proposes, each with cited evidence:

* Advance stage ("→ Term sheet — Gmail 'FF term sheet' Jul 2 + sheet row").
* Next-action suggestions for a stalled Rock.
* At-risk / blocked flags (no stage movement in N days — default 14 — and nothing upcoming).
* Mark Won + archive when momentum indicates completion.
* Quarter-review prep (§7).

Approve → the dashboard writes `goals.md` (the user's commit). The EA never writes goals directly. A small count on the inbox is the only cross-surface signal; the manifest stays untouched.

## 6. Lifecycle & archive (event-driven, flat files)

Every goal: draft → active → closed → archived. A goal leaves `goals.md` only when it closes:

* **Win** — marked complete (user, or approved proposal).
* **Learn** — dropped/deprioritized; the stage it reached is recorded.

On close, it moves out of `goals.md` and is appended to a flat, root-level archive file named for the quarter it closed in: `goals 2026-Q3.md` (matches the dated-note style). Archives are read-only history.

```markdown
# goals 2026-Q3

## Aion
- Series A 15M [goal:: aion/series-a-15m] [outcome:: win] [closed:: 2026-08-14] [reached:: Term sheet]
- Consumer MRI push [goal:: aion/consumer-mri] [outcome:: learn] [closed:: 2026-09-30] [reached:: Diligence] [note:: deprioritized behind Series A]
```

`reached::` is the name of the last stage in the trail (completed or current at close) — nothing else; "Won" is `outcome::`, not a stage.

The Archive/History view groups by quarter and shows Win rate over time, carries, and drops. A multi-quarter Rock stays active in `goals.md` (gaining `rolled-from::` when carried) and only lands in an archive file when it actually closes.

## 7. Quarterly review (fixed calendar quarters)

Anchored to Jan / Apr / Jul / Oct — the 90-day heartbeat. It edits the live set; it never swaps files. Phase 1 ships the review UI (user-driven); Phase 2 adds EA prep into the inbox. Steps:

1. Win / Learn per Rock (from its stage trail; evidence cited in Phase 2).
2. Carry / close per goal — carry (stays active, gets `rolled-from::`, `quarter::` updated), close as Learn (→ archive), or mark Won (→ archive).
3. Retro (optional, lightweight): Start / Stop / Keep — three short lines. Skippable.
4. Next quarter's Rocks — drafted from 1-year goals (+ signals in Phase 2), ready before quarter end; accept/edit.
5. Soft-focus check — flag if >~5 Rocks would be active.

A lighter annual review resets the 1-year goals (archived the same way); carried Rocks get their `serves::` re-pointed to the new annual slug as part of that review.

## 8. Write boundary (unchanged)

`goals.md` and the `goals <quarter>.md` archives are knowledge-vault content. The EA reads signals and the vault read-only and writes only proposals to the approvals inbox. Only the user commits — approving a proposal, or editing in Obsidian. Moving a closed goal into an archive file happens on user confirmation.

## 9. Acceptance criteria

**Phase 1**

* Old-format `goals.md` auto-migrates on first load (90-days → Rocks, 30-days → stages, `due::` stripped, backup written); already-new files pass through untouched.
* `goals.md` parses Area → North Star → 1-year → Rock → stages → tasks; canonical field emission (§1) round-trips idempotently; hand edits read back; the literal depth rule holds (a lone checkbox under a Rock is a stage).
* Goals page renders by area: stage trail, next action, status chip, last-movement per Rock; annual goals show roll-up computed from `goals.md` + current-year archives.
* Quick-capture and full setup both work; stages addable any time; Rocks complete only via explicit Won.
* Ticking a `[goal::]`-linked task in the manifest updates the exact-text-matched task under the current stage; a miss is a no-op that surfaces an inbox note; zero manifest UI change.
* Closing a goal moves it to `goals <quarter-it-closed>.md` with `outcome::`, `closed::`, `reached::` (last stage name); History view groups by quarter with Win rate; `goals.md` is never bulk-periodized; the `Goals-<quarter>.md` period-note path is gone.
* Manual quarterly review offers Win/Learn, carry/close per goal, optional Start/Stop/Keep, next-quarter drafting; soft-flags >~5 active Rocks.

**Phase 2**

* EA proposals (advance stage, next action, at-risk/blocked, mark-Won, review prep) land in the approvals inbox with cited evidence; approving writes `goals.md`; the EA never writes it directly.
* At-risk default: 14 days of no stage movement with nothing upcoming.

## 10. Open (non-blocking)

1. "Needs setup" nudge styling for quick-captured goals missing a stage / `serves::` link.
2. Exact copy/UX of the stage-naming prompt when engaging a stageless Rock.

Sources: [Traction / EOS 90-Day World](https://www.ninety.io/eos/blog/the-eos-quarterly-meeting-how-to-operate-in-a-90-day-world), [Claude Code /goal](https://code.claude.com/docs/en/goal), [OKR retrospective](https://mooncamp.com/blog/okr-retrospective).
