# Email approvals

General email uses the existing operation/approval records. An agent calls
`email.prepare` through Manifest MCP. The owner can also prepare through
`POST /api/email/prepare`. Neither route sends mail. The resulting review appears
in the originating Manifest chat and Feed; the same approval identity settles both.

Use `domain: "aion"` or `domain: "ooda"`, recipients, subject, body and a stable
`idempotencyKey`. Include the conversation and turn when calling HTTP; MCP binds
these from the runner when available. For example, request an OODA bid draft in
chat, review the displayed From/To/body/files, then approve. Edited content needs
a new preparation and approval. A draft can be prepared before connecting its sender.

Mappings are explicit:

- AION/recruiting/aion.bio: ben@aion.bio.
- OODA/real-estate/ooda.group: ben@ooda.group.
- Personal: unavailable until Outlook sending is implemented and connected.
- Unknown domains and mixed/unknown recipient domains without a work domain fail.
  External contractors do not imply a sender; supply their correspondence domain.

Settings → Connections has separate AION and OODA send connections. The AION
client retains `GMAIL_SEND_TOKEN` or `<dataDir>/gmail-send/token.json`; OODA uses
`<dataDir>/gmail-send/ooda/token.json`. Read-only mailbox tokens are not reused.
`GMAIL_SEND_FROM` no longer chooses a global account. Each Gmail request also names
its exact mailbox in the API URL, as supported by Google's
[users.messages.send API](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/send).
The mapping cannot authorize access to a mailbox belonging to another OAuth account.

Attachments are `{hash, name}` references to existing domain-owned artifact uploads.
Preparation verifies hashes and freezes bytes into an immutable local envelope
(maximum 20 MiB total). Approval uses that envelope's digest, not current file paths.
RFC threading headers can be supplied as `inReplyTo` and `references`; successful
receipts retain Gmail message and thread IDs. No ongoing monitor is started by this
feature and a Gmail thread ID alone is not a monitoring subscription.

Delivery is recorded before the network boundary. A lost acknowledgement or crash
recovers the existing receipt, never automatically replays the send. `partial`
with `deliveryStatus: uncertain` means inspect the mapped account's Sent folder;
it is not confirmed delivery and must not be treated as permission to resend.
A missing sender connection fails before sending and is shown explicitly.

Validation uses local fake Gmail transports. No real mail is sent by tests.
