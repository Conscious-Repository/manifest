# Reviewed project context — September 24

Records now searches saved workbench projects by display name or exact project ID.
Equal names retain separate identities. Preview reads the canonical vault project
record and projects only the selected project's saved instructions/reference links,
assigned conversation keys and their priorities, and saved folder associations.
Conversation and folder contents, other projects and freeform surrounding record
notes are excluded. Folder-derived sidebar groups become selectable only after
existing project creation has saved a real project ID.

Explicit selection retains the exact reviewed projection in the artifact registry,
with `project-context` provenance and source-project identity. Existing private
multi-selection, synced drafts and receipt-backed delivery carry the version.
Selecting does not create a project, change chat membership, provision a folder,
read member transcripts or execute anything. Stale previews must be reviewed again.
Project snapshots are read-only in the artifact editor. The source link opens
`#/chat/project/<encoded ID>`, with the exact saved project and access to its existing
editor; unavailable records show an error, and late loads cannot overwrite another
route. Missing source records lose their artifact navigation link.

Evidence:

- `TestProjectRecordContextIdentityRetentionAndPrivacy`: equal names/distinct IDs,
  metadata-only search, selected instructions and references, exclusion of other
  projects, deterministic ordering, no browse retention or source mutation,
  idempotent retention, source identity/navigation, changed-source rejection,
  explicit delivery authority, portal denial and corrupt/missing source errors.
- `TestProjectRecordContextNativeDelivery`: fake native delivery after project
  instructions change, exact retained bytes, repeat submission without duplicate
  execution, unchanged task scope and shared-input denial before execution.
- Chromium `chat-note-context.cjs`: keyboard project selection, exact ID/revision
  payload, source link and picker restoration, source-page editor target, no send
  composer, missing-record error and late-load navigation race. Both themes at
  320/390/1440px pass overflow checks. Phone picker screenshot inspected.

This covers saved workbench projects. Structured organizations/candidates,
schedule, capability/skill context, full output registration and integrated
provider/physical-device acceptance remain open. Provider sends use fixtures.

Focused project/record tests, the source-route dispatch/navigation test, Records
and workspace Chromium fixtures, JS syntax, diff checks and release build passed.
`make test` passed server (39.857s) and other packages except the known unchanged
Hermes source-hash canary re-audit failure in `cmd/re-intake-canary`.
