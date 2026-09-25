# Sourcing through lost-ack recovery — September 25

The sourcing-to-outreach integration test now runs confirmed and lost-ack variants.
Both source a candidate with evidence, apply the reviewed fit gate, prepare an
immutable outreach approval, preserve later unapproved drafts and use the
canonical owner approval endpoint. The fake HTTP sender records accepted bytes;
in the lost-ack variant it closes the connection before returning the second
send's acknowledgement. Separate attempts use distinct provider message/thread
identities.

The lost-ack path asserts a partial canonical receipt and refusal to record an
uncertain recruiting outcome. Restart and Execute cannot resend. The private
recovery endpoint refuses an unavailable mailbox without altering delivery.
A trusted fixture evidence reader then returns the accepted raw message for the
exact frozen sender and Message-ID. Full envelope comparison and canonical
reconciliation run normally; repeated recovery reads evidence once and never
sends. The confirmed outcome retains the original candidate/source revision and
reviewed message despite a newer saved draft.

Both variants continue through explicit idempotent recruiting outcome recording,
wrong-candidate refusal, original history preservation, reply-watch changes,
reply notice/badge creation, read-only receipt preview and notice dismissal.
Every stage keeps the fake provider send count at the two authorized attempts.

Evidence: `TestWorkbenchSourcedCandidateToCanonicalOutreach` in
`server/workbench_outreach_journey_test.go`, run with the race detector. The
fixture evidence callback is injected at the adapter boundary; this test does
not exercise Gmail search/raw REST transport or establish real-provider lost-ack
acceptance. Those reader contracts have separate fake-transport tests. This is
backend journey evidence; browser recovery/outcome coverage remains in the
existing shared-card/recruiting fixtures. No production mailbox or external send
is used. Full-suite/build/live results are in the plan checkpoint.
