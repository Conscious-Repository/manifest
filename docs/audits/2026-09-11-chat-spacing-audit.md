# Chat spacing audit — September 11

Scope: live project creation, New chat, agent/model dialog, side-chat setup, project assignment, and mobile chat header. Navigation only; no real project or agent messages created during audit.

Loop 1 — reproduce and measure on live server:
- Project creation had computed padding of **0px**, at both desktop and phone widths. Shared dialog CSS referenced undefined `--sp-16`.
- Footer buttons mixed unstyled native gray controls with chat-specific styling.
- New-chat model strings competed with agent labels for width.
- Side-chat forms stretched with the pane, rather than maintaining a comfortable form width.

Loop 2 — render local changes against live data:
- Shared dialog baseline now uses real spacing: 24px desktop / 20px phone padding, explicit typography, 16px body gaps, 24px footer separation and aligned actions. Empty error/status paragraphs take no space.
- New project is bounded at 400px, with a visible Name label and explicit primary action.
- New chat labels its Project selector; choices retain one-line agent names while secondary model names truncate.
- Model sits immediately after agent; working folder follows. Agent controls use consistent labels, field heights and footer actions.
- Side-chat form is bounded at 480px, with consistent field styling and secondary hint text.
- Mobile chat header stays in one row. Project assignment truncates long conversation descriptions to two lines, preserving the full title as a tooltip.
- Replaced undefined `--base-90` references in the touched shared/chat styles with a defined text color.

Loop 3 — verify:
- Visually inspected desktop and phone screenshots of all above flows, including a deliberately long project-assignment title.
- Project dialog now measures 400px wide with 24px padding on desktop, 356px wide with 20px padding at a 390px viewport.
- Real dialog fixture validates content/footer insets at 320/390/1440px in both themes and checks that touched CSS tokens resolve.
- Server tests/build and lifecycle, compact shell, workspace-tab fixtures passed. No horizontal document overflow in audited paths.

These are scoped interface fixes; provider execution and unrelated portal screens were not exercised.
