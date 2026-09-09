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
