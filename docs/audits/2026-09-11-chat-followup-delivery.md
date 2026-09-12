# Follow-up delivery while Codex questions are queued

The live Codex screen exposed an empty composer and queued async questions,
while herdr reported the agent as blocked. Herdr 0.9.0 explicitly rejects
`agent.prompt` in that state with `agent_blocked`, before sending input.
Manifest persisted an unconfirmed receipt and discarded the specific error.
This affected ordinary follow-ups as well as question answers.

The adapter now handles only that definitive rejection, verifies the exact
Codex occupant and queued-question/empty-composer screen, and pastes via
`pane.send_input`. After herdr's 300 ms paste-settle interval it rechecks the
occupant and submits Enter. Unknown outcomes never trigger this fallback.
Dialogs, existing composer text, other agents, and unrelated blocked states
remain excluded. Receipts are still persisted before any input and prevent
replay after partial submission, cancellation, or lost acknowledgements.

Delivery receipts now retain runtime errors for diagnosis and the composer
shows them. Unconfirmed question answers remain expanded and labeled as
unconfirmed, with an attention indicator in the question section.

Existing uncertain receipts are preserved, not reclassified from absence in
the transcript and not automatically replayed. Dismissing a saved send notice
allows an intentional new submission; retrying the same request only returns
its original receipt. Native question queue controls are still separate from
the ordinary text reply bridge.

Validation: adapter regression cases cover the exact queued-question screen,
real dialog refusal, existing draft refusal, other agents, and no replay after
a timeout. UI regression checks uncertainty stays visible. Full Go suite and
build passed. An opt-in isolated live Codex probe exercises queued questions
and a subsequent follow-up without touching a user's conversation.

Protocol evidence: herdr v0.9.0 socket API reference and
`src/app/api/agents.rs`, `src/app/api_helpers.rs`, `src/app/api/panes.rs` at
https://github.com/herdrdev/herdr/tree/v0.9.0.
