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

The offline mechanical reducer contract below is not a semantic merge. Remaining
blockers are a reviewed semantic merge for oversized inputs, followed by reviewed semantic
comparison and the existing execution, history, ownership and atomic application
gates. The September 16 audit measured context alone above the limit for all
three duties; this phase does not resample the live vault.

## Offline reducer contract v1

`domainextract.ReducerInput` and `cmd/extractor-reducer-check` now provide a
refusal-only semantic reducer with mechanical evidence validation. This does
not make multi-partition planning available, authorize a model turn, lift any
extraction/transaction hold, or establish semantic parity. `PlanContext` still
refuses oversized inputs. There is no live readiness reader for reducer reports.

Run only against an explicitly supplied, separately prepared copied fixture:

```sh
go run ./cmd/extractor-reducer-check \
  -copied-fixture -fixture /tmp/copied-reducer-fixture.json
```

The binary emits redacted JSON to stdout and writes no files. Exit 1 means
invalid evidence; exit 2 means mechanically valid evidence with semantic review
still required (the `go run` wrapper may translate exit status). No success exit
is provided. No config, default vault, operational directory, model, upstream,
approval store or service is opened. Absolute clean paths, regular files and
symlink-free ancestors are required. Input is limited to 16 MiB. Copy attestation
is an operator responsibility: bind mounts and a false attestation cannot be
detected. Do not point the CLI at operational data. This is not a sandbox.

The version 1 input contains:

- `binding`: version 1, reducer `manifest-offline-mechanical`, ritual, complete
  manifest digest, document envelope digest, namespace digest and partition count.
- `manifest`: the complete ContextManifest from the copied snapshot, and
  `records`: exact `{path,text}` records in strictly increasing path order.
  Reader metadata and namespace membership are reconstructed from those bytes
  and compared with the entire manifest. Required records must exist; explicit
  empty RE namespaces remain closed claims about the copied snapshot only.
- `documents`: the complete ordered source envelope, and `documentSha256`: one
  digest per complete Document (name and text), in the same order. OODA names
  must also be `sha256:` plus the raw text digest. Other sources must be clean
  relative Markdown paths outside system/extrinsic records, matching reader
  exclusions; backslashes and line/control separators are refused.
- `partitions`: contiguous zero-based indices, identical binding, sorted unique
  `members`, explicit `candidates` arrays (including empty arrays), and nonempty
  `summary`. Every manifest record must occur exactly once. Each partition with
  the full document envelope must fit the unchanged 56,000-byte Input limit.
- Each candidate contains an `id` and the existing Candidate object. IDs must
  equal existing canonical proposal IDs, be strictly increasing within each
  partition and unique across all partitions. Full-context payload validation
  checks exact source/evidence and existing reference constraints, without the
  production prompt-size check. Production ValidateReply still enforces it.
- `partitionSha256`: one digest of each complete PartitionResult in index order.

All envelope/result digests use lowercase SHA-256 of Go `encoding/json` output
for the declared typed value. ContextManifest uses its existing digest convention
(the SHA256 field is empty while hashing); record hashes cover raw UTF-8 bytes.
Candidate IDs use the existing source ID plus canonical proposal type, apply
path and body, truncated to 12 SHA-256 bytes. Any ID collision refuses even if
candidate bytes match. Typed encoding preserves array order and canonicalizes
payload JSON whitespace, but does not promise arbitrary payload key-order
invariance. Duplicate/case-aliased/unknown JSON keys refuse. Digests bind supplied
evidence, not its authenticity: retain a trusted externally pinned copy/digest.
An internally consistent rewritten fixture cannot prove historical integrity.

Output always has `state: reducer-review-required` and
`refusal: reducerReviewRequired`. Only fully validated fixtures receive
`mechanicalState: reducer-valid-mechanical-only`; others receive `reducer-invalid`
and a fixed reason code. Reports contain counts and digests, never paths,
categories, source text, summaries, candidate bodies or raw error messages.
Candidate-set digests use sorted canonical IDs; reports contain no timestamp.
Hashes permit comparison and are not anonymization against guessing.

Mechanical checks detect identical IDs and normalized kind/title target
collisions (contract candidates collide on apply path). They never combine,
drop, choose or publish candidates. Different titles may still be semantic
duplicates. Target conflicts remain explicit unresolved reasons, preserving the
full input set and count. Every otherwise valid result, including zero candidates,
requires global deduplication and participant/zero-output coverage review.
Closures, heuristic new/reinforce, RE property/work-node matching, money allocation
and target absence carry additional unresolved reason codes. Existing schemas
lack complete participant/output coverage, an explicit global semantic target
identity and a reviewed conflict disposition; therefore no safe semantic merge
or final proposal set is available in v1.

Before any live use, the owner must review immutable source/artifact identities,
complete namespace/context bytes, every partition/result and candidate digest,
complete legacy run/proposal/decision membership and explicit zero-output cases.
Retain candidate-by-candidate dispositions for duplicates, conflicts, explicit
closures with exact original state/title, heuristic reinforcement versus creation,
property/work-node ambiguity, existing contracts, monetary allocations and target
absence. Evidence must cover representative cases in each ritual, participant
recall, and partition/order changes without lost or silently discarded candidates.
A reviewed semantic merge algorithm/version and complete output-set evidence are
still required, as are an isolated no-publication evaluation route and uncertain
execution reconciliation with no automatic replay.

Live blockers remain: oversized input execution has no approved partition/merge
route; subscription completion/spend and historical source/artifact discrepancies
need reconciliation; semantic comparison, deployed ownership-fence evidence and
reviewed handoff receipts remain outstanding. Atomic dependency/write/audit/
approval settlement and crash recovery remain unavailable pending the owner’s
writer/reader coordination decision. Fixture validation lifts none of these gates.
