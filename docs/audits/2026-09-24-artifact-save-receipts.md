# Durable registered-text save receipts

Conditional artifact writes may now carry a request identity. The registry fingerprints the normalized request, excluding its timestamp, and commits the receipt in the same atomic object replacement as the new revision. A matching retry returns the original version without moving the current head or emitting a second revision event. Altered content, base, actor or metadata under the same identity conflicts. Unchanged-content saves record a receipt without inventing a revision. Existing callers without request identities retain their compare-and-swap contract.

The owner text endpoint returns the saved revision, saved version number and request identity separately from the current artifact head. Its text eligibility check uses the original starting bytes. Task-plan saves keep their canonical writer and existing recovery behavior.

The artifact editor persists its save identity with the exact draft before POST. Unchanged retries and reopening retain it; changing the text starts a new request. Acknowledgments must identify the submitted request, artifact and a known saved version before the draft is cleared. Recovery opens that saved version and explicitly reports when newer work exists. No retry overwrites newer work. Execution and working-tree files are unchanged.

Receipts stay in the existing artifact record; no second receipt store or scheduler is introduced. At 4,096 receipts per artifact, new identified saves fail explicitly rather than dropping recovery history. Legacy artifacts need no migration; missing receipts deserialize as empty.

## Evidence

- Registry tests: eight concurrent retries, one revision, disk reopen, newer head retained, altered payload/base/actor/note rejection, stale fresh identity rejection, and no-op receipt replay after further edits.
- Owner endpoint test: exact receipt response, replay after a newer revision, unchanged history/head, and changed-content conflict.
- Real Chromium: persisted identity before POST, lost acknowledgment, draft-controller reconstruction, identical retry identity/base/text, exact recovered version opened, newer head retained and draft cleared only after acknowledgment.
- Existing draft persistence/recovery and workspace fixtures passed. No real owner file edits or provider messages were submitted.

This covers registered text artifact saves. Canonical task-plan save reconciliation, full cross-adapter recovery, integrated provider journeys and physical-device acceptance remain open in the workbench plan.

Focused tests, race-enabled concurrent receipt test, build, JS syntax and diff checks passed. `make test` passed artifacts, server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files.
