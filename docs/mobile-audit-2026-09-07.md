# Primary-view mobile audit — 2026-09-07

Browser walkthrough using live data through the read-only local preview. Reviewed Day, Tasks (list, board and task workspace), Chat, Feed, Goals, Aion recruiting (People and Sources), Real Estate, Calendar, Contacts, Agents, Writing and Settings. Initial walkthrough at 390×844; focused verification at 360×780; desktop Chat checked at 1280px. This is an expert responsive walkthrough, not a human usability study or physical-device Safari test.

## Findings and changes

- Chat's stacked conversation directory occupied most of a phone viewport. Added a phone-only Conversations disclosure, bounded directory scrolling, and automatic closing when selecting a thread. The transcript owns the remaining height and the composer remains in the viewport. Chat sizing also listens to visual viewport resizing for the on-screen keyboard; physical-device keyboard behavior still needs a device check.
- Tasks squeezed the search field between layout and add controls, truncating its hint. Search now has its own full-width row. Domain metadata aligns below the title and trailing actions share available space instead of always forcing another full-width row.
- Mobile forms retained small desktop text. Shared fields, task messages and chat composition now use the existing 16px body token.
- Date arrows, section tabs, recruiting origin controls and run/role disclosures needed larger touch targets. Increased their mobile hit areas. Recruiting segmented controls can wrap and long search headings break within their container.

## Verification

- At 360px, Tasks search remained readable; opened and closed its workspace without editing records.
- Chat Conversations opened a bounded 312px directory and closed again; the composer remained visible. Desktop retained its normal sidebar and hid the new mobile control.
- Sources run headings, role and status filters stayed inside the 360px content width; Settings also measured 360px without page-wide horizontal overflow.
- Calendar, Day, Feed, Goals, Real Estate, Contacts, Agents and Writing were visually inspected; retained existing local horizontal tab scrolling where intentional.
- JavaScript syntax checks, all eight recruiting regression tests, and `go test ./server` passed.

Changes are scoped to mobile CSS and phone chat behavior. No candidate decisions, task edits, outbound messages, or configuration writes were made during the walkthrough.
