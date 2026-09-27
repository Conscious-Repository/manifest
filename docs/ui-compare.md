# ui-compare: a virtual browser for UI/UX work

`tools/ui-compare/ui-compare` captures a page in headless Chromium at phone to desktop widths, measures what rendered, and compares two pages side by side. The owner and the coding agents (Codex, Claude Code) use the same tool. The skill that tells agents about it is `tools/ui-compare/SKILL.md`, linked into `~/.codex/skills/ui-compare` and `~/.claude/skills/ui-compare` on metis.

## What it produces

For each side and width: a full-page PNG, and measured design tokens (typefaces, text sizes and weights, line height, text and background colors, radii, control padding, control heights and how many fall under 44 px, the widest text, horizontal overflow and which elements cause it, console errors). `summary.md` lists them with the differences between the two sides. `report.html` shows the screenshots in pairs.

## Limits, stated in its output

- Token differences are not a pixel diff. Layout and hierarchy are read from the screenshots.
- A bot check (Cloudflare and the like) is reported as BLOCKED and never bypassed. A page behind a login shows its login page. For either, compare against a screenshot: `ui-compare compare <ours> reference.png`.
- Phone widths emulate a phone, so a page without a viewport meta tag renders at desktop width.

Pinned by `TestFixtureUICompare` (`server/testdata/ui-compare.cjs`).
