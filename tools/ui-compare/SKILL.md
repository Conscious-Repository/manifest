---
name: ui-compare
description: Use a virtual (headless) browser to see a web UI — screenshots at phone to desktop widths, measured design tokens, overflow and console errors — and compare it side by side with a reference platform or a screenshot the owner provides. Use when asked to troubleshoot UI/UX, check a page on a phone width, or "compare our X with platform Y and close the gaps".
---

# ui-compare — see a page, compare it, close the gaps

The tool lives in the Manifest repo: `~/src/manifest/tools/ui-compare/ui-compare` (metis). It needs no setup there.

## Commands

```bash
T=~/src/manifest/tools/ui-compare/ui-compare
$T shot    <url>                     --out .ui-compare/<name>      # one page
$T compare <ours-url> <reference-url> --out .ui-compare/<name>      # side by side
$T compare <ours-url> reference.png  --out .ui-compare/<name>      # vs a screenshot
```

Options: `--widths 390,768,1280,1440` · `--theme dark` · `--selector CSS` (also crop one element) · `--before JS` (run before capture, e.g. open a menu) · `--wait MS` · `--viewport-only` · `--label-a ours --label-b codex`.

It writes `summary.md` (read this first), `report.html` (the side-by-side the owner can open from the chat's Files tab), one PNG per side per width, and `tokens.json`. Read the PNGs too: tokens describe type, color, radius, spacing and control sizes, not layout or hierarchy.

## Workflow for "match platform X"

1. Capture both at the same widths. Manifest's own pages are at `http://127.0.0.1:7777/#/…` on metis.
2. If the reference is BLOCKED (a bot check) or needs a login, do not try to get around it. Ask the owner for a screenshot of the reference and use `compare <ours> reference.png`.
3. List the concrete gaps: each with the measured values or the screenshot evidence, and the file and rule you would change.
4. Change the smallest thing that closes each gap, using the design tokens already in `server/web/css/00-core.css` and `05-primitives.css`.
5. Capture again and show the before and after in your reply, with the report path.

## Care

- Loading a Manifest page as a viewer can mark conversations seen or save reading position; do not click through the owner's live chats. Prefer the test fixtures (`server/testdata/*.cjs`) for anything that changes state.
- A phone width emulates a phone: a page without a viewport meta tag lays out at desktop width, as a real phone would.
