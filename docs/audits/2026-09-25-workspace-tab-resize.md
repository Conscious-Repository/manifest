# Workspace tab visibility after resizing

The per-delivery result verification exposed an active Context tab disappearing from the visible tab strip after rapid 320→390→1440→390 viewport changes. Reproduction measured the strip at x=20..298, the selected tab at x=427.55..472.36 and scrollLeft=0. The Context pane remained open. This was real clipping, not fractional boundary noise.

The existing ResizeObserver alone can miss a rapid round trip to the same observed strip size while the wide layout clamps scrollLeft to zero. The workspace now also schedules active-tab revelation after window resize, coalescing animation frames and reading final layout. It changes only strip scrolling: selection, focus, drafts and mounted content remain intact. Closing the workspace removes the listener and cancels pending work. Manual tab-strip scrolling is unaffected.

Evidence:
- The original reproduction timed out at the active-tab visibility assertion and captured the offscreen selection in `/tmp/manifest-tab-resize-repro.png`.
- With the fix, the same reproduction passed: scrollLeft=213, selected tab x=214.55..259.36 within the x=20..298 strip.
- `server/testdata/chat-workspace-tabs.cjs` now retains rapid round-trip and desktop→320/390 regressions. It verifies tab visibility, instruction selection, disclosure, unchanged keyboard focus and full-length result metadata bounds. Existing draft/pane/side-chat recovery checks pass without page errors.
- `/tmp/manifest-native-result-phone.png` inspected after the regression shows Context visible and focus retained on the instruction selector.
- JavaScript syntax, diff check and release build pass.

This resolves the measured resize limitation noted in the native delivery result audit. It is headless Chromium viewport evidence, not physical-phone keyboard/backgrounding or complete accessibility acceptance.

Full `make test` passed server (41.076s) and other packages except the two pre-existing canary source-hash failures for unchanged Hermes authority/successor files.
