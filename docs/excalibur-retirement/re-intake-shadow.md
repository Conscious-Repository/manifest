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
    "provider": "deepseek-local", "model": "deepseek-v4.1-flash",
    "tools": ["none"], "mcp": "no_mcp",
    "timeoutSeconds": 120, "maxSteps": 1, "ceilingUsd": 0
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
`hermes.VerifyDutyUsage`. The owner-selected primary is local Sparks provider
`deepseek-local`, exact model `deepseek-v4.1-flash`, tools `["none"]`, MCP
`no_mcp`, timeout at most 120 seconds, exactly one step, cost ceiling USD 0.
These bounds narrow the re-intake declaration; the Phase 1 launcher contract and
its zero-cost/tool-free checks are unchanged. Supplied redacted context grants
no vault access. There is no inherited provider/model or subscription default.
Config wiring remains the existing `hermes.duties` map; deployed config is unchanged.

Structural fixtures prove adapter preservation only, **not live DeepSeek extraction
quality**. Synthetic usage never mints `DutyVerified`. The existing bounded
launcher pins this DeepSeek identity, but a provider-specific primary re-intake
canary and owner review remain prerequisites. No live canary was performed here.
The ordinary Hermes CLI cannot substitute for the strict path.

Claude Code subscription and Codex subscription are named, inert
`hermes.SubscriptionOption` values. `Request.Fallback` is explicit owner input,
not a configured list, scheduler, retry policy or persisted recovery state.
Absent input selects no fallback; a requested fallback with missing, default or
ambiguous input refuses. The input must reference an owner action and complete
primary evidence with `PrimaryResolution=clean-resolved`. Those references are
unverified claims, never proof or an execution grant. Claude's candidate pin is
`claude-sub` / `claude-sonnet-5`; Codex has no verified model/provider receipt
contract, so no executable Codex pin is invented. Every selection still returns
`unsupported/unverified`; only structurally clean, exact Claude input reaches the
future adapter refusal boundary. No subscription adapter exists today.

CLI presence does not establish readiness. A future provider-specific canary must
prove machine-readable exact identity, completion, cost/usage, tool absence, MCP
absence, time/step bounds and no hidden fallback. Subscription payment cannot
stand in for zero-cost evidence. Neither primary errors nor receipt contents
construct a fallback choice. Shadow evidence records the primary choice and
exact declared authority, including refused comparisons; it is never a live run.

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
No byte parity claim is made for LLM prose. Historical source hashes are retained in `sources.json`; the fixture usage now
contains the synthetic DeepSeek primary identity. Its updated fixture hashes do
not change historical source hashes. Claude readiness uses separate synthetic
Claude usage, not a conversion of a live DeepSeek receipt.

## Future cutover record — NOT executed

The pure `CutoverEvidence.Check` helper rejects a missing recorded old-engine
pause, missing snapshot/ownership evidence, queued/running uncertainty or absent
owner comparison approval. Its fields are a checklist, not proof fetched from a
service and not authorization to launch. A passing record cannot enable anything.

Future evidence must record, in order, with timestamps, owner and exact hashes:

1. **Primary DeepSeek canary:** separately authorize a bounded synthetic re-intake
   canary with the exact primary authority above; verify provider-specific receipts
   and extraction semantics. Keep production routing off.
2. **Owner review:** Benjamin reviews canary evidence, structural/semantic limits,
   receipt completeness and the migration implementation before transfer.
3. **Old Excalibur pause/snapshot:** only after that review, separately authorize
   pausing the old ritual, account for all queued/running/manual/spool routes,
   snapshot/hash ritual and proposal inventory, and record single-writer ownership.
   The pure `CutoverEvidence.Check` is only a checklist, not service verification.
4. **Owner-selected primary run:** after the pause/snapshot and any future routing
   implementation are independently approved, explicitly select the exact DeepSeek
   authority for one document and at most one `re-contract` proposal, no portal.
5. **Receipt verification:** preserve and independently verify exact chosen
   authority/provider/model, completion, zero cost/usage, tools/MCP absence, bounds
   and no hidden fallback. Persist complete run evidence before accepting output.
6. **Proposals only after evidence:** only then may a future adapter file the one
   proposal through existing approvals. Confirm retains the existing vaultwriter
   boundary. No dual writers. Owner reviews subsequent proposals before retirement.

Subscription fallback is only a **separately authorized recovery experiment after
an entirely clean, resolved primary outcome and complete evidence**. It additionally
requires its own verified provider-specific canary and adapter, exact owner-selected
identity and independently reviewed primary evidence. None exists as an executable
subscription path today. There is no automatic retry or failover.

Timeout, partial output, missing receipt/usage, nonzero cost, model/provider drift,
tool/MCP claims, provider error, uncertain outcome or crash-after-effect freeze the
lane: preserve evidence, page Benjamin, await explicit action. These states cannot
qualify as clean resolution or trigger subscription fallback. If transfer has
occurred, leave the old engine paused; no automatic re-enable or duplicate writer.
The current shadow stop/page record is local only and sends no notification.

Owner checkpoint now: review this authority decision and adapter-preservation
results, then separately authorize the future primary DeepSeek canary. Live primary
re-intake readiness is unverified; both subscription options are unsupported/unverified.
No cutover, deployment, provider call, Excalibur pause or Phase 4 is authorized here.
