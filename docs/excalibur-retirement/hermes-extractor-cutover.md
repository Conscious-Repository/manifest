# Hermes extractor cutover

Hermes is the runtime for Manifest's `extractor/aion`, `extractor/ooda-email`
and `extractor/real-estate` duties. Operational migration is complete once the
Manifest service, enabled rituals, shared ownership fences and HTTP health pass
verification. This does not assert semantic parity: every semantic proposal still
passes the existing candidate/source validation and human approval contract.

The runner invokes `/home/benjamin/.local/bin/hermes` by argv with `chat -Q`,
`-m sparks --provider lab-sparks --safe-mode -t none --max-turns 1`. Each call has
a private `HERMES_HOME` and working directory, no inherited credentials, MCP,
rules, tools or fallback chain, a maximum 120-second timeout and bounded output.
The private config pins the alias and endpoint. Because the installed chat path
does not expand the alias before submission, Hermes' native provider `extra_body`
setting pins the wire model to `deepseek-v4.1-flash` and `tool_choice` to `none`.
No custom HTTP transport or tokenizer metadata is involved. Only two known CLI
startup warnings may precede the required candidate JSON.

Default authority preserves the existing ceilings (USD 4 for AION/real-estate,
USD 2 for OODA email) and one model step. Local compute is recorded under
`local-zero-marginal`; monetary telemetry is unavailable, not measured zero.
Explicit incompatible duty settings refuse. Re-intake keeps its separate executor.

Enable the three `domainExtraction` flags in the deployed Manifest configuration
and the three harness ritual files only after tests pass. Transfer each current
blocked fence with the existing `excalibur-engine dispatch-owner` command, using
the inspected revision, `blocked manifest`, and this reviewed document's SHA256.
The command preserves history and rejects revision/owner/hash format drift.
Restart Manifest with the new binary, then independently check all three duties
using `cmd/extractor-check -enabled`, the ritual metadata, HTTP and systemd.
Excalibur stays inactive and masked; connector services stay active.

The shared fence remains locked through model execution and candidate publication.
No additional semantic handoff receipt is required. Historical receipts remain
readable; old queues/state remain preserved. Stale ownership revisions and uncertain
jobs remain quarantined, with no automatic replay or fallback. No extraction is
performed as a coding or cutover test.
