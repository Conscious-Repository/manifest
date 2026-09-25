# Native queued versus running state

Accept marks a native conversation thinking before Claim dispatches its queued instruction. The workspace previously let this broad summary flag override the durable queued receipt, displaying Working before dispatch.

Explicit running receipts now take precedence, followed by queued receipts, then the existing legacy summary fallback. An active interruption remains visible even with later queued instructions. Cancelled instructions remain excluded from last-result status. The transcript waiting indicator and composer guidance use the same projection. Existing active-work filtering still includes both running and queued work; review state remains independent.

Evidence:
- `chat-attention.cjs` verifies accept-before-claim state, earlier completion followed by a queue, active interruption with later queue, receipt precedence, cancelled history and existing filter behavior.
- `chat-stage-switch-browser.cjs` loads the actual frontend with fixture APIs and verifies Queued → Working → Interruption requested in both transcript and composer. Existing cached/eager thread switching and phone error navigation pass without page errors.
- Task-row and artifact navigation race checks, JavaScript syntax, diff checks and release build pass.
- Full `make test` passed server (41.878s) and other packages except the two pre-existing Hermes source-hash canary failures.

This does not claim an actual provider has started from the legacy thinking fallback, change queue dispatch, or normalize every adapter. No production prompt or cancellation is used in verification.
