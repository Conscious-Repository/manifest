# Calendar context request cancellation — September 25

Records search and preview requests now carry abort signals. Starting another
search, changing record kind, replacing a preview or closing the inspector
cancels the obsolete read. Aborted responses cannot repopulate results or show
an error over the current view. Explicit retention actions keep their existing
request/recovery behavior.

The search, preview and retain HTTP handlers now pass request context through
record lookup to the calendar source. Calendar reads retain the 15-second cap
and also end when the owning request is canceled. A source result arriving after
cancellation is refused. The non-request internal helpers retain their existing
interfaces.

Race-tested handler trials block an injected provider read, cancel each owning
request and verify provider cancellation, unsuccessful response and no new
artifact snapshot. Existing calendar source/version/privacy tests pass.
Chromium exercises replaced searches, kind changes and inspector closure with
real abort signals, alongside existing Records selection/recovery tests. Build,
syntax and diff checks pass. Full-suite and read-only deployment verification
are recorded in the plan checkpoint.

This closes a request-lifecycle gap in calendar context. Other unchecked
workbench requirements remain open.
