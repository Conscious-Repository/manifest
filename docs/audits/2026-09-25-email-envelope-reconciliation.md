# Email envelope reconciliation — September 25

`gmailsend.MatchSentEnvelope` compares raw provider evidence with the complete
frozen message produced by the existing builder. It includes From/To/Cc,
subject, date, message identity, threading, body and attachment name/type/bytes.
Duplicate or unexpected authored headers and changed content are refused.
Known delivery/authentication transport headers may be added. Address/subject/date
representations, MIME boundary names, and base64/quoted-printable transport
encoding may differ when the decoded content and metadata remain equal. Body
bytes are not trimmed, case-folded or rewritten. Unsupported MIME layouts and
unknown extra headers remain unresolved instead of assuming equivalence.

`DeliveryStore.ReconcileApproved` consumes an independently authorized immutable
envelope hash and a trusted read-only provider lookup callback. Only uncertain
attempts can acquire new evidence. The callback receives the frozen sender and
Message-ID; returned mailbox and provider message/thread IDs are checked, then
the entire message is compared. Success records the existing provider receipt
plus mailbox, raw-evidence SHA-256 and check time under the same durable delivery
lock. Raw mailbox bytes are not persisted. Completed retries return the saved
receipt without fetching evidence again. Prepared messages cannot be reconciled;
missing/invalid evidence does not release the send boundary. This is not a new
approval system, and the callback must never accept browser-supplied evidence.

Race tests cover exact/plain/attachment messages, equivalent MIME encoding and
boundaries, recipient/body/attachment/header changes, invalid encodings,
concurrent reopen/recovery, immutable approval hashes, canceled/unavailable reads,
wrong mailbox or missing thread identity, prepared-message refusal, and receipt
write failure followed by recovery. Subsequent send calls cannot resend an
uncertain or recovered attempt. No real mailbox or provider send is used.

This completes the delivery-store mechanism, not the owner-facing workflow.
Still required: canonical operation approval/mapping checks, wiring the existing
Gmail evidence reader into a private explicit recovery endpoint, the shared
approval card action, and integrated operation/browser recovery tests. Full-suite,
build and deployment evidence is recorded in the workbench plan checkpoint.
