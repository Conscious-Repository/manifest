# Native delivery result identity

Native Hermes chat now retains the runner-reported model and Hermes session ID on each delivery, atomically with its reply and terminal receipt. Previously only the latest Hermes session survived on the conversation; reported model appeared in the ledger but was not retained on the exact receipt. The compatibility conversation pointer still updates when a session ID is reported.

Accepted recipient/model/context and their retry fingerprint are unchanged. Requested model is never substituted for missing reported telemetry. New runner returns carry a result object even when telemetry is unavailable; preflight/older receipts retain absent results. Neither absence nor a reported model proves provider completion: the delivery state remains separate, including failure and owner interruption. Duplicate completion leaves the first result and transcript unchanged. This does not backfill old receipts, provide a native-session browser/resume action, or normalize other adapters.

The private Context inspector displays the selected instruction's delivery ID, state, reported model and Hermes session. Missing values say Not reported; absent results have an explicit explanation. Values render as literal text. Sharing keeps its existing explicit publication projection; delivery metadata is not newly added to team messages.

Evidence:
- `agentchat/result_test.go`: two distinct results, restart, stale completion, unchanged recipient/fingerprint, no duplicate turns, missing telemetry for completed/failed/interrupted states and legacy/preflight absence.
- `server/agentchat_result_test.go`: actual fake CLI usage-file ingestion for success, reported failure and missing telemetry; requested versus reported model and durable receipt readback.
- Focused race tests pass with existing native interruption and tool-scope cases.
- `server/testdata/chat-workspace-tabs.cjs`: selected result, legacy absence, instruction switching without result leakage, pane restoration, full-length identity bounds at 390px. Phone screenshot inspected. Existing workspace/draft/side-chat cases pass. An exploratory rapid 320→390→1440→390 resize sequence timed out at the existing strict active-tab-boundary assertion; the stable 390px journey passes. That broader resize behavior needs separate measurement and is not certified here.
- Full `make test`: server passed (41.825s); only existing source-hash canary failures for unchanged `hermes/authority.go` and `hermes/claude_successor.go`. Build, syntax and diff checks pass.

No real provider call or production instruction is used by these fixtures. Physical-device and all-adapter acceptance remain open.
