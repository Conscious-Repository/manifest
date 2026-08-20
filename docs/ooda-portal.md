# OODA portal — operator runbook

`portal.ooda.group` — a private real-estate team surface for the OODA
partners. Third listener (`:7779`) in the one manifest process on metis,
behind the existing Cloudflare tunnel. Team state lives on PRIVATE.

## The shape

| | |
|---|---|
| URL | `portal.ooda.group` → tunnel `741774b4-…` → `127.0.0.1:7779` |
| Sign-in | Google OAuth, own Cloud project in the ooda.group Workspace |
| Who may in | `@ooda.group`, **or** any address named in `emails.json` |
| Team state | `/private/ooda/team` (0700) — activity.log, items.ext.json, emails.json, chat/ |
| Vault | READ-ONLY base. **No portal request ever writes it** (`TestNoPortalWriteTouchesTheVault`) |
| Agent | zeck — `/private/harnesses/zeck`, user `zeck`, inert until its own hermes is installed |

## Adding a partner

**Two steps, and skipping either means they bounce:**

1. Add the line to `/private/ooda/team/emails.json` (0600):
   `"their@address": "XX"` — the initials MUST match `people.md`.
   Live on the next request; no restart.
2. Add them as a **test user** in the Google Cloud consent screen. The
   client is External/Testing, so an unlisted address is rejected by
   Google before manifest's allow-list ever runs.

⚠ The address must hold an actual **Google Account**. A non-Gmail address
can, but does not automatically.

⚠ `emails.json` is BOTH the allow-list and the email→initials map, and
that is an invariant, not a convenience: an address admitted but unmapped
files its work under the address's local part (`me@olgasobkiv.com` →
owner `"me"` instead of `OS`).

**Removing a seat:** delete the line. Effective on the next request.

**Seats today:** BA, SM, BPA (two addresses), OS, SA. `IS` (Igor Sobkiv)
and `BF` (Brian Fromal) have **no address and therefore no seat** — they
stay assignable in `people.md` and appear as work owners, but cannot sign
in until an address is added above.

## What partners can see and do

**See:** everything, flat. Same bytes for admin and partner, **including
raw ledger line items with vendor names and amounts** (owner decision
2026-08-20 — the partners are co-investors). There is no per-role
redaction, so the sign-in gate is the entire boundary. If an admin and a
partner response ever differ, that is the bug.

**Do:** comment on anything · change fields on items assigned to THEM
(assignee lock — **no admin override lane**, mirroring AION's 2026-08-13
decision) · add their own items · propose items for others · file bids.

## The bid lane

A partner files a bid in the portal → it lands as a `kind:"bid"`
**proposal** in the team store and cards into the owner's FEED. Nothing
has touched the vault.

The owner accepts it in the **cockpit** (`GET/POST
/api/realestate/ooda-bids{,/id}`). That is the action that crosses
vaultwriter under `re-contracts` and mints the real contract record —
also minting the contractor record when the bid names a new counterparty.
It materializes as **`status: proposed`**: accepting the BID is not
accepting the CONTRACT, so no partner can commit money.

## zeck

Inert until **its own** hermes is installed at `/home/zeck/.local/bin/hermes`
with its own provider credentials under `/home/zeck/.hermes` (0600, owned
by zeck) — never a copy of the owner's. Then:
`sudo systemctl start zeck-runner`.

**Its isolation is an invariant, not a preference.** zeck reads NOTHING
under `/private/consciousrepo`: the vault is 0700, zeck holds no ACL on
it, and the unit sets `InaccessiblePaths=/private/consciousrepo` so the
vault is absent from its mount namespace even if a permission slips
(verified: root can list the vault without that line and cannot with it).
Grounding reaches zeck only as content **manifest** resolved and spooled
into the work order. Anyone loosening `ProtectSystem`, `InaccessiblePaths`,
or `ReadWritePaths` is undoing the bargain that put zeck on metis.

Telegram for zeck is **not configured** — the owner's own channel lives
inside `~/.hermes/config.yaml` / `.env` (0600, unread). zeck's would live
in `/home/zeck/.hermes`, never in this repo.

## When things break

**`/private` not unlocked** → manifest does not start at all
(`ConditionPathExists=/private/consciousrepo/goals.md`), so the portal is
down and a partner has no self-service. Unlock the LUKS volume and
`sudo systemctl start manifest`.

**"site can't be reached"** → almost certainly a local DNS cache, not the
portal. Public resolvers answer; a Tailscale MagicDNS negative cache does
not. `sudo dscacheutil -flushcache && sudo killall -HUP mDNSResponder`,
or toggle Tailscale.

**Stale numbers** → the portal serves the last good snapshot when the
records fail to recompose, and says so in a banner. Check
`/api/ooda/revision` for the error.

**`/api/team/snapshot`** returns only `{"team": …}` on this portal — the
AION data-file paths it also collects do not exist here. Not a bug.
