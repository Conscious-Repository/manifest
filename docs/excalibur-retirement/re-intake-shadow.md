# Re-intake: shadow comparison, not routed

Phase 3 stops at owner review of the frozen comparison. `reIntake.shadowEnabled`
is default off and permits only the offline `cmd/re-intake-shadow` command.
There is no production flag, HTTP route, poller subscription, portal projection,
new scheduler or board. Existing intake and delegation keep their current routes.
No engine ritual is paused or added to the retired list by this implementation.

The actual current contract is `re-contract`, validated by
`approvals.ParseReContractPayload`, `ReContractPayload.Validate` and
`ReContractPathAllowed`. `re-backlog` is the separate task/decision grammar in
`approvals/re.go`; substituting it here is refused. Source means the payload's
single CAS document reference, actor means `proposal.agent` (`extractor`), and
target means `proposal.apply-path`. Historical artifacts do not have separate
source/actor/target headers. One document may allocate across multiple properties.
The owner-only proposal has empty proposed content and no team-portal visibility.

## Offline replay

Use a dedicated offline config, never edit the deployed config. Create its
`dataDir` first, outside the vault and harness. Example (disabled intentionally):

```json
{
  "dataDir": "/tmp/owner-chosen-shadow-data",
  "reIntake": {"shadowEnabled": false},
  "hermes": {"duties": {"extractor/re-intake": {
    "provider": "claude-sub", "model": "claude-sonnet-5",
    "tools": ["none"], "mcp": "no_mcp",
    "timeoutSeconds": 120, "maxSteps": 15, "ceilingUsd": 2
  }}}
}
```

With the offline flag explicitly true, replay exactly one embedded fixture:

```
go run ./cmd/re-intake-shadow -config /tmp/owner-chosen-shadow-config.json -fixture single
go run ./cmd/re-intake-shadow -config /tmp/owner-chosen-shadow-config.json -fixture split
```

All output is beneath `<dataDir>/excalibur-retirement/shadow/re-intake/`:
content-hashed comparison receipts, the existing daily run ledger (confined with
`ledger.NewInRoot`), an exclusive invocation lock, and `STOP.json` on uncertainty.
No production ledger is opened. Linux directory descriptors and `os.Root`
confine writes; symlink ancestors are refused. Existing receipts are never
rewritten. Replaying an existing receipt or finding an interrupted lock refuses.
A stop record blocks subsequent replay. Preserve that directory for owner review;
an owner may explicitly choose a fresh offline dataDir for a corrected comparison.
The tool never deletes a stop record or automatically retries. Failure to persist
evidence is itself a stop-and-page error on stderr/nonzero exit.

The declaration check and strict usage parser reuse `hermes.DutyAuthority` and
`hermes.VerifyDutyUsage`. The pinned model stays `claude-sonnet-5`. Replay has no
provider/runner handle, executable tool registry, MCP, profile, or fallback.
Tools `[none]` represent supplied redacted context, not legacy vault access.
Timeout 120 seconds is an explicit offline test declaration, not an inferred
historical runtime limit. The ritual's 15-step / $2 ceilings remain ceilings.

**Live Claude execution is unsupported and refused by the existing runner.**
Synthetic fixture usage does not acquire its private `DutyVerified` receipt.
Passing this comparison is not evidence of bounded Claude execution, trustworthy
live usage, current provider readiness, or fresh LLM extraction quality. The
ordinary Hermes CLI and its default tools cannot substitute for the strict path.

## Fixture evidence

`reintake/fixtures/sources.json` records exact read-only historical source paths
and hashes. Two linked completed run/approved-proposal pairs cover one property
and two properties, allocation arithmetic, tasks/decisions, milestones, terms
and risks. The source CAS occurs in each run and each proposal ID is linked from
its run. Approved payloads may include owner edits; they are not certified as
unaltered initial engine output.

Only the field topology, enum/boolean values and relationships survive redaction.
Identifiers become fixture symbols; CAS references are synthetic hashes; dates
and amounts are synthetic (100 per allocation). Original prose, names, addresses,
message bodies, credentials and financial data are not copied into tests.
`source`, `actor`, `target`, `applyPath`, type and non-prose payload values compare
against the frozen expectation. The expectation and input share a redacted
historical source: this tests adapter preservation, not independent extraction.
Name, summary, allocation reason, task text, terms, exclusions and risk prose are
semantic-review-only; list cardinalities and structural references still compare.
Milestone names are redacted node identity symbols for relationship comparison.
No byte parity claim is made for LLM prose. Historical run model is retained;
provider/completion/cost/step evidence in the replay is explicitly synthetic.

## Future cutover record — NOT executed

The pure `CutoverEvidence.Check` helper rejects a missing recorded old-engine
pause, missing snapshot/ownership evidence, queued/running uncertainty or absent
owner comparison approval. Its fields are a checklist, not proof fetched from a
service and not authorization to launch. A passing record cannot enable anything.

Future evidence must record, in order, with timestamps, owner and exact hashes:

1. Manifest production flag **off**; reviewed shadow comparison and live-runner
   implementation/verification prerequisites approved separately.
2. Pause the old `extractor/re-intake` engine ritual. Verify the pause and refuse
   all old manual/delegation/spool routes; account for queued and running work.
   Do not proceed while its authority or in-flight outcome is uncertain.
3. Snapshot the ritual, pending/decided proposal identity inventory and ownership
   state. Record snapshot hashes, pause evidence, no queued/running work and the
   single authority transfer. No connector cursor is involved in this lane.
4. Only after that record is checked may an owner enable a **future** Manifest
   production flag. This patch does not implement that flag or enable command.
5. Run one owner-triggered document through the existing Manifest dispatch/runner.
   Independently verify bounded Claude execution, exact model/provider/tools,
   completion, steps, time and actual spend; persist run evidence **before**
   accepting output. Only then may the future adapter call `approvals.Propose`.
   Confirm remains the existing approvals/vaultwriter boundary. Never dual-write.
6. Owner reviews roughly three days of proposals and usage evidence before any
   later retirement work. Team portal remains excluded.

On any uncertainty: flag off, leave old engine paused, preserve evidence, page
owner, await explicit action. No automatic fallback, replay, proposal filing or
old-engine re-enable. The page instruction here is a local error/evidence marker;
this shadow implementation sends no message and publishes no production signal.
Re-enabling the engine is a separate owner decision after duplicate checks.

Owner checkpoint now: review fixture comparison, confirm `re-contract` alignment,
review semantic-only limits and the unsupported Claude execution prerequisite.
No cutover, deployment, provider call or Phase 4 is authorized by this document.
