# Manifest UI/UX audit — 2026-09-05

The strongest opportunity is making the existing controls and panels behave
consistently. The Jarvis theme already has a distinct visual identity; this pass
preserves its palette, grid, typography, panel borders, glow, and navigation model.
It does not implement the proposed new workspaces or change approval semantics.

## Shipped in this pass

| Finding | Change | Scope |
| --- | --- | --- |
| Feed reserved a 300px inspector showing irrelevant editing instructions | Reserve it only when connected editable proposal cards exist; release it when switching to Consume | Feed, desktop inspector, existing phone sheet |
| Long phone card titles were squeezed between multiple badges | Metadata wraps; the title takes a full row beneath it | Shared Feed card header on phones |
| No consistent keyboard focus treatment | Shared inset accent ring and reduced-motion support | Shared primitives, both themes |
| Collapsible sections were mouse-only divs | Native buttons with expanded state and associated section IDs | Consumers of `collapsibleSection` |
| Chat logo was a clickable span; collapsed navigation lost useful names | Real link, named destinations, current-page state; collapse control updates its name and expanded state | Global rail |
| Some compact selectors were too short and their active state was mostly a border-color change | 24px minimum compact target and neutral selected fill | Shared filter chips and rail controls; phone touch sizing retained |
| Header action groups could resist wrapping | Allow shared headers/actions to wrap | Shared page header |
| Search/Cast did not contain keyboard focus or reliably restore it | Shared focus lifecycle, inert background, visible close controls, accessible dialog names | Search and Cast only |
| Search task-capture row looked selectable but was outside arrow-key navigation | Include it in the result model; retain initial selection of the best existing match | Command palette |
| Palette selection could scroll out of view and lacked selected-result semantics | Scroll keyboard selection into view; combobox/listbox state | Search and Cast |
| Cast confused a failed load with no available skills, and late responses could replace newer state | Distinct loading/error/no-match states; preserve typed query; request generations reject stale responses | Cast; Search also rejects superseded requests |
| Native editor backgrounds and filled-button hover rules could clash with Jarvis | Explicit token backgrounds; preserve on-accent text on Cast hover | Palette inputs and raw editor background |

## Highest-value follow-up areas

1. **Feed action hierarchy and duplicate events.** A task reply and its portal
   notification can both describe the same event. Group by underlying work/result,
   retaining provenance in disclosure. Define one primary next action and keep
   secondary actions subordinate. This requires event identity and action policy,
   not just CSS. Do this alongside the planned preparation/approval flow.
2. **Task-to-work continuity.** Current task rows remain a launch/list surface.
   Add task-specific workspaces using the established shell and shared panel
   anatomy. Save pane selection, document position, and draft state. Keep source
   evidence and approval editing adjacent to the artifact being worked on.
3. **Modal and selection consistency beyond this pass.** Quick capture, pickers,
   raw editing, and custom tab-specific overlays still need individual lifecycle
   audits. Adopt the shared focus helper where they are truly modal. Review
   custom toggles for `aria-pressed`/`aria-selected`; CSS class changes alone do
   not expose state to assistive technology.
4. **Text contrast and density by role.** Small muted metadata is pervasive,
   especially in the default theme. Measure essential labels and controls against
   their actual backgrounds before changing ramp tokens globally. Retain quiet
   decoration, but distinguish it from text someone must read to act. No blanket
   WCAG conformance claim is made by this audit.
5. **State and refresh contract.** Continue replacing silent failures with useful
   recovery paths. Exercise polling while editing, stale selections, and pending
   actions in each new workspace. Shared interaction primitives need browser
   regression coverage beyond the repo's existing static JS/CSS guard tests.

## Guide for new surfaces

The implementation contract is [UI conventions](ui-conventions.md), now expanded
with accessibility, panel hierarchy, modal lifecycle, state handling, reduced
motion, and workspace inheritance rules. Reuse the existing tokens and helpers
before inventing any new UI idiom.

[Cantina Creative's Iron Man 3 portfolio](https://www.cantinacreative.com/film/iron-man-3)
provides the aesthetic reference. Using layered panels and restrained illumination
is our interpretation of that visual direction, not a claim of usability research.
[WAI's modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
supports focus containment and return behavior.
[WCAG target-size guidance](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)
supports the compact-control floor, with its documented exceptions. These are
behavioral constraints around the existing Jarvis feel.

## Verification and limits

- Changed JavaScript passes `node --check`; `go test ./server` passes.
- `go test ./...` passes except `record.TestCorpusGoals`. The same test fails
  identically on an archive of unchanged HEAD: the owner's external golden goals
  corpus loses `[until:: …]` and `[verify:: …]` fields on round-trip. No parser,
  corpus, or vault file was changed by this audit.
- Browser preview uses local assets with read-only GET access to the existing
  backend; write methods are unavailable. Checked Feed, Tasks, Settings, Search,
  and Cast. Checks include keyboard navigation/focus return, task-capture selection
  without submitting, no-match handling, and phone/tablet/desktop layouts.
- This is a bounded shared-conventions pass, not an exhaustive audit of every
  screen, assistive technology, network failure, or legacy overlay. No external
  send, approval, task capture, or agent run was invoked while testing.
