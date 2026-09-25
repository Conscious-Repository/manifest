# Work and context relationships — 2026-09-25

Workstream row: "Work and context relationships" in
`system/workbench/plans/2026-09-11-manifest-agent-workbench.md`. Builds on the
supervision projection (`6e7a399`) and the recovery pass (`cc2e1b9`); nothing
from either is redone. The record kinds shipped on September 24–25 (notes,
tasks, goals, people, projects, candidates, organizations, schedule, calendar,
prior outputs, saved native outputs) are reused, not rebuilt.

## Audit scope

Three maps were taken before any change: every path by which a record
reference is searched, previewed, retained and delivered (`chat_record_context.go`,
`chat_artifacts.go` `selectedArtifactContext` / `retainedArtifactContext`,
`agentchat.go`, `terminal_io.go`, `chat_related*.go`, `chat_shared_input.go`);
every surface where a reference crosses a permission boundary (share review and
import, live shared history, the portal mux, side/related chat origins,
artifact search and source links); and the relationship projections
(`relatedChats`, `sessionConversation`, `unifiedRow` dependency/priority
fields, the Context tab).

## What the row asked for, and where it stood

| Requirement | Found | Done here |
| --- | --- | --- |
| Permission-aware record references and typeahead | Records is a workspace tab with its own typeahead; the composer had no `[[` path (the `@` popup is portal intent tagging only). Access is enforced by mux: the records routes exist only on the owner server; the team portal never registers them. Shared conversations hide Records in the UI and refuse explicit selection on send. | `[[` composer typeahead over one cross-kind search (`kind=any`), gated by the same private-conversation rule as Records; choosing a row opens the exact record's reviewed snapshot in Records. Nothing is inserted into the message, nothing is retained by choosing, and Enter on a suggestion cannot send. |
| Links to goals, tasks, projects, people, organizations, candidates, schedule, knowledge, prior outputs | All shipped as reviewed exact snapshots with source routes; schedule/calendar are date-scoped and calendar reaches the provider. | Left as is. `kind=any` deliberately excludes schedule and calendar so a keystroke never fetches a provider or a date. An unavailable kind is named beside the results instead of hiding the others. |
| Capability/skill context | `SkillInventory` was hard-coded `not-reported` for every adapter and the Context tab printed "Enabled skills: Not reported by this adapter". No adapter reports what a turn loaded. | A read-time inventory of the skill folders each runtime reads (`server/chat_skills.go`): Hermes default/profile `skills/`, Claude Code user + working-folder `.claude/skills`, Codex `skills/`. Roots come from the session record and this process's environment, never a client path. `on-disk` for those three adapters; `not-reported` for herdr-shell, tmux-legacy and remote-keep. The Context tab reads it only when the "Skills on disk" disclosure is opened and says plainly that a turn loads a skill only when it names it. |
| Automatic output provenance/search | Saved native outputs (`9f19cb7`), coding results and run briefs already carry conversation/delivery/run provenance, are content-searchable and resolve source links back to the producing conversation, execution or run. Capture is explicit owner action by a settled decision. | Verified in one journey test rather than changed; see below. Automatic classification of every result remains an explicit non-goal here. |
| Parent/subrun/dependency/priority explicit | Head shows From:/Related:/Coding session: links (`relatedChats`); the Context tab shows the frozen parent snapshot; task previews carry Priority, Depends on, Blocked by, Unresolved dependencies, Dependents. | Verified in the journey test; unchanged. |
| Private-record leakage across permission boundaries | See below. | One labelling gap fixed; the rest audited and left as is with reasons. |

## Privacy audit

- **Portal mux.** `/api/chat/records*`, `/api/artifacts*`, `/api/chat/notes*`
  and the new `…/skills` routes exist only on the owner server. The team
  `PortalHandler` never registers them. Re-verified in
  `TestWorkbenchContextRelationshipsJourney` and `TestNativeSkillInventoryReadsProfileFolder`.
- **Shared input.** A shared conversation refuses explicit private selection
  before any runtime (`agentChatSendTo`, `terminal_io.go`), and side/related
  creation from a shared source is refused. The `[[` typeahead uses the same
  `chatCanSelectNoteContext` gate as Records, so a shared conversation never
  queries the records API from the composer (browser fixture, step 4).
- **Share review and import.** Every retained context reference in a private
  conversation becomes a staged file in the reviewed envelope, named by the
  snapshot's ref `<record id>#context-<hash>`, and the frozen parent
  `Origin.Context` becomes a "Conversation context" system message. Both are
  owner-reviewed before publication and the review UI says the full envelope
  is included. **Fixed:** the file list named a private record only by that
  ref, which reads as a file, not as a record leaving the owner's vault.
  `chatShareFile.Record` now names the source ("task inbox/first",
  "note people/alice.md") and the review list shows "private record snapshot:
  …" beside the file. This is a label on the reviewed envelope; the staged
  bytes and the fingerprint rule are unchanged.
- **Live shared history.** Newer native turns are projected with the
  provider's own user-turn text. Because a shared conversation cannot carry
  explicit private selections, the only reference bytes that can appear are
  task-linked artifacts of the shared task, which shared plans already expose
  by decision. Left as is.
- **Skill inventory.** Skill names and descriptions are owner configuration.
  Private mux only; the Context tab is never rendered for portals.

## Changes

| Area | Change | Files |
| --- | --- | --- |
| Skill context | `chatSkillInventoryFor` scans `<root>/**/SKILL.md` (depth 5, 500 entries, 64 KiB per file, symlinks not followed); frontmatter `name`/`description` with folder fallback; missing/unreadable root reported as unavailable, not empty. Routes `GET /api/agents/chat/{agent}/sessions/{id}/skills` and `GET /api/terminal/session/{id}/skills`. | `server/chat_skills.go`, `server/server.go` |
| Capability matrix | `SkillInventory: on-disk` for hermes-oneshot, herdr-codex, herdr-claude. | `server/chat_capabilities.go` |
| Cross-kind search | `GET /api/chat/records?kind=any&q=` over task, goal, note, person, project, candidate, organization; per-kind failures returned as `unavailable`. | `server/chat_record_context.go` |
| Share review label | `chatShareFile.Record` from `contextSnapshotSource`. | `server/chat_share_review.go`, `server/web/js/47-chat-share.js` |
| Composer typeahead | `[[query` popup in `renderChatComposer`; keyboard ArrowUp/Down, Enter (opens Records at the exact kind/ID, removes the trigger text), Escape; blur closes; 150 ms debounce with AbortController. | `server/web/js/48-chat.js`, `server/web/css/48-chat.css` |
| Context tab | "Skills on disk" disclosure inside Adapter capabilities: lazy read, cached until "read again", disclosure state in the saved view. | `server/web/js/49-chat-workspace.js` |

## Evidence

- `TestWorkbenchContextRelationshipsJourney` (`server/chat_context_relationships_test.go`):
  cross-kind search returns `task:inbox/first,task:work/second` for two
  equal titles with goal/note named unavailable and schedule/calendar untouched;
  the task snapshot carries Priority/Depends on/Blocked by and no other task's
  text; the conversation is linked to `inbox/first` by `SetTask` with the task
  file byte-identical afterwards (no duplicate record); the fake Hermes runner
  receives the exact reviewed bytes with `source-task="inbox/first"` and not a
  later edit; an identical retry runs nothing; Save output registers the reply
  with `provenance.delivery` = the request ID and `provenance.session` = the
  conversation key; `GET /api/artifacts?sources=1&q=` finds it by content with
  a `conversation` source routing to `#/chat/a/alfred/<id>`; the snapshot's own
  sources resolve to `task inbox/first`; the portal answers none of the private
  routes.
- `TestShareReviewNamesPrivateRecordSnapshots`: a task snapshot in a delivery
  context is labelled `task inbox/first` in the review; an ordinary plan is not.
- `TestNativeSkillInventoryReadsProfileFolder`, `TestTerminalSkillInventoryFollowsRegistryRow`
  (`server/chat_skills_test.go`): nested categories, frontmatter name with
  folder fallback, bounded description, symlinked SKILL.md ignored, profile
  vs default root, missing folder unavailable, read leaves the conversation
  untouched, portal 404, Claude user + project roots in order, Codex `.system`
  skills listed, tmux-legacy row reports nothing.
- `TestNativeConversationCapabilities`, `TestChatAdapterCapabilityMatrix`:
  updated so `on-disk` is asserted only for adapters with a known root.
- `testdata/chat-context-relationships.cjs` (`TestChatContextRelationshipsBrowserUI`,
  real front end over a stub API, headless Chromium 1200px and 390px):
  typeahead rows and unavailable note, keyboard selection, Enter removes the
  trigger and opens Records at `task · inbox/first` with the preview and
  `use in this private chat`; zero messages and zero retains from choosing;
  explicit selection retains once and shows the chip; Escape keeps typed
  text; Skills on disk reads once on open, not on reopen, again on request,
  and the disclosure round-trips through getView/restoreView; the shared
  conversation's gate is closed, no records query is made and no popup
  appears; at 390px the popup sits inside the viewport with no horizontal
  overflow. Screenshot `/tmp/manifest-context-relationships-phone.png`
  inspected. Fixture skips where Playwright does not resolve.
- Existing `chat-workspace-tabs.cjs` passes with the reworked Context tab.

## Verification

- `gofmt -l`, `go build ./...`, `go vet`, `git diff --check`, `node --check`
  on the three changed JS files and the fixture: clean.
- `cmd/re-intake-canary TestCanarySourceCallGraphIsolation`: green at HEAD.
  `hermes/authority.go` and `hermes/claude_successor.go` are untouched by this
  change (their last commit remains `e283514`), so the pins refreshed in
  `6e7a399` still describe the audited source; no hash was bumped.
- Full `go test ./...` with Playwright on `NODE_PATH`: recorded in the plan
  checkpoint.

## Left open, named

- The typeahead opens the reviewed snapshot; it does not retain on Enter. That
  is deliberate: the settled contract is review, then explicit selection.
- Schedule and calendar are not part of `kind=any`; use the Records kind
  selector for those.
- Codex's project-level skill folder is not listed because its location is
  not established here; Claude Code plugin/marketplace skills are not listed.
- No adapter reports which skills a turn actually loaded. The inventory says
  so on its face.
- Automatic registration of every adapter output remains explicit owner
  capture by the September 25 decision.
- Browser evidence is headless Chromium over a stub API, not a physical phone
  and not a live provider.
