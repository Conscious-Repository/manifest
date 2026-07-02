# Goals Page — Revert to the M1 UI (authoritative, supersedes the v1 redesign)

Decision: the M1 goals page (commit `307f473`, last seen at `ef83113`) was the
best version. **Revert the goals UI to it**, minus the MY PLATE column, rendered
over the **current** data model. This doc replaces the earlier redesign proposal
in this file; the mockup `goals-redesign-mockup.html` is obsolete (delete it).

## What this is

UI-only. The goals backend from `goals-build-spec.md` — ladder model, parser,
archive files, quarterly-review API, manifest write-back — is untouched. Only
`app/server/web/*` changes for the goals view.

## The page (single column)

Recover the M1 layout and interaction style from git (`ef83113:app/server/web/`),
with these deltas:

1. **MY PLATE column removed** — the goals page is just AREAS & GOALS, full
   width. (Leave the plate API alone; only the UI goes.)
2. **Tier mapping to the current model:** the nested cascade renders
   Rock (depth 0) → Stage (depth 1) → Task (depth 2) with the same always-
   expanded checkbox rows and add-child buttons as M1. Labels update:
   horizon label `ROCK → STAGE → TASK`; add buttons `+ Add rock`, `+ stage`,
   `+ task`.
3. **1-year goals:** one M1-style goal row per area, above the rocks, under a
   small `1-YEAR` label, with `+ Add 1-year goal`. No rollup badges.
4. **Due date picker removed** (the model has no `due::`). Owner chip stays as
   in M1 on depth 0–1.
5. **Closing a rock:** checking a rock's own checkbox completes it — confirm
   (`Archive "<title>"?`), then close with `outcome:: win` and move to the
   quarter archive file, exactly as the API already does. Each rock row also
   gets one small text action, `archive`, which closes it with
   `outcome:: learn` (optional note via the existing confirm-style prompt).
   The words Won/Drop/Learn appear nowhere.
6. **Everything else from the command-center UI is deleted:** the
   COMMAND/YEAR/REVIEW/HISTORY tabs, stepper, status chips/dropdown,
   needs-setup badge, Won/Drop pills, add-goal flow modal, soft-focus banner,
   and the header `+ Add goal` button (M1's per-area add buttons return, plus
   M1's `+ Add area`).
7. **Fields the UI doesn't render** (`quarter::`, `serves::`, `status::`,
   `rolled-from::`) must still round-trip untouched through every edit.

## Deliberately absent (bare minimum, by choice)

No history view, no review UI, no rollups, no status display. The archive files
and review API stay functional — reviews happen in markdown/Obsidian (or a
future UI) until wanted. If a quarter-turn nudge is ever desired it's a
one-line addition, not part of this revert.

## Acceptance

- Goals page looks and behaves like `ef83113` minus MY PLATE: area cards with
  name/North Star, always-expanded nested checkboxes, per-node add buttons.
- Renders the current goals.md; all inline fields survive edits byte-for-byte
  where unedited.
- Checking a rock (or its `archive` action) moves it to the quarter archive
  via the existing close API; stages/tasks check normally and manifest
  write-back still works.
- No tabs on the goals page. Zero references to Won/Drop in the UI.
