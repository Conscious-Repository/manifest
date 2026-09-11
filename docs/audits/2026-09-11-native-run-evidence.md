# Native run evidence checkpoint

Codex rollout projection now reads explicit `task_started` / `task_complete` records separately from transcript messages. Current-run evidence includes a provider turn ID when available, otherwise an immutable source-record ID, with timestamp and evidence ID. Full and tail parses produce the same evidence identity. Tail endpoints return the full current-run projection, preventing an empty tail from erasing completion.

A newer owner instruction without a known start clears prior completion to unknown. A completion naming a different run cannot finish the active run. Partial JSONL records are not consumed. The private session list exposes this evidence; the sidebar can identify a completed run even after the process stops. A new live working observation supersedes an earlier completion. No idle/screen/final-message text is treated as completion evidence.

Validation: focused parser/golden/endpoint/attention checks and build. The full server run uncovered only an extracted JavaScript fixture missing the new session-lookup dependency; the fixture was updated and its regression rerun. No real provider send or deployment.

Remaining: normalized append-only run/event history across all adapters, cancellation/failure/approval evidence, provider capabilities, and integrated native lifecycle journeys. Claude remains unverified where its adapter supplies no explicit run evidence. This is not a claim that the run-model phase is complete.
