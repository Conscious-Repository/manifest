# Manifest terminal and chats: audit and herdr replacement plan

Date: 2026-09-09. Audited checkout: `main`, `9549f6b` (the supplied `1cc3033` is an ancestor). This is an audit and proposed migration, not an implemented herdr integration. Repository delivery copy: `docs/audits/2026-09-09-terminal-chats-herdr.md`.

## Decision

Adopt Benjamin's shape: **chats are herdr's home; Terminal becomes a quick launcher/attachment into the live herdr server.** Replace process plumbing in stages, preserve CLI transcript readers and all board/file contracts, then remove Terminal's duplicate history UI. A herdr runtime list replaces live process inventory; it cannot replace durable conversation identity or the board's run history.

**Observability-only rule:** herdr owns processes and provides advisory state/wait signals. Its socket, snapshot, screen, and `working/blocked/done/idle/unknown` labels are never durable task truth. Only the CLI JSONL supplies chat transcript content; only validated `result.json` and the existing run-report/artifact contract establish board completion. A state event may trigger reading those files, never fabricate a result, approve an action, mark Review, or release the checkout writer gate by itself. Carry this paragraph into the runtime adapter's package/interface documentation and regression tests when implementing.

Read: `ARCHITECTURE.md`, the local manifest skill, `references/ui-owner-calls.md`, and `references/herdr-agent-multiplexer.md` under `/home/benjamin/.hermes/skills/devops/manifest/`. Benjamin's current direction supersedes the reference's older “not a replacement for the cockpit” posture. No architectural amendment or vault write was made. The repo `plans/` points into the vault; this report deliberately uses the requested `/tmp` path and a tracked audit copy under `docs/audits/`.

## 1. Current terminal ownership and cost

| Responsibility | Current owner and concrete seam | Consequence |
|---|---|---|
| Wiring | `main.go:514`, `UseTerminal` in `server/terminal.go:108`; `UseCodingRepo` immediately afterward | Terminal configuration also enables direct board coding; cannot simply remove `s.terminal`. Socket dir is `<dataDir>/tmux`, default under `~/.config/manifest`. |
| Session identity/history | `termSession` (`terminal.go:37`), `load/upsert/remove/saveLocked` | `terminals.json` holds ID, kind, device, cwd, name, resume ID/picker, Started, model, BoardBrief, timestamps, pin and remote Keep. Whole-file JSON rewrite under a mutex and temporary-file rename; writes currently swallow failures. This is operational metadata with real resume dependencies, not a transcript. |
| Inventory | `handleTermSessions` (`terminal.go:375`), `liveSet` | Reads registry, decorates registered rows with `tmux list-sessions`; it does not expose arbitrary live tmux sessions. Pinned/last-used sorting persists after processes exit. |
| Launch | `handleTermCreate`, `createAgentTermSession` (`terminal.go:473`), `termInner/termLaunchArgs/spawnTermTmux` | Browser creation first registers; attach boots. Agent creation registers and starts a detached `manifest_<id>` at 120×32. PATH/tool/cwd guards, persistent TMPDIR, terminal capabilities, mouse/clipboard, shell quoting are hand-maintained. |
| Survival and browser attachment | `tmuxSpawn`, `handleTermWS` (`terminal.go:933`) | Separate systemd user scope keeps tmux out of Manifest's service cgroup; fallback loses restart survival. WebSocket owns a PTY client, not terminal lifetime. Same-origin gate, resizing, detach cleanup and reconnect remain needed with herdr. |
| Resume | `baseLaunchCmd` (`terminal.go:343`), `execLaunch`, `boardLaunch` | Claude mints UUID then uses `--session-id` on first start and `--resume` later. Codex uses `codex resume --yolo <id>` when known, otherwise picker/new session. Board first launches are noninteractive and must never be replayed on reopen. |
| End vs forget | `handleTermKill` / `handleTermDelete` (`terminal.go:675/710`) | End kills local/kept-remote processes and retains history row; forget kills and removes row. Neither deletes CLI JSONL. Removing a row can remove the application link to a durable conversation. |
| Remote keep | `remoteKeepWrap`, `remoteInner`, `remoteKeepLive`, `killRemoteKeep` | Metis tmux hosts SSH; Keep nests another tmux on the target machine. Remote liveness has a 60-second async cache and 8-second probe timeout; delete/kill attempts both ends. A local herdr wrapping SSH alone would not preserve target-side survival. |
| Browser cockpit | `server/web/js/73-terminal.js`, `server/web/index.html` Terminal section | SESSIONS, NEW SESSION and HISTORY; search, pin, rename/OSC auto-name, default names, resume, device/cwd browsing, keep preference, xterm resize/reconnect. Visible page refreshes registry every 5 seconds. Files and Activity/stats share the stage and have separate consumers. |

Cost is concrete rather than a guessed performance percentage: `terminal.go` has 1,076 lines, `terminal_io.go` 285, `terminal_transcript.go` 542, and the Terminal JS 792 at the audit base. Not all of that is removable. Each inventory poll reads/parses the registry and invokes tmux; live chat adds transcript and capture requests. Retaining dead rows means every name/history lookup and update remains coupled to an ever-growing registry. Remote keep adds SSH probes, cache invalidation and two-ended teardown. Herdr substitutes its daemon/protocol maintenance for much of this code, not zero operational cost.

## 2. Current coding chats: data and input

`server/web/js/48-chat.js:1402` onward owns Stage S. `chatIsTerm()` selects Claude/Codex; `loadChatTermSessions` reads `/api/terminal/sessions`, and `renderChatTermLanding` plus the rail display the registry's coding rows. Creation posts `/api/terminal/session`; the composer posts `/api/terminal/session/{id}/input`. `chatTermOpenInTerminal` preserves the ID and opens the raw Terminal pane.

There are two precise qualifications to the supplied high-level description:

* Coding sessions render structured `turns` from the terminal transcript endpoint through `chatTermPaintTurns` → `chatTermPaintLines`, with `.chat-main.term`, command/output/step lines and `chatTermMerge`. `parseChatTurns(body)` is the Markdown session parser used by the other chat transports, not the direct CLI JSONL parser. Preserve both paths; do not convert coding chats into generic bubbles or engine Markdown.
* `terminal_transcript.go:460` resolves **local Claude only**, using cwd encoding plus ResumeID under `~/.claude/projects/`. `parseCodexTranscript` exists, but `transcriptPath` has no Codex branch, even when the row has a resume ID. The UI explicitly says the rollout is not wired and presents the live screen. Remote rows return no transcript path. Therefore working Codex file discovery is a separate prerequisite, not an already-shipped capability to credit to herdr.

`handleTermTranscript` (`terminal_io.go:25`) stats/reads CLI-owned JSONL, parses known messages/tool calls/results and returns `{turns,title,cost,live,offset,kind}`. The `(path,size,mtime)` cache and byte offsets avoid repeated full projection. The browser merges continuation/result records and resets on truncation. Manifest does not write the source JSONL. Preserve schema-drift tolerance and partial-line/offset semantics.

`handleTermScreen` captures the last 12 lines with tmux `capture-pane`. `handleTermInput` is local-only, validates origin/ID, checks or relaunches the session, then sends literal text plus Enter; multiline uses bracketed paste, keys use raw bytes. `termEnsureLive` serializes spawn per ID and waits up to 10 seconds with 250 ms prompt checks. It distinguishes a bare input marker from a highlighted menu option and rejects trust/confirmation dialogs. These protections are not redundant merely because herdr labels an agent idle.

Coding chats currently poll registry every 5 seconds and transcript/screen every 1.5 seconds while live and visible. Their `live` means tmux exists, not agent working. Spirit SSE in `chatOpenStream` is a separate engine event source; Alfred/profiles and portal-agent chats have their own execution/storage paths. Herdr's ability to detect Hermes does not authorize replacing the existing `hermes` runner, its proposals, per-thread writer locks or task bridge in this pass.

**Can herdr replace tmux without breaking JSONL? Yes, at the runtime boundary.** Launch the same CLI with the same home, cwd, model and conversation ID; change attach/input/read/process-state calls beneath the endpoints. Keep transcript APIs and rendering file-backed. Resolve Codex by validated CLI thread ID against its rollout metadata; never choose the newest file globally or infer identity from cwd/title alone. Keep board `events.jsonl` as a separate exec event stream; it is not automatically interchangeable with a rollout transcript. Picker-selected Claude sessions also need explicit identity capture before promising exact transcript/resume linkage.

## 3. Board, feed and ledger dependencies that must stay

`server/board_coding.go:71` (`startCodingTask`) enforces one running coding task across Claude and Codex in the configured checkout. It resolves the task/model, creates `<dataDir>/board-agents/<owner>/work/<run>/brief.md`, writes a running report, calls `createAgentTermSession`, and stores its manifest ID in `work/<run>/session`.

The initial launch (`boardLaunch`) is Claude `--print` or Codex `exec --json`, with explicit model and durable brief; Codex pipes events into `events.jsonl`. The shell atomically writes an `exit` marker. The row's Started posture is persisted before board spawn to avoid replay after a crash. `codingResume` extracts Codex `thread.started.thread_id` and stores ResumeID. Crash recovery can reconnect a work order to a row by exact BoardBrief when the `session` file is missing.

`codingResultSweep` first validates `result.json`: status completed/blocked and nonempty summary, optional artifactURL. It materializes `artifacts/library/<run>.md` and `artifacts/runs/<run>.md`. After a one-minute grace, exit marker or missing tmux without valid result produces `recovery.md` and failed report, never success. Eligible late results repair interrupted runs. With herdr, socket errors must cease being equivalent to confirmed process death: an unavailable daemon is unknown, not failed/completed. Preserve durable exit/result evidence and late-result ingestion.

`server/delegate.go:69` calls that sweep while deriving delegation state from spool/run/proposal files. `AgentLoopTicker` already runs every 60 seconds, including thread/plan ingestion, ledger sweep and portal chat sweep. `server/signals_runs.go`, unified todos and the existing Review lane consume those projections. `server/ledger_sweep.go:50` mirrors eligible finished run reports and assistant chat events, registers run artifacts with task provenance, and maintains its cursor. This ledger is not the Terminal history rail; herdr provides no substitute for it. Preserve existing eligibility/cursor behavior, task/run attribution, artifact links, approvals and receipts.

KEEP: result/brief/session/exit/recovery/events contracts; run reports/artifacts; delegation index; feed/Review/thread flows; ledger and cursor; model override and single-checkout gate; CLI JSONL/readers/term renderer; exact conversation identities; same-origin/input protections; vaultwriter and capability boundaries. No new scheduler, attention kind, approval lane or vault writer.

REPLACE progressively: tmux process creation/list/capture/send/attach for selected coding sessions; browser agent-state polling and local readiness guessing where verified herdr capabilities suffice; terminal history/pin/rename machinery when consumers migrate. KEEP temporarily: legacy tmux adapter and remote keep until their actual sessions are drained or explicitly resumed on a supported backend. Files/fleet stats are separate facilities, not registry history; do not delete their APIs while slimming Terminal.

## 4. Concrete minimal migration, in commit-sized gates

### A. Prove the installed protocol, then add a narrow runtime adapter

This audit read local `herdr --version` (0.9.0), `agent wait --help` and `api schema --json` (protocol 22, schema 1). The bundled schema includes `agent.prompt` with optional wait, `agent.wait`, and `events.subscribe`. No live server or pane was changed. The existing Metis wait spike is accepted; attach, restart and noninteractive board detection remain integration tests, not claims made here.

The current official [socket documentation](https://herdr.dev/docs/socket-api/) describes event subscriptions, occupant-pinned waits, atomic prompt-plus-wait, and subscribing before taking a bootstrap snapshot to avoid gaps. Treat that as an implementation guide and verify behavior against the installed binary before relying on it. The local version/schema is the compatibility gate; current website docs may describe a different build.

Introduce a small `terminalRuntime` seam (suggested new `server/terminal_runtime.go`, `server/terminal_herdr.go`): create, attach, inspect/list, read screen, send text/key, close, subscribe state, bounded wait. Separate runtime connectivity/process existence from agent state. Keep tmux behind the seam for old rows; reuse existing launch-command/model builders. Use explicit targets, never herdr's focused/default pane. Keep the Unix socket local, server-side and permission-restricted; browsers use existing Manifest trust boundaries.

Gate: isolated scratch herdr session proves shell/interactive Claude/Codex plus board-style noninteractive launches, exact cwd/model/resume, bracketed multiline delivery, Ctrl-C/arrows/y/n, dialog refusal, 120×32 creation and resize, detach survival and clean teardown. Verify headless output piped through `tee` still yields useful detection. If not, show unknown; never change the result contract to make detection look better.

### B. Move new coding chats; retain a small operational mapping

Version the existing row format initially with a backend discriminator (missing means tmux). Persist stable manifest session ID → host identity, backend, herdr session/workspace/pane identity, agent occupant/session identity, CLI kind/conversation ID, cwd, model and work-order/run link where applicable. Labels are display metadata, never lookup authority. Herdr runtime IDs need generation/occupant validation, not blind persistence across daemon restarts.

Do not bulk relaunch existing processes or delete `terminals.json`. Import its resume/work-order metadata idempotently with a backup; keep old IDs and URLs. After adoption, this becomes a compact chat/run association store, not a second terminal inventory/history. Historic conversations remain visible in Chats through CLI files plus explicit associations; shell-only history can be retired once no live/remote consumer uses it. Protect board links from a chat “forget” operation.

Wire existing transcript/screen/input endpoints through the row's backend. New eligible local chats use herdr; old active tmux sessions stay attached to tmux until they end. Resume an ended conversation into herdr only after confirming the old process is absent, identity is exact and work-order first-launch is not replayed. Unknown runtime state must never trigger automatic fallback launch or duplicate prompt submission.

Gate: old Claude JSONL fixture output and DOM classes unchanged; Codex exact-file discovery fixtures cover multiple sessions in one cwd, missing files and wrong IDs; legacy row import and exact resume work after Manifest restart. Add mapping persistence error handling before claiming successful launch.

### C. Push agent indicators and support turn supervision

One server subscription per configured daemon feeds an in-memory snapshot and a Manifest SSE endpoint (proposed `GET /api/terminal/events`). Bootstrap by subscription acknowledgement, snapshot, then buffered events; reconnect resnapshots. Event payloads carry manifest session ID, runtime generation/occupant, agentState, connectivity and observation time. Fan out to coding Chat rail/header and task-thread badges through the existing run/session link. Use quiet shared dots, lowercase verbs and `fmtWhen`; retain full-bleed exceptions and `.term` rendering. Other chat backends keep their own state sources.

UI agent state is event-driven: no interval fetches to discover working/blocked/idle. On disconnect show unavailable/unknown and invalidate stale working indicators. Bound queues, coalesce repeated updates and cancel subscriber resources on shutdown. Transcripts remain file-backed: retain the existing file tail temporarily (explicitly distinct from state polling), then optionally use file-change notifications for refresh. Always read final file changes even after a process stops; current live-only tail gating should not hide the final transcript.

For an authorized supervised send, use verified atomic `agent.prompt` plus wait when suitable, otherwise establish an event observation boundary before sending. Do not issue a naive wait for idle after sending: preexisting idle or a short missed turn can satisfy it incorrectly. Bound/cancel waits, track the same occupant, and report timeout as unobserved completion. Do not replay an uncertain send after reconnect. Wait wakes file reconciliation; file validation decides outcomes. Retain an independent file watcher or the existing 60-second sweep for missed events, restarts and late result writes. Removing durable recovery scans because wait exists would violate the truth boundary.

### D. Migrate board execution, then slim Terminal

Switch board create/inspect calls through the same adapter once interactive gates pass. Preserve the shared checkout lock and pinned model on both first launch and resume. Test restart between every persisted launch step, death without result, blocked/malformed/late results, socket outage while the process runs and successful result after lost events. No herdr label may complete a run or unblock a second checkout writer.

Terminal becomes a compact `open` launcher (shell/agent, cwd; host only where supported), live server session/pane list, and attach. Reuse xterm/WebSocket transport for a herdr attach client after proving it actually provides the required terminal interaction; an `agent attach` helper is not assumed to be a browser byte-stream API. `open in terminal` from Chat/task must target the exact pane. Closing the browser detaches; explicit end closes the process. No dead-session history rail, pin history or registry-based default-name accounting remains there. Conversation resume belongs in Chats; run history belongs in board/ledger. Retain Files/stats access during this change without expanding their scope.

Only delete tmux helpers and history endpoints after enumerating callers, migrating external `/api/terminal/agent-session` users (currently returns a `tmux` name), draining legacy local sessions and covering remote keep. Introduce a backend-qualified handle without silently changing the old response's meaning. Remove unused JS/CSS deliberately; respect owner-call conventions rather than copying legacy uppercase button labels.

## 5. Restart, remote machines and rollback

* Run herdr independently of Manifest's systemd cgroup with a stable user/home/TMPDIR and socket location. Client-detach survival does not establish daemon-crash or machine-reboot survival. Test each separately. Pin the aging 0.9.0 binary/protocol; unknown versions disable new herdr launches with a useful reason, preserving read-only transcripts and existing legacy attachment.
* On Manifest reconnect, reconcile host + daemon generation + runtime occupant against the stable chat/run association and CLI identity, then reload JSONL/results/reports. Missing or reused pane IDs are unresolved, never reassigned by title. An orphan live pane may appear in Terminal's live inventory without being assigned a task. Do not auto-adopt it into a run.
* A daemon snapshot/layout restore is operational state, not the vault. A lost process requires exact CLI resume, never brief replay. Keep resume identities outside disposable herdr state and recover them from file evidence where possible. Current registry/work-order data has nontrivial recovery value despite living in dataDir; do not casually call it safe to delete.
* Host-local dataDir and sockets do not sync under ARCHITECTURE §8. Shared harness files do not imply shared process ownership or valid local transcript paths. Only the owning Manifest process writes its run metadata; a second machine must not dispatch the same run. Remote herdr requires target-side daemon survival plus an explicit authenticated connection and transcript access model. Until that is proven, keep existing remote tmux Keep. No fleet-wide migration or Hermes transport rewrite in the first rollout.
* Rollback routes new launches to tmux; existing herdr-owned sessions retain the herdr adapter until ended. Never fall back by launching a second copy because herdr is unreachable. Keep migration backups and association schema readable by the rollback build; disabling the new backend cannot destroy exact resume or result ingestion.

## 6. Verification and delivered scope

No terminal/chat/board runtime was changed, no herdr daemon installed/restarted, and no vault files written. One unrelated baseline repair was necessary for the requested green suite: consolidate five duplicate bare selectors in `server/web/css/86-deal-diligence.css` and its required identical OODA copy `server/web/ooda/src/deal-underwriting.css`, retaining their effective declarations and media overrides. This is not an integration slice.

Regression gates for implementation: existing `terminal_*_test.go`, `web_chat_*_test.go`, `board_coding_test.go`, `board_models_test.go`, `todo_drop_test.go`, ledger/delegation/artifact tests; preserve DeepSeek enrichment (`recruiting/lookup_deepseek_test.go`), recruiting ties/leverage (`server/recruiting_leverage_test.go`), model override, drop cleanup and board direct assignment. Add meaningful adapter/reconnect/turn-race tests described above, not tests that merely mirror plumbing. Live browser checks must prove exact attach, term rendering, event-driven badges, multiline/menu safety and resume with the same JSONL.

Validation results are recorded below after final execution. Full logs remain in `/tmp/jarvis_herdr_{build,tests,gofmt}.log`. Commit/push is authorized for this report and the baseline repair; implementation and production migration remain deferred as requested.

Final verification (2026-09-09):

* `gofmt -l server/`: no output; no Go formatting changes needed.
* `go build ./...`: exit 0, BUILD_OK.
* `go test -count=1 ./server/... ./...`: exit 0; `manifest/server` passed in 8.040s, all packages passed. Tests ran in the supplied workspace including its pre-existing untracked test file; unrelated files were not staged.
* `git diff --check`: passed. A declaration comparison confirms all five consolidated selectors retain their effective top-level property values; media overrides remain in place. The required shared CSS copies match. No live browser/daemon migration was attempted.
* `/tmp/jarvis_herdr_audit_findings.md` exists and matches the tracked audit copy. The initial CSS duplicate-selector failure and subsequent shared-copy check were resolved before this final green run.
