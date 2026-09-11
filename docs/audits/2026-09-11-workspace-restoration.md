# Workspace restoration checkpoint

Inspector view state now uses the existing private revisioned chat-state store in a separate `workspace` slot, keyed by canonical conversation identity. Artifact/plan tabs retain exact selected revision, active tab and preview scroll position; project-context tabs and created side-chat tabs reopen. A hidden inspector remains hidden. Side-chat restoration loads the already created conversation and never calls creation or dispatch endpoints.

Navigation captures view state before disposing components. Artifact edits continue using their existing independent recovery store. Capturing scroll before hiding avoids a zero offset from hidden DOM layout; restoration waits for loaded content and visible layout. A route/key guard prevents late state responses from restoring the previous conversation's tabs into the next conversation. Workspace convenience-state conflicts accept the other device's stored bookmark without changing the current visible pane.

Validation: server suite, chatstate restart/conflict tests, Go build, actual artifact editor fixture, extended Chromium workspace fixture with hidden-pane restoration, 180px preview scroll, saved side-chat route, canonical conversation isolation, existing draft preservation and phone bounds. Production is unchanged.

Remaining: attachment-tab restoration, unfinished side-chat setup recovery, artifact preview submode (compare/edit) and diff file selection, full provider journey, comprehensive visual audit, release gates. This checkpoint is not completion of inspector continuity or the whole workbench plan.

## Per-file review follow-up

Working-folder diffs now expose every changed file as a collapsible row, with added/deleted/renamed/modified status and hunk addition/deletion counts. First file opens initially; expand/collapse-all controls keep larger reviews navigable. Individual diffs render lazily and retain literal text and line gutters. Expanded file paths are included in inspector view restoration and applied before restoring scroll.

Chromium fixtures cover literal script-like text, line numbers, per-file and all-file disclosure, phone overflow, and restored independent file expansion after navigation. The full server run found only a duplicate CSS selector introduced in this follow-up; its declarations were merged and the focused CSS check rerun. Syntax highlighting, diff-region annotations, preview compare/edit-mode restoration, and the larger plan gates remain open.

## Attachments and unfinished side-chat recovery

Attachment tabs now retain their original reference and preview position. Unfinished side-chat setup retains agent/model/folder settings in the existing conversation workspace state. Before creation, its exact payload and request ID are saved; an uncertain response locks those settings and offers `Retry creation`. Restoring a pending setup does not execute it. An explicit retry checks the same request identity. A created side-chat tab continues to restore its existing route.

Extended Chromium coverage exercises response loss, navigation, state-store reopening, exact retry identity/payload, no automatic creation, and actual attachment text preview restoration at 120px. File-edit drafts remain separate. Remaining continuity work includes compare/edit preview submodes and explicit side-chat return-to-parent findings, alongside the full plan gates.

## Comparison-mode restoration

Artifact view state now records preview versus comparison mode. Restoring a comparison reloads the selected immutable version and its preceding revision before applying scroll. It does not enter editing or save a version. The actual artifact-editor browser fixture closes and rebuilds the preview and verifies comparison mode remains selected. Edit-mode restoration remains outstanding.

## Keyboard navigation checkpoint

Added Ctrl+Alt shortcuts for new chat (N), search (F), composer (M), inspector (I), visible previous/next chat (arrows), and next visible input/review/failure item (J). Help is available in the workspace chooser. Modal dialogs, composition, ordinary typing and non-chat routes are excluded. The shortcut fixture verifies focus, navigation, attention targeting and exclusions; the existing workspace browser fixture passes. Stop-run keyboard handling and full accessibility/live audit remain open. Computer-use live audit was attempted but the tool reported the Mac locked; unlock was requested while repository work continued.

## Visible native stop control

A positively live native session now exposes Stop in the header using the existing armed confirmation and kill path. Ctrl+Alt+X arms and focuses it; another shortcut press does not confirm it, and held-key repeats are ignored. Enter/click confirms through the existing control. Live header repaint preserves an armed enabled control. Ended/unknown sessions do not receive a new stop action. Phone tests exposed overflow in the confirmation label; the label now wraps within its control.

Verified: shortcut regression (no repeated-key confirmation), desktop/390px header and confirmation bounds, server suite, focused CSS duplicate-selector validation and build. Other adapters still need their supported interruption paths audited; this does not invent unsupported stop capabilities.
