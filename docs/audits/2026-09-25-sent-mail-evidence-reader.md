# Sent-mail evidence reader — September 25

Adds `gmailsync.Client.SentMessageEvidence` as a prerequisite for recovering
uncertain approved email deliveries. It is not yet wired to an operation or UI.
It performs only GET requests, against an explicit mailbox rather than `me`,
using the frozen RFC Message-ID. Search is bounded to two results; multiple
results or another page are unresolved ambiguity. No match remains uncertain,
not permission to resend. One match is fetched as raw RFC mail and must retain
the listed provider message/thread identity, SENT label and exactly one matching
Message-ID header. The returned raw bytes still require comparison against the
approved immutable envelope before any receipt can become successful.

Requests propagate cancellation; failed, oversized or malformed responses return
no evidence. Listing is capped at 64 KiB, raw decoded mail at 32 MiB and the JSON
response at the corresponding encoded bound plus 64 KiB. Fixed errors exclude
provider and transport bodies. Reads have no automatic retry; repeating a read
is safe. No mail state, approval, cursor, receipt or envelope is mutated.

Provider contract references: Gmail's [message listing API](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/list)
supports `rfc822msgid` search; the [message resource](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages)
provides base64url raw RFC mail with `format=RAW`.

Fake-transport race tests cover exact mailbox/query/method/raw bytes, empty and
ambiguous listings, pagination, mismatched identities, missing SENT label,
invalid encoding, duplicate/wrong/missing Message-ID, query injection refusal,
response bounds, cancellation including late cancellation, and redacted first
and second read failures. No real mailbox was queried for verification.
Full-suite/build/deployment results are recorded in the plan checkpoint.

Still required: comparison with approved envelope bytes, guarded durable receipt
reconciliation, the owner-facing recovery action, and integrated recovery tests.
The broader workbench plan remains active.
