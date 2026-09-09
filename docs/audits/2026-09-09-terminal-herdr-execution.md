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
