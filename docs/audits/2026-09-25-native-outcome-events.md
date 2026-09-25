# Native outcome events follow saved receipts

Native Hermes runner returns now project their saved delivery state into activity events. A runner error after an owner stop emits `run.interrupted`, and a successful return racing that stop also emits `run.interrupted` rather than an assistant-completion event. Ordinary failure retains `run.failed`; ordinary completion retains `chat.assistant`. If saving the result fails, no outcome is emitted.

Events now carry the exact request ID, source agent, conversation, user/reply turn numbers and delivery state. Selected/requested model are separate from runner-reported model; missing telemetry does not substitute the selected model. Reported Hermes session/model come from the per-delivery result. Existing owner input events already carry the same request ID.

Evidence:
- Focused server race tests pass for actual started-runner HTTP interruption, ordinary success, reported failure, missing telemetry and existing assistant ledger contracts.
- `agentchat_outcome_test.go` reproduces a successful return after durable stop and checks one interrupted event with exact receipt metadata. Queued/running/cancelled/nonterminal records are not outcomes.
- The failed-save fixture starts a controlled CLI, makes the conversation file unavailable before releasing its failure, and verifies a persistence error with no activity outcome.
- `agentchat_result_test.go` checks actual usage-file ingestion through saved receipts and corresponding events, preserving selected versus reported model.

The existing ledger append remains best-effort. This does not add atomic receipt/event writes, reconstruct missing events after a crash, rewrite old events, or add preflight/restart events. Delivery receipts remain authoritative. No production run, stop, approval or outbound action is used for verification.

Full `make test` passed server (41.863s) and other packages except the two pre-existing canary source-hash failures for unchanged Hermes authority/successor files. Release build and diff checks pass.
