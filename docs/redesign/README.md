# Handoff: Manifest UI redesign (navigation shell, Todos, AION, Properties, Goals)

## Overview

This package redesigns the **Manifest** dashboard (`Conscious-Repository/manifest`, branch `main`,
UI at `server/web/`) to be navigationally clear in the way Obsidian is: a persistent left rail,
breadcrumbs with back/forward, a command palette, quiet chrome, and a single accent color.

It replaces the current top pill-bar + `MORE ▾` dropdown navigation and restructures four surfaces:
**Todos**, **AION**, **Properties** (board, property page, work), and **Goals**.

**The Day/home view (`#dayView`) is explicitly out of scope and must not change.** Its markup,
CSS (`css/10-day.css`), and JS (`js/10-day.js`) stay exactly as they are. The only thing that
changes for that view is the surrounding chrome (the nav becomes the left rail).

## About the design files

The files in this bundle are **design references created in HTML** — prototypes showing intended
look and behavior, not production code to copy. Manifest's UI is deliberately **vanilla JS +
plain CSS with no build step** (ARCHITECTURE §11: "one JS module per tab over the kernel's HTTP
projections… No frameworks; vanilla stays"). Implement these designs **in that existing
environment**: new rules in the numbered `server/web/css/*.css` files, new DOM built with the
existing `el()` / `ghostInput()` / `typeahead()` / `makeDirtyBar()` component library in
`js/05-components.js`. Do not introduce React, a bundler, or a CSS framework.

## Fidelity

**High-fidelity.** Colors, type sizes, spacing, and states below are exact and were measured
against the existing token set in `css/00-core.css`. Recreate them precisely.

---

## 1. Design tokens

Replace the color block in `css/00-core.css` `:root` with the ramp below. This is the core change:
the palette collapses to a **neutral gray ramp plus one accent**. Obsidian's own system works this
way — a neutral base scale for surfaces and text, with a single accent reserved for interactive
state — and that is the heuristic being adopted here.

```css
:root {
  /* neutral ramp — surfaces, rules, text */
  --base-00: #ffffff;   /* page + panel surface */
  --base-05: #fafafa;   /* raised row, inspector, hover */
  --base-10: #f5f5f4;   /* left rail */
  --base-20: #ececea;   /* selected row / active nav item */
  --base-25: #e5e5e5;   /* hairlines, borders */
  --base-30: #d4d4d4;   /* icons, empty checkboxes, disabled chevrons */
  --base-40: #a3a3a3;   /* muted labels, meta */
  --base-50: #737373;   /* secondary text */
  --base-60: #525252;   /* body sub-text */
  --base-70: #404040;   /* nav item text */
  --base-100: #171717;  /* ink: primary text, alarm */

  /* the single accent — means LIVE */
  --accent: #265acc;
  --accent-soft: #aebfe8;

  /* the one exception */
  --over: #b91c1c;      /* money over budget ONLY */

  --radius: 4px;
  --radius-lg: 6px;
  --sans: "Hanken Grotesk", -apple-system, BlinkMacSystemFont, sans-serif;
  --mono: "Carbon", "Spline Sans Mono", ui-monospace, "SF Mono", Menlo, monospace;
}
```

### Color rules (enforce these)

| Rule | Detail |
| --- | --- |
| `--accent` means **live** | Active tab/chip, selected row's left rule and underline, the next thing to click (`decide →`, `open AION →`, wikilinks), in-progress status, progress bars, primary buttons. |
| **No amber, no green** | `#b45309` and `#15803d` are removed everywhere. |
| Alarm is **weight + a dot** | Stalled / overdue / aging / needs-attention render as `--base-100` text at `font-weight: 500`, prefixed with a filled `●`, and a `2px solid var(--base-100)` left rule on the row. Never a color. |
| `--over` is money-only | The single red is used **only** on a money figure that exceeds its budget, and on that figure's progress bar. Never on text, chips, or rules. |
| Selection is gray | A selected row is `background: var(--base-20)` with a `2px solid var(--accent)` left rule. |

### Type scale

| Use | Font | Size | Weight | Letter-spacing |
| --- | --- | --- | --- | --- |
| Page title (property page) | sans | 26px | 600 | -0.2px |
| Section lead (north star) | sans | 20px | 500 | -0.1px |
| Big stat | mono | 26px | 400 | -0.5px |
| Primary row text | sans | 15.5px | 400 (500 when alarmed) | 0.4px |
| Secondary row text | sans | 14.5px | 400 | 0.4px |
| Nav item | sans | 13.5px | 400 | 0.4px |
| Body sub-text | sans | 13px | 400 | 0.4px |
| Section label | mono | 11px | 400 | 0.5px, uppercase |
| Breadcrumb / tab chip | mono | 11.5px | 400 | 0.3px |
| Meta / count | mono | 10.5px | 400 | 0.4px |
| Rail group label | mono | 10px | 400 | 0.5px, uppercase |
| Status chip | mono | 9.5px | 400 | 0.6px, uppercase |

Body remains `font-size: 16px; letter-spacing: 0.4px;` as today.

### Spacing

Content padding `18px 24px 32px`. Row padding `8–10px 10px`. Rail item padding `6px 14px`.
Section gap `22–26px`. Chip padding `3px 12px`, radius `12px`. Card padding `10px 12px`.

---

## 2. Screen: the shell

**Purpose:** always answer "where am I", and let every destination be reachable by keyboard.

### Layout

```
┌────────────┬──────────────────────────────────────────────┐
│ left rail  │ breadcrumb bar (40px)                        │
│ 212px      ├──────────────────────────────────────────────┤
│ (50px when │ view tabs (chips)                            │
│ collapsed) ├──────────────────────────────────────────────┤
│            │ scrolling content                            │
└────────────┴──────────────────────────────────────────────┘
```

Outer container `height: 100vh; display: flex; overflow: hidden`. Content column is
`flex: 1; min-width: 0; display: flex; flex-direction: column`, with the scrolling area
`flex: 1; min-height: 0; overflow-y: auto`.

### Left rail

- `width: 212px`, `background: var(--base-10)`, `border-right: 1px solid var(--base-25)`.
- Collapsed: `width: 50px`; only the glyph column renders; labels, counts, and group headings are
  hidden. The collapse toggle (`‹‹` / `››`, `--base-40`, 12px) sits top-right of the rail header.
  Persist the collapsed state in `localStorage` under `manifest.rail.collapsed`.
- Header: 22×22 `--base-100` square, radius 5px, white `◆` at 11px; then `MANIFEST` in mono 12px.
- Search button: full-width inside `margin: 0 10px 14px`, white, `1px solid var(--base-25)`,
  radius 6px, padding `6px 9px`; `⌕` at `--base-40`, label "Search" at 13px `--base-40`,
  `⌘K` right-aligned in mono 10px `--base-30`. Opens the command palette.
- Groups, in this order, with these members (flat list — no expandable tree):

  | Group | Items |
  | --- | --- |
  | PLAN | Day `☀` · Goals `◎` · Todos `✓` |
  | WORK | Aion `◆` · Properties `⌂` · Studio `▤` |
  | SIGNAL | Feed `≋` · Spirits `✦` · Contacts `◍` · Calendar `▦` · Reading `▢` |

  Counts appear on Plan/Work items and, within Signal, on **Feed only** — Feed's count is the
  number of unresolved items, so the rail doubles as the inbox badge. Spirits, Contacts,
  Calendar and Reading carry no count.

- Item: `display: flex; gap: 9px; padding: 6px 14px; font-size: 13.5px`. Glyph column is 14px wide,
  centered, `--base-40`. Count right-aligned, mono 10.5px `--base-40`.
  - Rest: text `--base-70`, transparent background.
  - Hover: `background: var(--base-20)`.
  - **Active: text and glyph `--accent`, `background: var(--base-20)`.**
- Footer, pinned bottom, `border-top: 1px solid var(--base-25)`, padding `9px 14px`:
  the existing save-state text (`saved` / `saving`, mono 10.5px) on the left, `⌘/ raw` on the right.

### Breadcrumb bar

`display: flex; align-items: center; gap: 10px; padding: 10px 24px; border-bottom: 1px solid var(--base-25)`.

- Back `‹` and forward `›` buttons, 14px. Enabled `--base-60`; disabled `--base-30`.
  They drive a real in-app history stack (see State Management), not `window.history`.
- Crumb segments in mono 11.5px: ancestors `--base-40`, current `--base-100`, separator `/` in
  `--base-30`. Ancestor crumbs are clickable.
- Right side: a context meta string in mono 11px `--base-40`
  (`streak 12 days` · `34 open · 7 waiting` · `published 2d ago · a41c9e2` · `63 properties · 7 deals`).
- On AION only, a **PUBLISH** button sits at the far right: `--accent` fill, white mono 11.5px text,
  radius 6px, padding `4px 11px`, with a white pill badge (`--accent` text, radius 8px, 10.5px)
  carrying the count of dirty sections. **This replaces the eight `statusDot` dirty dots** in
  `renderAionRail()` — those are removed. Clicking still opens the existing preview→diff→confirm
  publish panel unchanged.

### View tabs

A single row of chips under the breadcrumb: mono 11.5px, radius 12px, padding `3px 12px`,
`1px solid var(--base-25)`, text `--base-50`. Active chip: text and border `--accent`,
background stays white. Hover: border `--accent-soft`, text `--accent`.
This is the existing `.filter-chip` family, restyled — reuse the class.

### Command palette (⌘K / Ctrl-K)

Reuse the existing `.cmdbar` shell in `css/75-bars.css` verbatim (fixed overlay, 28% black
backdrop, 560px card at `margin-top: 12vh`, radius 8px, 17px input with a hairline under it).
**Extend its result set** from contacts-only to: every nav destination, every AION sub-tab, every
property, and every contact. Result row: name in sans 14.5px `--base-100`, hint in mono 11px
`--base-40`, `padding: 10px 18px`, selected/hover `background: var(--base-05)`.

### Raw markdown (⌘/ or Ctrl-/)

New global overlay. Opens the markdown file backing whatever view is showing, in a 760px × 70vh
card (white, `1px solid var(--base-25)`, radius 8px, `0 20px 60px rgba(0,0,0,0.22)`).

- Header: the vault-relative path in mono 11.5px `--base-100`, then the note
  "raw markdown · edits write straight to the vault" in mono 10.5px `--base-40`, then `✕ esc`.
- Body: full-bleed `<textarea>`, mono 12.5px, `line-height: 1.7`, padding 16px, `spellcheck="false"`.
- Footer: "fixpoint guaranteed — parse→emit is byte-identical" in mono 10.5px `--base-40`, and a
  `--accent` **save** button.
- Path mapping: Day → the daily note; Todos → `to do.md`; Goals → `goals.md`;
  AION backlog → `system/aion/backlog.md` (heuristics/vto/people/hiring/references/finances
  → their own file); a property → `system/realestate/<slug>.md`.
- Saving goes through the existing guarded write path (`PUT /api/note`); it must not bypass
  `vaultwriter` (ARCHITECTURE §4).

---

## 3. Screen: Todos

**Purpose:** decide what to do next without hunting through collapsed groups.

Tabs: **FOCUS** (default) · AION · REAL ESTATE · PERSONAL — one per domain from `/api/todos`.

### Sections, top to bottom

**1. `⚑ Decisions waiting on you`** — always visible, never collapsed.
Section label mono 11px uppercase `--base-100`, count in mono 10.5px `--base-40`.
Each row: `padding: 9px 10px`, `border-top: 1px solid var(--base-25)`,
**`border-left: 2px solid var(--base-100)`**, `background: var(--base-05)`.
Contents: `⚑` glyph 12px `--base-100`; text sans 15px weight 500; a domain pill (mono 10.5px
`--base-40`, `1px solid var(--base-25)`, radius 10px, `0 7px`); then right-aligned `● {age}` in
mono 11px `--base-100`; then `decide →` in mono 11px `--accent`.
Source: each domain's `issues` array in `/api/todos`.

**2. `Next — across every domain`** (label becomes `Open in {domain}` on a domain tab).
Right side of the label row: `⇅ drag to rank` in mono 10.5px `--base-40`.
Row: `display: flex; gap: 12px; padding: 10px 10px; border-top: 1px solid #f0f0f0`; hover
`background: var(--base-05)`. Cells in order: drag handle `⠿` (mono 11px `--base-30`,
`cursor: grab`, 16px wide) · checkbox (`○` `--base-30`; `✓` `--accent` when done) · text
(sans 15.5px; done → `--base-40` + line-through; stale → weight 500) · domain pill · rock tag
(mono 10.5px `--accent`) · right-aligned age (mono 11px; `● 21d` `--base-100` when ≥14 days,
plain `--base-40` otherwise).

**Order in this list IS the priority** — the user requested drag-to-rank. Persist rank as a new
`[rank:: n]` inline field on the todo line via the record kernel's field grammar, and sort by it
with the existing oldest-first `added` ordering as the tiebreak.

Below the list: the existing ghost input, restyled — `＋ capture · t`, mono 11.5px `--base-40`,
`1px dashed var(--base-25)`, radius 4px, padding `5px 11px`.

**3. `From AION backlog`** — read-only mirror, shown on FOCUS and on the AION domain tab.
Label row carries `read-only · system/aion/backlog.md` in mono 10.5px `--base-40` and an
`open AION →` link in mono 11px `--accent`.
Rows are `background: var(--base-05)`, text `--base-70` (visibly subordinate), with the item's
glyph (`○` task / `◇` decision), rock tag in `--accent`, and the owner (`@HZ`) right-aligned.
Source: `GET /api/aion` → `backlog[]`, filtered to open items. No write affordances here.

**4. Quiet rows** — a wrapping row of buttons for what used to be collapsed footers:
`⏳ Waiting on someone · 7`, `◌ Ideas & backlog · 21`, `✓ Done this week · 18`.
Button: `background: var(--base-05)`, `1px solid var(--base-25)`, radius 6px, padding `7px 12px`.

### Item types

One list, five types, distinguished by glyph and rule — not by separate pages:
`○` task · `⚑`/`◇` decision · `⏳` waiting · `◌` idea/backlog · buckets as containers.
Buckets keep today's behavior (open by default, collapse persists for the session).

---

## 4. Screen: AION — Backlog

**Purpose:** work the company substrate. This is the default AION view.

Tabs: **BACKLOG** · HEURISTICS · V/TO · GOALS · ORG · SETTINGS.
Far right of the tab row: `⌘/ edit raw` in mono 10.5px `--base-40`.

**The two filter-chip rows (`ALL/TASKS/DECISIONS` + `OPEN/DONE·DECIDED/ALL`) are removed.**
Their job is done by the structure below.

### Layout

Two columns: the list (`flex: 1`, `padding: 18px 20px 40px`) and a **300px inspector** on the
right (`border-left: 1px solid var(--base-25)`, `background: var(--base-05)`, `position: sticky; top: 0`).

### Decisions lane (top, always visible)

Label `◇ Decisions`, meta `3 open · 1 decided`, and a `＋ decision` ghost button right-aligned.
Open decision row: `border-left: 2px solid var(--base-100)`, `background: var(--base-05)`,
`◇` glyph, text sans 15px weight 500, meta (`needed by 2026-08-22 · @BA`) in mono 10.5px
`--base-40`, then a right-aligned status chip.
Decided rows drop to `--base-40` chip on a `--base-25` border with a transparent left rule, and
show `decided 2026-07-30 → Charles River` in their meta.

Status chip: mono 9.5px, `letter-spacing: 0.6px`, uppercase, radius 4px, padding `2px 7px`.
`OPEN` on an alarmed item = `--base-100` text and border. `IN PROGRESS` = `--accent`.
`OPEN` unremarkable = `--base-50` on `--base-25`. `DONE`/`DECIDED` = `--base-40` on `--base-25`.

### Tasks, grouped by owner

One group per person from `system/aion/people.md`, ordered by open count descending.
Group header: initials in mono 11px uppercase `--base-100`, full name in sans 13px `--base-50`,
open count in mono 10.5px `--base-40`.

Task row is a 4-column grid — `18px minmax(0,1fr) auto auto`, `gap: 11px`, `padding: 8px 10px`,
`border-top: 1px solid #f0f0f0`:

1. checkbox — `○` `--accent` / `●` `--base-40` when done (matches today's `.aion-check`)
2. a stacked cell: title (sans 14.5px; weight 500 and a `2px solid var(--base-100)` row rule when
   stale/overdue; line-through `--base-40` when done) over meta (mono 10.5px `--base-40`)
3. rock tag, mono 10.5px `--accent`, `white-space: nowrap` — **the rock is now its own column**, so
   it reads without parsing the meta line
4. status chip

Selected row: `background: var(--base-20)` + `border-left: 2px solid var(--accent)`.
Each group ends with a `＋ task for {INITIALS}` ghost button that pre-fills the owner.

### Inspector (replaces the inline `aion-editor` drawer)

The drawer is removed because it reflows the list. The inspector shows the selected row's whole
record without moving anything:

- Header: `Inspector` in mono 10px uppercase `--base-40`, `✕` right-aligned.
- Title: sans 15.5px, `border-bottom: 1px solid var(--accent)`, editable in place.
- Field rows: label in mono 10px uppercase `--base-40`, fixed 66px wide; value in sans 13.5px on a
  `1px dashed var(--base-25)` underline, click-to-edit.
  Fields: owner (typeahead over `people.md`) · rock (typeahead over open Aion rocks, plus
  `✕ no rock (unanchored)`) · due (date) · status (open / in progress) · captured (read-only) ·
  kind (read-only). Decisions swap due/status/rock for `needed by` and an outcome field plus the
  `decide → permanent log` action.
- Source block: `⧉ 2026-08-04 Aion sync`, linking to the note view.
- Footer: `saves on blur` in mono 10.5px `--base-40`, `⌘/ raw` link in `--accent`.

Editing model: **click any value to edit in place; commit on Enter or blur; Esc reverts** — the
existing `clickToEdit` / `typeahead` behavior, just relocated into the panel. No modal, no
save button for single-field edits (keep `PATCH`-per-field via
`POST /api/aion/backlog/{id}/update`).

---

## 5. Screen: AION — Org

**Purpose:** maintain the registries without four stacked tables.

The four tables become a **176px registry rail** on the left (`border-right: 1px solid var(--base-25)`):
`Registries` heading, then People (9) · Hiring (4) · References (23) · Finances.
Selected entry: `--accent` text on `--base-20`. Below it a `File` block showing the
vault-relative path in mono 10.5px `--base-50` and an `⌘/ edit raw` link in `--accent`.

Right pane holds one table at a time, keeping today's `aionTableEditor` semantics:
mono 10px uppercase column headers, one row per record with in-place inputs, an `✕` remove per row,
a `＋ {record}` ghost, and the **sticky dirty bar** at the bottom of the pane —
`N UNSAVED CHANGES` in mono 11px `--base-40`, a `--accent` **save**, a white **discard**.
Column templates: people `90px 1fr 1fr 28px`; hiring `1.3fr 1fr 130px 48px 24px`;
references `1.4fr 1.4fr 110px 96px 24px`.

Finances keeps its key/value rows plus the derived read-only runway line.

## 6. Screen: AION — Heuristics

Rows at `padding: 10px 8px`, `border-top: 1px solid #f0f0f0`: caret `▸` `--base-40`, statement in
sans 15px `--base-100` (click to edit inline), then a **reinforcement bar** — a `4px` tall
`--accent` bar 8px wide per source, capped at 56px — and the count `×7` in mono 11px `--accent`.
The bar is the addition: strength is legible without counting. Reorder (`↑`/`↓`), merge (`⇄`), and
retire (`✕`) actions stay as hover-revealed mini buttons.

---

## 7. Screen: Properties — Board

**Purpose:** check status and progress across all properties. This is the default landing view.

**Sub-tabs drop from seven to four: BOARD · WORK · ACCOUNTING · SETTINGS.**
Map, Parcels, and Contractors move: Map and Parcels become views inside a property record and a
palette destination; Contractors becomes a section of ACCOUNTING. Their existing JS modules stay —
only their entry points move.

The board is one grouped table (no cards), grouped **Active → Pipeline → Done**.
Group header: name in mono 11px uppercase `--base-100`, `12 properties` in mono 10.5px `--base-40`,
and a right-aligned rollup `$9.4M plan · $6.1M spent`.

Row grid `16px 2.4fr 1fr 1fr 1fr 1.5fr`, `gap: 12px`, `padding: 8px 8px`,
`border-top: 1px solid #f0f0f0`, hover `background: var(--base-05)`:

| Col | Content | Style |
| --- | --- | --- |
| flag | `▲` over budget · `●` needs attention · empty | 10px `--base-100` |
| address | short address | sans 14.5px; weight 500 when flagged |
| stage | current stage | mono 11px `--base-40` |
| budget | plan total | mono 13px `--base-100`, right |
| spent | paid to date | mono 13px, right; **`--over` only when over budget** |
| note | one-line reason/latest | sans 13px `--base-50`, ellipsis |

Attention is defined as **over budget/variance** or **open work items/blockers** (the two signals
the user selected). The filter/sort/export/composer cluster collapses to a single `filter…` input
right-aligned in the tab row; exports move to ACCOUNTING.

## 8. Screen: Property page

**Purpose:** one property, one scroll, never lost.

Two columns: content (`max-width: 780px`, `padding: 22px 28px 48px`, sections at `gap: 26px`) and a
**190px sticky outline** on the right (`border-left: 1px solid var(--base-25)`, `position: sticky; top: 0`).

- Outline: `On this page` in mono 10px uppercase `--base-40`, then Overview · Budget · Open work ·
  Log · Ledger · Docs, each with a count in mono 10.5px `--base-30`. The active section gets
  `--base-100` text and a `2px solid var(--accent)` left rule; scroll-spy updates it.
- Title block: address sans 26px/600, a `--accent` status chip (`1px solid var(--accent-soft)`,
  radius 4px), then a mono 11.5px `--base-40` line: `rehab · 3 units · winchester pair · banderson llc`.
- Stat strip: four equal columns between two hairlines. Each = mono 10px uppercase label,
  mono 26px value (`letter-spacing: -0.5px`; Paid is `--accent`), mono 11px sub-line.
- Budget: one row per category, grid `1.6fr 2fr 0.9fr 0.7fr`, with a **6px progress bar**
  (`background: #f2f2f2`, radius 3px; fill `--accent`, or `--over` at 100% width when over),
  the actual right-aligned in mono 13px, and the percentage in mono 11.5px `--base-40`.
- Open work: glyph (`⚑` decision / `○` task / `⏳` waiting) · text sans 15px · age right-aligned.
- Log: date in mono 11px `--base-40` in a fixed 84px column, entry in sans 14.5px `--base-70`,
  `line-height: 1.45`.

## 9. Screen: Properties — Work

Stage columns (`min-width: 212px`, `gap: 14px`, horizontally scrolling).
Column header: stage name mono 10.5px uppercase `--base-100` + count, over a
`1px solid var(--base-25)` rule.
Card: `1px solid var(--base-25)`, radius 5px, `padding: 10px 12px`, `gap: 7px`,
**`border-left: 2px solid`** — `--accent-soft` normally, `--base-100` when stalled or over.
Contents: address sans 14.5px/600 · next action sans 13px (`--over` only if the action is the
over-budget one) · a 3px progress bar · a footer with `$812K / $1.24M` in mono 11px `--base-40`
and, right-aligned, `● 34d` / `● over` in mono 10.5px `--base-100`.

## 10. Screen: Goals

176px **Areas rail** (Aion · Real Estate · Health · Family, each with a rock count), content on
the right.

- North star block: mono 10px uppercase label, statement sans 20px/500, annual goal in mono 11.5px
  `--base-40`.
- Rocks: one block per rock, `padding: 14px 12px`, `border-top: 1px solid #f0f0f0`,
  `border-left: 2px solid` (`--accent` current · `--accent-soft` other · `--base-100` stalled):
  - line 1 — name sans 17px/500, lint note in mono 10.5px `--base-100`
    (`● no finish line · stalled 19d`), stage right-aligned in mono 11px `--base-40`
  - line 2 — `UNTIL` label (mono 10px, 46px wide) + finish line sans 14px `--base-70`, KPI
    right-aligned in mono 11px `--accent`
  - line 3 — `NEXT` label + next action in sans 14px `--accent`, open-task count right-aligned

The point: **UNTIL and NEXT are always visible** rather than hidden behind expanding the rock.

---

## 11. Screen: FEED

**Purpose:** clear the inbox — every item gets a verdict.

One chronological stream with filter chips above: **ALL · PROPOSALS · FINDINGS · NOTICES ·
RECEIPTS**. Max width 880px. The breadcrumb meta reads `N unresolved · 3 proposals`, and the
rail's Feed count is that same N.

**Type chips lose their colors.** Paper purple, person teal, company amber, portal teal and
receipt violet all become one neutral outlined chip — mono 10px uppercase, `--base-50` text,
`1px solid var(--base-25)`, radius 4px, padding `1px 6px`. The kind is carried by the word
(`PAPER` · `PERSON` · `PORTAL` · `RECEIPT` · `PROPOSAL`), not the hue. Confidence likewise drops
green/amber for mono 10px `--base-40` (`HIGH CONFIDENCE`).

**Signals lane** sits above the stream: one line each, `padding: 6px 4px`,
`border-bottom: 1px solid var(--base-25)`. Label in mono 12px `--base-100` prefixed with `●`
(alarm as weight + dot, not amber), then right-aligned `act` (`--accent`) / `snooze` / `dismiss`
(`--base-40`).

**Cards:** `1px solid var(--base-25)`, radius 4px, `padding: 14px 16px`, `gap: 8px`.
A proposal is pinned — `border-color: var(--accent)` and `background: var(--base-05)`.
Card body: kind chip + title (sans 16px/500) + confidence; the why-line in sans 14px `--base-70`
at `line-height: 1.5`; a mono 11px `--base-40` meta line (`scout · 2h ago · arxiv.org`).

**Proposals show their diff inline** — `will write · <path>` in mono 10px uppercase, then the
patch in a bordered mono 12px block. Added lines get `background: #f2f5fc` with `#1c469e` text
(a tint of the accent, not green); removed lines stay neutral on white. This is the one place the
old green/red diff coloring is replaced rather than kept.

**Verdicts.** Only proposals put both verbs on the card face — **Confirm** (accent fill, white
text) and **Reject** (white, `--base-25` border). Other kinds carry their single verb the same
way: Keep/Discard on findings, Dismiss on notices, Acknowledge on receipts.

**Clearing.** The moment a card gets its verdict it collapses to a one-line stub —
`padding: 6px 10px`, the verb in mono 10.5px `--base-40`, the title struck through in
`--base-40`, and an `undo` link in `--accent`. The stub keeps the zero-inbox count honest without
the item vanishing irreversibly.

## 12. Screen: SPIRITS

**Purpose:** know what is scheduled and what it cost. One page, no sub-tabs — the RUNS /
RITUALS / PORTALS tab bar is removed; **portals move into Settings**.

Order down the page:

1. **Live strip** (only when something is running): `1px solid var(--accent-soft)`, radius 4px,
   `background: var(--base-05)`; an 8px `--accent` dot, `RUNNING` in mono 10px uppercase
   `--accent`, the ritual line in sans 14px, elapsed time right-aligned in mono 11px `--accent`.
   The pulse animation stays.
2. **Rituals board** — the default focus. Grid `1.3fr 1.6fr 1.4fr 1fr 0.7fr`:
   name (mono 12.5px) · cadence (human sans 13px over the raw cron in mono 10.5px `--base-40`) ·
   next fire (sans 13px with a `--base-40` relative suffix) · last outcome chip · charge ceiling
   (mono 12px, right).
3. **Recent runs** — outcome chip · title (mono 13px) · when (right), over a charge bar:
   6px tall, `background: var(--base-10)`, `1px solid var(--base-25)`, fill `--accent`.
   The section header carries the week's spend.

**Run outcome is the one color exception here** (money over budget is the other), and it gets
three states, not five: **COMPLETED** = `--accent` on `--accent-soft`; **ERROR** = `--over` on
`#eec3c3`; **STOPPED** (charge or step ceiling) = `--base-50` on `--base-25` — neutral, because a
ceiling stop is the system working, not a failure. `RUNNING` uses `--accent` on `--accent`.
Portal state (open/degraded) and confidence lose their colors entirely.

## 13. Screen: CONTACTS

**List.** Search input + a `◆ Cold` filter chip + `＋ contact`, then the **email review strip**
(kept where it is today, at the top of the list): `1px solid var(--base-25)`, radius 4px,
`padding: 8px 14px`. Header `Review — N unlinked emails` in mono 11px `--base-100` with
`calendar attendees matched by name` in mono 10.5px `--base-40`. Each row: the address in mono
12.5px · `→` in `--base-30` · the proposed contact in sans 14px · meeting count in mono 11px
`--base-40` · right-aligned **Link** (accent fill) and **Dismiss** (white). Linking removes the
row.

**Rows:** name in sans 15px on the left with a `·` no-note dot; going-cold contacts get
`● going cold` in mono 10.5px `--base-100` (weight-and-dot again, not amber). Right side stacks
`met 12d ago` (mono 11px `--base-40`) over `mentioned 3d ago` (mono 10px `--base-30`).
A mentioned-only contact — no calendar verification — renders its top line in italic `--base-30`,
preserving today's distinction between *met* and *mentioned*.

**Person page:** facts strip first, then the timeline (your call).
- Name sans 26px/500 + a neutral role chip; aliases in sans 13px `--base-40`.
- **Facts strip** — four columns between hairlines, same anatomy as the property stat strip:
  Last met (mono 15px `--base-100`, sub `calendar-verified`) · Last mentioned · Cadence · Emails.
- **Timeline** — date in mono 12px (88px column) · source in mono 10px uppercase (84px column) ·
  title in sans 13.5px · a `CALENDAR` badge in `--accent` on `--accent-soft` for
  calendar-verified entries only.
- **Open loops** — unchecked tasks from meeting notes: `○` glyph, text sans 14px, origin note
  right-aligned in mono 11px `--base-40`.

## 14. Screen: CALENDAR

**Restyle only** — the month grid's structure is untouched. `grid-auto-rows: minmax(124px, auto)`,
7 equal columns, `1px solid var(--base-25)` cell rules, adjacent-month cells on `--base-05` with
`--base-40` day numbers. Today's number keeps its `--accent` filled circle. Events stay a 5px
`--accent` dot + mono 11px time + truncating title, with `+N more` in mono 10.5px `--base-40`.
All-day events keep the `#eef2fb` bar. Only the token values change.

**Grid geometry (get this right).** The columns are Monday-start, so the leading offset is
`(new Date(y, m, 1).getDay() + 6) % 7` — a *count of cells before the 1st*, not an index — and
the cell for day `n` is at `offset + n - 1`. The grid must size to
`Math.ceil((offset + daysInMonth) / 7) * 7`, never a fixed 35: August 2026 begins on a Saturday
(offset 5) and therefore needs **six rows / 42 cells** or the 30th and 31st fall off the end.
Leading cells show `prevMonthDays + dayNum`, trailing cells `dayNum - daysInMonth`.

## 15. Screen: READING

Search + sort + filter + `＋ book` in one row, using the existing `.book-select` dropdowns.

**Currently-reading strip** (`.reading-strip`) sits above the shelf and ignores the status
filter. Head: `Currently reading — N` in mono 11px uppercase `--base-40`. Then a wrapping row of
**cards** (`.reading-card`, `gap: 10px`): `1px solid var(--accent-soft)`, radius 4px,
`padding: 10px 12px`, `min-width: 220px; max-width: 300px`, `gap: 6px` — title in sans 14px
`--base-100` (click opens the note), authors in sans 12.5px `--base-40`, and a
`✓ Mark read` pill aligned to the start.

**The shelf** (`.book-shelf`) is a hairline table of `.book-row`s — grid
`minmax(200px, 3fr) minmax(120px, 2fr) 56px 92px 100px`, `gap: 12px`, `align-items: baseline`,
`padding: 8px 4px`, `border-bottom: 1px solid var(--base-25)`. Columns:
TITLE (sans 14.5px, clickable → the note) · AUTHOR (sans 12.5px `--base-40`, each author a
separate clickable wikilink) · YEAR (mono 12px `--base-40`) · RATING · READ (mono 11.5px
`--base-40`, right-aligned; a currently-reading book reads `reading`).
The header row is the same grid with mono 10px `letter-spacing: 0.06em` `--base-40` labels and a
`border-bottom: 1px solid var(--base-100)`.

**Rating** is five interactive stars (`★` filled / `☆` empty) at 13px, `gap: 1px` — clicking a
star sets the rating, clicking the current value clears it. **One deviation from source:** the
filled star was `#d9a520` gold; under the single-accent rule it becomes `--accent`, with empty
stars `--base-30`. Revert to the gold if you'd rather keep it.

---

---

## Revision 2 — density, editable Goals, editable property page

These supersede the corresponding details above.

**Density: dense — everywhere.** This is a global rule, applied across Todos, Goals, AION
(backlog rows, decisions lane, ORG tables), Feed, Contacts, Properties and Spirits:

| | dense value |
| --- | --- |
| List row padding | `4px 8px` (was `8px 10px`) |
| Card padding | `9px 12px`, `gap: 6px` (was `14px 16px`, `gap: 8px`) |
| Stat / facts strip cell | `10px 16px 10px 0` |
| Primary row text | 14px (was 15–16px) |
| Card title | 14.5px (was 16px) |
| Body / why line | 13px, `line-height: 1.45` |
| Section label | 10px uppercase, `white-space: nowrap` |
| Meta / count | 9.5–10px |
| Row hairline | `#f4f4f4`; section hairline `--base-25` |

Two deliberate exceptions: the **Day view** keeps its 50px rows and 16px text untouched, and
**Reading** keeps the `.book-row` metrics measured from `65-reading.css`. Light theme only — no
dark mode.

Micro-labels in section headers must carry `white-space: nowrap` — at 10px uppercase they wrap to
two lines inside a 30px header row and silently double its height.

**Property page edits in place.** It was read-only, which is why it read as a wall of text.
Now: status and stage are `<select>`s in the title row (accent-bordered for status, neutral for
stage) — the two things edited most; the Log opens with a **composer row** (today's date, an
underlined input, `add ↵`) so writing an entry is the first thing available, not the last; budget
amounts and log lines are click-to-edit with a dashed underline on hover. Batched edits raise the
**sticky save bar** at the bottom of the pane — `N UNSAVED CHANGES`, accent **save**, neutral
**discard** — matching `makeDirtyBar` in `js/05-components.js`.

> Implementation note: a native `<select>` whose `value` is set before its `<option>` children
> mount falls back to the first option. Render the current value first in the options array (or
> set `selected` on the matching option) — the prototype does the former.

**Goals is one editable outline.** The rock cards are gone. An area now renders every item it
contains as a single indented outline — rock (15px/500, accent left rule) → stage (13.5px, `→`
marks the current one) → task (13px, `○`/`✓`) — with each line click-to-edit, `tab`/`shift-tab`
to re-parent, `enter` to add a sibling, and `＋ rock` at the foot. UNTIL rides on the rock's own
line as a quiet tag rather than its own row, and stalled rocks keep the ink left rule and the
`● stalled 19d` meta. This is what "see ALL items for each area" means: no expanding, no cards —
one outline you can type into, the way a note behaves in Obsidian.

**Todos is thinner.** Rock tags and the waiting/ideas/done buttons are removed from the row.
What remains per line: drag handle · checkbox · text · domain · age. The section header reads
**"Everything you've committed to"** — that is the one question the page answers.



| Interaction | Behavior |
| --- | --- |
| Rail collapse | Toggles 212px ↔ 50px. No animation needed; if added, 120ms ease. Persist to `localStorage`. |
| Nav click | Sets the route, pushes the previous route onto the history stack, clears the forward stack. |
| Back / forward | Pop/push the in-app history stack. Disabled state is `--base-30` and non-interactive. |
| `⌘K` / `Ctrl-K` | Toggles the command palette. `Esc` closes. Arrow keys move selection; Enter navigates. |
| `⌘/` / `Ctrl-/` | Toggles the raw-markdown overlay for the current view's file. `Esc` closes. |
| `t` | Existing global quick-add (`openTodoQuickAdd`) — unchanged. |
| Row click (AION) | Selects the row → inspector updates. The list does not reflow. |
| Checkbox click | Toggles done; `stopPropagation` so it never also selects the row. Optimistic update, then POST. |
| Click-to-edit | Span → input on click; Enter or blur commits; Esc reverts. Existing `clickToEdit`. |
| Table edits (ORG, V/TO) | Mark dirty → sticky bar appears → one `PUT` of the whole list. Existing `makeDirtyBar`. |
| Drag to rank (Todos) | Handle `⠿` starts the drag; drop writes `[rank:: n]` on affected lines and re-renders. |
| Hover | Rows `background: var(--base-05)`; chips gain `--accent-soft` border and `--accent` text. |
| Publish | Badge count = number of dirty sections. Click opens the existing preview → diff → confirm flow, unchanged. |

Responsive: below 1100px the AION inspector hides (the three-column task row needs ~430px of list
width beside it) and the property-page outline collapses; below 860px the rail auto-collapses to
icons and two-column layouts stack. In the AION task row the text column carries a
`minmax(200px, 1fr)` floor and the rock/chip columns are `minmax(0, auto)` with ellipsis, so the
title never loses width to them. In the decisions lane the title is `flex: 1 1 auto` and the meta
is `flex: 0 1 auto; max-width: 34%` — the meta truncates first, the decision text always reads.
The Day view keeps its existing 860px breakpoint rules.

---

## Revision 3 — properties stripped to todos, and the assignment model

This replaces §7, §8 and §9 (board, property page, work view) entirely.

### The model

A property is **an address, a status, one budget number, and todos.** No stages, no budget
categories, no per-stage kanban. The WORK sub-tab is deleted; `js/89-properties-workview.js`,
`js/86-properties-work.js` and their CSS come out of the page.

### Layout — one rail, one pane

`PROPERTIES` is a two-column surface, not a board that drills down:

- **200px rail** — a `Views` group (**All properties**, **Outstanding**) over a `Properties`
  group listing every property with its open-todo count, then `＋ property`. Selected entry is
  `--accent` on `--base-20`, as in every other rail.
- **Content pane** renders one of three things: the all-properties table, the outstanding view,
  or a single property.

**All properties** is three columns — `2.6fr 1fr 0.8fr`: address (sans 14px) · status (mono
10.5px `--base-40`) · open-todo count, right-aligned and `--base-100` when non-zero, `--base-30`
when zero. That's the whole board.

**A property page**, top to bottom: address (sans 22px/600) with the status `<select>` beside it ·
a two-cell strip — **BUDGET** and **SPENT** as mono 22px figures side by side, spent in `--over`
only when it exceeds budget · **TODOS** · **LEDGER** (date · category · vendor · amount, mono,
`padding: 3px 8px`). Nothing else. Docs, contractors, the log and the section outline come off
the page — reach them with `⌘/`.

The todo list ends with an always-present composer row: `○` + an underlined input reading
*add a todo for this property…* + `add ↵`. Adding a todo is the primary action of the page.

### Assignment — the accountability half

A todo carries exactly three things: **text**, an **assignee**, and the **property** it belongs
to. No due dates, no age, no waiting flag.

- Assignees come from a short curated per-area list (real estate: the GC, trades, counsel;
  Aion: `system/aion/people.md`). Written as `[owner:: …]` on the line.
- **Unassigned means yours.** The row shows `you` in `--accent`; someone else's shows their name
  in `--base-40`.
- Assign by **selecting the todo** — a 280px inspector opens on the right with the text, an
  assignee `<select>`, the property, and a plain-language routing note: *"Assigned to Marco (GC) —
  tracked here, never in your TODOS. It shows under Outstanding until they close it."*

### Routing rules (implement exactly)

| Condition | Behavior |
| --- | --- |
| Assigned to me (or unassigned) | Appears in **TODOS**, under the Real Estate domain (Aion todos under Aion) |
| Assigned to someone else | **Never** in TODOS — it lives on the property page and under **Outstanding** |
| Completed anywhere | Completed everywhere — one line in one file, two surfaces over it |
| Added from TODOS | Can target a property directly, landing on that property's page |

**Outstanding** groups everything owed to you **by property**: a property heading with
`N owed to you`, then the rows with the assignee right-aligned. This is the surface for chasing
people.

Aion works identically — same three fields, same routing, assignees from the Aion people
registry — so the two domains behave as one system with two vocabularies. Assignees are expected
to see their items eventually, so keep the assignee a real identity (a person note or registry
row), never a free-text string.

### Implementation shape (this is the whole trick)

Do **not** keep a todo list for Todos and another for properties/AION. There is one set of items;
TODOS is a **projection** over it:

```js
todoRows = [ ...personalLines,
             ...PROPERTIES.flatMap(p => todosFor(p)),
             ...AION.flatMap(o => o.tasks) ].filter(t => t.owner === me)
```

and completion lives in **one id-keyed store** both surfaces read and write. Get these two things
right and every rule in the table above holds for free; keep parallel arrays and they all break at
once — an item assigned to you won't reach Todos, and checking it in one place won't check it in
the other.

Because other people's items never reach TODOS, there is no "from AION backlog" mirror section —
that idea is withdrawn. Your AION and property items simply appear in the list under their domain.

**Every counter derives too.** Rail badges and breadcrumb metas are computed from the same models
as the rows — Todos from `todoRows.filter(t => !done[t.id]).length`, Aion from open tasks plus
open decisions, Goals from the area list, Properties from the property list, Feed from unresolved
items. No count is ever a string literal: a hardcoded badge is the same parallel-truth bug as a
hardcoded list, and it shows up as chrome that contradicts the page it labels. Note also that
Todos no longer has a waiting bucket, so the old `N waiting` half of its meta is dropped rather
than re-derived.

---

## State management

New client state (plain module-scope variables, matching the existing pattern):

```js
let uiRoute   = { section: "day", sub: "", id: "" }; // mirrors location.hash
let uiHistory = [];   // back stack of routes
let uiForward = [];   // forward stack
let railOpen  = true; // persisted in localStorage
let paletteOpen = false;
let rawOpen   = false;
let aionSelId = null; // inspector selection (replaces aionEditingId)
```

Routing keeps the existing hash scheme in `js/99-boot.js` (`#/todos`, `#/aion/org`,
`#/properties/<slug>`) — the rail and breadcrumbs are just a new rendering of it. `route()` should
additionally push onto `uiHistory` on hash change and re-render the rail's active state.

Data fetching is unchanged: `GET /api/day`, `/api/goals`, `/api/todos`, `/api/aion`,
`/api/properties`. The Todos AION mirror is the one new join — it reads `backlog[]` from the
existing `/api/aion` response; no new endpoint is required.

One new write concern: the todo `[rank:: n]` field. Add it as a recognized field in
`todos/fields`-equivalent parsing so it round-trips under the kernel's fixpoint guarantee
(ARCHITECTURE §3), and route the write through `vaultwriter` like every other todo edit (§4).

---

## Assets

No new image or icon assets. All glyphs are Unicode characters already in use in the codebase:
`◆ ☀ ◎ ✓ ≋ ✦ ▤ ◍ ⌂ ▦ ⌕ ⠿ ○ ● ✕ ▸ ▾ ⚑ ◇ ⏳ ◌ ⧗ ⧉ ‹ › ‹‹ ›› ▲ △ ⇅ ↑ ↓ ⇄`.
Fonts stay Hanken Grotesk + Spline Sans Mono from Google Fonts, with the optional self-hosted
`carbon.woff2` fallback chain untouched.

---

## Files in this bundle

| File | What it is |
| --- | --- |
| `PROMPT.md` | **Start here.** How to run this with Claude Code — where to put the bundle, and a copy-paste prompt per step. |
| `Manifest - Prototype.dc.html` | **The interactive prototype.** Click the rail, tabs, rows, and properties; `⌘K` opens the palette; `⌘/` opens raw markdown; checkboxes, todo assignment and the AION inspector are live. This is the primary reference for behavior, and the acceptance criterion for each step. |
| `Manifest - Redesign.dc.html` | The static option canvas. Turn 2 (top) is the accepted direction — `2a` Todos, `2b` Properties ledger, `2c` AION backlog, `2d` AION org — over a color-ramp swatch. Turn 1 below it is the earlier exploration, kept for context. |
| `Manifest - Current.dc.html` | A faithful recreation of the app **as it is today**, rebuilt from source. Use it to diff old against new. |
| `github.md` | The repo association and screen→source map. |
| `support.js` | Runtime for the three `.dc.html` files — keep it beside them so they open offline. |

Open any of them directly in a browser.

## Repo files this replaces or touches

| Area | Files |
| --- | --- |
| Tokens | `server/web/css/00-core.css` (`:root` block) |
| Nav → rail | `server/web/index.html` (`<header class="nav">`), `css/07-nav.css`, `js/99-boot.js` (`setNavExpanded`, `route`) |
| Shared chips/rows | `css/05-primitives.css` (`.filter-chip`, `.pill`, `.o-ghost`) |
| Todos | `css/90-todos.css`, `js/90-todos.js` |
| AION | `css/92-aion.css`, `js/92-aion.js` (remove `aionRowEditor` drawer + `renderAionRail` dots) |
| Properties | `css/80-properties-core.css`, `js/80-properties-core.js`, `js/81-properties-board.js`, `js/83-properties-page.js`, `css/89-properties-workview.css` |
| Goals | `css/20-goals.css`, `js/20-goals.js` |
| Feed | `css/45-feed.css`, `js/45-feed.js`, `js/55-approvals.js` (proposals lane) |
| Spirits | `css/40-spirits.css`, `js/40-spirits.js`, `js/58-rituals.js` |
| Contacts | `css/60-contacts.css`, `js/60-contacts.js` |
| Calendar | `css/30-calendar.css`, `js/30-calendar.js` |
| Reading | `css/65-reading.css`, `js/65-reading.js` |
| Command bars | `css/75-bars.css`, `js/75-bars.js` (extend results; add the `⌘/` raw overlay) |
| **Do not touch** | `css/10-day.css`, `js/10-day.js`, `#dayView` markup |

## Suggested order of work

1. Land the token ramp in `00-core.css` and purge amber/green across all CSS files.
2. Build the rail + breadcrumb shell; keep every existing view mounting inside it unchanged.
3. Extend the command palette; add the `⌘/` raw overlay.
4. Todos: decisions lane, ranked list, AION mirror, quiet rows.
5. AION: decisions lane, owner grouping, inspector (delete the drawer), publish badge, ORG rail.
6. Properties: four tabs, grouped ledger board, property page with sticky outline, work cards.
7. Goals: area rail and the always-visible UNTIL/NEXT rock rows.
8. Signal surfaces: Feed (neutral kind chips, signals lane, verdict stubs), Spirits (one page,
   portals moved to settings), Contacts (email review strip, facts-then-timeline person page),
   Calendar (restyle only), Reading.

Each step is independently shippable; the Day view is untouched throughout.
