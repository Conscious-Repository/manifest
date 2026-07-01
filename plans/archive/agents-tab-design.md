# Agents Tab — Concepts + Design (Hermes cockpit)

How to think about agents in the dashboard, and what the Agents tab should be. You
already run **Hermes Agent** (Nous Research's open-source, self-hosted, model-
agnostic harness) on a DigitalOcean box and talk to it via Telegram. The tab is a
**control surface over Hermes**, not a new orchestrator.

---

## 1. Vocabulary (so the confusion goes away)

- **Harness** — the runtime that turns a raw model into a working agent: handles
  the model connection, memory, tools, and channels. *Hermes is your harness.* You
  don't rebuild this.
- **Agent** — a running instance of the harness with a configuration.
- **Profile** (a.k.a. persona / role / subagent config) — a *named* configuration
  that defines one job: its brief/system prompt, allowed tools, model tier, memory
  scope, and permissions. Switching profile = the agent putting on a different hat.
- **Orchestrator / supervisor** — the top agent that takes your request and
  *routes* it to the right profile (a.k.a. handoff / triage / dispatch). At your
  scale, **Hermes itself is the orchestrator**; routing can even be you picking a
  profile in the console.

The mental model: **one harness (Hermes) → one orchestrator (Hermes) → a few
profiles.** You grow *profiles*, not the number of agents.

---

## 2. The right model at your scale

You've run 1–2 agents. The correct next step is **not** a swarm. Current practice
is blunt that ~40% of multi-agent setups fail in production from over-engineered
orchestration. So:

- **One orchestrator, a few specialized profiles.** Specialize by *job*, not by
  model. Add a profile when you notice a repeated task, not speculatively.
- **Model tiering.** Cheap model for triage/routing and light tasks; strong model
  for real reasoning. (Typical 40–60% cost savings.) Hermes is model-agnostic, so
  this is a per-profile setting.
- **State/memory, decided on purpose** (the #1 2026 best practice): know what
  persists vs what's ephemeral. Hermes already persists facts and learns skills —
  in **its own memory store on the DO box**. Keep that separate from your vault
  (next point).
- **Human-in-the-loop for side effects.** Anything irreversible waits for your
  approval (the `approvals/` gate from the build plan).
- **Observability.** You should *see* what ran, what it produced, what failed —
  this is the fix for "crons I set up and never check."

### The load-bearing boundary

Two memories, and they're not the same:

- **Hermes's memory** (on its server) — Hermes may freely write here; that's how it
  learns skills and remembers you.
- **Your Obsidian vault** — **AI is read-only, always.** Agents may *read* the
  vault for context and *propose* drafts to `approvals/`, but never author notes.

This keeps your "AI never writes to my DB" rule intact while letting Hermes be a
real, learning assistant.

---

## 3. What the Agents tab is — three surfaces

### A. Live console (what you asked for: query the agent directly)
A chat panel that talks to **Hermes on its server in real time**. Pick a profile,
send a message, watch the response stream. This is the synchronous "do this now /
ask this now" path — the same Hermes you reach on Telegram, but from inside the
dashboard with your vault context one click away.

### B. Roster / profiles manager
View and edit your agent **profiles** (markdown files in `Agents/`): brief, model
tier, tools, permissions, and schedule (crons). This is where you "manage agents
with specific profiles" — adding or tuning a profile is editing one small file.

### C. Work queue + observability (the durable backbone)
The Maildir queue from the build plan, surfaced: post a background/scheduled task,
Hermes picks it up, the result lands in `outbox/` and any side effects in
`approvals/`. The panel shows what's running, recent outputs, pending approvals, the
run log, and each cron's schedule + last-run health.

**Two modes, one tab:** the live console is *synchronous* (answer now); the queue is
*asynchronous + durable* (work in the background / on a schedule, crash-safe). You'll
use both.

---

## 4. How it connects to Hermes (architecture)

- **Live path:** Hermes ships an **OpenAI-compatible API server**
  (`/v1/chat/completions`, `/v1/responses`, `/api/jobs`, `/v1/skills`,
  `/v1/toolsets`), gated by `API_SERVER_KEY`. The dashboard's Go backend proxies to
  it **over your Tailscale tailnet** (private, no public port). Endpoint URL + key
  live in `~/.config/manifest/` — **never in the vault**. UI → dashboard → Hermes.
- **Durable path:** Hermes has access to a **synced copy of the `Agents/` folder**
  (Obsidian Sync / git / syncthing). It reads `inbox/`, writes `outbox/` and
  `approvals/`. Files are the contract; the dashboard just renders them.
- **Context:** agents read the vault read-only (funder briefs, goals, daily notes);
  proposals go to `approvals/`; nothing is authored into your notes.

> **Resolved:** Hermes has a built-in OpenAI-compatible API server, so no shim is
> needed. Enable it on the DO box and reach it over Tailscale. Exact setup +
> seeded profiles are in `agents-milestone-build.md`.

---

## 5. Profile file format (markdown, fits md-everything)

Each profile is one file in `Agents/`, frontmatter + brief:

```markdown
---
name: fundraising-aide
model: strong            # tier: strong | cheap  (mapped to concrete models in config)
tools: [vault.read, calendar.read, web.search]
permissions: read-only   # may propose to approvals/, never writes the vault
schedule: none           # or a cron, e.g. "0 7 * * *"
---
You prepare funder context briefs from the vault on request. Read-only: pull the
person's notes, last touchpoints, and transcripts; synthesize a pre-call brief.
Never write notes. If you draft a follow-up, place it in approvals/ for Benjamin.
```

Starter profiles to consider (add only what you'll use):
- **triage** (cheap) — routes/labels incoming requests, summarizes.
- **daily-digest** (cheap, scheduled) — your cron info-pulls → a digest in `outbox/`.
- **fundraising-aide** (strong, on-demand) — read-only funder briefs (ties to the
  funder feature).
- **scheduler** (cheap) — proposes schedule/calendar blocks → `approvals/`.

---

## 6. Best-practices checklist (for setup success)

- One orchestrator (Hermes), few profiles; grow profiles, not agents.
- Specialize profiles by job; tier models (cheap triage, strong reasoning).
- Decide memory on purpose: Hermes memory (writable) vs vault (read-only).
- Gate every irreversible action through `approvals/`.
- Make it observable: runs, outputs, failures, cron health all visible.
- Keep secrets out of the vault (`~/.config/manifest/`).
- No agent-spawns-agent; no speculative swarm.

---

## 7. Build notes / open questions

1. **Hermes interface:** resolved — built-in OpenAI-compatible API server over
   Tailscale (see `agents-milestone-build.md`).
2. **Auth:** token-based access to the Hermes endpoint, stored outside the vault.
3. **Which 2–3 profiles to seed first?** (Suggest: daily-digest + fundraising-aide.)
4. **Sync mechanism** for the `Agents/` folder to the DO box (Obsidian Sync / git /
   syncthing)?
5. Live console streaming protocol (SSE/websocket) from Hermes → dashboard.
