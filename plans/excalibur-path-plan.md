# Excalibur Path — All-Markdown Agent Harness (plan v2)

Direction: adopt the **Excalibur** philosophy (github.com/viemccoy/excalibur) — the
agent system is **markdown you own**: a spirit's identity, behavior, spellbooks,
rituals, memory, and charge all live in `.md`. This plan makes that real *without*
rebuilding a model runtime, and folds in the four concepts worth keeping (charge,
fail-closed warding, warden, memory tiers).

Decisions locked this round:
- **Backend = direct model calls, model-agnostic** (not Hermes as the brain). See §0.
- **Naming = mystical** (spirits / spellbooks / casts / rituals / charge / warden /
  grimoire / vessel / questbook / artifacts).
- **Layout = sibling folders** — the `excalibur/` harness sits *beside* the dashboard
  repo and the Obsidian vault, never inside the vault.

Credit: Excalibur is viemccoy's opinionated, code-free scaffold. This is an original
adaptation; when building, have Claude Code read Excalibur's `INVOCATION.md` and
`AGENTS.md` directly (the author intends a coding model to onboard from them).

---

## 0. The engine question — settled

Excalibur ships **no code**; it's conventions. "Going Excalibur" = **markdown is the
source of truth; a thin engine you own runs underneath.**

**The spirit's mind is a model, called directly.** The orchestrator (your primary
spirit's engine) calls an **OpenAI-compatible model endpoint** for *reasoning only* —
"given this context and these available casts, what next?" — and **the engine itself
executes the casts**, enforces charge and warding, and writes only to allowed
surfaces. Why direct, not Hermes:

- Hermes is an *agent*, not a model: routing through it makes your spellbooks,
  charge, and warding a thin skin over Hermes's own agent, which would own the real
  behavior. Direct calls make **the spirit + your engine the actual agent** — the
  whole point.
- One memory system (your markdown tiers), not two. Charge metered on *your* calls.
  Warding enforced by *your* executor. Fully model-agnostic and sovereign.

**Model-agnostic from day one:** talk to any OpenAI-compatible endpoint via a
`portal`. Start with the strongest reasoning brain (Claude) for the loop; swap to a
local/open model later with a config change. **Hermes is demoted to an optional
`spellbook`** the orchestrator may invoke for its heavy tools or Telegram reach —
subordinate to your loop, never the brain. Your verified SSE streaming contract still
applies (every OpenAI-compatible endpoint streams the same way).

---

## 1. The markdown tree (mystical, sibling to vault + dashboard)

```
excalibur/                       # sibling of the dashboard repo and the Obsidian vault
  chargebook.md                  # budget rules (charge)
  INVOCATION.md  AGENTS.md       # onboarding for the coding model (from Excalibur)
  spirits/
    <primary>/                   # your orchestrator spirit — name it (Excalibur's example: lapis)
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
    portals/                     # outbound endpoints: model APIs (OpenAI-compatible), MCPs, Hermes
    engine/                      # the thin orchestrator's own wiring docs
  artifacts/                     # SHARED surface (spirit + summoner)
    feed/                        # your .md personal-X feed items
    library/                     # research artifacts (options-scout)
    approvals/                   # proposed irreversible actions awaiting your yes
  questbook/                     # obligations / continuity ledger (shared)
  vessel/                        # machine-local: run ledgers, backups, helper runtimes
    state/<spirit>/conversations/<local-date>.jsonl   # every turn mirrored
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
  `identity.md` + `cornerstone.md` (+ a `ritual` for the scheduled one). Your old
  "profiles" *are* these spirit files.
- **The dashboard** is the summoner's console: talk to the primary spirit, watch
  charge, read the feed, act on approvals.
- **The Obsidian knowledge vault** is a *separate sibling tree*, a **read-only mount**
  to every spirit (§4). The harness's `artifacts/` is where spirits write; the vault
  is where *you* write.
- **Hermes** stays reachable as a `portal`/`spellbook` (optional), not the brain.

---

## 3. Charge — the budget primitive

A run gets a **charge** (a token/$ ceiling). Local management casts cost ~0; casts
that search, generate, or spawn an **emanation** (child spirit) cost charge; an
emanation draws from its parent's charge and can't exceed what it was allocated.

- `chargebook.md` holds base costs + reclaim-on-durable-progress rules.
- The engine meters each model response's token `usage` and decrements charge.
- A ritual that exhausts its charge **stops and reports** rather than overspending.
- The dashboard shows a charge bar per run.

## 4. Warding — fail-closed permissions (hardens your #1 rule)

Excalibur's security model makes "AI never writes my vault" **structural**:

- Each spirit's writable surface is an **explicit allow-list** in its `cornerstone.md`
  (e.g. primary → `artifacts/feed`, `artifacts/library`, `artifacts/approvals`,
  `questbook`). A path not listed **fails closed** — the cast refuses.
- The **Obsidian vault is never in any allow-list**; spirits get it read-only.
  Promotion to a real note is *your* dashboard action.
- Spellbooks are opt-in per spirit (`available_spellbooks` → `additional_spellbooks`
  → `open_spellbooks`); widening is a deliberate markdown edit.
- Authority/transport rules **fail closed if config is missing**. Secrets in env /
  keychain, never markdown.

## 5. Warden — the security spirit

A dedicated **warden** spirit runs on a ritual cadence and audits: warding drift, any
path that could reach the vault, secret handling, exposed transport, over-broad
spellbooks. It reports (and, within bounds you set, remediates) into
`artifacts/approvals/`. The primary spirit is not the only line of defense.

## 6. Memory tiers

- `memories/long-term.md` — always-loaded top-of-head.
- `memories/window/` — rolling recent context.
- `memories/archive/` — durable, retrieved on demand.
- A **consolidation ritual** summarizes window → long-term and ages the rest, so
  spirits aren't overwhelmed.

---

## 7. De-risked build — one vertical slice first

Do **not** instantiate the whole cosmology. Prove the loop with one scout.

**Slice 1 — `domain-scout`, end to end:**
1. **Engine** (thin): read the markdown, assemble the prompt from
   identity/cornerstone/memory, call the model **directly** via an OpenAI-compatible
   `portal` (Claude to start), run the agent loop, meter `usage` → charge, mirror
   every turn to `vessel/state/domain-scout/conversations/<date>.jsonl`.
2. **One spellbook, two casts:** `web.search` and `write_feed_item` (writes a `.md`
   into `artifacts/feed/`). That's the only capability the scout gets.
3. **Warding:** writable surface = `artifacts/feed/` only; vault read-only; fail
   closed.
4. **Ritual:** a daily schedule that runs the scout.
5. **Dashboard:** renders the feed scroll (keep/discard/snooze) + a charge bar + run
   log.

That slice exercises the entire architecture — markdown source of truth, your own
engine + direct model calls, real spellbooks you execute, charge, fail-closed
warding, the feed. Once it feels right, add: `warden` ritual, `options-scout`
(artifacts), memory consolidation, then `ea-coordinator` + approvals, then (optional)
a Hermes spellbook.

**Keep Hermes live as a bridge** meanwhile (Telegram / its console), so you always
have a working path while the engine matures.

---

## 8. Honest tradeoff

- You build+own a **thin engine**: prompt assembly, the agent loop, cast execution,
  charge, warding, the ledger, scheduling. Real work — but bounded, and Claude Code
  can do the Slice-1 version quickly because the casts are minimal (search + write).
- You **do** build your own tools (spellbooks) — that's the sovereignty you wanted,
  and it grows deliberately, not all at once.
- Upside: the whole system is markdown you own; model-agnostic; the vault boundary is
  structural; you get budgeting + a security auditor.
- Fallback if Slice 1 outweighs the payoff: the Hermes-cockpit design
  (`agents-milestone-build.md`) is intact and shippable.

---

## 9. Hand to Claude Code

- This plan + `agents-tab-design.md` (concepts). The verified SSE contract in
  `agents-milestone-build.md` still applies to *any* OpenAI-compatible endpoint.
- Have it read Excalibur's `INVOCATION.md` and `AGENTS.md` from the repo.
- Kickoff: "Build **Slice 1** only (§7). New `excalibur/` repo, **sibling** to the
  dashboard and the Obsidian vault. Mystical vocabulary. Thin engine calls the model
  **directly** via an OpenAI-compatible portal (Claude first, model-agnostic); the
  engine executes casts. One spirit `domain-scout` with one spellbook (`web.search` +
  `write_feed_item`). Writable surface = `artifacts/feed/` only; vault read-only; fail
  closed. Meter token usage into charge. Render the feed + charge bar in the dashboard.
  Stop and show me before adding more spirits."

---

## 10. Confirmed

1. ✅ Backend = **direct model calls, model-agnostic** (Claude first); Hermes optional
   as a spellbook/bridge, not the brain.
2. ✅ Naming = **mystical**.
3. ✅ Layout = **sibling folders**; `excalibur/` never inside the knowledge vault.

Only open pick left (not blocking Slice 1): name your **primary spirit**, and which
model/provider the first `portal` points at (recommend Claude for the reasoning loop).
