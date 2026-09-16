# Offline extractor context planning

The context planner is a read-only diagnostic for explicitly copied fixtures.
It does not change ReadInput, Submit, polling, model execution, approval
publication, snapshots, ownership, or vaultwriter. All successor activation and
application holds remain in force. No successor is enabled or migrated.

Run against a quiescent copy prepared separately by the operator:

```sh
go run ./cmd/extractor-context-plan \
  -copied-fixture -fixture-root /tmp/extractor-copy \
  -excluded-vault /absolute/vault-boundary \
  -ritual aion -budget 56000 -report /tmp/extractor-context-report.json
```

All paths must be explicit, absolute and clean. The excluded vault boundary is
only compared lexically; it is never opened or discovered from configuration.
The operator must supply the actual boundary and attest that the fixture is a
copy, not operational state. Ancestor/component symlinks are refused, and report
creation is exclusive, mode 0600, with file and directory sync. The output must
be outside both fixture and vault. A false copy attestation or bind mount cannot
be detected by this interface; it is not a sandbox for adversarial operators.
No default vault read, model call, service start, or approval write exists.

The CLI measures context with a null document envelope: serializedBytes is an
exact measurement of that envelope, a lower bound on a real request, **not**
source readiness. It always refuses a usable partition until exact sources are
provided. The library PlanContext accepts exact Documents for full Input JSON
measurement, including escaping, names, source text and envelope overhead. A
budget must be positive and at most the unchanged 56,000-byte production limit.
The prompt instructions are outside this existing serialized Input limit.

| State | Meaning |
| --- | --- |
| context-too-large | Complete serialized input (or CLI lower bound) exceeds the hard budget. |
| partition-planned | One complete source/context input fits; every context record occurs exactly once. No multi-partition plans are produced. |
| partition-unavailable | contextPartitionUnavailable; source envelope and/or required merge semantics are unavailable. No partial partitions are returned. |
| semantic-review-required | Always applies; byte fit is not semantic equivalence, execution authority, application safety or migration. |

The ritual UI keeps ownership separate and labels context unmeasured. Its
planning-state explanation does not import fixture results as live readiness.
The offline report is the planning integration; no live reader was added.

## Manifest and namespace scope

ReadContextManifest includes exact relative paths, raw SHA-256, raw byte sizes,
domain, reader category and declared frontmatter categories in stable path order.
It reads all three AION records, or RE backlog/people and every recursive exact
`.md` member of properties, contractors and contracts, without an early size stop.
Required missing files/directories, duplicate paths, changed manifest bytes,
nonregular records, path escapes and symlinks refuse. Empty collections are
explicit closed membership facts. A path not in the complete selected collection
is expected absent in that **copied snapshot**; missing collections never mean
empty. Non-Markdown sidecars are outside today's ReadInput selection contract.

Membership and category evidence preserve case-folded slug ambiguity, including
nested duplicates. Dependencies conservatively include all records; no text
search claims semantic independence. Whole property bytes preserve work-node
ambiguity. Existing contracts and closed membership preserve target-absence and
deduplication evidence. The manifest does not invent a category-index generation,
artifact-store identity, or live absence/CAS proof. It covers the current input
reader, not every dependency of future application. It is not a replacement for
ExtractionSnapshot or Job provenance. Input.ID still excludes context; planning
cannot authorize reruns after context drift.

The private manifest holds paths and categories; the durable CLI report contains
only fixed status/reason codes, counts, hashes and sizes. It omits source prose,
filenames, category values and model outputs. The manifest digest binds exact
paths, content hashes, metadata and namespace membership; record path hashes
allow comparison without printing names. Hashes are comparison identifiers, not
anonymization against guessing. Reports have no timestamp, so unchanged copies
produce identical bytes. A changing fixture is not a globally atomic snapshot;
use an immutable/quiescent copy. Nothing here claims coordinated live reads.

## Minimum future merge contract — still unavailable

Oversized context refuses with the following explicit unresolved requirements:

- Global candidate identity, deduplication and conflict resolution across all
  records and sources, including proposed duplicates between partitions.
- Complete participant, proposal-set and explicit zero-output coverage; no lost
  records, silent truncation, summaries or selective dropping of older context.
- Explicit closure reconciliation against exact original titles and state, and
  heuristic new-versus-reinforce reconciliation against the complete corpus.
- Closed category/slug namespaces, property/work-node ambiguity, existing
  contracts, and target absence validated against the whole snapshot.
- Exact source/artifact identity and verbatim evidence binding for every output.
- Versioned partition membership and reducer identity bound to the whole source,
  context and namespace snapshot, with deterministic final proposal identities.
- Uncertain/partial execution recovery with no automatic replay or publication;
  a reducer result cannot bypass approval or the separate atomic-commit hold.

No reducer is implemented or claimed semantically equivalent. Remaining blockers
are this merge contract for oversized inputs, followed by reviewed semantic
comparison and the existing execution, history, ownership and atomic application
gates. The September 16 audit measured context alone above the limit for all
three duties; this phase does not resample the live vault.
