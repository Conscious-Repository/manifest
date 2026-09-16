# Copied extractor evaluation evidence v1

`cmd/extractor-evaluate` is a no-write, refusal-only evidence validator for
owner-supplied copied JSON. No duty is enabled or migrated. Live state remains
`comparison-unrun`. It never calls a model, upstream, Submit, Start, polling,
approval publication, vaultwriter, ownership or a service. It does not discover
configuration, a vault, historical records or operational directories.

```sh
go run ./cmd/extractor-evaluate -copied-fixture -fixture /tmp/copied-evaluation.json
```

The command opens only that explicitly supplied file, requires an absolute clean
path, refuses symlinks in every component and nonregular files, and reads at most
16 MiB. It writes redacted JSON to stdout and no files. Exit 1 denotes rejected
evidence; exit 2 denotes internally consistent copied claims with comparison still
refused. There is no success exit. Flag and read errors never print raw errors.
The copy assertion is the operator's responsibility, not a sandbox: false
attestations, hard links and bind mounts cannot establish isolation. Prepare an
immutable copy separately; never supply an operational path.

## Fixture contract

The exported `domainextract.EvaluationFixture` defines the exact JSON schema:

- `version`: 1.
- `successor`: the complete `ReducerInput` described in
  [context planning](extractor-context-planning.md), including ritual, exact
  documents/artifact identities, full context manifest and records, namespace
  membership, partition bindings, candidates and all digests. The existing
  mechanical reducer validates it without changing any production limits.
- `legacy`: a nonempty array of `EvaluationRun`. Each retains exact copied native
  Markdown `raw`, raw-byte `sha256`, `snapshotSha256` of the successor's complete
  typed binding, ordered `proposalIds`, an explicit `proposals` array and an
  explicit `zeroOutput` claim. Each proposal carries the existing
  `approvals.Proposal` schema, its typed JSON `sha256`, an owner-transcribed
  `Candidate`, and `decision` equal to pending, approved or rejected status.
  IDs must match ordered membership exactly, with no duplicate runs/proposals.
  Shared/deduplicated historical proposals across runs currently refuse; they
  require an adapter that retains explicit per-run dispositions.
- `attestation`: `evidenceSha256`, `externallyPinned`, `completeHistory`,
  `completeParticipants`, `completeContext`, `immutableSources`,
  `successorZeroOutput`, and `ownerReview` (`pending` or `reviewed`). Missing
  claims add fixed unresolved dimensions; absent/invalid owner status refuses.
  Zero-output assertions are needed for each empty legacy run and for an empty
  successor set. Positive output with a zero-output claim refuses.

Digests use lowercase SHA-256 of Go encoding/json typed values, except native
run reports use exact raw UTF-8 bytes. `EvaluationEvidenceDigest` hashes a typed
object with version, successor and legacy, excluding attestations to avoid a
self-reference. Retain the digest **and all attestation claims** in a separately
trusted owner receipt. The CLI verifies consistency with the declared digest;
it cannot authenticate the issuer or detect an entirely rewritten fixture and
receipt. SHA equality is not semantic proof or anonymization against guessing.

## Why comparison still refuses

The inspected native runreport schema records run metadata, textual decision
trail and writes; it has no complete output/zero-output membership contract.
Legacy filing uses SHA-1 of lowercased action/body and can deduplicate across
pending, approved and rejected records. It does not bind proposal IDs to a run,
immutable source bytes or complete historical context. Current proposal status
also cannot establish the full historical decision sequence.

The conservative validator checks completed run metadata, exact report digests,
explicit supplied membership, native proposal IDs/digests, ritual/type/path,
decision status, and a single four-backtick payload fence. Transcribed payloads
must normalize to the same existing typed payload; original and transcribed JSON
are strictly checked. Existing candidate validation checks source/quote,
participants, references and payload shape against the supplied complete copy.
Snapshots from another binding, changed evidence, ambiguous fences, invalid
candidates and candidate-ID collisions refuse. Native trimmed/edited records
whose original IDs cannot be reproduced also refuse. Run frontmatter checks
are only a metadata projection, not validation of its free-form decision trail.
The owner-supplied snapshot link is an assertion, not native historical proof.

Consequently v1 implements the explicitly permitted refusal-only fallback.
It does **not** report cross-system exact matches, omissions, additions or target
conflict sets: no reviewed lossless native-history adapter is available. It does
not label zero comparison counts as an empty comparison. The reducer still
checks successor identities and flags normalized target conflicts without
resolving them. Different titles may describe the same obligation; identical
candidate bytes may still omit participants or misinterpret evidence. Every
report retains `evaluationIncomplete`, `semanticReviewRequired` and
`comparison-unrun`, including byte-identical, owner-reviewed fixtures.

Reports contain only fixed classifications/reasons, counts and digests. They
never return run IDs, paths, names, bodies, prose, timestamps, summaries,
credentials, payloads or raw errors. No automatic replay, resolution, winner
selection or publication is possible.

## Minimum evidence and remaining blockers

Before meaningful comparison, the owner must independently pin source-store and
artifact identities, exact documents, the complete historical context/namespace,
all runs (including interrupted/uncertain runs), every emitted or deduplicated
proposal and its decision history, explicit per-run zero-output dispositions,
and complete participant coverage. Versioned owner-reviewed native adapters
must preserve those links and all candidate semantics. This version accepts only
completed-run claims and does not reconcile other outcomes automatically.

Retain candidate-by-candidate review for commitments, omissions/additions,
duplicates/conflicts, explicit closures and original state, heuristic creation
versus reinforcement, property/work-node matching, contracts, money allocations
and target absence. Review representative cases per ritual, partition/order
changes, and one-turn versus legacy multi-step behavior. A review status flag
alone is not that evidence. No actual owner fixture or semantic study was run
in this implementation; tests use synthetic copied evidence.

Live blockers remain the unapproved oversized-context execution/merge route,
uncertain subscription completion/spend, historical missing sources and email
artifact discrepancies, complete semantic comparison, deployed fence evidence
and reviewed handoff receipts. Atomic dependency/write/audit/approval settlement
and crash recovery remain unavailable pending the owner's writer/reader
coordination decision. This offline validator lifts none of those holds.
