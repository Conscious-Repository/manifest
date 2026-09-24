# Reviewed prior-output handoff to private related chats

Side-chat setup captures the selected artifact when opened and displays its exact revision with an inclusion checkbox. An unlinked prior output requires a fresh checkbox choice; merely selecting it in the parent does not grant the child context. The captured reference and choice persist with setup state. Once creation is uncertain, the checkbox and target settings lock, and retries use the same payload/request identity even if the parent's selection changes. The related-chat dialog exposes the same inclusion choice.

Private related creation can record explicit artifact consent in the canonical Origin alongside exact references. This consent participates in the existing creation signature. Creation validates the selected bytes but does not execute work. Later child messages may use only those persisted handoff references through the existing scoped resolver; selecting a newer version does not inherit access. Planning, Codex and Claude child creation retain source identity and do not borrow an artifact's producing task.

Explicit handoffs require a separate private child and selected references. Shared/portal sources reject this path. Terminal-to-planning handoff now holds the source sharing gate through creation, matching the coding path. Sharing and unreviewed publication cannot race past the consent check.

Evidence: server fixtures cover planning/Codex/Claude draft creation, denial without opt-in, exact retained reference, no run at creation, denial for a newer artifact revision, same-request recovery and changed-consent conflict. A planning child successfully sends its handed-over version without a blanket private-context grant. Shared-terminal sources reject both child target types before runtime. Chromium verifies unchecked explicit handoff, source-selection changes after setup, exact frozen payload, lost reply/reopening and identical retry. Phone screenshot inspected; 320/390/1440px bounds passed.

This completes this explicit prior-output handoff journey with fixture-backed providers. Broader record typeahead, automatic output coverage, generic authorized team-file editing and integrated live-provider/physical-device acceptance remain open. No real provider messages or external actions were submitted.

Build, syntax/diff checks, browser workspace fixture and focused sharing/related/explicit tests passed. `make test` passed server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files.
