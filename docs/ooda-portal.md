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

**Seats today:** BA (`ben@ooda.group`), SM (`stephen@ooda.group`),
BPA (`bpabbassa@att.net`), BF (`brian@ooda.group`), OS
(`me@olgasobkiv.com` AND `olga.sobkiv1999@gmail.com` — she signs in with
the Gmail; the O365 address stays for contact/attribution; both map to
OS, which is fine — the map is many-emails → one-initials), SA
(`sydney5161@yahoo.com`). ⚠ The two Brians:
**`brian@ooda.group` is Brian FROMAL (BF)**, and Brian ANDERSON (BPA) is
`bpabbassa@att.net` — this mapping was wrong until 2026-08-21 (both
resolved to BPA; no activity had come from the address, so nothing was
misattributed). `IS` (Igor Sobkiv) has **no address and therefore no
seat** — he stays assignable in `people.md` and appears as a work owner,
but cannot sign in until an address is added above.

Partner emails also live on the `people.md` rows as `[email:: …]` — that
field feeds portal attribution AND the mailbox-sync relevance filter, so
keep it and this file agreeing.

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

**The harness tree is shared by two parties** — manifest (as `benjamin`)
writes orders into `vessel/spool`, zeck claims them by rename and writes
`artifacts/`. They share it through the **`zeckshare` setgid group**, the same
pattern `/shared/apps/kairos` uses. ACLs were tried first and failed: dirs
created *after* the ACL was applied inherited nothing, so every spool write
died "permission denied". setgid inherits automatically. Full recipe in
`deploy/zeck-runner.service`. ⚠ **Adding benjamin to the group requires
`systemctl restart manifest`** — systemd resolves a unit's groups at process
start, so a running manifest keeps the old set and the fix looks like it did
nothing.

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

## What the money words mean

The three dashboard figures are the owner's definitions, and the portal
states them on the page so a partner never has to guess:

| figure | counts |
|---|---|
| **committed** | the total budget of every project we already OWN, plus what it costs to close the ones under contract |
| **paid** | cash we can verify left a bank account — ledger expense rows, plus the purchase price of a deal that actually closed |
| **plan to go** | what remains on the projects we own, plus the whole budget of the ones we are closing on |

Two figures ride alongside because "committed" used to blur them:
**contracted** (Σ signed contract allocations — the auditable number) and
**recognized** (paid plus work finished at a firm price that has no expense
row yet).

⚠ **`control: owned` does NOT mean the purchase happened.** The vault sets it
the day a deal is signed. The closing test is STATUS — `realestate.AcqStateOf`
is the single definition, and every surface reads `property.acq` rather than
re-deriving it. Before that existed, 28 of the Garden SPE's 32 parcels reported
as owned and their $558,000 of unclosed purchase prices counted as spent.

## The map

`GET /api/ooda/map` composes two layers from the live vault — our holdings
(colored by acquisition state, filtered by `oodaVisibleProps` so the research
tail never leaks) and the researched parcels with their assessor facts. It is
cached against the projection revision and fetched lazily on first MAP entry.
Leaflet loads from cdnjs on demand; there is no build step and no API key,
and the tiles are keyless CARTO.

### Where the parcels come from — two layers, on purpose

| layer | source | count | clickable for |
|---|---|---|---|
| **research records** | `system/realestate/parcels/*.md` (via `cmd/parcel-pull`) | 175 | assessor facts **+ the owner's `## log` notes** |
| **study** | `<rePortalPath>/public/study-parcels.geojson` | the other ~2,270 | assessor facts only |

Together they are the full 2,448 lots `ooda.group/parcels` renders — Fountain
Park, Lewis Place, and west of Kingshighway north of Page. A lot we HOLD is
drawn once, as a property; a lot that has a research record is drawn as the
record, so the owner's notes always win. Nothing draws twice.

The study half is a FILE, not vault records, and deliberately so: turning
~2,270 un-annotated lots into records would put ~6,800 files in the vault to
hold facts nobody has written a word about, and would bloat every index and
mtime scan that walks the realestate root. `bgParcels.json` is the same
pattern. The 175 stay records because they carry the owner's thinking.

**Reading it from the re-portal checkout** (`rePortalPath`, already configured
on metis) means both maps and the public page share one snapshot, and
refreshing it is `git pull` in `~/re-portal` — not a hand copy per host.
`<dataDir>/realestate/studyParcels.json` is the fallback for a host with no
checkout. Missing on both → the map still works, just narrower.

To refresh the assessor data, re-run `scripts/build-study-parcels.py` in the
re-portal repo and commit the result; the portal picks it up on the next
request (the file's mtime rides in the map's cache key and ETag).

Both payloads gzip (`writeJSONZip`): 1.4 MB → ~200 KB. Cloudflare would have
compressed the portal at the edge anyway, but the cockpit is reached straight
over Tailscale where a phone pays the full weight.

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
