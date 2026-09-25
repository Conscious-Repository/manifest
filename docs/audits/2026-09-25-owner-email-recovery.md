# Owner email delivery recovery — September 25

The shared canonical email card now offers Check delivery for partial/interrupted
attempts. The private POST operation endpoint passes no submitted sender, payload
or raw evidence to the recovery mechanism. It validates the stored owner approval,
operation version/hash, immutable preview/envelope and current domain sender
mapping, then uses that sender's read-only Gmail connection to fetch unique Sent
evidence. The existing complete-envelope comparator must accept it before the
canonical operation becomes succeeded. There is no send or new approval call.
The provider read is bounded to 45 seconds and propagates HTTP cancellation.

Saved delivery receipts can repair interrupted operation writes without another
provider read. Reply monitoring starts after confirmation only if requested in
the frozen approval. Reconciliation provenance is exposed in the canonical result.
Failed checks preserve the original uncertain receipt and show a retryable error;
the browser does not offer resend. Success replaces the current card with the
same canonical receipt and announces the approval update to other projections.

Evidence: Manifest MCP race tests cover lost acknowledgement recovery, exact
sender/message lookup, request deadline, restart/idempotency, confirmed-outcome
readback, reply tracking, changed approval/version/mapping refusal, missing or
changed evidence, cancellation and operation-write repair. Server race tests
cover public portal denial, pending-approval refusal, saved-delivery repair and
ignored forged request-body fields. Chromium exercises the actual shared card,
failed-read retry, success replacement, status gating and 320/390/1440px bounds;
the phone screenshot was inspected. Full-suite/build/live results are in the
plan checkpoint. No real mailbox, approval, provider send or recovery mutation
was used in production verification.

Live Gmail lost-ack acceptance remains unverified. Supported MIME representations
are deliberately bounded; unsupported transformations leave delivery unresolved.
Personal sender support and the other unchecked workbench requirements remain
open. This does not prove completion of the whole plan.
