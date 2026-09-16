# AION successor readiness: capacity refusal

This is the first AION successor seam, **not activation or completed migration**.
`cmd/aion-successor` is read-only, has no inference transport, and always exits 1.
It does not load Manifest runtime configuration, start services, publish approvals,
change fences/rituals, or replay jobs. Excalibur remains retired and the three
extractor projections remain paused. No live vault read or inference was performed
for this implementation.

The explicit [config](aion-successor-config.json) pins owner-approved `lab-sparks`,
`deepseek-v4.1-flash`, the existing fixed endpoint, no tools/MCP/fallback, and
unavailable cost telemetry. `lab-sparks` names the deployment; the older Hermes
adapter uses `deepseek-local`. This checker does not silently alias or route
between them. The production extraction guard still accepts its historical
subscription authority only; it is not used by this command.

Capacity is **unknown**, represented by zero, not unlimited. Inspection found no
verified deployed context-window, tokenizer/chat-template accounting, output
reservation or endpoint request-byte budget. A caller-entered nonzero capacity
cannot self-certify these facts and still refuses. A declared byte limit below
the measured envelope gets `complete-input-exceeds-declared-capacity`; otherwise
the reason is `provider-capacity-unverified`.

The earlier September 16 audit measured AION context alone at 197,659 raw bytes
(not remeasured live here). Existing `Input.Validate` caps serialized input at
56,000 bytes; `hermes.runSuccessor` caps prompts at 64,000 bytes and the Python
helper reads a maximum 100,001-byte invocation packet, with 4,096 output tokens.
These application limits are not verified provider capacity. None was raised.
A no-write model canary **cannot safely run through this seam now**.

## Exact next diagnostic command

Prepare an owner-selected immutable copy containing all three canonical files
(`system/aion/{backlog,people,heuristics}.md`) and 1–4 selected source notes.
Missing required files refuse; empty files remain present with their exact hashes.
The manifest retains every record, categories, and namespace facts. AION's current
schema has no recursive namespace or absent-target rule; the empty namespace
list is preserved, not presented as proof about unenumerated files.

From the repository root, with `FIXTURE_ROOT` an absolute copied directory,
`VAULT_ROOT` the absolute real vault boundary, and `SOURCE_NOTE` a copied relative
Markdown path:

```sh
go run ./cmd/aion-successor \
  -config docs/excalibur-retirement/aion-successor-config.json \
  -fixture-root "$FIXTURE_ROOT" -copied-fixture \
  -vault-root "$VAULT_ROOT" -source "$SOURCE_NOTE"
```

Repeat `-source` for additional notes. The vault boundary is never opened in copy
mode. Repeat with `-expected-input-sha256 <inputSha256 from report>` to reject
changes from the measured snapshot. Optional separately authorized live reading
replaces `-fixture-root ... -copied-fixture` with `-live-read -no-write`; it still
only measures and refuses. **Do not interpret exit 1 as a model attempt.**
`go run` also prints its wrapper exit status; build a binary for automation.

The command emits redacted JSON to stdout and fixed refusal codes to stderr; it
opens no output file. Caller-managed redirection must remain outside vault and
operational state. Source/context symlinks, invalid UTF-8, duplicate or unsafe
source paths, and files above 8 MiB refuse. Double reads and an optional previous
hash detect observed drift, not concurrent-edit ABA or global filesystem snapshot
consistency. Immutable copies remain the preferred evidence boundary.

`serializedBytes` measures the exact JSON containing the complete manifest and
source/context envelope, including escaping; `legacyInputBytes` measures the old
Input form. Neither includes a future prompt or HTTP/chat envelope. No request is
constructed: request/result hashes are absent and usage is `not-invoked`, never
fabricated zero usage. Input, manifest, sources and provider configuration have
separate digests. `CheckCopiedReply` is a pure offline strict candidate validator;
its digest binds input, provider config and reply bytes. It emits only a count and
digest, never candidates for publication or a provider execution receipt. Shape
validation does not establish semantic parity, participant coverage or provenance.

## Required before canary and activation

First obtain owner-reviewed evidence of this exact deployed model/endpoint's
actual capacity and tokenizer/chat-template accounting, including output reserve.
Implement and test a bounded transport whose **exact serialized request**, full
input, endpoint/model, raw response, usage, finish state and sanitized error
receipt are bound together. Reconcile the adapter limits with that verified
capacity; reject drift and overflow before network I/O. No fallback or truncation.
Only then run an explicitly authorized no-write canary and retain its immutable
private request/result artifacts; a refusal report is not that receipt.

Before AION activation, an owner review receipt must independently pin the code
revision, config/capacity evidence, complete input/manifest/source hashes, exact
request/result/usage/error evidence and selected-source identity. Review must
cover every participant, deduplication, closures, heuristic new/reinforce decisions,
zero-output cases and unresolved candidates. Include review identity/time and an
explicit disposition. Mechanical validity alone is insufficient. The existing
history/fence/live-semantic handoff evidence, replay disposition and application
safety requirements still apply; this tool cannot create the operational receipt,
transfer ownership, resolve candidates or approve proposals.

OODA-email still needs complete RE context capacity, immutable email artifact
identity and money/allocation/reference review. Real-estate still needs complete
property/contractor/contract namespaces, matching/closure and absence evidence.
Both also need their own no-write canary, historical reconciliation, owner review
and application/handoff safety work. AION progress does not activate either lane.
