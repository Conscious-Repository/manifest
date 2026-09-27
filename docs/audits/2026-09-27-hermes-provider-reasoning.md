# Hermes provider and reasoning per chat message: canary re-audit (2026-09-27)

**Change.** The chat surface's model · effort picker lets the owner choose, per message to a native (Hermes) agent, a model from any provider Hermes lists (`provider_models_cache.json`) and a reasoning level. `agentchat.Recipient` gains `Provider` and `Effort`; `hermes.Request` gains `Provider` and `Reasoning`; legacy `buildArgs` appends `--provider` and `--reasoning` when they are set. Both flags are documented Hermes options (`hermes --help`: `[--provider PROVIDER] [--reasoning LEVEL]`).

**Validation before anything is recorded.** `hermesChoiceError` refuses a provider/model pair the Hermes cache does not list, a provider without a model, and any effort outside Hermes's own levels (none, minimal, low, medium, high, xhigh, max, ultra). A retry under the same request id with a different provider or effort is a request conflict, like a changed model.

**Successor isolation.** `Runner.Run` returns through the fixed `MigratedDuty` branch (`runSuccessor` / extraction) before `buildArgs`, so the new fields cannot reach the successor runtime. `successor.go`, `successor.py`, `claude_successor.go`, `authority.go` and `fallback.go` are byte-identical. The successor's duty authority still binds its own provider and model. No import, call, cost, tool or MCP authority changed. The `runner.go` pin in `cmd/re-intake-canary` was refreshed to the reviewed bytes, with this rationale beside it.

**Evidence.** `TestBuildArgsProviderAndReasoning` (flags only when set; default argv unchanged), `TestHermesChoiceRefusesUnlistedPairs`, `TestChatModelCatalogPerAgent`, `TestFixtureChatComposerModels` (the exact recipient on send), and the canary green after the pin.

**Not verified live.** No provider was called (D7). Whether each provider accepts a given model at runtime is shown by the turn's own outcome; a refusal lands as a visible failed turn.
