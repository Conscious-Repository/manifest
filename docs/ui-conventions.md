# Manifest UI Conventions

The dashboard's design contract. This file is the source of truth for the look
and feel — with no linter or CI, it *is* the enforcement surface: what review
checks against and what the next tab is built from. AION portal excluded (it
keeps its own conventions in `server/web/portal/`).

Doctrine (ARCHITECTURE.md §1): **manifest-quiet everywhere** — mono labels,
hairlines, ghost inputs, muted dots, one accent for live/active. Loud color is
reserved for nothing. §11: **one JS module + one CSS file per tab, composed
from the shared component library. No new UI idiom without touching the library
first.**

---

## The one rule

**No raw hex or raw px in a tab's CSS.** Every color, size, and space comes from
a token defined in `css/00-core.css`. The token file is the *only* place literals
live. If you need a value the scale doesn't have, add it to the scale — don't
inline it. (Exceptions: `box-shadow` offsets, and a color sitting on an accent
fill or a photo.)

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

*This contract systematizes discipline, not a frozen look. The tokens are the
vocabulary; the aesthetic they express stays open to refinement. When you change
a token value, you restyle the whole platform at once — which is the point.*
