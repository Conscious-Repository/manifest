# Side-chat return recovery — September 24

The previous Add to parent draft action generated a new random identity each time a response rendered, and the parent remembered acceptance only in a mounted frame's Set. Reopening either frame could append the same finding again. The parent also acknowledged before draft persistence succeeded.

Return identity now hashes the source route, recorded turn ID and exact response text. The parent validates the frame, origin and source route, and binds the operation to its original conversation. Responses without recorded turn identity retain Copy but do not offer a recoverable return action.

The existing owner-only ChatDraftState stores the appended text and a return receipt in one revision-checked snapshot. Acknowledgment follows server persistence. Receipts survive ordinary typing, attachment changes, Send clearing, reload and explicit conflict resolution that keeps the local draft. Choosing a saved draft discards uncommitted local changes as before. Returning a changed response or another turn is a distinct explicit action. Returning previously accepted content again is a no-op even after the owner removes or sends it; Copy remains available for deliberate reuse.

No message is sent by returning a finding. Existing draft stores, conflict controls, private routes and recovery semantics remain authoritative. Offline or conflicted state does not acknowledge success; the child offers retry and directs the owner to the parent draft's sync status. Existing state size limits apply; receipts are not silently evicted.

## Evidence

- `node testdata/chat-state.cjs` from `server`: lost server acknowledgment, device reload, second-device retry, preserved attachments and recipient, Send followed by fresh typing, distinct turns with identical text, navigation cancellation, offline failure, concurrent draft conflict and retained committed receipts.
- `NODE_PATH=/tmp/manifest-browser/node_modules node server/testdata/chat-workspace-tabs.cjs`: real headless Chromium, mocked draft API; exact response action, frame disposal/restoration and no duplicate append, alongside existing editor, workspace, desktop/mobile and context fixtures. The fixture accepts `PLAYWRIGHT_CHROMIUM_CHANNEL` when a specific installed browser is desired.
- `go test ./chatstate ./server` passed; `go build -o /tmp/manifest-workbench-build .` passed.
- `make test` and `go test ./...` retain the previously documented `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` failure requiring re-audit of `hermes/authority.go` and `hermes/claude_successor.go`. Those files are unchanged. Other packages passed.

This verifies a bounded draft-return journey. It does not certify all adapter recovery, physical-phone behavior, generic cross-device workspace recovery or the full workbench acceptance checklist. Browser integration uses fixture storage, not real provider sends.
