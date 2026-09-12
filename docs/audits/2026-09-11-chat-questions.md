# Native chat questions — 2026-09-11

The reported Codex session contained two `request_user_input_async` calls with
three questions. Manifest projected these only as clipped tool Activity, although
the native terminal showed three queued questions. The new panel projects their
full prompts/options and allows a custom answer above the chat composer.

Replies bind original question item IDs and canonical question text and are sent
as the native `send_user_message_question_reply` message envelope through the
existing herdr input path. This does not introduce an app-server connection or
manipulate the CLI's own queue UI. Receipt-backed sent/unconfirmed states survive
reload; question replies observed in the transcript mark the corresponding item
answered. Raw reply bytes are preserved for existing receipt matching, with a
readable presentation in chat.

Validation rejects unknown/already-submitted/duplicate question IDs, empty or
oversized answers, mixed commands/context, unsupported backends and shared input.
Receipt lookup precedes pending-state validation for an idempotent retry. An
ambiguous send is never replayed automatically. All validation and submission use
the existing session lock, identity checks and same-origin/share gates.

Scope: native Codex asynchronous question tools, including already-recorded
questions. Synchronous questions can be displayed but are explicitly answered in
Terminal; their native tool-result protocol is not replaced by a normal message.
This is not a new question API for Hermes or a change to execution approvals.

Verification: Go projection/validation/delivery tests; Node interaction tests for
free text, choices, stable DOM across polls, navigation binding, explicit submit,
refusal and uncertain delivery. The fixture runtime checks receipt-before-send
and prevents repeat effects. `go test ./...` and `go build ./...` pass. Browser
fixture checks use a fake sender; no answers to the owner's real questions were
submitted during testing.
