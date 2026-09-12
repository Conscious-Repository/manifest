# Coding chat steering and pending messages

A live read of the reported Codex pane found `agent_status: blocked` and three unanswered questions. The installed herdr protocol 22 rejects `agent.prompt` in that state before writing input. Manifest had persisted an unconfirmed receipt first, then treated this definite rejection as uncertain forever.

The readiness gate now reports interactive input requirements before creating a receipt. A race into herdr's documented pre-write rejection removes the provisional receipt and returns a definite “nothing sent” response; transport failures still remain uncertain and are never replayed automatically. Existing agent.prompt submits with Enter and supports a working agent.

Messages submitted to a working or blocked native coding session are saved as pending follow-ups above the composer. Explicit Steer sends now. Remove discards only the unsent follow-up. The ellipsis menu edits the saved text or opens side-chat setup with that text and the current conversation context. Pending messages do not auto-dispatch on idle; the row says “choose Steer to send.” Server-backed recovery state persists them across reloads. A CAS claim prevents another device editing or removing a message after dispatch starts. Uncertain sends cannot be restaged as editable messages.

Validation: terminal/runtime and shared-input Go regression tests, including working dispatch, blocked preflight and rejection races; browser tests for edit, side-chat context, remove, explicit steering, blocked recovery, mobile bounds and uncertain-send protection. No production agent instructions were sent for testing. Interactive questions/approvals must still be answered in chat or Terminal before a blocked adapter accepts steering.
