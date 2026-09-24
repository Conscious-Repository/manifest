# Native question-answer recovery

Native async question cards previously kept answers and submission IDs only in a browser Map. Reloading lost unfinished text and the request identity needed to reconcile an uncertain send.

Each question now uses the existing private ChatDraftState and chatstate store under a question-specific key. Session, native question identity and revision keep answers separate from other questions and the normal composer. Text, exact question revision, request ID and submission lock persist together before any runtime POST. Local recovery supports offline typing; revision checks expose cross-device conflicts through the existing draft controls. Persistence never submits an answer by itself.

Opening a saved submission checks its original terminal receipt. Sent receipts stay locked, unconfirmed or unreachable receipts remain uncertain, and a missing receipt offers an explicit retry of the same frozen request. No automatic retry crosses the runtime boundary. Receipt identity, answer text, question identity and revision must match. A definitive HTTP refusal plus absent receipt permits correction. Existing server receipts prevent replay after restart; stale native questions are still rejected under the input lock.

Native question projections also carry a content revision covering ID, wording, choices and question type. Current cards send that revision; if the prompt changes under the same ID, the server rejects the stale answer before invoking the runtime. Older clients without a revision retain their existing contract.

## Evidence

- Node UI/controller tests exercise stable focused DOM, options/free text without automatic selection, persistence before submission, captured session targeting, lost HTTP response, uncertain receipt after reload, local/second-device recovery, offline writes, lost draft-save acknowledgment, frozen explicit retry, concurrent drafts, mismatched receipts, independent question retirement, and changed-revision isolation.
- Store tests reopen a question draft after restart, reject stale writes and unsupported slots, and confirm the normal composer stays separate. Portal denial covers question draft keys.
- Native runtime fixtures verify exact duplicate answers recover from durable receipts after restarting without a daemon, both sent and uncertain. Changed question wording under the same ID rejects the old revision without a runtime call.
- Real headless Chromium fixture at 390px verifies typing, focused input across polling, reload, visible cross-device conflict resolution, a dropped submission response, and receipt reconciliation after another reload without a second send. The screenshot was inspected; there was no horizontal overflow or page error.

Scope: current private Codex async question cards through the herdr input adapter. Synchronous/native-terminal prompts and shared questions retain their Terminal path. No real provider answers were sent during fixtures. This is not physical-phone acceptance or certification of all adapter question/control semantics. The larger workbench plan remains active.

Focused Go question/state/privacy tests and all 11 Node recovery tests passed. The Chromium fixture and build passed. `go test ./...` and `make test` retained only the existing canary source-hash re-audit failure in unchanged `hermes/authority.go` and `hermes/claude_successor.go`; other packages passed. Final validation/build used a clean checkout at `1d27df4` plus this change, preserving the concurrently delivered fundraising fixes and unrelated local files.
