# Sourced candidate to canonical email approval — September 24

`TestWorkbenchSourcedCandidateToCanonicalOutreach` connects previously separate
backend checks using real stores/handlers and a local fake Gmail endpoint:

1. Accept a source-backed candidate into the capability-scoped private recruiting
   store, preserving the source URL and evidence quote.
2. Record an explicit recipient and evidence-backed review scores for the role's
   criteria; verify the recruiting gate passes.
3. Save a reviewed outreach draft through the recruiting handler and verify its
   readiness through the existing preflight handler.
4. Explicitly carry that draft's exact recipients, subject and body into canonical
   `/api/email/prepare`, with a real private conversation ID, turn and stable
   idempotency key. Verify no mail is sent, preparation retries reuse the same
   operation, and one proposal appears in Chat and Feed.
5. Save a newer recruiting draft. Reusing the approved preparation identity with
   changed contents is refused; the pending email retains the earlier payload.
6. Submit two owner confirmations concurrently. Exactly one fake message is sent
   from `ben@aion.bio` to the reviewed recipient with the reviewed body. Chat has
   the successful provider message/thread receipt and Feed has no pending copy.
7. Reopen the operation/approval stores and retry execution. The same receipt
   remains and the fake sender is not invoked again. Original recruiting draft
   history is preserved.

The focused journey passes under Go's race detector. This is a backend acceptance
path with an explicit transfer of reviewed draft fields, not proof of an integrated
recruiting-to-chat UI action. Canonical email outcomes are still separate from the
legacy recruiting outreach log; automatic reconciliation of those records remains
an implementation gap. No assertion requires that gap to remain. Lost provider
acknowledgment reconciliation, proactive reply notices and real sender/read
readiness also remain open. No real mail or production record was touched.

The full plan's sourcing-to-approved-outreach checkbox remains unchecked until the
user-facing journey and outcome reconciliation are established. This test supplies
one concrete part of that acceptance evidence rather than redefining the journey.

`make test` passed server (41.362s) and other packages except the known unchanged
Hermes source-hash canary re-audit failure. The final expanded gate/readiness
journey then passed its focused race run. This increment changes only tests and
audit evidence; it requires no runtime deployment or production restart.
