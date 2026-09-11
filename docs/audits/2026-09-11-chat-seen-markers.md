# Changed since last viewed

Inbox rows can indicate `new` when their activity differs from the marker saved on the last visit. Private convenience state uses the existing revisioned chat-state store (`inbox/seen`), not an authored work-record store. Native summaries include their exact transcript offset, so new transcript records change the marker independently of title and process state.

A marker is recorded only when the document is visible and the latest transcript region is visible. Older-history reading does not clear it. Compare-and-swap retries merge other conversation markers; older visits do not overwrite newer-device timestamps. No unread/accepted claim is inferred for conversations that have never been visited.

Tests cover CAS merge, duplicate-write suppression, latest/visibility gating, offset changes and store restart. The broader server run found only a reading-fixture dependency that was updated and rerun; focused server/state and desktop/phone header checks passed. Build passed. Full live inbox visual verification and notification semantics remain outstanding. Not deployed.
