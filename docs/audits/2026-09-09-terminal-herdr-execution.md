# terminal/chats → herdr execution findings

2026-09-09, starting main ba4fadcd0f7f20e9750086ab4a7f3c6dd2a2e60a.

## Gate A — verified adapter foundation

Added a runtime interface and isolated tmux/herdr adapters. Existing endpoints
still use their existing backend pending B. Process, connectivity and advisory
agent state are separate. Unsupported/unresolved supervision refuses before
sending; uncertain sends are never replayed. The herdr adapter requires private
socket permissions and 0.9.0 / protocol 22. The daemon is independently managed.

Live scratch daemon `manifest-migration-scratch`:
- Socket request/response and subscription acknowledgement followed by snapshot
  verified; full raw evidence `/tmp/jarvis_herdr_A_live.log`.
- Interactive Codex gpt-6-astra and Claude sonnet launched in the checkout;
  harmless multiline requests returned MULTILINE_OK as one turn. Atomic
  agent.prompt+wait took ~2–4 seconds, observing actual activity before settling.
- Exact resumes verified from process argv/cwd and prior transcript screen:
  Codex 01a084b2-78a3-73e0-82a1-150d710697a7 and Claude
  9c4cfcf0-abce-4bc3-9280-780113914b0a. No repository edits requested by probes.
- Raw capture proved Ctrl-C, all arrows, y/n bytes. Initial PTY 120×32,
  terminal-ID client attachment and resize 101×37, client termination with
  process survival, explicit pane close all verified.
- Shared boardLaunch builders ran Claude --print and Codex exec --json | tee;
  both wrote durable exit 0. Opt-in test TestHerdrLiveBoardLaunch passed (14s),
  `/tmp/jarvis_herdr_A_board_live.log`. This tests real CLI launches, not board
  result reconciliation (D). Standard tests skip this authenticated live test.
- Scratch server stopped; subsequent snapshot returned server_not_running.

Honest limits:
- Codex was labeled idle at its trust dialog. Added Codex dialog recognition;
  guard refuses text instead of answering. Label is not readiness authority.
- Headless Codex was idle while running; headless Claude stayed unknown. These
  advisory labels cannot complete/fail a board run or release its writer lock.
- Live agent_session was absent, so the adapter refuses supervised sends that
  cannot pin conversation identity. The raw atomic protocol itself passed live;
  resolved-session adapter supervision is fixture-tested, not credited as live.
- Herdr lacks expected-occupant compare-and-send parameters. Preflight and
  generation checks reduce races but do not supply an atomic conversation CAS.
- Terminal attach was verified over a real PTY, not a browser in gate A.
- Daemon crash/reboot survival is not claimed. No production deployment or
  legacy process/registry mutation has occurred.
- Per-pane revision did not advance on every state transition. Subscription
  uses events to obtain fresh snapshots, so stale buffered labels cannot regress
  current state and same-revision state transitions are retained.

Gate A validation: go build ./... passed; go test -count=1 ./server/... ./...
passed (manifest/server 7.531s); 17 focused runtime tests also passed with -race.
gofmt and git diff --check passed. Full logs /tmp/jarvis_herdr_A_{build,tests}.log.

B/C/D: not started; no migration completion claim.

## Gate B — verified new-chat routing and persistent associations

New local coding chats use herdr; missing backend remains tmux. Versioned rows
retain stable IDs, URLs, host/runtime/pane/occupant and conversation identities,
cwd/model and BoardBrief/run association. Legacy import makes a one-time backup
and changes metadata only. Registry parse/write failures prevent new launches.
Launch intent, allocated identity and submitted posture are fsynced before each
side effect; unresolved steps never auto-replay. Ended exact herdr conversations
can resume under the same Manifest ID. Existing tmux rows still use tmux; no
opportunistic legacy process migration or bulk relaunch was performed.

Backend-aware screen, transcript, input, attach and teardown are wired. Work-order
rows cannot be forgotten. Agent-session's tmux field retains its meaning and an
additive backend-qualified handle is provided. Codex exact transcript discovery
validates CLI metadata; new conversations can be identified from the exact
foreground Codex process's open rollout descriptor (Linux), never newest-by-cwd.

Validation:
- go build ./... and go test -count=1 ./server/... ./... both passed.
  Logs: /tmp/jarvis_herdr_B_build.log, /tmp/jarvis_herdr_B_tests.log.
- Live Codex open-file identity + exact rollout resolution passed.
  /tmp/jarvis_herdr_B_codex_live.log.
- Live HTTP create/input/transcript, reconstructed Manifest server, same persisted
  runtime identity, explicit stopped-conversation resume with exact ID/model
  passed (5.414s): /tmp/jarvis_herdr_B_chat_live.log.
- Initial live test exposed placeholder-prompt readiness failure; corrected to
  detected agent + dialog guard and reran successfully. No uncertain send retry.
- Fixtures cover legacy backup/import idempotence, corruption, each persisted
  launch boundary, restart unresolved phases, socket outage/no fallback, failed
  persistence/no launch, lost reply/no resend, work-order forget protection,
  shell readiness refusal, multiple Codex sessions/cwd, wrong/missing IDs,
  ambiguous copies and descriptor/path inode mismatch.
- gofmt and git diff --check passed.

Operational setup: installed/enabled the independent user service
manifest-herdr.service with named daemon manifest; verified active, 0.9.0/22,
empty snapshot. Existing legacy sessions untouched. All three scratch daemons
were stopped and their stopped scratch session records deleted. No Manifest
production restart has yet been performed. Browser UI verification remains C/D.

C/D: not started; board execution is still on its legacy tmux path.

Gate B verification correction: a late fixture edit was staged after the full
suite, and its literal shell-quote assertion failed. Corrected that assertion
without changing production code, then reran the required build and full suite
successfully. B therefore has a small follow-up test commit, a deviation from the
requested one-commit gate shape; published main was not rewritten.

## Gate C — verified event-driven state and bounded supervision

Added GET /api/terminal/events, one shared daemon subscription, bounded/coalesced
consumer projections, unknown/unavailable invalidation, resubscription and fresh
snapshots. The last consumer cancels its socket/reconnect resources. Payloads
include Manifest ID, runtime/occupant/generation, agent state, connectivity,
process, observation time and BoardBrief-derived run ID. Labels only wake the
existing result-file reconciliation; they never establish durable task truth.

Coding Chat rail/header and task-thread/run badges share one EventSource. Removed
the coding rail's 5-second state poll. The separate 1.5-second JSONL file tail
continues after stop, including final reads when hidden or a read is in flight.
HTTP screen/transcript responses cannot restore stale herdr indicators after
SSE disconnect. The independent 60-second AgentLoopTicker remains unchanged.

Authorized API input may request supervise:true with timeoutMs bounded to 30s.
It uses atomic agent.prompt+wait for settled states; unresolved conversation
identity refuses before submission, timeout is completion unobserved, no resend.
Legacy supervision explicitly refuses rather than silently performing a send.

Validation:
- Required build + full Go suite passed; /tmp/jarvis_herdr_C_{build,tests}.log.
- Race-enabled event/runtime/frontend focused tests passed (2.102s).
- Node behavior fixtures verify one EventSource/no state interval, disconnect
  invalidation, stale HTTP reply refusal, task sharing, hidden/in-flight final
  file reads. JavaScript syntax checks passed. gofmt/diff checks passed.
- LIVE adapter subscription observed Codex idle → working → idle (7.563s):
  /tmp/jarvis_herdr_C_events_live.log. Scratch daemon stopped/deleted.
- Live evidence caught protocol details missed by initial fixtures: general
  events use underscore names; filtered status events use dotted names and
  require explicit pane subscriptions. Corrected both before this gate commit.
  A preliminary pane enumeration builds the subscription; authoritative bootstrap
  still occurs after ACK. Pane topology changes force resubscription, with no
  simultaneous daemon subscriptions. No wildcard state support is assumed.

The normal autodeployer applied B; live rows carry backend/version fields and
the original production registry has a mode-0600 backup. Browser verification of
C followed deployment before D implementation; results below.


### Gate C deployed browser verification

Production Chromium check passed: actual Codex assistant JSONL remains visible
following process stop; zero registry requests over six seconds; stopping the
Manifest service invalidated the SSE connection and stale working indicators.
No page errors. Screenshot /tmp/jarvis_herdr_C_browser.png. The successful probe's
first cleanup request raced service startup; cleanup was retried successfully,
and zero probe rows remained. No offline-toggle result is claimed as proof.

## Gate D — verified board adapter and live Terminal surface

Board launches persist the stable session/work-order link before allocation,
then use the same herdr launch journal and pinned model builders as Chats.
Socket errors and incomplete launches remain unresolved/running and retain the
shared checkout writer lock. The reconciliation sweep reads valid result files
before and after stop inspection; labels cannot complete or release a run.
The independent 60-second file reconciliation sweep remains in place.

Terminal now contains a compact open launcher (shell/agent, cwd, host), live
runtime panes, and exact-pane xterm/WebSocket attachment. Files/stats and remote
Keep remain. Unassociated daemon panes use qualified opaque identity handles.
Dead history, pin history, rename and registry default-name accounting were
removed from Terminal. Chat/task actions select exact stable session IDs.
The existing agent-session response keeps its tmux meaning by default; explicit
backend:herdr returns a qualified handle and omits the tmux field.

Validation:
- go build ./... and go test -count=1 ./server/... ./... passed after the final
  executable-discovery fix; /tmp/jarvis_herdr_D_{build,tests}.log.
- Race-enabled focused runtime/board/frontend suite passed;
  /tmp/jarvis_herdr_D_race.log. Node behavioral fixtures and syntax passed.
- Fixtures cover restart at intent/allocated/submitted/active boundaries,
  unavailable daemon while retaining writer exclusion, labels and malformed
  results never releasing the writer, confirmed death without result, blocked
  and late results, valid result after lost events, and final result arriving
  during stop observation. Exact resume/model tests remain green.
- LIVE production board helper launched harmless Codex work, persisted its
  link, observed unknown while the scratch socket was temporarily renamed,
  and observed the still-running process when restored. Durable exit 0:
  TestHerdrLiveBoardJournalAndSocketOutage passed (8.039s),
  /tmp/jarvis_herdr_D_board_live.log. Scratch daemon stopped/deleted.
- LIVE browser check passed compact launcher, mapped shell attachment,
  unassociated exact-pane attachment, PTY resize, detach survival, Files/stats,
  and zero page errors. /tmp/jarvis_herdr_D_browser.log and .png.
  This caught Manifest's service PATH omitting ~/.local/bin; attach now resolves
  the standard user installation if PATH lookup fails, with a regression test.
  Required full checks were repeated after the fix. All probe panes removed.

Retained dependencies and honest limits:
- No real legacy session was killed or bulk-relaunched. tmux helpers and history
  APIs with remaining Chat/skill/remote Keep callers are retained until drained,
  per the requested deletion condition. Caller inventory and migration request
  format are in docs/audits/terminal-herdr-operations.md. Remote Keep is covered
  by fixtures/existing wrappers, not a new live remote-host probe.
- Board restart/result permutations are fixture-driven; the actual board helper,
  CLI, socket outage, attachment, SSE and transcript tests above used live daemons.
- herdr 0.9.0 may omit agent_session; supervised adapter sends refuse unresolved
  occupants. Atomic prompt+wait was proven live at protocol level; resolved
  adapter identity checks use fixtures. There is no expected-occupant CAS in
  this protocol, and no daemon crash/reboot survival promise. Unknown stays
  unknown; no result contract was weakened.
- make deploy encountered an existing SSH public-key failure. Authorized local
  build/service restart and the installed autodeployer provide deployment.

Gate commits pushed before D: A b108b20; B 7734603 plus test correction c6b3b91;
C fed5e97. B's extra correction is the documented one-commit-per-gate deviation.
D commit contains this report; its final hash/deployment stamp is recorded in
/tmp/jarvis_herdr_exec_findings.md after committing.
