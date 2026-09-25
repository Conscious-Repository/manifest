# Native chat preflight failure receipts — September 25

A continued native chat could claim a delivery, discover that its originating
coding runtime was missing/replaced/remote, and return without finishing the
receipt. The conversation then remained thinking although no invocation began.
These failures now atomically append a system explanation and mark the delivery
failed. An unavailable runner is handled through the same path instead of
accessing a missing runner. The message explicitly states that no agent
invocation started. Failed receipt identity remains durable and retry-safe.

Race tests cover absent runtime registry, missing runtime, replaced agent kind,
remote runtime and missing runner. Two queued instructions each finish with a
failed receipt and no tool-scope dispatch metadata; the session becomes idle.
Reopening/recovery retains failed state, and duplicate acceptance cannot append
another user/reply pair. Existing unavailable-context and successful tool-scope
checks pass. Full-suite/build/live results are recorded in the plan checkpoint.

This fixes pre-invocation failure supervision. Native chat still lacks an
explicit stop endpoint and per-delivery runner cancellation contract; this change
does not cancel running work or claim completion of that requirement.
