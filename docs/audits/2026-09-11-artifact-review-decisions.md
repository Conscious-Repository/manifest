# Artifact review decisions

A finished artifact had no durable owner acceptance state. The owner cockpit now
records explicit ready-for-review, accepted, changes-requested, and comment
entries for an exact artifact hash. These decisions do not mutate provider
execution state, close tasks, or authorize tools/external actions.

## Ownership and persistence

`GET/POST /api/artifacts/reviews?id=…&revision=…` resolves the existing artifact
registry and validates the immutable revision. Owner-authored events append to
`system/workbench/reviews/<artifact-id>.md` through the record kernel and an exact
review-directory vaultwriter capability. Entry provenance records the producing
thread/run/task and source path. Existing entries and surrounding hand-written
notes remain intact. A full-record revision protects edits, and request IDs make
lost-response retries idempotent. Reusing an ID for different content conflicts.

A newer artifact head does not inherit an older version's acceptance. This is
artifact review, not yet the complete thread/run attention projection. No portal
route is added; owner review decisions are separate from shared conversation
participation and external-action approvals.

## UI

The artifact preview has a compact Review disclosure. Changes requested can
prepare a message with the exact artifact version and notes in the existing chat
composer. The owner still presses Send. Closing or navigating away from the pane
before a request completes cannot draft into another conversation. A successful
decision disables identical repeated clicks until its fields change.

Text review ranges bind to immutable artifact text lines. Binary files and diff
snapshots do not offer the numeric range control; file/line references can be
written in notes. Diff-region selection remains a later requirement. Artifact
comparison and working diff previews now show old/new line gutters, using shared
semantic diff color tokens. Headers without hunk coordinates do not invent line
numbers.

## Evidence and limits

- Full `go test ./server` and `go build ./...` passed.
- Server regression: exact-version acceptance, idempotent replay, changed replay,
  newer-version isolation, hand-edit conflict/preservation, range bounds, source
  immutability, and portal exclusion.
- Chromium fixtures: review acknowledgement loss/retry, exact revision/range
  handoff, repeat-click prevention, detached-pane isolation, 320/390/1440 layouts,
  existing text editing and workspace-tab preservation, literal diff text and
  hunk gutters. Phone review screenshot inspected.
- No real provider instruction, external action, production deployment, or
  physical-phone test was performed. The complete plan remains in progress.

## File-scoped revision requests

Each expanded file in a working diff offers `request changes`, which selects a change-request review and anchors it to the file's exact line range in the immutable diff snapshot. Notes include the file path; snapshot line numbers are distinguished from source-file line numbers. Nothing is recorded or dispatched until the owner chooses to record the review. Unsent review notes recover locally per artifact revision. A newer note typed during acknowledgement is retained rather than cleared with the older submitted decision.

Artifact discussion now selects the active standalone/native chat context when opened there, instead of requiring a linked task or diverting to task-draft state. The recorded request still only prepares the composer.

Checks: full server suite, subsequent focused CSS/navigation checks, build, Chromium review retry/draft recovery, file-range selection, and workspace/editor regressions. This supplies file-level diff anchoring; arbitrary hunk/line selection, syntax highlighting, and complete live review-to-agent validation remain outstanding.
