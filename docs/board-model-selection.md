# Coding task models

Bare `@codex` selects `gpt-6-astra`; bare `@claude` selects `fable`.
Direct board assignments use the same defaults. Deployment policy lives in
`server/board_models.go`: each owner's `best` and `allowed` entries are the
single place to change defaults or enable additional model names.

Mention grammar: `@name[::intent][::model:slug]`, with either segment order.
There may be one intent and one model segment. Examples:

- `@codex::model:gpt-5.5` executes with GPT-5.5.
- `@claude::model:opus` executes with Opus.
- `@codex::model:gpt-5.5::plan` plans with GPT-5.5.
- `@claude::plan::model:opus` plans with Opus.
- `@codex::plan` plans with the best default.

The existing `::brief` and other persona intents remain separate from model
selection. Only explicit `::plan` makes coding owners plan rather than execute.
Model selection applies to coding owners only. Typed mentions are case
insensitive; a trailing sentence period is punctuation. Structural API tokens
use `agent:name` in place of `@name`, including the Ask/Do `agent` field.

Comment still dispatches at most one turn, to the first recognized agent;
comments without mentions remain record-only. On Ask/Do, the first recognized
mention overrides the selected agent (or existing assignee/default), unless
the selected agent token already supplies a model. Options come from matching
mentions when absent from that token. A bare autocomplete mention does not
hide options typed for that same agent. Task capture preserves model and intent
options through Ask/Do dispatch while stripping the address from the task text.
Owner fields always store the bare token.

Accepted overrides: Codex `gpt-6-astra`, `gpt-5.5`; Claude `fable`, `opus`,
`sonnet`. `best` explicitly selects the owner's default. Unknown names, empty
model segments, or duplicate segments fall back to best with a note in both
the thread and brief. This prevents a typo from launching an unsupported model;
a parsed plan intent is preserved even when model syntax is invalid.

The launch always supplies `-m` (Codex) or `--model` (Claude). The chosen model
is recorded in `brief.md`, a short thread entry, and the existing terminal
registry before spawning. Pending busy-task retries retain the requested
model. Reopening a session uses its recorded model and normal resume command,
without replaying the original work order. The stored Claude alias remains
pinned as an alias; its provider resolution can change over time.

Local verification on 2026-09-07: noninteractive probes with `gpt-6-astra` and
`fable` each completed and returned `hi`. Claude's installed `--help` explicitly
lists `fable`, `opus`, and `sonnet`; its settings selected `fable`. Codex's model
cache lists `gpt-5.5`. The alternate overrides were not executed in these probes;
CLI help/cache evidence is not a guarantee of account access for every model.
