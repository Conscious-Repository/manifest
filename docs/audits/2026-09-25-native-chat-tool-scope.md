# Native chat tool scope receipts — September 25

Hermes-backed native chat records the resolved toolset scope in its durable
delivery receipt before invoking the runner. Resolution uses the same helper as
CLI argument construction: explicit request, configured runner default, or an
empty override delegated to profile defaults. The last case does not enumerate
unknown profile tools. This records invocation configuration, not proof that any
particular tool was called or that all configured tools were available.

The metadata is separate from accepted input fingerprints, so retries preserve
input identity. Only running deliveries can acquire it; once recorded it cannot
be replaced by later configuration. Existing recipient, model, artifacts and
history-omission fields remain separate. The context inspector shows scope for
the selected recorded instruction and explicitly labels missing historical data.
It does not infer active skills or automatically capture another adapter's tools.

Focused race tests cover durable reopen, queued refusal, immutable scope,
unchanged retry identity, resolver/CLI argument agreement and actual native chat
dispatch through the fake runner. Chromium verifies recorded scope versus missing
historical scope within the real context pane and existing pane-state restoration.
Full-suite/build/deployment results are recorded in the plan checkpoint. Skills,
other adapter capabilities and the broader workbench requirements remain open.
