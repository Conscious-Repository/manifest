# Reply monitoring end conditions — September 24

The canonical sent-email card now offers an explicit stopping rule before
tracking starts: stop when a reply is found, or continue until manually stopped.
The card states its five-minute cadence and active rule. A reply-found watch
retains its stopping reason and previews after stopping and across restart.
The owner can stop earlier or restart with another rule.

The existing private watch endpoint accepts optional `stopAfterReply`; omitted
values preserve the existing rule. Existing watches and preparation-time
`monitorReplies` keep their until-stopped behavior. The pending approval explains
that behavior. Configuring the rule invalidates outstanding poll claims. Only a
successful anchored thread read with a qualifying reply stops monitoring;
provider failures, missing sent anchors and empty reads leave it enabled.
No additional scheduler, send path, approval or storage system is introduced.

Validation:

- Focused Manifest MCP race tests cover restart, provider error, missing anchor,
  empty thread, verified reply, durable stopping, no further reads after stop,
  resuming until-stopped mode, legacy toggles preserving the rule, and refusal
  before confirmed delivery. Existing overlapping-read tests also pass.
- The sourcing-to-approved-outreach server journey passes under the race detector
  with actual handler requests for policy configuration and legacy stop/start.
  It checks receipt state and unchanged fake-provider send count.
- Chromium renders the real canonical card and repository styles, exercises both
  rules, manual stopping, stopped-state recovery and failed-request recovery,
  and checks 320/390/1440px bounds. Screenshot review found cadence truncation;
  scoped wrapping fixes it and the repeated fixture checks that wrapping.
- Release build, JS syntax and diff checks pass. `make test` passes server
  (40.324s) and other packages except the known unchanged Hermes source-hash
  canary re-audit failure. The added endpoint assertions passed separately.

Proactive reply notices and uncertain-delivery provider reconciliation remain
open, as do other unchecked workbench requirements. This does not claim real
mailbox or physical-phone acceptance. Live verification uses read-only health
and asset checks; no production tracking or mail action is required.
