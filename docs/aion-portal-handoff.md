# AION Portal Move — Phase 1 handoff (2026-08-13)

## Where the work is
- **Branch:** `aion-portal-move` in `/home/benjamin/src/manifest` (local only — NOT pushed, autodeploy NOT triggered, live site unchanged)
- **Commit:** 973415a "phase 1: copy aion team portal into manifest; serve on :7778 (additive)"
- **`main`** is unchanged at 32ea5cd; aionbio repo untouched; `:7777` dashboard and live aion.bio still serving.

## What Phase 1 did (verified green)
- Copied `aionbio/public/portal/**` → `manifest/server/web/portal/**` (byte-identical)
- New `server/portal.go` — separate portal mux served on new port **7778** (default) via `go:embed web`
- `config.go` `PortalPort` (default 7778), `main.go` second goroutine listener (fail-safe: bind error never takes 7777 down)
- Tests: `go build/vet/test` green; `:7778` serves portal, `:7777` dashboard unaffected

## Decisions locked (from the conversation) — carry into Phase 2+
- Move **only** the team portal; aionbio keeps serving whitepaper/investor/dd/careers
- Self-host on metis via **Tailscale Funnel** at **custom** subdomain `portal.aion.bio` (user controls DNS, will point it when ready)
- Auth: **Google OAuth**, **any `@aion.bio`** account = authorized (wildcard)
- Read: **open read**, **login to write**
- Permissions: members change status/fields **only on items assigned to them**; anyone can comment
- New items: members **add for themselves** tagged `team/` (visible distinguishing mark); can **propose additions for others** → entered as proposals that **either you (owner) or the target approves**
- Team data lives in **derived state** on metis with a **git-trail**, stored in **`/shared`** (`/shared/apps/aion-portal` already created+chowned to benjamin via sudo)
- FEED: team writes surface as **notices** (existing kind, no §5 amendment needed)
- Constitution: `ARCHITECTURE.md` §1 "one user, one writer" needs a **dated amendment (§12)** to record the authorized-many-writers portal as an added class — include in the change
- On aionbio side: do NOT remove the portal there until we're ready; after move, retire the aionbio remote push for the portal (publish becomes writing into manifest's served tree + committing)

## Phase 1 open notes (carry forward)
- Portal also reachable on 7777 at `/portal/*` (side effect of recursive go:embed + catch-all). Shadow that path in Phase 5 so the private cockpit stays clean.
- `index.html` references `/investor/assets/colors_and_type.css` + `/favicon.png` (absolute paths into aionbio root → 404 on 7778). Phase 2: copy `investor/assets/` alongside or rewrite the two hrefs.

## Next steps (tomorrow)
1. (me) Branch → push when user wants; autodeploy will then serve the portal at `:7778`
2. Domain: user points `portal.aion.bio` DNS at tailnet + get Funnel cert
3. Phase 2: Google OAuth (reuse existing `google_credentials.json` stack) + local assets + shadow /portal on 7777
4. Phase 3: team writes (comments/status/done/add) via git-trail on /shared
5. Phase 4: FEED notices; Phase 5: harden (systemd ReadWritePaths, /shared perms) + amend ARCHITECTURE.md §1/§12
