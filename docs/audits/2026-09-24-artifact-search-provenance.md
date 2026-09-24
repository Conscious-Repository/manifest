# Registered artifact search and source navigation

The workbench Files inspector now offers this conversation or all registered files. The conversation view retains its task files and private attachments. Global search covers registered names, paths, IDs, kind, actor, harness, provenance and current immutable text up to 1 MiB. Search terms match case-insensitively across those fields. Historical revisions and unregistered filesystem paths are outside this search; attachments stay in their owning conversation. Binary, oversized and unreadable nonmatches are counted explicitly instead of represented as searched.

Results open the exact head revision returned by the query. A file from another conversation does not inherit the current conversation's task. Search scope, query and reading position use the existing workspace persistence. Loading, failure, no matches and unavailable source states are explicit; stale requests are aborted and ignored. No new record store or index is introduced.

Source links resolve exact recorded conversation keys. A continued coding execution can expose both the original conversation and the separate producing execution; legacy opaque IDs are not guessed from titles. Missing source records remain unlinked. Harness run links use a new explicit `artifact/run-in/<harness>/<id>` route and existing report reader, with exact harness selection and no cross-harness fallback. Legacy run URLs retain their existing behavior.

Search remains on the owner-only artifact API. Portal handlers do not expose it, and search returns metadata rather than matched text. Existing context authorization still controls which artifact revisions an agent receives; finding a result does not send or share it.

## Verification

- Go fixtures cover current-text versus historical-text search, metadata and ID matches, bounded/unsupported content, exact conversation filtering, identical titles, original-conversation versus continuation execution, missing and mismatched sources, exact harness report loading and missing-harness refusal.
- Portal denial fixture includes private artifact search with source metadata. Responses use `private, no-store`.
- Node fixture verifies new harness-specific run URLs and legacy run URLs, including rejection of a missing explicit harness.
- Headless Chromium workspace fixture verifies a content-only match, exact revision opening, source links, no inherited task, saved scope/query after workspace restoration and 390px overflow. The phone screenshot was inspected. This is browser viewport evidence, not physical-phone acceptance.
- Validation uses a clean checkout at `42a803c` plus this change because concurrent, unrelated fundraising edits temporarily prevented the shared checkout from compiling. Unrelated edits are preserved.

This implements discovery and provenance for already registered artifacts. Automatic registration of every adapter output, all record-kind references/typeahead, capability context, unsupported previews and the broader acceptance journeys remain active work in the canonical plan.

Server/artifact package tests, focused source/privacy fixtures, Node route checks and the Chromium workspace fixture passed. The clean-checkout build passed. Both `go test ./...` and `make test` retained only the documented `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` source-hash re-audit failure for unchanged `hermes/authority.go` and `hermes/claude_successor.go`; all other packages passed.
