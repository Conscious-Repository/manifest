# Workbench activity inspector

Added a persistent Activity tab for private conversation workspaces, using the same authorized transcript projections as the center view. Filters expose instructions, narration, tools/commands, errors and the existing canonical approval cards. Command input and output remain literal text in expandable rows. Errors retain a visible label. Existing transcript updates refresh the projection; unchanged updates are ignored and no new poller is introduced.

Filter, expanded rows and scroll position use the existing workspace view store. Disposal removes the update listener, and conversation identity gates prevent another thread from updating the pane. The browser test caught a deferred disclosure-event race during disposal/restoration; disconnected elements can no longer mutate restored expansion state.

Validation: full `go test ./...`, focused server chat/artifact tests, actual workspace browser fixture, syntax and whitespace checks. Phone fixture was visually inspected and the selector corrected to shared tokens and a 44px touch target. Browser coverage includes literal HTML in command input, filtering errors, destruction/restoration, updated events, and phone overflow. Existing canonical approval tests pass; this fixture does not execute approvals or call real agents.

Remaining: a comprehensive Context view, artifact entries and provenance within the activity history, cross-adapter normalized durable run/event records, integrated live-provider and release verification. This is a projection of available history, not a claim that every provider emits a complete execution log. Not deployed.
