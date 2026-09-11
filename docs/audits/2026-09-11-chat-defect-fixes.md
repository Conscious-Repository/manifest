# Chat audit defect fixes · 2026-09-11

All three findings remained present at `84e2005`; none was credited as already
fixed. Read the original chat audit, September 9 herdr execution/operations
references, September 10 chat handoff, architecture and UI conventions. No
checkout, ancestor, or applicable nested AGENTS.md was present.

- **R1:** reproduced the original Chromium outage fixture before editing.
  Attachment recovery now has connecting, connected, waiting, checking, paused
  and exhausted states. Six retries back off at 1.2, 2.4, 4.8, 9.6, 15 and 15
  seconds. Inventory requests and opening handshakes time out after 10 seconds.
  Only a connection lasting at least 10 seconds resets the failure budget;
  an HTTP upgrade followed by attachment failure cannot retry forever.
  Fresh inventory can recover the exact selected runtime, including after an
  unavailable→connected transition following exhaustion. Visibility/stage return
  resumes paused recovery. Exhaustion offers Reconnect. Detach, selection changes,
  and changed runtime identities cancel the old recovery. No prompt/input replay
  or new launch path was added.
- **R2:** Chat exposes only Enter, Escape, arrows and Ctrl-C, verified through
  herdr's existing key adapter. Tab/Shift-Tab are removed from Chat and rejected
  by its handler before HTTP dispatch; copy directs users to Terminal. Terminal's
  separate raw WebSocket byte transport retains both keys, fixture-verified.
  Shared-chat receipt/no-replay behavior remains covered by its existing fixture.
- **R3:** new coding-chat landing identifies the proposed destination as
  “this server · herdr.” Empty Codex transcript copy reports the absence of turns
  and offers the live screen/Terminal, without inventing a missing integration.
  Related legacy comments and the two frontend asset versions were updated.

Validation: focused Go/Node recovery, key, shared-key receipt and legacy input
fixtures; `go build ./...`; `go test ./...`; changed frontend `node --check`;
embedded index/first-party/vendor asset checks; and Chromium regression with
blocked network and fake inventory/WebSocket/xterm. The recovery fixture covers
repeat/extended outages, exhaustion and daemon return, hidden browser/view/stage,
all runtime identity fields, switching sessions, explicit detach, an in-flight
inventory reply after detach, ended panes, handshake failure, short-lived upgrades,
and absence of replay. The first full suite caught a comment-delimited existing
fixture affected by the copy update; it now delimits by function name, and the
full rerun passed. No unrelated test failure remained.

The historical audit reproducer is intentionally preserved; the fixed-browser
companion is `server/testdata/terminal-recovery-browser.cjs` (run from repo root
with NODE_PATH pointing at Playwright). Deterministic Node regressions run in the
Go server suite. No real terminal input, agent messages, daemon outages or process
kills were used for QA. These fixtures do not claim real daemon restart survival,
physical-phone keyboard acceptance, or authenticated CLI end-to-end coverage.
Push/deployment verification is recorded in the work-order result and findings.
