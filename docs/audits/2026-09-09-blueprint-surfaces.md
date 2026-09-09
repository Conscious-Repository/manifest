# Manifest blueprint surface audit

September 9, 2026. Scope: Manifest's Jarvis theme, starting with chat and extending through shared controls, task cards, recruiting editors, command menus, and terminal chrome.

## Design direction

A continuous blue drafting surface, with clear text and restrained lines. The user's screenshots show the main problem: almost-black search/filter blocks and a large almost-black conversation header interrupt the canvas. Their weight is unrelated to their importance. This is a surface hierarchy problem, not a need for more decoration.

Keep the existing structure, compact labels, grid, and conversational message shapes. Use shared rules so subsequent features fit naturally.

## Reference study and limits

Territory Studio's designers describe Far From Home's Stark/E.D.I.T.H. and Fury interfaces as visual languages developed around specific story requirements. They distinguish ambient screens from focal screens that must explain something quickly, and describe modular design frameworks for consistency.[1] The useful translation for Manifest is a stable visual grammar with a clear focal point, not the density of cinematic background graphics.

Cantina Creative's Iron Man 3 project documents its HUD and graphics work with process reels and stills.[2] It is a visual reference, not evidence that a movie HUD is suitable for everyday work. Film interfaces are staged for an audience; Manifest must support repeat interaction, keyboard navigation, and phone use.

No sufficiently reliable production breakdown of Brand New Day's computer interfaces was established during this research. That reference remains a direction supplied by the user; no specific design claims here are attributed to that film.

Nielsen Norman Group's minimalism guidance makes relevance, rather than an empty screen, the target. Its flat-design guidance warns against losing clickability when simplifying surfaces.[3][4] Consequently, fields retain visible outlines and focus indicators, while purely structural containers lose their unnecessary fill.

WCAG's contrast guidance requires at least 4.5:1 for ordinary text, with a 3:1 exception for large text.[5] Pale blue text on navy is the intended text hierarchy. This pass improves placeholder readability and visible keyboard focus; it does not constitute an application-wide accessibility certification.

## Audit findings and treatment

| Area | Finding | Treatment |
| --- | --- | --- |
| Shared theme | `--bg` mapped ordinary UI to near-black, also used for panels and overlays | Raise the Jarvis legacy surface to navy; introduce explicit surface roles |
| Conversation header | Large filled slab gives routine metadata excessive weight | Transparent header outside the independently scrolling transcript |
| Inbox controls | Black wrapper plus black fields creates nested blocks | Navy scrolling backing, slightly lighter blue fields |
| Composer and terminal controls | Ordinary inputs look like dark terminal windows | Shared field surface; preserve familiar borders and focus |
| Board | Cards appear as cutouts | Shared blue panel surface |
| Recruiting | Editor fields and sticky save strip use the same generic background | Field role for editing; opaque overlay role for save strip |
| Menus, picker modal, command palette | Need separation and must cover content beneath | Opaque blue overlay surface; retain boundaries and existing elevation |
| Actual terminal | Dense ANSI content needs a predictable opaque background | Preserve dedicated terminal tokens |
| Remaining legacy modules | Many consume `--bg` rather than semantic roles | Navy compatibility token removes black cutouts; migrate to roles as components are edited |

## Generalizable implementation rules

1. **Structure with space and lines first.** A heading belongs to its page. Add a fill only when it distinguishes an editable area, grouped content, or an overlapping layer.
2. **Use four surface roles.** `--surface-header` for ordinary structural headers; `--surface-field` for inputs and composers; `--surface-panel` for grouped content; `--surface-overlay` for menus, modals, and sticky UI covering scrolled content.
3. **Transparency is positional.** Never make an overlay transparent simply to achieve a holographic look. The chat header is safe because it sits outside the transcript scroll area. Inbox controls still occlude the rows beneath.
4. **Keep color meaningful.** Pale ink for content, muted blue for secondary labels, existing cyan accent for active/live controls, semantic colors for status. Do not add decorative warning colors.
5. **Keep controls recognizable.** Retain field outlines, labels, existing control shapes, and obvious keyboard focus. Avoid replacing buttons with unexplained glyphs.
6. **Let content be the focal point.** Metadata is smaller and quieter; prose uses the existing readable sans face. Mono remains appropriate for labels, time, and terminal content.
7. **Avoid stacking containers.** Do not put a filled header inside a filled wrapper unless those layers represent distinct interactions.
8. **Use the grid once.** The page owns the blueprint grid. Components should not each add grids, corner brackets, scan lines, or decorative telemetry.
9. **Motion must report activity.** No new ambient pulsing, scanning, rotating ornaments, or transition dependencies are introduced by this pass.
10. **Preserve phone operation.** Changes must keep tap targets, composer position, long-title wrapping, and session switching intact. A cinematic reference is not a reason to shrink controls or text.

## Review checklist for future components

- Can the user immediately identify the current item and the next action?
- Is each filled container serving a distinct role?
- Does an overlapping element fully obscure content beneath it?
- Are interactive controls still recognizable without hover?
- Are important labels readable without relying on glow?
- Does the screen work at phone width without horizontal page scrolling?
- Are colors, spacing, and typography taken from shared tokens?

## Sources

[1] [Territory Studio interview: Far From Home and cinematic interface design](https://scifiinterfaces.com/2020/06/23/scifi-interfaces-qa-with-territory-studio/), June 23, 2020. Direct interview with the designers.

[2] [Cantina Creative — Iron Man 3](https://www.cantinacreative.com/film/iron-man-3). Original studio project page.

[3] [Nielsen Norman Group — Aesthetic and Minimalist Design](https://www.nngroup.com/articles/aesthetic-minimalist-design/).

[4] [Nielsen Norman Group — Flat-Design Best Practices](https://www.nngroup.com/articles/flat-design-best-practices/).

[5] [W3C — Understanding Contrast (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html).
