# Workbench project persistence

Project names and membership were stored alongside disposable inbox preferences.
They now use the existing record kernel and vaultwriter in one private
`system/workbench/projects.md` registry. Project context documents and normalized
run/review projections remain subsequent work; this is not full plan completion.

The compatibility endpoint remains `/api/chat/state/inbox/workstreams`. It
returns `record_version` alongside the existing numeric revision. A write checks
both against the complete source before editing the typed block. Unknown fields
and surrounding owner Markdown remain intact. Old clients must refresh before
writing. Startup migrates once and leaves the old JSON cache as a backup. An
existing record always wins; corruption never silently falls back to stale data.

The capability permits exactly this record. No portal route is introduced.
Archive, stop, sending, approvals and provider histories are unchanged.

Regression coverage verifies migration, cache loss, restart, hand-edit conflicts,
missing preconditions, unknown-field preservation, malformed source protection,
and private route isolation. Browser model choice and lost-send recovery checks
exercise compatibility with the existing chat frontend.

## Project context workflow

The project registry now includes owner-authored instructions/reference text.
`Project context` in the sidebar, or `Context` in the inspector, opens a compact
editor. An unfinished edit uses the existing private, revisioned ChatDraftState
protocol, under a hashed `project-*` edit key. Both the draft and its source
revision survive closing. A conflicting saved source is shown for explicit
comparison before replacement; saving never sends a message.

On first send in a private chat, the selected project's instructions are included
as a visible user-turn snapshot with the exact record revision and source path.
This uses the same persisted send payload and outbox as the current message, so
retries retain the snapshot. Existing threads and direct portal conversations do
not automatically receive changed/private project text. A side conversation may
inherit the already-recorded parent context through its existing explicit flow.

Browser evidence: `server/testdata/chat-project-context.cjs` renders the real
editor at 320/390/1440px, exercises save/reopen/source conflict/recovered draft
conflict, and checks snapshot provenance and standalone isolation. Rendered phone
image inspected at `/tmp/manifest-project-context-phone.png`. Existing workspace
tabs, project dialog, and send recovery fixtures pass. This is fixture validation;
real provider dispatch and live deployment remain release gates.
