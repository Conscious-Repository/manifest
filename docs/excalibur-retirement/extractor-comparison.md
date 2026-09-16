# Offline extractor comparison: refusal-only checkpoint

`cmd/extractor-compare` verifies explicitly supplied artifact hashes and writes a
private, durable refusal report outside the vault. **No structural or semantic
parity is established, including for byte-identical inputs.** This implements the
refusal-only fallback: the current formats cannot support the requested safe
comparison. No live comparison receipts or model outputs were generated.

The checker has no model, upstream, approval-store, vault-writer, configuration,
ledger or ownership dependency. It reads only named staged files and filesystem
metadata; its sole write is a new `report.json`. It never retries extraction.

## Evidence gaps established by inspection

- Legacy `spirits/extractor/rituals/{aion,real-estate,ooda-email}.md` and
  `engine/internal/casts/approval.go` in the read-only Excalibur checkout publish
  prose and fenced payloads. Proposal IDs deduplicate action/body; they do not
  bind a complete run output set, immutable source snapshot or replay disposition.
  Run summaries (`spirits/spirits.go`) cannot prove proposal membership or that
  an absent output represents a completed zero-candidate extraction. Local legacy
  run/approval inspection found no extraction-snapshot field; this is a local
  inventory observation, not a live service receipt.
- `domainextract.Input`, `Response`, `Job` and `approvals.ExtractionSnapshot` bind
  some successor source/context bytes, but V1 has no complete category namespace,
  expected-absence predicates or artifact-store identity. A SHA-256 of a supplied
  file proves its bytes only; it does not prove its claimed source or completeness.
- `aion.ProposalPayload` (also RE backlog/resolve) has no explicit property/contract
  references. Resolve and heuristic matching use private titles/statements.
  Comparing those as prose or guessing reference identity is out of scope.
- `approvals.ReContractPayload` has allocation property/node identifiers, but the
  apply code can select a suffixed destination and perform multiple independent
  writes. There is no complete pure rendered write-set artifact. Proposed note
  metadata, categories, target absence and namespace uniqueness are not bound
  across both paths. See [application safety](extractor-application-safety.md).

To enable comparison, retain immutable source-store identities and bytes; complete
run-to-output membership (including explicit zero-output receipts); explicit
replay dispositions; a versioned category/reference namespace snapshot with
property, contract and node IDs and absent targets; proposed note metadata; and
pure rendered write sets with target, operation, capability and before/after
hashes. Bind all of these to each run/output, then implement strict native-format
adapters. No adapter may treat declarations alone as verified evidence.

## Running the checker

Prepare a **copy** of each owner-selected native evidence artifact outside the
vault. The tool does not discover, copy, normalize or manufacture evidence.
Use a private directory named `/var/tmp/extractor-comparison-<64 lowercase hex>`
with mode 0700. Choose a random opaque suffix. Supply the actual vault root via
`-vault`; it is used only for exclusion checks. Neither directory may contain the
other. Symlinked staging paths and artifact symlinks are refused.

Name each copy `legacy-<its SHA-256>.artifact` or
`successor-<its SHA-256>.artifact`. Compute digests from exact original bytes.
The report retains exact staged paths and both sets of hashes. Native original
paths remain in private evidence if needed; they are never printed in reports.
This restrictive namespace avoids exposing people, addresses or secrets embedded
in filenames. No probabilistic PII detector is relied on. Report strings are
fixed vocabulary, supported duties, opaque paths and verified digests only.

```sh
go run ./cmd/extractor-compare \
  -vault "$VAULT_ROOT" -ritual aion \
  -evidence-dir "$STAGING_DIRECTORY" \
  -legacy "$LEGACY_SHA256" -successor "$SUCCESSOR_SHA256"
```

Repeat each hash flag for additional artifacts (maximum 64 per side, each nonempty
regular file at most 8 MiB). Duplicate hashes within a side refuse as ambiguous.
Identical bytes across sides still refuse comparison. Artifact contents are
hashed, never parsed, compared as prose, or printed. Unsupported types, changed
sources/categories/targets, invalid references, replay=true and forged manifests
cannot get a passing result because **all formats refuse comparison**.

Exit 1 means `report.json` was exclusively created with mode 0600 and both file
and directory synced, recording verified input hashes and missing evidence.
Exit 2 means input/storage refusal; no completed report is promised. A storage
failure can leave an uncertain file; inspect it privately and choose a fresh
staging directory. Existing reports are never overwritten. `go run` wraps the
program's exit status; use a built binary when automating status handling.
Archive the staging directory in owner-controlled storage for long-term retention;
`/var/tmp` is outside the vault but may be subject to host cleanup policy.

## Retirement reporting contract

`comparisonReadiness` is independent of ownership, application safety and
migration. This version only emits `comparison-unrun`, with `status: refused`,
`structuralComparisonPerformed: false`, `semanticValidationPerformed: false`,
`migrated: false`, and `replay: false`. The latter describes checker behavior,
not validation of either run's replay flag. `inputHashesVerified` does not mean
source binding or structural comparison succeeded.

The retirement UI can reference this document and retained reports. Missing,
invalid, incomplete or refusal reports map to `comparison-unrun`. Reserve
`structural-diff` for a future complete validated structural comparison with
differences, and `structurally-matched; semantic-review-required` for a complete
validated match. Neither future status may mark a duty migrated or establish
semantic parity. This checker cannot emit either status. Ownership and existing
application holds remain unchanged.

Adversarial unit tests use synthetic byte strings only to prove refusal,
redaction, hash binding, path confinement and non-overwrite behavior. They are
not operational fixtures or evidence of extraction quality.
