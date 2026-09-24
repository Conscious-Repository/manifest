# Multiple reviewed context versions — September 24

Private chat composers now retain an ordered set of up to eight exact artifact
versions. Records and prior outputs add to that set; selecting the same ID and
revision twice does not duplicate it. Each entry opens its exact version and can
be removed independently. Two revisions of one artifact may be selected together.
Legacy single-reference drafts remain readable. The task-panel composer retains
its existing single-reference contract.

Synced drafts, all private send paths, related-chat setup and side-chat setup carry
the full set. Child drafts restore every handed-over version. Unlinked selections
still require explicit private-context consent; child origins grant only the exact
handed-over versions. Conflicting task-scoped additions are refused with guidance
to explicitly select the additional file from registered files. Shared authority
and the existing server limits (eight references, 64,000 bytes per text artifact,
96,000-byte combined context check including preceding wrappers) are unchanged.

Evidence:

- `TestChatContextSelectionUI`: legacy drafts, ordered additions, duplicate consent
  upgrade, simultaneous versions, atomic mixed-task and ninth-reference rejection.
- `TestNativeMultipleContextVersions`: three exact references after their heads
  change; one missing revision rejects the whole input before fake provider
  execution; durable receipt, restart/retry without replay, changed-set conflict.
- `TestExplicitRelatedArtifactHandoff`: two-reference origins for planning, Codex
  and Claude drafts; both exact bodies remain scoped, newer versions are refused,
  creation retry recovers the same child, and planning child delivery succeeds.
- Chromium workspace fixture: three chips, independent removal, synced draft
  recovery, all child-origin references, frozen side handoff, lost creation reply
  and exact retry after restoration. 320/390/1440px checks include no horizontal
  overflow within the selected list. Phone screenshot inspected.
- Chromium Records fixture, focused Go checks, JS syntax, release build and diff
  checks pass. `make test` passes server (41.836s) and other packages except
  the known `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` re-audit
  failure for unchanged Hermes source files.

Provider delivery uses isolated fixtures. This closes additive private composer
selection, not the remaining record types, automatic output registration, or full
integrated/live-provider/physical-device acceptance.
