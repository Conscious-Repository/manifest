# Explicit post-runtime-fix extraction attempt

`extractor-reconcile -operation post-runtime-fix` authorizes one child of an
existing pre-provider reconciliation attempt. Required arguments are `-job`
(original job ID), `-source`, `-source-sha256`, `-parent-attempt`,
`-owner-authorization`, and `-runtime-fix`, in addition to `-config` and the
configured private Hermes `TMPDIR`. The runtime reference records the operator's
verified installed fix; the command does not install or independently attest it.

Only an AION parent with immutable original-job/reconciliation bindings, matching
source bytes and context, and the current ownership revision is eligible. It must
be uncertain, replay=false, unpublished, and have no execution receipt, model,
cost, or candidates. Accepted reasons are the exact bounded-execution-unverified,
bounded-Hermes-execution-failed, or initialization-filesystem-boundary messages.
Other uncertain states require separate owner investigation.

The child ID binds the parent, runtime reference, and source hash. A durable
`domain-extraction/runtime-reconciliations/<original-job-id>.json` receipt blocks
all subsequent children, including different runtime references and a crash
before job creation. Do not delete receipts or reset jobs to retry. Original
jobs, attempts, reconciliation receipts, approvals, and vault files are preserved.
The normal worker verifies provider/model/HTTP/usage evidence and publishes only
snapshot-bound, deduplicated pending proposals through `ProposeOnce`. Approval
remains a separate owner action.
