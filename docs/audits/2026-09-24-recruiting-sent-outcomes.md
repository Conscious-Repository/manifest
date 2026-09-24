# Recording canonical sent outcomes in recruiting — September 24

Confirmed canonical email cards now offer `record sent outcome`. This explicit
owner action records the confirmed send in recruiting and advances an earlier-stage
candidate to outreach. It is separate from approving the email; it never invokes
or retries the sender. Later candidate stages are preserved.

The endpoint obtains the immutable operation, verifies owner approval and payload
hashes, checks its confirmed delivery envelope/provider IDs, and uses the source
metadata from the approved preview. Pending, uncertain, mismatched and wrong-candidate
receipts cannot be recorded. The recruiting store independently matches the exact
historical source-draft revision and email contents before writing.

The append-only sent row references the canonical operation and source draft
sequence. A late receipt consumes only its source draft: a newer unsent draft
remains editable and preparable. The candidate pointer keeps the latest send's
provider IDs even if outcomes are recorded out of order. No second provider
receipt or approval authority is introduced.

Log append and candidate update use the existing recruiting write capability.
They are separate writes: if candidate persistence fails, retry reuses the existing
sent row and repairs the pointer/stage. Applied-operation markers live on the
existing candidate outreach pointer. Once applied, retry preserves subsequent
owner edits instead of advancing a deliberately changed stage again. The UI shows
whether recording is complete or needs finishing. This is recovery across those
two writes, not a new general transaction or cross-process writer protocol.

Evidence:

- Race-tested `TestRecordOutreachOutcomeRecoversPartialWriteAndPreservesNewerDraft`:
  injected candidate-write failure after log append; immutable prior bytes;
  retry without duplicate row; newer draft preserved; pointer/stage repair;
  applied-marker idempotence after owner edits; conflicting payload refusal;
  new grammar fields retain serialization fixpoint.
- Race-tested `TestRecordOutreachOutcomeConsumesOnlyMatchingDraft`: out-of-order
  confirmed outcomes keep the newest provider pointer, retain both operation
  markers and do not resurrect an already sent draft.
- `TestEmailApprovalAndDeliveryRecovery` now verifies confirmed outcome reading
  and refusal of uncertain delivery. The expanded sourcing journey verifies
  pending/wrong-candidate refusal, repeated endpoint calls, stage/pointer update
  and unchanged fake provider send count.
- Chromium uses the actual recruiting section and approval card: Approve → sent
  receipt → record outcome → recorded state, with no second decision/send. The
  source-sequence helper preserves a newer draft. Existing refusal/retry,
  unsaved-edit, candidate-isolation and viewport checks pass; screenshot inspected.
- Focused race checks, JS syntax, diff checks and release build pass. `make test`
  passed server (40.608s), recruiting, Manifest MCP and other packages except the
  known unchanged Hermes source-hash canary re-audit failure.

Provider evidence uses local fakes. Recording existing production outcomes was not
part of this implementation check. Lost-provider-ack reconciliation, remaining
context kinds and broader adapter/physical-device acceptance remain open.
