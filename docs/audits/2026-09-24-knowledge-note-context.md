# Knowledge note context — September 24

The private workbench Notes inspector searches the existing vault index by name,
path and alias. Equal titles retain separate full paths; search does not resolve
or merge people, companies, or notes by title. This increment covers indexed,
non-AI knowledge-zone Markdown only. System records, imported records, individual
task/goal/schedule rows, and general recruiting record references remain outside
this picker. It creates no new source note or task.

Selecting a search result displays the complete literal note, source link and
content hash. An explicit “use in this private chat” retains the reviewed bytes
in the existing artifact registry and selects that exact reference in the existing
synced composer draft. It replaces the current single artifact selection. Send
is still required. The server rejects changed source bytes, empty/non-text notes,
notes over 64,000 bytes, invalid paths and notes outside the indexed knowledge
scope. Preview and search do not register artifacts. Retention retries deduplicate;
even a separately revised registry head cannot substitute for the reviewed hash.

The retained reference records the vault path and source kind. Private provider
context includes source-note identity and exact retained bytes, with existing
explicit-artifact receipt/fingerprint and shared-input restrictions. Source links
return from registered Files to the original note. The snapshot is preview-only
in the file editor; source edits belong to the existing note surface. Reopening
an explicit input in transcript/Context preserves its explicit-selection semantics.

The shared typeahead component has optional accessible combobox keyboard
navigation. Notes uses it for ArrowUp/Down, Enter and Escape. The inspector restores
query, path and reading position; if the source changed, it displays the new preview
with a review notice and does not replace the previously selected draft reference.

Evidence:

- `TestNoteContextIdentityPreviewAndRetention`: duplicate titles, aliases, excluded
  zones/AI notes, exact CRLF bytes, no browse side effects, stale revision rejection,
  deduplication, explicit selection requirement, source navigation and immutable
  retention retry despite a newer registry revision.
- `TestNoteContextPrivateDelivery`: fake Hermes receives original reviewed bytes
  and source path after a source edit; durable exact-reference receipt, no task
  mutation and no duplicate delivery on retry.
- `TestNoteContextBounds` and expanded private-workspace portal test: size/binary/
  query limits and no portal search/preview/retention routes.
- `server/testdata/chat-note-context.cjs`: real Chromium, keyboard selection of
  duplicate names, literal preview, stale retention failure, selected reference
  capture, restored-source-change notice, shared-chat hiding, both-theme bounds
  at 320/390/1440 px. Phone screenshot inspected at
  `/tmp/manifest-note-context-phone.png`.
- Existing Chromium workspace fixture passes. JavaScript syntax and diff checks
  pass. Release build passes. `make test` passes server, vaultindex and other
  packages except the known `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation`
  re-audit failure in unchanged `hermes/authority.go` and `hermes/claude_successor.go`.

Provider delivery here is isolated/fake. Physical-phone and broader record-kind
acceptance remain open; this is not completion of the workbench integration scope.
