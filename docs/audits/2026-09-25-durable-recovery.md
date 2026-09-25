# Durable recovery — 2026-09-25

Workstream row: "Durable recovery" in
`system/workbench/plans/2026-09-11-manifest-agent-workbench.md`. Builds on the
reliable-supervision projection (`6e7a399`); nothing from that change is redone.

## Audit scope

Three maps were taken before any change: the agentchat delivery journal and
restart repair, the side-chat return path end to end, and every piece of
workbench UI state (draft, recipient, task/context selection, workspace tabs
and per-tab view, artifact editor drafts, reading position, layout,
selection). The defects below are the ones that could lose a draft, hide an
exact target, or let a retry repeat a consequential action. The rest of the
audit is recorded under "Audited and left as is".

## Defects fixed

| # | Defect | Fix | Proof |
| --- | --- | --- | --- |
| 1 | An `unconfirmed` herdr input receipt (process died between writing the receipt and the daemon's reply) projected `disconnected` for ever, even after the provider recorded the prompt and answered it. The owner's only way out was to resubmit, repeating the instruction. | `receiptConfirmedByTranscript` (`server/chat_recovery.go`): the exact submitted bytes (`SubmittedHash`, the same rule `projectContinuationTurns` uses) recorded as a provider user turn prove the prompt landed; the ordinary sent-receipt rules then apply. Similar text does not count. The receipt file is not rewritten; the transcript is re-read on every projection. | `TestTerminalUnconfirmedReceiptRecoversFromProviderRecord`: fresh `Server` over the same registry/receipts; no record → disconnected; other bytes → disconnected; exact bytes + working → running; exact bytes + later assistant turn → ready_for_review naming both turns; retry of the same request returns the receipt (202) and sends nothing; changed content → 409. |
| 2 | While this process was inside `handleTermInput` (waiting up to 10 s for the prompt, then for the daemon call), a claimed outbox entry had no receipt and an unconfirmed receipt had no reply, so both projected `disconnected` and lit the Disconnected attention filter for a send that was in progress. | `termCfg.inflight` (volatile by design: a restart empties it). Claimed entry in flight → `submitted` "dispatch in progress in this process"; unconfirmed receipt in flight → `running` "send in progress in this process". An outbox entry that already has a receipt is no longer a second run. | `TestTerminalDispatchInProgressIsNotDisconnected`: herdr fixture gated on `pane.read` and `agent.prompt`; projection read at each gate; one run per request; after the send the receipt is `sent`; a fresh `Server` projects a leftover unconfirmed receipt as disconnected again. |
| 3 | Hermes: `Claim` wrote the running receipt, then the goroutine registered the invocation. A read in between projected the turn this process owned as disconnected. | `claimAgentChatDelivery` claims and registers under `runMu` in one critical section; the loop uses it for follow-on claims. | `TestHermesClaimIsLiveBeforeSendReturns`: synchronously after the send request returns, the receipt is running with a live invocation and the projection is `running`; one call in `calls.log`. |
| 4 | tmux-legacy sends answered 502 on failure. The browser retries 502/503 automatically (a proxy 502 means Manifest was never reached), so a send that failed after part of the text reached the pane was typed again. | Legacy `sendText`/`sendKey` failure answers 500 "send outcome uncertain … no automatic retry". Relaunch failure (nothing typed) keeps 502. | `TestLegacyTerminalSendFailureIsNotAutoRetried`: recorder fails the Enter step; 500 with the wording; text reached the pane once. |
| 5 | Non-durable send paths (spirit sessions, portal posts, native `/commands`, task threads) cleared the sent draft through the 600 ms typing debounce. A second device reading the draft in that window adopted the sent text and could resend it. `flush()` during an in-flight write returned that write's result, so the newest value waited for the debounce as well. | `clearSent` flushes immediately; `flush()` queues behind an in-flight write and sends the newest value. | `testdata/chat-state.cjs` (timer never fires in the harness): cleared draft persisted with no explicit flush; a second device reads it empty; queued flush lands the newest value. |
| 6 | Side-chat return receipts lived only in the parent draft; the child frame forgot them on reload, restore or another device and offered "Add to parent draft" again (the parent de-duplicated, so no duplicate text, but the receipt was invisible). | Child asks the parent once per route (`manifest-side-receipts-query`); the parent answers from the server-confirmed draft (`base.sideReturns`), never from unsent local state. Buttons carry their return identity and relabel to "Added to parent draft". | `testdata/chat-workspace-tabs.cjs`: after closing the workspace and restoring it from saved state, the child shows the receipt without a click; an unreturned response keeps its action; clicking the receipt again changes nothing. |
| 7 | The child limit (32 000 characters) and the server limit (96 000 bytes for the whole draft value) were measured differently. A long multibyte response, or several returns, produced a 400 that the client reported as "sync unavailable"; every retry failed and the draft could no longer sync. | `addSideFinding` measures the candidate value in bytes and refuses before appending, with a reason (`sideReturnError`). The parent forwards a definite refusal as `manifest-side-finding-nack`; the child shows "Not added · retry" with the reason. A 400/413 on any draft write now says the draft exceeds the size limit. | `chat-state.cjs`: oversized return refused, remote revision unchanged, a fitting return still lands; 400 reported as a size refusal with the text kept. `chat-workspace-tabs.cjs`: nack path renders the reason. |
| 8 | A local draft seeded before the synced state existed was written without its recipient; a first save could drop the chosen recipient or surface as a conflict. | `chatPrepareDraft` includes `recipient` in the seed. | Code path; covered by existing conflict fixtures' recipient assertions. |

## Audited and left as is (with why)

- **Hermes restart repair** (`RecoverDeliveries`, `ResumeAgentChats`): a started
  call is never replayed; queued instructions start exactly once. Unchanged.
- **Workspace tabs and reading position**: conflicts adopt the server copy
  without re-rendering, so the local pane's next save wins. These are
  navigation state, not consequential actions; a re-render mid-use would yank
  the owner's tabs. Left as last-writer-wins, documented here.
- **Recipient/model changes across devices** reach another device on focus,
  visibility or conversation open, and as a draft conflict when that device
  has unsent edits. No live push exists and none was added (no new poller).
- **`hermesTurnSweep` re-dispatch** (task-thread Ask/Do turns, up to 3 times)
  is the one deliberate replay in the codebase, shipped as `4fbea1c`. It
  contradicts the never-replay rule used everywhere else and is flagged for an
  owner decision rather than changed here.
- **Task-thread posts** (`POST /api/tasks/thread`) carry no request ID. A lost
  acknowledgement followed by an explicit owner retry records a second comment
  and a second dispatch. Adding request identity to the threads store is a
  separate bounded item.
- **Draft values over 15 000 bytes** are not sent with `keepalive` at page
  close; local storage recovers them on the same device.
- **Landing spirit/model, portal ask/propose, layout, sidebar and pane
  sizes** are device-local by design.

## Verification

- `gofmt -l`, `go build ./...`, `go vet ./...`, `git diff --check`,
  `node --check` on the three changed JS files: clean.
- `go test ./server ./agentchat ./chatstate -race` on the supervision,
  terminal, delivery, queue, continuation, side, question and interrupt
  suites: pass.
- `node testdata/chat-state.cjs` (from `server/`): pass.
- `NODE_PATH=/tmp/manifest-browser/node_modules node server/testdata/chat-workspace-tabs.cjs`
  (real headless Chromium, mocked draft API): pass.
- Full `go test ./...` with Playwright on PATH: recorded in the plan
  checkpoint. `cmd/re-intake-canary` is green (pins refreshed with rationale in
  `6e7a399`; not touched here). `server TestChatMobileChromeUI` is red at HEAD
  before this change (861 px composer geometry) and unrelated.

## Limits

- Adapter journeys are fixture-backed (hermes stub runner, herdr fixture
  daemon). No live provider, no production session, no physical device.
- Defect 1 needs the provider transcript to carry the user turn; a herdr
  session whose rollout is undiscoverable stays `disconnected`.
- Defect 6 shows receipts from the parent's confirmed draft; a return
  persisted from a second device becomes visible after the parent refreshes
  (focus, visibility, open), the same cadence as any other draft state.
- Nothing here changes what is sent: every fix is a projection, a persistence
  ordering, or a refusal.
