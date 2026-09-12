# Feed reading stream

The reference phone screenshot spent much of the first screen on a multi-row toolbar, boxed metadata, monospace post text and stretched action buttons. Feed now uses the existing readable measure on desktop, divider rows, sans-serif reading text, compact metadata and quiet inline reading/clearing actions. Default unfiled labels are omitted. Infrequent create/run tools use chooseActionMenu, which now accepts an optional accessible label without changing existing callers. Filter controls render before the slow feed request finishes.

Article titles and post text are native reader links. Existing handlers still own read, curate, dismiss, undo, approval, errand and ritual actions; no data/schedule changes were made. Approval cards retain explicit touch-sized decisions and payloads. Feed-only selectors preserve shared cards elsewhere and the docked approval inspector.

Validation: Chromium fixture rendering using real card functions with synthetic posts/articles/findings at 320/390/768/1000/1440 in light and Jarvis; checked containment, filters, native reader links, prose font, menu opening/Escape/focus restoration. Visual inspection of phone and desktop. Browser harness blocks non-GET/HEAD requests; no real publication, dismissal, job, send or approval was executed. Existing Feed/Consume/UI/theme/CSS tests and Go build passed. Physical-device acceptance remains user feedback.

Temporary QA: /tmp/feed-style-check.cjs and /tmp/feed-<width>-<theme>.png. No synthetic items were saved to the live feed.
