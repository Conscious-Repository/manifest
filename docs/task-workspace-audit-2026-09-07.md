# Tasks and Board UX audit — 2026-09-07

The task surface should answer two questions quickly: what can I advance now,
and what needs my judgment so an agent can continue? A task is also a durable
working conversation, which can begin as a short reminder and develop a brief,
produce artifacts, request approval, and lead to separate follow-up tasks.

## Evidence and owner decisions

The live `/api/tasks` projection contained 30 active tasks: 13 Aion, six real
estate/property, and 11 Personal/Home. Five carried agent owners; none had a
current delegation in that snapshot. Five recently completed personal tasks
included long Manifest bug reports. All 30 active cards appeared in Open while
Delegated and Review were empty. This is a point-in-time observation, not a claim
that agents have never run: completed coding tasks already demonstrate that use.

The live board, source, September 4 agent-chat plan, September 6 direct-assignment
plan, architecture, UI conventions, and supplied BuildTall doctrine were reviewed.
The owner clarified during this audit:

- Usually review delivered work or pick something to execute with available time,
  potentially starting several things together. Next actions should be the default.
- Approvals should remain in Feed and also be actionable beside task conversations,
  including task-specific Chat. Leaving the work to authorize it is friction.
- A short task can grow through conversation. Bitwarden hosting could finish and
  produce separate follow-ups for agent access and the Aion team.
- The electrician example defines the desired loop: research → shortlist → request
  bids → review drafts → send → track replies. It is an example, not outreach consent.

## Research translated into this system

| Reference | Useful pattern | Manifest application |
| --- | --- | --- |
| [Linear My Issues](https://linear.app/docs/my-issues) | Focused personal views and deliberate ordering | Next actions retains authored rank; domain and attention filters answer separate questions. |
| [Linear assignment and delegation](https://linear.app/docs/assigning-issues) | Delegated work remains visible to the responsible human; issue context travels into coding tools | Agent assignments stay visible in With agents. Opening a task leads to its conversation rather than renaming its title. Manifest keeps its existing owner model. |
| [Linear agents](https://linear.app/docs/agents-in-linear) | Agent participation is inspectable within the work | Named agents and truthful execution states; assignment alone does not claim a running job. |
| [Block Buzz](https://github.com/block/buzz) | Humans, agents, evidence, and decisions share a workspace and record | Task-linked approval evidence and controls appear in the conversation using the existing Feed record. No adoption of Buzz's relay infrastructure. |

These are selected patterns, not an attempt to reproduce another product. Linear's
team hierarchy and Buzz's transport are unnecessary for this single-owner system.

## Findings and changes

| Finding | Change |
| --- | --- |
| Four equally wide columns squeezed active work beside empty space and verbose history. | Three active lanes, completed work in a disclosure; filtered boards omit empty lanes. Phone boards stack; an open desktop task gets a readable workspace pane. |
| FOCUS meant every domain, not focus. No way to isolate available work or failures. | All domains plus independent Next actions, All active, With agents, and Needs attention filters. Search intersects those controls. Preferences persist. |
| Assignment was visually confused with execution. | Assigned without a run is explicit. Plan-ready, results, failures, blocking and waiting are derived centrally. No new persisted status. |
| Task-title clicks edited; work entry depended on tiny glyphs and hover. | Native title buttons open the workspace. Explicit Rename uses the shared component. Work, Done, Reopen, and Follow-up are visible controls. |
| Description, coordination and plan pushed conversation below the fold. | Conversation and actionable evidence lead; reference fields share a Task details disclosure. A ready plan remains visible. |
| Feed was a mandatory detour for approval. | Exact task-linked proposals use the same enriched renderer, guards and decision endpoint in the task panel and task Chat. Decision refreshes converge and leave a task-thread trace. |
| Repainting could discard an unsent message; a late request could display another task's data. | Per-task in-memory drafts retain text, recipient, mode and attachments across repaint/switching. Reads check task identity; a new selection clears stale controls. Drafts do not survive a page reload. |
| Failures looked empty; Manifest history was incorrectly grouped into Personal. | Task fetch failures retain last data and offer Retry. Completed tasks and backlog honor domain filtering; Manifest history resolves correctly. |
| Capture defaulted to Inbox even inside a domain. | Capture starts with the current domain. Follow-up capture can retain the current property. Tasks remain independent, with no new parent/child schema. |

The existing token system, light/Jarvis themes, native JavaScript, Markdown task
substrate, guarded writes, and shared components remain the implementation basis.
The architecture amendment records the owner's explicit inline-approval decision.

## Boundaries and next capability

Next actions means open work without an agent assignment, known blocking or waiting,
not a prediction of how long it takes. Authored rank determines order. No effort
estimates, AI ranking, fabricated next steps, or forced task decomposition were added.

The electrician flow is not yet a complete general-purpose email workflow. Existing
task proposals can enqueue approved errands; direct Gmail send is implemented for
recruiting. Task-linked outbound-message records, exact recipients/body/attachments,
provider message/thread IDs, inbound reply correlation, and follow-up monitoring
need a separate connected implementation. An approval trace says approved, not sent.
A send receipt must establish sent; a correlated inbound message must establish replied.
Reuse existing approval, errand, email-sync and scheduler paths when implementing it.

Approval cards require an explicit `[todo:: id]` association already used by task
proposals. Related subject matter alone does not attach an unrelated approval.
Only pending approvals are cards; settled decisions remain in their existing store
and task trace. Existing proposal-type guards still determine what approval does.

## Validation

Regression coverage exercises domain/query/work-state intersections, waiting and
blocked work, assignment versus execution, ready plans, failures, and exact approval
membership. Backend tests verify shared Feed/task approval identity and guards and
that settling one record removes it from the task. Existing task, approval and UI
checks are run alongside syntax checks. Browser review uses a local preview of the
changed assets with read-only access to live task data; production tasks are not
mutated during visual checks.

Verified results:

- `node --test tools/tests/task-views.test.cjs`: 3 tests passed.
- `go test ./server ./tasks ./goals ./approvals`: passed. Final targeted server
  checks also pass after the last UI changes and approval-trace regression test.
- `go test ./...`: all packages passed. A pre-existing golden-corpus failure
  was reproduced on untouched HEAD: the older snapshot still contains retired
  `until` and `verify` fields. The owner confirmed these fields are unused and
  should remain retired. The corpus check now permits only retired-field removal
  per line and requires subsequent serialization to be stable; other differences
  still fail. The existing goals tests verify frozen history is preserved. No
  personal snapshot or vault content was rewritten.
- Browser: desktop light and Jarvis; tablet and phone layouts; title-to-workspace;
  retained draft after switching away and back; search/no-match/keyboard clear;
  selected filters; fictional approval in task panel and task-specific Chat;
  refused approval preserves the card. No production mutations were performed.
- The board now refreshes through the existing 8-second task-panel polling loop
  while Tasks is visible, including with no panel selected. Editing and dragging
  pause refresh; completed agent work can appear without a manual reload.

The implementation is local and uncommitted. The local preview uses changed assets
against the existing server's read endpoints and rejects writes. Inline enriched
approvals were visually checked with an isolated fictional fixture; the new backend
projection is covered by server tests. Production deployment has not been performed.
