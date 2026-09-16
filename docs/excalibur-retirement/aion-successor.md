# AION Sparks no-write transport

The successor now has an optional fixed-local OpenAI-compatible canary transport.
Excalibur remains retired; AION, OODA-email and real-estate projections remain
paused and `comparison-unrun`. This command does not load application runtime
configuration, publish proposals/approvals, write vault, change fences, transfer
ownership or replay jobs. No real live canary was run for this implementation.

The existing `aion-successor-config.json` pins `lab-sparks`,
`deepseek-v4.1-flash`, `http://192.168.87.11:8000/v1`, no tools, no MCP and no
fallback. Historical Claude/subscription 56,000-byte input and 64,000-byte helper
limits remain unchanged. Only this separately capacity-checked transport bypasses
them. No proxy, redirects, credentials, streaming, retries or alternate provider
are used. Overall timeout is 120 seconds; response headers are bounded to 30
seconds and response bodies to 1 MiB. Unexpected response fields fail closed.

## Evidence before invocation

An explicitly saved `/v1/models` response must contain exactly one matching model,
owned by vLLM, with positive `max_model_len`. The observed 1,048,576-token window
is discovery evidence, **not proof that a 1 MiB input fits**. The loader hashes
all discovery bytes. The fixed provider configuration binds their interpretation;
it cannot authenticate where an externally supplied file came from.

First measure a copied fixture (all three canonical context files plus 1–4 source
notes), using absolute root paths and a saved discovery response:

```sh
go run ./cmd/aion-successor \
  -config docs/excalibur-retirement/aion-successor-config.json \
  -fixture-root "$FIXTURE_ROOT" -copied-fixture -vault-root "$VAULT_ROOT" \
  -source "$SOURCE_NOTE" -models "$MODELS_JSON" -reserved-output 4096
```

Without `-models`, historical measurement-only refusal remains available.
Measurement with models reports input, capability and exact request digests plus
request bytes, and `tokenizer-template-accounting-required`. It performs no HTTP.
The request includes extraction instructions, complete source/context input and
its manifest, JSON escaping, chat envelope and completion reservation. The library
`AionInput.SparksRequest` returns those exact private bytes for external accounting;
the CLI intentionally prints only their digest and size.

Obtain an **independently owner-reviewed bounded tokenizer/template probe** of
that exact request at the deployed revision. Store this accounting JSON privately:

```json
{
  "requestSha256": "<measurement request digest>",
  "capabilitySha256": "<measurement capability digest>",
  "tokenizerSha256": "<deployed tokenizer artifact digest>",
  "templateSha256": "<deployed chat template artifact digest>",
  "probeSha256": "<retained bounded probe evidence digest>",
  "serializedTokens": 0,
  "promptTokens": 0,
  "tokenMargin": 256,
  "byteMargin": 1024,
  "verifiedRequestBytes": 0,
  "reservedOutput": 4096
}
```

Zero values are placeholders and refuse. `serializedTokens` measures the entire
serialized JSON request with the identified tokenizer; `promptTokens` measures
the actual rendered chat template. Both plus output reserve and token margin
must fit the discovered window. `verifiedRequestBytes` is the probe-verified
serialized HTTP request byte bound, not a context-window declaration. Admission
uses the smaller of that bound minus byte margin and one byte per remaining
context token. This byte ceiling is an additional conservative constraint and
never substitutes for either measured token count. Token margin must be at least
128; byte margin at least 1024. Larger inputs require new exact-request evidence.

The command checks evidence bindings and an independently supplied SHA-256 of the
accounting file; it does **not** run a tokenizer, verify a probe's contents, or
cryptographically authenticate the reviewer. A fabricated attestation is not
capacity evidence. Retain tokenizer/template artifacts, discovery provenance,
probe inputs/results, revision, reviewer identity and time outside this tool.
Unknown accounting remains refused; do not fill counts from byte estimates.

## Explicit canary command

After the above evidence has been reviewed, a real canary can be run using:

```sh
go run ./cmd/aion-successor \
  -config docs/excalibur-retirement/aion-successor-config.json \
  -live-read -no-write -vault-root "$VAULT_ROOT" -source "$SOURCE_NOTE" \
  -expected-input-sha256 "$INPUT_SHA256" \
  -models "$MODELS_JSON" -reserved-output 4096 \
  -accounting "$ACCOUNTING_JSON" -accounting-sha256 "$ACCOUNTING_SHA256" \
  -canary -receipt "$PRIVATE_RECEIPT_DIR/aion-canary.json"
```

The existing receipt directory must have mode 0700, with no symlink components,
and lie outside vault and input. The new receipt has mode 0600 and must not exist.
Use a dedicated evidence directory, never application state. Copied fixture mode
can replace `-live-read` with `-fixture-root ... -copied-fixture`; invocation still
requires `-no-write`. Source/context symlinks refuse. Double reads and input pins
detect observed drift, not concurrent-edit ABA; immutable copies are preferred.

Receipts bind input/config/capability/accounting, exact request and raw response
hashes, validated usage, finish state, candidate count and sanitized error code.
They contain no private prompt, source paths, response prose or credentials.
The receipt digest covers every other field. Interrupted runs may leave an empty
reserved receipt; that is not invocation evidence. HTTP errors retain only a body
hash and fixed error code. Partial/oversized bodies are refused without presenting
a partial hash as a complete response. Model mismatch, tools, non-JSON, detected
secrets, invalid candidate evidence, incomplete finish, or inconsistent usage
refuse. Usage must match the measured rendered prompt count exactly. Candidate
validation happens only in memory; nothing is published. A successful canary
still reports `comparison-unrun`, not semantic equivalence or activation.

## Remaining activation work

A real canary is now executable **only with the verified evidence above**. The
reported model window alone is insufficient; no reviewed tokenizer/template
accounting is supplied by this change.

Before activation obtain an owner-reviewed canary receipt that pins code revision,
config/capacity/probe evidence, complete input/manifest/source identity, exact
request/response hashes, usage and errors. Review every participant, deduplication,
closures, heuristic new/reinforce decisions, zero-output cases and unresolved
candidates, with reviewer identity/time and explicit disposition. Retain private
raw evidence separately if needed for semantic comparison. Complete historical
reconciliation, legacy-history/fence/live-semantic handoff evidence, explicit
replay disposition and application safety review. This tool grants no authority.

OODA-email still needs complete RE context capacity, immutable email artifact
identity and money/allocation/reference review. Real-estate still needs complete
property/contractor/contract namespaces, matching/closure and absence evidence.
Each needs its own transport admission, no-write canary, historical reconciliation,
owner review and application/handoff safety work. AION does not activate them.

## Bounded accounting inspection

The accounting probe is a separate refusal receipt, **not** a canary accounting
attestation. Run it against an explicitly supplied immutable copy and saved model
discovery. All paths below must already exist except the new receipt file; the
receipt directory must be private (0700), outside both input and vault:

```sh
go run ./cmd/aion-successor \
  -config docs/excalibur-retirement/aion-successor-config.json \
  -fixture-root "$FIXTURE_ROOT" -copied-fixture -vault-root "$VAULT_ROOT" \
  -source "$SOURCE_NOTE" -models "$MODELS_JSON" -reserved-output 4096 \
  -accounting-probe -probe-live-read -no-write \
  -receipt "$PRIVATE_RECEIPT_DIR/aion-accounting-probe.json"
```

Omit `-probe-live-read -no-write` for offline request binding only (no HTTP).
Explicit `-live-read -no-write -vault-root ...` can replace copied fixture flags;
no vault or runtime config is discovered. Use `-expected-input-sha256` to pin a
previous snapshot. The library also accepts exact serialized request bytes and
rejects any difference from the request reconstructed from that input/config.
The command emits the same redacted receipt to stdout and a new 0600 file, then
exits nonzero with `tokenizer-template-accounting-required`. A receipt with this
state is evidence of inspection, never successful capacity admission.

Live inspection performs only GET `/v1/models` and GET `/openapi.json` at the
fixed Sparks origin. It sends no source text, credentials or request body, and
never calls `/tokenize` or chat completions. There is no proxy, redirect, retry,
alternate endpoint or fallback. Total deadline is 10 seconds, response header
deadline 5 seconds, headers at most 16 KiB and each body at most 1 MiB. Duplicate
JSON keys, invalid UTF-8, detected secrets, malformed capacities and changed
model/capacity refuse. Saved discovery and current discovery have separate hashes:
volatile permission IDs can change, but selected model/owner/window must match.
Only allowlisted fields are copied into the receipt; raw metadata is not retained.

Read-only inspection on 2026-09-16 observed `deepseek-v4.1-flash` owned by vLLM,
with `max_model_len=1048576`. The deployed OpenAPI document (234849 bytes,
SHA-256 `703145d7ed4ba7fff54f0412b4e630687838ff0c5489f2d135cb3cbf8cc08172`)
advertised completion and chat inputs for POST `/tokenize`, but its HTTP 200 JSON
response schema was empty (`{}`). It did not advertise `/tokenizer_info`.
The [vLLM provider contract](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html)
distinguishes tokenization from chat completion and describes tokenizer metadata;
upstream documentation alone cannot attest this deployed revision or template.
No AION input was supplied for the live inspection; no exact-input token count or
private AION accounting evidence is claimed by that observation.

The exact remaining blocker is a reviewed deployed tokenizer/template contract
that binds both complete serialized-request tokenization and actual completion
chat rendering to the model/revision, plus authoritative request-byte admission
evidence. This server's advertised response does not establish those facts. The
probe therefore records `tokenizer-response-contract-undocumented`; even a future
nonempty schema remains `tokenizer-template-contract-unsupported` until a reviewed
adapter exists. No guessed response/count fields or raw-token-to-chat equivalence
are supported. An endpoint being advertised is not proof it executes correctly.

The receipt binds input, config, capability, exact request bytes/hash, live model
metadata, OpenAPI evidence and its own digest (computed with `probeSha256` empty).
It reserves at least 4096 output tokens, a 256-token margin and a 1024-byte margin.
Measured serialized/prompt tokens and verified byte bound remain zero (unknown),
and tokenizer/template hashes remain absent. Request byte length is measured
exactly but is **not** a verified provider limit. These zero fields cannot be used
as the existing `SparksAccounting` attestation. Mocked tests prove refusal and
binding, not an authoritative positive accounting path.

Next obtain the independently reviewed accounting evidence described above,
then an explicitly authorized no-write canary receipt with exact usage matching.
The production canary remains uninvoked; projections remain paused. All semantic,
historical reconciliation, reviewer, replay, fence and activation/handoff receipt
requirements in “Remaining activation work” still apply.
