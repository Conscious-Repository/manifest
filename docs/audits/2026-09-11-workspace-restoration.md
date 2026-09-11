# Workspace restoration checkpoint

Inspector view state now uses the existing private revisioned chat-state store in a separate `workspace` slot, keyed by canonical conversation identity. Artifact/plan tabs retain exact selected revision, active tab and preview scroll position; project-context tabs and created side-chat tabs reopen. A hidden inspector remains hidden. Side-chat restoration loads the already created conversation and never calls creation or dispatch endpoints.

Navigation captures view state before disposing components. Artifact edits continue using their existing independent recovery store. Capturing scroll before hiding avoids a zero offset from hidden DOM layout; restoration waits for loaded content and visible layout. A route/key guard prevents late state responses from restoring the previous conversation's tabs into the next conversation. Workspace convenience-state conflicts accept the other device's stored bookmark without changing the current visible pane.

Validation: server suite, chatstate restart/conflict tests, Go build, actual artifact editor fixture, extended Chromium workspace fixture with hidden-pane restoration, 180px preview scroll, saved side-chat route, canonical conversation isolation, existing draft preservation and phone bounds. Production is unchanged.

Remaining: attachment-tab restoration, unfinished side-chat setup recovery, artifact preview submode (compare/edit) and diff file selection, full provider journey, comprehensive visual audit, release gates. This checkpoint is not completion of inspector continuity or the whole workbench plan.

## Per-file review follow-up

Working-folder diffs now expose every changed file as a collapsible row, with added/deleted/renamed/modified status and hunk addition/deletion counts. First file opens initially; expand/collapse-all controls keep larger reviews navigable. Individual diffs render lazily and retain literal text and line gutters. Expanded file paths are included in inspector view restoration and applied before restoring scroll.

Chromium fixtures cover literal script-like text, line numbers, per-file and all-file disclosure, phone overflow, and restored independent file expansion after navigation. The full server run found only a duplicate CSS selector introduced in this follow-up; its declarations were merged and the focused CSS check rerun. Syntax highlighting, diff-region annotations, preview compare/edit-mode restoration, and the larger plan gates remain open.
