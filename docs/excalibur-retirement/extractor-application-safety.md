# Extractor application safety — bounded hold, 2026-09-16

This phase adds a **fail-closed application gate**, not an extractor cutover or
semantic-parity receipt. Harness fences and legacy duties are unchanged.

Successor proposals retain an application-owned snapshot of exact source and
context SHA-256 values, bound to the proposal payload, type, ritual and target.
Source/replay identity still excludes context: context drift cannot authorize a
second model execution. Recovered verified jobs without matching snapshot
metadata become uncertain with replay=false rather than publishing unguarded
candidates.

Confirm compares the available vault dependencies and refuses changed or edited
snapshots. **Even matching snapshots remain pending and cannot write:** the
existing apply functions do not support atomic dependency comparison and commit.
The refusal explicitly reports uncertain/stale, pending and replay=false. There
is no retry, fallback, direct vault write or new approval lane. Rejection remains
available. Snapshot metadata survives approval serialization and card edits;
editing payloads invalidates the binding instead of silently refreshing evidence.

OODA successor input uses bounded raw canonical property, contractor and contract
records rather than the legacy portal summary. The source name must be the exact
SHA-256 of the email artifact bytes. Contract payload provenance remains that
same hash. Contract candidates require exact existing canonical property and
contractor slugs and work-node IDs; missing or ambiguous supplied references are
refused. Contractor creation and proposed tree additions are held for a later
phase. Missing directories or an oversized corpus fail closed, without truncation.
The unchanged legacy route still uses its original request format.

## Remaining gates

- Atomic writer CAS over the complete dependency and write set, including added
  records, deleted records, category routing, and category-index ambiguity across
  the entire vault. A preflight followed by independent writes is insufficient.
- Confirm-time artifact-store revalidation and a safe multi-file contract commit.
- Broader property locations, contract tree additions, and explicit property or
  contract references in RE backlog proposals. Current backlog schemas do not
  represent those references; the application hold prevents unsafe acceptance.
- Reconciliation of historical snapshot-less successor proposals already in an
  inbox. They cannot be distinguished from legacy proposals by current metadata;
  existing legacy proposals retain their previous behavior. Do not enable a
  successor against such an inbox without reconciliation.
- Semantic comparison against legacy AION commitment/closure/heuristic and
  RE/OODA outputs, live evidence, ownership handoff, and final decommission.

Offline adversarial tests cover source/context drift, proposal edits, missing and
ambiguous references, invalid nodes/contractors, exact artifact provenance,
unchanged replay identity, and no vault writes on Confirm refusal. Their success
is mechanical evidence only, never semantic parity.

Validation for this phase: gofmt, `go test ./...`, `go build ./...`,
`go vet ./...`, `go test -race ./...`, and `git diff --check` passed locally.
