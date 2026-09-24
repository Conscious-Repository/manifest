# Explicit prior-output context in private chats

All-files browsing now offers “use in this private chat” for supported private conversation targets and text previews. Browsing alone still adds no context, and request-changes reviews from browse tabs still do not silently draft a cross-conversation message. The explicit action selects the displayed immutable artifact version for the next owner message, shows its composer chip, and persists the selection in the existing draft store. It never adopts the producing task or changes artifact/task relationships.

Private planning and native input requests carry an explicitArtifacts flag. That opt-in permits the owner-selected registered versions to pass through the existing exact-content resolver even when not linked to the target task. Without it, established conversation/task scope rules remain. The resolver still rejects unavailable versions, unsupported binary extraction, more than eight references, oversized individual files and excessive combined context.

The opt-in is part of the durable instruction fingerprint and recorded receipt. Reusing a request ID with different selection authority conflicts. Planning delivery reads the retained exact references; native delivery records the selected references and submitted-context hash before crossing the runtime boundary. A retry cannot silently select the latest artifact head or re-execute an acknowledged native input.

Shared/native portal input, shared private-source sessions, native commands, key input and question-answer input cannot use this opt-in. Existing share publication still requires its reviewed-history consent path. There is no ambient access grant or separate grant store: the selection authorizes only the identified private message. Explicit selections are not automatically inherited by side chats.

## Evidence

- Fake Hermes planning journey rejects an unlinked artifact without opt-in, delivers the exact old bytes with opt-in, leaves the conversation standalone, records consent and references, recovers a retry after the artifact head advances and rejects changing consent under that request ID.
- Fake herdr native journey delivers exact prior output, records authority/references without a foreign task, recovers through a restarted server without a second prompt and rejects altered consent.
- Shared-input regression rejects the flag before runtime delivery.
- Chromium workspace journey searches a foreign-task output, opens its exact revision, restores browsing state, explicitly selects it, displays the composer chip and reconstructs its exact reference/opt-in from the real draft controller's saved state. No real provider prompts were sent.
- Build, JS syntax and diff checks passed. `make test` passed agentchat/server and other packages except the known Hermes authority/successor source-hash canary failure.

This advances prior-output context for private planning and native message paths; it does not certify full general-record typeahead, all shared/context paths, automatic output registration, integrated live-provider journeys or physical-device acceptance. The full workbench plan remains active.
