# Reliable supervision across adapters — 2026-09-25

Workstream row: "Reliable supervision across adapters" in
`system/workbench/plans/2026-09-11-manifest-agent-workbench.md`.

## What shipped

One normalised run/state vocabulary, projected read-only over the records that
already exist (no new store, no new event log, no new poll):

| State | Meaning | Proof named in `evidence` |
| --- | --- | --- |
| `submitted` | accepted durably, not started | queued delivery receipt / waiting outbox entry |
| `running` | provider call in flight, owned by this process | running receipt + live invocation; or sent receipt + observation `working` |
| `disconnected` | call outlived its client/process; outcome uncertain, never replayed | receipt marked `disconnected` by restart repair; running receipt with no live invocation; unconfirmed input receipt; sent receipt with process gone and no provider record |
| `ready_for_review` | provider returned and the reply landed (not owner acceptance) | reply heading `## Turn N — <agent>` present; provider run record or assistant turn after submission |
| `failed` / `interrupted` / `cancelled` | terminal negatives | receipt state + recorded reason |
| `unknown` | nothing proves any of the above | e.g. completed receipt whose reply turn is missing; idle process with no provider record |

Run identity: `RunID = <adapter>:<conversation key>#<request ID>` — the same
request ID the delivery journal, input receipts, outbox and ledger
(`Meta.runId`) already carry.

Capabilities are data per adapter (`server/chat_capabilities.go`):
`hermes-oneshot`, `herdr-codex`, `herdr-claude`, `herdr-shell`, `tmux-legacy`,
`remote-keep`. Every adapter reports `retry: explicit-resubmit`; none reports a
skill inventory. Unsupported actions are refused in words (409), never
silently ignored: tmux-legacy queue/steer, a stale question answer, an
interrupt for a turn that is not running.

## Surfaces

- `GET /api/agents/chat/{agent}/sessions/{id}` → `supervision`, `capabilities`.
- `GET /api/agents/chat/{agent}/sessions` → each row carries `supervision`
  (body-less projection: the reply turn must be within the turn count).
- `GET /api/terminal/session/{id}/transcript` → `supervision`, `capabilities`;
  `questions[].state` gains `stale` when the asking process is gone.
- `GET /api/terminal/sessions` → each row carries `supervision`, `capabilities`.
- `POST /api/terminal/session/{id}/input` refuses `steer`/`afterRun` on a
  non-herdr runtime (409) and refuses a question answer whose run is not live
  (409, before launch resolution, so no resume/relaunch happens).
- Ledger: `run.disconnected` on startup repair (flushed once the ledger is
  wired; the session file is the record either way); `runId` on
  `chat.user`, `chat.assistant`, `run.failed`, `run.interrupted`.
- Session frontmatter: additive `disconnected: true` on a delivery the restart
  repair found running. Turn grammar unchanged.
- UI: rail rows label from the projection (`Disconnected · outcome uncertain`,
  `Finished · ready for review`, evidence in the tooltip); attention filter
  gains "Disconnected"; native interrupt/cancel controls are gated on
  capabilities and a disconnected receipt shows a not-replayed notice; stale
  question cards say the run is gone instead of offering an answer form;
  Context › Adapter capabilities renders the full matrix for any adapter.

## Tests (all in `server/chat_supervision_test.go` unless noted)

- `TestSupervisionRestartDisconnectsWithoutReplay` — hanging fake `hermes`,
  two accepted instructions, a second `Server` over the same files: first run
  `disconnected` (receipt `interrupted` + `disconnected`), second `submitted`;
  `ResumeAgentChats` runs only the second (calls.log: one line each);
  `run.disconnected` ledger event carries the `runId`; the old goroutine's
  late result is refused; rail rows carry the same projection.
- `TestSupervisionCompletedRequiresReplyTurn` — a completed receipt whose reply
  turn is cut from the body projects `unknown`, not `ready_for_review`.
- `TestSupervisionRunningWithoutInvocationIsDisconnected` — running receipt,
  no live invocation → `disconnected`; on a box whose runner is off → `unknown`;
  the record is not rewritten.
- `TestSupervisionConcurrentThreadsInterleave` — two threads gated
  independently; finishing B leaves A `running`; distinct `runId`s in the ledger.
- `TestTerminalStaleQuestionRefused` — Codex async question, pane gone:
  projection `stale`, answer → 409 "stale … nothing sent", no receipt, no
  prompt, no relaunch.
- `TestTerminalSupervisionProjectionAndRefusals` — tmux-legacy steer/afterRun
  → 409 naming the adapter; herdr: draft `unknown`, sent+working `running`,
  idle without provider record `unknown`, pane gone `disconnected`,
  unconfirmed receipt `disconnected`, assistant turn after submission
  `ready_for_review`, outbox entry `submitted`.
- `TestChatAdapterCapabilityMatrix` — matrix consistency; interrupt of a
  finished turn is a 409, not a no-op.
- `TestNativeConversationCapabilities` (`chat_capabilities_test.go`) — retained.
- Browser fixtures `testdata/chat-stage-switch-browser.cjs` and
  `testdata/chat-workspace-tabs.cjs` pass with the full capability shape
  (Playwright from `/tmp/manifest-browser`).

## Journeys (fake adapter, named evidence)

- Supported journey: `hermes-oneshot`, stub runner reporting
  `model: stub-model`, `session_id: 20260925_gate-second`; evidence string
  `completed receipt request-second: reply heading “Turn 4 — alfred · …”
  present · runner session 20260925_gate-second · reported model stub-model`.
- Interrupted/restarted run: same test, `request-first` → `disconnected`,
  never replayed (one invocation in calls.log after the drain).

No live provider was invoked; no production session was touched.

## Canary

`cmd/re-intake-canary TestCanarySourceCallGraphIsolation` was red on
`hermes/authority.go` and `hermes/claude_successor.go`. Re-audited against
commit `e283514`: the only change is the literal `120` → named constant
`ExtractionTimeoutCap = 420` in Validate's local-binding bound,
`extractionDutyAllowed` and the extraction defaults. The canary duty still
validates (120 ≤ 420) and is still excluded from the extraction branch by exact
duty name; no call, import, provider, model, cost or tool authority changed.
Pins refreshed with that rationale in the test; the suite is green.

## Out of scope / remaining

- Terminal `ready_for_review` relies on provider lifecycle records or an
  assistant turn timestamp after the submission; a Claude transcript without
  timestamps stays `unknown`.
- No live herdr/Hermes journey was run; adapter evidence is fixture-backed.
- `interrupted` (owner stop) and `disconnected` are distinct only for
  deliveries repaired after this change; older interrupted receipts without the
  flag project as `interrupted`.
- Other workstreams (relationships, durable recovery, artifact review,
  approvals/mail, responsive) untouched.
