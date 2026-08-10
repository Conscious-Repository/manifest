# How to run this with Claude Code

## 1. Put the bundle in your repo

```bash
mkdir -p docs/redesign
cp -R design_handoff_manifest_redesign/* docs/redesign/
git add docs/redesign && git commit -m "docs: manifest redesign handoff"
```

Keeping it in-repo matters: Claude Code can read `docs/redesign/README.md` and open the
prototype at any point, in the same working tree as the code it's changing.

## 2. Open the prototype yourself first

```bash
open docs/redesign/"Manifest - Prototype.dc.html"
```

It runs offline — click the rail, press `⌘K` and `⌘/`, assign a property todo, check something
off in two places. Anything that feels wrong is cheaper to change here than in Go + JS.

## 3. Kick off

Run `claude` from the repo root and paste this:

> Read `docs/redesign/README.md` end to end, then `ARCHITECTURE.md`. This is a redesign of the
> embedded UI in `server/web/`. It is vanilla JS + plain CSS with no build step (ARCHITECTURE
> §11) — do not introduce a framework, bundler, or CSS library, and build new DOM with the
> existing helpers in `js/05-components.js`.
>
> Two hard constraints: the Day view (`css/10-day.css`, `js/10-day.js`, `#dayView`) must not
> change at all; and every vault write goes through `vaultwriter` under a declared capability
> (§4).
>
> Do step 1 only — land the token ramp in `css/00-core.css` and purge amber/green across the CSS
> files, per the README's Design tokens section. Show me the diff before you touch anything else.

Then one message per step, in the README's "Suggested order of work". **Do not paste all eight
steps at once** — each is a self-contained, shippable change, and reviewing them one at a time is
what keeps the recreation honest.

## 4. Prompts for the remaining steps

**Step 2 — the shell**

> Build the left rail + breadcrumb shell from README §2. Replace the `<header class="nav">` pill
> bar in `index.html` and rewrite `css/07-nav.css`. Every existing view keeps mounting inside it
> unchanged — this step is chrome only. `route()` in `js/99-boot.js` should also push onto an
> in-app history stack for the back/forward buttons. Persist the collapsed state under
> `manifest.rail.collapsed`.

**Step 3 — command bars**

> Extend the `⌘K` palette in `js/75-bars.js` from contacts-only to every nav destination, AION
> sub-tab, property and contact. Then add the `⌘/` raw-markdown overlay per README §2, routing
> its save through the existing `PUT /api/note`.

**Step 4 — Todos**

> Rebuild Todos per README §3 and Revision 3. The critical part is the implementation shape:
> Todos is a projection over property + AION + personal items filtered to items assigned to me,
> with ONE id-keyed completion store — not a parallel list. Every counter derives from that same
> model; no count is a string literal. Add `[rank:: n]` as a recognized inline field for
> drag-to-rank and route the write through `vaultwriter`.

**Step 5 — AION**

> Rebuild the AION backlog per README §4: decisions lane on top, tasks grouped by owner, the
> inline `aionRowEditor` drawer replaced by a right-side inspector, the eight publish dirty dots
> replaced by one count badge. Then ORG per §5 and heuristics per §6. Same assignment rules as
> properties (Revision 3) — items owned by others never reach Todos.

**Step 6 — Properties**

> Replace the Properties surface per README Revision 3. This is a deletion as much as a build:
> stages, budget categories, the WORK kanban and the 7-tab bar all come out. What remains is a
> rail of properties plus a pane showing address, status, budget, spent, todos and ledger.
> Delete `js/89-properties-workview.js` and `js/86-properties-work.js` and their CSS.

**Step 7 — Goals**

> Rebuild Goals per Revision 2: one editable outline per area — rock → stage → task, every item
> visible, each line click-to-edit, tab/shift-tab to re-parent. This replaces the collapsed rock
> rows in `js/20-goals.js`.

**Step 8 — Signal surfaces**

> Rebuild Feed, Spirits, Contacts, Calendar and Reading per README §11–15. Feed's type chips go
> neutral; Spirits collapses to one page with portals in settings; Contacts leads with the facts
> strip; Calendar is restyle-only — but fix the month geometry per §14; Reading follows the
> `.reading-card` / `.book-row` structure already in `css/65-reading.css`.

## 5. What to tell it when it drifts

Three failure modes worth naming up front, because each one bit during the design:

- **Parallel truth.** A second array of the same items, or a hardcoded count next to a derived
  list. One model, one completion store, every counter computed.
- **Inventing instead of reading.** If it writes a color, a spacing value or a layout it can't
  point to in this README or a repo file, it's guessing. Make it open the file.
- **Fixing the symptom.** Starving grid columns, wrapping labels and clipped text usually trace
  to one upstream constraint (a panel that shouldn't render at that width, an offset treated as
  an index). Ask for the root cause in one sentence before the edit.

## 6. Verifying

The prototype is the acceptance criterion. For each step: open it alongside the running app at
the same window width and compare the surface you just built. `Manifest - Current.dc.html` is the
before-state if you need to argue about what changed.
