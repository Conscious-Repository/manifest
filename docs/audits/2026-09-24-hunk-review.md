# Immutable hunk review

Working-change snapshots now group valid unified hunks into independently collapsible disclosures. Before/after file line numbers remain presentation coordinates; review requests carry one-based ranges in the exact immutable diff snapshot, including the hunk header and any no-newline marker. Selecting a hunk prepares the existing version-specific review form with its filename, header and snapshot range. Recording the decision uses the existing review store, stale-record check and idempotent receipt. Drafting the request still requires the owner's normal Send action to reach an agent.

Collapsed hunk state is saved with the selected artifact revision through the existing workspace view state. It restores for both visible files and files rendered lazily after reopening. Expand all reveals hunks as well as files. Closing/hiding files does not discard their hunk state.

The parser retains blank context lines and handles omitted/zero counts, deleted files, quoted filenames and multiple files/hunks. Invalid or incomplete hunks remain literal text without a precise hunk review action or invented file coordinates. Combined merge diffs and binary patches remain file-level recorded views with visible limits; unrecognized input is not labeled as no changes. Downloaded snapshot bytes are unchanged. No working-tree edit, staging, undo or execution is performed by review.

## Evidence

- Node diff fixture proves selected hunk ranges reconstruct the exact snapshot text, including multiple files, deletion/zero-count headers, blank context lines, no-newline markers, quoted paths and unsupported combined diffs.
- Headless Chromium hunk fixture covers independent collapse, restoration inside a still-closed lazy file, literal markup, range preparation, immutable revision in the review POST, and exact snapshot coordinates in the discussion draft. Both themes passed 320/390/1440px overflow checks; phone screenshot inspected.
- Existing workspace persistence and file-review browser fixtures passed. The legacy file-review fixture ran against installed bundled Chromium instead of its hardcoded Chrome channel.
- Server review fixture records a hunk against an older immutable snapshot after the artifact head changes, recovers an identical lost-acknowledgment retry, rejects altered retry ranges, and rejects that range against a shorter new snapshot. Review does not change the artifact head.

This delivers hunk navigation, collapse and exact version/range review for supported unified Git snapshots. Syntax highlighting, broader specialized/binary previews, generic authorized team-file editing and physical-device acceptance remain outside this increment. The full workbench plan remains active.

Focused review/diff tests, browser fixtures and build passed. Both `go test ./...` and `make test` retained only the documented canary source-hash re-audit failure for unchanged `hermes/authority.go` and `hermes/claude_successor.go`; all other packages passed. No real agent messages, external actions or owner review records were created during fixtures.
