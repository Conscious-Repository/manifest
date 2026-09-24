# Recruiting drafts use canonical email approval — September 24

The private recruiting board now prepares its saved outreach draft through
`POST /api/aion/recruiting/outreach/propose/{candidate ID}`. The request carries
the revision of the displayed saved draft. The server checks current recruiting
readiness and exact draft revision before freezing the AION email. Preparation
never decides or sends. Candidate ID and draft revision are retained as structured
email source metadata; their deterministic preparation key recovers one operation
after a lost response, source changes or completion.

Recruiting displays the existing canonical operation card, approval controls and
receipt alongside the append-only draft history. Feed and recruiting act on the
same approval identity. A decision refreshes the recruiting projection, and an
explicit refresh button recovers changes made elsewhere. This is a projection of
the canonical receipt, not a second delivery record. The old direct-send HTTP
endpoint remains compatible, but the board uses the canonical path.

Unsaved edits cannot be submitted as if they were the displayed saved revision.
Saving refreshes the revision; readiness failures remain failures. Loading another
candidate invalidates late log/approval responses so they cannot replace the
active candidate's view. No new chat or candidate record is created implicitly.
The source API and records remain on the private owner listener.

Evidence:

- Expanded `TestWorkbenchSourcedCandidateToCanonicalOutreach`, passing under the
  race detector: source-backed role review and readiness; canonical preparation;
  stale revision refusal; one pending Feed proposal; candidate-specific receipt
  projection; retry after a newer draft; exact reviewed email through fake Gmail;
  completed-receipt recovery without another send. Existing concurrent owner
  confirmation and store-restart checks still pass.
- `server/testdata/recruiting-outreach-approval.cjs`: actual recruiting section,
  canonical operation card and shared decision handler; readiness refusal, lost
  response and identical retry, no direct-send request, Approve→sent receipt
  refresh, unsaved edit guard, candidate navigation race and 320/390/1440px bounds.
  Phone screenshot inspected. No real email is sent.
- Generated Manifest MCP catalog includes optional structured source metadata;
  existing calls without it retain their request shape.

Remaining limits: the canonical outcome is visible beside the old outreach log,
while candidate stage/pointer and legacy sent-row reconciliation are not yet
implemented for this path. Private-chat association is supported by the existing
canonical email flow, but the board does not silently create or choose a chat.
Provider lost-ack reconciliation, physical-phone acceptance and the full integrated
journey remain open. This increment does not certify the entire workbench plan.

Final validation: focused race journey, browser fixture, generated-catalog checks,
JS syntax, diff checks and release build passed. `make test` passed server
(44.009s), Manifest MCP and other packages except the known unchanged Hermes
source-hash canary re-audit failure in `cmd/re-intake-canary`.
