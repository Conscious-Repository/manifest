# Manifest UI Conventions

The dashboard's design contract. This file is the source of truth for the look
and feel. Review checks this contract alongside the existing Go UI guard
tests and browser checks; the tests do not replace visual or keyboard review. AION portal excluded (it
keeps its own conventions in `server/web/portal/`).

Doctrine (ARCHITECTURE.md §1): **manifest-quiet everywhere** — mono labels,
hairlines, ghost inputs, muted dots, one accent for live/active. Loud color is
reserved for nothing. §11: **one JS module + one CSS file per tab, composed
from the shared component library. No new UI idiom without touching the library
first.**

---

## The one rule

**Use shared tokens for color, typography, spacing, control sizing, and panel roles.**
The token file is `css/00-core.css`. Introduce reusable values there before
using them across surfaces. Literal geometry is allowed for borders, breakpoints,
viewport constraints, and a component's unique layout; it must not create an
alternative spacing/type/color scale. Existing literals are migration debt,
not precedent. Accent fills always pair with `--on-accent`, including hover.

---

## Tokens (all defined in `css/00-core.css` `:root`)

### Color — role-named, never hue-named
- **Neutral ramp:** `--base-00` (#fff, page/panel) → `--base-100` (#171717, ink).
  Surfaces, rows, hairlines, and text all index into this one lightness ramp.
  Aliases `--bg / --text / --line / --muted / --icon / --hover` point into it.
- **Accent:** `--accent` (#265acc) means **LIVE / active — and only that.** Never
  use it for decoration or emphasis. `--accent-soft` for its quiet tint.
- **Status (semantic, separate from accent):** `--danger` (red), `--warn` (amber),
  `--good` (green), and `--over` (money-over-budget red — its own token). These
  are the *only* non-neutral, non-accent colors allowed. Reach for a status token,
  never a fresh hex.

The values above describe the default theme. Jarvis overrides these **same roles**
under `:root[data-theme="jarvis"]`: navy surfaces, cyan active states, the existing
grid, hairlines, and restrained glow. A new feature gets both themes through the
roles; it must not embed its own dark palette or replace the Jarvis treatment.

### Type — `--fs-*` scale
`--fs-3xs 8 · --fs-2xs 10 · --fs-xs 11 · --fs-sm 12 · --fs-md 13 · --fs-lg 14 ·
--fs-xl 16 · --fs-2xl 19 · --fs-3xl 30`. Roles: `2xs` micro/chips, `xs` the
workhorse mono label, `sm` small body, `md` dense body default, `lg` emphasis,
`xl` body baseline + section titles, `2xl` page titles. **No fractional sizes** —
the old `10.5/11.5/12.5/13.5` snap to the nearest step.

### Space — `--sp-*` scale
`--sp-1 2 · --sp-2 4 · --sp-3 6 · --sp-4 8 · --sp-5 10 · --sp-6 12 · --sp-7 16 ·
--sp-8 20 · --sp-9 24`. Compose padding shorthands from these (`padding: var(--sp-3)
var(--sp-5)`). Odd values (7, 9) snap to the nearest even step. Gap is where the
scale is already de-facto followed — extend it to padding/margin.

### Radius, shadow, fonts
- `--radius` 4px, `--radius-lg` 6px. Use them; don't draw raw `3/5/6px`.
- `--shadow-1` / `--shadow-2` — the only drop shadows. No hand-rolled `rgba(0,0,0,x)`.
- `--sans` (Hanken Grotesk) = human prose, **set once on `body`, inherited**.
  `--mono` (Carbon/Spline) = all metadata, labels, chips, counts, timestamps, code.
  These two families and their roles are the app's signature — keep the split strict.

---

## The signature label: `.micro-label`

The mono uppercase metadata label is manifest's most-repeated idiom. It lives as
**one class** in `css/05-primitives.css`:

```css
.micro-label { font-family: var(--mono); font-size: var(--fs-2xs);
  text-transform: uppercase; letter-spacing: 0.4px; color: var(--muted); }
```

Put `micro-label` on any label/chip element. A tab's own class adds ONLY
structural extras (border, padding, a state color override) — **never re-type the
font/size/transform recipe.** Before this, the recipe was copy-pasted across ~70
selectors; that is the drift we removed.

**Two ways the recipe reaches a label — both valid, never re-typed inline:**
1. **`.micro-label` utility (JS-applied)** — add the class in the `el(...)` call.
   Preferred for new labels and cross-cutting chips (Feed uses this).
2. **Per-file grouped rule (CSS-only)** — each tab file that has several label
   classes carries ONE grouped rule at the top holding the invariant recipe
   (`--mono` + `uppercase` + `letter-spacing`), and each label selector keeps only
   its own `font-size`/`color`/structure. This is how the existing tabs were
   consolidated (no JS churn). Look for the `mono-label group` comment.

Either way the rule is: the mono/uppercase/letter-spacing recipe exists in exactly
one place per label, never copied into the individual selector.

---

## The component library (`js/05-components.js` + `css/05-primitives.css`)

"Touch the library first" means: if the thing you're building already exists here,
use it; if it's new and reusable, add it here, not in a tab file. The catalog:

| Helper | Use it for |
|---|---|
| `el(tag, cls, text)` | every DOM node — the universal builder |
| `pill(text, fn)` / `pillLight(text, fn)` | buttons (solid dark pill / light pill) |
| `ghostInput(...)` | a ＋ that swaps to an input (the `.o-ghost` idiom) |
| `typeahead(opts)` | inline autocomplete + create (the `.ta-wrap` engine) — the ONE typeahead |
| `emptyRow(text)` | any empty-state line |
| `inputEl` / `selectEl` / `moneyInput` | form fields |
| `statusDot(on, title)` | the quiet on/off dot |
| `collapsibleSection(...)` | a titled toggle section |
| `showToast(msg, onClick, kind)` | transient confirmations (lives in the library) |
| `.modal` / `.cmdbar` | the ONE modal shell / the ONE command-overlay shell |
| `.micro-label` | the mono uppercase label (above) |

Modals and empty-states are already unified (~4 modal archetypes, not dozens).
Keep it that way: no new modal class — reuse `.modal` / `.cmdbar`.

---

## The tab contract (§11)

Each tab is `js/NN-name.js` + `css/NN-name.css` (1:1, cascade order kept aligned
in `index.html`), plus an `id="nameView"` section toggled `hidden`. The render
convention is a triad:

```
showX()    // entry: called by the router; kicks loadX + any polling
loadX()    // fetch /api/... into a module cache, then renderX
renderX()  // build DOM from cache into els.xxx via el()/library helpers
```

Every tab registers a ⌘K provider at parse time (see `cmdRegistry` in
`00-core.js`). A new tab should reach parity using only library helpers + tokens,
with **no new raw values** — that's the practical test that the system is real.

**Page container (the outer wrapper of every top-level view):** all views share
ONE column so titles and content line up tab-to-tab — never invent a per-tab
width:

```css
.someView { max-width: var(--page-max); margin: var(--sp-4) auto 64px; padding: 0 var(--page-gutter); }
```

`--page-max` (1180px) and `--page-gutter` (28px) live in `00-core.css`. Change
the width once there and every page moves together. The header inside is the
shared `.agent-head` + `.agent-title` (mono uppercase). Do not set a bespoke
`max-width`/`margin: auto` on a view.

---

## Self-check (the no-linter substitute — run before shipping UI)

```sh
cd server/web
# raw hex outside the token file (should be ~empty):
grep -rnE '#[0-9a-fA-F]{3,6}\b' css/ | grep -v '00-core.css'
# white/black rgba literals (use --ink / --tint mixes or --shadow-* instead):
grep -rnE 'rgba\((255,\s*255,\s*255|0,\s*0,\s*0)' css/
# fractional or off-scale font sizes (should snap to --fs-*):
grep -rnE 'font-size:\s*[0-9]+\.[0-9]+px' css/
# a label recipe re-typed instead of using .micro-label:
grep -rn 'text-transform: uppercase' css/ | grep 'var(--mono)'
```

A hit isn't automatically wrong — a shadow offset, an on-accent text color, a
genuinely new scale value — but each hit is a decision to make on purpose, not a
default to reach for.

---

## Responsive

One authoritative phone band: `css/95-mobile.css` (`@media (max-width: 860px)` /
`@media (hover: none)`) + `js/98-mobile.js`. The 860–1100 tablet band is
deliberately untouched. Keep breakpoint logic centralized there; a tab's own file
holds only the rare component-local grid tweak.

---

## Patterns promoted from the recruiting redesign (2026-09-04)

Adopted app-wide when the shape recurs; recruiting (`96-aion-recruiting`) is
the reference implementation.

**Two axes, two controls.** When a list cuts on ORIGIN/type *and* on
state/stage, origin is a segmented control on its own line (`.rec-seg` shape:
one bordered track, mono chips, `.on` = `--base-20` + accent text) and state
is the `.filter-chip` row beneath. Never merge them into one chip strip —
they answer different questions.

**Disclosure folds with derived metas.** Secondary inspector sections render
as one-line disclosure rows: caret · micro-label · a DERIVED meta string
right-aligned, which goes ink (`--base-100`, weight 500) when the section
wants attention (`● none yet`, `not handed off`). The working section stays
expanded; everything else folds. Meta strings are derived, never literals.

**Error ≠ empty ≠ no-match.** Every list distinguishes three states: a fetch
failure names itself and offers retry (never a silent empty board); a
genuinely empty collection invites the first action; a filter that hides
everything says the pipeline itself is fine.

**One state table.** When a status drives a chip, a summary line, and a
primary action, all three read ONE resolution function (recruiting's
`recGateTable`) — two adjacent conditionals deriving the same state is how
contradictory UI ships.

**Design-handoff overrides.** Where a Claude Design handoff differs from this
contract, the contract wins: selection is `--base-20` + accent (never a
black/ink fill); attention counts are ink `● N` (the alarm idiom, dot
first); breadcrumbs inside a tab's content are not used — the tab bar plus
the header meta carry location.

## An absence shows only when it changes the call (2026-09-05)

Honest UI names what is missing — but a line that reports the same absence on
every row teaches nothing and costs the reader four lines per item. The
recruiting source cards printed "no evidenced expertise yet", "no known path"
and "no title or affiliation on record" on nearly every draft; the owner's
verdict was that the card had become unreadable for the decision it exists to
support.

The rule: **render an absence when it would change what the reader does, and
otherwise let the row not exist.** "No citations — nothing here is backed" is
kept and stays loud, because accepting that record is the mistake it prevents.
"No path" is dropped, because a stranger off a crawl having no path is the
default, and the absence is visible in the row simply not being there.

Two corollaries from the same pass:

- **A repeated fact belongs to the container, not the item.** When every card
  in a list restates its query, its paper or its source, hoist it into the
  list's header once and leave each card only what differs.
- **Provenance on a card face is a HOST, never an address.** Full URLs and the
  trail that found them live in the disclosure and the `title`; a URL is
  reference material, and reference material wraps over three lines and pushes
  the deciding content off the screen.

---

*This contract systematizes discipline, not a frozen look. The tokens are the
vocabulary; the aesthetic they express stays open to refinement. When you change
a token value, you restyle the whole platform at once — which is the point.*


## Interaction contract (audit, 2026-09-05)

- **Native controls first.** Actions are buttons; destinations are links. Do not
  make a clickable `div` when a button works. Shared `collapsibleSection` supplies
  Enter/Space behavior, `aria-expanded`, and an associated body. Icon-only and
  collapsed-rail controls retain explicit accessible names; navigation exposes
  `aria-current="page"`.
- **Focus is visible.** The shared inset accent outline appears for keyboard
  focus, including inside clipped panels. Do not remove it in tab CSS. Selection
  uses a neutral surface plus a shape/border cue where appropriate; color alone
  must not carry essential state.
- **Dense does not mean tiny targets.** `--control-min` is 24px for desktop
  controls; `--control-height` is 28px for normal compact controls. Phone controls
  inherit the existing `--touch` / `--touch-sm` rules in `95-mobile.css`. Small
  text is acceptable for secondary metadata; essential instructions need readable
  body text. This is a design target, not a claim that every legacy control passes.
- **Modal lifecycle is shared.** Search and Cast use `containDialogFocus` to
  contain Tab/Shift+Tab, make the underlying page inert, and return focus on close.
  Include an accessible title, visible close button, and Escape behavior. Nested
  stages may use Escape to go back first. Clean up before hiding/removing; do not
  attach a modal lifecycle to an ordinary inspector or persistent workspace pane.
- **Palette results are one interaction model.** Arrow keys reach every listed
  command, including task capture; selected results stay in view and expose their
  state to assistive technology. Enter acts on the marked row. Opening/closing a
  palette must not let a late request overwrite the next session.
- **Report actual state.** Distinguish loading, failed loading, an empty collection,
  and no matches. Preserve the current query when data arrives. Use a concrete
  recovery action. Never show success until the operation is acknowledged.
- **Respect reduced motion.** Essential progress remains readable without animation.
  Shared CSS suppresses nonessential motion when the system requests it.

## Panels and future workspaces

Keep the existing shell, header, tokens, ghost controls, and shared card anatomy.
Write / Investigate / Build / Decide are task-scoped working modes within that
language. They do not each need a new visual system.

1. One dominant work region; subordinate evidence, tools, and agent commentary.
   An inspector appears only when the current content supports it. Avoid a
   permanent instruction panel that consumes space but offers no action.
2. Separate pane navigation from document controls and from external commitments.
   Keep the next action near its evidence, with precise verbs (e.g. Draft email,
   Review changes, Send). Preserve distinct meanings of Dismiss, Reject, Delete.
3. Use `--page-max` / `--page-gutter` for normal pages, `--measure` for readable
   prose, and `--inspector-w` for side inspection. Existing cockpit layouts may
   use the available width. A multi-pane workspace must explicitly define its
   own collapse order and scroll ownership before implementation.
4. At narrower widths, wrap metadata and actions before compressing the main
   content. The mobile Feed puts the title below its badges. Reuse the phone
   sheet pattern for inspectors; do not squeeze a writing canvas between rails.
5. Agent comments in the proposed writing panel belong to anchored annotations
   in a subordinate margin. References open to inspectable sources; suggestions
   are explicit accepts/dismissals. Human-authored text must remain human-authored.
6. Review new surfaces at desktop, tablet, and phone sizes in both themes. Check
   keyboard-only operation, long titles, loading/error/empty states, scroll and
   focus restoration, and edits during refresh. A screenshot alone is insufficient.

### Research grounding

[Cantina Creative's Iron Man 3 work](https://www.cantinacreative.com/film/iron-man-3)
is a visual reference for the Jarvis direction, not evidence of usable software.
Our interpretation is to retain layered panels, fine geometry, and restrained
illumination while giving the actual work the strongest hierarchy. Film HUD
animation and ornamental telemetry should not become routine interaction costs.

Behavior follows [WAI's modal dialog pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
and [WCAG's target-size guidance](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).
The latter specifies a 24×24 CSS-pixel minimum with exceptions including adequate
spacing and inline links. It does not require inflating every desktop control to
44px. See [the audit record](ui-ux-audit-2026-09-05.md) for scope and remaining debt.
