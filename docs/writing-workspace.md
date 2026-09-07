# Writing workspace — implementation evidence

Owner scope: 2026-09-07 updates to the vault's
`system/workbench/plans/2026-09-06-writing-workspace-and-send.md`.
This is an implementation record, not a second plan. Send is excluded.

## Available locally

WORK → Writing opens a task-optional writing surface. New Markdown files start
at the configured vault root. Open-file search, document tabs, source/live-preview
switching, find, wikilink completion and modifier-click navigation, explicit save,
Markdown export, rename, and a searchable folder-move picker are implemented.
The folder picker includes root and existing folders; creating folders from that
picker is not implemented. The screenshots are interaction references, not a
commitment to implement every Obsidian menu item (splits, merge, PDF, etc.).

Selections open a comment composer. Conversations are anchored to exact UTF-8
byte ranges and SHA-256 revisions, with original excerpts, replies, resolve,
dismiss, locate, and unique-context display reattachment. Ambiguous/deleted
passages remain readable and replyable. A note move carries its conversation
record and updates explicit task document pointers. Full-path wikilinks in other
notes are not rewritten. Task bindings use the existing todo-plan frontmatter;
assignment preserves unrelated fields, ordering and body bytes.

The owner clarified on 2026-09-07 that there is no separate source collection.
Reference retrieval now searches the existing vault automatically, retaining
knowledge-zone, daily-folder and AI-region filters. The API returns exact current
excerpts, hashes and byte spans, supplied to Ask as bounded context. Explicit wikilinks resolve by filename as well as text search.
The writing UI exposes only comment and ask, with no reference-specific controls.

## Agent questions

Ask uses the installed Hermes provider configuration through a fixed completion
helper, not `hermes -z`. The audited one-shot runtime enables tools, hooks and
unattended approvals; the writing helper imports only config/provider resolution
and sends a bounded chat-completion request with no tools. It has no dispatch
loop and rejects tool-call responses. The supplied packet includes selected text,
up to 6 KB of surrounding text, recent discussion, filtered FTS excerpts and up
to four explicitly linked vault notes. The existing knowledge/daily/AI-region
boundaries apply to both retrieval paths. No automatic web search is claimed.

Questions, running turns, answers/failures, model and reported token usage are
append-only events in the writing record. Request IDs deduplicate uncertain POST
retries; requests interrupted by a restart become retryable failures. The server
saves only Alfred's reply through the annotation capability, never editor bytes.
A running answer prevents a file move; editing remains available. The UI polls
pending turns and preserves the discussion across navigation and reload.

Runtime defaults to `~/.hermes/hermes-agent/venv/bin/python`, overrideable with
`hermes.annotationPython`. The current Metis runtime resolves a custom local
chat-completions provider. Unsupported provider modes fail without falling back
to an execution-capable agent. Completion time is capped at 160 seconds and
output at 2,400 tokens; there is no daily dollar-cap claim. Cancellation UI,
web-backed fact checking and mechanics acceptance/undo remain future work.

SSH access uses `ssh -i ~/.ssh/aion_cluster_ed25519 -o IdentitiesOnly=yes
benjamin@metis.tail8f89de.ts.net`. The key needed unlocking from macOS Keychain via
`ssh-add --apple-use-keychain ~/.ssh/aion_cluster_ed25519`; the server already
accepted it. No new credential or expanded permission was needed.

## Editor choice and build

The shipped `70-note.js` baseline has separate raw textarea and rendered preview.
It has no continuous live-preview editor or persistent selection-decoration
facility. Adopting that baseline would not satisfy the requested interaction;
adding a second mirrored layout engine would duplicate editor responsibilities.
The selected executable implementation is the pinned CodeMirror 6 bundle. Browser
checks demonstrate wrapped selection, saved highlights, editing and keyboard save.
No simulated textarea highlights are presented as a completed solution.

The standalone build recipe, exact dependency lockfile and license regeneration
are in `tools/writing-editor/README.md`. The approximately 550 KiB bundle and MIT
notices are embedded in the Go binary; no CDN or production JS build step is used.
This is initial feasibility evidence, not a claim to Obsidian feature parity or
completion of the plan's touch/IME and long-document pilot gates.

## Storage and concurrency

- `/api/note` PUT and checkbox edits require `ifRevision`. The existing raw
  overlay and note-editor callers now supply it. GET returns the revision of the
  exact bytes read. No save occurs on open; uniform CRLF is restored on save,
  including final-newline absence. Mixed EOL files open read-only.
- New editor operations use declared user-action capabilities, zone/path checks,
  symlink refusal, exclusive create/move destinations, shared in-process writer
  serialization, atomic save replacement and audit. Checkbox and contact edits
  serialize with editor saves; capability section updates use a serialized
  transform. Domain stores with previously captured whole-record snapshots still
  have their own concurrency contracts; this is not a repository-wide storage
  transaction migration.
- Prior observed note bytes are preserved in content-addressed sibling files
  `<note>.md.pre-write-<SHA256>`. They are durable history, not indexed Markdown.
  They currently have no pruning UI; retention/backup policy needs owner review
  before sustained deployment.
- External Obsidian/sync writers do not share Manifest's in-process mutex. There
  remains a check/rename race. The observed prior file is backed up and the
  submitted buffer is retained until acknowledgment; zero-loss filesystem CAS
  against arbitrary external writers is not claimed.
- Moves with conversations use exclusive companion creation and destination
  linking before removing the source note. Interruption may leave duplicate names
  requiring reconciliation. This is not an atomic multi-file rename transaction.
  Old conversation records are archived with a `.moved-<hash>` suffix.
- Conversations are readable fenced JSON events in Markdown under the configured
  system writing root. The record kernel owns framing and preserves unknown bytes.
  Owner state changes use record revisions; agent append denies owner-state
  mutation. Invalid records produce errors instead of being overwritten empty.
- Local drafts are keyed by vault identity, canonical path and page identity.
  Another tab's recovery entry is not overwritten. Recovery copies are offered,
  never silently installed. Save acknowledgments cannot mark newer typing saved.
  Quota failures are visible; localStorage is recovery, not backup. Caret, scroll
  and source-mode preferences persist separately; open tabs remain in memory
  across routes, with only the current route automatically reopened after reload.

## Validation

Automated tests cover exact-byte saves, stale/missing revisions, missing files,
Unicode/CRLF anchors, duplicate destinations, symlink/path/zone refusal, competing
editor saves, comment preservation and relocation, invalid records, denied agent
state writes, FTS collection boundaries/current-byte evidence, and HTTP integration.
Node tests cover save-in-flight typing, failed-save draft retention, CRLF/emoji
mapping, ambiguity, and mixed-line-ending detection. JavaScript syntax checks and
Go package tests pass for changed packages.

Browser checks used a disposable sample vault, not the owner's notes: open, live
preview/source layout, passage selection, comment creation/reload, keyboard save,
keyboard folder search and move with retained comments, external-edit conflict
and explicit saved-version recovery, desktop margin and phone
sheet. Default and Jarvis layouts were inspected at desktop, tablet and phone
sizes (1280, 1024, 390). Full touch/IME testing, adjacent-breakpoint behavior,
large real-vault performance and owner writing-session sign-off remain pending.

`go test ./...` has one pre-existing local failure: `record.TestCorpusGoals` on
retired `until`/`verify` fields in the local golden corpus. The identical failure
was reproduced in a clean `git archive HEAD` checkout. The implementation does
not change those goals parsers or mutate the owner's golden corpus to mask it.

At the initial UI checkpoint, no commit, push, deployment or live model request had been performed. Subsequent integration is recorded above.

## UI refinement and regression pass — 2026-09-07

Tabs now have integrated close icons, adjacent-tab close behavior and arrow/Home/End
navigation. The final tab resets the welcome view. Generated conversation records
are excluded from the file browser. Comments use compact quoted passages and
quiet author/time metadata; resolve/dismiss live in an overflow menu. A new
comment saves the exact selected document snapshot without an intermediate modal.
Unsent comment drafts are protected when closing a tab or leaving the page.

Regression fixes cover empty/stale selections, edits during submission, late
comment loads after another load or a file move, and incidental focus stealing.
The 861–1100px sheet previously made the page inert while remaining invisible;
it now displays, traps focus and releases focus correctly across breakpoints.
Browser checks in this pass covered 390/1024/1280px, comment submission, attempted
Ask with retained draft, unsent-close protection, resolve, adjacent close and
last-tab reset. Node tests cover eight state/byte invariants. Go tests also verify
reference retrieval with no collection setup. Full touch/IME and long-session
performance remain unverified; these checks do not establish that all bugs are absent.

### Obsidian-inspired refinement

Inspected the owner's local Obsidian 1.13.7 window and document menu without
changing note content. Writing now places tabs above a compact path/actions row,
uses quiet unboxed toolbar controls, hides save while clean, and exposes source
mode/live preview in the document menu. A small word count replaces the generic
Markdown footer label. New comments remain readable without immediately opening
an empty reply form. Manifest's existing theme tokens and shared menu remain.
