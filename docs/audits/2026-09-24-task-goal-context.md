# Task and goal context — September 24

Records extends the private workspace's Notes picker with open tasks and current
goals/stages. It uses the existing unified task and goals projections; no second
record store, new parser, task creation, assignment or execution is introduced.
Existing saved Notes tabs still restore as knowledge-note views in Records.

Task search retains composite IDs across personal, property and domain backlogs.
Equal titles are separate options showing their container and ID. Preview contains
projected task fields, dependency and artifact IDs, plus the existing description
and plan. It excludes comments, run state and linked file contents. Narrative reads
reject a mismatched plan identity instead of inheriting another task's description
from a colliding filename. Property-read errors fail the selection surface; existing
board callers retain their prior projection behavior.

Goal search covers the current goals tree, including stages, ID, ancestry and
aliases. Preview includes the selected branch, its current fields and descendants,
ancestor titles/IDs, area and north-star context. It excludes unrelated branches
and separate archived quarters. Changes to ancestry invalidate an older preview.

Explicit selection compares the entire preview hash, then retains exact bytes
through the existing artifact registry. The snapshot names the source record kind
and ID; task ownership and producing-run provenance remain unset. It is preview-only
in the artifact editor and links back to the original task/goal. Provider context
contains the exact retained snapshot and source ID, through the existing private
explicit-artifact path. Shared inputs and portal routes do not gain access.

Records preserves the selected kind, query, identity and reading position through
workspace recovery. Changed source content requires fresh review and cannot replace
an existing composer selection implicitly. Switching kinds clears prior preview
and invalidates delayed search results. One artifact remains selected per composer;
selecting a record replaces that selection, visibly stated before selection.

Validation:

- `TestRecordContextTaskIdentityAndStaleness`: separate equal-title IDs, dependency
  fields and narratives, no source mutation or browse side effects, exact-reference
  retry, no task ownership grant, stale plan rejection, source route and collision
  denial. Closed/missing tasks remain outside the open-task selector.
- `TestRecordContextGoalBranchAndRevision`: selected branch/ancestry, exclusion of
  unrelated goals, parent serves relation, source route, no source writes, stale
  ancestry and record-kind substitution rejection.
- `TestRecordContextDeliveryAndSharedBoundary`: fake native input contains the exact
  snapshot/source ID; retries do not execute twice; shared input rejects the same
  retained reference; public portal search/preview/retain routes remain absent.
- `TestRecordContextReadFailuresAndKnowledgeCompatibility`: property identity,
  explicit failure after the property index closes, and knowledge-note API parity.
- Existing knowledge-note provider/retention and unified-task tests pass. Chromium
  `server/testdata/chat-note-context.cjs` covers all three kinds, exact ID payloads,
  kind/selection restoration, stale search responses, shared-chat hiding and both
  themes at 320/390/1440 px. The existing workspace fixture passes. Phone screenshots
  inspected at `/tmp/manifest-note-context-phone.png` and
  `/tmp/manifest-record-context-phone.png`.
- Release build, JS syntax and diff checks pass. `make test` passes server and other
  packages except the known `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation`
  source-hash re-audit in unchanged Hermes files.

This does not complete general record linking: closed/archived task discovery,
schedule, structured people/organizations/candidates, capability/skill context,
multiple selections and integrated live-provider/physical-device acceptance remain
open. Context retention is an observed projection, not a transaction across all
underlying files. Provider evidence in this pass uses isolated fixtures.
