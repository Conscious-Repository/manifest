# Excalibur Path — All-Markdown Agent Harness (plan v4)

Direction: adopt the **Excalibur** philosophy (github.com/viemccoy/excalibur) — the
agent system is **markdown you own**: a spirit's identity, behavior, spellbooks,
rituals, memory, and charge all live in `.md`. This plan makes that real *without*
rebuilding a model runtime, and folds in the four concepts worth keeping (charge,
fail-closed warding, warden, memory tiers).

**v3 changes (audit round):** legibility is now a first-class requirement — every run
writes a markdown **run report** + the exact assembled prompt is inspectable (§6.5,
in Slice 1); the existing dashboard agent code is **replaced in one move** (§2.5);
engine language = **Go**; charge is denominated in **dollars** (§3); ritual
scheduling is owned by the engine (§7.5); primary spirit named **marduk**; the
cross-dependency with the goals spec's Phase 2 approvals is called out (§2.5).

**v4 changes (portals round):** portals now come in **two kinds** — `api`
(OpenAI-compatible HTTP) and `cli` (subprocess over a subscription login) — with the
Slice 1 default being **Claude via subscription CLI** (`claude -p`, billed to the
June-2026 Agent SDK credit pool, no API key) (§0.5); **Exa** locked as the
`web.search` provider; charge on subscription portals metered in **notional dollars**
at list API prices (§3); the **Obsidian CLI** (official, built into Obsidian 1.12)
becomes a read-only `obsidian` spellbook post-Slice-1 (§6.6); Hermes-as-portal
explicitly rejected as the brain (§0.5).

Decisions locked:
- **Backend = direct model calls, model-agnostic** (not Hermes as the brain). See §0.
- **Naming = mystical** (spirits / spellbooks / casts / rituals / charge / warden /
  grimoire / vessel / questbook / artifacts).
- **Layout = sibling folders** — the `excalibur/` harness sits *beside* the dashboard
  repo and the Obsidian vault, never inside the vault.
- **Engine = Go.** Same toolchain and idioms as the dashboard (single static binary,
  mdfm-style markdown handling, systemd-friendly). One stack, no second runtime.
- **Primary spirit = `marduk`.**
- **Reconcile = replace in one move** (§2.5): the Hermes-cockpit path and the app's
  `agents`/`profiles` packages are retired as part of this build.

Credit: Excalibur is viemccoy's opinionated, code-free scaffold (single-commit repo,
zero code by design — the author intends you to build the engine so you understand
its scope; there is no reference implementation to lean on). When building, have
Claude Code read Excalibur's `INVOCATION.md` and `AGENTS.md` directly.

---

## 0. The engine question — settled

Excalibur ships **no code**; it's conventions. "Going Excalibur" = **markdown is the
source of truth; a thin engine you own runs underneath.**

**The spirit's mind is a model, called directly.** The orchestrator (marduk's engine)
calls an **OpenAI-compatible model endpoint** for *reasoning only* — "given this
context and these available casts, what next?" — and **the engine itself executes
the casts**, enforces charge and warding, and writes only to allowed surfaces. Why
direct, not Hermes:

- Hermes is an *agent*, not a model: routing through it makes your spellbooks,
  charge, and warding a thin skin over Hermes's own agent, which would own the real
  behavior. Direct calls make **the spirit + your engine the actual agent** — the
  whole point.
- One memory system (your markdown tiers), not two. Charge metered on *your* calls.
  Warding enforced by *your* executor. Fully model-agnostic and sovereign.

**Model-agnostic from day one:** every brain is reached through a `portal` (§0.5);
swapping brains is a one-line markdown edit. Start with Claude for the loop; a
local/open model later is just another portal. Hermes may survive only as an
optional `spellbook` if a concrete need appears (e.g. Telegram reach) — subordinate
to your loop, never the brain. The verified SSE streaming contract from
`agents-milestone-build.md` still applies to any `api`-kind portal.

## 0.5 Portals — two kinds, chosen per spirit

A portal is a named "phone line" to a brain: a small def in `grimoire/portals/<name>.md`
(+ non-secret settings in `config/`). Spirits name their portal via `portal::` in
`cornerstone.md`; a ritual may override it. The dashboard gets a portal picker per
spirit. The portal is **reasoning-only** — it never touches files; the engine
executes all casts.

- **`kind: api`** — an OpenAI-compatible HTTP endpoint. API key in env, streaming
  SSE, charge metered at the prices in `chargebook.md`. This is also how a local
  model (Ollama/llama.cpp/etc.) plugs in later.
- **`kind: cli`** — a subprocess over a subscription login, for running on your
  always-local machine without new API keys:
  - **`claude-sub` (Slice 1 default):** `claude -p --output-format json`, tools
    disabled so it behaves as a pure reasoning call. Authenticates via your existing
    Claude login; as of June 15, 2026 this bills the plan's monthly **Agent SDK
    credit pool** ($100–200/mo on Max tiers) and does **not** count against
    interactive limits. The JSON output includes token usage, so charge metering
    works normally. **Guard:** the engine must strip `ANTHROPIC_API_KEY` from the
    subprocess env — if set, `claude -p` silently bills the API instead of the plan.
  - **`codex-sub` (optional):** `codex exec` under a ChatGPT login works the same
    way (counts against that plan's agentic limits). Available as a second cli
    portal if ever wanted; not the default.

**Hermes as a portal — rejected.** Hermes's OpenAI-compatible endpoint answers as
Hermes-the-agent (its own memory, its own behaviors), not as a raw model. Dialing it
as the brain reintroduces exactly what §0 rejected: warding and run reports would
describe Hermes's reasoning, not your spirit's — and Hermes needs upstream API keys
anyway. Hermes remains eligible only as a *spellbook* (a tool the engine calls for a
specific job).

---

## 1. The markdown tree (mystical, sibling to vault + dashboard)

```
excalibur/                       # sibling of the dashboard repo and the Obsidian vault
  chargebook.md                  # budget rules (charge, in dollars; per-model prices)
  INVOCATION.md  AGENTS.md       # onboarding for the coding model (from Excalibur)
  spirits/
    marduk/                      # your orchestrator spirit
      identity.md                # what this spirit is
      cornerstone.md             # how it behaves; its allowed surfaces + spellbooks
      rituals/                   # scheduled workings (crons), one .md each
      memories/
        long-term.md             # top-of-head, always loaded
        window/                  # rolling recent-memory window
        archive/                 # durable, retrieved on demand
    warden/                      # the security spirit (§5)
      identity.md  cornerstone.md
      rituals/audit.md
    scouts/                      # domain-scout, options-scout, ea-coordinator (spirits)
  grimoire/                      # spirit-side internals
    spellbooks/
      <book>/
        spellbook.md             # what the book grants + its permission surface
        <cast>/spell.md          # one concrete cast (a tool the engine executes)
    portals/                     # brains: api-kind (OpenAI-compatible HTTP) + cli-kind (claude-sub, codex-sub) — §0.5
    engine/                      # the thin orchestrator's own wiring docs
  artifacts/                     # SHARED surface (spirit + summoner)
    feed/                        # your .md personal-X feed items
    library/                     # research artifacts (options-scout)
    approvals/                   # THE approvals inbox (§2.5) — proposals awaiting your yes
    runs/                        # markdown run reports — the legible decision trail (§6.5)
  questbook/                     # obligations / continuity ledger (shared)
  vessel/                        # machine-local: run ledgers, backups, helper runtimes
    state/<spirit>/conversations/<local-date>.jsonl   # every turn mirrored (raw)
    state/<spirit>/prompts/<run-id>/                  # exact assembled prompts (§6.5)
  config/                        # portal endpoints, warding policy (no secrets)
```

- `identity.md` = what a spirit is; `cornerstone.md` = how it behaves + its allowed
  surfaces; `rituals/*.md` = schedules; `grimoire/spellbooks/*` = opt-in casts;
  `artifacts/` + `questbook/` = shared surfaces; `memories/*` = tiered memory;
  `vessel/*` = local runtime.
- **Editing a spirit = editing markdown**, through no-syntax dashboard editors, like
  your goals/manifest editors.

---

## 2. How it maps onto what you already have

- **The feed** (`artifacts/feed/`) is your `.md` personal-X feed — same schema,
  keep/discard/snooze, and "save to vault" promotion.
- **Scouts** become **spirits**: domain-scout, options-scout, ea-coordinator, each an
  `identity.md` + `cornerstone.md` (+ a `ritual` for the scheduled one). The old
  "profiles" *are* these spirit files — migrated, then the profiles package retires.
- **The dashboard** is the summoner's console: talk to marduk, watch charge, read
  the feed, read run reports, act on approvals.
- **The Obsidian knowledge vault** is a *separate sibling tree*, a **read-only mount**
  to every spirit (§4). The harness's `artifacts/` is where spirits write; the vault
  is where *you* write.

## 2.5 Reconciliation — replace in one move

The dashboard's existing agent machinery is superseded by this build, not run in
parallel:

- **Retire:** `agents/` (queue, supervisor, agentdef, its approvals), `profiles/`
  (package + store), `hermes/` client as a first-class path, and the Hermes-cockpit
  design (`agents-milestone-build.md` stands as history only).
- **Migrate:** existing profile `.md` files convert to spirit files
  (`identity.md`/`cornerstone.md` + ritual where scheduled). The append-only run-log
  idea survives as the vessel jsonl mirror + run reports.
- **Unify approvals:** `excalibur/artifacts/approvals/` becomes the **one** approvals
  inbox. The dashboard's approvals UI repoints to it. **Cross-plan dependency:** the
  goals build spec (`goals-build-spec.md`) Phase 2 has the EA writing proposals to
  the app's old approvals store — when goals Phase 2 lands, its proposals must
  target this surface instead. Coordinate the two handoffs; goals Phase 1 is
  unaffected.
- **Sequencing safety:** "one move" is a codebase decision, not a same-day cutover.
  Build Slice 1 → prove it → repoint the dashboard + delete the retired packages in
  this build's final step. The DO box / Hermes stays running untouched until the
  slice is proven, then decommission (or keep only if a Telegram-reach spellbook is
  actually wanted).

---

## 3. Charge — the budget primitive

A run gets a **charge** — a ceiling **denominated in dollars**. `chargebook.md`
carries per-model input/output token prices; the engine converts each model
response's `usage` at the active portal's rate and decrements. (Dollars, not tokens:
token counts stop being comparable the moment you mix models.) **Subscription (cli)
portals meter the same way, at notional list API prices** — the marginal cost is
prepaid by the plan's credit pool, but notional dollars keep charge comparable
across portals, keep budgets meaningful, and show what a run "would cost" against
the monthly pool.

- Local management casts cost ~0; casts that search, generate, or spawn an
  **emanation** (child spirit) cost charge; an emanation draws from its parent's
  allocation and can't exceed it.
- `chargebook.md` holds base cast costs, model prices, + reclaim-on-durable-progress
  rules.
- A ritual that exhausts its charge **stops and reports** rather than overspending.
- The dashboard shows a charge bar per run; the run report itemizes charge per step.

## 4. Warding — fail-closed permissions (hardens your #1 rule)

Excalibur's security model makes "AI never writes my vault" **structural**:

- Each spirit's writable surface is an **explicit allow-list** in its `cornerstone.md`
  (e.g. marduk → `artifacts/feed`, `artifacts/library`, `artifacts/approvals`,
  `artifacts/runs`, `questbook`). A path not listed **fails closed** — the cast
  refuses.
- The **Obsidian vault is never in any allow-list**; spirits get it read-only.
  Promotion to a real note is *your* dashboard action.
- Spellbooks are opt-in per spirit (`available_spellbooks` → `additional_spellbooks`
  → `open_spellbooks`); widening is a deliberate markdown edit.
- Authority/transport rules **fail closed if config is missing**. Secrets in env /
  keychain, never markdown.
- **Hostile-content honesty:** warding contains the blast radius of a
  prompt-injected web result (a compromised scout can only write feed items), but it
  can't prevent garbage feed items. The run report (§6.5) is what lets you *notice*
  — a feed item whose report shows an odd cast trail is your tell. Warden audits for
  the structural side.

## 5. Warden — the security spirit

A dedicated **warden** spirit runs on a ritual cadence and audits: warding drift, any
path that could reach the vault, secret handling, exposed transport, over-broad
spellbooks. It reports (and, within bounds you set, remediates) into
`artifacts/approvals/`. Marduk is not the only line of defense.

## 6. Memory tiers

- `memories/long-term.md` — always-loaded top-of-head.
- `memories/window/` — rolling recent context.
- `memories/archive/` — durable, retrieved on demand.
- A **consolidation ritual** summarizes window → long-term and ages the rest, so
  spirits aren't overwhelmed.

## 6.5 Legibility — the run report (the point of the whole exercise)

The reason for going Excalibur is a scaffold you can read and edit to understand how
your agents decide. Identity/cornerstone/spellbooks make the *inputs* legible; this
makes the *decisions* legible. **In Slice 1, not later.**

Every run writes `artifacts/runs/<date>-<spirit>-<run-id>.md`:

- **Context manifest** — exactly which files were assembled into the prompt
  (identity, cornerstone, which memory tiers/files, ritual text), in order.
- **Decision trail** — each loop step: casts available → cast chosen → the model's
  stated reason (the engine asks for a one-line rationale with each cast call), its
  arguments, its result summary.
- **Charge ledger** — cost per step, remaining charge.
- **Writes** — every file written, with path.
- **Outcome** — completed / stopped (charge exhausted / error), and what it reported.

The exact assembled prompt for each model call is preserved verbatim under
`vessel/state/<spirit>/prompts/<run-id>/`, and the dashboard's run view has a
"show assembled prompt" affordance. The jsonl mirror remains the raw machine record;
the run report is the human one. Over time, run reports are what you edit
cornerstones *against* — the feedback loop that tunes spirits to your use cases.

## 6.6 Obsidian CLI — a read-only spellbook (post-Slice-1)

Obsidian now ships an official CLI (built into Obsidian 1.12, GA Feb 2026, ~115
commands). Its shape matters: it's a **remote control for the running Obsidian app**,
not a headless tool — it launches Obsidian if it isn't running. Consequences:

- It never replaces the app's direct markdown read/write (mdfm/vaultwriter work
  with Obsidian closed; that stays the core).
- Its value is retrieval: an **`obsidian` spellbook** with read-only casts
  (`vault.search`, `vault.read` — backed by `obsidian search`, note reads, tag and
  property queries) gives spirits Obsidian's own index instead of grep. Warding
  blocks every write-capable command — the CLI can write to the vault, which is
  precisely what spirits must never do, so the spellbook whitelists read commands
  only. The engine falls back to direct file reads when Obsidian isn't running.
- Secondary, non-spirit use: dashboard conveniences ("open in Obsidian",
  `daily:append`) as desired.

Sequenced after Slice 1, alongside the warden ritual.

---

## 7. De-risked build — one vertical slice first

Do **not** instantiate the whole cosmology. Prove the loop with one scout.

**Slice 1 — `domain-scout`, end to end (Go engine):**
1. **Engine** (thin, Go): read the markdown, assemble the prompt from
   identity/cornerstone/memory, call the brain via the **`claude-sub` cli portal**
   (§0.5 — `claude -p`, subscription-billed, `ANTHROPIC_API_KEY` stripped from the
   subprocess env), run the agent loop, meter `usage` → notional-dollar charge,
   mirror every turn to `vessel/state/domain-scout/conversations/<date>.jsonl`,
   preserve assembled prompts, **write the run report** (§6.5). The portal
   abstraction ships with both kinds so an `api` portal is a config addition, not a
   refactor.
2. **One spellbook, two casts:** `web.search` (**Exa**; `EXA_API_KEY` in env, cost
   line in `chargebook.md`) and `write_feed_item` (writes a `.md` into
   `artifacts/feed/`). That's the only capability the scout gets.
3. **Warding:** writable surface = `artifacts/feed/` + `artifacts/runs/` only; vault
   read-only; fail closed.
4. **Ritual:** a daily schedule that runs the scout.
5. **Dashboard:** renders the feed scroll (keep/discard/snooze) + a charge bar + the
   run report view with "show assembled prompt."

That slice exercises the entire architecture — markdown source of truth, your own
engine + direct model calls, real spellbooks you execute, charge, fail-closed
warding, the feed, and the legible decision trail. Once it feels right, add:
`warden` ritual, the `obsidian` read-only spellbook (§6.6), `options-scout`
(artifacts), memory consolidation, then `ea-coordinator` + approvals (see §2.5
goals-spec dependency), then the §2.5 retirement step.

## 7.5 Ritual scheduling — engine-owned

The **engine owns the scheduler**: it reads `rituals/*.md` (cadence in frontmatter)
and runs them. One systemd unit starts the engine; the dashboard only *displays*
runs and schedules, never triggers rituals — otherwise the dashboard quietly becomes
the orchestrator. Manual "run now" from the dashboard is allowed as a request the
engine picks up, not a direct invocation.

---

## 8. Honest tradeoff

- You build+own a **thin engine**: prompt assembly, the agent loop, cast execution,
  charge, warding, run reports, the ledger, scheduling. Real work — but bounded, and
  Claude Code can do the Slice-1 version quickly because the casts are minimal
  (search + write).
- You **do** build your own tools (spellbooks) — that's the sovereignty you wanted,
  and it grows deliberately, not all at once.
- **Replace-in-one-move risk:** once the retirement step lands there is no Hermes
  fallback path. Mitigated by sequencing (§2.5): nothing is deleted until Slice 1 is
  proven and the dashboard is repointed.
- Upside: the whole system is markdown you own; model-agnostic; the vault boundary is
  structural; you get budgeting, a security auditor, and a decision trail you can
  actually read.

---

## 9. Hand to Claude Code

- This plan + `agents-tab-design.md` (concepts). The verified SSE contract in
  `agents-milestone-build.md` still applies to *any* OpenAI-compatible endpoint.
- Have it read Excalibur's `INVOCATION.md` and `AGENTS.md` from the repo.
- Kickoff: "Build **Slice 1** only (§7). New `excalibur/` repo, **sibling** to the
  dashboard and the Obsidian vault. Mystical vocabulary; primary spirit is
  **marduk** (Slice 1 instantiates only `domain-scout`). Thin engine in **Go**; the
  engine executes casts. Brain reached through the portal abstraction (§0.5, both
  kinds implemented); Slice 1 default portal = **`claude-sub`** (`claude -p
  --output-format json`, tools disabled, `ANTHROPIC_API_KEY` stripped from the
  subprocess env so it bills the subscription). One spirit `domain-scout` with one
  spellbook (`web.search` via **Exa** + `write_feed_item`). Writable surface =
  `artifacts/feed/` + `artifacts/runs/` only; vault read-only; fail closed. Meter
  token usage into (notional-)dollar charge per `chargebook.md`. **Every run writes
  a markdown run report per §6.5 and preserves the exact assembled prompts.**
  Engine owns the ritual scheduler (§7.5). Render the feed + charge bar + run
  reports (with assembled-prompt view) in the dashboard. Do not touch the existing
  `agents`/`profiles`/`hermes` packages yet — retirement (§2.5) is the final step,
  after I've approved the slice. Stop and show me before adding more spirits."

---

## 10. Confirmed

1. ✅ Backend = **direct model calls, model-agnostic** (Claude first); Hermes retired
   (optional future spellbook only if a concrete need appears).
2. ✅ Naming = **mystical**; primary spirit = **marduk**.
3. ✅ Layout = **sibling folders**; `excalibur/` never inside the knowledge vault.
4. ✅ Engine = **Go**; charge in **dollars** (notional on subscription portals);
   engine-owned scheduling.
5. ✅ Legibility = run reports + assembled-prompt preservation, in Slice 1.
6. ✅ Reconcile = **replace in one move**, sequenced (build → prove → repoint →
   retire); DO box stays up until then.
7. ✅ Portal = two kinds (§0.5); Slice 1 default = **`claude-sub`** (subscription
   CLI); Hermes-as-brain rejected.
8. ✅ `web.search` provider = **Exa** (`EXA_API_KEY` in env, chargebook cost line).
9. ✅ Obsidian CLI = read-only `obsidian` spellbook post-Slice-1 (§6.6).

No open picks remain. Prerequisites before kickoff: an Exa API key in env, and a
logged-in `claude` CLI on this machine (already present if Claude Code runs here).
