# Artifact and change review — September 25

Workstream 4 of the workbench plan. The September 24–25 checkpoints already
delivered hunk collapse and hunk review, syntax for code and hunks, table, link,
image/PDF and generic-metadata previews, save receipts, pending-review recovery,
provenance and exact history restoration. This pass reconciled the remaining
row ("richer diffs … immutable range anchors … distinguish saving a version from
changing a working tree … detect stale range/base") against the code. It found
four gaps and closed them. Generic team-file editing was not built (see Open).

## Gaps found and closed

1. **A decision could land on the wrong version.** `POST /api/artifacts/reviews`
   without `revision` defaulted to the head at the moment the request arrived. A
   client that reviewed version 2 while an agent saved version 3 would record
   "accepted" against bytes the reviewer never saw. A POST now requires an
   explicit revision (428); a GET still defaults to head.
2. **Range anchors were line numbers only.** A recorded range now stores
   `range_hash`, the server-computed SHA-256 of the exact selected lines of that
   revision. A caller may send the fingerprint of the lines it displayed. If the
   bytes at those numbers differ, the request is refused with 412 and nothing is
   written. The browser computes this fingerprint from the displayed revision
   where `crypto.subtle` exists (secure contexts). Where it does not, the server
   still records its own fingerprint. A fingerprint without a range is 400.
   Idempotent replay is unchanged: the fingerprint follows from revision plus
   range, and a refused request is not replayed under its identity.
3. **Stale range and base were invisible.** Review reads now carry a read-time
   projection that is never persisted: `version`, `head` and `head_version`, plus
   `anchors` per recorded range on an older revision. Each anchor says where the
   exact lines stand in the latest version: `unchanged` (same place), `moved`
   (one other place, with its lines), `repeated` (several places, so none is
   named), `changed`, or `unavailable` (not text, or over 1 MiB). The review
   panel shows "Version N is newer. Decisions here apply to version M only." Its
   summary reads "· vN is newer", and each ranged entry states its anchor.
   Decisions stay bound to their own revision. An anchor is evidence for the
   reviewer, not a carried-forward decision.
4. **Saving a version and changing the working tree looked the same.**
   `GET /api/artifacts/get?working=1` adds `workingFile`, which says whether the
   file a ref names now holds the latest version, an older registered version
   (by number), bytes matching no registered version (changed outside
   Manifest), is missing, or cannot be read. It reads through the same spirits
   allow-list as every ref read. Section refs (`#plan`, `#context-…`) and
   agent/runtime addresses without a harness tree report `none` ("Saved in
   Manifest only"). The workspace shows this line above provenance. After an
   owner edit it reads "holds version 1, not the latest version 2. Saved
   versions have not been written to it." The test proves the file bytes on disk
   are unchanged.

## Richer comparison

Version comparison (and the unsaved-edit review, which uses the same view) now
folds unchanged runs behind "show N unchanged lines". It keeps three context
lines around each change and never folds identical text away. Each changed block
that has lines in the newer version offers "request changes to lines a–b". That
goes through the existing review form (`prepareChange`), bound to the selected
revision's exact lines, so the fingerprint and anchor rules above apply. A pure
deletion names no lines. Diff snapshots (`.diff`) keep their hunk-level review
and are not offered block review from comparison. Fold and review controls meet
the 44px touch floor.

## Evidence

- `server/artifact_reviews_test.go`
  - `TestArtifactReviewRangeAnchorsAndStaleBase`: 428 without a revision; 412 on
    a mismatched fingerprint with no record written; the server fingerprints an
    unhashed range; 400 for a fingerprint without a range; identical retry
    reconciles; after a newer head, `version`/`head_version` report a stale base
    and anchors report moved/changed, then unchanged/repeated; the persisted
    record carries no projection fields.
  - `TestAnchorRange` covers the anchor table.
  - Existing exact-version/replay/conflict/portal and hunk-anchor tests pass
    unchanged.
- `server/artifact_objects_test.go` `TestArtifactSaveVersionLeavesWorkingFile`:
  registration matches; the projection is opt-in; an owner edit saves v2 while
  the file bytes on disk remain v1 and project `differs`/version 1; an outside
  edit projects `differs` with no version; a removed file projects `missing` and
  history is retained; section and agent refs project `none`; the team portal
  has no route to the projection.
- `server/testdata/artifact-diff.cjs` (run by `TestArtifactRevisionDiff`): folds
  reconstruct both versions exactly, fold only unchanged rows, keep context, and
  change blocks name exactly the newer lines.
- `server/testdata/artifact-change-review.cjs` (run by
  `TestArtifactChangeReviewBrowserUI` where Playwright resolves): the real
  workspace over a stub API in headless Chromium. It covers the working-file
  line, compare folds and expansion, exact line-10 change request with the
  SHA-256 of `line ten`, 412 refusal preserving notes with no record, a fresh
  request identity on the next attempt, a version-bound decision plus moved
  anchor after a newer head, both themes at 320/390/1440px with no horizontal
  overflow, and a ≥44px touch floor. Phone screenshot
  `/tmp/manifest-artifact-change-review-phone.png` inspected.
- Existing artifact browser fixtures pass: review, review-recovery,
  text-editor, metadata, hunk review/syntax, table/table-review, syntax,
  provenance, save receipt/recovery, link preview, sources. The following also
  pass: `chat-workspace-tabs`, `chat-file-links`, `chat-stage-switch-browser`
  and `TestResearchRevisionBrowserWithBackend`. `chat-review-paths.cjs` hardcodes
  `channel:'chrome'`, which is not installed here, so it did not run.

## Open, named rather than closed

- **Generic team-file editing where explicitly authorized** is not built. Today
  only task plans included in a reviewed share are editable by teammates
  (`server/chat_shared_plans.go`). A generic-file grant needs an explicit
  per-file edit consent in share review. That changes the staged share bytes
  under the fingerprint rule and needs portal views, so it needs its own
  increment and owner sign-off on the consent wording.
- The browser only sends a range fingerprint in secure contexts; plain-HTTP
  cockpits rely on the server's fingerprint and the revision-scoped draft key.
- Anchors match exact lines only; there is no fuzzy relocation.
- The working-file projection covers refs readable through the spirits
  allow-list. Coding-runtime working trees remain represented by immutable
  snapshots (`changes.diff`), which already state that capture does not change
  them.
- No live provider, no production review, no physical device.
