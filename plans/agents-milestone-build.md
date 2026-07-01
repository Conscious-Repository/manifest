# Agents Milestone — Build Handoff (Hermes over Tailscale)

Build-ready spec for the Agents tab. Pairs with `agents-tab-design.md` (concepts).
Your harness is **Hermes Agent** (Nous Research) on a DigitalOcean box, reached
today via Telegram, with the box on your **Tailscale** tailnet.

> **Key fact (verified):** Hermes ships a built-in **OpenAI-compatible API server**
> — `/v1/chat/completions`, `/v1/responses`, `/api/jobs` (cron management),
> `/v1/skills`, `/v1/toolsets` — gated by `API_SERVER_KEY`, with streaming + tool
> progress and idempotency support. So we use Hermes's own API + job store as the
> durable backbone; **no custom Maildir queue is needed for Hermes** (that pattern
> stays as the model only if you ever add a non-Hermes agent). Docs:
> https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server

---

## 0. Transport decision — researched & validated

You asked to confirm tailnet + API is the most robust choice before building. It is.

- **Tailscale (chosen).** Peer-to-peer WireGuard mesh: devices connect directly →
  lowest latency, highest throughput, nothing leaves your network. **Tailscale
  Serve** exposes the Hermes port to *authenticated tailnet members only*, over
  encrypted WireGuard, terminating TLS for you with **no reverse-proxy config**.
  ACLs are default-deny; free for personal use. This is the documented pattern for
  privately reaching a self-hosted LLM/agent.
- **Cloudflare Tunnel (rejected).** Solid, but it's a *public-internet* reverse
  proxy — more attack surface, some reported reliability hiccups, and you don't need
  public access. Revisit only if you ever want Hermes reachable from a non-tailnet
  device.
- **Raw SSH tunnel (rejected).** Works, but more brittle/manual than Serve.
- **Hermes maturity.** The API server is actively hardened — recent releases (past
  v0.4.0) shipped a large reliability pass (200+ fixes) across race conditions,
  stuck sessions, **approval routing**, and **API-server streaming with conversation
  history**. Healthy signal — but **pin to the version you run (currently
  v0.17.0)**: treat each endpoint's response schema as a versioned contract and fail
  gracefully if it changes.

**Decision:** Tailscale Serve in front of the Hermes API server, API-key gated, no
public exposure. Resilience bonus: the `.md` feed (§2–§3) lives on your machine, so
the dashboard's feed keeps working even if the DO box is offline.

---

## 1. Transport — turn Hermes into a private endpoint

### On the DO box (one-time)
1. **(Optional)** `hermes setup --portal` — only for Nous Portal / Tool Gateway.
   The API server itself needs just `API_SERVER_ENABLED=true` + `API_SERVER_KEY`;
   **don't block on portal OAuth**.
2. Set env: `API_SERVER_ENABLED=true` and `API_SERVER_KEY=<long random secret>`.
3. Run the API server on localhost, then expose it to your tailnet with
   **Tailscale Serve** (TLS + tailnet-only, no reverse-proxy config, no public
   port): `tailscale serve --bg <hermes-port>`. Do **not** use Funnel (that's
   public).
4. Verify from your Mac (on the tailnet):
   `curl -H "Authorization: Bearer $KEY" https://<hermes-magicdns>/v1/skills`.
5. Lock it down with a default-deny ACL granting only your devices access to the
   Hermes node/port.

### In the dashboard (Go)
- Config at `~/.config/manifest/agents/config.yaml` (never in the vault):
  ```yaml
  hermes:
    baseURL: "https://ubuntu-s-1vcpu-1gb-amd-atl1-01.tail8f89de.ts.net"
    apiKeyEnv: "HERMES_API_KEY"     # key in OS keychain or a 0600 env file — never in YAML/vault
  paths:
    profiles:  "~/.config/manifest/agents/profiles"
    feed:      "~/.config/manifest/agents/feed"
    approvals: "~/.config/manifest/agents/approvals"
    artifacts: "~/.config/manifest/agents/artifacts"
    vault:     "~/vault"
  models: { strong: "server-default", cheap: "server-default" }
  ```
- Proxy to Hermes: chat (`/v1/chat/completions`, streamed), capability discovery
  (`/v1/skills`, `/v1/toolsets`), cron management (`/api/jobs`).
- **Response-shape footgun:** list endpoints return
  `{ "object": "list", "data": [...] }`, not a bare array. Parse with a wrapper:
  ```go
  type HermesList[T any] struct {
      Object string `json:"object"`
      Data   []T    `json:"data"`
  }
  ```
- **Build the thin Hermes client first** (Health, ListSkills, ListToolsets,
  StreamChat, ListJobs, CreateJob, UpdateJob) and validate with `curl` before any
  UI.
- Security = three layers: Tailscale ACL (only your devices), the API key, no public
  port.

### Verified streaming chat contract (tested, working)

The full chain is confirmed live (Mac↔Tailscale↔DO box, TLS, key auth, `/v1/skills`,
and streamed `/v1/chat/completions` reconstructing to the expected text). The console
is now implementation, not research.

Request (note: model is `hermes-agent`, `stream: true`):
```bash
curl -N \
  -H "Authorization: Bearer $HERMES_API_KEY" \
  -H "Content-Type: application/json" \
  https://ubuntu-s-1vcpu-1gb-amd-atl1-01.tail8f89de.ts.net/v1/chat/completions \
  -d '{
    "model": "hermes-agent",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Reply with exactly: hermes tailscale chat ok"}
    ]
  }'
```

Response is **Server-Sent Events** (OpenAI-style chunks):
```
data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}
data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"her"}}]}
data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"mes"}}]}
...
data: {"object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{...}}
data: [DONE]
```

Dashboard parsing rules:
- Parse lines beginning with `data:`; ignore blank lines.
- Stop on `data: [DONE]`.
- Append `choices[0].delta.content` chunks to the visible assistant message.
- `choices[0].finish_reason == "stop"` ⇒ clean completion; read final `usage` if
  present.

**Key-safety / proxy rule (important):** the **browser never holds the API key**.
The frontend calls the dashboard's own Go backend (same origin); the Go backend holds
`HERMES_API_KEY`, calls Hermes, and **re-streams the SSE** to the frontend. This keeps
the key server-side and sidesteps CORS entirely. Never call Hermes directly from
browser JS.

---

## 2. Three stores — and the one hard rule

Be explicit about who writes what. There are **three** stores, deliberately kept
separate:

1. **Hermes memory + job/session store** (on the DO box). Hermes writes here freely
   — its learning, memory, cron state. Fine.
2. **Feed store — agent-owned markdown** (drives the in-dashboard feed, §3). Agents
   write one `.md` per feed item here. You're content with a `.md` DB driving the
   feed, so this is by design — but it is **its own store, not your knowledge
   vault**, and is **excluded from the knowledge index** so it never mixes with your
   hand-authored `people`/`papers` notes or Dataview queries. Approvals live here too
   (agent-owned operational markdown).
3. **Your Obsidian knowledge vault.** AI is **read-only, always.** This is "THIS DB"
   from your rule — people, papers, daily notes, goals. Agents read it for context;
   they never author a note in it.

The bridge from (2) to (3) is **you**: "Save to vault" on a feed item or artifact
promotes it into a real `people`/`papers` note — *your* write, your categories.
Agents never cross that line.

**Where it physically lives (decided):** keep *all* agent operational state
**outside the Obsidian vault**, under `~/.config/manifest/agents/` — `profiles/`,
`feed/`, `approvals/`, `artifacts/`. Filesystem separation is a stronger guarantee
than "excluded from the index": your knowledge vault then contains **zero**
agent-written bytes. If you want the feed visible inside Obsidian later, mirror it
**read-only** (symlink) — but the source of truth stays outside. (This refines the
earlier `vault/Feed/` idea per your agent's review; it's the right call.)

## 2.5 How feed items reach your Mac (the missing piece)

The gap your agent caught: domain-scout runs on the **DO box** (that's where Hermes
is), but the feed store lives on your **Mac**. Hermes cannot write your Mac
filesystem. Resolution — **Hermes generates; the dashboard materializes:**

- Schedule the scout as a **Hermes job** (`/api/jobs`) on the DO box — always-on,
  fires even when your Mac is asleep.
- The job's brief tells it to **emit feed items as structured output** (a JSON array
  or fenced markdown blocks) *in its response* — it does **not** try to write files
  (it can't reach your Mac anyway).
- The **dashboard** (on your Mac), whenever it's online, pulls new job-run outputs
  over Tailscale, parses them, and **writes the `.md` feed items into the local feed
  store**, deduped by `id`. On wake it backfills any runs it missed.
- Net: Hermes never touches your Mac; the **dashboard is the only writer** of local
  files; the feed is local (offline-readable) and durable; always-on *generation*
  (DO) is cleanly decoupled from *storage/rendering* (Mac).
- Same pattern for **artifacts** (options-scout) and **approvals** (ea-coordinator):
  the agent returns structured output via the API; the dashboard materializes it
  locally.

> Verify during Phase 0: does `/api/jobs` expose **historical run outputs** for the
> pull-and-materialize flow? If a past run's output isn't retrievable, fallback —
> the dashboard owns a lightweight local schedule that calls `/v1/chat/completions`
> with the profile when the Mac is online (simpler, but misses runs while asleep).
> Prefer Hermes-cron + pull.

---

## 3. The three surfaces (build order)

### Step 1 — Live console (ship first; immediate value)
A chat panel in the Agents tab that streams `/v1/chat/completions` from Hermes over
Tailscale. Pick a profile (§4), type, watch the response + inline tool progress.
This is "query my agent directly on its server," in the dashboard.

### Step 2 — Profiles manager
CRUD for profiles (markdown files; §4). Populate the tool list from Hermes's
`/v1/skills` + `/v1/toolsets` so you only grant tools that exist. Applying a profile
parameterizes the API call (system brief + tool/permission scope + model tier).

### Step 3 — The Feed (your personal X, `.md`-backed, in the dashboard)
The centerpiece. domain-scout (and options-scout) write feed items as markdown into
the **Feed store**; the dashboard renders them as a **personalized, infinite-scroll
feed** — your own X timeline, made just for you.

**Feed item = one `.md` file** with frontmatter (richer than the first draft — these
fields are load-bearing for filtering/promotion later, not bloat):
```markdown
---
id: 2026-06-30-domain-scout-bioelectricity-paper-001   # stable; dedupe key
type: paper            # paper | person | company | finding | artifact
title: <headline>
why: <one line on why it matters to you>
link: https://...
source: <where it came from>
agent: domain-scout
profile: domain-scout
domain: bioelectricity
date: 2026-06-30T14:00:00Z
status: new            # new | kept | discarded | snoozed
confidence: medium     # low | medium | high
vault_note:            # set to the note path when promoted (Save to vault)
snooze_until:          # timestamp when snoozed
tags: []
---
<optional body: a short summary, or the full comparison for an artifact>
```

**The scroll UX:**
- Newest-first cards, infinite scroll, scannable: title, the *why* line, source, a
  type chip. Feels like a feed, not an inbox.
- Per-card actions: **keep / discard / snooze** — the dashboard writes `status` back
  to the item's frontmatter (a UI action, not the agent).
- Filters/tabs by `type` and domain; a "for you" default ordering.
- **Save to vault** on a kept item creates a real `people`/`papers` note (your
  write); the feed item keeps a link to it.
- **Artifacts** (options-scout's 5-option comparisons) are just `type: artifact`
  items with a richer body, rendered expanded.

This is the fix for "noise that occasionally yields value": the stream is always
there to skim like a timeline, value is one tap to keep, and nothing is lost —
it's durable markdown on your machine, readable even if Hermes is down.

### Step 4 — Cron management + observability
- Manage Hermes crons via `/api/jobs` (schedule domain-scout; pause/edit). Surface
  each job's schedule + **last-run health** so nothing runs silently again.
- Dashboard shows: active sessions, recent runs + status, artifacts, feed items,
  pending approvals.

### Step 5 — EA coordinator + approvals gate (after comms read is wired)
Side-effectful work. The agent **drafts and proposes**; the dashboard collects
proposals into an **approvals** queue (outside the vault); **you** approve/send.
Never autonomous sends, refunds, purchases, or money movement.

---

## 4. Seeded profiles (mapped to your actual jobs)

Profiles are markdown (frontmatter + brief). Model tier (`strong`/`cheap`) maps to
concrete models in config.

```markdown
---
name: options-scout
model: strong
tools: [web.search, files, vault.read]
permissions: read-only        # delivers an artifact to the outbox; never buys; never writes vault
schedule: none                # on-demand
---
Given a request like "buy X amount of Y, find 5 options," research the market and
deliver a comparison artifact: options, price, key specs, pros/cons, links, source,
and a recommendation. Do not purchase or enter any credentials. Output the artifact
to the outbox for Benjamin to review.
```

```markdown
---
name: domain-scout
model: cheap            # gather cheap; escalate to strong only to summarize
tools: [web.search, vault.read, files]
permissions: read-only
schedule: "0 7 * * *"   # daily; managed via /api/jobs
---
Scan for new people, papers, companies, and findings relevant to Benjamin's biotech
domain (read his vault to learn the domain). Produce a short feed of candidates,
each one line: what it is, why it matters, link, source. Push to the research feed;
do not write vault notes.
```

```markdown
---
name: ea-coordinator
model: strong
tools: [calendar.read, email.read, web.search, files]   # email.draft only, never send
permissions: propose-only        # all sends/irreversible steps -> approvals/
schedule: none
---
Executive-assistant tasks. Examples: "draft a reply to X with time suggestions"
(read the calendar for free slots, draft the reply); "coordinate a refund on X"
(draft the message + a step plan). Put every outgoing message or irreversible step
into approvals for Benjamin to send. Never send, pay, or move money yourself.
```

Add profiles only as repeated needs appear; specialize by job; keep one
orchestrator (Hermes), no agent-spawns-agent.

---

## 5. Acceptance criteria

- From the dashboard, a streamed chat with Hermes works over Tailscale using the API
  key; tool progress shows inline; no public port is open.
- Profiles CRUD writes markdown; the tool picker is populated from Hermes
  `/v1/skills` + `/v1/toolsets`; selecting a profile changes model/tools/brief for
  the session.
- options-scout returns a comparison artifact to the outbox; "Save to vault" creates
  a note only on the user's click.
- domain-scout (scheduled via `/api/jobs`) writes `.md` feed items; the dashboard
  renders them as an infinite-scroll feed; keep/discard/snooze writes `status` back
  to frontmatter; each cron shows last-run health.
- ea-coordinator drafts to the approvals queue; nothing sends without explicit
  human approval; no purchases/credentials/money movement ever.
- The vault is provably AI-untouched: every agent write lands outside it; only
  user actions create vault notes.

---

## 6. Sequencing & dependencies

- Steps 1–2 (console + profiles) and 3–4 (research feed + crons) are **read-only and
  safe** — build these first; they're immediately useful and can't harm anything.
- Step 5 (ea-coordinator) depends on **Gmail read + calendar** being wired (the
  comms/calendar work) and on the **approvals** gate — schedule it after those.

## 7. Open items

1. ✅ Decided — all agent state under `~/.config/manifest/agents/`, outside the
   vault (optional read-only Obsidian mirror later).
2. ⚠️ **The one real unknown:** does `/api/jobs` expose **historical run outputs**
   so the dashboard can pull-and-materialize feed items? Verify in Phase 0; else use
   the local-schedule fallback (§2.5).
3. Map `strong` / `cheap` tiers to concrete models in Hermes config.
4. ✅ Decided — profiles = local markdown presets; revisit Hermes-native skills
   later (a profile may later reference `skills: [...]`).
5. Approvals: own markdown queue for MVP; revisit Hermes-native approval routing
   later. SSE passthrough for streaming.

---

## Sources

- Hermes API server: https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server
- Hermes Agent docs + releases: https://hermes-agent.nousresearch.com/docs/ ·
  https://github.com/NousResearch/hermes-agent/releases
- Tailscale Serve + ACLs: https://tailscale.com/kb/1018/acls ·
  https://tailscale.com/docs/features/magicdns
- Private self-hosted LLM access pattern: https://tailscale.com/blog/self-host-a-local-ai-stack
